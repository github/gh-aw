// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Safe Output Messages Module (Barrel File)
 *
 * This module re-exports all message functions from the modular message files.
 * It provides backward compatibility for existing code that imports from messages.cjs.
 *
 * For new code, prefer importing directly from the specific modules:
 * - ./messages_core.cjs - Core utilities (getMessages, renderTemplate, toSnakeCase)
 * - ./messages_footer.cjs - Footer messages (getFooterMessage, getFooterInstallMessage, generateFooterWithMessages)
 * - ./messages_staged.cjs - Staged mode messages (getStagedTitle, getStagedDescription)
 * - ./messages_run_status.cjs - Run status messages (getRunStartedMessage, getRunSuccessMessage, getRunFailureMessage)
 * - ./messages_close_discussion.cjs - Close discussion messages (getCloseOlderDiscussionMessage)
 *
 * This module supports placeholder-based templates for messages.
 * Both camelCase and snake_case placeholder formats are supported.
 * For the authoritative and up-to-date list of supported placeholders,
 * see the documentation in ./messages_core.cjs.
 */

// Re-export core utilities
const { getMessages, renderTemplate, renderTemplateFromFile } = require("./messages_core.cjs");

// Re-export footer messages
const {
  getDetectionCautionAlert,
  getBodyFooterMessage,
  getFooterMessage,
  getFooterInstallMessage,
  getFooterAgentFailureIssueMessage,
  getFooterAgentFailureCommentMessage,
  generateFooterWithMessages,
  generateXMLMarker,
} = require("./messages_footer.cjs");

// Re-export staged mode messages
const { getStagedTitle, getStagedDescription } = require("./messages_staged.cjs");

// Re-export run status messages
const { getRunStartedMessage, getRunSuccessMessage, getRunFailureMessage } = require("./messages_run_status.cjs");

// Re-export close discussion messages
const { getCloseOlderDiscussionMessage } = require("./messages_close_discussion.cjs");

// Re-export body header messages
const { getBodyHeader, getDisclosureHeader } = require("./messages_header.cjs");

module.exports = {
  getMessages,
  renderTemplate,
  renderTemplateFromFile,
  getDetectionCautionAlert,
  getBodyFooterMessage,
  getFooterMessage,
  getFooterInstallMessage,
  getFooterAgentFailureIssueMessage,
  getFooterAgentFailureCommentMessage,
  generateFooterWithMessages,
  generateXMLMarker,
  getStagedTitle,
  getStagedDescription,
  getRunStartedMessage,
  getRunSuccessMessage,
  getRunFailureMessage,
  getCloseOlderDiscussionMessage,
  getBodyHeader,
  getDisclosureHeader,
};
