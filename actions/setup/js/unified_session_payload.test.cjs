import { afterEach, describe, expect, it, vi } from "vitest";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { normalizeUnifiedSessionEvent, normalizeModelInventoryData } from "./unified_session_payload.cjs";
import { collectUnifiedSession, mergeSessionSources, writeUnifiedSession } from "./unified_session.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary, convertCopilotEventsToLegacyLogEntries } from "./log_parser_shared.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";
import { normalizeCodexSession } from "./codex_session.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { normalizeCopilotSession } from "./copilot_session.cjs";
import { normalizeGeminiSession } from "./gemini_session.cjs";
import { collapseStreamedMessages } from "./agent_session_render.cjs";
import { reconcileSessionUsage, selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { createSessionValidator, validateSession } from "./scripts/validate_session.cjs";
import { dynamicWorkflow } from "./fixtures/claude_dynamic_workflow.cjs";
import { DYNAMIC_WORKFLOW_EVENT_TYPES } from "./dynamic_workflow_session.cjs";

describe("essential unified session payloads", () => {
  it("projects job outcomes without copying dependency outputs", () => {
    const event = { type: "workflow.job_result", data: { jobId: "agent", result: "failure", outputs: { secret: "PRIVATE_OUTPUT" }, name: "PRIVATE_NAME" } };
    const projected = normalizeUnifiedSessionEvent(event);
    expect(projected.data).toEqual({ jobId: "agent", result: "failure" });
    expect(normalizeUnifiedSessionEvent(projected)).toEqual(projected);
    expect(event.data.outputs.secret).toBe("PRIVATE_OUTPUT");
  });

  it.each([false, true])("preserves Copilot file edits through the published projection when tool success is %s", success => {
    const native = [
      { type: "tool.execution_start", id: "edit-start", data: { toolCallId: "edit", toolName: "edit", arguments: { path: "/workspace/partial.txt" } } },
      {
        type: "tool.execution_complete",
        id: "edit-end",
        data: {
          toolCallId: "edit",
          success,
          fileEdits: [{ kind: "edit", path: "/workspace/partial.txt" }],
          ...(success ? {} : { error: { message: "Second edit failed" } }),
        },
      },
    ];
    const original = structuredClone(native);
    const events = normalizeCopilotSession(native);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "sandbox/agent/logs/copilot-session-state/release-events/events.jsonl", events }]);
    const published = serializeSessionArtifact(unified).trim().split("\n").map(JSON.parse);
    const completion = published.find(event => event.id === "edit-end");
    expect(completion.data).toMatchObject({
      toolName: "edit",
      success,
      fileEdits: [{ kind: "edit", path: "/workspace/partial.txt" }],
      ...(success ? {} : { error: { message: "Second edit failed" } }),
    });
    const validate = createSessionValidator("unified").event;
    for (const event of published) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(published.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(native).toEqual(original);
  });

  it("preserves top-level AWF routing metadata with endpoint-local precedence and no credentials", () => {
    const source = {
      models_fetch_complete: false,
      routing_models: [{ model_id: "fallback", supported_endpoints: ["/responses", "ws:/responses"], apiKey: "PRIVATE_ROUTING_KEY" }],
      endpoints: [
        { provider: "copilot", models: [{ id: "fallback", token: "PRIVATE_TOKEN" }], base_url: "PRIVATE_URL" },
        { provider: "openai", models: ["local"], routing_models: [{ model_id: "local", supported_endpoints: ["/chat/completions"], headers: "PRIVATE_HEADERS" }] },
        { provider: "anthropic", models: [], routing_models: [] },
      ],
      credential: "PRIVATE_CREDENTIAL",
    };
    const original = structuredClone(source);
    const event = normalizeUnifiedSessionEvent({ type: "model.inventory", data: source });
    expect(event.data).toEqual({
      modelsFetchComplete: false,
      endpoints: [
        { provider: "copilot", models: ["fallback"], routingModels: [{ modelId: "fallback", supportedEndpoints: ["/responses", "ws:/responses"] }] },
        { provider: "openai", models: ["local"], routingModels: [{ modelId: "local", supportedEndpoints: ["/chat/completions"] }] },
        { provider: "anthropic", models: [], routingModels: [] },
      ],
    });
    expect(normalizeModelInventoryData(source)).toEqual(event.data);
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
    const [unified] = mergeSessionSources([{ component: "firewall", phase: "agent", path: "awf-reflect.json", events: [event] }]);
    const validate = createSessionValidator("unified").event;
    expect(validate(unified), JSON.stringify(validate.errors)).toBe(true);
    expect(JSON.stringify(unified)).not.toContain("PRIVATE_");
    expect(source).toEqual(original);
  });

  it.each(["models", "available_models", "detected_models", "model_ids", "availableModels"])("normalizes valid model collections using %s", field => {
    const models = ["a", { id: "b", apiKey: "PRIVATE_KEY" }, { name: "c" }, { model: "d" }];
    const expected = { endpoints: [{ provider: "copilot", models: ["a", "b", "c", "d"] }] };
    expect(normalizeModelInventoryData({ endpoints: [{ provider: "copilot", [field]: models }] })).toEqual(expected);
    expect(normalizeModelInventoryData({ providers: { copilot: { [field]: models } } })).toEqual(expected);
    expect(normalizeModelInventoryData({ provider: "copilot", [field]: models })).toEqual(expected);
  });

  it.each([
    [{ endpoints: [] }, { endpoints: [] }],
    [{ providers: {} }, { endpoints: [] }],
    [{ provider: "copilot", models: [] }, { endpoints: [{ provider: "copilot", models: [] }] }],
    [{ endpoints: [{ provider: "anthropic", configured: false, models: null }] }, { endpoints: [{ provider: "anthropic", configured: false }] }],
    [{ endpoints: [{ provider: "opencode", configured: true, models: null, models_url: null }] }, { endpoints: [{ provider: "opencode", configured: true }] }],
    [{ endpoints: [{ provider: "copilot", configured: true, models: ["a"], routing_models: null }] }, { endpoints: [{ provider: "copilot", configured: true, models: ["a"] }] }],
    [{ routing_models: [{ model_id: "fallback" }], endpoints: [{ provider: "copilot", configured: false, models: null, routing_models: null }] }, { endpoints: [{ provider: "copilot", configured: false }] }],
    [{ routing_models: null, endpoints: [{ provider: "copilot", models: [] }] }, { endpoints: [{ provider: "copilot", models: [] }] }],
    [
      { endpoints: [{ provider: "copilot", models: ["a"], routing_models: [{ model_id: "a", supported_reasoning_efforts: [], context_window_tokens: null }] }] },
      { endpoints: [{ provider: "copilot", models: ["a"], routingModels: [{ modelId: "a", supportedReasoningEfforts: [] }] }] },
    ],
  ])("accepts genuine empty inventories and keeps AWF null collections unknown: %j", (source, expected) => {
    expect(normalizeModelInventoryData(source)).toEqual(expected);
    const event = normalizeUnifiedSessionEvent({ type: "model.inventory", data: source });
    expect(event.data).toEqual(expected);
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
    expect(normalizeModelInventoryData(JSON.parse(JSON.stringify(event.data)))).toEqual(expected);
  });

  it.each([
    null,
    [],
    {},
    { endpoints: null },
    { endpoints: [null] },
    { endpoints: ["copilot"] },
    { endpoints: [{ provider: "copilot" }] },
    { endpoints: [{ provider: 1, models: [] }] },
    { endpoints: [{ provider: " ", models: [] }] },
    { endpoints: [{ provider: "copilot", models: "malformed" }] },
    { endpoints: [{ provider: "copilot", models: null }] },
    { endpoints: [{ provider: "copilot", configured: "false", models: null }] },
    { endpoints: [{ provider: "copilot", models: ["valid"] }, null] },
    { providers: [] },
    { providers: { copilot: null } },
    { providers: { copilot: ["model"] } },
    { providers: { copilot: {} } },
    { provider: "copilot", models: ["valid", null] },
    { provider: "copilot", models: ["valid", false] },
    { provider: "copilot", models: ["valid", 42] },
    { provider: "copilot", models: [{ id: { token: "PRIVATE_KEY" } }] },
    { provider: "copilot", models: [{ id: 42, name: "valid" }] },
    { provider: "copilot", models: [""] },
    { endpoints: [], routing_models: "malformed" },
    { endpoints: [], routing_models: [null] },
    { endpoints: [], routing_models: [{ model_id: 42 }] },
    { endpoints: [], routing_models: [{ model_id: "model", supported_endpoints: "malformed" }] },
    { endpoints: [], routing_models: [{ model_id: "model", supported_endpoints: ["/responses", 42] }] },
    { provider: "copilot", models: [], routingModels: [null] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", supportedEndpoints: null }] },
    { endpoints: [{ provider: "copilot", configured: "true", models: ["model"] }] },
    { endpoints: [{ provider: "copilot", configured: true, models: null, routing_models: "malformed" }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", supportedReasoningEfforts: "high" }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", supportedReasoningEfforts: ["high", 42] }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", supportedReasoningEfforts: ["very high"] }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", supportedReasoningEfforts: [""] }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", contextWindowTokens: 0 }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", contextWindowTokens: -1 }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", contextWindowTokens: 1.5 }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", contextWindowTokens: "128000" }] },
    { provider: "copilot", models: [], routingModels: [{ modelId: "model", contextWindowTokens: Number.MAX_SAFE_INTEGER + 1 }] },
    { endpoints: [], models_fetch_complete: "true" },
  ])("rejects malformed inventory rather than producing a success-shaped empty payload: %j", source => {
    expect(normalizeModelInventoryData(source)).toBeUndefined();
    expect(() => normalizeUnifiedSessionEvent({ type: "model.inventory", data: source })).toThrow("ERR_VALIDATION: Model inventory snapshot must contain valid endpoint model collections");
  });

  it("publishes contextWindowTokens as a positive safe integer in the unified schema", () => {
    const source = { type: "model.inventory", data: { endpoints: [{ provider: "copilot", models: ["a"], routing_models: [{ model_id: "a", context_window_tokens: 1 }] }] } };
    const [event] = mergeSessionSources([{ component: "agent", phase: "agent", path: "awf-reflect.json", events: [source] }]);
    const validate = createSessionValidator("unified").event;
    expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    const withWindow = contextWindowTokens => ({ ...event, data: { endpoints: [{ ...event.data.endpoints[0], routingModels: [{ modelId: "a", contextWindowTokens }] }] } });
    expect(validate(withWindow(Number.MAX_SAFE_INTEGER)), JSON.stringify(validate.errors)).toBe(true);
    for (const contextWindowTokens of [0, 1.5, Number.MAX_SAFE_INTEGER + 1]) expect(validate(withWindow(contextWindowTokens)), String(contextWindowTokens)).toBe(false);
  });

  it("projects canonical MCP configuration through the shared metadata allowlist", () => {
    const source = {
      type: "mcp.configuration",
      id: "configuration",
      parentId: "session",
      ts: 123,
      agentId: "custom",
      ephemeral: false,
      url: "https://private.example",
      credential: "secret",
      data: {
        servers: [{ serverName: "github", transport: "http", required: true, tools: ["get_issue"], url: "https://private.example", credential: "secret" }],
        credential: "secret",
      },
    };
    const original = structuredClone(source);
    const event = normalizeUnifiedSessionEvent(source);
    expect(event).toEqual({
      type: "mcp.configuration",
      id: "configuration",
      parentId: "session",
      timestamp: 123,
      agentId: "custom",
      ephemeral: false,
      data: { servers: [{ serverName: "github", transport: "http", required: true, tools: ["get_issue"] }] },
    });
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
    const [unified] = mergeSessionSources([{ component: "agent", phase: "agent", path: "custom.jsonl", events: [source] }]);
    const validate = createSessionValidator("unified").event;
    expect(validate(unified), JSON.stringify(validate.errors)).toBe(true);
    expect(source).toEqual(original);
  });

  it("retains background task correlation and observed errors on tool updates", () => {
    const source = {
      type: "tool.execution_update",
      data: { toolCallId: "shell", task_id: "background", task_type: "local_bash", status: "failed", output: "observed progress\n", error: "observed failure", nativeEnvelope: { omitted: true } },
    };
    const original = structuredClone(source);
    const [event] = mergeSessionSources([{ component: "agent", phase: "agent", path: "tasks.jsonl", events: [source] }]);
    expect(event.data).toEqual({ toolCallId: "shell", taskId: "background", taskType: "local_bash", status: "failed", output: "observed progress\n", error: "observed failure" });
    expect(event.data).not.toHaveProperty("success");
    expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
    const validate = createSessionValidator("unified").event;
    expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(source).toEqual(original);
  });

  it("preserves grader attribution and unavailable diagnostics without executable payloads", () => {
    const result = {
      id: "coverage",
      source: "builtin",
      status: "unavailable",
      value: null,
      passed: null,
      message: "No trajectory evidence available",
      severity: "warning",
      implementation: { id: "gh-aw/graders", version: 1, digest: "", script: "PRIVATE_SCRIPT" },
      metrics: [{ id: "observed", value: 0, trace: "PRIVATE_TRACE" }],
      diagnostics: { private: "PRIVATE_DIAGNOSTICS" },
      script: "PRIVATE_SCRIPT",
    };
    const manifest = { version: 1, graders: [{ id: "coverage", source: "builtin", enabled: false, min: 0, max: 1, digest: "", script: "PRIVATE_SCRIPT" }] };
    const run = { id: "123", attempt: 2, graderCount: 1, passed: 0, failed: 0, errors: 0 };
    const records = [
      { type: "grader.manifest", data: manifest },
      { type: "grader.result", data: { version: 1, run: { ...run, script: "PRIVATE_SCRIPT" }, results: [result] } },
      { type: "grader.result", data: { ...result, implementation: null, metrics: [] } },
      { type: "grader.result", data: { run: null, results: [] } },
    ];
    const original = structuredClone(records);
    const unified = mergeSessionSources([{ component: "grader", phase: "agent", path: "agent/graders/grader_results.json", events: records }]);
    expect(unified[0].data).toEqual({ version: 1, graders: [{ id: "coverage", source: "builtin", enabled: false, min: 0, max: 1, digest: "" }] });
    const expected = {
      id: "coverage",
      source: "builtin",
      status: "unavailable",
      value: null,
      passed: null,
      message: result.message,
      severity: "warning",
      implementation: { id: "gh-aw/graders", version: 1, digest: "" },
      metrics: [{ id: "observed", value: 0 }],
    };
    expect(unified[1].data).toEqual({ version: 1, run, results: [expected] });
    expect(unified[2].data).toEqual({ ...expected, implementation: null, metrics: [] });
    expect(unified[3].data).toEqual({ run: null, results: [] });
    const validate = createSessionValidator("unified").event;
    for (const event of unified) {
      expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
      expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    }
    expect(JSON.stringify(unified)).not.toContain("PRIVATE_");
    expect(records).toEqual(original);
  });

  it("preserves eval run and experiment attribution without evaluation questions", () => {
    const assignments = { variant: { value: "baseline" } };
    const source = { type: "eval.result", timestamp: "2026-10-11T00:00:00Z", data: { id: "quality", answer: "UNKNOWN", model: "fixture", runid: "123", experiments: assignments, question: "PRIVATE_QUESTION" } };
    const original = structuredClone(source);
    const event = normalizeUnifiedSessionEvent(source);
    expect(event).toEqual({ type: source.type, timestamp: source.timestamp, data: { id: "quality", answer: "UNKNOWN", model: "fixture", runId: "123", assignments } });
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
    const [unified] = mergeSessionSources([{ component: "eval", phase: "evals", path: "evals/evals.jsonl", events: [source] }]);
    expect(createSessionValidator("unified").event(unified)).toBe(true);
    expect(source).toEqual(original);
  });

  it.each([
    [{ runId: "123" }, "123"],
    [{ runid: "123" }, "123"],
    [{ run_id: "123" }, "123"],
    [{ runId: "123", runid: "122", run_id: "121" }, "123"],
    [{ runid: "123", run_id: "122" }, "123"],
    [{ runId: undefined, runid: "123" }, "123"],
    [{ runId: null, runid: "123" }, null],
    [{ runid: null, run_id: "123" }, null],
    [{ runId: 0, runid: "123" }, 0],
    [{ runId: "", runid: "123" }, ""],
  ])("preserves explicit eval run attribution from %j", (attribution, runId) => {
    const event = normalizeUnifiedSessionEvent({ type: "eval.result", data: attribution });
    expect(event.data).toEqual({ runId });
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
  });

  it.each([true, false])("retains final model result and BYOK=%s through persisted unified projection", isByok => {
    const source = {
      type: "model.call_final_result",
      ephemeral: true,
      agentId: "researcher",
      parentToolCallId: "delegate-1",
      data: {
        model: "anthropic/claude-sonnet-4.6",
        isByok,
        result: { status: "success", usage: { inputTokens: 0, outputTokens: 42 }, stopReason: "end_turn" },
        extensionEvidence: { retained: true },
      },
    };
    const original = structuredClone(source);
    const projected = normalizeUnifiedSessionEvent(source);
    expect(projected.data).toEqual({ ...source.data, agentId: "researcher", parentToolCallId: "delegate-1" });
    const [unified] = mergeSessionSources([{ component: "agent", phase: "agent", path: "sdk.jsonl", events: normalizeCopilotSession([source]) }]);
    const persisted = JSON.parse(JSON.stringify(unified));
    expect(persisted.data).toMatchObject(source.data);
    expect(persisted.data.parentToolCallId).toBe("delegate-1");
    expect(normalizeUnifiedSessionEvent(persisted).data).toEqual(persisted.data);
    const validate = createSessionValidator("unified").event;
    expect(validate(persisted), JSON.stringify(validate.errors)).toBe(true);
    expect(source).toEqual(original);
  });

  it.each([401, 503, 0, null])("retains observed session-error status codes through projection and schema validation (%s)", statusCode => {
    const source = { type: "session.error", data: { errorType: "provider", message: "Observed failure.", statusCode, opaque: "PRIVATE_ERROR_CONTEXT" } };
    const original = structuredClone(source);
    const [event] = mergeSessionSources([{ component: "agent", phase: "agent", path: "errors.jsonl", events: [source] }]);
    expect(event.data).toEqual({ errorType: "provider", message: "Observed failure.", statusCode });
    expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
    const validate = createSessionValidator("unified").event;
    expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(normalizeUnifiedSessionEvent({ type: "session.error", data: { message: "No status observed." } }).data).not.toHaveProperty("statusCode");
    expect(source).toEqual(original);
  });

  it("retains declared partial-input and per-step usage evidence", () => {
    const events = [
      { type: "tool.execution_start", data: { sessionId: "session", stepIndex: 0, toolName: "bash", input: { command: "preview..." }, inputTruncated: true } },
      { type: "usage.report", data: { sessionId: "session", stepIndex: 0, usage: { input_tokens: 0, cache_read_input_tokens: 1 } } },
      { type: "tool.execution_complete", data: { sessionId: "session", stepIndex: 0, status: "declined", exitCode: null } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "steps.jsonl", events }]);
    expect(unified[0].data).toEqual(events[0].data);
    expect(unified[1].data).toEqual({ sessionId: "session", stepIndex: 0, usage: { inputTokens: 0, cacheReadInputTokens: 1 } });
    expect(unified[2].data).toEqual(events[2].data);
    for (const [kind, trace] of [
      ["agent", events],
      ["unified", unified],
    ]) {
      const validate = createSessionValidator(kind).event;
      for (const event of trace) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    }
  });

  it("preserves native step correlation without fabricating stored tool identifiers", () => {
    const events = [
      { type: "tool.execution_start", data: { sessionId: "session", stepIndex: 0, toolName: "lookup", input: "first" } },
      { type: "tool.execution_start", data: { sessionId: "session", stepIndex: 1, toolName: "lookup", input: "second" } },
      { type: "tool.execution_complete", data: { sessionId: "session", stepIndex: 1, toolName: "lookup", output: "second result", success: true } },
      { type: "tool.execution_complete", data: { sessionId: "session", stepIndex: 0, toolName: "lookup", output: "first result", success: false } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "steps.jsonl", events }]);
    expect(unified.map(event => event.data.stepIndex)).toEqual([0, 1, 1, 0]);
    expect(unified.every(event => !Object.hasOwn(event.data, "toolCallId"))).toBe(true);
    const display = convertCopilotEventsToLegacyLogEntries(unified);
    expect(display[2].message.content[0].tool_use_id).toBe(display[1].message.content[0].id);
    expect(display[3].message.content[0].tool_use_id).toBe(display[0].message.content[0].id);
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("retains diagnostic severity and refusal correlation without duplicate native envelopes", () => {
    const events = [
      { type: "session.error", agentId: "child", parentToolCallId: "parent", data: { severity: "warning", status: "warning", code: 0, error: null, message: "", content: "exact\n", sourceType: "error", opaque: "omit" } },
      {
        type: "assistant.refusal",
        agentId: "child",
        data: { reason: "refusal", content: "exact\n", messageId: "message", model: "model", apiCallId: "request", interactionId: "interaction", turnId: "turn", parentToolCallId: null, opaque: "omit" },
      },
      { type: "mcp.event", data: { event: "extension_start_failure", serverName: "github", status: "error", opaque: "omit" } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "diagnostics.jsonl", events }]);
    expect(unified[0].data).toEqual({ severity: "warning", status: "warning", code: 0, error: null, message: "", content: "exact\n", sourceType: "error", agentId: "child", parentToolCallId: "parent" });
    expect(unified[1].data).toEqual({ reason: "refusal", content: "exact\n", messageId: "message", model: "model", apiCallId: "request", interactionId: "interaction", turnId: "turn", parentToolCallId: null, agentId: "child" });
    expect(unified[2].data).toEqual({ event: "extension_start_failure", serverName: "github", status: "error" });
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it.each([
    [{ data: { parentToolCallId: "parent" } }, { parentToolCallId: "parent" }],
    [{ parentToolCallId: "parent", data: {} }, { parentToolCallId: "parent" }],
    [{ data: { parentToolCallId: null } }, { parentToolCallId: null }],
    [{ data: { parentToolCallId: "parent", parentToolUseId: "observed-parent" } }, { parentToolCallId: "parent", parentToolUseId: "observed-parent" }],
    [{ data: { parent_tool_use_id: "parent" } }, { parentToolUseId: "parent" }],
    [{ data: { parentToolCallId: "parent", parentToolUseId: null } }, { parentToolCallId: "parent", parentToolUseId: null }],
  ])("retains observed parent identities without a derived duplicate: %j", (scope, expected) => {
    const event = { type: "session.error", ...scope, data: { message: "exact\n", statusCode: 401, ...scope.data } };
    const projected = normalizeUnifiedSessionEvent(event);
    expect(projected.data).toEqual({ message: "exact\n", statusCode: 401, ...expected });
    expect(normalizeUnifiedSessionEvent(projected)).toEqual(projected);
    const validate = createSessionValidator("unified").event;
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "diagnostics.jsonl", events: [event] }]);
    expect(validate(unified[0]), JSON.stringify(validate.errors)).toBe(true);
    expect(event.data).toEqual({ message: "exact\n", statusCode: 401, ...scope.data });
  });

  it("compacts Copilot terminal transport observations without repeating accounting or textual answers", () => {
    const events = [
      {
        type: "session.shutdown",
        data: {
          shutdownType: "error",
          errorReason: "interrupted",
          totalPremiumRequests: 0,
          totalNanoAiu: 0,
          totalApiDurationMs: 0,
          sessionStartTime: 0,
          currentModel: "model",
          modelMetrics: { duplicated: true },
          tokenDetails: { duplicated: true },
          codeChanges: { duplicated: true },
        },
      },
      { type: "session.task_complete", data: { success: false, summary: "already mapped answer\n" } },
      { type: "session.task_complete", data: { success: true, summary: null } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "copilot.jsonl", events }]);
    expect(unified[0].data).toEqual({ shutdownType: "error", errorReason: "interrupted", premiumRequests: 0, totalNanoAiu: 0, totalApiDurationMs: 0, sessionStartTime: 0, currentModel: "model" });
    expect(unified[1].data).toEqual({ success: false });
    expect(unified[2].data).toEqual({ success: true, summary: null });
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("preserves completion-only dynamic workflow launch metadata without a tool start", () => {
    const record = dynamicWorkflow.find(record => record.tool_use_result);
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "partial.jsonl", events: normalizeClaudeSession([record]) }]);
    const launch = events.find(event => event.type === "tool.execution_complete");
    expect(launch.data).toMatchObject({
      taskId: "dynamic-task",
      taskType: "local_workflow",
      workflowName: "smoke-claude-dynamic",
      workflowRunId: "dynamic-run",
      status: "async_launched",
      toolCallId: "workflow-tool",
      success: true,
    });
    expect(launch.data).not.toHaveProperty("toolName");
    expect(generatePlainTextSummary(events)).toContain("[launch succeeded; workflow outcome pending]");
    expect(createSessionValidator("unified").event(launch)).toBe(true);
  });

  it("enforces the dynamic workflow privacy projection in the published schema", () => {
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent.jsonl", events: normalizeClaudeSession(dynamicWorkflow) }]);
    const validate = createSessionValidator("unified").event;
    const launch = events.find(event => event.type === "tool.execution_complete");
    expect(validate(launch), JSON.stringify(validate.errors)).toBe(true);
    for (const type of Object.values(DYNAMIC_WORKFLOW_EVENT_TYPES)) {
      const event = events.find(event => event.type === type);
      expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
      for (const field of ["prompt", "description", "summary", "workflow_progress"]) {
        expect(validate({ ...event, data: { ...event.data, [field]: "PRIVATE_NATIVE_FIELD" } })).toBe(false);
      }
      expect(validate({ ...event, data: { ...event.data, tasks: [{ taskId: "task", prompt: "PRIVATE_TASK_SCRIPT" }] } })).toBe(false);
      expect(validate({ ...event, data: { ...event.data, workflowProgress: [{ agentId: "agent", promptPreview: "PRIVATE_AGENT_PROMPT" }] } })).toBe(false);
      expect(validate({ ...event, data: { ...event.data, usage: { totalTokens: 0, summary: "PRIVATE_USAGE" } } })).toBe(false);
    }
  });

  it("projects engine-independent dynamic workflow events without native Claude envelopes", () => {
    const source = [
      {
        type: "tool.execution_complete",
        data: { toolName: "run_dynamic_workflow", toolCallId: "launch", success: true, status: "async_launched", taskId: "task", taskType: "dynamic_workflow", workflowName: "example", workflowRunId: "run" },
      },
      { type: "dynamicWorkflows.task_started", data: { taskId: "task", toolCallId: "launch", taskType: "dynamic_workflow", workflowName: "example", sessionId: "session", prompt: "PRIVATE_SCRIPT" } },
      {
        type: "dynamicWorkflows.task_progress",
        data: {
          taskId: "task",
          toolCallId: "launch",
          usage: { totalTokens: 0, toolUses: 0, durationMs: 0 },
          workflowProgress: [{ type: "workflow_agent", index: 0, phaseIndex: 0, agentId: "agent", state: "done", promptPreview: "PRIVATE_AGENT_PROMPT" }],
        },
      },
      { type: "dynamicWorkflows.task_updated", data: { taskId: "task", status: "completed" } },
      { type: "dynamicWorkflows.task_notification", data: { taskId: "task", toolCallId: "launch", status: "completed", summary: "PRIVATE_SUMMARY" } },
      { type: "dynamicWorkflows.background_tasks_changed", data: { tasks: [{ taskId: "task", taskType: "dynamic_workflow", status: "completed", description: "PRIVATE_DESCRIPTION" }] } },
    ];
    const original = structuredClone(source);
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "other-engine.jsonl", events: source }]);
    expect(events.map(event => event.type)).toEqual(source.map(event => event.type));
    expect(events[0].data).toEqual(source[0].data);
    expect(events[2].data).toEqual({
      taskId: "task",
      toolCallId: "launch",
      usage: { totalTokens: 0, toolUses: 0, durationMs: 0 },
      workflowProgress: [{ type: "workflow_agent", index: 0, phaseIndex: 0, agentId: "agent", state: "done" }],
    });
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    for (const event of events) expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("workflowName=example workflowRunId=run [launch succeeded; workflow outcome pending]");
      expect(output).toContain("dynamicWorkflows.task_notification taskId=task toolCallId=launch status=completed");
      expect(output).not.toContain("PRIVATE_");
      expect(output).not.toContain("claude.");
    }
    expect(source).toEqual(original);
  });

  it("removes duplicated engine envelopes without trimming significant message text", () => {
    const content = "  first\nsecond\t\n" + "x".repeat(10000);
    const message = {
      type: "assistant.message",
      id: "message",
      parentId: null,
      timestamp: "2026-10-02T00:00:01Z",
      model: "duplicated",
      message: { content },
      data: { content, text: content, signature: "opaque", model: "duplicated" },
    };
    const original = structuredClone(message);
    const compact = normalizeUnifiedSessionEvent(message);
    expect(compact).toEqual({ type: "assistant.message", id: "message", parentId: null, timestamp: message.timestamp, data: { content, model: "duplicated" } });
    expect(Buffer.byteLength(JSON.stringify(compact))).toBeLessThan(Buffer.byteLength(JSON.stringify(message)) / 2);
    expect(message).toEqual(original);
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it.each(["assistant.message", "assistant.reasoning"])("retains supplied %s stream boundaries without copying opaque payloads", type => {
    const source = {
      type,
      session_id: "session",
      parent_tool_use_id: "parent-tool",
      agentId: "child",
      message: { id: "message", content: "PRIVATE_NATIVE_SNAPSHOT" },
      data: { content: "exact text", delta: true, partial: false, contentIndex: 0, channel: "channel", opaque: "PRIVATE_EXTENSION" },
    };
    const compact = normalizeUnifiedSessionEvent(source);
    expect(compact.data).toEqual({ content: "exact text", delta: true, partial: false, contentIndex: 0, channel: "channel", sessionId: "session", parentToolUseId: "parent-tool", agentId: "child", messageId: "message" });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    expect(JSON.stringify(compact)).not.toContain("PRIVATE_");
  });

  it.each(["user.message", "assistant.message", "assistant.reasoning", "assistant.refusal", "assistant.message_update"])("preserves supplied %s engine attribution without inferring absent values", type => {
    const data = { content: "Exact answer\n", sourceEngine: "cursor" };
    const event = { type, data };
    const original = structuredClone(event);
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
    expect(normalizeUnifiedSessionEvent(normalizeUnifiedSessionEvent(event))).toEqual(event);
    expect(event).toEqual(original);
    expect(normalizeUnifiedSessionEvent({ type, data: { content: data.content } }).data).not.toHaveProperty("sourceEngine");
  });

  it("keeps Copilot, Claude and Gemini streaming evidence through collection and compaction", () => {
    const claude = event => ({ type: "stream_event", session_id: "session", event });
    const traces = [
      normalizeCopilotSession(["Hello", " ", "world."].map((deltaContent, index) => ({ type: "assistant.message_delta", id: `event-${index}`, data: { messageId: "answer", deltaContent } }))),
      normalizeClaudeSession([
        claude({ type: "message_start", message: { id: "answer" } }),
        claude({ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } }),
        ...["Hello", " ", "world."].map(text => claude({ type: "content_block_delta", index: 0, delta: { type: "text_delta", text } })),
        claude({ type: "message_stop" }),
      ]),
      normalizeGeminiSession(["Hello", " ", "world."].map((content, timestamp) => ({ type: "message", role: "assistant", timestamp, delta: true, content }))),
    ];
    for (const events of traces) {
      const compact = mergeSessionSources([{ component: "agent", phase: "agent", path: "stream.jsonl", events }]);
      const projected = collapseStreamedMessages(compact);
      expect(projected.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["Hello world."]);
      const output = generatePlainTextSummary(compact);
      expect(output.match(/agent\/agent assistant\.message /g)).toHaveLength(1);
      expect(output.match(/◆ Hello world\./g)).toHaveLength(1);
    }
  });

  it.each([
    ["tool.execution_start", { toolCallId: "call", parameters: false, input: 0, command: "", text: "duplicate" }, { toolCallId: "call", input: 0, command: "" }],
    ["tool.execution_start", { toolName: "bash", arguments: { command: "cat restricted-file" } }, { toolName: "bash", input: { command: "cat restricted-file" } }],
    ["tool.execution_complete", { result: false, output: null, is_error: true, duration_ms: 0, exit_code: 1, metadata: "duplicate" }, { output: null, result: false, isError: true, durationMs: 0, exitCode: 1 }],
    ["tool.execution_complete", { toolCallId: "declined", status: "declined", exit_code: null }, { toolCallId: "declined", status: "declined", exitCode: null }],
    ["session.init", { sourceEngine: "copilot", model: "fixture", session_id: "session", tools: Array(100).fill("large descriptor") }, { sourceEngine: "copilot", model: "fixture", sessionId: "session" }],
    ["firewall.http_access", { domain: "example.com", http_status: 0, squid_request_status: "DENIED", credentials: "omit" }, { host: "example.com", status: 0, decision: "DENIED" }],
    ["firewall.steering", { eventName: "token_steering", message: "budget warning", reason: null, body: "omit" }, { event: "token_steering", message: "budget warning", reason: null }],
    [
      "firewall.steering",
      { event: "ai_credit_steering", request_id: "steered", provider: "copilot", threshold: 80, message: "[AWF AI CREDIT WARNING] budget" },
      { event: "ai_credit_steering", requestId: "steered", threshold: 80, message: "[AWF AI CREDIT WARNING] budget" },
    ],
    ["mcp.tool_call", { tool_call_id: "call", tool_name: "lookup", duration: 5, input_size: 0, output_size: 10, status: "error" }, { toolCallId: "call", toolName: "lookup", durationMs: 5, inputSize: 0, outputSize: 10, status: "error" }],
    ["execution.result", { exit_code: 0, duration_ms: 0, outcome: "success", extra: "omit" }, { exitCode: 0, durationMs: 0, outcome: "success" }],
    [
      "detection.result",
      { job_result: "success", conclusion: "warning", reason: "threat_detected", secret_leak: false, prompt_injection: false, malicious_patch: true, reasons: ["PRIVATE_REASON"], raw: "omit" },
      { jobResult: "success", conclusion: "warning", reason: "threat_detected", secretLeak: false, promptInjection: false, maliciousPatch: true },
    ],
    ["safe_output.request", { type: "create_issue", repo: "example/repo", body: "omit", title: "omit" }, { type: "create_issue", repo: "example/repo" }],
    ["safe_output.result", { type: "create_issue", number: 0, identifier: null, body: "omit" }, { type: "create_issue", number: 0, identifier: null }],
    ["eval.result", { id: "quality", answer: false, question: "omit", model: "fixture" }, { id: "quality", answer: false, model: "fixture" }],
    ["experiment.assignment", { experiment: "B" }, { assignments: { experiment: "B" } }],
    ["experiment.assignment", { assignments: "treatment", model_size: "large" }, { assignments: { assignments: "treatment", model_size: "large" } }],
    ["experiment.assignment", { assignments: { assignments: "treatment", model_size: "large" } }, { assignments: { assignments: "treatment", model_size: "large" } }],
  ])("normalizes essential %s fields and aliases", (type, data, expected) => {
    const original = structuredClone(data);
    const compact = normalizeUnifiedSessionEvent({ type, data });
    expect(compact).toEqual({ type, data: expected });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    expect(data).toEqual(original);
  });

  it("flattens RPC metadata but excludes arguments, response bodies, and error context", () => {
    const events = [
      { type: "mcp.rpc.request", data: { server_id: "github", payload: { id: 0, method: "tools/call", params: { name: "list_issues", arguments: { token: "PRIVATE_RPC_ARGUMENT" } } } } },
      { type: "mcp.rpc.response", data: { server_name: "github", payload: { id: 0, result: { content: "PRIVATE_RPC_BODY" }, error: { code: 0, message: "rejected", data: "PRIVATE_ERROR_CONTEXT" } } } },
    ];
    const compact = events.map(normalizeUnifiedSessionEvent);
    expect(compact.map(event => event.data)).toEqual([
      { serverName: "github", rpcId: 0, method: "tools/call", toolName: "list_issues" },
      { serverName: "github", rpcId: 0, error: { code: 0, message: "rejected" } },
    ]);
    expect(JSON.stringify(compact)).not.toContain("PRIVATE_");
    for (const output of [generatePlainTextSummary(compact), generateCopilotCliStyleSummary(compact)]) {
      expect(output).toContain("rpcId=0");
      expect(output).toContain("list_issues");
      expect(output).toContain("code=0");
    }
  });

  it("keeps canonical detection values, including false flags and an empty categorical reason", () => {
    const compact = normalizeUnifiedSessionEvent({
      type: "detection.result",
      data: { jobResult: "failure", job_result: "success", conclusion: "failure", reason: "", promptInjection: false, prompt_injection: true, secretLeak: false, maliciousPatch: false },
    });
    expect(compact.data).toEqual({ jobResult: "failure", conclusion: "failure", reason: "", promptInjection: false, secretLeak: false, maliciousPatch: false });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it("keeps only supplied workflow version identifiers without conflating CLI and engine versions", () => {
    const source = {
      type: "workflow.info",
      data: { engine_id: "custom", agent_version: "v2", version: "v3", cli_version: "", awf_version: "", awmg_version: "", token: "omit" },
    };
    expect(normalizeUnifiedSessionEvent(source).data).toEqual({
      engineId: "custom",
      agentVersion: "v2",
      cliVersion: "",
      awfVersion: "",
      mcpgVersion: "",
    });
    expect(normalizeUnifiedSessionEvent({ type: "workflow.info", data: { engine_id: "custom", version: "v3" } }).data).toEqual({
      engineId: "custom",
    });
    const metadata = normalizeUnifiedSessionEvent({ type: "workflow.info", data: { engine: "claude", model: "requested", event_name: "schedule", cli_version: "1.0", mcpg_version: "2.0" } });
    expect(metadata.data).toEqual({ engineId: "claude", cliVersion: "1.0", mcpgVersion: "2.0", model: "requested", requestedModel: "requested", triggerType: "schedule" });
    expect(normalizeUnifiedSessionEvent(metadata)).toEqual(metadata);
  });

  it("preserves caller context alongside recorded episode lineage and round trips canonical fields", () => {
    const source = {
      type: "workflow.info",
      data: {
        run_id: "300",
        run_attempt: "2",
        workflow_id: "org/repo/.github/workflows/worker.yml@refs/heads/main",
        workflow_call_id: "300-2:org/repo/.github/workflows/worker.yml@refs/heads/main",
        caller_workflow_ref: "org/parent/.github/workflows/caller.yml@refs/heads/main",
        repository: "org/repo",
        ref: "refs/heads/main",
        sha: "abc123",
        actor: "octocat",
        target_repo: "org/target",
        context: {
          episode_id: "100-1:root",
          hop_id: "200-1:caller",
          parent_hop_id: "100-1:root",
          origin_event: "issues",
          root_repo: "org/repo",
          root_workflow_id: "org/repo/.github/workflows/root.yml@refs/heads/main",
          root_run_id: "100",
          workflow_call_id: "200-1:caller",
          repo: "org/parent",
          run_id: "200",
          run_attempt: "1",
          workflow_id: "org/parent/.github/workflows/caller.yml@refs/heads/main",
          event_type: "workflow_dispatch",
          otel_trace_id: "trace-1",
          work_queue_assignment: { payload: { enabled: false, count: 0, items: [], optional: null } },
        },
      },
    };
    const original = structuredClone(source);
    const compact = normalizeUnifiedSessionEvent(source);
    expect(compact.data).toEqual({
      runId: "300",
      runAttempt: "2",
      workflowId: "org/repo/.github/workflows/worker.yml@refs/heads/main",
      workflowCallId: "300-2:org/repo/.github/workflows/worker.yml@refs/heads/main",
      callerWorkflowRef: "org/parent/.github/workflows/caller.yml@refs/heads/main",
      repository: "org/repo",
      ref: "refs/heads/main",
      sha: "abc123",
      actor: "octocat",
      targetRepo: "org/target",
      awContext: source.data.context,
      episode: {
        episodeId: "100-1:root",
        hopId: "200-1:caller",
        parentHopId: "100-1:root",
        originEvent: "issues",
        rootRepo: "org/repo",
        rootWorkflowId: "org/repo/.github/workflows/root.yml@refs/heads/main",
        rootRunId: "100",
        parentRepo: "org/parent",
        parentRunId: "200",
        parentRunAttempt: "1",
        parentWorkflowId: "org/parent/.github/workflows/caller.yml@refs/heads/main",
        parentWorkflowCallId: "200-1:caller",
        parentEventType: "workflow_dispatch",
      },
    });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    expect(compact.data.awContext).not.toBe(source.data.context);
    expect(compact.data.awContext.work_queue_assignment).not.toBe(source.data.context.work_queue_assignment);
    expect(source).toEqual(original);
  });

  it("retains webhook batch identities but omits raw payloads from session logs", () => {
    const context = {
      items: [{ item_type: "dependabot_alert", item_number: "1", repo: "org/repo" }],
      webhook: { event: "dependabot_alert", deliveries: [{ delivery_id: "d1", payload: { private: "do-not-log" } }] },
    };
    const before = JSON.stringify(context);
    const result = normalizeUnifiedSessionEvent({ type: "workflow.info", data: { context } });
    expect(result.data.awContext).toEqual({ items: context.items, webhook: { event: "dependabot_alert", deliveries: [{ delivery_id: "d1" }] } });
    expect(JSON.stringify(result)).not.toContain("do-not-log");
    expect(normalizeUnifiedSessionEvent(result)).toEqual(result);
    expect(JSON.stringify(context)).toBe(before);
  });

  it("preserves explicit canonical episode values and does not infer missing lineage", () => {
    const event = {
      type: "workflow.info",
      data: {
        episode: { episodeId: "", episode_id: "wrong", parentHopId: null, parent_hop_id: "wrong", rootRunId: "00100", parentRunId: "00200", run_id: "wrong", parentRepo: null, private: "omit" },
        context: { episode_id: "wrong", run_id: "wrong", repo: "wrong" },
      },
    };
    const compact = normalizeUnifiedSessionEvent(event);
    expect(compact.data).toEqual({ awContext: event.data.context, episode: { episodeId: "", parentHopId: null, rootRunId: "00100", parentRunId: "00200", parentRepo: null } });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    expect(normalizeUnifiedSessionEvent({ type: "workflow.info", data: { episode: null, context: { episode_id: "wrong" } } }).data).toEqual({ awContext: { episode_id: "wrong" }, episode: null });
    for (const context of [undefined, null, {}, []]) {
      expect(normalizeUnifiedSessionEvent({ type: "workflow.info", data: { run_id: "200", context } }).data).toEqual({
        runId: "200",
        ...(context && typeof context === "object" && !Array.isArray(context) ? { awContext: context } : {}),
      });
    }
  });

  it("prefers canonical caller context and omits absent or non-object context", () => {
    const compact = normalizeUnifiedSessionEvent({ type: "workflow.info", data: { awContext: { run_id: "100" }, context: { run_id: "wrong" } } });
    expect(compact.data).toEqual({ awContext: { run_id: "100" }, episode: { parentRunId: "100" } });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    for (const awContext of [undefined, null, [], "", false, 0]) {
      expect(normalizeUnifiedSessionEvent({ type: "workflow.info", data: { awContext, context: { run_id: "wrong" } } }).data).not.toHaveProperty("awContext");
    }
    expect(normalizeUnifiedSessionEvent({ type: "workflow.info", data: {} }).data).not.toHaveProperty("awContext");
  });

  it("projects legacy caller identity without synthesizing canonical episode or hop IDs", () => {
    const context = { workflow_call_id: "legacy", run_id: "00100", run_attempt: "02", workflow_id: "caller", repo: "org/repo", event_type: "issues" };
    const compact = normalizeUnifiedSessionEvent({ type: "workflow.info", data: { run_id: "200", context } });
    expect(compact.data.episode).toEqual({
      parentWorkflowCallId: "legacy",
      parentRunId: "00100",
      parentRunAttempt: "02",
      parentWorkflowId: "caller",
      parentRepo: "org/repo",
      parentEventType: "issues",
    });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it("keeps a reusable workflow caller distinct even when parent and current run IDs match", () => {
    const compact = normalizeUnifiedSessionEvent({
      type: "workflow.info",
      data: {
        run_id: "300",
        run_attempt: "2",
        workflow_id: "org/platform/.github/workflows/worker.lock.yml@refs/tags/v1",
        workflow_call_id: "300-2:org/platform/.github/workflows/worker.lock.yml@refs/tags/v1",
        caller_workflow_ref: "org/repo/.github/workflows/caller.yml@refs/heads/main",
        context: { run_id: "300", workflow_id: "caller.yml", workflow_call_id: "300-2:caller.yml", hop_id: "300-2:caller.yml", parent_hop_id: "100-1:root" },
      },
    });
    expect(compact.data).toMatchObject({
      runId: "300",
      runAttempt: "2",
      workflowId: "org/platform/.github/workflows/worker.lock.yml@refs/tags/v1",
      workflowCallId: "300-2:org/platform/.github/workflows/worker.lock.yml@refs/tags/v1",
      callerWorkflowRef: "org/repo/.github/workflows/caller.yml@refs/heads/main",
      episode: { parentRunId: "300", parentWorkflowId: "caller.yml", parentWorkflowCallId: "300-2:caller.yml", hopId: "300-2:caller.yml", parentHopId: "100-1:root" },
    });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it.each(["", null, false, 0])("preserves explicit canonical workflow call ID %j over its legacy alias", workflowCallId => {
    const compact = normalizeUnifiedSessionEvent({
      type: "workflow.info",
      data: { workflowCallId, workflow_call_id: "legacy" },
    });
    expect(compact.data).toEqual({ workflowCallId });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it("uses one accounting shape without losing zero values or adding overlapping totals", () => {
    const data = {
      provider: "copilot",
      model: "gpt-5.4-mini",
      purpose: "subagent",
      path: "/v1/messages",
      x_initiator: "agent",
      request_id: "request-1",
      inputTokens: 0,
      output_tokens: 2,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      ai_credits_this_response: 0,
      ai_credits_total: 0,
      duration_ms: 0,
      opaque: "omit",
    };
    const runtime = normalizeUnifiedSessionEvent({ type: "firewall.token_usage", data });
    expect(runtime.data).toEqual({
      provider: "copilot",
      model: "gpt-5.4-mini",
      purpose: "subagent",
      path: "/v1/messages",
      xInitiator: "agent",
      requestId: "request-1",
      aic: 0,
      totalAic: 0,
      durationMs: 0,
      usage: { inputTokens: 0, outputTokens: 2, cacheReadInputTokens: 0, cacheCreationInputTokens: 0 },
    });
    const report = normalizeUnifiedSessionEvent({
      type: "usage.report",
      data: { purpose: "routing_classification", path: "/responses", x_initiator: "router" },
    });
    expect(report.data).toEqual({ purpose: "routing_classification", path: "/responses", xInitiator: "router" });
    const agent = normalizeUnifiedSessionEvent({ type: "session.result", data: { num_turns: 0, usage: { inputTokens: 0, output_tokens: 2, overflowed_tokens: ["cache_read_input_tokens"], unused: 10 } } });
    expect(agent.data).toEqual({ numTurns: 0, usage: { inputTokens: 0, outputTokens: 2, overflowedTokens: ["cacheReadInputTokens"] } });
    const reconciled = reconcileSessionUsage({ cache_read_input_tokens: 4 }, agent.data.usage);
    expect(reconciled.cache_read_input_tokens).toBeUndefined();
    expect(reconciled.cacheReadInputTokens).toBeUndefined();
    expect(reconciled.overflowed_tokens).toEqual(["cache_read_input_tokens"]);
    expect(normalizeUnifiedSessionEvent(runtime)).toEqual(runtime);
  });

  it("keeps only the AWF steering notice type and threshold on token usage events", () => {
    const steered = normalizeUnifiedSessionEvent({
      type: "firewall.token_usage",
      data: { request_id: "steered", model: "claude-sonnet-5", steering: { type: "ai_credit", threshold: 90, message: "omit" } },
    });
    expect(steered.data).toEqual({ requestId: "steered", model: "claude-sonnet-5", steering: { type: "ai_credit", threshold: 90 } });
    const report = normalizeUnifiedSessionEvent({ type: "usage.report", data: { request_id: "steered", steering: { type: "timeout", threshold: 99 } } });
    expect(report.data).toEqual({ requestId: "steered", steering: { type: "timeout", threshold: 99 } });
    expect(normalizeUnifiedSessionEvent(steered)).toEqual(steered);
    for (const steering of [null, "ai_credit", [], {}, { message: "omit" }, { type: "ai_credit" }, { type: "unknown", threshold: 80 }, { type: "token", threshold: "90" }, { type: "token", threshold: 50 }]) {
      expect(normalizeUnifiedSessionEvent({ type: "firewall.token_usage", data: { request_id: "plain", steering } }).data).not.toHaveProperty("steering");
    }
    expect(normalizeUnifiedSessionEvent({ type: "firewall.token_usage", data: { request_id: "plain" } }).data).not.toHaveProperty("steering");
  });

  it("retains normalized subagent invocation and per-request usage fields", () => {
    const source = {
      type: "subagent.request",
      agentId: "invocation-1",
      data: {
        invocationId: "invocation-1",
        agentName: "reader",
        model: "gpt-5.4-mini",
        inputTokens: 10,
        outputTokens: 2,
        reasoningOutputTokens: 1,
        cacheReadTokens: 3,
        cacheWriteTokens: 4,
        discarded: "omit",
      },
    };
    expect(normalizeUnifiedSessionEvent(source)).toEqual({
      type: "subagent.request",
      agentId: "invocation-1",
      data: {
        invocationId: "invocation-1",
        agentName: "reader",
        model: "gpt-5.4-mini",
        inputTokens: 10,
        outputTokens: 2,
        reasoningOutputTokens: 1,
        cacheReadTokens: 3,
        cacheWriteTokens: 4,
      },
    });
  });

  it.each([0, 3])("keeps Codex terminal metadata and %i reasoning tokens without duplicate accounting", reasoning => {
    const native = [{ type: "turn.completed", usage: { input_tokens: 10, cached_input_tokens: 4, cache_write_input_tokens: 0, output_tokens: 5, reasoning_output_tokens: reasoning } }];
    const events = normalizeCodexSession(native);
    const compact = events.map(normalizeUnifiedSessionEvent);
    expect(compact).toEqual([
      {
        type: "session.result",
        data: {
          status: "completed",
          sourceType: "turn.completed",
          numTurns: 1,
          usage: { inputTokens: 10, outputTokens: 5, reasoningOutputTokens: reasoning, cacheReadInputTokens: 4, cacheCreationInputTokens: 0 },
        },
      },
    ]);
    expect(normalizeUnifiedSessionEvent(compact[0])).toEqual(compact[0]);
    expect(sessionTokenTotal(selectSessionResult(compact).usage)).toBe(15);
    expect(events[0].data.usage.cached_input_tokens).toBe(4);
    expect(events[0].data.usage.cache_write_input_tokens).toBe(0);
  });

  it("retains failed turns separately from nonfatal Codex diagnostics", () => {
    const events = normalizeCodexSession([
      { type: "item.completed", item: { id: "diagnostic", type: "error", message: "Model metadata unavailable." } },
      { type: "turn.failed", error: { message: "Response rejected." } },
    ]);
    expect(events.map(normalizeUnifiedSessionEvent)).toEqual([
      { type: "session.result", data: { errors: ["Model metadata unavailable."] } },
      { type: "session.result", data: { status: "failed", sourceType: "turn.failed", errors: [{ message: "Response rejected." }] } },
    ]);
  });

  it("retains grader decisions and safe-output errors without evaluator scripts", () => {
    const grader = { id: "quality", value: 0, passed: false, direction: "higher", threshold: 0, script: "PRIVATE_SCRIPT", description: "omit" };
    expect(normalizeUnifiedSessionEvent({ type: "grader.manifest", data: { graders: [grader] } }).data).toEqual({
      graders: [{ id: "quality", value: 0, passed: false, direction: "higher", threshold: 0 }],
    });
    expect(normalizeUnifiedSessionEvent({ type: "grader.result", data: { results: [grader] } }).data).toEqual({
      results: [{ id: "quality", value: 0, passed: false, direction: "higher", threshold: 0 }],
    });
    expect(normalizeUnifiedSessionEvent({ type: "safe_output.error", data: { errors: [{ type: "add_comment", error: "rejected", payload: "omit" }] } }).data).toEqual({
      errors: [{ type: "add_comment" }],
    });
  });

  it("preserves correlation, native timestamp ordering, and redaction on compact payloads", () => {
    const secret = "opaque-mask";
    const sources = [
      { component: "firewall", phase: "agent", path: "audit.jsonl", timestampUnit: "seconds", events: [{ type: "firewall.http_access", ts: 1, data: { host: "example.com", decision: "DENIED" } }] },
      { component: "agent", phase: "agent", path: "session.jsonl", events: [{ type: "assistant.message", timestamp: 999, id: secret, data: { content: secret } }] },
    ];
    const original = structuredClone(sources);
    const compact = mergeSessionSources(sources);
    expect(compact.map(event => event.provenance.timestampMs)).toEqual([999, 1000]);
    expect(compact[1]).toMatchObject({ timestamp: 1, provenance: { path: "audit.jsonl", index: 0 } });
    expect(compact[1]).not.toHaveProperty("ts");
    const serialized = serializeSessionArtifact(compact, [secret]);
    expect(serialized).not.toContain(secret);
    expect(serialized.endsWith("\n")).toBe(true);
    expect(sources).toEqual(original);
  });

  it("keeps unknown extension payloads opaque rather than guessing their essential fields", () => {
    const extension = { type: "vendor.extension", data: { future: [false, null, 0] } };
    expect(normalizeUnifiedSessionEvent(extension)).toEqual(extension);
  });

  it("keeps explicit failures when aliases or result envelopes conflict", () => {
    for (const data of [
      { isError: false, is_error: true },
      { success: true, result: { isError: true } },
      { success: true, output: null, result: { is_error: true } },
    ]) {
      const compact = normalizeUnifiedSessionEvent({ type: "tool.execution_complete", data });
      expect(compact.data.isError).toBe(true);
      expect(generatePlainTextSummary([compact])).toContain("Failed Tools: 1");
      const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "session.jsonl", events: [compact] }]);
      expect(generatePlainTextSummary(unified)).toContain("[failed]");
    }
  });

  it("retains empty usage, scalar errors, and nested observed timestamps", () => {
    expect(normalizeUnifiedSessionEvent({ type: "session.result", data: { usage: {} } }).data).toEqual({ usage: {} });
    expect(normalizeUnifiedSessionEvent({ type: "safe_output.error", data: { errors: ["rejected", null, false] } }).data).toEqual({ errors: [] });
    expect(normalizeUnifiedSessionEvent({ type: "assistant.message", message: { timestamp: 0 }, data: { content: "" } })).toEqual({ type: "assistant.message", timestamp: 0, data: { content: "" } });
  });

  it("preserves authoritative Copilot checkpoint credits and explicit zero accounting", () => {
    for (const ai_credits of [0, 2.5]) {
      const compact = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "copilot", ai_credits, premium_requests: 0 } });
      expect(compact.data).toEqual({ provider: "copilot", totalAic: ai_credits, premiumRequests: 0 });
      expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
      expect(generatePlainTextSummary([compact])).toContain(`totalAic=${ai_credits}`);
    }
  });

  it("preserves camelCase AIC checkpoints and resolves only valid priced token usage", () => {
    const checkpoint = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", aiCreditsTotal: 2.5, input_tokens: 100 } }, "evals");
    expect(checkpoint.data).toMatchObject({ totalAic: 2.5 });
    expect(checkpoint.data).not.toHaveProperty("aic");

    const cacheOnly = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", cache_read_input_tokens: 500 } }, "evals");
    expect(cacheOnly.data.aic).toBeGreaterThan(0);

    for (const inputTokens of [-5, Infinity, NaN]) {
      const invalid = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", input_tokens: inputTokens } }, "detection");
      expect(invalid.data).not.toHaveProperty("aic");
    }

    const otherPhase = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", input_tokens: 100 } }, "conclusion");
    expect(otherPhase.data).not.toHaveProperty("aic");
  });

  it("normalizes actual safe-output failure reports without dropping operation error codes", () => {
    const compact = normalizeUnifiedSessionEvent({
      type: "safe_output.error",
      data: { status: "failure", errorCode: "ERR_SAFE_OUTPUT", failures: [{ type: "add_comment", errorCode: "ERR_TARGET", error: "target unavailable" }] },
    });
    expect(compact.data).toEqual({ status: "failure", errorCode: "ERR_SAFE_OUTPUT", errors: [{ type: "add_comment", errorCode: "ERR_TARGET" }] });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    const output = generatePlainTextSummary([compact]);
    expect(output).toContain("ERR_SAFE_OUTPUT");
    expect(output).toContain("ERR_TARGET");
    expect(output).toContain("type=add_comment");
  });
});

describe("AWF model routing candidate and request metadata", () => {
  const GOLDEN_DIR = fileURLToPath(new URL("../../../pkg/cli/testdata/model_routing_golden/", import.meta.url));
  const ROUTING_LOG = "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl";
  const NEW_FIELDS = ["rankedChoices", "eligibleChoices", "catalogueOverlap", "selectedEndpoint", "requestedEndpoint", "upstreamEndpoint", "unavailable", "status", "phase"];
  const SOURCE_FIELDS = {
    rankedChoices: ["ranked_choices", "rankedChoices"],
    eligibleChoices: ["eligible_choices", "eligibleChoices"],
    catalogueOverlap: ["catalogue_overlap", "catalogueOverlap"],
    selectedEndpoint: ["selected_endpoint", "selectedEndpoint"],
    requestedEndpoint: ["requested_endpoint", "requestedEndpoint"],
    upstreamEndpoint: ["upstream_endpoint", "upstreamEndpoint"],
    unavailable: ["unavailable"],
    status: ["status"],
    phase: ["phase"],
  };
  const readJsonl = file =>
    fs
      .readFileSync(file, "utf8")
      .split("\n")
      .filter(line => line.trim())
      .map(line => JSON.parse(line));
  const readRoutingLog = name => readJsonl(`${GOLDEN_DIR}${name}/${ROUTING_LOG}`);
  const camelize = record => Object.fromEntries(Object.entries(record).map(([key, value]) => [key === "_schema" ? "schema" : key.replace(/_([a-z0-9])/g, (_, char) => char.toUpperCase()), value]));
  const SPELLINGS = [
    ["snake_case", record => structuredClone(record)],
    ["camelCase", camelize],
  ];
  const routingEvent = record => ({ type: "firewall.model_routing", ...(record.timestamp !== undefined ? { timestamp: record.timestamp } : {}), data: record });
  // Normalize once, checking input immutability, idempotence, and JSON round trips.
  const project = event => {
    const original = structuredClone(event);
    const projected = normalizeUnifiedSessionEvent(event, "agent");
    expect(event).toEqual(original);
    expect(normalizeUnifiedSessionEvent(structuredClone(projected), "agent")).toEqual(projected);
    expect(JSON.parse(JSON.stringify(projected))).toEqual(projected);
    return projected;
  };
  const withoutNewFields = record => Object.fromEntries(Object.entries(record).filter(([key]) => !Object.values(SOURCE_FIELDS).flat().includes(key)));
  const ranked = choices => choices.map((choice, index) => ({ rank: index + 1, ...choice }));

  // Minimized, sanitized copies from smoke-copilot-routed run 38984582061 (commit 354b5c3),
  // artifact `usage`, path `agent/model-routing.jsonl`, schema model-routing/v0.28.59.
  // conversation_sha256, interaction_id, github_repository, and github_workflow_ref were removed.
  const SMOKE_SELECTION = {
    _schema: "model-routing/v0.28.59",
    timestamp: "2026-10-17T00:05:52.673Z",
    event: "model_routing",
    stage: "selection",
    objective: { goal: "cost", mode: "economy" },
    provider: "copilot",
    selected_provider: "copilot",
    wire_model: "gpt-5.6-luna",
    endpoint: "/responses",
    labels: { task_type: "chore", scope: "local", task_complexity: "trivial" },
    mode: "economy",
    classifier_model: "github-copilot/gpt-5.6-luna",
    classifier_effort: "high",
    router: { name: "gh-aw-router", version: "0.1.3" },
    ranked_choices: [
      { model: "github-copilot/gpt-5.6-luna", effort: "high" },
      { model: "github-copilot/gpt-5.6-luna", effort: "xhigh" },
      { model: "github-copilot/gpt-5.6-luna", effort: "max" },
      { model: "github-copilot/gpt-5.6-luna", effort: "medium" },
      { model: "github-copilot/gpt-5.4-mini", effort: "xhigh" },
      { model: "github-copilot/gpt-5.4-mini", effort: "high" },
      { model: "github-copilot/gpt-5.4-mini", effort: "medium" },
      { model: "github-copilot/gpt-5.6-luna", effort: "low" },
      { model: "github-copilot/gpt-5.4-mini", effort: "low" },
      { model: "github-copilot/claude-haiku-4.5", effort: null },
      { model: "github-copilot/gpt-5.6-luna", effort: "none" },
      { model: "github-copilot/gpt-5.4-mini", effort: "none" },
    ],
    selected_id: "choice-0007",
    selected_model: "github-copilot/gpt-5.6-luna",
    selected_effort: "high",
    degraded_classification: false,
    degraded_reason: null,
    classifier_attempts: 1,
    eligible_choices: 12,
    catalogue_overlap: 12,
    latency_ms: 4212,
  };
  const SMOKE_REQUEST = {
    _schema: "model-routing/v0.28.59",
    timestamp: "2026-10-17T00:06:03.778Z",
    event: "model_routing",
    stage: "request",
    routed: "as_selected",
    deviations: [],
    unavailable: [],
    method: "POST",
    pathname: "/responses",
    provider: "copilot",
    requested_model: "gpt-5.6-luna",
    requested_effort: "high",
    selected_provider: "copilot",
    selected_model: "gpt-5.6-luna",
    selected_effort: "high",
    selected_endpoint: "/responses",
    requested_endpoint: "/responses",
    upstream_endpoint: "/responses",
    request_id: "3e8b8c51-1a07-46a5-a21f-c8ecea381bf4",
    outcome: "completed",
    status: 200,
  };
  // Recorded raw AWF logs: run 37728959715 (model-routing/v0.28.44) and run 38174216426 (model-routing/v0.28.51).
  const CODEX = readRoutingLog("codex-routed-provider-unavailable");
  const BUDGET = readRoutingLog("copilot-budget-exhausted-steering");
  const recorded = (records, predicate) => {
    const record = records.find(predicate);
    expect(record).toBeDefined();
    return record;
  };
  const CODEX_SELECTION = recorded(CODEX, record => record.stage === "selection");
  const BUDGET_SELECTION = recorded(BUDGET, record => record.stage === "selection");
  const CODEX_REQUEST = recorded(CODEX, record => record.request_id === "ff510188-891a-4ad1-97eb-a12c47c2d7fc");
  const BUDGET_REQUEST = recorded(BUDGET, record => record.request_id === "2698b73b-55b7-4c97-8d6d-8d7c88b69461");
  const CODEX_FAILURE = recorded(CODEX, record => record.timestamp === "2026-10-08T04:47:41.353Z");
  const BUDGET_FAILURE = recorded(BUDGET, record => record.timestamp === "2026-10-11T20:59:12.712Z");

  it.each(SPELLINGS)("preserves recorded AWF selection rankings and pool counts in source order (%s)", (_, spell) => {
    const smoke = project(routingEvent(spell(SMOKE_SELECTION)));
    expect(smoke.data.rankedChoices).toEqual([
      { rank: 1, model: "github-copilot/gpt-5.6-luna", effort: "high" },
      { rank: 2, model: "github-copilot/gpt-5.6-luna", effort: "xhigh" },
      { rank: 3, model: "github-copilot/gpt-5.6-luna", effort: "max" },
      { rank: 4, model: "github-copilot/gpt-5.6-luna", effort: "medium" },
      { rank: 5, model: "github-copilot/gpt-5.4-mini", effort: "xhigh" },
      { rank: 6, model: "github-copilot/gpt-5.4-mini", effort: "high" },
      { rank: 7, model: "github-copilot/gpt-5.4-mini", effort: "medium" },
      { rank: 8, model: "github-copilot/gpt-5.6-luna", effort: "low" },
      { rank: 9, model: "github-copilot/gpt-5.4-mini", effort: "low" },
      { rank: 10, model: "github-copilot/claude-haiku-4.5", effort: null },
      { rank: 11, model: "github-copilot/gpt-5.6-luna", effort: "none" },
      { rank: 12, model: "github-copilot/gpt-5.4-mini", effort: "none" },
    ]);
    expect(smoke.data.rankedChoices.some(choice => Object.hasOwn(choice, "id"))).toBe(false);
    expect(smoke.data).toMatchObject({ eligibleChoices: 12, catalogueOverlap: 12, endpoint: "/responses", selectedId: "choice-0007" });
    expect(smoke.data).not.toHaveProperty("selectedEndpoint");

    for (const [record, count] of [
      [CODEX_SELECTION, 23],
      [BUDGET_SELECTION, 6],
    ]) {
      const projected = project(routingEvent(spell(record)));
      expect(projected.data.rankedChoices).toHaveLength(count);
      expect(projected.data.rankedChoices.map(choice => choice.rank)).toEqual(Array.from({ length: count }, (_, index) => index + 1));
      expect(projected.data.rankedChoices.map(({ model, effort }) => ({ model, effort }))).toEqual(record.ranked_choices);
      expect(projected.data).toMatchObject({ eligibleChoices: count, catalogueOverlap: count });
    }

    // Contract-compatibility variant: AWF route choices carry an `id` (routing-controller.js createSelection),
    // but current persistence does not emit it. A supplied ID is kept; none is invented.
    const withIds = structuredClone(SMOKE_SELECTION);
    withIds.ranked_choices[0].id = "choice-0007";
    const projected = project(routingEvent(spell(withIds)));
    expect(projected.data.rankedChoices[0]).toEqual({ rank: 1, id: "choice-0007", model: "github-copilot/gpt-5.6-luna", effort: "high" });
    expect(projected.data.rankedChoices.slice(1).some(choice => Object.hasOwn(choice, "id"))).toBe(false);

    // A supplied selected endpoint on a selection is retained exactly; an empty ranking and zero counts stay supplied values.
    const supplied = project(routingEvent(spell({ ...SMOKE_SELECTION, selected_endpoint: "/chat/completions", ranked_choices: [], eligible_choices: 0, catalogue_overlap: 0 })));
    expect(supplied.data).toMatchObject({ endpoint: "/responses", selectedEndpoint: "/chat/completions", rankedChoices: [], eligibleChoices: 0, catalogueOverlap: 0 });
  });

  it.each(SPELLINGS)("preserves recorded AWF request endpoints HTTP status and unavailable metadata (%s)", (_, spell) => {
    for (const [record, status, outcome] of [
      [SMOKE_REQUEST, 200, "completed"],
      [CODEX_REQUEST, 400, "failed"],
      [BUDGET_REQUEST, 403, "rejected"],
    ]) {
      const projected = project(routingEvent(spell(record)));
      expect(projected.data).toMatchObject({
        selectedEndpoint: "/responses",
        requestedEndpoint: "/responses",
        upstreamEndpoint: "/responses",
        unavailable: [],
        status,
        outcome,
        requestId: record.request_id,
        routed: record.routed,
        deviations: record.deviations,
        requestedModel: record.requested_model,
        requestedEffort: record.requested_effort,
        selectedModel: record.selected_model,
        selectedEffort: record.selected_effort,
      });
      expect(projected.data).not.toHaveProperty("endpoint");
      const { unavailable: _unavailable, ...withoutUnavailable } = record;
      expect(project(routingEvent(spell(withoutUnavailable))).data).not.toHaveProperty("unavailable");
    }
    // AWF lists unobserved telemetry dimensions, not unavailable models; the array is kept as supplied.
    const unobserved = project(routingEvent(spell({ ...SMOKE_REQUEST, unavailable: ["model", "effort"] })));
    expect(unobserved.data.unavailable).toEqual(["model", "effort"]);
  });

  it.each(SPELLINGS)("preserves recorded AWF failure phase independently of collection provenance (%s)", (_, spell) => {
    for (const [record, code] of [
      [CODEX_FAILURE, "provider_unavailable"],
      [BUDGET_FAILURE, "ai_credits_limit_exceeded"],
    ]) {
      const event = routingEvent(spell(record));
      const projected = project(event);
      expect(projected.timestamp).toBe(record.timestamp);
      expect(projected.data).toEqual({ schema: record._schema, stage: "failure", phase: "primary", code });
      const [merged] = mergeSessionSources([{ component: "firewall", phase: "agent", path: ROUTING_LOG, events: [structuredClone(event)] }]);
      expect(merged.provenance).toMatchObject({ component: "firewall", phase: "agent", path: ROUTING_LOG });
      expect(merged.data.phase).toBe("primary");
      expect(merged.timestamp).toBe(record.timestamp);
      const { phase: _phase, ...withoutPhase } = record;
      expect(project(routingEvent(spell(withoutPhase))).data).not.toHaveProperty("phase");
    }
  });

  it.each(SPELLINGS)("leaves absent AWF routing metadata absent for older session records (%s)", (_, spell) => {
    // Already-projected model-routing/v0.28.51 selection from copilot-budget-exhausted-steering/usage/aw_session.jsonl.
    const session = readJsonl(`${GOLDEN_DIR}copilot-budget-exhausted-steering/usage/aw_session.jsonl`);
    const older = session.find(event => event.type === "firewall.model_routing" && event.timestamp === "2026-10-11T20:58:04.898Z");
    expect(older.data.stage).toBe("selection");
    const { provenance: _provenance, ...olderEvent } = older;
    const projectedOlder = project(olderEvent);
    expect(projectedOlder.data).toEqual(older.data);
    for (const field of NEW_FIELDS) expect(Object.hasOwn(projectedOlder.data, field)).toBe(false);

    // Compatibility variants: the real selection, request, and failure inputs with the new fields omitted.
    for (const record of [SMOKE_SELECTION, CODEX_SELECTION, SMOKE_REQUEST, CODEX_REQUEST, BUDGET_REQUEST, CODEX_FAILURE, BUDGET_FAILURE]) {
      const full = project(routingEvent(spell(record)));
      const stripped = project(routingEvent(spell(withoutNewFields(record))));
      for (const field of NEW_FIELDS) expect(Object.hasOwn(stripped.data, field)).toBe(false);
      expect(stripped.data).toEqual(Object.fromEntries(Object.entries(full.data).filter(([key]) => !NEW_FIELDS.includes(key))));
    }
  });

  it.each(SPELLINGS)("excludes private AWF routing fields while retaining only candidate metadata (%s)", (_, spell) => {
    // Privacy-test sentinels in real AWF field names; these values are not recorded telemetry.
    const sentinels = { conversation_sha256: "PRIVACY_TEST_CONVERSATION_SHA256", detail: "PRIVACY_TEST_DETAIL", method: "PRIVACY_TEST_METHOD", pathname: "PRIVACY_TEST_PATHNAME" };
    const selection = { ...SMOKE_SELECTION, ...sentinels };
    // Privacy variant: unexpected nested candidate properties must not be copied through.
    selection.ranked_choices = selection.ranked_choices.map(choice => ({ ...choice, predicted_success: 0.9, explanation: "PRIVACY_TEST_EXPLANATION", payload: { prompt: "PRIVACY_TEST_PROMPT" } }));
    // AWF controller failure shape (routing-controller.js, stage "failure" with `detail`), not a recorded run.
    const controllerFailure = {
      _schema: "model-routing/v0.28.59",
      timestamp: "2026-10-17T00:05:52.700Z",
      event: "model_routing",
      stage: "failure",
      objective: { goal: "cost", mode: "economy" },
      code: "routing_configuration_error",
      catalogue_overlap: 12,
      latency_ms: 10,
      ...sentinels,
    };
    for (const [record, expected] of [
      [selection, project(routingEvent(spell(SMOKE_SELECTION))).data],
      [{ ...SMOKE_REQUEST, ...sentinels }, project(routingEvent(spell(SMOKE_REQUEST))).data],
      [{ ...CODEX_FAILURE, ...sentinels }, project(routingEvent(spell(CODEX_FAILURE))).data],
      [controllerFailure, { schema: "model-routing/v0.28.59", stage: "failure", objective: { goal: "cost", mode: "economy" }, code: "routing_configuration_error", catalogueOverlap: 12, latencyMs: 10 }],
    ]) {
      const projected = project(routingEvent(spell(record)));
      expect(projected.data).toEqual(expected);
      const serialized = JSON.stringify(projected);
      expect(serialized).not.toContain("PRIVACY_TEST");
      for (const key of ["conversation_sha256", "conversationSha256", "detail", "method", "pathname", "predicted_success", "explanation", "payload"]) expect(serialized).not.toContain(`"${key}"`);
      for (const choice of projected.data.rankedChoices ?? []) expect(Object.keys(choice).every(key => ["rank", "id", "model", "effort"].includes(key))).toBe(true);
    }
  });

  it("projects recorded model-routing goldens with only the intended metadata additions", () => {
    const validate = createSessionValidator("unified").event;
    const cases = fs.readdirSync(GOLDEN_DIR).filter(name => fs.existsSync(`${GOLDEN_DIR}${name}/${ROUTING_LOG}`));
    expect(cases).toEqual(expect.arrayContaining(["codex-routed-provider-unavailable", "copilot-budget-exhausted-steering"]));
    const sessionRouting = name => {
      const file = `${GOLDEN_DIR}${name}/usage/aw_session.jsonl`;
      return fs.existsSync(file) ? readJsonl(file).filter(event => event.type === "firewall.model_routing") : [];
    };
    for (const name of cases) {
      const raw = readRoutingLog(name).map(record => ({ record, event: routingEvent(record) }));
      const sessions = sessionRouting(name).map(({ provenance: _provenance, ...event }) => ({ record: event.data, event }));
      for (const { record, event } of [...raw, ...sessions]) {
        const projected = project(event);
        for (const field of NEW_FIELDS) {
          const alias = SOURCE_FIELDS[field].find(key => Object.hasOwn(record, key));
          expect(Object.hasOwn(projected.data, field), `${name} ${field}`).toBe(alias !== undefined);
          if (alias === undefined) continue;
          if (field === "rankedChoices") expect(projected.data.rankedChoices).toEqual(ranked(record[alias].map(({ model, effort }) => ({ model, effort }))));
          else expect(projected.data[field]).toEqual(record[alias]);
        }
        const [published] = serializeSessionArtifact(mergeSessionSources([{ component: "firewall", phase: "agent", path: ROUTING_LOG, events: [event] }]))
          .trim()
          .split("\n")
          .map(line => JSON.parse(line));
        expect(validate(published), `${name}: ${JSON.stringify(validate.errors)}`).toBe(true);
        expect(published.data).toEqual(projected.data);
      }
      // Already-projected sessions gain nothing they did not supply.
      for (const { event } of sessions.filter(({ record }) => !Object.hasOwn(record, "_schema"))) expect(project(event).data).toEqual(event.data);
    }
  });
});

describe("AWF model inventories with unknown endpoints and live reasoning metadata", () => {
  const FIXTURE_DIR = fileURLToPath(new URL("./test_data/awf_reflect/", import.meta.url));
  const GOLDEN_DIR = fileURLToPath(new URL("../../../pkg/cli/testdata/model_routing_golden/", import.meta.url));
  const REFLECT_PATH = "sandbox/firewall/awf-reflect.json";
  const INVENTORY_SOURCES = ["agent/awf-reflect.json", REFLECT_PATH, "sandbox/firewall/models.json"];
  // Recorded fixtures; see test_data/awf_reflect/README.md for run, artifact, and digest provenance.
  const RECORDED = { file: "run-38995410124-awf-reflect.json", sha256: "9d59511015584d359b4159737d1d44ed12ad9426a1690e62148ab53b7543648a" };
  const LEGACY = { file: "run-29812420778-legacy-awf-reflect.json", sha256: "da2570969e0d62bd10195f029b5472f750404b515784d8c68e9b267e0975b410" };
  const ENDPOINT_KEYS = ["provider", "configured", "models", "routingModels"];
  const ROUTING_KEYS = ["modelId", "supportedEndpoints", "supportedReasoningEfforts", "contextWindowTokens"];
  const sha256 = bytes => crypto.createHash("sha256").update(bytes).digest("hex");
  const readFixture = ({ file, sha256: digest }) => {
    const bytes = fs.readFileSync(path.join(FIXTURE_DIR, file));
    expect(sha256(bytes)).toBe(digest);
    return bytes;
  };
  const classify = endpoint => (!Object.hasOwn(endpoint, "models") ? "unknown" : endpoint.models.length ? "listed" : "empty");
  const roots = [];
  afterEach(() => {
    for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
  });

  // Collect and publish from a root containing only the stated inventory source.
  function publish(files) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-model-inventory-"));
    roots.push(root);
    for (const [file, content] of Object.entries(files)) {
      fs.mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
      fs.writeFileSync(path.join(root, file), content);
    }
    const warn = vi.fn();
    writeUnifiedSession({ rootDir: root, warn, dailyAIC: {}, jobResults: "" });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(content)).toBeGreaterThan(0);
    const events = content
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    return {
      events,
      warn,
      inventories: events.filter(event => event.type === "model.inventory"),
      inventoryWarnings: events.filter(event => event.type === "session.collection_warning" && event.data.code === "invalid_model_inventory"),
    };
  }

  // Canonical projection is deterministic, idempotent, and survives JSON round trips.
  function expectStableProjection(data) {
    expect(normalizeModelInventoryData(data)).toEqual(data);
    expect(normalizeModelInventoryData(JSON.parse(JSON.stringify(data)))).toEqual(data);
    const event = normalizeUnifiedSessionEvent({ type: "model.inventory", data });
    expect(event.data).toEqual(data);
    expect(normalizeUnifiedSessionEvent(structuredClone(event))).toEqual(event);
    expect(JSON.stringify(normalizeModelInventoryData(data))).toBe(JSON.stringify(data));
  }

  function expectAllowlisted(data) {
    for (const endpoint of data.endpoints) {
      expect(Object.keys(endpoint).every(key => ENDPOINT_KEYS.includes(key))).toBe(true);
      for (const routing of endpoint.routingModels ?? []) expect(Object.keys(routing).every(key => ROUTING_KEYS.includes(key))).toBe(true);
    }
    const serialized = JSON.stringify(data);
    for (const value of ["http", "api-proxy", "base_url", "models_url", "credential", "Bearer", "github_token", "pricing", "candidate_metadata_complete", '"source"']) expect(serialized).not.toContain(value);
  }

  it("preserves the complete recorded 38995410124 inventory with unknown unconfigured endpoints", () => {
    const bytes = readFixture(RECORDED);
    const source = JSON.parse(bytes.toString("utf8"));
    const original = structuredClone(source);
    const { inventories, inventoryWarnings, warn } = publish({ [REFLECT_PATH]: bytes });
    expect(inventories).toHaveLength(1);
    expect(inventoryWarnings).toHaveLength(0);
    expect(warn).not.toHaveBeenCalledWith(expect.stringContaining("invalid_model_inventory"));
    const [inventory] = inventories;
    expect(inventory.provenance).toEqual({ component: "firewall", phase: "agent", path: REFLECT_PATH, index: 0 });
    const { data } = inventory;
    expect(data.modelsFetchComplete).toBe(true);
    expect(data.endpoints.map(endpoint => endpoint.provider)).toEqual(source.endpoints.map(endpoint => endpoint.provider));
    expect(data.endpoints).toHaveLength(5);
    for (const [index, endpoint] of data.endpoints.entries()) {
      const recorded = source.endpoints[index];
      if (endpoint.provider === "copilot") continue;
      expect(["openai", "anthropic", "gemini", "vertex"]).toContain(endpoint.provider);
      expect(recorded.models).toBeNull();
      expect(recorded.routing_models).toBeNull();
      expect(endpoint).toEqual({ provider: recorded.provider, configured: false });
      expect(Object.hasOwn(endpoint, "models")).toBe(false);
      expect(Object.hasOwn(endpoint, "routingModels")).toBe(false);
    }
    const copilot = data.endpoints.find(endpoint => endpoint.provider === "copilot");
    const recordedCopilot = source.endpoints.find(endpoint => endpoint.provider === "copilot");
    expect(copilot.configured).toBe(true);
    expect(copilot.models).toHaveLength(40);
    expect(copilot.models).toEqual(recordedCopilot.models);
    expect(copilot.routingModels).toHaveLength(40);
    expect(copilot.routingModels.map(entry => entry.modelId)).toEqual(recordedCopilot.routing_models.map(entry => entry.model_id));
    expect(copilot.routingModels.map(entry => entry.supportedEndpoints)).toEqual(recordedCopilot.routing_models.map(entry => entry.supported_endpoints));
    expect(copilot.routingModels.map(entry => entry.supportedReasoningEfforts)).toEqual(recordedCopilot.routing_models.map(entry => entry.supported_reasoning_efforts));
    expect(copilot.routingModels.some(entry => entry.supportedReasoningEfforts.length === 0)).toBe(true);
    const known = recordedCopilot.routing_models.filter(entry => entry.context_window_tokens !== null);
    expect(known).toHaveLength(36);
    for (const [index, entry] of copilot.routingModels.entries()) {
      const recordedWindow = recordedCopilot.routing_models[index].context_window_tokens;
      if (recordedWindow === null) expect(Object.hasOwn(entry, "contextWindowTokens")).toBe(false);
      else expect(entry.contextWindowTokens).toBe(recordedWindow);
    }
    expect(copilot.routingModels.filter(entry => !Object.hasOwn(entry, "contextWindowTokens"))).toHaveLength(4);
    expectAllowlisted(data);
    expectStableProjection(data);
    expect(normalizeModelInventoryData(source)).toEqual(data);
    expect(source).toEqual(original);
    expect(sha256(fs.readFileSync(path.join(FIXTURE_DIR, RECORDED.file)))).toBe(RECORDED.sha256);
  });

  it("keeps a configured pending endpoint unknown instead of inventing an empty model list", () => {
    // Pending-fetch variant of the recorded reflection: the recorded Copilot endpoint with null collections, fetch incomplete.
    const recorded = JSON.parse(readFixture(RECORDED).toString("utf8"));
    const recordedCopilot = recorded.endpoints.find(endpoint => endpoint.provider === "copilot");
    const pending = { ...recorded, models_fetch_complete: false, endpoints: [{ ...recordedCopilot, models: null, routing_models: null }] };
    const original = structuredClone(pending);
    const { inventories, inventoryWarnings } = publish({ [REFLECT_PATH]: JSON.stringify(pending) });
    expect(inventories).toHaveLength(1);
    expect(inventoryWarnings).toHaveLength(0);
    const { data } = inventories[0];
    expect(data).toEqual({ endpoints: [{ provider: "copilot", configured: true }], modelsFetchComplete: false });
    const [endpoint] = data.endpoints;
    expect(Object.hasOwn(endpoint, "models")).toBe(false);
    expect(Object.hasOwn(endpoint, "routingModels")).toBe(false);
    expect(classify(endpoint)).toBe("unknown");
    const roundTrip = JSON.parse(JSON.stringify(normalizeModelInventoryData(data)));
    expect(roundTrip.endpoints[0]).toEqual({ provider: "copilot", configured: true });
    expect(classify(roundTrip.endpoints[0])).toBe("unknown");
    expectStableProjection(data);
    expect(pending).toEqual(original);
  });

  it("preserves the existing projection of a recorded legacy golden reflection", () => {
    // Recorded legacy reflection from run 29812420778, which predates AWF routing metadata.
    // No golden case retains its source reflection; see test_data/awf_reflect/README.md.
    const bytes = readFixture(LEGACY);
    const source = JSON.parse(bytes.toString("utf8"));
    const original = structuredClone(source);
    expect(source.endpoints.some(endpoint => Object.hasOwn(endpoint, "routing_models"))).toBe(false);
    expect(Object.hasOwn(source, "routing_models")).toBe(false);
    // Output of the pre-change normalizer on this file: AWF null model lists became empty arrays and `configured` was dropped.
    const preChange = { endpoints: source.endpoints.map(endpoint => ({ provider: endpoint.provider, models: endpoint.models ?? [] })), modelsFetchComplete: true };
    const { inventories, inventoryWarnings } = publish({ [REFLECT_PATH]: bytes });
    expect(inventories).toHaveLength(1);
    expect(inventoryWarnings).toHaveLength(0);
    const { data } = inventories[0];
    expect(data.endpoints.map(endpoint => endpoint.provider)).toEqual(preChange.endpoints.map(endpoint => endpoint.provider));
    expect(data.modelsFetchComplete).toBe(preChange.modelsFetchComplete);
    for (const [index, endpoint] of data.endpoints.entries()) {
      const recorded = source.endpoints[index];
      expect(endpoint.configured).toBe(recorded.configured);
      if (recorded.models === null) {
        // Only correction: an unconfigured endpoint's unknown list is no longer reported as empty.
        expect(endpoint).toEqual({ provider: recorded.provider, configured: recorded.configured });
        expect(preChange.endpoints[index]).toEqual({ provider: recorded.provider, models: [] });
      } else {
        expect(endpoint).toEqual({ ...preChange.endpoints[index], configured: recorded.configured });
        expect(endpoint.models).toHaveLength(36);
      }
      expect(Object.hasOwn(endpoint, "routingModels")).toBe(false);
    }
    expect(JSON.stringify(data)).not.toMatch(/supportedReasoningEfforts|contextWindowTokens|routingModels/);
    expectAllowlisted(data);
    expectStableProjection(data);
    expect(source).toEqual(original);
  });

  it("warns and emits no inventory for a recorded reflection with a non-string model ID", () => {
    // Malformed-input variant of the recorded reflection: only the first Copilot model ID is numeric.
    const malformed = JSON.parse(readFixture(RECORDED).toString("utf8"));
    const copilot = malformed.endpoints.find(endpoint => endpoint.provider === "copilot");
    copilot.models[0] = 42;
    expect(malformed.endpoints.filter(endpoint => endpoint.models === null)).toHaveLength(4);
    expect(normalizeModelInventoryData(malformed)).toBeUndefined();
    const { events, inventories, inventoryWarnings } = publish({ [REFLECT_PATH]: JSON.stringify(malformed) });
    expect(inventories).toHaveLength(0);
    expect(inventoryWarnings).toHaveLength(1);
    expect(inventoryWarnings[0].data).toEqual({ path: REFLECT_PATH, code: "invalid_model_inventory" });
    expect(JSON.stringify(events)).not.toContain('"endpoints"');
  });

  it.each([
    ["non-array reasoning efforts", entry => (entry.supported_reasoning_efforts = "high")],
    ["non-string reasoning effort", entry => (entry.supported_reasoning_efforts = ["high", 42])],
    ["free-form reasoning effort", entry => (entry.supported_reasoning_efforts = ["<b>high</b>"])],
    ["zero context window", entry => (entry.context_window_tokens = 0)],
    ["fractional context window", entry => (entry.context_window_tokens = 1.5)],
    ["string context window", entry => (entry.context_window_tokens = "200000")],
    ["non-boolean configured", (_, endpoint) => (endpoint.configured = "true")],
  ])("warns and emits no inventory for a recorded reflection variant with %s", (_, mutate) => {
    // Malformed-input variant of the recorded reflection; the unconfigured endpoint nulls stay unchanged.
    const malformed = JSON.parse(readFixture(RECORDED).toString("utf8"));
    const copilot = malformed.endpoints.find(endpoint => endpoint.provider === "copilot");
    mutate(copilot.routing_models[0], copilot);
    expect(normalizeModelInventoryData(malformed)).toBeUndefined();
    const { inventories, inventoryWarnings } = publish({ [REFLECT_PATH]: JSON.stringify(malformed) });
    expect(inventories).toHaveLength(0);
    expect(inventoryWarnings.map(event => event.data)).toEqual([{ path: REFLECT_PATH, code: "invalid_model_inventory" }]);
  });

  it("limits recorded model-routing golden changes to corrected inventory events", () => {
    const cases = fs.readdirSync(GOLDEN_DIR).filter(name => fs.statSync(path.join(GOLDEN_DIR, name)).isDirectory());
    expect(cases.length).toBeGreaterThan(0);
    for (const name of cases) {
      // Historical goldens never retained an inventory source, so none is synthesized for them.
      for (const source of INVENTORY_SOURCES) expect(fs.existsSync(path.join(GOLDEN_DIR, name, source)), `${name} ${source}`).toBe(false);
      const sessionFile = path.join(GOLDEN_DIR, name, "usage/aw_session.jsonl");
      if (fs.existsSync(sessionFile)) {
        const recorded = fs.readFileSync(sessionFile, "utf8");
        expect(recorded, name).not.toContain('"model.inventory"');
        expect(recorded, name).not.toContain("invalid_model_inventory");
      }
      const { events } = collectUnifiedSession({ rootDir: path.join(GOLDEN_DIR, name), warn: () => {}, dailyAIC: {}, jobResults: "" });
      expect(
        events.some(event => event.type === "model.inventory"),
        name
      ).toBe(false);
      expect(
        events.some(event => event.data?.code === "invalid_model_inventory"),
        name
      ).toBe(false);
    }
    // For the recorded inventory sources, the only changes a source makes are its single inventory event and its collection bookkeeping.
    for (const fixture of [RECORDED, LEGACY]) {
      const withSource = publish({ [REFLECT_PATH]: readFixture(fixture) });
      const withoutSource = publish({});
      expect(withSource.inventories).toHaveLength(1);
      expect(withSource.inventoryWarnings).toHaveLength(0);
      const unrelated = events => events.filter(event => event.type !== "model.inventory" && event.type !== "session.collection");
      expect(unrelated(withSource.events)).toEqual(unrelated(withoutSource.events));
      const collection = events => events.find(event => event.type === "session.collection").data;
      const baseline = collection(withoutSource.events);
      expect(collection(withSource.events)).toEqual({
        ...baseline,
        sources: [...baseline.sources, { component: "firewall", phase: "agent", path: REFLECT_PATH, events: 1 }],
        untimedEvents: baseline.untimedEvents + 1,
        absentComponents: baseline.absentComponents.filter(component => component !== "firewall"),
      });
    }
  });
});
