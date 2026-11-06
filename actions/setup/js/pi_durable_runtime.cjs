// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { isPiBashAllowed, reservePiBudget } = require("./pi_tool_policy.cjs");
const { parseMaxToolCalls } = require("./copilot_sdk_tool_budget.cjs");
const { sanitizeProviderErrorMessage } = require("./pi_provider_error.cjs");
const { VERSION } = require("./pi_durable_checkpoint.cjs");

async function loadDurablePackage(name, file = "index.js") {
  const root = process.env.GH_AW_PI_DURABLE_PACKAGE_ROOT;
  if (!root || !path.isAbsolute(root)) throw new Error("GH_AW_PI_DURABLE_PACKAGE_ROOT must name the installed node_modules directory");
  const directory = path.join(root, "@earendil-works", name);
  const manifest = JSON.parse(fs.readFileSync(path.join(directory, "package.json"), "utf8"));
  if (manifest.version !== VERSION || manifest.name !== `@earendil-works/${name}`) throw new Error(`Pi durable requires @earendil-works/${name}@${VERSION}`);
  return import(pathToFileURL(path.join(directory, "dist", file)).href);
}

/** Rebuild the provider from this run's gateway config, never from memory. */
async function createGatewayModels(agentDir, selection) {
  const { createModels, createProvider } = await loadDurablePackage("pi-ai", "models.js");
  const gateway = JSON.parse(fs.readFileSync(path.join(agentDir, "models.json"), "utf8")).providers?.["aw-gateway"];
  if (!gateway?.baseUrl || !Array.isArray(gateway.models)) throw new Error("Pi durable requires the compiler-managed model gateway");
  const configured = selection?.wire_model || (process.env.GH_AW_PI_MODEL || process.env.PI_MODEL || "").split("/").slice(1).join("/");
  const entry = gateway.models.find(item => item.id === configured);
  if (!entry) throw new Error("Pi durable model is not available in the compiler-managed gateway");
  const api = entry.api || gateway.api;
  if (!["openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai"].includes(api)) throw new Error("Unsupported Pi durable model gateway API");
  const stream = await loadDurablePackage("pi-ai", `api/${api}.js`);
  const model = {
    id: entry.id,
    provider: "aw-gateway",
    name: entry.name || entry.id,
    api,
    baseUrl: gateway.baseUrl,
    reasoning: entry.reasoning ?? false,
    input: entry.input || ["text"],
    cost: entry.cost || { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: entry.contextWindow || 128000,
    maxTokens: entry.maxTokens || 16384,
    ...(entry.compat ? { compat: entry.compat } : {}),
  };
  const keyName = gateway.apiKey;
  const apiKey = process.env[keyName] || keyName || "awf-proxy";
  const models = createModels();
  models.setProvider(
    createProvider({
      id: "aw-gateway",
      baseUrl: gateway.baseUrl,
      models: [model],
      headers: { ...gateway.headers, "x-awf-caller": "main" },
      auth: { apiKey: { name: "Workflow gateway", resolve: async () => ({ auth: { apiKey } }) } },
      api: { stream: stream.stream, streamSimple: stream.streamSimple },
    })
  );
  return { models, model: { provider: "aw-gateway", modelId: model.id } };
}

/** No retries of tools/call: an ambiguous transport failure may have mutated externally. */
async function installGatewayTools(durable, registry, agentDir, clients = []) {
  const config = JSON.parse(fs.readFileSync(path.join(agentDir, "mcp.json"), "utf8"));
  const tools = [];
  for (const [serverName, server] of Object.entries(config.mcpServers || {})) {
    if (!server.url || server.command || (server.type && server.type !== "http")) throw new Error("Pi durable supports only compiler-managed HTTP MCP gateway servers");
    const url = new URL(server.url);
    if (url.protocol !== "http:" || !["localhost", "127.0.0.1", "host.docker.internal", "mcp-gateway"].includes(url.hostname)) throw new Error("Pi durable MCP must use the compiler-managed loopback gateway");
    let sessionId;
    let requestId = 0;
    async function rpc(method, params, context, notification = false) {
      const id = ++requestId;
      const response = await fetch(url, {
        method: "POST",
        headers: { ...server.headers, "Content-Type": "application/json", Accept: "application/json, text/event-stream", "MCP-Protocol-Version": "2025-03-26", ...(sessionId ? { "Mcp-Session-Id": sessionId } : {}) },
        body: JSON.stringify({ jsonrpc: "2.0", ...(notification ? {} : { id }), method, params }),
        signal: context?.abortSignal ? AbortSignal.any([context.abortSignal, AbortSignal.timeout(60000)]) : AbortSignal.timeout(60000),
      });
      if (!response.ok) throw new Error(`Pi durable MCP gateway returned HTTP ${response.status}`);
      sessionId = response.headers.get("mcp-session-id") || sessionId;
      if (notification || response.status === 202) {
        await response.body?.cancel();
        return {};
      }
      if (!response.body) throw new Error("Pi durable MCP gateway response is empty");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const sse = response.headers.get("content-type")?.includes("text/event-stream");
      let buffer = "";
      let received = 0;
      let result;
      try {
        while (!result) {
          const { value, done } = await reader.read();
          if (done) {
            if (!sse) result = JSON.parse(buffer);
            break;
          }
          received += value.byteLength;
          if (received > 16 * 1024 * 1024) throw new Error("Pi durable MCP response exceeds 16 MiB");
          buffer += decoder.decode(value, { stream: true });
          if (!sse) continue;
          const frames = buffer.split(/\r?\n\r?\n/);
          buffer = frames.pop() || "";
          for (const frame of frames) {
            const data = frame
              .split(/\r?\n/)
              .filter(line => line.startsWith("data:"))
              .map(line => line.slice(5).trimStart())
              .join("\n");
            if (!data) continue;
            const record = JSON.parse(data);
            if (record.id === id) {
              result = record;
              break;
            }
          }
        }
      } finally {
        await reader.cancel();
      }
      if (result?.id !== id) throw new Error("Pi durable MCP gateway response has an unexpected request ID");
      if (!result || result.error) throw new Error("Pi durable MCP gateway request failed");
      return result.result;
    }
    await rpc("initialize", { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "gh-aw-pi-durable", version: VERSION } });
    await rpc("notifications/initialized", {}, undefined, true);
    clients.push({
      close: async () => {
        if (sessionId)
          await fetch(url, { method: "DELETE", headers: { ...server.headers, "Mcp-Session-Id": sessionId }, signal: AbortSignal.timeout(2000) })
            .then(async response => {
              await response.body?.cancel();
              if (!response.ok && response.status !== 404 && response.status !== 405) throw new Error("MCP session cleanup failed");
            })
            .catch(() => process.stderr.write("[pi-durable] MCP gateway session cleanup failed\n"));
      },
    });
    let cursor;
    do {
      const listed = await rpc("tools/list", cursor ? { cursor } : {});
      if (!Array.isArray(listed.tools)) throw new Error("Invalid Pi durable MCP tools/list response");
      for (const tool of listed.tools) {
        const name = `mcp__${serverName}__${tool.name}`;
        if (!/^[A-Za-z0-9_-]+$/.test(name) || tools.some(item => item.name === name)) throw new Error("Invalid or duplicate Pi durable MCP tool name");
        tools.push(
          durable.defineTool({
            name,
            description: tool.description || tool.name,
            parameters: tool.inputSchema,
            // All remote tools are conservatively non-replayable, even annotated reads.
            async execute(args, _api, context) {
              const result = await rpc("tools/call", { name: tool.name, arguments: args }, context);
              return { output: result.content || [], ...(result.isError ? { diagnostics: [{ code: "tool_error", message: "MCP tool reported an error" }] } : {}) };
            },
          })
        );
      }
      if (listed.nextCursor === cursor && cursor !== undefined) throw new Error("Pi durable MCP pagination did not advance");
      cursor = listed.nextCursor;
    } while (cursor);
  }
  if (tools.length) registry.install(durable.defineExtension({ name: "gh-aw-mcp-gateway", tools }));
}

function installWorkflowPolicy(durable, registry, checkpoint, stop) {
  const policy = JSON.parse(process.env.GH_AW_PI_TOOL_POLICY || "{}");
  const calls = process.env.GH_AW_MAX_TOOL_CALLS ? parseMaxToolCalls(process.env.GH_AW_MAX_TOOL_CALLS) : undefined;
  const denials = process.env.GH_AW_MAX_TOOL_DENIALS ? parseMaxToolCalls(process.env.GH_AW_MAX_TOOL_DENIALS) : undefined;
  registry.install(
    durable.defineExtension({
      name: "gh-aw-workflow-policy",
      hooks: [
        durable.hook(durable.ToolTask, {
          beforeTool(call) {
            checkpoint.state.calls = reservePiBudget("calls", checkpoint.state.calls, calls);
            let block;
            if (calls !== undefined && checkpoint.state.calls > calls) block = "Workflow tool-call budget exhausted";
            else if (call.name === "bash" && !isPiBashAllowed(call.arguments?.command, policy.bash)) block = "Command is outside the workflow tools.bash allowlist";
            else if (policy.edit === false && ["edit", "write"].includes(call.name)) block = "File editing is disabled by the workflow";
            if (block) {
              checkpoint.state.denials = reservePiBudget("denials", checkpoint.state.denials, denials);
              if ((denials !== undefined && checkpoint.state.denials >= denials) || (calls !== undefined && checkpoint.state.calls > calls)) stop("policy");
            }
            checkpoint.save();
            return block ? { block } : undefined;
          },
        }),
      ],
    })
  );
}

/** Translate official watchEvents batches to the existing Pi streaming contract. */
function createEventEmitter(emit) {
  function message(entry) {
    return entry?.model?.[0];
  }
  return event => {
    if (event.type === "snapshot") return;
    if (event.type === "run_start") return emit({ type: "agent_start" });
    if (event.type === "run_end") return emit({ type: "agent_end" });
    if (event.type === "message_end") {
      const value = message(event.entry);
      if (value) emit({ type: "message_end", message: value });
      return;
    }
    if (event.type === "message_update") {
      for (const change of event.changes) {
        if (change.type === "message") continue;
        const update = { ...change };
        if (change.type === "toolcall_start") {
          update.id = change.block.id;
          update.toolName = change.block.name;
        } else if (change.type === "block") {
          // Final blocks are snapshots; message_end publishes their complete content.
          continue;
        } else if (change.type === "toolcall_delta") {
          // Durable streams JSON-path deltas; the Pi parser expects JSON text.
          continue;
        }
        delete update.block;
        emit({ type: "message_update", usage: event.usage, assistantMessageEvent: update });
      }
      return;
    }
    if (event.type.startsWith("tool_execution_")) {
      const result = event.result || message(event.entry);
      return emit({
        type: event.type,
        toolCallId: event.toolCallId,
        toolName: event.toolName,
        ...(event.parentToolCallId ? { parentToolCallId: event.parentToolCallId } : {}),
        ...(event.type === "tool_execution_start" ? { args: event.args } : { result, partialResult: event.output, isError: result?.isError }),
      });
    }
    if (event.type === "task_failed") return emit({ type: "error", errorMessage: sanitizeProviderErrorMessage(event.message) });
    if (["message_start", "turn_start", "turn_end", "auto_retry_start", "auto_retry_end", "compaction_start", "compaction_end"].includes(event.type)) emit(event);
  };
}

module.exports = { loadDurablePackage, createGatewayModels, installGatewayTools, installWorkflowPolicy, createEventEmitter };
