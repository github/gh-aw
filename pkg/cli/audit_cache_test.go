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

func TestWriteAuditDataCreatesReusableAuditFile(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success"}
	auditData := AuditData{CacheSource: auditCacheSourceFull, Overview: buildAuditOverview(run, nil)}

	require.NoError(t, writeAuditData(runDir, auditData))

	path := filepath.Join(runDir, auditFileName)
	assert.Equal(t, path, existingAuditPath(runDir))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	cached, ok := loadCachedAuditData(runDir, run, auditCacheSourceFull)
	require.True(t, ok)
	assert.Equal(t, auditData.Overview, cached.Overview)
}

func TestLoadCachedAuditDataRequiresMatchingRunState(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "in_progress", Conclusion: "", UpdatedAt: time.Now()}
	require.NoError(t, writeAuditData(runDir, AuditData{CacheSource: auditCacheSourceFull, Overview: buildAuditOverview(run, nil)}))

	tests := []struct {
		name string
		run  WorkflowRun
		ok   bool
	}{
		{name: "same state", run: run, ok: true},
		{name: "status changed", run: WorkflowRun{DatabaseID: 42, Status: "completed"}, ok: false},
		{name: "conclusion changed", run: WorkflowRun{DatabaseID: 42, Status: "in_progress", Conclusion: "success"}, ok: false},
		{name: "run changed", run: WorkflowRun{DatabaseID: 43, Status: "in_progress"}, ok: false},
		{name: "updated time changed", run: WorkflowRun{DatabaseID: 42, Status: "in_progress", UpdatedAt: run.UpdatedAt.Add(time.Second)}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := loadCachedAuditData(runDir, tt.run, auditCacheSourceFull)
			assert.Equal(t, tt.ok, ok)
		})
	}
	_, ok := loadCachedAuditData(runDir, run, auditCacheSourceLogs)
	assert.False(t, ok)
}

func TestWriteLogsAuditFilesUsesCacheUntilRunStateChanges(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{
		DatabaseID:   42,
		Status:       "completed",
		Conclusion:   "success",
		WorkflowName: "test",
		LogsPath:     runDir,
	}
	cached := AuditData{
		CacheSource: auditCacheSourceLogs,
		Overview:    buildAuditOverview(run, nil),
		KeyFindings: []AuditFinding{{Title: "cache marker"}},
	}
	require.NoError(t, writeAuditData(runDir, cached))

	writeLogsAuditFiles([]ProcessedRun{{Run: run}}, false)
	data, err := os.ReadFile(auditPath(runDir))
	require.NoError(t, err)
	var unchanged AuditData
	require.NoError(t, json.Unmarshal(data, &unchanged))
	require.Len(t, unchanged.KeyFindings, 1)
	assert.Equal(t, "cache marker", unchanged.KeyFindings[0].Title)

	run.Conclusion = "failure"
	writeLogsAuditFiles([]ProcessedRun{{Run: run}}, false)
	data, err = os.ReadFile(auditPath(runDir))
	require.NoError(t, err)
	var refreshed AuditData
	require.NoError(t, json.Unmarshal(data, &refreshed))
	assert.Equal(t, "failure", refreshed.Overview.Conclusion)
	for _, finding := range refreshed.KeyFindings {
		assert.NotEqual(t, "cache marker", finding.Title)
	}
}

func TestWriteLogsAuditFilesUpdatesCachedComparison(t *testing.T) {
	root := t.TempDir()
	current := ProcessedRun{Run: WorkflowRun{
		DatabaseID:   42,
		Status:       "completed",
		Conclusion:   "failure",
		WorkflowName: "test",
		CreatedAt:    time.Now(),
		LogsPath:     filepath.Join(root, "current"),
	}}
	baseline := ProcessedRun{Run: WorkflowRun{
		DatabaseID:   41,
		Status:       "completed",
		Conclusion:   "success",
		WorkflowName: "test",
		CreatedAt:    current.Run.CreatedAt.Add(-time.Minute),
		LogsPath:     filepath.Join(root, "baseline"),
	}}

	writeLogsAuditFiles([]ProcessedRun{current}, false)
	writeLogsAuditFiles([]ProcessedRun{baseline, current}, false)

	cached, ok := loadCachedAuditData(current.Run.LogsPath, current.Run, auditCacheSourceLogs)
	require.True(t, ok)
	require.NotNil(t, cached.Comparison)
	assert.True(t, cached.Comparison.BaselineFound)
	require.NotNil(t, cached.Comparison.Baseline)
	assert.Equal(t, baseline.Run.DatabaseID, cached.Comparison.Baseline.RunID)
}

func TestWriteLogsAuditFilesContinuesAfterWriteFailure(t *testing.T) {
	root := t.TempDir()
	badPath := filepath.Join(root, "not-a-directory")
	require.NoError(t, os.WriteFile(badPath, nil, 0o600))
	validPath := filepath.Join(root, "valid")
	runs := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 41, Status: "completed", Conclusion: "success", LogsPath: badPath}},
		{Run: WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: validPath}},
	}

	writeLogsAuditFiles(runs, false)
	_, ok := loadCachedAuditData(validPath, runs[1].Run, auditCacheSourceLogs)
	assert.True(t, ok)
}

func TestBuildLogsDataLinksExistingAudit(t *testing.T) {
	runDir := t.TempDir()
	require.NoError(t, writeAuditData(runDir, AuditData{}))
	run := ProcessedRun{Run: WorkflowRun{DatabaseID: 42, LogsPath: runDir}}

	data := buildLogsData([]ProcessedRun{run}, t.TempDir(), nil)

	require.Len(t, data.Runs, 1)
	assert.Equal(t, auditPath(runDir), data.Runs[0].AuditPath)
}
