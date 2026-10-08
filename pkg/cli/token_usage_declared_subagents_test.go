//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

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
	require.ElementsMatch(t, []string{"small", "gpt-5.4-mini"}, actuals[0].ServedModels)
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

func TestSubagentDispatchLine(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, line string
		want       subagentDispatchKey
		count      int
	}{
		{"compact dispatch", "● Agent-alpha(claude-haiku-4.5) Get model name", subagentDispatchKey{agent: "Agent-alpha", model: "claude-haiku-4.5"}, 1},
		{"labelled dispatch", "● Research worker (model: opus) Research routing", subagentDispatchKey{agent: "Research worker", model: "opus"}, 1},
		{"ordinary prose", "class RoutingProfile(StrictModel)", subagentDispatchKey{}, 0},
		{"quoted dispatch", "Example: ● Research (model: opus)", subagentDispatchKey{}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			counts := make(map[subagentDispatchKey]int)
			countSubagentDispatchLine(counts, test.line)
			require.Len(t, counts, test.count)
			if test.count != 0 {
				require.Equal(t, 1, counts[test.want])
			}
		})
	}
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
