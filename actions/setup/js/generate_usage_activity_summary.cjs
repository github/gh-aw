#!/usr/bin/env node

// This script aggregates usage activity data from various log sources and generates
// a compact summary.json file for the usage artifact.
// usage-activity-summary/v1 structure:
//   firewall: total/allowed/blocked request counters
//   session: aggregate Copilot session event counters
//   gateway: tool-call counts, sizes, durations, and per-server/tool breakdowns
//   integrity: aggregate DIFC filtering counts from gateway/RPC logs
//   safe_outputs: total item count and per-type breakdown from safe-output-items manifest
//   experiments: A/B experiment variant assignments for the current run
//   working_set: cumulative input-token traffic relative to peak invocation input

const fs = require("fs");
const path = require("path");
const { readExperimentAssignments } = require("./experiment_helpers.cjs");
const { calculateWorkingSetFromJSONL } = require("./working_set_metrics.cjs");

require("./shim.cjs");

const SQUID_STATUS_INDEX = 6;
const SQUID_DECISION_INDEX = 7;
const SQUID_DOMAIN_INDEX = 2;
const SQUID_DEST_INDEX = 3;
const SQUID_CLIENT_INDEX = 1;
const LOCALHOST_CLIENT_PREFIX = "::1:";
const PLACEHOLDER_DOMAIN_KEY = "-";
const PLACEHOLDER_DEST_KEY = "-:-";
const ERROR_DOMAIN_PREFIX = "error:";
const AGENT_TOKEN_USAGE_PATH = "/tmp/gh-aw/usage/agent/token_usage.jsonl";
const RPC_EVENT_TO_TYPE = { rpc_request: "REQUEST", rpc_response: "RESPONSE", difc_filtered: "DIFC_FILTERED" };

function findFiles(rootDir, shouldIncludeFile, maxDepth = Number.POSITIVE_INFINITY, currentDepth = 0) {
  if (!fs.existsSync(rootDir)) {
    return [];
  }

  const files = [];
  let entries;
  try {
    entries = fs.readdirSync(rootDir, { withFileTypes: true });
  } catch {
    return [];
  }

  for (const entry of entries) {
    const entryPath = path.join(rootDir, entry.name);
    if (entry.isDirectory()) {
      if (currentDepth < maxDepth) {
        files.push(...findFiles(entryPath, shouldIncludeFile, maxDepth, currentDepth + 1));
      }
    } else if (entry.isFile() && shouldIncludeFile(entry)) {
      files.push(entryPath);
    }
  }
  return files;
}

function findPrefixedDirectories(parentDir, prefix) {
  if (!fs.existsSync(parentDir)) {
    return [];
  }
  let entries;
  try {
    entries = fs.readdirSync(parentDir, { withFileTypes: true });
  } catch {
    return [];
  }
  return entries.filter(entry => entry.isDirectory() && entry.name.startsWith(prefix)).map(entry => path.join(parentDir, entry.name));
}

/**
 * @param {string} [tokenUsagePath]
 * @returns {{ workingSet: ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"], ignoredRecords: number }}
 */
function parseWorkingSetMetrics(tokenUsagePath = AGENT_TOKEN_USAGE_PATH) {
  if (!fs.existsSync(tokenUsagePath)) {
    return calculateWorkingSetFromJSONL("");
  }
  try {
    return calculateWorkingSetFromJSONL(fs.readFileSync(tokenUsagePath, "utf-8"));
  } catch (err) {
    throw new Error(`Failed to read working-set token usage from ${tokenUsagePath}: ${String(err)}`, { cause: err });
  }
}

/**
 * Check if a Squid decision indicates an allowed request
 */
function isAllowedDecision(decision) {
  // Squid decision tokens appear in multiple formats (for example
  // TCP_TUNNEL:HIER_DIRECT and TCP_MISS/200), so normalize on the leading verb.
  const base = decision.trim().toUpperCase().split(/[/:]/)[0];
  return ["TCP_TUNNEL", "TCP_HIT", "TCP_MISS"].includes(base);
}

/**
 * Resolve the domain key used in aggregate firewall stats.
 *
 * @param {string} domain
 * @param {string} dest
 * @returns {string}
 */
function getFirewallDomainKey(domain, dest) {
  // Squid can emit either "-" or "-:-" for missing destination fields, so both
  // placeholders are treated as invalid destination keys.
  if (domain !== PLACEHOLDER_DOMAIN_KEY) {
    return domain;
  }
  if (!isPlaceholderFirewallField(dest)) {
    return dest;
  }
  return PLACEHOLDER_DOMAIN_KEY;
}

/**
 * @param {string} value
 * @returns {boolean}
 */
function isPlaceholderFirewallField(value) {
  return value === PLACEHOLDER_DEST_KEY || value === PLACEHOLDER_DOMAIN_KEY;
}

/**
 * @param {string} domain
 * @returns {boolean}
 */
function isValidDomainKey(domain) {
  return domain !== PLACEHOLDER_DOMAIN_KEY && !domain.startsWith(ERROR_DOMAIN_PREFIX);
}

/**
 * @param {string} client
 * @param {string} domain
 * @param {string} dest
 * @returns {boolean}
 */
function isInternalFirewallErrorEntry(client, domain, dest) {
  return client.startsWith(LOCALHOST_CLIENT_PREFIX) && domain === PLACEHOLDER_DOMAIN_KEY && isPlaceholderFirewallField(dest);
}

/**
 * Parse firewall logs and aggregate request counts
 */
function parseFirewallLogs() {
  const firewall = {
    total_requests: 0,
    allowed_requests: 0,
    blocked_requests: 0,
    allowed_domains: new Set(),
    blocked_domains: new Set(),
    requests_by_domain: {},
  };

  const firewallLogDirs = [
    "/tmp/gh-aw/sandbox/firewall/logs",
    "/tmp/gh-aw/threat-detection/sandbox/firewall/logs",
    ...findPrefixedDirectories("/tmp/gh-aw", "squid-logs-"),
    ...findPrefixedDirectories("/tmp/gh-aw/threat-detection", "squid-logs-"),
  ];

  for (const logDir of firewallLogDirs) {
    for (const logPath of findFiles(logDir, entry => entry.name.endsWith(".log"))) {
      try {
        const content = fs.readFileSync(logPath, "utf-8");
        const lines = content.split("\n");

        for (const raw of lines) {
          const line = raw.trim();
          if (!line || line.startsWith("#")) {
            continue;
          }

          const parts = line.split(/\s+/);
          if (parts.length < 8) {
            continue;
          }

          // Skip non-Squid diagnostic lines (WARNING:, DNS, Accepting, etc.) by
          // validating that the first field is a numeric Unix timestamp.
          if (!/^\d+(\.\d+)?$/.test(parts[0])) {
            continue;
          }

          const domain = parts[SQUID_DOMAIN_INDEX];
          const dest = parts[SQUID_DEST_INDEX];
          const client = parts[SQUID_CLIENT_INDEX] || "";
          const isInternalErrorEntry = isInternalFirewallErrorEntry(client, domain, dest);
          if (isInternalErrorEntry) {
            continue;
          }

          // Domain key resolution intentionally considers both domain and dest
          // because Squid may leave domain unset while dest remains usable.
          const domainKey = getFirewallDomainKey(domain, dest);
          // Keep total/allowed/blocked counters aligned with per-domain buckets by
          // excluding unresolved placeholder/error keys from both representations.
          if (!isValidDomainKey(domainKey)) {
            continue;
          }

          firewall.total_requests += 1;

          // Squid access log columns (0-based):
          // 0=timestamp 1=client 2=domain 3=dest 4=proto 5=method
          // 6=status 7=decision 8=url 9=user-agent
          // Keep indices named for easier maintenance if format changes.
          const status = parts[SQUID_STATUS_INDEX];
          const decision = parts[SQUID_DECISION_INDEX];

          let allowed = false;
          const code = parseInt(status, 10);
          if (!Number.isNaN(code) && [200, 206, 304].includes(code)) {
            allowed = true;
          }

          if (!allowed && isAllowedDecision(decision)) {
            allowed = true;
          }

          if (!firewall.requests_by_domain[domainKey]) {
            firewall.requests_by_domain[domainKey] = { allowed: 0, blocked: 0 };
          }

          if (allowed) {
            firewall.allowed_requests += 1;
            firewall.requests_by_domain[domainKey].allowed += 1;
            firewall.allowed_domains.add(domainKey);
          } else {
            firewall.blocked_requests += 1;
            firewall.requests_by_domain[domainKey].blocked += 1;
            firewall.blocked_domains.add(domainKey);
          }
        }
      } catch (err) {
        // Skip files that can't be read
        continue;
      }
    }
  }

  if (firewall.total_requests === 0) {
    return null;
  }

  const requestsByDomain = {};
  for (const [domain, stats] of Object.entries(firewall.requests_by_domain)) {
    if (!isValidDomainKey(domain)) {
      continue;
    }
    requestsByDomain[domain] = stats;
  }

  return {
    total_requests: firewall.total_requests,
    allowed_requests: firewall.allowed_requests,
    blocked_requests: firewall.blocked_requests,
    allowed_domains: Array.from(firewall.allowed_domains).filter(isValidDomainKey).sort(),
    blocked_domains: Array.from(firewall.blocked_domains).filter(isValidDomainKey).sort(),
    requests_by_domain: requestsByDomain,
  };
}

/**
 * Parse Copilot session event logs and aggregate counters
 */
function parseSessionLogs(sessionLogDirs = ["/tmp/gh-aw/sandbox/agent/logs/copilot-session-state", "/tmp/gh-aw/threat-detection/sandbox/agent/logs/copilot-session-state"]) {
  const session = {
    total_events: 0,
    session_starts: 0,
    session_shutdowns: 0,
    turns: 0,
    assistant_messages: 0,
    reasoning_events: 0,
    tool_execution_starts: 0,
    tool_execution_completes: 0,
    failed_tool_executions: 0,
  };

  for (const logDir of sessionLogDirs) {
    for (const eventsPath of findFiles(logDir, entry => entry.name === "events.jsonl", 1)) {
      try {
        const content = fs.readFileSync(eventsPath, "utf-8");
        const lines = content.split("\n");

        for (const raw of lines) {
          const line = raw.trim();
          if (!line || !line.startsWith("{")) {
            continue;
          }

          let entry;
          try {
            entry = JSON.parse(line);
          } catch {
            continue;
          }

          const eventType = String(entry.type || "")
            .trim()
            .toLowerCase();
          session.total_events += 1;

          if (eventType === "session.start") {
            session.session_starts += 1;
          } else if (eventType === "session.shutdown") {
            session.session_shutdowns += 1;
          } else if (eventType === "user.message") {
            session.turns += 1;
          } else if (eventType === "assistant.message") {
            session.assistant_messages += 1;
          }
          // Copilot session logs use both reasoning and assistant.reasoning
          // across CLI/runtime versions, so count both as reasoning events.
          else if (eventType === "reasoning" || eventType === "assistant.reasoning") {
            session.reasoning_events += 1;
          } else if (eventType === "tool.execution_start") {
            session.tool_execution_starts += 1;
          } else if (eventType === "tool.execution_complete") {
            session.tool_execution_completes += 1;
            const data = entry.data || {};
            const success = typeof data === "object" ? data.success !== false : true;
            if (!success) {
              session.failed_tool_executions += 1;
            }
          }
        }
      } catch (err) {
        // Skip files that can't be read
        continue;
      }
    }
  }

  return session.total_events > 0 ? session : null;
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function nonNegativeNumber(value) {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function jsonByteLength(value) {
  try {
    const serialized = JSON.stringify(value);
    return serialized === undefined ? 0 : Buffer.byteLength(serialized, "utf8");
  } catch {
    return 0;
  }
}

/**
 * @param {Map<string, number>} counts
 * @returns {Record<string, number>}
 */
function sortedCounts(counts) {
  return Object.fromEntries(Array.from(counts.entries()).sort(([left], [right]) => left.localeCompare(right)));
}

/**
 * @param {any} gateway
 * @param {string} serverName
 */
function getGatewayServer(gateway, serverName) {
  let server = gateway.servers.get(serverName);
  if (!server) {
    server = {
      server_name: serverName,
      request_count: 0,
      tool_call_count: 0,
      failed_calls: 0,
      total_input_size: 0,
      total_output_size: 0,
      total_duration_ms: 0,
    };
    gateway.servers.set(serverName, server);
  }
  return server;
}

/**
 * @param {any} gateway
 * @param {string} serverName
 * @param {string} toolName
 */
function getGatewayTool(gateway, serverName, toolName) {
  const key = JSON.stringify([serverName, toolName]);
  let tool = gateway.tools.get(key);
  if (!tool) {
    tool = {
      server_name: serverName,
      tool_name: toolName,
      call_count: 0,
      failed_calls: 0,
      total_input_size: 0,
      total_output_size: 0,
      max_input_size: 0,
      max_output_size: 0,
      total_duration_ms: 0,
      max_duration_ms: 0,
    };
    gateway.tools.set(key, tool);
  }
  return tool;
}

/**
 * @param {any} gateway
 * @param {string} serverName
 * @param {string} toolName
 * @param {number} inputSize
 * @param {string} timestamp
 */
function recordGatewayToolCall(gateway, serverName, toolName, inputSize, timestamp) {
  const server = getGatewayServer(gateway, serverName);
  const tool = getGatewayTool(gateway, serverName, toolName);
  const call = {
    tool_call_id: `call-${gateway.tool_calls.length + 1}`,
    timestamp,
    server_name: serverName,
    tool_name: toolName,
    request_size: inputSize,
    response_size: 0,
    duration_ms: 0,
    outcome: "incomplete",
  };
  gateway.tool_calls.push(call);
  gateway.total_calls += 1;
  gateway.total_input_size += inputSize;
  gateway.max_input_size = Math.max(gateway.max_input_size, inputSize);
  server.request_count += 1;
  server.tool_call_count += 1;
  server.total_input_size += inputSize;
  tool.call_count += 1;
  tool.total_input_size += inputSize;
  tool.max_input_size = Math.max(tool.max_input_size, inputSize);
  return call;
}

/**
 * @param {any} gateway
 * @param {string} serverName
 * @param {string} toolName
 * @param {{failed: boolean, outputSize: number, durationMs: number}} result
 * @param {{response_size: number, duration_ms: number, outcome: string}} call
 */
function recordGatewayToolResult(gateway, serverName, toolName, result, call) {
  const server = getGatewayServer(gateway, serverName);
  const tool = getGatewayTool(gateway, serverName, toolName);
  if (result.failed) {
    gateway.failed_calls += 1;
    server.failed_calls += 1;
    tool.failed_calls += 1;
  }
  gateway.total_output_size += result.outputSize;
  gateway.max_output_size = Math.max(gateway.max_output_size, result.outputSize);
  gateway.total_duration_ms += result.durationMs;
  gateway.max_duration_ms = Math.max(gateway.max_duration_ms, result.durationMs);
  server.total_output_size += result.outputSize;
  server.total_duration_ms += result.durationMs;
  tool.total_output_size += result.outputSize;
  tool.max_output_size = Math.max(tool.max_output_size, result.outputSize);
  tool.total_duration_ms += result.durationMs;
  tool.max_duration_ms = Math.max(tool.max_duration_ms, result.durationMs);
  call.response_size = result.outputSize;
  call.duration_ms = result.durationMs;
  call.outcome = result.failed ? "failure" : "success";
}

/**
 * @param {any} integrity
 * @param {Record<string, any>} entry
 */
function recordIntegrityFilteredEvent(integrity, entry) {
  const serverName = String(entry.server_id || entry.server_name || "unknown").trim() || "unknown";
  const toolName = String(entry.tool_name || "unknown").trim() || "unknown";
  const reason = String(entry.reason || "unknown").trim() || "unknown";
  integrity.total_filtered += 1;
  integrity.filtered_server_counts.set(serverName, (integrity.filtered_server_counts.get(serverName) || 0) + 1);
  integrity.filtered_tool_counts.set(toolName, (integrity.filtered_tool_counts.get(toolName) || 0) + 1);
  integrity.filtered_reason_counts.set(reason, (integrity.filtered_reason_counts.get(reason) || 0) + 1);
}

/**
 * @param {Record<string, any>} entry
 * @returns {string}
 */
function getRpcMessageType(entry) {
  if (typeof entry.type === "string" && entry.type) {
    return entry.type.toUpperCase();
  }
  const event = typeof entry.event === "string" ? entry.event.toLowerCase() : "";
  return RPC_EVENT_TO_TYPE[event] || event.toUpperCase();
}

/**
 * @param {any} activity
 * @param {string} content
 */
function parseGatewayJSONL(activity, content) {
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (!line || !line.startsWith("{")) {
      continue;
    }
    try {
      const entry = JSON.parse(line);
      if (!entry || typeof entry !== "object") {
        continue;
      }
      if (getRpcMessageType(entry) === "DIFC_FILTERED") {
        recordIntegrityFilteredEvent(activity.integrity, entry);
        continue;
      }
      const event = String(entry.event || "")
        .trim()
        .toLowerCase();
      const method = String(entry.method || "")
        .trim()
        .toLowerCase();
      if (event !== "tool_call" && method !== "tools/call") {
        continue;
      }
      const serverName = String(entry.server_name || entry.server_id || "").trim();
      const toolName = String(entry.tool_name || entry.method || "").trim();
      if (!serverName || !toolName) {
        continue;
      }
      const inputSize = nonNegativeNumber(entry.input_size);
      const outputSize = nonNegativeNumber(entry.output_size);
      const durationMs = nonNegativeNumber(entry.duration);
      const status = String(entry.status || "")
        .trim()
        .toLowerCase();
      const level = String(entry.level || "")
        .trim()
        .toLowerCase();
      const failed = status === "error" || String(entry.error || "").trim() !== "" || level === "error";
      const call = recordGatewayToolCall(activity.gateway, serverName, toolName, inputSize, String(entry.timestamp || ""));
      recordGatewayToolResult(activity.gateway, serverName, toolName, { failed, outputSize, durationMs }, call);
    } catch {
      continue;
    }
  }
}

/**
 * @param {any} activity
 * @param {string} content
 */
function parseRPCMessagesJSONL(activity, content) {
  const pending = new Map();
  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (!line || !line.startsWith("{")) {
      continue;
    }
    try {
      const entry = JSON.parse(line);
      if (!entry || typeof entry !== "object") {
        continue;
      }
      const messageType = getRpcMessageType(entry);
      if (messageType === "DIFC_FILTERED") {
        recordIntegrityFilteredEvent(activity.integrity, entry);
        continue;
      }
      const serverName = String(entry.server_id || entry.server_name || "").trim();
      const payload = entry.payload && typeof entry.payload === "object" ? entry.payload : null;
      if (!serverName || !payload) {
        continue;
      }
      if (String(entry.direction || "").toUpperCase() === "OUT" && messageType === "REQUEST" && payload.method === "tools/call") {
        const toolName = String(payload.params?.name || "").trim();
        if (!toolName) {
          continue;
        }
        const inputSize = jsonByteLength(payload.params?.arguments ?? payload.params);
        const call = recordGatewayToolCall(activity.gateway, serverName, toolName, inputSize, String(entry.timestamp || ""));
        if (payload.id !== null && payload.id !== undefined) {
          const key = JSON.stringify([serverName, payload.id]);
          pending.set(key, {
            serverName,
            toolName,
            timestampMs: Date.parse(entry.timestamp),
            call,
          });
        }
        continue;
      }
      if (String(entry.direction || "").toUpperCase() !== "IN" || messageType !== "RESPONSE" || payload.id === null || payload.id === undefined) {
        continue;
      }
      const key = JSON.stringify([serverName, payload.id]);
      const request = pending.get(key);
      if (!request) {
        continue;
      }
      pending.delete(key);
      const responseTimestampMs = Date.parse(entry.timestamp);
      const durationMs = Number.isFinite(request.timestampMs) && Number.isFinite(responseTimestampMs) ? Math.max(0, responseTimestampMs - request.timestampMs) : 0;
      const hasRPCError = payload.error !== null && payload.error !== undefined;
      const failed = hasRPCError || payload.result?.isError === true;
      const outputSize = jsonByteLength(hasRPCError ? payload.error : payload.result);
      recordGatewayToolResult(activity.gateway, request.serverName, request.toolName, { failed, outputSize, durationMs }, request.call);
    } catch {
      continue;
    }
  }
}

/**
 * Parse MCP gateway/RPC logs and aggregate tool-call and integrity-filter metrics.
 */
function parseGatewayActivity(logRoots = ["/tmp/gh-aw", "/tmp/gh-aw/threat-detection", "/tmp/gh-aw/sandbox/agent/logs", "/tmp/gh-aw/threat-detection/sandbox/agent/logs"]) {
  const activity = {
    gateway: {
      total_calls: 0,
      failed_calls: 0,
      total_input_size: 0,
      total_output_size: 0,
      max_input_size: 0,
      max_output_size: 0,
      total_duration_ms: 0,
      max_duration_ms: 0,
      servers: new Map(),
      tools: new Map(),
      tool_calls: [],
    },
    integrity: {
      total_filtered: 0,
      filtered_server_counts: new Map(),
      filtered_tool_counts: new Map(),
      filtered_reason_counts: new Map(),
    },
  };

  for (const root of logRoots) {
    const gatewayPath = [path.join(root, "mcp-logs/gateway.jsonl"), path.join(root, "gateway.jsonl")].find(candidate => fs.existsSync(candidate));
    const rpcPath = [path.join(root, "mcp-logs/rpc-messages.jsonl"), path.join(root, "rpc-messages.jsonl")].find(candidate => fs.existsSync(candidate));
    const selectedPath = gatewayPath || rpcPath;
    if (!selectedPath) {
      continue;
    }
    try {
      const content = fs.readFileSync(selectedPath, "utf-8");
      if (gatewayPath) {
        parseGatewayJSONL(activity, content);
      } else {
        parseRPCMessagesJSONL(activity, content);
      }
    } catch {
      continue;
    }
  }

  const gateway =
    activity.gateway.total_calls > 0
      ? {
          total_calls: activity.gateway.total_calls,
          failed_calls: activity.gateway.failed_calls,
          total_input_size: activity.gateway.total_input_size,
          total_output_size: activity.gateway.total_output_size,
          max_input_size: activity.gateway.max_input_size,
          max_output_size: activity.gateway.max_output_size,
          total_duration_ms: activity.gateway.total_duration_ms,
          max_duration_ms: activity.gateway.max_duration_ms,
          tool_calls: activity.gateway.tool_calls,
          servers: Array.from(activity.gateway.servers.values())
            .sort((left, right) => left.server_name.localeCompare(right.server_name))
            .map(server => ({
              ...server,
              avg_duration_ms: server.tool_call_count > 0 ? server.total_duration_ms / server.tool_call_count : 0,
            })),
          tools: Array.from(activity.gateway.tools.values())
            .sort((left, right) => left.server_name.localeCompare(right.server_name) || left.tool_name.localeCompare(right.tool_name))
            .map(tool => ({
              ...tool,
              avg_duration_ms: tool.call_count > 0 ? tool.total_duration_ms / tool.call_count : 0,
            })),
        }
      : null;
  const integrity =
    activity.integrity.total_filtered > 0
      ? {
          total_filtered: activity.integrity.total_filtered,
          filtered_server_counts: sortedCounts(activity.integrity.filtered_server_counts),
          filtered_tool_counts: sortedCounts(activity.integrity.filtered_tool_counts),
          filtered_reason_counts: sortedCounts(activity.integrity.filtered_reason_counts),
        }
      : null;
  return { gateway, integrity };
}

function parseGatewayLogs() {
  return parseGatewayActivity().gateway;
}

/**
 * Parse the safe-output-items manifest and aggregate item counts by type.
 * Reads the JSONL file written by the safe_outputs job and downloaded into
 * the conclusion job via the safe-outputs-items artifact.
 *
 * Three distinct return states let callers distinguish artifact provenance:
 *   • returns null                          → manifest file not found
 *   • returns { total_items: 0, ... }       → manifest present but contained no loggable items
 *   • returns { total_items: N, ... }       → manifest present with N items
 *   • throws                                → manifest file exists but could not be read
 *
 * @param {string} [manifestPath] - Path to the manifest file (defaults to MANIFEST_FILE_PATH)
 * @returns {{ total_items: number, items_by_type: Record<string, number> } | null}
 */
const MANIFEST_FILE_PATH = "/tmp/gh-aw/safe-output-items.jsonl";

function parseSafeOutputsManifest(manifestPath = MANIFEST_FILE_PATH) {
  if (!fs.existsSync(manifestPath)) {
    return null;
  }

  // Let read errors propagate so the caller can distinguish "unreadable file"
  // from "file present but no items" — both previously collapsed to null.
  let content;
  try {
    content = fs.readFileSync(manifestPath, "utf-8");
  } catch (error) {
    throw new Error(`Failed to read safe output manifest ${manifestPath}`, { cause: error });
  }

  const itemsByType = {};
  let totalItems = 0;

  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (!line || !line.startsWith("{")) {
      continue;
    }

    let entry;
    try {
      entry = JSON.parse(line);
    } catch {
      continue;
    }

    const itemType = String(entry.type || "").trim();
    if (!itemType) {
      continue;
    }

    totalItems += 1;
    itemsByType[itemType] = (itemsByType[itemType] || 0) + 1;
  }

  return {
    total_items: totalItems,
    items_by_type: itemsByType,
  };
}

/**
 * Parse A/B experiment assignments for the current run.
 * Reads the assignments.json file written by pick_experiment.cjs.
 * Returns null when no experiments are active for this run.
 *
 * @returns {{ assignments: Record<string, string> } | null}
 */
function parseExperimentsData() {
  const assignments = readExperimentAssignments();
  if (!assignments || Object.keys(assignments).length === 0) {
    return null;
  }
  return { assignments };
}

/**
 * Main function to generate usage activity summary
 */
function main() {
  const summary = { schema: "usage-activity-summary/v1" };

  // Parse firewall logs
  const firewall = parseFirewallLogs();
  if (firewall) {
    summary.firewall = firewall;
  }

  // Parse session logs
  const session = parseSessionLogs();
  if (session) {
    summary.session = session;
  }

  // Parse gateway/RPC logs once for both MCP and integrity-filter aggregates.
  const gatewayActivity = parseGatewayActivity();
  if (gatewayActivity.gateway) {
    summary.gateway = gatewayActivity.gateway;
  }
  if (gatewayActivity.integrity) {
    summary.integrity = gatewayActivity.integrity;
  }

  // Parse safe outputs manifest.
  // parseSafeOutputsManifest() has three distinct outcomes that drive the three
  // states downstream consumers need to distinguish:
  //   • safe_outputs absent        → manifest not found (artifact download failed or job never ran)
  //   • safe_outputs.total_items == 0 → manifest present, no items logged
  //   • safe_outputs.total_items > 0  → manifest present with N items
  // A read error is kept separate: it logs a warning but omits safe_outputs so
  // the consumer cannot mistake a broken artifact for a legitimately empty one.
  try {
    const safeOutputs = parseSafeOutputsManifest();
    if (safeOutputs === null) {
      core.info(`safe-output-items manifest not found at ${MANIFEST_FILE_PATH} — safe-outputs-items artifact may not have been downloaded`);
    } else {
      summary.safe_outputs = safeOutputs;
      if (safeOutputs.total_items === 0) {
        core.info(`safe-output-items manifest: 0 item(s) logged (file present but contained no loggable items)`);
      } else {
        core.info(`safe-output-items manifest: ${safeOutputs.total_items} item(s) logged (types: ${Object.keys(safeOutputs.items_by_type).join(", ")})`);
      }
    }
  } catch (err) {
    core.warning(`safe-output-items manifest could not be read from ${MANIFEST_FILE_PATH}: ${String(err)} — safe_outputs omitted from summary`);
  }

  // Include A/B experiment assignments so the CLI can read them from the usage artifact.
  const experiments = parseExperimentsData();
  if (experiments) {
    summary.experiments = experiments;
  }

  // Compute run-level Working-Set Rebuild Factor after the agent token-usage
  // file has been copied into the compact usage artifact.
  try {
    const { workingSet, ignoredRecords } = parseWorkingSetMetrics();
    summary.working_set = workingSet;
    if (ignoredRecords > 0) {
      core.warning(`Working-set rebuild measurement ignored ${ignoredRecords} malformed or unsupported token-usage record(s)`);
    }
  } catch (err) {
    summary.working_set = calculateWorkingSetFromJSONL("").workingSet;
    core.warning(`Working-set rebuild measurement unavailable: ${String(err)}`);
  }

  // Write summary to file
  const outputPath = "/tmp/gh-aw/usage/activity/summary.json";
  try {
    fs.writeFileSync(outputPath, JSON.stringify(summary, null, 2), "utf-8");
  } catch (err) {
    throw new Error(`Failed to write file ${outputPath}: ${String(err)}`, { cause: err });
  }
  core.info(outputPath);
}

// Run main function
if (require.main === module) {
  main();
}

module.exports = {
  parseFirewallLogs,
  parseSessionLogs,
  parseGatewayLogs,
  parseGatewayActivity,
  parseSafeOutputsManifest,
  parseExperimentsData,
  calculateWorkingSetFromJSONL,
  parseWorkingSetMetrics,
  AGENT_TOKEN_USAGE_PATH,
  MANIFEST_FILE_PATH,
};
