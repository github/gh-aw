//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsCommandAuditIntegration(t *testing.T) {
	outputDir := t.TempDir()
	run := ProcessedRun{Run: WorkflowRun{
		DatabaseID:   42,
		Status:       "completed",
		Conclusion:   "success",
		WorkflowName: "test",
		Turns:        2,
		LogsPath:     filepath.Join(outputDir, "42"),
	}}

	_, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{
		audit:     true,
		outputDir: outputDir,
	})
	require.NoError(t, err)

	_, ok := loadCachedAuditData(run.Run.LogsPath, run.Run, auditCacheSourceLogs)
	assert.True(t, ok)
	_, err = os.Stat(filepath.Join(outputDir, drain3WeightsFilename))
	assert.NoError(t, err)
}
