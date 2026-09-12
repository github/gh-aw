// @ts-check
/// <reference types="@actions/github-script" />

const { AGENT_LOGIN_NAMES, getAgentLogins, getAvailableAgentLogins, findAgent, getIssueDetails, getPullRequestDetails, assignAgentToIssue, generatePermissionErrorSummary, resolveReasoningEffort } = require("./assign_agent_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTarget, isStagedMode } = require("./safe_output_helpers.cjs");
const { generateStagedPreview } = require("./staged_preview.cjs");
const { isTemporaryId, normalizeTemporaryId, resolveRepoIssueTarget } = require("./temporary_id.cjs");
const { sleep } = require("./error_recovery.cjs");
const { parseAllowedRepos, validateRepo, resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { resolvePullRequestRepo } = require("./pr_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { normalizeIssueIntentMetadata } = require("./issue_intents.cjs");

/**
 * Create a dedicated GitHub client for assign-to-agent operations.
 *
 * Token precedence:
 *   1. config["github-token"] — per-handler PAT configured in the workflow frontmatter
 *   2. GH_AW_ASSIGN_TO_AGENT_TOKEN — agent token injected by the compiler as a step env var
 *      (evaluates to: GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)
 *   3. global github — step-level token (fallback when no agent token is available)
 *
 * @param {Object} config - Handler configuration
 * @returns {Promise<Object>} Authenticated GitHub client
 */
async function createAssignToAgentGitHubClient(config) {
  const token = config["github-token"] || process.env.GH_AW_ASSIGN_TO_AGENT_TOKEN;
  if (!token) {
    core.debug("No dedicated agent token configured — using step-level github client for assign-to-agent operations");
    return github;
  }
  core.info("Using dedicated github client for assign-to-agent operations");
  return global.getOctokit(token);
}

/**
 * @param {Array<{issue_number: number|null, pull_number: number|null, agent: string, owner: string|null, repo: string|null, pull_request_repo?: string|null, success: boolean, skipped?: boolean, error?: string}>} allResults
 * @returns {string}
 */
function formatAssignedOutput(allResults) {
  return allResults
    .filter(r => r.success && !r.skipped)
    .map(r => {
      const number = r.issue_number || r.pull_number;
      const prefix = r.issue_number ? "issue" : "pr";
      return `${prefix}:${number}:${r.agent}`;
    })
    .join("\n");
}

/**
 * @param {Array<{issue_number: number|null, pull_number: number|null, agent: string, owner: string|null, repo: string|null, pull_request_repo?: string|null, success: boolean, skipped?: boolean, error?: string}>} allResults
 * @returns {string}
 */
function formatErrorsOutput(allResults) {
  return (
    allResults
      // Include skipped(ignore-if-error) entries that still captured an error so
      // downstream failure handling can surface assignment problems in issue/comment reports.
      // Include hard failures (!success) and ignored failures (skipped=true with error).
      .filter(r => r.error && (r.skipped || !r.success))
      .map(r => {
        const number = r.issue_number || r.pull_number;
        const prefix = r.issue_number ? "issue" : "pr";
        return `${prefix}:${number}:${r.agent}:${r.error}`;
      })
      .join("\n")
  );
}

/**
 * @param {Array<{issue_number: number|null, pull_number: number|null, agent: string, owner: string|null, repo: string|null, pull_request_repo?: string|null, success: boolean, skipped?: boolean, error?: string}>} allResults
 * @returns {number}
 */
function countHardFailures(allResults) {
  return allResults.filter(r => !r.success && !r.skipped).length;
}

/**
 * @param {Array<{issue_number: number|null, pull_number: number|null, agent: string, owner: string|null, repo: string|null, pull_request_repo?: string|null, success: boolean, skipped?: boolean, error?: string}>} allResults
 * @returns {Promise<void>}
 */
async function writeAssignSummary(allResults) {
  const successResults = allResults.filter(r => r.success && !r.skipped);
  const skippedResults = allResults.filter(r => r.skipped);
  const failedResults = allResults.filter(r => !r.success && !r.skipped);

  if (allResults.length === 0) return;

  let summaryContent = "## Agent Assignment\n\n";

  if (successResults.length > 0) {
    summaryContent += `✅ Successfully assigned ${successResults.length} agent(s):\n\n`;
    summaryContent += successResults
      .map(r => {
        const itemType = r.issue_number ? `Issue #${r.issue_number}` : `Pull Request #${r.pull_number}`;
        return `- ${itemType} → Agent: ${r.agent}${r.pull_request_repo ? ` (PR target: ${r.pull_request_repo})` : ""}`;
      })
      .join("\n");
    summaryContent += "\n\n";
  }

  if (skippedResults.length > 0) {
    summaryContent += `⏭️ Skipped ${skippedResults.length} agent assignment(s) (ignore-if-error enabled):\n\n`;
    summaryContent += skippedResults
      .map(r => {
        const itemType = r.issue_number ? `Issue #${r.issue_number}` : `Pull Request #${r.pull_number}`;
        return `- ${itemType} → Agent: ${r.agent}${r.pull_request_repo ? ` (PR target: ${r.pull_request_repo})` : ""} (assignment failed due to error)`;
      })
      .join("\n");
    summaryContent += "\n\n";
  }

  if (failedResults.length > 0) {
    summaryContent += `❌ Failed to assign ${failedResults.length} agent(s):\n\n`;
    summaryContent += failedResults
      .map(r => {
        const itemType = r.issue_number ? `Issue #${r.issue_number}` : `Pull Request #${r.pull_number}`;
        return `- ${itemType} → Agent: ${r.agent}${r.pull_request_repo ? ` (PR target: ${r.pull_request_repo})` : ""}: ${r.error}`;
      })
      .join("\n");

    const hasPermissionError = failedResults.some(r => r.error?.includes("Resource not accessible") || r.error?.includes("Insufficient permissions"));
    if (hasPermissionError) {
      summaryContent += generatePermissionErrorSummary();
    }
    summaryContent += "\n\n";
  }

  try {
    await core.summary.addRaw(summaryContent).write();
  } catch (error) {
    core.warning(`Failed to write agent assignment summary: ${getErrorMessage(error)}`);
  }
}

/**
 * Handler factory for assign-to-agent safe output.
 *
 * Replaces the standalone assign_to_agent step. This function is called once by the
 * safe output handler manager with the handler's configuration. It returns a message
 * processor function that is invoked for each assign_to_agent message in the agent output.
 *
 * @param {Object} config - Handler configuration from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG
 * @returns {Promise<Function>} Message processor function
 */
async function main(config = {}) {
  // Parse configuration (replaces env vars from the old standalone step)
  const maxCount = parseInt(String(config.max ?? "1"), 10);
  if (Number.isNaN(maxCount) || maxCount < 1) {
    throw new Error(`Invalid max value: ${config.max}. Must be a positive integer`);
  }
  const defaultAgent = String(config.name ?? "copilot").trim();
  const defaultModel = config.model ? String(config.model).trim() : null;
  const reasoningEffort = config["reasoning-effort"] ?? null;
  const defaultCustomAgent = config["custom-agent"] ? String(config["custom-agent"]).trim() : null;
  const defaultCustomInstructions = config["custom-instructions"] ? String(config["custom-instructions"]).trim() : null;
  const configuredBaseBranch = config["base-branch"] ? String(config["base-branch"]).trim() : null;
  const targetConfig = config.target ? String(config.target).trim() : "triggering";
  const ignoreIfError = config["ignore-if-error"] === true || config["ignore-if-error"] === "true";
  const issueIntentEnabled = config.issue_intent !== false;
  const allowedAgents = config.allowed
    ? Array.isArray(config.allowed)
      ? config.allowed.map(a => String(a).trim()).filter(Boolean)
      : String(config.allowed)
          .split(",")
          .map(a => a.trim())
          .filter(Boolean)
    : null;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const allowedPullRequestRepos = parseAllowedRepos(config["allowed-pull-request-repos"]);

  // Create a dedicated Octokit instance using the agent token
  const githubClient = await createAssignToAgentGitHubClient(config);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  core.info(`Default agent: ${defaultAgent}`);
  if (defaultModel) core.info(`Default model: ${defaultModel}`);
  if (defaultCustomAgent) core.info(`Default custom agent: ${defaultCustomAgent}`);
  if (configuredBaseBranch) core.info(`Configured base branch: ${configuredBaseBranch}`);
  core.info(`Target configuration: ${targetConfig}`);
  core.info(`Max count: ${maxCount}`);
  core.info(`Issue intent enabled: ${issueIntentEnabled}`);
  if (ignoreIfError) core.info("Ignore-if-error mode enabled: Will not fail if agent assignment encounters auth or availability errors");
  if (allowedAgents) core.info(`Allowed agents: ${allowedAgents.join(", ")}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) core.info(`Allowed repos: ${[...allowedRepos].join(", ")}`);

  // Resolve pull request repo upfront (if globally configured)
  /** @type {any} */
  let pullRequestOwner = null;
  /** @type {any} */
  let pullRequestRepo = null;
  let effectiveBaseBranch = configuredBaseBranch;
  const pullRequestRepoConfig = config["pull-request-repo"] ? String(config["pull-request-repo"]).trim() : null;

  if (pullRequestRepoConfig) {
    const parts = pullRequestRepoConfig.split("/");
    if (parts.length === 2) {
      const repoValidation = validateRepo(pullRequestRepoConfig, pullRequestRepoConfig, allowedPullRequestRepos);
      if (!repoValidation.valid) {
        throw new Error(`E004: ${repoValidation.error}`);
      }
      pullRequestOwner = parts[0];
      pullRequestRepo = parts[1];
      core.info(`Using pull request repository: ${pullRequestOwner}/${pullRequestRepo}`);
      try {
        const resolved = await resolvePullRequestRepo(githubClient, pullRequestOwner, pullRequestRepo, configuredBaseBranch);
        effectiveBaseBranch = resolved.effectiveBaseBranch;
        if (!configuredBaseBranch && effectiveBaseBranch) {
          core.info(`Resolved pull request repository default branch: ${effectiveBaseBranch}`);
        }
      } catch (error) {
        throw new Error(`Failed to fetch pull request repository for ${pullRequestOwner}/${pullRequestRepo}: ${getErrorMessage(error)}`, { cause: error });
      }
    } else {
      core.warning(`Invalid pull-request-repo format: ${pullRequestRepoConfig}. Expected owner/repo. PRs will be created in issue repository.`);
    }
  }

  // Closure-level state
  let processedCount = 0;
  /** @type {Array<{issue_number: number|null, pull_number: number|null, agent: string, owner: string|null, repo: string|null, pull_request_repo?: string|null, success: boolean, skipped?: boolean, error?: string}>} */
  const allResults = [];
  const agentCache = {};
  const processedAssignmentTargets = new Set();

  /**
   * Message processor — called once per assign_to_agent message by the handler manager.
   *
   * @param {Object} message - The assign_to_agent message from agent output
   * @param {Object} resolvedTemporaryIds - Plain object of already-resolved temp IDs
   * @param {Map<string, {repo: string, number: number}>} temporaryIdMap - Live temp ID map
   * @returns {Promise<{success: boolean, error?: string, skipped?: boolean, deferred?: boolean}>}
   */
  const handleMessage = async function (message, resolvedTemporaryIds, temporaryIdMap) {
    // Handle staged mode — emit preview and skip actual assignment
    if (isStaged) {
      await generateStagedPreview({
        title: "Assign to Agent",
        description: "The following agent assignments would be made if staged mode was disabled:",
        items: [message],
        renderItem: item => {
          const parts = [];
          if (item.issue_number) {
            parts.push(`**Issue:** #${item.issue_number}`);
          } else if (item.pull_number) {
            parts.push(`**Pull Request:** #${item.pull_number}`);
          }
          parts.push(`**Agent:** ${item.agent || defaultAgent}`);
          if (defaultModel) parts.push(`**Model:** ${defaultModel}`);
          const stagedReasoningEffort = resolveReasoningEffort(reasoningEffort);
          if (stagedReasoningEffort) parts.push(`**Reasoning Effort:** ${stagedReasoningEffort}`);
          if (defaultCustomAgent) parts.push(`**Custom Agent:** ${defaultCustomAgent}`);
          if (defaultCustomInstructions) parts.push(`**Custom Instructions:** ${defaultCustomInstructions}`);
          return parts.join("\n") + "\n\n";
        },
      });
      return { success: true, skipped: true };
    }

    const agentName = message.agent ?? defaultAgent;
    const intentMetadata = issueIntentEnabled ? normalizeIssueIntentMetadata(message) : {};
    const model = defaultModel;
    const customAgent = defaultCustomAgent;
    const customInstructions = defaultCustomInstructions || null;

    // Validate that both issue_number and pull_number are not specified simultaneously
    if (message.issue_number != null && message.pull_number != null) {
      const error = "Cannot specify both issue_number and pull_number in the same assign_to_agent item";
      core.error(error);
      allResults.push({ issue_number: message.issue_number, pull_number: message.pull_number, agent: agentName, owner: null, repo: null, success: false, error });
      return { success: false, error };
    }

    // Defer if issue_number is a temporary ID that hasn't been resolved yet
    // Strip leading '#' so both 'aw_abc1' and '#aw_abc1' (canonical validator form) are handled
    if (message.issue_number != null) {
      const issueNumStr = String(message.issue_number).trim();
      if (isTemporaryId(issueNumStr)) {
        const normalized = normalizeTemporaryId(issueNumStr);
        if (!temporaryIdMap.has(normalized)) {
          core.info(`Deferring assign_to_agent — temporary ID ${message.issue_number} not yet resolved`);
          return { success: false, deferred: true };
        }
      }
    }

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "issue/PR");
    if (!repoResult.success) {
      core.error(`E004: ${repoResult.error}`);
      allResults.push({ issue_number: message.issue_number || null, pull_number: message.pull_number || null, agent: agentName, owner: null, repo: null, success: false, error: repoResult.error });
      return { success: false, error: repoResult.error };
    }
    let effectiveOwner = repoResult.repoParts.owner;
    let effectiveRepo = repoResult.repoParts.repo;
    let itemForTarget = message;

    // Resolve temporary ID in issue_number to real issue number
    if (message.issue_number != null) {
      const resolvedTarget = resolveRepoIssueTarget(message.issue_number, temporaryIdMap, effectiveOwner, effectiveRepo);
      if (!resolvedTarget.resolved) {
        const error = resolvedTarget.errorMessage || `Failed to resolve issue target: ${message.issue_number}`;
        core.error(error);
        allResults.push({ issue_number: message.issue_number, pull_number: null, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error });
        return { success: false, error };
      }
      effectiveOwner = resolvedTarget.resolved.owner;
      effectiveRepo = resolvedTarget.resolved.repo;
      itemForTarget = { ...message, issue_number: resolvedTarget.resolved.number };
      if (resolvedTarget.wasTemporaryId) {
        core.info(`Resolved temporary issue id to ${effectiveOwner}/${effectiveRepo}#${resolvedTarget.resolved.number}`);
      }
    }

    // Determine effective target configuration
    const hasExplicitTarget = itemForTarget.issue_number != null || itemForTarget.pull_number != null;
    const effectiveTarget = hasExplicitTarget ? "*" : targetConfig;

    const basePullRequestRepoSlug = pullRequestOwner && pullRequestRepo ? `${pullRequestOwner}/${pullRequestRepo}` : `${effectiveOwner}/${effectiveRepo}`;

    // Handle per-item pull_request_repo override
    let effectivePullRequestRepoSlug = basePullRequestRepoSlug;
    let effectivePullRequestBaseBranch = effectiveBaseBranch;
    let hasValidatedPerItemPullRequestRepoOverride = false;
    const hasPullRequestRepoOverrideField = message.pull_request_repo != null;
    const trimmedPullRequestRepoOverride = typeof message.pull_request_repo === "string" ? message.pull_request_repo.trim() : "";
    if (trimmedPullRequestRepoOverride) {
      const itemPullRequestRepo = trimmedPullRequestRepoOverride;
      const pullRequestRepoParts = itemPullRequestRepo.split("/");
      if (pullRequestRepoParts.length === 2) {
        const defaultPullRequestRepo = pullRequestRepoConfig || defaultTargetRepo;
        const pullRequestRepoValidation = validateRepo(itemPullRequestRepo, defaultPullRequestRepo, allowedPullRequestRepos);
        if (!pullRequestRepoValidation.valid) {
          const error = pullRequestRepoValidation.error ?? "Repository validation failed";
          core.error(`E004: ${error}`);
          allResults.push({ issue_number: message.issue_number || null, pull_number: message.pull_number || null, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error });
          return { success: false, error };
        }
        try {
          const resolvedPullRequestRepo = await resolvePullRequestRepo(githubClient, pullRequestRepoParts[0], pullRequestRepoParts[1], configuredBaseBranch);
          effectivePullRequestRepoSlug = itemPullRequestRepo;
          effectivePullRequestBaseBranch = resolvedPullRequestRepo.effectiveBaseBranch;
          hasValidatedPerItemPullRequestRepoOverride = true;
          core.info(`Using per-item pull request repository: ${itemPullRequestRepo}`);
        } catch (error) {
          const errorMsg = `Failed to resolve pull request repository for ${itemPullRequestRepo}: ${getErrorMessage(error)}`;
          core.error(errorMsg);
          allResults.push({ issue_number: message.issue_number || null, pull_number: message.pull_number || null, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error: errorMsg });
          return { success: false, error: errorMsg };
        }
      } else {
        core.warning(`Invalid pull_request_repo format: ${itemPullRequestRepo}. Expected owner/repo. Using global pull-request-repo if configured.`);
      }
    } else if (hasPullRequestRepoOverrideField && typeof message.pull_request_repo === "string") {
      core.warning("Invalid pull_request_repo value. Expected owner/repo. Using global pull-request-repo if configured.");
    } else if (hasPullRequestRepoOverrideField) {
      core.warning("Invalid pull_request_repo value. Expected a non-empty owner/repo string. Using global pull-request-repo if configured.");
    }

    // Resolve the target issue or pull request number from context
    const targetResult = resolveTarget({
      targetConfig: effectiveTarget,
      item: itemForTarget,
      context,
      itemType: "assign_to_agent",
      supportsPR: true,
      supportsIssue: false,
    });

    if (!targetResult.success) {
      if (targetResult.shouldFail) {
        core.error(targetResult.error);
        allResults.push({ issue_number: message.issue_number || null, pull_number: message.pull_number || null, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error: targetResult.error });
        return { success: false, error: targetResult.error };
      } else {
        core.info(targetResult.error);
        return { success: false, skipped: true };
      }
    }

    const number = targetResult.number;
    const type = targetResult.contextType;
    const issueNumber = type === "issue" ? number : null;
    const pullNumber = type === "pull request" ? number : null;

    if (Number.isNaN(number) || number <= 0) {
      const error = `Invalid ${type} number: ${number}`;
      core.error(error);
      allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error });
      return { success: false, error };
    }

    // Validate agent name
    if (!AGENT_LOGIN_NAMES[agentName]) {
      const error = `Unsupported agent: ${agentName}`;
      core.warning(`Agent "${agentName}" is not supported. Supported agents: ${Object.keys(AGENT_LOGIN_NAMES).join(", ")}`);
      allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error });
      return { success: false, error };
    }

    // Enforce allowed agents list
    if (allowedAgents && !allowedAgents.includes(agentName)) {
      const error = `Agent not allowed: ${agentName}`;
      core.error(`Agent "${agentName}" is not in the allowed list. Allowed agents: ${allowedAgents.join(", ")}`);
      allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, error });
      return { success: false, error };
    }

    // Atomically reserve the max-count slot before beginning an assignment attempt.
    if (processedCount >= maxCount) {
      core.info(`⏭ Max count (${maxCount}) reached, skipping agent assignment`);
      allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, success: false, skipped: true });
      return { success: false, skipped: true };
    }
    processedCount++;

    // Add delay between consecutive assignments to avoid spawning too many agents at once
    if (processedCount > 1) {
      core.info("Waiting 10 seconds before processing next agent assignment...");
      await sleep(10000);
    }

    try {
      // Find agent (use cache to avoid repeated lookups)
      let agentLogin = agentCache[agentName];
      if (!agentLogin) {
        core.info(`Looking for ${agentName} coding agent...`);
        agentLogin = await findAgent(effectiveOwner, effectiveRepo, agentName, issueNumber || pullNumber, githubClient);
        if (!agentLogin) {
          throw new Error(`${agentName} coding agent is not available for this repository`);
        }
        agentCache[agentName] = agentLogin;
        core.info(`Found ${agentName} coding agent (login: ${agentLogin})`);
      }

      // Get issue or PR details
      core.info(`Getting ${type} details...`);
      let assignableId;
      let currentAssignees;
      /** @type {any} */
      let taskContext = null;
      if (issueNumber) {
        const issueDetails = await getIssueDetails(effectiveOwner, effectiveRepo, issueNumber, githubClient);
        if (!issueDetails) throw new Error(`Failed to get issue details`);
        assignableId = issueDetails.issueId;
        currentAssignees = issueDetails.currentAssignees;
        taskContext = issueDetails.taskContext || { owner: effectiveOwner, repo: effectiveRepo, type: "issue", number: issueNumber };
      } else if (pullNumber) {
        const prDetails = await getPullRequestDetails(effectiveOwner, effectiveRepo, pullNumber, githubClient);
        if (!prDetails) throw new Error(`Failed to get pull request details`);
        assignableId = prDetails.pullRequestId;
        currentAssignees = prDetails.currentAssignees;
        taskContext = prDetails.taskContext || { owner: effectiveOwner, repo: effectiveRepo, type: "pull", number: pullNumber };
      } else {
        throw new Error(`No issue or pull request number available`);
      }

      core.info(`${type} ID: ${assignableId}`);

      const assignmentContextKey = `${effectiveOwner}/${effectiveRepo}:${type}:${number}:${effectivePullRequestRepoSlug}`;
      const seenThisContextBefore = processedAssignmentTargets.has(assignmentContextKey);
      // Track assignment context (target + per-item pull_request_repo) to prevent duplicate
      // re-assignment calls while still allowing one global issue to fan out to multiple repos.
      processedAssignmentTargets.add(assignmentContextKey);
      const shouldAllowReassignment = hasValidatedPerItemPullRequestRepoOverride && !seenThisContextBefore;

      // Skip if agent is already assigned and no explicit per-item pull_request_repo is specified.
      // When a different pull_request_repo is provided on the message, allow re-assignment
      // so Copilot can be triggered for a different target repository on the same issue.
      const knownLogins = getAgentLogins(agentName);
      if (currentAssignees.some(a => a.login === agentLogin || knownLogins.includes(a.login)) && !shouldAllowReassignment) {
        core.info(`${agentName} is already assigned to ${type} #${number}`);
        allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, pull_request_repo: effectivePullRequestRepoSlug, success: true });
        return { success: true };
      }

      core.info(`Assigning ${agentName} coding agent to ${type} #${number}...`);
      if (model) core.info(`Using model: ${model}`);
      if (customAgent) core.info(`Using custom agent: ${customAgent}`);
      if (customInstructions) core.info(`Using custom instructions: ${customInstructions.substring(0, 100)}${customInstructions.length > 100 ? "..." : ""}`);
      if (effectivePullRequestBaseBranch) core.info(`Using base branch: ${effectivePullRequestBaseBranch}`);

      const success = await assignAgentToIssue(
        assignableId,
        agentLogin,
        currentAssignees,
        agentName,
        allowedAgents,
        model,
        customAgent,
        customInstructions,
        effectivePullRequestBaseBranch,
        githubClient,
        taskContext,
        effectivePullRequestRepoSlug,
        intentMetadata,
        issueIntentEnabled,
        reasoningEffort
      );
      if (!success) throw new Error(`Failed to assign ${agentName} via REST`);

      core.info(`Successfully assigned ${agentName} coding agent to ${type} #${number}`);
      allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, pull_request_repo: effectivePullRequestRepoSlug, success: true });
      return { success: true };
    } catch (error) {
      let errorMessage = getErrorMessage(error);

      // When the agent specified an issue_number that turns out to be a PR, skip
      // silently without posting a comment — error comments on PRs are confusing.
      if (/** @type {any} */ error.isPullRequest) {
        core.warning(`Skipping assign_to_agent for #${number}: target is a pull request, not an issue.`);
        allResults.push({
          issue_number: issueNumber,
          pull_number: pullNumber,
          agent: agentName,
          owner: effectiveOwner,
          repo: effectiveRepo,
          pull_request_repo: effectivePullRequestRepoSlug,
          success: false,
          skipped: true,
          error: errorMessage,
        });
        return { success: false, skipped: true, error: errorMessage };
      }

      const isAuthError = ["Bad credentials", "Not Authenticated", "Resource not accessible", "Insufficient permissions", "requires authentication"].some(msg => errorMessage.includes(msg));
      const isAvailabilityError = errorMessage.includes("coding agent is not available for this repository");

      if (ignoreIfError && (isAuthError || isAvailabilityError)) {
        const errorType = isAuthError ? "authentication/permission" : "agent availability";
        core.warning(`Agent assignment failed for ${agentName} on ${type} #${number} due to ${errorType} error. Skipping due to ignore-if-error=true.`);
        core.info(`Error details: ${errorMessage}`);
        allResults.push({
          issue_number: issueNumber,
          pull_number: pullNumber,
          agent: agentName,
          owner: effectiveOwner,
          repo: effectiveRepo,
          pull_request_repo: effectivePullRequestRepoSlug,
          success: true,
          skipped: true,
          error: errorMessage,
        });
        return { success: true, skipped: true };
      }

      if (isAvailabilityError) {
        try {
          const available = await getAvailableAgentLogins(effectiveOwner, effectiveRepo, issueNumber || pullNumber, githubClient);
          if (available.length > 0) errorMessage += ` (available agents: ${available.join(", ")})`;
        } catch (e) {
          core.debug("Failed to enrich unavailable agent message with available list");
        }
      }

      core.error(`Failed to assign agent "${agentName}" to ${type} #${number}: ${errorMessage}`);

      // Post failure comment on the issue/PR so the user sees the failure in context
      try {
        await githubClient.rest.issues.createComment({
          owner: effectiveOwner,
          repo: effectiveRepo,
          issue_number: number,
          body: sanitizeContent(`⚠️ **Assignment failed**: Failed to assign ${agentName} coding agent to this ${type}.\n\nError: ${errorMessage}`, { maxLength: 65000 }),
        });
        core.info(`Posted failure comment on ${type} #${number} in ${effectiveOwner}/${effectiveRepo}`);
      } catch (commentError) {
        core.warning(`Failed to post failure comment on ${type} #${number}: ${getErrorMessage(commentError)}`);
      }

      allResults.push({ issue_number: issueNumber, pull_number: pullNumber, agent: agentName, owner: effectiveOwner, repo: effectiveRepo, pull_request_repo: effectivePullRequestRepoSlug, success: false, error: errorMessage });
      return { success: false, error: errorMessage };
    }
  };

  handleMessage.getAssignToAgentAssigned = () => formatAssignedOutput(allResults);
  handleMessage.getAssignToAgentErrors = () => formatErrorsOutput(allResults);
  handleMessage.getAssignToAgentErrorCount = () => countHardFailures(allResults);
  handleMessage.writeAssignToAgentSummary = () => writeAssignSummary(allResults);

  return handleMessage;
}

/**
 * Returns the "assigned" output string for step outputs.
 * Format: "issue:N:agent" or "pr:N:agent" per successful assignment, newline-separated.
 * @returns {string}
 */
function getAssignToAgentAssigned(handler) {
  return typeof handler?.getAssignToAgentAssigned === "function" ? handler.getAssignToAgentAssigned() : "";
}

/**
 * Returns the "assignment_errors" output string for step outputs.
 * Format: "issue:N:agent:error" or "pr:N:agent:error" per failure/skipped-with-error,
 * newline-separated.
 * @returns {string}
 */
function getAssignToAgentErrors(handler) {
  return typeof handler?.getAssignToAgentErrors === "function" ? handler.getAssignToAgentErrors() : "";
}

/**
 * Returns the "assignment_error_count" output value.
 * @returns {number}
 */
function getAssignToAgentErrorCount(handler) {
  return typeof handler?.getAssignToAgentErrorCount === "function" ? handler.getAssignToAgentErrorCount() : 0;
}

/**
 * Writes a step summary for agent assignment results.
 * Called by the handler manager after all messages have been processed.
 * @returns {Promise<void>}
 */
async function writeAssignToAgentSummary(handler) {
  if (typeof handler?.writeAssignToAgentSummary === "function") {
    await handler.writeAssignToAgentSummary();
  }
}

module.exports = { main, getAssignToAgentAssigned, getAssignToAgentErrors, getAssignToAgentErrorCount, writeAssignToAgentSummary };
