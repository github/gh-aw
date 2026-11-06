// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadHandlers, processMessages } from "./safe_output_handler_manager.cjs";

describe("connector threat-warning policy", () => {
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

  async function configuredHandler(type) {
    const handlers = await loadHandlers({ [type]: { threat_warning_policy: "abort" } }, {});
    const handler = vi.fn().mockResolvedValue({ success: true });
    handlers.set(type, handler);
    return { handlers, handler };
  }

  it.each(["jira_add_label", "linear_update_issue", "sentry_resolve_issue", "custom_alias_mutate"])("blocks %s before invoking its credentialed handler", async type => {
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "warning");
    vi.stubEnv("GH_AW_DETECTION_REASON", "threat_detected");
    const { handlers, handler } = await configuredHandler(type);
    const result = await processMessages(handlers, [{ type, threat_warning_policy: "reviewable", staged: false }]);
    expect(handler).not.toHaveBeenCalled();
    expect(result.results[0]).toMatchObject({
      type,
      success: false,
      cancelled: true,
      threatDetected: true,
      errorCode: "threat_detected_abort_policy",
    });
  });

  it.each(["agent_failure", "parse_error"])("also blocks warning-normalized detection failures: %s", async reason => {
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "failure");
    vi.stubEnv("GH_AW_DETECTION_REASON", reason);
    const { handlers, handler } = await configuredHandler("custom_alias_mutate");
    const result = await processMessages(handlers, [{ type: "custom_alias_mutate" }]);
    expect(handler).not.toHaveBeenCalled();
    expect(result.results[0]).toMatchObject({ cancelled: true, errorCode: "threat_detected_abort_policy" });
  });

  it.each(["success", "skipped", ""])("preserves normal execution with detection conclusion %j", async conclusion => {
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", conclusion);
    const { handlers, handler } = await configuredHandler("custom_alias_mutate");
    const result = await processMessages(handlers, [{ type: "custom_alias_mutate" }]);
    expect(handler).toHaveBeenCalledOnce();
    expect(result.results[0].success).toBe(true);
  });

  it("keeps classifications private to each trusted handler map", async () => {
    vi.stubEnv("GH_AW_DETECTION_CONCLUSION", "warning");
    const { handlers, handler } = await configuredHandler("custom_alias_mutate");
    const customHandler = vi.fn().mockResolvedValue({ success: true });
    const unrelated = await loadHandlers({}, {});
    unrelated.set("custom_alias_mutate", customHandler);
    await processMessages(handlers, [{ type: "custom_alias_mutate" }]);
    await processMessages(unrelated, [{ type: "custom_alias_mutate" }]);
    expect(handler).not.toHaveBeenCalled();
    expect(customHandler).toHaveBeenCalledOnce();
  });
});
