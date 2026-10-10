import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const native = require("./fixtures/copilot_ci_audit.cjs");
const { normalizeCopilotSession, hasCopilotConversation } = require("./copilot_session.cjs");
const { parseCopilotLog } = require("./parse_copilot_log.cjs");
const { projectSessionResult, sessionContext, sessionToolSuccess } = require("./agent_session.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { collectUnifiedSession } = require("./unified_session.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");

describe("Copilot run-backed accounting and publication", () => {
  it("retains the observed reasoning-token zero through parser, persisted trace and essential projection", () => {
    const original = structuredClone(native);
    const parsed = parseCopilotLog(native.map(event => JSON.stringify(event)).join("\n"));
    const persisted = JSON.parse(JSON.stringify(parsed.logEntries));
    const essential = persisted.map(event => normalizeUnifiedSessionEvent(event));
    expect(projectSessionResult(persisted)).toMatchObject({
      duration_ms: 43821,
      usage: { input_tokens: 98534, output_tokens: 1426, reasoning_output_tokens: 0, cache_read_input_tokens: 56564, cache_creation_input_tokens: 34441 },
    });
    expect(essential.find(event => event.type === "session.result").data.usage).toEqual({
      inputTokens: 98534,
      outputTokens: 1426,
      reasoningOutputTokens: 0,
      cacheReadInputTokens: 56564,
      cacheCreationInputTokens: 34441,
    });
    expect(projectSessionResult(persisted).total_cost_usd).toBeUndefined();
    expect(projectSessionResult(persisted).num_turns).toBeUndefined();
    expect(parsed.markdown).not.toContain("PRIVATE_AUDIT_PROMPT");
    expect(parsed.markdown).not.toContain("PRIVATE_TRANSFORMED_PROMPT");
    expect(essential.find(event => event.type === "assistant.reasoning").data.content).toBe("  Observed reasoning.\r\n");
    expect(essential.find(event => event.type === "tool.execution_complete").data).toMatchObject({ success: false, error: { code: "PERMISSION_DENIED" } });
    expect(normalizeCopilotSession(persisted)).toEqual(persisted);
    expect(native).toEqual(original);
  });

  it("upgrades older managed projection snapshots without duplicating their native observations", () => {
    const persisted = JSON.parse(JSON.stringify(normalizeCopilotSession(native)));
    for (const event of persisted) {
      if (!event.copilotProjection || event.type === "session.init") continue;
      delete event.data.sessionId;
      delete event.data.sourceEngine;
      if (event.data.usage) delete event.data.usage.reasoning_output_tokens;
    }
    const original = structuredClone(persisted);
    const upgraded = normalizeCopilotSession(persisted);
    expect(upgraded).toHaveLength(persisted.length);
    expect(projectSessionResult(upgraded).usage.reasoning_output_tokens).toBe(0);
    expect(upgraded.filter(event => !event.copilotProjection)).toEqual(persisted.filter(event => !event.copilotProjection));
    expect(persisted).toEqual(original);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(upgraded)))).toEqual(JSON.parse(JSON.stringify(upgraded)));
  });
});

// Supplemental child, retry, streaming and SDK cases use installed SDK signatures.
// The sampled CI runs use the native CLI and do not establish SDK-driver coverage.
describe("Copilot scoped partial and snapshot evidence (synthetic)", () => {
  it("retains error-only native histories in collection rather than requiring a conversation", () => {
    const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-error-audit-"));
    const events = [
      { type: "session.start", id: "start", data: { sessionId: "failed", selectedModel: "observed-model" } },
      { type: "session.error", id: "error", data: { errorType: "authentication", message: "Observed authentication failure.", statusCode: 401 } },
    ];
    try {
      const directory = path.join(rootDir, "sandbox/agent/logs/copilot-session-state/failed");
      fs.mkdirSync(directory, { recursive: true });
      fs.writeFileSync(path.join(directory, "events.jsonl"), events.map(event => JSON.stringify(event)).join("\n") + "\n");
      const normalized = normalizeCopilotSession(events);
      expect(hasCopilotConversation(normalized)).toBe(true);
      expect(hasCopilotConversation(normalizeCopilotSession([events[0]]))).toBe(false);
      expect(hasCopilotConversation(normalizeCopilotSession([{ type: "assistant.usage", data: { inputTokens: 0 } }]))).toBe(true);
      const collected = collectUnifiedSession({ rootDir, engine: "copilot" }).events;
      expect(collected.find(event => event.type === "session.error").data).toEqual(events[1].data);
      expect(projectSessionResult(collected).errors).toEqual([events[1].data]);
      expect(collected.some(event => event.type === "assistant.message" || event.type === "tool.execution_complete")).toBe(false);
    } finally {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  });

  it.each([false, true])("retains valid partial native evidence and explicit warnings without replaying stdio (initialized: %s)", initialized => {
    const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-partial-audit-"));
    const nativePath = "sandbox/agent/logs/copilot-session-state/partial/events.jsonl";
    const records = [
      ...(initialized ? [{ type: "session.start", id: "start", data: { sessionId: "partial", selectedModel: "observed-model" } }] : []),
      { type: "assistant.message", id: "partial-answer", data: { content: "  Partial answer.\n" } },
      { type: "tool.execution_start", id: "pending", data: { toolCallId: "pending", toolName: "bash", input: false } },
      { type: "session.error", id: "interrupted", data: { message: "Observed interruption.", code: 0 } },
      { type: "vendor.partial", id: "extension", data: { content: "", value: null } },
    ];
    try {
      const file = path.join(rootDir, nativePath);
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(
        file,
        `${JSON.stringify(records[0])}\nPRIVATE_INVALID_RECORD\n${records
          .slice(1)
          .map(event => JSON.stringify(event))
          .join("\n")}\n`
      );
      fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), [...records, { type: "assistant.message", data: { content: "Fallback-only duplicate transcript." } }].map(event => JSON.stringify(event)).join("\n"));
      const events = collectUnifiedSession({ rootDir, engine: "copilot" }).events;
      expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  Partial answer.\n"]);
      expect(events.find(event => event.type === "vendor.partial").data).toEqual(records.at(-1).data);
      expect(events.find(event => event.type === "tool.execution_start").data.input).toBe(false);
      expect(events.some(event => event.type === "tool.execution_complete")).toBe(false);
      expect(events.find(event => event.type === "session.collection_warning").data).toEqual({ path: nativePath, code: "malformed_jsonl", line: 2 });
      expect(events.at(-1).data.warnings).toBe(1);
      expect(JSON.stringify(events)).not.toContain("PRIVATE_INVALID_RECORD");
      expect(events).toEqual(collectUnifiedSession({ rootDir, engine: "copilot" }).events);
    } finally {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  });

  it("counts persisted partial-source diagnostics in unified coverage", () => {
    const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-warning-audit-"));
    try {
      fs.writeFileSync(path.join(rootDir, "agent-session.jsonl"), JSON.stringify({ type: "session.collection_warning", data: { code: "malformed_jsonl", path: "native/events.jsonl" } }) + "\n");
      const events = collectUnifiedSession({ rootDir, engine: "copilot" }).events;
      expect(events.filter(event => event.type === "session.collection_warning")).toHaveLength(1);
      expect(events.at(-1).data.warnings).toBe(1);
    } finally {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  });

  it("does not correlate tool IDs across retries, children or unidentified calls", () => {
    const events = normalizeCopilotSession([
      { type: "session.start", data: { sessionId: "first" } },
      { type: "tool.execution_start", data: { toolCallId: "same", toolName: "root-tool" } },
      { type: "tool.execution_start", agentId: "child", data: { toolCallId: "same", toolName: "child-tool" } },
      { type: "tool.execution_start", data: { toolName: "anonymous" } },
      { type: "tool.execution_complete", data: { toolCallId: "same", success: false } },
      { type: "tool.execution_complete", agentId: "child", data: { toolCallId: "same", success: true } },
      { type: "tool.execution_complete", data: {} },
      { type: "session.start", data: { sessionId: "retry" } },
      { type: "tool.execution_complete", data: { toolCallId: "same", success: false } },
    ]);
    expect(events.filter(event => event.type === "tool.execution_complete").map(event => event.data.toolName)).toEqual(["root-tool", "child-tool", undefined, undefined]);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("scopes usage and finalized turns separately, keeping child accounting out of the root result", () => {
    const source = [
      { type: "session.start", data: { sessionId: "root" } },
      { type: "assistant.usage", id: "root-usage", data: { apiCallId: "same", inputTokens: 10, outputTokens: 0, reasoningTokens: 0 } },
      { type: "assistant.turn_end", id: "root-turn", data: { turnId: "0" } },
      { type: "assistant.usage", agentId: "child", data: { apiCallId: "same", inputTokens: 100, outputTokens: 5, reasoningTokens: 3 } },
      { type: "assistant.turn_end", agentId: "child", data: { turnId: "0" } },
      { type: "assistant.turn_end", agentId: "child", data: { turnId: "1" } },
      { type: "assistant.usage", data: { parentToolCallId: "launch", apiCallId: "same", inputTokens: 200, outputTokens: 0 } },
    ];
    const events = normalizeCopilotSession(source);
    expect(projectSessionResult(events)).toMatchObject({ num_turns: 1, usage: { input_tokens: 10, output_tokens: 0, reasoning_output_tokens: 0 } });
    expect(
      projectSessionResult(
        events.filter(event => event.agentId === "child"),
        { includeNested: true }
      )
    ).toMatchObject({ num_turns: 2, usage: { input_tokens: 100, output_tokens: 5, reasoning_output_tokens: 3 } });
    expect(sessionContext(events.find(event => event.data.parentToolCallId === "launch"))).toMatchObject({ parentToolUseId: "launch" });
    const persisted = JSON.parse(JSON.stringify(events));
    expect(normalizeCopilotSession(persisted)).toEqual(persisted);
  });

  it("excludes canonical native child results without requiring an invented engine label", () => {
    const events = [
      { type: "session.result", data: { usage: { input_tokens: 10 }, numTurns: 0 } },
      { type: "subagent.started", agentId: "child", data: { toolCallId: "launch" } },
      { type: "session.result", agentId: "child", data: { usage: { input_tokens: 100 }, numTurns: 5 } },
    ];
    expect(sessionContext(events[1]).agentId).toBeUndefined();
    expect(projectSessionResult(events)).toMatchObject({ usage: { input_tokens: 10 }, num_turns: 0 });
    expect(projectSessionResult(events, { includeNested: true })).toMatchObject({ usage: { input_tokens: 100 }, num_turns: 5 });
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("reconciles reports for the same response without counting updated snapshots twice", () => {
    const events = normalizeCopilotSession([
      { type: "assistant.usage", data: { apiCallId: "response", inputTokens: 5, outputTokens: 0 } },
      { type: "assistant.usage", data: { apiCallId: "response", inputTokens: 5, outputTokens: 3, reasoningTokens: 0 } },
      { type: "assistant.usage", data: { apiCallId: "next", inputTokens: 2, outputTokens: 1 } },
    ]);
    expect(projectSessionResult(events).usage).toEqual({ input_tokens: 7, output_tokens: 4, reasoning_output_tokens: 0 });
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("does not let child initialization replace root accounting or collapse repeated IDs across retries", () => {
    const report = { type: "assistant.usage", id: "reused", data: { apiCallId: "same", inputTokens: 5 } };
    const events = normalizeCopilotSession([
      { type: "session.start", data: { sessionId: "root" } },
      report,
      { type: "session.init", agentId: "child", data: { sessionId: "child-session", sourceEngine: "copilot" } },
      { type: "assistant.usage", id: "root-second", data: { apiCallId: "second", inputTokens: 2 } },
      { type: "session.start", data: { sessionId: "retry" } },
      report,
    ]);
    expect(events.filter(event => event.type === "session.result").map(event => [event.data.sessionId, event.data.usage.input_tokens])).toEqual([
      ["root", 5],
      ["root", 7],
      ["retry", 5],
    ]);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("does not duplicate a reasoning snapshot because an identified native delta was copied twice", () => {
    const delta = { type: "assistant.reasoning_delta", id: "copied", data: { reasoningId: "r", deltaContent: "  exact\n" } };
    const events = normalizeCopilotSession([delta, structuredClone(delta), { type: "assistant.reasoning", data: { reasoningId: "r", content: "  exact\n" } }]);
    expect(events.filter(event => event.type === "assistant.reasoning_delta")).toHaveLength(2);
    expect(events.filter(event => event.type === "assistant.reasoning")).toHaveLength(1);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("maps observed SDK content-filter finish reasons without natural-language false positives or duplicate refusals", () => {
    const events = normalizeCopilotSession([
      { type: "assistant.message", data: { apiCallId: "filtered", content: "", finishReason: "content_filter" } },
      { type: "assistant.usage", data: { apiCallId: "filtered", finishReason: "content_filter", inputTokens: 0 } },
      { type: "assistant.usage", data: { apiCallId: "unanswered", finishReason: "content_filter", outputTokens: 0 } },
      { type: "assistant.message", data: { content: "The document mentions content_filter.", finishReason: "stop" } },
    ]);
    expect(events.filter(event => event.type === "assistant.refusal").map(event => [event.data.reason, event.data.content])).toEqual([
      ["content_filter", ""],
      ["content_filter", undefined],
    ]);
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("retains reasoning fragments and scopes message snapshots to the same child and session", () => {
    const records = [
      { type: "session.start", data: { sessionId: "root" } },
      { type: "assistant.message_delta", id: "root-delta", data: { messageId: "same", deltaContent: "  root \n" } },
      { type: "assistant.message", agentId: "child", data: { messageId: "same", content: "  root \n" } },
      { type: "assistant.reasoning_delta", id: "reason-1", data: { reasoningId: "r", deltaContent: " \r\n" } },
      { type: "assistant.reasoning_delta", id: "reason-2", data: { reasoningId: "r", deltaContent: "" } },
      { type: "session.start", data: { sessionId: "retry" } },
      { type: "assistant.reasoning", data: { reasoningId: "r", content: " \r\n" } },
    ];
    const events = normalizeCopilotSession(records);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  root \n", "  root \n"]);
    expect(events.filter(event => event.type === "assistant.reasoning").map(event => event.data.content)).toEqual([" \r\n", "", " \r\n"]);
    expect(hasCopilotConversation([records[3]])).toBe(true);
    const complete = normalizeCopilotSession([
      ...normalizeCopilotSession(records.slice(0, 5)),
      { type: "assistant.message", data: { messageId: "same", content: "  root \n" } },
      { type: "assistant.reasoning", data: { reasoningId: "r", content: " \r\n" } },
    ]);
    expect(complete.filter(event => event.copilotProjection === "assistant.message_delta" || event.copilotProjection === "assistant.reasoning_delta")).toEqual([]);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(complete)))).toEqual(JSON.parse(JSON.stringify(complete)));
  });
});

describe("Copilot SDK evidence capture (synthetic)", () => {
  async function capture(events, failure) {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-audit-"));
    const stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    let onEvent = () => {};
    const session = {
      sessionId: "sdk-audit",
      on: handler => {
        onEvent = handler;
      },
      sendAndWait: async () => {
        for (const event of events) onEvent(event);
        if (failure) throw failure;
        return null;
      },
      disconnect: async () => {},
    };
    class Client {
      start = async () => {};
      createSession = async () => session;
      stop = async () => {};
    }
    try {
      const result = await runWithCopilotSDK({
        sdkUri: "http://127.0.0.1:3002",
        prompt: "synthetic prompt",
        logger: () => {},
        sessionStateBaseDir: directory,
        sdkModule: { CopilotClient: Client, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
      });
      const saved = fs
        .readFileSync(path.join(directory, "sdk-audit", "events.jsonl"), "utf8")
        .trim()
        .split("\n")
        .map(JSON.parse);
      return { result, saved };
    } finally {
      stderr.mockRestore();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  }

  it("retains native envelopes, refusals, reasoning, errors and genuine unknown extensions without inventing metrics", async () => {
    const events = [
      ...native,
      { type: "assistant.message", id: "refused", parentId: null, data: { content: "", refusal: "  Cannot comply.\n", apiCallId: "refusal-call", reasoningText: " Policy check.\n" }, nativeFlag: false },
      { type: "assistant.reasoning_delta", ephemeral: true, data: { reasoningId: "partial", deltaContent: " unfinished\n" } },
      { type: "assistant.usage", ephemeral: true, data: { apiCallId: "live", inputTokens: 0, outputTokens: 0, reasoningTokens: 0 } },
      { type: "session.error", data: { errorType: "provider", message: "  Observed failure.\n", statusCode: 0 } },
      { type: "model.call_failure", ephemeral: true, data: { apiCallId: "failed", failureKind: "http_error", statusCode: 503, errorMessage: "  Provider failed.\n" } },
      { type: "vendor.audit", nativeFlag: false, data: { zero: 0, false: false, empty: "", null: null, array: [] } },
      { type: "ui.transient", ephemeral: true, data: { private: "not captured" } },
    ];
    const original = structuredClone(events);
    const { saved } = await capture(events);
    expect(saved.map(event => event.type)).toEqual(events.slice(0, -1).map(event => event.type));
    expect(saved[1]).toEqual(native[1]);
    expect(saved.find(event => event.id === "audit-message")).toEqual(native[2]);
    expect(saved.find(event => event.id === "audit-tool-start").data.command).toBe("  echo sanitized\n");
    expect(saved.find(event => event.id === "audit-tool-end").data.error).toEqual(native[4].data.error);
    expect(saved.find(event => event.type === "vendor.audit")).toEqual(events.at(-2));
    expect(
      Object.hasOwn(
        saved.find(event => event.type === "assistant.usage"),
        "timestamp"
      )
    ).toBe(false);
    const normalized = parseCopilotLog(saved.map(event => JSON.stringify(event)).join("\n")).logEntries;
    expect(normalized.find(event => event.id === "refused" && event.type === "assistant.refusal").data).toMatchObject({ reason: "refusal", content: "  Cannot comply.\n", apiCallId: "refusal-call" });
    expect(normalized.find(event => event.type === "assistant.reasoning" && event.id === "refused").data.content).toBe(" Policy check.\n");
    expect(projectSessionResult(normalized).errors).toEqual([events.at(-4).data, events.at(-3).data]);
    expect(normalized.some(event => event.type.startsWith("copilot."))).toBe(false);
    expect(events).toEqual(original);
  });

  it("preserves falsy input, native failures and absent orphan outcomes with scoped tool enrichment", async () => {
    const { saved } = await capture([
      { type: "tool.execution_start", data: { toolCallId: "", toolName: "root", input: false, command: "" } },
      { type: "tool.execution_start", agentId: "child", data: { toolCallId: "", toolName: "child", input: null } },
      { type: "tool.execution_complete", data: { toolCallId: "", success: true, shellExecution: { exitCode: 2 }, error: "", result: null } },
      { type: "tool.execution_complete", agentId: "child", data: { toolCallId: "", success: false, output: 0 } },
      { type: "tool.execution_complete", data: { toolCallId: "orphan" } },
    ]);
    expect(saved[0].data).toEqual({ toolCallId: "", toolName: "root", input: false, command: "" });
    expect(saved[2].data).toMatchObject({ toolName: "root", success: true, error: "", result: null });
    expect(saved[3].data).toMatchObject({ toolName: "child", success: false, output: 0 });
    expect(saved[4].data).toEqual({ toolCallId: "orphan" });
    expect(sessionToolSuccess(normalizeCopilotSession(saved)[2].data)).toBe(false);
  });

  it("persists an observed SDK send failure as a session error without completing a dangling call", async () => {
    const { result, saved } = await capture([{ type: "tool.execution_start", data: { toolCallId: "pending", toolName: "bash" } }], Object.assign(new Error("SDK send failed"), { code: 0 }));
    expect(result.exitCode).toBe(1);
    expect(saved.at(-1)).toMatchObject({ type: "session.error", data: { errorType: "sdk_driver", message: "SDK send failed", code: 0 } });
    const normalized = normalizeCopilotSession(saved);
    expect(projectSessionResult(normalized).errors).toEqual([saved.at(-1).data]);
    expect(normalized.some(event => event.type === "tool.execution_complete")).toBe(false);
  });
});
