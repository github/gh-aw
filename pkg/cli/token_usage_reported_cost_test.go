//go:build !integration

package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportedNativeUsageCostsWithoutCatalog(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		aic  float64
	}{
		{"native USD", `{"model":"future-model","cost_usd":0.125}`, 12.5},
		{"native cumulative USD", `{"model":"future-model","total_cost_usd":0.25}`, 25},
		{"zero USD", `{"model":"future-model","cost_usd":0}`, 0},
		{"credits take precedence", `{"ai_credits":3,"total_cost_usd":0.25}`, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.json), &parsed))
			aic, found := parseLegacyUsageAIC(parsed)
			assert.True(t, found)
			assert.InDelta(t, tc.aic, aic, 1e-9)

			var entry agentUsageEntry
			require.NoError(t, json.Unmarshal([]byte(tc.json), &entry))
			summary := buildAgentUsageSummary(entry)
			assert.True(t, summary.AICFound)
			assert.InDelta(t, tc.aic, summary.TotalAIC, 1e-9)
		})
	}
}

func TestRawUsageWithoutReportedCostsIsUnpriced(t *testing.T) {
	for _, input := range []string{
		`{"model":"gpt-5.4","provider":"openai","input_tokens":100000,"output_tokens":10000}`,
		`{"model":"gpt-5.4","cost_usd":-1}`,
		`{"model":"gpt-5.4","cost_usd":null}`,
	} {
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(input), &parsed))
		aic, found := parseLegacyUsageAIC(parsed)
		assert.False(t, found)
		assert.Zero(t, aic)

		var entry agentUsageEntry
		require.NoError(t, json.Unmarshal([]byte(input), &entry))
		summary := buildAgentUsageSummary(entry)
		assert.False(t, summary.AICFound)
		assert.Zero(t, summary.TotalAIC)
	}
}

func TestReportedCumulativeCreditsSupplyMissingDelta(t *testing.T) {
	summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{"future-model": {}}}
	populateAICFromTokenUsageEntries(summary, []TokenUsageEntry{
		{Model: "future-model", AICreditsTotal: json.RawMessage("2.5")},
		{Model: "future-model", AICreditsTotal: json.RawMessage("3")},
	})
	assert.True(t, summary.AICFound)
	assert.InDelta(t, 3.0, summary.TotalAIC, 1e-9)
	assert.InDelta(t, 3.0, summary.ByModel["future-model"].AIC, 1e-9)
}

func TestReportedCumulativeCreditsAgreeAcrossAttribution(t *testing.T) {
	entries := []TokenUsageEntry{
		{Timestamp: "2026-11-13T10:00:02Z", RequestID: "main", Model: "future-model",
			Purpose: "agent", CallerSource: "caller_header", AICreditsTotal: json.RawMessage("1.5")},
		{Timestamp: "2026-11-13T10:00:00Z", RequestID: "classifier", Model: "future-model",
			Purpose: "routing_classification", CallerSource: "awf", AICreditsTotal: json.RawMessage("0.1")},
		{Timestamp: "2026-11-13T10:00:03Z", RequestID: "child-2", Model: "future-model",
			Purpose: "subagent", AgentName: "reader", CallerSource: "caller_header", AICreditsTotal: json.RawMessage("2")},
		{Timestamp: "2026-11-13T10:00:01Z", RequestID: "child-1", Model: "future-model",
			Purpose: "subagent", AgentName: "reader", CallerSource: "caller_header", AICreditsTotal: json.RawMessage("0.5")},
	}
	summary := &TokenUsageSummary{
		ByModel: map[string]*ModelTokenUsage{"future-model": {}},
		AgentUsage: []AgentUsageBreakdown{{
			AgentName: "reader", AgentType: "subagent", SourceEngine: "pi",
			Models: []AgentModelUsage{{Model: "future-model"}},
		}},
	}
	populateAICFromTokenUsageEntries(summary, entries)
	assert.InDelta(t, 2.0, summary.TotalAIC, 1e-9)
	matchPiAgentUsageCredits(summary, entries, newModelIdentityResolver(""))
	require.Len(t, summary.AgentUsage, 2)
	assert.InDelta(t, 0.9, summary.AgentUsage[0].AIC, 1e-9)
	assert.InDelta(t, 1, summary.AgentUsage[1].AIC, 1e-9)
	summary.Warnings = nil
	reconcileAgentUsageCredits(summary, entries)
	assert.Empty(t, summary.Warnings, "classifier credits must be excluded consistently")

	routing := &ModelRoutingSummary{}
	aggregateModelRoutingCosts(routing, []modelRoutingRecord{
		{RequestID: "main", Routed: "deviated"},
		{RequestID: "child-1", Routed: "as_selected"},
		{RequestID: "child-2", Routed: "as_selected"},
	}, entries)
	assert.InDelta(t, 0.1, routing.ClassifierCost.AIC, 1e-9)
	assert.InDelta(t, 0.9, routing.SelectedModelCost.AIC, 1e-9)
	assert.InDelta(t, 1, routing.DeviatedTrafficCost.AIC, 1e-9)
	assert.Empty(t, entries[0].AICreditsThisResponse, "reported records must not be rewritten")
}
