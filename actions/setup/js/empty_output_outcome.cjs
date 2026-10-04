// @ts-check

const fs = require("fs");
const path = require("path");
const { collectUnifiedSession } = require("./unified_session.cjs");
const { agentErrorDiagnosticText } = require("./agent_execution.cjs");
const { extractDeniedCommands } = require("./permission_denied_helpers.cjs");
const { extractShellCommandFromToolData } = require("./tool_call_details.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { collectAddMaskedValues, redactMaskedValues } = require("./add_mask_redaction.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");

/**
 * Silence is not evidence of an intentional noop. Preserve runtime diagnostics
 * in a first-class incomplete signal when the agent emitted no valid outputs.
 * @param {string[]} errors
 * @param {string} [rootDir]
 * @returns {{type: string, reason: string, details?: string}}
 */
function buildEmptyOutputOutcome(errors, rootDir = "/tmp/gh-aw") {
  const diagnostics = new Set(errors);
  const starts = new Map();
  let events = [];
  let maskedValues = [];
  try {
    ({ events, maskedValues } = collectUnifiedSession({ rootDir, warn: () => {} }));
  } catch {
    diagnostics.add("Runtime diagnostics could not be collected.");
  }
  let stdio = "";
  try {
    const stdioPath = path.join(rootDir, "agent-stdio.log");
    if (fs.lstatSync(stdioPath).isFile()) stdio = fs.readFileSync(stdioPath, "utf8");
  } catch {
    stdio = "";
  }
  maskedValues.push(...collectAddMaskedValues(stdio));
  const secrets = collectArtifactSecretValues();
  const redact = value => redactMaskedValues(String(redactManifestValue(value, secrets)), maskedValues);
  const redactJson = value => JSON.stringify(value, (_key, nested) => (typeof nested === "string" ? redact(nested) : nested));
  for (const event of events) {
    if (event.provenance.component !== "agent") continue;
    /** @type {any} */
    const data = event.data;
    const key = `${event.provenance.path}:${event.session_id || ""}:${data.toolCallId}`;
    if (event.type === "tool.execution_start") {
      starts.set(key, data);
    } else if (event.type === "tool.execution_complete" && data.success === false) {
      const start = starts.get(key);
      const toolName = data.toolName || start?.toolName || "unknown tool";
      const tool = [data.mcpServerName || start?.mcpServerName, toolName].filter(Boolean).join(".");
      const errorValue = data.error || data.output || data.result || "Tool execution failed";
      const error = typeof errorValue === "string" ? redact(errorValue) : redactJson(errorValue);
      const command = /^(bash|shell)$/i.test(toolName) ? extractShellCommandFromToolData(start) : "";
      diagnostics.add(`${tool}${command ? `: ${command}` : ""}: ${error}`);
    } else if (event.type === "guard.tool_denials_exceeded" && typeof data.reason === "string") {
      diagnostics.add(data.reason);
    } else if (event.type === "session.result" && Array.isArray(data.permissionDenials)) {
      for (const denial of data.permissionDenials) {
        const command = extractShellCommandFromToolData({ input: denial.tool_input });
        diagnostics.add(`Permission denied: ${denial.tool_name || "unknown tool"}${command ? `: ${command}` : ""}`);
      }
    }
  }
  const safeStdio = stdio
    .split("\n")
    .map(line => {
      try {
        return redactJson(JSON.parse(line));
      } catch {
        return redact(line);
      }
    })
    .join("\n");
  const attributedDiagnostics = agentErrorDiagnosticText(safeStdio);
  for (const command of extractDeniedCommands(attributedDiagnostics)) diagnostics.add(`Permission denied: ${command}`);
  if (attributedDiagnostics) diagnostics.add(attributedDiagnostics);
  const details = [...diagnostics].slice(0, 20).join("\n");
  const sanitized = sanitizeContent(redact(details)).slice(0, 8000);
  return {
    type: "report_incomplete",
    reason: "Agent finished without emitting any valid safe outputs; task completion could not be confirmed.",
    ...(sanitized ? { details: sanitized } : {}),
  };
}

module.exports = { buildEmptyOutputOutcome };
