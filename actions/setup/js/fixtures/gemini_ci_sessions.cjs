// Sanitized Smoke Gemini agent-stdio.log records, downloaded from existing CI:
// https://github.com/github/gh-aw/actions/runs/36504829912 (agent artifact 11006632304)
// https://github.com/github/gh-aw/actions/runs/36283760088 (agent artifact 10919543914)
// Only the user prompts are replaced. Native IDs, timestamps, provider diagnostics,
// models, and zero-valued statistics are retained. Neither run observed an answer
// or tool lifecycle; those cases in gemini_session.test.cjs are synthetic.

const error = {
  type: "unknown",
  message:
    "[API Error: Your project has exceeded its monthly spending cap. Please go to AI Studio at https://ai.studio/spend to manage your project spend cap. Learn more at https://ai.google.dev/gemini-api/docs/billing#project-spend-caps. ]\n" +
    "Please wait and try again later. To increase your limits, request a quota increase through AI Studio, or switch to another /auth method",
};

const stats = {
  total_tokens: 0,
  input_tokens: 0,
  output_tokens: 0,
  cached: 0,
  input: 0,
  duration_ms: 0,
  tool_calls: 0,
  models: {
    "gemini-3.1-flash-lite": { total_tokens: 0, input_tokens: 0, output_tokens: 0, cached: 0, input: 0 },
    "gemini-3.1-pro-preview": { total_tokens: 0, input_tokens: 0, output_tokens: 0, cached: 0, input: 0 },
  },
};

const spendingCapSeptember29 = [
  { type: "init", timestamp: "2026-09-29T00:50:48.706Z", session_id: "7c0078c8-7724-4568-8fcf-08f78c9579b1", model: "auto" },
  { type: "message", timestamp: "2026-09-29T00:50:48.946Z", role: "user", content: "SANITIZED_CI_PROMPT: Inspect the example repository." },
  { type: "result", timestamp: "2026-09-29T00:55:25.203Z", status: "error", error, stats },
];

const spendingCapSeptember27 = [
  { type: "init", timestamp: "2026-09-27T00:54:10.758Z", session_id: "54317e9f-e763-4dcc-8817-45c78cb48b06", model: "auto" },
  { type: "message", timestamp: "2026-09-27T00:54:10.987Z", role: "user", content: "SANITIZED_CI_PROMPT: Inspect another example repository." },
  { type: "result", timestamp: "2026-09-27T00:59:06.625Z", status: "error", error, stats },
];

module.exports = { spendingCapSeptember29, spendingCapSeptember27 };
