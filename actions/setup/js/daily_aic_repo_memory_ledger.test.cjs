import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let ledger;
let directory;
const now = Date.parse("2026-09-23T13:02:21.000Z");

describe("daily_aic_repo_memory_ledger", () => {
  beforeEach(async () => {
    vi.resetModules();
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "daily-aic-ledger-"));
    global.core = { info: vi.fn(), warning: vi.fn() };
    const mod = await import("./daily_aic_repo_memory_ledger.cjs");
    ledger = mod.default || mod;
  });

  afterEach(() => {
    delete global.core;
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("reads current and previous UTC day buckets and filters by repository, workflow, and window", () => {
    const repoMemoryDir = path.join(directory, "memory");
    const root = ledger.ledgerRoot(repoMemoryDir);
    fs.mkdirSync(root, { recursive: true });
    const currentEntry = {
      version: 1,
      repository: "owner/repo",
      workflow_id: "triage",
      run_id: 10,
      run_url: "https://example.test/10",
      actor: "monalisa",
      aic: 5,
      timestamp: "2026-09-23T12:00:00.000Z",
    };
    const previousEntry = { ...currentEntry, run_id: 9, aic: 7, timestamp: "2026-09-22T20:00:00.000Z" };
    const staleEntry = { ...currentEntry, run_id: 8, aic: 99, timestamp: "2026-09-22T12:00:00.000Z" };
    const otherActor = { ...currentEntry, run_id: 11, actor: "octocat", aic: 99 };
    fs.writeFileSync(path.join(root, "2026-09-23.jsonl"), [JSON.stringify(currentEntry), JSON.stringify(otherActor)].join("\n"), "utf8");
    fs.writeFileSync(path.join(root, "2026-09-22.jsonl"), [JSON.stringify(previousEntry), JSON.stringify(staleEntry)].join("\n"), "utf8");

    expect(ledger.readLedgerEntries({ repoMemoryDir, repository: "owner/repo", workflowId: "triage", now })).toEqual([currentEntry, otherActor, previousEntry]);
  });

  it("appends current run AIC to the current UTC bucket", () => {
    const repoMemoryDir = path.join(directory, "memory");
    const rawUsageDir = path.join(directory, "raw");
    const usageDir = path.join(directory, "usage");
    fs.mkdirSync(rawUsageDir, { recursive: true });
    fs.mkdirSync(usageDir, { recursive: true });
    fs.writeFileSync(path.join(rawUsageDir, "agent_usage.jsonl"), `${JSON.stringify({ aic: 100 })}\n`, "utf8");
    fs.writeFileSync(path.join(usageDir, "agent_usage.jsonl"), `${JSON.stringify({ aic: 3 })}\n${JSON.stringify({ usage: { aic: 4 } })}\n`, "utf8");
    process.env.GITHUB_RUN_ID = "42";
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GH_AW_WORKFLOW_ID = "triage";
    process.env.GITHUB_ACTOR = "monalisa";
    delete process.env.GITHUB_TRIGGERING_ACTOR;
    process.env.GH_AW_RUN_URL = "https://example.test/runs/42";
    try {
      ledger.appendCurrentRunLedgerEntry({ repoMemoryDir, usageRoot: usageDir, now });
    } finally {
      delete process.env.GITHUB_RUN_ID;
      delete process.env.GITHUB_REPOSITORY;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GITHUB_ACTOR;
      delete process.env.GH_AW_RUN_URL;
    }

    const content = fs.readFileSync(path.join(ledger.ledgerRoot(repoMemoryDir), "2026-09-23.jsonl"), "utf8").trim();
    expect(JSON.parse(content)).toMatchObject({
      version: 1,
      repository: "owner/repo",
      workflow_id: "triage",
      run_id: 42,
      run_url: "https://example.test/runs/42",
      actor: "monalisa",
      aic: 7,
      timestamp: "2026-09-23T13:02:21.000Z",
    });
  });
});
