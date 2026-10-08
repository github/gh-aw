// @ts-check
"use strict";

const { diagnoseToolResult } = require("./command_diagnostics_tools.cjs");
const { renderDiagnostics } = require("./command_diagnostics_render.cjs");

/**
 * Failed SDK tools expose only their failure message and accept additionalContext;
 * replacing or reclassifying that failure is neither necessary nor supported.
 * @param {import("./types/command_diagnostics").DiagnosticLanguage[]} languages
 * @param {{root?: string, secrets?: string[]}} context
 * @returns {Pick<import("@github/copilot-sdk").SessionHooks, "onPostToolUse" | "onPostToolUseFailure">}
 */
function buildCopilotDiagnosticHooks(languages, context) {
  return {
    onPostToolUse(input) {
      const report = diagnoseToolResult(input.toolName, input.toolArgs, input.toolResult, languages, { ...context, cwd: input.workingDirectory });
      if (report) return { additionalContext: renderDiagnostics(report) };
      return undefined;
    },
    onPostToolUseFailure(input) {
      const report = diagnoseToolResult(input.toolName, input.toolArgs, input.error, languages, { ...context, cwd: input.workingDirectory });
      if (report) return { additionalContext: renderDiagnostics(report) };
      return undefined;
    },
  };
}

module.exports = { buildCopilotDiagnosticHooks };
