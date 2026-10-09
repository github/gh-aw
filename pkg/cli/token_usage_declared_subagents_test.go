//go:build !integration

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeclaredSubagentModelAudit(t *testing.T) {
	for _, test := range []struct {
		name     string
		model    string
		provider string
		reason   string
	}{
		{"unused model", "gpt-5.6-luna", "github-copilot", modelMismatchReasonModelNotObserved},
		{"alias resolves", "claude-haiku-4.5", "github-copilot", ""},
		{"qualified observation", "github-copilot/claude-haiku-4.5", "github-copilot", ""},
		{"wrong provider", "claude-haiku-4.5", "anthropic", modelMismatchReasonModelNotObserved},
		{"missing evidence", "", "", modelMismatchReasonTokenUsageMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"reader","model":"small","patterns":["copilot/*haiku*"]}]}`), 0600))
			summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{}}
			if test.model != "" {
				summary.ByModel[test.model] = &ModelTokenUsage{Provider: test.provider, Requests: 1}
			}
			augmentDeclaredSubagentModels(dir, summary)
			require.Len(t, summary.DeclaredSubagentModels, 1)
			row := summary.DeclaredSubagentModels[0]
			require.Equal(t, test.reason, row.ReasonCode)
			require.Zero(t, row.InvocationCount, "model presence is not proof of invocation")
			findings := generateSubagentModelFindings(summary)
			if test.reason == modelMismatchReasonModelNotObserved {
				require.Len(t, findings, 1)
				require.Equal(t, AuditFindingSubagentModelNotObserved, findings[0].Code)
			} else {
				require.Empty(t, findings)
			}
		})
	}
}

func TestDeclaredExperimentalSubagentModelAudit(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"reader","model":"small","patterns":["github-copilot/gpt-5-mini","github-copilot/gpt-5.4"]}]}`), 0600))
	summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{
		"gpt-5-mini": {Provider: "github-copilot", Requests: 1},
	}}
	augmentDeclaredSubagentModels(dir, summary)
	require.Len(t, summary.DeclaredSubagentModels, 1)
	row := summary.DeclaredSubagentModels[0]
	require.Equal(t, "small", row.RequestedModel)
	require.Equal(t, "gpt-5-mini", row.EffectiveModel)
	require.Empty(t, row.ReasonCode)
	require.Zero(t, row.InvocationCount, "matching a possible experiment model is not proof of delegation")
	require.Empty(t, generateSubagentModelFindings(summary))
}

func TestMatchesDeclaredModelEffectiveIDs(t *testing.T) {
	for _, test := range []struct {
		name, pattern, observed, provider string
		want                              bool
	}{
		{"Claude dated version ID", "github-copilot/claude-haiku-4.5", "claude-haiku-4-5-20251001", "github-copilot", true},
		{"OpenAI dated model ID", "copilot/gpt-4o-mini", "gpt-4o-mini-2024-07-18", "github-copilot", true},
		{"unrelated model version", "copilot/gpt-4o-mini", "gpt-4o-mini-audio-2024-07-18", "github-copilot", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, matchesDeclaredModel(test.pattern, test.observed, test.provider))
		})
	}
}

func TestModelIdentityResolverFoldsAliasAndDatedIDs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"quick-checker","model":"small","patterns":["copilot/gpt-5.4-mini"]}]}`), 0600))
	resolver := newModelIdentityResolver(dir)
	require.Equal(t, "gpt-5.4-mini", resolver.resolve("small", "", []string{"small", "gpt-5.4-mini-2026-03-17"}))
	require.Equal(t, "claude-haiku-4.5", normalizeModelIdentity("claude-haiku-4-5-20251001"))

	actuals := resolveSubagentActualModels([]SubagentModelActual{
		{Model: "small", Requests: 4, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 100}},
		{Model: "gpt-5.4-mini", AIC: 1.144},
	}, resolver)
	require.Len(t, actuals, 1)
	require.Equal(t, "gpt-5.4-mini", actuals[0].Model)
	require.Equal(t, 4, actuals[0].Requests)
	require.InDelta(t, 1.144, actuals[0].AIC, 0.000001)
	require.Empty(t, actuals[0].ServedModels)
	withWireID := resolveSubagentActualModels(actuals, resolver, "gpt-5.4-mini-2026-03-17")
	require.Equal(t, []string{"gpt-5.4-mini-2026-03-17"}, withWireID[0].ServedModels)
}

func TestResolveAgentUsagePrefersPerAgentModelEvidence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"worker","model":"small","patterns":["copilot/gpt-5.4-mini"]}]}`), 0600))
	agents := []AgentUsageBreakdown{{
		AgentName: "worker", AgentType: "subagent", RequestedModels: []string{"small"},
		Models: []AgentModelUsage{
			{Model: "small", Requests: 4, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 40}},
			{Model: "gpt-5.4-mini", AIC: 1.144},
		},
	}}

	resolved := resolveAgentUsageModels(agents, newModelIdentityResolver(dir), "claude-haiku-4.5", "gpt-5.4-mini-2026-03-17")
	require.Len(t, resolved[0].Models, 1)
	require.Equal(t, "gpt-5.4-mini", resolved[0].Models[0].Model)
	require.Equal(t, 4, resolved[0].Models[0].Requests)
	require.Equal(t, 40, resolved[0].Models[0].InputTokens)
	require.InDelta(t, 1.144, resolved[0].Models[0].AIC, 0.000001)
	require.Contains(t, resolved[0].ServedModels, "gpt-5.4-mini-2026-03-17")
	require.NotContains(t, resolved[0].ServedModels, "claude-haiku-4.5")
}

func TestSubagentAttributionDoesNotBorrowAnotherAgentsModel(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"worker","model":"small","patterns":["copilot/gpt-5.4-mini"]}]}`), 0600))
	session := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"subagent.started","agentId":"worker","data":{"agentDisplayName":"worker","model":"small"}}`) +
		subagentSessionRecord(`{"type":"session.shutdown","data":{"agentMetrics":{"main":{"modelMetrics":{"claude-haiku-4.5":{"requests":{"count":1}}}},"worker":{"agentDisplayName":"worker","modelMetrics":{"small":{"requests":{"count":4},"usage":{"inputTokens":40}},"gpt-5.4-mini":{"requests":{"count":0},"totalNanoAiu":1144000000}}}}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-session.jsonl"), []byte(session), 0600))
	summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{
		"claude-haiku-4.5": {Provider: "github-copilot", Requests: 1},
	}}

	augmentSubagentModelAttribution(dir, summary)
	require.Len(t, summary.SubagentModelRequests, 1)
	require.Equal(t, "gpt-5.4-mini", summary.SubagentModelRequests[0].EffectiveModel)
	require.NotContains(t, summary.SubagentModelRequests[0].ServedModels, "claude-haiku-4.5")
	require.Len(t, summary.SubagentModelActuals, 1)
	require.Equal(t, "gpt-5.4-mini", summary.SubagentModelActuals[0].Model)
	require.Equal(t, 4, summary.SubagentModelActuals[0].Requests)
	require.Equal(t, 40, summary.SubagentModelActuals[0].InputTokens)
	require.InDelta(t, 1.144, summary.SubagentModelActuals[0].AIC, 0.000001)
}

func TestPiStructuredSubagentModelAttribution(t *testing.T) {
	dir := t.TempDir()
	session := `{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}` + "\n" +
		`{"type":"pi.subagent_dispatch","data":{"invocationId":"call-1","agent":"reader","requestedModel":"small","resolvedModel":"claude-haiku-4.5"}}` + "\n" +
		`{"type":"pi.subagent_event","data":{"invocationId":"call-1","event":{"message":{"model":"claude-haiku-4-5-20251001","usage":{"input":10,"output":2}}}}}` + "\n" +
		`{"type":"pi.subagent_result","data":{"invocationId":"call-1","agent":"reader","outcome":"completed"}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-session.jsonl"), []byte(session), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("{\"type\":\"gh_aw_subagent_dispatch\",\"agent\":\"reader\",\"requested_model\":\"small\",\"resolved_model\":\"claude-haiku-4.5\"}\n"), 0600))
	summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{"claude-haiku-4.5": {Provider: "github-copilot", Requests: 1}}}
	augmentSubagentModelAttribution(dir, summary)
	require.Len(t, summary.SubagentModelRequests, 1)
	row := summary.SubagentModelRequests[0]
	require.Equal(t, "small", row.RequestedModel)
	require.Equal(t, "claude-haiku-4.5", row.ResolvedModel)
	require.Equal(t, row.ResolvedModel, row.EffectiveModel)
	require.Equal(t, 1, row.CompletedCount)
	require.Empty(t, row.ReasonCode)
	require.Zero(t, summary.MismatchCount)
}

func TestPiStructuredSubagentUsageAndTerminalResult(t *testing.T) {
	dir := t.TempDir()
	session := `{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}` + "\n" +
		`{"type":"pi.subagent_dispatch","data":{"invocationId":"call-1","agent":"reader","requestedModel":"small","resolvedModel":"claude-haiku-4.5"}}` + "\n" +
		`{"type":"pi.subagent_event","data":{"invocationId":"call-1","event":{"message":{"model":"claude-haiku-4-5-20251001","usage":{"input":10,"output":2,"cacheRead":3,"cacheWrite":4}}}}}` + "\n" +
		`{"type":"pi.subagent_result","data":{"invocationId":"call-1","agent":"reader","outcome":"completed"}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-session.jsonl"), []byte(session), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("● Fake (model: wrong)"), 0600))
	summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{
		"claude-haiku-4-5-20251001": {Provider: "github-copilot", Requests: 2},
		"gpt-5.6-luna":              {Provider: "github-copilot", Requests: 4},
	}}
	augmentSubagentModelAttribution(dir, summary)
	require.Empty(t, summary.Warnings)
	require.Len(t, summary.SubagentModelRequests, 1)
	require.Equal(t, 1, summary.SubagentModelRequests[0].CompletedCount)
	require.Equal(t, "claude-haiku-4.5", summary.SubagentModelRequests[0].EffectiveModel)
	require.Empty(t, summary.SubagentModelRequests[0].ReasonCode)
	require.Len(t, summary.SubagentModelActuals, 1)
	require.Equal(t, "claude-haiku-4.5", summary.SubagentModelActuals[0].Model)
	require.Equal(t, 1, summary.SubagentModelActuals[0].Requests)
	require.Equal(t, 10, summary.SubagentModelActuals[0].InputTokens)
	require.Equal(t, 2, summary.SubagentModelActuals[0].OutputTokens)
}

func TestMatchPiAgentUsageCreditsUsesResponseModelAndProxyTimeOrder(t *testing.T) {
	resolver := newModelIdentityResolver("")
	summary := &TokenUsageSummary{AgentUsage: []AgentUsageBreakdown{
		{AgentName: "first", AgentType: "subagent", SourceEngine: "pi", requestUsages: []agentRequestUsage{{
			Model: "claude-haiku-4-5-20251001", Timestamp: time.Date(2026, 3, 17, 10, 1, 0, 0, time.UTC),
			TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10, OutputTokens: 2},
		}}},
		{AgentName: "second", AgentType: "subagent", SourceEngine: "pi", requestUsages: []agentRequestUsage{{
			Model: "claude-haiku-4-5-20251001", Timestamp: time.Date(2026, 3, 17, 10, 3, 0, 0, time.UTC),
			TokenCoreMetrics: TokenCoreMetrics{InputTokens: 20, OutputTokens: 4},
		}}},
	}}
	entries := []TokenUsageEntry{
		{Timestamp: "2026-03-17T10:04:00Z", Provider: "anthropic", Model: "claude-haiku-4-5-20251001", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 20, OutputTokens: 4}, AICreditsThisResponse: json.RawMessage(`0.7`)},
		{Timestamp: "2026-03-17T10:02:00Z", Provider: "anthropic", Model: "claude-haiku-4-5-20251001", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10, OutputTokens: 2}, AICreditsThisResponse: json.RawMessage(`0.5`)},
	}

	matchPiAgentUsageCredits(summary, entries, resolver)
	require.Empty(t, summary.Warnings)
	require.InDelta(t, 0.5, summary.AgentUsage[0].AIC, 0.000001)
	require.InDelta(t, 0.7, summary.AgentUsage[1].AIC, 0.000001)
	require.Len(t, summary.AgentUsage, 3)
	require.Equal(t, "main", summary.AgentUsage[2].AgentType)

	unmatched := &TokenUsageSummary{AgentUsage: []AgentUsageBreakdown{{
		AgentName: "reader", AgentType: "subagent", SourceEngine: "pi",
		requestUsages: []agentRequestUsage{{Model: "claude-haiku-4-5-20251001", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 1}}},
	}}}
	matchPiAgentUsageCredits(unmatched, entries[:1], resolver)
	require.Len(t, unmatched.Warnings, 1)
	require.Contains(t, unmatched.Warnings[0], "credits were not inferred")
	require.Zero(t, unmatched.AgentUsage[0].AIC)
}

func TestMatchPiAgentUsageCreditsKeepsMainAgentWhenOneChildDoesNotMatch(t *testing.T) {
	entries := []TokenUsageEntry{
		{Provider: "anthropic", Model: "claude-haiku-4-5-20251001", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10}, AICreditsThisResponse: json.RawMessage(`0.6`)},
		{Provider: "github-copilot", Model: "gpt-5.6-luna", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 30}, AICreditsThisResponse: json.RawMessage(`0.1`)},
		{Provider: "github-copilot", Model: "gpt-5.6-luna", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 31}, AICreditsThisResponse: json.RawMessage(`0.1`)},
		{Provider: "github-copilot", Model: "gpt-5.6-luna", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 32}, AICreditsThisResponse: json.RawMessage(`0.12`)},
	}
	summary := &TokenUsageSummary{AgentUsage: []AgentUsageBreakdown{
		{
			AgentName: "file-summarizer", AgentType: "subagent", SourceEngine: "pi",
			requestUsages: []agentRequestUsage{{
				Model:            "claude-haiku-4-5-20251001",
				TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10},
			}},
		},
		{
			AgentName: "quick-checker", AgentType: "subagent", SourceEngine: "pi",
			RequestedModels: []string{"small"},
			ResolvedModels:  []string{"claude-haiku-4.5"},
			requestUsages: []agentRequestUsage{{
				Model:            "claude-haiku-4-5-20251001",
				TokenCoreMetrics: TokenCoreMetrics{InputTokens: 20},
			}},
		},
	}}

	matchPiAgentUsageCredits(summary, entries, newModelIdentityResolver(""))

	require.Len(t, summary.Warnings, 1)
	require.Contains(t, summary.Warnings[0], "quick-checker")
	require.Len(t, summary.AgentUsage, 3)
	main := summary.AgentUsage[2]
	require.Equal(t, "main", main.AgentType)
	require.Equal(t, 3, main.Requests)
	require.InDelta(t, 0.32, main.AIC, 0.000001)
	require.NotContains(t, main.Models, AgentModelUsage{Model: "claude-haiku-4-5-20251001"})
}

func TestReconcileAgentUsageCreditsNamesEndpoint(t *testing.T) {
	summary := &TokenUsageSummary{
		AICFound: true, TotalAIC: 2.576, endpoint: "/responses",
		AgentUsage: []AgentUsageBreakdown{{AgentName: "main", AIC: 2.743}},
	}
	reconcileAgentUsageCredits(summary, nil)
	require.Len(t, summary.Warnings, 1)
	require.Contains(t, summary.Warnings[0], "unknown endpoint")
	require.Contains(t, summary.Warnings[0], "differ from non-classifier proxy total")
}

func TestReconcileAgentUsageCreditsIgnoresClassifierEndpoint(t *testing.T) {
	summary := &TokenUsageSummary{
		AICFound: true, TotalAIC: 17.058, endpoint: "/responses",
		AgentUsage: []AgentUsageBreakdown{{AgentName: "main", AIC: 17.018}},
	}
	entries := []TokenUsageEntry{
		{Purpose: "routing_classification", Path: "/responses", AICreditsThisResponse: json.RawMessage(`0.04`)},
		{Purpose: "agent", Path: "/v1/messages", AICreditsThisResponse: json.RawMessage(`17.018`)},
	}

	reconcileAgentUsageCredits(summary, entries)

	require.Empty(t, summary.Warnings)
}

func TestReconcileAgentUsageCreditsNamesAgentEndpoint(t *testing.T) {
	summary := &TokenUsageSummary{
		AICFound: true, TotalAIC: 17.118, endpoint: "/responses",
		AgentUsage: []AgentUsageBreakdown{{AgentName: "main", AIC: 17.018}},
	}
	entries := []TokenUsageEntry{
		{Purpose: "routing_classification", Path: "/responses", AICreditsThisResponse: json.RawMessage(`0.04`)},
		{Purpose: "agent", Path: "/v1/messages", AICreditsThisResponse: json.RawMessage(`17.078`)},
	}

	reconcileAgentUsageCredits(summary, entries)

	require.Len(t, summary.Warnings, 1)
	require.Contains(t, summary.Warnings[0], "/v1/messages")
	require.NotContains(t, summary.Warnings[0], "/responses")
}

func TestLegacyPiAttributionFixture(t *testing.T) {
	fixture := filepath.Join("testdata", "subagent_attribution", "37733652548")
	session, err := os.ReadFile(filepath.Join(fixture, "agent-session.jsonl"))
	require.NoError(t, err)
	requests, _, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(string(session)), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 2)
	require.Equal(t, 1, requests[0].CompletedCount)
	require.Equal(t, 1, requests[1].CompletedCount)

	usage, err := os.ReadFile(filepath.Join(fixture, "token-usage.jsonl"))
	require.NoError(t, err)
	entries := make([]TokenUsageEntry, 0)
	for line := range strings.SplitSeq(strings.TrimSpace(string(usage)), "\n") {
		var entry TokenUsageEntry
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}

	resolver := newModelIdentityResolver(fixture)
	agents = resolveAgentUsageModels(agents, resolver, tokenUsageEntryModels(entries)...)
	actuals := subagentActualsFromAgentUsage(agents)
	requests = resolveSubagentRequestModels(requests, actuals, resolver, tokenUsageEntryModels(entries)...)
	actuals = resolveSubagentActualModels(actuals, resolver, tokenUsageEntryModels(entries)...)
	summary := &TokenUsageSummary{AgentUsage: agents, AICFound: true}
	for _, entry := range entries {
		summary.TotalAIC += tokenUsageEntryCredits(entry)
	}
	matchPiAgentUsageCredits(summary, entries, resolver)
	reconcileAgentUsageCredits(summary, entries)

	require.Len(t, actuals, 1)
	require.Equal(t, 4, actuals[0].Requests)
	require.Equal(t, "claude-haiku-4.5", actuals[0].Model)
	require.NotContains(t, actuals[0].ServedModels, "small")
	require.Len(t, summary.AgentUsage, 3)
	creditsByAgent := make(map[string]float64)
	var quickCheckerRequest SubagentModelRequest
	for _, request := range requests {
		if request.AgentName == "quick-checker" {
			quickCheckerRequest = request
			break
		}
	}
	require.Equal(t, "quick-checker", quickCheckerRequest.AgentName)
	require.Equal(t, "claude-haiku-4.5", quickCheckerRequest.EffectiveModel)
	require.NotContains(t, quickCheckerRequest.ServedModels, "small")
	require.Contains(t, quickCheckerRequest.ServedModels, "claude-haiku-4-5-20251001")
	for _, agent := range summary.AgentUsage {
		creditsByAgent[agent.AgentName] = agent.AIC
		if agent.AgentName == "quick-checker" {
			require.NotContains(t, agent.ServedModels, "small")
			require.Contains(t, agent.ServedModels, "claude-haiku-4-5-20251001")
		}
	}
	require.InDelta(t, 1.393, creditsByAgent["file-summarizer"], 0.000001)
	require.InDelta(t, 1.001, creditsByAgent["quick-checker"], 0.000001)
	require.Empty(t, summary.Warnings)
}

func TestPiInvocationIDAttributionFixture(t *testing.T) {
	path := filepath.Join("testdata", "subagent_attribution", "pi-invocation-ids", "agent-session.jsonl")
	session, err := os.ReadFile(path)
	require.NoError(t, err)
	requests, _, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(string(session)), false)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 2)
	require.Equal(t, "file-summarizer", requests[0].AgentName)
	require.Equal(t, 1, requests[0].FailedCount)
	require.Equal(t, "quick-checker", requests[1].AgentName)
	require.Equal(t, 1, requests[1].CompletedCount)
	require.Equal(t, "file-summarizer", agents[0].AgentName)
	require.Equal(t, 1, agents[0].FailedCount)
	require.Equal(t, "quick-checker", agents[1].AgentName)
	require.Equal(t, 1, agents[1].CompletedCount)
}

func TestPiFailedSubagentsStillAttributeMainAgentCredits(t *testing.T) {
	entries := []TokenUsageEntry{
		{Model: "gpt-5.4-mini", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10}, AICreditsThisResponse: json.RawMessage(`1.2`)},
		{Purpose: "routing_classification", Model: "gpt-5.4-mini", AICreditsThisResponse: json.RawMessage(`0.5`)},
	}
	summary := &TokenUsageSummary{
		AICFound: true, TotalAIC: 1.7,
		AgentUsage: []AgentUsageBreakdown{{
			AgentName: "reader", AgentType: "subagent", SourceEngine: "pi", FailedCount: 1,
		}},
	}

	matchPiAgentUsageCredits(summary, entries, newModelIdentityResolver(""))
	require.Len(t, summary.AgentUsage, 2)
	require.Equal(t, "main", summary.AgentUsage[1].AgentType)
	require.Equal(t, 1, summary.AgentUsage[1].Requests)
	require.InDelta(t, 1.2, summary.AgentUsage[1].AIC, 0.000001)
	reconcileAgentUsageCredits(summary, entries)
	require.Empty(t, summary.Warnings)
	require.InDelta(t, 1.7, summary.TotalAIC, 0.000001)
}

func TestPiConcurrentSubagentRequestsMatchProxyEntriesOutOfOrder(t *testing.T) {
	usageEvent := func(agent, timestamp string, input, output int) string {
		record := subagentSessionRecord(fmt.Sprintf(`{"type":"pi.subagent_event","data":{"agent":%q,"event":{"message":{"model":"claude-haiku-4-5-20251001","usage":{"input":%d,"output":%d}}}}}`, agent, input, output))
		return strings.Replace(record, `"data":`, `"timestamp":`+fmt.Sprintf("%q", timestamp)+`,"data":`, 1)
	}
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"invocationId":"summary","agent":"file-summarizer","requestedModel":"claude-haiku-4.5","resolvedModel":"claude-haiku-4.5"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"invocationId":"quick","agent":"quick-checker","requestedModel":"small","resolvedModel":"claude-haiku-4.5"}}`) +
		usageEvent("file-summarizer", "2026-10-08T10:02:00Z", 20, 3) +
		usageEvent("quick-checker", "2026-10-08T10:03:00Z", 10, 2) +
		usageEvent("file-summarizer", "2026-10-08T10:04:00Z", 40, 5) +
		usageEvent("quick-checker", "2026-10-08T10:05:00Z", 30, 4)

	_, _, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	entries := []TokenUsageEntry{
		{Timestamp: "2026-10-08T10:01:00Z", Provider: "anthropic", Model: "claude-haiku-4-5-20251001", Path: "/v1/messages", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 30, OutputTokens: 4}, AICreditsThisResponse: json.RawMessage(`0.558`)},
		{Timestamp: "2026-10-08T10:02:00Z", Provider: "anthropic", Model: "claude-haiku-4-5-20251001", Path: "/v1/messages", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 40, OutputTokens: 5}, AICreditsThisResponse: json.RawMessage(`0.45`)},
		{Timestamp: "2026-10-08T10:03:00Z", Provider: "anthropic", Model: "claude-haiku-4-5-20251001", Path: "/v1/messages", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10, OutputTokens: 2}, AICreditsThisResponse: json.RawMessage(`0.799`)},
		{Timestamp: "2026-10-08T10:06:00Z", Provider: "anthropic", Model: "claude-haiku-4-5-20251001", Path: "/v1/messages", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 20, OutputTokens: 3}, AICreditsThisResponse: json.RawMessage(`0.619`)},
		{Timestamp: "2026-10-08T10:00:00Z", Provider: "github-copilot", Model: "gpt-5.6-luna", Path: "/responses", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 100, OutputTokens: 10}, AICreditsThisResponse: json.RawMessage(`0.1063`)},
		{Timestamp: "2026-10-08T10:00:01Z", Provider: "github-copilot", Model: "gpt-5.6-luna", Path: "/responses", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 101, OutputTokens: 10}, AICreditsThisResponse: json.RawMessage(`0.1063`)},
		{Timestamp: "2026-10-08T10:00:02Z", Provider: "github-copilot", Model: "gpt-5.6-luna", Path: "/responses", TokenCoreMetrics: TokenCoreMetrics{InputTokens: 102, OutputTokens: 10}, AICreditsThisResponse: json.RawMessage(`0.1064`)},
	}
	summary := &TokenUsageSummary{AICFound: true, TotalAIC: 2.745, AgentUsage: agents}
	resolver := newModelIdentityResolver("")
	actuals := resolveSubagentActualModels(subagentActualsFromAgentUsage(agents), resolver)
	require.Len(t, actuals, 1)
	require.Equal(t, 4, actuals[0].Requests)

	matchPiAgentUsageCredits(summary, entries, resolver)
	reconcileAgentUsageCredits(summary, entries)

	creditsByAgent := make(map[string]float64)
	for _, agent := range summary.AgentUsage {
		creditsByAgent[agent.AgentName] = agent.AIC
	}
	require.InDelta(t, 1.069, creditsByAgent["file-summarizer"], 0.000001)
	require.InDelta(t, 1.357, creditsByAgent["quick-checker"], 0.000001)
	require.InDelta(t, 0.319, creditsByAgent["main"], 0.000001)
	for _, agent := range summary.AgentUsage {
		if agent.AgentName == "main" {
			require.Equal(t, 3, agent.Requests)
		}
	}
	require.Empty(t, summary.Warnings)
}

func TestCombineSubagentEffortKeepsMixedState(t *testing.T) {
	require.Equal(t, "mixed", combineSubagentEffort(combineSubagentEffort("low", "high"), "high"))
	require.Equal(t, "mixed", combineSubagentEffort(combineSubagentEffort("high", "low"), "low"))
	require.Equal(t, "mixed", combineSubagentEffort("mixed", "low"))
}

func TestDeclaredSubagentModelsExcludeDetectionUsage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"reader","model":"claude-haiku-4.5"}]}`), 0600))
	summary := buildTokenUsageSummary([]TokenUsageEntry{
		{Model: "gpt-5.6-luna", Provider: "github-copilot", Purpose: "agent"},
		{Model: "claude-haiku-4.5", Provider: "github-copilot", Purpose: "detection"},
	}, 0)
	augmentDeclaredSubagentModels(dir, summary)
	require.Len(t, summary.DeclaredSubagentModels, 1)
	require.Equal(t, modelMismatchReasonModelNotObserved, summary.DeclaredSubagentModels[0].ReasonCode)
	require.True(t, matchesDeclaredModel("openrouter/anthropic/claude-sonnet-4", "anthropic/claude-sonnet-4", "openrouter"))
}
