// @ts-check

/**
 * Detect agent engine errors in the agent stdio log and AWF firewall audit log.
 *
 * Scans the agent stdio log for known error patterns and the AWF firewall audit
 * JSONL log for structured error events, then sets GitHub Actions output variables
 * for each detected error class:
 *
 *   - inference_access_error: The COPILOT_GITHUB_TOKEN does not have valid
 *     access to inference (e.g., "Access denied by policy settings").
 *   - mcp_policy_error: MCP servers were blocked by enterprise/organization
 *     policy (e.g., "MCP servers were blocked by policy: 'github', 'safeoutputs'").
 *   - agentic_engine_timeout: A timeout signature was detected in engine logs.
 *     This includes process termination by signal (SIGTERM/SIGKILL/SIGINT),
 *     typically due to step timeout-minutes, and SDK idle-timeout messages
 *     ("Timeout after <n>ms waiting for session.idle"). It is also set when the
 *     engine execution step failed after running for its full `timeout-minutes`
 *     budget, which is how GitHub Actions step-level timeouts surface for engines
 *     that leave no timeout signature in the agent log.
 *   - model_not_supported_error: The configured model is invalid or unsupported
 *     for the selected engine/account (for example unknown model name, model not
 *     found, or model unavailable for the plan).
 *   - http_400_response_error: The engine surfaced a generic HTTP 400 Bad Request
 *     response (for example "Response status code does not indicate success: 400 (Bad Request)").
 *   - capi_quota_exceeded_error: The Copilot CAPI quota has been exhausted
 *     or rate-limited (e.g., "CAPIError: 429 429 quota exceeded",
 *     "CAPIError: Too Many Requests", or the Copilot CLI's own
 *     retry-exhaustion message "Failed to get response from the AI model;
 *     retried N times ... Last error: 429" which carries no "CAPIError:"
 *     prefix). Quota errors are non-retryable because the Copilot CLI/SDK has
 *     already retried internally before surfacing the error.
 *   - capi_server_error: The Copilot CLI exhausted its internal retries on a
 *     5xx response. This is retryable by the harness because it may recover
 *     during harness backoff.
 *   - invocation_cap_exceeded: The per-run pooled LLM invocation cap is
 *     fully exhausted (e.g., "CAPIError: 429 Maximum LLM invocations exceeded (N/N)"
 *     or `"type":"max_runs_exceeded"`). This is more specific than generic
 *     CAPI quota exhaustion and takes precedence in step outputs.
 *   - missing_model_pricing_error / missing_model_pricing_model_name: The AWF API
 *     proxy rejected a request because the model has no AI credits pricing entry.
 *     Detected from the agent stdio log (text pattern) and the AWF firewall audit
 *     JSONL log (`unknown_model_ai_credits` event type). Both sources are checked
 *     and their results merged.
 *   - shell_expansion_guard_rejected: The sandbox's shell command-injection guard
 *     rejected a shell command for containing (or appearing to contain) bash
 *     expansion patterns (command substitution, indirect expansion, parameter
 *     transformation, etc.), e.g. "...could enable arbitrary code execution.
 *     Please rewrite the command without these expansion patterns." This can
 *     misfire on benign multi-line `printf`/`safeoutputs` CLI invocations; agents
 *     should switch to the `jq -Rs` file-piping pattern instead of retrying the
 *     same command verbatim.
 * This replaces the individual bash scripts (detect_inference_access_error.sh,
 * detect_mcp_policy_error.sh) with a single JavaScript step.
 *
 * In addition to the output variables above, when the engine sets
 * GH_AW_ENGINE_INTERNAL_LOGS_DIR (via CodingAgentEngine.GetInternalLogsDir) and the execution
 * step failed, this script tails the most recently modified `*.log` file under that directory
 * into the step log (see renderInternalEngineLogOnFailure()). Some engines (e.g. Codex CLI)
 * write their own tracing/diagnostic output to files rather than to stdout/stderr, so a bare
 * non-zero exit code with no console output can still have a diagnosable error recorded there.
 *
 * Exit codes:
 *   0 — Always succeeds (uses continue-on-error in the workflow step)
 */

"use strict";

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const errorPatterns = require("./agent_error_patterns.cjs");
const { detectErrors, buildOutputLines, isStepTimeout, sanitizeModelName } = errorPatterns;
const { parseUnknownModelAICreditsAndModelFromAuditLog, parseMaxCacheMissesExceededFromEventLog } = require("./ai_credits_context.cjs");
const { renderLogFromFile } = require("./render_detection_log.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { collectAgentExecution, agentErrorDiagnosticText, parseAgentExitCode } = require("./agent_execution.cjs");
const { writeSessionArtifact } = require("./session_artifact.cjs");
const { collectAddMaskedValues } = require("./add_mask_redaction.cjs");

const LOG_FILE = "/tmp/gh-aw/agent-stdio.log";

// File written by the engine execution step with the epoch milliseconds at which the
// engine CLI was started. Used to measure how long the engine ran before it was killed.
const AGENT_CLI_START_MS_FILE = "/tmp/gh-aw/agent_cli_start_ms.txt";

/**
 * Read the engine CLI start timestamp (epoch milliseconds) written by the execution step.
 * @returns {number} Epoch milliseconds, or NaN when unavailable/unparsable
 */
function readAgentCLIStartMs() {
  try {
    if (!fs.existsSync(AGENT_CLI_START_MS_FILE)) return NaN;
    return parseInt(fs.readFileSync(AGENT_CLI_START_MS_FILE, "utf8").trim(), 10);
  } catch {
    return NaN;
  }
}

/**
 * Detect a step-level timeout from the current environment and on-disk start timestamp.
 * @returns {boolean}
 */
function detectStepTimeoutFromEnvironment() {
  return isStepTimeout({
    outcome: process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME,
    timeoutMinutes: process.env.GH_AW_ENGINE_STEP_TIMEOUT_MINUTES,
    startMs: readAgentCLIStartMs(),
    nowMs: Date.now(),
  });
}

/**
 * Write GitHub Actions outputs to $GITHUB_OUTPUT.
 * @param {{ inferenceAccessError: boolean, mcpPolicyError: boolean, agenticEngineTimeout: boolean, modelNotSupportedError: boolean, http400ResponseError: boolean, capiQuotaExceededError: boolean, invocationCapExceeded: boolean, maxCacheMissesExceeded: boolean, missingModelPricingError: boolean, missingModelPricingModelName: string, shellExpansionGuardRejected: boolean }} results
 */
function writeOutputs(results) {
  const outputFile = process.env.GITHUB_OUTPUT;
  if (!outputFile) {
    process.stderr.write("[detect-agent-errors] GITHUB_OUTPUT not set — skipping output\n");
    return;
  }

  const lines = buildOutputLines(results);
  try {
    fs.appendFileSync(outputFile, lines.join("\n") + "\n");
  } catch (err) {
    process.stderr.write(`[detect-agent-errors] Failed to write to GITHUB_OUTPUT: ${getErrorMessage(err)}\n`);
  }
}

/**
 * Finds the most recently modified `*.log` file under `dir`, recursing into subdirectories.
 * @param {string} dir
 * @returns {string | undefined}
 */
function findMostRecentLogFile(dir) {
  /** @type {{ path: string, mtimeMs: number }[]} */
  const logFiles = [];

  /** @param {string} current */
  function walk(current) {
    let entries;
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const entryPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        walk(entryPath);
      } else if (entry.isFile() && entry.name.endsWith(".log")) {
        try {
          logFiles.push({ path: entryPath, mtimeMs: fs.statSync(entryPath).mtimeMs });
        } catch {
          // Ignore files that disappear or fail to stat between readdir and stat.
        }
      }
    }
  }

  walk(dir);
  if (logFiles.length === 0) {
    return undefined;
  }

  logFiles.sort((a, b) => b.mtimeMs - a.mtimeMs);
  return logFiles[0].path;
}

/**
 * Renders the most recently modified log file under the engine-provided internal logs
 * directory (GH_AW_ENGINE_INTERNAL_LOGS_DIR) to the step log, when the engine execution
 * step failed.
 *
 * Some engines (e.g. Codex CLI) write their own tracing/diagnostic output to files rather
 * than to stdout/stderr, so a bare non-zero exit code with no console output can still have
 * a diagnosable error recorded there. Without this, such failures are invisible black-box
 * "exit code 1" crashes even though the engine logged the real cause internally.
 *
 * This reuses the same renderLogFromFile helper used for the threat-detection log, so the
 * rendered content gets the same secret redaction and `::stop-commands::` wrapping
 * (preventing `::`-shaped log lines from being interpreted as workflow commands) without
 * duplicating that logic in shell.
 *
 * This is a no-op when GH_AW_ENGINE_INTERNAL_LOGS_DIR is not set, the execution outcome was
 * not "failure", or no log files are found under the directory.
 * @returns {Promise<void>}
 */
async function renderInternalEngineLogOnFailure() {
  const internalLogsDir = process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR;
  if (!internalLogsDir) {
    return;
  }

  const outcome = process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME;
  if (outcome !== "failure") {
    return;
  }

  const logFile = findMostRecentLogFile(internalLogsDir);
  if (!logFile) {
    process.stderr.write(`[detect-agent-errors] No engine internal log files found under ${internalLogsDir}\n`);
    return;
  }

  await renderLogFromFile(logFile, `Engine internal logs (${internalLogsDir})`, { tailLines: 200 });
}

/**
 * @param {string} logContent
 * @param {ReturnType<typeof detectErrors>} results
 * @param {string} [rootDir]
 */
function persistAgentExecution(logContent, results, rootDir = path.dirname(LOG_FILE)) {
  const exitPath = path.join(rootDir, "agent_execution_exit_code.txt");
  const execution = collectAgentExecution({
    content: logContent,
    categories: buildOutputLines(results)
      .filter(line => line.endsWith("=true"))
      .map(line => line.slice(0, -5)),
    ...(fs.existsSync(exitPath) ? { exitCode: parseAgentExitCode(fs.readFileSync(exitPath, "utf8")) } : {}),
  });
  writeSessionArtifact(path.join(rootDir, "agent-errors.jsonl"), execution ? [execution] : [], collectAddMaskedValues(logContent));
}

async function main() {
  let logContent = "";

  if (fs.existsSync(LOG_FILE)) {
    try {
      logContent = fs.readFileSync(LOG_FILE, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${LOG_FILE}: ${getErrorMessage(err)}`, { cause: err });
    }
  } else {
    process.stderr.write(`[detect-agent-errors] Log file not found: ${LOG_FILE}\n`);
  }

  const stdioResults = detectErrors(agentErrorDiagnosticText(logContent));

  // Also check the AWF firewall structured JSONL logs for the `unknown_model_ai_credits`
  // event — the API proxy event log is preferred and the audit log is used as a fallback.
  // These logs carry both the error type and the model name, providing a more reliable
  // detection source than text-scanning the stdio log.
  const { detected: auditMissingPricing, modelName: auditModelName } = parseUnknownModelAICreditsAndModelFromAuditLog();
  if (auditMissingPricing && !stdioResults.missingModelPricingError) {
    process.stderr.write(`[detect-agent-errors] Detected missing model pricing from firewall structured log: model "${auditModelName}" has no AI credits pricing configured\n`);
  }

  // Also check the AWF API proxy event logs for the `max_cache_misses_exceeded` structured
  // event. This covers all engines since the proxy guardrail fires independently of the
  // underlying AI engine.
  const eventLogCacheMissesExceeded = parseMaxCacheMissesExceededFromEventLog();
  if (eventLogCacheMissesExceeded && !stdioResults.maxCacheMissesExceeded) {
    process.stderr.write("[detect-agent-errors] Detected max cache misses exceeded from AWF API proxy event log\n");
  }

  // A GitHub Actions step-level timeout kills the engine externally and can leave no
  // timeout signature in the agent log, so it is detected from the step outcome and the
  // engine run duration instead.
  const stepTimeout = detectStepTimeoutFromEnvironment();
  if (stepTimeout && !stdioResults.agenticEngineTimeout) {
    process.stderr.write(`[detect-agent-errors] Detected step timeout: the engine execution step reached its ${process.env.GH_AW_ENGINE_STEP_TIMEOUT_MINUTES}-minute timeout-minutes budget and was terminated\n`);
  }

  const results = {
    ...stdioResults,
    agenticEngineTimeout: stdioResults.agenticEngineTimeout || stepTimeout,
    maxCacheMissesExceeded: stdioResults.maxCacheMissesExceeded || eventLogCacheMissesExceeded,
    missingModelPricingError: stdioResults.missingModelPricingError || auditMissingPricing,
    missingModelPricingModelName: stdioResults.missingModelPricingModelName || sanitizeModelName(auditModelName),
  };

  if (results.inferenceAccessError) {
    process.stderr.write("[detect-agent-errors] Detected inference access error in agent log\n");
  }
  if (results.mcpPolicyError) {
    process.stderr.write("[detect-agent-errors] Detected MCP policy error in agent log\n");
  }
  if (stdioResults.agenticEngineTimeout) {
    process.stderr.write("[detect-agent-errors] Detected agentic engine timeout signature in agent log\n");
  }
  if (results.modelNotSupportedError) {
    process.stderr.write("[detect-agent-errors] Detected model configuration error: configured model is invalid or unavailable for this engine/account\n");
  }
  if (results.http400ResponseError) {
    process.stderr.write("[detect-agent-errors] Detected HTTP 400 response error in agent log\n");
  }
  if (results.capiQuotaExceededError) {
    process.stderr.write("[detect-agent-errors] Detected CAPI quota exhaustion: Copilot quota has been exceeded\n");
  }
  if (results.invocationCapExceeded) {
    process.stderr.write("[detect-agent-errors] Detected invocation cap exhaustion: the pooled per-run LLM invocation budget is fully saturated\n");
  }
  if (results.maxCacheMissesExceeded) {
    process.stderr.write("[detect-agent-errors] Detected max cache misses exceeded: the AWF API proxy consecutive cache miss limit was reached\n");
  }
  if (results.missingModelPricingError && !auditMissingPricing) {
    process.stderr.write(`[detect-agent-errors] Detected missing model pricing: model "${results.missingModelPricingModelName}" has no AI credits pricing configured\n`);
  }
  if (results.shellExpansionGuardRejected) {
    process.stderr.write(
      "[detect-agent-errors] Detected sandbox shell expansion guard rejection: a shell command was rejected for dangerous bash expansion patterns; use the jq -Rs file-piping pattern for multi-line safeoutputs CLI bodies instead of retrying\n"
    );
  }

  writeOutputs(results);

  persistAgentExecution(logContent, results);

  await renderInternalEngineLogOnFailure();
}

if (require.main === module) {
  main().catch(err => {
    process.stderr.write(`[detect-agent-errors] Unhandled error: ${err instanceof Error && err.stack ? err.stack : getErrorMessage(err)}\n`);
  });
}

module.exports = {
  ...errorPatterns,
  persistAgentExecution,
  main,
  detectStepTimeoutFromEnvironment,
  findMostRecentLogFile,
  renderInternalEngineLogOnFailure,
};
