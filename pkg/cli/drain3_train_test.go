//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrainDrain3Weights_NoRuns(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	err := TrainDrain3Weights(nil, tmpDir, false)
	require.NoError(t, err, "should not error when no runs provided")

	// No weights file should be written.
	_, statErr := os.Stat(filepath.Join(tmpDir, drain3WeightsFilename))
	assert.True(t, os.IsNotExist(statErr), "weights file should not be created for empty run list")
}

func TestTrainDrain3Weights_WithRuns(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	runs := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID: 1,
				Conclusion: "success",
				Turns:      5,
				TokenUsage: 1000,
			},
			MCPFailures:  []MCPFailureReport{{ServerName: "github", Status: "ok"}},
			MissingTools: []MissingToolReport{{Tool: "terraform", Reason: "missing"}},
		},
		{
			Run: WorkflowRun{
				DatabaseID: 2,
				Conclusion: "failure",
				Turns:      8,
				ErrorCount: 2,
			},
			MCPFailures: []MCPFailureReport{{ServerName: "search", Status: "timeout"}},
		},
	}

	err := TrainDrain3Weights(runs, tmpDir, true)
	require.NoError(t, err, "training should succeed with valid runs")

	// Weights file should be written.
	weightsPath := filepath.Join(tmpDir, drain3WeightsFilename)
	data, err := os.ReadFile(weightsPath)
	require.NoError(t, err, "weights file should exist after training")
	require.NotEmpty(t, data, "weights file should not be empty")

	// File should be valid JSON with stage keys.
	var weights map[string]any
	err = json.Unmarshal(data, &weights)
	require.NoError(t, err, "weights file should be valid JSON")
	assert.NotEmpty(t, weights, "weights JSON should have stage keys")

	// Should contain at least one of the expected stage keys.
	expectedStages := []string{"plan", "tool_call", "error", "finish"}
	for _, stage := range expectedStages {
		assert.Contains(t, weights, stage, "weights should contain stage %q", stage)
	}
}

func TestTrainDrain3Weights_JSONStructure(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	runs := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID: 1,
				Conclusion: "success",
				Turns:      3,
				TokenUsage: 500,
			},
		},
	}

	err := TrainDrain3Weights(runs, tmpDir, false)
	require.NoError(t, err, "training should not error")

	weightsPath := filepath.Join(tmpDir, drain3WeightsFilename)
	data, err := os.ReadFile(weightsPath)
	require.NoError(t, err, "weights file should exist")

	// Should be a map of stage → snapshot objects.
	var weights map[string]json.RawMessage
	err = json.Unmarshal(data, &weights)
	require.NoError(t, err, "should unmarshal to map of raw JSON")

	// Each value should itself be a valid JSON object.
	for stage, raw := range weights {
		var snap map[string]any
		require.NoError(t, json.Unmarshal(raw, &snap), "stage %q snapshot should be valid JSON", stage)
		assert.Contains(t, snap, "clusters", "stage %q snapshot should have clusters key", stage)
		assert.Contains(t, snap, "config", "stage %q snapshot should have config key", stage)
	}
}

func TestTrainDrain3Weights_LoadsExistingWeights(t *testing.T) {
	seedDir := t.TempDir()
	runs := []ProcessedRun{{
		Run: WorkflowRun{
			DatabaseID: 1,
			Conclusion: "success",
			Turns:      3,
		},
	}}
	require.NoError(t, TrainDrain3Weights(runs, seedDir, false))
	weightsPath := filepath.Join(seedDir, drain3WeightsFilename)

	outputDir := t.TempDir()
	_, stderr := captureOutput(t, func() error {
		return trainDrain3Weights(runs, outputDir, weightsPath, false)
	})

	assert.Contains(t, stderr, "Loaded log pattern weights from: "+weightsPath)
	assert.FileExists(t, filepath.Join(outputDir, drain3WeightsFilename))
}

func TestTrainDrain3Weights_RejectsInvalidExistingWeights(t *testing.T) {
	weightsPath := filepath.Join(t.TempDir(), "weights.json")
	require.NoError(t, os.WriteFile(weightsPath, []byte(`not json`), 0o600))

	err := trainDrain3Weights([]ProcessedRun{{Run: WorkflowRun{Turns: 1}}}, t.TempDir(), weightsPath, false)

	require.ErrorContains(t, err, "load weights file")
}

func TestLogsCommandHasTrainFlag(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	flag := cmd.Flags().Lookup("train")
	require.NotNil(t, flag, "logs command should have --train flag")
	assert.Equal(t, "bool", flag.Value.Type(), "--train flag should be a bool")
	assert.Equal(t, "false", flag.DefValue, "--train flag should default to false")
}
