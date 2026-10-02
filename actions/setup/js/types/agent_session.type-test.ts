import type { CoreSessionEvent, SessionEventDataMap, ToolExecutionCompleteEvent } from "./agent_session";
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
