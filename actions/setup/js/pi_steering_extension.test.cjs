import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { default: extension, loadSteeringConfig } = await import("./pi_steering_extension.cjs");

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-01T00:00:00Z"));
  vi.stubEnv("GH_AW_TIMEOUT_MINUTES", "10");
  vi.stubEnv("GH_AW_STEERING_TIME_WARNING_MINUTES", "5");
  vi.stubEnv("GH_AW_STEERING_TIME_CRITICAL_MINUTES", "2");
  vi.stubEnv("GH_AW_HARNESS_RECOVERY", "");
  vi.stubEnv("GH_AW_HARNESS_FIREWALL_STEERING", "");
  vi.spyOn(process.stderr, "write").mockReturnValue(true);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});

function harness() {
  const handlers = {};
  const sendUserMessage = vi.fn();
  extension({
    on: (event, handler) => {
      const previous = handlers[event];
      handlers[event] = async (...args) => {
        if (previous) await previous(...args);
        await handler(...args);
      };
    },
    sendUserMessage,
  });
  return { handlers, sendUserMessage };
}

describe("Pi timeout steering", () => {
  it("loads independent Firewall Steering once per session without resetting its clock or evidence cap on agent_start", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "pi-steering-firewall-"));
    vi.stubEnv("GH_AW_TMP_DIR", directory);
    vi.stubEnv("GH_AW_HARNESS_FIREWALL_STEERING", "1");
    const file = path.join(directory, "sandbox/firewall/audit/log.jsonl");
    const denied = '{"_schema":"audit/v0.26.0","host":"PRIVATE.example:443","status":403,"decision":"TCP_DENIED"}\n';
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, denied);
    try {
      const { handlers, sendUserMessage } = harness();
      const event = { message: { stopReason: "toolUse" } };
      await handlers.agent_start();
      await handlers.turn_end(event);
      expect(sendUserMessage).not.toHaveBeenCalled();
      fs.appendFileSync(file, denied);
      await handlers.turn_end(event);
      expect(sendUserMessage).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("firewall denied network access"), { deliverAs: "steer" });
      await handlers.agent_start();
      fs.appendFileSync(file, denied);
      await handlers.turn_end(event);
      expect(sendUserMessage).toHaveBeenCalledOnce();
      vi.advanceTimersByTime(6 * 60000);
      await handlers.agent_start();
      await handlers.turn_end(event);
      expect(sendUserMessage).toHaveBeenLastCalledWith(expect.stringContaining("wrap up"), { deliverAs: "steer" });
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("loads opt-in Recovery through the existing extension without resetting its budget on agent_start", async () => {
    vi.stubEnv("GH_AW_HARNESS_RECOVERY", "1");
    vi.stubEnv("AWF_REFLECT_ENABLED", "1");
    const fetchImpl = vi.fn(
      async (_url, options) =>
        new Response(
          options?.method === "POST"
            ? JSON.stringify({ answers: [{ name: "recovery", type: "choice", choice: "change_strategy" }] })
            : JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] })
        )
    );
    vi.stubGlobal("fetch", fetchImpl);
    try {
      const { handlers, sendUserMessage } = harness();
      const event = { message: { stopReason: "toolUse" }, toolResults: Array.from({ length: 3 }, () => ({ toolName: "bash", isError: true })) };
      await handlers.agent_start();
      for (let index = 0; index < 3; index++) {
        await handlers.turn_end(event);
        await handlers.agent_start();
      }
      expect(fetchImpl).toHaveBeenCalledTimes(4);
      expect(sendUserMessage).toHaveBeenCalledTimes(2);
      vi.advanceTimersByTime(6 * 60000);
      await handlers.turn_end(event);
      expect(fetchImpl).toHaveBeenCalledTimes(4);
      expect(sendUserMessage).toHaveBeenLastCalledWith(expect.stringContaining("wrap up"), { deliverAs: "steer" });
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("uses defaults when environment values are absent or invalid", () => {
    vi.stubEnv("GH_AW_TIMEOUT_MINUTES", "");
    vi.stubEnv("GH_AW_STEERING_TIME_WARNING_MINUTES", "invalid");
    vi.stubEnv("GH_AW_STEERING_TIME_CRITICAL_MINUTES", "");
    expect(loadSteeringConfig()).toEqual({ timeoutMinutes: 30, timeWarningMinutes: 5, timeCriticalMinutes: 2 });
  });

  it("accepts fractional thresholds", () => {
    vi.stubEnv("GH_AW_STEERING_TIME_WARNING_MINUTES", "2.5");
    expect(loadSteeringConfig().timeWarningMinutes).toBe(2.5);
  });

  it("does not steer before the session starts or when plenty of time remains", async () => {
    const { handlers, sendUserMessage } = harness();
    await handlers.turn_end({}, {});
    await handlers.agent_start();
    vi.advanceTimersByTime(60_000);
    await handlers.turn_end({}, {});
    expect(sendUserMessage).not.toHaveBeenCalled();
  });

  it("uses the v1 public API to deliver warning and critical messages exactly once", async () => {
    const { handlers, sendUserMessage } = harness();
    await handlers.agent_start();
    vi.advanceTimersByTime(6 * 60_000);
    await handlers.turn_end({ message: { role: "assistant", stopReason: "toolUse" } }, {});
    await handlers.turn_end({ message: { role: "assistant", stopReason: "toolUse" } }, {});
    expect(sendUserMessage).toHaveBeenCalledTimes(1);
    expect(sendUserMessage).toHaveBeenNthCalledWith(1, expect.stringContaining("wrap up"), { deliverAs: "steer" });
    vi.advanceTimersByTime(3 * 60_000);
    await handlers.turn_end({ message: { role: "assistant", stopReason: "toolUse" } }, {});
    await handlers.turn_end({ message: { role: "assistant", stopReason: "toolUse" } }, {});
    expect(sendUserMessage).toHaveBeenCalledTimes(2);
    expect(sendUserMessage).toHaveBeenNthCalledWith(2, expect.stringContaining("CRITICAL"), { deliverAs: "steer" });
  });

  it("does not reset the wall-clock deadline when Pi retries a low-level agent run", async () => {
    const { handlers, sendUserMessage } = harness();
    await handlers.agent_start();
    vi.advanceTimersByTime(9 * 60_000);
    await handlers.agent_start();
    await handlers.turn_end({ message: { role: "assistant", stopReason: "toolUse" } }, {});
    expect(sendUserMessage).toHaveBeenCalledOnce();
    expect(sendUserMessage).toHaveBeenCalledWith(expect.stringContaining("CRITICAL"), { deliverAs: "steer" });
  });

  it.each(["stop", "error", "aborted", "unknown", undefined])("does not steer terminal assistant turns with stopReason %s", async stopReason => {
    const { handlers, sendUserMessage } = harness();
    await handlers.agent_start();
    vi.advanceTimersByTime(9 * 60_000);
    await handlers.turn_end({ message: { role: "assistant", stopReason } }, {});
    expect(sendUserMessage).not.toHaveBeenCalled();
  });

  it("steers only ongoing tool turns", async () => {
    const { handlers, sendUserMessage } = harness();
    await handlers.agent_start();
    vi.advanceTimersByTime(9 * 60_000);
    await handlers.turn_end({ message: { role: "assistant", stopReason: "toolUse" } }, {});
    expect(sendUserMessage).toHaveBeenCalledOnce();
    expect(sendUserMessage).toHaveBeenCalledWith(expect.stringContaining("CRITICAL"), { deliverAs: "steer" });
  });
});
