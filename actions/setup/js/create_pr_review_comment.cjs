// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_pull_request_review_comment";

/**
 * Main handler factory for create_pull_request_review_comment
 * Returns a message handler function that validates and buffers individual review comments.
 * Comments are buffered in the PR review buffer (passed via config._prReviewBuffer) and
 * submitted as a single PR review after all messages have been processed.
 *
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const defaultSide = config.side || "RIGHT";
  const commentTarget = config.target || "triggering";
  const maxCount = config.max || 10;
  const buffer = config._prReviewBuffer;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  if (!buffer) {
    core.warning("create_pull_request_review_comment: No PR review buffer provided in config");
    return async function handleCreatePRReviewComment() {
      return { success: false, error: "No PR review buffer available" };
    };
  }

  core.info(`PR review comment target configuration: ${commentTarget}`);
  core.info(`Default comment side configuration: ${defaultSide}`);
  core.info(`Max count: ${maxCount}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Extract triggering context for footer generation
  const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
  const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
  const triggeringDiscussionNumber = context.payload?.discussion?.number;

  // Set footer context once for the review buffer
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runId = context.runId;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;

  buffer.setFooterContext({
    workflowName,
    runUrl,
    workflowSource,
    workflowSourceURL,
    triggeringIssueNumber,
    triggeringPRNumber,
    triggeringDiscussionNumber,
  });

  /**
   * Message handler function that validates and buffers a single create_pull_request_review_comment message
   * @param {Object} message - The create_pull_request_review_comment message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status and comment details
   */
  return async function handleCreatePRReviewComment(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create_pull_request_review_comment: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const commentItem = message;

    core.info(`Processing create_pull_request_review_comment: path=${commentItem.path}, line=${commentItem.line}, bodyLength=${commentItem.body?.length || 0}`);

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(commentItem, defaultTargetRepo, allowedRepos, "PR review comment");
    if (!repoResult.success) {
      core.warning(`Skipping PR review comment: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: itemRepo, repoParts } = repoResult;
    core.info(`Target repository: ${itemRepo}`);

    // Check if we're in a pull request context, or an issue comment context on a PR
    const isPRContext =
      context.eventName === "pull_request" ||
      context.eventName === "pull_request_review" ||
      context.eventName === "pull_request_review_comment" ||
      (context.eventName === "issue_comment" && context.payload.issue && context.payload.issue.pull_request);

    // Validate context based on target configuration
    if (commentTarget === "triggering" && !isPRContext) {
      core.info('Target is "triggering" but not running in pull request context, skipping review comment creation');
      return {
        success: false,
        error: "Not in pull request context",
      };
    }

    // Validate required fields
    if (!commentItem.path) {
      core.warning('Missing required field "path" in review comment item');
      return {
        success: false,
        error: 'Missing required field "path"',
      };
    }

    if (!commentItem.line || (typeof commentItem.line !== "number" && typeof commentItem.line !== "string")) {
      core.warning('Missing or invalid required field "line" in review comment item');
      return {
        success: false,
        error: 'Missing or invalid required field "line"',
      };
    }

    if (!commentItem.body || typeof commentItem.body !== "string") {
      core.warning('Missing or invalid required field "body" in review comment item');
      return {
        success: false,
        error: 'Missing or invalid required field "body"',
      };
    }

    // Determine the PR number for this review comment
    let pullRequestNumber;
    let pullRequest;

    if (commentTarget === "*") {
      // For target "*", we need an explicit PR number from the comment item
      if (commentItem.pull_request_number) {
        pullRequestNumber = parseInt(commentItem.pull_request_number, 10);
        if (isNaN(pullRequestNumber) || pullRequestNumber <= 0) {
          core.warning(`Invalid pull request number specified: ${commentItem.pull_request_number}`);
          return {
            success: false,
            error: `Invalid pull request number: ${commentItem.pull_request_number}`,
          };
        }
      } else {
        core.warning('Target is "*" but no pull_request_number specified in comment item');
        return {
          success: false,
          error: 'Target is "*" but no pull_request_number specified',
        };
      }
    } else if (commentTarget && commentTarget !== "triggering") {
      // Explicit PR number specified in target
      pullRequestNumber = parseInt(commentTarget, 10);
      if (isNaN(pullRequestNumber) || pullRequestNumber <= 0) {
        core.warning(`Invalid pull request number in target configuration: ${commentTarget}`);
        return {
          success: false,
          error: `Invalid pull request number in target: ${commentTarget}`,
        };
      }
    } else {
      // Default behavior: use triggering PR
      if (context.payload.pull_request) {
        pullRequestNumber = context.payload.pull_request.number;
        pullRequest = context.payload.pull_request;
      } else if (context.payload.issue && context.payload.issue.pull_request) {
        pullRequestNumber = context.payload.issue.number;
      } else {
        core.warning("Pull request context detected but no pull request found in payload");
        return {
          success: false,
          error: "No pull request found in payload",
        };
      }
    }

    if (!pullRequestNumber) {
      core.warning("Could not determine pull request number");
      return {
        success: false,
        error: "Could not determine pull request number",
      };
    }

    // If we don't have the full PR details yet, fetch them
    if (!pullRequest || !pullRequest.head || !pullRequest.head.sha) {
      try {
        const { data: fullPR } = await github.rest.pulls.get({
          owner: repoParts.owner,
          repo: repoParts.repo,
          pull_number: pullRequestNumber,
        });
        pullRequest = fullPR;
        core.info(`Fetched full pull request details for PR #${pullRequestNumber} in ${itemRepo}`);
      } catch (error) {
        core.warning(`Failed to fetch pull request details for PR #${pullRequestNumber}: ${getErrorMessage(error)}`);
        return {
          success: false,
          error: `Failed to fetch pull request details: ${getErrorMessage(error)}`,
        };
      }
    }

    // Check if we have the commit SHA needed for creating review comments
    if (!pullRequest || !pullRequest.head || !pullRequest.head.sha) {
      core.warning(`Pull request head commit SHA not found for PR #${pullRequestNumber} - cannot create review comment`);
      return {
        success: false,
        error: "Pull request head commit SHA not found",
      };
    }

    // Parse line numbers
    const line = parseInt(commentItem.line, 10);
    if (isNaN(line) || line <= 0) {
      core.warning(`Invalid line number: ${commentItem.line}`);
      return {
        success: false,
        error: `Invalid line number: ${commentItem.line}`,
      };
    }

    let startLine = undefined;
    if (commentItem.start_line) {
      startLine = parseInt(commentItem.start_line, 10);
      if (isNaN(startLine) || startLine <= 0 || startLine > line) {
        core.warning(`Invalid start_line number: ${commentItem.start_line} (must be <= line: ${line})`);
        return {
          success: false,
          error: `Invalid start_line: ${commentItem.start_line}`,
        };
      }
    }

    // Determine side (LEFT or RIGHT)
    const side = commentItem.side || defaultSide;
    if (side !== "LEFT" && side !== "RIGHT") {
      core.warning(`Invalid side value: ${side} (must be LEFT or RIGHT)`);
      return {
        success: false,
        error: `Invalid side value: ${side}`,
      };
    }

    // Set the review context (first comment sets it)
    // Reject comments targeting a different repo/PR than the first comment
    const existingCtx = buffer.getReviewContext();
    if (existingCtx && (existingCtx.repo !== itemRepo || existingCtx.pullRequestNumber !== pullRequestNumber)) {
      core.warning(`Skipping review comment: targets ${itemRepo}#${pullRequestNumber} but buffer is bound to ${existingCtx.repo}#${existingCtx.pullRequestNumber}. ` + "All review comments in a single review must target the same PR.");
      return {
        success: false,
        error: `Review comments must target the same PR (buffer is bound to ${existingCtx.repo}#${existingCtx.pullRequestNumber})`,
      };
    }

    buffer.setReviewContext({
      repo: itemRepo,
      repoParts: repoParts,
      pullRequestNumber: pullRequestNumber,
      pullRequest: pullRequest,
    });

    // Buffer the comment instead of posting it individually
    /** @type {import('./pr_review_buffer.cjs').BufferedComment} */
    const bufferedComment = {
      path: commentItem.path,
      line: line,
      body: commentItem.body.trim(),
      side: side,
    };

    if (startLine !== undefined) {
      bufferedComment.start_line = startLine;
    }

    buffer.addComment(bufferedComment);

    core.info(`Buffered review comment on PR #${pullRequestNumber} in ${itemRepo} at ${commentItem.path}:${line}${startLine ? ` (lines ${startLine}-${line})` : ""} [${side}]`);

    return {
      success: true,
      buffered: true,
      pull_request_number: pullRequestNumber,
      repo: itemRepo,
    };
  };
}

module.exports = { main };
