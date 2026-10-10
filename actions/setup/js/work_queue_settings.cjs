// @ts-check
"use strict";

const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { defaultPolicy, validatePolicy } = require("./work_queue_policy.cjs");

function settingsError(property, message) {
  return new Error(`${property}: ${message}; update .github/workflows/aw.json`);
}

function object(value, property, allowed) {
  if (!value || typeof value !== "object" || Array.isArray(value) || (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null))
    throw settingsError(property, "must be a JSON object, not null, an array, or a scalar");
  if (allowed) {
    for (const key of Object.keys(value).sort()) {
      if (!allowed.includes(key)) throw settingsError(`${property}.${key}`, "unknown scheduling property; identities, routes, trust, credentials and producers are managed by AW");
    }
  }
  return value;
}

function integer(value, property, maximum) {
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) throw settingsError(property, `must be an integer in 1..${maximum}`);
  return value;
}

function validKey(value, maximum, allowEmpty = false) {
  return (allowEmpty || value !== "") && Buffer.byteLength(value, "utf8") <= maximum && !/\p{Cc}/u.test(value) && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value);
}

function retrySettings(value, property, defaults) {
  const fields = object(value, property, ["max_attempts", "backoff_seconds"]);
  return {
    max_attempts: Object.hasOwn(fields, "max_attempts") ? integer(fields.max_attempts, `${property}.max_attempts`, 16) : defaults.max_attempts,
    backoff_seconds: Object.hasOwn(fields, "backoff_seconds") ? integer(fields.backoff_seconds, `${property}.backoff_seconds`, 3600) : defaults.backoff_seconds,
  };
}

function issuesSettings(value) {
  if (value === false) return undefined;
  if (value === true) return { label: "work" };
  const fields = object(value, "work_queue.issues", ["label"]);
  const label = Object.hasOwn(fields, "label") ? fields.label : "work";
  if (
    typeof label !== "string" ||
    /^\p{White_Space}*$/u.test(label) ||
    Buffer.byteLength(label, "utf8") > 33 ||
    label.includes("${{") ||
    /\p{Cc}/u.test(label) ||
    /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(label)
  )
    throw settingsError("work_queue.issues.label", "must be a nonblank literal of at most 33 UTF-8 bytes without control characters or expressions");
  return { label };
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
  const fields = object(value, "work_queue", ["mode", "class_weights", "accounting_weights", "concurrency", "pending_limit", "retry", "pools", "issues"]);
  const settings = {
    mode: "weighted-priority",
    class_weights: [8, 4, 2, 1, 1],
    accounting_weights: { "": 1 },
    concurrency: 16,
    pending_limit: 4096,
    retry: { max_attempts: 3, backoff_seconds: 30 },
    pools: {},
  };
  if (Object.hasOwn(fields, "mode")) {
    if (!["weighted-priority", "strict-priority"].includes(fields.mode)) throw settingsError("work_queue.mode", "must be weighted-priority or strict-priority");
    settings.mode = fields.mode;
  }
  if (Object.hasOwn(fields, "concurrency")) settings.concurrency = integer(fields.concurrency, "work_queue.concurrency", 4096);
  if (Object.hasOwn(fields, "pending_limit")) settings.pending_limit = integer(fields.pending_limit, "work_queue.pending_limit", 4096);
  if (Object.hasOwn(fields, "class_weights")) {
    if (!Array.isArray(fields.class_weights) || fields.class_weights.length !== 5) throw settingsError("work_queue.class_weights", "must contain exactly five integer weights in 1..1000");
    settings.class_weights = fields.class_weights.map((weight, index) => integer(weight, `work_queue.class_weights[${index}]`, 1000));
  }
  if (Object.hasOwn(fields, "accounting_weights")) {
    const weights = object(fields.accounting_weights, "work_queue.accounting_weights");
    for (const key of Object.keys(weights).sort()) {
      const property = `work_queue.accounting_weights.${key}`;
      if (!validKey(key, 128, true)) throw settingsError(property, "must be an accounting key of at most 128 UTF-8 bytes without control characters");
      const weight = integer(weights[key], property, 1000);
      if (key === "" && weight !== 1) throw settingsError(property, "the default accounting key must have weight 1");
      Object.defineProperty(settings.accounting_weights, key, { value: weight, enumerable: true, configurable: true, writable: true });
    }
    if (Object.keys(settings.accounting_weights).length > 1024) throw settingsError("work_queue.accounting_weights", "must contain at most 1024 keys including the default key");
  }
  if (Object.hasOwn(fields, "retry")) settings.retry = retrySettings(fields.retry, "work_queue.retry", settings.retry);
  if (Object.hasOwn(fields, "pools")) {
    const pools = object(fields.pools, "work_queue.pools");
    if (Object.keys(pools).length > 64) throw settingsError("work_queue.pools", "must contain at most 64 scheduling pools");
    for (const name of Object.keys(pools).sort()) {
      const property = `work_queue.pools.${name}`;
      if (!validKey(name, 256)) throw settingsError(property, "pool name must be a nonempty identity of at most 256 UTF-8 bytes");
      const pool = object(pools[name], property, ["concurrency", "per_account_limit", "retry"]);
      const preferences = {
        concurrency: Object.hasOwn(pool, "concurrency") ? integer(pool.concurrency, `${property}.concurrency`, 4096) : settings.concurrency,
        ...(Object.hasOwn(pool, "per_account_limit") ? { per_account_limit: integer(pool.per_account_limit, `${property}.per_account_limit`, 4096) } : {}),
        retry: Object.hasOwn(pool, "retry") ? retrySettings(pool.retry, `${property}.retry`, settings.retry) : { ...settings.retry },
      };
      Object.defineProperty(settings.pools, name, { value: preferences, enumerable: true, configurable: true, writable: true });
    }
  }
  const issues = Object.hasOwn(fields, "issues") ? issuesSettings(fields.issues) : undefined;
  return { ...settings, ...(issues === undefined ? {} : { issues }) };
}

// Copy scheduling only; Issues projection and route/template binding belong to the host.
function applySettings(policy, settings = parseSettings()) {
  settings = parseSettings(settings);
  const result = structuredClone(policy);
  if (!result.pools || !Object.keys(result.pools).length) throw settingsError("work_queue.pools", "AW must approve at least one worker pool before scheduling settings can apply");
  result.mode = settings.mode;
  result.class_weights = [...settings.class_weights];
  result.accounting_weights = { ...settings.accounting_weights };
  result.limits.pending_nodes = settings.pending_limit;
  for (const name of Object.keys(settings.pools).sort()) {
    if (Object.hasOwn(result.pools, name)) continue;
    if (!Object.hasOwn(result.pools, "default")) throw settingsError(`work_queue.pools.${name}`, "additional scheduling pools require an AW-approved default pool to inherit worker routes");
    Object.defineProperty(result.pools, name, { value: structuredClone(result.pools.default), enumerable: true, configurable: true, writable: true });
  }
  if (Object.keys(result.pools).length > 64) throw settingsError("work_queue.pools", "resolved policy must contain at most 64 scheduling pools");
  for (const name of Object.keys(result.pools).sort()) {
    const pool = result.pools[name];
    const preferences = Object.hasOwn(settings.pools, name) ? settings.pools[name] : settings;
    pool.logical_limit = preferences.concurrency;
    pool.native_limit = preferences.concurrency;
    if (preferences.per_account_limit === undefined) delete pool.per_account_limit;
    else pool.per_account_limit = preferences.per_account_limit;
    pool.retry = { max_attempts: preferences.retry.max_attempts, backoff_ms: preferences.retry.backoff_seconds * 1000 };
    for (const profile of Object.values(pool.profiles)) {
      profile.max_claims = 1;
      profile.share_keys = false;
    }
  }
  return result;
}

// The host supplies approved AW targets, never aw.json scheduling preferences.
/** @param {{repository: string, ref: string, workflows: string[], settings?: object}} options */
function buildAWPolicy({ repository, ref, workflows, settings }) {
  if (settings !== undefined) object(settings, "work_queue");
  if (typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw settingsError("work_queue", "the AW host must supply the verified owner/repository");
  if (typeof ref !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(ref) || /^0+$/.test(ref)) throw settingsError("work_queue", "the AW host must supply a non-placeholder immutable worker revision");
  if (!Array.isArray(workflows) || workflows.length < 1 || workflows.length > 256) throw settingsError("work_queue.pools", "AW must approve 1..256 worker targets");
  const policy = defaultPolicy({ principal: "", repository, ref });
  policy.producers = {};
  const pool = policy.pools.default;
  const template = pool.profiles.default;
  delete template.principal;
  const profiles = {};
  for (const name of [...new Set(workflows)].sort()) {
    if (typeof name !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name) || name.includes("..")) throw settingsError("work_queue.pools", "AW worker targets must be approved workflow IDs, not paths");
    profiles[name] = { ...template, workflow: `.github/workflows/${name}.lock.yml` };
  }
  const proposal = { ...policy, authorization: "aw", pools: { default: { ...pool, profiles, default_profile: Object.keys(profiles).sort()[0] } } };
  return validatePolicy(applySettings(proposal, parseSettings(settings)));
}

module.exports = { applySettings, buildAWPolicy, parseSettings };
