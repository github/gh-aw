export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

/**
 * @minimum 0
 * @maximum 9007199254740991
 * @multipleOf 1
 */
export type SessionCount = number;

/** @minimum 0 */
export type SessionMetric = number;

/**
 * @minimum 0
 * @maximum 255
 * @multipleOf 1
 */
export type SessionExitCode = number;

/**
 * @minimum -9007199254740991
 * @maximum 9007199254740991
 * @multipleOf 1
 */
export type SessionErrorNumber = number;

/** @minLength 1 */
export type SessionDiagnosticName = string;

export interface EventMetadata {
  id?: string;
  parentId?: string | null;
  timestamp?: string | number;
  [key: string]: unknown;
}

export interface SessionInitData {
  sourceEngine?: string;
  model?: string;
  sessionId?: string | null;
  cwd?: string;
  tools?: JsonValue[];
  mcpServers?: JsonValue[];
  slashCommands?: JsonValue[];
  modelInfo?: JsonValue;
  [key: string]: unknown;
}

export interface MessageData {
  content?: JsonValue;
  [key: string]: unknown;
}

export interface ToolExecutionStartData {
  toolCallId?: string;
  toolName?: string;
  input?: JsonValue;
  parameters?: JsonValue;
  command?: string;
  mcpServerName?: string;
  [key: string]: unknown;
}

export interface ToolExecutionCompleteData {
  toolCallId?: string;
  toolName?: string;
  success?: boolean;
  output?: JsonValue;
  result?: JsonValue;
  error?: JsonValue;
  durationMs?: SessionMetric;
  exitCode?: number;
  status?: string;
  is_error?: boolean;
  isError?: boolean;
  mcpServerName?: string;
  [key: string]: unknown;
}

export interface SessionUsage {
  total_tokens?: SessionCount;
  input_tokens?: SessionCount;
  output_tokens?: SessionCount;
  reasoning_output_tokens?: SessionCount;
  cache_creation_input_tokens?: SessionCount;
  cache_read_input_tokens?: SessionCount;
  totalTokens?: SessionCount;
  inputTokens?: SessionCount;
  outputTokens?: SessionCount;
  cacheCreationInputTokens?: SessionCount;
  cacheReadInputTokens?: SessionCount;
  input_tokens_include_cache?: boolean;
  /** Unavailable aggregate fields whose contributions exceeded safe integer precision. */
  overflowed_tokens?: string[];
  [key: string]: unknown;
}

export interface SessionResultData {
  numTurns?: SessionCount;
  durationMs?: SessionMetric;
  totalCostUsd?: SessionMetric;
  status?: string;
  sourceType?: string;
  usage?: SessionUsage;
  errors?: JsonValue[];
  permissionDenials?: JsonValue[];
  [key: string]: unknown;
}

export interface SessionInitEvent extends EventMetadata {
  type: "session.init";
  data: SessionInitData;
}

export interface UserMessageEvent extends EventMetadata {
  type: "user.message";
  data: MessageData;
}

export interface AssistantMessageEvent extends EventMetadata {
  type: "assistant.message";
  data: MessageData;
}

export interface AssistantReasoningEvent extends EventMetadata {
  type: "assistant.reasoning";
  data: MessageData;
}

export interface ToolExecutionStartEvent extends EventMetadata {
  type: "tool.execution_start";
  data: ToolExecutionStartData;
}

export interface ToolExecutionCompleteEvent extends EventMetadata {
  type: "tool.execution_complete";
  data: ToolExecutionCompleteData;
}

export interface SessionResultEvent extends EventMetadata {
  type: "session.result";
  data: SessionResultData;
}

export interface AgentExecutionData {
  /** @uniqueItems true */
  categories: SessionDiagnosticName[];
  /** @uniqueItems true */
  errorCodes: (SessionDiagnosticName | SessionErrorNumber)[];
  /** @uniqueItems true */
  errorTypes: SessionDiagnosticName[];
  exitCode?: SessionExitCode;
  [key: string]: unknown;
}

export interface AgentExecutionEvent extends EventMetadata {
  type: "agent.execution";
  data: AgentExecutionData;
}

export interface SessionFileFormatData {
  /** @minimum 1 @multipleOf 1 */
  version: number;
  [key: string]: unknown;
}

export interface SessionFileFormatEvent extends EventMetadata {
  type: "session.format";
  data: SessionFileFormatData;
}

export interface DetectionResultData {
  jobResult?: string;
  conclusion?: string;
  reason?: string;
  promptInjection?: boolean;
  secretLeak?: boolean;
  maliciousPatch?: boolean;
  [key: string]: unknown;
}

export interface DetectionResultEvent extends EventMetadata {
  type: "detection.result";
  data: DetectionResultData;
}

export type CoreSessionEvent =
  SessionInitEvent | UserMessageEvent | AssistantMessageEvent | AssistantReasoningEvent | ToolExecutionStartEvent | ToolExecutionCompleteEvent | SessionResultEvent | SessionFileFormatEvent | DetectionResultEvent | AgentExecutionEvent;

export interface SessionEventDataMap {
  "agent.execution": AgentExecutionData;
  "detection.result": DetectionResultData;
  "session.format": SessionFileFormatData;
  "session.init": SessionInitData;
  "user.message": MessageData;
  "assistant.message": MessageData;
  "assistant.reasoning": MessageData;
  "tool.execution_start": ToolExecutionStartData;
  "tool.execution_complete": ToolExecutionCompleteData;
  "session.result": SessionResultData;
}

export type SessionEventData<T extends string> = T extends keyof SessionEventDataMap ? SessionEventDataMap[T] : Record<string, unknown>;
export type SessionEventFor<T extends string> = EventMetadata & { type: T; data: SessionEventData<T> };

export interface NativeSessionEvent extends EventMetadata {
  type: `${string}.${string}`;
  data: Record<string, unknown>;
}

export type SessionEvent = CoreSessionEvent | NativeSessionEvent;
export type AgentSession = SessionEvent[];

/** Collector metadata is separate from native engine event metadata. */
export interface SessionProvenance {
  component: string;
  phase: string;
  path: string;
  /** Position in the source's normalized event array, not necessarily a raw line. */
  index: SessionCount;
  /** Ordering key only; numeric native timestamp units are schema-dependent. */
  timestampMs?: number;
  /** Preserves a source event's preexisting provenance field. */
  native?: unknown;
}
