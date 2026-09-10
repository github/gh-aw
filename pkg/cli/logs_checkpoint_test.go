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
