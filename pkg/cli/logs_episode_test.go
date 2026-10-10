//go:build !integration

package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildEpisodeDataIncludesToolCalls(t *testing.T) {
	t.Parallel()

	runs := []RunData{
		{
			RunID:        101,
			WorkflowName: "my-workflow",
			Status:       "completed",
			Conclusion:   "success",
			TokenUsage:   1000,
			CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   101,
				WorkflowName: "my-workflow",
			},
			MCPToolUsage: &MCPToolUsageData{
				ToolCalls: []MCPToolCall{
					{
						ServerName: "github",
						ToolName:   "get_file_contents",
						InputSize:  400,
						OutputSize: 9200,
						Duration:   "350ms",
						Status:     "success",
					},
					{
						ServerName: "github",
						ToolName:   "create_pull_request",
						InputSize:  200,
						OutputSize: 3000,
						Duration:   "600ms",
						Status:     "error",
						Error:      "403 Resource not accessible by integration",
					},
				},
			},
		},
	}

	episodes, _ := buildEpisodeData(runs, processedRuns)
	require.Len(t, episodes, 1, "expected one episode")

	ep := episodes[0]
	require.Len(t, ep.ToolCalls, 2, "expected two tool calls in episode")

	// Tool calls are sorted by server, then tool name. With server="github":
	// "create_pull_request" < "get_file_contents" alphabetically.

	// First (alphabetically): create_pull_request — error call
	tc0 := ep.ToolCalls[0]
	assert.Equal(t, "create_pull_request", tc0.Tool, "tool name should match")
	assert.Equal(t, "github", tc0.Server, "server name should match")
	assert.Equal(t, (200+3000)/CharsPerToken, tc0.Tokens, "tokens should be estimated from sizes")
	assert.Equal(t, int64(600), tc0.DurationMS, "duration_ms should be 600")
	assert.Equal(t, "error", tc0.Status, "status should match")
	assert.Equal(t, "403 Resource not accessible by integration", tc0.Error, "error message should match")

	// Second (alphabetically): get_file_contents — success call
	tc1 := ep.ToolCalls[1]
	assert.Equal(t, "get_file_contents", tc1.Tool, "tool name should match")
	assert.Equal(t, "github", tc1.Server, "server name should match")
	assert.Equal(t, (400+9200)/CharsPerToken, tc1.Tokens, "tokens should be estimated from sizes")
	assert.Equal(t, int64(350), tc1.DurationMS, "duration_ms should be 350")
	assert.Equal(t, "success", tc1.Status, "status should match")
	assert.Empty(t, tc1.Error, "no error expected")
}

func TestBuildEpisodeDataSetsBlockedAtCapWhenFirewallCountHitsCap(t *testing.T) {
	t.Parallel()

	runs := []RunData{
		{
			RunID:        401,
			WorkflowName: "firewall-heavy",
			Status:       "completed",
			CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   401,
				WorkflowName: "firewall-heavy",
			},
			FirewallAnalysis: &FirewallAnalysis{
				AnalysisBase: AnalysisBase{
					TotalRequests:   100,
					BlockedRequests: firewallBlockedRequestCap, // exactly at the proxy cap
					AllowedRequests: 50,
				},
			},
		},
	}

	episodes, _ := buildEpisodeData(runs, processedRuns)
	require.Len(t, episodes, 1, "expected one episode")

	ep := episodes[0]
	assert.Equal(t, firewallBlockedRequestCap, ep.BlockedRequestCount, "blocked count should be accumulated")
	assert.True(t, ep.BlockedRequestAtCap, "BlockedRequestAtCap should be true when count == cap")
}

func TestBuildEpisodeDataDoesNotSetBlockedAtCapBelowThreshold(t *testing.T) {
	t.Parallel()

	runs := []RunData{
		{
			RunID:        402,
			WorkflowName: "low-block",
			Status:       "completed",
			CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   402,
				WorkflowName: "low-block",
			},
			FirewallAnalysis: &FirewallAnalysis{
				AnalysisBase: AnalysisBase{
					TotalRequests:   100,
					BlockedRequests: 8,
					AllowedRequests: 92,
				},
			},
		},
	}

	episodes, _ := buildEpisodeData(runs, processedRuns)
	require.Len(t, episodes, 1, "expected one episode")

	ep := episodes[0]
	assert.Equal(t, 8, ep.BlockedRequestCount, "blocked count should be accumulated")
	assert.False(t, ep.BlockedRequestAtCap, "BlockedRequestAtCap should be false when count is below cap")
}

func TestBuildEpisodeDataNoToolCallsWhenMCPUsageAbsent(t *testing.T) {
	t.Parallel()

	runs := []RunData{
		{
			RunID:        200,
			WorkflowName: "no-mcp-workflow",
			Status:       "completed",
			CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   200,
				WorkflowName: "no-mcp-workflow",
			},
			MCPToolUsage: nil, // no MCP tool usage
		},
	}

	episodes, _ := buildEpisodeData(runs, processedRuns)
	require.Len(t, episodes, 1, "expected one episode")

	ep := episodes[0]
	assert.Empty(t, ep.ToolCalls, "tool_calls should be absent when no MCP usage data")
}

func TestBuildEpisodeDataAggregatesAIC(t *testing.T) {
	t.Parallel()

	runs := []RunData{
		{
			RunID:        501,
			WorkflowName: "effective-a",
			Status:       "completed",
			AIC:          1.2,
			CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			RunID:        502,
			WorkflowName: "effective-b",
			Status:       "completed",
			AIC:          0.345,
			CreatedAt:    time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
		},
	}

	episodes, _ := buildEpisodeData(runs, nil)
	require.Len(t, episodes, 2, "expected one episode per unrelated run")

	byRunID := make(map[int64]EpisodeData, len(episodes))
	for _, episode := range episodes {
		require.Len(t, episode.RunIDs, 1, "each unrelated run should produce its own episode")
		byRunID[episode.RunIDs[0]] = episode
	}

	assert.InDelta(t, 1.2, byRunID[501].TotalAIC, 1e-9, "episode should preserve AIC from run 501")
	assert.InDelta(t, 0.345, byRunID[502].TotalAIC, 1e-9, "episode should preserve AIC from run 502")
}

func TestBuildEpisodeDataPreservesCanonicalIdentityAcrossSelections(t *testing.T) {
	t.Parallel()

	const episodeID = "100-1:org/repo/.github/workflows/root.yml@refs/heads/main"
	root := RunData{
		RunID: 100, Repository: "org/repo", TokenUsage: 10, AIC: 0.1,
		AwContext: &AwContext{RunID: "100", EpisodeID: episodeID},
	}
	legacyRoot := root
	legacyRoot.AwContext = nil
	child := RunData{
		RunID: 200, Repository: "org/repo", TokenUsage: 20, AIC: 0.2,
		AwContext: &AwContext{Repo: "org/repo", RunID: "100", WorkflowCallID: episodeID, EpisodeID: episodeID},
	}
	grandchild := RunData{
		RunID: 300, Repository: "org/repo", TokenUsage: 30, AIC: 0.3,
		AwContext: &AwContext{Repo: "org/repo", RunID: "200", WorkflowCallID: "200-1:worker", EpisodeID: episodeID},
	}

	tests := []struct {
		name      string
		runs      []RunData
		edgeCount int
		tokens    int
		aic       float64
	}{
		{"full chain", []RunData{root, child, grandchild}, 2, 60, 0.6},
		{"legacy root", []RunData{legacyRoot, child, grandchild}, 2, 60, 0.6},
		{"reversed chain", []RunData{grandchild, child, root}, 2, 60, 0.6},
		{"missing root", []RunData{child, grandchild}, 1, 50, 0.5},
		{"missing middle", []RunData{root, grandchild}, 0, 40, 0.4},
		{"single descendant", []RunData{grandchild}, 0, 30, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			episodes, edges := buildEpisodeData(tt.runs, nil)
			require.Len(t, episodes, 1)
			assert.Equal(t, episodeID, episodes[0].EpisodeID)
			assert.Equal(t, len(tt.runs), episodes[0].TotalRuns)
			assert.Equal(t, tt.tokens, episodes[0].TotalTokens)
			assert.InDelta(t, tt.aic, episodes[0].TotalAIC, 1e-9)
			require.Len(t, edges, tt.edgeCount)
			for _, edge := range edges {
				assert.Equal(t, episodeID, edge.EpisodeID)
				assert.NotEqual(t, edge.SourceRunID, edge.TargetRunID)
			}
		})
	}
}

func TestBuildEpisodeDataRejectsConflictingCanonicalLineage(t *testing.T) {
	t.Parallel()

	for _, runs := range [][]RunData{
		{
			{RunID: 100, AwContext: &AwContext{EpisodeID: "episode-a"}},
			{RunID: 200, AwContext: &AwContext{RunID: "100", EpisodeID: "episode-b"}},
		},
		{
			{RunID: 100, AwContext: &AwContext{EpisodeID: "episode-a"}},
			{RunID: 150, AwContext: &AwContext{RunID: "100"}},
			{RunID: 200, AwContext: &AwContext{RunID: "150", EpisodeID: "episode-b"}},
		},
	} {
		episodes, edges := buildEpisodeData(runs, nil)
		require.Len(t, episodes, 2)
		for _, edge := range edges {
			assert.NotEqual(t, int64(200), edge.TargetRunID, "a legacy bridge must not merge different canonical episodes")
		}
		ids := []string{episodes[0].EpisodeID, episodes[1].EpisodeID}
		assert.ElementsMatch(t, []string{"episode-a", "episode-b"}, ids)
	}
}

func TestBuildEpisodeDataCanonicalContextDoesNotInferMissingCaller(t *testing.T) {
	t.Parallel()

	created := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	runs := []RunData{
		{RunID: 100, Event: "push", Repository: "org/repo", HeadSHA: "abc", Branch: "main", CreatedAt: created},
		{
			RunID: 200, Event: "workflow_run", Repository: "org/repo", HeadSHA: "abc", Branch: "main", CreatedAt: created.Add(time.Minute),
			AwContext: &AwContext{RunID: "999", EpisodeID: "canonical-episode"},
		},
	}
	episodes, edges := buildEpisodeData(runs, nil)
	assert.Len(t, episodes, 2)
	assert.Empty(t, edges, "a matching SHA is not a substitute for the absent canonical caller")
}

func TestBuildEpisodeDataSharedRunDoesNotCreateSelfEdge(t *testing.T) {
	t.Parallel()

	run := RunData{
		RunID: 100, Event: "workflow_call", TokenUsage: 20,
		AwContext: &AwContext{
			RunID: "100", EpisodeID: "root-episode",
			HopID: "100-1:caller", ParentHopID: "100-1:root",
		},
	}
	episodes, edges := buildEpisodeData([]RunData{run}, nil)
	require.Len(t, episodes, 1)
	assert.Equal(t, "root-episode", episodes[0].EpisodeID)
	assert.Equal(t, 1, episodes[0].TotalRuns)
	assert.Equal(t, 20, episodes[0].TotalTokens)
	assert.Empty(t, edges, "hop relationships within a shared run are not run-to-run edges")
}

func TestBuildDispatchEpisodeEdgeRejectsRepositoryMismatch(t *testing.T) {
	t.Parallel()

	run := RunData{RunID: 200, AwContext: &AwContext{RunID: "100", Repo: "other/repo"}}
	_, ok := buildDispatchEpisodeEdge(run, map[int64]RunData{100: {RunID: 100, Repository: "org/repo"}})
	assert.False(t, ok)
}

func TestBuildEpisodeDataAggregatesToolCallsAcrossRuns(t *testing.T) {
	t.Parallel()

	// Two runs belonging to the same episode (via dispatch)
	workflowCallID := "dispatch:wc-42"
	runs := []RunData{
		{
			RunID:        301,
			WorkflowName: "orchestrator",
			Status:       "completed",
			CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			AwContext: &AwContext{
				WorkflowCallID: "wc-42",
			},
		},
		{
			RunID:        302,
			WorkflowName: "worker",
			Status:       "completed",
			CreatedAt:    time.Date(2024, 1, 1, 12, 1, 0, 0, time.UTC),
			AwContext: &AwContext{
				WorkflowCallID: "wc-42",
			},
		},
	}
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 301, WorkflowName: "orchestrator"},
			MCPToolUsage: &MCPToolUsageData{
				ToolCalls: []MCPToolCall{
					{
						ServerName: "github",
						ToolName:   "search_code",
						InputSize:  100,
						OutputSize: 500,
						Duration:   "200ms",
						Status:     "success",
					},
				},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 302, WorkflowName: "worker"},
			MCPToolUsage: &MCPToolUsageData{
				ToolCalls: []MCPToolCall{
					{
						ServerName: "github",
						ToolName:   "create_issue",
						InputSize:  50,
						OutputSize: 200,
						Duration:   "400ms",
						Status:     "success",
					},
				},
			},
		},
	}

	episodes, _ := buildEpisodeData(runs, processedRuns)
	require.Len(t, episodes, 1, "expected one merged episode from two dispatch runs")

	ep := episodes[0]
	assert.Equal(t, workflowCallID, ep.EpisodeID, "episode id should reflect dispatch call id")
	assert.Len(t, ep.ToolCalls, 2, "tool_calls should include calls from both runs")
}

func TestMCPToolCallToEpisodeToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          MCPToolCall
		expectedTool   string
		expectedServer string
		expectedTokens int
		expectedDurMS  int64
		expectedStatus string
		expectedError  string
	}{
		{
			name: "success call with duration",
			input: MCPToolCall{
				ServerName: "github",
				ToolName:   "list_issues",
				InputSize:  400,
				OutputSize: 1200,
				Duration:   "250ms",
				Status:     "success",
			},
			expectedTool:   "list_issues",
			expectedServer: "github",
			expectedTokens: (400 + 1200) / CharsPerToken,
			expectedDurMS:  250,
			expectedStatus: "success",
		},
		{
			name: "error call with error message",
			input: MCPToolCall{
				ServerName: "playwright",
				ToolName:   "navigate",
				InputSize:  100,
				OutputSize: 0,
				Duration:   "1s",
				Status:     "error",
				Error:      "timeout",
			},
			expectedTool:   "navigate",
			expectedServer: "playwright",
			expectedTokens: 100 / CharsPerToken,
			expectedDurMS:  1000,
			expectedStatus: "error",
			expectedError:  "timeout",
		},
		{
			name: "call without duration",
			input: MCPToolCall{
				ServerName: "github",
				ToolName:   "get_repo",
				InputSize:  200,
				OutputSize: 800,
				Duration:   "",
				Status:     "success",
			},
			expectedTool:   "get_repo",
			expectedServer: "github",
			expectedTokens: (200 + 800) / CharsPerToken,
			expectedDurMS:  0,
			expectedStatus: "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mcpToolCallToEpisodeToolCall(tt.input)
			assert.Equal(t, tt.expectedTool, got.Tool, "Tool should match")
			assert.Equal(t, tt.expectedServer, got.Server, "Server should match")
			assert.Equal(t, tt.expectedTokens, got.Tokens, "Tokens should be estimated from sizes")
			assert.Equal(t, tt.expectedDurMS, got.DurationMS, "DurationMS should match")
			assert.Equal(t, tt.expectedStatus, got.Status, "Status should match")
			assert.Equal(t, tt.expectedError, got.Error, "Error should match")
		})
	}
}

func TestCompareEpisodeSeedsPrefersKindThenConfidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		left     episodeSeed
		right    episodeSeed
		expected int
	}{
		{
			name: "workflow call beats dispatch",
			left: episodeSeed{
				EpisodeID:  "workflow_call:123",
				Kind:       "workflow_call",
				Confidence: "medium",
			},
			right: episodeSeed{
				EpisodeID:  "dispatch:123",
				Kind:       "dispatch_workflow",
				Confidence: "high",
			},
			expected: 1,
		},
		{
			name: "higher confidence wins within same kind",
			left: episodeSeed{
				EpisodeID:  "dispatch:high",
				Kind:       "dispatch_workflow",
				Confidence: "high",
			},
			right: episodeSeed{
				EpisodeID:  "dispatch:low",
				Kind:       "dispatch_workflow",
				Confidence: "low",
			},
			expected: 1,
		},
		{
			name: "episode id breaks ties deterministically",
			left: episodeSeed{
				EpisodeID:  "standalone:200",
				Kind:       "standalone",
				Confidence: "high",
			},
			right: episodeSeed{
				EpisodeID:  "standalone:100",
				Kind:       "standalone",
				Confidence: "high",
			},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, compareEpisodeSeeds(tt.left, tt.right), "seed precedence should be deterministic")
		})
	}
}

func TestFilterLineageCandidatesPrefersSingleNonNestedCandidate(t *testing.T) {
	t.Parallel()

	child := RunData{
		RunID:     30,
		Event:     "workflow_run",
		CreatedAt: time.Date(2024, 1, 1, 12, 10, 0, 0, time.UTC),
	}
	runs := []RunData{
		{
			RunID:      10,
			Event:      "push",
			Repository: "github/gh-aw",
			HeadSHA:    "abc123",
			Branch:     "main",
			CreatedAt:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			RunID:      20,
			Event:      "workflow_run",
			Repository: "github/gh-aw",
			HeadSHA:    "abc123",
			Branch:     "main",
			CreatedAt:  time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC),
		},
	}

	candidates := filterLineageCandidates(runs, child, func(candidate RunData) bool {
		return candidate.Repository == "github/gh-aw" && candidate.HeadSHA == "abc123" && candidate.Branch == "main"
	})

	require.Len(t, candidates, 1, "one non-nested candidate should be preferred")
	assert.Equal(t, int64(10), candidates[0].RunID, "the non-workflow_run parent should be selected")
}

func TestBuildWorkflowRunEpisodeEdgeReturnsNoEdgeForAmbiguousCandidates(t *testing.T) {
	t.Parallel()

	child := RunData{
		RunID:      300,
		Event:      "workflow_run",
		Repository: "github/gh-aw",
		HeadSHA:    "abc123",
		Branch:     "main",
		CreatedAt:  time.Date(2024, 1, 1, 12, 10, 0, 0, time.UTC),
	}
	runs := []RunData{
		{
			RunID:      100,
			Event:      "push",
			Repository: "github/gh-aw",
			HeadSHA:    "abc123",
			Branch:     "main",
			CreatedAt:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			RunID:      200,
			Event:      "pull_request",
			Repository: "github/gh-aw",
			HeadSHA:    "abc123",
			Branch:     "main",
			CreatedAt:  time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC),
		},
		child,
	}

	_, ok := buildWorkflowRunEpisodeEdge(child, runs)
	assert.False(t, ok, "an edge should not be created when multiple upstream candidates match")
}

func TestBuildDispatchEpisodeEdgeReturnsNoEdgeForInvalidRunID(t *testing.T) {
	t.Parallel()

	run := RunData{
		RunID: 500,
		AwContext: &AwContext{
			RunID:          "not-a-number",
			WorkflowCallID: "500-1",
		},
	}
	runsByID := map[int64]RunData{
		500: run,
	}

	_, ok := buildDispatchEpisodeEdge(run, runsByID)
	assert.False(t, ok, "dispatch edge should not be created when context.run_id is invalid")
}

func TestBuildDispatchEpisodeEdgeReturnsNoEdgeWhenSourceRunMissing(t *testing.T) {
	t.Parallel()

	run := RunData{
		RunID: 501,
		AwContext: &AwContext{
			RunID:          "999",
			WorkflowCallID: "501-1",
		},
	}
	runsByID := map[int64]RunData{
		501: run,
	}

	_, ok := buildDispatchEpisodeEdge(run, runsByID)
	assert.False(t, ok, "dispatch edge should not be created when the parent run is not present")
}

func TestClassifyEpisodeEscalationThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		episode        EpisodeData
		expectedOK     bool
		expectedReason string
	}{
		{
			name:           "repeated risky runs escalate",
			episode:        EpisodeData{RiskyNodeCount: 2},
			expectedOK:     true,
			expectedReason: "repeated_risky_runs",
		},
		{
			name:           "repeated new mcp failures escalate",
			episode:        EpisodeData{NewMCPFailureRunCount: 2},
			expectedOK:     true,
			expectedReason: "repeated_new_mcp_failures",
		},
		{
			name:           "repeated blocked request increases escalate",
			episode:        EpisodeData{BlockedRequestIncreaseRunCount: 2},
			expectedOK:     true,
			expectedReason: "repeated_blocked_request_increase",
		},
		{
			name:           "repeated resource heavy runs escalate",
			episode:        EpisodeData{ResourceHeavyNodeCount: 2},
			expectedOK:     true,
			expectedReason: "repeated_resource_heavy_for_domain",
		},
		{
			name:           "repeated poor control escalates",
			episode:        EpisodeData{PoorControlNodeCount: 2},
			expectedOK:     true,
			expectedReason: "repeated_poor_agentic_control",
		},
		{
			name:           "single signals do not escalate",
			episode:        EpisodeData{RiskyNodeCount: 1, ResourceHeavyNodeCount: 1, PoorControlNodeCount: 1},
			expectedOK:     false,
			expectedReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := classifyEpisodeEscalation(tt.episode)
			assert.Equal(t, tt.expectedOK, ok, "escalation eligibility should match threshold behavior")
			assert.Equal(t, tt.expectedReason, reason, "escalation reason should match threshold behavior")
		})
	}
}

func TestBuildSuggestedRoutePreferenceOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		episode  EpisodeData
		expected string
	}{
		{
			name:     "primary workflow wins",
			episode:  EpisodeData{PrimaryWorkflow: "orchestrator", WorkflowNames: []string{"worker-a", "worker-b"}},
			expected: "workflow:orchestrator",
		},
		{
			name:     "first workflow name is fallback",
			episode:  EpisodeData{WorkflowNames: []string{"worker-a", "worker-b"}},
			expected: "workflow:worker-a",
		},
		{
			name:     "repo owners fallback when no workflows known",
			episode:  EpisodeData{},
			expected: "repo:owners",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, buildSuggestedRoute(tt.episode), "suggested route should follow the documented preference order")
		})
	}
}
