// @ts-check
/**
 * Missing Messages Helper
 *
 * This module provides access to collected report_missing_tool, report_missing_data,
 * and report_incomplete messages for use by safe output handlers.
 */

const { generateMissingInfoSections } = require("./missing_info_formatter.cjs");

/**
 * Global storage for collected missing messages
 * @type {{missingTools: Array<any>, missingData: Array<any>, noopMessages?: Array<any>, reportIncomplete: Array<any>} | null}
 */
/** @type {any} */
let collectedMissings = null;

/**
 * Set the collected missing messages
 * @param {{missingTools: Array<any>, missingData: Array<any>, noopMessages?: Array<any>, reportIncomplete: Array<any>}} missings - Collected missing messages
 */
function setCollectedMissings(missings) {
  collectedMissings = missings;
}

/**
 * Get the collected missing messages
 * @returns {{missingTools: Array<any>, missingData: Array<any>, noopMessages?: Array<any>, reportIncomplete: Array<any>} | null} Collected missing messages
 */
function getCollectedMissings() {
  return collectedMissings;
}

/**
 * Generate missing info sections for appending to safe output footers
 * @returns {string} HTML details sections for missing tools, data, and incomplete signals
 */
function getMissingInfoSections() {
  if (!collectedMissings) {
    return "";
  }
  return generateMissingInfoSections(collectedMissings);
}

/**
 * Build diagnostic nodes without interpreting agent-authored markup.
 * @param {{missingTools?: Array<any>, missingData?: Array<any>, noopMessages?: Array<any>, reportIncomplete?: Array<any>} | null} missings
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function generateMissingInfoReportSections(missings) {
  if (!missings) return [];
  /** @param {unknown} value @returns {import("./report_ast.cjs").ReportInline} */
  const text = value => ({ type: "text", value: String(value ?? ""), agentTainted: true });
  /** @param {import("./report_ast.cjs").ReportInline[]} children @returns {import("./report_ast.cjs").ReportBlock} */
  const paragraph = children => ({ type: "paragraph", children, agentTainted: true });
  /** @param {string} label @param {unknown} value @returns {import("./report_ast.cjs").ReportBlock} */
  const detail = (label, value) => ({ type: "list", ordered: false, items: [[paragraph([{ type: "emphasis", children: [text(label)], agentTainted: false }, text(`: ${value}`)])]], agentTainted: true });
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const sections = [];
  /** @param {string} summary @param {import("./report_ast.cjs").ReportBlock[][]} items @param {import("./report_ast.cjs").ReportBlock[]} [extra] */
  const add = (summary, items, extra = []) => {
    if (items.length) sections.push({ type: "details", summary, children: [{ type: "list", ordered: false, items, agentTainted: true }, ...extra], agentTainted: false });
  };
  const tools = missings.missingTools || [];
  const alternatives = tools.filter(item => item.alternatives);
  add(
    "Missing Tools",
    tools.map(item => [paragraph([{ type: "strong", children: [text(item.tool)], agentTainted: true }, text(`: ${item.reason || ""}`)])]),
    alternatives.length ? [paragraph([text("Alternatives:")]), { type: "table", headers: [[text("Tool")], [text("Alternative")]], rows: alternatives.map(item => [[text(item.tool)], [text(item.alternatives)]]), agentTainted: true }] : []
  );
  add(
    "Missing Data",
    (missings.missingData || []).map(item => [
      paragraph([{ type: "strong", children: [text(item.data_type)], agentTainted: true }, text(`: ${item.reason || ""}`)]),
      ...(item.context ? [detail("Context", item.context)] : []),
      ...(item.alternatives ? [detail("Alternatives", item.alternatives)] : []),
    ])
  );
  add(
    "No-Op Messages",
    (missings.noopMessages || []).map(item => [paragraph([text(item.message)])])
  );
  add(
    "Incomplete Signals",
    (missings.reportIncomplete || []).map(item => [paragraph([text(item.reason)]), ...(item.details ? [detail("Details", item.details)] : [])])
  );
  return sections;
}

module.exports = {
  setCollectedMissings,
  getCollectedMissings,
  getMissingInfoSections,
  generateMissingInfoReportSections,
};
