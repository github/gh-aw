// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "remove_labels";

const { validateLabels } = require("./safe_output_validator.cjs");
const { getErrorMessage, getErrorStatus } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveSafeOutputIssueTarget } = require("./temporary_id.cjs");
const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");
const { normalizeIssueIntentLabelNames } = require("./issue_intents.cjs");
const { currentEffectPreparation, freezeEffectPlan, describeEffectPlan, sendPlannedEffect, settleEffect, effectPlanOutcome, EffectTransportError } = require("./safe_output_effect_plan.cjs");
const { authorizeSafeOutputEffectPlan } = require("./safe_output_execution.cjs");
const { observeIssueTarget: observeLabelTarget } = require("./safe_output_effect_targets.cjs");

/**
 * Exact-effect label removal. The approved plan equals the transmitted request
 * list; GitHub removes labels by name, one request at a time, with no atomic
 * precondition, so identities and filters are rechecked before every send.
 * @param {{preparation: object, githubClient: any, repoParts: {owner: string, repo: string}, itemRepo: string, itemNumber: number, contextType: string, labels: string[], requiredLabels: string[], requiredTitlePrefix: string}} options
 */
async function removeLabelsWithEffectPlan({ preparation, githubClient, repoParts, itemRepo, itemNumber, contextType, labels, requiredLabels, requiredTitlePrefix }) {
  const filtersPass = (observed, removed) => requiredLabels.every(required => observed.names.includes(required) || removed.has(required.toLowerCase())) && (!requiredTitlePrefix || observed.title.startsWith(requiredTitlePrefix));
  const initial = await observeLabelTarget(githubClient, repoParts, itemNumber);
  if (!filtersPass(initial, new Set())) {
    return { success: false, skipped: true, error: "Item does not match required-labels or required-title-prefix filter" };
  }
  // Labels already absent are filtered before freezing, matching the legacy
  // treatment of absent labels; they never become approved requests.
  // Case variants resolving to one label identity become one request (first occurrence).
  const seen = new Set();
  const planned = labels.filter(label => {
    const id = initial.labels.get(label.toLowerCase());
    if (id === undefined || seen.has(id)) return false;
    seen.add(id);
    return true;
  });
  if (planned.length === 0) {
    core.info(`No requested labels are present on ${contextType} #${itemNumber} in ${itemRepo}`);
    return { success: true, number: itemNumber, labelsRemoved: [], message: "No labels to remove", contextType };
  }
  const { identity } = initial;
  const labelIds = planned.map(label => /** @type {number} */ initial.labels.get(label.toLowerCase()));
  const handle = freezeEffectPlan(preparation, {
    targets: [
      { kind: "repository", repository: itemRepo, repository_id: identity.repository_id },
      { kind: identity.kind, repository: itemRepo, repository_id: identity.repository_id, number: itemNumber, resource_id: identity.resource_id, ...(identity.node_id ? { node_id: identity.node_id } : {}) },
      ...[...new Set(labelIds)].map(id => ({ kind: "label", repository: itemRepo, repository_id: identity.repository_id, resource_id: String(id) })),
    ],
    effects: planned.map(label => ({
      operation: "issues.removeLabel",
      path: `/repos/${encodeURIComponent(repoParts.owner)}/${encodeURIComponent(repoParts.repo)}/issues/${itemNumber}/labels/${encodeURIComponent(label)}`,
    })),
    preconditions: [
      { id: "target_identity", enforcement: "observed", observation: identity },
      { id: "label_identities", enforcement: "observed", observation: labelIds },
      { id: "item_filters", enforcement: "observed", observation: { required_labels: requiredLabels, required_title_prefix: requiredTitlePrefix } },
    ],
  });
  core.info(`Authorizing exact removal of ${planned.length} label(s) from ${contextType} #${itemNumber} in ${itemRepo}`);
  await authorizeSafeOutputEffectPlan(handle);

  /** @type {string[]} */
  const removedLabels = [];
  /** @type {Array<{label: string, error: string}>} */
  const failedLabels = [];
  /** @type {Set<string>} */
  const applied = new Set();
  let stopReason = "";
  for (let index = 0; index < planned.length; index++) {
    // Earlier approved removals are expected progress, not drift.
    let observed;
    try {
      observed = await observeLabelTarget(githubClient, repoParts, itemNumber);
    } catch {
      stopReason = "Label target could not be revalidated";
      break;
    }
    const unchanged =
      ["repository_id", "resource_id", "kind", "node_id"].every(field => observed.identity[field] === identity[field]) &&
      planned.every((label, position) => position < index || observed.labels.get(label.toLowerCase()) === labelIds[position]) &&
      filtersPass(observed, applied);
    if (!unchanged) {
      stopReason = "Label target, label identities or filters changed after authorization";
      break;
    }
    const label = planned[index];
    try {
      await sendPlannedEffect(handle, index, { client: githubClient });
      removedLabels.push(label);
      applied.add(label.toLowerCase());
    } catch (error) {
      if (!(error instanceof EffectTransportError) || !error.transmitted) {
        stopReason = error instanceof EffectTransportError && error.reason_code === "authorization_expired" ? "Authorization expired before the remaining approved removals" : "Approved removal could not be sent";
        break;
      }
      // A 404 or lost response is not proof of absence: reconcile identity and label state.
      /** @type {Awaited<ReturnType<typeof observeLabelTarget>> | null} */
      let reconciled = null;
      try {
        reconciled = await observeLabelTarget(githubClient, repoParts, itemNumber);
      } catch {
        reconciled = null;
      }
      const sameTarget = !!reconciled && reconciled.identity.repository_id === identity.repository_id && reconciled.identity.resource_id === identity.resource_id;
      if (reconciled && sameTarget && !reconciled.labels.has(label.toLowerCase())) {
        settleEffect(handle, index, "absent");
        applied.add(label.toLowerCase());
      } else if (sameTarget && error.reason_code === "rejected" && error.status !== 404) {
        failedLabels.push({ label, error: getErrorMessage(error.cause ?? error) });
      } else {
        settleEffect(handle, index, "indeterminate");
        stopReason = "Label removal outcome is indeterminate; inspect the live item before any further action. Remaining removals were not attempted.";
        break;
      }
    }
  }
  const outcome = effectPlanOutcome(handle);
  const notAttempted = describeEffectPlan(handle).effects.filter(effect => effect.status === "not_attempted" || effect.status === "blocked").length;
  if (removedLabels.length > 0) core.info(`Removed ${removedLabels.length} of ${planned.length} approved label(s) from ${contextType} #${itemNumber} in ${itemRepo}`);
  return {
    success: outcome === "confirmed",
    number: itemNumber,
    labelsRemoved: removedLabels,
    failedLabels: failedLabels.length > 0 ? failedLabels : undefined,
    contextType,
    ...(outcome === "confirmed" ? {} : { error: stopReason || `${failedLabels.length} approved label removal(s) were rejected`, labels_not_attempted: notAttempted }),
    ...(outcome === "partial" || outcome === "indeterminate" ? { operation_outcome: outcome } : {}),
  };
}

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
      const preparation = currentEffectPreparation(HANDLER_TYPE);
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

      let itemNumber;
      let contextType;

      if (target === "*") {
        const effectiveContext = resolveInvocationContext(context);
        const triggeringItemNumber = effectiveContext.eventPayload?.issue?.number ?? effectiveContext.eventPayload?.pull_request?.number;
        // Accept common aliases: issue_number, pr_number, and pull_number are normalised to item_number
        const targetResult = resolveSafeOutputIssueTarget({ message, resolvedTemporaryIds, repoParts, handlerType: HANDLER_TYPE });
        if (!targetResult.success) return targetResult;
        itemNumber = targetResult.number ?? triggeringItemNumber;
        contextType = effectiveContext.eventPayload?.pull_request ? "pull request" : "issue";
      } else if (target === "triggering") {
        const targetResult = resolveTarget({ targetConfig: target, item: message, context, itemType: HANDLER_TYPE, supportsPR: true });
        if (!targetResult.success) {
          if (targetResult.shouldFail === false) {
            core.warning(targetResult.error);
            return { success: false, skipped: true, reason: targetResult.error, error: targetResult.error };
          }
          return targetResult;
        }
        itemNumber = targetResult.number;
        contextType = targetResult.contextType;
      } else {
        const effectiveContext = resolveInvocationContext(context);
        itemNumber = Number(target);
        contextType = effectiveContext.eventPayload?.pull_request ? "pull request" : "issue";
      }

      itemNumber = Number(itemNumber);
      if (!Number.isInteger(itemNumber) || itemNumber <= 0) {
        const error = target !== "*" && target !== "triggering" ? "Invalid issue/PR number" : "No issue/PR number available";
        core.warning(error);
        return { success: false, error };
      }

      const requestedLabels = message.labels ?? [];
      core.info(preparation ? `Requested label removals: ${Array.isArray(requestedLabels) ? requestedLabels.length : 0}` : `Requested labels to remove: ${JSON.stringify(requestedLabels)}`);
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

      if (preparation && !isStaged) {
        return removeLabelsWithEffectPlan({ preparation, githubClient, repoParts, itemRepo, itemNumber, contextType: contextType || "issue", labels: uniqueLabels, requiredLabels, requiredTitlePrefix });
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
          const errorStatus = getErrorStatus(error);
          if (errorStatus === 404 || (errorStatus == null && errorMessage.includes("Label does not exist"))) {
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
