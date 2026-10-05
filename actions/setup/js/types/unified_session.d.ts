import type {
  AgentExecutionData,
  DetectionResultData,
  EventMetadata,
  JsonValue,
  MessageData,
  SessionCount,
  SessionEvent,
  SessionEventDataMap,
  SessionFileFormatData,
  SessionInitData,
  SessionProvenance,
  SessionResultData,
  ToolExecutionCompleteData,
  ToolExecutionStartData,
} from "./agent_session";

/** Essential accounting projection; parser traces retain their snake_case usage. */
export interface UnifiedSessionUsage {
  totalTokens?: SessionCount;
  inputTokens?: SessionCount;
  outputTokens?: SessionCount;
  reasoningOutputTokens?: SessionCount;
  cacheReadInputTokens?: SessionCount;
  cacheCreationInputTokens?: SessionCount;
  inputTokensIncludeCache?: boolean;
  overflowedTokens?: string[];
}

export interface UnifiedSessionResultData extends Pick<SessionResultData, "numTurns" | "durationMs" | "totalCostUsd" | "status" | "sourceType" | "errors" | "permissionDenials"> {
  usage?: UnifiedSessionUsage | null;
}

/** Runtime observations retain supplied JSON values, including null and false. */
export interface RuntimeObservationData {
  event?: JsonValue;
  level?: JsonValue;
  status?: JsonValue;
  message?: JsonValue;
  reason?: JsonValue;
  requestId?: JsonValue;
}

export interface McpObservationData {
  serverName?: JsonValue;
  direction?: JsonValue;
  rpcId?: JsonValue;
  method?: JsonValue;
  toolName?: JsonValue;
  toolCallId?: JsonValue;
  requestId?: JsonValue;
  durationMs?: JsonValue;
  inputSize?: JsonValue;
  outputSize?: JsonValue;
  status?: JsonValue;
  reason?: JsonValue;
  error?: JsonValue;
}

export interface FirewallAccessData {
  host?: JsonValue;
  method?: JsonValue;
  status?: JsonValue;
  decision?: JsonValue;
  bytes?: JsonValue;
  durationMs?: JsonValue;
}

export interface UsageReportData {
  provider?: JsonValue;
  model?: JsonValue;
  requestId?: JsonValue;
  status?: JsonValue;
  aic?: JsonValue;
  totalAic?: JsonValue;
  premiumRequests?: JsonValue;
  durationMs?: JsonValue;
  usage?: UnifiedSessionUsage | null;
}

export interface SafeOutputData {
  type?: JsonValue;
  repo?: JsonValue;
  number?: JsonValue;
  provider?: JsonValue;
  identifier?: JsonValue;
  url?: JsonValue;
  status?: JsonValue;
  message?: JsonValue;
  error?: JsonValue;
  errorCode?: JsonValue;
}

export interface SafeOutputErrorData extends SafeOutputData {
  errors?: (SafeOutputData | string | number | boolean | null | JsonValue[])[];
}

export interface ExperimentStateData {
  runId?: JsonValue;
  assignments?: JsonValue;
  counts?: JsonValue;
}

export interface ExperimentAssignmentData {
  assignments: JsonValue;
}

export interface GraderData {
  id?: JsonValue;
  name?: JsonValue;
  status?: JsonValue;
  value?: JsonValue;
  score?: JsonValue;
  unit?: JsonValue;
  passed?: JsonValue;
  direction?: JsonValue;
  threshold?: JsonValue;
  error?: JsonValue;
}

export interface GraderManifestData {
  graders?: GraderData[];
}

export interface GraderResultData extends GraderData {
  results?: GraderData[];
}

export interface EvalResultData {
  id?: JsonValue;
  answer?: JsonValue;
  model?: JsonValue;
  error?: JsonValue;
}

export interface ExecutionResultData {
  outcome?: JsonValue;
  conclusion?: JsonValue;
  exitCode?: JsonValue;
  durationMs?: JsonValue;
  startedAt?: JsonValue;
  finishedAt?: JsonValue;
}

export interface WorkflowInfoData {
  engineId?: JsonValue;
  agentVersion?: JsonValue;
  cliVersion?: JsonValue;
  awfVersion?: JsonValue;
  mcpgVersion?: JsonValue;
  requestedModel?: JsonValue;
  triggerType?: JsonValue;
  workflow?: JsonValue;
  repository?: JsonValue;
  runId?: JsonValue;
}

export interface CollectionWarningData {
  code: string;
  path?: string;
  /** @minimum 1 @multipleOf 1 */
  line?: number;
  [key: string]: unknown;
}

export interface CollectionData {
  sources: {
    component: string;
    phase: string;
    path: string;
    events: SessionCount;
    timestampUnit?: "seconds" | "milliseconds";
  }[];
  warnings: SessionCount;
  untimedEvents: SessionCount;
  absentComponents: string[];
}

/** Known essential payloads. Unknown native extensions remain open. */
export interface UnifiedSessionEventDataMap {
  "session.format": SessionFileFormatData;
  "agent.execution": AgentExecutionData;
  "session.init": Pick<SessionInitData, "sourceEngine" | "model" | "sessionId" | "cwd">;
  "session.start": Pick<SessionInitData, "sourceEngine" | "model" | "sessionId" | "cwd">;
  "user.message": Pick<MessageData, "content">;
  "assistant.message": Pick<MessageData, "content">;
  "assistant.reasoning": Pick<MessageData, "content">;
  "tool.execution_start": Pick<ToolExecutionStartData, "toolCallId" | "toolName" | "input" | "command" | "mcpServerName">;
  "tool.execution_complete": Pick<ToolExecutionCompleteData, "toolCallId" | "toolName" | "success" | "output" | "error" | "durationMs" | "exitCode" | "status" | "mcpServerName" | "isError">;
  "session.result": UnifiedSessionResultData;
  "mcp.rpc.request": McpObservationData;
  "mcp.rpc.response": McpObservationData;
  "mcp.difc.filtered": McpObservationData;
  "mcp.guard.blocked": McpObservationData;
  "mcp.tool_call": McpObservationData;
  "mcp.event": RuntimeObservationData;
  "firewall.http_access": FirewallAccessData;
  "firewall.token_usage": UsageReportData;
  "firewall.steering": RuntimeObservationData;
  "firewall.event": RuntimeObservationData;
  "safe_output.request": SafeOutputData;
  "safe_output.result": SafeOutputData;
  "safe_output.error": SafeOutputErrorData;
  "experiment.state": ExperimentStateData;
  "experiment.assignment": ExperimentAssignmentData;
  "grader.manifest": GraderManifestData;
  "grader.result": GraderResultData;
  "eval.result": EvalResultData;
  "usage.report": UsageReportData;
  "execution.result": ExecutionResultData;
  "detection.result": DetectionResultData;
  "workflow.info": WorkflowInfoData;
  "session.collection_warning": CollectionWarningData;
  "session.collection": CollectionData;
}

export type KnownUnifiedSessionEvent = {
  [T in keyof UnifiedSessionEventDataMap]: EventMetadata & { type: T; data: UnifiedSessionEventDataMap[T] & Record<string, unknown>; provenance: SessionProvenance };
}[keyof UnifiedSessionEventDataMap];

export type UnifiedSessionEvent = KnownUnifiedSessionEvent | (SessionEvent & { provenance: SessionProvenance });
export type UnifiedSession = UnifiedSessionEvent[];

/** Schema generator entry point, not a serialized session wrapper. */
export interface SessionSchemaDefinitions {
  metadata: EventMetadata;
  provenance: SessionProvenance;
  agent: SessionEventDataMap;
  unified: UnifiedSessionEventDataMap;
}
