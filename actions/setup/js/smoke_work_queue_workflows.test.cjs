import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { parseDocument } from "./work_queue_yaml.cjs";

function readWorkflow(name, compiled = false) {
  const source = readFileSync(new URL(`../../../.github/workflows/smoke-work-queue-${name}.${compiled ? "lock.yml" : "md"}`, import.meta.url), "utf8");
  const yaml = compiled ? source : source.match(/^---\n([\s\S]*?)\n---\n/)[1];
  const document = parseDocument(yaml, { version: "1.2", uniqueKeys: true });
  if (document.errors.length) throw new AggregateError(document.errors, "Invalid smoke workflow");
  return document.toJS();
}

function verification(name) {
  const workflow = readWorkflow(name);
  const job = workflow.jobs[`verify_smoke_${name === "worker" ? "completion" : "dispatch"}`];
  const step = job.steps[0];
  const script = step.run.match(/node <<'JS'\n([\s\S]*?)\nJS/)[1];
  return { job, step, run: env => runInNewContext(script, { require: () => assert, process: { env } }) };
}

const successfulJobs = { SMOKE_AGENT_RESULT: "success", SMOKE_SAFE_OUTPUTS_RESULT: "success" };
const boundLaunch = { dispatch_id: "dispatch:smoke", state: "bound", released: false, dispatched: true, run_id: "42", html_url: "https://github.com/owner/repo/actions/runs/42" };
// Shapes emitted by dispatchQueueIntent and work_queue_dispatch.main, not agent intents.
const dispatchReceipt = (overrides = {}) => ({
  version: 3,
  status: "ok",
  success: true,
  receipts: [
    { status: "durable", request_id: "request:submit" },
    { status: "durable", success: true, request_id: "request:dispatch", dispatches: 1, launches: [boundLaunch], ...overrides },
  ],
  remaining_dispatches: 0,
});
const dispatchEnv = receipt => ({ ...successfulJobs, SMOKE_DISPATCH_RECEIPT: JSON.stringify(receipt) });

describe("native work queue smoke verification", () => {
  it.each(["dispatcher", "worker"])("%s always checks completed upstream jobs, including skipped agents", name => {
    const { job, step, run } = verification(name);
    expect(job.if).toBe("always()");
    expect(job.needs).toEqual(["agent", "safe_outputs"]);
    expect(step.env.SMOKE_AGENT_RESULT).toBe("${{ needs.agent.result }}");
    expect(step.env.SMOKE_SAFE_OUTPUTS_RESULT).toBe("${{ needs.safe_outputs.result }}");
    const env = name === "worker" ? { ...successfulJobs, SMOKE_QUEUE_OUTCOME: "completed" } : dispatchEnv(dispatchReceipt());
    for (const variable of ["SMOKE_AGENT_RESULT", "SMOKE_SAFE_OUTPUTS_RESULT"]) {
      for (const result of ["skipped", "failure", "cancelled", ""]) {
        expect(() => run({ ...env, [variable]: result })).toThrow();
      }
    }
  });

  it("requires the trusted controls receipt and a verified native worker launch", () => {
    const { step, run } = verification("dispatcher");
    expect(step.env.SMOKE_DISPATCH_RECEIPT).toBe("${{ needs.safe_outputs.outputs.work_queue_requests }}");
    expect(() => run(dispatchEnv(dispatchReceipt()))).not.toThrow();
    for (const invalid of [
      dispatchReceipt({ status: "no_grant", dispatches: 0, launches: [] }),
      dispatchReceipt({ status: "staged_preview", dispatches: 0, launches: [] }),
      dispatchReceipt({ launches: [] }),
      dispatchReceipt({ dispatches: 2, launches: [boundLaunch, boundLaunch] }),
      dispatchReceipt({ launches: [{ ...boundLaunch, state: "launch_unresolved" }] }),
      dispatchReceipt({ launches: [{ ...boundLaunch, state: "released", released: true, dispatched: false }] }),
      dispatchReceipt({ launches: [{ ...boundLaunch, state: "rejected" }] }),
      dispatchReceipt({ launches: [{ ...boundLaunch, run_id: undefined }] }),
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
        expect(compiled.jobs.safe_outputs.outputs.work_queue_outcome).toBe("${{ steps.process_safe_outputs.outputs.work_queue_outcome }}");
      }
    }
  });
});
