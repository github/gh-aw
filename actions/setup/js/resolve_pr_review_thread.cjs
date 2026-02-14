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
 * Look up a review thread's parent PR number via the GraphQL API.
 * Used to validate the thread belongs to the triggering PR before resolving.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} threadId - Review thread node ID (e.g., 'PRRT_kwDOABCD...')
 * @returns {Promise<number|null>} The PR number the thread belongs to, or null if not found
 */
async function getThreadPullRequestNumber(github, threadId) {
  const query = /* GraphQL */ `
    query ($threadId: ID!) {
      node(id: $threadId) {
        ... on PullRequestReviewThread {
          pullRequest {
            number
          }
        }
      }
    }
  `;

  const result = await github.graphql(query, { threadId });

  return result?.node?.pullRequest?.number ?? null;
}

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
 * Extract the triggering pull request number from the GitHub Actions event payload.
 * Supports both pull_request events (payload.pull_request.number) and issue_comment
 * events on PRs (payload.issue.number when payload.issue.pull_request is present).
 * @param {any} payload - The context.payload from the GitHub Actions event
 * @returns {number|undefined} The PR number, or undefined if not in a PR context
 */
function getTriggeringPRNumber(payload) {
  return payload?.pull_request?.number || (payload?.issue?.pull_request ? payload.issue.number : undefined);
}

/**
 * Main handler factory for resolve_pull_request_review_thread
 * Returns a message handler function that processes individual resolve messages.
 *
 * Resolution is scoped to the triggering PR only — the handler validates that each
 * thread belongs to the triggering pull request before resolving it. This prevents
 * agents from resolving threads on unrelated PRs.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;

  // Determine the triggering PR number from context
  const triggeringPRNumber = getTriggeringPRNumber(context.payload);

  core.info(`Resolve PR review thread configuration: max=${maxCount}, triggeringPR=${triggeringPRNumber || "none"}`);

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

      // Validate triggering PR context
      if (!triggeringPRNumber) {
        core.warning("Cannot resolve review thread: not running in a pull request context");
        return {
          success: false,
          error: "Cannot resolve review threads outside of a pull request context",
        };
      }

      // Look up the thread to validate it belongs to the triggering PR
      const threadPRNumber = await getThreadPullRequestNumber(github, threadId);
      if (threadPRNumber === null) {
        core.warning(`Review thread not found or not a PullRequestReviewThread: ${threadId}`);
        return {
          success: false,
          error: `Review thread not found: ${threadId}`,
        };
      }

      if (threadPRNumber !== triggeringPRNumber) {
        core.warning(`Thread ${threadId} belongs to PR #${threadPRNumber}, not triggering PR #${triggeringPRNumber}`);
        return {
          success: false,
          error: `Thread belongs to PR #${threadPRNumber}, but only threads on the triggering PR #${triggeringPRNumber} can be resolved`,
        };
      }

      core.info(`Resolving review thread: ${threadId} (PR #${triggeringPRNumber})`);

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

module.exports = { main, HANDLER_TYPE, getTriggeringPRNumber };
