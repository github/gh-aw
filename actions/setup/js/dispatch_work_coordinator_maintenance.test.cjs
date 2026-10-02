import { describe, expect, it, vi } from "vitest";
import { main, parseMaintenanceConfig } from "./dispatch_work_coordinator_maintenance.cjs";

describe("Dispatch Work Coordinator maintenance", () => {
  it("accepts only a workflow identity and object schema", () => {
    expect(
      parseMaintenanceConfig(
        JSON.stringify({
          identity: ".github/workflows/worker.lock.yml",
          schema: { type: "object", properties: { title: { type: "string" } } },
        })
      )
    ).toEqual({
      identity: ".github/workflows/worker.lock.yml",
      schema: { type: "object", properties: { title: { type: "string" } } },
    });
    expect(() =>
      parseMaintenanceConfig(
        JSON.stringify({
          identity: ".github/workflows/../worker.lock.yml",
          schema: { type: "object" },
        })
      )
    ).toThrow("configuration is invalid");
  });

  it("compacts safely when the coordinator branch has not been initialized", async () => {
    const originalFetch = global.fetch;
    global.fetch = vi.fn(async () => ({ ok: false, status: 404 }));
    try {
      const result = await main({
        GITHUB_REPOSITORY: "owner/repo",
        GITHUB_RUN_ID: "123",
        GITHUB_WORKFLOW_REF: "owner/repo/.github/workflows/agentics-maintenance.yml@refs/heads/main",
        GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN: "test-token",
        GH_AW_DISPATCH_WORK_COORDINATOR_CONFIG: JSON.stringify({
          identity: ".github/workflows/worker.lock.yml",
          schema: { type: "object" },
        }),
      });
      expect(result.recovered_claims).toEqual([]);
      expect(result.projection.counts.outstanding).toBe(0);
      expect(global.fetch).toHaveBeenCalledTimes(2);
    } finally {
      global.fetch = originalFetch;
    }
  });

  it("fails closed for missing trusted maintenance configuration", async () => {
    await expect(main({})).rejects.toThrow("maintenance configuration is incomplete");
  });
});
