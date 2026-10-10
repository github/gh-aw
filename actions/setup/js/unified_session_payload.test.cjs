import { describe, expect, it } from "vitest";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary, convertCopilotEventsToLegacyLogEntries } from "./log_parser_shared.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";
import { normalizeCodexSession } from "./codex_session.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { normalizeCopilotSession } from "./copilot_session.cjs";
import { normalizeGeminiSession } from "./gemini_session.cjs";
import { collapseStreamedMessages } from "./agent_session_render.cjs";
import { reconcileSessionUsage, selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { dynamicWorkflow } from "./fixtures/claude_dynamic_workflow.cjs";
import { DYNAMIC_WORKFLOW_EVENT_TYPES } from "./dynamic_workflow_session.cjs";

describe("essential unified session payloads", () => {
  it.each([401, 503, 0, null])("retains observed session-error status codes through projection and schema validation (%s)", statusCode => {
    const source = { type: "session.error", data: { errorType: "provider", message: "Observed failure.", statusCode, opaque: "PRIVATE_ERROR_CONTEXT" } };
    const original = structuredClone(source);
    const [event] = mergeSessionSources([{ component: "agent", phase: "agent", path: "errors.jsonl", events: [source] }]);
    expect(event.data).toEqual({ errorType: "provider", message: "Observed failure.", statusCode });
    expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
    const validate = createSessionValidator("unified").event;
    expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(normalizeUnifiedSessionEvent({ type: "session.error", data: { message: "No status observed." } }).data).not.toHaveProperty("statusCode");
    expect(source).toEqual(original);
  });

  it("retains declared partial-input and per-step usage evidence", () => {
    const events = [
      { type: "tool.execution_start", data: { sessionId: "session", stepIndex: 0, toolName: "bash", input: { command: "preview..." }, inputTruncated: true } },
      { type: "usage.report", data: { sessionId: "session", stepIndex: 0, usage: { input_tokens: 0, cache_read_input_tokens: 1 } } },
      { type: "tool.execution_complete", data: { sessionId: "session", stepIndex: 0, status: "declined", exitCode: null } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "steps.jsonl", events }]);
    expect(unified[0].data).toEqual(events[0].data);
    expect(unified[1].data).toEqual({ sessionId: "session", stepIndex: 0, usage: { inputTokens: 0, cacheReadInputTokens: 1 } });
    expect(unified[2].data).toEqual(events[2].data);
    for (const [kind, trace] of [
      ["agent", events],
      ["unified", unified],
    ]) {
      const validate = createSessionValidator(kind).event;
      for (const event of trace) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    }
  });

  it("preserves native step correlation without fabricating stored tool identifiers", () => {
    const events = [
      { type: "tool.execution_start", data: { sessionId: "session", stepIndex: 0, toolName: "lookup", input: "first" } },
      { type: "tool.execution_start", data: { sessionId: "session", stepIndex: 1, toolName: "lookup", input: "second" } },
      { type: "tool.execution_complete", data: { sessionId: "session", stepIndex: 1, toolName: "lookup", output: "second result", success: true } },
      { type: "tool.execution_complete", data: { sessionId: "session", stepIndex: 0, toolName: "lookup", output: "first result", success: false } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "steps.jsonl", events }]);
    expect(unified.map(event => event.data.stepIndex)).toEqual([0, 1, 1, 0]);
    expect(unified.every(event => !Object.hasOwn(event.data, "toolCallId"))).toBe(true);
    const display = convertCopilotEventsToLegacyLogEntries(unified);
    expect(display[2].message.content[0].tool_use_id).toBe(display[1].message.content[0].id);
    expect(display[3].message.content[0].tool_use_id).toBe(display[0].message.content[0].id);
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("retains diagnostic severity and refusal correlation without duplicate native envelopes", () => {
    const events = [
      { type: "session.error", agentId: "child", parentToolCallId: "parent", data: { severity: "warning", status: "warning", code: 0, error: null, message: "", content: "exact\n", sourceType: "error", opaque: "omit" } },
      {
        type: "assistant.refusal",
        agentId: "child",
        data: { reason: "refusal", content: "exact\n", messageId: "message", model: "model", apiCallId: "request", interactionId: "interaction", turnId: "turn", parentToolCallId: null, opaque: "omit" },
      },
      { type: "mcp.event", data: { event: "extension_start_failure", serverName: "github", status: "error", opaque: "omit" } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "diagnostics.jsonl", events }]);
    expect(unified[0].data).toEqual({ severity: "warning", status: "warning", code: 0, error: null, message: "", content: "exact\n", sourceType: "error", agentId: "child", parentToolCallId: "parent" });
    expect(unified[1].data).toEqual({ reason: "refusal", content: "exact\n", messageId: "message", model: "model", apiCallId: "request", interactionId: "interaction", turnId: "turn", parentToolCallId: null, agentId: "child" });
    expect(unified[2].data).toEqual({ event: "extension_start_failure", serverName: "github", status: "error" });
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it.each([
    [{ data: { parentToolCallId: "parent" } }, { parentToolCallId: "parent" }],
    [{ parentToolCallId: "parent", data: {} }, { parentToolCallId: "parent" }],
    [{ data: { parentToolCallId: null } }, { parentToolCallId: null }],
    [{ data: { parentToolCallId: "parent", parentToolUseId: "observed-parent" } }, { parentToolCallId: "parent", parentToolUseId: "observed-parent" }],
    [{ data: { parent_tool_use_id: "parent" } }, { parentToolUseId: "parent" }],
    [{ data: { parentToolCallId: "parent", parentToolUseId: null } }, { parentToolCallId: "parent", parentToolUseId: null }],
  ])("retains observed parent identities without a derived duplicate: %j", (scope, expected) => {
    const event = { type: "session.error", ...scope, data: { message: "exact\n", statusCode: 401, ...scope.data } };
    const projected = normalizeUnifiedSessionEvent(event);
    expect(projected.data).toEqual({ message: "exact\n", statusCode: 401, ...expected });
    expect(normalizeUnifiedSessionEvent(projected)).toEqual(projected);
    const validate = createSessionValidator("unified").event;
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "diagnostics.jsonl", events: [event] }]);
    expect(validate(unified[0]), JSON.stringify(validate.errors)).toBe(true);
    expect(event.data).toEqual({ message: "exact\n", statusCode: 401, ...scope.data });
  });

  it("compacts Copilot terminal transport observations without repeating accounting or textual answers", () => {
    const events = [
      {
        type: "session.shutdown",
        data: {
          shutdownType: "error",
          errorReason: "interrupted",
          totalPremiumRequests: 0,
          totalNanoAiu: 0,
          totalApiDurationMs: 0,
          sessionStartTime: 0,
          currentModel: "model",
          modelMetrics: { duplicated: true },
          tokenDetails: { duplicated: true },
          codeChanges: { duplicated: true },
        },
      },
      { type: "session.task_complete", data: { success: false, summary: "already mapped answer\n" } },
      { type: "session.task_complete", data: { success: true, summary: null } },
    ];
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "copilot.jsonl", events }]);
    expect(unified[0].data).toEqual({ shutdownType: "error", errorReason: "interrupted", premiumRequests: 0, totalNanoAiu: 0, totalApiDurationMs: 0, sessionStartTime: 0, currentModel: "model" });
    expect(unified[1].data).toEqual({ success: false });
    expect(unified[2].data).toEqual({ success: true, summary: null });
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("preserves completion-only dynamic workflow launch metadata without a tool start", () => {
    const record = dynamicWorkflow.find(record => record.tool_use_result);
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "partial.jsonl", events: normalizeClaudeSession([record]) }]);
    const launch = events.find(event => event.type === "tool.execution_complete");
    expect(launch.data).toMatchObject({
      taskId: "dynamic-task",
      taskType: "local_workflow",
      workflowName: "smoke-claude-dynamic",
      workflowRunId: "dynamic-run",
      status: "async_launched",
      toolCallId: "workflow-tool",
      success: true,
    });
    expect(launch.data).not.toHaveProperty("toolName");
    expect(generatePlainTextSummary(events)).toContain("[launch succeeded; workflow outcome pending]");
    expect(createSessionValidator("unified").event(launch)).toBe(true);
  });

  it("enforces the dynamic workflow privacy projection in the published schema", () => {
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent.jsonl", events: normalizeClaudeSession(dynamicWorkflow) }]);
    const validate = createSessionValidator("unified").event;
    const launch = events.find(event => event.type === "tool.execution_complete");
    expect(validate(launch), JSON.stringify(validate.errors)).toBe(true);
    for (const type of Object.values(DYNAMIC_WORKFLOW_EVENT_TYPES)) {
      const event = events.find(event => event.type === type);
      expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
      for (const field of ["prompt", "description", "summary", "workflow_progress"]) {
        expect(validate({ ...event, data: { ...event.data, [field]: "PRIVATE_NATIVE_FIELD" } })).toBe(false);
      }
      expect(validate({ ...event, data: { ...event.data, tasks: [{ taskId: "task", prompt: "PRIVATE_TASK_SCRIPT" }] } })).toBe(false);
      expect(validate({ ...event, data: { ...event.data, workflowProgress: [{ agentId: "agent", promptPreview: "PRIVATE_AGENT_PROMPT" }] } })).toBe(false);
      expect(validate({ ...event, data: { ...event.data, usage: { totalTokens: 0, summary: "PRIVATE_USAGE" } } })).toBe(false);
    }
  });

  it("projects engine-independent dynamic workflow events without native Claude envelopes", () => {
    const source = [
      {
        type: "tool.execution_complete",
        data: { toolName: "run_dynamic_workflow", toolCallId: "launch", success: true, status: "async_launched", taskId: "task", taskType: "dynamic_workflow", workflowName: "example", workflowRunId: "run" },
      },
      { type: "dynamicWorkflows.task_started", data: { taskId: "task", toolCallId: "launch", taskType: "dynamic_workflow", workflowName: "example", sessionId: "session", prompt: "PRIVATE_SCRIPT" } },
      {
        type: "dynamicWorkflows.task_progress",
        data: {
          taskId: "task",
          toolCallId: "launch",
          usage: { totalTokens: 0, toolUses: 0, durationMs: 0 },
          workflowProgress: [{ type: "workflow_agent", index: 0, phaseIndex: 0, agentId: "agent", state: "done", promptPreview: "PRIVATE_AGENT_PROMPT" }],
        },
      },
      { type: "dynamicWorkflows.task_updated", data: { taskId: "task", status: "completed" } },
      { type: "dynamicWorkflows.task_notification", data: { taskId: "task", toolCallId: "launch", status: "completed", summary: "PRIVATE_SUMMARY" } },
      { type: "dynamicWorkflows.background_tasks_changed", data: { tasks: [{ taskId: "task", taskType: "dynamic_workflow", status: "completed", description: "PRIVATE_DESCRIPTION" }] } },
    ];
    const original = structuredClone(source);
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "other-engine.jsonl", events: source }]);
    expect(events.map(event => event.type)).toEqual(source.map(event => event.type));
    expect(events[0].data).toEqual(source[0].data);
    expect(events[2].data).toEqual({
      taskId: "task",
      toolCallId: "launch",
      usage: { totalTokens: 0, toolUses: 0, durationMs: 0 },
      workflowProgress: [{ type: "workflow_agent", index: 0, phaseIndex: 0, agentId: "agent", state: "done" }],
    });
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    for (const event of events) expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("workflowName=example workflowRunId=run [launch succeeded; workflow outcome pending]");
      expect(output).toContain("dynamicWorkflows.task_notification taskId=task toolCallId=launch status=completed");
      expect(output).not.toContain("PRIVATE_");
      expect(output).not.toContain("claude.");
    }
    expect(source).toEqual(original);
  });

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
    expect(compact).toEqual({ type: "assistant.message", id: "message", parentId: null, timestamp: message.timestamp, data: { content, model: "duplicated" } });
    expect(Buffer.byteLength(JSON.stringify(compact))).toBeLessThan(Buffer.byteLength(JSON.stringify(message)) / 2);
    expect(message).toEqual(original);
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it.each(["assistant.message", "assistant.reasoning"])("retains supplied %s stream boundaries without copying opaque payloads", type => {
    const source = {
      type,
      session_id: "session",
      parent_tool_use_id: "parent-tool",
      agentId: "child",
      message: { id: "message", content: "PRIVATE_NATIVE_SNAPSHOT" },
      data: { content: "exact text", delta: true, partial: false, contentIndex: 0, channel: "channel", opaque: "PRIVATE_EXTENSION" },
    };
    const compact = normalizeUnifiedSessionEvent(source);
    expect(compact.data).toEqual({ content: "exact text", delta: true, partial: false, contentIndex: 0, channel: "channel", sessionId: "session", parentToolUseId: "parent-tool", agentId: "child", messageId: "message" });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
    expect(JSON.stringify(compact)).not.toContain("PRIVATE_");
  });

  it("keeps Copilot, Claude and Gemini streaming evidence through collection and compaction", () => {
    const claude = event => ({ type: "stream_event", session_id: "session", event });
    const traces = [
      normalizeCopilotSession(["Hello", " ", "world."].map((deltaContent, index) => ({ type: "assistant.message_delta", id: `event-${index}`, data: { messageId: "answer", deltaContent } }))),
      normalizeClaudeSession([
        claude({ type: "message_start", message: { id: "answer" } }),
        claude({ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } }),
        ...["Hello", " ", "world."].map(text => claude({ type: "content_block_delta", index: 0, delta: { type: "text_delta", text } })),
        claude({ type: "message_stop" }),
      ]),
      normalizeGeminiSession(["Hello", " ", "world."].map((content, timestamp) => ({ type: "message", role: "assistant", timestamp, delta: true, content }))),
    ];
    for (const events of traces) {
      const compact = mergeSessionSources([{ component: "agent", phase: "agent", path: "stream.jsonl", events }]);
      const projected = collapseStreamedMessages(compact);
      expect(projected.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["Hello world."]);
      const output = generatePlainTextSummary(compact);
      expect(output.match(/agent\/agent assistant\.message Hello world\./g)).toHaveLength(1);
    }
  });

  it.each([
    ["tool.execution_start", { toolCallId: "call", parameters: false, input: 0, command: "", text: "duplicate" }, { toolCallId: "call", input: 0, command: "" }],
    ["tool.execution_start", { toolName: "bash", arguments: { command: "cat restricted-file" } }, { toolName: "bash", input: { command: "cat restricted-file" } }],
    ["tool.execution_complete", { result: false, output: null, is_error: true, duration_ms: 0, exit_code: 1, metadata: "duplicate" }, { output: null, result: false, isError: true, durationMs: 0, exitCode: 1 }],
    ["tool.execution_complete", { toolCallId: "declined", status: "declined", exit_code: null }, { toolCallId: "declined", status: "declined", exitCode: null }],
    ["session.init", { sourceEngine: "copilot", model: "fixture", session_id: "session", tools: Array(100).fill("large descriptor") }, { sourceEngine: "copilot", model: "fixture", sessionId: "session" }],
    ["firewall.http_access", { domain: "example.com", http_status: 0, squid_request_status: "DENIED", credentials: "omit" }, { host: "example.com", status: 0, decision: "DENIED" }],
    ["firewall.steering", { eventName: "token_steering", message: "budget warning", reason: null, body: "omit" }, { event: "token_steering", message: "budget warning", reason: null }],
    ["mcp.tool_call", { tool_call_id: "call", tool_name: "lookup", duration: 5, input_size: 0, output_size: 10, status: "error" }, { toolCallId: "call", toolName: "lookup", durationMs: 5, inputSize: 0, outputSize: 10, status: "error" }],
    ["execution.result", { exit_code: 0, duration_ms: 0, outcome: "success", extra: "omit" }, { exitCode: 0, durationMs: 0, outcome: "success" }],
    [
      "detection.result",
      { job_result: "success", conclusion: "warning", reason: "threat_detected", secret_leak: false, prompt_injection: false, malicious_patch: true, reasons: ["PRIVATE_REASON"], raw: "omit" },
      { jobResult: "success", conclusion: "warning", reason: "threat_detected", secretLeak: false, promptInjection: false, maliciousPatch: true },
    ],
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

  it("keeps canonical detection values, including false flags and an empty categorical reason", () => {
    const compact = normalizeUnifiedSessionEvent({
      type: "detection.result",
      data: { jobResult: "failure", job_result: "success", conclusion: "failure", reason: "", promptInjection: false, prompt_injection: true, secretLeak: false, maliciousPatch: false },
    });
    expect(compact.data).toEqual({ jobResult: "failure", conclusion: "failure", reason: "", promptInjection: false, secretLeak: false, maliciousPatch: false });
    expect(normalizeUnifiedSessionEvent(compact)).toEqual(compact);
  });

  it("keeps only supplied workflow version identifiers without conflating CLI and engine versions", () => {
    const source = {
      type: "workflow.info",
      data: { engine_id: "custom", agent_version: "v2", version: "v3", cli_version: "", awf_version: "", awmg_version: "", token: "omit" },
    };
    expect(normalizeUnifiedSessionEvent(source).data).toEqual({
      engineId: "custom",
      agentVersion: "v2",
      cliVersion: "",
      awfVersion: "",
      mcpgVersion: "",
    });
    expect(normalizeUnifiedSessionEvent({ type: "workflow.info", data: { engine_id: "custom", version: "v3" } }).data).toEqual({
      engineId: "custom",
    });
    const metadata = normalizeUnifiedSessionEvent({ type: "workflow.info", data: { engine: "claude", model: "requested", event_name: "schedule", cli_version: "1.0", mcpg_version: "2.0" } });
    expect(metadata.data).toEqual({ engineId: "claude", cliVersion: "1.0", mcpgVersion: "2.0", model: "requested", requestedModel: "requested", triggerType: "schedule" });
    expect(normalizeUnifiedSessionEvent(metadata)).toEqual(metadata);
  });

  it("uses one accounting shape without losing zero values or adding overlapping totals", () => {
    const data = {
      provider: "copilot",
      model: "gpt-5.4-mini",
      purpose: "subagent",
      path: "/v1/messages",
      x_initiator: "agent",
      request_id: "request-1",
      inputTokens: 0,
      output_tokens: 2,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      ai_credits_this_response: 0,
      ai_credits_total: 0,
      duration_ms: 0,
      opaque: "omit",
    };
    const runtime = normalizeUnifiedSessionEvent({ type: "firewall.token_usage", data });
    expect(runtime.data).toEqual({
      provider: "copilot",
      model: "gpt-5.4-mini",
      purpose: "subagent",
      path: "/v1/messages",
      xInitiator: "agent",
      requestId: "request-1",
      aic: 0,
      totalAic: 0,
      durationMs: 0,
      usage: { inputTokens: 0, outputTokens: 2, cacheReadInputTokens: 0, cacheCreationInputTokens: 0 },
    });
    const report = normalizeUnifiedSessionEvent({
      type: "usage.report",
      data: { purpose: "routing_classification", path: "/responses", x_initiator: "router" },
    });
    expect(report.data).toEqual({ purpose: "routing_classification", path: "/responses", xInitiator: "router" });
    const agent = normalizeUnifiedSessionEvent({ type: "session.result", data: { num_turns: 0, usage: { inputTokens: 0, output_tokens: 2, overflowed_tokens: ["cache_read_input_tokens"], unused: 10 } } });
    expect(agent.data).toEqual({ numTurns: 0, usage: { inputTokens: 0, outputTokens: 2, overflowedTokens: ["cacheReadInputTokens"] } });
    const reconciled = reconcileSessionUsage({ cache_read_input_tokens: 4 }, agent.data.usage);
    expect(reconciled.cache_read_input_tokens).toBeUndefined();
    expect(reconciled.cacheReadInputTokens).toBeUndefined();
    expect(reconciled.overflowed_tokens).toEqual(["cache_read_input_tokens"]);
    expect(normalizeUnifiedSessionEvent(runtime)).toEqual(runtime);
  });

  it("retains normalized subagent invocation and per-request usage fields", () => {
    const source = {
      type: "subagent.request",
      agentId: "invocation-1",
      data: {
        invocationId: "invocation-1",
        agentName: "reader",
        model: "gpt-5.4-mini",
        inputTokens: 10,
        outputTokens: 2,
        cacheReadTokens: 3,
        cacheWriteTokens: 4,
        discarded: "omit",
      },
    };
    expect(normalizeUnifiedSessionEvent(source)).toEqual({
      type: "subagent.request",
      agentId: "invocation-1",
      data: {
        invocationId: "invocation-1",
        agentName: "reader",
        model: "gpt-5.4-mini",
        inputTokens: 10,
        outputTokens: 2,
        cacheReadTokens: 3,
        cacheWriteTokens: 4,
      },
    });
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
          usage: { inputTokens: 10, outputTokens: 5, reasoningOutputTokens: reasoning, cacheReadInputTokens: 4, cacheCreationInputTokens: 0 },
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

  it("preserves camelCase AIC checkpoints and resolves only valid priced token usage", () => {
    const checkpoint = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", aiCreditsTotal: 2.5, input_tokens: 100 } }, "evals");
    expect(checkpoint.data).toMatchObject({ totalAic: 2.5 });
    expect(checkpoint.data).not.toHaveProperty("aic");

    const cacheOnly = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", cache_read_input_tokens: 500 } }, "evals");
    expect(cacheOnly.data.aic).toBeGreaterThan(0);

    for (const inputTokens of [-5, Infinity, NaN]) {
      const invalid = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", input_tokens: inputTokens } }, "detection");
      expect(invalid.data).not.toHaveProperty("aic");
    }

    const otherPhase = normalizeUnifiedSessionEvent({ type: "usage.report", data: { provider: "openai", model: "gpt-4o-mini", input_tokens: 100 } }, "conclusion");
    expect(otherPhase.data).not.toHaveProperty("aic");
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
