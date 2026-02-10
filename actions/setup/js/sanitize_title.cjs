// @ts-check
/**
 * Title sanitization utilities for issues, discussions, and pull requests
 * @module sanitize_title
 */

const { hardenUnicodeText } = require("./sanitize_content_core.cjs");

/**
 * Sanitizes a title by applying Unicode security hardening and preventing duplicate prefixes
 * @param {string} title - The title to sanitize
 * @param {string} [titlePrefix] - Optional prefix that may need to be added
 * @returns {string} The sanitized title
 */
function sanitizeTitle(title, titlePrefix = "") {
  if (!title || typeof title !== "string") {
    return "";
  }

  let sanitized = title.trim();

  // Apply Unicode security hardening (NFC normalization, zero-width removal, etc.)
  sanitized = hardenUnicodeText(sanitized);

  // If a prefix is provided, remove any existing occurrences to avoid duplication
  if (titlePrefix && titlePrefix.trim()) {
    const cleanPrefix = titlePrefix.trim();

    // First, check for and remove variations with common separators that agents might add
    // This prevents issues like "[Agent]: Title" being treated as duplicate
    // Get the prefix without any trailing space for separator checking
    const prefixWithoutSpace = cleanPrefix.replace(/\s+$/, "");
    const separatorPatterns = [":", " -", " |"];

    let foundSeparator = false;
    for (const separator of separatorPatterns) {
      const variation = prefixWithoutSpace + separator;
      if (sanitized.startsWith(variation)) {
        sanitized = sanitized.substring(variation.length).trim();
        foundSeparator = true;
        break; // Only remove one separator variation
      }
    }

    // If no separator was found, remove the exact prefix (case-sensitive match)
    // Keep removing until we no longer find the prefix at the start
    if (!foundSeparator) {
      while (sanitized.startsWith(cleanPrefix)) {
        sanitized = sanitized.substring(cleanPrefix.length).trim();
      }
    }
  }

  return sanitized;
}

/**
 * Applies a title prefix to a sanitized title
 * This should be called after sanitizeTitle() to ensure the title is clean before prefixing
 * @param {string} sanitizedTitle - The already-sanitized title
 * @param {string} titlePrefix - The prefix to add
 * @returns {string} The title with prefix applied
 */
function applyTitlePrefix(sanitizedTitle, titlePrefix) {
  if (!titlePrefix || !titlePrefix.trim()) {
    return sanitizedTitle;
  }

  const cleanTitle = sanitizedTitle.trim();

  // Only add prefix if title doesn't already start with it
  // The titlePrefix parameter is used as-is (not trimmed) to preserve any trailing space
  if (cleanTitle && !cleanTitle.startsWith(titlePrefix)) {
    return titlePrefix + cleanTitle;
  }

  return cleanTitle;
}

module.exports = {
  sanitizeTitle,
  applyTitlePrefix,
};
