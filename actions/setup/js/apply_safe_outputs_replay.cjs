// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Apply Safe Outputs Replay Driver
 *
 * Downloads the agent output artifact from a previous workflow run and replays
 * the safe outputs, applying them to the repository.
 *
 * Called from the `apply_safe_outputs` job in the agentic-maintenance workflow.
 *
 * Required environment variables:
 *   GH_AW_RUN_URL   - Run URL or run ID to replay safe outputs from.
 *                     Accepts a full URL (https://github.com/{owner}/{repo}/actions/runs/{runId})
 *                     or a plain run ID (digits only).
 *   GH_TOKEN        - GitHub token for artifact download via `gh run download`.
 *
 * Optional environment variables:
 *   GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG - If set, overrides the auto-generated handler config.
 */

const fs = require("fs");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_CONFIG, ERR_SYSTEM, ERR_VALIDATION, ERR_PARSE } = require("./error_codes.cjs");
const { AGENT_OUTPUT_FILENAME, TMP_GH_AW_PATH } = require("./constants.cjs");
const { parseLockFileHeaders } = require("./lock_file_headers.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { normalizeSafeOutputConfig, normalizeSafeOutputName } = require("./safe_output_names.cjs");
const { isDeepStrictEqual } = require("node:util");

/**
 * Parse a run ID from a run URL or plain run ID string.
 *
 * Accepts:
 *   - A plain run ID: "23560193313"
 *   - A full run URL: "https://github.com/{owner}/{repo}/actions/runs/{runId}"
 *   - A run URL with job: "https://github.com/{owner}/{repo}/actions/runs/{runId}/job/{jobId}"
 *
 * @param {string} runUrl - The run URL or run ID to parse
 * @returns {{ runId: string, owner: string|null, repo: string|null }} Parsed components
 */
function parseRunUrl(runUrl) {
  if (!runUrl || typeof runUrl !== "string") {
    throw new Error(`${ERR_VALIDATION}: run_url is required`);
  }

  const trimmed = runUrl.trim();

  // Check if it's a plain run ID (digits only)
  if (/^\d+$/.test(trimmed)) {
    return { runId: trimmed, owner: null, repo: null };
  }

  // Parse a full GitHub Actions URL
  // Pattern: https://github.com/{owner}/{repo}/actions/runs/{runId}[/job/{jobId}]
  const match = trimmed.match(/^https:\/\/github\.com\/([^/?#]+)\/([^/?#]+)\/actions\/runs\/(\d+)(?:\/job\/\d+)?(?:[?#].*)?$/);
  if (match) {
    return { runId: match[3], owner: match[1], repo: match[2] };
  }

  throw new Error(`${ERR_VALIDATION}: Cannot parse run ID from: ${trimmed}. Expected a plain run ID (digits only) or a GitHub Actions run URL (https://github.com/{owner}/{repo}/actions/runs/{runId}).`);
}

/**
 * Resolve the policy from the run's immutable compiled workflow, never its
 * agent artifact or today's workflow configuration.
 * @param {string} runId
 * @param {string} owner
 * @param {string} repo
 */
async function getReplayPolicy(runId, owner, repo) {
  if (!Number.isSafeInteger(Number(runId)) || Number(runId) < 1) throw new Error(`${ERR_VALIDATION}: Replay run ID must be a positive safe integer`);
  const { data: run } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: Number(runId) });
  const workflowPath = run.path?.split("@")[0];
  if (
    run.status !== "completed" ||
    typeof run.run_attempt !== "number" ||
    !Number.isSafeInteger(run.run_attempt) ||
    run.run_attempt < 1 ||
    !/^[a-f0-9]{40}$/.test(run.head_sha || "") ||
    !workflowPath ||
    !/^\.github\/workflows\/[^/]+\.ya?ml$/.test(workflowPath)
  ) {
    throw new Error(`${ERR_VALIDATION}: Replay requires a completed run with an immutable workflow identity`);
  }
  const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRunAttempt, {
    owner,
    repo,
    run_id: Number(runId),
    attempt_number: run.run_attempt,
    per_page: 100,
  });
  const recorded = await loadRecordedWriteGate(jobs, runId, owner, repo, run.run_attempt, run.head_sha, workflowPath);
  // Push uses the pushed commit; schedule uses the last default-branch commit;
  // workflow_dispatch uses the selected branch/tag revision. PR and reusable
  // contexts do not establish this equivalence and need the actual record.
  if (!recorded && !["push", "schedule", "workflow_dispatch"].includes(run.event)) {
    throw new Error(`${ERR_VALIDATION}: Actual workflow-definition identity is unavailable for this trigger; refusing legacy replay`);
  }
  const workflowSha = recorded?.record.workflow_sha || run.head_sha;
  const { data: source } = await github.rest.repos.getContent({ owner, repo, path: workflowPath, ref: workflowSha });
  if (Array.isArray(source) || source.type !== "file" || source.encoding !== "base64" || typeof source.content !== "string") {
    throw new Error(`${ERR_VALIDATION}: Original compiled workflow is unavailable; refusing replay`);
  }
  const compiledSource = Buffer.from(source.content, "base64").toString("utf8");
  const headers = parseLockFileHeaders(compiledSource.split(/\r?\n/, 3));
  if ((/^# gh-aw-metadata:/m.test(compiledSource) && !headers.metadata) || (headers.metadata?.dry_run !== undefined && typeof headers.metadata.dry_run !== "boolean")) {
    throw new Error(`${ERR_VALIDATION}: Original workflow metadata is malformed; refusing replay`);
  }
  if (headers.metadata?.dry_run === true) {
    throw new Error(`${ERR_VALIDATION}: Dry-run outputs cannot be replayed as writes`);
  }
  if (!headers.manifest) {
    const legacyHeader = headers.metadata || /^# frontmatter-hash: [a-f0-9]+$/m.test(compiledSource);
    if (recorded || !legacyHeader || /^# gh-aw-manifest:/m.test(compiledSource) || /\bwrite_gate\b|gate-steps|Record write gate verdict/.test(compiledSource)) {
      throw new Error(`${ERR_VALIDATION}: Original compiled workflow has no valid compiler manifest; refusing replay`);
    }
    return { run, gate: null, recorded: null };
  }
  if (headers.manifest.version !== 1) throw new Error(`${ERR_VALIDATION}: Unsupported original compiler manifest; refusing replay`);
  const gate = headers.manifest.write_gate;
  if (gate === undefined) {
    if (recorded || /\bwrite_gate\b|gate-steps|Record write gate verdict/.test(compiledSource)) throw new Error(`${ERR_VALIDATION}: Original write gate policy is missing; refusing replay`);
    return { run, gate: null, recorded: null };
  }
  if (
    !gate ||
    typeof gate !== "object" ||
    !("version" in gate) ||
    gate.version !== 1 ||
    !("record_job" in gate) ||
    typeof gate.record_job !== "string" ||
    !gate.record_job ||
    !("artifact_immutable" in gate) ||
    gate.artifact_immutable !== true
  ) {
    throw new Error(`${ERR_VALIDATION}: Original write gate policy is malformed; refusing replay`);
  }
  if (!recorded || recorded.recorderName !== gate.record_job) throw new Error(`${ERR_VALIDATION}: Missing or mismatched compiler-owned write gate recorder; refusing replay`);
  return { run, gate, recorded };
}

/**
 * @param {unknown} record
 * @param {{runId: string, repository: string, attempt: number, headSha: string, workflowPath?: string, workflowSha?: string}} identity
 */
function validateReplayWriteGateRecord(record, identity) {
  if (!record || typeof record !== "object" || Array.isArray(record)) throw new Error(`${ERR_VALIDATION}: Missing recorded write gate verdict; refusing replay`);
  const fields = ["version", "repository", "run_id", "run_attempt", "head_sha", "workflow_sha", "workflow_ref", "safe_outputs_result", "verdict", "reason"];
  if (Object.keys(record).length !== fields.length || fields.some(key => !Object.hasOwn(record, key))) throw new Error(`${ERR_VALIDATION}: Malformed recorded write gate verdict; refusing replay`);
  const value = Object.fromEntries(Object.entries(record));
  if (
    value.version !== 1 ||
    value.repository !== identity.repository ||
    value.run_id !== identity.runId ||
    value.run_attempt !== String(identity.attempt) ||
    value.head_sha !== identity.headSha ||
    typeof value.workflow_sha !== "string" ||
    !/^[a-f0-9]{40}$/.test(value.workflow_sha) ||
    (identity.workflowSha !== undefined && value.workflow_sha !== identity.workflowSha) ||
    typeof value.workflow_ref !== "string" ||
    !value.workflow_ref.startsWith(`${identity.repository}/${identity.workflowPath || ".github/workflows/"}${identity.workflowPath ? "@" : ""}`) ||
    typeof value.reason !== "string" ||
    !value.reason
  ) {
    throw new Error(`${ERR_VALIDATION}: Recorded write gate identity does not match the original run; refusing replay`);
  }
  if (value.safe_outputs_result !== "success" || value.verdict !== "allow") {
    throw new Error(`${ERR_VALIDATION}: Replay requires a successful recorded allow verdict; partial, denied, and unavailable gates cannot be replayed`);
  }
}

/**
 * Verify the trusted recording steps succeeded, then validate their record.
 * A conflicting upload from another job makes the compiler-owned upload fail,
 * so an artifact with the same name cannot stand in for a trusted verdict.
 * @param {Array<{name: string, status: string, steps?: Array<{name: string, status: string, conclusion: string | null}>}>} jobs
 * @param {string} runId
 * @param {string} owner
 * @param {string} repo
 * @param {number} attempt
 * @param {string} headSha
 * @param {string} [workflowPath]
 */
async function loadRecordedWriteGate(jobs, runId, owner, repo, attempt, headSha, workflowPath) {
  const names = ["Record write gate verdict", "Upload write gate verdict"];
  const matches = jobs.filter(job => job.steps?.some(step => names.includes(step.name)));
  if (matches.length === 0) return null;
  if (matches.length !== 1 || matches[0].status !== "completed") throw new Error(`${ERR_VALIDATION}: No unique completed write gate recorder; refusing replay`);
  for (const name of names) {
    const steps = (matches[0].steps || []).filter(step => step.name === name);
    if (steps.length !== 1 || steps[0].conclusion !== "success" || steps[0].status !== "completed") {
      throw new Error(`${ERR_VALIDATION}: Write gate recording did not complete successfully; refusing replay`);
    }
  }
  let directory;
  try {
    fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
    directory = fs.mkdtempSync(path.join(TMP_GH_AW_PATH, "replay-write-gate-"));
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Cannot prepare recorded write gate download`, { cause: error });
  }
  try {
    const exitCode = await exec.exec("gh", ["run", "download", runId, "--repo", `${owner}/${repo}`, "--name", "write-gate", "--dir", directory]);
    if (exitCode !== 0) throw new Error(`${ERR_SYSTEM}: Recorded write gate artifact is unavailable; refusing replay`);
    const record = parseStrictJSON(fs.readFileSync(path.join(directory, "write_gate.json"), "utf8"));
    validateReplayWriteGateRecord(record, { runId, repository: `${owner}/${repo}`, attempt, headSha, workflowPath });
    return { record: Object.fromEntries(Object.entries(record)), recorderName: matches[0].name };
  } catch (error) {
    throw new Error(`${ERR_VALIDATION}: Cannot verify recorded write gate verdict; refusing replay`, { cause: error });
  } finally {
    try {
      fs.rmSync(directory, { recursive: true });
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Could not remove temporary write gate download`, { cause: error });
    }
  }
}

/**
 * @param {{run: {run_attempt?: number, head_sha: string}, gate: unknown, recorded?: {record: Record<string, unknown>} | null}} policy
 * @param {string} runId
 * @param {string} owner
 * @param {string} repo
 */
async function enforceReplayWriteGate(policy, runId, owner, repo) {
  if (!policy.gate) return;
  if (!policy.recorded || policy.run.run_attempt === undefined) throw new Error(`${ERR_VALIDATION}: Missing recorded write gate verdict; refusing replay`);
  validateReplayWriteGateRecord(policy.recorded.record, { runId, repository: `${owner}/${repo}`, attempt: policy.run.run_attempt, headSha: policy.run.head_sha });
}

/**
 * @param {unknown} gate
 * @param {string} agentOutputFile
 * @param {string} [override]
 */
function prepareGatedReplayConfig(gate, agentOutputFile, override = process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) {
  if (
    !gate ||
    typeof gate !== "object" ||
    !("replay_config" in gate) ||
    !("replay_types" in gate) ||
    !("replay_staged" in gate) ||
    typeof gate.replay_staged !== "boolean" ||
    !gate.replay_config ||
    typeof gate.replay_config !== "object" ||
    Array.isArray(gate.replay_config) ||
    !Array.isArray(gate.replay_types) ||
    gate.replay_types.some(type => typeof type !== "string")
  ) {
    throw new Error(`${ERR_VALIDATION}: Original resolved replay restrictions are unavailable; refusing replay`);
  }
  const serialized = JSON.stringify(gate.replay_config, (_key, value) => {
    if (typeof value === "number" && !Number.isFinite(value)) throw new Error(`${ERR_VALIDATION}: Original replay restrictions require finite numbers`);
    return value;
  });
  if (serialized.includes("${{") || serialized.includes("${GH_AW_INPUT_") || /"(?:github-token|head-github-token)"\s*:/.test(serialized)) {
    throw new Error(`${ERR_VALIDATION}: Original replay config requires unavailable runtime restrictions or credentials`);
  }
  if (override && !isDeepStrictEqual(parseStrictJSON(override), parseStrictJSON(serialized))) {
    throw new Error(`${ERR_VALIDATION}: Gated replay cannot override the original handler restrictions`);
  }
  const config = normalizeSafeOutputConfig(gate.replay_config);
  let output;
  try {
    output = parseStrictJSON(fs.readFileSync(agentOutputFile, "utf8"));
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Cannot read gated replay output`, { cause: error });
  }
  if (!output || typeof output !== "object" || !("items" in output) || !Array.isArray(output.items)) throw new Error(`${ERR_VALIDATION}: Gated replay requires a valid output item array`);
  for (const item of output.items) {
    if (!item || typeof item !== "object" || typeof item.type !== "string") throw new Error(`${ERR_VALIDATION}: Malformed gated replay item`);
    const type = normalizeSafeOutputName(item.type);
    if (!gate.replay_types.includes(type) || !Object.hasOwn(config, type)) throw new Error(`${ERR_VALIDATION}: Replay item has no supported handler in the original policy`);
  }
  return { config: serialized, staged: gate.replay_staged ? "true" : "false" };
}

/**
 * Download the agent artifact from a workflow run using `gh run download`.
 *
 * @param {string} runId - The workflow run ID
 * @param {string} destDir - Destination directory for the downloaded artifact
 * @param {string|null} repoSlug - Optional repository slug (owner/repo)
 * @returns {Promise<string>} Path to the downloaded agent_output.json file
 */
async function downloadAgentArtifact(runId, destDir, repoSlug) {
  core.info(`Downloading agent artifact from run ${runId}...`);

  try {
    fs.mkdirSync(destDir, { recursive: true });
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to create directory ${destDir}: ${getErrorMessage(err)}`, { cause: err });
  }

  const args = ["run", "download", runId, "--name", "agent", "--dir", destDir];
  if (repoSlug) {
    args.push("--repo", repoSlug);
  }

  const exitCode = await exec.exec("gh", args);
  if (exitCode !== 0) {
    throw new Error(`${ERR_SYSTEM}: Failed to download agent artifact from run ${runId}`);
  }

  const outputFile = path.join(destDir, AGENT_OUTPUT_FILENAME);
  if (!fs.existsSync(outputFile)) {
    throw new Error(`${ERR_SYSTEM}: Agent output file not found at ${outputFile} after download. Ensure run ${runId} has an "agent" artifact containing ${AGENT_OUTPUT_FILENAME}.`);
  }

  core.info(`✓ Agent artifact downloaded to ${outputFile}`);
  return outputFile;
}

/**
 * Build a handler config from the items present in the agent output file.
 * Each item type found in the output is enabled (with an empty config object).
 *
 * @param {string} agentOutputFile - Path to the agent_output.json file
 * @returns {Object} Handler config keyed by normalized type name
 */
function buildHandlerConfigFromOutput(agentOutputFile) {
  let content;
  try {
    content = fs.readFileSync(agentOutputFile, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${agentOutputFile}: ${getErrorMessage(err)}`, { cause: err });
  }
  let validatedOutput;
  try {
    validatedOutput = JSON.parse(content);
  } catch (err) {
    throw new Error(`${ERR_PARSE}: ` + "Failed to parse agent output file " + agentOutputFile + ": " + getErrorMessage(err), { cause: err });
  }

  if (!validatedOutput.items || !Array.isArray(validatedOutput.items)) {
    core.info("No items found in agent output; handler config will be empty");
    return {};
  }

  // Normalize type: convert dashes to underscores (mirrors safe_outputs_append.cjs)
  const config = Object.fromEntries(validatedOutput.items.filter(item => item.type && typeof item.type === "string").map(item => [item.type.replace(/-/g, "_"), {}]));

  core.info(`Handler config built from ${validatedOutput.items.length} item(s): ${Object.keys(config).join(", ")}`);
  return config;
}

/**
 * Download the agent artifact from a previous run and apply the safe outputs.
 *
 * @returns {Promise<void>}
 */
async function main() {
  const runUrl = process.env.GH_AW_RUN_URL;
  if (!runUrl) {
    const error = new Error(`${ERR_CONFIG}: GH_AW_RUN_URL environment variable is required but not set`);
    core.setFailed(error.message);
    throw error;
  }

  core.info(`Applying safe outputs from run: ${runUrl}`);

  // Parse run ID and optional owner/repo from the URL
  let runId, owner, repo;
  try {
    ({ runId, owner, repo } = parseRunUrl(runUrl));
  } catch (error) {
    core.setFailed(getErrorMessage(error));
    throw error;
  }

  core.info(`Parsed run ID: ${runId}`);

  // repoFromUrl is non-null only when the URL explicitly specified an owner/repo.
  // displayRepoSlug falls back to the current workflow context for logging.
  const repoFromUrl = owner && repo ? `${owner}/${repo}` : null;
  const displayRepoSlug = repoFromUrl ?? `${context.repo.owner}/${context.repo.repo}`;
  core.info(`Target repository: ${displayRepoSlug}`);

  let policy;
  try {
    policy = await getReplayPolicy(runId, owner || context.repo.owner, repo || context.repo.repo);
    if (policy.gate && displayRepoSlug.toLowerCase() !== `${context.repo.owner}/${context.repo.repo}`.toLowerCase()) {
      throw new Error(`${ERR_VALIDATION}: Gated outputs can only be replayed in their original repository`);
    }
    fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
    await enforceReplayWriteGate(policy, runId, owner || context.repo.owner, repo || context.repo.repo);
  } catch (error) {
    core.setFailed(`Cannot authorize safe-output replay: ${getErrorMessage(error)}`);
    // The label replay wrapper must not publish success after a gate refusal.
    throw error;
  }

  // Download the agent artifact into /tmp/gh-aw/
  const destDir = TMP_GH_AW_PATH;
  let agentOutputFile;
  try {
    agentOutputFile = await downloadAgentArtifact(runId, destDir, repoFromUrl);
  } catch (error) {
    core.setFailed(getErrorMessage(error));
    throw error;
  }

  // Set GH_AW_AGENT_OUTPUT so the handler manager can find the output file
  process.env.GH_AW_AGENT_OUTPUT = agentOutputFile;
  core.info(`Set GH_AW_AGENT_OUTPUT=${agentOutputFile}`);

  // Auto-build GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG from the output if not already set
  if (policy.gate) {
    try {
      const original = prepareGatedReplayConfig(policy.gate, agentOutputFile);
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = original.config;
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = original.staged;
    } catch (error) {
      core.setFailed(`Cannot authorize gated replay items: ${getErrorMessage(error)}`);
      throw error;
    }
  } else if (!process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) {
    try {
      const handlerConfig = buildHandlerConfigFromOutput(agentOutputFile);
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify(handlerConfig);
      core.info("Auto-configured GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG from agent output");
    } catch (error) {
      core.setFailed(`Failed to build handler config: ${getErrorMessage(error)}`);
      throw error;
    }
  }

  // Apply safe outputs via the handler manager
  core.info("Applying safe outputs...");
  const { main: runHandlerManager } = require("./safe_output_handler_manager.cjs");
  await runHandlerManager();
  if (process.exitCode && process.exitCode !== 0) {
    throw new Error(`${ERR_SYSTEM}: Safe-output replay handler processing failed; see the reported handler errors`);
  }
}

module.exports = { main, parseRunUrl, buildHandlerConfigFromOutput, getReplayPolicy, enforceReplayWriteGate, validateReplayWriteGateRecord, loadRecordedWriteGate, prepareGatedReplayConfig };
