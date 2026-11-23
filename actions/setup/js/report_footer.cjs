// @ts-check

const { getFooterAgentFailureIssueTemplate, getAgenticWorkflowMetadata } = require("./messages_footer.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");

/**
 * @param {import("./messages_footer.cjs").AgentFailureContext} ctx
 * @param {{expiresHours: number, failureFields: {key: string, value: string}[], now?: Date}} options
 * @returns {import("./report_ast.cjs").ReportFooter}
 */
function buildFailureReportFooter(ctx, { expiresHours, failureFields, now = new Date() }) {
  if (!Number.isFinite(expiresHours) || expiresHours < 0) throw new TypeError("Report expiration hours must be nonnegative");
  const { template, context: values } = getFooterAgentFailureIssueTemplate(ctx);
  /** @type {import("./report_markdown.cjs").ReportContext} */
  const context = Object.fromEntries(Object.entries(values).map(([key, value]) => [key, value ?? ""]));
  /** @type {import("./report_markdown.cjs").ReportInlineSlot} */
  const history = {
    type: "inline_slot",
    children: ctx.historyUrl
      ? [
          { type: "text", value: " · ", agentTainted: false },
          { type: "link", url: ctx.historyUrl, children: [{ type: "text", value: "history", agentTainted: false }], agentTainted: false },
        ]
      : [],
  };
  context.history_link = history;
  context.historyLink = history;
  const [workflow, ...fields] = getAgenticWorkflowMetadata(ctx.workflowName, ctx.runUrl);
  /** @type {import("./report_ast.cjs").ReportFooter} */
  const footer = {
    type: "footer",
    agentTainted: false,
    children: parseReportMarkdown(template, { context, agentTainted: false }),
    markers: [
      { type: "xml_marker", name: "gh-aw-agentic-workflow", value: workflow.value, fields, agentTainted: false },
      { type: "xml_marker", name: "gh-aw-failure-issue", value: "true", fields: failureFields, agentTainted: false },
    ],
  };
  return expiresHours ? { ...footer, expiresAt: new Date(now.getTime() + expiresHours * 60 * 60 * 1000).toISOString() } : footer;
}

module.exports = { buildFailureReportFooter };
