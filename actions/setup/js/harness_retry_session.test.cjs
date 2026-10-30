import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, parseEngineSession, writeUnifiedSession } from "./unified_session.cjs";
import { projectSessionResult } from "./agent_session.cjs";
import { correlateHarnessAttempts, parseHarnessAttempts } from "./harness_retry_session.cjs";
import { createSessionLookups } from "./grader_session.cjs";

const roots = [];
afterEach(() => {
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});
function fixture(engine, attempts) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-retry-session-"));
  roots.push(root);
  const write = (file, value) => {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof value === "string" ? value : JSON.stringify(value));
  };
  if (attempts) write("agent/harness_retry.json", { attempts: attempts.length, exit_code: attempts.at(-1)?.exit_code ?? 1, attempt_results: attempts });
  return { root, write, collect: () => collectUnifiedSession({ rootDir: root, engine, warn: () => {} }).events };
}
function attempt(number, exitCode = 0) {
  return {
    attempt: number,
    attempt_id: `harness:${number}`,
    exit_code: exitCode,
    failure_classes: exitCode === 0 || exitCode === null ? [] : ["server_error"],
    started_at: number * 1000,
    ...(exitCode !== null ? { finished_at: number * 1000 + 500 } : {}),
    strategy: number === 1 ? "restart" : "resume",
    backoff_ms: number === 1 ? 0 : 20,
    outcome: exitCode === null ? "running" : exitCode === 0 ? "success" : "failure",
  };
}

describe.each(["copilot", "claude", "codex"])("%s attempt conformance", engine => {
  it.each([
    ["single attempt", [attempt(1)]],
    ["successful retry", [attempt(1, 1), attempt(2)]],
    ["exhaustion", [attempt(1, 1), attempt(2, 1)]],
    ["interruption", [attempt(1, 1), attempt(2, null)]],
  ])("preserves %s exactly once through serialization", (_name, attempts) => {
    const { root, collect } = fixture(engine, attempts);
    const events = collect();
    const observed = events.filter(event => event.type === "harness.attempt");
    expect(observed.map(event => event.data.attempt)).toEqual(attempts.map(entry => entry.attempt));
    expect(new Set(observed.map(event => event.data.attemptId)).size).toBe(attempts.length);
    expect(observed.map(event => event.data.exitCode)).toEqual(attempts.map(entry => entry.exit_code));
    const outputPath = path.join(root, "usage/aw_session.jsonl");
    writeUnifiedSession({ rootDir: root, engine, outputPath });
    const saved = fs.readFileSync(outputPath, "utf8").trim().split("\n").map(JSON.parse);
    expect(saved.filter(event => event.type === "harness.attempt")).toEqual(observed);
  });

  it("does not invent attempts when telemetry is absent", () => {
    const { write, collect } = fixture(engine);
    write("agent-session.jsonl", `${JSON.stringify({ type: "session.init", data: { sessionId: "continuation" } })}\n`);
    expect(collect().some(event => event.type === "harness.attempt")).toBe(false);
  });

  it("correlates timed usage without adding accounting snapshots", () => {
    const { write, collect } = fixture(engine, [attempt(1, 1), attempt(2)]);
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", [1, 2].map(number => JSON.stringify({ event: "token_usage", timestamp: number * 1000 + 100, input_tokens: 10, output_tokens: 5 })).join("\n") + "\n");
    write("agent_usage.jsonl", `${JSON.stringify({ input_tokens: 20, output_tokens: 10 })}\n`);
    const events = collect();
    expect(events.filter(event => event.type === "firewall.token_usage").map(event => event.provenance.attemptId)).toEqual(["harness:1", "harness:2"]);
    expect(createSessionLookups(events).stats).toMatchObject({ totalInputTokens: 20, totalOutputTokens: 10, totalRequests: 2 });
  });
});

it.each([
  ["claude", false],
  ["claude", true],
  ["codex", false],
  ["codex", true],
])("preserves each %s transcript and accounting with shared session=%s", (engine, sharedSession) => {
  const { write, collect } = fixture(engine, [attempt(1, 1), attempt(2)]);
  const records = number =>
    engine === "claude"
      ? [
          { type: "system", subtype: "init", session_id: sharedSession ? "shared-session" : `session-${number}` },
          { type: "assistant", message: { content: [{ type: "text", text: `attempt-${number}` }] } },
          { type: "result", subtype: "success", num_turns: 1, usage: { input_tokens: number * 10, output_tokens: number } },
        ]
      : [
          { type: "thread.started", thread_id: sharedSession ? "shared-session" : `session-${number}` },
          { type: "item.completed", item: { id: "reused-id", type: "agent_message", text: `attempt-${number}` } },
          { type: "turn.completed", usage: { input_tokens: number * 10, output_tokens: number } },
        ];
  write("agent-session.jsonl", `${JSON.stringify({ type: "assistant.message", data: { content: "final-only" } })}\n`);
  const content = [1, 2].map(number => `[${engine}-harness] 2026-10-30T00:00:00Z attempt ${number}: spawning: ${engine}\n${records(number).map(JSON.stringify).join("\n")}\n`).join("");
  write("agent-stdio.log", content);
  const events = collect();
  const messages = events.filter(event => event.type === "assistant.message");
  expect(messages.map(event => event.data.content)).toEqual(["attempt-1", "attempt-2"]);
  expect(messages.map(event => event.provenance.attemptId)).toEqual(["harness:1", "harness:2"]);
  const positions = events.filter(event => event.provenance.component === "agent").map(event => event.provenance.index);
  expect(new Set(positions).size).toBe(positions.length);
  expect(events.filter(event => event.type === "session.result" && event.data.usage).map(event => event.data.usage.inputTokens)).toEqual(engine === "codex" ? [10, 30] : [10, 20]);
  const expected = projectSessionResult(parseEngineSession(content, engine));
  const observed = projectSessionResult(events.filter(event => event.provenance.component === "agent"));
  expect(observed.usage).toMatchObject(expected.usage);
  expect(observed.num_turns).toEqual(expected.num_turns);
});

it("retains canonical evidence when spawn markers are incomplete or ambiguous", () => {
  const attempts = [1, 2].map(number => ({ attempt: number, exitCode: 0, failureClasses: [] }));
  const parse = () => [{ type: "assistant.message", data: { content: "observed" } }];
  expect(parseHarnessAttempts("[claude-harness] attempt 2: spawning: claude\n", "claude", attempts, parse)).toBeUndefined();
  expect(parseHarnessAttempts("[claude-harness] attempt 1: spawning: claude\n".repeat(2), "claude", attempts, parse)).toBeUndefined();
  expect(parseHarnessAttempts("[claude-harness] attempt 1: spawning: claude\n[claude-harness] attempt 2: spawning: claude\n", "claude", attempts, parse)).toBeUndefined();
});

it("uses manifest-bound lifecycle markers and retains an empty interrupted attempt", () => {
  const attempts = [
    { attempt: 1, attemptId: "harness:1", startupAttemptId: "startup:2", exitCode: 1, failureClasses: [] },
    { attempt: 2, attemptId: "harness:2", startupAttemptId: "startup:2", exitCode: null, failureClasses: [] },
  ];
  const marker = number => `[gh-aw-harness-attempt] ${JSON.stringify({ event: "start", attempt: number, attempt_id: `harness:${number}`, startup_attempt_id: "startup:2" })}\n`;
  const content = `${marker(1)}observed\n${marker(2)}`;
  const parse = segment => (segment.includes("observed") ? [{ type: "assistant.message", data: { content: "observed" } }] : []);
  const segments = parseHarnessAttempts(content, "claude", attempts, parse);
  expect(segments?.map(segment => segment.events.length)).toEqual([1, 0]);
  expect(parseHarnessAttempts(content + marker(2), "claude", attempts, parse)).toBeUndefined();
  expect(parseHarnessAttempts(content.replace('"startup:2"', '"startup:1"'), "claude", attempts, parse)).toBeUndefined();
});

it("reconstructs startup-only failures from the usage artifact without inventing engine attempts", () => {
  const { write, collect } = fixture("claude");
  write("usage/agent/harness_retry.json", {
    attempts: 0,
    exit_code: 1,
    attempt_results: [],
    startup_attempt_results: [1, 2].map(number => ({ ...attempt(number, 1), attempt_id: `startup:${number}`, strategy: "restart" })),
  });
  const events = collect();
  expect(events.filter(event => event.type === "harness.attempt").map(event => [event.data.layer, event.data.attemptId])).toEqual([
    ["startup", "startup:1"],
    ["startup", "startup:2"],
  ]);
  expect(new Set(events.filter(event => event.provenance.component === "harness").map(event => event.provenance.index)).size).toBe(3);
});

it("does not replace protected canonical sessions with raw attempt transcripts", () => {
  const { root, write } = fixture("claude", [attempt(1, 1), attempt(2)]);
  write("agent-session.jsonl", `${JSON.stringify({ type: "assistant.message", data: { content: "verified" } })}\n`);
  write("agent-stdio.log", "[claude-harness] attempt 1: spawning: claude\nPRIVATE_RAW_TRANSCRIPT\n[claude-harness] attempt 2: spawning: claude\n");
  const { events } = collectUnifiedSession({ rootDir: root, engine: "claude", protectedSessionCapture: true });
  expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["verified"]);
  expect(JSON.stringify(events)).not.toContain("PRIVATE_RAW_TRANSCRIPT");
});

it("applies existing redaction to every reconstructed attempt", () => {
  const { root, write } = fixture("claude", [attempt(1, 1), attempt(2)]);
  write(
    "agent-stdio.log",
    "::add-mask::PRIVATE_RETRY_VALUE\n" +
      [1, 2]
        .map(
          number =>
            `[claude-harness] attempt ${number}: spawning: claude\n${JSON.stringify({
              type: "assistant",
              message: { content: [{ type: "text", text: `attempt-${number}: PRIVATE_RETRY_VALUE` }] },
            })}\n`
        )
        .join("")
  );
  const outputPath = path.join(root, "usage/aw_session.jsonl");
  writeUnifiedSession({ rootDir: root, engine: "claude", outputPath });
  const saved = fs.readFileSync(outputPath, "utf8");
  expect(saved).not.toContain("PRIVATE_RETRY_VALUE");
  expect(saved).toContain("attempt-1");
  expect(saved).toContain("attempt-2");
});

it("does not assign untimed or overlapping observations to an attempt", () => {
  const provenance = { component: "harness", phase: "agent", path: "agent/harness_retry.json", index: 0 };
  const events = [
    { type: "harness.attempt", data: { layer: "harness", attemptId: "harness:1", startedAt: 100, finishedAt: 200 }, provenance },
    { type: "harness.attempt", data: { layer: "harness", attemptId: "harness:2", startedAt: 100, finishedAt: 200 }, provenance },
    { type: "assistant.message", data: { content: "untimed" }, provenance: { ...provenance, component: "agent" } },
    { type: "usage.report", timestamp: 150, data: {}, provenance: { ...provenance, component: "usage" } },
  ];
  expect(
    correlateHarnessAttempts(events)
      .slice(2)
      .every(event => event.provenance.attemptId === undefined)
  ).toBe(true);
});
