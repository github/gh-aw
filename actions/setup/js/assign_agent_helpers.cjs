// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Shared helper functions for assigning coding agents (like Copilot) to issues
 * These functions use GraphQL to properly assign bot actors that cannot be assigned via gh CLI
 *
 * NOTE: All functions use the built-in `github` global object for authentication.
 * The token must be set at the step level via the `github-token` parameter in GitHub Actions.
 * This approach is required for compatibility with actions/github-script@v8.
 */

/**
 * Map agent names to their GitHub bot login names
 * @type {Record<string, string>}
 */
const AGENT_LOGIN_NAMES = {
  copilot: "copilot-swe-agent",
};

/**
 * Check if an assignee is a known coding agent (bot)
 * @param {string} assignee - Assignee name (may include @ prefix)
 * @returns {string|null} Agent name if it's a known agent, null otherwise
 */
function getAgentName(assignee) {
  // Normalize: remove @ prefix if present
  const normalized = assignee.startsWith("@") ? assignee.slice(1) : assignee;

  // Check if it's a known agent
  if (AGENT_LOGIN_NAMES[normalized]) {
    return normalized;
  }

  return null;
}

/**
 * Return list of coding agent bot login names that are currently available as assignable actors
 * (intersection of suggestedActors and known AGENT_LOGIN_NAMES values)
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<string[]>}
 */
async function getAvailableAgentLogins(owner, repo) {
  const query = `
    query($owner: String!, $repo: String!) {
      repository(owner: $owner, name: $repo) {
        suggestedActors(first: 100, capabilities: CAN_BE_ASSIGNED) {
          nodes { ... on Bot { login __typename } }
        }
      }
    }
  `;
  try {
    const response = await github.graphql(query, { owner, repo });
    const actors = response.repository?.suggestedActors?.nodes || [];
    const knownValues = Object.values(AGENT_LOGIN_NAMES);
    const available = actors.filter(actor => actor?.login && knownValues.includes(actor.login)).map(actor => actor.login);
    return available.sort();
  } catch (e) {
    const errorMessage = e instanceof Error ? e.message : String(e);
    core.debug(`Failed to list available agent logins: ${errorMessage}`);
    return [];
  }
}

/**
 * Find an agent in repository's suggested actors using GraphQL
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} agentName - Agent name (copilot)
 * @returns {Promise<string|null>} Agent ID or null if not found
 */
async function findAgent(owner, repo, agentName) {
  const query = `
    query($owner: String!, $repo: String!) {
      repository(owner: $owner, name: $repo) {
        suggestedActors(first: 100, capabilities: CAN_BE_ASSIGNED) {
          nodes {
            ... on Bot {
              id
              login
              __typename
            }
          }
        }
      }
    }
  `;

  try {
    const response = await github.graphql(query, { owner, repo });
    const actors = response.repository.suggestedActors.nodes;

    const loginName = AGENT_LOGIN_NAMES[agentName];
    if (!loginName) {
      core.error(`Unknown agent: ${agentName}. Supported agents: ${Object.keys(AGENT_LOGIN_NAMES).join(", ")}`);
      return null;
    }

    const agent = actors.find(actor => actor.login === loginName);
    if (agent) {
      return agent.id;
    }

    const knownValues = Object.values(AGENT_LOGIN_NAMES);
    const available = actors.filter(a => a?.login && knownValues.includes(a.login)).map(a => a.login);

    core.warning(`${agentName} coding agent (${loginName}) is not available as an assignee for this repository`);
    if (available.length > 0) {
      core.info(`Available assignable coding agents: ${available.join(", ")}`);
    } else {
      core.info("No coding agents are currently assignable in this repository.");
    }
    if (agentName === "copilot") {
      core.info("Please visit https://docs.github.com/en/copilot/using-github-copilot/using-copilot-coding-agent-to-work-on-tasks/about-assigning-tasks-to-copilot");
    }
    return null;
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.error(`Failed to find ${agentName} agent: ${errorMessage}`);

    // Re-throw authentication/permission errors so they can be handled by the caller
    // This allows ignore-if-missing logic to work properly
    if (
      errorMessage.includes("Bad credentials") ||
      errorMessage.includes("Not Authenticated") ||
      errorMessage.includes("Resource not accessible") ||
      errorMessage.includes("Insufficient permissions") ||
      errorMessage.includes("requires authentication")
    ) {
      throw error;
    }

    return null;
  }
}

/**
 * Get issue details (ID and current assignees) using GraphQL
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<{issueId: string, currentAssignees: Array<{id: string, login: string}>}|null>}
 */
async function getIssueDetails(owner, repo, issueNumber) {
  const query = `
    query($owner: String!, $repo: String!, $issueNumber: Int!) {
      repository(owner: $owner, name: $repo) {
        issue(number: $issueNumber) {
          id
          assignees(first: 100) {
            nodes {
              id
              login
            }
          }
        }
      }
    }
  `;

  try {
    const response = await github.graphql(query, { owner, repo, issueNumber });
    const issue = response.repository.issue;

    if (!issue || !issue.id) {
      core.error("Could not get issue data");
      return null;
    }

    const currentAssignees = issue.assignees.nodes.map(assignee => ({
      id: assignee.id,
      login: assignee.login,
    }));

    return {
      issueId: issue.id,
      currentAssignees,
    };
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.error(`Failed to get issue details: ${errorMessage}`);
    // Re-throw the error to preserve the original error message for permission error detection
    throw error;
  }
}

/**
 * Get pull request details (ID and current assignees) using GraphQL
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} pullNumber - Pull request number
 * @returns {Promise<{pullRequestId: string, currentAssignees: Array<{id: string, login: string}>}|null>}
 */
async function getPullRequestDetails(owner, repo, pullNumber) {
  const query = `
    query($owner: String!, $repo: String!, $pullNumber: Int!) {
      repository(owner: $owner, name: $repo) {
        pullRequest(number: $pullNumber) {
          id
          assignees(first: 100) {
            nodes {
              id
              login
            }
          }
        }
      }
    }
  `;

  try {
    const response = await github.graphql(query, { owner, repo, pullNumber });
    const pullRequest = response.repository.pullRequest;

    if (!pullRequest || !pullRequest.id) {
      core.error("Could not get pull request data");
      return null;
    }

    const currentAssignees = pullRequest.assignees.nodes.map(assignee => ({
      id: assignee.id,
      login: assignee.login,
    }));

    return {
      pullRequestId: pullRequest.id,
      currentAssignees,
    };
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.error(`Failed to get pull request details: ${errorMessage}`);
    // Re-throw the error to preserve the original error message for permission error detection
    throw error;
  }
}

/**
 * Assign agent to issue or pull request using GraphQL replaceActorsForAssignable mutation
 * @param {string} assignableId - GitHub issue or pull request ID
 * @param {string} agentId - Agent ID
 * @param {Array<{id: string, login: string}>} currentAssignees - List of current assignees with id and login
 * @param {string} agentName - Agent name for error messages
 * @param {string[]|null} allowedAgents - Optional list of allowed agent names. If provided, filters out non-allowed agents from current assignees.
 * @returns {Promise<boolean>} True if successful
 */
async function assignAgentToIssue(assignableId, agentId, currentAssignees, agentName, allowedAgents = null) {
  // Filter current assignees based on allowed list (if configured)
  let filteredAssignees = currentAssignees;
  if (allowedAgents && allowedAgents.length > 0) {
    filteredAssignees = currentAssignees.filter(assignee => {
      // Check if this assignee is a known agent
      const agentName = getAgentName(assignee.login);
      if (agentName) {
        // It's an agent - only keep if in allowed list
        const isAllowed = allowedAgents.includes(agentName);
        if (!isAllowed) {
          core.info(`Filtering out agent "${assignee.login}" (not in allowed list)`);
        }
        return isAllowed;
      }
      // Not an agent - keep it (regular user assignee)
      return true;
    });
  }

  // Build actor IDs array - include new agent and preserve filtered assignees
  const actorIds = [agentId, ...filteredAssignees.map(a => a.id).filter(id => id !== agentId)];

  const mutation = `
    mutation($assignableId: ID!, $actorIds: [ID!]!) {
      replaceActorsForAssignable(input: {
        assignableId: $assignableId,
        actorIds: $actorIds
      }) {
        __typename
      }
    }
  `;

  try {
    core.info("Using built-in github object for mutation");

    core.debug(`GraphQL mutation with variables: assignableId=${assignableId}, actorIds=${JSON.stringify(actorIds)}`);
    const response = await github.graphql(mutation, {
      assignableId: assignableId,
      actorIds,
      headers: {
        "GraphQL-Features": "issues_copilot_assignment_api_support",
      },
    });

    if (response?.replaceActorsForAssignable?.__typename) {
      return true;
    }
    core.error("Unexpected response from GitHub API");
    return false;
  } catch (error) {
    const errorMessage = getErrorMessage(error);

    // Check for 502 Bad Gateway errors - these often occur but the assignment still succeeds
    // prettier-ignore
    const err = /** @type {any} */ (error);
    const is502Error = err?.response?.status === 502 || errorMessage.includes("502 Bad Gateway");

    if (is502Error) {
      core.warning(`Received 502 error from cloud gateway during agent assignment, but assignment may have succeeded`);
      core.info(`502 error details logged for troubleshooting`);

      // Log the 502 error details without failing
      try {
        if (error && typeof error === "object") {
          const details = {
            ...(err.errors && { errors: err.errors }),
            ...(err.response && { response: err.response }),
            ...(err.data && { data: err.data }),
          };
          const serialized = JSON.stringify(details, null, 2);
          if (serialized !== "{}") {
            core.info("502 error details (for troubleshooting):");
            serialized
              .split("\n")
              .filter(line => line.trim())
              .forEach(line => core.info(line));
          }
        }
      } catch (loggingErr) {
        const loggingErrMsg = loggingErr instanceof Error ? loggingErr.message : String(loggingErr);
        core.debug(`Failed to serialize 502 error details: ${loggingErrMsg}`);
      }

      // Treat 502 as success since assignment typically succeeds despite the error
      core.info(`Treating 502 error as success - agent assignment likely completed`);
      return true;
    }

    // Debug: surface the raw GraphQL error structure for troubleshooting fine-grained permission issues
    try {
      core.debug(`Raw GraphQL error message: ${errorMessage}`);
      if (error && typeof error === "object") {
        // Common GraphQL error shapes: error.errors (array), error.data, error.response
        const details = {
          ...(err.errors && { errors: err.errors }),
          ...(err.response && { response: err.response }),
          ...(err.data && { data: err.data }),
        };
        // If GitHub returns an array of errors with 'type'/'message'
        if (Array.isArray(err.errors)) {
          details.compactMessages = err.errors.map(e => e.message).filter(Boolean);
        }
        const serialized = JSON.stringify(details, null, 2);
        if (serialized !== "{}") {
          core.debug(`Raw GraphQL error details: ${serialized}`);
          // Also emit non-debug version so users without ACTIONS_STEP_DEBUG can see it
          core.error("Raw GraphQL error details (for troubleshooting):");
          serialized
            .split("\n")
            .filter(line => line.trim())
            .forEach(line => core.error(line));
        }
      }
    } catch (loggingErr) {
      // Never fail assignment because of debug logging
      const loggingErrMsg = loggingErr instanceof Error ? loggingErr.message : String(loggingErr);
      core.debug(`Failed to serialize GraphQL error details: ${loggingErrMsg}`);
    }

    // Check for permission-related errors
    if (errorMessage.includes("Resource not accessible by personal access token") || errorMessage.includes("Resource not accessible by integration") || errorMessage.includes("Insufficient permissions to assign")) {
      // Attempt fallback mutation addAssigneesToAssignable when replaceActorsForAssignable is forbidden
      core.info("Primary mutation replaceActorsForAssignable forbidden. Attempting fallback addAssigneesToAssignable...");
      try {
        const fallbackMutation = `
          mutation($assignableId: ID!, $assigneeIds: [ID!]!) {
            addAssigneesToAssignable(input: {
              assignableId: $assignableId,
              assigneeIds: $assigneeIds
            }) {
              clientMutationId
            }
          }
        `;
        core.info("Using built-in github object for fallback mutation");
        core.debug(`Fallback GraphQL mutation with variables: assignableId=${assignableId}, assigneeIds=[${agentId}]`);
        const fallbackResp = await github.graphql(fallbackMutation, {
          assignableId: assignableId,
          assigneeIds: [agentId],
          headers: {
            "GraphQL-Features": "issues_copilot_assignment_api_support",
          },
        });
        if (fallbackResp?.addAssigneesToAssignable) {
          core.info(`Fallback succeeded: agent '${agentName}' added via addAssigneesToAssignable.`);
          return true;
        }
        core.warning("Fallback mutation returned unexpected response; proceeding with permission guidance.");
      } catch (fallbackError) {
        const fallbackErrMsg = fallbackError instanceof Error ? fallbackError.message : String(fallbackError);
        core.error(`Fallback addAssigneesToAssignable failed: ${fallbackErrMsg}`);
      }
      logPermissionError(agentName);
    } else {
      core.error(`Failed to assign ${agentName}: ${errorMessage}`);
    }
    return false;
  }
}

/**
 * Log detailed permission error guidance
 * @param {string} agentName - Agent name for error messages
 */
function logPermissionError(agentName) {
  core.error(`Failed to assign ${agentName}: Insufficient permissions`);
  core.error("");
  core.error("Assigning Copilot agents requires:");
  core.error("  1. All four workflow permissions:");
  core.error("     - actions: write");
  core.error("     - contents: write");
  core.error("     - issues: write");
  core.error("     - pull-requests: write");
  core.error("");
  core.error("  2. A classic PAT with 'repo' scope OR fine-grained PAT with explicit Write permissions above:");
  core.error("     (Fine-grained PATs must grant repository access + write for Issues, Pull requests, Contents, Actions)");
  core.error("");
  core.error("  3. Repository settings:");
  core.error("     - Actions must have write permissions");
  core.error("     - Go to: Settings > Actions > General > Workflow permissions");
  core.error("     - Select: 'Read and write permissions'");
  core.error("");
  core.error("  4. Organization/Enterprise settings:");
  core.error("     - Check if your org restricts bot assignments");
  core.error("     - Verify Copilot is enabled for your repository");
  core.error("");
  core.info("For more information, see: https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr");
}

/**
 * Generate permission error summary content for step summary
 * @returns {string} Markdown content for permission error guidance
 */
function generatePermissionErrorSummary() {
  return `
### ⚠️ Permission Requirements

Assigning Copilot agents requires **ALL** of these permissions:

\`\`\`yaml
permissions:
  actions: write
  contents: write
  issues: write
  pull-requests: write
\`\`\`

**Token capability note:**
- Current token (PAT or GITHUB_TOKEN) lacks assignee mutation capability for this repository.
- Both \`replaceActorsForAssignable\` and fallback \`addAssigneesToAssignable\` returned FORBIDDEN/Resource not accessible.
- This typically means bot/user assignment requires an elevated OAuth or GitHub App installation token.

**Recommended remediation paths:**
1. Create & install a GitHub App with: Issues/Pull requests/Contents/Actions (write) → use installation token in job.
2. Manual assignment: add the agent through the UI until broader token support is available.
3. Open a support ticket referencing failing mutation \`replaceActorsForAssignable\` and repository slug.

**Why this failed:** Fine-grained and classic PATs can update issue title (verified) but not modify assignees in this environment.

📖 Reference: https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr (general agent docs)
`;
}

/**
 * Assign an agent to an issue using GraphQL
 * This is the main entry point for assigning agents from other scripts
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {string} agentName - Agent name (e.g., "copilot")
 * @returns {Promise<{success: boolean, error?: string}>}
 */
async function assignAgentToIssueByName(owner, repo, issueNumber, agentName) {
  // Check if agent is supported
  if (!AGENT_LOGIN_NAMES[agentName]) {
    const error = `Agent "${agentName}" is not supported. Supported agents: ${Object.keys(AGENT_LOGIN_NAMES).join(", ")}`;
    core.warning(error);
    return { success: false, error };
  }

  try {
    // Find agent using the github object authenticated via step-level github-token
    core.info(`Looking for ${agentName} coding agent...`);
    const agentId = await findAgent(owner, repo, agentName);
    if (!agentId) {
      const error = `${agentName} coding agent is not available for this repository`;
      // Enrich with available agent logins
      const available = await getAvailableAgentLogins(owner, repo);
      const enrichedError = available.length > 0 ? `${error} (available agents: ${available.join(", ")})` : error;
      return { success: false, error: enrichedError };
    }
    core.info(`Found ${agentName} coding agent (ID: ${agentId})`);

    // Get issue details (ID and current assignees) via GraphQL
    core.info("Getting issue details...");
    const issueDetails = await getIssueDetails(owner, repo, issueNumber);
    if (!issueDetails) {
      return { success: false, error: "Failed to get issue details" };
    }

    core.info(`Issue ID: ${issueDetails.issueId}`);

    // Check if agent is already assigned
    if (issueDetails.currentAssignees.some(a => a.id === agentId)) {
      core.info(`${agentName} is already assigned to issue #${issueNumber}`);
      return { success: true };
    }

    // Assign agent using GraphQL mutation (no allowed list filtering in this helper)
    core.info(`Assigning ${agentName} coding agent to issue #${issueNumber}...`);
    const success = await assignAgentToIssue(issueDetails.issueId, agentId, issueDetails.currentAssignees, agentName, null);

    if (!success) {
      return { success: false, error: `Failed to assign ${agentName} via GraphQL` };
    }

    core.info(`Successfully assigned ${agentName} coding agent to issue #${issueNumber}`);
    return { success: true };
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    return { success: false, error: errorMessage };
  }
}

module.exports = {
  AGENT_LOGIN_NAMES,
  getAgentName,
  getAvailableAgentLogins,
  findAgent,
  getIssueDetails,
  getPullRequestDetails,
  assignAgentToIssue,
  logPermissionError,
  generatePermissionErrorSummary,
  assignAgentToIssueByName,
};
