// @ts-check
"use strict";
// @safe-outputs-exempt SEC-005 — authorizeWorkerClaim enforces the compiled profile effect_scope for assertClaimAuthorized callers, rejecting foreign message and resource repositories with E004.
const fs = require("fs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("claims");

const { canonicalBytes, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { EFFECTS_BYTES } = require("./work_queue_limits.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { policyAllowsWorker, readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { openClaim } = require("./work_queue_replay.cjs");
const { exhaustedCancellation, loadQueue, publishRequest } = require("./work_queue_binding.cjs");
const { readFinishIntents } = require("./work_queue_intents.cjs");
const { DEFAULT_FINISH_INTENT_PATH } = require("./work_queue_mcp_server.cjs");
const { effectsPath } = require("./work_queue_effect_client.cjs");
const { readInboundWorkQueueAssignment } = require("./aw_context.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { getPromptPath, renderTemplateFromFile } = require("./messages_core.cjs");
const { nodeId } = require("./work_queue_graph.cjs");
const {
  admissionInputs,
  admissionRequirement,
  authorizationProtocol,
  authorizeAdmission,
  continuationOperations,
  matchContinuation,
  parseContinuationReceipt,
  readContinuationConfig,
  readContinuationRequest,
  readHandlerConfig,
} = require("./work_queue_continuation.cjs");

/** Max effects-file size read by the finish step (raw lines before the EFFECTS_BYTES Completion bound). */
const MAX_EFFECTS_FILE_BYTES = 1024 * 1024;

function finishIntentPath() {
  return process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || DEFAULT_FINISH_INTENT_PATH;
}

function runtimeOptions(options) {
  return { githubClient: options.queueClient || options.githubClient || options.github || global.github, context: options.context || global.context, core: options.core || global.core, branch: options.branch };
}

function denied() {
  return Object.assign(new Error("work_queue_effect_scope_denied"), { code: "E004" });
}

/** Every repository named by the message or resolved resource must equal the profile's effect scope. */
function assertEffectScope(normalized, resource, effectScope) {
  const targets = [normalized.repository, normalized.repo, normalized.target_repo, normalized["target-repo"]].filter(value => value !== undefined);
  if (targets.some(target => typeof target !== "string" || target !== effectScope)) throw denied();
  if (resource === undefined) return;
  if (!resource || typeof resource !== "object" || Array.isArray(resource)) throw denied();
  const resourceTargets = [resource.repository, resource.repo].filter(value => value !== undefined);
  if (!resourceTargets.length || resourceTargets.some(target => typeof target !== "string" || target !== effectScope)) throw denied();
}

/**
 * Authorize one Claim-scoped output: the Claim is open, its Dispatch is bound to this
 * run attempt, the finish intent for its handle is `completed` (unless
 * `requireCompletion:false`), the compiled policy env still routes the Dispatch's pool
 * to its worker, and every target repository equals that policy's `effect_scope`.
 */
async function authorizeWorkerClaim(options = {}) {
  const configured = runtimeOptions(options);
  const assignment = normalizeAssignment(options.assignment);
  const normalized = normalizeClaimScope(Object.hasOwn(options, "message") ? options.message : Object.hasOwn(options, "claim_handle") ? { claim_handle: options.claim_handle } : {}, assignment);
  if (Object.hasOwn(options, "claim_handle") && normalized.claim_handle !== options.claim_handle) throw new Error("work_queue_claim_scope_invalid");
  const member = assignment.claims.find(claim => claim.handle === normalized.claim_handle);
  const { state } = await loadQueue({ githubClient: configured.githubClient, context: configured.context, role: "worker", core: configured.core, ...(configured.branch === undefined ? {} : { branch: configured.branch }) });
  const claim = state.claims.get(member.claim_id);
  const dispatch = state.dispatches.get(assignment.dispatch_id);
  if (!claim || !dispatch || claim.dispatch_id !== dispatch.dispatch_id || claim.work_id !== member.work_id || claim.handle !== member.handle) throw new Error("work_queue_claim_ownership_invalid");
  const runId = String(configured.context.runId);
  const runAttempt = Number(configured.context.runAttempt ?? 1);
  const base = { claim_handle: member.handle, claim_id: member.claim_id, work_id: member.work_id, run_id: runId, run_attempt: runAttempt };
  if (claim.state !== "open") return { ...base, authorized: false, suppressed: true, state: claim.state };
  if (dispatch.state !== "bound" || dispatch.run?.run_id !== runId || dispatch.run.run_attempt !== runAttempt) return { ...base, authorized: false, state: "unbound" };
  const policy = options.policy ?? readWorkQueuePolicyEnv();
  if (!policy || !policyAllowsWorker(policy, dispatch.pool, dispatch.worker_profile)) throw denied();
  const effectScope = policy.effect_scope;
  assertEffectScope(normalized, options.resource, effectScope);
  if (options.requireCompletion !== false) {
    const intents = options.finishIntents ?? readFinishIntents(options.finishIntentPath ?? finishIntentPath());
    if (intents.get(member.handle) !== "completed") return { ...base, effect_scope: effectScope, authorized: false, suppressed: true, state: "failed" };
  }
  log.debug("claim.authorize.complete", { require_completion: options.requireCompletion !== false });
  return { ...base, effect_scope: effectScope, authorized: true, state: "open" };
}

/**
 * Effects-file lines grouped by Claim handle, without `claim_handle`.
 * @param {string} filename
 * @returns {Map<string, Array<{kind: string, resource: unknown}>>}
 */
function readClaimEffects(filename) {
  const effects = new Map();
  if (!fs.existsSync(filename)) return effects;
  const stat = fs.lstatSync(filename);
  if (!stat.isFile() || stat.size > MAX_EFFECTS_FILE_BYTES) throw new Error("work_queue_effects_invalid");
  for (const line of fs.readFileSync(filename, "utf8").split("\n")) {
    if (!line.trim()) continue;
    const entry = parseStrictJSON(line);
    if (!entry || typeof entry !== "object" || typeof entry.claim_handle !== "string" || typeof entry.kind !== "string") throw new Error("work_queue_effects_invalid");
    const list = effects.get(entry.claim_handle) ?? [];
    list.push({ kind: entry.kind, resource: entry.resource ?? null });
    effects.set(entry.claim_handle, list);
  }
  return effects;
}

/** Effects over EFFECTS_BYTES are dropped (Completion carries `[]`) with a warning. */
function boundedEffects(entries, handle, coreApi) {
  if (canonicalBytes(entries) <= EFFECTS_BYTES) return entries;
  coreApi?.warning?.(`Claim ${handle}: ${entries.length} recorded effects exceed ${EFFECTS_BYTES} bytes; the Completion records no effects`);
  return [];
}

/**
 * @typedef {{
 *   config: import("./work_queue_continuation.cjs").ContinuationConfig,
 *   entry: import("./work_queue_continuation.cjs").ContinuationEntry | null | undefined,
 *   verify?: (child: any, claim: any) => void
 * }} FinishContinuations
 */

/**
 * Operations of one `finish` commit for the Dispatch's open Claim: Completion for a
 * `completed` intent, else ClaimCancellation{worker_failed} plus
 * WorkCancellation{attempts_exhausted} on the last attempt. With a routed
 * continuation, the Completion is followed immediately by at most one successor
 * Work; a request without a receipt fails the plan.
 * @param {any} state
 * @param {string} dispatchId
 * @param {Map<string, string>} intents
 * @param {Map<string, Array<unknown>>} effects bounded effects per handle
 * @param {import("./work_queue_policy.cjs").PolicyEnv | null} policy
 * @param {FinishContinuations} [continuations]
 */
function finishOperations(state, dispatchId, intents, effects, policy, continuations = undefined) {
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("dispatch_missing", "the assigned Dispatch is not in the ledger");
  const claim = openClaim(state, dispatch);
  if (!claim) return [];
  if (intents.get(claim.handle) !== "completed") return [{ kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "worker_failed" }, ...exhaustedCancellation(state, claim, policy)];
  const completion = { kind: "Completion", work_id: claim.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId, claim_handle: claim.handle, effects: effects.get(claim.handle) ?? [] };
  if (continuations?.entry === undefined) return [completion];
  const { config, entry, verify } = continuations;
  if (entry === null) throw queueError("continuation_receipt_missing", `completed Claim ${claim.handle} requested a continuation without an accepted receipt`);
  const [child] = continuationOperations(state, claim, entry, config, policy);
  if (!child) return [completion];
  // A successor requires the policy; it replaces its completing parent, so the pool's pending count is unchanged.
  // prettier-ignore
  const { pending_limit: limit } = /** @type {NonNullable<typeof policy>} */ (policy);
  const pool = state.works.get(claim.work_id).pool;
  let pending = 0;
  for (const work of state.works.values()) if (work.pool === pool && (work.state === "available" || work.state === "claimed")) pending++;
  if (pending > limit) throw queueError("work_queue_pending_limit", `pool ${pool} would exceed its pending_limit of ${limit}`);
  verify?.(child, claim);
  return [completion, child];
}

/**
 * Read the routed request evidence and receipt for this Dispatch's Claim and, when
 * global authorization is configured, authorize its successor's admission now.
 * @returns {Promise<{continuations: FinishContinuations, admission: {decision: any, digest: string, evidencePath: string} | null} | null>}
 */
async function prepareContinuations({ env, githubClient, context, coreApi, branch, dispatchId, intents, effects, policy, authorization }) {
  const config = readContinuationConfig(env);
  if (!config) return null;
  const { state } = await loadQueue({ githubClient, context, role: "worker", core: coreApi, ...(branch === undefined ? {} : { branch }) });
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("work_queue_assignment_unknown", "the assigned Dispatch is not in the ledger");
  const claim = state.claims.get(dispatch.claims[0]);
  const { available, evidence, request } = readContinuationRequest({ env, context, claim });
  if (!available) coreApi.info("Work queue finish: no bound agent output; no continuation can be admitted");
  const entry = matchContinuation({ config, request, receipt: parseContinuationReceipt(env.GH_AW_WORK_QUEUE_CONTINUATIONS), intent: intents.get(claim.handle), claim });
  /** @type {FinishContinuations} */
  const continuations = { config, entry };
  const handlerConfig = readHandlerConfig(env);
  const requirement = admissionRequirement(handlerConfig);
  if (!requirement || !entry) return { continuations, admission: null };
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  const inputsFor = (child, parent) => admissionInputs({ child, claim: parent, dispatchId, repository, evidence, entry });
  // A finish already committed plans no successor here: recovery is a read.
  const [, child] = finishOperations(state, dispatchId, intents, effects, policy, continuations);
  const admission = child ? await authorizeAdmission({ requirement, inputs: inputsFor(child, claim), handlerConfig, config, core: coreApi, env, authorization }) : null;
  const { assertAuthorizationBatchFresh, canonicalDigest } = authorizationProtocol();
  continuations.verify = (planned, parent) => {
    if (!admission || admission.digest !== canonicalDigest(inputsFor(planned, parent))) throw queueError("continuation_authorization_scope_changed", "the successor no longer matches its authorized admission");
    assertAuthorizationBatchFresh({ authorizations: [admission.decision] });
  };
  return { continuations, admission };
}

function recordAdmissionOutcomes(prepared, operationStatus, coreApi) {
  if (!prepared?.admission) return;
  const { decision, evidencePath } = prepared.admission;
  authorizationProtocol().recordAuthorizationOutcome({ evidencePath, authorization: decision, operationStatus, core: coreApi });
}

/**
 * The committed successor of this Dispatch's completed Claim (zero or one result), reported only after the finish commit.
 * @param {any} state
 * @param {FinishContinuations | undefined} continuations
 * @returns {Array<{claim_handle: string, status: "admitted" | "depth_limit", work_id?: string, approval_issue?: number}>}
 */
function continuationResults(state, continuations) {
  const entry = continuations?.entry;
  if (!entry) return [];
  const { claim_handle: handle, claim_id: claimId, issue, status } = entry.receipt;
  const claim = state.claims.get(claimId);
  const childId = claim && nodeId(state.works.get(claim.work_id).graph_id, `continuation:${claim.work_id}`);
  const child = childId && state.works.get(childId);
  if (child) return [{ claim_handle: handle, status: "admitted", work_id: child.work_id, ...(issue ? { approval_issue: Number(issue.number) } : {}) }];
  return status === "depth_limit" && claim?.state === "completed" ? [{ claim_handle: handle, status: "depth_limit" }] : [];
}

/** The approval Issue the manager created for this run; after a failed finish it pins no successor. */
function orphanApprovalIssues(env) {
  if (env.GH_AW_CONTINUATION_ROUTE !== "issue") return [];
  try {
    const receipt = parseContinuationReceipt(env.GH_AW_WORK_QUEUE_CONTINUATIONS);
    return receipt?.issue ? [receipt.issue.number] : [];
  } catch {
    return [];
  }
}

function renderSummary(claims) {
  const completed = claims.filter(claim => claim.state === "completed").length;
  return renderTemplateFromFile(getPromptPath("work_queue_reconciliation_summary.md"), { completed_claims: completed, failed_claims: claims.length - completed }).trimEnd() + "\n\n\n";
}

/**
 * Finish step (safe_outputs job, after Process Safe Outputs): publish one `finish`
 * commit for this run's Dispatch and set `work_queue_outcome`.
 */
async function main(options = {}) {
  const { githubClient, context, core: coreApi, branch } = runtimeOptions(options);
  const env = options.env || process.env;
  const reference = readInboundWorkQueueAssignment(context.payload);
  if (!reference) {
    coreApi.info("Work queue finish: this run has no work_queue_assignment");
    return null;
  }
  if (isStagedMode(options.config)) {
    coreApi.info("Work queue finish: staged preview; no finish commit published");
    return null;
  }
  const dispatchId = reference.dispatch_id;
  const policy = options.policy ?? readWorkQueuePolicyEnv();
  let intents;
  try {
    intents = readFinishIntents(options.finishIntentPath ?? finishIntentPath());
  } catch (error) {
    coreApi.warning(`Work queue finish intents are invalid; every Claim fails: ${error instanceof Error ? error.message : String(error)}`);
    intents = new Map();
  }
  const effects = new Map([...readClaimEffects(options.effectsPath ?? effectsPath())].map(([handle, entries]) => [handle, boundedEffects(entries, handle, coreApi)]));
  /** @type {Awaited<ReturnType<typeof prepareContinuations>>} */
  let prepared = null;
  let published;
  try {
    prepared = await prepareContinuations({ env, githubClient, context, coreApi, branch, dispatchId, intents, effects, policy, authorization: options.authorization ?? {} });
    const continuations = prepared?.continuations;
    published = await publishRequest({
      githubClient,
      context,
      role: "worker",
      core: coreApi,
      ...(branch === undefined ? {} : { branch }),
      kind: "finish",
      parameters: { dispatch_id: dispatchId },
      operationsFor: state => finishOperations(state, dispatchId, intents, effects, policy, continuations),
    });
  } catch (error) {
    recordAdmissionOutcomes(prepared, "skipped", coreApi);
    const orphans = orphanApprovalIssues(env);
    if (orphans.length) coreApi.warning(`Work queue finish was not published; continuation approval Issue(s) ${orphans.map(number => `#${number}`).join(", ")} pin no successor and cannot release work.`);
    throw error;
  }
  recordAdmissionOutcomes(prepared, published.committed && !published.recovered ? "confirmed" : "skipped", coreApi);
  const dispatch = published.state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("work_queue_assignment_unknown", "the assigned Dispatch is not in the ledger");
  const claims = dispatch.claims.map(id => published.state.claims.get(id));
  const outcome = claims.length && claims.every(claim => claim.state === "completed") ? "completed" : "failed";
  const continuations = published.committed ? continuationResults(published.state, prepared?.continuations) : [];
  coreApi.setOutput("work_queue_outcome", outcome);
  coreApi.info(`Work queue finish: ${dispatchId} ${outcome} (${published.committed ? (published.recovered ? "already committed" : "committed") : "nothing to finish"})`);
  for (const result of continuations) {
    coreApi.info(
      result.status === "admitted"
        ? `Work queue continuation of ${result.claim_handle}: queued Work ${result.work_id}${result.approval_issue ? ` awaiting approval Issue #${result.approval_issue}` : ""}`
        : `Work queue continuation of ${result.claim_handle}: depth limit reached; no successor queued`
    );
  }
  await coreApi.summary?.addRaw(renderSummary(claims)).write();
  if (policy) {
    try {
      await require("./work_queue_issues.cjs").projectIssues({ githubClient, context, core: coreApi, policy, state: published.state, ...(branch === undefined ? {} : { branch }) });
    } catch (error) {
      coreApi.warning(`Work queue Issue projection failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return {
    dispatch_id: dispatchId,
    outcome,
    committed: published.committed,
    claims: Object.fromEntries(claims.map(claim => [claim.handle, claim.state])),
    ...(prepared ? { continuations } : {}),
  };
}

module.exports = { authorizeWorkerClaim, finishOperations, main, readClaimEffects };
