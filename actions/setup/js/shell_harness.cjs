// @ts-check

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const { runProcess } = require("./process_runner.cjs");
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
  const runAttempt = () =>
    runProcess({
      command: "bash",
      args: ["-o", "pipefail", "-c", command],
      logArgs: [engineName],
      attempt: 0,
      log,
      onStdoutLine: line => observeDurableSession(durableSession, engineName, line),
    });
  const result = process.env.GH_AW_SKIP_IF_DECISION ? await require("./harness_retry_runner.cjs").runSingleHarnessInvocation({ runAttempt, harnessName: `${engineName} harness`, log }) : await runAttempt();
  process.exitCode = result.exitCode;
}

if (require.main === module) {
  main().catch(err => {
    process.stderr.write(`shell-harness: fatal error: ${getErrorMessage(err)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main };
