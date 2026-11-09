// @ts-check

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const { runProcess } = require("./process_runner.cjs");

async function main() {
  const [engineName, command] = process.argv.slice(2);
  if (!engineName || !command) {
    process.stderr.write("shell-harness: Usage: node shell_harness.cjs <engine-name> <command>\n");
    process.exitCode = 2;
    return;
  }

  const runAttempt = () =>
    runProcess({
      command: "bash",
      args: ["-o", "pipefail", "-c", command],
      logArgs: [engineName],
      attempt: 0,
      log: message => process.stderr.write(`[${engineName}-harness] ${message}\n`),
    });
  const result = process.env.GH_AW_SKIP_IF_DECISION
    ? await require("./harness_retry_runner.cjs").runSingleHarnessInvocation({ runAttempt, harnessName: `${engineName} harness`, log: message => process.stderr.write(`[${engineName}-harness] ${message}\n`) })
    : await runAttempt();
  process.exitCode = result.exitCode;
}

if (require.main === module) {
  main().catch(err => {
    process.stderr.write(`shell-harness: fatal error: ${getErrorMessage(err)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main };
