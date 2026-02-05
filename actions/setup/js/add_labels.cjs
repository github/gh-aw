// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "add_labels";

const { validateLabels } = require("./safe_output_validator.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Main handler factory for add_labels
 * Returns a message handler function that processes individual add_labels messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedLabels = config.allowed || [];
  const maxCount = config.max || 10;

  core.info(`Add labels configuration: max=${maxCount}`);
  if (allowedLabels.length > 0) {
    core.info(`Allowed labels: ${allowedLabels.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single add_labels message
   * @param {Object} message - The add_labels message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleAddLabels(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping add_labels: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    // Determine target issue/PR number
    const itemNumber = message.item_number !== undefined ? parseInt(String(message.item_number), 10) : (context.payload?.issue?.number ?? context.payload?.pull_request?.number);

    if (!itemNumber || isNaN(itemNumber)) {
      const error = message.item_number !== undefined ? `Invalid item number: ${message.item_number}` : "No issue/PR number available";
      core.warning(error);
      return { success: false, error };
    }

    const contextType = context.payload?.pull_request ? "pull request" : "issue";
    const requestedLabels = message.labels ?? [];
    core.info(`Requested labels: ${JSON.stringify(requestedLabels)}`);

    // If no labels provided, return a helpful message with allowed labels if configured
    if (requestedLabels.length === 0) {
      const labelSource = allowedLabels.length > 0 ? `the allowed list: ${JSON.stringify(allowedLabels)}` : "the repository's available labels";
      const error = `No labels provided. Please provide at least one label from ${labelSource}`;
      core.info(error);
      return { success: false, error };
    }

    // Use validation helper to sanitize and validate labels
    const labelsResult = validateLabels(requestedLabels, allowedLabels, maxCount);
    if (!labelsResult.valid) {
      // If no valid labels, log info and return gracefully
      if (labelsResult.error?.includes("No valid labels")) {
        core.info("No labels to add");
        return {
          success: true,
          number: itemNumber,
          labelsAdded: [],
          message: "No valid labels found",
        };
      }
      // For other validation errors, return error
      core.warning(`Label validation failed: ${labelsResult.error}`);
      return {
        success: false,
        error: labelsResult.error ?? "Invalid labels",
      };
    }

    const uniqueLabels = labelsResult.value ?? [];

    // Early return if no labels after validation
    if (uniqueLabels.length === 0) {
      core.info("No labels to add");
      return {
        success: true,
        number: itemNumber,
        labelsAdded: [],
        message: "No labels to add",
      };
    }

    core.info(`Adding ${uniqueLabels.length} labels to ${contextType} #${itemNumber}: ${JSON.stringify(uniqueLabels)}`);

    try {
      await github.rest.issues.addLabels({
        ...context.repo,
        issue_number: itemNumber,
        labels: uniqueLabels,
      });

      core.info(`Successfully added ${uniqueLabels.length} labels to ${contextType} #${itemNumber}`);
      return {
        success: true,
        number: itemNumber,
        labelsAdded: uniqueLabels,
        contextType,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to add labels: ${errorMessage}`);
      return { success: false, error: errorMessage };
    }
  };
}

module.exports = { main };
