// @ts-check
/**
 * Core sanitization utilities without mention filtering
 * This module provides the base sanitization functions that don't require
 * mention resolution or filtering. It's designed to be imported by both
 * sanitize_content.cjs (full version) and sanitize_incoming_text.cjs (minimal version).
 */

/**
 * Module-level set to collect redacted URL domains across sanitization calls.
 * @type {string[]}
 */
const redactedDomains = [];

/**
 * Gets the list of redacted URL domains collected during sanitization.
 * @returns {string[]} Array of redacted domain strings
 */
function getRedactedDomains() {
  return [...redactedDomains];
}

/**
 * Adds a domain to the redacted domains list
 * @param {string} domain - Domain to add
 */
function addRedactedDomain(domain) {
  redactedDomains.push(domain);
}

/**
 * Clears the list of redacted URL domains.
 * Useful for testing or resetting state between operations.
 */
function clearRedactedDomains() {
  redactedDomains.length = 0;
}

/**
 * Writes the collected redacted URL domains to a log file.
 * Only creates the file if there are redacted domains.
 * @param {string} [filePath] - Path to write the log file. Defaults to /tmp/gh-aw/redacted-urls.log
 * @returns {string|null} The file path if written, null if no domains to write
 */
function writeRedactedDomainsLog(filePath) {
  if (redactedDomains.length === 0) {
    return null;
  }

  const fs = require("fs");
  const path = require("path");
  const targetPath = filePath || "/tmp/gh-aw/redacted-urls.log";

  // Ensure directory exists
  const dir = path.dirname(targetPath);
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }

  // Write domains to file, one per line
  fs.writeFileSync(targetPath, redactedDomains.join("\n") + "\n");

  return targetPath;
}

/**
 * Extract domains from a URL and return an array of domain variations
 * @param {string} url - The URL to extract domains from
 * @returns {string[]} Array of domain variations
 */
function extractDomainsFromUrl(url) {
  if (!url || typeof url !== "string") {
    return [];
  }

  try {
    // Parse the URL
    const urlObj = new URL(url);
    const hostname = urlObj.hostname.toLowerCase();

    // Return both the exact hostname and common variations
    const domains = [hostname];

    // For github.com, add api and raw content domain variations
    if (hostname === "github.com") {
      domains.push("api.github.com");
      domains.push("raw.githubusercontent.com");
      domains.push("*.githubusercontent.com");
    }
    // For custom GitHub Enterprise domains, add api. prefix and raw content variations
    else if (!hostname.startsWith("api.")) {
      domains.push("api." + hostname);
      // For GitHub Enterprise, raw content is typically served from raw.hostname
      domains.push("raw." + hostname);
    }

    return domains;
  } catch (e) {
    // Invalid URL, return empty array
    return [];
  }
}

/**
 * Build the list of allowed domains from environment variables and GitHub context
 * @returns {string[]} Array of allowed domains
 */
function buildAllowedDomains() {
  const allowedDomainsEnv = process.env.GH_AW_ALLOWED_DOMAINS;
  const defaultAllowedDomains = ["github.com", "github.io", "githubusercontent.com", "githubassets.com", "github.dev", "codespaces.new"];

  let allowedDomains = allowedDomainsEnv
    ? allowedDomainsEnv
        .split(",")
        .map(d => d.trim())
        .filter(d => d)
    : defaultAllowedDomains;

  // Extract and add GitHub domains from GitHub context URLs
  const githubServerUrl = process.env.GITHUB_SERVER_URL;
  const githubApiUrl = process.env.GITHUB_API_URL;

  if (githubServerUrl) {
    const serverDomains = extractDomainsFromUrl(githubServerUrl);
    allowedDomains = allowedDomains.concat(serverDomains);
  }

  if (githubApiUrl) {
    const apiDomains = extractDomainsFromUrl(githubApiUrl);
    allowedDomains = allowedDomains.concat(apiDomains);
  }

  // Remove duplicates
  return [...new Set(allowedDomains)];
}

/**
 * Sanitize a domain name to only include alphanumeric characters and dots,
 * keeping up to 3 domain parts (e.g., sub.example.com).
 * If more than 3 parts exist, truncates with "..."
 * @param {string} domain - The domain to sanitize
 * @returns {string} The sanitized domain
 */
function sanitizeDomainName(domain) {
  if (!domain || typeof domain !== "string") {
    return "";
  }

  // Split domain into parts
  const parts = domain.split(".");

  // Keep only alphanumeric characters in each part
  const sanitizedParts = parts.map(part => part.replace(/[^a-zA-Z0-9]/g, ""));

  // Filter out empty parts
  const nonEmptyParts = sanitizedParts.filter(part => part.length > 0);

  // Take up to 3 parts
  if (nonEmptyParts.length <= 3) {
    return nonEmptyParts.join(".");
  } else {
    // Take first 3 parts and add "..."
    return nonEmptyParts.slice(0, 3).join(".") + "...";
  }
}

/**
 * Sanitize URL protocols - replace non-https with <sanitized-domain>/redacted
 * @param {string} s - The string to process
 * @returns {string} The string with non-https protocols redacted
 */
function sanitizeUrlProtocols(s) {
  // Match common non-https protocols
  // This regex matches: protocol://domain or protocol:path or incomplete protocol://
  // Examples: http://, ftp://, file://, data:, javascript:, mailto:, tel:, ssh://, git://
  // The regex also matches incomplete protocols like "http://" or "ftp://" without a domain
  // Note: No word boundary check to catch protocols even when preceded by word characters
  return s.replace(/((?:http|ftp|file|ssh|git):\/\/([\w.-]*)(?:[^\s]*)|(?:data|javascript|vbscript|about|mailto|tel):[^\s]+)/gi, (match, _fullMatch, domain) => {
    // Extract domain for http/ftp/file/ssh/git protocols
    if (domain) {
      const domainLower = domain.toLowerCase();
      const sanitized = sanitizeDomainName(domainLower);
      const truncated = domainLower.length > 12 ? domainLower.substring(0, 12) + "..." : domainLower;
      if (typeof core !== "undefined" && core.info) {
        core.info(`Redacted URL: ${truncated}`);
      }
      if (typeof core !== "undefined" && core.debug) {
        core.debug(`Redacted URL (full): ${match}`);
      }
      addRedactedDomain(domainLower);
      // Return sanitized domain format
      return sanitized ? `(${sanitized}/redacted)` : "(redacted)";
    } else {
      // For other protocols (data:, javascript:, etc.), track the protocol itself
      const protocolMatch = match.match(/^([^:]+):/);
      if (protocolMatch) {
        const protocol = protocolMatch[1] + ":";
        // Truncate the matched URL for logging (keep first 12 chars + "...")
        const truncated = match.length > 12 ? match.substring(0, 12) + "..." : match;
        if (typeof core !== "undefined" && core.info) {
          core.info(`Redacted URL: ${truncated}`);
        }
        if (typeof core !== "undefined" && core.debug) {
          core.debug(`Redacted URL (full): ${match}`);
        }
        addRedactedDomain(protocol);
      }
      return "(redacted)";
    }
  });
}

/**
 * Remove unknown domains
 * @param {string} s - The string to process
 * @param {string[]} allowed - List of allowed domains
 * @returns {string} The string with unknown domains redacted
 */
function sanitizeUrlDomains(s, allowed) {
  // Match HTTPS URLs with optional port and path
  // This regex is designed to:
  // 1. Match https:// URIs with explicit protocol
  // 2. Capture the hostname/domain
  // 3. Allow optional port (:8080)
  // 4. Allow optional path and query string (but not trailing commas/periods)
  // 5. Stop before another https:// URL in query params (using negative lookahead)
  const httpsUrlRegex = /https:\/\/([\w.-]+(?::\d+)?)(\/(?:(?!https:\/\/)[^\s,])*)?/gi;

  return s.replace(httpsUrlRegex, (match, hostnameWithPort, pathPart) => {
    // Extract just the hostname (remove port if present)
    const hostname = hostnameWithPort.split(":")[0].toLowerCase();
    pathPart = pathPart || "";

    // Check if domain is in the allowed list or is a subdomain of an allowed domain
    const isAllowed = allowed.some(allowedDomain => {
      const normalizedAllowed = allowedDomain.toLowerCase();

      // Exact match
      if (hostname === normalizedAllowed) {
        return true;
      }

      // Wildcard match (*.example.com matches subdomain.example.com)
      if (normalizedAllowed.startsWith("*.")) {
        const baseDomain = normalizedAllowed.substring(2); // Remove *.
        return hostname.endsWith("." + baseDomain) || hostname === baseDomain;
      }

      // Subdomain match (example.com matches subdomain.example.com)
      return hostname.endsWith("." + normalizedAllowed);
    });

    if (isAllowed) {
      return match; // Keep the full URL as-is
    } else {
      // Redact the domain but preserve the protocol and structure for debugging
      const sanitized = sanitizeDomainName(hostname);
      const truncated = hostname.length > 12 ? hostname.substring(0, 12) + "..." : hostname;
      if (typeof core !== "undefined" && core.info) {
        core.info(`Redacted URL: ${truncated}`);
      }
      if (typeof core !== "undefined" && core.debug) {
        core.debug(`Redacted URL (full): ${match}`);
      }
      addRedactedDomain(hostname);
      // Return sanitized domain format
      return sanitized ? `(${sanitized}/redacted)` : "(redacted)";
    }
  });
}

/**
 * Neutralizes commands at the start of text by wrapping them in backticks
 * @param {string} s - The string to process
 * @returns {string} The string with neutralized commands
 */
function neutralizeCommands(s) {
  const commandName = process.env.GH_AW_COMMAND;
  if (!commandName) {
    return s;
  }

  // Escape special regex characters in command name
  const escapedCommand = commandName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  // Neutralize /command at the start of text (with optional leading whitespace)
  // Only match at the start of the string or after leading whitespace
  return s.replace(new RegExp(`^(\\s*)/(${escapedCommand})\\b`, "i"), "$1`/$2`");
}

/**
 * Neutralizes ALL @mentions by wrapping them in backticks
 * This is the core version without any filtering
 * @param {string} s - The string to process
 * @returns {string} The string with neutralized mentions
 */
function neutralizeAllMentions(s) {
  // Replace @name or @org/team outside code with `@name`
  // No filtering - all mentions are neutralized
  return s.replace(/(^|[^\w`])@([A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)/g, (m, p1, p2) => {
    // Log when a mention is escaped to help debug issues
    if (typeof core !== "undefined" && core.info) {
      core.info(`Escaped mention: @${p2} (not in allowed list)`);
    }
    return `${p1}\`@${p2}\``;
  });
}

/**
 * Removes XML comments from content
 * @param {string} s - The string to process
 * @returns {string} The string with XML comments removed
 */
function removeXmlComments(s) {
  // Remove <!-- comment --> and malformed <!--! comment --!>
  // Consolidated into single atomic regex to prevent intermediate state vulnerabilities
  // The pattern <!--[\s\S]*?--!?> matches both <!-- ... --> and <!-- ... --!>
  // Apply repeatedly to handle nested/overlapping patterns that could reintroduce comment markers
  let previous;
  do {
    previous = s;
    s = s.replace(/<!--[\s\S]*?--!?>/g, "");
  } while (s !== previous);
  return s;
}

/**
 * Converts XML/HTML tags to parentheses format to prevent injection
 * @param {string} s - The string to process
 * @returns {string} The string with XML tags converted to parentheses
 */
function convertXmlTags(s) {
  // Allow safe HTML tags: b, blockquote, br, code, details, em, h1–h6, hr, i, li, ol, p, pre, strong, sub, summary, sup, table, tbody, td, th, thead, tr, ul
  const allowedTags = ["b", "blockquote", "br", "code", "details", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "li", "ol", "p", "pre", "strong", "sub", "summary", "sup", "table", "tbody", "td", "th", "thead", "tr", "ul"];

  // First, process CDATA sections specially - convert tags inside them and the CDATA markers
  s = s.replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, (match, content) => {
    // Convert tags inside CDATA content
    const convertedContent = content.replace(/<(\/?[A-Za-z][A-Za-z0-9]*(?:[^>]*?))>/g, "($1)");
    // Return with CDATA markers also converted to parentheses
    return `(![CDATA[${convertedContent}]])`;
  });

  // Convert opening tags: <tag> or <tag attr="value"> to (tag) or (tag attr="value")
  // Convert closing tags: </tag> to (/tag)
  // Convert self-closing tags: <tag/> or <tag /> to (tag/) or (tag /)
  // But preserve allowed safe tags
  return s.replace(/<(\/?[A-Za-z!][^>]*?)>/g, (match, tagContent) => {
    // Extract tag name from the content (handle closing tags and attributes)
    const tagNameMatch = tagContent.match(/^\/?\s*([A-Za-z][A-Za-z0-9]*)/);
    if (tagNameMatch) {
      const tagName = tagNameMatch[1].toLowerCase();
      if (allowedTags.includes(tagName)) {
        return match; // Preserve allowed tags
      }
    }
    return `(${tagContent})`; // Convert other tags to parentheses
  });
}

/**
 * Neutralizes bot trigger phrases by wrapping them in backticks
 * @param {string} s - The string to process
 * @returns {string} The string with neutralized bot triggers
 */
function neutralizeBotTriggers(s) {
  // Neutralize common bot trigger phrases like "fixes #123", "closes #asdfs", etc.
  return s.replace(/\b(fixes?|closes?|resolves?|fix|close|resolve)\s+#(\w+)/gi, (match, action, ref) => `\`${action} #${ref}\``);
}

/**
 * Builds the list of allowed repositories for GitHub reference filtering
 * Returns null if all references should be allowed (default behavior)
 * Returns empty array if no references should be allowed (escape all)
 * @returns {string[]|null} Array of allowed repository slugs or null if all allowed
 */
function buildAllowedGitHubReferences() {
  const allowedRefsEnv = process.env.GH_AW_ALLOWED_GITHUB_REFS;
  if (allowedRefsEnv === undefined) {
    return null; // All references allowed by default (env var not set)
  }

  if (allowedRefsEnv === "") {
    return []; // Empty array means escape all references
  }

  return allowedRefsEnv
    .split(",")
    .map(ref => ref.trim().toLowerCase())
    .filter(ref => ref);
}

/**
 * Gets the current repository slug from GitHub context
 * @returns {string} Repository slug in "owner/repo" format, or empty string if not available
 */
function getCurrentRepoSlug() {
  // Try to get from GITHUB_REPOSITORY env var
  const repoSlug = process.env.GITHUB_REPOSITORY;
  if (repoSlug) {
    return repoSlug.toLowerCase();
  }
  return "";
}

/**
 * Neutralizes GitHub references (#123 or owner/repo#456) by wrapping them in backticks
 * if they reference repositories not in the allowed list
 * @param {string} s - The string to process
 * @param {string[]|null} allowedRepos - List of allowed repository slugs (lowercase), or null to allow all
 * @returns {string} The string with unauthorized references neutralized
 */
function neutralizeGitHubReferences(s, allowedRepos) {
  // If no restrictions configured (null), allow all references
  if (allowedRepos === null) {
    return s;
  }

  const currentRepo = getCurrentRepoSlug();

  // Match GitHub references:
  // - #123 (current repo reference)
  // - owner/repo#456 (cross-repo reference)
  // - GH-123 (GitHub shorthand)
  // Must not be inside backticks or code blocks
  return s.replace(/(^|[^\w`])(?:([a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?)\/([a-z0-9._-]+))?#(\w+)/gi, (match, prefix, owner, repo, issueNum) => {
    let targetRepo;

    if (owner && repo) {
      // Cross-repo reference: owner/repo#123
      targetRepo = `${owner}/${repo}`.toLowerCase();
    } else {
      // Current repo reference: #123
      targetRepo = currentRepo;
    }

    // Check if "repo" is in allowed list (means current repo)
    const allowCurrentRepo = allowedRepos.includes("repo");

    // Check if this specific repo is in the allowed list
    const isAllowed = allowedRepos.includes(targetRepo) || (allowCurrentRepo && targetRepo === currentRepo);

    if (isAllowed) {
      return match; // Keep the original reference
    } else {
      // Escape the reference
      const refText = owner && repo ? `${owner}/${repo}#${issueNum}` : `#${issueNum}`;

      // Log when a reference is escaped
      if (typeof core !== "undefined" && core.info) {
        core.info(`Escaped GitHub reference: ${refText} (not in allowed list)`);
      }

      return `${prefix}\`${refText}\``;
    }
  });
}

/**
 * Apply truncation limits to content
 * @param {string} content - The content to truncate
 * @param {number} [maxLength] - Maximum length of content (default: 524288)
 * @returns {string} The truncated content
 */
function applyTruncation(content, maxLength) {
  maxLength = maxLength || 524288;
  const lines = content.split("\n");
  const maxLines = 65000;

  // If content has too many lines, truncate by lines (primary limit)
  if (lines.length > maxLines) {
    const truncationMsg = "\n[Content truncated due to line count]";
    const truncatedLines = lines.slice(0, maxLines).join("\n") + truncationMsg;

    // If still too long after line truncation, shorten but keep the line count message
    if (truncatedLines.length > maxLength) {
      return truncatedLines.substring(0, maxLength - truncationMsg.length) + truncationMsg;
    } else {
      return truncatedLines;
    }
  } else if (content.length > maxLength) {
    return content.substring(0, maxLength) + "\n[Content truncated due to length]";
  }

  return content;
}

/**
 * Decodes HTML entities to prevent bypass of @mention detection.
 * Handles named entities (e.g., &commat;), decimal entities (e.g., &#64;),
 * and hex entities (e.g., &#x40;), including double-encoded variants (e.g., &amp;commat;).
 *
 * @param {string} text - Input text that may contain HTML entities
 * @returns {string} Text with HTML entities decoded
 */
function decodeHtmlEntities(text) {
  if (!text || typeof text !== "string") {
    return "";
  }

  let result = text;

  // Decode named entity for @ symbol (including double-encoded variants)
  // &commat; and &amp;commat; → @
  result = result.replace(/&(?:amp;)?commat;/gi, "@");

  // Decode decimal entities (including double-encoded variants)
  // &#64; and &amp;#64; → @
  // &#NNN; and &amp;#NNN; → corresponding character
  result = result.replace(/&(?:amp;)?#(\d+);/g, (match, code) => {
    const codePoint = parseInt(code, 10);
    // Validate code point is in valid Unicode range
    if (codePoint >= 0 && codePoint <= 0x10ffff) {
      return String.fromCodePoint(codePoint);
    }
    // Return original match if invalid
    return match;
  });

  // Decode hex entities (including double-encoded variants)
  // &#x40;, &#X40;, &amp;#x40;, &amp;#X40; → @
  // &#xHHH;, &#XHHH;, &amp;#xHHH;, &amp;#XHHH; → corresponding character
  result = result.replace(/&(?:amp;)?#[xX]([0-9a-fA-F]+);/g, (match, code) => {
    const codePoint = parseInt(code, 16);
    // Validate code point is in valid Unicode range
    if (codePoint >= 0 && codePoint <= 0x10ffff) {
      return String.fromCodePoint(codePoint);
    }
    // Return original match if invalid
    return match;
  });

  return result;
}

/**
 * Performs text hardening to protect against Unicode-based attacks.
 * This applies multiple layers of character normalization and filtering
 * to ensure consistent text processing and prevent visual spoofing.
 *
 * @param {string} text - Input text to harden
 * @returns {string} Hardened text with Unicode security applied
 */
function hardenUnicodeText(text) {
  if (!text || typeof text !== "string") {
    return "";
  }

  let result = text;

  // Step 1: Normalize Unicode to canonical composition (NFC)
  // This ensures consistent character representation across different encodings
  result = result.normalize("NFC");

  // Step 2: Decode HTML entities to prevent @mention bypass
  // This MUST happen early, before any other processing, to ensure entities
  // are converted to their actual characters for proper sanitization
  result = decodeHtmlEntities(result);

  // Step 3: Strip invisible zero-width characters that can hide content
  // These include: zero-width space, zero-width non-joiner, zero-width joiner,
  // word joiner, and byte order mark
  result = result.replace(/[\u200B\u200C\u200D\u2060\uFEFF]/g, "");

  // Step 4: Remove bidirectional text override controls
  // These can be used to reverse text direction and create visual spoofs
  result = result.replace(/[\u202A\u202B\u202C\u202D\u202E\u2066\u2067\u2068\u2069]/g, "");

  // Step 5: Convert full-width ASCII characters to standard ASCII
  // Full-width characters (U+FF01-FF5E) can be used to bypass filters
  result = result.replace(/[\uFF01-\uFF5E]/g, char => {
    const code = char.charCodeAt(0);
    // Map full-width to half-width by subtracting offset
    const standardCode = code - 0xfee0;
    return String.fromCharCode(standardCode);
  });

  return result;
}

/**
 * Core sanitization function without mention filtering
 * @param {string} content - The content to sanitize
 * @param {number} [maxLength] - Maximum length of content (default: 524288)
 * @returns {string} The sanitized content
 */
function sanitizeContentCore(content, maxLength) {
  if (!content || typeof content !== "string") {
    return "";
  }

  // Build list of allowed domains from environment and GitHub context
  const allowedDomains = buildAllowedDomains();

  // Build list of allowed GitHub references from environment
  const allowedGitHubRefs = buildAllowedGitHubReferences();

  let sanitized = content;

  // Apply Unicode hardening first to normalize text representation
  // This prevents Unicode-based attacks and ensures consistent processing
  sanitized = hardenUnicodeText(sanitized);

  // Remove ANSI escape sequences and control characters early
  // This must happen before mention neutralization to avoid creating bare mentions
  // when control characters are removed between @ and username
  sanitized = sanitized.replace(/\x1b\[[0-9;]*[mGKH]/g, "");
  // Remove control characters except newlines (\n), tabs (\t), and carriage returns (\r)
  sanitized = sanitized.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, "");

  // Neutralize commands at the start of text (e.g., /bot-name)
  sanitized = neutralizeCommands(sanitized);

  // Neutralize ALL @mentions (no filtering in core version)
  sanitized = neutralizeAllMentions(sanitized);

  // Remove XML comments first
  sanitized = removeXmlComments(sanitized);

  // Convert XML tags to parentheses format to prevent injection
  sanitized = convertXmlTags(sanitized);

  // URI filtering - replace non-https protocols with "(redacted)"
  sanitized = sanitizeUrlProtocols(sanitized);

  // Domain filtering for HTTPS URIs
  sanitized = sanitizeUrlDomains(sanitized, allowedDomains);

  // Apply truncation limits
  sanitized = applyTruncation(sanitized, maxLength);

  // Neutralize GitHub references if restrictions are configured
  sanitized = neutralizeGitHubReferences(sanitized, allowedGitHubRefs);

  // Neutralize common bot trigger phrases
  sanitized = neutralizeBotTriggers(sanitized);

  // Balance markdown code regions to fix improperly nested fences
  // This repairs markdown where AI models generate nested code blocks at the same indentation
  const { balanceCodeRegions } = require("./markdown_code_region_balancer.cjs");
  sanitized = balanceCodeRegions(sanitized);

  // Trim excessive whitespace
  return sanitized.trim();
}

module.exports = {
  sanitizeContentCore,
  getRedactedDomains,
  addRedactedDomain,
  clearRedactedDomains,
  writeRedactedDomainsLog,
  extractDomainsFromUrl,
  buildAllowedDomains,
  buildAllowedGitHubReferences,
  getCurrentRepoSlug,
  sanitizeDomainName,
  sanitizeUrlProtocols,
  sanitizeUrlDomains,
  neutralizeCommands,
  neutralizeGitHubReferences,
  removeXmlComments,
  convertXmlTags,
  neutralizeBotTriggers,
  applyTruncation,
  hardenUnicodeText,
  decodeHtmlEntities,
};
