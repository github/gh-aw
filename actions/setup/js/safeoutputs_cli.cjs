// @ts-check
"use strict";

const { normalizeSafeOutputName } = require("./safe_output_names.cjs");

/**
 * Shared safeoutputs CLI helpers for all agentic harnesses.
 *
 * Provides the low-level CLI invocation helper and the higher-level emit
 * functions so that report_missing_tool / report_incomplete diagnostics are emitted
 * through the supported safeoutputs CLI channel rather than via direct file
 * appends.  This survives read-only teardown states (EROFS) where direct
 * appends would silently drop structured failure signals.
 */

const childProcess = require("child_process");
const fs = require("fs");
const { ERR_VALIDATION, ERR_SYSTEM } = require("./error_codes.cjs");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");
const { iterateJsonl } = require("./jsonl_helpers.cjs");
const { buildMissingToolPermissionIssuePayload } = require("./permission_denied_helpers.cjs");

const SAFEOUTPUTS_CLI_TIMEOUT_MS = getSetupTimeoutMs("safeoutputsCli");

/**
 * @typedef {(toolName: string, args: Record<string, string>) => void} RunSafeOutputsCLILike
 */

/**
 * Invoke the safeoutputs CLI with named arguments.
 * @param {string} toolName
 * @param {Record<string, string>} args
 */
function runSafeOutputsCLI(toolName, args) {
  const command = process.env.GH_AW_SAFEOUTPUTS_CLI || "safeoutputs";
  /** @type {string[]} */
  const commandArgs = [toolName];
  for (const [key, value] of Object.entries(args)) {
    if (typeof value !== "string" || value.length === 0) continue;
    if (!/^[a-zA-Z0-9_-]+$/.test(key)) {
      throw new Error(`${ERR_VALIDATION}: invalid safeoutputs argument key: ${key}`);
    }
    commandArgs.push(`--${key}`);
    commandArgs.push(value);
  }
  try {
    childProcess.execFileSync(command, commandArgs, { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: SAFEOUTPUTS_CLI_TIMEOUT_MS });
  } catch (error) {
    const err = /** @type {{message?: string, stderr?: string | Buffer}} */ error ?? {};
    const stderr = typeof err.stderr === "string" ? err.stderr.trim() : Buffer.isBuffer(err.stderr) ? err.stderr.toString("utf8").trim() : "";
    const message = typeof err.message === "string" ? err.message : String(error);
    const keysSummary = Object.keys(args).join(", ");
    throw new Error(stderr ? `${ERR_SYSTEM}: safeoutputs ${toolName}(${keysSummary}) failed: ${message}: ${stderr}` : `${ERR_SYSTEM}: safeoutputs ${toolName}(${keysSummary}) failed: ${message}`);
  }
}

/**
 * Build report_missing_tool alternatives text with optional denied-command diagnostics.
 * @param {string} baseAlternatives
 * @param {string[]} deniedCommands
 * @returns {string}
 */
function buildMissingToolAlternatives(baseAlternatives, deniedCommands) {
  if (!Array.isArray(deniedCommands) || deniedCommands.length === 0) {
    return baseAlternatives;
  }
  const maxLength = 512;
  if (baseAlternatives.length >= maxLength) {
    return baseAlternatives.slice(0, maxLength);
  }

  const remaining = maxLength - baseAlternatives.length;
  const prefix = " Denied commands: ";
  if (remaining <= prefix.length) {
    return baseAlternatives.slice(0, maxLength);
  }

  let suffix = prefix;
  let appendedCount = 0;
  for (const command of deniedCommands) {
    const delimiter = appendedCount === 0 ? "" : " | ";
    const candidate = `${delimiter}${command}`;
    if (suffix.length + candidate.length > remaining) {
      const remainingCount = deniedCommands.length - appendedCount;
      const more = ` | ... and ${remainingCount} more`;
      if (remainingCount > 0 && suffix.length + more.length <= remaining) {
        suffix += more;
      }
      break;
    }
    suffix += candidate;
    appendedCount += 1;
  }

  return (baseAlternatives + suffix).slice(0, maxLength);
}

/**
 * Emit a structured report_missing_tool signal for repeated permission-denied failures.
 * @param {{
 *   safeOutputsPath?: string,
 *   runSafeOutputsCLI?: RunSafeOutputsCLILike,
 *   logger?: (message: string) => void,
 *   deniedCommands?: string[],
 *   permissionOutput?: string
 * }=} options
 */
function emitMissingToolPermissionIssue(options) {
  const safeOutputsPath = options && typeof options.safeOutputsPath === "string" ? options.safeOutputsPath : process.env.GH_AW_SAFE_OUTPUTS || "";
  const runSafeOutputs = options && options.runSafeOutputsCLI ? options.runSafeOutputsCLI : runSafeOutputsCLI;
  const logger = options && options.logger ? options.logger : defaultLog;
  const deniedCommands = options && options.deniedCommands ? options.deniedCommands : [];
  const permissionOutput = options && typeof options.permissionOutput === "string" ? options.permissionOutput : "";

  if (!safeOutputsPath) {
    logger("report_missing_tool skipped: GH_AW_SAFE_OUTPUTS is not set");
    return;
  }
  try {
    const payload = JSON.parse(buildMissingToolPermissionIssuePayload(deniedCommands, permissionOutput));
    runSafeOutputs("report_missing_tool", {
      tool: payload.tool,
      reason: payload.reason,
      alternatives: buildMissingToolAlternatives(payload.alternatives, deniedCommands),
    });
    logger(`report_missing_tool emitted via safeoutputs CLI: ${safeOutputsPath}`);
  } catch (error) {
    const err = /** @type {Error} */ /** @type {unknown} */ error;
    logger(`report_missing_tool emission failed: ${err.message}`);
  }
}

/**
 * Append a structured report_incomplete signal when infrastructure failures prevent completion.
 * This allows downstream failure handling to classify transient infrastructure errors explicitly.
 * @param {string} details
 * @param {{
 *   safeOutputsPath?: string,
 *   runSafeOutputsCLI?: RunSafeOutputsCLILike,
 *   logger?: (message: string) => void
 * }=} options
 */
function emitInfrastructureIncomplete(details, options) {
  const safeOutputsPath = options && typeof options.safeOutputsPath === "string" ? options.safeOutputsPath : process.env.GH_AW_SAFE_OUTPUTS || "";
  const runSafeOutputs = options && options.runSafeOutputsCLI ? options.runSafeOutputsCLI : runSafeOutputsCLI;
  const logger = options && options.logger ? options.logger : defaultLog;

  if (!safeOutputsPath) {
    logger("report_incomplete skipped: GH_AW_SAFE_OUTPUTS is not set");
    return;
  }
  try {
    runSafeOutputs("report_incomplete", {
      reason: "infrastructure_error",
      details,
    });
    logger(`report_incomplete emitted via safeoutputs CLI: ${safeOutputsPath}`);
  } catch (error) {
    const err = /** @type {Error} */ /** @type {unknown} */ error;
    logger(`report_incomplete emission failed: ${err.message}`);
  }
}

/**
 * Diagnostic safe-output types that represent infrastructure signals, not task-level work.
 * Entries with these types are excluded when checking whether expected outputs were produced.
 */
// Expected-output checks look for task-level outputs only, so noop is diagnostic
// there even though terminal-output checks treat noop as a completed result.
// report_missing_data is not an infrastructure diagnostic for expected-output checks,
// but it is non-terminal for shared harness checks by default. Engines with
// legacy terminal semantics opt report_missing_data/report_incomplete in via flags.
const TERMINAL_SAFE_OUTPUT_TYPES = new Set(["noop"]);
const DIAGNOSTIC_SAFE_OUTPUT_TYPES = new Set(["noop", "report_missing_tool", "report_incomplete"]);
// Types in this set are excluded by the catch-all fallthrough; opt-in checks for
// report_missing_data/report_incomplete must run before that fallthrough.
const NON_TERMINAL_SAFE_OUTPUT_TYPES = new Set(["report_missing_tool", "report_missing_data", "report_incomplete"]);

/**
 * Read the safe-outputs JSONL file and check whether it contains at least one
 * non-diagnostic entry (i.e. an output that represents real, task-level work).
 * Returns true when such an entry exists; false otherwise.
 * Used by harnesses to suppress the terminal permission-denied verdict
 * when the agent already produced the expected output — preventing false-red runs.
 * @param {string} safeOutputsPath - Path to the safe-outputs JSONL file
 * @param {{
 *   logger?: (msg: string) => void,
 *   readFileSync?: (path: string, encoding: BufferEncoding) => string
 * }=} options
 * @returns {boolean}
 */
function hasExpectedSafeOutputs(safeOutputsPath, options) {
  const logger = options && options.logger ? options.logger : defaultLog;
  const readFile = options && options.readFileSync ? options.readFileSync : fs.readFileSync;

  if (!safeOutputsPath) {
    return false;
  }

  let content;
  try {
    content = readFile(safeOutputsPath, "utf8");
  } catch {
    // File does not exist or is not readable — no expected entries present
    return false;
  }

  for (const parsed of iterateJsonl(content)) {
    if (parsed && parsed.type && !DIAGNOSTIC_SAFE_OUTPUT_TYPES.has(normalizeSafeOutputName(parsed.type))) {
      logger(`hasExpectedSafeOutputs: non-diagnostic entry found in ${safeOutputsPath}: type=${parsed.type}`);
      return true;
    }
  }
  return false;
}

/**
 * Read a safe-outputs JSONL file and check whether it contains a terminal agent
 * result. Terminal outputs include noop and any non-diagnostic task output types.
 * `byteOffset` scopes the check to output appended by the current attempt.
 * @param {string} safeOutputsPath - Path to the safe-outputs JSONL file
 * @param {{
 *   byteOffset?: number,
 *   includeMissingData?: boolean,
 *   includeReportIncomplete?: boolean,
 *   excludePartialReviewComments?: boolean,
 *   logger?: (msg: string) => void,
 *   readFileSync?: (path: string, encoding: BufferEncoding) => string
 * }=} options
 * @returns {boolean}
 */
function hasTerminalSafeOutput(safeOutputsPath, options) {
  const logger = options && options.logger ? options.logger : defaultLog;
  const byteOffset = options && Number.isFinite(options.byteOffset) ? Number(options.byteOffset) : 0;

  if (!safeOutputsPath) {
    return false;
  }
  if (byteOffset > 0 && options?.readFileSync) {
    logger("hasTerminalSafeOutput: byteOffset is unsupported with injected readFileSync; returning false");
    return false;
  }

  let content;
  try {
    if (byteOffset > 0) {
      const fd = fs.openSync(safeOutputsPath, "r");
      try {
        const stats = fs.fstatSync(fd);
        const fileSize = stats.size;
        if (fileSize <= byteOffset) return false;
        const length = fileSize - byteOffset;
        const buf = Buffer.allocUnsafe(length);
        fs.readSync(fd, buf, 0, length, byteOffset);
        content = buf.toString("utf8");
      } finally {
        fs.closeSync(fd);
      }
    } else {
      const readFile = options && options.readFileSync ? options.readFileSync : fs.readFileSync;
      content = readFile(safeOutputsPath, "utf8");
    }
  } catch {
    return false;
  }

  for (const parsed of iterateJsonl(content)) {
    const type = parsed && typeof parsed.type === "string" ? normalizeSafeOutputName(parsed.type) : "";
    if (!type) continue;
    // Inline findings do not prove that the final review was submitted.
    if ((type === "create_pull_request_review_comment" || type === "create-pull-request-review-comment") && options?.excludePartialReviewComments) continue;
    if (TERMINAL_SAFE_OUTPUT_TYPES.has(type)) {
      logger(`hasTerminalSafeOutput: terminal entry found in ${safeOutputsPath}: type=${type}`);
      return true;
    }
    if (type === "report_missing_data" && options?.includeMissingData) {
      logger(`hasTerminalSafeOutput: terminal entry found in ${safeOutputsPath}: type=${type}`);
      return true;
    }
    if (type === "report_incomplete" && options?.includeReportIncomplete) {
      logger(`hasTerminalSafeOutput: terminal entry found in ${safeOutputsPath}: type=${type}`);
      return true;
    }
    if (!NON_TERMINAL_SAFE_OUTPUT_TYPES.has(type)) {
      logger(`hasTerminalSafeOutput: terminal entry found in ${safeOutputsPath}: type=${type}`);
      return true;
    }
  }
  return false;
}

/**
 * Read the safe-outputs JSONL file and check whether any noop entry has been written.
 * Returns true when at least one {"type":"noop"} line is present; false otherwise.
 * Used by harnesses to skip agent startup or suppress retries when a noop was already
 * recorded — indicating the work is complete or there was nothing to do.
 * @param {string} safeOutputsPath - Path to the safe-outputs JSONL file
 * @param {{
 *   logger?: (msg: string) => void,
 *   readFileSync?: (path: string, encoding: BufferEncoding) => string
 * }=} options
 * @returns {boolean}
 */
function hasNoopInSafeOutputs(safeOutputsPath, options) {
  const logger = options && options.logger ? options.logger : defaultLog;
  const readFile = options && options.readFileSync ? options.readFileSync : fs.readFileSync;

  if (!safeOutputsPath) {
    return false;
  }

  let content;
  try {
    content = readFile(safeOutputsPath, "utf8");
  } catch {
    // File does not exist or is not readable — no noop entries present
    return false;
  }

  for (const parsed of iterateJsonl(content)) {
    if (parsed && parsed.type === "noop") {
      logger(`hasNoopInSafeOutputs: noop entry found in ${safeOutputsPath}`);
      return true;
    }
  }
  return false;
}

/**
 * Default logger that writes to stderr.
 * @param {string} message
 */
function defaultLog(message) {
  process.stderr.write(`[safeoutputs-cli] ${message}\n`);
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    runSafeOutputsCLI,
    buildMissingToolAlternatives,
    emitMissingToolPermissionIssue,
    emitInfrastructureIncomplete,
    hasExpectedSafeOutputs,
    hasTerminalSafeOutput,
    hasNoopInSafeOutputs,
  };
}
