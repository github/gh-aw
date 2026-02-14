// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Type constant for handler identification
 */
const HANDLER_TYPE = "resolve_pull_request_review_thread";

/**
 * Resolve a pull request review thread using the GraphQL API.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} threadId - Review thread node ID (e.g., 'PRRT_kwDOABCD...')
 * @returns {Promise<{threadId: string, isResolved: boolean}>} Resolved thread details
 */
async function resolveReviewThreadAPI(github, threadId) {
  const query = /* GraphQL */ `
    mutation ($threadId: ID!) {
      resolveReviewThread(input: { threadId: $threadId }) {
        thread {
          id
          isResolved
        }
      }
    }
  `;

  const result = await github.graphql(query, { threadId });

  return {
    threadId: result.resolveReviewThread.thread.id,
    isResolved: result.resolveReviewThread.thread.isResolved,
  };
}

/**
 * Main handler factory for resolve_pull_request_review_thread
 * Returns a message handler function that processes individual resolve messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;

  core.info(`Resolve PR review thread configuration: max=${maxCount}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single resolve_pull_request_review_thread message
   * @param {Object} message - The resolve message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleResolvePRReviewThread(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping resolve_pull_request_review_thread: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    try {
      // Validate required fields
      const threadId = item.thread_id;
      if (!threadId || typeof threadId !== "string" || threadId.trim().length === 0) {
        core.warning('Missing or invalid required field "thread_id" in resolve message');
        return {
          success: false,
          error: 'Missing or invalid required field "thread_id" - must be a non-empty string (GraphQL node ID)',
        };
      }

      core.info(`Resolving review thread: ${threadId}`);

      const resolveResult = await resolveReviewThreadAPI(github, threadId);

      if (resolveResult.isResolved) {
        core.info(`Successfully resolved review thread: ${threadId}`);
        return {
          success: true,
          thread_id: threadId,
          is_resolved: true,
        };
      } else {
        core.error(`Failed to resolve review thread: ${threadId}`);
        return {
          success: false,
          error: `Failed to resolve review thread: ${threadId}`,
        };
      }
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to resolve review thread: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main, HANDLER_TYPE };
