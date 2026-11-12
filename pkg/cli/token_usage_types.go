package cli

import (
	"bytes"
	"encoding/json"
	"time"

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
}

// TokenUsageEntry represents a single line from token-usage.jsonl
type TokenUsageEntry struct {
	Schema    string `json:"_schema,omitempty"` // Self-describing record type, e.g. "token-usage/v0.26.0"
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
	RequestID string `json:"request_id"`
	Purpose   string `json:"purpose,omitempty"`
	// AgentName and CallerSource are AWF caller attribution (x-awf-caller).
	// CallerSource is "awf" for AWF-owned purposes and "caller_header" for a
	// sandbox caller declaration; older records omit both.
	AgentName    string `json:"agent_name,omitempty"`
	CallerSource string `json:"caller_source,omitempty"`
	XInitiator   string `json:"x_initiator,omitempty"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Path         string `json:"path"`
	Status       int    `json:"status"`
	Streaming    bool   `json:"streaming"`
	TokenCoreMetrics
	DurationMs              int             `json:"duration_ms"`
	ResponseBytes           int             `json:"response_bytes"`
	AICreditsThisResponse   json.RawMessage `json:"ai_credits_this_response,omitempty"`
	AICreditsTotal          json.RawMessage `json:"ai_credits_total,omitempty"`
	InputTokensIncludeCache json.RawMessage `json:"input_tokens_include_cache,omitempty"`
	// Steering is the AWF steering notice delivered on this request, if any.
	// Older AWF versions never record it, so nil means "no notice recorded".
	Steering *TokenUsageSteering `json:"steering,omitempty"`
	// SteeringDelivery is the AWF steering notice delivery recorded on this
	// request, written once per conversation per notice. AWF v0.29.11 and older
	// never record it, so nil means "no delivery recorded".
	SteeringDelivery *TokenUsageSteeringDelivery `json:"steering_delivery,omitempty"`
}

// TokenUsageSteering is the steering notice AWF attached to a model request.
type TokenUsageSteering struct {
	Type      string            `json:"type"`      // ai_credit, timeout or token
	Threshold steeringThreshold `json:"threshold"` // 80, 90, 95 or 99 (percent of the budget used)
}

// UnmarshalJSON decodes a steering notice leniently so that an unexpected shape
// never prevents the surrounding token-usage record from being read.
func (s *TokenUsageSteering) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type      json.RawMessage   `json:"type"`
		Threshold steeringThreshold `json:"threshold"`
	}
	*s = TokenUsageSteering{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var steeringType string
	if err := json.Unmarshal(raw.Type, &steeringType); err == nil {
		s.Type = steeringType
	}
	s.Threshold = raw.Threshold
	return nil
}

// valid reports whether the notice carries a known AWF steering type and
// threshold, matching the unified session schema.
func (s *TokenUsageSteering) valid() bool {
	if s == nil {
		return false
	}
	switch s.Type {
	case "ai_credit", "timeout", "token":
	default:
		return false
	}
	switch s.Threshold {
	case 80, 90, 95, 99:
		return true
	default:
		return false
	}
}

// steeringThreshold is a budget percentage that decodes leniently: any value
// other than exactly 80, 90, 95 or 99 (including fractions, strings and other
// percentages) decodes to zero instead of failing the enclosing record.
type steeringThreshold int

// UnmarshalJSON implements json.Unmarshaler.
func (t *steeringThreshold) UnmarshalJSON(data []byte) error {
	*t = 0
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	switch value {
	case 80, 90, 95, 99:
		*t = steeringThreshold(value)
	}
	return nil
}

// Steering delivery triggers recorded by AWF.
const (
	steeringTriggerThreshold  = "threshold"
	steeringTriggerLittleLeft = "little_left"
)

// TokenUsageSteeringDelivery is one delivery of an AWF steering notice to a
// conversation. A little_left notice has no threshold and decodes with a zero
// Threshold; an empty Conversation means AWF could not key the conversation.
type TokenUsageSteeringDelivery struct {
	Type         string            `json:"type"`    // ai_credit or timeout
	Trigger      string            `json:"trigger"` // threshold or little_left
	Threshold    steeringThreshold `json:"threshold"`
	Conversation string            `json:"conversation"`
	First        bool              `json:"first"`
}

// UnmarshalJSON decodes a steering delivery leniently so that an unexpected
// shape never prevents the surrounding token-usage record from being read.
// Deliveries that do not match the AWF contract decode as invalid.
func (d *TokenUsageSteeringDelivery) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type         json.RawMessage `json:"type"`
		Trigger      json.RawMessage `json:"trigger"`
		Threshold    json.RawMessage `json:"threshold"`
		Conversation json.RawMessage `json:"conversation"`
		First        json.RawMessage `json:"first"`
	}
	*d = TokenUsageSteeringDelivery{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	first, firstOK := jsonBool(raw.First)
	conversation, conversationOK := steeringConversation(raw.Conversation)
	if !firstOK || !conversationOK {
		return nil
	}
	trigger := optionalJSONString(raw.Trigger)
	var threshold steeringThreshold
	if len(raw.Threshold) == 0 || json.Unmarshal(raw.Threshold, &threshold) != nil {
		return nil
	}
	isNull := string(bytes.TrimSpace(raw.Threshold)) == "null"
	switch {
	case trigger == steeringTriggerThreshold && threshold != 0:
	case trigger == steeringTriggerLittleLeft && isNull:
	default:
		return nil
	}
	d.Type = optionalJSONString(raw.Type)
	d.Trigger = trigger
	d.Threshold = threshold
	d.Conversation = conversation
	d.First = first
	return nil
}

// MarshalJSON writes the delivery in AWF's token-usage.jsonl form, with a null
// threshold for little_left notices and a null conversation when it is unknown.
func (d TokenUsageSteeringDelivery) MarshalJSON() ([]byte, error) {
	out := struct {
		Type         string  `json:"type"`
		Trigger      string  `json:"trigger"`
		Threshold    *int    `json:"threshold"`
		Conversation *string `json:"conversation"`
		First        bool    `json:"first"`
	}{Type: d.Type, Trigger: d.Trigger, First: d.First}
	out.Threshold, out.Conversation = d.thresholdAndConversation()
	return json.Marshal(out)
}

// thresholdAndConversation returns the delivery threshold and conversation as
// optional values: nil for a little_left threshold or an unkeyed conversation.
func (d TokenUsageSteeringDelivery) thresholdAndConversation() (*int, *string) {
	var threshold *int
	if d.Trigger != steeringTriggerLittleLeft {
		value := int(d.Threshold)
		threshold = &value
	}
	var conversation *string
	if d.Conversation != "" {
		value := d.Conversation
		conversation = &value
	}
	return threshold, conversation
}

// valid reports whether the delivery carries a known AWF steering type and a
// trigger with a matching threshold, matching the unified session schema.
func (d *TokenUsageSteeringDelivery) valid() bool {
	if d == nil {
		return false
	}
	switch d.Type {
	case "ai_credit", "timeout":
	default:
		return false
	}
	switch d.Trigger {
	case steeringTriggerThreshold:
		return d.Threshold != 0
	case steeringTriggerLittleLeft:
		return d.Threshold == 0
	default:
		return false
	}
}

// jsonBool decodes a required JSON boolean, rejecting a missing value or null.
func jsonBool(raw json.RawMessage) (bool, bool) {
	var value *bool
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return false, false
	}
	return *value, true
}

// steeringConversation decodes an AWF conversation key: twelve lowercase hex
// characters, or null when AWF could not key the conversation (returned as "").
func steeringConversation(raw json.RawMessage) (string, bool) {
	if string(bytes.TrimSpace(raw)) == "null" {
		return "", true
	}
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || len(value) != 12 {
		return "", false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", false
		}
	}
	return value, true
}

// SteeringDelivery is one delivery of an AWF steering notice to a conversation,
// read from firewall.token_usage events in the unified session. Threshold is
// nil for a little_left notice and Conversation is nil when AWF could not key
// the conversation.
type SteeringDelivery struct {
	Type         string  `json:"type" console:"header:Type"`
	Trigger      string  `json:"trigger" console:"header:Trigger"`
	Threshold    *int    `json:"threshold" console:"header:Threshold"`
	Conversation *string `json:"conversation" console:"header:Conversation"`
	First        bool    `json:"first" console:"header:First"`
	RequestID    string  `json:"request_id,omitempty" console:"header:Request ID,omitempty"`
	Phase        string  `json:"phase,omitempty" console:"header:Phase,omitempty"`
}

// SteeringNotice is an AWF steering notice delivered on a model request, read
// from firewall.token_usage events in the unified session.
type SteeringNotice struct {
	Type      string `json:"type" console:"header:Type"`
	Threshold int    `json:"threshold" console:"header:Threshold"`
	RequestID string `json:"request_id,omitempty" console:"header:Request ID,omitempty"`
	Phase     string `json:"phase,omitempty" console:"header:Phase,omitempty"`
}

// AmbientContextMetrics captures token footprint for the first LLM invocation.
type AmbientContextMetrics struct {
	InputTokens  int `json:"input_tokens" console:"header:Ambient Input,format:number"`
	CachedTokens int `json:"cached_tokens" console:"header:Ambient Cached,format:number"`
}

// TokenUsageSummary contains aggregated token usage from the firewall proxy
type TokenUsageSummary struct {
	TotalInputTokens       int                         `json:"total_input_tokens" console:"header:Input Tokens,format:number"`
	TotalOutputTokens      int                         `json:"total_output_tokens" console:"header:Output Tokens,format:number"`
	TotalCacheReadTokens   int                         `json:"total_cache_read_tokens" console:"header:Cache Read,format:number"`
	TotalCacheWriteTokens  int                         `json:"total_cache_write_tokens" console:"header:Cache Write,format:number"`
	TotalRequests          int                         `json:"total_requests" console:"header:Requests"`
	TotalSteeringEvents    int                         `json:"total_steering_events,omitempty" console:"header:Steering Events,format:number,omitempty"`
	SteeringEventCounts    map[string]int              `json:"steering_event_counts,omitempty" console:"-"`
	TotalDurationMs        int                         `json:"total_duration_ms"`
	TotalResponseBytes     int                         `json:"total_response_bytes"`
	CacheEfficiency        float64                     `json:"cache_efficiency"`
	TotalAIC               float64                     `json:"total_aic,omitempty"`
	AICFound               bool                        `json:"-"`
	AmbientContext         *AmbientContextMetrics      `json:"ambient_context,omitempty"`
	ByModel                map[string]*ModelTokenUsage `json:"by_model"`
	SubagentModelRequests  []SubagentModelRequest      `json:"subagent_model_requests,omitempty"`
	DeclaredSubagentModels []SubagentModelRequest      `json:"declared_subagent_models,omitempty"`
	SubagentModelActuals   []SubagentModelActual       `json:"subagent_model_actuals,omitempty"`
	AgentUsage             []AgentUsageBreakdown       `json:"agent_usage,omitempty"`
	SteeringNotices        []SteeringNotice            `json:"steering_notices,omitempty" console:"-"`
	// SteeringDeliveries lists every AWF steering notice delivery, one per
	// conversation per notice, read from the unified session.
	SteeringDeliveries []SteeringDelivery `json:"steering_deliveries,omitempty" console:"-"`
	// SteeringNoticesUnavailable is true when the unified session could not be
	// read, so the absence of steering_notices does not mean "no notices".
	SteeringNoticesUnavailable bool     `json:"steering_notices_unavailable,omitempty" console:"-"`
	MismatchCount              int      `json:"mismatch_count,omitempty"`
	Warnings                   []string `json:"warnings,omitempty"`
	agentModels                map[string]*ModelTokenUsage
	endpoint                   string
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
	AgentName       string   `json:"agent_name"`
	RequestedModel  string   `json:"requested_model"`
	ResolvedModel   string   `json:"resolved_model,omitempty"`
	ServedModels    []string `json:"served_models,omitempty"`
	ErrorCode       string   `json:"error_code,omitempty"`
	InvocationCount int      `json:"invocation_count"`
	CompletedCount  int      `json:"completed_count,omitempty"`
	FailedCount     int      `json:"failed_count,omitempty"`
	IncompleteCount int      `json:"incomplete_count,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	Error           string   `json:"error,omitempty"`
	EffectiveModel  string   `json:"effective_model,omitempty"`
	ReasonCode      string   `json:"reason_code,omitempty"`
}

// SubagentModelActual captures model usage observed in token-usage logs.
type SubagentModelActual struct {
	Model         string   `json:"model"`
	Provider      string   `json:"provider,omitempty"`
	Requests      int      `json:"requests"`
	ResolvedModel string   `json:"resolved_model,omitempty"`
	ServedModels  []string `json:"served_models,omitempty"`
	TokenCoreMetrics
	AIC              float64 `json:"aic,omitempty"`
	TotalDurationMs  int     `json:"total_duration_ms,omitempty"`
	agentName        string
	identityEvidence []string
	// sessionModels lists the model ids the session recorded for the agent
	// before identity resolution; nil when unknown.
	sessionModels []string
	// requestModels lists the original request-bearing model ids that resolved
	// to Model; nil when unknown.
	requestModels []string
}

type AgentUsageBreakdown struct {
	AgentName       string   `json:"agent_name"`
	DisplayNames    []string `json:"display_names,omitempty"`
	AgentType       string   `json:"agent_type"`
	SourceEngine    string   `json:"source_engine,omitempty"`
	InstanceCount   int      `json:"instance_count"`
	CompletedCount  int      `json:"completed_count,omitempty"`
	FailedCount     int      `json:"failed_count,omitempty"`
	IncompleteCount int      `json:"incomplete_count,omitempty"`
	RequestedModels []string `json:"requested_models,omitempty"`
	ResolvedModels  []string `json:"resolved_models,omitempty"`
	ServedModels    []string `json:"served_models,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	Requests        int      `json:"requests"`
	TokenCoreMetrics
	AIC                float64           `json:"aic,omitempty"`
	TotalApiDurationMs int               `json:"total_api_duration_ms,omitempty"`
	Models             []AgentModelUsage `json:"models,omitempty"`
	requestUsages      []agentRequestUsage
	sessionModels      []string
	// definitionName is the session's agentName for an instance; when set,
	// public rows are keyed by it and instanceDisplayNames become DisplayNames.
	definitionName       string
	instanceDisplayNames []string
}

type agentRequestUsage struct {
	Model           string
	Timestamp       time.Time
	UsageIncomplete bool
	TokenCoreMetrics
}

type AgentModelUsage struct {
	Model         string `json:"model"`
	ResolvedModel string `json:"resolved_model,omitempty"`
	Requests      int    `json:"requests"`
	TokenCoreMetrics
	AIC float64 `json:"aic,omitempty"`
	// requestModels lists the original request-bearing model ids that resolved
	// to Model.
	requestModels []string
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
// The event name appears under one of four top-level field names or nested payload
// fields depending on the proxy version; the message field is present on steering events.
type proxyEventsEntry struct {
	// Event name appears under one of these four keys or in the nested payload.
	Event          string          `json:"event"`
	Type           string          `json:"type"`
	EventNameSnake string          `json:"event_name"`
	EventNameCamel string          `json:"eventName"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	// Message text (present on steering events).
	Message string `json:"message"`
	// Optional RFC3339/RFC3339Nano timestamp (not always present).
	Timestamp string `json:"timestamp"`
	// Request ID and budget threshold (present on steering events from AWF releases
	// that include gh-aw-firewall#9790).
	RequestID string            `json:"request_id"`
	Threshold steeringThreshold `json:"threshold"`
	// Trigger ("threshold" or "little_left") and the opaque per-run conversation
	// key are present on steering events from AWF releases after v0.29.11. A
	// little_left notice has a null threshold.
	Trigger      json.RawMessage `json:"trigger,omitempty"`
	Conversation json.RawMessage `json:"conversation,omitempty"`
}

// tokenUsageJSONLPath is the relative path within the firewall logs directory
const tokenUsageJSONLPath = "api-proxy-logs/token-usage.jsonl"
const proxyEventsJSONLPath = "api-proxy-logs/events.jsonl"
const proxyEventLogsJSONLPath = "api-proxy-logs/event-logs.jsonl"
const agentUsageJSONPath = "agent_usage.json"
const modelMismatchReasonTokenUsageMissing = "TOKEN_USAGE_MISSING"
const modelMismatchReasonModelNotObserved = "REQUESTED_MODEL_NOT_OBSERVED"
const modelMismatchReasonSubagentFailed = "SUBAGENT_FAILED"
const tokenSteeringEventName = "token_steering"
const timeoutSteeringEventName = "timeout_steering"
const aiCreditSteeringEventName = "ai_credit_steering"
const awfTokenWarningPrefix = "[AWF TOKEN WARNING]"
const awfTimeWarningPrefix = "[AWF TIME WARNING]"
const awfAICreditWarningPrefix = "[AWF AI CREDIT WARNING]"
