// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Log detailed GraphQL error information
 * @param {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} error - GraphQL error
 * @param {string} operation - Operation description
 */
function logGraphQLError(error, operation) {
  core.info(`GraphQL Error during: ${operation}`);
  core.info(`Message: ${getErrorMessage(error)}`);

  const errorList = Array.isArray(error.errors) ? error.errors : [];
  const hasInsufficientScopes = errorList.some(e => e?.type === "INSUFFICIENT_SCOPES");
  const hasNotFound = errorList.some(e => e?.type === "NOT_FOUND");

  if (hasInsufficientScopes) {
    core.info(
      "This looks like a token permission problem for Projects v2. The GraphQL fields used by create_project require a token with Projects access (classic PAT: scope 'project'; fine-grained PAT: Organization permission 'Projects' and access to the org). Fix: set safe-outputs.create-project.github-token to a secret PAT that can create projects in the target org."
    );
  } else if (hasNotFound && /projectV2\b/.test(getErrorMessage(error))) {
    core.info("GitHub returned NOT_FOUND for ProjectV2. This can mean either: (1) the owner does not exist, or (2) the token does not have access to that org/user.");
  }

  if (error.errors) {
    core.info(`Errors array (${error.errors.length} error(s)):`);
    error.errors.forEach((err, idx) => {
      core.info(`  [${idx + 1}] ${err.message}`);
      if (err.type) core.info(`      Type: ${err.type}`);
      if (err.path) core.info(`      Path: ${JSON.stringify(err.path)}`);
      if (err.locations) core.info(`      Locations: ${JSON.stringify(err.locations)}`);
    });
  }

  if (error.request) core.info(`Request: ${JSON.stringify(error.request, null, 2)}`);
  if (error.data) core.info(`Response data: ${JSON.stringify(error.data, null, 2)}`);
}

/**
 * Get owner ID for an org or user
 * @param {string} ownerType - Either "org" or "user"
 * @param {string} ownerLogin - Login name of the owner
 * @returns {Promise<string>} Owner node ID
 */
async function getOwnerId(ownerType, ownerLogin) {
  if (ownerType === "org") {
    const result = await github.graphql(
      `query($login: String!) {
        organization(login: $login) {
          id
        }
      }`,
      { login: ownerLogin }
    );
    return result.organization.id;
  } else {
    const result = await github.graphql(
      `query($login: String!) {
        user(login: $login) {
          id
        }
      }`,
      { login: ownerLogin }
    );
    return result.user.id;
  }
}

/**
 * Create a new GitHub Project V2
 * @param {string} ownerId - Owner node ID
 * @param {string} title - Project title
 * @returns {Promise<{ projectId: string, projectNumber: number, projectTitle: string, projectUrl: string, itemId?: string }>} Created project info
 */
async function createProjectV2(ownerId, title) {
  core.info(`Creating project with title: "${title}"`);

  const result = await github.graphql(
    `mutation($ownerId: ID!, $title: String!) {
      createProjectV2(input: { ownerId: $ownerId, title: $title }) {
        projectV2 {
          id
          number
          title
          url
        }
      }
    }`,
    { ownerId, title }
  );

  const project = result.createProjectV2.projectV2;
  core.info(`✓ Created project #${project.number}: ${project.title}`);
  core.info(`  URL: ${project.url}`);

  return {
    projectId: project.id,
    projectNumber: project.number,
    projectTitle: project.title,
    projectUrl: project.url,
  };
}

/**
 * Add an item to a project
 * @param {string} projectId - Project node ID
 * @param {string} contentId - Content node ID (issue, PR, etc.)
 * @returns {Promise<string>} Item ID
 */
async function addItemToProject(projectId, contentId) {
  core.info(`Adding item to project...`);

  const result = await github.graphql(
    `mutation($projectId: ID!, $contentId: ID!) {
      addProjectV2ItemById(input: { projectId: $projectId, contentId: $contentId }) {
        item {
          id
        }
      }
    }`,
    { projectId, contentId }
  );

  const itemId = result.addProjectV2ItemById.item.id;
  core.info(`✓ Added item to project`);

  return itemId;
}

/**
 * Get issue node ID from issue number
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<string>} Issue node ID
 */
async function getIssueNodeId(owner, repo, issueNumber) {
  const result = await github.graphql(
    `query($owner: String!, $repo: String!, $issueNumber: Int!) {
      repository(owner: $owner, name: $repo) {
        issue(number: $issueNumber) {
          id
        }
      }
    }`,
    { owner, repo, issueNumber }
  );

  return result.repository.issue.id;
}

/**
 * Parse project URL into components
 * @param {string} projectUrl - Project URL
 * @returns {{ scope: string, ownerLogin: string, projectNumber: string }} Project info
 */
function parseProjectUrl(projectUrl) {
  if (!projectUrl || typeof projectUrl !== "string") {
    throw new Error(`Invalid project URL: expected string, got ${typeof projectUrl}`);
  }

  const match = projectUrl.match(/github\.com\/(users|orgs)\/([^/]+)\/projects\/(\d+)/);
  if (!match) {
    throw new Error(`Invalid project URL: "${projectUrl}". Expected format: https://github.com/orgs/myorg/projects/123`);
  }

  return {
    scope: match[1],
    ownerLogin: match[2],
    projectNumber: match[3],
  };
}

/**
 * List all views for a project
 * @param {string} projectId - Project node ID
 * @returns {Promise<Array<{id: string, name: string, number: number}>>} Array of views
 */
async function listProjectViews(projectId) {
  core.info(`Listing views for project...`);

  const result = await github.graphql(
    `query($projectId: ID!) {
      node(id: $projectId) {
        ... on ProjectV2 {
          views(first: 20) {
            nodes {
              id
              name
              number
            }
          }
        }
      }
    }`,
    { projectId }
  );

  const views = result.node.views.nodes;
  core.info(`Found ${views.length} view(s) in project`);

  return views;
}

/**
 * Create a project view
 * @param {string} projectUrl - Project URL
 * @param {Object} viewConfig - View configuration
 * @param {string} viewConfig.name - View name
 * @param {string} viewConfig.layout - View layout (table, board, roadmap)
 * @param {string} [viewConfig.filter] - View filter
 * @param {Array<number>} [viewConfig.visible_fields] - Visible field IDs
 * @param {string} [viewConfig.description] - View description (not supported by GitHub API, will be ignored)
 * @returns {Promise<void>}
 */
async function createProjectView(projectUrl, viewConfig) {
  const projectInfo = parseProjectUrl(projectUrl);
  const projectNumber = parseInt(projectInfo.projectNumber, 10);

  const name = typeof viewConfig.name === "string" ? viewConfig.name.trim() : "";
  if (!name) {
    throw new Error("View name is required and must be a non-empty string");
  }

  const layout = typeof viewConfig.layout === "string" ? viewConfig.layout.trim() : "";
  if (!layout || !["table", "board", "roadmap"].includes(layout)) {
    throw new Error(`Invalid view layout "${layout}". Must be one of: table, board, roadmap`);
  }

  const filter = typeof viewConfig.filter === "string" ? viewConfig.filter : undefined;
  let visibleFields = Array.isArray(viewConfig.visible_fields) ? viewConfig.visible_fields : undefined;

  if (visibleFields) {
    const invalid = visibleFields.filter(v => typeof v !== "number" || !Number.isFinite(v));
    if (invalid.length > 0) {
      throw new Error(`Invalid visible_fields. Must be an array of numbers (field IDs). Invalid values: ${invalid.map(v => JSON.stringify(v)).join(", ")}`);
    }
  }

  if (layout === "roadmap" && visibleFields && visibleFields.length > 0) {
    core.warning('visible_fields is not applicable to layout "roadmap"; ignoring.');
    visibleFields = undefined;
  }

  if (typeof viewConfig.description === "string" && viewConfig.description.trim()) {
    core.warning("view.description is not supported by the GitHub Projects Views API; ignoring.");
  }

  const route = projectInfo.scope === "orgs" ? "POST /orgs/{org}/projectsV2/{project_number}/views" : "POST /users/{user_id}/projectsV2/{project_number}/views";

  const params =
    projectInfo.scope === "orgs"
      ? {
          org: projectInfo.ownerLogin,
          project_number: projectNumber,
          name,
          layout,
          ...(filter ? { filter } : {}),
          ...(visibleFields ? { visible_fields: visibleFields } : {}),
        }
      : {
          user_id: projectInfo.ownerLogin,
          project_number: projectNumber,
          name,
          layout,
          ...(filter ? { filter } : {}),
          ...(visibleFields ? { visible_fields: visibleFields } : {}),
        };

  core.info(`Creating project view: ${name} (${layout})...`);
  const response = await github.request(route, params);
  const created = response?.data;

  if (created?.id) {
    core.info(`✓ Created view: ${name} (ID: ${created.id})`);
  } else {
    core.info(`✓ Created view: ${name}`);
  }
}

/**
 * Main entry point - handler factory that returns a message handler function
 * @param {Object} config - Handler configuration
 * @param {Object} githubClient - GitHub client (Octokit instance) to use for API calls
 * @returns {Promise<Function>} Message handler function
 */
async function main(config = {}, githubClient = null) {
  // Extract configuration
  const defaultTargetOwner = config.target_owner || "";
  const maxCount = config.max || 1;
  const titlePrefix = config.title_prefix || "Project";
  const configuredViews = Array.isArray(config.views) ? config.views : [];

  // Use the provided github client, or fall back to the global github object
  // The global github object is available when running via github-script action
  // @ts-ignore - global.github is set by setupGlobals() from github-script context
  const github = githubClient || global.github;

  if (!github) {
    throw new Error("GitHub client is required but not provided. Either pass a github client to main() or ensure global.github is set by github-script action.");
  }

  if (defaultTargetOwner) {
    core.info(`Default target owner: ${defaultTargetOwner}`);
  }
  core.info(`Max count: ${maxCount}`);
  if (config.title_prefix) {
    core.info(`Title prefix: ${titlePrefix}`);
  }
  if (configuredViews.length > 0) {
    core.info(`Found ${configuredViews.length} configured view(s) in frontmatter`);
  }

  // Track state
  let processedCount = 0;

  /**
   * Message handler function that processes a single create_project message
   * @param {Object} message - The create_project message to process
   * @param {Map<string, {repo?: string, number?: number, projectUrl?: string}>} temporaryIdMap - Unified map of temporary IDs
   * @param {Object} resolvedTemporaryIds - Plain object version of temporaryIdMap for backward compatibility
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCreateProject(message, temporaryIdMap, resolvedTemporaryIds = {}) {
    // Check max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create_project: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    try {
      let { title, owner, owner_type, item_url } = message;

      // Generate a title if not provided by the agent
      if (!title) {
        // Try to generate a project title from the issue context
        const issueTitle = context.payload?.issue?.title;
        const issueNumber = context.payload?.issue?.number;

        if (issueTitle) {
          // Use the issue title with the configured prefix
          title = `${titlePrefix}: ${issueTitle}`;
          core.info(`Generated title from issue: "${title}"`);
        } else if (issueNumber) {
          // Fallback to issue number if no title is available
          title = `${titlePrefix} #${issueNumber}`;
          core.info(`Generated title from issue number: "${title}"`);
        } else {
          throw new Error("Missing required field 'title' in create_project call and unable to generate from context");
        }
      }

      // Determine owner - use explicit owner, default, or error
      const targetOwner = owner || defaultTargetOwner;
      if (!targetOwner) {
        throw new Error("No owner specified and no default target-owner configured. Either provide 'owner' field or configure 'target-owner' in safe-outputs.create-project");
      }

      // Determine owner type (org or user)
      const ownerType = owner_type || "org"; // Default to org if not specified

      core.info(`Creating project "${title}" for ${ownerType}/${targetOwner}`);

      // Get owner ID
      const ownerId = await getOwnerId(ownerType, targetOwner);

      // Create the project
      const projectInfo = await createProjectV2(ownerId, title);

      // If item_url is provided, add it to the project
      if (item_url) {
        core.info(`Adding item to project: ${item_url}`);

        // Parse item URL to get issue number
        const urlMatch = item_url.match(/github\.com\/([^/]+)\/([^/]+)\/issues\/(\d+)/);
        if (urlMatch) {
          const [, itemOwner, itemRepo, issueNumberStr] = urlMatch;
          const issueNumber = parseInt(issueNumberStr, 10);

          // Get issue node ID
          const contentId = await getIssueNodeId(itemOwner, itemRepo, issueNumber);

          // Add item to project
          const itemId = await addItemToProject(projectInfo.projectId, contentId);
          projectInfo.itemId = itemId;
        } else {
          core.warning(`Could not parse item URL: ${item_url}`);
        }
      }

      core.info(`✓ Successfully created project: ${projectInfo.projectUrl}`);

      // Create configured views if any
      if (configuredViews.length > 0) {
        core.info(`Creating ${configuredViews.length} configured view(s) on project: ${projectInfo.projectUrl}`);

        for (let i = 0; i < configuredViews.length; i++) {
          const viewConfig = configuredViews[i];
          try {
            await createProjectView(projectInfo.projectUrl, viewConfig);
            core.info(`✓ Created view ${i + 1}/${configuredViews.length}: ${viewConfig.name} (${viewConfig.layout})`);
          } catch (err) {
            // prettier-ignore
            const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
            core.error(`Failed to create configured view ${i + 1}: ${viewConfig.name}`);
            logGraphQLError(error, `Creating configured view: ${viewConfig.name}`);
          }
        }

        // Note: GitHub's default "View 1" will remain. The deleteProjectV2View GraphQL mutation
        // is not documented and may not work reliably. Configured views are created as additional views.
      }

      // Return result
      return {
        success: true,
        projectId: projectInfo.projectId,
        projectNumber: projectInfo.projectNumber,
        projectTitle: projectInfo.projectTitle,
        projectUrl: projectInfo.projectUrl,
        itemId: projectInfo.itemId,
      };
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      logGraphQLError(error, "create_project");
      return {
        success: false,
        error: getErrorMessage(error),
      };
    }
  };
}

module.exports = { main };
