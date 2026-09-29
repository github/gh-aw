package cli

import (
	"fmt"
	"sort"
	"strings"
)

// maxConsoleFrictionEvents caps the event list printed to the console; the full
// list always remains available in the audit JSON.
const maxConsoleFrictionEvents = 20

// Friction attribution states, ordered from strongest to weakest evidence.
const (
	// FrictionStateMeasured means the cost was read directly off a record that
	// describes the friction event itself.
	FrictionStateMeasured = "measured"
	// FrictionStateCausal means the cost was taken from the model invocation
	// that the friction event demonstrably caused.
	FrictionStateCausal = "causal"
	// FrictionStateStatistical means the cost was apportioned from the mean
	// healthy-invocation cost of the run.
	FrictionStateStatistical = "statistical"
	// FrictionStateUnavailable means the driver was detected but no cost could
	// be attributed with the data available.
	FrictionStateUnavailable = "unavailable"
	// FrictionStateUnsupported means the driver cannot express this dimension.
	FrictionStateUnsupported = "unsupported"
)

// FrictionCostSummary is the precomputed estimated avoidable marginal cost
// attributable to execution-friction events in a run. It is computed once in
// the conclusion job and consumed verbatim when the section is present.
type FrictionCostSummary struct {
	MeasurementState        string                       `json:"measurement_state"`
	CanonicalUnit           string                       `json:"canonical_unit,omitempty"`
	Sources                 []string                     `json:"sources,omitempty"`
	TotalEvents             int                          `json:"total_events"`
	TotalOccurrences        int                          `json:"total_occurrences"`
	CountedOccurrences      int                          `json:"counted_occurrences"`
	SuppressedOccurrences   int                          `json:"suppressed_occurrences"`
	LinkedInvocations       int                          `json:"linked_invocations,omitempty"`
	UnattributedOccurrences int                          `json:"unattributed_occurrences,omitempty"`
	Cost                    FrictionCost                 `json:"cost"`
	TotalRunAIC             *float64                     `json:"total_run_aic,omitempty"`
	TotalRunAICPartial      bool                         `json:"total_run_aic_partial,omitempty"`
	FrictionRatio           *float64                     `json:"friction_ratio,omitempty"`
	DimensionStates         map[string]string            `json:"dimension_states,omitempty"`
	Uncertainty             map[string]FrictionUncertain `json:"uncertainty,omitempty"`
	Drivers                 []FrictionDriverSummary      `json:"drivers,omitempty"`
	Groups                  []FrictionGroup              `json:"groups,omitempty"`
	Events                  []FrictionEvent              `json:"events,omitempty"`
	EventsTruncated         bool                         `json:"events_truncated,omitempty"`
	UnmeasuredDrivers       []FrictionUnmeasuredDriver   `json:"unmeasured_drivers,omitempty"`
	IgnoredTokenRecords     int                          `json:"ignored_token_records,omitempty"`
	// Derived marks a summary synthesized by the CLI from raw logs because the
	// run predates precomputed friction. It is never set by the conclusion job.
	Derived bool `json:"derived,omitempty"`
}

// FrictionCost holds the friction cost expressed in every supported dimension.
// AIC is canonical; other dimensions are populated only where the driver
// supports them.
type FrictionCost struct {
	AIC       float64        `json:"aic"`
	Tokens    FrictionTokens `json:"tokens"`
	Turns     int            `json:"turns"`
	ToolCalls int            `json:"tool_calls"`
	LatencyMS int64          `json:"latency_ms"`
}

// FrictionTokens breaks friction token cost down by token class.
type FrictionTokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
	Total      int64 `json:"total"`
}

// FrictionUncertain records how confident a single cost dimension is.
type FrictionUncertain struct {
	State           string   `json:"state"`
	Method          string   `json:"method,omitempty"`
	Confidence      string   `json:"confidence,omitempty"`
	RelativeError   *float64 `json:"relative_error,omitempty"`
	LowerBound      *float64 `json:"lower_bound,omitempty"`
	UpperBound      *float64 `json:"upper_bound,omitempty"`
	ConfidenceLevel *float64 `json:"confidence_level,omitempty"`
	SampleSize      int      `json:"sample_size,omitempty"`
	Basis           string   `json:"basis,omitempty"`
}

// FrictionDriverSummary aggregates every event produced by one friction driver.
type FrictionDriverSummary struct {
	Driver                string       `json:"driver" console:"header:Driver"`
	Class                 string       `json:"class,omitempty" console:"header:Class"`
	Source                string       `json:"source,omitempty" console:"header:Source"`
	Events                int          `json:"events" console:"-"`
	Occurrences           int          `json:"occurrences" console:"header:Occurrences"`
	CountedOccurrences    int          `json:"counted_occurrences" console:"header:Counted"`
	SuppressedOccurrences int          `json:"suppressed_occurrences" console:"header:Suppressed,omitempty"`
	State                 string       `json:"state" console:"header:State"`
	Cost                  FrictionCost `json:"cost" console:"-"`
}

// FrictionGroup describes one causal group and how overlapping observations
// from different sources were de-duplicated.
type FrictionGroup struct {
	GroupID               string   `json:"group_id"`
	PrimarySource         string   `json:"primary_source,omitempty"`
	EventIDs              []string `json:"event_ids,omitempty"`
	EventIDsTruncated     bool     `json:"event_ids_truncated,omitempty"`
	TotalOccurrences      int      `json:"total_occurrences"`
	CountedOccurrences    int      `json:"counted_occurrences"`
	SuppressedOccurrences int      `json:"suppressed_occurrences"`
	Rule                  string   `json:"rule,omitempty"`
}

// FrictionEvent is a single attributed friction occurrence or aggregate record.
type FrictionEvent struct {
	ID                    string            `json:"id"`
	Driver                string            `json:"driver"`
	Source                string            `json:"source,omitempty"`
	GroupID               string            `json:"group_id,omitempty"`
	Label                 string            `json:"label,omitempty"`
	Detail                string            `json:"detail,omitempty"`
	Timestamp             string            `json:"timestamp,omitempty"`
	Occurrences           int               `json:"occurrences"`
	CountedOccurrences    int               `json:"counted_occurrences"`
	SuppressedOccurrences int               `json:"suppressed_occurrences"`
	SuppressedBy          string            `json:"suppressed_by,omitempty"`
	AttributionClass      string            `json:"attribution_class,omitempty"`
	State                 string            `json:"state"`
	EstimationMethod      string            `json:"estimation_method,omitempty"`
	LowerBoundAIC         *float64          `json:"lower_bound_aic,omitempty"`
	UpperBoundAIC         *float64          `json:"upper_bound_aic,omitempty"`
	Confidence            string            `json:"confidence,omitempty"`
	DimensionStates       map[string]string `json:"dimension_states,omitempty"`
	Cost                  FrictionCost      `json:"cost"`
}

// FrictionUnmeasuredDriver documents a driver that produced no cost, and why.
type FrictionUnmeasuredDriver struct {
	Driver string `json:"driver"`
	Reason string `json:"reason"`
}

// frictionDisplayValue formats friction AIC for compact table display,
// returning "" when friction was not measured so table renderers fall back to
// their standard placeholder.
func frictionDisplayValue(f *FrictionCostSummary) string {
	if f == nil || f.MeasurementState == FrictionStateUnavailable {
		return ""
	}
	return formatAICValue(f.Cost.AIC)
}

// frictionSummaryLine renders the one-line aggregate shown in console metrics.
func frictionSummaryLine(f *FrictionCostSummary) string {
	if f == nil {
		return ""
	}
	if f.MeasurementState == FrictionStateUnavailable && f.TotalOccurrences == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("occurrences=%d", f.CountedOccurrences)}
	if f.MeasurementState != FrictionStateUnavailable {
		parts = append(parts, "aic="+formatAICValue(f.Cost.AIC))
		if f.FrictionRatio != nil {
			parts = append(parts, fmt.Sprintf("ratio=%.1f%%", *f.FrictionRatio*100))
		}
		if uncertainty, ok := f.Uncertainty["aic"]; ok && uncertainty.LowerBound != nil && uncertainty.UpperBound != nil {
			parts = append(parts, "range="+formatAICValue(*uncertainty.LowerBound)+"–"+formatAICValue(*uncertainty.UpperBound)+" AIC")
		}
		parts = append(parts, frictionCostDimensionParts(f.Cost)...)
	}
	parts = append(parts, "state="+f.MeasurementState)
	if rel := frictionRelativeError(f); rel != "" {
		parts = append(parts, "±"+rel)
	}
	if f.SuppressedOccurrences > 0 {
		parts = append(parts, fmt.Sprintf("deduped=%d", f.SuppressedOccurrences))
	}
	if f.Derived {
		parts = append(parts, "derived-from-logs")
	}
	return strings.Join(parts, " ")
}

func frictionCostDimensionParts(cost FrictionCost) []string {
	var parts []string
	if cost.Tokens.Total > 0 {
		parts = append(parts, fmt.Sprintf("tokens=%d", cost.Tokens.Total))
	}
	if cost.Turns > 0 {
		parts = append(parts, fmt.Sprintf("turns=%d", cost.Turns))
	}
	if cost.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("tool-calls=%d", cost.ToolCalls))
	}
	if cost.LatencyMS > 0 {
		parts = append(parts, fmt.Sprintf("latency=%dms", cost.LatencyMS))
	}
	return parts
}

// frictionRelativeError renders the canonical-dimension relative error as a
// percentage, or "" when the estimate carries no usable error bound.
func frictionRelativeError(f *FrictionCostSummary) string {
	if f == nil {
		return ""
	}
	u, ok := f.Uncertainty["aic"]
	if !ok || u.RelativeError == nil || *u.RelativeError <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%%", *u.RelativeError*100)
}

// formatAICValue renders AI credits with enough precision to stay useful for
// small friction values without printing noise for large ones.
func formatAICValue(v float64) string {
	switch {
	case v == 0:
		return "0"
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 1:
		return fmt.Sprintf("%.2f", v)
	default:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
	}
}

// deriveFrictionFromLogs builds a minimal friction summary for historical runs
// that predate precomputed friction. Only directly observable dimensions are
// reported: AIC is deliberately left unavailable because the causal and
// statistical attribution models require the per-invocation token-usage stream
// that the conclusion job reads at run time.
func deriveFrictionFromLogs(mcpUsage *MCPToolUsageData, session *usageActivitySession) *FrictionCostSummary {
	if mcpUsage == nil && session == nil {
		return nil
	}

	var events []FrictionEvent
	sources := map[string]bool{}
	gatewayFailures := 0

	if mcpUsage != nil {
		sources["mcp_gateway"] = true
		for _, call := range mcpUsage.ToolCalls {
			if !isFrictionToolCallStatus(call.Status) && call.Error == "" {
				continue
			}
			gatewayFailures++
			id := call.ToolCallID
			if id == "" {
				id = fmt.Sprintf("%s/%s@%s", call.ServerName, call.ToolName, call.Timestamp)
			}
			events = append(events, FrictionEvent{
				ID:                 "mcp_tool_error:" + id,
				Driver:             "mcp_tool_error",
				Source:             "mcp_gateway",
				GroupID:            "tool_failure",
				Label:              fmt.Sprintf("%s/%s", call.ServerName, call.ToolName),
				Detail:             call.Error,
				Timestamp:          call.Timestamp,
				Occurrences:        1,
				CountedOccurrences: 1,
				State:              FrictionStateUnavailable,
				DimensionStates: map[string]string{
					"aic":        FrictionStateUnavailable,
					"tokens":     FrictionStateUnavailable,
					"turns":      FrictionStateUnsupported,
					"tool_calls": FrictionStateMeasured,
					"latency_ms": FrictionStateUnavailable,
				},
				Cost: FrictionCost{ToolCalls: 1},
			})
		}
		if mcpUsage.Integrity != nil && mcpUsage.Integrity.TotalFiltered > 0 {
			events = append(events, FrictionEvent{
				ID:                 "integrity_filter:aggregate",
				Driver:             "integrity_filter",
				Source:             "mcp_gateway",
				GroupID:            "integrity_filter",
				Label:              "integrity-filtered responses",
				Occurrences:        mcpUsage.Integrity.TotalFiltered,
				CountedOccurrences: mcpUsage.Integrity.TotalFiltered,
				State:              FrictionStateUnavailable,
				DimensionStates: map[string]string{
					"aic":        FrictionStateUnavailable,
					"tokens":     FrictionStateUnavailable,
					"turns":      FrictionStateUnsupported,
					"tool_calls": FrictionStateMeasured,
					"latency_ms": FrictionStateUnsupported,
				},
				Cost: FrictionCost{ToolCalls: mcpUsage.Integrity.TotalFiltered},
			})
		}
	}

	if session != nil {
		sources["agent_session"] = true
		// Apply the same fidelity rule as the precomputed model: the gateway
		// owns overlapping tool failures, the session contributes only excess.
		if excess := session.FailedToolExecutions - gatewayFailures; session.FailedToolExecutions > 0 {
			counted := max(excess, 0)
			event := FrictionEvent{
				ID:                    "session_tool_failure:aggregate",
				Driver:                "session_tool_failure",
				Source:                "agent_session",
				GroupID:               "tool_failure",
				Label:                 "agent session tool executions",
				Occurrences:           session.FailedToolExecutions,
				CountedOccurrences:    counted,
				SuppressedOccurrences: session.FailedToolExecutions - counted,
				State:                 FrictionStateUnavailable,
				DimensionStates: map[string]string{
					"aic":        FrictionStateUnavailable,
					"tokens":     FrictionStateUnavailable,
					"turns":      FrictionStateUnsupported,
					"tool_calls": FrictionStateMeasured,
					"latency_ms": FrictionStateUnsupported,
				},
				Cost: FrictionCost{ToolCalls: counted},
			}
			if event.SuppressedOccurrences > 0 {
				event.SuppressedBy = "causal-group:tool_failure"
			}
			events = append(events, event)
		}
	}

	if len(events) == 0 {
		return nil
	}

	summary := &FrictionCostSummary{
		MeasurementState: FrictionStateUnavailable,
		CanonicalUnit:    "aic",
		Sources:          sortedKeys(sources),
		TotalEvents:      len(events),
		DimensionStates: map[string]string{
			"aic":        FrictionStateUnavailable,
			"tokens":     FrictionStateUnavailable,
			"turns":      FrictionStateUnavailable,
			"tool_calls": FrictionStateMeasured,
			"latency_ms": FrictionStateUnavailable,
		},
		Events:  events,
		Derived: true,
	}
	byDriver := map[string]*FrictionDriverSummary{}
	for _, event := range events {
		summary.TotalOccurrences += event.Occurrences
		summary.CountedOccurrences += event.CountedOccurrences
		summary.SuppressedOccurrences += event.SuppressedOccurrences
		summary.Cost.ToolCalls += event.Cost.ToolCalls
		driver, ok := byDriver[event.Driver]
		if !ok {
			driver = &FrictionDriverSummary{Driver: event.Driver, Source: event.Source, Class: event.GroupID, State: FrictionStateUnavailable}
			byDriver[event.Driver] = driver
		}
		driver.Events++
		driver.Occurrences += event.Occurrences
		driver.CountedOccurrences += event.CountedOccurrences
		driver.SuppressedOccurrences += event.SuppressedOccurrences
		driver.Cost.ToolCalls += event.Cost.ToolCalls
	}
	for _, name := range sortedDriverNames(byDriver) {
		summary.Drivers = append(summary.Drivers, *byDriver[name])
	}
	return summary
}

// deriveFrictionFallback returns a historical summary only when it attributes
// at least one counted friction occurrence.
func deriveFrictionFallback(mcpUsage *MCPToolUsageData, session *usageActivitySession) *FrictionCostSummary {
	summary := deriveFrictionFromLogs(mcpUsage, session)
	if summary == nil || summary.CountedOccurrences == 0 {
		return nil
	}
	return summary
}

// isFrictionToolCallStatus reports whether an MCP tool call status indicates a
// failed call.
func isFrictionToolCallStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed", "failure":
		return true
	default:
		return false
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedDriverNames(drivers map[string]*FrictionDriverSummary) []string {
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
