// @ts-check

/** @type {Record<string, Record<string, string[]>>} */
const COPILOT_WORKFLOW_EVENT_FIELDS = {
  "workflow.run_started": { runId: ["runId"], workflowName: ["workflowName"], attempt: ["attempt"] },
  "workflow.run_updated": { runId: ["runId"], revision: ["revision"] },
  "workflow.run_settled": {
    runId: ["runId"],
    status: ["status"],
    consumedNanoAiu: ["consumedNanoAiu"],
    consumedSubagents: ["consumedSubagents"],
    elapsedMs: ["elapsedMs"],
    failureType: ["failureType"],
  },
  "subagent.started": {
    workflowRunId: ["workflowRunId", "factoryRunId"],
    toolCallId: ["toolCallId"],
    agentName: ["agentName"],
    agentDisplayName: ["agentDisplayName"],
    parentId: ["parentId"],
    model: ["model"],
    executionMode: ["executionMode"],
  },
  "subagent.configured": { model: ["model"], contextTier: ["contextTier"], reasoningEffort: ["reasoningEffort"], multiTurn: ["multiTurn"] },
  "subagent.completed": {
    toolCallId: ["toolCallId"],
    agentName: ["agentName"],
    agentDisplayName: ["agentDisplayName"],
    model: ["model"],
    durationMs: ["durationMs"],
    totalTokens: ["totalTokens"],
    totalToolCalls: ["totalToolCalls"],
    cancelled: ["cancelled"],
  },
  "subagent.failed": { toolCallId: ["toolCallId"], agentName: ["agentName"], model: ["model"], durationMs: ["durationMs"], error: ["error"] },
};

const COPILOT_WORKFLOW_EVENT_TYPES = new Set(Object.keys(COPILOT_WORKFLOW_EVENT_FIELDS));

module.exports = { COPILOT_WORKFLOW_EVENT_FIELDS, COPILOT_WORKFLOW_EVENT_TYPES };
