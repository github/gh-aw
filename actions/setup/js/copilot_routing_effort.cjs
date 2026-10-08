// @ts-check

"use strict";

const { ROUTING_REASONING_EFFORTS, isRoutingReasoningEffort } = require("./awf_model_routing.cjs");

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
