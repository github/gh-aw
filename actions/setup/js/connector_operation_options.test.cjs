// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadHandlers, processMessages } from "./safe_output_handler_manager.cjs";

describe("trusted connector operation budgets", () => {
  beforeEach(() => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "fixture", repo: "fixture" }, payload: { pull_request: { number: 1, state: "open" } } });
    vi.stubGlobal("github", { rest: { pulls: { get: vi.fn().mockResolvedValue({ data: { state: "open" } }) } } });
    for (const name of ["GH_AW_SAFE_OUTPUT_JOBS", "GH_AW_SAFE_OUTPUT_SCRIPTS", "GH_AW_SAFE_OUTPUT_ACTIONS", "GH_AW_DETECTION_CONCLUSION", "GH_AW_DETECTION_REASON", "GH_AW_RUN_DETECTION"]) {
      vi.stubEnv(name, "");
    }
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("uses independent trusted budgets and rejects the third invocation before any handler work", async () => {
    const config = {
      tracker_add_comment: { connector_operation: true, threat_warning_policy: "abort", max: 2, staged: true },
      tracker_update_issue: { connector_operation: true, threat_warning_policy: "abort", max: 1, staged: false },
    };
    const handlers = await loadHandlers(config, {});
    const comment = vi.fn().mockResolvedValue({ staged: true });
    const update = vi.fn().mockResolvedValue({ updated: true });
    handlers.set("tracker_add_comment", comment);
    handlers.set("tracker_update_issue", update);
    const result = await processMessages(handlers, [
      { type: "tracker_add_comment", max: 100, staged: false, config: { max: 100, staged: false } },
      { type: "tracker_update_issue" },
      { type: "tracker_add_comment" },
      { type: "tracker_add_comment", max: 100 },
      { type: "tracker_update_issue" },
    ]);
    expect(comment).toHaveBeenCalledTimes(2);
    expect(update).toHaveBeenCalledOnce();
    expect(result.results.filter(item => item.errorCode === "connector_budget_exceeded")).toHaveLength(2);
    expect(config.tracker_add_comment).toEqual({ connector_operation: true, threat_warning_policy: "abort", max: 2, staged: true });
  });

  it("counts failed attempts and does not reset across dispatch calls", async () => {
    const handlers = await loadHandlers({ tracker_mutate: { connector_operation: true, max: 1 } }, {});
    const handler = vi.fn().mockRejectedValue(new Error("failure before network"));
    handlers.set("tracker_mutate", handler);
    await processMessages(handlers, [{ type: "tracker_mutate" }]);
    const second = await processMessages(handlers, [{ type: "tracker_mutate", max: 100 }]);
    expect(handler).toHaveBeenCalledOnce();
    expect(second.results[0]).toMatchObject({ success: false, errorCode: "connector_budget_exceeded" });
  });

  it("reserves budgets atomically across concurrent dispatch calls", async () => {
    const handlers = await loadHandlers({ tracker_mutate: { connector_operation: true, max: 1 } }, {});
    const handler = vi.fn().mockResolvedValue({});
    handlers.set("tracker_mutate", handler);
    const results = await Promise.all([processMessages(handlers, [{ type: "tracker_mutate" }]), processMessages(handlers, [{ type: "tracker_mutate" }])]);
    expect(handler).toHaveBeenCalledOnce();
    expect(results.flatMap(result => result.results).filter(item => item.errorCode === "connector_budget_exceeded")).toHaveLength(1);
  });

  it("does not charge HTTP retries inside one approved invocation again", async () => {
    const handlers = await loadHandlers({ tracker_mutate: { connector_operation: true, max: 1 } }, {});
    const request = vi.fn().mockResolvedValue({});
    const handler = vi.fn(async () => {
      await request();
      await request();
      await request();
      return {};
    });
    handlers.set("tracker_mutate", handler);
    const result = await processMessages(handlers, [{ type: "tracker_mutate" }]);
    expect(result.results[0].success).toBe(true);
    expect(request).toHaveBeenCalledTimes(3);
    expect(handler).toHaveBeenCalledOnce();
  });

  it("snapshots budgets rather than trusting later config mutations", async () => {
    const options = { connector_operation: true, max: 1 };
    const handlers = await loadHandlers({ tracker_mutate: options }, {});
    options.max = 100;
    const handler = vi.fn().mockResolvedValue({});
    handlers.set("tracker_mutate", handler);
    await processMessages(handlers, [{ type: "tracker_mutate" }, { type: "tracker_mutate" }]);
    expect(handler).toHaveBeenCalledOnce();
  });

  it.each([undefined, null, 0, -1, 101, 1.5, "3", true, Number.NaN])("rejects malformed compiler-owned budget %s", async max => {
    await expect(loadHandlers({ tracker_mutate: { connector_operation: true, max } }, {})).rejects.toThrow("Invalid trusted connector budget");
  });

  it("does not silently change arbitrary custom script behavior", async () => {
    const handlers = await loadHandlers({ arbitrary_script: { max: 1 } }, {});
    const handler = vi.fn().mockResolvedValue({});
    handlers.set("arbitrary_script", handler);
    await processMessages(handlers, [{ type: "arbitrary_script" }, { type: "arbitrary_script" }, { type: "arbitrary_script" }]);
    expect(handler).toHaveBeenCalledTimes(3);
  });

  it("keeps budgets private to each trusted handler map and preserves threat aborts", async () => {
    const config = { tracker_mutate: { connector_operation: true, threat_warning_policy: "abort", max: 1 } };
    const first = await loadHandlers(config, {});
    const second = await loadHandlers(config, {});
    const handler = vi.fn().mockResolvedValue({});
    first.set("tracker_mutate", handler);
    second.set("tracker_mutate", handler);
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "warning");
    await processMessages(first, [{ type: "tracker_mutate", threat_warning_policy: "reviewable" }]);
    expect(handler).not.toHaveBeenCalled();
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "success");
    await processMessages(first, [{ type: "tracker_mutate" }]);
    await processMessages(second, [{ type: "tracker_mutate" }]);
    expect(handler).toHaveBeenCalledTimes(2);
  });
});
