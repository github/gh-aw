// @ts-check
"use strict";

const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { hasTerminalSafeOutput } = require("./safeoutputs_cli.cjs");

/**
 * Capture the append-only safe-output baseline before starting an attempt.
 * Claude retains its strict inspection policy; other engines tolerate unreadable files.
 * @param {string} outputPath
 * @param {{ strict?: boolean, errorLabel?: string }} [options]
 * @returns {number}
 */
function getSafeOutputsByteOffset(outputPath, options = {}) {
  if (!outputPath) return 0;
  try {
    return fs.statSync(outputPath).size;
  } catch (error) {
    if (!options.strict || (error && typeof error === "object" && "code" in error && error.code === "ENOENT")) return 0;
    throw new Error(`Failed to inspect ${options.errorLabel || "safe-outputs file"}: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Observe only records appended during this attempt, retaining engine-specific terminal types.
 * @param {string} outputPath
 * @param {number} byteOffset
 * @param {Parameters<typeof hasTerminalSafeOutput>[1]} [options]
 */
function hasAttemptTerminalSafeOutput(outputPath, byteOffset, options = {}) {
  return !!outputPath && Number.isSafeInteger(byteOffset) && byteOffset >= 0 && hasTerminalSafeOutput(outputPath, { ...options, byteOffset });
}

/**
 * Engines select eligible late exits and veto guards before invoking this helper.
 * @param {{
 *   eligible: boolean, safeOutputsPath: string, byteOffset: number,
 *   terminalOptions?: Parameters<typeof hasTerminalSafeOutput>[1],
 *   log: (message: string) => void, message: string,
 * }} options
 * @returns {import("./harness_retry_runner.cjs").HarnessFailureDecision | null}
 */
function stopForAttemptTerminalSafeOutput({ eligible, safeOutputsPath, byteOffset, terminalOptions, log, message }) {
  if (!eligible || !hasAttemptTerminalSafeOutput(safeOutputsPath, byteOffset, { ...terminalOptions, logger: log })) return null;
  log(message);
  return { action: "stop", exitCode: 0 };
}

module.exports = { getSafeOutputsByteOffset, hasAttemptTerminalSafeOutput, stopForAttemptTerminalSafeOutput };
