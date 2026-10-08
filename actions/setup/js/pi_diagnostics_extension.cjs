// @ts-check
"use strict";

const { parseDiagnosticLanguagesJSON } = require("./command_diagnostics.cjs");
const { diagnoseToolResult, diagnosticSecrets } = require("./command_diagnostics_tools.cjs");
const { renderDiagnostics } = require("./command_diagnostics_render.cjs");

/** @param {any} pi */
function piDiagnosticsExtension(pi) {
  const languages = parseDiagnosticLanguagesJSON(process.env.GH_AW_DIAGNOSTICS);
  if (languages.length === 0) return;
  const root = process.env.GITHUB_WORKSPACE || process.env.GH_AW_ENGINE_CWD || process.cwd();
  const secrets = diagnosticSecrets(process.env);
  pi.on("tool_result", (event, ctx) => {
    const report = diagnoseToolResult(event.toolName, event.input, event.content, languages, { root, cwd: ctx.cwd, secrets });
    if (!report) return;
    return {
      content: [...event.content, { type: "text", text: renderDiagnostics(report) }],
      details: { ...event.details, diagnostics: report },
      ...(event.structuredContent !== undefined ? { structuredContent: event.structuredContent } : {}),
      // No isError override: the execution outcome belongs to Pi.
    };
  });
}

module.exports = piDiagnosticsExtension;
