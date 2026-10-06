// @ts-check
"use strict";

const { normalizeAssignment, normalizeClaimScope, assertClaimAuthorized, currentClaimHandle, currentClaimAssignment, withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { digest, canonical, canonicalBytes } = require("./work_queue_codec.cjs");

const NO_WRITE_TYPES = new Set(["noop", "missing_tool", "missing_data", "report_incomplete"]);
const controlInventories = new WeakMap();

async function readDeliveryControlInventory(options) {
  const assignment = normalizeAssignment(options.assignment);
  const member = assignment.claims.find(claim => claim.handle === options.claim_handle);
  if (!member) throw new Error("Delivery queue-control inventory requires an original Claim");
  const inventory = await require("./work_queue_control_receipts.cjs").readClaimQueueControls({ ...options, assignment });
  controlInventories.set(inventory, { assignment: canonical(assignment), claim_handle: member.handle });
  return inventory;
}

function validateDeliveryContract(value) {
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
      Object.keys(output).some(key => !["type", "min", "max"].includes(key))
    ) {
      throw new Error("Invalid bounded effect_contract");
    }
    types.add(output.type);
  }
  if (value.no_writes !== undefined && typeof value.no_writes !== "boolean") throw new Error("effect_contract.no_writes must be boolean");
  if (value.outputs.length === 0 && value.no_writes !== true) throw new Error("Empty output contracts must explicitly declare no_writes");
  return value;
}

/**
 * Delivery is a separate barrier from Completion. Delegation, job success and
 * a manifest entry alone never prove that the immutable output contract was met.
 * @param {Record<string, any>} options
 */
async function verifyClaimDelivery(options) {
  const { claim_handle, messages = [], results = [], verifyOutput, authorize } = options;
  const assignment = normalizeAssignment(options.assignment);
  if (!currentClaimHandle()) {
    return withClaimExecution({ assignment, claim_handle, authorize }, () => verifyClaimDelivery(options));
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
  if (options.staged) return { ...receipt, verification: "staged_preview" };
  let controlInventory;
  if (options.readControlInventory) {
    try {
      controlInventory = await options.readControlInventory();
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
    const message = normalized[index];
    const outcome = results.find(result => result.messageIndex === index);
    if (!outcome || !outcome.success || outcome.delegated || outcome.deferred || outcome.skipped || outcome.cancelled) {
      return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: "Missing exact nondelegated delivery receipt" };
    }
    if (outcome.claim_handle && outcome.claim_handle !== claim_handle) throw new Error("Foreign Claim delivery receipt");
    if (NO_WRITE_TYPES.has(message.type)) {
      outputs.push({ type: message.type, effect: "none" });
      continue;
    }
    if (typeof verifyOutput !== "function") return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: "Independent delivery verifier unavailable" };
    await assertClaimAuthorized(message, { authorize, context: options.context, github: options.github });
    const proof = await verifyOutput({ claim, message, result: outcome.result, context: options.context, github: options.github });
    if (!proof || proof.verified !== true || proof.claim_handle !== claim_handle || !proof.resource || !proof.evidence) {
      return { ...receipt, disposition: outputs.length ? "partial" : "unknown", reason: "Independent scoped resource verification failed" };
    }
    if (["work_queue_submit", "work_queue_dispatch_next"].includes(message.type)) {
      const control = controlInventory?.controls.find(entry => entry.request_id === proof.evidence.request_id);
      if (
        !control ||
        canonical(control) !== canonical(proof.evidence) ||
        control.completion_id !== controlInventory.completion_id ||
        control.type !== message.type ||
        proof.resource.kind !== "queue_commit" ||
        proof.resource.id !== control.commit_id ||
        verifiedControls.has(control.request_id)
      )
        return { ...receipt, reason: "Queue-control delivery has no exact independent completed-Claim ledger receipt" };
      verifiedControls.add(control.request_id);
    }
    await assertClaimAuthorized(
      {
        ...message,
        ...(proof.resource.repository ? { repo: proof.resource.repository } : {}),
        ...(proof.resource.number ? { item_number: proof.resource.number } : {}),
      },
      { authorize, context: options.context, github: options.github, resource: proof.resource }
    );
    for (const resource of proof.effect_resources || []) {
      await assertClaimAuthorized({ ...message, ...(resource.repository ? { repo: resource.repository } : {}) }, { authorize, context: options.context, github: options.github, resource });
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
  return { ...receipt, verification: "verified", disposition: outputs.some(output => output.effect !== "none") ? "complete" : "none", descriptor, ...(controlInventory ? { controls_digest: controlInventory.controls_digest } : {}) };
}

/** @param {Record<string, any>} options */
async function verifyBuiltinDeliveryOutput(options) {
  const { claim, message, result, github } = options;
  // PR creation/branch updates also need pinned commit/tree evidence, not merely a visible PR.
  if (["create_pull_request", "update_pull_request"].includes(message.type)) return { verified: false };
  const effectFields = {
    create_issue: ["title", "body", "labels", "assignees", "milestone"],
    update_issue: ["title", "body", "labels", "assignees", "milestone", "state", "status"],
    close_issue: ["state_reason"],
    add_comment: ["body"],
    add_labels: ["labels"],
    remove_labels: ["labels"],
    replace_label: ["label_to_add", "label_to_remove"],
  };
  const verifiedFields = new Set(["type", "claim_handle", "claim_id", "work_id", "repo", "item_number", "issue_number", "pull_request_number", "temporary_id", ...(effectFields[message.type] || [])]);
  if (Object.keys(message).some(field => !verifiedFields.has(field))) return { verified: false };
  if (!result || result.staged || !github?.rest || message.data !== undefined || message.duplicate_of !== undefined) return { verified: false };
  const repository = result.repo || message.repo || options.config?.["target-repo"] || options.context?.payload?.repository?.full_name;
  const pieces = typeof repository === "string" ? repository.split("/") : [];
  if (pieces.length !== 2) return { verified: false };
  const [owner, repo] = pieces;
  const number = Number(result.number || result.issue_number || result.pull_request_number || message.item_number || message.issue_number || message.pull_request_number);
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
  if (message.labels !== undefined && ["create_issue", "update_issue"].includes(message.type)) {
    const expected = names(message.labels);
    const actual = names(data.labels || []);
    if (!expected.every(value => actual.includes(value)) || (message.type === "update_issue" && JSON.stringify(expected) !== JSON.stringify(actual))) return { verified: false };
  }
  if (message.assignees !== undefined) {
    const expected = names(message.assignees);
    const actual = names(data.assignees || []);
    if (!expected.every(value => actual.includes(value)) || (message.type === "update_issue" && JSON.stringify(expected) !== JSON.stringify(actual))) return { verified: false };
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
  return {
    verified: true,
    claim_handle: claim.handle,
    resource,
    evidence: { source: "github_api", observed_at: Date.now(), digest: digest(observed) },
  };
}

module.exports = { NO_WRITE_TYPES, validateDeliveryContract, readDeliveryControlInventory, verifyClaimDelivery, verifyBuiltinDeliveryOutput };
