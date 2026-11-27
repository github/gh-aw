// @ts-check
"use strict";

const { canonical, queueError } = require("./work_queue_codec.cjs");
const { expectedGitHubHost } = require("./work_queue_host.cjs");

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
    if (field === "host" && value !== expectedGitHubHost()) throw scopeError("effect target host is unsupported");
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

module.exports = { expectedGitHubHost, validateEffectResource, frozenResourceScope };
