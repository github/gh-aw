// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const { spawn } = require("node:child_process");
const { loadTaskManifest, isRecord } = require("./tasks_config.cjs");
const { within, resolveTaskExecutable, taskEnvironment, killTaskProcess } = require("./tasks_process.cjs");
const { startTasksServer } = require("./tasks_mcp_server.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string} filename */
function requireReadOnlyMount(filename) {
  if (process.platform !== "linux") throw new Error("Tasks require the AWF Linux sandbox");
  const target = fs.realpathSync(filename);
  const mounts = fs
    .readFileSync("/proc/self/mountinfo", "utf8")
    .trim()
    .split("\n")
    .map(line => {
      const fields = line.split(" ");
      return { path: fields[4].replace(/\\([0-7]{3})/g, (_, octal) => String.fromCharCode(parseInt(octal, 8))), options: fields[5].split(",") };
    });
  const mount = mounts.filter(entry => within(entry.path, target)).sort((a, b) => b.path.length - a.path.length)[0];
  if (!mount?.options.includes("ro")) throw new Error(`Tasks require a read-only sandbox mount for ${filename}`);
}

/**
 * @param {string | undefined} raw
 * @returns {{type: "http", url: string, headers: {Authorization: string}, tools: string[], timeout: number} | undefined}
 */
function tasksMCPConfig(raw = process.env.GH_AW_TASKS_MCP) {
  if (raw === undefined) return undefined;
  const config = JSON.parse(raw);
  if (!isRecord(config) || Object.keys(config).some(key => !["type", "url", "headers", "tools", "timeout"].includes(key)) || config.type !== "http" || typeof config.url !== "string" || !isRecord(config.headers))
    throw new Error("Invalid compiler-owned tasks MCP configuration");
  const url = new URL(config.url);
  if (url.protocol !== "http:" || url.hostname !== "127.0.0.1" || !url.port || url.pathname !== "/mcp" || url.search || url.hash || url.username || url.password) throw new Error("Tasks MCP must use a private loopback endpoint");
  const authorization = config.headers.Authorization;
  if (typeof authorization !== "string" || !/^Bearer [a-f0-9]{64}$/.test(authorization) || Object.keys(config.headers).length !== 1) throw new Error("Tasks MCP requires its per-run authorization token");
  return { type: "http", url: config.url, headers: { Authorization: authorization }, tools: ["run_task"], timeout: 605_000 };
}

/** @param {Record<string, unknown>} servers @param {string | undefined} [raw] */
function addTasksMCPServer(servers, raw = process.env.GH_AW_TASKS_MCP) {
  const tasks = tasksMCPConfig(raw);
  if (!tasks) return servers;
  if (Object.hasOwn(servers, "tasks")) throw new Error("The MCP server name tasks is reserved");
  return { ...servers, tasks };
}

/**
 * @param {string} manifestPath
 * @param {string[]} command
 * @param {{env?: NodeJS.ProcessEnv, verifyMount?: (filename: string) => void}} [options]
 */
async function runTasksRuntime(manifestPath, command, { env = process.env, verifyMount = requireReadOnlyMount } = {}) {
  if (command.length === 0) throw new Error("Tasks runtime requires the compiler-owned engine command");
  if (!env.GITHUB_WORKSPACE) throw new Error("Tasks runtime requires GITHUB_WORKSPACE");
  const root = fs.realpathSync(env.GITHUB_WORKSPACE);
  if (within(root, fs.realpathSync(manifestPath)) || within(root, fs.realpathSync(__dirname))) throw new Error("Task manifest and server code must be outside the checkout");
  verifyMount(manifestPath);
  verifyMount(__filename);
  const tasks = loadTaskManifest(manifestPath);
  const privateRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-tasks-"));
  fs.chmodSync(privateRoot, 0o700);
  const controller = new AbortController();
  /** @type {ReturnType<typeof spawn> | undefined} */
  let child;
  /** @type {Awaited<ReturnType<typeof startTasksServer>> | undefined} */
  let server;
  /** @type {NodeJS.Timeout | undefined} */
  let killTimer;
  let abortEngine = () => {};
  const stop = () => {
    controller.abort(new Error("Agent execution interrupted"));
  };
  try {
    const taskEnv = taskEnvironment(root, env, privateRoot);
    const executables = Object.fromEntries(Object.entries(tasks).map(([name, task]) => [name, resolveTaskExecutable(task.command, root, taskEnv)]));
    server = await startTasksServer({ tasks, executables, cwd: root, env: taskEnv, token: crypto.randomBytes(32).toString("hex"), signal: controller.signal, onError: error => controller.abort(error) });
    const ready = await fetch(server.config.url, {
      method: "POST",
      headers: { ...server.config.headers, "Content-Type": "application/json", Accept: "application/json, text/event-stream" },
      body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" }),
      signal: AbortSignal.timeout(5000),
    });
    const catalog = await ready.json();
    if (!ready.ok || !isRecord(catalog) || !isRecord(catalog.result) || !Array.isArray(catalog.result.tools) || catalog.result.tools.length !== 1 || !isRecord(catalog.result.tools[0]) || catalog.result.tools[0].name !== "run_task")
      throw new Error("Tasks MCP readiness check failed");
    process.on("SIGTERM", stop).on("SIGINT", stop);
    const engineEnv = { ...env, GH_AW_TASKS_MCP: JSON.stringify(server.config) };
    const running = spawn(command[0], command.slice(1), { env: engineEnv, stdio: "inherit", detached: true, shell: false });
    child = running;
    /** @type {Promise<number>} */
    const engineExit = new Promise((resolve, reject) => {
      running.once("error", reject);
      running.once("exit", (code, signal) => {
        try {
          killTaskProcess(running);
          resolve(code ?? (signal ? 128 + os.constants.signals[signal] : 1));
        } catch (error) {
          reject(error);
        }
      });
      running.once("close", () => clearTimeout(killTimer));
      abortEngine = () => {
        try {
          killTaskProcess(running);
          killTimer = setTimeout(() => reject(new Error("Engine did not stop after cancellation")), 2000);
        } catch (error) {
          reject(error);
        }
      };
      controller.signal.addEventListener("abort", abortEngine, { once: true });
      if (controller.signal.aborted) abortEngine();
    });
    return await engineExit;
  } finally {
    process.off("SIGTERM", stop).off("SIGINT", stop);
    controller.signal.removeEventListener("abort", abortEngine);
    clearTimeout(killTimer);
    controller.abort(new Error("Agent execution finished"));
    try {
      const cleanup = await Promise.allSettled([
        Promise.resolve().then(() => {
          if (child) killTaskProcess(child);
        }),
        server?.close(),
      ]);
      const failures = cleanup.filter(result => result.status === "rejected").map(result => result.reason);
      if (failures.length) throw new AggregateError(failures, "Tasks runtime cleanup failed");
    } finally {
      fs.rmSync(privateRoot, { recursive: true });
    }
  }
}

if (require.main === module) {
  const args = process.argv.slice(2);
  if (args[0] !== "--manifest" || !args[1] || args[2] !== "--") {
    process.stderr.write("Tasks runtime requires --manifest <path> -- <engine command>\n");
    process.exitCode = 1;
  } else {
    runTasksRuntime(args[1], args.slice(3))
      .then(code => {
        process.exitCode = code;
      })
      .catch(error => {
        process.stderr.write(`[tasks] ${getErrorMessage(error)}\n`);
        process.exitCode = 1;
      });
  }
}

module.exports = { requireReadOnlyMount, tasksMCPConfig, addTasksMCPServer, runTasksRuntime };
