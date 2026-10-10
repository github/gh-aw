import type {
  AgentExecutionData,
  AssistantRefusalData,
  DetectionResultData,
  EventMetadata,
  JsonValue,
  MessageData,
  SessionCount,
  SessionEvent,
  SessionEventDataMap,
  SessionFileFormatData,
  SessionInitData,
  SessionMetric,
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

export interface UnifiedSessionContextData {
  sessionId?: JsonValue;
  parentToolUseId?: JsonValue;
  parentToolCallId?: JsonValue;
  agentId?: JsonValue;
}

export interface UnifiedSessionResultData extends UnifiedSessionContextData, Pick<SessionResultData, "sourceEngine" | "numTurns" | "durationMs" | "totalCostUsd" | "status" | "sourceType" | "errors" | "permissionDenials" | "agentMetrics"> {
  usage?: UnifiedSessionUsage | null;
}

export interface UnifiedMessageData extends Pick<MessageData, "content"> {
  delta?: JsonValue;
  partial?: JsonValue;
  messageId?: JsonValue;
  contentIndex?: JsonValue;
  channel?: JsonValue;
  sessionId?: JsonValue;
  agentId?: JsonValue;
  parentToolUseId?: JsonValue;
  model?: JsonValue;
  apiCallId?: JsonValue;
  interactionId?: JsonValue;
  turnId?: JsonValue;
  parentToolCallId?: JsonValue;
}

export interface DynamicWorkflowTaskData {
  taskId?: string;
  toolCallId?: string;
  taskType?: string;
  workflowName?: string;
  status?: string;
  sessionId?: string;
  parentToolUseId?: string | null;
}

export interface DynamicWorkflowProgressData {
  type?: string;
  index?: SessionCount;
  phaseIndex?: SessionCount;
  agentId?: string;
  model?: string;
  state?: string;
  attempt?: SessionCount;
  startedAt?: SessionMetric;
  queuedAt?: SessionMetric;
  lastProgressAt?: SessionMetric;
}

export interface DynamicWorkflowObservationData extends DynamicWorkflowTaskData {
  usage?: {
    totalTokens?: SessionCount;
    toolUses?: SessionCount;
    durationMs?: SessionMetric;
  };
  tasks?: DynamicWorkflowTaskData[];
  workflowProgress?: DynamicWorkflowProgressData[];
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

export interface ModelRoutingData {
  schema?: JsonValue;
  stage?: JsonValue;
  purpose?: JsonValue;
  attempt?: JsonValue;
  classifier_model?: JsonValue;
  classifier_attempts?: JsonValue;
  classifierModel?: JsonValue;
  classifierEffort?: JsonValue;
  objective?: JsonValue;
  provider?: JsonValue;
  labels?: JsonValue;
  mode?: JsonValue;
  degradedClassification?: JsonValue;
  degradedReason?: JsonValue;
  selectedId?: JsonValue;
  selectedProvider?: JsonValue;
  routed?: JsonValue;
  outcome?: JsonValue;
  selected_model?: JsonValue;
  selected_effort?: JsonValue;
  selectedModel?: JsonValue;
  selectedEffort?: JsonValue;
  wireModel?: JsonValue;
  endpoint?: JsonValue;
  router?: JsonValue;
  latencyMs?: JsonValue;
  code?: JsonValue;
  requestId?: JsonValue;
  request_id?: JsonValue;
  classifierAttempts?: JsonValue;
  deviations?: JsonValue;
  requestedModel?: JsonValue;
  requestedEffort?: JsonValue;
}

export interface UnifiedModelRoutingData {
  status?: JsonValue;
  source?: JsonValue;
  provider?: JsonValue;
  wireModel?: JsonValue;
  model?: JsonValue;
  effort?: JsonValue;
  appliedEffort?: JsonValue;
  effectiveEndpoint?: JsonValue;
  selectedEndpoint?: JsonValue;
  mode?: JsonValue;
  selectedId?: JsonValue;
  routerVersion?: JsonValue;
  failureCode?: JsonValue;
}

export interface ModelRoutingOutcomeData {
  status?: JsonValue;
  wireModel?: JsonValue;
  effectiveEndpoint?: JsonValue;
  selectedEndpoint?: JsonValue;
  effort?: JsonValue;
  appliedEffort?: JsonValue;
  failureCode?: JsonValue;
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

export interface UsageReportData extends UnifiedSessionContextData {
  stepIndex?: SessionCount;
  provider?: JsonValue;
  model?: JsonValue;
  purpose?: JsonValue;
  path?: JsonValue;
  xInitiator?: JsonValue;
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

export interface DailyAICDecisionData {
  status: "disabled" | "skipped" | "under_budget" | "exceeded" | "structural_error" | "transient_error";
  exceeded?: boolean;
  total?: number;
  estimated?: number;
  threshold?: number;
}

export interface WorkflowInfoData {
  engineId?: JsonValue;
  agentVersion?: JsonValue;
  cliVersion?: JsonValue;
  awfVersion?: JsonValue;
  mcpgVersion?: JsonValue;
  model?: JsonValue;
  requestedModel?: JsonValue;
  modelRouting?: UnifiedModelRoutingData;
  triggerType?: JsonValue;
  dryRun?: JsonValue;
  workflow?: JsonValue;
  repository?: JsonValue;
  runId?: JsonValue;
}

export interface GitHubApiRateLimitData {
  source?: JsonValue;
  credentialSource?: JsonValue;
  operation?: JsonValue;
  resource?: JsonValue;
  limit?: JsonValue;
  remaining?: JsonValue;
  used?: JsonValue;
  reset?: JsonValue;
  attempt?: JsonValue;
  delayMs?: JsonValue;
  status?: JsonValue;
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
  "session.init": Pick<SessionInitData, "sourceEngine" | "model" | "sessionId" | "cwd" | "reasoningEffort"> & UnifiedSessionContextData;
  "session.start": Pick<SessionInitData, "sourceEngine" | "model" | "sessionId" | "cwd" | "reasoningEffort"> & UnifiedSessionContextData;
  "session.error": {
    error?: JsonValue;
    message?: JsonValue;
    content?: JsonValue;
    code?: JsonValue;
    errorType?: JsonValue;
    model?: JsonValue;
    severity?: JsonValue;
    status?: JsonValue;
    sourceType?: JsonValue;
    exitCode?: JsonValue;
  } & UnifiedSessionContextData;
  "session.info": { message?: JsonValue; content?: JsonValue; model?: JsonValue; status?: JsonValue; sourceType?: JsonValue } & UnifiedSessionContextData;
  "session.shutdown": {
    shutdownType?: JsonValue;
    errorReason?: JsonValue;
    premiumRequests?: JsonValue;
    totalNanoAiu?: JsonValue;
    totalApiDurationMs?: JsonValue;
    sessionStartTime?: JsonValue;
    currentModel?: JsonValue;
  } & UnifiedSessionContextData;
  "session.task_complete": { success?: JsonValue; summary?: JsonValue } & UnifiedSessionContextData;
  "user.message": UnifiedMessageData;
  "prompt.system": Pick<MessageData, "content">;
  "prompt.user": Pick<MessageData, "content">;
  "assistant.message": UnifiedMessageData;
  "assistant.refusal": Pick<AssistantRefusalData, "reason" | "content" | "policyCategory" | "explanation" | "partial"> & UnifiedMessageData;
  "assistant.reasoning": UnifiedMessageData;
  "tool.execution_start": Pick<ToolExecutionStartData, "toolCallId" | "stepIndex" | "toolName" | "input" | "inputTruncated" | "command" | "mcpServerName"> & UnifiedSessionContextData;
  "tool.execution_update": Pick<ToolExecutionStartData, "toolCallId" | "stepIndex" | "toolName" | "input" | "mcpServerName"> & {
    output?: JsonValue;
    partial?: JsonValue;
    delta?: JsonValue;
    contentIndex?: JsonValue;
  } & UnifiedSessionContextData;
  "tool.execution_complete": Pick<
    ToolExecutionCompleteData,
    "toolCallId" | "stepIndex" | "toolName" | "success" | "output" | "result" | "error" | "durationMs" | "exitCode" | "status" | "mcpServerName" | "isError" | "taskId" | "taskType" | "workflowName" | "workflowRunId"
  > &
    UnifiedSessionContextData;
  "dynamicWorkflows.task_started": DynamicWorkflowObservationData;
  "dynamicWorkflows.task_progress": DynamicWorkflowObservationData;
  "dynamicWorkflows.task_updated": DynamicWorkflowObservationData;
  "dynamicWorkflows.task_notification": DynamicWorkflowObservationData;
  "dynamicWorkflows.background_tasks_changed": DynamicWorkflowObservationData;
  "session.result": UnifiedSessionResultData;
  "mcp.rpc.request": McpObservationData;
  "mcp.rpc.response": McpObservationData;
  "mcp.difc.filtered": McpObservationData;
  "mcp.guard.blocked": McpObservationData;
  "mcp.tool_call": McpObservationData;
  "mcp.event": RuntimeObservationData & { serverName?: JsonValue };
  "firewall.http_access": FirewallAccessData;
  "firewall.token_usage": UsageReportData;
  "firewall.model_routing": ModelRoutingData;
  "model_routing.outcome": ModelRoutingOutcomeData;
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
  "guardrail.daily_aic": DailyAICDecisionData;
  "workflow.info": WorkflowInfoData;
  "github_api.rate_limit": GitHubApiRateLimitData;
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
