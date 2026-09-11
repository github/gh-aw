// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { resolveTarget, isTemplatableTrue, isStagedMode, logStagedPreviewInfo, checkRequiredFilter } = require("./safe_output_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "submit_pull_request_review";

/** @type {Set<string>} Valid review event types */
const VALID_EVENTS = new Set(["APPROVE", "REQUEST_CHANGES", "COMMENT"]);

/**
 * Main handler factory for submit_pull_request_review
 * Returns a message handler that stores review metadata (body and event)
 * in a PR review buffer. The actual review submission happens during the
 * handler manager's finalization step.
 *
 * Supports two buffer modes:
 *   - Registry mode (config._prReviewBufferRegistry): per-PR buffers managed by a registry.
 *     Target is resolved first; each distinct PR gets its own independent buffer.
 *   - Legacy mode (config._prReviewBuffer): a single shared buffer (backward compat).
 *
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const maxCount = config.max || 1;
  const targetConfig = config.target || "triggering";
  // Registry mode takes precedence over legacy single-buffer mode.
  // The two modes are mutually exclusive: when a registry is provided, _prReviewBuffer is ignored.
  const registry = config._prReviewBufferRegistry || null;
  const legacyBuffer = registry ? null : config._prReviewBuffer || null;
  if (registry && config._prReviewBuffer) {
    core.warning("submit_pull_request_review: Both _prReviewBufferRegistry and _prReviewBuffer were provided; registry mode takes precedence and _prReviewBuffer will be ignored.");
  }
  const supersedeOlderReviews = parseBoolTemplatable(config.supersede_older_reviews, false);
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);
  const footerContext = {
    workflowName: process.env.GH_AW_WORKFLOW_NAME || "Workflow",
    runUrl: buildWorkflowRunUrl(context, context.repo),
    workflowSource: process.env.GH_AW_WORKFLOW_SOURCE || "",
    workflowSourceURL: process.env.GH_AW_WORKFLOW_SOURCE_URL || "",
    triggeringIssueNumber: context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined,
    triggeringPRNumber: context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined),
    triggeringDiscussionNumber: context.payload?.discussion?.number,
    bodyFooter: config.body_footer,
  };
  if (registry) registry.setDefaultFooterContext(footerContext);
  else if (legacyBuffer) legacyBuffer.setFooterContext(footerContext);

  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

  // Build the allowed events set from config (empty set means all events are allowed)
  const allowedEvents = new Set(Array.isArray(config.allowed_events) && config.allowed_events.length > 0 ? config.allowed_events.map(e => String(e).toUpperCase()) : []);

  if (!registry && !legacyBuffer) {
    core.warning("submit_pull_request_review: No PR review buffer provided in config");
    return async function handleSubmitPRReview() {
      return { success: false, error: "No PR review buffer available" };
    };
  }

  core.info(`Submit PR review handler initialized: max=${maxCount}, target=${targetConfig}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (allowedEvents.size > 0) {
    core.info(`Allowed review events: ${Array.from(allowedEvents).join(", ")}`);
  }

  if (isTemplatableTrue(config.staged)) {
    if (registry) registry.setDefaultStaged(true);
    else legacyBuffer.setStaged(true);
  }
  if (isStagedMode(config)) {
    logStagedPreviewInfo("PR review will be previewed without being submitted");
  }
  if (supersedeOlderReviews) {
    core.warning("submit_pull_request_review: supersede-older-reviews is best-effort. Prefer allowed-events: [COMMENT] by default and use REQUEST_CHANGES only when merge-blocking is required.");
    if (registry) {
      registry.setDefaultSupersedeOlderReviews(true);
    } else if (typeof legacyBuffer.setSupersedeOlderReviews === "function") {
      legacyBuffer.setSupersedeOlderReviews(true);
    }
  }

  const pinnedCommitId = typeof config.commit_id === "string" ? config.commit_id.trim() : "";
  if (pinnedCommitId) {
    core.info(`submit_pull_request_review: commit-id pinned to ${pinnedCommitId}`);
    if (registry) {
      registry.setDefaultPinnedCommitId(pinnedCommitId);
    } else if (legacyBuffer && typeof legacyBuffer.setPinnedCommitId === "function") {
      legacyBuffer.setPinnedCommitId(pinnedCommitId);
    }
  }

  let processedCount = 0;

  /**
   * Message handler that stores review metadata
   * @param {Object} message - The submit_pull_request_review message
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs
   * @returns {Promise<Object>} Result with success status
   */
  return async function handleSubmitPRReview(message, resolvedTemporaryIds) {
    if (processedCount >= maxCount) {
      core.warning(`Skipping submit_pull_request_review: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    // Validate event field — default to COMMENT when not provided
    const event = message.event ? message.event.toUpperCase() : "COMMENT";
    if (!VALID_EVENTS.has(event)) {
      core.warning(`Invalid review event: ${message.event}. Must be one of: APPROVE, REQUEST_CHANGES, COMMENT`);
      return {
        success: false,
        error: `Invalid review event: ${message.event}. Must be one of: APPROVE, REQUEST_CHANGES, COMMENT`,
      };
    }

    // Enforce allowed-events filter (infrastructure-level enforcement)
    if (allowedEvents.size > 0 && !allowedEvents.has(event)) {
      const allowedList = Array.from(allowedEvents).join(", ");
      core.warning(`Review event '${event}' is not allowed. Allowed events: ${allowedList}`);
      return {
        success: false,
        error: `Review event '${event}' is not allowed by safe-outputs configuration. Allowed events: ${allowedList}`,
      };
    }

    // Body is required for REQUEST_CHANGES per GitHub API docs;
    // optional for APPROVE and COMMENT
    const body = message.body || "";
    if (event === "REQUEST_CHANGES" && !body) {
      core.warning("Review body is required for REQUEST_CHANGES");
      return {
        success: false,
        error: "Review body is required for REQUEST_CHANGES",
      };
    }

    // Only increment after validation passes
    processedCount++;

    if (registry) {
      return await handleWithRegistry(message, event, body);
    }
    return await handleWithLegacyBuffer(message, event, body);
  };

  /**
   * Registry path: resolve target PR first, then obtain a per-PR buffer and store metadata.
   * @param {Object} message
   * @param {string} event
   * @param {string} body
   * @returns {Promise<Object>}
   */
  async function handleWithRegistry(message, event, body) {
    const targetResult = resolveTarget({
      targetConfig,
      item: message,
      context,
      itemType: "PR review",
      supportsPR: false,
      supportsIssue: false,
    });

    if (!targetResult.success || !targetResult.number) {
      const errMsg = (targetResult.success === false ? targetResult.error : undefined) || "Could not determine target PR";
      core.warning(`Could not resolve PR for review: ${errMsg}`);
      return { success: false, error: errMsg };
    }

    const prNum = targetResult.number;

    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "PR review");
    if (!repoResult.success) {
      core.warning(`Could not resolve repository for PR review: ${repoResult.error}`);
      return { success: false, error: repoResult.error };
    }

    const { repo, repoParts } = repoResult;

    const buffer = registry.getOrCreate(repo, prNum);
    if (!buffer) {
      return { success: false, error: `Could not get review buffer for ${repo}#${prNum}` };
    }

    const filterResult = await checkRequiredFilter(githubClient, repoParts, prNum, requiredLabels, requiredTitlePrefix, "submit_pr_review");
    if (filterResult) return filterResult;

    if (buffer.hasReviewMetadata()) {
      const errMsg = `PR ${repo}#${prNum} already has a pending review submission. Only one submit_pull_request_review per PR per run is allowed; use target: "*" with max: 1 so each PR gets its own invocation.`;
      core.warning(`submit_pull_request_review: ${errMsg}`);
      return { success: false, error: errMsg };
    }

    core.info(`Setting review metadata for ${repo}#${prNum}: event=${event}, bodyLength=${body.length}`);
    buffer.setReviewMetadata(body, event);

    if (!buffer.getReviewContext()) {
      await setReviewContextOnBuffer(buffer, prNum, repo, repoParts, message);
    }

    return {
      success: true,
      event,
      body_length: body.length,
      pull_request_number: prNum,
      repo,
      deferred_manifest: true,
    };
  }

  /**
   * Legacy path: use the single shared buffer (backward compat for tests and older callers).
   * @param {Object} message
   * @param {string} event
   * @param {string} body
   * @returns {Promise<Object>}
   */
  async function handleWithLegacyBuffer(message, event, body) {
    const buffer = legacyBuffer;
    core.info(`Setting review metadata: event=${event}, bodyLength=${body.length}`);

    const existingReviewCtx = buffer.getReviewContext();
    if (existingReviewCtx) {
      const filterResult = await checkRequiredFilter(githubClient, existingReviewCtx.repoParts, existingReviewCtx.pullRequestNumber, requiredLabels, requiredTitlePrefix, "submit_pr_review");
      if (filterResult) return filterResult;
    }

    buffer.setReviewMetadata(body, event);

    if (!buffer.getReviewContext()) {
      const targetResult = resolveTarget({
        targetConfig,
        item: message,
        context,
        itemType: "PR review",
        supportsPR: false,
        supportsIssue: false,
      });

      if (!targetResult.success) {
        if (targetResult.shouldFail) {
          core.warning(`Could not resolve PR for review context: ${targetResult.error}`);
        }
      } else if (targetResult.number) {
        const prNum = targetResult.number;
        const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "PR review");
        if (!repoResult.success) {
          core.warning(`Could not resolve repository for PR review context: ${repoResult.error}`);
        } else {
          const { repo, repoParts } = repoResult;
          await setReviewContextOnBuffer(buffer, prNum, repo, repoParts, message);
        }
      }
    }

    return {
      success: true,
      event,
      body_length: body.length,
      deferred_manifest: true,
    };
  }

  /**
   * Fetch (or use payload) PR details and set review context on a buffer.
   * @param {Object} buffer
   * @param {number} prNum
   * @param {string} repo
   * @param {Object} repoParts
   * @param {Object} message
   */
  async function setReviewContextOnBuffer(buffer, prNum, repo, repoParts, message) {
    const payloadPR = context.payload?.pull_request;
    const usePayloadPR = payloadPR && payloadPR.number === prNum && payloadPR.head?.sha && repo === `${context.repo.owner}/${context.repo.repo}`;

    if (usePayloadPR) {
      buffer.setReviewContext({
        repo,
        repoParts,
        pullRequestNumber: payloadPR.number,
        pullRequest: payloadPR,
      });
      core.info(`Set review context from triggering PR: ${repo}#${payloadPR.number}`);
    } else {
      try {
        const { data: fetchedPR } = await githubClient.rest.pulls.get({
          owner: repoParts.owner,
          repo: repoParts.repo,
          pull_number: prNum,
        });
        if (fetchedPR?.head?.sha) {
          buffer.setReviewContext({
            repo,
            repoParts,
            pullRequestNumber: fetchedPR.number,
            pullRequest: fetchedPR,
          });
          core.info(`Set review context from target: ${repo}#${fetchedPR.number}`);
        } else {
          core.warning("Fetched PR missing head.sha - cannot set review context");
        }
      } catch (fetchErr) {
        core.warning(`Could not fetch PR #${prNum} for review context: ${getErrorMessage(fetchErr)}`);
      }
    }
  }
}

module.exports = { main };
