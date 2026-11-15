// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { TMP_GH_AW_PATH } = require("./constants.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

const REASON = /^[a-z0-9_.-]{1,64}$/;
const MAX_BYTES = 16 * 1024 * 1024;
const DECISIONS_FILE = path.join(TMP_GH_AW_PATH, "write-gate", "decisions.json");

/** @param {string} [filename] */
function initialize(filename = DECISIONS_FILE) {
  const directory = path.dirname(filename);
  try {
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    const stat = fs.lstatSync(directory);
    if (!stat.isDirectory() || stat.isSymbolicLink()) throw new Error(`${ERR_VALIDATION}: Write gate requires a regular private directory`);
    try {
      fs.unlinkSync(filename);
    } catch (error) {
      if (!error || typeof error !== "object" || Object.getOwnPropertyDescriptor(error, "code")?.value !== "ENOENT") throw error;
    }
  } catch (cause) {
    throw new Error(`${ERR_VALIDATION}: Write gate directory could not be initialized`, { cause });
  }
  core.exportVariable("GH_AW_WRITE_DECISIONS", filename);
  core.setOutput("path", filename);
}

/**
 * @param {{filename?: string, outcomes: unknown, dryRun?: boolean}} options
 * @returns {{decision: string, reason: string, error: boolean, sha256?: string}}
 */
function evaluate({ filename, outcomes, dryRun = false }) {
  if (dryRun) return { decision: "deny", reason: "dry_run", error: false };
  const denied = { decision: "deny", reason: "gate_error", error: true };
  if (!filename || !Array.isArray(outcomes) || !outcomes.length || outcomes.some(outcome => outcome !== "success")) return denied;
  let descriptor;
  try {
    descriptor = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    const stat = fs.fstatSync(descriptor);
    if (!stat.isFile() || stat.size > MAX_BYTES) return denied;
    const bytes = fs.readFileSync(descriptor);
    if (bytes.length !== stat.size || bytes.length > MAX_BYTES) return denied;
    const value = parseStrictJSON(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes), { maxBytes: MAX_BYTES });
    if (!value || typeof value !== "object" || Array.isArray(value) || !["allow", "deny", "partial"].includes(value.decision) || typeof value.reason !== "string" || !REASON.test(value.reason)) return denied;
    if (value.decision === "partial" && !Array.isArray(value.items)) return denied;
    return { decision: value.decision, reason: value.reason, error: false, sha256: createHash("sha256").update(bytes).digest("hex") };
  } catch {
    return denied;
  } finally {
    if (descriptor !== undefined) fs.closeSync(descriptor);
  }
}

function main() {
  let outcomes;
  try {
    outcomes = parseStrictJSON(process.env.GH_AW_WRITE_GATE_STEP_OUTCOMES || "null");
  } catch {
    outcomes = null;
  }
  const verdict = evaluate({ filename: process.env.GH_AW_WRITE_DECISIONS, outcomes, dryRun: process.env.GH_AW_WRITE_GATE_DRY_RUN === "true" });
  core.setOutput("write_gate", verdict.decision);
  core.setOutput("write_gate_reason", verdict.reason);
  core.setOutput("decisions_sha256", verdict.sha256 || "");
  if (verdict.error) {
    core.setFailed(`${ERR_VALIDATION}: Write gate failed closed (gate_error). Every gate step should succeed and write decision/reason JSON to GH_AW_WRITE_DECISIONS; for example {"decision":"deny","reason":"policy_denied"}.`);
    return;
  }
  core.info(`Write gate: ${verdict.decision} (${verdict.reason})`);
}

module.exports = { initialize, evaluate, main, DECISIONS_FILE };
