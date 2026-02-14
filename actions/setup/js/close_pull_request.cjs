// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { generateFooterWithMessages } = require("./messages_footer.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const HANDLER_TYPE = "close_pull_request";

/**
 * Get pull request details using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, title: string, labels: Array<{name: string}>, html_url: string, state: string}>} Pull request details
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
 * @param {string} message - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addPullRequestComment(github, owner, repo, prNumber, message) {
  const { data: comment } = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: prNumber,
    body: message,
  });

  return comment;
}

/**
 * Close a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, html_url: string, title: string}>} Pull request details
 */
async function closePullRequest(github, owner, repo, prNumber) {
  const { data: pr } = await github.rest.pulls.update({
    owner,
    repo,
    pull_number: prNumber,
    state: "closed",
  });

  return pr;
}

/**
 * Handler factory for close-pull-request safe outputs
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const requiredLabels = config.required_labels || [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const maxCount = config.max || 10;
  const comment = config.comment || "";

  core.info(`Close pull request configuration: max=${maxCount}`);
  if (requiredLabels.length > 0) {
    core.info(`Required labels: ${requiredLabels.join(", ")}`);
  }
  if (requiredTitlePrefix) {
    core.info(`Required title prefix: ${requiredTitlePrefix}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single close_pull_request message
   * @param {Object} message - The close_pull_request message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleClosePullRequest(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping close_pull_request: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    // Log message structure for debugging (avoid logging body content)
    core.info(
      `Processing close_pull_request message: ${JSON.stringify({
        has_body: !!item.body,
        body_length: item.body ? item.body.length : 0,
        pull_request_number: item.pull_request_number,
      })}`
    );

    // Determine comment body - prefer non-empty item.body over non-empty config.comment
    /** @type {string} */
    let commentToPost;
    /** @type {string} */
    let commentSource = "unknown";

    if (typeof item.body === "string" && item.body.trim() !== "") {
      commentToPost = item.body;
      commentSource = "item.body";
    } else if (typeof comment === "string" && comment.trim() !== "") {
      commentToPost = comment;
      commentSource = "config.comment";
    } else {
      core.warning("No comment body provided in message and no default comment configured");
      return {
        success: false,
        error: "No comment body provided",
      };
    }

    core.info(`Comment body determined: length=${commentToPost.length}, source=${commentSource}`);

    // Determine PR number
    let prNumber;
    if (item.pull_request_number !== undefined) {
      prNumber = parseInt(String(item.pull_request_number), 10);
      if (isNaN(prNumber)) {
        core.warning(`Invalid pull request number: ${item.pull_request_number}`);
        return {
          success: false,
          error: `Invalid pull request number: ${item.pull_request_number}`,
        };
      }
    } else {
      // Use context PR if available
      const contextPR = context.payload?.pull_request?.number;
      if (!contextPR) {
        core.warning("No pull_request_number provided and not in pull request context");
        return {
          success: false,
          error: "No pull_request_number provided and not in pull request context",
        };
      }
      prNumber = contextPR;
    }

    core.info(`Processing close_pull_request for PR #${prNumber}`);

    // Get PR details
    const { owner, repo } = context.repo;
    let pr;
    try {
      core.info(`Fetching PR details for #${prNumber} in ${owner}/${repo}`);
      pr = await getPullRequestDetails(github, owner, repo, prNumber);
      core.info(`PR #${prNumber} fetched: state=${pr.state}, title="${pr.title}", labels=[${pr.labels.map(l => l.name || l).join(", ")}]`);
    } catch (error) {
      const errorMsg = getErrorMessage(error);
      core.warning(`Failed to get PR #${prNumber} details: ${errorMsg}`);
      return {
        success: false,
        error: `Failed to get PR #${prNumber} details: ${errorMsg}`,
      };
    }

    // Check if already closed - but still add comment
    const wasAlreadyClosed = pr.state === "closed";
    if (wasAlreadyClosed) {
      core.info(`PR #${prNumber} is already closed, but will still add comment`);
    }

    // Check label filter
    if (!checkLabelFilter(pr.labels, requiredLabels)) {
      core.info(`Skipping PR #${prNumber}: does not match label filter (required: ${requiredLabels.join(", ")})`);
      return {
        success: false,
        error: `PR does not match required labels`,
      };
    }
    if (requiredLabels.length > 0) {
      core.info(`PR #${prNumber} has all required labels: ${requiredLabels.join(", ")}`);
    }

    // Check title prefix filter
    if (!checkTitlePrefixFilter(pr.title, requiredTitlePrefix)) {
      core.info(`Skipping PR #${prNumber}: title does not start with '${requiredTitlePrefix}'`);
      return {
        success: false,
        error: `PR title does not start with required prefix`,
      };
    }
    if (requiredTitlePrefix) {
      core.info(`PR #${prNumber} has required title prefix: "${requiredTitlePrefix}"`);
    }

    // Add comment with the body from the message
    let commentPosted = false;
    try {
      const triggeringPRNumber = context.payload?.pull_request?.number;
      const triggeringIssueNumber = context.payload?.issue?.number;
      const commentBody = buildCommentBody(commentToPost, triggeringIssueNumber, triggeringPRNumber);
      core.info(`Adding comment to PR #${prNumber}: length=${commentBody.length}`);
      await addPullRequestComment(github, owner, repo, prNumber, commentBody);
      commentPosted = true;
      core.info(`✓ Comment posted to PR #${prNumber}`);
      core.info(`Comment details: body_length=${commentBody.length}`);
    } catch (error) {
      const errorMsg = getErrorMessage(error);
      core.error(`Failed to add comment to PR #${prNumber}: ${errorMsg}`);
      core.error(
        `Error details: ${JSON.stringify({
          prNumber,
          hasBody: !!item.body,
          bodyLength: item.body ? item.body.length : 0,
          errorMessage: errorMsg,
        })}`
      );
      // Continue with closing even if comment fails - but log the error
    }

    // Close the PR if not already closed
    let closedPR;
    if (wasAlreadyClosed) {
      core.info(`PR #${prNumber} was already closed, comment ${commentPosted ? "posted successfully" : "posting attempted"}`);
      closedPR = pr;
    } else {
      try {
        core.info(`Closing PR #${prNumber}`);
        closedPR = await closePullRequest(github, owner, repo, prNumber);
        core.info(`✓ PR #${prNumber} closed successfully: ${closedPR.title}`);
      } catch (error) {
        const errorMsg = getErrorMessage(error);
        core.error(`Failed to close PR #${prNumber}: ${errorMsg}`);
        core.error(
          `Error details: ${JSON.stringify({
            prNumber,
            hasBody: !!item.body,
            bodyLength: item.body ? item.body.length : 0,
            errorMessage: errorMsg,
          })}`
        );
        return {
          success: false,
          error: `Failed to close PR #${prNumber}: ${errorMsg}`,
        };
      }
    }

    core.info(`close_pull_request completed for PR #${prNumber}: ${commentPosted ? "comment posted and " : ""}PR ${wasAlreadyClosed ? "was already closed" : "closed successfully"}`);

    return {
      success: true,
      pull_request_number: closedPR.number,
      pull_request_url: closedPR.html_url,
      alreadyClosed: wasAlreadyClosed,
      commentPosted,
    };
  };
}

/**
 * Check if labels match the required labels filter
 * @param {Array<{name: string}>} prLabels - Labels on the PR
 * @param {string[]} requiredLabels - Required labels (any match)
 * @returns {boolean} True if PR has at least one required label or no filter is set
 */
function checkLabelFilter(prLabels, requiredLabels) {
  if (requiredLabels.length === 0) return true;

  const labelNames = prLabels.map(l => l.name);
  return requiredLabels.some(required => labelNames.includes(required));
}

/**
 * Check if title matches the required prefix filter
 * @param {string} title - PR title
 * @param {string} requiredTitlePrefix - Required title prefix
 * @returns {boolean} True if title starts with required prefix or no filter is set
 */
function checkTitlePrefixFilter(title, requiredTitlePrefix) {
  if (!requiredTitlePrefix) return true;
  return title.startsWith(requiredTitlePrefix);
}

/**
 * Build comment body with tracker ID and footer
 * @param {string} body - The original comment body
 * @param {number|undefined} triggeringIssueNumber - Issue number that triggered this workflow
 * @param {number|undefined} triggeringPRNumber - PR number that triggered this workflow
 * @returns {string} The complete comment body with tracker ID and footer
 */
function buildCommentBody(body, triggeringIssueNumber, triggeringPRNumber) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runId = context.runId;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;

  return body.trim() + getTrackerID("markdown") + generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, undefined);
}

module.exports = { main };
