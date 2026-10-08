//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractEngineConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		awInfoContent  string
		awInfoSubdir   string // subdir under tmpDir to place aw_info.json
		expectedEngine string
		expectedModel  string
		expectNil      bool
	}{
		{
			name:           "full engine config",
			awInfoContent:  `{"engine_id":"copilot","engine_name":"GitHub Copilot CLI","model":"gpt-4","version":"1.0.0","cli_version":"2.0.0","event_name":"issues","repository":"owner/repo"}`,
			expectedEngine: "copilot",
			expectedModel:  "gpt-4",
		},
		{
			name:           "minimal engine config",
			awInfoContent:  `{"engine_id":"claude"}`,
			expectedEngine: "claude",
		},
		{
			name:      "empty logs path",
			expectNil: true,
		},
		{
			name:           "with firewall version",
			awInfoContent:  `{"engine_id":"copilot","awf_version":"1.2.3"}`,
			expectedEngine: "copilot",
		},
		{
			name:           "aw_info.json in activation subdirectory",
			awInfoContent:  `{"engine_id":"claude","model":"claude-sonnet"}`,
			awInfoSubdir:   "activation",
			expectedEngine: "claude",
			expectedModel:  "claude-sonnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.expectNil && tt.awInfoContent == "" {
				result := extractEngineConfigWithInferredEngine("", "")
				assert.Nil(t, result, "Should return nil for empty logs path")
				return
			}

			tmpDir := testutil.TempDir(t, "engine-config-*")
			targetDir := tmpDir
			if tt.awInfoSubdir != "" {
				targetDir = filepath.Join(tmpDir, tt.awInfoSubdir)
				err := os.MkdirAll(targetDir, 0755)
				require.NoError(t, err, "Should create subdir")
			}
			err := os.WriteFile(filepath.Join(targetDir, "aw_info.json"), []byte(tt.awInfoContent), 0644)
			require.NoError(t, err, "Should write aw_info.json")

			result := extractEngineConfigWithInferredEngine(tmpDir, "")
			if tt.expectNil {
				assert.Nil(t, result, "Should return nil")
				return
			}

			require.NotNil(t, result, "Engine config should not be nil")
			assert.Equal(t, tt.expectedEngine, result.EngineID, "Engine ID should match")
			if tt.expectedModel != "" {
				assert.Equal(t, tt.expectedModel, result.Model, "Model should match")
			}
		})
	}
}

func TestExtractEngineConfigUsesEffectiveRoutedModel(t *testing.T) {
	tests := []struct {
		name          string
		awInfo        string
		routingRecord string
		model         string
		effort        string
		status        string
		requested     string
	}{
		{
			name:      "new routed aw_info",
			awInfo:    `{"engine_id":"copilot","model":"gpt-5.6-luna","requested_model":"auto","model_routing":{"status":"selected","wire_model":"gpt-5.6-luna","effort":"xhigh","applied_effort":"xhigh"}}`,
			model:     "gpt-5.6-luna",
			effort:    "xhigh",
			status:    "selected",
			requested: "auto",
		},
		{
			name:          "legacy routed artifact",
			awInfo:        `{"engine_id":"copilot","model":"auto"}`,
			routingRecord: `{"_schema":"model-routing/v0.28.37","stage":"selection","selected_model":"gpt-5.6-luna","wire_model":"gpt-5.6-luna","selected_effort":"high"}`,
			model:         "gpt-5.6-luna",
			effort:        "high",
			status:        "selected",
			requested:     "auto",
		},
		{
			name:          "failed route does not expose placeholder",
			awInfo:        `{"engine_id":"copilot","model":"auto","requested_model":"auto","model_routing":{"status":"failed","failure_code":"no_route"}}`,
			routingRecord: `{"_schema":"model-routing/v0.28.37","stage":"failure","code":"no_route"}`,
			status:        "failed",
			requested:     "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(tt.awInfo), 0o644))
			if tt.routingRecord != "" {
				routingPath := filepath.Join(dir, "sandbox", "firewall", "logs", "api-proxy-logs", "model-routing.jsonl")
				require.NoError(t, os.MkdirAll(filepath.Dir(routingPath), 0o755))
				require.NoError(t, os.WriteFile(routingPath, []byte(tt.routingRecord+"\n"), 0o644))
			}

			config := extractEngineConfigWithInferredEngine(dir, "")
			require.NotNil(t, config)
			assert.Equal(t, tt.model, config.Model)
			assert.Equal(t, tt.effort, config.ModelEffort)
			assert.Equal(t, tt.status, config.ModelRoutingStatus)
			assert.Equal(t, tt.requested, config.RequestedModel)
		})
	}
}

func TestExtractEngineConfigWithDetails(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "engine-config-details-*")
	awInfoContent := `{
		"engine_id": "copilot",
		"engine_name": "GitHub Copilot CLI",
		"model": "gpt-4",
		"version": "1.0.0",
		"cli_version": "2.0.0",
		"awf_version": "3.0.0",
		"event_name": "issues.opened",
		"repository": "org/repo"
	}`
	err := os.WriteFile(filepath.Join(tmpDir, "aw_info.json"), []byte(awInfoContent), 0644)
	require.NoError(t, err, "Should write aw_info.json")

	result := extractEngineConfigWithInferredEngine(tmpDir, "")
	require.NotNil(t, result, "Engine config should not be nil")
	assert.Equal(t, "copilot", result.EngineID, "Engine ID should match")
	assert.Equal(t, "GitHub Copilot CLI", result.EngineName, "Engine name should match")
	assert.Equal(t, "gpt-4", result.Model, "Model should match")
	assert.Equal(t, "1.0.0", result.Version, "Version should match")
	assert.Equal(t, "2.0.0", result.CLIVersion, "CLI version should match")
	assert.Equal(t, "3.0.0", result.FirewallVersion, "Firewall version should match")
	assert.Equal(t, "issues.opened", result.TriggerEvent, "Trigger event should match")
	assert.Equal(t, "org/repo", result.Repository, "Repository should match")
}

func TestExtractEngineConfigInferredWithoutAwInfo(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "engine-infer-*")
	logContent := `{"type":"result","subtype":"success","num_turns":3,"usage":{"input_tokens":100,"output_tokens":200}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent-stdio.log"), []byte(logContent), 0o644))

	_, inferredEngineID := inferFallbackLogMetrics(tmpDir)
	result := extractEngineConfigWithInferredEngine(tmpDir, inferredEngineID)
	require.NotNil(t, result, "Engine config should be inferred when aw_info.json is missing but agent log is available")
	assert.NotEmpty(t, result.EngineID, "Inferred engine ID should not be empty")
}

func TestInferFallbackLogMetricsFindsNestedAgentStdioLog(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "engine-infer-nested-*")
	nestedDir := filepath.Join(tmpDir, "agent", "logs")
	require.NoError(t, os.MkdirAll(nestedDir, 0o755))
	logContent := `{"type":"result","subtype":"success","num_turns":4,"usage":{"input_tokens":120,"output_tokens":80}}`
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "agent-stdio.log"), []byte(logContent), 0o644))

	metrics, inferredEngineID := inferFallbackLogMetrics(tmpDir)
	assert.Positive(t, metrics.Turns, "Fallback metrics should be extracted from nested agent-stdio.log")
	assert.NotEmpty(t, inferredEngineID, "Engine ID should be inferred from nested agent-stdio.log")
}

func TestExtractPromptAnalysis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		promptContent   string
		promptDir       string // relative path for prompt.txt
		expectNil       bool
		expectedSize    int
		expectedRelPath string // expected relative path in PromptFile
	}{
		{
			name:            "prompt in root directory",
			promptContent:   "This is a test prompt for the AI agent",
			promptDir:       "",
			expectedSize:    38,
			expectedRelPath: "prompt.txt",
		},
		{
			name:            "prompt in aw-prompts subdirectory",
			promptContent:   "Another test prompt.",
			promptDir:       "aw-prompts",
			expectedSize:    20,
			expectedRelPath: filepath.Join("aw-prompts", "prompt.txt"),
		},
		{
			name:            "prompt in activation/aw-prompts subdirectory",
			promptContent:   "Activation prompt content here",
			promptDir:       filepath.Join("activation", "aw-prompts"),
			expectedSize:    30,
			expectedRelPath: filepath.Join("activation", "aw-prompts", "prompt.txt"),
		},
		{
			name:            "prompt in agent/aw-prompts subdirectory",
			promptContent:   "Agent prompt.",
			promptDir:       filepath.Join("agent", "aw-prompts"),
			expectedSize:    13,
			expectedRelPath: filepath.Join("agent", "aw-prompts", "prompt.txt"),
		},
		{
			name:      "no prompt file",
			expectNil: true,
		},
		{
			name:      "empty logs path",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.name == "empty logs path" {
				result := extractPromptAnalysis("")
				assert.Nil(t, result, "Should return nil for empty logs path")
				return
			}

			tmpDir := testutil.TempDir(t, "prompt-analysis-*")

			if tt.promptContent != "" {
				promptDir := tmpDir
				if tt.promptDir != "" {
					promptDir = filepath.Join(tmpDir, tt.promptDir)
					err := os.MkdirAll(promptDir, 0755)
					require.NoError(t, err, "Should create prompt directory")
				}
				err := os.WriteFile(filepath.Join(promptDir, "prompt.txt"), []byte(tt.promptContent), 0644)
				require.NoError(t, err, "Should write prompt.txt")
			}

			result := extractPromptAnalysis(tmpDir)
			if tt.expectNil {
				assert.Nil(t, result, "Should return nil when no prompt")
				return
			}

			require.NotNil(t, result, "Prompt analysis should not be nil")
			assert.Equal(t, tt.expectedSize, result.PromptSize, "Prompt size should match")
			assert.Equal(t, tt.expectedRelPath, result.PromptFile, "Prompt file should be a relative path")
		})
	}
}

func TestBuildSessionAnalysis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		run             WorkflowRun
		metrics         LogMetrics
		jobDetails      []JobInfoWithDuration
		expectTimeout   bool
		expectTurns     int
		expectNoopCount int
	}{
		{
			name: "successful run with metrics",
			run: WorkflowRun{
				Duration:   5 * time.Minute,
				Conclusion: "success",
				NoopCount:  0,
			},
			metrics: LogMetrics{
				Turns:      10,
				TokenUsage: 5000,
			},
			expectTurns: 10,
		},
		{
			name: "cancelled run detects timeout",
			run: WorkflowRun{
				Duration:   30 * time.Minute,
				Conclusion: "cancelled",
			},
			metrics:       LogMetrics{Turns: 3},
			expectTimeout: true,
			expectTurns:   3,
		},
		{
			name: "timed out run",
			run: WorkflowRun{
				Duration:   60 * time.Minute,
				Conclusion: "timed_out",
			},
			metrics:       LogMetrics{Turns: 5},
			expectTimeout: true,
			expectTurns:   5,
		},
		{
			name: "job-level timeout detection",
			run: WorkflowRun{
				Duration:   10 * time.Minute,
				Conclusion: "failure",
			},
			metrics: LogMetrics{Turns: 2},
			jobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "agent", Conclusion: "cancelled"}},
			},
			expectTimeout: true,
			expectTurns:   2,
		},
		{
			name: "run with noops",
			run: WorkflowRun{
				Duration:   5 * time.Minute,
				Conclusion: "success",
				NoopCount:  2,
			},
			metrics:         LogMetrics{Turns: 5},
			expectTurns:     5,
			expectNoopCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			processedRun := ProcessedRun{
				Run:        tt.run,
				JobDetails: tt.jobDetails,
			}

			result := buildSessionAnalysis(processedRun, tt.metrics)
			require.NotNil(t, result, "Session analysis should not be nil")
			assert.Equal(t, tt.expectTurns, result.TurnCount, "Turn count should match")
			assert.Equal(t, tt.expectTimeout, result.TimeoutDetected, "Timeout detection should match")
			assert.Equal(t, tt.expectNoopCount, result.NoopCount, "Noop count should match")

			if tt.run.Duration > 0 {
				assert.NotEmpty(t, result.WallTime, "Wall time should be set when duration is positive")
			}

			if tt.metrics.Turns > 0 && tt.run.Duration > 0 {
				assert.NotEmpty(t, result.AvgTurnDuration, "Avg turn duration should be set")
			}

			if tt.metrics.TokenUsage > 0 && tt.run.Duration > 0 {
				assert.Positive(t, result.TokensPerMinute, "Tokens per minute should be positive")
			}
		})
	}
}

func TestBuildSafeOutputSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		items         []CreatedItemReport
		chainMetrics  SafeOutputChainMetrics
		expectNil     bool
		expectedTotal int
		expectedTypes int
	}{
		{
			name:      "nil items",
			items:     nil,
			expectNil: true,
		},
		{
			name:      "empty items",
			items:     []CreatedItemReport{},
			expectNil: true,
		},
		{
			name:          "chain metrics only",
			items:         nil,
			expectNil:     false,
			expectedTotal: 0,
			expectedTypes: 0,
			chainMetrics: SafeOutputChainMetrics{
				TemporaryIDMapStatus: temporaryIDMapStatusMissing,
			},
		},
		{
			name: "single item",
			items: []CreatedItemReport{
				{Type: "create_pull_request", URL: "https://github.com/org/repo/pull/1"},
			},
			expectedTotal: 1,
			expectedTypes: 1,
		},
		{
			name: "multiple items of different types",
			items: []CreatedItemReport{
				{Type: "create_pull_request", URL: "https://github.com/org/repo/pull/1"},
				{Type: "create_pull_request", URL: "https://github.com/org/repo/pull/2"},
				{Type: "add_comment", URL: "https://github.com/org/repo/issues/1#comment-1"},
				{Type: "create_review", URL: "https://github.com/org/repo/pull/1/reviews/1"},
			},
			expectedTotal: 4,
			expectedTypes: 3,
			chainMetrics: SafeOutputChainMetrics{
				TemporaryIDMapStatus:       temporaryIDMapStatusLoaded,
				TemporaryIDMappings:        2,
				ChainedTargetCount:         1,
				ChainedFollowupActionCount: 2,
				DelegatedTempTargetCount:   1,
				ClosedTempTargetCount:      1,
			},
		},
		{
			name: "items with unknown types",
			items: []CreatedItemReport{
				{Type: ""},
				{Type: "custom_action"},
			},
			expectedTotal: 2,
			expectedTypes: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := buildSafeOutputSummary(tt.items, tt.chainMetrics)
			if tt.expectNil {
				assert.Nil(t, result, "Should return nil for empty items")
				return
			}

			require.NotNil(t, result, "Summary should not be nil")
			assert.Equal(t, tt.expectedTotal, result.TotalItems, "Total items should match")
			assert.Len(t, result.ItemsByType, tt.expectedTypes, "Type count should match")
			if tt.expectedTypes > 0 {
				assert.NotEmpty(t, result.Summary, "Summary string should not be empty")
			} else {
				assert.Equal(t, "No items", result.Summary, "Summary string should preserve the no-items fallback when only chain metrics are present")
			}
			assert.Len(t, result.TypeDetails, tt.expectedTypes, "Type details should match type count")
			assert.Equal(t, tt.chainMetrics.TemporaryIDMapStatus, result.TemporaryIDMapStatus, "temp map status should match")
			assert.Equal(t, tt.chainMetrics.TemporaryIDMappings, result.TemporaryIDMappings, "temp mapping count should match")
			assert.Equal(t, tt.chainMetrics.ClosedTempTargetCount, result.ClosedTempTargetCount, "closed temp target count should match")
		})
	}
}

func TestBuildSafeOutputSummaryString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		details  []SafeOutputTypeDetail
		expected string
	}{
		{
			name:     "empty details",
			details:  nil,
			expected: "No items",
		},
		{
			name: "single type",
			details: []SafeOutputTypeDetail{
				{Type: "create_pull_request", Count: 2},
			},
			expected: "2 PR(s)",
		},
		{
			name: "multiple types",
			details: []SafeOutputTypeDetail{
				{Type: "create_pull_request", Count: 2},
				{Type: "add_comment", Count: 1},
			},
			expected: "2 PR(s), 1 comment(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := buildSafeOutputSummaryString(tt.details)
			assert.Equal(t, tt.expected, result, "Summary string should match")
		})
	}
}

func TestPrettifySafeOutputType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "PR(s)", prettifySafeOutputType("create_pull_request"), "Should prettify PR type")
	assert.Equal(t, "issue(s)", prettifySafeOutputType("create_issue"), "Should prettify issue type")
	assert.Equal(t, "comment(s)", prettifySafeOutputType("add_comment"), "Should prettify comment type")
	assert.Equal(t, "custom_type", prettifySafeOutputType("custom_type"), "Should return unknown types as-is")
}

func TestBuildMCPServerHealth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mcpUsage    *MCPToolUsageData
		mcpFailures []MCPFailureReport
		expectNil   bool
	}{
		{
			name:      "nil inputs",
			expectNil: true,
		},
		{
			name: "with server stats only",
			mcpUsage: &MCPToolUsageData{
				Servers: []MCPServerStats{
					{
						MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 40, ErrorCount: 2},
						RequestCount:       50,
						AvgDuration:        "120ms",
					},
					{
						MCPServerStatsBase: MCPServerStatsBase{ServerName: "filesystem", ToolCallCount: 15, ErrorCount: 0},
						RequestCount:       20,
						AvgDuration:        "5ms",
					},
				},
			},
		},
		{
			name: "with failures only",
			mcpFailures: []MCPFailureReport{
				{ServerName: "github", Status: "connection_timeout"},
			},
		},
		{
			name: "with stats and failures",
			mcpUsage: &MCPToolUsageData{
				Servers: []MCPServerStats{
					{
						MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 40, ErrorCount: 5},
						RequestCount:       50,
						AvgDuration:        "200ms",
					},
				},
			},
			mcpFailures: []MCPFailureReport{
				{ServerName: "failing-server", Status: "crash"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := buildMCPServerHealth(tt.mcpUsage, tt.mcpFailures)
			if tt.expectNil {
				assert.Nil(t, result, "Should return nil when both inputs are nil/empty")
				return
			}

			require.NotNil(t, result, "Health should not be nil")
			assert.NotEmpty(t, result.Summary, "Summary should not be empty")
			assert.GreaterOrEqual(t, result.TotalServers, 0, "Total servers should be non-negative")

			if len(tt.mcpFailures) > 0 {
				assert.Positive(t, result.FailedSvrs, "Should have failed servers when failures reported")
			}
		})
	}
}

func TestBuildMCPServerHealthErrorRate(t *testing.T) {
	t.Parallel()
	mcpUsage := &MCPToolUsageData{
		Servers: []MCPServerStats{
			{
				MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 80, ErrorCount: 15},
				RequestCount:       100,
				AvgDuration:        "200ms",
			},
		},
	}

	result := buildMCPServerHealth(mcpUsage, nil)
	require.NotNil(t, result, "Health should not be nil")
	assert.InDelta(t, 15.0, result.ErrorRate, 0.01, "Error rate should be 15%")
	assert.Len(t, result.Servers, 1, "Should have 1 server")

	// Server with >10% error rate should show degraded
	assert.Contains(t, result.Servers[0].Status, "degraded", "Server with >10% error rate should be degraded")
	assert.Equal(t, 1, result.DegradedSvrs, "Should count 1 degraded server")
	assert.Equal(t, 0, result.HealthySvrs, "Should count 0 healthy servers")
	assert.Equal(t, 0, result.FailedSvrs, "Should count 0 failed servers")
}

func TestBuildSlowestToolCalls(t *testing.T) {
	t.Parallel()
	calls := []MCPToolCall{
		{ServerName: "github", ToolName: "search_code", Duration: "100ms"},
		{ServerName: "github", ToolName: "get_file", Duration: "500ms"},
		{ServerName: "filesystem", ToolName: "read", Duration: "50ms"},
		{ServerName: "github", ToolName: "create_pr", Duration: "2s"},
		{ServerName: "github", ToolName: "list_issues", Duration: "300ms"},
		{ServerName: "filesystem", ToolName: "write", Duration: "1s"},
	}

	result := buildSlowestToolCalls(calls, 3)
	require.Len(t, result, 3, "Should return top 3 slowest calls")
	assert.Equal(t, "create_pr", result[0].ToolName, "Slowest call should be create_pr")
	assert.Equal(t, "write", result[1].ToolName, "Second slowest should be write")
	assert.Equal(t, "get_file", result[2].ToolName, "Third slowest should be get_file")
}

func TestBuildSlowestToolCallsEmpty(t *testing.T) {
	t.Parallel()
	result := buildSlowestToolCalls(nil, 5)
	assert.Nil(t, result, "Should return nil for empty calls")

	result = buildSlowestToolCalls([]MCPToolCall{}, 5)
	assert.Nil(t, result, "Should return nil for empty slice")
}

func TestBuildAuditDataWithExpandedSections(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "audit-expanded-*")

	// Create test aw_info.json in activation/ subdir (unflattened artifact structure)
	activationDir := filepath.Join(tmpDir, "activation")
	err := os.MkdirAll(filepath.Join(activationDir, "aw-prompts"), 0755)
	require.NoError(t, err, "Should create activation/aw-prompts directory")

	awInfoContent := `{"engine_id":"copilot","engine_name":"GitHub Copilot CLI","model":"gpt-4","version":"1.0"}`
	err = os.WriteFile(filepath.Join(activationDir, "aw_info.json"), []byte(awInfoContent), 0644)
	require.NoError(t, err, "Should write aw_info.json in activation/")

	// Create test prompt.txt in activation/aw-prompts/ (unflattened)
	promptContent := "Please fix the bug in the login page."
	err = os.WriteFile(filepath.Join(activationDir, "aw-prompts", "prompt.txt"), []byte(promptContent), 0644)
	require.NoError(t, err, "Should write prompt.txt in activation/aw-prompts/")

	// Create safe output manifest
	manifestContent := `{"type":"create_pull_request","url":"https://github.com/org/repo/pull/1","repo":"org/repo","number":1,"timestamp":"2024-01-01T10:00:00Z"}
{"type":"add_comment","url":"https://github.com/org/repo/issues/1#comment-1","repo":"org/repo","timestamp":"2024-01-01T10:01:00Z"}`
	err = os.WriteFile(filepath.Join(tmpDir, safeOutputItemsManifestFilename), []byte(manifestContent), 0644)
	require.NoError(t, err, "Should write safe output manifest")

	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   12345,
			WorkflowName: "Test Workflow",
			Status:       "completed",
			Conclusion:   "success",
			Duration:     5 * time.Minute,
			LogsPath:     tmpDir,
			TokenUsage:   5000,
			Turns:        10,
		},
		MCPFailures: []MCPFailureReport{
			{ServerName: "broken-server", Status: "timeout"},
		},
	}

	metrics := LogMetrics{
		TokenUsage: 5000,
		Turns:      10,
	}

	mcpToolUsage := &MCPToolUsageData{
		Servers: []MCPServerStats{
			{
				MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 25, ErrorCount: 1},
				RequestCount:       30,
				AvgDuration:        "150ms",
			},
		},
		Summary: []MCPToolSummary{
			{ServerName: "github", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "search_code", CallCount: 10}},
		},
	}

	auditData := buildAuditData(context.Background(), processedRun, metrics, mcpToolUsage)

	// Verify new expanded sections are populated
	t.Run("AuditEngineConfig", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, auditData.EngineConfig, "Engine config should be populated")
		assert.Equal(t, "copilot", auditData.EngineConfig.EngineID, "Engine ID should match")
		assert.Equal(t, "gpt-4", auditData.EngineConfig.Model, "Model should match")
	})

	t.Run("PromptAnalysis", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, auditData.PromptAnalysis, "Prompt analysis should be populated")
		assert.Len(t, promptContent, auditData.PromptAnalysis.PromptSize, "Prompt size should match")
		assert.Equal(t, filepath.Join("activation", "aw-prompts", "prompt.txt"), auditData.PromptAnalysis.PromptFile, "Prompt file should be a relative path")
	})

	t.Run("SessionAnalysis", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, auditData.SessionAnalysis, "Session analysis should be populated")
		assert.Equal(t, 10, auditData.SessionAnalysis.TurnCount, "Turn count should match")
		assert.NotEmpty(t, auditData.SessionAnalysis.WallTime, "Wall time should be set")
		assert.NotEmpty(t, auditData.SessionAnalysis.AvgTurnDuration, "Avg turn duration should be set")
		assert.Positive(t, auditData.SessionAnalysis.TokensPerMinute, "Tokens per minute should be positive")
		assert.False(t, auditData.SessionAnalysis.TimeoutDetected, "Should not detect timeout for success")
	})

	t.Run("SafeOutputSummary", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, auditData.SafeOutputSummary, "Safe output summary should be populated")
		assert.Equal(t, 2, auditData.SafeOutputSummary.TotalItems, "Should have 2 total items")
		assert.Len(t, auditData.SafeOutputSummary.ItemsByType, 2, "Should have 2 types")
		assert.NotEmpty(t, auditData.SafeOutputSummary.Summary, "Summary string should be set")
	})

	t.Run("MCPServerHealth", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, auditData.MCPServerHealth, "MCP server health should be populated")
		assert.Equal(t, 2, auditData.MCPServerHealth.TotalServers, "Should have 2 servers (1 from stats + 1 failed)")
		assert.Equal(t, 1, auditData.MCPServerHealth.FailedSvrs, "Should have 1 failed server")
		assert.Equal(t, 1, auditData.MCPServerHealth.HealthySvrs, "Should have 1 healthy server")
		assert.NotEmpty(t, auditData.MCPServerHealth.Summary, "Summary should be set")
	})

	// Verify existing sections still work
	t.Run("ExistingFieldsPreserved", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, int64(12345), auditData.Overview.RunID, "Run ID should be preserved")
		assert.Equal(t, 5000, auditData.Metrics.TokenUsage, "Token usage should be preserved")
		assert.NotNil(t, auditData.MCPToolUsage, "MCP tool usage should be preserved")
		assert.Len(t, auditData.MCPFailures, 1, "MCP failures should be preserved")
	})
}

func TestBuildAuditDataExpandedWithNoData(t *testing.T) {
	t.Parallel()
	// Test that expanded sections are nil when no data is available
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   99999,
			WorkflowName: "Minimal Workflow",
			Status:       "completed",
			Conclusion:   "success",
		},
	}
	metrics := LogMetrics{}

	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

	assert.Nil(t, auditData.EngineConfig, "Engine config should be nil without aw_info.json")
	assert.Nil(t, auditData.PromptAnalysis, "Prompt analysis should be nil without prompt.txt")
	assert.NotNil(t, auditData.SessionAnalysis, "Session analysis should always be present")
	assert.Nil(t, auditData.SafeOutputSummary, "Safe output summary should be nil without items")
	assert.Nil(t, auditData.MCPServerHealth, "MCP server health should be nil without data")
}

func TestAwInfoHasMCPServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		awInfoContent  string
		awInfoSubdir   string
		expectedNames  []string
		expectedHasMCP bool
	}{
		{
			name:           "with MCP servers",
			awInfoContent:  `{"engine_id":"copilot","steps":{"mcp_servers":{"github":{},"filesystem":{}}}}`,
			expectedNames:  []string{"filesystem", "github"},
			expectedHasMCP: true,
		},
		{
			name:           "without MCP servers",
			awInfoContent:  `{"engine_id":"copilot","steps":{"firewall":"squid"}}`,
			expectedHasMCP: false,
		},
		{
			name:           "without steps",
			awInfoContent:  `{"engine_id":"copilot"}`,
			expectedHasMCP: false,
		},
		{
			name:           "with MCP servers in activation subdir",
			awInfoContent:  `{"engine_id":"copilot","steps":{"mcp_servers":{"github":{}}}}`,
			awInfoSubdir:   "activation",
			expectedNames:  []string{"github"},
			expectedHasMCP: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := testutil.TempDir(t, "mcp-servers-*")
			targetDir := tmpDir
			if tt.awInfoSubdir != "" {
				targetDir = filepath.Join(tmpDir, tt.awInfoSubdir)
				err := os.MkdirAll(targetDir, 0755)
				require.NoError(t, err, "Should create subdir")
			}
			err := os.WriteFile(filepath.Join(targetDir, "aw_info.json"), []byte(tt.awInfoContent), 0644)
			require.NoError(t, err, "Should write aw_info.json")

			names, hasMCP := extractMCPServerNamesFromAwInfo(tmpDir)
			assert.Equal(t, tt.expectedHasMCP, hasMCP, "Has MCP servers should match")
			if tt.expectedHasMCP {
				assert.Equal(t, tt.expectedNames, names, "MCP server names should match")
			}
		})
	}
}

func TestMCPServerHealthDetailJSONSchema(t *testing.T) {
	t.Parallel()
	detail := MCPServerHealthDetail{
		MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 40, ErrorCount: 2},
		RequestCount:       50,
		ErrorRate:          4,
		ErrorRateStr:       "4.0%",
		AvgLatency:         "120ms",
		Status:             "healthy",
	}

	data, err := json.Marshal(detail)
	require.NoError(t, err, "Marshalling health detail should succeed")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded), "Result should be valid JSON")

	assert.Equal(t, "github", decoded["server_name"], "server_name key should be preserved")
	assert.InDelta(t, 50, decoded["request_count"], 0.001, "request_count key should be preserved")
	assert.InDelta(t, 40, decoded["tool_calls"], 0.001, "tool_calls key should be preserved")
	assert.InDelta(t, 2, decoded["error_count"], 0.001, "error_count key should be preserved")
	assert.InDelta(t, 4.0, decoded["error_rate"], 0.001, "error_rate key should be preserved")
	assert.Equal(t, "4.0%", decoded["error_rate_str"], "error_rate_str key should be preserved")
	assert.Equal(t, "120ms", decoded["avg_latency"], "avg_latency key should be preserved")
	assert.Equal(t, "healthy", decoded["status"], "status key should be preserved")

	var roundTripped MCPServerHealthDetail
	require.NoError(t, json.Unmarshal(data, &roundTripped), "Unmarshalling should succeed")
	assert.Equal(t, detail, roundTripped, "Round-tripped value should equal the original")
}

func TestMCPServerHealthDetailJSONKeepsZeroErrorCount(t *testing.T) {
	t.Parallel()
	detail := MCPServerHealthDetail{
		MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 40},
		RequestCount:       50,
		Status:             "healthy",
	}

	data, err := json.Marshal(detail)
	require.NoError(t, err, "Marshalling health detail should succeed")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded), "Result should be valid JSON")

	require.Contains(t, decoded, "error_count", "error_count should be emitted even when zero")
	assert.InDelta(t, 0, decoded["error_count"], 0.001, "error_count should be zero")
}
