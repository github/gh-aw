//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      HealthConfig
		wantDaysErr bool
		errContains string
	}{
		{
			name:        "valid 7 days",
			config:      HealthConfig{Days: 7, Threshold: 80.0},
			wantDaysErr: false,
		},
		{
			name:        "valid 30 days",
			config:      HealthConfig{Days: 30, Threshold: 80.0},
			wantDaysErr: false,
		},
		{
			name:        "valid 90 days",
			config:      HealthConfig{Days: 90, Threshold: 80.0},
			wantDaysErr: false,
		},
		{
			name:        "zero days - validation error",
			config:      HealthConfig{Days: 0, Threshold: 80.0},
			wantDaysErr: true,
			errContains: "invalid days value: 0",
		},
		{
			name:        "negative days - validation error",
			config:      HealthConfig{Days: -1, Threshold: 80.0},
			wantDaysErr: true,
			errContains: "invalid days value: -1",
		},
		{
			name:        "days 15 - validation error",
			config:      HealthConfig{Days: 15, Threshold: 80.0},
			wantDaysErr: true,
			errContains: "invalid days value: 15",
		},
		{
			name:        "days 91 - validation error",
			config:      HealthConfig{Days: 91, Threshold: 80.0},
			wantDaysErr: true,
			errContains: "invalid days value: 91",
		},
		{
			name:        "days 365 - validation error",
			config:      HealthConfig{Days: 365, Threshold: 80.0},
			wantDaysErr: true,
			errContains: "invalid days value: 365",
		},
	}

	// Stub out the GitHub API call so valid-days cases don't fall through to
	// real network access (which can take tens of seconds per case, or hang,
	// in sandboxed/offline test environments). Only days validation is under
	// test here; run listing itself is covered by dedicated tests elsewhere.
	original := healthListWorkflowRuns
	t.Cleanup(func() { healthListWorkflowRuns = original })
	originalDiscover := healthDiscoverWorkflows
	t.Cleanup(func() { healthDiscoverWorkflows = originalDiscover })
	healthDiscoverWorkflows = func(*HealthCollection) ([]GitHubWorkflow, error) { return nil, nil }
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, collection *HealthCollection) ([]WorkflowRun, int, error) {
		return nil, 0, nil
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runHealth(context.Background(), tt.config)
			if tt.wantDaysErr {
				require.Error(t, err, "RunHealth should return a validation error for: %s", tt.name)
				require.ErrorContains(t, err, tt.errContains, "Error message should describe the validation failure")
			} else {
				// Valid days values pass days validation; any error comes from GitHub API access
				if err != nil {
					assert.NotContains(t, err.Error(), "invalid days value", "Valid days should not produce a days validation error")
				}
			}
		})
	}
}

func TestRunHealthInvalidDays(t *testing.T) {
	tests := []struct {
		name        string
		days        int
		errContains string
	}{
		{name: "zero", days: 0, errContains: "invalid days value: 0"},
		{name: "negative", days: -1, errContains: "invalid days value: -1"},
		{name: "too large 91", days: 91, errContains: "invalid days value: 91"},
		{name: "too large 365", days: 365, errContains: "invalid days value: 365"},
		{name: "not a valid option 15", days: 15, errContains: "invalid days value: 15"},
		{name: "not a valid option 8", days: 8, errContains: "invalid days value: 8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := HealthConfig{Days: tt.days, Threshold: 80.0}
			err := runHealth(context.Background(), config)
			require.Error(t, err, "RunHealth should return an error for days=%d", tt.days)
			require.ErrorContains(t, err, tt.errContains, "Error should describe the invalid days value")
			require.ErrorContains(t, err, "Must be 7, 30, or 90", "Error should list the valid days options")
		})
	}
}

func TestHealthCommand(t *testing.T) {
	cmd := NewHealthCommand()

	require.NotNil(t, cmd, "Health command should be created")
	assert.Equal(t, "health", cmd.Name(), "Command name should be 'health'")
	assert.True(t, cmd.HasAvailableFlags(), "Command should have flags")
	assert.Contains(t, cmd.Long, "Warnings when success rate drops below threshold", "Health help should consistently use warnings terminology")

	// Check that required flags are registered
	daysFlag := cmd.Flags().Lookup("days")
	require.NotNil(t, daysFlag, "Should have --days flag")
	assert.Equal(t, "7", daysFlag.DefValue, "Default days should be 7")
	assert.Equal(t, "Maximum evidence horizon in days (allowed values: 7, 30, or 90)", daysFlag.Usage)

	thresholdFlag := cmd.Flags().Lookup("threshold")
	require.NotNil(t, thresholdFlag, "Should have --threshold flag")
	assert.Equal(t, "80", thresholdFlag.DefValue, "Default threshold should be 80")

	jsonFlag := cmd.Flags().Lookup("json")
	require.NotNil(t, jsonFlag, "Should have --json flag")

	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag, "Should have --repo flag")
}

func TestDisplayDetailedHealthJSON(t *testing.T) {
	tests := []struct {
		name         string
		runs         []WorkflowRun
		wantZeroRuns bool
	}{
		{
			name:         "nil runs - empty JSON structure",
			runs:         nil,
			wantZeroRuns: true,
		},
		{
			name:         "empty runs slice - empty JSON structure",
			runs:         []WorkflowRun{},
			wantZeroRuns: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := HealthConfig{
				WorkflowName: "test-workflow",
				Days:         7,
				Threshold:    80.0,
				JSONOutput:   true,
			}

			// Redirect stdout to capture JSON output
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			require.NoError(t, err, "os.Pipe should not fail")
			os.Stdout = w

			runErr := displayDetailedHealth(tt.runs, config)

			w.Close()
			os.Stdout = oldStdout

			require.NoError(t, runErr, "displayDetailedHealth should not return an error for %s", tt.name)

			outputBytes, readErr := io.ReadAll(r)
			require.NoError(t, readErr, "Reading captured output should not fail")
			output := string(outputBytes)

			require.NotEmpty(t, output, "JSON output should not be empty")

			var health WorkflowHealth
			require.NoError(t, json.Unmarshal([]byte(output), &health), "Output should be valid JSON")

			assert.Equal(t, "test-workflow", health.WorkflowName, "WorkflowName should match config")
			if tt.wantZeroRuns {
				assert.Equal(t, 0, health.TotalRuns, "TotalRuns should be zero for empty input")
				assert.Equal(t, 0, health.SuccessCount, "SuccessCount should be zero for empty input")
			}
		})
	}
}

func TestRunHealthEmptySummaryJSON(t *testing.T) {
	originalDiscover, originalList := healthDiscoverWorkflows, healthListWorkflowRuns
	t.Cleanup(func() {
		healthDiscoverWorkflows, healthListWorkflowRuns = originalDiscover, originalList
	})
	healthDiscoverWorkflows = func(*HealthCollection) ([]GitHubWorkflow, error) { return nil, nil }
	healthListWorkflowRuns = func(ListWorkflowRunsOptions, int, *HealthCollection) ([]WorkflowRun, int, error) {
		return nil, 0, nil
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Stdout = oldStdout
		r.Close()
		w.Close()
	})
	os.Stdout = w
	runErr := runHealth(context.Background(), HealthConfig{Days: 7, Threshold: 80, JSONOutput: true, RepoOverride: "owner/repo", LogsDir: t.TempDir()})
	require.NoError(t, w.Close())
	os.Stdout = oldStdout
	require.NoError(t, runErr)
	output, err := io.ReadAll(r)
	require.NoError(t, err)
	var summary HealthSummary
	require.NoError(t, json.Unmarshal(output, &summary))
	assert.Zero(t, summary.TotalWorkflows)
}
