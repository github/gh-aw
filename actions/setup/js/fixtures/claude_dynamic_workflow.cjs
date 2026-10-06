// Sanitized shapes from https://github.com/github/gh-aw/actions/runs/37482989112.
const session = { session_id: "dynamic-session", parent_tool_use_id: null };
const task = { ...session, task_id: "dynamic-task", tool_use_id: "workflow-tool" };
const phase = { type: "workflow_phase", index: 1, title: "Read fixture" };
const agent = {
  type: "workflow_agent",
  index: 1,
  label: "PRIVATE_AGENT_LABEL",
  phaseIndex: 1,
  phaseTitle: "Read fixture",
  agentId: "dynamic-agent",
  model: "claude-sonnet-4-6",
  state: "start",
  startedAt: 1791298666830,
  queuedAt: 1791298666812,
  attempt: 1,
  promptPreview: "PRIVATE_AGENT_PROMPT",
  promptFramed: true,
  lastProgressAt: 1791298666830,
};

const dynamicWorkflow = [
  { ...session, type: "system", subtype: "init", tools: ["Read", "Workflow"], model: "claude-sonnet-4-6", claude_code_version: "2.1.288", uuid: "dynamic-init" },
  {
    ...session,
    type: "assistant",
    uuid: "dynamic-launch",
    message: { id: "dynamic-message", role: "assistant", content: [{ type: "tool_use", id: "workflow-tool", name: "Workflow", input: { name: "smoke-claude-dynamic", args: { runId: "123" } } }] },
  },
  {
    ...session,
    type: "user",
    uuid: "dynamic-launch-result",
    message: { role: "user", content: [{ type: "tool_result", tool_use_id: "workflow-tool", content: "Workflow launched in background. Task ID: dynamic-task", is_error: false }] },
    tool_use_result: {
      status: "async_launched",
      taskId: "dynamic-task",
      taskType: "local_workflow",
      workflowName: "smoke-claude-dynamic",
      runId: "dynamic-run",
      summary: "Read a packaged fixture",
      transcriptDir: "/workspace/session/subagents/workflows/dynamic-run",
      scriptPath: "/workspace/session/workflows/scripts/smoke-claude-dynamic.js",
    },
  },
  {
    ...session,
    type: "system",
    subtype: "background_tasks_changed",
    tasks: [{ task_id: "dynamic-task", task_type: "local_workflow", description: "PRIVATE_TASK_DESCRIPTION" }],
    uuid: "dynamic-background-start",
  },
  {
    ...task,
    type: "system",
    subtype: "task_started",
    description: "PRIVATE_TASK_DESCRIPTION",
    task_type: "local_workflow",
    workflow_name: "smoke-claude-dynamic",
    prompt: "PRIVATE_WORKFLOW_SCRIPT",
    uuid: "dynamic-start",
  },
  {
    ...task,
    type: "system",
    subtype: "task_progress",
    description: "PRIVATE_TASK_DESCRIPTION",
    summary: "PRIVATE_TASK_SUMMARY",
    last_tool_name: "Read fixture",
    usage: { total_tokens: 0, tool_uses: 0, duration_ms: 0 },
    workflow_progress: [phase, agent],
    uuid: "dynamic-progress-start",
  },
  {
    ...task,
    type: "system",
    subtype: "task_progress",
    usage: { total_tokens: 250, tool_uses: 2, duration_ms: 5000 },
    workflow_progress: [phase, { ...agent, state: "done", lastProgressAt: 1791298671830 }],
    uuid: "dynamic-progress-end",
  },
  { ...session, type: "system", subtype: "task_updated", task_id: "dynamic-task", patch: { status: "completed", end_time: 1791298671939 }, uuid: "dynamic-update" },
  { ...session, type: "system", subtype: "background_tasks_changed", tasks: [], uuid: "dynamic-background-end" },
  {
    ...task,
    type: "system",
    subtype: "task_notification",
    status: "completed",
    output_file: "/workspace/session/tasks/dynamic-task.output",
    summary: "PRIVATE_TASK_SUMMARY",
    usage: { total_tokens: 250, tool_uses: 2, duration_ms: 5000 },
    uuid: "dynamic-completion",
  },
  { ...session, type: "result", subtype: "success", is_error: false, num_turns: 3, usage: { input_tokens: 7, output_tokens: 11 }, total_cost_usd: 0.01, uuid: "dynamic-result" },
];

module.exports = { dynamicWorkflow };
