// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");

/**
 * @typedef {Object} UpdateHandlerConfig
 * @property {string} itemType - Type of item (e.g., "update_issue", "update_pull_request", "update_discussion")
 * @property {string} itemTypeName - Human-readable name (e.g., "issue", "pull request", "discussion")
 * @property {boolean} supportsPR - Whether this handler supports PR context
 * @property {Function} resolveItemNumber - Function to resolve item number from message
 * @property {Function} buildUpdateData - Function to build update data from message
 * @property {Function} executeUpdate - Function to execute the update API call
 * @property {Function} formatSuccessResult - Function to format success result
 * @property {Object} [additionalConfig] - Additional configuration options specific to the handler
 */

/**
 * Creates a handler factory function with common update logic
 * This factory encapsulates the shared control flow:
 * - Configuration defaults (target, max count)
 * - Max count enforcement
 * - Target resolution
 * - Empty update validation
 * - Success/error response shaping
 *
 * @param {UpdateHandlerConfig} handlerConfig - Configuration for the specific update handler
 * @returns {HandlerFactoryFunction} Handler factory function
 */
function createUpdateHandlerFactory(handlerConfig) {
  const { itemType, itemTypeName, supportsPR, resolveItemNumber, buildUpdateData, executeUpdate, formatSuccessResult, additionalConfig = {} } = handlerConfig;

  /**
   * Main handler factory
   * @type {HandlerFactoryFunction}
   */
  return async function main(config = {}) {
    // Extract configuration with defaults
    const updateTarget = config.target || "triggering";
    const maxCount = config.max || 10;

    // Check if we're in staged mode
    const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

    // Build configuration log message
    const configParts = [`max=${maxCount}`, `target=${updateTarget}`];

    // Add additional config items to log
    Object.entries(additionalConfig).forEach(([key, value]) => {
      if (config[key] !== undefined) {
        configParts.push(`${key}=${config[key]}`);
      }
    });

    core.info(`Update ${itemTypeName} configuration: ${configParts.join(", ")}`);

    // Track state
    let processedCount = 0;

    /**
     * Message handler function
     * @param {Object} message - The update message
     * @param {Object} resolvedTemporaryIds - Resolved temporary IDs
     * @returns {Promise<Object>} Result
     */
    return async function handleUpdate(message, resolvedTemporaryIds) {
      // Check max limit
      if (processedCount >= maxCount) {
        core.warning(`Skipping ${itemType}: max count of ${maxCount} reached`);
        return {
          success: false,
          error: `Max count of ${maxCount} reached`,
        };
      }

      processedCount++;

      const item = message;

      // Resolve item number (may use custom logic)
      const itemNumberResult = resolveItemNumber(item, updateTarget, context);

      if (!itemNumberResult.success) {
        core.warning(itemNumberResult.error);
        return {
          success: false,
          error: itemNumberResult.error,
        };
      }

      const itemNumber = itemNumberResult.number;
      core.info(`Resolved target ${itemTypeName} #${itemNumber} (target config: ${updateTarget})`);

      // Build update data (handler-specific logic)
      const updateDataResult = buildUpdateData(item, config);

      if (!updateDataResult.success) {
        core.warning(updateDataResult.error);
        return {
          success: false,
          error: updateDataResult.error,
        };
      }

      // Check if buildUpdateData returned a skipped result (for update_pull_request)
      if (updateDataResult.skipped) {
        core.info(`No update fields provided for ${itemTypeName} #${itemNumber} - treating as no-op (skipping update)`);
        return {
          success: true,
          skipped: true,
          reason: updateDataResult.reason,
        };
      }

      const updateData = updateDataResult.data;

      // Validate that we have something to update
      // Note: Fields starting with "_" are internal (e.g., _rawBody, _operation)
      // and will be processed by executeUpdate. We should NOT skip if _rawBody exists.
      const updateFields = Object.keys(updateData).filter(k => !k.startsWith("_"));
      const hasRawBody = updateData._rawBody !== undefined;

      if (updateFields.length === 0 && !hasRawBody) {
        core.info(`No update fields provided for ${itemTypeName} #${itemNumber} - treating as no-op (skipping update)`);
        return {
          success: true,
          skipped: true,
          reason: "No update fields provided",
        };
      }

      core.info(`Updating ${itemTypeName} #${itemNumber} with: ${JSON.stringify(updateFields)}`);

      // If in staged mode, preview the update without applying it
      if (isStaged) {
        core.info(`Staged mode: Would update ${itemTypeName} #${itemNumber} with fields: ${JSON.stringify(updateFields)}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            number: itemNumber,
            updateFields,
            hasRawBody,
          },
        };
      }

      // Execute the update
      try {
        const updatedItem = await executeUpdate(github, context, itemNumber, updateData);
        core.info(`Successfully updated ${itemTypeName} #${itemNumber}: ${updatedItem.html_url || updatedItem.url}`);

        // Format and return success result
        return formatSuccessResult(itemNumber, updatedItem);
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.error(`Failed to update ${itemTypeName} #${itemNumber}: ${errorMessage}`);
        return {
          success: false,
          error: errorMessage,
        };
      }
    };
  };
}

module.exports = { createUpdateHandlerFactory };
