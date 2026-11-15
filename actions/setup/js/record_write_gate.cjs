// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("node:fs");
const path = require("node:path");
const { TMP_GH_AW_PATH } = require("./constants.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/**
 * Record compiler-owned job outputs, not agent-authored metadata.
 * This record does not assert per-item enforcement or artifact integrity.
 * @param {NodeJS.ProcessEnv} [env]
 */
function buildWriteGateRecord(env = process.env) {
  const repository = env.GH_AW_WRITE_GATE_REPOSITORY;
  const runId = env.GH_AW_WRITE_GATE_RUN_ID;
  const attempt = env.GH_AW_WRITE_GATE_RUN_ATTEMPT;
  const headSha = env.GH_AW_WRITE_GATE_HEAD_SHA;
  if (!repository || !/^[^/\s]+\/[^/\s]+$/.test(repository) || !runId || !/^[1-9]\d*$/.test(runId) || !attempt || !/^[1-9]\d*$/.test(attempt) || !headSha || !/^[a-f0-9]{40}$/.test(headSha)) {
    throw new Error(`${ERR_VALIDATION}: Cannot record write gate without repository, run, attempt, and commit identity`);
  }
  const result = env.GH_AW_WRITE_GATE_JOB_RESULT || "unknown";
  const verdict = env.GH_AW_WRITE_GATE;
  const validVerdict = result === "success" && ["allow", "deny", "partial"].includes(verdict || "");
  return {
    version: 1,
    repository,
    run_id: runId,
    run_attempt: attempt,
    head_sha: headSha,
    safe_outputs_result: result,
    verdict: validVerdict ? verdict : "deny",
    reason: validVerdict ? env.GH_AW_WRITE_GATE_REASON || "unspecified" : "gate_unavailable",
  };
}

function main() {
  const record = buildWriteGateRecord();
  const directory = path.join(TMP_GH_AW_PATH, "write-gate");
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(path.join(directory, "write_gate.json"), JSON.stringify(record), { flag: "wx" });
  core.info(`Recorded write gate verdict: ${record.verdict}`);
}

module.exports = { main, buildWriteGateRecord };
