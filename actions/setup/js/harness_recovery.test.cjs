// @ts-check
import { describe, expect, it, vi } from "vitest";
import { createRecoveryPolicy, evaluateRecoveryDecision } from "./harness_recovery.cjs";

const context = { engine: "pi", capabilities: ["steer"] };
const failed = { toolName: "bash", success: false };
function turn(tools = [failed, failed, failed], overrides = {}) {
  return { type: "turn.completed", ongoing: true, remainingMs: 600000, tools, ...overrides };
}
function policyOptions(overrides = {}) {
  return { decide: vi.fn().mockResolvedValue("change_strategy"), log: vi.fn(), minimumRemainingMs: 300000, ...overrides };
}
function endpoint() {
  return new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] }));
}
function answer(choice) {
  return new Response(JSON.stringify({ answers: [{ name: "recovery", type: "choice", choice }] }));
}

describe("shared Recovery policy", () => {
  it("detects repeated tool failures across turns and sends only bounded numeric evidence", async () => {
    const options = policyOptions();
    const policy = createRecoveryPolicy(options);
    expect(await policy.onEvent(turn([failed]), context)).toBeUndefined();
    expect(await policy.onEvent(turn([failed]), context)).toBeUndefined();
    const actions = await policy.onEvent(turn([failed]), context);
    expect(options.decide).toHaveBeenCalledExactlyOnceWith({ consecutiveFailures: 3, decision: 1, remainingMs: 600000 });
    expect(actions).toEqual([{ type: "steer", message: expect.stringContaining("never bypass a denial") }]);
  });

  it.each([{ toolName: "bash", success: true }, { toolName: "bash" }, { toolName: "read", success: false }])("breaks the streak on success, unknown outcomes or a different tool: %j", async separator => {
    const options = policyOptions();
    const policy = createRecoveryPolicy(options);
    await policy.onEvent(turn([failed, failed, separator, failed]), context);
    expect(options.decide).not.toHaveBeenCalled();
  });

  it.each(["continue", "unknown", "change_strategy"])("requires fresh failures and caps %s decisions at two per session", async choice => {
    const options = policyOptions({ decide: vi.fn().mockResolvedValue(choice) });
    const policy = createRecoveryPolicy(options);
    for (let index = 0; index < 20; index++) {
      const actions = await policy.onEvent(turn(), context);
      if (index < 2) expect(actions).toHaveLength(choice === "change_strategy" ? 1 : 0);
    }
    expect(options.decide).toHaveBeenCalledTimes(2);
  });

  it.each([{ ongoing: false }, { remainingMs: 300000 }, { remainingMs: -1 }, { remainingMs: NaN }, { remainingMs: Infinity }])("does not evaluate terminal or budget-limited turns: %j", async overrides => {
    const options = policyOptions();
    await createRecoveryPolicy(options).onEvent(turn(undefined, overrides), context);
    expect(options.decide).not.toHaveBeenCalled();
  });

  it("ignores attempt-level events and adapters without steering", async () => {
    const options = policyOptions();
    const policy = createRecoveryPolicy(options);
    await policy.onEvent({ type: "attempt.completed", failureClasses: [] }, context);
    await policy.onEvent(turn(), { engine: "codex", capabilities: [] });
    expect(options.decide).not.toHaveBeenCalled();
  });

  it("does not re-evaluate without new failures or overlap an in-flight decision", async () => {
    let resolve;
    const options = policyOptions({
      decide: vi.fn(
        () =>
          new Promise(done => {
            resolve = done;
          })
      ),
    });
    const policy = createRecoveryPolicy(options);
    const pending = policy.onEvent(turn(), context);
    await policy.onEvent(turn(), context);
    expect(options.decide).toHaveBeenCalledOnce();
    resolve("unknown");
    await pending;
    await policy.onEvent(turn([]), context);
    await policy.onEvent(turn([failed, failed]), context);
    expect(options.decide).toHaveBeenCalledOnce();
  });
});

describe("Recovery Decisions integration", () => {
  const evidence = { consecutiveFailures: 3, decision: 1, remainingMs: 600000 };
  const env = { AWF_REFLECT_ENABLED: "1" };

  it.each(["continue", "change_strategy", "unknown"])("uses the existing proxy for a %s decision without transmitting private evidence", async choice => {
    const fetchImpl = vi.fn().mockResolvedValueOnce(endpoint()).mockResolvedValueOnce(answer(choice));
    const log = vi.fn();
    expect(await evaluateRecoveryDecision({ ...evidence, output: "PRIVATE", toolName: "SECRET" }, { env, fetchImpl, log })).toBe(choice);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(fetchImpl.mock.calls[0][1].redirect).toBe("error");
    const [url, options] = fetchImpl.mock.calls[1];
    expect(url).toBe("http://api-proxy:10002/v1/decisions");
    expect(options.headers["Copilot-Integration-Id"]).toBe("agentic-workflows");
    expect(JSON.parse(options.body).questions[0].instructions).toEqual([{ type: "input_json", data: evidence }]);
    expect(options.body).not.toMatch(/PRIVATE|SECRET/);
  });

  it("does not make requests without the sandbox proxy", async () => {
    const fetchImpl = vi.fn();
    const log = vi.fn();
    expect(await evaluateRecoveryDecision(evidence, { env: {}, fetchImpl, log })).toBe("unknown");
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(log).toHaveBeenCalledWith(expect.stringContaining("sandbox_unavailable"));
  });

  it.each([
    () => new Response("PRIVATE", { status: 500 }),
    () => answer("grant_permissions"),
    () => new Response(JSON.stringify({ answers: [{ name: "recovery", type: "refusal" }] })),
    () => Promise.reject(new Error("PRIVATE credentials")),
    () => Promise.reject(new DOMException("PRIVATE credentials", "TimeoutError")),
  ])("abstains without retrying or disclosing failed decision content", async response => {
    const fetchImpl = vi.fn().mockResolvedValueOnce(endpoint()).mockImplementationOnce(response);
    const log = vi.fn();
    expect(await evaluateRecoveryDecision(evidence, { env, fetchImpl, log })).toBe("unknown");
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(JSON.stringify(log.mock.calls)).not.toContain("PRIVATE");
  });

  it("abstains before requesting a decision if reflection consumed the request budget", async () => {
    const clock = vi.spyOn(Date, "now").mockReturnValue(0);
    const fetchImpl = vi.fn(async () => {
      clock.mockReturnValue(5000);
      return endpoint();
    });
    try {
      expect(await evaluateRecoveryDecision(evidence, { env, fetchImpl, log: vi.fn() })).toBe("unknown");
      expect(fetchImpl).toHaveBeenCalledOnce();
    } finally {
      clock.mockRestore();
    }
  });

  it("reduces the decision timeout by time already spent on reflection", async () => {
    const clock = vi.spyOn(Date, "now").mockReturnValue(0);
    const timeouts = vi.spyOn(AbortSignal, "timeout");
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(async () => {
        clock.mockReturnValue(2000);
        return endpoint();
      })
      .mockResolvedValueOnce(answer("continue"));
    try {
      expect(await evaluateRecoveryDecision(evidence, { env, fetchImpl, log: vi.fn() })).toBe("continue");
      expect(timeouts.mock.calls).toEqual([[5000], [3000]]);
    } finally {
      clock.mockRestore();
      timeouts.mockRestore();
    }
  });

  it("never follows a reflected endpoint outside the existing firewall proxy", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "https://example.com/v1/models" }] })));
    expect(await evaluateRecoveryDecision(evidence, { env, fetchImpl, log: vi.fn() })).toBe("unknown");
    expect(fetchImpl).toHaveBeenCalledOnce();
  });
});
