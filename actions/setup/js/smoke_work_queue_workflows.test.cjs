import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

function readWorkflow(name, compiled = false) {
  const source = readFileSync(new URL(`../../../.github/workflows/smoke-work-queue-${name}.${compiled ? "lock.yml" : "md"}`, import.meta.url), "utf8");
  const yaml = compiled ? source : source.match(/^---\n([\s\S]*?)\n---\n/)[1];
  const document = readDocumentMetadata(yaml, { version: "1.2", uniqueKeys: true });
  return document.value;
}

function verification(name) {
  const workflow = readWorkflow(name);
  const job = workflow.jobs[`verify_smoke_${name === "worker" ? "completion" : "dispatch"}`];
  const step = job.steps[0];
  const script = step.run.match(/node <<'JS'\n([\s\S]*?)\nJS/)[1];
  return { job, step, run: env => runInNewContext(script, { require: () => assert, process: { env } }) };
}

function prepareDispatcher(policy) {
  const step = readWorkflow("dispatcher").steps[0];
  const script = step.with.script;
  const staged = [];
  const fs = { mkdirSync() {}, writeFileSync() {} };
  const path = { join: (...parts) => parts.join("/") };
  const require = name => {
    if (name === "node:fs") return fs;
    if (name === "node:path") return path;
    if (name.endsWith("work_queue_mcp_server.cjs")) return { loadWorkQueueSnapshot: () => ({ projection: { policy } }) };
    if (name.endsWith("work_queue_intents.cjs")) return { stagePlanIntents: (_filename, plan) => staged.push(plan) };
    throw new Error(`Unexpected module ${name}`);
  };
  const run = () =>
    runInNewContext(script, {
      require,
      process: { env: { RUNNER_TEMP: "/tmp" } },
      context: { runId: "123", repo: { owner: "owner", repo: "repo" }, payload: { repository: { id: 456 } } },
      core: { info() {} },
    });
  return { run, staged };
}

const successfulJobs = { SMOKE_ACTIVATION_RESULT: "success", SMOKE_AGENT_RESULT: "success", SMOKE_SAFE_OUTPUTS_RESULT: "success" };
const boundLaunch = { dispatch_id: "dispatch:smoke", state: "bound", released: false, dispatched: true, run_id: "42", html_url: "https://github.com/owner/repo/actions/runs/42" };
const pendingLaunch = { ...boundLaunch, state: "activation_pending" };
const boundLaunches = [boundLaunch, { ...boundLaunch, dispatch_id: "dispatch:smoke-2", run_id: "43" }, { ...boundLaunch, dispatch_id: "dispatch:smoke-3", run_id: "44" }];
const pendingLaunches = boundLaunches.map(launch => ({ ...launch, state: "activation_pending" }));
const launchesWith = launch => [launch, ...pendingLaunches.slice(1)];
// Shapes emitted by dispatchQueueIntent and work_queue_dispatch.main, not agent intents.
const dispatchReceipt = (overrides = {}) => ({
  version: 3,
  status: "activation_pending",
  success: true,
  receipts: [
    { status: "durable", request_id: "request:submit" },
    { status: "durable", success: true, request_id: "request:dispatch", dispatches: 3, launches: pendingLaunches, ...overrides },
  ],
  remaining_dispatches: 0,
});
const dispatchEnv = receipt => ({ ...successfulJobs, SMOKE_DISPATCH_RECEIPT: JSON.stringify(receipt) });

describe("native work queue smoke verification", () => {
  it("does not stage smoke Work until the installed pool allows three Claims", () => {
    const oldPolicy = prepareDispatcher({ pools: { "smoke-work-queue": { logical_limit: 1, native_limit: 1 } } });
    expect(() => oldPolicy.run()).toThrow("Installed smoke-work-queue Policy does not allow three Claims");
    expect(oldPolicy.staged).toEqual([]);

    const uninitialized = prepareDispatcher(null);
    uninitialized.run();
    expect(uninitialized.staged[0].nodes).toHaveLength(3);

    const installed = prepareDispatcher({ pools: { "smoke-work-queue": { logical_limit: 3, native_limit: 3 } } });
    installed.run();
    expect(installed.staged[0].nodes).toHaveLength(3);
  });

  it("uses isolated worker concurrency and a read-only bounded monitor snapshot", () => {
    const worker = readWorkflow("worker");
    expect(worker.concurrency["job-discriminator"]).toBe("${{ github.run_id }}");

    const monitor = readWorkflow("monitor");
    expect(monitor.checkout["pull-request"]).toBe(false);
    expect(monitor.tools["cli-proxy"]).toBe(true);
    expect(monitor.tools["work-queue"]).toBe(true);
    expect(monitor.tools.bash).toContain("work-queue:*");
    expect(monitor.steps[0].with.script).toContain("work_queue_mcp_server.cjs");

    const monitorLock = readFileSync(new URL("../../../.github/workflows/smoke-work-queue-monitor.lock.yml", import.meta.url), "utf8");
    expect(monitorLock).toContain('GH_AW_WORK_QUEUE_ROLE: "observer"');
    expect(monitorLock).toContain('"tools": ["work_queue_read", "work_queue_explain"]');
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

  it("requires a trusted accepted dispatch receipt without waiting for worker activation", () => {
    const { step, run } = verification("dispatcher");
    expect(step.env.SMOKE_DISPATCH_RECEIPT).toBe("${{ needs.safe_outputs.outputs.work_queue_requests }}");
    expect(() => run(dispatchEnv(dispatchReceipt()))).not.toThrow();
    expect(() => run(dispatchEnv({ ...dispatchReceipt({ launches: boundLaunches }), status: "ok" }))).not.toThrow();
    for (const invalid of [
      dispatchReceipt({ status: "no_grant", dispatches: 0, launches: [] }),
      dispatchReceipt({ status: "staged_preview", dispatches: 0, launches: [] }),
      dispatchReceipt({ launches: [] }),
      dispatchReceipt({ dispatches: 2, launches: [boundLaunch, boundLaunch] }),
      dispatchReceipt({ launches: launchesWith({ ...boundLaunch, state: "launch_unresolved" }) }),
      dispatchReceipt({ launches: launchesWith({ ...boundLaunch, state: "released", released: true, dispatched: false }) }),
      dispatchReceipt({ launches: launchesWith({ ...boundLaunch, state: "rejected" }) }),
      dispatchReceipt({ launches: launchesWith({ ...pendingLaunch, dispatched: false }) }),
      dispatchReceipt({ success: false }),
      dispatchReceipt({ launches: launchesWith({ ...boundLaunch, run_id: undefined }) }),
      dispatchReceipt({ acknowledgement_only: true, launches: [] }),
      { ...dispatchReceipt(), success: false },
      { ...dispatchReceipt(), receipts: [] },
    ]) {
      expect(() => run(dispatchEnv(invalid))).toThrow();
    }
    expect(() => run({ ...successfulJobs, SMOKE_DISPATCH_RECEIPT: "" })).toThrow();
    expect(() => run({ ...successfulJobs })).toThrow();
  });

  it("requires a verified Result even if agent execution and safe outputs succeeded", () => {
    const { step, run } = verification("worker");
    expect(step.env.SMOKE_QUEUE_OUTCOME).toBe("${{ needs.safe_outputs.outputs.work_queue_outcome }}");
    expect(() => run({ ...successfulJobs, SMOKE_QUEUE_OUTCOME: "completed" })).not.toThrow();
    // Missing finish artifacts cancel the Claim despite successful trusted processing.
    for (const outcome of ["cancelled", "completed_with_cancellations", "delivery_pending", "staged_preview", ""]) {
      expect(() => run({ ...successfulJobs, SMOKE_QUEUE_OUTCOME: outcome })).toThrow();
    }
    expect(() => run(successfulJobs)).toThrow();
  });

  it("requires trusted worker activation separately from a completed Result", () => {
    const { step, run } = verification("worker");
    expect(step.env.SMOKE_ACTIVATION_RESULT).toBe("${{ needs.activation.result }}");
    const { SMOKE_ACTIVATION_RESULT, ...withoutActivation } = successfulJobs;
    expect(() => run({ ...withoutActivation, SMOKE_QUEUE_OUTCOME: "completed" })).toThrow();
  });

  it("compiles always-running verifiers and exports only trusted receipt and outcome sources", () => {
    for (const name of ["dispatcher", "worker"]) {
      const compiled = readWorkflow(name, true);
      const id = `verify_smoke_${name === "worker" ? "completion" : "dispatch"}`;
      expect(compiled.jobs[id].if).toBe("always()");
      expect(compiled.jobs[id].needs).toEqual(expect.arrayContaining(["agent", "safe_outputs"]));
      expect(compiled.jobs[id].steps).toEqual(expect.arrayContaining(readWorkflow(name).jobs[id].steps));
      expect(compiled.jobs.safe_outputs.outputs.work_queue_requests).toBe("${{ steps.work_queue_controls.outputs.work_queue_requests }}");
      const controls = compiled.jobs.safe_outputs.steps.find(step => step.id === "work_queue_controls");
      expect(controls.env.GH_AW_WORK_QUEUE_ROLE).toBe(name === "worker" ? "worker" : "dispatcher");
      expect(controls.with.script).toContain("work_queue_control_adapter.cjs");
      expect(controls.with.script).toContain("await main({ core, github, context");
      if (name === "worker") {
        expect(compiled.jobs[id].needs).toContain("activation");
        const activation = compiled.jobs.activation.steps.find(step => step.id === "work_queue_snapshot");
        expect(activation.with.script).toContain("write_work_queue_snapshot.cjs");
        expect(compiled.jobs.safe_outputs.outputs.work_queue_outcome).toBe("${{ steps.process_safe_outputs.outputs.work_queue_outcome }}");
      }
    }
  });
});
