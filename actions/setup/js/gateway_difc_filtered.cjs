// @ts-check
/// <reference types="@actions/github-script" />

const { iterateJsonl, readLines } = require("./jsonl_helpers.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderReport } = require("./report_ast.cjs");
/**
 * Gateway DIFC Filtered Module
 *
 * This module handles reading MCP gateway logs and extracting DIFC_FILTERED events
 * for display in AI-generated footers.
 */

const fs = require("fs");
const { GATEWAY_JSONL_PATH, RPC_MESSAGES_PATH } = require("./constants.cjs");

/**
 * Parses JSONL content and extracts DIFC_FILTERED events
 * @param {string|Iterable<string>} jsonlContent - JSONL text or complete lines
 * @returns {Array<Object>} Array of DIFC_FILTERED event objects
 */
function parseDifcFilteredEvents(jsonlContent) {
  const filteredEvents = [];
  for (const entry of iterateJsonl(jsonlContent, { lineFilter: line => line.includes("DIFC_FILTERED") })) {
    if (entry?.type === "DIFC_FILTERED") {
      filteredEvents.push(entry);
    }
  }
  return filteredEvents;
}

/**
 * Reads DIFC_FILTERED events from MCP gateway logs.
 *
 * This function checks two possible locations for gateway logs:
 * 1. Path specified by gatewayJsonlPath (or /tmp/gh-aw/mcp-logs/gateway.jsonl by default)
 * 2. Path specified by rpcMessagesPath (or /tmp/gh-aw/mcp-logs/rpc-messages.jsonl as fallback)
 *
 * @param {string} [gatewayJsonlPath] - Path to gateway.jsonl. Defaults to /tmp/gh-aw/mcp-logs/gateway.jsonl
 * @param {string} [rpcMessagesPath] - Path to rpc-messages.jsonl fallback. Defaults to /tmp/gh-aw/mcp-logs/rpc-messages.jsonl
 * @returns {Array<Object>} Array of DIFC_FILTERED event objects
 */
function getDifcFilteredEvents(gatewayJsonlPath, rpcMessagesPath) {
  const jsonlPath = gatewayJsonlPath || GATEWAY_JSONL_PATH;
  const rpcPath = rpcMessagesPath || RPC_MESSAGES_PATH;

  if (fs.existsSync(jsonlPath)) {
    try {
      return parseDifcFilteredEvents(readLines(jsonlPath));
    } catch {
      return [];
    }
  }

  if (fs.existsSync(rpcPath)) {
    try {
      return parseDifcFilteredEvents(readLines(rpcPath));
    } catch {
      return [];
    }
  }

  return [];
}

/**
 * Generates HTML details/summary section for integrity-filtered items wrapped in a GitHub note alert.
 * @param {Array<Object>} filteredEvents - Array of DIFC_FILTERED event objects
 * @returns {string} GitHub note alert with details section, or empty string if no filtered events
 */
function generateDifcFilteredSection(filteredEvents) {
  const children = generateDifcFilteredReportSection(filteredEvents);
  return children.length ? "\n\n" + renderReport({ type: "report", title: "Integrity filtering", children }).body : "";
}

/**
 * @param {Array<Object>} filteredEvents
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function generateDifcFilteredReportSection(filteredEvents) {
  if (!filteredEvents || filteredEvents.length === 0) {
    return [];
  }

  // Deduplicate events by their significant fields
  const seen = new Set();
  const uniqueEvents = filteredEvents.filter(event => {
    const key = [event.html_url || "", event.tool_name || "", event.description || "", event.reason || ""].join("|");
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });

  const count = uniqueEvents.length;
  const itemWord = count === 1 ? "item" : "items";
  const verb = count === 1 ? "was" : "were";
  const subjectNegationPhrase = count === 1 ? "it doesn't" : "they don't";

  const maxItems = 16;
  const visibleEvents = uniqueEvents.slice(0, maxItems);
  const remainingCount = uniqueEvents.length - visibleEvents.length;

  /** @type {import("./report_ast.cjs").ReportBlock[][]} */
  const items = visibleEvents.map(event => {
    /** @type {import("./report_ast.cjs").ReportInline[]} */
    let reference;
    if (event.html_url) {
      /** @type {string | import("./report_markdown.cjs").ReportInlineSlot} */
      const label = /^[1-9]\d*$/.test(String(event.number)) ? { type: "inline_slot", children: [{ type: "issue_reference", number: String(event.number), agentTainted: false }] } : event.html_url;
      const link = parseReportMarkdown("[{label}]({url})", { context: { label, url: event.html_url }, agentTainted: true })[0];
      if (link.type !== "paragraph") throw new TypeError("Integrity reference must be an inline link");
      reference = link.children;
    } else {
      const desc = event.description ? event.description.replace(/^[a-z-]+:(?!\/\/)/i, "") : null;
      const validDesc = desc && desc !== "#unknown" ? desc : null;
      reference = validDesc
        ? [{ type: /^[\w.-]+\/[\w.-]+#[1-9]\d*$/.test(validDesc) ? "code" : "text", value: validDesc, agentTainted: true }]
        : [{ type: event.tool_name ? "code" : "text", value: event.tool_name || "-", agentTainted: true }];
    }
    const reason = (event.reason || "-").replace(/^Resource '[^']*' /, "").replace(/\n/g, " ");
    return [
      {
        type: "paragraph",
        agentTainted: false,
        children: [...reference, { type: "text", value: " ", agentTainted: false }, { type: event.tool_name ? "code" : "text", value: event.tool_name || "-", agentTainted: true }, { type: "text", value: `: ${reason}`, agentTainted: true }],
      },
    ];
  });

  if (remainingCount > 0) {
    items.push([{ type: "paragraph", agentTainted: false, children: [{ type: "text", value: `... and ${remainingCount} more ${remainingCount === 1 ? "item" : "items"}`, agentTainted: false }] }]);
  }

  const promptsDir = process.env.GH_AW_PROMPTS_DIR || `${process.env.RUNNER_TEMP}/gh-aw/prompts`;
  const remediationPath = `${promptsDir}/integrity_filter_remediation.md`;
  let remediationText;
  try {
    remediationText = fs.readFileSync(remediationPath, "utf8");
  } catch (err) {
    throw new Error(`Failed to read file ${remediationPath}: ${String(err)}`, { cause: err });
  }
  return [
    {
      type: "alert",
      kind: "NOTE",
      agentTainted: false,
      children: [
        {
          type: "details",
          summary: `🔒 Integrity filter blocked ${count} ${itemWord}`,
          agentTainted: false,
          children: [
            { type: "paragraph", agentTainted: false, children: [{ type: "text", value: `The following ${itemWord} ${verb} blocked because ${subjectNegationPhrase} meet the GitHub integrity level.`, agentTainted: false }] },
            { type: "list", ordered: false, items, agentTainted: false },
            ...parseReportMarkdown(remediationText, { agentTainted: false }),
          ],
        },
      ],
    },
  ];
}

module.exports = {
  parseDifcFilteredEvents,
  getDifcFilteredEvents,
  generateDifcFilteredSection,
  generateDifcFilteredReportSection,
};
