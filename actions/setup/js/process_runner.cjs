// @ts-check

/**
 * Shared process runner utilities for agent harnesses.
 *
 * Provides a common runProcess helper used by both the Claude and Copilot
 * harnesses to spawn child processes, forward stdout/stderr (stdin is closed;
 * prompts are delivered via file/argument, not stdin), collect output for
 * retry decisions, track byte counts, and surface spawn errors.
 *
 * Each harness retains its own logging prefix and argument-redaction logic;
 * the caller passes a log function and an optional logArgs array so that
 * sensitive values (e.g. prompt text) are never written to logs.
 */

"use strict";

const { spawn } = require("child_process");
const os = require("os");

/**
 * Convert a Node.js child-process termination signal (e.g. "SIGSYS") into the
 * conventional shell-style exit code (128 + signal number), matching the value
 * the OS would report if the process had exited with that status directly.
 * Node only reports a non-null `signal` when the process was killed by a signal
 * (in which case `code` is null), so the caller must synthesize an exit code to
 * preserve the fatal-signal information for downstream classification (see
 * harness_crash_signals.cjs). Returns null when the signal name is unrecognized.
 * @param {NodeJS.Signals | null} signal
 * @returns {number | null}
 */
function exitCodeForSignal(signal) {
  if (!signal) return null;
  const signalNumber = os.constants.signals[signal];
  return typeof signalNumber === "number" ? 128 + signalNumber : null;
}

/**
 * Format elapsed milliseconds as a human-readable string (e.g. "3m 12s").
 * @param {number} ms
 * @returns {string}
 */
function formatDuration(ms) {
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

/**
 * Sleep for a specified duration.
 * @param {number} ms - Duration in milliseconds
 * @returns {Promise<void>}
 */
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Run a command with the given arguments, forwarding stdout/stderr (stdin is closed;
 * see the spawn call below for why).
 * Also collects combined stdout+stderr output for error pattern detection.
 *
 * The child process is spawned with `cwd` set to `process.env.GH_AW_ENGINE_CWD` when
 * available, falling back to `process.env.GITHUB_WORKSPACE`, so that engines and their
 * skill-discovery paths resolve relative to the configured or repository checkout root
 * rather than the harness working directory.
 *
 * @param {{
 *   command: string,
 *   args: string[],
 *   attempt: number,
 *   log: (message: string) => void,
 *   logArgs?: string[],
 *   env?: NodeJS.ProcessEnv,
 *   postResultWatchdog?: {
 *     shouldArm: () => boolean,
 *     inactivityTimeoutMs: number,
 *     pollIntervalMs?: number,
 *     termGraceMs?: number
 *   },
 *   runtimeGuard?: {
 *     shouldTerminate: () => boolean | { terminate: boolean, reason?: string } | Promise<boolean | { terminate: boolean, reason?: string }>,
 *     pollIntervalMs?: number,
 *     termGraceMs?: number
 *   },
 *   stallWarningIntervalMs?: number
 * }} options
 *   - command   - The executable to run
 *   - args      - Arguments to pass to the command
 *   - attempt   - Current attempt index (0-based), used for logging
 *   - log       - Caller-supplied logging function (harness-specific prefix)
 *   - logArgs   - Safe arg list used only for logging; defaults to `args`.
 *                 Pass a redacted copy to avoid leaking sensitive values.
 *   - stallWarningIntervalMs - Interval of child-process silence after which the
 *                 driver logs a stall warning. Defaults to the value resolved from
 *                 GH_AW_HARNESS_STALL_WARNING_MS; 0 disables the warnings. An explicit
 *                 caller value is used as-is (not clamped to the environment range) so
 *                 tests can use short intervals.
 * @returns {Promise<{exitCode: number, output: string, hasOutput: boolean, durationMs: number, watchdogFired: boolean, runtimeGuardFired: boolean, runtimeGuardReason: string}>}
 */
function runProcess({ command, args, attempt, log, logArgs, env, postResultWatchdog, runtimeGuard, stallWarningIntervalMs }) {
  return new Promise(resolve => {
    const startTime = Date.now();
    // Guard against the promise being settled more than once.  On some systems Node
    // emits 'close' after 'error' (or vice-versa); only the first terminal event should
    // log and resolve so callers receive a deterministic result.
    let settled = false;
    /** @param {{exitCode: number, output: string, hasOutput: boolean, durationMs: number, watchdogFired: boolean, runtimeGuardFired: boolean, runtimeGuardReason: string}} result */
    function settle(result) {
      if (settled) return;
      settled = true;
      if (postResultWatchdogTimer) clearInterval(postResultWatchdogTimer);
      if (runtimeGuardTimer) clearInterval(runtimeGuardTimer);
      if (runtimeGuardKillTimer) clearTimeout(runtimeGuardKillTimer);
      if (stallWatchdogTimer) clearInterval(stallWatchdogTimer);
      resolve(result);
    }

    const argsForLog = logArgs ?? args;
    log(`attempt ${attempt + 1}: spawning: ${command} ${argsForLog.join(" ").substring(0, 200)}`);

    // stdin is closed (not inherited) rather than forwarded: every harness delivers the
    // prompt via a file or CLI argument, so the child never needs to read from stdin.
    // Closing it means a CLI that unexpectedly falls back to an interactive
    // "read from stdin" mode (e.g. due to an unrecognized argument) sees immediate EOF
    // and exits/errors quickly instead of hanging silently until the step's
    // timeout-minutes budget is exhausted.
    const child = spawn(command, args, {
      stdio: ["ignore", "pipe", "pipe"],
      env: env ?? process.env,
      cwd: process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || undefined,
    });

    log(`attempt ${attempt + 1}: process started (pid=${child.pid ?? "unknown"})`);

    let collectedOutput = "";
    let hasOutput = false;
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let lastActivityAt = Date.now();
    let watchdogArmed = false;
    let runtimeGuardFired = false;
    let runtimeGuardReason = "";
    // Each termination source tracks its own SIGTERM/SIGKILL timestamps so the two
    // grace periods never interfere with one another.
    let watchdogSentSigtermAt = 0;
    let watchdogSentSigkillAt = 0;
    let guardSentSigtermAt = 0;
    let guardSentSigkillAt = 0;
    const watchdogPollIntervalMs = Math.max(50, Number(postResultWatchdog?.pollIntervalMs) || 1000);
    const watchdogTermGraceMs = Math.max(50, Number(postResultWatchdog?.termGraceMs) || 5000);
    const runtimeGuardPollIntervalMs = Math.max(50, Number(runtimeGuard?.pollIntervalMs) || 1000);
    const runtimeGuardTermGraceMs = Math.max(50, Number(runtimeGuard?.termGraceMs) || 5000);
    const rawInactivityTimeout = Number(postResultWatchdog?.inactivityTimeoutMs);
    const watchdogInactivityTimeoutMs = Number.isFinite(rawInactivityTimeout) && rawInactivityTimeout > 0 ? Math.max(50, rawInactivityTimeout) : 0;
    /** @type {NodeJS.Timeout | null} */
    let postResultWatchdogTimer = null;
    /** @type {NodeJS.Timeout | null} */
    let runtimeGuardTimer = null;
    /** @type {NodeJS.Timeout | null} */
    let runtimeGuardKillTimer = null;
    /** @type {NodeJS.Timeout | null} */
    let stallWatchdogTimer = null;
    const stallIntervalMs = Number.isFinite(Number(stallWarningIntervalMs)) ? Math.max(0, Number(stallWarningIntervalMs)) : resolveStallWarningIntervalMs(env ?? process.env);
    let stallWarnings = 0;
    let stalledSinceMs = 0;

    // Record child-process output activity. When the driver previously reported a
    // stall, log an explicit recovery line so the step log distinguishes "was hung,
    // then resumed" from "still silent".
    function recordActivity() {
      lastActivityAt = Date.now();
      if (stalledSinceMs > 0) {
        log(`attempt ${attempt + 1}: stall watchdog: output resumed after ${formatDuration(Date.now() - stalledSinceMs)} of silence`);
        stalledSinceMs = 0;
      }
    }

    child.stdout.on(
      "data",
      /** @param {Buffer} data */ data => {
        hasOutput = true;
        stdoutBytes += data.length;
        collectedOutput += data.toString();
        recordActivity();
        process.stdout.write(data);
      }
    );

    child.stderr.on(
      "data",
      /** @param {Buffer} data */ data => {
        hasOutput = true;
        stderrBytes += data.length;
        collectedOutput += data.toString();
        recordActivity();
        process.stderr.write(data);
      }
    );

    // Driver-level stall watchdog: a hung agent CLI leaves the "Execute ... CLI" step
    // in_progress with no log output at all until GitHub Actions cancels the step at
    // timeout-minutes, which is indistinguishable from a slow-but-healthy run without
    // cross-referencing job/step metadata. Emitting a periodic, greppable warning makes
    // the hang self-diagnosable from the step log alone.
    if (stallIntervalMs > 0) {
      stallWatchdogTimer = setInterval(() => {
        if (settled) return;
        const idleMs = Date.now() - lastActivityAt;
        if (idleMs < stallIntervalMs) return;
        if (stalledSinceMs === 0) stalledSinceMs = lastActivityAt;
        stallWarnings++;
        log(
          `attempt ${attempt + 1}: stall watchdog: no output from '${command}' for ${formatDuration(idleMs)}` +
            ` (elapsed=${formatDuration(Date.now() - startTime)} pid=${child.pid ?? "unknown"} warnings=${stallWarnings})` +
            ` - the step may be hung${formatStepTimeoutBudget(startTime, env ?? process.env)}`
        );
      }, stallIntervalMs);
    }

    if (postResultWatchdog && watchdogInactivityTimeoutMs > 0) {
      postResultWatchdogTimer = setInterval(() => {
        if (settled) return;
        if (!watchdogArmed) {
          try {
            watchdogArmed = postResultWatchdog.shouldArm();
          } catch {
            watchdogArmed = false;
          }
          if (watchdogArmed) {
            lastActivityAt = Date.now();
            log(`attempt ${attempt + 1}: post-result watchdog armed inactivityTimeout=${watchdogInactivityTimeoutMs}ms`);
          }
        }
        if (!watchdogArmed) return;
        const idleMs = Date.now() - lastActivityAt;
        if (watchdogSentSigtermAt === 0 && idleMs >= watchdogInactivityTimeoutMs) {
          watchdogSentSigtermAt = Date.now();
          log(`attempt ${attempt + 1}: post-result watchdog terminating idle process after ${idleMs}ms (SIGTERM)`);
          child.kill("SIGTERM");
          return;
        }
        if (watchdogSentSigtermAt > 0 && watchdogSentSigkillAt === 0 && Date.now() - watchdogSentSigtermAt >= watchdogTermGraceMs) {
          watchdogSentSigkillAt = Date.now();
          log(`attempt ${attempt + 1}: post-result watchdog forcing process exit after ${watchdogTermGraceMs}ms grace (SIGKILL)`);
          child.kill("SIGKILL");
        }
      }, watchdogPollIntervalMs);
    }

    if (runtimeGuard && typeof runtimeGuard.shouldTerminate === "function") {
      // SIGKILL escalation uses a dedicated timeout rather than the poll interval so the
      // grace period is honoured exactly, even when the guard polls infrequently.
      const escalateToSigkill = () => {
        runtimeGuardKillTimer = null;
        if (settled || guardSentSigkillAt > 0) return;
        guardSentSigkillAt = Date.now();
        log(`attempt ${attempt + 1}: runtime guard forcing process exit after ${runtimeGuardTermGraceMs}ms grace (SIGKILL)`);
        child.kill("SIGKILL");
      };
      // Guards against overlapping polls when `shouldTerminate` is asynchronous.
      let guardCheckInFlight = false;
      runtimeGuardTimer = setInterval(async () => {
        if (settled || guardCheckInFlight) return;
        if (runtimeGuardFired) return;
        guardCheckInFlight = true;
        /** @type {boolean | { terminate: boolean, reason?: string }} */
        let decision = false;
        try {
          decision = await runtimeGuard.shouldTerminate();
        } catch {
          decision = false;
        } finally {
          guardCheckInFlight = false;
        }
        if (settled || runtimeGuardFired) return;
        const terminate = typeof decision === "boolean" ? decision : !!decision && decision.terminate === true;
        if (!terminate) return;
        runtimeGuardFired = true;
        runtimeGuardReason = typeof decision === "object" && decision !== null && typeof decision.reason === "string" ? decision.reason : "";
        const reasonSuffix = runtimeGuardReason ? ` (${runtimeGuardReason})` : "";
        guardSentSigtermAt = Date.now();
        log(`attempt ${attempt + 1}: runtime guard requested termination${reasonSuffix} (SIGTERM)`);
        child.kill("SIGTERM");
        if (runtimeGuardTimer) {
          clearInterval(runtimeGuardTimer);
          runtimeGuardTimer = null;
        }
        runtimeGuardKillTimer = setTimeout(escalateToSigkill, runtimeGuardTermGraceMs);
      }, runtimeGuardPollIntervalMs);
    }

    child.on("exit", (code, signal) => {
      log(`attempt ${attempt + 1}: process exit event` + ` exitCode=${code ?? exitCodeForSignal(signal) ?? 1}` + (signal ? ` signal=${signal}` : ""));
    });

    // Resolve on 'close', not 'exit', to ensure stdio streams are fully drained.
    child.on("close", (code, signal) => {
      const durationMs = Date.now() - startTime;
      // When the process is killed by a signal, Node reports code=null and a signal
      // name instead. Synthesize the conventional 128+signal exit code so fatal-signal
      // crashes (e.g. SIGSYS) are visible to exit-code-based retry classification even
      // when the shell/runtime never reports a raw numeric exit status.
      const exitCode = code ?? exitCodeForSignal(signal) ?? 1;
      const watchdogFired = watchdogSentSigtermAt > 0;
      log(
        `attempt ${attempt + 1}: process closed` +
          ` exitCode=${exitCode}` +
          (signal ? ` signal=${signal}` : "") +
          ` duration=${formatDuration(durationMs)}` +
          ` stdout=${stdoutBytes}B stderr=${stderrBytes}B hasOutput=${hasOutput}` +
          (watchdogFired ? ` watchdogFired=true` : "") +
          (runtimeGuardFired ? ` runtimeGuardFired=true` : "") +
          (stallWarnings > 0 ? ` stallWarnings=${stallWarnings}` : "")
      );
      settle({ exitCode, output: collectedOutput, hasOutput, durationMs, watchdogFired, runtimeGuardFired, runtimeGuardReason });
    });

    child.on("error", err => {
      const durationMs = Date.now() - startTime;
      // prettier-ignore
      const errno = /** @type {NodeJS.ErrnoException} */ (err);
      const errCode = errno.code ?? "unknown";
      const errSyscall = errno.syscall ?? "unknown";
      log(`attempt ${attempt + 1}: failed to start process '${command}': ${err.message}` + ` (code=${errCode} syscall=${errSyscall})`);
      settle({
        exitCode: 1,
        output: collectedOutput,
        hasOutput,
        durationMs,
        watchdogFired: false,
        runtimeGuardFired: false,
        runtimeGuardReason: "",
      });
    });
  });
}

// Driver-level stall watchdog: how long the spawned agent CLI may stay completely
// silent before the driver logs a warning marking the step as potentially hung.
const DEFAULT_STALL_WARNING_INTERVAL_MS = 5 * 60 * 1000;
const MIN_STALL_WARNING_INTERVAL_MS = 1000;
const MAX_STALL_WARNING_INTERVAL_MS = 60 * 60 * 1000;

/**
 * Resolve the stall-warning interval from the environment.
 * Falls back to DEFAULT_STALL_WARNING_INTERVAL_MS when unset or non-numeric, and
 * returns 0 (warnings disabled) when explicitly set to a value <= 0.
 * Otherwise clamps to [MIN_STALL_WARNING_INTERVAL_MS, MAX_STALL_WARNING_INTERVAL_MS].
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {number}
 */
function resolveStallWarningIntervalMs(env = process.env) {
  const raw = env.GH_AW_HARNESS_STALL_WARNING_MS;
  if (raw === undefined || String(raw).trim() === "") {
    return DEFAULT_STALL_WARNING_INTERVAL_MS;
  }
  const configured = Number(raw);
  if (!Number.isFinite(configured)) {
    return DEFAULT_STALL_WARNING_INTERVAL_MS;
  }
  if (configured <= 0) {
    return 0;
  }
  return Math.min(MAX_STALL_WARNING_INTERVAL_MS, Math.max(MIN_STALL_WARNING_INTERVAL_MS, configured));
}

/**
 * Build the trailing "; the step timeout ... will cancel this step in ..." fragment of
 * the stall warning, based on the step timeout advertised via GH_AW_TIMEOUT_MINUTES.
 * Returns an empty string when the timeout is unknown or invalid.
 * @param {number} startTime - Timestamp (ms) when the child process was spawned
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {string}
 */
function formatStepTimeoutBudget(startTime, env = process.env) {
  const timeoutMinutes = Number(env.GH_AW_TIMEOUT_MINUTES);
  if (!Number.isFinite(timeoutMinutes) || timeoutMinutes <= 0) {
    return "";
  }
  const remainingMs = startTime + Math.floor(timeoutMinutes * 60 * 1000) - Date.now();
  if (remainingMs <= 0) {
    return `; the ${timeoutMinutes}-minute step timeout has been reached and GitHub Actions will cancel this step`;
  }
  return `; GitHub Actions will cancel this step in about ${formatDuration(remainingMs)} (timeout-minutes=${timeoutMinutes})`;
}

// Post-result watchdog: shared constants and timeout resolver used by all harnesses.
// These are kept here so both copilot_harness and codex_harness stay in sync.
const MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS = 50;
const DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS = 2 * 60 * 1000;
/** Maximum allowed value for GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS to prevent the watchdog from being
 *  effectively disabled by an excessively large override (e.g. a stray zero). */
const MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS = 10 * 60 * 1000;

/**
 * Resolve the post-result watchdog inactivity timeout from the environment.
 * Falls back to DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS when unset or invalid.
 * Clamps to [MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS, MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS].
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {number}
 */
function resolvePostResultWatchdogIdleTimeoutMs(env = process.env) {
  const configuredTimeoutMs = Number(env.GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS);
  if (!Number.isFinite(configuredTimeoutMs) || configuredTimeoutMs <= 0) {
    return DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS;
  }
  return Math.min(MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS, Math.max(MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS, configuredTimeoutMs));
}

/**
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {boolean}
 */
function isCopilotSDKEnabled(env) {
  const sourceEnv = env ?? process.env;
  return Boolean(sourceEnv.COPILOT_SDK_URI);
}

/**
 * Returns the Copilot SDK environment additions to inject into child processes
 * when SDK mode is active.
 *
 * When COPILOT_SDK_URI is set in process.env, returns an object with
 * { COPILOT_SDK_URI } so callers can merge it into their child-process env.
 * Returns an empty object when SDK mode is not active, making it safe to call
 * unconditionally.
 *
 * Intended to be shared by all engine harnesses (copilot_harness, claude_harness, …)
 * so that COPILOT_SDK_URI is forwarded consistently without duplicating the logic.
 *
 * @param {NodeJS.ProcessEnv} [env] - Source environment (defaults to process.env)
 * @returns {NodeJS.ProcessEnv}
 */
function buildCopilotSDKEnv(env) {
  const sourceEnv = env ?? process.env;
  if (!isCopilotSDKEnabled(sourceEnv)) return {};
  const uri = sourceEnv.COPILOT_SDK_URI;
  if (!uri) return {};
  /** @type {NodeJS.ProcessEnv} */
  const sdkEnv = { COPILOT_SDK_URI: uri };
  sdkEnv.COPILOT_SDK_LOG_LEVEL = sourceEnv.COPILOT_SDK_LOG_LEVEL || "all";
  if (sourceEnv.COPILOT_SDK_SEND_TIMEOUT_MS) {
    sdkEnv.COPILOT_SDK_SEND_TIMEOUT_MS = sourceEnv.COPILOT_SDK_SEND_TIMEOUT_MS;
    return sdkEnv;
  }

  const timeoutMinutes = Number(sourceEnv.GH_AW_TIMEOUT_MINUTES);
  if (!Number.isFinite(timeoutMinutes) || timeoutMinutes <= 0) {
    return sdkEnv;
  }

  // Keep SDK sendAndWait timeout below the job step timeout by 30 seconds.
  const timeoutMs = Math.max(Math.floor(timeoutMinutes * 60 * 1000) - 30 * 1000, 1000);
  sdkEnv.COPILOT_SDK_SEND_TIMEOUT_MS = String(timeoutMs);
  return sdkEnv;
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    runProcess,
    formatDuration,
    sleep,
    isCopilotSDKEnabled,
    buildCopilotSDKEnv,
    MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS,
    DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
    MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS,
    resolvePostResultWatchdogIdleTimeoutMs,
    DEFAULT_STALL_WARNING_INTERVAL_MS,
    MIN_STALL_WARNING_INTERVAL_MS,
    MAX_STALL_WARNING_INTERVAL_MS,
    resolveStallWarningIntervalMs,
    formatStepTimeoutBudget,
  };
}
