import { describe, expect, it } from "vitest";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { projectSessionResult, projectSessionInitialization, observedSessionModel, sessionContext } from "./agent_session.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { scopedAgentSessions } from "./unified_session_render.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary, MAX_STEP_SUMMARY_SIZE, convertCopilotEventsToLegacyLogEntries } from "./log_parser_shared.cjs";
import { parseClaudeLog } from "./parse_claude_log.cjs";
import { renderSubagentSummary } from "./subagent_session_render.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { subagents } from "./fixtures/claude_ci_subagents.cjs";

const compact = events => mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-stdio.log", events }]);

describe("Claude subagents and nested sessions", () => {
  it("maps actual local-agent starts and notifications to shared subagent lifecycle events", () => {
    const events = normalizeClaudeSession(subagents);
    const starts = events.filter(event => event.type === "subagent.started");
    expect(starts.map(event => event.agentId)).toEqual(["agent-a", "agent-b", "agent-grandchild"]);
    expect(starts[0].data).toMatchObject({ toolCallId: "launch-a", agentName: "general-purpose", executionMode: "background", spawnDepth: 1 });
    expect(starts[2].data).toMatchObject({ toolCallId: "launch-grandchild", parentId: "agent-a", parentToolUseId: "launch-a", spawnDepth: 2 });
    expect(events.find(event => event.type === "subagent.completed")).toMatchObject({ agentId: "agent-a", data: { toolCallId: "launch-a", totalTokens: 103, totalToolCalls: 1, durationMs: 10 } });
    expect(events.find(event => event.type === "subagent.failed")).toMatchObject({ agentId: "agent-b", data: { toolCallId: "launch-b", durationMs: 20 } });
    expect(events.filter(event => event.type === "subagent.configured").map(event => event.data.model)).toEqual(["fixture-model", "fixture-model", "fixture-model"]);
    expect(events.filter(event => event.type === "dynamicWorkflows.task_started")).toEqual([]);
    expect(starts[0].data.prompt).toBe("PRIVATE_SUBAGENT_PROMPT");
    expect(JSON.stringify(compact(events).filter(event => event.type.startsWith("subagent.")))).not.toContain("PRIVATE_");
    expect(generatePlainTextSummary(events)).not.toContain("PRIVATE_");
    expect(generatePlainTextSummary(events)).toContain("totalTokens=103 totalToolCalls=1 durationMs=10");
    expect(compact(events).find(event => event.type === "subagent.failed").data).toMatchObject({ totalTokens: 203, totalToolCalls: 1 });
  });

  it.each(["completed", "failed", "stopped"].flatMap(status => ["native start", "normalized start", "launcher only"].map(registration => [status, registration])))(
    "preserves nested %s context from %s despite incomplete observations",
    (status, registration) => {
      const start = subagents.find(record => record.task_id === "agent-grandchild");
      const launch = subagents.find(record => record.tool_use_result?.agentId === "agent-grandchild");
      const registered =
        registration === "native start"
          ? start
          : registration === "launcher only"
            ? launch
            : { type: "subagent.started", agentId: start.task_id, data: { task_type: "local_agent", sessionId: start.session_id, parentToolUseId: start.parent_tool_use_id, agentId: "agent-a", toolCallId: start.tool_use_id } };
      const notification = {
        type: "system",
        subtype: "task_notification",
        session_id: start.session_id,
        task_type: "local_agent",
        task_id: start.task_id,
        parent_tool_use_id: undefined,
        tool_use_id: undefined,
        agentId: undefined,
        status,
        uuid: "notification-id",
        usage: { total_tokens: 13, tool_uses: 2, duration_ms: 7 },
      };
      const records = [
        subagents.find(record => record.task_id === "agent-a"),
        registered,
        { type: "user", session_id: start.session_id, tool_use_result: { agentId: start.task_id, resolvedModel: "observed-model" }, message: { content: [{ type: "tool_result", content: "Launched" }] } },
        notification,
      ];
      const original = structuredClone(records);
      const events = normalizeClaudeSession(records);
      const lifecycle = events.filter(event => event.type === (status === "failed" ? "subagent.failed" : "subagent.completed"));
      expect(lifecycle).toHaveLength(1);
      expect(sessionContext(lifecycle[0])).toStrictEqual({ sessionId: start.session_id, parentToolUseId: "launch-a", agentId: "agent-a" });
      expect(lifecycle[0].agentId).toBe("agent-grandchild");
      expect(lifecycle[0].data).toMatchObject({ toolCallId: "launch-grandchild", status, totalTokens: 13, totalToolCalls: 2, durationMs: 7 });
      expect(lifecycle[0].data.cancelled).toBe(status === "stopped" ? true : undefined);
      expect(lifecycle[0].uuid).toBe(notification.uuid);
      expect(lifecycle[0].usage).toStrictEqual(notification.usage);
      expect(lifecycle[0].parent_tool_use_id).toBeUndefined();
      const configured = events.filter(event => event.type === "subagent.configured").at(-1);
      expect(sessionContext(configured)).toStrictEqual(sessionContext(lifecycle[0]));
      expect(configured.data.toolCallId).toBe("launch-grandchild");
      expect(configured.data.model).toBe("observed-model");
      expect(records).toStrictEqual(original);
      const roundTrip = JSON.parse(JSON.stringify(events));
      expect(JSON.parse(JSON.stringify(normalizeClaudeSession(JSON.parse(JSON.stringify(records)))))).toStrictEqual(roundTrip);
      expect(normalizeClaudeSession(events)).toStrictEqual(events);
      expect(normalizeClaudeSession(roundTrip)).toStrictEqual(roundTrip);
      const projected = compact(events);
      const grouped = scopedAgentSessions(projected).find(group => group.label.includes("unresolved subagent") && group.label.includes("parentToolUseId=launch-a"));
      expect(grouped.events.find(event => event.type === lifecycle[0].type).agentId).toBe("agent-grandchild");
      for (const [kind, session] of [
        ["agent", events],
        ["unified", projected],
      ]) {
        const validate = createSessionValidator(kind).event;
        for (const event of session) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
      }
    }
  );

  it.each(
    [
      [
        { parent_tool_use_id: "launch-b", agentId: "explicit-caller" },
        { parentToolUseId: "launch-b", agentId: "explicit-caller" },
      ],
      [{ parent_tool_use_id: "launch-b" }, { parentToolUseId: "launch-b", agentId: "agent-b" }],
      [{ parent_tool_use_id: null }, { parentToolUseId: null }],
      [{ data: { parentToolUseId: null } }, { parentToolUseId: null }],
      [{ parent_tool_use_id: "unknown" }, { parentToolUseId: "unknown" }],
      [{ data: { parentToolUseId: "launch-b", agentId: "payload-caller" } }, { parentToolUseId: "launch-b", agentId: "payload-caller" }],
    ].flatMap(([context, expected]) => [undefined, "local_agent"].map(task_type => [{ ...context, task_type }, expected]))
  )("lets explicit notification context %j override stored caller scope", (context, expected) => {
    const start = subagents.find(record => record.task_id === "agent-grandchild");
    const events = normalizeClaudeSession([
      ...subagents.filter(record => record.subtype === "task_started"),
      { ...start, agentId: "agent-a" },
      { type: "system", subtype: "task_notification", session_id: start.session_id, task_id: start.task_id, status: "completed", ...context },
    ]);
    const completed = events.find(event => event.type === "subagent.completed");
    expect(completed.agentId).toBe("agent-grandchild");
    expect(sessionContext(completed)).toStrictEqual({ sessionId: start.session_id, ...expected });
    expect(sessionContext(compact(events).find(event => event.type === "subagent.completed"))).toStrictEqual(sessionContext(completed));
  });

  it("does not infer ambiguous callers or mix repeated task IDs across sessions", () => {
    const records = ["first", "second"].flatMap(session_id => [
      { type: "system", subtype: "task_started", session_id, task_id: "caller-a", tool_use_id: "shared-launcher", task_type: "local_agent" },
      ...(session_id === "first" ? [{ type: "system", subtype: "task_started", session_id, task_id: "caller-b", tool_use_id: "shared-launcher", task_type: "local_agent" }] : []),
      { type: "system", subtype: "task_started", session_id, parent_tool_use_id: "shared-launcher", task_id: "repeated-target", tool_use_id: `${session_id}-launcher`, task_type: "local_agent" },
      { type: "system", subtype: "task_notification", session_id, task_id: "repeated-target", status: "completed" },
    ]);
    const completed = normalizeClaudeSession(records).filter(event => event.type === "subagent.completed");
    expect(completed.map(sessionContext)).toStrictEqual([
      { sessionId: "first", parentToolUseId: "shared-launcher" },
      { sessionId: "second", parentToolUseId: "shared-launcher", agentId: "caller-a" },
    ]);
    expect(completed.map(event => event.data.toolCallId)).toEqual(["first-launcher", "second-launcher"]);
    expect(completed.map(event => event.agentId)).toEqual(["repeated-target", "repeated-target"]);
  });

  it("retains caller scope on compact tools, results, refusals and initialization", () => {
    const records = [
      ...subagents,
      { type: "system", subtype: "init", session_id: "child-session", parent_tool_use_id: "launch-a", model: "child-model" },
      { type: "assistant", session_id: "child-session", parent_tool_use_id: "launch-a", message: { id: "refused", stop_reason: "refusal", content: [] } },
      { type: "result", session_id: "child-session", parent_tool_use_id: "launch-a", usage: { input_tokens: 999 }, errors: ["child error"] },
    ];
    const events = compact(normalizeClaudeSession(records));
    expect(events.find(event => event.type === "session.init" && event.data.sessionId === "child-session").data.parentToolUseId).toBe("launch-a");
    expect(events.find(event => event.type === "assistant.refusal").data).toMatchObject({ sessionId: "child-session", parentToolUseId: "launch-a" });
    const childTools = events.filter(event => event.type.startsWith("tool.execution") && event.data.parentToolUseId === "launch-a");
    expect(childTools).toHaveLength(4);
    expect(childTools.every(event => event.data.sessionId === "nested-session")).toBe(true);
    expect(projectSessionResult(events)).toMatchObject({ usage: { input_tokens: 7, output_tokens: 3 } });
    expect(projectSessionResult(events).errors).toBeUndefined();
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("retains per-child partial usage without adding it to root accounting", () => {
    const events = normalizeClaudeSession(subagents);
    const results = events.filter(event => event.type === "session.result");
    expect(results).toHaveLength(4);
    for (const [parentToolUseId, input_tokens] of [
      ["launch-a", 100],
      ["launch-b", 200],
      ["launch-grandchild", 300],
    ]) {
      expect(results.find(event => event.data.parentToolUseId === parentToolUseId).data).toMatchObject({ partial: true, usage: { input_tokens, output_tokens: 3 } });
    }
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 7, output_tokens: 3 });
    expect(projectSessionResult(compact(events)).usage).toMatchObject({ input_tokens: 7, output_tokens: 3 });
    const roundTrip = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(roundTrip)).toEqual(roundTrip);
    expect(normalizeClaudeSession(events)).toEqual(events);
    const validate = createSessionValidator("agent").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("separates nested conversations and correlates reused tool IDs only within their caller", () => {
    const events = compact(normalizeClaudeSession(subagents));
    const groups = scopedAgentSessions(events);
    expect(groups).toHaveLength(4);
    for (const [parent, label] of [
      ["launch-a", "child-a"],
      ["launch-b", "child-b"],
      ["launch-grandchild", "grandchild"],
    ]) {
      const group = groups.find(group => group.label.includes(`parentToolUseId=${parent}`));
      expect(group.events.find(event => event.type === "assistant.message").data.content).toBe(`${label} answer\n`);
      const output = generatePlainTextSummary(group.events);
      expect(output).toContain(`${label} output`);
      expect(output).not.toContain("root output");
      expect(output).toContain(`Tokens: ${label === "child-a" ? 103 : label === "child-b" ? 203 : 303} total`);
    }
    const legacy = convertCopilotEventsToLegacyLogEntries(normalizeClaudeSession(subagents));
    const blocks = legacy.flatMap(event => event.message?.content ?? []);
    const outputs = new Map(blocks.filter(block => block.type === "tool_result").map(block => [block.tool_use_id, block.content]));
    const commands = blocks.filter(block => block.type === "tool_use" && block.name === "Bash");
    expect(commands).toHaveLength(4);
    expect(commands.map(tool => [tool.input.command, outputs.get(tool.id)])).toEqual([
      ["printf 'child-a'", "child-a output"],
      ["printf 'child-b'", "child-b output"],
      ["printf 'grandchild'", "grandchild output"],
      ["printf 'root'", "root output"],
    ]);
    const rawEvents = normalizeClaudeSession(subagents);
    expect(generatePlainTextSummary(rawEvents).match(/Agent conversation:/g)).toHaveLength(4);
    const markdown = parseClaudeLog(JSON.stringify(subagents)).markdown;
    expect(markdown.match(/Agent conversation:/g)).toHaveLength(4);
    expect(markdown).not.toContain("PRIVATE_");
    expect(markdown.match(/<summary>Information<\/summary>/g)).toHaveLength(4);
    expect(rawEvents.filter(event => event.type === "tool.execution_start" && event.data.toolName === "Bash").map(event => event.data.toolCallId)).toEqual(Array(4).fill("reused-tool"));
  });

  it("isolates reused launcher and message IDs across independent root sessions", () => {
    const records = [...subagents, ...subagents.map(record => ({ ...record, session_id: "second-session" }))];
    const original = structuredClone(records);
    const events = normalizeClaudeSession(records);
    expect(records).toEqual(original);
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 14, output_tokens: 6 });
    expect(projectSessionResult(compact(events)).usage).toMatchObject({ input_tokens: 14, output_tokens: 6 });
    const groups = scopedAgentSessions(compact(events));
    expect(groups).toHaveLength(8);
    expect(groups.filter(group => group.label.includes("sessionId=nested-session"))).toHaveLength(4);
    expect(groups.filter(group => group.label.includes("sessionId=second-session"))).toHaveLength(4);
  });

  it("pairs ID-less results by name only inside the same caller scope", () => {
    const scopes = [null, "child-a", "child-b"];
    const events = [
      ...scopes.map(parentToolUseId => ({ type: "tool.execution_start", data: { sessionId: "root", parentToolUseId, toolName: "Bash", input: { command: parentToolUseId ?? "root" } } })),
      ...scopes.toReversed().map(parentToolUseId => ({ type: "tool.execution_complete", data: { sessionId: "root", parentToolUseId, toolName: "Bash", output: parentToolUseId ?? "root" } })),
    ];
    const blocks = convertCopilotEventsToLegacyLogEntries(events).flatMap(event => event.message?.content ?? []);
    const outputs = new Map(blocks.filter(block => block.type === "tool_result").map(block => [block.tool_use_id, block.content]));
    for (const tool of blocks.filter(block => block.type === "tool_use")) expect(outputs.get(tool.id)).toBe(tool.input.command);
  });

  it("reconciles interleaved streamed responses with reused IDs separately", () => {
    const chunk = (parent_tool_use_id, event) => ({ type: "stream_event", session_id: "shared", parent_tool_use_id, event });
    const records = [
      ...[null, "child"].map(parent => chunk(parent, { type: "message_start", message: { id: "reused", model: "fixture-model", content: [], usage: { input_tokens: 1 } } })),
      ...[null, "child"].map(parent => chunk(parent, { type: "content_block_start", index: 0, content_block: { type: "text", text: parent ? "child " : "root " } })),
      ...[null, "child"].map(parent => chunk(parent, { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "answer" } })),
      ...[null, "child"].map(parent => chunk(parent, { type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 2 } })),
      ...[null, "child"].map(parent => chunk(parent, { type: "message_stop" })),
    ];
    const events = compact(normalizeClaudeSession(records));
    const groups = scopedAgentSessions(events);
    expect(groups).toHaveLength(2);
    expect(
      groups.map(group =>
        group.events
          .filter(event => event.type === "assistant.message")
          .map(event => event.data.content)
          .join("")
      )
    ).toEqual(["root answer", "child answer"]);
    expect(generatePlainTextSummary(events)).toContain("child answer");
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 1, output_tokens: 2 });
  });

  it("bounds both nested summary views including oversized multibyte child text", () => {
    const records = [...subagents, { type: "assistant", session_id: "nested-session", parent_tool_use_id: "launch-a", message: { id: "large", content: [{ type: "text", text: "\u754c".repeat(MAX_STEP_SUMMARY_SIZE) }] } }];
    const markdown = parseClaudeLog(JSON.stringify(records)).markdown;
    expect(Buffer.byteLength(markdown)).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
    expect(markdown).toContain("size limit reached");
    const cli = generateCopilotCliStyleSummary(compact(normalizeClaudeSession(records)), { maxBytes: 8192 });
    expect(Buffer.byteLength(cli)).toBeLessThanOrEqual(8192);
  });

  it("retains child terminal accounting without synthesizing overlapping partial usage", () => {
    const records = [...subagents, { type: "result", session_id: "nested-session", parent_tool_use_id: "launch-a", subtype: "error_max_turns", usage: { input_tokens: 111 } }];
    const events = compact(normalizeClaudeSession(records));
    const child = events.filter(event => event.type === "session.result" && event.data.parentToolUseId === "launch-a");
    expect(child).toHaveLength(1);
    expect(child[0].data.usage).toMatchObject({ inputTokens: 111, outputTokens: 3 });
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 7, output_tokens: 3 });
    expect(parseClaudeLog(JSON.stringify(records)).maxTurnsHit).toBe(false);
  });

  it("does not misclassify local-agent progress, cancellation or errors as dynamic workflows", () => {
    const events = normalizeClaudeSession([
      ...subagents,
      { type: "system", subtype: "task_progress", session_id: "nested-session", task_id: "agent-a", tool_use_id: "launch-a", status: "running" },
      { type: "system", subtype: "task_notification", session_id: "nested-session", task_id: "agent-a", status: "stopped", usage: { total_tokens: 0 } },
      { type: "system", subtype: "task_notification", session_id: "nested-session", task_id: "agent-b", status: "failed", error: "child-only error" },
      { type: "system", subtype: "task_notification", session_id: "nested-session", task_id: "orphan", task_type: "local_agent", status: "completed" },
      { type: "user", session_id: "nested-session", message: { content: [] }, tool_use_result: { agentId: "agent-a", resolvedModel: "fixture-model" } },
    ]);
    expect(events.find(event => event.type === "claude.system" && event.data.subtype === "task_progress").data.task_id).toBe("agent-a");
    expect(events.filter(event => event.type.startsWith("dynamicWorkflows."))).toEqual([]);
    expect(events.find(event => event.type === "subagent.completed" && event.data.cancelled)).toMatchObject({ data: { toolCallId: "launch-a", totalTokens: 0, cancelled: true } });
    expect(events.find(event => event.type === "subagent.completed" && event.agentId === "orphan")).toBeDefined();
    expect(events.find(event => event.type === "subagent.failed" && event.data.error)).toMatchObject({ data: { error: "child-only error" } });
    expect(projectSessionResult(events).errors).toBeUndefined();
    expect(renderSubagentSummary(events).join("\n")).toContain("status=stopped");
  });

  it("does not let unidentified nested initialization or metrics replace root observations", () => {
    const events = [
      ...normalizeClaudeSession(subagents),
      { type: "session.result", data: { agentMetrics: { "agent-a": { totalNanoAiu: 1e9 } } } },
      { type: "session.init", data: { sourceEngine: "claude", sessionId: "child-session", parentToolUseId: "unseen-launcher", model: "child-model" } },
      { type: "session.result", data: { sourceEngine: "claude", sessionId: "child-session", parentToolUseId: "unseen-launcher", agentMetrics: { wrong: { totalNanoAiu: 2e9 } } } },
    ];
    expect(projectSessionInitialization(events).model).toBe("fixture-model");
    expect(observedSessionModel(events)).toBe("fixture-model");
    expect(renderSubagentSummary(events).join("\n")).toContain("agent-a credits: 1.000");
    expect(renderSubagentSummary(events).join("\n")).not.toContain("agentId=wrong");
  });

  it("keeps partial orphan child diagnostics isolated when no lifecycle start is present", () => {
    const events = compact(
      normalizeClaudeSession([
        { type: "result", session_id: "root", usage: { input_tokens: 1 } },
        { type: "assistant", session_id: "root", parent_tool_use_id: "unseen-parent", error: "server_error", message: { content: [] } },
      ])
    );
    expect(events.find(event => event.type === "session.result" && event.data.errors).data.parentToolUseId).toBe("unseen-parent");
    expect(projectSessionResult(events).errors).toBeUndefined();
    expect(projectSessionResult(events).usage.input_tokens).toBe(1);
  });

  it("accepts malformed neighboring records and undefined optional context without changing scope", () => {
    const record = { type: 42, session_id: "root", parent_tool_use_id: "child", data: { sessionId: undefined, parentToolUseId: undefined } };
    expect(sessionContext(record)).toEqual({ sessionId: "root", parentToolUseId: "child" });
    expect(sessionContext(JSON.parse(JSON.stringify(record)))).toEqual(sessionContext(record));
    expect(() => normalizeClaudeSession([record, ...subagents])).not.toThrow();
  });

  it.each([
    { sessionId: null, parentToolUseId: null, session_id: "ignored", parent_tool_use_id: "ignored" },
    { session_id: null, parent_tool_use_id: null, data: { session_id: "ignored", parent_tool_use_id: "ignored" } },
  ])("retains explicit null scope in the shared context helper for %j", source => {
    expect(sessionContext(source)).toStrictEqual({ sessionId: null, parentToolUseId: null });
    expect(sessionContext(JSON.parse(JSON.stringify(source)))).toStrictEqual(sessionContext(source));
  });
});
