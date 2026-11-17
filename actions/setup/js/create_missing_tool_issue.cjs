// @ts-check
/// <reference types="@actions/github-script" />

const { getIssueLabel } = require("./issue_label.cjs");
const { buildMissingIssueHandler } = require("./missing_issue_helpers.cjs");
const { getPromptPath } = require("./messages_core.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_missing_tool_issue";

/**
 * Main handler factory for create_missing_tool_issue
 * Returns a message handler function that processes individual create_missing_tool_issue messages
 * @type {HandlerFactoryFunction}
 */
const main = buildMissingIssueHandler({
  handlerType: HANDLER_TYPE,
  defaultTitlePrefix: "[missing tool]",
  defaultLabels: [getIssueLabel()],
  itemsField: "missing_tools",
  templatePath: getPromptPath("missing_tool_issue.md"),
  templateListKey: "missing_tools_list",
  buildCommentHeader: runUrl => [`## Missing Tools Reported`, ``, `The following tools were reported as missing during [workflow run](${runUrl}):`, ``],
  renderCommentItem: (tool, index) => {
    const lines = [`### ${index + 1}. \`${tool.tool}\``, `**Reason:** ${tool.reason}`];
    if (tool.alternatives) lines.push(`**Alternatives:** ${tool.alternatives}`);
    lines.push(``);
    return lines;
  },
  renderIssueItem: (tool, index) => {
    const lines = [`#### ${index + 1}. \`${tool.tool}\``, `**Reason:** ${tool.reason}`];
    if (tool.alternatives) lines.push(`**Alternatives:** ${tool.alternatives}`);
    lines.push(`**Reported at:** ${tool.timestamp}`, ``);
    return lines;
  },
});

module.exports = { main };
