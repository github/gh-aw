// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { processItems } = require("./safe_output_processor.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

// GitHub Copilot reviewer bot username
const COPILOT_REVIEWER_BOT = "copilot-pull-request-reviewer[bot]";

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "add_reviewer";

/**
 * Main handler factory for add_reviewer
 * Returns a message handler function that processes individual add_reviewer messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedReviewers = config.allowed || [];
  const maxCount = config.max || 10;

  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  core.info(`Add reviewer configuration: max=${maxCount}`);
  if (allowedReviewers.length > 0) {
    core.info(`Allowed reviewers: ${allowedReviewers.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single add_reviewer message
   * @param {Object} message - The add_reviewer message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleAddReviewer(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping add_reviewer: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const reviewerItem = message;

    // Determine PR number
    let prNumber;
    if (reviewerItem.pull_request_number !== undefined) {
      prNumber = parseInt(String(reviewerItem.pull_request_number), 10);
      if (isNaN(prNumber)) {
        core.warning(`Invalid pull_request_number: ${reviewerItem.pull_request_number}`);
        return {
          success: false,
          error: `Invalid pull_request_number: ${reviewerItem.pull_request_number}`,
        };
      }
    } else {
      // Use context PR if available
      const contextPR = context.payload?.pull_request?.number;
      if (!contextPR) {
        core.warning("No pull_request_number provided and not in PR context");
        return {
          success: false,
          error: "No PR number available",
        };
      }
      prNumber = contextPR;
    }

    const requestedReviewers = reviewerItem.reviewers || [];
    core.info(`Requested reviewers: ${JSON.stringify(requestedReviewers)}`);

    // Use shared helper to filter, sanitize, dedupe, and limit
    const uniqueReviewers = processItems(requestedReviewers, allowedReviewers, maxCount);

    if (uniqueReviewers.length === 0) {
      core.info("No reviewers to add");
      return {
        success: true,
        prNumber: prNumber,
        reviewersAdded: [],
        message: "No valid reviewers found",
      };
    }

    core.info(`Adding ${uniqueReviewers.length} reviewers to PR #${prNumber}: ${JSON.stringify(uniqueReviewers)}`);

    // If in staged mode, preview without executing
    if (isStaged) {
      core.info(`Staged mode: Would add reviewers to PR #${prNumber}`);
      return {
        success: true,
        staged: true,
        previewInfo: {
          number: prNumber,
          reviewers: uniqueReviewers,
        },
      };
    }

    try {
      // Special handling for "copilot" reviewer - separate it from other reviewers in a single pass
      const hasCopilot = uniqueReviewers.includes("copilot");
      const otherReviewers = hasCopilot ? uniqueReviewers.filter(r => r !== "copilot") : uniqueReviewers;

      // Add non-copilot reviewers first
      if (otherReviewers.length > 0) {
        await github.rest.pulls.requestReviewers({
          owner: context.repo.owner,
          repo: context.repo.repo,
          pull_number: prNumber,
          reviewers: otherReviewers,
        });
        core.info(`Successfully added ${otherReviewers.length} reviewer(s) to PR #${prNumber}`);
      }

      // Add copilot reviewer separately if requested
      if (hasCopilot) {
        try {
          await github.rest.pulls.requestReviewers({
            owner: context.repo.owner,
            repo: context.repo.repo,
            pull_number: prNumber,
            reviewers: [COPILOT_REVIEWER_BOT],
          });
          core.info(`Successfully added copilot as reviewer to PR #${prNumber}`);
        } catch (copilotError) {
          const copilotErrorMsg = copilotError instanceof Error ? copilotError.message : String(copilotError);
          core.warning(`Failed to add copilot as reviewer: ${copilotErrorMsg}`);
          // Don't fail the whole step if copilot reviewer fails
        }
      }

      return {
        success: true,
        prNumber: prNumber,
        reviewersAdded: uniqueReviewers,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to add reviewers: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
