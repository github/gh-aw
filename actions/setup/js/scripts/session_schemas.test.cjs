import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { generateSessionSchemas, SCHEMA_DIRECTORY } from "./generate_session_schemas.cjs";
import { validateSession, createSessionValidator } from "./validate_session.cjs";
import { collectUnifiedSession, parseEngineSession, SESSION_FILE_FORMAT_VERSION } from "../unified_session.cjs";
import { serializeSessionArtifact } from "../session_artifact.cjs";
import { normalizeUnifiedSessionEvent } from "../unified_session_payload.cjs";

const roots = [];
const jsonl = events => events.map(event => JSON.stringify(event)).join("\n") + "\n";
const header = {
  type: "session.format",
  data: { version: SESSION_FILE_FORMAT_VERSION },
  provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 },
};
const unified = (event, index = 0) => ({ ...event, provenance: { component: "agent", phase: "agent", path: "agent-session.jsonl", index } });
const tempRoot = () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-session-schema-"));
  roots.push(root);
  return root;
};
afterEach(() => {
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("Generated session schemas", () => {
  it("regenerates byte-identical checked-in schemas and compiles them without external references", async () => {
    const generated = await generateSessionSchemas();
    for (const [filename, content] of Object.entries(generated)) {
      expect(content).toBe(fs.readFileSync(path.join(SCHEMA_DIRECTORY, filename), "utf8"));
      expect(JSON.parse(content).$schema).toBe("http://json-schema.org/draft-07/schema#");
    }
    expect(createSessionValidator("agent").array([])).toBe(true);
    expect(createSessionValidator("unified").array([header])).toBe(true);
  });

  it("validates canonical core events, extensions, native metadata and supplied falsy values without mutation", () => {
    const events = [
      { type: "session.init", id: "native", parentId: null, timestamp: 0, nativeVersion: 3, data: { sourceEngine: "goose", tools: [], extra: false } },
      { type: "user.message", data: { content: "" } },
      { type: "assistant.message", data: { content: null } },
      { type: "assistant.reasoning", data: { content: " \n" } },
      { type: "tool.execution_start", data: { input: false } },
      { type: "tool.execution_complete", data: { success: false, output: 0, result: [], error: null, durationMs: 0 } },
      { type: "session.result", data: { numTurns: 0, totalCostUsd: 0, usage: { input_tokens: 0, totalTokens: 0 }, errors: [] } },
      { type: "vendor.progress", data: { nested: [false, 0, null, "", {}, []] } },
    ];
    const content = JSON.stringify(events);
    expect(validateSession(content, "agent", "json")).toBe(events.length);
    expect(validateSession(jsonl(events), "agent")).toBe(events.length);
    expect(JSON.stringify(events)).toBe(content);
    expect(validateSession("", "agent")).toBe(0);
  });

  it.each([
    { type: "result", data: {} },
    { type: "assistant.message", data: [] },
    { type: "assistant.message" },
    { type: "tool.execution_complete", data: { success: "yes" } },
    { type: "session.result", data: { numTurns: -1 } },
    { type: "session.result", data: { durationMs: -1 } },
    { type: "session.result", data: { usage: { input_tokens: 0.5 } } },
    { type: "session.result", data: { usage: { input_tokens: Number.MAX_SAFE_INTEGER + 1 } } },
    { type: "detection.result", data: { promptInjection: "false" } },
    { type: "session.format", data: {} },
    { type: "session.format", data: { version: 0 } },
    { type: "session.format", data: { version: 1.5 } },
    { type: "agent.execution", data: { categories: ["timeout", "timeout"], errorCodes: [], errorTypes: [] } },
    { type: "agent.execution", data: { categories: [], errorCodes: [0.5], errorTypes: [] } },
    { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [""], exitCode: 256 } },
  ])("rejects invalid known payloads instead of accepting them as native extensions: %j", event => {
    expect(() => validateSession(jsonl([event]), "agent")).toThrow("Invalid session event at line 1");
  });

  it("separates canonical accounting from the essential unified projection", () => {
    const canonical = { type: "session.result", data: { usage: { input_tokens: 0, output_tokens: 2, reasoning_output_tokens: 1 } } };
    const compact = normalizeUnifiedSessionEvent(canonical);
    expect(compact.data.usage).toEqual({ inputTokens: 0, outputTokens: 2, reasoningOutputTokens: 1 });
    expect(validateSession(jsonl([header, unified(compact)]))).toBe(2);
    expect(() => validateSession(jsonl([header, unified(canonical)]))).toThrow("Invalid session event");
    expect(() => validateSession(jsonl([header, unified({ type: "session.init", data: { tools: [] } })]))).toThrow("Invalid session event");
    expect(validateSession(jsonl([header, unified({ type: "vendor.progress", data: { arbitrary: { body: false } } })]))).toBe(2);
  });

  it.each(["claude", "copilot", "codex", "gemini", "pi", "custom", "opencode", "goose"])("validates the %s parser's native passthrough and unified collection", engine => {
    const root = tempRoot();
    const input = jsonl([
      { type: "session.init", data: { sourceEngine: engine, model: "fixture" } },
      { type: "assistant.message", data: { content: "Done.\n" } },
      { type: "session.result", data: { numTurns: 0, usage: { input_tokens: 0 }, errors: [] } },
    ]);
    const parsed = parseEngineSession(input, engine);
    expect(validateSession(serializeSessionArtifact(parsed), "agent")).toBe(3);
    fs.writeFileSync(path.join(root, "agent-stdio.log"), input);
    const collected = collectUnifiedSession({
      rootDir: root,
      engine,
      warn: message => {
        throw new Error(message);
      },
    });
    expect(validateSession(serializeSessionArtifact(collected.events))).toBe(5);
  });

  it("validates actual raw engine mappings rather than only native passthrough", () => {
    const inputs = {
      claude: [{ type: "result", num_turns: 0, usage: { input_tokens: 0 } }],
      copilot: [{ type: "result", num_turns: 0, usage: { input_tokens: 0 } }],
      codex: [
        { type: "thread.started", thread_id: "thread" },
        { type: "turn.completed", usage: { input_tokens: 10, output_tokens: 2 } },
      ],
      gemini: [
        { type: "init", model: "fixture", session_id: "session" },
        { type: "result", stats: { input_tokens: 10, output_tokens: 2, duration_ms: 0 } },
      ],
      pi: [
        { type: "init", model: "fixture" },
        { type: "result", stats: { input_tokens: 10, output_tokens: 2, turns: 1 } },
      ],
      custom: [{ type: "result", num_turns: 0, usage: { input_tokens: 0 } }],
      opencode: [{ type: "step_finish", sessionID: "session", timestamp: 0, part: { id: "part", cost: 0, tokens: { input: 10, output: 2, cache: { read: 0, write: 0 } } } }],
      goose: [
        { type: "message", message: { id: "message", role: "assistant", created: 0, content: [{ type: "text", text: "Done.\n" }] } },
        { type: "complete", input_tokens: 10, output_tokens: 2 },
      ],
    };
    for (const [engine, records] of Object.entries(inputs)) {
      const events = parseEngineSession(jsonl(records), engine);
      expect(events.length, engine).toBeGreaterThan(0);
      expect(validateSession(serializeSessionArtifact(events), "agent")).toBe(events.length);
    }
  });

  it("validates collected runtime families, decoded strings, projection aliases, warnings and coverage", () => {
    const root = tempRoot();
    const sources = {
      "agent-session.jsonl": [{ type: "assistant.message", timestamp: 1, data: { content: "Done.\n" } }],
      "mcp-logs/rpc.jsonl": [{ timestamp: 2, event: "rpc_response", server_id: "github", payload: { id: 0, error: { code: 0, message: "Failure", context: "omitted" } } }],
      "sandbox/firewall/logs/audit.jsonl": [{ ts: 0.003, event: "http_access", host: "example.com", status: 403, decision: "denied" }],
      "sandbox/firewall/logs/tokens.jsonl": [{ ts: 4, event: "token_usage", input_tokens: 0, output_tokens: 2, ai_credits: 0 }],
      "safeoutputs.jsonl": [{ type: "create_issue", title: "omitted" }],
      "safe-output-items.jsonl": [{ type: "create_issue", number: 1, status: "success" }],
      "safe-output-errors.json": { failures: [{ type: "add_comment", error: "failed" }] },
      "experiments/state.json": { run_id: "run", counts: { a: 0 } },
      "experiments/assignments.json": { a: false },
      "agent/graders/grader_manifest.json": { graders: [{ id: "quality", script: "omitted" }] },
      "agent/graders/grader_results.json": { results: [{ id: "quality", value: 0, passed: false }] },
      "evals/evals.jsonl": [{ id: "eval", answer: "line one\nline two", model: "fixture", question: "omitted" }],
      "agent_usage.jsonl": [{ usage: { provider: "fixture", input_tokens: 0, output_tokens: 2 } }],
      "usage/agent/execution.json": { exit_code: 0, outcome: "success" },
      "usage/detection/detection_result.json": { job_result: "success", conclusion: "warning", reason: "threat_detected", prompt_injection: false },
      "aw_info.json": { engine_id: "copilot", run_id: "run", event_name: "workflow_dispatch" },
    };
    for (const [file, value] of Object.entries(sources)) {
      const target = path.join(root, file);
      fs.mkdirSync(path.dirname(target), { recursive: true });
      fs.writeFileSync(target, Array.isArray(value) ? jsonl(value) : JSON.stringify(value));
    }
    fs.appendFileSync(path.join(root, "mcp-logs/rpc.jsonl"), "{broken\n");
    const warnings = [];
    const collected = collectUnifiedSession({ rootDir: root, warn: message => warnings.push(message) });
    expect(warnings).toHaveLength(1);
    const content = serializeSessionArtifact(collected.events);
    expect(validateSession(content)).toBe(collected.events.length);
    expect(content).not.toContain("omitted");
    expect(content).not.toContain("{broken");
  });

  it.each(
    [
      [],
      [unified({ type: "assistant.message", data: {} })],
      [{ ...header, data: { version: 2 } }],
      [{ ...header, timestamp: 0 }],
      [{ ...header, provenance: { ...header.provenance, phase: "agent" } }],
      [{ ...header, provenance: { ...header.provenance, timestampMs: 0 } }],
      [header, { type: "vendor.progress", data: {} }],
      [header, unified({ type: "vendor.progress", data: {} }, -1)],
      [header, { ...unified({ type: "vendor.progress", data: {} }), provenance: { ...header.provenance, path: "../secret" } }],
      [header, { ...unified({ type: "vendor.progress", data: {} }), provenance: { ...header.provenance, path: "/secret" } }],
    ].map(events => [events])
  )("rejects invalid headers and provenance: %j", events => {
    expect(() => validateSession(JSON.stringify(events), "unified", "json")).toThrow();
  });

  it("checks timestamp ordering, the untimed tail and execution singleton semantics", () => {
    const timed = time => ({ ...unified({ type: "vendor.progress", data: {} }), provenance: { ...unified({}).provenance, timestampMs: time } });
    const untimed = unified({ type: "vendor.progress", data: {} });
    expect(validateSession(jsonl([header, timed(0), timed(0), timed(1), untimed]))).toBe(5);
    expect(() => validateSession(jsonl([header, timed(1), timed(0)]))).toThrow("timestamp ordered");
    expect(() => validateSession(jsonl([header, untimed, timed(0)]))).toThrow("timestamp ordered");
    const execution = unified({ type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } });
    expect(() => validateSession(jsonl([header, execution, execution]))).toThrow("multiple agent.execution");
    expect(() => validateSession(jsonl([header, { ...execution, data: { ...execution.data, exitCode: 256 } }]))).toThrow("SessionExitCode");
  });

  it("checks exact JSONL framing without requiring canonical JSON number or escape spellings", () => {
    const valid = jsonl([header]);
    expect(validateSession(valid.replace('"version":1', '"version":1.0'))).toBe(1);
    expect(validateSession(jsonl([header, unified({ type: "assistant.message", data: { content: "  spaced \n" } })]))).toBe(2);
    expect(() => validateSession(valid.trimEnd())).toThrow("newline");
    expect(() => validateSession(valid + "\n")).toThrow("Blank");
    expect(() => validateSession(valid.replace('"type":', '"type": '))).toThrow("compact JSON");
    expect(() => validateSession(valid + "{PRIVATE_SECRET\n")).toThrow("line 2");
    expect(() => validateSession('{"events":[]}', "agent", "json")).toThrow("not a wrapper");
    expect(() => validateSession('[{"type":"session.result","data":{"durationMs":1e999}}]', "agent", "json")).toThrow("Invalid agent session");
  });

  it("does not expose private object keys in schema diagnostics", () => {
    try {
      validateSession('{"type":"assistant.message","data":{"content":{"PRIVATE_SECRET":1e999}}}\n', "agent");
      throw new Error("Expected schema validation failure");
    } catch (error) {
      expect(error.message).toContain("Invalid session event at line 1");
      expect(error.message).not.toContain("PRIVATE_SECRET");
    }
  });

  it("validates large JSON arrays without expanding them onto the call stack", () => {
    const events = Array.from({ length: 150000 }, () => ({ type: "vendor.progress", data: {} }));
    expect(validateSession(JSON.stringify(events), "agent", "json")).toBe(events.length);
  });

  it("runs both CLIs from outside the repository and fails with diagnostics but without payload text", () => {
    const root = tempRoot();
    const file = path.join(root, "session.jsonl");
    fs.writeFileSync(file, jsonl([header]));
    const validator = path.join(import.meta.dirname, "validate_session.cjs");
    const success = spawnSync(process.execPath, [validator, "unified", file], { cwd: root, encoding: "utf8" });
    expect(success.status).toBe(0);
    expect(success.stdout).toBe("Valid unified session: 1 record(s)\n");
    fs.appendFileSync(file, "{PRIVATE_SECRET\n");
    const failure = spawnSync(process.execPath, [validator, "unified", file], { cwd: root, encoding: "utf8" });
    expect(failure.status).toBe(1);
    expect(failure.stdout).toBe("");
    expect(failure.stderr).toContain("line 2");
    expect(failure.stderr).not.toContain("PRIVATE_SECRET");
    const generator = path.join(import.meta.dirname, "generate_session_schemas.cjs");
    expect(spawnSync(process.execPath, [generator, "--check"], { cwd: root, encoding: "utf8" }).status).toBe(0);
    expect(spawnSync(process.execPath, [generator, "--invalid"], { cwd: root, encoding: "utf8" }).status).toBe(1);
  });
});
