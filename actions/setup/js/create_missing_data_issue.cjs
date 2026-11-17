// @ts-check
/// <reference types="@actions/github-script" />

const { getIssueLabel } = require("./issue_label.cjs");
const { buildMissingIssueHandler } = require("./missing_issue_helpers.cjs");
const { getPromptPath } = require("./messages_core.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_missing_data_issue";

/**
 * Main handler factory for create_missing_data_issue
 * Returns a message handler function that processes individual create_missing_data_issue messages
 * @type {HandlerFactoryFunction}
 */
const main = buildMissingIssueHandler({
  handlerType: HANDLER_TYPE,
  defaultTitlePrefix: "[missing data]",
  defaultLabels: [getIssueLabel()],
  itemsField: "missing_data",
  templatePath: getPromptPath("missing_data_issue.md"),
  templateListKey: "missing_data_list",
  buildCommentHeader: runUrl => [`## Missing Data Reported`, ``, `The following data was reported as missing during [workflow run](${runUrl}):`, ``],
  renderCommentItem: (item, index) => {
    const lines = [`### ${index + 1}. **${item.data_type}**`, `**Reason:** ${item.reason}`];
    if (item.context) lines.push(`**Context:** ${item.context}`);
    if (item.alternatives) lines.push(`**Alternatives:** ${item.alternatives}`);
    lines.push(``);
    return lines;
  },
  renderIssueItem: (item, index) => {
    const lines = [`#### ${index + 1}. **${item.data_type}**`, `**Reason:** ${item.reason}`];
    if (item.context) lines.push(`**Context:** ${item.context}`);
    if (item.alternatives) lines.push(`**Alternatives:** ${item.alternatives}`);
    lines.push(`**Reported at:** ${item.timestamp}`, ``);
    return lines;
  },
});

module.exports = { main };
