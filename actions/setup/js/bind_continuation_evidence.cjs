// @ts-check
/// <reference types="@actions/github-script" />

const crypto = require("crypto");
const fs = require("fs");

const POSITIVE_DECIMAL = /^[1-9][0-9]*$/;

/**
 * Bind the final agent_output.json bytes to the producing run attempt.
 * Returns `<run_id>:<run_attempt>:<sha256>`, or null when there is no output to bind.
 * Only a regular file is hashed; a symlink or other file type is rejected.
 * @param {{path: string, runId: string | undefined, runAttempt: string | undefined}} options
 * @returns {string | null}
 */
function bindContinuationEvidence({ path, runId, runAttempt }) {
  if (!path) throw new Error("continuation_evidence_path_missing");
  if (!POSITIVE_DECIMAL.test(String(runId || "")) || !POSITIVE_DECIMAL.test(String(runAttempt || ""))) {
    throw new Error("continuation_evidence_context_invalid");
  }
  let fd;
  try {
    fd = fs.openSync(path, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
  } catch (error) {
    if (/** @type {NodeJS.ErrnoException} */ error.code === "ENOENT") return null;
    throw new Error("continuation_evidence_unreadable", { cause: error });
  }
  try {
    if (!fs.fstatSync(fd).isFile()) throw new Error("continuation_evidence_not_regular_file");
    // Hash exactly the bytes read once from the open descriptor.
    const bytes = fs.readFileSync(fd);
    if (bytes.length === 0) return null;
    return `${runId}:${runAttempt}:${crypto.createHash("sha256").update(bytes).digest("hex")}`;
  } finally {
    fs.closeSync(fd);
  }
}

/**
 * Agent-job step: expose `continuation_evidence` for routed request_continuation.
 * Missing, empty or unreadable output leaves the output unset, so no handoff can occur.
 * @param {{core: typeof import('@actions/core'), env?: NodeJS.ProcessEnv}} options
 */
async function main({ core, env = process.env }) {
  let evidence;
  try {
    evidence = bindContinuationEvidence({ path: env.GH_AW_CONTINUATION_AGENT_OUTPUT || "", runId: env.GITHUB_RUN_ID, runAttempt: env.GITHUB_RUN_ATTEMPT });
  } catch (error) {
    core.warning(`Continuation evidence unavailable: ${error instanceof Error ? error.message : String(error)}`);
    return;
  }
  if (evidence === null) {
    core.info("No agent output to bind; continuation routing has no evidence for this attempt.");
    return;
  }
  core.setOutput("continuation_evidence", evidence);
  core.info("Bound continuation evidence to this run attempt.");
}

module.exports = { bindContinuationEvidence, main };
