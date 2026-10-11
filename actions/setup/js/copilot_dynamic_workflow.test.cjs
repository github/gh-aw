import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseCopilotLog } from "./parse_copilot_log.cjs";
import { collectUnifiedSession, mergeSessionSources } from "./unified_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";
import { runWithCopilotSDK } from "./copilot_sdk_session.cjs";
import { projectSessionResult } from "./agent_session.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";

const runId = "dynamic-smoke-run";
const nativePath = "sandbox/agent/logs/copilot-session-state/root/events.jsonl";
const lifecycle = [
  { type: "workflow.run_started", data: { runId, workflowName: "smoke-copilot-dynamic-workflow", attempt: 1 }, ephemeral: true },
  { type: "workflow.run_updated", data: { runId, revision: 0 }, ephemeral: true },
  { type: "workflow.run_updated", data: { runId, revision: 1 }, ephemeral: true },
  {
    type: "subagent.started",
    agentId: "smoke-child",
    data: { workflowRunId: runId, toolCallId: "child-call", agentName: "smoke-marker", agentDisplayName: "Smoke marker", model: "fixture-model", agentDescription: "PRIVATE_PROMPT" },
  },
  { type: "subagent.configured", agentId: "smoke-child", data: { model: "fixture-model", multiTurn: false } },
  { type: "assistant.message", agentId: "smoke-child", data: { content: "GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK" } },
  { type: "subagent.completed", agentId: "smoke-child", data: { toolCallId: "child-call", agentName: "smoke-marker", durationMs: 0, totalTokens: 0, totalToolCalls: 0, cancelled: false } },
  { type: "workflow.run_settled", data: { runId, status: "completed", consumedNanoAiu: 0, consumedSubagents: 1, elapsedMs: 0 }, ephemeral: true },
].map((event, index) => ({ id: `workflow-event-${index}`, parentId: index ? `workflow-event-${index - 1}` : null, timestamp: `2026-10-06T15:00:0${index}Z`, ...event }));
const serialize = events => events.map(event => JSON.stringify(event)).join("\n") + "\n";

describe("Copilot dynamic workflow sessions", () => {
  const directories = [];
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
  });
  const temporaryRoot = () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-dynamic-session-"));
    directories.push(root);
    return root;
  };

  it("collects exact per-agent accounting and attribution metadata into the unified file", () => {
    const root = temporaryRoot();
    const modelMetric = (count, totalNanoAiu) => ({
      requests: { count, cost: 0 },
      usage: { inputTokens: 100, outputTokens: 10, cacheReadTokens: 20, cacheWriteTokens: 30 },
      totalNanoAiu,
    });
    const agentMetrics = {
      main: { totalNanoAiu: 389122000000, modelMetrics: { opus: modelMetric(41, 389122000000) } },
      research: { agentName: "research", agentDisplayName: "routing-research", totalNanoAiu: 494207000000, modelMetrics: { opus: modelMetric(45, 494207000000) } },
      explore: { agentName: "explore", agentDisplayName: "routing-explore", totalNanoAiu: 9141000000, modelMetrics: { opus: modelMetric(3, 9141000000) } },
    };
    const native = [
      { type: "session.start", data: { sessionId: "root", selectedModel: "opus", reasoningEffort: "xhigh" } },
      {
        type: "subagent.started",
        agentId: "research",
        data: {
          toolCallId: "research-call",
          agentName: "research",
          agentDisplayName: "routing-research",
          agentType: "built-in",
          model: "opus",
          modelSelectionSource: "agent_definition_default",
          executionMode: "background",
          agentDescription: "PRIVATE_PROMPT",
        },
      },
      { type: "subagent.configured", agentId: "research", data: { model: "opus", reasoningEffort: "xhigh" } },
      { type: "subagent.started", agentId: "explore", data: { parentId: "research", toolCallId: "explore-call", agentName: "explore", agentDisplayName: "routing-explore", model: "opus", executionMode: "sync" } },
      { type: "subagent.configured", agentId: "explore", data: { model: "opus", reasoningEffort: "low" } },
      { type: "session.model_change", agentId: "explore", data: { newModel: "opus", reasoningEffort: "low", source: "agent" } },
      { type: "assistant.message", agentId: "explore", data: { content: "Done", model: "opus", apiCallId: "api-1", interactionId: "interaction-1", turnId: "turn-1", parentToolCallId: "explore-call", toolRequests: [] } },
      { type: "subagent.completed", agentId: "explore", data: { model: "opus", firstDispatchedModel: "opus", totalTokens: 110, totalToolCalls: 1, durationMs: 100 } },
      { type: "session.shutdown", data: { agentMetrics } },
    ];
    const parsed = parseCopilotLog(serialize(native)).logEntries;
    expect(parsed.find(event => event.type === "session.result").data.agentMetrics).toEqual(agentMetrics);
    const file = path.join(root, nativePath);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, serialize(native));
    const events = JSON.parse(JSON.stringify(collectUnifiedSession({ rootDir: root, engine: "copilot" }).events));
    expect(events.find(event => event.type === "session.init").data).toMatchObject({ model: "opus", reasoningEffort: "xhigh" });
    expect(events.find(event => event.type === "subagent.started" && event.agentId === "research").data).toMatchObject({ agentType: "built-in", modelSelectionSource: "agent_definition_default", executionMode: "background" });
    expect(events.find(event => event.type === "subagent.started" && event.agentId === "explore").data.parentId).toBe("research");
    expect(events.find(event => event.type === "subagent.configured" && event.agentId === "explore").data.reasoningEffort).toBe("low");
    expect(events.find(event => event.type === "assistant.message").data).toMatchObject({ model: "opus", apiCallId: "api-1", interactionId: "interaction-1", turnId: "turn-1", parentToolCallId: "explore-call" });
    expect(events.find(event => event.type === "subagent.completed").data.firstDispatchedModel).toBe("opus");
    expect(events.find(event => event.type === "session.result").data.agentMetrics).toEqual(agentMetrics);
    expect(events.find(event => event.type === "subagent.started").data).not.toHaveProperty("agentDescription");
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("retains native lifecycle signals and correlates subagents without conflating workflow accounting with session usage", () => {
    const original = structuredClone(lifecycle);
    const parsed = parseCopilotLog(serialize(lifecycle)).logEntries;
    expect(parsed).toEqual(lifecycle);
    const merged = mergeSessionSources([{ component: "agent", phase: "agent", path: nativePath, events: parsed }]);
    expect(merged.map(event => event.type)).toEqual(lifecycle.map(event => event.type));
    expect(merged.filter(event => event.type === "workflow.run_updated").map(event => event.data.revision)).toEqual([0, 1]);
    expect(merged[3]).toMatchObject({ agentId: "smoke-child", data: { workflowRunId: runId }, provenance: { path: nativePath, index: 3 } });
    expect(merged[5]).toMatchObject({ agentId: "smoke-child", data: { content: "GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK", agentId: "smoke-child" } });
    expect(merged[7]).toMatchObject({ ephemeral: true, data: { status: "completed", consumedNanoAiu: 0, consumedSubagents: 1, elapsedMs: 0 } });
    expect(projectSessionResult(merged)).toBeUndefined();
    expect(merged[3].data).not.toHaveProperty("agentDescription");
    expect(lifecycle).toEqual(original);
    for (const event of merged) {
      const { provenance, ...normalized } = event;
      expect(normalizeUnifiedSessionEvent(normalized)).toEqual(normalized);
    }
  });

  it.each(["error", "paused", "cancelled", "halted"])("preserves a %s outcome rather than synthesizing successful completion", status => {
    const source = {
      type: "workflow.run_settled",
      ephemeral: true,
      data: { runId, status, consumedNanoAiu: 0, consumedSubagents: 0, elapsedMs: 0, failureType: "workflow_limit_reached", result: "PRIVATE_RESULT" },
    };
    const event = normalizeUnifiedSessionEvent(source);
    expect(event.data).toEqual({ runId, status, consumedNanoAiu: 0, consumedSubagents: 0, elapsedMs: 0, failureType: "workflow_limit_reached" });
    const output = generatePlainTextSummary(mergeSessionSources([{ component: "agent", phase: "agent", path: nativePath, events: [event] }]));
    expect(output).toContain(`status=${status}`);
    expect(output).toContain("failureType=workflow_limit_reached");
    expect(output).not.toContain("PRIVATE_RESULT");
    expect(output).not.toContain("status=completed");
  });

  it("uses legacy workflow run identity only when the current field is absent", () => {
    expect(normalizeUnifiedSessionEvent({ type: "subagent.started", data: { factoryRunId: "legacy" } }).data.workflowRunId).toBe("legacy");
    expect(normalizeUnifiedSessionEvent({ type: "subagent.started", data: { workflowRunId: null, factoryRunId: "legacy" } }).data.workflowRunId).toBeNull();
  });

  it("preserves and renders subagent failures without exposing opaque results", () => {
    const source = {
      type: "subagent.failed",
      agentId: "smoke-child",
      data: { toolCallId: "child-call", agentName: "smoke-marker", model: "fixture-model", durationMs: 0, error: "subagent failed", result: "PRIVATE_RESULT" },
    };
    const merged = mergeSessionSources([{ component: "agent", phase: "agent", path: nativePath, events: [source] }]);
    expect(merged[0].data).toEqual({ toolCallId: "child-call", agentName: "smoke-marker", model: "fixture-model", durationMs: 0, error: "subagent failed" });
    for (const output of [generatePlainTextSummary(merged), generateCopilotCliStyleSummary(merged)]) {
      expect(output).toContain("subagent.failed agentId=smoke-child toolCallId=child-call");
      expect(output).toContain("durationMs=0");
      expect(output).toContain("subagent failed");
      expect(output).not.toContain("PRIVATE_RESULT");
    }
  });

  it("renders workflow status, revisions, resource observations, and subagent identity in both publication views", () => {
    const merged = mergeSessionSources([{ component: "agent", phase: "agent", path: nativePath, events: parseCopilotLog(serialize(lifecycle)).logEntries }]);
    for (const output of [generatePlainTextSummary(merged), generateCopilotCliStyleSummary(merged)]) {
      expect(output).toContain(`workflow.run_started runId=${runId} workflowName=smoke-copilot-dynamic-workflow attempt=1`);
      expect(output).toContain(`workflow.run_updated runId=${runId} revision=0`);
      expect(output).toContain(`workflow.run_settled runId=${runId} status=completed consumedNanoAiu=0 consumedSubagents=1 elapsedMs=0`);
      expect(output).toContain(`subagent.started agentId=smoke-child workflowRunId=${runId} toolCallId=child-call`);
      expect(output).toContain("subagent.completed agentId=smoke-child toolCallId=child-call");
      expect(output).toContain("totalTokens=0 totalToolCalls=0 cancelled=false");
      expect(output).not.toContain("PRIVATE_PROMPT");
      expect(output).not.toContain("[extension payload omitted]");
    }
  });

  it("collects observed workflow events from canonical and native sessions without duplicating them", () => {
    const root = temporaryRoot();
    const events = [{ type: "session.start", data: { sessionId: "root", startTime: "2026-10-06T14:59:59Z" } }, ...lifecycle];
    const file = path.join(root, nativePath);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, serialize(events));
    const native = collectUnifiedSession({ rootDir: root, engine: "copilot" }).events;
    expect(native.filter(event => event.type.startsWith("workflow.run_"))).toHaveLength(4);
    fs.writeFileSync(path.join(root, "agent-session.jsonl"), serialize(parseCopilotLog(serialize(events)).logEntries.map((event, index) => ({ ...event, provenance: { component: "agent", phase: "agent", path: nativePath, index } }))));
    const canonical = collectUnifiedSession({ rootDir: root, engine: "copilot" }).events;
    expect(canonical.filter(event => event.type.startsWith("workflow.run_"))).toHaveLength(4);
    expect(canonical.filter(event => event.type === "subagent.started")).toMatchObject([{ agentId: "smoke-child", data: { workflowRunId: runId } }]);
  });

  it("serializes ephemeral SDK workflow signals, child metadata and observed message deltas", async () => {
    const root = temporaryRoot();
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    vi.stubEnv("GH_AW_SDK_IDLE_MS", "1234");
    const setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");
    const delta = { type: "assistant.message_delta", ephemeral: true, data: { deltaContent: "  observed delta\n" } };
    let onEvent = () => {};
    const session = {
      sessionId: "sdk-workflow",
      on: handler => {
        onEvent = handler;
      },
      sendAndWait: async () => {
        for (const event of lifecycle) onEvent(event);
        onEvent(delta);
        return { data: { content: "done" } };
      },
      disconnect: async () => {},
    };
    class FakeClient {
      start = async () => {};
      createSession = async () => session;
      stop = async () => {};
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "Run the smoke workflow",
      logger: () => {},
      sessionStateBaseDir: root,
      sdkModule: { CopilotClient: FakeClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result).toMatchObject({ exitCode: 0, output: "done", hasOutput: true });
    const events = fs
      .readFileSync(path.join(root, session.sessionId, "events.jsonl"), "utf8")
      .trim()
      .split("\n")
      .map(JSON.parse);
    expect(events.filter(event => event.type.startsWith("workflow.run_"))).toEqual(lifecycle.filter(event => event.type.startsWith("workflow.run_")));
    expect(events.find(event => event.type === "subagent.started")).toEqual({ ...lifecycle[3], data: { ...lifecycle[3].data, agentDisplayName: lifecycle[3].data.agentName } });
    expect(events.find(event => event.type === "assistant.message")).toEqual(lifecycle[5]);
    expect(events.filter(event => event.type === "assistant.message_delta")).toEqual([delta]);
    expect(setTimeoutSpy.mock.calls.filter(([, timeout]) => timeout === 1234)).toHaveLength(0);
  });
});
