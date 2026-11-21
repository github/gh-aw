// @ts-check
"use strict";

const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { validatePolicyEnv } = require("./work_queue_policy.cjs");

function settingsError(property, message) {
  return new Error(`${property}: ${message}; update .github/workflows/aw.json`);
}

function object(value, property, allowed) {
  if (!value || typeof value !== "object" || Array.isArray(value) || (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null))
    throw settingsError(property, "must be a JSON object, not null, an array, or a scalar");
  if (allowed) {
    for (const key of Object.keys(value).sort()) {
      if (!allowed.includes(key)) throw settingsError(`${property}.${key}`, "unknown scheduling property; worker routes and credentials are managed by AW");
    }
  }
  return value;
}

function integer(value, property, minimum, maximum) {
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) throw settingsError(property, `must be an integer in ${minimum}..${maximum}`);
  return value;
}

const LONE_SURROGATE = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u;

function validKey(value, maximum) {
  return value !== "" && Buffer.byteLength(value, "utf8") <= maximum && !/\p{Cc}/u.test(value) && !LONE_SURROGATE.test(value);
}

function retrySettings(value, property, defaults) {
  const fields = object(value, property, ["max_attempts"]);
  return { max_attempts: Object.hasOwn(fields, "max_attempts") ? integer(fields.max_attempts, `${property}.max_attempts`, 1, 16) : defaults.max_attempts };
}

/**
 * @param {unknown} value
 * @param {number} maximum
 * @returns {boolean}
 */
function validLiteral(value, maximum) {
  return typeof value === "string" && !/^\p{White_Space}*$/u.test(value) && Buffer.byteLength(value, "utf8") <= maximum && !value.includes("${{") && !/\p{Cc}/u.test(value) && !LONE_SURROGATE.test(value);
}

function issuesSettings(value) {
  if (value === false) return undefined;
  if (value === true) return { label: "work", title_prefix: "[work queue]", orphaned_after_days: 30 };
  const fields = object(value, "work_queue.issues", ["label", "title_prefix", "orphaned_after_days"]);
  const label = Object.hasOwn(fields, "label") ? fields.label : "work";
  if (!validLiteral(label, 33)) throw settingsError("work_queue.issues.label", "must be a nonblank literal of at most 33 UTF-8 bytes without control characters or expressions");
  const titlePrefix = Object.hasOwn(fields, "title_prefix") ? fields.title_prefix : "[work queue]";
  if (!validLiteral(titlePrefix, 80)) throw settingsError("work_queue.issues.title_prefix", "must be a nonblank literal of at most 80 UTF-8 bytes without control characters or expressions");
  const orphanedAfterDays = Object.hasOwn(fields, "orphaned_after_days") ? integer(fields.orphaned_after_days, "work_queue.issues.orphaned_after_days", 1, 3650) : 30;
  return { label, title_prefix: titlePrefix, orphaned_after_days: orphanedAfterDays };
}

// Accept a raw work_queue section or an already decoded JSON object. Undefined
// (or empty raw bytes) represents absence; explicit null is always invalid.
function parseSettings(rawSection) {
  let value = rawSection;
  if (Buffer.isBuffer(value)) {
    const text = value.toString("utf8");
    if (!Buffer.from(text, "utf8").equals(value)) throw settingsError("work_queue", "JSON must be valid UTF-8");
    value = text;
  }
  if (typeof value === "string" && value !== "") {
    try {
      // Preserve paths for invalid numeric settings while retaining strict JSON
      // duplicate/Unicode checks. Non-integer numeric tokens become invalid strings.
      const text = value.replace(/"(?:\\.|[^"\\])*"|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/g, token => {
        if (token.startsWith('"') || /^(?:0|[1-9][0-9]*|-[1-9][0-9]*)$/.test(token)) return token;
        return JSON.stringify(token);
      });
      value = parseStrictJSON(text);
    } catch (error) {
      throw settingsError("work_queue", `must be valid scheduling JSON: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  if (value === undefined || value === "") value = {};
  const fields = object(value, "work_queue", ["concurrency", "pending_limit", "retry", "pools", "issues"]);
  const settings = { concurrency: 16, pending_limit: 4096, retry: { max_attempts: 3 }, pools: {} };
  if (Object.hasOwn(fields, "concurrency")) settings.concurrency = integer(fields.concurrency, "work_queue.concurrency", 1, 4096);
  if (Object.hasOwn(fields, "pending_limit")) settings.pending_limit = integer(fields.pending_limit, "work_queue.pending_limit", 1, 4096);
  if (Object.hasOwn(fields, "retry")) settings.retry = retrySettings(fields.retry, "work_queue.retry", settings.retry);
  if (Object.hasOwn(fields, "pools")) {
    const pools = object(fields.pools, "work_queue.pools");
    if (Object.keys(pools).length > 64) throw settingsError("work_queue.pools", "must contain at most 64 scheduling pools");
    for (const name of Object.keys(pools).sort()) {
      const property = `work_queue.pools.${name}`;
      if (!validKey(name, 256)) throw settingsError(property, "pool name must be a nonempty identity of at most 256 UTF-8 bytes");
      const pool = object(pools[name], property, ["concurrency", "per_account_limit", "retry"]);
      const preferences = {
        concurrency: Object.hasOwn(pool, "concurrency") ? integer(pool.concurrency, `${property}.concurrency`, 1, 4096) : settings.concurrency,
        per_account_limit: Object.hasOwn(pool, "per_account_limit") ? integer(pool.per_account_limit, `${property}.per_account_limit`, 1, 4096) : 0,
        retry: Object.hasOwn(pool, "retry") ? retrySettings(pool.retry, `${property}.retry`, settings.retry) : { ...settings.retry },
      };
      Object.defineProperty(settings.pools, name, { value: preferences, enumerable: true, configurable: true, writable: true });
    }
  }
  const issues = Object.hasOwn(fields, "issues") ? issuesSettings(fields.issues) : undefined;
  return { ...settings, ...(issues === undefined ? {} : { issues }) };
}

/**
 * Build the compiled v4 policy environment from aw.json preferences and the
 * approved AW worker workflows. Every pool may route to every approved worker.
 * @param {{repository: string, workflows: string[], settings?: unknown}} options
 * @returns {import("./work_queue_policy.cjs").PolicyEnv}
 */
function buildPolicyEnv({ repository, workflows, settings }) {
  if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw settingsError("work_queue", "the AW host must supply the owner/repository");
  if (!Array.isArray(workflows) || workflows.length < 1 || workflows.length > 256) throw settingsError("work_queue.pools", "AW must approve 1..256 worker workflows");
  const parsed = parseSettings(settings);
  const names = [...new Set(workflows)].sort();
  const profiles = {};
  for (const name of names) {
    if (typeof name !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name) || name.includes("..")) throw settingsError("work_queue.pools", "AW worker targets must be approved workflow IDs, not paths");
    profiles[name] = { workflow: `.github/workflows/${name}.lock.yml`, effect_scope: repository };
  }
  const pools = {};
  const define = (name, preferences) =>
    Object.defineProperty(pools, name, {
      value: { concurrency: preferences.concurrency, per_account_limit: preferences.per_account_limit, max_attempts: preferences.retry.max_attempts, default_profile: names[0], profiles: structuredClone(profiles) },
      enumerable: true,
      configurable: true,
      writable: true,
    });
  define("default", Object.hasOwn(parsed.pools, "default") ? parsed.pools.default : { concurrency: parsed.concurrency, per_account_limit: 0, retry: parsed.retry });
  for (const name of Object.keys(parsed.pools).sort()) if (name !== "default") define(name, parsed.pools[name]);
  return validatePolicyEnv({ version: 4, pending_limit: parsed.pending_limit, issues: parsed.issues ?? false, pools });
}

module.exports = { buildPolicyEnv, parseSettings };
