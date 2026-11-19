// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 * @typedef {import('./types/handler-factory').ResolvedTemporaryIds} ResolvedTemporaryIds
 * @typedef {import('./types/handler-factory').HandlerResult} HandlerResult
 */

/**
 * @typedef {{
 *   item_number?: number|string,
 *   issue_number?: number|string,
 *   pr_number?: number|string,
 *   pull_number?: number|string,
 *   label_to_remove: string,
 *   label_to_add: string,
 *   repo?: string
 * }} ReplaceLabelMessage
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "replace_label";

const { matchesSimpleGlob } = require("./glob_pattern_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { hardenUnicodeText } = require("./sanitize_content_core.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveSafeOutputIssueTarget } = require("./temporary_id.cjs");
const { attachExecutionState, fetchIssueState, normalizeLabelNames } = require("./safe_output_execution_metadata.cjs");
const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");
const { currentEffectPreparation, freezeEffectPlan, executeEffectPlan } = require("./safe_output_effect_plan.cjs");
const { authorizeSafeOutputEffectPlan } = require("./safe_output_execution.cjs");
const { observeIssueTarget, issueTargets, repositoryPath } = require("./safe_output_effect_targets.cjs");

const POLICY_REJECTION_ERROR_NAME = "ReplaceLabelPolicyRejectionError";
const SET_LABELS_RETRY_CONFIG = {
  ...RATE_LIMIT_RETRY_CONFIG,
  shouldRetry: error => {
    const status = error?.response?.status ?? error?.status ?? null;
    const retryableHttpStatus = typeof status === "number" && status >= 500 && status < 600;
    const retryableTransportError = status == null && RATE_LIMIT_RETRY_CONFIG.shouldRetry(error);
    return error?.name !== POLICY_REJECTION_ERROR_NAME && (retryableHttpStatus || retryableTransportError || RATE_LIMIT_RETRY_CONFIG.shouldRetry(error));
  },
};

/**
 * Validate a single label against blocked and allowed-list patterns.
 * Uses explicit rejection semantics — does not silently filter or truncate the label name.
 * Blocked patterns are evaluated first (security boundary), consistent with safe_output_validator.cjs.
 *
 * @param {string} labelName - Label name to validate
 * @param {string[]} allowedPatterns - Allowlist patterns (empty = all labels allowed)
 * @param {string[]} blockedPatterns - Blocklist patterns
 * @param {string} fieldName - Field name for error messages (e.g. "label_to_add")
 * @returns {{valid: true} | {valid: false, error: string}}
 */
function validateSingleLabel(labelName, allowedPatterns, blockedPatterns, fieldName) {
  const normalizedLabelName = hardenUnicodeText(labelName);
  if (blockedPatterns.length > 0) {
    const isBlocked = blockedPatterns.some(pattern => {
      if (matchesSimpleGlob(labelName, pattern)) return true;
      // Only asterisks present in the original pattern are glob operators.
      const normalizedPattern = pattern
        .split("*")
        .map(part => hardenUnicodeText(part).replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
        .join(".*");
      // eslint-disable-next-line gh-aw-custom/require-escaped-regexp-interpolation -- literal fragments are escaped before joining the original wildcard operators
      return !!normalizedLabelName && !!normalizedPattern && new RegExp(`^${normalizedPattern}$`, "i").test(normalizedLabelName);
    });
    if (isBlocked) {
      return { valid: false, error: `${fieldName} "${labelName}" matches a blocked pattern` };
    }
  }
  if (allowedPatterns.length > 0) {
    const isAllowed = allowedPatterns.some(pattern => matchesSimpleGlob(labelName, pattern));
    if (!isAllowed) {
      return { valid: false, error: `${fieldName} "${labelName}" is not in the allowed list` };
    }
  }
  return { valid: true };
}

/**
 * Exact-effect replacement: one PUT of the complete finalized label set.
 * setLabels replaces the whole set without an atomic precondition, so the
 * observed set and filters are rechecked immediately before sending.
 * @param {{preparation: object, githubClient: any, repoParts: {owner: string, repo: string}, itemRepo: string, itemNumber: number, contextType: string, labelToRemove: string, labelToAdd: string, requiredLabels: string[], requiredTitlePrefix: string}} options
 */
async function replaceLabelWithEffectPlan({ preparation, githubClient, repoParts, itemRepo, itemNumber, contextType, labelToRemove, labelToAdd, requiredLabels, requiredTitlePrefix }) {
  const observed = await observeIssueTarget(githubClient, repoParts, itemNumber);
  const filtersPass = target => requiredLabels.every(required => target.names.includes(required)) && (!requiredTitlePrefix || target.title.startsWith(requiredTitlePrefix));
  if (!filtersPass(observed)) return { success: false, skipped: true, error: "Item does not match required-labels or required-title-prefix filter" };
  const labelToRemoveIsPresent = observed.names.includes(labelToRemove);
  const desired = [...new Set([...observed.names.filter(name => name !== labelToRemove), labelToAdd])];
  const labelSet = [...observed.labels.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  const handle = freezeEffectPlan(preparation, {
    targets: issueTargets(observed, itemRepo, itemNumber),
    effects: [{ operation: "issues.setLabels", path: `${repositoryPath(repoParts.owner, repoParts.repo)}/issues/${itemNumber}/labels`, body: { labels: desired } }],
    preconditions: [
      { id: "target_identity", enforcement: "observed", observation: observed.identity },
      { id: "label_set", enforcement: "observed", observation: labelSet },
      { id: "item_filters", enforcement: "observed", observation: { required_labels: requiredLabels, required_title_prefix: requiredTitlePrefix } },
    ],
  });
  await authorizeSafeOutputEffectPlan(handle);
  /** @type {any} */
  let updated = null;
  const run = await executeEffectPlan(handle, {
    client: githubClient,
    beforeEach: async () => {
      const current = await observeIssueTarget(githubClient, repoParts, itemNumber);
      const currentSet = [...current.labels.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
      if (current.identity.resource_id !== observed.identity.resource_id || JSON.stringify(currentSet) !== JSON.stringify(labelSet) || !filtersPass(current)) throw new Error("drift");
    },
    reconcile: async (_index, error) => {
      if (error?.reason_code !== "indeterminate") return null;
      const current = await observeIssueTarget(githubClient, repoParts, itemNumber);
      const same = current.identity.resource_id === observed.identity.resource_id && JSON.stringify([...current.names].sort()) === JSON.stringify([...desired].sort());
      return same ? { status: "confirmed" } : null;
    },
    onConfirmed: (_index, response) => {
      updated = response?.data;
    },
  });
  if (run.outcome !== "confirmed") {
    return {
      success: false,
      error: run.stop_reason === "precondition_changed" ? "Label set or filters changed after authorization; fresh evaluation required" : "Label replacement outcome is not confirmed; inspect the live item before any further action",
      ...(run.outcome === "indeterminate" ? { operation_outcome: "indeterminate" } : {}),
    };
  }
  core.info(`Replaced label on ${contextType} #${itemNumber} in ${itemRepo} through the approved effect plan`);
  const updatedNames = Array.isArray(updated) ? updated.map(label => label?.name).filter(Boolean) : desired;
  return {
    success: true,
    number: itemNumber,
    repo: itemRepo,
    labelRemoved: labelToRemoveIsPresent ? labelToRemove : null,
    labelAdded: labelToAdd,
    contextType,
    labels: updatedNames,
  };
}

/**
 * Main handler factory for replace_label.
 * Uses a single REST API call (`issues.setLabels`) to replace one label with another.
 * @type {HandlerFactoryFunction}
 */
const main = createCountGatedHandler({
  handlerType: HANDLER_TYPE,
  setup: async (config, maxCount, isStaged) => {
    const target = config.target || "triggering";
    const currentAllowedAdd = () => (Array.isArray(config.allowed_add) ? config.allowed_add : []);
    const currentAllowedRemove = () => (Array.isArray(config.allowed_remove) ? config.allowed_remove : []);
    const currentBlockedPatterns = () => (Array.isArray(config.blocked) ? config.blocked : []);
    const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
    const requiredTitlePrefix = config.required_title_prefix || "";
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
    const githubClient = await createAuthenticatedGitHubClient(config);

    // Config keys use snake_case (set by the Go handler config builder)
    const initialAllowedAdd = currentAllowedAdd();
    const initialAllowedRemove = currentAllowedRemove();
    const initialBlockedPatterns = currentBlockedPatterns();
    /** @type {{from: string, to: string}[]} */
    const configAllowedTransitions = Array.isArray(config.allowed_transitions) ? config.allowed_transitions : [];

    core.info(`Replace label configuration: max=${maxCount}`);
    if (configAllowedTransitions.length > 0) core.info(`Allowed transitions: ${configAllowedTransitions.map(t => `"${t.from}" → "${t.to}"`).join(", ")}`);
    if (initialAllowedAdd.length > 0) core.info(`Allowed labels to add: ${initialAllowedAdd.join(", ")}`);
    if (initialAllowedRemove.length > 0) core.info(`Allowed labels to remove: ${initialAllowedRemove.join(", ")}`);
    if (initialBlockedPatterns.length > 0) core.info(`Blocked patterns: ${initialBlockedPatterns.join(", ")}`);
    if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
    if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);
    core.info(`Default target repo: ${defaultTargetRepo}`);
    if (allowedRepos.size > 0) core.info(`Allowed repos: ${[...allowedRepos].join(", ")}`);

    /**
     * Message handler function that processes a single replace_label message.
     * @param {ReplaceLabelMessage} message - The replace_label message to process
     * @param {ResolvedTemporaryIds} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
     * @returns {Promise<HandlerResult>} Result with success/error status
     */
    return async function handleReplaceLabel(message, resolvedTemporaryIds) {
      // Resolve and validate target repository
      const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "label");
      if (!repoResult.success) {
        core.warning(`Skipping replace_label: ${repoResult.error}`);
        return { success: false, error: repoResult.error };
      }
      const { repo: itemRepo, repoParts } = repoResult;
      core.info(`Target repository: ${itemRepo}`);

      let itemNumber;
      let contextType;

      if (target === "*") {
        const effectiveContext = resolveInvocationContext(context);
        const triggeringItemNumber = effectiveContext.eventPayload?.issue?.number ?? effectiveContext.eventPayload?.pull_request?.number;
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

      const labelToRemove = String(message.label_to_remove ?? "").trim();
      const labelToAdd = String(message.label_to_add ?? "").trim();

      core.info(`Requested label replacement for ${contextType} #${itemNumber}: "${labelToRemove}" → "${labelToAdd}"`);

      if (!labelToRemove || !labelToAdd) {
        const error = "Both label_to_remove and label_to_add must be provided and non-empty";
        core.warning(error);
        return { success: false, error };
      }

      // Validate label_to_remove against blocked patterns and allowed-remove list
      const removeValidation = validateSingleLabel(labelToRemove, initialAllowedRemove, initialBlockedPatterns, "label_to_remove");
      if (!removeValidation.valid) {
        core.warning(`label_to_remove validation failed: ${removeValidation.error}`);
        return { success: false, error: removeValidation.error };
      }

      // Validate label_to_add against blocked patterns and allowed-add list
      const addValidation = validateSingleLabel(labelToAdd, initialAllowedAdd, initialBlockedPatterns, "label_to_add");
      if (!addValidation.valid) {
        core.warning(`label_to_add validation failed: ${addValidation.error}`);
        return { success: false, error: addValidation.error };
      }

      // Validate the (from, to) pair against the allowed-transitions list.
      // When allowed-transitions is configured, the pair must match at least one entry exactly.
      // This check is applied after individual label validation so blocked/allowlist guards
      // run first (they are security boundaries); transition validation is an additional
      // state-machine constraint on top of them.
      if (configAllowedTransitions.length > 0) {
        const transitionAllowed = configAllowedTransitions.some(t => t.from === labelToRemove && t.to === labelToAdd);
        if (!transitionAllowed) {
          const error = `Transition "${labelToRemove}" → "${labelToAdd}" is not in the allowed-transitions list`;
          core.warning(error);
          return { success: false, error };
        }
      }

      // Apply required-labels and required-title-prefix filters
      const { data: item } = await githubClient.rest.issues.get({
        owner: repoParts.owner,
        repo: repoParts.repo,
        issue_number: itemNumber,
      });

      if (requiredLabels.length > 0) {
        const itemLabels = (item.labels || []).map(/** @param {any} l */ l => (typeof l === "string" ? l : l.name || ""));
        if (!requiredLabels.every(r => itemLabels.includes(r))) {
          core.warning(`Skipping replace_label for ${contextType} #${itemNumber}: does not match required-labels filter (${requiredLabels.join(", ")})`);
          return { success: false, skipped: true, error: "Item does not match required-labels filter" };
        }
      }
      if (requiredTitlePrefix && !item.title?.startsWith(requiredTitlePrefix)) {
        core.warning(`Skipping replace_label for ${contextType} #${itemNumber}: title does not start with required prefix "${requiredTitlePrefix}"`);
        return { success: false, skipped: true, error: "Item title does not start with required prefix" };
      }

      // If in staged mode, preview the replacement without applying it
      if (isStaged) {
        logStagedPreviewInfo(`Would replace label "${labelToRemove}" → "${labelToAdd}" on ${contextType} #${itemNumber} in ${itemRepo}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            number: itemNumber,
            repo: itemRepo,
            labelToRemove,
            labelToAdd,
            contextType,
          },
        };
      }

      const preparation = currentEffectPreparation(HANDLER_TYPE);
      if (preparation) {
        return replaceLabelWithEffectPlan({ preparation, githubClient, repoParts, itemRepo, itemNumber, contextType: contextType || "issue", labelToRemove, labelToAdd, requiredLabels, requiredTitlePrefix });
      }

      // Compute the new label set: current labels minus labelToRemove, plus labelToAdd (deduped).
      // If labelToRemove is not on the issue we still proceed — it simply won't appear in the set.
      const currentLabelNames = (item.labels || []).map(/** @param {any} l */ l => (typeof l === "string" ? l : l.name || "")).filter(Boolean);
      let labelToRemoveIsPresent = currentLabelNames.includes(labelToRemove);
      if (!labelToRemoveIsPresent) {
        core.info(`Label "${labelToRemove}" is not present on ${contextType} #${itemNumber} in ${itemRepo} — will only add "${labelToAdd}"`);
      }

      try {
        let beforeState;
        const { data: updatedLabels } = await withRetry(
          async () => {
            beforeState = await fetchIssueState(githubClient, repoParts, itemNumber);
            const preWriteRemoveValidation = validateSingleLabel(labelToRemove, currentAllowedRemove(), currentBlockedPatterns(), "label_to_remove");
            if (!preWriteRemoveValidation.valid) {
              core.warning(`label_to_remove validation failed before setLabels: ${preWriteRemoveValidation.error}`);
              const policyError = new Error(preWriteRemoveValidation.error);
              policyError.name = POLICY_REJECTION_ERROR_NAME;
              throw policyError;
            }
            const preWriteAddValidation = validateSingleLabel(labelToAdd, currentAllowedAdd(), currentBlockedPatterns(), "label_to_add");
            if (!preWriteAddValidation.valid) {
              core.warning(`label_to_add validation failed before setLabels: ${preWriteAddValidation.error}`);
              const policyError = new Error(preWriteAddValidation.error);
              policyError.name = POLICY_REJECTION_ERROR_NAME;
              throw policyError;
            }
            const beforeWriteLabelNames = normalizeLabelNames(beforeState.labels);
            labelToRemoveIsPresent = beforeWriteLabelNames.includes(labelToRemove);
            const newLabelNames = [...new Set([...beforeWriteLabelNames.filter(n => n !== labelToRemove), labelToAdd])];

            return githubClient.rest.issues.setLabels({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: itemNumber,
              labels: newLabelNames,
            });
          },
          SET_LABELS_RETRY_CONFIG,
          `replace_label on ${contextType} #${itemNumber} in ${itemRepo}`
        );

        const updatedLabelNames = (updatedLabels || []).map((/** @param {any} l */ l) => l.name || "").filter(Boolean);

        if (!updatedLabelNames.includes(labelToAdd)) {
          const error = `replace_label: label_to_add ${JSON.stringify(labelToAdd)} not found in POST-setLabels response`;
          core.error(error);
          return { success: false, error };
        }
        if (labelToRemoveIsPresent && labelToRemove !== labelToAdd && updatedLabelNames.includes(labelToRemove)) {
          const error = `replace_label: label_to_remove ${JSON.stringify(labelToRemove)} still present after setLabels call`;
          core.error(error);
          return { success: false, error };
        }

        core.info(`Successfully replaced label "${labelToRemove}" → "${labelToAdd}" on ${contextType} #${itemNumber} in ${itemRepo}`);
        core.info(`Updated labels: ${JSON.stringify(updatedLabelNames)}`);

        return attachExecutionState(
          {
            success: true,
            number: itemNumber,
            repo: itemRepo,
            labelRemoved: labelToRemoveIsPresent ? labelToRemove : null,
            labelAdded: labelToAdd,
            contextType,
          },
          beforeState,
          {
            ...beforeState,
            labels: updatedLabelNames.length > 0 ? updatedLabelNames : normalizeLabelNames(item.labels),
          }
        );
      } catch (err) {
        const errorMessage = getErrorMessage(err);
        core.error(`Failed to replace label: ${errorMessage}`);
        return { success: false, error: errorMessage };
      }
    };
  },
});

module.exports = { main };
