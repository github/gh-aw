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

const EMPTY_OUTPUT_CAUSES = new Set(["engine_driver_failure", "safeoutputs_cli_error", "invalid_safe_outputs", "missing_terminal_safe_output"]);

/**
 * Silence is not evidence of an intentional noop. Preserve runtime diagnostics
 * in a first-class incomplete signal when the agent emitted no valid outputs.
 * @param {string[]} errors
 * @param {string} [rootDir]
 * @returns {{type: string, reason: string, details?: string}}
 */
function buildEmptyOutputOutcome(errors, rootDir = "/tmp/gh-aw") {
  const diagnostics = new Set(["Agent finished without emitting a terminal safe output; task completion could not be confirmed.", ...errors]);
  let reason = errors.length ? "invalid_safe_outputs" : "missing_terminal_safe_output";
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
  try {
    const auditPath = path.join(rootDir, "mcp-cli-audit/safeoutputs.jsonl");
    if (fs.lstatSync(auditPath).isFile()) {
      for (const line of fs.readFileSync(auditPath, "utf8").split("\n")) {
        try {
          const entry = JSON.parse(line);
          if (["parse_args_error", "unrecognized_args", "tool_error", "call_error"].includes(entry?.event)) {
            reason = "safeoutputs_cli_error";
            diagnostics.add("The safeoutputs CLI failed. Run `safeoutputs <tool> --help` and pass a single quoted JSON object or --key value flags.");
          }
        } catch {
          // Ignore malformed audit records without losing the remaining evidence.
        }
      }
    }
  } catch {
    // Ignore missing CLI audit evidence for workflows using MCP directly.
  }
  for (const event of events) {
    if (event.type === "agent.execution" && event.provenance.component === "execution" && event.provenance.phase === "agent" && event.data.exitCode > 0) {
      reason = "engine_driver_failure";
      diagnostics.add(`Driver exit code: ${event.data.exitCode}. The engine driver exited before a terminal safe output was recorded.`);
    }
    if (event.provenance.component !== "agent") continue;
    /** @type {any} */
    const data = event.data;
    const key = `${event.provenance.path}:${event.session_id || ""}:${data.toolCallId}`;
    if (event.type === "tool.execution_start") {
      starts.set(key, data);
    } else if (
      event.type === "tool.execution_complete" &&
      (data.success === false ||
        data.is_error === true ||
        data.isError === true ||
        data.result?.isError === true ||
        data.result?.is_error === true ||
        data.status === "failed" ||
        data.status === "error" ||
        data.error != null ||
        (typeof data.exitCode === "number" && data.exitCode !== 0))
    ) {
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
  const sanitized = sanitizeContent(redact(details), { maxLength: 8000 });
  return {
    type: "report_incomplete",
    reason,
    ...(sanitized ? { details: sanitized } : {}),
  };
}

module.exports = { buildEmptyOutputOutcome, EMPTY_OUTPUT_CAUSES };
