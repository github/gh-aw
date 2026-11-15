// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("node:fs");
const path = require("node:path");
const { TMP_GH_AW_PATH } = require("./constants.cjs");
const { ERR_VALIDATION, ERR_SYSTEM } = require("./error_codes.cjs");
const { redactArtifactContent } = require("./redact_secrets.cjs");
const { getSecretValues } = require("./redact_evals_results.cjs");
const { getOTLPSecretValues } = require("./otlp_header_helpers.cjs");

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
  const workflowSha = env.GH_AW_WRITE_GATE_WORKFLOW_SHA;
  const workflowRef = env.GH_AW_WRITE_GATE_WORKFLOW_REF;
  if (
    !repository ||
    !/^[^/\s]+\/[^/\s]+$/.test(repository) ||
    !runId ||
    !/^[1-9]\d*$/.test(runId) ||
    !attempt ||
    !/^[1-9]\d*$/.test(attempt) ||
    !headSha ||
    !/^[a-f0-9]{40}$/.test(headSha) ||
    !workflowSha ||
    !/^[a-f0-9]{40}$/.test(workflowSha) ||
    !workflowRef?.startsWith(`${repository}/.github/workflows/`)
  ) {
    throw new Error(`${ERR_VALIDATION}: Cannot record write gate without repository, run, attempt, and commit identity`);
  }
  const result = env.GH_AW_WRITE_GATE_JOB_RESULT || "unknown";
  const verdict = env.GH_AW_WRITE_GATE;
  const validVerdict = ["allow", "deny", "partial"].includes(verdict || "");
  const record = {
    version: 1,
    repository,
    run_id: runId,
    run_attempt: attempt,
    head_sha: headSha,
    workflow_sha: workflowSha,
    workflow_ref: workflowRef,
    safe_outputs_result: result,
    verdict: validVerdict ? verdict : "deny",
    reason: validVerdict ? env.GH_AW_WRITE_GATE_REASON || "unspecified" : "gate_unavailable",
  };
  for (const [field, name] of Object.entries({
    pending_writes_sha256: "GH_AW_PENDING_WRITES_SHA256",
    write_decisions_sha256: "GH_AW_WRITE_GATE_DECISIONS_SHA256",
    pending_write_inputs_sha256: "GH_AW_PENDING_WRITE_INPUTS_SHA256",
    safe_output_items_sha256: "GH_AW_WRITE_GATE_ITEMS_SHA256",
  })) {
    const value = env[name];
    if (!value) continue;
    if (!/^[a-f0-9]{64}$/.test(value)) throw new Error(`${ERR_VALIDATION}: Recorded replay integrity pin is malformed`);
    record[field] = value;
  }
  return record;
}

async function main() {
  // github.sha can identify a merge commit; Actions run.head_sha is a separate
  // identity. Record both it and the actual workflow-definition commit.
  const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
  const record = buildWriteGateRecord({ ...process.env, GH_AW_WRITE_GATE_HEAD_SHA: run.head_sha });
  const directory = path.join(TMP_GH_AW_PATH, "write-gate");
  const serialized = redactArtifactContent(JSON.stringify(record), getSecretValues(), getOTLPSecretValues()).content;
  try {
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, "write_gate.json"), serialized, { flag: "wx" });
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Could not persist the write gate verdict`, { cause: error });
  }
  core.info(`Recorded write gate verdict: ${record.verdict}`);
}

module.exports = { main, buildWriteGateRecord };
