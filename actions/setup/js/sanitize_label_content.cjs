// @ts-check
/**
 * Sanitize label content for GitHub API
 * Removes control characters, ANSI codes, and neutralizes @mentions
 * @module sanitize_label_content
 */

const { hardenUnicodeText } = require("./sanitize_content_core.cjs");

/**
 * Sanitizes label content by removing control characters, ANSI escape codes,
 * and neutralizing @mentions to prevent unintended notifications.
 *
 * @param {string} content - The label content to sanitize
 * @returns {string} The sanitized label content
 */
function sanitizeLabelContent(content) {
  if (!content || typeof content !== "string") {
    return "";
  }
  let sanitized = content.trim();

  // Apply Unicode hardening first
  sanitized = hardenUnicodeText(sanitized);

  // Remove ANSI escape sequences FIRST (before removing control chars)
  sanitized = sanitized.replace(/\x1b\[[0-9;]*[mGKH]/g, "");
  // Then remove control characters (except newlines and tabs)
  sanitized = sanitized.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, "");
  sanitized = sanitized.replace(/(^|[^\w`])@([A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)/g, (_m, p1, p2) => `${p1}\`@${p2}\``);
  sanitized = sanitized.replace(/[<>&'"]/g, "");
  return sanitized.trim();
}

module.exports = { sanitizeLabelContent };
