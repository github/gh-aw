// @ts-check

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const { runProcess } = require("./process_runner.cjs");
const { openPiDurableSession, observeDurableSession } = require("./durable_native_session.cjs");

async function main() {
  const [engineName, command] = process.argv.slice(2);
  if (!engineName || !command) {
    process.stderr.write("shell-harness: Usage: node shell_harness.cjs <engine-name> <command>\n");
    process.exitCode = 2;
    return;
  }

  const durableSession = engineName === "pi" ? openPiDurableSession() : undefined;
  const result = await runProcess({
    command: "bash",
    args: ["-o", "pipefail", "-c", command],
    logArgs: [engineName],
    attempt: 0,
    log: message => process.stderr.write(`[${engineName}-harness] ${message}\n`),
    onStdoutLine: line => observeDurableSession(durableSession, engineName, line),
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
