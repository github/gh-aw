// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { assignmentForDispatch } = require("./work_queue_scheduler.cjs");
const { readFinishIntents } = require("./work_queue_intents.cjs");
const { createProgramSdk } = require("./program_sdk.cjs");
const { main: runProgram, resolveProgramPath } = require("./run_program.cjs");

const POLICY = { version: 4, pending_limit: 30, issues: false, effect_scope: "owner/repo", workers: ["worker"], pools: { default: { concurrency: 3, per_account_limit: 0, max_attempts: 3 } } };
const CONFIG = { max: 2, workflows: ["worker"], work_queue_workflows: ["worker"], workflow_files: { worker: ".lock.yml" } };
const dispatcherContext = { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1, payload: {}, ref: "refs/heads/main" };
const ENV_KEYS = ["GITHUB_EVENT_PATH", "GITHUB_WORKFLOW_REF", "GITHUB_REF", "GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_SNAPSHOT", "GH_AW_WORK_QUEUE_POLICY", "GH_AW_SAFE_OUTPUTS_STAGED"];

function fakeCore() {
  return { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn(async () => undefined) } };
}

let directory;
const savedEnv = {};
const savedContext = global.context;

beforeEach(() => {
  directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-program-"));
  for (const key of ENV_KEYS) savedEnv[key] = process.env[key];
  for (const key of ENV_KEYS) delete process.env[key];
});

afterEach(() => {
  fs.rmSync(directory, { recursive: true, force: true });
  for (const [key, value] of Object.entries(savedEnv)) {
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
  global.context = savedContext;
});

function writeSnapshot(envelope) {
  const snapshotPath = path.join(directory, "snapshot.json");
  fs.writeFileSync(snapshotPath, JSON.stringify({ version: 4, origin: h.dispatcher, ...envelope }));
  return snapshotPath;
}

/** A dispatcher program SDK whose trusted controls publish to the in-memory queue. */
async function publisherSdk(fake, core = fakeCore()) {
  process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/dispatcher.lock.yml@refs/heads/main";
  process.env.GH_AW_WORK_QUEUE_ROLE = "dispatcher";
  const env = {
    GH_AW_WORK_QUEUE_ENABLED: "true",
    GH_AW_WORK_QUEUE_ROLE: "dispatcher",
    GH_AW_WORK_QUEUE_SNAPSHOT: writeSnapshot({ sha: null, transactionLog: "", role: "dispatcher", worker: null }),
    GH_AW_WORK_QUEUE_CONTROL_CONFIG: JSON.stringify(CONFIG),
    GH_AW_SAFE_OUTPUTS: path.join(directory, "outputs.jsonl"),
    RUNNER_TEMP: directory,
  };
  const configureControls = async options => ({ ...options, githubClient: fake.githubClient, dispatchClient: { rest: { actions: fake.githubClient.rest.actions } }, policy: POLICY });
  const program = await createProgramSdk({ core, github: fake.githubClient, context: dispatcherContext, env, configureControls });
  return { ...program, core, env };
}

describe("publisher programs", () => {
  it("commit submissions and dispatches directly and report the receipts", async () => {
    const fake = h.fakeQueueGitHub();
    const { sdk, finalize, core } = await publisherSdk(fake);

    const submitted = await sdk.workQueue.submit({
      nodes: [
        { node_key: "a", payload: { repo: "a" } },
        { node_key: "b", payload: { repo: "b" } },
      ],
    });
    expect(submitted).toMatchObject({ status: "committed" });
    const dispatched = await sdk.workQueue.dispatchNext({ pool: "default", max_claims: 2, max_dispatches: 2 });
    expect(dispatched).toMatchObject({ status: "committed", dispatches: 2 });
    expect(dispatched.launches.map(launch => launch.state)).toEqual(["started", "started"]);

    const state = replayTransactionLog(fake.contents());
    expect([...state.works.values()].map(work => work.state)).toEqual(["claimed", "claimed"]);
    expect([...state.dispatches.values()].map(dispatch => dispatch.state)).toEqual(["started", "started"]);

    await finalize();
    const output = JSON.parse(core.setOutput.mock.calls.find(([name]) => name === "work_queue_requests")[1]);
    expect(output).toMatchObject({ success: true, status: "committed", remaining_dispatches: 0 });
    expect(output.receipts).toHaveLength(2);
  });

  it("enforce the dispatch budget across calls", async () => {
    const fake = h.fakeQueueGitHub();
    const { sdk } = await publisherSdk(fake);
    await sdk.workQueue.submit({ nodes: ["a", "b", "c"].map(key => ({ node_key: key, payload: { key } })) });
    await sdk.workQueue.dispatchNext({ pool: "default", max_claims: 2, max_dispatches: 2 });

    await expect(sdk.workQueue.dispatchNext({ pool: "default", max_claims: 1, max_dispatches: 1 })).rejects.toThrow("work_queue_control_blocked: work_queue_dispatch_budget_exceeded");
    expect(fake.posts).toHaveLength(2);
  });

  it("reject invalid tool input before publishing", async () => {
    const fake = h.fakeQueueGitHub();
    const { sdk } = await publisherSdk(fake);
    await expect(sdk.workQueue.dispatchNext({ pool: "default", max_claims: 1 })).rejects.toThrow();
    await expect(sdk.workQueue.claimFinish({ outcome: "completed" })).rejects.toThrow("work_queue_tool_unavailable");
    expect(fake.contents()).toBeUndefined();
  });

  it("cannot emit safe outputs", async () => {
    const { sdk } = await publisherSdk(h.fakeQueueGitHub());
    expect(() => sdk.safeOutputs.emit({ type: "noop", message: "x" })).toThrow("safe_outputs_unavailable");
  });
});

describe("worker programs", () => {
  /** A worker snapshot whose Dispatch is bound to run 200, plus a workspace holding `source` as the program. */
  function workerRun(source) {
    const works = [h.work("a")];
    const planned = h.dispatchNext(h.submit([], works), { max_claims: 1, max_dispatches: 1 });
    const dispatchId = planned.decision.dispatches[0];
    const log = h.bind(h.launch(planned.log, dispatchId), dispatchId, h.workerActor("200", 1));
    const assignment = assignmentForDispatch(replayTransactions(log), dispatchId);
    const workspace = path.join(directory, "workspace");
    fs.mkdirSync(path.join(workspace, ".github/workflows"), { recursive: true });
    fs.writeFileSync(path.join(workspace, ".github/workflows/worker.mjs"), source);
    const safeOutputsDir = path.join(directory, "gh-aw", "safeoutputs");
    fs.mkdirSync(safeOutputsDir, { recursive: true });
    fs.writeFileSync(path.join(safeOutputsDir, "config.json"), JSON.stringify({ noop: { max: 1 } }));
    const snapshotPath = writeSnapshot({ sha: "head", transactionLog: h.ledgerText(log), role: "worker", worker: assignment });
    Object.assign(process.env, { GH_AW_WORK_QUEUE_ENABLED: "true", GH_AW_WORK_QUEUE_ROLE: "worker", GH_AW_WORK_QUEUE_SNAPSHOT: snapshotPath });
    const env = {
      ...process.env,
      GITHUB_WORKSPACE: workspace,
      GH_AW_PROGRAM: ".github/workflows/worker.mjs",
      GH_AW_SAFE_OUTPUTS: path.join(safeOutputsDir, "outputs.jsonl"),
      GH_AW_SAFE_OUTPUTS_CONFIG_PATH: path.join(safeOutputsDir, "config.json"),
      GH_AW_WORK_QUEUE_FINISH_INTENT: path.join(directory, "finish.jsonl"),
      RUNNER_TEMP: directory,
    };
    const context = { repo: { owner: "owner", repo: "repo" }, runId: 200, runAttempt: 1, payload: { inputs: { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: dispatchId }) } } };
    // setupGlobals publishes the github-script context; Claim scoping reads the run's assignment from it.
    global.context = context;
    return { env, context, assignment, safeOutputsPath: env.GH_AW_SAFE_OUTPUTS };
  }

  it("stage Claim-scoped safe outputs and the finish outcome for the safe_outputs job", async () => {
    const { env, context, assignment, safeOutputsPath } = workerRun(`
      export default async function (aw) {
        const claim = aw.workQueue.assignment.claims[0];
        aw.safeOutputs.emit({ type: "noop", message: "processed " + claim.work_id });
        await aw.workQueue.claimFinish({ outcome: "completed" });
      }
    `);
    process.env.GH_AW_WORK_QUEUE_FINISH_INTENT = env.GH_AW_WORK_QUEUE_FINISH_INTENT;
    try {
      await runProgram({ core: fakeCore(), github: {}, context, env });
    } finally {
      delete process.env.GH_AW_WORK_QUEUE_FINISH_INTENT;
    }

    const [entry] = fs
      .readFileSync(safeOutputsPath, "utf8")
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(entry).toMatchObject({ type: "noop", message: `processed ${assignment.claims[0].work_id}`, claim_handle: assignment.claims[0].handle });
    expect(entry.output_id).toMatch(/^so_/);
    expect(readFinishIntents(env.GH_AW_WORK_QUEUE_FINISH_INTENT)).toEqual(new Map([[assignment.claims[0].handle, "completed"]]));
  });

  it("reject safe-output types the workflow did not enable", async () => {
    const { env, context } = workerRun(`export default async aw => { aw.safeOutputs.emit({ type: "create_issue", title: "x", body: "y" }); };`);
    await expect(runProgram({ core: fakeCore(), github: {}, context, env })).rejects.toThrow("safe_outputs_type_not_configured: create_issue");
  });
});

describe("program resolution", () => {
  it("refuses a program that resolves outside the workspace through a symlink", () => {
    const workspace = path.join(directory, "workspace");
    fs.mkdirSync(path.join(workspace, ".github/workflows"), { recursive: true });
    fs.writeFileSync(path.join(directory, "outside.mjs"), "export default () => {};");
    fs.symlinkSync(path.join(directory, "outside.mjs"), path.join(workspace, ".github/workflows/program.mjs"));
    expect(() => resolveProgramPath({ GITHUB_WORKSPACE: workspace, GH_AW_PROGRAM: ".github/workflows/program.mjs" })).toThrow("program_path_invalid");
  });

  it("requires a default-exported function", async () => {
    const workspace = path.join(directory, "workspace");
    fs.mkdirSync(path.join(workspace, ".github/workflows"), { recursive: true });
    fs.writeFileSync(path.join(workspace, ".github/workflows/program.mjs"), "export const run = () => {};");
    await expect(runProgram({ core: fakeCore(), github: {}, context: dispatcherContext, env: { GITHUB_WORKSPACE: workspace, GH_AW_PROGRAM: ".github/workflows/program.mjs" } })).rejects.toThrow("program_entry_invalid");
  });
});
