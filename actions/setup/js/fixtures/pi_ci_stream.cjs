// @ts-check

// Shape and order from Daily Repository Chronicle run 36884805242, agent artifact
// 11174126484/pi-streaming.jsonl. All text, IDs, timestamps and arguments are synthetic.
const user = { role: "user", content: [{ type: "text", text: "SANITIZED PRIVATE PROMPT" }], timestamp: 1 };
const call = { type: "toolCall", id: "sanitized-call", name: "bash", arguments: { command: "printf sanitized" } };
const first = {
  role: "assistant",
  content: [call],
  api: "openai-responses",
  provider: "openai",
  model: "gpt-5.4",
  timestamp: 2,
  responseId: "sanitized-response-1",
  stopReason: "toolUse",
  usage: { input: 6407, output: 140, cacheRead: 1536, cacheWrite: 0, totalTokens: 8083, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } },
};
const result = { content: [{ type: "text", text: "sanitized output" }], structuredContent: { success: true, data: { count: 0 } } };
const toolResult = { role: "toolResult", toolCallId: call.id, toolName: call.name, content: result.content, isError: false, timestamp: 3 };
const final = {
  role: "assistant",
  content: [{ type: "text", text: "Sanitized complete.\n" }],
  model: "gpt-5.4",
  timestamp: 4,
  responseId: "sanitized-response-2",
  stopReason: "stop",
  usage: { input: 20, output: 5, cacheRead: 4, cacheWrite: 1, totalTokens: 30, cost: { total: 0 } },
};

const success = [
  { type: "session", version: 3, id: "sanitized-session", timestamp: "2026-01-01T00:00:00Z", cwd: "/sanitized/repo" },
  { type: "agent_start" },
  { type: "turn_start" },
  { type: "message_start", message: { role: "system", content: "SANITIZED SYSTEM PROMPT", sections: [], timestamp: 0, toolsAdded: [] } },
  { type: "message_end", message: { role: "system", content: "SANITIZED SYSTEM PROMPT", sections: [], timestamp: 0, toolsAdded: [] } },
  { type: "message_start", message: user },
  { type: "message_end", message: user },
  { type: "message_start", message: { ...first, content: [], usage: { input: 0, output: 0 } } },
  { type: "message_update", usage: { input: 0, output: 0 }, assistantMessageEvent: { type: "toolcall_start", contentIndex: 0, id: call.id, toolName: call.name } },
  { type: "message_update", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 0, delta: '{"command":' } },
  { type: "message_update", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 0, delta: '"printf sanitized"}' } },
  { type: "message_update", assistantMessageEvent: { type: "toolcall_end", contentIndex: 0, toolCall: call } },
  { type: "message_end", message: first },
  { type: "tool_execution_start", toolCallId: call.id, toolName: call.name, args: call.arguments },
  { type: "tool_execution_update", toolCallId: call.id, toolName: call.name, args: call.arguments, partialResult: { content: [{ type: "text", text: "sanitized" }] } },
  { type: "tool_execution_end", toolCallId: call.id, toolName: call.name, result, isError: false },
  { type: "message_start", message: toolResult },
  { type: "message_end", message: toolResult },
  { type: "turn_end", message: first, toolResults: [toolResult] },
  { type: "turn_start" },
  { type: "message_start", message: { ...final, content: [], usage: { input: 0, output: 0 } } },
  { type: "message_update", assistantMessageEvent: { type: "text_start", contentIndex: 0 } },
  { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "Sanitized " } },
  { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "complete.\n" } },
  { type: "message_update", assistantMessageEvent: { type: "text_end", contentIndex: 0, content: "Sanitized complete.\n" } },
  { type: "message_end", message: final },
  { type: "turn_end", message: final, toolResults: [] },
  { type: "agent_end", messages: [user, first, toolResult, final], willRetry: false },
  { type: "agent_settled" },
];

// Provider error shape from Repository Tree Map Generator run 36447274044,
// agent artifact 10981486046/pi-streaming.jsonl. Prompt and identifiers replaced.
const error = {
  role: "assistant",
  content: [],
  api: "openai-responses",
  provider: "copilot",
  model: "auto",
  timestamp: 5,
  stopReason: "error",
  errorMessage: '400: {"message":"The requested model is not supported.","code":"model_not_supported","param":"model","type":"invalid_request_error"}',
  usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } },
};
const failure = [
  success[0],
  { type: "agent_start" },
  { type: "turn_start" },
  { type: "message_start", message: user },
  { type: "message_end", message: user },
  { type: "message_start", message: { ...error, usage: undefined, errorMessage: undefined } },
  { type: "message_end", message: error },
  { type: "turn_end", message: error, toolResults: [] },
  { type: "agent_end", messages: [user, error], willRetry: false },
  { type: "agent_settled" },
];

module.exports = { success, failure };
