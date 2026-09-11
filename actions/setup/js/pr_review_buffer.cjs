// @ts-check
/// <reference types="@actions/github-script" />

/**
 * PR Review Buffer Factory
 *
 * Creates a buffer instance that collects PR review comments and review metadata
 * so they can be submitted as a single GitHub PR review via pulls.createReview().
 *
 * Cross-repository validation: The review buffer receives pre-validated repository
 * information from handlers like create_pr_review_comment.cjs which use
 * validateTargetRepo/checkAllowedRepo before setting the review context.
 *
 * Usage:
 *   const { createReviewBuffer } = require("./pr_review_buffer.cjs");
 *   const buffer = createReviewBuffer();
 *   buffer.addComment({ path: "file.js", line: 10, body: "Fix this" });
 *   buffer.setReviewMetadata("LGTM", "APPROVE");
 *   await buffer.submitReview();
 */

const { generateFooterWithMessages, getBodyFooterMessage, getDetectionCautionAlert } = require("./messages_footer.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { generateWorkflowCallIdMarker, matchesWorkflowId } = require("./generate_footer.cjs");
const { attachExecutionState, fetchPullRequestReviewState } = require("./safe_output_execution_metadata.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG, isTransientError, sleep } = require("./error_recovery.cjs");
const { ERR_API } = require("./error_codes.cjs");

const SUPERSEDE_REVIEW_MESSAGE = "Superseded by updated review from same workflow.";
const MAX_SUPERSEDE_REVIEW_PAGES = 10;
const MAX_REVIEW_BODY_LENGTH = 65000;
const DEFAULT_FALLBACK_EXCERPT_LENGTH = 500;
const FALLBACK_SECTION_HEADER = "### Comments that could not be inline-anchored";
const FALLBACK_EMPTY_COMMENT_BODY = "_(empty comment body)_";
const FALLBACK_TRUNCATION_SUFFIX = "\n\n_(Fallback review body truncated to fit GitHub length limits.)_";
const FALLBACK_OMISSION_NOTE = "_(Unanchored comment details omitted to fit GitHub length limits.)_";
const ELLIPSIS = "…";
// GitHub API message fragment returned when a PR is locked and review submission is rejected.
// Must be lowercase — compared against errorMessage.toLowerCase() for case-insensitive matching.
const LOCKED_PR_REVIEW_MESSAGE = "lock prevents review";

/** Returns true if the error message indicates a locked-PR 422 rejection. */
const isLockedPrError = errorMessage => errorMessage.toLowerCase().includes(LOCKED_PR_REVIEW_MESSAGE);
// Number of retries before treating a locked-PR 422 as a permanent soft skip.
// A small number is used so the run does not stall when the PR is permanently locked.
const LOCKED_PR_RETRY_COUNT = 3;
// Delay between lock-retry attempts. Short enough to keep the run responsive
// while still giving a transient lock a few seconds to clear.
const LOCKED_PR_RETRY_DELAY_MS = 5000;
// Keep review retries bounded so safe-outputs can recover from short installation-token
// quota stalls without spending most of the workflow timeout waiting for a reset.
const REVIEW_RATE_LIMIT_RETRY_CONFIG = {
  ...RATE_LIMIT_RETRY_CONFIG,
  maxRetries: 1,
  // Use short backoff + small jitter for review submission so retries remain bounded
  // while still avoiding synchronized thundering-herd retries.
  initialDelayMs: 1000,
  jitterMs: 200,
  maxDelayMs: 60000,
};

/**
 * @typedef {Object} BufferedComment
 * @property {string} path - File path relative to repo root
 * @property {number} line - Line number (end line for multi-line)
 * @property {string} body - Comment body text
 * @property {number} [start_line] - Start line for multi-line comments
 * @property {string} [side] - LEFT or RIGHT
 * @property {string} [start_side] - start_side for multi-line comments
 */

/**
 * @typedef {Object} ReviewMetadata
 * @property {string} body - Overall review body text
 * @property {string} event - Review event: APPROVE, REQUEST_CHANGES, or COMMENT
 */

/**
 * @typedef {Object} ReviewContext
 * @property {string} repo - Repository slug (owner/repo)
 * @property {{owner: string, repo: string}} repoParts - Parsed owner and repo
 * @property {number} pullRequestNumber - PR number
 * @property {Object} pullRequest - Full PR object with head.sha
 */

/**
 * Create a new PR review buffer instance.
 * All state is encapsulated in the returned object — no module-level globals.
 *
 * @returns {Object} Buffer instance with methods to add comments, set metadata, and submit review
 */
function createReviewBuffer() {
  /** @type {BufferedComment[]} */
  const bufferedComments = [];

  /** @type {ReviewMetadata | null} */
  let reviewMetadata = null;

  /** @type {ReviewContext | null} */
  let reviewContext = null;

  /** @type {{workflowName: string, runUrl: string, workflowSource: string, workflowSourceURL: string, triggeringIssueNumber: number|undefined, triggeringPRNumber: number|undefined, triggeringDiscussionNumber: number|undefined, bodyFooter?: string} | null} */
  let footerContext = null;

  /** @type {string} Footer mode: "always" (default), "none", or "if-body" */
  let footerMode = "always";

  /** @type {boolean} Staged mode: when true, preview review without submitting (set via setStaged(), reset on buffer clear) */
  let stagedMode = false;

  /** @type {boolean} When true, dismiss older same-workflow REQUEST_CHANGES reviews after posting a replacement review. */
  let supersedeOlderReviews = false;

  /** @type {string} When non-empty, pins the review to this commit SHA instead of the live PR head or GH_AW_HEAD_SHA. */
  let pinnedCommitId = "";

  /**
   * Best-effort execution-state capture.
   * When the installation token is out of quota, metadata collection should not
   * prevent the actual review submission from proceeding.
   *
   * @param {{owner: string, repo: string}} repoParts
   * @param {number} pullRequestNumber
   * @param {"before" | "after"} phase
   * @returns {Promise<Object | null>}
   */
  async function fetchReviewStateBestEffort(repoParts, pullRequestNumber, phase) {
    try {
      return await fetchPullRequestReviewState(github, repoParts, pullRequestNumber);
    } catch (error) {
      if (!isTransientError(error)) {
        throw new Error(`${ERR_API}: Failed to capture ${phase} PR review state for #${pullRequestNumber}: ${getErrorMessage(error)} (non-transient)`, { cause: error });
      }
      core.warning(`Failed to capture ${phase} PR review state for #${pullRequestNumber}: ${getErrorMessage(error)}. Continuing without execution-state metadata.`);
      return null;
    }
  }
  /**
   * Add a validated comment to the buffer.
   * Rejects comments targeting a different repo/PR than the first comment.
   * @param {BufferedComment} comment - Validated comment to buffer
   */
  function addComment(comment) {
    bufferedComments.push(comment);
    core.info(`Buffered review comment ${bufferedComments.length}: ${comment.path}:${comment.line}`);
  }

  /**
   * Set the review metadata (body and event).
   * Overwrites any previously set metadata (last call wins).
   * @param {string} body - Overall review body text
   * @param {string} event - Review event: APPROVE, REQUEST_CHANGES, or COMMENT
   */
  function setReviewMetadata(body, event) {
    reviewMetadata = { body, event };
    core.info(`Set review metadata: event=${event}, bodyLength=${body.length}`);
  }

  /**
   * Set the review context (target repo and PR).
   * Only sets if not already set (first comment determines context).
   * @param {ReviewContext} ctx - Review context
   * @returns {boolean} true if context was set, false if already set
   */
  function setReviewContext(ctx) {
    if (reviewContext === null) {
      reviewContext = ctx;
      core.info(`Set review context: ${ctx.repo}#${ctx.pullRequestNumber}`);
      return true;
    }
    return false;
  }

  /**
   * Get the current review context (repo and PR).
   * @returns {ReviewContext | null}
   */
  function getReviewContext() {
    return reviewContext;
  }

  /**
   * Set the footer context for generating review footer.
   * Only sets if not already set.
   * @param {Object} ctx - Footer context
   */
  function setFooterContext(ctx) {
    if (footerContext === null) {
      footerContext = ctx;
    }
  }

  /**
   * Set the footer mode for review body.
   * Supported modes:
   *   - "always" (default): Always include footer
   *   - "none": Never include footer
   *   - "if-body": Only include footer if review body is non-empty
   * Also accepts boolean values for backward compatibility:
   *   - true → "always"
   *   - false → "none"
   * Note: submit-pull-request-review.footer is emitted as a string by the Go compiler.
   * The global footer setting is emitted as a boolean and converted by getEffectiveFooterString.
   * @param {string|boolean} value - Footer mode string or boolean
   */
  function setFooterMode(value) {
    if (typeof value === "boolean") {
      // Normalize boolean to string mode (backward compatibility)
      const normalized = value ? "always" : "none";
      core.info(`Normalized boolean footer config (${value}) to mode: "${normalized}"`);
      footerMode = normalized;
    } else if (typeof value === "string") {
      // Validate string mode
      if (value === "always" || value === "none" || value === "if-body") {
        footerMode = value;
        core.info(`PR review footer mode set to "${footerMode}"`);
      } else {
        core.warning(`Invalid footer mode: "${value}". Using default "always". Valid values: "always", "none", "if-body"`);
        footerMode = "always";
      }
    } else {
      core.warning(`Invalid footer mode type: ${typeof value}. Using default "always".`);
      footerMode = "always";
    }
  }

  /**
   * Set staged mode for the review buffer.
   * When staged, submitReview() will preview the review without actually submitting.
   * @param {boolean} value - Whether staged mode is enabled
   */
  function setStaged(value) {
    stagedMode = value;
    if (value) {
      core.info("PR review buffer staged mode enabled");
    }
  }

  /**
   * Enable/disable superseding older same-workflow REQUEST_CHANGES reviews.
   * @param {boolean} value - Whether supersede behavior is enabled
   */
  function setSupersedeOlderReviews(value) {
    supersedeOlderReviews = value === true;
    if (supersedeOlderReviews) {
      core.info("PR review supersede mode enabled");
    }
  }

  /**
   * Pin the review to a specific commit SHA, overriding GH_AW_HEAD_SHA and the live PR head.
   * @param {string} commitId - The commit SHA to pin the review to
   */
  function setPinnedCommitId(commitId) {
    if (commitId && typeof commitId === "string") {
      pinnedCommitId = commitId;
      core.info(`PR review pinned to commit: ${commitId}`);
    }
  }

  /**
   * Check if there are buffered comments to submit.
   * @returns {boolean}
   */
  function hasBufferedComments() {
    return bufferedComments.length > 0;
  }

  /**
   * Check if review metadata has been set.
   * @returns {boolean}
   */
  function hasReviewMetadata() {
    return reviewMetadata !== null;
  }

  /**
   * Get the number of buffered comments.
   * @returns {number}
   */
  function getBufferedCount() {
    return bufferedComments.length;
  }

  /**
   * Submit the buffered review as a single pulls.createReview() call.
   * Supports body-only reviews (no inline comments) when metadata is set.
   * If no submit_pull_request_review message was provided, defaults to event: "COMMENT".
   *
   * @returns {Promise<Object>} Result with success status and review details
   */
  async function submitReview() {
    if (bufferedComments.length === 0 && !reviewMetadata) {
      core.info("No buffered review comments or review metadata to submit");
      return { success: true, skipped: true };
    }

    if (!reviewContext) {
      core.info("No review context set - skipping PR review submission");
      return {
        success: true,
        skipped: true,
        reason: "No review context available",
      };
    }

    const { repo, repoParts, pullRequestNumber, pullRequest } = reviewContext;

    if (!pullRequest || !pullRequest.head || !pullRequest.head.sha) {
      core.warning("Pull request head SHA not available - cannot submit review");
      return { success: false, error: "Pull request head SHA not available" };
    }

    // Use the head SHA captured at trigger time (GH_AW_HEAD_SHA, injected by the compiler)
    // when available, falling back to the live PR head SHA. This pins the review to the
    // commit the agent actually reviewed, preventing attribution drift when new commits are
    // pushed during the run (most common under workflow_run triggers where the safe_outputs
    // job runs after the agent job and pulls.get() may return a newer HEAD sha).
    // A user-specified pinnedCommitId (from the commit-id config option) takes highest priority.
    const awHeadSHA = process.env.GH_AW_HEAD_SHA || "";
    const resolvedCommitId = pinnedCommitId || awHeadSHA || pullRequest.head.sha;
    if (pinnedCommitId && pinnedCommitId !== pullRequest.head.sha) {
      core.info(`Using config-pinned commit SHA: ${pinnedCommitId} (PR head is now ${pullRequest.head.sha})`);
    } else if (awHeadSHA && awHeadSHA !== pullRequest.head.sha) {
      core.info(`Using trigger-time head SHA: ${awHeadSHA} (PR head is now ${pullRequest.head.sha})`);
    }

    // Determine review event and body
    let event = reviewMetadata ? reviewMetadata.event : "COMMENT";
    let body = reviewMetadata ? reviewMetadata.body : "";

    // Determine if we should add footer based on footer mode
    let shouldAddFooter = footerMode === "always";
    if (footerMode === "if-body") {
      // Only add footer if body is non-empty (has meaningful content)
      shouldAddFooter = body.trim().length > 0;
      core.info(`Footer mode "if-body": body is ${body.trim().length > 0 ? "non-empty" : "empty"}, ${shouldAddFooter ? "adding" : "skipping"} footer`);
    }

    // Inject CAUTION at top of body unconditionally if threat detection warning was raised,
    // independent of footer inclusion so the alert is never silently dropped.
    if (footerContext) {
      const detectionCaution = getDetectionCautionAlert(footerContext.workflowName, footerContext.runUrl);
      if (detectionCaution) {
        body = detectionCaution + "\n\n" + body;
        // When CAUTION is present, ensure the footer (and XML marker) is always included
        // so the review body is not empty of metadata, and re-evaluate shouldAddFooter.
        shouldAddFooter = true;
      }
    }

    // Add footer to review body if we should and we have footer context
    if (shouldAddFooter && footerContext) {
      body +=
        "\n\n" +
        generateFooterWithMessages(
          footerContext.workflowName,
          footerContext.runUrl,
          footerContext.workflowSource,
          footerContext.workflowSourceURL,
          footerContext.triggeringIssueNumber,
          footerContext.triggeringPRNumber,
          footerContext.triggeringDiscussionNumber,
          undefined,
          { skipDetectionCaution: true }
        );

      const callerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID || "";
      if (callerWorkflowId) {
        body += "\n" + generateWorkflowCallIdMarker(callerWorkflowId);
      }
    }
    if (footerContext) {
      const bodyFooter = getBodyFooterMessage(footerContext.bodyFooter, footerContext);
      if (bodyFooter) {
        body = body.trimEnd() + "\n\n" + bodyFooter.trimEnd();
      }
    }

    // Build comments array for the API
    let comments = bufferedComments.map(comment => {
      /** @type {any} */
      const apiComment = {
        path: comment.path,
        line: comment.line,
        body: comment.body,
      };

      if (comment.start_line !== undefined) {
        apiComment.start_line = comment.start_line;
      }

      if (comment.side) {
        apiComment.side = comment.side;
      }

      if (comment.start_line !== undefined && comment.start_side) {
        apiComment.start_side = comment.start_side;
      } else if (comment.start_line !== undefined && comment.side) {
        // Fall back to side when start_side is not explicitly provided
        apiComment.start_side = comment.side;
      }

      return apiComment;
    });

    // Sub-pattern B: Validate comment paths against the PR diff before POSTing.
    // Comments targeting paths not in the diff cause GitHub to return 422 "Path could not be resolved".
    if (comments.length > 0) {
      try {
        const changedPaths = new Set();
        let listPage = 1;
        // Cap at 10 pages (1,000 files). PRs with more than 1,000 changed files are
        // extremely rare and path validation is best-effort; we proceed without filtering
        // if any individual listFiles call throws (see catch block below).
        const MAX_LIST_FILES_PAGES = 10;
        while (listPage <= MAX_LIST_FILES_PAGES) {
          const { data: files } = await github.rest.pulls.listFiles({
            owner: repoParts.owner,
            repo: repoParts.repo,
            pull_number: pullRequestNumber,
            per_page: 100,
            page: listPage,
          });
          if (!Array.isArray(files) || files.length === 0) break;
          for (const f of files) {
            changedPaths.add(f.filename);
            // For renamed files, the old path (previous_filename) is also valid for review comments.
            if (f.previous_filename) changedPaths.add(f.previous_filename);
          }
          if (files.length < 100) break;
          listPage++;
        }
        // `listPage > MAX_LIST_FILES_PAGES` is only true when the loop exited via the
        // while-condition (not via a break), which only happens after a full page of 100
        // files caused listPage to be incremented past the cap. A partial page always
        // triggers the `files.length < 100` break first, so hitPageCap implies the last
        // page was full and there may be more files beyond the 1,000-file limit.
        // Fail-open in that case: the collected set is non-authoritative and filtering
        // would risk dropping valid comments on the un-fetched files.
        const hitPageCap = listPage > MAX_LIST_FILES_PAGES;
        // Only filter when we received a non-empty file list and did not hit the cap;
        // an empty list likely indicates an API quirk or a PR with no diff.
        if (changedPaths.size > 0 && !hitPageCap) {
          const invalidComments = comments.filter(c => !changedPaths.has(c.path));
          if (invalidComments.length > 0) {
            for (const c of invalidComments) {
              core.warning(`Skipping review comment at '${c.path}:${c.line}' — path not found in PR #${pullRequestNumber} diff`);
            }
            comments = comments.filter(c => changedPaths.has(c.path));
          }
        }
      } catch (pathValidationError) {
        core.warning(`Failed to validate comment paths against PR diff: ${getErrorMessage(pathValidationError)}. Proceeding without path validation.`);
      }
    }

    // Sub-pattern A: Guard against empty review submission (no body and no inline comments).
    // GitHub returns 422 "Unprocessable Entity" when both are absent.
    if (comments.length === 0 && !body) {
      const errorMsg = "Empty review: review body is empty and no inline comments are present" + (bufferedComments.length > 0 ? " (all comment paths were outside the PR diff)" : "") + ". Skipping POST to avoid 422.";
      core.warning(errorMsg);
      return { success: false, error: errorMsg };
    }

    core.info(`Submitting PR review on ${repo}#${pullRequestNumber}: event=${event}, comments=${comments.length}, bodyLength=${body.length}`);

    // If in staged mode, preview the review without submitting
    const isStaged = isStagedMode({ staged: stagedMode });
    if (isStaged) {
      let summaryContent = "## 🎭 Staged Mode: PR Review Preview\n\n";
      summaryContent += "The following PR review would be submitted if staged mode was disabled:\n\n";
      summaryContent += `**Target PR:** ${repo}#${pullRequestNumber}\n\n`;
      summaryContent += `**Review Event:** ${event}\n\n`;

      if (body) {
        summaryContent += `**Review Body:**\n${body}\n\n`;
      }

      if (comments.length > 0) {
        summaryContent += `**Inline Comments:** ${comments.length}\n\n`;
        for (let i = 0; i < comments.length; i++) {
          const comment = comments[i];
          summaryContent += `${i + 1}. \`${comment.path}:${comment.line}\` (${comment.side || "RIGHT"})\n`;
          summaryContent += `   ${comment.body.substring(0, 100)}${comment.body.length > 100 ? "..." : ""}\n\n`;
        }
      }

      summaryContent += "---\n\n";

      await core.summary.addRaw(summaryContent).write();
      core.info("📝 PR review preview written to step summary (staged mode)");
      return { success: true, staged: true };
    }

    const beforeState = await fetchReviewStateBestEffort(repoParts, pullRequestNumber, "before");

    /** @type {any} */
    const requestParams = {
      owner: repoParts.owner,
      repo: repoParts.repo,
      pull_number: pullRequestNumber,
      commit_id: resolvedCommitId,
      event: event,
    };

    // Only include comments if there are any
    if (comments.length > 0) {
      requestParams.comments = comments;
    }

    // Only include body if non-empty
    if (body) {
      requestParams.body = body;
    }

    /**
     * Dismiss older REQUEST_CHANGES reviews from the same workflow after posting a replacement review.
     * This is best-effort: failures are logged as warnings and do not fail the current review submission.
     * @param {number} currentReviewId
     */
    async function maybeSupersedeOlderReviews(currentReviewId) {
      if (!supersedeOlderReviews) {
        return;
      }

      const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
      const workflowCallId = process.env.GH_AW_CALLER_WORKFLOW_ID || "";
      if (!workflowId && !workflowCallId) {
        core.warning("supersede-older-reviews is enabled but neither GH_AW_WORKFLOW_ID nor GH_AW_CALLER_WORKFLOW_ID is set. Skipping stale review dismissal.");
        return;
      }
      const workflowCallMarker = workflowCallId ? generateWorkflowCallIdMarker(workflowCallId) : "";
      try {
        /** @type {any[]} */
        const reviews = [];
        let page = 1;
        const perPage = 100;
        while (page <= MAX_SUPERSEDE_REVIEW_PAGES) {
          const { data } = await github.rest.pulls.listReviews({
            owner: repoParts.owner,
            repo: repoParts.repo,
            pull_number: pullRequestNumber,
            per_page: perPage,
            page,
          });

          if (!Array.isArray(data) || data.length === 0) {
            break;
          }
          reviews.push(...data);
          if (data.length < perPage) {
            break;
          }
          page++;
        }
        if (page > MAX_SUPERSEDE_REVIEW_PAGES) {
          core.warning(`supersede-older-reviews reached pagination safety limit (${MAX_SUPERSEDE_REVIEW_PAGES} pages).`);
        }

        const staleReviews = reviews.filter(review => {
          if (!review || review.id === currentReviewId) return false;
          if (review.state !== "CHANGES_REQUESTED") return false;
          if (review.user?.type !== "Bot") return false;
          if (workflowCallMarker) {
            return review.body?.includes(workflowCallMarker) || false;
          }
          return matchesWorkflowId(review.body, workflowId);
        });

        for (const staleReview of staleReviews) {
          try {
            await github.rest.pulls.dismissReview({
              owner: repoParts.owner,
              repo: repoParts.repo,
              pull_number: pullRequestNumber,
              review_id: staleReview.id,
              message: SUPERSEDE_REVIEW_MESSAGE,
            });
            core.info(`Dismissed superseded review #${staleReview.id}`);
          } catch (dismissError) {
            core.warning(`Failed to dismiss stale review #${staleReview.id}: ${getErrorMessage(dismissError)}`);
          }
        }
      } catch (listOrSupersedeError) {
        core.warning(`Failed to supersede older reviews: ${getErrorMessage(listOrSupersedeError)}`);
      }
    }

    async function createReviewWithRetry(params) {
      return withRetry(() => github.rest.pulls.createReview(params), REVIEW_RATE_LIMIT_RETRY_CONFIG, `pulls.createReview ${repo}#${pullRequestNumber}`);
    }

    async function fetchAfterStateIfAvailable() {
      // Only fetch after-state when before-state capture succeeded; otherwise we are
      // already in degraded mode and avoid spending another API call on metadata.
      return beforeState ? fetchReviewStateBestEffort(repoParts, pullRequestNumber, "after") : null;
    }

    /**
     * Build the success result payload for a submitted PR review, wrapping it with
     * execution-state metadata. Extracted to avoid duplicating the shape across the
     * initial submit, own-PR-COMMENT retry, locked-PR retry, and body-only fallback paths.
     *
     * @param {{ id: number, html_url: string, state?: string }} review - Created review object
     * @param {string} resolvedEvent - The review event actually used (may differ from the requested event)
     * @param {number} commentCount - Number of inline comments included
     * @param {import("./safe_output_execution_metadata.cjs").ReviewState|null} afterState - Post-submit review state
     */
    function buildReviewSuccessResult(review, resolvedEvent, commentCount, afterState) {
      return attachExecutionState(
        {
          success: true,
          url: review.html_url,
          number: pullRequestNumber,
          review_id: review.id,
          review_url: review.html_url,
          pull_request_number: pullRequestNumber,
          repo: repo,
          event: resolvedEvent,
          comment_count: commentCount,
          metadata: {
            review_id: review.id,
            review_event: resolvedEvent,
            ...(review.state ? { review_state: review.state } : {}),
          },
        },
        beforeState,
        afterState
      );
    }

    try {
      const { data: review } = await createReviewWithRetry(requestParams);
      await maybeSupersedeOlderReviews(review.id);
      const afterState = await fetchAfterStateIfAvailable();

      core.info(`Created PR review #${review.id}: ${review.html_url}`);

      return buildReviewSuccessResult(review, event, comments.length, afterState);
    } catch (error) {
      const errorMessage = getErrorMessage(error);

      // Retry with COMMENT when the API rejects APPROVE/REQUEST_CHANGES on own PR.
      // This handles all token types (GITHUB_TOKEN lacks read:user scope for proactive checks).
      // These error message strings are returned verbatim by the GitHub API (Unprocessable Entity).
      const ownPrMessages = ["Can not request changes on your own pull request", "Can not approve your own pull request"];
      if (event !== "COMMENT" && ownPrMessages.some(msg => errorMessage.includes(msg))) {
        core.warning(`Cannot submit ${event} review on own PR. Retrying with event=COMMENT.`);
        try {
          requestParams.event = "COMMENT";
          const { data: review } = await createReviewWithRetry(requestParams);
          await maybeSupersedeOlderReviews(review.id);
          const afterState = await fetchAfterStateIfAvailable();
          core.info(`Created PR review #${review.id}: ${review.html_url}`);
          return buildReviewSuccessResult(review, "COMMENT", comments.length, afterState);
        } catch (retryError) {
          const retryErrorMsg = getErrorMessage(retryError);
          // If the COMMENT retry still fails due to unresolvable line(s), fall back to body-only COMMENT.
          if (retryErrorMsg.includes("Line could not be resolved") || retryErrorMsg.includes("Path could not be resolved")) {
            core.warning(`COMMENT retry on own PR failed with unresolvable line(s): ${retryErrorMsg}. Falling back to body-only COMMENT.`);
            try {
              const ownPrBodyOnlyParams = { ...requestParams };
              delete ownPrBodyOnlyParams.comments;
              ownPrBodyOnlyParams.event = "COMMENT";
              ownPrBodyOnlyParams.body = appendUnanchoredCommentsSection(typeof requestParams.body === "string" ? requestParams.body : "", comments);
              const { data: review } = await createReviewWithRetry(ownPrBodyOnlyParams);
              await maybeSupersedeOlderReviews(review.id);
              const afterState = await fetchAfterStateIfAvailable();
              core.info(`Created PR review #${review.id} (own-PR body-only COMMENT): ${review.html_url}`);
              return buildReviewSuccessResult(review, "COMMENT", 0, afterState);
            } catch (bodyOnlyError) {
              core.error(`Failed to submit body-only COMMENT review: ${getErrorMessage(bodyOnlyError)}`);
              return { success: false, error: getErrorMessage(bodyOnlyError) };
            }
          }
          core.error(`Failed to submit PR review on retry: ${retryErrorMsg}`);
          return {
            success: false,
            error: retryErrorMsg,
          };
        }
      }

      // When the PR is locked, retry a few times to detect if the lock is temporary,
      // then treat as a soft skip (success:true, skipped:true) so the run is not failed.
      // GitHub returns 422 with message "lock prevents review" for locked PRs.
      // We check the error message (which withRetry/enhanceError preserves in "Original error:")
      // rather than the status code, which may not survive error wrapping.
      if (isLockedPrError(errorMessage)) {
        core.warning(`PR #${pullRequestNumber} is locked (422 "${LOCKED_PR_REVIEW_MESSAGE}"). Retrying ${LOCKED_PR_RETRY_COUNT} time(s) to check if the lock is temporary...`);
        for (let attempt = 1; attempt <= LOCKED_PR_RETRY_COUNT; attempt++) {
          await sleep(LOCKED_PR_RETRY_DELAY_MS);
          try {
            const { data: review } = await createReviewWithRetry(requestParams);
            await maybeSupersedeOlderReviews(review.id);
            const afterState = await fetchAfterStateIfAvailable();
            core.info(`Created PR review #${review.id} after lock retry (attempt ${attempt}/${LOCKED_PR_RETRY_COUNT}): ${review.html_url}`);
            return buildReviewSuccessResult(review, event, comments.length, afterState);
          } catch (retryError) {
            const retryErrorMessage = getErrorMessage(retryError);
            if (isLockedPrError(retryErrorMessage)) {
              core.warning(`PR #${pullRequestNumber} is still locked (attempt ${attempt}/${LOCKED_PR_RETRY_COUNT})`);
            } else {
              // Different error on retry — surface as a regular failure
              core.error(`Failed to submit PR review on lock retry attempt ${attempt}: ${retryErrorMessage}`);
              return { success: false, error: retryErrorMessage };
            }
          }
        }
        // All retries exhausted — treat as a soft skip so the run stays green
        const skipMsg = `Review skipped — PR #${pullRequestNumber} is locked`;
        core.warning(skipMsg);
        return { success: true, skipped: true, reason: skipMsg, pr_locked: true };
      }

      // When the API cannot resolve a line or path reference in an inline comment, retry as a
      // body-only review so that the overall review (and its footer body) is still submitted
      // successfully. Matches both "Line could not be resolved" and "Path could not be resolved".
      if ((errorMessage.includes("Line could not be resolved") || errorMessage.includes("Path could not be resolved")) && comments.length > 0) {
        const unresolvableCommentIndices = extractUnresolvableCommentIndices(error, comments.length);
        if (unresolvableCommentIndices.length > 0 && unresolvableCommentIndices.length < comments.length) {
          const unresolvableCommentIndexSet = new Set(unresolvableCommentIndices);
          const resolvableComments = comments.filter((_, index) => !unresolvableCommentIndexSet.has(index));
          const unresolvableComments = comments.filter((_, index) => unresolvableCommentIndexSet.has(index));
          core.warning(
            `PR review submission failed due to unresolvable comment line(s): ${errorMessage}. ` +
              `Retrying with ${resolvableComments.length} resolvable inline comment(s); ` +
              `${unresolvableComments.length} comment(s) will be appended to the review body.`
          );
          try {
            const partialParams = {
              ...requestParams,
              comments: resolvableComments,
              body: appendUnanchoredCommentsSection(typeof requestParams.body === "string" ? requestParams.body : "", unresolvableComments),
            };
            const { data: review } = await createReviewWithRetry(partialParams);
            await maybeSupersedeOlderReviews(review.id);
            const afterState = await fetchAfterStateIfAvailable();
            core.info(`Created PR review #${review.id} (partial-anchor fallback): ${review.html_url}`);
            return buildReviewSuccessResult(review, event, resolvableComments.length, afterState);
          } catch (partialRetryError) {
            core.warning(`Failed to submit partially anchored PR review: ${getErrorMessage(partialRetryError)}. Falling back to body-only review.`);
          }
        }

        core.warning(`PR review submission failed due to unresolvable comment line(s): ${errorMessage}. Retrying as body-only review.`);
        const bodyOnlyParams = { ...requestParams };
        delete bodyOnlyParams.comments;
        bodyOnlyParams.body = appendUnanchoredCommentsSection(typeof requestParams.body === "string" ? requestParams.body : "", comments);
        try {
          const { data: review } = await createReviewWithRetry(bodyOnlyParams);
          await maybeSupersedeOlderReviews(review.id);
          const afterState = await fetchAfterStateIfAvailable();
          core.info(`Created PR review #${review.id} (body-only fallback): ${review.html_url}`);
          return buildReviewSuccessResult(review, event, 0, afterState);
        } catch (retryError) {
          const retryErrorMsg = getErrorMessage(retryError);
          // If body-only also fails because it's a self-authored PR, retry as body-only COMMENT.
          if (bodyOnlyParams.event !== "COMMENT" && ownPrMessages.some(msg => retryErrorMsg.includes(msg))) {
            core.warning(`Body-only ${bodyOnlyParams.event} review rejected on own PR. Retrying as body-only COMMENT.`);
            try {
              bodyOnlyParams.event = "COMMENT";
              const { data: review } = await createReviewWithRetry(bodyOnlyParams);
              await maybeSupersedeOlderReviews(review.id);
              const afterState = await fetchAfterStateIfAvailable();
              core.info(`Created PR review #${review.id} (body-only COMMENT fallback): ${review.html_url}`);
              return buildReviewSuccessResult(review, "COMMENT", 0, afterState);
            } catch (ownPrRetryError) {
              core.error(`Failed to submit body-only COMMENT review: ${getErrorMessage(ownPrRetryError)}`);
              return { success: false, error: getErrorMessage(ownPrRetryError) };
            }
          }
          core.error(`Failed to submit body-only PR review: ${retryErrorMsg}`);
          return {
            success: false,
            error: retryErrorMsg,
          };
        }
      }

      core.error(`Failed to submit PR review: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  }

  /**
   * Reset the buffer state (for testing).
   */
  function reset() {
    bufferedComments.length = 0;
    reviewMetadata = null;
    reviewContext = null;
    footerContext = null;
    footerMode = "always";
    stagedMode = false;
  }

  return {
    addComment,
    setReviewMetadata,
    setReviewContext,
    getReviewContext,
    setFooterContext,
    setFooterMode,
    setIncludeFooter: setFooterMode, // Backward compatibility alias
    setStaged,
    setSupersedeOlderReviews,
    setPinnedCommitId,
    hasBufferedComments,
    hasReviewMetadata,
    getBufferedCount,
    submitReview,
    reset,
  };
}

/**
 * Create a registry that manages per-PR review buffers.
 * Each distinct (repo, prNumber) pair gets its own independent buffer instance.
 *
 * Default settings applied to every newly created buffer (footerMode, footerContext,
 * staged, supersedeOlderReviews) can be configured via the returned setters before
 * any messages are processed.
 *
 * @returns {Object} Registry with getOrCreate, getAllEntries, hasAnyContent, and config setters
 */
function createPrReviewBufferRegistry() {
  /** @type {Map<string, Object>} */
  const bufferMap = new Map();

  /** @type {{repo: string, prNumber: number, buffer: Object}[]} */
  const insertionOrder = [];

  // Defaults applied to each new buffer when it is first created.
  /** @type {string|boolean} */
  let defaultFooterMode = "always";
  /** @type {Object | null} */
  let defaultFooterContext = null;
  let defaultStaged = false;
  let defaultSupersedeOlderReviews = false;
  /** @type {string} */
  let defaultPinnedCommitId = "";

  /**
   * Get or create the buffer for the given (repo, prNumber) pair.
   * Returns null when repo or prNumber are falsy (unresolvable target).
   * @param {string | null} repo - Repository slug (owner/repo)
   * @param {number | null} prNumber - Pull request number
   * @returns {Object | null} Buffer for this PR, or null if target cannot be resolved
   */
  function getOrCreate(repo, prNumber) {
    if (!repo || !prNumber) {
      return null;
    }
    const k = `${repo}#${prNumber}`;
    if (!bufferMap.has(k)) {
      const buffer = createReviewBuffer();
      buffer.setFooterMode(defaultFooterMode);
      if (defaultFooterContext) {
        buffer.setFooterContext(defaultFooterContext);
      }
      if (defaultStaged) {
        buffer.setStaged(true);
      }
      if (defaultSupersedeOlderReviews) {
        buffer.setSupersedeOlderReviews(true);
      }
      if (defaultPinnedCommitId) {
        buffer.setPinnedCommitId(defaultPinnedCommitId);
      }
      bufferMap.set(k, buffer);
      insertionOrder.push({ repo, prNumber, buffer });
      core.info(`PR review registry: created buffer for ${repo}#${prNumber}`);
    }
    return bufferMap.get(k);
  }

  /**
   * Return all buffered entries in insertion order.
   * @returns {{repo: string, prNumber: number, buffer: Object}[]}
   */
  function getAllEntries() {
    return insertionOrder;
  }

  /**
   * Returns true if any buffer has buffered comments or review metadata.
   * @returns {boolean}
   */
  function hasAnyContent() {
    return insertionOrder.some(e => e.buffer.hasBufferedComments() || e.buffer.hasReviewMetadata());
  }

  /** @param {string|boolean} value */
  function setDefaultFooterMode(value) {
    defaultFooterMode = value;
  }

  /** @param {Object} ctx */
  function setDefaultFooterContext(ctx) {
    defaultFooterContext = ctx;
  }

  /** @param {boolean} value */
  function setDefaultStaged(value) {
    defaultStaged = value === true;
  }

  /** @param {boolean} value */
  function setDefaultSupersedeOlderReviews(value) {
    defaultSupersedeOlderReviews = value === true;
  }

  /** @param {string} value */
  function setDefaultPinnedCommitId(value) {
    if (value && typeof value === "string") {
      defaultPinnedCommitId = value;
    }
  }

  return {
    getOrCreate,
    getAllEntries,
    hasAnyContent,
    setDefaultFooterMode,
    setDefaultFooterContext,
    setDefaultStaged,
    setDefaultSupersedeOlderReviews,
    setDefaultPinnedCommitId,
  };
}

module.exports = { createReviewBuffer, createPrReviewBufferRegistry };

/**
 * Parse API validation errors to identify specific inline comments that could not be resolved.
 * Returns 0-based indexes corresponding to items in the buffered comments array.
 *
 * @param {unknown} error
 * @param {number} totalComments
 * @returns {number[]}
 */
function extractUnresolvableCommentIndices(error, totalComments) {
  const indices = new Set();
  // prettier-ignore
  const errorAsAny = /** @type {any} */ (error);
  const candidateErrors = [errorAsAny, errorAsAny?.originalError, errorAsAny?.cause];

  for (const candidate of candidateErrors) {
    if (!candidate || typeof candidate !== "object") {
      continue;
    }
    // prettier-ignore
    const candidateAsAny = /** @type {any} */ (candidate);
    const apiErrors = candidateAsAny.response && candidateAsAny.response.data && Array.isArray(candidateAsAny.response.data.errors) ? candidateAsAny.response.data.errors : null;
    if (!apiErrors) {
      continue;
    }

    for (const apiError of apiErrors) {
      const field = typeof apiError?.field === "string" ? apiError.field : "";
      const message = typeof apiError?.message === "string" ? apiError.message : "";
      if (!message.includes("Line could not be resolved") && !message.includes("Path could not be resolved")) {
        continue;
      }

      const fieldMatch = field.match(/comments(?:\[|\.)(\d+)(?:\]|\.|$)/);
      if (!fieldMatch) {
        continue;
      }

      const parsedIndex = Number.parseInt(fieldMatch[1], 10);
      if (!Number.isInteger(parsedIndex) || parsedIndex < 0 || parsedIndex >= totalComments) {
        continue;
      }

      indices.add(parsedIndex);
    }
  }

  return Array.from(indices).sort((a, b) => a - b);
}

/**
 * Append a fallback section that preserves inline comment content when comments cannot be anchored.
 * @param {string} reviewBody
 * @param {BufferedComment[]} comments
 * @returns {string}
 */
function appendUnanchoredCommentsSection(reviewBody, comments) {
  const baseBody = reviewBody || "";
  const sectionPrefix = baseBody ? `\n\n${FALLBACK_SECTION_HEADER}\n\n` : `${FALLBACK_SECTION_HEADER}\n\n`;
  const overheadLength = comments.reduce((sum, comment, index) => {
    const separatorLength = index > 0 ? 2 : 0; // \n\n separator used by join("\n\n")
    return sum + separatorLength + renderUnanchoredCommentBlock(comment, "").length;
  }, 0);
  const availableExcerptChars = MAX_REVIEW_BODY_LENGTH - (baseBody.length + sectionPrefix.length + overheadLength);

  let perCommentExcerptLimit = DEFAULT_FALLBACK_EXCERPT_LENGTH;
  if (comments.length > 0) {
    if (availableExcerptChars <= 0) {
      perCommentExcerptLimit = 0;
    } else {
      perCommentExcerptLimit = Math.min(DEFAULT_FALLBACK_EXCERPT_LENGTH, Math.floor(availableExcerptChars / comments.length));
    }
  }

  const detailsBlocks = comments.map(comment => {
    const rawBody = (comment.body || "").trim();
    if (perCommentExcerptLimit <= 0) {
      return renderUnanchoredCommentBlock(comment, FALLBACK_EMPTY_COMMENT_BODY);
    }

    const shouldTruncate = perCommentExcerptLimit > 0 && rawBody.length > perCommentExcerptLimit;
    const truncateLength = perCommentExcerptLimit >= ELLIPSIS.length ? perCommentExcerptLimit - ELLIPSIS.length : 0;
    const truncatedBody = shouldTruncate ? rawBody.substring(0, truncateLength) : rawBody;
    const excerpt = shouldTruncate ? `${truncatedBody}${ELLIPSIS}` : rawBody;
    const safeExcerpt = excerpt || FALLBACK_EMPTY_COMMENT_BODY;
    return renderUnanchoredCommentBlock(comment, safeExcerpt);
  });

  const mergedBody = `${baseBody}${sectionPrefix}${detailsBlocks.join("\n\n")}`;
  if (mergedBody.length <= MAX_REVIEW_BODY_LENGTH) {
    return mergedBody;
  }

  const maxBodyLength = Math.max(0, MAX_REVIEW_BODY_LENGTH - FALLBACK_TRUNCATION_SUFFIX.length);
  if (baseBody.length > maxBodyLength) {
    return `${baseBody.substring(0, maxBodyLength)}${FALLBACK_TRUNCATION_SUFFIX}`;
  }

  const omissionBody = `${baseBody}${sectionPrefix}${FALLBACK_OMISSION_NOTE}`;
  if (omissionBody.length <= MAX_REVIEW_BODY_LENGTH) {
    return omissionBody;
  }

  return `${baseBody.substring(0, maxBodyLength)}${FALLBACK_TRUNCATION_SUFFIX}`;
}

/**
 * @param {BufferedComment} comment
 * @param {string} bodyText
 * @returns {string}
 */
function renderUnanchoredCommentBlock(comment, bodyText) {
  const summaryText = `${comment.path}:${comment.line}`;
  return `<details><summary>${escapeHtml(summaryText)}</summary>\n\n${escapeHtml(bodyText)}\n\n</details>`;
}

/**
 * @param {string} value
 * @returns {string}
 */
function escapeHtml(value) {
  return value.replace(/[&<>"']/g, character => {
    switch (character) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      case "'":
        return "&#39;";
      default:
        return character;
    }
  });
}
