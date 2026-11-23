// @ts-check

/**
 * Unified agentic engine harness core.
 *
 * Every engine harness (`<engine>_harness.cjs`) shares one lifecycle:
 *
 *   1. capture and strip execution-evidence configuration from the environment;
 *   2. parse `<command> [args...]` and resolve the shared retry configuration;
 *   3. skip the agent when a prior step already wrote a noop safe output;
 *   4. run attempts through {@link runHarnessRetryLoop} with a soft-timeout guard,
 *      retry telemetry and a per-attempt safe-outputs byte offset;
 *   5. classify failures (non-retryable guards, AI-credit and invocation budgets);
 *   6. log the final exit code and total duration.
 *
 * This module owns those generic pieces. Engine specializations supply only what
 * is genuinely engine specific: argument/model preparation, a single attempt
 * (`runAttempt`) and the engine's failure policy (`handleFailure`), composed from
 * the shared failure building blocks exported here.
 *
 * Layers:
 *   - {@link createHarnessRuntime}: retry configuration, soft deadline, telemetry and
 *     safe-outputs offsets around the retry loop. Used directly by the generic harness.
 *   - {@link startEngineHarness}: adds the process-level lifecycle (evidence capture,
 *     argv parsing, startup log, noop pre-flight, completion log) for CLI harnesses.
 */

"use strict";

const { formatDuration, resolvePostResultWatchdogIdleTimeoutMs } = require("./process_runner.cjs");
const { resolveRetryConfig: resolveSharedRetryConfig } = require("./harness_retry_config.cjs");
const { buildSoftTimeoutGuard, detectNonRetryableHarnessGuard, observeInvocationBudget, parseAICreditsExceededProxyRejection } = require("./harness_retry_guard.cjs");
const { captureExecutionEvidenceConfiguration, resolveHarnessRetryTelemetryPath, runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, stripExecutionEvidenceEnv } = require("./harness_retry_runner.cjs");
const { getSafeOutputsByteOffset, hasAttemptTerminalSafeOutput } = require("./harness_safe_outputs.cjs");
const { emitInfrastructureIncomplete, hasExpectedSafeOutputs, hasNoopInSafeOutputs, hasTerminalSafeOutput } = require("./safeoutputs_cli.cjs");
const { parseMaxAICreditsExceededFromAuditLog } = require("./ai_credits_context.cjs");

/** @typedef {import("./harness_retry_runner.cjs").HarnessAttemptResult} HarnessAttemptResult */
/** @typedef {import("./harness_retry_runner.cjs").HarnessFailureDecision} HarnessFailureDecision */
/** @typedef {{ maxRetries: number, initialDelayMs: number, backoffMultiplier: number, maxDelayMs: number }} HarnessRetryConfig */
/** @typedef {NonNullable<Parameters<typeof hasAttemptTerminalSafeOutput>[2]>} TerminalSafeOutputOptions */

/**
 * @typedef {{
 *   runAttempt: (attempt: number, observeStrategy: (strategy: "restart" | "resume") => void, attemptContext: { safeOutputsByteOffset: number }) => Promise<HarnessAttemptResult>,
 *   handleFailure: (params: { attempt: number, result: HarnessAttemptResult }) => Promise<HarnessFailureDecision> | HarnessFailureDecision,
 *   getRetryMode?: (attempt: number) => string,
 *   sleepFn?: (ms: number) => Promise<void>,
 * }} HarnessRunOptions
 */

/**
 * Create the engine-agnostic retry runtime: shared retry configuration, soft deadline,
 * retry telemetry and safe-outputs offsets around {@link runHarnessRetryLoop}.
 * `softDeadlineStart: "run"` restarts the driver clock and soft deadline when `run()` enters
 * the retry loop, so pre-flight time does not count against the retry budget.
 * @param {{
 *   harnessName: string,
 *   log: (message: string) => void,
 *   env?: NodeJS.ProcessEnv,
 *   resolveRetryConfig?: (env: NodeJS.ProcessEnv, log: (message: string) => void) => HarnessRetryConfig,
 *   safeOutputsOffset?: (safeOutputsPath: string) => number,
 *   driverStartTime?: number,
 *   softDeadlineStart?: "harness" | "run",
 * }} options
 */
function createHarnessRuntime(options) {
  const { harnessName, log } = options;
  const env = options.env ?? process.env;
  const retryConfig = (options.resolveRetryConfig ?? resolveSharedRetryConfig)(env, log);
  const safeOutputsOffset = options.safeOutputsOffset ?? getSafeOutputsByteOffset;
  // The same deadline is checked between and during attempts; engines keep a reference to this guard.
  let driverStartTime = options.driverStartTime ?? Date.now();
  const softTimeoutGuard = buildSoftTimeoutGuard(driverStartTime, env);
  const restartDriverClock = () => {
    driverStartTime = Date.now();
    if (softTimeoutGuard) Object.assign(softTimeoutGuard, buildSoftTimeoutGuard(driverStartTime, env));
  };
  const safeOutputsPath = env.GH_AW_SAFE_OUTPUTS || "";

  /**
   * Byte offset of the safe-outputs file, so attempt-scoped checks only see records appended later.
   * @returns {number}
   */
  const snapshotSafeOutputs = () => (safeOutputsPath ? safeOutputsOffset(safeOutputsPath) : 0);

  /**
   * Post-result watchdog that arms once this attempt staged a terminal safe output.
   * @param {number} safeOutputsByteOffset
   * @param {TerminalSafeOutputOptions} [terminalOptions]
   */
  const postResultWatchdog = (safeOutputsByteOffset, terminalOptions = {}) =>
    safeOutputsPath
      ? {
          shouldArm: () => hasAttemptTerminalSafeOutput(safeOutputsPath, safeOutputsByteOffset, { logger: log, ...terminalOptions }),
          inactivityTimeoutMs: resolvePostResultWatchdogIdleTimeoutMs(env),
        }
      : undefined;

  /**
   * Whether the failed attempt staged a terminal safe output after its starting offset.
   * @param {HarnessAttemptResult} result
   * @param {TerminalSafeOutputOptions} [terminalOptions]
   */
  const attemptWroteTerminalSafeOutput = (result, terminalOptions = {}) => hasAttemptTerminalSafeOutput(safeOutputsPath, result.safeOutputsByteOffset ?? 0, { logger: log, ...terminalOptions });

  /**
   * Run the shared retry loop. Each attempt receives the safe-outputs offset captured
   * before it started; the offset is also recorded on the attempt result.
   * @param {HarnessRunOptions & { maxRetries?: number }} runOptions
   */
  const run = runOptions => {
    if (options.softDeadlineStart === "run") restartDriverClock();
    return runHarnessRetryLoop({
      telemetryPath: resolveHarnessRetryTelemetryPath(env),
      maxRetries: runOptions.maxRetries ?? retryConfig.maxRetries,
      initialDelayMs: retryConfig.initialDelayMs,
      backoffMultiplier: retryConfig.backoffMultiplier,
      maxDelayMs: retryConfig.maxDelayMs,
      driverStartTime,
      harnessName,
      log,
      softTimeoutGuard,
      getRetryMode: runOptions.getRetryMode,
      sleepFn: runOptions.sleepFn,
      runAttempt: async (attempt, observeStrategy) => {
        const safeOutputsByteOffset = snapshotSafeOutputs();
        const result = await runOptions.runAttempt(attempt, observeStrategy, { safeOutputsByteOffset });
        return { ...result, safeOutputsByteOffset };
      },
      handleFailure: runOptions.handleFailure,
    });
  };

  return {
    harnessName,
    log,
    env,
    retryConfig,
    safeOutputsPath,
    get driverStartTime() {
      return driverStartTime;
    },
    softTimeoutGuard,
    postResultWatchdog,
    attemptWroteTerminalSafeOutput,
    run,
  };
}

/**
 * Start a CLI engine harness: capture execution evidence, parse `<command> [args...]`,
 * create the shared runtime and log the startup configuration.
 * Returns null (after exiting with status 1) when no command was supplied.
 * @param {{
 *   engine: string,
 *   harnessName: string,
 *   log: (message: string) => void,
 *   env?: NodeJS.ProcessEnv,
 *   argv?: string[],
 *   resolveRetryConfig?: (env: NodeJS.ProcessEnv, log: (message: string) => void) => HarnessRetryConfig,
 *   safeOutputsOffset?: (safeOutputsPath: string) => number,
 *   softDeadlineStart?: "harness" | "run",
 *   exit?: (code: number) => void,
 * }} options
 */
function startEngineHarness(options) {
  const { engine, log } = options;
  const env = options.env ?? process.env;
  const exit = options.exit ?? (code => process.exit(code));
  const executionEvidence = captureExecutionEvidenceConfiguration(env);
  stripExecutionEvidenceEnv(env);
  const [, , command, ...args] = options.argv ?? process.argv;
  if (!command) {
    process.stderr.write(`${engine}-harness: Usage: node ${engine}_harness.cjs <command> [args...]\n`);
    exit(1);
    return null;
  }

  const runtime = createHarnessRuntime({ ...options, env });
  const { maxRetries, initialDelayMs, backoffMultiplier, maxDelayMs } = runtime.retryConfig;
  log(`starting: command=${command} maxRetries=${maxRetries} initialDelayMs=${initialDelayMs}` + ` backoffMultiplier=${backoffMultiplier} maxDelayMs=${maxDelayMs}` + ` nodeVersion=${process.version} platform=${process.platform}`);

  return {
    ...runtime,
    get driverStartTime() {
      return runtime.driverStartTime;
    },
    engine,
    command,
    args,
    executionEvidence,
    /**
     * Pre-flight: a noop written by a prior step means there is nothing left to do,
     * so starting the agent would be wasteful and potentially harmful.
     */
    shouldSkipForPreexistingNoop: () => shouldSkipForNoopSafeOutputs({ safeOutputsPath: runtime.safeOutputsPath, hasNoopInSafeOutputs, log, executionEvidence }),
    /**
     * Log the final exit code and total driver duration.
     * @param {number} exitCode
     * @returns {number}
     */
    finish: exitCode => {
      log(`done: exitCode=${exitCode} totalDuration=${formatDuration(Date.now() - runtime.driverStartTime)}`);
      return exitCode;
    },
  };
}

/**
 * Record the detected failure classes on the attempt result for retry telemetry.
 * @param {HarnessAttemptResult} result
 * @param {Record<string, boolean>} detections
 * @returns {string[]}
 */
function recordFailureClasses(result, detections) {
  result.failureClasses = Object.entries(detections)
    .filter(([, detected]) => detected)
    .map(([reason]) => reason);
  return result.failureClasses;
}

/**
 * Format the per-attempt failure diagnostic line. Undefined fields are omitted.
 * @param {number} attempt
 * @param {Record<string, unknown>} fields
 * @returns {string}
 */
function formatAttemptFailure(attempt, fields) {
  return (
    `attempt ${attempt + 1} failed:` +
    Object.entries(fields)
      .filter(([, value]) => value !== undefined)
      .map(([key, value]) => ` ${key}=${value}`)
      .join("")
  );
}

/**
 * Shared non-retryable guard policy: AWF API proxy guardrails, AI-credit budgets,
 * proxy blocking and the pooled LLM invocation cap. Returns a decision when a guard
 * applies, or null to continue with the engine's own failure policy.
 *
 * AI-credit exhaustion is trusted only with proxy or firewall-audit evidence and then
 * exits 0 as budget control. An invocation cap reached after the core work was staged
 * also exits 0 so a false-red verdict does not mask a completed task.
 * @param {{
 *   attempt: number,
 *   result: HarnessAttemptResult,
 *   log: (message: string) => void,
 *   safeOutputsPath: string,
 *   guardOutput?: string,
 *   isAuthenticationFailed?: boolean,
 *   vetoAICreditsSuccess?: boolean,
 *   stopOnGoalAlreadyActive?: boolean,
 *   requireTerminalSafeOutputForInvocationCap?: boolean,
 *   invocationCapIncompleteReason?: string,
 * }} params
 * @returns {Promise<HarnessFailureDecision | null>}
 */
async function resolveNonRetryableGuardDecision(params) {
  const { attempt, result, log, safeOutputsPath, isAuthenticationFailed = false } = params;
  const guard = detectNonRetryableHarnessGuard(params.guardOutput ?? result.output);
  await observeInvocationBudget({ guard, nativeTerminalRateLimitError: result.nativeTerminalRateLimitError === true, attempt, log, result });
  if (guard.apiProxyGuardRejection) {
    const reason = `AWF API proxy guardrail rejected the request: ${guard.apiProxyGuardRejection.guard}`;
    emitInfrastructureIncomplete(reason, { logger: log });
    log(`attempt ${attempt + 1}: ${reason} — not retrying (terminal proxy guard)`);
    return { action: "stop" };
  }

  const proxyAICreditsRejection = parseAICreditsExceededProxyRejection(result.output);
  if (proxyAICreditsRejection) {
    log(`attempt ${attempt + 1}: AWF API proxy rejected the request with HTTP 403 max-AI-credits (${proxyAICreditsRejection.aiCredits}/${proxyAICreditsRejection.maxAICredits}) — trusted budget-abort evidence`);
  }
  const trustedAICreditsExceeded = guard.aiCreditsExceeded && (!!proxyAICreditsRejection || parseMaxAICreditsExceededFromAuditLog());
  if (guard.aiCreditsExceeded && !trustedAICreditsExceeded) {
    log(`attempt ${attempt + 1}: AI credits marker found in CLI output without trusted firewall audit confirmation — preserving normal failure handling`);
  }
  // Some CLIs surface the proxy's budget rejection as an authentication failure (e.g. Claude Code
  // reports `error: authentication_failed` for "403 Maximum AI credits exceeded"). When the trusted
  // proxy signature is present that veto must not mask intentional budget enforcement.
  const aiCreditsBudgetEnforced = trustedAICreditsExceeded && (!isAuthenticationFailed || !!proxyAICreditsRejection) && !params.vetoAICreditsSuccess;
  const goalAlreadyActive = params.stopOnGoalAlreadyActive === true && guard.goalAlreadyActive;
  if (!aiCreditsBudgetEnforced && !guard.awfAPIProxyBlockingRequests && !goalAlreadyActive && !guard.maxRunsExceeded) return null;

  const reasons = [];
  if (aiCreditsBudgetEnforced) reasons.push("AI credits budget exceeded");
  if (guard.awfAPIProxyBlockingRequests) reasons.push("AWF API proxy is blocking requests");
  if (goalAlreadyActive) reasons.push("goal is already active for this thread (use update_goal when the current goal is complete)");
  if (guard.maxRunsExceeded) reasons.push("maximum LLM invocations exceeded");
  log(`attempt ${attempt + 1}: ${reasons.join(" and ")} — not retrying (non-retryable guard condition)`);
  if (aiCreditsBudgetEnforced) {
    log(`attempt ${attempt + 1}: AI credits budget enforced — exiting 0 (budget control, not an error)`);
    return { action: "stop", exitCode: 0 };
  }
  if (guard.maxRunsExceeded && safeOutputsPath && hasExpectedSafeOutputs(safeOutputsPath, { logger: log })) {
    if (!params.requireTerminalSafeOutputForInvocationCap || hasTerminalSafeOutput(safeOutputsPath, { excludePartialReviewComments: true, logger: log })) {
      log(`attempt ${attempt + 1}: invocation cap saturated but safe-outputs already contain expected output — suppressing terminal verdict (false-red: core work succeeded)`);
      return { action: "stop", exitCode: 0 };
    }
  }
  if (guard.maxRunsExceeded && params.invocationCapIncompleteReason) {
    emitInfrastructureIncomplete(params.invocationCapIncompleteReason, { logger: log });
  }
  return { action: "stop" };
}

/**
 * Log why a non-retried failure is final: retries exhausted, or no output was produced.
 * @param {{ attempt: number, maxRetries: number, exitCode: number, log: (message: string) => void }} params
 * @returns {HarnessFailureDecision}
 */
function stopAfterUnrecoverableFailure({ attempt, maxRetries, exitCode, log }) {
  if (attempt >= maxRetries) {
    log(`all ${maxRetries} retries exhausted — giving up (exitCode=${exitCode})`);
  } else {
    log(`attempt ${attempt + 1}: no output produced — not retrying` + ` (possible causes: binary not found, permission denied, auth failure, or silent startup crash)`);
  }
  return { action: "stop" };
}

module.exports = {
  createHarnessRuntime,
  startEngineHarness,
  recordFailureClasses,
  formatAttemptFailure,
  resolveNonRetryableGuardDecision,
  stopAfterUnrecoverableFailure,
};
