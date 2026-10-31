//go:build !integration && !js && !wasm

package parser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyRemoteIncludesPreserveTempDirectory(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	const shared = "---\nengine: copilot\ntools:\n  bash: [echo]\nsafe-outputs:\n  create-issue:\n---\nShared instructions\n"
	expanders := map[string]func(*testing.T, string, string, *ImportCache) error{
		"engines": func(t *testing.T, content, baseDir string, cache *ImportCache) error {
			values, err := ExpandIncludesForEngines(content, baseDir, cache)
			if err == nil {
				require.NotEmpty(t, values)
			}
			return err
		},
		"safe-outputs": func(t *testing.T, content, baseDir string, cache *ImportCache) error {
			values, err := ExpandIncludesForSafeOutputs(content, baseDir, cache)
			if err == nil {
				require.NotEmpty(t, values)
			}
			return err
		},
		"tools": func(t *testing.T, content, baseDir string, cache *ImportCache) error {
			value, _, err := ExpandIncludesWithManifest(content, baseDir, true, cache)
			if err == nil {
				require.Contains(t, value, "bash")
			}
			return err
		},
		"markdown": func(t *testing.T, content, baseDir string, cache *ImportCache) error {
			value, _, err := ExpandIncludesWithManifest(content, baseDir, false, cache)
			if err == nil {
				require.Contains(t, value, "Shared instructions")
			}
			return err
		},
	}
	for name, expand := range expanders {
		for _, cached := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/missing", true: "/cached"}[cached], func(t *testing.T) {
				repoDir := t.TempDir()
				tempDir := t.TempDir()
				t.Setenv("TMPDIR", tempDir)
				require.NoError(t, os.WriteFile(filepath.Join(tempDir, "sentinel"), []byte("unchanged"), 0o600))
				before, err := os.ReadDir(tempDir)
				require.NoError(t, err)
				cache := NewImportCache(repoDir)
				if cached {
					_, err := cache.Set("owner", "repo", "shared.md", sha, []byte(shared))
					require.NoError(t, err)
				}
				cache.SetReadOnly(true)
				err = expand(t, "@include owner/repo/shared.md@"+sha+"\n", repoDir, cache)
				if cached {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "is not cached")
					require.NoDirExists(t, cache.GetCacheDir())
				}
				after, err := os.ReadDir(tempDir)
				require.NoError(t, err)
				require.Equal(t, before, after, "include expansion must not create files outside the repository")
				sentinel, err := os.ReadFile(filepath.Join(tempDir, "sentinel"))
				require.NoError(t, err)
				require.Equal(t, "unchanged", string(sentinel))
			})
		}
	}
}

func TestReadOnlyNestedRemoteIncludes(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "cached"}[cached], func(t *testing.T) {
			repoDir := t.TempDir()
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			cache := NewImportCache(repoDir)
			_, err := cache.Set("owner", "repo", "outer.md", sha, []byte("@include owner/repo/inner.md@"+sha+"\n"))
			require.NoError(t, err)
			if cached {
				_, err = cache.Set("owner", "repo", "inner.md", sha, []byte("Nested instructions\n"))
				require.NoError(t, err)
			}
			cache.SetReadOnly(true)
			content, _, err := ExpandIncludesWithManifest("@include owner/repo/outer.md@"+sha+"\n", repoDir, false, cache)
			if cached {
				require.NoError(t, err)
				require.Contains(t, content, "Nested instructions")
			} else {
				require.ErrorContains(t, err, "inner.md")
				require.ErrorContains(t, err, "is not cached")
			}
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestWritableRemoteIncludeCacheRefreshesBranch(t *testing.T) {
	originalFactory := createRESTClientForHostFunc
	originalResolve := resolveRefToSHAViaGitFunc
	originalDownload := downloadFileViaGitFunc
	t.Cleanup(func() {
		createRESTClientForHostFunc = originalFactory
		resolveRefToSHAViaGitFunc = originalResolve
		downloadFileViaGitFunc = originalDownload
	})
	createRESTClientForHostFunc = func(string) (*api.RESTClient, error) {
		return nil, errors.New("unauthorized: authentication required")
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	resolutions := 0
	resolveRefToSHAViaGitFunc = func(context.Context, string, string, string, string) (string, error) {
		resolutions++
		return sha, nil
	}
	downloads := 0
	downloadFileViaGitFunc = func(context.Context, string, string, string, string, string) ([]byte, error) {
		downloads++
		return []byte("instructions at " + sha), nil
	}
	cache := NewImportCache(t.TempDir())
	_, err := cache.Set("owner", "repo", "shared.md", sha, []byte("instructions at "+sha))
	require.NoError(t, err)
	cache.SetReadOnly(true)
	pinned, err := ResolveIncludePath("owner/repo/shared.md@"+sha, "", cache)
	require.NoError(t, err)
	require.Contains(t, pinned, sha)
	_, err = ResolveIncludePath("owner/repo/shared.md@main", "", cache)
	require.ErrorContains(t, err, "offline imports require a cached pinned import")
	require.Equal(t, 0, resolutions, "read-only imports must not resolve refs")
	require.Equal(t, 0, downloads, "read-only imports must not download files")
	_, err = ResolveIncludePath("owner/repo/uncached.md@"+sha, "", cache)
	require.ErrorContains(t, err, "is not cached")
	require.Equal(t, 0, resolutions)
	require.Equal(t, 0, downloads)

	cache.SetReadOnly(false)
	first, err := ResolveIncludePath("owner/repo/shared.md@main", "", cache)
	require.NoError(t, err)
	require.Contains(t, first, sha)
	require.Equal(t, 1, resolutions)
	require.Zero(t, downloads)
	sha = "abcdef0123456789abcdef0123456789abcdef01"
	second, err := ResolveIncludePath("owner/repo/shared.md@main", "", cache)
	require.NoError(t, err)
	require.Contains(t, second, sha)
	require.NotEqual(t, first, second)
	require.Equal(t, 2, resolutions)
	require.Equal(t, 1, downloads)
}
