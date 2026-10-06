// Sanitized wire shape from githubnext/gh-aw-rai run 37397159182:
// agent/sandbox/agent/logs/process-1791248741707-407.2.log, 2026-10-06T01:12:45.797Z.
// This Claude response uses the OpenAI-compatible Chat Completions transport.
const contentFiltered = {
  id: "response-filtered",
  object: "chat.completion",
  model: "claude-sonnet-5",
  choices: [{ index: 0, finish_reason: "content_filter", message: { role: "assistant", content: null, refusal: null } }],
  usage: { prompt_tokens: 19929, completion_tokens: 9, prompt_tokens_details: { cached_tokens: 19251 }, total_tokens: 19938 },
};

// Official provider contracts, with benign text and identifiers:
// https://developers.openai.com/api/reference/resources/chat
// https://developers.openai.com/api/reference/resources/responses/streaming-events
// https://platform.claude.com/docs/en/build-with-claude/refusals-and-fallback
const openaiRefusal = {
  id: "response-refused",
  object: "chat.completion",
  choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: null, refusal: "  Cannot provide that answer.\r\n" } }],
  usage: { prompt_tokens: 10, completion_tokens: 4 },
};
const responsesRefusal = {
  id: "response-example",
  object: "response",
  status: "completed",
  output: [{ id: "message-example", type: "message", role: "assistant", content: [{ type: "refusal", refusal: "  Cannot provide that answer.\r\n" }] }],
  usage: { input_tokens: 10, output_tokens: 4 },
};
const anthropicRefusal = {
  id: "message-refused",
  type: "message",
  role: "assistant",
  model: "claude-sonnet-5.5",
  content: [],
  stop_reason: "refusal",
  stop_details: { type: "refusal", category: "general_harms", explanation: "  This request was declined.\r\n" },
  usage: { input_tokens: 10, output_tokens: 0 },
};

module.exports = { contentFiltered, openaiRefusal, responsesRefusal, anthropicRefusal };
