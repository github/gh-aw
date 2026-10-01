// @ts-check

"use strict";

/**
 * @param {unknown} value
 * @returns {number}
 */
function parseMaxToolCalls(value) {
  const parsed = typeof value === "number" ? value : typeof value === "string" && /^\d+$/.test(value) ? Number(value) : NaN;
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    throw new Error("maxToolCalls must be a positive safe integer");
  }
  return parsed;
}

/**
 * Build a synchronous pre-tool hook. Incrementing before returning reserves one
 * call atomically in the JavaScript event loop, including concurrent subagent calls.
 *
 * @param {number | string | undefined} configuredLimit
 * @param {(event: {toolName: string, sessionId: string, callCount: number, limit: number, exhausted: boolean}) => void} onDispatch
 * @returns {{onPreToolUse: NonNullable<import("@github/copilot-sdk").SessionHooks["onPreToolUse"]>, getCallCount: () => number} | undefined}
 */
function buildCopilotSDKToolCallBudget(configuredLimit, onDispatch) {
  if (configuredLimit === undefined) return undefined;
  const limit = parseMaxToolCalls(configuredLimit);
  let callCount = 0;

  return {
    onPreToolUse(input, invocation) {
      callCount += 1;
      const exhausted = callCount > limit;
      const event = {
        toolName: input.toolName || "unknown",
        sessionId: input.sessionId || invocation.sessionId || "unknown",
        callCount,
        limit,
        exhausted,
      };
      onDispatch(event);
      if (!exhausted) return;
      return {
        permissionDecision: "deny",
        permissionDecisionReason: `The workflow aggregate tool-call budget is exhausted (${limit} calls). This tool was not executed; stop using tools and provide the best result from completed work.`,
      };
    },
    getCallCount: () => callCount,
  };
}

module.exports = { parseMaxToolCalls, buildCopilotSDKToolCallBudget };
