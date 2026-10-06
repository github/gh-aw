// @ts-check
"use strict";

const fs = require("fs");
const queue = require("./work_queue_replay.cjs");
const store = require("./work_queue_store.cjs");
const { closed, digest, parseStrictJSON } = require("./work_queue_codec.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { DEFAULT_FINISH_INTENT_PATH, loadWorkQueueSnapshot } = require("./work_queue_mcp_server.cjs");
const { MAX_INTENTS, MAX_INTENT_BYTES, requestForIntent } = require("./work_queue_intents.cjs");
const { bindWorkerAssignment, loadQueue, publishOperations, validateStoredAssignment, expectedWorkerRun } = require("./work_queue_binding.cjs");
const { authenticatePublisher, fetchNativeRunAttempt, validateNativeRun } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { claimControlReceipts } = require("./work_queue_control_receipts.cjs");
const { validateDeliveryContract } = require("./work_queue_delivery.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";
const FINISH_INTENT_PATH = DEFAULT_FINISH_INTENT_PATH;
const SAFE_OUTPUTS_PATH = "/tmp/gh-aw/safeoutputs.jsonl";

function readWorkerSnapshot(snapshotPath = process.env.GH_AW_WORK_QUEUE_SNAPSHOT || SNAPSHOT_PATH) {
  return loadWorkQueueSnapshot(snapshotPath).worker;
}

function readFinishIntent(filename = process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || FINISH_INTENT_PATH, assignment) {
  const intents = new Map();
  const errors = [];
  if (!fs.existsSync(filename)) return { intents, errors };
  const stat = fs.statSync(filename);
  if (!stat.isFile() || stat.size > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  const lines = fs
    .readFileSync(filename, "utf8")
    .split("\n")
    .filter(line => line.trim());
  if (lines.length > MAX_INTENTS) throw new Error("work_queue_intent_limit");
  for (const [index, line] of lines.entries()) {
    let handle = null;
    try {
      const intent = parseStrictJSON(line);
      closed(intent, ["version", "intent_id", "kind", "parameters"], ["claim_handle"], "finish intent");
      if (intent.version !== 3 || intent.kind !== "finish" || typeof intent.intent_id !== "string" || !intent.intent_id) throw new Error("work_queue_finish_invalid");
      closed(intent.parameters, ["outcome"], [], "finish parameters");
      if (!["completed", "cancelled"].includes(intent.parameters.outcome)) throw new Error("work_queue_finish_invalid");
      const scoped = normalizeClaimScope({ ...intent.parameters, ...(Object.hasOwn(intent, "claim_handle") ? { claim_handle: intent.claim_handle } : {}) }, assignment);
      handle = scoped.claim_handle;
      const previous = intents.get(handle);
      if (previous && previous.outcome !== scoped.outcome) throw new Error("work_queue_finish_conflict");
      intents.set(handle, { intent_id: intent.intent_id, outcome: scoped.outcome });
    } catch {
      errors.push({ line: index + 1, claim_handle: handle, code: handle ? "work_queue_finish_conflict" : "work_queue_finish_scope_invalid" });
    }
  }
  for (const error of errors) if (error.claim_handle) intents.delete(error.claim_handle);
  return { intents, errors };
}

function runtimeOptions(options) {
  return { ...options, githubClient: options.githubClient || options.github || global.github, context: options.context || global.context, core: options.core || global.core };
}

async function authorizeWorkerClaim(options = {}) {
  const configured = runtimeOptions(options);
  const assignment = normalizeAssignment(options.assignment);
  const normalized = normalizeClaimScope(Object.hasOwn(options, "message") ? options.message : Object.hasOwn(options, "claim_handle") ? { claim_handle: options.claim_handle } : {}, assignment);
  if (Object.hasOwn(options, "claim_handle") && normalized.claim_handle !== options.claim_handle) throw new Error("work_queue_claim_scope_invalid");
  const member = assignment.claims.find(claim => claim.handle === normalized.claim_handle);
  const latest = await loadQueue(configured);
  const { dispatch, profile } = validateStoredAssignment(latest.projection, assignment);
  const trustedContext = await authenticatePublisher({ ...configured, role: "worker", dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
  const native = validateNativeRun(trustedContext.native_run, { ...expectedWorkerRun(assignment, profile, configured.context), run_id: trustedContext.run_id });
  if (!dispatch.run || dispatch.run.run_id !== native.run_id || dispatch.run.run_attempt !== 1) throw new Error("work_queue_binding_not_durable");
  const targets = [normalized.repository, normalized.repo, normalized.target_repo, normalized["target-repo"]].filter(value => value !== undefined);
  if (targets.some(target => typeof target !== "string" || target !== profile.effect_scope)) throw new Error("work_queue_effect_scope_denied");
  const claim = latest.projection.claims.get(member.claim_id);
  const work = latest.projection.works.get(member.work_id);
  const base = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, run_id: native.run_id, run_attempt: 1, effect_scope: profile.effect_scope };
  const resource = options.resource || { repository: targets[0] || profile.effect_scope };
  if (resource.repository !== profile.effect_scope) throw new Error("work_queue_effect_scope_denied");
  if (claim?.state === "cancelled") return { ...base, authorized: false, suppressed: true, state: "cancelled" };
  if (options.requireCompletion !== false && claim?.state !== "completed") return { ...base, authorized: false, state: "open" };
  if (options.requireCompletion !== false && work.barrier !== "pending") {
    if (work.state !== "completed" || work.claim_id !== member.claim_id) throw new Error("work_queue_claim_ownership_invalid");
    return { ...base, authorized: false, suppressed: work.barrier === "verified", state: work.barrier === "verified" ? "result" : "delivery_failed" };
  }
  queue.validateClaimAuthority(latest.projection, member.claim_id, trustedContext, {
    requireCompletion: options.requireCompletion !== false,
    resource,
  });
  return { ...base, authorized: true, state: options.requireCompletion === false ? "open" : "completed" };
}

async function reconcileWorkerClaim(options = {}) {
  const configured = runtimeOptions(options);
  const supplied = options.assignment ?? (options.worker === undefined ? readWorkerSnapshot(options.snapshotPath) : options.worker);
  if (!supplied) return { version: 3, status: options.requireAssignment ? "missing" : "unassigned", claims: {}, errors: [] };
  const assignment = normalizeAssignment(supplied);
  const staged = readFinishIntent(options.finishIntentPath, assignment);
  const states = Object.create(null);
  if (isStagedMode(options) || isStagedMode(options.config)) {
    const latest = await loadQueue(configured);
    const { dispatch } = validateStoredAssignment(latest.projection, assignment);
    for (const member of assignment.claims) states[member.handle] = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, state: "staged_preview", authorized: false };
    return { version: 3, dispatch_id: assignment.dispatch_id, ...(dispatch.run ? { run_id: dispatch.run.run_id, run_attempt: 1 } : {}), status: "staged_preview", claims: states, errors: staged.errors };
  }
  const admitted = await bindWorkerAssignment({ ...configured, assignment });
  const blocked = new Set(staged.errors.map(error => error.claim_handle).filter(Boolean));
  for (const member of assignment.claims) {
    try {
      if (blocked.has(member.handle)) throw new Error("work_queue_finish_conflict");
      let latest = await loadQueue(configured);
      const claim = latest.projection.claims.get(member.claim_id);
      const work = latest.projection.works.get(member.work_id);
      const intent = staged.intents.get(member.handle);
      if (claim?.state === "completed") {
        if (intent?.outcome === "cancelled") throw new Error("work_queue_finish_conflict");
        states[member.handle] = {
          claim_handle: member.handle,
          claim_id: member.claim_id,
          work_id: member.work_id,
          state: work.barrier === "verified" ? "result" : work.barrier === "failed" ? "delivery_failed" : "completed",
          authorized: work.barrier === "pending",
        };
        continue;
      }
      if (claim?.state === "cancelled") {
        if (intent?.outcome === "completed") throw new Error("work_queue_finish_conflict");
        states[member.handle] = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, state: "cancelled", authorized: false };
        continue;
      }
      const trustedContext = { ...admitted.trustedContext, claim_handle: member.handle };
      queue.validateClaimAuthority(latest.projection, member.claim_id, trustedContext, { requireCompletion: false });
      const outcome = intent?.outcome ?? "cancelled";
      const intentId = intent?.intent_id ?? `wrapup:${digest({ dispatch_id: assignment.dispatch_id, handle: member.handle })}`;
      const request = requestForIntent(trustedContext, intentId, "finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome });
      await (configured.publishWorkQueueRequest || store.publishWorkQueueRequest)({
        githubClient: configured.queueClient || configured.githubClient,
        owner: configured.context.repo.owner,
        repo: configured.context.repo.repo,
        ...(configured.branch === undefined ? {} : { branch: configured.branch }),
        context: trustedContext,
        actor: actorFromContext(trustedContext),
        request,
        core: configured.core,
      });
      latest = await loadQueue(configured);
      const verified = latest.projection.claims.get(member.claim_id);
      if (verified?.state !== outcome) throw new Error("work_queue_finish_not_durable");
      const authorized = outcome === "completed";
      states[member.handle] = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, state: outcome, authorized };
    } catch {
      states[member.handle] = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, state: "blocked", authorized: false };
    }
  }
  const values = Object.values(states);
  const completed = values.filter(state => ["completed", "result", "delivery_failed"].includes(state.state)).length;
  const cancelled = values.filter(state => state.state === "cancelled").length;
  const status = completed + cancelled !== values.length ? "pending" : completed && cancelled ? "completed_with_cancellations" : completed ? "completed" : "cancelled";
  return { version: 3, dispatch_id: assignment.dispatch_id, run_id: admitted.binding.run_id, run_attempt: 1, status, claims: states, errors: staged.errors };
}

async function verifyWithinBudget(verifier, member, context, remainingMs) {
  const controller = new AbortController();
  let timer;
  try {
    return await Promise.race([
      Promise.resolve().then(() => verifier(member, { ...context, signal: controller.signal })),
      new Promise(resolve => {
        timer = setTimeout(
          () => {
            controller.abort();
            resolve(null);
          },
          Math.max(1, Math.min(15000, remainingMs))
        );
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

async function finalizeWorkerResults(options = {}) {
  const configured = runtimeOptions(options);
  const assignment = normalizeAssignment(options.assignment || readWorkerSnapshot(options.snapshotPath));
  const initial = await loadQueue(configured);
  const { dispatch, profile } = validateStoredAssignment(initial.projection, assignment, { allowReleased: true });
  if (isStagedMode(options) || isStagedMode(options.config))
    return { version: 3, dispatch_id: assignment.dispatch_id, claims: Object.fromEntries(assignment.claims.map(member => [member.handle, { state: "staged_preview", effects: "unknown" }])) };
  const trustedContext = await authenticatePublisher({ ...configured, role: "reconciler" });
  const expected = { ...expectedWorkerRun(assignment, profile, configured.context), run_id: dispatch.run?.run_id };
  if (!dispatch.run) throw new Error("work_queue_binding_not_durable");
  if (trustedContext.run_id === dispatch.run.run_id && trustedContext.run_attempt !== 1) throw new Error("rerun_not_authorized");
  const results = Object.create(null);
  for (const member of assignment.claims) {
    try {
      let latest = await loadQueue(configured);
      const work = latest.projection.works.get(member.work_id);
      const claim = latest.projection.claims.get(member.claim_id);
      if (claim?.state !== "completed") {
        results[member.handle] = { state: claim?.state === "cancelled" ? "cancelled" : "pending", effects: "unknown" };
        continue;
      }
      if (work?.state !== "completed" || work.claim_id !== member.claim_id || typeof work.completion_id !== "string") throw new Error("work_queue_claim_ownership_invalid");
      if (work.barrier !== "pending") {
        const effects = work.barrier === "verified" ? latest.projection.terminalBarriers.get(member.work_id)?.evidence.effects : work.disposition;
        results[member.handle] = { state: work.barrier === "verified" ? "result" : "delivery_failed", effects: effects ?? "unknown" };
        continue;
      }
      if (typeof options.verifyEffects !== "function") {
        results[member.handle] = { state: "pending", effects: "unknown", reason: "verification_unavailable" };
        continue;
      }
      const contract = member.work?.effect_contract;
      let contractSupported = true;
      try {
        validateDeliveryContract(contract);
      } catch {
        contractSupported = false;
      }
      const policy = latest.projection.policy.pools[assignment.pool].reconciliation;
      const beforeVerification = validateNativeRun(await fetchNativeRunAttempt(configured.githubClient, expected.repository, dispatch.run.run_id), expected);
      let verification = null;
      let attempts = 0;
      let verified = false;
      const verificationStarted = Date.now();
      for (; attempts < policy.max_attempts; attempts++) {
        try {
          verification = await verifyWithinBudget(options.verifyEffects, member, { assignment, contract, attempt: attempts + 1, run: dispatch.run }, policy.deadline_ms - (Date.now() - verificationStarted));
        } catch {
          verification = null;
        }
        verified =
          contractSupported &&
          verification?.verified === true &&
          verification.contractVerified === true &&
          typeof verification.receipt === "string" &&
          verification.receipt.length > 0 &&
          verification.descriptor &&
          typeof verification.descriptor === "object" &&
          !Array.isArray(verification.descriptor) &&
          ["none", "partial"].includes(verification.effects);
        if (verified) break;
        if (Date.now() - verificationStarted >= policy.deadline_ms) {
          attempts++;
          break;
        }
        if (attempts + 1 < policy.max_attempts) await (options.sleepFn || (delay => new Promise(resolve => setTimeout(resolve, delay))))(Math.min(1000, 50 * 2 ** attempts));
      }
      latest = await loadQueue(configured);
      validateStoredAssignment(latest.projection, assignment, { allowReleased: true });
      const currentWork = latest.projection.works.get(member.work_id);
      const currentClaim = latest.projection.claims.get(member.claim_id);
      if (currentClaim?.state !== "completed" || currentClaim.dispatch_id !== assignment.dispatch_id || currentWork?.state !== "completed" || currentWork.claim_id !== member.claim_id || currentWork.completion_id !== work.completion_id)
        throw new Error("work_queue_claim_ownership_changed");
      if (currentWork.barrier !== "pending") {
        const effects = currentWork.barrier === "verified" ? latest.projection.terminalBarriers.get(member.work_id)?.evidence.effects : currentWork.disposition;
        results[member.handle] = { state: currentWork.barrier === "verified" ? "result" : "delivery_failed", effects: effects ?? "unknown" };
        continue;
      }
      const native = validateNativeRun(await fetchNativeRunAttempt(configured.githubClient, expected.repository, dispatch.run.run_id), expected);
      const controls = claimControlReceipts(latest.projection, assignment, member.handle);
      const controlsDigest = digest(controls);
      const verifiedControlsDigest = verification?.controls_digest;
      if (verifiedControlsDigest !== undefined ? verifiedControlsDigest !== controlsDigest : controls.length !== 0) verified = false;
      const checkedAt = options.now ?? Date.now();
      const base = { repository: expected.repository, workflow: expected.workflow, ref: expected.ref, principal: expected.principal_id, checked_at: checkedAt, run_id: dispatch.run.run_id, run_attempt: 1 };
      if (verified) {
        const operation = {
          kind: "Result",
          work_id: member.work_id,
          claim_id: member.claim_id,
          completion_id: work.completion_id,
          descriptor: verification.descriptor,
          evidence: { ...base, kind: "delivery", source: "verified_receipts", receipt: verification.receipt, effects: verification.effects },
        };
        await publishOperations(configured, trustedContext, ["result", assignment.dispatch_id, member.handle, work.completion_id], "result", [operation], state => {
          if (digest(claimControlReceipts(state, assignment, member.handle)) !== controlsDigest) throw new Error("work_queue_control_inventory_changed");
        });
        results[member.handle] = { state: "result", effects: verification.effects ?? "unknown" };
      } else {
        let disposition = contractSupported && ["none", "partial"].includes(verification?.effects) ? verification.effects : "unknown";
        if (controls.length && disposition === "none") disposition = "partial";
        if (disposition === "none" && (!verification?.receipt || !beforeVerification.terminal)) disposition = "unknown";
        if (!native.terminal) {
          results[member.handle] = { state: "pending", effects: disposition, reason: contractSupported ? "terminal_evidence_required" : "effect_contract_invalid" };
          continue;
        }
        if (attempts < policy.max_attempts) {
          results[member.handle] = { state: "pending", effects: disposition, reason: "verification_budget_not_exhausted" };
          continue;
        }
        const evidence = {
          ...base,
          kind: "terminal_run",
          source: "github_api",
          status: "completed",
          conclusion: native.conclusion,
          attempts: Math.max(1, attempts),
          effects: disposition,
          ...(verification?.receipt ? { receipt: verification.receipt } : {}),
        };
        const operation = { kind: "DeliveryFailure", work_id: member.work_id, claim_id: member.claim_id, completion_id: work.completion_id, reason: "verification_exhausted", disposition, evidence };
        await publishOperations(configured, trustedContext, ["delivery_failure", assignment.dispatch_id, member.handle, work.completion_id], "delivery_failure", [operation]);
        results[member.handle] = { state: "delivery_failed", effects: disposition };
      }
      latest = await loadQueue(configured);
      if (!["verified", "failed"].includes(latest.projection.works.get(member.work_id)?.barrier)) throw new Error("work_queue_result_not_durable");
    } catch {
      results[member.handle] = { state: "pending", effects: "unknown", reason: "verification_unresolved" };
    }
  }
  return { version: 3, dispatch_id: assignment.dispatch_id, claims: results };
}

function renderSummary(result) {
  const states = typeof result === "object" ? Object.values(result.claims || {}) : [];
  return `## Work queue reconciliation\n\n<details>\n<summary>Show independent Claim reconciliation</summary>\n\n${states.filter(state => state.authorized).length} Claims may process scoped effects; ${states.filter(state => !state.authorized).length} Claims are cancelled, settled or blocked. Completion is not a verified Result. Shared native capacity remains reserved until exact terminal/nonlaunch evidence.\n\n</details>\n`;
}

async function main(options = {}) {
  const coreApi = options.core || core;
  try {
    const result = await reconcileWorkerClaim(options);
    coreApi.setOutput("claim_authorizations", JSON.stringify(result));
    coreApi.info(`Work queue reconciliation: ${result.status}; authorization is per Claim`);
    await coreApi.summary.addRaw(renderSummary(result)).write();
    return result;
  } catch {
    coreApi.setOutput("claim_authorizations", JSON.stringify({ version: 3, status: "failed", claims: {} }));
    await coreApi.summary.addRaw(renderSummary({ claims: {} })).write();
    throw new Error("Work queue reconciliation failed; unverified Claim effects are blocked");
  }
}

module.exports = {
  FINISH_INTENT_PATH,
  SAFE_OUTPUTS_PATH,
  SNAPSHOT_PATH,
  main,
  readFinishIntent,
  readWorkerSnapshot,
  reconcileWorkerClaim,
  authorizeWorkerClaim,
  verifyWithinBudget,
  finalizeWorkerResults,
  renderSummary,
};
