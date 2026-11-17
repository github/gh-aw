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
	markdownFile := filepath.Join(dir, "workflow.md")
	data, err := collectPurgeData(dir, []string{markdownFile}, false)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, "workflow.lock.yml")}, data.expectedLockFiles)
}

func TestRunPurgeOperations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lockFile := filepath.Join(dir, "orphan.lock.yml")
	invalidFile := filepath.Join(dir, "workflow.invalid.yml")
	require.NoError(t, os.WriteFile(lockFile, []byte("lock"), 0o600))
	require.NoError(t, os.WriteFile(invalidFile, []byte("invalid"), 0o600))

	runPurgeOperations(dir, &purgeTrackingData{}, false)

	_, err := os.Stat(lockFile)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(invalidFile)
	require.ErrorIs(t, err, os.ErrNotExist)
}
