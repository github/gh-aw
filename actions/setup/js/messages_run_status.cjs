// @ts-check

/**
 * Run Status Message Module
 *
 * This module provides run status messages (started, success, failure)
 * for workflow execution notifications.
 */

const { getMessages, renderTemplate, renderTemplateFromFile, toSnakeCase, getPromptPath } = require("./messages_core.cjs");
const { getDetectionReasonText, getThreatDetectedMarkerTemplate, getThreatEngineErrorMarkerTemplate, normalizeThreatKinds, isToolingFailureReason } = require("./threat_detection_warning.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const PULL_REQUEST_CREATED = "Pull request created: [#{item_number}]({item_url})";
const ISSUE_CREATED = "Issue created: [#{item_number}]({item_url})";
const COMMIT_PUSHED = "Commit pushed: [`{short_sha}`]({commit_url})";

/**
 * Renders a message using a custom template from config or a default template.
 * @param {string} messageKey - Key in the messages config (e.g., "runStarted")
 * @param {string} defaultTemplate - Default template string with {placeholder} syntax
 * @param {Object} ctx - Context object for template substitution
 * @returns {string} Rendered message
 */
function renderConfiguredMessage(messageKey, defaultTemplate, ctx) {
  const messages = getMessages();
  const template = messages?.[messageKey] ?? defaultTemplate;
  return renderTemplate(template, toSnakeCase(ctx));
}

/**
 * @typedef {Object} RunStartedContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 * @property {string} eventType - Event type description (e.g., "issue", "pull request", "discussion")
 * @property {string} [emoji] - Optional workflow emoji override
 */

/**
 * Get the run-started message, using custom template if configured.
 * @param {RunStartedContext} ctx - Context for run-started message generation
 * @returns {string} Run-started message
 */
function getRunStartedMessage(ctx) {
  const normalizedEmoji = typeof ctx?.emoji === "string" && ctx.emoji.trim() ? ctx.emoji.trim() : "🚀";
  return renderConfiguredMessage("runStarted", "{emoji} [{workflow_name}]({run_url}) has started processing this {event_type}", { ...ctx, emoji: normalizedEmoji });
}

/**
 * @typedef {Object} RunSuccessContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 */

/**
 * Get the run-success message, using custom template if configured.
 * @param {RunSuccessContext} ctx - Context for run-success message generation
 * @returns {string} Run-success message
 */
function getRunSuccessMessage(ctx) {
  return renderConfiguredMessage("runSuccess", "✅ [{workflow_name}]({run_url}) completed successfully!", ctx);
}

/**
 * @typedef {Object} RunFailureContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 * @property {string} status - Status text (e.g., "failed", "was cancelled", "timed out")
 */

/**
 * Get the run-failure message, using custom template if configured.
 * @param {RunFailureContext} ctx - Context for run-failure message generation
 * @returns {string} Run-failure message
 */
function getRunFailureMessage(ctx) {
  return renderConfiguredMessage("runFailure", "❌ [{workflow_name}]({run_url}) {status}. Please review the logs for details.", ctx);
}

/**
 * @typedef {Object} DetectionFailureContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 */

/**
 * Get the detection-failure message, using custom template if configured.
 * @param {DetectionFailureContext} ctx - Context for detection-failure message generation
 * @returns {string} Detection-failure message
 */
function getDetectionFailureMessage(ctx) {
  return renderConfiguredMessage("detectionFailure", "⚠️ Security scanning failed for [{workflow_name}]({run_url}). Review the logs for details.", ctx);
}

/**
 * @typedef {Object} PullRequestCreatedContext
 * @property {number} itemNumber - PR number
 * @property {string} itemUrl - URL of the pull request
 */

/**
 * Get the pull-request-created message, using custom template if configured.
 * @param {PullRequestCreatedContext} ctx - Context for message generation
 * @returns {string} Pull-request-created message
 */
function getPullRequestCreatedMessage(ctx) {
  return renderConfiguredMessage("pullRequestCreated", PULL_REQUEST_CREATED, ctx);
}

/**
 * @typedef {Object} IssueCreatedContext
 * @property {number} itemNumber - Issue number
 * @property {string} itemUrl - URL of the issue
 */

/**
 * Get the issue-created message, using custom template if configured.
 * @param {IssueCreatedContext} ctx - Context for message generation
 * @returns {string} Issue-created message
 */
function getIssueCreatedMessage(ctx) {
  return renderConfiguredMessage("issueCreated", ISSUE_CREATED, ctx);
}

/**
 * @typedef {Object} CommitPushedContext
 * @property {string} commitSha - Full commit SHA
 * @property {string} shortSha - Short (7-char) commit SHA
 * @property {string} commitUrl - URL of the commit
 */

/**
 * Get the commit-pushed message, using custom template if configured.
 * @param {CommitPushedContext} ctx - Context for message generation
 * @returns {string} Commit-pushed message
 */
function getCommitPushedMessage(ctx) {
  return renderConfiguredMessage("commitPushed", COMMIT_PUSHED, ctx);
}

/**
 * @param {"issue" | "pull_request" | "commit"} kind
 * @param {PullRequestCreatedContext | CommitPushedContext} ctx
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function buildCreatedMessageBlocks(kind, ctx) {
  const key = kind === "issue" ? "issueCreated" : kind === "commit" ? "commitPushed" : "pullRequestCreated";
  const fallback = kind === "issue" ? ISSUE_CREATED : kind === "commit" ? COMMIT_PUSHED : PULL_REQUEST_CREATED;
  return parseReportMarkdown(getMessages()?.[key] ?? fallback, { context: toSnakeCase(ctx), agentTainted: false });
}

/**
 * @typedef {Object} DetectionWarningContext
 * @property {string} workflowName - Name of the workflow
 * @property {string} runUrl - URL of the workflow run
 * @property {string} reason - Categorized reason for the warning (e.g. "threat_detected", "agent_failure", "parse_error")
 */

/**
 * Get the detection-warning message with progressive disclosure via details/summary.
 * Used when continue-on-error is true (default) instead of false.
 *
 * When the reason indicates a tooling failure (agent_failure or parse_error) the
 * message uses a [!WARNING] admonition so reviewers can distinguish "detection
 * engine crashed" from "detection engine found something". Actual threat findings
 * (threat_detected) keep the [!CAUTION] admonition.
 *
 * @param {DetectionWarningContext} ctx - Context for detection-warning message generation
 * @returns {string} Detection-warning message with admonition
 */
function getDetectionWarningMessage(ctx) {
  const reasonText = getDetectionReasonText(ctx.reason);
  const isEngineError = isToolingFailureReason(ctx.reason);
  const templateFile = isEngineError ? "threat_detection_engine_error.md" : "threat_detection_caution.md";
  const messageKey = isEngineError ? "detectionEngineError" : "detectionWarning";
  const markerTemplate = isEngineError ? getThreatEngineErrorMarkerTemplate() : getThreatDetectedMarkerTemplate();
  const messages = getMessages();
  const configTemplate = messages?.[messageKey];
  if (configTemplate) {
    return renderTemplate(configTemplate, toSnakeCase({ ...ctx, reasonText, threat_detected_marker: markerTemplate, threatKinds: normalizeThreatKinds(ctx.reason) }));
  }
  return renderTemplateFromFile(getPromptPath(templateFile), toSnakeCase({ ...ctx, reasonText, threat_detected_marker: markerTemplate, threatKinds: normalizeThreatKinds(ctx.reason) })).trimEnd();
}

/**
 * @param {DetectionWarningContext} ctx
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function getDetectionWarningBlocks(ctx) {
  const isEngineError = isToolingFailureReason(ctx.reason);
  const key = isEngineError ? "detectionEngineError" : "detectionWarning";
  const template = getMessages()?.[key] || renderTemplateFromFile(getPromptPath(isEngineError ? "threat_detection_engine_error.md" : "threat_detection_caution.md"), {});
  return parseReportMarkdown(template, {
    agentTainted: false,
    allowFlagMarkers: true,
    context: toSnakeCase({
      ...ctx,
      reasonText: getDetectionReasonText(ctx.reason),
      threatKinds: normalizeThreatKinds(ctx.reason),
      threat_detected_marker: [{ type: "marker", name: isEngineError ? "gh-aw-threat-engine-error" : "gh-aw-threat-detected", agentTainted: false }],
    }),
  });
}

module.exports = {
  getRunStartedMessage,
  getRunSuccessMessage,
  getRunFailureMessage,
  getDetectionFailureMessage,
  getDetectionWarningMessage,
  getDetectionWarningBlocks,
  getPullRequestCreatedMessage,
  getIssueCreatedMessage,
  getCommitPushedMessage,
  buildCreatedMessageBlocks,
};
