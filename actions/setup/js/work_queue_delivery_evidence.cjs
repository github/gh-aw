// @ts-check
"use strict";

const { canonicalBytes, digest, parseStrictJSON } = require("./work_queue_codec.cjs");
const { normalizeAssignment, normalizeClaimScope, claimIdentity } = require("./work_queue_claim_scope.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
const { loadQueue, publishOperations, validateStoredAssignment } = require("./work_queue_binding.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");
const { claimControlReceipts, verifyCommittedClaimQueueControl } = require("./work_queue_control_receipts.cjs");
const { requestIdForIntent } = require("./work_queue_intents.cjs");
const { NO_WRITE_TYPES, durableBuiltinReceipt, verifyBuiltinDeliveryOutput } = require("./work_queue_delivery.cjs");
const { durableClaimAdapterEntry, durableDeclaredBuiltinReceipt, verifyClaimAdapterOutput } = require("./work_queue_claim_adapters.cjs");
const { durableNativeCreationReceipt, verifyNativeCreationDelivery } = require("./work_queue_native_creation_delivery.cjs");
const { durableAssetReceipt, verifyAssetDelivery } = require("./work_queue_upload_assets.cjs");
const { durableCodeScanningReceipt, verifyCodeScanningDelivery } = require("./work_queue_code_scanning.cjs");
const { durableCodeCoverageReceipt, verifyCodeCoverageDelivery } = require("./work_queue_code_coverage.cjs");
const { durableArtifactReceipt, verifyArtifactDelivery } = require("./upload_artifact.cjs");

const SPECIALIZED_VERIFIERS = {
  native_creation: { types: ["create_discussion", "create_pull_request"], durable: (result, message, claim) => durableNativeCreationReceipt(result, message, claim), verify: verifyNativeCreationDelivery },
  asset: { types: ["upload_asset"], durable: (result, _message, claim) => durableAssetReceipt(result, claim), verify: verifyAssetDelivery },
  code_scanning: { types: ["create_code_scanning_alert"], durable: (result, _message, claim) => durableCodeScanningReceipt(result, claim), verify: verifyCodeScanningDelivery },
  code_coverage: { types: ["upload_code_coverage"], durable: (result, _message, claim) => durableCodeCoverageReceipt(result, claim), verify: verifyCodeCoverageDelivery },
  artifact: { types: ["upload_artifact"], durable: (result, _message, claim) => durableArtifactReceipt(result, claim), verify: verifyArtifactDelivery },
};
const ADAPTER_VERIFIERS = new Set(["rest_adapter", "graphql_adapter", "git_tree_adapter", "prepared_builtin"]);

const MAX_EVIDENCE_BYTES = 32 * 1024;

function evidenceEntryFor(index, raw, { assignment, claim, results, config, effects, context }) {
  const message = normalizeClaimScope({ ...raw, type: normalizeSafeOutputName(raw.type) }, assignment);
  if (message.claim_handle !== claim.handle) throw new Error("work_queue_claim_scope_invalid");
  const type = message.type;
  if (NO_WRITE_TYPES.has(type)) return { entry: { index, type, verifier: "none" } };
  const outcome = results.find(result => result.messageIndex === index);
  // Omitting a failed optional output would turn an incomplete pass into a
  // complete synthetic inventory after restart. Never publish that subset.
  if (!outcome || !outcome.success || outcome.delegated || outcome.deferred || outcome.skipped || outcome.cancelled) return { unsupported: `delivery_evidence_receipt_missing:${type}` };
  if (outcome.claim_handle && outcome.claim_handle !== claim.handle) throw new Error("work_queue_claim_scope_invalid");
  const verification = claim.work.effect_contract?.outputs?.find(output => normalizeSafeOutputName(output.type) === type)?.verification;
  const input = { claim, message, result: outcome.result, effects, config: config[type], context, verification };
  let entry;
  const adapter = config.claim_adapters?.[type];
  if (adapter) {
    entry = durableClaimAdapterEntry({ ...input, adapter });
  } else if (verification !== undefined) {
    const receipt = durableDeclaredBuiltinReceipt(input);
    if (receipt) entry = { verifier: "declared_builtin", receipt };
  } else {
    const specialized = Object.entries(SPECIALIZED_VERIFIERS).find(([, spec]) => spec.types.includes(type));
    const verifier = specialized ? specialized[0] : "builtin";
    const receipt = specialized ? specialized[1].durable(outcome.result, message, claim) : durableBuiltinReceipt(input);
    if (receipt) entry = { verifier, receipt };
  }
  if (!entry) return { unsupported: `delivery_evidence_unsupported:${type}` };
  return { entry: { index, type, ...entry } };
}

function collectDeliveryEvidence(options) {
  const assignment = normalizeAssignment(options.assignment);
  const claim = assignment.claims.find(member => member.handle === options.claim_handle);
  if (!claim) throw new Error("work_queue_claim_scope_invalid");
  const outputs = [];
  const messages = options.messages || [];
  const results = options.results || [];
  const config = options.config || {};
  for (const [index, raw] of messages.entries()) {
    const result = evidenceEntryFor(index, raw, { assignment, claim, results, config, effects: options.effects || [], context: options.context });
    if (result.unsupported) return result;
    outputs.push(result.entry);
  }
  for (const [offset, raw] of (options.controls || []).entries()) {
    const message = normalizeClaimScope(raw, assignment);
    if (message.claim_handle !== claim.handle || !["work_queue_submit", "work_queue_dispatch_next"].includes(message.type)) throw new Error("work_queue_control_receipt_scope_invalid");
    // request_id is filled from authenticated worker authority at publication,
    // not the unauthenticated Actions context supplied to the handler manager.
    outputs.push({ index: messages.length + offset, type: message.type, verifier: "queue_control", receipt: { intent_id: message.intent_id } });
  }
  const identity = claimIdentity(claim.handle, assignment);
  if ((options.effects || []).some(effect => Object.entries(identity).some(([field, value]) => effect[field] !== value))) return { unsupported: "delivery_evidence_foreign_effect" };
  const effects = (options.effects || []).map(effect => ({
    kind: effect.kind,
    repository: effect.repository,
    outcome: effect.outcome,
    ...(effect.id ? { id: effect.id } : {}),
    ...(Number.isSafeInteger(effect.number) ? { number: effect.number } : {}),
    ...(effect.commit ? { commit: effect.commit } : {}),
    ...(effect.expected ? { expected: Object.fromEntries(["ref", "sha", "labels", "assignees"].filter(field => Object.hasOwn(effect.expected, field)).map(field => [field, effect.expected[field]])) } : {}),
  }));
  if (outputs.length > 128 || effects.length > 256 || outputs.some(entry => entry.index > 127) || canonicalBytes({ outputs, effects }) > MAX_EVIDENCE_BYTES - 1024) return { unsupported: "delivery_evidence_oversized" };
  return { outputs, effects };
}

async function publishDeliveryEvidence(options) {
  const configured = { ...options, githubClient: options.githubClient || options.github || global.github, context: options.context || global.context, core: options.core || global.core };
  const assignment = normalizeAssignment(options.assignment);
  const { claim_handle } = normalizeClaimScope({ claim_handle: options.claim_handle }, assignment);
  const member = assignment.claims.find(claim => claim.handle === claim_handle);
  const trustedContext = await authenticatePublisher({ ...configured, role: "worker", dispatch_id: assignment.dispatch_id, claim_handle });
  const latest = await loadQueue(configured);
  const { dispatch } = validateStoredAssignment(latest.projection, assignment, { allowReleased: true });
  const claim = latest.projection.claims.get(member.claim_id);
  const work = latest.projection.works.get(member.work_id);
  if (dispatch.released || claim?.state !== "completed" || (claim.keep_open ? claim.barrier : work?.barrier) !== "pending") return { published: false, reason: "claim_ineffective" };
  if (latest.projection.deliveryEvidence.has(member.claim_id)) return { published: false, reason: "delivery_evidence_exists" };
  const outputs = options.outputs.map(entry => (entry.verifier === "queue_control" ? { ...entry, receipt: { ...entry.receipt, request_id: requestIdForIntent(trustedContext, entry.receipt.intent_id) } } : entry));
  const operation = {
    kind: "DeliveryEvidence",
    work_id: member.work_id,
    claim_id: member.claim_id,
    dispatch_id: assignment.dispatch_id,
    claim_handle,
    completion_id: work.completion_id,
    run_id: dispatch.run.run_id,
    run_attempt: 1,
    outputs,
    effects: options.effects,
    controls_digest: digest(claimControlReceipts(latest.projection, assignment, claim_handle)),
  };
  const result = await publishOperations(configured, trustedContext, ["delivery_evidence", assignment.dispatch_id, claim_handle, work.completion_id], "delivery_evidence", [operation]);
  return { ...result, published: result.persisted === true || result.publishedNow === true };
}

async function verifyEvidenceEntry(input) {
  const entry = input.result?.entry;
  if (!entry) return { verified: false, diagnostics: { event: "delivery.durable.unsupported" } };
  const options = { ...input, durable: true, ...(entry.adapter === undefined ? {} : { adapter: parseStrictJSON(entry.adapter) }) };
  if (entry.verifier === "builtin") return verifyBuiltinDeliveryOutput(options);
  const specialized = SPECIALIZED_VERIFIERS[entry.verifier]?.verify;
  if (specialized) return specialized(options);
  if (ADAPTER_VERIFIERS.has(entry.verifier)) return verifyClaimAdapterOutput(options);
  if (entry.verifier === "queue_control") return verifyCommittedClaimQueueControl({ ...options, claim_handle: input.claim.handle, request_id: entry.receipt.request_id });
  return { verified: false, diagnostics: { event: "delivery.durable.unsupported" } };
}

module.exports = { collectDeliveryEvidence, publishDeliveryEvidence, verifyEvidenceEntry };
