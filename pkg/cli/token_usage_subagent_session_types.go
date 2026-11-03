package cli

import (
	"encoding/json"
)

type sessionSubagentEvent struct {
	unifiedDetectionEvent
	AgentID    string          `json:"agentId"`
	Timestamp  json.RawMessage `json:"timestamp"`
	TS         json.RawMessage `json:"ts"`
	Time       json.RawMessage `json:"time"`
	Provenance struct {
		Component     string          `json:"component"`
		Phase         string          `json:"phase"`
		Path          string          `json:"path"`
		TimestampUnit string          `json:"timestampUnit"`
		TimestampMs   json.RawMessage `json:"timestampMs"`
		Index         json.RawMessage `json:"index"`
		Native        struct {
			Path          string          `json:"path"`
			Index         json.RawMessage `json:"index"`
			PersistedPath string          `json:"persistedPath"`
		} `json:"native"`
	} `json:"provenance"`
}

type sessionSubagentData struct {
	SourceEngine     string                                `json:"sourceEngine"`
	SessionID        string                                `json:"sessionId"`
	InvocationID     string                                `json:"invocationId"`
	ToolCallID       string                                `json:"toolCallId"`
	AgentName        string                                `json:"agentName"`
	AgentDisplayName string                                `json:"agentDisplayName"`
	Model            string                                `json:"model"`
	NewModel         string                                `json:"newModel"`
	RequestedModel   string                                `json:"requestedModel"`
	ResolvedModel    string                                `json:"resolvedModel"`
	Outcome          string                                `json:"outcome"`
	Cancelled        bool                                  `json:"cancelled"`
	ErrorCode        string                                `json:"errorCode"`
	ErrorMessage     string                                `json:"errorMessage"`
	ReasoningEffort  string                                `json:"reasoningEffort"`
	RequestID        string                                `json:"requestId"`
	InputTokens      *int                                  `json:"inputTokens"`
	OutputTokens     *int                                  `json:"outputTokens"`
	CacheReadTokens  *int                                  `json:"cacheReadTokens"`
	CacheWriteTokens *int                                  `json:"cacheWriteTokens"`
	Error            json.RawMessage                       `json:"error"`
	Agent            string                                `json:"agent"`
	ToolName         string                                `json:"toolName"`
	Input            json.RawMessage                       `json:"input"`
	Success          *bool                                 `json:"success"`
	Event            json.RawMessage                       `json:"event"`
	AgentMetrics     map[string]sessionSubagentAgentMetric `json:"agentMetrics"`
}

type sessionSubagentAgentMetric struct {
	AgentName          string                                `json:"agentName"`
	AgentDisplayName   string                                `json:"agentDisplayName"`
	TotalNanoAiu       int64                                 `json:"totalNanoAiu"`
	TotalApiDurationMs int                                   `json:"totalApiDurationMs"`
	ModelMetrics       map[string]sessionSubagentModelMetric `json:"modelMetrics"`
}

type sessionSubagentModelMetric struct {
	Requests struct {
		Count *int `json:"count"`
	} `json:"requests"`
	Usage struct {
		InputTokens      int `json:"inputTokens"`
		OutputTokens     int `json:"outputTokens"`
		CacheReadTokens  int `json:"cacheReadTokens"`
		CacheWriteTokens int `json:"cacheWriteTokens"`
		ReasoningTokens  int `json:"reasoningTokens"`
	} `json:"usage"`
	TotalNanoAiu int64 `json:"totalNanoAiu"`
}

type piSubagentEventPayload struct {
	Message struct {
		Model         string          `json:"model"`
		ResponseModel string          `json:"responseModel"`
		Timestamp     json.RawMessage `json:"timestamp"`
		StopReason    string          `json:"stopReason"`
		ErrorMessage  string          `json:"errorMessage"`
		Usage         struct {
			Input      int `json:"input"`
			Output     int `json:"output"`
			CacheRead  int `json:"cacheRead"`
			CacheWrite int `json:"cacheWrite"`
		} `json:"usage"`
	} `json:"message"`
}
