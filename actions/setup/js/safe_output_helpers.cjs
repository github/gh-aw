// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Shared helper functions for safe-output scripts
 * Provides common validation and target resolution logic
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { matchesSimpleGlob } = require("./glob_pattern_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");

/**
 * Parse a comma-separated list of allowed items from environment variable
 * @param {string|undefined} envValue - Environment variable value
 * @returns {string[]|undefined} Array of allowed items, or undefined if no restrictions
 */
function parseAllowedItems(envValue) {
  const trimmed = envValue?.trim();
  if (!trimmed) {
    return undefined;
  }
  return trimmed
    .split(",")
    .map(item => item.trim())
    .filter(item => item);
}

/**
 * Parse and validate max count from environment variable
 * @param {string|undefined} envValue - Environment variable value
 * @param {number} defaultValue - Default value if not specified
 * @returns {{valid: true, value: number} | {valid: false, error: string}} Validation result
 */
function parseMaxCount(envValue, defaultValue = 3) {
  if (!envValue) {
    return { valid: true, value: defaultValue };
  }

  const parsed = parseInt(envValue, 10);
  if (Number.isNaN(parsed) || parsed < 1) {
    return {
      valid: false,
      error: `Invalid max value: ${envValue}. Must be a positive integer`,
    };
  }

  return { valid: true, value: parsed };
}

/**
 * Resolve the target number (issue/PR) based on configuration and context
 *
 * This function determines which issue or pull request to target based on:
 * - The handler's support for issues vs PRs (supportsPR, supportsIssue)
 * - The target configuration ("triggering", "*", or explicit number)
 * - The workflow context (issue event, PR event, etc.)
 * - Fields in the safe output item (issue_number, pull_request_number, item_number)
 *
 * @param {Object} params - Resolution parameters
 * @param {string} params.targetConfig - Target configuration ("triggering", "*", or explicit number)
 * @param {any} params.item - Safe output item with optional item_number, issue_number, or pull_request_number
 * @param {any} params.context - GitHub Actions context
 * @param {string} params.itemType - Type of item being processed (for error messages)
 * @param {boolean} [params.supportsPR] - When true, handler supports BOTH issues and PRs (e.g., add_labels)
 *                                         When false, handler supports PRs ONLY (e.g., add_reviewers)
 * @param {boolean} [params.supportsIssue] - When true, handler supports issues ONLY (e.g., update_issue)
 *                                           Optional; defaults to false.
 * @returns {{success: true, number: number, contextType: string} | {success: false, error: string, shouldFail: boolean}} Resolution result
 */
function resolveTarget(params) {
  const { targetConfig, item, context, itemType, supportsPR = false, supportsIssue = false } = params;
  let invocationContext;
  try {
    invocationContext = resolveInvocationContext(context);
  } catch (err) {
    return {
      success: false,
      error: `Failed to resolve invocation context for ${itemType}: ${getErrorMessage(err)}`,
      shouldFail: true,
    };
  }
  const effectiveEventName = invocationContext?.eventName || context.eventName;
  const effectivePayload = invocationContext?.eventPayload || context.payload;

  // Check context type
  const prEventNames = new Set(["pull_request", "pull_request_target", "pull_request_review", "pull_request_review_comment"]);
  const isIssueCommentOnPR = effectiveEventName === "issue_comment" && Boolean(effectivePayload?.issue?.pull_request);
  const isIssueContext = effectiveEventName === "issues" || (effectiveEventName === "issue_comment" && !isIssueCommentOnPR);
  const isPRContext = prEventNames.has(effectiveEventName) || isIssueCommentOnPR;

  // Default target is "triggering"
  const target = targetConfig || "triggering";

  // Validate context for triggering mode
  if (target === "triggering") {
    if (supportsPR) {
      // Supports both issues and PRs
      if (!isIssueContext && !isPRContext) {
        return {
          success: false,
          error: `Target is "triggering" but not running in issue or pull request context, skipping ${itemType}`,
          shouldFail: false, // Just skip, don't fail the workflow
        };
      }
    } else if (supportsIssue) {
      // Supports issues only
      if (!isIssueContext) {
        return {
          success: false,
          error: `Target is "triggering" but not running in issue context, skipping ${itemType}`,
          shouldFail: false, // Just skip, don't fail the workflow
        };
      }
    } else {
      // Supports PRs only
      if (!isPRContext) {
        return {
          success: false,
          error: `Target is "triggering" but not running in pull request context, skipping ${itemType}`,
          shouldFail: false, // Just skip, don't fail the workflow
        };
      }
    }
  }

  // Resolve target number
  let itemNumber;
  let contextType;
  let authorizedTargetNumber;

  if (target === "*") {
    // Use item_number, issue_number, or pull_request_number (aliases: pr_number, pr, pull_number) from item
    let numberField;
    const pullRequestNumberField = item.pull_request_number || item.pr_number || item.pr || item.pull_number;
    if (supportsPR) {
      // Supports both issues and PRs: check all fields
      numberField = item.item_number || item.issue_number || pullRequestNumberField;
    } else if (supportsIssue) {
      // Supports issues only: check issue-related fields
      numberField = item.item_number || item.issue_number;
    } else {
      // Supports PRs only: check PR field
      numberField = pullRequestNumberField;
    }

    let fieldNames;
    if (supportsPR) {
      fieldNames = "item_number/issue_number/pull_request_number/pr_number/pr/pull_number";
    } else if (supportsIssue) {
      fieldNames = "item_number/issue_number";
    } else {
      fieldNames = "pull_request_number/pr_number/pr/pull_number";
    }

    if (numberField) {
      itemNumber = typeof numberField === "number" ? numberField : parseInt(String(numberField), 10);
      if (Number.isNaN(itemNumber) || itemNumber <= 0) {
        return {
          success: false,
          error: `Invalid ${fieldNames} specified: ${numberField}`,
          shouldFail: true,
        };
      }
      if (supportsPR || supportsIssue) {
        contextType = item.item_number || item.issue_number ? "issue" : "pull request";
      } else {
        contextType = "pull request";
      }
    } else {
      return {
        success: false,
        error: `Target is "*" but no ${fieldNames} specified in ${itemType} item`,
        shouldFail: true,
      };
    }
  } else if (target !== "triggering") {
    // Explicit number
    itemNumber = parseInt(target, 10);
    if (Number.isNaN(itemNumber) || itemNumber <= 0) {
      // Determine the correct item type name based on what the handler supports
      // Convention: supportsPR=true means both issues and PRs (unless supportsIssue explicitly says otherwise)
      //             supportsIssue=true means issues only
      //             supportsPR=false with supportsIssue=false/undefined means PRs only
      let itemTypeName;
      let helpText = "";

      if (supportsIssue === true) {
        // Issues only
        itemTypeName = "issue";
        helpText = `Make sure you're using a proper expression like "\${{ github.event.issue.number }}" and that the workflow is running in an issue context.`;
      } else if (supportsPR === true) {
        // Both issues and PRs (supportsPR=true is used by handlers that support both)
        itemTypeName = "issue or pull request";
        helpText = `Make sure you're using a proper expression like "\${{ github.event.issue.number }}" for issues or "\${{ github.event.pull_request.number }}" for pull requests, and that the workflow is running in the correct context.`;
      } else {
        // PRs only (supportsPR=false, supportsIssue=false/undefined)
        itemTypeName = "pull request";
        helpText = `Make sure you're using a proper expression like "\${{ github.event.pull_request.number }}" and that the workflow is running in a pull request context.`;
      }

      // Provide helpful error message if target looks like a failed expression evaluation
      let errorMessage = `Invalid ${itemTypeName} number in target configuration: ${target}`;
      if (target === "event" || target === "[object Object]" || target.includes("github.event")) {
        errorMessage += `. It looks like the target contains a GitHub Actions expression that didn't evaluate correctly. ${helpText}`;
      }

      return {
        success: false,
        error: errorMessage,
        shouldFail: true,
      };
    }
    authorizedTargetNumber = itemNumber;
    contextType = supportsPR || supportsIssue ? "issue" : "pull request";
  } else {
    // Use triggering context
    if (isIssueContext) {
      if (effectivePayload.issue) {
        itemNumber = effectivePayload.issue.number;
        authorizedTargetNumber = itemNumber;
        contextType = "issue";
      } else {
        return {
          success: false,
          error: "Issue context detected but no issue found in payload",
          shouldFail: true,
        };
      }
    } else if (isPRContext) {
      if (effectivePayload.pull_request) {
        itemNumber = effectivePayload.pull_request.number;
        authorizedTargetNumber = itemNumber;
        contextType = "pull request";
      } else if (isIssueCommentOnPR) {
        itemNumber = effectivePayload.issue.number;
        authorizedTargetNumber = itemNumber;
        contextType = "pull request";
      } else {
        return {
          success: false,
          error: "Pull request context detected but no pull request found in payload",
          shouldFail: true,
        };
      }
    }
  }

  if (!itemNumber) {
    const itemTypeName = supportsPR ? "issue or pull request" : supportsIssue ? "issue" : "pull request";
    return {
      success: false,
      error: `Could not determine ${itemTypeName} number`,
      shouldFail: true,
    };
  }

  const authorizationInvariant = assertTargetAuthorizationInvariant({
    targetConfig: target,
    resolvedNumber: itemNumber,
    authorizedNumber: authorizedTargetNumber,
    itemType,
  });
  if (!authorizationInvariant.success) {
    return authorizationInvariant;
  }

  return {
    success: true,
    number: itemNumber,
    contextType: contextType || (supportsPR || supportsIssue ? "issue" : "pull request"),
  };
}

/**
 * Fail closed when configured target authorization and resolved target diverge.
 * For non-wildcard targets, the authorized target must come from trusted
 * configuration or triggering context, never from agent-supplied item fields.
 *
 * @param {Object} params
 * @param {string|undefined} params.targetConfig
 * @param {number|undefined} params.resolvedNumber
 * @param {number|undefined} params.authorizedNumber
 * @param {string} params.itemType
 * @returns {{success: true} | {success: false, error: string, shouldFail: boolean}}
 */
function assertTargetAuthorizationInvariant(params) {
  const { targetConfig, resolvedNumber, authorizedNumber, itemType } = params;
  const target = targetConfig || "triggering";
  if (target === "*") {
    return { success: true };
  }

  if (typeof authorizedNumber !== "number" || !Number.isInteger(authorizedNumber) || authorizedNumber <= 0) {
    return {
      success: false,
      error: `ERR_TARGET_AUTHORIZATION: could not determine authorized target for ${itemType}`,
      shouldFail: true,
    };
  }

  if (resolvedNumber !== authorizedNumber) {
    return {
      success: false,
      error: `ERR_TARGET_AUTHORIZATION: resolved target #${resolvedNumber} does not match authorized target #${authorizedNumber} for ${itemType}`,
      shouldFail: true,
    };
  }

  return { success: true };
}

/**
 * Load custom safe output job types from environment variable
 * These are job names defined in safe-outputs.jobs that are processed by custom jobs
 * @returns {Set<string>} Set of custom safe output job type names
 */
function loadCustomSafeOutputJobTypes() {
  const safeOutputJobsEnv = process.env.GH_AW_SAFE_OUTPUT_JOBS;
  if (!safeOutputJobsEnv) {
    return new Set();
  }

  try {
    const safeOutputJobs = JSON.parse(safeOutputJobsEnv);
    // The environment variable is a map of job names to output keys
    // We need the job names (keys) as the message types to ignore
    const jobTypes = Object.keys(safeOutputJobs);
    if (typeof core !== "undefined") {
      core.debug(`Loaded ${jobTypes.length} custom safe output job type(s): ${jobTypes.join(", ")}`);
    }
    return new Set(jobTypes);
  } catch (error) {
    if (typeof core !== "undefined") {
      core.warning(`Failed to parse GH_AW_SAFE_OUTPUT_JOBS: ${getErrorMessage(error)}`);
    }
    return new Set();
  }
}

/**
 * Determine issue number from message or context
 * @param {Object} message - Message object that may contain issue_number
 * @returns {{success: true, issueNumber: number} | {success: false, error: string}}
 */
function resolveIssueNumber(message) {
  // Determine issue number
  let issueNumber;
  if (message.issue_number !== undefined) {
    issueNumber = parseInt(String(message.issue_number), 10);
    if (Number.isNaN(issueNumber)) {
      return {
        success: false,
        error: `Invalid issue_number: ${message.issue_number}`,
      };
    }
  } else {
    // Use context issue if available (guard against context being undefined in
    // non-github-script environments such as schedule events or the MCP server)
    const ctx = typeof context !== "undefined" ? context : null;
    const contextIssue = ctx?.payload?.issue?.number;
    if (!contextIssue) {
      return {
        success: false,
        error: "No issue number available",
      };
    }
    issueNumber = contextIssue;
  }

  return {
    success: true,
    issueNumber: issueNumber,
  };
}

/**
 * Extract assignees from message supporting both singular and plural forms
 * @param {Object} message - Message object that may contain assignee or assignees
 * @returns {string[]} Array of assignee usernames
 */
function extractAssignees(message) {
  // Support both singular "assignee" and plural "assignees" for flexibility
  let requestedAssignees = [];
  if (message.assignees && Array.isArray(message.assignees)) {
    requestedAssignees = message.assignees;
  } else if (message.assignee) {
    requestedAssignees = [message.assignee];
  }
  return requestedAssignees;
}

/**
 * Check if a username matches a blocked pattern
 * Supports exact matching and glob-style patterns (e.g., "*[bot]")
 * @param {string} username - The username to check
 * @param {string} pattern - The pattern to match against (e.g., "copilot", "*[bot]")
 * @returns {boolean} True if username matches the blocked pattern
 */
function matchesBlockedPattern(username, pattern) {
  return matchesSimpleGlob(username, pattern);
}

/**
 * Check if a username is blocked by any pattern in the blocked list
 * @param {string} username - The username to check
 * @param {string[]|undefined} blockedPatterns - Array of blocked patterns (e.g., ["copilot", "*[bot]"])
 * @returns {boolean} True if username is blocked
 */
function isUsernameBlocked(username, blockedPatterns) {
  if (!blockedPatterns || blockedPatterns.length === 0) {
    return false;
  }

  return blockedPatterns.some(pattern => matchesBlockedPattern(username, pattern));
}

/**
 * Load custom safe output script handlers from environment variable
 * These are inline scripts defined in safe-outputs.scripts that run in the handler loop
 * @returns {Map<string, string>} Map of script type names to their .cjs filenames
 */
function loadCustomSafeOutputScriptHandlers() {
  const safeOutputScriptsEnv = process.env.GH_AW_SAFE_OUTPUT_SCRIPTS;
  if (!safeOutputScriptsEnv) {
    return new Map();
  }

  try {
    const safeOutputScripts = JSON.parse(safeOutputScriptsEnv);
    // The environment variable is a map of normalized script names to .cjs filenames
    const scriptHandlers = new Map(Object.entries(safeOutputScripts));
    if (typeof core !== "undefined") {
      core.debug(`Loaded ${scriptHandlers.size} custom safe output script handler(s): ${[...scriptHandlers.keys()].join(", ")}`);
    }
    return scriptHandlers;
  } catch (error) {
    if (typeof core !== "undefined") {
      core.warning(`Failed to parse GH_AW_SAFE_OUTPUT_SCRIPTS: ${getErrorMessage(error)}`);
    }
    return new Map();
  }
}

/**
 * Load custom safe output action handlers from environment variable.
 * These are GitHub Actions configured in safe-outputs.actions that are processed
 * by compiler-injected `uses:` steps after the handler manager exports their payloads.
 * The handler manager processes the tool call, applies temporary ID substitutions,
 * and exports `action_<name>_payload` outputs that the injected steps consume.
 * @returns {Map<string, string>} Map of normalized action type name to action name (for handler config)
 */
function loadCustomSafeOutputActionHandlers() {
  const safeOutputActionsEnv = process.env.GH_AW_SAFE_OUTPUT_ACTIONS;
  if (!safeOutputActionsEnv) {
    return new Map();
  }

  try {
    const safeOutputActions = JSON.parse(safeOutputActionsEnv);
    // The environment variable is a map of normalized action names to themselves
    const actionHandlers = new Map(Object.entries(safeOutputActions));
    if (typeof core !== "undefined") {
      core.debug(`Loaded ${actionHandlers.size} custom safe output action handler(s): ${[...actionHandlers.keys()].join(", ")}`);
    }
    return actionHandlers;
  } catch (error) {
    if (typeof core !== "undefined") {
      core.warning(`Failed to parse GH_AW_SAFE_OUTPUT_ACTIONS: ${getErrorMessage(error)}`);
    }
    return new Map();
  }
}

/**
 * Parse a templatable boolean value that may arrive as a boolean literal or as
 * the string result of a resolved GitHub Actions expression. Undefined,
 * null, false, and unresolved expression strings return false.
 * @param {any} value - Candidate templatable boolean value.
 * @returns {boolean}
 */
function isTemplatableTrue(value) {
  return value === true || value === "true";
}

/**
 * Returns true when the current execution is in staged mode.
 * Staged mode is active when either the global GH_AW_SAFE_OUTPUTS_STAGED
 * environment variable is "true" or when the per-handler config resolves to true.
 * Use this helper in all handlers to ensure consistent staged mode detection.
 * @param {Object} [config] - Handler configuration object (may have staged: true/"true")
 * @returns {boolean}
 */
function isStagedMode(config) {
  return process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || (config != null && isTemplatableTrue(config.staged));
}

/**
 * Checks required-labels and required-title-prefix precondition filters.
 * Returns a skip result if the item does not pass, or null if it passes.
 * Fetches the issue/PR from GitHub to check its labels and title.
 *
 * @param {Object} githubClient - Authenticated GitHub client (Octokit)
 * @param {{owner: string, repo: string}} repoParts - Repository owner and name
 * @param {number} itemNumber - Issue or PR number to check
 * @param {string[]} requiredLabels - Labels that must ALL be present on the item
 * @param {string} requiredTitlePrefix - Title prefix the item must start with
 * @param {string} handlerType - Handler type name used in log messages
 * @returns {Promise<{success: false, skipped: true, error: string}|null>}
 */
async function checkRequiredFilter(githubClient, repoParts, itemNumber, requiredLabels, requiredTitlePrefix, handlerType) {
  if (!requiredLabels.length && !requiredTitlePrefix) return null;

  const { data: item } = await githubClient.rest.issues.get({
    owner: repoParts.owner,
    repo: repoParts.repo,
    issue_number: itemNumber,
  });

  if (requiredLabels.length > 0) {
    const itemLabels = (item.labels || []).map(/** @param {any} l */ l => (typeof l === "string" ? l : l.name || ""));
    if (!requiredLabels.every(r => itemLabels.includes(r))) {
      core.info(`Skipping ${handlerType} for #${itemNumber}: does not match required-labels filter (${requiredLabels.join(", ")})`);
      return { success: false, skipped: true, error: `Item does not match required-labels filter` };
    }
  }

  if (requiredTitlePrefix && !item.title?.startsWith(requiredTitlePrefix)) {
    core.info(`Skipping ${handlerType} for #${itemNumber}: title does not start with required prefix "${requiredTitlePrefix}"`);
    return { success: false, skipped: true, error: `Item title does not start with required prefix` };
  }

  return null;
}

module.exports = {
  parseAllowedItems,
  parseMaxCount,
  resolveTarget,
  assertTargetAuthorizationInvariant,
  loadCustomSafeOutputJobTypes,
  loadCustomSafeOutputScriptHandlers,
  loadCustomSafeOutputActionHandlers,
  resolveIssueNumber,
  extractAssignees,
  matchesBlockedPattern,
  isUsernameBlocked,
  isTemplatableTrue,
  isStagedMode,
  logStagedPreviewInfo,
  checkRequiredFilter,
};
