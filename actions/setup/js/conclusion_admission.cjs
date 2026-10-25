// @ts-check
"use strict";

const { AdmissionError, readAdmissionConfig, buildAdmissionIdentity } = require("./admission_protocol.cjs");
const { requestAuthority } = require("./check_admission.cjs");
const { compactUnifiedSession, readCompactUnifiedSession, MAX_COMPACT_SESSION_BYTES, MAX_COMPACT_SESSION_EVENTS } = require("./compact_unified_session.cjs");
const { iterateLines, parseJsonlContent } = require("./jsonl_helpers.cjs");

const SESSION_FILE = "/tmp/gh-aw/usage/aw_session.jsonl";
const MAX_REQUEST_BYTES = 64 * 1024;
const FAILURE = "ERR_ADMISSION: Unable to deliver the unified session to the conclusion service; completed workflow results are unchanged.";

/** @param {NodeJS.ProcessEnv} env */
function readUsageArtifactIds(env) {
  const ids = [env.GH_AW_USAGE_ARTIFACT_RETRY_ID || "", env.GH_AW_USAGE_ARTIFACT_ID || ""].filter(Boolean);
  if (ids.some(id => !/^[1-9][0-9]{0,19}$/.test(id))) throw new AdmissionError();
  return [...new Set(ids)];
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @param {string} sessionFile
 */
function readCompactSession(env, sessionFile) {
  const encoded = env.GH_AW_ADMISSION_COMPACT_SESSION;
  if (encoded === undefined) return readCompactUnifiedSession(sessionFile);
  let compact;
  try {
    compact = JSON.parse(encoded);
  } catch {
    throw new AdmissionError();
  }
  if (
    compact === null ||
    typeof compact !== "object" ||
    compact.version !== 1 ||
    typeof compact.session !== "string" ||
    Buffer.byteLength(compact.session) > MAX_COMPACT_SESSION_BYTES ||
    !Number.isInteger(compact.included_events) ||
    compact.included_events < 0 ||
    compact.included_events > MAX_COMPACT_SESSION_EVENTS ||
    !Number.isSafeInteger(compact.omitted_events) ||
    compact.omitted_events < 0 ||
    !Number.isSafeInteger(compact.oversized_lines) ||
    compact.oversized_lines < 0 ||
    typeof compact.scan_truncated !== "boolean" ||
    typeof compact.truncated !== "boolean"
  ) {
    throw new AdmissionError();
  }
  const lines = compact.session === "" ? [] : [...iterateLines(compact.session)];
  if (lines.at(-1) === "") lines.pop();
  if ((!compact.session.endsWith("\n") && compact.session !== "") || lines.length !== compact.included_events) throw new AdmissionError();
  try {
    const projected = compactUnifiedSession(parseJsonlContent(compact.session));
    if (projected.session !== compact.session || projected.includedEvents !== compact.included_events) throw new AdmissionError();
  } catch {
    throw new AdmissionError();
  }
  return {
    version: compact.version,
    session: compact.session,
    includedEvents: compact.included_events,
    omittedEvents: compact.omitted_events,
    oversizedLines: compact.oversized_lines,
    scanTruncated: compact.scan_truncated,
    truncated: compact.truncated,
  };
}

/**
 * @param {{env?: NodeJS.ProcessEnv, fetchImpl?: typeof fetch, now?: () => number, mask?: (token: string) => void, sessionFile?: string}} [options]
 * @returns {Promise<{result: "skipped" | "delivered" | "unavailable"}>}
 */
async function sendConclusion({ env = process.env, sessionFile = SESSION_FILE, ...options } = {}) {
  const config = readAdmissionConfig(env);
  if (!config) return { result: "skipped" };
  const identity = JSON.parse(buildAdmissionIdentity(env));
  const usageArtifactIds = readUsageArtifactIds(env);
  const { version, session, includedEvents, omittedEvents, oversizedLines, scanTruncated, truncated } = readCompactSession(env, sessionFile);
  const body = JSON.stringify({
    ...identity,
    session,
    session_compaction: { version, included_events: includedEvents, omitted_events: omittedEvents, oversized_lines: oversizedLines, scan_truncated: scanTruncated, truncated },
    usage_artifact_ids: usageArtifactIds,
  });
  if (Buffer.byteLength(body) > MAX_REQUEST_BYTES) throw new AdmissionError();
  const endpoint = config.endpoint.replace(/\/api\/admission$/, "/api/conclusion");
  const { decision } = await requestAuthority({
    ...options,
    env,
    config: { ...config, endpoint },
    body,
    acceptedStatuses: [202, 204],
    parseResponse: async response => {
      void response.body?.cancel().catch(() => {
        // Discarded response bytes cannot change accepted delivery.
      });
      return true;
    },
  });
  return { result: decision ? "delivered" : "unavailable" };
}

/** @param {typeof import("@actions/core")} core */
async function main(core) {
  try {
    const result = await sendConclusion({ mask: token => core.setSecret(token) });
    if (result.result === "unavailable") core.warning(FAILURE);
    else if (result.result === "delivered") core.info("Admission conclusion: compact unified session accepted for processing.");
  } catch {
    core.warning(FAILURE);
  }
}

module.exports = { sendConclusion, main, readUsageArtifactIds, SESSION_FILE, MAX_REQUEST_BYTES, FAILURE };
