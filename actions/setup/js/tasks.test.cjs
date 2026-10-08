import { describe, it } from "vitest";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");
const { parseTaskManifest, loadTaskManifest, MAX_TASK_OUTPUT_BYTES } = require("./tasks_config.cjs");
const { taskEnvironment, resolveTaskExecutable, runTaskProcess } = require("./tasks_process.cjs");
const { startTasksServer } = require("./tasks_mcp_server.cjs");
const { tasksMCPConfig, addTasksMCPServer, runTasksRuntime } = require("./tasks_runtime.cjs");
const { loadCopilotSDKMCPConfig } = require("./copilot_sdk_mcp_config.cjs");
const { restrictTaskCatalog } = require("./copilot_sdk_task_catalog.cjs");
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");
const { preparePiRuntime } = require("./pi_runtime.cjs");

const task = (code, args = [], timeout = 10) => ({ description: "Check source", command: "node", args: ["-e", code, "--", ...args], timeout });
const temporary = () => fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "tasks-test-")));

async function execute(definition, signal = new AbortController().signal) {
  const cwd = temporary();
  try {
    return await runTaskProcess(process.execPath, definition, { cwd, env: taskEnvironment(cwd, process.env, cwd), signal });
  } finally {
    fs.rmSync(cwd, { recursive: true });
  }
}

async function withServer(callback) {
  const cwd = temporary();
  const definitions = {
    check: task("process.stdout.write(JSON.stringify({args:process.argv.slice(1),secret:process.env.OPENAI_API_KEY,cwd:process.cwd()}))", ["$(touch unwanted)", "a b", "*"]),
    fail: task("process.exit(7)"),
    slow: task("setTimeout(()=>{},10000)"),
  };
  const server = await startTasksServer({
    tasks: parseTaskManifest({ version: 1, tasks: definitions }),
    executables: Object.fromEntries(Object.keys(definitions).map(name => [name, process.execPath])),
    cwd,
    env: taskEnvironment(cwd, { ...process.env, OPENAI_API_KEY: "not-inherited" }, cwd),
    token: crypto.randomBytes(32).toString("hex"),
    signal: new AbortController().signal,
    log: () => {},
  });
  let id = 0;
  const rpc = async (method, params, headers = server.config.headers) => {
    const response = await fetch(server.config.url, {
      method: "POST",
      headers: { ...headers, "Content-Type": "application/json" },
      body: JSON.stringify({ jsonrpc: "2.0", id: ++id, method, params }),
    });
    return { response, body: response.status === 200 ? await response.json() : undefined, id };
  };
  try {
    await callback({ server, rpc, cwd });
  } finally {
    await server.close();
    fs.rmSync(cwd, { recursive: true });
  }
}

describe("task manifests and environment", () => {
  it("validates and freezes fixed definitions", () => {
    const parsed = parseTaskManifest({ version: 1, tasks: { check: task("process.exit(0)") } });
    assert(Object.isFrozen(parsed.check.args));
    for (const replacement of [{ command: "./node" }, { timeout: 0 }, { args: ["${{ github.sha }}"] }, { env: {} }, { args: null }]) {
      assert.throws(() => parseTaskManifest({ version: 1, tasks: { check: { ...task(""), ...replacement } } }));
    }
    assert.throws(() => parseTaskManifest({ version: 1, tasks: { constructor: task("") } }));
    assert.throws(() => parseTaskManifest({ version: 2, tasks: { check: task("") } }));
  });
  it("bounds file input and rejects symlinks", () => {
    const dir = temporary();
    try {
      const filename = path.join(dir, "manifest.json");
      fs.writeFileSync(filename, JSON.stringify({ version: 1, tasks: { check: task("") } }));
      assert.equal(loadTaskManifest(filename).check.command, "node");
      fs.symlinkSync(filename, path.join(dir, "link"));
      assert.throws(() => loadTaskManifest(path.join(dir, "link")));
      fs.writeFileSync(filename, " ".repeat(1024 * 1024 + 1));
      assert.throws(() => loadTaskManifest(filename), /1 MiB/);
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });
  it("excludes credentials, loader settings, command files, and checkout executables", () => {
    const dir = temporary();
    try {
      const source = { PATH: `${dir}${path.delimiter}/nonexistent/tasks-path${path.delimiter}${path.dirname(process.execPath)}`, OPENAI_API_KEY: "secret", GITHUB_TOKEN: "secret", GITHUB_OUTPUT: "output", NODE_OPTIONS: "--require evil" };
      const env = taskEnvironment(dir, source, dir);
      for (const name of ["OPENAI_API_KEY", "GITHUB_TOKEN", "GITHUB_OUTPUT", "NODE_OPTIONS"]) assert.equal(env[name], undefined);
      assert.equal(env.GOPROXY, "off");
      assert.equal(env.GOFLAGS, "-mod=readonly");
      assert.equal(env.PATH, path.dirname(process.execPath));
      fs.symlinkSync(process.execPath, path.join(dir, "node"));
      assert.equal(resolveTaskExecutable("node", dir, source), fs.realpathSync(process.execPath));
      fs.writeFileSync(path.join(dir, "unsafe"), "#!/bin/sh\nexit 0\n", { mode: 0o755 });
      assert.throws(() => resolveTaskExecutable("unsafe", dir, source), /outside the checkout/);
      assert.throws(() => resolveTaskExecutable("missing-executable", dir, source), /prepare it/);
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });
});

describe("bounded task processes", () => {
  it("terminates descendants in the task process group when the leader exits", async () => {
    const result = await execute(task("const {spawn}=require('node:child_process');const child=spawn(process.execPath,['-e','setTimeout(()=>{},10000)'],{stdio:'ignore'});console.log(child.pid);child.unref()"));
    const pid = Number(result.stdout.trim());
    assert(Number.isSafeInteger(pid) && pid > 0);
    await new Promise(resolve => setTimeout(resolve, 100));
    try {
      assert.throws(() => process.kill(pid, 0), { code: "ESRCH" });
    } finally {
      try {
        process.kill(pid, "SIGKILL");
      } catch (error) {
        if (!(error instanceof Error) || !("code" in error) || error.code !== "ESRCH") throw error;
      }
    }
  });
  it("preserves literal argv and reports nonzero status", async () => {
    const result = await execute(task("console.log(JSON.stringify(process.argv.slice(1)));process.exit(7)", ["$(touch unwanted)", "a b", "*"]));
    assert.deepEqual(JSON.parse(result.stdout), ["$(touch unwanted)", "a b", "*"]);
    assert.equal(result.exitCode, 7);
  });
  it("accepts exactly the combined output budget and rejects exceeding it", async () => {
    const exact = await execute(task(`process.stdout.write(Buffer.alloc(${MAX_TASK_OUTPUT_BYTES},97))`));
    assert.equal(Buffer.byteLength(exact.stdout), MAX_TASK_OUTPUT_BYTES);
    await assert.rejects(execute(task(`process.stdout.write(Buffer.alloc(${MAX_TASK_OUTPUT_BYTES + 1},97))`)), /output limit/);
  });
  it("enforces timeout and cancellation", async () => {
    await assert.rejects(execute(task("setTimeout(()=>{},10000)", [], 1)), /timed out/);
    const controller = new AbortController();
    const running = execute(task("setTimeout(()=>{},10000)"), controller.signal);
    const timer = setTimeout(() => controller.abort(), 100);
    try {
      await assert.rejects(running, /cancelled/);
    } finally {
      clearTimeout(timer);
    }
  });
});

describe("native tasks MCP", () => {
  it("accepts cancellation notifications without adding execution parameters", async () =>
    withServer(async ({ server, rpc }) => {
      const running = rpc("tools/call", { name: "run_task", arguments: { name: "slow" } });
      await new Promise(resolve => setTimeout(resolve, 100));
      const cancelled = await fetch(server.config.url, {
        method: "POST",
        headers: { ...server.config.headers, "Content-Type": "application/json" },
        body: JSON.stringify({ jsonrpc: "2.0", method: "notifications/cancelled", params: { requestId: 1 } }),
      });
      assert.equal(cancelled.status, 202);
      assert.equal((await running).body.result.isError, true);
    }));
  it("requires authentication and exposes only run_task", async () =>
    withServer(async ({ rpc }) => {
      assert.equal((await rpc("tools/list", {}, {})).response.status, 401);
      assert.equal((await rpc("tools/list", {}, { Authorization: "Bearer " + "é".repeat(32) + "a".repeat(32) })).response.status, 401);
      const listed = await rpc("tools/list");
      assert.deepEqual(
        listed.body.result.tools.map(tool => tool.name),
        ["run_task"]
      );
      assert.equal(listed.body.result.tools[0].inputSchema.additionalProperties, false);
      assert.equal((await rpc("initialize")).body.result.protocolVersion, "2025-03-26");
    }));
  it("executes fixed argv in the workspace with restricted environment", async () =>
    withServer(async ({ rpc, cwd }) => {
      const result = await rpc("tools/call", { name: "run_task", arguments: { name: "check" } });
      assert.equal(result.body.result.isError, false);
      const execution = JSON.parse(result.body.result.content[0].text);
      const output = JSON.parse(execution.stdout);
      assert.deepEqual(output.args, ["$(touch unwanted)", "a b", "*"]);
      assert.equal(output.secret, undefined);
      assert.equal(output.cwd, cwd);
      assert.equal(fs.existsSync(path.join(cwd, "unwanted")), false);
      assert.equal((await rpc("tools/call", { name: "run_task", arguments: { name: "fail" } })).body.result.isError, true);
    }));
  it("rejects unknown tasks, execution overrides, and arbitrary methods", async () =>
    withServer(async ({ rpc }) => {
      for (const args of [{ name: "unknown" }, { name: "check", args: ["injected"] }, { name: "check", cwd: "/tmp" }, { name: "check", env: {} }]) {
        assert((await rpc("tools/call", { name: "run_task", arguments: args })).body.error);
      }
      assert((await rpc("resources/read")).body.error);
    }));
  it("cancels active work during server shutdown", async () =>
    withServer(async ({ server, rpc }) => {
      const result = rpc("tools/call", { name: "run_task", arguments: { name: "slow" } });
      let closing;
      const close = setTimeout(() => {
        closing = server.close();
      }, 100);
      try {
        const execution = await result;
        assert.equal(execution.body.result.isError, true);
      } finally {
        clearTimeout(close);
        if (closing) await closing;
      }
    }));
});

describe("engine task configuration", () => {
  it("injects a direct native Pi endpoint without accepting exposure overrides", () => {
    const dir = temporary();
    const saved = Object.fromEntries(["RUNNER_TEMP", "PI_CODING_AGENT_DIR", "GH_AW_LOCKED_TASKS_MCP"].map(key => [key, process.env[key]]));
    try {
      process.env.RUNNER_TEMP = dir;
      process.env.PI_CODING_AGENT_DIR = path.join(dir, "agent");
      process.env.GH_AW_LOCKED_TASKS_MCP = JSON.stringify({ type: "http", url: "http://127.0.0.1:1234/mcp", headers: { Authorization: "Bearer " + "a".repeat(64) } });
      preparePiRuntime({});
      const config = JSON.parse(fs.readFileSync(path.join(dir, "agent/mcp.json"), "utf8"));
      assert.equal(config.mcpServers["locked-tasks"].exposure, "direct");
      assert.deepEqual(config.mcpServers["locked-tasks"].tools, ["run_task"]);
      assert.throws(() => preparePiRuntime({ mcp: { exposure: "hidden" } }), /cannot be hidden/);
    } finally {
      for (const [key, value] of Object.entries(saved)) {
        if (value === undefined) delete process.env[key];
        else process.env[key] = value;
      }
      fs.rmSync(dir, { recursive: true });
    }
  });
  it("reserves native tasks configuration", () => {
    const raw = JSON.stringify({ type: "http", url: "http://127.0.0.1:1234/mcp", headers: { Authorization: "Bearer " + "a".repeat(64) } });
    assert.deepEqual(tasksMCPConfig(raw).tools, ["run_task"]);
    assert.throws(() => addTasksMCPServer({ "locked-tasks": {} }, raw), /reserved/);
    assert.throws(() => tasksMCPConfig(raw.replace("127.0.0.1", "example.com")), /loopback/);
    assert.throws(() => tasksMCPConfig(JSON.stringify({ ...JSON.parse(raw), command: "evil" })), /Invalid/);
  });
  it("supervises engine startup and shutdown using compiler-owned inputs", async () => {
    const dir = temporary();
    try {
      const workspace = path.join(dir, "checkout");
      fs.mkdirSync(workspace);
      const manifest = path.join(dir, "manifest.json");
      fs.writeFileSync(manifest, JSON.stringify({ version: 1, tasks: { check: task("") } }));
      const code =
        "const c=JSON.parse(process.env.GH_AW_LOCKED_TASKS_MCP);fetch(c.url,{method:'POST',headers:{...c.headers,'Content-Type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})}).then(r=>r.json()).then(r=>process.exit(r.result.tools[0].name==='run_task'?0:1))";
      const mounts = [];
      assert.equal(await runTasksRuntime(manifest, [process.execPath, "-e", code], { env: { ...process.env, GITHUB_WORKSPACE: workspace }, verifyMount: name => mounts.push(name) }), 0);
      assert.equal(mounts.length, 2);
      await assert.rejects(runTasksRuntime(manifest, [process.execPath], { env: { ...process.env, GITHUB_WORKSPACE: dir }, verifyMount: () => {} }), /outside the checkout/);
      await assert.rejects(
        runTasksRuntime(manifest, [process.execPath], {
          env: { ...process.env, GITHUB_WORKSPACE: workspace },
          verifyMount: () => {
            throw new Error("not read-only");
          },
        }),
        /not read-only/
      );
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });
  it("loads converted gateway servers without shell transports", () => {
    const dir = temporary();
    try {
      const filename = path.join(dir, "mcp.json");
      fs.writeFileSync(filename, JSON.stringify({ mcpServers: { github: { url: "http://gateway/mcp/github", headers: { Authorization: "scoped" } } } }));
      assert.equal(loadCopilotSDKMCPConfig(filename).github.type, "http");
      fs.writeFileSync(filename, JSON.stringify({ mcpServers: { bad: { command: "bash" } } }));
      assert.throws(() => loadCopilotSDKMCPConfig(filename), /HTTP\/SSE/);
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });
  it("restricts the SDK catalog and authorizes canonical names through metadata", async () => {
    class ToolSet {
      items = [];
      addMcp(name) {
        this.items.push(`mcp:${name}`);
      }
      toArray() {
        return this.items;
      }
    }
    const allowed = { name: "locked-tasks-run_task", mcpServerName: "locked-tasks", mcpToolName: "run_task" };
    const extra = { name: "github-delete", mcpServerName: "github", mcpToolName: "delete" };
    let restricted = false;
    const session = {
      rpc: {
        tools: { initializeAndValidate: async () => {}, getCurrentMetadata: async () => ({ tools: restricted ? [allowed] : [allowed, extra] }) },
        options: {
          update: async input => {
            assert.deepEqual(input.availableTools, ["mcp:locked-tasks-run_task"]);
            restricted = true;
            return { success: true };
          },
        },
      },
    };
    const metadata = await restrictTaskCatalog(session, { ToolSet, availableTools: new ToolSet(), allowedTools: ["locked-tasks(run_task)"], mcpServers: { "locked-tasks": { tools: ["run_task"] } } });
    assert.deepEqual(metadata, [allowed]);
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["locked-tasks(run_task)"] }, () => ({ kind: "approve-once" }), { getMCPToolMetadata: () => metadata });
    assert.equal(handler({ kind: "mcp", serverName: "locked-tasks", toolName: "locked-tasks-run_task" }).kind, "approve-once");
    assert.equal(handler({ kind: "mcp", serverName: "locked-tasks", toolName: "run_task" }).kind, "reject");
    assert.equal(handler({ kind: "mcp", serverName: "github", toolName: "locked-tasks-run_task" }).kind, "reject");
  });
});
