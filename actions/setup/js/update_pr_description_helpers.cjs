// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Helper functions for updating pull request descriptions
 * Handles append, prepend, replace, and replace-island operations
 * @module update_pr_description_helpers
 */

const { assembleMarkdownBodyParts, buildGeneratedFooter } = require("./markdown_body_helpers.cjs");
const { getBodyFooterMessage } = require("./messages_footer.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");

/**
 * Build the AI footer with workflow attribution
 * Uses the common generateFooterWithMessages helper (includes install instructions,
 * missing info sections, blocked domains, and XML metadata marker).
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {string} [historyUrl] - GitHub search URL for items created by this workflow
 * @returns {string} AI attribution footer
 */
function buildAIFooter(workflowName, runUrl, historyUrl) {
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
  // Use typeof guard since context is a global injected by the Actions Script runtime
  const ctx = typeof context !== "undefined" ? context : null;
  return buildGeneratedFooter({
    workflowName,
    runUrl,
    workflowSource,
    workflowSourceURL,
    triggeringIssueNumber: ctx?.payload?.issue?.number,
    triggeringPRNumber: ctx?.payload?.pull_request?.number,
    triggeringDiscussionNumber: ctx?.payload?.discussion?.number,
    historyUrl,
  });
}

/**
 * Build the island start marker for replace-island mode
 * @param {string} workflowId - Workflow ID (stable identifier across runs)
 * @returns {string} Island start marker
 */
function buildIslandStartMarker(workflowId) {
  return `<!-- gh-aw-island-start:${workflowId} -->`;
}

/**
 * Build the island end marker for replace-island mode
 * @param {string} workflowId - Workflow ID (stable identifier across runs)
 * @returns {string} Island end marker
 */
function buildIslandEndMarker(workflowId) {
  return `<!-- gh-aw-island-end:${workflowId} -->`;
}

/**
 * Find and extract island content from body
 * @param {string} body - The body content to search
 * @param {string} workflowId - Workflow ID (stable identifier across runs)
 * @returns {{found: boolean, startIndex: number, endIndex: number}} Island location info
 */
function findIsland(body, workflowId) {
  const startMarker = buildIslandStartMarker(workflowId);
  const endMarker = buildIslandEndMarker(workflowId);

  const startIndex = body.indexOf(startMarker);
  if (startIndex === -1) {
    return { found: false, startIndex: -1, endIndex: -1 };
  }

  const endIndex = body.indexOf(endMarker, startIndex);
  if (endIndex === -1) {
    return { found: false, startIndex: -1, endIndex: -1 };
  }

  return { found: true, startIndex, endIndex: endIndex + endMarker.length };
}

/**
 * Update body content with the specified operation
 * Generic helper for updating markdown bodies (PRs, releases, discussions, etc.)
 * @param {Object} params - Update parameters
 * @param {string} params.currentBody - Current body content
 * @param {string} params.newContent - New content to add/replace
 * @param {string} params.operation - Operation type: "append", "prepend", "replace", or "replace-island"
 * @param {string} params.workflowName - Name of the workflow
 * @param {string} params.runUrl - URL of the workflow run
 * @param {string} params.workflowId - Workflow ID (stable identifier across runs)
 * @param {boolean} [params.includeFooter=true] - Whether to include AI-generated footer (default: true)
 * @param {string} [params.bodyFooter] - Deterministic body footer template
 * @param {string} [params.historyUrl] - GitHub search URL for items created by this workflow
 * @returns {string} Updated body content
 */
function updateBody(params) {
  const { currentBody, newContent, operation, workflowName, runUrl, workflowId, includeFooter = true, bodyFooter, historyUrl } = params;
  // When footer is enabled use the full footer (includes install instructions, XML marker, etc.)
  // When footer is disabled still add standalone workflow-id marker for searchability
  const aiFooter = includeFooter ? buildAIFooter(workflowName, runUrl, historyUrl) : "";
  const footerSection = aiFooter ? `\n\n${aiFooter}` : "";
  const renderedBodyFooter = getBodyFooterMessage(bodyFooter, { workflowName, runUrl });
  const bodyFooterSection = renderedBodyFooter ? `\n\n${renderedBodyFooter.trimEnd()}` : "";
  const workflowIdMarker = !includeFooter && workflowId ? `\n\n${generateWorkflowIdMarker(workflowId)}` : "";

  // Sanitize new content to prevent injection attacks
  const sanitizedNewContent = sanitizeContent(newContent);

  // Inject CAUTION at top of new content if threat detection warning was raised
  const { detectionCaution } = assembleMarkdownBodyParts({
    includeFooter: false,
    workflowName,
    runUrl,
  });
  const contentWithCaution = detectionCaution ? `${detectionCaution}\n\n${sanitizedNewContent}` : sanitizedNewContent;

  if (operation === "replace") {
    // Replace: use new content with optional AI footer
    core.info("Operation: replace (full body replacement)");
    return contentWithCaution + footerSection + bodyFooterSection + workflowIdMarker;
  }

  if (operation === "replace-island") {
    // Try to find existing island for this workflow ID
    const island = findIsland(currentBody, workflowId);
    const startMarker = buildIslandStartMarker(workflowId);
    const endMarker = buildIslandEndMarker(workflowId);
    const islandContent = `${startMarker}\n${contentWithCaution}${footerSection}${bodyFooterSection}${workflowIdMarker}\n${endMarker}`;

    if (island.found) {
      // Replace the island content
      core.info(`Operation: replace-island (updating existing island for workflow ${workflowId})`);
      const before = currentBody.substring(0, island.startIndex);
      const after = currentBody.substring(island.endIndex);
      return before + islandContent + after;
    } else {
      // Island not found, fall back to append mode
      core.info(`Operation: replace-island (island not found for workflow ${workflowId}, falling back to append)`);
      return currentBody + `\n\n---\n\n${islandContent}`;
    }
  }

  if (operation === "prepend") {
    // Prepend: add content, AI footer (if enabled), and horizontal line at the start
    core.info("Operation: prepend (add to start with separator)");
    const prependSection = `${contentWithCaution}${footerSection}${bodyFooterSection}${workflowIdMarker}\n\n---\n\n`;
    return prependSection + currentBody;
  }

  // Default to append
  core.info("Operation: append (add to end with separator)");
  const appendSection = `\n\n---\n\n${contentWithCaution}${footerSection}${bodyFooterSection}${workflowIdMarker}`;
  return currentBody + appendSection;
}

/**
 * Build an updated entity body with workflow attribution.
 * @param {Object} params - Body update parameters
 * @param {any} params.context - GitHub Actions context for the target repository
 * @param {string} params.currentBody - Current body content
 * @param {string} params.newContent - New content to add or replace
 * @param {string} params.operation - Body update operation
 * @param {boolean} params.includeFooter - Whether to include the generated footer
 * @param {any} [params.workflowRepo] - Original workflow repository for run attribution
 * @param {"issue" | "pull_request"} params.itemType - Updated entity type
 * @param {string} [params.bodyFooter] - Deterministic body footer template
 * @returns {string} Updated body content
 */
function buildUpdatedBody({ context, currentBody, newContent, operation, includeFooter, workflowRepo, itemType, bodyFooter }) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "GitHub Agentic Workflow";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
  const workflowCallId = process.env.GH_AW_CALLER_WORKFLOW_ID || "";
  const runUrl = buildWorkflowRunUrl(context, workflowRepo || context.repo);
  const historyUrl =
    generateHistoryUrl({
      owner: context.repo.owner,
      repo: context.repo.repo,
      itemType,
      workflowCallId,
      workflowId,
      serverUrl: context.serverUrl,
    }) || undefined;

  return updateBody({
    currentBody,
    newContent,
    operation,
    workflowName,
    runUrl,
    workflowId,
    includeFooter,
    bodyFooter,
    historyUrl,
  });
}

module.exports = {
  buildAIFooter,
  buildUpdatedBody,
  buildIslandStartMarker,
  buildIslandEndMarker,
  findIsland,
  updateBody,
};
