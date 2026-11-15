// @ts-check
/// <reference types="@actions/github-script" />

const { AsyncLocalStorage } = require("node:async_hooks");
const { readInput } = require("./pending_writes_files.cjs");
const { readAuthorizedManifest, parsePendingJSON, inputDigest } = require("./pending_writes.cjs");
const { readDecisionInventory } = require("./write_gate_inventory.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { isTemporaryId } = require("./temporary_id.cjs");

const replayAuthorizations = new WeakSet();
const activeReplay = new AsyncLocalStorage();
const APPROVED_JOB_NAME = "Apply approved held safe outputs";

/**
 * Require an actual environment review for this replay job/run. Neither the
 * dispatch actor nor an authored approved flag is approval evidence.
 * @param {string} environment
 * @returns {Promise<{environment: string, environment_id: number, approvers: string[]}>}
 */
async function readReplayEnvironmentApproval(environment) {
  const runId = context.runId;
  const attempt = Number(process.env.GITHUB_RUN_ATTEMPT);
  const checkRunId = process.env.GH_AW_JOB_CHECK_RUN_ID;
  if (
    !environment ||
    /[\x00-\x1f\x7f]/.test(environment) ||
    environment.length > 255 ||
    process.env.GITHUB_JOB !== "apply_held_safe_outputs" ||
    !Number.isSafeInteger(runId) ||
    runId < 1 ||
    !Number.isSafeInteger(attempt) ||
    attempt < 1 ||
    !/^[1-9]\d*$/.test(checkRunId || "")
  ) {
    throw new Error(`${ERR_VALIDATION}: Held replay requires the reviewed approval-environment job`);
  }
  const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRunAttempt, { ...context.repo, run_id: runId, attempt_number: attempt, per_page: 100 });
  const matching = jobs.filter(job => job.name === APPROVED_JOB_NAME && typeof job.check_run_url === "string" && job.check_run_url.endsWith(`/check-runs/${checkRunId}`));
  if (matching.length !== 1 || !["in_progress", "completed"].includes(matching[0].status) || matching[0].steps?.filter(step => step.name === APPROVED_JOB_NAME).length !== 1) {
    throw new Error(`${ERR_VALIDATION}: Held replay job identity is unavailable or ambiguous`);
  }
  const { data: reviews } = await github.rest.actions.getReviewsForRun({ ...context.repo, run_id: runId });
  if (!Array.isArray(reviews)) throw new Error(`${ERR_VALIDATION}: Environment review evidence is unavailable`);
  const relevant = reviews.filter(review => Array.isArray(review.environments) && review.environments.some(entry => entry.name?.toLowerCase() === environment.toLowerCase()));
  if (relevant.length === 0 || relevant.some(review => review.state !== "approved")) throw new Error(`${ERR_VALIDATION}: Held replay has no unambiguous approved environment review`);
  const ids = new Set();
  const approvers = new Set();
  for (const review of relevant) {
    const user = review.user;
    if (!user || user.type !== "User" || !Number.isSafeInteger(user.id) || user.id < 1 || !/^[A-Za-z0-9_-]{1,100}$/.test(user.login)) throw new Error(`${ERR_VALIDATION}: Environment reviewer identity is unusable`);
    approvers.add(user.login);
    for (const entry of review.environments.filter(entry => entry.name?.toLowerCase() === environment.toLowerCase())) {
      if (!Number.isSafeInteger(entry.id) || entry.id === undefined || entry.id < 1) throw new Error(`${ERR_VALIDATION}: Environment identity is unusable`);
      ids.add(entry.id);
    }
  }
  if (ids.size !== 1) throw new Error(`${ERR_VALIDATION}: Environment review identities conflict`);
  return { environment, environment_id: [...ids][0], approvers: [...approvers].sort() };
}

/**
 * Only independently source-bound policy and digest-bound original state can
 * acquire this in-process capability. Deserializing a flag never grants it.
 *
 * @param {{policy: any, manifestPath: string, decisionsPath: string, snapshotPath: string, itemsPath: string}} options
 */
function readOriginalHeldReplayState({ policy, manifestPath, decisionsPath, snapshotPath, itemsPath }) {
  const { isSourceBoundReplayPolicy } = require("./apply_safe_outputs_replay.cjs");
  const record = policy?.recorded?.record;
  if (
    !isSourceBoundReplayPolicy(policy) ||
    !policy.gate ||
    record?.safe_outputs_result !== "success" ||
    record.verdict !== "partial" ||
    record.repository !== `${context.repo.owner}/${context.repo.repo}` ||
    ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"].some(key => typeof record[key] !== "string" || !/^[a-f0-9]{64}$/.test(record[key]))
  ) {
    throw new Error(`${ERR_VALIDATION}: Held replay requires original completed gate authorization and integrity evidence`);
  }
  const manifest = readAuthorizedManifest(manifestPath, record.pending_writes_sha256);
  const inventory = readDecisionInventory(decisionsPath, record.write_decisions_sha256, record.pending_writes_sha256);
  const snapshotBytes = readInput(snapshotPath);
  if (snapshotBytes.sha256 !== record.pending_write_inputs_sha256) throw new Error(`${ERR_VALIDATION}: Original replay snapshot digest mismatch`);
  const snapshot = parsePendingJSON(snapshotBytes.bytes.toString("utf8"));
  if (
    snapshot.schema !== "gh-aw/pending-write-inputs/v1" ||
    snapshot.manifest_sha256 !== record.pending_writes_sha256 ||
    inputDigest(snapshot.run) !== inputDigest(manifest.run) ||
    snapshot.run.repository !== record.repository ||
    snapshot.run.run_id !== record.run_id ||
    snapshot.run.run_attempt !== record.run_attempt ||
    snapshot.run.workflow_sha !== record.workflow_sha ||
    snapshot.run.workflow_ref !== record.workflow_ref
  ) {
    throw new Error(`${ERR_VALIDATION}: Original held replay state identity mismatch`);
  }
  const gateBinding = manifest.inputs.find(input => input.id === "gate_config" && input.kind === "value");
  if (!gateBinding || gateBinding.sha256 !== inputDigest(snapshot.gate_config) || typeof snapshot.gate_config?.approval_environment !== "string") {
    throw new Error(`${ERR_VALIDATION}: Original approval environment is not bound to the gate`);
  }
  for (const id of ["handler_config", "context", "environment"]) {
    const binding = manifest.inputs.find(input => input.id === id && input.kind === "value");
    if (!binding || binding.sha256 !== inputDigest(snapshot[id])) throw new Error(`${ERR_VALIDATION}: Original replay configuration or context is not bound to the gate`);
  }
  const itemBytes = readInput(itemsPath);
  if (itemBytes.sha256 !== record.safe_output_items_sha256) throw new Error(`${ERR_VALIDATION}: Original item outcome digest mismatch`);
  const outcomes = itemBytes.bytes
    .toString("utf8")
    .split("\n")
    .filter(Boolean)
    .map(line => parsePendingJSON(line));
  const heldIds = inventory.items
    .filter(item => item.gate_decision === "hold")
    .map(item => item.output_id)
    .filter(id => {
      const matching = outcomes.filter(row => row.output_id === id);
      return matching.length === 1 && matching[0].gate_decision === "hold" && matching[0].status === "skipped" && matching[0].skipped === true && matching[0].reason === "gate_held";
    });
  if (heldIds.length === 0) throw new Error(`${ERR_VALIDATION}: Original gate contains no held items`);
  for (const id of heldIds) {
    const planned = manifest.items.find(item => item.output_id === id);
    const decision = inventory.items.find(item => item.output_id === id);
    const outcome = outcomes.find(row => row.output_id === id);
    if (!planned || planned.complete !== true || planned.item_sha256 !== decision?.item_sha256 || planned.item_sha256 !== outcome?.item_sha256 || planned.input_sha256 !== decision?.input_sha256)
      throw new Error(`${ERR_VALIDATION}: Held item digest does not match the original gate`);
  }
  const confirmed = inventory.items
    .filter(item => item.gate_decision === "apply")
    .flatMap(item => {
      const matching = outcomes.filter(row => row.output_id === item.output_id);
      const row = matching[0];
      if (matching.length !== 1 || row.gate_decision !== "apply" || row.item_sha256 !== item.item_sha256 || row.status !== "success" || row.skipped || row.deferred || row.cancelled) return [];
      if (row.temporaryId && (!isTemporaryId(row.temporaryId) || !Number.isSafeInteger(row.number) || row.number < 1 || row.repo !== manifest.items.find(planned => planned.output_id === item.output_id)?.target.repository)) return [];
      return [Object.freeze({ output_id: item.output_id, ...(row.temporaryId ? { temporaryId: row.temporaryId, repo: row.repo, number: row.number } : {}) })];
    });
  return { record, manifest, inventory, snapshot, heldIds, confirmed };
}

/** @param {Parameters<typeof readOriginalHeldReplayState>[0]} options */
async function authorizeHeldReplay(options) {
  const { record, snapshot, heldIds, confirmed } = readOriginalHeldReplayState(options);
  const approval = await readReplayEnvironmentApproval(snapshot.gate_config.approval_environment);
  const authorization = Object.freeze({
    source_run_id: record.run_id,
    source_run_attempt: record.run_attempt,
    approval_run_id: context.runId,
    manifest_sha256: record.pending_writes_sha256,
    write_decisions_sha256: record.write_decisions_sha256,
    held_ids: Object.freeze(heldIds),
    approval_environment: approval.environment,
    approval_environment_id: approval.environment_id,
    approvers: Object.freeze(approval.approvers),
    confirmed_dependencies: Object.freeze(confirmed),
  });
  replayAuthorizations.add(authorization);
  return authorization;
}

/** @param {object} authorization @param {() => any} operation */
function withHeldReplayAuthorization(authorization, operation) {
  if (!replayAuthorizations.has(authorization)) throw new Error(`${ERR_VALIDATION}: Held replay approval capability is missing`);
  return activeReplay.run({ authorization, completed: new Set() }, operation);
}

function currentHeldReplayAuthorization() {
  return activeReplay.getStore()?.authorization;
}

/** Actual native handler/publication receipts only; approval is not completion. */
function recordHeldReplayCompletion(item, result) {
  const scope = activeReplay.getStore();
  if (!scope) return;
  const authorization = scope.authorization;
  if (!authorization.held_ids.includes(item.output_id) || authorization.manifest_sha256 !== process.env.GH_AW_PENDING_WRITES_SHA256 || authorization.write_decisions_sha256 !== process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256)
    throw new Error(`${ERR_VALIDATION}: Held completion has no matching replay capability`);
  if (
    result?.success === true &&
    !result.skipped &&
    !result.staged &&
    !result.deferred &&
    !result.deferred_manifest &&
    !result.cancelled &&
    !result.delegated &&
    !(item.type === "ledger_append" && result.queued) &&
    typeof result.action_name !== "string"
  )
    scope.completed.add(item.output_id);
}

function heldReplayDependencyCompleted(id) {
  const scope = activeReplay.getStore();
  return scope?.completed.has(id) === true || scope?.authorization.confirmed_dependencies.some(entry => entry.output_id === id) === true;
}

module.exports = { readReplayEnvironmentApproval, readOriginalHeldReplayState, authorizeHeldReplay, withHeldReplayAuthorization, currentHeldReplayAuthorization, recordHeldReplayCompletion, heldReplayDependencyCompleted };
