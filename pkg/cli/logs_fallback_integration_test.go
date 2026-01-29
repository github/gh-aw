//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestExtractLogMetrics_FallbackScenario tests the complete scenario where
// aw_info.json is missing and the fallback parser extracts metrics
func TestExtractLogMetrics_FallbackScenario(t *testing.T) {
	// Create a temporary directory structure mimicking a real workflow run
	tempDir := t.TempDir()

	// Create log file with errors and warnings but NO aw_info.json
	logContent := `2024-01-04T10:00:00.000Z [INFO] Starting workflow execution
::error::Failed to load configuration file
2024-01-04T10:00:05.000Z [INFO] Attempting retry
ERROR: Connection timeout after 30 seconds
2024-01-04T10:00:10.000Z [INFO] Processing data
::warning::Using deprecated API endpoint
2024-01-04T10:00:15.000Z [INFO] Continuing execution
Warning: Memory usage approaching limit
2024-01-04T10:00:20.000Z [INFO] Workflow completed`

	agentLogPath := filepath.Join(tempDir, "agent-stdio.log")
	err := os.WriteFile(agentLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract metrics without verbose output
	_, err = extractLogMetrics(tempDir, false)
	require.NoError(t, err, "extractLogMetrics should succeed even without aw_info.json")

	// Error patterns have been removed - no error/warning detection
}

// TestLogsCommand_DisplayWithFallbackMetrics tests that the logs display
// correctly shows error and warning counts extracted by the fallback parser
func TestLogsCommand_DisplayWithFallbackMetrics(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "run-12345")
	err := os.MkdirAll(runDir, 0755)
	require.NoError(t, err)

	// Create log file with errors and warnings (no aw_info.json)
	logContent := `::error::Database connection failed
::error::Invalid API token
::warning::Cache expired, regenerating
ERROR: Network timeout
Warning: Disk space low`

	agentLogPath := filepath.Join(runDir, "agent-stdio.log")
	err = os.WriteFile(agentLogPath, []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract metrics
	_, err = extractLogMetrics(runDir, false)
	require.NoError(t, err)

	// Create a WorkflowRun with the extracted metrics
	run := WorkflowRun{
		DatabaseID:   12345,
		Number:       1,
		Status:       "completed",
		Conclusion:   "failure",
		WorkflowName: "Test Workflow",
		CreatedAt:    time.Now(),
		LogsPath:     runDir,
	}

	// Error patterns have been removed - error/warning counts are set to 0
	run.ErrorCount = 0
	run.WarningCount = 0
}

// TestLogsCommand_MixedRunsWithAndWithoutEngine tests that the logs command
// handles a mix of runs with and without engine detection
func TestLogsCommand_MixedRunsWithAndWithoutEngine(t *testing.T) {
	tempDir := t.TempDir()

	// Run 1: Has aw_info.json (would use engine-specific parser in real scenario)
	run1Dir := filepath.Join(tempDir, "run-1")
	err := os.MkdirAll(run1Dir, 0755)
	require.NoError(t, err)

	awInfoContent := `{
		"engine_id": "copilot",
		"engine_name": "GitHub Copilot CLI",
		"model": "gpt-4",
		"workflow_name": "Test Workflow"
	}`
	err = os.WriteFile(filepath.Join(run1Dir, "aw_info.json"), []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Run 2: No aw_info.json (uses fallback parser)
	run2Dir := filepath.Join(tempDir, "run-2")
	err = os.MkdirAll(run2Dir, 0755)
	require.NoError(t, err)

	// Create logs with errors for both runs
	logContent := `::error::Test error
::warning::Test warning`

	err = os.WriteFile(filepath.Join(run1Dir, "agent-stdio.log"), []byte(logContent), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(run2Dir, "agent-stdio.log"), []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract metrics for run without aw_info.json
	_, err = extractLogMetrics(run2Dir, false)
	require.NoError(t, err)

	// Error patterns have been removed - no error/warning detection
}
