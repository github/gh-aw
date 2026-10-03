import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, mergeSessionSources, normalizeRuntimeEvent, parseEngineSession, sessionTimestamp, writeUnifiedSession } from "./unified_session.cjs";
import { serializeSessionArtifact, writeSessionArtifact } from "./session_artifact.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";

const req = require;
const { writeDetectionUsageResult } = req("./generate_usage_activity_summary.cjs");
const { success: piSuccess } = req("./fixtures/pi_ci_stream.cjs");
const claudeFixtures = req("./fixtures/claude_ci_sessions.cjs");
const codexNoTools = fs.readFileSync(new URL("./test_data/codex_ci_no_tools.jsonl", import.meta.url), "utf8");

describe("Unified conclusion session", () => {
  let root;
  let originalEnv;
  let originalCore;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-unified-session-"));
    originalEnv = { ...process.env };
    originalCore = global.core;
    global.core = { info: vi.fn(), warning: vi.fn() };
  });
  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(file, entries) {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof entries === "string" ? entries : file.endsWith(".json") ? JSON.stringify(entries) : entries.map(entry => JSON.stringify(entry)).join("\n") + "\n");
    return target;
  }

  it("reconstructs OpenCode sessions from stdio with native timestamps and tool correlation", () => {
    write("aw_info.json", { engine_id: "opencode", agent_version: "fixture" });
    write("agent-stdio.log", [
      { type: "text", sessionID: "ses_fixture", timestamp: 1790899201000, part: { id: "text", text: "Done." } },
      {
        type: "tool_use",
        sessionID: "ses_fixture",
        timestamp: 1790899202000,
        part: { id: "tool", callID: "call", tool: "bash", state: { status: "completed", input: { command: "pwd" }, output: "/workspace", time: { start: 1790899200500, end: 1790899202000 } } },
      },
      { type: "step_finish", sessionID: "ses_fixture", timestamp: 1790899203000, part: { id: "finish", cost: 0, tokens: { input: 10, output: 5, cache: { read: 20, write: 0 } } } },
    ]);
    const events = writeUnifiedSession({
      rootDir: root,
      warn: message => {
        throw new Error(message);
      },
    });
    const start = events.find(event => event.type === "tool.execution_start");
    expect(start).toMatchObject({ provenance: { path: "agent-stdio.log", timestampMs: 1790899202000 }, data: { toolCallId: "call", toolName: "bash", input: { command: "pwd" } } });
    expect(events.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolCallId: "call", success: true, durationMs: 1500 });
    expect(events.find(event => event.type === "session.result").data).toMatchObject({ totalCostUsd: 0, numTurns: 1, usage: { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: 20 } });
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("Done.");
  });

  it("merges every required component with essential payloads and timestamp ordering", () => {
    const message = { type: "assistant.message", id: "same-id", timestamp: "2026-10-02T00:00:03Z", data: { content: "Done.\n", extra: [0, false, null] }, nativeField: "preserved" };
    const rpc = { timestamp: "2026-10-02T00:00:02+00:00", event: "rpc_response", payload: { jsonrpc: "2.0", id: 0, result: { content: [{ text: "response" }] } }, server_id: "github", direction: "IN" };
    const audit = { ts: 1790899201.25, host: "example.com", decision: "TCP_DENIED", status: 403, extra: false };
    write("agent-session.jsonl", [message]);
    write("mcp-logs/rpc-messages.jsonl", [rpc]);
    write("sandbox/firewall/audit/audit.jsonl", [audit]);
    write("sandbox/firewall/logs/api-proxy-logs/events.jsonl", [{ timestamp: "2026-10-02T00:00:03.5Z", eventName: "token_steering", message: "budget warning" }]);
    write("sandbox/firewall/logs/api-proxy-logs/token-tracker-audit.jsonl", [{ ts: 1790899203600, event: "TRACK_END", rid: "request" }]);
    write("threat-detection/sandbox/firewall/logs/audit.jsonl", [{ timestamp: "2026-10-02T00:00:04Z", event: "http_access", status: 200 }]);
    write("safeoutputs.jsonl", [{ type: "create_issue", title: "Requested" }]);
    write("safe-output-items.jsonl", [{ type: "create_issue", timestamp: "2026-10-02T00:00:05Z", url: "https://github.com/example/repo/issues/1" }]);
    write("safe-output-errors.json", { errors: [{ type: "add_comment", error: "rejected" }] });
    write("experiments/assignments.json", { experiment: "variant-b" });
    write("experiments/state.jsonl", [{ run_id: "1", timestamp: "2026-10-02T00:00:00Z", assignments: { experiment: "variant-b" } }]);
    write("agent/graders/grader_manifest.json", { graders: [{ id: "quality" }] });
    write("agent/graders/grader_results.json", { results: [{ id: "quality", score: 0, passed: false }] });
    write("evals/evals.jsonl", [{ id: "quality", timestamp: "2026-10-02T00:00:06Z", answer: "no" }]);
    write("usage/agent/execution.json", { outcome: "failure", duration_ms: 0 });
    write("usage/detection/detection_result.json", { conclusion: "failure", secret_leak: false });
    const events = writeUnifiedSession({ rootDir: root });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(persisted).toEqual(events);
    expect(persisted[0]).toEqual({
      type: "session.format",
      data: { version: 1 },
      provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 },
    });
    expect(persisted.filter(event => event.type === "session.format")).toHaveLength(1);
    expect(new Set(events.map(event => event.provenance.component))).toEqual(new Set(["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval", "execution", "detection", "collector"]));
    expect(events.find(event => event.type === "assistant.message")).toMatchObject(normalizeUnifiedSessionEvent(message));
    expect(events.find(event => event.type === "assistant.message")).not.toHaveProperty("nativeField");
    expect(events.find(event => event.type === "mcp.rpc.response").data).toEqual({ serverName: "github", direction: "IN", rpcId: 0 });
    expect(events.find(event => event.type === "firewall.http_access").data).toEqual({ host: "example.com", decision: "TCP_DENIED", status: 403 });
    expect(events.find(event => event.type === "firewall.http_access").provenance.timestampMs).toBe(1790899201250);
    expect(events.find(event => event.data.requestId === "request").provenance.timestampMs).toBe(1790899203600);
    expect(events.filter(event => event.provenance.phase === "detection")).toHaveLength(2);
    expect(events.find(event => event.type === "grader.result").data.results[0]).toEqual({ id: "quality", score: 0, passed: false });
    const times = events.slice(1).map(event => event.provenance.timestampMs ?? Infinity);
    expect(times).toEqual([...times].sort((a, b) => (a === b ? 0 : a < b ? -1 : 1)));
    expect(events.at(-1).type).toBe("session.collection");
    expect(events.at(-1).data).toMatchObject({ warnings: 0, absentComponents: [] });
    const original = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toBe(original);
  });

  it("collects available tool and engine versions from workflow metadata", () => {
    write("aw_info.json", {
      cli_version: "v1.2.3",
      awf_version: "v0.4.5",
      awmg_version: "v0.6.7",
      engine_id: "copilot",
      agent_version: "v2.3.4",
      model: "fixture",
      secret: "omit",
    });
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.find(event => event.type === "workflow.info")).toMatchObject({
      data: {
        cliVersion: "v1.2.3",
        awfVersion: "v0.4.5",
        mcpgVersion: "v0.6.7",
        engineId: "copilot",
        agentVersion: "v2.3.4",
        engine: "copilot",
        model: "fixture",
      },
      provenance: { path: "aw_info.json", component: "workflow" },
    });
    expect(events.find(event => event.type === "workflow.info").data).not.toHaveProperty("secret");
  });

  it("retains native provenance, equal-time source order, and invalid/absent timestamps without mutation", () => {
    const sources = [
      {
        component: "agent",
        phase: "agent",
        path: "session",
        events: [
          { type: "vendor.notice", timestamp: "invalid", data: { flag: false }, provenance: { vendor: 1 } },
          { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", id: "duplicate", data: { content: "" } },
          { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", id: "duplicate", data: { content: "second" } },
        ],
      },
      { component: "mcp", phase: "agent", path: "rpc", events: [{ type: "mcp.event", timestamp: "2026-10-02T00:00:01Z", data: {} }] },
    ];
    const original = structuredClone(sources);
    const events = mergeSessionSources(sources);
    expect(events.map(event => event.data.content)).toEqual(["", "second", undefined, undefined]);
    expect(events.at(-1).provenance).toMatchObject({ native: { vendor: 1 }, index: 0 });
    expect(events.at(-1).timestamp).toBe("invalid");
    expect(events.at(-1).provenance).not.toHaveProperty("timestampMs");
    expect(sources).toEqual(original);
    expect(mergeSessionSources(sources)).toEqual(events);
  });

  it("normalizes pretty-printed source JSON to compact JSONL without losing payload whitespace", () => {
    const native = [
      { type: "assistant.message", data: { content: "  first line\nsecond line\t\n", structured: { empty: "", zero: 0, flag: false } } },
      { type: "vendor.extension", data: { nested: [{ key: null }] } },
    ];
    const prettyPrinted = JSON.stringify(native, null, 2);
    write("aw_info.json", { engine_id: "custom" });
    write("agent-stdio.log", prettyPrinted);
    writeUnifiedSession({ rootDir: root });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content.endsWith("\n")).toBe(true);
    expect(content).not.toContain("\n\n");
    const lines = content.trimEnd().split("\n");
    for (const line of lines) expect(line).toBe(JSON.stringify(JSON.parse(line)));
    const agent = lines.map(JSON.parse).filter(event => event.provenance.component === "agent");
    expect(agent.map(({ provenance, ...event }) => event)).toEqual(native.map(normalizeUnifiedSessionEvent));
    expect(lines.some(line => line.includes("\\nsecond line\\t\\n"))).toBe(true);
  });

  it("uses source-schema timestamp units and nested Pi message times without fabricating time", () => {
    expect(sessionTimestamp({ ts: 1.25 }, "seconds")).toBe(1250);
    expect(sessionTimestamp({ ts: 1250 })).toBe(1250);
    expect(sessionTimestamp({ timestamp: 0 })).toBe(0);
    expect(sessionTimestamp({ message: { timestamp: 1250 } })).toBe(1250);
    for (const value of ["", "123", "2026-10-02", NaN, Infinity, 8640000000000001]) expect(sessionTimestamp({ timestamp: value })).toBeUndefined();
  });

  it("prefers persisted canonical events over raw logs and avoids replicated firewall accounting", () => {
    write("agent-session.jsonl", [{ type: "vendor.extension", data: { preserved: true } }]);
    write("sandbox/agent/logs/copilot-session-state/uuid/events.jsonl", [{ type: "user.message", data: { content: "duplicate" } }]);
    write("agent-stdio.log", "not a session");
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "");
    write("sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", [{ timestamp: "2026-10-02T00:00:01Z", event: "token_usage", input_tokens: 999 }]);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(1);
    expect(events.some(event => event.data.input_tokens === 999)).toBe(false);
    expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", events: 0 }));
  });

  it.each([false, true])("retains current Codex accounting through conclusion collection (canonical present: %j)", canonicalPresent => {
    write("agent-stdio.log", codexNoTools);
    if (canonicalPresent) write("agent-session.jsonl", parseEngineSession(codexNoTools, "codex"));
    writeUnifiedSession({ rootDir: root, engine: "codex" });
    const published = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    const agent = published.filter(event => event.provenance.component === "agent");
    expect(agent.map(event => event.type)).toEqual(["session.init", "session.result", "turn.started", "assistant.message", "assistant.message", "session.result"]);
    expect(agent[1].data).toEqual({ errors: ["Model metadata unavailable; using fallback metadata."] });
    expect(agent.at(-1).data).toEqual({
      status: "completed",
      sourceType: "turn.completed",
      numTurns: 1,
      usage: { input_tokens: 16847, output_tokens: 167, cache_read_input_tokens: 8576, cache_creation_input_tokens: 0, reasoning_output_tokens: 0 },
    });
    expect(agent.at(-1)).not.toHaveProperty("usage");
    expect(agent.every(event => event.provenance.path === (canonicalPresent ? "agent-session.jsonl" : "agent-stdio.log"))).toBe(true);
    expect(published.some(event => event.type === "session.collection_warning")).toBe(false);
  });

  it.each(["", "{bad\n", '{"type":"result","usage":{}}\n'])("falls back from an unusable canonical session (%j) to native events", content => {
    const nativePath = "sandbox/agent/logs/copilot-session-state/uuid/events.jsonl";
    write("agent-session.jsonl", content);
    write(nativePath, [{ type: "assistant.message", data: { content: "recovered" } }]);
    write("agent-stdio.log", "not a session");
    const { events } = collectUnifiedSession({ rootDir: root, warn: vi.fn() });
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([expect.objectContaining({ type: "assistant.message", data: { content: "recovered" }, provenance: expect.objectContaining({ path: nativePath }) })]);
    expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: "agent-session.jsonl", events: 0 }));
  });

  it.each([false, true])("falls back to raw logs when canonical and native events are unusable (native present: %j)", nativePresent => {
    write("agent-session.jsonl", "");
    if (nativePresent) write("sandbox/agent/logs/copilot-session-state/uuid/events.jsonl", "{bad\n");
    write("agent-stdio.log", JSON.stringify([{ type: "assistant.message", data: { content: "raw fallback" } }]));
    const { events } = collectUnifiedSession({ rootDir: root, engine: "custom", warn: vi.fn() });
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([expect.objectContaining({ data: { content: "raw fallback" }, provenance: expect.objectContaining({ path: "agent-stdio.log" }) })]);
  });

  it.each([
    ["experiments/state.jsonl", "usage/experiment/state.jsonl"],
    ["experiments/state.json", "usage/experiment/state.json"],
    ["experiments/assignments.json", "usage/experiment/assignments.json"],
    ["agent/graders/grader_manifest.json", "usage/graders/grader_manifest.json"],
    ["agent/graders/grader_results.json", "usage/graders/grader_results.json"],
    ["evals/evals.jsonl", "usage/evals.jsonl"],
    ["agent_usage.jsonl", "usage/agent_usage.jsonl"],
    ["threat-detection/detection_usage.jsonl", "usage/detection_usage.jsonl"],
    ["detection_usage.jsonl", "usage/detection_usage.jsonl"],
    ["evals/evals_token_usage.jsonl", "usage/evals/token_usage.jsonl"],
    ["agent_execution.json", "usage/agent/execution.json"],
    ["threat-detection/execution.json", "usage/detection/execution.json"],
    ["evals/evals/execution.json", "usage/evals/execution.json"],
    ["evals/execution.json", "usage/evals/execution.json"],
  ])("prefers original provenance from %s and retains the %s mirror fallback", (original, mirror) => {
    const observation = { value: "source", id: "source", type: "source", status: "source", model: "source", assignments: { experiment: "source" }, outcome: "source", conclusion: "source", run_id: "source" };
    const record = file => (file.endsWith(".json") ? observation : [observation]);
    const source = write(original, record(original));
    write(mirror, record(mirror));
    const { events } = collectUnifiedSession({ rootDir: root });
    const observed = events.filter(event => event.provenance.path === original || event.provenance.path === mirror);
    expect(observed).toHaveLength(1);
    expect(observed[0].provenance.path).toBe(original);
    fs.unlinkSync(source);
    const fallback = collectUnifiedSession({ rootDir: root }).events.filter(event => event.provenance.path === original || event.provenance.path === mirror);
    expect(fallback).toHaveLength(1);
    expect(fallback[0].provenance.path).toBe(mirror);
  });

  it.each(["structured", "inline"])("collects the sanitized %s detection verdict with its job outcome and categorical reason", source => {
    const verdict = { prompt_injection: true, secret_leak: false, malicious_patch: true, reasons: ["PRIVATE_DETECTOR_REASON"] };
    if (source === "structured") write("threat-detection/detection_result.json", verdict);
    else write("threat-detection/detection.log", `PRIVATE_DETECTOR_TRANSCRIPT\nTHREAT_DETECTION_RESULT:${JSON.stringify(verdict)}\n`);
    process.env.GH_AW_DETECTION_JOB_RESULT = "success";
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";
    process.env.GH_AW_DETECTION_REASON = "threat_detected";
    writeDetectionUsageResult(path.join(root, "threat-detection"), path.join(root, "usage/detection/detection_result.json"));
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "detection.result")).toEqual([
      {
        type: "detection.result",
        data: { jobResult: "success", conclusion: "warning", reason: "threat_detected", promptInjection: true, secretLeak: false, maliciousPatch: true },
        provenance: { component: "detection", phase: "detection", path: "usage/detection/detection_result.json", index: 0 },
      },
    ]);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("PRIVATE_DETECTOR_");
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
  });

  it.each([
    ["success", "success", "", { prompt_injection: false, secret_leak: false, malicious_patch: false }],
    ["failure", "failure", "agent_failure", undefined],
    ["success", "warning", "parse_error", undefined],
    ["cancelled", "", "", undefined],
    ["skipped", "skipped", "detection_skipped", undefined],
  ])("preserves detection status %s/%s without inventing absent verdict flags", (job, conclusion, reason, verdict) => {
    process.env.GH_AW_DETECTION_JOB_RESULT = job;
    process.env.GH_AW_DETECTION_CONCLUSION = conclusion;
    process.env.GH_AW_DETECTION_REASON = reason;
    if (verdict) write("threat-detection/detection_result.json", verdict);
    if (job === "skipped") write("threat-detection/detection_result.json", { prompt_injection: true, secret_leak: true, malicious_patch: true });
    writeDetectionUsageResult(path.join(root, "threat-detection"), path.join(root, "usage/detection/detection_result.json"));
    const result = collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "detection.result");
    expect(result).toHaveLength(1);
    expect(result[0].data).toEqual({ jobResult: job, conclusion, reason, ...(verdict ? { promptInjection: false, secretLeak: false, maliciousPatch: false } : {}) });
  });

  it("falls back to the raw structured verdict when no conclusion result is available", () => {
    write("threat-detection/detection_result.json", { prompt_injection: false, secret_leak: true, malicious_patch: false, reason: "PRIVATE_RAW_REASON", reasons: ["PRIVATE_DETECTOR_REASON"] });
    const results = collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "detection.result");
    expect(results).toEqual([
      {
        type: "detection.result",
        data: { promptInjection: false, secretLeak: true, maliciousPatch: false },
        provenance: { component: "detection", phase: "detection", path: "threat-detection/detection_result.json", index: 0 },
      },
    ]);
    expect(JSON.stringify(results)).not.toContain("PRIVATE_RAW_REASON");
  });

  it("recovers adjacent valid records, records malformed/partial coverage and skips symlinks", () => {
    const warn = vi.fn();
    write("agent-session.jsonl", '{"type":"assistant.message","data":{"content":"first"}}\n{bad\nnull\n{"type":"vendor.after","data":{}}\n');
    write("safe-output-errors.json", "{truncated");
    const secret = write("outside.jsonl", [{ type: "assistant.message", data: { content: "must not follow" } }]);
    fs.mkdirSync(path.join(root, "mcp-logs"));
    fs.symlinkSync(secret, path.join(root, "mcp-logs", "unsafe.jsonl"));
    fs.symlinkSync(path.join(root, "mcp-logs"), path.join(root, "experiments"));
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(2);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["malformed_jsonl", "non_object_record", "symlink_not_read", "malformed_json"]);
    expect(warn).toHaveBeenCalled();
    expect(JSON.stringify(events)).not.toContain("must not follow");
  });

  it("reports absent components for an empty run without a fake agent result", () => {
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
    expect(events[0].provenance).not.toHaveProperty("timestampMs");
    expect(events[1]).toMatchObject({ type: "session.collection", data: { absentComponents: ["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval"], warnings: 0, untimedEvents: 0 } });
  });

  it("pins only the collector file-format header, preserving a native session.format extension", () => {
    write("agent-session.jsonl", [
      { type: "session.format", timestamp: "2026-10-02T00:00:02Z", data: { version: "native-engine-format" } },
      { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", data: { content: "first timed event" } },
    ]);
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.map(event => event.type)).toEqual(["session.format", "assistant.message", "session.format", "session.collection"]);
    expect(events[0].data.version).toBe(1);
    expect(events[2]).toMatchObject({ data: { version: "native-engine-format" }, provenance: { component: "agent", index: 0 } });
  });

  it("fails explicitly on read errors and removes a stale output rather than uploading it", () => {
    const source = write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "current" } }]);
    const output = write("usage/aw_session.jsonl", "old session");
    const originalRead = fs.readFileSync;
    vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
      if (file === source) throw new Error("fixture read denied");
      return originalRead(file, ...args);
    });
    expect(() => writeUnifiedSession({ rootDir: root })).toThrow("fixture read denied");
    expect(fs.existsSync(output)).toBe(false);
  });

  it("retains unfamiliar MCP/firewall messages and does not confuse prototypes with event kinds", () => {
    for (const component of ["mcp", "firewall"]) {
      const raw = { timestamp: "invalid", event: "__proto__", future: [false, null, 0] };
      const event = normalizeRuntimeEvent(component, raw);
      expect(event).toMatchObject({ type: `${component}.event`, data: raw, timestamp: "invalid" });
      expect(raw.future).toEqual([false, null, 0]);
    }
  });

  it("redacts decoded secrets and mask values in all payloads, including IDs, before persistence", () => {
    const secret = 'secret"with\\escaped\ncharacters';
    const token = "ghp_" + "a".repeat(36);
    process.env.GH_AW_SECRET_NAMES = "SESSION_SECRET";
    process.env.SECRET_SESSION_SECRET = secret;
    const events = [{ type: "tool.execution_complete", id: secret, data: { toolCallId: token, output: { nested: secret, mask: "opaque-mask" }, success: false } }];
    const original = structuredClone(events);
    const content = serializeSessionArtifact(events, ["opaque-mask"]);
    expect(content).not.toContain(token);
    const event = JSON.parse(content);
    expect(event.id).toBe("***REDACTED***");
    expect(event.data.toolCallId).toBe("***REDACTED***");
    expect(event.data.output).toEqual({ nested: "***REDACTED***", mask: "***" });
    expect(events).toEqual(original);
    write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "opaque-mask" } }]);
    write("agent-stdio.log", "::add-mask::opaque-mask\n");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("opaque-mask");
  });

  it("uses a standardized error code for invalid session data", () => {
    expect(() => serializeSessionArtifact({})).toThrow("ERR_VALIDATION: Expected a session event array");
  });

  it("writes atomically and cleans up failed outputs without following a temporary symlink", () => {
    const output = write("usage/aw_session.jsonl", "old");
    const target = write("outside.jsonl", "unchanged");
    fs.symlinkSync(target, `${output}.tmp`);
    expect(() => writeSessionArtifact(output, [{ type: "vendor.event", data: {} }])).toThrow("ERR_SYSTEM: Failed to write session artifact");
    expect(fs.readFileSync(target, "utf8")).toBe("unchanged");
    expect(fs.existsSync(output)).toBe(false);
    expect(fs.existsSync(`${output}.tmp`)).toBe(false);
  });

  it.each([
    ["claude", () => JSON.stringify(claudeFixtures.success)],
    [
      "copilot",
      () =>
        JSON.stringify([
          { type: "session.start", timestamp: "2026-10-02T00:00:01Z", data: { sessionId: "copilot" } },
          { type: "assistant.message", data: { content: "done" } },
        ]),
    ],
    ["codex", () => fs.readFileSync(path.join(import.meta.dirname, "test_data/codex_ci_smoke.jsonl"), "utf8")],
    [
      "gemini",
      () =>
        JSON.stringify([
          { type: "init", model: "gemini-test" },
          { type: "assistant", message: "done" },
          { type: "result", stats: { input_tokens: 10, output_tokens: 2 } },
        ]),
    ],
    ["pi", () => piSuccess.map(entry => JSON.stringify(entry)).join("\n")],
    [
      "custom",
      () =>
        JSON.stringify([
          { type: "assistant.message", data: { content: "custom done" } },
          { type: "vendor.custom", data: { score: 0 } },
        ]),
    ],
  ])("normalizes and persists a %s workflow session using its existing engine adapter", (engine, fixture) => {
    const content = fixture();
    const expected = parseEngineSession(content, engine);
    expect(expected.length).toBeGreaterThan(0);
    write("aw_info.json", { engine_id: engine });
    write(engine === "pi" ? "pi-streaming.jsonl" : "agent-stdio.log", content);
    write("mcp-logs/gateway.jsonl", [{ timestamp: "2026-10-02T00:00:02Z", event: "tool_call", tool_name: "lookup" }]);
    const events = writeUnifiedSession({ rootDir: root });
    const actual = events
      .filter(event => event.provenance.component === "agent")
      .sort((a, b) => a.provenance.index - b.provenance.index)
      .map(({ provenance, ...event }) => event);
    expect(actual).toEqual(JSON.parse(JSON.stringify(expected.map(normalizeUnifiedSessionEvent))));
    expect(events.some(event => event.type === "mcp.tool_call")).toBe(true);
    expect(events.every(event => event.type.includes(".") && event.data && typeof event.data === "object")).toBe(true);
  });
});
