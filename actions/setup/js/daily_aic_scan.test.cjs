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
  global.core = { info: vi.fn(), warning: vi.fn() };
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete global.core;
  delete global.github;
  delete global.context;
  fs.rmSync(directory, { recursive: true, force: true });
});

function fixture(runs = [run(1), run(2), run(3)]) {
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
    listPage: vi.fn(async () => ({ response: response({ workflow_runs: runs }), sourceRunCount: runs.length, lookupMode: "workflow_id" })),
    token: "synthetic",
    workflowName: "Example",
    cachePath,
    now,
  };
}

function writeEntries(entries) {
  fs.writeFileSync(cachePath, entries.map(entry => JSON.stringify(entry)).join("\n"));
}

describe("complete daily AIC scan observations", () => {
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
    expect(f.artifactClient.listArtifacts).toHaveBeenCalledTimes(3);
    expect(f.artifactClient.downloadArtifact).toHaveBeenCalledTimes(3);
    f.artifactClient.listArtifacts.mockClear();
    f.artifactClient.downloadArtifact.mockClear();
    f.github.rest.actions.listJobsForWorkflowRun.mockClear();
    expect((await scanDailyAIC(f)).cacheHits).toBe(3);
    expect(f.artifactClient.listArtifacts).not.toHaveBeenCalled();
    expect(f.artifactClient.downloadArtifact).not.toHaveBeenCalled();
    expect(f.github.rest.actions.listJobsForWorkflowRun).not.toHaveBeenCalled();
  });

  it.each(["missing", "metadata-only", "malformed", "unknown-model", "old-attempt", "detection", "evals", "invalid-numeric"])("does not convert %s usage into an under-budget observation", async kind => {
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
    await expect(scanDailyAIC(f)).rejects.toThrow();
    expect(readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now).size).toBe(0);
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

  it.each([401, 403, 429, 500])("stops after the first %i failure and persists only resolved observations", async status => {
    const f = fixture();
    f.getRunAIC.mockResolvedValueOnce(2).mockRejectedValueOnce(apiError(status, {}, "synthetic failure"));
    await expect(scanDailyAIC(f)).rejects.toMatchObject({ status });
    expect(f.getRunAIC).toHaveBeenCalledTimes(2);
    const saved = readScanCache(fs.readFileSync(cachePath, "utf8"), repository, 7, now);
    expect([...saved.keys()]).toEqual([1]);
  });

  it("refuses a truncated listing and never inspects its incomplete candidate set", async () => {
    const f = fixture();
    f.listPage.mockResolvedValue({ response: response({ workflow_runs: [run(1)] }), sourceRunCount: 100, lookupMode: "workflow_id" });
    await expect(scanDailyAIC(f)).rejects.toThrow("complete pagination");
    expect(f.listPage).toHaveBeenCalledTimes(10);
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

  it("restores the complete scan artifact and feeds it to the next authoritative scan", async () => {
    vi.spyOn(Date, "now").mockReturnValue(now);
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, id: 50, event: "pull_request_target", repository: { full_name: repository } };
    global.context = { repo: { owner: "example", repo: "project" }, runId: 99, payload: { repository: { default_branch: "main" } } };
    global.github = {
      auth: async () => ({ token: "synthetic" }),
      rest: {
        actions: {
          getWorkflowRun: async () => response(current),
          listWorkflowRuns: async () => response({ workflow_runs: [producer] }),
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
    const f = fixture();
    expect((await scanDailyAIC(f)).cacheHits).toBe(3);
    expect(f.getRunAIC).not.toHaveBeenCalled();
    expect(downloadArtifact).toHaveBeenCalledOnce();
  });

  it("rejects contributor workflow artifacts and non-default-branch dispatch snapshots", () => {
    const current = { workflow_id: 7, path: ".github/workflows/example.yml" };
    const producer = { ...current, repository: { full_name: repository }, head_repository: { full_name: repository }, head_branch: "main" };
    expect(isTrustedProducer({ ...producer, event: "pull_request" }, current, repository, "main")).toBe(false);
    expect(isTrustedProducer({ ...producer, event: "workflow_dispatch", head_branch: "topic" }, current, repository, "main")).toBe(false);
    expect(isTrustedProducer({ ...producer, event: "workflow_dispatch" }, current, repository, "main")).toBe(true);
  });
});
