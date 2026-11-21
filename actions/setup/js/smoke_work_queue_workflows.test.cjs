import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

const requireSetup = createRequire(import.meta.url);

function readWorkflowFile(file, compiled = false) {
  const source = readFileSync(new URL(`../../../.github/workflows/${file}.${compiled ? "lock.yml" : "md"}`, import.meta.url), "utf8");
  const yaml = compiled ? source : source.match(/^---\n([\s\S]*?)\n---\n/)[1];
  const document = readDocumentMetadata(yaml, { version: "1.2", uniqueKeys: true });
  return document.value;
}

function readWorkflow(name, compiled = false) {
  return readWorkflowFile(`smoke-work-queue-${name}`, compiled);
}

/** The compiled GH_AW_WORK_QUEUE_POLICY JSON, joined from its chunked transport when needed. */
function compiledPolicy(env) {
  if (env.GH_AW_WORK_QUEUE_POLICY !== undefined) return JSON.parse(env.GH_AW_WORK_QUEUE_POLICY);
  const parts = Number(env.GH_AW_WORK_QUEUE_POLICY_PARTS);
  return JSON.parse(Array.from({ length: parts }, (_, index) => env[`GH_AW_WORK_QUEUE_POLICY_${index}`]).join(""));
}

function verification(name) {
  const workflow = readWorkflow(name);
  const job = workflow.jobs[`verify_smoke_${name === "worker" ? "completion" : "dispatch"}`];
  const step = job.steps[0];
  const script = step.run.match(/node <<'JS'\n([\s\S]*?)\nJS/)[1];
  return { job, step, run: env => runInNewContext(script, { require: () => assert, process: { env } }) };
}

const successfulJobs = { SMOKE_ACTIVATION_RESULT: "success", SMOKE_AGENT_RESULT: "success", SMOKE_SAFE_OUTPUTS_RESULT: "success" };
const dispatchId = index => `d_${String(index).repeat(64)}_1`;
const startedLaunches = [1, 2, 3].map(index => ({ dispatch_id: dispatchId(index), state: "started" }));
// Shapes emitted by the trusted control step (`work_queue_requests`), not agent intents.
const dispatchReceipt = (overrides = {}) => ({
  version: 4,
  success: true,
  status: "committed",
  receipts: [
    { intent_id: "submit", request_id: "request:submit", commit_id: "q_submit", status: "committed" },
    { intent_id: "dispatch", request_id: "request:dispatch", commit_id: "q_dispatch", status: "committed", dispatches: 3, launches: startedLaunches, ...overrides },
  ],
  remaining_dispatches: 0,
  summary: "Committed 3 dispatches",
});
const dispatchEnv = receipt => ({ ...successfulJobs, SMOKE_DISPATCH_RECEIPT: JSON.stringify(receipt) });

describe("native work queue smoke verification", () => {
  it("stages three smoke Work and one bounded dispatch without reading an installed Policy", () => {
    const script = readWorkflow("dispatcher").steps[0].with.script;
    expect(script).not.toContain("work_queue_mcp_server.cjs");
    expect(script).not.toMatch(/logical_limit|native_limit|Policy/);
    const staged = [];
    const require = name => {
      if (name === "node:fs") return { mkdirSync() {}, writeFileSync() {} };
      if (name === "node:path") return { join: (...parts) => parts.join("/") };
      if (name.endsWith("work_queue_intents.cjs")) return { stagePlanIntents: (_filename, plan) => staged.push(plan) };
      throw new Error(`Unexpected module ${name}`);
    };
    runInNewContext(script, {
      require,
      process: { env: { RUNNER_TEMP: "/tmp" } },
      context: { runId: "123", repo: { owner: "owner", repo: "repo" }, payload: { repository: { id: 456 } } },
      core: { info() {} },
    });
    expect(staged).toHaveLength(1);
    expect(staged[0].nodes.map(node => node.node_key)).toEqual(["smoke-1", "smoke-2", "smoke-3"]);
    expect(staged[0].nodes.every(node => node.graph_id === "smoke-work-queue:123" && node.payload.smoke === "work-queue-v1")).toBe(true);
    expect(staged[0].dispatch).toEqual({ pool: "smoke-work-queue", max_claims: 3, max_dispatches: 3 });
  });

  it("compiles the smoke pool concurrency from aw.json into the dispatcher policy", () => {
    const lock = readWorkflow("dispatcher", true);
    const controls = lock.jobs.safe_outputs.steps.find(step => step.id === "work_queue_controls");
    const policy = compiledPolicy(controls.env);
    expect(policy.version).toBe(4);
    expect(policy.pools["smoke-work-queue"]).toMatchObject({ concurrency: 3, max_attempts: 3 });
    expect(policy.pools["smoke-work-queue"].profiles["smoke-work-queue-worker"]).toEqual({ workflow: ".github/workflows/smoke-work-queue-worker.lock.yml", effect_scope: "${{ github.repository }}" });
  });

  it("uses isolated worker concurrency and a read-only, time-free monitor", () => {
    const worker = readWorkflow("worker");
    expect(worker.concurrency["job-discriminator"]).toBe("${{ github.run_id }}");

    const monitor = readWorkflow("monitor");
    expect(monitor.checkout["pull-request"]).toBe(false);
    expect(monitor.tools["cli-proxy"]).toBe(true);
    expect(monitor.tools["work-queue"]).toBe(true);
    expect(monitor.tools.bash).toContain("work-queue:*");
    const script = monitor.steps[0].with.script;
    expect(script).toContain("work_queue_replay.cjs");
    expect(script).toContain("per_page: 6");
    for (const clock of ["Date.now", "Date.parse", "new Date", "created:", "BUDGET", "secs(", "over_budget", "timing("]) {
      expect(script).not.toContain(clock);
    }

    const monitorLock = readFileSync(new URL("../../../.github/workflows/smoke-work-queue-monitor.lock.yml", import.meta.url), "utf8");
    expect(monitorLock).toContain('GH_AW_WORK_QUEUE_ROLE: "observer"');
    expect(monitorLock).toContain('"tools": ["work_queue_read"]');
    expect(monitorLock).not.toContain('"work_queue_submit"');
    expect(monitorLock).not.toContain('"work_queue_dispatch_next"');
    expect(monitorLock).not.toContain('"work_queue_claim_finish"');
  });

  it.each(["dispatcher", "worker"])("%s always checks completed upstream jobs, including skipped agents", name => {
    const { job, step, run } = verification(name);
    expect(job.if).toBe("always()");
    expect(job.needs).toEqual(name === "worker" ? ["activation", "agent", "safe_outputs"] : ["agent", "safe_outputs"]);
    expect(step.env.SMOKE_AGENT_RESULT).toBe("${{ needs.agent.result }}");
    expect(step.env.SMOKE_SAFE_OUTPUTS_RESULT).toBe("${{ needs.safe_outputs.result }}");
    const env = name === "worker" ? { ...successfulJobs, SMOKE_QUEUE_OUTCOME: "completed" } : dispatchEnv(dispatchReceipt());
    for (const variable of [...(name === "worker" ? ["SMOKE_ACTIVATION_RESULT"] : []), "SMOKE_AGENT_RESULT", "SMOKE_SAFE_OUTPUTS_RESULT"]) {
      for (const result of ["skipped", "failure", "cancelled", ""]) {
        expect(() => run({ ...env, [variable]: result })).toThrow();
      }
    }
  });

  it("requires a committed dispatch receipt with three started launches", () => {
    const { step, run } = verification("dispatcher");
    expect(step.env.SMOKE_DISPATCH_RECEIPT).toBe("${{ needs.safe_outputs.outputs.work_queue_requests }}");
    expect(() => run(dispatchEnv(dispatchReceipt()))).not.toThrow();
    for (const invalid of [
      dispatchReceipt({ status: "blocked", reason: "work_queue_pending_limit", dispatches: 0, launches: [] }),
      dispatchReceipt({ launches: [] }),
      dispatchReceipt({ dispatches: 2, launches: startedLaunches.slice(0, 2) }),
      dispatchReceipt({ launches: [{ dispatch_id: dispatchId(1), state: "launch_failed", reason: "http_5xx" }, ...startedLaunches.slice(1)] }),
      dispatchReceipt({ launches: [{ dispatch_id: "dispatch:smoke", state: "started" }, ...startedLaunches.slice(1)] }),
      { ...dispatchReceipt(), version: 3 },
      { ...dispatchReceipt(), success: false },
      { ...dispatchReceipt(), receipts: [] },
    ]) {
      expect(() => run(dispatchEnv(invalid))).toThrow();
    }
    expect(() => run({ ...successfulJobs, SMOKE_DISPATCH_RECEIPT: "" })).toThrow();
    expect(() => run({ ...successfulJobs })).toThrow();
  });

  it("requires a committed Completion even if agent execution and safe outputs succeeded", () => {
    const { step, run } = verification("worker");
    expect(step.env.SMOKE_QUEUE_OUTCOME).toBe("${{ needs.safe_outputs.outputs.work_queue_outcome }}");
    expect(() => run({ ...successfulJobs, SMOKE_QUEUE_OUTCOME: "completed" })).not.toThrow();
    for (const outcome of ["failed", "staged_preview", ""]) {
      expect(() => run({ ...successfulJobs, SMOKE_QUEUE_OUTCOME: outcome })).toThrow();
    }
    expect(() => run(successfulJobs)).toThrow();
  });

  it("requires trusted worker activation separately from a completed Claim", () => {
    const { step, run } = verification("worker");
    expect(step.env.SMOKE_ACTIVATION_RESULT).toBe("${{ needs.activation.result }}");
    const { SMOKE_ACTIVATION_RESULT, ...withoutActivation } = successfulJobs;
    expect(() => run({ ...withoutActivation, SMOKE_QUEUE_OUTCOME: "completed" })).toThrow();
  });

  it("validates the v4 assignment from the activation snapshot and a version-4 finish intent", () => {
    const worker = readWorkflow("worker");
    const pre = worker["pre-agent-steps"][0].run;
    expect(pre).toContain("assert.equal(input.version, 4)");
    expect(pre).toContain("/tmp/gh-aw/work-queue.snapshot.json");
    expect(pre).toContain('assert.equal(work.smoke, "work-queue-v1")');
    expect(pre).not.toContain("normalizeAssignment");
    const post = worker["post-steps"][0].run;
    expect(post).toContain('{ version: 4, claim_handle: handle, outcome: "completed" }');
    expect(post).not.toContain("keep_open");
  });

  it("compiles always-running verifiers, the finish step after Process Safe Outputs, and trusted receipt sources", () => {
    for (const name of ["dispatcher", "worker"]) {
      const compiled = readWorkflow(name, true);
      const id = `verify_smoke_${name === "worker" ? "completion" : "dispatch"}`;
      expect(compiled.jobs[id].if).toBe("always()");
      expect(compiled.jobs[id].needs).toEqual(expect.arrayContaining(["agent", "safe_outputs"]));
      expect(compiled.jobs[id].steps).toEqual(expect.arrayContaining(readWorkflow(name).jobs[id].steps));
      expect(compiled.jobs.safe_outputs.outputs.work_queue_requests).toBe("${{ steps.work_queue_controls.outputs.work_queue_requests }}");
      const steps = compiled.jobs.safe_outputs.steps;
      const controls = steps.find(step => step.id === "work_queue_controls");
      expect(controls.env.GH_AW_WORK_QUEUE_ROLE).toBe(name === "worker" ? "worker" : "dispatcher");
      expect(compiledPolicy(controls.env).version).toBe(4);
      expect(controls.with.script).toContain("work_queue_control_adapter.cjs");
      expect(controls.with.script).toContain("await main({ core, github, context");
      if (name === "worker") {
        expect(compiled.jobs[id].needs).toContain("activation");
        const activation = compiled.jobs.activation.steps.find(step => step.id === "work_queue_snapshot");
        expect(activation.with.script).toContain("write_work_queue_snapshot.cjs");
        expect(compiled.jobs.activation.outputs.work_queue_assignment_settled).toBe("${{ steps.work_queue_snapshot.outputs.work_queue_assignment_settled }}");
        expect(compiled.jobs.agent.if).toContain("needs.activation.outputs.work_queue_assignment_settled != 'true'");
        expect(compiled.jobs.safe_outputs.if).toContain("needs.activation.outputs.work_queue_assignment_settled != 'true'");
        const handler = steps.findIndex(step => step.id === "process_safe_outputs");
        const finish = steps.findIndex(step => step.name === "Finish work queue claim");
        expect(handler).toBeGreaterThan(-1);
        expect(finish).toBeGreaterThan(handler);
        expect(steps[finish].id).toBe("work_queue_finish");
        expect(steps[finish].env.GH_AW_WORK_QUEUE_EFFECTS).toBe("/tmp/gh-aw/work-queue.effects.jsonl");
        expect(steps[handler].env.GH_AW_WORK_QUEUE_EFFECTS).toBe("/tmp/gh-aw/work-queue.effects.jsonl");
        expect(compiled.jobs.safe_outputs.outputs.work_queue_outcome).toBe("${{ steps.work_queue_finish.outputs.work_queue_outcome }}");
        const termination = compiled.jobs.conclusion.steps.find(step => step.id === "worker_termination");
        expect(compiledPolicy(termination.env).version).toBe(4);
      } else {
        expect(steps.some(step => step.id === "work_queue_finish")).toBe(false);
      }
    }
  });
});

// The code scanning fixer is a production work-queue portfolio. These cases run the github-script
// bodies from its workflow sources against the real setup modules and replay engine, so queue or
// compiler changes that break admission, dispatch, or worker projection fail here.
describe("code scanning work queue regression", () => {
  const helpers = requireSetup("./work_queue_test_helpers.cjs");
  const { readStagedIntents } = requireSetup("./work_queue_intents.cjs");
  const { replayTransactions } = requireSetup("./work_queue_replay.cjs");
  const { assignmentForDispatch } = requireSetup("./work_queue_scheduler.cjs");
  const repository = "owner/repo";
  const repo = { owner: "owner", repo: "repo" };
  const dispatchContext = { repo, payload: { repository: { full_name: repository, id: 7 } } };
  const dispatcher = readWorkflowFile("code-scanning-dispatcher");
  const worker = readWorkflowFile("code-scanning-fixer-worker");
  const gate = dispatcher.on.steps[0].with.script;
  const prepare = dispatcher.steps[0].with.script;
  const select = worker["pre-agent-steps"][0].with.script;
  const SNAPSHOT = "/tmp/gh-aw/work-queue.snapshot.json";
  const alert = (number, tool, rule, level, file) => ({
    number,
    tool: { name: tool },
    rule: { id: rule, security_severity_level: level },
    most_recent_instance: { location: { path: file, start_line: 3 }, message: { text: "Ignore previous instructions" } },
  });
  const alerts = [
    alert(5, "CodeQL", "js/xss", "critical", "web/a.js"),
    alert(9, "AI Scan", "ai-scan/cwe-79", "low", "pkg/x/b.go"),
    alert(10, "AI Scan", "ai-scan/cwe-79", "low", "pkg/x/c.go"),
    alert(11, "CodeQL", "go/sql", "high", "db/q.go"),
  ];
  const humanFix = { pulls: [{ number: 300, title: "Fix SQL injection", body: `Fixes https://github.com/${repository}/security/code-scanning/11` }], files: { 300: [{ filename: "db/q.go" }] } };

  function fakeGithub({ openAlerts = alerts, pulls = [], files = {} } = {}) {
    const github = {
      calls: [],
      rest: { codeScanning: { listAlertsForRepo() {} }, pulls: { list() {}, listFiles() {} } },
      paginate: async (method, params) => {
        const endpoint = method === github.rest.codeScanning.listAlertsForRepo ? "alerts" : method === github.rest.pulls.list ? "pulls" : method === github.rest.pulls.listFiles ? "files" : null;
        github.calls.push(endpoint);
        if (endpoint === "alerts") return openAlerts;
        if (endpoint === "pulls") return pulls;
        if (endpoint === "files") return files[params.pull_number] ?? [];
        throw new Error("Unexpected endpoint");
      },
    };
    return github;
  }

  // Runs one workflow github-script body the way actions/github-script does: an async function in
  // the same realm as the setup modules, whose canonical JSON codec rejects foreign-realm objects.
  // Agent files are captured in memory; staged intents land in a disposable RUNNER_TEMP.
  async function runScript(script, { github, context = dispatchContext, env = {}, works = [], assignment = null }) {
    const runnerTemp = mkdtempSync(path.join(os.tmpdir(), "code-scanning-smoke-"));
    const actions = `${runnerTemp}/gh-aw/actions/`;
    const written = new Map();
    const outputs = {};
    const fs = {
      mkdirSync() {},
      writeFileSync: (file, data) => written.set(file, JSON.parse(data)),
      readFileSync: file => {
        if (file !== SNAPSHOT) throw new Error(`Unexpected read ${file}`);
        return JSON.stringify({ version: 4, worker: assignment });
      },
    };
    const require = name => {
      if (name === "node:fs") return fs;
      if (name === "node:path") return path;
      if (name === `${actions}work_queue_mcp_server.cjs`) return { loadWorkQueueSnapshot: () => ({ projection: { works: new Map(works.map(work => [work.work_id, work])) } }) };
      if (name.startsWith(actions)) return requireSetup(`./${name.slice(actions.length)}`);
      throw new Error(`Unexpected module ${name}`);
    };
    const core = { info() {}, setOutput: (key, value) => (outputs[key] = value) };
    const AsyncFunction = Object.getPrototypeOf(async () => {}).constructor;
    try {
      await new AsyncFunction("require", "github", "context", "core", "process", script)(require, github, context, core, { env: { ...env, RUNNER_TEMP: runnerTemp } });
      return { outputs, written, intents: readStagedIntents(path.join(runnerTemp, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl")) };
    } finally {
      rmSync(runnerTemp, { recursive: true, force: true });
    }
  }

  // Applies staged dispatcher intents to a v4 ledger, as trusted control processing would.
  function codeScanningQueue() {
    let log = [];
    const profiles = { "code-scanning-fixer-worker": { workflow: ".github/workflows/code-scanning-fixer-worker.lock.yml" } };
    return {
      state: () => replayTransactions(log),
      works: () => [...replayTransactions(log).works.values()],
      apply: intents => {
        for (const { kind, parameters } of intents) {
          if (kind === "submit") {
            const known = replayTransactions(log).works;
            const fresh = parameters.nodes.map(node => helpers.work(node.node_key, node)).filter(node => !known.has(node.work_id));
            if (fresh.length) log = helpers.submit(log, fresh);
          } else {
            log = helpers.dispatchNext(log, { ...parameters, concurrency: 3, per_account_limit: 0, profiles }).log;
          }
        }
      },
    };
  }

  it("gates pre-activation on an open alert that no open pull request covers", async () => {
    expect((await runScript(gate, { github: fakeGithub(humanFix) })).outputs).toEqual({ has_alerts: "true" });
    const everyFile = { pulls: [{ number: 301, title: "Refactor", body: "" }], files: { 301: alerts.map(entry => ({ filename: entry.most_recent_instance.location.path })) } };
    expect((await runScript(gate, { github: fakeGithub(everyFile) })).outputs).toEqual({ has_alerts: "false" });
    const idle = fakeGithub({ openAlerts: [] });
    expect((await runScript(gate, { github: idle })).outputs).toEqual({ has_alerts: "false" });
    expect(idle.calls).toEqual(["alerts"]);
    const truncated = { pulls: humanFix.pulls, files: { 300: Array.from({ length: 3000 }, (_, index) => ({ filename: `file-${index}.go` })) } };
    await expect(runScript(gate, { github: fakeGithub(truncated) })).rejects.toThrow(/pull request #300/);
  });

  it("admits AI Scan-first clusters, grants worker Claims, and projects a worker assignment without newly covered alerts", async () => {
    const queue = codeScanningQueue();
    const first = await runScript(prepare, { github: fakeGithub(humanFix), works: queue.works() });
    const plan = first.written.get("/tmp/gh-aw/agent/code-scanning-plan.json");
    expect(plan.nodes.map(node => node.payload.alerts.map(member => member.number))).toEqual([[9, 10], [5]]);
    expect(plan.covered).toEqual([{ number: 11, pull_numbers: [300] }]);
    expect(first.intents.map(intent => intent.kind)).toEqual(["submit", "dispatch_next"]);
    expect(first.intents[1].parameters).toEqual({ pool: "code-scanning", max_claims: 3, max_dispatches: 3 });
    queue.apply(first.intents);
    expect(queue.works().map(work => work.state)).toEqual(["claimed", "claimed"]);

    const assignment = [...queue.state().dispatches.values()].map(dispatch => assignmentForDispatch(queue.state(), dispatch.dispatch_id)).find(candidate => candidate.claims[0].work.ai_scan);
    const workerRun = {
      github: fakeGithub({ pulls: [{ number: 302, title: "Tidy b.go", body: "" }], files: { 302: [{ filename: "pkg/x/b.go" }] } }),
      context: { repo, payload: { inputs: { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: assignment.dispatch_id }) } } },
      assignment,
    };
    const selected = await runScript(select, { ...workerRun, env: { GH_AW_WORK_QUEUE_ROLE: "worker" } });
    expect(selected.written.get("/tmp/gh-aw/agent/alert-selection.json")).toMatchObject({ claim_handle: assignment.claims[0].handle, ai_scan: true, alerts: [{ number: 10 }], covered_alerts: [{ number: 9, pull_numbers: [302] }] });
    expect(selected.written.get("/tmp/gh-aw/agent/open-pull-files.json")).toEqual([{ number: 302, files: ["pkg/x/b.go"] }]);
    expect(selected.intents).toEqual([]);
    await expect(runScript(select, { ...workerRun, env: { GH_AW_WORK_QUEUE_ROLE: "dispatcher" } })).rejects.toThrow(/only as a work-queue worker/);

    const rerun = await runScript(prepare, { github: fakeGithub(humanFix), works: queue.works() });
    expect(rerun.written.get("/tmp/gh-aw/agent/code-scanning-plan.json")).toMatchObject({ admitted: 0, covered: [{ number: 11, pull_numbers: [300] }] });
    const before = queue.works();
    queue.apply(rerun.intents);
    expect(queue.works()).toEqual(before);
  });

  it("compiles the alert gate into pre-activation and binds dispatcher and worker queue roles", () => {
    const controls = compiled => compiled.jobs.safe_outputs.steps.find(step => step.id === "work_queue_controls");
    const dispatcherLock = readWorkflowFile("code-scanning-dispatcher", true);
    const preActivation = dispatcherLock.jobs.pre_activation;
    expect(preActivation.permissions).toMatchObject({ "pull-requests": "read", "security-events": "read" });
    expect(preActivation.steps.find(step => step.id === "alerts").with.script).toContain("hasUncoveredAlert");
    expect(preActivation.outputs.has_alerts).toBe("${{ steps.alerts.outputs.has_alerts }}");
    expect(dispatcherLock.jobs.activation.if).toContain("needs.pre_activation.outputs.has_alerts == 'true'");
    expect(controls(dispatcherLock).env.GH_AW_WORK_QUEUE_ROLE).toBe("dispatcher");
    const handlerConfig = JSON.parse(dispatcherLock.jobs.safe_outputs.steps.find(step => step.env?.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG).env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG);
    expect(handlerConfig.dispatch_workflow).toMatchObject({ work_queue_enabled: true, work_queue_workflows: ["code-scanning-fixer-worker"], max: 3 });
    expect(handlerConfig.dispatch_workflow).not.toHaveProperty("work_queue_dispatch_credential");

    const workerLock = readWorkflowFile("code-scanning-fixer-worker", true);
    expect(controls(workerLock).env.GH_AW_WORK_QUEUE_ROLE).toBe("worker");
    expect(workerLock.jobs.activation.steps.some(step => step.id === "work_queue_snapshot")).toBe(true);
    const agentSteps = workerLock.jobs.agent.steps.map(step => step.name ?? "");
    const selection = agentSteps.indexOf("Select the assigned code scanning cluster");
    expect(selection).toBeGreaterThan(-1);
    expect(selection).toBeLessThan(agentSteps.findIndex(name => name.startsWith("Execute GitHub Copilot")));
  });
});
