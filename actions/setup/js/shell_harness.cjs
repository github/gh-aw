// @ts-check

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const { runProcess } = require("./process_runner.cjs");
const { GENERIC_HARNESS_OUTPUT_TAIL_BYTES, runRetryingHarness } = require("./harness_generic_retry.cjs");
const { openPiDurableSession, observeDurableSession } = require("./durable_native_session.cjs");
const path = require("node:path");

async function main() {
  const [engineName, command] = process.argv.slice(2);
  if (!engineName || !command) {
    process.stderr.write("shell-harness: Usage: node shell_harness.cjs <engine-name> <command>\n");
    process.exitCode = 2;
    return;
  }

  if (process.env.GH_AW_ENGINE_CWD) {
    process.env.GH_AW_ENGINE_CWD = path.resolve(process.env.GH_AW_ENGINE_CWD);
  }
  const durableSession = engineName === "pi" ? openPiDurableSession() : undefined;
  /** @param {string} message */
  const log = message => process.stderr.write(`[${engineName}-harness] ${message}\n`);
  if (process.env.GH_AW_SKIP_IF_DECISION) {
    log("decision gate: harness initialized before decision evaluation");
  }
  // Durable Pi transcripts are bound by --session, so later attempts resume that session.
  const result = await runRetryingHarness({
    harnessName: `${engineName} harness`,
    log,
    getRetryMode: () => (durableSession ? "resume durable session" : "fresh run"),
    runAttempt: attempt =>
      runProcess({
        command: "bash",
        args: ["-o", "pipefail", "-c", command],
        logArgs: [engineName],
        attempt,
        log,
        maxCollectedOutputBytes: GENERIC_HARNESS_OUTPUT_TAIL_BYTES,
        onStdoutLine: line => observeDurableSession(durableSession, engineName, line),
      }),
  });
  process.exitCode = result.exitCode;
}

if (require.main === module) {
  main().catch(err => {
    process.stderr.write(`shell-harness: fatal error: ${getErrorMessage(err)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main };
