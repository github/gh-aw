// @ts-check

"use strict";

/**
 * Reasoning efforts accepted for AWF model-routing selections.
 *
 * Must match the values the pinned Copilot CLI lists for `--reasoning-effort`
 * (`copilot --help` → `[possible values: none, minimal, low, medium, high, xhigh, max]`),
 * which is also the effort set used by gh-aw-router and the AWF routing candidate pool.
 */
const ROUTING_REASONING_EFFORTS = Object.freeze(["none", "minimal", "low", "medium", "high", "xhigh", "max"]);

/**
 * @param {unknown} effort
 * @returns {boolean}
 */
function isRoutingReasoningEffort(effort) {
  return typeof effort === "string" && ROUTING_REASONING_EFFORTS.includes(effort);
}

/**
 * Resolve the reasoning effort the SDK driver should forward to the session.
 * @param {NodeJS.ProcessEnv} env
 * @returns {string | undefined}
 */
function resolveRoutingReasoningEffort(env) {
  if (env.GH_AW_MODEL_ROUTING !== "1") return undefined;
  const effort = env.GH_AW_COPILOT_ROUTING_EFFORT;
  return isRoutingReasoningEffort(effort) ? effort : undefined;
}

module.exports = { ROUTING_REASONING_EFFORTS, isRoutingReasoningEffort, resolveRoutingReasoningEffort };
