// @ts-check
/// <reference types="@actions/github-script" />

const { createHash } = require("node:crypto");
const { ERR_API, ERR_CONFIG, ERR_PERMISSION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { buildServiceRequestHeaders } = require("./send_otlp_span.cjs");

const MAX_ADMISSION_BYTES = 16 * 1024;
const MAX_OIDC_BYTES = 32 * 1024;

/** @returns {never} */
function reject(message) {
  throw new Error(`${ERR_PERMISSION}: Gateway workflow admission rejected: ${message}`);
}

function positiveId(value) {
  return typeof value === "string" && /^[1-9][0-9]{0,19}$/.test(value);
}

function httpsConfiguration(value, name, base = false) {
  try {
    if (typeof value !== "string" || !value || (base && value.endsWith("/"))) throw new Error();
    const url = new URL(value);
    const canonical = base && url.pathname === "/" ? url.href.slice(0, -1) : url.href;
    if (url.protocol !== "https:" || url.username || url.password || url.href.includes("?") || url.href.includes("#") || canonical !== value) throw new Error();
    return value;
  } catch {
    throw new Error(`${ERR_CONFIG}: Set ${name} to a canonical HTTPS ${base ? "base without a trailing slash" : "endpoint"} without credentials, query, or fragment.`);
  }
}

/** @param {NodeJS.ProcessEnv} env */
function admissionBinding(env) {
  const binding = {
    repository: env.GH_AW_GATEWAY_REPOSITORY || "",
    repository_id: env.GH_AW_GATEWAY_REPOSITORY_ID || "",
    repository_owner_id: env.GH_AW_GATEWAY_OWNER_ID || "",
    workflow_path: env.GH_AW_GATEWAY_WORKFLOW_PATH || "",
    workflow_ref: env.GH_AW_GATEWAY_WORKFLOW_REF || "",
    workflow_sha: env.GH_AW_GATEWAY_WORKFLOW_SHA || "",
    workflow_hash: env.GH_AW_GATEWAY_WORKFLOW_HASH || "",
    run_id: env.GH_AW_GATEWAY_RUN_ID || "",
    run_attempt: env.GH_AW_GATEWAY_RUN_ATTEMPT || "",
    actor_id: env.GH_AW_GATEWAY_ACTOR_ID || "",
    ref: env.GH_AW_GATEWAY_REF || "",
    event_name: env.GH_AW_GATEWAY_EVENT_NAME || "",
    event_type: env.GH_AW_GATEWAY_EVENT_TYPE || "",
  };
  const workflowId = binding.workflow_path.slice(".github/workflows/".length, -".lock.yml".length);
  if (
    !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(binding.repository) ||
    ![binding.repository_id, binding.repository_owner_id, binding.run_id, binding.run_attempt, binding.actor_id].every(positiveId) ||
    !/^\.github\/workflows\/[^/\\\x00-\x1f\x7f]+\.lock\.yml$/u.test(binding.workflow_path) ||
    !/^[a-f0-9]{40}$/.test(binding.workflow_sha) ||
    !/^[a-f0-9]{64}$/.test(binding.workflow_hash) ||
    !/^refs\/heads\/[^\x00-\x20\x7f]+$/u.test(binding.ref) ||
    binding.workflow_ref !== `${binding.repository}/${binding.workflow_path}@${binding.ref}` ||
    binding.event_name !== "repository_dispatch" ||
    binding.event_type !== `gh-aw-webhook-${createHash("sha256").update(workflowId).digest("hex").slice(0, 24)}`
  ) {
    reject("GitHub-owned repository, workflow revision, run, and route context and the compiler-generated frontmatter hash are required");
  }
  return binding;
}

/**
 * @param {Response} response
 * @param {number} limit
 */
async function boundedBytes(response, limit) {
  const length = response.headers.get("content-length");
  if (length !== null && (!/^[0-9]+$/.test(length) || Number(length) > limit)) {
    await response.body?.cancel();
    throw new Error(`${ERR_API}: Response exceeds its byte limit.`);
  }
  const reader = response.body?.getReader();
  if (!reader) throw new Error(`${ERR_API}: Response body is missing.`);
  const chunks = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) {
        await reader.cancel();
        throw new Error(`${ERR_API}: Response exceeds its byte limit.`);
      }
      chunks.push(Buffer.from(value));
    }
  } finally {
    reader.releaseLock();
  }
  return Buffer.concat(chunks);
}

/**
 * @param {string} url
 * @param {RequestInit} options
 * @param {number} limit
 * @param {typeof fetch} fetchImpl
 * @param {string} operation
 */
async function requestBytes(url, options, limit, fetchImpl, operation) {
  let status = 0;
  try {
    const response = await fetchImpl(url, { ...options, redirect: "error", signal: AbortSignal.timeout(30000), cache: "no-store" });
    status = response.status;
    if (status !== 200) {
      await response.body?.cancel();
      throw new Error(`${ERR_API}: Request was not accepted.`);
    }
    return await boundedBytes(response, limit);
  } catch {
    throw new Error(`${ERR_API}: Gateway ${operation} failed${status ? ` (HTTP ${status})` : ""}. Check service availability, access, and response limits; admission remains blocked.`);
  }
}

function parseObject(bytes, operation) {
  try {
    const parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
    return parsed;
  } catch {
    reject(`${operation} must return a valid UTF-8 JSON object`);
  }
}

function oidcClaims(token, audience, binding) {
  if (typeof token !== "string" || Buffer.byteLength(token, "utf8") > MAX_OIDC_BYTES || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token)) {
    reject("GitHub did not return a bounded OIDC JWT");
  }
  const claims = parseObject(Buffer.from(token.split(".")[1], "base64url"), "GitHub OIDC issuance");
  const signedFields = ["repository", "repository_id", "repository_owner_id", "workflow_ref", "workflow_sha", "run_id", "run_attempt", "actor_id", "ref", "event_name"];
  if (
    claims.aud !== audience ||
    signedFields.some(field => claims[field] !== binding[field]) ||
    !positiveId(claims.check_run_id) ||
    !Number.isSafeInteger(claims.exp) ||
    claims.exp <= Date.now() / 1000 ||
    !Number.isSafeInteger(claims.nbf) ||
    claims.nbf > Date.now() / 1000
  ) {
    reject("the issued OIDC audience and workflow/run claims must match this admission");
  }
  // This consistency check is not signature verification; the backend verifies the JWT.
  return claims;
}

/**
 * @param {typeof core} actionsCore
 * @param {NodeJS.ProcessEnv} env
 * @param {typeof fetch} fetchImpl
 */
async function verifyGatewayAdmission(actionsCore, env, fetchImpl = fetch) {
  const endpoint = httpsConfiguration(env.GH_AW_GATEWAY_GET_TOKEN_URL, "GH_AW_GATEWAY_GET_TOKEN_URL");
  const audienceBase = httpsConfiguration(env.GH_AW_GATEWAY_OIDC_AUDIENCE, "GH_AW_GATEWAY_OIDC_AUDIENCE", true);
  const binding = admissionBinding(env);
  const workflowHash = binding.workflow_hash;
  const audience = `${audienceBase}/gh-aw/gateway/v1/workflow/sha256/${workflowHash}`;
  const correlationHeaders = buildServiceRequestHeaders(env);
  const jobId = env.GITHUB_JOB;
  let idToken;
  try {
    idToken = await actionsCore.getIDToken(audience);
  } catch {
    throw new Error(`${ERR_API}: Unable to obtain hash-bound GitHub OIDC admission. Check the admission job's id-token: write permission.`);
  }
  if (typeof idToken === "string" && idToken) actionsCore.setSecret(idToken);
  const claims = oidcClaims(idToken, audience, binding);
  const request = { schema_version: 1, ...binding, check_run_id: claims.check_run_id, ...(jobId ? { job_id: jobId } : {}) };
  const body = JSON.stringify(request);
  if (Buffer.byteLength(body, "utf8") > MAX_ADMISSION_BYTES) reject("the getToken request exceeds its UTF-8 byte limit");
  const decision = parseObject(
    await requestBytes(
      endpoint,
      { method: "POST", headers: { ...correlationHeaders, Authorization: `Bearer ${idToken}`, Accept: "application/json", "Content-Type": "application/json" }, body },
      MAX_ADMISSION_BYTES,
      fetchImpl,
      "getToken admission"
    ),
    "getToken admission"
  );
  const keys = [...Object.keys(request), "allowed"];
  if (decision.allowed !== true || Object.keys(decision).length !== keys.length || Object.keys(decision).some(key => !keys.includes(key)) || Object.keys(request).some(key => decision[key] !== request[key])) {
    reject("getToken must explicitly approve the exact closed workflow/run binding without additional credential fields");
  }
  return { verified: true, workflow_hash: workflowHash };
}

async function main() {
  core.setOutput("verified", "false");
  try {
    await verifyGatewayAdmission(core, process.env);
    core.setOutput("verified", "true");
    core.info("Verified the executing gateway workflow version with the admission server.");
  } catch (error) {
    core.setFailed(getErrorMessage(error));
  }
}

module.exports = { main, verifyGatewayAdmission };
