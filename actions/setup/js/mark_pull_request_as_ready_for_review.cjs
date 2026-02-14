// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { generateFooterWithMessages } = require("./messages_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "mark_pull_request_as_ready_for_review";

/**
 * Get pull request details using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, title: string, html_url: string, draft: boolean}>} Pull request details
 */
async function getPullRequestDetails(github, owner, repo, prNumber) {
  const { data: pr } = await github.rest.pulls.get({
    owner,
    repo,
    pull_number: prNumber,
  });

  if (!pr) {
    throw new Error(`Pull request #${prNumber} not found in ${owner}/${repo}`);
  }

  return pr;
}

/**
 * Add comment to a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @param {string} commentBody - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addPullRequestComment(github, owner, repo, prNumber, commentBody) {
  const { data: comment } = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: prNumber,
    body: commentBody,
  });

  return comment;
}

/**
 * Mark a pull request as ready for review using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, html_url: string, title: string}>} Pull request details
 */
async function markPullRequestAsReadyForReview(github, owner, repo, prNumber) {
  const { data: pr } = await github.rest.pulls.update({
    owner,
    repo,
    pull_number: prNumber,
    draft: false,
  });

  return pr;
}

/**
 * Main handler factory for mark_pull_request_as_ready_for_review
 * Returns a message handler function that processes individual mark_pull_request_as_ready_for_review messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;

  core.info(`Mark pull request as ready for review configuration: max=${maxCount}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single mark_pull_request_as_ready_for_review message
   * @param {Object} message - The mark_pull_request_as_ready_for_review message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleMarkPullRequestAsReadyForReview(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping mark_pull_request_as_ready_for_review: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    // Determine PR number
    let prNumber;
    if (item.pull_request_number !== undefined) {
      prNumber = parseInt(String(item.pull_request_number), 10);
      if (isNaN(prNumber)) {
        core.warning(`Invalid pull_request_number: ${item.pull_request_number}`);
        return {
          success: false,
          error: `Invalid pull_request_number: ${item.pull_request_number}`,
        };
      }
    } else {
      // Use context PR if available
      const contextPR = context.payload?.pull_request?.number;
      if (!contextPR) {
        core.warning("No pull_request_number provided and not in pull request context");
        return {
          success: false,
          error: "No pull request number available",
        };
      }
      prNumber = contextPR;
    }

    // Validate reason
    if (!item.reason || typeof item.reason !== "string" || item.reason.trim().length === 0) {
      core.warning(`Item has empty or invalid reason, skipping`);
      return {
        success: false,
        error: "Reason is required and must be a non-empty string",
      };
    }

    try {
      // First, get the current PR to check if it's a draft
      const currentPR = await getPullRequestDetails(github, context.repo.owner, context.repo.repo, prNumber);

      // Check if it's already not a draft
      if (!currentPR.draft) {
        core.info(`Pull request #${prNumber} is already marked as ready for review (not a draft)`);
        return {
          success: true,
          number: prNumber,
          url: currentPR.html_url,
          title: currentPR.title,
          alreadyReady: true,
        };
      }

      // Update the PR to mark as ready for review
      const pr = await markPullRequestAsReadyForReview(github, context.repo.owner, context.repo.repo, prNumber);

      // Add comment with reason
      const workflowName = process.env.GH_AW_WORKFLOW_NAME || "GitHub Agentic Workflow";
      const runUrl = `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`;
      const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
      const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";

      // Extract triggering context for footer generation
      const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
      const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
      const triggeringDiscussionNumber = context.payload?.discussion?.number;

      const sanitizedReason = sanitizeContent(item.reason);
      const footer = generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber);
      const commentBody = `${sanitizedReason}\n\n${footer}`;

      await addPullRequestComment(github, context.repo.owner, context.repo.repo, prNumber, commentBody);

      core.info(`✓ Marked PR #${prNumber} as ready for review and added comment: ${pr.html_url}`);

      return {
        success: true,
        number: prNumber,
        url: pr.html_url,
        title: pr.title,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to mark PR #${prNumber} as ready for review: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
