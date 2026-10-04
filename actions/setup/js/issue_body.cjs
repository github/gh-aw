// @ts-check
/// <reference types="@actions/github-script" />

const { generateFooterWithMessages, getBodyFooterMessage, getDetectionCautionAlert } = require("./messages_footer.cjs");
const { getBodyHeader, getDisclosureHeader } = require("./messages_header.cjs");
const { generateWorkflowIdMarker, generateWorkflowCallIdMarker, generateCloseKeyMarker } = require("./generate_footer.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { addExpirationToFooter } = require("./ephemerals.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

/**
 * Decorate a trusted issue body with the standard safe-output attribution.
 * Content sanitization belongs to the caller so custom metadata is preserved.
 * @param {string} body
 * @param {{repo: {owner: string, repo: string}, footer?: boolean, bodyFooter?: string, expiresHours?: number, closeOlderKey?: string}} options
 * @returns {string}
 */
function formatIssueBody(body, { repo, footer = true, bodyFooter, expiresHours = 0, closeOlderKey }) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME ?? "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
  const workflowId = process.env.GH_AW_WORKFLOW_ID ?? "";
  const callerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID ?? "";
  const runUrl = buildWorkflowRunUrl(context, context.repo);
  const bodyLines = body.split("\n");

  const bodyHeader = getBodyHeader({ workflowName, runUrl });
  if (bodyHeader) bodyLines.unshift(...bodyHeader.split("\n"), "");
  const disclosureHeader = getDisclosureHeader({ workflowName, runUrl });
  if (disclosureHeader) bodyLines.unshift(...disclosureHeader.split("\n"), "");
  const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);
  if (detectionCaution) bodyLines.unshift(...detectionCaution.split("\n"), "");

  const trackerIDComment = getTrackerID("markdown");
  if (trackerIDComment) bodyLines.push(trackerIDComment);

  if (footer) {
    const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
    const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
    const triggeringDiscussionNumber = context.payload?.discussion?.number;
    const historyUrl = generateHistoryUrl({
      owner: repo.owner,
      repo: repo.repo,
      itemType: "issue",
      workflowCallId: callerWorkflowId,
      workflowId,
      serverUrl: context.serverUrl,
    });
    const generatedFooter = addExpirationToFooter(
      generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, triggeringDiscussionNumber, historyUrl, { skipDetectionCaution: true }).trimEnd(),
      expiresHours,
      "Issue"
    );
    bodyLines.push("", generatedFooter);
  }

  const configuredFooter = getBodyFooterMessage(bodyFooter, { workflowName, runUrl });
  if (configuredFooter) bodyLines.push("", configuredFooter.trimEnd());
  if (workflowId) bodyLines.push("", generateWorkflowIdMarker(workflowId));
  if (callerWorkflowId) bodyLines.push(generateWorkflowCallIdMarker(callerWorkflowId));
  if (closeOlderKey) bodyLines.push(generateCloseKeyMarker(closeOlderKey));

  bodyLines.push("");
  return bodyLines.join("\n").trim();
}

module.exports = { formatIssueBody };
