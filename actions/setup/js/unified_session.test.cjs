import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, mergeSessionSources, normalizeRuntimeEvent, parseEngineSession, sessionTimestamp, writeUnifiedSession } from "./unified_session.cjs";
import { serializeSessionArtifact, writeSessionArtifact } from "./session_artifact.cjs";

const req = require;
const { success: piSuccess } = req("./fixtures/pi_ci_stream.cjs");
const claudeFixtures = req("./fixtures/claude_ci_sessions.cjs");

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

  it("merges every required component with complete payloads and timestamp ordering", () => {
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
    const persisted = fs.readFileSync(path.join(root, "usage/session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(persisted).toEqual(events);
    expect(new Set(events.map(event => event.provenance.component))).toEqual(new Set(["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval", "execution", "detection", "collector"]));
    expect(events.find(event => event.type === "assistant.message")).toMatchObject(message);
    expect(events.find(event => event.type === "mcp.rpc.response").data).toEqual(rpc);
    expect(events.find(event => event.type === "firewall.http_access").data).toEqual(audit);
    expect(events.find(event => event.type === "firewall.http_access").provenance.timestampMs).toBe(1790899201250);
    expect(events.find(event => event.data.rid === "request").provenance.timestampMs).toBe(1790899203600);
    expect(events.filter(event => event.provenance.phase === "detection")).toHaveLength(2);
    expect(events.find(event => event.type === "grader.result").data.results[0]).toEqual({ id: "quality", score: 0, passed: false });
    const times = events.map(event => event.provenance.timestampMs ?? Infinity);
    expect(times).toEqual([...times].sort((a, b) => (a === b ? 0 : a < b ? -1 : 1)));
    expect(events.at(-1).type).toBe("session.collection");
    expect(events.at(-1).data).toMatchObject({ warnings: 0, absentComponents: [] });
    const original = fs.readFileSync(path.join(root, "usage/session.jsonl"), "utf8");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/session.jsonl"), "utf8")).toBe(original);
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
    const content = fs.readFileSync(path.join(root, "usage/session.jsonl"), "utf8");
    expect(content.endsWith("\n")).toBe(true);
    expect(content).not.toContain("\n\n");
    const lines = content.trimEnd().split("\n");
    for (const line of lines) expect(line).toBe(JSON.stringify(JSON.parse(line)));
    const agent = lines.map(JSON.parse).filter(event => event.provenance.component === "agent");
    expect(agent.map(({ provenance, ...event }) => event)).toEqual(native);
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
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "session.collection", data: { absentComponents: ["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval"], warnings: 0, untimedEvents: 0 } });
  });

  it("fails explicitly on read errors and removes a stale output rather than uploading it", () => {
    const source = write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "current" } }]);
    const output = write("usage/session.jsonl", "old session");
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
    expect(fs.readFileSync(path.join(root, "usage/session.jsonl"), "utf8")).not.toContain("opaque-mask");
  });

  it("writes atomically and cleans up failed outputs without following a temporary symlink", () => {
    const output = write("usage/session.jsonl", "old");
    const target = write("outside.jsonl", "unchanged");
    fs.symlinkSync(target, `${output}.tmp`);
    expect(() => writeSessionArtifact(output, [{ type: "vendor.event", data: {} }])).toThrow();
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
    expect(actual).toEqual(JSON.parse(JSON.stringify(expected)));
    expect(events.some(event => event.type === "mcp.tool_call")).toBe(true);
    expect(events.every(event => event.type.includes(".") && event.data && typeof event.data === "object")).toBe(true);
  });
});
