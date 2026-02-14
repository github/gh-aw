// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");

/**
 * Get issue details using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<{number: number, title: string, labels: Array<{name: string}>, html_url: string, state: string}>} Issue details
 */
async function getIssueDetails(github, owner, repo, issueNumber) {
  const { data: issue } = await github.rest.issues.get({
    owner,
    repo,
    issue_number: issueNumber,
  });

  if (!issue) {
    throw new Error(`Issue #${issueNumber} not found in ${owner}/${repo}`);
  }

  return issue;
}

/**
 * Add comment to a GitHub Issue using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {string} message - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addIssueComment(github, owner, repo, issueNumber, message) {
  const { data: comment } = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
    body: message,
  });

  return comment;
}

/**
 * Close a GitHub Issue using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<{number: number, html_url: string, title: string}>} Issue details
 */
async function closeIssue(github, owner, repo, issueNumber) {
  const { data: issue } = await github.rest.issues.update({
    owner,
    repo,
    issue_number: issueNumber,
    state: "closed",
  });

  return issue;
}

/**
 * Main handler factory for close_issue
 * Returns a message handler function that processes individual close_issue messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const requiredLabels = config.required_labels || [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const maxCount = config.max || 10;
  const comment = config.comment || "";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  core.info(`Close issue configuration: max=${maxCount}`);
  if (requiredLabels.length > 0) {
    core.info(`Required labels: ${requiredLabels.join(", ")}`);
  }
  if (requiredTitlePrefix) {
    core.info(`Required title prefix: ${requiredTitlePrefix}`);
  }
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single close_issue message
   * @param {Object} message - The close_issue message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCloseIssue(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping close_issue: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    // Log message structure for debugging (avoid logging body content)
    core.info(
      `Processing close_issue message: ${JSON.stringify({
        has_body: !!item.body,
        body_length: item.body ? item.body.length : 0,
        issue_number: item.issue_number,
        has_repo: !!item.repo,
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

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "issue");
    if (!repoResult.success) {
      core.warning(`Skipping close_issue: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: itemRepo, repoParts } = repoResult;
    core.info(`Target repository: ${itemRepo}`);

    // Determine issue number
    let issueNumber;
    if (item.issue_number !== undefined) {
      issueNumber = parseInt(String(item.issue_number), 10);
      if (isNaN(issueNumber)) {
        core.warning(`Invalid issue number: ${item.issue_number}`);
        return {
          success: false,
          error: `Invalid issue number: ${item.issue_number}`,
        };
      }
    } else {
      // Use context issue if available
      const contextIssue = context.payload?.issue?.number;
      if (!contextIssue) {
        core.warning("No issue_number provided and not in issue context");
        return {
          success: false,
          error: "No issue number available",
        };
      }
      issueNumber = contextIssue;
    }

    try {
      // Fetch issue details
      core.info(`Fetching issue details for #${issueNumber} in ${repoParts.owner}/${repoParts.repo}`);
      const issue = await getIssueDetails(github, repoParts.owner, repoParts.repo, issueNumber);
      core.info(`Issue #${issueNumber} fetched: state=${issue.state}, title="${issue.title}", labels=[${issue.labels.map(l => l.name || l).join(", ")}]`);

      // Check if already closed - but still add comment
      const wasAlreadyClosed = issue.state === "closed";
      if (wasAlreadyClosed) {
        core.info(`Issue #${issueNumber} is already closed, but will still add comment`);
      }

      // Validate required labels if configured
      if (requiredLabels.length > 0) {
        const issueLabels = issue.labels.map(l => (typeof l === "string" ? l : l.name || ""));
        const missingLabels = requiredLabels.filter(required => !issueLabels.includes(required));
        if (missingLabels.length > 0) {
          core.warning(`Issue #${issueNumber} missing required labels: ${missingLabels.join(", ")}`);
          return {
            success: false,
            error: `Missing required labels: ${missingLabels.join(", ")}`,
          };
        }
        core.info(`Issue #${issueNumber} has all required labels: ${requiredLabels.join(", ")}`);
      }

      // Validate required title prefix if configured
      if (requiredTitlePrefix && !issue.title.startsWith(requiredTitlePrefix)) {
        core.warning(`Issue #${issueNumber} title doesn't start with "${requiredTitlePrefix}"`);
        return {
          success: false,
          error: `Title doesn't start with "${requiredTitlePrefix}"`,
        };
      }
      if (requiredTitlePrefix) {
        core.info(`Issue #${issueNumber} has required title prefix: "${requiredTitlePrefix}"`);
      }

      // If in staged mode, preview the close without executing it
      if (isStaged) {
        core.info(`Staged mode: Would close issue #${issueNumber} in ${itemRepo}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            number: issueNumber,
            repo: itemRepo,
            alreadyClosed: wasAlreadyClosed,
            hasComment: !!commentToPost,
          },
        };
      }

      // Add comment with the body from the message
      core.info(`Adding comment to issue #${issueNumber}: length=${commentToPost.length}`);
      const commentResult = await addIssueComment(github, repoParts.owner, repoParts.repo, issueNumber, commentToPost);
      core.info(`✓ Comment posted to issue #${issueNumber}: ${commentResult.html_url}`);
      core.info(`Comment details: id=${commentResult.id}, body_length=${commentToPost.length}`);

      // Close the issue if not already closed
      let closedIssue;
      if (wasAlreadyClosed) {
        core.info(`Issue #${issueNumber} was already closed, comment added successfully`);
        closedIssue = issue;
      } else {
        core.info(`Closing issue #${issueNumber} in ${itemRepo}`);
        closedIssue = await closeIssue(github, repoParts.owner, repoParts.repo, issueNumber);
        core.info(`✓ Issue #${issueNumber} closed successfully: ${closedIssue.html_url}`);
      }

      core.info(`close_issue completed successfully for issue #${issueNumber}`);

      return {
        success: true,
        number: issueNumber,
        url: closedIssue.html_url,
        title: closedIssue.title,
        alreadyClosed: wasAlreadyClosed,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to close issue #${issueNumber}: ${errorMessage}`);
      core.error(
        `Error details: ${JSON.stringify({
          issueNumber,
          repo: itemRepo,
          hasBody: !!item.body,
          bodyLength: item.body ? item.body.length : 0,
          errorMessage,
        })}`
      );
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
