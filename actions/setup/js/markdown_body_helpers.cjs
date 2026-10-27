// @ts-check
/// <reference types="@actions/github-script" />

const { generateFooterWithMessages, buildGeneratedFooterParts, buildFooterBlocks, getDetectionCautionAlert, getDetectionCautionBlocks, generateXMLMarker } = require("./messages_footer.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");

/**
 * Import footer templates while retaining Claim and diagnostic subtrees as nodes.
 * @param {Parameters<typeof buildGeneratedFooter>[0]} params
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function buildGeneratedFooterBlocks(params) {
  const { workflowName, runUrl, workflowSource = "", workflowSourceURL = "", triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl } = params;
  const parts = buildGeneratedFooterParts(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, { skipDetectionCaution: true });
  return buildFooterBlocks(parts);
}

/**
 * Build the shared generated footer body.
 * @param {Object} params
 * @param {string} params.workflowName
 * @param {string} params.runUrl
 * @param {string} [params.workflowSource]
 * @param {string} [params.workflowSourceURL]
 * @param {number} [params.triggeringIssueNumber]
 * @param {number} [params.triggeringPRNumber]
 * @param {number} [params.triggeringDiscussionNumber]
 * @param {string} [params.historyUrl]
 * @returns {string}
 */
function buildGeneratedFooter(params) {
  const { workflowName, runUrl, workflowSource = "", workflowSourceURL = "", triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl } = params;
  return generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl || undefined, { skipDetectionCaution: true }).trimEnd();
}

/**
 * Build shared markdown assembly parts: caution, footer, and fallback marker.
 * @param {Object} params
 * @param {boolean} params.includeFooter
 * @param {string} params.workflowName
 * @param {string} params.runUrl
 * @param {string} [params.workflowSource]
 * @param {string} [params.workflowSourceURL]
 * @param {number} [params.triggeringIssueNumber]
 * @param {number} [params.triggeringPRNumber]
 * @param {number} [params.triggeringDiscussionNumber]
 * @param {string} [params.historyUrl]
 * @param {string} [params.workflowId]
 * @param {"workflow-id"|"xml"|"none"} [params.markerWhenFooterDisabled]
 * @returns {{ detectionCaution: string, footer: string, noFooterMarker: string }}
 */
function assembleMarkdownBodyParts(params) {
  const { includeFooter, workflowName, runUrl, workflowSource = "", workflowSourceURL = "", triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, workflowId = "", markerWhenFooterDisabled = "none" } = params;

  const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);
  const footer = includeFooter
    ? buildGeneratedFooter({
        workflowName,
        runUrl,
        workflowSource,
        workflowSourceURL,
        triggeringIssueNumber,
        triggeringPRNumber,
        triggeringDiscussionNumber,
        historyUrl,
      })
    : "";

  let noFooterMarker = "";
  if (!includeFooter && markerWhenFooterDisabled === "workflow-id" && workflowId) {
    noFooterMarker = generateWorkflowIdMarker(workflowId);
  } else if (!includeFooter && markerWhenFooterDisabled === "xml") {
    noFooterMarker = generateXMLMarker(workflowName, runUrl);
  }

  return { detectionCaution, footer, noFooterMarker };
}

/**
 * Tree-first counterpart for issue and comment publishers.
 * @param {Parameters<typeof assembleMarkdownBodyParts>[0]} params
 */
function assembleReportBodyParts(params) {
  const { includeFooter, workflowName, runUrl, workflowId = "", markerWhenFooterDisabled = "none" } = params;
  const detectionCaution = getDetectionCautionBlocks(workflowName, runUrl);
  const footer = includeFooter ? buildGeneratedFooterBlocks(params) : [];
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const noFooterMarker = [];
  if (!includeFooter && markerWhenFooterDisabled === "workflow-id" && workflowId) {
    noFooterMarker.push({ type: "xml_marker", name: "gh-aw-workflow-id", value: workflowId, agentTainted: false });
  } else if (!includeFooter && markerWhenFooterDisabled === "xml") {
    noFooterMarker.push(...parseReportMarkdown(generateXMLMarker(workflowName, runUrl), { allowMetadata: true, agentTainted: false }));
  }
  return { detectionCaution, footer, noFooterMarker };
}

module.exports = {
  buildGeneratedFooter,
  buildGeneratedFooterBlocks,
  assembleMarkdownBodyParts,
  assembleReportBodyParts,
};
