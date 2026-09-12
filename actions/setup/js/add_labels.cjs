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
 *   labels?: Array<string|{name: string, rationale?: string, confidence?: "LOW"|"MEDIUM"|"HIGH", suggest?: boolean}>,
 *   repo?: string
 * }} AddLabelsMessage
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "add_labels";

const { validateLabels } = require("./safe_output_validator.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { tryEnforceArrayLimit } = require("./limit_enforcement_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveSafeOutputIssueTarget } = require("./temporary_id.cjs");
const { attachExecutionState, fetchIssueState, normalizeLabelNames } = require("./safe_output_execution_metadata.cjs");
const { MAX_LABELS } = require("./constants.cjs");
const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { normalizeIssueIntentLabelInputs, buildIssueIntentLabelUpdates } = require("./issue_intents.cjs");
const { fetchAllRepoLabels } = require("./github_api_helpers.cjs");
const { SAFE_OUTPUT_E099 } = require("./error_codes.cjs");
const { deterministicLabelColor } = require("./create_labels.cjs");

/**
 * @param {{ rationale?: string, confidence?: string, suggest?: boolean } | null | undefined} spec
 * @returns {boolean}
 */
function hasLabelIntentMetadata(spec) {
  return Boolean(spec && (spec.rationale || spec.confidence || spec.suggest));
}

/**
 * Detect whether an item fetched via the REST issues endpoint is a pull request.
 * The endpoint returns a `pull_request` field for PRs, and PR node IDs start with "PR_".
 * @param {{ pull_request?: unknown } | null | undefined} issueData
 * @param {string} nodeId
 * @returns {boolean}
 */
function isPullRequestItem(issueData, nodeId) {
  return Boolean(issueData?.pull_request) || nodeId.startsWith("PR_");
}

/**
 * @param  {...Array<string|{ name?: string }>} labelGroups
 * @returns {string[]}
 */
function mergeLabelNames(...labelGroups) {
  const merged = [];
  const seenLower = new Set();
  for (const label of labelGroups.flatMap(group => normalizeLabelNames(group))) {
    const key = label.toLowerCase();
    if (!seenLower.has(key)) {
      seenLower.add(key);
      merged.push(label);
    }
  }
  return merged;
}

/**
 * Ensures the given label names exist in the target repository, creating any that are
 * missing. A 422 response from createLabel means the label already exists (e.g. a
 * concurrent creation) and is treated as success. Other creation errors are non-fatal
 * and logged as warnings, since the caller will surface a clear "label not found" error
 * later if the label is still required and missing.
 * @param {any} githubClient - GitHub API client
 * @param {{ owner: string, repo: string }} repoParts
 * @param {string[]} labelNames - Requested label names to ensure exist
 * @param {any} core - Actions core logging object
 * @returns {Promise<void>}
 */
async function ensureLabelsExist(githubClient, repoParts, labelNames, core) {
  if (labelNames.length === 0) {
    return;
  }

  const repoLabels = await fetchAllRepoLabels(githubClient, repoParts.owner, repoParts.repo);
  const existingNamesLower = new Set(repoLabels.map(label => label.name.toLowerCase()));
  const missingLabelNames = labelNames.filter(name => !existingNamesLower.has(name.toLowerCase()));

  if (missingLabelNames.length === 0) {
    return;
  }

  core.info(`Creating ${missingLabelNames.length} missing label(s) in ${repoParts.owner}/${repoParts.repo}: ${JSON.stringify(missingLabelNames)}`);
  for (const name of missingLabelNames) {
    try {
      await withRetry(
        () =>
          githubClient.rest.issues.createLabel({
            owner: repoParts.owner,
            repo: repoParts.repo,
            name,
            color: deterministicLabelColor(name),
            description: "",
          }),
        RATE_LIMIT_RETRY_CONFIG,
        `create label ${JSON.stringify(name)} in ${repoParts.owner}/${repoParts.repo}`
      );
      core.info(`Created missing label ${JSON.stringify(name)} in ${repoParts.owner}/${repoParts.repo}`);
    } catch (error) {
      if (error !== null && typeof error === "object" && /** @type {any} */ error.status === 422) {
        core.info(`Label ${JSON.stringify(name)} already exists in ${repoParts.owner}/${repoParts.repo}`);
      } else {
        core.warning(`Failed to create label ${JSON.stringify(name)} in ${repoParts.owner}/${repoParts.repo}: ${getErrorMessage(error)}`);
      }
    }
  }
}

/**
 * Apply labels with issue-intent metadata through the GraphQL updateIssue mutation.
 * That mutation replaces the issue's label set, so the requested specs are merged with the
 * issue's existing labels to preserve add-only semantics. Existing labels are sent without
 * intent metadata; newly requested labels carry their metadata.
 * @param {{
 *   githubClient: any,
 *   core: any,
 *   repoParts: { owner: string, repo: string },
 *   itemNumber: number,
 *   itemRepo: string,
 *   contextType: string,
 *   issueData: any,
 *   issueNodeId: string,
 *   labelSpecs: Array<{ name: string }>,
 * }} params
 * @returns {Promise<string[]>} The label names on the issue after the mutation and any recovery
 */
async function applyIssueIntentLabels({ githubClient, core, repoParts, itemNumber, itemRepo, contextType, issueData, issueNodeId, labelSpecs }) {
  const repoLabels = await fetchAllRepoLabels(githubClient, repoParts.owner, repoParts.repo);
  const labelIdByName = new Map(repoLabels.map(label => [label.name.toLowerCase(), label.id]));

  const existingLabelNames = normalizeLabelNames(issueData.labels || []);
  const labelSpecNamesLower = new Set(labelSpecs.map(spec => spec.name.toLowerCase()));
  const mergedSpecs = [...labelSpecs, ...existingLabelNames.filter(name => !labelSpecNamesLower.has(name.toLowerCase())).map(name => ({ name }))];

  const labelIntentUpdates = buildIssueIntentLabelUpdates(mergedSpecs, labelIdByName);

  core.info(`Adding ${labelSpecs.length} labels to ${contextType} #${itemNumber} in ${itemRepo} via GraphQL intent mutation`);
  // updateIssue accepts LabelUpdateInput (rationale/confidence/suggest), which is gated
  // by the "update_issue_suggestions" GraphQL feature flag.
  const intentHeaders = { "GraphQL-Features": "update_issue_suggestions" };
  const result = await withRetry(
    () =>
      githubClient.graphql(
        `mutation($issueId: ID!, $labels: [LabelUpdateInput!]!) {
          updateIssue(input: { id: $issueId, labels: $labels }) {
            issue {
              id
              labels(first: 100) {
                nodes {
                  name
                }
              }
            }
          }
        }`,
        { issueId: issueNodeId, labels: labelIntentUpdates, headers: intentHeaders }
      ),
    RATE_LIMIT_RETRY_CONFIG,
    `add_labels to ${contextType} #${itemNumber} in ${itemRepo}`
  );

  let afterLabels = normalizeLabelNames(result?.updateIssue?.issue?.labels?.nodes || []);
  const afterNamesLower = new Set(afterLabels.map(name => name.toLowerCase()));
  const missingExistingLabels = existingLabelNames.filter(name => !afterNamesLower.has(name.toLowerCase()));

  if (missingExistingLabels.length > 0) {
    core.warning(
      `The GraphQL intent mutation removed ${missingExistingLabels.length} pre-existing label(s) from ${contextType} #${itemNumber} in ${itemRepo}; restoring them via the REST add-labels endpoint: ${JSON.stringify(missingExistingLabels)}`
    );
    const { data: restoredLabels } = await withRetry(
      () =>
        githubClient.rest.issues.addLabels({
          owner: repoParts.owner,
          repo: repoParts.repo,
          issue_number: itemNumber,
          labels: missingExistingLabels,
        }),
      RATE_LIMIT_RETRY_CONFIG,
      `restore labels on ${contextType} #${itemNumber} in ${itemRepo}`
    );
    afterLabels = mergeLabelNames(existingLabelNames, afterLabels, restoredLabels);
  }

  return afterLabels;
}

/**
 * Main handler factory for add_labels
 * Uses shared count-gated scaffold for max-limit enforcement.
 * @type {HandlerFactoryFunction}
 */
const main = createCountGatedHandler({
  handlerType: HANDLER_TYPE,
  setup: async (config, maxCount, isStaged) => {
    const { allowed: allowedLabels = [], blocked: blockedPatterns = [] } = config;
    const target = config.target || "triggering";
    const issueIntentEnabled = config.issue_intent !== false;
    const issueIntentStrict = config.issue_intent === true; // strict mode: plain-string labels rejected, metadata required
    const createIfMissing = config.create_if_missing === true;
    const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
    const requiredTitlePrefix = config.required_title_prefix || "";
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
    const githubClient = await createAuthenticatedGitHubClient(config);

    core.info(`Add labels configuration: max=${maxCount}`);
    if (allowedLabels.length > 0) core.info(`Allowed labels: ${allowedLabels.join(", ")}`);
    if (blockedPatterns.length > 0) core.info(`Blocked patterns: ${blockedPatterns.join(", ")}`);
    if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
    if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);
    core.info(`Create missing labels: ${createIfMissing}`);
    core.info(`Default target repo: ${defaultTargetRepo}`);
    if (allowedRepos.size > 0) core.info(`Allowed repos: ${[...allowedRepos].join(", ")}`);

    /**
     * Message handler function that processes a single add_labels message
     * @param {AddLabelsMessage} message - The add_labels message to process
     * @param {ResolvedTemporaryIds} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
     * @returns {Promise<HandlerResult>} Result with success/error status
     */
    return async function handleAddLabels(message, resolvedTemporaryIds) {
      // Resolve and validate target repository
      const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "label");
      if (!repoResult.success) {
        core.warning(`Skipping add_labels: ${repoResult.error}`);
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
      core.info(`Requested labels: ${JSON.stringify(requestedLabels)}`);
      /** @type {Map<string, {name: string, rationale?: string, confidence?: "LOW"|"MEDIUM"|"HIGH", suggest?: boolean}>} */
      const requestedLabelSpecByLowerName = new Map();
      let requestedLabelNames;
      try {
        const requestedLabelInputs = normalizeIssueIntentLabelInputs(requestedLabels);

        // In strict mode (issue_intent: true), reject plain string labels — metadata is required
        if (issueIntentStrict) {
          const plainStringLabels = requestedLabelInputs.filter(label => typeof label === "string");
          if (plainStringLabels.length > 0) {
            const error = `Plain string label names are not permitted when issue_intent is explicitly enabled. Provide label objects with a "name" field and intent metadata (rationale, confidence). Plain labels: ${plainStringLabels.map(l => JSON.stringify(l)).join(", ")}`;
            core.warning(error);
            return { success: false, error };
          }
          // In strict mode, objects without rationale and confidence are also rejected
          const missingMetadataLabelNames = [];
          for (const label of requestedLabelInputs) {
            if (typeof label !== "object" || label === null) {
              continue;
            }
            if (!label.rationale || !label.confidence) {
              missingMetadataLabelNames.push(JSON.stringify(label.name ?? "<unnamed>"));
            }
          }
          if (missingMetadataLabelNames.length > 0) {
            const error = `Label objects must include both "rationale" and "confidence" when issue_intent is explicitly enabled. Missing metadata on: ${missingMetadataLabelNames.join(", ")}`;
            core.warning(error);
            return { success: false, error };
          }
        }

        requestedLabelNames = requestedLabelInputs.map(label => {
          if (typeof label === "string") {
            return label;
          }
          const key = label.name.toLowerCase();
          const existing = requestedLabelSpecByLowerName.get(key);
          const newHasMetadata = hasLabelIntentMetadata(label);
          const existingHasMetadata = hasLabelIntentMetadata(existing);
          if (!existing || (!existingHasMetadata && newHasMetadata)) {
            requestedLabelSpecByLowerName.set(key, label);
          }
          return label.name;
        });
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.warning(`Invalid add_labels payload: ${errorMessage}`);
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
          const missingLabels = requiredLabels.filter(r => !itemLabels.includes(r));
          if (missingLabels.length > 0) {
            core.info(`Skipping add_labels for ${contextType} #${itemNumber}: does not match required-labels filter (${requiredLabels.join(", ")})`);
            return {
              success: false,
              skipped: true,
              reasonCode: "REQUIRED_LABELS_MISMATCH",
              reason: "Required labels missing",
              error: "Item does not match required-labels filter",
              target: { repo: itemRepo, number: itemNumber },
              safeDetails: { requiredLabels, missingLabels },
            };
          }
        }
        if (requiredTitlePrefix && !item.title?.startsWith(requiredTitlePrefix)) {
          core.info(`Skipping add_labels for ${contextType} #${itemNumber}: title does not start with required prefix "${requiredTitlePrefix}"`);
          return {
            success: false,
            skipped: true,
            reasonCode: "REQUIRED_TITLE_PREFIX_MISMATCH",
            reason: "Required title prefix missing",
            error: "Item title does not start with required prefix",
            target: { repo: itemRepo, number: itemNumber },
            safeDetails: { requiredTitlePrefix },
          };
        }
      }

      // If no labels provided, return a helpful message with allowed labels if configured
      if (requestedLabelNames.length === 0) {
        const labelSource = allowedLabels.length > 0 ? `the allowed list: ${JSON.stringify(allowedLabels)}` : "the repository's available labels";
        const error = `No labels provided. Please provide at least one label from ${labelSource}`;
        core.info(error);
        return { success: false, error };
      }

      // Enforce max limits on labels before validation
      const limitResult = tryEnforceArrayLimit(requestedLabelNames, MAX_LABELS, "labels");
      if (!limitResult.success) {
        core.warning(`Label limit exceeded: ${limitResult.error}`);
        return { success: false, error: limitResult.error };
      }

      // Use validation helper to sanitize and validate labels
      const labelsResult = validateLabels(requestedLabelNames, allowedLabels, maxCount, blockedPatterns);

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

      // Build the resolved label specs (name + optional intent metadata) for the validated
      // unique labels, preserving the order returned by validation.
      const uniqueLabelSpecs = uniqueLabels.map(name => requestedLabelSpecByLowerName.get(name.toLowerCase()) ?? { name });
      const intentLabelSpecs = uniqueLabelSpecs.filter(spec => hasLabelIntentMetadata(spec));
      const useIssueIntentPath = issueIntentEnabled && intentLabelSpecs.length > 0;

      // The REST issues.addLabels endpoint only accepts label name strings; it does not
      // support issue-intent metadata (rationale/confidence/suggest). Passing objects with
      // those extra keys causes GitHub to return success while silently applying no labels.
      // When intent metadata is present, route through the GraphQL updateIssue/LabelUpdateInput
      // mutation instead (see update_issue.cjs), which does support intent metadata.
      const labelsRequestPayload = uniqueLabels;

      core.info(`Adding ${uniqueLabels.length} labels to ${contextType} #${itemNumber} in ${itemRepo}: ${JSON.stringify(labelsRequestPayload)}`);

      // If in staged mode, preview the labels without adding them
      if (isStaged) {
        logStagedPreviewInfo(`Would add ${uniqueLabels.length} labels to ${contextType} #${itemNumber} in ${itemRepo}`);
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

      try {
        const beforeState = await fetchIssueState(githubClient, repoParts, itemNumber);

        if (createIfMissing) {
          await ensureLabelsExist(githubClient, repoParts, uniqueLabels, core);
        }

        if (useIssueIntentPath) {
          // Intent metadata is only supported via the GraphQL updateIssue mutation.
          const { data: issueData } = await withRetry(
            () =>
              githubClient.rest.issues.get({
                owner: repoParts.owner,
                repo: repoParts.repo,
                issue_number: itemNumber,
              }),
            RATE_LIMIT_RETRY_CONFIG,
            `get ${contextType} #${itemNumber} in ${itemRepo}`
          );

          const issueNodeId = issueData?.node_id;
          if (!issueNodeId) {
            throw new Error(`${SAFE_OUTPUT_E099}: Failed to resolve GraphQL node ID for ${contextType} #${itemNumber}`);
          }

          // The GraphQL updateIssue mutation only accepts Issue node IDs, and
          // UpdatePullRequestInput does not accept a `labels` field, so there is no
          // intent-aware mutation for PRs. Fall back to the REST add-labels endpoint
          // (add-only, without intent metadata).
          if (isPullRequestItem(issueData, issueNodeId)) {
            core.info(`Issue-intent label metadata is not supported for pull requests; falling back to the REST add-labels endpoint for ${contextType} #${itemNumber} in ${itemRepo}`);
          } else {
            const existingLabels = normalizeLabelNames(issueData.labels || []);
            const existingNamesLower = new Set(existingLabels.map(name => name.toLowerCase()));
            const newLabelSpecs = uniqueLabelSpecs.filter(spec => !existingNamesLower.has(spec.name.toLowerCase()));
            let afterLabels =
              newLabelSpecs.length > 0
                ? await applyIssueIntentLabels({
                    githubClient,
                    core,
                    repoParts,
                    itemNumber,
                    itemRepo,
                    contextType,
                    issueData,
                    issueNodeId,
                    labelSpecs: newLabelSpecs,
                  })
                : existingLabels;
            let afterNamesLower = new Set(afterLabels.map(name => name.toLowerCase()));
            const plainLabelsNotApplied = newLabelSpecs.filter(spec => !hasLabelIntentMetadata(spec) && !afterNamesLower.has(spec.name.toLowerCase())).map(spec => spec.name);

            if (plainLabelsNotApplied.length > 0) {
              core.info(`Adding ${plainLabelsNotApplied.length} metadata-free label(s) not applied by the intent mutation via the REST add-labels endpoint: ${JSON.stringify(plainLabelsNotApplied)}`);
              const { data: labels } = await withRetry(
                () =>
                  githubClient.rest.issues.addLabels({
                    owner: repoParts.owner,
                    repo: repoParts.repo,
                    issue_number: itemNumber,
                    labels: plainLabelsNotApplied,
                  }),
                RATE_LIMIT_RETRY_CONFIG,
                `add metadata-free labels to ${contextType} #${itemNumber} in ${itemRepo}`
              );
              afterLabels = mergeLabelNames(afterLabels, labels);
              afterNamesLower = new Set(afterLabels.map(name => name.toLowerCase()));
            }

            const labelsAdded = newLabelSpecs.filter(spec => afterNamesLower.has(spec.name.toLowerCase())).map(spec => spec.name);
            const labelsSuggested = newLabelSpecs.filter(spec => hasLabelIntentMetadata(spec) && !afterNamesLower.has(spec.name.toLowerCase())).map(spec => spec.name);

            if (newLabelSpecs.length === 0) {
              core.info(`No new labels to add to ${contextType} #${itemNumber} in ${itemRepo}`);
            }
            core.info(`Successfully added ${labelsAdded.length} labels to ${contextType} #${itemNumber} in ${itemRepo}`);
            if (labelsSuggested.length > 0) {
              core.info(`Suggested ${labelsSuggested.length} labels for ${contextType} #${itemNumber} in ${itemRepo}: ${JSON.stringify(labelsSuggested)}`);
            }
            return attachExecutionState(
              {
                success: true,
                number: itemNumber,
                repo: itemRepo,
                labelsAdded,
                labelsSuggested,
                contextType,
              },
              beforeState,
              {
                ...beforeState,
                labels: afterLabels,
              }
            );
          }
        }

        const { data: labels } = await withRetry(
          () =>
            githubClient.rest.issues.addLabels({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: itemNumber,
              labels: labelsRequestPayload,
            }),
          RATE_LIMIT_RETRY_CONFIG,
          `add_labels to ${contextType} #${itemNumber} in ${itemRepo}`
        );

        core.info(`Successfully added ${uniqueLabels.length} labels to ${contextType} #${itemNumber} in ${itemRepo}`);
        return attachExecutionState(
          {
            success: true,
            number: itemNumber,
            repo: itemRepo,
            labelsAdded: uniqueLabels,
            contextType,
          },
          beforeState,
          {
            ...beforeState,
            labels: normalizeLabelNames(labels),
          }
        );
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.error(`Failed to add labels: ${errorMessage}`);
        return { success: false, error: errorMessage };
      }
    };
  },
});

module.exports = { main };
