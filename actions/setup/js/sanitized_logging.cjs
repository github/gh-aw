// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Sanitized Logging Helpers
 *
 * This module provides safe logging functions that neutralize GitHub Actions
 * workflow commands (::command::) at the start of lines to prevent workflow
 * command injection when logging user-generated content.
 *
 * GitHub Actions interprets lines starting with "::" as workflow commands.
 * For example: "::set-output name=x::value" or "::error::message"
 *
 * When logging user-controlled strings, these must be sanitized to prevent
 * injection attacks where malicious input could trigger unintended workflow commands.
 */

/**
 * Neutralizes GitHub Actions workflow commands by replacing line-start "::"
 * @param {string} message - The message to neutralize
 * @returns {string} The neutralized message
 */
function neutralizeWorkflowCommands(message) {
  if (typeof message !== "string") {
    return message;
  }

  // Replace "::" at the start of any line with ": :" (space inserted)
  // The 'm' flag makes ^ match at the start of each line
  return message.replace(/^::/gm, ": :");
}

/**
 * Safe wrapper for core.info that neutralizes workflow commands
 * @param {string} message - The message to log
 */
function safeInfo(message) {
  core.info(neutralizeWorkflowCommands(message));
}

/**
 * Safe wrapper for core.debug that neutralizes workflow commands
 * @param {string} message - The message to log
 */
function safeDebug(message) {
  core.debug(neutralizeWorkflowCommands(message));
}

/**
 * Safe wrapper for core.warning that neutralizes workflow commands
 * @param {string} message - The message to log
 */
function safeWarning(message) {
  core.warning(neutralizeWorkflowCommands(message));
}

/**
 * Safe wrapper for core.error that neutralizes workflow commands
 * @param {string} message - The message to log
 */
function safeError(message) {
  core.error(neutralizeWorkflowCommands(message));
}

module.exports = {
  neutralizeWorkflowCommands,
  safeInfo,
  safeDebug,
  safeWarning,
  safeError,
};
