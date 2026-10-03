// Selected, sanitized observations from the existing successful Smoke Gemini run:
// https://github.com/github/gh-aw/actions/runs/36078916290, agent/agent-stdio.log.
// Prompts, assistant text, paths, and failure descriptions are replacements.
// Native IDs, timestamps, tool names, outcome shapes, empty output, and reported
// full-session accounting are retained. This is a partial trace: the original
// has 33 assistant fragments and 86 tool starts/completions, not just this subset.

module.exports = [
  { type: "init", timestamp: "2026-09-25T00:47:28.072Z", session_id: "1ae771e3-0f6f-470d-87ca-45281d458bc6", model: "auto" },
  { type: "message", timestamp: "2026-09-25T00:47:28.318Z", role: "user", content: "SANITIZED_CI_LIFECYCLE_PROMPT: Inspect the example repository." },
  { type: "tool_use", timestamp: "2026-09-25T00:47:39.096Z", tool_name: "read_file", tool_id: "read_file__call_621569", parameters: { file_path: "/workspace/example.txt" } },
  { type: "tool_result", timestamp: "2026-09-25T00:47:39.125Z", tool_id: "read_file__call_621569", status: "success", output: "" },
  { type: "message", timestamp: "2026-09-25T00:48:36.689Z", role: "assistant", content: "  Inspecting the example.\n", delta: true },
  { type: "tool_use", timestamp: "2026-09-25T00:50:53.266Z", tool_name: "list_directory", tool_id: "list_directory__call_347288", parameters: { dir_path: "/outside/example" } },
  {
    type: "tool_result",
    timestamp: "2026-09-25T00:50:53.284Z",
    tool_id: "list_directory__call_347288",
    status: "error",
    output: "Directory is outside the allowed workspace.",
    error: { type: "invalid_tool_params", message: "Directory is outside the allowed workspace." },
  },
  { type: "message", timestamp: "2026-09-25T00:53:51.916Z", role: "assistant", content: "Inspection complete.\n", delta: true },
  {
    type: "result",
    timestamp: "2026-09-25T00:53:51.940Z",
    status: "success",
    stats: {
      total_tokens: 6413770,
      input_tokens: 6376298,
      output_tokens: 6710,
      cached: 4989184,
      input: 1387114,
      duration_ms: 383869,
      tool_calls: 86,
      models: {
        "gemini-3.1-flash-lite": { total_tokens: 10789, input_tokens: 10443, output_tokens: 48, cached: 0, input: 10443 },
        "gemini-3.5-flash": { total_tokens: 6348432, input_tokens: 6321441, output_tokens: 6145, cached: 4985187, input: 1336254 },
        "gemini-3-flash-preview": { total_tokens: 54549, input_tokens: 44414, output_tokens: 517, cached: 3997, input: 40417 },
      },
    },
  },
];
