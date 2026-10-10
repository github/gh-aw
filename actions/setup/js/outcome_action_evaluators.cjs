// @ts-check

const { isNonBotActor, after, secondsBetween, nonBotCommentsAfter, itemNumber, itemRepo } = require("./outcome_evidence.cjs");
const reviews = require("./outcome_review_evaluators.cjs");

const SKIPPED = new Set(["noop", "missing_tool", "missing_data", "report_incomplete"]);
const UNSUPPORTED = new Set(["create_discussion", "close_discussion", "update_discussion", "hide_comment", "create_pull_request_review_comment", "resolve_pull_request_review_thread"]);

/** @param {string} status @param {string} strength @param {string} signal @param {string} [detail] @returns {import("./outcome_types").OutcomeResult} */
function result(status, strength, signal, detail = signal) {
  return {
    result: status,
    outcome_status: status,
    evidence_strength: strength,
    signal,
    detail,
    resolution_sec: null,
    pending_age_sec: null,
    review_comments: null,
    changed_files: null,
    additions: null,
    deletions: null,
    reactions_total: null,
    reactions_positive: null,
    reactions_negative: null,
    comments: null,
    human_comments: 0,
    human_reviews: 0,
    human_edits: 0,
    zero_touch: false,
  };
}

/** @param {any} item @param {string} repo @param {(endpoint: string) => any} api @param {number} nowMs @param {(status: string, detail: string) => any} normalize */
function evaluateAction(item, repo, api, nowMs, normalize) {
  repo = itemRepo(item, repo);
  const number = itemNumber(item);
  const type = item.type || "";
  let out = result("unknown", "none", "missing_reference");
  let failedPrimary = false;
  const get = (/** @type {string} */ endpoint, primary = false) => {
    try {
      const value = api(`repos/${repo}/${endpoint}`);
      if (value == null) throw new Error(`GitHub outcome API failed: ${endpoint}`);
      return value;
    } catch (error) {
      failedPrimary = primary;
      throw error;
    }
  };
  try {
    if (SKIPPED.has(type)) return result("skipped", "none", "skipped");
    if (UNSUPPORTED.has(type)) return result("unknown", "none", "unsupported_evaluator", "no action-specific evaluator");
    if (!repo || (!number && type !== "dispatch_workflow")) {
      if (["update_issue", "update_pull_request", "replace_label"].includes(type)) return result("unknown", "none", "missing_execution_state");
      return out;
    }
    switch (type) {
      case "create_issue":
        out = evaluateIssue(item, number, get);
        break;
      case "create_pull_request":
        out = evaluatePR(item, number, get);
        break;
      case "add_comment":
        out = evaluateComment(item, number, get);
        break;
      case "add_labels":
      case "replace_label":
        out = evaluateLabels(item, number, get);
        break;
      case "close_issue":
      case "close_pull_request":
        out = evaluateClose(type, number, get);
        break;
      case "dispatch_workflow":
        out = evaluateDispatch(item, get);
        break;
      case "push_to_pull_request_branch":
        out = evaluatePush(item, number, get);
        break;
      case "assign_milestone": {
        const expected = reviews.getMetadataNumber(item, "milestone_number");
        if (expected === null) return result("unknown", "none", "missing_execution_state");
        const issue = get(`issues/${number}`);
        out = issue.milestone?.number === expected ? result("accepted", "medium", "milestone_assigned") : result("rejected", "medium", "milestone_removed");
        break;
      }
      case "mark_pull_request_as_ready_for_review": {
        const submitted = get(`pulls/${number}/reviews`);
        if (submitted.some((/** @type {any} */ review) => isNonBotActor(review.user) && review.state !== "PENDING" && after(review.submitted_at, item.timestamp))) {
          out = result("accepted", "medium", "reviewed");
        } else {
          const pr = get(`pulls/${number}`);
          out = pr.state === "open" ? result("pending", "medium", "awaiting_review") : result("ignored", "medium", "ignored");
        }
        break;
      }
      case "assign_to_agent":
        out = evaluateAssignment(item, number, get);
        break;
      case "add_reviewer":
      case "submit_pull_request_review":
      case "update_issue":
      case "update_pull_request": {
        const evaluators = {
          add_reviewer: reviews.evaluateAddReviewer,
          submit_pull_request_review: reviews.evaluateSubmitPullRequestReview,
          update_issue: reviews.evaluateUpdateIssue,
          update_pull_request: reviews.evaluateUpdatePullRequest,
        };
        const evaluated = evaluators[type](item, repo, (/** @type {string} */ endpoint) => get(endpoint.replace(`repos/${repo}/`, "")));
        out = { ...result("unknown", "weak", "unknown"), ...evaluated, ...normalize(evaluated.result, evaluated.detail) };
        if (evaluated.detail === "review not found") Object.assign(out, result("unknown", "weak", "review_missing"));
        if (evaluated.detail === "team reviewer membership is unverified") Object.assign(out, result("unknown", "weak", "team_membership_unverified"));
        break;
      }
      default:
        get(`issues/${number}`);
        out = result("unknown", "weak", "target_exists_only", "object still exists");
    }
  } catch (error) {
    const status = error && typeof error === "object" && "status" in error ? error.status : null;
    out = status === 404 && failedPrimary ? result("rejected", "strong", "deleted", "deleted or inaccessible") : result("error", "weak", "evaluation_error", String(error));
  }
  const age = secondsBetween(item.timestamp, new Date(nowMs).toISOString());
  if (out.outcome_status === "pending" && age !== null) out.pending_age_sec = age;
  return out;
}

/** @param {any} item @param {number} number @param {(endpoint: string, primary?: boolean) => any} get */
function evaluateIssue(item, number, get) {
  const issue = get(`issues/${number}`, true);
  if (issue.state === "closed") {
    if (issue.state_reason === "completed") {
      return { ...result("accepted", "strong", "completed"), resolution_sec: secondsBetween(item.timestamp, issue.closed_at) };
    }
    if (issue.state_reason === "not_planned") {
      const bot = closeByBot(number, get);
      return { ...result(bot ? "lifecycle" : "rejected", bot ? "medium" : "strong", bot ? "lifecycle" : "closed_not_planned"), resolution_sec: secondsBetween(item.timestamp, issue.closed_at) };
    }
    return result("unknown", "weak", "unknown", "closed without a resolution reason");
  }
  if (issue.state !== "open") return result("unknown", "weak", "unknown");
  const comments = nonBotCommentsAfter(get(`issues/${number}/comments`), item.timestamp);
  return { ...result("pending", "medium", comments ? "acted_on" : "open"), human_comments: comments };
}

/** @param {any} item @param {number} number @param {(endpoint: string, primary?: boolean) => any} get */
function evaluatePR(item, number, get) {
  const pr = get(`pulls/${number}`, true);
  const out =
    pr.merged === true
      ? result("accepted", "strong", "merged")
      : pr.state === "closed"
        ? result("rejected", "strong", "closed_without_merge")
        : pr.state === "open"
          ? result("pending", "medium", "open")
          : result("unknown", "weak", "unknown");
  out.resolution_sec = secondsBetween(item.timestamp, pr.merged_at || pr.closed_at);
  // Acceptance is authoritative even if supplementary human-effort evidence is unavailable.
  const optional = (/** @type {string} */ endpoint) => {
    try {
      return get(endpoint);
    } catch (error) {
      console.warn(`Outcome supplementary evidence unavailable: ${String(error)}`);
      return null;
    }
  };
  const comments = optional(`issues/${number}/comments`);
  const submitted = optional(`pulls/${number}/reviews`);
  const commits = optional(`pulls/${number}/commits`);
  if (Array.isArray(comments)) out.human_comments = nonBotCommentsAfter(comments, item.timestamp);
  if (Array.isArray(submitted)) out.human_reviews = submitted.filter(review => review.state !== "PENDING" && isNonBotActor(review.user) && after(review.submitted_at, item.timestamp)).length;
  if (Array.isArray(commits)) out.human_edits = commits.filter(commit => isNonBotActor(commit.author) && (after(commit.commit?.committer?.date, item.timestamp) || after(commit.commit?.author?.date, item.timestamp))).length;
  out.zero_touch =
    out.result === "accepted" &&
    Number.isFinite(Date.parse(item.timestamp)) &&
    Array.isArray(comments) &&
    Array.isArray(submitted) &&
    Array.isArray(commits) &&
    comments.every(comment => comment.user?.login && Number.isFinite(Date.parse(comment.created_at))) &&
    submitted.every(review => review.state === "PENDING" || (review.user?.login && Number.isFinite(Date.parse(review.submitted_at)))) &&
    commits.every(commit => commit.author?.login && (Number.isFinite(Date.parse(commit.commit?.committer?.date)) || Number.isFinite(Date.parse(commit.commit?.author?.date)))) &&
    out.human_comments === 0 &&
    out.human_reviews === 0 &&
    out.human_edits === 0;
  return out;
}

/** @param {any} item @param {number} number @param {(endpoint: string, primary?: boolean) => any} get */
function evaluateComment(item, number, get) {
  const match = String(item.url || "").match(/(?:#issuecomment-|\/comments\/)(\d+)/);
  if (!match) return result("unknown", "none", "missing_reference");
  const comment = get(`issues/comments/${match[1]}`, true);
  const reactions = Number(comment.reactions?.total_count || 0);
  let replies = 0;
  try {
    replies = nonBotCommentsAfter(get(`issues/${number}/comments`), comment.created_at);
  } catch (error) {
    if (reactions === 0) throw error;
    console.warn(`Outcome supplementary evidence unavailable: ${String(error)}`);
  }
  return { ...result(reactions > 0 || replies > 0 ? "accepted" : "pending", "medium", reactions > 0 || replies > 0 ? "acted_on" : "pending"), human_comments: replies, reactions_total: reactions };
}

/** @param {number} number @param {(endpoint: string) => any} get */
function closeByBot(number, get) {
  const events = get(`issues/${number}/events`);
  const close = events.filter((/** @type {any} */ event) => event.event === "closed").slice(-1)[0];
  if (!close?.actor?.login) throw new Error("latest close actor is unavailable");
  return !isNonBotActor(close.actor);
}

/** @param {string} type @param {number} number @param {(endpoint: string) => any} get */
function evaluateClose(type, number, get) {
  const data = get(`${type === "close_issue" ? "issues" : "pulls"}/${number}`);
  if (data.state !== "closed") return result("rejected", "strong", "reopened");
  if (data.merged === true) return result("rejected", "strong", "closed_by_merge");
  return closeByBot(number, get) ? result("lifecycle_close", "medium", "lifecycle_close") : result("accepted", "strong", "closed");
}

/** @param {any} value @returns {string[]} */
function labels(value) {
  return Array.isArray(value)
    ? value
        .map(label =>
          String(typeof label === "object" ? label.name : label)
            .trim()
            .toLowerCase()
        )
        .sort()
    : [];
}

/** @param {any} item @param {number} number @param {(endpoint: string) => any} get */
function evaluateLabels(item, number, get) {
  const before = item.before_state?.labels ?? item.labelsBefore;
  const after = item.after_state?.labels;
  const add = item.type === "replace_label" ? after : item.labelsAdded?.length ? item.labelsAdded : item.labels;
  if (!Array.isArray(before) || !Array.isArray(add)) return result("unknown", "none", "missing_execution_state");
  const added = labels(add).filter(label => !labels(before).includes(label));
  const removed = item.type === "replace_label" ? labels(before).filter(label => !labels(after).includes(label)) : [];
  if (added.length === 0 && removed.length === 0) return result("unknown", "none", "no_state_delta");
  const current = labels(item.type === "replace_label" ? get(`issues/${number}`).labels : get(`issues/${number}/labels`));
  if (added.every(label => current.includes(label)) && removed.every(label => !current.includes(label))) return result("accepted", "medium", "state_retained");
  if (item.type === "replace_label" && added.every(label => !current.includes(label)) && removed.every(label => current.includes(label))) return result("rejected", "strong", "state_reverted");
  return result("rejected", "strong", "state_replaced");
}

/** @param {any} item @param {(endpoint: string) => any} get */
function evaluateDispatch(item, get) {
  const id = item.metadata?.run_id;
  if (!Number.isSafeInteger(id) || id <= 0) return result("pending", "none", "missing_run_id");
  const run = get(`actions/runs/${id}`);
  if (run.status !== "completed") return result("pending", "medium", "workflow_pending");
  if (run.conclusion === "success") return result("accepted", "strong", "workflow_success");
  return ["failure", "timed_out", "cancelled", "action_required"].includes(run.conclusion) ? result("rejected", "strong", "workflow_failed") : result("ignored", "medium", "workflow_no_effect");
}

/** @param {any} item @param {number} number @param {(endpoint: string) => any} get */
function evaluatePush(item, number, get) {
  const pr = get(`pulls/${number}`);
  if (pr.merged !== true) return pr.state === "closed" ? result("rejected", "strong", "closed_without_merge") : result("pending", "medium", "open");
  const shas = [...(item.metadata?.pushed_commit_shas || []), ...(item.metadata?.commit_sha ? [item.metadata.commit_sha] : [])];
  if (!shas.length || !/^[a-fA-F0-9]{7,40}$/.test(pr.merge_commit_sha || "") || shas.some(sha => !/^[a-fA-F0-9]{7,40}$/.test(sha))) return result("unknown", "none", "missing_execution_state");
  for (const sha of shas) {
    const comparison = get(`compare/${sha}...${pr.merge_commit_sha}`);
    if (!["ahead", "identical"].includes(comparison.status)) return result("unknown", "weak", "commit_retention_unknown");
  }
  return result("accepted", "strong", "merged");
}

/** @param {any} item @param {number} number @param {(endpoint: string) => any} get */
function evaluateAssignment(item, number, get) {
  const issue = get(`issues/${number}`);
  const events = get(`issues/${number}/timeline`);
  const linked = events.find(
    (/** @type {any} */ event) =>
      event.event === "cross-referenced" &&
      event.source?.issue?.pull_request &&
      Number.isSafeInteger(event.source.issue.number) &&
      event.source.issue.number > 0 &&
      ["copilot-swe-agent", "github-actions[bot]"].includes(String(event.source.issue.user?.login || "").toLowerCase()) &&
      after(event.created_at, item.timestamp)
  );
  if (linked) {
    const pr = get(`pulls/${linked.source.issue.number}`);
    return pr.merged === true ? result("accepted", "strong", "merged") : pr.state === "closed" ? result("rejected", "strong", "closed_without_merge") : result("pending", "medium", "open");
  }
  return result("unknown", "none", "missing_execution_state", "no attributable agent PR");
}

module.exports = { evaluateAction, result };
