// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Shared helper functions for safe-output scripts
 * Provides common validation and target resolution logic
 */

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
  if (isNaN(parsed) || parsed < 1) {
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
 * @param {boolean} params.supportsPR - When true, handler supports BOTH issues and PRs (e.g., add_labels)
 *                                       When false, handler supports PRs ONLY (e.g., add_reviewers)
 * @param {boolean} params.supportsIssue - When true, handler supports issues ONLY (e.g., update_issue)
 *                                          Mutually exclusive with supportsPR=false
 * @returns {{success: true, number: number, contextType: string} | {success: false, error: string, shouldFail: boolean}} Resolution result
 */
function resolveTarget(params) {
  const { targetConfig, item, context, itemType, supportsPR = false, supportsIssue = false } = params;

  // Check context type
  const isIssueContext = context.eventName === "issues" || context.eventName === "issue_comment";
  const isPRContext = context.eventName === "pull_request" || context.eventName === "pull_request_review" || context.eventName === "pull_request_review_comment";

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

  if (target === "*") {
    // Use item_number, issue_number, or pull_request_number from item
    let numberField;
    if (supportsPR) {
      // Supports both issues and PRs: check all fields
      numberField = item.item_number || item.issue_number || item.pull_request_number;
    } else if (supportsIssue) {
      // Supports issues only: check issue-related fields
      numberField = item.item_number || item.issue_number;
    } else {
      // Supports PRs only: check PR field
      numberField = item.pull_request_number;
    }

    if (numberField) {
      itemNumber = typeof numberField === "number" ? numberField : parseInt(String(numberField), 10);
      if (isNaN(itemNumber) || itemNumber <= 0) {
        const fieldNames = supportsPR ? "item_number/issue_number/pull_request_number" : supportsIssue ? "item_number/issue_number" : "pull_request_number";
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
      const fieldNames = supportsPR ? "item_number/issue_number" : supportsIssue ? "item_number/issue_number" : "pull_request_number";
      return {
        success: false,
        error: `Target is "*" but no ${fieldNames} specified in ${itemType} item`,
        shouldFail: true,
      };
    }
  } else if (target !== "triggering") {
    // Explicit number
    itemNumber = parseInt(target, 10);
    if (isNaN(itemNumber) || itemNumber <= 0) {
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
    contextType = supportsPR || supportsIssue ? "issue" : "pull request";
  } else {
    // Use triggering context
    if (isIssueContext) {
      if (context.payload.issue) {
        itemNumber = context.payload.issue.number;
        contextType = "issue";
      } else {
        return {
          success: false,
          error: "Issue context detected but no issue found in payload",
          shouldFail: true,
        };
      }
    } else if (isPRContext) {
      if (context.payload.pull_request) {
        itemNumber = context.payload.pull_request.number;
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

  return {
    success: true,
    number: itemNumber,
    contextType: contextType || (supportsPR || supportsIssue ? "issue" : "pull request"),
  };
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
      const { getErrorMessage } = require("./error_helpers.cjs");
      core.warning(`Failed to parse GH_AW_SAFE_OUTPUT_JOBS: ${getErrorMessage(error)}`);
    }
    return new Set();
  }
}

module.exports = {
  parseAllowedItems,
  parseMaxCount,
  resolveTarget,
  loadCustomSafeOutputJobTypes,
};
