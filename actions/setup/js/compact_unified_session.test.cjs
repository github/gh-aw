import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { compactUnifiedSession, readCompactUnifiedSession, MAX_COMPACT_SESSION_BYTES, MAX_COMPACT_SESSION_EVENTS, MAX_COMPACT_STRING_BYTES, MAX_SOURCE_LINE_BYTES, MAX_SOURCE_SCAN_BYTES } = require("./compact_unified_session.cjs");
const event = (type, data) => ({ type, data, timestamp: "2026-10-20T19:00:00Z", provenance: { component: "agent", phase: "agent", path: "agent-session.jsonl", index: 0 } });
const parse = result => (result.session.trim() ? result.session.trim().split("\n").map(JSON.parse) : []);

describe("compact unified session projection", () => {
  it("retains bounded accounting and outcome metadata without mutating the source", () => {
    const events = [
      event("session.format", { version: 1 }),
      event("workflow.info", { engineId: "copilot", model: "auto", awContext: { content: "private context" } }),
      event("usage.report", { aic: 1.5, model: "model", usage: { inputTokens: 10, outputTokens: 3, inputTokensIncludeCache: false, raw: "private usage" }, path: "private path" }),
      event("session.result", { status: "success", numTurns: 2, usage: { totalTokens: 13 }, errors: ["private error"], agentMetrics: { unbounded: "private metrics" } }),
      event("guardrail.admission", { status: "admitted", maxAiCredits: 100 }),
      event("guardrail.daily_aic", { status: "under_budget", exceeded: false, total: 2 }),
      event("workflow.job_result", { jobId: "agent", result: "success", durationMs: 42 }),
      event("execution.result", { outcome: "success", exitCode: 0 }),
      event("grader.result", { value: 1, run: { passed: 1, errors: 0, unbounded: "private run" }, results: ["private results"] }),
      event("memory.push", {
        memoryId: "default",
        jobResult: "failure",
        outcome: "success",
        status: "saved",
        branch: "memory/default",
        commit: "b".repeat(40),
        content: "private memory",
        error: "private error",
      }),
    ];
    const original = JSON.stringify(events);
    const result = compactUnifiedSession(events);
    expect(result.version).toBe(1);
    expect(result.includedEvents).toBe(events.length);
    expect(result.omittedEvents).toBe(0);
    expect(result.truncated).toBe(false);
    const projected = parse(result);
    expect(projected[2].data).toEqual({ aic: 1.5, model: "model", usage: { inputTokens: 10, outputTokens: 3, inputTokensIncludeCache: false } });
    expect(projected[3].data).toEqual({ status: "success", numTurns: 2, usage: { totalTokens: 13 } });
    expect(projected[8].data).toEqual({ value: 1, run: { passed: 1, errors: 0 } });
    expect(projected[9].data).toEqual({
      memoryId: "default",
      jobResult: "failure",
      outcome: "success",
      status: "saved",
      branch: "memory/default",
      commit: "b".repeat(40),
    });
    expect(result.session).not.toContain("private");
    expect(JSON.stringify(events)).toBe(original);
  });

  it("drops all conversation, reasoning, prompt, tool and unknown event kinds", () => {
    const types = [
      "user.message",
      "assistant.message",
      "assistant.reasoning",
      "assistant.refusal",
      "assistant.message_update",
      "prompt.system",
      "prompt.user",
      "tool.execution_start",
      "tool.execution_complete",
      "session.init",
      "session.info",
      "session.error",
      "native.extension",
      "__proto__",
      "constructor",
    ];
    const result = compactUnifiedSession(types.map(type => event(type, { content: "private agent content", output: "private tool output" })));
    expect(result.session).toBe("");
    expect(result.omittedEvents).toBe(types.length);
    expect(result.truncated).toBe(false);
  });

  it("retains firewall usage as a distinct observation without summing overlapping accounting", () => {
    const report = event("usage.report", { aic: 1, usage: { totalTokens: 100 } });
    const firewall = event("firewall.token_usage", { aic: 1, usage: { totalTokens: 100 }, path: "private request path" });
    const result = parse(compactUnifiedSession([report, firewall]));
    expect(result.map(item => item.type)).toEqual(["usage.report", "firewall.token_usage"]);
    expect(result.map(item => item.data)).toEqual([
      { aic: 1, usage: { totalTokens: 100 } },
      { aic: 1, usage: { totalTokens: 100 } },
    ]);
  });

  it("omits unbounded strings, arrays, objects and nonfinite scalar values", () => {
    const input = event("usage.report", {
      model: "x".repeat(MAX_COMPACT_STRING_BYTES + 1),
      provider: "🦊".repeat(MAX_COMPACT_STRING_BYTES / 4),
      purpose: "\ud800",
      aic: Infinity,
      totalAic: NaN,
      premiumRequests: { nested: 1 },
      durationMs: [1, 2],
      status: "ok",
    });
    input.timestamp = { private: "metadata" };
    input.provenance.path = "x".repeat(MAX_COMPACT_STRING_BYTES + 1);
    input.raw = "private raw";
    const projected = parse(compactUnifiedSession([input]))[0];
    expect(projected.data).toEqual({ provider: "🦊".repeat(MAX_COMPACT_STRING_BYTES / 4), status: "ok" });
    expect(projected.timestamp).toBeUndefined();
    expect(projected.provenance.path).toBeUndefined();
    expect(projected.raw).toBeUndefined();
  });

  it("has an exact event-count ceiling and deterministic source ordering", () => {
    const events = Array.from({ length: MAX_COMPACT_SESSION_EVENTS + 1 }, (_, index) => ({ type: "session.format", data: { version: index } }));
    const result = compactUnifiedSession(events);
    expect(result.includedEvents).toBe(MAX_COMPACT_SESSION_EVENTS);
    expect(result.omittedEvents).toBe(1);
    expect(result.truncated).toBe(true);
    expect(parse(result).map(item => item.data.version)).toEqual(events.slice(0, MAX_COMPACT_SESSION_EVENTS).map(item => item.data.version));
    expect(compactUnifiedSession(events)).toEqual(result);
  });

  it.each(["🦊", '"', "\\", "\u0000"])("bounds UTF-8 JSONL bytes after serialization with %j", character => {
    const value = character.repeat(Math.floor(MAX_COMPACT_STRING_BYTES / Buffer.byteLength(character)));
    const events = Array.from({ length: 1000 }, () => event("workflow.info", { engineId: value, agentVersion: value, cliVersion: value, model: value, repository: value }));
    const result = compactUnifiedSession(events);
    expect(Buffer.byteLength(result.session)).toBeLessThanOrEqual(MAX_COMPACT_SESSION_BYTES);
    expect(Buffer.byteLength(JSON.stringify(result.session))).toBeLessThanOrEqual(2 * MAX_COMPACT_SESSION_BYTES + 2);
    expect(result.includedEvents).toBeLessThan(MAX_COMPACT_SESSION_EVENTS);
    expect(result.omittedEvents).toBe(1000 - result.includedEvents);
    expect(result.truncated).toBe(true);
  });

  it("admits exactly 16 KiB and drops a candidate that would exceed it by one byte", () => {
    const prefix = { type: "usage.report", data: { provider: "x".repeat(200), model: "x".repeat(200) } };
    const last = { type: "usage.report", data: { provider: "", model: "" } };
    const prefixBytes = Buffer.byteLength(JSON.stringify(prefix) + "\n");
    const lastBytes = Buffer.byteLength(JSON.stringify(last) + "\n");
    const count = Math.floor((MAX_COMPACT_SESSION_BYTES - lastBytes) / prefixBytes);
    const padding = MAX_COMPACT_SESSION_BYTES - count * prefixBytes - lastBytes;
    last.data.provider = "x".repeat(Math.min(MAX_COMPACT_STRING_BYTES, padding));
    last.data.model = "x".repeat(padding - last.data.provider.length);
    const events = [...Array(count).fill(prefix), last];
    const exact = compactUnifiedSession(events);
    expect(Buffer.byteLength(exact.session)).toBe(MAX_COMPACT_SESSION_BYTES);
    expect(exact.includedEvents).toBe(count + 1);
    expect(exact.truncated).toBe(false);
    last.data.model += "x";
    const overflow = compactUnifiedSession(events);
    expect(overflow.includedEvents).toBe(count);
    expect(overflow.omittedEvents).toBe(1);
    expect(overflow.truncated).toBe(true);
  });
});

describe("bounded compact session file reader", () => {
  let directory;
  let file;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "compact-session-"));
    file = path.join(directory, "aw_session.jsonl");
  });
  afterEach(() => {
    vi.restoreAllMocks();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("skips multi-chunk oversized agent messages and retains subsequent accounting", () => {
    const header = { type: "session.format", data: { version: 1 } };
    const usage = event("usage.report", { aic: 2, usage: { inputTokens: 100 } });
    fs.writeFileSync(file, JSON.stringify(header) + "\n" + JSON.stringify(event("assistant.message", { content: "private".repeat(200000) })) + "\n" + JSON.stringify(usage));
    const original = fs.readFileSync(file);
    const readFile = vi.spyOn(fs, "readFileSync");
    const result = readCompactUnifiedSession(file);
    expect(readFile).not.toHaveBeenCalled();
    expect(parse(result).map(item => item.type)).toEqual(["session.format", "usage.report"]);
    expect(result.oversizedLines).toBe(1);
    expect(result.omittedEvents).toBe(1);
    expect(result.truncated).toBe(true);
    expect(fs.readFileSync(file)).toEqual(original);
  });

  it("accepts a record at the byte limit and skips the one-byte overflow", () => {
    const record = JSON.stringify(event("session.format", { version: 1 }));
    fs.writeFileSync(file, record + " ".repeat(MAX_SOURCE_LINE_BYTES - Buffer.byteLength(record)) + "\n" + record + " ".repeat(MAX_SOURCE_LINE_BYTES - Buffer.byteLength(record) + 1) + "\n");
    const result = readCompactUnifiedSession(file);
    expect(result.includedEvents).toBe(1);
    expect(result.oversizedLines).toBe(1);
  });

  it("bounds source scanning even for a huge file and marks incomplete coverage", () => {
    fs.writeFileSync(file, '{"type":"session.format","data":{"version":1}}\n');
    fs.truncateSync(file, MAX_SOURCE_SCAN_BYTES + 1024);
    const result = readCompactUnifiedSession(file);
    expect(result.includedEvents).toBe(1);
    expect(result.scanTruncated).toBe(true);
    expect(result.truncated).toBe(true);
    expect(Buffer.byteLength(result.session)).toBeLessThanOrEqual(MAX_COMPACT_SESSION_BYTES);
  });

  it("does not chase data appended after the source-size snapshot", () => {
    fs.writeFileSync(file, '{"type":"session.format","data":{"version":1}}\n');
    const read = fs.readSync;
    vi.spyOn(fs, "readSync").mockImplementationOnce((...args) => {
      fs.appendFileSync(file, '{"type":"usage.report","data":{"aic":999}}\n');
      return read(...args);
    });
    expect(parse(readCompactUnifiedSession(file))).toEqual([{ type: "session.format", data: { version: 1 } }]);
  });

  it.each(["invalid UTF-8", "invalid JSON", "empty", "directory", "symlink"])("rejects %s source files", kind => {
    fs.writeFileSync(file, kind === "invalid UTF-8" ? Buffer.from([0xff]) : "{");
    if (kind === "empty") fs.writeFileSync(file, "");
    if (kind === "directory") file = directory;
    if (kind === "symlink") {
      const target = path.join(directory, "target");
      fs.renameSync(file, target);
      fs.symlinkSync(target, file);
    }
    expect(() => readCompactUnifiedSession(file)).toThrow();
  });

  it("does not expose rejected record contents through parser diagnostics", () => {
    fs.writeFileSync(file, '{"type":"private-sensitive-record" invalid JSON}');
    expect(() => readCompactUnifiedSession(file)).toThrow("Invalid UTF-8 or JSON in compact unified session source.");
    try {
      readCompactUnifiedSession(file);
    } catch (error) {
      expect(String(error)).not.toContain("private-sensitive-record");
      expect(error.cause).toBeUndefined();
    }
  });
});
