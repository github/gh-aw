// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
// @safe-outputs-exempt SEC-005 — authorizeWorkerClaim enforces the authenticated profile.effect_scope allowlist for assertClaimAuthorized callers, rejecting foreign message and resource repositories with E004.
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("claims");

const { isProxy } = require("node:util").types;
const queue = require("./work_queue_replay.cjs");
const store = require("./work_queue_store.cjs");
const { canonical, closed, digest, integer, parseStrictJSON } = require("./work_queue_codec.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { DEFAULT_FINISH_INTENT_PATH, loadWorkQueueSnapshot } = require("./work_queue_mcp_server.cjs");
const { readIntentLines, requestForIntent } = require("./work_queue_intents.cjs");
const { bindWorkerAssignment, loadQueue, publishOperations, validateStoredAssignment, expectedWorkerRun, bindingForRun } = require("./work_queue_binding.cjs");
const { authenticatePublisher, fetchNativeRunAttempt, validateNativeRun } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { claimControlReceipts } = require("./work_queue_control_receipts.cjs");
const { validateDeliveryContract, isTrustedClaimDelivery } = require("./work_queue_delivery.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";
const FINISH_INTENT_PATH = DEFAULT_FINISH_INTENT_PATH;

/** @typedef {Record<string, unknown> & {verified: true, contractVerified: true, receipt: string, descriptor: object, effects: "none" | "partial"}} VerifiedClaimEffects */

/**
 * @param {Record<string, unknown> | null} value
 * @returns {value is VerifiedClaimEffects}
 */
function verifiedClaimEffects(value) {
  return (
    typeof value === "object" &&
    value !== null &&
    value.verified === true &&
    value.contractVerified === true &&
    typeof value.receipt === "string" &&
    value.receipt.length > 0 &&
    value.descriptor !== null &&
    typeof value.descriptor === "object" &&
    !Array.isArray(value.descriptor) &&
    (value.effects === "none" || value.effects === "partial")
  );
}

/** @param {VerifiedClaimEffects} proof */
function snapshotClaimDelivery(proof) {
  const ancestors = new Set();
  /**
   * @param {unknown} value
   * @param {number} depth
   */
  function inspect(value, depth) {
    if (value === null || typeof value !== "object") return;
    if (depth > 64 || ancestors.has(value) || isProxy(value)) throw new Error(`${SAFE_OUTPUT_E001}: work_queue_delivery_proof_invalid`);
    ancestors.add(value);
    for (const descriptor of Object.values(Object.getOwnPropertyDescriptors(value))) {
      if (descriptor.get || descriptor.set) throw new Error("work_queue_delivery_proof_invalid");
      if (descriptor.enumerable) inspect(descriptor.value, depth + 1);
    }
    ancestors.delete(value);
  }
  inspect(proof, 0);
  return JSON.parse(canonical(proof));
}

function readWorkerSnapshot(snapshotPath = process.env.GH_AW_WORK_QUEUE_SNAPSHOT || SNAPSHOT_PATH) {
  return loadWorkQueueSnapshot(snapshotPath).worker;
}

function readFinishIntent(filename = process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || FINISH_INTENT_PATH, assignment) {
  const intents = new Map();
  const errors = [];
  const lines = readIntentLines(filename);
  for (const [index, line] of lines.entries()) {
    if (!line.trim()) continue;
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
  log.debug("claim.authorize.start");
  const configured = runtimeOptions(options);
  const assignment = normalizeAssignment(options.assignment);
  const normalized = normalizeClaimScope(Object.hasOwn(options, "message") ? options.message : Object.hasOwn(options, "claim_handle") ? { claim_handle: options.claim_handle } : {}, assignment);
  if (Object.hasOwn(options, "claim_handle") && normalized.claim_handle !== options.claim_handle) throw new Error("work_queue_claim_scope_invalid");
  const member = assignment.claims.find(claim => claim.handle === normalized.claim_handle);
  const latest = await loadQueue(configured);
  const { dispatch, profile } = validateStoredAssignment(latest.projection, assignment);
  const trustedContext = await authenticatePublisher({ ...configured, role: "worker", dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
  const expected = expectedWorkerRun(assignment, profile, configured.context, trustedContext.repository);
  const native = validateNativeRun(trustedContext.native_run, { ...expected, run_id: trustedContext.run_id });
  if (!dispatch.run || dispatch.state !== "bound" || canonical(dispatch.run) !== canonical(bindingForRun(native, expected))) throw new Error("work_queue_binding_not_durable");
  const targets = [normalized.repository, normalized.repo, normalized.target_repo, normalized["target-repo"]].filter(value => value !== undefined);
  const allowedRepos = [profile.effect_scope];
  if (targets.some(target => typeof target !== "string" || !allowedRepos.includes(target))) throw Object.assign(new Error("work_queue_effect_scope_denied"), { code: "E004" });
  const claim = latest.projection.claims.get(member.claim_id);
  const work = latest.projection.works.get(member.work_id);
  const base = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, run_id: native.run_id, run_attempt: 1, effect_scope: profile.effect_scope };
  let resource;
  if (options.resource !== undefined) {
    const suppliedResource = options.resource;
    if (!suppliedResource || typeof suppliedResource !== "object" || Array.isArray(suppliedResource)) throw new Error("work_queue_effect_scope_denied");
    const resourceTargets = [suppliedResource.repository, suppliedResource.repo].filter(value => value !== undefined);
    if (!resourceTargets.length || resourceTargets.some(target => typeof target !== "string" || !allowedRepos.includes(target))) throw Object.assign(new Error("work_queue_effect_scope_denied"), { code: "E004" });
    resource = { ...suppliedResource, repository: resourceTargets[0] };
    delete resource.repo;
  }
  if (claim?.state === "cancelled") return { ...base, authorized: false, suppressed: true, state: "cancelled" };
  if (options.requireCompletion !== false && claim?.state !== "completed") return { ...base, authorized: false, state: "open" };
  if (options.requireCompletion !== false && work.barrier !== "pending") {
    if (work.state !== "completed" || work.claim_id !== member.claim_id) throw new Error("work_queue_claim_ownership_invalid");
    return { ...base, authorized: false, suppressed: work.barrier === "verified", state: work.barrier === "verified" ? "result" : "delivery_failed" };
  }
  queue.validateClaimAuthority(latest.projection, member.claim_id, trustedContext, {
    requireCompletion: options.requireCompletion !== false,
    ...(resource === undefined ? {} : { resource }),
  });
  log.debug("claim.authorize.complete", { require_completion: options.requireCompletion !== false });
  return { ...base, authorized: true, state: options.requireCompletion === false ? "open" : "completed" };
}

async function reconcileWorkerClaim(options = {}) {
  log.debug("claims.reconcile.start");
  const configured = runtimeOptions(options);
  const supplied = options.assignment ?? (options.worker === undefined ? readWorkerSnapshot(options.snapshotPath) : options.worker);
  if (!supplied) return { version: 3, status: options.requireAssignment ? "missing" : "unassigned", claims: {}, errors: [] };
  const assignment = normalizeAssignment(supplied);
  const staged = readFinishIntent(options.finishIntentPath, assignment);
  log.debug("claims.reconcile.intents", { claims: assignment.claims.length, intents: staged.intents.size, errors: staged.errors.length });
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
      log.debug("claim.finish.persisted", { completed: outcome === "completed", cancelled: outcome === "cancelled" });
      const authorized = outcome === "completed";
      states[member.handle] = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, state: outcome, authorized };
    } catch (error) {
      log.failure("claim.finish.blocked", error);
      states[member.handle] = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, state: "blocked", authorized: false };
    }
  }
  const values = Object.values(states);
  const completed = values.filter(state => ["completed", "result", "delivery_failed"].includes(state.state)).length;
  const cancelled = values.filter(state => state.state === "cancelled").length;
  const status = completed + cancelled !== values.length ? "pending" : completed && cancelled ? "completed_with_cancellations" : completed ? "completed" : "cancelled";
  log.debug("claims.reconcile.complete", { completed, cancelled, pending: completed + cancelled !== values.length });
  return { version: 3, dispatch_id: assignment.dispatch_id, run_id: admitted.binding.run_id, run_attempt: 1, status, claims: states, errors: staged.errors };
}

/**
 * @param {(member: object, context: Record<string, unknown> & {signal: AbortSignal}) => Record<string, unknown> | null | Promise<Record<string, unknown> | null>} verifier
 * @param {object} member
 * @param {Record<string, unknown>} context
 * @param {number} remainingMs
 * @returns {Promise<Record<string, unknown> | null>}
 */
async function verifyWithinBudget(verifier, member, context, remainingMs) {
  const controller = new AbortController();
  let timer;
  try {
    /** @type {Promise<null>} */
    const timeout = new Promise(resolve => {
      timer = setTimeout(
        () => {
          log.debug("delivery.verifier.timed_out");
          controller.abort();
          resolve(null);
        },
        Math.max(1, Math.min(15000, remainingMs))
      );
    });
    return await Promise.race([Promise.resolve().then(() => verifier(member, { ...context, signal: controller.signal })), timeout]);
  } finally {
    clearTimeout(timer);
  }
}

async function finalizeWorkerResults(options = {}) {
  log.debug("results.finalize.start");
  const configured = runtimeOptions(options);
  const assignment = normalizeAssignment(options.assignment || readWorkerSnapshot(options.snapshotPath));
  const initial = await loadQueue(configured);
  const { dispatch, profile } = validateStoredAssignment(initial.projection, assignment, { allowReleased: true });
  if (isStagedMode(options) || isStagedMode(options.config))
    return { version: 3, dispatch_id: assignment.dispatch_id, claims: Object.fromEntries(assignment.claims.map(member => [member.handle, { state: "staged_preview", effects: "unknown" }])) };
  const trustedContext = await authenticatePublisher({ ...configured, role: "reconciler" });
  const expected = { ...expectedWorkerRun(assignment, profile, configured.context, trustedContext.repository), run_id: dispatch.run?.run_id };
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
      if (trustedContext.run_id !== dispatch.run.run_id && !beforeVerification.terminal) {
        results[member.handle] = { state: "pending", effects: "unknown", reason: "terminal_evidence_required" };
        continue;
      }
      /** @type {Record<string, unknown> | null} */
      let verification = null;
      let attempts = 0;
      /** @type {VerifiedClaimEffects | null} */
      let verified = null;
      /** @type {VerifiedClaimEffects | null} */
      let verifiedSnapshot = null;
      const verificationStarted = Date.now();
      integer(work.completion_at, 0, Number.MAX_SAFE_INTEGER, "Completion timestamp");
      const remainingDeadline = policy.deadline_ms - Math.max(0, (options.now ?? verificationStarted) - work.completion_at);
      for (; attempts < policy.max_attempts && Date.now() - verificationStarted < remainingDeadline; attempts++) {
        log.debug("delivery.verifier.attempt", { attempt: attempts + 1 });
        try {
          verification = await verifyWithinBudget(options.verifyEffects, member, { assignment, contract, attempt: attempts + 1, run: dispatch.run }, remainingDeadline - (Date.now() - verificationStarted));
        } catch (error) {
          log.failure("delivery.verifier.failed", error);
          verification = null;
        }
        const trustedDelivery = isTrustedClaimDelivery(verification, assignment, member.handle);
        if (verification?.verified === true && !trustedDelivery) verification = null;
        verified = contractSupported && trustedDelivery && verifiedClaimEffects(verification) ? verification : null;
        log.debug("delivery.verifier.checked", { trusted: trustedDelivery, verified: !!verified });
        if (verified) {
          verifiedSnapshot = snapshotClaimDelivery(verified);
          break;
        }
        if (Date.now() - verificationStarted >= remainingDeadline) {
          attempts++;
          break;
        }
        if (attempts + 1 < policy.max_attempts)
          await (options.sleepFn || (delay => new Promise(resolve => setTimeout(resolve, delay))))(Math.min(1000, 50 * 2 ** attempts, Math.max(0, remainingDeadline - (Date.now() - verificationStarted))));
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
      const verifiedControlsDigest = verifiedSnapshot?.controls_digest;
      const verificationReceipt = typeof verification?.receipt === "string" && verification.receipt.length > 0 ? verification.receipt : undefined;
      if (typeof verifiedControlsDigest !== "string" || !/^[0-9a-f]{64}$/.test(verifiedControlsDigest) || verifiedControlsDigest !== controlsDigest) verified = null;
      const checkedAt = options.now ?? Date.now();
      const base = { repository: expected.repository, workflow: expected.workflow, ref: expected.ref, principal: expected.principal_id, checked_at: checkedAt, run_id: dispatch.run.run_id, run_attempt: 1 };
      if (verified && verifiedSnapshot) {
        if (!isTrustedClaimDelivery(verified, assignment, member.handle)) throw new Error("work_queue_delivery_proof_changed");
        const operation = {
          kind: "Result",
          work_id: member.work_id,
          claim_id: member.claim_id,
          completion_id: work.completion_id,
          descriptor: verifiedSnapshot.descriptor,
          evidence: { ...base, kind: "delivery", source: "verified_receipts", receipt: verifiedSnapshot.receipt, effects: verifiedSnapshot.effects },
        };
        await publishOperations(configured, trustedContext, ["result", assignment.dispatch_id, member.handle, work.completion_id], "result", [operation], state => {
          if (!isTrustedClaimDelivery(verified, assignment, member.handle)) throw new Error("work_queue_delivery_proof_changed");
          validateStoredAssignment(state, assignment, { allowReleased: true });
          if (digest(claimControlReceipts(state, assignment, member.handle)) !== verifiedSnapshot.controls_digest) throw new Error("work_queue_control_inventory_changed");
        });
        results[member.handle] = { state: "result", effects: verifiedSnapshot.effects };
        log.debug("result.persisted");
      } else {
        let disposition = contractSupported && verificationReceipt && (verification?.effects === "none" || verification?.effects === "partial") ? verification.effects : "unknown";
        if (controls.length && disposition === "none") disposition = "partial";
        if (disposition === "none" && (!verificationReceipt || !beforeVerification.terminal)) disposition = "unknown";
        if (!native.terminal) {
          results[member.handle] = { state: "pending", effects: disposition, reason: contractSupported ? "terminal_evidence_required" : "effect_contract_invalid" };
          continue;
        }
        if (attempts < policy.max_attempts && checkedAt - work.completion_at < policy.deadline_ms) {
          results[member.handle] = { state: "pending", effects: disposition, reason: "verification_budget_not_exhausted" };
          continue;
        }
        const evidence = {
          ...base,
          kind: "terminal_run",
          source: "github_api",
          status: "completed",
          conclusion: native.conclusion,
          ...(attempts > 0 ? { attempts } : {}),
          effects: disposition,
          ...(verificationReceipt ? { receipt: verificationReceipt } : {}),
        };
        const operation = { kind: "DeliveryFailure", work_id: member.work_id, claim_id: member.claim_id, completion_id: work.completion_id, reason: "verification_exhausted", disposition, evidence };
        await publishOperations(configured, trustedContext, ["delivery_failure", assignment.dispatch_id, member.handle, work.completion_id], "delivery_failure", [operation]);
        results[member.handle] = { state: "delivery_failed", effects: disposition };
        log.debug("delivery.failure.persisted", { attempts, unknown_effects: disposition === "unknown" });
      }
      latest = await loadQueue(configured);
      if (!["verified", "failed"].includes(latest.projection.works.get(member.work_id)?.barrier)) throw new Error("work_queue_result_not_durable");
    } catch (error) {
      log.failure("result.verification.unresolved", error);
      results[member.handle] = { state: "pending", effects: "unknown", reason: "verification_unresolved" };
    }
  }
  log.debug("results.finalize.complete", { claims: Object.keys(results).length });
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
  } catch (error) {
    log.failure("claims.reconcile.failed", error);
    coreApi.setOutput("claim_authorizations", JSON.stringify({ version: 3, status: "failed", claims: {} }));
    await coreApi.summary.addRaw(renderSummary({ claims: {} })).write();
    throw new Error("Work queue reconciliation failed; unverified Claim effects are blocked");
  }
}

module.exports = {
  FINISH_INTENT_PATH,
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
