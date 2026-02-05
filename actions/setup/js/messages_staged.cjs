// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Staged Mode Message Module
 *
 * This module provides staged mode title and description generation
 * for safe-output preview functionality.
 */

const { getMessages, renderTemplate, toSnakeCase } = require("./messages_core.cjs");

/**
 * @typedef {Object} StagedContext
 * @property {string} operation - The operation name (e.g., "Create Issues", "Add Comments")
 */

/**
 * Get the staged mode title, using custom template if configured.
 * @param {StagedContext} ctx - Context for staged title generation
 * @returns {string} Staged mode title
 */
function getStagedTitle(ctx) {
  const messages = getMessages();

  // Create context with both camelCase and snake_case keys
  const templateContext = toSnakeCase(ctx);

  // Default staged title template
  const defaultTitle = "## 🔍 Preview: {operation}";

  // Use custom title if configured
  return messages?.stagedTitle ? renderTemplate(messages.stagedTitle, templateContext) : renderTemplate(defaultTitle, templateContext);
}

/**
 * Get the staged mode description, using custom template if configured.
 * @param {StagedContext} ctx - Context for staged description generation
 * @returns {string} Staged mode description
 */
function getStagedDescription(ctx) {
  const messages = getMessages();

  // Create context with both camelCase and snake_case keys
  const templateContext = toSnakeCase(ctx);

  // Default staged description template
  const defaultDescription = "📋 The following operations would be performed if staged mode was disabled:";

  // Use custom description if configured
  return messages?.stagedDescription ? renderTemplate(messages.stagedDescription, templateContext) : renderTemplate(defaultDescription, templateContext);
}

module.exports = {
  getStagedTitle,
  getStagedDescription,
};
