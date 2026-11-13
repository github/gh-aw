// @ts-check

"use strict";

const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { isGitHubRetryAfterProvider } = require("./copilot_retry_after.cjs");

const CYBERSECURITY_POLICY_REJECTION_PREFIX = "422 This content was flagged for possible cybersecurity risk.";

/**
 * @param {any} event
 * @returns {boolean}
 */
function isTopLevelEvent(event) {
  const data = event?.data ?? {};
  return ![event?.agentId, event?.agent_id, data.agentId, data.agent_id, event?.parentToolUseId, event?.parent_tool_use_id, data.parentToolUseId, data.parent_tool_use_id, event?.parentToolCallId, data.parentToolCallId].some(
    value => typeof value === "string" && value.length > 0
  );
}

/**
 * @param {any} value
 * @returns {boolean}
 */
function hasCybersecurityPolicyRejectionPrefix(value) {
  return typeof value === "string" && value.startsWith(CYBERSECURITY_POLICY_REJECTION_PREFIX);
}

/**
 * Detect terminal cybersecurity-policy rejections from structured, top-level
 * error events emitted by harnesses routed through a GitHub provider.
 * @param {string} output
 * @param {string | undefined} provider
 * @param {"claude" | "codex" | "copilot"} engine
 * @returns {boolean}
 */
function isGitHubProviderCybersecurityPolicyRejection(output, provider, engine) {
  if (!isGitHubRetryAfterProvider(provider)) return false;

  for (const event of iterateJsonl(output)) {
    if (!isTopLevelEvent(event)) continue;

    if (event?.type === "session.error" && ["query", "sdk_driver"].includes(event.data?.errorType)) {
      if (hasCybersecurityPolicyRejectionPrefix(event.data?.message)) return true;
      continue;
    }

    if (engine === "claude" && event?.type === "result" && event.is_error === true) {
      if (hasCybersecurityPolicyRejectionPrefix(event.result)) return true;
      continue;
    }

    if (engine === "claude" && (event?.type === "error" || (event?.type === "system" && event.subtype === "api_error"))) {
      if (hasCybersecurityPolicyRejectionPrefix(event.error?.message ?? event.error ?? event.message)) return true;
      continue;
    }

    if (engine === "codex" && ["error", "turn.failed"].includes(event?.type)) {
      if (hasCybersecurityPolicyRejectionPrefix(event.error?.message ?? event.error ?? event.message)) return true;
    }
  }
  return false;
}

module.exports = { CYBERSECURITY_POLICY_REJECTION_PREFIX, isGitHubProviderCybersecurityPolicyRejection };
