//go:build !integration

package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workflowLogsArchiveFixture(t *testing.T, invalidEntry bool) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	file, err := writer.Create("safe_outputs/4_Reconcile work queue claim.txt")
	require.NoError(t, err)
	_, err = file.Write([]byte("2026-10-02T12:00:00Z Work queue: worker completion verified\n"))
	require.NoError(t, err)
	if invalidEntry {
		file, err = writer.Create("../invalid.txt")
		require.NoError(t, err)
		_, err = file.Write([]byte("invalid"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return data.Bytes()
}

func TestWorkflowLogsAtomicCompletion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.Error(t, storeWorkflowRunLogsArchive(workflowLogsArchiveFixture(t, true), dir, false))
	assert.False(t, workflowRunLogsComplete(dir))
	assert.NoDirExists(t, filepath.Join(dir, "workflow-logs"))
	require.NoError(t, storeWorkflowRunLogsArchive(workflowLogsArchiveFixture(t, false), dir, false))
	assert.True(t, workflowRunLogsComplete(dir))
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.Len(t, report.Operations, 1)
	require.Error(t, storeWorkflowRunLogsArchive(workflowLogsArchiveFixture(t, true), dir, false))
	assert.True(t, workflowRunLogsComplete(dir), "failed replacement must preserve a completed download")
	report, err = extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.Len(t, report.Operations, 1)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "staging directories must be removed")
}

func TestWorkflowLogsIncompleteLegacyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	step := filepath.Join(dir, "workflow-logs", "safe_outputs", "4_Reconcile work queue claim.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(step), 0o755))
	require.NoError(t, os.WriteFile(step, []byte("2026-10-02T12:00:00Z Work queue: partial evidence\n"), 0o600))
	assert.False(t, workflowRunLogsComplete(dir))
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	assert.Nil(t, report, "partial archive data must not be reported as completed evidence")
	require.NoError(t, storeWorkflowRunLogsArchive(workflowLogsArchiveFixture(t, false), dir, false))
	report, err = extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.Len(t, report.Operations, 1)
	assert.NotContains(t, report.Operations[0].Message, "partial evidence")
}

func TestDownloadAllCachedArtifactsBackfillsWorkflowLogs(t *testing.T) {
	dir, bin := t.TempDir(), t.TempDir()
	require.NoError(t, markArtifactDownloaded(dir, "all"))
	archivePath := filepath.Join(bin, "logs.zip")
	require.NoError(t, os.WriteFile(archivePath, workflowLogsArchiveFixture(t, false), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(`#!/bin/sh
if [ "$1" = "api" ]; then
  cat "$WORK_QUEUE_TEST_ARCHIVE"
  exit 0
fi
exit 1
`), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WORK_QUEUE_TEST_ARCHIVE", archivePath)
	require.NoError(t, downloadRunArtifacts(context.Background(), downloadArtifactsOptions{runID: 42, outputDir: dir}))
	assert.True(t, workflowRunLogsComplete(dir))
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.Len(t, report.Operations, 1)
}
