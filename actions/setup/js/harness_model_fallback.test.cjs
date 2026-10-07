import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { runHarnessModelFallbackLoop } = require("./harness_retry_runner.cjs");
const { isModelFallbackFailure } = require("./model_fallback.cjs");

function options(overrides = {}) {
  return {
    maxRetries: 2,
    initialDelayMs: 10,
    backoffMultiplier: 2,
    maxDelayMs: 100,
    driverStartTime: Date.now(),
    harnessName: "fallback test",
    log: () => {},
    softTimeoutGuard: null,
    fallbackModels: ["secondary", "last"],
    shouldFallback: isModelFallbackFailure,
    runAttempt: async () => ({ exitCode: 1, output: "CAPIError: 503 Service Unavailable", hasOutput: true }),
    handleFailure: () => ({ action: "stop" }),
    switchModel: async () => {},
    sleepFn: async () => {},
    ...overrides,
  };
}

describe("shared model fallback loop", () => {
  it("uses ordered models with independent retry budgets and backoff", async () => {
    let model = "primary";
    const attempts = [];
    const delays = [];
    const run = await runHarnessModelFallbackLoop(
      options({
        runAttempt: async attempt => {
          attempts.push([model, attempt]);
          return { exitCode: model === "last" ? 0 : 1, output: "CAPIError: 503", hasOutput: true };
        },
        switchModel: async next => {
          model = next;
        },
        sleepFn: async delay => {
          delays.push(delay);
        },
      })
    );
    expect(run.exitCode).toBe(0);
    expect(attempts).toEqual([
      ["primary", 0],
      ["primary", 1],
      ["primary", 2],
      ["secondary", 0],
      ["secondary", 1],
      ["secondary", 2],
      ["last", 0],
    ]);
    expect(delays).toEqual([10, 20, 10, 20]);
  });

  it("does not change single-model retry decisions", async () => {
    const run = await runHarnessModelFallbackLoop(options({ fallbackModels: [] }));
    expect(run.attempts).toBe(1);
    expect(run.exitCode).toBe(1);
  });

  it("honors explicit terminal success and replay-safety vetoes", async () => {
    for (const decision of [
      { action: "stop", exitCode: 0 },
      { action: "stop", allowModelFallback: false },
    ]) {
      const run = await runHarnessModelFallbackLoop(
        options({
          handleFailure: () => decision,
          switchModel: async () => {
            throw new Error("must not switch");
          },
        })
      );
      expect(run.attempts).toBe(1);
    }
  });

  it.each([401, 403, 429])("does not switch on HTTP %i", async status => {
    const run = await runHarnessModelFallbackLoop(
      options({
        runAttempt: async () => ({ exitCode: 1, output: JSON.stringify({ type: "session.error", data: { status } }), hasOutput: true }),
        switchModel: async () => {
          throw new Error("must not switch");
        },
      })
    );
    expect(run.attempts).toBe(1);
  });

  it.each(["cancelled", "runtimeGuardFired", "watchdogFired"])("does not switch after %s", async guard => {
    const run = await runHarnessModelFallbackLoop(
      options({
        runAttempt: async () => ({ exitCode: 1, output: "CAPIError: 503", hasOutput: true, [guard]: true }),
        switchModel: async () => {
          throw new Error("must not switch");
        },
      })
    );
    expect(run.exitCode).toBe(1);
  });

  it("does not extend an expired job deadline", async () => {
    const guard = { timeoutMinutes: 1, softDeadlineMs: Date.now() + 10000 };
    const run = await runHarnessModelFallbackLoop(
      options({
        softTimeoutGuard: guard,
        runAttempt: async () => {
          guard.softDeadlineMs = Date.now() - 1;
          return { exitCode: 1, output: "CAPIError: 503", hasOutput: true };
        },
        switchModel: async () => {
          throw new Error("must not switch");
        },
      })
    );
    expect(run.exitCode).toBe(1);
    expect(run.attempts).toBe(1);
  });

  it("preserves failure when all configured models are exhausted", async () => {
    const selections = [];
    const run = await runHarnessModelFallbackLoop(
      options({
        maxRetries: 0,
        switchModel: async model => {
          selections.push(model);
        },
      })
    );
    expect(selections).toEqual(["secondary", "last"]);
    expect(run.exitCode).toBe(1);
  });

  it("surfaces provider configuration failures", async () => {
    await expect(
      runHarnessModelFallbackLoop(
        options({
          maxRetries: 0,
          switchModel: async () => {
            throw new Error("missing provider credentials");
          },
        })
      )
    ).rejects.toThrow("missing provider credentials");
  });
});
