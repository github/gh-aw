// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { readBoundedBody } = require("./admission_protocol.cjs");
const { buildServiceRequestHeaders, isWellFormedUnicodeString } = require("./service_correlation.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

const LIMIT = 65536;
const TIMEOUT_MS = 10000;
const DEFAULT_DIRECTORY = "/tmp/gh-aw/experiments";
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

/** @param {Response} response */
function discard(response) {
  void response.body?.cancel().catch(() => {
    // Cancellation is best-effort; raw transport diagnostics must remain private.
  });
}

/**
 * @typedef {Object} BrokerOptions
 * @property {{setSecret: (value: string) => void | Promise<void>, setOutput?: (name: string, value: string) => void, exportVariable?: (name: string, value: string) => void}} core
 * @property {NodeJS.ProcessEnv} [env]
 * @property {typeof fetch} [fetchImpl]
 * @property {() => number} [now]
 * @property {typeof fs} [fsImpl]
 */

/**
 * One deadline covers both OIDC issuance and the exchange, including body reads.
 * The broker owns idempotency; this client never retries or picks a local variant.
 * @param {"assign" | "report"} operation
 * @param {Record<string, any>} payload
 * @param {BrokerOptions} options
 */
async function exchange(operation, payload, { core, env = process.env, fetchImpl = fetch, now = Date.now }) {
  const snapshot = { ...env };
  const endpoint = snapshot.GH_AW_EXPERIMENTS_BROKER_URL || "";
  httpsUrl(endpoint, "CONFIGURATION");
  const hints = identity(snapshot);
  const oidcUrl = httpsUrl(snapshot.ACTIONS_ID_TOKEN_REQUEST_URL, "OIDC");
  const bootstrap = snapshot.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
  if (typeof bootstrap !== "string" || !/^[\x21-\x7e]+$/.test(bootstrap) || typeof core?.setSecret !== "function") throw failure("OIDC");
  oidcUrl.searchParams.set("audience", endpoint);
  const controller = new AbortController();
  /** @type {ReturnType<typeof setTimeout> | undefined} */
  let timer;
  /** @type {Promise<never>} */
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => {
      reject(failure("UNAVAILABLE"));
      controller.abort();
    }, TIMEOUT_MS);
  });
  const request = async () => {
    let token;
    try {
      const response = await fetchImpl(oidcUrl, { headers: { authorization: `bearer ${bootstrap}`, accept: "application/json" }, redirect: "manual", signal: controller.signal });
      if (response.status !== 200 || response.redirected) {
        discard(response);
        throw failure("OIDC");
      }
      const result = parseJSON(await readBoundedBody(response, LIMIT));
      if (typeof result?.value !== "string") throw failure("OIDC");
      token = result.value;
      await core.setSecret(token);
      if (!/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token)) throw failure("OIDC");
      const claims = JSON.parse(Buffer.from(token.split(".")[1], "base64url").toString("utf8"));
      if (typeof claims?.exp !== "number" || !Number.isFinite(claims.exp) || claims.exp * 1000 <= now()) throw failure("OIDC");
    } catch {
      throw failure("OIDC");
    }
    if (controller.signal.aborted) throw failure("UNAVAILABLE");
    let response;
    try {
      response = await fetchImpl(endpoint, {
        method: "POST",
        headers: { ...buildServiceRequestHeaders(snapshot), "content-type": "application/json", accept: "application/json", "cache-control": "no-store" },
        body: JSON.stringify({ operation, oidc_token: token, ...hints, ...payload }),
        redirect: "manual",
        signal: controller.signal,
      });
    } catch {
      throw failure("UNAVAILABLE");
    }
    if (response.redirected || response.status !== (operation === "assign" ? 200 : 204)) {
      discard(response);
      if (response.redirected) throw failure("UNAVAILABLE");
      if (response.status === 401 || response.status === 403) throw failure("UNAUTHORIZED");
      if (response.status === 409) throw failure("CONFLICT");
      if ([400, 404, 422, 501].includes(response.status)) throw failure("UNSUPPORTED");
      throw failure("UNAVAILABLE");
    }
    try {
      if (operation === "report") {
        if (response.body && (await readBoundedBody(response, LIMIT)) !== "") throw failure("MALFORMED_RESPONSE");
        return;
      }
      return parseJSON(await readBoundedBody(response, LIMIT));
    } catch {
      throw failure("MALFORMED_RESPONSE");
    }
  };
  try {
    return await Promise.race([request(), timeout]);
  } finally {
    clearTimeout(timer);
  }
}

/**
 * @param {string} file
 * @param {typeof fs} fsImpl
 */
function readJSON(file, fsImpl) {
  try {
    if (fsImpl.statSync(file).size > 16 * LIMIT) throw failure("INPUT");
    const raw = fsImpl.readFileSync(file, "utf8");
    if (Buffer.byteLength(raw) > 16 * LIMIT) throw failure("INPUT");
    return parseJSON(raw, 16 * LIMIT);
  } catch {
    throw failure("INPUT");
  }
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

const COUNTERS = Object.freeze({
  firewall: ["total_requests", "allowed_requests", "blocked_requests"],
  session: ["total_events", "session_starts", "session_shutdowns", "turns", "assistant_messages", "reasoning_events", "tool_execution_starts", "tool_execution_completes", "failed_tool_executions"],
  gateway: ["total_calls", "failed_calls", "total_input_size", "total_output_size", "max_input_size", "max_output_size"],
  integrity: ["total_filtered"],
  safe_outputs: ["total_items"],
  ledger: ["transactions_added"],
  friction: ["counted_occurrences"],
});
const MEASURES = Object.freeze({
  gateway: ["total_duration_ms", "max_duration_ms"],
  friction: ["total_run_aic", "friction_ratio"],
});
const DIMENSIONS = ["aic", "tokens", "turns", "tool_calls", "latency_ms"];
const AVAILABLE_STATES = ["measured", "causal", "statistical"];

/** @param {unknown} summary */
function observations(summary) {
  if (!object(summary) || summary.schema !== "usage-activity-summary/v1") throw failure("INPUT");
  /** @type {Record<string, any>} */
  const result = {};
  const copy = (section, fields, integer) => {
    if (!Object.hasOwn(summary, section) || summary[section] === null) return;
    if (!object(summary[section])) throw failure("INPUT");
    for (const field of fields) {
      if (!Object.hasOwn(summary[section], field)) continue;
      const value = summary[section][field];
      if (typeof value !== "number" || !Number.isFinite(value) || value < 0 || value > Number.MAX_SAFE_INTEGER || (integer && !Number.isSafeInteger(value))) throw failure("INPUT");
      if (!Object.hasOwn(result, section)) result[section] = {};
      result[section][field] = value;
    }
  };
  for (const [section, fields] of Object.entries(COUNTERS)) copy(section, fields, true);
  for (const [section, fields] of Object.entries(MEASURES)) copy(section, fields, false);
  if (object(summary.friction)) {
    const states = {};
    if (Object.hasOwn(summary.friction, "dimension_states")) {
      if (!object(summary.friction.dimension_states)) throw failure("INPUT");
      for (const dimension of DIMENSIONS) {
        if (!Object.hasOwn(summary.friction.dimension_states, dimension)) continue;
        const state = summary.friction.dimension_states[dimension];
        if (![...AVAILABLE_STATES, "unavailable", "unsupported"].includes(state)) throw failure("INPUT");
        states[dimension] = state;
      }
      if (Object.keys(states).length) {
        if (!result.friction) result.friction = {};
        result.friction.dimension_states = states;
      }
    }
    if (Object.hasOwn(summary.friction, "measurement_state")) {
      if (!["measured", "causal", "statistical", "unavailable"].includes(summary.friction.measurement_state)) throw failure("INPUT");
      if (!result.friction) result.friction = {};
      result.friction.measurement_state = summary.friction.measurement_state;
    }
    if (Object.hasOwn(summary.friction, "cost")) {
      const cost = observationsCost(summary.friction.cost, states);
      if (Object.keys(cost).length) {
        if (!result.friction) result.friction = {};
        result.friction.cost = cost;
      }
    }
    const partial = summary.friction.total_run_aic_partial;
    if (Object.hasOwn(summary.friction, "total_run_aic_partial") && typeof partial !== "boolean") throw failure("INPUT");
    if (result.friction) {
      if (partial === true) delete result.friction.total_run_aic;
      if (partial === true || !AVAILABLE_STATES.includes(states.aic)) delete result.friction.friction_ratio;
    }
  }
  return result;
}

/** @param {unknown} value @param {Record<string, string>} states */
function observationsCost(value, states) {
  if (!object(value)) throw failure("INPUT");
  /** @type {Record<string, any>} */
  const result = {};
  for (const key of ["aic", "turns", "tool_calls", "latency_ms"]) {
    if (!Object.hasOwn(value, key)) continue;
    const number = value[key];
    if (typeof number !== "number" || !Number.isFinite(number) || number < 0 || number > Number.MAX_SAFE_INTEGER || (["turns", "tool_calls"].includes(key) && !Number.isSafeInteger(number))) throw failure("INPUT");
    if (AVAILABLE_STATES.includes(states[key])) result[key] = number;
  }
  if (Object.hasOwn(value, "tokens")) {
    if (!object(value.tokens)) throw failure("INPUT");
    const tokens = {};
    for (const key of ["input", "output", "cache_read", "cache_write", "reasoning", "total"]) {
      if (!Object.hasOwn(value.tokens, key)) continue;
      const number = value.tokens[key];
      if (!Number.isSafeInteger(number) || number < 0) throw failure("INPUT");
      tokens[key] = number;
    }
    if (Object.keys(tokens).length && AVAILABLE_STATES.includes(states.tokens)) result.tokens = tokens;
  }
  return result;
}

/** @param {BrokerOptions} options */
async function assign(options) {
  const { core, env = process.env, fsImpl = fs, now = Date.now } = options;
  if (typeof core?.setOutput !== "function" || typeof core?.exportVariable !== "function") throw failure("CONFIGURATION");
  const spec = specification(env);
  const result = decision(await exchange("assign", { experiments: spec }, options), spec);
  const directory = env.GH_AW_EXPERIMENT_STATE_DIR || DEFAULT_DIRECTORY;
  const stateFile = env.GH_AW_EXPERIMENT_STATE_FILE || path.join(directory, "state.jsonl");
  try {
    const run = { run_id: env.GITHUB_RUN_ID, timestamp: new Date(now()).toISOString(), assignments: result.assignments };
    fsImpl.mkdirSync(directory, { recursive: true });
    fsImpl.mkdirSync(path.dirname(stateFile), { recursive: true });
    // A snapshot retains cumulative counts without inventing historical run records.
    fsImpl.writeFileSync(stateFile, `${JSON.stringify({ counts: result.counts, runs: [run] })}\n`, "utf8");
    fsImpl.writeFileSync(path.join(directory, "assignments.json"), `${JSON.stringify(result.assignments, null, 2)}\n`, "utf8");
  } catch {
    throw failure("STORAGE");
  }
  for (const [name, variant] of Object.entries(result.assignments)) core.setOutput(name, variant);
  core.setOutput("experiments", JSON.stringify(result.assignments));
  const attributes = Object.entries(result.assignments)
    .map(([name, variant]) => `experiment.${name}=${encodeURIComponent(variant)}`)
    .join(",");
  core.exportVariable("OTEL_RESOURCE_ATTRIBUTES", env.OTEL_RESOURCE_ATTRIBUTES ? `${env.OTEL_RESOURCE_ATTRIBUTES},${attributes}` : attributes);
  return result;
}

/** @param {BrokerOptions} options */
async function report(options) {
  const { env = process.env, fsImpl = fs } = options;
  const spec = env.GH_AW_EXPERIMENT_SPEC === undefined ? undefined : specification(env);
  const selected = assignments(readJSON(path.join(env.GH_AW_EXPERIMENT_STATE_DIR || DEFAULT_DIRECTORY, "assignments.json"), fsImpl), spec, "INPUT");
  if (!text(env.GH_AW_EXPERIMENT_RESULTS_FILE) || !text(env.GH_AW_EXPERIMENT_METADATA_FILE)) throw failure("INPUT");
  verifyProducer(readJSON(env.GH_AW_EXPERIMENT_METADATA_FILE, fsImpl), env);
  const summary = readJSON(env.GH_AW_EXPERIMENT_RESULTS_FILE, fsImpl);
  if (!object(summary?.experiments)) throw failure("INPUT");
  const recorded = assignments(summary.experiments.assignments, spec, "INPUT");
  if (!exactKeys(recorded, Object.keys(selected)) || Object.entries(selected).some(([name, value]) => recorded[name] !== value)) throw failure("INPUT");
  const results = observations(summary);
  if (!["success", "failure", "cancelled", "skipped"].includes(env.GH_AW_EXPERIMENT_AGENT_RESULT || "")) throw failure("INPUT");
  results.agent_result = env.GH_AW_EXPERIMENT_AGENT_RESULT;
  await exchange("report", { assignments: selected, results }, options);
}

module.exports = { assign, report };
