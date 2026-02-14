// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Type constant for handler identification
 */
const HANDLER_TYPE = "hide_comment";

/**
 * Hide a comment using the GraphQL API.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} nodeId - Comment node ID (e.g., 'IC_kwDOABCD123456')
 * @param {string} reason - Reason for hiding (default: spam)
 * @returns {Promise<{id: string, isMinimized: boolean}>} Hidden comment details
 */
async function hideCommentAPI(github, nodeId, reason = "spam") {
  const query = /* GraphQL */ `
    mutation ($nodeId: ID!, $classifier: ReportedContentClassifiers!) {
      minimizeComment(input: { subjectId: $nodeId, classifier: $classifier }) {
        minimizedComment {
          isMinimized
        }
      }
    }
  `;

  const result = await github.graphql(query, { nodeId, classifier: reason });

  return {
    id: nodeId,
    isMinimized: result.minimizeComment.minimizedComment.isMinimized,
  };
}

/**
 * Main handler factory for hide_comment
 * Returns a message handler function that processes individual hide_comment messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedReasons = config.allowed_reasons || [];
  const maxCount = config.max || 5;

  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  core.info(`Hide comment configuration: max=${maxCount}`);
  if (allowedReasons.length > 0) {
    core.info(`Allowed reasons: ${allowedReasons.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single hide_comment message
   * @param {Object} message - The hide_comment message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleHideComment(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping hide_comment: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    try {
      const commentId = item.comment_id;
      if (!commentId || typeof commentId !== "string") {
        core.warning("comment_id is required and must be a string (GraphQL node ID)");
        return {
          success: false,
          error: "comment_id is required and must be a string (GraphQL node ID)",
        };
      }

      const reason = item.reason || "SPAM";

      // Normalize reason to uppercase for GitHub API
      const normalizedReason = reason.toUpperCase();

      // Validate reason against allowed reasons if specified (case-insensitive)
      if (allowedReasons.length > 0) {
        const normalizedAllowedReasons = allowedReasons.map(r => r.toUpperCase());
        if (!normalizedAllowedReasons.includes(normalizedReason)) {
          core.warning(`Reason "${reason}" is not in allowed-reasons list [${allowedReasons.join(", ")}]. Skipping comment ${commentId}.`);
          return {
            success: false,
            error: `Reason "${reason}" is not in allowed-reasons list`,
          };
        }
      }

      core.info(`Hiding comment: ${commentId} (reason: ${normalizedReason})`);

      // If in staged mode, preview without executing
      if (isStaged) {
        core.info(`Staged mode: Would hide comment ${commentId}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            commentId,
            reason: normalizedReason,
          },
        };
      }

      const hideResult = await hideCommentAPI(github, commentId, normalizedReason);

      if (hideResult.isMinimized) {
        core.info(`Successfully hidden comment: ${commentId}`);
        return {
          success: true,
          comment_id: commentId,
          is_hidden: true,
        };
      } else {
        core.error(`Failed to hide comment: ${commentId}`);
        return {
          success: false,
          error: `Failed to hide comment: ${commentId}`,
        };
      }
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to hide comment: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main, HANDLER_TYPE };
