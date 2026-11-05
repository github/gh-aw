// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { preparePiRuntime } = require("./pi_runtime.cjs");
const { createCheckpoint } = require("./pi_durable_checkpoint.cjs");
const { createDurableTelemetry } = require("./pi_durable_telemetry.cjs");
const { loadDurablePackage, createGatewayModels, installGatewayTools, installWorkflowPolicy, createEventEmitter } = require("./pi_durable_runtime.cjs");
const { sanitizeProviderErrorMessage } = require("./pi_provider_error.cjs");

async function bounded(promise, milliseconds, message) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(message)), milliseconds);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

/** @param {any} [options] */
async function main(options = {}) {
  const storageKind = process.env.GH_AW_PI_DURABLE_STORAGE;
  const artifactDirectory = process.env.GH_AW_PI_DURABLE_DIR;
  const checkpointFile = process.env.GH_AW_PI_DURABLE_CHECKPOINT;
  if (
    storageKind !== "artifact" ||
    !artifactDirectory ||
    !checkpointFile ||
    !path.isAbsolute(artifactDirectory) ||
    path.dirname(checkpointFile) !== artifactDirectory ||
    !/^pi-durable-[A-Za-z0-9_-]+\.json$/.test(path.basename(checkpointFile))
  ) {
    throw new Error("Pi durable requires artifact storage and a workflow-scoped JSON checkpoint");
  }
  const scope = path.basename(checkpointFile, ".json");
  const cwd = process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || process.cwd();
  const runtime = options.runtime || preparePiRuntime();
  if (runtime.config?.session && Object.keys(runtime.config.session).length) throw new Error("Pi durable cannot use engine.config.session; the artifact owns the durable session");
  const { agentDir, settings = {}, skillPaths = [], routingSelection } = runtime;
  const promptFile = process.env.GH_AW_PI_USER_PROMPT || process.env.GH_AW_PROMPT;
  if (!promptFile) throw new Error("GH_AW_PROMPT is required");
  const prompt = fs.readFileSync(promptFile, "utf8");
  const workDirectory = options.workDirectory || path.join("/tmp/gh-aw/agent/pi-durable", createHash("sha256").update(scope).digest("hex").slice(0, 16));
  const checkpoint = createCheckpoint(checkpointFile, workDirectory, scope, storageKind);
  const durable = await loadDurablePackage("pi-durable");
  const chord = await loadDurablePackage("chord", "context/index.js");
  const { openNodeJsonlStorage } = await loadDurablePackage("pi-durable", "storage/jsonl/node.js");
  const { NodeExecutionEnv } = await loadDurablePackage("pi-durable", "env/node.js");
  const { CodingTools } = await loadDurablePackage("pi-durable", "tools/index.js");
  const gateway = options.models ? { models: options.models, model: options.model } : await createGatewayModels(agentDir, routingSelection);
  const telemetry = createDurableTelemetry({ exportSpan: options.exportSpan });
  checkpoint.state.trace = telemetry.trace;
  const context = chord.BACKGROUND_CONTEXT;
  const registry = options.registry || durable.createRegistry();
  registry.install(CodingTools);
  const clients = [];
  let stopped = false;
  let stopReason;
  let resolveStop;
  const stopping = new Promise(resolve => {
    resolveStop = resolve;
  });
  const stop = reason => {
    stopped = true;
    stopReason = reason;
    resolveStop(null);
  };
  installWorkflowPolicy(durable, registry, checkpoint, stop);
  const systemFile = process.env.GH_AW_PI_SYSTEM_PROMPT;
  const instructions = systemFile ? fs.readFileSync(systemFile, "utf8") : "";
  const skills = [...new Set(skillPaths)];
  // Only activation-owned skills are discovered; repository/user extensions never run.
  const stagedSkills = path.join(agentDir, "skills");
  if (process.env.GH_AW_PI_BARE !== "true" && fs.existsSync(stagedSkills)) {
    for (const entry of fs.readdirSync(stagedSkills, { withFileTypes: true })) {
      const file = path.join(stagedSkills, entry.name, "SKILL.md");
      if (entry.isDirectory() && fs.existsSync(file) && fs.lstatSync(file).isFile()) skills.push(file);
    }
  }
  registry.install(
    durable.defineExtension({
      name: "gh-aw-workflow-instructions",
      sections: [
        durable.section("workflow", () => instructions || "Complete the workflow task using the provided tools.", { tag: false }),
        ...[...new Set(skills)].map((file, index) => durable.section(`workflow-skill-${index}`, () => fs.readFileSync(file, "utf8"))),
      ],
    })
  );
  const emit = options.emit || (event => process.stdout.write(JSON.stringify(event) + "\n"));
  const emitEvent = createEventEmitter(emit);
  let harness;
  let watch;
  let timer;
  const signalHandler = () => stop();
  process.once("SIGTERM", signalHandler);
  process.once("SIGINT", signalHandler);
  const closeMs = options.closeMs || 10000;
  try {
    if (!options.skipGatewayTools) await installGatewayTools(durable, registry, agentDir, clients);
    const storage = await openNodeJsonlStorage(workDirectory, context, { fsync: true });
    harness = await durable.Harness.open(
      checkpoint.wrap(storage),
      {
        models: gateway.models,
        registry,
        telemetry: telemetry.telemetry,
        env: ({ cwd: workingDirectory }) => new NodeExecutionEnv({ cwd: workingDirectory || cwd }),
        settings: { retry: { maxRetries: 2, baseDelayMs: 1000, maxDelayMs: 30000 }, toolExecution: "sequential", progress: { partialIntervalMs: 100, persistWithinMs: 1000 } },
        onReport: report => process.stderr.write(`[pi-durable] ${report.kind}\n`),
      },
      context
    );
    const root = await harness.root(context, { agent: { model: gateway.model, cwd, ...(settings.defaultThinkingLevel ? { thinkingLevel: settings.defaultThinkingLevel } : {}) } });
    // Stored agent choices contain only references; every run rebinds the gateway and policy.
    await root.configure({ model: gateway.model, cwd, ...(settings.defaultThinkingLevel ? { thinkingLevel: settings.defaultThinkingLevel } : {}) }, context);
    emit({ type: "session", version: 3, id: `${scope}:${root.id}`, cwd });
    watch = await durable.watchEvents(harness, root.id, context);
    watch.start(async events => {
      for (const event of events) {
        if (event.type === "usage_changed" || event.type === "snapshot") checkpoint.state.usage = event.usage;
        emitEvent(event);
      }
    });
    if (options.onHarness) await options.onHarness(harness, root, stop);
    const timeoutMinutes = Number(process.env.GH_AW_TIMEOUT_MINUTES || 20);
    if (!Number.isFinite(timeoutMinutes) || timeoutMinutes <= 0) throw new Error("Invalid workflow timeout for Pi durable");
    timer = setTimeout(() => stop("deadline"), options.timeoutMs ?? Math.max(1000, timeoutMinutes * 60000 - closeMs - 30000));
    const runId = process.env.GITHUB_RUN_ID || "local";
    if (!checkpoint.state.pending && checkpoint.state.runId === runId) {
      if (checkpoint.state.failure === "policy") throw new Error("Pi durable workflow tool budget exhausted");
      if (checkpoint.state.failure === "unanswered") throw new Error("Pi durable submission ended unanswered");
      return;
    }
    if (!checkpoint.state.pending) {
      checkpoint.state.requestId = `gh-aw:${runId}:${process.env.GITHUB_RUN_ATTEMPT || "1"}`;
      checkpoint.state.runId = runId;
      checkpoint.state.pending = true;
      checkpoint.state.submissionId = null;
      checkpoint.state.calls = 0;
      checkpoint.state.denials = 0;
      checkpoint.state.failure = null;
      checkpoint.save();
    }
    let submission = checkpoint.state.submissionId === null ? undefined : await harness.submission(checkpoint.state.submissionId, context);
    if (!submission) {
      submission = await root.submit({ type: "input", content: prompt, requestId: checkpoint.state.requestId }, context);
      checkpoint.state.submissionId = submission.id;
      checkpoint.save();
    }
    const settled = await Promise.race([submission.wait(context), stopping]);
    if (stopped && stopReason === "policy") {
      await bounded(root.abort(context), closeMs, "Pi durable policy abort exceeded its shutdown deadline");
      checkpoint.state.pending = false;
      checkpoint.state.runId = runId;
      checkpoint.state.failure = "policy";
      checkpoint.save();
      throw new Error("Pi durable workflow tool budget exhausted");
    }
    if (stopped && stopReason === "deadline") {
      process.stderr.write("[pi-durable] Yielding before the job deadline; the pending submission will resume from the artifact\n");
      return;
    }
    if (stopped || !settled) throw new Error("Pi durable interrupted; the pending submission will resume from the artifact");
    checkpoint.state.pending = false;
    checkpoint.state.runId = runId;
    checkpoint.state.failure = settled.status === "done" ? null : "unanswered";
    checkpoint.state.usage = await harness.usage(context);
    checkpoint.save();
    if (settled.status !== "done") throw new Error(`Pi durable submission ended unanswered (${settled.reason || "unknown"})`);
  } finally {
    clearTimeout(timer);
    process.removeListener("SIGTERM", signalHandler);
    process.removeListener("SIGINT", signalHandler);
    if (harness) await bounded(harness.close(context), closeMs, "Pi durable close exceeded its shutdown deadline");
    if (watch) await watch.stop();
    checkpoint.save();
    await Promise.all(clients.map(client => client.close()));
    await bounded(telemetry.flush(), closeMs, "Pi durable telemetry flush exceeded its shutdown deadline");
  }
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[pi-durable] ${sanitizeProviderErrorMessage(error?.message)}\n`);
    // A non-cooperative tool must not keep the job alive past bounded cleanup.
    process.exitCode = 1;
    const timer = setTimeout(() => process.exit(1), 1000);
    timer.unref();
  });
}

module.exports = { main, bounded };
