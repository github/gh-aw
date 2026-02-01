// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Safe Output Project Handler Manager
 *
 * This module manages the dispatch of project-related safe output messages to dedicated handlers.
 * It handles safe output types that require GH_AW_PROJECT_GITHUB_TOKEN:
 * - create_project
 * - create_project_status_update
 *
 * These types are separated from the main handler manager because they require a different
 * GitHub token (GH_AW_PROJECT_GITHUB_TOKEN) than other safe output types.
 */

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { writeSafeOutputSummaries } = require("./safe_output_summary.cjs");
const { loadTemporaryIdMap } = require("./temporary_id.cjs");
const { loadCustomSafeOutputJobTypes } = require("./safe_output_helpers.cjs");

/**
 * Handler map configuration for project-related safe outputs
 * Maps safe output types to their handler module file paths
 * All these types require GH_AW_PROJECT_GITHUB_TOKEN
 */
const PROJECT_HANDLER_MAP = {
  create_project: "./create_project.cjs",
  create_project_status_update: "./create_project_status_update.cjs",
  update_project: "./update_project.cjs",
  copy_project: "./copy_project.cjs",
};

/**
 * Load configuration for project-related safe outputs
 * Reads configuration from GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG environment variable
 * @returns {Object} Safe outputs configuration
 */
function loadConfig() {
  if (!process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG) {
    throw new Error("GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG environment variable is required but not set");
  }

  try {
    const config = JSON.parse(process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG);
    core.info(`Loaded project handler config from GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG: ${JSON.stringify(config)}`);
    // Normalize config keys: convert hyphens to underscores
    return Object.fromEntries(Object.entries(config).map(([k, v]) => [k.replace(/-/g, "_"), v]));
  } catch (error) {
    throw new Error(`Failed to parse GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG: ${getErrorMessage(error)}`);
  }
}

/**
 * Load and initialize handlers for enabled project-related safe output types
 * Calls each handler's factory function (main) to get message processors
 * @param {Object} config - Safe outputs configuration
 * @returns {Promise<Map<string, Function>>} Map of type to message handler function
 */
async function loadHandlers(config) {
  const messageHandlers = new Map();

  core.info("Loading and initializing project-related safe output handlers based on configuration...");

  for (const [type, handlerPath] of Object.entries(PROJECT_HANDLER_MAP)) {
    // Check if this safe output type is enabled in the config
    // The presence of the config key indicates the handler should be loaded
    if (config[type]) {
      try {
        const handlerModule = require(handlerPath);
        if (handlerModule && typeof handlerModule.main === "function") {
          // Call the factory function with config to get the message handler
          const handlerConfig = config[type] || {};
          const messageHandler = await handlerModule.main(handlerConfig);

          if (typeof messageHandler !== "function") {
            // This is a fatal error - the handler is misconfigured
            // Re-throw to fail the step rather than continuing
            const error = new Error(`Handler ${type} main() did not return a function - expected a message handler function but got ${typeof messageHandler}`);
            core.error(`✗ Fatal error loading handler ${type}: ${error.message}`);
            throw error;
          }

          messageHandlers.set(type, messageHandler);
          core.info(`✓ Loaded and initialized handler for: ${type}`);
        } else {
          core.warning(`Handler module ${type} does not export a main function`);
        }
      } catch (error) {
        // Re-throw fatal handler validation errors
        const errorMessage = getErrorMessage(error);
        if (errorMessage.includes("did not return a function")) {
          throw error;
        }
        // For other errors (e.g., module not found), log warning and continue
        core.warning(`Failed to load handler for ${type}: ${errorMessage}`);
      }
    } else {
      core.debug(`Handler not enabled: ${type}`);
    }
  }

  core.info(`Loaded ${messageHandlers.size} project handler(s)`);
  return messageHandlers;
}

/**
 * Process project-related safe output messages
 * @param {Map<string, Function>} messageHandlers - Map of type to handler function
 * @param {Array<Object>} messages - Array of safe output messages
 * @returns {Promise<{results: Array<Object>, processedCount: number, temporaryProjectMap: Object}>} Processing results
 */
async function processMessages(messageHandlers, messages) {
  const results = [];
  let processedCount = 0;

  // Build a temporary project ID map as we process create_project messages
  const temporaryProjectMap = new Map();

  // Load temporary ID map from environment (populated by previous step)
  const temporaryIdMap = loadTemporaryIdMap();
  if (temporaryIdMap.size > 0) {
    core.info(`Loaded temporary ID map with ${temporaryIdMap.size} entry(ies)`);
  }

  // Load custom safe output job types that are processed by dedicated custom jobs
  const customSafeOutputJobTypes = loadCustomSafeOutputJobTypes();

  core.info(`Processing ${messages.length} project-related message(s)...`);

  // Process messages in order of appearance
  for (let i = 0; i < messages.length; i++) {
    const message = messages[i];
    const messageType = message.type;

    if (!messageType) {
      core.warning(`Skipping message ${i + 1} without type`);
      continue;
    }

    const messageHandler = messageHandlers.get(messageType);

    if (!messageHandler) {
      // Check if this message type is a custom safe output job
      if (customSafeOutputJobTypes.has(messageType)) {
        // Silently skip - this is handled by a custom safe output job
        core.debug(`Message ${i + 1} (${messageType}) will be handled by custom safe output job`);
        continue;
      }

      // Skip messages that are not project-related
      // These should be handled by other steps (main handler manager or standalone steps)
      core.debug(`Message ${i + 1} (${messageType}) is not a project-related type - skipping`);
      continue;
    }

    try {
      core.info(`Processing message ${i + 1}/${messages.length}: ${messageType}`);

      // Call the message handler with the individual message
      // Pass both the temporary project map and temporary ID map for resolution
      const result = await messageHandler(message, temporaryProjectMap, temporaryIdMap);

      // Check if the handler explicitly returned a failure
      if (result && result.success === false) {
        const errorMsg = result.error || "Handler returned success: false";
        core.error(`✗ Message ${i + 1} (${messageType}) failed: ${errorMsg}`);
        results.push({
          type: messageType,
          messageIndex: i,
          success: false,
          error: errorMsg,
        });
        continue;
      }

      // If this was a create_project, store the mapping
      if (messageType === "create_project" && result && result.projectUrl && message.temporary_id) {
        temporaryProjectMap.set(message.temporary_id.toLowerCase(), result.projectUrl);
        core.info(`✓ Stored project mapping: ${message.temporary_id} -> ${result.projectUrl}`);
      }

      results.push({
        type: messageType,
        messageIndex: i,
        success: true,
        result,
      });

      processedCount++;
      core.info(`✓ Message ${i + 1} (${messageType}) completed successfully`);
    } catch (error) {
      core.error(`✗ Message ${i + 1} (${messageType}) failed: ${getErrorMessage(error)}`);
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        error: getErrorMessage(error),
      });
    }
  }

  // Convert temporaryProjectMap to plain object for serialization
  const temporaryProjectMapObj = Object.fromEntries(temporaryProjectMap);

  return { results, processedCount, temporaryProjectMap: temporaryProjectMapObj };
}

/**
 * Main entry point for the project handler manager
 * Orchestrates loading config, handlers, and processing messages
 */
async function main() {
  try {
    core.info("=== Starting Project Handler Manager ===");

    // Validate that GH_AW_PROJECT_GITHUB_TOKEN is set
    if (!process.env.GH_AW_PROJECT_GITHUB_TOKEN) {
      throw new Error("GH_AW_PROJECT_GITHUB_TOKEN environment variable is required for project-related safe outputs. " + "Configure a GitHub token with Projects permissions in your workflow secrets.");
    }

    // Load configuration
    const config = loadConfig();

    // Load and initialize handlers
    const messageHandlers = await loadHandlers(config);

    if (messageHandlers.size === 0) {
      core.info("No project-related handlers enabled - nothing to process");
      core.setOutput("processed_count", 0);
      return;
    }

    // Load agent output
    core.info("Loading agent output...");
    const result = await loadAgentOutput();
    const messages = result.items || [];

    if (messages.length === 0) {
      core.info("No messages to process");
      core.setOutput("processed_count", 0);
      return;
    }

    // Process messages
    const { results, processedCount, temporaryProjectMap } = await processMessages(messageHandlers, messages);

    // Write step summaries for all processed safe-outputs
    await writeSafeOutputSummaries(results, messages);

    // Set outputs
    core.setOutput("processed_count", processedCount);

    // Export temporary project map as output so the regular handler manager can use it
    // to resolve project URLs in text (e.g., update_issue body)
    const temporaryProjectMapJson = JSON.stringify(temporaryProjectMap || {});
    core.setOutput("temporary_project_map", temporaryProjectMapJson);
    core.info(`Exported temporary project map with ${Object.keys(temporaryProjectMap || {}).length} mapping(s)`);

    // Summary
    const successCount = results.filter(r => r.success).length;
    const failureCount = results.filter(r => !r.success).length;

    core.info("\n=== Project Handler Manager Summary ===");
    core.info(`Total messages: ${messages.length}`);
    core.info(`Project-related messages processed: ${processedCount}`);
    core.info(`Successful: ${successCount}`);
    core.info(`Failed: ${failureCount}`);
    core.info(`Temporary project IDs registered: ${Object.keys(temporaryProjectMap || {}).length}`);

    if (failureCount > 0) {
      core.setFailed(`${failureCount} project-related message(s) failed to process`);
    }
  } catch (error) {
    core.setFailed(`Project handler manager failed: ${getErrorMessage(error)}`);
  }
}

// Export for testing
module.exports = {
  loadConfig,
  loadHandlers,
  processMessages,
  main,
};

// Run main if this script is executed directly (not required as a module)
if (require.main === module) {
  main();
}
