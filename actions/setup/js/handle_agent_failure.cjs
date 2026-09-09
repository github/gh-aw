// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getDetectionCautionAlert, getFooterAgentFailureIssueMessage, getFooterAgentFailureCommentMessage, generateXMLMarker } = require("./messages.cjs");
const { renderTemplate, renderTemplateFromFile, getPromptPath, renderFilesList } = require("./messages_core.cjs");
const { getCurrentBranch } = require("./get_current_branch.cjs");
const { createExpirationLine, extractExpirationDate, generateFooterWithExpiration } = require("./ephemerals.cjs");
const { MAX_SUB_ISSUES, getSubIssueCount } = require("./sub_issue_helpers.cjs");
const { formatMissingData, formatMissingTools } = require("./missing_info_formatter.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { AWF_INFRA_LINE_RE } = require("./log_parser_shared.cjs");
const { applyAddMaskRedaction, collectAddMaskedValues, isAddMaskCommandLine } = require("./add_mask_redaction.cjs");
const {
  resolveFirewallAuditLogPath,
  resolveAICreditsFailureState,
  parseMaxAICreditsFromAuditLog,
  parseAICreditsErrorInfoFromAuditLog,
  parseUnknownModelAICreditsFromAuditLog,
  parseMaxCacheMissesExceededFromEventLog,
} = require("./ai_credits_context.cjs");
const { MAX_CACHE_MISSES_EXCEEDED_PATTERN, SHELL_EXPANSION_GUARD_REJECTED_PATTERN } = require("./detect_agent_errors.cjs");
const { formatAICCredits } = require("./daily_aic_workflow_helpers.cjs");
const { formatAIC } = require("./model_costs.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { parseTokenUsageJsonl, generateTokenUsageSummary } = require("./parse_mcp_gateway_log.cjs");
const { readDedupedTokenUsage, TOKEN_USAGE_PATHS } = require("./parse_token_usage.cjs");
const { extractShellCommandFromToolData } = require("./tool_call_details.cjs");
const fs = require("fs");
const https = require("https");
const os = require("os");
const path = require("path");

const DEFAULT_ACTION_FAILURE_ISSUE_EXPIRES_HOURS = 24 * 7;
/** Claude Code error emitted when a `--continue` resume finds no deferred tool marker. */
const NO_DEFERRED_MARKER_LINE_RE = /No deferred tool marker found/i;
/** Claude harness line reporting that the no-deferred-marker error was recovered by a fresh retry. */
const CLAUDE_HARNESS_NO_DEFERRED_MARKER_RECOVERY_RE = /^\[claude-harness\].*no deferred tool marker on --continue.*retrying as fresh run.*--continue disabled permanently/i;
/**
 * Terminal failure reported by an engine harness wrapper, e.g.
 * `[copilot-harness] unexpected error: copilot-sdk headless server did not become ready ...`.
 * These lines carry the actionable root cause even though the harness prefix marks them as
 * infrastructure output, so they are extracted as engine error details.
 */
const HARNESS_UNEXPECTED_ERROR_RE = /^\[(?:copilot|claude|codex)-harness\]\s*unexpected error:\s*(.+)$/;
const FAILURE_ISSUE_DEDUP_WINDOW_HOURS = 24;
const FAILURE_ISSUE_CATEGORY_DAILY_CAP = 50;
const FAILURE_ISSUE_WINDOW_MS = FAILURE_ISSUE_DEDUP_WINDOW_HOURS * 60 * 60 * 1000;
const DEFAULT_OTEL_JSONL_PATH = "/tmp/gh-aw/otel.jsonl";
/** Path to the failure categories file written by handle_agent_failure and read by the OTLP conclusion span. */
const FAILURE_CATEGORIES_PATH = "/tmp/gh-aw/failure_categories.json";
const GITHUB_API_VERSION = "2022-11-28";
const COPILOT_SESSION_STATE_DIR = path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state");
const RECENT_TOOL_CALLS_WITH_COMMAND_PREVIEW = new Set(["bash", "shell"]);
const ELLIPSIS = "...";
const ELLIPSIS_LENGTH = ELLIPSIS.length;
// Engine-side 429/rate-limit signatures:
// - HTTP 429 accompanied by "too many requests"/"rate limit" phrasing
// - provider error codes like rate_limit_error / rate_limit_exceeded
// - Copilot/CAPI "CAPIError: 429" and utility-model quota text
// - retry wrapper text that includes the canonical "Failed to get response..." phrase
const ENGINE_RATE_LIMIT_429_RE =
  /(?:\b429\b[\s\S]{0,120}(?:too many requests|rate[\s-]*limit)|\brate_limit_(?:error|exceeded)\b|capierror:\s*429|failed to get response from the ai model[\s\S]{0,120}\b429\b|exceeded your rate limit for utility models)/i;
const ENGINE_MAX_RUNS_EXCEEDED_RE = /(?:\bmax_runs_exceeded\b|\bmaximum\s+llm\s+invocations\s+exceeded\b)/i;
const ALLOWED_FILES_ERROR_RE = /^(?<summary>.*outside the allowed-files list) \((?<files>.+?)\)\. (?<remediation>Add the files to the allowed-files configuration field or remove them from the (?:patch|bundle)\.)$/;

/**
 * Parse action failure issue expiration from environment.
 *
 * A value of "0" is an explicit signal from the compiler that no maintenance
 * workflow will exist to enforce expiration, so expiration must be disabled
 * (no expiration marker is written to failure issues). Missing/invalid values
 * fall back to the 168-hour default for backwards compatibility with older
 * generated lock files that always set a positive value.
 * @returns {number} Expiration in hours (0 means disabled; defaults to 168 when unset/invalid)
 */
function getActionFailureIssueExpiresHours() {
  const raw = process.env.GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS || "";
  if (raw === "") {
    return DEFAULT_ACTION_FAILURE_ISSUE_EXPIRES_HOURS;
  }
  if (raw === "0") {
    return 0;
  }
  if (/^[1-9]\d*$/.test(raw)) {
    return Number(raw);
  }
  return DEFAULT_ACTION_FAILURE_ISSUE_EXPIRES_HOURS;
}

/**
 * Extracts the numeric run ID from a GitHub Actions run URL.
 * @param {string} runUrl  e.g. "https://github.com/owner/repo/actions/runs/12345678"
 * @returns {string} run ID, or empty string if not found
 */
function extractRunId(runUrl) {
  if (!runUrl) return "";
  const m = runUrl.match(/\/actions\/runs\/(\d+)/);
  return m ? m[1] : "";
}

/**
 * Build a GitHub markdown warning alert line.
 * @param {string} title
 * @param {string} message
 * @returns {string}
 */
function buildWarningAlertLine(title, message) {
  return `\n> [!WARNING]\n> **${title}**: ${message}\n`;
}

/**
 * Render an allowed-files error using progressive disclosure for the file list.
 * @param {string} type
 * @param {string} error
 * @returns {string|null}
 */
function renderAllowedFilesError(type, error) {
  const match = error.match(ALLOWED_FILES_ERROR_RE);
  if (!match?.groups) {
    return null;
  }

  const summary = match.groups.summary;
  const files = match.groups.files.split(",").map(file => file.trim());
  const remediation = match.groups.remediation;
  const fileCount = files.length;
  const fileWord = fileCount === 1 ? "file" : "files";

  return `- \`${type}\`: ${summary}. ${remediation}

<details>
<summary>Show ${fileCount} blocked ${fileWord}</summary>

${renderFilesList(files)}

</details>
`;
}

/**
 * Render a prompt template from runtime prompts.
 * @param {string} templateName
 * @param {Record<string, string|number|boolean|undefined>} [context]
 * @returns {string}
 */
function renderPromptTemplate(templateName, context = {}) {
  return renderTemplateFromFile(getPromptPath(templateName), context);
}

/**
 * Attempt to find a pull request for the current branch
 * @returns {Promise<{number: number, html_url: string, head_sha: string, mergeable: boolean | null, mergeable_state: string, updated_at: string} | null>} PR info or null if not found
 */
async function findPullRequestForCurrentBranch() {
  try {
    const { owner, repo } = context.repo;
    const currentBranch = getCurrentBranch();

    core.info(`Searching for pull request from branch: ${currentBranch}`);

    // Search for open PRs with the current branch as head
    const searchQuery = `repo:${owner}/${repo} is:pr is:open head:${currentBranch}`;

    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const pr = searchResult.data.items[0];
      core.info(`Found pull request #${pr.number}: ${pr.html_url}`);

      // Fetch detailed PR info to get mergeable state and head SHA
      try {
        const detailedPR = await github.rest.pulls.get({
          owner,
          repo,
          pull_number: pr.number,
        });

        core.info(`PR #${pr.number} details - head_sha: ${detailedPR.data.head.sha}, mergeable: ${detailedPR.data.mergeable}, mergeable_state: ${detailedPR.data.mergeable_state}`);

        return {
          number: pr.number,
          html_url: pr.html_url,
          head_sha: detailedPR.data.head.sha,
          mergeable: detailedPR.data.mergeable,
          mergeable_state: detailedPR.data.mergeable_state || "unknown",
          updated_at: detailedPR.data.updated_at,
        };
      } catch (detailsError) {
        core.warning(`Failed to fetch detailed PR info: ${getErrorMessage(detailsError)}`);
        // Fall back to basic info
        return {
          number: pr.number,
          html_url: pr.html_url,
          head_sha: "",
          mergeable: null,
          mergeable_state: "unknown",
          updated_at: "",
        };
      }
    }

    core.info(`No pull request found for branch: ${currentBranch}`);
    return null;
  } catch (error) {
    core.warning(`Failed to find pull request for current branch: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Parse HTML comment metadata into key/value pairs.
 * @param {string} body - Body text to inspect
 * @param {string} markerKey - Marker key that must be present in the comment
 * @returns {Record<string, string>|null} Parsed metadata or null when not found
 */
function parseHTMLCommentMetadata(body, markerKey) {
  if (!body) {
    return null;
  }

  for (const match of body.matchAll(/<!--\s*([\s\S]*?)\s*-->/g)) {
    const content = match[1].trim();
    if (!content.includes(`${markerKey}:`)) {
      continue;
    }

    /** @type {Record<string, string>} */
    const metadata = {};
    const pairMatches = [...content.matchAll(/(?:^|,\s*)([a-zA-Z0-9_-]+):\s*/g)];
    for (let index = 0; index < pairMatches.length; index += 1) {
      const pairMatch = pairMatches[index];
      const nextPairMatch = pairMatches[index + 1];
      const valueStart = (pairMatch.index || 0) + pairMatch[0].length;
      const valueEnd = nextPairMatch ? nextPairMatch.index || content.length : content.length;
      metadata[pairMatch[1]] = content.slice(valueStart, valueEnd).trim();
    }

    if (metadata[markerKey]) {
      return metadata;
    }
  }

  return null;
}

/**
 * Build the stable category set used to match failure issues precisely.
 * @param {Object} options - Active failure signals
 * @returns {string[]} Sorted failure categories
 */
function buildFailureMatchCategories(options) {
  const categories = [];

  if (options.isTimedOut) categories.push("timed_out");
  if (options.hasAssignmentErrors) categories.push("assignment_errors");
  if (options.hasAssignCopilotFailures) categories.push("assign_copilot_failures");
  if (options.hasSkillInstallFailures) categories.push("skill_install_failures");
  if (options.hasCreateDiscussionErrors) categories.push("create_discussion_errors");
  if (options.hasCodePushFailures) categories.push("code_push_failures");
  if (options.hasRepoMemoryValidationErrors) categories.push("repo_memory_validation_errors");
  if (options.hasPushRepoMemoryFailure) categories.push("push_repo_memory_failure");
  if (options.hasMissingSafeOutputs) categories.push("missing_safe_outputs");
  if (options.hasReportIncomplete) categories.push("report_incomplete");
  if (options.hasMissingTool) categories.push("missing_tool");
  if (options.hasToolDenialsExceeded) categories.push("tool_denials_exceeded");
  if (options.hasMissingData) categories.push("missing_data");
  if (options.hasCacheMissMisconfiguration) categories.push("cache_miss_misconfiguration");
  if (options.secretVerificationFailed) categories.push("secret_verification_failed");
  if (options.hasDockerSbxSecretsFailed) categories.push("docker_sbx_secrets_missing");
  if (options.inferenceAccessError) categories.push("inference_access_error");
  if (options.mcpPolicyError) categories.push("mcp_policy_error");
  if (options.modelNotSupportedError) categories.push("model_not_supported_error");
  if (options.http400ResponseError) categories.push("http_400_response_error");
  if (options.aiCreditsRateLimitError) categories.push("ai_credits_rate_limit_error");
  if (options.hasEngineRateLimit429) categories.push("engine_rate_limit_429");
  if (options.unknownModelAICredits) categories.push("unknown_model_ai_credits");
  if (options.missingModelPricingError) categories.push("missing_model_pricing");
  if (options.shellExpansionGuardRejected) categories.push("shell_expansion_guard_rejected");
  if (options.maxAICreditsExceeded) categories.push("max_ai_credits_exceeded");
  if (options.hasAppTokenMintingFailed) categories.push("app_token_minting_failed");
  if (options.hasLockdownCheckFailed) categories.push("lockdown_check_failed");
  if (options.hasOAuthTokenCheckFailed) categories.push("oauth_token_check_failed");
  if (options.hasStaleLockFileFailed) categories.push("stale_lock_file_failed");
  if (options.hasDailyAICExceeded) categories.push("daily_ai_credits_exceeded");
  if (options.isAWFFirewallStartupFailed) categories.push("awf_firewall_startup_failed");

  // Keep agent_failure as the fallback class only when no existing
  // specific category already explains the failure cause.
  const hasSpecificFailureCategory = categories.length > 0;
  if (options.agentConclusion === "failure" && !options.isTimedOut && !hasSpecificFailureCategory) {
    categories.push("agent_failure");
  }

  return categories.sort();
}

/**
 * Build a precise failure issue title for known failure classes.
 * Falls back to the generic failure title when no specific class matches.
 * @param {Object} options
 * @param {string} options.workflowName
 * @param {boolean} options.isTimedOut
 * @param {boolean} options.hasMissingSafeOutputs
 * @param {boolean} options.hasReportIncomplete
 * @param {boolean} options.hasMissingTool
 * @param {boolean} options.hasMissingData
 * @param {boolean} options.hasCacheMissMisconfiguration
 * @param {boolean} options.hasToolDenialsExceeded
 * @param {boolean} options.hasAppTokenMintingFailed
 * @param {boolean} options.hasLockdownCheckFailed
 * @param {boolean} options.hasOAuthTokenCheckFailed
 * @param {boolean} options.hasStaleLockFileFailed
 * @param {boolean} options.hasDailyAICExceeded
 * @param {boolean} options.aiCreditsRateLimitError
 * @param {boolean} options.hasEngineRateLimit429
 * @param {boolean} options.maxAICreditsExceeded
 * @param {boolean} options.hasAssignmentErrors
 * @param {boolean} options.http400ResponseError
 * @param {boolean} options.unknownModelAICredits
 * @param {boolean} [options.hasDockerSbxSecretsFailed]
 * @param {boolean} [options.missingModelPricingError]
 * @param {string} [options.missingModelPricingModelName]
 * @param {boolean} [options.shellExpansionGuardRejected]
 * @returns {string}
 */
function buildFailureIssueTitle(options) {
  const { workflowName } = options;
  if (options.hasDailyAICExceeded) return `[aw] ${workflowName} exceeded daily AI credits budget`;
  if (options.maxAICreditsExceeded) return `[aw] ${workflowName} exceeded max AI credits`;
  if (options.aiCreditsRateLimitError) return `[aw] ${workflowName} hit AI credits rate limit`;
  if (options.hasEngineRateLimit429) return `[aw] ${workflowName} hit engine rate limit (HTTP 429)`;
  // Missing model pricing is surfaced by the proxy as HTTP 400, so prefer the
  // specialized title before falling back to the generic transport-level error.
  if (options.missingModelPricingError) {
    const modelSuffix = options.missingModelPricingModelName ? ` (${options.missingModelPricingModelName})` : "";
    return `[aw] ${workflowName} has no AI credits pricing for model${modelSuffix}`;
  }
  // Keep HTTP 400 below AI-credits signals: quota/rate-limit indicates an account-level
  // budget state that should take precedence when both classes are detected.
  if (options.http400ResponseError) return `[aw] ${workflowName} hit HTTP 400 bad request`;
  // Unknown model pricing is a configuration error that may also trigger a timeout;
  // report it explicitly so the title is not misleadingly "timed out".
  if (options.unknownModelAICredits) return `[aw] ${workflowName} has unknown model pricing`;
  if (options.hasAppTokenMintingFailed) return `[aw] ${workflowName} failed to mint GitHub App token`;
  if (options.hasLockdownCheckFailed) return `[aw] ${workflowName} failed lockdown check`;
  if (options.hasOAuthTokenCheckFailed) return `[aw] ${workflowName} has OAuth token misconfiguration`;
  if (options.hasStaleLockFileFailed) return `[aw] ${workflowName} has stale lock file`;
  if (options.shellExpansionGuardRejected) return `[aw] ${workflowName} hit shell expansion guard rejection`;
  if (options.hasDockerSbxSecretsFailed) return `[aw] ${workflowName} is missing docker-sbx Docker Hub secrets`;
  if (options.isTimedOut) return `[aw] ${workflowName} timed out`;
  if (options.hasToolDenialsExceeded) return `[aw] ${workflowName} exceeded tool denial limit`;
  if (options.hasCacheMissMisconfiguration) return `[aw] ${workflowName} has cache-memory miss misconfiguration`;
  if (options.hasReportIncomplete) return `[aw] ${workflowName} reported incomplete result`;
  if (options.hasMissingSafeOutputs) return `[aw] ${workflowName} produced no safe outputs`;
  if (options.hasMissingTool) return `[aw] ${workflowName} is missing required tool`;
  if (options.hasMissingData) return `[aw] ${workflowName} is missing required data`;
  if (options.hasAssignmentErrors) return `[aw] ${workflowName} failed to assign agent`;
  return `[aw] ${workflowName} failed`;
}

/**
 * Generate a precise failure-match marker for failure issue bodies.
 * @param {Object} options - Marker options
 * @param {string} options.workflowId - Workflow identifier
 * @param {string} options.branch - Triggering branch
 * @param {number|undefined} options.pullRequestNumber - Triggering pull request number
 * @param {string[]} options.failureCategories - Sorted failure categories
 * @returns {string} HTML comment marker
 */
function generateFailureMatchMarker(options) {
  const { workflowId, branch, pullRequestNumber, failureCategories } = options;
  const parts = ["gh-aw-failure-issue: true", `workflow_id: ${workflowId}`, `branch: ${branch || ""}`, `failure_categories: ${failureCategories.join("|")}`];

  if (pullRequestNumber) {
    parts.push(`pull_request: ${pullRequestNumber}`);
  }

  return `<!-- ${parts.join(", ")} -->`;
}

/**
 * Determine whether an existing issue body matches the current failure precisely.
 * @param {string} body - Existing issue body
 * @param {Object} options - Match criteria
 * @param {string} options.workflowId - Workflow identifier
 * @param {string[]} options.failureCategories - Sorted failure categories
 * @returns {boolean} True when the issue body matches and is not expired
 */
function isReusableFailureIssue(body, options) {
  if (!body) {
    return false;
  }

  const expirationDate = extractExpirationDate(body);
  if (expirationDate && expirationDate.getTime() <= Date.now()) {
    return false;
  }

  const workflowMarker = parseHTMLCommentMetadata(body, "gh-aw-agentic-workflow");
  if (!workflowMarker || workflowMarker.workflow_id !== options.workflowId) {
    return false;
  }

  const failureMarker = parseHTMLCommentMetadata(body, "gh-aw-failure-issue");
  if (!failureMarker) {
    return false;
  }

  if ((failureMarker.workflow_id || "") !== options.workflowId) {
    return false;
  }

  return (failureMarker.failure_categories || "") === options.failureCategories.join("|");
}

/**
 * Determine whether an issue timestamp falls within the active dedup/throttle window.
 * @param {string|undefined} createdAt - Issue created_at timestamp
 * @param {number} windowStartMs - Inclusive lower bound as Unix ms
 * @returns {boolean} True when timestamp is missing or within the window
 */
function isIssueWithinWindow(createdAt, windowStartMs) {
  if (!createdAt) {
    return true;
  }
  const createdMs = Date.parse(createdAt);
  return Number.isFinite(createdMs) && createdMs >= windowStartMs;
}

/**
 * Escape a GitHub search phrase for safe inclusion inside double quotes.
 * GitHub search phrases are wrapped in double quotes, so embedded backslashes and
 * quotes must be escaped, and newlines are normalized to spaces to keep the query
 * on a single line.
 * @param {string} value - Raw phrase value
 * @returns {string} Escaped phrase
 */
function escapeGitHubSearchPhrase(value) {
  return value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\r?\n|\r/g, " ");
}

/**
 * Find an existing open failure issue that exactly matches the current failure metadata.
 * @param {Object} options - Search options
 * @param {string} options.owner - Repository owner
 * @param {string} options.repo - Repository name
 * @param {string} options.workflowId - Workflow identifier
 * @param {string[]} options.failureCategories - Sorted failure categories
 * @returns {Promise<{number: number, html_url: string} | null>} Matching issue or null
 */
async function findExistingFailureIssue(options) {
  const { owner, repo, workflowId, failureCategories } = options;
  const windowStartMs = Date.now() - FAILURE_ISSUE_WINDOW_MS;
  const since = new Date(windowStartMs).toISOString().slice(0, 19) + "Z";
  const escapedWorkflowId = escapeGitHubSearchPhrase(workflowId);
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:agentic-workflows created:>=${since} ` + `"gh-aw-agentic-workflow:" "workflow_id: ${escapedWorkflowId}" in:body`;
  const perPage = 100;
  for (let page = 1; ; page += 1) {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: perPage,
      page,
    });

    for (const item of searchResult.data.items) {
      if (!isIssueWithinWindow(item.created_at, windowStartMs)) {
        continue;
      }
      let body = typeof item.body === "string" ? item.body : "";
      if (!body) {
        const issueResult = await github.rest.issues.get({
          owner,
          repo,
          issue_number: item.number,
        });
        body = issueResult.data.body || "";
      }

      if (
        isReusableFailureIssue(body, {
          workflowId,
          failureCategories,
        })
      ) {
        return {
          number: item.number,
          html_url: item.html_url,
        };
      }
    }

    if (searchResult.data.items.length < perPage) {
      break;
    }
  }

  return null;
}

/**
 * Count recently created failure issues that include the specified failure category.
 * @param {Object} options - Query options
 * @param {string} options.owner - Repository owner
 * @param {string} options.repo - Repository name
 * @param {string} options.category - Failure category name
 * @returns {Promise<number>} Number of matching issues created within the dedup window
 */
async function countRecentFailureIssuesByCategory(options) {
  const { owner, repo, category } = options;
  const windowStartMs = Date.now() - FAILURE_ISSUE_WINDOW_MS;
  const since = new Date(windowStartMs).toISOString().slice(0, 19) + "Z";
  const escapedCategory = escapeGitHubSearchPhrase(category);
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:agentic-workflows created:>=${since} ` + `"gh-aw-failure-issue:" "failure_categories:" "${escapedCategory}" in:body`;
  const perPage = 100;
  let count = 0;

  for (let page = 1; ; page += 1) {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: perPage,
      page,
    });

    for (const item of searchResult.data.items) {
      if (!isIssueWithinWindow(item.created_at, windowStartMs)) {
        continue;
      }

      let body = typeof item.body === "string" ? item.body : "";
      if (!body) {
        const issueResult = await github.rest.issues.get({
          owner,
          repo,
          issue_number: item.number,
        });
        body = issueResult.data.body || "";
      }

      const marker = parseHTMLCommentMetadata(body, "gh-aw-failure-issue");
      if (!marker) {
        continue;
      }
      const categories = (marker.failure_categories || "")
        .split("|")
        .map(part => part.trim())
        .filter(Boolean);
      if (categories.includes(category)) {
        count += 1;
      }
    }

    if (searchResult.data.items.length < perPage) {
      break;
    }
  }

  return count;
}

/**
 * Find categories that hit the daily new-issue cap.
 * @param {Object} options - Query options
 * @param {string} options.owner - Repository owner
 * @param {string} options.repo - Repository name
 * @param {string[]} options.failureCategories - Categories for the current failure
 * @returns {Promise<Array<{category: string, count: number}>>}
 */
async function getCappedFailureCategories(options) {
  const { owner, repo, failureCategories } = options;
  const uniqueCategories = [...new Set(failureCategories)];
  /** @type {Array<{category: string, count: number}>} */
  const capped = [];

  for (const category of uniqueCategories) {
    const count = await countRecentFailureIssuesByCategory({ owner, repo, category });
    if (count >= FAILURE_ISSUE_CATEGORY_DAILY_CAP) {
      capped.push({ category, count });
    }
  }

  return capped;
}

/**
 * Search for or create the parent issue for all agentic workflow failures
 * @param {number|null} previousParentNumber - Previous parent issue number if creating due to limit
 * @param {string} [ownerOverride] - Repository owner override (from failure-issue-repo config)
 * @param {string} [repoOverride] - Repository name override (from failure-issue-repo config)
 * @param {number} [expiresHours] - Expiration in hours for created parent issue
 * @returns {Promise<{number: number, node_id: string}>} Parent issue number and node ID
 */
async function ensureParentIssue(previousParentNumber = null, ownerOverride, repoOverride, expiresHours = DEFAULT_ACTION_FAILURE_ISSUE_EXPIRES_HOURS) {
  const { owner: contextOwner, repo: contextRepo } = context.repo;
  const owner = ownerOverride || contextOwner;
  const repo = repoOverride || contextRepo;
  const parentTitle = "[aw] Failed runs";
  const parentLabel = "agentic-workflows";

  core.info(`Searching for parent issue: "${parentTitle}"`);

  // Search for existing parent issue
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${parentLabel} in:title "${parentTitle}"`;

  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const existingIssue = searchResult.data.items[0];
      core.info(`Found existing parent issue #${existingIssue.number}: ${existingIssue.html_url}`);

      // Enforce the parent issue's own expiration marker: an expired parent
      // must not keep receiving new sub-issues, mirroring isReusableFailureIssue's
      // handling of individual per-run failure issues.
      let existingBody;
      if (typeof existingIssue.body === "string") {
        existingBody = existingIssue.body;
      } else {
        // The search API response may omit or truncate the body field; fetch the
        // full issue to reliably read the expiration marker.
        try {
          const issueResult = await github.rest.issues.get({
            owner,
            repo,
            issue_number: existingIssue.number,
          });
          existingBody = issueResult.data.body || "";
        } catch (error) {
          core.warning(`Could not fetch parent issue #${existingIssue.number} body: ${getErrorMessage(error)}. Continuing without expiration marker check.`);
          existingBody = "";
        }
      }
      const parentExpirationDate = extractExpirationDate(existingBody);

      if (parentExpirationDate && parentExpirationDate.getTime() <= Date.now()) {
        core.info(`Parent issue #${existingIssue.number} has expired (expired ${parentExpirationDate.toISOString()})`);
        core.info(`Creating a new parent issue (previous parent #${existingIssue.number} has expired)`);

        // Fall through to create a new parent issue, passing the previous parent number
        previousParentNumber = existingIssue.number;
      } else {
        // Check the sub-issue count
        const subIssueCount = await getSubIssueCount(owner, repo, existingIssue.number);

        if (subIssueCount !== null && subIssueCount >= MAX_SUB_ISSUES) {
          core.warning(`Parent issue #${existingIssue.number} has ${subIssueCount} sub-issues (max: ${MAX_SUB_ISSUES})`);
          core.info(`Creating a new parent issue (previous parent #${existingIssue.number} is full)`);

          // Fall through to create a new parent issue, passing the previous parent number
          previousParentNumber = existingIssue.number;
        } else {
          // Parent issue is within limits, return it
          if (subIssueCount !== null) {
            core.info(`Parent issue has ${subIssueCount} sub-issues (within limit of ${MAX_SUB_ISSUES})`);
          }
          return {
            number: existingIssue.number,
            node_id: existingIssue.node_id,
          };
        }
      }
    }
  } catch (error) {
    core.warning(`Error searching for parent issue: ${getErrorMessage(error)}`);
  }

  // Create parent issue if it doesn't exist or if previous one is full
  const creationReason = previousParentNumber ? `creating new parent (previous #${previousParentNumber} reached limit)` : "creating first parent";
  core.info(`No suitable parent issue found, ${creationReason}`);

  let parentBodyContent = `This issue tracks all failures from agentic workflows in this repository. Each failed workflow run creates a sub-issue linked here for organization and easy filtering.`;

  // Add reference to previous parent if this is a continuation
  if (previousParentNumber) {
    parentBodyContent += `

> **Note:** This is a continuation parent issue. The previous parent issue #${previousParentNumber} reached the maximum of ${MAX_SUB_ISSUES} sub-issues.`;
  }

  parentBodyContent += `

### Purpose

This parent issue helps you:
- View all workflow failures in one place by checking the sub-issues below
- Filter out failure issues from your main issue list using \`no:parent-issue\`
- Track the health of your agentic workflows over time

### Sub-Issues

All individual workflow failure issues are linked as sub-issues below. Click on any sub-issue to see details about a specific failure.

### Troubleshooting Failed Workflows

#### Using agentic-workflows Agent (Recommended)

**Agent:** \`agentic-workflows\`  
**Purpose:** Debug and fix workflow failures

**Instructions:**

1. Invoke the agent: Type \`/agent\` in GitHub Copilot Chat and select **agentic-workflows**
2. Provide context: Tell the agent to **debug** the workflow failure
3. Supply the workflow run URL for analysis
4. The agent will:
   - Analyze failure logs
   - Identify root causes
   - Propose specific fixes
   - Validate solutions

#### Using gh aw CLI

You can also debug failures using the \`gh aw\` CLI:

\`\`\`bash
# Download and analyze workflow logs
gh aw logs <workflow-run-url>

# Audit a specific workflow run
gh aw audit <run-id>
\`\`\`

#### Manual Investigation

1. Click on a sub-issue to see the failed workflow details
2. Follow the workflow run link in the issue
3. Review the agent job logs for error messages
4. Check the workflow configuration in your repository

### Resources

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)
- [Troubleshooting Guide](https://github.github.com/gh-aw/troubleshooting/common-issues/)

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.`;

  // Add expiration marker inside the quoted section using helper
  const footer = generateFooterWithExpiration({
    footerText: parentBodyContent,
    expiresHours,
  });
  const parentBody = footer;

  try {
    const newIssue = await github.rest.issues.create({
      owner,
      repo,
      title: parentTitle,
      body: parentBody,
      labels: [parentLabel],
      headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
    });

    core.info(`✓ Created parent issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
    return {
      number: newIssue.data.number,
      node_id: newIssue.data.node_id,
    };
  } catch (error) {
    core.error(`Failed to create parent issue: ${getErrorMessage(error)}`);
    throw error;
  }
}

/**
 * Link an issue as a sub-issue to a parent issue
 * @param {string} parentNodeId - GraphQL node ID of the parent issue
 * @param {string} subIssueNodeId - GraphQL node ID of the sub-issue
 * @param {number} parentNumber - Parent issue number (for logging)
 * @param {number} subIssueNumber - Sub-issue number (for logging)
 */
async function linkSubIssue(parentNodeId, subIssueNodeId, parentNumber, subIssueNumber) {
  core.info(`Linking issue #${subIssueNumber} as sub-issue of #${parentNumber}`);

  try {
    // Use GraphQL to link the sub-issue
    await github.graphql(
      `mutation($parentId: ID!, $subIssueId: ID!) {
        addSubIssue(input: {issueId: $parentId, subIssueId: $subIssueId}) {
          issue {
            id
            number
          }
          subIssue {
            id
            number
          }
        }
      }`,
      {
        parentId: parentNodeId,
        subIssueId: subIssueNodeId,
      }
    );

    core.info(`✓ Successfully linked #${subIssueNumber} as sub-issue of #${parentNumber}`);
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    if (errorMessage.includes("Field 'addSubIssue' doesn't exist") || errorMessage.includes("not yet available")) {
      core.warning(`Sub-issue API not available. Issue #${subIssueNumber} created but not linked to parent.`);
    } else {
      core.warning(`Failed to link sub-issue: ${errorMessage}`);
    }
  }
}

/**
 * Build create_discussion errors context string from error environment variable
 * @param {string} createDiscussionErrors - Newline-separated error strings
 * @returns {string} Formatted error context for display
 */
function buildCreateDiscussionErrorsContext(createDiscussionErrors) {
  if (!createDiscussionErrors) {
    return "";
  }

  let context = buildWarningAlertLine("Create Discussion Failed", "Failed to create one or more discussions.") + "\n**Discussion Errors:**\n";
  const errorLines = createDiscussionErrors.split("\n").filter(line => line.trim());
  for (const errorLine of errorLines) {
    const parts = errorLine.split(":");
    if (parts.length >= 4) {
      // parts[0] is "discussion", parts[1] is index - both unused
      const repo = parts[2];
      const title = parts[3];
      const error = parts.slice(4).join(":"); // Rest is the error message
      context += `- Discussion "${title}" in ${repo}: ${error}\n`;
    }
  }
  context += "\n";
  return context;
}

/**
 * Build a fork context hint string when the repository is a fork.
 * @returns {string} Fork hint string, or empty string if not a fork
 */
function buildForkContextHint() {
  if (context.payload?.repository?.fork) {
    return "\n💡 **This repository is a fork.** If this failure is due to missing API keys or tokens, note that secrets from the parent repository are not inherited. Configure the required secrets directly in your fork's Settings → Secrets and variables → Actions.\n";
  }
  return "";
}

/**
 * Build a context string describing code-push failures for inclusion in failure issue/comment bodies.
 * Manifest file protection refusals are separated from other push failures to give them a dedicated
 * section with clearer remediation instructions.
 * @param {string} codePushFailureErrors - Newline-separated list of "type:error" entries
 * @param {{number: number, html_url: string, head_sha?: string, mergeable?: boolean | null, mergeable_state?: string, updated_at?: string} | null} pullRequest - PR info if available
 * @param {string} [runUrl] - URL of the current workflow run, used to provide patch download instructions
 * @returns {string} Formatted context string, or empty string if no failures
 */
function buildCodePushFailureContext(codePushFailureErrors, pullRequest = null, runUrl = "") {
  if (!codePushFailureErrors) {
    return "";
  }

  // Split errors into protected-file protection refusals, patch size errors, patch apply failures, and other push failures
  const manifestErrors = [];
  const patchSizeErrors = [];
  const patchApplyErrors = [];
  const otherErrors = [];
  const errorLines = codePushFailureErrors.split("\n").filter(line => line.trim());
  for (const errorLine of errorLines) {
    const colonIndex = errorLine.indexOf(":");
    if (colonIndex !== -1) {
      const type = errorLine.substring(0, colonIndex);
      const error = errorLine.substring(colonIndex + 1);
      if (error.includes("manifest files") || error.includes("protected files")) {
        manifestErrors.push({ type, error });
      } else if (error.includes("Patch size") && error.includes("exceeds")) {
        patchSizeErrors.push({ type, error });
      } else if (error.includes("Failed to apply patch")) {
        patchApplyErrors.push({ type, error });
      } else {
        otherErrors.push({ type, error });
      }
    }
  }

  let context = "";

  // Protected file protection section — shown before generic failures
  if (manifestErrors.length > 0) {
    context +=
      "\n**🛡️ Protected Files**: The code push was refused because the patch modifies protected files (package manifests, agent instruction files, or repository security configuration). " +
      "This protection guards against unintended supply chain changes.\n";
    if (pullRequest) {
      context += `\n**Target Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})\n`;
    }
    context += "\n**Blocked Operations:**\n";
    for (const { type, error } of manifestErrors) {
      context += `- \`${type}\`: ${error}\n`;
    }
    // Build a dynamic YAML snippet listing only the safe output types that were actually blocked
    const typeToYamlKey = {
      create_pull_request: "create-pull-request",
      push_to_pull_request_branch: "push-to-pull-request-branch",
    };
    const blockedTypes = [...new Set(manifestErrors.map(e => e.type))];
    let yamlSnippet = "```yaml\nsafe-outputs:\n";
    for (const type of blockedTypes) {
      const yamlKey = typeToYamlKey[type] || type.replace(/_/g, "-");
      yamlSnippet += `  ${yamlKey}:\n    protected-files: fallback-to-issue\n`;
    }
    yamlSnippet += "```\n";
    context += "\n<details>\n<summary>⚙️ Configure <code>protected-files: fallback-to-issue</code></summary>\n\n";
    context += yamlSnippet;
    context += "</details>\n";
  }

  // Patch size exceeded section
  if (patchSizeErrors.length > 0) {
    context += "\n**📦 Patch Size Exceeded**: The code push was rejected because the generated patch is too large.\n";
    if (pullRequest) {
      context += `\n**Target Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})\n`;
    }
    context += "\n**Errors:**\n";
    for (const { type, error } of patchSizeErrors) {
      context += `- \`${type}\`: ${error}\n`;
    }
    // Build a dynamic YAML snippet listing only the safe output types that had patch size errors
    const typeToYamlKey = {
      create_pull_request: "create-pull-request",
      push_to_pull_request_branch: "push-to-pull-request-branch",
    };
    const affectedTypes = [...new Set(patchSizeErrors.map(e => e.type))];
    // Derive the suggested value from the actual limit in the first error message
    const maxAllowedMatch = patchSizeErrors[0]?.error.match(/maximum allowed size \((\d+) KB\)/);
    const maxAllowedKb = maxAllowedMatch ? Number.parseInt(maxAllowedMatch[1], 10) : 4096;
    const suggestedKb = maxAllowedKb * 2;
    let yamlSnippet = "```yaml\nsafe-outputs:\n";
    for (const type of affectedTypes) {
      const yamlKey = typeToYamlKey[type] || type.replace(/_/g, "-");
      yamlSnippet += `  ${yamlKey}:\n    max-patch-size: ${suggestedKb}  # Example: double the default limit (in KB, default: ${maxAllowedKb})\n`;
    }
    yamlSnippet += "```\n";
    context += "\nTo allow larger patches, increase `max-patch-size` in your workflow's front matter (value in KB):\n";
    context += yamlSnippet;

    // Provide download instructions so the user can inspect what the agent generated
    const runId = extractRunId(runUrl);

    context += "\n<details>\n<summary>📥 Download the oversized patch to inspect or apply manually</summary>\n\n";
    if (runId) {
      context += `\`\`\`sh
# Download the patch artifact from the workflow run
gh run download ${runId} -n agent -D /tmp/agent-${runId}

# List available patches
ls /tmp/agent-${runId}/*.patch

# Inspect the patch
cat /tmp/agent-${runId}/YOUR_PATCH_FILE.patch | head -100

# Optionally apply the patch manually on a new branch
git checkout -b aw/manual-apply
git am --3way /tmp/agent-${runId}/YOUR_PATCH_FILE.patch
git push origin aw/manual-apply
gh pr create --head aw/manual-apply
\`\`\`
\nThe patch artifact is available at: [View run and download artifacts](${runUrl})\n`;
    } else {
      context += "Download the patch artifact from the workflow run, then inspect or apply it with `git am --3way <patch-file>`.\n";
    }
    context += "\n</details>\n";
  }

  // Patch apply failure section — shown when the patch could not be applied (e.g. merge conflict)
  if (patchApplyErrors.length > 0) {
    context += "\n**🔀 Patch Apply Failed**: The patch could not be applied to the current state of the repository. " + "This is typically caused by a merge conflict between the agent's changes and recent commits on the target branch.\n";
    if (pullRequest) {
      context += `\n**Target Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})\n`;
    }
    context += "\n**Failed Operations:**\n";
    for (const { type, error } of patchApplyErrors) {
      context += `- \`${type}\`: ${error}\n`;
    }

    // Extract run ID from runUrl for use in the download command
    const runId = extractRunId(runUrl);

    context += "\n<details>\n<summary>📋 Apply the patch manually</summary>\n\n";
    if (runId) {
      context += `\`\`\`sh
# Download the patch artifact from the workflow run
gh run download ${runId} -n agent -D /tmp/agent-${runId}

# List available patches
ls /tmp/agent-${runId}/*.patch

# Create a new branch (adjust as needed)
git checkout -b aw/manual-apply

# Apply the patch (--3way handles cross-repo patches)
git am --3way /tmp/agent-${runId}/YOUR_PATCH_FILE.patch

# If there are conflicts, resolve them and continue (or abort):
# git am --continue
# git am --abort

# Push and create a pull request
git push origin aw/manual-apply
gh pr create --head aw/manual-apply
\`\`\`
\nThe patch artifact is available at: [View run and download artifacts](${runUrl})\n`;
    } else {
      context += "Download the patch artifact from the workflow run, then apply it with `git am --3way <patch-file>`.\n";
    }
    context += "\n</details>\n";
  }

  // Generic code-push failure section
  if (otherErrors.length > 0) {
    context += buildWarningAlertLine("Code Push Failed", "A code push safe output failed, and subsequent safe outputs were cancelled.");
    if (pullRequest) {
      context += `\n**Target Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})`;

      // Add PR state diagnostics
      const workflowSha = process.env.GITHUB_SHA || "";
      const prDetails = [];

      // Check for merge conflicts
      if (pullRequest.mergeable === false) {
        prDetails.push("❌ **Merge conflicts detected** - the PR has conflicts that need resolution");
      } else if (pullRequest.mergeable_state === "dirty") {
        prDetails.push("❌ **PR is in dirty state** - likely has merge conflicts");
      } else if (pullRequest.mergeable_state === "blocked") {
        prDetails.push("**PR is blocked** - required status checks or reviews may be missing");
      } else if (pullRequest.mergeable_state === "behind") {
        prDetails.push("**PR is behind base branch** - may need to be updated");
      }

      // Check if branch was updated since workflow started
      if (workflowSha && pullRequest.head_sha && workflowSha !== pullRequest.head_sha) {
        prDetails.push(`**Branch was updated** - workflow started at \`${workflowSha.substring(0, 7)}\`, PR head is now \`${pullRequest.head_sha.substring(0, 7)}\``);
      }

      // Add SHA info for debugging
      if (pullRequest.head_sha) {
        prDetails.push(`**PR head SHA:** \`${pullRequest.head_sha.substring(0, 7)}\``);
      }
      if (workflowSha) {
        prDetails.push(`**Workflow SHA:** \`${workflowSha.substring(0, 7)}\``);
      }
      if (pullRequest.mergeable_state && pullRequest.mergeable_state !== "unknown") {
        prDetails.push(`**Mergeable state:** ${pullRequest.mergeable_state}`);
      }

      if (prDetails.length > 0) {
        context += "\n\n**PR State at Push Time:**\n";
        for (const detail of prDetails) {
          context += `- ${detail}\n`;
        }
      }
    }
    context += "\n**Code Push Errors:**\n";
    for (const { type, error } of otherErrors) {
      context += renderAllowedFilesError(type, error) || `- \`${type}\`: ${error}\n`;
    }
    context += "\n";
  } else if (manifestErrors.length > 0 || patchSizeErrors.length > 0 || patchApplyErrors.length > 0) {
    // Only manifest, patch size, or patch apply errors — ensure trailing newline
    context += "\n";
  }

  return context;
}

/**
 * Build a context string for push_repo_memory job failures, with a dedicated section for patch size errors.
 * @param {boolean} hasPushRepoMemoryFailure - Whether the push_repo_memory job failed
 * @param {string[]} repoMemoryPatchSizeExceededIDs - Memory IDs that exceeded the patch size limit
 * @param {string} runUrl - URL of the current workflow run
 * @returns {string} Formatted context string, or empty string if no failure
 */
function buildPushRepoMemoryFailureContext(hasPushRepoMemoryFailure, repoMemoryPatchSizeExceededIDs, runUrl) {
  if (!hasPushRepoMemoryFailure) {
    return "";
  }
  if (repoMemoryPatchSizeExceededIDs.length > 0) {
    let context = "\n**📦 Repo-Memory Patch Size Exceeded**: The repo-memory push failed because the memory data is too large.\n";
    context += "\n**Affected memories:** " + repoMemoryPatchSizeExceededIDs.map(id => `\`${id}\``).join(", ") + "\n";
    context += "\nTo allow larger memory snapshots, increase `max-patch-size` in your workflow's `repo-memory` front matter (value in bytes):\n";
    context += "```yaml\nrepo-memory:\n";
    for (const memoryID of repoMemoryPatchSizeExceededIDs) {
      context += `  - id: ${memoryID}\n    max-patch-size: 51200  # Example: 5x the default limit (in bytes, default: 10240, max: 102400)\n`;
    }
    context += "```\n\n";
    return context;
  }
  return (
    buildWarningAlertLine(
      "Repo-Memory Push Failed",
      "The push-repo-memory job failed to write memory back to the repository. This may indicate a permission issue, a configuration error, or a network problem. Check the [workflow run](" + runUrl + ") for details."
    ) + "\n"
  );
}

/**
 * Load missing_data messages from agent output
 * @param {Array<any>} [items] - Optional pre-loaded agent output items. When provided, avoids re-reading the output file.
 * @returns {Array<{data_type: string, reason: string, context?: string, alternatives?: string}>} Array of missing data messages
 */
function loadMissingDataMessages(items) {
  try {
    let resolvedItems = items;
    if (!resolvedItems) {
      const { loadAgentOutput } = require("./load_agent_output.cjs");
      const agentOutputResult = loadAgentOutput();
      if (!agentOutputResult.success || !agentOutputResult.items) {
        return [];
      }
      resolvedItems = agentOutputResult.items;
    }

    // Extract missing_data messages from agent output
    const missingDataMessages = [];
    for (const item of resolvedItems) {
      if (item.type === "missing_data") {
        // Accept items with at least a reason; data_type may be absent for cache-miss signals
        if (item.reason) {
          missingDataMessages.push({
            data_type: item.data_type || "",
            reason: item.reason,
            context: item.context || null,
            alternatives: item.alternatives || null,
          });
        }
      }
    }

    return missingDataMessages;
  } catch (error) {
    core.warning(`Failed to load missing_data messages: ${getErrorMessage(error)}`);
    return [];
  }
}

/**
 * Resolve whether any cache-memory restore step matched a cache entry.
 * Reads per-cache restore outputs propagated via GH_AW_CACHE_MEMORY_RESTORE_<index>_* env vars.
 * @returns {boolean}
 */
function resolveCacheMemoryRestored() {
  for (const [key, value] of Object.entries(process.env)) {
    if (!key.startsWith("GH_AW_CACHE_MEMORY_RESTORE_")) {
      continue;
    }
    if (key.endsWith("_MATCHED_KEY") && String(value || "").trim() !== "") {
      return true;
    }
    if (
      key.endsWith("_CACHE_HIT") &&
      String(value || "")
        .trim()
        .toLowerCase() === "true"
    ) {
      return true;
    }
  }
  return false;
}

/**
 * Build missing_data context string for display in failure issues/comments.
 * When cache-memory is enabled, a restore matched, and a cache_miss is detected,
 * appends a configuration-problem warning to the context.
 * @param {boolean} cacheMemoryEnabled - Whether cache-memory is configured for this workflow
 * @param {boolean} cacheMemoryRestored - Whether cache restore matched an existing cache key in this run
 * @param {Array<any>} [items] - Optional pre-loaded agent output items. When provided, avoids re-reading the output file.
 * @returns {string} Formatted missing data context
 */
function buildMissingDataContext(cacheMemoryEnabled, cacheMemoryRestored, items) {
  const missingDataMessages = loadMissingDataMessages(items);

  if (missingDataMessages.length === 0) {
    return "";
  }

  core.info(`Found ${missingDataMessages.length} missing_data message(s)`);

  // Detect cache_miss: if cache-memory restore matched and the agent reported a cache miss,
  // this indicates the prompt is referencing an incorrect file path within the cache directory.
  const hasCacheMiss = missingDataMessages.some(m => m.reason === "cache_memory_miss");

  // When cache-memory is configured and cache_miss is present, avoid repeating the same
  // signal in the generic "Missing Data" section. Keep the specialised cache warning below.
  const shouldShowCacheWarning = cacheMemoryEnabled && cacheMemoryRestored && hasCacheMiss;
  const displayableMissingData = shouldShowCacheWarning ? missingDataMessages.filter(m => m.reason !== "cache_memory_miss") : missingDataMessages;

  let context = "";
  if (displayableMissingData.length > 0) {
    const formattedList = formatMissingData(displayableMissingData);
    context += buildWarningAlertLine("Missing Data Reported", "The agent reported missing data during execution.") + "\n**Missing Data:**\n";
    context += formattedList;
    context += "\n\n";
  }

  if (shouldShowCacheWarning) {
    core.info("Cache-miss detected after a successful cache restore — likely a configuration problem");
    const templatePath = getPromptPath("cache_memory_miss.md");
    context += "\n" + renderTemplateFromFile(templatePath, {}) + "\n";
  }

  return context;
}

/**
 * Extract denied command entries from a missing_tool alternatives string.
 * Handles batched command text in the form:
 * ".... Denied commands: cmd1 | cmd2 | cmd3"
 * while preserving internal pipes inside tool call parentheses.
 * @param {string | null | undefined} alternatives
 * @returns {string[]}
 */
function extractDeniedCommandsFromAlternatives(alternatives) {
  if (!alternatives || typeof alternatives !== "string") {
    return [];
  }

  const marker = "Denied commands:";
  const markerIndex = alternatives.indexOf(marker);
  if (markerIndex < 0) {
    return [];
  }

  const deniedSection = alternatives.slice(markerIndex + marker.length).trim();
  if (!deniedSection) {
    return [];
  }

  const commands = [];
  let current = "";
  let depth = 0;

  for (let i = 0; i < deniedSection.length; i++) {
    const ch = deniedSection[i];
    if (ch === "(") {
      depth += 1;
      current += ch;
      continue;
    }
    if (ch === ")") {
      depth = Math.max(0, depth - 1);
      current += ch;
      continue;
    }

    if (depth === 0 && deniedSection.slice(i, i + 3) === " | ") {
      const value = current.trim();
      if (value && !/^\.\.\. and \d+ more$/i.test(value)) {
        commands.push(value);
      }
      current = "";
      i += 2;
      continue;
    }

    current += ch;
  }

  const last = current.trim();
  if (last && !/^\.\.\. and \d+ more$/i.test(last)) {
    commands.push(last);
  }

  return [...new Set(commands)];
}

/**
 * Normalize denied command strings for permission-context deduplication.
 * `read(path)` grants are path-agnostic once enabled, so collapse all reads.
 * Shell heredoc programs (e.g. `shell(python3 << 'EOF'\n...\nEOF)`) are collapsed
 * to `shell(<program> ...)` so the display stays single-line and actionable.
 * @param {string} command
 * @returns {string}
 */
function normalizeDeniedPermissionCommand(command) {
  const trimmed = typeof command === "string" ? command.trim() : "";
  if (!trimmed) return "";
  // Strip optional "permission denied: " prefix present in tool_denials_exceeded reason strings.
  const cmd = trimmed.replace(/^permission denied:\s*/i, "");
  const readMatch = cmd.match(/^read\s*\((.*)\)$/i);
  if (readMatch && !/[\r\n]/.test(readMatch[1] || "")) {
    return "read(...)";
  }
  // Normalize shell heredoc programs: shell(python3 << 'EOF'\n...\nEOF) → shell(python3 ...)
  // Matches any shell(...) invocation that opens a heredoc with << so the multi-line
  // program body is replaced by an ellipsis, keeping the display single-line.
  // Only the opening line needs to match — we don't need to parse the body or closing delimiter.
  // The \w+ captures common heredoc markers (e.g. EOF, EOL, HEREDOC, PYTHON).
  const shellHeredocMatch = cmd.match(/^shell\s*\(\s*(\S+)\s+<<\s*['"]?\w+['"]?[\r\n]/);
  if (shellHeredocMatch) {
    return `shell(${shellHeredocMatch[1]} ...)`;
  }
  return cmd;
}

/**
 * Collapse tool call details to a compact single-line preview.
 * @param {string} value
 * @param {number} [maxLen]
 * @returns {string}
 */
function normalizeToolCallPreview(value, maxLen = 120) {
  const singleLine = String(value || "")
    .replace(/`/g, "'")
    .replace(/\s+/g, " ")
    .trim();
  if (!singleLine) return "";
  if (singleLine.length <= maxLen) return singleLine;
  return `${singleLine.slice(0, maxLen - ELLIPSIS_LENGTH)}${ELLIPSIS}`;
}

/**
 * Best-effort extraction of a shell command preview from a tool.execution_start payload.
 * @param {Record<string, any>} data
 * @returns {string}
 */
function extractShellCommandPreview(data) {
  return normalizeToolCallPreview(extractShellCommandFromToolData(data));
}

/**
 * Format a compact display value for a recent tool call entry.
 * @param {string} toolName
 * @param {string} mcpServerName
 * @param {Record<string, any>} data
 * @returns {string}
 */
function formatRecentToolCall(toolName, mcpServerName, data) {
  const base = mcpServerName ? `${mcpServerName}.${toolName}` : toolName;
  const normalizedToolName = typeof toolName === "string" ? toolName.toLowerCase() : "";
  if (!RECENT_TOOL_CALLS_WITH_COMMAND_PREVIEW.has(normalizedToolName)) {
    return base;
  }
  const commandPreview = extractShellCommandPreview(data);
  return commandPreview ? `${base}(${commandPreview})` : base;
}

/**
 * Load missing_tool messages from agent output.
 * Returns an empty array when the output file doesn't exist, cannot be parsed, or has no missing_tool items.
 * @param {Array<any>} [items] - Optional pre-loaded agent output items. When provided, avoids re-reading the output file.
 * @returns {Array<{tool: string|null, reason: string, alternatives?: string|null, denied_commands: Array<string>}>} Array of missing tool messages
 */
function loadMissingToolMessages(items) {
  try {
    let resolvedItems = items;
    if (!resolvedItems) {
      const { loadAgentOutput } = require("./load_agent_output.cjs");
      const agentOutputResult = loadAgentOutput();
      if (!agentOutputResult.success || !agentOutputResult.items) {
        return [];
      }
      resolvedItems = agentOutputResult.items;
    }

    const missingToolMessages = [];
    for (const item of resolvedItems) {
      if (item.type === "missing_tool") {
        if (item.reason) {
          const deniedCommandsFromItem = Array.isArray(item.denied_commands) ? item.denied_commands.filter(cmd => typeof cmd === "string" && cmd.trim()) : [];
          const deniedCommands = deniedCommandsFromItem.length > 0 ? deniedCommandsFromItem : extractDeniedCommandsFromAlternatives(item.alternatives);
          missingToolMessages.push({
            tool: item.tool || null,
            reason: item.reason,
            alternatives: item.alternatives || null,
            denied_commands: deniedCommands,
          });
        }
      }
    }

    return missingToolMessages;
  } catch (error) {
    core.warning(`Failed to load missing_tool messages: ${getErrorMessage(error)}`);
    return [];
  }
}

/**
 * Determine whether a missing_tool message represents permission denial.
 * @param {{tool: string|null, denied_commands: Array<string>}} message
 * @returns {boolean}
 */
function isPermissionDeniedMissingTool(message) {
  return message.tool === "tool/permission" && Array.isArray(message.denied_commands) && message.denied_commands.length > 0;
}

/**
 * Build missing_tool context string for display in failure issues/comments.
 * @param {Array<any>} [items] - Optional pre-loaded agent output items. When provided, avoids re-reading the output file.
 * @returns {string} Formatted missing tool context
 */
function buildMissingToolContext(items) {
  const missingToolMessages = loadMissingToolMessages(items).filter(message => !isPermissionDeniedMissingTool(message));

  if (missingToolMessages.length === 0) {
    return "";
  }

  core.info(`Found ${missingToolMessages.length} missing_tool message(s)`);

  const formattedList = formatMissingTools(missingToolMessages);

  let context = buildWarningAlertLine("Missing Tools Reported", "The agent reported missing tools during execution.") + "\n**Missing Tools:**\n";
  context += formattedList;
  context += "\n\n";

  return context;
}

/**
 * Build permission denied context string when the agent reported numerous permission denied errors.
 * Reads denied_commands from any missing_tool message with tool === "tool/permission".
 * @param {Array<any>} [items] - Optional pre-loaded agent output items.
 * @param {string} [workflowId] - Workflow ID for the suggested fix prompt placeholder.
 * @returns {string} Formatted permission denied context, or empty string if not applicable.
 */
function buildPermissionDeniedContext(items, workflowId) {
  const missingToolMessages = loadMissingToolMessages(items);
  const permissionItems = missingToolMessages.filter(isPermissionDeniedMissingTool);

  if (permissionItems.length === 0) {
    return "";
  }

  // Aggregate denied commands across all permission items and deduplicate.
  const allDenied = new Set();
  for (const item of permissionItems) {
    for (const cmd of item.denied_commands) {
      const normalized = normalizeDeniedPermissionCommand(cmd);
      if (normalized) allDenied.add(normalized);
    }
  }

  if (allDenied.size === 0) {
    return "";
  }

  core.info(`Found ${allDenied.size} denied command(s) in permission_denied context`);

  const deniedArray = [...allDenied];
  const deniedCommandsList = deniedArray.map(cmd => `- \`${cmd}\``).join("\n");
  const deniedCommandsInline = deniedArray.map(cmd => `\`${cmd}\``).join(", ");
  const deniedCount = String(deniedArray.length);

  const rendered = renderPromptTemplate("permission_denied_context.md", {
    denied_count: deniedCount,
    denied_commands_list: deniedCommandsList,
    denied_commands_inline: deniedCommandsInline,
    workflow_id: workflowId || "the workflow",
  });
  return "\n" + rendered;
}

/**
 * Load max-tool-denials guard events from Copilot SDK session events.jsonl files.
 * @returns {Array<{denialCount: number, threshold: number, reason: string, recentToolCalls: Array<string>, timestamp: string}>}
 */
function loadToolDenialsExceededEvents() {
  try {
    if (!fs.existsSync(COPILOT_SESSION_STATE_DIR)) {
      return [];
    }

    const events = [];
    const sessionDirs = fs.readdirSync(COPILOT_SESSION_STATE_DIR, { withFileTypes: true });
    for (const entry of sessionDirs) {
      if (!entry.isDirectory()) continue;
      const eventsPath = path.join(COPILOT_SESSION_STATE_DIR, entry.name, "events.jsonl");
      if (!fs.existsSync(eventsPath)) continue;
      const content = fs.readFileSync(eventsPath, "utf8");
      const lines = content.split("\n");
      /** @type {Array<string>} */
      const recentToolCalls = [];
      for (const rawLine of lines) {
        const line = rawLine.trim();
        if (!line) continue;
        try {
          const parsed = JSON.parse(line);
          if (parsed.type === "tool.execution_start" && parsed.data && typeof parsed.data === "object") {
            const toolName = typeof parsed.data.toolName === "string" ? parsed.data.toolName.trim() : "";
            if (toolName) {
              const mcpServerName = typeof parsed.data.mcpServerName === "string" ? parsed.data.mcpServerName.trim() : "";
              recentToolCalls.push(formatRecentToolCall(toolName, mcpServerName, parsed.data));
              if (recentToolCalls.length > 5) recentToolCalls.shift();
            }
            continue;
          }
          if (parsed.type !== "guard.tool_denials_exceeded" || !parsed.data || typeof parsed.data !== "object") {
            continue;
          }
          const denialCount = Number.parseInt(String(parsed.data.denialCount), 10);
          const threshold = Number.parseInt(String(parsed.data.threshold), 10);
          if (!Number.isFinite(denialCount) || !Number.isFinite(threshold)) {
            continue;
          }
          events.push({
            denialCount,
            threshold,
            reason: typeof parsed.data.reason === "string" ? parsed.data.reason.trim() : "",
            recentToolCalls: recentToolCalls.slice(),
            timestamp: typeof parsed.timestamp === "string" ? parsed.timestamp : "",
          });
        } catch {
          // Malformed line — ignored.
        }
      }
    }
    return events;
  } catch (error) {
    core.warning(`Failed to load tool-denials-exceeded events: ${getErrorMessage(error)}`);
    return [];
  }
}

/**
 * Build context for max-tool-denials guardrail failures from Copilot SDK events.
 * @param {Array<{denialCount: number, threshold: number, reason: string, recentToolCalls?: Array<string>, timestamp?: string}>} events
 * @param {string} [workflowId]
 * @returns {string}
 */
function buildToolDenialsExceededContext(events, workflowId) {
  if (!Array.isArray(events) || events.length === 0) {
    return "";
  }
  // Select the event with the newest ISO timestamp so the correct lead-up context is
  // shown when guard events arrive from multiple session directories in arbitrary order.
  const latestEvent = events.reduce((best, ev) => {
    const evTs = typeof ev.timestamp === "string" ? ev.timestamp : "";
    const bestTs = typeof best.timestamp === "string" ? best.timestamp : "";
    return evTs > bestTs ? ev : best;
  });
  const denialCount = String(latestEvent.denialCount);
  const threshold = String(latestEvent.threshold);
  const reason = latestEvent.reason || "permission denied by workflow tool permissions";

  // Normalize the reason for display: multi-line programs (e.g. Python 3 heredocs) are
  // collapsed to a single-line summary so the issue body renders cleanly.
  const normalizedReason = normalizeDeniedPermissionCommand(reason);
  const recentToolCallsList = Array.isArray(latestEvent.recentToolCalls) && latestEvent.recentToolCalls.length > 0 ? latestEvent.recentToolCalls.map(toolCall => `- \`${toolCall}\``).join("\n") : "- _No tool calls captured_";

  const templatePath = getPromptPath("tool_denials_exceeded_context.md");
  let template;
  try {
    template = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return (
    "\n" +
    renderTemplate(template, {
      denial_count: denialCount,
      threshold,
      reason: normalizedReason,
      recent_tool_calls_list: recentToolCallsList,
      workflow_id: workflowId || "the workflow",
    })
  );
}

/**
 * Load report_incomplete messages from agent output
 * @param {Array<any>} [items] - Optional pre-loaded agent output items. When provided, avoids re-reading the output file.
 * @returns {Array<{reason: string, details?: string}>} Array of report_incomplete messages
 */
function loadReportIncompleteMessages(items) {
  try {
    let resolvedItems = items;
    if (!resolvedItems) {
      const { loadAgentOutput } = require("./load_agent_output.cjs");
      const agentOutputResult = loadAgentOutput();
      if (!agentOutputResult.success || !agentOutputResult.items) {
        return [];
      }
      resolvedItems = agentOutputResult.items;
    }

    const messages = [];
    for (const item of resolvedItems) {
      if (item.type === "report_incomplete" && item.reason) {
        messages.push({
          reason: item.reason,
          details: item.details || null,
        });
      }
    }

    return messages;
  } catch (error) {
    core.warning(`Failed to load report_incomplete messages: ${getErrorMessage(error)}`);
    return [];
  }
}

const DIAGNOSTIC_AGENT_OUTPUT_TYPES = new Set(["noop", "missing_tool", "missing_data", "report_incomplete"]);
const TASK_COMPLETE_REGISTRATION_SIGNALS = ["not registering", "not registered", "not recognizing", "not recognized", "recognition issue", "not yet marked", "haven't marked", "tool calls are not registering"];
const TASK_COMPLETE_COMPLETION_SIGNALS = ["completed successfully", "safe-output", "safe output", "no remaining work"];
const TASK_COMPLETE_COMPLETION_REGEXPS = [/\banalysis steps? completed\b/, /\ball steps completed\b/];

/**
 * Determine whether agent output contains at least one real task-level item.
 * @param {Array<any> | undefined} items
 * @returns {boolean}
 */
function hasTaskLevelAgentOutput(items) {
  if (!Array.isArray(items)) {
    return false;
  }
  return items.some(item => item && typeof item.type === "string" && !DIAGNOSTIC_AGENT_OUTPUT_TYPES.has(item.type));
}

/**
 * Detect a spurious report_incomplete caused only by task_complete registration/recognition
 * trouble after the agent already finished the real task work.
 * @param {{reason?: string, details?: string} | undefined} item
 * @returns {boolean}
 */
function isTaskCompleteRegistrationIssue(item) {
  if (!item) {
    return false;
  }
  const text = `${item.reason || ""}\n${item.details || ""}`.toLowerCase();
  if (!text.includes("task_complete")) {
    return false;
  }
  const hasRegistrationSignal = TASK_COMPLETE_REGISTRATION_SIGNALS.some(signal => text.includes(signal));
  const hasCompletionSignal = TASK_COMPLETE_COMPLETION_SIGNALS.some(signal => text.includes(signal)) || TASK_COMPLETE_COMPLETION_REGEXPS.some(regexp => regexp.test(text));
  return hasRegistrationSignal && hasCompletionSignal;
}

/**
 * Build report_incomplete context string for display in failure issues/comments.
 * This surfaces the agent's structured incompletion signal so maintainers can
 * distinguish a tool-failure report from a real task outcome.
 * @param {Array<any>} [items] - Optional pre-loaded agent output items. When provided, avoids re-reading the output file.
 * @returns {string} Formatted report_incomplete context
 */
function buildReportIncompleteContext(items) {
  const messages = loadReportIncompleteMessages(items);

  if (messages.length === 0) {
    return "";
  }

  core.info(`Found ${messages.length} report_incomplete signal(s)`);

  let context = buildWarningAlertLine("Task Could Not Be Completed", "The agent reported that the task could not be performed due to an infrastructure or tool failure.") + "\n**Reasons:**\n";
  for (const msg of messages) {
    context += `- ${msg.reason}\n`;
    if (msg.details) {
      context += `  \n  ${msg.details}\n`;
    }
  }
  context +=
    "\nThis is a structured incompletion signal (`report_incomplete`), not a real task outcome. Any other safe outputs emitted alongside this signal (e.g., comments) describe the failure state, not a completed review or action.\n\n";

  return context;
}

/**
 * Build a context string with a frontmatter hint when the agent timed out.
 * @param {boolean} isTimedOut - Whether the agent job timed out
 * @param {string} timeoutMinutes - Current timeout value in minutes (e.g. "20")
 * @returns {string} Formatted timeout context string, or empty string if not timed out
 */
function buildTimeoutContext(isTimedOut, timeoutMinutes) {
  if (!isTimedOut) {
    return "";
  }

  const currentMinutes = parseInt(timeoutMinutes || "20", 10);
  const suggestedMinutes = currentMinutes + 10;

  const templatePath = getPromptPath("agent_timeout.md");
  return "\n" + renderTemplateFromFile(templatePath, { current_minutes: currentMinutes, suggested_minutes: suggestedMinutes });
}

/**
 * Determine whether engine-failure context should be included.
 * Timeout outcomes should rely on dedicated timeout messaging instead.
 * @param {string} agentConclusion
 * @param {boolean} hasToolDenialsExceeded
 * @param {boolean} isTimedOut
 * @param {boolean} hasMissingModelPricingError
 * @param {boolean} hasShellExpansionGuardRejected
 * @returns {boolean}
 */
function shouldBuildEngineFailureContext(agentConclusion, hasToolDenialsExceeded, isTimedOut, hasMissingModelPricingError = false, hasShellExpansionGuardRejected = false) {
  return agentConclusion === "failure" && !hasToolDenialsExceeded && !isTimedOut && !hasMissingModelPricingError && !hasShellExpansionGuardRejected;
}

/**
 * Determine whether issue create/update failed due to token permission limits.
 * @param {unknown} error
 * @returns {boolean}
 */
function isIssueWritePermissionError(error) {
  /** @type {{status?: unknown} | null} */
  const typedError = error && typeof error === "object" ? error : null;
  const status = Number(typedError?.status);
  const message = getErrorMessage(error).toLowerCase();
  return status === 403 && (message.includes("resource not accessible by integration") || message.includes("resource not accessible by personal access token") || message.includes("insufficient permissions"));
}

/**
 * Publish the failure issue as step outputs so later conclusion steps can act on it.
 * @param {{number: number, html_url: string}} issue
 * @returns {void}
 */
function setFailureIssueOutputs(issue) {
  core.setOutput("failure_issue_number", String(issue.number));
  core.setOutput("failure_issue_url", issue.html_url);
}

/**
 * Build a context string when the Copilot CLI failed due to the token lacking inference access.
 * @param {boolean} hasInferenceAccessError - Whether an inference access error was detected
 * @returns {string} Formatted context string, or empty string if no error
 */
function buildInferenceAccessErrorContext(hasInferenceAccessError) {
  if (!hasInferenceAccessError) {
    return "";
  }

  const templatePath = getPromptPath("inference_access_error.md");
  let template;
  try {
    template = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return "\n" + template;
}

/**
 * Build a context string when MCP servers were blocked by enterprise/organization policy.
 * This is a persistent configuration error — retrying will not help.
 * @param {boolean} hasMCPPolicyError - Whether an MCP policy error was detected
 * @returns {string} Formatted context string, or empty string if no error
 */
function buildMCPPolicyErrorContext(hasMCPPolicyError) {
  if (!hasMCPPolicyError) {
    return "";
  }

  return "\n" + renderPromptTemplate("mcp_policy_error.md");
}

/**
 * Build a context string when the requested model is not supported for the subscription tier.
 * This is a persistent configuration error — retrying will not help.
 * @param {boolean} hasModelNotSupportedError - Whether a model-not-supported error was detected
 * @returns {string} Formatted context string, or empty string if no error
 */
function buildModelNotSupportedErrorContext(hasModelNotSupportedError) {
  if (!hasModelNotSupportedError) {
    return "";
  }

  return "\n" + renderPromptTemplate("model_not_supported_error.md");
}

/**
 * Build a context string when the agentic engine returned a generic HTTP 400 Bad Request response.
 * @param {boolean} hasHTTP400ResponseError - Whether a generic HTTP 400 response error was detected
 * @returns {string} Formatted context string, or empty string if no error
 */
function buildHTTP400ResponseErrorContext(hasHTTP400ResponseError) {
  if (!hasHTTP400ResponseError) {
    return "";
  }

  return "\n" + renderPromptTemplate("http_400_response_error.md");
}

/**
 * Builds the unknown_model_ai_credits failure context block for templates.
 * @param {boolean} hasUnknownModelAICreditsError
 * @returns {string}
 */
function buildUnknownModelAICreditsContext(hasUnknownModelAICreditsError) {
  if (!hasUnknownModelAICreditsError) {
    return "";
  }

  return "\n" + renderPromptTemplate("unknown_model_ai_credits.md");
}

/**
 * Fetch the models.dev pricing catalog and look up per-million-token pricing for a model.
 * Returns null when the catalog is unavailable, the model is not found, or pricing is missing.
 * @param {string} modelName - The model name to look up (e.g. "claude-opus-5")
 * @param {string} [providerName] - Preferred provider key (e.g. "anthropic")
 * @returns {Promise<{input: number, output: number, cacheRead?: number, cacheWrite?: number}|null>}
 */
async function fetchModelPricingFromModelsDev(modelName, providerName = "") {
  if (!modelName) return null;
  const url = "https://models.dev/catalog.json";
  const normalizedModel = modelName.toLowerCase().replace(/[._]/g, "-");
  const normalizedProvider = (providerName || "").trim().toLowerCase();
  const MAX_MODELS_DEV_RESPONSE_BYTES = 2 * 1024 * 1024;
  /** @type {string} */
  const rawJson = await new Promise((resolve, reject) => {
    const req = https.get(url, res => {
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`models.dev returned HTTP ${res.statusCode}`));
        return;
      }
      let receivedBytes = 0;
      const chunks = [];
      res.on("data", chunk => {
        receivedBytes += chunk.length;
        if (receivedBytes > MAX_MODELS_DEV_RESPONSE_BYTES) {
          req.destroy(new Error(`models.dev response exceeded ${MAX_MODELS_DEV_RESPONSE_BYTES} bytes`));
          return;
        }
        chunks.push(chunk);
      });
      res.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
      res.on("error", reject);
    });
    const hardDeadline = setTimeout(() => req.destroy(new Error("models.dev request timed out")), 5000);
    req.on("close", () => clearTimeout(hardDeadline));
    req.on("error", reject);
  });

  /** @type {any} */
  let catalog;
  try {
    catalog = JSON.parse(rawJson);
  } catch {
    throw new Error("models.dev returned non-JSON response");
  }
  const providers = catalog?.providers ?? {};

  const providerEntries = Object.entries(providers);
  const lookupOrder = normalizedProvider
    ? [...providerEntries.filter(([provider]) => provider.toLowerCase() === normalizedProvider), ...providerEntries.filter(([provider]) => provider.toLowerCase() !== normalizedProvider)]
    : providerEntries;

  for (const [, providerData] of lookupOrder) {
    const models = /** @type {any} */ providerData?.models ?? {};
    for (const [mName, mData] of Object.entries(models)) {
      const normalized = mName.toLowerCase().replace(/[._]/g, "-");
      if (normalized === normalizedModel) {
        const cost = /** @type {any} */ mData?.cost ?? {};
        const inputPerMillion = typeof cost.input === "number" ? cost.input : null;
        const outputPerMillion = typeof cost.output === "number" ? cost.output : null;
        if (inputPerMillion === null || outputPerMillion === null) return null;
        /** @type {{input: number, output: number, cacheRead?: number, cacheWrite?: number}} */
        const result = { input: inputPerMillion, output: outputPerMillion };
        if (typeof cost.cache_read === "number") result.cacheRead = cost.cache_read;
        if (typeof cost.cache_write === "number") result.cacheWrite = cost.cache_write;
        return result;
      }
    }
  }
  return null;
}

/**
 * Format a per-million-token price as a YAML-safe per-token scientific notation string.
 * @param {number} perMillionTokens
 * @returns {string}
 */
function formatPerTokenPrice(perMillionTokens) {
  const perToken = perMillionTokens / 1_000_000;
  return perToken.toExponential().replace(/e\+?(-?)0*(\d+)$/, "e$1$2");
}

/**
 * Infer the frontmatter provider key from the engine ID.
 * @param {string} engineId
 * @returns {string}
 */
function inferProviderKeyFromEngineId(engineId) {
  switch ((engineId || "").toLowerCase()) {
    case "claude":
      return "anthropic";
    case "codex":
      return "openai";
    case "copilot":
      return "github-copilot";
    default:
      return "github-copilot";
  }
}

/**
 * @param {string} value
 * @returns {string}
 */
function quoteYAMLKey(value) {
  return `'${String(value).replace(/'/g, "''")}'`;
}

/**
 * Build a frontmatter YAML pricing snippet for the missing model.
 * Returns null when pricing data is unavailable.
 * @param {string} modelName
 * @param {string} engineId
 * @param {{input: number, output: number, cacheRead?: number, cacheWrite?: number}|null} pricing Per-million-token values from models.dev
 * @returns {string|null}
 */
function buildModelPricingFrontmatterSnippet(modelName, engineId, pricing, isPlaceholderPricing = false) {
  if (!modelName || !pricing) return null;
  const provider = inferProviderKeyFromEngineId(engineId);
  const inputStr = formatPerTokenPrice(pricing.input);
  const outputStr = formatPerTokenPrice(pricing.output);
  const quotedModelName = quoteYAMLKey(modelName);
  let costBlock = "";
  if (isPlaceholderPricing) {
    costBlock += "            # Placeholder values — replace with actual pricing for this model\n";
  }
  costBlock += `            input: "${inputStr}"      # $${pricing.input.toFixed(2)} per million input tokens\n`;
  costBlock += `            output: "${outputStr}"     # $${pricing.output.toFixed(2)} per million output tokens\n`;
  if (pricing.cacheRead !== undefined) {
    costBlock += `            cache_read: "${formatPerTokenPrice(pricing.cacheRead)}"  # $${pricing.cacheRead.toFixed(2)} per million cache-read tokens\n`;
  }
  if (pricing.cacheWrite !== undefined) {
    costBlock += `            cache_write: "${formatPerTokenPrice(pricing.cacheWrite)}" # $${pricing.cacheWrite.toFixed(2)} per million cache-write tokens\n`;
  }
  return `\`\`\`yaml
models:
  providers:
    ${provider}:
      models:
        ${quotedModelName}:
          cost:
${costBlock.trimEnd()}
\`\`\``;
}

/**
 * Build a frontmatter YAML pricing skeleton for manual completion when live pricing is unavailable.
 * @param {string} modelName
 * @param {string} engineId
 * @returns {string|null}
 */
function buildManualModelPricingFrontmatterSnippet(modelName, engineId) {
  return buildModelPricingFrontmatterSnippet(modelName, engineId, { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, true);
}

/**
 * Builds the missing_model_pricing failure context block for templates.
 * Fetches current pricing from models.dev and includes a ready-to-use frontmatter snippet.
 * @param {boolean} hasMissingModelPricingError
 * @param {string} modelName
 * @param {string} engineId
 * @returns {Promise<string>}
 */
async function buildMissingModelPricingContext(hasMissingModelPricingError, modelName, engineId) {
  if (!hasMissingModelPricingError) {
    return "";
  }

  const resolvedModelName = modelName || "unknown";
  let pricingSnippet = buildManualModelPricingFrontmatterSnippet(resolvedModelName, engineId) || "";
  if (modelName) {
    try {
      const pricing = await fetchModelPricingFromModelsDev(modelName, inferProviderKeyFromEngineId(engineId));
      if (pricing) {
        const snippet = buildModelPricingFrontmatterSnippet(resolvedModelName, engineId, pricing);
        if (snippet) {
          pricingSnippet = snippet;
        }
      }
    } catch (err) {
      core.info(`Could not fetch pricing from models.dev for model "${modelName}": ${getErrorMessage(err)}`);
    }
  }

  return (
    "\n" +
    renderPromptTemplate("missing_model_pricing.md", {
      model_name: resolvedModelName,
      model_name_yaml_key: quoteYAMLKey(resolvedModelName),
      pricing_snippet: pricingSnippet,
      has_pricing_snippet: pricingSnippet ? "true" : "",
    })
  );
}

/**
 * Detect HTTP 429/rate-limit engine failures in text payloads.
 * @param {string} content
 * @returns {boolean}
 */
function hasEngineRateLimit429Signal(content) {
  if (!content) {
    return false;
  }
  return ENGINE_RATE_LIMIT_429_RE.test(content);
}

/**
 * Detect max-runs guardrail failures in text payloads.
 * Returns true when content includes either the `max_runs_exceeded` error type
 * or the "Maximum LLM invocations exceeded" message fragment.
 * @param {string|null|undefined} content
 * @returns {boolean}
 */
function hasEngineMaxRunsExceededSignal(content) {
  if (!content) {
    return false;
  }
  return ENGINE_MAX_RUNS_EXCEEDED_RE.test(content);
}

/**
 * Detect HTTP 429/rate-limit engine failures from OTLP JSONL mirror payloads.
 * @param {string} [otelJsonlPathOverride]
 * @returns {boolean}
 */
function hasEngineRateLimit429InOTELMirror(otelJsonlPathOverride) {
  const otelJsonlPath = otelJsonlPathOverride || process.env.GH_AW_OTEL_JSONL_PATH || DEFAULT_OTEL_JSONL_PATH;
  try {
    if (!fs.existsSync(otelJsonlPath)) {
      return false;
    }
    const content = fs.readFileSync(otelJsonlPath, "utf8");
    return hasEngineRateLimit429Signal(content);
  } catch {
    return false;
  }
}

/**
 * Build dedicated context for engine 429/rate-limit failures.
 * @param {string} engineLabel
 * @returns {string}
 */
function buildEngineRateLimit429Context(engineLabel) {
  const normalizedEngineLabel = engineLabel.trim() || "AI";
  return "\n" + renderPromptTemplate("engine_rate_limit_429.md", { engine_label: normalizedEngineLabel });
}

/**
 * Build dedicated context for max-runs guardrail failures.
 * Renders the max-runs-exceeded prompt template with the active engine label.
 * @param {string} [engineLabel]
 * @returns {string}
 */
function buildEngineMaxRunsExceededContext(engineLabel) {
  const normalizedEngineLabel = (typeof engineLabel === "string" ? engineLabel : "").trim() || "AI";
  return "\n" + renderPromptTemplate("engine_max_runs_exceeded.md", { engine_label: normalizedEngineLabel });
}

/**
 * Detect max consecutive cache misses failures in text payloads.
 * Returns true when content includes either the `max_cache_misses_exceeded` error type
 * or the "Maximum consecutive cache misses exceeded" message fragment.
 * Uses the shared MAX_CACHE_MISSES_EXCEEDED_PATTERN from detect_agent_errors for
 * consistency with the unified detection mechanism.
 * @param {string|null|undefined} content
 * @returns {boolean}
 */
function hasEngineMaxCacheMissesExceededSignal(content) {
  if (!content) {
    return false;
  }
  return MAX_CACHE_MISSES_EXCEEDED_PATTERN.test(content);
}

/**
 * Detect sandbox shell-expansion guard rejections in text payloads.
 * @param {string|null|undefined} content
 * @returns {boolean}
 */
function hasShellExpansionGuardRejectedSignal(content) {
  if (!content) {
    return false;
  }
  return SHELL_EXPANSION_GUARD_REJECTED_PATTERN.test(content);
}

/**
 * Build dedicated context for sandbox shell-expansion guard rejections.
 * @param {boolean} hasShellExpansionGuardRejected
 * @returns {string}
 */
function buildShellExpansionGuardRejectedContext(hasShellExpansionGuardRejected) {
  if (!hasShellExpansionGuardRejected) {
    return "";
  }
  return "\n" + renderPromptTemplate("shell_expansion_guard_rejected.md");
}

/**
 * Build dedicated context for max consecutive cache misses failures.
 * Renders the max-cache-misses-exceeded prompt template with the active engine label.
 * @param {string} [engineLabel]
 * @returns {string}
 */
function buildEngineMaxCacheMissesExceededContext(engineLabel) {
  const normalizedEngineLabel = (typeof engineLabel === "string" ? engineLabel : "").trim() || "AI";
  return "\n" + renderPromptTemplate("max_cache_misses_exceeded.md", { engine_label: normalizedEngineLabel });
}

/**
 * Read and render token usage from token-usage.jsonl for inclusion in the ET computation table.
 * Returns null gracefully when files are absent, empty, or unparseable.
 * @returns {{ markdown: string, modelNames: string[] } | null} Pre-rendered per-model markdown table data, or null
 */
function readTokenUsageMarkdown() {
  try {
    const readablePaths = TOKEN_USAGE_PATHS.filter(p => {
      try {
        return fs.existsSync(p) && fs.statSync(p).size > 0;
      } catch {
        return false;
      }
    });
    if (readablePaths.length === 0) return null;
    const content = readDedupedTokenUsage(readablePaths);
    if (!content.trim()) return null;
    const tokenSummary = parseTokenUsageJsonl(content);
    if (!tokenSummary) return null;
    const markdown = generateTokenUsageSummary(tokenSummary);
    if (!markdown) return null;
    const modelNames = Array.from(new Set((tokenSummary.entries || []).map(entry => entry.model).filter(Boolean)));
    return { markdown, modelNames };
  } catch {
    return null;
  }
}

/**
 * Build a context string when AI credits budget exhaustion/rate-limit is detected from gateway logs.
 * @param {boolean} hasAICreditsRateLimitError
 * @param {string} aiCredits
 * @param {string} maxAICredits
 * @param {string} runUrl
 * @param {boolean} [isBudgetExceeded] - true when the agent exceeded the configured max-ai-credits budget; false when the 429 was a throughput throttle
 * @returns {string}
 */
function buildAICreditsRateLimitErrorContext(hasAICreditsRateLimitError, aiCredits, maxAICredits, runUrl, isBudgetExceeded = false) {
  if (!hasAICreditsRateLimitError) {
    return "";
  }

  const numericAICredits = Number(aiCredits);
  const numericMaxAICredits = Number(maxAICredits);
  const formattedAICredits = Number.isFinite(numericAICredits) && numericAICredits > 0 ? formatAIC(numericAICredits) : "";
  const formattedMaxAICredits = Number.isFinite(numericMaxAICredits) && numericMaxAICredits > 0 ? formatAIC(numericMaxAICredits) : "";
  const overage = Number.isFinite(numericAICredits) && Number.isFinite(numericMaxAICredits) && numericAICredits > numericMaxAICredits ? numericAICredits - numericMaxAICredits : NaN;
  const formattedOverage = Number.isFinite(overage) && overage > 0 ? formatAIC(overage) : "";

  // Build inline metrics summary (no table, no run URL)
  let metricsSummary = "";
  if (formattedAICredits && formattedMaxAICredits) {
    metricsSummary = ` Used \`${formattedAICredits}\` of \`${formattedMaxAICredits}\` max`;
    if (formattedOverage) {
      metricsSummary += ` (over by \`${formattedOverage}\`)`;
    }
    metricsSummary += ".";
  } else if (formattedAICredits) {
    metricsSummary = ` Used \`${formattedAICredits}\`.`;
  }

  // Use the budget-exceeded template when the agent exhausted its configured limit;
  // use the throughput-throttle template when the 429 arrived before the budget was spent.
  const templateName = isBudgetExceeded ? "ai_credits_rate_limit_error.md" : "ai_credits_rate_limit_throttle.md";
  let templatePath = "";
  try {
    templatePath = getPromptPath(templateName);
  } catch (error) {
    throw new Error(`failed to resolve template path for ${templateName} (${getErrorMessage(error)}); ensure RUNNER_TEMP or GH_AW_PROMPTS_DIR is set and the template file exists`, { cause: error });
  }

  let suggestedCredits;
  if (isBudgetExceeded) {
    // Suggest a new limit: 2x current max, or 2x actual usage if max is unknown, or a reasonable default.
    const baseForSuggestion = Number.isFinite(numericMaxAICredits) && numericMaxAICredits > 0 ? numericMaxAICredits : Number.isFinite(numericAICredits) && numericAICredits > 0 ? numericAICredits : 0;
    suggestedCredits = baseForSuggestion > 0 ? Math.ceil(baseForSuggestion * 2) : 2000;
  }

  try {
    return (
      "\n" +
      renderTemplateFromFile(templatePath, {
        metrics_summary: metricsSummary,
        suggested_credits: suggestedCredits,
      })
    );
  } catch (error) {
    throw new Error(`failed to render template at ${templatePath}: ${getErrorMessage(error)}; verify template syntax and required placeholders: metrics_summary, suggested_credits`, { cause: error });
  }
}

/**
 * Build a context string when a GitHub App token minting step failed.
 * @param {boolean} hasAppTokenMintingFailed - Whether any GitHub App token minting step failed
 * @returns {string} Formatted context string, or empty string if no error
 */
function buildAppTokenMintingFailedContext(hasAppTokenMintingFailed) {
  if (!hasAppTokenMintingFailed) {
    return "";
  }

  const templatePath = "/opt/gh-aw/prompts/app_token_minting_failed.md";
  return "\n" + renderTemplateFromFile(templatePath, {});
}

/**
 * Build a context string when the lockdown check step failed in the activation job.
 * @param {boolean} hasLockdownCheckFailed - Whether the lockdown check failed
 * @returns {string} Formatted context string, or empty string if no failure
 */
function buildLockdownCheckFailedContext(hasLockdownCheckFailed) {
  if (!hasLockdownCheckFailed) {
    return "";
  }

  const templatePath = getPromptPath("lockdown_check_failed.md");
  let template;
  try {
    template = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return "\n" + template;
}

/**
 * Build a context string when the OAuth token check step detected an OAuth token (gho_...)
 * in the activation job. OAuth tokens are not suitable for automation and must be replaced
 * with fine-grained Personal Access Tokens.
 * @param {boolean} hasOAuthTokenCheckFailed - Whether an OAuth token was detected
 * @param {string} runUrl - The workflow run URL for reference in the message
 * @returns {string} Formatted context string, or empty string if no failure
 */
function buildOAuthTokenCheckFailedContext(hasOAuthTokenCheckFailed, runUrl) {
  if (!hasOAuthTokenCheckFailed) {
    return "";
  }

  const templatePath = getPromptPath("oauth_token_check_failed.md");
  let template;
  try {
    template = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return "\n" + renderTemplate(template, { run_url: runUrl });
}

/**
 * Build a context string when the frontmatter hash (stale lock file) check failed in the
 * activation job. This surfaces remediation guidance — including how to disable the check —
 * directly in the failure issue / comment.
 * @param {boolean} hasStaleLockFileFailed - Whether the stale lock file check failed
 * @returns {string} Formatted context string, or empty string if no failure
 */
function buildStaleLockFileFailedContext(hasStaleLockFileFailed) {
  if (!hasStaleLockFileFailed) {
    return "";
  }

  const templatePath = getPromptPath("stale_lock_file_failed.md");
  let template;
  try {
    template = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return "\n" + template;
}

/**
 * Build a context string when the 24-hour per-workflow AIC guardrail prevented the agent from
 * starting in the activation job.
 * @param {boolean} hasDailyAICExceeded - Whether the daily workflow quota was exceeded
 * @param {string} totalAIC - Aggregated AIC usage across the last 24 hours
 * @param {string} threshold - Configured daily workflow threshold
 * @returns {string} Formatted context string, or empty string if no failure
 */
function buildDailyAICExceededContext(hasDailyAICExceeded, totalAIC, threshold) {
  if (!hasDailyAICExceeded) {
    return "";
  }

  const templatePath = getPromptPath("daily_workflow_aic_exceeded.md");
  const formattedTotalAIC = formatAICCredits(totalAIC);
  const formattedThreshold = formatAICCredits(threshold);
  return (
    "\n" +
    renderTemplateFromFile(templatePath, {
      total_aic: formattedTotalAIC || "unknown",
      threshold: formattedThreshold || "unknown",
    })
  );
}

/**
 * Build the "Optimize token consumption" details section for the failure issue when a guardrail
 * limit was the root cause of the failure.
 *
 * Guardrails that trigger this section:
 *   - max-ai-credits:       per-run AI Credits budget exceeded
 *   - max-daily-ai-credits: 24-hour per-workflow AI Credits quota exhausted
 *   - max-tool-denials:     Copilot SDK tool-denial threshold hit
 *   - max-turns / timeout:  agent ran out of turns or wall-clock time
 *
 * @param {object} options
 * @param {boolean} options.maxAICreditsExceeded       - max-ai-credits guardrail triggered
 * @param {boolean} options.hasDailyAICExceeded        - max-daily-ai-credits guardrail triggered
 * @param {boolean} options.hasToolDenialsExceeded     - max-tool-denials guardrail triggered
 * @param {boolean} options.isTimedOut                 - timeout / max-turns guardrail triggered
 * @param {string}  options.runUrl                     - URL to the failed workflow run
 * @returns {string} Rendered section or empty string when no guardrail was triggered
 */
function buildOptimizeTokenConsumptionContext({ maxAICreditsExceeded, hasDailyAICExceeded, hasToolDenialsExceeded, isTimedOut, runUrl }) {
  const guardrailTriggered = maxAICreditsExceeded || hasDailyAICExceeded || hasToolDenialsExceeded || isTimedOut;
  if (!guardrailTriggered) {
    return "";
  }

  let guardrailName = "guardrail limit";
  if (maxAICreditsExceeded) guardrailName = "max-ai-credits";
  else if (hasDailyAICExceeded) guardrailName = "max-daily-ai-credits";
  else if (hasToolDenialsExceeded) guardrailName = "max-tool-denials";
  else if (isTimedOut) guardrailName = "max-turns / timeout";

  const templatePath = getPromptPath("optimize_token_consumption_context.md");
  return renderTemplateFromFile(templatePath, { guardrail_name: guardrailName, run_url: runUrl });
}

// Maps engine ID (GH_AW_ENGINE_ID) to credential name for use with GH_AW_ENGINE_API_HOSTS.
const ENGINE_ID_TO_CREDENTIAL = /** @type {Record<string, string>} */ {
  copilot: "`COPILOT_GITHUB_TOKEN`",
  claude: "`ANTHROPIC_API_KEY`",
  codex: "`CODEX_API_KEY` / `OPENAI_API_KEY`",
  gemini: "`GEMINI_API_KEY`",
};

// Maps engine ID to a human-readable provider label.
const ENGINE_ID_TO_LABEL = /** @type {Record<string, string>} */ {
  copilot: "GitHub Copilot",
  claude: "Anthropic Claude",
  codex: "OpenAI Codex",
  gemini: "Google Gemini",
};

// Hardcoded fallback provider hosts for when GH_AW_ENGINE_API_HOSTS is not set.
// The host patterns are matched against the "host" field in audit.jsonl entries.
const FIREWALL_AUTH_PROVIDER_HOSTS = /** @type {Array<{provider: string, pattern: RegExp, credential: string}>} */ [
  { provider: "GitHub Copilot", pattern: /\.githubcopilot\.com/i, credential: "`COPILOT_GITHUB_TOKEN`" },
  { provider: "OpenAI Codex", pattern: /^api\.openai\.com/i, credential: "`CODEX_API_KEY` / `OPENAI_API_KEY`" },
  { provider: "Anthropic Claude", pattern: /^api\.anthropic\.com/i, credential: "`ANTHROPIC_API_KEY`" },
  { provider: "Google Gemini", pattern: /^generativelanguage\.googleapis\.com/i, credential: "`GEMINI_API_KEY`" },
];

/**
 * Build the list of registered provider entries from the GH_AW_ENGINE_API_HOSTS and
 * GH_AW_ENGINE_ID environment variables. Falls back to the hardcoded
 * FIREWALL_AUTH_PROVIDER_HOSTS list when the env var is not set.
 *
 * @returns {Array<{provider: string, credential: string, hosts?: string[], pattern?: RegExp}>} Provider entries
 */
function buildRegisteredProviderEntries() {
  const engineApiHosts = process.env.GH_AW_ENGINE_API_HOSTS;
  const engineId = (process.env.GH_AW_ENGINE_ID || "").toLowerCase();

  if (engineApiHosts) {
    const hosts = engineApiHosts
      .split(",")
      .map(h => h.trim().toLowerCase())
      .filter(Boolean);
    if (hosts.length > 0) {
      const provider = ENGINE_ID_TO_LABEL[engineId] || engineId || "Engine";
      const credential = ENGINE_ID_TO_CREDENTIAL[engineId] || "`API_KEY`";
      return [{ provider, credential, hosts }];
    }
  }

  // Fallback: use hardcoded patterns, converting to the same shape.
  return FIREWALL_AUTH_PROVIDER_HOSTS.map(({ provider, pattern, credential }) => ({
    provider,
    credential,
    hosts: /** @type {string[]} */ [],
    pattern,
  }));
}

/**
 * Parse the firewall audit.jsonl for authentication rejection entries.
 *
 * Performance strategy for large JSONL files (three-pass approach):
 *   1. Quick file-level regex pre-scan: bail early if no 401/403 status codes appear at all.
 *   2. Per-line regex pre-filter: skip lines without a 401/403 status pattern before JSON.parse.
 *   3. Full JSON parse: only for lines that pass both pre-filters.
 *
 * Providers are resolved dynamically from GH_AW_ENGINE_API_HOSTS / GH_AW_ENGINE_ID env vars
 * (set by the workflow compiler for the current engine). Falls back to a hardcoded list of
 * known public provider API hosts when the env var is not available.
 *
 * @param {string} auditJsonlPath - Path to audit.jsonl
 * @returns {Array<{provider: string, credential: string}>} Unique providers with auth rejections
 */
function parseFirewallAuthErrors(auditJsonlPath) {
  try {
    const resolvedPath = resolveFirewallAuditLogPath(auditJsonlPath);
    if (!fs.existsSync(resolvedPath)) {
      return [];
    }

    const content = fs.readFileSync(resolvedPath, "utf8");
    if (!content.trim()) {
      return [];
    }

    // Pass 1 — file-level pre-scan: bail early when no 401 or 403 appears anywhere.
    // The audit.jsonl format uses `"status":401` or `"status": 401` (compact or spaced).
    // `40[13]` matches digits 1 and 3 only, covering status codes 401 and 403.
    if (!/"status"\s*:\s*40[13]/.test(content)) {
      return [];
    }

    // Build the provider list from env vars (or fallback to hardcoded patterns).
    const providerEntries = buildRegisteredProviderEntries();
    const seenProviders = new Set();
    const results = [];

    for (const line of content.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed || trimmed[0] !== "{") continue;

      // Pass 2 — per-line regex pre-filter: skip lines that cannot have a 401/403 status.
      // This avoids the JSON.parse overhead on the majority of non-auth-failure lines.
      if (!/"status"\s*:\s*40[13]/.test(trimmed)) continue;

      let entry;
      try {
        entry = JSON.parse(trimmed);
      } catch {
        continue;
      }

      const status = entry.status;
      if (status !== 401 && status !== 403) continue;

      const host = typeof entry.host === "string" ? entry.host : "";
      if (!host) continue;
      // Strip port from host for matching (e.g. "api.openai.com:443" → "api.openai.com")
      const hostWithoutPort = host.replace(/:\d+$/, "").toLowerCase();

      for (const providerEntry of providerEntries) {
        const { provider, credential } = providerEntry;
        if (seenProviders.has(provider)) continue;

        // Dynamic matching: check if the host is in the registered hosts list.
        let matched = false;
        if (providerEntry.hosts && providerEntry.hosts.length > 0) {
          matched = providerEntry.hosts.some(h => hostWithoutPort === h || hostWithoutPort.endsWith("." + h));
        } else if (providerEntry.pattern) {
          // Fallback: use the pattern from the hardcoded list.
          matched = providerEntry.pattern.test(hostWithoutPort);
        }

        if (matched) {
          seenProviders.add(provider);
          results.push({ provider, credential });
          break;
        }
      }

      // Early exit: all providers have been matched, no need to scan remaining lines.
      if (seenProviders.size === providerEntries.length) break;
    }

    return results;
  } catch {
    return [];
  }
}

/**
 * Build a context string when the firewall audit log shows authentication rejections
 * from AI provider endpoints (expired or missing API credentials).
 *
 * Reads audit.jsonl from the known path relative to GH_AW_AGENT_OUTPUT and surfaces
 * a remediation hint for each affected provider in the failure issue/comment.
 *
 * @param {string} [auditJsonlPathOverride] - Path override for testing
 * @returns {string} Formatted context string, or empty string if no auth errors
 */
function buildCredentialAuthErrorContext(auditJsonlPathOverride) {
  const auditJsonlPath = resolveFirewallAuditLogPath(auditJsonlPathOverride);

  const authErrors = parseFirewallAuthErrors(auditJsonlPath);

  if (authErrors.length === 0) {
    return "";
  }

  core.info(`Firewall audit log: detected ${authErrors.length} credential auth rejection(s) for: ${authErrors.map(e => e.provider).join(", ")}`);

  const providersList = authErrors.map(e => `- ${e.provider} (${e.credential})`).join("\n");

  const templatePath = getPromptPath("credential_auth_error.md");
  return "\n" + renderTemplateFromFile(templatePath, { providers: providersList });
}

/**
 * Build a context string when assign_to_agent reported assignment errors.
 * Includes remediation guidance for token and Copilot access setup.
 * @param {string} assignmentErrors
 * @returns {string}
 */
function buildAssignmentErrorsContext(assignmentErrors) {
  if (!assignmentErrors || !assignmentErrors.trim()) {
    return "";
  }

  const errorLines = assignmentErrors.split("\n").filter(line => line.trim());
  let renderedErrors = "";
  for (const errorLine of errorLines) {
    const parts = errorLine.split(":");
    if (parts.length >= 4) {
      const type = parts[0]; // "issue" or "pr"
      const number = parts[1];
      const agent = parts[2];
      const error = parts.slice(3).join(":");
      renderedErrors += `- ${type === "issue" ? "Issue" : "PR"} #${number} (agent: ${agent}): ${error}\n`;
    }
  }

  const templatePath = getPromptPath("copilot_assignment_errors_context.md");
  return renderTemplateFromFile(templatePath, {
    warning_line: buildWarningAlertLine("Agent Assignment Failed", "Failed to assign agent to issues or pull requests."),
    assignment_errors: renderedErrors.trimEnd(),
  });
}
/**
 * Build a context string when assigning the Copilot coding agent to created issues failed.
 * @param {boolean} hasAssignCopilotFailures - Whether any copilot assignments failed
 * @param {string} assignCopilotErrors - Newline-separated list of "issue:number:copilot:error" entries
 * @returns {string} Formatted context string, or empty string if no failures
 */
function buildAssignCopilotFailureContext(hasAssignCopilotFailures, assignCopilotErrors) {
  if (!hasAssignCopilotFailures) {
    return "";
  }

  // Build a list of failed issue assignments
  let issueList = "";
  if (assignCopilotErrors) {
    const errorLines = assignCopilotErrors.split("\n").filter(line => line.trim());
    for (const errorLine of errorLines) {
      const parts = errorLine.split(":");
      if (parts.length >= 4) {
        const number = parts[1];
        const error = parts.slice(3).join(":"); // Rest is the error message
        issueList += `- Issue #${number}: ${error}\n`;
      }
    }
  }

  const templatePath = getPromptPath("assign_copilot_to_created_issues_failure.md");
  return "\n" + renderTemplateFromFile(templatePath, { issues: issueList });
}

/**
 * Build a context string when frontmatter skill installation failed.
 * @param {boolean} hasSkillInstallFailures - Whether any skill installs failed
 * @param {string} skillInstallErrors - Newline-separated list of "skill<TAB>error" entries
 * @returns {string} Formatted context string, or empty string if no failures
 */
function buildSkillInstallFailureContext(hasSkillInstallFailures, skillInstallErrors) {
  if (!hasSkillInstallFailures) {
    return "";
  }

  let skillList = "";
  if (skillInstallErrors) {
    const errorLines = skillInstallErrors.split("\n").filter(line => line.trim());
    for (const errorLine of errorLines) {
      // New format uses tab delimiter; keep colon parsing as a backward-compatible fallback.
      const tabIdx = errorLine.indexOf("\t");
      if (tabIdx >= 0) {
        const skill = errorLine.slice(0, tabIdx);
        const error = errorLine.slice(tabIdx + 1);
        skillList += `- \`${skill}\`: ${error}\n`;
        continue;
      }
      const colonIdx = errorLine.indexOf(":");
      if (colonIdx >= 0) {
        const skill = errorLine.slice(0, colonIdx);
        const error = errorLine.slice(colonIdx + 1);
        skillList += `- \`${skill}\`: ${error}\n`;
      } else {
        skillList += `- ${errorLine}\n`;
      }
    }
  }

  const templatePath = getPromptPath("skill_install_failure.md");
  return "\n" + renderTemplateFromFile(templatePath, { skills: skillList });
}

/**
 * For the Copilot engine, adds a suggestion to use `permissions.copilot-requests: write`
 * to enable Copilot inference through the org without a personal access token.
 * @param {string} secretVerificationResult - The secret verification result ("failed" or other)
 * @param {string} engineSecretFailureMessage - Engine-specific failure message from GH_AW_ENGINE_SECRET_FAILURE_MESSAGE
 * @returns {string} Formatted context string, or empty string if verification did not fail
 */
function buildSecretVerificationContext(secretVerificationResult, engineSecretFailureMessage) {
  if (secretVerificationResult !== "failed") {
    return "";
  }

  let context =
    buildWarningAlertLine("Secret Verification Failed", "The workflow's secret validation step failed. Please check that the required secrets are configured in your repository settings.") +
    "\nFor more information on configuring tokens, see: https://github.github.com/gh-aw/reference/engines/\n";

  if (engineSecretFailureMessage) {
    context += "\n" + engineSecretFailureMessage + "\n";
  }

  return context;
}

/**
 * Build a docker-sbx setup context from the dedicated runtime guidance template.
 * @param {string} dockerSbxSecretsResult
 * @returns {string}
 */
function buildDockerSbxSecretsContext(dockerSbxSecretsResult) {
  if (dockerSbxSecretsResult !== "failed") {
    return "";
  }
  return renderPromptTemplate("docker_sbx_secrets_missing.md");
}

/**
 * Check whether agent-stdio.log contains a terminal_reason: "completed" result entry,
 * indicating the agent finished its task successfully despite a non-zero job exit code.
 * Log lines may be prefixed with a timestamp (e.g. "2026-04-27T21:45:00.080Z  {JSON}").
 *
 * Lazy strategy: tests a single regex against the entire file content in one pass and
 * returns immediately on the first match, avoiding line splitting and JSON parsing.
 * The pattern is specific enough (`"terminal_reason"` key with `"completed"` value,
 * 0 or 1 literal space around the colon) that false positives from unrelated content
 * are negligible in practice.
 *
 * @returns {boolean} true if terminal_reason: "completed" was found in the log
 */
function hasAgentTerminalReasonCompleted() {
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
  const stdioLogPath = agentOutputFile ? path.join(path.dirname(agentOutputFile), "agent-stdio.log") : "/tmp/gh-aw/agent-stdio.log";
  try {
    if (!fs.existsSync(stdioLogPath)) {
      return false;
    }
    const logContent = fs.readFileSync(stdioLogPath, "utf8");
    // Single-pass scan: "terminal_reason" key with 0 or 1 literal space around
    // the colon and "completed" value. JSON uses either compact ("key":"val") or
    // single-spaced ("key" : "val") formatting. `[ ]?` matches only a space
    // character (not tabs or newlines). Returns on first match without splitting
    // lines or parsing JSON.
    return /"terminal_reason"[ ]?:[ ]?"completed"/.test(logContent);
  } catch {
    // IO error is ignored — assume not completed.
  }
  return false;
}

/**
 * Detect AWF firewall startup failure signals from log content.
 * Uses specific failure patterns to avoid false positives on successful runs
 * where container lifecycle lines (e.g., " Container awf-cli-proxy  Started")
 * also mention awf-cli-proxy.
 * @param {string} logContent Full content of agent-stdio.log
 * @param {Set<string>} [errorMessages] Collected error messages from the log parsing loop (optional)
 * @returns {{ isFirewallFailed: boolean, hasDNSFailure: boolean, hasDNSEAIAgain: boolean }}
 */
function detectAWFStartupSignals(logContent, errorMessages = undefined) {
  const hasFirewallFailedMsg = logContent.includes("AWF firewall failed to start");
  const hasDependencyFailedMsg = logContent.includes("dependency failed to start: container awf-cli-proxy");
  const hasErrorMsgWithProxy = errorMessages !== undefined && Array.from(errorMessages).some(msg => msg.includes("awf-cli-proxy"));
  const isFirewallFailed = hasFirewallFailedMsg || hasDependencyFailedMsg || hasErrorMsgWithProxy;

  const hasDNSEAIAgain = logContent.includes("EAI_AGAIN");
  const hasDNSDiagnosisUnknown = logContent.includes("diagnosis=unknown") && logContent.includes("awmg-cli-proxy");
  const hasDNSFailure = isFirewallFailed && (hasDNSEAIAgain || hasDNSDiagnosisUnknown);

  return { isFirewallFailed, hasDNSFailure, hasDNSEAIAgain };
}

/**
 * Detect whether the agent-stdio.log contains evidence of an AWF firewall startup failure.
 * Reads the log file from the path derived from GH_AW_AGENT_OUTPUT, falling back to the
 * default path. Returns false when the log file does not exist or cannot be read.
 * @returns {boolean}
 */
function detectAWFFirewallStartupFailureFromLog() {
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
  const stdioLogPath = agentOutputFile ? path.join(path.dirname(agentOutputFile), "agent-stdio.log") : "/tmp/gh-aw/agent-stdio.log";
  try {
    if (!fs.existsSync(stdioLogPath)) return false;
    const logContent = fs.readFileSync(stdioLogPath, "utf8");
    return detectAWFStartupSignals(logContent).isFirewallFailed;
  } catch {
    return false;
  }
}

/**
 * Detect whether the agent failure was caused by engine HTTP 429/rate limiting.
 * Checks agent-stdio.log first, then falls back to OTLP mirror payloads.
 * @returns {boolean}
 */
function detectEngineRateLimit429Failure() {
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
  const stdioLogPath = agentOutputFile ? path.join(path.dirname(agentOutputFile), "agent-stdio.log") : "/tmp/gh-aw/agent-stdio.log";
  try {
    if (fs.existsSync(stdioLogPath)) {
      const logContent = fs.readFileSync(stdioLogPath, "utf8");
      // If the agent completed successfully (terminal_reason: "completed"), the failure
      // was caused by something other than the agent itself. Suppress the 429 signal to
      // avoid giving a rate-limit title to an unrelated post-processing failure.
      if (/"terminal_reason"[ ]?:[ ]?"completed"/.test(logContent)) {
        return false;
      }
      if (hasEngineRateLimit429Signal(logContent)) {
        return true;
      }
    }
  } catch {
    // Ignore read errors and continue with OTLP mirror fallback.
  }
  return hasEngineRateLimit429InOTELMirror();
}

/**
 * Extract terminal error messages from agent-stdio.log to surface engine failures.
 * First tries to match known error patterns (ERROR:, Error:, Fatal:, panic:, Reconnecting...).
 * Falls back to the last non-empty lines of the log when no patterns match, so that
 * even timeout or unexpected-termination failures include the final agent output.
 * The log file is available in the conclusion job after the agent artifact is downloaded.
 * @returns {string} Formatted context string, or empty string if no engine failure found
 */
function buildEngineFailureContext(options = {}) {
  const suppressEngineRateLimit429 = options.suppressEngineRateLimit429 === true;
  const maxCacheMissesExceededFromDetection = options.maxCacheMissesExceeded === true;
  const shellExpansionGuardRejectedFromDetection = options.shellExpansionGuardRejected === true;
  // Derive agent-stdio.log path from the agent output file path (same directory)
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
  const stdioLogPath = agentOutputFile ? path.join(path.dirname(agentOutputFile), "agent-stdio.log") : "/tmp/gh-aw/agent-stdio.log";

  // Include engine ID in failure messages when available (e.g. "copilot", "claude", "codex")
  const engineId = process.env.GH_AW_ENGINE_ID || "";
  const engineLabel = engineId ? ` \`${engineId}\`` : " AI";
  const hasStructuredMaxCacheMissesSignal = maxCacheMissesExceededFromDetection || parseMaxCacheMissesExceededFromEventLog();

  try {
    if (!fs.existsSync(stdioLogPath)) {
      if (shellExpansionGuardRejectedFromDetection) {
        core.info("agent-stdio.log not found, but shell expansion guard rejection was detected — using dedicated context message");
        return buildShellExpansionGuardRejectedContext(true);
      }
      if (hasStructuredMaxCacheMissesSignal) {
        core.info("agent-stdio.log not found, but structured max cache misses signal was detected — using dedicated context message");
        return buildEngineMaxCacheMissesExceededContext(engineLabel);
      }
      core.info(`agent-stdio.log not found at ${stdioLogPath}, skipping engine failure context`);
      return "";
    }

    const logContent = fs.readFileSync(stdioLogPath, "utf8");
    if (!logContent.trim()) {
      if (shellExpansionGuardRejectedFromDetection) {
        core.info("agent-stdio.log is empty, but shell expansion guard rejection was detected — using dedicated context message");
        return buildShellExpansionGuardRejectedContext(true);
      }
      if (hasStructuredMaxCacheMissesSignal) {
        core.info("agent-stdio.log is empty, but structured max cache misses signal was detected — using dedicated context message");
        return buildEngineMaxCacheMissesExceededContext(engineLabel);
      }
      return "";
    }

    const lines = logContent.split("\n");

    // Values registered through `::add-mask::` are masked in the live job log but appear
    // verbatim in the captured log file. Collect them so every excerpt rendered into the
    // failure issue is redacted.
    const maskedValues = collectAddMaskedValues(logContent);
    if (maskedValues.length > 0) {
      core.info(`Detected ${maskedValues.length} add-mask value(s) in agent-stdio.log; redacting them from rendered output`);
    }

    // Guard: if the agent completed successfully (terminal_reason: "completed"), the job
    // failure was caused by something other than the agent itself (e.g., post-processing
    // or infrastructure). Suppress the engine failure context to avoid false positive labels.
    // Use the already-loaded logContent directly to avoid a redundant file read.
    if (/"terminal_reason"[ ]?:[ ]?"completed"/.test(logContent)) {
      core.info("Agent completed successfully (terminal_reason: completed) — suppressing engine failure context");
      return "";
    }

    if (!suppressEngineRateLimit429 && (hasEngineRateLimit429Signal(logContent) || hasEngineRateLimit429InOTELMirror())) {
      core.info("Detected engine HTTP 429/rate-limit signal — using dedicated context message");
      return buildEngineRateLimit429Context(engineLabel);
    }

    if (hasShellExpansionGuardRejectedSignal(logContent) || shellExpansionGuardRejectedFromDetection) {
      core.info("Detected shell expansion guard rejection — using dedicated context message");
      return buildShellExpansionGuardRejectedContext(true);
    }

    if (hasEngineMaxRunsExceededSignal(logContent)) {
      core.info("Detected engine max-runs guardrail signal — using dedicated context message");
      return buildEngineMaxRunsExceededContext(engineLabel);
    }

    if (hasEngineMaxCacheMissesExceededSignal(logContent) || hasStructuredMaxCacheMissesSignal) {
      core.info("Detected engine max cache misses signal — using dedicated context message");
      return buildEngineMaxCacheMissesExceededContext(engineLabel);
    }

    const errorMessages = new Set();
    // "No deferred tool marker found" is only noise when the Claude harness reports that it
    // recovered from it by retrying fresh with --continue permanently disabled. Correlate each
    // marker line with a later harness-prefixed recovery line so that markers that were never
    // recovered (including a terminal marker after an earlier recovered one) stay visible.
    const recoveredNoDeferredMarkerLines = new Set();
    {
      const pendingMarkerLines = [];
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        if (NO_DEFERRED_MARKER_LINE_RE.test(line)) {
          pendingMarkerLines.push(i);
          continue;
        }
        if (pendingMarkerLines.length > 0 && CLAUDE_HARNESS_NO_DEFERRED_MARKER_RECOVERY_RE.test(line)) {
          for (const markerLine of pendingMarkerLines) {
            recoveredNoDeferredMarkerLines.add(markerLine);
          }
          pendingMarkerLines.length = 0;
        }
      }
    }
    const isRecoveredNoDeferredMarkerLine = index => recoveredNoDeferredMarkerLines.has(index);

    for (const [lineIndex, line] of lines.entries()) {
      // Codex / generic CLI: "ERROR: <message>" at the start of a line
      const errorPrefixMatch = line.match(/^ERROR:\s*(.+)$/);
      if (errorPrefixMatch) {
        errorMessages.add(errorPrefixMatch[1].trim());
        continue;
      }

      // Node.js / generic: "Error: <message>" at the start of a line
      const errorCapMatch = line.match(/^Error:\s*(.+)$/);
      if (errorCapMatch) {
        const message = errorCapMatch[1].trim();
        if (isRecoveredNoDeferredMarkerLine(lineIndex)) {
          continue;
        }
        errorMessages.add(message);
        continue;
      }

      // Engine harness wrappers report their terminal failure as
      // "[<engine>-harness] unexpected error: <message>" (e.g. the copilot-sdk headless server
      // never becoming ready). Surface it as the root cause instead of falling back to the tail.
      const harnessUnexpectedErrorMatch = line.match(HARNESS_UNEXPECTED_ERROR_RE);
      if (harnessUnexpectedErrorMatch) {
        errorMessages.add(harnessUnexpectedErrorMatch[1].trim());
        continue;
      }

      // AWF startup failures can appear as "[ERROR] Failed to start ...".
      // Exclude generic wrapper [ERROR] lines (e.g., command completion/exit noise)
      // because they are infrastructure output and don't provide actionable startup context.
      const awfStartupBracketErrorMatch = line.match(/^\[ERROR\]\s*((?:Failed to start|dependency failed to start:).+)$/);
      if (awfStartupBracketErrorMatch) {
        errorMessages.add(awfStartupBracketErrorMatch[1].trim());
        continue;
      }

      // Fatal errors: "Fatal: <message>" or "FATAL: <message>"
      const fatalMatch = line.match(/^(?:FATAL|Fatal):\s*(.+)$/);
      if (fatalMatch) {
        errorMessages.add(fatalMatch[1].trim());
        continue;
      }

      // AWF docker-compose dependency failures surface this root-cause line without
      // an explicit log-level prefix.
      const awfDependencyFailureMatch = line.match(/^dependency failed to start:\s*(.+)$/);
      if (awfDependencyFailureMatch) {
        errorMessages.add(`dependency failed to start: ${awfDependencyFailureMatch[1].trim()}`);
        continue;
      }

      // Go runtime panic: "panic: <message>"
      const panicMatch = line.match(/^panic:\s*(.+)$/);
      if (panicMatch) {
        errorMessages.add(panicMatch[1].trim());
        continue;
      }

      // Reconnect-style lines that embed the error reason: "Reconnecting... N/M (reason)"
      const reconnectMatch = line.match(/^Reconnecting\.\.\.\s+\d+\/\d+\s*\((.+)\)$/);
      if (reconnectMatch) {
        errorMessages.add(reconnectMatch[1].trim());
      }
    }

    if (errorMessages.size > 0) {
      core.info(`Found ${errorMessages.size} engine error message(s) in agent-stdio.log`);

      // Check for cyber_policy_violation specifically and return a dedicated message
      const hasCyberPolicyViolation = Array.from(errorMessages).some(msg => msg.includes("cyber_policy_violation"));
      if (hasCyberPolicyViolation) {
        core.info("Detected cyber_policy_violation error — using dedicated context message");
        const templatePath = getPromptPath("cyber_policy_violation.md");
        try {
          return "\n" + renderTemplateFromFile(templatePath, {});
        } catch {
          // Template not available — fall through to generic engine failure message
          core.info(`cyber_policy_violation template not found at ${templatePath}, using generic message`);
        }
      }

      // Check for AWF firewall startup failure (cli-proxy / DIFC proxy could not start)
      const { isFirewallFailed, hasDNSFailure, hasDNSEAIAgain } = detectAWFStartupSignals(logContent, errorMessages);
      if (isFirewallFailed) {
        core.info("Detected AWF firewall startup failure — using dedicated context message");
        let context = buildWarningAlertLine("AWF Firewall Startup Failure", `The AWF firewall failed to start — the${engineLabel} agent was never invoked.`) + "\n";
        if (hasDNSFailure) {
          const dnsDiagnosis = hasDNSEAIAgain
            ? "**Diagnosis:** DNS resolution of `awmg-cli-proxy` returned `EAI_AGAIN` (temporary DNS failure). The DIFC probe exhausted its retry budget before the name resolved.\n\n"
            : "**Diagnosis:** The DIFC proxy (`awmg-cli-proxy`) failed to respond (`diagnosis=unknown`). The probe exhausted its retry budget before the service became reachable.\n\n";
          context += dnsDiagnosis;
        }
        context += "\n<details>\n<summary>Error details</summary>\n\n";
        for (const message of errorMessages) {
          context += `- ${applyAddMaskRedaction(message, maskedValues)}\n`;
        }
        context += `\n</details>\n\nSee [Diagnosing AWF Failures](https://github.com/github/gh-aw-firewall/blob/main/docs/diagnosing-awf-failures.md) for troubleshooting guidance.\n\n`;
        return context;
      }

      let context = buildWarningAlertLine("Engine Failure", `The${engineLabel} engine terminated before producing output.`) + "\n**Error details:**\n";
      for (const message of errorMessages) {
        context += `- ${applyAddMaskRedaction(message, maskedValues)}\n`;
      }
      context += "\n";
      return context;
    }

    // AWF infrastructure lines written by the firewall/container wrapper — not produced by
    // the engine itself. They must be filtered out of the fallback tail so the failure
    // context surfaces actual agent output rather than container lifecycle noise
    // (e.g. "Container awf-squid  Removed", "[WARN] Command completed with exit code: 1",
    // "Process exiting with code: 1"). Shared constant from log_parser_shared.cjs keeps the
    // pattern in sync with parse_copilot_log.cjs.
    const INFRA_LINE_RE = AWF_INFRA_LINE_RE;

    // AWF infrastructure messages can wrap onto indented continuation lines, e.g.
    //   [WARN] --pids-limit/container.pidsLimit is not supported by this microVM runtime …
    //      The Docker agent cgroup cannot be passed through, so pids.max/pids.current are unavailable.
    // Those continuations belong to the infrastructure line above them, so they must be
    // filtered out as well — otherwise they can be reported as the "last agent output".
    const infraContinuationLines = new Set();
    {
      let previousWasInfra = false;
      for (const [index, line] of lines.entries()) {
        if (!line.trim()) {
          previousWasInfra = false;
          continue;
        }
        if (INFRA_LINE_RE.test(line)) {
          previousWasInfra = true;
          continue;
        }
        if (previousWasInfra && /^\s/.test(line)) {
          infraContinuationLines.add(index);
          continue;
        }
        previousWasInfra = false;
      }
    }

    // Fallback: no known error patterns found — include the last non-empty lines so that
    // failures caused by timeouts or unexpected terminations still surface useful context.
    const TAIL_LINES = 10;
    const nonEmptyLines = lines.map((line, index) => ({ line, index })).filter(entry => entry.line.trim());
    if (nonEmptyLines.length === 0) {
      return "";
    }

    // Exclude AWF infrastructure lines so the fallback displays only actual engine output.
    // `::add-mask::` command lines are runner directives, not agent output: drop them so the
    // rendered tail neither leaks the masked value nor wastes a tail slot.
    const agentLines = nonEmptyLines
      .filter(entry => !INFRA_LINE_RE.test(entry.line) && !infraContinuationLines.has(entry.index) && !isAddMaskCommandLine(entry.line) && !isRecoveredNoDeferredMarkerLine(entry.index))
      .map(entry => entry.line);

    if (agentLines.length === 0) {
      // The log contains only AWF infrastructure lines — the engine exited before producing
      // any substantive output. Check first if this is an AWF firewall startup failure.
      const { isFirewallFailed: isAWFFirewallStartupFailedInfra, hasDNSFailure: hasDNSFailureInfra, hasDNSEAIAgain: hasDNSEAIAgainInfra } = detectAWFStartupSignals(logContent);
      if (isAWFFirewallStartupFailedInfra) {
        core.info("Detected AWF firewall startup failure in infra-only log — using dedicated context message");
        let context = buildWarningAlertLine("AWF Firewall Startup Failure", `The AWF firewall failed to start — the${engineLabel} agent was never invoked.`) + "\n";
        if (hasDNSFailureInfra) {
          const dnsDiagnosis = hasDNSEAIAgainInfra
            ? "**Diagnosis:** DNS resolution of `awmg-cli-proxy` returned `EAI_AGAIN` (temporary DNS failure). The DIFC probe exhausted its retry budget before the name resolved.\n\n"
            : "**Diagnosis:** The DIFC proxy (`awmg-cli-proxy`) failed to respond (`diagnosis=unknown`). The probe exhausted its retry budget before the service became reachable.\n\n";
          context += dnsDiagnosis;
        }
        context += `\nSee [Diagnosing AWF Failures](https://github.com/github/gh-aw-firewall/blob/main/docs/diagnosing-awf-failures.md) for troubleshooting guidance.\n\n`;
        return context;
      }

      // This pattern is characteristic of a transient startup failure
      // (e.g., API service unavailable, rate-limiting, token not yet provisioned).
      core.info("agent-stdio.log contains only infrastructure lines — engine likely failed at startup (possible transient failure)");
      const recurringFailureGuidance =
        process.env.GH_AW_ENGINE_ID === "copilot"
          ? "If this failure recurs, check the GitHub Copilot status page and review the firewall audit logs.\n\n"
          : "If this failure recurs, check the provider status page (if available) and review the firewall audit logs.\n\n";
      let context = buildWarningAlertLine("Engine Failure", `The${engineLabel} engine terminated before producing output.`) + "\n";
      context += "The engine exited immediately without producing any output. This often indicates a transient infrastructure issue (e.g., service unavailable, API rate limiting). " + recurringFailureGuidance;
      return context;
    }

    const tailLines = agentLines.slice(-TAIL_LINES);
    core.info(`No specific error patterns found; including last ${tailLines.length} line(s) of agent-stdio.log as fallback`);

    let context = buildWarningAlertLine("Engine Failure", `The${engineLabel} engine terminated unexpectedly.`) + "\n**Last agent output:**\n\`\`\`\n";
    context += applyAddMaskRedaction(tailLines.join("\n"), maskedValues);
    context += "\n```\n\n";
    return context;
  } catch (error) {
    core.info(`Failed to read agent-stdio.log for engine failure context: ${getErrorMessage(error)}`);
    return "";
  }
}

/** Cascade detection constants */
const CASCADE_WINDOW_MINUTES = 60;
const CASCADE_WINDOW_MS = CASCADE_WINDOW_MINUTES * 60 * 1000;
const CASCADE_THRESHOLD = 10;
const CASCADE_ROLLUP_TITLE = "[aw] Failure cascade detected";
const CASCADE_LABEL = "cascade-suspected";
const CASCADE_ROLLUP_LABEL = "cascade-rollup";
/** Daily-cap rollup constants */
const DAILY_CAP_ROLLUP_TITLE = "[aw] Daily failure issue cap exceeded";
const DAILY_CAP_ROLLUP_LABEL = "daily-cap-exceeded";
/** Matches individual failure issue titles produced by handle_agent_failure */
const FAILURE_TITLE_PATTERN = /^\[aw\] \S.*$/;

/**
 * Ensure a GitHub label exists in the repository, creating it with a deterministic
 * pastel color if it does not exist yet. Failures are non-fatal (logged as warnings).
 * @param {string} owner
 * @param {string} repo
 * @param {string} labelName
 * @returns {Promise<void>}
 */
async function ensureLabelExists(owner, repo, labelName) {
  try {
    await github.rest.issues.getLabel({ owner, repo, name: labelName });
  } catch (err) {
    // 404 → label does not exist, create it
    const statusCode = err && typeof err === "object" && "status" in err ? /** @type {any} */ err.status : undefined;
    if (statusCode !== 404) {
      core.warning(`Could not check label "${labelName}": ${getErrorMessage(err)}`);
      return;
    }
    try {
      // Derive a deterministic pastel color from the label name
      let hash = 0;
      for (let i = 0; i < labelName.length; i++) {
        hash = (hash * 31 + labelName.charCodeAt(i)) >>> 0;
      }
      const r = 128 + (hash & 0x3f);
      const g = 128 + ((hash >> 6) & 0x3f);
      const b = 128 + ((hash >> 12) & 0x3f);
      const color = ((r << 16) | (g << 8) | b).toString(16).padStart(6, "0");
      await github.rest.issues.createLabel({ owner, repo, name: labelName, color });
      core.info(`✓ Created label "${labelName}" (#${color})`);
    } catch (createErr) {
      core.warning(`Could not create label "${labelName}": ${getErrorMessage(createErr)}`);
    }
  }
}

/**
 * Detect whether a failure cascade is active by counting `[aw] *` failure issues
 * created within the last CASCADE_WINDOW_MINUTES minutes.
 *
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<Array<{number: number, title: string, html_url: string, created_at: string}>>}
 *   Issues that belong to the cascade window (may be empty).
 */
async function findRecentFailureIssues(owner, repo) {
  const windowStart = new Date(Date.now() - CASCADE_WINDOW_MS);
  const since = windowStart.toISOString().slice(0, 19) + "Z"; // e.g. "2026-05-22T02:00:00Z"

  // GitHub search API supports `created:>=YYYY-MM-DDTHH:MM:SSZ`
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:agentic-workflows "[aw]" in:title created:>=${since}`;

  try {
    const result = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 100,
      sort: "created",
      order: "asc",
    });
    return result.data.items
      .filter(item => FAILURE_TITLE_PATTERN.test(item.title))
      .map(item => ({
        number: item.number,
        title: item.title,
        html_url: item.html_url,
        created_at: item.created_at,
      }));
  } catch (err) {
    core.warning(`Could not query recent failure issues for cascade detection: ${getErrorMessage(err)}`);
    return [];
  }
}

/**
 * Find an existing open cascade rollup issue.
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<{number: number, html_url: string} | null>}
 */
async function findExistingCascadeRollupIssue(owner, repo) {
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${CASCADE_ROLLUP_LABEL} in:title "${CASCADE_ROLLUP_TITLE}"`;
  try {
    const result = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });
    if (result.data.total_count > 0) {
      const item = result.data.items[0];
      return { number: item.number, html_url: item.html_url };
    }
  } catch (err) {
    core.warning(`Could not search for cascade rollup issue: ${getErrorMessage(err)}`);
  }
  return null;
}

/**
 * Find an existing open daily-cap rollup issue, or create one.
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<{number: number, html_url: string} | null>}
 */
async function findOrCreateDailyCapRollupIssue(owner, repo) {
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:agentic-workflows in:title "${DAILY_CAP_ROLLUP_TITLE}"`;
  try {
    const result = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });
    if (result.data.total_count > 0) {
      const item = result.data.items[0];
      core.info(`Found existing daily cap rollup issue #${item.number}: ${item.html_url}`);
      return { number: item.number, html_url: item.html_url };
    }
  } catch (err) {
    core.warning(`Could not search for daily cap rollup issue: ${getErrorMessage(err)}`);
  }

  // No existing issue found — create one
  const body = renderTemplateFromFile(getPromptPath("daily_cap_rollup_issue.md"), {
    cap: FAILURE_ISSUE_CATEGORY_DAILY_CAP,
    window_hours: FAILURE_ISSUE_DEDUP_WINDOW_HOURS,
  });

  try {
    await ensureLabelExists(owner, repo, DAILY_CAP_ROLLUP_LABEL);
    const newIssue = await github.rest.issues.create({
      owner,
      repo,
      title: DAILY_CAP_ROLLUP_TITLE,
      body,
      labels: ["agentic-workflows", DAILY_CAP_ROLLUP_LABEL],
      headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
    });
    core.info(`✓ Created daily cap rollup issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
    return { number: newIssue.data.number, html_url: newIssue.data.html_url };
  } catch (err) {
    core.warning(`Could not create daily cap rollup issue: ${getErrorMessage(err)}`);
    return null;
  }
}

/**
 * Detect a failure cascade and, when one is active, create/update a rollup issue
 * and add the `cascade-suspected` label to every issue in the cascade window
 * (including the issue that was just created/updated, identified by `triggeringIssueNumber`).
 *
 * A cascade is active when ≥CASCADE_THRESHOLD `[aw] * failed` issues were filed
 * within the last CASCADE_WINDOW_MINUTES minutes.
 *
 * @param {string} owner
 * @param {string} repo
 * @param {number} triggeringIssueNumber - The issue just created or updated by this run
 * @returns {Promise<void>}
 */
async function detectAndHandleFailureCascade(owner, repo, triggeringIssueNumber) {
  try {
    const recentIssues = await findRecentFailureIssues(owner, repo);

    // Ensure the triggering issue is included even if GitHub search indexing lags
    const issueNumbers = new Set(recentIssues.map(i => i.number));
    issueNumbers.add(triggeringIssueNumber);

    if (issueNumbers.size < CASCADE_THRESHOLD) {
      core.info(`Cascade check: ${issueNumbers.size} failure issue(s) in the last ${CASCADE_WINDOW_MINUTES} min (threshold: ${CASCADE_THRESHOLD}) — no cascade`);
      return;
    }

    core.info(`⚠️ Cascade detected: ${issueNumbers.size} failure issues in the last ${CASCADE_WINDOW_MINUTES} min — creating rollup and labeling individual issues`);

    // Ensure required labels exist
    await ensureLabelExists(owner, repo, CASCADE_LABEL);
    await ensureLabelExists(owner, repo, CASCADE_ROLLUP_LABEL);

    // Build rollup body
    const affectedList = recentIssues.map(i => `- [#${i.number}](${i.html_url}) — ${i.title}`).join("\n");
    const windowStart = new Date(Date.now() - CASCADE_WINDOW_MS);
    const rollupBody = [
      `## ⚠️ Failure Cascade Detected`,
      ``,
      `**${issueNumbers.size} \`[aw] * failed\` issues** were filed within the last **${CASCADE_WINDOW_MINUTES} minutes** (since ${windowStart.toUTCString()}).`,
      ``,
      `This volume suggests a common root cause (e.g., lockfile drift, provider outage, infrastructure change) rather than isolated workflow failures.`,
      ``,
      `### Affected Workflows`,
      ``,
      affectedList || `_(none indexed yet — search indexing may lag)_`,
      ``,
      `### What to Do`,
      ``,
      `1. Identify the common root cause (check recent infra changes, provider status, lockfile drift).`,
      `2. Once the root cause is resolved, batch-close the \`${CASCADE_LABEL}\`-labeled issues.`,
      `3. Close this rollup issue when the cascade is resolved.`,
      ``,
      `### Labels`,
      ``,
      `Each individual failure issue in the cascade window has been labeled \`${CASCADE_LABEL}\` so you can filter and batch-close them once the root cause is patched.`,
    ].join("\n");

    // Create or update the cascade rollup issue
    const existing = await findExistingCascadeRollupIssue(owner, repo);
    if (existing) {
      await github.rest.issues.update({
        owner,
        repo,
        issue_number: existing.number,
        body: rollupBody,
      });
      core.info(`✓ Updated cascade rollup issue #${existing.number}: ${existing.html_url}`);
    } else {
      const newRollup = await github.rest.issues.create({
        owner,
        repo,
        title: CASCADE_ROLLUP_TITLE,
        body: rollupBody,
        labels: ["agentic-workflows", CASCADE_ROLLUP_LABEL],
        headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
      });
      core.info(`✓ Created cascade rollup issue #${newRollup.data.number}: ${newRollup.data.html_url}`);
    }

    // Add cascade-suspected label to every issue in the window
    for (const number of issueNumbers) {
      try {
        await github.rest.issues.addLabels({
          owner,
          repo,
          issue_number: number,
          labels: [CASCADE_LABEL],
        });
        core.info(`✓ Labeled issue #${number} with "${CASCADE_LABEL}"`);
      } catch (labelErr) {
        core.warning(`Could not label issue #${number} with "${CASCADE_LABEL}": ${getErrorMessage(labelErr)}`);
      }
    }
  } catch (err) {
    core.warning(`Cascade detection failed (non-fatal): ${getErrorMessage(err)}`);
  }
}

/**
 * Handle agent job failure by creating or updating a failure tracking issue
 * This script is called from the conclusion job when the agent job has failed
 * or when the agent succeeded but produced no safe outputs
 */
async function main() {
  try {
    // Get workflow context
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
    const workflowID = process.env.GH_AW_WORKFLOW_ID || "unknown";
    const agentConclusion = process.env.GH_AW_AGENT_CONCLUSION || "";
    const runUrl = process.env.GH_AW_RUN_URL || "";
    const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
    const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
    const secretVerificationResult = process.env.GH_AW_SECRET_VERIFICATION_RESULT || "";
    const dockerSbxSecretsResult = process.env.GH_AW_DOCKER_SBX_SECRETS_RESULT || "";
    const engineSecretFailureMessage = process.env.GH_AW_ENGINE_SECRET_FAILURE_MESSAGE || "";
    const assignmentErrors = process.env.GH_AW_ASSIGNMENT_ERRORS || "";
    const assignmentErrorCount = process.env.GH_AW_ASSIGNMENT_ERROR_COUNT || "0";
    const assignCopilotErrors = process.env.GH_AW_ASSIGN_COPILOT_ERRORS || "";
    const assignCopilotFailureCount = process.env.GH_AW_ASSIGN_COPILOT_FAILURE_COUNT || "0";
    const skillInstallErrors = process.env.GH_AW_SKILL_INSTALL_ERRORS || "";
    const skillInstallFailureCount = process.env.GH_AW_SKILL_INSTALL_FAILURE_COUNT || "0";
    const createDiscussionErrors = process.env.GH_AW_CREATE_DISCUSSION_ERRORS || "";
    const createDiscussionErrorCount = process.env.GH_AW_CREATE_DISCUSSION_ERROR_COUNT || "0";
    const codePushFailureErrors = process.env.GH_AW_CODE_PUSH_FAILURE_ERRORS || "";
    const codePushFailureCount = process.env.GH_AW_CODE_PUSH_FAILURE_COUNT || "0";
    const checkoutPRSuccess = process.env.GH_AW_CHECKOUT_PR_SUCCESS || "";
    const timeoutMinutes = process.env.GH_AW_TIMEOUT_MINUTES || "";
    const { aiCredits, maxAICredits, aiCreditsRateLimitError, maxAICreditsExceeded } = resolveAICreditsFailureState();
    const inferenceAccessError = process.env.GH_AW_INFERENCE_ACCESS_ERROR === "true";
    const mcpPolicyError = process.env.GH_AW_MCP_POLICY_ERROR === "true";
    const agenticEngineTimeout = process.env.GH_AW_AGENTIC_ENGINE_TIMEOUT === "true";
    const modelNotSupportedError = process.env.GH_AW_MODEL_NOT_SUPPORTED_ERROR === "true";
    const http400ResponseError = process.env.GH_AW_HTTP_400_RESPONSE_ERROR === "true";
    const maxCacheMissesExceeded = process.env.GH_AW_MAX_CACHE_MISSES_EXCEEDED === "true" && agentConclusion === "failure";
    const unknownModelAICreditsFromOutput = process.env.GH_AW_UNKNOWN_MODEL_AI_CREDITS === "true";
    const unknownModelAICreditsFromAudit = parseUnknownModelAICreditsFromAuditLog();
    const unknownModelAICredits = unknownModelAICreditsFromAudit || (unknownModelAICreditsFromOutput && agentConclusion === "failure");
    const missingModelPricingError = process.env.GH_AW_MISSING_MODEL_PRICING_ERROR === "true" && agentConclusion === "failure";
    const missingModelPricingModelName = process.env.GH_AW_MISSING_MODEL_PRICING_MODEL_NAME || "";
    const shellExpansionGuardRejected = process.env.GH_AW_SHELL_EXPANSION_GUARD_REJECTED === "true" && agentConclusion === "failure";
    const pushRepoMemoryResult = process.env.GH_AW_PUSH_REPO_MEMORY_RESULT || "";
    const reportFailureAsIssue = parseBoolTemplatable(process.env.GH_AW_FAILURE_REPORT_AS_ISSUE, true);
    // Parse included categories filter for report-failure-as-issue (optional JSON array of category strings)
    const failureCategoriesFilterRaw = process.env.GH_AW_FAILURE_CATEGORIES_FILTER || "";
    /** @type {any} */
    let failureCategoriesFilter = null;
    if (failureCategoriesFilterRaw) {
      try {
        failureCategoriesFilter = JSON.parse(failureCategoriesFilterRaw);
        if (!Array.isArray(failureCategoriesFilter)) {
          core.warning(`GH_AW_FAILURE_CATEGORIES_FILTER is not an array, ignoring: ${failureCategoriesFilterRaw}`);
          failureCategoriesFilter = null;
        } else {
          core.info(`Failure categories include filter enabled: ${failureCategoriesFilter.join(", ")}`);
        }
      } catch (parseError) {
        core.warning(`Failed to parse GH_AW_FAILURE_CATEGORIES_FILTER, ignoring: ${getErrorMessage(parseError)}`);
        failureCategoriesFilter = null;
      }
    }
    // Parse excluded categories filter for report-failure-as-issue (optional JSON array of category strings)
    const failureExcludedCategoriesFilterRaw = process.env.GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER || "";
    /** @type {any} */
    let failureExcludedCategoriesFilter = null;
    if (failureExcludedCategoriesFilterRaw) {
      try {
        failureExcludedCategoriesFilter = JSON.parse(failureExcludedCategoriesFilterRaw);
        if (!Array.isArray(failureExcludedCategoriesFilter)) {
          core.warning(`GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER is not an array, ignoring: ${failureExcludedCategoriesFilterRaw}`);
          failureExcludedCategoriesFilter = null;
        } else {
          core.info(`Failure categories exclude filter enabled: ${failureExcludedCategoriesFilter.join(", ")}`);
        }
      } catch (parseError) {
        core.warning(`Failed to parse GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER, ignoring: ${getErrorMessage(parseError)}`);
        failureExcludedCategoriesFilter = null;
      }
    }
    // Feature flags: control whether missing_tool/missing_data signals trigger agent failure handling.
    // Defaults to true (new behavior); set to false to restore pre-2026 behavior where these signals
    // are only shown in output footers / separate issues without activating the failure code path.
    const missingToolReportAsFailure = process.env.GH_AW_MISSING_TOOL_REPORT_AS_FAILURE !== "false";
    const missingDataReportAsFailure = process.env.GH_AW_MISSING_DATA_REPORT_AS_FAILURE !== "false";
    // GitHub App token minting failures from the safe_outputs job, conclusion job, and activation job.
    // Any of these being "true" indicates a GitHub App authentication configuration error.
    const safeOutputsAppTokenMintingFailed = process.env.GH_AW_SAFE_OUTPUTS_APP_TOKEN_MINTING_FAILED === "true";
    const conclusionAppTokenMintingFailed = process.env.GH_AW_CONCLUSION_APP_TOKEN_MINTING_FAILED === "true";
    const activationAppTokenMintingFailed = process.env.GH_AW_ACTIVATION_APP_TOKEN_MINTING_FAILED === "true";
    const hasAppTokenMintingFailed = safeOutputsAppTokenMintingFailed || conclusionAppTokenMintingFailed || activationAppTokenMintingFailed;
    // Lockdown check failure from the activation job — set when validate_lockdown_requirements fails.
    // The agent is skipped in this case, but the conclusion job still runs to report the failure.
    const hasLockdownCheckFailed = process.env.GH_AW_LOCKDOWN_CHECK_FAILED === "true";
    // OAuth token check failure from the activation job — set when check_oauth_tokens.sh detects
    // a gho_... OAuth token in COPILOT_GITHUB_TOKEN, GH_AW_GITHUB_TOKEN, or GH_AW_GITHUB_MCP_SERVER_TOKEN.
    // The agent is skipped in this case; the conclusion job runs to surface remediation guidance.
    const hasOAuthTokenCheckFailed = process.env.GH_AW_OAUTH_TOKEN_CHECK_FAILED === "true";
    // Stale lock file check failure from the activation job — set when the frontmatter hash
    // stored in the compiled .lock.yml no longer matches the source .md file.
    // The agent is skipped in this case; the conclusion job runs to surface remediation guidance.
    const hasStaleLockFileFailed = process.env.GH_AW_STALE_LOCK_FILE_FAILED === "true";
    const hasDailyAICExceeded = process.env.GH_AW_DAILY_AI_CREDITS_EXCEEDED === "true";
    const dailyAICTotal = process.env.GH_AW_DAILY_AI_CREDITS_TOTAL_EFFECTIVE_TOKENS || "";
    const dailyAICThreshold = process.env.GH_AW_DAILY_AI_CREDITS_THRESHOLD || "";
    // Cache-memory availability flag — set when cache-memory is configured for the workflow.
    // Used to detect cache-miss misconfigurations reported by the agent.
    const cacheMemoryEnabled = process.env.GH_AW_CACHE_MEMORY_ENABLED === "true";
    const cacheMemoryRestored = cacheMemoryEnabled ? resolveCacheMemoryRestored() : false;

    // Collect repo-memory validation errors from all memory configurations
    const repoMemoryValidationErrors = [];
    const repoMemoryPatchSizeExceededIDs = [];
    for (const key in process.env) {
      if (key.startsWith("GH_AW_REPO_MEMORY_VALIDATION_FAILED_")) {
        const memoryID = key.replace("GH_AW_REPO_MEMORY_VALIDATION_FAILED_", "");
        const failed = process.env[key] === "true";
        if (failed) {
          const errorKey = `GH_AW_REPO_MEMORY_VALIDATION_ERROR_${memoryID}`;
          const errorMessage = process.env[errorKey] || "Unknown validation error";
          repoMemoryValidationErrors.push({ memoryID, errorMessage });
        }
      }
      if (key.startsWith("GH_AW_REPO_MEMORY_PATCH_SIZE_EXCEEDED_")) {
        const memoryID = key.replace("GH_AW_REPO_MEMORY_PATCH_SIZE_EXCEEDED_", "");
        if (process.env[key] === "true") {
          repoMemoryPatchSizeExceededIDs.push(memoryID);
        }
      }
    }

    core.info(`Agent conclusion: ${agentConclusion}`);
    core.info(`Workflow name: ${workflowName}`);
    core.info(`Workflow ID: ${workflowID}`);
    core.info(`Secret verification result: ${secretVerificationResult}`);
    core.info(`Engine secret failure message: ${engineSecretFailureMessage ? "(set)" : "(none)"}`);
    core.info(`Assignment error count: ${assignmentErrorCount}`);
    core.info(`Assign copilot failure count: ${assignCopilotFailureCount}`);
    core.info(`Skill install failure count: ${skillInstallFailureCount}`);
    core.info(`Create discussion error count: ${createDiscussionErrorCount}`);
    core.info(`Code push failure count: ${codePushFailureCount}`);
    core.info(`Checkout PR success: ${checkoutPRSuccess}`);
    core.info(`AI credits: ${aiCredits || "(none)"}`);
    core.info(`Configured max AI credits: ${maxAICredits || "(none)"}`);
    core.info(`AI credits rate-limit error: ${aiCreditsRateLimitError}`);
    core.info(`Max AI credits exceeded (harness budget abort): ${maxAICreditsExceeded}`);
    core.info(`Daily workflow AIC guardrail exceeded: ${hasDailyAICExceeded}`);
    core.info(`Inference access error: ${inferenceAccessError}`);
    core.info(`MCP policy error: ${mcpPolicyError}`);
    core.info(`Agentic engine timeout: ${agenticEngineTimeout}`);
    core.info(`Model not supported error: ${modelNotSupportedError}`);
    core.info(`HTTP 400 response error: ${http400ResponseError}`);
    core.info(`Unknown model AI credits error: ${unknownModelAICredits}`);
    core.info(`Unknown model AI credits sources (audit/output): ${unknownModelAICreditsFromAudit}/${unknownModelAICreditsFromOutput}`);
    core.info(`Missing model pricing error: ${missingModelPricingError} (model: ${missingModelPricingModelName || "(unknown)"})`);
    core.info(`Shell expansion guard rejected: ${shellExpansionGuardRejected}`);
    core.info(`Push repo-memory result: ${pushRepoMemoryResult}`);
    core.info(`App token minting failed (safe_outputs/conclusion/activation): ${safeOutputsAppTokenMintingFailed}/${conclusionAppTokenMintingFailed}/${activationAppTokenMintingFailed}`);
    core.info(`Lockdown check failed: ${hasLockdownCheckFailed}`);
    core.info(`OAuth token check failed: ${hasOAuthTokenCheckFailed}`);
    core.info(`Stale lock file check failed: ${hasStaleLockFileFailed}`);
    core.info(`Cache memory enabled: ${cacheMemoryEnabled}`);
    core.info(`Cache memory restored (from restore outputs): ${cacheMemoryRestored}`);
    core.info(`Missing tool report-as-failure: ${missingToolReportAsFailure}`);
    core.info(`Missing data report-as-failure: ${missingDataReportAsFailure}`);

    // Check if the agent timed out.
    // A job-level timeout sets agentConclusion to "timed_out".
    // A step-level timeout (timeout-minutes on the engine execution step) is detected by
    // the detect-copilot-errors step which checks for SIGTERM/SIGKILL/SIGINT signals
    // in the engine output and sets the agentic_engine_timeout output.
    const isTimedOut = (agentConclusion === "timed_out" || agenticEngineTimeout) && !shellExpansionGuardRejected;

    // Check if there are assignment errors (regardless of agent job status).
    // Use assignment_errors as the single source of truth because it includes
    // both hard failures and skipped(ignore-if-error) assignment errors.
    const hasAssignmentErrors = assignmentErrors.split("\n").some(line => line.trim());

    // Check if there are copilot assignment failures for created issues (regardless of agent job status)
    const hasAssignCopilotFailures = parseInt(assignCopilotFailureCount, 10) > 0;

    // Check if any frontmatter skill installs failed (regardless of agent job status)
    const hasSkillInstallFailures = parseInt(skillInstallFailureCount, 10) > 0;

    // Check if there are create_discussion errors (regardless of agent job status)
    const hasCreateDiscussionErrors = parseInt(createDiscussionErrorCount, 10) > 0;

    // Check if there are code-push failures (regardless of agent job status)
    const hasCodePushFailures = parseInt(codePushFailureCount, 10) > 0;

    // Check if the push_repo_memory job itself failed (e.g. permission or config errors)
    const hasPushRepoMemoryFailure = pushRepoMemoryResult === "failure";

    // Check if agent succeeded but produced no safe outputs
    let hasMissingSafeOutputs = false;
    let hasOnlyNoopOutputs = false;
    let hasReportIncomplete = false;
    // Tracks the case where agentConclusion is "failure" but the agent completed its work
    // successfully (terminal_reason: completed) and produced valid non-noop safe outputs.
    // This is a false-positive failure caused by a transient error after the agent's task
    // was done (e.g., post-processing step or AI model server returning a spurious error).
    let hasCompletedDespiteJobFailure = false;
    const { loadAgentOutput } = require("./load_agent_output.cjs");
    const agentOutputResult = loadAgentOutput();
    const taskCompleteRegistrationIssueOnlyReportIncomplete =
      agentOutputResult.success &&
      agentOutputResult.items &&
      agentOutputResult.items.some(item => item.type === "report_incomplete") &&
      hasAgentTerminalReasonCompleted() &&
      hasTaskLevelAgentOutput(agentOutputResult.items) &&
      agentOutputResult.items.filter(item => item.type === "report_incomplete").every(isTaskCompleteRegistrationIssue);

    if (agentConclusion === "success") {
      if (!agentOutputResult.success || !agentOutputResult.items || agentOutputResult.items.length === 0) {
        hasMissingSafeOutputs = true;
        core.info("Agent succeeded but produced no safe outputs");
      } else {
        // Check if all outputs are noop types
        const nonNoopItems = agentOutputResult.items.filter(item => item.type !== "noop");
        if (nonNoopItems.length === 0) {
          hasOnlyNoopOutputs = true;
          core.info("Agent succeeded with only noop outputs - this is not a failure");
        }
      }
    } else if (agentConclusion === "failure") {
      // The agent may have called noop successfully but the AI model server subsequently
      // returned a transient error (e.g. "Response was interrupted due to a server error"),
      // causing exit code 1. In that case we should not report a failure issue since the
      // agent completed its intended work.
      if (agentOutputResult.success && agentOutputResult.items && agentOutputResult.items.length > 0) {
        const nonNoopItems = agentOutputResult.items.filter(item => item.type !== "noop");
        if (nonNoopItems.length === 0) {
          hasOnlyNoopOutputs = true;
          core.info("Agent failed with exit code 1 but produced only noop outputs - treating as successful no-action (transient AI model error)");
        } else if (!nonNoopItems.some(item => item.type === "report_incomplete" && !isTaskCompleteRegistrationIssue(item))) {
          // The agent produced valid non-noop safe outputs (e.g. create_discussion) but the
          // job exit code is non-zero. If terminal_reason: completed is present in the log,
          // the failure was a transient error after the agent finished its task — do not report
          // a failure issue.
          if (hasAgentTerminalReasonCompleted()) {
            hasCompletedDespiteJobFailure = true;
            core.info("Agent failed with exit code 1 but completed successfully (terminal_reason: completed) with valid safe outputs — treating as completed (transient AI model error)");
          }
        }
      }
    }

    // Check if the agent emitted report_incomplete — a first-class signal that the task
    // could not be performed (e.g., MCP crash, missing auth, inaccessible repo).
    // This activates failure handling even when the agent exited 0 and emitted other
    // safe outputs such as add_comment, preventing a tool-failure narrative from being
    // classified as a successful review or other completed task.
    if (agentOutputResult.success && agentOutputResult.items && agentOutputResult.items.length > 0) {
      const reportIncompleteItems = agentOutputResult.items.filter(item => item.type === "report_incomplete");
      if (reportIncompleteItems.length > 0) {
        if (taskCompleteRegistrationIssueOnlyReportIncomplete) {
          core.info("Ignoring report_incomplete signal(s) caused only by task_complete registration trouble after successful task-level outputs");
        } else {
          hasReportIncomplete = true;
          core.info(`Agent emitted ${reportIncompleteItems.length} report_incomplete signal(s) - activating failure handling`);
          for (const item of reportIncompleteItems) {
            core.info(`  report_incomplete reason: ${item.reason}`);
          }
          // Continue after marking the step failed so optional issue reporting can still run.
          // eslint-disable-next-line gh-aw-custom/require-return-after-core-setfailed
          core.setFailed("Agent reported that it could not complete the task");
        }
      }
    }

    // Check if the agent emitted missing_tool messages — treated as agent failures so they
    // are surfaced in the failure issue comment rather than only in the output footer.
    let hasMissingTool = false;
    if (missingToolReportAsFailure && agentOutputResult.items) {
      const missingToolItems = agentOutputResult.items.filter(item => item.type === "missing_tool" && item.reason);
      if (missingToolItems.length > 0) {
        hasMissingTool = true;
        core.info(`Agent emitted ${missingToolItems.length} missing_tool message(s) - activating failure handling`);
      }
    } else if (!missingToolReportAsFailure) {
      core.info("Missing tool report-as-failure is disabled - missing_tool signals will not trigger failure handling");
    }

    // Check if the agent emitted missing_data messages — treated as agent failures so they
    // are surfaced in the failure issue comment rather than only in the output footer.
    let hasMissingData = false;
    if (missingDataReportAsFailure && agentOutputResult.items) {
      const missingDataItems = agentOutputResult.items.filter(item => item.type === "missing_data" && item.reason);
      if (missingDataItems.length > 0) {
        hasMissingData = true;
        core.info(`Agent emitted ${missingDataItems.length} missing_data message(s) - activating failure handling`);
      }
    } else if (!missingDataReportAsFailure) {
      core.info("Missing data report-as-failure is disabled - missing_data signals will not trigger failure handling");
    }

    const engineId = String(process.env.GH_AW_ENGINE_ID || "")
      .trim()
      .toLowerCase();
    const toolDenialsExceededEvents = engineId === "copilot" ? loadToolDenialsExceededEvents() : [];
    const hasToolDenialsExceeded = toolDenialsExceededEvents.length > 0;
    if (hasToolDenialsExceeded) {
      core.info(`Detected ${toolDenialsExceededEvents.length} guard.tool_denials_exceeded event(s) from Copilot SDK events.jsonl`);
    }
    const hasEngineRateLimit429 = agentConclusion === "failure" && !maxAICreditsExceeded && !aiCreditsRateLimitError && detectEngineRateLimit429Failure();

    // Detect cache-miss misconfiguration: the agent reported a missing_data with reason
    // "cache_memory_miss" after a cache restore matched. This indicates the prompt
    // is referencing an incorrect path inside the cache directory.
    // Check for items regardless of agentOutputResult.success so that cache-miss signals
    // emitted alongside other output are not missed when the agent job also fails.
    let hasCacheMissMisconfiguration = false;
    if (cacheMemoryEnabled && cacheMemoryRestored && agentOutputResult.items) {
      const cacheMissItems = agentOutputResult.items.filter(item => item.type === "missing_data" && item.reason === "cache_memory_miss");
      if (cacheMissItems.length > 0) {
        hasCacheMissMisconfiguration = true;
        core.info(`Cache-miss misconfiguration detected: ${cacheMissItems.length} missing_data item(s) with reason "cache_memory_miss" after cache restore matched an existing key`);
      }
    } else if (cacheMemoryEnabled && !cacheMemoryRestored) {
      core.info("Cache-memory is configured but no cache restore match was found; cache_memory_miss is treated as expected cache miss (actions/cache branch scoping and first-run behavior)");
    }

    // Only proceed if the agent job actually failed OR timed out OR there are assignment errors OR
    // create_discussion errors OR code-push failures OR push_repo_memory failed OR missing safe outputs
    // OR a GitHub App token minting step failed OR the lockdown check failed OR copilot assignment failed
    // OR the stale lock file check failed OR the agent reported task incompletion via report_incomplete
    // OR a cache-miss was detected after cache restore succeeded (configuration problem)
    // OR the agent reported missing tools or missing data (treated as agent failures by default)
    // OR the secret validation step failed (engine secret missing)
    // OR docker-sbx is configured but its required Docker Hub secrets are missing.
    // BUT skip if we only have noop outputs (that's a successful no-action scenario)
    const hasSecretVerificationFailed = secretVerificationResult === "failed";
    const hasDockerSbxSecretsFailed = dockerSbxSecretsResult === "failed";
    if (
      agentConclusion !== "failure" &&
      !isTimedOut &&
      !hasSecretVerificationFailed &&
      !hasDockerSbxSecretsFailed &&
      !hasAssignmentErrors &&
      !hasAssignCopilotFailures &&
      !hasSkillInstallFailures &&
      !hasCreateDiscussionErrors &&
      !hasCodePushFailures &&
      !hasPushRepoMemoryFailure &&
      !hasMissingSafeOutputs &&
      !hasAppTokenMintingFailed &&
      !hasLockdownCheckFailed &&
      !hasOAuthTokenCheckFailed &&
      !hasStaleLockFileFailed &&
      !hasDailyAICExceeded &&
      !hasReportIncomplete &&
      !hasCacheMissMisconfiguration &&
      !aiCreditsRateLimitError &&
      !maxAICreditsExceeded &&
      !hasMissingTool &&
      !hasMissingData &&
      !hasToolDenialsExceeded
    ) {
      core.info(
        `Agent job did not fail and no assignment/discussion/code-push/push-repo-memory/app-token/lockdown/oauth-token-check/stale-lock-file/daily-workflow-aic/ai-credits/max-ai-credits-exceeded/report-incomplete/cache-miss/missing-tool/missing-data/tool-denials-exceeded/secret-verification/docker-sbx-secret errors and has safe outputs (conclusion: ${agentConclusion}), skipping failure handling`
      );
      return;
    }

    // If we only have noop outputs (and no report_incomplete/cache-miss/missing-tool/data/tool-denials-exceeded), skip failure handling
    if (hasOnlyNoopOutputs && !hasReportIncomplete && !hasCacheMissMisconfiguration && !hasMissingTool && !hasMissingData && !hasToolDenialsExceeded) {
      core.info("Agent completed with only noop outputs - skipping failure handling");
      return;
    }

    // If the agent completed its work successfully (terminal_reason: completed) and produced
    // valid non-noop safe outputs despite the job's non-zero exit code, skip failure handling.
    // This prevents false-positive failure issues when a transient AI model error occurs
    // after the agent has already finished its task (e.g., create_discussion produced but
    // the server returned a spurious error on teardown).
    if (hasCompletedDespiteJobFailure && !hasReportIncomplete && !hasCacheMissMisconfiguration) {
      core.info("Agent completed with valid safe outputs despite job failure (terminal_reason: completed) — skipping failure handling");
      return;
    }

    // Check if failure issue reporting is disabled
    if (!reportFailureAsIssue) {
      core.info("Failure issue reporting is disabled (report-failure-as-issue: false), skipping failure issue creation");
      return;
    }

    // Check if the failure was due to PR checkout (e.g., PR was merged and branch deleted)
    // If checkout_pr_success is "false", skip creating an issue as this is expected behavior
    if (agentConclusion === "failure" && checkoutPRSuccess === "false") {
      core.info("Skipping failure handling - failure was due to PR checkout (likely PR merged)");
      return;
    }

    // Determine the failure issue repository destination.
    // SEC-005: GH_AW_FAILURE_ISSUE_REPO is set in the workflow frontmatter at compile time
    // and is therefore a trusted compile-time configuration value. No validateTargetRepo
    // allowlist check is required; the frontmatter trust boundary provides the equivalent
    // security guarantee.
    // If GH_AW_FAILURE_ISSUE_REPO is set, use that repo instead of the current repo
    const failureIssueRepo = process.env.GH_AW_FAILURE_ISSUE_REPO || "";
    let owner, repo;
    if (failureIssueRepo && failureIssueRepo.includes("/")) {
      const parts = failureIssueRepo.split("/");
      owner = parts[0];
      repo = parts[1];
      core.info(`Using configured failure issue repo: ${owner}/${repo}`);
    } else {
      ({ owner, repo } = context.repo);
    }

    /** @type {{ number: number, labels: Array<string | { name?: string | null }> } | null} */
    let failureIssue = null;
    const failureIssueNumber = Number.parseInt(process.env.GH_AW_FAILURE_ISSUE_NUMBER || "", 10);
    if (Number.isFinite(failureIssueNumber) && failureIssueNumber > 0) {
      const { data: issue } = await github.rest.issues.get({
        owner,
        repo,
        issue_number: failureIssueNumber,
      });
      if (issue.pull_request) {
        throw new Error(`Failure item #${failureIssueNumber} is a pull request, not an issue`);
      }
      if (issue.state !== "open") {
        throw new Error(`Failure issue #${failureIssueNumber} is not open`);
      }
      failureIssue = issue;
      core.info(`Reusing failure issue #${failureIssueNumber}`);
    }

    // Try to find a pull request for the current branch
    const pullRequest = await findPullRequestForCurrentBranch();
    const currentBranch = getCurrentBranch();

    // Generate history URL for linking to all failure issues created by this workflow
    const historyUrl = generateHistoryUrl({
      owner,
      repo,
      itemType: "issue",
      workflowId: workflowID,
    });
    const actionFailureIssueExpiresHours = getActionFailureIssueExpiresHours();

    // Check if parent issue creation is enabled (defaults to false)
    const groupReports = process.env.GH_AW_GROUP_REPORTS === "true";

    // Ensure parent issue exists first (only if enabled)
    let parentIssue;
    if (groupReports) {
      try {
        parentIssue = await ensureParentIssue(null, owner, repo, actionFailureIssueExpiresHours);
      } catch (error) {
        core.warning(`Could not create parent issue, proceeding without parent: ${getErrorMessage(error)}`);
        // Continue without parent issue
      }
    } else {
      core.info("Parent issue creation is disabled (group-reports: false)");
    }

    // Sanitize workflow name for title
    const sanitizedWorkflowName = sanitizeContent(workflowName, { maxLength: 100 });
    const issueTitle = buildFailureIssueTitle({
      workflowName: sanitizedWorkflowName,
      isTimedOut,
      hasMissingSafeOutputs,
      hasReportIncomplete,
      hasMissingTool,
      hasMissingData,
      hasCacheMissMisconfiguration,
      hasToolDenialsExceeded,
      hasAppTokenMintingFailed,
      hasLockdownCheckFailed,
      hasOAuthTokenCheckFailed,
      hasStaleLockFileFailed,
      hasDailyAICExceeded,
      aiCreditsRateLimitError,
      hasEngineRateLimit429,
      maxAICreditsExceeded,
      shellExpansionGuardRejected,
      hasAssignmentErrors,
      http400ResponseError,
      unknownModelAICredits,
      missingModelPricingError,
      missingModelPricingModelName,
      hasDockerSbxSecretsFailed,
    });
    const failureCategories = buildFailureMatchCategories({
      agentConclusion,
      isTimedOut,
      hasAssignmentErrors,
      hasAssignCopilotFailures,
      hasSkillInstallFailures,
      hasCreateDiscussionErrors,
      hasCodePushFailures,
      hasRepoMemoryValidationErrors: repoMemoryValidationErrors.length > 0,
      hasPushRepoMemoryFailure,
      hasMissingSafeOutputs,
      hasReportIncomplete,
      hasMissingTool,
      hasToolDenialsExceeded,
      hasMissingData,
      hasCacheMissMisconfiguration,
      secretVerificationFailed: hasSecretVerificationFailed,
      hasDockerSbxSecretsFailed,
      inferenceAccessError,
      mcpPolicyError,
      modelNotSupportedError,
      http400ResponseError,
      aiCreditsRateLimitError,
      hasEngineRateLimit429,
      unknownModelAICredits,
      missingModelPricingError,
      maxAICreditsExceeded,
      hasAppTokenMintingFailed,
      hasLockdownCheckFailed,
      hasOAuthTokenCheckFailed,
      hasStaleLockFileFailed,
      hasDailyAICExceeded,
      isAWFFirewallStartupFailed: detectAWFFirewallStartupFailureFromLog(),
    });

    // Persist failure categories so the OTLP conclusion span can record them
    // as gh-aw.failure.categories for metrics and counting in OTLP backends.
    try {
      fs.mkdirSync(path.dirname(FAILURE_CATEGORIES_PATH), { recursive: true });
      fs.writeFileSync(FAILURE_CATEGORIES_PATH, JSON.stringify(failureCategories));
    } catch (writeError) {
      core.warning(`Failed to write failure categories: ${getErrorMessage(writeError)}`);
    }

    // Check if failure categories match the filter (if configured)
    // Logic:
    // 1. If only excluded categories are specified: report all EXCEPT those categories
    // 2. If only included categories are specified: report ONLY those categories
    // 3. If both are specified: report categories that are included AND not excluded
    if (failureCategoriesFilter || failureExcludedCategoriesFilter) {
      const includeCategories = Array.isArray(failureCategoriesFilter) ? failureCategoriesFilter : [];
      const excludeCategories = Array.isArray(failureExcludedCategoriesFilter) ? failureExcludedCategoriesFilter : [];
      const hasIncludeFilter = includeCategories.length > 0;
      const hasExcludeFilter = excludeCategories.length > 0;

      let shouldCreateIssue = false;

      if (hasIncludeFilter && hasExcludeFilter) {
        // Both filters: must match include AND not match exclude
        const hasIncludedCategory = failureCategories.some(cat => includeCategories.includes(cat));
        const hasExcludedCategory = failureCategories.some(cat => excludeCategories.includes(cat));
        shouldCreateIssue = hasIncludedCategory && !hasExcludedCategory;
        if (!shouldCreateIssue) {
          core.info(`Skipping failure issue creation: categories don't match filters. Categories: [${failureCategories.join(", ")}], Include: [${includeCategories.join(", ")}], Exclude: [${excludeCategories.join(", ")}]`);
        } else {
          core.info(`Failure categories match filters, proceeding with issue creation. Include: [${includeCategories.join(", ")}], Exclude: [${excludeCategories.join(", ")}]`);
        }
      } else if (hasIncludeFilter) {
        // Only include filter: must match at least one included category
        shouldCreateIssue = failureCategories.some(cat => includeCategories.includes(cat));
        if (!shouldCreateIssue) {
          core.info(`Skipping failure issue creation: no failure categories match include filter. Categories: [${failureCategories.join(", ")}], Include: [${includeCategories.join(", ")}]`);
        } else {
          core.info(`Failure categories match include filter, proceeding with issue creation. Matching categories: [${failureCategories.filter(cat => includeCategories.includes(cat)).join(", ")}]`);
        }
      } else if (hasExcludeFilter) {
        // Only exclude filter: must NOT match any excluded category
        const hasExcludedCategory = failureCategories.some(cat => excludeCategories.includes(cat));
        shouldCreateIssue = !hasExcludedCategory;
        if (!shouldCreateIssue) {
          core.info(`Skipping failure issue creation: failure categories match exclude filter. Categories: [${failureCategories.join(", ")}], Exclude: [${excludeCategories.join(", ")}]`);
        } else {
          core.info(`Failure categories don't match exclude filter, proceeding with issue creation. Categories: [${failureCategories.join(", ")}]`);
        }
      }

      if (!shouldCreateIssue) {
        return;
      }
    }

    core.info(`Checking for existing issue with precise failure metadata for title: "${issueTitle}"`);

    try {
      const existingIssue = failureIssue
        ? null
        : await findExistingFailureIssue({
            owner,
            repo,
            workflowId: workflowID,
            failureCategories,
          });

      // Build missing model pricing context once; both issue-create and issue-comment
      // paths render the same remediation block and should not refetch models.dev.
      const missingModelPricingContext = await buildMissingModelPricingContext(missingModelPricingError, missingModelPricingModelName, process.env.GH_AW_ENGINE_ID || "");

      if (existingIssue) {
        // Issue exists, add a comment
        core.info(`Found existing issue #${existingIssue.number}: ${existingIssue.html_url}`);

        // Read comment template
        const commentTemplatePath = getPromptPath("agent_failure_comment.md");
        const commentTemplate = fs.readFileSync(commentTemplatePath, "utf8");

        // Extract run ID from URL (e.g., https://github.com/owner/repo/actions/runs/123 -> "123")
        const runId = extractRunId(runUrl);

        // Build assignment errors context
        const assignmentErrorsContext = buildAssignmentErrorsContext(assignmentErrors);

        // Build create_discussion errors context
        const createDiscussionErrorsContext = hasCreateDiscussionErrors ? buildCreateDiscussionErrorsContext(createDiscussionErrors) : "";

        // Build code-push failure context
        const codePushFailureContext = hasCodePushFailures ? buildCodePushFailureContext(codePushFailureErrors, pullRequest, runUrl) : "";

        // Build repo-memory validation errors context
        let repoMemoryValidationContext = "";
        if (repoMemoryValidationErrors.length > 0) {
          repoMemoryValidationContext = buildWarningAlertLine("Repo-Memory Validation Failed", "Invalid file types detected in repo-memory.");
          if (pullRequest) {
            repoMemoryValidationContext += `\n\n**Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})`;
          }
          repoMemoryValidationContext += "\n\n**Validation Errors:**\n";
          for (const { memoryID, errorMessage } of repoMemoryValidationErrors) {
            repoMemoryValidationContext += `- Memory "${memoryID}": ${errorMessage}\n`;
          }
          repoMemoryValidationContext += "\n";
        }

        // Build push_repo_memory job failure context
        const pushRepoMemoryFailureContext = buildPushRepoMemoryFailureContext(hasPushRepoMemoryFailure, repoMemoryPatchSizeExceededIDs, runUrl);

        // Build missing_data context (only when report-as-failure is enabled for this signal type)
        const missingDataContext = missingDataReportAsFailure ? buildMissingDataContext(cacheMemoryEnabled, cacheMemoryRestored, agentOutputResult.items) : "";

        // Build tool-denials-exceeded guard context from events.jsonl
        const toolDenialsExceededContext = buildToolDenialsExceededContext(toolDenialsExceededEvents, workflowID);
        // Build missing_tool context (only when report-as-failure is enabled for this signal type).
        // Suppress when tool-denials-exceeded is present: excessive tool denials take precedence.
        const missingToolContext = missingToolReportAsFailure && !hasToolDenialsExceeded ? buildMissingToolContext(agentOutputResult.items) : "";

        // Build permission denied context (denied commands list + fix prompt).
        // Suppress when tool-denials-exceeded is present: that context already covers the denied errors.
        const permissionDeniedContext = hasToolDenialsExceeded ? "" : buildPermissionDeniedContext(agentOutputResult.items, workflowID);
        // Build report_incomplete context
        const reportIncompleteContext = buildReportIncompleteContext(agentOutputResult.items);

        // Build missing safe outputs context
        let missingSafeOutputsContext = "";
        if (hasMissingSafeOutputs) {
          missingSafeOutputsContext = buildWarningAlertLine("No Safe Outputs Generated", "The agent job succeeded but did not produce any safe outputs.");
          if (pullRequest) {
            missingSafeOutputsContext += `\n\n**Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})`;
          }
          missingSafeOutputsContext += "\n\nThis typically indicates:\n";
          missingSafeOutputsContext += "- The safe output server failed to run\n";
          missingSafeOutputsContext += "- The prompt failed to generate any meaningful result\n";
          missingSafeOutputsContext += "- The agent should have called `noop` to explicitly indicate no action was taken\n\n";
        }

        // Build fork context hint
        const forkContext = buildForkContextHint();

        // Build engine failure context (surfaces terminal errors from agent-stdio.log).
        // Suppress when tool-denials-exceeded is present: the engine termination is a
        // direct consequence of the SDK hitting the denial threshold, so the tool-denials
        // context is the more actionable signal.
        // Also suppress when missing-model-pricing is detected: the pricing error is the
        // root cause and the engine error block would be redundant noise.
        const engineFailureContext = shouldBuildEngineFailureContext(agentConclusion, hasToolDenialsExceeded, isTimedOut, missingModelPricingError, shellExpansionGuardRejected)
          ? buildEngineFailureContext({
              suppressEngineRateLimit429: maxAICreditsExceeded,
              maxCacheMissesExceeded,
            })
          : "";
        // Build timeout context
        const timeoutContext = buildTimeoutContext(isTimedOut, timeoutMinutes);

        // Build inference access error context
        const inferenceAccessErrorContext = buildInferenceAccessErrorContext(inferenceAccessError);

        // Build MCP policy error context
        const mcpPolicyErrorContext = buildMCPPolicyErrorContext(mcpPolicyError);

        // Build model not supported error context
        const modelNotSupportedErrorContext = buildModelNotSupportedErrorContext(modelNotSupportedError);
        const http400ResponseErrorContext = buildHTTP400ResponseErrorContext(http400ResponseError);
        const aiCreditsRateLimitErrorContext = buildAICreditsRateLimitErrorContext(aiCreditsRateLimitError || maxAICreditsExceeded, aiCredits, maxAICredits, runUrl, maxAICreditsExceeded);
        const unknownModelAICreditsContext = buildUnknownModelAICreditsContext(unknownModelAICredits);

        // Build GitHub App token minting failure context
        const appTokenMintingFailedContext = buildAppTokenMintingFailedContext(hasAppTokenMintingFailed);

        // Build lockdown check failure context
        const lockdownCheckFailedContext = buildLockdownCheckFailedContext(hasLockdownCheckFailed);

        // Build OAuth token check failure context
        const oauthTokenCheckFailedContext = buildOAuthTokenCheckFailedContext(hasOAuthTokenCheckFailed, runUrl);

        // Build stale lock file failure context
        const staleLockFileFailedContext = buildStaleLockFileFailedContext(hasStaleLockFileFailed);
        const dailyAICExceededContext = buildDailyAICExceededContext(hasDailyAICExceeded, dailyAICTotal, dailyAICThreshold);

        // Build copilot assignment failure context for created issues
        const assignCopilotFailureContext = buildAssignCopilotFailureContext(hasAssignCopilotFailures, assignCopilotErrors);

        // Build skill install failure context
        const skillInstallFailureContext = buildSkillInstallFailureContext(hasSkillInstallFailures, skillInstallErrors);

        // Build credential auth error context (firewall audit.jsonl 401/403 from provider endpoints)
        const credentialAuthErrorContext = buildCredentialAuthErrorContext();

        // Create template context
        const templateContext = {
          run_url: runUrl,
          run_id: runId,
          workflow_name: workflowName,
          workflow_source: workflowSource,
          workflow_source_url: workflowSourceURL,
          secret_verification_failed: String(hasSecretVerificationFailed),
          secret_verification_context: buildSecretVerificationContext(secretVerificationResult, engineSecretFailureMessage),
          docker_sbx_secrets_context: buildDockerSbxSecretsContext(dockerSbxSecretsResult),
          credential_auth_error_context: credentialAuthErrorContext,
          assignment_errors_context: assignmentErrorsContext,
          assign_copilot_failure_context: assignCopilotFailureContext,
          skill_install_failure_context: skillInstallFailureContext,
          create_discussion_errors_context: createDiscussionErrorsContext,
          code_push_failure_context: codePushFailureContext,
          repo_memory_validation_context: repoMemoryValidationContext,
          push_repo_memory_failure_context: pushRepoMemoryFailureContext,
          missing_data_context: missingDataContext,
          missing_tool_context: missingToolContext,
          permission_denied_context: permissionDeniedContext,
          tool_denials_exceeded_context: toolDenialsExceededContext,
          report_incomplete_context: reportIncompleteContext,
          missing_safe_outputs_context: missingSafeOutputsContext,
          engine_failure_context: engineFailureContext,
          timeout_context: timeoutContext,
          fork_context: forkContext,
          inference_access_error_context: inferenceAccessErrorContext,
          mcp_policy_error_context: mcpPolicyErrorContext,
          model_not_supported_error_context: modelNotSupportedErrorContext,
          http_400_response_error_context: http400ResponseErrorContext,
          ai_credits_rate_limit_error_context: aiCreditsRateLimitErrorContext,
          unknown_model_ai_credits_context: unknownModelAICreditsContext,
          missing_model_pricing_context: missingModelPricingContext,
          shell_expansion_guard_rejected_context: buildShellExpansionGuardRejectedContext(shellExpansionGuardRejected),
          app_token_minting_failed_context: appTokenMintingFailedContext,
          lockdown_check_failed_context: lockdownCheckFailedContext,
          oauth_token_check_failed_context: oauthTokenCheckFailedContext,
          stale_lock_file_failed_context: staleLockFileFailedContext,
          daily_ai_credits_exceeded_context: dailyAICExceededContext,
        };

        // Render the comment template
        const commentBody = renderTemplate(commentTemplate, templateContext);

        // Generate footer for the comment using templated message
        const ctx = {
          workflowName,
          runUrl,
          workflowSource,
          workflowSourceUrl: workflowSourceURL,
          historyUrl: historyUrl || undefined,
          aiCredits,
        };
        core.info(`Generating failure comment footer with aiCredits context: ${aiCredits || "(none)"}`);
        const footer = getFooterAgentFailureCommentMessage(ctx);

        // Prepend detection caution alert (when present) so it appears first in the comment body
        const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);
        const fullCommentBodyRaw = detectionCaution ? `${detectionCaution}\n\n${commentBody}\n\n${footer}` : `${commentBody}\n\n${footer}`;

        // Combine comment body with footer
        const fullCommentBody = sanitizeContent(fullCommentBodyRaw, { maxLength: 65000 });

        await github.rest.issues.createComment({
          owner,
          repo,
          issue_number: existingIssue.number,
          body: fullCommentBody,
        });

        core.info(`✓ Added comment to existing issue #${existingIssue.number}`);

        // Cascade detection: check for storm of failures within the window
        await detectAndHandleFailureCascade(owner, repo, existingIssue.number);
      } else {
        // No existing issue, create a new one
        core.info("No existing issue found, creating a new one");
        const cappedCategories = failureIssue
          ? []
          : await getCappedFailureCategories({
              owner,
              repo,
              failureCategories,
            });
        if (cappedCategories.length > 0) {
          const summary = cappedCategories.map(({ category, count }) => `${category} (${count}/${FAILURE_ISSUE_CATEGORY_DAILY_CAP})`).join(", ");
          core.warning(`Daily per-category issue cap reached for ${summary}.`);
          core.info(`Summarize-and-stop: skipping new issue creation because category cap was reached in the last ${FAILURE_ISSUE_DEDUP_WINDOW_HOURS}h.`);

          // Create or reuse a centralized rollup issue and add a comment so the failure is
          // still tracked rather than silently dropped.
          try {
            const rollupIssue = await findOrCreateDailyCapRollupIssue(owner, repo);
            if (rollupIssue) {
              const commentBody = sanitizeContent(
                renderTemplateFromFile(getPromptPath("daily_cap_rollup_comment.md"), {
                  workflow_name: workflowName,
                  run_url: runUrl,
                  summary,
                  cap: FAILURE_ISSUE_CATEGORY_DAILY_CAP,
                  window_hours: FAILURE_ISSUE_DEDUP_WINDOW_HOURS,
                }),
                { maxLength: 65000 }
              );
              await github.rest.issues.createComment({
                owner,
                repo,
                issue_number: rollupIssue.number,
                body: commentBody,
              });
              core.info(`✓ Added cap-exceeded comment to rollup issue #${rollupIssue.number}: ${rollupIssue.html_url}`);
            }
          } catch (err) {
            core.warning(`Could not update daily cap rollup issue: ${getErrorMessage(err)}`);
          }
          return;
        }

        // Read issue template
        const issueTemplatePath = getPromptPath("agent_failure_issue.md");
        const issueTemplate = fs.readFileSync(issueTemplatePath, "utf8");

        // Build assignment errors context
        const assignmentErrorsContext = buildAssignmentErrorsContext(assignmentErrors);

        // Build create_discussion errors context
        const createDiscussionErrorsContext = hasCreateDiscussionErrors ? buildCreateDiscussionErrorsContext(createDiscussionErrors) : "";

        // Build code-push failure context
        const codePushFailureContext = hasCodePushFailures ? buildCodePushFailureContext(codePushFailureErrors, pullRequest, runUrl) : "";

        // Build repo-memory validation errors context
        let repoMemoryValidationContext = "";
        if (repoMemoryValidationErrors.length > 0) {
          repoMemoryValidationContext = buildWarningAlertLine("Repo-Memory Validation Failed", "Invalid file types detected in repo-memory.");
          if (pullRequest) {
            repoMemoryValidationContext += `\n\n**Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})`;
          }
          repoMemoryValidationContext += "\n\n**Validation Errors:**\n";
          for (const { memoryID, errorMessage } of repoMemoryValidationErrors) {
            repoMemoryValidationContext += `- Memory "${memoryID}": ${errorMessage}\n`;
          }
          repoMemoryValidationContext += "\n";
        }

        // Build push_repo_memory job failure context
        const pushRepoMemoryFailureContext = buildPushRepoMemoryFailureContext(hasPushRepoMemoryFailure, repoMemoryPatchSizeExceededIDs, runUrl);

        // Build missing_data context (only when report-as-failure is enabled for this signal type)
        const missingDataContext = missingDataReportAsFailure ? buildMissingDataContext(cacheMemoryEnabled, cacheMemoryRestored, agentOutputResult.items) : "";

        // Build tool-denials-exceeded guard context from events.jsonl
        const toolDenialsExceededContext = buildToolDenialsExceededContext(toolDenialsExceededEvents, workflowID);
        // Build missing_tool context (only when report-as-failure is enabled for this signal type).
        // Suppress when tool-denials-exceeded is present: excessive tool denials take precedence.
        const missingToolContext = missingToolReportAsFailure && !hasToolDenialsExceeded ? buildMissingToolContext(agentOutputResult.items) : "";

        // Build report_incomplete context
        const reportIncompleteContext = buildReportIncompleteContext(agentOutputResult.items);

        // Build permission denied context (denied commands list + fix prompt).
        // Suppress when tool-denials-exceeded is present: that context already covers the denied errors.
        const permissionDeniedContext = hasToolDenialsExceeded ? "" : buildPermissionDeniedContext(agentOutputResult.items, workflowID);

        // Build missing safe outputs context
        let missingSafeOutputsContext = "";
        if (hasMissingSafeOutputs) {
          missingSafeOutputsContext = buildWarningAlertLine("No Safe Outputs Generated", "The agent job succeeded but did not produce any safe outputs.");
          if (pullRequest) {
            missingSafeOutputsContext += `\n\n**Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})`;
          }
          missingSafeOutputsContext += "\n\nThis typically indicates:\n";
          missingSafeOutputsContext += "- The safe output server failed to run\n";
          missingSafeOutputsContext += "- The prompt failed to generate any meaningful result\n";
          missingSafeOutputsContext += "- The agent should have called `noop` to explicitly indicate no action was taken\n\n";
        }

        // Build fork context hint
        const forkContext = buildForkContextHint();

        // Build engine failure context (surfaces terminal errors from agent-stdio.log).
        // Suppress when tool-denials-exceeded is present: the engine termination is a
        // direct consequence of the SDK hitting the denial threshold, so the tool-denials
        // context is the more actionable signal.
        // Also suppress when missing-model-pricing is detected: the pricing error is the
        // root cause and the engine error block would be redundant noise.
        const engineFailureContext = shouldBuildEngineFailureContext(agentConclusion, hasToolDenialsExceeded, isTimedOut, missingModelPricingError, shellExpansionGuardRejected)
          ? buildEngineFailureContext({ suppressEngineRateLimit429: maxAICreditsExceeded })
          : "";

        // Build timeout context
        const timeoutContext = buildTimeoutContext(isTimedOut, timeoutMinutes);

        // Build inference access error context
        const inferenceAccessErrorContext = buildInferenceAccessErrorContext(inferenceAccessError);

        // Build MCP policy error context
        const mcpPolicyErrorContext = buildMCPPolicyErrorContext(mcpPolicyError);

        // Build model not supported error context
        const modelNotSupportedErrorContext = buildModelNotSupportedErrorContext(modelNotSupportedError);
        const http400ResponseErrorContext = buildHTTP400ResponseErrorContext(http400ResponseError);
        const aiCreditsRateLimitErrorContext = buildAICreditsRateLimitErrorContext(aiCreditsRateLimitError || maxAICreditsExceeded, aiCredits, maxAICredits, runUrl, maxAICreditsExceeded);
        const unknownModelAICreditsContext = buildUnknownModelAICreditsContext(unknownModelAICredits);

        // Build GitHub App token minting failure context
        const appTokenMintingFailedContext = buildAppTokenMintingFailedContext(hasAppTokenMintingFailed);

        // Build lockdown check failure context
        const lockdownCheckFailedContext = buildLockdownCheckFailedContext(hasLockdownCheckFailed);

        // Build OAuth token check failure context
        const oauthTokenCheckFailedContext = buildOAuthTokenCheckFailedContext(hasOAuthTokenCheckFailed, runUrl);

        // Build stale lock file failure context
        const staleLockFileFailedContext = buildStaleLockFileFailedContext(hasStaleLockFileFailed);
        const dailyAICExceededContext = buildDailyAICExceededContext(hasDailyAICExceeded, dailyAICTotal, dailyAICThreshold);

        // Build copilot assignment failure context for created issues
        const assignCopilotFailureContext = buildAssignCopilotFailureContext(hasAssignCopilotFailures, assignCopilotErrors);

        // Build skill install failure context
        const skillInstallFailureContext = buildSkillInstallFailureContext(hasSkillInstallFailures, skillInstallErrors);

        // Build credential auth error context (firewall audit.jsonl 401/403 from provider endpoints)
        const credentialAuthErrorContext = buildCredentialAuthErrorContext();

        // Build optimize token consumption context (shown when a guardrail was the failure root cause)
        const optimizeTokenConsumptionContext = buildOptimizeTokenConsumptionContext({ maxAICreditsExceeded, hasDailyAICExceeded, hasToolDenialsExceeded, isTimedOut, runUrl });

        // Create template context with sanitized workflow name
        const templateContext = {
          workflow_name: sanitizedWorkflowName,
          workflow_id: workflowID,
          run_url: runUrl,
          workflow_source_url: workflowSourceURL || "#",
          branch: currentBranch,
          pull_request_info: pullRequest ? `  \n**Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})` : "",
          secret_verification_failed: String(hasSecretVerificationFailed),
          secret_verification_context: buildSecretVerificationContext(secretVerificationResult, engineSecretFailureMessage),
          docker_sbx_secrets_context: buildDockerSbxSecretsContext(dockerSbxSecretsResult),
          credential_auth_error_context: credentialAuthErrorContext,
          assignment_errors_context: assignmentErrorsContext,
          assign_copilot_failure_context: assignCopilotFailureContext,
          skill_install_failure_context: skillInstallFailureContext,
          create_discussion_errors_context: createDiscussionErrorsContext,
          code_push_failure_context: codePushFailureContext,
          repo_memory_validation_context: repoMemoryValidationContext,
          push_repo_memory_failure_context: pushRepoMemoryFailureContext,
          missing_data_context: missingDataContext,
          missing_tool_context: missingToolContext,
          permission_denied_context: permissionDeniedContext,
          tool_denials_exceeded_context: toolDenialsExceededContext,
          report_incomplete_context: reportIncompleteContext,
          missing_safe_outputs_context: missingSafeOutputsContext,
          engine_failure_context: engineFailureContext,
          timeout_context: timeoutContext,
          fork_context: forkContext,
          inference_access_error_context: inferenceAccessErrorContext,
          mcp_policy_error_context: mcpPolicyErrorContext,
          model_not_supported_error_context: modelNotSupportedErrorContext,
          http_400_response_error_context: http400ResponseErrorContext,
          ai_credits_rate_limit_error_context: aiCreditsRateLimitErrorContext,
          unknown_model_ai_credits_context: unknownModelAICreditsContext,
          missing_model_pricing_context: missingModelPricingContext,
          shell_expansion_guard_rejected_context: buildShellExpansionGuardRejectedContext(shellExpansionGuardRejected),
          app_token_minting_failed_context: appTokenMintingFailedContext,
          lockdown_check_failed_context: lockdownCheckFailedContext,
          oauth_token_check_failed_context: oauthTokenCheckFailedContext,
          stale_lock_file_failed_context: staleLockFileFailedContext,
          daily_ai_credits_exceeded_context: dailyAICExceededContext,
          optimize_token_consumption_context: optimizeTokenConsumptionContext,
        };

        // Render the issue template
        const issueBodyContent = renderTemplate(issueTemplate, templateContext);

        // Generate footer for the issue using templated message
        const ctx = {
          workflowName,
          runUrl,
          workflowSource,
          workflowSourceUrl: workflowSourceURL,
          historyUrl: historyUrl || undefined,
          aiCredits,
        };
        core.info(`Generating failure issue footer with aiCredits context: ${aiCredits || "(none)"}`);
        const footer = getFooterAgentFailureIssueMessage(ctx);
        const failureMatchMarker = generateFailureMatchMarker({
          workflowId: workflowID,
          branch: currentBranch,
          pullRequestNumber: pullRequest?.number,
          failureCategories,
        });

        // Add expiration marker inside the quoted footer section using helper
        const footerWithExpires = generateFooterWithExpiration({
          footerText: footer,
          expiresHours: actionFailureIssueExpiresHours,
          suffix: `\n\n${generateXMLMarker(workflowName, runUrl)}\n${failureMatchMarker}`,
        });

        // Prepend detection caution alert (when present) so it appears first in the issue body
        const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);

        // Combine issue body with footer
        const bodyLines = detectionCaution ? [detectionCaution, "", issueBodyContent, "", footerWithExpires] : [issueBodyContent, "", footerWithExpires];
        const issueBody = bodyLines.join("\n");

        let newIssue;
        if (failureIssue) {
          const labels = [...new Set([...(failureIssue.labels || []).map(label => (typeof label === "string" ? label : label.name || "")).filter(Boolean), "agentic-workflows"])];
          newIssue = await github.rest.issues.update({
            owner,
            repo,
            issue_number: failureIssue.number,
            title: issueTitle,
            body: issueBody,
            labels,
            state: "open",
            headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
          });
          core.info(`✓ Reused failure issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
        } else {
          newIssue = await github.rest.issues.create({
            owner,
            repo,
            title: issueTitle,
            body: issueBody,
            labels: ["agentic-workflows"],
            headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
          });
          core.info(`✓ Created new issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
        }

        setFailureIssueOutputs(newIssue.data);

        // Link as sub-issue to parent if parent issue was created
        if (parentIssue) {
          try {
            await linkSubIssue(parentIssue.node_id, newIssue.data.node_id, parentIssue.number, newIssue.data.number);
          } catch (error) {
            core.warning(`Could not link issue as sub-issue: ${getErrorMessage(error)}`);
            // Continue even if linking fails
          }
        }

        // Cascade detection: check for storm of failures within the window
        await detectAndHandleFailureCascade(owner, repo, newIssue.data.number);
      }
    } catch (error) {
      if (isIssueWritePermissionError(error)) {
        core.info(`Skipping failure tracking issue creation/update: token lacks issues:write permission (${getErrorMessage(error)})`);
      } else {
        core.warning(`Failed to create or update failure tracking issue: ${getErrorMessage(error)}`);
      }
      // Don't fail the workflow if we can't create the issue
    }
  } catch (error) {
    core.warning(`Error in handle_agent_failure: ${getErrorMessage(error)}`);
    // Don't fail the workflow
  }
}

module.exports = {
  main,
  buildCodePushFailureContext,
  buildPushRepoMemoryFailureContext,
  buildAppTokenMintingFailedContext,
  buildLockdownCheckFailedContext,
  buildOAuthTokenCheckFailedContext,
  buildStaleLockFileFailedContext,
  buildDailyAICExceededContext,
  buildOptimizeTokenConsumptionContext,
  buildTimeoutContext,
  shouldBuildEngineFailureContext,
  isIssueWritePermissionError,
  setFailureIssueOutputs,
  buildAssignCopilotFailureContext,
  buildEngineFailureContext,
  detectAWFFirewallStartupFailureFromLog,
  buildReportIncompleteContext,
  buildMCPPolicyErrorContext,
  buildModelNotSupportedErrorContext,
  buildHTTP400ResponseErrorContext,
  buildMissingDataContext,
  resolveCacheMemoryRestored,
  buildMissingToolContext,
  buildPermissionDeniedContext,
  normalizeDeniedPermissionCommand,
  loadToolDenialsExceededEvents,
  buildToolDenialsExceededContext,
  buildCredentialAuthErrorContext,
  buildAssignmentErrorsContext,
  buildAICreditsRateLimitErrorContext,
  buildUnknownModelAICreditsContext,
  buildMissingModelPricingContext,
  buildModelPricingFrontmatterSnippet,
  fetchModelPricingFromModelsDev,
  hasEngineMaxRunsExceededSignal,
  hasEngineRateLimit429Signal,
  hasEngineRateLimit429InOTELMirror,
  detectEngineRateLimit429Failure,
  buildEngineMaxRunsExceededContext,
  buildEngineRateLimit429Context,
  hasEngineMaxCacheMissesExceededSignal,
  buildEngineMaxCacheMissesExceededContext,
  hasShellExpansionGuardRejectedSignal,
  buildShellExpansionGuardRejectedContext,
  readTokenUsageMarkdown,
  parseFirewallAuthErrors,
  parseMaxAICreditsFromAuditLog,
  parseAICreditsErrorInfoFromAuditLog,
  getActionFailureIssueExpiresHours,
  hasAgentTerminalReasonCompleted,
  detectAndHandleFailureCascade,
  findRecentFailureIssues,
  buildSecretVerificationContext,
  buildDockerSbxSecretsContext,
  CASCADE_WINDOW_MINUTES,
  CASCADE_WINDOW_MS,
  CASCADE_THRESHOLD,
  CASCADE_LABEL,
  CASCADE_ROLLUP_LABEL,
  CASCADE_ROLLUP_TITLE,
  FAILURE_TITLE_PATTERN,
  buildFailureMatchCategories,
  buildFailureIssueTitle,
  FAILURE_CATEGORIES_PATH,
};
