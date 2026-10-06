import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import http from "node:http";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const { buildModelsJSON } = await import("./pi_models_json.cjs");
const cli = process.env.GH_AW_PI_TEST_CLI;

describe.runIf(cli)("Pi v1 real headless CLI", () => {
  it.each(["openai-completions", "openai-responses"])("loads native MCP, resources, codemode, and non-bypassable tool policy over %s without project context", async api => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-v1-live-"));
    const requests = [];
    const mcpCalls = [];
    let turn = 0;
    const server = http.createServer(async (req, res) => {
      if (req.method !== "POST") {
        res.writeHead(405);
        res.end();
        return;
      }
      let text = "";
      for await (const chunk of req) text += chunk;
      const body = JSON.parse(text);
      res.setHeader("Content-Type", "application/json");
      if (req.url === "/mcp") {
        if (body.id === undefined) {
          res.writeHead(202);
          res.end();
          return;
        }
        let result;
        if (body.method === "initialize") result = { protocolVersion: body.params.protocolVersion, capabilities: { tools: {}, resources: {} }, serverInfo: { name: "fixture", version: "1.0.0" } };
        else if (body.method === "tools/list") result = { tools: [{ name: "noop", description: "Record a fixture result", inputSchema: { type: "object", properties: {} } }] };
        else if (body.method === "tools/call") {
          mcpCalls.push(body.params.name);
          result = { content: [{ type: "text", text: "fixture noop completed" }] };
        } else if (body.method === "resources/list") result = { resources: [{ uri: "test://fixture/status", name: "status", mimeType: "text/plain" }] };
        else if (body.method === "resources/read") {
          mcpCalls.push("resource");
          result = { contents: [{ uri: body.params.uri, mimeType: "text/plain", text: "fixture status" }] };
        } else result = {};
        res.end(JSON.stringify({ jsonrpc: "2.0", id: body.id, result }));
        return;
      }
      requests.push(body);
      const calls = [
        ["bash", { command: "echo allowed" }],
        ["bash", { command: "printf blocked" }],
        ["mcp__fixture__noop", {}],
        ["read_mcp_resource", { server: "fixture", uri: "test://fixture/status" }],
        ["codemode", { code: "text(await tools.mcp__fixture__noop({}));" }],
      ];
      const call = calls[turn++];
      if (api === "openai-responses") {
        const output = call
          ? [{ type: "function_call", id: `fc_${turn}`, call_id: `call-${turn}`, name: call[0], arguments: JSON.stringify(call[1]), status: "completed" }]
          : [{ type: "message", id: `msg_${turn}`, role: "assistant", content: [{ type: "output_text", text: "Fixture complete", annotations: [] }], status: "completed" }];
        res.setHeader("Content-Type", "text/event-stream");
        const events = [
          { type: "response.created", response: { id: `response-${turn}`, status: "in_progress", output: [] } },
          { type: "response.output_item.added", output_index: 0, item: output[0] },
          { type: "response.output_item.done", output_index: 0, item: output[0] },
          { type: "response.completed", response: { id: `response-${turn}`, status: "completed", output, usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12 } } },
        ];
        res.end(events.map(event => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(""));
        return;
      }
      const message = call ? { role: "assistant", content: null, tool_calls: [{ id: `call-${turn}`, type: "function", function: { name: call[0], arguments: JSON.stringify(call[1]) } }] } : { role: "assistant", content: "Fixture complete" };
      if (body.stream) {
        res.setHeader("Content-Type", "text/event-stream");
        const delta = call ? { ...message, tool_calls: message.tool_calls.map(tool => ({ ...tool, index: 0 })) } : message;
        res.end(
          `data: ${JSON.stringify({ id: `response-${turn}`, object: "chat.completion.chunk", created: 1, model: "fixture", choices: [{ index: 0, delta, finish_reason: call ? "tool_calls" : "stop" }], usage: { prompt_tokens: 10, completion_tokens: 2, total_tokens: 12 } })}\n\ndata: [DONE]\n\n`
        );
      } else {
        res.end(
          JSON.stringify({
            id: `response-${turn}`,
            object: "chat.completion",
            created: 1,
            model: "fixture",
            choices: [{ index: 0, message, finish_reason: call ? "tool_calls" : "stop" }],
            usage: { prompt_tokens: 10, completion_tokens: 2, total_tokens: 12 },
          })
        );
      }
    });
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("Fixture server did not start");
    const baseUrl = `http://127.0.0.1:${address.port}`;
    const agentDir = path.join(dir, "agent");
    fs.mkdirSync(agentDir);
    fs.writeFileSync(
      path.join(agentDir, "models.json"),
      buildModelsJSON({ baseUrl, apiKeyEnvVar: "COPILOT_GITHUB_TOKEN", modelId: "fixture", provider: "github", api, metadata: { contextWindow: 128000, maxTokens: 1024, compat: { supportsOpenAIGrammarTools: true } } })
    );
    fs.writeFileSync(path.join(agentDir, "mcp.json"), JSON.stringify({ mcpServers: { fixture: { url: `${baseUrl}/mcp`, exposure: "direct" } } }));
    fs.writeFileSync(path.join(agentDir, "settings.json"), JSON.stringify({ defaultTools: ["+codemode", "+tool_search"], enableInstallTelemetry: false }));
    fs.writeFileSync(path.join(dir, "AGENTS.md"), "CONTEXT_MUST_NOT_BE_LOADED");
    try {
      const execution = promisify(execFile)(
        cli,
        [
          "--print",
          "--mode",
          "json",
          "--no-session",
          "--no-approve",
          "--no-context-files",
          "--no-extensions",
          "--model",
          "aw-gateway/fixture",
          "--extension",
          path.join(import.meta.dirname, "pi_tool_policy.cjs"),
          "--extension",
          "builtin:mcp",
          "--extension",
          "builtin:codemode",
          "--extension",
          "builtin:tool-search",
          "Complete the fixture task",
        ],
        { cwd: dir, env: { PATH: process.env.PATH, HOME: dir, PI_OFFLINE: "1", PI_CODING_AGENT_DIR: agentDir, GH_AW_PI_TOOL_POLICY: '{"bash":["echo"]}', GH_AW_MAX_TOOL_CALLS: "10" }, timeout: 20000, maxBuffer: 2 * 1024 * 1024 }
      );
      execution.child.stdin.end();
      const { stdout } = await execution;
      const records = stdout
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      expect(records.some(record => record.type === "agent_settled")).toBe(true);
      expect(records.find(record => record.type === "tool_execution_end" && record.toolCallId.split("|")[0] === "call-2").isError).toBe(true);
      expect(mcpCalls).toEqual(["noop", "resource", "noop"]);
      expect(requests.some(request => request.tools?.some(tool => tool.function?.name === "codemode" || tool.name === "codemode"))).toBe(true);
      if (api === "openai-responses") {
        const tools = requests[0].tools;
        expect(tools.find(tool => tool.name === "codemode")).toMatchObject({ type: "function", parameters: { properties: { code: { type: "string" } } } });
        expect(tools.some(tool => tool.type === "custom")).toBe(false);
      }
      expect(JSON.stringify(requests)).not.toContain("CONTEXT_MUST_NOT_BE_LOADED");
    } finally {
      await new Promise(resolve => server.close(resolve));
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
