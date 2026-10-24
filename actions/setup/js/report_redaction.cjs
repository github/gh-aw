// @ts-check

const { normalizeReport } = require("./report_ast.cjs");

/** @typedef {import("./report_ast.cjs").Report} Report */
/** @typedef {import("./report_ast.cjs").ReportBlock} ReportBlock */
/** @typedef {import("./report_ast.cjs").ReportInline} ReportInline */
/** @typedef {(node: ReportBlock | ReportInline) => boolean} ReportRedactionPolicy */

/**
 * A coarse replacement must not swallow framework descendants.
 * Missing provenance is treated as agent-tainted; only explicit false marks a framework-owned node.
 * @param {ReportBlock | ReportInline} node
 * @returns {boolean}
 */
function isAgentSubtree(node) {
  if (node.agentTainted === false) return false;
  if (node.type === "details" && Array.isArray(node.summary) && !node.summary.every(isAgentSubtree)) return false;
  if ("children" in node) return node.children.every(isAgentSubtree);
  if (node.type === "list") return node.items.every(item => item.every(isAgentSubtree));
  if (node.type === "table") return [...node.headers, ...node.rows.flat()].every(cell => cell.every(isAgentSubtree));
  return true;
}

/** @returns {ReportInline} Plain framework-owned "Content redacted." text without link or formatting wrappers. */
function inlineRedactionNotice() {
  return { type: "text", value: "Content redacted.", agentTainted: false };
}

/** @returns {ReportBlock} */
function redactionNotice() {
  return { type: "paragraph", agentTainted: false, children: [inlineRedactionNotice()] };
}

/**
 * Replace selected agent-origin blocks as complete subtrees, not string fragments.
 * An inline match replaces its containing agent paragraph, heading, task, or table.
 * Framework and mixed-origin containers are traversed, never selected.
 * Missing provenance is tainted.
 * Tainted titles are offered as text nodes; selected titles become framework text.
 * This is an additional content policy, not a substitute for normal sanitization.
 * @param {Report} report
 * @param {ReportRedactionPolicy} shouldRedact
 * @returns {Report}
 */
function redactReport(report, shouldRedact) {
  if (typeof shouldRedact !== "function") throw new TypeError("Report redaction requires a block policy");
  const normalized = normalizeReport(report);
  /**
   * @param {ReportInline[]} nodes
   * @returns {{nodes: ReportInline[], redacted: boolean}}
   */
  function inlines(nodes) {
    let redacted = false;
    let matching = false;
    /** @type {ReportInline[]} */
    const result = [];
    for (const node of nodes) {
      const agentSubtree = isAgentSubtree(node);
      if (agentSubtree && shouldRedact(node)) {
        redacted = true;
        if (!matching) result.push(inlineRedactionNotice());
        matching = true;
        continue;
      }
      if ("children" in node) {
        const children = inlines(node.children);
        redacted ||= children.redacted;
        if (agentSubtree && children.redacted) {
          if (!matching) result.push(inlineRedactionNotice());
          matching = true;
          continue;
        }
        matching = false;
        result.push({ ...node, children: children.nodes });
      } else {
        matching = false;
        result.push(node);
      }
    }
    return { nodes: result, redacted };
  }
  /**
   * @param {ReportBlock[]} nodes
   * @returns {{nodes: ReportBlock[], redacted: boolean}}
   */
  function blocks(nodes) {
    let redacted = false;
    const result = nodes.map(node => {
      const agentSubtree = isAgentSubtree(node);
      if (agentSubtree && shouldRedact(node)) {
        redacted = true;
        return redactionNotice();
      }
      switch (node.type) {
        case "paragraph":
        case "heading":
        case "task": {
          const children = inlines(node.children);
          redacted ||= children.redacted;
          if (children.redacted && agentSubtree) return redactionNotice();
          return { ...node, children: children.nodes };
        }
        case "table": {
          const headers = node.headers.map(inlines);
          const rows = node.rows.map(row => row.map(inlines));
          const tableRedacted = [...headers, ...rows.flat()].some(cell => cell.redacted);
          redacted ||= tableRedacted;
          if (agentSubtree && tableRedacted) return redactionNotice();
          return { ...node, headers: headers.map(cell => cell.nodes), rows: rows.map(row => row.map(cell => cell.nodes)) };
        }
        case "details": {
          const children = blocks(node.children);
          const summary = Array.isArray(node.summary) ? inlines(node.summary) : undefined;
          const detailsRedacted = children.redacted || Boolean(summary?.redacted);
          redacted ||= detailsRedacted;
          if (agentSubtree && detailsRedacted) return redactionNotice();
          return { ...node, summary: summary ? summary.nodes : node.summary, children: children.nodes };
        }
        case "alert":
        case "quote": {
          const children = blocks(node.children);
          redacted ||= children.redacted;
          return { ...node, children: children.nodes };
        }
        case "list": {
          const items = node.items.map(blocks);
          redacted ||= items.some(item => item.redacted);
          return { ...node, items: items.map(item => item.nodes) };
        }
        default:
          return node;
      }
    });
    return { nodes: result, redacted };
  }
  const titleRedacted = normalized.agentTainted !== false && shouldRedact({ type: "text", value: normalized.title, agentTainted: true });
  const result = {
    ...normalized,
    ...(titleRedacted ? { title: "Content redacted.", agentTainted: false } : {}),
    children: blocks(normalized.children).nodes,
    ...(normalized.preamble ? { preamble: blocks(normalized.preamble).nodes } : {}),
  };
  if (normalized.footer) {
    const notices = [];
    const markers = normalized.footer.markers.filter(marker => {
      if (marker.agentTainted === false || !shouldRedact(marker)) return true;
      notices.push(redactionNotice());
      return false;
    });
    result.footer = { ...normalized.footer, children: [...blocks(normalized.footer.children).nodes, ...notices], markers };
  }
  return normalizeReport(result);
}

module.exports = { redactReport };
