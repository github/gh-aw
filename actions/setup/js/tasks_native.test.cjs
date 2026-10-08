import { describe, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const crypto = require("node:crypto");
const { execFile } = require("node:child_process");
const { promisify } = require("node:util");
const { parseTaskManifest } = require("./tasks_config.cjs");
const { taskEnvironment } = require("./tasks_process.cjs");
const { startTasksServer } = require("./tasks_mcp_server.cjs");
const { tasksMCPConfig } = require("./tasks_runtime.cjs");
const { preparePiRuntime } = require("./pi_runtime.cjs");
const { restrictTaskCatalog } = require("./copilot_sdk_task_catalog.cjs");
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");

async function withNativeTasks(callback) {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "tasks-native-test-")));
  let server;
  try {
    const cwd = path.join(root, "checkout");
    const home = path.join(root, "home");
    fs.mkdirSync(cwd);
    fs.mkdirSync(home);
    server = await startTasksServer({
      tasks: parseTaskManifest({ version: 1, tasks: { check: { description: "Check fixture", command: "node", args: ["-e", "console.log('fixture passed')"], timeout: 10 } } }),
      executables: { check: process.execPath },
      cwd,
      env: taskEnvironment(cwd, process.env, home),
      token: crypto.randomBytes(32).toString("hex"),
      signal: new AbortController().signal,
      log: () => {},
    });
    await callback({ root, cwd, home, server });
  } finally {
    if (server) await server.close();
    fs.rmSync(root, { recursive: true });
  }
}

describe("native tasks consumers", () => {
  it.skipIf(!process.env.GH_AW_TEST_COPILOT_BIN)(
    "discovers, restricts, authorizes, and executes tasks through the real SDK",
    async () =>
      withNativeTasks(async ({ cwd, home, server }) => {
        const sdk = require("@github/copilot-sdk");
        const client = new sdk.CopilotClient({
          connection: sdk.RuntimeConnection.forStdio({
            path: process.env.GH_AW_TEST_COPILOT_BIN,
            args: ["--no-auto-update", "--disable-builtin-mcps"],
            env: { PATH: process.env.PATH, HOME: home, TMPDIR: home, COPILOT_TELEMETRY_DISABLED: "1" },
          }),
          workingDirectory: cwd,
          useLoggedInUser: false,
          logLevel: "error",
        });
        let session;
        try {
          await client.start();
          const availableTools = new sdk.ToolSet();
          availableTools.addMcp("*");
          const mcpServers = { tasks: tasksMCPConfig(JSON.stringify(server.config)) };
          let metadata = [];
          session = await client.createSession({
            model: "fixture/gpt-4o",
            providers: [{ name: "fixture", type: "openai", baseUrl: "http://127.0.0.1:1/v1", apiKey: "fixture-only" }],
            models: [{ id: "gpt-4o", provider: "fixture" }],
            availableTools,
            mcpServers,
            toolSearch: { enabled: false },
            onPermissionRequest: buildCopilotSDKPermissionHandler({ allowedTools: ["tasks(run_task)"] }, sdk.approveAll, { getMCPToolMetadata: () => metadata }),
          });
          metadata = await restrictTaskCatalog(session, { ToolSet: sdk.ToolSet, availableTools, allowedTools: ["tasks(run_task)"], mcpServers });
          assert.equal(metadata.length, 1);
          assert.equal(metadata[0].mcpServerName, "tasks");
          assert.equal(metadata[0].mcpToolName, "run_task");
          const result = await session.rpc.tools.execute({ name: metadata[0].name, arguments: { name: "check" } });
          assert.equal(result.resultType, "success");
          const execution = JSON.parse(result.textResultForLlm);
          assert.equal(execution.exitCode, 0);
          assert.equal(execution.stdout, "fixture passed\n");
        } finally {
          try {
            if (session) await session.disconnect();
          } finally {
            await client.stop();
          }
        }
      }),
    60000
  );

  it.skipIf(!process.env.GH_AW_TEST_PI_BIN)(
    "discovers the compiler-generated direct tasks endpoint in real Pi",
    async () =>
      withNativeTasks(async ({ root, cwd, home, server }) => {
        const saved = Object.fromEntries(["RUNNER_TEMP", "PI_CODING_AGENT_DIR", "GH_AW_TASKS_MCP"].map(key => [key, process.env[key]]));
        try {
          process.env.RUNNER_TEMP = root;
          process.env.PI_CODING_AGENT_DIR = path.join(home, "pi-agent");
          process.env.GH_AW_TASKS_MCP = JSON.stringify(server.config);
          preparePiRuntime({});
          const result = await promisify(execFile)(process.env.GH_AW_TEST_PI_BIN, ["mcp", "list", "--json"], {
            cwd,
            env: { PATH: process.env.PATH, HOME: home, TMPDIR: home, PI_OFFLINE: "1", PI_CODING_AGENT_DIR: process.env.PI_CODING_AGENT_DIR },
            timeout: 20000,
          });
          const catalog = JSON.parse(result.stdout);
          assert.deepEqual(catalog.errors, []);
          const tasks = catalog.servers.find(entry => entry.name === "tasks");
          assert.equal(tasks.state, "connected");
          assert.equal(tasks.exposure, "direct");
          assert.deepEqual(tasks.tools, ["run_task"]);
        } finally {
          for (const [key, value] of Object.entries(saved)) {
            if (value === undefined) delete process.env[key];
            else process.env[key] = value;
          }
        }
      }),
    30000
  );
});
