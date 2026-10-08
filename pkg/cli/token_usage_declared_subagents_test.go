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

func TestPiStructuredSubagentModelAttribution(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-session.jsonl"), []byte("{\"type\":\"session.init\",\"data\":{\"sourceEngine\":\"pi\",\"sessionId\":\"parent\"}}\n{\"type\":\"pi.subagent_dispatch\",\"data\":{\"agent\":\"reader\",\"requestedModel\":\"small\",\"resolvedModel\":\"claude-haiku-4.5\"}}\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("{\"type\":\"gh_aw_subagent_dispatch\",\"agent\":\"reader\",\"requested_model\":\"small\",\"resolved_model\":\"claude-haiku-4.5\"}\n"), 0600))
	summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{"claude-haiku-4.5": {Provider: "github-copilot", Requests: 1}}}
	augmentSubagentModelAttribution(dir, summary)
	require.Len(t, summary.SubagentModelRequests, 1)
	row := summary.SubagentModelRequests[0]
	require.Equal(t, "small", row.RequestedModel)
	require.Equal(t, "claude-haiku-4.5", row.ResolvedModel)
	require.Equal(t, row.ResolvedModel, row.EffectiveModel)
	require.Empty(t, row.ReasonCode)
	require.Zero(t, summary.MismatchCount)
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
