// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "update_pull_request";

const { updateBody } = require("./update_pr_description_helpers.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");
const { createUpdateHandlerFactory } = require("./update_handler_factory.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");

/**
 * Execute the pull request update API call
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {number} prNumber - PR number to update
 * @param {any} updateData - Data to update
 * @returns {Promise<any>} Updated pull request
 */
async function executePRUpdate(github, context, prNumber, updateData) {
  // Handle body operation (append/prepend/replace/replace-island)
  const operation = updateData._operation || "replace";
  const rawBody = updateData._rawBody;

  // Remove internal fields
  const { _operation, _rawBody, ...apiData } = updateData;

  // If we have a body, process it with the appropriate operation
  if (rawBody !== undefined) {
    // Fetch current PR body for all operations (needed for append/prepend/replace-island/replace)
    const { data: currentPR } = await github.rest.pulls.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: prNumber,
    });
    const currentBody = currentPR.body || "";

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

  const { data: pr } = await github.rest.pulls.update({
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: prNumber,
    ...apiData,
  });

  return pr;
}

/**
 * Resolve PR number from message and configuration
 * @param {Object} item - The message item
 * @param {string} updateTarget - Target configuration
 * @param {Object} context - GitHub Actions context
 * @returns {{success: true, number: number} | {success: false, error: string}} Resolution result
 */
function resolvePRNumber(item, updateTarget, context) {
  const targetResult = resolveTarget({
    targetConfig: updateTarget,
    item: { ...item, item_number: item.pull_request_number },
    context: context,
    itemType: "update_pull_request",
    supportsPR: false, // update_pull_request only supports PRs, not issues
    supportsIssue: false,
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
 * @returns {{success: true, data: Object} | {success: true, skipped: true, reason: string} | {success: false, error: string}} Update data result
 */
function buildPRUpdateData(item, config) {
  const canUpdateTitle = config.allow_title !== false; // Default true
  const canUpdateBody = config.allow_body !== false; // Default true

  const updateData = {};
  let hasUpdates = false;

  if (canUpdateTitle && item.title !== undefined) {
    // Sanitize title for Unicode security (no prefix handling needed for updates)
    updateData.title = sanitizeTitle(item.title);
    hasUpdates = true;
  }

  if (canUpdateBody && item.body !== undefined) {
    // Store operation information
    // Use operation from item, or fall back to config default, or use "replace" as final default
    const operation = item.operation || config.default_operation || "replace";
    updateData._operation = operation;
    updateData._rawBody = item.body;
    updateData.body = item.body;
    hasUpdates = true;
  }

  // Other fields (always allowed)
  if (item.state !== undefined) {
    updateData.state = item.state;
    hasUpdates = true;
  }
  if (item.base !== undefined) {
    updateData.base = item.base;
    hasUpdates = true;
  }

  if (!hasUpdates) {
    return {
      success: true,
      skipped: true,
      reason: "No update fields provided or all fields are disabled",
    };
  }

  return { success: true, data: updateData };
}

/**
 * Format success result for PR update
 * @param {number} prNumber - PR number
 * @param {Object} updatedPR - Updated PR object
 * @returns {Object} Formatted success result
 */
function formatPRSuccessResult(prNumber, updatedPR) {
  return {
    success: true,
    pull_request_number: prNumber,
    pull_request_url: updatedPR.html_url,
    title: updatedPR.title,
  };
}

/**
 * Main handler factory for update_pull_request
 * Returns a message handler function that processes individual update_pull_request messages
 * @type {HandlerFactoryFunction}
 */
const main = createUpdateHandlerFactory({
  itemType: "update_pull_request",
  itemTypeName: "pull request",
  supportsPR: false,
  resolveItemNumber: resolvePRNumber,
  buildUpdateData: buildPRUpdateData,
  executeUpdate: executePRUpdate,
  formatSuccessResult: formatPRSuccessResult,
  additionalConfig: {
    allow_title: true,
    allow_body: true,
  },
});

module.exports = { main };
