// @ts-check
import { describe, expect, it, vi } from "vitest";
import { installPiRecovery } from "./pi_recovery_extension.cjs";
import piToolPolicy from "./pi_tool_policy.cjs";

function harness(overrides = {}) {
  const handlers = {};
  const pi = {
    on: (name, handler) => {
      handlers[name] = handler;
    },
    sendUserMessage: vi.fn(),
  };
  const fetchImpl = vi.fn(
    async (_url, options) =>
      new Response(
        options?.method === "POST"
          ? JSON.stringify({ answers: [{ name: "recovery", type: "choice", choice: "change_strategy" }] })
          : JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] })
      )
  );
  const options = { remainingMs: () => 600000, minimumRemainingMs: 300000, env: { GH_AW_HARNESS_RECOVERY: "1", AWF_REFLECT_ENABLED: "1" }, fetchImpl, log: vi.fn(), ...overrides };
  installPiRecovery(pi, options);
  return { handlers, pi, fetchImpl, options };
}
function turn(results = Array.from({ length: 3 }, () => ({ toolName: "bash", isError: true, content: [{ text: "PRIVATE tool output" }] })), stopReason = "toolUse") {
  return { message: { stopReason, content: "PRIVATE prompt" }, toolResults: results };
}

describe("Pi Recovery adapter", () => {
  it.each([{}, { GH_AW_HARNESS_RECOVERY: "true" }, { GH_AW_HARNESS_RECOVERY: "1", GH_AW_PI_SUBAGENT_CHILD: "1" }, { GH_AW_HARNESS_RECOVERY: "1", GH_AW_PHASE: "detection" }])("installs no hooks without an eligible opt-in: %j", env => {
    const { handlers, fetchImpl } = harness({ env });
    expect(handlers).toEqual({});
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("delivers a native steering message after repeated failures without forwarding raw evidence", async () => {
    const { handlers, pi, fetchImpl } = harness();
    await handlers.turn_end(turn());
    expect(pi.sendUserMessage).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("Repeated tool failures"), { deliverAs: "steer" });
    expect(JSON.stringify(fetchImpl.mock.calls)).not.toContain("PRIVATE");
  });

  it("recognizes nonzero native exit codes even when isError is false", async () => {
    const { handlers, pi } = harness();
    await handlers.turn_end(turn(Array.from({ length: 3 }, () => ({ toolName: "bash", isError: false, details: { exitCode: 2 } }))));
    expect(pi.sendUserMessage).toHaveBeenCalledOnce();
  });

  it("does not count missing outcomes as failures or preserve a stale failure streak", async () => {
    const { handlers, fetchImpl } = harness();
    const failed = { toolName: "bash", isError: true };
    await handlers.turn_end(turn([failed, failed]));
    await handlers.turn_end(turn([{ toolName: "bash" }, failed]));
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["stop", "error", "aborted", undefined])("does not revive a terminal turn with stopReason %s", async stopReason => {
    const { handlers, pi, fetchImpl } = harness();
    const event = turn();
    event.message.stopReason = stopReason;
    await handlers.turn_end(event);
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
  });

  it.each([undefined, 305000, 300000, 0, NaN])("does not evaluate outside the active time budget: %s", async remaining => {
    const { handlers, fetchImpl } = harness({ remainingMs: () => remaining });
    await handlers.turn_end(turn());
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("cannot revive an execution stopped by the existing Pi tool-denial guard", async () => {
    const previousExitCode = process.exitCode;
    vi.stubEnv("GH_AW_PI_TOOL_POLICY", '{"bash":false}');
    vi.stubEnv("GH_AW_MAX_TOOL_DENIALS", "1");
    vi.stubEnv("GH_AW_MAX_TOOL_CALLS", "");
    vi.stubEnv("GH_AW_PI_TOOL_BUDGET_DIR", "");
    const stderr = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    try {
      const { handlers, pi, fetchImpl } = harness();
      piToolPolicy(pi);
      const ctx = { abort: vi.fn() };
      handlers.tool_call({ toolName: "bash", input: { command: "blocked command" } }, ctx);
      expect(ctx.abort).toHaveBeenCalledOnce();
      expect(process.exitCode).toBe(1);
      await handlers.turn_end(turn());
      expect(fetchImpl).not.toHaveBeenCalled();
      expect(pi.sendUserMessage).not.toHaveBeenCalled();
    } finally {
      process.exitCode = previousExitCode;
      vi.unstubAllEnvs();
      stderr.mockRestore();
    }
  });

  it("suppresses late decision results when timeout steering's completion window is reached", async () => {
    let remaining = 600000;
    const { handlers, pi, options } = harness({ remainingMs: () => remaining });
    options.fetchImpl.mockImplementationOnce(async () => {
      remaining = 300000;
      return new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] }));
    });
    await handlers.turn_end(turn());
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
    expect(options.log).toHaveBeenCalledWith(expect.stringContaining("completion window reached"));
  });

  it("counts failed decisions against the cap instead of retrying the decision model indefinitely", async () => {
    const { handlers, pi, options } = harness();
    options.fetchImpl.mockRejectedValue(new Error("PRIVATE provider diagnostic"));
    for (let index = 0; index < 10; index++) await handlers.turn_end(turn());
    expect(options.fetchImpl).toHaveBeenCalledTimes(2);
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
    expect(JSON.stringify(options.log.mock.calls)).not.toContain("PRIVATE");
  });
});
