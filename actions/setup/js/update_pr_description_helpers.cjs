// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Helper functions for updating pull request descriptions
 * Handles append, prepend, replace, and replace-island operations
 * @module update_pr_description_helpers
 */

const { getFooterMessage } = require("./messages_footer.cjs");

/**
 * Build the AI footer with workflow attribution
 * Uses the messages system to support custom templates from frontmatter
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @returns {string} AI attribution footer
 */
function buildAIFooter(workflowName, runUrl) {
  return "\n\n" + getFooterMessage({ workflowName, runUrl });
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
 * @returns {string} Updated body content
 */
function updateBody(params) {
  const { currentBody, newContent, operation, workflowName, runUrl, workflowId, includeFooter = true } = params;
  const aiFooter = includeFooter ? buildAIFooter(workflowName, runUrl) : "";

  if (operation === "replace") {
    // Replace: use new content with optional AI footer
    core.info("Operation: replace (full body replacement)");
    return newContent + aiFooter;
  }

  if (operation === "replace-island") {
    // Try to find existing island for this workflow ID
    const island = findIsland(currentBody, workflowId);

    if (island.found) {
      // Replace the island content
      core.info(`Operation: replace-island (updating existing island for workflow ${workflowId})`);
      const startMarker = buildIslandStartMarker(workflowId);
      const endMarker = buildIslandEndMarker(workflowId);
      const islandContent = `${startMarker}\n${newContent}${aiFooter}\n${endMarker}`;

      const before = currentBody.substring(0, island.startIndex);
      const after = currentBody.substring(island.endIndex);
      return before + islandContent + after;
    } else {
      // Island not found, fall back to append mode
      core.info(`Operation: replace-island (island not found for workflow ${workflowId}, falling back to append)`);
      const startMarker = buildIslandStartMarker(workflowId);
      const endMarker = buildIslandEndMarker(workflowId);
      const islandContent = `${startMarker}\n${newContent}${aiFooter}\n${endMarker}`;
      const appendSection = `\n\n---\n\n${islandContent}`;
      return currentBody + appendSection;
    }
  }

  if (operation === "prepend") {
    // Prepend: add content, AI footer (if enabled), and horizontal line at the start
    core.info("Operation: prepend (add to start with separator)");
    const prependSection = `${newContent}${aiFooter}\n\n---\n\n`;
    return prependSection + currentBody;
  }

  // Default to append
  core.info("Operation: append (add to end with separator)");
  const appendSection = `\n\n---\n\n${newContent}${aiFooter}`;
  return currentBody + appendSection;
}

module.exports = {
  buildAIFooter,
  buildIslandStartMarker,
  buildIslandEndMarker,
  findIsland,
  updateBody,
};
