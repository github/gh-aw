import { describe, expect, it, vi } from "vitest";

const fs = await import("node:fs");
const { runDispatchWorkCoordinatorActivation } = await import("./dispatch_work_coordinator_activation.cjs");

describe("Dispatch Work Coordinator activation", () => {
  it("writes only a persisted effective Claim as trusted output", async () => {
    const output = `/tmp/dispatch-work-activation-${process.pid}`;
    const env = {
      GITHUB_REPOSITORY: "owner/repo",
      GITHUB_WORKFLOW_REF: "owner/repo/.github/workflows/worker.yml@refs/heads/main",
      GITHUB_RUN_ID: "123",
      GITHUB_OUTPUT: output,
      GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN: "test-token",
      GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA: JSON.stringify({ type: "object" }),
    };
    const originalClient = global.fetch;
    global.fetch = vi.fn(async () => ({ ok: false, status: 404 }));
    try {
      const result = await runDispatchWorkCoordinatorActivation(env);
      expect(result).toBeNull();
      expect(fs.readFileSync(output, "utf8")).toBe("assignment=null\n");
    } finally {
      global.fetch = originalClient;
      fs.rmSync(output, { force: true });
    }
  });

  it("fails closed when trusted run configuration is missing", async () => {
    await expect(runDispatchWorkCoordinatorActivation({})).rejects.toThrow("configuration is incomplete");
  });

  it("requires a persisted assignment for worker-role activation", async () => {
    const output = `/tmp/dispatch-work-required-activation-${process.pid}`;
    const env = {
      GITHUB_REPOSITORY: "owner/repo",
      GITHUB_WORKFLOW_REF: "owner/repo/.github/workflows/worker.yml@refs/heads/main",
      GITHUB_RUN_ID: "123",
      GITHUB_OUTPUT: output,
      GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN: "test-token",
      GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA: JSON.stringify({ type: "object" }),
      GH_AW_DISPATCH_WORK_COORDINATOR_ID: "shared-queue",
      GH_AW_DISPATCH_WORK_COORDINATOR_REQUIRE_ASSIGNMENT: "true",
    };
    const originalFetch = global.fetch;
    global.fetch = vi.fn(async () => ({ ok: false, status: 404 }));
    try {
      await expect(runDispatchWorkCoordinatorActivation(env)).rejects.toThrow("no available assignment");
      expect(fs.existsSync(output)).toBe(false);
    } finally {
      global.fetch = originalFetch;
      fs.rmSync(output, { force: true });
    }
  });
});
