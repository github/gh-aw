// @ts-check

// Sanitized native excerpts from Smoke Copilot runs 37713903842, 37470423256
// and 37553647085. The last workflow failed in safe outputs, not in the agent.
// Text, identities, context, timestamps and accounting values are replaced.
const copilotCiMessages = [
  {
    type: "session.start",
    id: "start",
    timestamp: "2026-10-08T00:00:00.000Z",
    data: { sessionId: "sample", selectedModel: "claude-sonnet-5.5", reasoningEffort: "medium", context: { cwd: "/workspace/repo" } },
  },
  {
    type: "user.message",
    id: "user",
    data: { content: "PRIVATE_USER_PROMPT", transformedContent: "PRIVATE_TRANSFORMED_PROMPT", messageId: "user-message", interactionId: "interaction", turnId: "0", parentAgentTaskId: "task", delivery: "immediate" },
  },
  {
    type: "system.message",
    id: "system",
    data: { role: "system", content: "PRIVATE_SYSTEM_PROMPT", contentBlocks: [{ content: "PRIVATE_SYSTEM_PROMPT", isStatic: true }], interactionId: "interaction" },
  },
  { type: "assistant.turn_start", id: "turn-start", data: { turnId: "0", interactionId: "interaction" } },
  {
    type: "assistant.message",
    id: "message",
    parentId: "turn-start",
    data: {
      content: "  Checking the repository.\n",
      messageId: "answer",
      originatingMessageId: "origin",
      model: "claude-sonnet-5.5",
      interactionId: "interaction",
      turnId: "0",
      apiCallId: "api",
      reasoningText: "  Inspect the available evidence.\n",
      reasoningOpaque: "PRIVATE_OPAQUE_REASONING",
      toolRequests: [
        { toolCallId: "shell", name: "bash", arguments: { command: "printf sanitized" }, type: "function", intentionSummary: "PRIVATE_INTENTION", toolTitle: "Run a command" },
        { toolCallId: "complete", name: "task_complete", arguments: { summary: "  Completed the task.\n" }, type: "function", toolTitle: "Complete task" },
      ],
      rte: true,
    },
  },
  { type: "tool.execution_start", id: "shell-start", data: { toolCallId: "shell", toolName: "bash", arguments: { command: "printf sanitized" }, model: "claude-sonnet-5.5", turnId: "0", shellToolInfo: { possiblePaths: [] } } },
  {
    type: "tool.execution_complete",
    id: "shell-end",
    data: { toolCallId: "shell", model: "claude-sonnet-5.5", interactionId: "interaction", turnId: "0", success: true, shellExecution: { exitCode: 0 }, result: { content: "  sanitized output\n" }, toolTelemetry: {} },
  },
  { type: "tool.execution_start", id: "complete-start", data: { toolCallId: "complete", toolName: "task_complete", arguments: { summary: "  Completed the task.\n" }, turnId: "0" } },
  { type: "tool.execution_complete", id: "complete-end", data: { toolCallId: "complete", success: true, result: { content: "  Completed the task.\n", detailedContent: "PRIVATE_DETAILS" }, interactionId: "interaction", turnId: "0" } },
  { type: "assistant.turn_end", id: "turn-end", data: { turnId: "0" } },
  { type: "session.task_complete", id: "task-complete", data: { summary: "  Completed the task.\n", success: true } },
  {
    type: "session.shutdown",
    id: "shutdown",
    timestamp: "2026-10-08T00:00:01.000Z",
    data: {
      sessionStartTime: Date.parse("2026-10-08T00:00:00.000Z"),
      modelMetrics: { "claude-sonnet-5.5": { requests: { count: 1, cost: 0 }, usage: { inputTokens: 30, outputTokens: 5, cacheReadTokens: 10, cacheWriteTokens: 0, reasoningTokens: 0 } } },
      tokenDetails: { input: { tokenCount: 5 }, cache_read: { tokenCount: 10 }, cache_write: { tokenCount: 15 }, output: { tokenCount: 5 } },
    },
  },
];

module.exports = { copilotCiMessages };
