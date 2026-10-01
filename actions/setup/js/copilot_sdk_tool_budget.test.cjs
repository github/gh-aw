import { describe, it, expect, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { parseMaxToolCalls, buildCopilotSDKToolCallBudget } = require("./copilot_sdk_tool_budget.cjs");

describe("copilot_sdk_tool_budget.cjs", () => {
  it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, "", "0", "1.5", "1e2", null])("rejects invalid limit %s", value => {
    expect(() => parseMaxToolCalls(value)).toThrow("maxToolCalls must be a positive safe integer");
  });

  it("executes the last permitted call and denies the next one", () => {
    const onDispatch = vi.fn();
    const budget = buildCopilotSDKToolCallBudget(2, onDispatch);
    expect(budget).toBeDefined();

    expect(budget.onPreToolUse({ toolName: "bash", sessionId: "root", toolArgs: {} }, { sessionId: "root" })).toBeUndefined();
    expect(budget.onPreToolUse({ toolName: "mcp", sessionId: "root", toolArgs: {} }, { sessionId: "root" })).toBeUndefined();
    const denied = budget.onPreToolUse({ toolName: "edit", sessionId: "root", toolArgs: {} }, { sessionId: "root" });

    expect(denied).toMatchObject({ permissionDecision: "deny" });
    expect(denied.permissionDecisionReason).toContain("budget is exhausted (2 calls)");
    expect(budget.getCallCount()).toBe(3);
    expect(onDispatch).toHaveBeenNthCalledWith(2, expect.objectContaining({ toolName: "mcp", callCount: 2, exhausted: false }));
    expect(onDispatch).toHaveBeenNthCalledWith(3, expect.objectContaining({ toolName: "edit", callCount: 3, exhausted: true }));
  });

  it("reserves calls atomically across concurrent tool and subagent dispatches", async () => {
    const onDispatch = vi.fn();
    const budget = buildCopilotSDKToolCallBudget(10, onDispatch);
    const results = await Promise.all(
      Array.from({ length: 50 }, (_, index) => Promise.resolve().then(() => budget.onPreToolUse({ toolName: index % 2 ? "bash" : "mcp", sessionId: index % 3 ? "root" : "subagent", toolArgs: {} }, { sessionId: "root" })))
    );

    expect(results.filter(result => result?.permissionDecision === "deny")).toHaveLength(40);
    expect(onDispatch.mock.calls.filter(([event]) => !event.exhausted)).toHaveLength(10);
    expect(budget.getCallCount()).toBe(50);
    expect(onDispatch).toHaveBeenCalledWith(expect.objectContaining({ sessionId: "subagent" }));
  });

  it("does not install a hook when no limit is configured", () => {
    expect(buildCopilotSDKToolCallBudget(undefined, () => {})).toBeUndefined();
  });
});
