// @ts-check

const reportAST = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/** @typedef {import("./report_ast.cjs").Report} Report */
/** @typedef {import("./report_ast.cjs").ReportBlock} ReportBlock */
/** @typedef {import("./report_ast.cjs").ReportInline} ReportInline */

const MAX_BODY_LENGTH = 65536;
const SIZE_NOTICE = "Content redacted to fit size limit.";
const PROTECTED_TYPES = new Set(["xml_marker", "json_marker", "review_marker", "marker", "expiration", "closing_reference"]);

/** @param {ReportBlock | ReportInline} node */
function isSizeRedactable(node) {
  if (node.agentTainted === false || node.sizeProtected || PROTECTED_TYPES.has(node.type)) return false;
  if (node.type === "details" && Array.isArray(node.summary) && !node.summary.every(isSizeRedactable)) return false;
  if ("children" in node) return node.children.every(isSizeRedactable);
  if (node.type === "list") return node.items.every(item => item.every(isSizeRedactable));
  if (node.type === "table") return [...node.headers, ...node.rows.flat()].every(cell => cell.every(isSizeRedactable));
  return true;
}

/** @param {unknown} value @returns {number} */
function textSize(value) {
  if (typeof value === "string") return value.length;
  if (value && typeof value === "object") return Object.values(value).reduce((size, item) => size + textSize(item), 0);
  return 0;
}

/** @param {Report} report @param {Set<ReportBlock | ReportInline>} selected @returns {Report} */
function replaceSelected(report, selected) {
  const notices = new WeakSet();
  /** @template {ReportBlock | ReportInline} T @param {T[]} nodes @returns {T[]} */
  function compact(nodes) {
    return nodes.filter((node, index) => !notices.has(node) || !notices.has(nodes[index - 1]));
  }
  /** @param {ReportInline[]} nodes @returns {ReportInline[]} */
  function inlines(nodes) {
    return compact(
      nodes.map(node => {
        if (selected.has(node)) {
          /** @type {ReportInline} */
          const notice = { type: "text", value: SIZE_NOTICE, agentTainted: false };
          notices.add(notice);
          return notice;
        }
        return "children" in node ? { ...node, children: inlines(node.children) } : node;
      })
    );
  }
  /** @param {ReportBlock[]} nodes @returns {ReportBlock[]} */
  function blocks(nodes) {
    return compact(
      nodes.map(node => {
        if (selected.has(node)) {
          /** @type {ReportBlock} */
          const notice = { type: "paragraph", agentTainted: false, children: [{ type: "text", value: SIZE_NOTICE, agentTainted: false }] };
          notices.add(notice);
          return notice;
        }
        if (node.sizeProtected || PROTECTED_TYPES.has(node.type)) return node;
        switch (node.type) {
          case "paragraph":
          case "heading":
          case "task":
            return { ...node, children: inlines(node.children) };
          case "details":
            return { ...node, summary: Array.isArray(node.summary) ? inlines(node.summary) : node.summary, children: blocks(node.children) };
          case "quote":
          case "alert":
            return { ...node, children: blocks(node.children) };
          case "list":
            return { ...node, items: node.items.map(blocks) };
          case "table":
            return { ...node, headers: node.headers.map(inlines), rows: node.rows.map(row => row.map(inlines)) };
          default:
            return node;
        }
      })
    );
  }
  return { ...report, children: blocks(report.children), ...(report.preamble ? { preamble: blocks(report.preamble) } : {}) };
}

/**
 * Trim trailing whitespace before measuring publication output, replacing only agent-owned subtrees. Metadata
 * and footers are reserved in full; structured data is replaced only as a unit.
 * @param {Report} report
 * @param {number} [maxLength]
 * @returns {{title: string, body: string}}
 */
function renderSizedReport(report, maxLength = MAX_BODY_LENGTH) {
  if (!Number.isSafeInteger(maxLength) || maxLength < 1) throw new TypeError("Report body size limit must be a positive integer");
  /** @param {Report} document */
  function render(document) {
    const result = reportAST.renderReport(document);
    return { ...result, body: result.body.trimEnd() };
  }
  const original = render(report);
  if (original.body.length <= maxLength) return original;
  const normalized = reportAST.redactReportForRendering(report);
  reportAST.assertRedactedReport(normalized);
  /** @type {Array<{node: ReportBlock | ReportInline, size: number}>} */
  const candidates = [];
  /** @param {Array<ReportBlock | ReportInline>} nodes */
  function collect(nodes) {
    for (const node of nodes) {
      if (node.sizeProtected || PROTECTED_TYPES.has(node.type)) continue;
      if (isSizeRedactable(node)) {
        candidates.push({ node, size: textSize(node) });
        continue;
      }
      if ("children" in node) collect(node.children);
      if (node.type === "details" && Array.isArray(node.summary)) collect(node.summary);
      if (node.type === "list") node.items.forEach(collect);
      if (node.type === "table") [...node.headers, ...node.rows.flat()].forEach(collect);
    }
  }
  collect(normalized.children);
  if (normalized.preamble) collect(normalized.preamble);
  candidates.sort((left, right) => right.size - left.size);
  /** @type {Set<ReportBlock | ReportInline>} */
  const selected = new Set();
  let remaining = original.body.length - maxLength;
  for (const candidate of candidates) {
    selected.add(candidate.node);
    remaining -= Math.max(0, candidate.size - SIZE_NOTICE.length);
    if (remaining <= 0) break;
  }
  const fitted = render(replaceSelected(normalized, selected));
  if (fitted.body.length <= maxLength) return fitted;
  for (const candidate of candidates) selected.add(candidate.node);
  const minimal = render(replaceSelected(normalized, selected));
  if (minimal.body.length <= maxLength) return minimal;
  throw new Error(`${ERR_VALIDATION}: Report body exceeds maximum length of ${maxLength} characters; protected markers, footers, and framework content cannot be reduced`);
}

/**
 * Preserve existing Markdown exactly when it fits. On overflow, reserve the
 * trusted prefix/footer strings verbatim and fit only the agent body tree.
 * @param {string} body
 * @param {{prefix?: string, suffix?: string, maxLength?: number, allowedMentions?: string[]}} [options]
 */
function fitMarkdownBody(body, { prefix = "", suffix = "", maxLength = MAX_BODY_LENGTH, allowedMentions = [] } = {}) {
  if (!Number.isSafeInteger(maxLength) || maxLength < 1) throw new TypeError("Report body size limit must be a positive integer");
  if (prefix.length + body.length + suffix.length <= maxLength) return prefix + body + suffix;
  const budget = maxLength - prefix.length - suffix.length;
  if (budget < 1) throw new Error(`${ERR_VALIDATION}: Protected markers and footers exceed the maximum body length of ${maxLength} characters`);
  const children = parseReportMarkdown(body, { agentTainted: true, allowMetadata: true, minimumHeadingLevel: 2, allowedMentions });
  return prefix + renderSizedReport({ type: "report", title: "Comment", children }, budget).body + suffix;
}

module.exports = { MAX_BODY_LENGTH, SIZE_NOTICE, renderSizedReport, fitMarkdownBody };
