// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { replaceTemporaryIdReferences } = require("./temporary_id.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");

/**
 * Internal safe-output message fields that should not be forwarded as action inputs.
 * These fields are part of the safe-output messaging protocol and are not user-defined.
 * Maintained as an explicit set so future protocol fields can be added without silently
 * forwarding them to external action `with:` inputs.
 * @type {Set<string>}
 */
const INTERNAL_MESSAGE_FIELDS = new Set(["type", "claim_handle", "claim_id", "work_id"]);

/**
 * Main handler factory for a custom safe output action.
 *
 * Each configured safe-output action gets its own instance of this factory function,
 * invoked with a config that includes `action_name` (the normalized tool name).
 *
 * The handler:
 *  1. Enforces that the action is called at most once (per the spec).
 *  2. Applies temporary ID substitutions to all string-valued fields in the payload.
 *  3. Exports the processed payload as a step output named `action_<name>_payload`.
 *
 * The compiler generates a corresponding GitHub Actions step with:
 *   if: steps.process_safe_outputs.outputs.action_<name>_payload != ''
 *   uses: <resolved-action-ref>
 *   with:
 *     <input>: ${{ fromJSON(steps.process_safe_outputs.outputs.action_<name>_payload).<input> }}
 *
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const actionName = config.action_name || "unknown_action";
  const outputKey = `action_${actionName}_payload`;

  core.info(`Custom action handler initialized: action_name=${actionName}, output_key=${outputKey}`);

  // Track whether this action has been called (enforces once-only constraint)
  let called = false;

  /**
   * Handler function that processes a single tool call for this action.
   * Applies temporary ID substitutions and exports the payload as a step output.
   *
   * @param {Object} message - The tool call message from the agent output
   * @param {Object} resolvedTemporaryIds - Map of temp IDs to resolved values (plain object)
   * @param {Map<string, Object>} temporaryIdMap - Live map of temporary IDs (for substitution)
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCustomAction(message, resolvedTemporaryIds, temporaryIdMap = new Map()) {
    // Enforce once-only constraint
    if (called) {
      const error = `Action "${actionName}" can only be called once per workflow run`;
      core.warning(error);
      return {
        success: false,
        error,
      };
    }
    called = true;

    try {
      core.info(`Processing custom action: ${actionName}`);

      // Build the processed payload by:
      // 1. Applying temporary ID reference substitutions to string fields
      // 2. Redacting (sanitizing) all string fields via sanitizeContent() before export.
      //    This prevents prompt-injected content from leaking URLs, mentions, or harmful
      //    content into external action inputs.
      const processedInputs = {};
      for (const [key, value] of Object.entries(message)) {
        // Skip internal safe-output messaging fields that are not action inputs.
        // Maintained as an explicit set to allow future additions without silently
        // forwarding new internal fields to external action steps.
        if (INTERNAL_MESSAGE_FIELDS.has(key)) {
          continue;
        }

        if (typeof value === "string") {
          // Apply temporary ID reference substitution (e.g., "aw_abc1" → "42"), then
          // sanitize to redact malicious URLs, neutralize bot-trigger phrases, and
          // escape @mentions that could cause unintended notifications in the action.
          const substituted = replaceTemporaryIdReferences(value, temporaryIdMap);
          processedInputs[key] = sanitizeContent(substituted);
        } else {
          processedInputs[key] = value;
        }
      }

      // Export the processed payload as a step output
      const payloadJSON = JSON.stringify(processedInputs);
      core.setOutput(outputKey, payloadJSON);
      core.info(`✓ Custom action "${actionName}": exported payload as "${outputKey}" (${payloadJSON.length} bytes)`);

      return {
        success: true,
        action_name: actionName,
        payload: payloadJSON,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to process custom action "${actionName}": ${errorMessage}`);
      return {
        success: false,
        error: `Failed to process custom action "${actionName}": ${errorMessage}`,
      };
    }
  };
}

module.exports = { main };
