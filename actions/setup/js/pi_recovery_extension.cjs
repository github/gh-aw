// @ts-check
"use strict";

const { createHarnessPolicyDispatcher } = require("./harness_policy.cjs");
const { createRecoveryPolicy, evaluateRecoveryDecision, isHarnessRecoveryEnabled, DECISION_TIMEOUT_MS } = require("./harness_recovery.cjs");
const { isPiResultError } = require("./pi_session.cjs");

/**
 * Installed by the existing steering extension in CLI, SDK and RPC modes.
 * Reuses its wall-clock budget so native retries cannot reset Recovery's limits.
 * @param {{ on: (event: "turn_end", handler: (event: any) => Promise<void>) => void, sendUserMessage: (message: string, options: {deliverAs: "steer"}) => void }} pi
 * @param {{
 *   remainingMs: () => number | undefined,
 *   minimumRemainingMs: number,
 *   env?: NodeJS.ProcessEnv,
 *   fetchImpl?: typeof fetch,
 *   log?: (message: string) => void,
 * }} options
 */
function installPiRecovery(pi, { remainingMs, minimumRemainingMs, env = process.env, fetchImpl = fetch, log = message => process.stderr.write(`[gh-aw/recovery] ${message}\n`) }) {
  if (!isHarnessRecoveryEnabled(env)) return;
  // Keep the entire decision request outside timeout steering's completion window.
  const reserveMs = Math.max(10000, minimumRemainingMs);
  const dispatcher = createHarnessPolicyDispatcher({
    adapter: {
      engine: "pi",
      actions: {
        steer: action => {
          const remaining = remainingMs();
          if (process.exitCode || remaining === undefined || !Number.isFinite(remaining) || remaining <= reserveMs) {
            log("recovery: steering suppressed; terminal guard or completion window reached");
            return;
          }
          pi.sendUserMessage(action.message, { deliverAs: "steer" });
        },
      },
    },
    policies: [
      createRecoveryPolicy({
        decide: evidence => evaluateRecoveryDecision(evidence, { env, fetchImpl, log }),
        minimumRemainingMs: reserveMs + DECISION_TIMEOUT_MS,
        log,
      }),
    ],
  });
  pi.on("turn_end", async event => {
    const remaining = remainingMs();
    if (process.exitCode || remaining === undefined || event.message?.stopReason !== "toolUse") return;
    /** @type {{toolName: string, success?: boolean}[]} */
    const tools = [];
    for (const result of Array.isArray(event.toolResults) ? event.toolResults : []) {
      if (typeof result?.toolName !== "string" || !result.toolName || result.toolName.length > 256) continue;
      const failed = isPiResultError(result);
      tools.push({ toolName: result.toolName, success: failed ? false : result.isError === false ? true : undefined });
    }
    await dispatcher.dispatch({ type: "turn.completed", ongoing: true, remainingMs: remaining, tools });
  });
}

module.exports = { installPiRecovery };
