//go:build !integration

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeExtensionIfOutdated_DevBuild(t *testing.T) {
	// Save original version and restore after test
	originalVersion := GetVersion()
	defer SetVersionInfo(originalVersion)

	// Set a dev version – upgrade check must be skipped for dev builds because
	// workflow.IsReleasedVersion returns false for non-release builds.
	SetVersionInfo("dev")

	// Verify the function exits before making any API calls.
	// If it did make API calls we'd see a network error in test environments,
	// but the function must return (false, "", nil) immediately.
	upgraded, installPath, err := upgradeExtensionIfOutdated(context.Background(), false, false)
	require.NoError(t, err, "Should not return error for dev builds")
	assert.False(t, upgraded, "Should not report upgrade for dev builds")
	assert.Empty(t, installPath, "installPath should be empty for dev builds")
}

func TestUpgradeExtensionIfOutdated_SilentFailureOnAPIError(t *testing.T) {
	// When the GitHub API is unreachable the function must fail silently and
	// must NOT report an upgrade so that the rest of the upgrade command
	// continues unaffected.

	originalVersion := GetVersion()
	defer SetVersionInfo(originalVersion)

	// Use a release version so the API call is attempted
	SetVersionInfo("v0.1.0")

	upgraded, installPath, err := upgradeExtensionIfOutdated(context.Background(), false, false)
	require.NoError(t, err, "Should fail silently on API errors")
	assert.False(t, upgraded, "Should not report upgrade when API is unreachable")
	assert.Empty(t, installPath, "installPath should be empty when API is unreachable")
}

func TestFirstAttemptWriter_Linux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("Linux-only behavior")
	}
	var buf bytes.Buffer
	dst := &bytes.Buffer{}
	w := firstAttemptWriter(dst, &buf)
	// On Linux the writer should be the buffer, not dst.
	assert.Equal(t, &buf, w, "firstAttemptWriter should return the buffer on Linux")
}

func TestFirstAttemptWriter_Windows(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("Windows-only behavior")
	}
	var buf bytes.Buffer
	dst := &bytes.Buffer{}
	w := firstAttemptWriter(dst, &buf)
	// On Windows the writer should be the buffer (rename+retry workaround).
	assert.Equal(t, &buf, w, "firstAttemptWriter should return the buffer on Windows")
}

func TestFirstAttemptWriter_NonLinuxNonWindows(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("Non-Linux/non-Windows behavior only")
	}
	var buf bytes.Buffer
	dst := &bytes.Buffer{}
	w := firstAttemptWriter(dst, &buf)
	// On other platforms the writer should be dst.
	assert.Equal(t, dst, w, "firstAttemptWriter should return dst on non-Linux/non-Windows")
}

func TestNeedsRenameWorkaround(t *testing.T) {
	t.Parallel()

	result := needsRenameWorkaround()
	expected := runtime.GOOS == "linux" || runtime.GOOS == "windows"
	assert.Equal(t, expected, result, "needsRenameWorkaround should return true only on Linux and Windows")
}

func TestRenamePathForUpgrade(t *testing.T) {
	t.Parallel()

	// Create a temporary file to act as the "executable".
	dir := t.TempDir()
	exe := filepath.Join(dir, "gh-aw")
	require.NoError(t, os.WriteFile(exe, []byte("binary"), 0o755), "Should create temp executable")

	installPath, backupPath, err := renamePathForUpgrade(exe)
	require.NoError(t, err, "renamePathForUpgrade should succeed")
	assert.Equal(t, exe, installPath, "installPath should equal the original exe path")
	assert.NotEmpty(t, backupPath, "backupPath should be non-empty")
	assert.Contains(t, backupPath, ".bak", "backupPath should have .bak suffix")

	// The original path should no longer exist.
	_, statErr := os.Stat(exe)
	assert.True(t, os.IsNotExist(statErr), "Original executable should have been renamed away")

	// The backup should exist at the returned path.
	_, statErr = os.Stat(backupPath)
	require.NoError(t, statErr, "Backup file should exist")
}

func TestRenamePathForUpgrade_NonExistentFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	exe := filepath.Join(dir, "nonexistent")

	_, _, err := renamePathForUpgrade(exe)
	assert.Error(t, err, "renamePathForUpgrade should fail for non-existent file")
}

func TestRestoreExecutableBackup_NoNewBinary(t *testing.T) {
	t.Parallel()

	// Simulate: backup exists, new binary was NOT written (upgrade failed).
	dir := t.TempDir()
	installPath := filepath.Join(dir, "gh-aw")
	backup := installPath + ".99999.bak"

	require.NoError(t, os.WriteFile(backup, []byte("old binary"), 0o755), "Should create backup")

	restoreExecutableBackup(installPath, backup)

	// Backup should be renamed back to installPath.
	_, statErr := os.Stat(installPath)
	require.NoError(t, statErr, "Original executable should be restored")

	// Backup file should be gone.
	_, statErr = os.Stat(backup)
	assert.True(t, os.IsNotExist(statErr), "Backup file should have been renamed away")
}

func TestRestoreExecutableBackup_NewBinaryPresent(t *testing.T) {
	t.Parallel()

	// Simulate: both backup and new binary exist (upgrade partially succeeded).
	dir := t.TempDir()
	installPath := filepath.Join(dir, "gh-aw")
	backup := installPath + ".99999.bak"

	require.NoError(t, os.WriteFile(installPath, []byte("new binary"), 0o755), "Should create new binary")
	require.NoError(t, os.WriteFile(backup, []byte("old binary"), 0o755), "Should create backup")

	restoreExecutableBackup(installPath, backup)

	// New binary should still be present.
	_, statErr := os.Stat(installPath)
	require.NoError(t, statErr, "New binary should remain intact")

	// Backup should be cleaned up.
	_, statErr = os.Stat(backup)
	assert.True(t, os.IsNotExist(statErr), "Backup file should be cleaned up")
}

func TestCleanupExecutableBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	backupPath := filepath.Join(dir, "gh-aw.99999.bak")

	require.NoError(t, os.WriteFile(backupPath, []byte("old binary"), 0o755), "Should create backup")

	cleanupExecutableBackup(backupPath)

	// Backup file should be removed.
	_, statErr := os.Stat(backupPath)
	assert.True(t, os.IsNotExist(statErr), "Backup file should be removed after cleanup")
}

func TestCleanupExecutableBackup_NoBackup(t *testing.T) {
	t.Parallel()

	// Should not fail if backup doesn't exist.
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "gh-aw.99999.bak")

	// No panic or error expected.
	cleanupExecutableBackup(backupPath)
}

func TestIsWindowsLockError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		err      error
		expected bool
	}{
		{
			name:     "access denied in output",
			output:   "gh: Access is denied.\n",
			err:      nil,
			expected: true,
		},
		{
			name:     "sharing violation in output",
			output:   "The process cannot access the file because it is being used by another process.",
			err:      nil,
			expected: true,
		},
		{
			name:     "access denied in error",
			output:   "",
			err:      errors.New("exit status 1: Access is denied"),
			expected: true,
		},
		{
			name:     "gh cli stale bak cleanup failure",
			output:   "failed to remove previous extension update state: unlinkat C:\\extensions\\gh-aw\\gh-aw.exe.1234.bak: Access is denied.",
			err:      errors.New("exit status 1"),
			expected: true,
		},
		{
			name:     "unrelated error",
			output:   "gh: 401 Unauthorized",
			err:      errors.New("exit status 1"),
			expected: false,
		},
		{
			name:     "empty output and nil error",
			output:   "",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWindowsLockError(tt.output, tt.err)
			assert.Equal(t, tt.expected, result, "isWindowsLockError result mismatch")
		})
	}
}

func TestCleanupStaleWindowsBackups(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a stale .bak file (from a previous run, not our own backup)
	staleBak := filepath.Join(dir, "gh-aw.exe.1234.bak")
	require.NoError(t, os.WriteFile(staleBak, []byte("old binary"), 0o755), "Should create stale bak")

	// Create our own backup path (should be skipped)
	ownBak := filepath.Join(dir, "gh-aw.exe.9999.bak")
	require.NoError(t, os.WriteFile(ownBak, []byte("our backup"), 0o755), "Should create own bak")

	// Create a non-.bak file (should be left alone)
	otherFile := filepath.Join(dir, "manifest.yml")
	require.NoError(t, os.WriteFile(otherFile, []byte("manifest"), 0o644), "Should create other file")

	cleanupStaleWindowsBackups(dir, ownBak)

	// Stale .bak should be removed
	_, err := os.Stat(staleBak)
	assert.True(t, os.IsNotExist(err), "Stale .bak should be removed")

	// Our own backup should be left alone
	_, err = os.Stat(ownBak)
	require.NoError(t, err, "Own backup should be preserved")

	// Non-.bak file should be left alone
	_, err = os.Stat(otherFile)
	require.NoError(t, err, "Non-.bak file should be preserved")
}

func TestCleanupStaleWindowsBackups_EmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Should not panic on empty directory
	cleanupStaleWindowsBackups(dir, "")
}

func TestCleanupStaleWindowsBackups_NonexistentDir(t *testing.T) {
	t.Parallel()

	// Should not panic when directory doesn't exist
	cleanupStaleWindowsBackups("/nonexistent/dir", "")
}

func TestExtensionUpgradeArgs(t *testing.T) {
	t.Parallel()

	args := extensionUpgradeArgs()
	assert.Equal(t, []string{"extension", "upgrade", "github/gh-aw", "--force"}, args, "upgrade command must force upgrades for pinned extensions")
}

func TestPrereleaseChannelNotice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		currentVersion     string
		latestStable       string
		includePrereleases bool
		want               []string
	}{
		{
			name:               "stable channel prerelease hint",
			currentVersion:     "v0.75.3-beta.1",
			latestStable:       "v0.74.8",
			includePrereleases: false,
			want: []string{
				"Current gh-aw version v0.75.3-beta.1 (pre-release) is newer than the latest stable release v0.74.8; upgrading without --pre-releases would downgrade to that release.",
				"Run `gh aw upgrade --pre-releases` to get the latest pre-release.",
			},
		},
		{
			name:               "no hint when prereleases included",
			currentVersion:     "v0.75.3-beta.1",
			latestStable:       "v0.74.8",
			includePrereleases: true,
			want:               nil,
		},
		{
			name:               "no hint for stable version",
			currentVersion:     "v0.74.8",
			latestStable:       "v0.74.8",
			includePrereleases: false,
			want:               nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prereleaseChannelNotice(tt.currentVersion, tt.latestStable, tt.includePrereleases))
		})
	}
}

func TestExtensionHelpCommands(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"gh extension upgrade github/gh-aw --force",
		extensionUpgradeHelpCommand("v0.74.8"),
		"stable releases should use gh extension upgrade")
	assert.Equal(t,
		"gh extension install github/gh-aw --force --pin v0.75.3-beta.1",
		extensionUpgradeHelpCommand("v0.75.3-beta.1"),
		"pre-releases should use an exact pin")
	assert.Equal(t,
		"gh extension install github/gh-aw",
		extensionInstallHelpCommand("v0.74.8"),
		"stable reinstalls should use plain install")
	assert.Equal(t,
		"gh extension install github/gh-aw --force --pin v0.75.3-beta.1",
		extensionInstallHelpCommand("v0.75.3-beta.1"),
		"pre-release reinstalls should preserve the exact tag")
}

func TestRenderReleaseVersion(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "v0.74.8", renderReleaseVersion("v0.74.8"))
	assert.Equal(t, "v0.75.3-beta.1 (pre-release)", renderReleaseVersion("v0.75.3-beta.1"))
}

func TestParseInstalledVersionOutput(t *testing.T) {
	t.Run("parses version without v prefix", func(t *testing.T) {
		got, err := parseInstalledVersionOutput("gh-aw version 0.77.5 (2026-06-01)")
		require.NoError(t, err)
		assert.Equal(t, "v0.77.5", got)
	})

	t.Run("parses stable version", func(t *testing.T) {
		got, err := parseInstalledVersionOutput("gh-aw version v0.77.5 (2026-06-01)")
		require.NoError(t, err)
		assert.Equal(t, "v0.77.5", got)
	})

	t.Run("parses prerelease version", func(t *testing.T) {
		got, err := parseInstalledVersionOutput("gh-aw version v0.77.6-beta.1")
		require.NoError(t, err)
		assert.Equal(t, "v0.77.6-beta.1", got)
	})

	t.Run("returns error when no version present", func(t *testing.T) {
		_, err := parseInstalledVersionOutput("gh-aw version unknown")
		require.Error(t, err)
		require.ErrorContains(t, err, "could not parse installed gh-aw version")
	})

	t.Run("uses first version match when multiple exist", func(t *testing.T) {
		got, err := parseInstalledVersionOutput("gh-aw v0.77.5 (built from v0.77.6)")
		require.NoError(t, err)
		assert.Equal(t, "v0.77.5", got)
	})

	t.Run("returns error for empty output", func(t *testing.T) {
		_, err := parseInstalledVersionOutput("")
		require.Error(t, err)
		require.ErrorContains(t, err, "could not parse installed gh-aw version")
	})
}

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "0.77.5", normalizeVersion("v0.77.5"))
	assert.Equal(t, "0.77.5", normalizeVersion("0.77.5"))
	assert.Equal(t, "1.0.0-beta.1", normalizeVersion("v1.0.0-beta.1"))
	assert.Empty(t, normalizeVersion(""))
}

// TestGhCmdForExtension verifies that ghCmdForExtension always pins
// GH_HOST=github.com so that GHE-authenticated environments do not
// redirect extension upgrade/install/remove commands to the wrong host.
func TestGhCmdForExtension(t *testing.T) {
	t.Run("sets GH_HOST to github.com", func(t *testing.T) {
		cmd := ghCmdForExtension(context.Background(), "extension", "list")
		ghHost := ""
		for _, e := range cmd.Env {
			if v, ok := strings.CutPrefix(e, "GH_HOST="); ok {
				ghHost = v
			}

		}
		assert.Equal(t, "github.com", ghHost, "GH_HOST must be github.com")
	})

	t.Run("overrides existing GH_HOST set to a GHE instance", func(t *testing.T) {
		t.Setenv("GH_HOST", "ghe.example.com")

		cmd := ghCmdForExtension(context.Background(), "extension", "upgrade", extensionRepo, "--force")
		ghHostValues := []string{}
		for _, e := range cmd.Env {
			if v, ok := strings.CutPrefix(e, "GH_HOST="); ok {
				ghHostValues = append(ghHostValues, v)
			}
		}
		require.Len(t, ghHostValues, 1, "exactly one GH_HOST entry must be present")
		assert.Equal(t, "github.com", ghHostValues[0], "GH_HOST must be overridden to github.com")
	})

	t.Run("uses gh as executable", func(t *testing.T) {
		cmd := ghCmdForExtension(context.Background(), "extension", "list")
		assert.Equal(t, "gh", filepath.Base(cmd.Path), "executable must be gh")
	})
}

func TestUpgradeExtensionCanceledBeforeLookup(t *testing.T) {
	before := getLatestExtensionReleaseFn
	t.Cleanup(func() { getLatestExtensionReleaseFn = before })
	getLatestExtensionReleaseFn = func(context.Context, bool) (string, error) {
		t.Fatal("canceled upgrade must not query release metadata")
		return "", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	upgraded, _, err := upgradeExtensionIfOutdated(ctx, false, true)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, upgraded)
	require.ErrorIs(t, ghCmdForExtension(ctx, "extension", "install", extensionRepo).Run(), context.Canceled)
}

func TestUpgradeExtensionCancellationStopsRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell executable fixture")
	}
	beforeVersion, beforeLookup, beforeRelease := GetVersion(), getLatestExtensionReleaseFn, workflow.IsRelease()
	t.Cleanup(func() {
		SetVersionInfo(beforeVersion)
		getLatestExtensionReleaseFn = beforeLookup
		workflow.SetIsRelease(beforeRelease)
	})
	SetVersionInfo("v0.1.0")
	workflow.SetIsRelease(true)
	getLatestExtensionReleaseFn = func(context.Context, bool) (string, error) { return "v0.2.0", nil }
	dir := t.TempDir()
	ready, calls := filepath.Join(dir, "ready"), filepath.Join(dir, "calls")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_AW_EXTENSION_CALLS\"\nprintf ready > \"$GH_AW_EXTENSION_READY\"\nexec sleep 30\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_AW_EXTENSION_READY", ready)
	t.Setenv("GH_AW_EXTENSION_CALLS", calls)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, _, err := upgradeExtensionIfOutdated(ctx, false, false)
		completed <- err
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-completed:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("extension installation did not stop after cancellation")
	}
	invocations, err := os.ReadFile(calls)
	require.NoError(t, err)
	assert.Equal(t, "extension upgrade github/gh-aw --force\n", string(invocations), "cancellation must prevent remove/reinstall retries")
}
