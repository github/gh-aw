import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { success, failure, childObservations, release130 } from "./fixtures/pi_ci_stream.cjs";
import { parsePiLog } from "./parse_pi_log.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { writeUnifiedSession } from "./unified_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { createSessionValidator, validateSession } from "./scripts/validate_session.cjs";

describe("Pi native-to-unified artifact pipeline", () => {
  let root;
  let originalEnv;
  let originalCore;
  const readEvents = file => fs.readFileSync(file, "utf8").trimEnd().split("\n").map(JSON.parse);

  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-pi-session-"));
    originalEnv = { ...process.env };
    originalCore = global.core;
    process.env.GH_AW_AGENT_OUTPUT = path.join(root, "sandbox/agent/logs/pi-streaming.jsonl");
    fs.mkdirSync(path.dirname(process.env.GH_AW_AGENT_OUTPUT), { recursive: true });
    delete process.env.GH_AW_SAFE_OUTPUTS;
    global.core = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      exportVariable: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };
  });

  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    fs.rmSync(root, { recursive: true, force: true });
  });

  it.each([false, true])("publishes message clocks and tool correlation from saved canonical events (metadata-only clocks=%s)", legacy => {
    const records = [...success, ...childObservations];
    const events = JSON.parse(JSON.stringify(parsePiLog(records.map(JSON.stringify).join("\n")).logEntries));
    if (legacy) {
      for (const event of events) if (event.data.messageMetadata?.timestamp !== undefined) delete event.timestamp;
    }
    const original = structuredClone(events);
    fs.writeFileSync(path.join(root, "agent-session.jsonl"), events.map(JSON.stringify).join("\n") + "\n");
    const unified = writeUnifiedSession({ rootDir: root, engine: "pi", warn: vi.fn() });
    const agent = unified.filter(event => event.provenance.path === "agent-session.jsonl");
    const messages = agent.filter(event => event.type === "assistant.message" || event.type === "assistant.reasoning" || event.type === "tool.execution_start");
    expect(messages).toHaveLength(4);
    for (const event of messages) {
      const native = events[event.provenance.index];
      expect(event.timestamp).toBe(native.data.messageMetadata.timestamp);
      expect(event.provenance.timestampMs).toBe(native.data.messageMetadata.timestamp);
      expect(event.data.messageId).toBe(native.data.messageId);
      expect(event.data.model).toBe(native.data.model);
      expect(event.data).not.toHaveProperty("messageMetadata");
    }
    expect(messages.find(event => event.type === "tool.execution_start").data.input).toEqual({ command: "printf sanitized" });
    expect(messages.find(event => event.agentId)?.agentId).toBe("legacy:pi:sanitized-reader:1");
    const bytes = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(bytes)).toBe(unified.length);
    expect(readEvents(path.join(root, "agent-session.jsonl"))).toEqual(original);
    writeUnifiedSession({ rootDir: root, engine: "pi", warn: vi.fn() });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toBe(bytes);
  });

  // Sanitized excerpts from separate runs, combined locally to exercise the
  // collector. This is not a new live execution or an exact single-run fixture.
  it.each([
    ["success with child observations", [...success, ...childObservations]],
    ["provider failure", failure],
    ["v1.3.0 release contract", release130],
  ])("publishes %s once with authoritative content, scope and provenance", async (_, records) => {
    const raw = records.map(record => JSON.stringify(record)).join("\n") + "\n";
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);
    await runLogParser({ parserName: "Pi", parseLog: parsePiLog, rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(global.core.summary.addRaw).toHaveBeenCalled();
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    const parsed = JSON.parse(JSON.stringify(parsePiLog(raw).logEntries));
    expect(canonical.filter(event => event.type !== "agent.execution")).toEqual(parsed);

    const unified = writeUnifiedSession({ rootDir: root, engine: "pi", warn: vi.fn() });
    const file = path.join(root, "usage/aw_session.jsonl");
    expect(readEvents(file)).toEqual(unified);
    const agent = unified.filter(event => event.provenance?.path === "agent-session.jsonl" && event.type !== "agent.execution");
    expect(agent.map(event => event.type)).toEqual(parsed.map(event => event.type));
    expect(agent.map(event => event.data)).toEqual(parsed.map(event => normalizeUnifiedSessionEvent(event).data));
    for (const [index, event] of agent.entries()) {
      expect(event.provenance).toMatchObject({ component: "agent", phase: "agent", path: "agent-session.jsonl", index });
    }
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(unified.some(event => event.type.startsWith("pi."))).toBe(false);
    expect(unified.some(event => event.type === "session.collection_warning")).toBe(false);
    const bytes = fs.readFileSync(file, "utf8");
    writeUnifiedSession({ rootDir: root, engine: "pi", warn: vi.fn() });
    expect(fs.readFileSync(file, "utf8")).toBe(bytes);
    expect(fs.readFileSync(process.env.GH_AW_AGENT_OUTPUT, "utf8")).toBe(raw);

    fs.unlinkSync(path.join(root, "agent-session.jsonl"));
    const reconstructed = writeUnifiedSession({ rootDir: root, engine: "pi", warn: vi.fn() });
    expect(reconstructed.filter(event => event.provenance?.component === "agent" && event.type !== "agent.execution").map(event => event.data)).toEqual(agent.map(event => event.data));
    expect(reconstructed.filter(event => event.provenance?.component === "agent").every(event => event.provenance.path === "sandbox/agent/logs/pi-streaming.jsonl")).toBe(true);
  });
});
