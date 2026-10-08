package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const subagentSessionHeader = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector"}}` + "\n"

func subagentSessionRecord(record string) string {
	return strings.TrimSuffix(record, "}") + `,"provenance":{"component":"agent","phase":"agent"}}` + "\n"
}

func TestSessionSubagentModels(t *testing.T) {
	start := `{"type":"subagent.started","agentId":"research","data":{"agentName":"research","agentDisplayName":"routing-research","model":"opus","executionMode":"background"}}`
	config := `{"type":"subagent.configured","agentId":"research","data":{"model":"sonnet","reasoningEffort":"low"}}`
	child := `{"type":"subagent.started","agentId":"explore","data":{"agentName":"explore","agentDisplayName":"routing-explore","parentId":"research","model":"haiku"}}`
	metrics := `{"type":"session.result","data":{"agentMetrics":{"main":{"modelMetrics":{"opus":{"requests":{"count":41}}}},"research":{"modelMetrics":{"sonnet":{"requests":{"count":45}}}},"explore":{"modelMetrics":{"haiku":{"requests":{"count":3}}}}}}}`
	session := subagentSessionHeader + subagentSessionRecord(start) + subagentSessionRecord(config) + subagentSessionRecord(child) + subagentSessionRecord(metrics)

	t.Run("uses structured models and only subagent actuals", func(t *testing.T) {
		requests, actuals, found, err := parseSessionSubagentModels(strings.NewReader(session), true)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, requests, 2)
		assert.Equal(t, SubagentModelRequest{AgentName: "routing-research", RequestedModel: "opus", EffectiveModel: "sonnet", InvocationCount: 1}, requests[1])
		assert.Equal(t, []SubagentModelActual{{Model: "haiku", Requests: 3}, {Model: "sonnet", Requests: 45}}, actuals)
	})

	t.Run("uses only the final Copilot attempt", func(t *testing.T) {
		for _, finalAttemptHasSubagent := range []bool{false, true} {
			name := "without subagents"
			if finalAttemptHasSubagent {
				name = "with different subagent"
			}
			t.Run(name, func(t *testing.T) {
				firstInit := subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"first"}}`)
				finalInit := subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"final"}}`)
				firstMetrics := subagentSessionRecord(`{"type":"session.result","data":{"agentMetrics":{"main":{"modelMetrics":{"opus":{"requests":{"count":41}}}},"research":{"agentName":"research","modelMetrics":{"sonnet":{"requests":{"count":45}}}}}}}`)
				finalMetrics := `{"type":"session.result","data":{"agentMetrics":{"main":{"modelMetrics":{"opus":{"requests":{"count":3}}}}`
				finalStart := ""
				if finalAttemptHasSubagent {
					finalStart = subagentSessionRecord(`{"type":"subagent.started","agentId":"final","data":{"agentName":"final","agentDisplayName":"final-worker","model":"haiku"}}`)
					finalMetrics += `,"final":{"agentName":"final","modelMetrics":{"haiku":{"requests":{"count":7}}}}`
				}
				finalMetrics = subagentSessionRecord(finalMetrics + `}}}`)
				content := subagentSessionHeader + firstInit + subagentSessionRecord(start) + firstMetrics + finalInit + finalStart + finalMetrics

				requests, actuals, found, err := parseSessionSubagentModels(strings.NewReader(content), true)
				require.NoError(t, err)
				require.True(t, found)
				if finalAttemptHasSubagent {
					assert.Equal(t, []SubagentModelRequest{{AgentName: "final-worker", RequestedModel: "haiku", EffectiveModel: "haiku", InvocationCount: 1}}, requests)
					assert.Equal(t, []SubagentModelActual{{Model: "haiku", Requests: 7}}, actuals)
				} else {
					assert.Empty(t, requests)
					assert.Empty(t, actuals)
				}
			})
		}
	})

	t.Run("aggregates repeated logical subagent requests", func(t *testing.T) {
		first := subagentSessionRecord(`{"type":"subagent.started","agentId":"research-1","data":{"agentName":"research","agentDisplayName":"routing-research","model":"opus"}}`)
		second := subagentSessionRecord(`{"type":"subagent.started","agentId":"research-2","data":{"agentName":"research","agentDisplayName":"routing-research","model":"opus"}}`)
		requests, _, found, err := parseSessionSubagentModels(strings.NewReader(subagentSessionHeader+first+second), true)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, []SubagentModelRequest{{AgentName: "routing-research", RequestedModel: "opus", EffectiveModel: "opus", InvocationCount: 2}}, requests)
	})

	t.Run("prefers usage artifact over canonical trace and log", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte(session), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-session.jsonl"), []byte(child+"\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-stdio.log"), []byte("● Fake(wrong) prose"), 0o644))
		summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{"other": {Requests: 100}}}
		augmentSubagentModelAttribution(root, summary)
		require.Len(t, summary.SubagentModelRequests, 2)
		assert.Empty(t, summary.Warnings)
		assert.Zero(t, summary.MismatchCount)
		assert.Equal(t, []SubagentModelActual{{Model: "haiku", Requests: 3}, {Model: "sonnet", Requests: 45}}, summary.SubagentModelActuals)
	})

	t.Run("uses canonical bootstrap trace without conclusion artifact", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-session.jsonl"), []byte(start+"\n"+config+"\n"), 0o644))
		requests, _, found, err := readSessionSubagentModels(root)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, requests, 1)
		assert.Equal(t, "sonnet", requests[0].EffectiveModel)
	})

	t.Run("continues to canonical trace after malformed usage artifact", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte("invalid\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-session.jsonl"), []byte(subagentSessionRecord(start)), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-stdio.log"), []byte("● Fake (model: wrong)"), 0o644))
		summary := &TokenUsageSummary{}
		augmentSubagentModelAttribution(root, summary)
		require.Equal(t, []SubagentModelRequest{{AgentName: "routing-research", RequestedModel: "opus", EffectiveModel: "opus", InvocationCount: 1}}, summary.SubagentModelRequests)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "failed to parse unified subagent information")
	})

	t.Run("does not add native and projected snapshots", func(t *testing.T) {
		requests, actuals, found, err := parseSessionSubagentModels(strings.NewReader(session+subagentSessionRecord(strings.Replace(metrics, "session.result", "session.shutdown", 1))), true)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Len(t, requests, 2)
		assert.Equal(t, 45, actuals[1].Requests)
	})

	t.Run("structured empty Copilot session suppresses heuristic rows", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte(subagentSessionHeader+subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot"}}`)), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-stdio.log"), []byte("● Fake(wrong) quoted text"), 0o644))
		summary := &TokenUsageSummary{}
		augmentSubagentModelAttribution(root, summary)
		assert.Empty(t, summary.SubagentModelRequests)
		assert.Empty(t, summary.Warnings)
	})

	t.Run("ignores detection and eval subagents", func(t *testing.T) {
		content := subagentSessionHeader + strings.ReplaceAll(subagentSessionRecord(start), `"phase":"agent"`, `"phase":"detection"`)
		requests, _, found, err := parseSessionSubagentModels(strings.NewReader(content), true)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, requests)
	})

	t.Run("malformed unified session warns and falls back", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte(subagentSessionHeader+"invalid\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-stdio.log"), []byte("● Research (model: opus) Research routing\n[INFO] container(s)\nclass RoutingProfile(StrictModel)\nlen(d)\n"), 0o644))
		summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{"opus": {Requests: 1}}}
		augmentSubagentModelAttribution(root, summary)
		require.Len(t, summary.SubagentModelRequests, 1)
		assert.Equal(t, "Research", summary.SubagentModelRequests[0].AgentName)
		assert.Equal(t, "opus", summary.SubagentModelRequests[0].RequestedModel)
		require.Len(t, summary.Warnings, 2)
		assert.Contains(t, summary.Warnings[0], "failed to parse unified subagent information")
	})

	t.Run("rejects symlink sources", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "trace.jsonl")
		require.NoError(t, os.WriteFile(file, []byte(start+"\n"), 0o644))
		require.NoError(t, os.Symlink(file, filepath.Join(root, "agent-session.jsonl")))
		_, _, _, err := readSessionSubagentModels(root)
		require.ErrorContains(t, err, "symbolic link")
	})
}
