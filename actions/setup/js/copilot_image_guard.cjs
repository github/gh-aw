// @ts-check
"use strict";

const { createCopilotInvocationObserver } = require("./copilot_invocation_observer.cjs");
const { copilotSessionStateDirectories } = require("./copilot_model_selection.cjs");

const COPILOT_TOOL_IMAGE_EXIT_CODE = 80;
const COPILOT_TOOL_IMAGE_DIAGNOSTIC =
  "Copilot tool image input is unsupported on this BYOK inference path: an image result has no inline image data and may be silently dropped. Refusing to report success without visual content. See https://github.com/github/copilot-cli/issues/4600.";

/** @param {NodeJS.ProcessEnv} [env] */
function isCopilotBYOK(env = process.env) {
  return Boolean(env.COPILOT_PROVIDER_BASE_URL || env.COPILOT_API_KEY);
}

/** @param {any} event */
function hasUnsupportedCopilotToolImage(event) {
  const binaries = event?.data?.result?.binaryResultsForLlm;
  return event?.type === "tool.execution_complete" && event.data.success === true && Array.isArray(binaries) && binaries.some(binary => binary?.type === "image" && (typeof binary.data !== "string" || !binary.data));
}

/**
 * Inspect only newly appended events for this CLI invocation, including subagents.
 * @param {{sessionId: string, sessionDirs?: string[]}} options
 */
function createCopilotImageObserver({ sessionId, sessionDirs = copilotSessionStateDirectories() }) {
  if (!/^[a-z0-9_-]+$/i.test(sessionId)) throw new Error("Copilot image guard requires an exact invocation session ID");
  return createCopilotInvocationObserver({
    sessionId,
    sessionDirs,
    onEvent: event => (hasUnsupportedCopilotToolImage(event) ? COPILOT_TOOL_IMAGE_DIAGNOSTIC : ""),
    onError: () => "Copilot tool image capability verification failed; refusing to report an unverified run as successful.",
  });
}

module.exports = { COPILOT_TOOL_IMAGE_EXIT_CODE, COPILOT_TOOL_IMAGE_DIAGNOSTIC, isCopilotBYOK, hasUnsupportedCopilotToolImage, createCopilotImageObserver };
