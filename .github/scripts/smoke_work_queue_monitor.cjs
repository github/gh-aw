"use strict";

// Judges the latest settled smoke dispatcher execution by the Claims it granted.
// FIFO dispatch means a dispatcher can claim older Work than it submitted, so
// ownership comes from the dispatcher's own dispatch_next grants, and terminal
// state from each granted Claim (never from the Work, which a retry can reuse).

// Same revision as the queue logger, so both apply one request-ID redaction rule.
const { getGitHubRequestId, ownValue, validGitHubRequestId } = require("../../actions/setup/js/work_queue_logging.cjs");

const POOL = "smoke-work-queue";
const EXPECTED_CLAIMS = 3;
const DISPATCHER_WORKFLOW_ID = "smoke-work-queue-dispatcher.lock.yml";
const DISPATCHER_WORKFLOW = `.github/workflows/${DISPATCHER_WORKFLOW_ID}`;
const WORKER_WORKFLOW = ".github/workflows/smoke-work-queue-worker.lock.yml";
const ACTIVE_RUN_STATES = new Set(["queued", "in_progress", "waiting", "pending", "requested"]);
const UNBOUND_DISPATCH_STATES = new Set(["reserved", "started"]);
const MAX_CANDIDATES = 6;
const MAX_ARCHIVED_SEGMENTS = 6;
const LEDGER_PATH = "work-queue.jsonl";
const FULL_SHA = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;

// Only this module's own ledger inconsistencies carry the tag; a caught dependency
// error claiming `evidence: true` takes the redacted read-error path.
const EVIDENCE = Symbol("smoke-monitor-evidence");

/** A ledger inconsistency, reported without its details. */
function evidenceFailure(code) {
  return Object.assign(new Error(), { code, [EVIDENCE]: true });
}

/** Allowlisted summary of a failed read; messages, headers and bodies are withheld. */
function safeError(operation, error) {
  if (error instanceof Error && Object.hasOwn(error, EVIDENCE)) {
    return { operation, status: null, code: error.code, category: "evidence", short_message: "Ledger evidence is missing or inconsistent.", correlation_id: null };
  }
  const status = Number.isInteger(error?.status) ? error.status : Number.isInteger(error?.response?.status) ? error.response.status : null;
  const rawCode = error?.code;
  const code = typeof rawCode === "string" && /^[A-Z0-9_]{1,32}$/.test(rawCode) ? rawCode : null;
  const correlation_id = getGitHubRequestId(error) ?? validGitHubRequestId(ownValue(ownValue(error, "request"), "requestId")) ?? null;
  return {
    operation,
    status,
    code,
    category: status == null ? (code ? "transport" : "unavailable") : `http_${Math.floor(status / 100)}xx`,
    short_message: "Read failed; response details withheld.",
    correlation_id,
  };
}

function isCheckpoint(commit) {
  return commit?.operations?.[0]?.kind === "Checkpoint";
}

const runKey = (runId, attempt) => `${runId}/${attempt}`;

/**
 * @param {{github: any, context: any, core: any, loadSnapshot: () => {sha: string | null, projection: any}, readStateAtSHA: (sha: string) => Promise<any>}} options
 */
async function collectSmokeCycle({ github, context, core, loadSnapshot, readStateAtSHA }) {
  const { owner, repo } = context.repo;
  const repository = `${owner}/${repo}`;
  const serverUrl = context.serverUrl || "https://github.com";
  const report = { dispatcher_runs: [], dispatcher: null, queue: { available: false, claims: [] }, workers: [], errors: [], healthy: false };
  const recordError = (errors, operation, error) => {
    const summary = safeError(operation, error);
    errors.push(summary);
    core.warning(`Smoke monitor ${operation} failed (${summary.category}${summary.status == null ? "" : ` ${summary.status}`})`);
  };

  let snapshot = null;
  try {
    const loaded = loadSnapshot();
    snapshot = { sha: loaded.sha, projection: loaded.projection };
    report.queue.available = true;
  } catch (error) {
    recordError(report.errors, "work_queue_snapshot", error);
  }

  let runs = [];
  try {
    runs = (await github.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: DISPATCHER_WORKFLOW_ID, per_page: MAX_CANDIDATES })).data.workflow_runs.slice(0, MAX_CANDIDATES);
    report.dispatcher_runs = runs.map(({ id, run_attempt, html_url, status, conclusion }) => ({ id, run_attempt, url: html_url, status, conclusion }));
  } catch (error) {
    recordError(report.errors, "list_dispatcher_runs", error);
  }

  // Original dispatch_next grants of the listed dispatcher attempts, from the newest segment holding them.
  const candidates = new Set(runs.map(run => runKey(run.id, run.run_attempt)));
  const grants = new Map();
  const indexGrants = projection => {
    const found = new Map();
    for (const commit of projection.transactions) {
      const { actor, request } = commit;
      if (request.kind !== "dispatch_next" || request.parameters.pool !== POOL) continue;
      if (actor.role !== "dispatcher" || actor.repository !== repository || actor.workflow !== DISPATCHER_WORKFLOW) continue;
      const key = runKey(actor.run_id, actor.run_attempt);
      if (!candidates.has(key) || grants.has(key)) continue;
      const claims = commit.operations.filter(operation => operation.kind === "Claim");
      found.set(key, [...(found.get(key) ?? []), { commit_id: commit.id, request_id: request.id, claims }]);
    }
    for (const [key, list] of found) grants.set(key, list);
  };
  if (snapshot) indexGrants(snapshot.projection);

  // A checkpoint folds earlier grants into Claims that no longer name their actor; walk
  // Git history from the captured SHA, one archived segment at a time, to recover them.
  const history = { segment: snapshot, archived: 0, error: null };
  const grantsFor = async key => {
    while (!grants.has(key)) {
      if (history.error) throw history.error;
      const tail = history.segment;
      const first = tail.projection.transactions[0];
      if (!isCheckpoint(first)) return [];
      try {
        if (history.archived >= MAX_ARCHIVED_SEGMENTS) throw evidenceFailure("HISTORY_EXHAUSTED");
        history.archived += 1;
        if (typeof tail.sha !== "string" || !FULL_SHA.test(tail.sha)) throw evidenceFailure("HISTORY_UNANCHORED");
        const listed = (await github.rest.repos.listCommits({ owner, repo, sha: tail.sha, path: LEDGER_PATH, per_page: 1, page: tail.projection.transactions.length })).data;
        const parents = Array.isArray(listed) && listed.length === 1 ? listed[0].parents : null;
        if (!Array.isArray(parents) || parents.length !== 1 || typeof parents[0]?.sha !== "string" || !FULL_SHA.test(parents[0].sha)) throw evidenceFailure("HISTORY_UNANCHORED");
        const projection = await readStateAtSHA(parents[0].sha);
        if (projection.repository !== snapshot.projection.repository || projection.tip !== first.operations[0].prior_tip) throw evidenceFailure("HISTORY_UNANCHORED");
        history.segment = { sha: parents[0].sha, projection };
        indexGrants(projection);
      } catch (error) {
        history.error = error;
        throw error;
      }
    }
    return grants.get(key);
  };

  const attempts = new Map();
  const readAttempt = async ({ run_id, run_attempt }, errors) => {
    const key = runKey(run_id, run_attempt);
    if (!attempts.has(key)) {
      let entry;
      try {
        const { data } = await github.request("GET /repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}", { owner, repo, run_id: Number(run_id), attempt_number: run_attempt });
        if (String(data?.id) !== run_id || data.run_attempt !== run_attempt) throw evidenceFailure("RUN_ATTEMPT_MISMATCH");
        entry = { worker: { id: run_id, run_attempt, url: data.html_url, status: data.status, conclusion: data.conclusion }, error: null };
      } catch (error) {
        entry = { worker: { id: run_id, run_attempt, url: `${serverUrl}/${repository}/actions/runs/${run_id}/attempts/${run_attempt}`, status: "unavailable", conclusion: null }, error };
      }
      attempts.set(key, entry);
    }
    const entry = attempts.get(key);
    if (entry.error) recordError(errors, "read_bound_worker_run", entry.error);
    return entry.worker;
  };

  // Claim states and bindings always come from the activation projection.
  const evaluate = async run => {
    const result = { claims: [], workers: [], errors: [], grantCount: 0, distinct: 0, inFlight: false };
    if (!snapshot) return result;
    let granted;
    try {
      granted = (await grantsFor(runKey(run.id, run.run_attempt))).flatMap(grant => grant.claims.map(operation => ({ grant, operation })));
    } catch (error) {
      recordError(result.errors, "read_claim_history", error);
      return result;
    }
    result.grantCount = granted.length;
    const { claims, dispatches } = snapshot.projection;
    const seen = new Set();
    const workers = new Map();
    for (const { grant, operation } of granted) {
      const claim = claims.get(operation.claim_id);
      const dispatch = claim ? dispatches.get(claim.dispatch_id) : undefined;
      result.claims.push({
        claim_id_prefix: operation.claim_id.slice(0, 12),
        work_id_prefix: operation.work_id.slice(0, 12),
        state: claim?.state ?? null,
        dispatch_state: dispatch?.state ?? null,
        cancellation_reason: claim?.cancellation_reason ?? null,
        run_id: dispatch?.run?.run_id ?? null,
        run_attempt: dispatch?.run?.run_attempt ?? null,
      });
      if (seen.has(operation.claim_id)) {
        recordError(result.errors, "match_claim_records", evidenceFailure("CLAIM_DUPLICATE"));
        continue;
      }
      seen.add(operation.claim_id);
      if (!claim || !dispatch) {
        recordError(result.errors, "match_claim_records", evidenceFailure("CLAIM_RECORD_MISSING"));
        continue;
      }
      const consistent =
        claim.work_id === operation.work_id &&
        claim.dispatch_id === operation.dispatch_id &&
        claim.commit_id === grant.commit_id &&
        claim.request_id === grant.request_id &&
        dispatch.dispatch_id === operation.dispatch_id &&
        dispatch.commit_id === grant.commit_id &&
        dispatch.claims.includes(operation.claim_id) &&
        dispatch.pool === POOL &&
        dispatch.workflow === WORKER_WORKFLOW;
      if (!consistent) {
        recordError(result.errors, "match_claim_records", evidenceFailure("CLAIM_RECORD_CONFLICT"));
        continue;
      }
      if (dispatch.run) {
        const worker = await readAttempt(dispatch.run, result.errors);
        workers.set(runKey(worker.id, worker.run_attempt), worker);
        if (ACTIVE_RUN_STATES.has(worker.status)) result.inFlight = true;
      } else if (claim.state === "open" && UNBOUND_DISPATCH_STATES.has(dispatch.state)) {
        result.inFlight = true;
      }
    }
    result.distinct = seen.size;
    result.workers = [...workers.values()];
    return result;
  };

  // Newest first; a successful dispatcher whose sound evidence shows work still running yields to an older one.
  let selected = null;
  let evidence = null;
  for (const run of runs) {
    if (run.status !== "completed") continue;
    const result = await evaluate(run);
    const sound = snapshot && result.errors.length === 0 && result.grantCount === EXPECTED_CLAIMS;
    if (run.conclusion === "success" && sound && result.inFlight) continue;
    selected = run;
    evidence = result;
    break;
  }

  if (selected) {
    report.dispatcher = { id: selected.id, run_attempt: selected.run_attempt, url: selected.html_url, conclusion: selected.conclusion };
    report.queue.claims = evidence.claims;
    report.workers = evidence.workers;
    report.errors.push(...evidence.errors);
    const workerOf = new Map(evidence.workers.map(worker => [runKey(worker.id, worker.run_attempt), worker]));
    const runIds = new Set(evidence.claims.map(claim => claim.run_id).filter(Boolean));
    report.healthy =
      selected.conclusion === "success" &&
      report.queue.available &&
      report.errors.length === 0 &&
      evidence.grantCount === EXPECTED_CLAIMS &&
      evidence.distinct === EXPECTED_CLAIMS &&
      runIds.size === EXPECTED_CLAIMS &&
      evidence.claims.every(claim => {
        const worker = claim.run_id ? workerOf.get(runKey(claim.run_id, claim.run_attempt)) : undefined;
        return claim.state === "completed" && worker?.status === "completed" && worker.conclusion === "success";
      });
  }
  return report;
}

module.exports = { collectSmokeCycle };
