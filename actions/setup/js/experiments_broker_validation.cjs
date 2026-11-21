// @ts-check
"use strict";

const { isWellFormedUnicodeString } = require("./service_correlation.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

const LIMIT = 65536;
const MESSAGES = Object.freeze({
  CONFIGURATION: "Experiments broker configuration is invalid.",
  OIDC: "Experiments broker identity acquisition failed.",
  UNAVAILABLE: "Experiments broker is unavailable.",
  UNAUTHORIZED: "Experiments broker authorization failed.",
  UNSUPPORTED: "Experiments broker capability is unsupported.",
  CONFLICT: "Experiments broker request conflicts with an existing operation.",
  MALFORMED_RESPONSE: "Experiments broker response is invalid.",
  INPUT: "Experiments broker input is invalid.",
  STORAGE: "Experiments broker artifact persistence failed.",
});

/** @param {keyof typeof MESSAGES} kind */
function failure(kind) {
  return Object.assign(new Error(MESSAGES[kind]), { code: `ERR_EXPERIMENTS_BROKER_${kind}` });
}

/** @param {unknown} value @returns {value is Record<string, any>} */
function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {unknown} value @returns {value is string} */
function text(value) {
  return typeof value === "string" && value.length > 0 && value.length <= 4096 && isWellFormedUnicodeString(value) && !/[\u0000-\u001f\u007f-\u009f]/u.test(value);
}

/** @param {unknown} value @param {keyof typeof MESSAGES} kind */
function httpsUrl(value, kind) {
  try {
    if (typeof value !== "string" || !/^https:\/\//i.test(value) || /[\s\\#]/u.test(value) || /^https:\/\/[^/?]*@/i.test(value)) throw failure(kind);
    const url = new URL(value);
    if (url.protocol !== "https:" || !url.hostname || url.username || url.password) throw failure(kind);
    return url;
  } catch {
    throw failure(kind);
  }
}

/** @param {unknown} value @param {string[]} keys @returns {value is Record<string, any>} */
function exactKeys(value, keys) {
  return object(value) && Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}

/** @param {string} raw @param {number} [maxBytes] */
function parseJSON(raw, maxBytes = LIMIT) {
  // Metadata and measurements may use finite fractional numbers. Shape validators
  // below independently require safe integer counters and allowlisted observations.
  try {
    return parseStrictJSON(raw, { maxBytes, finiteNumbersIn: Object.keys(JSON.parse(raw) || {}) });
  } catch {
    throw failure("INPUT");
  }
}

/** @param {NodeJS.ProcessEnv} env */
function specification(env) {
  try {
    const raw = env.GH_AW_EXPERIMENT_SPEC;
    if (!raw || Buffer.byteLength(raw) > LIMIT) throw failure("CONFIGURATION");
    const spec = parseJSON(raw);
    if (!object(spec) || !Object.keys(spec).length) throw failure("CONFIGURATION");
    for (const [name, config] of Object.entries(spec)) {
      const variants = Array.isArray(config) ? config : object(config) ? config.variants : null;
      if (!/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(name) || !Array.isArray(variants) || variants.length < 2 || !variants.every(text) || new Set(variants).size !== variants.length) throw failure("CONFIGURATION");
    }
    return spec;
  } catch {
    throw failure("CONFIGURATION");
  }
}

/** @param {NodeJS.ProcessEnv} env */
function identity(env) {
  const result = {
    job_id: env.GITHUB_JOB,
    workflow_id: env.GH_AW_WORKFLOW_ID,
    workflow_ref: env.GH_AW_WORKFLOW_REF,
    invocation_id: env.GH_AW_EXPERIMENT_INVOCATION_ID,
    run_id: env.GITHUB_RUN_ID,
    run_attempt: env.GITHUB_RUN_ATTEMPT,
    repository: env.GITHUB_REPOSITORY,
    server_url: env.GITHUB_SERVER_URL,
  };
  if (Object.values(result).some(value => !text(value))) throw failure("CONFIGURATION");
  if (!/^[A-Za-z0-9_-]{1,128}$/.test(result.invocation_id || "")) throw failure("CONFIGURATION");
  if (!/^[1-9][0-9]*$/.test(result.run_id || "") || !/^[1-9][0-9]*$/.test(result.run_attempt || "") || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(result.repository || "")) throw failure("CONFIGURATION");
  httpsUrl(result.server_url, "CONFIGURATION");
  // These are routing hints; the service binds identity using verified OIDC claims.
  return result;
}

/** @param {unknown} value @param {Record<string, any> | undefined} spec @param {keyof typeof MESSAGES} kind */
function assignments(value, spec, kind) {
  if (!object(value) || !Object.keys(value).length || (spec && !exactKeys(value, Object.keys(spec)))) throw failure(kind);
  for (const [name, variant] of Object.entries(value)) {
    if (!/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(name) || !text(variant)) throw failure(kind);
    if (spec) {
      const config = spec[name];
      if (!(Array.isArray(config) ? config : config.variants).includes(variant)) throw failure(kind);
    }
  }
  return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)));
}

/** @param {unknown} value @param {Record<string, any>} spec */
function decision(value, spec) {
  if (!exactKeys(value, ["assignments", "counts"])) throw failure("MALFORMED_RESPONSE");
  const result = value;
  const selected = assignments(result.assignments, spec, "MALFORMED_RESPONSE");
  if (!exactKeys(result.counts, Object.keys(spec))) throw failure("MALFORMED_RESPONSE");
  const counts = Object.fromEntries(
    Object.keys(selected).map(name => {
      const config = spec[name];
      const variants = Array.isArray(config) ? config : config.variants;
      const values = result.counts[name];
      if (!exactKeys(values, variants) || !Object.values(values).every(count => Number.isSafeInteger(count) && count >= 0)) throw failure("MALFORMED_RESPONSE");
      return [name, Object.fromEntries(variants.map(variant => [variant, values[variant]]))];
    })
  );
  return { assignments: selected, counts };
}

/** @param {unknown} metadata @param {NodeJS.ProcessEnv} env */
function verifyProducer(metadata, env) {
  const expected = identity(env);
  if (
    !object(metadata) ||
    !Number.isSafeInteger(metadata.run_id) ||
    String(metadata.run_id) !== expected.run_id ||
    metadata.run_attempt !== expected.run_attempt ||
    metadata.repository !== expected.repository ||
    metadata.server_url !== httpsUrl(expected.server_url, "CONFIGURATION").origin ||
    metadata.workflow_id !== expected.workflow_ref ||
    metadata.workflow_ref !== expected.workflow_ref
  )
    throw failure("INPUT");
}

module.exports = { LIMIT, failure, object, text, httpsUrl, exactKeys, parseJSON, specification, identity, assignments, decision, verifyProducer };
