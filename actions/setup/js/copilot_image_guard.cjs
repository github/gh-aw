// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { copilotSessionStateDirectories } = require("./copilot_model_selection.cjs");
const { copilotSessionContext } = require("./copilot_session.cjs");

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
  const files = sessionDirs.map(directory => path.join(directory, sessionId, "events.jsonl"));
  const offsets = new Map();
  const partial = new Map();
  let diagnostic = "";
  const sizeOf = file => {
    try {
      return fs.statSync(file).size;
    } catch (error) {
      if (error !== null && typeof error === "object" && "code" in error && error.code === "ENOENT") return 0;
      throw error;
    }
  };
  for (const file of files) offsets.set(file, sizeOf(file));

  function check(final = false) {
    if (diagnostic) return diagnostic;
    try {
      for (const file of files) {
        const size = sizeOf(file);
        let offset = offsets.get(file) || 0;
        if (size < offset) {
          offset = 0;
          partial.delete(file);
        }
        if (size === offset && !partial.has(file)) continue;
        const buffer = Buffer.alloc(size - offset);
        const fd = fs.openSync(file, "r");
        let bytesRead;
        try {
          bytesRead = fs.readSync(fd, buffer, 0, buffer.length, offset);
        } finally {
          fs.closeSync(fd);
        }
        offsets.set(file, offset + bytesRead);
        const text = (partial.get(file) || "") + buffer.subarray(0, bytesRead).toString("utf8");
        const end = final ? text.length : text.lastIndexOf("\n") + 1;
        partial.set(file, text.slice(end));
        for (const event of iterateJsonl(text.slice(0, end))) {
          const context = copilotSessionContext(event);
          if (context.sessionId !== undefined && context.sessionId !== sessionId) continue;
          if (hasUnsupportedCopilotToolImage(event)) return (diagnostic = COPILOT_TOOL_IMAGE_DIAGNOSTIC);
        }
      }
    } catch {
      diagnostic = "Copilot tool image capability verification failed; refusing to report an unverified run as successful.";
    }
    return diagnostic;
  }
  return { check };
}

module.exports = { COPILOT_TOOL_IMAGE_EXIT_CODE, COPILOT_TOOL_IMAGE_DIAGNOSTIC, isCopilotBYOK, hasUnsupportedCopilotToolImage, createCopilotImageObserver };
