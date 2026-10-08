// Sanitized selected shapes from agent-stdio.log:
// docs review https://github.com/github/gh-aw/actions/runs/37623157868 (artifact 11482714846);
// dynamic workflow https://github.com/github/gh-aw/actions/runs/37732499835 (artifact 11530348077);
// smoke https://github.com/github/gh-aw/actions/runs/37089888775.
// Text, commands, IDs, paths, and token counts are replacements.

const normalization = [
  {
    type: "assistant",
    uuid: "text-record",
    session_id: "normalization-session",
    parent_tool_use_id: null,
    timestamp: "2026-10-07T12:46:45.000Z",
    message: {
      id: "normalization-message",
      role: "assistant",
      model: "claude-sonnet-5",
      content: [{ type: "text", text: "  Inspect the example.\n" }],
    },
  },
  {
    type: "assistant",
    session_id: "normalization-session",
    parent_tool_use_id: null,
    message: {
      id: "normalization-message",
      model: "claude-sonnet-5",
      content: [{ type: "tool_use", id: "denied-tool", name: "Edit", input: { file_path: "/workspace/example.txt", old_string: "before", new_string: "after" } }],
    },
  },
  {
    type: "system",
    subtype: "permission_denied",
    tool_name: "Edit",
    tool_use_id: "denied-tool",
    decision_reason_type: "mode",
    message: "Permission to use Edit has been denied.",
    uuid: "denial-record",
    session_id: "normalization-session",
  },
  {
    type: "user",
    session_id: "normalization-session",
    parent_tool_use_id: null,
    message: { role: "user", content: [{ type: "tool_result", tool_use_id: "denied-tool", content: "Permission to use Edit has been denied.", is_error: true }] },
    tool_use_result: "Error: Permission to use Edit has been denied.",
  },
  {
    type: "assistant",
    session_id: "normalization-session",
    parent_tool_use_id: null,
    message: { id: "mcp-message", model: "claude-sonnet-4-6", content: [{ type: "tool_use", id: "mcp-tool", name: "mcp__safeoutputs__noop", input: { message: "Example complete." } }] },
  },
  {
    type: "user",
    session_id: "normalization-session",
    parent_tool_use_id: null,
    message: { role: "user", content: [{ type: "tool_result", tool_use_id: "mcp-tool", content: [{ type: "text", text: '{"result":"success"}' }] }] },
    tool_use_result: [{ type: "text", text: '{"result":"success"}' }],
  },
  {
    type: "result",
    subtype: "success",
    is_error: false,
    session_id: "normalization-session",
    num_turns: 2,
    usage: { input_tokens: 3, output_tokens: 7, output_tokens_details: { thinking_tokens: 2 }, cache_read_input_tokens: 11, cache_creation_input_tokens: 5 },
    permission_denials: [{ tool_name: "Edit", tool_use_id: "denied-tool", tool_input: { file_path: "/workspace/example.txt", old_string: "before", new_string: "after" } }],
  },
];

module.exports = { normalization };
