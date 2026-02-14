// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { generateFooterWithMessages } = require("./messages_footer.cjs");
const { getPRNumber } = require("./update_context_helpers.cjs");

/**
 * Type constant for handler identification
 */
const HANDLER_TYPE = "reply_to_pull_request_review_comment";

/**
 * Main handler factory for reply_to_pull_request_review_comment
 * Returns a message handler function that processes individual reply messages.
 *
 * Replies are scoped to the triggering PR by default — the handler validates that each
 * reply targets the triggering pull request before creating it. This prevents
 * agents from replying to comments on unrelated PRs.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;
  const replyTarget = config.target || "triggering";
  const includeFooter = config.footer !== false; // Default to true
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  // Determine the triggering PR number from context
  const triggeringPRNumber = getPRNumber(context.payload);

  // Extract workflow context for footer generation
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runId = context.runId;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  const runUrl = context.payload?.repository?.html_url ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;

  core.info(`Reply to PR review comment configuration: max=${maxCount}, target=${replyTarget}, footer=${includeFooter}, triggeringPR=${triggeringPRNumber || "none"}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single reply_to_pull_request_review_comment message
   * @param {Object} message - The reply message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleReplyToPRReviewComment(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping reply_to_pull_request_review_comment: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    const item = message;

    try {
      // Validate required fields before incrementing processedCount
      const commentId = item.comment_id;
      if (!commentId || (typeof commentId !== "number" && typeof commentId !== "string")) {
        core.warning('Missing or invalid required field "comment_id" in reply message');
        return {
          success: false,
          error: 'Missing or invalid required field "comment_id" - must be a positive number or numeric string',
        };
      }

      const numericCommentId = typeof commentId === "string" ? parseInt(commentId, 10) : commentId;
      if (isNaN(numericCommentId) || numericCommentId < 1) {
        core.warning(`Invalid comment_id: ${commentId} - must be a positive integer`);
        return {
          success: false,
          error: `Invalid comment_id: ${commentId} - must be a positive integer`,
        };
      }

      const body = item.body;
      if (!body || typeof body !== "string" || body.trim().length === 0) {
        core.warning('Missing or invalid required field "body" in reply message');
        return {
          success: false,
          error: 'Missing or invalid required field "body" - must be a non-empty string',
        };
      }

      // Resolve and validate target repository
      const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "PR review comment reply");
      if (!repoResult.success) {
        core.warning(`Skipping reply to PR review comment: ${repoResult.error}`);
        return {
          success: false,
          error: repoResult.error,
        };
      }

      const { owner, repo } = repoResult.repoParts;

      // Determine the target PR number
      let targetPRNumber;
      if (replyTarget === "triggering") {
        if (!triggeringPRNumber) {
          core.warning("Cannot reply to review comment: not running in a pull request context");
          return {
            success: false,
            error: "Cannot reply to review comments outside of a pull request context",
          };
        }
        targetPRNumber = triggeringPRNumber;
      } else if (replyTarget === "*") {
        // For wildcard target, PR number must be provided in the message
        targetPRNumber = item.pull_request_number;
        if (!targetPRNumber) {
          core.warning("pull_request_number is required when target is '*'");
          return {
            success: false,
            error: "pull_request_number is required when target is '*'",
          };
        }
      } else {
        // Explicit PR number in target
        targetPRNumber = parseInt(replyTarget, 10);
        if (Number.isNaN(targetPRNumber) || !Number.isFinite(targetPRNumber) || targetPRNumber <= 0) {
          core.warning(`Invalid target PR number: '${replyTarget}' - must be a positive integer`);
          return {
            success: false,
            error: `Invalid target PR number: '${replyTarget}' - must be a positive integer`,
          };
        }
      }

      // Validation passed — count this message against the max quota
      processedCount++;

      // In staged mode, skip the API call and return a preview result
      if (isStaged) {
        core.info(`Staged mode: Would reply to review comment ${numericCommentId} on PR #${targetPRNumber} (${owner}/${repo})`);
        return { skipped: true, reason: "staged_mode" };
      }

      // Append footer with workflow information when enabled
      let finalBody = body;
      if (includeFooter) {
        const footer = generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, undefined, triggeringPRNumber, undefined);
        finalBody = body.trimEnd() + footer;
      }

      core.info(`Replying to review comment ${numericCommentId} on PR #${targetPRNumber} (${owner}/${repo})`);

      const result = await github.rest.pulls.createReplyForReviewComment({
        owner,
        repo,
        pull_number: targetPRNumber,
        comment_id: numericCommentId,
        body: finalBody,
      });

      core.info(`Successfully replied to review comment ${numericCommentId}: ${result.data?.html_url}`);

      return {
        success: true,
        comment_id: numericCommentId,
        reply_id: result.data?.id,
        reply_url: result.data?.html_url,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to reply to review comment: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main, HANDLER_TYPE };
