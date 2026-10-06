import type { CoreSessionEvent, DetectionResultEvent, SessionEventDataMap, ToolExecutionCompleteEvent } from "./agent_session";
import { createSessionEvent } from "../agent_session.cjs";
import type { UnifiedSessionEvent, UnifiedSessionEventDataMap } from "./unified_session";

const messages: CoreSessionEvent[] = [
  { type: "session.init", data: { sourceEngine: "copilot", tools: [] } },
  { type: "user.message", data: { content: "private prompt" } },
  { type: "assistant.message", data: { content: "" } },
  { type: "assistant.refusal", data: { reason: "content_filter", policyCategory: null } },
  { type: "assistant.reasoning", data: { content: "reasoning" } },
  { type: "tool.execution_start", data: { toolCallId: "call", input: false } },
  { type: "tool.execution_complete", data: { toolCallId: "call", success: false, output: null } },
  { type: "session.result", data: { numTurns: 0, usage: { input_tokens: 0 }, errors: [{ code: "failed" }] } },
  { type: "detection.result", data: { promptInjection: false } },
  { type: "session.format", data: { version: 1 } },
  { type: "agent.execution", data: { categories: [], errorCodes: [502, "provider_error"], errorTypes: ["server_error"], exitCode: 0 } },
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
createSessionEvent({}, "assistant.refusal", { reason: "refusal", content: "", explanation: null, partial: true });
// @ts-expect-error Refusal reasons are explicit provider signals, not arbitrary prose.
createSessionEvent({}, "assistant.refusal", { reason: "I cannot help" });
// @ts-expect-error A refusal requires a structured reason even when text is unavailable.
createSessionEvent({}, "assistant.refusal", {});
// @ts-expect-error File format version is numeric, not a document version string.
createSessionEvent({}, "session.format", { version: "1.1.0" });
// @ts-expect-error The file format header requires a version.
createSessionEvent({}, "session.format", {});
createSessionEvent({}, "agent.execution", { categories: ["agentic_engine_timeout"], errorCodes: [], errorTypes: [], exitCode: 143 });
// @ts-expect-error Execution exit codes are numeric.
createSessionEvent({}, "agent.execution", { categories: [], errorCodes: [], errorTypes: [], exitCode: "1" });
// @ts-expect-error Native error codes are strings or numbers.
createSessionEvent({}, "agent.execution", { categories: [], errorCodes: [false], errorTypes: [] });

createSessionEvent({}, "session.result", { status: "completed", sourceType: "turn.completed", usage: { reasoning_output_tokens: 0 } });
// @ts-expect-error Source terminal status is a string, not a completion flag.
createSessionEvent({}, "session.result", { status: true });
// @ts-expect-error Native source event type is a string.
createSessionEvent({}, "session.result", { sourceType: 0 });
// @ts-expect-error Reasoning tokens are numeric.
createSessionEvent({}, "session.result", { usage: { reasoning_output_tokens: "0" } });

const detection: DetectionResultEvent = createSessionEvent({}, "detection.result", { jobResult: "success", conclusion: "warning", reason: "threat_detected", promptInjection: true, secretLeak: false, maliciousPatch: false });
void detection;
createSessionEvent({}, "detection.result", { jobResult: "skipped", conclusion: "skipped", reason: "" });
// @ts-expect-error Detection verdict flags are booleans, not status strings.
createSessionEvent({}, "detection.result", { promptInjection: "false" });
// @ts-expect-error Detection conclusions are strings, not success flags.
createSessionEvent({}, "detection.result", { conclusion: true });
// @ts-expect-error Detection reason is a categorical string, not detector prose entries.
createSessionEvent({}, "detection.result", { reason: ["private reason"] });

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

const unifiedResult: UnifiedSessionEventDataMap["session.result"] = {
  numTurns: 0,
  usage: { inputTokens: 0, reasoningOutputTokens: 2, overflowedTokens: ["outputTokens"] },
};
void unifiedResult;
const invalidUnifiedUsage: UnifiedSessionEventDataMap["session.result"] = {
  usage: {
    // @ts-expect-error Unified payloads use camelCase, not parser accounting keys.
    input_tokens: 1,
  },
};
void invalidUnifiedUsage;
const invalidUnifiedInit: UnifiedSessionEventDataMap["session.init"] = {
  // @ts-expect-error Tool inventories are omitted from the essential projection.
  tools: [],
};
void invalidUnifiedInit;
