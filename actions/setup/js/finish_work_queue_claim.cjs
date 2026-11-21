// @ts-check
"use strict";
// @safe-outputs-exempt SEC-005 — authorizeWorkerClaim enforces the compiled profile effect_scope for assertClaimAuthorized callers, rejecting foreign message and resource repositories with E004.
const fs = require("fs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("claims");

const { canonicalBytes, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { EFFECTS_BYTES } = require("./work_queue_limits.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { openClaims } = require("./work_queue_replay.cjs");
const { exhaustedCancellation, loadQueue, publishRequest } = require("./work_queue_binding.cjs");
const { readFinishIntents } = require("./work_queue_intents.cjs");
const { DEFAULT_FINISH_INTENT_PATH } = require("./work_queue_mcp_server.cjs");
const { effectsPath } = require("./work_queue_effect_client.cjs");
const { readInboundWorkQueueAssignment } = require("./aw_context.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { getPromptPath, renderTemplateFromFile } = require("./messages_core.cjs");

/** Max effects-file size read by the finish step (16 Claims × EFFECTS_BYTES with headroom). */
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
 * `requireCompletion:false`), and every target repository equals the profile's
 * `effect_scope` from the compiled policy env.
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
  const effectScope = policy?.pools?.[dispatch.pool]?.profiles?.[dispatch.worker_profile]?.effect_scope;
  if (typeof effectScope !== "string") throw denied();
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
 * Operations of one `finish` commit: Completion for a `completed` intent, else
 * ClaimCancellation{worker_failed} plus WorkCancellation{attempts_exhausted} on the last attempt.
 * @param {any} state
 * @param {string} dispatchId
 * @param {Map<string, string>} intents
 * @param {Map<string, Array<unknown>>} effects bounded effects per handle
 * @param {import("./work_queue_policy.cjs").PolicyEnv | null} policy
 */
function finishOperations(state, dispatchId, intents, effects, policy) {
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("dispatch_missing", "the assigned Dispatch is not in the ledger");
  return openClaims(state, dispatch).flatMap(claim =>
    intents.get(claim.handle) === "completed"
      ? [{ kind: "Completion", work_id: claim.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId, claim_handle: claim.handle, effects: effects.get(claim.handle) ?? [] }]
      : [{ kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "worker_failed" }, ...exhaustedCancellation(state, claim, policy)]
  );
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
  const published = await publishRequest({
    githubClient,
    context,
    role: "worker",
    core: coreApi,
    ...(branch === undefined ? {} : { branch }),
    kind: "finish",
    parameters: { dispatch_id: dispatchId },
    operationsFor: state => finishOperations(state, dispatchId, intents, effects, policy),
  });
  const dispatch = published.state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("work_queue_assignment_unknown", "the assigned Dispatch is not in the ledger");
  const claims = dispatch.claims.map(id => published.state.claims.get(id));
  const outcome = claims.length && claims.every(claim => claim.state === "completed") ? "completed" : "failed";
  coreApi.setOutput("work_queue_outcome", outcome);
  coreApi.info(`Work queue finish: ${dispatchId} ${outcome} (${published.committed ? (published.recovered ? "already committed" : "committed") : "nothing to finish"})`);
  await coreApi.summary?.addRaw(renderSummary(claims)).write();
  if (policy) {
    try {
      await require("./work_queue_issues.cjs").projectIssues({ githubClient, context, core: coreApi, policy, state: published.state, ...(branch === undefined ? {} : { branch }) });
    } catch (error) {
      coreApi.warning(`Work queue Issue projection failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return { dispatch_id: dispatchId, outcome, committed: published.committed, claims: Object.fromEntries(claims.map(claim => [claim.handle, claim.state])) };
}

module.exports = { authorizeWorkerClaim, finishOperations, main, readClaimEffects };
