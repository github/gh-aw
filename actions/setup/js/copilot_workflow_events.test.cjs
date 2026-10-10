import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { COPILOT_WORKFLOW_EVENT_TYPES } = require("./copilot_workflow_events.cjs");
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");

describe("Copilot subagent selection evidence", () => {
  it("captures selected events and preserves model evidence through unified projection", () => {
    const event = {
      type: "subagent.selected",
      ephemeral: true,
      agentId: "child",
      timestamp: "2026-10-10T00:00:00Z",
      data: {
        agentName: "researcher",
        agentDisplayName: "Researcher",
        tools: ["read", "grep"],
        invocationId: "invoke-1",
        toolCallId: "delegate-1",
        parentToolCallId: "delegate-1",
        model: "copilot/claude-sonnet-4.6",
        selectedModel: "anthropic/claude-sonnet-4.6",
        resolvedModel: "anthropic/claude-sonnet-4.6",
        modelSelectionSource: "custom-agent",
      },
    };
    expect(COPILOT_WORKFLOW_EVENT_TYPES.has(event.type)).toBe(true);
    const normalized = normalizeCopilotSession([event]).find(entry => entry.type === event.type);
    expect(normalizeUnifiedSessionEvent(normalized)).toMatchObject({
      type: event.type,
      agentId: "child",
      data: event.data,
    });
  });
});
