// @ts-check
/// <reference types="@actions/github-script" />

/**
 * PR Review Buffer Factory
 *
 * Creates a buffer instance that collects PR review comments and review metadata
 * so they can be submitted as a single GitHub PR review via pulls.createReview().
 *
 * Usage:
 *   const { createReviewBuffer } = require("./pr_review_buffer.cjs");
 *   const buffer = createReviewBuffer();
 *   buffer.addComment({ path: "file.js", line: 10, body: "Fix this" });
 *   buffer.setReviewMetadata("LGTM", "APPROVE");
 *   await buffer.submitReview();
 */

const { generateFooterWithMessages } = require("./messages_footer.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

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

  /** @type {{workflowName: string, runUrl: string, workflowSource: string, workflowSourceURL: string, triggeringIssueNumber: number|undefined, triggeringPRNumber: number|undefined, triggeringDiscussionNumber: number|undefined} | null} */
  let footerContext = null;

  /** @type {boolean} Whether to include footer in review body (default: true, controlled by config.footer) */
  let includeFooter = true;

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
   * Set whether to include footer in review body.
   * Controlled by the `footer` config option (default: true).
   * @param {boolean} value - Whether to include footer
   */
  function setIncludeFooter(value) {
    includeFooter = value;
    core.info(`PR review footer ${value ? "enabled" : "disabled"}`);
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
      core.warning("No review context set - cannot submit review");
      return { success: false, error: "No review context available" };
    }

    const { repo, repoParts, pullRequestNumber, pullRequest } = reviewContext;

    if (!pullRequest || !pullRequest.head || !pullRequest.head.sha) {
      core.warning("Pull request head SHA not available - cannot submit review");
      return { success: false, error: "Pull request head SHA not available" };
    }

    // Determine review event and body
    const event = reviewMetadata ? reviewMetadata.event : "COMMENT";
    let body = reviewMetadata ? reviewMetadata.body : "";

    // Add footer to review body if enabled and we have footer context.
    // Footer is always added (even for body-less reviews) to track which workflow submitted the review.
    if (includeFooter && footerContext) {
      body += generateFooterWithMessages(
        footerContext.workflowName,
        footerContext.runUrl,
        footerContext.workflowSource,
        footerContext.workflowSourceURL,
        footerContext.triggeringIssueNumber,
        footerContext.triggeringPRNumber,
        footerContext.triggeringDiscussionNumber
      );
    }

    // Build comments array for the API
    const comments = bufferedComments.map(comment => {
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

    core.info(`Submitting PR review on ${repo}#${pullRequestNumber}: event=${event}, comments=${comments.length}, bodyLength=${body.length}`);

    try {
      /** @type {any} */
      const requestParams = {
        owner: repoParts.owner,
        repo: repoParts.repo,
        pull_number: pullRequestNumber,
        commit_id: pullRequest.head.sha,
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

      const { data: review } = await github.rest.pulls.createReview(requestParams);

      core.info(`Created PR review #${review.id}: ${review.html_url}`);

      return {
        success: true,
        review_id: review.id,
        review_url: review.html_url,
        pull_request_number: pullRequestNumber,
        repo: repo,
        event: event,
        comment_count: comments.length,
      };
    } catch (error) {
      core.error(`Failed to submit PR review: ${getErrorMessage(error)}`);
      return {
        success: false,
        error: getErrorMessage(error),
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
    includeFooter = true;
  }

  return {
    addComment,
    setReviewMetadata,
    setReviewContext,
    getReviewContext,
    setFooterContext,
    setIncludeFooter,
    hasBufferedComments,
    hasReviewMetadata,
    getBufferedCount,
    submitReview,
    reset,
  };
}

module.exports = { createReviewBuffer };
