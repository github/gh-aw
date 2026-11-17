// @ts-check
"use strict";

const { iterateJsonl } = require("./jsonl_helpers.cjs");
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { parsePiConfig, DEFAULT_SESSION_DIR } = require("./pi_runtime.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string} stream @returns {string} */
function sessionIDFromStream(stream) {
  for (const record of iterateJsonl(stream, { lineFilter: line => line.startsWith("{") })) {
    if (record.type === "session" && typeof record.id === "string") return record.id;
  }
  throw new Error("Pi did not emit a session header; cannot export the session");
}

function main() {
  if (!parsePiConfig().session?.export) return;
  const streamFile = process.env.GH_AW_AGENT_OUTPUT;
  if (!streamFile) throw new Error("GH_AW_AGENT_OUTPUT must point to the Pi streaming log");
  const sessionId = sessionIDFromStream(fs.readFileSync(streamFile, "utf8"));
  const sessionFile = fs.readdirSync(DEFAULT_SESSION_DIR).find(file => file.endsWith(`_${sessionId}.jsonl`));
  if (!sessionFile) throw new Error(`Persisted Pi session is missing: ${sessionId}`);
  const output = path.join(process.env.PI_CODING_AGENT_DIR || require("./runtime_paths.cjs").runtimePath("pi-agent-dir"), "session.html");
  execFileSync(process.env.GH_AW_PI_COMMAND || "pi", ["--export", path.join(DEFAULT_SESSION_DIR, sessionFile), output], { timeout: 60_000, stdio: ["ignore", "ignore", "inherit"] });
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    process.stderr.write(`[gh-aw/pi-session-export] ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  }
}

module.exports = { main, sessionIDFromStream };
