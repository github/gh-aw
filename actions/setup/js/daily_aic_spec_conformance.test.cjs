import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const guardrail = require("./check_daily_aic_workflow_guardrail.cjs");
const ledger = require("./daily_aic_repo_memory_ledger.cjs");
const { apiError } = require("./daily_aic_api_budget.cjs");

const now = Date.parse("2025-02-03T12:00:00Z");
const timestamp = new Date(now - 60_000).toISOString();
const repository = "example/project";
const response = data => ({ status: 200, headers: { "x-ratelimit-remaining": "4000" }, data });
const run = id => ({ id, workflow_id: 7, run_attempt: 1, status: "completed", conclusion: "success", created_at: timestamp, updated_at: timestamp });
let directory;
let outputs;
let github;
let client;
let componentMode;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-spec-conformance-"));
  outputs = {};
  componentMode = "raw";
  for (const [key, value] of Object.entries({
    GH_AW_MAX_DAILY_AI_CREDITS: "9",
    GH_AW_MAX_AI_CREDITS: "8",
    GH_AW_MAX_DAILY_AI_CREDITS_BACKEND: "artifacts",
    GH_AW_GITHUB_TOKEN: "synthetic",
    GITHUB_TOKEN: "",
    GH_TOKEN: "",
    GITHUB_EVENT_NAME: "schedule",
    GH_AW_HAS_SLASH_COMMAND: "false",
    GH_AW_HAS_LABEL_COMMAND: "false",
    GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT: "",
    GH_AW_WORKFLOW_ID: "7",
    GH_AW_DAILY_AIC_REPO_MEMORY_TRUSTED: "false",
    GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING: "false",
    GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC: "false",
  })) {
    vi.stubEnv(key, value);
  }
  vi.stubGlobal("core", {
    setOutput: (key, value) => {
      outputs[key] = value;
    },
    setFailed: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
    summary: { addDetails: vi.fn(), write: vi.fn() },
  });
  github = {
    rest: {
      actions: {
        getWorkflowRun: vi.fn(async () => response({ workflow_id: 7 })),
        listWorkflowRuns: vi.fn(async () => response({ workflow_runs: [run(1)] })),
        listWorkflowRunsForRepo: vi.fn(async () => response({ workflow_runs: [run(1)] })),
        listJobsForWorkflowRun: vi.fn(async () =>
          response({
            jobs: ["Agent", "Detection", "Evaluations"].map((name, index) => ({
              id: index + 1,
              name,
              run_attempt: 1,
              status: "completed",
              conclusion: componentMode === "skipped" ? "skipped" : "success",
              started_at: new Date(now - 62_000).toISOString(),
              completed_at: new Date(now - 61_000).toISOString(),
            })),
          })
        ),
      },
    },
  };
  vi.stubGlobal("github", github);
  vi.stubGlobal("context", { repo: { owner: "example", repo: "project" }, runId: 99 });
  client = {
    listArtifacts: vi.fn(async options => ({
      artifacts: [{ id: options.findBy.workflowRunId, name: "usage", createdAt: new Date(timestamp) }],
    })),
    downloadArtifact: vi.fn(async (id, options) => {
      for (const component of ["agent", "detection", "evals"]) {
        if (component === "evals" && componentMode === "failed_decisions") {
          const root = path.join(options.path, component);
          fs.mkdirSync(root, { recursive: true });
          fs.writeFileSync(
            path.join(root, "evals_status.json"),
            JSON.stringify({
              status: "not_evaluated",
              reason_code: "judge_failed",
              reason: "The Decisions evals request finished with outcome failure; no answers were recorded.",
              engine: "decisions",
              run_id: String(id),
              run_attempt: "1",
              execution_outcome: "failure",
              execution_state: "started",
            })
          );
          continue;
        }
        if (component === "agent" && (componentMode === "missing" || (componentMode === "mixed" && id === 2))) continue;
        const root = path.join(options.path, component);
        fs.mkdirSync(root, { recursive: true });
        const value = componentMode === "zero" ? 0 : component === "agent" ? id : 1;
        fs.writeFileSync(path.join(root, "token_usage.jsonl"), componentMode === "empty_detection" && component === "detection" ? "" : JSON.stringify({ ai_credits_this_response: value }));
      }
      if (componentMode === "overlapping") fs.writeFileSync(path.join(options.path, "agent_usage.jsonl"), JSON.stringify({ aic: 99 }));
      if (componentMode === "empty_detection") fs.writeFileSync(path.join(options.path, "detection_usage.jsonl"), JSON.stringify({ aic: 99 }));
      return { downloadPath: options.path };
    }),
  };
  vi.spyOn(guardrail, "getArtifactClient").mockResolvedValue(client);
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.useRealTimers();
  fs.rmSync(directory, { recursive: true, force: true });
});

async function check() {
  const pending = guardrail.main({ cachePath: path.join(directory, "scan.jsonl"), repoMemoryDir: directory });
  await vi.runAllTimersAsync();
  await pending;
}

function writeLedgerCoverage(start = now - ledger.WINDOW_MS, overrides = {}) {
  fs.mkdirSync(ledger.ledgerRoot(directory), { recursive: true });
  fs.writeFileSync(ledger.ledgerCoveragePath(directory, repository, "7"), JSON.stringify({ version: 1, repository, workflow_id: "7", initialized_at: new Date(start).toISOString(), ...overrides }));
}

function expectDecision(status, total, estimated = 0, threshold = 9) {
  expect(outputs).toMatchObject({
    daily_ai_credits_guardrail_status: status,
    daily_ai_credits_total: String(total),
    daily_ai_credits_estimated: String(estimated),
    daily_ai_credits_threshold: String(threshold),
    daily_ai_credits_exceeded: status === "exceeded" ? "true" : "false",
    daily_ai_credits_guardrail_error: "",
  });
  expect(global.core.setFailed).not.toHaveBeenCalled();
}

describe("daily AIC specification: basic conformance", () => {
  it.each([
    ["T-AIC-DG-024 below threshold", "9", "under_budget"],
    ["T-AIC-DG-024 equality", "7", "exceeded"],
    ["T-AIC-DG-024 above threshold", "6", "exceeded"],
  ])("%s", async (_id, limit, status) => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS", limit);
    github.rest.actions.listWorkflowRuns.mockResolvedValue(response({ workflow_runs: [run(1), run(2)] }));
    await check();
    expectDecision(status, 7, 0, Number(limit));
  });

  it("T-AIC-DG-017 deduplicates and excludes the current and old runs", async () => {
    github.rest.actions.listWorkflowRuns.mockResolvedValue(
      response({
        workflow_runs: [run(1), run(1), run(99), { ...run(2), created_at: new Date(now - 25 * 60 * 60_000).toISOString() }],
      })
    );
    await check();
    expectDecision("under_budget", 3);
    expect(client.listArtifacts).toHaveBeenCalledOnce();
  });

  it.each(["malformed", "incomplete"])("T-AIC-DG-017 fails closed on %s history", async kind => {
    const workflow_runs = kind === "malformed" ? undefined : Array.from({ length: 100 }, () => run(1));
    github.rest.actions.listWorkflowRuns.mockResolvedValue(response({ workflow_runs }));
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: "transient_error", daily_ai_credits_total: "", daily_ai_credits_estimated: "", daily_ai_credits_exceeded: "false" });
    expect(global.core.setFailed).toHaveBeenCalledOnce();
    expect(client.listArtifacts).not.toHaveBeenCalled();
    expect(github.rest.actions.listWorkflowRuns).toHaveBeenCalledTimes(kind === "malformed" ? 1 : 25);
  });

  it.each([
    ["T-AIC-DG-019 raw source wins over summary", "overlapping", 3],
    ["T-AIC-DG-020 authoritative zero records", "zero", 0],
    ["T-AIC-DG-020 skipped billable jobs", "skipped", 0],
    ["T-AIC-DG-020 empty Detection wins over summary", "empty_detection", 2],
    ["T-AIC-DG-021 missing Agent replaces the whole run", "missing", 8],
  ])("%s", async (_id, mode, total) => {
    componentMode = mode;
    await check();
    expectDecision("under_budget", total, mode === "missing" ? 8 : 0);
    if (mode === "skipped") expect(client.listArtifacts).not.toHaveBeenCalled();
  });

  it("T-AIC-DG-021/024 reports mixed recorded and whole-run estimated usage", async () => {
    componentMode = "mixed";
    github.rest.actions.listWorkflowRuns.mockResolvedValue(response({ workflow_runs: [run(1), run(2)] }));
    await check();
    expectDecision("exceeded", 11, 8);
  });

  it("T-AIC-DG-027 replaces whole-run estimates after a successful wrapper job reports a failed Decisions judge", async () => {
    componentMode = "failed_decisions";
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS", "5000");
    vi.stubEnv("GH_AW_MAX_AI_CREDITS", "1000");
    const runs = [run(1), run(2), run(3), run(4), run(5)];
    github.rest.actions.listWorkflowRuns.mockResolvedValue(response({ workflow_runs: runs }));
    fs.writeFileSync(
      path.join(directory, "scan.jsonl"),
      runs
        .map(item =>
          JSON.stringify({
            version: 2,
            coverage_version: 3,
            repository,
            workflow_id: 7,
            run_id: item.id,
            run_attempt: 1,
            created_at: timestamp,
            updated_at: timestamp,
            observed_at: new Date(now).toISOString(),
            source: "estimated",
            aic: 1000,
          })
        )
        .join("\n")
    );

    await check();

    expectDecision("under_budget", 20, 0, 5000);
    expect(client.listArtifacts).toHaveBeenCalledTimes(5);
    const observations = fs.readFileSync(path.join(directory, "scan.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(observations.map(entry => entry.source)).toEqual(Array(5).fill("recorded"));
    expect(observations.map(entry => entry.aic)).toEqual([2, 3, 4, 5, 6]);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("provider billing remains unverified"));
  });

  it("T-AIC-DG-021 fails closed when unresolved accounting has no positive maximum", async () => {
    componentMode = "missing";
    vi.stubEnv("GH_AW_MAX_AI_CREDITS", "0");
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: "transient_error", daily_ai_credits_total: "", daily_ai_credits_exceeded: "false" });
    expect(global.core.setFailed).toHaveBeenCalledOnce();
  });

  it.each([
    ["recorded", 1, 3, 11, 0],
    ["estimated", 1, 3, 3, 1],
    ["recorded", 2, 3, 3, 1],
    ["recorded", 1, 2, 3, 1],
    [undefined, 1, 1, 3, 1],
  ])("T-AIC-DG-018 cache source=%s attempt=%i coverage=%i", async (source, attempt, coverage, total, inspections) => {
    fs.writeFileSync(
      path.join(directory, "scan.jsonl"),
      JSON.stringify({
        version: 2,
        coverage_version: coverage,
        repository,
        workflow_id: 7,
        run_id: 1,
        run_attempt: attempt,
        created_at: timestamp,
        updated_at: timestamp,
        observed_at: new Date(now).toISOString(),
        source,
        aic: 11,
      })
    );
    await check();
    expectDecision(total === 11 ? "exceeded" : "under_budget", total);
    expect(github.rest.actions.listJobsForWorkflowRun).toHaveBeenCalledTimes(inspections);
    expect(client.listArtifacts).toHaveBeenCalledTimes(inspections);
  });

  it.each([
    [401, {}, "structural_error", 1],
    [403, {}, "structural_error", 1],
    [429, {}, "transient_error", 1],
    [503, { "retry-after": "120" }, "transient_error", 1],
    [503, {}, "transient_error", 3],
  ])("T-AIC-DG-022 authoritative HTTP %i with headers %j", async (status, headers, expected, attempts) => {
    github.rest.actions.getWorkflowRun.mockRejectedValue(apiError(status, headers, "read failed"));
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: expected, daily_ai_credits_total: "", daily_ai_credits_estimated: "", daily_ai_credits_exceeded: "false" });
    // A quota-blocked stop skips the agent via the job condition instead of failing activation.
    if (expected === "quota_blocked") expect(global.core.setFailed).not.toHaveBeenCalled();
    else expect(global.core.setFailed).toHaveBeenCalledOnce();
    expect(github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(attempts);
    expect(client.listArtifacts).not.toHaveBeenCalled();
  });

  it("T-AIC-DG-022 quota exhaustion keeps accounting unknown for enforced startup gating", async () => {
    github.rest.actions.getWorkflowRun.mockRejectedValue(apiError(403, { "x-ratelimit-remaining": "0" }, "rate limit"));
    await check();
    expect(outputs).toMatchObject({
      daily_ai_credits_guardrail_status: "quota_blocked",
      daily_ai_credits_total: "",
      daily_ai_credits_estimated: "",
      daily_ai_credits_exceeded: "false",
    });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(github.rest.actions.getWorkflowRun).toHaveBeenCalledOnce();
    expect(client.listArtifacts).not.toHaveBeenCalled();
  });

  it("T-AIC-DG-022 recovers an intermittent current-run read without estimating", async () => {
    github.rest.actions.getWorkflowRun.mockRejectedValueOnce(Object.assign(new Error("offline"), { code: "ECONNRESET" }));
    await check();
    expectDecision("under_budget", 3);
    expect(github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(2);
  });

  it("T-AIC-DG-023 filters repository history after workflow-history 404", async () => {
    github.rest.actions.listWorkflowRuns.mockRejectedValue(apiError(404, {}, "Not Found"));
    github.rest.actions.listWorkflowRunsForRepo.mockResolvedValue(response({ workflow_runs: [run(1), { ...run(2), workflow_id: 8 }] }));
    await check();
    expectDecision("under_budget", 3);
    expect(github.rest.actions.listWorkflowRunsForRepo).toHaveBeenCalledOnce();
  });

  it.each([
    ["T-AIC-DG-008", "workflow_call"],
    ["T-AIC-DG-009", "repository_dispatch"],
    ["T-AIC-DG-010", "workflow_dispatch"],
  ])("%s bypasses %s without reads", async (_id, event) => {
    vi.stubEnv("GITHUB_EVENT_NAME", event);
    await check();
    expect(outputs.daily_ai_credits_guardrail_status).toBe("skipped");
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
    expect(client.listArtifacts).not.toHaveBeenCalled();
  });

  it("Section 9.14 disabled limits do not read accounting", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS", "-1");
    await check();
    expect(outputs.daily_ai_credits_guardrail_status).toBe("disabled");
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
    expect(guardrail.getArtifactClient).not.toHaveBeenCalled();
  });

  it("Section 9.14 missing credentials fail before API reads", async () => {
    vi.stubEnv("GH_AW_GITHUB_TOKEN", "");
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: "structural_error", daily_ai_credits_total: "", daily_ai_credits_exceeded: "false" });
    expect(global.core.setFailed).toHaveBeenCalledOnce();
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
    expect(guardrail.getArtifactClient).not.toHaveBeenCalled();
  });

  it.each([
    ["false", "structural_error", ""],
    ["true", "under_budget", "0"],
  ])("T-AIC-DG-025 empty ledger trusted=%s", async (trusted, status, total) => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_TRUSTED", trusted);
    vi.stubEnv("GH_AW_GITHUB_TOKEN", "");
    writeLedgerCoverage();
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: status, daily_ai_credits_total: total });
    expect(global.core.setFailed).toHaveBeenCalledTimes(trusted === "true" ? 0 : 1);
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it("T-AIC-DG-025 filters ledger timestamps and selects the latest duplicate", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_TRUSTED", "true");
    const entry = (id, aic, time) => JSON.stringify({ version: 1, repository, workflow_id: "7", run_id: id, aic, timestamp: new Date(time).toISOString() });
    const root = path.join(directory, "daily-aic-ledger");
    writeLedgerCoverage();
    fs.writeFileSync(
      path.join(root, "2025-02-03.jsonl"),
      ["malformed", entry(1, 1, now - 120_000), entry(1, 2, now - 60_000), entry(1, 3, now - 60_000), entry(2, 99, now - 25 * 60 * 60_000), entry(3, 99, now + 60_000), entry(4, 4, now)].join("\n")
    );
    fs.writeFileSync(path.join(root, "2025-02-02.jsonl"), entry(5, 1, now - 24 * 60 * 60_000));
    await check();
    expectDecision("under_budget", 8);
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it.each(["missing", "legacy", "warming", "future", "wrong_identity", "malformed"])("bootstraps a %s ledger from authoritative history instead of assuming zero", async state => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_TRUSTED", state === "missing" ? "false" : "true");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING", state === "missing" ? "true" : "false");
    if (state === "warming") writeLedgerCoverage(now - ledger.WINDOW_MS + 1);
    if (state === "future") writeLedgerCoverage(now + 1);
    if (state === "wrong_identity") writeLedgerCoverage(undefined, { workflow_id: "other" });
    if (state === "malformed") {
      writeLedgerCoverage();
      fs.writeFileSync(ledger.ledgerCoveragePath(directory, repository, "7"), "{");
    }
    await check();
    expectDecision("under_budget", 3);
    expect(github.rest.actions.listWorkflowRuns).toHaveBeenCalledOnce();
    expect(client.downloadArtifact).toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Bootstrapping repo-memory AIC with authoritative artifact accounting"));
  });

  it("does not trust coverage or usage from an unclassified clone", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    writeLedgerCoverage();
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: "structural_error", daily_ai_credits_total: "", daily_ai_credits_exceeded: "false" });
    expect(global.core.setFailed).toHaveBeenCalledOnce();
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it("preserves the preexisting explicit insecure override without requiring coverage metadata", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_TRUSTED", "true");
    vi.stubEnv("GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC", "true");
    vi.stubEnv("GH_AW_GITHUB_TOKEN", "");
    const read = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("Permission denied"), { code: "EACCES" });
    });
    await check();
    expectDecision("under_budget", 0);
    expect(read).not.toHaveBeenCalled();
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it("does not read an untrusted orphan ledger even when it contains mature coverage", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING", "true");
    writeLedgerCoverage();
    fs.writeFileSync(path.join(ledger.ledgerRoot(directory), "2025-02-03.jsonl"), JSON.stringify({ version: 1, repository, workflow_id: "7", run_id: 1, aic: 0, timestamp }));
    await check();
    expectDecision("under_budget", 3);
    expect(github.rest.actions.listWorkflowRuns).toHaveBeenCalledOnce();
  });

  it("keeps the daily limit enforced during bootstrap", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING", "true");
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS", "3");
    await check();
    expectDecision("exceeded", 3, 0, 3);
    expect(outputs.daily_ai_credits_exceeded).toBe("true");
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("bootstraps the observed missing-branch failure when historical AI jobs were skipped", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING", "true");
    componentMode = "skipped";
    await check();
    expectDecision("under_budget", 0);
    expect(github.rest.actions.listJobsForWorkflowRun).toHaveBeenCalled();
    expect(client.downloadArtifact).not.toHaveBeenCalled();
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("requires an artifact token during bootstrap instead of trusting an empty branch", async () => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING", "true");
    vi.stubEnv("GH_AW_GITHUB_TOKEN", "");
    await check();
    expect(outputs).toMatchObject({ daily_ai_credits_guardrail_status: "structural_error", daily_ai_credits_total: "" });
    expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("no artifact lookup token"));
    expect(github.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
  });

  it.each([403, 429])("fails bootstrap closed on HTTP %i rather than using the ledger or per-run estimate", async status => {
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", "repo-memory");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_BRANCH_MISSING", "true");
    github.rest.actions.listWorkflowRuns.mockRejectedValue(apiError(status, {}, "denied"));
    await check();
    expect(outputs.daily_ai_credits_total).toBe("");
    expect(outputs.daily_ai_credits_exceeded).toBe("false");
    expect(global.core.setFailed).toHaveBeenCalledOnce();
    expect(client.downloadArtifact).not.toHaveBeenCalled();
  });
});
