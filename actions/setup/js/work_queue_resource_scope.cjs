// @ts-check
"use strict";

const { canonical, queueError } = require("./work_queue_codec.cjs");

const RESOURCE_FIELDS = new Set(["repository", "kind", "host", "repository_id", "resource_id", "number", "comment_id", "run_id", "ref", "path"]);
const DECIMAL_FIELDS = new Set(["repository_id", "resource_id", "number", "comment_id", "run_id"]);

/** @typedef {Record<string, string> & {repository: string}} EffectResource */
/** @typedef {{version: 1, resources: EffectResource[]}} FrozenResourceScope */

function scopeError(message) {
  return queueError("claim_scope_invalid", message);
}

/**
 * @param {unknown} resource
 * @returns {EffectResource}
 */
function validateEffectResource(resource) {
  if (
    !resource ||
    typeof resource !== "object" ||
    Array.isArray(resource) ||
    !("repository" in resource) ||
    !Object.hasOwn(resource, "repository") ||
    typeof resource.repository !== "string" ||
    !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(resource.repository)
  ) {
    throw scopeError("effect target requires a canonical repository");
  }
  /** @type {EffectResource} */
  const validated = { repository: resource.repository };
  for (const [field, value] of Object.entries(resource)) {
    if (!RESOURCE_FIELDS.has(field) || typeof value !== "string" || !value.length || Buffer.byteLength(value, "utf8") > 256 || /[\x00-\x1f\x7f]|[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value)) {
      throw scopeError("effect target fields must be closed bounded nonempty identities");
    }
    if (DECIMAL_FIELDS.has(field) && !/^[1-9][0-9]*$/.test(value)) throw scopeError("effect resource IDs must be positive canonical decimals");
    if (field === "host" && value !== "github.com") throw scopeError("effect target host is unsupported");
    if (field === "kind" && !["issue", "pull_request"].includes(value)) throw scopeError("effect target has an unknown resource kind");
    if (field === "path" && (value.startsWith("/") || value.includes("\\") || value.split("/").some(part => !part || part === "." || part === ".."))) {
      throw scopeError("effect paths must be exact nontraversing repository-relative paths");
    }
    validated[field] = value;
  }
  return validated;
}

/**
 * @param {unknown} payload
 * @returns {FrozenResourceScope | undefined}
 */
function frozenResourceScope(payload) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload) || !("resource_scope" in payload) || !Object.hasOwn(payload, "resource_scope")) return undefined;
  const scope = payload.resource_scope;
  if (
    !scope ||
    typeof scope !== "object" ||
    Array.isArray(scope) ||
    !("version" in scope) ||
    !("resources" in scope) ||
    !Object.hasOwn(scope, "version") ||
    !Object.hasOwn(scope, "resources") ||
    Object.keys(scope).some(field => !["version", "resources"].includes(field)) ||
    scope.version !== 1 ||
    !Array.isArray(scope.resources) ||
    scope.resources.length > 128
  ) {
    throw scopeError("Work resource_scope requires closed version 1 and at most 128 resource selectors");
  }
  const resources = scope.resources.map(validateEffectResource);
  const seen = new Set();
  for (const selector of resources) {
    const encoded = canonical(selector);
    if (seen.has(encoded)) throw scopeError("Work resource_scope selectors must be unique canonical objects");
    seen.add(encoded);
  }
  return { version: 1, resources };
}

/**
 * @param {Record<string, string>} selector
 * @param {Record<string, string>} target
 */
function matchesEffectResource(selector, target) {
  return Object.entries(selector).every(([field, expected]) => target[field] === expected);
}

function hasNativeBinding(selector) {
  if (selector.host !== "github.com" || !selector.repository_id) return false;
  if (["kind", "number", "resource_id", "comment_id"].some(field => Object.hasOwn(selector, field))) {
    return !!selector.kind && !!selector.number && !!selector.resource_id;
  }
  return true;
}

function assertWorkTarget(work, target) {
  const scope = frozenResourceScope(work.payload);
  const compiledTarget = require("./work_queue_claim_scope.cjs").isProtectedClaimResourceTarget(target);
  const matchesSelector = selector => matchesEffectResource(selector, target) && (compiledTarget || ["run_id", "ref", "path"].every(field => !Object.hasOwn(target, field) || selector[field] === target[field]));
  if (!work.subject && !scope?.resources.some(selector => hasNativeBinding(selector) && matchesSelector(selector))) {
    throw scopeError("effect requires a positive immutable Work target binding with native repository and resource identities");
  }
  if (scope && !scope.resources.some(selector => (work.subject || hasNativeBinding(selector)) && matchesSelector(selector))) throw scopeError("effect target lies outside immutable Work resource_scope");
  if (work.subject) {
    const expected = validateEffectResource(work.subject);
    if (!hasNativeBinding(expected) || !matchesEffectResource(expected, target)) throw scopeError("effect target does not match the full immutable Work subject");
    if (["run_id", "ref", "path"].some(field => Object.hasOwn(target, field)) && !scope) throw scopeError("generic effects require an explicit immutable Work selector and cannot bypass Subject");
  }
}

function validateEffectResourceAuthority(state, authority, resource) {
  const target = validateEffectResource(resource);
  const { claim, work, dispatch } = authority;
  const installedProfile = state.policy?.pools?.[work.pool]?.profiles?.[work.worker_profile];
  if (!installedProfile || !dispatch?.profile || !dispatch.run || (!dispatch.profile.logical_contract && target.repository !== installedProfile.effect_scope) || target.repository !== dispatch.profile.effect_scope) {
    throw scopeError("effect target lies outside frozen and installed profile scope");
  }
  if (target.run_id !== undefined && target.run_id !== dispatch.run.run_id) throw scopeError("effect target is not the original native worker run");
  const original = dispatch.claims.find(member => member.claim_id === claim.claim_id);
  if (!original || canonical(original.work) !== canonical(work.payload)) throw scopeError("Work payload differs from the original immutable assignment");
  assertWorkTarget(work, target);
  const visited = new Set([work.work_id]);
  let descendant = work;
  for (;;) {
    const creator =
      state.workCreators?.get(descendant.work_id) ??
      [...(state.historicalTransactions ?? []), ...state.transactions].find(commit => commit.operations.some(operation => operation.kind === "Work" && operation.work_id === descendant.work_id))?.actor;
    if (creator?.role !== "worker") break;
    const parentDispatch = state.dispatches.get(creator.dispatch_id);
    const parentMember = parentDispatch?.claims.find(member => member.handle === creator.claim_handle);
    const parent = parentMember && state.works.get(parentMember.work_id);
    if (!parent || visited.has(parent.work_id)) throw scopeError("immutable Work ancestor authority is missing or cyclic");
    const parentProfile = state.policy?.pools?.[parent.pool]?.profiles?.[parent.worker_profile];
    if (!parentProfile || (!parentDispatch.profile?.logical_contract && target.repository !== parentProfile.effect_scope) || target.repository !== parentDispatch.profile?.effect_scope)
      throw scopeError("effect target lies outside immutable ancestor profile scope");
    visited.add(parent.work_id);
    assertWorkTarget(parent, target);
    descendant = parent;
  }
  return authority;
}

module.exports = { validateEffectResource, frozenResourceScope, validateEffectResourceAuthority };
