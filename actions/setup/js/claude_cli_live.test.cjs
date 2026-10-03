import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { randomUUID } from "node:crypto";

const { CLAUDE_RESUME_PROMPT, claudeBareCapabilities, removeClaudePlugin } = require("./claude_runtime.cjs");
const cli = process.env.GH_AW_CLAUDE_TEST_CLI;

async function fixture(run) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "claude-cli-contract-"));
  const requests = [];
  const mcpCalls = [];
  let respond = () => ({ type: "text", text: "Local fixture complete" });
  const server = http.createServer(async (req, res) => {
    let raw = "";
    for await (const chunk of req) raw += chunk;
    if (req.url === "/mcp") {
      if (req.method !== "POST") {
        res.writeHead(405).end();
        return;
      }
      const message = JSON.parse(raw);
      if (message.id === undefined) {
        res.writeHead(202).end();
        return;
      }
      let result = {};
      if (message.method === "initialize") result = { protocolVersion: message.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: "fixture", version: "1.0.0" } };
      else if (message.method === "tools/list") result = { tools: [{ name: "noop", description: "Synthetic noop fixture", inputSchema: { type: "object", properties: {}, additionalProperties: false } }] };
      else if (message.method === "tools/call") {
        mcpCalls.push(message.params.name);
        result = { content: [{ type: "text", text: "Synthetic MCP result" }] };
      }
      res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ jsonrpc: "2.0", id: message.id, result }));
      return;
    }
    if (!req.url.startsWith("/v1/messages")) {
      res.writeHead(200, { "Content-Type": "application/json" }).end("{}");
      return;
    }
    const body = JSON.parse(raw);
    requests.push(body);
    const block = respond(body);
    if (block.type === "fixture_error") {
      res.writeHead(529, { "Content-Type": "application/json" }).end(JSON.stringify({ type: "error", error: { type: "overloaded_error", message: "Synthetic transient fixture failure" } }));
      return;
    }
    const stopReason = block.type === "tool_use" ? "tool_use" : "end_turn";
    const message = { id: `msg_${randomUUID()}`, type: "message", role: "assistant", model: body.model, content: [block], stop_reason: stopReason, stop_sequence: null, usage: { input_tokens: 10, output_tokens: 5 } };
    if (!body.stream) {
      res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify(message));
      return;
    }
    const event = (type, data) => res.write(`event: ${type}\ndata: ${JSON.stringify({ type, ...data })}\n\n`);
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    event("message_start", { message: { ...message, content: [], stop_reason: null } });
    event("content_block_start", { index: 0, content_block: block.type === "text" ? { type: "text", text: "" } : { ...block, input: {} } });
    event("content_block_delta", { index: 0, delta: block.type === "text" ? { type: "text_delta", text: block.text } : { type: "input_json_delta", partial_json: JSON.stringify(block.input) } });
    event("content_block_stop", { index: 0 });
    event("message_delta", { delta: { stop_reason: stopReason, stop_sequence: null }, usage: { output_tokens: 5 } });
    event("message_stop", {});
    res.end();
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Claude fixture server did not start");
  const baseArgs = ["--bare", "--print", "--no-chrome", "--strict-mcp-config", "--verbose", "--output-format", "stream-json", "--permission-mode", "dontAsk", "--max-turns", "3"];
  async function execute(args, stdin) {
    const invocation = promisify(execFile)(cli, args, {
      cwd: dir,
      env: {
        PATH: process.env.PATH,
        HOME: dir,
        TMPDIR: dir,
        CLAUDE_CONFIG_DIR: path.join(dir, "config"),
        ANTHROPIC_BASE_URL: `http://127.0.0.1:${address.port}`,
        ANTHROPIC_API_KEY: "synthetic-loopback-only",
        ANTHROPIC_MODEL: "claude-sonnet-4-6",
        DISABLE_TELEMETRY: "1",
        DISABLE_ERROR_REPORTING: "1",
        CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1",
        CLAUDE_CODE_DISABLE_FAST_MODE: "1",
        CLAUDE_CODE_MAX_RETRIES: "0",
      },
      timeout: 20000,
      maxBuffer: 2 * 1024 * 1024,
    });
    invocation.child.stdin.end(stdin);
    const { stdout } = await invocation;
    return stdout.split(/\r?\n/).flatMap(line => {
      try {
        return [JSON.parse(line)];
      } catch {
        return [];
      }
    });
  }
  try {
    await run({
      dir,
      requests,
      mcpCalls,
      baseUrl: `http://127.0.0.1:${address.port}`,
      baseArgs,
      execute,
      respond: callback => {
        respond = callback;
      },
    });
  } finally {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

describe.runIf(cli)("Claude Code native CLI contracts (synthetic loopback API only)", () => {
  it("bounds transient API retries using the documented Claude CLI environment variable", async () => {
    await fixture(async ({ baseArgs, execute, respond, requests }) => {
      respond(() => ({ type: "fixture_error" }));
      let failure;
      try {
        await execute(baseArgs, "Synthetic transient failure");
      } catch (error) {
        failure = error;
      }
      expect(failure).toMatchObject({ code: 1 });
      expect(requests).toHaveLength(1);
      const terminal = failure.stdout
        .split(/\r?\n/)
        .flatMap(line => {
          try {
            return [JSON.parse(line)];
          } catch {
            return [];
          }
        })
        .find(record => record.type === "result");
      expect(terminal.is_error).toBe(true);
      expect(terminal.session_id).toEqual(expect.any(String));
      respond(() => ({ type: "text", text: "Recovered synthetic task" }));
      const resumed = await execute([...baseArgs, "--resume", terminal.session_id], CLAUDE_RESUME_PROMPT);
      expect(resumed.find(record => record.type === "result")).toMatchObject({ session_id: terminal.session_id, is_error: false });
    });
  }, 30000);

  it("reads stdin and resumes the exact session with continuation input", async () => {
    await fixture(async ({ baseArgs, execute, requests }) => {
      const initial = await execute(baseArgs, "Synthetic task with no repository data");
      const init = initial.find(record => record.type === "system" && record.subtype === "init");
      expect(init.permissionMode).toBe("dontAsk");
      expect(init.tools).not.toContain("Task");
      expect(init.tools).not.toContain("Agent");
      expect(init.tools).not.toContain("Skill");
      expect(initial.find(record => record.type === "result").is_error).toBe(false);
      const resumed = await execute([...baseArgs, "--resume", init.session_id], CLAUDE_RESUME_PROMPT);
      expect(resumed.find(record => record.type === "system" && record.subtype === "init").session_id).toBe(init.session_id);
      expect(resumed.find(record => record.type === "result").is_error).toBe(false);
      const messages = requests.at(-1).messages;
      expect(JSON.stringify(messages)).toContain("Synthetic task with no repository data");
      expect(JSON.stringify(messages.at(-1))).toContain(CLAUDE_RESUME_PROMPT);
    });
  }, 30000);

  it("honors absolute memory Edit grants and repository deny rules under a permission override", async () => {
    await fixture(async ({ dir, baseArgs, execute, respond }) => {
      const memory = path.join(dir, "memory", "nested");
      fs.mkdirSync(memory, { recursive: true });
      for (const denied of [false, true]) {
        const file = path.join(denied ? dir : memory, "sample.txt");
        fs.writeFileSync(file, "before");
        let turn = 0;
        respond(() => {
          const index = turn++;
          if (index === 0) return { type: "tool_use", id: "read-fixture", name: "Read", input: { file_path: file } };
          if (index === 1) return { type: "tool_use", id: "edit-fixture", name: "Edit", input: { file_path: file, old_string: "before", new_string: "after" } };
          return { type: "text", text: "Fixture finished" };
        });
        const absolute = value => `//${value.replace(/^\/+/, "")}/**`;
        const args = denied
          ? [...baseArgs, "--permission-mode", "acceptEdits", "--allowed-tools", "Edit", "--disallowed-tools", `Edit(${absolute(dir)}),Write,MultiEdit,NotebookEdit`]
          : [...baseArgs, "--allowed-tools", `Edit(${absolute(path.dirname(memory))})`];
        const events = await execute(args, "Perform the synthetic file fixture");
        expect(fs.readFileSync(file, "utf8")).toBe(denied ? "before" : "after");
        const toolResult = events.flatMap(event => (event.type === "user" ? (event.message?.content ?? []) : [])).find(block => block.tool_use_id === "edit-fixture");
        expect(toolResult).toBeDefined();
        expect(toolResult.is_error === true).toBe(denied);
      }
    });
  }, 30000);

  it("registers and invokes an explicitly supplied bare-mode workflow skill", async () => {
    await fixture(async ({ dir, baseArgs, execute, requests }) => {
      const managed = path.join(dir, "managed");
      fs.mkdirSync(path.join(managed, "skills", "example"), { recursive: true });
      fs.writeFileSync(path.join(managed, "skills/example/SKILL.md"), "---\nname: example\ndescription: Synthetic fixture skill\n---\nSYNTHETIC_WORKFLOW_SKILL_CONTEXT\n");
      const loaded = claudeBareCapabilities(baseArgs, managed, dir);
      try {
        const events = await execute(loaded.args, "/gh-aw-workflow:example");
        const init = events.find(record => record.type === "system" && record.subtype === "init");
        expect(init.plugins.some(plugin => plugin.name === "gh-aw-workflow")).toBe(true);
        expect(init.skills).toContain("gh-aw-workflow:example");
        expect(JSON.stringify(requests.at(-1).messages)).toContain("SYNTHETIC_WORKFLOW_SKILL_CONTEXT");
      } finally {
        removeClaudePlugin(loaded.pluginDir);
      }
    });
  }, 30000);

  it("connects an explicit HTTP MCP server in strict bare mode", async () => {
    await fixture(async ({ dir, baseUrl, baseArgs, execute, respond, mcpCalls }) => {
      const config = path.join(dir, "mcp.json");
      fs.writeFileSync(config, JSON.stringify({ mcpServers: { fixture: { type: "http", url: `${baseUrl}/mcp` } } }));
      let turn = 0;
      respond(() => (turn++ === 0 ? { type: "tool_use", id: "mcp-fixture", name: "mcp__fixture__noop", input: {} } : { type: "text", text: "MCP fixture finished" }));
      const events = await execute([...baseArgs, "--allowed-tools", "mcp__fixture__noop", "--mcp-config", config], "Call the synthetic MCP fixture");
      const init = events.find(record => record.type === "system" && record.subtype === "init");
      expect(init.mcp_servers).toContainEqual(expect.objectContaining({ name: "fixture", status: "connected" }));
      expect(mcpCalls).toEqual(["noop"]);
    });
  }, 30000);
});
