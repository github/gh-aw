// @ts-check

const fs = require("fs");
const path = require("path");
const { collectUnifiedSession } = require("./unified_session.cjs");
const { agentErrorDiagnosticText, agentErrorSummaryText } = require("./agent_execution.cjs");
const { extractDeniedCommands } = require("./permission_denied_helpers.cjs");
const { extractShellCommandFromToolData } = require("./tool_call_details.cjs");
const { collectArtifactSecretValues } = require("./safe_output_manifest.cjs");
const { collectAddMaskedValues } = require("./add_mask_redaction.cjs");
const { redactDiagnosticText, redactAndBoundDiagnostics } = require("./diagnostic_sanitization.cjs");

// Keep cause titles and the category allow-list in sync; schema and docs list these causes too.
const EMPTY_OUTPUT_CAUSES = Object.freeze({
  engine_driver_failure: "engine driver failed before emitting a terminal safe output",
  safeoutputs_cli_error: "failed to invoke safeoutputs CLI",
  invalid_safe_outputs: "produced no valid safe outputs",
  missing_terminal_safe_output: "finished without a terminal safe output",
});
const EMPTY_OUTPUT_FAILURE_CAUSES = Object.freeze({
  engine_outage: "experienced an engine outage",
  request_rejection: "had a request rejected",
  prompt_exhaustion: "reported incomplete result",
});

const PROMPT_EXHAUSTION_ERROR_CATEGORIES = new Set(["effective_tokens_limit_exceeded", "invocation_cap_exceeded"]);
const REQUEST_REJECTION_ERROR_CATEGORIES = new Set([
  "ai_credits_rate_limit_error",
  "authentication_failed",
  "awf_api_proxy_blocking_requests",
  "capi_quota_exceeded_error",
  "http_400_response_error",
  "inference_access_error",
  "max_cache_misses_exceeded",
  "max_ai_credits_exceeded",
  "mcp_policy_error",
  "model_not_supported_error",
  "shell_expansion_guard_rejected",
]);

/**
 * Silence is not evidence of an intentional noop. Preserve runtime diagnostics
 * in a first-class incomplete signal when the agent emitted no valid outputs.
 * @param {string[]} errors
 * @param {string} [rootDir]
 * @returns {{
 *   type: string,
 *   reason: string,
 *   failureCause: string,
 *   driverExitCode?: number,
 *   retryCount: number,
 *   engineErrorType?: string,
 *   details?: string
 * }}
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
  const redact = value => redactDiagnosticText(String(value), { secrets, maskedValues });
  const redactJson = value => JSON.stringify(value, (_key, nested) => (typeof nested === "string" ? redact(nested) : nested));
  let safeoutputsCliError = false;
  try {
    const auditPath = path.join(rootDir, "mcp-cli-audit/safeoutputs.jsonl");
    if (fs.lstatSync(auditPath).isFile()) {
      for (const line of fs.readFileSync(auditPath, "utf8").split("\n")) {
        try {
          const entry = JSON.parse(line);
          if (["parse_args_error", "unrecognized_args"].includes(entry?.event)) {
            safeoutputsCliError = true;
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
  let driverExitCode;
  let executionCategories = [];
  let executionErrorCodes = [];
  for (const event of events) {
    if (event.type === "agent.execution" && event.provenance.component === "execution" && event.provenance.phase === "agent" && event.data.exitCode > 0) {
      // A CLI parse error itself makes the bridge exit non-zero; preserve that more specific cause.
      if (!safeoutputsCliError) reason = "engine_driver_failure";
      diagnostics.add(`Driver exit code: ${event.data.exitCode}. The engine driver exited before a terminal safe output was recorded.`);
    }
    if (event.type === "agent.execution" && event.provenance.component === "execution" && event.provenance.phase === "agent") {
      if (Number.isSafeInteger(event.data.exitCode)) driverExitCode = event.data.exitCode;
      executionCategories = Array.isArray(event.data.categories) ? event.data.categories : executionCategories;
      executionErrorCodes = Array.isArray(event.data.errorCodes) ? event.data.errorCodes : executionErrorCodes;
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
  // Extract denied commands without copying harness configuration or transcript data.
  const attributedDiagnostics = agentErrorDiagnosticText(safeStdio, events);
  for (const command of extractDeniedCommands(attributedDiagnostics)) diagnostics.add(`Permission denied: ${command}`);
  const engineSummary = agentErrorSummaryText(safeStdio, events);
  if (engineSummary) diagnostics.add(engineSummary);
  const engineErrorType = engineSummary.match(/\(([a-z][a-z0-9_]*)\)/)?.[1] || "";
  if (driverExitCode !== undefined && ![...diagnostics].some(diagnostic => diagnostic.startsWith("Driver exit code:"))) {
    diagnostics.add(`Driver exit code: ${driverExitCode}`);
  }
  const errorCodes = executionErrorCodes.filter(code => Number.isSafeInteger(code) && code >= 400 && code <= 599);
  const isPromptExhaustion = executionCategories.some(category => PROMPT_EXHAUSTION_ERROR_CATEGORIES.has(category));
  const isRequestRejection = ["invalid_safe_outputs", "safeoutputs_cli_error"].includes(reason) || executionCategories.some(category => REQUEST_REJECTION_ERROR_CATEGORIES.has(category)) || errorCodes.some(code => code >= 400 && code < 500);
  const isEngineOutage = reason === "engine_driver_failure" || executionCategories.some(category => ["agentic_engine_timeout", "capi_server_error", "sandbox_runtime_crash"].includes(category)) || errorCodes.some(code => code >= 500);
  const failureCause = isPromptExhaustion ? "prompt_exhaustion" : isRequestRejection ? "request_rejection" : isEngineOutage ? "engine_outage" : "prompt_exhaustion";
  const retryEvents = events.filter(event => event.type === "claude.api_retry" || (event.type === "system" && event.data.subtype === "api_retry"));
  const harnessRetryLines = stdio.split(/\r?\n/).filter(line => /^\[(?:copilot|claude|codex)-harness\].*\bretrying\b/i.test(line));
  const harnessRetryCount = harnessRetryLines.length;
  const retryCount = Math.max(retryEvents.length, harnessRetryCount);
  const retryStatusCodes = [
    ...new Set(
      [...retryEvents.map(event => event.data.status ?? event.data.error_status ?? event.data.code), ...harnessRetryLines.flatMap(line => [...line.matchAll(/\b(?:HTTP\s+|status=)([1-5]\d{2})\b/gi)].map(match => Number(match[1])))].filter(
        code => Number.isSafeInteger(code) && code >= 100 && code <= 599
      )
    ),
  ];
  diagnostics.add(`Failure classification: ${failureCause}`);
  if (engineErrorType) {
    diagnostics.add(`Last engine error type: ${engineErrorType}`);
  } else if (reason === "engine_driver_failure") {
    diagnostics.add("Last engine error type: unknown");
  }
  diagnostics.add(`Retry attempts observed: ${retryCount}${retryStatusCodes.length ? ` (HTTP ${retryStatusCodes.join(", HTTP ")})` : ""}`);
  const details = [...diagnostics].slice(0, 20).join("\n");
  const sanitized = redactAndBoundDiagnostics(details, { secrets, maskedValues });
  return {
    type: "report_incomplete",
    reason,
    failureCause,
    ...(driverExitCode !== undefined ? { driverExitCode } : {}),
    retryCount,
    ...(engineErrorType ? { engineErrorType } : {}),
    ...(sanitized ? { details: sanitized } : {}),
  };
}

module.exports = { buildEmptyOutputOutcome, EMPTY_OUTPUT_CAUSES, EMPTY_OUTPUT_FAILURE_CAUSES };
