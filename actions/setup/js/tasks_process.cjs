// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { spawn } = require("node:child_process");
const { MAX_TASK_OUTPUT_BYTES } = require("./tasks_config.cjs");

/** @param {string} root @param {string} candidate */
function within(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative === "" || (relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative));
}

/** @param {string} command @param {string} root @param {NodeJS.ProcessEnv} env */
function resolveTaskExecutable(command, root, env) {
  for (const directory of (env.PATH || "").split(path.delimiter)) {
    if (!path.isAbsolute(directory)) continue;
    let executable;
    try {
      executable = fs.realpathSync(path.join(directory, command));
    } catch (error) {
      if (["ENOENT", "ENOTDIR"].includes(error.code)) continue;
      throw error;
    }
    if (within(root, executable)) throw new Error(`Task executable ${command} must be prepared outside the checkout`);
    if (!fs.statSync(executable).isFile()) throw new Error(`Task executable ${command} is not a regular file`);
    fs.accessSync(executable, fs.constants.X_OK);
    return executable;
  }
  throw new Error(`Task executable ${command} is missing; prepare it in workflow setup`);
}

/** @param {string} root @param {NodeJS.ProcessEnv} source @param {string} home */
function taskEnvironment(root, source, home) {
  /** @type {NodeJS.ProcessEnv} */
  const env = {
    HOME: home,
    TMPDIR: home,
    LANG: "C.UTF-8",
    CI: "true",
    GIT_TERMINAL_PROMPT: "0",
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_ALLOW_PROTOCOL: "",
    GIT_NO_LAZY_FETCH: "1",
    GOENV: "off",
    GOTOOLCHAIN: "local",
    GOFLAGS: "-mod=readonly",
    GOWORK: "off",
    GOPROXY: "off",
    GOSUMDB: "off",
  };
  env.PATH = (source.PATH || "")
    .split(path.delimiter)
    .filter(directory => {
      if (!path.isAbsolute(directory)) return false;
      try {
        return !within(root, fs.realpathSync(directory));
      } catch (error) {
        if (["ENOENT", "ENOTDIR"].includes(error.code)) return false;
        throw error;
      }
    })
    .join(path.delimiter);
  for (const key of ["GOROOT", "GOPATH", "GOCACHE", "GOMODCACHE"]) {
    const value = source[key];
    if (value) {
      if (!path.isAbsolute(value) || within(root, fs.realpathSync(value))) throw new Error(`Tasks ${key} must be a prepared absolute directory outside the checkout`);
      env[key] = value;
    }
  }
  return env;
}

/** @param {import("node:child_process").ChildProcess} child */
function killTaskProcess(child) {
  if (!child.pid) return;
  try {
    process.kill(-child.pid, "SIGKILL");
  } catch (error) {
    if (error.code !== "ESRCH") throw error;
  }
}

/**
 * @param {string} executable
 * @param {import("./tasks_config.cjs").TaskDefinition} task
 * @param {{cwd: string, env: NodeJS.ProcessEnv, signal: AbortSignal}} options
 * @returns {Promise<{exitCode: number, stdout: string, stderr: string, durationMs: number}>}
 */
function runTaskProcess(executable, task, { cwd, env, signal }) {
  if (process.platform === "win32") throw new Error("Sandbox tasks require POSIX process groups");
  signal.throwIfAborted();
  const start = Date.now();
  return new Promise((resolve, reject) => {
    const child = spawn(executable, task.args, { cwd, env, shell: false, detached: true, stdio: ["ignore", "pipe", "pipe"] });
    /** @type {Buffer[]} */
    const stdout = [];
    /** @type {Buffer[]} */
    const stderr = [];
    let bytes = 0;
    /** @type {Error | undefined} */
    let failure;
    let settled = false;
    /** @type {NodeJS.Timeout | undefined} */
    let cleanupTimer;
    function cleanupListeners() {
      clearTimeout(timer);
      clearTimeout(cleanupTimer);
      signal.removeEventListener("abort", abort);
    }
    /** @param {Error} error */
    function stop(error) {
      if (settled || failure) return;
      failure = error;
      try {
        killTaskProcess(child);
      } catch (cleanupError) {
        failure = new AggregateError([error, cleanupError], "Task execution and process cleanup failed");
      }
      cleanupTimer = setTimeout(() => {
        if (settled) return;
        settled = true;
        cleanupListeners();
        child.stdout?.destroy();
        child.stderr?.destroy();
        reject(new AggregateError([failure], "Task process did not close within the cleanup deadline"));
      }, 2000);
    }
    const abort = () => stop(new Error("Task cancelled", { cause: signal.reason }));
    const timer = setTimeout(() => stop(new Error(`Task timed out after ${task.timeout} seconds`)), task.timeout * 1000);
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) abort();
    /** @param {Buffer[]} output @param {Buffer} data */
    function collect(output, data) {
      if (failure || settled) return;
      if (data.length > MAX_TASK_OUTPUT_BYTES - bytes) return stop(new Error(`Task exceeded the combined ${MAX_TASK_OUTPUT_BYTES}-byte output limit`));
      bytes += data.length;
      output.push(data);
    }
    child.stdout?.on("data", data => collect(stdout, data)).on("error", stop);
    child.stderr?.on("data", data => collect(stderr, data)).on("error", stop);
    child.on("error", stop);
    child.on("exit", () => {
      try {
        killTaskProcess(child);
      } catch (error) {
        stop(error);
      }
    });
    child.on("close", (code, terminationSignal) => {
      if (settled) return;
      settled = true;
      cleanupListeners();
      if (failure) return reject(failure);
      const exitCode = code ?? (terminationSignal ? 128 + os.constants.signals[terminationSignal] : undefined);
      if (exitCode === undefined) return reject(new Error("Task closed without an exit status"));
      resolve({ exitCode, stdout: Buffer.concat(stdout).toString("utf8"), stderr: Buffer.concat(stderr).toString("utf8"), durationMs: Date.now() - start });
    });
  });
}

module.exports = { within, resolveTaskExecutable, taskEnvironment, killTaskProcess, runTaskProcess };
