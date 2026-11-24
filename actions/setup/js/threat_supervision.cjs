// @ts-check
"use strict";

const { spawn } = require("node:child_process");
const { Duplex } = require("node:stream");
const { CATEGORIES, readSink } = require("./threat_report_result.cjs");

// A dedicated Linux subreaper, not the (possibly shared) Node host, adopts
// this attempt's orphans even across setsid/double-fork. Embedded because
// setup distributes the .cjs files. No compiler, npm addon, or privileges.
// Check the required kernel/procfs support *before* launching the engine.
const REAPER = `
import ctypes, errno, os, select, signal, subprocess, sys
children = "/proc/thread-self/children"
with open(children) as f:
    f.read()
# The PID numbers in "children" are relative to the procfs mount. Refuse a
# mount from an ancestor PID namespace rather than signalling a different PID.
with open("/proc/self/status") as f:
    if not any(line.split() == ["NSpid:", str(os.getpid())] for line in f):
        sys.exit("Detector procfs must match the current PID namespace")
if ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
    sys.exit("Cannot establish detector process ownership")
signal.signal(signal.SIGCHLD, signal.SIG_DFL)
os.close(os.pidfd_open(os.getpid()))

# fd 3 is a private control socket. Parent death or a stop request gives EOF.
if select.select([3], [], [], 0)[0]:
    sys.exit(0)
try:
    engine = subprocess.Popen(sys.argv[1:], close_fds=True, start_new_session=True)
except OSError as exc:
    os.write(3, errno.errorcode.get(exc.errno, "EIO").encode())
    sys.exit(1)
try:
    pidfd = os.pidfd_open(engine.pid)
    select.select([3, pidfd], [], [])
    code = engine.poll()
finally:
    # Only signal our *direct* children. No other thread or signal handler
    # reaps them, so their PIDs cannot be recycled between enumeration and
    # kill. Each killed generation reparents the next one to this subreaper.
    while True:
        try:
            pid, _ = os.waitpid(-1, os.WNOHANG)
        except ChildProcessError:
            break
        if pid:
            continue
        with open(children) as f:
            for pid in f.read().split():
                os.kill(int(pid), signal.SIGKILL)
        os.waitpid(-1, 0)
sys.exit(0 if code is None else (code if code >= 0 else 128 - code))
`;

// Keep independent byte tails: a chatty stdout cannot evict an auth error
// on stderr. Never interpret a truncated line as a Claude protocol record.
function capture(stream) {
  const limit = 64 * 1024;
  const ring = Buffer.alloc(limit);
  let length = 0;
  let pos = 0;
  stream.on("data", chunk => {
    length += chunk.length;
    const bytes = chunk.subarray(-limit);
    const first = bytes.copy(ring, pos);
    bytes.copy(ring, 0, first);
    pos = (pos + bytes.length) % limit;
  });
  return () => {
    const tail = length > limit ? Buffer.concat([ring.subarray(pos), ring.subarray(0, pos)]) : ring.subarray(0, length);
    const lines = tail.toString("utf8").split("\n");
    if (length > limit) lines.shift();
    return lines;
  };
}

function logTail(lines) {
  for (const line of lines.filter(Boolean).slice(-16)) {
    const safe = line
      .slice(0, 1024)
      .replace(/:(?=:)/g, ":\\")
      .replaceAll("##[", "##\\[")
      .replace(/[\p{Cc}\p{Cf}\u2028\u2029]/gu, c => `\\u${c.codePointAt(0).toString(16).padStart(4, "0")}`);
    process.stderr.write(`[engine] ${safe}\n`);
  }
}

function maxTurns(lines) {
  for (let i = lines.length - 1; i >= 0; i--) {
    if (!lines[i].trimStart().startsWith("{")) continue;
    try {
      const event = JSON.parse(lines[i]);
      if (event.type === "result" && event.parent_tool_use_id == null) return event.subtype === "error_max_turns";
    } catch {
      // Ordinary diagnostics and incomplete JSON aren't protocol events.
    }
  }
  return false;
}

// Poll for structural completeness only. detect() validates eligibility and
// publishes a valid report before considering any timeout or exit status.
const ALL_ELIGIBLE = Object.fromEntries(CATEGORIES.map(key => [key, true]));
function resultReady(sink) {
  try {
    return Boolean(sink && readSink(sink, ALL_ELIGIBLE));
  } catch {
    return false;
  }
}

function supervise({ launch, args, env, prompt, sink, timeoutMs, signal }) {
  if (process.platform !== "linux") throw new Error("Detector process supervision requires Linux");
  return new Promise(resolve => {
    const child = spawn("/usr/bin/python3", ["-I", "-S", "-c", REAPER, launch.command, ...args], { env, cwd: launch.cwd, detached: true, stdio: ["pipe", "pipe", "pipe", "pipe"] });
    const control = child.stdio[3];
    if (!(control instanceof Duplex)) throw new Error("Detector control pipe is unavailable");
    const stdout = capture(child.stdout);
    const stderr = capture(child.stderr);
    let spawnError = "";
    let reason;
    /** @type {Error|null} */
    let error = null;
    const stop = why => {
      if (reason) return;
      reason = why;
      control.end();
    };
    const abort = () => stop("cancelled");
    const timer = timeoutMs > 0 ? setTimeout(() => stop("timeout"), timeoutMs) : undefined;
    const poll = sink ? setInterval(() => resultReady(sink) && stop("reported"), 250) : undefined;
    signal?.addEventListener("abort", abort, { once: true });
    if (signal?.aborted) abort();
    control.on("data", chunk => (spawnError += chunk));
    const pipeError = err => {
      if (err.code !== "EPIPE" && err.code !== "ECONNRESET") error ??= err;
    };
    control.on("error", pipeError);
    child.stdin.on("error", pipeError);
    child.stdin.end(launch.mode === "direct" ? prompt : undefined);
    child.once("error", err => (error ??= err));
    child.once("close", (exitCode, exitSignal) => {
      clearTimeout(timer);
      clearInterval(poll);
      signal?.removeEventListener("abort", abort);
      const lines = stdout();
      logTail(lines);
      logTail(stderr());
      const cancelled = reason === "cancelled";
      const timedOut = reason === "timeout" || (!cancelled && launch.convention === "claude" && maxTurns(lines));
      if (spawnError) error ??= Object.assign(new Error(`engine launch failed: ${spawnError}`), { code: spawnError });
      if (timedOut || cancelled) error ??= new Error(timedOut ? "engine timed out" : "engine cancelled");
      if (!reason && exitCode !== 0 && !resultReady(sink)) error ??= new Error(`engine exited with ${exitSignal || `code ${exitCode}`}`);
      resolve({ timedOut, cancelled, exitCode, error });
    });
  });
}

module.exports = { supervise };
