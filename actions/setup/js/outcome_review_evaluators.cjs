// @ts-check
const crypto = require("crypto");
const { secondsBetween, itemNumber, itemRepo } = require("./outcome_evidence.cjs");

/** @typedef {import("./outcome_types").OutcomeResult} EvalResult */

/** @param {EvalResult} out @param {string} timestamp */
function setPendingAge(out, timestamp) {
  out.pending_age_sec = secondsBetween(timestamp, new Date().toISOString());
}

/**
 * @param {any} item
 * @returns {number | null}
 */
function getItemNumber(item) {
  return itemNumber(item) || null;
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @returns {string}
 */
function getItemRepo(item, defaultRepo) {
  return itemRepo(item, defaultRepo);
}

/**
 * @param {any} item
 * @param {string} key
 * @returns {string[]}
 */
function getMetadataStringArray(item, key) {
  const raw = item?.metadata?.[key];
  if (!Array.isArray(raw)) return [];
  return raw.map(value => String(value || "").trim()).filter(Boolean);
}

/**
 * @param {any} item
 * @param {string} key
 * @returns {number | null}
 */
function getMetadataNumber(item, key) {
  const raw = item?.metadata?.[key];
  if (typeof raw === "number" && Number.isSafeInteger(raw) && raw > 0) return raw;
  if (typeof raw === "string" && /^\d+$/.test(raw.trim())) {
    const parsed = Number(raw);
    return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
  }
  return null;
}

/**
 * @param {string} key
 * @param {any} value
 * @returns {any}
 */
function normalizeStateValue(key, value) {
  if (key === "labels" || key === "assignees") {
    if (!Array.isArray(value)) return [];
    return value
      .map(entry =>
        key === "labels"
          ? String(entry || "")
              .trim()
              .toLowerCase()
          : String(entry || "").trim()
      )
      .filter(Boolean)
      .sort();
  }
  if (key === "draft") {
    return value === true;
  }
  if (typeof value === "string") {
    return value.trim();
  }
  return value ?? "";
}

function hashOutcomeBody(body) {
  const normalized = String(body || "")
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map(line => line.replace(/[ \t]+$/g, ""))
    .join("\n")
    .trim();
  return crypto.createHash("sha256").update(normalized, "utf8").digest("hex");
}

/**
 * @param {string} key
 * @param {any} left
 * @param {any} right
 * @returns {boolean}
 */
function stateValuesEqual(key, left, right) {
  const normalizedLeft = normalizeStateValue(key, left);
  const normalizedRight = normalizeStateValue(key, right);
  return JSON.stringify(normalizedLeft) === JSON.stringify(normalizedRight);
}

/**
 * @param {Record<string, any> | null | undefined} beforeState
 * @param {Record<string, any> | null | undefined} afterState
 * @param {Record<string, any>} currentState
 * @param {string[]} fields
 * @returns {{changed: string[], retained: string[], reverted: string[], replaced: string[]}}
 */
function compareRetainedState(beforeState, afterState, currentState, fields) {
  const changed = [];
  const retained = [];
  const reverted = [];
  const replaced = [];

  for (const field of fields) {
    if (!afterState || !(field in afterState)) continue;
    const beforeValue = beforeState ? beforeState[field] : undefined;
    const afterValue = afterState[field];
    if (stateValuesEqual(field, beforeValue, afterValue)) continue;
    changed.push(field);
    const currentValue = currentState[field];
    if (stateValuesEqual(field, currentValue, afterValue)) {
      retained.push(field);
      continue;
    }
    if (stateValuesEqual(field, currentValue, beforeValue)) {
      reverted.push(field);
      continue;
    }
    replaced.push(field);
  }

  return { changed, retained, reverted, replaced };
}

function extractIssueUpdateState(issue) {
  return {
    title: typeof issue?.title === "string" ? issue.title : "",
    body_hash: hashOutcomeBody(issue?.body),
    state: typeof issue?.state === "string" ? issue.state : "",
    labels: Array.isArray(issue?.labels)
      ? issue.labels
          .map(label => {
            if (typeof label === "string") return label;
            if (label && typeof label.name === "string") return label.name;
            return "";
          })
          .filter(Boolean)
      : [],
    assignees: Array.isArray(issue?.assignees)
      ? issue.assignees
          .map(assignee => {
            if (typeof assignee === "string") return assignee;
            if (assignee && typeof assignee.login === "string") return assignee.login;
            return "";
          })
          .filter(Boolean)
      : [],
  };
}

function extractPullRequestUpdateState(pullRequest) {
  return {
    title: typeof pullRequest?.title === "string" ? pullRequest.title : "",
    body_hash: hashOutcomeBody(pullRequest?.body),
    state: typeof pullRequest?.state === "string" ? pullRequest.state : "",
    base: typeof pullRequest?.base?.ref === "string" ? pullRequest.base.ref : "",
    draft: pullRequest?.draft === true,
    head_sha: typeof pullRequest?.head?.sha === "string" ? pullRequest.head.sha : "",
  };
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @param {(endpoint: string) => any} api
 * @param {{fields: string[], loadCurrent: (repo: string, number: number) => { currentState: Record<string, any>, merged?: boolean } | null}} options
 * @returns {EvalResult}
 */
function evaluateRetainedUpdate(item, defaultRepo, api, options) {
  const repo = getItemRepo(item, defaultRepo);
  const number = getItemNumber(item);
  /** @type {EvalResult} */
  const out = {
    result: "unknown",
    outcome_status: "unknown",
    evidence_strength: "none",
    signal: "missing_execution_state",
    detail: "",
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
    zero_touch: false,
  };

  if (!repo || !number) {
    out.detail = "missing execution state";
    return out;
  }

  if (!item.before_state || !item.after_state) {
    out.detail = "missing execution state";
    return out;
  }

  const loaded = options.loadCurrent(repo, number);
  if (!loaded || !loaded.currentState) {
    out.signal = "unknown";
    out.detail = "api error";
    out.evidence_strength = "none";
    return out;
  }

  const comparison = compareRetainedState(item.before_state, item.after_state, loaded.currentState, options.fields);
  if (comparison.changed.length === 0) {
    out.detail = "no persisted state delta";
    out.signal = "no_state_delta";
    return out;
  }

  if (comparison.retained.length === comparison.changed.length) {
    out.result = "accepted";
    if (loaded.merged) {
      out.outcome_status = "accepted";
      out.evidence_strength = "strong";
      out.signal = "state_retained_and_merged";
      out.detail = "update retained and merged";
      return out;
    }
    out.outcome_status = "accepted";
    out.evidence_strength = "medium";
    out.signal = "state_retained";
    out.detail = "update retained";
    return out;
  }

  out.result = "rejected";
  out.outcome_status = "rejected";
  out.evidence_strength = "strong";
  if (comparison.reverted.length === comparison.changed.length) {
    out.signal = "state_reverted";
    out.detail = "update reverted";
    return out;
  }
  out.signal = "state_replaced";
  out.detail = "update replaced";
  return out;
}

/**
 * @param {string | undefined} timestamp
 * @param {string | undefined} threshold
 * @returns {boolean}
 */
function isOnOrAfter(timestamp, threshold) {
  if (!timestamp) return false;
  if (!threshold) return true;
  const a = Date.parse(timestamp);
  const b = Date.parse(threshold);
  if (!Number.isFinite(a)) {
    return false;
  }
  if (!Number.isFinite(b)) {
    return false;
  }
  return a >= b;
}

/**
 * @param {any} review
 * @returns {boolean}
 */
function isSubmittedReview(review) {
  const state = String(review?.state || "").toUpperCase();
  return Boolean(review?.submitted_at) && state !== "" && state !== "PENDING";
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @param {(endpoint: string) => any} api
 * @returns {EvalResult}
 */
function evaluateAddReviewer(item, defaultRepo, api) {
  const repo = getItemRepo(item, defaultRepo);
  const number = getItemNumber(item);
  const timestamp = item.timestamp || "";
  /** @type {EvalResult} */
  const out = {
    result: "unknown",
    outcome_status: "unknown",
    evidence_strength: "weak",
    signal: "unknown",
    detail: "",
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
    zero_touch: false,
  };

  if (!repo || !number) {
    out.detail = "missing review request reference";
    return out;
  }

  const requestedReviewers = new Set(getMetadataStringArray(item, "requested_reviewers").map(login => login.toLowerCase()));
  const requestedTeams = new Set(getMetadataStringArray(item, "requested_team_reviewers").map(team => team.toLowerCase()));
  const reviews = api(`repos/${repo}/pulls/${number}/reviews`);
  const requested = api(`repos/${repo}/pulls/${number}/requested_reviewers`);

  if (!Array.isArray(reviews) || !requested) {
    out.detail = "api error";
    setPendingAge(out, timestamp);
    return out;
  }

  const latestReviewByRequestedReviewer = new Map();
  for (const review of reviews) {
    const state = String(review?.state || "").toUpperCase();
    const submittedAt = review?.submitted_at;
    if (!state || state === "PENDING" || !submittedAt || !isOnOrAfter(submittedAt, timestamp)) continue;
    const login = String(review?.user?.login || "").toLowerCase();
    if (!requestedReviewers.has(login)) continue;
    const previous = latestReviewByRequestedReviewer.get(login);
    if (!previous || isOnOrAfter(submittedAt, previous?.submitted_at)) {
      latestReviewByRequestedReviewer.set(login, review);
    }
  }
  const relevantReviews = Array.from(latestReviewByRequestedReviewer.values());

  if (relevantReviews.some(review => String(review?.state || "").toUpperCase() === "APPROVED")) {
    out.result = "accepted";
    out.detail = "review approved";
    return out;
  }

  if (relevantReviews.length > 0) {
    out.result = "accepted";
    out.detail = "review submitted";
    return out;
  }

  // A submitted review does not establish membership in the requested team.
  const anyReviewAfterRequest = reviews.some(review => isSubmittedReview(review) && isOnOrAfter(review?.submitted_at, timestamp));
  if (requestedTeams.size > 0 && anyReviewAfterRequest) {
    out.result = "unknown";
    out.detail = "team reviewer membership is unverified";
    return out;
  }

  const pendingUsers = new Set((requested.users || []).map(user => String(user?.login || "").toLowerCase()));
  const pendingTeams = new Set((requested.teams || []).map(team => String(team?.slug || team?.name || "").toLowerCase()));
  const stillPending = Array.from(requestedReviewers).some(login => pendingUsers.has(login)) || Array.from(requestedTeams).some(team => pendingTeams.has(team));

  if (stillPending) {
    out.result = "pending";
    out.detail = "awaiting review";
    setPendingAge(out, timestamp);
    return out;
  }

  if (requestedReviewers.size > 0 || requestedTeams.size > 0) {
    out.result = "rejected";
    out.detail = "review request removed";
    return out;
  }

  out.detail = "unknown review request state";
  return out;
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @param {(endpoint: string) => any} api
 * @returns {EvalResult}
 */
function evaluateUpdateIssue(item, defaultRepo, api) {
  return evaluateRetainedUpdate(item, defaultRepo, api, {
    fields: ["title", "body_hash", "state", "labels", "assignees"],
    loadCurrent: (repo, number) => {
      const issue = api(`repos/${repo}/issues/${number}`);
      if (!issue) return null;
      return { currentState: extractIssueUpdateState(issue) };
    },
  });
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @param {(endpoint: string) => any} api
 * @returns {EvalResult}
 */
function evaluateUpdatePullRequest(item, defaultRepo, api) {
  return evaluateRetainedUpdate(item, defaultRepo, api, {
    fields: ["title", "body_hash", "state", "base", "draft", "head_sha"],
    loadCurrent: (repo, number) => {
      const pullRequest = api(`repos/${repo}/pulls/${number}`);
      if (!pullRequest) return null;
      return {
        currentState: extractPullRequestUpdateState(pullRequest),
        merged: pullRequest.merged === true,
      };
    },
  });
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @param {(endpoint: string) => any} api
 * @returns {EvalResult}
 */
function evaluateSubmitPullRequestReview(item, defaultRepo, api) {
  const repo = getItemRepo(item, defaultRepo);
  const number = getItemNumber(item);
  const timestamp = item.timestamp || "";
  /** @type {EvalResult} */
  const out = {
    result: "unknown",
    outcome_status: "unknown",
    evidence_strength: "weak",
    signal: "unknown",
    detail: "",
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
    zero_touch: false,
  };

  if (!repo || !number) {
    out.detail = "missing review reference";
    return out;
  }

  const reviewId = getMetadataNumber(item, "review_id");
  const pr = api(`repos/${repo}/pulls/${number}`);
  const reviews = api(`repos/${repo}/pulls/${number}/reviews`);

  if (!pr || !Array.isArray(reviews) || !pr.state) {
    out.detail = "api error";
    setPendingAge(out, timestamp);
    return out;
  }

  const submittedReviews = reviews.filter(candidate => isSubmittedReview(candidate));
  const review = submittedReviews.find(candidate => Number(candidate?.id) === reviewId);

  if (!review) {
    out.detail = "review not found";
    return out;
  }

  const reviewState = String(review?.state || item?.metadata?.review_state || "").toUpperCase();
  const reviewSubmittedAt = review?.submitted_at || timestamp;
  const latestReview = submittedReviews.sort((a, b) => Date.parse(a.submitted_at) - Date.parse(b.submitted_at)).slice(-1)[0];

  out.review_comments = typeof pr.review_comments === "number" ? pr.review_comments : null;
  out.changed_files = typeof pr.changed_files === "number" ? pr.changed_files : null;
  out.additions = typeof pr.additions === "number" ? pr.additions : null;
  out.deletions = typeof pr.deletions === "number" ? pr.deletions : null;
  out.comments = typeof pr.comments === "number" ? pr.comments : null;

  if (reviewState === "DISMISSED") {
    out.result = "rejected";
    out.detail = "review dismissed";
    return out;
  }

  if (pr.merged === true) {
    if (reviewState === "APPROVED") {
      out.result = "accepted";
      out.detail = "review approved";
      if (pr.created_at && pr.merged_at) {
        out.resolution_sec = secondsBetween(pr.created_at, pr.merged_at);
      }
      return out;
    }

    if (reviewState === "CHANGES_REQUESTED") {
      const commits = api(`repos/${repo}/pulls/${number}/commits`);
      const hasPushAfterReview = Array.isArray(commits) ? commits.some(commit => isOnOrAfter(commit?.commit?.committer?.date || commit?.commit?.author?.date, reviewSubmittedAt)) : false;
      if (hasPushAfterReview) {
        out.result = "accepted";
        out.detail = "changes requested addressed and merged";
        if (pr.created_at && pr.merged_at) {
          out.resolution_sec = secondsBetween(pr.created_at, pr.merged_at);
        }
        return out;
      }
    }
  }

  if (pr.state === "closed" && pr.merged !== true) {
    out.result = "rejected";
    out.detail = "closed without merge after review";
    if (pr.created_at && pr.closed_at) {
      out.resolution_sec = secondsBetween(pr.created_at, pr.closed_at);
    }
    return out;
  }

  if (pr.state === "open" && latestReview && Number(latestReview.id) === Number(review.id)) {
    out.result = "pending";
    out.detail = "latest review awaiting outcome";
    setPendingAge(out, timestamp);
    return out;
  }

  out.detail = "unknown review lifecycle";
  return out;
}

module.exports = { evaluateAddReviewer, evaluateUpdateIssue, evaluateUpdatePullRequest, evaluateSubmitPullRequestReview, getMetadataNumber };
