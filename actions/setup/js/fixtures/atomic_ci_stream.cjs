// @ts-check

// Captured from published @bastani/atomic@0.9.79 running `--print --mode json`
// with one `workflow` list tool call and a closing text turn over an AWF-style
// OpenAI-compatible proxy that reused `responseId: "1"` across turns.
// Local paths are replaced with `/workspace/repo` and system/catalog text is trimmed.

const system = {
  role: "system",
  content: "",
  sections: {
    preamble: "You are a coding assistant running inside Atomic, a coding agent harness.",
    tools: "<tools>\nAvailable tools:\n- read: Read a path selector.\n- workflow: Run and inspect workflows.\n</tools>",
  },
  timestamp: 1794451530458,
};

const user = {
  role: "user",
  content: [{ type: "text", text: "List the available workflows." }],
  timestamp: 1794451530458,
};

const call = {
  type: "toolCall",
  id: "call_1",
  name: "workflow",
  arguments: { action: "list" },
};

const zeroUsage = {
  input: 0,
  output: 0,
  cacheRead: 0,
  cacheWrite: 0,
  totalTokens: 0,
  cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
};

const turnUsage = {
  input: 5,
  output: 2,
  cacheRead: 0,
  cacheWrite: 0,
  reasoning: 0,
  totalTokens: 7,
  cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0, totalFromProvider: false },
};

const firstAssistantStart = {
  role: "assistant",
  content: [call],
  api: "openai-completions",
  provider: "awf-proxy",
  model: "mock-model",
  usage: zeroUsage,
  stopReason: "toolUse",
  timestamp: 1794451530486,
  responseId: "1",
  rawStopReason: "tool_calls",
};

const firstAssistantEnd = {
  ...firstAssistantStart,
  usage: turnUsage,
  durationMs: 25,
  thinkingLevel: "off",
};

const workflowListText = JSON.stringify(
  {
    action: "list",
    items: [
      {
        name: "adversarial-verification",
        description: "Produce a candidate, score independent per-criterion verifier reports, and apply a deterministic mean-and-veto gate with bounded repairs.",
      },
    ],
  },
  null,
  2
);

const toolExecutionResult = {
  content: [{ type: "text", text: workflowListText }],
  details: { action: "list", count: 1 },
};

const toolResultMessage = {
  role: "toolResult",
  toolCallId: "call_1",
  toolName: "workflow",
  content: toolExecutionResult.content,
  details: toolExecutionResult.details,
  isError: false,
  timestamp: 1794451530577,
};

const finalAssistantStart = {
  role: "assistant",
  content: [{ type: "text", text: "listed" }],
  api: "openai-completions",
  provider: "awf-proxy",
  model: "mock-model",
  usage: zeroUsage,
  stopReason: "stop",
  timestamp: 1794451530583,
  responseId: "1",
  rawStopReason: "stop",
};

const finalAssistantEnd = {
  ...finalAssistantStart,
  usage: turnUsage,
  durationMs: 5,
  thinkingLevel: "off",
};

const atomicWorkflowStream = [
  { type: "session", version: 4, id: "01a1cda5-618c-773e-a218-2b71aeb9777d", createdAt: "2026-11-12T02:45:30.124Z", cwd: "/workspace/repo" },
  { type: "agent_start" },
  { type: "turn_start" },
  { type: "message_start", message: system },
  { type: "message_end", message: system },
  { type: "message_start", message: user },
  { type: "message_end", message: user },
  { type: "message_start", message: firstAssistantStart },
  { type: "message_update", usage: zeroUsage, assistantMessageEvent: { type: "toolcall_start", contentIndex: 0, id: "call_1", toolName: "workflow" } },
  { type: "message_update", usage: zeroUsage, assistantMessageEvent: { type: "toolcall_delta", contentIndex: 0, delta: '{"action":"list"}' } },
  { type: "message_update", usage: turnUsage, assistantMessageEvent: { type: "toolcall_end", contentIndex: 0, toolCall: call } },
  { type: "message_end", message: firstAssistantEnd },
  { type: "tool_execution_start", toolCallId: "call_1", toolName: "workflow", args: { action: "list" } },
  { type: "tool_execution_end", toolCallId: "call_1", toolName: "workflow", result: toolExecutionResult, isError: false, durationMs: 63 },
  { type: "message_start", message: toolResultMessage },
  { type: "message_end", message: toolResultMessage },
  { type: "turn_end", message: firstAssistantEnd, toolResults: [toolResultMessage] },
  { type: "turn_start" },
  { type: "message_start", message: finalAssistantStart },
  { type: "message_update", usage: zeroUsage, assistantMessageEvent: { type: "text_start", contentIndex: 0 } },
  { type: "message_update", usage: turnUsage, assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "listed" } },
  { type: "message_update", usage: turnUsage, assistantMessageEvent: { type: "text_end", contentIndex: 0, content: "listed" } },
  { type: "message_end", message: finalAssistantEnd },
  { type: "turn_end", message: finalAssistantEnd, toolResults: [] },
  { type: "agent_end", messages: [system, user, firstAssistantEnd, toolResultMessage, finalAssistantEnd] },
  { type: "agent_settled", aborted: false },
];

module.exports = { atomicWorkflowStream };
