// @ts-check

"use strict";

const fs = require("fs");
const path = require("path");
const { formatDuration, sleep } = require("./process_runner.cjs");
const { emitSoftTimeoutSignal } = require("./harness_retry_guard.cjs");

const executionEvidenceEnvVars = ["GH_AW_AWF_EXECUTION_EVIDENCE_FILE", "GH_AW_AWF_EXECUTION_COMPONENT"];

/** @param {NodeJS.ProcessEnv} [env] */
function resolveHarnessRetryTelemetryPath(env = process.env) {
  const detection = env.GH_AW_PHASE === "detection";
  const directory = env.GH_AW_HARNESS_TELEMETRY_DIR || path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", detection ? "threat-detection" : "agent");
  return path.join(directory, detection ? "detection_harness_retry.json" : "harness_retry.json");
}

/**
 * @param {Record<string, string | undefined>} env
 * @returns {{ evidenceFile?: string, component?: string, runId?: string, runAttempt?: string }}
 */
function captureExecutionEvidenceConfiguration(env) {
  return {
    evidenceFile: env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE,
    component: env.GH_AW_AWF_EXECUTION_COMPONENT,
    runId: env.GITHUB_RUN_ID,
    runAttempt: env.GITHUB_RUN_ATTEMPT,
  };
}

/**
 * @template {Record<string, string | undefined>} T
 * @param {T} env
 * @returns {T}
 */
function stripExecutionEvidenceEnv(env) {
  for (const name of executionEvidenceEnvVars) {
    delete env[name];
  }
  return env;
}

/**
 * Observe explicit continuation selectors without retaining their values.
 * @param {string[]} args
 * @param {"claude" | "codex" | "copilot"} engine
 * @returns {"restart" | "resume"}
 */
function selectedHarnessStrategy(args, engine) {
  const valueFlags = new Set(["--prompt", "-p", "--prompt-file", "--model", "-m", "--settings", "--session-id", "--config", "--cd", "-C", "--add-dir", "--allowedTools", "--disallowedTools", ...(engine === "codex" ? ["-c"] : [])]);
  let codexExec = false;
  for (let index = 0; index < args.length; index++) {
    const arg = args[index];
    if (arg === "--") break;
    if (valueFlags.has(arg)) {
      index++;
      continue;
    }
    if (arg === "--continue" || arg === "--resume" || arg.startsWith("--resume=") || (engine !== "codex" && arg === "-r") || (engine === "claude" && arg === "-c")) return "resume";
    if (engine === "codex") {
      if (arg === "exec") codexExec = true;
      else if (codexExec && !arg.startsWith("-")) return arg === "resume" ? "resume" : "restart";
    }
  }
  return "restart";
}

/**
 * `nextDelayMs` overrides the delay before the immediately next attempt; following retries
 * resume exponential backoff from that delay.
 * @typedef {{ exitCode: number, output: string, hasOutput: boolean, durationMs?: number, watchdogFired?: boolean, runtimeGuardFired?: boolean, runtimeGuardReason?: string, safeOutputsByteOffset?: number, cancelled?: boolean, failureClasses?: string[], strategy?: "restart" | "resume" }} HarnessAttemptResult
 * @typedef {{ action: "retry" | "stop", exitCode?: number, nextDelayMs?: number }} HarnessFailureDecision
 */

/**
 * Return true when the harness should skip spawning an agent because a prior step
 * already wrote a noop safe-output.
 * @param {{ safeOutputsPath: string, hasNoopInSafeOutputs: (path: string, options: { logger: (message: string) => void }) => boolean, log: (message: string) => void, executionEvidence?: ReturnType<typeof captureExecutionEvidenceConfiguration> }} options
 * @returns {boolean}
 */
function shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log, executionEvidence }) {
  if (!safeOutputsPath || !hasNoopInSafeOutputs(safeOutputsPath, { logger: log })) {
    return false;
  }
  log("pre-flight: noop message found in safe-outputs — skipping agent (work is already complete or no work needed)");
  const evidenceFile = executionEvidence?.evidenceFile;
  const component = executionEvidence?.component;
  const runId = executionEvidence?.runId;
  const runAttempt = executionEvidence?.runAttempt;
  if (
    evidenceFile &&
    path.isAbsolute(evidenceFile) &&
    typeof component === "string" &&
    /^[a-z][a-z0-9_-]{0,63}$/.test(component) &&
    /^\d+$/.test(runId || "") &&
    /^\d+$/.test(runAttempt || "") &&
    Number.isSafeInteger(Number(runId)) &&
    Number(runId) > 0 &&
    Number.isSafeInteger(Number(runAttempt)) &&
    Number(runAttempt) > 0
  ) {
    try {
      // Only this pre-flight branch proves no model ran; a noop after an attempt does not.
      fs.mkdirSync(path.dirname(evidenceFile), { recursive: true });
      fs.writeFileSync(`${evidenceFile}.tmp`, JSON.stringify({ version: 1, component, run_id: Number(runId), run_attempt: Number(runAttempt), state: "not_started" }) + "\n");
      fs.renameSync(`${evidenceFile}.tmp`, evidenceFile);
      log(`pre-flight: recorded ${component} execution evidence as not_started`);
    } catch (error) {
      log(`pre-flight: could not record zero-use execution evidence: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return true;
}

function runPreflightNoopCheck() {
  const { hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");
  return shouldSkipForNoopSafeOutputs({
    safeOutputsPath: process.env.GH_AW_SAFE_OUTPUTS || "",
    hasNoopInSafeOutputs,
    log: message => process.stderr.write(`${message}\n`),
    executionEvidence: captureExecutionEvidenceConfiguration(process.env),
  });
}

if (require.main === module && process.argv[2] === "--preflight-noop") {
  process.exitCode = runPreflightNoopCheck() ? 0 : 1;
}

/**
 * Host-only startup lifecycle updates. Never forward execution-evidence configuration
 * or engine output through this advisory telemetry channel.
 * @param {string} event
 * @param {number} attempt
 * @param {number} value Backoff before a start, or the observed exit code on finish.
 * @param {boolean} startupFailure
 */
function recordStartupAttempt(event, attempt, value, startupFailure = false) {
  try {
    if (!["start", "finish"].includes(event) || !Number.isSafeInteger(attempt) || attempt < 1 || !Number.isSafeInteger(value) || value < 0) return;
    const telemetryPath = resolveHarnessRetryTelemetryPath();
    /** @type {{ attempts: number, exit_code: number, attempt_results: unknown[], startup_attempt_results: Record<string, any>[] }} */
    let telemetry = { attempts: 0, exit_code: 1, attempt_results: [], startup_attempt_results: [] };
    if (event !== "start" || attempt !== 1) {
      try {
        telemetry = JSON.parse(fs.readFileSync(telemetryPath, "utf8"));
      } catch {
        // Missing telemetry must not prevent startup or retry.
      }
    }
    if (!Array.isArray(telemetry.startup_attempt_results)) telemetry.startup_attempt_results = [];
    if (event === "start") {
      telemetry.startup_attempt_results.push({
        attempt,
        attempt_id: `startup:${attempt}`,
        started_at: Date.now(),
        finished_at: null,
        strategy: "restart",
        backoff_ms: value,
        outcome: "running",
        exit_code: null,
        failure_classes: [],
      });
    } else {
      const current = telemetry.startup_attempt_results.find(result => result.attempt === attempt);
      if (!current) return;
      current.finished_at = Date.now();
      current.exit_code = value;
      current.outcome = value === 0 ? "success" : [130, 143].includes(value) ? "cancelled" : "failure";
      current.failure_classes = startupFailure ? ["startup_error"] : [];
    }
    fs.mkdirSync(path.dirname(telemetryPath), { recursive: true });
    fs.writeFileSync(`${telemetryPath}.tmp`, JSON.stringify(telemetry) + "\n");
    fs.renameSync(`${telemetryPath}.tmp`, telemetryPath);
  } catch {
    // Startup telemetry is advisory, including malformed or unwritable records.
  }
}

if (require.main === module && process.argv[2] === "--startup-attempt") {
  recordStartupAttempt(process.argv[3], Number(process.argv[4]), Number(process.argv[5]), process.argv[6] === "startup_error");
}

/**
 * Return true when a failed attempt wrote a noop safe-output, making retries wasteful.
 * @param {{ attempt: number, safeOutputsPath: string, hasNoopInSafeOutputs: (path: string, options: { logger: (message: string) => void }) => boolean, log: (message: string) => void }} options
 * @returns {boolean}
 */
function shouldStopForNoopSafeOutputs({ attempt, safeOutputsPath, hasNoopInSafeOutputs, log }) {
  if (!safeOutputsPath || !hasNoopInSafeOutputs(safeOutputsPath, { logger: log })) {
    return false;
  }
  log(`attempt ${attempt + 1}: noop message found in safe-outputs — not retrying (work is already complete or no work needed)`);
  return true;
}

/**
 * Shared retry loop for engine harnesses. Engine-specific argument selection,
 * execution, and failure classification remain callbacks; this centralizes the
 * attempt/backoff/soft-timeout/success state machine.
 * @param {{
 *   maxRetries: number,
 *   initialDelayMs: number,
 *   backoffMultiplier: number,
 *   maxDelayMs: number,
 *   driverStartTime: number,
 *   harnessName: string,
 *   log: (message: string) => void,
 *   softTimeoutGuard: { timeoutMinutes: number, softDeadlineMs: number } | null,
 *   runAttempt: (attempt: number, observeStrategy: (strategy: "restart" | "resume") => void) => Promise<HarnessAttemptResult>,
 *   handleFailure: (context: { attempt: number, result: HarnessAttemptResult, maxRetries: number }) => Promise<HarnessFailureDecision> | HarnessFailureDecision,
 *   getRetryMode?: (attempt: number) => string,
 *   sleepFn?: (ms: number) => Promise<void>,
 *   telemetryPath?: string,
 * }} options
 * @returns {Promise<{ exitCode: number, attempts: number, lastResult: HarnessAttemptResult | null }>}
 */
async function runHarnessRetryLoop(options) {
  let delay = options.initialDelayMs;
  let lastExitCode = 1;
  let attempts = 0;
  /** @type {HarnessAttemptResult | null} */
  let lastResult = null;
  const sleepFor = options.sleepFn ?? sleep;
  let startupAttemptId;
  if (options.telemetryPath) {
    try {
      const startup = JSON.parse(fs.readFileSync(options.telemetryPath, "utf8")).startup_attempt_results?.at(-1);
      if (startup?.outcome === "running" && Number.isSafeInteger(startup.attempt) && startup.attempt > 0) startupAttemptId = `startup:${startup.attempt}`;
    } catch {
      // Standalone harnesses have no host startup scope.
    }
  }
  /** @type {{ attempt: number, attempt_id: string, startup_attempt_id?: string, started_at: number, finished_at: number | null, strategy: "restart" | "resume", backoff_ms: number, outcome: string, exit_code: number | null, failure_classes: string[] }[]} */
  const attemptResults = [];
  /** @param {"start" | "finish"} event @param {(typeof attemptResults)[number]} result */
  const writeAttemptMarker = (event, result) => {
    try {
      // Use stdout: direct execution may capture stdout only. Never copy raw child
      // output to new files; the aggregate log's existing redaction remains authoritative.
      process.stdout.write(`\n[gh-aw-harness-attempt] ${JSON.stringify({ event, attempt_id: result.attempt_id, attempt: result.attempt, ...(result.startup_attempt_id ? { startup_attempt_id: result.startup_attempt_id } : {}) })}\n`);
    } catch {
      // Boundary markers are advisory and must not affect retry decisions.
    }
  };
  const writeTelemetry = () => {
    if (!options.telemetryPath) return;
    try {
      fs.mkdirSync(path.dirname(options.telemetryPath), { recursive: true });
      let startupAttempts;
      try {
        startupAttempts = JSON.parse(fs.readFileSync(options.telemetryPath, "utf8")).startup_attempt_results;
      } catch {
        // A standalone harness has no host startup telemetry.
      }
      fs.writeFileSync(`${options.telemetryPath}.tmp`, JSON.stringify({ attempts, exit_code: lastExitCode, attempt_results: attemptResults, ...(Array.isArray(startupAttempts) ? { startup_attempt_results: startupAttempts } : {}) }) + "\n");
      fs.renameSync(`${options.telemetryPath}.tmp`, options.telemetryPath);
    } catch (error) {
      try {
        options.log(`could not record harness retry telemetry: ${error instanceof Error ? error.message : String(error)}`);
      } catch {
        // Telemetry diagnostics must not change the harness outcome.
      }
    }
  };
  writeTelemetry();

  try {
    for (let attempt = 0; attempt <= options.maxRetries; attempt++) {
      if (options.softTimeoutGuard && Date.now() >= options.softTimeoutGuard.softDeadlineMs) {
        emitSoftTimeoutSignal(options.softTimeoutGuard, `before attempt ${attempt + 1}`, options.harnessName, options.log);
        lastExitCode = 1;
        break;
      }

      let backoffMs = 0;
      /** @type {"restart" | "resume"} */
      let strategy = "restart";
      if (attempt > 0) {
        const retryMode = options.getRetryMode ? options.getRetryMode(attempt) : "fresh run";
        strategy = /(?:resume|continue)/.test(retryMode) ? "resume" : "restart";
        const sleepDelay = options.softTimeoutGuard ? Math.min(delay, Math.max(0, options.softTimeoutGuard.softDeadlineMs - Date.now())) : delay;
        backoffMs = sleepDelay;
        options.log(`retry ${attempt}/${options.maxRetries}: sleeping ${sleepDelay}ms before next attempt (${retryMode})`);
        await sleepFor(sleepDelay);
        delay = Math.min(delay * options.backoffMultiplier, options.maxDelayMs);
        options.log(`retry ${attempt}/${options.maxRetries}: woke up, next delay will be ${delay}ms`);
        if (options.softTimeoutGuard && Date.now() >= options.softTimeoutGuard.softDeadlineMs) {
          emitSoftTimeoutSignal(options.softTimeoutGuard, "after backoff sleep", options.harnessName, options.log);
          lastExitCode = 1;
          break;
        }
      }

      attempts = attempt + 1;
      /** @type {(typeof attemptResults)[number]} */
      const attemptResult = {
        attempt: attempts,
        attempt_id: `harness:${attempts}`,
        ...(startupAttemptId ? { startup_attempt_id: startupAttemptId } : {}),
        started_at: Date.now(),
        finished_at: null,
        strategy,
        backoff_ms: backoffMs,
        outcome: "running",
        exit_code: null,
        failure_classes: [],
      };
      attemptResults.push(attemptResult);
      writeTelemetry();
      writeAttemptMarker("start", attemptResult);
      let result;
      try {
        result = await options.runAttempt(attempt, selected => {
          if (selected === "restart" || selected === "resume") {
            attemptResult.strategy = selected;
            writeTelemetry();
          }
        });
      } catch (error) {
        attemptResult.finished_at = Date.now();
        attemptResult.outcome = "interrupted";
        writeAttemptMarker("finish", attemptResult);
        throw error;
      }
      lastResult = result;
      lastExitCode = result.exitCode;
      attemptResult.exit_code = result.exitCode;
      attemptResult.finished_at = Date.now();
      attemptResult.outcome = result.cancelled ? "cancelled" : result.exitCode === 0 ? "success" : "failure";
      if (result.strategy === "restart" || result.strategy === "resume") attemptResult.strategy = result.strategy;
      if (result.exitCode !== 0) {
        attemptResult.failure_classes = [result.cancelled ? "cancelled" : result.runtimeGuardFired ? "runtime_guard" : result.watchdogFired ? "watchdog" : result.hasOutput ? "unknown_error" : "no_output"];
      }
      writeAttemptMarker("finish", attemptResult);
      writeTelemetry();

      if (result.cancelled) {
        options.log(`attempt ${attempt + 1}: cancelled — not retrying`);
        break;
      }

      if (result.exitCode === 0) {
        options.log(`success on attempt ${attempt + 1}: totalDuration=${formatDuration(Date.now() - options.driverStartTime)}`);
        lastExitCode = 0;
        break;
      }

      const decision = await options.handleFailure({ attempt, result, maxRetries: options.maxRetries });
      if (result.failureClasses?.length) {
        attemptResult.failure_classes = result.failureClasses;
      }
      if (typeof decision.exitCode === "number") {
        lastExitCode = decision.exitCode;
      }
      if (typeof decision.nextDelayMs === "number") {
        delay = decision.nextDelayMs;
      }
      writeTelemetry();
      if (decision.action === "retry") {
        continue;
      }
      break;
    }
  } finally {
    writeTelemetry();
  }

  return { exitCode: lastExitCode, attempts, lastResult };
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    captureExecutionEvidenceConfiguration,
    resolveHarnessRetryTelemetryPath,
    recordStartupAttempt,
    runHarnessRetryLoop,
    runPreflightNoopCheck,
    shouldSkipForNoopSafeOutputs,
    shouldStopForNoopSafeOutputs,
    stripExecutionEvidenceEnv,
    selectedHarnessStrategy,
  };
}
