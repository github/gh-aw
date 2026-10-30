// @ts-check
/// <reference types="@actions/github-script" />

const { buildMissingIssueHandler } = require("./missing_issue_helpers.cjs");
const { getPromptPath } = require("./messages_core.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_report_incomplete_issue";
const DEFAULT_INCOMPLETE_SIGNAL_REASON = "incomplete_signal_not_provided";

/**
 * Main handler factory for create_report_incomplete_issue
 * Returns a message handler function that creates or updates a tracking issue
 * when the agent emitted report_incomplete signals, aggregating all reasons
 * into a single issue comment per workflow run.
 * @type {HandlerFactoryFunction}
 */
const main = buildMissingIssueHandler({
  handlerType: HANDLER_TYPE,
  defaultTitlePrefix: "[incomplete]",
  defaultLabels: ["agentic-workflows"],
  itemsField: "incomplete_signals",
  fallbackItems: () => [
    {
      reason: DEFAULT_INCOMPLETE_SIGNAL_REASON,
      timestamp: new Date().toISOString(),
    },
  ],
  templatePath: getPromptPath("missing_tool_issue.md"),
  templateListKey: "missing_tools_list",
  buildCommentHeader: runUrl => [`## Incomplete Run Reported`, ``, `The agent reported that the task could not be completed during [workflow run](${runUrl}):`, ``],
  renderCommentItem: (_item, index) => [`### ${index + 1}. Incomplete signal`, "**Task completion:** unconfirmed", ""],
  renderIssueItem: (_item, index) => [`#### ${index + 1}. Incomplete signal`, "**Task completion:** unconfirmed", ""],
});

module.exports = { main };
