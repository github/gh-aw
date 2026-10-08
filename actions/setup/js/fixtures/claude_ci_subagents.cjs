// Sanitized SDK shapes from documentation review run 37623157868, agent artifact
// 11482714846/agent-stdio.log. Prompts, IDs, commands and metrics are replacements.
// Reused IDs and the grandchild are synthetic isolation/depth regressions.
const session_id = "nested-session";
const assistant = (parent_tool_use_id, id, content, usage) => ({
  type: "assistant",
  session_id,
  parent_tool_use_id,
  message: { id, model: "fixture-model", content, ...(usage ? { usage } : {}) },
});
const task = (task_id, tool_use_id, parent_tool_use_id, spawn_depth) => ({
  type: "system",
  subtype: "task_started",
  session_id,
  parent_tool_use_id,
  task_id,
  tool_use_id,
  task_type: "local_agent",
  subagent_type: "general-purpose",
  is_backgrounded: true,
  spawn_depth,
  description: "PRIVATE_TASK_DESCRIPTION",
  prompt: "PRIVATE_SUBAGENT_PROMPT",
});
const launch = (parent_tool_use_id, tool_use_id, agentId) => ({
  type: "user",
  session_id,
  parent_tool_use_id,
  message: { role: "user", content: [{ type: "tool_result", tool_use_id, content: [{ type: "text", text: "Agent launched." }] }] },
  tool_use_result: { isAsync: true, status: "async_launched", agentId, resolvedModel: "fixture-model", prompt: "PRIVATE_SUBAGENT_PROMPT" },
});
const command = (parent, label, input_tokens) => [
  assistant(parent, "reused-message", [{ type: "text", text: `${label} answer\n` }], { input_tokens, output_tokens: 2 }),
  assistant(parent, "reused-message", [{ type: "tool_use", id: "reused-tool", name: "Bash", input: { command: `printf '${label}'` } }], { input_tokens, output_tokens: 3 }),
  { type: "user", session_id, parent_tool_use_id: parent, message: { content: [{ type: "tool_result", tool_use_id: "reused-tool", content: `${label} output`, is_error: false }] } },
];
const subagents = [
  { type: "system", subtype: "init", session_id, model: "fixture-model" },
  assistant(null, "launches", [
    { type: "tool_use", id: "launch-a", name: "Agent", input: { prompt: "PRIVATE_SUBAGENT_PROMPT" } },
    { type: "tool_use", id: "launch-b", name: "Agent", input: { prompt: "PRIVATE_SUBAGENT_PROMPT" } },
  ]),
  task("agent-a", "launch-a", null, 1),
  launch(null, "launch-a", "agent-a"),
  task("agent-b", "launch-b", null, 1),
  launch(null, "launch-b", "agent-b"),
  ...command("launch-a", "child-a", 100),
  ...command("launch-b", "child-b", 200),
  assistant("launch-a", "grandchild-launch", [{ type: "tool_use", id: "launch-grandchild", name: "Agent", input: { prompt: "PRIVATE_SUBAGENT_PROMPT" } }]),
  task("agent-grandchild", "launch-grandchild", "launch-a", 2),
  launch("launch-a", "launch-grandchild", "agent-grandchild"),
  ...command("launch-grandchild", "grandchild", 300),
  ...command(null, "root", 7),
  { type: "system", subtype: "task_notification", session_id, task_id: "agent-a", tool_use_id: "launch-a", status: "completed", summary: "PRIVATE_TASK_SUMMARY", usage: { total_tokens: 103, tool_uses: 1, duration_ms: 10 } },
  { type: "system", subtype: "task_notification", session_id, task_id: "agent-b", tool_use_id: "launch-b", status: "failed", summary: "PRIVATE_TASK_SUMMARY", usage: { total_tokens: 203, tool_uses: 1, duration_ms: 20 } },
  { type: "result", session_id, subtype: "success", is_error: false, usage: { input_tokens: 7, output_tokens: 3 } },
];

module.exports = { subagents };
