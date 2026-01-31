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
      "This looks like a token permission problem for Projects v2. The GraphQL fields used by copy_project require a token with Projects access (classic PAT: scope 'project'; fine-grained PAT: Organization permission 'Projects' and access to the org). Fix: set safe-outputs.copy-project.github-token to a secret PAT that can access the target org project."
    );
  } else if (hasNotFound && /projectV2\b/.test(getErrorMessage(error))) {
    core.info(
      "GitHub returned NOT_FOUND for ProjectV2. This can mean either: (1) the project number is wrong for Projects v2, (2) the project is a classic Projects board (not Projects v2), or (3) the token does not have access to that org/user project."
    );
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
 * Parse project URL into components
 * @param {unknown} projectUrl - Project URL
 * @returns {{ scope: string, ownerLogin: string, projectNumber: string }} Project info
 */
function parseProjectUrl(projectUrl) {
  if (!projectUrl || typeof projectUrl !== "string") {
    throw new Error(`Invalid project input: expected string, got ${typeof projectUrl}. The "sourceProject" field is required and must be a full GitHub project URL.`);
  }

  const match = projectUrl.match(/github\.com\/(users|orgs)\/([^/]+)\/projects\/(\d+)/);
  if (!match) {
    throw new Error(`Invalid project URL: "${projectUrl}". The "sourceProject" field must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/123).`);
  }

  return {
    scope: match[1],
    ownerLogin: match[2],
    projectNumber: match[3],
  };
}

/**
 * Get owner ID for an org or user
 * @param {string} scope - Either "orgs" or "users"
 * @param {string} ownerLogin - Login name of the owner
 * @returns {Promise<string>} Owner node ID
 */
async function getOwnerId(scope, ownerLogin) {
  if (scope === "orgs") {
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
 * Get project node ID from owner and project number
 * @param {string} scope - Either "orgs" or "users"
 * @param {string} ownerLogin - Login name of the owner
 * @param {string} projectNumber - Project number
 * @returns {Promise<string>} Project node ID
 */
async function getProjectId(scope, ownerLogin, projectNumber) {
  const projectNumberInt = parseInt(projectNumber, 10);

  if (scope === "orgs") {
    const result = await github.graphql(
      `query($login: String!, $number: Int!) {
        organization(login: $login) {
          projectV2(number: $number) {
            id
          }
        }
      }`,
      { login: ownerLogin, number: projectNumberInt }
    );
    return result.organization.projectV2.id;
  } else {
    const result = await github.graphql(
      `query($login: String!, $number: Int!) {
        user(login: $login) {
          projectV2(number: $number) {
            id
          }
        }
      }`,
      { login: ownerLogin, number: projectNumberInt }
    );
    return result.user.projectV2.id;
  }
}

/**
 * Copy a project using the copyProjectV2 mutation
 * @param {object} output - Safe output entry
 * @returns {Promise<{ projectId: string, projectTitle: string, projectUrl: string }>}
 */
async function copyProject(output) {
  // Use environment variables as defaults if fields are not provided
  const defaultSourceProject = process.env.GH_AW_COPY_PROJECT_SOURCE;
  const defaultTargetOwner = process.env.GH_AW_COPY_PROJECT_TARGET_OWNER;

  const { sourceProject, owner, title, includeDraftIssues } = output;

  // Use provided values or fall back to defaults
  const effectiveSourceProject = sourceProject || defaultSourceProject;
  const effectiveOwner = owner || defaultTargetOwner;

  if (!effectiveSourceProject) {
    throw new Error('The "sourceProject" field is required. It must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/123). Provide it in the tool call or configure "source-project" in the workflow frontmatter.');
  }

  if (!effectiveOwner) {
    throw new Error('The "owner" field is required. It must be the owner login name (org or user) where the new project will be created. Provide it in the tool call or configure "target-owner" in the workflow frontmatter.');
  }

  if (!title) {
    throw new Error('The "title" field is required. It specifies the title of the new project.');
  }

  // Default to false if not specified
  const shouldIncludeDraftIssues = includeDraftIssues === true;

  core.info(`Copying project from: ${effectiveSourceProject}`);
  core.info(`New project owner: ${effectiveOwner}`);
  core.info(`New project title: ${title}`);
  core.info(`Include draft issues: ${shouldIncludeDraftIssues}`);

  // Parse source project URL
  const sourceProjectInfo = parseProjectUrl(effectiveSourceProject);
  core.info(`Source project - scope: ${sourceProjectInfo.scope}, owner: ${sourceProjectInfo.ownerLogin}, number: ${sourceProjectInfo.projectNumber}`);

  // Get source project ID
  let sourceProjectId;
  try {
    sourceProjectId = await getProjectId(sourceProjectInfo.scope, sourceProjectInfo.ownerLogin, sourceProjectInfo.projectNumber);
    core.info(`Source project ID: ${sourceProjectId}`);
  } catch (err) {
    // prettier-ignore
    const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
    logGraphQLError(error, "getting source project ID");
    throw new Error(`Failed to get source project ID: ${getErrorMessage(error)}`);
  }

  // Determine target owner scope (try org first, then user)
  let targetOwnerId;
  let targetScope;
  try {
    targetOwnerId = await getOwnerId("orgs", effectiveOwner);
    targetScope = "orgs";
    core.info(`Target owner ID (org): ${targetOwnerId}`);
  } catch (orgError) {
    // prettier-ignore
    const orgGraphQLError = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (orgError);
    core.info(`Failed to find "${effectiveOwner}" as organization: ${getErrorMessage(orgGraphQLError)}`);
    logGraphQLError(orgGraphQLError, `looking up organization "${effectiveOwner}"`);

    try {
      targetOwnerId = await getOwnerId("users", effectiveOwner);
      targetScope = "users";
      core.info(`Target owner ID (user): ${targetOwnerId}`);
    } catch (userError) {
      // prettier-ignore
      const userGraphQLError = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (userError);
      core.info(`Failed to find "${effectiveOwner}" as user: ${getErrorMessage(userGraphQLError)}`);
      logGraphQLError(userGraphQLError, `looking up user "${effectiveOwner}"`);

      throw new Error(`Failed to find owner "${effectiveOwner}" as either an organization or user. Check the logs above for details about the GraphQL errors.`);
    }
  }

  // Execute the copyProjectV2 mutation
  try {
    const result = await github.graphql(
      `mutation CopyProject($sourceProjectId: ID!, $ownerId: ID!, $title: String!, $includeDraftIssues: Boolean!) {
        copyProjectV2(input: {
          projectId: $sourceProjectId
          ownerId: $ownerId
          title: $title
          includeDraftIssues: $includeDraftIssues
        }) {
          projectV2 {
            id
            title
            url
          }
        }
      }`,
      {
        sourceProjectId,
        ownerId: targetOwnerId,
        title,
        includeDraftIssues: shouldIncludeDraftIssues,
      }
    );

    const newProject = result.copyProjectV2.projectV2;
    core.info(`Successfully copied project!`);
    core.info(`New project ID: ${newProject.id}`);
    core.info(`New project title: ${newProject.title}`);
    core.info(`New project URL: ${newProject.url}`);

    return {
      projectId: newProject.id,
      projectTitle: newProject.title,
      projectUrl: newProject.url,
    };
  } catch (err) {
    // prettier-ignore
    const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
    logGraphQLError(error, "copying project");
    throw new Error(`Failed to copy project: ${getErrorMessage(error)}`);
  }
}

/**
 * Main entry point - handler factory that returns a message handler function
 * @param {Object} config - Handler configuration
 * @param {number} [config.max] - Maximum number of copy_project items to process
 * @param {string} [config.source_project] - Default source project URL
 * @param {string} [config.target_owner] - Default target owner
 * @returns {Promise<Function>} Message handler function
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;
  const defaultSourceProject = config.source_project || "";
  const defaultTargetOwner = config.target_owner || "";

  core.info(`Max count: ${maxCount}`);
  if (defaultSourceProject) {
    core.info(`Default source project: ${defaultSourceProject}`);
  }
  if (defaultTargetOwner) {
    core.info(`Default target owner: ${defaultTargetOwner}`);
  }

  // Track state
  let processedCount = 0;

  /**
   * Message handler function that processes a single copy_project message
   * @param {Object} message - The copy_project message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs (unused for copy_project)
   * @returns {Promise<Object>} Result with success/error status and project details
   */
  return async function handleCopyProject(message, resolvedTemporaryIds) {
    // Check max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping copy_project: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    try {
      // Process the copy_project message
      const projectResult = await copyProject(message);

      // Set step outputs
      core.setOutput("project_id", projectResult.projectId);
      core.setOutput("project_title", projectResult.projectTitle);
      core.setOutput("project_url", projectResult.projectUrl);

      return {
        success: true,
        projectId: projectResult.projectId,
        projectTitle: projectResult.projectTitle,
        projectUrl: projectResult.projectUrl,
      };
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      logGraphQLError(error, "copy_project");
      return {
        success: false,
        error: getErrorMessage(error),
      };
    }
  };
}

module.exports = { copyProject, parseProjectUrl, getProjectId, getOwnerId, main };
