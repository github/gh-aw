// @ts-check
// @safe-outputs-exempt SEC-004
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { getBaseBranch } = require("./get_base_branch.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { generateStagedPreview } = require("./staged_preview.cjs");
const { appendConfiguredBodyFooter } = require("./body_footer.cjs");

/**
 * Create a dedicated GitHub client for create-agent-session operations.
 *
 * Token precedence:
 *   1. config["github-token"] — per-handler PAT configured in the workflow frontmatter
 *   2. GH_AW_AGENT_SESSION_TOKEN — agent token injected by the compiler as a step env var
 *      (evaluates to: GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)
 *   3. global github — step-level token (fallback when no agent token is available)
 *
 * @param {Object} config - Handler configuration
 * @returns {Promise<Object>} Authenticated GitHub client
 */
async function createAgentSessionGitHubClient(config) {
  const token = config["github-token"] || process.env.GH_AW_AGENT_SESSION_TOKEN;
  if (!token) {
    core.debug("No dedicated agent token configured — using step-level github client for create-agent-session operations");
    return github;
  }
  core.info("Using dedicated github client for create-agent-session operations");
  return global.getOctokit(token);
}

/**
 * Handler factory for create-agent-session safe output.
 *
 * Replaces the standalone create_agent_session step. This function is called once by the
 * safe output handler manager with the handler's configuration. It returns a message
 * processor function that is invoked for each create_agent_session message in the agent output.
 *
 * @param {Object} config - Handler configuration from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG
 * @returns {Promise<Function>} Message processor function
 */
async function main(config = {}) {
  // Per-invocation state — captured in a closure (rather than module scope) so that
  // concurrent or repeated `main()` invocations in the same process never share or
  // clobber each other's results. The accessor functions below are attached to the
  // returned `handleMessage` function so the handler manager can read final output
  // values after all messages have been processed for this specific invocation.
  /** @type {Array<{id: string, url: string, success: boolean, error?: string}>} */
  const allResults = [];

  // Parse configuration
  const configuredBaseBranch = config.base ? String(config.base).trim() : null;
  const isStaged = isStagedMode(config);
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  if (configuredBaseBranch) core.info(`Configured base branch: ${configuredBaseBranch}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) core.info(`Allowed repos: ${[...allowedRepos].join(", ")}`);

  // Create a dedicated Octokit instance using the agent token
  const githubClient = await createAgentSessionGitHubClient(config);

  /**
   * Process a single create_agent_session message.
   * @param {Object} message - The agent output message
   * @returns {Promise<{success: boolean, id?: string, url?: string, error?: string, skipped?: boolean}>}
   */
  const handleMessage = async function (message) {
    if (!message.body || message.body.trim() === "") {
      core.warning("Agent task description is empty, skipping");
      allResults.push({ id: "", url: "", success: false, error: "Empty task description" });
      return { success: false, error: "Empty task description" };
    }
    const taskDescription = appendConfiguredBodyFooter(message.body, config.body_footer);

    // Resolve and validate target repository for this message
    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "agent session");
    if (!repoResult.success) {
      const errorMsg = `E004: ${repoResult.error}`;
      core.error(errorMsg);
      allResults.push({ id: "", url: "", success: false, error: repoResult.error });
      return { success: false, error: repoResult.error };
    }
    const { repo: effectiveRepo, repoParts } = repoResult;

    // Resolve base branch: use custom config if set, otherwise resolve dynamically.
    // Dynamic resolution is needed for issue_comment events on PRs where the base branch
    // is not available in GitHub Actions expressions and requires an API call.
    const baseBranch = configuredBaseBranch || (await getBaseBranch(repoParts));

    if (isStaged) {
      await generateStagedPreview({
        title: "Create Agent Session",
        description: "The following agent sessions would be created if staged mode was disabled:",
        items: [message],
        renderItem: item => {
          const parts = [];
          parts.push(`**Description:**\n${taskDescription}`);
          parts.push(`**Base Branch:** ${baseBranch}`);
          parts.push(`**Target Repository:** ${effectiveRepo}`);
          return parts.join("\n\n") + "\n\n";
        },
      });
      return { success: true, skipped: true };
    }

    try {
      core.info(`Task ${allResults.length + 1}: Creating agent session in ${effectiveRepo} on branch ${baseBranch}`);

      // Call the GitHub REST API to start a task
      // Reference: https://docs.github.com/en/rest/agent-tasks/agent-tasks?apiVersion=2026-03-10#start-a-task
      const response = await githubClient.request("POST /agents/repos/{owner}/{repo}/tasks", {
        owner: repoParts.owner,
        repo: repoParts.repo,
        prompt: taskDescription,
        base_ref: baseBranch,
        headers: { "X-GitHub-Api-Version": "2026-03-10" },
      });

      const task = response.data;
      const taskId = task.id || "";
      const taskUrl = task.html_url || task.url || "";

      core.info(`✅ Successfully created agent session ${taskId}`);
      allResults.push({ id: taskId, url: taskUrl, success: true });
      return { success: true, id: taskId, url: taskUrl };
    } catch (error) {
      const errorMessage = getErrorMessage(error);

      // Check for authentication/permission errors
      if (errorMessage.includes("authentication") || errorMessage.includes("permission") || errorMessage.includes("forbidden") || errorMessage.includes("401") || errorMessage.includes("403")) {
        core.error(`Failed to create agent session due to authentication/permission error.`);
        core.error(`The default GITHUB_TOKEN may not have permission to create agent sessions.`);
        core.error(`Configure a Personal Access Token (PAT) using the handler's github-token setting or GH_AW_AGENT_SESSION_TOKEN.`);
        core.error(`See documentation: https://github.github.com/gh-aw/reference/safe-outputs/#agent-task-creation-create-agent-session`);
      } else {
        core.error(`Error creating agent session: ${errorMessage}`);
      }
      allResults.push({ id: "", url: "", success: false, error: errorMessage });
      return { success: false, error: errorMessage };
    }
  };

  /**
   * Returns the session_number output: the ID of the first successfully created session.
   * @returns {string}
   */
  handleMessage.getSessionNumber = () => {
    const first = allResults.find(r => r.success && r.id);
    return first ? first.id : "";
  };

  /**
   * Returns the session_url output: the URL of the first successfully created session.
   * @returns {string}
   */
  handleMessage.getSessionUrl = () => {
    const first = allResults.find(r => r.success && r.url);
    return first ? first.url : "";
  };

  /**
   * Writes a step summary for agent session creation results.
   * Called by the handler manager after all messages have been processed.
   * @returns {Promise<void>}
   */
  handleMessage.writeSummary = async () => {
    const successResults = allResults.filter(r => r.success);
    const failedResults = allResults.filter(r => !r.success);

    if (allResults.length === 0) return;

    let summaryContent = "## Agent Sessions\n\n";

    if (successResults.length > 0) {
      summaryContent += `✅ Successfully created ${successResults.length} agent session(s):\n\n`;
      summaryContent += successResults
        .map((r, i) => {
          if (r.url && r.id) {
            return `- [${r.id}](${r.url})`;
          } else if (r.url) {
            return `- [Session ${i + 1}](${r.url})`;
          }
          return `- Session ${i + 1}`;
        })
        .join("\n");
      summaryContent += "\n\n";
    }

    if (failedResults.length > 0) {
      summaryContent += `❌ Failed to create ${failedResults.length} agent session(s):\n\n`;
      summaryContent += failedResults.map(r => `- ${r.error || "Unknown error"}`).join("\n");
      summaryContent += "\n\n";
    }

    try {
      await core.summary.addRaw(summaryContent).write();
    } catch (error) {
      core.warning(`Failed to write agent session summary: ${getErrorMessage(error)}`);
    }
  };

  return handleMessage;
}

module.exports = { main };
