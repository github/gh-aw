// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_project_status_update";

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
      "This looks like a token permission problem for Projects v2. The GraphQL fields used by create-project-status-update require a token with Projects access (classic PAT: scope 'project'; fine-grained PAT: Organization permission 'Projects' and access to the org). Fix: set safe-outputs.create-project-status-update.github-token to a secret PAT that can access the target org project."
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
    throw new Error(`Invalid project input: expected string, got ${typeof projectUrl}. The "project" field is required and must be a full GitHub project URL.`);
  }

  const match = projectUrl.match(/github\.com\/(users|orgs)\/([^/]+)\/projects\/(\d+)/);
  if (!match) {
    throw new Error(`Invalid project URL: "${projectUrl}". The "project" field must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/123).`);
  }

  return {
    scope: match[1],
    ownerLogin: match[2],
    projectNumber: match[3],
  };
}

/**
 * List accessible Projects v2 for org or user
 * @param {{ scope: string, ownerLogin: string, projectNumber: string }} projectInfo - Project info
 * @returns {Promise<{ nodes: Array<{ id: string, number: number, title: string, closed?: boolean, url: string }>, totalCount?: number, diagnostics: { rawNodesCount: number, nullNodesCount: number, rawEdgesCount: number, nullEdgeNodesCount: number } }>} List result
 */
async function listAccessibleProjectsV2(projectInfo) {
  const baseQuery = `projectsV2(first: 100) {
    totalCount
    nodes {
      id
      number
      title
      closed
      url
    }
  }`;

  const query =
    projectInfo.scope === "orgs"
      ? `query($login: String!) {
        organization(login: $login) {
          ${baseQuery}
        }
      }`
      : `query($login: String!) {
      user(login: $login) {
        ${baseQuery}
      }
    }`;

  const result = await github.graphql(query, { login: projectInfo.ownerLogin });
  const conn = projectInfo.scope === "orgs" ? result?.organization?.projectsV2 : result?.user?.projectsV2;

  const rawNodes = Array.isArray(conn?.nodes) ? conn.nodes : [];
  const nodes = rawNodes.filter(Boolean);

  return {
    nodes: nodes,
    totalCount: conn?.totalCount,
    diagnostics: {
      rawNodesCount: rawNodes.length,
      nullNodesCount: rawNodes.length - nodes.length,
      rawEdgesCount: 0,
      nullEdgeNodesCount: 0,
    },
  };
}

/**
 * Summarize list of projects
 * @param {Array<{ number: number, title: string, closed?: boolean }>} projects - Projects list
 * @param {number} [limit=20] - Max number to show
 * @returns {string} Summary string
 */
function summarizeProjectsV2(projects, limit = 20) {
  if (!Array.isArray(projects) || projects.length === 0) {
    return "(none)";
  }

  const normalized = projects
    .filter(p => p && typeof p.number === "number" && typeof p.title === "string")
    .slice(0, limit)
    .map(p => `#${p.number} ${p.closed ? "(closed) " : ""}${p.title}`);

  return normalized.length > 0 ? normalized.join("; ") : "(none)";
}

/**
 * Summarize empty projects list with diagnostics
 * @param {{ totalCount?: number, diagnostics?: { rawNodesCount: number, nullNodesCount: number, rawEdgesCount: number, nullEdgeNodesCount: number } }} list - List result
 * @returns {string} Summary string
 */
function summarizeEmptyProjectsV2List(list) {
  const total = typeof list.totalCount === "number" ? list.totalCount : undefined;
  const d = list?.diagnostics;
  const diag = d ? ` nodes=${d.rawNodesCount} (null=${d.nullNodesCount}), edges=${d.rawEdgesCount} (nullNode=${d.nullEdgeNodesCount})` : "";

  if (typeof total === "number" && total > 0) {
    return `(none; totalCount=${total} but returned 0 readable project nodes${diag}. This often indicates the token can see the org/user but lacks Projects v2 access, or the org enforces SSO and the token is not authorized.)`;
  }

  return `(none${diag})`;
}

/**
 * Resolve a project by number
 * @param {{ scope: string, ownerLogin: string, projectNumber: string }} projectInfo - Project info
 * @param {number} projectNumberInt - Project number
 * @returns {Promise<{ id: string, number: number, title: string, url: string }>} Project details
 */
async function resolveProjectV2(projectInfo, projectNumberInt) {
  try {
    const query =
      projectInfo.scope === "orgs"
        ? `query($login: String!, $number: Int!) {
          organization(login: $login) {
            projectV2(number: $number) {
              id
              number
              title
              url
            }
          }
        }`
        : `query($login: String!, $number: Int!) {
          user(login: $login) {
            projectV2(number: $number) {
              id
              number
              title
              url
            }
          }
        }`;

    const direct = await github.graphql(query, {
      login: projectInfo.ownerLogin,
      number: projectNumberInt,
    });

    const project = projectInfo.scope === "orgs" ? direct?.organization?.projectV2 : direct?.user?.projectV2;

    if (project) return project;

    // If the query succeeded but returned null, fall back to list search
    core.warning(`Direct projectV2(number) query returned null; falling back to projectsV2 list search`);
  } catch (error) {
    core.warning(`Direct projectV2(number) query failed; falling back to projectsV2 list search: ${getErrorMessage(error)}`);
  }

  // Wrap fallback query in try-catch to handle transient API errors gracefully
  let list;
  try {
    list = await listAccessibleProjectsV2(projectInfo);
  } catch (fallbackError) {
    // Both direct query and fallback list query failed - this could be a transient API error
    const who = projectInfo.scope === "orgs" ? `org ${projectInfo.ownerLogin}` : `user ${projectInfo.ownerLogin}`;
    throw new Error(`Unable to resolve project #${projectNumberInt} for ${who}. Both direct projectV2 query and fallback projectsV2 list query failed. This may be a transient GitHub API error. Error: ${getErrorMessage(fallbackError)}`);
  }

  const nodes = Array.isArray(list.nodes) ? list.nodes : [];
  const found = nodes.find(p => p && typeof p.number === "number" && p.number === projectNumberInt);

  if (found) return found;

  const summary = nodes.length > 0 ? summarizeProjectsV2(nodes) : summarizeEmptyProjectsV2List(list);
  const total = typeof list.totalCount === "number" ? ` (totalCount=${list.totalCount})` : "";
  const who = projectInfo.scope === "orgs" ? `org ${projectInfo.ownerLogin}` : `user ${projectInfo.ownerLogin}`;

  throw new Error(`Project #${projectNumberInt} not found or not accessible for ${who}.${total} Accessible Projects v2: ${summary}`);
}

/**
 * Validate status enum value
 * @param {unknown} status - Status value to validate
 * @returns {string} Validated status
 */
function validateStatus(status) {
  const validStatuses = ["INACTIVE", "ON_TRACK", "AT_RISK", "OFF_TRACK", "COMPLETE"];
  const statusStr = String(status || "ON_TRACK").toUpperCase();

  if (!validStatuses.includes(statusStr)) {
    core.warning(`Invalid status "${status}", using ON_TRACK. Valid values: ${validStatuses.join(", ")}`);
    return "ON_TRACK";
  }

  return statusStr;
}

/**
 * Format date to ISO 8601 (YYYY-MM-DD)
 * @param {unknown} date - Date to format (string or Date object)
 * @returns {string} Formatted date
 */
function formatDate(date) {
  if (!date) {
    return new Date().toISOString().split("T")[0];
  }

  if (typeof date === "string") {
    // If already in YYYY-MM-DD format, return as-is
    if (/^\d{4}-\d{2}-\d{2}$/.test(date)) {
      return date;
    }
    // Otherwise parse and format
    const parsed = new Date(date);
    if (isNaN(parsed.getTime())) {
      core.warning(`Invalid date "${date}", using today`);
      return new Date().toISOString().split("T")[0];
    }
    return parsed.toISOString().split("T")[0];
  }

  if (date instanceof Date) {
    return date.toISOString().split("T")[0];
  }

  core.warning(`Invalid date type ${typeof date}, using today`);
  return new Date().toISOString().split("T")[0];
}

/**
 * Main handler factory for create_project_status_update
 * Returns a message handler function that processes individual create_project_status_update messages
 * @param {Object} config - Handler configuration
 * @param {Object} githubClient - GitHub client (Octokit instance) to use for API calls
 * @returns {Promise<Function>} Message handler function
 */
async function main(config = {}, githubClient = null) {
  const maxCount = config.max || 10;

  // Use the provided github client, or fall back to the global github object
  // @ts-ignore - global.github is set by setupGlobals() from github-script context
  const github = githubClient || global.github;

  if (!github) {
    throw new Error("GitHub client is required but not provided. Either pass a github client to main() or ensure global.github is set by github-script action.");
  }

  core.info(`Max count: ${maxCount}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Track created status updates for outputs
  const createdStatusUpdates = [];

  /**
   * Message handler function that processes a single create_project_status_update message
   * @param {Object} message - The create_project_status_update message to process
   * @param {Map<string, {repo?: string, number?: number, projectUrl?: string}>} temporaryIdMap - Unified map of temporary IDs
   * @param {Object} resolvedTemporaryIds - Plain object version of temporaryIdMap for backward compatibility
   * @returns {Promise<Object>} Result with success/error status and status update details
   */
  return async function handleCreateProjectStatusUpdate(message, temporaryIdMap, resolvedTemporaryIds = {}) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create-project-status-update: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const output = message;

    // Validate that project field is explicitly provided in the message
    // The project field is required in agent output messages and must be a full GitHub project URL
    const effectiveProjectUrl = output.project;

    if (!effectiveProjectUrl || typeof effectiveProjectUrl !== "string" || effectiveProjectUrl.trim() === "") {
      core.error('Missing required "project" field. The agent must explicitly include the project URL in the output message: {"type": "create_project_status_update", "project": "https://github.com/orgs/myorg/projects/42", "body": "..."}');
      return {
        success: false,
        error: "Missing required field: project",
      };
    }

    if (!output.body) {
      core.error("Missing required field: body (status update content)");
      return {
        success: false,
        error: "Missing required field: body",
      };
    }

    try {
      core.info(`Creating status update for project: ${effectiveProjectUrl}`);

      // Parse project URL and resolve project ID
      const projectInfo = parseProjectUrl(effectiveProjectUrl);
      const projectNumberInt = parseInt(projectInfo.projectNumber, 10);

      if (!Number.isFinite(projectNumberInt)) {
        throw new Error(`Invalid project number parsed from URL: ${projectInfo.projectNumber}`);
      }

      const project = await resolveProjectV2(projectInfo, projectNumberInt);
      const projectId = project.id;

      core.info(`✓ Resolved project #${project.number} (${projectInfo.ownerLogin}) (ID: ${projectId})`);

      // Validate and format inputs
      const status = validateStatus(output.status);
      const startDate = formatDate(output.start_date);
      const targetDate = formatDate(output.target_date);
      const body = String(output.body);

      core.info(`Creating status update: ${status} (${startDate} → ${targetDate})`);
      core.info(`Body preview: ${body.substring(0, 100)}${body.length > 100 ? "..." : ""}`);

      // Create the status update using GraphQL mutation
      const mutation = `
        mutation($projectId: ID!, $body: String!, $startDate: Date, $targetDate: Date, $status: ProjectV2StatusUpdateStatus!) {
          createProjectV2StatusUpdate(
            input: {
              projectId: $projectId,
              body: $body,
              startDate: $startDate,
              targetDate: $targetDate,
              status: $status
            }
          ) {
            statusUpdate {
              id
              body
              bodyHTML
              startDate
              targetDate
              status
              createdAt
            }
          }
        }
      `;

      const result = await github.graphql(mutation, {
        projectId,
        body,
        startDate,
        targetDate,
        status,
      });

      const statusUpdate = result.createProjectV2StatusUpdate.statusUpdate;

      core.info(`✓ Created status update: ${statusUpdate.id}`);
      core.info(`  Status: ${statusUpdate.status}`);
      core.info(`  Start: ${statusUpdate.startDate}`);
      core.info(`  Target: ${statusUpdate.targetDate}`);
      core.info(`  Created: ${statusUpdate.createdAt}`);

      // Track created status update
      createdStatusUpdates.push({
        id: statusUpdate.id,
        project_id: projectId,
        project_number: project.number,
        status: statusUpdate.status,
        start_date: statusUpdate.startDate,
        target_date: statusUpdate.targetDate,
        created_at: statusUpdate.createdAt,
      });

      // Set output for step
      core.setOutput("status-update-id", statusUpdate.id);
      core.setOutput("created-status-updates", JSON.stringify(createdStatusUpdates));

      return {
        success: true,
        status_update_id: statusUpdate.id,
        project_id: projectId,
        status: statusUpdate.status,
      };
    } catch (err) {
      // prettier-ignore
      const error = /** @type {Error & { errors?: Array<{ type?: string, message: string, path?: unknown, locations?: unknown }>, request?: unknown, data?: unknown }} */ (err);
      core.error(`Failed to create project status update: ${getErrorMessage(error)}`);
      logGraphQLError(error, "Creating project status update");

      return {
        success: false,
        error: getErrorMessage(error),
      };
    }
  };
}

module.exports = { main, HANDLER_TYPE };
