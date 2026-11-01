//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func isolateUpdateCheckCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "cache"))
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	return cache
}

func TestUpdateCheckState_PrivatePersistentCache(t *testing.T) {
	cache := isolateUpdateCheckCache(t)
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	legacyDir := filepath.Join(tmpDir, "gh-aw")
	target := t.TempDir()
	if runtime.GOOS == "windows" {
		require.NoError(t, os.Mkdir(legacyDir, 0o777))
		target = legacyDir
	} else {
		require.NoError(t, os.Symlink(target, legacyDir))
	}
	victim := filepath.Join(target, lastCheckFileName)
	require.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o644))

	for _, fileName := range []string{lastCheckFileName, compileUpdateCheckFileName} {
		path := getUpdateCheckFilePathFor(fileName, updateCheckLog)
		require.Equal(t, filepath.Join(cache, "gh-aw-update-check", fileName), path)
		require.True(t, shouldRunUpdateCheckAtPath(path, 24*time.Hour, "test", updateCheckLog))
		writeUpdateCheckTime(path, constants.FilePermSensitive, "test", updateCheckLog)
		require.FileExists(t, path)
		require.Equal(t, path, getUpdateCheckFilePathFor(fileName, updateCheckLog))
		require.False(t, shouldRunUpdateCheckAtPath(path, 24*time.Hour, "test", updateCheckLog))
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Dir(path))
			require.NoError(t, err)
			require.Equal(t, constants.DirPermOwnerOnly, info.Mode().Perm())
			info, err = os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, constants.FilePermSensitive, info.Mode().Perm())
		}
	}
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "untouched", string(data))
}

func TestUpdateCheckState_RejectsUnsafeCacheDirectory(t *testing.T) {
	for _, precreation := range []string{"symlink", "public directory", "file"} {
		t.Run(precreation, func(t *testing.T) {
			if runtime.GOOS == "windows" && precreation != "file" {
				t.Skip("Requires Unix symlinks or permissions")
			}
			cache := isolateUpdateCheckCache(t)
			require.NoError(t, os.MkdirAll(cache, 0o700))
			dir := filepath.Join(cache, "gh-aw-update-check")
			switch precreation {
			case "symlink":
				require.NoError(t, os.Symlink(t.TempDir(), dir))
			case "public directory":
				require.NoError(t, os.Mkdir(dir, 0o755))
				require.NoError(t, os.Chmod(dir, 0o755))
			case "file":
				require.NoError(t, os.WriteFile(dir, []byte("untouched"), 0o600))
			}
			require.Empty(t, getUpdateCheckFilePathFor(lastCheckFileName, updateCheckLog))
		})
	}
}

func TestUpdateCheckState_AtomicMarkerReplacement(t *testing.T) {
	for _, precreation := range []string{"public file", "symlink"} {
		t.Run(precreation, func(t *testing.T) {
			if runtime.GOOS == "windows" && precreation == "symlink" {
				t.Skip("Requires Unix symlinks")
			}
			isolateUpdateCheckCache(t)
			path := getUpdateCheckFilePathFor(lastCheckFileName, updateCheckLog)
			require.NotEmpty(t, path)
			victim := filepath.Join(t.TempDir(), "victim")
			require.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o600))
			if precreation == "symlink" {
				require.NoError(t, os.Symlink(victim, path))
				require.False(t, shouldRunUpdateCheckAtPath(path, 24*time.Hour, "test", updateCheckLog))
			} else {
				require.NoError(t, os.WriteFile(path, []byte("old timestamp"), 0o644))
			}

			writeUpdateCheckTime(path, constants.FilePermSensitive, "test", updateCheckLog)
			info, err := os.Lstat(path)
			require.NoError(t, err)
			require.True(t, info.Mode().IsRegular())
			if runtime.GOOS != "windows" {
				require.Equal(t, constants.FilePermSensitive, info.Mode().Perm())
			}
			require.False(t, shouldRunUpdateCheckAtPath(path, 24*time.Hour, "test", updateCheckLog))
			data, err := os.ReadFile(victim)
			require.NoError(t, err)
			require.Equal(t, "untouched", string(data))
			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, entries, 1, "atomic writes must clean up their temporary files")
		})
	}
}

func TestUpdateCheckState_WriteFailureCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marker-directory")
	require.NoError(t, os.Mkdir(path, 0o700))
	require.Error(t, writeUpdateCheckTimestamp(path, []byte("timestamp"), constants.FilePermSensitive))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "marker-directory", entries[0].Name())
}
