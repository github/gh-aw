// @ts-check
"use strict";

const { invokeDecisions } = require("./decisions_client.cjs");
const { resolveDecisionEndpoint } = require("./skip_if_decision.cjs");

const FAILURE_THRESHOLD = 3;
const MAX_DECISIONS = 2;
const DECISION_TIMEOUT_MS = 5000;
const RECOVERY_MESSAGE =
  "Repeated failures indicate the current approach is not progressing. Stop repeating the failing operation and reassess the available evidence. Choose a different approach only within the workflow's existing tool permissions and firewall policy; never bypass a denial or request broader access. If blocked, report the limitation through the configured safe outputs rather than claiming completion.";

/** @param {NodeJS.ProcessEnv} [env] */
function isHarnessRecoveryEnabled(env = process.env) {
  return env.GH_AW_HARNESS_RECOVERY === "1" && (!env.GH_AW_PHASE || env.GH_AW_PHASE === "agent") && env.GH_AW_PI_SUBAGENT_CHILD !== "1";
}

/** @typedef {"continue" | "change_strategy" | "unknown"} RecoveryDecision */
/** @typedef {Readonly<{ consecutiveFailures: number, decision: number, remainingMs: number }>} RecoveryEvidence */

/**
 * Counts attempts, including failed evaluations, against a fixed per-session cap.
 * Holds only the current failure identity and counters, never a transcript or tool output.
 * @param {{
 *   decide: (evidence: RecoveryEvidence) => Promise<RecoveryDecision>,
 *   log: (message: string) => void,
 *   minimumRemainingMs: number,
 * }} options
 * @returns {import("./harness_policy.cjs").HarnessPolicy}
 */
function createRecoveryPolicy({ decide, log, minimumRemainingMs }) {
  let lastFailure = "";
  let consecutiveFailures = 0;
  let decisions = 0;
  let evaluating = false;
  let observation = 0;
  return {
    id: "recovery",
    async onEvent(event, context) {
      if (event.type !== "turn.completed" && event.type !== "retry.ready") return;
      const capability = event.type === "retry.ready" ? "retry_guidance" : "steer";
      if ((event.type === "turn.completed" && !event.ongoing) || !Number.isFinite(event.remainingMs) || event.remainingMs <= minimumRemainingMs || !context.capabilities.includes(capability) || decisions >= MAX_DECISIONS) return;
      const failures =
        event.type === "retry.ready" ? [{ identity: `attempt:${JSON.stringify([...event.failureClasses].sort())}`, failed: true }] : event.tools.map(tool => ({ identity: `tool:${tool.toolName}`, failed: tool.success === false }));
      if (!failures.length) return;
      const currentObservation = ++observation;
      for (const failure of failures) {
        if (!failure.failed) {
          lastFailure = "";
          consecutiveFailures = 0;
        } else {
          consecutiveFailures = failure.identity === lastFailure ? consecutiveFailures + 1 : 1;
          lastFailure = failure.identity;
        }
      }
      if (consecutiveFailures < FAILURE_THRESHOLD || evaluating) return;
      const evidence = Object.freeze({ consecutiveFailures, decision: ++decisions, remainingMs: event.remainingMs });
      consecutiveFailures = 0;
      evaluating = true;
      try {
        const choice = await decide(evidence);
        log(`recovery: decision=${decisions}/${MAX_DECISIONS}; outcome=${choice}`);
        return choice === "change_strategy" && observation === currentObservation ? [{ type: capability, message: RECOVERY_MESSAGE }] : [];
      } finally {
        consecutiveFailures = 0;
        evaluating = false;
      }
    },
  };
}

/**
 * One bounded request through the existing firewall proxy; no direct-provider fallback.
 * Only numeric evidence is sent. Failures abstain with fixed diagnostics, never provider text.
 * @param {RecoveryEvidence} evidence
 * @param {{env?: NodeJS.ProcessEnv, fetchImpl?: typeof fetch, log: (message: string) => void}} options
 * @returns {Promise<RecoveryDecision>}
 */
async function evaluateRecoveryDecision(evidence, { env = process.env, fetchImpl = fetch, log }) {
  if (env.AWF_REFLECT_ENABLED !== "1") {
    log("recovery: decision unavailable; error=sandbox_unavailable; abstaining");
    return "unknown";
  }
  const startedAt = Date.now();
  try {
    const endpoint = await resolveDecisionEndpoint(fetchImpl, DECISION_TIMEOUT_MS);
    const remaining = DECISION_TIMEOUT_MS - (Date.now() - startedAt);
    if (remaining <= 0) {
      log("recovery: decision unavailable; error=timeout; abstaining");
      return "unknown";
    }
    const result = await invokeDecisions({
      ...endpoint,
      input:
        "Decide whether repeated execution failures warrant changing strategy. Evidence contains counts only, not task semantics. Do not infer task completion or permission to bypass restrictions. Choose unknown if the evidence is insufficient.",
      questions: [
        {
          type: "choice",
          name: "recovery",
          instructions: [{ type: "input_json", data: { consecutiveFailures: evidence.consecutiveFailures, decision: evidence.decision, remainingMs: evidence.remainingMs } }],
          choices: [
            { value: "continue", description: "Continue the current approach without an intervention." },
            { value: "change_strategy", description: "Ask the agent to reassess its approach within existing permissions." },
            { value: "unknown", description: "Insufficient evidence; abstain." },
          ],
        },
      ],
      timeoutMs: remaining,
      integrationId: "agentic-workflows",
      fetchImpl,
    });
    const answer = result.answers[0];
    return answer.type === "choice" && (answer.choice === "continue" || answer.choice === "change_strategy") ? answer.choice : "unknown";
  } catch (error) {
    const reason = error instanceof Error && ["AbortError", "TimeoutError"].includes(error.name) ? "timeout" : "evaluation_failed";
    log(`recovery: decision unavailable; error=${reason}; abstaining`);
    return "unknown";
  }
}

module.exports = { createRecoveryPolicy, evaluateRecoveryDecision, isHarnessRecoveryEnabled, DECISION_TIMEOUT_MS };
