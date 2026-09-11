//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsCommandAuditIntegration(t *testing.T) {
	outputDir := t.TempDir()
	cachePath := filepath.Join(t.TempDir(), "logs.jsonl")
	run := ProcessedRun{Run: WorkflowRun{
		DatabaseID:   42,
		Status:       "completed",
		Conclusion:   "success",
		WorkflowName: "test",
		Turns:        2,
		LogsPath:     filepath.Join(outputDir, "42"),
	}}
	require.NoError(t, os.MkdirAll(run.Run.LogsPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(run.Run.LogsPath, "aw_info.json"), []byte(`{"engine_id":"copilot"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(run.Run.LogsPath, safeOutputItemsManifestFilename), []byte(`{"type":"create_issue","url":"https://github.com/github/gh-aw/issues/1","timestamp":"2026-09-11T04:00:00Z"}`+"\n"), 0o600))

	_, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{
		audit:             true,
		outputDir:         outputDir,
		cachedJSONLWriter: newCachedLogsJSONLWriter(cachePath),
	})
	require.NoError(t, err)

	_, ok := loadCachedAuditData(run.Run.LogsPath, run.Run, auditCacheSourceLogs)
	assert.True(t, ok)
	_, err = os.Stat(filepath.Join(outputDir, drain3WeightsFilename))
	assert.NoError(t, err)

	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	var record cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &record))
	require.NotNil(t, record.Run)
	require.NotNil(t, record.Run.Audit)
	require.NotNil(t, record.Run.AwInfo)
	assert.Equal(t, "copilot", record.Run.AwInfo.EngineID)
	require.Len(t, record.Run.SafeOutputs, 1)
	assert.Equal(t, "create_issue", record.Run.SafeOutputs[0].Type)
}
