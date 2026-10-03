import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { default: extension, loadSteeringConfig } = await import("./pi_steering_extension.cjs");

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-01T00:00:00Z"));
  vi.stubEnv("GH_AW_TIMEOUT_MINUTES", "10");
  vi.stubEnv("GH_AW_STEERING_TIME_WARNING_MINUTES", "5");
  vi.stubEnv("GH_AW_STEERING_TIME_CRITICAL_MINUTES", "2");
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
  extension({ on: (event, handler) => (handlers[event] = handler), sendUserMessage });
  return { handlers, sendUserMessage };
}

describe("Pi timeout steering", () => {
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
    await handlers.turn_end({}, {});
    await handlers.turn_end({}, {});
    expect(sendUserMessage).toHaveBeenCalledTimes(1);
    expect(sendUserMessage).toHaveBeenNthCalledWith(1, expect.stringContaining("wrap up"), { deliverAs: "steer" });
    vi.advanceTimersByTime(3 * 60_000);
    await handlers.turn_end({}, {});
    await handlers.turn_end({}, {});
    expect(sendUserMessage).toHaveBeenCalledTimes(2);
    expect(sendUserMessage).toHaveBeenNthCalledWith(2, expect.stringContaining("CRITICAL"), { deliverAs: "steer" });
  });

  it("does not reset the wall-clock deadline when Pi retries a low-level agent run", async () => {
    const { handlers, sendUserMessage } = harness();
    await handlers.agent_start();
    vi.advanceTimersByTime(9 * 60_000);
    await handlers.agent_start();
    await handlers.turn_end({}, {});
    expect(sendUserMessage).toHaveBeenCalledOnce();
    expect(sendUserMessage).toHaveBeenCalledWith(expect.stringContaining("CRITICAL"), { deliverAs: "steer" });
  });
});
