// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Close Discussion Message Module
 *
 * This module provides the message for closing older discussions
 * when a newer one is created.
 */

const { getMessages, renderTemplate, toSnakeCase } = require("./messages_core.cjs");

/**
 * @typedef {Object} CloseOlderDiscussionContext
 * @property {string} newDiscussionUrl - URL of the new discussion that replaced this one
 * @property {number} newDiscussionNumber - Number of the new discussion
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 */

/**
 * Get the close-older-discussion message, using custom template if configured.
 * @param {CloseOlderDiscussionContext} ctx - Context for message generation
 * @returns {string} Close older discussion message
 */
function getCloseOlderDiscussionMessage(ctx) {
  const messages = getMessages();

  // Create context with both camelCase and snake_case keys
  const templateContext = toSnakeCase(ctx);

  // Default close-older-discussion template
  const defaultMessage = `This discussion has been marked as **outdated** by [{workflow_name}]({run_url}).

A newer discussion is available at **[Discussion #{new_discussion_number}]({new_discussion_url})**.`;

  // Use custom message if configured
  return messages?.closeOlderDiscussion ? renderTemplate(messages.closeOlderDiscussion, templateContext) : renderTemplate(defaultMessage, templateContext);
}

module.exports = {
  getCloseOlderDiscussionMessage,
};
