// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "remove_labels";

const { validateLabels } = require("./safe_output_validator.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveSafeOutputIssueTarget } = require("./temporary_id.cjs");
const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { normalizeIssueIntentLabelNames } = require("./issue_intents.cjs");

/**
 * Main handler factory for remove_labels
 * Uses shared count-gated scaffold for max-limit enforcement.
 * @type {HandlerFactoryFunction}
 */
const main = createCountGatedHandler({
  handlerType: HANDLER_TYPE,
  setup: async (config, maxCount, isStaged) => {
    // Extract configuration
    const allowedLabels = config.allowed || [];
    const blockedPatterns = config.blocked || [];
    const target = config.target || "triggering";
    const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
    const requiredTitlePrefix = config.required_title_prefix || "";
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
    const githubClient = await createAuthenticatedGitHubClient(config);

    core.info(`Remove labels configuration: max=${maxCount}`);
    if (allowedLabels.length > 0) {
      core.info(`Allowed labels to remove: ${allowedLabels.join(", ")}`);
    }
    if (blockedPatterns.length > 0) {
      core.info(`Blocked patterns: ${blockedPatterns.join(", ")}`);
    }
    if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
    if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);
    core.info(`Default target repo: ${defaultTargetRepo}`);
    if (allowedRepos.size > 0) {
      core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
    }

    /**
     * Message handler function that processes a single remove_labels message
     * @param {Object} message - The remove_labels message to process
     * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
     * @returns {Promise<Object>} Result with success/error status
     */
    return async function handleRemoveLabels(message, resolvedTemporaryIds) {
      // Resolve and validate target repository
      const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "label");
      if (!repoResult.success) {
        core.warning(`Skipping remove_labels: ${repoResult.error}`);
        return {
          success: false,
          error: repoResult.error,
        };
      }
      const { repo: itemRepo, repoParts } = repoResult;
      core.info(`Target repository: ${itemRepo}`);

      const effectiveContext = resolveInvocationContext(context);
      const triggeringItemNumber = effectiveContext.eventPayload?.issue?.number ?? effectiveContext.eventPayload?.pull_request?.number;
      let itemNumber;

      if (target === "*") {
        // Accept common aliases: issue_number, pr_number, and pull_number are normalised to item_number
        const targetResult = resolveSafeOutputIssueTarget({ message, resolvedTemporaryIds, repoParts, handlerType: HANDLER_TYPE });
        if (!targetResult.success) return targetResult;
        itemNumber = targetResult.number ?? triggeringItemNumber;
      } else if (target === "triggering") {
        itemNumber = triggeringItemNumber;
      } else {
        itemNumber = Number(target);
      }

      itemNumber = Number(itemNumber);
      if (!Number.isInteger(itemNumber) || itemNumber <= 0) {
        const error = target !== "*" && target !== "triggering" ? "Invalid issue/PR number" : "No issue/PR number available";
        core.warning(error);
        return { success: false, error };
      }

      const contextType = effectiveContext.eventPayload?.pull_request ? "pull request" : "issue";
      const requestedLabels = message.labels ?? [];
      core.info(`Requested labels to remove: ${JSON.stringify(requestedLabels)}`);
      let requestedLabelNames;
      try {
        requestedLabelNames = normalizeIssueIntentLabelNames(requestedLabels);
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.warning(`Invalid remove_labels payload: ${errorMessage}`);
        return { success: false, error: errorMessage };
      }

      // Apply required-labels and required-title-prefix filters
      if (requiredLabels.length > 0 || requiredTitlePrefix) {
        const { data: item } = await githubClient.rest.issues.get({
          owner: repoParts.owner,
          repo: repoParts.repo,
          issue_number: itemNumber,
        });
        if (requiredLabels.length > 0) {
          const itemLabels = (item.labels || []).map(/** @param {any} l */ l => (typeof l === "string" ? l : l.name || ""));
          if (!requiredLabels.every(r => itemLabels.includes(r))) {
            core.info(`Skipping remove_labels for ${contextType} #${itemNumber}: does not match required-labels filter (${requiredLabels.join(", ")})`);
            return { success: false, skipped: true, error: `Item does not match required-labels filter` };
          }
        }
        if (requiredTitlePrefix && !item.title?.startsWith(requiredTitlePrefix)) {
          core.info(`Skipping remove_labels for ${contextType} #${itemNumber}: title does not start with required prefix "${requiredTitlePrefix}"`);
          return { success: false, skipped: true, error: `Item title does not start with required prefix` };
        }
      }

      // If no labels provided, return a helpful message with allowed labels if configured
      if (!requestedLabelNames || requestedLabelNames.length === 0) {
        let errorMessage = "No labels provided. Please provide at least one label from";
        if (allowedLabels.length > 0) {
          errorMessage += ` the allowed list: ${JSON.stringify(allowedLabels)}`;
        } else {
          errorMessage += " the issue/PR's current labels";
        }
        core.info(errorMessage);
        return {
          success: false,
          error: errorMessage,
        };
      }

      // Use validation helper to sanitize and validate labels
      const labelsResult = validateLabels(requestedLabelNames, allowedLabels, maxCount, blockedPatterns);
      if (!labelsResult.valid) {
        // If no valid labels, log info and return gracefully
        if (labelsResult.error?.includes("No valid labels")) {
          core.info("No labels to remove");
          return {
            success: true,
            number: itemNumber,
            labelsRemoved: [],
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

      if (uniqueLabels.length === 0) {
        core.info("No labels to remove");
        return {
          success: true,
          number: itemNumber,
          labelsRemoved: [],
          message: "No labels to remove",
        };
      }

      core.info(`Removing ${uniqueLabels.length} labels from ${contextType} #${itemNumber} in ${itemRepo}: ${JSON.stringify(uniqueLabels)}`);

      // If in staged mode, preview the label removal without actually removing
      if (isStaged) {
        logStagedPreviewInfo(`Would remove ${uniqueLabels.length} labels from ${contextType} #${itemNumber} in ${itemRepo}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            number: itemNumber,
            repo: itemRepo,
            labels: uniqueLabels,
            contextType,
          },
        };
      }

      // Track successfully removed labels
      const removedLabels = [];
      const failedLabels = [];

      // Remove labels one at a time (GitHub API doesn't have a bulk remove endpoint)
      for (const label of uniqueLabels) {
        try {
          await githubClient.rest.issues.removeLabel({
            owner: repoParts.owner,
            repo: repoParts.repo,
            issue_number: itemNumber,
            name: label,
          });
          removedLabels.push(label);
          core.info(`Removed label "${label}" from ${contextType} #${itemNumber} in ${itemRepo}`);
        } catch (error) {
          // Label might not exist on the issue/PR - this is not a failure
          const errorMessage = getErrorMessage(error);
          if (errorMessage.includes("Label does not exist") || errorMessage.includes("404")) {
            core.info(`Label "${label}" was not present on ${contextType} #${itemNumber} in ${itemRepo}, skipping`);
          } else {
            core.warning(`Failed to remove label "${label}": ${errorMessage}`);
            failedLabels.push({ label, error: errorMessage });
          }
        }
      }

      if (removedLabels.length > 0) {
        core.info(`Successfully removed ${removedLabels.length} labels from ${contextType} #${itemNumber} in ${itemRepo}`);
      }

      return {
        success: true,
        number: itemNumber,
        labelsRemoved: removedLabels,
        failedLabels: failedLabels.length > 0 ? failedLabels : undefined,
        contextType,
      };
    };
  },
});

module.exports = { main };
