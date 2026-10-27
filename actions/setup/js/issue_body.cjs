// @ts-check
/// <reference types="@actions/github-script" />

const { buildGeneratedFooterParts, getDetectionCautionBlocks } = require("./messages_footer.cjs");
const { getBodyHeaderTemplate, getDisclosureHeaderTemplate } = require("./messages_header.cjs");
const { generateWorkflowIdMarker, generateWorkflowCallIdMarker, generateCloseKeyMarker } = require("./generate_footer.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { addExpirationToFooter } = require("./ephemerals.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { appendExecutionProvenance, stripExecutionProvenance } = require("./execution_provenance.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { normalizeReportText } = require("./report_ast.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { redactReport } = require("./report_redaction.cjs");

// JSON safe-output input cannot forge framework-authored preamble nodes.
const COMMENT_PREAMBLE = Symbol("gh-aw-comment-preamble");
const ISSUE_BODY_SUFFIX = Symbol("gh-aw-issue-body-suffix");
/**
 * @typedef {{repo: {owner: string, repo: string}, footer?: boolean, bodyFooter?: string, expiresHours?: number, closeOlderKey?: string, allowedMentions?: string[], redact?: import("./report_redaction.cjs").ReportRedactionPolicy, itemType?: "issue" | "pull_request" | "comment" | "discussion_comment", runUrl?: string, workflowIdMarker?: boolean}} IssueBodyOptions
 */

/**
 * Decorate a trusted issue body with the standard safe-output attribution.
 * Content sanitization belongs to the caller so custom metadata is preserved.
 * @param {string | import("./report_ast.cjs").ReportBlock[]} body
 * @param {IssueBodyOptions} options
 * @returns {string}
 */
function formatIssueBody(body, options) {
  return renderSizedReport(buildIssueBodyReport("Issue", body, options)).body.trimEnd();
}

/**
 * Assemble content, attribution, and provenance without rendering intermediate trees.
 * @param {string} title
 * @param {string | import("./report_ast.cjs").ReportBlock[]} body
 * @param {IssueBodyOptions} options
 * @returns {import("./report_ast.cjs").Report}
 */
function buildIssueBodyReport(
  title,
  body,
  { repo, footer = true, bodyFooter, expiresHours = 0, closeOlderKey, allowedMentions = [], redact, itemType = "issue", runUrl = buildWorkflowRunUrl(context, context.repo), workflowIdMarker = true }
) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME ?? "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
  const workflowId = process.env.GH_AW_WORKFLOW_ID ?? "";
  const callerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID ?? "";
  const bodyHeader = getBodyHeaderTemplate();
  const disclosureHeader = getDisclosureHeaderTemplate();
  const headerContext = { workflow_name: workflowName, workflowName, run_url: runUrl, runUrl };
  const detectionCaution = getDetectionCautionBlocks(workflowName, runUrl);
  const trackerIDComment = getTrackerID("markdown");
  const bodyNodes = typeof body === "string" ? parseReportMarkdown(stripExecutionProvenance(body), { minimumHeadingLevel: 2, allowMetadata: true, allowedMentions, agentTainted: true }) : body;
  /** @type {(import("./messages_footer.cjs").FooterPart | {type: "body_slot", children: import("./report_ast.cjs").ReportBlock[]})[]} */
  const parts = [
    { type: "body_slot", children: detectionCaution },
    "\n\n",
    { type: "template", template: [disclosureHeader, bodyHeader].filter(Boolean).join("\n\n"), context: headerContext },
    "\n\n",
    { type: "body_slot", children: bodyNodes },
    "\n\n",
    trackerIDComment,
  ];

  if (footer) {
    const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
    const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
    const triggeringDiscussionNumber = context.payload?.discussion?.number;
    const historyUrl = generateHistoryUrl({
      owner: repo.owner,
      repo: repo.repo,
      itemType,
      workflowCallId: callerWorkflowId,
      workflowId,
      serverUrl: context.serverUrl,
    });
    const footerParts = buildGeneratedFooterParts(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, { skipDetectionCaution: true });
    // Expiration belongs before the final metadata marker, even with Claim nodes.
    const suffix = footerParts.pop();
    if (typeof suffix !== "string") throw new TypeError("Generated footer must end with Markdown metadata");
    footerParts.push(addExpirationToFooter(suffix.trimEnd(), expiresHours, itemType === "pull_request" ? "Pull Request" : "Issue"));
    parts.push("\n\n", ...footerParts);
    if (itemType === "pull_request" && expiresHours > 0) parts.push("\n\n", { type: "body_slot", children: [{ type: "xml_marker", name: "gh-aw-expires-type", value: "pull-request", agentTainted: false }] });
  }

  if (bodyFooter) parts.push("\n\n", { type: "template", template: bodyFooter, context: headerContext });
  if (workflowId && workflowIdMarker) parts.push("\n\n", generateWorkflowIdMarker(workflowId));
  if (callerWorkflowId) parts.push("\n\n", generateWorkflowCallIdMarker(callerWorkflowId));
  if (closeOlderKey) parts.push("\n\n", generateCloseKeyMarker(closeOlderKey));

  const literalMarkdown = normalizeReportText(stripExecutionProvenance(JSON.stringify(bodyNodes) + parts.map(part => (typeof part === "string" ? part : part.type === "template" ? part.template : "")).join("")));
  /** @type {import("./report_markdown.cjs").ReportContext} */
  const templateContext = {};
  /** @type {Record<string, import("./report_ast.cjs").ReportInline[]>} */
  const inlineSlots = {};
  /** @type {Record<string, import("./report_ast.cjs").ReportBlock[]>} */
  const blockSlots = {};
  const markdown = parts
    .map(part => {
      if (typeof part === "string") return part;
      if (part.type === "template") {
        return part.template.replace(/\{(\w+)\}/g, (field, name) => {
          let key = `gh_aw_issue_template_${Object.keys(templateContext).length}`;
          while (literalMarkdown.includes(`{${key}}`) || Object.hasOwn(blockSlots, key)) key += "_";
          templateContext[key] = Object.hasOwn(part.context, name) && part.context[name] !== undefined ? part.context[name] : field;
          return `{${key}}`;
        });
      }
      if (part.type !== "quote_line") {
        let key = part.type === "body_slot" ? "gh_aw_issue_body" : `gh_aw_issue_diagnostics_${Object.keys(blockSlots).length}`;
        while (literalMarkdown.includes(`{${key}}`) || Object.hasOwn(blockSlots, key)) key += "_";
        blockSlots[key] = part.type === "diagnostic_sections" ? part.buildChildren().map(node => ({ ...node, sizeProtected: true })) : part.children;
        return part.type === "body_slot" ? `{${key}}` : `\n\n{${key}}\n\n`;
      }
      let key = `gh_aw_issue_footer_${Object.keys(inlineSlots).length}`;
      while (literalMarkdown.includes(`{${key}}`)) key += "_";
      inlineSlots[key] = part.children.map(node => ({ ...node, agentTainted: false }));
      return `> {${key}}`;
    })
    .join("");
  // Parse wrappers and adjacent quote lines together, while Claim values remain typed.
  const consumedSlots = new Set();
  const children = parseReportMarkdown(stripExecutionProvenance(markdown), {
    context: templateContext,
    inlineSlots,
    blockSlots,
    onBlockSlot: key => consumedSlots.add(key),
    minimumHeadingLevel: 2,
    allowMetadata: true,
    allowedMentions,
    agentTainted: false,
  });
  if (Object.keys(blockSlots).some(key => !consumedSlots.has(key))) throw new TypeError("Issue body wrappers must preserve report content slots; check bodyHeader and bodyFooter for code fences");

  children.push(...parseReportMarkdown(appendExecutionProvenance("", workflowName, runUrl), { minimumHeadingLevel: 2, allowMetadata: true, agentTainted: false }));
  /** @type {import("./report_ast.cjs").Report} */
  const report = { type: "report", title, agentTainted: true, children };
  return redact ? redactReport(report, redact) : report;
}

module.exports = { formatIssueBody, buildIssueBodyReport, COMMENT_PREAMBLE, ISSUE_BODY_SUFFIX };
