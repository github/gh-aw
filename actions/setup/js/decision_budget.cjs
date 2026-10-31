// @ts-check
"use strict";

const { findModelPricing, usdToAIC } = require("./model_costs.cjs");

function resolveDecisionBudget(env) {
  const value = env.GH_AW_ADMISSION_RESULT === "admitted" ? env.GH_AW_ADMISSION_MAX_AI_CREDITS : env.GH_AW_MAX_AI_CREDITS;
  if (typeof value !== "string" || !/^[1-9][0-9]{0,9}$/.test(value) || Number(value) > 2147483647) {
    throw new Error("ERR_DECISION: Missing or invalid resolved AI credit budget.");
  }
  return Number(value);
}

// Byte-level input bounds deliberately overestimate text tokens; framing gets
// a separate allowance. Reserve worst-case cost even when usage is unavailable.
function decisionReservation(selection, requestBytes, outputTokens) {
  const pricing = selection.pricing || findModelPricing(selection.provider, selection.model);
  if (!pricing || pricing.input === undefined || pricing.output === undefined) return null;
  const rates = ["input", "output", "cache_read", "cache_write", "reasoning"].map(key => {
    const value = pricing[key];
    if (value === undefined) return 0;
    if (typeof value !== "number" && (typeof value !== "string" || !value.trim())) return NaN;
    return Number(value);
  });
  if (rates.some(value => !Number.isFinite(value) || value < 0) || rates[0] <= 0) return null;
  const inputPrice = Math.max(rates[0], rates[2], rates[3]);
  const outputPrice = Math.max(rates[1], rates[4]);
  return usdToAIC((requestBytes + 512) * inputPrice + (selection.type === "decisions" ? 0 : outputTokens * outputPrice));
}

module.exports = { resolveDecisionBudget, decisionReservation };
