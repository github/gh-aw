// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTarget, isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { attachExecutionState } = require("./safe_output_execution_metadata.cjs");
const { withRetry, isTransientError } = require("./error_recovery.cjs");
const { loadTemporaryIdMapFromResolved, resolveRepoIssueTarget } = require("./temporary_id.cjs");

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
 * @property {Function} [itemFilter] - Optional async filter function called before update; returns null to proceed or a result object to skip
 * @property {{captureBefore?: Function, captureAfter?: Function}} [captureExecutionMetadata] - Optional execution metadata hooks
 */

/**
 * Creates a standard resolve number function for issue/PR handlers that use resolveTarget helper
 * This factory eliminates duplication between update_issue and update_pull_request
 *
 * @param {Object} config - Configuration for the resolve function
 * @param {string} config.itemType - Type of item (e.g., "update_issue", "update_pull_request")
 * @param {string} config.itemNumberField - Field name in item object (e.g., "issue_number", "pull_request_number")
 * @param {boolean} config.supportsPR - Whether this handler supports PR context
 * @param {boolean} config.supportsIssue - Whether this handler supports issue context
 * @returns {Function} Resolve number function
 */
function createStandardResolveNumber(config) {
  const { itemType, itemNumberField, supportsPR, supportsIssue } = config;

  return function resolveNumber(item, updateTarget, context, resolvedTemporaryIds) {
    // Resolve model-provided temporary IDs only when wildcard targeting allows them.
    let resolvedItem = item;
    const itemNumberValue = item[itemNumberField];
    if (updateTarget === "*" && resolvedTemporaryIds && itemNumberValue != null) {
      const tempIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds);
      const resolvedTarget = resolveRepoIssueTarget(itemNumberValue, tempIdMap, context.repo.owner, context.repo.repo);
      if (resolvedTarget.wasTemporaryId && resolvedTarget.resolved) {
        resolvedItem = { ...item, [itemNumberField]: resolvedTarget.resolved.number };
        core.info(`Resolved temporary ID '${itemNumberValue}' to #${resolvedTarget.resolved.number}`);
      } else if (resolvedTarget.wasTemporaryId && !resolvedTarget.resolved) {
        return {
          success: false,
          deferred: true,
          error: resolvedTarget.errorMessage || `Unresolved temporary ID: ${itemNumberValue}`,
        };
      }
    }

    const targetResult = resolveTarget({
      targetConfig: updateTarget,
      item: { ...resolvedItem, item_number: resolvedItem[itemNumberField] },
      context: context,
      itemType: itemType,
      supportsPR: supportsPR,
      supportsIssue: supportsIssue,
    });

    if (!targetResult.success) {
      return { success: false, shouldFail: targetResult.shouldFail, error: targetResult.error };
    }

    return { success: true, number: targetResult.number };
  };
}

/**
 * Creates a standard format success result function
 * This factory eliminates duplication across all update handlers
 *
 * @param {Object} fieldMapping - Mapping of result fields
 * @param {string} fieldMapping.numberField - Field name for number (e.g., "number", "pull_request_number")
 * @param {string} fieldMapping.urlField - Field name for URL (e.g., "url", "pull_request_url")
 * @param {string} fieldMapping.urlSource - Source field in updated item (e.g., "html_url", "url")
 * @returns {Function} Format success result function
 */
function createStandardFormatResult(fieldMapping) {
  const { numberField, urlField, urlSource } = fieldMapping;

  return function formatSuccessResult(itemNumber, updatedItem) {
    const result = {
      success: true,
      [numberField]: itemNumber,
      [urlField]: updatedItem[urlSource],
      title: updatedItem.title,
    };
    return result;
  };
}

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
  const { itemType, itemTypeName, supportsPR, resolveItemNumber, buildUpdateData, executeUpdate, formatSuccessResult, additionalConfig = {}, itemFilter = null, captureExecutionMetadata = null } = handlerConfig;

  /**
   * Main handler factory
   * @type {HandlerFactoryFunction}
   */
  return async function main(config = {}) {
    // Extract configuration with defaults
    const updateTarget = config.target || "triggering";
    const maxCount = config.max || 10;

    // Create an authenticated GitHub client. Uses config["github-token"] when set
    // (for cross-repository operations), otherwise falls back to the step-level github.
    const githubClient = await createAuthenticatedGitHubClient(config);

    // Resolve default target repo and allowed repos for cross-repository routing.
    // If no target-repo is configured, defaults to the current repository.
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

    // Check if we're in staged mode
    const isStaged = isStagedMode(config);

    const configParts = [
      `max=${maxCount}`,
      `target=${updateTarget}`,
      ...Object.entries(additionalConfig)
        .filter(([key]) => config[key] !== undefined)
        .map(([key]) => `${key}=${config[key]}`),
    ];

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

      // Resolve cross-repo target: always validate the target repository against the
      // allowed repos and use it as the effective context. When item.repo is set it
      // overrides the default; otherwise defaultTargetRepo is used. This mirrors the
      // add_comment routing behaviour and ensures target-repo config is honoured even
      // when the agent does not explicitly provide a repo field.
      // Using {any} type to allow partial context override (effectiveContext.repo may differ from context.repo).
      const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, itemTypeName);
      if (!repoResult.success) {
        core.warning(repoResult.error);
        return { success: false, error: repoResult.error };
      }
      /** @type {any} */
      const effectiveContext = { ...context, repo: repoResult.repoParts };
      // Log cross-repo routing when the agent explicitly set a repo field,
      // or when the resolved repo differs from the current workflow's repository.
      const workflowRepo = `${context.repo.owner}/${context.repo.repo}`;
      if (item.repo || repoResult.repo !== workflowRepo) {
        core.info(`Cross-repo update: targeting ${repoResult.repo}`);
      }

      // Resolve item number (may use custom logic)
      const itemNumberResult = resolveItemNumber(item, updateTarget, effectiveContext, resolvedTemporaryIds);

      if (!itemNumberResult.success) {
        // shouldFail:false means the target cannot be resolved in this context but it is not
        // an error (e.g. target:triggering on a schedule run has no triggering issue).
        // Treat it as a soft skip so it does not count toward fatal failures.
        if (itemNumberResult.shouldFail === false) {
          core.info(itemNumberResult.error);
          return {
            success: false,
            skipped: true,
            error: itemNumberResult.error,
          };
        }
        core.warning(itemNumberResult.error);
        return {
          success: false,
          deferred: itemNumberResult.deferred || false,
          error: itemNumberResult.error,
        };
      }

      const itemNumber = itemNumberResult.number;
      core.info(`Resolved target ${itemTypeName} #${itemNumber} (target config: ${updateTarget})`);

      // Apply required-labels/required-title-prefix filter if configured
      if (itemFilter) {
        const filterResult = await itemFilter(githubClient, repoResult.repoParts, itemNumber, config);
        if (filterResult) {
          return filterResult;
        }
      }

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

      // Store the original workflow repo in updateData so that executeUpdate functions
      // can build correct attribution URLs. effectiveContext.repo may be overridden to
      // the cross-repo target, but the run URL must always reference the current workflow.
      updateData._workflowRepo = context.repo;

      // Validate that we have something to update
      // Note: Fields starting with "_" are internal (e.g., _rawBody, _operation)
      // and will be processed by executeUpdate. We should NOT skip if _rawBody exists.
      const updateFields = Object.keys(updateData).filter(k => !k.startsWith("_"));
      const hasRawBody = updateData._rawBody !== undefined;

      // Sanitize body content before forwarding to the GitHub API (safe outputs conformance)
      if (hasRawBody) {
        updateData._rawBody = sanitizeContent(updateData._rawBody);
      }

      if (updateFields.length === 0 && !hasRawBody) {
        core.info(`No update fields provided for ${itemTypeName} #${itemNumber} - treating as no-op (skipping update)`);
        return {
          success: true,
          skipped: true,
          reason: "No update fields provided",
        };
      }

      // Include "body" in logged fields when a body update is queued (stored as internal _rawBody)
      const logFields = hasRawBody ? [...updateFields, "body"] : updateFields;
      core.info(`Updating ${itemTypeName} #${itemNumber} in ${effectiveContext.repo.owner}/${effectiveContext.repo.repo} with: ${JSON.stringify(logFields)}`);

      // If in staged mode, preview the update without applying it
      if (isStaged) {
        logStagedPreviewInfo(`Would update ${itemTypeName} #${itemNumber} with fields: ${JSON.stringify(logFields)}`);
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

      // Execute the update using the authenticated client and effective context.
      // githubClient uses config["github-token"] when set (for cross-repo), otherwise global github.
      // effectiveContext.repo contains the target repo owner/name for cross-repo routing.
      // Retry on transient errors (e.g. GitHub API returning HTML instead of JSON on 500 crashes).
      try {
        const beforeState = captureExecutionMetadata?.captureBefore ? await captureExecutionMetadata.captureBefore(githubClient, effectiveContext, itemNumber, updateData) : null;
        const updatedItem = await withRetry(() => executeUpdate(githubClient, effectiveContext, itemNumber, updateData), { maxRetries: 1, initialDelayMs: 2000, shouldRetry: isTransientError }, `update ${itemTypeName} #${itemNumber}`);
        core.info(`Successfully updated ${itemTypeName} #${itemNumber}: ${updatedItem.html_url || updatedItem.url}`);

        // Format and return success result
        const result = {
          ...formatSuccessResult(itemNumber, updatedItem),
          repo: `${effectiveContext.repo.owner}/${effectiveContext.repo.repo}`,
        };
        const afterState = captureExecutionMetadata?.captureAfter ? await captureExecutionMetadata.captureAfter(updatedItem, beforeState, updateData) : null;
        return attachExecutionState(result, beforeState, afterState);
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

module.exports = {
  createUpdateHandlerFactory,
  createStandardResolveNumber,
  createStandardFormatResult,
};
