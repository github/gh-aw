//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsCheckpointWriterFlushesOnStop(t *testing.T) {
	outputDir := t.TempDir()
	writer := startLogsCheckpointWriter(LogsDownloadOptions{
		OutputDir:      outputDir,
		SummaryFile:    "summary.json",
		SuppressRender: true,
	}, time.Hour)
	require.NotNil(t, writer)

	writer.Update([]ProcessedRun{{Run: WorkflowRun{DatabaseID: 123}}})
	writer.Stop()
	writer.Stop()

	data, err := os.ReadFile(filepath.Join(outputDir, "summary.json"))
	require.NoError(t, err)
	var summary LogsData
	require.NoError(t, json.Unmarshal(data, &summary))
	require.Len(t, summary.Runs, 1)
	assert.Equal(t, int64(123), summary.Runs[0].RunID)
}

func TestLogsCheckpointWriterPersistsAtInterval(t *testing.T) {
	outputDir := t.TempDir()
	writer := startLogsCheckpointWriter(LogsDownloadOptions{
		OutputDir:   outputDir,
		SummaryFile: "summary.json",
	}, 10*time.Millisecond)
	require.NotNil(t, writer)
	defer writer.Stop()

	writer.Update([]ProcessedRun{{Run: WorkflowRun{DatabaseID: 456}}})
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(outputDir, "summary.json"))
		return err == nil
	}, time.Second, 10*time.Millisecond)
}

func TestLogsCheckpointWriterPersistsCachedJSONAtInterval(t *testing.T) {
	outputDir := t.TempDir()
	cachedJSON := filepath.Join(t.TempDir(), "cached.json")
	writer := startLogsCheckpointWriter(LogsDownloadOptions{
		OutputDir:  outputDir,
		CachedLogs: cachedJSON,
	}, 10*time.Millisecond)
	require.NotNil(t, writer)
	defer writer.Stop()

	writer.Update([]ProcessedRun{{Run: WorkflowRun{DatabaseID: 789}}})
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(cachedJSON)
		if err != nil {
			return false
		}
		var cached LogsData
		return json.Unmarshal(data, &cached) == nil &&
			len(cached.Runs) == 1 &&
			cached.Runs[0].RunID == 789
	}, time.Second, 10*time.Millisecond)
}

func TestLogsCheckpointWriterCombinesTargets(t *testing.T) {
	cachedJSON := filepath.Join(t.TempDir(), "cached.json")
	writer := startLogsCheckpointWriter(LogsDownloadOptions{CachedLogs: cachedJSON}, time.Hour)
	require.NotNil(t, writer)

	writer.UpdateTarget("first", []ProcessedRun{{Run: WorkflowRun{DatabaseID: 1}}})
	writer.UpdateTarget("second", []ProcessedRun{{Run: WorkflowRun{DatabaseID: 2}}})
	writer.Stop()

	data, err := os.ReadFile(cachedJSON)
	require.NoError(t, err)
	var cached LogsData
	require.NoError(t, json.Unmarshal(data, &cached))
	require.Len(t, cached.Runs, 2)
	runIDs := []int64{cached.Runs[0].RunID, cached.Runs[1].RunID}
	assert.ElementsMatch(t, []int64{1, 2}, runIDs)
}
