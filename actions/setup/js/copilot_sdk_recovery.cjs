// @ts-check
"use strict";

const { createHarnessPolicyDispatcher } = require("./harness_policy.cjs");
const { createRecoveryPolicy, evaluateRecoveryDecision, isHarnessRecoveryEnabled, DECISION_TIMEOUT_MS } = require("./harness_recovery.cjs");
const { createFirewallSteeringMonitor } = require("./harness_firewall_steering.cjs");

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
  const firewall = createFirewallSteeringMonitor({ env, minimumRemainingMs: 10000, log });
  const policy = isHarnessRecoveryEnabled(env) ? createRecoveryPolicy({ decide, log, minimumRemainingMs: 10000 + DECISION_TIMEOUT_MS }) : undefined;
  if (!policy && !firewall) return;

  /**
   * @param {{sessionId: string, toolName: string}} input
   * @param {{sessionId: string}} invocation
   * @param {boolean | undefined} success
   */
  async function observe(input, invocation, success) {
    const sessionId = getSessionId();
    if (!sessionId || input.sessionId !== sessionId || invocation.sessionId !== sessionId || !isActive()) return;
    if (typeof input.toolName !== "string" || !input.toolName || input.toolName.length > 256) return;
    /** @type {string[]} */
    const messages = [];
    /** @type {import("./harness_policy.cjs").HarnessPolicyAdapter} */
    const adapter = {
      engine: "copilot",
      actions: {
        steer(action) {
          if (isActive() && remainingMs() > 10000) messages.push(action.message);
          else log("harness-policy: steering suppressed; terminal guard or completion window reached");
        },
      },
    };
    if (firewall) await firewall.observe(adapter, remainingMs());
    if (policy) {
      const dispatcher = createHarnessPolicyDispatcher({ adapter, policies: [policy] });
      await dispatcher.dispatch({ type: "turn.completed", ongoing: true, remainingMs: remainingMs(), tools: [{ toolName: input.toolName, success }] });
    }
    return messages.length && isActive() && remainingMs() > 10000 ? { additionalContext: messages.join("\n\n") } : undefined;
  }

  return {
    onPostToolUse: (input, invocation) => observe(input, invocation, input.toolResult.resultType === "success" ? true : undefined),
    onPostToolUseFailure: (input, invocation) => observe(input, invocation, false),
  };
}

module.exports = { buildCopilotSDKRecoveryHooks };
