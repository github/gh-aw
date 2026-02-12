// @ts-check

/**
 * Neutralize GitHub Actions workflow commands in text by escaping double colons.
 * This prevents injection of commands like ::set-output::, ::warning::, ::error::, etc.
 *
 * GitHub Actions workflow commands have the format:
 * ::command parameter1={data},parameter2={data}::{command value}
 *
 * By replacing :: with :\u200B: (zero-width space), we prevent the command from being parsed
 * while maintaining readability.
 *
 * @param {string} text - The text to neutralize
 * @returns {string} The neutralized text
 */
function neutralizeWorkflowCommands(text) {
  if (typeof text !== "string") {
    return String(text);
  }
  // Replace :: with : followed by zero-width space followed by :
  // This breaks the workflow command syntax while keeping text readable
  return text.replace(/::/g, ":\u200B:");
}

/**
 * Sanitized wrapper for core.info that neutralizes workflow commands in user-generated content.
 * Use this instead of core.info() when logging user-generated text that might contain
 * malicious workflow commands.
 *
 * @param {string} message - The message to log (will be sanitized)
 */
function safeInfo(message) {
  core.info(neutralizeWorkflowCommands(message));
}

/**
 * Sanitized wrapper for core.debug that neutralizes workflow commands in user-generated content.
 * Use this instead of core.debug() when logging user-generated text that might contain
 * malicious workflow commands.
 *
 * @param {string} message - The message to log (will be sanitized)
 */
function safeDebug(message) {
  core.debug(neutralizeWorkflowCommands(message));
}

/**
 * Sanitized wrapper for core.warning that neutralizes workflow commands in user-generated content.
 * Use this instead of core.warning() when logging user-generated text that might contain
 * malicious workflow commands.
 *
 * @param {string} message - The message to log (will be sanitized)
 */
function safeWarning(message) {
  core.warning(neutralizeWorkflowCommands(message));
}

/**
 * Sanitized wrapper for core.error that neutralizes workflow commands in user-generated content.
 * Use this instead of core.error() when logging user-generated text that might contain
 * malicious workflow commands.
 *
 * @param {string} message - The message to log (will be sanitized)
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
