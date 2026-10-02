export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

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
  durationMs?: number;
  exitCode?: number;
  status?: string;
  is_error?: boolean;
  isError?: boolean;
  mcpServerName?: string;
  [key: string]: unknown;
}

export interface SessionUsage {
  total_tokens?: number;
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
  inputTokens?: number;
  outputTokens?: number;
  cacheCreationInputTokens?: number;
  cacheReadInputTokens?: number;
  input_tokens_include_cache?: boolean;
  [key: string]: unknown;
}

export interface SessionResultData {
  numTurns?: number;
  durationMs?: number;
  totalCostUsd?: number;
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

export type CoreSessionEvent = SessionInitEvent | UserMessageEvent | AssistantMessageEvent | AssistantReasoningEvent | ToolExecutionStartEvent | ToolExecutionCompleteEvent | SessionResultEvent;

export interface SessionEventDataMap {
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
