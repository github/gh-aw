// @ts-check
"use strict";

const { canonical, closed } = require("./work_queue_codec.cjs");
const { assertClaimAuthorized, claimIdentity, assertClaimIdentity, receiptMatchesClaim } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

const FIELD = /^[A-Za-z_][A-Za-z_0-9]*$/;
const RESERVED = new Set([
  "owner",
  "repo",
  "method",
  "url",
  "baseUrl",
  "headers",
  "request",
  "token",
  "auth",
  "data",
  "mediaType",
  "__proto__",
  "constructor",
  "prototype",
  "claim_handle",
  "claim_id",
  "work_id",
  "dispatch_id",
  "receipt_id",
]);
const privateReceipts = new WeakMap();

function routeFields(route) {
  if (typeof route !== "string" || !/^\/repos\/\{owner\}\/\{repo\}\/[A-Za-z0-9_{}./-]+$/.test(route) || route.includes("..") || route.includes("//")) throw new Error("Trusted REST adapter requires a fixed repository-relative route");
  const fields = [...route.matchAll(/\{([A-Za-z_][A-Za-z_0-9]*)\}/g)].map(match => match[1]);
  if (route.replace(/\{[A-Za-z_][A-Za-z_0-9]*\}/g, "").match(/[{}]/)) throw new Error("Malformed trusted REST adapter route");
  return fields;
}

function validateRestAdapter(adapter) {
  closed(adapter.request, ["method", "route", "permission"], [], "trusted REST adapter request");
  closed(adapter.verifier, ["route", "fields", "resource-kind"], ["number-field"], "trusted REST adapter verifier");
  if (!["POST", "PUT", "PATCH"].includes(adapter.request.method)) throw new Error("Trusted REST adapter requires an explicit supported mutation method");
  if (!["checks", "contents", "issues", "pull-requests", "deployments", "discussions"].includes(adapter.request.permission)) throw new Error("Trusted REST adapter requires an explicit native write permission");
  const requestFields = routeFields(adapter.request.route);
  const readFields = routeFields(adapter.verifier.route);
  if (requestFields.includes("receipt_id") || !readFields.includes("receipt_id")) throw new Error("Independent REST readback must bind the exact private native receipt ID");
  if (!["unknown", "issue", "pull_request", "comment", "release", "check_run", "deployment", "repository"].includes(adapter.verifier["resource-kind"])) throw new Error("Unsupported trusted REST resource kind");
  const declarations = Object.assign(Object.create(null), adapter.expected || {});
  for (const field of Object.keys(adapter["field-map"] || {})) declarations[field] = true;
  if (!Object.keys(declarations).length || Object.keys(declarations).length > 64 || Object.keys(declarations).some(field => !FIELD.test(field) || RESERVED.has(field)))
    throw new Error("Trusted REST adapter has invalid or reserved effect fields");
  const verifierFields = adapter.verifier.fields;
  if (!verifierFields || typeof verifierFields !== "object" || Array.isArray(verifierFields) || !Object.keys(verifierFields).length || Object.keys(verifierFields).length > 64)
    throw new Error("Trusted REST verifier requires bounded declared field readback");
  for (const [field, observed] of Object.entries(verifierFields)) {
    if (!Object.hasOwn(declarations, field) || typeof observed !== "string" || !FIELD.test(observed) || RESERVED.has(observed)) throw new Error("Trusted REST verifier has an undeclared field projection");
  }
  for (const field of Object.keys(declarations)) {
    if (!requestFields.includes(field) && !Object.hasOwn(verifierFields, field)) throw new Error("Every declared REST effect field requires independent readback");
  }
  for (const field of [...requestFields, ...readFields]) {
    if (["owner", "repo", "receipt_id"].includes(field)) continue;
    if (!Object.hasOwn(declarations, field)) throw new Error("Trusted REST route contains an unbound selector");
  }
  if (adapter.verifier["number-field"] !== undefined && (!FIELD.test(adapter.verifier["number-field"]) || RESERVED.has(adapter.verifier["number-field"]))) throw new Error("Invalid trusted REST verifier resource number field");
  return adapter;
}

function concreteRoute(template, values) {
  return template.replace(/\{([A-Za-z_][A-Za-z_0-9]*)\}/g, (_, field) => {
    const value = values[field];
    if ((typeof value !== "string" && typeof value !== "number") || String(value).length > 256 || !/^[A-Za-z0-9_.-]+$/.test(String(value)) || [".", ".."].includes(String(value)))
      throw new Error("Trusted REST route requires a bounded concrete selector");
    return encodeURIComponent(String(value));
  });
}

function nativeIdentity(value) {
  const id = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  if (typeof id !== "string" || !/^[1-9][0-9]{0,255}$/.test(id)) throw new Error("Trusted REST response has no lossless native resource identity");
  return id;
}

function createRestEffectHandler(adapter, github) {
  adapter = validateRestAdapter(structuredClone(adapter));
  const identity = claimIdentity(require("./work_queue_claim_scope.cjs").currentClaimHandle());
  const client = wrapClaimEffectClient(github, { claim_handle: identity.claim_handle });
  const stagedMode = isStagedMode();
  return async message => {
    assertClaimIdentity(identity);
    await assertClaimAuthorized(message, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: identity.claim_handle };
    const [owner, repo] = adapter["target-repo"].split("/");
    const fields = { ...(adapter.expected || {}) };
    for (const field of Object.keys(adapter["field-map"] || {})) fields[field] = message[field];
    const values = { ...fields, owner, repo };
    const route = concreteRoute(adapter.request.route, values);
    const selectors = new Set(routeFields(adapter.request.route));
    const body = Object.fromEntries(Object.entries(fields).filter(([field]) => !selectors.has(field)));
    const response = await client.request(`${adapter.request.method} ${route}`, body);
    const id = nativeIdentity(response?.data?.id);
    const result = { success: true, repo: adapter["target-repo"], id };
    privateReceipts.set(result, { ...identity, adapter: canonical(adapter), fields: structuredClone(fields), id });
    return result;
  };
}

async function verifyRestAdapterDelivery(options) {
  const { adapter, result, claim, github } = options;
  validateRestAdapter(adapter);
  const receipt = result && privateReceipts.get(result);
  if (!receiptMatchesClaim(receipt, claim) || receipt.adapter !== canonical(adapter)) return { verified: false };
  const [owner, repo] = adapter["target-repo"].split("/");
  const route = concreteRoute(adapter.verifier.route, { ...receipt.fields, owner, repo, receipt_id: receipt.id });
  const { data } = await github.request(`GET ${route}`);
  if (!data || nativeIdentity(data.id) !== receipt.id) return { verified: false };
  for (const [desired, observed] of Object.entries(adapter.verifier.fields)) {
    if (!Object.hasOwn(data, observed) || canonical(data[observed]) !== canonical(receipt.fields[desired])) return { verified: false };
  }
  const numberField = adapter.verifier["number-field"];
  const number = numberField === undefined ? undefined : data[numberField];
  if (numberField !== undefined && (!Number.isSafeInteger(number) || number < 1)) return { verified: false };
  const resource = { kind: adapter.verifier["resource-kind"], repository: adapter["target-repo"], id: receipt.id, ...(number === undefined ? {} : { number }) };
  return { verified: true, claim_handle: claim.handle, resource, evidence: { source: "independent_native_readback", id: receipt.id, fields: Object.keys(adapter.verifier.fields) } };
}

module.exports = { validateRestAdapter, createRestEffectHandler, verifyRestAdapterDelivery };
