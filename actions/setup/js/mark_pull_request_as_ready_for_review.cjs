// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { generateFooterWithMessages, getDetectionCautionAlert } = require("./messages_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode, checkRequiredFilter, resolveTarget } = require("./safe_output_helpers.cjs");
const { ERR_NOT_FOUND } = require("./error_codes.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "mark_pull_request_as_ready_for_review";

/**
 * Get pull request details using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, title: string, html_url: string, draft: boolean, node_id: string}>} Pull request details
 */
async function getPullRequestDetails(github, owner, repo, prNumber) {
  const { data: pr } = await github.rest.pulls.get({
    owner,
    repo,
    pull_number: prNumber,
  });

  if (!pr) {
    throw new Error(`${ERR_NOT_FOUND}: Pull request #${prNumber} not found in ${owner}/${repo}`);
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

/** @type {string} GraphQL mutation to mark a pull request as ready for review */
const MARK_PR_READY_MUTATION = /* GraphQL */ `
  mutation ($pullRequestId: ID!) {
    markPullRequestReadyForReview(input: { pullRequestId: $pullRequestId }) {
      pullRequest {
        number
        isDraft
        url
        title
      }
    }
  }
`;

/**
 * Mark a pull request as ready for review using the GraphQL API.
 * The REST API `pulls.update` silently ignores `draft: false`, so the GraphQL
 * mutation `markPullRequestReadyForReview` must be used instead.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} pullRequestNodeId - The PR's GraphQL node ID (e.g., 'PR_kwDO...')
 * @returns {Promise<{number: number, html_url: string, title: string, isDraft: boolean}>} Pull request details
 */
async function markPullRequestAsReadyForReview(github, pullRequestNodeId) {
  const result = await github.graphql(MARK_PR_READY_MUTATION, { pullRequestId: pullRequestNodeId });

  const pr = result.markPullRequestReadyForReview.pullRequest;
  return {
    number: pr.number,
    html_url: pr.url,
    title: pr.title,
    isDraft: pr.isDraft,
  };
}

/**
 * Main handler factory for mark_pull_request_as_ready_for_review
 * Returns a message handler function that processes individual mark_pull_request_as_ready_for_review messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;
  const targetConfig = config.target || "triggering";
  const githubClient = await createAuthenticatedGitHubClient(config);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

  core.info(`Mark pull request as ready for review configuration: max=${maxCount}`);

  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  if (defaultTargetRepo) core.info(`Target repository: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) core.info(`Allowed repositories: ${Array.from(allowedRepos).join(", ")}`);

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

    // Determine PR number and target repo
    const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "pull request");
    if (!repoResult.success) {
      core.warning(`mark_pull_request_as_ready_for_review: ${repoResult.error}`);
      return { success: false, error: repoResult.error };
    }
    const prOwner = repoResult.repoParts.owner;
    const prRepo = repoResult.repoParts.repo;

    const targetResult = resolveTarget({
      targetConfig,
      item,
      context,
      itemType: HANDLER_TYPE,
    });
    if (!targetResult.success) {
      core.warning(targetResult.error);
      return {
        success: false,
        error: targetResult.error,
      };
    }
    const prNumber = targetResult.number;

    const repoParts = { owner: prOwner, repo: prRepo };
    const filterResult = await checkRequiredFilter(githubClient, repoParts, prNumber, requiredLabels, requiredTitlePrefix, "mark_pull_request_as_ready_for_review");
    if (filterResult) return filterResult;

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
      const currentPR = await getPullRequestDetails(githubClient, prOwner, prRepo, prNumber);

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

      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would mark PR #${prNumber} as ready for review`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            number: prNumber,
            isDraft: currentPR.draft,
            hasReason: !!item.reason,
          },
        };
      }

      // Update the PR to mark as ready for review using GraphQL mutation
      const pr = await markPullRequestAsReadyForReview(githubClient, currentPR.node_id);

      if (pr.isDraft) {
        core.error(`GraphQL mutation reported success but PR #${prNumber} is still a draft`);
        return {
          success: false,
          error: `Failed to mark PR #${prNumber} as ready for review: PR is still in draft state`,
        };
      }

      // Add comment with reason
      const workflowName = process.env.GH_AW_WORKFLOW_NAME || "GitHub Agentic Workflow";
      const runUrl = buildWorkflowRunUrl(context, context.repo);
      const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
      const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";

      // Extract triggering context for footer generation
      const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
      const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
      const triggeringDiscussionNumber = context.payload?.discussion?.number;

      const sanitizedReason = sanitizeContent(item.reason);

      // Inject CAUTION at top of body if threat detection warning was raised
      const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);
      const cautionPrefix = detectionCaution ? detectionCaution + "\n\n" : "";

      const footer = generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, undefined, { skipDetectionCaution: true });
      const commentBody = `${cautionPrefix}${sanitizedReason}\n\n${footer}`;

      await addPullRequestComment(githubClient, prOwner, prRepo, prNumber, commentBody);

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
