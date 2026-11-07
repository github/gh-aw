import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const { getRunAIC } = require("./check_daily_aic_workflow_guardrail.cjs");
const { sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");
const { createAPIBudget } = require("./daily_aic_api_budget.cjs");
const { loadBillableJobs } = require("./daily_aic_component_coverage.cjs");
const { captureExecutionEvidenceConfiguration, shouldSkipForNoopSafeOutputs } = require("./harness_retry_runner.cjs");
const time = "2025-01-01T12:00:00Z";
const later = "2025-01-01T12:30:00Z";
const componentName = name => {
  const last = name.split(" / ").pop().toLowerCase();
  return last === "evaluations" ? "evals" : last;
};
const job = (name, overrides = {}) => ({
  id: componentName(name) === "agent" ? 1 : componentName(name) === "detection" ? 2 : 3,
  name,
  status: "completed",
  conclusion: "success",
  run_attempt: 1,
  started_at: time,
  completed_at: time,
  ...overrides,
});
const callerJobs = prefix => [job(`${prefix}activation`, { id: 90 }), job(`${prefix}conclusion`, { id: 91 })];
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
          const latest = jobs.filter(item => componentName(item.name) === name && item.conclusion !== "skipped").sort((a, b) => b.run_attempt - a.run_attempt)[0];
          return { id: latest?.id, name, createdAt: new Date(overrides.producerTime || latest?.completed_at || time) };
        }),
      ],
    })),
    downloadArtifact: vi.fn(async (_id, options) => {
      const artifactFiles = _id === 1 && overrides.agentFiles ? overrides.agentFiles : files;
      for (const [name, value] of Object.entries(artifactFiles)) {
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

const failedDecisionsStatus = {
  status: "not_evaluated",
  reason_code: "judge_failed",
  reason: "The Decisions evals request finished with outcome failure; no answers were recorded.",
  engine: "decisions",
  run_id: "1",
  run_attempt: "1",
  execution_outcome: "failure",
  execution_state: "started",
};

const unavailableDecisionsStatus = {
  ...failedDecisionsStatus,
  reason_code: "model_unavailable",
  reason: "The Decisions model is unavailable; no answers were recorded.",
  execution_outcome: "success",
  execution_state: "model_unavailable",
};

it.each([undefined, ""])("T-AIC-DG-028 preserves other component usage when a successful Decisions job skips an unavailable model with usage %j", async usage => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":51.5}',
      "detection/token_usage.jsonl": '{"aic":6.5}',
      "evals/evals_status.json": JSON.stringify(unavailableDecisionsStatus),
      ...(usage === undefined ? {} : { "evals/token_usage.jsonl": usage }),
      ...(usage === undefined ? {} : { "evals.jsonl": "", "evals/evals.jsonl": "" }),
    },
    [job("agent"), job("detection"), job("evals")]
  );
  await expect(f.result).resolves.toBe(58);
  expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("provider billing remains unverified"));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"model_unavailable"'));
});

it.each([
  ["another run", { run_id: "2" }],
  ["another attempt", { run_attempt: "2" }],
  ["missing run identity", { run_id: undefined }],
  ["missing attempt identity", { run_attempt: undefined }],
  ["another engine", { engine: "copilot" }],
  ["missing engine", { engine: undefined }],
  ["failed request", { execution_outcome: "failure" }],
  ["cancelled request", { execution_outcome: "cancelled" }],
  ["missing outcome", { execution_outcome: undefined }],
  ["started state", { execution_state: "started" }],
  ["missing state", { execution_state: undefined }],
  ["failed judge reason", { reason_code: "judge_failed" }],
  ["no assistant response reason", { reason_code: "no_assistant_response" }],
  ["unknown reason", { reason_code: "unknown" }],
  ["missing reason text", { reason: undefined }],
  ["evaluated status", { status: "evaluated" }],
  ["missing status", { status: undefined }],
])("does not accept unavailable-model evidence with %s", async (_description, overrides) => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": JSON.stringify({ ...unavailableDecisionsStatus, ...overrides }) }, [job("agent"), job("evals")]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
  expect(global.core.warning).not.toHaveBeenCalled();
});

it.each(["cancelled", "timed_out"])("does not apply unavailable-model evidence to a %s evals job", async conclusion => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": JSON.stringify(unavailableDecisionsStatus) }, [job("agent"), job("evals", { conclusion })]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it.each(["evals.jsonl", "evals/evals.jsonl"])("does not accept unavailable-model evidence alongside answer records in %s", async answerFile => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
      "evals/evals_status.json": JSON.stringify(unavailableDecisionsStatus),
      [answerFile]: '{"question":"Is the result valid?","answer":"YES"}',
    },
    [job("agent"), job("evals")]
  );
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it.each(["", "{invalid", "null"])("does not accept corrupt unavailable-model evidence (%j)", async status => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": status }, [job("agent"), job("evals")]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it.each(["evals/token_usage.jsonl", "evals.jsonl", "evals/evals.jsonl", "evals/evals_status.json"])("does not accept unavailable-model evidence with unreadable %s", async unreadableFile => {
  const f = evaluate({}, [job("agent"), job("evals")]);
  f.client.downloadArtifact.mockImplementation(async (_id, options) => {
    fs.mkdirSync(path.join(options.path, "agent"), { recursive: true });
    fs.writeFileSync(path.join(options.path, "agent/token_usage.jsonl"), '{"aic":2}');
    fs.mkdirSync(path.join(options.path, "evals"), { recursive: true });
    if (unreadableFile !== "evals/evals_status.json") fs.writeFileSync(path.join(options.path, "evals/evals_status.json"), JSON.stringify(unavailableDecisionsStatus));
    fs.mkdirSync(path.join(options.path, unreadableFile), { recursive: true });
    return { downloadPath: options.path };
  });
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
  expect(global.core.warning).not.toHaveBeenCalled();
});

it.each([
  ["aggregate", { artifactTime: time }, "Usage artifact does not cover the evals component attempt"],
  ["producer", { attempt: 2, producerTime: time }, "Cannot verify the evals producer artifact for its job attempt"],
])("does not accept unavailable-model evidence without verified %s coverage", async (_description, overrides, error) => {
  const attempt = overrides.attempt || 1;
  const f = evaluate(
    { "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": JSON.stringify({ ...unavailableDecisionsStatus, run_attempt: String(attempt) }) },
    [job("agent", { run_attempt: attempt }), job("evals", { run_attempt: attempt, started_at: later, completed_at: later })],
    overrides
  );
  await expect(f.result).rejects.toThrow(error);
  expect(global.core.warning).not.toHaveBeenCalled();
});

it("accepts unavailable-model evidence bound to the selected earlier successful attempt", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": JSON.stringify(unavailableDecisionsStatus) }, [job("agent", { run_attempt: 2 }), job("evals")], { attempt: 2 });
  await expect(f.result).resolves.toBe(2);
});

it("rejects unavailable-model evidence bound to the run attempt rather than the selected job attempt", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": JSON.stringify({ ...unavailableDecisionsStatus, run_attempt: "2" }) }, [job("agent", { run_attempt: 2 }), job("evals")], { attempt: 2 });
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it.each([
  ['{"aic":3}', 5],
  ["{invalid", null],
  ['{"input_tokens":2,"model":"unknown"}', null],
])("does not replace recorded evals usage %j with an unavailable-model fallback", async (usage, total) => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/token_usage.jsonl": usage, "evals/evals_status.json": JSON.stringify(unavailableDecisionsStatus) }, [job("agent"), job("evals")]);
  if (total === null) await expect(f.result).rejects.toThrow();
  else await expect(f.result).resolves.toBe(total);
  expect(global.core.warning).not.toHaveBeenCalled();
});

it("preserves other component usage when an advisory Decisions judge fails in a successful job", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":51.64972}',
      "detection/token_usage.jsonl": '{"aic":6.60246}',
      "evals/evals_status.json": JSON.stringify(failedDecisionsStatus),
    },
    [job("agent"), job("detection"), job("evals")]
  );

  await expect(f.result).resolves.toBeCloseTo(58.25218);
  expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("provider billing remains unverified"));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"evals"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"failed_before_accounting"'));
});

it.each([
  ["another run", { run_id: "2" }],
  ["another attempt", { run_attempt: "2" }],
  ["missing run identity", { run_id: undefined }],
  ["missing attempt identity", { run_attempt: undefined }],
  ["another engine", { engine: "copilot" }],
  ["cancelled judge", { execution_outcome: "cancelled" }],
  ["successful judge", { execution_outcome: "success" }],
  ["unknown judge outcome", { execution_outcome: undefined }],
  ["unknown execution state", { execution_state: undefined }],
  ["unavailable model state", { execution_state: "model_unavailable" }],
  ["no assistant response", { reason_code: "no_assistant_response" }],
  ["not evaluated status missing", { status: undefined }],
])("does not accept failed-judge evidence with %s", async (_description, overrides) => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
      "evals/evals_status.json": JSON.stringify({ ...failedDecisionsStatus, ...overrides }),
    },
    [job("agent"), job("evals")]
  );
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it.each(["", "{invalid"])("does not accept malformed failed-judge evidence (%j)", async status => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": status }, [job("agent"), job("evals")]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it("does not apply successful-job judge evidence to a cancelled evals job", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "evals/evals_status.json": JSON.stringify(failedDecisionsStatus) }, [job("agent"), job("evals", { conclusion: "cancelled" })]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed evals component");
});

it("counts recorded evals usage even when the judge subsequently fails", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
      "evals/token_usage.jsonl": '{"aic":3}',
      "evals/evals_status.json": JSON.stringify(failedDecisionsStatus),
    },
    [job("agent"), job("evals")]
  );
  await expect(f.result).resolves.toBe(5);
  expect(global.core.warning).not.toHaveBeenCalled();
});

it("does not replace invalid nonempty evals usage with a failed-judge fallback", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
      "evals/token_usage.jsonl": "{invalid",
      "evals/evals_status.json": JSON.stringify(failedDecisionsStatus),
    },
    [job("agent"), job("evals")]
  );
  await expect(f.result).rejects.toThrow();
  expect(global.core.warning).not.toHaveBeenCalled();
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

it.each(
  [
    ["", ["agent", "detection", "evals"]],
    ["caller / ", ["agent", "detection", "evals"]],
    ["outer / caller / ", ["agent", "detection", "evals"]],
    ["", ["Agent", "Detection", "Evaluations"]],
    ["caller / ", ["Agent", "Detection", "Evaluations"]],
    ["outer / caller / ", ["Agent", "Detection", "Evaluations"]],
  ].flatMap(([prefix, names]) => [1, 2].map(attempt => [prefix, names, attempt]))
)("counts %j-prefixed %j jobs using their lowercase accounting paths (attempt %i)", async (prefix, names, attempt) => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":18.02604}',
      "detection/token_usage.jsonl": '{"aic":30.08748}',
      "evals/token_usage.jsonl": '{"aic":1}',
    },
    [...names.map(name => job(`${prefix}${name}`, { run_attempt: attempt })), ...callerJobs(prefix)],
    { attempt }
  );
  await expect(f.result).resolves.toBeCloseTo(49.11352);
  expect(f.client.listArtifacts).toHaveBeenCalledOnce();
  expect(global.core.info).not.toHaveBeenCalledWith(expect.stringContaining('"reason":"no_billable_jobs"'));
  for (const [index, component] of ["agent", "detection", "evals"].entries()) {
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining(`"component":"${component}","jobId":${index + 1},"runAttempt":${attempt}`));
  }
});

it.each(["caller / ", "outer / caller / "])("rejects a stale producer for %j-prefixed jobs on rerun", async prefix => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}' }, [job(`${prefix}agent`, { run_attempt: 2, started_at: later, completed_at: later }), ...callerJobs(prefix)], { attempt: 2, producerTime: time });
  await expect(f.result).rejects.toThrow("Cannot verify the agent producer artifact for its job attempt");
});

it.each(["caller / agent-helper", "caller / my-agent", "outer / caller / agent-helper"])("ignores look-alike component %s even in a compiler caller", async name => {
  const prefix = name.slice(0, name.lastIndexOf(" / ") + 3);
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":99}' }, [job(name), ...callerJobs(prefix)]);
  await expect(f.result).resolves.toBe(0);
  expect(f.client.listArtifacts).not.toHaveBeenCalled();
});

it.each(["security / agent", "other / detection", "outer / other / evals"])("ignores unrelated suffix match %s alongside compiler jobs", async name => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":2}', "detection/token_usage.jsonl": '{"aic":99}', "evals/token_usage.jsonl": '{"aic":99}' }, [job("Agent"), job(name, { id: 99 })]);
  await expect(f.result).resolves.toBe(2);
});

it.each(["activation", "conclusion"])("does not infer compiler ownership from %s alone", async sibling => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":99}' }, [job("security / agent"), job(`security / ${sibling}`)]);
  await expect(f.result).resolves.toBe(0);
});

it("requires compiler siblings under the same caller prefix", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":99}' }, [job("outer / caller / agent"), job("outer / activation"), job("outer / caller / conclusion")]);
  await expect(f.result).resolves.toBe(0);
});

it("recognizes compiler siblings on a later job page", async () => {
  const agent = job("outer / caller / agent");
  const pages = [[agent, ...Array.from({ length: 99 }, (_, id) => job("unrelated", { id: id + 100 }))], callerJobs("outer / caller / ")];
  const listJobsForWorkflowRun = vi.fn(async ({ page }) => ({ data: { jobs: pages[page - 1] } }));
  const { components } = await loadBillableJobs({ github: { rest: { actions: { listJobsForWorkflowRun } } }, budget: createAPIBudget() }, "example", "project", { id: 1, run_attempt: 1 });
  expect(components.get("agent")).toBe(agent);
  expect(listJobsForWorkflowRun).toHaveBeenCalledTimes(2);
});

it("treats differently cased names for the same component as ambiguous", async () => {
  const f = evaluate({}, [job("Agent", { id: 1 }), job("agent", { id: 2 })]);
  await expect(f.result).rejects.toThrow("Ambiguous daily AIC component jobs");
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

it("counts a successful sampled agent as zero when execution evidence proves inference did not start", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": "",
      "agent/execution.json": JSON.stringify({
        version: 1,
        component: "agent",
        run_id: 1,
        run_attempt: 1,
        state: "not_started",
      }),
    },
    [job("agent")]
  );

  await expect(f.result).resolves.toBe(0);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"agent"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"execution_not_started"'));
});

it("counts missing evals accounting as zero when the failed job collected no usage", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [
      job("agent"),
      job("evals", {
        conclusion: "failure",
        steps: [
          { name: "Collect evals token usage", conclusion: "success" },
          { name: "Upload evals accounting after failure", conclusion: "success" },
        ],
      }),
    ]
  );
  await expect(f.result).resolves.toBe(2);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"evals"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"failed_before_accounting"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"source":"evals/token_usage.jsonl"'));
});

it("counts missing evals accounting as zero when the successful-path upload step ran instead", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [
      job("agent"),
      job("evals", {
        conclusion: "failure",
        steps: [
          { name: "Collect evals token usage", conclusion: "success" },
          { name: "Upload evals results", conclusion: "success" },
        ],
      }),
    ]
  );
  await expect(f.result).resolves.toBe(2);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"evals"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"failed_before_accounting"'));
});

it("counts missing evals accounting as zero for a legacy failed evals job", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [
      job("agent"),
      job("evals", {
        conclusion: "failure",
        steps: [
          { name: "Install AWF binary", conclusion: "failure" },
          { name: "Collect evals token usage", conclusion: "success" },
          { name: "Upload evals results", conclusion: "skipped" },
        ],
      }),
    ]
  );
  await expect(f.result).resolves.toBe(2);
});

it("counts missing evals accounting as zero when collection did not succeed", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [
      job("agent"),
      job("evals", {
        conclusion: "failure",
        steps: [
          { name: "Collect evals token usage", conclusion: "failure" },
          { name: "Upload evals accounting after failure", conclusion: "success" },
        ],
      }),
    ]
  );
  await expect(f.result).resolves.toBe(2);
});

it("counts missing evals accounting as zero when the eval artifact upload failed", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [
      job("agent"),
      job("evals", {
        conclusion: "failure",
        steps: [
          { name: "Collect evals token usage", conclusion: "success" },
          { name: "Upload evals accounting after failure", conclusion: "failure" },
        ],
      }),
    ]
  );
  await expect(f.result).resolves.toBe(2);
});

it("counts missing evals accounting as zero when the current failure upload was skipped", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [
      job("agent"),
      job("evals", {
        conclusion: "failure",
        steps: [
          { name: "Collect evals token usage", conclusion: "success" },
          { name: "Upload evals results", conclusion: "skipped" },
          { name: "Upload evals accounting after failure", conclusion: "skipped" },
        ],
      }),
    ]
  );
  await expect(f.result).resolves.toBe(2);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"evals"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"failed_before_accounting"'));
});

it("counts missing evals accounting as zero when the job failed before any step ran", async () => {
  const f = evaluate(
    {
      "agent/token_usage.jsonl": '{"aic":2}',
    },
    [job("agent"), job("evals", { conclusion: "failure", steps: [] })]
  );
  await expect(f.result).resolves.toBe(2);
});

it.each([null, 0])("counts missing agent accounting as zero when a failed job never received a runner (%s)", async runnerId => {
  const f = evaluate({}, [job("agent", { conclusion: "failure", runner_id: runnerId, runner_name: null, steps: [] })]);
  await expect(f.result).resolves.toBe(0);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"runner_not_assigned"'));
});

it("counts a cancelled agent job as zero when GitHub proves execution never started", async () => {
  const f = evaluate(
    {
      "detection/token_usage.jsonl": '{"aic":1}',
    },
    [
      job("agent", {
        conclusion: "cancelled",
        runner_id: null,
        runner_name: null,
        steps: [],
      }),
      job("detection"),
    ]
  );
  await expect(f.result).resolves.toBe(1);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"agent"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"runner_not_assigned"'));
});

it.each([
  ["runner metadata is absent", { steps: [] }],
  ["runner name metadata is absent", { runner_id: null, steps: [] }],
  ["step metadata is absent", { runner_id: null, runner_name: null }],
  ["a runner was assigned", { runner_id: 123, runner_name: "GitHub Actions 1", steps: [] }],
  ["a step was created", { runner_id: 0, runner_name: "", steps: [{ name: "Set up job", conclusion: "cancelled" }] }],
])("still requires accounting for a cancelled agent job when %s", async (_description, metadata) => {
  const f = evaluate({}, [job("agent", { conclusion: "cancelled", ...metadata })]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent component in run 1 (attempt 1, job 1, conclusion cancelled)");
});

it("counts missing agent accounting as zero when the agent job failed", async () => {
  const f = evaluate({}, [job("agent", { conclusion: "failure" })]);
  await expect(f.result).resolves.toBe(0);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"failed_before_accounting"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"source":"agent/token_usage.jsonl"'));
});

it("counts legacy pre-harness agent failures as zero from the agent artifact", async () => {
  const f = evaluate({}, [job("agent", { conclusion: "failure" })], {
    agentFiles: {
      "agent-stdio.log": "[ERROR] Fatal error: cloud-hypervisor --version failed\nProcess exiting with code: 1\n",
    },
  });
  await expect(f.result).resolves.toBe(0);
  expect(f.client.downloadArtifact).toHaveBeenCalledTimes(2);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"legacy_pre_harness_failure"'));
});

it("still requires accounting when a legacy agent artifact contains a harness marker", async () => {
  const f = evaluate({}, [job("agent", { conclusion: "failure" })], {
    agentFiles: {
      "agent-stdio.log": "[claude-harness] starting\n[ERROR] Fatal error: agent failed\nProcess exiting with code: 1\n",
    },
  });
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent component");
});

it("counts a legacy successful sample replay with empty accounting as zero", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": "" }, [job("agent")], {
    agentFiles: {
      "agent-stdio.log": '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":1,"driver":"apply_samples"}\n',
    },
  });
  await expect(f.result).resolves.toBe(0);
  expect(f.client.downloadArtifact).toHaveBeenCalledTimes(2);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"legacy_sample_replay"'));
});

it("still requires accounting when an agent job succeeds", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": "" }, [job("agent")]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent component");
});

it("counts a successful pre-flight noop with harness evidence and empty accounting as zero AIC", async () => {
  const evidenceFile = path.join(directory, "agent_execution.json");
  shouldSkipForNoopSafeOutputs({
    safeOutputsPath: "/tmp/safe-outputs.jsonl",
    hasNoopInSafeOutputs: () => true,
    log: () => {},
    executionEvidence: captureExecutionEvidenceConfiguration({
      GH_AW_AWF_EXECUTION_EVIDENCE_FILE: evidenceFile,
      GH_AW_AWF_EXECUTION_COMPONENT: "agent",
      GITHUB_RUN_ID: "1",
      GITHUB_RUN_ATTEMPT: "1",
    }),
  });
  const f = evaluate({ "agent/token_usage.jsonl": "", "agent/execution.json": fs.readFileSync(evidenceFile, "utf8") }, [job("agent")]);
  await expect(f.result).resolves.toBe(0);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"execution_not_started"'));
});

it.each(["success", "failure"])("still requires accounting after model execution starts with job conclusion %s", async conclusion => {
  const f = evaluate({ "agent/execution.json": JSON.stringify({ version: 1, component: "agent", run_id: 1, run_attempt: 1, state: "started" }) }, [job("agent", { conclusion })]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent");
});

it("accepts a completed run with no jobs as zero usage", async () => {
  const f = evaluate({}, []);
  await expect(f.result).resolves.toBe(0);
  expect(f.client.listArtifacts).not.toHaveBeenCalled();
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"aic":0,"reason":"no_billable_jobs"'));
});

it("counts usage for compiler-generated job display names", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":18.02604}', "detection_usage.jsonl": '{"aic":30.08748}' }, [job("Agent"), job("Detection")]);
  await expect(f.result).resolves.toBeCloseTo(48.11352);
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"agent"'));
  expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"component":"detection"'));
});

it("maps the Evaluations display name and job_id to their components", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":1}', "evals/token_usage.jsonl": '{"aic":2}' }, [job("Agent"), job("Evaluations")]);
  await expect(f.result).resolves.toBeCloseTo(3);
  const g = evaluate({ "agent_usage.jsonl": '{"aic":4}' }, [job("Display", { job_id: "agent" })]);
  await expect(g.result).resolves.toBeCloseTo(4);
});

it("prefers the agent job_id over a conflicting billable display name", async () => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":4}' }, [job("Detection", { id: 1, job_id: "agent" })]);
  await expect(f.result).resolves.toBe(4);
});

it.each(["detection", "evals"])("prefers the %s job_id over the Agent display name", async component => {
  const f = evaluate({ "agent/token_usage.jsonl": '{"aic":1}', [`${component}/token_usage.jsonl`]: '{"aic":2}' }, [job("Agent", { id: 1 }), job("Agent", { id: 2, job_id: component })]);
  await expect(f.result).resolves.toBe(3);
});

it("requires accounting for executed jobs with generated display names", async () => {
  const f = evaluate({}, [job("Agent")]);
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent component");
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

it.each(["agent", "detection", "evals"])("accepts provable zero usage when %s execution never started", async component => {
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
    component === "agent" ? [job("agent", { conclusion: "failure" })] : [job("agent", { conclusion: "skipped" }), job(component, { conclusion: "failure" })]
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

it("T-AIC-DG-026 counts verified prior usage when a runner-assigned agent rerun is cancelled without a producer artifact", async () => {
  const f = evaluate(
    { "agent_usage.jsonl": '{"aic":2}' },
    [job("agent"), job("agent", { id: 10, run_attempt: 2, conclusion: "cancelled", runner_id: 123, runner_name: "runner", steps: [{ name: "Set up job" }], started_at: later, completed_at: later })],
    { attempt: 2, runStarted: later, producerTime: time }
  );
  await expect(f.result).resolves.toBe(2);
  expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Usage from the cancelled attempt may be unaccounted for"));
});

it("rejects a cancelled rerun without a verifiable earlier producer", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":2}' }, [job("agent"), job("agent", { id: 10, run_attempt: 2, conclusion: "cancelled", started_at: later, completed_at: later })], {
    attempt: 2,
    runStarted: later,
    producerTime: "2025-01-01T11:00:00Z",
  });
  await expect(f.result).rejects.toThrow("Cannot verify the agent producer artifact");
  expect(global.core.warning).not.toHaveBeenCalled();
});

it("rejects an ambiguous previous attempt rather than choosing arbitrary carried-forward usage", async () => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":2}' }, [job("agent"), job("Agent", { id: 11 }), job("agent", { id: 10, run_attempt: 2, conclusion: "cancelled", started_at: later, completed_at: later })], {
    attempt: 2,
    runStarted: later,
    producerTime: time,
  });
  await expect(f.result).rejects.toThrow("Ambiguous daily AIC component jobs");
});

it.each([
  ["chronological", [job("agent"), job("Agent", { id: 11 }), job("agent", { id: 10, run_attempt: 2, started_at: later, completed_at: later })]],
  ["latest-first", [job("agent", { id: 10, run_attempt: 2, started_at: later, completed_at: later }), job("agent"), job("Agent", { id: 11 })]],
])("ignores ambiguous older jobs when the latest job has its own producer (%s order)", async (_order, jobs) => {
  const f = evaluate({ "agent_usage.jsonl": '{"aic":2}' }, jobs, { attempt: 2, runStarted: later, producerTime: later });
  await expect(f.result).resolves.toBe(2);
});

it("rejects a cancelled rerun when the carried-forward accounting is missing", async () => {
  const f = evaluate({}, [job("agent"), job("agent", { id: 10, run_attempt: 2, conclusion: "cancelled", started_at: later, completed_at: later })], { attempt: 2, runStarted: later, producerTime: time });
  await expect(f.result).rejects.toThrow("Missing accounting for executed agent");
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
