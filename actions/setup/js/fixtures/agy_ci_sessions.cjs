// Sanitized subset of the native stream in the agent artifact of
// https://github.com/github/gh-aw/actions/runs/37848575608 (failed conformance,
// successful agent execution). Paths, conversation IDs and nonce payloads are
// replaced; states, missing fields, durations, text and accounting are retained.
const conversation = "agy-ci-conversation";
const step = data => ({ event: "step_update", step_update: { conversation_id: conversation, ...data } });

const agySmoke = [
  { event: "init", conversation_id: conversation, init: { model: "gemini-3.8-flash-medium", cwd: "/workspace/gh-aw", tools: ["write_to_file", "call_mcp_tool"], permission_mode: "always-proceed" } },
  step({ step_index: 0, state: "DONE", step_type: "user_input" }),
  step({
    step_index: 1,
    state: "DONE",
    step_type: "agent_response",
    duration_seconds: 4.544596791,
    usage: { input_tokens: 17580, output_tokens: 544, thinking_tokens: 492, cache_read_tokens: 0, total_tokens: 18124 },
  }),
  step({
    step_index: 16,
    state: "ACTIVE",
    step_type: "tool",
    tool_name: "call_mcp_tool",
    tool_info: { name: "call_mcp_tool", parameters: { Arguments: { file_nonce: "fixture-nonce" }, ServerName: "agy-native", ToolName: "native_challenge" } },
  }),
  step({
    step_index: 16,
    state: "DONE",
    step_type: "tool",
    tool_name: "call_mcp_tool",
    duration_seconds: 0.07261689,
    tool_info: { name: "call_mcp_tool", parameters: { Arguments: { file_nonce: "fixture-nonce" }, ServerName: "agy-native", ToolName: "native_challenge" }, output: '{"toolNonce":"fixture-receipt"}' },
  }),
  step({ step_index: 20, state: "ACTIVE", step_type: "tool", tool_name: "write_to_file", tool_info: { name: "write_to_file", parameters: { TargetFile: "/workspace/result.json" } } }),
  step({
    step_index: 20,
    state: "DONE",
    step_type: "tool",
    tool_name: "write_to_file",
    duration_seconds: 0.020827639,
    tool_info: { name: "write_to_file", parameters: { TargetFile: "/workspace/result.json" } },
  }),
  ...["Engine confi", "guration conformance sui", "te and native Agy confor"].map(text_delta => step({ step_index: 33, state: "ACTIVE", step_type: "agent_response", text_delta })),
  step({
    step_index: 33,
    state: "DONE",
    step_type: "agent_response",
    text_delta: "mance gate executed successfully.\n",
    duration_seconds: 3.540337529,
    usage: { input_tokens: 5715, output_tokens: 371, thinking_tokens: 358, cache_read_tokens: 20916, total_tokens: 6086 },
  }),
  {
    event: "result",
    result: {
      conversation_id: conversation,
      status: "SUCCESS",
      response: "Engine configuration conformance suite and native Agy conformance gate executed successfully.\n",
      duration_seconds: 51.779892838,
      num_turns: 1,
      usage: { input_tokens: 79491, output_tokens: 4742, thinking_tokens: 3221, cache_read_tokens: 298918, total_tokens: 84233 },
    },
  },
];

module.exports = { agySmoke };
