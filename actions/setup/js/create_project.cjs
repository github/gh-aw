// @ts-check
// @safe-outputs-exempt SEC-004
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeTemporaryId, isTemporaryId, generateTemporaryId, getOrGenerateTemporaryId } = require("./temporary_id.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { ERR_CONFIG, ERR_NOT_FOUND, ERR_VALIDATION } = require("./error_codes.cjs");
const { logGraphQLError } = require("./github_api_helpers.cjs");

/** @type {import('./github_api_helpers.cjs').GraphQLErrorHints} */
const PROJECT_GRAPHQL_HINTS = {
  insufficientScopesHint:
    "This looks like a token permission problem for Projects v2. The GraphQL fields used by create_project require a token with Projects access (classic PAT: scope 'project'; fine-grained PAT: Organization permission 'Projects' and access to the org). Fix: set safe-outputs.create-project.github-token to a secret PAT that can create projects in the target org.",
  notFoundHint: "GitHub returned NOT_FOUND for ProjectV2. This can mean either: (1) the owner does not exist, or (2) the token does not have access to that org/user.",
  notFoundPredicate: msg => /projectV2\b/.test(msg),
};

/**
 * Get owner ID for an org or user
 * @param {string} ownerType - Either "org" or "user"
 * @param {string} ownerLogin - Login name of the owner
 * @returns {Promise<string>} Owner node ID
 */
async function getOwnerId(ownerType, ownerLogin) {
  if (ownerType === "org") {
    /** @type {{ organization: { id: string } }} */
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
    /** @type {{ user: { id: string } }} */
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

  /** @type {{ createProjectV2: { projectV2: { id: string, number: number, title: string, url: string } } }} */
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

  /** @type {{ addProjectV2ItemById: { item: { id: string } } }} */
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
  /** @type {{ repository: { issue: { id: string } } }} */
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
    throw new Error(`${ERR_VALIDATION}: Invalid project URL: expected string, got ${typeof projectUrl}`);
  }

  const match = projectUrl.match(/github\.com\/(users|orgs)\/([^/]+)\/projects\/(\d+)/);
  if (!match) {
    throw new Error(`${ERR_VALIDATION}: Invalid project URL: "${projectUrl}". Expected format: https://github.com/orgs/myorg/projects/123`);
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

  /** @type {{ node: { views: { nodes: Array<{ id: string, name: string, number: number }> } } }} */
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
    throw new Error(`${ERR_VALIDATION}: View name is required and must be a non-empty string`);
  }

  const layout = typeof viewConfig.layout === "string" ? viewConfig.layout.trim() : "";
  if (!layout || !["table", "board", "roadmap"].includes(layout)) {
    throw new Error(`${ERR_VALIDATION}: Invalid view layout "${layout}". Must be one of: table, board, roadmap`);
  }

  const filter = typeof viewConfig.filter === "string" ? viewConfig.filter : undefined;
  let visibleFields = Array.isArray(viewConfig.visible_fields) ? viewConfig.visible_fields : undefined;

  if (visibleFields) {
    const invalid = visibleFields.filter(v => typeof v !== "number" || !Number.isFinite(v));
    if (invalid.length > 0) {
      throw new Error(`${ERR_VALIDATION}: Invalid visible_fields. Must be an array of numbers (field IDs). Invalid values: ${invalid.map(v => JSON.stringify(v)).join(", ")}`);
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

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  // Use the provided github client, or fall back to the global github object
  // The global github object is available when running via github-script action
  // @ts-ignore - global.github is set by setupGlobals() from github-script context
  const github = githubClient || global.github;

  if (!github) {
    throw new Error(`${ERR_CONFIG}: GitHub client is required but not provided. Either pass a github client to main() or ensure global.github is set by github-script action.`);
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
   * @param {Object} resolvedTemporaryIds - Plain object version of temporaryIdMap for backward compatibility
   * @param {Map<string, {repo?: string, number?: number, projectUrl?: string}>|null} temporaryIdMap - Unified map of temporary IDs
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCreateProject(message, resolvedTemporaryIds = {}, temporaryIdMap = null) {
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

      // Get or generate the temporary ID for this project
      const tempIdResult = getOrGenerateTemporaryId(message, "project");
      if (tempIdResult.error) {
        core.warning(`Skipping project: ${tempIdResult.error}`);
        return {
          success: false,
          error: tempIdResult.error,
        };
      }
      // At this point, temporaryId is guaranteed to be a string (not null)
      const temporaryId = /** @type {string} */ tempIdResult.temporaryId;

      // Resolve temporary ID in item_url if present
      if (item_url && typeof item_url === "string") {
        // Check if item_url contains a temporary ID (either as URL or plain ID)
        // Format: https://github.com/owner/repo/issues/#aw_XXX or #aw_XXX
        const urlMatch = item_url.match(/issues\/(#?aw_[A-Za-z0-9_]{3,12})\s*$/i);
        const plainMatch = item_url.match(/^(#?aw_[A-Za-z0-9_]{3,12})\s*$/i);

        if (urlMatch || plainMatch) {
          const tempIdStr = (urlMatch && urlMatch[1]) || (plainMatch && plainMatch[1]) || "";

          // Check if it's a valid temporary ID
          if (isTemporaryId(tempIdStr)) {
            // Look up in the unified temporaryIdMap (Map) or resolvedTemporaryIds (plain object)
            const normalizedKey = normalizeTemporaryId(tempIdStr);
            const resolved = temporaryIdMap instanceof Map ? temporaryIdMap.get(normalizedKey) : resolvedTemporaryIds[normalizedKey];

            if (resolved && resolved.repo && resolved.number) {
              // Build the proper GitHub issue URL
              const resolvedUrl = `${process.env.GITHUB_SERVER_URL || "https://github.com"}/${resolved.repo}/issues/${resolved.number}`;
              core.info(`Resolved temporary ID ${tempIdStr} in item_url to ${resolvedUrl}`);
              item_url = resolvedUrl;
            } else {
              throw new Error(`${ERR_NOT_FOUND}: Temporary ID '${tempIdStr}' in item_url not found. Ensure create_issue was called before create_project.`);
            }
          }
        }
      }

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
          throw new Error(`${ERR_VALIDATION}: Missing required field 'title' in create_project call and unable to generate from context`);
        }
      }

      // Determine owner - use explicit owner, default, or error
      const targetOwner = owner || defaultTargetOwner;
      if (!targetOwner) {
        throw new Error(`${ERR_VALIDATION}: No owner specified and no default target-owner configured. Either provide 'owner' field or configure 'target-owner' in safe-outputs.create-project`);
      }

      // Determine owner type (org or user)
      const ownerType = owner_type || "org"; // Default to org if not specified

      core.info(`Creating project "${title}" for ${ownerType}/${targetOwner}`);

      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would create project "${title}"`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            title,
            ownerType,
            targetOwner,
            temporaryId,
          },
        };
      }

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

      // Store temporary ID mapping so subsequent operations can reference this project
      const normalizedTempId = normalizeTemporaryId(temporaryId ?? "");
      if (temporaryIdMap instanceof Map) {
        temporaryIdMap.set(normalizedTempId, { projectUrl: projectInfo.projectUrl });
      }
      core.info(`Stored temporary ID mapping: ${temporaryId} -> ${projectInfo.projectUrl}`);

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
            logGraphQLError(error, `Creating configured view: ${viewConfig.name}`, PROJECT_GRAPHQL_HINTS);
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
        temporaryId,
      };
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      logGraphQLError(error, "create_project", PROJECT_GRAPHQL_HINTS);
      return {
        success: false,
        error: getErrorMessage(error),
      };
    }
  };
}

module.exports = { main };
