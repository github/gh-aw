//go:build !integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditJqFilter_BasicFiltering(t *testing.T) {
	// Create sample audit JSON output
	auditJSON := `{
  "overview": {
    "run_id": 123456,
    "workflow_name": "test-workflow",
    "status": "completed",
    "conclusion": "success"
  },
  "metrics": {
    "token_usage": 1000,
    "estimated_cost": "$0.50"
  },
  "jobs": [
    {"name": "job1", "status": "completed"},
    {"name": "job2", "status": "completed"}
  ]
}`

	tests := []struct {
		name     string
		jqFilter string
		validate func(t *testing.T, output string)
		wantErr  bool
	}{
		{
			name:     "extract overview",
			jqFilter: ".overview",
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON")
				runID, ok := result["run_id"].(float64)
				require.True(t, ok, "run_id should be a number")
				assert.Equal(t, int64(123456), int64(runID), "Should extract run_id from overview")
				assert.Equal(t, "test-workflow", result["workflow_name"], "Should extract workflow_name from overview")
			},
			wantErr: false,
		},
		{
			name:     "extract metrics",
			jqFilter: ".metrics",
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON")
				tokenUsage, ok := result["token_usage"].(float64)
				require.True(t, ok, "token_usage should be a number")
				assert.Equal(t, int64(1000), int64(tokenUsage), "Should extract token_usage from metrics")
			},
			wantErr: false,
		},
		{
			name:     "extract jobs array",
			jqFilter: ".jobs",
			validate: func(t *testing.T, output string) {
				var result []map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON array")
				assert.Len(t, result, 2, "Should have 2 jobs")
				assert.Equal(t, "job1", result[0]["name"], "First job should be job1")
			},
			wantErr: false,
		},
		{
			name:     "extract specific field",
			jqFilter: ".overview.run_id",
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				assert.Equal(t, "123456", output, "Should extract run_id as string")
			},
			wantErr: false,
		},
		{
			name:     "identity filter",
			jqFilter: ".",
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON")
				assert.Contains(t, result, "overview", "Should contain overview")
				assert.Contains(t, result, "metrics", "Should contain metrics")
				assert.Contains(t, result, "jobs", "Should contain jobs")
			},
			wantErr: false,
		},
		{
			name:     "invalid jq filter",
			jqFilter: ".[invalid",
			validate: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ApplyJqFilter(auditJSON, tt.jqFilter)

			if tt.wantErr {
				assert.Error(t, err, "Expected error for invalid jq filter")
				return
			}

			require.NoError(t, err, "Should apply jq filter without error")
			if tt.validate != nil {
				tt.validate(t, output)
			}
		})
	}
}

func TestAuditJqFilter_ComplexFiltering(t *testing.T) {
	// Create more complex audit JSON output with nested structures
	auditJSON := `{
  "overview": {
    "run_id": 123456,
    "workflow_name": "test-workflow",
    "status": "completed",
    "conclusion": "success",
    "created_at": "2024-01-01T00:00:00Z",
    "duration": "5m30s",
    "url": "https://github.com/owner/repo/actions/runs/123456"
  },
  "metrics": {
    "token_usage": 1000,
    "estimated_cost": "$0.50",
    "turns": 5,
    "error_count": 0,
    "warning_count": 2
  },
  "jobs": [
    {"name": "job1", "status": "completed", "conclusion": "success", "duration": "2m10s"},
    {"name": "job2", "status": "completed", "conclusion": "success", "duration": "3m20s"}
  ],
  "missing_tools": [
    {"tool": "github_api", "reason": "not configured"},
    {"tool": "slack_webhook", "reason": "token missing"}
  ]
}`

	tests := []struct {
		name     string
		jqFilter string
		validate func(t *testing.T, output string)
	}{
		{
			name:     "map job names",
			jqFilter: ".jobs | map(.name)",
			validate: func(t *testing.T, output string) {
				var result []string
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON array")
				assert.Equal(t, []string{"job1", "job2"}, result, "Should extract job names")
			},
		},
		{
			name:     "count jobs",
			jqFilter: ".jobs | length",
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				assert.Equal(t, "2", output, "Should count 2 jobs")
			},
		},
		{
			name:     "select specific job",
			jqFilter: `.jobs[] | select(.name == "job1")`,
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON")
				assert.Equal(t, "job1", result["name"], "Should select job1")
			},
		},
		{
			name:     "extract missing tool names",
			jqFilter: ".missing_tools | map(.tool)",
			validate: func(t *testing.T, output string) {
				var result []string
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON array")
				assert.Equal(t, []string{"github_api", "slack_webhook"}, result, "Should extract tool names")
			},
		},
		{
			name:     "combine multiple fields",
			jqFilter: `{run_id: .overview.run_id, token_usage: .metrics.token_usage, job_count: (.jobs | length)}`,
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Output should be valid JSON")
				runID, ok := result["run_id"].(float64)
				require.True(t, ok, "run_id should be a number")
				assert.Equal(t, int64(123456), int64(runID), "Should have run_id")
				tokenUsage, ok := result["token_usage"].(float64)
				require.True(t, ok, "token_usage should be a number")
				assert.Equal(t, int64(1000), int64(tokenUsage), "Should have token_usage")
				jobCount, ok := result["job_count"].(float64)
				require.True(t, ok, "job_count should be a number")
				assert.Equal(t, int64(2), int64(jobCount), "Should have job_count")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ApplyJqFilter(auditJSON, tt.jqFilter)
			require.NoError(t, err, "Should apply jq filter without error")
			tt.validate(t, output)
		})
	}
}

func TestAuditJqFilter_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		jsonInput string
		jqFilter  string
		wantErr   bool
	}{
		{
			name:      "empty jq filter",
			jsonInput: `{"data": "test"}`,
			jqFilter:  "",
			wantErr:   true,
		},
		{
			name:      "empty JSON object",
			jsonInput: `{}`,
			jqFilter:  ".",
			wantErr:   false,
		},
		{
			name:      "null value",
			jsonInput: `{"value": null}`,
			jqFilter:  ".value",
			wantErr:   false,
		},
		{
			name:      "array with null",
			jsonInput: `[1, null, 3]`,
			jqFilter:  ".",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ApplyJqFilter(tt.jsonInput, tt.jqFilter)
			if tt.wantErr {
				assert.Error(t, err, "Expected error")
			} else {
				assert.NoError(t, err, "Should not error")
			}
		})
	}
}

// TestAuditJqFilter_RealWorldExample tests with a realistic audit output structure
func TestAuditJqFilter_RealWorldExample(t *testing.T) {
	// Realistic audit output based on actual AuditData structure
	auditJSON := `{
  "overview": {
    "run_id": 21784234145,
    "workflow_name": "Test Workflow",
    "status": "completed",
    "conclusion": "success",
    "created_at": "2026-02-07T10:00:00Z",
    "started_at": "2026-02-07T10:01:00Z",
    "updated_at": "2026-02-07T10:15:00Z",
    "duration": "14m0s",
    "event": "workflow_dispatch",
    "branch": "main",
    "url": "https://github.com/github/gh-aw/actions/runs/21784234145",
    "logs_path": ".github/aw/logs/run-21784234145"
  },
  "metrics": {
    "token_usage": 15234,
    "estimated_cost": "$0.23",
    "turns": 8,
    "error_count": 0,
    "warning_count": 3
  },
  "jobs": [
    {
      "name": "agent",
      "status": "completed",
      "conclusion": "success",
      "duration": "12m30s"
    }
  ],
  "downloaded_files": [
    {
      "path": "aw_info.json",
      "size": 1024,
      "size_formatted": "1.0 KB",
      "description": "Workflow configuration",
      "is_directory": false
    }
  ],
  "missing_tools": [],
  "mcp_failures": [],
  "errors": [],
  "warnings": [
    {
      "file": "workflow.md",
      "line": 10,
      "type": "deprecation",
      "message": "Using deprecated syntax"
    }
  ],
  "tool_usage": [
    {
      "name": "bash",
      "call_count": 15,
      "max_input_size": 256,
      "max_output_size": 1024,
      "max_duration": "2.5s"
    }
  ],
  "firewall_analysis": null
}`

	tests := []struct {
		name     string
		jqFilter string
		validate func(t *testing.T, output string)
	}{
		{
			name:     "extract overview section",
			jqFilter: ".overview",
			validate: func(t *testing.T, output string) {
				var overview map[string]any
				err := json.Unmarshal([]byte(output), &overview)
				require.NoError(t, err, "Should parse overview")
				runID, ok := overview["run_id"].(float64)
				require.True(t, ok, "run_id should be a number")
				assert.Equal(t, int64(21784234145), int64(runID), "Should have correct run_id")
				assert.Equal(t, "Test Workflow", overview["workflow_name"], "Should have workflow name")
			},
		},
		{
			name:     "extract key metrics",
			jqFilter: `{token_usage: .metrics.token_usage, cost: .metrics.estimated_cost, duration: .overview.duration}`,
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Should parse result")
				tokenUsage, ok := result["token_usage"].(float64)
				require.True(t, ok, "token_usage should be a number")
				assert.Equal(t, int64(15234), int64(tokenUsage), "Should have token_usage")
				assert.Equal(t, "$0.23", result["cost"], "Should have cost")
				assert.Equal(t, "14m0s", result["duration"], "Should have duration")
			},
		},
		{
			name:     "check for missing tools",
			jqFilter: `.missing_tools | length`,
			validate: func(t *testing.T, output string) {
				output = strings.TrimSpace(output)
				assert.Equal(t, "0", output, "Should have 0 missing tools")
			},
		},
		{
			name:     "summary with job count",
			jqFilter: `{run_id: .overview.run_id, status: .overview.conclusion, job_count: (.jobs | length)}`,
			validate: func(t *testing.T, output string) {
				var result map[string]any
				err := json.Unmarshal([]byte(output), &result)
				require.NoError(t, err, "Should parse summary")
				runID, ok := result["run_id"].(float64)
				require.True(t, ok, "run_id should be a number")
				assert.Equal(t, int64(21784234145), int64(runID), "Should have run_id")
				assert.Equal(t, "success", result["status"], "Should have status")
				jobCount, ok := result["job_count"].(float64)
				require.True(t, ok, "job_count should be a number")
				assert.Equal(t, int64(1), int64(jobCount), "Should have 1 job")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ApplyJqFilter(auditJSON, tt.jqFilter)
			require.NoError(t, err, "Should apply jq filter without error")
			tt.validate(t, output)
		})
	}
}
