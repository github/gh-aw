//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectPurgeDataWithPatterns_BadLockPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := collectPurgeDataWithPatterns(dir, nil, false, "[", "*.invalid.yml")
	require.Error(t, err)
	require.ErrorIs(t, err, filepath.ErrBadPattern)
}

func TestCollectPurgeDataWithPatterns_BadSecondPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := collectPurgeDataWithPatterns(dir, nil, false, "*.lock.yml", "[")
	require.Error(t, err)
	require.ErrorIs(t, err, filepath.ErrBadPattern)
}

func TestCollectPurgeDataWithPatterns_ValidPatterns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data, err := collectPurgeDataWithPatterns(dir, nil, false, "*.lock.yml", "*.invalid.yml")
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Empty(t, data.existingLockFiles)
	require.Empty(t, data.existingInvalidFiles)
}

func TestCollectPurgeData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lockFile := filepath.Join(dir, "workflow.lock.yml")
	require.NoError(t, os.WriteFile(lockFile, nil, 0o600))

	data, err := collectPurgeData(dir, []string{filepath.Join(dir, "workflow.md")}, false)

	require.NoError(t, err)
	require.Equal(t, []string{lockFile}, data.existingLockFiles)
	require.Equal(t, []string{filepath.Join(dir, "workflow.lock.yml")}, data.expectedLockFiles)
}

func TestRunPurgeOperations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keepLockFile := filepath.Join(dir, "keep.lock.yml")
	orphanLockFile := filepath.Join(dir, "orphan.lock.yml")
	invalidFile := filepath.Join(dir, "workflow.invalid.yml")
	for _, path := range []string{keepLockFile, orphanLockFile, invalidFile} {
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}

	runPurgeOperations(dir, &purgeTrackingData{
		expectedLockFiles: []string{keepLockFile},
	}, false)

	require.FileExists(t, keepLockFile)
	require.NoFileExists(t, orphanLockFile)
	require.NoFileExists(t, invalidFile)
}

func TestCollectPurgeDataWithPatternsVerbose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow.lock.yml"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow.invalid.yml"), nil, 0o600))

	data, err := collectPurgeDataWithPatterns(dir, nil, true, "*.lock.yml", "*.invalid.yml")

	require.NoError(t, err)
	require.Len(t, data.existingLockFiles, 1)
	require.Len(t, data.existingInvalidFiles, 1)
}
