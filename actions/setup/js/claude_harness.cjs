// @ts-check

/**
 * Claude Code CLI Harness with Retry Logic
 *
 * Wraps the Claude Code CLI command with retry logic for failures that occur after the session
 * has been partially executed.  Passes all arguments to the claude subprocess, forwarding
 * stdout/stderr; the initial prompt is delivered through stdin.
 *
 * Retry policy:
 *   - If the process produced any output (hasOutput) and exits with a non-zero code, the
 *     driver resumes the captured session ID after genuine assistant progress.
 *   - Overloaded API errors (HTTP 529 / "overloaded_error") and rate-limit errors (HTTP 429 /
 *     "rate_limit_error") are well-known transient failure modes and are logged explicitly, but
 *     any partial-execution failure is retried — not just those specific errors.
 *   - "The request body is not valid JSON" (HTTP 400) is a transport-level serialization bug,
 *     observed immediately after a `permission_denied` tool-result on a compound Bash command.
 *     Fresh restarts after partial work require GH_AW_CLAUDE_ALLOW_FRESH_RESTART=true;
 *     otherwise the harness fails explicitly instead of replaying side effects.
 *   - Connection-refused failures before the first assistant response are retried as fresh
 *     runs because there is no session state to resume.
 *   - Other failures that produce no output use a separate bounded startup retry budget.
 *   - On a `--resume <id>` retry the initial prompt is omitted: Claude Code resumes the session
 *     from its on-disk state rather than re-processing the original instructions.
 *   - Retries use exponential backoff: 5s → 10s → 20s (capped at 60s) by default.
 *   - Maximum 3 retry attempts after the initial run by default.
 *
 * Prompt handling:
 *   - The harness expects a `--prompt-file <path>` argument in the args list.
 *   - For the initial run it reads the file and sends the content through stdin.
 *   - For resume retries stdin is empty.
 *
 * Usage: node claude_harness.cjs <command> [args...]
 * Example: node claude_harness.cjs claude --print --prompt-file /tmp/gh-aw/aw-prompts/prompt.txt
 */

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");
const { runProcess, formatDuration, resolvePostResultWatchdogIdleTimeoutMs } = require("./process_runner.cjs");
const { applyClaudeRuntimeTimeouts } = require("./tool_timeouts.cjs");
const { claudeFailureEvidence, hasClaudeSessionProgress, claudeSessionId, claudePermissionDenials, claudeBareCapabilities, claudeRepositoryEditPolicy, claudeSafeOutputsOffset, removeClaudePlugin } = require("./claude_runtime.cjs");
const { runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, shouldStopForNoopSafeOutputs } = require("./harness_retry_runner.cjs");
const { resolveRetryConfig: resolveSharedRetryConfig } = require("./harness_retry_config.cjs");
const {
  AWF_API_PROXY_REFLECT_URL,
  AWF_REFLECT_OUTPUT_PATH,
  AWF_REFLECT_TIMEOUT_MS,
  AWF_MODELS_URL_TIMEOUT_MS,
  GEMINI_MODEL_NAME_PREFIX,
  enrichReflectModels,
  extractModelIds,
  fetchAWFReflect,
  fetchModelsFromUrl,
  normalizeReflectProviderName,
  resolveProviderEndpointFromReflect,
} = require("./awf_reflect.cjs");
const { emitMissingToolPermissionIssue, emitInfrastructureIncomplete, hasExpectedSafeOutputs, hasNoopInSafeOutputs, hasTerminalSafeOutput } = require("./safeoutputs_cli.cjs");
const { countPermissionDeniedIssues, hasNumerousPermissionDeniedIssues, extractDeniedCommands, buildMissingToolPermissionIssuePayload } = require("./permission_denied_helpers.cjs");
const { detectNonRetryableHarnessGuard, buildSoftTimeoutGuard, emitSoftTimeoutSignal, isAuthenticationFailedError, parseAICreditsExceededProxyRejection } = require("./harness_retry_guard.cjs");
const { isCrashSignalExitCode, crashSignalNameForExitCode } = require("./harness_crash_signals.cjs");
const { MODEL_NOT_SUPPORTED_PATTERN: INVALID_MODEL_ERROR_PATTERN } = require("./detect_agent_errors.cjs");
const { applyModelFallback } = require("./model_fallback.cjs");
const { parseMaxAICreditsExceededFromAuditLog } = require("./ai_credits_context.cjs");

// Pattern to detect Anthropic API overload errors (HTTP 529).
// Matches "overloaded_error" from the Anthropic error type field, and the
// "Overloaded" human-readable message that Claude Code emits in its stream-json output.
const OVERLOADED_ERROR_PATTERN = /overloaded_error|"overloaded"/i;

// Pattern to detect Anthropic rate-limit errors (HTTP 429).
// Claude CLI may surface this as:
//   - transport-style text (e.g. "429 Too Many Requests")
//   - embedded stream-json result fields (e.g. "api_error_status":429)
//   - human-readable message text ("rate limit")
const RATE_LIMIT_ERROR_PATTERN = /rate_limit_error|429 Too Many Requests|"api_error_status"\s*:\s*429|request rejected \(429\)|rate limit/i;

// Pattern to detect the transport-level "invalid JSON request body" error.
// Observed after a `permission_denied` tool-result on a compound Bash command: the
// CLI appears to re-serialize the conversation for the next turn incorrectly,
// producing an empty/malformed body that the Anthropic API rejects with HTTP 400
// before any model logic runs. This is a serialization glitch, not a real
// application-level 400 from the model, so it should be retried — but as a fresh
// run rather than --continue, since resuming would resend the same corrupted
// session state and reproduce the identical error.
const INVALID_JSON_BODY_ERROR_PATTERN = /request body is not valid JSON/i;
const CONNECTION_REFUSED_ERROR_PATTERN = /connection refused|ECONNREFUSED/i;

// Pattern to detect a clean max-turns exit from Claude Code.
// Claude Code emits a JSON result object with "subtype":"error_max_turns" when the
// session ends because the turn limit was reached.  This is a deterministic terminal
// condition — --continue cannot recover it because no deferred tool marker was written.
const MAX_TURNS_EXIT_PATTERN = /"subtype"\s*:\s*"error_max_turns"/;

// Pattern to detect a "no deferred tool marker" error from Claude Code.
// This occurs when --continue is attempted but the session either was never deferred,
// the deferred marker is stale (tool already ran), or it falls outside the tail-scan
// window.  Retrying with --continue will always produce the same instant failure, so
// this path must not be retried via --continue (fall back to a fresh run if budget remains).
const NO_DEFERRED_MARKER_PATTERN = /No deferred tool marker found/i;
const SIGNAL_TERMINATION_EXIT_CODES = new Set([137, 143]);
const MAX_STARTUP_RETRIES = 2;

/**
 * Emit a timestamped diagnostic log line to stderr.
 * All driver messages are prefixed with "[claude-harness]" so they are easy to
 * grep out of the combined agent-stdio.log.
 * @param {string} message
 */
function log(message) {
  const ts = new Date().toISOString();
  process.stderr.write(`[claude-harness] ${ts} ${message}\n`);
}

/**
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(message: string) => void} [logger]
 * @returns {{maxRetries: number, initialDelayMs: number, backoffMultiplier: number, maxDelayMs: number}}
 */
function resolveRetryConfig(env = process.env, logger = log) {
  return resolveSharedRetryConfig(env, logger);
}

/**
 * Parse bounded startup retries for zero-output startup failures.
 * Retries are always fresh runs (never --continue).
 *
 * @param {NodeJS.ProcessEnv} [env]
 * @param {(message: string) => void} [logger]
 * @returns {number}
 */
function resolveStartupRetryLimit(env = process.env, logger = log) {
  const envVar = env.GH_AW_HARNESS_STARTUP_RETRIES == null || env.GH_AW_HARNESS_STARTUP_RETRIES === "" ? "GH_AW_CLAUDE_STARTUP_RETRIES" : "GH_AW_HARNESS_STARTUP_RETRIES";
  const raw = env[envVar];
  if (raw == null || raw === "") {
    return 1;
  }
  if (!/^[+-]?\d+$/.test(raw)) {
    logger(`invalid ${envVar}='${raw}' (expected integer); using default startup retries=1`);
    return 1;
  }
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed)) {
    logger(`invalid ${envVar}='${raw}' (not finite); using default startup retries=1`);
    return 1;
  }
  const clamped = Math.min(Math.max(parsed, 0), MAX_STARTUP_RETRIES);
  if (clamped !== parsed) {
    logger(`${envVar}=${parsed} out of range; clamped to ${clamped}`);
  }
  return clamped;
}

/**
 * Determines if the collected output contains an Anthropic overload error.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isOverloadedError(output) {
  return OVERLOADED_ERROR_PATTERN.test(output);
}

/**
 * Determines if the collected output contains an Anthropic rate-limit error.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isRateLimitError(output) {
  return RATE_LIMIT_ERROR_PATTERN.test(output);
}

/**
 * Remove stream-JSON user events, which contain tool results, from output used
 * by failure classifiers.
 *
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {string}
 */
function classifiableOutput(output) {
  return claudeFailureEvidence(output);
}

/**
 * Determines if the collected output signals a clean max-turns exit.
 * When Claude Code hits its turn limit it emits a result object with
 * "subtype":"error_max_turns".  This is not a transient error — retrying
 * with --continue will always fail because no deferred tool marker was written.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isMaxTurnsExit(output) {
  return MAX_TURNS_EXIT_PATTERN.test(output);
}

/**
 * Determines if the collected output contains the transport-level "invalid JSON
 * request body" error (HTTP 400 "The request body is not valid JSON"). This has
 * been observed immediately following a `permission_denied` tool-result on a
 * compound Bash command, where the CLI appears to re-serialize the conversation
 * incorrectly for the next turn. It is a serialization bug, not a genuine
 * application-level 400 from the model.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isInvalidJsonBodyError(output) {
  return INVALID_JSON_BODY_ERROR_PATTERN.test(output);
}

/**
 * Determines if the collected output contains a refused network connection.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isConnectionRefusedError(output) {
  return CONNECTION_REFUSED_ERROR_PATTERN.test(output);
}

/**
 * Determines whether Claude produced an assistant response before failing.
 * System initialization and transport-error events do not represent resumable work.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
/**
 * Determines if the collected output contains a "no deferred tool marker" error.
 * This occurs when Claude Code is invoked with --continue but the session was never
 * deferred, the deferred marker is stale (tool already ran), or it falls outside the
 * tail-scan window.  Each retry with --continue will instantly produce the same error,
 * so this should not be retried via --continue (fall back to fresh run retries).
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isNoDeferredMarkerError(output) {
  return NO_DEFERRED_MARKER_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates an invalid or unavailable model name.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isInvalidModelError(output) {
  return INVALID_MODEL_ERROR_PATTERN.test(output);
}

/**
 * Determines whether the exit code corresponds to signal-style termination
 * (SIGKILL=137 / SIGTERM=143), typically from timeout/cancellation.
 * @param {number} exitCode
 * @returns {boolean}
 */
function isSignalTerminationExitCode(exitCode) {
  return SIGNAL_TERMINATION_EXIT_CODES.has(exitCode);
}

/**
 * Decide whether the next retry should use --continue.
 * @param {{
 *   attempt: number,
 *   maxRetries: number,
 *   exitCode: number,
 *   hasOutput: boolean,
 *   isNoDeferredMarker: boolean,
 *   continueDisabledPermanently: boolean
 * }} input
 * @returns {boolean}
 */
function shouldRetryWithContinue({ attempt, maxRetries, exitCode, hasOutput, isNoDeferredMarker, continueDisabledPermanently }) {
  if (attempt >= maxRetries || !hasOutput || continueDisabledPermanently) {
    return false;
  }
  if (isSignalTerminationExitCode(exitCode) || isCrashSignalExitCode(exitCode)) {
    return false;
  }
  if (isNoDeferredMarker) {
    return false;
  }
  return true;
}

/**
 * Resolve --prompt-file arguments for the initial Claude run.
 * Separate --prompt-file content from argv for stdin delivery. This avoids
 * argument-size limits and Claude's variadic --mcp-config parsing.
 *
 * @param {string[]} args
 * @returns {{args: string[], prompt: string | null}}
 */
function resolveClaudePromptFileArgs(args) {
  /** @type {string[]} */
  const filteredArgs = [];
  /** @type {string|null} */
  let promptContent = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] !== "--prompt-file") {
      filteredArgs.push(args[i]);
      continue;
    }

    if (i + 1 >= args.length) {
      throw new Error("--prompt-file requires a readable file path");
    }

    const promptFile = args[i + 1];
    try {
      const stat = fs.statSync(promptFile);
      log(`resolved --prompt-file: path=${promptFile} size=${stat.size}B`);
      promptContent = fs.readFileSync(promptFile, "utf8");
    } catch (error) {
      const err = /** @type {Error} */ error;
      // An unreadable prompt file means no task instructions can be delivered to Claude.
      // Propagate as a fatal error rather than forwarding the harness-only flag to the
      // claude subprocess (which would fail with an "unknown option" error).
      throw new Error(`--prompt-file '${promptFile}' is not readable: ${err.message}`, { cause: err });
    }
    i++; // Skip the prompt-file path argument
  }

  return { args: filteredArgs, prompt: promptContent };
}

/**
 * Strip --prompt-file and its path argument from args.
 * Used for --continue retries where Claude resumes from on-disk session state
 * and should not be given the original prompt again.
 *
 * @param {string[]} args
 * @returns {string[]} Args with --prompt-file pair removed
 */
function stripPromptFileArgs(args) {
  /** @type {string[]} */
  const filteredArgs = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--prompt-file" && i + 1 < args.length) {
      i++; // Skip path too
      continue;
    }
    filteredArgs.push(args[i]);
  }
  return filteredArgs;
}

/**
 * Strip any user-supplied --continue flags from args.
 * The harness decides when --continue should be used on retries.
 *
 * @param {string[]} args
 * @returns {string[]}
 */
function stripContinueArgs(args) {
  return args.filter(arg => arg !== "--continue");
}

/**
 * Build Claude child process env with provider endpoint overrides resolved from /reflect.
 * @returns {Promise<NodeJS.ProcessEnv>}
 */
async function buildClaudeChildEnv() {
  const childEnv = { ...process.env };
  applyClaudeRuntimeTimeouts(childEnv);
  applyModelFallback(childEnv, "ANTHROPIC_MODEL", log);
  const provider = normalizeReflectProviderName(process.env.GH_AW_LLM_PROVIDER, "anthropic");
  try {
    const raw = fs.readFileSync(AWF_REFLECT_OUTPUT_PATH, "utf8");
    const reflectData = JSON.parse(raw);
    const resolved = resolveProviderEndpointFromReflect({ provider, reflectData, logger: log });
    if (resolved && resolved.baseUrl) {
      childEnv.ANTHROPIC_BASE_URL = resolved.baseUrl;
      log(`configured ANTHROPIC_BASE_URL from /reflect for provider=${provider}: ${resolved.baseUrl}`);
    }
  } catch (error) {
    const err = /** @type {Error} */ error;
    log(`warning: unable to resolve provider endpoint from /reflect: ${err.message}`);
  }
  return childEnv;
}

/**
 * Main entry point: run claude with retry logic for transient API failures.
 */
async function main() {
  const [, , command, ...args] = process.argv;
  const retryConfig = resolveRetryConfig(process.env, log);
  const { maxRetries, initialDelayMs, backoffMultiplier, maxDelayMs } = retryConfig;
  const startupRetryLimit = resolveStartupRetryLimit(process.env, log);

  if (!command) {
    process.stderr.write("claude-harness: Usage: node claude_harness.cjs <command> [args...]\n");
    process.exit(1);
  }

  log(`starting: command=${command} maxRetries=${maxRetries} initialDelayMs=${initialDelayMs}` + ` backoffMultiplier=${backoffMultiplier} maxDelayMs=${maxDelayMs}` + ` nodeVersion=${process.version} platform=${process.platform}`);

  // Resolve the prompt for the initial run (reads --prompt-file content).
  // A missing or unreadable prompt file is treated as a fatal startup error.
  let initialArgs;
  let prompt;
  let pluginDir;
  try {
    const resolved = resolveClaudePromptFileArgs(args);
    prompt = resolved.prompt;
    const capabilities = claudeBareCapabilities(claudeRepositoryEditPolicy(stripContinueArgs(resolved.args), process.env));
    initialArgs = capabilities.args;
    pluginDir = capabilities.pluginDir;
  } catch (err) {
    const e = /** @type {Error} */ err;
    log(`fatal: ${e.message}`);
    process.exit(1);
  }
  const freshRetryArgs = stripContinueArgs(initialArgs);
  // Args without --prompt-file, used as the base for --continue retries.
  const continueBaseArgs = freshRetryArgs;

  const safeInitialArgs = initialArgs;
  const safeFreshRetryArgs = freshRetryArgs;

  // Fetch AWF API proxy reflection data before running the agent to capture initial proxy state.
  // This is best-effort: failures are logged but do not affect the agent run.
  await fetchAWFReflect({ logger: log });
  const childEnv = await buildClaudeChildEnv();

  // Pre-flight: skip the agent entirely when a noop has already been written by a prior step.
  // A noop indicates the work is complete or there is nothing to do — starting the agent
  // would be wasteful and potentially harmful.
  const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS || "";
  if (shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log })) {
    removeClaudePlugin(pluginDir);
    process.exit(0);
  }

  let lastExitCode = 1;
  let useContinueOnRetry = false;
  let continueDisabledPermanently = false;
  let startupRetriesUsed = 0;
  // Tracks whether the *active session* (the run currently being resumed via --continue)
  // has ever produced an assistant response. This must persist across attempts — a later
  // --continue attempt can fail during its own startup (e.g. connection refused before it
  // emits anything) even though earlier attempts in the same session already made progress.
  // Reset only when a genuinely fresh run begins (see below), never on a --continue attempt.
  let sessionHasProgress = false;
  let sessionId;
  const driverStartTime = Date.now();
  // The same deadline guards both the retry loop and each active child process.
  const softTimeoutGuard = buildSoftTimeoutGuard(driverStartTime);

  const retryRun = await runHarnessRetryLoop({
    maxRetries,
    initialDelayMs,
    backoffMultiplier,
    maxDelayMs,
    driverStartTime,
    harnessName: "Claude harness",
    log,
    softTimeoutGuard,
    getRetryMode: () => (useContinueOnRetry ? "--continue" : "fresh run"),
    runAttempt: async attempt => {
      // Resume only the captured session; never select the latest unrelated conversation.
      // Claude Code resumes the session from on-disk state; re-sending the original
      // instructions would re-execute the full task from scratch.
      let currentArgs;
      if (attempt > 0 && useContinueOnRetry) {
        currentArgs = [...continueBaseArgs, "--resume", sessionId];
      } else {
        currentArgs = attempt === 0 ? initialArgs : freshRetryArgs;
        // This attempt starts a brand-new session (either attempt 0, or a fresh
        // retry that discards prior on-disk state) — no assistant progress can carry
        // forward from any earlier attempt, so reset the tracker.
        sessionHasProgress = false;
        sessionId = undefined;
      }

      // Use redacted args for logging when the run carries the prompt text.
      const logArgs = attempt === 0 ? safeInitialArgs : useContinueOnRetry ? currentArgs : safeFreshRetryArgs;
      const safeOutputsByteOffset = claudeSafeOutputsOffset(safeOutputsPath);
      const result = await runProcess({
        command,
        args: currentArgs,
        attempt,
        log,
        logArgs,
        env: childEnv,
        stdin: attempt > 0 && useContinueOnRetry ? undefined : (prompt ?? undefined),
        postResultWatchdog: safeOutputsPath
          ? {
              shouldArm: () => hasTerminalSafeOutput(safeOutputsPath, { byteOffset: safeOutputsByteOffset, logger: log }),
              inactivityTimeoutMs: resolvePostResultWatchdogIdleTimeoutMs(),
            }
          : undefined,
        runtimeGuard: softTimeoutGuard
          ? {
              shouldTerminate: () => (Date.now() >= softTimeoutGuard.softDeadlineMs ? { terminate: true, reason: "Claude execution reached its soft deadline" } : false),
            }
          : undefined,
      });
      sessionId = claudeSessionId(result.output) || sessionId;
      return { ...result, exitCode: result.runtimeGuardFired && result.exitCode === 0 ? 1 : result.exitCode, safeOutputsByteOffset };
    },
    handleFailure: ({ attempt, result }) => {
      if (result.runtimeGuardFired && softTimeoutGuard) {
        emitSoftTimeoutSignal(softTimeoutGuard, "during execution", "Claude harness", log);
        return { action: "stop" };
      }
      if (result.watchdogFired && hasTerminalSafeOutput(safeOutputsPath, { byteOffset: result.safeOutputsByteOffset, logger: log })) {
        log("post-result watchdog stopped an idle process after terminal safe-output — treating as success");
        return { action: "stop", exitCode: 0 };
      }
      const classifierOutput = claudeFailureEvidence(result.output);
      const isOverloaded = isOverloadedError(classifierOutput);
      const isRateLimit = isRateLimitError(classifierOutput);
      const isAuthenticationFailed = isAuthenticationFailedError(classifierOutput);
      const isMaxTurns = isMaxTurnsExit(classifierOutput);
      const isNoDeferredMarker = isNoDeferredMarkerError(classifierOutput);
      const isInvalidModel = isInvalidModelError(classifierOutput);
      const isInvalidJsonBody = isInvalidJsonBodyError(classifierOutput);
      const isConnectionRefused = isConnectionRefusedError(classifierOutput);
      // Accumulate across attempts of the same session: once an assistant response has been
      // observed, it stays true for the remainder of this session's --continue attempts, even
      // if a later attempt's own output contains nothing but startup/transport errors.
      sessionHasProgress = sessionHasProgress || hasClaudeSessionProgress(result.output);
      const denials = claudePermissionDenials(result.output);
      const permissionDeniedCount = denials.count;
      const hasNumerousPermissionDenied = permissionDeniedCount >= 3;
      const crashSignalName = crashSignalNameForExitCode(result.exitCode);
      log(
        `attempt ${attempt + 1} failed:` +
          ` exitCode=${result.exitCode}` +
          (crashSignalName ? ` crashSignal=${crashSignalName}` : "") +
          ` isOverloadedError=${isOverloaded}` +
          ` isRateLimitError=${isRateLimit}` +
          ` isAuthenticationFailedError=${isAuthenticationFailed}` +
          ` isMaxTurnsExit=${isMaxTurns}` +
          ` isNoDeferredMarkerError=${isNoDeferredMarker}` +
          ` isInvalidModelError=${isInvalidModel}` +
          ` isInvalidJsonBodyError=${isInvalidJsonBody}` +
          ` isConnectionRefusedError=${isConnectionRefused}` +
          ` sessionHasProgress=${sessionHasProgress}` +
          ` permissionDeniedCount=${permissionDeniedCount}` +
          ` hasNumerousPermissionDenied=${hasNumerousPermissionDenied}` +
          ` hasOutput=${result.hasOutput}` +
          ` retriesRemaining=${maxRetries - attempt}`
      );

      if (shouldStopForNoopSafeOutputs({ attempt, safeOutputsPath, hasNoopInSafeOutputs, log })) {
        return { action: "stop", exitCode: 0 };
      }

      const nonRetryableGuard = detectNonRetryableHarnessGuard(classifierOutput);
      if (nonRetryableGuard.apiProxyGuardRejection) {
        const reason = `AWF API proxy guardrail rejected the request: ${nonRetryableGuard.apiProxyGuardRejection.guard}`;
        log(`${reason} — not retrying`);
        emitInfrastructureIncomplete(reason, { logger: log });
        return { action: "stop" };
      }
      const proxyAICreditsRejection = parseAICreditsExceededProxyRejection(result.output);
      if (proxyAICreditsRejection) {
        log(`attempt ${attempt + 1}: AWF API proxy rejected the request with HTTP 403 max-AI-credits (${proxyAICreditsRejection.aiCredits}/${proxyAICreditsRejection.maxAICredits}) — trusted budget-abort evidence`);
      }
      const trustedAICreditsExceeded = nonRetryableGuard.aiCreditsExceeded && (!!proxyAICreditsRejection || parseMaxAICreditsExceededFromAuditLog());
      if (nonRetryableGuard.aiCreditsExceeded && !trustedAICreditsExceeded) {
        log(`attempt ${attempt + 1}: AI credits marker found in CLI output without trusted firewall audit confirmation — preserving normal failure handling`);
      }
      // Some CLIs surface the proxy's budget rejection as an authentication failure (e.g. Claude Code
      // reports `error: authentication_failed` for "403 Maximum AI credits exceeded"). When the trusted
      // proxy signature is present that veto must not mask intentional budget enforcement.
      const shouldTreatAICreditsExceededAsSuccess = trustedAICreditsExceeded && (!isAuthenticationFailed || !!proxyAICreditsRejection);
      if (shouldTreatAICreditsExceededAsSuccess || nonRetryableGuard.awfAPIProxyBlockingRequests || nonRetryableGuard.maxRunsExceeded) {
        const reasons = [];
        if (shouldTreatAICreditsExceededAsSuccess) reasons.push("AI credits budget exceeded");
        if (nonRetryableGuard.awfAPIProxyBlockingRequests) reasons.push("AWF API proxy is blocking requests");
        if (nonRetryableGuard.maxRunsExceeded) reasons.push("maximum LLM invocations exceeded");
        log(`attempt ${attempt + 1}: ${reasons.join(" and ")} — not retrying (non-retryable guard condition)`);
        if (shouldTreatAICreditsExceededAsSuccess) {
          log(`attempt ${attempt + 1}: AI credits budget enforced — exiting 0 (budget control, not an error)`);
          return { action: "stop", exitCode: 0 };
        }
        if (nonRetryableGuard.maxRunsExceeded && safeOutputsPath && hasExpectedSafeOutputs(safeOutputsPath, { logger: log })) {
          log(`attempt ${attempt + 1}: invocation cap saturated but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
          return { action: "stop", exitCode: 0 };
        }
        return { action: "stop" };
      }

      const isSignalTermination = isSignalTerminationExitCode(result.exitCode);
      const isCrashSignal = isCrashSignalExitCode(result.exitCode);
      if (sessionHasProgress && (isSignalTermination || isCrashSignal || isNoDeferredMarker || isInvalidJsonBody) && process.env.GH_AW_CLAUDE_ALLOW_FRESH_RESTART !== "true") {
        const reason = "Claude cannot safely resume completed work; refusing to replay the original task. Set GH_AW_CLAUDE_ALLOW_FRESH_RESTART=true only for replay-safe workflows.";
        emitInfrastructureIncomplete(reason, { logger: log });
        log(reason);
        return { action: "stop" };
      }
      if (attempt < maxRetries && result.hasOutput && (isSignalTermination || isCrashSignal)) {
        continueDisabledPermanently = true;
        useContinueOnRetry = false;
        const reason = isCrashSignal
          ? `fatal-signal crash exitCode=${result.exitCode} (signal=${crashSignalName}, failure_reason=sandbox_runtime_crash)`
          : `signal-style termination exitCode=${result.exitCode} (failure_reason=cancelled_or_timed_out)`;
        log(`attempt ${attempt + 1}: ${reason} — will retry with fresh run (--continue disabled permanently) (attempt ${attempt + 2}/${maxRetries + 1})`);
        return { action: "retry" };
      }

      if (attempt === 0 && isAuthenticationFailed) {
        log(`attempt ${attempt + 1}: authentication failed — not retrying (first-attempt auth failure is non-retryable)`);
        return { action: "stop" };
      }

      if (isInvalidModel) {
        log(`attempt ${attempt + 1}: invalid/unsupported model configuration — not retrying (specify a valid engine model name in workflow frontmatter)`);
        return { action: "stop" };
      }

      if (hasNumerousPermissionDenied) {
        if (safeOutputsPath && hasExpectedSafeOutputs(safeOutputsPath, { logger: log })) {
          log(`attempt ${attempt + 1}: detected numerous permission-denied issues but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
          return { action: "stop", exitCode: 0 };
        }
        const deniedCommands = denials.commands;
        emitMissingToolPermissionIssue({ deniedCommands, logger: log });
        log(`attempt ${attempt + 1}: detected numerous permission-denied issues — not retrying (classified as missing tool/permission issue)`);
        return { action: "stop" };
      }

      if (isMaxTurns) {
        log(`attempt ${attempt + 1}: max_turns exit — not retriable via --continue`);
        return { action: "stop" };
      }

      if (isNoDeferredMarker) {
        if (attempt < maxRetries && result.hasOutput) {
          if (!continueDisabledPermanently && !sessionId) {
            log("partial execution produced no session ID — not retrying to avoid resuming an unrelated conversation");
            return { action: "stop" };
          }
          useContinueOnRetry = false;
          continueDisabledPermanently = true;
          log(`attempt ${attempt + 1}: no deferred tool marker on --continue — retrying as fresh run (failure_reason=harness_retry_path_invalid, --continue disabled permanently, attempt ${attempt + 2}/${maxRetries + 1})`);
          return { action: "retry" };
        }
        log(`attempt ${attempt + 1}: no deferred tool marker — not retriable via --continue (failure_reason=harness_retry_path_invalid)`);
        return { action: "stop" };
      }

      if (isInvalidJsonBody) {
        if (attempt < maxRetries && result.hasOutput) {
          useContinueOnRetry = false;
          continueDisabledPermanently = true;
          log(
            `attempt ${attempt + 1}: invalid JSON request body (transport-level serialization bug, likely following a permission_denied) — retrying as fresh run (--continue disabled permanently, attempt ${attempt + 2}/${maxRetries + 1})`
          );
          return { action: "retry" };
        }
        log(`attempt ${attempt + 1}: invalid JSON request body — not retriable via --continue (failure_reason=harness_retry_path_invalid)`);
        return { action: "stop" };
      }

      if (isConnectionRefused && !sessionHasProgress && attempt < maxRetries) {
        useContinueOnRetry = false;
        log(`attempt ${attempt + 1}: connection refused before first assistant response — retrying as fresh run with backoff (attempt ${attempt + 2}/${maxRetries + 1})`);
        return { action: "retry" };
      }

      const isNoProgressOutputFailure = !sessionHasProgress && result.hasOutput && !isSignalTerminationExitCode(result.exitCode) && !isCrashSignalExitCode(result.exitCode);
      if (isNoProgressOutputFailure && attempt < maxRetries && startupRetriesUsed < startupRetryLimit) {
        startupRetriesUsed++;
        useContinueOnRetry = false;
        log(`attempt ${attempt + 1}: output produced but no Claude session progress — retrying startup as fresh run ` + `(startup retry ${startupRetriesUsed}/${startupRetryLimit}, next attempt ${attempt + 2}/${maxRetries + 1})`);
        return { action: "retry", nextDelayMs: initialDelayMs };
      }
      if (isNoProgressOutputFailure) {
        log(`attempt ${attempt + 1}: output produced but no Claude session progress — not retrying (startup retry budget exhausted: ${startupRetriesUsed}/${startupRetryLimit})`);
        return { action: "stop" };
      }

      if (attempt < maxRetries && result.hasOutput) {
        const retryWithContinue = shouldRetryWithContinue({
          attempt,
          maxRetries,
          exitCode: result.exitCode,
          hasOutput: result.hasOutput,
          isNoDeferredMarker,
          continueDisabledPermanently,
        });
        const reason = isOverloaded ? "overloaded_error (transient)" : isRateLimit ? "rate_limit_error (transient)" : "partial execution";
        useContinueOnRetry = retryWithContinue;
        const retryMode = retryWithContinue ? "--continue" : "fresh run (--continue disabled permanently)";
        log(`attempt ${attempt + 1}: ${reason} — will retry with ${retryMode} (attempt ${attempt + 2}/${maxRetries + 1})`);
        return { action: "retry" };
      }

      if (attempt >= maxRetries) {
        log(`all ${maxRetries} retries exhausted — giving up (exitCode=${result.exitCode})`);
      } else {
        if (startupRetriesUsed < startupRetryLimit) {
          startupRetriesUsed++;
          useContinueOnRetry = false;
          const nextAttemptNumber = attempt + 2;
          const totalAttempts = maxRetries + 1;
          log(`attempt ${attempt + 1}: no output produced — retrying startup as fresh run ` + `(startup retry ${startupRetriesUsed}/${startupRetryLimit}, next attempt ${nextAttemptNumber} of ${totalAttempts} total attempts)`);
          return { action: "retry", nextDelayMs: initialDelayMs };
        }
        log(
          `attempt ${attempt + 1}: no output produced — not retrying (startup retry budget exhausted: ${startupRetriesUsed}/${startupRetryLimit}; possible causes: binary not found, permission denied, auth failure, or silent startup crash)`
        );
      }

      return { action: "stop" };
    },
  });
  lastExitCode = retryRun.exitCode;

  // Fetch AWF API proxy reflection data and persist to disk for post-run step summary.
  await fetchAWFReflect({ logger: log });

  log(`done: exitCode=${lastExitCode} totalDuration=${formatDuration(Date.now() - driverStartTime)}`);
  removeClaudePlugin(pluginDir);
  process.exit(lastExitCode);
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    resolveClaudePromptFileArgs,
    stripPromptFileArgs,
    classifiableOutput,
    isRateLimitError,
    isAuthenticationFailedError,
    isMaxTurnsExit,
    isNoDeferredMarkerError,
    isInvalidModelError,
    isInvalidJsonBodyError,
    isConnectionRefusedError,
    hasClaudeSessionProgress,
    isSignalTerminationExitCode,
    isCrashSignalExitCode,
    crashSignalNameForExitCode,
    shouldRetryWithContinue,
    countPermissionDeniedIssues,
    hasNumerousPermissionDeniedIssues,
    extractDeniedCommands,
    buildMissingToolPermissionIssuePayload,
    emitMissingToolPermissionIssue,
    hasNoopInSafeOutputs,
    hasExpectedSafeOutputs,
    resolveRetryConfig,
    resolveStartupRetryLimit,
    applyModelFallback,
  };
}

if (require.main === module) {
  main().catch(err => {
    log(`unexpected error: ${getErrorMessage(err)}`);
    process.exit(1);
  });
}
