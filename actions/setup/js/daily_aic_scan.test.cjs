import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const { scanDailyAIC } = require("./daily_aic_scan.cjs");
const { createAPIBudget, apiError, retryNotBefore, safeResponseHeaders } = require("./daily_aic_api_budget.cjs");
const { readScanCache, scanCacheEntry, AIC_SCAN_CACHE_FILE_PATH } = require("./daily_aic_cache_helpers.cjs");
const { mainWithPaths: restore, isTrustedProducer } = require("./restore_aic_scan_cache.cjs");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const guardrail = require("./check_daily_aic_workflow_guardrail.cjs");
const ledger = require("./daily_aic_repo_memory_ledger.cjs");

const now = Date.parse("2025-02-03T12:00:00Z");
const time = new Date(now - 60000).toISOString();
const repository = "example/project";
const run = (id, overrides = {}) => ({
  id,
  workflow_id: 7,
  run_attempt: 1,
  status: "completed",
  conclusion: "success",
  created_at: time,
  updated_at: time,
  run_started_at: time,
  ...overrides,
});
const response = data => ({ status: 200, headers: { "x-ratelimit-remaining": "4000" }, data });
let directory;
let cachePath;

beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-scan-test-"));
  cachePath = path.join(directory, "scan.jsonl");
  global.core = { info: vi.fn(), warning: vi.fn(), exportVariable: vi.fn(), setOutput: vi.fn() };
  vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  delete global.core;
  delete global.github;
  delete global.context;
  fs.rmSync(directory, { recursive: true, force: true });
});

function fixture(runs = [run(1), run(2), run(3)]) {
  const pageRead = vi.fn(async () => ({ response: response({ workflow_runs: runs }), sourceRunCount: runs.length, lookupMode: "workflow_id" }));
  const getRunAIC = vi.fn(async (_client, id) => (id === 2 ? 0 : id * 2));
  const github = {
    rest: {
      actions: {
        getWorkflowRun: vi.fn(async () => response({ workflow_id: 7 })),
        listJobsForWorkflowRun: vi.fn(async () =>
          response({
            jobs: [
              {
                id: 1,
                name: "agent",
                run_attempt: 1,
                status: "completed",
                conclusion: "success",
                started_at: time,
                completed_at: time,
              },
            ],
          })
        ),
      },
    },
  };
  return {
    github,
    context: { repo: { owner: "example", repo: "project" }, runId: 99 },
    budget: createAPIBudget(),
    artifactClient: {},
    getRunAIC,
    pageRead,
    listPage: vi.fn(async (client, params, budget) => {
      let result;
      await budget.request(async () => {
        result = await pageRead(client, params);
        return result.response;
      });
      return result;
    }),
    token: "synthetic",
    fallbackAIC: 1000,
    cachePath,
    now,
  };
}

function writeEntries(entries) {
  fs.writeFileSync(cachePath, entries.map(entry => JSON.stringify(entry)).join("\n"));
}

describe("complete daily AIC phase recovery", () => {
  function recoveryClock() {
    let current = now;
    vi.spyOn(Date, "now").mockImplementation(() => current);
    return {
      now: () => current,
      runAttempt: "1",
      sleep: vi.fn(async delay => {
        current += delay;
      }),
    };
  }

  function quotaFailure() {
    return apiError(403, { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(now / 1000 + 60) }, "API rate limit exceeded");
  }

  it("recovers first-read exhaustion before restoring or doing any accounting", async () => {
    const recovery = recoveryClock();
    const f = fixture([run(1)]);
    f.github.rest.actions.getWorkflowRun.mockRejectedValueOnce(quotaFailure());
    const listWorkflowRuns = vi.fn(async () => response({ workflow_runs: [] }));
    f.github.rest.actions.listWorkflowRuns = listWorkflowRuns;
    global.github = f.github;
    global.context = { ...f.context, payload: { repository: { default_branch: "main" } } };
    await restore(cachePath, { recovery });
    expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(61000);
    expect(f.github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(2);
    expect(listWorkflowRuns).toHaveBeenCalledOnce();
    expect(f.getRunAIC).not.toHaveBeenCalled();
    const result = await scanDailyAIC({ ...f, recovery });
    expect(result.countedRuns).toMatchObject([{ id: 1, aic: 2 }]);
    expect(f.github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(3);
    expect(f.listPage).toHaveBeenCalledOnce();
  });

  it("restarts the authoritative listing and counts new runs after partial-scan exhaustion", async () => {
    const recovery = recoveryClock();
    const f = fixture([run(1), run(2)]);
    delete f.now;
    f.pageRead.mockResolvedValueOnce({ response: response({ workflow_runs: [run(1), run(2)] }), sourceRunCount: 2, lookupMode: "workflow_id" }).mockResolvedValue({
      response: response({ workflow_runs: [run(1), run(2), run(3)] }),
      sourceRunCount: 3,
      lookupMode: "workflow_id",
    });
    f.getRunAIC.mockResolvedValueOnce(2).mockRejectedValueOnce(quotaFailure()).mockResolvedValueOnce(4).mockResolvedValueOnce(6);
    const result = await scanDailyAIC({ ...f, recovery });
    expect(recovery.sleep).toHaveBeenCalledOnce();
    expect(f.github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(2);
    expect(f.listPage).toHaveBeenCalledTimes(2);
    expect(f.listPage.mock.calls[1][1].created).toBe(`>=${new Date(now + 61000 - 86400000).toISOString()}`);
    expect(result.countedRuns.map(({ id, aic }) => [id, aic])).toEqual([
      [1, 2],
      [2, 4],
      [3, 6],
    ]);
    expect(result.cacheHits).toBe(1);
    expect(f.getRunAIC.mock.calls.map(([, id]) => id)).toEqual([1, 2, 2, 3]);
  });

  it("recovers the authoritative scan's first read before counting its entire fresh window", async () => {
    const recovery = recoveryClock();
    const f = fixture([run(1), run(2), run(3)]);
    f.github.rest.actions.getWorkflowRun.mockRejectedValueOnce(quotaFailure());
    const result = await scanDailyAIC({ ...f, recovery });
    expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(61000);
    expect(f.github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(2);
    expect(f.listPage).toHaveBeenCalledOnce();
    expect(f.getRunAIC).toHaveBeenCalledTimes(3);
    expect(result.countedRuns.map(({ id }) => id)).toEqual([1, 2, 3]);
  });

  it("does not reuse a recorded observation if the run attempt changes during deferral", async () => {
    const recovery = recoveryClock();
    const f = fixture([run(1), run(2)]);
    f.pageRead.mockResolvedValueOnce({ response: response({ workflow_runs: [run(1), run(2)] }), sourceRunCount: 2, lookupMode: "workflow_id" }).mockResolvedValue({
      response: response({ workflow_runs: [run(1, { run_attempt: 2 }), run(2)] }),
      sourceRunCount: 2,
      lookupMode: "workflow_id",
    });
    f.getRunAIC.mockResolvedValueOnce(2).mockRejectedValueOnce(quotaFailure()).mockResolvedValueOnce(20).mockResolvedValueOnce(4);
    const result = await scanDailyAIC({ ...f, recovery });
    expect(result.countedRuns.map(({ id, aic }) => [id, aic])).toEqual([
      [1, 20],
      [2, 4],
    ]);
    expect(result.cacheHits).toBe(0);
    expect(f.getRunAIC.mock.calls.map(([, id]) => id)).toEqual([1, 2, 1, 2]);
  });

  it("fails closed on renewed first-read exhaustion without listing history", async () => {
    const recovery = recoveryClock();
    const f = fixture([]);
    f.github.rest.actions.getWorkflowRun.mockRejectedValue(quotaFailure());
    await expect(scanDailyAIC({ ...f, recovery })).rejects.toMatchObject({ code: "DAILY_AIC_QUOTA_STOP" });
    expect(recovery.sleep).toHaveBeenCalledOnce();
    expect(f.github.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(2);
    expect(f.listPage).not.toHaveBeenCalled();
    expect(f.getRunAIC).not.toHaveBeenCalled();
  });

  it.each([
    ["reset beyond the ten-minute budget", { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(now / 1000 + 900) }, "1", 0],
    ["rerun ineligible for recovery", { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(now / 1000 + 60) }, "2", 0],
    ["unknown reset", { "x-ratelimit-remaining": "0" }, "1", 0],
    ["renewed exhaustion", { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(now / 1000 + 60) }, "1", 1],
  ])("publishes an explicit quota-blocked restore outcome for %s without authorizing accounting", async (_case, headers, runAttempt, sleeps) => {
    const recovery = { ...recoveryClock(), runAttempt };
    const getWorkflowRun = vi.fn().mockRejectedValue(apiError(403, { ...headers, "x-ratelimit-limit": "15000", "x-ratelimit-used": "15000", "x-ratelimit-resource": "core" }, "API rate limit exceeded"));
    const listWorkflowRuns = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun, listWorkflowRuns } } };
    const summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    global.core.summary = summary;
    await expect(restore(cachePath, { recovery })).resolves.toBeUndefined();
    expect(recovery.sleep).toHaveBeenCalledTimes(sleeps);
    expect(getWorkflowRun).toHaveBeenCalledTimes(sleeps + 1);
    expect(listWorkflowRuns).not.toHaveBeenCalled();
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("### GitHub API quota exhausted"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Accounting remains unverified; agent startup is blocked"));
    expect(JSON.parse(global.core.exportVariable.mock.calls[0][1])).toMatchObject({ resource: "core", limit: 15000, remaining: 0, used: 15000, operation: "current_run" });
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("bounds snapshot misses on normal workflow history before a fresh full scan", async () => {
    recoveryClock();
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, event: "schedule", head_branch: "main", repository: { full_name: repository }, head_repository: { full_name: repository } };
    const listWorkflowRunArtifacts = vi.fn(async () => response({ artifacts: [] }));
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: vi.fn(async () => ({ token: "synthetic" })),
      rest: {
        actions: {
          getWorkflowRun: vi.fn(async () => response(current)),
          listWorkflowRuns: vi.fn(async ({ per_page }) => response({ workflow_runs: Array.from({ length: 10 }, (_, id) => ({ ...producer, id })).slice(0, per_page) })),
          listWorkflowRunArtifacts,
        },
      },
    };
    await restore(cachePath);
    expect(global.github.rest.actions.listWorkflowRuns).toHaveBeenCalledOnce();
    expect(global.github.rest.actions.listWorkflowRuns).toHaveBeenCalledWith(expect.objectContaining({ per_page: 10 }));
    expect(listWorkflowRunArtifacts).toHaveBeenCalledTimes(2);
    expect(listWorkflowRunArtifacts.mock.calls.map(([params]) => params.run_id)).toEqual([0, 1]);
    expect(fs.existsSync(cachePath)).toBe(false);
    const f = fixture([run(1), run(2), run(3)]);
    const result = await scanDailyAIC(f);
    expect(result.countedRuns).toHaveLength(3);
    expect(result.cacheHits).toBe(0);
    expect(f.getRunAIC).toHaveBeenCalledTimes(3);
    expect(f.listPage).toHaveBeenCalledOnce();
  });
});

describe("daily AIC confirmed-zero diagnostics", () => {
  const notStarted = {
    id: 1,
    name: "Agent",
    run_attempt: 1,
    status: "completed",
    conclusion: "cancelled",
    runner_id: null,
    runner_name: null,
    steps: [],
  };

  it.each(["skipped", "cancelled", "failure"])("confirms zero before artifact lookup when the agent is %s and never ran", async conclusion => {
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    f.artifactClient = { listArtifacts: vi.fn().mockRejectedValue(new Error("artifact unavailable")) };
    f.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue(response({ jobs: [{ ...notStarted, conclusion }] }));
    const result = await scanDailyAIC(f);
    expect(result.countedRuns).toMatchObject([{ aic: 0, source: "recorded", accountingReason: "confirmed_zero" }]);
    expect(f.artifactClient.listArtifacts).not.toHaveBeenCalled();
    f.github.rest.actions.listJobsForWorkflowRun.mockClear();
    expect((await scanDailyAIC(f)).countedRuns).toMatchObject([{ aic: 0, accountingReason: "confirmed_zero" }]);
    expect(f.github.rest.actions.listJobsForWorkflowRun).not.toHaveBeenCalled();
  });

  it.each(
    [
      [{ ...notStarted, runner_id: 1 }],
      [{ ...notStarted, steps: undefined }],
      [{ ...notStarted }, { ...notStarted, id: 2, name: "Detection", conclusion: "success", runner_id: 1 }],
      [
        { ...notStarted, conclusion: "success", runner_id: 1 },
        { ...notStarted, id: 2, run_attempt: 2 },
      ],
    ].map(jobs => [jobs])
  )("does not infer zero when any component may have executed", async jobs => {
    const f = fixture([run(1, { run_attempt: 2 })]);
    f.getRunAIC = guardrail.getRunAIC;
    f.artifactClient = { listArtifacts: vi.fn().mockResolvedValue({ artifacts: [] }) };
    f.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue(response({ jobs }));
    const result = await scanDailyAIC(f);
    expect(result.countedRuns).toMatchObject([{ aic: 1000, source: "estimated" }]);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("check artifact upload and retention"));
  });

  it("retains conservative estimates when job metadata is unavailable", async () => {
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    f.github.rest.actions.listJobsForWorkflowRun.mockRejectedValue(new Error("Job metadata unavailable"));
    expect((await scanDailyAIC(f)).countedRuns).toMatchObject([{ aic: 1000, source: "estimated" }]);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Job metadata unavailable"));
  });

  it("rechecks recorded zero observations made before the corrected rerun coverage rules", async () => {
    const f = fixture([run(1)]);
    const legacy = { ...scanCacheEntry(run(1), 0, repository, 7, now), coverage_version: 2 };
    writeEntries([legacy]);
    const result = await scanDailyAIC(f);
    expect(result.cacheHits).toBe(0);
    expect(result.countedRuns).toMatchObject([{ aic: 2, source: "recorded" }]);
    expect(f.getRunAIC).toHaveBeenCalledOnce();
  });

  it("reports artifact discovery failures without replacing the underlying API error", async () => {
    const f = fixture([run(1)]);
    const error = apiError(403, {}, "Resource not accessible by integration");
    const artifactClient = { listArtifacts: vi.fn().mockRejectedValue(error) };
    await expect(guardrail.getRunAIC(artifactClient, 1, f.token, "example", "project", run(1), f)).rejects.toBe(error);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Artifact discovery failed"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Resource not accessible by integration"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Actions read permissions"));
  });

  it("reports download failures with the run and artifact identities", async () => {
    const f = fixture([run(1)]);
    const error = apiError(410, {}, "Artifact has expired");
    const artifactClient = {
      listArtifacts: vi.fn().mockResolvedValue({ artifacts: [{ id: 42, name: "usage", createdAt: new Date(time) }] }),
      downloadArtifact: vi.fn().mockRejectedValue(error),
    };
    await expect(guardrail.getRunAIC(artifactClient, 1, f.token, "example", "project", run(1), f)).rejects.toBe(error);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"artifactId":42'));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Artifact has expired"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Artifact accounting failed"));
  });
});

describe("daily AIC computation contract gaps", () => {
  const params = { owner: "example", repo: "project", workflowId: 7, created: ">=2025-02-02T12:00:00Z", page: 1, perPage: 100, lookupMode: "workflow_id" };

  it.each([{ "x-ratelimit-remaining": "100" }, { "retry-after": "120" }])("never bypasses a restricted history 404", async headers => {
    const f = fixture([]);
    f.github.rest.actions.listWorkflowRuns = vi.fn().mockRejectedValue(apiError(404, headers, "Not Found"));
    f.github.rest.actions.listWorkflowRunsForRepo = vi.fn(async () => response({ workflow_runs: [] }));
    await expect(guardrail.listCompletedWorkflowRunsPage(f.github, params, f.budget)).rejects.toMatchObject({ status: 404 });
    expect(f.github.rest.actions.listWorkflowRunsForRepo).not.toHaveBeenCalled();
    expect(f.budget.snapshot().requests).toBe(1);
  });

  it("retries repository history without replaying the workflow 404", async () => {
    vi.useFakeTimers();
    const f = fixture([]);
    f.github.rest.actions.listWorkflowRuns = vi.fn().mockRejectedValue(apiError(404, {}, "Not Found"));
    f.github.rest.actions.listWorkflowRunsForRepo = vi
      .fn()
      .mockRejectedValueOnce(apiError(502, {}, "Server Error"))
      .mockResolvedValue(response({ workflow_runs: [run(1)] }));
    const pending = expect(guardrail.listCompletedWorkflowRunsPage(f.github, params, f.budget)).resolves.toMatchObject({ sourceRunCount: 1, lookupMode: "repo_workflow_id_fallback" });
    await vi.runAllTimersAsync();
    await pending;
    expect(f.github.rest.actions.listWorkflowRuns).toHaveBeenCalledOnce();
    expect(f.github.rest.actions.listWorkflowRunsForRepo).toHaveBeenCalledTimes(2);
    expect(f.budget.snapshot().requests).toBe(3);
  });

  it("retries artifact JSON body timeouts with fresh signals and quota observations", async () => {
    vi.useFakeTimers();
    const first = new Response("", { status: 200, headers: { "x-ratelimit-remaining": "4000" } });
    first.json = async () => {
      throw new DOMException("body timeout", "TimeoutError");
    };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(first)
      .mockResolvedValue(new Response(JSON.stringify({ artifacts: [] }), { status: 200, headers: { "x-ratelimit-remaining": "3999" } }));
    vi.stubGlobal("fetch", fetch);
    const budget = createAPIBudget();
    const client = new DefaultArtifactClient({ request: budget.request });
    const pending = expect(client.listArtifacts({ findBy: { token: "synthetic", repositoryOwner: "example", repositoryName: "project", workflowRunId: 1 } })).resolves.toMatchObject({ artifacts: [] });
    await vi.runAllTimersAsync();
    await pending;
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(budget.snapshot().requests).toBe(2);
    expect(fetch.mock.calls[0][1].signal).not.toBe(fetch.mock.calls[1][1].signal);
  });

  it.each([false, true])("retries signed-storage body interruptions (exhausted=%s) without replaying the redirect or publishing partial data", async exhausted => {
    const interrupted = () =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(new TextEncoder().encode("partial"));
            controller.error(Object.assign(new Error("connection reset"), { code: "ECONNRESET" }));
          },
        }),
        { status: 200 }
      );
    let bodies = 0;
    const fetch = vi.fn(async url => {
      if (String(url).includes("api.github.com")) return new Response(null, { status: 302, headers: { location: "https://storage.example.invalid/file", "x-ratelimit-remaining": "4000" } });
      bodies++;
      return exhausted || bodies === 1 ? interrupted() : new Response("complete", { status: 200, headers: { "content-disposition": 'attachment; filename="accounting.jsonl"' } });
    });
    vi.stubGlobal("fetch", fetch);
    const budget = createAPIBudget();
    const client = new DefaultArtifactClient({ request: budget.request });
    const download = client.downloadArtifact(1, { path: directory, findBy: { token: "synthetic", repositoryOwner: "example", repositoryName: "project" } });
    const assertion = exhausted ? expect(download).rejects.toThrow("connection reset") : expect(download).resolves.toMatchObject({ downloadPath: directory });
    await assertion;
    expect(bodies).toBe(exhausted ? 3 : 2);
    expect(budget.snapshot().requests).toBe(1);
    expect(fetch.mock.calls.filter(([url]) => String(url).includes("api.github.com"))).toHaveLength(1);
    expect(fetch.mock.calls.filter(([url]) => String(url).includes("storage")).every(([_url, options]) => !options.headers)).toBe(true);
    if (exhausted) expect(fs.readdirSync(directory)).toEqual([]);
    else expect(fs.readFileSync(path.join(directory, "accounting.jsonl"), "utf8")).toBe("complete");
  });

  it.each([undefined, null, "invalid"])("rejects missing or malformed history instead of proving an empty window", async workflow_runs => {
    const f = fixture([]);
    f.github.rest.actions.listWorkflowRuns = vi.fn(async () => response({ workflow_runs }));
    await expect(guardrail.listCompletedWorkflowRunsPage(f.github, params, f.budget)).rejects.toThrow("Incomplete daily AIC workflow history");
  });

  it("rejects repository entries whose workflow membership cannot be determined", async () => {
    const f = fixture([]);
    f.github.rest.actions.listWorkflowRunsForRepo = vi.fn(async () => response({ workflow_runs: [{ id: 1 }] }));
    await expect(guardrail.listCompletedWorkflowRunsPage(f.github, { ...params, lookupMode: "repo_workflow_id_fallback" }, f.budget)).rejects.toThrow("Incomplete daily AIC repository workflow history");
  });

  it.each([undefined, null, 0, -1, 1.5, "1"])("rejects invalid historical run identity %s before accounting or estimation", async id => {
    const f = fixture([run(id)]);
    await expect(scanDailyAIC(f)).rejects.toThrow("invalid identity");
    expect(f.getRunAIC).not.toHaveBeenCalled();
  });

  it.each([undefined, 0, -1, 1.5, "7"])("rejects invalid current workflow identity %s before listing history", async workflow_id => {
    const f = fixture([]);
    f.github.rest.actions.getWorkflowRun.mockResolvedValue(response({ workflow_id }));
    await expect(scanDailyAIC(f)).rejects.toThrow("Cannot resolve the daily AIC workflow");
    expect(f.listPage).not.toHaveBeenCalled();
    expect(f.getRunAIC).not.toHaveBeenCalled();
  });

  it.each(["ai_credits", "aiCredits", "aic"])("does not treat %s zero as proof of zero when token pricing is unknown", key => {
    const file = path.join(directory, "unknown-pricing.jsonl");
    fs.writeFileSync(file, JSON.stringify({ [key]: 0, provider: "unknown-provider", model: "unknown-model", input_tokens: 100 }));
    expect(() => guardrail.sumAICFromUsageJSONLFiles([file], { strict: true })).toThrow("could not be resolved");
    fs.writeFileSync(file, JSON.stringify({ [key]: 1, provider: "unknown-provider", model: "unknown-model", input_tokens: 100 }));
    expect(guardrail.sumAICFromUsageJSONLFiles([file], { strict: true })).toBe(1);
    fs.writeFileSync(file, JSON.stringify({ ai_credits_this_response: 0, input_tokens: 100 }));
    expect(guardrail.sumAICFromUsageJSONLFiles([file], { strict: true })).toBe(0);
  });

  it("counts and reports confirmed-zero runs rather than removing them from statistics", () => {
    expect(guardrail.calculateDailyAICStats([{ aic: 0 }, { aic: 2 }])).toMatchObject({ count: 2, total: 2, average: 1, min: 0 });
    expect(Number.isFinite(guardrail.calculateDailyAICStats([{ aic: 0 }, { aic: 1e308 }]).stddev)).toBe(true);
  });

  it.each([NaN, Infinity, -1, undefined])("rejects invalid aggregate input %s rather than silently dropping it", aic => {
    expect(() => guardrail.calculateDailyAICStats([{ aic }])).toThrow("not finite and non-negative");
  });

  it.each(["artifacts", "repo-memory"])("fails closed on aggregate overflow in the %s backend", async backend => {
    const f = fixture([]);
    const stamp = new Date().toISOString();
    global.context = f.context;
    global.github = f.github;
    const outputs = {};
    global.core.setOutput = (key, value) => {
      outputs[key] = value;
    };
    global.core.setFailed = vi.fn();
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS", "9");
    vi.stubEnv("GH_AW_MAX_DAILY_AI_CREDITS_BACKEND", backend);
    vi.stubEnv("GITHUB_EVENT_NAME", "schedule");
    vi.stubEnv("GH_AW_GITHUB_TOKEN", "synthetic");
    vi.stubEnv("GH_AW_WORKFLOW_ID", "7");
    vi.stubEnv("GH_AW_DAILY_AIC_REPO_MEMORY_TRUSTED", "true");
    f.github.rest.actions.listWorkflowRuns = vi.fn(async () => response({ workflow_runs: [run(1, { created_at: stamp, updated_at: stamp }), run(2, { created_at: stamp, updated_at: stamp })] }));
    vi.spyOn(guardrail, "getArtifactClient").mockResolvedValue({});
    vi.spyOn(guardrail, "getRunAIC").mockResolvedValue(1e308);
    const root = path.join(directory, "daily-aic-ledger");
    fs.mkdirSync(root);
    fs.writeFileSync(ledger.ledgerCoveragePath(directory, repository, "7"), JSON.stringify({ version: 1, repository, workflow_id: "7", initialized_at: new Date(Date.now() - ledger.WINDOW_MS).toISOString() }));
    fs.writeFileSync(path.join(root, `${stamp.slice(0, 10)}.jsonl`), [1, 2].map(run_id => JSON.stringify({ version: 1, repository, workflow_id: "7", run_id, timestamp: stamp, aic: 1e308 })).join("\n"));
    await guardrail.main({ cachePath, repoMemoryDir: directory });
    expect(outputs.daily_ai_credits_guardrail_status).toBe("transient_error");
    expect(outputs.daily_ai_credits_total).toBe("");
    expect(outputs.daily_ai_credits_exceeded).toBe("false");
    expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("aggregate total is not finite"));
  });

  it("classifies marked reserve stops as transient rather than permission errors", () => {
    expect(guardrail.isStructuralGuardrailError(Object.assign(apiError(403, { "x-ratelimit-remaining": "100" }, "reserve"), { code: "DAILY_AIC_QUOTA_STOP" }))).toBe(false);
    expect(guardrail.isStructuralGuardrailError(apiError(403, {}, "permission"))).toBe(true);
  });
});

describe("complete daily AIC scan observations", () => {
  it("limits repository fallback listings to the 24-hour accounting window", async () => {
    const f = fixture([]);
    await scanDailyAIC(f);
    expect(f.listPage).toHaveBeenCalledWith(
      f.github,
      expect.objectContaining({
        created: ">=2025-02-02T12:00:00.000Z",
      }),
      f.budget
    );
  });

  it("reuses real artifact accounting, persists it, then avoids list and download calls", async () => {
    const f = fixture();
    f.getRunAIC = guardrail.getRunAIC;
    f.artifactClient = {
      listArtifacts: vi.fn(async options => ({ artifacts: [{ id: options.findBy.workflowRunId, name: "usage", createdAt: new Date(time) }] })),
      downloadArtifact: vi.fn(async (id, options) => {
        fs.writeFileSync(path.join(options.path, "agent_usage.jsonl"), JSON.stringify({ aic: id === 2 ? 0 : id * 2 }));
        return { downloadPath: options.path };
      }),
    };
    const first = await scanDailyAIC(f);
    expect(first.countedRuns.map(item => item.aic)).toEqual([2, 0, 6]);
    expect(first.countedRuns.map(item => item.source)).toEqual(["recorded", "recorded", "recorded"]);
    expect(f.artifactClient.listArtifacts).toHaveBeenCalledTimes(3);
    expect(f.artifactClient.downloadArtifact).toHaveBeenCalledTimes(3);
    f.artifactClient.listArtifacts.mockClear();
    f.artifactClient.downloadArtifact.mockClear();
    f.github.rest.actions.listJobsForWorkflowRun.mockClear();
    expect((await scanDailyAIC(f)).cacheHits).toBe(3);
    expect(f.artifactClient.listArtifacts).not.toHaveBeenCalled();
    expect(f.artifactClient.downloadArtifact).not.toHaveBeenCalled();
    expect(f.github.rest.actions.listJobsForWorkflowRun).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"scan_cache"'));
  });

  it("recounts version 1 zero-credit observations from completed Agent and Detection jobs", async () => {
    const prior = scanCacheEntry(run(1), 0, repository, 7, now);
    prior.coverage_version = 1;
    writeEntries([prior]);
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    f.github.rest.actions.listJobsForWorkflowRun.mockResolvedValue(
      response({
        jobs: [
          { id: 1, name: "Agent", run_attempt: 1, status: "completed", conclusion: "success", started_at: time, completed_at: time },
          { id: 2, name: "Detection", run_attempt: 1, status: "completed", conclusion: "success", started_at: time, completed_at: time },
        ],
      })
    );
    f.artifactClient = {
      listArtifacts: vi.fn(async () => ({ artifacts: [{ id: 10, name: "usage", createdAt: new Date(time) }] })),
      downloadArtifact: vi.fn(async (_id, options) => {
        for (const [name, aic] of [
          ["agent", 18.02604],
          ["detection", 30.08748],
        ]) {
          const file = path.join(options.path, name, "token_usage.jsonl");
          fs.mkdirSync(path.dirname(file), { recursive: true });
          fs.writeFileSync(file, JSON.stringify({ aic }));
        }
        return { downloadPath: options.path };
      }),
    };
    const result = await scanDailyAIC(f);
    expect(result.cacheHits).toBe(0);
    expect(result.countedRuns[0].aic).toBeCloseTo(48.11352);
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(1)).toMatchObject({ coverage_version: 3, source: "recorded" });
    expect((await scanDailyAIC(f)).cacheHits).toBe(1);
  });

  it.each(["missing", "metadata-only", "malformed", "unknown-model", "old-attempt", "detection", "evals", "invalid-numeric"])("conservatively assumes max AI Credits when %s usage cannot be resolved", async kind => {
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    if (["detection", "evals"].includes(kind)) {
      f.github.rest.actions.listJobsForWorkflowRun.mockImplementation(async () =>
        response({
          jobs: [
            { id: 1, name: "agent", run_attempt: 1, status: "completed", conclusion: "success", started_at: time, completed_at: time },
            { id: 2, name: kind, run_attempt: 1, status: "completed", conclusion: "success", started_at: time, completed_at: time },
          ],
        })
      );
    }
    f.artifactClient = {
      listArtifacts: async () => ({ artifacts: kind === "missing" ? [] : [{ id: 1, name: "usage", createdAt: new Date(kind === "old-attempt" ? now - 120000 : time) }] }),
      downloadArtifact: async (_id, options) => {
        const value = kind === "malformed" ? "{invalid" : JSON.stringify(kind === "metadata-only" ? { workflow: "Example" } : { aic: 2 });
        fs.writeFileSync(path.join(options.path, "agent_usage.jsonl"), value);
        if (kind === "unknown-model") {
          fs.appendFileSync(path.join(options.path, "agent_usage.jsonl"), "\n" + JSON.stringify({ model: "synthetic-unknown-model", input_tokens: 100 }));
        }
        if (kind === "invalid-numeric") {
          fs.appendFileSync(path.join(options.path, "agent_usage.jsonl"), '\n{"provider":"openai","model":"gpt-4o","input_tokens":"invalid","output_tokens":500}');
        }
        return { downloadPath: options.path };
      },
    };
    const result = await scanDailyAIC(f);
    expect(result.countedRuns[0].aic).toBe(1000);
    expect(result.countedRuns[0].source).toBe("estimated");
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(1)?.aic).toBe(1000);
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(1)?.source).toBe("estimated");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Assuming max AI Credits after all accounting sources failed"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"max_ai_credits_fallback"'));
  });

  it("retries estimated and provenance-less cached values, replacing them with recorded usage", async () => {
    const first = fixture([run(1), run(2)]);
    first.getRunAIC.mockRejectedValue(new Error("accounting unavailable"));
    expect((await scanDailyAIC(first)).countedRuns.map(item => item.source)).toEqual(["estimated", "estimated"]);

    const legacy = readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(2);
    delete legacy.source;
    writeEntries([readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(1), legacy]);
    const second = fixture([run(1), run(2)]);
    second.getRunAIC.mockImplementation(async (_client, id) => (id === 1 ? 4 : 5));
    expect((await scanDailyAIC(second)).countedRuns.map(item => [item.aic, item.source])).toEqual([
      [4, "recorded"],
      [5, "recorded"],
    ]);
    expect(second.getRunAIC).toHaveBeenCalledTimes(2);
    expect((await scanDailyAIC(fixture([run(1), run(2)]))).cacheHits).toBe(2);
  });

  it("keeps unresolved legacy cache values out of recorded usage", async () => {
    const entry = scanCacheEntry(run(1), 1000, repository, 7, now);
    delete entry.source;
    writeEntries([entry]);
    const f = fixture([run(1)]);
    f.getRunAIC.mockRejectedValue(new Error("accounting unavailable"));
    const result = await scanDailyAIC(f);
    expect(result.countedRuns[0]).toMatchObject({ aic: 1000, source: "estimated" });
    expect(f.getRunAIC).toHaveBeenCalledOnce();
  });

  it("reinspects recorded zero usage cached with the old component coverage", async () => {
    const entry = scanCacheEntry(run(1), 0, repository, 7, now);
    entry.coverage_version = 1;
    writeEntries([entry]);
    const f = fixture([run(1)]);
    f.getRunAIC.mockResolvedValue(48.11352);
    const result = await scanDailyAIC(f);
    expect(result.cacheHits).toBe(0);
    expect(result.countedRuns[0]).toMatchObject({ aic: 48.11352, source: "recorded" });
    expect(f.getRunAIC).toHaveBeenCalledOnce();
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(1)?.aic).toBe(48.11352);
  });

  it("accepts corrected estimates without replacing recorded accounting for the same attempt", () => {
    const recorded = scanCacheEntry(run(1), 4, repository, 7, now - 2000, "recorded");
    const estimated = scanCacheEntry(run(1), 1000, repository, 7, now - 1000, "estimated");
    const nextAttempt = scanCacheEntry(run(1, { run_attempt: 2 }), 1000, repository, 7, now, "estimated");
    expect(readScanCache([JSON.stringify(recorded), JSON.stringify(estimated)].join("\n"), repository, 7, now).get(1).aic).toBe(4);
    expect(readScanCache([JSON.stringify(recorded), JSON.stringify(nextAttempt)].join("\n"), repository, 7, now).get(1)).toMatchObject({ run_attempt: 2, source: "estimated" });
  });

  it("still rejects unresolved usage when no positive max-AI-credits fallback is available", async () => {
    const f = fixture([run(1)]);
    f.fallbackAIC = 0;
    f.getRunAIC.mockRejectedValue(new Error("synthetic accounting failure"));

    await expect(scanDailyAIC(f)).rejects.toThrow("synthetic accounting failure");
    expect(global.core.warning).not.toHaveBeenCalled();
  });

  it("retries a transient historical jobs read without restarting the scan", async () => {
    vi.useFakeTimers();
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    f.github.rest.actions.listJobsForWorkflowRun.mockRejectedValueOnce(apiError(502, { "x-ratelimit-remaining": "4000" }, "Server Error"));
    f.artifactClient = {
      listArtifacts: vi.fn(async () => ({ artifacts: [{ id: 1, name: "usage", createdAt: new Date(time) }] })),
      downloadArtifact: vi.fn(async (_id, options) => {
        fs.writeFileSync(path.join(options.path, "agent_usage.jsonl"), '{"aic":2}');
        return { downloadPath: options.path };
      }),
    };
    const pending = scanDailyAIC(f);
    await vi.runAllTimersAsync();
    expect((await pending).countedRuns[0]).toMatchObject({ aic: 2, source: "recorded" });
    expect(f.github.rest.actions.listJobsForWorkflowRun).toHaveBeenCalledTimes(2);
    expect(f.github.rest.actions.getWorkflowRun).toHaveBeenCalledOnce();
    expect(f.listPage).toHaveBeenCalledOnce();
    expect(f.artifactClient.downloadArtifact).toHaveBeenCalledOnce();
  });

  it.each([502, 504])("estimates historical accounting after exhausting HTTP %i retries", async status => {
    vi.useFakeTimers();
    const f = fixture([run(1), run(2)]);
    f.getRunAIC = guardrail.getRunAIC;
    f.github.rest.actions.listJobsForWorkflowRun.mockRejectedValue(apiError(status, { "x-ratelimit-remaining": "4000" }, "Server Error"));
    const pending = scanDailyAIC(f);
    await vi.runAllTimersAsync();
    expect((await pending).countedRuns.map(item => [item.aic, item.source])).toEqual([
      [1000, "estimated"],
      [1000, "estimated"],
    ]);
    expect(f.github.rest.actions.listJobsForWorkflowRun).toHaveBeenCalledTimes(6);
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).get(1)).toMatchObject({ aic: 1000, source: "estimated" });
    const recovered = fixture([run(1), run(2)]);
    expect((await scanDailyAIC(recovered)).countedRuns.map(item => item.source)).toEqual(["recorded", "recorded"]);
    expect(recovered.getRunAIC).toHaveBeenCalledTimes(2);
  });

  it.each(["list", "download", "blob"])("estimates usage after exhausting native-fetch artifact %s retries", async stage => {
    vi.useFakeTimers();
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    const fetch = vi.fn(async url => {
      if (url.endsWith("/artifacts") || url.includes("/artifacts?")) {
        if (stage === "list") return new Response(null, { status: 502, headers: { "x-ratelimit-remaining": "4000" } });
        return new Response(JSON.stringify({ artifacts: [{ id: 1, name: "usage", created_at: time }] }), { headers: { "x-ratelimit-remaining": "4000" } });
      }
      if (url.endsWith("/zip") && stage === "blob") {
        return new Response(null, { status: 302, headers: { location: "https://example.invalid/blob", "x-ratelimit-remaining": "4000" } });
      }
      return new Response(null, { status: 504, headers: { "x-ratelimit-remaining": "4000" } });
    });
    vi.stubGlobal("fetch", fetch);
    f.artifactClient = await guardrail.getArtifactClient(undefined, f.budget.request);
    const pending = scanDailyAIC(f);
    await vi.runAllTimersAsync();
    expect((await pending).countedRuns[0]).toMatchObject({ aic: 1000, source: "estimated" });
    expect(fetch).toHaveBeenCalledTimes(stage === "list" ? 3 : stage === "download" ? 4 : 5);
    expect(f.budget.snapshot().requests).toBe(stage === "blob" ? 5 : stage === "list" ? 6 : 7);
    if (stage === "blob") {
      for (const [url, init] of fetch.mock.calls.filter(([url]) => url === "https://example.invalid/blob")) {
        expect(url).toBe("https://example.invalid/blob");
        expect(init.headers).toBeUndefined();
      }
    }
  });

  it("recovers timed-out artifact listing and transient download reads with fresh signals", async () => {
    vi.useFakeTimers();
    const f = fixture([run(1)]);
    f.getRunAIC = guardrail.getRunAIC;
    const controllers = [];
    vi.spyOn(AbortSignal, "timeout").mockImplementation(() => {
      const controller = new AbortController();
      controllers.push(controller);
      return controller.signal;
    });
    const fetch = vi
      .fn()
      .mockImplementationOnce(async (_url, init) => {
        controllers[0].abort(new DOMException("Timed out", "TimeoutError"));
        throw init.signal.reason;
      })
      .mockResolvedValueOnce(new Response(JSON.stringify({ artifacts: [{ id: 1, name: "usage", created_at: time }] })))
      .mockResolvedValueOnce(new Response(null, { status: 504 }))
      .mockResolvedValueOnce(new Response(null, { status: 302, headers: { location: "https://example.invalid/blob" } }))
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
      .mockResolvedValueOnce(new Response('{"aic":2}', { headers: { "content-type": "application/json", "content-disposition": 'attachment; filename="agent_usage.jsonl"' } }));
    vi.stubGlobal("fetch", fetch);
    f.artifactClient = await guardrail.getArtifactClient(undefined, f.budget.request);
    const pending = scanDailyAIC(f);
    await vi.runAllTimersAsync();
    expect((await pending).countedRuns[0]).toMatchObject({ aic: 2, source: "recorded" });
    expect(fetch).toHaveBeenCalledTimes(6);
    expect(f.budget.snapshot().requests).toBe(6);
    expect(new Set(fetch.mock.calls.map(([_url, init]) => init.signal)).size).toBe(6);
    expect(controllers[0].signal.aborted).toBe(true);
    expect(controllers.slice(1).every(controller => !controller.signal.aborted)).toBe(true);
    const warm = fixture([run(1)]);
    expect((await scanDailyAIC(warm)).cacheHits).toBe(1);
    expect(warm.getRunAIC).not.toHaveBeenCalled();
  });

  it("still rejects exhausted server errors without a positive per-run maximum", async () => {
    vi.useFakeTimers();
    const f = fixture([run(1)]);
    f.fallbackAIC = 0;
    f.getRunAIC = guardrail.getRunAIC;
    f.github.rest.actions.listJobsForWorkflowRun.mockRejectedValue(apiError(502, {}, "Server Error"));
    const assertion = expect(scanDailyAIC(f)).rejects.toMatchObject({ status: 502 });
    await vi.runAllTimersAsync();
    await assertion;
    expect(f.github.rest.actions.listJobsForWorkflowRun).toHaveBeenCalledTimes(3);
    expect(global.core.warning).not.toHaveBeenCalled();
  });

  it.each([401, 403, 404, 429])("never estimates or continues historical accounting after HTTP %i", async status => {
    const f = fixture([run(1), run(2)]);
    f.getRunAIC.mockRejectedValue(apiError(status, {}, "denied"));
    await expect(scanDailyAIC(f)).rejects.toMatchObject({ status });
    expect(f.getRunAIC).toHaveBeenCalledOnce();
    expect(global.core.warning).not.toHaveBeenCalled();
  });

  it.each(["reserve", "retry-after"])("never estimates through a %s restriction on a historical 5xx", async kind => {
    const f = fixture([run(1), run(2)]);
    f.getRunAIC = guardrail.getRunAIC;
    const headers = kind === "reserve" ? { "x-ratelimit-remaining": "100" } : { "retry-after": "120" };
    f.github.rest.actions.listJobsForWorkflowRun.mockRejectedValue(apiError(502, headers, "Server Error"));
    await expect(scanDailyAIC(f)).rejects.toMatchObject({ status: 502 });
    expect(f.github.rest.actions.listJobsForWorkflowRun).toHaveBeenCalledOnce();
    expect(global.core.warning).not.toHaveBeenCalled();
  });

  it.each(["current", "history"])("fails closed when the authoritative %s read exhausts retries", async kind => {
    vi.useFakeTimers();
    const f = fixture([run(1)]);
    const request = kind === "current" ? f.github.rest.actions.getWorkflowRun : f.pageRead;
    request.mockRejectedValue(apiError(502, { "x-ratelimit-remaining": "4000" }, "Server Error"));
    const assertion = expect(scanDailyAIC(f)).rejects.toMatchObject({ status: 502 });
    await vi.runAllTimersAsync();
    await assertion;
    expect(request).toHaveBeenCalledTimes(3);
    expect(f.getRunAIC).not.toHaveBeenCalled();
    expect(global.core.warning).not.toHaveBeenCalled();
  });

  it("records absent usage as zero only with authoritative skipped billable jobs for that attempt", async () => {
    const f = fixture([run(1, { run_attempt: 2 })]);
    f.getRunAIC = guardrail.getRunAIC;
    f.artifactClient = { listArtifacts: async () => ({ artifacts: [] }) };
    const jobs = vi.fn(async () =>
      response({
        jobs: [
          { id: 1, name: "agent", run_attempt: 2, status: "completed", conclusion: "skipped" },
          { id: 2, name: "detection", run_attempt: 2, status: "completed", conclusion: "skipped" },
        ],
      })
    );
    f.github.rest.actions.listJobsForWorkflowRun = jobs;
    expect((await scanDailyAIC(f)).countedRuns[0].aic).toBe(0);
    expect(jobs).toHaveBeenCalledWith(expect.objectContaining({ run_id: 1, filter: "all" }));
    jobs.mockClear();
    expect((await scanDailyAIC(f)).cacheHits).toBe(1);
    expect(jobs).not.toHaveBeenCalled();
  });

  it("persists every resolved value and eliminates all historical artifact reads on a warm scan", async () => {
    const cold = fixture();
    const first = await scanDailyAIC(cold);
    expect(first.countedRuns.map(item => item.aic)).toEqual([2, 0, 6]);
    expect(cold.getRunAIC).toHaveBeenCalledTimes(3);
    const warm = fixture();
    const second = await scanDailyAIC(warm);
    expect(second.cacheHits).toBe(3);
    expect(second.countedRuns).toEqual(first.countedRuns);
    expect(warm.getRunAIC).not.toHaveBeenCalled();
    expect(warm.listPage).toHaveBeenCalledTimes(1);
  });

  it.each(["missing", "legacy", "uncertified", "stale", "wrong-repository", "wrong-workflow", "future"])("treats %s cache as observations missing, not zero", async kind => {
    const entry = scanCacheEntry(run(1), 2, repository, 7, now);
    if (kind === "legacy") delete entry.version;
    if (kind === "uncertified") delete entry.coverage_version;
    if (kind === "stale") entry.observed_at = new Date(now - 49 * 3600000).toISOString();
    if (kind === "future") entry.observed_at = new Date(now + 1000).toISOString();
    if (kind === "wrong-repository") entry.repository = "other/project";
    if (kind === "wrong-workflow") entry.workflow_id = 8;
    if (kind !== "missing") writeEntries([entry]);
    const f = fixture([run(1)]);
    expect((await scanDailyAIC(f)).countedRuns[0].aic).toBe(2);
    expect(f.getRunAIC).toHaveBeenCalledOnce();
  });

  it("fills an incomplete snapshot and deduplicates repeated runs", async () => {
    writeEntries([scanCacheEntry(run(1), 2, repository, 7, now)]);
    const f = fixture([run(1), run(1), run(2)]);
    const result = await scanDailyAIC(f);
    expect(result.countedRuns).toHaveLength(2);
    expect(f.getRunAIC).toHaveBeenCalledOnce();
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).size).toBe(2);
  });

  it("invalidates reruns and updated completion metadata instead of reusing earlier attempt cost", async () => {
    writeEntries([scanCacheEntry(run(1), 2, repository, 7, now), scanCacheEntry(run(2), 0, repository, 7, now)]);
    const f = fixture([run(1, { run_attempt: 2 }), run(2, { updated_at: new Date(now).toISOString() })]);
    await scanDailyAIC(f);
    expect(f.getRunAIC).toHaveBeenCalledTimes(2);
    const warm = fixture([run(1, { run_attempt: 2 }), run(2, { updated_at: new Date(now).toISOString() })]);
    await scanDailyAIC(warm);
    expect(warm.getRunAIC).not.toHaveBeenCalled();
  });

  it("allows concurrent incomplete snapshots to miss entries, never to undercount", async () => {
    const entries = [1, 2, 3].map(id => scanCacheEntry(run(id), id === 2 ? 0 : id * 2, repository, 7, now));
    for (const snapshot of [entries.slice(0, 2), entries.slice(1)]) {
      writeEntries(snapshot);
      const f = fixture();
      expect((await scanDailyAIC(f)).countedRuns.reduce((sum, item) => sum + item.aic, 0)).toBe(8);
      expect(f.getRunAIC).toHaveBeenCalledOnce();
    }
  });

  it.each([401, 403, 404, 429])("stops after the first %i failure and persists only resolved observations", async status => {
    const f = fixture();
    f.getRunAIC.mockResolvedValueOnce(2).mockRejectedValueOnce(apiError(status, {}, "synthetic failure"));
    await expect(scanDailyAIC(f)).rejects.toMatchObject({ status });
    expect(f.getRunAIC).toHaveBeenCalledTimes(2);
    const saved = readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now);
    expect([...saved.keys()]).toEqual([1]);
  });

  it("refuses a truncated listing and never inspects its incomplete candidate set", async () => {
    const f = fixture();
    f.pageRead.mockResolvedValue({ response: response({ workflow_runs: [run(1)] }), sourceRunCount: 100, lookupMode: "workflow_id" });
    await expect(scanDailyAIC(f)).rejects.toThrow("complete pagination");
    expect(f.listPage).toHaveBeenCalledTimes(25);
    expect(f.getRunAIC).not.toHaveBeenCalled();
  });

  it("rejects conflicting observations for an immutable completed attempt", () => {
    const entries = [2, 3].map(aic => scanCacheEntry(run(1), aic, repository, 7, now));
    expect(() => readScanCache(entries.map(entry => JSON.stringify(entry)).join("\n"), repository, 7, now)).toThrow("Conflicting");
  });
});

describe("business response circuit breaker", () => {
  it("stops before following a signed download redirect when the business quota reaches its reserve", async () => {
    const fetch = vi.fn(
      async () =>
        new Response(null, {
          status: 302,
          headers: {
            location: "https://example.invalid/download?secret=must-not-log",
            "x-ratelimit-remaining": "100",
          },
        })
    );
    vi.stubGlobal("fetch", fetch);
    const client = new DefaultArtifactClient({ onResponse: createAPIBudget().observe });
    await expect(
      client.downloadArtifact(1, {
        path: directory,
        findBy: { token: "synthetic", repositoryOwner: "example", repositoryName: "project" },
      })
    ).rejects.toThrow("preserve GitHub API quota");
    expect(fetch).toHaveBeenCalledOnce();
  });

  it("stops a native-fetch artifact listing on the first exhausted response and keeps only safe headers", async () => {
    const headers = { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(now / 1000 + 60), "retry-after": "120", authorization: "must-not-log" };
    const fetch = vi.fn(async () => new Response("denied", { status: 403, headers }));
    vi.stubGlobal("fetch", fetch);
    const client = new DefaultArtifactClient({ onResponse: createAPIBudget().observe });
    await expect(client.listArtifacts({ findBy: { token: "synthetic", workflowRunId: 1, repositoryOwner: "example", repositoryName: "project" } })).rejects.toMatchObject({ status: 403, response: { headers: safeResponseHeaders(headers) } });
    expect(fetch).toHaveBeenCalledOnce();
    expect(safeResponseHeaders(headers)).not.toHaveProperty("authorization");
    expect(retryNotBefore(headers, now)).toBe(new Date(now + 120000).toISOString());
  });

  it("does not retry permission failures as quota failures", async () => {
    const fetch = vi.fn(async () => new Response("forbidden", { status: 403 }));
    vi.stubGlobal("fetch", fetch);
    const client = new DefaultArtifactClient({ onResponse: createAPIBudget().observe });
    await expect(client.listArtifacts({ findBy: { token: "synthetic", workflowRunId: 1, repositoryOwner: "example", repositoryName: "project" } })).rejects.toMatchObject({ status: 403 });
    expect(fetch).toHaveBeenCalledOnce();
    expect(retryNotBefore({}, now)).toBe("");
  });
});

describe("trusted artifact fallback without writable Actions cache", () => {
  it.each([
    [1, false],
    [2, false],
    [2, true],
  ])("restores from page %i before reading later history that would exhaust quota (empty earlier snapshot: %s)", async (snapshotPage, emptyEarlierSnapshot) => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, id: 50, event: "push", head_branch: "main", repository: { full_name: repository }, head_repository: { full_name: repository } };
    const workflowRunAt = (page, id) => {
      if (page === snapshotPage && id === 0) return producer;
      if (emptyEarlierSnapshot && page === 1 && id === 0) return { ...producer, id: 49 };
      return { ...producer, id: 1000 + id, workflow_id: 8 };
    };
    const listWorkflowRunsForRepo = vi.fn(async ({ page }) => {
      if (page > snapshotPage) throw apiError(403, { "x-ratelimit-remaining": "0" }, "quota exhausted");
      return response({
        workflow_runs: Array.from({ length: 100 }, (_, id) => workflowRunAt(page, id)),
      });
    });
    const listWorkflowRunArtifacts = vi.fn(async ({ run_id }) => response({ artifacts: [{ id: run_id + 100, name: "aic-usage-scan-v2", expired: false }] }));
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: vi.fn(async () => ({ token: "synthetic" })),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => {
            throw apiError(404, {}, "workflow not found");
          },
          listWorkflowRunsForRepo,
          listWorkflowRunArtifacts,
        },
      },
    };
    const downloadArtifact = vi.fn(async (id, options) => {
      fs.writeFileSync(path.join(options.path, path.basename(AIC_SCAN_CACHE_FILE_PATH)), id === 149 ? "" : JSON.stringify(scanCacheEntry(run(1), 2, repository, 7, now)));
      return { downloadPath: options.path };
    });
    await restore(cachePath, { createArtifactClient: () => ({ downloadArtifact }) });
    expect(global.github.auth).toHaveBeenCalledExactlyOnceWith({ type: "token" });
    expect(listWorkflowRunsForRepo).toHaveBeenCalledTimes(snapshotPage);
    expect(listWorkflowRunArtifacts).toHaveBeenCalledTimes(emptyEarlierSnapshot ? 2 : 1);
    expect(downloadArtifact).toHaveBeenCalledTimes(emptyEarlierSnapshot ? 2 : 1);
    const f = fixture([run(1), run(2)]);
    const result = await scanDailyAIC(f);
    expect(result.cacheHits).toBe(1);
    expect(result.countedRuns).toHaveLength(2);
    expect(f.listPage).toHaveBeenCalledOnce();
    expect(f.getRunAIC).toHaveBeenCalledOnce();
    expect(f.getRunAIC.mock.calls[0][1]).toBe(2);
  });

  it.each([
    ["missing auth", undefined],
    ["rejected auth", undefined],
    ["undefined auth result", undefined],
    ["null auth result", null],
    ["non-object auth result", "synthetic"],
    ["missing token", {}],
    ["non-string token", { token: 1 }],
    ["empty token", { token: "" }],
  ])("fails closed for %s after finding a trusted producer", async (kind, authResult) => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, id: 50, event: "pull_request_target", repository: { full_name: repository } };
    const failure = new Error("private auth failure");
    const auth = vi.fn().mockResolvedValue(authResult);
    if (kind === "rejected auth") auth.mockRejectedValue(failure);
    const listWorkflowRunArtifacts = vi.fn();
    const downloadArtifact = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = {
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => response({ workflow_runs: [producer] }),
          listWorkflowRunArtifacts,
        },
      },
    };
    if (kind !== "missing auth") global.github.auth = auth;
    const assertion = expect(restore(cachePath, { createArtifactClient: () => ({ downloadArtifact }) })).rejects;
    if (kind === "missing auth") await assertion.toThrow(TypeError);
    else if (kind === "rejected auth") await assertion.toBe(failure);
    else await assertion.toThrow("No token available to restore daily AIC observations");
    if (kind !== "missing auth") expect(auth).toHaveBeenCalledExactlyOnceWith({ type: "token" });
    expect(listWorkflowRunArtifacts).not.toHaveBeenCalled();
    expect(downloadArtifact).not.toHaveBeenCalled();
    expect(fs.existsSync(cachePath)).toBe(false);
    expect(JSON.stringify([global.core.info.mock.calls, global.core.warning.mock.calls])).not.toContain("private auth failure");
  });

  it("stops on quota exhaustion on a later required-workflow history page when no snapshot was verified", async () => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const failure = apiError(403, { "x-ratelimit-remaining": "0" }, "quota exhausted");
    const listWorkflowRunsForRepo = vi
      .fn()
      .mockResolvedValueOnce(response({ workflow_runs: Array.from({ length: 100 }, (_, id) => ({ ...current, id, event: "pull_request", repository: { full_name: repository } })) }))
      .mockRejectedValue(failure);
    const listWorkflowRunArtifacts = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => {
            throw apiError(404, {}, "workflow not found");
          },
          listWorkflowRunsForRepo,
          listWorkflowRunArtifacts,
        },
      },
    };
    global.core.summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    await expect(restore(cachePath)).resolves.toBeUndefined();
    expect(global.core.exportVariable).toHaveBeenCalledWith("GH_AW_DAILY_AIC_QUOTA_BLOCKED", expect.stringContaining('"operation":"repository_history"'));
    expect(listWorkflowRunsForRepo).toHaveBeenCalledTimes(2);
    expect(listWorkflowRunArtifacts).not.toHaveBeenCalled();
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("restore stopped at repository_history after 1 attempt(s)"));
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("API responses observed by this restore: 4"));
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("a cache miss has not been established"));
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("summarizes first-read quota exhaustion and blocks all further restore requests", async () => {
    vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-14T10:04:13Z"));
    const getWorkflowRun = vi.fn().mockRejectedValue(apiError(403, { "x-ratelimit-remaining": "0", "x-ratelimit-limit": "15000", "x-ratelimit-used": "15000", "x-ratelimit-reset": "1791972360" }, "secret-test-value"));
    const listWorkflowRuns = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun, listWorkflowRuns } } };
    const summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    global.core.summary = summary;
    await expect(restore(cachePath)).resolves.toBeUndefined();
    expect(getWorkflowRun).toHaveBeenCalledOnce();
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("### GitHub API quota exhausted"));
    expect(global.core.setOutput).toHaveBeenCalledWith("github_api_quota_blocked", "true");
    expect(JSON.parse(global.core.exportVariable.mock.calls[0][1])).toMatchObject({ status: 403, resource: "unknown", limit: 15000, remaining: 0, used: 15000, reset: "2026-10-14T10:06:00.000Z", operation: "current_run" });
    expect(listWorkflowRuns).not.toHaveBeenCalled();
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("restore stopped at current_run after 1 attempt(s)"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("quota_exhausted"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("reset=2026-10-14T10:06:00.000Z"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Accounting remains unverified; agent startup is blocked"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Activation stage: Restore daily AIC scan observations"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("API responses observed by this restore: 1; shared quota used is not this step's request count"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Snapshot discovery has not started in the failed restore phase attempt; earlier attempts may have queried history or artifacts"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Blocked until at least 2026-10-14T10:06:00.000Z"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Check existing scheduled runs after the deadline before retrying"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("github-token reference"));
    expect(JSON.stringify([global.core.info.mock.calls, summary.addRaw.mock.calls])).not.toContain("secret-test-value");
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("does not let a summary-write failure hide the original permission failure", async () => {
    const failure = apiError(403, {}, "secret-test-value");
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun: vi.fn().mockRejectedValue(failure) } } };
    global.core.summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockRejectedValue(new Error("private summary payload")) };
    await expect(restore(cachePath)).rejects.toBe(failure);
    expect(global.core.warning).toHaveBeenCalledWith("Failed to write daily AIC snapshot restore failure summary.");
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("permission_denied"));
    expect(JSON.stringify([global.core.info.mock.calls, global.core.warning.mock.calls])).not.toContain("private summary payload");
  });

  it("includes quota metadata in summaries when artifact transport wraps a read failure", async () => {
    vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-14T10:04:13Z"));
    const failure = apiError(403, { "x-ratelimit-remaining": "0", "x-ratelimit-reset": "1791972360" }, "API rate limit exceeded for installation; private payload");
    const wrapped = new Error("unable to download artifact: private payload", { cause: failure });
    const getWorkflowRun = vi.fn().mockRejectedValue(wrapped);
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun } } };
    const summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    global.core.summary = summary;
    await expect(restore(cachePath)).resolves.toBeUndefined();
    expect(global.core.exportVariable).toHaveBeenCalledWith("GH_AW_DAILY_AIC_QUOTA_BLOCKED", expect.any(String));
    expect(getWorkflowRun).toHaveBeenCalledOnce();
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("quota_exhausted"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("retry_not_before=2026-10-14T10:06:00.000Z"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Blocked until at least 2026-10-14T10:06:00.000Z"));
    expect(global.core.info).toHaveBeenCalledWith("[daily-aic-cache] No further API requests before 2026-10-14T10:06:00.000Z");
    expect(JSON.stringify([global.core.info.mock.calls, summary.addRaw.mock.calls])).not.toContain("private payload");
  });

  it.each([
    [{ "x-ratelimit-remaining": "0", "x-ratelimit-reset": "1791972360", "retry-after": "120" }, "2026-10-14T10:06:13.000Z"],
    [{ "x-ratelimit-remaining": "4000", "x-ratelimit-reset": "1791972900", "retry-after": "120" }, "2026-10-14T10:06:13.000Z"],
    [{ "x-ratelimit-remaining": "100", "x-ratelimit-reset": "1791972900", "retry-after": "120" }, "2026-10-14T10:15:00.000Z"],
    [{ "x-ratelimit-remaining": "4000", "x-ratelimit-reset": "1791972900", "retry-after": "invalid" }, null],
    [{ "retry-after": "120" }, "2026-10-14T10:06:13.000Z"],
    [{ "x-ratelimit-remaining": "0" }, null],
    [{ "x-ratelimit-remaining": "0", "x-ratelimit-reset": "9999999999999999", "retry-after": "private-header-value" }, null],
    [{ "x-ratelimit-remaining": "0", "x-ratelimit-reset": "1" }, null],
  ])("reports a safe recovery deadline without retrying restricted reads: %j", async (headers, deadline) => {
    vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-14T10:04:13Z"));
    const failure = apiError(403, headers, "private API payload");
    const getWorkflowRun = vi.fn().mockRejectedValue(failure);
    const listWorkflowRuns = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun, listWorkflowRuns } } };
    const summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    global.core.summary = summary;
    await expect(restore(cachePath)).resolves.toBeUndefined();
    expect(global.core.exportVariable).toHaveBeenCalledOnce();
    expect(getWorkflowRun).toHaveBeenCalledOnce();
    expect(listWorkflowRuns).not.toHaveBeenCalled();
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(deadline ? `Blocked until at least ${deadline}` : "Blocked until: unknown"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("A reset does not verify accounting or authorize agent startup"));
    expect(JSON.stringify([global.core.info.mock.calls, summary.addRaw.mock.calls])).not.toMatch(/private API payload|private-header-value/);
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it.each([
    ["HTTP 502", () => apiError(502, {}, "private API payload"), 502],
    ["HTTP 504", () => apiError(504, {}, "private API payload"), 504],
    ["transport", () => Object.assign(new Error("private transport payload"), { code: "ECONNRESET" }), "unknown"],
  ])("summarizes exhausted %s restore reads without exposing raw errors", async (_kind, createFailure, status) => {
    vi.useFakeTimers();
    const failure = createFailure();
    const getWorkflowRun = vi.fn().mockRejectedValue(failure);
    const listWorkflowRuns = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun, listWorkflowRuns } } };
    const summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    global.core.summary = summary;
    const assertion = expect(restore(cachePath)).rejects.toBe(failure);
    await vi.runAllTimersAsync();
    await assertion;
    expect(getWorkflowRun).toHaveBeenCalledTimes(3);
    expect(listWorkflowRuns).not.toHaveBeenCalled();
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("restore stopped at current_run after 3 attempt(s)"));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(`HTTP ${status}; reason=retry_exhausted`));
    expect(summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Accounting remains unverified; agent startup is blocked"));
    expect(summary.write).toHaveBeenCalledOnce();
    expect(JSON.stringify([global.core.info.mock.calls, summary.addRaw.mock.calls])).not.toContain(failure.message);
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("does not let a summary-write failure hide the original exhausted transport failure", async () => {
    vi.useFakeTimers();
    const failure = Object.assign(new Error("private transport payload"), { code: "ECONNRESET" });
    const getWorkflowRun = vi.fn().mockRejectedValue(failure);
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: {} };
    global.github = { rest: { actions: { getWorkflowRun } } };
    global.core.summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockRejectedValue(new Error("private summary payload")) };
    const assertion = expect(restore(cachePath)).rejects.toBe(failure);
    await vi.runAllTimersAsync();
    await assertion;
    expect(getWorkflowRun).toHaveBeenCalledTimes(3);
    expect(global.core.warning).toHaveBeenCalledWith("Failed to write daily AIC snapshot restore failure summary.");
    expect(global.core.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("HTTP unknown; reason=retry_exhausted"));
    const logged = JSON.stringify([global.core.info.mock.calls, global.core.warning.mock.calls, global.core.summary.addRaw.mock.calls]);
    expect(logged).not.toContain("private transport payload");
    expect(logged).not.toContain("private summary payload");
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("restores required-workflow observations from repository history after workflow history returns 404", async () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, id: 50, event: "push", head_branch: "main", repository: { full_name: repository }, head_repository: { full_name: repository } };
    const listWorkflowRunsForRepo = vi.fn(async ({ page }) =>
      response({ workflow_runs: page === 1 ? Array.from({ length: 100 }, (_, id) => ({ ...producer, id: id + 100, path: "other.yml" })) : [{ ...producer, id: 51, workflow_id: 8 }, { ...producer, id: 52, head_branch: "topic" }, producer] })
    );
    const listWorkflowRunArtifacts = vi.fn(async () => response({ artifacts: [{ id: 100, name: "aic-usage-scan-v2", expired: false }] }));
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: async () => ({ token: "synthetic" }),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: vi.fn(async () => {
            throw apiError(404, {}, "workflow not found");
          }),
          listWorkflowRunsForRepo,
          listWorkflowRunArtifacts,
        },
      },
    };
    const downloadArtifact = vi.fn(async (_id, options) => {
      fs.writeFileSync(path.join(options.path, path.basename(AIC_SCAN_CACHE_FILE_PATH)), JSON.stringify(scanCacheEntry(run(1), 2, repository, 7, now)));
      return { downloadPath: options.path };
    });
    await restore(cachePath, { createArtifactClient: () => ({ downloadArtifact }) });
    expect(global.github.rest.actions.listWorkflowRuns).toHaveBeenCalledWith({ owner: "example", repo: "project", workflow_id: 7, status: "completed", created: ">=2025-02-01T12:00:00.000Z", per_page: 10 });
    expect(listWorkflowRunsForRepo).toHaveBeenCalledTimes(2);
    expect(global.core.info).toHaveBeenCalledWith('[daily-aic-cache] Evaluated repository history page: {"page":1,"listedRuns":100,"trustedCandidates":0}');
    expect(global.core.info).toHaveBeenCalledWith('[daily-aic-cache] Evaluated repository history page: {"page":2,"listedRuns":3,"trustedCandidates":1}');
    expect(listWorkflowRunsForRepo).toHaveBeenCalledWith({ owner: "example", repo: "project", status: "completed", created: ">=2025-02-01T12:00:00.000Z", per_page: 100, page: 1 });
    expect(listWorkflowRunArtifacts).toHaveBeenCalledOnce();
    expect(listWorkflowRunArtifacts).toHaveBeenCalledWith({ owner: "example", repo: "project", run_id: 50, per_page: 100 });
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("GitHub API rate limit level:"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"remaining":4000'));
    expect((await scanDailyAIC(fixture([run(1)]))).cacheHits).toBe(1);
  });

  it.each([0, 6])("bounds required-workflow history searches with %i trusted producers per page and no snapshot", async trustedPerPage => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const listWorkflowRunsForRepo = vi.fn(async ({ page }) =>
      response({
        workflow_runs: Array.from({ length: 100 }, (_, id) => ({
          ...current,
          id: page * 100 + id,
          event: id < trustedPerPage ? "push" : "pull_request",
          head_branch: "main",
          repository: { full_name: repository },
          head_repository: { full_name: repository },
        })),
      })
    );
    const listWorkflowRunArtifacts = vi.fn(async () => response({ artifacts: [] }));
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: vi.fn(async () => ({ token: "synthetic" })),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => {
            throw apiError(404, {}, "workflow not found");
          },
          listWorkflowRunsForRepo,
          listWorkflowRunArtifacts,
        },
      },
    };
    await restore(cachePath);
    expect(listWorkflowRunsForRepo).toHaveBeenCalledTimes(trustedPerPage ? 1 : 2);
    expect(global.github.auth).toHaveBeenCalledTimes(trustedPerPage ? 1 : 0);
    expect(listWorkflowRunArtifacts).toHaveBeenCalledTimes(trustedPerPage ? 2 : 0);
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("does not fall back to repository history for non-404 workflow errors", async () => {
    const listWorkflowRunsForRepo = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      rest: {
        actions: {
          getWorkflowRun: async () => response({ workflow_id: 7, path: ".github/workflows/example.yml" }),
          listWorkflowRuns: async () => {
            throw apiError(403, {}, "forbidden");
          },
          listWorkflowRunsForRepo,
        },
      },
    };
    await expect(restore(cachePath)).rejects.toMatchObject({ status: 403 });
    expect(listWorkflowRunsForRepo).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("GitHub API rate limit level:"));
  });

  it.each([
    ["quota reserve", { "x-ratelimit-remaining": "100" }, { code: "DAILY_AIC_QUOTA_STOP" }],
    ["explicit Retry-After", { "retry-after": "120" }, { status: 404 }],
  ])("does not bypass %s through the workflow-history 404 fallback", async (_reason, headers, expected) => {
    const listWorkflowRunsForRepo = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      rest: {
        actions: {
          getWorkflowRun: async () => response({ workflow_id: 7, path: ".github/workflows/example.yml" }),
          listWorkflowRuns: async () => {
            throw apiError(404, headers, "not found");
          },
          listWorkflowRunsForRepo,
        },
      },
    };
    await expect(restore(cachePath)).rejects.toMatchObject(expected);
    expect(listWorkflowRunsForRepo).not.toHaveBeenCalled();
  });

  it.each([undefined, null, 0, -1, 1.5, "7", Number.MAX_SAFE_INTEGER + 1])("rejects invalid restore workflow identity %s before listing history", async workflow_id => {
    const listWorkflowRuns = vi.fn();
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      rest: {
        actions: {
          getWorkflowRun: async () => response({ workflow_id }),
          listWorkflowRuns,
        },
      },
    };
    await expect(restore(cachePath)).rejects.toThrow("Cannot resolve workflow for daily AIC snapshot restore");
    expect(listWorkflowRuns).not.toHaveBeenCalled();
  });

  it.each([401, 403, 429])("stops fallback fan-out on HTTP %i", async status => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = id => ({ ...current, id, event: "pull_request_target", repository: { full_name: repository } });
    const list = vi.fn(async () => {
      throw apiError(status, {}, "synthetic failure");
    });
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: async () => ({ token: "synthetic" }),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => response({ workflow_runs: [producer(50), producer(51), producer(52)] }),
          listWorkflowRunArtifacts: list,
        },
      },
    };
    await expect(restore(cachePath)).rejects.toMatchObject({ status });
    expect(list).toHaveBeenCalledOnce();
  });

  it.each(["pull_request_target", "issues", "issue_comment"])("restores a trusted %s snapshot behind two newer untrusted PR runs and feeds it to the next authoritative scan", async event => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, id: 50, event, repository: { full_name: repository }, head_repository: { full_name: repository }, head_branch: "main" };
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: async () => ({ token: "synthetic" }),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: vi.fn(async ({ per_page }) => response({ workflow_runs: [{ ...producer, id: 99 }, { ...producer, id: 52, event: "pull_request" }, { ...producer, id: 51, event: "pull_request" }, producer].slice(0, per_page) })),
          listWorkflowRunsForRepo: vi.fn(),
          listWorkflowRunArtifacts: vi.fn(async () => response({ artifacts: [{ id: 100, name: "aic-usage-scan-v2", expired: false }] })),
        },
      },
    };
    const downloadArtifact = vi.fn(async (_id, options) => {
      fs.writeFileSync(path.join(options.path, path.basename(AIC_SCAN_CACHE_FILE_PATH)), [1, 2, 3].map(id => JSON.stringify(scanCacheEntry(run(id), id === 2 ? 0 : id * 2, repository, 7, now))).join("\n"));
      return { downloadPath: options.path };
    });
    // An old prefix cache may exist; it does not suppress the verified artifact.
    writeEntries([{ run_id: 1, aic: 1 }]);
    await restore(cachePath, { createArtifactClient: () => ({ downloadArtifact }) });
    expect(global.github.rest.actions.listWorkflowRuns).toHaveBeenCalledWith({ owner: "example", repo: "project", workflow_id: 7, status: "completed", created: ">=2025-02-01T12:00:00.000Z", per_page: 10 });
    expect(global.github.rest.actions.listWorkflowRunArtifacts).toHaveBeenCalledOnce();
    expect(global.github.rest.actions.listWorkflowRunArtifacts).toHaveBeenCalledWith({ owner: "example", repo: "project", run_id: 50, per_page: 100 });
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Querying workflow-specific run history"));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Workflow-specific run history returned candidates"));
    const f = fixture();
    expect((await scanDailyAIC(f)).cacheHits).toBe(3);
    expect(f.getRunAIC).not.toHaveBeenCalled();
    expect(downloadArtifact).toHaveBeenCalledOnce();
    expect(global.github.rest.actions.listWorkflowRunsForRepo).not.toHaveBeenCalled();
    const producerLogs = global.core.info.mock.calls.filter(([message]) => message.startsWith("[daily-aic-cache] Evaluated snapshot producer:")).map(([message]) => JSON.parse(message.slice(message.indexOf("{"))));
    expect(producerLogs).toEqual([
      expect.objectContaining({ producerRunId: 99, isCurrentRun: true, trusted: true }),
      expect.objectContaining({ producerRunId: 52, isCurrentRun: false, trusted: false }),
      expect.objectContaining({ producerRunId: 51, isCurrentRun: false, trusted: false }),
      expect.objectContaining({ producerRunId: 50, isCurrentRun: false, trusted: true, defaultBranchMatches: true, headRepositoryMatches: true }),
    ]);
  });

  it.each([
    ["absent", "no_unexpired_scan_artifact"],
    ["expired", "no_unexpired_scan_artifact"],
    ["missing-file", "missing_snapshot_file"],
    ["empty", "no_valid_observations"],
  ])("logs why a trusted %s snapshot cannot be restored", async (kind, reason) => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, id: 50, event: "issues", repository: { full_name: repository }, head_repository: { full_name: repository }, head_branch: "main" };
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: async () => ({ token: "sensitive-test-token" }),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => response({ workflow_runs: [producer] }),
          listWorkflowRunArtifacts: async () => response({ artifacts: kind === "absent" ? [] : [{ id: 100, name: "aic-usage-scan-v2", expired: kind === "expired" }] }),
        },
      },
    };
    await restore(cachePath, {
      createArtifactClient: () => ({
        downloadArtifact: async (_id, options) => {
          if (kind === "empty") fs.writeFileSync(path.join(options.path, path.basename(AIC_SCAN_CACHE_FILE_PATH)), "");
          return { downloadPath: options.path };
        },
      }),
    });
    const logs = global.core.info.mock.calls.map(([message]) => message).join("\n");
    expect(logs).toContain(`"reason":"${reason}"`);
    expect(logs).not.toContain("sensitive-test-token");
    expect(fs.existsSync(cachePath)).toBe(false);
  });

  it("rejects contributor workflow artifacts and non-default-branch dispatch snapshots", () => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, repository: { full_name: repository }, head_repository: { full_name: repository }, head_branch: "main" };
    expect(isTrustedProducer({ ...producer, event: "pull_request" }, current, repository, "main")).toBe(false);
    expect(isTrustedProducer({ ...producer, event: "workflow_dispatch", head_branch: "topic" }, current, repository, "main")).toBe(false);
    expect(isTrustedProducer({ ...producer, event: "workflow_dispatch" }, current, repository, "main")).toBe(true);
  });

  it.each(["issues", "issue_comment"])("requires complete default-branch provenance for %s producers", event => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, event, repository: { full_name: repository }, head_repository: { full_name: repository }, head_branch: "main" };
    expect(isTrustedProducer(producer, current, repository, "main")).toBe(true);
    for (const overrides of [{ workflow_id: 8 }, { path: "other.yml" }, { repository: { full_name: "other/project" } }, { head_repository: { full_name: "fork/project" } }, { head_repository: undefined }, { head_branch: "topic" }]) {
      expect(isTrustedProducer({ ...producer, ...overrides }, current, repository, "main")).toBe(false);
    }
    expect(isTrustedProducer(producer, current, repository, "")).toBe(false);
  });
});
