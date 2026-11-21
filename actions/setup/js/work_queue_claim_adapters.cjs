// @ts-check
"use strict";
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { closed, parseStrictJSON, canonical } = require("./work_queue_codec.cjs");
const { currentClaimHandle, normalizeRuntimeMessage, assertClaimAuthorized, claimArtifactPath, assertClaimArtifactFile, claimIdentity, assertClaimIdentity } = require("./work_queue_claim_scope.cjs");
const { validateRestAdapter, createRestEffectHandler } = require("./work_queue_rest_adapter.cjs");
const { validateGitTreeAdapter, createGitTreeEffectHandler } = require("./work_queue_git_tree_adapter.cjs");
const { validateGraphqlAdapter, createGraphqlEffectHandler } = require("./work_queue_graphql_adapter.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { authorizeSafeOutputInvocation } = require("./safe_output_execution.cjs");

const ADAPTER_EFFECT_TYPES = new Set(["create_issue", "update_issue", "close_issue", "add_comment", "add_labels", "remove_labels", "replace_label", "github_rest", "git_tree", "github_graphql"]);
const BUILTIN_EFFECT_FIELDS = Object.freeze({
  create_issue: ["title", "body", "labels", "assignees", "milestone"],
  update_issue: ["title", "body", "labels", "assignees", "milestone", "state", "status", "state_reason"],
  close_issue: ["state_reason"],
  add_comment: ["body"],
  add_labels: ["labels"],
  remove_labels: ["labels"],
  replace_label: ["label_to_add", "label_to_remove"],
});
const RESOURCE_NUMBER_FIELDS = ["item_number", "issue_number", "pull_request_number"];

function builtinAdapterFields(type) {
  const fields = BUILTIN_EFFECT_FIELDS[type];
  return fields && [...fields, ...(type === "create_issue" ? [] : RESOURCE_NUMBER_FIELDS)];
}

function builtinTargetNumber(message) {
  const numbers = RESOURCE_NUMBER_FIELDS.filter(field => Object.hasOwn(message, field)).map(field => {
    const value = message[field];
    if (typeof value === "number" ? !Number.isSafeInteger(value) || value < 1 : typeof value !== "string" || !/^[1-9][0-9]{0,255}$/.test(value)) {
      throw new Error("Native builtin Claim effect requires a positive canonical resource number");
    }
    return String(value);
  });
  if (new Set(numbers).size > 1) throw new Error("Native builtin Claim effect contains conflicting explicit resource numbers");
}

// `verifier` (github_rest) and `verifier-id` are accepted for compiled-config
// compatibility but unused: v4 records effects without delivery readback.
function validateAdapter(adapter) {
  closed(adapter, ["mode", "effect-type", "target-repo"], ["field-map", "expected", "request", "verifier", "git-tree", "graphql", "verifier-id"], "trusted Claim adapter");
  if (!["prepared", "script"].includes(adapter.mode) || !ADAPTER_EFFECT_TYPES.has(adapter["effect-type"])) throw new Error("Unsupported trusted Claim adapter effect");
  if (typeof adapter["target-repo"] !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(adapter["target-repo"])) throw new Error("Trusted Claim adapter requires a fixed repository scope");
  const allowedFields = builtinAdapterFields(adapter["effect-type"]);
  for (const fields of [adapter["field-map"] || {}, adapter.expected || {}]) {
    if (!fields || typeof fields !== "object" || Array.isArray(fields) || (allowedFields && Object.keys(fields).some(field => !allowedFields.includes(field)))) throw new Error("Unknown trusted Claim adapter effect field");
  }
  if (Object.values(adapter["field-map"] || {}).some(field => typeof field !== "string" || !/^[A-Za-z_][A-Za-z_0-9]*$/.test(field))) throw new Error("Trusted Claim adapter field mappings must be explicit message fields");
  if (Object.keys(adapter["field-map"] || {}).some(field => Object.hasOwn(adapter.expected || {}, field))) throw new Error("Trusted Claim adapter cannot map and fix the same effect field");
  if (adapter["effect-type"] === "github_rest") validateRestAdapter(adapter);
  else if (adapter.request !== undefined) throw new Error("REST request configuration requires github_rest effect-type");
  if (adapter["effect-type"] === "git_tree") validateGitTreeAdapter(adapter);
  else if (adapter["git-tree"] !== undefined) throw new Error("Code configuration requires git_tree effect-type");
  if (adapter["effect-type"] === "github_graphql") validateGraphqlAdapter(adapter);
  else if (adapter.graphql !== undefined) throw new Error("GraphQL configuration requires github_graphql effect-type");
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
    await assertClaimAuthorized(projected);
    const authorization = await authorizeSafeOutputInvocation({
      config: options,
      inputs: { message: projected, adapter },
      scope: { binding: "invocation", refs: [] },
    });
    if (authorization.state === "allowed") await assertClaimAuthorized(projected);
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
    if (!result || typeof result !== "object") throw new Error("Trusted Claim adapter did not return an effect result");
    return result;
  };
}

// @safe-outputs-exempt SEC-005 — work_queue_claim_adapters.cjs:46 validates a literal configured repository; projection fixes that destination and assertClaimAuthorized enforces the per-Claim profile allowlist before effects.
module.exports = {
  ADAPTER_EFFECT_TYPES,
  validateAdapter,
  preparedAdapterPath,
  projectAdapterMessage,
  loadPreparedPayload,
  createClaimAdapterHandler,
};
