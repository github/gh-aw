//go:build !integration && (linux || darwin)

package cli

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPLogsReportACLAllowsPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	require.NoError(t, validateMCPLogsACL(file))
}

func TestMCPLogsReportLifecycle(t *testing.T) {
	base := t.TempDir()
	first := &mcpLogsReportCache{tempDir: base}
	second := &mcpLogsReportCache{tempDir: base}
	t.Cleanup(func() { require.NoError(t, first.close()) })
	t.Cleanup(func() { require.NoError(t, second.close()) })

	const report = `{"runs":[]}`
	old, err := first.store(report)
	require.NoError(t, err)
	got, err := first.store(report)
	require.NoError(t, err)
	require.Equal(t, old, got)
	other, err := second.store(report)
	require.NoError(t, err)
	require.NotEqual(t, filepath.Dir(old), filepath.Dir(other))

	require.NoError(t, os.Remove(old))
	require.NoError(t, os.Remove(filepath.Dir(old)))
	fresh, err := first.store(report)
	require.NoError(t, err)
	require.NotEqual(t, filepath.Dir(old), filepath.Dir(fresh))

	require.NoError(t, first.close())
	_, err = os.Lstat(filepath.Dir(fresh))
	require.ErrorIs(t, err, fs.ErrNotExist)
	data, err := os.ReadFile(other)
	require.NoError(t, err)
	require.Equal(t, report, string(data))
	_, err = first.store(report)
	require.ErrorContains(t, err, "cache is closed")
}

func TestMCPLogsReportDigestValidation(t *testing.T) {
	cache := &mcpLogsReportCache{tempDir: t.TempDir()}
	t.Cleanup(func() { require.NoError(t, cache.close()) })
	const report = `{"runs":[]}`
	path, err := cache.store(report)
	require.NoError(t, err)
	name := filepath.Base(path)
	info, err := cache.root.Lstat(name)
	require.NoError(t, err)
	require.NoError(t, verifyMCPLogsReport(cache.root, name, info, sha256.Sum256([]byte(report))))
	require.ErrorContains(t, verifyMCPLogsReport(cache.root, name, info, [sha256.Size]byte{}), "does not match its SHA-256 digest")
}
