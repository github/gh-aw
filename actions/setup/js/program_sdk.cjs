// @ts-check
"use strict";

/**
 * gh-aw program SDK: the object a deterministic `engine: none` program receives.
 *
 * - `workQueue` wraps the work-queue MCP tools (`work_queue_read`, `work_queue_submit`,
 *   `work_queue_dispatch_next`, `work_queue_claim_finish`) with the same argument
 *   shapes and validation. Publisher programs (dispatcher role) commit each submit and
 *   dispatch immediately through the trusted controls and receive the control receipt;
 *   worker programs stage intents exactly like an agent, applied by the safe_outputs job.
 * - `safeOutputs.emit` appends one safe-output message for the safe_outputs job.
 * - `core`, `github`, `context`, `exec`, `io` and `getOctokit` are the github-script globals.
 */

const fs = require("fs");
const os = require("os");
const path = require("path");
const { DEFAULT_SNAPSHOT_PATH, createWorkQueueTools, loadWorkQueueSnapshot, readWorkQueueState } = require("./work_queue_mcp_server.cjs");
const { readStagedIntentBatch } = require("./work_queue_intents.cjs");
const { controlResult } = require("./work_queue_control_receipts.cjs");
const { createAppendFunction } = require("./safe_outputs_append.cjs");
const { createSubmissionTracker } = require("./safe_output_submission.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");

const TOOL_NAMES = Object.freeze({ read: "work_queue_read", submit: "work_queue_submit", dispatchNext: "work_queue_dispatch_next", claimFinish: "work_queue_claim_finish" });

/** @param {unknown} value */
function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value) && Object.getPrototypeOf(value) === Object.prototype;
}

/** Copy caller input so later mutation by the program cannot change a staged or committed request. */
function snapshotArgs(args, name) {
  if (args === undefined) return {};
  if (!isPlainObject(args)) throw new TypeError(`${name} expects a plain object argument`);
  return JSON.parse(JSON.stringify(args));
}

/** @param {{content: Array<{text: string}>}} result */
function toolResult(result) {
  return JSON.parse(result.content[0].text);
}

/**
 * @param {{core: any, github: any, context: any, getOctokit?: any, env: NodeJS.ProcessEnv, configureControls?: (options: any) => Promise<any>, processIntents?: (options: any, intents: any[], errors: any[]) => Promise<{receipts: any[], remaining: number}>}} options
 */
async function createWorkQueueClient(options) {
  const { core, env } = options;
  const role = env.GH_AW_WORK_QUEUE_ROLE;
  const snapshot = loadWorkQueueSnapshot(env.GH_AW_WORK_QUEUE_SNAPSHOT || DEFAULT_SNAPSHOT_PATH);
  const publisher = role === "dispatcher";
  /** @type {{intentPath?: string}} */
  const toolOptions = {};
  const tools = new Map(createWorkQueueTools(snapshot, toolOptions).map(tool => [tool.name, tool]));
  /** @type {any[]} */
  const receipts = [];
  let controls;
  let remaining = 0;
  let scratch;
  let sequence = 0;

  if (publisher) {
    const config = env.GH_AW_WORK_QUEUE_CONTROL_CONFIG ? JSON.parse(env.GH_AW_WORK_QUEUE_CONTROL_CONFIG) : {};
    const configure = options.configureControls || require("./work_queue_control_adapter.cjs").configureControls;
    controls = await configure({ core, github: options.github, context: options.context, getOctokit: options.getOctokit, config, env });
    remaining = require("./work_queue_dispatch.cjs").configuredDispatchBudget({ config });
    scratch = fs.mkdtempSync(path.join(env.RUNNER_TEMP || os.tmpdir(), "gh-aw-program-"));
  }

  /** The handler of an MCP tool registered for this snapshot's role. @param {string} name */
  function tool(name) {
    const handler = tools.get(name)?.handler;
    if (!handler) throw new Error(`work_queue_tool_unavailable: ${name} is not available to the ${snapshot.role} role`);
    return handler;
  }

  /**
   * Commit one staged intent through the trusted controls; throw when it is blocked.
   * @param {string} name
   * @param {Record<string, unknown>} args
   */
  async function publish(name, args) {
    toolOptions.intentPath = path.join(/** @type {string} */ scratch, `intent-${++sequence}.jsonl`);
    try {
      tool(name)(args);
      const { intents, errors } = readStagedIntentBatch(toolOptions.intentPath);
      const processIntents = options.processIntents || require("./work_queue_dispatch.cjs").processParsedWorkQueueIntents;
      const result = await processIntents({ ...controls, maxDispatches: remaining }, intents, errors);
      remaining = result.remaining;
      receipts.push(...result.receipts);
      const blocked = result.receipts.find(receipt => receipt.status === "blocked");
      if (blocked) throw new Error(`work_queue_control_blocked: ${blocked.reason}`);
      return result.receipts[0];
    } finally {
      fs.rmSync(toolOptions.intentPath, { force: true });
      delete toolOptions.intentPath;
    }
  }

  /** @param {string} name @param {unknown} args */
  async function call(name, args) {
    const input = snapshotArgs(args, name);
    if (publisher && (name === TOOL_NAMES.submit || name === TOOL_NAMES.dispatchNext)) return publish(name, input);
    return toolResult(tool(name)(input));
  }

  const client = Object.freeze({
    role: snapshot.role,
    /** Assigned Claim of a worker run, or null. */
    assignment: readWorkQueueState(snapshot, { limit: 1 }).assignment ?? null,
    read: args => call(TOOL_NAMES.read, args),
    submit: args => call(TOOL_NAMES.submit, args),
    dispatchNext: args => call(TOOL_NAMES.dispatchNext, args),
    claimFinish: args => call(TOOL_NAMES.claimFinish, args),
  });

  async function finalize() {
    if (scratch) fs.rmSync(scratch, { recursive: true, force: true });
    if (!publisher || !receipts.length) return;
    const result = controlResult(receipts, remaining);
    core.setOutput("work_queue_requests", JSON.stringify(result));
    if (core.summary?.addRaw && core.summary?.write) await core.summary.addRaw(result.summary).write();
  }

  return { client, publisher, finalize };
}

/**
 * @param {{env: NodeJS.ProcessEnv, publisher: boolean}} options
 */
function createSafeOutputsClient({ env, publisher }) {
  const outputFile = env.GH_AW_SAFE_OUTPUTS;
  const tracker = createSubmissionTracker();
  let allowed;
  const allowedTypes = () => {
    if (allowed) return allowed;
    const configPath = env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    allowed = new Set(configPath && fs.existsSync(configPath) ? Object.keys(JSON.parse(fs.readFileSync(configPath, "utf8"))).map(normalizeSafeOutputName) : []);
    return allowed;
  };
  return Object.freeze({
    /**
     * Append one safe-output message; returns its output_id for depends-on references.
     * @param {Record<string, unknown> & {type: string}} entry
     */
    emit(entry) {
      if (publisher) throw new Error("safe_outputs_unavailable: publisher programs commit queue controls directly and cannot emit safe outputs");
      if (!outputFile) throw new Error("safe_outputs_unavailable: configure safe-outputs in the workflow frontmatter to emit safe outputs");
      if (!isPlainObject(entry) || typeof entry.type !== "string" || !entry.type) throw new TypeError("safeOutputs.emit expects an object with a string type");
      const message = JSON.parse(JSON.stringify(entry));
      const type = normalizeSafeOutputName(message.type);
      if (!allowedTypes().has(type)) throw new Error(`safe_outputs_type_not_configured: ${type} is not enabled in safe-outputs`);
      const append = createAppendFunction(outputFile);
      let outputId;
      tracker.append({ ...message, type }, recorded => {
        outputId = recorded.output_id;
        append(recorded);
      });
      return outputId;
    },
  });
}

/**
 * Build the SDK passed to a program, and the finalizer the runner calls afterwards.
 * @param {{core: any, github: any, context: any, exec?: any, io?: any, getOctokit?: any, env?: NodeJS.ProcessEnv, configureControls?: any, processIntents?: any}} options
 */
async function createProgramSdk(options) {
  const env = options.env || process.env;
  const queue = env.GH_AW_WORK_QUEUE_ENABLED === "true" ? await createWorkQueueClient({ ...options, env }) : null;
  const sdk = Object.freeze({
    core: options.core,
    github: options.github,
    context: options.context,
    exec: options.exec,
    io: options.io,
    getOctokit: options.getOctokit,
    workQueue: queue ? queue.client : null,
    safeOutputs: createSafeOutputsClient({ env, publisher: queue?.publisher === true }),
  });
  return { sdk, finalize: async () => queue?.finalize() };
}

module.exports = { createProgramSdk };
