// @ts-check
"use strict";

const { createHarnessPolicyDispatcher } = require("./harness_policy.cjs");
const { createRecoveryPolicy, evaluateRecoveryDecision, isHarnessRecoveryEnabled, DECISION_TIMEOUT_MS } = require("./harness_recovery.cjs");

/**
 * Native post-tool hooks return guidance, never modify tool results or permissions.
 * The policy is session-scoped; each invocation owns its additionalContext recipient.
 * @param {{
 *   getSessionId: () => string | undefined,
 *   remainingMs: () => number,
 *   isActive: () => boolean,
 *   log: (message: string) => void,
 *   env?: NodeJS.ProcessEnv,
 *   decide?: Parameters<typeof createRecoveryPolicy>[0]["decide"],
 * }} options
 * @returns {import("@github/copilot-sdk").SessionHooks | undefined}
 */
function buildCopilotSDKRecoveryHooks({ getSessionId, remainingMs, isActive, log, env = process.env, decide = evidence => evaluateRecoveryDecision(evidence, { env, log }) }) {
  if (!isHarnessRecoveryEnabled(env)) return;
  const policy = createRecoveryPolicy({ decide, log, minimumRemainingMs: 10000 + DECISION_TIMEOUT_MS });

  /**
   * @param {{sessionId: string, toolName: string}} input
   * @param {{sessionId: string}} invocation
   * @param {boolean | undefined} success
   */
  async function observe(input, invocation, success) {
    const sessionId = getSessionId();
    if (!sessionId || input.sessionId !== sessionId || invocation.sessionId !== sessionId || !isActive()) return;
    if (typeof input.toolName !== "string" || !input.toolName || input.toolName.length > 256) return;
    /** @type {string | undefined} */
    let additionalContext;
    const dispatcher = createHarnessPolicyDispatcher({
      adapter: {
        engine: "copilot",
        actions: {
          steer(action) {
            if (isActive() && remainingMs() > 10000) additionalContext = action.message;
            else log("recovery: steering suppressed; terminal guard or completion window reached");
          },
        },
      },
      policies: [policy],
    });
    await dispatcher.dispatch({ type: "turn.completed", ongoing: true, remainingMs: remainingMs(), tools: [{ toolName: input.toolName, success }] });
    return additionalContext ? { additionalContext } : undefined;
  }

  return {
    onPostToolUse: (input, invocation) => observe(input, invocation, input.toolResult.resultType === "success" ? true : undefined),
    onPostToolUseFailure: (input, invocation) => observe(input, invocation, false),
  };
}

module.exports = { buildCopilotSDKRecoveryHooks };
