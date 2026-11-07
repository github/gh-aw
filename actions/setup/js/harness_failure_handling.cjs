// @ts-check

"use strict";

const { extractDeniedCommands } = require("./permission_denied_helpers.cjs");
const { emitMissingToolPermissionIssue, hasExpectedSafeOutputs } = require("./safeoutputs_cli.cjs");

/**
 * Handle an already-classified repeated permission failure without replaying work.
 * Classification stays engine-specific; Claude supplies commands from structured events.
 * @param {{
 *   attempt: number,
 *   output: string,
 *   safeOutputsPath: string,
 *   log: (message: string) => void,
 *   deniedCommands?: string[],
 * }} options
 * @returns {import("./harness_retry_runner.cjs").HarnessFailureDecision}
 */
function stopForPermissionDenied({ attempt, output, safeOutputsPath, log, deniedCommands }) {
  if (safeOutputsPath && hasExpectedSafeOutputs(safeOutputsPath, { logger: log })) {
    log(`attempt ${attempt + 1}: detected numerous permission-denied issues but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
    return { action: "stop", exitCode: 0 };
  }
  emitMissingToolPermissionIssue({ deniedCommands: deniedCommands ?? extractDeniedCommands(output), permissionOutput: output, logger: log });
  log(`attempt ${attempt + 1}: detected numerous permission-denied issues — not retrying (classified as missing tool/permission issue)`);
  return { action: "stop" };
}

module.exports = { stopForPermissionDenied };
