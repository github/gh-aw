// @ts-check
/// <reference types="@actions/github-script" />

const { generateFooterWithMessages, getDetectionCautionAlert, generateXMLMarker } = require("./messages_footer.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");

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
 * Ensure a blank line follows every closing `</details>` tag so that markdown
 * placed directly after a collapsible block is still rendered by GitHub.
 * Closing tags inside fenced code blocks are left untouched, as are tags that
 * are part of another construct (for example blockquoted alerts) or that share
 * their line with other content.
 * @param {string} markdown
 * @returns {string}
 */
function ensureBlankLineAfterDetails(markdown) {
  if (!markdown || typeof markdown !== "string" || !markdown.includes("</details>")) {
    return markdown;
  }

  const lines = markdown.split("\n");
  /** @type {string[]} */
  const result = [];
  let fenceMarker = "";

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fenceMatch = line.match(/^ {0,3}(`{3,}|~{3,})/);
    if (fenceMatch) {
      const marker = fenceMatch[1][0];
      if (!fenceMarker) {
        fenceMarker = marker;
      } else if (marker === fenceMarker) {
        fenceMarker = "";
      }
    }

    result.push(line);

    if (fenceMarker || !/^\s*<\/details>\s*$/.test(line)) {
      continue;
    }

    const nextLine = lines[i + 1];
    if (nextLine === undefined || nextLine.trim() === "") {
      continue;
    }

    result.push("");
  }

  return result.join("\n");
}

module.exports = {
  buildGeneratedFooter,
  assembleMarkdownBodyParts,
  ensureBlankLineAfterDetails,
};
