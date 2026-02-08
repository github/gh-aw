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

/**
 * Parses a single firewall log line
 * Format: timestamp client_ip:port domain dest_ip:port proto method status decision url user_agent
 * @param {string} line - Log line to parse
 * @returns {object|null} Parsed entry or null if invalid
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
  if (statusCode === 403 || statusCode === 407) {
    return true;
  }

  // Check decision field
  if (decision.includes("NONE_NONE") || decision.includes("TCP_DENIED")) {
    return true;
  }

  // Check for allowed indicators
  if (statusCode === 200 || statusCode === 206 || statusCode === 304) {
    return false;
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

      // Check if request was blocked
      const isBlocked = isRequestBlocked(entry.decision, entry.status);
      if (isBlocked) {
        const sanitizedDomain = extractAndSanitizeDomain(entry.domain);
        if (sanitizedDomain && sanitizedDomain !== "-") {
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
 * @param {string[]} blockedDomains - Array of blocked domain names
 * @returns {string} GitHub warning alert with details section, or empty string if no blocked domains
 */
function generateBlockedDomainsSection(blockedDomains) {
  if (!blockedDomains || blockedDomains.length === 0) {
    return "";
  }

  const domainCount = blockedDomains.length;
  const domainWord = domainCount === 1 ? "domain" : "domains";

  let section = "\n\n> [!WARNING]\n";
  section += `> <details>\n`;
  section += `> <summary>⚠️ Firewall blocked ${domainCount} ${domainWord}</summary>\n`;
  section += `>\n`;
  section += `> The following ${domainWord} ${domainCount === 1 ? "was" : "were"} blocked by the firewall during workflow execution:\n`;
  section += `>\n`;

  // List domains as bullet points (within the alert)
  for (const domain of blockedDomains) {
    section += `> - \`${domain}\`\n`;
  }

  section += `>\n`;
  section += `> </details>\n`;

  return section;
}

module.exports = {
  parseFirewallLogLine,
  isRequestBlocked,
  extractAndSanitizeDomain,
  getBlockedDomains,
  generateBlockedDomainsSection,
};
