// @ts-check
/// <reference types="@actions/github-script" />

const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { getBodyFooterBlocks } = require("./messages_footer.cjs");
const { expandAllowedAliases } = require("./sanitize_content.cjs");

/**
 * Append a configured deterministic body footer using the current workflow context.
 * When `maxLength` is provided, fit agent content to the remaining budget without
 * shortening or omitting the configured footer.
 * Pass the same resolved mention allowlist used to sanitize the body.
 * @param {string} body
 * @param {string|undefined} template
 * @param {{workflowRepo?: any, maxLength?: number, allowedMentions?: string[]}} [options]
 * @returns {string}
 */
function appendConfiguredBodyFooter(body, template, options = {}) {
  const { workflowRepo, maxLength, allowedMentions = [] } = options || {};
  if (!template && maxLength === undefined) return body;
  const report = appendConfiguredReportFooter(
    { type: "report", title: "Comment", children: parseReportMarkdown(body, { minimumHeadingLevel: 2, allowMetadata: true, agentTainted: true, allowedMentions: expandAllowedAliases(allowedMentions) }) },
    template,
    { workflowRepo }
  );
  return renderSizedReport(report, maxLength).body;
}

/**
 * Append trusted policy nodes without rendering or reparsing the document.
 * @param {import("./report_ast.cjs").Report} report
 * @param {string | undefined} template
 * @param {{workflowRepo?: any}} [options]
 * @returns {import("./report_ast.cjs").Report}
 */
function appendConfiguredReportFooter(report, template, { workflowRepo } = {}) {
  if (!template) return report;
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const runUrl = buildWorkflowRunUrl(context, workflowRepo || context.repo);
  return { ...report, children: [...report.children, ...getBodyFooterBlocks(template, { workflowName, runUrl })] };
}

module.exports = { appendConfiguredBodyFooter, appendConfiguredReportFooter };
