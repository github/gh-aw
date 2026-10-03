import type { CoreSessionEvent, SessionEventDataMap, ToolExecutionCompleteEvent, UnifiedSessionEvent } from "./agent_session";
import { createSessionEvent } from "../agent_session.cjs";

const messages: CoreSessionEvent[] = [
  { type: "session.init", data: { sourceEngine: "copilot", tools: [] } },
  { type: "user.message", data: { content: "private prompt" } },
  { type: "assistant.message", data: { content: "" } },
  { type: "assistant.reasoning", data: { content: "reasoning" } },
  { type: "tool.execution_start", data: { toolCallId: "call", input: false } },
  { type: "tool.execution_complete", data: { toolCallId: "call", success: false, output: null } },
  { type: "session.result", data: { numTurns: 0, usage: { input_tokens: 0 }, errors: [{ code: "failed" }] } },
];
void messages;

const completion: ToolExecutionCompleteEvent = {
  type: "tool.execution_complete",
  data: {
    // @ts-expect-error Outcome is boolean, not a status string.
    success: "failed",
  },
};
void completion;

const result: SessionEventDataMap["session.result"] = {
  // @ts-expect-error Turns are numeric.
  numTurns: "2",
};
void result;

const knownCompletion = createSessionEvent({}, "tool.execution_complete", { success: true, output: 0 });
const knownOutcome: boolean | undefined = knownCompletion.data.success;
void knownOutcome;
// @ts-expect-error The factory checks the payload associated with the event type.
createSessionEvent({}, "tool.execution_complete", { success: "yes" });
// @ts-expect-error Legacy record types are not canonical event signatures.
createSessionEvent({}, "result", {});
createSessionEvent({}, "vendor.progress", { nativeValue: false });
createSessionEvent({}, "session.format", { version: 1 });
// @ts-expect-error File format version is numeric, not a document version string.
createSessionEvent({}, "session.format", { version: "1.1.0" });
// @ts-expect-error The file format header requires a version.
createSessionEvent({}, "session.format", {});

createSessionEvent({}, "session.result", { status: "completed", sourceType: "turn.completed", usage: { reasoning_output_tokens: 0 } });
// @ts-expect-error Source terminal status is a string, not a completion flag.
createSessionEvent({}, "session.result", { status: true });
// @ts-expect-error Native source event type is a string.
createSessionEvent({}, "session.result", { sourceType: 0 });
// @ts-expect-error Reasoning tokens are numeric.
createSessionEvent({}, "session.result", { usage: { reasoning_output_tokens: "0" } });

const mergedEvent: UnifiedSessionEvent = {
  type: "mcp.rpc.response",
  data: { payload: { id: 0, result: false } },
  provenance: { component: "mcp", phase: "agent", path: "mcp-logs/rpc-messages.jsonl", index: 0, timestampMs: 0 },
};
void mergedEvent;

const invalidProvenance: UnifiedSessionEvent = {
  type: "firewall.event",
  data: {},
  provenance: {
    component: "firewall",
    phase: "agent",
    path: "sandbox/firewall/logs/audit.jsonl",
    // @ts-expect-error Source position is numeric.
    index: "1",
  },
};
void invalidProvenance;
