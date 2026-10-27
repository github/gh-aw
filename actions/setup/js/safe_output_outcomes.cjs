// @ts-check

const { classifySafeOutputResult, pickOutcomeField } = require("./safe_outputs_status.cjs");

const OUTCOME_STATUSES = new Set(["success", "failed", "skipped", "warning", "cancelled", "deferred", "delegated", "staged", "failure"]);
const RESULT_URL_FIELDS = [
  "url",
  "html_url",
  "issue_url",
  "pull_request_url",
  "discussion_url",
  "comment_url",
  "reply_url",
  "review_url",
  "item_url",
  "commit_url",
  "check_run_url",
  "projectUrl",
  "autofixUrl",
  "artifactUrl",
  "run_url",
  "workflow_run_url",
];

/**
 * Keep resource identity, never signed query parameters or free-form URL paths.
 * Unknown providers are omitted rather than risking titles or credentials.
 * @param {unknown} value
 * @returns {string|undefined}
 */
function canonicalSafeOutputUrl(value) {
  if (typeof value !== "string" || /[\u0000-\u0020\u007f]/.test(value)) return undefined;
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" || url.username || url.password) return undefined;
    const githubOrigin = new URL(process.env.GITHUB_SERVER_URL || "https://github.com").origin;
    if (url.origin !== "https://github.com" && url.origin !== githubOrigin) return undefined;
    if (
      !/^\/(?:[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/(?:(?:issues|pull|discussions|runs|check-runs|actions\/artifacts|security\/code-scanning)\/[0-9]+|actions\/runs\/[0-9]+(?:\/(?:artifacts|job)\/[0-9]+)?|commit\/[a-fA-F0-9]{40,64})|(?:users|orgs)\/[A-Za-z0-9_.-]+\/projects\/[0-9]+)\/?$/.test(
        url.pathname
      )
    )
      return undefined;
    url.search = "";
    if (!/^#(?:issuecomment-|discussioncomment-|pullrequestreview-|discussion_r)[0-9]+$/.test(url.hash)) url.hash = "";
    return url.toString();
  } catch {
    return undefined;
  }
}

/**
 * Project both new outcomes and legacy safe-output evidence through one allowlist.
 * @param {any} source
 * @returns {Record<string, any>}
 */
function projectSafeOutputData(source) {
  const data = {};
  if (!source || typeof source !== "object" || Array.isArray(source)) return data;
  for (const key of ["type", "repo", "provider", "identifier", "errorCode"]) {
    const value = source[key] ?? (key === "repo" ? source.repository : key === "errorCode" ? source.error_code : undefined);
    if (typeof value === "string" && /^[A-Za-z0-9_./:-]{1,200}$/.test(value)) data[key] = value;
  }
  for (const key of ["number", "messageIndex"]) {
    if (Number.isSafeInteger(source[key]) && source[key] >= 0) data[key] = source[key];
  }
  if (source.identifier === null) data.identifier = null;
  if (OUTCOME_STATUSES.has(source.status)) data.status = source.status;
  if (typeof source.staged === "boolean") data.staged = source.staged;
  const url = canonicalSafeOutputUrl(source.url);
  if (url) data.url = url;
  return data;
}

/**
 * Only processing results, never agent requests, establish application outcomes.
 * @param {any[]} results
 * @param {boolean} staged
 * @returns {Array<Record<string, any>>}
 */
function buildSafeOutputOutcomes(results, staged = false) {
  return results.flatMap(record => {
    const fanout = Array.isArray(record.result);
    const items = fanout ? (record.result.length ? record.result : [undefined]) : [record.result];
    return items.map(result => {
      const item = fanout ? { ...record, result, success: record.success === false ? false : (result?.success ?? record.success), ...(record.result.length ? {} : { skipped: true }) } : record;
      let status = classifySafeOutputResult(item);
      if (status === "success" && pickOutcomeField(item, "deferred_manifest")) status = "deferred";
      const isStaged = staged || pickOutcomeField(item, "staged") === true;
      const url = !isStaged && (status === "success" || status === "warning") ? RESULT_URL_FIELDS.map(field => canonicalSafeOutputUrl(result?.[field])).find(Boolean) : undefined;
      return {
        timestamp: new Date().toISOString(),
        ...projectSafeOutputData({
          type: record.type,
          messageIndex: record.messageIndex,
          status: isStaged && status === "success" ? "staged" : status,
          staged: isStaged,
          url,
        }),
      };
    });
  });
}

/**
 * Resolve provisional buffer records using only the submitted review response.
 * @param {any[]} results
 * @param {string} repo
 * @param {number} prNumber
 * @param {any} review
 */
function finalizeBufferedReviewOutcomes(results, repo, prNumber, review) {
  const comments = Array.isArray(review.review_comments) ? [...review.review_comments] : [];
  const completeComments = comments.length === review.comment_count;
  for (const record of results) {
    const result = record.result;
    if (!record.success || !result?.deferred_manifest || result.repo !== repo || result.pull_request_number !== prNumber) continue;
    if (review.staged) {
      result.deferred_manifest = false;
      result.staged = true;
    } else if (record.type === "submit_pull_request_review") {
      result.deferred_manifest = false;
      result.review_url = review.review_url;
    } else if (record.type === "create_pull_request_review_comment") {
      const index = comments.findIndex(comment => ["path", "line", "start_line", "side"].every(key => comment.metadata?.[key] === result.metadata?.[key]));
      if (index >= 0) {
        const [comment] = comments.splice(index, 1);
        result.deferred_manifest = false;
        result.url = comment.url || review.review_url;
      } else if (completeComments) {
        record.skipped = true;
      }
    }
  }
}

module.exports = { canonicalSafeOutputUrl, projectSafeOutputData, buildSafeOutputOutcomes, finalizeBufferedReviewOutcomes };
