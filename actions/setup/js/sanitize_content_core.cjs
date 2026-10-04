// @ts-check
/**
 * Core sanitization utilities without mention filtering
 * This module provides the base sanitization functions that don't require
 * mention resolution or filtering. It's designed to be imported by both
 * sanitize_content.cjs (full version) and sanitize_incoming_text.cjs (minimal version).
 */

const { isRepoAllowed } = require("./repo_helpers.cjs");
const { resolveMatchedCommand } = require("./slash_command_matcher.cjs");

const SAFE_OUTPUTS_URLS_ENV = "GH_AW_SAFE_OUTPUTS_URLS";
const SAFE_OUTPUTS_URLS_ALLOWED_ONLY = "allowed-only";
const SAFE_OUTPUTS_URLS_ALLOWED_OR_CODE_REGION = "allowed-or-code-region";

/**
 * Matches angle-bracket HTTPS autolinks in CommonMark and Slack mrkdwn form:
 *   <https://example.com/path>
 *   <https://example.com/path|Slack label>
 *
 * Capture groups:
 *   1: hostname with optional port  ([\w.-]+(?::\d+)?)
 *   2: optional path                (\/[^\s<>|]*)
 *   3: optional Slack label         ([^<>]*)
 *
 * The path intentionally stops at "|" so that the Slack label is captured
 * separately.  A bare "|" inside a URL path or query string (valid per
 * RFC 3986 but uncommon — browsers percent-encode it as %7C) is therefore
 * treated as the Slack label separator.  This is a known limitation; use
 * %7C in URLs where a literal "|" must be preserved verbatim.
 *
 * The strict hostname pattern ([\w.-]+) deliberately excludes IPv6 literals
 * and userinfo forms so that those fall through to tag-conversion instead of
 * being preserved as autolinks.
 *
 * Defined at module level (not inside sanitizeUrlDomains) so the RegExp object
 * is allocated once and reused across calls.  String.prototype.replace resets
 * lastIndex automatically, so the /g flag is safe here.
 */
const angleBracketHttpsAutolinkRegex = /<https:\/\/([\w.-]+(?::\d+)?)(\/[^\s<>|]*)?(?:\|([^<>]*))?>/g;

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
    try {
      fs.mkdirSync(dir, { recursive: true });
    } catch (err) {
      throw new Error(`Failed to create directory ${dir}: ${String(err)}`, { cause: err });
    }
  }

  // Write domains to file, one per line
  try {
    fs.writeFileSync(targetPath, redactedDomains.join("\n") + "\n");
  } catch (err) {
    throw new Error(`Failed to write file ${targetPath}: ${String(err)}`, { cause: err });
  }

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

  // Join the parts back together
  const joined = nonEmptyParts.join(".");

  // If the domain is longer than 48 characters, truncate to show first 24 and last 24
  if (joined.length > 48) {
    const first24 = joined.substring(0, 24);
    const last24 = joined.substring(joined.length - 24);
    return first24 + "…" + last24;
  }

  return joined;
}

/**
 * Character class (as a source fragment) for the delimiters that may introduce a
 * protocol-relative URL. A "//" is only treated as the start of a URL when it
 * sits at the start of the string or immediately after one of these, so that
 * "//" segments inside the path of an absolute URL (e.g.
 * "https://github.com//issues") are not misread as a new URL.
 *
 * Beyond whitespace/bracket/quote, this includes the delimiters that actually
 * precede a URL in rendered contexts: "<" and "=" for HTML attributes and
 * CommonMark angle-bracket link destinations (`<img src=//host/x>`,
 * `[a](<//host/x>)`), and ",", ">", "|" and "`" which separate URLs in prose,
 * tables and markup. Omitting these left renderable URLs unfiltered.
 *
 * Shared by the userinfo-stripping pre-pass and the protocol-relative filtering
 * pass so the two cannot disagree about what counts as a URL start: if the
 * strip pass recognized a URL the filter pass did not (or vice versa), a
 * spoofed host could be normalized into a form that is then trusted.
 */
const URL_START_DELIMITERS = "[\\s([{\"'<=,>|`]";

/**
 * Character class (as a source fragment) matching one character of a URL
 * authority (the "userinfo@host:port" component).
 *
 * The authority ends at the path/query/fragment ("/", "?", "#") and at
 * whitespace, but it must ALSO end at the delimiters that terminate a URL in
 * prose and markup. Consuming those was a bypass rather than a cosmetic issue:
 * with a class of merely [^\s/?#], the authority of the first URL in
 * "https://x.com,https://github.com@evil.com/" swallowed ",https:", so the
 * global scan resumed past the second URL's scheme and never stripped its
 * userinfo — leaving the spoofed host to be read as the allowlisted github.com.
 * The same applied to adjacent markdown images "![a](//x.com)![b](//host@evil)".
 */
const URL_AUTHORITY_CHAR = "[^\\s/?#,()<>[\\]{}\"'`|\\\\]";

/**
 * Remove the ASCII tab, CR and LF characters that URL parsers discard.
 *
 * WHATWG URL parsing strips these from anywhere in a URL before parsing, so
 * "//github.com\tA@evil.com/x" is fetched as host "evil.com" with the userinfo
 * "github.comA" — while a regex that treats them as terminators sees only the
 * allowlisted "github.com". Callers must therefore compare hosts on the
 * stripped form to match what a browser will actually request.
 *
 * @param {string} authority - The raw authority text
 * @returns {string} The authority with tab/CR/LF removed
 */
function stripUrlIgnorableWhitespace(authority) {
  return authority.replace(/[\t\r\n]/g, "");
}

/**
 * Strip URL userinfo (user:password@) from all scheme://... URLs in a string.
 * This must run before any domain filtering so that credentials embedded in
 * a URL authority are never passed to the allowlist check or returned to the
 * caller.
 *
 * Examples:
 *   https://user:REDACTED@example.com/repo.git  →  https://example.com/repo.git
 *   git://user@example.com/repo.git          →  git://example.com/repo.git
 *
 * @param {string} s - The string to process
 * @returns {string} The string with userinfo removed from all URLs
 */
function stripUrlUserinfo(s) {
  // Capture the authority-like component right after scheme:// - everything
  // up to the start of the path (/), query (?), fragment (#), or whitespace.
  //
  // The scheme quantifier is bounded ({0,30}) rather than unbounded (*). With
  // an unbounded quantifier, a long run of characters that never resolves to
  // "://" (e.g. hundreds of thousands of plain letters) forces the scheme
  // group to greedily consume the whole remainder and then backtrack one
  // character at a time before the match attempt fails at that start
  // position - and this repeats at every subsequent start position, giving
  // O(n^2) behavior on pathological input even though there is no nested
  // quantifier. Real URL schemes are always short (RFC 3986 examples and IANA
  // registrations top out well under 30 characters), so bounding the
  // quantifier keeps this linear without affecting legitimate matches.
  //
  // Once captured, look for the LAST "@" within that authority component (in
  // plain JS, not regex) and drop everything up to and including it. Using
  // the last "@" ensures chained userinfo values (e.g. "a@b@c@host") are
  // fully stripped, while stopping the authority match at "?"/"#" ensures an
  // ordinary URL whose query string happens to contain "@" is left untouched.
  //
  // Tab/CR/LF are allowed *inside* the authority and then discarded, because
  // URL parsers discard them too: without this, "https://github.com\tA@evil.com/"
  // would present an authority of just the allowlisted "github.com" to the
  // filter while a browser fetches evil.com. Tolerating them is safe because a
  // rewrite only happens when the cleaned authority contains "@" — so an
  // ordinary host that merely happens to be followed by a newline and more
  // prose is left untouched.
  const schemeUserinfoRegex = new RegExp(`([a-z][a-z0-9+.-]{0,30}://)((?:${URL_AUTHORITY_CHAR}|[\\t\\r\\n])*)`, "gi");
  return s.replace(schemeUserinfoRegex, (match, scheme, authority) => {
    const cleaned = stripUrlIgnorableWhitespace(authority);
    const at = cleaned.lastIndexOf("@");
    if (at === -1) return match;
    return scheme + cleaned.slice(at + 1);
  });
}

/**
 * Strip URL userinfo (user:password@) from protocol-relative URLs (//host/path).
 *
 * Browsers on an HTTPS page resolve "//host/path" to "https://host/path", so a
 * protocol-relative URL carries the same userinfo-spoofing risk as an explicit
 * https:// URL: in "//github.com@evil.com/x" the real host is evil.com, but a
 * host pattern that stops at "@" would read it as the allowlisted github.com.
 * stripUrlUserinfo() cannot cover this form because it requires a scheme.
 *
 * The "//" is only treated as a protocol-relative URL when it appears at the
 * start of the string or immediately after a URL-introducing delimiter (see
 * URL_START_DELIMITERS) — the same anchoring used by the protocol-relative pass
 * in sanitizeUrlDomains() — so "//" segments inside the path of an absolute URL
 * (e.g. "https://github.com//issues") are left untouched.
 *
 * Backslashes are accepted in the separator position because URL parsers treat
 * "\" as "/" for special schemes, so "\\github.com@evil.com/x" and
 * "/\github.com@evil.com/x" both resolve to host evil.com in a browser.
 *
 * @param {string} s - The string to process
 * @returns {string} The string with userinfo removed from protocol-relative URLs
 */
function stripProtocolRelativeUserinfo(s) {
  const protoRelativeUserinfoRegex = new RegExp(`(^|${URL_START_DELIMITERS})([/\\\\]{2})((?:${URL_AUTHORITY_CHAR}|[\\t\\r\\n])*)`, "g");
  return s.replace(protoRelativeUserinfoRegex, (match, prefix, _slashes, authority) => {
    const cleaned = stripUrlIgnorableWhitespace(authority);
    const at = cleaned.lastIndexOf("@");
    if (at === -1) return match;
    return prefix + "//" + cleaned.slice(at + 1);
  });
}

/**
 * Sanitize URL protocols - replace non-https with <sanitized-domain>/redacted
 * @param {string} s - The string to process
 * @returns {string} The string with non-https protocols redacted
 */
function sanitizeUrlProtocols(s) {
  // Strip userinfo (user:password@) from all scheme:// URLs before any other
  // processing so that credentials never appear in redaction summaries or logs.
  s = stripUrlUserinfo(s);

  // Normalize percent-encoded colons before applying the protocol filter.
  // This prevents bypasses via javascript%3Aalert(1) (single-encoded),
  // javascript%253Aalert(1) (double-encoded), or deeper nesting.
  // Strategy: iteratively decode %25 -> % (up to 4 passes, which handles
  // encodings up to 5 levels deep) until stable, then decode %3A -> :
  // so the filter regex always sees literal colons.
  let normalized = s;
  // Iteratively decode %25XX (percent-encoded percent signs) one level at a
  // time. 4 passes handles up to 5 encoding levels, which is far beyond the
  // 1-2 levels a browser uses during URL parsing. Any input requiring more
  // passes almost certainly has no browser-decoded equivalent that would
  // produce a dangerous protocol. The early-exit break keeps this O(n) for
  // typical non-malicious input.
  for (let i = 0; i < 4; i++) {
    // Replace %25XX (percent-encoded percent sign) with %XX one level at a time.
    const next = normalized.replace(/%25([0-9A-Fa-f]{2})/gi, "%$1");
    if (next === normalized) break;
    normalized = next;
  }
  normalized = normalized.replace(/%3[Aa]/gi, ":"); // decode %3A -> :

  // ── Step 1: allowlist-based protocol:// filtering ──────────────────────────
  // Redact every scheme://  URL that is NOT https://.  This covers http://,
  // ftp://, ssh://, git://, ws://, wss://, smb://, irc://, ldap://, ldaps://,
  // rtsp://, feed://, and any future schemes — eliminating the class of
  // blocklist-incompleteness bypasses.
  //
  // Regex anchors that protect existing https:// URLs:
  //   (?<![a-z0-9]) — negative lookbehind: ensures we do not match a suffix of
  //                   another protocol name (e.g. "ttps://" inside "https://…").
  //   (?!https://)  — negative lookahead: explicitly excludes https://, which is
  //                   passed through to sanitizeUrlDomains for domain filtering.
  let result = normalized.replace(/(?<![a-z0-9])(?!https:\/\/)([a-z][a-z0-9+.-]*)(:\/\/)([\w.-]*)([^\s]*)/gi, (_match, scheme, _slashes, domain, _rest) => {
    const fullMatch = _match;
    if (!domain) {
      // No host present (e.g. "file:///path" or bare "http://"). Use the scheme
      // (e.g. "file://") as the redacted-domain token so the redaction summary
      // remains useful without recording an empty-string entry.
      const truncated = fullMatch.length > 12 ? fullMatch.substring(0, 12) + "..." : fullMatch;
      core.info(`Redacted URL: ${truncated}`);
      addRedactedDomain(scheme.toLowerCase() + "://");
      return "(redacted)";
    }
    const domainLower = domain.toLowerCase();
    const sanitized = sanitizeDomainName(domainLower);
    const truncated = domainLower.length > 12 ? domainLower.substring(0, 12) + "..." : domainLower;
    core.info(`Redacted URL: ${truncated}`);
    addRedactedDomain(domainLower);
    return sanitized ? `(${sanitized}/redacted)` : "(redacted)";
  });

  // ── Step 2: blocklist-based single-colon scheme filtering ───────────────────
  // For schemes that do not use "//", a targeted blocklist is used because a
  // fully general single-colon pattern produces too many false positives
  // (e.g. "key:value" in YAML, "std::vector" in C++, "C:\path" on Windows).
  return result.replace(/(?:data|javascript|vbscript|about|mailto|tel|magnet):[^\s]+/gi, match => {
    const protocolMatch = match.match(/^([^:]+):/);
    if (protocolMatch) {
      const protocol = protocolMatch[1] + ":";
      const truncated = match.length > 12 ? match.substring(0, 12) + "..." : match;
      core.info(`Redacted URL: ${truncated}`);
      addRedactedDomain(protocol);
    }
    return "(redacted)";
  });
}

/**
 * Remove unknown domains
 * @param {string} s - The string to process
 * @param {string[]} allowed - List of allowed domains
 * @returns {string} The string with unknown domains redacted
 */
function sanitizeUrlDomains(s, allowed) {
  // Strip userinfo (user:password@) from HTTPS URLs before any domain filtering
  // so that credentials are never passed to the allowlist check or preserved
  // in the output for an allowed domain. Protocol-relative URLs (//host/path)
  // are stripped too, since browsers resolve them to https:// and they are
  // subject to the same allowlist check below.
  s = stripUrlUserinfo(s);
  s = stripProtocolRelativeUserinfo(s);

  // Match HTTPS URLs with optional port and path
  // This regex is designed to:
  // 1. Match https:// URIs with explicit protocol
  // 2. Capture the hostname/domain
  // 3. Allow optional port (:8080)
  // 4. Allow optional path and query string (but not trailing commas/periods)
  // 5. Stop before another https:// URL in query params (using negative lookahead)
  const httpsUrlRegex = /https:\/\/([\w.-]+(?::\d+)?)(\/(?:(?!https:\/\/)[^\s,])*)?/gi;

  /**
   * Shared domain-allowlist check and redaction logic.
   * @param {string} match - The full matched URL string
   * @param {string} hostnameWithPort - The hostname (and optional port) portion
   * @returns {string} The original match if allowed, or a redacted replacement
   */
  function applyDomainFilter(match, hostnameWithPort) {
    // Extract just the hostname (remove port if present)
    const hostname = hostnameWithPort.split(":")[0].toLowerCase();

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
      core.info(`Redacted URL: ${truncated}`);
      addRedactedDomain(hostname);
      // Return sanitized domain format
      return sanitized ? `(${sanitized}/redacted)` : "(redacted)";
    }
  }

  // Pass 1: handle angle-bracket autolinks and Slack mrkdwn links as a unit so
  // later generic URL matching does not consume the trailing ">" or "|label".
  //
  // Capture groups from angleBracketHttpsAutolinkRegex:
  //   1: hostnameWithPort
  //   2: optional path (stops before "|")
  //   3: optional Slack label (everything after "|" up to ">", may contain spaces)
  //
  // If the domain is disallowed the label text is preserved alongside the
  // redacted URL so that the author's visible link text is not silently lost.
  s = s.replace(angleBracketHttpsAutolinkRegex, (match, hostnameWithPort, path = "", label = "") => {
    const url = `https://${hostnameWithPort}${path}`;
    const filtered = applyDomainFilter(url, hostnameWithPort);
    if (filtered === url) return match; // allowed — preserve original angle-bracket form
    // Disallowed — redact URL but preserve label text if present so the author's
    // visible link text is not silently lost.
    return label ? `${filtered}|${label}` : filtered;
  });

  // Pass 2: handle remaining explicit https:// URLs
  s = s.replace(httpsUrlRegex, (match, hostnameWithPort) => applyDomainFilter(match, hostnameWithPort));

  // Second pass: handle protocol-relative URLs (//hostname/path).
  // Browsers on HTTPS pages resolve these to https://, so they must be subject
  // to the same domain allowlist check as explicit https:// URLs.
  // We only treat // as a protocol-relative URL when it appears at the start of
  // the string or immediately after a clear delimiter (whitespace, brackets,
  // or quotes). This avoids matching // segments inside the path of an allowed
  // https:// URL, such as "https://github.com//issues".
  // The path stop-condition (?!\/\/) stops before the next protocol-relative URL
  // (analogous to how the httpsUrlRegex stops before the next https:// URL).
  // Capture groups:
  // Second pass: handle protocol-relative URLs (//hostname/path).
  // Browsers on HTTPS pages resolve these to https://, so they must be subject
  // to the same domain allowlist check as explicit https:// URLs.
  // We only treat // as a protocol-relative URL when it appears at the start of
  // the string or immediately after a URL-introducing delimiter. The delimiter
  // set is shared with the userinfo-stripping pre-pass (URL_START_DELIMITERS)
  // so the two passes cannot disagree about where a URL begins. This avoids
  // matching // segments inside the path of an allowed https:// URL, such as
  // "https://github.com//issues".
  // The separator accepts backslashes ("\\host", "/\host") because URL parsers
  // treat "\" as "/" for special schemes, so those forms reach the same host.
  // The path stop-condition (?!\/\/) stops before the next protocol-relative URL
  // (analogous to how the httpsUrlRegex stops before the next https:// URL).
  // Capture groups:
  //   1: prefix (start-of-string or delimiter)
  //   2: separator (// or a backslash variant)
  //   3: hostname (and optional port)
  //   4: optional path
  const protoRelativeUrlRegex = new RegExp(`(^|${URL_START_DELIMITERS})([/\\\\]{2})([\\w.-]+(?::\\d+)?)((?:/(?:(?![/\\\\]{2})[^\\s,])*)?)`, "gi");

  s = s.replace(protoRelativeUrlRegex, (match, prefix, _separator, hostnameWithPort, path = "") => {
    // Normalize the separator to "//" so a backslash form can never survive as
    // an allowed URL in a shape the regex would not re-examine.
    const url = `//${hostnameWithPort}${path}`;
    return prefix + applyDomainFilter(url, hostnameWithPort);
  });

  return s;
}

/**
 * Creates a code-span wrapper whose delimiter length does not occur in the input.
 * This prevents surrounding attacker-controlled backticks from pairing with the
 * sanitizer's delimiters and exposing the wrapped token in rendered Markdown.
 * @param {string} s - The complete string being processed
 * @returns {(text: string, before?: string, after?: string) => string} A render-safe inline-code wrapper
 */
function createRenderSafeCodeSpanWrapper(s) {
  const usedDelimiterLengths = new Set(Array.from(s.matchAll(/`+/g), match => match[0].length));
  let delimiterLength = 1;
  while (usedDelimiterLengths.has(delimiterLength)) {
    delimiterLength++;
  }
  // Do not let an attacker inflate every replacement by supplying progressively
  // longer backtick runs. HTML code elements provide the same inert rendering
  // without a delimiter whose length depends on the input.
  if (delimiterLength > 16) {
    return text => `<code>${text.replace(/[&<>"']/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character])}</code>`;
  }
  const delimiter = "`".repeat(delimiterLength);
  return (text, before = "", after = "") => {
    const leadingSeparator = before.endsWith("`") ? " " : "";
    const trailingSeparator = after.startsWith("`") ? " " : "";
    return `${leadingSeparator}${delimiter}${text}${delimiter}${trailingSeparator}`;
  };
}

/**
 * Neutralizes commands at the start of text by wrapping them in a render-safe code span.
 * Reads all command names from GH_AW_COMMANDS (JSON array).
 * @param {string} s - The string to process
 * @returns {string} The string with neutralized commands
 */
function neutralizeCommands(s) {
  /** @type {string[]} */
  let commandNames = [];

  const commandsJSON = process.env.GH_AW_COMMANDS;
  if (commandsJSON) {
    try {
      const parsed = JSON.parse(commandsJSON);
      if (Array.isArray(parsed)) {
        commandNames = parsed.filter(c => typeof c === "string" && c.length > 0);
      }
    } catch {
      // Invalid JSON — ignored, no commands to neutralize.
    }
  }

  if (commandNames.length === 0) {
    return s;
  }

  const leadingWhitespace = s.match(/^\s*/)?.[0] ?? "";
  const remainder = s.slice(leadingWhitespace.length);
  const wrapInCodeSpan = createRenderSafeCodeSpanWrapper(s);
  const matchedCommand = resolveMatchedCommand(remainder, commandNames);
  if (matchedCommand) {
    const escapedCommand = matchedCommand.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return s.replace(new RegExp(`^(\\s*)/(${escapedCommand})\\b`, "i"), (match, whitespace, command, offset, input) => `${whitespace}${wrapInCodeSpan(`/${command}`, "", input[offset + match.length])}`);
  }

  for (const name of commandNames) {
    if (name.endsWith("*")) {
      continue;
    }
    const escapedCommand = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const result = s.replace(new RegExp(`^(\\s*)/(${escapedCommand})\\b`, "i"), (match, whitespace, command, offset, input) => `${whitespace}${wrapInCodeSpan(`/${command}`, "", input[offset + match.length])}`);
    if (result !== s) {
      return result;
    }
  }

  return s;
}

/**
 * Neutralizes ALL @mentions by wrapping them in render-safe code spans.
 * This is the core version without any filtering
 * @param {string} s - The string to process
 * @returns {string} The string with neutralized mentions
 */
function neutralizeAllMentions(s) {
  const wrapInCodeSpan = createRenderSafeCodeSpanWrapper(s);
  return applyToNonCodeRegions(s, (segment, regionBefore = "", regionAfter = "") => {
    // No filtering - all mentions outside matched code regions are neutralized.
    // Use an explicit ASCII class so underscores before mentions remain covered.
    return segment.replace(/(^|[^A-Za-z0-9])@([A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)/g, (match, prefix, alias, offset) => {
      core.info(`Escaped mention: @${alias} (not in allowed list)`);
      const before = prefix || (offset === 0 ? regionBefore : "");
      const after = segment[offset + match.length] || (offset + match.length === segment.length ? regionAfter : "");
      return `${prefix}${wrapInCodeSpan(`@${alias}`, before, after)}`;
    });
  });
}

/**
 * Returns the character ranges [start, end) of fenced code blocks in markdown content.
 * Fenced code blocks are delimited by lines starting with 3+ backticks or 3+ tildes.
 * The returned ranges span from the first character of the opening fence line through
 * the last character of the closing fence line (inclusive of any trailing newline).
 *
 * @param {string} s - Markdown content to scan
 * @returns {Array<[number, number]>} Array of [start, end) character positions
 */
function getFencedCodeRanges(s) {
  /** @type {Array<[number, number]>} */
  const ranges = [];
  const lines = s.split("\n");
  let pos = 0;
  let inBlock = false;
  let blockStart = -1;
  let fenceChar = "";
  let fenceLen = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();
    // Character position of the end of this line's content (not including the newline separator)
    const lineContentEnd = pos + line.length;
    // Character position after the newline separator (or same as lineContentEnd for the last line)
    const lineEnd = i < lines.length - 1 ? lineContentEnd + 1 : lineContentEnd;

    if (!inBlock) {
      // CommonMark permits at most three leading spaces. Backtick fence info
      // strings cannot contain backticks; such lines may instead contain an
      // inline code span followed by prose that still requires sanitization.
      const m = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
      if (m && (m[1][0] !== "`" || !m[2].includes("`"))) {
        inBlock = true;
        blockStart = pos;
        fenceChar = m[1][0];
        fenceLen = m[1].length;
      }
    } else {
      // A closing fence: same character, at least as long, only whitespace after
      const fc = fenceChar === "`" ? "\\`" : "~";
      // eslint-disable-next-line gh-aw-custom/require-escaped-regexp-interpolation -- `fc` is an already regex-escaped fence character and `fenceLen` is a numeric quantifier
      const closingRegex = new RegExp(`^ {0,3}[${fc}]{${fenceLen},}\\s*$`);
      if (closingRegex.test(line)) {
        ranges.push([blockStart, lineEnd]);
        inBlock = false;
        blockStart = -1;
        fenceChar = "";
        fenceLen = 0;
      }
    }

    pos = lineEnd;
  }

  // Unclosed fence – treat the rest as code (safer fallback)
  if (inBlock && blockStart !== -1) {
    ranges.push([blockStart, s.length]);
  }

  return ranges;
}

/**
 * Applies a transformation function to a text segment while skipping inline code spans
 * (backtick-delimited sequences).  The transformation is applied to each run of
 * non-code text; inline code spans are preserved verbatim.
 *
 * @param {string} text - The text to process (should not contain fenced code blocks)
 * @param {(s: string, before?: string, after?: string) => string} fn - Transformation to apply to non-code portions
 * @param {string} [outerBefore] - Character immediately before this text segment
 * @param {string} [outerAfter] - Character immediately after this text segment
 * @returns {string} The processed text
 */
function applyFnOutsideInlineCode(text, fn, outerBefore = "", outerAfter = "") {
  if (!text) return fn(text || "", outerBefore, outerAfter);

  const parts = [];
  let i = 0;
  let textStart = 0;

  while (i < text.length) {
    if (text[i] !== "`") {
      i++;
      continue;
    }

    // Count consecutive backticks at the current position
    const btStart = i;
    let btCount = 0;
    while (i < text.length && text[i] === "`") {
      btCount++;
      i++;
    }
    // i is now past the opening backtick sequence

    // Look for the matching closing sequence of exactly btCount backticks
    let closeIdx = -1;
    let j = i;
    while (j < text.length) {
      if (text[j] === "`") {
        let closeCount = 0;
        const jStart = j;
        while (j < text.length && text[j] === "`") {
          closeCount++;
          j++;
        }
        if (closeCount === btCount) {
          closeIdx = jStart;
          break;
        }
        // Different length – keep scanning (j already advanced past these backticks)
      } else {
        j++;
      }
    }

    if (closeIdx !== -1) {
      // Valid inline code span found: apply fn to the text before it, then keep the code span
      if (textStart < btStart) {
        const before = textStart > 0 ? text[textStart - 1] : outerBefore;
        parts.push(fn(text.slice(textStart, btStart), before, text[btStart]));
      }
      parts.push(text.slice(btStart, closeIdx + btCount));
      textStart = closeIdx + btCount;
      i = textStart;
    }
    // If no matching close was found, the backticks are treated as regular text (i already advanced)
  }

  // Apply fn to any remaining non-code text
  if (textStart < text.length) {
    const before = textStart > 0 ? text[textStart - 1] : outerBefore;
    parts.push(fn(text.slice(textStart), before, outerAfter));
  }

  return parts.join("");
}

/**
 * Applies a transformation function only to the non-code regions of markdown content.
 * Skips both fenced code blocks (``` / ~~~ delimited) and inline code spans (backtick
 * delimited) so that the transformation is not applied to code content.
 *
 * Falls back to applying fn to the entire string if any parsing error occurs.
 *
 * @param {string} s - Markdown content to process
 * @param {(s: string, before?: string, after?: string) => string} fn - Transformation to apply outside code regions
 * @returns {string} The content with the transformation applied only outside code regions
 */
function applyToNonCodeRegions(s, fn) {
  if (!s || typeof s !== "string") {
    return s || "";
  }

  try {
    const codeRanges = getFencedCodeRanges(s);

    if (codeRanges.length === 0) {
      // No fenced code blocks – still protect inline code spans
      return applyFnOutsideInlineCode(s, fn);
    }

    const parts = [];
    let pos = 0;

    for (const [start, end] of codeRanges) {
      if (pos < start) {
        // Non-code text before this code block: protect inline code spans
        parts.push(applyFnOutsideInlineCode(s.slice(pos, start), fn, pos > 0 ? s[pos - 1] : "", s[start]));
      }
      // Fenced code block: preserve verbatim
      parts.push(s.slice(start, end));
      pos = end;
    }

    // Non-code text after the last code block
    if (pos < s.length) {
      parts.push(applyFnOutsideInlineCode(s.slice(pos), fn, pos > 0 ? s[pos - 1] : "", ""));
    }

    return parts.join("");
  } catch (_e) {
    // Fallback: apply fn to the entire string (conservative – redacts more, never less)
    return fn(s, "", "");
  }
}

/**
 * Removes XML comments from content
 * @param {string} s - The string to process
 * @returns {string} The string with XML comments removed
 */
function removeXmlComments(s) {
  // Remove <!-- comment --> and malformed <!--! comment --!>
  // Uses a depth-tracking scan to correctly handle nested comment openers such as
  // <!-- <!-- --> PAYLOAD --> where a lazy regex would only consume the innermost
  // <!-- --> pair, leaving PAYLOAD visible in the output.
  let result = "";
  let commentDepth = 0;
  let position = 0;
  while (position < s.length) {
    const ch = s[position];
    if (ch === "<" && s.startsWith("<!--", position)) {
      // Comment opener — increase nesting depth regardless of current depth
      commentDepth++;
      position += 4;
    } else if (commentDepth > 0 && ch === "-" && s.startsWith("--!>", position)) {
      // Malformed comment closer --!> (only meaningful inside an open comment)
      commentDepth--;
      position += 4;
    } else if (commentDepth > 0 && ch === "-" && s.startsWith("-->", position)) {
      // Normal comment closer --> (only meaningful inside an open comment)
      commentDepth--;
      position += 3;
    } else {
      // Include character in output only when outside all comment regions
      if (commentDepth === 0) {
        result += ch;
      }
      position++;
    }
  }
  return result;
}

/**
 * Neutralizes quoted title text in markdown link syntax to prevent steganographic injection.
 * Link titles are invisible in rendered GitHub markdown (they appear only as hover-tooltips)
 * but pass through to the AI model in raw text, creating a hidden injection channel
 * structurally equivalent to HTML comments (which are already stripped by removeXmlComments).
 *
 * For inline links the title is moved into the visible link text as a parenthesised sub-element
 * so that the content is no longer hidden while accessibility information is preserved:
 *   [text](url "title")  → [text (title)](url)
 *   [text](url 'title')  → [text (title)](url)
 *   [text](url (title))  → [text (title)](url)
 *
 * Reference-style link definitions have no inline display text, so the title is stripped:
 *   [ref]: url "title"   → [ref]: url
 *   [ref]: url 'title'   → [ref]: url
 *   [ref]: url (title)   → [ref]: url
 *
 * @param {string} s - The string to process
 * @returns {string} The string with markdown link titles neutralized
 */
function neutralizeMarkdownLinkTitles(s) {
  // Move title into link text for inline links: [text](url "title") → [text (title)](url)
  // Capturing groups:
  //   1: opening bracket  [
  //   2: link text
  //   3: ]( separator
  //   4: angle-bracket URL  <url>  (mutually exclusive with group 5)
  //   5: bare URL           (mutually exclusive with group 4)
  //   6: double-quoted title text  (mutually exclusive with 7 and 8)
  //   7: single-quoted title text  (mutually exclusive with 6 and 8)
  //   8: parenthesized title text  (mutually exclusive with 6 and 7)
  //   9: optional whitespace before closing )
  s = s.replace(/(\[)([^\]]*)(\]\()(?:(<[^>]*>)|([^\s)]+))\s+(?:"([^"]*)"|'([^']*)'|\(([^)]*)\))(\s*\))/g, (match, ob, linkText, mid, angUrl, bareUrl, dqT, sqT, pT, close) => {
    const url = angUrl !== undefined ? angUrl : bareUrl;
    const title = dqT !== undefined ? dqT : sqT !== undefined ? sqT : pT;
    return `${ob}${linkText} (${title})${mid}${url}${close}`;
  });

  // Strip title from reference-style link definitions: [ref]: url "title" → [ref]: url
  // These must appear at the start of a line (per CommonMark spec). The gm flag makes
  // ^ match after each newline so the substitution works correctly on multi-line content.
  s = s.replace(/^(\[[^\]]+\]:\s+\S+)\s+(?:"[^"]*"|'[^']*'|\([^)]*\))\s*$/gm, "$1");

  return s;
}

/**
 * Converts XML/HTML tags to parentheses format to prevent injection
 * @param {string} s - The string to process
 * @returns {string} The string with XML tags converted to parentheses
 */
function convertXmlTags(s) {
  // Allow safe HTML tags supported by GitHub Flavored Markdown:
  // b, blockquote, br, code, details, em, h1–h6, hr, i, img, li, ol, p, pre, strong, sub, summary, sup, table, tbody, td, th, thead, tr, ul
  // Plus GFM inline tags: abbr, del, ins, kbd, mark, s, span
  // Note: img on* event handlers and style are stripped by stripDangerousAttributes(); src is covered by sanitizeUrlDomains()
  const allowedTags = [
    "abbr",
    "b",
    "blockquote",
    "br",
    "code",
    "del",
    "details",
    "em",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
    "hr",
    "i",
    "img",
    "ins",
    "kbd",
    "li",
    "mark",
    "ol",
    "p",
    "pre",
    "s",
    "span",
    "strong",
    "sub",
    "summary",
    "sup",
    "table",
    "tbody",
    "td",
    "th",
    "thead",
    "tr",
    "ul",
  ];

  /**
   * Slack/CommonMark autolinks use angle brackets but are not HTML tags.
   * Preserve HTTPS forms here so downstream URL filtering can inspect them
   * without convertXmlTags rewriting them to parentheses first.
   *
   * Supported forms:
   *   <https://example.com/path>
   *   <https://example.com/path|label>
   *
   * @param {string} tagContent
   * @returns {boolean}
   */
  function isHttpsAngleBracketAutolink(tagContent) {
    // Only match simple hostname forms that sanitizeUrlDomains can parse and
    // filter.  The hostname must consist of word characters, dots, and hyphens
    // ([\w.-]+), which excludes:
    //   - IPv6 literals  e.g. https://[2001:db8::1]/
    //   - Userinfo forms e.g. https://user@evil.example/
    // Those forms fall through to the generic tag-conversion path and are
    // wrapped in parentheses instead of being preserved as autolinks.
    //
    // (\/[^\s<>|]*)? — optional path (mirrors angleBracketHttpsAutolinkRegex)
    // (?:\|[^<>]*)?  — optional Slack label, may contain spaces
    return /^https:\/\/[\w.-]+(?::\d+)?(\/[^\s<>|]*)?(?:\|[^<>]*)?$/.test(tagContent);
  }

  // First, process CDATA sections specially - convert tags inside them and the CDATA markers
  s = s.replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, (match, content) => {
    // Convert tags inside CDATA content
    const convertedContent = content.replace(/<(\/?[A-Za-z][A-Za-z0-9]*(?:[^>]*?))>/g, "($1)");
    // Return with CDATA markers also converted to parentheses
    return `(![CDATA[${convertedContent}]])`;
  });

  /**
   * Strips dangerous HTML attributes from an allowed tag's content string.
   * Removes on* event handler attributes (e.g. onclick, ontoggle), style,
   * title, and data-* attributes in all quoting forms (double-quoted,
   * single-quoted, unquoted, bare).
   *
   * title= and data-* are stripped because their values are invisible in
   * GitHub's rendered markdown (title= appears only as a hover-tooltip;
   * data-* attributes are stripped by GFM's sanitizer) but arrive at the
   * agent verbatim in raw text, creating a steganographic injection channel
   * structurally identical to the one fixed for markdown link titles.
   *
   * Safe attributes such as class, open, lang, id, etc. are preserved.
   *
   * Note: `\s+` (requiring at least one whitespace before the attribute name) is
   * intentional — HTML attributes are always separated from the tag name and from
   * each other by at least one whitespace character. Using `\s*` would risk false
   * matches inside tag names (e.g. matching "ong" inside "strong").
   *
   * @param {string} tagContent - Tag content without surrounding angle brackets
   * @returns {string} Tag content with dangerous attributes removed
   */
  function stripDangerousAttributes(tagContent) {
    // Match: one-or-more whitespace-or-slash + (on* | style | title | data-*) + optional =value
    // Value forms: "...", '...', or unquoted (no whitespace / > / quote chars), or bare (no =)
    // The unquoted form excludes >, whitespace, and all quote characters (', ", `) so it
    // cannot consume the closing > of the tag or straddle other attribute values.
    // Using [\s/]+ (instead of \s+) also strips dangerous attributes that are immediately
    // preceded by a "/" with no space — e.g. the malformed <img/onerror=alert(1) src=x>.
    return tagContent.replace(/[\s/]+(?:on\w+|style|title|data-[\w-]+)(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>"'`]*))?/gi, "");
  }

  // Convert opening tags: <tag> or <tag attr="value"> to (tag) or (tag attr="value")
  // Convert closing tags: </tag> to (/tag)
  // Convert self-closing tags: <tag/> or <tag /> to (tag/) or (tag /)
  // But preserve allowed safe tags (with dangerous attributes stripped)
  return s.replace(/<(\/?[A-Za-z!][^>]*?)>/g, (match, tagContent) => {
    if (isHttpsAngleBracketAutolink(tagContent)) {
      return match;
    }
    // Extract tag name from the content (handle closing tags and attributes)
    const tagNameMatch = tagContent.match(/^\/?\s*([A-Za-z][A-Za-z0-9]*)/);
    if (tagNameMatch) {
      const tagName = tagNameMatch[1].toLowerCase();
      if (allowedTags.includes(tagName)) {
        // Strip dangerous attributes (on* event handlers, style, title, data-*) before preserving
        const sanitizedContent = stripDangerousAttributes(tagContent);
        return `<${sanitizedContent}>`;
      }
    }
    return `(${tagContent})`; // Convert other tags to parentheses
  });
}

/**
 * Maximum number of bot trigger references allowed before filtering is applied.
 */
const MAX_BOT_TRIGGER_REFERENCES = 10;

/**
 * Neutralizes bot trigger phrases by wrapping them in render-safe code spans.
 * The first `maxBotMentions` unquoted trigger references are left unchanged;
 * any occurrences beyond that threshold are wrapped in backticks.
 * Already-quoted entries are never re-quoted.
 * @param {string} s - The string to process
 * @param {number} [maxBotMentions] - Number of occurrences to allow before escaping (default: MAX_BOT_TRIGGER_REFERENCES)
 * @returns {string} The string with excess bot triggers neutralized
 */
function neutralizeBotTriggers(s, maxBotMentions = MAX_BOT_TRIGGER_REFERENCES) {
  const pattern = /\b(fixes?|closes?|resolves?|fix|close|resolve)\s+#(\w+)/gi;
  let count = 0;
  const wrapInCodeSpan = createRenderSafeCodeSpanWrapper(s);
  return applyToNonCodeRegions(s, (segment, regionBefore = "", regionAfter = "") => {
    return segment.replace(pattern, (match, action, ref, offset) => {
      count++;
      if (count <= maxBotMentions) {
        return match;
      }
      const before = segment[offset - 1] || (offset === 0 ? regionBefore : "");
      const after = segment[offset + match.length] || (offset + match.length === segment.length ? regionAfter : "");
      return wrapInCodeSpan(`${action} #${ref}`, before, after);
    });
  });
}

/**
 * Neutralizes template syntax delimiters to prevent potential template injection
 * if content is processed by downstream template engines.
 *
 * This is a defense-in-depth measure. GitHub's markdown rendering doesn't evaluate
 * template syntax, but this prevents issues if content is later processed by
 * template engines (Jinja2, Liquid, ERB, JavaScript template literals).
 *
 * Fenced code blocks (including GitHub suggestion blocks) and inline code spans are
 * preserved verbatim so that legitimate source content inside code regions is not altered.
 *
 * @param {string} s - The string to process
 * @returns {string} The string with escaped template delimiters (outside code regions)
 */
function neutralizeTemplateDelimiters(s) {
  if (!s || typeof s !== "string") {
    return "";
  }

  // Track which template types were detected (outside code regions) for deduped logging.
  const detectedTypes = new Set();

  /**
   * Escapes template delimiters in a plain-text segment (no fenced blocks or inline code).
   * @param {string} text - Plain text to escape
   * @returns {string} Text with template delimiters escaped
   */
  function escapeInText(text) {
    let result = text;

    // Escape Jinja2/Liquid double curly braces: {{ ... }}
    // Replace {{ with \{\{ to prevent template evaluation
    if (/\{\{/.test(result)) {
      if (!detectedTypes.has("jinja2")) {
        detectedTypes.add("jinja2");
        core.info("Template syntax detected: Jinja2/Liquid double braces {{");
      }
      result = result.replace(/\{\{/g, "\\{\\{");
    }

    // Escape ERB delimiters: <%= ... %>
    // Replace <%= with \<%= to prevent ERB evaluation
    if (/<%=/.test(result)) {
      if (!detectedTypes.has("erb")) {
        detectedTypes.add("erb");
        core.info("Template syntax detected: ERB delimiter <%=");
      }
      result = result.replace(/<%=/g, "\\<%=");
    }

    // Escape JavaScript template literal delimiters: ${ ... }
    // Replace ${ with \$\{ to prevent template literal evaluation
    if (/\$\{/.test(result)) {
      if (!detectedTypes.has("js")) {
        detectedTypes.add("js");
        core.info("Template syntax detected: JavaScript template literal ${");
      }
      result = result.replace(/\$\{/g, "\\$\\{");
    }

    // Escape Jinja2 comment delimiters: {# ... #}
    // Replace {# with \{\# to prevent Jinja2 comment evaluation
    if (/\{#/.test(result)) {
      if (!detectedTypes.has("jinja2comment")) {
        detectedTypes.add("jinja2comment");
        core.info("Template syntax detected: Jinja2 comment {#");
      }
      result = result.replace(/\{#/g, "\\{\\#");
    }

    // Escape Jekyll raw blocks: {% raw %} and {% endraw %}
    // Replace {% with \{\% to prevent Jekyll directive evaluation
    if (/\{%/.test(result)) {
      if (!detectedTypes.has("jekyll")) {
        detectedTypes.add("jekyll");
        core.info("Template syntax detected: Jekyll/Liquid directive {%");
      }
      result = result.replace(/\{%/g, "\\{\\%");
    }

    return result;
  }

  // Apply escaping only to non-code regions (skip fenced code blocks and inline code spans).
  // This preserves the verbatim content of suggestion blocks and other code fences.
  const result = applyToNonCodeRegions(s, escapeInText);

  // Log a summary warning if any template patterns were detected
  if (detectedTypes.size > 0) {
    core.warning(
      "Template-like syntax detected and escaped. " +
        "This is a defense-in-depth measure to prevent potential template injection " +
        "if content is processed by downstream template engines. " +
        "GitHub's markdown rendering does not evaluate template syntax."
    );
  }

  return result;
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
    core.info("GitHub reference filtering: all references will be escaped (GH_AW_ALLOWED_GITHUB_REFS is empty)");
    return []; // Empty array means escape all references
  }

  const refs = allowedRefsEnv
    .split(",")
    .map(ref => ref.trim().toLowerCase())
    .filter(ref => ref);
  core.info(`GitHub reference filtering: allowed repos = ${refs.join(", ")}`);
  return refs;
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
 * Neutralizes GitHub references (#123 or owner/repo#456) by wrapping them in
 * render-safe code spans
 * if they reference repositories not in the allowed list.
 * Supports wildcard patterns (e.g., "myorg/*", "*") via isRepoAllowed().
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
  const wrapInCodeSpan = createRenderSafeCodeSpanWrapper(s);

  // Expand the special "repo" keyword to the current repo slug and build a Set for isRepoAllowed()
  const allowedSet = new Set(allowedRepos.map(r => (r === "repo" ? currentRepo : r)));

  return applyToNonCodeRegions(s, (segment, regionBefore = "", regionAfter = "") => {
    // Match #123 and owner/repo#456 outside matched code regions.
    return segment.replace(/(^|[^\w])(?:([a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?)\/([a-z0-9._-]+))?#(\w+)/gi, (match, prefix, owner, repo, issueNum, offset) => {
      let targetRepo;

      if (owner && repo) {
        // Cross-repo reference: owner/repo#123
        targetRepo = `${owner}/${repo}`.toLowerCase();
      } else {
        // Current repo reference: #123
        targetRepo = currentRepo;
      }

      // Check if this repo is allowed using isRepoAllowed (supports wildcard patterns)
      if (isRepoAllowed(targetRepo, allowedSet)) {
        return match; // Keep the original reference
      }

      const refText = owner && repo ? `${owner}/${repo}#${issueNum}` : `#${issueNum}`;
      core.info(`Escaped GitHub reference: ${refText} (not in allowed list)`);
      const before = prefix || (offset === 0 ? regionBefore : "");
      const after = segment[offset + match.length] || (offset + match.length === segment.length ? regionAfter : "");
      return `${prefix}${wrapInCodeSpan(refText, before, after)}`;
    });
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
 * Decodes HTML entities to prevent bypass of @mention detection and to ensure
 * HTML-encoded characters do not persist in sanitized output (e.g. &gt; in titles).
 * Handles named entities (e.g., &commat;, &gt;, &lt;, &amp;, &shy;, &zwnj;, &zwj;,
 * &lrm;, &rlm;, &ZeroWidthSpace;, &NoBreak;, &af;/&ApplyFunction;, &it;/&InvisibleTimes;,
 * &ic;/&InvisibleComma;), decimal entities (e.g., &#64;), and hex entities (e.g., &#x40;),
 * including double-encoded variants (e.g., &amp;commat;).
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

  // Decode common named HTML entities (including double-encoded variants)
  // These prevent HTML-encoded characters from persisting as literal entities
  // in sanitized output (e.g. a title containing &gt; instead of >).
  // &gt; and &amp;gt; → >
  result = result.replace(/&(?:amp;)?gt;/gi, ">");
  // &lt; and &amp;lt; → < (convertXmlTags will then neutralise any resulting tags)
  result = result.replace(/&(?:amp;)?lt;/gi, "<");
  // &amp; and &amp;amp; → & (decoded after gt/lt so &amp;gt; is already handled above)
  result = result.replace(/&(?:amp;)?amp;/gi, "&");

  // Decode named entities for invisible/formatting characters that are stripped in
  // hardenUnicodeText Step 3. Without this, the named-entity forms survive entity
  // decoding and defeat neutralizeAllMentions (e.g. @&shy;user passes the mention
  // regex because "&" is not in [A-Za-z0-9], then renders as @user in GitHub).
  // Each entity is decoded to its actual Unicode code point so Step 3 can strip it.
  // &shy; and &amp;shy; → U+00AD (soft hyphen)
  result = result.replace(/&(?:amp;)?shy;/gi, "\u00AD");
  // &zwnj; and &amp;zwnj; → U+200C (zero-width non-joiner)
  result = result.replace(/&(?:amp;)?zwnj;/gi, "\u200C");
  // &zwj; and &amp;zwj; → U+200D (zero-width joiner)
  result = result.replace(/&(?:amp;)?zwj;/gi, "\u200D");
  // &lrm; and &amp;lrm; → U+200E (left-to-right mark)
  result = result.replace(/&(?:amp;)?lrm;/gi, "\u200E");
  // &rlm; and &amp;rlm; → U+200F (right-to-left mark)
  result = result.replace(/&(?:amp;)?rlm;/gi, "\u200F");
  // &ZeroWidthSpace; and &amp;ZeroWidthSpace; → U+200B (zero-width space)
  result = result.replace(/&(?:amp;)?ZeroWidthSpace;/gi, "\u200B");
  // &NoBreak; and &amp;NoBreak; → U+2060 (word joiner)
  result = result.replace(/&(?:amp;)?NoBreak;/gi, "\u2060");
  // &af; / &ApplyFunction; and double-encoded variants → U+2061 (invisible function application)
  result = result.replace(/&(?:amp;)?(?:af|ApplyFunction);/gi, "\u2061");
  // &it; / &InvisibleTimes; and double-encoded variants → U+2062 (invisible times)
  result = result.replace(/&(?:amp;)?(?:it|InvisibleTimes);/gi, "\u2062");
  // &ic; / &InvisibleComma; and double-encoded variants → U+2063 (invisible separator)
  result = result.replace(/&(?:amp;)?(?:ic|InvisibleComma);/gi, "\u2063");
  // &ip; / &InvisiblePlus; and double-encoded variants → U+2064 (invisible plus)
  // Note: U+2064 is the upper bound of the \u2060-\u2064 range stripped in Step 3.
  result = result.replace(/&(?:amp;)?(?:ip|InvisiblePlus);/gi, "\u2064");

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
 * Unicode TR#39 confusables map for Cyrillic and Greek characters that are
 * visually identical or near-identical to Latin characters.
 * Keys are Cyrillic/Greek codepoints; values are their Latin equivalents.
 * Reference: https://www.unicode.org/reports/tr39/#Confusable_Detection
 */
const HOMOGLYPH_MAP = {
  // --- Cyrillic uppercase → Latin ---
  "\u0410": "A", // А → A
  "\u0412": "B", // В → B
  "\u0415": "E", // Е → E
  "\u041A": "K", // К → K
  "\u041C": "M", // М → M
  "\u041D": "H", // Н → H
  "\u041E": "O", // О → O
  "\u0420": "P", // Р → P
  "\u0421": "C", // С → C
  "\u0422": "T", // Т → T
  "\u0425": "X", // Х → X
  "\u0405": "S", // Ѕ → S (Cyrillic Dze)
  "\u0406": "I", // І → I (Cyrillic Byelorussian-Ukrainian I)
  // --- Cyrillic lowercase → Latin ---
  "\u0430": "a", // а → a
  "\u0435": "e", // е → e
  "\u043E": "o", // о → o
  "\u0440": "p", // р → p
  "\u0441": "c", // с → c
  "\u0445": "x", // х → x
  "\u0443": "y", // у → y
  "\u0456": "i", // і → i (Ukrainian/Byelorussian)
  "\u0455": "s", // ѕ → s (Macedonian dze)
  "\u0458": "j", // ј → j (Macedonian je)
  // --- Greek uppercase → Latin ---
  "\u0391": "A", // Α → A
  "\u0392": "B", // Β → B
  "\u0395": "E", // Ε → E
  "\u0396": "Z", // Ζ → Z
  "\u0397": "H", // Η → H
  "\u0399": "I", // Ι → I
  "\u039A": "K", // Κ → K
  "\u039C": "M", // Μ → M
  "\u039D": "N", // Ν → N
  "\u039F": "O", // Ο → O
  "\u03A1": "P", // Ρ → P
  "\u03A4": "T", // Τ → T
  "\u03A5": "Y", // Υ → Y
  "\u03A7": "X", // Χ → X
  // --- Greek lowercase → Latin ---
  "\u03BF": "o", // ο → o
  "\u03BD": "v", // ν → v
  "\u03B9": "i", // ι → i
};

/**
 * Regex matching only the exact characters present in HOMOGLYPH_MAP.
 * Built dynamically from the map keys to stay in sync without manual maintenance.
 */
const HOMOGLYPH_REGEX = new RegExp("[" + Object.keys(HOMOGLYPH_MAP).join("") + "]", "g");

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
  // left-to-right mark (U+200E), right-to-left mark (U+200F),
  // soft hyphen (U+00AD), combining grapheme joiner (U+034F),
  // word joiner (U+2060), invisible mathematical operators
  // (U+2061 FUNCTION APPLICATION, U+2062 INVISIBLE TIMES,
  //  U+2063 INVISIBLE SEPARATOR, U+2064 INVISIBLE PLUS),
  // and byte order mark
  result = result.replace(/[\u00AD\u034F\u200B-\u200F\u2060-\u2064\uFEFF]/g, "");

  // Step 3b: Strip Unicode Tag Characters block (U+E0000–U+E007F, Plane 14).
  // These 128 Cf-category codepoints have exact 1:1 ASCII equivalents
  // (e.g. U+E0041 = TAG LATIN CAPITAL LETTER A) and are completely invisible
  // in all standard renderers including GitHub Markdown, enabling fully
  // invisible prompt-injection payloads that decode 1:1 to ASCII content.
  // Represented as surrogate pairs \uDB40\uDC00–\uDB40\uDC7F in JavaScript.
  result = result.replace(/\uDB40[\uDC00-\uDC7F]/g, "");

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

  // Step 6: Apply NFKC normalization to handle compatibility characters
  // NFKC decomposes ligatures (ﬁ→fi), superscripts, circled letters, etc.
  // This must come after full-width conversion to avoid double-processing
  result = result.normalize("NFKC");

  // Step 7: Map Cyrillic and Greek homoglyph characters to their Latin equivalents
  // These characters are visually indistinguishable from Latin letters and are used
  // to bypass text filters while appearing to contain only ASCII-like content.
  // Based on Unicode TR#39 confusables (https://www.unicode.org/reports/tr39/).
  result = result.replace(HOMOGLYPH_REGEX, char => HOMOGLYPH_MAP[char]);

  return result;
}

/**
 * Core sanitization function without mention filtering
 * @param {string} content - The content to sanitize
 * @param {number} [maxLength] - Maximum length of content (default: 524288)
 * @param {number} [maxBotMentions] - Max bot trigger references before filtering (default: MAX_BOT_TRIGGER_REFERENCES)
 * @returns {string} The sanitized content
 */
function sanitizeContentCore(content, maxLength, maxBotMentions) {
  if (!content || typeof content !== "string") {
    return "";
  }

  // Apply truncation early to avoid running expensive operations on oversized inputs.
  // This is a pre-pass truncation on raw content; a second truncation pass is applied
  // later after normalization (which may reduce length via stripping invisible chars).
  content = applyTruncation(content, maxLength);

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

  // Remove XML comments before mention neutralization to prevent bypass: if removeXmlComments
  // ran after neutralizeAllMentions, a comment like <!-- @user payload --> would first become
  // <!-- `@user` payload --> and applyFnOutsideInlineCode would split at the backtick boundary,
  // preventing the full <!--...--> pattern from being matched.
  sanitized = applyToNonCodeRegions(sanitized, removeXmlComments);

  // Remove markdown link titles — a steganographic injection channel analogous to HTML comments.
  // Quoted title text ([text](url "TITLE") and [ref]: url "TITLE") is invisible in GitHub's
  // rendered markdown (shown only as hover-tooltips) but reaches the AI model verbatim.
  // Must run before mention neutralization for the same ordering reason as removeXmlComments.
  sanitized = applyToNonCodeRegions(sanitized, neutralizeMarkdownLinkTitles);

  // Convert XML tags to parentheses format – skip code blocks and inline code so that
  // type parameters (e.g. VBuffer<float32>) and code containing angle brackets are preserved
  sanitized = applyToNonCodeRegions(sanitized, convertXmlTags);

  // URI filtering - replace non-https protocols with "(redacted)"
  sanitized = applyURLSanitizationPolicy(sanitized, allowedDomains);

  // Apply truncation limits
  sanitized = applyTruncation(sanitized, maxLength);

  // Neutralize ALL @mentions after truncation so a length boundary cannot split
  // an inserted code-span delimiter and reactivate the mention.
  sanitized = neutralizeAllMentions(sanitized);

  // Neutralize GitHub references if restrictions are configured
  sanitized = neutralizeGitHubReferences(sanitized, allowedGitHubRefs);

  // Neutralize common bot trigger phrases
  sanitized = neutralizeBotTriggers(sanitized, maxBotMentions);

  // Neutralize template syntax delimiters (defense-in-depth)
  // This prevents potential issues if content is processed by downstream template engines
  sanitized = neutralizeTemplateDelimiters(sanitized);

  // Balance markdown code regions to fix improperly nested fences
  // This repairs markdown where AI models generate nested code blocks at the same indentation
  const { balanceCodeRegions } = require("./markdown_code_region_balancer.cjs");
  sanitized = balanceCodeRegions(sanitized);

  // Trim excessive whitespace
  return sanitized.trim();
}

/**
 * Apply URL sanitization using configured safe-outputs URL policy.
 * @param {string} content
 * @param {string[]} allowedDomains
 * @returns {string}
 */
function applyURLSanitizationPolicy(content, allowedDomains) {
  const urlPolicy = process.env[SAFE_OUTPUTS_URLS_ENV] || SAFE_OUTPUTS_URLS_ALLOWED_ONLY;
  if (urlPolicy === SAFE_OUTPUTS_URLS_ALLOWED_OR_CODE_REGION) {
    // Preserve fenced/inline code regions (including ```suggestion blocks) verbatim.
    // This avoids corrupting patch payloads while still sanitizing prose.
    let sanitized = applyToNonCodeRegions(content, sanitizeUrlProtocols);
    sanitized = applyToNonCodeRegions(sanitized, s => sanitizeUrlDomains(s, allowedDomains));
    return sanitized;
  }
  // Default policy ("allowed-only"): sanitize URLs in all content regions,
  // including fenced and inline code spans.
  let sanitized = sanitizeUrlProtocols(content);
  sanitized = sanitizeUrlDomains(sanitized, allowedDomains);
  return sanitized;
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
  applyURLSanitizationPolicy,
  createRenderSafeCodeSpanWrapper,
  getFencedCodeRanges,
  neutralizeCommands,
  neutralizeGitHubReferences,
  removeXmlComments,
  neutralizeMarkdownLinkTitles,
  convertXmlTags,
  applyToNonCodeRegions,
  neutralizeBotTriggers,
  MAX_BOT_TRIGGER_REFERENCES,
  neutralizeTemplateDelimiters,
  applyTruncation,
  hardenUnicodeText,
  decodeHtmlEntities,
};
