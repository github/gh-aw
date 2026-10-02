import { createRequire } from "node:module";
import { beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);

let dispatch;

beforeEach(() => {
  delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  dispatch = vi.fn().mockResolvedValue({});
  global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() } };
  global.context = { repo: { owner: "octo", repo: "repo" }, payload: { repository: { default_branch: "main" } } };
  global.github = { rest: { actions: { createWorkflowDispatch: dispatch }, repos: { get: vi.fn().mockResolvedValue({ data: { default_branch: "trunk" } }) } } };
});

const { main } = require("./ledger_request_compaction.cjs");

describe("ledger_request_compaction", () => {
  it("dispatches Agentic Maintenance with a targeted compaction request", async () => {
    const handler = await main({ ledgers: ["findings"], max: 1 });
    const result = await handler({ type: "ledger_request_compaction", reason: "many small segments" });
    expect(result).toMatchObject({ success: true, ledger: "findings", workflow: "agentics-maintenance.yml" });
    expect(dispatch).toHaveBeenCalledWith({ owner: "octo", repo: "repo", workflow_id: "agentics-maintenance.yml", ref: "refs/heads/main", inputs: { operation: "compact_ledger", ledger: "findings" } });
  });

  it("requires a configured compaction-enabled ledger", async () => {
    const handler = await main({ ledgers: ["findings", "metrics"], max: 2 });
    const missing = await handler({});
    expect(missing.success).toBe(false);
    expect(missing.error).toBe("ledger is required when more than one compaction-enabled ledger is configured (findings, metrics)");
    expect((await handler({ ledger: "secrets" })).success).toBe(false);
    expect(dispatch).not.toHaveBeenCalled();
    expect((await handler({ ledger: "metrics" })).success).toBe(true);
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it("deduplicates requests and enforces the max count", async () => {
    const handler = await main({ ledgers: ["findings", "metrics"], max: 1 });
    expect((await handler({ ledger: "findings" })).success).toBe(true);
    expect(await handler({ ledger: "findings" })).toMatchObject({ success: true, skipped: true });
    expect((await handler({ ledger: "metrics" })).success).toBe(false);
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it("ignores an untrusted workflow override and does not dispatch in staged mode", async () => {
    const handler = await main({ ledgers: ["findings"], workflow: "../evil.yml", staged: true });
    expect(await handler({})).toMatchObject({ success: true, staged: true });
    expect(dispatch).not.toHaveBeenCalled();
  });

  it("reports dispatch failures without throwing", async () => {
    dispatch.mockRejectedValueOnce(new Error("Service unavailable"));
    const handler = await main({ ledgers: ["findings"] });
    const result = await handler({});
    expect(result.success).toBe(false);
    expect(result.error).toContain("Service unavailable");
  });

  it("rejects configuration without compaction-enabled ledgers", async () => {
    await expect(main({ ledgers: [] })).rejects.toThrow("E001: ledger_request_compaction has no compaction-enabled ledgers");
  });
});
