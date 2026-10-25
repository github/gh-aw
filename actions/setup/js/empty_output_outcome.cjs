// @ts-check

const { iterateJsonl, iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const fs = require("fs");
const path = require("path");
const { collectUnifiedSession } = require("./unified_session.cjs");
const { agentErrorSummaryText, collectAgentErrorEvidence, DRIVER_STDERR_RECORD_PATTERN, DRIVER_STDERR_SUMMARIES } = require("./agent_execution.cjs");
const { crashSignalNameForExitCode } = require("./harness_crash_signals.cjs");
const { sessionContext, isNestedSessionEvent } = require("./agent_session.cjs");
const { collectArtifactSecretValues } = require("./safe_output_manifest.cjs");
const { collectAddMaskedValues } = require("./add_mask_redaction.cjs");
const { redactDiagnosticText, redactAndBoundDiagnostics } = require("./diagnostic_sanitization.cjs");
const { lstatGuard } = require("./symlink_guard.cjs");

// Keep cause titles and the category allow-list in sync; schema and docs list these causes too.
const EMPTY_OUTPUT_CAUSES = Object.freeze({
  agent_startup_failure: "failed before agent execution started",
  engine_driver_failure: "engine driver failed before emitting a terminal safe output",
  safeoutputs_cli_error: "failed to invoke safeoutputs CLI",
  invalid_safe_outputs: "produced no valid safe outputs",
  missing_terminal_safe_output: "finished without a terminal safe output",
});
const EMPTY_OUTPUT_FAILURE_CAUSES = Object.freeze({
  agent_startup_failure: "failed before agent execution started",
  driver_crash: "engine driver crashed",
  secret_redaction_failure: "failed to redact secrets in logs",
  engine_outage: "experienced an engine outage",
  request_rejection: "had a request rejected",
  prompt_exhaustion: "reported incomplete result",
  missing_terminal_safe_output: "finished without a terminal safe output",
  unknown: "finished without a clear failure cause",
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
 * Read fixed runtime evidence paths without following artifact symlinks or
 * copying any payload into public diagnostics.
 * @param {string} rootDir
 * @param {string} relativePath
 * @returns {{status: "recorded", content: string} | {status: "not recorded" | "unreadable", content?: undefined}}
 */
function readCompletionEvidence(rootDir, relativePath) {
  try {
    let current = rootDir;
    if (!lstatGuard(current)?.isDirectory()) return { status: "unreadable" };
    for (const part of relativePath.split("/")) {
      current = path.join(current, part);
      if (!lstatGuard(current)) return { status: "unreadable" };
    }
    const stat = lstatGuard(current);
    if (!stat?.isFile() || stat.size > 10 * 1024 * 1024) return { status: "unreadable" };
    return { status: "recorded", content: fs.readFileSync(current, "utf8") };
  } catch (error) {
    const missing = error !== null && typeof error === "object" && "code" in error && error.code === "ENOENT";
    return { status: missing ? "not recorded" : "unreadable" };
  }
}

/**
 * Registration and persistence are observations, not proof of task completion.
 * Keep CLI and gateway counts separate: one invocation can appear in both.
 * @param {string} [rootDir]
 * @returns {string[]}
 */
function buildCompletionEvidence(rootDir = "/tmp/gh-aw") {
  const runnerTemp = process.env.RUNNER_TEMP || "/home/runner/work/_temp";
  let availability = "unknown (registration evidence not recorded)";
  const flag = readCompletionEvidence(path.join(runnerTemp, "gh-aw"), "safeoutputs/gateway_empty.flag");
  const cache = readCompletionEvidence(path.join(runnerTemp, "gh-aw"), "mcp-cli/tools/safeoutputs.json");
  const gateway = readCompletionEvidence(rootDir, "mcp-logs/tools.json");
  const validTools = tools => Array.isArray(tools) && tools.every(tool => typeof tool?.name === "string" && tool.name.length > 0);
  if (flag.status === "recorded") {
    availability = "unavailable (gateway registered zero tools)";
  } else if (flag.status === "unreadable") {
    // An unreadable authoritative override prevents a confident registration claim.
    availability = "unknown (registration evidence could not be read)";
  } else {
    for (const evidence of [cache, gateway]) {
      if (evidence.status === "recorded") {
        try {
          const value = JSON.parse(evidence.content);
          const tools = evidence === cache ? value : value?.servers?.safeoutputs;
          if (validTools(tools)) {
            availability = `${tools.length ? "available" : "unavailable"} at ${evidence === cache ? "setup" : "gateway snapshot"} (registered ${tools.length} tool${tools.length === 1 ? "" : "s"})`;
            break;
          }
          availability = "unknown (invalid registration evidence)";
        } catch {
          availability = "unknown (registration evidence could not be read)";
        }
      } else if (evidence.status === "unreadable") {
        availability = "unknown (registration evidence could not be read)";
      }
    }
  }
  const summary = [`Safe-output tools: ${availability}`];
  for (const [label, relativePath, eventName] of [
    ["CLI", "mcp-cli-audit/safeoutputs.audit.jsonl", "call_start"],
    ["MCP", "mcp-logs/rpc-messages.jsonl", "tools/call"],
  ]) {
    const evidence = readCompletionEvidence(rootDir, relativePath);
    let count = 0;
    let malformed = 0;
    for (const event of iterateJsonlEvents(evidence.content || "")) {
      if (event.type === "invalid") {
        if (event.reason === "blank") continue;
        malformed++;
        continue;
      }
      const entry = event.value;
      if (label === "CLI" ? entry?.event === eventName : ["rpc_request", "REQUEST"].includes(entry?.event ?? entry?.type) && entry?.server_id === "safeoutputs" && (entry?.method ?? entry?.payload?.method) === eventName) count++;
    }
    summary.push(`Safe-output ${label} calls observed: ${evidence.status === "recorded" ? `${count}${malformed ? ` (partial evidence: ${malformed} malformed record${malformed === 1 ? "" : "s"})` : ""}` : `unknown (${evidence.status})`}`);
  }
  const output = readCompletionEvidence(rootDir, "safeoutputs.jsonl");
  let recordCount = 0;
  let malformed = 0;
  for (const event of iterateJsonlEvents(output.content || "")) {
    if (event.type === "invalid" && event.reason === "blank") continue;
    recordCount++;
    if (event.type === "invalid") {
      malformed++;
      continue;
    }
    const item = event.value;
    if (!item || typeof item !== "object" || Array.isArray(item) || typeof item.type !== "string") malformed++;
  }
  summary.push(
    `Safe-output persistence: ${output.status === "recorded" ? `${recordCount ? "nonempty" : "empty"} (${recordCount} record${recordCount === 1 ? "" : "s"}, ${malformed} malformed; not validation or publication evidence)` : output.status}`
  );
  return summary;
}

/**
 * @param {string} [rootDir]
 * @returns {ReturnType<typeof collectUnifiedSession>}
 */
function collectOutcomeSession(rootDir = "/tmp/gh-aw") {
  return collectUnifiedSession({
    rootDir,
    ...(process.env.GH_AW_ENGINE_ID === "aider" ? { engine: "aider" } : {}),
    protectedSessionCapture: process.env.GH_AW_PROTECTED_SESSION_CAPTURE === "1",
    warn: () => {},
  });
}

/** @param {import("./types/unified_session").UnifiedSession} events @returns {boolean} */
function hasFailedFinalResult(events) {
  const workflowInfo = events.find(event => event.type === "workflow.info" && event.provenance.component === "workflow");
  const engineId = process.env.GH_AW_ENGINE_ID ?? workflowInfo?.data.engineId ?? workflowInfo?.data.engine_id;
  if (typeof engineId !== "string" || !["aider", "gemini"].includes(engineId)) return false;
  const result = events.filter(event => event.type === "session.result" && event.provenance.component === "agent" && event.provenance.phase === "agent" && !isNestedSessionEvent(event)).at(-1);
  return Array.isArray(result?.data.errors) && result.data.errors.length > 0;
}

/**
 * Silence is not evidence of an intentional noop. Preserve runtime diagnostics
 * in a first-class incomplete signal when the agent emitted no valid outputs.
 * @param {string[]} errors
 * @param {string} [rootDir]
 * @param {ReturnType<typeof collectUnifiedSession>} [session]
 * @returns {{
 *   type: string,
 *   reason: string,
 *   failureCause: string,
 *   driverExitCode?: number,
 *   driverExitSignal?: string,
 *   retryCount: number,
 *   engineErrorType?: string,
 *   details?: string
 * }}
 */
function buildEmptyOutputOutcome(errors, rootDir = "/tmp/gh-aw", session) {
  const protectedCapture = process.env.GH_AW_PROTECTED_SESSION_CAPTURE === "1";
  const diagnostics = new Set();
  if (errors.length) diagnostics.add("Safe-output validation status: failed");
  let reason = errors.length ? "invalid_safe_outputs" : "missing_terminal_safe_output";
  let events = [];
  let maskedValues = [];
  try {
    ({ events, maskedValues } = session ?? collectOutcomeSession(rootDir));
  } catch (error) {
    if (protectedCapture) throw error;
    diagnostics.add("Runtime diagnostics could not be collected.");
  }
  const failedFinalResult = hasFailedFinalResult(events);
  if (failedFinalResult) reason = "engine_driver_failure";
  let stdio = "";
  try {
    const stdioPath = path.join(rootDir, "agent-stdio.log");
    if (!protectedCapture && fs.lstatSync(stdioPath).isFile()) stdio = fs.readFileSync(stdioPath, "utf8");
  } catch {
    stdio = "";
  }
  maskedValues.push(...collectAddMaskedValues(stdio));
  const secrets = collectArtifactSecretValues();
  const redact = value => redactDiagnosticText(String(value), { secrets, maskedValues });
  const redactJson = value => JSON.stringify(value, (_key, nested) => (typeof nested === "string" ? redact(nested) : nested));
  let safeoutputsCliError = false;
  try {
    const auditPath = path.join(rootDir, "mcp-cli-audit/safeoutputs.audit.jsonl");
    if (fs.lstatSync(auditPath).isFile()) {
      for (const entry of iterateJsonl(fs.readFileSync(auditPath, "utf8"))) {
        try {
          if (["parse_args_error", "unrecognized_args"].includes(entry?.event)) {
            safeoutputsCliError = true;
            if (!failedFinalResult) reason = "safeoutputs_cli_error";
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
  let lastToolCallStatus = "not recorded";
  let lastToolExitCode;
  let failedToolCalls = 0;
  let permissionDenials = 0;
  let toolDenialLimitExceeded = false;
  let executionCategories = [];
  let executionErrorCodes = [];
  let contentFiltered = false;
  for (const event of events) {
    if (event.type === "agent.execution" && event.provenance.component === "execution" && event.provenance.phase === "agent" && event.data.exitCode > 0) {
      // A CLI parse error itself makes the bridge exit non-zero; preserve that more specific cause.
      if (!safeoutputsCliError) reason = "engine_driver_failure";
    }
    if (event.type === "agent.execution" && event.provenance.component === "execution" && event.provenance.phase === "agent") {
      if (Number.isSafeInteger(event.data.exitCode)) driverExitCode = event.data.exitCode;
      executionCategories = Array.isArray(event.data.categories) ? event.data.categories : executionCategories;
      executionErrorCodes = Array.isArray(event.data.errorCodes) ? event.data.errorCodes : executionErrorCodes;
    }
    if (event.provenance.component !== "agent") continue;
    /** @type {any} */
    const data = event.data;
    const context = sessionContext(event);
    if (event.type === "assistant.refusal" && data.reason === "content_filter" && context.agentId === undefined && !context.parentToolUseId) {
      contentFiltered = true;
      diagnostics.add("The model response was blocked by content filtering; task completion could not be confirmed.");
    } else if (event.type === "tool.execution_start") {
      lastToolCallStatus = "started (completion not recorded)";
      lastToolExitCode = undefined;
    } else if (event.type === "tool.execution_complete") {
      const failed =
        data.success === false ||
        data.is_error === true ||
        data.isError === true ||
        data.result?.isError === true ||
        data.result?.is_error === true ||
        data.status === "failed" ||
        data.status === "error" ||
        data.error != null ||
        (typeof data.exitCode === "number" && data.exitCode !== 0);
      lastToolCallStatus = failed ? "failed" : data.success === true || data.exitCode === 0 ? "succeeded" : "completed (outcome unknown)";
      lastToolExitCode = Number.isSafeInteger(data.exitCode) && data.exitCode >= 0 && data.exitCode <= 255 ? data.exitCode : undefined;
      if (failed) failedToolCalls++;
    } else if (event.type === "guard.tool_denials_exceeded") {
      toolDenialLimitExceeded = true;
    } else if (event.type === "session.result" && Array.isArray(data.permissionDenials)) {
      permissionDenials += data.permissionDenials.length;
    }
  }
  let agentNotStarted = false;
  for (const relativePath of ["agent_execution.json", "usage/agent/execution.json"]) {
    const evidence = readCompletionEvidence(rootDir, relativePath);
    if (evidence.status === "not recorded") continue;
    if (evidence.status === "recorded") {
      try {
        const execution = JSON.parse(evidence.content);
        const runId = process.env.GITHUB_RUN_ID || events.find(event => event.type === "workflow.info" && event.provenance.component === "workflow")?.data.runId;
        const runAttempt = process.env.GITHUB_RUN_ATTEMPT;
        agentNotStarted =
          execution?.version === 1 &&
          execution.component === "agent" &&
          execution.state === "not_started" &&
          Number.isSafeInteger(execution.run_id) &&
          execution.run_id > 0 &&
          Number.isSafeInteger(execution.run_attempt) &&
          execution.run_attempt > 0 &&
          (runId === undefined || String(execution.run_id) === String(runId)) &&
          (runAttempt === undefined || String(execution.run_attempt) === runAttempt) &&
          driverExitCode === undefined &&
          !events.some(event => event.provenance.component === "agent");
      } catch {
        diagnostics.add("Agent execution state could not be read.");
      }
    } else {
      diagnostics.add("Agent execution state could not be read.");
    }
    break;
  }
  if (agentNotStarted) reason = "agent_startup_failure";
  const redactedLines = [];
  for (const event of iterateJsonlEvents(stdio)) {
    const ending = event.rawLine.endsWith("\r\n") ? "\r\n" : event.rawLine.endsWith("\n") ? "\n" : "";
    redactedLines.push(event.type === "json" ? redactJson(event.value) + ending : redact(event.rawLine));
  }
  const safeStdio = redactedLines.join("");
  const rawSourceEngine = process.env.GH_AW_ENGINE_ID === "aider" || events.some(event => event.type === "workflow.info" && event.provenance.component === "workflow" && event.data.engineId === "aider") ? "aider" : undefined;
  const engineSummary = agentNotStarted ? "" : agentErrorSummaryText(safeStdio, events, rawSourceEngine);
  if (engineSummary) diagnostics.add(engineSummary);
  const driverExitSignal = crashSignalNameForExitCode(driverExitCode) ?? ({ 130: "SIGINT", 137: "SIGKILL", 143: "SIGTERM" }[driverExitCode] || undefined);
  let driverStderrSummary = "";
  let capturedStderrSummary = "";
  let unhandledDriverError = false;
  if (!agentNotStarted && driverExitCode > 0) {
    const evidence = collectAgentErrorEvidence(safeStdio, events, rawSourceEngine);
    const lastCapture = evidence.diagnostics
      .map(line => line.match(DRIVER_STDERR_RECORD_PATTERN))
      .filter(Boolean)
      .at(-1);
    if (lastCapture && Number(lastCapture[1]) === driverExitCode && Object.hasOwn(DRIVER_STDERR_SUMMARIES, lastCapture[2])) {
      capturedStderrSummary = DRIVER_STDERR_SUMMARIES[lastCapture[2]];
    }
    for (const line of evidence.diagnostics.slice().reverse()) {
      driverStderrSummary = agentErrorSummaryText(line, [], rawSourceEngine);
      if (!driverStderrSummary && /^\[(?:copilot-sdk-driver|sdk-driver)\]\s+(?:\[sdk-driver\]\s+)?(?:error|unhandled error):/i.test(line)) {
        driverStderrSummary = "SDK driver reported an error";
        // Session errors use "error:" too; only the outer driver's unhandled
        // exception establishes a crash rather than a handled provider failure.
        unhandledDriverError = /^\[copilot-sdk-driver\]\s+unhandled error:/i.test(line);
      }
      if (driverStderrSummary) break;
    }
  }
  const engineErrorType = engineSummary.match(/\(([a-z][a-z0-9_]*)\)/)?.[1] || "";
  const errorCodes = executionErrorCodes.filter(code => Number.isSafeInteger(code) && code >= 400 && code <= 599);
  const driverExitMeaning =
    { 1: "General failure", 2: "Invalid usage (conventional; engine-specific)", 124: "Command timed out (timeout utility convention)", 126: "Command is not executable", 127: "Command not found" }[driverExitCode] ||
    (driverExitSignal ? `Terminated by ${driverExitSignal} (128 + signal number)` : "Unknown (engine-specific exit code)");
  const isPromptExhaustion = executionCategories.some(category => PROMPT_EXHAUSTION_ERROR_CATEGORIES.has(category));
  const isRequestRejection =
    contentFiltered || ["invalid_safe_outputs", "safeoutputs_cli_error"].includes(reason) || executionCategories.some(category => REQUEST_REJECTION_ERROR_CATEGORIES.has(category)) || errorCodes.some(code => code >= 400 && code < 500);
  // Crash categories can be inferred from exit codes alone; they do not establish an outage.
  const isEngineOutage = executionCategories.some(category => ["agentic_engine_timeout", "capi_server_error"].includes(category)) || errorCodes.some(code => code >= 500);
  // Missing completion and nonzero driver exits are observed outcomes, not evidence of budget exhaustion or an outage.
  const failureCause = agentNotStarted
    ? "agent_startup_failure"
    : crashSignalNameForExitCode(driverExitCode) || (driverExitCode === undefined && executionCategories.includes("sandbox_runtime_crash")) || unhandledDriverError
      ? "driver_crash"
      : isPromptExhaustion
        ? "prompt_exhaustion"
        : isRequestRejection
          ? "request_rejection"
          : isEngineOutage
            ? "engine_outage"
            : reason === "missing_terminal_safe_output"
              ? "missing_terminal_safe_output"
              : driverExitCode > 0
                ? "driver_crash"
                : "unknown";
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
  const completionEvidence = buildCompletionEvidence(rootDir);
  const observedCompletionEvidence = completionEvidence.filter(
    line => !line.endsWith(": not recorded") && !line.includes("unknown (not recorded)") && !line.includes("unknown (registration evidence not recorded)") && !line.startsWith("Safe-output persistence: empty (0 records,")
  );
  const summary = agentNotStarted
    ? [
        `Failure classification: ${failureCause}`,
        "Agent execution state: not_started",
        "The runtime recorded that agent execution never started. Inspect the failed startup step and gateway/setup logs in the linked run; missing safe outputs are downstream, not evidence that the agent omitted a terminal output.",
      ]
    : failureCause === "missing_terminal_safe_output"
      ? [
          `Failure classification: ${failureCause}`,
          ...(driverExitCode !== undefined ? [`Driver exit code: ${driverExitCode}`] : []),
          ...(lastToolCallStatus !== "not recorded" ? [`Last tool call status: ${lastToolCallStatus}`] : []),
          ...(lastToolExitCode !== undefined ? [`Last tool exit code: ${lastToolExitCode}`] : []),
          ...(failedToolCalls ? [`Failed tool calls observed: ${failedToolCalls}`] : []),
          ...observedCompletionEvidence,
          "No terminal safe output was recorded. These observations do not establish why the agent omitted it; inspect the agent artifact (agent-session.jsonl and agent-stdio.log) for the final actions, then ensure the workflow emits a terminal safe output.",
        ]
      : [
          `Failure classification: ${failureCause}`,
          reason === "engine_driver_failure" && !failedFinalResult
            ? `Driver exit code: ${driverExitCode ?? "not recorded"}. The engine driver exited before a terminal safe output was recorded.`
            : `Driver exit code: ${driverExitCode ?? "not recorded"}`,
          ...(driverExitSignal ? [`Driver exit signal (inferred from exit code): ${driverExitSignal}`] : []),
          ...(driverExitCode > 0
            ? [`Driver exit meaning: ${driverExitMeaning}`, `Last driver stderr summary: ${capturedStderrSummary || driverStderrSummary || (stdio.trim() ? "not recorded (no classified stderr diagnostic)" : "no output captured")}`]
            : []),
          `Last tool call status: ${lastToolCallStatus}`,
          ...(lastToolExitCode !== undefined ? [`Last tool exit code: ${lastToolExitCode}`] : []),
          `Failed tool calls observed: ${failedToolCalls}`,
          `Token budget state: ${executionCategories.includes("effective_tokens_limit_exceeded") ? "exceeded" : "unknown (no exhaustion signal recorded)"}`,
          `Step budget state (LLM invocations): ${executionCategories.includes("invocation_cap_exceeded") ? "exceeded" : "unknown (no exhaustion signal recorded)"}`,
          ...completionEvidence,
        ];
  if (failureCause === "missing_terminal_safe_output" && !observedCompletionEvidence.length) summary.splice(-1, 0, "Safe-output telemetry: not recorded");
  if (failedFinalResult) diagnostics.add("The engine's final result reported an error; task completion could not be confirmed, even if safe outputs were recorded.");
  else if (!agentNotStarted && failureCause !== "missing_terminal_safe_output") diagnostics.add("Agent finished without emitting a terminal safe output; task completion could not be confirmed.");
  if (errorCodes.length) diagnostics.add(`Engine HTTP error codes: ${[...new Set(errorCodes)].join(", ")}`);
  if (permissionDenials) diagnostics.add(`Permission denials observed: ${permissionDenials}`);
  if (toolDenialLimitExceeded) diagnostics.add("Tool denial limit status: exceeded");
  if (engineErrorType) {
    diagnostics.add(`Last engine error type: ${engineErrorType}`);
  } else if (reason === "engine_driver_failure") {
    diagnostics.add("Last engine error type: unknown");
  }
  if (!agentNotStarted && (retryCount || failureCause !== "missing_terminal_safe_output")) diagnostics.add(`Retry attempts observed: ${retryCount}${retryStatusCodes.length ? ` (HTTP ${retryStatusCodes.join(", HTTP ")})` : ""}`);
  const details = [...summary, ...diagnostics].slice(0, 20).join("\n");
  const sanitized = redactAndBoundDiagnostics(details, { secrets, maskedValues });
  return {
    type: "report_incomplete",
    reason,
    failureCause,
    ...(driverExitCode !== undefined ? { driverExitCode } : {}),
    ...(driverExitSignal ? { driverExitSignal } : {}),
    retryCount,
    ...(engineErrorType ? { engineErrorType } : {}),
    ...(sanitized ? { details: sanitized } : {}),
  };
}

module.exports = { buildEmptyOutputOutcome, buildCompletionEvidence, collectOutcomeSession, hasFailedFinalResult, EMPTY_OUTPUT_CAUSES, EMPTY_OUTPUT_FAILURE_CAUSES };
