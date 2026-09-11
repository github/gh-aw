// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "update_issue";

const { resolveTarget, checkRequiredFilter } = require("./safe_output_helpers.cjs");
const { createUpdateHandlerFactory, createStandardResolveNumber, createStandardFormatResult } = require("./update_handler_factory.cjs");
const { buildUpdatedBody } = require("./update_pr_description_helpers.cjs");
const { buildCommonEntityUpdateData } = require("./update_entity_helpers.cjs");
const { loadTemporaryProjectMap, replaceTemporaryProjectReferences } = require("./temporary_id.cjs");
const { tryEnforceArrayLimit } = require("./limit_enforcement_helpers.cjs");
const { ERR_VALIDATION, ERR_API } = require("./error_codes.cjs");
const { fetchIssueState, mergeIssueState } = require("./safe_output_execution_metadata.cjs");
const { MAX_LABELS, MAX_ASSIGNEES } = require("./constants.cjs");
const { fetchAllRepoLabels } = require("./github_api_helpers.cjs");
const { buildIssueIntentLabelUpdates, getIssueIntentLabelNames, normalizeIssueIntentLabelSpecs } = require("./issue_intents.cjs");

/**
 * Execute the issue update API call
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {number} issueNumber - Issue number to update
 * @param {any} updateData - Data to update
 * @returns {Promise<any>} Updated issue
 */
async function executeIssueUpdate(github, context, issueNumber, updateData) {
  // Handle body operation (append/prepend/replace/replace-island)
  // Default to "append" to add footer with AI attribution
  const operation = updateData._operation || "append";
  let rawBody = updateData._rawBody;
  const includeFooter = updateData._includeFooter !== false; // Default to true
  const bodyFooter = updateData._bodyFooter;
  const titlePrefix = updateData._titlePrefix || "";
  const labelsWereProvided = updateData.labels !== undefined;
  const labelSpecs = labelsWereProvided ? normalizeIssueIntentLabelSpecs(updateData.labels) : undefined;
  const useIssueIntentLabels = Boolean(labelSpecs);

  // Remove internal fields
  const { _operation, _rawBody, _includeFooter, _bodyFooter, _titlePrefix, _workflowRepo, ...apiData } = updateData;
  if (labelSpecs) {
    apiData.labels = getIssueIntentLabelNames(labelSpecs);
  }
  if (useIssueIntentLabels) {
    delete apiData.labels;
  }

  /** @type {any | null} */
  let currentIssue = null;

  // Fetch current issue if needed (title prefix validation or body update)
  if (titlePrefix || rawBody !== undefined || useIssueIntentLabels) {
    const response = await github.rest.issues.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      issue_number: issueNumber,
    });
    currentIssue = response.data;

    // Validate title prefix if specified
    if (titlePrefix) {
      const currentTitle = currentIssue.title || "";
      if (!currentTitle.startsWith(titlePrefix)) {
        throw new Error(`${ERR_VALIDATION}: Issue title "${currentTitle}" does not start with required prefix "${titlePrefix}"`);
      }
      core.info(`✓ Title prefix validation passed: "${titlePrefix}"`);
    }

    if (rawBody !== undefined) {
      // Load and apply temporary project URL replacements FIRST
      // This resolves any temporary project IDs (e.g., #aw_abc123def456) to actual project URLs
      const temporaryProjectMap = loadTemporaryProjectMap();
      if (temporaryProjectMap.size > 0) {
        rawBody = replaceTemporaryProjectReferences(rawBody, temporaryProjectMap);
        core.debug(`Applied ${temporaryProjectMap.size} temporary project URL replacement(s)`);
      }

      const currentBody = currentIssue.body || "";

      apiData.body = buildUpdatedBody({
        context,
        currentBody,
        newContent: rawBody,
        operation,
        includeFooter,
        bodyFooter,
        workflowRepo: _workflowRepo,
        itemType: "issue",
      });

      core.info(`Will update body (length: ${apiData.body.length})`);
    }
  }

  // Resolve and validate label IDs before any writes to prevent partial updates:
  // if a label name doesn't exist, buildIssueIntentLabelUpdates throws here so the
  // REST update below is never attempted.
  /** @type {any} */
  let labelIntentUpdates = null;
  if (useIssueIntentLabels && labelSpecs) {
    const repoLabels = await fetchAllRepoLabels(github, context.repo.owner, context.repo.repo);
    const labelIdByName = new Map(repoLabels.map(label => [label.name.toLowerCase(), label.id]));
    labelIntentUpdates = buildIssueIntentLabelUpdates(labelSpecs, labelIdByName);
    core.info(`Validated ${labelIntentUpdates.length} label(s) for issue #${issueNumber}`);
  }

  /** @type {any} */
  let issue = currentIssue;
  if (Object.keys(apiData).length > 0) {
    const response = await github.rest.issues.update({
      owner: context.repo.owner,
      repo: context.repo.repo,
      issue_number: issueNumber,
      ...apiData,
    });
    issue = response.data;
  }

  if (useIssueIntentLabels && labelIntentUpdates) {
    const issueNodeId = issue?.node_id || currentIssue?.node_id;
    if (!issueNodeId) {
      throw new Error(`${ERR_API}: Failed to resolve GraphQL node ID for issue #${issueNumber}`);
    }

    core.info("Using GraphQL intent path for label update with GraphQL-Features header");
    const labels = labelIntentUpdates;
    core.info(`Updating ${labels.length} label(s) on issue #${issueNumber} via GraphQL intent mutation`);
    const result = await github.graphql(
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
      { issueId: issueNodeId, labels, headers: { "GraphQL-Features": "update_issue_suggestions" } }
    );

    issue = {
      ...(issue || currentIssue || {}),
      labels: result?.updateIssue?.issue?.labels?.nodes || [],
    };
  }

  return issue;
}

/**
 * Resolve issue number from message and configuration
 * Uses the standard resolve helper for consistency with update_pull_request
 */
const resolveIssueNumber = createStandardResolveNumber({
  itemType: "update_issue",
  itemNumberField: "issue_number",
  supportsPR: false, // Not used when supportsIssue is true
  supportsIssue: true, // update_issue only supports issues, not PRs
});

/**
 * Build update data from message
 * @param {Object} item - The message item
 * @param {Object} config - Configuration object
 * @returns {{success: true, data: Object} | {success: false, error: string}} Update data result
 */
function buildIssueUpdateData(item, config) {
  // hasCommonUpdates is not needed here: the issue handler always continues to check
  // entity-specific fields (state, labels, assignees, milestone, title prefix).
  const { updateData } = buildCommonEntityUpdateData(item, config, {
    defaultOperation: "append",
    onBodyDisallowed: () => {
      core.warning("Body update not allowed by safe-outputs configuration");
    },
  });

  // The safe-outputs schema uses "status" (open/closed), while the GitHub API uses "state".
  // Accept both for compatibility.
  if (item.state !== undefined) {
    updateData.state = item.state;
  } else if (item.status !== undefined) {
    updateData.state = item.status;
  }
  if (item.labels !== undefined) {
    updateData.labels = item.labels;
  }
  if (item.assignees !== undefined) {
    updateData.assignees = item.assignees;
  }
  if (item.milestone !== undefined) {
    updateData.milestone = item.milestone;
  }

  // Enforce max limits on labels and assignees before API calls
  const labelsLimitResult = tryEnforceArrayLimit(updateData.labels, MAX_LABELS, "labels");
  if (!labelsLimitResult.success) {
    core.warning(`Issue update limit exceeded: ${labelsLimitResult.error}`);
    return { success: false, error: labelsLimitResult.error };
  }

  const assigneesLimitResult = tryEnforceArrayLimit(updateData.assignees, MAX_ASSIGNEES, "assignees");
  if (!assigneesLimitResult.success) {
    core.warning(`Issue update limit exceeded: ${assigneesLimitResult.error}`);
    return { success: false, error: assigneesLimitResult.error };
  }

  // Store title prefix for validation in executeIssueUpdate
  if (config.title_prefix) {
    updateData._titlePrefix = config.title_prefix;
  }

  return { success: true, data: updateData };
}

/**
 * Format success result for issue update
 * Uses the standard format helper for consistency across update handlers
 */
const formatIssueSuccessResult = createStandardFormatResult({
  numberField: "number",
  urlField: "url",
  urlSource: "html_url",
});

/**
 * Main handler factory for update_issue
 * Returns a message handler function that processes individual update_issue messages
 * @type {HandlerFactoryFunction}
 */
const main = createUpdateHandlerFactory({
  itemType: "update_issue",
  itemTypeName: "issue",
  supportsPR: false, // Not used by factory, but kept for documentation
  resolveItemNumber: resolveIssueNumber,
  buildUpdateData: buildIssueUpdateData,
  executeUpdate: executeIssueUpdate,
  formatSuccessResult: formatIssueSuccessResult,
  captureExecutionMetadata: {
    captureBefore: async (githubClient, effectiveContext, issueNumber) => fetchIssueState(githubClient, effectiveContext.repo, issueNumber),
    captureAfter: async (updatedIssue, beforeState) => mergeIssueState(beforeState, updatedIssue),
  },
  itemFilter: async (githubClient, repoParts, issueNumber, config) => {
    const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
    const requiredTitlePrefix = config.required_title_prefix || "";
    return checkRequiredFilter(githubClient, repoParts, issueNumber, requiredLabels, requiredTitlePrefix, "update_issue");
  },
});

module.exports = { main, buildIssueUpdateData };
