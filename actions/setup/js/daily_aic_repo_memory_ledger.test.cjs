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
    vi.restoreAllMocks();
    delete global.core;
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("streams repo-memory ledger buckets and recovers after corrupt large records", () => {
    const repoMemoryDir = path.join(directory, "memory");
    const root = ledger.ledgerRoot(repoMemoryDir);
    fs.mkdirSync(root, { recursive: true });
    const entry = { version: 1, repository: "owner/repo", workflow_id: "triage", run_id: 1, aic: 7, timestamp: new Date(now).toISOString() };
    fs.writeFileSync(path.join(root, "2026-09-23.jsonl"), "noise\n".repeat(20000) + '{"broken":"' + "x".repeat(2 * 1024 * 1024) + '"}x\n' + JSON.stringify(entry));
    const read = vi.spyOn(fs, "readFileSync");
    const readChunk = vi.spyOn(fs, "readSync");
    const close = vi.spyOn(fs, "closeSync");
    expect(ledger.readLedgerEntries({ repoMemoryDir, repository: "owner/repo", workflowId: "triage", now })).toEqual([entry]);
    expect(read).not.toHaveBeenCalled();
    expect(readChunk.mock.calls.length).toBeGreaterThan(2);
    expect(close).toHaveBeenCalledTimes(1);
  });

  it("surfaces unreadable repo-memory ledgers instead of undercounting usage", () => {
    vi.spyOn(fs, "openSync").mockImplementation(() => {
      throw Object.assign(new Error("Permission denied"), { code: "EACCES" });
    });
    expect(() => ledger.readLedgerEntries({ repoMemoryDir: directory, repository: "owner/repo", workflowId: "triage", now })).toThrow("Failed to read daily AIC repo-memory ledger");
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
    expect(ledger.readLedgerCoverageStart({ repoMemoryDir, repository: "owner/repo", workflowId: "triage", now })).toBe(now);
  });

  it("skips unpriced usage instead of recording it as free", () => {
    const repoMemoryDir = path.join(directory, "memory");
    const usageDir = path.join(directory, "usage");
    fs.mkdirSync(usageDir, { recursive: true });
    fs.writeFileSync(path.join(usageDir, "agent_usage.jsonl"), JSON.stringify({ provider: "unknown", model: "unknown", input_tokens: 100 }), "utf8");
    vi.stubEnv("GITHUB_RUN_ID", "42");
    vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
    vi.stubEnv("GH_AW_WORKFLOW_ID", "triage");

    ledger.appendCurrentRunLedgerEntry({ repoMemoryDir, usageRoot: usageDir, now });

    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("usage is unpriced"));
    expect(fs.existsSync(ledger.ledgerRoot(repoMemoryDir))).toBe(false);
  });

  it("keeps coverage starts isolated per repository and workflow", () => {
    expect(ledger.ledgerCoveragePath(directory, "owner/repo", "triage")).not.toBe(ledger.ledgerCoveragePath(directory, "other/repo", "triage"));
    expect(ledger.ledgerCoveragePath(directory, "owner/repo", "triage")).not.toBe(ledger.ledgerCoveragePath(directory, "owner/repo", "other"));
    expect(path.dirname(ledger.ledgerCoveragePath(directory, "owner/repo", "../triage"))).toBe(ledger.ledgerRoot(directory));
  });

  it.each(["missing", "malformed", "future", "wrong_identity"])("does not accept %s coverage metadata", kind => {
    const file = ledger.ledgerCoveragePath(directory, "owner/repo", "triage");
    fs.mkdirSync(ledger.ledgerRoot(directory), { recursive: true });
    if (kind !== "missing") {
      fs.writeFileSync(
        file,
        kind === "malformed" ? "{" : JSON.stringify({ version: 1, repository: kind === "wrong_identity" ? "other/repo" : "owner/repo", workflow_id: "triage", initialized_at: new Date(now + (kind === "future" ? 1 : -1)).toISOString() })
      );
    }
    expect(ledger.readLedgerCoverageStart({ repoMemoryDir: directory, repository: "owner/repo", workflowId: "triage", now })).toBeNull();
  });

  it("surfaces unreadable coverage metadata instead of authorizing ledger-only accounting", () => {
    vi.spyOn(fs, "readFileSync").mockImplementation(() => {
      throw Object.assign(new Error("Permission denied"), { code: "EACCES" });
    });
    expect(() => ledger.readLedgerCoverageStart({ repoMemoryDir: directory, repository: "owner/repo", workflowId: "triage", now })).toThrow("Failed to read daily AIC repo-memory coverage");
  });

  it("preserves the original trusted writer coverage start across later appends", () => {
    vi.stubEnv("GITHUB_RUN_ID", "42");
    vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
    vi.stubEnv("GH_AW_WORKFLOW_ID", "triage");
    const options = { repoMemoryDir: directory, usageRoot: path.join(directory, "usage"), now };
    try {
      ledger.appendCurrentRunLedgerEntry(options);
      ledger.appendCurrentRunLedgerEntry({ ...options, now: now + ledger.WINDOW_MS });
      expect(ledger.readLedgerCoverageStart({ repoMemoryDir: directory, repository: "owner/repo", workflowId: "triage", now: now + ledger.WINDOW_MS })).toBe(now);
    } finally {
      vi.unstubAllEnvs();
    }
  });
});
