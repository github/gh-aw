// @ts-check

const { parseReportMarkdown } = require("./report_markdown.cjs");
const fs = require("node:fs");
const { getPromptPath } = require("./messages_core.cjs");

/** @typedef {import("./report_ast.cjs").Report} Report */
/** @typedef {import("./report_ast.cjs").ReportInline} ReportInline */
/** @typedef {import("./report_ast.cjs").ReportBlock} ReportBlock */

/**
 * Keep legacy diagnostic templates at an explicit import boundary. Scalar metadata
 * stays literal, and diagnostic headings cannot become top-level report sections.
 * @param {string} template
 * @param {string} title
 * @param {Record<string, string | ReportBlock[]>} values
 * @returns {Report}
 */
function buildFailureReport(template, title, values) {
  /** @type {import("./report_markdown.cjs").ReportContext} */
  const context = {};
  for (const [key, value] of Object.entries(values)) {
    context[key] = typeof value === "string" && key.endsWith("_context") ? parseReportMarkdown(value, { minimumHeadingLevel: 4, agentTainted: true }) : value;
  }
  /** @param {string} key */
  const scalar = key => (typeof values[key] === "string" ? values[key] : "");

  /** @type {ReportInline[]} */
  const links = [];
  const addLink = (label, url) => {
    if (links.length) links.push({ type: "text", value: " · ", agentTainted: false });
    links.push({ type: "link", url, children: [{ type: "text", value: label, agentTainted: false }], agentTainted: false });
  };
  if (scalar("run_url")) addLink(/^\d+$/.test(scalar("run_id")) ? `View run #${scalar("run_id")}` : "View run", scalar("run_url"));
  else links.push({ type: "text", value: "Run link unavailable: run URL was not recorded.", agentTainted: false });
  if (scalar("workflow_source_url") && scalar("workflow_source_url") !== "#") addLink("Workflow source", scalar("workflow_source_url"));
  if (scalar("history_url")) addLink("Previous failures", scalar("history_url"));
  context.run_links_context = [{ type: "paragraph", children: links, agentTainted: false }];
  context.pull_request_context = Array.isArray(values.pull_request_info) ? values.pull_request_info : parseReportMarkdown(scalar("pull_request_info"), { agentTainted: false });

  const evidence = [context.report_incomplete_context, context.failure_diagnostics_context].flatMap(value => (Array.isArray(value) ? value : []));
  context.runtime_evidence_context = evidence.length ? [{ type: "details", summary: "Runtime evidence", children: evidence, agentTainted: false }] : [];

  return { type: "report", title, children: parseReportMarkdown(template, { context, minimumHeadingLevel: 3, agentTainted: false }), agentTainted: false };
}

/** @param {unknown} value @returns {ReportInline} */
const text = value => ({ type: "text", value: String(value ?? ""), agentTainted: true });
/** @param {unknown} value @returns {ReportInline} */
const code = value => ({ type: "code", value: String(value ?? ""), agentTainted: true });
/** @param {ReportInline[]} children @returns {ReportInline} */
const strong = children => ({ type: "strong", children, agentTainted: false });
/** @param {ReportInline[]} children @returns {ReportBlock} */
const paragraph = children => ({ type: "paragraph", children, agentTainted: false });
/** @param {string} value @returns {ReportBlock} */
const heading = value => ({ type: "heading", level: 4, children: [text(value)], agentTainted: false });
/** @param {string} value @param {string} [language] @returns {ReportBlock} */
const codeBlock = (value, language = "text") => ({ type: "code_block", value, language, agentTainted: true });
/** @param {string | ReportInline[]} summary @param {ReportBlock[]} children @returns {ReportBlock} */
const details = (summary, children) => ({ type: "details", summary, children, agentTainted: false });
/** @param {ReportBlock[][]} items @returns {ReportBlock} */
const list = items => ({ type: "list", ordered: false, items, agentTainted: true });
/** @param {string} label @param {string} url @returns {ReportInline} */
const link = (label, url) => ({ type: "link", url, children: [text(label)], agentTainted: false });
/** @param {string} title @param {string | ReportInline[]} message @returns {ReportBlock[]} */
const warning = (title, message) => [{ type: "alert", kind: "WARNING", children: [paragraph([strong([text(title)]), text(": "), ...(typeof message === "string" ? [text(message)] : message)])], agentTainted: false }];
/** @param {string} template @param {import("./report_markdown.cjs").ReportContext} [context] @returns {ReportBlock[]} */
const fragment = (template, context = {}) => parseReportMarkdown(template, { context, minimumHeadingLevel: 4, agentTainted: false });
/** @param {string} name @param {import("./report_markdown.cjs").ReportContext} [context] @returns {ReportBlock[]} */
const prompt = (name, context = {}) => fragment(fs.readFileSync(getPromptPath(name), "utf8"), context);

module.exports = { buildFailureReport, failureNodes: { text, code, strong, paragraph, heading, codeBlock, details, list, link, warning, fragment, prompt } };
