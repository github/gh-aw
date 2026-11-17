// @ts-check
/// <reference types="@actions/github-script" />

const { iterateJsonl } = require("./jsonl_helpers.cjs");
const fs = require("fs");

const AW_INFO_PATH = require("./runtime_paths.cjs").runtimePath("aw_info.json");
const AGENT_OUTPUT_PATH = require("./runtime_paths.cjs").runtimePath("agent_output.json");
const OTLP_EXPORT_ERRORS_PATH = require("./runtime_paths.cjs").runtimePath("otlp-export-errors.count");
const OTLP_EXPORT_ERROR_DETAILS_PATH = require("./runtime_paths.cjs").runtimePath("otlp-export-errors.jsonl");
const gatewayEventPaths = [require("./runtime_paths.cjs").runtimePath("mcp-logs/gateway.jsonl"), require("./runtime_paths.cjs").runtimePath("mcp-logs/rpc-messages.jsonl")];
// Squid access log paths: current AWF layout (squid-logs/ subdirectory) and legacy layout (directly under logs/).
const squidAccessLogPaths = [require("./runtime_paths.cjs").runtimePath("sandbox/firewall/logs/squid-logs/access.log"), require("./runtime_paths.cjs").runtimePath("sandbox/firewall/logs/access.log")];

function readJSONIfExists(path) {
  if (!fs.existsSync(path)) {
    return null;
  }

  try {
    return JSON.parse(fs.readFileSync(path, "utf8"));
  } catch {
    return null;
  }
}

function countBlockedRequests() {
  let total = 0;

  for (const path of gatewayEventPaths) {
    if (!fs.existsSync(path)) {
      continue;
    }

    let fileContent;
    try {
      fileContent = fs.readFileSync(path, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${path}: ${String(err)}`, { cause: err });
    }
    for (const entry of iterateJsonl(fileContent)) {
      if (entry && entry.type === "DIFC_FILTERED") total++;
    }
  }

  return total;
}

function uniqueCreatedItemTypes(items) {
  const types = new Set();

  for (const item of items) {
    if (item && typeof item.type === "string" && item.type.trim() !== "") {
      types.add(item.type);
    }
  }

  return [...types].sort();
}

function checkSquidAccessLogPresent() {
  for (const path of squidAccessLogPaths) {
    if (fs.existsSync(path)) {
      return true;
    }
  }
  return false;
}

function readOTLPExportErrorCount() {
  if (!fs.existsSync(OTLP_EXPORT_ERRORS_PATH)) {
    return 0;
  }

  try {
    const raw = fs.readFileSync(OTLP_EXPORT_ERRORS_PATH, "utf8").trim();
    const parsed = parseInt(raw, 10);
    return Number.isInteger(parsed) && parsed > 0 ? parsed : 0;
  } catch {
    return 0;
  }
}

function readOTLPExportErrorDetails() {
  if (!fs.existsSync(OTLP_EXPORT_ERROR_DETAILS_PATH)) {
    return [];
  }

  const details = [];
  try {
    for (const entry of iterateJsonl(fs.readFileSync(OTLP_EXPORT_ERROR_DETAILS_PATH, "utf8"))) {
      try {
        if (!entry || typeof entry !== "object") continue;
        const host = typeof entry.host === "string" ? entry.host.trim() : "";
        const reason = typeof entry.reason === "string" ? entry.reason.trim() : "";
        const status = Number.isInteger(entry.status) && entry.status > 0 ? entry.status : undefined;
        if (!host || !reason) continue;
        details.push(`${host}${status ? ` status=${status}` : ""} reason=${reason}`);
      } catch {
        // Ignore malformed JSONL lines.
      }
    }
  } catch {
    return [];
  }

  return details;
}

function collectObservabilityData() {
  const awInfo = readJSONIfExists(AW_INFO_PATH) || {};
  const agentOutput = readJSONIfExists(AGENT_OUTPUT_PATH) || { items: [], errors: [] };
  const items = Array.isArray(agentOutput.items) ? agentOutput.items : [];
  const errors = Array.isArray(agentOutput.errors) ? agentOutput.errors : [];
  // Prefer GITHUB_AW_OTEL_TRACE_ID (written to GITHUB_ENV by action_setup_otlp.cjs)
  // so the summary always shows the trace ID that is actually present in the OTLP backend.
  // Fall back to context.otel_trace_id for cross-workflow traces propagated from a parent.
  // Do NOT fall back to workflow_call_id — it is not a valid OTLP trace ID.
  const traceId = process.env.GITHUB_AW_OTEL_TRACE_ID || (awInfo.context ? awInfo.context.otel_trace_id || "" : "");

  const firewallEnabled = awInfo.firewall_enabled === true;
  return {
    workflowName: awInfo.workflow_name || "",
    engineId: awInfo.engine_id || "",
    traceId,
    staged: awInfo.staged === true,
    firewallEnabled,
    squidAccessLogPresent: firewallEnabled ? checkSquidAccessLogPresent() : null,
    createdItemCount: items.length,
    createdItemTypes: uniqueCreatedItemTypes(items),
    outputErrorCount: errors.length,
    blockedRequests: countBlockedRequests(),
    otlpExportErrors: readOTLPExportErrorCount(),
    otlpExportErrorDetails: readOTLPExportErrorDetails(),
  };
}

function buildObservabilitySummary(data) {
  const posture = data.createdItemCount > 0 ? "write-capable" : "read-only";
  const lines = [];

  lines.push("<details>");
  lines.push("<summary>Observability</summary>");
  lines.push("");

  if (data.workflowName) {
    lines.push(`- **workflow**: ${data.workflowName}`);
  }
  if (data.engineId) {
    lines.push(`- **engine**: ${data.engineId}`);
  }
  if (data.traceId) {
    lines.push(`- **trace id**: ${data.traceId}`);
  }

  lines.push(`- **posture**: ${posture}`);
  lines.push(`- **created items**: ${data.createdItemCount}`);
  lines.push(`- **blocked requests**: ${data.blockedRequests}`);
  lines.push(`- **agent output errors**: ${data.outputErrorCount}`);
  lines.push(`- **otlp export errors**: ${data.otlpExportErrors}`);
  lines.push(`- **firewall enabled**: ${data.firewallEnabled}`);
  if (data.firewallEnabled && data.squidAccessLogPresent !== null) {
    lines.push(`- **squid access.log present**: ${data.squidAccessLogPresent}`);
    if (!data.squidAccessLogPresent) {
      lines.push("- Squid access.log not found; egress traffic for this run cannot be audited.");
    }
  }
  lines.push(`- **staged**: ${data.staged}`);

  if (data.otlpExportErrors > 0) {
    lines.push("- OTLP export failures detected; telemetry may not be visible in the backend.");
  }

  if (data.createdItemTypes.length > 0) {
    lines.push("- **item types**:");
    for (const itemType of data.createdItemTypes) {
      lines.push(`  - ${itemType}`);
    }
  }

  if (data.otlpExportErrorDetails.length > 0) {
    lines.push("- **otlp export failure details**:");
    for (const detail of data.otlpExportErrorDetails) {
      lines.push(`  - ${detail}`);
    }
  }

  lines.push("");
  lines.push("</details>");

  return lines.join("\n") + "\n";
}

async function main(core) {
  const data = collectObservabilityData();
  const markdown = buildObservabilitySummary(data);
  await core.summary.addRaw(markdown).write();
  core.info("Generated observability summary in step summary");
}

module.exports = {
  buildObservabilitySummary,
  collectObservabilityData,
  main,
};
