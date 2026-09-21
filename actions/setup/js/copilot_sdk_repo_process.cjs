// @ts-check

"use strict";

const childProcess = require("child_process");
const { constants: bufferConstants } = require("buffer");
const { constants: osConstants } = require("os");
const { win32 } = require("path");

const CLEANUP_TIMEOUT_MS = 2000;

// Reflection.Emit avoids Add-Type's compiler subprocesses and temporary files.
// The unnamed, non-breakaway job contains only the supervisor PID and descendants.
const WINDOWS_JOB_KEEPER = `
$ErrorActionPreference = 'Stop'
$assembly = [AppDomain]::CurrentDomain.DefineDynamicAssembly([Reflection.AssemblyName]::new('OwnedRepoJob'), 'Run')
$type = $assembly.DefineDynamicModule('OwnedRepoJob').DefineType('OwnedRepoJobNative', 'Public, Abstract, Sealed')
function ImportNative([string]$name, [Type]$result, [Type[]]$parameters) {
  $method = $type.DefinePInvokeMethod($name, 'kernel32.dll', $name, 'Public, Static, PinvokeImpl', 'Standard', $result, $parameters, 'Winapi', 'Unicode')
  $method.SetImplementationFlags($method.GetMethodImplementationFlags() -bor [Reflection.MethodImplAttributes]::PreserveSig)
}
ImportNative 'CreateJobObjectW' ([IntPtr]) @([IntPtr], [IntPtr])
ImportNative 'SetInformationJobObject' ([bool]) @([IntPtr], [int], [IntPtr], [uint32])
ImportNative 'OpenProcess' ([IntPtr]) @([uint32], [bool], [uint32])
ImportNative 'AssignProcessToJobObject' ([bool]) @([IntPtr], [IntPtr])
ImportNative 'WaitForSingleObject' ([uint32]) @([IntPtr], [uint32])
ImportNative 'CloseHandle' ([bool]) @([IntPtr])
$native = $type.CreateType()
$job = [IntPtr]::Zero
$target = [IntPtr]::Zero
$info = [IntPtr]::Zero
try {
  $job = $native::CreateJobObjectW([IntPtr]::Zero, [IntPtr]::Zero)
  if ($job -eq [IntPtr]::Zero) { throw 'Could not create owned Windows job' }
  # JOBOBJECT_EXTENDED_LIMIT_INFORMATION: LimitFlags is at offset 16 on both ABIs.
  $size = if ([IntPtr]::Size -eq 8) { 144 } else { 112 }
  $info = [Runtime.InteropServices.Marshal]::AllocHGlobal($size)
  [Runtime.InteropServices.Marshal]::Copy([byte[]]::new($size), 0, $info, $size)
  [Runtime.InteropServices.Marshal]::WriteInt32($info, 16, 0x2000)
  if (!$native::SetInformationJobObject($job, 9, $info, $size)) { throw 'Could not set KILL_ON_JOB_CLOSE' }
  $target = $native::OpenProcess(0x100101, $false, $ownedPid)
  if ($target -eq [IntPtr]::Zero -or !$native::AssignProcessToJobObject($job, $target)) { throw 'Could not assign owned supervisor PID to Windows job' }
  [Console]::Out.WriteLine('ready')
  [Console]::Out.Flush()
  if ($native::WaitForSingleObject($target, [uint32]::MaxValue) -ne 0) { throw 'Could not wait for owned supervisor PID' }
} finally {
  if ($info -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::FreeHGlobal($info) }
  $targetClosed = $target -eq [IntPtr]::Zero -or $native::CloseHandle($target)
  $jobClosed = $job -eq [IntPtr]::Zero -or $native::CloseHandle($job)
  if (!$targetClosed -or !$jobClosed) { throw 'Could not close owned Windows job handles' }
}
`;

/**
 * @typedef {{command: string, args: string[], cwd: string, env: NodeJS.ProcessEnv, signal?: AbortSignal, timeoutMs: number, maxOutputBytes: number}} ProcessOptions
 * @typedef {{exitCode: number, stdout: string, stderr: string}} ProcessResult
 * @typedef {{type: "stdout" | "stderr", data: Buffer} | {type: "close", code: number | null, signal: NodeJS.Signals | null} | {type: "error", message: string, code?: string}} SupervisorMessage
 */

/** @param {string} code @param {string} message @param {unknown} [cause] */
function processError(code, message, cause) {
  return Object.assign(new Error(message, { cause }), { code });
}

// process_runner.cjs keeps its equivalent helper private.
/** @param {NodeJS.Signals | null} signal */
function exitCodeForSignal(signal) {
  const number = signal ? osConstants.signals[signal] : undefined;
  return typeof number === "number" ? 128 + number : null;
}

/** @param {unknown} value @returns {value is SupervisorMessage} */
function isSupervisorMessage(value) {
  if (!value || typeof value !== "object" || !("type" in value)) return false;
  switch (value.type) {
    case "stdout":
    case "stderr":
      return "data" in value && Buffer.isBuffer(value.data);
    case "error":
      return "message" in value && typeof value.message === "string" && (!("code" in value) || value.code === undefined || typeof value.code === "string");
    case "close":
      return (
        "code" in value &&
        (value.code === null || (typeof value.code === "number" && Number.isInteger(value.code))) &&
        "signal" in value &&
        (value.signal === null || (typeof value.signal === "string" && Object.hasOwn(osConstants.signals, value.signal)))
      );
    default:
      return false;
  }
}

// Evaluated only in an owned Windows subprocess; output stays on private IPC.
function superviseWindowsCommand() {
  const { spawn } = require("child_process");
  if (!process.send) throw new Error("Repository process supervisor requires IPC");
  const sendMessage = process.send.bind(process);
  /** @param {SupervisorMessage} message @param {() => void} [resume] */
  function send(message, resume) {
    sendMessage(message, error => {
      if (error) throw error;
      resume?.();
    });
  }
  let started = false;
  process.on("message", value => {
    if (started) throw new Error("Duplicate repository process supervisor request");
    started = true;
    if (
      !value ||
      typeof value !== "object" ||
      !("command" in value) ||
      typeof value.command !== "string" ||
      !("args" in value) ||
      !Array.isArray(value.args) ||
      !value.args.every(arg => typeof arg === "string") ||
      !("cwd" in value) ||
      typeof value.cwd !== "string" ||
      !("env" in value) ||
      !value.env ||
      typeof value.env !== "object" ||
      Array.isArray(value.env) ||
      !Object.values(value.env).every(entry => entry === undefined || typeof entry === "string")
    ) {
      throw new Error("Invalid repository process supervisor request");
    }
    const { command, args, cwd } = value;
    const env = { ...value.env };
    /** @param {NodeJS.ErrnoException} error */
    const reportError = error => send({ type: "error", message: error.message, code: error.code });
    try {
      const child = spawn(command, args, { cwd, env, shell: false, windowsHide: true, stdio: ["ignore", "pipe", "pipe"] });
      child.on("error", reportError);
      child.on("close", (code, signal) => send({ type: "close", code, signal }));
      /** @type {Array<["stdout" | "stderr", import("stream").Readable | null | undefined]>} */
      const streams = [
        ["stdout", child.stdout],
        ["stderr", child.stderr],
      ];
      for (const [type, stream] of streams) {
        if (!stream) continue;
        stream.on("error", reportError);
        stream.on("data", data => {
          stream.pause();
          send({ type, data }, () => stream.resume());
        });
      }
    } catch (error) {
      reportError(error instanceof Error ? error : new Error(String(error)));
    }
  });
}

/**
 * Internal executor, NEVER a model-exposed command API. All inputs must come from
 * trusted, approved workflow policy. No command, cwd, environment or budget defaults.
 * The combined cap counts raw stdout/stderr bytes; UTF-8 is decoded after close.
 * Normal nonzero exits are results; signals use exitCodeForSignal when recognized.
 * Failures reject (ABORT_ERR, ETIMEDOUT, ERR_REPO_PROCESS_OUTPUT_LIMIT, or OS errors).
 * Cleanup failures remain rejections and add cleanupErrors, never partial results.
 * POSIX groups stay referenced. Windows uses private, backpressured IPC inside a
 * PID-scoped Job Object; its fixed PowerShell 5.1 keeper receives no command data.
 * Killing that owned keeper closes the job, including orphaned grandchildren.
 * Normal completion also cleans the owned tree before returning the command result.
 * Host environment is never merged into env. Cleanup has a separate 2 s deadline.
 * @param {ProcessOptions} options
 * @returns {Promise<ProcessResult>}
 */
async function runCopilotSDKRepoProcess({ command, args, cwd, env, signal, timeoutMs, maxOutputBytes }) {
  if (typeof command !== "string" || !command || !Array.isArray(args) || args.findIndex(arg => typeof arg !== "string") !== -1) throw new TypeError("command and args must be explicit strings");
  if (typeof cwd !== "string" || !cwd || !env || typeof env !== "object" || Array.isArray(env)) throw new TypeError("cwd and env must be explicitly supplied");
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0 || timeoutMs > 2147483647) throw new RangeError("timeoutMs must be a positive integer <= 2147483647");
  if (!Number.isSafeInteger(maxOutputBytes) || maxOutputBytes <= 0 || maxOutputBytes > bufferConstants.MAX_STRING_LENGTH) throw new RangeError(`maxOutputBytes must be a positive integer <= ${bufferConstants.MAX_STRING_LENGTH}`);
  if (signal !== undefined && (typeof signal?.aborted !== "boolean" || typeof signal.addEventListener !== "function" || typeof signal.removeEventListener !== "function")) throw new TypeError("signal must be an AbortSignal");
  const abortError = () => Object.assign(processError("ABORT_ERR", "Repository process cancelled", signal?.reason), { name: "AbortError" });
  if (signal?.aborted) throw abortError();

  const windows = process.platform === "win32";
  /** @type {{command: string, cwd: string, env: NodeJS.ProcessEnv} | null} */
  let keeperCommand = null;
  if (windows) {
    const systemRoot = process.env.SystemRoot;
    if (!systemRoot || !win32.isAbsolute(systemRoot)) throw processError("ERR_REPO_PROCESS_CLEANUP", "An absolute SystemRoot is required for owned Windows process-tree cleanup");
    keeperCommand = { command: win32.join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), cwd: systemRoot, env: { SystemRoot: systemRoot } };
  }

  return new Promise((resolve, reject) => {
    const child = childProcess.spawn(windows ? process.execPath : command, windows ? ["--input-type=commonjs", "-e", `(${superviseWindowsCommand.toString()})()`] : args, {
      cwd,
      env,
      shell: false,
      windowsHide: true,
      detached: !windows,
      stdio: windows ? ["ignore", "ignore", "ignore", "ipc"] : ["ignore", "pipe", "pipe"],
      serialization: "advanced",
    });
    /** @type {Buffer[]} */
    const stdout = [];
    /** @type {Buffer[]} */
    const stderr = [];
    /** @type {Error | null} */
    let failure = null;
    /** @type {ProcessResult | null} */
    let result = null;
    /** @type {NodeJS.Timeout | undefined} */
    let cleanupTimer;
    /** @type {import("child_process").ChildProcess | undefined} */
    let keeper;
    let removeKeeperListeners = () => {};
    let outputBytes = 0;
    let settled = false;
    let stopping = false;
    let closed = false;
    let keeperClosed = false;
    let cleanupFinished = true;
    const onAbort = () => fail(abortError());
    const timer = setTimeout(() => fail(processError("ETIMEDOUT", `Repository process timed out after ${timeoutMs} ms`)), timeoutMs);

    /** @param {Error | ProcessResult} result */
    function settle(result) {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      clearTimeout(cleanupTimer);
      signal?.removeEventListener("abort", onAbort);
      removeKeeperListeners();
      child.stdout?.destroy();
      child.stderr?.destroy();
      if (child.connected) child.disconnect();
      child.off("error", fail).off("close", onClose).off("spawn", onSpawn).off("message", onMessage);
      child.stdout?.off("data", onStdout).off("error", fail);
      child.stderr?.off("data", onStderr).off("error", fail);
      stdout.length = stderr.length = 0;
      if (result instanceof Error) reject(result);
      else resolve(result);
    }

    function finishIfClosed() {
      if (!closed || !cleanupFinished || (keeper && !keeperClosed)) return;
      if (failure) settle(failure);
      else if (result) settle(result);
    }

    /** @param {Error} error */
    function fail(error) {
      stop(error);
    }

    /** @param {number} exitCode */
    function completeCommand(exitCode) {
      stop({ exitCode, stdout: Buffer.concat(stdout).toString("utf8"), stderr: Buffer.concat(stderr).toString("utf8") });
    }

    /** @param {Error | ProcessResult} outcome */
    function stop(outcome) {
      if (settled || stopping) return;
      stopping = true;
      if (outcome instanceof Error) failure = outcome;
      else result = outcome;
      cleanupFinished = false;
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      stdout.length = stderr.length = 0;
      /** @type {Error[]} */
      const cleanupErrors = [];
      if (failure) Object.assign(failure, { cleanupErrors });
      /** @param {unknown} reason */
      function record(reason) {
        const cleanupError = reason instanceof Error ? reason : new Error(String(reason));
        cleanupErrors.push(cleanupError);
        if (!failure) {
          failure = Object.assign(processError("ERR_REPO_PROCESS_CLEANUP", `Repository process cleanup failed: ${cleanupError.message}`, cleanupError), { cleanupErrors });
        } else {
          failure.message += `; cleanup failed: ${cleanupError.message}`;
        }
        return failure;
      }
      /** @param {import("child_process").ChildProcess | undefined} owned */
      function killOwned(owned) {
        if (!owned || owned.pid === undefined || owned.exitCode !== null || owned.signalCode !== null) return;
        try {
          // A false return may race exit; close or the cleanup deadline decides.
          owned.kill("SIGKILL");
        } catch (killError) {
          record(killError);
        }
      }
      cleanupTimer = setTimeout(() => {
        const error = record(new Error(`Owned process tree did not close within ${CLEANUP_TIMEOUT_MS} ms`));
        killOwned(keeper);
        killOwned(child);
        settle(error);
      }, CLEANUP_TIMEOUT_MS);
      if (child.pid === undefined) return settle(outcome);
      try {
        if (windows) {
          killOwned(keeper);
          killOwned(child);
        } else {
          try {
            process.kill(-child.pid, "SIGKILL");
          } catch (killError) {
            // ESRCH means the entire owned group has already disappeared.
            if (!(killError && typeof killError === "object" && "code" in killError && killError.code === "ESRCH")) throw killError;
          }
        }
      } catch (killError) {
        record(killError);
      }
      cleanupFinished = true;
      finishIfClosed();
    }

    /** @param {Buffer[]} chunks @param {Buffer} data */
    function collect(chunks, data) {
      if (settled || stopping) return;
      if (data.length > maxOutputBytes - outputBytes) return fail(processError("ERR_REPO_PROCESS_OUTPUT_LIMIT", `Repository process exceeded the combined ${maxOutputBytes}-byte output limit`));
      outputBytes += data.length;
      if (data.length) chunks.push(data);
    }
    /** @param {Buffer} data */
    const onStdout = data => collect(stdout, data);
    /** @param {Buffer} data */
    const onStderr = data => collect(stderr, data);

    /** @param {import("child_process").Serializable} value */
    function onMessage(value) {
      if (settled || stopping) return;
      if (!isSupervisorMessage(value)) return fail(processError("ERR_REPO_PROCESS_SUPERVISOR", "Unexpected repository process supervisor message"));
      const message = value;
      if (message.type === "stdout" || message.type === "stderr") {
        collect(message.type === "stdout" ? stdout : stderr, message.data);
      } else if (message?.type === "error") {
        fail(Object.assign(new Error(message.message), { code: message.code }));
      } else if (message?.type === "close") {
        const exitCode = message.code ?? exitCodeForSignal(message.signal);
        if (exitCode === null || !Number.isInteger(exitCode) || !child.connected) fail(processError("ERR_REPO_PROCESS_SUPERVISOR", "Repository process supervisor returned no usable exit status"));
        else completeCommand(exitCode);
      } else {
        fail(processError("ERR_REPO_PROCESS_SUPERVISOR", "Unexpected repository process supervisor message"));
      }
    }

    /** @param {number | null} code @param {NodeJS.Signals | null} exitSignal */
    function onClose(code, exitSignal) {
      closed = true;
      if (stopping) {
        finishIfClosed();
        return;
      }
      if (windows && code !== 0) return fail(processError("ERR_REPO_PROCESS_SUPERVISOR", `Repository process supervisor failed (exit code: ${code}, signal: ${exitSignal})`));
      const exitCode = windows ? null : (code ?? exitCodeForSignal(exitSignal));
      if (exitCode === null) return fail(processError("ERR_REPO_PROCESS_EXIT", "Repository process closed without a recognized exit status"));
      completeCommand(exitCode);
    }

    function onSpawn() {
      if (!keeperCommand || stopping) return;
      try {
        if (!child.pid) throw processError("ERR_REPO_PROCESS_JOB", "Windows supervisor has no owned PID");
        const encoded = Buffer.from(`$ownedPid = ${child.pid}\n${WINDOWS_JOB_KEEPER}`, "utf16le").toString("base64");
        const job = childProcess.spawn(keeperCommand.command, ["-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded], {
          cwd: keeperCommand.cwd,
          env: keeperCommand.env,
          shell: false,
          windowsHide: true,
          stdio: ["ignore", "pipe", "ignore"],
        });
        keeper = job;
        let readiness = "";
        let started = false;
        /** @param {Buffer} data */
        const onReady = data => {
          if (stopping || settled || !data.length) return;
          readiness += data.toString("ascii");
          if (started || (!"ready\r\n".startsWith(readiness) && !"ready\n".startsWith(readiness))) return fail(processError("ERR_REPO_PROCESS_JOB", "Unexpected Windows job keeper response"));
          if (readiness === "ready\r\n" || readiness === "ready\n") {
            started = true;
            try {
              child.send({ command, args, cwd, env }, error => {
                if (error) fail(error);
              });
            } catch (error) {
              fail(error instanceof Error ? error : new Error(String(error)));
            }
          }
        };
        /** @param {number | null} code @param {NodeJS.Signals | null} jobSignal */
        const onJobClose = (code, jobSignal) => {
          keeperClosed = true;
          if (!stopping && (code !== 0 || !started)) fail(processError("ERR_REPO_PROCESS_JOB", `Windows job keeper failed (exit code: ${code}, signal: ${jobSignal}, ready: ${started})`));
          finishIfClosed();
        };
        removeKeeperListeners = () => {
          job.stdout?.destroy();
          job.stdout?.off("data", onReady).off("error", fail);
          job.off("error", fail).off("close", onJobClose);
        };
        job.on("error", fail).once("close", onJobClose);
        job.stdout?.on("data", onReady).on("error", fail);
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    }

    child.on("error", fail).once("close", onClose).once("spawn", onSpawn).on("message", onMessage);
    child.stdout?.on("data", onStdout).on("error", fail);
    child.stderr?.on("data", onStderr).on("error", fail);
    signal?.addEventListener("abort", onAbort, { once: true });
    if (signal?.aborted) onAbort();
  });
}

module.exports = { runCopilotSDKRepoProcess };
