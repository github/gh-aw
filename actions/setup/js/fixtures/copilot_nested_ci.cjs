// @ts-check

// Sanitized lifecycle and first-turn protocol excerpts from githubnext/
// gh-aw-routing-sandbox runs 37505153056 (research -> two explore agents)
// and 37732418818 (failed Haiku dispatch with successful sibling/fallback).
// IDs, prompts, messages, arguments and results are replaced; unrelated events
// and subsequent turns are omitted. Request counts and nano-AIU are observed.
const model = "claude-opus-5";
const copilotNestedCi = [
  { type: "session.start", id: "root-start", data: { sessionId: "root", selectedModel: model, reasoningEffort: "xhigh" } },
  { type: "user.message", data: { content: "PRIVATE_MAIN_PROMPT", messageId: "root-user", interactionId: "root-interaction", turnId: "0" } },
  { type: "assistant.turn_start", data: { turnId: "0", interactionId: "root-interaction" } },
  {
    type: "assistant.message",
    data: {
      messageId: "root-message",
      originatingMessageId: "root-user",
      model,
      content: "  Inspecting the evidence.\n",
      apiCallId: "root-api",
      interactionId: "root-interaction",
      turnId: "0",
      toolRequests: [{ toolCallId: "root-tool", name: "bash", arguments: { command: "printf sanitized" } }],
    },
  },
  { type: "tool.execution_start", data: { toolCallId: "root-tool", toolName: "bash", arguments: { command: "printf sanitized" }, turnId: "0", model } },
  { type: "tool.execution_complete", data: { toolCallId: "root-tool", success: true, result: { content: "  sanitized root output\n" }, turnId: "0", model } },
  { type: "assistant.turn_end", data: { turnId: "0" } },
  {
    type: "subagent.started",
    agentId: "research",
    parentId: "preceding-event",
    data: { toolCallId: "spawn-research", agentName: "research", agentDisplayName: "Research", model, executionMode: "background", agentDescription: "PRIVATE_AGENT_DESCRIPTION" },
  },
  { type: "subagent.configured", agentId: "research", data: { model, reasoningEffort: "xhigh", multiTurn: true } },
  { type: "user.message", agentId: "research", data: { content: "PRIVATE_RESEARCH_PROMPT", messageId: "research-user", interactionId: "research-interaction", turnId: "0", parentAgentTaskId: "research" } },
  { type: "system.message", agentId: "research", data: { role: "system", content: "PRIVATE_RESEARCH_SYSTEM", contentBlocks: [{ content: "PRIVATE_RESEARCH_SYSTEM" }], interactionId: "research-interaction" } },
  { type: "assistant.turn_start", agentId: "research", data: { turnId: "0", parentToolCallId: "spawn-research" } },
  {
    type: "assistant.message",
    agentId: "research",
    data: {
      messageId: "research-message",
      originatingMessageId: "research-user",
      model,
      content: "  Researching the evidence.\n",
      apiCallId: "research-api",
      interactionId: "research-interaction",
      turnId: "0",
      parentToolCallId: "spawn-research",
      toolRequests: [{ toolCallId: "research-tool", name: "github-get_me", arguments: {} }],
    },
  },
  { type: "tool.execution_start", agentId: "research", data: { toolCallId: "research-tool", toolName: "github-get_me", mcpServerName: "github", arguments: {}, turnId: "0", model, parentToolCallId: "spawn-research" } },
  { type: "tool.execution_complete", agentId: "research", data: { toolCallId: "research-tool", success: true, result: { content: "  sanitized research output\n" }, turnId: "0", model, parentToolCallId: "spawn-research" } },
  { type: "assistant.turn_end", agentId: "research", data: { turnId: "0", parentToolCallId: "spawn-research" } },
  { type: "subagent.started", agentId: "left", parentId: "preceding-left-event", data: { toolCallId: "spawn-left", parentId: "research", agentName: "explore", agentDisplayName: "Left", model, executionMode: "sync" } },
  { type: "subagent.started", agentId: "right", parentId: "preceding-right-event", data: { toolCallId: "spawn-right", parentId: "research", agentName: "explore", agentDisplayName: "Right", model, executionMode: "sync" } },
  { type: "subagent.configured", agentId: "right", data: { model, reasoningEffort: "low", multiTurn: false } },
  { type: "subagent.configured", agentId: "left", data: { model, reasoningEffort: "low", multiTurn: false } },
  {
    type: "assistant.message",
    agentId: "left",
    data: { messageId: "left-message", originatingMessageId: "left-user", content: "  Inspecting the left evidence.\n", model, apiCallId: "left-api", turnId: "0", parentToolCallId: "spawn-left" },
  },
  {
    type: "assistant.message",
    agentId: "right",
    data: { messageId: "right-message", originatingMessageId: "right-user", content: "  Inspecting the right evidence.\n", model, apiCallId: "right-api", turnId: "0", parentToolCallId: "spawn-right" },
  },
  { type: "assistant.turn_end", agentId: "left", data: { turnId: "0", parentToolCallId: "spawn-left" } },
  { type: "assistant.turn_end", agentId: "right", data: { turnId: "0", parentToolCallId: "spawn-right" } },
  { type: "subagent.completed", agentId: "left", data: { toolCallId: "spawn-left", agentName: "explore", model, totalToolCalls: 2, totalTokens: 23092, durationMs: 17390 } },
  { type: "subagent.completed", agentId: "right", data: { toolCallId: "spawn-right", agentName: "explore", model, totalToolCalls: 11, totalTokens: 175092, durationMs: 100230 } },
  { type: "subagent.completed", agentId: "research", data: { toolCallId: "spawn-research", agentName: "research", model, totalToolCalls: 82, durationMs: 441614 } },
  {
    type: "session.shutdown",
    data: {
      agentMetrics: {
        main: { totalNanoAiu: 389121550000, modelMetrics: { [model]: { requests: { count: 41 } } } },
        research: { agentName: "research", totalNanoAiu: 494206500000, modelMetrics: { [model]: { requests: { count: 45 } } } },
        left: { agentName: "explore", totalNanoAiu: 9141050000, modelMetrics: { [model]: { requests: { count: 3 } } } },
        right: { agentName: "explore", totalNanoAiu: 40977350000, modelMetrics: { [model]: { requests: { count: 12 } } } },
      },
    },
  },
];

const copilotFailedChildCi = [
  { type: "session.start", data: { sessionId: "root", selectedModel: "gpt-5.6-luna" } },
  { type: "subagent.started", agentId: "failed-child", data: { toolCallId: "spawn-failed", agentName: "file-summarizer", model: "claude-haiku-4.5", executionMode: "sync" } },
  { type: "subagent.configured", agentId: "failed-child", data: { model: "claude-haiku-4.5", multiTurn: false } },
  { type: "assistant.turn_start", agentId: "failed-child", data: { turnId: "0", parentToolCallId: "spawn-failed" } },
  { type: "assistant.turn_end", agentId: "failed-child", data: { turnId: "0", parentToolCallId: "spawn-failed" } },
  { type: "session.error", agentId: "failed-child", data: { errorType: "runtime", statusCode: 400, message: "Cannot translate Copilot request feature 'include'" } },
  { type: "subagent.failed", agentId: "failed-child", data: { toolCallId: "spawn-failed", agentName: "file-summarizer", model: "claude-haiku-4.5", error: "Cannot translate Copilot request feature 'include'" } },
  { type: "assistant.message", data: { content: "  Completed using the available evidence.\n", messageId: "root-message" } },
  { type: "assistant.turn_end", data: { turnId: "0" } },
];

module.exports = { copilotNestedCi, copilotFailedChildCi };
