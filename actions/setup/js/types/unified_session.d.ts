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
  sourceEngine?: string;
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

export interface PluginInstallationData {
  plugin: string;
  repository: string;
  ref: string;
  path: string;
  status: "installed";
}

/** AWF API proxy steering event-log observation. */
export interface FirewallSteeringData extends RuntimeObservationData {
  threshold?: JsonValue;
}

/** AWF steering notice delivered on a model request. */
export interface SteeringNotice {
  type: "ai_credit" | "timeout" | "token";
  threshold: 80 | 90 | 95 | 99;
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

export interface ModelEndpointMismatchData {
  category: "model_endpoint_mismatch";
  phase: "startup" | "runtime";
  configured_model: string;
  resolved_model: string;
  wire_api: string;
  wire_api_source: string;
  supported_endpoints: string[];
  detail: string;
  fix: string;
  model?: string;
  endpoint?: string;
}

export interface ModelResolutionData {
  engine: string;
  role: string;
  configured_model: string;
  resolved_model: string | null;
  provider: string | null;
  status: "resolved" | "exact" | "failed";
  cause: "model_unavailable" | "catalog_unavailable" | null;
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

export interface ModelInventoryData {
  endpoints: {
    provider: string;
    models: string[];
    routingModels?: {
      modelId: string;
      supportedEndpoints?: string[];
    }[];
  }[];
  modelsFetchComplete?: boolean;
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
  httpStatus?: number;
  retryHint?: string;
  reason?: JsonValue;
  error?: JsonValue;
}

export interface McpConfigurationData {
  servers: {
    serverName: string;
    transport?: "stdio" | "http" | "sse" | "streamable-http";
    required?: boolean;
    tools?: string[];
  }[];
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
  steering?: SteeringNotice;
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
  source?: JsonValue;
  enabled?: JsonValue;
  min?: JsonValue;
  max?: JsonValue;
  digest?: JsonValue;
  severity?: JsonValue;
  message?: JsonValue;
}

export interface GraderManifestData {
  version?: JsonValue;
  graders?: GraderData[];
}

export interface GraderMeasurementData extends GraderData {
  implementation?: {
    id?: JsonValue;
    version?: JsonValue;
    digest?: JsonValue;
  } | null;
  metrics?: { id?: JsonValue; value?: JsonValue }[];
}

export interface GraderResultData extends GraderMeasurementData {
  version?: JsonValue;
  run?: {
    id?: JsonValue;
    attempt?: JsonValue;
    graderCount?: JsonValue;
    passed?: JsonValue;
    failed?: JsonValue;
    errors?: JsonValue;
  } | null;
  results?: GraderMeasurementData[];
}

export interface EvalResultData {
  id?: JsonValue;
  answer?: JsonValue;
  model?: JsonValue;
  error?: JsonValue;
  runId?: JsonValue;
  assignments?: JsonValue;
}

export interface ExecutionResultData {
  outcome?: JsonValue;
  conclusion?: JsonValue;
  exitCode?: JsonValue;
  durationMs?: JsonValue;
  startedAt?: JsonValue;
  finishedAt?: JsonValue;
}

/** Conclusion's dependency snapshot, not the still-running conclusion job. */
export interface WorkflowJobResultData {
  /** @pattern ^[A-Za-z_][A-Za-z0-9_-]*$ */
  jobId: string;
  result: "success" | "failure" | "cancelled" | "skipped";
  durationMs?: SessionCount;
}

export interface DailyAICDecisionData {
  status: "disabled" | "skipped" | "under_budget" | "exceeded" | "structural_error" | "transient_error";
  exceeded?: boolean;
  total?: number;
  estimated?: number;
  threshold?: number;
}

/** Observed lineage from aw_info.context; hopId identifies the recorded caller. */
export interface EpisodeLineageData {
  episodeId?: JsonValue;
  hopId?: JsonValue;
  parentHopId?: JsonValue;
  originEvent?: JsonValue;
  rootRepo?: JsonValue;
  rootWorkflowId?: JsonValue;
  rootRunId?: JsonValue;
}

export interface ModelSelectionData {
  status?: JsonValue;
  configuredModel?: JsonValue;
  resolvedModel?: JsonValue;
  effectiveModel?: JsonValue;
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
  modelSelection?: ModelSelectionData;
  triggerType?: JsonValue;
  dryRun?: JsonValue;
  workflow?: JsonValue;
  repository?: JsonValue;
  runId?: JsonValue;
  awContext?: Record<string, JsonValue>;
  episode?: EpisodeLineageData | null;
}

export interface SkillInventoryData {
  skills: { name: string; source: string; scope: string; digest: string; purpose?: string }[];
}

export interface SkillConflictData {
  name: string;
  kind: "name_collision" | "scope_collision";
  sources: string[];
}

export interface SkillInvokedData {
  name?: JsonValue;
  path?: JsonValue;
  source?: JsonValue;
  trigger?: JsonValue;
  model?: JsonValue;
  invokedAtTurn?: JsonValue;
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
    timestampUnit?: "seconds" | "milliseconds" | "nanoseconds";
  }[];
  warnings: SessionCount;
  untimedEvents: SessionCount;
  absentComponents: string[];
}

/** Known essential payloads. Unknown native extensions remain open. */
export interface UnifiedSessionEventDataMap {
  "workflow.job_result": WorkflowJobResultData;
  "model.inventory": ModelInventoryData;
  "skill.inventory": SkillInventoryData;
  "skill.conflict": SkillConflictData;
  "skill.invoked": SkillInvokedData;
  "skill.context_delivered_ref": { interactionId?: JsonValue; source?: JsonValue; contentId?: JsonValue };
  "session.format": SessionFileFormatData;
  "agent.execution": AgentExecutionData;
  "session.init": Pick<SessionInitData, "sourceEngine" | "model" | "sessionId" | "cwd" | "reasoningEffort"> & UnifiedSessionContextData;
  "session.start": Pick<SessionInitData, "sourceEngine" | "model" | "sessionId" | "cwd" | "reasoningEffort"> & UnifiedSessionContextData;
  "session.error": {
    error?: JsonValue;
    message?: JsonValue;
    content?: JsonValue;
    code?: JsonValue;
    statusCode?: JsonValue;
    errorType?: JsonValue;
    model?: JsonValue;
    severity?: JsonValue;
    status?: JsonValue;
    sourceType?: JsonValue;
    exitCode?: JsonValue;
  } & UnifiedSessionContextData;
  "session.info": {
    message?: JsonValue;
    content?: JsonValue;
    model?: JsonValue;
    status?: JsonValue;
    sourceType?: JsonValue;
    role?: JsonValue;
    messageId?: JsonValue;
    toolCallId?: JsonValue;
    toolName?: JsonValue;
    /** Repeated observed metadata, never additive inference usage. */
    usageSnapshot?: JsonValue;
    /** Native estimates, not measured or additive token usage. */
    estimatedTokens?: JsonValue;
    estimatedTokensDelta?: JsonValue;
    cwd?: JsonValue;
    branch?: JsonValue;
    kind?: JsonValue;
    rateLimitInfo?: JsonValue;
    isAuthenticating?: JsonValue;
    output?: JsonValue;
  } & UnifiedSessionContextData;
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
  "assistant.message_update": UnifiedMessageData & {
    sourceType?: JsonValue;
    /** Native update subtype distinguishes historical Pi text and thinking controls. */
    type?: JsonValue;
    /** Observed native usage, not an additive accounting contribution. */
    usageSnapshot?: JsonValue;
  };
  "tool.execution_start": Pick<ToolExecutionStartData, "toolCallId" | "stepIndex" | "toolName" | "input" | "inputTruncated" | "command" | "mcpServerName"> & UnifiedSessionContextData;
  "tool.execution_update": Pick<ToolExecutionStartData, "toolCallId" | "stepIndex" | "toolName" | "input" | "mcpServerName"> & {
    output?: JsonValue;
    partial?: JsonValue;
    delta?: JsonValue;
    contentIndex?: JsonValue;
    sourceType?: JsonValue;
    /** Repeated observed metadata, never additive inference usage. */
    usageSnapshot?: JsonValue;
    durationMs?: JsonValue;
    status?: JsonValue;
    command?: JsonValue;
    taskId?: string;
    taskType?: string;
    error?: JsonValue;
  } & UnifiedSessionContextData;
  "tool.execution_complete": Pick<
    ToolExecutionCompleteData,
    "toolCallId" | "stepIndex" | "toolName" | "success" | "output" | "result" | "error" | "fileEdits" | "durationMs" | "exitCode" | "status" | "mcpServerName" | "isError" | "taskId" | "taskType" | "workflowName" | "workflowRunId"
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
  "mcp.configuration": McpConfigurationData;
  "firewall.http_access": FirewallAccessData;
  "firewall.token_usage": UsageReportData;
  "firewall.model_routing": ModelRoutingData;
  "model_routing.outcome": ModelRoutingOutcomeData;
  "model_endpoint.mismatch": ModelEndpointMismatchData;
  "model.resolution": ModelResolutionData;
  "firewall.steering": FirewallSteeringData;
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
  "plugin.installation": PluginInstallationData;
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
