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

// The fail-once smoke Work fails its first attempt by design; only that one worker_failed Claim is healthy.
function plannedFailure(claim, works, claims) {
  if (claim.state !== "cancelled" || claim.cancellation_reason !== "worker_failed") return false;
  if (works.get(claim.work_id)?.payload?.smoke_mode !== "fail-once") return false;
  let failures = 0;
  for (const other of claims.values()) if (other.work_id === claim.work_id && other.state === "cancelled" && other.cancellation_reason === "worker_failed") failures++;
  return failures === 1;
}

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

  // Ledger segments, newest first: the activation snapshot, then each log a checkpoint folded.
  // A segment's `parent` caches its checkpoint's prior Git tip once read; a failed read caches
  // nothing, so it belongs only to the candidate whose walk needed it.
  const segments = snapshot ? [{ sha: snapshot.sha, projection: snapshot.projection, parent: null }] : [];

  // Original dispatch_next grants of the listed dispatcher attempts, merged across every walked
  // segment; each remembers the segment that holds it.
  const candidates = new Set(runs.map(run => runKey(run.id, run.run_attempt)));
  const grants = new Map();
  const indexGrants = segment => {
    for (const commit of segments[segment].projection.transactions) {
      const { actor, request } = commit;
      if (request.kind !== "dispatch_next" || request.parameters.pool !== POOL) continue;
      if (actor.role !== "dispatcher" || actor.repository !== repository || actor.workflow !== DISPATCHER_WORKFLOW) continue;
      const key = runKey(actor.run_id, actor.run_attempt);
      if (!candidates.has(key)) continue;
      const list = grants.get(key) ?? [];
      if (list.some(grant => grant.commit_id === commit.id)) continue;
      const claims = commit.operations.filter(operation => operation.kind === "Claim");
      grants.set(key, [...list, { commit_id: commit.id, request_id: request.id, segment, claims }]);
    }
  };
  if (snapshot) indexGrants(0);

  /** The prior Git tip of the checkpoint commit that starts a segment. */
  const parentOf = async segment => {
    if (typeof segment.sha !== "string" || !FULL_SHA.test(segment.sha)) throw evidenceFailure("HISTORY_UNANCHORED");
    const listed = (await github.rest.repos.listCommits({ owner, repo, sha: segment.sha, path: LEDGER_PATH, per_page: 1, page: segment.projection.transactions.length })).data;
    const parents = Array.isArray(listed) && listed.length === 1 ? listed[0].parents : null;
    if (!Array.isArray(parents) || parents.length !== 1 || typeof parents[0]?.sha !== "string" || !FULL_SHA.test(parents[0].sha)) throw evidenceFailure("HISTORY_UNANCHORED");
    return parents[0].sha;
  };

  // A checkpoint folds earlier grants into Claims that no longer name their actor; walk Git
  // history from the captured SHA, one archived segment at a time, to recover them. A listed
  // commit that is not the checkpoint's real parent fails the prior_tip anchor below.
  const grantsFor = async key => {
    for (let index = 0; !grants.has(key); index++) {
      const segment = segments[index];
      const first = segment.projection.transactions[0];
      if (!isCheckpoint(first)) return [];
      if (index + 1 < segments.length) continue;
      if (segments.length > MAX_ARCHIVED_SEGMENTS) throw evidenceFailure("HISTORY_EXHAUSTED");
      segment.parent ??= await parentOf(segment);
      const projection = await readStateAtSHA(segment.parent);
      if (projection.repository !== snapshot.projection.repository || projection.tip !== first.operations[0].prior_tip) throw evidenceFailure("HISTORY_UNANCHORED");
      segments.push({ sha: segment.parent, projection, parent: null });
      indexGrants(segments.length - 1);
    }
    return grants.get(key);
  };

  // A checkpoint prunes terminal Work with its Claims and Dispatches. The newest segment still
  // holding a granted Claim has its latest state; the grant's own segment always holds it.
  const recordsOf = (claimId, through) => {
    for (let index = 0; index <= through; index++) {
      const { claims, dispatches, works } = segments[index].projection;
      const claim = claims.get(claimId);
      if (claim) return { claim, dispatch: dispatches.get(claim.dispatch_id), pruned: index > 0, planned: plannedFailure(claim, works, claims) };
    }
    return { claim: undefined, dispatch: undefined, pruned: false, planned: false };
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

  // Claim states and bindings come from the newest ledger segment holding the granted Claim.
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
    const seen = new Set();
    const workers = new Map();
    for (const { grant, operation } of granted) {
      const { claim, dispatch, pruned, planned } = recordsOf(operation.claim_id, grant.segment);
      result.claims.push({
        claim_id_prefix: operation.claim_id.slice(0, 12),
        work_id_prefix: operation.work_id.slice(0, 12),
        state: claim?.state ?? null,
        dispatch_state: dispatch?.state ?? null,
        cancellation_reason: claim?.cancellation_reason ?? null,
        planned_failure: planned,
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
      // Only terminal Work is pruned, so a Claim absent from the snapshot must be settled.
      if (!consistent || (pruned && (claim.state === "open" || dispatch.state !== "closed"))) {
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
        return (claim.state === "completed" || claim.planned_failure) && worker?.status === "completed" && worker.conclusion === "success";
      });
  }
  return report;
}

module.exports = { collectSmokeCycle };
