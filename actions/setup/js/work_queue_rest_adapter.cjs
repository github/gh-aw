// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
// @safe-outputs-exempt SEC-005 — work_queue_claim_adapters.cjs validates the fixed adapter repository; assertClaimAuthorized and the guarded effect client authorize resolved targets before writes.

const { canonicalBytes, closed } = require("./work_queue_codec.cjs");
const { assertClaimAuthorized, claimIdentity, assertClaimIdentity, currentClaimHandle } = require("./work_queue_claim_scope.cjs");
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

function routeFields(route) {
  if (typeof route !== "string" || !/^\/repos\/\{owner\}\/\{repo\}\/[A-Za-z0-9_{}./-]+$/.test(route) || route.includes("..") || route.includes("//"))
    throw new Error(`${SAFE_OUTPUT_E001}: Trusted REST adapter requires a fixed repository-relative route`);
  const fields = [...route.matchAll(/\{([A-Za-z_][A-Za-z_0-9]*)\}/g)].map(match => match[1]);
  if (route.replace(/\{[A-Za-z_][A-Za-z_0-9]*\}/g, "").match(/[{}]/)) throw new Error("Malformed trusted REST adapter route");
  return fields;
}

// The adapter `verifier` key is accepted for compiled-config compatibility but
// unused: v4 records effects without delivery readback.
function validateRestAdapter(adapter) {
  closed(adapter.request, ["method", "route", "permission"], [], "trusted REST adapter request");
  if (!["POST", "PUT", "PATCH"].includes(adapter.request.method)) throw new Error("Trusted REST adapter requires an explicit supported mutation method");
  if (!["checks", "contents", "issues", "pull-requests", "deployments", "discussions"].includes(adapter.request.permission)) throw new Error("Trusted REST adapter requires an explicit native write permission");
  const requestFields = routeFields(adapter.request.route);
  const declarations = Object.assign(Object.create(null), adapter.expected || {});
  for (const field of Object.keys(adapter["field-map"] || {})) declarations[field] = true;
  if (!Object.keys(declarations).length || Object.keys(declarations).length > 64 || Object.keys(declarations).some(field => !FIELD.test(field) || RESERVED.has(field)))
    throw new Error("Trusted REST adapter has invalid or reserved effect fields");
  for (const field of requestFields) {
    if (["owner", "repo"].includes(field)) continue;
    if (!Object.hasOwn(declarations, field)) throw new Error("Trusted REST route contains an unbound selector");
  }
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
  const identity = claimIdentity(currentClaimHandle());
  const client = wrapClaimEffectClient(github, { claim_handle: identity.claim_handle });
  const stagedMode = isStagedMode();
  return async message => {
    assertClaimIdentity(identity);
    if (Object.hasOwn(message, "repo") && message.repo !== adapter["target-repo"]) throw new Error("Explicit Claim repository conflicts with its trusted REST adapter");
    await assertClaimAuthorized({ ...message, repo: adapter["target-repo"] }, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: identity.claim_handle };
    const [owner, repo] = adapter["target-repo"].split("/");
    const fields = { ...(adapter.expected || {}) };
    for (const field of Object.keys(adapter["field-map"] || {})) {
      if (!Object.hasOwn(message, field)) throw new Error("Trusted REST adapter is missing a declared input field");
      fields[field] = message[field];
    }
    if (canonicalBytes(fields) > 1024 * 1024) throw new Error("Trusted REST effect input exceeds its byte ceiling");
    const route = concreteRoute(adapter.request.route, { ...fields, owner, repo });
    const selectors = new Set(routeFields(adapter.request.route));
    const body = Object.fromEntries(Object.entries(fields).filter(([field]) => !selectors.has(field)));
    const response = await client.request(`${adapter.request.method} ${route}`, body);
    return { success: true, repo: adapter["target-repo"], id: nativeIdentity(response?.data?.id) };
  };
}

module.exports = { validateRestAdapter, createRestEffectHandler };
