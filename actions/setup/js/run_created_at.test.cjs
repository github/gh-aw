// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";

const { normalizeRunCreatedAt, resolveRunCreatedAtForInfo, resolveRunCreatedAtForGrading } = require("./run_created_at.cjs");

// error_recovery.cjs (withRetry) logs through the github-script `core` global.
const mockCore = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() };
// @ts-ignore - github-script global
global.core = mockCore;

beforeEach(() => {
  vi.clearAllMocks();
});

describe("normalizeRunCreatedAt", () => {
  it("keeps an already normalized UTC timestamp", () => {
    expect(normalizeRunCreatedAt("2026-09-12T20:58:00Z")).toBe("2026-09-12T20:58:00Z");
  });

  it("normalizes offsets and millisecond precision", () => {
    expect(normalizeRunCreatedAt("2026-09-12T22:58:00+02:00")).toBe("2026-09-12T20:58:00Z");
    expect(normalizeRunCreatedAt("2026-09-12T20:58:00.553Z")).toBe("2026-09-12T20:58:00Z");
    expect(normalizeRunCreatedAt("2026-09-12T20:58:00.999Z")).toBe("2026-09-12T20:58:00Z");
  });

  it("reports unusable values as empty", () => {
    expect(normalizeRunCreatedAt("")).toBe("");
    expect(normalizeRunCreatedAt("   ")).toBe("");
    expect(normalizeRunCreatedAt("not-a-timestamp")).toBe("");
    expect(normalizeRunCreatedAt("2026")).toBe("");
    expect(normalizeRunCreatedAt("2026-09-12 20:58:00")).toBe("");
    expect(normalizeRunCreatedAt(null)).toBe("");
    expect(normalizeRunCreatedAt(undefined)).toBe("");
    expect(normalizeRunCreatedAt(1757710680000)).toBe("");
  });
});

describe("resolveRunCreatedAtForGrading", () => {
  it("normalizes the activation output to a UTC ISO timestamp", async () => {
    await expect(resolveRunCreatedAtForGrading({ GH_AW_RUN_CREATED_AT: "2026-09-12T20:58:00.000Z" })).resolves.toEqual({ createdAt: "2026-09-12T20:58:00Z" });
  });

  it("falls back to Actions run metadata when the activation output is missing", async () => {
    const getWorkflowRun = vi.fn().mockResolvedValue({ data: { created_at: "2026-09-12T20:58:00Z" } });
    const resolved = await resolveRunCreatedAtForGrading({ GH_AW_RUN_CREATED_AT: "", GITHUB_REPOSITORY: "githubnext/gh-aw-cao", GITHUB_RUN_ID: "34718229379" }, { rest: { actions: { getWorkflowRun } } });

    expect(getWorkflowRun).toHaveBeenCalledWith({ owner: "githubnext", repo: "gh-aw-cao", run_id: 34718229379 });
    expect(resolved).toEqual({ createdAt: "2026-09-12T20:58:00Z" });
  });

  it("reports an acquisition failure when the fallback lookup fails", async () => {
    const getWorkflowRun = vi.fn().mockRejectedValue(new Error("forbidden"));
    const resolved = await resolveRunCreatedAtForGrading({ GITHUB_REPOSITORY: "githubnext/gh-aw-cao", GITHUB_RUN_ID: "34718229379" }, { rest: { actions: { getWorkflowRun } } });

    expect(resolved.createdAt).toBe("");
    expect(resolved.error).toContain("forbidden");
  });

  it("reports an acquisition failure when no lookup is possible", async () => {
    const resolved = await resolveRunCreatedAtForGrading({ GITHUB_REPOSITORY: "", GITHUB_RUN_ID: "" }, null);

    expect(resolved.createdAt).toBe("");
    expect(resolved.error).toContain("unavailable");
  });
});

describe("resolveRunCreatedAtForInfo", () => {
  it("normalizes the workflow run creation time", async () => {
    const getWorkflowRun = vi.fn().mockResolvedValue({ data: { created_at: "2026-09-12T22:58:00+02:00" } });
    const resolved = await resolveRunCreatedAtForInfo(mockCore, { repo: { owner: "githubnext", repo: "gh-aw-cao" }, runId: 34718229379 }, { rest: { actions: { getWorkflowRun } } });

    expect(resolved).toBe("2026-09-12T20:58:00Z");
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("warns and returns an empty value when no client is available", async () => {
    const resolved = await resolveRunCreatedAtForInfo(mockCore, { repo: { owner: "githubnext", repo: "gh-aw-cao" }, runId: 1 }, null);

    expect(resolved).toBe("");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("no authenticated GitHub client"));
  });

  it("warns and returns an empty value when the lookup fails permanently", async () => {
    const getWorkflowRun = vi.fn().mockRejectedValue(Object.assign(new Error("forbidden"), { status: 403 }));
    const resolved = await resolveRunCreatedAtForInfo(mockCore, { repo: { owner: "githubnext", repo: "gh-aw-cao" }, runId: 1 }, { rest: { actions: { getWorkflowRun } } });

    expect(resolved).toBe("");
    expect(getWorkflowRun).toHaveBeenCalledTimes(1);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("forbidden"));
  });
});
