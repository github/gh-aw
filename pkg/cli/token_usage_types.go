package cli

import (
	"encoding/json"

	"github.com/github/gh-aw/pkg/logger"
)

var tokenUsageLog = logger.New("cli:token_usage")

// TokenCoreMetrics is the single source of truth for the token-usage quartet
// shared across per-request, per-model, and per-run representations.
// All JSON tags use snake_case to match the token-usage.jsonl file format.
type TokenCoreMetrics struct {
	InputTokens      int `json:"input_tokens" console:"header:Input,format:number"`
	OutputTokens     int `json:"output_tokens" console:"header:Output,format:number"`
	CacheReadTokens  int `json:"cache_read_tokens" console:"header:Cache Read,format:number"`
	CacheWriteTokens int `json:"cache_write_tokens" console:"header:Cache Write,format:number"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	EffectiveTokens  int `json:"effective_tokens,omitempty"`
}

// TokenUsageEntry represents a single line from token-usage.jsonl
type TokenUsageEntry struct {
	Schema    string `json:"_schema,omitempty"` // Self-describing record type, e.g. "token-usage/v0.26.0"
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
	RequestID string `json:"request_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	Streaming bool   `json:"streaming"`
	TokenCoreMetrics
	DurationMs              int             `json:"duration_ms"`
	ResponseBytes           int             `json:"response_bytes"`
	AICreditsThisResponse   json.RawMessage `json:"ai_credits_this_response,omitempty"`
	AICreditsTotal          json.RawMessage `json:"ai_credits_total,omitempty"`
	InputTokensIncludeCache json.RawMessage `json:"input_tokens_include_cache,omitempty"`
}

// AmbientContextMetrics captures token footprint for the first LLM invocation.
type AmbientContextMetrics struct {
	InputTokens     int `json:"input_tokens" console:"header:Ambient Input,format:number"`
	CachedTokens    int `json:"cached_tokens" console:"header:Ambient Cached,format:number"`
	EffectiveTokens int `json:"effective_tokens,omitempty"`
}

// TokenUsageSummary contains aggregated token usage from the firewall proxy
type TokenUsageSummary struct {
	TotalInputTokens      int                         `json:"total_input_tokens" console:"header:Input Tokens,format:number"`
	TotalOutputTokens     int                         `json:"total_output_tokens" console:"header:Output Tokens,format:number"`
	TotalCacheReadTokens  int                         `json:"total_cache_read_tokens" console:"header:Cache Read,format:number"`
	TotalCacheWriteTokens int                         `json:"total_cache_write_tokens" console:"header:Cache Write,format:number"`
	TotalRequests         int                         `json:"total_requests" console:"header:Requests"`
	TotalSteeringEvents   int                         `json:"total_steering_events,omitempty" console:"header:Steering Events,format:number,omitempty"`
	TotalDurationMs       int                         `json:"total_duration_ms"`
	TotalResponseBytes    int                         `json:"total_response_bytes"`
	CacheEfficiency       float64                     `json:"cache_efficiency"`
	TotalEffectiveTokens  int                         `json:"total_effective_tokens,omitempty"`
	TotalAIC              float64                     `json:"total_aic,omitempty"`
	AICFound              bool                        `json:"-"`
	AmbientContext        *AmbientContextMetrics      `json:"ambient_context,omitempty"`
	ByModel               map[string]*ModelTokenUsage `json:"by_model"`
	SubagentModelRequests []SubagentModelRequest      `json:"subagent_model_requests,omitempty"`
	SubagentModelActuals  []SubagentModelActual       `json:"subagent_model_actuals,omitempty"`
	MismatchCount         int                         `json:"mismatch_count,omitempty"`
	Warnings              []string                    `json:"warnings,omitempty"`
}

// ModelTokenUsage contains per-model token usage statistics
type ModelTokenUsage struct {
	Provider string `json:"provider"`
	TokenCoreMetrics
	Requests      int     `json:"requests" console:"header:Requests"`
	DurationMs    int     `json:"duration_ms"`
	ResponseBytes int     `json:"response_bytes"`
	AIC           float64 `json:"aic,omitempty"`
}

// ModelTokenUsageRow is a table-rendering view of per-model token statistics.
// Keep this row schema limited to the token quartet to preserve output shape.
type ModelTokenUsageRow struct {
	Model            string  `json:"model" console:"header:Model"`
	Provider         string  `json:"provider" console:"header:Provider"`
	InputTokens      int     `json:"input_tokens" console:"header:Input,format:number"`
	OutputTokens     int     `json:"output_tokens" console:"header:Output,format:number"`
	CacheReadTokens  int     `json:"cache_read_tokens" console:"header:Cache Read,format:number"`
	CacheWriteTokens int     `json:"cache_write_tokens" console:"header:Cache Write,format:number"`
	AIC              float64 `json:"aic,omitempty"`
	Requests         int     `json:"requests" console:"header:Requests"`
	AvgDuration      string  `json:"avg_duration" console:"header:Avg Duration"`
}

// SubagentModelRequest captures requested/effective model attribution for a sub-agent.
type SubagentModelRequest struct {
	AgentName       string `json:"agent_name"`
	RequestedModel  string `json:"requested_model"`
	InvocationCount int    `json:"invocation_count"`
	EffectiveModel  string `json:"effective_model,omitempty"`
	ReasonCode      string `json:"reason_code,omitempty"`
}

// SubagentModelActual captures model usage observed in token-usage logs.
type SubagentModelActual struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
	Requests int    `json:"requests"`
}

// agentUsageEntry is the JSON structure written by parse_token_usage.cjs to
// /tmp/gh-aw/agent_usage.json.  It aggregates the total token counts for a run
// and is included in both the "agent" and "usage" artifacts.
type agentUsageEntry struct {
	// Provider and Model fields are only populated when the usage data came from a
	// single model (legacy per-request format written by older versions of the harness).
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// PrimaryModel is the dominant model for runs that used multiple models.
	PrimaryModel string `json:"primary_model"`
	// Raw token counts.
	TokenCoreMetrics
	// AmbientContextTokens is the first-request ambient input token count emitted by parse_token_usage.cjs.
	AmbientContextTokens *int `json:"ambient_context"`
	// AICredits is the pre-computed total AI Credits value written by parse_token_usage.cjs.
	// When present and valid it is used directly so we don't need per-model pricing.
	AICredits json.RawMessage `json:"ai_credits"`
}

// proxyEventsEntry is a JSONL record from api-proxy-logs/events.jsonl.
// The event name appears under one of four field names depending on the proxy version;
// the message field is present on steering events.
type proxyEventsEntry struct {
	// Event name appears under one of these four keys; all are checked.
	Event          string `json:"event"`
	Type           string `json:"type"`
	EventNameSnake string `json:"event_name"`
	EventNameCamel string `json:"eventName"`
	// Message text (present on steering events).
	Message string `json:"message"`
	// Optional RFC3339/RFC3339Nano timestamp (not always present).
	Timestamp string `json:"timestamp"`
}

// tokenUsageJSONLPath is the relative path within the firewall logs directory
const tokenUsageJSONLPath = "api-proxy-logs/token-usage.jsonl"
const proxyEventsJSONLPath = "api-proxy-logs/events.jsonl"
const agentUsageJSONPath = "agent_usage.json"
const modelMismatchReasonTokenUsageMissing = "TOKEN_USAGE_MISSING"
const modelMismatchReasonModelNotObserved = "REQUESTED_MODEL_NOT_OBSERVED"
const subagentStdioWarning = "partial or incorrect data: sub-agent model requests are inferred from agent-stdio.log; use token_usage.jsonl for reliable token consumption"
const tokenSteeringEventName = "token_steering"
const timeoutSteeringEventName = "timeout_steering"
const awfTokenWarningPrefix = "[AWF TOKEN WARNING]"
const awfTimeWarningPrefix = "[AWF TIME WARNING]"
