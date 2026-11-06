// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { copilotSessionContext } = require("./copilot_session.cjs");

/**
 * Tail newly appended events for one invocation, including its subagents.
 * @param {{sessionId: string, sessionDirs: string[], onEvent: (event: any) => string, onError: (error: unknown) => string}} options
 */
function createCopilotInvocationObserver({ sessionId, sessionDirs, onEvent, onError }) {
  if (!/^[a-z0-9_-]+$/i.test(sessionId)) throw new Error("Copilot event observation requires an exact invocation session ID");
  const files = sessionDirs.map(directory => path.join(directory, sessionId, "events.jsonl"));
  /** @type {Map<string, number>} */
  const offsets = new Map();
  /** @type {Map<string, string>} */
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
          offsets.set(file, offset);
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
          diagnostic = onEvent(event);
          if (diagnostic) return diagnostic;
        }
      }
    } catch (error) {
      diagnostic = onError(error);
    }
    return diagnostic;
  }
  return { check };
}

module.exports = { createCopilotInvocationObserver };
