// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "call_workflow";

const { buildAwContext } = require("./aw_context.cjs");
const { redactWebhookPayloads } = require("./webhook_context.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

/**
 * Main handler factory for call_workflow.
 * Unlike dispatch_workflow, this handler makes no GitHub API calls.
 * It validates the selected workflow against the compile-time allowlist and
 * serialises the agent's inputs as a JSON payload string, then sets GitHub
 * Actions step outputs that the conditional `uses:` jobs read at runtime.
 *
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedWorkflows = config.workflows || [];
  const maxCount = config.max || 1;

  core.info(`Call workflow configuration: max=${maxCount}`);
  if (allowedWorkflows.length > 0) {
    core.info(`Allowed workflows: ${allowedWorkflows.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;
  const isStaged = isStagedMode(config);

  /**
   * Message handler function that processes a single call_workflow message.
   * Sets step outputs call_workflow_name and call_workflow_payload which
   * are consumed by the compiler-generated conditional `uses:` fan-out jobs.
   *
   * @param {Object} message - The call_workflow message to process
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCallWorkflow(message) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping call_workflow: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const workflowName = message.workflow_name;

    if (!workflowName || workflowName.trim() === "") {
      core.warning("Workflow name is empty, skipping");
      return {
        success: false,
        error: "Workflow name is empty",
      };
    }

    // Validate workflow is in allowed list.
    // An empty allowlist is treated as permissive (no restriction).
    // In practice, the compiler always populates this list from frontmatter.
    if (allowedWorkflows.length === 0) {
      core.warning("No allowed workflows configured; allowing any workflow (permissive mode).");
    } else if (!allowedWorkflows.includes(workflowName)) {
      const error = `Workflow "${workflowName}" is not in the allowed workflows list: ${allowedWorkflows.join(", ")}`;
      core.warning(error);
      return {
        success: false,
        error: error,
      };
    }

    try {
      core.info(`Selecting workflow for call: ${workflowName}`);

      // Serialise inputs as a JSON payload string so they can be forwarded
      // through a single `payload` input to the called workflow.
      /** @type {Record<string, unknown>} */
      const inputs = message.inputs && typeof message.inputs === "object" ? { ...message.inputs } : {};
      if (!("aw_context" in inputs)) {
        inputs.aw_context = JSON.stringify(buildAwContext());
      } else if (typeof inputs.aw_context !== "string" || inputs.aw_context === "") {
        inputs.aw_context = JSON.stringify(inputs.aw_context);
      }
      const payloadJson = JSON.stringify(inputs);
      const logPayloadJson = JSON.stringify({ ...inputs, aw_context: redactWebhookPayloads(inputs.aw_context) });

      // If in staged mode, preview the workflow call without executing it
      if (isStaged) {
        logStagedPreviewInfo(`Would call workflow: ${workflowName} with payload: ${logPayloadJson}`);
        return {
          success: true,
          staged: true,
          workflow_name: workflowName,
          payload: payloadJson,
        };
      }

      // Set the step outputs that the conditional `uses:` jobs check
      core.setOutput("call_workflow_name", workflowName);
      core.setOutput("call_workflow_payload", payloadJson);

      core.info(`✓ Selected workflow: ${workflowName}`);
      core.info(`  Payload: ${logPayloadJson}`);

      return {
        success: true,
        delegated: true,
        workflow_name: workflowName,
        payload: payloadJson,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to process call_workflow "${workflowName}": ${errorMessage}`);

      return {
        success: false,
        error: `Failed to process call_workflow "${workflowName}": ${errorMessage}`,
      };
    }
  };
}

module.exports = { main };
