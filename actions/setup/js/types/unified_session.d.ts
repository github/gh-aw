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
  /** AWF route choices in rank order; ranks are one-based array positions. */
  rankedChoices?: ModelRoutingRankedChoice[];
  eligibleChoices?: JsonValue;
  catalogueOverlap?: JsonValue;
  selectedEndpoint?: JsonValue;
  requestedEndpoint?: JsonValue;
  upstreamEndpoint?: JsonValue;
  /** Request-telemetry dimensions AWF did not observe, such as `model` and `effort`; not unavailable models. */
  unavailable?: string[];
  /** Observed HTTP status, independent of `outcome`. */
  status?: JsonValue;
  /** AWF failure phase, such as `primary` or `bootstrap`; not the collection provenance phase. */
  phase?: JsonValue;
  /** Identifier-only provider or AWF error code on failed request records; absent means unknown. */
  errorCode?: string;
  /** Identifier-only provider or AWF error type on failed request records; absent means unknown. */
  errorType?: string;
  /** Whether a failed request was rejected by the upstream provider or by AWF; absent means unknown. */
  errorSource?: "upstream" | "awf";
}

/** Proxy-written upstream error response, projected without bodies, headers or messages. */
export interface FirewallUpstreamErrorData {
  requestId?: string;
  provider?: string;
  model?: string;
  /** @asType integer */
  httpStatus?: number;
  /** Identifier-shaped provider error code; absent means the provider did not supply one. */
  errorCode?: string;
  errorType?: string;
  errorParam?: string;
}

/** Runner-written configured model policy; absent fields are unknown, not false, empty or zero. */
export interface ModelConfigData {
  /** Configured routing policy, never the router's per-run selection. */
  routing: {
    enabled: boolean;
    goal?: string;
    mode?: string;
    /** Configured routing candidates as authored (exact ids or gh-aw aliases) without provider prefix; absent means the router default or unknown. */
    allowedModels?: string[];
    /** Effective router pool sent to AWF as provider-qualified ids or patterns: aliases expanded, narrowed by the model policy; absent when unknown or expression-valued. */
    candidateModels?: string[];
  };
  /** Configured model allow/block policy; empty lists mean no restriction was configured. */
  models?: { allowed: string[]; blocked: string[] };
  /**
   * Effective per-run AI credit budget.
   * @asType integer
   * @minimum 1
   * @maximum 2147483647
   */
  maxAiCredits?: number;
  /** Provenance of the budget: compiled frontmatter, the repository default, or the admission allowance. */
  maxAiCreditsSource?: "frontmatter" | "default" | "admission";
  /** Concrete main model the run would use without routing, after alias resolution. */
  baselineModel?: string;
}

/** Collector-written repricing of main-agent requests at the baseline model; routed runs only. */
export interface ModelBaselineEstimateData {
  estimated: true;
  baselineModel?: string;
  /** Sum over priced main-agent requests; absent when no request could be priced. */
  aic?: number;
  /** @asType integer @minimum 0 */
  mainRequests: number;
  /** @asType integer @minimum 0 */
  pricedRequests: number;
  /** @asType integer @minimum 0 */
  unpricedRequests: number;
  /** @asType integer @minimum 0 */
  unattributedRequests: number;
  /** True only when every agent-phase request was attributed and every main request was priced. */
  complete: boolean;
}

export interface ModelRoutingRankedChoice {
  rank: number;
  id?: string;
  model?: string;
  effort?: string | null;
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
  cause: "model_unavailable" | "catalog_unavailable" | "unsupported_endpoint" | null;
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
    /** AWF's endpoint configuration state; absent when the source did not supply it. */
    configured?: boolean;
    /** Advertised model IDs in source order; absent means unknown, not empty. */
    models?: string[];
    /** Routing metadata in source order; absent means unknown, not empty. */
    routingModels?: {
      modelId: string;
      supportedEndpoints?: string[];
      /** Identifier-shaped reasoning efforts; absent means unknown; a supplied empty array stays empty. */
      supportedReasoningEfforts?: string[];
      /**
       * Positive context window size in tokens; absent means unknown.
       * @asType integer
       * @minimum 1
       * @maximum 9007199254740991
       */
      contextWindowTokens?: number;
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

export interface McpCliAuditData {
  /** @asType integer @minimum 0 */
  callStarts: SessionCount;
  /** @asType integer @minimum 0 */
  callCompletions: SessionCount;
  /** @asType integer @minimum 0 */
  toolErrors: SessionCount;
  /** @asType integer @minimum 0 */
  malformedRecords: SessionCount;
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
  /** AWF caller declaration: declared sub-agent name for `purpose: subagent`; absent for the main agent and AWF-owned traffic. */
  agentName?: JsonValue;
  /** Origin of `purpose`: `awf` for AWF-owned traffic, `caller_header` for an `x-awf-caller` declaration. */
  callerSource?: JsonValue;
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
  customGraders?: string[];
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

/** Validated admission allowances, not spend or credentials. */
export interface AdmissionDecisionData {
  status: "admitted" | "ignored" | "skipped";
  /** @minimum 1
   * @maximum 2147483647
   * @asType integer */
  maxAiCredits?: number;
  /** @minimum 1
   * @maximum 2147483647
   * @asType integer */
  threatDetectionMaxAiCredits?: number;
  /** @minimum 1
   * @maximum 2147483647
   * @asType integer */
  evalsMaxAiCredits?: number;
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
  parentRepo?: JsonValue;
  parentRunId?: JsonValue;
  parentRunAttempt?: JsonValue;
  parentWorkflowId?: JsonValue;
  parentWorkflowCallId?: JsonValue;
  parentEventType?: JsonValue;
}

export interface ModelSelectionData {
  status?: JsonValue;
  configuredModel?: JsonValue;
  resolvedModel?: JsonValue;
  effectiveModel?: JsonValue;
}

export interface WorkflowInfoData {
  engineId?: JsonValue;
  /** Compiled Copilot harness; absent means unknown, never inferred from engineId. */
  engineHarness?: "copilot-cli" | "copilot-sdk";
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
  /** Executing workflow reference supplied by the compiler from job.workflow_ref. */
  workflowId?: JsonValue;
  /** Executing workflow reference (job.workflow_ref); same value as workflowId. */
  workflowRef?: JsonValue;
  /** Current workflow invocation ID from aw_info, separate from the caller's ID. */
  workflowCallId?: JsonValue;
  /** GITHUB_WORKFLOW_REF; identifies the top-level caller in reusable workflows. */
  callerWorkflowRef?: JsonValue;
  repository?: JsonValue;
  runId?: JsonValue;
  runAttempt?: JsonValue;
  /** GitHub server origin, such as https://github.com. */
  serverUrl?: JsonValue;
  ref?: JsonValue;
  sha?: JsonValue;
  actor?: JsonValue;
  targetRepo?: JsonValue;
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
  "model.config": ModelConfigData;
  "model.baseline_estimate": ModelBaselineEstimateData;
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
  "mcp.cli_audit": McpCliAuditData;
  "firewall.http_access": FirewallAccessData;
  "firewall.token_usage": UsageReportData;
  "firewall.model_routing": ModelRoutingData;
  "firewall.upstream_error": FirewallUpstreamErrorData;
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
  "guardrail.admission": AdmissionDecisionData;
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
