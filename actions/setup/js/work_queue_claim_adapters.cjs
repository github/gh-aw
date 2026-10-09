// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { closed, parseStrictJSON, canonical, digest } = require("./work_queue_codec.cjs");
const {
  currentClaimHandle,
  currentClaimAssignment,
  normalizeRuntimeMessage,
  assertClaimAuthorized,
  claimArtifactPath,
  assertClaimArtifactFile,
  claimIdentity,
  assertClaimIdentity,
  receiptMatchesClaim,
  createClaimResourceVerification,
} = require("./work_queue_claim_scope.cjs");
const { verifyBuiltinDeliveryOutput } = require("./work_queue_delivery.cjs");
const { validateRestAdapter, createRestEffectHandler, verifyRestAdapterDelivery } = require("./work_queue_rest_adapter.cjs");
const { validateGitTreeAdapter, createGitTreeEffectHandler, verifyGitTreeDelivery } = require("./work_queue_git_tree_adapter.cjs");
const { validateGraphqlAdapter, createGraphqlEffectHandler, verifyGraphqlAdapterDelivery } = require("./work_queue_graphql_adapter.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { builtinAdapterFields, builtinTargetNumber, adapterVerifierId, adapterEffectFields, matchesDeclaredAdapterExpected } = require("./work_queue_declared_verification.cjs");

const ADAPTER_EFFECT_TYPES = new Set(["create_issue", "update_issue", "close_issue", "add_comment", "add_labels", "remove_labels", "replace_label", "github_rest", "git_tree", "github_graphql"]);
const EFFECT_FIELDS = new Set(["title", "body", "labels", "assignees", "milestone", "state", "status", "state_reason", "item_number", "issue_number", "pull_request_number", "label_to_add", "label_to_remove"]);
const privateReceipts = new WeakMap();
const builtinReceipts = new WeakMap();
const declaredVerifiers = new WeakMap();
const DECLARED_EFFECT_TYPES = new Set(ADAPTER_EFFECT_TYPES);

function builtinMessageFields(message) {
  const fields = builtinAdapterFields(message.type);
  if (!fields) throw new Error("E001: Unsupported native builtin Claim verifier");
  const metadata = new Set(["type", "claim_handle", "claim_id", "work_id", "repo", "temporary_id"]);
  if (Object.keys(message).some(field => !metadata.has(field) && !fields.includes(field))) throw new Error("Native builtin Claim verifier cannot ignore undeclared message fields");
  builtinTargetNumber(message);
  return Object.fromEntries(fields.filter(field => Object.hasOwn(message, field)).map(field => [field, structuredClone(message[field])]));
}

function matchesBuiltinExpected(message, verification) {
  closed(verification, ["verifier_id", "expected"], [], "declared native builtin verification");
  const fields = builtinMessageFields(message);
  closed(verification.expected, Object.keys(fields), [], "declared native builtin expected fields");
  return verification.verifier_id === message.type && canonical(verification.expected) === canonical(fields);
}

function wrapDeclaredBuiltinHandler(type, handler) {
  if (!builtinAdapterFields(type)) return handler;
  const identity = claimIdentity(currentClaimHandle());
  const wrapped = async (message, ...args) => {
    assertClaimIdentity(identity);
    const original = normalizeRuntimeMessage(message);
    if (original.type !== type) throw new Error("Native builtin Claim handler type conflicts with its trusted binding");
    const member = currentClaimAssignment().claims.find(claim => claim.handle === original.claim_handle);
    if (!member) throw new Error("Native builtin Claim handler lost its immutable assignment member");
    const verification = member.work.effect_contract?.outputs?.find(output => output.type === type)?.verification;
    if (verification !== undefined && !matchesBuiltinExpected(original, verification)) throw new Error("Native builtin Claim effect differs from its immutable declared verification expectation");
    const receiptMessage = structuredClone(original);
    const result = await handler(original, ...args);
    if (verification !== undefined) {
      if (!result || typeof result !== "object" || Array.isArray(result) || builtinReceipts.has(result)) throw new Error("Native builtin Claim handler requires a unique private effect receipt");
      builtinReceipts.set(result, { ...identity, message: receiptMessage });
    }
    return result;
  };
  return Object.assign(wrapped, handler);
}

function createDeclaredAdapterVerifier(adapters = {}, nativeConfig = {}, effects = []) {
  const registry = new Map(Object.entries(structuredClone(adapters)).map(([type, adapter]) => [type, validateAdapter(adapter)]));
  for (const type of Object.keys(nativeConfig)) {
    if (!registry.has(type) && nativeConfig[type] && builtinAdapterFields(type)) registry.set(type, { "effect-type": type, "verifier-id": type, native: true });
  }
  const ids = new Set();
  for (const [type, adapter] of registry) {
    if (!DECLARED_EFFECT_TYPES.has(adapter["effect-type"])) continue;
    const id = adapterVerifierId(adapter, type);
    if (ids.has(id)) throw new Error("Trusted Claim adapter verifier IDs must be unique");
    ids.add(id);
  }
  const verify = async options => {
    const { message, verification } = options;
    const adapter = registry.get(message.type);
    if (!adapter || !DECLARED_EFFECT_TYPES.has(adapter["effect-type"]) || verification?.verifier_id !== adapterVerifierId(adapter, message.type)) return { verified: false };
    let proof;
    if (adapter.native) {
      const receipt = options.result && builtinReceipts.get(options.result);
      if (
        !receiptMatchesClaim(receipt, options.claim) ||
        receipt.message.type !== message.type ||
        (Object.hasOwn(message, "repo") && message.repo !== receipt.message.repo) ||
        !matchesBuiltinExpected(message, verification) ||
        !matchesBuiltinExpected(receipt.message, verification)
      )
        return { verified: false };
      proof = await verifyBuiltinDeliveryOutput({ ...options, message: receipt.message, effects });
    } else proof = await verifyClaimAdapterOutput({ ...options, adapter });
    if (proof.verified !== true) return proof;
    return createClaimResourceVerification({ ...proof, evidence: { ...proof.evidence, verifier_id: verification.verifier_id, expected_digest: digest(verification.expected) } });
  };
  declaredVerifiers.set(verify, registry);
  return verify;
}

function isDeclaredAdapterVerifier(verify, type, verification) {
  if (typeof verify !== "function") return false;
  const adapter = declaredVerifiers.get(verify)?.get(type);
  return !!adapter && DECLARED_EFFECT_TYPES.has(adapter["effect-type"]) && verification?.verifier_id === adapterVerifierId(adapter, type);
}

function validateAdapter(adapter) {
  closed(adapter, ["mode", "effect-type", "target-repo"], ["field-map", "expected", "request", "verifier", "git-tree", "graphql", "verifier-id"], "trusted Claim adapter");
  if (!["prepared", "script"].includes(adapter.mode) || !ADAPTER_EFFECT_TYPES.has(adapter["effect-type"])) throw new Error("Unsupported trusted Claim adapter effect");
  if (Object.hasOwn(adapter, "verifier-id") && (!DECLARED_EFFECT_TYPES.has(adapter["effect-type"]) || typeof adapter["verifier-id"] !== "string" || !/^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(adapter["verifier-id"])))
    throw new Error("Trusted Claim adapter verifier ID requires a bounded independently verified native effect binding");
  if (typeof adapter["target-repo"] !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(adapter["target-repo"])) throw new Error("Trusted Claim adapter requires a fixed repository scope");
  const allowedFields = builtinAdapterFields(adapter["effect-type"]);
  for (const fields of [adapter["field-map"] || {}, adapter.expected || {}]) {
    if (!fields || typeof fields !== "object" || Array.isArray(fields) || (allowedFields && Object.keys(fields).some(field => !allowedFields.includes(field)))) throw new Error("Unknown trusted Claim adapter effect field");
  }
  if (Object.values(adapter["field-map"] || {}).some(field => typeof field !== "string" || !/^[A-Za-z_][A-Za-z_0-9]*$/.test(field))) throw new Error("Trusted Claim adapter field mappings must be explicit message fields");
  if (Object.keys(adapter["field-map"] || {}).some(field => Object.hasOwn(adapter.expected || {}, field))) throw new Error("Trusted Claim adapter cannot map and fix the same effect field");
  if (adapter["effect-type"] === "github_rest") validateRestAdapter(adapter);
  else if (adapter.request !== undefined || adapter.verifier !== undefined) throw new Error("REST verifier configuration requires github_rest effect-type");
  if (adapter["effect-type"] === "git_tree") validateGitTreeAdapter(adapter);
  else if (adapter["git-tree"] !== undefined) throw new Error("Code verifier configuration requires git_tree effect-type");
  if (adapter["effect-type"] === "github_graphql") validateGraphqlAdapter(adapter);
  else if (adapter.graphql !== undefined) throw new Error("GraphQL verifier configuration requires github_graphql effect-type");
  return adapter;
}

function preparedAdapterPath(base, handle, type, assignment) {
  return path.join(claimArtifactPath(base, handle, assignment), "adapters", crypto.createHash("sha256").update(type).digest("hex") + ".json");
}

function projectAdapterMessage(message, adapter, payload = message) {
  validateAdapter(adapter);
  const normalized = normalizeRuntimeMessage(message);
  const projected = { type: adapter["effect-type"], claim_handle: normalized.claim_handle, repo: adapter["target-repo"] };
  for (const [destination, source] of Object.entries(adapter["field-map"] || {})) {
    if (!Object.hasOwn(payload, source)) throw new Error(`Prepared Claim adapter is missing mapped field ${source}`);
    projected[destination] = payload[source];
  }
  Object.assign(projected, adapter.expected || {});
  if (builtinAdapterFields(adapter["effect-type"])) builtinTargetNumber(projected);
  return normalizeRuntimeMessage(projected);
}

function loadPreparedPayload(message, adapter, filename) {
  assertClaimArtifactFile(filename, path.dirname(path.dirname(filename)));
  const stat = fs.lstatSync(filename);
  if (!stat.isFile() || stat.nlink !== 1 || stat.size > 1024 * 1024) throw new Error("Prepared Claim adapter artifact is not a bounded regular file");
  const fd = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  let prepared;
  try {
    const opened = fs.fstatSync(fd);
    if (!opened.isFile() || opened.ino !== stat.ino || opened.dev !== stat.dev || opened.size > 1024 * 1024) throw new Error("Prepared Claim adapter artifact changed during ingestion");
    prepared = parseStrictJSON(fs.readFileSync(fd, "utf8"));
  } finally {
    fs.closeSync(fd);
  }
  closed(prepared, ["version", "claim_handle", "type", "messages"], [], "prepared Claim adapter artifact");
  if (prepared.version !== 3 || prepared.claim_handle !== currentClaimHandle() || prepared.type !== message.type || !Array.isArray(prepared.messages) || prepared.messages.length > 128)
    throw new Error("Prepared Claim adapter artifact has foreign immutable attribution");
  const matches = prepared.messages.filter(entry => {
    closed(entry, ["input", "payload"], [], "prepared Claim adapter message");
    if (!entry.payload || typeof entry.payload !== "object" || Array.isArray(entry.payload)) throw new Error("Prepared Claim adapter payload must be an object");
    return canonical(entry.input) === canonical(message);
  });
  if (matches.length !== 1) throw new Error("Prepared Claim adapter requires one exact original scoped input");
  const payload = matches[0].payload;
  const allowed = new Set(Object.values(adapter["field-map"] || {}));
  if (Object.keys(payload).some(key => !allowed.has(key))) throw new Error("Prepared Claim adapter emitted an undeclared effect field or selector");
  return payload;
}

async function createClaimAdapterHandler(options) {
  const adapter = validateAdapter(structuredClone(options.adapter));
  const factoryClaim = currentClaimHandle();
  if (!factoryClaim) throw new Error("Trusted Claim adapter requires a per-Claim execution factory");
  const factoryIdentity = claimIdentity(factoryClaim);
  const stagedMode = isStagedMode();
  let execute;
  return async (message, resolvedIds, temporaryIds) => {
    if (currentClaimHandle() !== factoryClaim) throw new Error("Trusted Claim adapter cannot escape its factory Claim");
    assertClaimIdentity(factoryIdentity);
    const original = normalizeRuntimeMessage(message);
    if (Object.hasOwn(original, "repo") && original.repo !== adapter["target-repo"]) throw new Error("Custom Claim output explicit repository conflicts with its trusted adapter");
    message = await assertClaimAuthorized({ ...original, repo: adapter["target-repo"] }, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: factoryClaim };
    const payload = loadPreparedPayload(original, adapter, options.filename || preparedAdapterPath(options.artifactRoot || "/tmp/gh-aw", factoryClaim, message.type));
    const projected = projectAdapterMessage(message, adapter, payload);
    const member = currentClaimAssignment().claims.find(claim => claim.handle === factoryClaim);
    if (!member) throw new Error("Prepared Claim adapter lost its immutable member");
    const verification = member.work.effect_contract?.outputs?.find(output => output.type === original.type)?.verification;
    if (verification !== undefined) {
      if (!DECLARED_EFFECT_TYPES.has(adapter["effect-type"]) || verification.verifier_id !== adapterVerifierId(adapter, original.type)) throw new Error("Declared effect contract has no supported trusted adapter verifier binding");
      if (!matchesDeclaredAdapterExpected(adapter, adapterEffectFields(adapter, projected), verification)) throw new Error("Prepared Claim effect differs from its immutable declared verification expectation");
    }
    await assertClaimAuthorized(projected);
    if (!execute) {
      execute =
        adapter["effect-type"] === "github_rest"
          ? createRestEffectHandler(adapter, options.github)
          : adapter["effect-type"] === "git_tree"
            ? createGitTreeEffectHandler(adapter, options.github)
            : adapter["effect-type"] === "github_graphql"
              ? createGraphqlEffectHandler(adapter, options.github)
              : await options.loadEffectHandler(adapter["effect-type"]);
    }
    if (typeof execute !== "function") throw new Error("Trusted Claim adapter executable is unavailable");
    const result = await execute(projected, resolvedIds, temporaryIds);
    if (!result || typeof result !== "object") throw new Error("Trusted Claim adapter did not return an exact effect receipt");
    privateReceipts.set(result, { ...factoryIdentity, adapter: canonical(adapter), message: projected });
    return result;
  };
}

async function verifyClaimAdapterOutput(options) {
  const adapter = validateAdapter(options.adapter);
  const receipt = options.result && privateReceipts.get(options.result);
  if (!receiptMatchesClaim(receipt, options.claim) || receipt.adapter !== canonical(adapter)) return { verified: false };
  if (adapter["effect-type"] === "github_rest") return verifyRestAdapterDelivery(options);
  if (adapter["effect-type"] === "git_tree") return verifyGitTreeDelivery(options);
  if (adapter["effect-type"] === "github_graphql") return verifyGraphqlAdapterDelivery(options);
  if (!matchesDeclaredAdapterExpected(adapter, adapterEffectFields(adapter, receipt.message), options.verification)) return { verified: false };
  return verifyBuiltinDeliveryOutput({ ...options, message: receipt.message });
}

module.exports = {
  wrapDeclaredBuiltinHandler,
  ADAPTER_EFFECT_TYPES,
  EFFECT_FIELDS,
  validateAdapter,
  preparedAdapterPath,
  projectAdapterMessage,
  loadPreparedPayload,
  createClaimAdapterHandler,
  verifyClaimAdapterOutput,
  createDeclaredAdapterVerifier,
  isDeclaredAdapterVerifier,
};
