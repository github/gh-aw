// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { isTemplatableTrue, isStagedMode, logStagedPreviewInfo, checkRequiredFilter } = require("./safe_output_helpers.cjs");
const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");
const { parseIntTemplatable } = require("./templatable.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_pull_request_review_comment";

/**
 * Main handler factory for create_pull_request_review_comment
 * Returns a message handler function that validates and buffers individual review comments.
 * Comments are buffered in a PR review buffer and submitted as a single PR review after
 * all messages have been processed.
 *
 * Supports two buffer modes:
 *   - Registry mode (config._prReviewBufferRegistry): per-PR buffers managed by a registry.
 *     Each distinct (repo, PR) pair gets its own independent buffer.
 *   - Legacy mode (config._prReviewBuffer): a single shared buffer (backward compat).
 *
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const defaultSide = config.side || "RIGHT";
  const commentTarget = config.target || "triggering";
  const maxCount = config.max || 10;
  const registry = config._prReviewBufferRegistry || null;
  const legacyBuffer = registry ? null : config._prReviewBuffer || null;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);
  let invocationContext = {};
  try {
    invocationContext = resolveInvocationContext(context);
  } catch (error) {
    const isValidationError = error instanceof Error && "code" in error && error.code === ERR_VALIDATION;
    if (isValidationError) {
      throw error;
    }
    const errorMessage = getErrorMessage(error);
    core.warning(`create_pull_request_review_comment: failed to resolve invocation context, using raw context: ${errorMessage}`);
  }
  const effectiveEventName = invocationContext.eventName || context.eventName;
  const effectivePayload = invocationContext.eventPayload || context.payload;
  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

  const maxMentions = parseIntTemplatable(config.mentions?.max, 50);
  let allowedMentionAliases = [];
  if (Array.isArray(config.allowedMentionAliases)) {
    allowedMentionAliases = config.allowedMentionAliases;
  } else if (config.mentions != null) {
    allowedMentionAliases = await resolveAllowedMentionsFromPayload(context, githubClient, core, config.mentions);
  }

  if (!registry && !legacyBuffer) {
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

  // Propagate per-handler staged flag to the PR review buffer
  if (isTemplatableTrue(config.staged)) {
    if (registry) registry.setDefaultStaged(true);
    else legacyBuffer.setStaged(true);
  }
  if (isStagedMode(config)) {
    logStagedPreviewInfo("PR review comments will be previewed without being submitted");
  }

  const pinnedCommitId = typeof config.commit_id === "string" ? config.commit_id.trim() : "";
  if (pinnedCommitId) {
    core.info(`create_pull_request_review_comment: commit-id pinned to ${pinnedCommitId}`);
    if (registry && typeof registry.setDefaultPinnedCommitId === "function") {
      registry.setDefaultPinnedCommitId(pinnedCommitId);
    } else if (legacyBuffer && typeof legacyBuffer.setPinnedCommitId === "function") {
      legacyBuffer.setPinnedCommitId(pinnedCommitId);
    }
  }

  // Extract triggering context for footer generation
  const triggeringIssueNumber = effectivePayload?.issue?.number && !effectivePayload?.issue?.pull_request ? effectivePayload.issue.number : undefined;
  const triggeringPRNumber = effectivePayload?.pull_request?.number || (effectivePayload?.issue?.pull_request ? effectivePayload.issue.number : undefined);
  const triggeringDiscussionNumber = effectivePayload?.discussion?.number;

  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runUrl = buildWorkflowRunUrl(context, context.repo);

  // Build the shared footer context object used by both modes.
  const footerCtx = {
    workflowName,
    runUrl,
    workflowSource,
    workflowSourceURL,
    triggeringIssueNumber,
    triggeringPRNumber,
    triggeringDiscussionNumber,
  };

  // For legacy single-buffer mode, set footer context once at init (unchanged behavior).
  // For registry mode, set the registry default so that buffers created via
  // submit_pull_request_review (without a create_pull_request_review_comment call) also
  // receive the correct footer context when getOrCreate() initialises them.
  if (legacyBuffer) {
    legacyBuffer.setFooterContext(footerCtx);
  } else if (registry) {
    registry.setDefaultFooterContext(footerCtx);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

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
      effectiveEventName === "pull_request" ||
      effectiveEventName === "pull_request_target" ||
      effectiveEventName === "pull_request_review" ||
      effectiveEventName === "pull_request_review_comment" ||
      (effectiveEventName === "issue_comment" && effectivePayload.issue && effectivePayload.issue.pull_request);

    // Validate context based on target configuration
    if (commentTarget === "triggering" && !isPRContext) {
      core.info('Target is "triggering" but not running in pull request context, skipping review comment creation');
      return {
        success: false,
        error: "Not in pull request context",
        skipped: true,
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
        if (Number.isNaN(pullRequestNumber) || pullRequestNumber <= 0) {
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
      if (Number.isNaN(pullRequestNumber) || pullRequestNumber <= 0) {
        core.warning(`Invalid pull request number in target configuration: ${commentTarget}`);
        return {
          success: false,
          error: `Invalid pull request number in target: ${commentTarget}`,
        };
      }
    } else {
      // Default behavior: use triggering PR
      if (effectivePayload.pull_request) {
        pullRequestNumber = effectivePayload.pull_request.number;
        pullRequest = effectivePayload.pull_request;
      } else if (effectivePayload.issue && effectivePayload.issue.pull_request) {
        pullRequestNumber = effectivePayload.issue.number;
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
        const { data: fullPR } = await githubClient.rest.pulls.get({
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

    const filterResult = await checkRequiredFilter(githubClient, repoParts, pullRequestNumber, requiredLabels, requiredTitlePrefix, "create_pull_request_review_comment");
    if (filterResult) return filterResult;

    // Parse line numbers
    const line = parseInt(commentItem.line, 10);
    if (Number.isNaN(line) || line <= 0) {
      core.warning(`Invalid line number: ${commentItem.line}`);
      return {
        success: false,
        error: `Invalid line number: ${commentItem.line}`,
      };
    }

    /** @type {any} */
    let startLine = undefined;
    if (commentItem.start_line) {
      startLine = parseInt(commentItem.start_line, 10);
      if (Number.isNaN(startLine) || startLine <= 0 || startLine > line) {
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

    // Obtain the buffer for this PR.
    // In registry mode: get or create a per-PR buffer (no cross-PR check needed).
    // In legacy mode: use the single shared buffer with cross-PR rejection.
    let buffer;
    if (registry) {
      buffer = registry.getOrCreate(itemRepo, pullRequestNumber);
      if (!buffer) {
        return { success: false, error: `Could not get review buffer for ${itemRepo}#${pullRequestNumber}` };
      }
      // Apply footer context to this buffer. setFooterContext() is first-wins internally,
      // so calling it on every message for the same PR is safe and no-ops after the first call.
      buffer.setFooterContext({
        workflowName,
        runUrl,
        workflowSource,
        workflowSourceURL,
        triggeringIssueNumber,
        triggeringPRNumber,
        triggeringDiscussionNumber,
      });
    } else {
      buffer = legacyBuffer;
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
      body: sanitizeContent(commentItem.body.trim(), { allowedAliases: allowedMentionAliases, maxMentions }),
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
