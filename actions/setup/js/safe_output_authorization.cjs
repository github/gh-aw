// @ts-check
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { performance } = require("node:perf_hooks");
const { parseStrictJSON, utf8Compare, validString } = require("./work_queue_codec.cjs");
const { readBoundedBody } = require("./admission_protocol.cjs");
const { withRetry } = require("./error_recovery.cjs");

const VERSION = "gh-aw.safe-output-authorization.v1";
const MAX_REQUEST_BYTES = 65536;
const MAX_RESPONSE_BYTES = 8192;
const HTTP_TIMEOUT_MS = 10000;
const TOTAL_TIMEOUT_MS = 20000;
const MIN_VALIDITY_MS = 2000;
const MAX_VALIDITY_MS = 30000;
const HASH = /^[a-f0-9]{64}$/;
const CODE = /^[A-Za-z0-9][A-Za-z0-9_.:@/-]{0,255}$/;
const prepared = new WeakSet();
const consumed = new WeakSet();
const freshness = new WeakMap();
const reasons = new Set([
  "invalid_preparation",
  "invalid_configuration",
  "missing_credentials",
  "credential_error",
  "authority_unavailable",
  "invalid_response",
  "binding_mismatch",
  "invalid_validity",
  "denied",
  "evidence_failed",
  "already_requested",
  "timeout",
]);

class SafeOutputAuthorizationError extends Error {
  /** @param {string} [reason] @param {any} [authorization] */
  constructor(reason = "invalid_response", authorization = undefined) {
    const reasonCode = reasons.has(reason) ? reason : "invalid_response";
    super(`ERR_SAFE_OUTPUT_AUTHORIZATION: ${reasonCode}`);
    this.name = "SafeOutputAuthorizationError";
    this.reason_code = reasonCode;
    this.authorization = authorization;
  }
}

/** @returns {never} */
function fail(reason) {
  throw new SafeOutputAuthorizationError(reason);
}

function closed(value, required, optional = []) {
  if (!value || typeof value !== "object" || Array.isArray(value) || ![Object.prototype, null].includes(Object.getPrototypeOf(value))) fail("invalid_preparation");
  if (required.some(key => !Object.hasOwn(value, key)) || Object.keys(value).some(key => !required.includes(key) && !optional.includes(key))) fail("invalid_preparation");
}

function text(value, max = 1024) {
  if (typeof value !== "string" || !value || Buffer.byteLength(value) > max || /[\u0000-\u001f\u007f-\u009f]/u.test(value)) fail("invalid_preparation");
  try {
    validString(value);
  } catch {
    fail("invalid_preparation");
  }
  return value;
}

function hash(value) {
  if (typeof value !== "string" || !HASH.test(value)) fail("invalid_preparation");
  return value;
}

// JSON-only, bounded canonicalization. Keys sort by UTF-8 bytes; finite numbers
// use JSON's shortest representation. Secret values must be replaced with
// reviewed references before callers construct the normalized input snapshot.
function canonical(value) {
  let nodes = 0;
  let bytes = 0;
  const active = new Set();
  function visit(item, depth) {
    if (++nodes > 65536 || depth > 32) fail("invalid_preparation");
    if (item === null || typeof item === "boolean") return JSON.stringify(item);
    if (typeof item === "number") {
      if (!Number.isFinite(item)) fail("invalid_preparation");
      return JSON.stringify(item);
    }
    if (typeof item === "string") {
      try {
        validString(item);
      } catch {
        fail("invalid_preparation");
      }
      bytes += Buffer.byteLength(item);
      if (bytes > 8 * 1024 * 1024) fail("invalid_preparation");
      return JSON.stringify(item);
    }
    if (!item || typeof item !== "object" || active.has(item)) fail("invalid_preparation");
    active.add(item);
    let result;
    if (Array.isArray(item)) {
      if (item.length > 16384) fail("invalid_preparation");
      const entries = [];
      for (let index = 0; index < item.length; index++) {
        const descriptor = Object.getOwnPropertyDescriptor(item, String(index));
        if (!descriptor || !("value" in descriptor)) fail("invalid_preparation");
        entries.push(visit(descriptor.value, depth + 1));
      }
      result = `[${entries.join(",")}]`;
    } else {
      if (![Object.prototype, null].includes(Object.getPrototypeOf(item)) || Object.getOwnPropertySymbols(item).length) fail("invalid_preparation");
      result = `{${Object.keys(item)
        .sort(utf8Compare)
        .map(key => {
          const descriptor = Object.getOwnPropertyDescriptor(item, key);
          if (!descriptor || !("value" in descriptor)) fail("invalid_preparation");
          return `${visit(key, depth + 1)}:${visit(descriptor.value, depth + 1)}`;
        })
        .join(",")}}`;
    }
    active.delete(item);
    return result;
  }
  return visit(value, 0);
}

function canonicalDigest(value) {
  return crypto.createHash("sha256").update(canonical(value)).digest("hex");
}

function freeze(value) {
  if (value && typeof value === "object") {
    Object.values(value).forEach(freeze);
    Object.freeze(value);
  }
  return value;
}

function validateCaller(caller) {
  closed(caller, ["host", "repository_id", "workflow_ref", "workflow_sha", "workflow_hash", "run_id", "run_attempt"]);
  for (const value of Object.values(caller)) text(value);
  if (!/^[1-9][0-9]{0,39}$/.test(caller.repository_id) || !/^[1-9][0-9]{0,39}$/.test(caller.run_id) || !/^[1-9][0-9]{0,9}$/.test(caller.run_attempt)) fail("invalid_preparation");
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(caller.workflow_sha) || !/^[a-f0-9]{64}(?::[a-f0-9]{64})?$/.test(caller.workflow_hash)) fail("invalid_preparation");
  const host = new URL(caller.host);
  if (host.protocol !== "https:" || host.origin !== caller.host || host.username || host.password) fail("invalid_preparation");
}

function buildAuthorizationCaller(env = process.env) {
  const caller = {
    host: env.GITHUB_SERVER_URL || "https://github.com",
    repository_id: env.GITHUB_REPOSITORY_ID || "",
    workflow_ref: env.GITHUB_WORKFLOW_REF || env.GH_AW_INFO_WORKFLOW_ID || "",
    workflow_sha: env.GITHUB_WORKFLOW_SHA || "",
    workflow_hash: env.GH_AW_AUTHORIZATION_WORKFLOW_HASH || env.GH_AW_ADMISSION_WORKFLOW_HASH || "",
    run_id: env.GITHUB_RUN_ID || "",
    run_attempt: env.GITHUB_RUN_ATTEMPT || "",
  };
  try {
    validateCaller(caller);
    return freeze(caller);
  } catch {
    fail("invalid_preparation");
  }
}

function validateOutput(output) {
  closed(output, ["type", "name", "executor", "implementation_digest"]);
  for (const key of ["type", "name", "executor"]) {
    if (!CODE.test(text(output[key], 256))) fail("invalid_preparation");
  }
  if (!["manager", "native", "script", "action", "job", "standalone", "workflow", "queue"].includes(output.executor)) fail("invalid_preparation");
  hash(output.implementation_digest);
}

// The attempt and implementation revision are deliberately excluded from the
// logical claim identity, but remain bound in each complete request envelope.
function stableInvocationId({ caller, batchDigest, recordIndex, output }) {
  try {
    validateCaller(caller);
    validateOutput(output);
    hash(batchDigest);
    if (!Number.isSafeInteger(recordIndex) || recordIndex < 0) fail("invalid_preparation");
    return canonicalDigest({
      version: VERSION,
      host: caller.host,
      repository_id: caller.repository_id,
      workflow_ref: caller.workflow_ref,
      run_id: caller.run_id,
      batch_digest: batchDigest,
      record_index: recordIndex,
      type: output.type,
      name: output.name,
    });
  } catch {
    fail("invalid_preparation");
  }
}

function references(values) {
  if (!Array.isArray(values) || values.length > 64) fail("invalid_preparation");
  return values.map(value => {
    // References are trusted opaque resource identities, never arbitrary URLs,
    // raw bodies, or secret values.
    if (!/^[A-Za-z0-9][A-Za-z0-9_.:@/#-]{0,511}$/.test(text(value, 512))) fail("invalid_preparation");
    return value;
  });
}

/**
 * `resolved_effect` is accepted only for the exact registered plan created by a
 * trusted effect-preparation adapter; the enum alone is never evidence.
 * @param {{caller: any, output: any, normalizedInputs: unknown, batchDigest: string, recordIndex: number,
 * scope?: {binding: string, refs: string[], limits_digest?: string},
 * context?: {policy_digest?: string, evidence_refs?: string[], claim_id?: string, delivery_id?: string}, effectPlan?: object}} options
 */
function prepareInvocation({ caller, output, normalizedInputs, batchDigest, recordIndex, scope = { binding: "invocation", refs: [] }, context = {}, effectPlan = undefined }) {
  try {
    if (scope?.binding === "resolved_effect") require("./safe_output_effect_plan.cjs").assertEffectPlanScope(effectPlan, normalizedInputs, scope);
    else if (effectPlan !== undefined) fail("invalid_preparation");
    // Snapshot once: authorization and execution must use the same normalized
    // data; the envelope intentionally retains only its digest.
    const inputDigest = canonicalDigest(normalizedInputs);
    const invocationId = stableInvocationId({ caller, output, batchDigest, recordIndex });
    closed(scope, ["binding", "refs"], ["limits_digest"]);
    if (!["invocation", "resolved_effect"].includes(scope.binding)) fail("invalid_preparation");
    const scopeSnapshot = { binding: scope.binding, refs: references(scope.refs), ...(scope.limits_digest === undefined ? {} : { limits_digest: hash(scope.limits_digest) }) };
    closed(context, [], ["policy_digest", "evidence_refs", "claim_id", "delivery_id"]);
    const contextSnapshot = {};
    if (context.policy_digest !== undefined) contextSnapshot.policy_digest = hash(context.policy_digest);
    if (context.evidence_refs !== undefined) contextSnapshot.evidence_refs = references(context.evidence_refs);
    for (const key of ["claim_id", "delivery_id"]) {
      if (context[key] !== undefined) {
        if (!CODE.test(text(context[key], 256))) fail("invalid_preparation");
        contextSnapshot[key] = context[key];
      }
    }
    const envelope = freeze({
      version: VERSION,
      request_id: crypto.randomUUID(),
      invocation_id: invocationId,
      caller: { ...caller },
      output: { ...output },
      batch: { digest: batchDigest, record_index: recordIndex },
      input_digest: inputDigest,
      scope: scopeSnapshot,
      context: contextSnapshot,
    });
    if (Buffer.byteLength(canonical(envelope)) + 256 > MAX_REQUEST_BYTES) fail("invalid_preparation");
    prepared.add(envelope);
    return envelope;
  } catch {
    fail("invalid_preparation");
  }
}

function configuration(requirement, env) {
  try {
    if (requirement?.from_environment === true) {
      closed(requirement, ["from_environment"], ["source"]);
      requirement = { endpoint: env.GH_AW_DEFAULT_SAFE_OUTPUT_AUTHORIZATION_URL, audience: env.GH_AW_DEFAULT_SAFE_OUTPUT_AUTHORIZATION_AUDIENCE, ...(requirement.source === undefined ? {} : { source: requirement.source }) };
    }
    closed(requirement, ["endpoint", "audience"], ["source"]);
    const endpoint = text(requirement.endpoint, 2048);
    const audience = text(requirement.audience, 2048);
    const url = new URL(endpoint);
    if (!/^https:\/\/[^/\\?#\s]+(?:\/[^\\?#\s]*)?$/.test(endpoint) || url.protocol !== "https:" || !url.hostname || url.username || url.password || url.search || url.hash || endpoint.includes("${{") || audience.includes("${{"))
      fail("invalid_configuration");
    if (requirement.source !== undefined && !["global", "local"].includes(requirement.source)) fail("invalid_configuration");
    return { endpoint, audience, source: requirement.source || "local" };
  } catch {
    fail("invalid_configuration");
  }
}

function recordAuthorizationSpan(phase, startMs, projection, coreApi) {
  const traceId = process.env.GITHUB_AW_OTEL_TRACE_ID || "";
  if (!/^[a-f0-9]{32}$/.test(traceId) || /^0{32}$/.test(traceId)) return;
  const warn = () => {
    try {
      coreApi?.warning?.("ERR_SAFE_OUTPUT_AUTHORIZATION: telemetry_failed");
    } catch {
      // Telemetry must not change authorization or execution outcomes.
    }
  };
  try {
    const attributes = {
      "gh-aw.safe_output.gate.phase": phase,
      "gh-aw.safe_output.gate.state": projection.state,
      "gh-aw.safe_output.gate.reason_code": projection.reason_code,
    };
    for (const key of ["executor", "binding", "requirement_source"]) {
      if (projection[key] !== undefined) attributes[`gh-aw.safe_output.gate.${key}`] = projection[key];
    }
    // Mirror synchronously; never wait for network export inside a live grant.
    void require("./otlp.cjs")
      .logSpan(`safe_output.authorization.${phase}`, attributes, { startMs, endMs: Date.now(), isError: ["error", "denied"].includes(projection.state), onError: warn })
      .catch(warn);
  } catch {
    warn();
  }
}

async function boundedRequest(operation, deadline, now, coreApi, phase) {
  const startMs = Date.now();
  let projection = { state: "succeeded", reason_code: "completed" };
  try {
    return await withRetry(
      async signal => {
        if (!signal) fail("invalid_preparation");
        try {
          return await operation(signal);
        } catch (error) {
          throw error instanceof SafeOutputAuthorizationError ? error : new SafeOutputAuthorizationError("authority_unavailable");
        }
      },
      { maxRetries: 0, shouldRetry: () => false, timeoutMs: HTTP_TIMEOUT_MS, deadline, now, timeoutError: () => new SafeOutputAuthorizationError("timeout"), logger: coreApi },
      "safe-output authorization request"
    );
  } catch (error) {
    const original = error instanceof Error && "originalError" in error ? error.originalError : error;
    const failure = original instanceof SafeOutputAuthorizationError ? original : new SafeOutputAuthorizationError("authority_unavailable");
    projection = { state: "error", reason_code: failure.reason_code };
    throw failure;
  } finally {
    recordAuthorizationSpan(phase, startMs, projection, coreApi);
  }
}

async function jsonResponse(response, maxBytes) {
  if (response.status !== 200 || response.redirected) {
    void response.body?.cancel().catch(() => {});
    fail("authority_unavailable");
  }
  if (!/^application\/json(?:\s*;\s*charset\s*=\s*(?:"utf-8"|utf-8))?\s*$/i.test(response.headers.get("content-type") || "")) {
    void response.body?.cancel().catch(() => {});
    fail("invalid_response");
  }
  try {
    return parseStrictJSON(await readBoundedBody(response, maxBytes), { maxBytes });
  } catch {
    fail("invalid_response");
  }
}

async function acquireToken({ core, env, config, fetchImpl, deadline, now, wallNow }) {
  try {
    core?.debug?.("Safe-output authorization: requesting OIDC credential");
    const bootstrap = env.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
    if (!bootstrap || !env.ACTIONS_ID_TOKEN_REQUEST_URL || typeof core?.setSecret !== "function") fail("missing_credentials");
    core.setSecret(bootstrap);
    const requestURL = text(env.ACTIONS_ID_TOKEN_REQUEST_URL, 8192);
    const url = new URL(requestURL);
    if (!/^https:\/\/[^/\\?#\s]+(?:[/?][^\\#\s]*)?$/.test(requestURL) || url.protocol !== "https:" || url.username || url.password || url.hash) fail("credential_error");
    url.searchParams.set("audience", config.audience);
    return await boundedRequest(
      async signal => {
        const response = await fetchImpl(url, { method: "GET", headers: { authorization: `bearer ${bootstrap}`, accept: "application/json" }, redirect: "manual", signal });
        const body = await jsonResponse(response, MAX_RESPONSE_BYTES);
        if (typeof body?.value !== "string" || body.value.length > 7168 || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(body.value)) fail("credential_error");
        core.setSecret(body.value);
        const payload = parseStrictJSON(Buffer.from(body.value.split(".")[1], "base64url").toString("utf8"), { maxBytes: MAX_RESPONSE_BYTES });
        if (!Number.isSafeInteger(payload.exp) || payload.exp * 1000 <= wallNow() || payload.aud !== config.audience) fail("credential_error");
        return body.value;
      },
      deadline,
      now,
      core,
      "oidc"
    );
  } catch (error) {
    if (error instanceof SafeOutputAuthorizationError && ["missing_credentials", "timeout"].includes(error.reason_code)) throw error;
    fail("credential_error");
  }
}

function utc(value) {
  if (typeof value !== "string" || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$/.test(value)) fail("invalid_validity");
  const milliseconds = Date.parse(value);
  if (!Number.isFinite(milliseconds) || new Date(milliseconds).toISOString() !== value) fail("invalid_validity");
  return milliseconds;
}

function validateDecision(decision, invocation, requestDigest, now) {
  try {
    closed(decision, ["version", "decision", "request_digest", "invocation_id", "run_attempt", "policy_revision", "decision_id", "issued_at", "expires_at"]);
  } catch {
    fail("invalid_response");
  }
  if (decision.version !== VERSION || !["allow", "deny"].includes(decision.decision)) fail("invalid_response");
  if (decision.request_digest !== requestDigest || decision.invocation_id !== invocation.invocation_id || decision.run_attempt !== invocation.caller.run_attempt) fail("binding_mismatch");
  if (typeof decision.policy_revision !== "string" || !CODE.test(decision.policy_revision) || typeof decision.decision_id !== "string" || !CODE.test(decision.decision_id)) fail("invalid_response");
  const issued = utc(decision.issued_at);
  const expires = utc(decision.expires_at);
  if (issued > now || expires <= issued || expires - issued > MAX_VALIDITY_MS || expires - now < MIN_VALIDITY_MS || expires - now > MAX_VALIDITY_MS) fail("invalid_validity");
  return { decision_id: decision.decision_id, policy_revision: decision.policy_revision, issued_at: decision.issued_at, expires_at: decision.expires_at };
}

function authorizationEvidencePath(env = process.env) {
  if (!env.RUNNER_TEMP) throw new Error("Safe-output authorization requires trusted evidence storage");
  return path.join(env.RUNNER_TEMP, "gh-aw", "safe-output-authorization.jsonl");
}

// Append-only records are individually fsynced before returning permission.
// The compiler/manager owns the path; no authority or agent field selects it.
function persist(evidencePath, projection) {
  let fd;
  try {
    if (typeof evidencePath !== "string" || !evidencePath) fail("evidence_failed");
    fd = fs.openSync(evidencePath, fs.constants.O_WRONLY | fs.constants.O_APPEND | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW, 0o600);
    if (!fs.fstatSync(fd).isFile()) fail("evidence_failed");
    fs.writeFileSync(fd, `${canonical(projection)}\n`, "utf8");
    fs.fsyncSync(fd);
    fs.closeSync(fd);
    fd = undefined;
    // Persist creation of a new directory entry as well as file contents.
    const directory = fs.openSync(path.dirname(evidencePath), fs.constants.O_RDONLY);
    try {
      fs.fsyncSync(directory);
    } finally {
      fs.closeSync(directory);
    }
  } catch {
    fail("evidence_failed");
  } finally {
    if (fd !== undefined) fs.closeSync(fd);
  }
}

/**
 * Required authorization is fail closed. Callers must never enter an executor
 * for skipped_staged or a thrown error, and must not mutate approved inputs.
 * @param {{core?: any, requirement?: any, invocation?: any, staged?: boolean, evidencePath?: string,
 * env?: NodeJS.ProcessEnv, fetchImpl?: typeof fetch, now?: () => number, wallNow?: () => number}} options
 */
async function authorizePreparedInvocation({ core, requirement, invocation, staged = false, evidencePath, env = process.env, fetchImpl = fetch, now = () => performance.now(), wallNow = () => Date.now() }) {
  if (staged === true) {
    core?.debug?.("Safe-output authorization: skipped_staged");
    return { state: "skipped_staged", reason_code: "skipped_staged" };
  }
  if (requirement === undefined || requirement === null) {
    core?.debug?.("Safe-output authorization: not_configured");
    return { state: "not_configured", reason_code: "not_configured" };
  }
  const startMs = Date.now();
  let projection = { state: "error", reason_code: "invalid_preparation" };
  try {
    if (typeof staged !== "boolean" || !prepared.has(invocation)) fail("invalid_preparation");
    const config = configuration(requirement, env);
    projection = {
      ...projection,
      invocation_id: invocation.invocation_id,
      output_type: invocation.output.type,
      output_name: invocation.output.name,
      executor: invocation.output.executor,
      binding: invocation.scope.binding,
      requirement_source: config.source,
      run_attempt: invocation.caller.run_attempt,
    };
    core?.debug?.(`Safe-output authorization: prepared invocation=${projection.invocation_id} executor=${projection.executor} binding=${projection.binding} source=${projection.requirement_source}`);
    // Windows cannot fsync the containing directory through Node's file API.
    // Refuse before consuming a request or acquiring credentials.
    if (process.platform === "win32") fail("evidence_failed");
    if (consumed.has(invocation)) fail("already_requested");
    consumed.add(invocation);
    if (!evidencePath) fail("evidence_failed");
    const requestDigest = canonicalDigest(invocation);
    projection.request_digest = requestDigest;
    const body = canonical({ version: VERSION, request_digest: requestDigest, invocation });
    if (Buffer.byteLength(body) > MAX_REQUEST_BYTES) fail("invalid_preparation");
    const start = now();
    const wallStart = wallNow();
    const deadline = start + TOTAL_TIMEOUT_MS;
    // Anchor UTC freshness to monotonic elapsed time, resisting wall-clock rollback.
    const currentUTC = () => Math.max(wallNow(), wallStart + Math.max(0, now() - start));
    const token = await acquireToken({ core, env, config, fetchImpl, deadline, now, wallNow: currentUTC });
    core?.debug?.(`Safe-output authorization: requesting decision invocation=${projection.invocation_id}`);
    const decision = await boundedRequest(
      async signal => {
        const response = await fetchImpl(config.endpoint, {
          method: "POST",
          headers: { authorization: `bearer ${token}`, "content-type": "application/json", "cache-control": "no-store" },
          body,
          redirect: "manual",
          signal,
        });
        return jsonResponse(response, MAX_RESPONSE_BYTES);
      },
      deadline,
      now,
      core,
      "authority"
    );
    const metadata = validateDecision(decision, invocation, requestDigest, currentUTC());
    projection = { ...projection, ...metadata };
    if (decision.decision === "deny") fail("denied");
    projection = { ...projection, state: "allowed", reason_code: "allowed" };
    persist(evidencePath, projection);
    if (now() >= deadline || utc(metadata.expires_at) - currentUTC() < MIN_VALIDITY_MS) fail("invalid_validity");
    core?.debug?.(`Safe-output authorization: allowed invocation=${projection.invocation_id} evidence=recorded`);
    freeze(projection);
    freshness.set(projection, { now, currentUTC, deadline, evidencePath });
    return projection;
  } catch (error) {
    const reason = error instanceof SafeOutputAuthorizationError ? error.reason_code : "authority_unavailable";
    projection = { ...projection, state: reason === "denied" ? "denied" : "error", reason_code: reason };
    try {
      if (evidencePath) persist(evidencePath, projection);
    } catch {
      projection.reason_code = "evidence_failed";
      projection.state = "error";
    }
    core?.debug?.(`Safe-output authorization: blocked reason=${projection.reason_code}`);
    throw new SafeOutputAuthorizationError(projection.reason_code, freeze(projection));
  } finally {
    recordAuthorizationSpan("decision", startMs, projection, core);
  }
}

// Batch gates collect fresh decisions at the live executor boundary, then
// recheck every decision before entering ANY author-provided batch code.
function assertAuthorizationFresh({ authorization }) {
  const proof = authorization && freshness.get(authorization);
  if (!proof || authorization.state !== "allowed") throw new SafeOutputAuthorizationError("invalid_preparation");
  if (proof.now() >= proof.deadline || utc(authorization.expires_at) - proof.currentUTC() < MIN_VALIDITY_MS) {
    let projection = { ...authorization, state: "error", reason_code: "invalid_validity" };
    try {
      persist(proof.evidencePath, projection);
    } catch {
      projection = { ...projection, reason_code: "evidence_failed" };
    }
    throw new SafeOutputAuthorizationError(projection.reason_code, freeze(projection));
  }
  return authorization;
}

function assertAuthorizationBatchFresh({ authorizations }) {
  if (!Array.isArray(authorizations) || authorizations.length > 16384) fail("invalid_preparation");
  for (const authorization of authorizations) assertAuthorizationFresh({ authorization });
  return authorizations;
}

function recordAuthorizationOutcome({ evidencePath, authorization, operationStatus, core }) {
  try {
    if (!["confirmed", "partial", "failed", "indeterminate", "success", "skipped", "staged"].includes(operationStatus)) fail("evidence_failed");
    // Never persist arbitrary operation results, errors, or caller-added fields.
    const projection = {};
    for (const key of ["state", "reason_code", "invocation_id", "output_type", "output_name", "executor", "binding", "requirement_source", "run_attempt", "request_digest", "decision_id", "policy_revision", "issued_at", "expires_at"]) {
      if (authorization[key] !== undefined) projection[key] = authorization[key];
    }
    persist(evidencePath, { authorization: projection, operation_status: operationStatus });
    return { evidenceRecorded: true };
  } catch {
    try {
      core?.warning?.("ERR_SAFE_OUTPUT_AUTHORIZATION: post_effect_evidence_failed");
    } catch {
      // Logging failure must not change a confirmed or uncertain effect.
    }
    return { evidenceRecorded: false, reason_code: "post_effect_evidence_failed" };
  }
}

module.exports = {
  VERSION,
  MAX_REQUEST_BYTES,
  MAX_RESPONSE_BYTES,
  HTTP_TIMEOUT_MS,
  TOTAL_TIMEOUT_MS,
  MIN_VALIDITY_MS,
  MAX_VALIDITY_MS,
  SafeOutputAuthorizationError,
  canonicalDigest,
  buildAuthorizationCaller,
  stableInvocationId,
  prepareInvocation,
  authorizationEvidencePath,
  authorizePreparedInvocation,
  assertAuthorizationFresh,
  assertAuthorizationBatchFresh,
  recordAuthorizationOutcome,
};
