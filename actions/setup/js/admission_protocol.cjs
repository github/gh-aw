// @ts-check
"use strict";

const { ERR_ADMISSION } = require("./error_codes.cjs");

const FAILURE = `${ERR_ADMISSION}: Workflow admission failed.`;
const CONTROL = /[\u0000-\u001f\u007f-\u009f]/u;
const DECIMAL = /^[1-9][0-9]*$/;

class AdmissionError extends Error {
  /** @param {boolean} [transient] */
  constructor(transient = false) {
    super(FAILURE);
    this.transient = transient;
  }
}

/** @param {string} value */
function isScalarString(value) {
  for (const char of value) {
    const point = char.codePointAt(0);
    if (point !== undefined && point >= 0xd800 && point <= 0xdfff) return false;
  }
  return true;
}

/** @param {string} value */
function parseCreditLimit(value) {
  if (!DECIMAL.test(value) || value.length > 10 || Number(value) > 2147483647) throw new AdmissionError();
  return Number(value);
}

/** @param {NodeJS.ProcessEnv} env */
function readAdmissionConfig(env) {
  const endpoint = env.GH_AW_DEFAULT_ADMISSION_URL || "";
  if (!endpoint) return null;
  let url;
  try {
    url = new URL(endpoint);
  } catch {
    throw new AdmissionError();
  }
  if (
    !/^https:\/\/[^/\\?#\s]+\/api\/admission$/.test(endpoint) ||
    CONTROL.test(endpoint) ||
    endpoint.includes("${{") ||
    url.protocol !== "https:" ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.pathname !== "/api/admission" ||
    url.search ||
    url.hash
  )
    throw new AdmissionError();
  const audience = env.GH_AW_DEFAULT_ADMISSION_AUDIENCE || endpoint;
  if (!isScalarString(audience) || CONTROL.test(audience) || audience.includes("${{")) throw new AdmissionError();
  const onOutage = env.GH_AW_DEFAULT_ADMISSION_ON_OUTAGE || "ignore";
  if (onOutage !== "ignore" && onOutage !== "block") throw new AdmissionError();
  return { endpoint, audience, onOutage };
}

/** @param {NodeJS.ProcessEnv} env */
function buildAdmissionIdentity(env) {
  const identity = {
    repo: env.GITHUB_REPOSITORY || "",
    workflow_id: env.GH_AW_INFO_WORKFLOW_ID || "",
    workflow_hash: env.GH_AW_ADMISSION_WORKFLOW_HASH || "",
    workflow_call_id: "",
    run_id: env.GITHUB_RUN_ID || "",
    run_attempt: env.GITHUB_RUN_ATTEMPT || "",
    ...(env.GITHUB_JOB ? { job_id: env.GITHUB_JOB } : {}),
    ref: env.GITHUB_REF || "",
    event_type: env.GITHUB_EVENT_NAME || "",
    actor: env.GITHUB_ACTOR || "",
    triggering_actor: env.GITHUB_TRIGGERING_ACTOR || "",
  };
  if (!DECIMAL.test(identity.run_id) || !DECIMAL.test(identity.run_attempt)) throw new AdmissionError();
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(identity.repo)) throw new AdmissionError();
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/\.github\/workflows\/[^/]+\.ya?ml@.+$/u.test(identity.workflow_id)) throw new AdmissionError();
  if (!/^[0-9a-f]{64}:[0-9a-f]{64}$/.test(identity.workflow_hash)) throw new AdmissionError();
  identity.workflow_call_id = `${identity.run_id}-${identity.run_attempt}:${identity.workflow_id}`;
  if (Object.values(identity).some(value => !value || value.length > 4096 || !isScalarString(value) || CONTROL.test(value))) throw new AdmissionError();
  const body = JSON.stringify(identity);
  if (Buffer.byteLength(body) > 8192) throw new AdmissionError();
  return body;
}

/**
 * This flat grammar retains numeric wire spelling and decoded
 * member names; JSON.parse alone loses both duplicate keys and exponent spelling.
 * @param {string} text
 */
function parseAdmissionDecision(text) {
  try {
    JSON.parse(text);
  } catch {
    throw new AdmissionError();
  }
  const string = '"(?:[^"\\\\\\u0000-\\u001f]|\\\\(?:["\\\\/bfnrt]|u[0-9a-fA-F]{4}))*"';
  const pair = `(${string})\\s*:\\s*([1-9][0-9]*)`;
  const match = new RegExp(`^[\\x20\\t\\r\\n]*\\{\\s*${pair}(?:\\s*,\\s*${pair})?(?:\\s*,\\s*${pair})?\\s*\\}[\\x20\\t\\r\\n]*$`).exec(text);
  if (!match) throw new AdmissionError();
  const allowed = new Set(["max_ai_credits", "max_ai_credits_threat_detection", "max_ai_credits_evals"]);
  /** @type {Map<string, string>} */
  const entries = new Map();
  for (let index = 1; index < match.length; index += 2) {
    if (match[index] === undefined) continue;
    const key = JSON.parse(match[index]);
    if (!allowed.has(key) || entries.has(key)) throw new AdmissionError();
    entries.set(key, match[index + 1]);
  }
  return {
    maxAiCredits: parseCreditLimit(entries.get("max_ai_credits") || ""),
    ...(entries.has("max_ai_credits_threat_detection") ? { threatDetectionMaxAiCredits: parseCreditLimit(entries.get("max_ai_credits_threat_detection") || "") } : {}),
    ...(entries.has("max_ai_credits_evals") ? { evalsMaxAiCredits: parseCreditLimit(entries.get("max_ai_credits_evals") || "") } : {}),
  };
}

/** @param {Response} response @param {number} limit */
async function readBoundedBody(response, limit) {
  if (!response.body) throw new AdmissionError();
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) throw new AdmissionError();
      chunks.push(value);
    }
  } finally {
    void reader.cancel().catch(() => {});
  }
  try {
    return new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(Buffer.concat(chunks));
  } catch {
    throw new AdmissionError();
  }
}

module.exports = { AdmissionError, FAILURE, isScalarString, parseCreditLimit, readAdmissionConfig, buildAdmissionIdentity, parseAdmissionDecision, readBoundedBody };
