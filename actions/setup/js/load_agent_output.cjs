// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { EMPTY_OUTPUT_FAILURE_CAUSES } = require("./empty_output_outcome.cjs");

const fs = require("fs");
const { normalizeRuntimeMessage, readClaimScopeContext, currentClaimHandle } = require("./work_queue_claim_scope.cjs");

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

module.exports = { loadAgentOutput, truncateForLogging, MAX_LOG_CONTENT_LENGTH };
