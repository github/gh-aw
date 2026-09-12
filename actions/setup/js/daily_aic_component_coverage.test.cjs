import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const { getRunAIC } = require("./check_daily_aic_workflow_guardrail.cjs");
const { sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");
const { createAPIBudget } = require("./daily_aic_api_budget.cjs");
const time = "2025-01-01T12:00:00Z";
const later = "2025-01-01T12:30:00Z";
const job = (name, overrides = {}) => ({
  id: name === "agent" ? 1 : name === "detection" ? 2 : 3,
  name,
  status: "completed",
  conclusion: "success",
  run_attempt: 1,
  started_at: time,
  completed_at: time,
  ...overrides,
});
let directory;
beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-components-"));
  global.core = { info: vi.fn(), warning: vi.fn() };
});
afterEach(() => {
  vi.restoreAllMocks();
  fs.rmSync(directory, { recursive: true, force: true });
  delete global.core;
});

function evaluate(files, jobs, overrides = {}) {
  const list = vi.fn(async () => ({ status: 200, headers: {}, data: { jobs } }));
  const client = {
    listArtifacts: vi.fn(async () => ({
      artifacts: [
        { id: 10, name: "usage", createdAt: new Date(overrides.artifactTime || later) },
        ...["agent", "detection", "evals"].map(name => {
          const latest = jobs.filter(item => item.name === name && item.conclusion !== "skipped").sort((a, b) => b.run_attempt - a.run_attempt)[0];
          return { id: latest?.id, name, createdAt: new Date(overrides.producerTime || latest?.completed_at || time) };
        }),
      ],
    })),
    downloadArtifact: vi.fn(async (_id, options) => {
      for (const [name, value] of Object.entries(files)) {
        const file = path.join(options.path, ...name.split("/"));
        fs.mkdirSync(path.dirname(file), { recursive: true });
        fs.writeFileSync(file, value);
      }
      return { downloadPath: options.path };
    }),
  };
  return {
    list,
    client,
    result: getRunAIC(
      client,
      1,
      "synthetic",
      "example",
      "project",
      {
        id: 1,
        run_attempt: overrides.attempt || 1,
        run_started_at: overrides.runStarted || time,
      },
      { github: { rest: { actions: { listJobsForWorkflowRun: list } } }, budget: createAPIBudget() }
    ),
  };
}

it("rejects executed evals with missing accounting despite valid agent usage", async () => {
  const f = evaluate(
    {
      "agent_usage.jsonl": '{"aic":2}',
      "detection_usage.jsonl": "",
      "agent/token_usage.jsonl": "",
      "detection/token_usage.jsonl": "",
      "evals.jsonl": "",
    },
    [job("agent"), job("evals")]
  );
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component in run 1 (attempt 1, job 3, conclusion success)");
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"evals"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"state":"empty"'));
  expect(f.list).toHaveBeenCalledOnce();
});

it("counts an empty detection accounting file as zero AIC", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
      "detection/token_usage.jsonl": "",
      "detection_usage.jsonl": '{"aic":99}',
    },
    [job("agent"), job("detection")]
  );

  await expect(f.result).resolves.toBe(2);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"detection"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"empty_detection_accounting"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"source":"detection/token_usage.jsonl"'));
});

it("still requires accounting when detection/token_usage.jsonl is missing (not empty)", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
      "detection_usage.jsonl": "",
    },
    [job("agent"), job("detection")]
  );
  await expect(f.result).rejects.toThrow("Missing accounting for executed detection component");
});

it.each(["skipped", "not-configured"])("accepts %s detection without requiring placeholder data", async state => {
  const jobs = state === "skipped" ? [job("agent"), job("detection", { conclusion: "skipped" })] : [job("agent")];
  const f = evaluate(
    {
      "agent_usage.jsonl": '{"aic":2}',
      "agent/token_usage.jsonl": "",
      "detection/token_usage.jsonl": "",
    },
    jobs
  );
  await expect(f.result).resolves.toBe(2);
  if (state === "skipped") {
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"job_skipped"'));
  }
});

it("accepts aggregated agent accounting when a failed request produced no raw usage", async () => {
  const f = evaluate(
    {
      "agent_usage.json": '{"input_tokens":0,"output_tokens":0,"ai_credits":0}',
      "agent_usage.jsonl": "",
      "agent/token_usage.jsonl": "",
    },
    [job("agent", { conclusion: "failure" })]
  );
  await expect(f.result).resolves.toBe(0);
});

it("counts empty authoritative agent accounting as zero when the agent job failed", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": "" }, [job("agent", { conclusion: "failure" })]);
  await expect(f.result).resolves.toBe(0);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"failed_before_accounting"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"source":"agent/token_usage.jsonl"'));
});

it("still requires accounting when a failed agent job has no authoritative source", async () => {
  const f = evaluate({}, [job("agent", { conclusion: "failure" })]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent component");
});

it("still requires accounting when an agent job succeeds", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": "" }, [job("agent")]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent component");
});

it("accepts a completed run with no jobs as zero usage", async () => {
  const f = evaluate({}, []);
  await expect(f.result).resolves.toBe(0);
  expect(f.client.listArtifacts).not.toHaveBeenCalled();
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"no_billable_jobs"'));
});

it("accepts a completed run with all billable jobs skipped as zero usage", async () => {
  const f = evaluate({}, [job("agent", { conclusion: "skipped" }), job("detection", { conclusion: "skipped" })]);
  await expect(f.result).resolves.toBe(0);
  expect(f.client.listArtifacts).not.toHaveBeenCalled();
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"all_billable_jobs_skipped"'));
});

it("rejects a non-empty job list without the required agent job", async () => {
  const f = evaluate({}, [job("detection")]);
  await expect(f.result).rejects.toThrow("Cannot prove complete billable-component coverage");
});

it.each(["agent", "detection"])("accepts provable zero usage when %s execution never started", async component => {
  const f = evaluate(
    {
      [`${component}/execution.json`]: JSON.stringify({
        version: 1,
        component,
        run_id: 1,
        run_attempt: 1,
        state: "not_started",
      }),
    },
    component === "agent" ? [job("agent", { conclusion: "failure" })] : [job("agent", { conclusion: "skipped" }), job("detection", { conclusion: "failure" })]
  );
  await expect(f.result).resolves.toBe(0);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"execution_not_started"'));
});

it.each([
  ["started execution", { version: 1, component: "agent", run_id: 1, run_attempt: 1, state: "started" }],
  ["malformed evidence", { version: 1, component: "agent", run_id: 1, run_attempt: 1 }],
])("fails closed for missing accounting after %s", async (_name, evidence) => {
  const f = evaluate({ "agent/execution.json": JSON.stringify(evidence) }, [job("agent", { conclusion: "failure" })]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent");
});

it("rejects stale zero-usage evidence from an earlier rerun attempt", async () => {
  const f = evaluate(
    {
      "agent/execution.json": JSON.stringify({
        version: 1,
        component: "agent",
        run_id: 1,
        run_attempt: 1,
        state: "not_started",
      }),
    },
    [job("agent", { id: 10, run_attempt: 2, conclusion: "failure", started_at: later, completed_at: later })],
    { attempt: 2, runStarted: later, producerTime: later }
  );
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent");
});

it("selects raw accounting once per component instead of summing overlapping summaries", async () => {
  const f = evaluate(
    {
      "agent_usage.jsonl": '{"aic":99}',
      "agent_usage.json": '{"ai_credits":999}',
      "agent/token_usage.jsonl": '{"aic":2}',
      "detection_usage.jsonl": '{"aic":99}',
      "detection/token_usage.jsonl": '{"aic":3}',
      "evals.jsonl": '{"question":"Is the result valid?","answer":"YES"}',
      "evals/token_usage.jsonl": '{"ai_credits_this_response":4,"ai_credits_total":4}',
    },
    [job("agent"), job("detection"), job("evals")]
  );
  await expect(f.result).resolves.toBe(9);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"accounting_file","source":"agent/token_usage.jsonl"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"accounting_file","source":"detection/token_usage.jsonl"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"accounting_file","source":"evals/token_usage.jsonl"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Computed covered component total"));
});

it("does not fall back to a valid summary when authoritative raw data is malformed", async () => {
  const f = evaluate(
    {
      "agent_usage.jsonl": '{"aic":2}',
      "agent/token_usage.jsonl": '{"aic":false}',
    },
    [job("agent")]
  );
  await expect(f.result).rejects.toThrow("could not be resolved");
});

it("does not fall back to a valid summary when authoritative raw data is unreadable", async () => {
  const f = evaluate({}, [job("agent")]);
  f.client.downloadArtifact.mockImplementation(async (_id, options) => {
    fs.writeFileSync(path.join(options.path, "agent_usage.jsonl"), '{"aic":2}');
    fs.mkdirSync(path.join(options.path, "agent/token_usage.jsonl"), { recursive: true });
    return { downloadPath: options.path };
  });

  await expect(f.result).rejects.toThrow("agent/token_usage.jsonl is unreadable");
});

it("counts carried-forward agent usage and rerun detection once after a failed-only rerun", async () => {
  const f = evaluate(
    {
      "agent_usage.jsonl": '{"aic":2}',
      "detection_usage.jsonl": '{"aic":3}',
    },
    [job("detection", { id: 20, run_attempt: 2, started_at: later, completed_at: later }), job("agent"), job("detection", { conclusion: "failure" })],
    { attempt: 2, runStarted: later }
  );
  await expect(f.result).resolves.toBe(5);
  expect(f.list).toHaveBeenCalledWith(expect.objectContaining({ filter: "all" }));
});

it("accepts an earlier usage artifact when only a nonbillable job was rerun", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":2}' }, [job("agent"), { ...job("conclusion"), id: 8, run_attempt: 2, started_at: later, completed_at: later }], { attempt: 2, runStarted: later, artifactTime: time });
  await expect(f.result).resolves.toBe(2);
});

it("does not erase previously executed usage when a later attempt skips that component", async () => {
  const f = evaluate(
    { "agent_usage.jsonl": '{"aic":2}', "detection_usage.jsonl": '{"aic":3}' },
    [job("agent"), job("agent", { id: 10, run_attempt: 2, conclusion: "skipped", started_at: later, completed_at: later }), job("detection", { id: 20, run_attempt: 2, started_at: later, completed_at: later })],
    { attempt: 2, runStarted: later }
  );
  await expect(f.result).resolves.toBe(5);
});

it("rejects missing carried-forward component data in a newly uploaded rerun artifact", async () => {
  const f = evaluate({ "agent_usage.jsonl": "", "detection_usage.jsonl": '{"aic":3}' }, [job("agent"), job("detection", { id: 20, run_attempt: 2, started_at: later, completed_at: later })], { attempt: 2, runStarted: later });
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent");
});

it("rejects a newly repacked stale producer after a component rerun fails", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":2}', "detection_usage.jsonl": '{"aic":3}' }, [job("agent"), job("detection", { id: 20, run_attempt: 2, conclusion: "failure", started_at: later, completed_at: later })], {
    attempt: 2,
    runStarted: later,
    producerTime: time,
  });
  await expect(f.result).rejects.toThrow("Cannot verify the detection producer");
});

it("rejects an artifact from before the newly executed component completed", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":2}', "detection_usage.jsonl": '{"aic":3}' }, [job("agent"), job("detection", { id: 20, run_attempt: 2, started_at: later, completed_at: later })], {
    attempt: 2,
    runStarted: later,
    artifactTime: time,
  });
  await expect(f.result).rejects.toThrow("does not cover the detection");
});

it.each([
  { ai_credits: false },
  { aiCredits: true },
  { aic: null },
  { aic: [] },
  { aic: -1 },
  { aic: "NaN" },
  { aic: "Infinity" },
  { aic: "" },
  { aic: "0x10" },
  { provider: "openai", model: "gpt-4o", input_tokens: "invalid", output_tokens: 500 },
  { aic: 2, usage: { inputTokens: false } },
  { aic: 2, cacheReadTokens: -1 },
  { aic: 2, reasoning_tokens: "bad" },
  { ai_credits_this_response: false },
  { ai_credits_this_response: 2, ai_credits_total: -1 },
])("strict accounting rejects present invalid numeric fields: %j", record => {
  const file = path.join(directory, "usage.jsonl");
  fs.writeFileSync(file, '{"aic":2}\n' + JSON.stringify(record));
  expect(() => sumAICFromUsageJSONLFiles([file], { strict: true })).toThrow("could not be resolved");
});

it("uses AWF response deltas once, never cumulative totals or overlapping token estimates", () => {
  const file = path.join(directory, "usage.jsonl");
  const first = JSON.stringify({ request_id: "a", ai_credits_this_response: "2", ai_credits_total: 2, input_tokens: 100 });
  fs.writeFileSync(file, first + "\n" + first + '\n{"request_id":"b","ai_credits_this_response":3,"ai_credits_total":5}');
  expect(sumAICFromUsageJSONLFiles([file], { strict: true })).toBe(5);
});

it("preserves supported decimal numeric strings and the legacy non-strict parser", () => {
  const file = path.join(directory, "usage.jsonl");
  fs.writeFileSync(file, '{"aic":"2.5"}\n{"usage":{"ai_credits":"1e1"}}');
  expect(sumAICFromUsageJSONLFiles([file], { strict: true })).toBe(12.5);
  fs.writeFileSync(file, '{"aic":2}\n{"provider":"openai","model":"gpt-4o","input_tokens":"invalid","output_tokens":500}');
  expect(sumAICFromUsageJSONLFiles([file])).toBe(2.5);
  fs.writeFileSync(file, '{"ai_credits":false}');
  expect(sumAICFromUsageJSONLFiles([file])).toBe(0);
});
