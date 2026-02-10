// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "update_issue";

const { resolveTarget } = require("./safe_output_helpers.cjs");
const { createUpdateHandlerFactory } = require("./update_handler_factory.cjs");
const { updateBody } = require("./update_pr_description_helpers.cjs");
const { loadTemporaryProjectMap, replaceTemporaryProjectReferences } = require("./temporary_id.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");

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

  // Remove internal fields
  const { _operation, _rawBody, ...apiData } = updateData;

  // If we have a body, process it with the appropriate operation
  if (rawBody !== undefined) {
    // Load and apply temporary project URL replacements FIRST
    // This resolves any temporary project IDs (e.g., #aw_abc123def456) to actual project URLs
    const temporaryProjectMap = loadTemporaryProjectMap();
    if (temporaryProjectMap.size > 0) {
      rawBody = replaceTemporaryProjectReferences(rawBody, temporaryProjectMap);
      core.debug(`Applied ${temporaryProjectMap.size} temporary project URL replacement(s)`);
    }

    // Fetch current issue body for all operations (needed for append/prepend/replace-island/replace)
    const { data: currentIssue } = await github.rest.issues.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      issue_number: issueNumber,
    });
    const currentBody = currentIssue.body || "";

    // Get workflow run URL for AI attribution
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "GitHub Agentic Workflow";
    const runUrl = `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`;

    // Use helper to update body (handles all operations including replace)
    apiData.body = updateBody({
      currentBody,
      newContent: rawBody,
      operation,
      workflowName,
      runUrl,
      runId: context.runId,
    });

    core.info(`Will update body (length: ${apiData.body.length})`);
  }

  const { data: issue } = await github.rest.issues.update({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: issueNumber,
    ...apiData,
  });

  return issue;
}

/**
 * Resolve issue number from message and configuration
 * @param {Object} item - The message item
 * @param {string} updateTarget - Target configuration
 * @param {Object} context - GitHub Actions context
 * @returns {{success: true, number: number} | {success: false, error: string}} Resolution result
 */
function resolveIssueNumber(item, updateTarget, context) {
  const targetResult = resolveTarget({
    targetConfig: updateTarget,
    item: { ...item, item_number: item.issue_number },
    context: context,
    itemType: "update_issue",
    supportsPR: false, // Not used when supportsIssue is true
    supportsIssue: true, // update_issue only supports issues, not PRs
  });

  if (!targetResult.success) {
    return { success: false, error: targetResult.error };
  }

  return { success: true, number: targetResult.number };
}

/**
 * Build update data from message
 * @param {Object} item - The message item
 * @param {Object} config - Configuration object
 * @returns {{success: true, data: Object} | {success: false, error: string}} Update data result
 */
function buildIssueUpdateData(item, config) {
  const updateData = {};

  if (item.title !== undefined) {
    // Sanitize title for Unicode security (no prefix handling needed for updates)
    updateData.title = sanitizeTitle(item.title);
  }
  if (item.body !== undefined) {
    // Store operation information for consistent footer/append behavior.
    // Default to "append" so we preserve the original issue text.
    updateData._operation = item.operation || "append";
    updateData._rawBody = item.body;
  }
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

  return { success: true, data: updateData };
}

/**
 * Format success result for issue update
 * @param {number} issueNumber - Issue number
 * @param {Object} updatedIssue - Updated issue object
 * @returns {Object} Formatted success result
 */
function formatIssueSuccessResult(issueNumber, updatedIssue) {
  return {
    success: true,
    number: issueNumber,
    url: updatedIssue.html_url,
    title: updatedIssue.title,
  };
}

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
});

module.exports = { main, buildIssueUpdateData };
