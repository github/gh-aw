// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { sanitizeWorkflowName } = require("./sanitize_workflow_name.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

// Internal AWF sidecar container hostnames added to network.topologyAttach by
// gh-aw itself. These are framework-managed and should be excluded from blocked
// domain reporting in step summaries so they do not appear as actionable items.
const AWF_INTERNAL_SIDECAR_HOSTS = new Set(["awmg-mcpg", "awmg-cli-proxy"]);

/**
 * Returns true when domainKey refers to a framework-internal sidecar container.
 * domainKey may be "hostname:port" or bare "hostname".
 * @param {string} domainKey
 * @returns {boolean}
 */
function isInternalSidecarHost(domainKey) {
  if (!domainKey || domainKey === "-") return false;
  const lastColon = domainKey.lastIndexOf(":");
  const host = lastColon > 0 ? domainKey.substring(0, lastColon) : domainKey;
  return AWF_INTERNAL_SIDECAR_HOSTS.has(host);
}

/**
 * Parses firewall logs and creates a step summary
 * Firewall log format: timestamp client_ip:port domain dest_ip:port proto method status decision url user_agent
 */

async function main() {
  try {
    // Get the firewall logs directory path - awf writes logs to /tmp/gh-aw/sandbox/firewall/logs
    const squidLogsDir = require("./runtime_paths.cjs").runtimePath("sandbox/firewall/logs");

    if (!fs.existsSync(squidLogsDir)) {
      core.info(`No firewall logs directory found at: ${squidLogsDir}`);
      return;
    }

    // Find all access.log files
    const files = fs.readdirSync(squidLogsDir).filter(file => file.endsWith(".log"));

    if (files.length === 0) {
      core.info(`No firewall log files found in: ${squidLogsDir}`);
      return;
    }

    core.info(`Found ${files.length} firewall log file(s)`);

    // Parse all log files and aggregate results
    let totalRequests = 0;
    let allowedRequests = 0;
    let blockedRequests = 0;
    const allowedDomains = new Set();
    const blockedDomains = new Set();
    const requestsByDomain = new Map();

    for (const file of files) {
      const filePath = path.join(squidLogsDir, file);
      core.info(`Parsing firewall log: ${file}`);

      const content = fs.readFileSync(filePath, "utf8");
      const lines = content.split("\n").filter(line => line.trim());

      const result = analyzeFirewallLogLines(lines);
      totalRequests += result.totalRequests;
      allowedRequests += result.allowedRequests;
      blockedRequests += result.blockedRequests;
      for (const domain of result.allowedDomains) {
        allowedDomains.add(domain);
      }
      for (const domain of result.blockedDomains) {
        blockedDomains.add(domain);
      }
      for (const [domain, stats] of result.requestsByDomain) {
        if (!requestsByDomain.has(domain)) {
          requestsByDomain.set(domain, { allowed: 0, blocked: 0 });
        }
        const existing = requestsByDomain.get(domain);
        existing.allowed += stats.allowed;
        existing.blocked += stats.blocked;
      }
    }

    // Generate step summary
    const summary = generateFirewallSummary({
      totalRequests,
      allowedRequests,
      blockedRequests,
      allowedDomains: Array.from(allowedDomains).sort(),
      blockedDomains: Array.from(blockedDomains).sort(),
      requestsByDomain,
    });

    await core.summary.addRaw(summary).write();
    core.info("Firewall log summary generated successfully");
  } catch (error) {
    core.setFailed(`${ERR_PARSE}: ${getErrorMessage(error)}`);
  }
}

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
 * Determines if a request was allowed based on decision and status
 * @param {string} decision - Decision field (e.g., TCP_TUNNEL:HIER_DIRECT, NONE_NONE:HIER_NONE)
 * @param {string} status - Status code (e.g., 200, 403, 0)
 * @returns {boolean} True if request was allowed
 */
function isRequestAllowed(decision, status) {
  // Check status code first
  const statusCode = parseInt(status, 10);
  if (statusCode === 200 || statusCode === 206 || statusCode === 304) {
    return true;
  }

  // Check decision field
  if (decision.includes("TCP_TUNNEL") || decision.includes("TCP_HIT") || decision.includes("TCP_MISS")) {
    return true;
  }

  if (decision.includes("NONE_NONE") || decision.includes("TCP_DENIED") || statusCode === 403 || statusCode === 407) {
    return false;
  }

  // Default to denied for safety
  return false;
}

/**
 * Analyzes an array of raw log lines and returns aggregated request statistics.
 * Internal Squid error entries (client IP ::1, no domain, no destination) are filtered out.
 * @param {string[]} lines - Raw log lines to analyze
 * @returns {{totalRequests: number, allowedRequests: number, blockedRequests: number, allowedDomains: Set<string>, blockedDomains: Set<string>, requestsByDomain: Map<string, {allowed: number, blocked: number}>}}
 */
function analyzeFirewallLogLines(lines) {
  let totalRequests = 0;
  let allowedRequests = 0;
  let blockedRequests = 0;
  const allowedDomains = new Set();
  const blockedDomains = new Set();
  const requestsByDomain = new Map();

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

    totalRequests++;

    // Determine if request was allowed or blocked
    const isAllowed = isRequestAllowed(entry.decision, entry.status);

    // When domain is "-" (iptables-dropped traffic not visible to Squid),
    // fall back to dest IP:port so blocked requests show their actual destination instead of "-"
    // Only fall back if destIpPort is a valid host:port (not "-" or "-:-" which are placeholder values)
    const domainKey = entry.domain !== "-" ? entry.domain : entry.destIpPort !== "-" && entry.destIpPort !== "-:-" ? entry.destIpPort : "-";

    if (isAllowed) {
      allowedRequests++;
      allowedDomains.add(domainKey);
    } else {
      // Skip internal sidecar hostnames (awmg-mcpg, awmg-cli-proxy) from the
      // blocked domain set. These are framework-managed topology-attach containers
      // and are not user-actionable external blocked domains.
      if (!isInternalSidecarHost(domainKey)) {
        blockedRequests++;
        blockedDomains.add(domainKey);
      }
    }

    // Track request count per domain.
    // Skip internal sidecar hostnames for blocked entries — they are already excluded from
    // blockedRequests/blockedDomains above and must not appear in the summary domain table.
    if (isAllowed || !isInternalSidecarHost(domainKey)) {
      if (!requestsByDomain.has(domainKey)) {
        requestsByDomain.set(domainKey, { allowed: 0, blocked: 0 });
      }
      const domainStats = requestsByDomain.get(domainKey);
      if (isAllowed) {
        domainStats.allowed++;
      } else {
        domainStats.blocked++;
      }
    }
  }

  return { totalRequests, allowedRequests, blockedRequests, allowedDomains, blockedDomains, requestsByDomain };
}

/**
 * Generates markdown summary from firewall log analysis
 * Uses details/summary structure with basic stats in summary and domain table in details
 * @param {any} analysis - Analysis results
 * @returns {string} Markdown formatted summary
 */
function generateFirewallSummary(analysis) {
  const { totalRequests, requestsByDomain } = analysis;

  // Filter out invalid domains (placeholder "-" values and Envoy error codes)
  const validDomains = Array.from(requestsByDomain.keys())
    .filter(domain => domain !== "-" && !domain.startsWith("error:"))
    .sort();
  const uniqueDomainCount = validDomains.length;

  // Calculate valid allowed and blocked requests in a single pass
  let validAllowedRequests = 0;
  let validBlockedRequests = 0;
  for (const domain of validDomains) {
    const stats = requestsByDomain.get(domain);
    validAllowedRequests += stats.allowed;
    validBlockedRequests += stats.blocked;
  }

  let summary = "";

  // Wrap entire summary in details/summary tags
  summary += "<details>\n";
  summary += `<summary>sandbox agent: ${totalRequests} request${totalRequests !== 1 ? "s" : ""} | `;
  summary += `${validAllowedRequests} allowed | `;
  summary += `${validBlockedRequests} blocked | `;
  summary += `${uniqueDomainCount} unique domain${uniqueDomainCount !== 1 ? "s" : ""}</summary>\n\n`;

  if (uniqueDomainCount > 0) {
    summary += "| Domain | Allowed | Blocked |\n";
    summary += "|--------|---------|--------|\n";

    for (const domain of validDomains) {
      const stats = requestsByDomain.get(domain);
      summary += `| ${domain} | ${stats.allowed} | ${stats.blocked} |\n`;
    }
  } else {
    summary += "No firewall activity detected.\n";
  }

  summary += "\n</details>\n\n";

  return summary;
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    parseFirewallLogLine,
    isRequestAllowed,
    analyzeFirewallLogLines,
    generateFirewallSummary,
    isInternalSidecarHost,
    main,
  };
}
