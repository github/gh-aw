// @ts-check
"use strict";

const { canonicalResourceTarget, resolveRepositoryTarget, resolveParentResourceTarget } = require("./work_queue_effect_resource.cjs");

const { AsyncLocalStorage } = require("node:async_hooks");
const { normalizeAssignment, normalizeClaimScope, assertClaimAuthorized, currentClaimHandle, currentClaimAssignment, withClaimExecution, claimEffectChannelMatches, createClaimResourceVerification, withClaimResourceVerification } = require("./work_queue_claim_scope.cjs");
const { digest, canonical, canonicalBytes } = require("./work_queue_codec.cjs");
const { BUILTIN_EFFECT_FIELDS, builtinTargetNumber } = require("./work_queue_declared_verification.cjs");

const NO_WRITE_TYPES = new Set(["noop", "missing_tool", "missing_data", "report_incomplete"]);
const controlInventories = new WeakMap();
const deliveryReceipts = new WeakMap();
const deliveryAttestations = new WeakMap();
const deliveryVerifier = new AsyncLocalStorage();

function recordClaimDelivery(delivery, assignment, member, binding, inventory, options) {
  const nativeRun = inventory && controlInventories.get(inventory)?.run;
  if (
    !binding ||
    !nativeRun ||
    binding.run_id !== nativeRun.run_id ||
    binding.run_attempt !== nativeRun.run_attempt ||
    binding.run_attempt !== 1 ||
    typeof binding.run_id !== "string" ||
    !/^[1-9][0-9]{0,255}$/.test(binding.run_id) ||
    !options.requireControlInventory ||
    delivery.controls_digest !== inventory.controls_digest ||
    !claimEffectChannelMatches(options.effectChannel, options.effects || [], assignment, member.handle) ||
    (delivery.disposition === "none" && member.work.effect_contract?.kind !== "none" && member.work.effect_contract?.no_writes !== true)
  )
    return;
  const record = {
    version: 3,
    dispatch_id: assignment.dispatch_id,
    claim_id: member.claim_id,
    work_id: member.work_id,
    claim_handle: member.handle,
    assignment_digest: digest(assignment),
    contract_digest: digest(member.work.effect_contract),
    run_id: binding.run_id,
    run_attempt: 1,
    run: nativeRun,
    effects_digest: digest(options.effects || []),
    descriptor: delivery.descriptor,
    effects: delivery.disposition === "none" ? "none" : "partial",
    controls_digest: delivery.controls_digest,
  };
  deliveryReceipts.set(delivery, JSON.parse(canonical(record)));
}

function createClaimDeliveryVerifier(options) {
  const assignment = normalizeAssignment(options.assignment);
  if (typeof options.recheck !== "function") throw new Error("Claim delivery verifier requires trusted independent readback");
  return (member, verification) => deliveryVerifier.run({ ...options, assignment }, () => verifyClaimDelivery(member, verification));
}

function isTrustedClaimDelivery(delivery, assignment, claim_handle) {
  const attestation = delivery && typeof delivery === "object" && deliveryAttestations.get(delivery);
  if (!attestation || attestation.claim_handle !== claim_handle || !assignment || typeof assignment !== "object") return false;
  try {
    return attestation.delivery_digest === digest(delivery) && attestation.assignment_digest === digest(assignment);
  } catch {
    return false;
  }
}

/**
 * The two-argument lifecycle facade only accepts live, protected readback receipts.
 * The one-argument form retains the internal diagnostic settlement API.
 * @param {Record<string, any>} member
 * @param {Record<string, any>} [verification]
 * @returns {Promise<Record<string, any>>}
 */
async function verifyClaimDelivery(member, verification) {
  if (verification === undefined) return inspectClaimDelivery(member);
  const pending = { verified: false, effects: "unknown" };
  const trusted = deliveryVerifier.getStore();
  if (!trusted) return pending;
  verification.signal?.throwIfAborted();
  const assignment = normalizeAssignment(verification.assignment);
  const original = assignment.claims.find(claim => claim.handle === member.handle);
  if (
    !original ||
    canonical(assignment) !== canonical(trusted.assignment) ||
    canonical(member) !== canonical(original) ||
    verification.contract === undefined ||
    original.work.effect_contract === undefined ||
    canonical(verification.contract) !== canonical(original.work.effect_contract) ||
    !Number.isSafeInteger(verification.attempt) ||
    verification.attempt < 1 ||
    verification.attempt > 4096 ||
    verification.run?.run_attempt !== 1 ||
    typeof verification.run.run_id !== "string" ||
    !/^[1-9][0-9]{0,255}$/.test(verification.run.run_id)
  )
    return pending;
  const delivery = await trusted.recheck(original, verification);
  verification.signal?.throwIfAborted();
  const record = delivery && deliveryReceipts.get(delivery);
  if (
    !record ||
    delivery.verification !== "verified" ||
    record.assignment_digest !== digest(assignment) ||
    record.contract_digest !== digest(verification.contract) ||
    record.dispatch_id !== assignment.dispatch_id ||
    record.claim_handle !== original.handle ||
    record.claim_id !== original.claim_id ||
    record.work_id !== original.work_id ||
    record.run_id !== verification.run.run_id ||
    record.run_attempt !== verification.run.run_attempt ||
    canonical(record.run) !== canonical(verification.run)
  )
    return pending;
  const proof = {
    verified: true,
    contractVerified: true,
    receipt: digest(record),
    descriptor: JSON.parse(canonical(record.descriptor)),
    effects: record.effects,
    controls_digest: record.controls_digest,
  };
  deliveryAttestations.set(proof, Object.freeze({ delivery_digest: digest(proof), assignment_digest: digest(assignment), claim_handle: original.handle }));
  return proof;
}

async function readDeliveryControlInventory(options) {
  options.signal?.throwIfAborted();
  const assignment = normalizeAssignment(options.assignment);
  const member = assignment.claims.find(claim => claim.handle === options.claim_handle);
  if (!member) throw new Error("Delivery queue-control inventory requires an original Claim");
  const controls = require("./work_queue_control_receipts.cjs");
  const inventory = await controls.readClaimQueueControls({ ...options, assignment });
  options.signal?.throwIfAborted();
  if (typeof inventory?.controls_digest !== "string" || !/^[0-9a-f]{64}$/.test(inventory.controls_digest)) throw new Error("Independent queue-control inventory requires its exact readback digest, including zero controls");
  controlInventories.set(inventory, { assignment: canonical(assignment), claim_handle: member.handle, run: controls.claimQueueControlRun(inventory) });
  return inventory;
}

function validateDeliveryContract(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Missing or unsupported immutable Work effect_contract");
  canonical(value);
  if (value?.kind === "none" && Object.keys(value).length === 1) {
    return { version: 1, outputs: [], no_writes: true };
  }
  if (!value || typeof value !== "object" || Array.isArray(value) || value.version !== 1 || !Array.isArray(value.outputs) || value.outputs.length > 128 || Object.keys(value).some(key => !["version", "outputs", "no_writes"].includes(key))) {
    throw new Error("Missing or unsupported immutable Work effect_contract");
  }
  const types = new Set();
  for (const output of value.outputs) {
    if (
      !output ||
      typeof output.type !== "string" ||
      !output.type ||
      types.has(output.type) ||
      !Number.isSafeInteger(output.min) ||
      !Number.isSafeInteger(output.max) ||
      output.min < 0 ||
      output.max < output.min ||
      output.max > 128 ||
      Object.keys(output).some(key => !["type", "min", "max", "verification"].includes(key))
    ) {
      throw new Error("Invalid bounded effect_contract");
    }
    if (Object.hasOwn(output, "verification")) {
      const intent = output.verification;
      if (
        !intent ||
        typeof intent !== "object" ||
        Array.isArray(intent) ||
        Object.keys(intent).some(key => !["verifier_id", "expected"].includes(key)) ||
        typeof intent.verifier_id !== "string" ||
        intent.verifier_id.length > 128 ||
        !/^[A-Za-z0-9][A-Za-z0-9_.:-]*$/.test(intent.verifier_id) ||
        /[^A-Za-z0-9_.:-]/.test(intent.verifier_id) ||
        !intent.expected ||
        typeof intent.expected !== "object" ||
        Array.isArray(intent.expected)
      )
        throw new Error("Invalid declarative effect_contract verification intent");
    }
    types.add(output.type);
  }
  if (value.no_writes !== undefined && typeof value.no_writes !== "boolean") throw new Error("effect_contract.no_writes must be boolean");
  if (value.outputs.length === 0 && value.no_writes !== true) throw new Error("Empty output contracts must explicitly declare no_writes");
  if (value.no_writes === true && value.outputs.length !== 0) throw new Error("A no-write contract cannot declare resource outputs");
  return value;
}

/**
 * Delivery is a separate barrier from Completion. Delegation, job success and
 * a manifest entry alone never prove that the immutable output contract was met.
 * @param {Record<string, any>} options
 */
async function inspectClaimDelivery(options) {
  options.signal?.throwIfAborted();
  const { claim_handle, messages = [], results = [], verifyOutput } = options;
  let binding;
  const authorize = async request => {
    const proof = await (options.authorize || require("./finish_work_queue_claim.cjs").authorizeWorkerClaim)(request);
    if (proof?.authorized === true && proof.run_id !== undefined) {
      const current = { run_id: proof.run_id, run_attempt: proof.run_attempt };
      if (binding && canonical(binding) !== canonical(current)) throw new Error("Delivery authority changed its original native run binding");
      binding = current;
    }
    return proof;
  };
  const assignment = normalizeAssignment(options.assignment);
  if (!currentClaimHandle()) {
    return withClaimExecution({ assignment, claim_handle, authorize }, () => inspectClaimDelivery(options));
  }
  if (canonical(currentClaimAssignment()) !== canonical(assignment)) throw new Error("Delivery cannot replace its original immutable assignment");
  if (currentClaimHandle() !== claim_handle) throw new Error("Delivery cannot escape its trusted Claim execution context");
  const claim = assignment.claims.find(member => member.handle === claim_handle);
  if (!claim) throw new Error("Delivery handle is foreign to the immutable assignment");
  const receipt = { version: 3, claim_handle, dispatch_id: assignment.dispatch_id, work_id: claim.work_id, claim_id: claim.claim_id, verification: "unknown", disposition: "unknown", descriptor: null };
  const normalized = messages.map(message => normalizeClaimScope(message, assignment));
  if (normalized.some(message => message.claim_handle !== claim_handle)) throw new Error("Delivery cannot consume another Claim's messages");
  try {
    await assertClaimAuthorized({ type: "work_queue_result", claim_handle }, { authorize, context: options.context, github: options.github, requireCompletion: !options.staged });
  } catch (error) {
    if (error.suppressed && error.state === "cancelled") return { ...receipt, verification: "cancelled", disposition: "none" };
    if (error.suppressed && error.state === "result") return { ...receipt, verification: "result", reason: "Durable Result already settled; effects were not replayed" };
    return { ...receipt, reason: error.message };
  }
  options.signal?.throwIfAborted();
  if (options.staged) return { ...receipt, verification: "staged_preview" };
  let controlInventory;
  if (options.readControlInventory) {
    try {
      controlInventory = await options.readControlInventory();
      options.signal?.throwIfAborted();
      const provenance = controlInventories.get(controlInventory);
      if (!provenance || provenance.assignment !== canonical(assignment) || provenance.claim_handle !== claim_handle) return { ...receipt, reason: "Independent durable queue-control inventory is unavailable or untrusted" };
    } catch (error) {
      return { ...receipt, reason: `Independent queue-control readback failed: ${error.message}` };
    }
  } else if (options.requireControlInventory) {
    return { ...receipt, reason: "Independent durable queue-control inventory is required" };
  }
  let contract;
  try {
    contract = validateDeliveryContract(claim.work.effect_contract);
  } catch (error) {
    return { ...receipt, reason: error.message };
  }
  const known = new Set(contract.outputs.map(output => output.type));
  if (normalized.some(message => !known.has(message.type) && !(contract.no_writes === true && NO_WRITE_TYPES.has(message.type)))) {
    return { ...receipt, reason: "Undeclared output type in immutable effect_contract" };
  }
  for (const expected of contract.outputs) {
    const count = normalized.filter(message => message.type === expected.type).length;
    if (count < expected.min || count > expected.max) return { ...receipt, reason: "Immutable output cardinality contract was not met" };
  }
  if (contract.no_writes === true && normalized.some(message => !NO_WRITE_TYPES.has(message.type))) {
    return { ...receipt, reason: "A no-write contract cannot deliver resource-writing outputs" };
  }
  if (contract.no_writes === true && options.effects?.length) return { ...receipt, reason: "A no-write contract has attempted undeclared resource effects" };
  const outputs = [];
  const verifiedResources = [];
  const verifiedControls = new Set();
  for (let index = 0; index < normalized.length; index++) {
    options.signal?.throwIfAborted();
    const message = normalized[index];
    const outcome = results.find(result => result.messageIndex === index);
    if (!outcome || !outcome.success || outcome.delegated || outcome.deferred || outcome.skipped || outcome.cancelled) {
      return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: "Missing exact nondelegated delivery receipt" };
    }
    if (outcome.claim_handle && outcome.claim_handle !== claim_handle) throw new Error("Foreign Claim delivery receipt");
    const verification = contract.outputs.find(expected => expected.type === message.type)?.verification;
    const verify = verification === undefined ? verifyOutput : options.verifyDeclaredOutput;
    if (verification !== undefined && (NO_WRITE_TYPES.has(message.type) || !require("./work_queue_claim_adapters.cjs").isDeclaredAdapterVerifier(verify, message.type, verification)))
      return { ...receipt, reason: "Declared immutable verification intent requires a supported trusted verifier binding" };
    if (NO_WRITE_TYPES.has(message.type)) {
      outputs.push({ type: message.type, effect: "none" });
      continue;
    }
    if (typeof verify !== "function") return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: "Independent delivery verifier unavailable" };
    await assertClaimAuthorized(message, { authorize, context: options.context, github: options.github });
    const proof = await verify({ claim, message, result: outcome.result, context: options.context, github: options.github, signal: options.signal, ...(verification === undefined ? {} : { verification }) });
    options.signal?.throwIfAborted();
    if (!proof || proof.verified !== true || proof.claim_handle !== claim_handle || !proof.resource || typeof proof.resource !== "object" || Array.isArray(proof.resource) || !proof.evidence) {
      return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: "Independent scoped resource verification failed" };
    }
    if (proof.effect_resources !== undefined && !Array.isArray(proof.effect_resources)) return { ...receipt, reason: "Independent effect metadata requires a complete resource array" };
    if (["work_queue_submit", "work_queue_dispatch_next"].includes(message.type)) {
      const control = controlInventory?.controls.find(entry => entry.request_id === proof.evidence.request_id);
      if (
        !control ||
        canonical(control) !== canonical(proof.evidence) ||
        control.completion_id !== controlInventory.completion_id ||
        control.type !== message.type ||
        proof.resource.kind !== "queue_commit" ||
        proof.resource.id !== control.commit_id ||
        proof.resource.repository !== controlInventories.get(controlInventory)?.run.repository ||
        Object.keys(proof.resource).some(field => !["kind", "repository", "id"].includes(field)) ||
        (proof.effect_resources !== undefined && (!Array.isArray(proof.effect_resources) || proof.effect_resources.length !== 0)) ||
        verifiedControls.has(control.request_id)
      )
        return { ...receipt, reason: "Queue-control delivery has no exact independent completed-Claim ledger receipt" };
      const checked = await require("./work_queue_control_receipts.cjs").verifyClaimQueueControl({ assignment, claim_handle, message, inventory: controlInventory });
      if (checked.verified !== true || checked.claim_handle !== claim_handle || canonical(checked.resource) !== canonical(proof.resource) || canonical(checked.evidence) !== canonical(proof.evidence)) {
        return { ...receipt, reason: "Queue-control delivery does not match its independently checked original intent" };
      }
      verifiedControls.add(control.request_id);
    }
    if (!["work_queue_submit", "work_queue_dispatch_next"].includes(message.type)) {
      try {
        await withClaimResourceVerification(proof, async () => {
          for (const resource of proof.authority_resources || [proof.authority_resource]) {
            await assertClaimAuthorized(
              {
                ...message,
                ...(resource.repository ? { repo: resource.repository } : {}),
                ...(proof.resource.number ? { item_number: proof.resource.number } : {}),
              },
              { authorize, context: options.context, github: options.github, resource: canonicalResourceTarget(resource) }
            );
          }
        });
      } catch (error) {
        const reason = error instanceof Error ? error.message : "unavailable protected authority receipt";
        return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: `Independent scoped resource authorization failed: ${reason}` };
      }
    }
    outputs.push({ type: message.type, resource: proof.resource, evidence: proof.evidence });
    verifiedResources.push(proof.resource, ...(proof.effect_resources || []));
  }
  const descriptor = { version: 1, outputs };
  if (controlInventory?.controls.some(control => !verifiedControls.has(control.request_id))) return { ...receipt, reason: "An undeclared durable queue-control effect is outside the complete output contract" };
  for (const effect of options.effects || []) {
    const covered =
      effect.claim_handle === claim_handle &&
      effect.claim_id === claim.claim_id &&
      effect.work_id === claim.work_id &&
      effect.dispatch_id === assignment.dispatch_id &&
      effect.outcome === "succeeded" &&
      verifiedResources.some(resource => {
        return resource && effect.kind === resource.kind && effect.repository === resource.repository && (effect.id ? effect.id === resource.id : Number.isSafeInteger(effect.number) && effect.number === resource.number);
      });
    if (!covered) return { ...receipt, disposition: "unknown", reason: "An auxiliary or ambiguous effect has no independently verified declared resource receipt" };
  }
  if (canonicalBytes(descriptor) > 4 * 1024) return { ...receipt, reason: "Verified Result descriptor exceeds the supported byte ceiling" };
  const delivery = {
    ...receipt,
    verification: "verified",
    disposition: outputs.some(output => output.effect !== "none") ? "complete" : "none",
    descriptor,
    ...(controlInventory ? { controls_digest: controlInventory.controls_digest } : {}),
  };
  options.signal?.throwIfAborted();
  recordClaimDelivery(delivery, assignment, claim, binding, controlInventory, options);
  return delivery;
}

/** @param {Record<string, any>} options */
async function verifyBuiltinDeliveryOutput(options) {
  const { claim, message, result, github } = options;
  // PR creation/branch updates also need pinned commit/tree evidence, not merely a visible PR.
  if (["create_pull_request", "update_pull_request"].includes(message.type)) return { verified: false };
  const verifiedFields = new Set(["type", "claim_handle", "claim_id", "work_id", "repo", "item_number", "issue_number", "pull_request_number", "temporary_id", ...(BUILTIN_EFFECT_FIELDS[message.type] || [])]);
  if (Object.keys(message).some(field => !verifiedFields.has(field))) return { verified: false };
  if (!result || result.staged || !github?.rest || message.data !== undefined || message.duplicate_of !== undefined) return { verified: false };
  const repository = result.repo || message.repo || options.config?.["target-repo"] || options.context?.payload?.repository?.full_name;
  const pieces = typeof repository === "string" ? repository.split("/") : [];
  if (pieces.length !== 2) return { verified: false };
  await assertClaimAuthorized({ ...message, repo: repository }, { authorize: options.authorize, context: options.context, github });
  const [owner, repo] = pieces;
  const requestedNumber = builtinTargetNumber(message);
  const number = Number(result.number || result.issue_number || result.pull_request_number || message.item_number || message.issue_number || message.pull_request_number);
  if (requestedNumber !== undefined && requestedNumber !== String(number)) return { verified: false };
  let data;
  let kind;
  if (["create_issue", "update_issue", "close_issue", "add_labels", "remove_labels", "replace_label"].includes(message.type) && Number.isSafeInteger(number) && number > 0) {
    ({ data } = await github.rest.issues.get({ owner, repo, issue_number: number }));
    kind = "issue";
  } else if (["close_pull_request", "merge_pull_request", "mark_pull_request_as_ready_for_review"].includes(message.type) && Number.isSafeInteger(number) && number > 0) {
    ({ data } = await github.rest.pulls.get({ owner, repo, pull_number: number }));
    kind = "pull_request";
  } else if (message.type === "add_comment" && Number.isSafeInteger(Number(result.comment_id || result.commentId || result.id))) {
    if (!Number.isSafeInteger(number) || number < 1) return { verified: false };
    ({ data } = await github.rest.issues.getComment({ owner, repo, comment_id: Number(result.comment_id || result.commentId || result.id) }));
    const api = (process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "");
    if (data?.issue_url !== `${api}/repos/${repository}/issues/${number}`) return { verified: false };
    kind = "comment";
  } else {
    return { verified: false };
  }
  if (!data || !data.id || !data.html_url || (data.number !== undefined && data.number !== number)) return { verified: false };
  if (typeof data.id === "number" ? !Number.isSafeInteger(data.id) || data.id < 1 : typeof data.id !== "string" || !/^[1-9][0-9]{0,255}$/.test(data.id)) return { verified: false };
  const expected = {};
  for (const effect of options.effects || []) {
    if (effect.claim_handle === claim.handle && effect.outcome === "succeeded" && effect.kind === kind && effect.repository === repository && (effect.id ? effect.id === String(data.id) : effect.number === number)) {
      Object.assign(expected, effect.expected || {});
    }
  }
  for (const field of ["title", "body"]) {
    const value = Object.prototype.hasOwnProperty.call(expected, field) ? expected[field] : message[field];
    if (value !== undefined && data[field] !== value) return { verified: false };
  }
  for (const field of ["state", "state_reason"]) if (expected[field] !== undefined && data[field] !== expected[field]) return { verified: false };
  if (expected.milestone !== undefined && (expected.milestone === null ? data.milestone != null : data.milestone?.number !== Number(expected.milestone))) return { verified: false };
  if ((message.state || message.status) && data.state !== (message.state || message.status)) return { verified: false };
  if (message.state_reason && data.state_reason !== message.state_reason) return { verified: false };
  if (message.type.startsWith("close_") && data.state !== "closed") return { verified: false };
  if (message.type === "merge_pull_request" && data.merged !== true) return { verified: false };
  if (message.type === "mark_pull_request_as_ready_for_review" && data.draft !== false) return { verified: false };
  if (["add_labels", "remove_labels"].includes(message.type)) {
    const actual = new Set((data.labels || []).map(label => (typeof label === "string" ? label : label.name).toLowerCase()));
    for (const label of message.labels || []) {
      const name = (typeof label === "string" ? label : label.name).toLowerCase();
      if (actual.has(name) !== (message.type === "add_labels")) return { verified: false };
    }
  }
  const names = values => values.map(value => (typeof value === "string" ? value : value.name || value.login).toLowerCase()).sort();
  const desiredLabels = Object.hasOwn(expected, "labels") ? expected.labels : message.labels;
  if (desiredLabels !== undefined && ["create_issue", "update_issue"].includes(message.type)) {
    const desired = names(desiredLabels);
    const actual = names(data.labels || []);
    const exact = Object.hasOwn(expected, "labels") || options.verification !== undefined || message.type === "update_issue";
    if (!desired.every(value => actual.includes(value)) || (exact && JSON.stringify(desired) !== JSON.stringify(actual))) return { verified: false };
  }
  const desiredAssignees = Object.hasOwn(expected, "assignees") ? expected.assignees : message.assignees;
  if (desiredAssignees !== undefined) {
    const desired = names(desiredAssignees);
    const actual = names(data.assignees || []);
    const exact = Object.hasOwn(expected, "assignees") || options.verification !== undefined || message.type === "update_issue";
    if (!desired.every(value => actual.includes(value)) || (exact && JSON.stringify(desired) !== JSON.stringify(actual))) return { verified: false };
  }
  if (message.milestone !== undefined) {
    if (message.milestone === null ? data.milestone != null : !Number.isSafeInteger(Number(message.milestone)) || data.milestone?.number !== Number(message.milestone)) return { verified: false };
  }
  if (message.type === "replace_label") {
    const actual = new Set(names(data.labels || []));
    if (message.label_to_remove && actual.has(String(message.label_to_remove).trim().toLowerCase())) return { verified: false };
    if (message.label_to_add && !actual.has(String(message.label_to_add).trim().toLowerCase())) return { verified: false };
  }
  const resource = { kind, repository, number: data.number || number || null, id: String(data.id), url: data.html_url };
  let authorityResource = ["issue", "pull_request"].includes(kind) ? await resolveParentResourceTarget(github, { repository, kind, number: resource.number }, data) : await resolveRepositoryTarget(github, resource);
  if (kind === "comment") {
    if (typeof github.rest.issues.get !== "function") return { verified: false };
    const { data: parent } = await github.rest.issues.get({ owner, repo, issue_number: number });
    if (!parent || parent.number !== number) return { verified: false };
    authorityResource = { ...authorityResource, ...(await resolveParentResourceTarget(github, { repository, kind: "issue", number }, parent)) };
  }
  const observed = {
    id: resource.id,
    number: resource.number,
    title: data.title || null,
    body: data.body || null,
    state: data.state || null,
    merged: data.merged === true,
    draft: data.draft === true,
    labels: (data.labels || []).map(label => (typeof label === "string" ? label : label.name)).sort(),
    assignees: (data.assignees || []).map(user => user.login).sort(),
    milestone: data.milestone?.number ?? null,
    state_reason: data.state_reason ?? null,
  };
  const proof = {
    verified: true,
    claim_handle: claim.handle,
    resource,
    authority_resource: authorityResource,
    evidence: { source: "github_api", observed_at: Date.now(), digest: digest(observed) },
  };
  return currentClaimHandle() ? createClaimResourceVerification(proof) : proof;
}

module.exports = { NO_WRITE_TYPES, validateDeliveryContract, readDeliveryControlInventory, inspectClaimDelivery, createClaimDeliveryVerifier, verifyClaimDelivery, isTrustedClaimDelivery, verifyBuiltinDeliveryOutput };
