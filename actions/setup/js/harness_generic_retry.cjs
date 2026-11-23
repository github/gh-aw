// @ts-check

"use strict";

const { parseCopilotRetryAfterMs } = require("./copilot_retry_after.cjs");
const { detectNonRetryableHarnessGuard, isAuthenticationFailedError, isHTMLResponseError } = require("./harness_error_patterns.cjs");
const { resolveRetryConfig } = require("./harness_retry_config.cjs");
const { buildSoftTimeoutGuard } = require("./harness_retry_guard.cjs");
const { resolveHarnessRetryTelemetryPath, runHarnessRetryLoop, shouldStopForNoopSafeOutputs } = require("./harness_retry_runner.cjs");
const { getSafeOutputsByteOffset } = require("./harness_safe_outputs.cjs");
const { hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");

/** Bounded tail of combined attempt output retained for failure classification. */
const GENERIC_HARNESS_OUTPUT_TAIL_BYTES = 256 * 1024;

/**
 * Retryable signals (rate limits, transient errors, Retry-After) are only read from
 * the end of the output, where provider/CLI terminal errors appear, so earlier
 * model-generated transcript text cannot steer a retry.
 */
const GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS = 8 * 1024;

const RATE_LIMIT_PATTERN =
  /\b(?:HTTP(?:\/\d(?:\.\d)?)?|status(?:Code|_code)?|code|error)["']?\s*[:=]?\s*["']?429\b|\b429\s+Too Many Requests\b|\btoo many requests\b|\brate[ _-]?limit(?:ed|_error|_exceeded|\s+exceeded|\s+reached)\b|\bRESOURCE_EXHAUSTED\b/i;
const TRANSIENT_ERROR_PATTERN =
  /\b(?:HTTP(?:\/\d(?:\.\d)?)?|status(?:Code|_code)?)["']?\s*[:=]?\s*["']?(?:500|502|503|504)\b|\b(?:502 Bad Gateway|503 Service Unavailable|504 Gateway Timeout)\b|\b(?:ECONNRESET|ETIMEDOUT|EAI_AGAIN|ECONNREFUSED)\b|\bsocket hang up\b|\boverloaded(?:_error)?\b|["']status["']\s*:\s*["']UNAVAILABLE["']/i;

/**
 * @param {string} output
 * @returns {string}
 */
function retrySignalTail(output) {
  return output.length > GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS ? output.slice(-GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS) : output;
}

/**
 * Classify a failed attempt for harnesses that do not own an engine-specific policy.
 * Non-retryable guards are checked across the whole output; only provider rate limits
 * and transient transport/server errors in the output tail are retryable.
 * @param {string} output
 * @returns {"rate_limit_error" | "transient_error" | "non_retryable" | null}
 */
function classifyGenericHarnessFailure(output) {
  const text = typeof output === "string" ? output : "";
  const guard = detectNonRetryableHarnessGuard(text);
  if (guard.aiCreditsExceeded || guard.awfAPIProxyBlockingRequests || guard.goalAlreadyActive || guard.maxRunsExceeded || guard.modelEndpointMismatch || guard.apiProxyGuardRejection || isAuthenticationFailedError(text)) {
    return "non_retryable";
  }
  const tail = retrySignalTail(text);
  if (RATE_LIMIT_PATTERN.test(tail)) return "rate_limit_error";
  if (TRANSIENT_ERROR_PATTERN.test(tail) || isHTMLResponseError(tail)) return "transient_error";
  return null;
}

/**
 * Harness-agnostic retry entry point. The harness supplies one fresh or resumable
 * attempt; this resolves the shared retry configuration, soft timeout, telemetry,
 * Retry-After handling and a conservative default failure policy.
 *
 * A retry is never attempted after the agent appended safe outputs, because a
 * re-run could duplicate externally visible side effects. Without a safe-outputs
 * path those side effects cannot be observed, so retries are disabled.
 * @param {{
 *   harnessName: string,
 *   log: (message: string) => void,
 *   runAttempt: (attempt: number) => Promise<import("./harness_retry_runner.cjs").HarnessAttemptResult>,
 *   getRetryMode?: (attempt: number) => string,
 *   env?: NodeJS.ProcessEnv,
 *   sleepFn?: (ms: number) => Promise<void>,
 * }} options
 */
async function runRetryingHarness(options) {
  const env = options.env ?? process.env;
  const { harnessName, log } = options;
  const { maxRetries: configuredMaxRetries, initialDelayMs, backoffMultiplier, maxDelayMs } = resolveRetryConfig(env, log);
  const driverStartTime = Date.now();
  const safeOutputsPath = env.GH_AW_SAFE_OUTPUTS || "";
  const maxRetries = safeOutputsPath ? configuredMaxRetries : 0;
  if (!safeOutputsPath && configuredMaxRetries > 0) {
    log("GH_AW_SAFE_OUTPUTS is not set — retries disabled because safe-output side effects cannot be observed");
  }
  let safeOutputsByteOffset = 0;
  return runHarnessRetryLoop({
    telemetryPath: resolveHarnessRetryTelemetryPath(env),
    maxRetries,
    initialDelayMs,
    backoffMultiplier,
    maxDelayMs,
    driverStartTime,
    harnessName,
    log,
    softTimeoutGuard: buildSoftTimeoutGuard(driverStartTime, env),
    getRetryMode: options.getRetryMode,
    sleepFn: options.sleepFn,
    runAttempt: async attempt => {
      safeOutputsByteOffset = safeOutputsPath ? getSafeOutputsByteOffset(safeOutputsPath) : 0;
      const result = await options.runAttempt(attempt);
      return { ...result, safeOutputsByteOffset };
    },
    handleFailure: ({ attempt, result }) => {
      const failureClass = classifyGenericHarnessFailure(result.output);
      log(`attempt ${attempt + 1}: failed with exitCode=${result.exitCode} failureClass=${failureClass ?? "unclassified"}`);
      if (shouldStopForNoopSafeOutputs({ attempt, safeOutputsPath, hasNoopInSafeOutputs, log })) return { action: "stop" };
      if (safeOutputsPath) {
        const currentOffset = getSafeOutputsByteOffset(safeOutputsPath);
        if (!Number.isSafeInteger(safeOutputsByteOffset) || !Number.isSafeInteger(currentOffset) || currentOffset > safeOutputsByteOffset) {
          log(`attempt ${attempt + 1}: safe outputs were recorded or could not be inspected — not retrying to avoid duplicate side effects`);
          return { action: "stop" };
        }
      }
      if (failureClass !== "rate_limit_error" && failureClass !== "transient_error") {
        if (failureClass) result.failureClasses = [failureClass];
        return { action: "stop" };
      }
      result.failureClasses = [failureClass];
      if (failureClass === "rate_limit_error" && result.retryAfterMs == null) {
        result.retryAfterMs = parseCopilotRetryAfterMs(retrySignalTail(result.output));
      }
      if (attempt >= maxRetries) {
        log(`attempt ${attempt + 1}: ${failureClass} — all ${maxRetries} retries exhausted`);
        return { action: "stop" };
      }
      return { action: "retry" };
    },
  });
}

module.exports = { GENERIC_HARNESS_OUTPUT_TAIL_BYTES, GENERIC_HARNESS_RETRY_SIGNAL_TAIL_CHARS, classifyGenericHarnessFailure, runRetryingHarness };
