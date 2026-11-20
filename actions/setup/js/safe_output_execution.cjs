// @ts-check

const { ERR_PERMISSION } = require("./error_codes.cjs");
const { classifySafeOutputResult, pickOutcomeField } = require("./safe_outputs_status.cjs");
const { AsyncLocalStorage } = require("node:async_hooks");

const invocationContext = new AsyncLocalStorage();

function withSafeOutputInvocation(context, callback) {
  return invocationContext.run(context, callback);
}

function currentSafeOutputInvocation() {
  return invocationContext.getStore();
}

/**
 * Keep isolated MCP bundles free of runner credentials and authorization I/O.
 * The trusted executor supplies the gate through its private invocation context.
 * @param {{inputs: unknown, scope?: {binding: string, refs: string[]}, config?: any, verification?: any}} options
 */
async function authorizeSafeOutputInvocation(options) {
  const state = currentSafeOutputInvocation();
  if (!state?.requirement && !options.config?.authorization) return { state: "not_configured" };
  if (!state?.requirement || !state.invocation || typeof state.authorize !== "function") {
    throw new Error(ERR_PERMISSION + ": Protected safe-output handlers require their trusted prepared invocation context");
  }
  return state.authorize(options);
}

/** @returns {import("./types/handler-factory").SafeOutputAuthorizationEvidence | undefined} */
function projectAuthorization(source) {
  if (!source || typeof source !== "object") return undefined;
  const result = {};
  if (!["not_configured", "exempt_builtin", "skipped_staged", "allowed", "denied", "error", "not_requested"].includes(source.state)) return undefined;
  result.state = source.state;
  for (const key of ["reason_code", "decision_id", "policy_revision", "invocation_id", "request_digest"]) {
    if (typeof source[key] === "string" && /^[A-Za-z0-9_.:/-]{1,256}$/.test(source[key])) result[key] = source[key];
  }
  for (const key of ["output_type", "output_name", "run_attempt"]) {
    if (typeof source[key] === "string" && /^[A-Za-z0-9_.:/-]{1,256}$/.test(source[key])) result[key] = source[key];
  }
  if (["manager", "native", "script", "action", "job", "standalone", "workflow", "queue"].includes(source.executor)) result.executor = source.executor;
  if (["invocation", "resolved_effect"].includes(source.binding)) result.binding = source.binding;
  if (["global", "local"].includes(source.requirement_source)) result.requirement_source = source.requirement_source;
  for (const key of ["issued_at", "expires_at"]) {
    const value = source[key];
    if (typeof value === "string" && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString() === value) result[key] = value;
  }
  if (typeof source.evidenceRecorded === "boolean") result.evidenceRecorded = source.evidenceRecorded;
  return result;
}

const EXECUTION_STATUSES = new Set(["success", "failed", "skipped", "warning", "cancelled", "deferred", "delegated", "staged", "failure"]);
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
    const advisory = /^\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/security\/advisories\/GHSA(?:-[23456789cfghjmpqrvwx]{4}){3}\/?$/.test(url.pathname);
    if (
      !advisory &&
      !/^\/(?:[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/(?:(?:issues|pull|discussions|runs|check-runs|actions\/artifacts|security\/code-scanning)\/[0-9]+|actions\/runs\/[0-9]+(?:\/(?:artifacts|job)\/[0-9]+)?|commit\/[a-fA-F0-9]{40,64})|(?:users|orgs)\/[A-Za-z0-9_.-]+\/projects\/[0-9]+)\/?$/.test(
        url.pathname
      )
    )
      return undefined;
    url.search = "";
    const fragment = advisory ? /^#advisory-comment-[1-9][0-9]*$/ : /^#(?:issuecomment-|discussioncomment-|pullrequestreview-|discussion_r)[0-9]+$/;
    if (!fragment.test(url.hash)) url.hash = "";
    return url.toString();
  } catch {
    return undefined;
  }
}

/**
 * Project safe-output execution evidence through one allowlist.
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
  if (typeof source.output_id === "string" && /^so_[A-Za-z0-9_-]{12}$/.test(source.output_id)) data.output_id = source.output_id;
  if (source.identifier === null) data.identifier = null;
  if (EXECUTION_STATUSES.has(source.status)) data.status = source.status;
  if (typeof source.staged === "boolean") data.staged = source.staged;
  if (typeof source.execution_trace === "string" && source.execution_trace.length <= 1024 && /^[A-Za-z]{4}(?:_[A-Za-z]{4})*$/.test(source.execution_trace)) data.execution_trace = source.execution_trace;
  if (source.operation_outcome === "indeterminate") data.operation_outcome = "indeterminate";
  const authorization = projectAuthorization(source.authorization);
  if (authorization) data.authorization = authorization;
  const url = canonicalSafeOutputUrl(source.url);
  if (url) data.url = url;
  return data;
}

/**
 * Only processing results, never agent requests, establish execution facts.
 * @param {any[]} results
 * @param {boolean} staged
 * @returns {Array<Record<string, any>>}
 */
function buildSafeOutputExecutions(results, staged = false) {
  return results.flatMap(record => {
    const fanout = Array.isArray(record.result);
    const items = fanout ? (record.result.length ? record.result : [undefined]) : [record.result];
    return items.map(result => {
      const item = fanout ? { ...record, result, success: record.success === false ? false : (result?.success ?? record.success), ...(record.result.length ? {} : { skipped: true }) } : record;
      const status = classifySafeOutputResult(item);
      const isStaged = staged || pickOutcomeField(item, "staged") === true;
      const url = !isStaged && (status === "success" || status === "warning") ? RESULT_URL_FIELDS.map(field => canonicalSafeOutputUrl(result?.[field])).find(Boolean) : undefined;
      return {
        timestamp: new Date().toISOString(),
        ...projectSafeOutputData({
          type: record.type,
          messageIndex: record.messageIndex,
          output_id: record.output_id,
          errorCode: record.errorCode,
          status: isStaged && status === "success" ? "staged" : status,
          staged: isStaged,
          operation_outcome: pickOutcomeField(item, "operation_outcome"),
          execution_trace: pickOutcomeField(item, "execution_trace"),
          authorization: record.authorization,
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
function finalizeBufferedReviewExecutions(results, repo, prNumber, review) {
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
        result.buffered = false;
        result.url = comment.url || review.review_url;
      } else if (completeComments) {
        record.skipped = true;
      }
    }
  }
}

module.exports = { canonicalSafeOutputUrl, projectSafeOutputData, buildSafeOutputExecutions, finalizeBufferedReviewExecutions, withSafeOutputInvocation, currentSafeOutputInvocation, authorizeSafeOutputInvocation, projectAuthorization };
