// Sanitized excerpt of existing Smoke Copilot run 37866304925 (CLI, not SDK).
// https://github.com/github/gh-aw/actions/runs/37866304925
// https://github.com/github/gh-aw/actions/runs/37866304925/artifacts/11588637333
// sandbox/agent/logs/copilot-session-state/9466684b-4e2c-4fa5-85d8-b133f83ba6cd/events.jsonl
// Persisted projections: agent/agent-session.jsonl and usage artifact 11588975692.
// IDs, timestamps, paths, commands, prompts and response text are sanitized.
// Accounting values are the observed full-session snapshot, not excerpt totals.
module.exports = [
  {
    type: "session.start",
    id: "audit-start",
    parentId: null,
    timestamp: "2026-10-09T00:00:00.000Z",
    data: { sessionId: "audit-session", selectedModel: "claude-sonnet-5.5", reasoningEffort: "medium", contextTier: null, alreadyInUse: false, remoteSteerable: false, context: { cwd: "/workspace/example" } },
  },
  { type: "user.message", id: "audit-user", parentId: "audit-start", data: { content: "PRIVATE_AUDIT_PROMPT", transformedContent: "PRIVATE_TRANSFORMED_PROMPT", messageId: "audit-prompt", turnId: "0" } },
  {
    type: "assistant.message",
    id: "audit-message",
    parentId: "audit-user",
    data: {
      content: "",
      reasoningText: "  Observed reasoning.\r\n",
      reasoningOpaque: "opaque",
      messageId: "audit-answer",
      model: "claude-sonnet-5.5",
      apiCallId: "audit-call",
      interactionId: "audit-interaction",
      turnId: "0",
      rte: false,
      toolRequests: [],
    },
  },
  { type: "tool.execution_start", id: "audit-tool-start", data: { toolCallId: "audit-tool", toolName: "bash", arguments: { command: "  echo sanitized\n" }, turnId: "0", model: "claude-sonnet-5.5" } },
  { type: "tool.execution_complete", id: "audit-tool-end", data: { toolCallId: "audit-tool", success: false, error: { message: "Sanitized permission denial.", code: "PERMISSION_DENIED" }, turnId: "0", model: "claude-sonnet-5.5" } },
  { type: "session.task_complete", id: "audit-complete", data: { success: true, summary: "  Observed final summary.\n" } },
  {
    type: "session.shutdown",
    id: "audit-shutdown",
    timestamp: "2026-10-09T00:00:43.821Z",
    data: {
      shutdownType: "routine",
      sessionStartTime: Date.parse("2026-10-09T00:00:00.000Z"),
      totalPremiumRequests: 0,
      totalNanoAiu: 12107690000,
      totalApiDurationMs: 9640,
      tokenDetails: { input: { tokenCount: 7529 }, cache_read: { tokenCount: 56564 }, cache_write: { tokenCount: 34441 }, output: { tokenCount: 1426 } },
      modelMetrics: { "claude-sonnet-5.5": { requests: { count: 3, cost: 0 }, usage: { inputTokens: 98534, outputTokens: 1426, cacheReadTokens: 56564, cacheWriteTokens: 0, reasoningTokens: 0 } } },
    },
  },
];
