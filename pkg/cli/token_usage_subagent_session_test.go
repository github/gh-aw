package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const subagentSessionHeader = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector"}}` + "\n"

const copilotSubagentIntegrationMetrics = `{
	"main":{"totalNanoAiu":389122000000,"modelMetrics":{"opus":{"requests":{"count":41},"usage":{"inputTokens":4100,"outputTokens":410,"cacheReadTokens":41,"cacheWriteTokens":82}}}},
	"research":{"agentName":"research","agentDisplayName":"subagent-research","totalNanoAiu":494207000000,"modelMetrics":{"opus":{"requests":{"count":45},"usage":{"inputTokens":4500,"outputTokens":450,"cacheReadTokens":45,"cacheWriteTokens":90}}}},
	"ghaw":{"agentName":"explore","agentDisplayName":"ghaw-issues","totalNanoAiu":9141000000,"modelMetrics":{"opus":{"requests":{"count":3},"usage":{"inputTokens":300,"outputTokens":30,"cacheReadTokens":3,"cacheWriteTokens":6}}}},
	"awf":{"agentName":"explore","agentDisplayName":"awf-routing","totalNanoAiu":40977000000,"modelMetrics":{"opus":{"requests":{"count":12},"usage":{"inputTokens":1200,"outputTokens":120,"cacheReadTokens":12,"cacheWriteTokens":24}}}}
}`

func subagentSessionRecord(record string) string {
	return strings.TrimSuffix(record, "}") + `,"provenance":{"component":"agent","phase":"agent"}}` + "\n"
}

func tokenUsageEntryModels(entries []TokenUsageEntry) []string {
	models := make([]string, 0, len(entries))
	for _, entry := range entries {
		models = appendUnique(models, entry.Model)
	}
	return models
}

func TestSessionSubagentModelsInterleavedRetries(t *testing.T) {
	t.Parallel()
	for _, canonical := range []bool{false, true} {
		t.Run(fmt.Sprintf("canonical=%t", canonical), func(t *testing.T) {
			t.Parallel()
			record := func(kind, path, id, timestamp, data string) string {
				provenance := `"path":"` + path + `"`
				if canonical {
					provenance = `"path":"agent-session.jsonl","native":{"path":"` + path + `"}`
				}
				return `{"type":"` + kind + `","agentId":"` + id + `","timestamp":"` + timestamp + `","data":` + data +
					`,"provenance":{"component":"agent","phase":"agent",` + provenance + "}}\n"
			}
			early := "2026-10-08T01:00:00Z"
			late := "2026-10-08T01:01:00Z"
			content := subagentSessionHeader +
				record("session.start", "first.jsonl", "", early, `{"sessionId":"first"}`) +
				record("session.init", "first.jsonl", "", early, `{"sourceEngine":"copilot","sessionId":"first"}`) +
				record("session.start", "last.jsonl", "", late, `{"sessionId":"last"}`) +
				record("session.init", "last.jsonl", "", late, `{"sourceEngine":"copilot","sessionId":"last"}`) +
				record("subagent.started", "last.jsonl", "child", late, `{"agentName":"final-worker","model":"haiku"}`) +
				record("session.result", "last.jsonl", "", late, `{"agentMetrics":{"child":{"modelMetrics":{"haiku":{"requests":{"count":7}}}}}}`) +
				record("subagent.started", "first.jsonl", "child", late, `{"agentName":"stale-worker","model":"opus"}`) +
				record("session.result", "first.jsonl", "", late, `{"agentMetrics":{"child":{"modelMetrics":{"opus":{"requests":{"count":99}}}}}}`)
			requests, actuals, found, err := parseSessionSubagentModels(strings.NewReader(content), true)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, []SubagentModelRequest{{AgentName: "final-worker", RequestedModel: "haiku", EffectiveModel: "haiku", InvocationCount: 1, IncompleteCount: 1}}, requests)
			require.Equal(t, []SubagentModelActual{{Model: "haiku", Requests: 7}}, actuals)
		})
	}
}

func TestSubagentSessionStart(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, record string
		milliseconds int64
		wantError    bool
	}{
		{name: "RFC3339", record: `{"timestamp":"1970-01-01T00:00:02Z"}`, milliseconds: 2000},
		{name: "milliseconds", record: `{"timestamp":2000}`, milliseconds: 2000},
		{name: "seconds", record: `{"timestamp":2,"provenance":{"timestampUnit":"seconds"}}`, milliseconds: 2000},
		{name: "missing", record: `{}`},
		{name: "null", record: `{"timestamp":null}`},
		{name: "invalid", record: `{"timestamp":"not-a-date"}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var event sessionSubagentEvent
			require.NoError(t, json.Unmarshal([]byte(test.record), &event))
			start, err := subagentSessionStart(event)
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if test.milliseconds == 0 {
				require.True(t, start.IsZero())
			} else {
				require.Equal(t, test.milliseconds, start.UnixMilli())
			}
		})
	}
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
		assert.Equal(t, SubagentModelRequest{AgentName: "routing-research", RequestedModel: "opus", ResolvedModel: "sonnet", EffectiveModel: "sonnet", InvocationCount: 1, IncompleteCount: 1, Effort: "low"}, requests[1])
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
					assert.Equal(t, []SubagentModelRequest{{AgentName: "final-worker", RequestedModel: "haiku", EffectiveModel: "haiku", InvocationCount: 1, IncompleteCount: 1}}, requests)
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
		assert.Equal(t, []SubagentModelRequest{{AgentName: "routing-research", RequestedModel: "opus", InvocationCount: 2, IncompleteCount: 2}}, requests)
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
		require.Len(t, summary.SubagentModelActuals, 2)
		assert.Equal(t, []string{"sonnet", "haiku"}, []string{summary.SubagentModelActuals[0].Model, summary.SubagentModelActuals[1].Model})
		assert.Equal(t, []int{45, 3}, []int{summary.SubagentModelActuals[0].Requests, summary.SubagentModelActuals[1].Requests})
	})

	t.Run("uses canonical bootstrap trace without conclusion artifact", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-session.jsonl"), []byte(start+"\n"+config+"\n"), 0o644))
		requests, _, found, err := readSessionSubagentModels(root)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, requests, 1)
		assert.Empty(t, requests[0].EffectiveModel)
	})

	t.Run("does not use legacy trace when usage artifact is present but malformed", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte("invalid\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-session.jsonl"), []byte(subagentSessionRecord(start)), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-stdio.log"), []byte("● Fake (model: wrong)"), 0o644))
		summary := &TokenUsageSummary{}
		augmentSubagentModelAttribution(root, summary)
		assert.Empty(t, summary.SubagentModelRequests)
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

	t.Run("malformed unified session warns without inferring rows from stdio", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte(subagentSessionHeader+"invalid\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "agent-stdio.log"), []byte("● Check repository status (shell)\n● Read OpenAPI spec (head)\n● Research (model: opus) Research routing\n[INFO] container(s)\nclass RoutingProfile(StrictModel)\nlen(d)\n"), 0o644))
		summary := &TokenUsageSummary{ByModel: map[string]*ModelTokenUsage{"opus": {Requests: 1}}}
		augmentSubagentModelAttribution(root, summary)
		assert.Empty(t, summary.SubagentModelRequests)
		require.Len(t, summary.Warnings, 1)
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

func TestSessionSubagentFailuresAreNotReportedAsServed(t *testing.T) {
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"failed-run"}}`) +
		subagentSessionRecord(`{"type":"subagent.started","agentId":"failed-1","data":{"agentDisplayName":"file-summarizer","model":"claude-haiku-4.5"}}`) +
		subagentSessionRecord(`{"type":"subagent.failed","agentId":"failed-1","data":{"error":"HTTP 400\nCannot translate request"}}`) +
		subagentSessionRecord(`{"type":"subagent.started","agentId":"quick-1","data":{"agentDisplayName":"quick-checker","model":"small"}}`) +
		subagentSessionRecord(`{"type":"subagent.completed","agentId":"quick-1","data":{}}`) +
		subagentSessionRecord(`{"type":"session.shutdown","data":{"agentMetrics":{"failed-1":{"agentDisplayName":"file-summarizer","modelMetrics":{"claude-haiku-4.5":{"requests":{"count":0}}}},"quick-1":{"agentDisplayName":"quick-checker","modelMetrics":{"small":{"requests":{"count":2},"usage":{"inputTokens":10,"outputTokens":3}},"gpt-5.4-mini":{"requests":{"count":0},"totalNanoAiu":1144000000}}}}}}`)

	requests, actuals, found, err := parseSessionSubagentModels(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 2)
	failed := requests[0]
	require.Equal(t, "file-summarizer", failed.AgentName)
	require.Equal(t, "SUBAGENT_FAILED", failed.ReasonCode)
	require.Equal(t, 1, failed.FailedCount)
	require.Empty(t, failed.ErrorCode)
	require.Empty(t, failed.EffectiveModel)
	require.Equal(t, "HTTP 400 Cannot translate request", failed.Error)
	completed := requests[1]
	require.Equal(t, "quick-checker", completed.AgentName)
	require.Equal(t, 1, completed.CompletedCount)
	require.Equal(t, "small", completed.EffectiveModel)
	require.Len(t, actuals, 3)
	findings := generateSubagentModelFindings(&TokenUsageSummary{
		SubagentModelRequests: requests,
		DeclaredSubagentModels: []SubagentModelRequest{{
			AgentName: "file-summarizer", RequestedModel: "claude-haiku-4.5",
			ReasonCode: modelMismatchReasonModelNotObserved,
		}},
	})
	require.Len(t, findings, 2)
	require.Equal(t, AuditFindingSubagentFailed, findings[0].Code)
	require.Contains(t, findings[0].Description, "file-summarizer failed 1 invocation")
	require.Contains(t, findings[0].Description, "HTTP 400 Cannot translate request")
}

func TestCopilotToolExecutionObjectErrorDoesNotRejectSubagentEvents(t *testing.T) {
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"subagent.started","agentId":"worker","data":{"agentDisplayName":"Research","model":"opus"}}`) +
		subagentSessionRecord(`{"type":"tool.execution_complete","data":{"toolCallId":"tool-1","success":false,"error":{"message":"tool execution failed","code":"tool_error"}}}`) +
		subagentSessionRecord(`{"type":"subagent.failed","agentId":"worker","data":{"error":{"message":"HTTP 400 Cannot translate request","code":"bad_request"}}}`)

	requests, _, _, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 1)
	require.Equal(t, "Research", requests[0].AgentName)
	require.Equal(t, 1, requests[0].FailedCount)
	require.Equal(t, "HTTP 400 Cannot translate request", requests[0].Error)
}

func TestSessionAgentUsageIncludesMainAndSubagents(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(`{"sub_agent_models":[{"name":"quick-checker","model":"small","patterns":["copilot/gpt-5.4-mini"]}]}`), 0600))
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"usage"}}`) +
		subagentSessionRecord(`{"type":"subagent.started","agentId":"quick-1","data":{"agentDisplayName":"quick-checker","model":"small"}}`) +
		subagentSessionRecord(`{"type":"subagent.configured","agentId":"quick-1","data":{"reasoningEffort":"low"}}`) +
		subagentSessionRecord(`{"type":"subagent.completed","agentId":"quick-1","data":{}}`) +
		subagentSessionRecord(`{"type":"session.shutdown","data":{"agentMetrics":{"main":{"totalNanoAiu":1599000000,"totalApiDurationMs":1200,"modelMetrics":{"gpt-5.6-luna":{"requests":{"count":10},"usage":{"inputTokens":100,"outputTokens":20}}}},"quick-1":{"agentDisplayName":"quick-checker","totalNanoAiu":1144000000,"totalApiDurationMs":400,"modelMetrics":{"small":{"requests":{"count":4},"usage":{"inputTokens":40,"outputTokens":8}},"gpt-5.4-mini":{"requests":{"count":0},"totalNanoAiu":1144000000}}}}}}`)
	requests, actuals, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 1)
	require.Len(t, agents, 2)
	require.Equal(t, "main", agents[0].AgentType)
	require.InDelta(t, 1.599, agents[0].AIC, 0.000001)
	require.Equal(t, "quick-checker", agents[1].AgentName)
	require.Equal(t, 1, agents[1].CompletedCount)
	require.Equal(t, "low", agents[1].Effort)
	require.Equal(t, 4, agents[1].Requests)
	require.InDelta(t, 1.144, agents[1].AIC, 0.000001)
	resolver := newModelIdentityResolver(dir)
	modelID := "gpt-5.4-mini-2026-03-17"
	requests = resolveSubagentRequestModels(requests, actuals, resolver, modelID)
	actuals = resolveSubagentActualModels(actuals, resolver, modelID)
	agents = resolveAgentUsageModels(agents, resolver, modelID)
	require.Equal(t, "gpt-5.4-mini", requests[0].EffectiveModel)
	require.Contains(t, requests[0].ServedModels, modelID)
	require.NotContains(t, requests[0].ServedModels, "small")
	require.Len(t, actuals, 1)
	require.Equal(t, 4, actuals[0].Requests)
	require.InDelta(t, 1.144, actuals[0].AIC, 0.000001)
	require.Contains(t, actuals[0].ServedModels, modelID)
	require.Len(t, agents[1].Models, 1)
	require.Equal(t, "gpt-5.4-mini", agents[1].Models[0].Model)
	require.Contains(t, agents[1].ServedModels, modelID)
	require.NotContains(t, agents[1].ServedModels, "small")
}

func TestPiLegacySubagentEventsCorrelateByAgent(t *testing.T) {
	usageEvent := subagentSessionRecord(`{"type":"pi.subagent_event","data":{"agent":"reader","event":{"message":{"model":"gpt-5.4-mini","usage":{"input":10,"output":2}}}}}`)
	usageEvent = strings.Replace(usageEvent, `"data":`, `"timestamp":"2026-10-08T10:00:00Z","data":`, 1)
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"agent":"reader","requestedModel":"small","resolvedModel":"gpt-5.4-mini"}}`) +
		usageEvent

	requests, actuals, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []SubagentModelRequest{{
		AgentName: "reader", RequestedModel: "small", ResolvedModel: "gpt-5.4-mini",
		InvocationCount: 1, IncompleteCount: 1, EffectiveModel: "gpt-5.4-mini",
	}}, requests)
	require.Equal(t, []SubagentModelActual{{
		Model: "gpt-5.4-mini", Requests: 1, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10, OutputTokens: 2},
	}}, actuals)
	require.Len(t, agents, 1)
	require.Equal(t, 1, agents[0].IncompleteCount)
	require.Equal(t, 1, agents[0].Requests)
	require.Equal(t, "2026-10-08T10:00:00Z", agents[0].requestUsages[0].Timestamp.Format(time.RFC3339))

	completedContent := content +
		subagentSessionRecord(`{"type":"pi.subagent_result","data":{"agent":"reader","outcome":"completed"}}`)
	requests, _, agents, found, err = parseSessionSubagentModelsDetailed(strings.NewReader(completedContent), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 1, requests[0].CompletedCount)
	require.Zero(t, requests[0].IncompleteCount)
	require.Equal(t, 1, agents[0].CompletedCount)
	require.Zero(t, agents[0].IncompleteCount)
}

func TestCopilotSubagentUsageRetainsIncompleteOutcome(t *testing.T) {
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"subagent.started","agentId":"child","data":{"agentDisplayName":"reader","model":"small"}}`)

	requests, _, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 1, requests[0].IncompleteCount)
	require.Equal(t, 1, agents[0].IncompleteCount)
}

func TestPiFailedZeroTokenMessageDoesNotCountAsServed(t *testing.T) {
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"invocationId":"call-1","agent":"reader","requestedModel":"small","resolvedModel":"gpt-5.4-mini"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_event","data":{"invocationId":"call-1","agent":"reader","event":{"message":{"model":"gpt-5.4-mini","stopReason":"error","errorMessage":"connection failed","usage":{"input":0,"output":0}}}}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_result","data":{"invocationId":"call-1","agent":"reader","outcome":"failed","error":"connection failed"}}`)

	requests, actuals, agents, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, actuals)
	require.Equal(t, 1, requests[0].FailedCount)
	require.Zero(t, requests[0].CompletedCount)
	require.Equal(t, modelMismatchReasonSubagentFailed, requests[0].ReasonCode)
	require.Empty(t, requests[0].EffectiveModel)
	require.Equal(t, 0, requests[0].IncompleteCount)
	require.Equal(t, "connection failed", requests[0].Error)
	require.Equal(t, 1, agents[0].FailedCount)
	require.Zero(t, agents[0].IncompleteCount)
	require.Zero(t, agents[0].Requests)
}

func TestPiResponseModelAccumulatesUsageAcrossMessages(t *testing.T) {
	message := `{"model":"gpt-5.4-mini","responseModel":"gpt-5.4-mini-2026-03-17","usage":{"input":10,"output":2}}`
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"invocationId":"call-1","agent":"reader","requestedModel":"small","resolvedModel":"gpt-5.4-mini"}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_event","data":{"invocationId":"call-1","agent":"reader","event":{"message":`+message+`}}}`) +
		subagentSessionRecord(`{"type":"pi.subagent_event","data":{"invocationId":"call-1","agent":"reader","event":{"message":`+message+`}}}`)

	requests, actuals, _, found, err := parseSessionSubagentModelsDetailed(strings.NewReader(content), true)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, requests, 1)
	require.Equal(t, "gpt-5.4-mini-2026-03-17", requests[0].EffectiveModel)
	require.Len(t, actuals, 1)
	require.Equal(t, "gpt-5.4-mini-2026-03-17", actuals[0].Model)
	require.Equal(t, 2, actuals[0].Requests)
	require.Equal(t, 20, actuals[0].InputTokens)
	require.Equal(t, 4, actuals[0].OutputTokens)
}

func TestSubagentGroupEffectiveModelIsIndependentOfIterationOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		metrics := subagentSessionRecord(`{"type":"session.shutdown","data":{"agentMetrics":{"completed":{"modelMetrics":{"gpt-5.4-mini":{"requests":{"count":1}}}}}}}`)
		startFailed := subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"invocationId":"failed","agent":"worker","requestedModel":"small"}}`)
		startCompleted := subagentSessionRecord(`{"type":"pi.subagent_dispatch","data":{"invocationId":"completed","agent":"worker","requestedModel":"small"}}`)
		fail := subagentSessionRecord(`{"type":"pi.subagent_result","data":{"invocationId":"failed","agent":"worker","outcome":"failed","error":"unavailable"}}`)
		complete := subagentSessionRecord(`{"type":"pi.subagent_result","data":{"invocationId":"completed","agent":"worker","outcome":"completed"}}`)
		if reverse {
			startFailed, startCompleted = startCompleted, startFailed
			fail, complete = complete, fail
		}
		content := subagentSessionHeader +
			subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"pi","sessionId":"parent"}}`) +
			startFailed + startCompleted + fail + complete + metrics
		requests, _, found, err := parseSessionSubagentModels(strings.NewReader(content), true)
		require.NoError(t, err)
		require.True(t, found)
		require.Len(t, requests, 1)
		require.Equal(t, 1, requests[0].FailedCount)
		require.Equal(t, 1, requests[0].CompletedCount)
		require.Equal(t, "gpt-5.4-mini", requests[0].EffectiveModel)
		require.Empty(t, requests[0].ReasonCode)
	}
}

func TestCopilotUnifiedFixtureRetainsFailedCrossFamilySubagent(t *testing.T) {
	fixture := filepath.Join("testdata", "subagent_attribution", "copilot-cross-family")
	summary := &TokenUsageSummary{}
	augmentSubagentModelAttribution(fixture, summary)

	require.Len(t, summary.SubagentModelRequests, 1)
	failed := summary.SubagentModelRequests[0]
	require.Equal(t, "File-summarizer", failed.AgentName)
	require.Equal(t, modelMismatchReasonSubagentFailed, failed.ReasonCode)
	require.Equal(t, "SUBAGENT_MODEL_UNAVAILABLE", failed.ErrorCode)
	require.Equal(t, "The requested model has no available endpoint.", failed.Error)
	require.Equal(t, 1, failed.FailedCount)
	require.Empty(t, failed.EffectiveModel)
	require.Len(t, summary.AgentUsage, 2)
	require.Equal(t, "main", summary.AgentUsage[0].AgentType)
	require.Equal(t, 1, summary.AgentUsage[0].Requests)
	require.InDelta(t, 0.25, summary.AgentUsage[0].AIC, 0.000001)
	require.Equal(t, 1, summary.AgentUsage[1].FailedCount)
	require.Zero(t, summary.AgentUsage[1].Requests)
	require.Zero(t, summary.AgentUsage[1].AIC)
}

func TestCopilotAgentMetricsWithoutLifecycleRetainAttribution(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "usage"), 0o755))
	content := subagentSessionHeader +
		subagentSessionRecord(`{"type":"session.init","data":{"sourceEngine":"copilot","sessionId":"metrics-only"}}`) +
		subagentSessionRecord(`{"type":"subagent.configured","data":{"agentName":"identity-less","model":"opus"}}`) +
		subagentSessionRecord(`{"type":"session.shutdown","data":{"agentMetrics":`+strings.ReplaceAll(copilotSubagentIntegrationMetrics, "\n", "")+`}}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "usage", "aw_session.jsonl"), []byte(content), 0o644))

	_, _, parsedAgents, found, err := readSessionSubagentModelsDetailed(root)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, parsedAgents, 4)
	summary := &TokenUsageSummary{}
	augmentSubagentModelAttribution(root, summary)

	require.Len(t, summary.AgentUsage, 4)
	agents := make(map[string]AgentUsageBreakdown, len(summary.AgentUsage))
	for _, agent := range summary.AgentUsage {
		agents[agent.AgentName] = agent
	}
	require.InDelta(t, 389.122, agents["main"].AIC, 0.000001)
	require.InDelta(t, 494.207, agents["subagent-research"].AIC, 0.000001)
	require.InDelta(t, 40.977, agents["awf-routing"].AIC, 0.000001)
	require.InDelta(t, 9.141, agents["ghaw-issues"].AIC, 0.000001)
	require.Len(t, summary.SubagentModelRequests, 3)
	for _, request := range summary.SubagentModelRequests {
		require.Zero(t, request.InvocationCount)
		require.Zero(t, request.CompletedCount)
		require.Zero(t, request.FailedCount)
		require.Zero(t, request.IncompleteCount)
		require.Equal(t, "opus", request.RequestedModel)
	}
	require.Len(t, summary.SubagentModelActuals, 1)
	require.Equal(t, 60, summary.SubagentModelActuals[0].Requests)
	require.Equal(t, TokenCoreMetrics{InputTokens: 6000, OutputTokens: 600, CacheReadTokens: 60, CacheWriteTokens: 120}, summary.SubagentModelActuals[0].TokenCoreMetrics)
}

func TestPiUnifiedFixtureAttributesInterleavedRequestsFromFirewallEvents(t *testing.T) {
	fixture := filepath.Join("testdata", "subagent_attribution", "pi-interleaved")
	_, actuals, agents, found, err := readSessionSubagentModelsDetailed(fixture)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, actuals, 1)
	require.Equal(t, 4, actuals[0].Requests)

	entries, err := readUnifiedTokenUsageEntries(fixture)
	require.NoError(t, err)
	require.Len(t, entries, 5)
	require.Equal(t, "/v1/messages", entries[0].Path)
	require.Equal(t, "subagent", entries[0].Purpose)
	require.Equal(t, "z-1", entries[0].RequestID)

	resolver := newModelIdentityResolver("")
	agents = resolveAgentUsageModels(agents, resolver, tokenUsageEntryModels(entries)...)
	summary := &TokenUsageSummary{AgentUsage: agents, AICFound: true, TotalAIC: 2.35}
	matchPiAgentUsageCredits(summary, entries, resolver)
	reconcileAgentUsageCredits(summary, entries)

	creditsByAgent := make(map[string]float64)
	requestsByAgent := make(map[string]int)
	var mainAgent *AgentUsageBreakdown
	for _, agent := range summary.AgentUsage {
		creditsByAgent[agent.AgentName] = agent.AIC
		requestsByAgent[agent.AgentName] = agent.Requests
		if agent.AgentType == "main" {
			mainAgent = &agent
		}
	}
	require.InDelta(t, 0.9, creditsByAgent["a-reader"], 0.000001)
	require.InDelta(t, 1.2, creditsByAgent["z-reader"], 0.000001)
	require.InDelta(t, 0.25, creditsByAgent["main"], 0.000001)
	require.Equal(t, 2, requestsByAgent["a-reader"])
	require.Equal(t, 2, requestsByAgent["z-reader"])
	require.Equal(t, 1, requestsByAgent["main"])
	require.NotNil(t, mainAgent)
	require.Equal(t, []string{"gpt-5.6-luna"}, mainAgent.ServedModels)
	require.Empty(t, summary.Warnings)
}
