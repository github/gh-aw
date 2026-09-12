// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-004 — body fields are read-only API context, never written back

const { getErrorMessage } = require("./error_helpers.cjs");
const { getPromptPath, renderTemplateFromFile } = require("./messages_core.cjs");

/**
 * Shared helper functions for assigning coding agents (like Copilot) to issues.
 * These functions use GitHub REST APIs.
 */

/**
 * Map agent names to their GitHub bot login aliases.
 * Keep the most common/current alias first so logs have a stable primary name.
 * @type {Record<string, string[]>}
 */
const AGENT_LOGIN_NAMES = {
  // Prefer [bot] aliases first so assignability checks and assignment requests
  // use the canonical bot login when both plain and [bot] aliases exist.
  copilot: ["copilot-swe-agent[bot]", "github-copilot-enterprise[bot]", "github-copilot[bot]", "copilot-swe-agent", "github-copilot-enterprise", "github-copilot"],
};

/**
 * Normalize a GitHub login for internal matching.
 * @param {string} login
 * @returns {string}
 */
function normalizeLogin(login) {
  return login.startsWith("@") ? login.slice(1) : login;
}

/**
 * Reverse lookup of assignee aliases to canonical agent names.
 * @type {Record<string, string>}
 */
const AGENT_NAME_BY_LOGIN = Object.fromEntries(Object.entries(AGENT_LOGIN_NAMES).flatMap(([agentName, logins]) => logins.map(login => [normalizeLogin(login), agentName])));

const REASONING_EFFORT_VALUES = new Set(["none", "minimal", "low", "medium", "high", "xhigh"]);

function resolveReasoningEffort(reasoningEffort) {
  if (reasoningEffort == null) return null;

  const normalizedEffort = typeof reasoningEffort === "string" ? reasoningEffort.trim().toLowerCase() : null;
  if (normalizedEffort == null || !REASONING_EFFORT_VALUES.has(normalizedEffort)) {
    core.warning(`Ignoring reasoning-effort: expected one of ${[...REASONING_EFFORT_VALUES].join(", ")}.`);
    return null;
  }
  return normalizedEffort;
}

/**
 * GitHub can surface bots either via type="Bot" or a [bot] login suffix.
 * Check both because assignee responses are not always consistent across endpoints.
 * @param {{login?: string, type?: string}|null|undefined} assignee
 * @returns {boolean}
 */
function isBotAssignee(assignee) {
  return assignee?.type === "Bot" || Boolean(assignee?.login?.endsWith("[bot]"));
}

/**
 * Return the known GitHub login aliases for an agent.
 * @param {string} agentName
 * @returns {string[]}
 */
function getAgentLogins(agentName) {
  const logins = AGENT_LOGIN_NAMES[agentName];
  if (!logins) return [];
  return logins;
}

/**
 * Check if an assignee is a known coding agent (bot)
 * @param {string} assignee - Assignee name (may include @ prefix)
 * @returns {string|null} Agent name if it's a known agent, null otherwise
 */
function getAgentName(assignee) {
  // Normalize: remove @ prefix if present
  const normalized = normalizeLogin(assignee);

  // Check if it's a known agent
  if (AGENT_LOGIN_NAMES[normalized]) {
    return normalized;
  }
  return AGENT_NAME_BY_LOGIN[normalized] || null;
}

/**
 * Parse and validate an issue/PR number for assignee REST endpoints.
 * @param {number|string|null|undefined} issueNumber
 * @param {string} contextLabel
 * @returns {number}
 */
function parseIssueNumber(issueNumber, contextLabel) {
  const parsedIssueNumber = Number(issueNumber);
  if (!Number.isInteger(parsedIssueNumber) || parsedIssueNumber <= 0) {
    throw new Error(`Invalid issue number for ${contextLabel}: received '${String(issueNumber)}', expected a positive integer`);
  }
  return parsedIssueNumber;
}

/**
 * Return list of coding agent bot login names that are currently available as assignable actors
 * in this repository, preferring issue-scoped checks when issue/PR context is available
 * and falling back to repository-scoped checks.
 * @param {string} owner
 * @param {string} repo
 * @param {number|string|null} [issueNumber]
 * @param {Object} [githubClient] - Authenticated GitHub client (defaults to global github)
 * @returns {Promise<string[]>}
 */
async function getAvailableAgentLogins(owner, repo, issueNumber = null, githubClient = github) {
  // Deduplicate defensively so future alias additions across agents do not duplicate REST lookups.
  const knownValues = [...new Set(Object.values(AGENT_LOGIN_NAMES).flat())];
  const available = [];
  for (const login of knownValues) {
    try {
      await validateAssigneeAlias(owner, repo, login, issueNumber, githubClient);
      available.push(login);
    } catch (e) {
      const status = e && typeof e === "object" && "status" in e ? e.status : undefined;
      if (status !== 404) {
        core.info(`Failed to check assignability for ${login}: ${getErrorMessage(e)}`);
      }
    }
  }
  return available.sort();
}

/**
 * Validate whether an assignee alias can be assigned in the repository context.
 * Prefer issue-level assignability checks when issue/PR number is available because
 * some agent bots are not surfaced by repository-scoped checks.
 * @param {string} owner
 * @param {string} repo
 * @param {string} assignee
 * @param {number|string|null} issueNumber
 * @param {Object} githubClient
 */
async function validateAssigneeAlias(owner, repo, assignee, issueNumber, githubClient) {
  const parsedIssueNumber = parseIssueNumber(issueNumber, "assignee check");
  if (typeof githubClient?.request !== "function") {
    throw new Error("GitHub client does not support request() method required for REST issue assignee checks");
  }
  core.info(`Checking assignee alias ${assignee} via issue-scoped endpoint for ${owner}/${repo}#${parsedIssueNumber}`);
  await githubClient.request("GET /repos/{owner}/{repo}/issues/{issue_number}/assignees/{assignee}", {
    owner,
    repo,
    issue_number: parsedIssueNumber,
    assignee,
  });
  core.info(`Assignee alias ${assignee} is assignable via issue-scoped check`);
}

/**
 * Return assignable bot logins from the repository assignee list.
 * @param {string} owner
 * @param {string} repo
 * @param {Object} [githubClient]
 * @returns {Promise<string[]>}
 */
async function getAssignableBots(owner, repo, githubClient = github) {
  try {
    const assignees = [];
    let page = 1;
    let pageData = [];
    const MAX_PAGES = 5; // Limit to 5 pages (500 assignees) to bound API calls on large repositories

    do {
      const response = await githubClient.rest.issues.listAssignees({
        owner,
        repo,
        per_page: 100,
        page,
      });
      pageData = Array.isArray(response.data) ? response.data : [];
      assignees.push(...pageData);
      page++;
    } while (pageData.length === 100 && page <= MAX_PAGES);

    return [
      ...new Set(
        assignees
          .filter(isBotAssignee)
          .map(assignee => assignee.login)
          .filter(Boolean)
      ),
    ].sort();
  } catch (error) {
    core.debug(`Failed to list assignable bots for ${owner}/${repo}: ${getErrorMessage(error)}`);
    return [];
  }
}

/**
 * Find an agent that can be assigned in the repository using REST
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} agentName - Agent name (copilot)
 * @param {number|string|null} [issueNumber] - Optional issue/PR number for issue-scoped assignability check
 * @param {Object} [githubClient] - Authenticated GitHub client (defaults to global github)
 * @returns {Promise<string|null>} Agent login or null if not found
 */
async function findAgent(owner, repo, agentName, issueNumber = null, githubClient = github) {
  const loginNames = getAgentLogins(agentName);
  if (loginNames.length === 0) {
    core.error(`Unknown agent: ${agentName}. Supported agents: ${Object.keys(AGENT_LOGIN_NAMES).join(", ")}`);
    return null;
  }

  core.info(`Trying ${loginNames.length} ${agentName} assignee aliases: ${loginNames.join(", ")}`);

  const aliasFailures = [];
  for (const loginName of loginNames) {
    try {
      core.info(`Checking assignee alias: ${loginName}`);
      await validateAssigneeAlias(owner, repo, loginName, issueNumber, githubClient);
    } catch (checkError) {
      const errorMessage = getErrorMessage(checkError);
      const status = checkError?.status;
      const statusLabel = status ? ` (${status})` : "";
      aliasFailures.push(`${loginName}${statusLabel}: ${errorMessage}`);
      if (
        errorMessage.includes("Bad credentials") ||
        errorMessage.includes("Not Authenticated") ||
        errorMessage.includes("Resource not accessible") ||
        errorMessage.includes("Insufficient permissions") ||
        errorMessage.includes("requires authentication")
      ) {
        core.error(`Failed to check assignee alias ${loginName} for ${agentName}: ${errorMessage}`);
        throw checkError;
      }
      core.info(`Assignee alias ${loginName} was not assignable: ${errorMessage}`);
      continue;
    }
    core.info(`Resolved ${agentName} agent via assignee alias ${loginName}`);
    return loginName;
  }

  const bots = await getAssignableBots(owner, repo, githubClient);
  core.warning(`${agentName} coding agent aliases are not available as assignees for this repository`);
  core.info(`Assignee aliases tried: ${loginNames.join(", ")}`);
  if (aliasFailures.length > 0) {
    core.info(`Alias lookup results: ${aliasFailures.join(" | ")}`);
  }
  if (bots.length > 0) {
    core.info(`Assignable bots in this repository: ${bots.join(", ")}`);
  } else {
    core.info("No assignable bots found in this repository.");
  }
  if (agentName === "copilot") {
    core.info("Please visit https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/use-cloud-agent-via-the-api#using-the-issues-api");
  }
  return null;
}

/**
 * Get issue details (context and current assignees) using REST
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {Object} [githubClient] - Authenticated GitHub client (defaults to global github)
 * @returns {Promise<{issueId: string, currentAssignees: Array<{id: string, login: string}>, htmlUrl: string, title: string, body: string, taskContext: {owner: string, repo: string, type: "issue", number: number}}|null>}
 */
async function getIssueDetails(owner, repo, issueNumber, githubClient = github) {
  try {
    const { data: issue } = await githubClient.rest.issues.get({ owner, repo, issue_number: issueNumber });
    if (!issue || !issue.number) {
      core.error("Could not get issue data");
      return null;
    }
    // GitHub's issues API returns pull requests too; reject them here so callers
    // never accidentally treat a PR as an assignable issue.
    if (issue.pull_request) {
      throw Object.assign(new Error(`#${issueNumber} is a pull request, not an issue — use pull_number instead of issue_number to assign to a pull request`), { isPullRequest: true });
    }
    const currentAssignees = (issue.assignees || []).map(assignee => ({
      id: String(assignee.id),
      login: assignee.login,
    }));

    return {
      issueId: String(issue.id),
      currentAssignees,
      htmlUrl: issue.html_url || "",
      title: issue.title || "",
      body: issue.body || "",
      taskContext: { owner, repo, type: "issue", number: issue.number },
    };
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    if (!(/** @type {any} */ error.isPullRequest)) {
      core.error(`Failed to get issue details: ${errorMessage}`);
    }
    throw error;
  }
}

/**
 * Get pull request details (context and current assignees) using REST
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} pullNumber - Pull request number
 * @param {Object} [githubClient] - Authenticated GitHub client (defaults to global github)
 * @returns {Promise<{pullRequestId: string, currentAssignees: Array<{id: string, login: string}>, htmlUrl: string, title: string, body: string, taskContext: {owner: string, repo: string, type: "pull", number: number}}|null>}
 */
async function getPullRequestDetails(owner, repo, pullNumber, githubClient = github) {
  try {
    const { data: pullRequest } = await githubClient.rest.pulls.get({ owner, repo, pull_number: pullNumber });
    if (!pullRequest || !pullRequest.number) {
      core.error("Could not get pull request data");
      return null;
    }
    const currentAssignees = (pullRequest.assignees || []).map(assignee => ({
      id: String(assignee.id),
      login: assignee.login,
    }));

    return {
      pullRequestId: String(pullRequest.id),
      currentAssignees,
      htmlUrl: pullRequest.html_url || "",
      title: pullRequest.title || "",
      body: pullRequest.body || "",
      taskContext: { owner, repo, type: "pull", number: pullRequest.number },
    };
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.error(`Failed to get pull request details: ${errorMessage}`);
    throw error;
  }
}

/**
 * Start an agent task for issue or pull request context using REST
 * @param {string} assignableId - Synthetic target ID in format owner/repo#issue:N or owner/repo#pull:N
 * @param {string} agentLogin - Agent login name
 * @param {Array<{id: string, login: string}>} currentAssignees - List of current assignees with id and login
 * @param {string} agentName - Agent name for error messages
 * @param {string[]|null} allowedAgents - Optional list of allowed agent names. If provided, filters out non-allowed agents from current assignees.
 * @param {string|null} model - Optional AI model to use (e.g., "claude-opus-4.6", "auto")
 * @param {string|null} customAgent - Optional custom agent ID for custom agents
 * @param {string|null} customInstructions - Optional custom instructions for the agent
 * @param {string|null} baseBranch - Optional base branch for the PR (REST base_ref field)
 * @param {Object} [githubClient] - Authenticated GitHub client (defaults to global github)
 * @param {{owner: string, repo: string, type: "issue"|"pull", number: number}|null} [taskContext] - Source issue/PR context for REST path
 * @param {string|null} [pullRequestRepoSlug] - Optional pull request repository slug (owner/repo) for REST path
 * @param {{rationale?: string, confidence?: "LOW"|"MEDIUM"|"HIGH", suggest?: boolean}} [intentMetadata] - Optional issue-intent metadata
 * @param {boolean} [useIssueIntent] - Whether to include issue-intent metadata/headers
 * @param {string|null} [reasoningEffort] - Optional reasoning effort
 * @returns {Promise<boolean>} True if successful
 */
async function assignAgentToIssue(
  assignableId,
  agentLogin,
  currentAssignees,
  agentName,
  allowedAgents = null,
  model = null,
  customAgent = null,
  customInstructions = null,
  baseBranch = null,
  githubClient = github,
  taskContext = null,
  pullRequestRepoSlug = null,
  intentMetadata = {},
  useIssueIntent = true,
  reasoningEffort = null
) {
  // SECURITY: pullRequestRepoSlug specifies a cross-repo target repository slug.
  // Callers MUST validate the corresponding repository slug against allowedRepos using
  // validateTargetRepo (from repo_helpers.cjs) before invoking this function.
  // Filter current assignees based on allowed list (if configured)
  let filteredAssignees = currentAssignees;
  if (allowedAgents && allowedAgents.length > 0) {
    filteredAssignees = currentAssignees.filter(assignee => {
      // Check if this assignee is a known agent
      const assigneeAgentName = getAgentName(assignee.login);
      if (assigneeAgentName) {
        // It's an agent - only keep if in allowed list
        const isAllowed = allowedAgents.includes(assigneeAgentName);
        if (!isAllowed) {
          core.info(`Filtering out agent "${assignee.login}" (not in allowed list)`);
        }
        return isAllowed;
      }
      // Not an agent - keep it (regular user assignee)
      return true;
    });
  }

  if (!githubClient?.request) {
    core.error(`GitHub client does not support REST requests; cannot create agent task`);
    return false;
  }

  if (!taskContext) {
    core.error(`Invalid assignment context: ${assignableId}`);
    return false;
  }
  const targetOwner = taskContext.owner;
  const targetRepo = taskContext.repo;
  let issueNumber;
  try {
    issueNumber = parseIssueNumber(taskContext.number, "assignment");
  } catch (e) {
    core.error(getErrorMessage(e));
    return false;
  }

  try {
    core.info(`Assigning via issues assignees REST API with login: ${agentLogin}`);
    const assignee = useIssueIntent && intentMetadata && Object.keys(intentMetadata).length > 0 ? { login: agentLogin, ...intentMetadata } : agentLogin;
    const assignParams = {
      owner: targetOwner,
      repo: targetRepo,
      issue_number: issueNumber,
      assignees: [assignee],
    };
    const agentAssignment = {};
    if (pullRequestRepoSlug != null) agentAssignment.target_repo = pullRequestRepoSlug;
    if (baseBranch != null) agentAssignment.base_branch = baseBranch;
    if (customInstructions != null) agentAssignment.custom_instructions = customInstructions;
    if (customAgent != null) agentAssignment.custom_agent = customAgent;
    if (model != null) agentAssignment.model = model;
    const validReasoningEffort = resolveReasoningEffort(reasoningEffort);
    if (validReasoningEffort != null) agentAssignment.reasoning_effort = validReasoningEffort;
    if (Object.keys(agentAssignment).length > 0) assignParams.agent_assignment = agentAssignment;
    await githubClient.request("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", assignParams);
    return true;
  } catch (error) {
    const errorMessage = getErrorMessage(error);

    if (
      errorMessage.includes("Bad credentials") ||
      errorMessage.includes("Not Authenticated") ||
      errorMessage.includes("Resource not accessible") ||
      errorMessage.includes("Insufficient permissions") ||
      errorMessage.includes("requires authentication")
    ) {
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
  const errorTemplatePath = getPromptPath("copilot_assignment_permission_error.md");
  const referencesTemplatePath = getPromptPath("copilot_assignment_permission_references.md");
  const renderedError = renderTemplateFromFile(errorTemplatePath, { agent_name: agentName }).trimEnd();
  const renderedReferences = renderTemplateFromFile(referencesTemplatePath, {}).trimEnd();

  for (const line of renderedError.split("\n")) {
    core.error(line);
  }
  for (const line of renderedReferences.split("\n")) {
    core.info(line);
  }
}

/**
 * Generate permission error summary content for step summary
 * @returns {string} Markdown content for permission error guidance
 */
function generatePermissionErrorSummary() {
  const templatePath = getPromptPath("copilot_assignment_permission_requirements.md");
  return renderTemplateFromFile(templatePath, {});
}

/**
 * Assign an agent to an issue by starting an agent task using REST
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
    const agentId = await findAgent(owner, repo, agentName, issueNumber);
    if (!agentId) {
      return { success: false, error: `${agentName} coding agent is not available for this repository` };
    }
    core.info(`Found ${agentName} coding agent (login: ${agentId})`);

    // Get issue details and current assignees via REST
    core.info("Getting issue details...");
    const issueDetails = await getIssueDetails(owner, repo, issueNumber);
    if (!issueDetails) {
      return { success: false, error: "Failed to get issue details" };
    }

    core.info(`Issue context: ${issueDetails.issueId}`);

    // Check if agent is already assigned
    const knownLogins = getAgentLogins(agentName);
    if (issueDetails.currentAssignees.some(a => a.login === agentId || knownLogins.includes(a.login))) {
      core.info(`${agentName} is already assigned to issue #${issueNumber}`);
      return { success: true };
    }

    // Assign agent by starting a REST task (no allowed list filtering in this helper)
    core.info(`Assigning ${agentName} coding agent to issue #${issueNumber}...`);
    const success = await assignAgentToIssue(issueDetails.issueId, agentId, issueDetails.currentAssignees, agentName, null, null, null, null, null, github, issueDetails.taskContext);

    if (!success) {
      return { success: false, error: `Failed to assign ${agentName} via REST` };
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
  getAgentLogins,
  getAvailableAgentLogins,
  getAssignableBots,
  findAgent,
  getIssueDetails,
  getPullRequestDetails,
  assignAgentToIssue,
  resolveReasoningEffort,
  logPermissionError,
  generatePermissionErrorSummary,
  assignAgentToIssueByName,
};
