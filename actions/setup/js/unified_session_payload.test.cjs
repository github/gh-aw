import { describe, expect, it } from "vitest";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";
import { normalizeCodexSession } from "./codex_session.cjs";
import { selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";

describe("essential unified session payloads", () => {
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
    expect(compact).toEqual({ type: "assistant.message", id: "message", parentId: null, timestamp: message.timestamp, data: { content } });
    expect(Buffer.byteLength(JSON.stringify(compact))).toBeLessThan(Buffer.byteLength(JSON.stringify(message)) / 2);
    expect(message).toEqual(original);
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it.each([
    ["tool.execution_start", { toolCallId: "call", parameters: false, input: 0, command: "", text: "duplicate" }, { toolCallId: "call", input: 0, command: "" }],
    ["tool.execution_complete", { result: false, output: null, is_error: true, duration_ms: 0, exit_code: 1, metadata: "duplicate" }, { output: null, isError: true, durationMs: 0, exitCode: 1 }],
    ["session.init", { sourceEngine: "copilot", model: "fixture", session_id: "session", tools: Array(100).fill("large descriptor") }, { sourceEngine: "copilot", model: "fixture", sessionId: "session" }],
    ["session.runtime", { engine: "copilot", engineVersion: "1.0.90", sandboxRuntime: "cloud-hypervisor", extra: "omit" }, { engine: "copilot", engineVersion: "1.0.90", sandboxRuntime: "cloud-hypervisor" }],
    ["session.sandbox", { runtime: "none", firewallEnabled: false, firewallType: "", allowedDomains: [], extra: "omit" }, { runtime: "none", firewallEnabled: false, firewallType: "", allowedDomains: [] }],
    [
      "session.sandbox",
      { runtime: "docker", firewallEnabled: true, firewallType: "squid", firewallVersion: "v0.30.1", mcpGatewayVersion: "v1.0.0", allowedDomains: ["example.com"], extra: "omit" },
      { runtime: "docker", firewallEnabled: true, firewallType: "squid", firewallVersion: "v0.30.1", mcpGatewayVersion: "v1.0.0", allowedDomains: ["example.com"] },
    ],
    ["firewall.http_access", { domain: "example.com", http_status: 0, squid_request_status: "DENIED", credentials: "omit" }, { host: "example.com", status: 0, decision: "DENIED" }],
    ["firewall.steering", { eventName: "token_steering", message: "budget warning", reason: null, body: "omit" }, { event: "token_steering", message: "budget warning", reason: null }],
    ["mcp.tool_call", { tool_call_id: "call", tool_name: "lookup", duration: 5, input_size: 0, output_size: 10, status: "error" }, { toolCallId: "call", toolName: "lookup", durationMs: 5, inputSize: 0, outputSize: 10, status: "error" }],
    ["execution.result", { exit_code: 0, duration_ms: 0, outcome: "success", extra: "omit" }, { exitCode: 0, durationMs: 0, outcome: "success" }],
    ["detection.result", { job_result: "success", secret_leak: false, prompt_injection: false, raw: "omit" }, { jobResult: "success", secretLeak: false, promptInjection: false }],
    ["safe_output.request", { type: "create_issue", repo: "example/repo", body: "omit", title: "omit" }, { type: "create_issue", repo: "example/repo" }],
    ["safe_output.result", { type: "create_issue", number: 0, identifier: null, body: "omit" }, { type: "create_issue", number: 0, identifier: null }],
    ["eval.result", { id: "quality", answer: false, question: "omit", model: "fixture" }, { id: "quality", answer: false, model: "fixture" }],
    ["experiment.assignment", { experiment: "B" }, { assignments: { experiment: "B" } }],
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

  it("uses one accounting shape without losing zero values or adding overlapping totals", () => {
    const data = { provider: "copilot", inputTokens: 0, output_tokens: 2, cache_read_tokens: 0, cache_write_tokens: 0, ai_credits_this_response: 0, ai_credits_total: 0, duration_ms: 0, opaque: "omit" };
    const runtime = normalizeUnifiedSessionEvent({ type: "firewall.token_usage", data });
    expect(runtime.data).toEqual({ provider: "copilot", aic: 0, totalAic: 0, durationMs: 0, usage: { input_tokens: 0, output_tokens: 2, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 } });
    const agent = normalizeUnifiedSessionEvent({ type: "session.result", data: { num_turns: 0, usage: { inputTokens: 0, output_tokens: 2, overflowed_tokens: ["cache_read_input_tokens"], unused: 10 } } });
    expect(agent.data).toEqual({ numTurns: 0, usage: { input_tokens: 0, output_tokens: 2, overflowed_tokens: ["cache_read_input_tokens"] } });
    expect(normalizeUnifiedSessionEvent(runtime)).toEqual(runtime);
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
          usage: { input_tokens: 10, output_tokens: 5, reasoning_output_tokens: reasoning, cache_read_input_tokens: 4, cache_creation_input_tokens: 0 },
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
      errors: [{ type: "add_comment", error: "rejected" }],
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

  it("retains raw aw-info metadata as an opaque payload instead of the compact workflow projection", () => {
    const event = { type: "workflow.aw_info", data: { engine_id: "copilot", version: "1.0.90", agent_runtime: "docker", context: { run_id: 0 }, future: [false, null, ""] } };
    const original = structuredClone(event);
    expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
    expect(normalizeUnifiedSessionEvent(normalizeUnifiedSessionEvent(event))).toEqual(event);
    expect(event).toEqual(original);
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
    expect(normalizeUnifiedSessionEvent({ type: "safe_output.error", data: { errors: ["rejected", null, false] } }).data).toEqual({ errors: ["rejected", null, false] });
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

  it("normalizes actual safe-output failure reports without dropping operation error codes", () => {
    const compact = normalizeUnifiedSessionEvent({
      type: "safe_output.error",
      data: { status: "failure", errorCode: "ERR_SAFE_OUTPUT", failures: [{ type: "add_comment", errorCode: "ERR_TARGET", error: "target unavailable" }] },
    });
    expect(compact.data).toEqual({ status: "failure", errorCode: "ERR_SAFE_OUTPUT", errors: [{ type: "add_comment", errorCode: "ERR_TARGET", error: "target unavailable" }] });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    const output = generatePlainTextSummary([compact]);
    expect(output).toContain("ERR_SAFE_OUTPUT");
    expect(output).toContain("ERR_TARGET");
    expect(output).toContain("type=add_comment");
  });
});
