// @ts-check

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { PassThrough } from "stream";
import { getEventListeners } from "events";
import { mkdtemp, readFile, realpath, rm, writeFile } from "fs/promises";
import { tmpdir, constants as osConstants } from "os";
import { join, parse } from "path";
import { setTimeout as delay } from "timers/promises";

const require = createRequire(import.meta.url);
const childProcess = require("child_process");
const { constants: bufferConstants } = require("buffer");
const { runCopilotSDKRepoProcess } = require("./copilot_sdk_repo_process.cjs");
const platformDescriptor = Object.getOwnPropertyDescriptor(process, "platform");
if (!platformDescriptor) throw new Error("process.platform descriptor is missing");

/** @param {string} [script] @param {Record<string, unknown>} [overrides] */
function options(script = "", overrides = {}) {
  return { command: process.execPath, args: ["--input-type=commonjs", "-e", script], cwd: process.cwd(), env: {}, timeoutMs: 5000, maxOutputBytes: 256 * 1024, ...overrides };
}

/** @param {number | undefined} pid @param {boolean} [ipc] */
function fakeChild(pid, ipc = false) {
  const child = Object.assign(new childProcess.ChildProcess(), {
    pid,
    connected: ipc,
    stdin: null,
    stdout: new PassThrough(),
    stderr: new PassThrough(),
    kill: vi.fn(() => true),
    unref: vi.fn(),
    send: vi.fn((_message, callback) => {
      callback(null);
      return true;
    }),
    disconnect: vi.fn(() => {
      Object.assign(child, { connected: false });
    }),
  });
  return child;
}

/** @param {import("child_process").ChildProcess} child @param {number | null} [code] @param {NodeJS.Signals | null} [signal] */
function closeChild(child, code = 0, signal = null) {
  Object.assign(child, { exitCode: code, signalCode: signal });
  child.emit("close", code, signal);
}

function expectClean(child, signal) {
  for (const event of ["error", "exit", "close", "spawn", "message"]) expect(child.listenerCount(event)).toBe(0);
  for (const stream of [child.stdout, child.stderr]) {
    expect(stream.destroyed).toBe(true);
    expect(stream.listenerCount("data")).toBe(0);
    expect(stream.listenerCount("error")).toBe(0);
  }
  if (signal) expect(getEventListeners(signal, "abort")).toHaveLength(0);
  expect(child.unref).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
}

describe("runCopilotSDKRepoProcess deterministic lifecycle", () => {
  let spawn;
  let kill;

  beforeEach(() => {
    vi.useFakeTimers();
    Object.defineProperty(process, "platform", { value: "linux", configurable: true });
    spawn = vi.spyOn(childProcess, "spawn").mockImplementation(() => {
      throw new Error("Unexpected subprocess spawn in an inert unit test");
    });
    kill = vi.spyOn(process, "kill").mockReturnValue(true);
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    Object.defineProperty(process, "platform", platformDescriptor);
  });

  it.each(["timeoutMs", "maxOutputBytes"])("rejects invalid %s without spawning", async bound => {
    for (const value of [0, -1, 1.5, NaN, Infinity, -Infinity, undefined, null, "100", Number.MAX_SAFE_INTEGER]) {
      await expect(runCopilotSDKRepoProcess(options("", { [bound]: value }))).rejects.toThrow(RangeError);
    }
    expect(spawn).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("rejects timer overflow and impossible string bounds without spawning", async () => {
    await expect(runCopilotSDKRepoProcess(options("", { timeoutMs: 2147483648 }))).rejects.toThrow(RangeError);
    await expect(runCopilotSDKRepoProcess(options("", { maxOutputBytes: bufferConstants.MAX_STRING_LENGTH + 1 }))).rejects.toThrow(RangeError);
    expect(spawn).not.toHaveBeenCalled();
  });

  it("requires explicit command, argv, cwd, environment and a valid optional signal", async () => {
    for (const override of [{ command: "" }, { args: [1] }, { args: Array(1) }, { cwd: "" }, { cwd: undefined }, { env: undefined }, { env: null }, { signal: {} }]) {
      await expect(runCopilotSDKRepoProcess(options("", override))).rejects.toThrow(TypeError);
    }
    expect(spawn).not.toHaveBeenCalled();
  });

  it("rejects pre-aborted signals with their reason before spawning", async () => {
    const controller = new AbortController();
    const cause = new Error("already stopped");
    controller.abort(cause);
    await expect(runCopilotSDKRepoProcess(options("", { signal: controller.signal }))).rejects.toMatchObject({ name: "AbortError", code: "ABORT_ERR", cause });
    expect(spawn).not.toHaveBeenCalled();
    expect(getEventListeners(controller.signal, "abort")).toHaveLength(0);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("propagates synchronous spawn failures without leaving timers", async () => {
    const error = Object.assign(new Error("spawn failed"), { code: "ENOENT" });
    spawn.mockImplementation(() => {
      throw error;
    });
    await expect(runCopilotSDKRepoProcess(options())).rejects.toBe(error);
    expect(kill).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("rejects asynchronous spawn errors once even if close follows", async () => {
    const child = fakeChild(undefined);
    spawn.mockReturnValue(child);
    const error = Object.assign(new Error("missing executable"), { code: "ENOENT" });
    const rejected = expect(runCopilotSDKRepoProcess(options())).rejects.toBe(error);
    child.emit("error", error);
    closeChild(child, -2);
    await rejected;
    expect(kill).not.toHaveBeenCalled();
    expectClean(child);
  });

  it.each([0, 37])("returns exact exit code %s, drains after exit and never forwards output", async exitCode => {
    const child = fakeChild(41001);
    spawn.mockReturnValue(child);
    const controller = new AbortController();
    const request = options("", { signal: controller.signal, maxOutputBytes: 7 });
    const outWrite = vi.spyOn(process.stdout, "write").mockReturnValue(true);
    const errWrite = vi.spyOn(process.stderr, "write").mockReturnValue(true);
    const resolved = vi.fn();
    const promise = runCopilotSDKRepoProcess(request).then(result => {
      resolved();
      return result;
    });
    const utf8 = Buffer.from("\u{1f642}");
    child.stdout.emit("data", utf8.subarray(0, 2));
    child.emit("exit", exitCode, null);
    await Promise.resolve();
    expect(resolved).not.toHaveBeenCalled();
    expect(kill).not.toHaveBeenCalled();
    child.stdout.emit("data", utf8.subarray(2));
    child.stderr.emit("data", Buffer.from("err"));
    closeChild(child, exitCode);
    await expect(promise).resolves.toEqual({ exitCode, stdout: "\u{1f642}", stderr: "err" });
    expect(spawn).toHaveBeenCalledWith(request.command, request.args, expect.objectContaining({ cwd: request.cwd, env: request.env, shell: false, detached: true, stdio: ["ignore", "pipe", "pipe"] }));
    expect(spawn.mock.calls[0][2]).not.toHaveProperty("signal");
    expect(outWrite).not.toHaveBeenCalled();
    expect(errWrite).not.toHaveBeenCalled();
    expect(kill).toHaveBeenCalledExactlyOnceWith(-41001, "SIGKILL");
    expectClean(child, controller.signal);
  });

  it("cleans POSIX descendants without open pipes before returning their parent's status", async () => {
    const child = fakeChild(41012);
    spawn.mockReturnValue(child);
    let descendantAlive = true;
    kill.mockImplementation((pid, signal) => {
      expect(pid).toBe(-41012);
      expect(signal).toBe("SIGKILL");
      expect(child.exitCode).toBe(37);
      descendantAlive = false;
      return true;
    });
    const promise = runCopilotSDKRepoProcess(options());
    closeChild(child, 37);
    await expect(promise).resolves.toEqual({ exitCode: 37, stdout: "", stderr: "" });
    expect(descendantAlive).toBe(false);
    expect(kill).toHaveBeenCalledTimes(1);
    expect(child.kill).not.toHaveBeenCalled();
    expectClean(child);
  });

  it.each(["ESRCH", "EPERM"])("handles %s when cleaning a normally completed POSIX group", async code => {
    const child = fakeChild(41013);
    spawn.mockReturnValue(child);
    const error = Object.assign(new Error("group cleanup"), { code });
    kill.mockImplementation(() => {
      throw error;
    });
    const promise = runCopilotSDKRepoProcess(options());
    const checked = code === "ESRCH" ? expect(promise).resolves.toEqual({ exitCode: 0, stdout: "", stderr: "" }) : expect(promise).rejects.toMatchObject({ code: "ERR_REPO_PROCESS_CLEANUP", cleanupErrors: [error] });
    closeChild(child, 0);
    await checked;
    expect(kill).toHaveBeenCalledExactlyOnceWith(-41013, "SIGKILL");
    expectClean(child);
  });

  it("accepts the minimum positive budgets and an exact combined byte cap", async () => {
    const child = fakeChild(41002);
    spawn.mockReturnValue(child);
    const promise = runCopilotSDKRepoProcess(options("", { timeoutMs: 1, maxOutputBytes: 1 }));
    child.stderr.emit("data", Buffer.from("x"));
    closeChild(child);
    await expect(promise).resolves.toEqual({ exitCode: 0, stdout: "", stderr: "x" });
    expectClean(child);
  });

  it.each(["stdout", "stderr", "combined", "utf8"])("rejects %s overflow instead of returning truncated output", async stream => {
    const child = fakeChild(41003);
    spawn.mockReturnValue(child);
    const rejected = expect(runCopilotSDKRepoProcess(options("", { maxOutputBytes: stream === "utf8" ? 3 : 5 }))).rejects.toMatchObject({ code: "ERR_REPO_PROCESS_OUTPUT_LIMIT" });
    if (stream === "combined") {
      child.stdout.emit("data", Buffer.from("123"));
      child.stderr.emit("data", Buffer.from("456"));
    } else if (stream === "utf8") {
      child.stdout.emit("data", Buffer.from("\u{1f642}"));
    } else {
      child[stream].emit("data", Buffer.from("123456"));
    }
    expect(kill).toHaveBeenCalledExactlyOnceWith(-41003, "SIGKILL");
    closeChild(child, 0);
    await rejected;
    expectClean(child);
  });

  it("enforces the exact timeout and handles synchronous close during group kill", async () => {
    const child = fakeChild(41004);
    spawn.mockReturnValue(child);
    kill.mockImplementation(() => {
      closeChild(child, null, "SIGKILL");
      return true;
    });
    const rejected = expect(runCopilotSDKRepoProcess(options("", { timeoutMs: 100 }))).rejects.toMatchObject({ code: "ETIMEDOUT" });
    vi.advanceTimersByTime(99);
    expect(kill).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    await rejected;
    expect(kill).toHaveBeenCalledExactlyOnceWith(-41004, "SIGKILL");
    expectClean(child);
  });

  it("bounds cleanup when exit occurs but streams and close never finish", async () => {
    const child = fakeChild(41005);
    spawn.mockReturnValue(child);
    const rejected = expect(runCopilotSDKRepoProcess(options("", { timeoutMs: 100 }))).rejects.toMatchObject({ code: "ETIMEDOUT", cleanupErrors: [expect.objectContaining({ message: expect.stringContaining("2000 ms") })] });
    Object.assign(child, { exitCode: 0 });
    child.emit("exit", 0, null);
    vi.advanceTimersByTime(100);
    expect(child.stdout.destroyed).toBe(false);
    vi.advanceTimersByTime(1999);
    expect(child.stdout.destroyed).toBe(false);
    vi.advanceTimersByTime(1);
    await rejected;
    expect(kill).toHaveBeenCalledExactlyOnceWith(-41005, "SIGKILL");
    expect(child.kill).not.toHaveBeenCalled();
    expectClean(child);
    closeChild(child, 0);
  });

  it.each([false, true])("cancels a running child, including abort during spawn: %s", async duringSpawn => {
    const child = fakeChild(41006);
    const controller = new AbortController();
    spawn.mockImplementation(() => {
      if (duringSpawn) controller.abort("stop");
      return child;
    });
    const rejected = expect(runCopilotSDKRepoProcess(options("", { signal: controller.signal }))).rejects.toMatchObject({ code: "ABORT_ERR", name: "AbortError", cause: "stop" });
    if (!duringSpawn) controller.abort("stop");
    expect(kill).toHaveBeenCalledExactlyOnceWith(-41006, "SIGKILL");
    closeChild(child, null, "SIGKILL");
    await rejected;
    expectClean(child, controller.signal);
  });

  it("preserves the first failure across output, abort, error, exit and close races", async () => {
    const child = fakeChild(41007);
    spawn.mockReturnValue(child);
    const controller = new AbortController();
    const resolved = vi.fn();
    const rejected = vi.fn();
    const observed = runCopilotSDKRepoProcess(options("", { signal: controller.signal, timeoutMs: 100, maxOutputBytes: 1 })).then(resolved, rejected);
    child.stdout.emit("data", Buffer.from("xx"));
    controller.abort();
    child.emit("exit", 0, null);
    child.emit("error", new Error("late error"));
    closeChild(child, 0);
    closeChild(child, 42);
    vi.advanceTimersByTime(10000);
    await observed;
    expect(resolved).not.toHaveBeenCalled();
    expect(rejected).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ code: "ERR_REPO_PROCESS_OUTPUT_LIMIT" }));
    expect(kill).toHaveBeenCalledTimes(1);
    expectClean(child, controller.signal);
  });

  it("rejects stream errors and reports cleanup errors without changing the primary failure", async () => {
    const child = fakeChild(41008);
    spawn.mockReturnValue(child);
    const error = new Error("pipe failed");
    const denied = Object.assign(new Error("group kill denied"), { code: "EPERM" });
    kill.mockImplementation(() => {
      throw denied;
    });
    const rejected = expect(runCopilotSDKRepoProcess(options())).rejects.toBe(error);
    child.stderr.emit("error", error);
    closeChild(child, 1);
    await rejected;
    expect(error).toMatchObject({ cleanupErrors: [denied] });
    expect(error.message).toContain("group kill denied");
    expectClean(child);
  });

  it("treats ESRCH as an already-cleaned group, not a new failure", async () => {
    const child = fakeChild(41009);
    spawn.mockReturnValue(child);
    kill.mockImplementation(() => {
      throw Object.assign(new Error("no group"), { code: "ESRCH" });
    });
    const rejected = expect(runCopilotSDKRepoProcess(options("", { timeoutMs: 1 }))).rejects.toMatchObject({ code: "ETIMEDOUT", cleanupErrors: [] });
    vi.advanceTimersByTime(1);
    closeChild(child, 0);
    await rejected;
    expectClean(child);
  });

  it("maps recognized signals and rejects a missing exit status", async () => {
    const child = fakeChild(41010);
    spawn.mockReturnValueOnce(child);
    const promise = runCopilotSDKRepoProcess(options());
    closeChild(child, null, "SIGTERM");
    await expect(promise).resolves.toEqual({ exitCode: 128 + osConstants.signals.SIGTERM, stdout: "", stderr: "" });
    expectClean(child);
    const unknown = fakeChild(41011);
    spawn.mockReturnValueOnce(unknown);
    const rejected = expect(runCopilotSDKRepoProcess(options())).rejects.toMatchObject({ code: "ERR_REPO_PROCESS_EXIT" });
    closeChild(unknown, null, null);
    await rejected;
    expectClean(unknown);
  });

  describe("Windows owned supervisor", () => {
    beforeEach(() => {
      Object.defineProperty(process, "platform", { value: "win32", configurable: true });
      vi.stubEnv("SystemRoot", "C:\\Windows");
    });

    it.each([0, 37])("establishes a PID-scoped job and preserves command status %s during owned termination", async exitCode => {
      const child = fakeChild(42001, true);
      const keeper = fakeChild(42002);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const request = options("", { env: { ONLY_POLICY: "yes" } });
      const promise = runCopilotSDKRepoProcess(request);
      child.emit("spawn");
      expect(child.send).not.toHaveBeenCalled();
      const [keeperPath, keeperArgs, keeperOptions] = spawn.mock.calls[1];
      expect(keeperPath).toBe("C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe");
      expect(keeperArgs.slice(0, 4)).toEqual(["-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand"]);
      const script = Buffer.from(keeperArgs[4], "base64").toString("utf16le");
      expect(script).toContain("$ownedPid = 42001\n");
      expect(script).toContain("AssignProcessToJobObject");
      expect(script).not.toContain("ONLY_POLICY");
      expect(keeperOptions).toEqual({ cwd: "C:\\Windows", env: { SystemRoot: "C:\\Windows" }, shell: false, windowsHide: true, stdio: ["ignore", "pipe", "ignore"] });
      keeper.stdout.emit("data", Buffer.from("rea"));
      expect(child.send).not.toHaveBeenCalled();
      keeper.stdout.emit("data", Buffer.from("dy\r\n"));
      expect(child.send).toHaveBeenCalledWith({ command: request.command, args: request.args, cwd: request.cwd, env: request.env }, expect.any(Function));
      expect(spawn).toHaveBeenCalledWith(
        process.execPath,
        expect.arrayContaining(["--input-type=commonjs", "-e"]),
        expect.objectContaining({ env: request.env, detached: false, shell: false, stdio: ["ignore", "ignore", "ignore", "ipc"], serialization: "advanced" })
      );
      child.emit("message", { type: "stdout", data: Buffer.from("out") });
      child.emit("message", { type: "stderr", data: Buffer.from("err") });
      child.emit("message", { type: "close", code: exitCode, signal: null });
      expect(child.kill).toHaveBeenCalledExactlyOnceWith("SIGKILL");
      expect(keeper.kill).toHaveBeenCalledExactlyOnceWith("SIGKILL");
      expect(child.disconnect).not.toHaveBeenCalled();
      closeChild(child, 1);
      expect(vi.getTimerCount()).toBe(1);
      closeChild(keeper, null, "SIGKILL");
      await expect(promise).resolves.toEqual({ exitCode, stdout: "out", stderr: "err" });
      expect(spawn).toHaveBeenCalledTimes(2);
      expect(kill).not.toHaveBeenCalled();
      expectClean(child);
    });

    it("preserves the recorded result when owned kill callbacks close both processes synchronously", async () => {
      const child = fakeChild(42016, true);
      const keeper = fakeChild(42017);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      child.kill.mockImplementation(() => {
        closeChild(child, 1);
        return true;
      });
      keeper.kill.mockImplementation(() => {
        closeChild(keeper, null, "SIGKILL");
        return true;
      });
      const promise = runCopilotSDKRepoProcess(options());
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      child.emit("message", { type: "stdout", data: Buffer.from("complete") });
      child.emit("message", { type: "close", code: 37, signal: null });
      await expect(promise).resolves.toEqual({ exitCode: 37, stdout: "complete", stderr: "" });
      expect(child.kill).toHaveBeenCalledTimes(1);
      expect(keeper.kill).toHaveBeenCalledTimes(1);
      expectClean(child);
    });

    it("uses the cleanup deadline, not the execution timeout, after recording a normal result", async () => {
      const child = fakeChild(42018, true);
      const keeper = fakeChild(42019);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const rejected = expect(runCopilotSDKRepoProcess(options("", { timeoutMs: 100 }))).rejects.toMatchObject({
        code: "ERR_REPO_PROCESS_CLEANUP",
        cleanupErrors: [expect.objectContaining({ message: expect.stringContaining("2000 ms") })],
      });
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      child.emit("message", { type: "close", code: 0, signal: null });
      vi.advanceTimersByTime(1999);
      expect(child.stdout.destroyed).toBe(false);
      expect(child.kill).toHaveBeenCalledTimes(1);
      vi.advanceTimersByTime(1);
      await rejected;
      expectClean(child);
    });

    it("kills only owned handles and waits for the job keeper even if the supervisor closes first", async () => {
      const child = fakeChild(42002, true);
      const keeper = fakeChild(42003);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const controller = new AbortController();
      const rejected = expect(runCopilotSDKRepoProcess(options("", { signal: controller.signal }))).rejects.toMatchObject({ code: "ABORT_ERR", cleanupErrors: [] });
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      controller.abort();
      expect(keeper.kill).toHaveBeenCalledExactlyOnceWith("SIGKILL");
      expect(child.kill).toHaveBeenCalledExactlyOnceWith("SIGKILL");
      closeChild(child, 1);
      expect(vi.getTimerCount()).toBe(1);
      closeChild(keeper, null, "SIGKILL");
      await rejected;
      expect(keeper.listenerCount("error")).toBe(0);
      expect(keeper.listenerCount("close")).toBe(0);
      expect(keeper.stdout.destroyed).toBe(true);
      expect(kill).not.toHaveBeenCalled();
      expectClean(child, controller.signal);
    });

    it.each(["spawn", "no-pipe", "status", "protocol", "empty"])("rejects job keeper %s failure before starting the policy command", async mode => {
      const child = fakeChild(42004, true);
      const spawnFailed = mode === "spawn" || mode === "no-pipe";
      const keeper = fakeChild(spawnFailed ? undefined : 42005);
      if (mode === "no-pipe") Object.assign(keeper, { stdout: null });
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const rejected = expect(runCopilotSDKRepoProcess(options())).rejects.toMatchObject({ code: spawnFailed ? "ENOENT" : "ERR_REPO_PROCESS_JOB" });
      child.emit("spawn");
      if (spawnFailed) keeper.emit("error", Object.assign(new Error("keeper unavailable"), { code: "ENOENT" }));
      if (mode === "protocol") keeper.stdout.emit("data", Buffer.from("not ready"));
      closeChild(keeper, mode === "empty" ? 0 : 1);
      closeChild(child, 1);
      await rejected;
      expect(child.send).not.toHaveBeenCalled();
      expect(keeper.listenerCount("error")).toBe(0);
      expect(keeper.listenerCount("close")).toBe(0);
      expectClean(child);
    });

    it.each(["sync", "async"])("rejects %s IPC send failures and cleans both owned processes", async mode => {
      const child = fakeChild(42010, true);
      const keeper = fakeChild(42011);
      const error = Object.assign(new Error("IPC failed"), { code: "ERR_IPC_CHANNEL_CLOSED" });
      child.send.mockImplementation((_message, callback) => {
        if (mode === "sync") throw error;
        callback(error);
        return false;
      });
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const rejected = expect(runCopilotSDKRepoProcess(options())).rejects.toBe(error);
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      closeChild(child, 1);
      closeChild(keeper, 1);
      await rejected;
      expect(child.kill).toHaveBeenCalledTimes(1);
      expect(keeper.kill).toHaveBeenCalledTimes(1);
      expectClean(child);
    });

    it("does not send a policy command if readiness arrives after cancellation", async () => {
      const child = fakeChild(42012, true);
      const keeper = fakeChild(42013);
      const controller = new AbortController();
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const rejected = expect(runCopilotSDKRepoProcess(options("", { signal: controller.signal }))).rejects.toMatchObject({ code: "ABORT_ERR" });
      child.emit("spawn");
      controller.abort();
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      closeChild(keeper, 1);
      closeChild(child, 1);
      await rejected;
      expect(child.send).not.toHaveBeenCalled();
      expectClean(child, controller.signal);
    });

    it("cannot launch a second policy command on duplicate readiness", async () => {
      const child = fakeChild(42014, true);
      const keeper = fakeChild(42015);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const rejected = expect(runCopilotSDKRepoProcess(options())).rejects.toMatchObject({ code: "ERR_REPO_PROCESS_JOB" });
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      keeper.stdout.emit("data", Buffer.alloc(0));
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      closeChild(keeper, 1);
      closeChild(child, 1);
      await rejected;
      expect(child.send).toHaveBeenCalledTimes(1);
      expectClean(child);
    });

    it("bounds cleanup if both owned processes never emit close", async () => {
      const child = fakeChild(42008, true);
      const keeper = fakeChild(42009);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const rejected = expect(runCopilotSDKRepoProcess(options("", { timeoutMs: 1 }))).rejects.toMatchObject({ code: "ETIMEDOUT", cleanupErrors: [expect.any(Error)] });
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      vi.advanceTimersByTime(2001);
      await rejected;
      expect(keeper.kill).toHaveBeenCalledWith("SIGKILL");
      expect(child.kill).toHaveBeenCalledWith("SIGKILL");
      expect(keeper.stdout.destroyed).toBe(true);
      expect(keeper.stdout.listenerCount("data")).toBe(0);
      expect(keeper.listenerCount("error")).toBe(0);
      expect(keeper.listenerCount("close")).toBe(0);
      expectClean(child);
    });

    it.each(["error", "overflow", "unexpected", "status", "primitive", "output-type", "error-type", "signal"])("rejects supervisor %s messages rather than reporting success", async kind => {
      const child = fakeChild(42006, true);
      const keeper = fakeChild(42007);
      spawn.mockReturnValueOnce(child).mockReturnValueOnce(keeper);
      const code = kind === "error" ? "ENOENT" : kind === "overflow" ? "ERR_REPO_PROCESS_OUTPUT_LIMIT" : "ERR_REPO_PROCESS_SUPERVISOR";
      const rejected = expect(runCopilotSDKRepoProcess(options("", { maxOutputBytes: 3 }))).rejects.toMatchObject({ code });
      child.emit("spawn");
      keeper.stdout.emit("data", Buffer.from("ready\n"));
      if (kind === "error") child.emit("message", { type: "error", message: "missing command", code: "ENOENT" });
      else if (kind === "overflow") {
        child.emit("message", { type: "stdout", data: Buffer.from("ab") });
        child.emit("message", { type: "stderr", data: Buffer.from("cd") });
      } else if (kind === "status") child.emit("message", { type: "close", code: "0", signal: null });
      else if (kind === "primitive") child.emit("message", null);
      else if (kind === "output-type") child.emit("message", { type: "stdout", data: "not a buffer" });
      else if (kind === "error-type") child.emit("message", { type: "error", message: 42, code: "ENOENT" });
      else if (kind === "signal") child.emit("message", { type: "close", code: null, signal: "not-a-signal" });
      else child.emit("message", { type: "unexpected" });
      child.emit("message", { type: "close", code: 0, signal: null });
      closeChild(keeper, null, "SIGKILL");
      closeChild(child, 1);
      await rejected;
      expect(spawn).toHaveBeenCalledTimes(2);
      expectClean(child);
    });

    it("fails before spawning if a trusted absolute keeper location is unavailable", async () => {
      vi.stubEnv("SystemRoot", "");
      await expect(runCopilotSDKRepoProcess(options())).rejects.toMatchObject({ code: "ERR_REPO_PROCESS_CLEANUP" });
      expect(spawn).not.toHaveBeenCalled();
      expect(vi.getTimerCount()).toBe(0);
    });
  });
});

/** @param {number} pid */
async function isRunning(pid) {
  try {
    process.kill(pid, 0);
    if (process.platform === "linux") {
      // Orphaned zombies cannot execute or retain pipes, but some CI init processes
      // delay reaping them. Do not mistake that for a live descendant.
      const stat = await readFile(join(parse(process.cwd()).root, "proc", String(pid), "stat"), "utf8");
      if (stat.slice(stat.lastIndexOf(")") + 2).startsWith("Z")) return false;
    }
    return true;
  } catch (error) {
    if (error.code === "ESRCH" || error.code === "ENOENT") return false;
    throw error;
  }
}

/** @param {() => Promise<boolean>} predicate */
async function waitFor(predicate) {
  const deadline = Date.now() + 5000;
  while (!(await predicate())) {
    if (Date.now() >= deadline) throw new Error("Owned test process did not reach the expected lifecycle state");
    await delay(20);
  }
}

/** @param {Promise<unknown> | undefined} promise */
async function awaitCleanup(promise) {
  if (!promise) return;
  let timer;
  try {
    await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error("Owned test executor did not settle during cleanup")), 5000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

describe("runCopilotSDKRepoProcess inert real subprocesses", () => {
  it("preserves executable, literal argv, cwd, explicit minimal env and EOF on stdin", async () => {
    const directory = await mkdtemp(join(tmpdir(), "gh aw repo process "));
    vi.stubEnv("GH_AW_REPO_PROCESS_PARENT_ONLY", "must-not-leak");
    try {
      const cwd = await realpath(directory);
      const argv = ["literal spaces", "; not a shell", "$(not-a-shell)", "&|<>^%PATH%", '"quoted"', ""];
      const script = `
        let input = "";
        process.stdin.on("data", chunk => { input += chunk; });
        process.stdin.on("end", () => {
          process.stdout.write(JSON.stringify({
            executable: process.execPath, argv: process.argv.slice(1), cwd: process.cwd(),
            selected: process.env.ONLY_POLICY, inherited: process.env.GH_AW_REPO_PROCESS_PARENT_ONLY ?? null, input
          }));
          process.stderr.write("private diagnostic");
        });
        process.stdin.resume();
      `;
      const result = await runCopilotSDKRepoProcess(options(script, { args: ["--input-type=commonjs", "-e", script, "--", ...argv], cwd, env: { ONLY_POLICY: "supplied" } }));
      expect(result.exitCode).toBe(0);
      expect(JSON.parse(result.stdout)).toEqual({ executable: process.execPath, argv, cwd, selected: "supplied", inherited: null, input: "" });
      expect(result.stderr).toBe("private diagnostic");
    } finally {
      vi.unstubAllEnvs();
      await rm(directory, { recursive: true, force: true });
    }
  });

  it("returns a real nonzero exit and rejects a real missing executable", async () => {
    const directory = await mkdtemp(join(tmpdir(), "gh-aw-repo-process-"));
    try {
      await expect(runCopilotSDKRepoProcess(options('process.stdout.write("out"); process.stderr.write("err"); process.exitCode = 37;'))).resolves.toEqual({ exitCode: 37, stdout: "out", stderr: "err" });
      await expect(runCopilotSDKRepoProcess(options("", { command: join(directory, "missing-executable"), args: [] }))).rejects.toMatchObject({ code: "ENOENT" });
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  });

  it.each(["abort", "timeout", "overflow", ...(process.platform === "win32" ? [] : ["normal-ignored-stdio"])])(
    "cleans an orphaned grandchild on %s",
    async reason => {
      const directory = await mkdtemp(join(tmpdir(), "gh-aw-repo-tree-"));
      const parentFile = join(directory, "parent.pid");
      const grandchildFile = join(directory, "grandchild.pid");
      const triggerFile = join(directory, "overflow");
      const controller = new AbortController();
      const nativeSpawn = childProcess.spawn;
      /** @type {import("child_process").ChildProcess[]} */
      const roots = [];
      let spawnSpy;
      let observed;
      const normalExit = reason === "normal-ignored-stdio";
      try {
        spawnSpy = vi.spyOn(childProcess, "spawn").mockImplementation((command, args, spawnOptions) => {
          const child = nativeSpawn(command, args, spawnOptions);
          roots.push(child);
          return child;
        });
        const grandchildScript = `
        const fs = require("fs");
        fs.writeFileSync(${JSON.stringify(grandchildFile)}, String(process.pid));
        setInterval(() => {
          if (fs.existsSync(${JSON.stringify(triggerFile)})) process.stdout.write("x".repeat(1024));
        }, 20);
        setTimeout(() => process.exit(0), 15000);
      `;
        const script = `
        const fs = require("fs");
        fs.writeFileSync(${JSON.stringify(parentFile)}, String(process.pid));
        const child = require("child_process").spawn(process.execPath, ["--input-type=commonjs", "-e", ${JSON.stringify(grandchildScript)}], {
          cwd: process.cwd(), env: process.env, shell: false, stdio: ${JSON.stringify(normalExit ? ["ignore", "ignore", "ignore"] : ["ignore", "inherit", "inherit"])}
        });
        child.on("error", error => { throw error; });
        child.on("spawn", () => {
          if (${normalExit}) {
            setInterval(() => {
              if (fs.existsSync(${JSON.stringify(grandchildFile)}) && Number(fs.readFileSync(${JSON.stringify(grandchildFile)}, "utf8")) > 0) process.exit(37);
            }, 20);
          } else process.exit(0);
        });
        setTimeout(() => process.exit(1), 15000);
      `;
        const request = options(script, { cwd: directory, signal: controller.signal, timeoutMs: 8000, maxOutputBytes: 512 });
        if (process.platform === "win32") {
          // A .NET parent does not place its child in Node's own kill-on-exit job.
          // This exercises an orphaned descendant, not a child Node already reaped.
          const systemRoot = process.env.SystemRoot;
          if (!systemRoot) throw new Error("SystemRoot is required for the Windows lifecycle fixture");
          const grandchildScriptFile = join(directory, "grandchild.cjs");
          await writeFile(grandchildScriptFile, grandchildScript);
          const quote = value => `'${value.replace(/'/g, "''")}'`;
          const parentScript = `
          $ErrorActionPreference = 'Stop'
          [IO.File]::WriteAllText(${quote(parentFile)}, [string]$PID)
          $start = [Diagnostics.ProcessStartInfo]::new()
          $start.FileName = ${quote(process.execPath)}
          $start.Arguments = ${quote(`"${grandchildScriptFile}"`)}
          $start.WorkingDirectory = ${quote(directory)}
          $start.UseShellExecute = $false
          $child = [Diagnostics.Process]::Start($start)
          if ($null -eq $child) { throw 'Could not start owned test grandchild' }
          $child.Dispose()
          exit 0
        `;
          request.command = join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe");
          request.args = ["-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", Buffer.from(parentScript, "utf16le").toString("base64")];
          request.env = { SystemRoot: systemRoot };
        }
        observed = runCopilotSDKRepoProcess(request).then(
          result => ({ result, error: null }),
          error => ({ result: null, error })
        );
        await waitFor(async () => {
          try {
            return Number(await readFile(parentFile, "utf8")) > 0 && Number(await readFile(grandchildFile, "utf8")) > 0;
          } catch (error) {
            if (error.code === "ENOENT") return false;
            throw error;
          }
        });
        const parentPid = Number(await readFile(parentFile, "utf8"));
        const grandchildPid = Number(await readFile(grandchildFile, "utf8"));
        await waitFor(async () => !(await isRunning(parentPid)));
        if (!normalExit) expect(await isRunning(grandchildPid)).toBe(true);
        if (reason === "abort") controller.abort("lifecycle test");
        if (reason === "overflow") await writeFile(triggerFile, "overflow");
        const outcome = await observed;
        if (normalExit) {
          expect(outcome.error).toBeNull();
          expect(outcome.result).toEqual({ exitCode: 37, stdout: "", stderr: "" });
        } else {
          expect(outcome.result).toBeNull();
          expect(outcome.error).toMatchObject({ code: reason === "abort" ? "ABORT_ERR" : reason === "timeout" ? "ETIMEDOUT" : "ERR_REPO_PROCESS_OUTPUT_LIMIT", cleanupErrors: [] });
        }
        await waitFor(async () => !(await isRunning(grandchildPid)));
      } finally {
        controller.abort("test cleanup");
        spawnSpy?.mockRestore();
        const cleanupErrors = [];
        try {
          for (const root of roots) {
            try {
              if (process.platform !== "win32" && root.pid) process.kill(-root.pid, "SIGKILL");
              else if (root.exitCode === null && root.signalCode === null) root.kill("SIGKILL");
            } catch (error) {
              if (error.code !== "ESRCH") cleanupErrors.push(error);
            } finally {
              root.stdout?.destroy();
              root.stderr?.destroy();
              if (root.connected) root.disconnect();
            }
          }
          for (const file of [grandchildFile, parentFile]) {
            try {
              const pid = Number(await readFile(file, "utf8"));
              if (!Number.isSafeInteger(pid) || pid <= 0) throw new Error("Invalid owned test PID");
              if (await isRunning(pid)) process.kill(pid, "SIGKILL");
              await waitFor(async () => !(await isRunning(pid)));
            } catch (error) {
              if (error.code !== "ENOENT" && error.code !== "ESRCH") cleanupErrors.push(error);
            }
          }
          await waitFor(async () => roots.every(root => root.pid === undefined || root.exitCode !== null || root.signalCode !== null));
          await awaitCleanup(observed);
        } finally {
          await rm(directory, { recursive: true, force: true });
        }
        if (cleanupErrors.length) throw new AggregateError(cleanupErrors, "Could not clean all owned test processes");
      }
    },
    20000
  );
});
