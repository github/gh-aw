// @ts-check
"use strict";

// Exercise the real pinned harness with scripted models; no paid API requests.
import { it as test, afterEach, describe, vi } from "vitest";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { main } = require("./pi_durable_driver.cjs");
const { loadDurablePackage, createGatewayModels, installGatewayTools } = require("./pi_durable_runtime.cjs");
const { createCheckpoint, assertNoSecrets } = require("./pi_durable_checkpoint.cjs");
const { createDurableTelemetry } = require("./pi_durable_telemetry.cjs");
const { transformPiV3Entries } = require("./pi_session.cjs");
process.env.GH_AW_PI_DURABLE_PACKAGE_ROOT ||= path.join(__dirname, "node_modules");
const originalEnv = { ...process.env };
const directories = [];

describe("official Pi durable offline integration", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    for (const name of Object.keys(process.env)) if (!(name in originalEnv)) delete process.env[name];
    Object.assign(process.env, originalEnv);
    for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
  });

  function fixture(storage = "artifact") {
    const directory = path.resolve(`.pi-durable-test-${randomUUID()}`);
    directories.push(directory);
    fs.mkdirSync(path.join(directory, "artifact"), { recursive: true });
    fs.mkdirSync(path.join(directory, "agent"), { recursive: true });
    fs.writeFileSync(path.join(directory, "prompt"), "Complete the workflow");
    Object.assign(process.env, {
      GH_AW_PI_DURABLE_DIR: path.join(directory, "artifact"),
      GH_AW_PI_DURABLE_CHECKPOINT: path.join(directory, "artifact/pi-durable-test.json"),
      GH_AW_PI_DURABLE_STORAGE: storage,
      GH_AW_PROMPT: path.join(directory, "prompt"),
      GH_AW_ENGINE_CWD: directory,
      GITHUB_RUN_ID: "100",
      GITHUB_RUN_ATTEMPT: "1",
      GH_AW_PI_TOOL_POLICY: '{"bash":false,"edit":true}',
    });
    return { directory, workDirectory: path.join(directory, "work"), runtime: { agentDir: path.join(directory, "agent"), settings: {}, config: {} }, skipGatewayTools: true, emit() {}, exportSpan: async () => {} };
  }

  async function scripted(responses) {
    const { createModels, createProvider } = await loadDurablePackage("pi-ai", "models.js");
    const { createAssistantMessageEventStream } = await loadDurablePackage("pi-ai", "utils/event-stream.js");
    let count = 0;
    const model = {
      provider: "scripted",
      id: "offline",
      name: "Offline",
      api: "openai-completions",
      baseUrl: "http://localhost",
      reasoning: false,
      input: ["text"],
      contextWindow: 128000,
      maxTokens: 4096,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    };
    const stream = (selected, context, options) => {
      const result = createAssistantMessageEventStream();
      const index = count++;
      Promise.resolve()
        .then(async () => {
          const content = await responses(index, context, options);
          if (options?.signal?.aborted) {
            result.push({ type: "error", reason: "aborted", error: { role: "assistant", content: [], api: model.api, provider: model.provider, model: model.id, usage: usage(), stopReason: "aborted", timestamp: Date.now() } });
            return;
          }
          const message = {
            role: "assistant",
            content,
            api: selected.api,
            provider: selected.provider,
            model: selected.id,
            usage: usage(),
            stopReason: content.some(block => block.type === "toolCall") ? "toolUse" : "stop",
            timestamp: Date.now(),
          };
          result.push({ type: "start", partial: { ...message, content: [] } });
          for (let contentIndex = 0; contentIndex < content.length; contentIndex++) {
            const block = content[contentIndex];
            if (block.type === "text") {
              result.push({ type: "text_start", contentIndex, partial: { ...message, content: [{ type: "text", text: "" }] } });
              result.push({ type: "text_delta", contentIndex, delta: block.text, partial: message });
              result.push({ type: "text_end", contentIndex, content: block.text, partial: message });
            }
          }
          result.push({ type: "done", reason: message.stopReason, message });
        })
        .catch(error =>
          result.push({
            type: "error",
            reason: "error",
            error: { role: "assistant", content: [], api: model.api, provider: model.provider, model: model.id, usage: usage(), stopReason: "error", errorMessage: error.message, timestamp: Date.now() },
          })
        );
      return result;
    };
    const models = createModels();
    models.setProvider(createProvider({ id: "scripted", models: [model], auth: { apiKey: { name: "Offline", resolve: async () => ({ auth: { apiKey: "offline" } }) } }, api: { stream, streamSimple: stream } }));
    return { models, model: { provider: model.provider, modelId: model.id }, requests: () => count };
  }

  function usage() {
    return { input: 2, output: 3, cacheRead: 0, cacheWrite: 0, totalTokens: 5, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
  }

  test("real JSONL harness persists artifact sessions, emits unified events and skips completed reruns", async () => {
    for (const storage of ["artifact"]) {
      const options = fixture(storage);
      const engine = await scripted(async () => [{ type: "text", text: "Offline answer" }]);
      const events = [];
      const spans = [];
      await main({ ...options, ...engine, emit: event => events.push(event), exportSpan: async span => spans.push(span) });
      const saved = JSON.parse(fs.readFileSync(process.env.GH_AW_PI_DURABLE_CHECKPOINT, "utf8"));
      assert.equal(saved.source, "@earendil-works/pi-durable");
      assert.equal(saved.packageVersion, "1.4.0");
      assert.equal(saved.state.pending, false);
      assert.ok(Object.keys(saved.files).includes("main.jsonl"));
      assert.ok(Object.keys(saved.files).some(file => file.startsWith("doc-")));
      assert.equal(saved.state.usage.models["scripted/offline"].totalTokens, 5);
      assert.ok(spans.some(span => span.name === "pi.durable.phase"));
      assert.ok(events.some(event => event.type === "session" && event.version === 3));
      const normalized = transformPiV3Entries(events);
      assert.ok(
        normalized.some(event => event.type === "assistant.message" || event.type === "assistant.message_delta" || event.type === "assistant.message_update"),
        JSON.stringify(normalized)
      );
      process.env.GITHUB_RUN_ATTEMPT = "2";
      await main({ ...options, ...engine });
      assert.equal(engine.requests(), 1);
      process.env.GITHUB_RUN_ID = "101";
      await main({ ...options, ...engine });
      assert.equal(engine.requests(), 2);
      const next = JSON.parse(fs.readFileSync(process.env.GH_AW_PI_DURABLE_CHECKPOINT, "utf8"));
      assert.equal(next.state.usage.models["scripted/offline"].totalTokens, 10);
    }
  });

  test("clean interrupted model request resumes the original pending submission after storage reopen", async () => {
    const options = fixture();
    const interrupted = await scripted(async (_index, _context, request) => {
      await new Promise(resolve => request.signal.addEventListener("abort", resolve, { once: true }));
      return [];
    });
    await main({ ...options, ...interrupted, timeoutMs: 30 });
    const before = JSON.parse(fs.readFileSync(process.env.GH_AW_PI_DURABLE_CHECKPOINT, "utf8"));
    assert.equal(before.state.pending, true);
    process.env.GITHUB_RUN_ID = "200";
    const resumed = await scripted(async () => [{ type: "text", text: "Resumed" }]);
    await main({ ...options, ...resumed });
    const after = JSON.parse(fs.readFileSync(process.env.GH_AW_PI_DURABLE_CHECKPOINT, "utf8"));
    assert.equal(after.state.submissionId, before.state.submissionId);
    assert.equal(after.state.requestId, before.state.requestId);
    assert.equal(after.state.pending, false);
    assert.equal(resumed.requests(), 1);
    process.env.GITHUB_RUN_ATTEMPT = "2";
    await main({ ...options, ...resumed });
    assert.equal(resumed.requests(), 1, "retrying the run that completed a resumed submission must not submit another prompt");
  });

  test("an interrupted mutating tool is not blindly executed again", async () => {
    const options = fixture();
    const durable = await loadDurablePackage("pi-durable");
    let mutations = 0;
    let interrupt;
    const makeRegistry = () => {
      const registry = durable.createRegistry();
      registry.install(
        durable.defineExtension({
          name: "external",
          tools: [
            durable.defineTool({
              name: "mutate",
              description: "External mutation",
              parameters: { type: "object", properties: {}, additionalProperties: false },
              async execute(_args, _api, context) {
                mutations++;
                setTimeout(() => interrupt(), 10);
                await new Promise(resolve => context.abortSignal.addEventListener("abort", resolve, { once: true }));
                return { output: "interrupted" };
              },
            }),
          ],
        })
      );
      return registry;
    };
    const first = await scripted(async () => [{ type: "toolCall", id: "mutation-1", name: "mutate", arguments: {} }]);
    await assert.rejects(
      main({
        ...options,
        ...first,
        registry: makeRegistry(),
        timeoutMs: 2000,
        onHarness: async (_harness, _root, stop) => {
          interrupt = stop;
        },
      }),
      /interrupted/
    );
    assert.equal(mutations, 1);
    const resumed = await scripted(async () => [{ type: "text", text: "Do not repeat the mutation" }]);
    await main({ ...options, ...resumed, registry: makeRegistry() });
    assert.equal(mutations, 1);
  });

  test("workflow tools and aggregate call budget remain enforced", async () => {
    const options = fixture();
    process.env.GH_AW_MAX_TOOL_CALLS = "2";
    process.env.GH_AW_PI_TOOL_POLICY = '{"bash":false,"edit":false}';
    const engine = await scripted(async index =>
      index === 0
        ? [{ type: "toolCall", id: "call-1", name: "write", arguments: { path: "forbidden", content: "no" } }]
        : index === 1
          ? [{ type: "toolCall", id: "call-2", name: "bash", arguments: { command: "touch forbidden" } }]
          : [{ type: "text", text: "Policy respected" }]
    );
    await main({ ...options, ...engine });
    assert.equal(fs.existsSync(path.join(options.directory, "forbidden")), false);
    const state = JSON.parse(fs.readFileSync(process.env.GH_AW_PI_DURABLE_CHECKPOINT, "utf8")).state;
    assert.equal(state.calls, 2);
    assert.equal(state.denials, 2);
  });

  test("retrying a policy-aborted run preserves failure instead of reporting success", async () => {
    const options = fixture();
    process.env.GH_AW_MAX_TOOL_CALLS = "1";
    process.env.GH_AW_PI_TOOL_POLICY = '{"bash":false}';
    const engine = await scripted(async index => [{ type: "toolCall", id: `denied-${index}`, name: "bash", arguments: { command: "touch forbidden" } }]);
    await assert.rejects(main({ ...options, ...engine }), /tool budget exhausted/);
    const requests = engine.requests();
    process.env.GITHUB_RUN_ATTEMPT = "2";
    await assert.rejects(main({ ...options, ...engine }), /tool budget exhausted/);
    assert.equal(engine.requests(), requests);
  });

  test("rejects secret-bearing persistence, incompatible checkpoints, traversal, missing sidecars and unsupported storage", async () => {
    const options = fixture();
    process.env.GATEWAY_SECRET = "super-secret-gateway-value";
    assert.throws(() => assertNoSecrets({ output: process.env.GATEWAY_SECRET }), /credential/);
    const engine = await scripted(async () => [{ type: "text", text: "Safe output" }]);
    await main({ ...options, ...engine });
    const file = process.env.GH_AW_PI_DURABLE_CHECKPOINT;
    const saved = JSON.parse(fs.readFileSync(file, "utf8"));
    assert.ok(!fs.readFileSync(file, "utf8").includes(process.env.GATEWAY_SECRET));
    fs.writeFileSync(file, JSON.stringify({ ...saved, packageVersion: "0.0.0" }));
    assert.throws(() => createCheckpoint(file, options.workDirectory, "pi-durable-test", "artifact"), /Incompatible/);
    fs.writeFileSync(file, JSON.stringify({ ...saved, files: { ...saved.files, "../evil.jsonl": "{}\n" }, hashes: { ...saved.hashes, "../evil.jsonl": "invalid" } }));
    assert.throws(() => createCheckpoint(file, options.workDirectory, "pi-durable-test", "artifact"), /Invalid/);
    const sidecar = Object.keys(saved.files).find(name => name.startsWith("doc-"));
    delete saved.files[sidecar];
    delete saved.hashes[sidecar];
    fs.writeFileSync(file, JSON.stringify(saved));
    await assert.rejects(main({ ...options, ...engine }), /sidecar|Missing|missing|JSONL/i);
    process.env.GH_AW_PI_DURABLE_STORAGE = "sqlite";
    await assert.rejects(main({ ...options, ...engine }), /artifact storage/);
  });

  test("official telemetry adapter propagates saved trace context and flushes phase spans", async () => {
    process.env.GITHUB_AW_OTEL_TRACE_ID = "a".repeat(32);
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = "b".repeat(16);
    const spans = [];
    const adapter = createDurableTelemetry({ exportSpan: async span => spans.push(span) });
    let carrier;
    await adapter.telemetry.startSpan({ name: "admit" }, async span => {
      carrier = span.propagation.inject(span);
    });
    await adapter.telemetry.propagation.extract(carrier).startSpan({ name: "resume" }, async span => {
      span.addEvent("checkpoint", { phase: "tool" });
    });
    await adapter.flush();
    assert.equal(spans[0].traceId, "a".repeat(32));
    assert.equal(spans[0].parentId, "b".repeat(16));
    assert.equal(spans[1].parentId, spans[0].id);
    assert.equal(spans[1].events[0].name, "checkpoint");
  });

  test("gateway models are rebuilt from fresh credentials without persisting endpoint metadata", async () => {
    const options = fixture();
    process.env.GH_AW_PI_MODEL = "openai/offline";
    process.env.FRESH_GATEWAY_KEY = "fresh-gateway-secret";
    fs.writeFileSync(
      path.join(options.runtime.agentDir, "models.json"),
      JSON.stringify({ providers: { "aw-gateway": { baseUrl: "http://localhost:1234/v1", api: "openai-completions", apiKey: "FRESH_GATEWAY_KEY", models: [{ id: "offline", headers: { Authorization: "should-not-be-copied" } }] } } })
    );
    const gateway = await createGatewayModels(options.runtime.agentDir);
    assert.deepEqual(gateway.model, { provider: "aw-gateway", modelId: "offline" });
    assert.equal((await gateway.models.getAuth("aw-gateway")).auth.apiKey, "fresh-gateway-secret");
    assert.equal(gateway.models.getModel("aw-gateway", "offline").headers, undefined);
  });

  test("the real harness calls authenticated MCP tools and closes their fresh gateway session", async () => {
    const options = fixture();
    fs.writeFileSync(path.join(options.runtime.agentDir, "mcp.json"), JSON.stringify({ mcpServers: { github: { type: "http", url: "http://localhost:1234/mcp", headers: { Authorization: "Bearer gateway-session" } } } }));
    const requests = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_url, init) => {
      if (init.method === "DELETE") {
        requests.push({ method: "DELETE", headers: init.headers });
        return new Response(null, { status: 204 });
      }
      const request = JSON.parse(init.body);
      requests.push({ ...request, headers: init.headers });
      if (request.method === "notifications/initialized") return new Response(null, { status: 202 });
      const result =
        request.method === "initialize"
          ? { protocolVersion: "2025-03-26", capabilities: { tools: {} } }
          : request.method === "tools/list"
            ? { tools: [{ name: "get_issue", description: "Read issue", inputSchema: { type: "object", properties: { number: { type: "number" } }, required: ["number"] } }] }
            : { content: [{ type: "text", text: "Issue retrieved" }] };
      return new Response(JSON.stringify({ jsonrpc: "2.0", id: request.id, result }), { headers: { "content-type": "application/json", "mcp-session-id": "fresh-session" } });
    });
    const engine = await scripted(index => (index === 0 ? [{ type: "toolCall", id: "read-issue", name: "mcp__github__get_issue", arguments: { number: 42 } }] : [{ type: "text", text: "Done" }]));
    await main({ ...options, ...engine, skipGatewayTools: false });
    const call = requests.find(request => request.method === "tools/call");
    assert.deepEqual(call.params, { name: "get_issue", arguments: { number: 42 } });
    assert.equal(call.headers.Authorization, "Bearer gateway-session");
    assert.equal(call.headers["Mcp-Session-Id"], "fresh-session");
    assert.equal(requests.at(-1).method, "DELETE");
    assert.ok(!fs.readFileSync(process.env.GH_AW_PI_DURABLE_CHECKPOINT, "utf8").includes("gateway-session"));
  });
});
