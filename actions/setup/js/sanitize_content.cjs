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
  applyURLSanitizationPolicy,
  createRenderSafeCodeSpanWrapper,
  neutralizeCommands,
  neutralizeGitHubReferences,
  removeXmlComments,
  neutralizeMarkdownLinkTitles,
  convertXmlTags,
  applyToNonCodeRegions,
  neutralizeBotTriggers,
  neutralizeTemplateDelimiters,
  applyTruncation,
  hardenUnicodeText,
} = require("./sanitize_content_core.cjs");

const { balanceCodeRegions } = require("./markdown_code_region_balancer.cjs");

/**
 * User-facing mention aliases that should be accepted when runtime bot logins are allowlisted.
 * @type {Record<string, string[]>}
 */
const RUNTIME_TO_MENTION_ALIAS_MAP = {
  "copilot-swe-agent": ["copilot"],
};

/**
 * @typedef {Object} SanitizeOptions
 * @property {number} [maxLength] - Maximum length of content (default: 524288)
 * @property {string[]} [allowedAliases] - List of aliases (@mentions) that should not be neutralized
 * @property {number} [maxMentions] - Maximum number of unique allowed aliases to preserve
 * @property {Set<string>} [allowedAliasesSeen] - Allowed aliases already preserved in this output item
 * @property {number} [maxBotMentions] - Maximum bot trigger references before filtering (default: 10)
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
  /** @type {number | undefined} */
  let maxMentions;
  /** @type {number | undefined} */
  let maxBotMentions;
  /** @type {Set<string> | undefined} */
  let allowedAliasesSeen;

  if (typeof maxLengthOrOptions === "number") {
    maxLength = maxLengthOrOptions;
  } else if (maxLengthOrOptions && typeof maxLengthOrOptions === "object") {
    maxLength = maxLengthOrOptions.maxLength;
    // Pre-process allowed aliases to lowercase for efficient comparison
    const normalizedAllowedAliases = normalizeAllowedAliases(maxLengthOrOptions.allowedAliases);
    allowedAliasesLowercase = expandAllowedAliases(normalizedAllowedAliases);
    maxMentions = maxLengthOrOptions.maxMentions;
    maxBotMentions = maxLengthOrOptions.maxBotMentions;
    allowedAliasesSeen = maxLengthOrOptions.allowedAliasesSeen;
  }

  // If no allowed aliases specified, use core sanitization (which neutralizes all mentions)
  if (allowedAliasesLowercase.length === 0) {
    return sanitizeContentCore(content, maxLength, maxBotMentions);
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

  // Remove XML comments before mention neutralization to prevent bypass: if removeXmlComments
  // ran after neutralizeMentions, a comment like <!-- @user payload --> would first become
  // <!-- `@user` payload --> and applyFnOutsideInlineCode would split at the backtick boundary,
  // preventing the full <!--...--> pattern from being matched.
  sanitized = applyToNonCodeRegions(sanitized, removeXmlComments);

  // Neutralize markdown link titles as a hidden/steganographic injection channel analogous to
  // HTML comments: inline-link titles are made visible in link text, while reference-style
  // titles are stripped. Quoted title text ([text](url "TITLE") and [ref]: url "TITLE") is
  // invisible in GitHub's rendered markdown (shown only as hover-tooltips) but reaches the AI
  // model verbatim. Must run before mention neutralization for the same ordering reason as
  // removeXmlComments.
  sanitized = applyToNonCodeRegions(sanitized, neutralizeMarkdownLinkTitles);

  // Convert XML tags – skip code blocks and inline code
  sanitized = applyToNonCodeRegions(sanitized, convertXmlTags);

  // URI filtering (shared with core)
  sanitized = applyURLSanitizationPolicy(sanitized, allowedDomains);

  // Apply truncation limits (shared with core)
  sanitized = applyTruncation(sanitized, maxLength);

  // Neutralize mentions after truncation so the length boundary cannot split an
  // inserted code-span delimiter and reactivate a mention.
  sanitized = neutralizeMentions(sanitized, allowedAliasesLowercase, maxMentions, allowedAliasesSeen);

  // Neutralize GitHub references if restrictions are configured
  sanitized = neutralizeGitHubReferences(sanitized, allowedGitHubRefs);

  // Neutralize bot triggers
  sanitized = neutralizeBotTriggers(sanitized, maxBotMentions);

  // Neutralize template syntax delimiters (defense-in-depth)
  // This prevents potential issues if content is processed by downstream template engines
  sanitized = neutralizeTemplateDelimiters(sanitized);

  // Balance markdown code regions to fix improperly nested fences
  // This repairs markdown where AI models generate nested code blocks at the same indentation
  sanitized = balanceCodeRegions(sanitized);

  return sanitized.trim();

  /**
   * Normalize configured allowed aliases into an array so string inputs are
   * treated as one alias instead of being iterated character-by-character.
   * @param {string | string[] | undefined} aliases
   * @returns {string[]}
   */
  function normalizeAllowedAliases(aliases) {
    if (Array.isArray(aliases)) {
      return aliases;
    }
    if (typeof aliases === "string") {
      return [aliases];
    }
    return [];
  }

  /**
   * Expand allowlisted runtime aliases into accepted mention aliases.
   * @param {string[]} aliases
   * @returns {string[]}
   */
  function expandAllowedAliases(aliases) {
    const expanded = new Set();
    for (const alias of aliases) {
      if (typeof alias !== "string" || alias.length === 0) {
        continue;
      }
      const normalized = alias.toLowerCase();
      expanded.add(normalized);
      const mentionAliases = RUNTIME_TO_MENTION_ALIAS_MAP[normalized];
      if (Array.isArray(mentionAliases)) {
        for (const mentionAlias of mentionAliases) {
          expanded.add(mentionAlias.toLowerCase());
        }
      }
    }
    return [...expanded];
  }

  /**
   * Neutralize @mentions with selective filtering
   * @param {string} s - The string to process
   * @param {string[]} allowedLowercase - List of allowed aliases (lowercase)
   * @param {number | undefined} maxAllowed - Maximum number of unique allowed aliases to preserve
   * @param {Set<string> | undefined} seenAllowedAliases - Allowed aliases already preserved in this output item
   * @returns {string} Processed string
   */
  function neutralizeMentions(s, allowedLowercase, maxAllowed, seenAllowedAliases) {
    const wrapInCodeSpan = createRenderSafeCodeSpanWrapper(s);
    const allowedAliasesSeen = seenAllowedAliases || new Set();
    return applyToNonCodeRegions(s, (segment, regionBefore = "", regionAfter = "") => {
      return segment.replace(/(^|[^A-Za-z0-9])@([A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)/g, (match, prefix, alias, offset) => {
        const normalizedAlias = alias.toLowerCase();
        const isAllowed = allowedLowercase.includes(normalizedAlias);
        const isWithinLimit = maxAllowed === undefined || allowedAliasesSeen.has(normalizedAlias) || allowedAliasesSeen.size < maxAllowed;
        if (isAllowed && isWithinLimit) {
          allowedAliasesSeen.add(normalizedAlias);
          return `${prefix}@${alias}`;
        }
        if (typeof core !== "undefined" && core.info) {
          core.info(isAllowed ? `Escaped mention: @${alias} (mention limit exceeded)` : `Escaped mention: @${alias} (not in allowed list)`);
        }
        const before = prefix || (offset === 0 ? regionBefore : "");
        const after = segment[offset + match.length] || (offset + match.length === segment.length ? regionAfter : "");
        return `${prefix}${wrapInCodeSpan(`@${alias}`, before, after)}`;
      });
    });
  }
}

module.exports = { sanitizeContent, getRedactedDomains, clearRedactedDomains, writeRedactedDomainsLog };
