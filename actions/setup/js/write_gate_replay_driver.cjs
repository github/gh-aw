// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { parseRunUrl, getReplayPolicy } = require("./apply_safe_outputs_replay.cjs");
const { readOriginalHeldReplayState, authorizeHeldReplay, withHeldReplayAuthorization } = require("./write_gate_replay_approval.cjs");
const { ERR_VALIDATION, ERR_SYSTEM } = require("./error_codes.cjs");

const REPLAY_PINS = ["pending_writes_sha256", "write_decisions_sha256", "pending_write_inputs_sha256", "safe_output_items_sha256"];

async function originalReplayPolicy() {
  const { runId, owner, repo } = parseRunUrl(process.env.GH_AW_RUN_URL || "");
  const sourceOwner = owner || context.repo.owner;
  const sourceRepo = repo || context.repo.repo;
  const policy = await getReplayPolicy(runId, sourceOwner, sourceRepo, true);
  if (policy.gate && `${sourceOwner}/${sourceRepo}` !== `${context.repo.owner}/${context.repo.repo}`) throw new Error(`${ERR_VALIDATION}: Gated replay requires the original repository`);
  return { policy, runId, repository: `${sourceOwner}/${sourceRepo}` };
}

/** @param {Awaited<ReturnType<typeof originalReplayPolicy>>} original */
async function downloadHeldReplayState(original) {
  const directory = fs.mkdtempSync(path.join(process.env.RUNNER_TEMP || os.tmpdir(), "gh-aw-held-replay-"));
  try {
    const artifacts = [
      ["pending-writes", "manifest"],
      ["write-decisions", "decisions"],
      ["pending-write-inputs", "inputs"],
      ["safe-outputs-items", "outcomes"],
    ];
    for (const [artifact, folder] of artifacts) {
      const destination = path.join(directory, folder);
      fs.mkdirSync(destination, { mode: 0o700 });
      const status = await exec.exec("gh", ["run", "download", original.runId, "--repo", original.repository, "--name", artifact, "--dir", destination]);
      if (status !== 0) throw new Error(`${ERR_SYSTEM}: Original held replay artifact ${artifact} is unavailable`);
    }
    const paths = {
      policy: original.policy,
      manifestPath: path.join(directory, "manifest", "pending-writes.json"),
      decisionsPath: path.join(directory, "decisions", "write-decisions.normalized.json"),
      snapshotPath: path.join(directory, "inputs", "pending-write-inputs.json"),
      itemsPath: path.join(directory, "outcomes", "safe-output-items.jsonl"),
    };
    const state = readOriginalHeldReplayState(paths);
    return { directory, paths, state };
  } catch (cause) {
    fs.rmSync(directory, { recursive: true, force: true });
    throw new Error(`${ERR_VALIDATION}: Original held replay state could not be verified`, { cause });
  }
}

/** Read-only preparation selects the original bound approval environment. */
async function prepareHeldReplayFromEnv() {
  const original = await originalReplayPolicy();
  if (!original.policy.gate || original.policy.recorded?.record.verdict === "allow") {
    core.setOutput("mode", "legacy");
    return;
  }
  const replay = await downloadHeldReplayState(original);
  try {
    const environment = replay.state.snapshot.gate_config.approval_environment;
    if (!environment || typeof environment !== "string" || /[\x00-\x1f\x7f]/.test(environment) || environment.length > 255) throw new Error(`${ERR_VALIDATION}: Original held replay approval environment is unusable`);
    core.setOutput("mode", "held");
    core.setOutput("approval_environment", environment);
    core.setOutput("source_run_attempt", replay.state.record.run_attempt);
    core.setOutput("source_workflow_sha", replay.state.record.workflow_sha);
    for (const pin of REPLAY_PINS) core.setOutput(pin, replay.state.record[pin]);
  } finally {
    fs.rmSync(replay.directory, { recursive: true, force: true });
  }
}

/**
 * Snapshot restoration is supplied by the compiler-owned pending-input module,
 * never selected from agent data. All digests are rechecked after approval.
 *
 * @param {{restorePendingInputs: (options: {directory: string, expectedSha256: string, manifest: any, runnerTemp?: string}) => {handlerConfig: any, gateConfig: any, bindings: any[], agentOutputPath: string, context: any, environment: Record<string, string>}, runHandlers?: () => Promise<any>}} dependencies
 */
async function executeHeldReplayFromEnv({ restorePendingInputs, runHandlers = () => require("./safe_output_handler_manager.cjs").main() }) {
  const original = await originalReplayPolicy();
  const replay = await downloadHeldReplayState(original);
  const previousContext = global.context;
  const previousGithub = global.github;
  const previousOctokit = global.getOctokit;
  const previous = new Map();
  function bind(name, value) {
    previous.set(name, process.env[name]);
    process.env[name] = value;
  }
  try {
    for (const pin of REPLAY_PINS) {
      const prepared = process.env[`GH_AW_REPLAY_${pin.toUpperCase()}`];
      if (prepared !== replay.state.record[pin]) throw new Error(`${ERR_VALIDATION}: Held replay state changed after approval preparation`);
    }
    if (process.env.GH_AW_REPLAY_SOURCE_RUN_ATTEMPT !== replay.state.record.run_attempt || process.env.GH_AW_REPLAY_SOURCE_WORKFLOW_SHA !== replay.state.record.workflow_sha)
      throw new Error(`${ERR_VALIDATION}: Held replay source revision changed after preparation`);
    const authorization = await authorizeHeldReplay(replay.paths);
    const forceStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";
    const restored = restorePendingInputs({ directory: path.dirname(replay.paths.snapshotPath), expectedSha256: replay.state.record.pending_write_inputs_sha256, manifest: replay.state.manifest, runnerTemp: process.env.RUNNER_TEMP });
    for (const [name, value] of Object.entries(restored.environment)) bind(name, value);
    if (forceStaged) bind("GH_AW_REPLAY_FORCE_STAGED", "true");
    bind("GH_AW_PENDING_WRITES_REQUIRED", "true");
    bind("GH_AW_PENDING_WRITES", replay.paths.manifestPath);
    bind("GH_AW_PENDING_WRITES_SHA256", replay.state.record.pending_writes_sha256);
    bind("GH_AW_PENDING_WRITES_BINDINGS", JSON.stringify(restored.bindings));
    bind("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify(restored.handlerConfig));
    bind("GH_AW_AGENT_OUTPUT", restored.agentOutputPath);
    bind("GH_AW_WRITE_GATE_DECISIONS", replay.paths.decisionsPath);
    bind("GH_AW_WRITE_GATE_DECISIONS_SHA256", replay.state.record.write_decisions_sha256);
    bind("GH_AW_WRITE_GATE_REVIEW_REPO", restored.gateConfig.review_repo);
    bind("GH_AW_WRITE_GATE_ALLOWED_REPOS", restored.gateConfig.allowed_repos);
    bind("GH_AW_WRITE_GATE_APPROVAL_ENVIRONMENT", restored.gateConfig.approval_environment);
    global.context = restored.context;
    require("./pending_writes_client.cjs").installPendingWriteClientGuards();
    await withHeldReplayAuthorization(authorization, async () => {
      const result = await runHandlers();
      if (result?.success !== true) throw new Error(`${ERR_VALIDATION}: Approved held replay did not complete; inspect its item outcomes`);
    });
  } finally {
    global.context = previousContext;
    global.github = previousGithub;
    global.getOctokit = previousOctokit;
    for (const [name, value] of previous) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
    fs.rmSync(replay.directory, { recursive: true, force: true });
  }
}

module.exports = { prepareHeldReplayFromEnv, executeHeldReplayFromEnv, downloadHeldReplayState };
