import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { normalizeCopilotSession } from "./copilot_session.cjs";
import { observedSessionModel, projectSessionInitialization, projectSessionResult, sessionEventContexts } from "./agent_session.cjs";
import { convertCopilotEventsToLegacyLogEntries, generatePlainTextSummary, generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";
import { collapseStreamedMessages } from "./agent_session_render.cjs";
import { mergeSessionSources, writeUnifiedSession } from "./unified_session.cjs";
import { scopedAgentSessions } from "./unified_session_render.cjs";
import { renderSubagentSummary } from "./subagent_session_render.cjs";
import { readCopilotSessions } from "./log_parser_bootstrap.cjs";
import { parseCopilotLog } from "./parse_copilot_log.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { copilotNestedCi, copilotFailedChildCi } from "./fixtures/copilot_nested_ci.cjs";

const byType = (events, type) => events.filter(event => event.type === type);
const compact = events => mergeSessionSources([{ component: "agent", phase: "agent", path: "events.jsonl", events }]);
const stable = events => expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
const native = (type, agentId, data) => ({ type, ...(agentId ? { agentId } : {}), data });
const require = createRequire(import.meta.url);

describe("Copilot nested session normalization", () => {
  it("preserves the observed three-level tree, exact content and accounting without inflating root turns", () => {
    const original = structuredClone(copilotNestedCi);
    const events = normalizeCopilotSession(copilotNestedCi);
    expect(copilotNestedCi).toEqual(original);
    stable(events);
    expect(projectSessionResult(events).num_turns).toBe(1);
    const turns = events.filter(event => event.copilotProjection === "finalized-turns");
    expect(Object.fromEntries(turns.map(event => [event.data.agentId ?? "main", event.data.numTurns]))).toEqual({ main: 1, research: 1, left: 1, right: 1 });
    expect(byType(events, "tool.execution_start")).toHaveLength(2);
    expect(byType(events, "assistant.message").map(event => event.data.content)).toEqual(byType(original, "assistant.message").map(event => event.data.content));
    const left = byType(events, "assistant.message").find(event => event.agentId === "left");
    expect(left.data).toMatchObject({ sessionId: "root", agentId: "left", parentAgentId: "research", parentToolCallId: "spawn-left", originatingMessageId: "left-user" });
    expect(left.data.parentAgentId).not.toBe("preceding-left-event");
    expect(byType(events, "user.message").find(event => event.agentId === "research").data.parentToolCallId).toBe("spawn-research");
    expect(projectSessionResult(events).usage).toBeUndefined();
    expect(events.find(event => event.type === "session.result" && event.data.agentMetrics).data.agentMetrics).toEqual(original.at(-1).data.agentMetrics);
    const published = compact(events);
    const validate = createSessionValidator("unified").event;
    for (const event of published) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(byType(published, "assistant.message").find(event => event.agentId === "left").data.parentAgentId).toBe("research");
    expect(published.find(event => event.type === "session.result" && event.agentId === "left" && event.data.numTurns !== undefined).data).toMatchObject({ sessionId: "root", agentId: "left", parentAgentId: "research", numTurns: 1 });
  });

  it("renders separate root, child and grandchild conversations while retaining the complete tree overview", () => {
    const events = compact(normalizeCopilotSession(copilotNestedCi));
    const groups = scopedAgentSessions(events);
    expect(groups).toHaveLength(4);
    expect(groups.find(group => group.label.includes("agentId=left")).label).toContain("parentAgentId=research");
    for (const group of groups) {
      const owners = new Set(byType(group.events, "assistant.message").map(event => event.agentId ?? "main"));
      expect(owners.size).toBe(1);
    }
    const root = groups.find(group => group.label === "agent/events.jsonl");
    expect(byType(root.events, "subagent.started")).toHaveLength(3);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("Agent conversation: agent/events.jsonl (sessionId=root agentId=research");
      expect(output).toContain("Agent conversation: agent/events.jsonl (sessionId=root agentId=left parentAgentId=research");
      expect(output).toContain("assistant.message agentId=left parentAgentId=research parentToolCallId=spawn-left");
      expect(output).toContain("parentId=research");
      expect(output).toContain("status=completed");
      expect(output).toContain("research credits: 494.207");
      expect(output).not.toContain("PRIVATE_");
    }
  });

  it("keeps failed child diagnostics visible but out of root-session errors and initialization", () => {
    const events = normalizeCopilotSession(copilotFailedChildCi);
    stable(events);
    expect(projectSessionResult(events)).toMatchObject({ num_turns: 1, errors: undefined });
    expect(observedSessionModel(events)).toBe("gpt-5.6-luna");
    expect(byType(events, "session.result").find(event => event.agentId === "failed-child" && event.data.errors).data.errors[0]).toMatchObject({ statusCode: 400 });
    for (const output of [generatePlainTextSummary(compact(events)), generateCopilotCliStyleSummary(compact(events))]) {
      expect(output).toContain("status=failed");
      expect(output).toContain("Completed using the available evidence.");
    }
  });

  it("does not let child starts, results or accounting snapshots overwrite the parent", () => {
    const rootMetrics = { main: { totalNanoAiu: 1e9 }, child: { totalNanoAiu: 2e9 } };
    const events = normalizeCopilotSession([
      native("session.start", undefined, { sessionId: "root", selectedModel: "root-model", context: { cwd: "/root" } }),
      native("subagent.started", "child", { toolCallId: "spawn", agentName: "explore" }),
      native("session.start", "child", { sessionId: "child-session", selectedModel: "child-model", context: { cwd: "/child" } }),
      native("session.result", undefined, { usage: { input_tokens: 10 }, numTurns: 2, agentMetrics: rootMetrics }),
      native("session.result", "child", { usage: { input_tokens: 999 }, numTurns: 99, agentMetrics: { child: { totalNanoAiu: 999e9 } }, errors: ["child failure"] }),
      native("assistant.turn_end", undefined, { turnId: "root-turn" }),
    ]);
    expect(projectSessionInitialization(events)).toMatchObject({ model: "root-model", session_id: "root", cwd: "/root" });
    expect(observedSessionModel([null, ...events])).toBe("root-model");
    expect(projectSessionResult(events)).toMatchObject({ usage: { input_tokens: 10 }, num_turns: 2, errors: undefined });
    expect(renderSubagentSummary(events).join("\n")).toContain("child credits: 2.000");
    expect(renderSubagentSummary(events).join("\n")).not.toContain("999.000");
    expect(events.find(event => event.type === "assistant.turn_end" && !event.agentId).data.agentId).toBeUndefined();
    stable(events);
  });

  it("correlates modern agent IDs despite missing deprecated parent-tool markers", () => {
    const events = normalizeCopilotSession([
      native("assistant.message", "child", { content: "", messageId: "m", parentToolCallId: "spawn", toolRequests: [{ toolCallId: "tool", name: "lookup", arguments: false }] }),
      native("tool.execution_start", "child", { toolCallId: "tool", toolName: "lookup", arguments: false }),
      native("tool.execution_complete", "child", { toolCallId: "tool", parentToolCallId: "spawn", success: true }),
    ]);
    expect(byType(events, "tool.execution_start")).toHaveLength(1);
    expect(byType(events, "tool.execution_complete")[0].data.toolName).toBe("lookup");
    stable(events);
  });

  it("resolves legacy parent-tool-only events only when lifecycle evidence identifies one child", () => {
    const events = [
      native("session.start", undefined, { sessionId: "root" }),
      native("subagent.started", "child", { toolCallId: "spawn" }),
      native("assistant.message", undefined, { parentToolCallId: "spawn", content: "legacy child" }),
      native("subagent.started", "other", { toolCallId: "spawn" }),
      native("assistant.message", undefined, { parentToolCallId: "spawn", content: "ambiguous child" }),
    ];
    const contexts = sessionEventContexts(events);
    expect(contexts[2].agentId).toBe("child");
    expect(contexts[4].agentId).toBeUndefined();
    const normalized = normalizeCopilotSession(events);
    expect(byType(normalized, "assistant.message")[0].agentId).toBe("child");
    expect(byType(normalized, "assistant.message")[0].data.agentId).toBe("child");
    expect(byType(normalized, "assistant.message")[1].data.agentId).toBeUndefined();
    stable(normalized);
  });

  it("preserves opaque child extensions and distinguishes identical parent/child summaries", () => {
    const extension = native("vendor.extension", "child", { opaque: { content: "unchanged" } });
    const events = normalizeCopilotSession([
      native("session.start", undefined, { sessionId: "root" }),
      native("subagent.started", "child", { toolCallId: "spawn" }),
      native("assistant.message", undefined, { content: "Same answer" }),
      native("session.task_complete", "child", { summary: "Same answer" }),
      extension,
    ]);
    expect(byType(events, "assistant.message")).toHaveLength(2);
    expect(byType(events, "vendor.extension")[0]).toEqual(extension);
    stable(events);
  });

  it("keeps live usage cumulative within each agent instead of across siblings", () => {
    const events = normalizeCopilotSession([
      native("session.start", undefined, { sessionId: "root" }),
      native("assistant.usage", undefined, { apiCallId: "shared", inputTokens: 10, outputTokens: 1 }),
      native("assistant.usage", "left", { apiCallId: "shared", inputTokens: 20, outputTokens: 2 }),
      native("assistant.usage", "right", { apiCallId: "shared", inputTokens: 30, outputTokens: 3 }),
      native("assistant.usage", "left", { apiCallId: "second", inputTokens: 5, outputTokens: 4 }),
    ]);
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 10, output_tokens: 1 });
    const left = byType(events, "session.result").filter(event => event.agentId === "left");
    expect(left.at(-1).data.usage).toMatchObject({ input_tokens: 25, output_tokens: 6 });
    stable(events);
  });

  it("repairs historical globally counted projections from retained native evidence", () => {
    const usage = native("assistant.usage", "child", { apiCallId: "child-api", inputTokens: 20, outputTokens: 2 });
    usage.id = "usage";
    const events = normalizeCopilotSession([
      native("session.start", undefined, { sessionId: "root" }),
      native("assistant.usage", undefined, { apiCallId: "root-api", inputTokens: 10, outputTokens: 1 }),
      usage,
      { ...usage, type: "session.result", data: { ...usage.data, usage: { input_tokens: 30, output_tokens: 3 } }, copilotProjection: "assistant.usage" },
      native("assistant.turn_end", undefined, { turnId: "0" }),
      native("assistant.turn_end", "child", { turnId: "0" }),
      { type: "session.result", data: { numTurns: 2 }, copilotProjection: "finalized-turns" },
    ]);
    expect(projectSessionResult(events)).toMatchObject({ num_turns: 1, usage: { input_tokens: 10, output_tokens: 1 } });
    expect(byType(events, "session.result").find(event => event.agentId === "child" && event.data.usage).data.usage).toMatchObject({ input_tokens: 20, output_tokens: 2 });
    expect(events.filter(event => event.copilotProjection === "finalized-turns")).toHaveLength(2);
    stable(events);
    const onlyProjection = [{ type: "session.result", data: { numTurns: 3 }, copilotProjection: "finalized-turns" }];
    expect(normalizeCopilotSession(onlyProjection)).toEqual(onlyProjection);
  });

  it("gives reused raw tool IDs distinct display IDs and pairs interleaved results within their agents", () => {
    const events = [
      native("session.start", undefined, { sessionId: "root" }),
      native("tool.execution_start", "left", { toolCallId: "same", toolName: "lookup", input: { owner: "left" } }),
      native("tool.execution_start", "right", { toolCallId: "same", toolName: "lookup", input: { owner: "right" } }),
      native("tool.execution_complete", "right", { toolCallId: "same", output: "right output", success: true }),
      native("tool.execution_complete", "left", { toolCallId: "same", output: "left output", success: true }),
    ];
    const original = structuredClone(events);
    const legacy = convertCopilotEventsToLegacyLogEntries(events);
    const tools = legacy.filter(event => event.message?.content[0].type === "tool_use");
    const results = legacy.filter(event => event.message?.content[0].type === "tool_result");
    expect(new Set(tools.map(event => event.message.content[0].id)).size).toBe(2);
    for (const result of results) {
      const tool = tools.find(event => event.message.content[0].id === result.message.content[0].tool_use_id);
      expect(tool.agentId).toBe(result.agentId);
      expect(tool.message.content[0].input.owner).toBe(result.agentId);
    }
    expect(events).toEqual(original);
  });

  it("keeps ID-less same-name tools scoped and does not match an orphan against another child", () => {
    const legacy = convertCopilotEventsToLegacyLogEntries([
      native("tool.execution_start", "left", { toolName: "lookup", input: { owner: "left" } }),
      native("tool.execution_complete", "right", { toolName: "lookup", output: "orphan" }),
      native("tool.execution_complete", "left", { toolName: "lookup", output: "left output" }),
    ]);
    const tools = legacy.filter(event => event.message?.content[0].type === "tool_use");
    const results = legacy.filter(event => event.message?.content[0].type === "tool_result");
    expect(tools).toHaveLength(2);
    expect(tools.find(event => event.agentId === "right").message.content[0].orphaned).toBe(true);
    expect(results.find(event => event.agentId === "left").message.content[0].tool_use_id).toBe(tools.find(event => event.agentId === "left").message.content[0].id);
  });

  it("keeps tool pairing private when publication masks distinct agent identities to the same text", () => {
    const { redactSessionForPublication } = require("./agent_session_render.cjs");
    const events = [
      native("tool.execution_start", "secret-left", { toolCallId: "same", toolName: "lookup", input: { owner: "left" } }),
      native("tool.execution_start", "secret-right", { toolCallId: "same", toolName: "lookup", input: { owner: "right" } }),
      native("tool.execution_complete", "secret-right", { toolCallId: "same", output: "right" }),
      native("tool.execution_complete", "secret-left", { toolCallId: "same", output: "left" }),
    ];
    const published = redactSessionForPublication(events, text => text.replace(/secret-(left|right)/g, "masked"));
    const legacy = convertCopilotEventsToLegacyLogEntries(published);
    const tools = legacy.filter(event => event.message?.content[0].type === "tool_use");
    const results = legacy.filter(event => event.message?.content[0].type === "tool_result");
    expect(tools).toHaveLength(2);
    for (const result of results) {
      const block = result.message.content[0];
      expect(tools.find(event => event.message.content[0].id === block.tool_use_id).message.content[0].input.owner).toBe(block.content);
    }
    expect(JSON.stringify(legacy)).not.toContain("secret-");
  });

  it("does not collapse child reasoning into another child and accepts mixed deprecated metadata", () => {
    const events = collapseStreamedMessages([
      native("assistant.reasoning", "left", { reasoningId: "same", content: "L", delta: true, parentToolCallId: "spawn-left" }),
      native("assistant.reasoning", "left", { reasoningId: "same", content: "eft", delta: true }),
      native("assistant.reasoning", "right", { reasoningId: "same", content: "Right", delta: true }),
    ]);
    expect(events.map(event => event.data.content)).toEqual(["Left", "Right"]);
  });

  it("collects native and canonical children as one root attempt without losing provenance or duplicating tools", () => {
    const rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-nested-"));
    try {
      const relative = "sandbox/agent/logs/copilot-session-state/root/events.jsonl";
      const events = [...copilotNestedCi.slice(0, 8), native("session.start", "research", { sessionId: "child-session", selectedModel: "child-model" }), ...copilotNestedCi.slice(8)];
      fs.mkdirSync(path.dirname(path.join(rootDir, relative)), { recursive: true });
      fs.writeFileSync(path.join(rootDir, relative), events.map(JSON.stringify).join("\n") + "\n");
      const canonical = normalizeCopilotSession(events).map((event, index) => ({ ...event, provenance: { path: relative, index } }));
      fs.writeFileSync(path.join(rootDir, "agent-session.jsonl"), canonical.map(JSON.stringify).join("\n") + "\n");
      writeUnifiedSession({ rootDir, engine: "copilot" });
      const published = fs.readFileSync(path.join(rootDir, "usage/aw_session.jsonl"), "utf8");
      const unified = published.trim().split("\n").map(JSON.parse);
      expect(byType(unified, "session.collection_warning")).toEqual([]);
      expect(byType(unified, "tool.execution_start")).toHaveLength(2);
      expect(byType(unified, "subagent.started")).toHaveLength(3);
      expect(projectSessionResult(unified).num_turns).toBe(1);
      expect(byType(unified, "assistant.message").every(event => event.provenance.path === relative)).toBe(true);
      expect(scopedAgentSessions(unified)).toHaveLength(4);
      writeUnifiedSession({ rootDir, engine: "copilot" });
      expect(fs.readFileSync(path.join(rootDir, "usage/aw_session.jsonl"), "utf8")).toBe(published);
      expect(readCopilotSessions(path.join(rootDir, "sandbox/agent/logs/copilot-session-state"), parseCopilotLog)).toHaveLength(1);
    } finally {
      fs.rmSync(rootDir, { recursive: true, force: true });
    }
  });
});
