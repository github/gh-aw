// @ts-check

const { redactDiagnosticText } = require("./diagnostic_sanitization.cjs");
const { renderReport } = require("./report_ast.cjs");
const { generateStagedPreview } = require("./staged_preview.cjs");

/**
 * Preview the complete request, including structured advisory fields, without
 * interpreting agent-controlled values as step-summary Markdown.
 * @param {string} title
 * @param {string} description
 * @param {Record<string, any>} request
 * @returns {Promise<void>}
 */
async function previewSecurityAdvisory(title, description, request) {
  /** @type {import("./report_ast.cjs").Report} */
  const report = {
    type: "report",
    title,
    children: [{ type: "code_block", language: "json", value: JSON.stringify(request, (_key, value) => (typeof value === "string" ? redactDiagnosticText(value) : value), 2), agentTainted: true }],
  };
  await generateStagedPreview({
    title,
    description,
    items: [report],
    renderItem: item => renderReport(item).body,
  });
}

module.exports = { previewSecurityAdvisory };
