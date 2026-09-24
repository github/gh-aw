// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Firewall Blocked Domains Module
 *
 * This module handles reading firewall logs and extracting blocked domains
 * for display in AI-generated footers.
 */

const fs = require("fs");
const path = require("path");
const { sanitizeDomainName } = require("./sanitize_content_core.cjs");
const { renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");
const { renderMarkdownTemplate } = require("./render_template.cjs");

// Internal AWF sidecar container hostnames added to network.topologyAttach by
// gh-aw itself (e.g. the MCP Gateway and the CLI proxy). These are
// framework-managed, not user-controllable external domains, and must never
// surface in the "blocked domains" warning shown on issues/PRs.
const AWF_INTERNAL_SIDECAR_HOSTS = ["awmg-mcpg", "awmg-cli-proxy"];

// Pre-compute sanitized forms at module load time.
// sanitizeDomainName strips non-alphanumeric characters (including hyphens),
// which is exactly how these container names appear after log sanitization
// (e.g. "awmg-mcpg" → "awmgmcpg", "awmg-cli-proxy" → "awmgcliproxy").
const AWF_INTERNAL_SIDECAR_HOSTS_SANITIZED = new Set(AWF_INTERNAL_SIDECAR_HOSTS.map(h => sanitizeDomainName(h)));

/**
 * Parses a single firewall log line
 * Format: timestamp client_ip:port domain dest_ip:port proto method status decision url user_agent
 * @param {string} line - Log line to parse
 * @returns {any|null} Parsed entry or null if invalid
 */
function parseFirewallLogLine(line) {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("#")) {
    return null;
  }

  // Split by whitespace but preserve quoted strings
  const fields = trimmed.match(/(?:[^\s"]+|"[^"]*")+/g);
  if (!fields || fields.length < 10) {
    return null;
  }

  // Only validate timestamp (essential for log format detection)
  const timestamp = fields[0];
  if (!/^\d+(\.\d+)?$/.test(timestamp)) {
    return null;
  }

  return {
    timestamp,
    clientIpPort: fields[1],
    domain: fields[2],
    destIpPort: fields[3],
    proto: fields[4],
    method: fields[5],
    status: fields[6],
    decision: fields[7],
    url: fields[8],
    userAgent: fields[9]?.replace(/^"|"$/g, "") || "-",
  };
}

/**
 * Determines if a request was blocked based on decision and status
 * @param {string} decision - Decision field (e.g., TCP_TUNNEL:HIER_DIRECT, NONE_NONE:HIER_NONE)
 * @param {string} status - Status code (e.g., 200, 403, 0)
 * @returns {boolean} True if request was blocked
 */
function isRequestBlocked(decision, status) {
  // Check status code first
  const statusCode = parseInt(status, 10);
  if (statusCode === 200 || statusCode === 206 || statusCode === 304) {
    return false;
  }

  if (statusCode === 403 || statusCode === 407) {
    return true;
  }

  // Check decision field
  if (decision.includes("NONE_NONE") || decision.includes("TCP_DENIED")) {
    return true;
  }

  if (decision.includes("TCP_TUNNEL") || decision.includes("TCP_HIT") || decision.includes("TCP_MISS")) {
    return false;
  }

  // Default to blocked for safety
  return true;
}

/**
 * Extracts the base domain from a domain:port string and sanitizes it
 * @param {string} domainWithPort - Domain with port (e.g., "example.com:443")
 * @returns {string} Sanitized base domain (e.g., "example.com")
 */
function extractAndSanitizeDomain(domainWithPort) {
  if (!domainWithPort || domainWithPort === "-") {
    return "";
  }

  // Remove port by taking everything before the last colon
  const lastColonIndex = domainWithPort.lastIndexOf(":");
  const domain = lastColonIndex > 0 ? domainWithPort.substring(0, lastColonIndex) : domainWithPort;

  // Sanitize the domain using the same function as content sanitization
  return sanitizeDomainName(domain);
}

/**
 * Reads firewall logs and extracts blocked domains
 *
 * This function checks two possible locations for firewall logs:
 * 1. /tmp/gh-aw/sandbox/firewall/logs/ (original location during agent execution)
 * 2. Path specified by logsDir parameter (for safe-outputs jobs with downloaded artifacts)
 *
 * @param {string} [logsDir] - Path to firewall logs directory. Defaults to /tmp/gh-aw/sandbox/firewall/logs
 * @returns {string[]} Array of unique blocked domains (sanitized, sorted)
 */
function getBlockedDomains(logsDir) {
  const squidLogsDir = logsDir || "/tmp/gh-aw/sandbox/firewall/logs/";

  // Check if logs directory exists
  if (!fs.existsSync(squidLogsDir)) {
    return [];
  }

  // Find all .log files
  let files;
  try {
    files = fs.readdirSync(squidLogsDir).filter(file => file.endsWith(".log"));
  } catch (error) {
    // If we can't read the directory, return empty array
    return [];
  }

  if (files.length === 0) {
    return [];
  }

  // Parse all log files and collect blocked domains
  const blockedDomainsSet = new Set();

  for (const file of files) {
    const filePath = path.join(squidLogsDir, file);

    let content;
    try {
      content = fs.readFileSync(filePath, "utf8");
    } catch (error) {
      // Skip files we can't read
      continue;
    }

    const lines = content.split("\n").filter(line => line.trim());

    for (const line of lines) {
      const entry = parseFirewallLogLine(line);
      if (!entry) {
        continue;
      }

      // Skip internal Squid error entries (client IP ::1, no domain, no destination)
      // These are internal Squid connection errors (e.g., error:transaction-end-before-headers)
      // and are not actual external network requests.
      // Example: 1773003472.027 ::1:52010 - -:- 0.0 - 0 NONE_NONE:HIER_NONE error:transaction-end-before-headers "-"
      if (entry.clientIpPort.startsWith("::1:") && entry.domain === "-" && (entry.destIpPort === "-:-" || entry.destIpPort === "-")) {
        continue;
      }

      // Check if request was blocked
      const isBlocked = isRequestBlocked(entry.decision, entry.status);
      if (isBlocked) {
        // When domain is "-" (iptables-dropped traffic not visible to Squid),
        // fall back to dest IP:port so blocked requests show their actual destination instead of "-"
        // Only fall back if destIpPort is a valid host:port (not "-" or "-:-" which are placeholder values)
        let domainField = entry.domain;
        if (domainField === "-" && entry.destIpPort !== "-" && entry.destIpPort !== "-:-") {
          domainField = entry.destIpPort;
        }
        const sanitizedDomain = extractAndSanitizeDomain(domainField);
        if (sanitizedDomain && sanitizedDomain !== "-" && !AWF_INTERNAL_SIDECAR_HOSTS_SANITIZED.has(sanitizedDomain)) {
          blockedDomainsSet.add(sanitizedDomain);
        }
      }
    }
  }

  // Convert to sorted array
  return Array.from(blockedDomainsSet).sort();
}

/**
 * Generates HTML details/summary section for blocked domains wrapped in a GitHub warning alert
 * @param {string[]} blockedDomains - Array of blocked domain names (expected to be pre-sanitized via getBlockedDomains)
 * @param {string} [templatePath] - Optional path to template file (defaults to RUNNER_TEMP/gh-aw/prompts/firewall_blocked_domains.md)
 * @returns {string} GitHub warning alert with details section, or empty string if no blocked domains
 */
function generateBlockedDomainsSection(blockedDomains, templatePath) {
  if (!blockedDomains || blockedDomains.length === 0) {
    return "";
  }

  const domainCount = blockedDomains.length;
  const domainWord = domainCount === 1 ? "domain" : "domains";
  const verb = domainCount === 1 ? "was" : "were";

  // Build domain bullet list lines
  const domainList = blockedDomains.map(domain => `> - \`${domain}\`\n`).join("");

  // Build YAML network.allowed list lines
  const yamlNetworkList = blockedDomains.map(domain => `>     - "${domain}"\n`).join("");

  const hasGitHubApiBlocked = blockedDomains.includes("api.github.com");

  // Resolve template path: explicit > GH_AW_PROMPTS_DIR / RUNNER_TEMP (production) > source tree (local dev/test)
  let resolvedTemplatePath = templatePath;
  if (!resolvedTemplatePath) {
    const hasRuntimePromptsDir = !!(process.env.RUNNER_TEMP || process.env.GH_AW_PROMPTS_DIR);
    resolvedTemplatePath = hasRuntimePromptsDir ? getPromptPath("firewall_blocked_domains.md") : path.join(__dirname, "../md/firewall_blocked_domains.md");
  }

  // First pass: substitute {key} placeholders.
  // has_github_api_blocked is set to the string "true" or "false" so that
  // renderMarkdownTemplate's isTruthy() correctly evaluates the
  // {{#if {has_github_api_blocked}}} conditional in the template
  // (isTruthy("false") === false per the template engine's explicit check).
  const rendered = renderTemplateFromFile(resolvedTemplatePath, {
    domain_count: domainCount,
    domain_word: domainWord,
    verb,
    domain_list: domainList,
    yaml_network_list: yamlNetworkList,
    has_github_api_blocked: hasGitHubApiBlocked ? "true" : "false",
  });

  // Second pass: evaluate {{#if ...}} conditional blocks (e.g. the gh-proxy tip section)
  // Template starts without leading newlines; prepend separator expected by callers
  return "\n\n" + renderMarkdownTemplate(rendered);
}

module.exports = {
  parseFirewallLogLine,
  isRequestBlocked,
  extractAndSanitizeDomain,
  getBlockedDomains,
  generateBlockedDomainsSection,
};
