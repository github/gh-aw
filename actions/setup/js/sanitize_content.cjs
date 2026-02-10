// @ts-check
/**
 * Full sanitization utilities with mention filtering support
 * This module provides the complete sanitization with selective mention filtering.
 * For incoming text that doesn't need mention filtering, use sanitize_incoming_text.cjs instead.
 */

const {
  sanitizeContentCore,
  getRedactedDomains,
  clearRedactedDomains,
  writeRedactedDomainsLog,
  buildAllowedDomains,
  buildAllowedGitHubReferences,
  getCurrentRepoSlug,
  sanitizeUrlProtocols,
  sanitizeUrlDomains,
  neutralizeCommands,
  neutralizeGitHubReferences,
  removeXmlComments,
  convertXmlTags,
  neutralizeBotTriggers,
  applyTruncation,
  hardenUnicodeText,
} = require("./sanitize_content_core.cjs");

const { balanceCodeRegions } = require("./markdown_code_region_balancer.cjs");

/**
 * @typedef {Object} SanitizeOptions
 * @property {number} [maxLength] - Maximum length of content (default: 524288)
 * @property {string[]} [allowedAliases] - List of aliases (@mentions) that should not be neutralized
 */

/**
 * Sanitizes content for safe output in GitHub Actions with optional mention filtering
 * @param {string} content - The content to sanitize
 * @param {number | SanitizeOptions} [maxLengthOrOptions] - Maximum length of content (default: 524288) or options object
 * @returns {string} The sanitized content
 */
function sanitizeContent(content, maxLengthOrOptions) {
  // Handle both old signature (maxLength) and new signature (options object)
  /** @type {number | undefined} */
  let maxLength;
  /** @type {string[]} */
  let allowedAliasesLowercase = [];

  if (typeof maxLengthOrOptions === "number") {
    maxLength = maxLengthOrOptions;
  } else if (maxLengthOrOptions && typeof maxLengthOrOptions === "object") {
    maxLength = maxLengthOrOptions.maxLength;
    // Pre-process allowed aliases to lowercase for efficient comparison
    allowedAliasesLowercase = (maxLengthOrOptions.allowedAliases || []).map(alias => alias.toLowerCase());
  }

  // If no allowed aliases specified, use core sanitization (which neutralizes all mentions)
  if (allowedAliasesLowercase.length === 0) {
    return sanitizeContentCore(content, maxLength);
  }

  // If allowed aliases are specified, we need custom mention filtering
  // We'll apply the same sanitization pipeline but with selective mention filtering

  if (!content || typeof content !== "string") {
    return "";
  }

  // Build list of allowed domains (shared with core)
  const allowedDomains = buildAllowedDomains();

  // Build list of allowed GitHub references from environment
  const allowedGitHubRefs = buildAllowedGitHubReferences();

  let sanitized = content;

  // Apply Unicode hardening first to normalize text representation
  sanitized = hardenUnicodeText(sanitized);

  // Remove ANSI escape sequences and control characters early
  sanitized = sanitized.replace(/\x1b\[[0-9;]*[mGKH]/g, "");
  sanitized = sanitized.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, "");

  // Neutralize commands at the start of text
  sanitized = neutralizeCommands(sanitized);

  // Neutralize @mentions with selective filtering (custom logic for allowed aliases)
  sanitized = neutralizeMentions(sanitized, allowedAliasesLowercase);

  // Remove XML comments
  sanitized = removeXmlComments(sanitized);

  // Convert XML tags
  sanitized = convertXmlTags(sanitized);

  // URI filtering (shared with core)
  sanitized = sanitizeUrlProtocols(sanitized);
  sanitized = sanitizeUrlDomains(sanitized, allowedDomains);

  // Apply truncation limits (shared with core)
  sanitized = applyTruncation(sanitized, maxLength);

  // Neutralize GitHub references if restrictions are configured
  sanitized = neutralizeGitHubReferences(sanitized, allowedGitHubRefs);

  // Neutralize bot triggers
  sanitized = neutralizeBotTriggers(sanitized);

  // Balance markdown code regions to fix improperly nested fences
  // This repairs markdown where AI models generate nested code blocks at the same indentation
  sanitized = balanceCodeRegions(sanitized);

  return sanitized.trim();

  /**
   * Neutralize @mentions with selective filtering
   * @param {string} s - The string to process
   * @param {string[]} allowedLowercase - List of allowed aliases (lowercase)
   * @returns {string} Processed string
   */
  function neutralizeMentions(s, allowedLowercase) {
    return s.replace(/(^|[^\w`])@([A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)/g, (_m, p1, p2) => {
      // Check if this mention is in the allowed aliases list (case-insensitive)
      const isAllowed = allowedLowercase.includes(p2.toLowerCase());
      if (isAllowed) {
        return `${p1}@${p2}`; // Keep the original mention
      }
      // Log when a mention is escaped
      if (typeof core !== "undefined" && core.info) {
        core.info(`Escaped mention: @${p2} (not in allowed list)`);
      }
      return `${p1}\`@${p2}\``; // Neutralize the mention
    });
  }
}

module.exports = { sanitizeContent, getRedactedDomains, clearRedactedDomains, writeRedactedDomainsLog };
