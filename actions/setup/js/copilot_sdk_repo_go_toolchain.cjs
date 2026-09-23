// @ts-check

"use strict";

/**
 * Pure command-argv builders for the go-repository profile's fixed action
 * set. Isolated from the SDK tool-call dispatch (copilot_sdk_repo_tools.cjs)
 * and the process/workspace execution guarantees (copilot_sdk_repo_workspace.cjs,
 * copilot_sdk_repo_process.cjs) so the Go toolchain contract can be reviewed,
 * tested, and eventually swapped for another language toolchain independently
 * of how commands are executed or how the SDK exposes them as tools.
 *
 * Every action here maps one-to-one onto a single bounded subprocess; none of
 * these builders accept caller-controlled flags, only caller-controlled file
 * lists that are validated by the workspace before being passed through.
 */

const GO_SOURCE_EXTENSION = ".go";

/** @param {string} filename */
function isGoSourceFile(filename) {
  return filename.endsWith(GO_SOURCE_EXTENSION);
}

/** @param {string[]} absolutePaths */
function goFormatWriteArgs(absolutePaths) {
  return ["-w", "-l", ...absolutePaths];
}

/** @returns {string[]} */
function goFormatCheckTreeArgs() {
  return ["-l", "."];
}

/** @returns {string[]} */
function goVetArgs() {
  return ["vet", "./..."];
}

/** @param {string} outputPath */
function goBuildArgs(outputPath) {
  return ["build", "-o", outputPath, "./..."];
}

/** @param {boolean} full */
function goTestArgs(full) {
  return full ? ["test", "-count=1", "./..."] : ["test", "-run", "^$", "./..."];
}

module.exports = {
  GO_SOURCE_EXTENSION,
  isGoSourceFile,
  goFormatWriteArgs,
  goFormatCheckTreeArgs,
  goVetArgs,
  goBuildArgs,
  goTestArgs,
};
