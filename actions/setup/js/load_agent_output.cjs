// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { EMPTY_OUTPUT_FAILURE_CAUSES } = require("./empty_output_outcome.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");

const fs = require("fs");
const path = require("path");
const { normalizeRuntimeMessage, readClaimScopeContext, currentClaimHandle } = require("./work_queue_claim_scope.cjs");

const DIAGNOSTIC_AGENT_OUTPUT_TYPES = new Set(["noop", "report_missing_tool", "report_missing_data", "report_incomplete", "request_continuation"]);
const TASK_COMPLETE_REGISTRATION_SIGNALS = ["not registering", "not registered", "not recognizing", "not recognized", "recognition issue", "not yet marked", "haven't marked", "tool calls are not registering"];
const TASK_COMPLETE_COMPLETION_SIGNALS = ["completed successfully", "safe-output", "safe output", "no remaining work"];
const TASK_COMPLETE_COMPLETION_REGEXPS = [/\banalysis steps? completed\b/, /\ball steps completed\b/];

/**
 * Detect the existing task_complete registration-only diagnostic exception.
 * @param {{reason?: string, details?: string} | undefined} item
 * @returns {boolean}
 */
function isTaskCompleteRegistrationIssue(item) {
  if (!item) {
    return false;
  }
  const text = `${item.reason || ""}\n${item.details || ""}`.toLowerCase();
  if (!text.includes("task_complete")) {
    return false;
  }
  const hasRegistrationSignal = TASK_COMPLETE_REGISTRATION_SIGNALS.some(signal => text.includes(signal));
  const hasCompletionSignal = TASK_COMPLETE_COMPLETION_SIGNALS.some(signal => text.includes(signal)) || TASK_COMPLETE_COMPLETION_REGEXPS.some(regexp => regexp.test(text));
  return hasRegistrationSignal && hasCompletionSignal;
}

/**
 * Check the engine log for the terminal completion evidence used by failure policy.
 * @returns {boolean}
 */
function hasAgentTerminalReasonCompleted() {
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
  const stdioLogPath = agentOutputFile ? path.join(path.dirname(agentOutputFile), "agent-stdio.log") : "/tmp/gh-aw/agent-stdio.log";
  try {
    if (!fs.existsSync(stdioLogPath)) {
      return false;
    }
    return /"terminal_reason"[ ]?:[ ]?"completed"/.test(fs.readFileSync(stdioLogPath, "utf8"));
  } catch {
    return false;
  }
}

/**
 * Suppress registration-only diagnostics only with both terminal completion
 * evidence and a real task-level output, matching the agent failure policy.
 * @param {Array<any> | undefined} items
 * @returns {boolean}
 */
function shouldIgnoreTaskCompleteRegistrationReports(items) {
  if (!Array.isArray(items)) {
    return false;
  }
  const reports = items.filter(item => item?.type === "report_incomplete");
  return (
    reports.length > 0 &&
    hasAgentTerminalReasonCompleted() &&
    items.some(item => item && typeof item.type === "string" && !DIAGNOSTIC_AGENT_OUTPUT_TYPES.has(normalizeSafeOutputName(item.type))) &&
    reports.every(isTaskCompleteRegistrationIssue)
  );
}

/**
 * Maximum content length to log for debugging purposes
 * @type {number}
 */
const MAX_LOG_CONTENT_LENGTH = 10000;

/**
 * Truncate content for logging if it exceeds the maximum length
 * @param {string} content - Content to potentially truncate
 * @returns {string} Truncated content with indicator if truncated
 */
function truncateForLogging(content) {
  if (content.length <= MAX_LOG_CONTENT_LENGTH) {
    return content;
  }
  return content.substring(0, MAX_LOG_CONTENT_LENGTH) + `\n... (truncated, total length: ${content.length})`;
}

/**
 * Load and parse agent output from the GH_AW_AGENT_OUTPUT file
 *
 * This utility handles the common pattern of:
 * 1. Reading the GH_AW_AGENT_OUTPUT environment variable
 * 2. Loading the file content
 * 3. Validating the JSON structure
 * 4. Returning parsed items array
 *
 * @param {{partitioning?: boolean}} [options]
 * @returns {{
 *   success: true,
 *   items: any[],
 *   collectorEmptyOutputCause?: string,
 *   collectorFailureCause?: string,
 *   collectorDriverExitCode?: number,
 *   collectorRetryCount?: number,
 *   collectorEngineErrorType?: string
 * } | {
 *   success: false,
 *   items?: undefined,
 *   error?: string
 * }} Result object with success flag and items array (if successful) or error message
 */
function loadAgentOutput(options = {}) {
  const claimHandle = currentClaimHandle();
  const scope = claimHandle ? null : readClaimScopeContext();
  const queueEnabled = Boolean(scope || claimHandle);
  if (!options.partitioning && scope && !claimHandle) {
    return { success: false, error: "Queue standalone handlers require a trusted per-Claim execution context" };
  }
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;

  // No agent output file specified
  if (!agentOutputFile) {
    core.info("No GH_AW_AGENT_OUTPUT environment variable found");
    return { success: false };
  }

  // Read agent output from file
  let outputContent;
  try {
    outputContent = fs.readFileSync(agentOutputFile, "utf8");
  } catch (error) {
    const errorMessage = `Error reading agent output file: ${getErrorMessage(error)}`;
    // Use info instead of error for missing files - this is a normal scenario
    // when the agent fails before producing any safe-outputs
    core.info(errorMessage);
    return { success: false, error: errorMessage };
  }

  // Check for empty content
  if (outputContent.trim() === "") {
    core.info("Agent output content is empty");
    return { success: false };
  }

  core.info(`Agent output content length: ${outputContent.length}`);

  // Parse the validated output JSON
  let validatedOutput;
  try {
    validatedOutput = queueEnabled ? require("./work_queue_codec.cjs").parseStrictJSON(outputContent) : JSON.parse(outputContent);
  } catch (error) {
    const errorMessage = `Error parsing agent output JSON: ${getErrorMessage(error)}`;
    core.error(errorMessage);
    core.info(`Failed to parse content:\n${truncateForLogging(outputContent)}`);
    return { success: false, error: errorMessage };
  }

  // Validate items array exists
  if (!validatedOutput.items || !Array.isArray(validatedOutput.items)) {
    core.info("No valid items found in agent output");
    core.info(`Parsed content: ${truncateForLogging(JSON.stringify(validatedOutput))}`);
    return { success: false };
  }

  validatedOutput.items = validatedOutput.items.map(item => (item && typeof item.type === "string" ? { ...item, type: normalizeSafeOutputName(item.type) } : item));

  return {
    success: true,
    items: queueEnabled
      ? validatedOutput.items
          .map(item => {
            try {
              return normalizeRuntimeMessage(item);
            } catch (error) {
              return {
                type: item?.type,
                ...(item && Object.hasOwn(item, "claim_handle") ? { claim_handle: item.claim_handle } : {}),
                _claimScopeError: getErrorMessage(error),
                _claimScopeErrorCode: error.code,
              };
            }
          })
          .filter(item => !claimHandle || (!item._claimScopeError && item.claim_handle === claimHandle))
      : validatedOutput.items,
    ...(typeof validatedOutput.collectorEmptyOutputCause === "string" ? { collectorEmptyOutputCause: validatedOutput.collectorEmptyOutputCause } : {}),
    ...(Object.hasOwn(EMPTY_OUTPUT_FAILURE_CAUSES, validatedOutput.collectorFailureCause) ? { collectorFailureCause: validatedOutput.collectorFailureCause } : {}),
    ...(Number.isSafeInteger(validatedOutput.collectorDriverExitCode) && validatedOutput.collectorDriverExitCode >= 0 && validatedOutput.collectorDriverExitCode <= 255
      ? { collectorDriverExitCode: validatedOutput.collectorDriverExitCode }
      : {}),
    ...(Number.isSafeInteger(validatedOutput.collectorRetryCount) && validatedOutput.collectorRetryCount >= 0 ? { collectorRetryCount: validatedOutput.collectorRetryCount } : {}),
    ...(typeof validatedOutput.collectorEngineErrorType === "string" && /^[a-z][a-z0-9_]{0,63}$/.test(validatedOutput.collectorEngineErrorType) ? { collectorEngineErrorType: validatedOutput.collectorEngineErrorType } : {}),
  };
}

module.exports = { loadAgentOutput, truncateForLogging, MAX_LOG_CONTENT_LENGTH, isTaskCompleteRegistrationIssue, hasAgentTerminalReasonCompleted, shouldIgnoreTaskCompleteRegistrationReports };
