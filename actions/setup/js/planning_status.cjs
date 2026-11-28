// @ts-check

const { ERR_API, ERR_PERMISSION, ERR_VALIDATION } = require("./error_codes.cjs");
const PLANNING_STATUS_HEADERS = { "X-GitHub-Api-Version": "2026-03-10" };
const OPEN_CATEGORIES = new Set(["backlog", "planned", "started"]);
const CLOSED_CATEGORIES = new Set(["completed", "not_planned", "duplicate"]);

/** @param {unknown} value @returns {boolean} */
function isPlanningStatusSelector(value) {
  return (typeof value === "string" && value.trim().length > 0) || (Number.isSafeInteger(value) && Number(value) > 0);
}

/** @param {any} status @param {unknown} selector */
function matchesPlanningStatus(status, selector) {
  return typeof selector === "number" ? status.id === selector : typeof selector === "string" && status.name.trim().toLowerCase() === selector.trim().toLowerCase();
}

/** Resolve native statuses before any write: unsupported previews must never fall back to issue state. */
async function resolvePlanningStatus(client, owner, repo, selector, allowed = [], initial = false) {
  if (!isPlanningStatusSelector(selector)) throw new Error(`${ERR_VALIDATION}: Planning status must be a non-empty name or a positive integer ID`);
  if (!Array.isArray(allowed) || allowed.some(value => !isPlanningStatusSelector(value))) throw new Error(`${ERR_VALIDATION}: Allowed planning statuses must be names or positive integer IDs`);
  let data;
  try {
    ({ data } = await client.request("GET /repos/{owner}/{repo}/planning-statuses", { owner, repo, headers: PLANNING_STATUS_HEADERS }));
  } catch (error) {
    if (typeof error === "object" && error !== null && (error.status === 404 || error.response?.status === 404))
      throw new Error(`${ERR_API}: Native issue planning statuses are unavailable for this repository (public preview)`, { cause: error });
    throw error;
  }
  if (!Array.isArray(data)) throw new Error(`${ERR_API}: Invalid planning-statuses response; native issue statuses are unavailable`);
  const matches = data.filter(candidate => candidate && typeof candidate.name === "string" && matchesPlanningStatus(candidate, selector));
  if (matches.length > 1) throw new Error(`${ERR_VALIDATION}: Ambiguous planning status: ${JSON.stringify(selector)}; use a unique integer ID`);
  const status = matches[0];
  if (!status || !Number.isSafeInteger(status.id) || status.id <= 0) throw new Error(`${ERR_VALIDATION}: Unknown planning status: ${JSON.stringify(selector)}`);
  if (data.filter(candidate => candidate && typeof candidate.name === "string" && candidate.name.trim().toLowerCase() === status.name.trim().toLowerCase()).length !== 1) {
    throw new Error(`${ERR_VALIDATION}: Ambiguous planning status name: ${JSON.stringify(status.name)}; GitHub writes statuses by name`);
  }
  if (!OPEN_CATEGORIES.has(status.category) && !CLOSED_CATEGORIES.has(status.category)) throw new Error(`${ERR_API}: Unsupported planning status category: ${JSON.stringify(status.category)}`);
  if (allowed.length > 0 && !allowed.some(value => matchesPlanningStatus(status, value))) throw new Error(`${ERR_PERMISSION}: Planning status ${JSON.stringify(status.name)} is not in the allowed list`);
  if (initial && !OPEN_CATEGORIES.has(status.category)) throw new Error(`${ERR_VALIDATION}: Initial planning status must belong to an open category (Backlog, Todo, In progress, or In review)`);
  return { id: status.id, name: status.name, category: status.category };
}

/** @param {{category: string}} status */
function planningStatusState(status) {
  return { state: OPEN_CATEGORIES.has(status.category) ? "open" : "closed", state_reason: CLOSED_CATEGORIES.has(status.category) ? status.category : null };
}

/** @param {any} issue @param {any} status */
function verifyPlanningStatus(issue, status) {
  const expected = planningStatusState(status);
  if (
    !issue?.status ||
    issue.status.id !== status.id ||
    issue.status.name !== status.name ||
    issue.status.category !== status.category ||
    issue.state !== expected.state ||
    (expected.state === "closed" && issue.state_reason !== expected.state_reason)
  ) {
    throw new Error(`${ERR_API}: GitHub did not apply the requested native planning status; it may be unavailable or permission was denied`);
  }
}

/** Read the documented Issue.duplicateOf relationship; REST issue responses do not expose it. */
async function fetchIssueDuplicate(client, owner, repo, issueNumber) {
  const result = await client.graphql(
    `query($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        issue(number: $number) {
          number
          duplicateOf {
            fullDatabaseId
            number
            repository { nameWithOwner }
          }
        }
      }
    }`,
    { owner, repo, number: issueNumber, headers: PLANNING_STATUS_HEADERS }
  );
  const issue = result?.repository?.issue;
  if (!issue || issue.number !== issueNumber || !Object.hasOwn(issue, "duplicateOf")) throw new Error(`${ERR_API}: Cannot verify the native duplicate issue relationship`);
  if (issue.duplicateOf === null) return null;
  const duplicate = issue.duplicateOf;
  const id = typeof duplicate.fullDatabaseId === "string" && /^[1-9]\d*$/.test(duplicate.fullDatabaseId) ? Number(duplicate.fullDatabaseId) : duplicate.fullDatabaseId;
  if (!Number.isSafeInteger(id) || id <= 0 || !Number.isSafeInteger(duplicate.number) || duplicate.number <= 0 || typeof duplicate.repository?.nameWithOwner !== "string") {
    throw new Error(`${ERR_API}: Cannot verify the native duplicate issue identity`);
  }
  return { id, number: duplicate.number, repo: duplicate.repository.nameWithOwner };
}

/** @param {any} issue @param {{id: number}|null} [duplicate] */
function planningStatusSnapshot(issue, duplicate = null) {
  return {
    state: issue.state,
    state_reason: issue.state_reason ?? null,
    planning_status: issue.status ? { id: issue.status.id, name: issue.status.name, category: issue.status.category } : null,
    duplicate_issue_id: duplicate?.id ?? null,
  };
}

module.exports = { PLANNING_STATUS_HEADERS, isPlanningStatusSelector, resolvePlanningStatus, planningStatusState, verifyPlanningStatus, planningStatusSnapshot, fetchIssueDuplicate };
