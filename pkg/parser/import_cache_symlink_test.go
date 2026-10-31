//go:build !integration && !js && !wasm

package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func requireCacheSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) {
			t.Skipf("symlinks unavailable: %v", err)
		}
		require.NoError(t, err)
	}
}

func TestImportCacheRejectsSymlinkEntries(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing target", true: "dangling target"}[dangling], func(t *testing.T) {
			cache := NewImportCache(t.TempDir())
			target := filepath.Join(t.TempDir(), "outside.md")
			if !dangling {
				require.NoError(t, os.WriteFile(target, []byte("outside secret"), 0o600))
			}
			path := filepath.Join(cache.GetCacheDir(), "owner", "repo", "sha", "shared.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			requireCacheSymlink(t, target, path)

			_, found := cache.Get("owner", "repo", "shared.md", "sha")
			require.False(t, found)
			content, err := cache.ReadFile(path)
			require.Error(t, err)
			require.Empty(t, content)
			content, err = ReadFile(path)
			require.Error(t, err)
			require.Empty(t, content)
			_, err = cache.Set("owner", "repo", "shared.md", "sha", []byte("overwrite"))
			require.Error(t, err)
			if dangling {
				require.NoFileExists(t, target)
			} else {
				content, err = os.ReadFile(target)
				require.NoError(t, err)
				require.Equal(t, "outside secret", string(content))
			}
		})
	}
}

func TestImportCacheRejectsSymlinkDirectories(t *testing.T) {
	components := []string{".github", "aw", "imports", "owner", "repo", "sha"}
	for index, component := range components {
		for _, dangling := range []bool{false, true} {
			t.Run(component+map[bool]string{false: "/existing", true: "/dangling"}[dangling], func(t *testing.T) {
				base := t.TempDir()
				cache := NewImportCache(base)
				outside := t.TempDir()
				target := outside
				if dangling {
					target = filepath.Join(outside, "missing")
				} else {
					targetFile := filepath.Join(append([]string{outside}, components[index+1:]...)...)
					require.NoError(t, os.MkdirAll(targetFile, 0o700))
					require.NoError(t, os.WriteFile(filepath.Join(targetFile, "shared.md"), []byte("outside secret"), 0o600))
				}
				link := filepath.Join(append([]string{base}, components[:index+1]...)...)
				require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
				requireCacheSymlink(t, target, link)
				path := filepath.Join(cache.GetCacheDir(), "owner", "repo", "sha", "shared.md")

				_, found := cache.Get("owner", "repo", "shared.md", "sha")
				require.False(t, found)
				content, err := ReadFile(path)
				require.Error(t, err)
				require.Empty(t, content)
				_, err = cache.Set("owner", "repo", "shared.md", "sha", []byte("overwrite"))
				require.Error(t, err)
				if dangling {
					require.NoDirExists(t, target)
				} else {
					targetFile := filepath.Join(append([]string{outside}, components[index+1:]...)...)
					content, err = os.ReadFile(filepath.Join(targetFile, "shared.md"))
					require.NoError(t, err)
					require.Equal(t, "outside secret", string(content))
				}
				require.NoFileExists(t, filepath.Join(outside, ".gitattributes"))
			})
		}
	}
}

func TestImportCacheGitAttributesSymlink(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "dangling"}[dangling], func(t *testing.T) {
			cache := NewImportCache(t.TempDir())
			require.NoError(t, os.MkdirAll(cache.GetCacheDir(), 0o700))
			target := filepath.Join(t.TempDir(), "attributes")
			if !dangling {
				require.NoError(t, os.WriteFile(target, []byte("custom attributes"), 0o600))
			}
			requireCacheSymlink(t, target, filepath.Join(cache.GetCacheDir(), ".gitattributes"))
			require.Error(t, cache.ensureGitAttributes())
			// Attribute generation remains non-fatal, but cannot follow its link.
			_, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("valid content"))
			require.NoError(t, err)
			if dangling {
				require.NoFileExists(t, target)
			} else {
				content, err := os.ReadFile(target)
				require.NoError(t, err)
				require.Equal(t, "custom attributes", string(content))
			}
		})
	}
}

func TestImportCacheReadAfterLookupRejectsSymlink(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	path, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("cached"))
	require.NoError(t, err)
	resolved, found := cache.Get("owner", "repo", "shared.md", "sha")
	require.True(t, found)
	target := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(target, []byte("outside secret"), 0o600))
	require.NoError(t, os.Remove(path))
	requireCacheSymlink(t, target, path)

	content, err := ReadFile(resolved)
	require.Error(t, err)
	require.Empty(t, content)
	_, err = processIncludedFileWithVisited(resolved, "", false, map[string]struct{}{}, cache)
	require.Error(t, err)
	state := &importBFSState{acc: &importAccumulator{}}
	err = handleStandardImportItem(importQueueItem{fullPath: resolved}, "", cache, "", "", state)
	require.ErrorContains(t, err, "failed to read imported file")
}

func TestImportCachePinnedDirectoryWrite(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	relativeDir := filepath.Join(ImportCacheDir, "owner", "repo", "sha")
	dir, err := cache.openCacheDirectory(relativeDir, true)
	require.NoError(t, err)
	defer dir.Close()
	path := filepath.Join(cache.baseDir, relativeDir)
	moved := path + "-moved"
	require.NoError(t, os.Rename(path, moved))
	outside := t.TempDir()
	target := filepath.Join(outside, "shared.md")
	require.NoError(t, os.WriteFile(target, []byte("outside"), 0o600))
	requireCacheSymlink(t, outside, path)

	require.NoError(t, writeCacheFileAtomic(dir, "shared.md", []byte("cached"), constants.FilePermSensitive, true))
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "outside", string(content))
	content, err = os.ReadFile(filepath.Join(moved, "shared.md"))
	require.NoError(t, err)
	require.Equal(t, "cached", string(content))
	content, err = cache.ReadFile(filepath.Join(path, "shared.md"))
	require.Error(t, err)
	require.Empty(t, content)
}

func TestImportCacheAtomicWriteDoesNotTruncateHardlink(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	path, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("original"))
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "hardlink.md")
	require.NoError(t, os.Link(path, target))
	_, err = cache.Set("owner", "repo", "shared.md", "sha", []byte("replacement"))
	require.NoError(t, err)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "original", string(content))
	content, err = cache.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(content))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "temporary cache files must be removed")
}

func TestImportCacheAtomicPublication(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	first := bytes.Repeat([]byte("a"), 128*1024)
	second := bytes.Repeat([]byte("b"), 128*1024)
	path, err := cache.Set("owner", "repo", "shared.md", "sha", first)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		for range 50 {
			if _, err := cache.Set("owner", "repo", "shared.md", "sha", second); err != nil {
				done <- err
				return
			}
			if _, err := cache.Set("owner", "repo", "shared.md", "sha", first); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	// Raw reads verify atomic publication independently of reader validation.
	for range 100 {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		require.True(t, bytes.Equal(content, first) || bytes.Equal(content, second))
	}
	require.NoError(t, <-done)
}

func TestImportCacheConcurrentSymlinkReplacement(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	path, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("cached"))
	require.NoError(t, err)
	target := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(target, []byte("outside secret"), 0o600))
	tempPath := filepath.Join(filepath.Dir(path), "swap.md")
	requireCacheSymlink(t, target, tempPath)
	require.NoError(t, os.Remove(tempPath))
	var workers sync.WaitGroup
	stop := make(chan struct{})
	errors := make(chan error, 1)
	workers.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.Symlink(target, tempPath); err != nil {
				errors <- err
				return
			}
			if err := os.Rename(tempPath, path); err != nil {
				errors <- err
				return
			}
			if err := os.WriteFile(tempPath, []byte("cached"), 0o600); err != nil {
				errors <- err
				return
			}
			if err := os.Rename(tempPath, path); err != nil {
				errors <- err
				return
			}
		}
	})
	t.Cleanup(func() {
		close(stop)
		workers.Wait()
		select {
		case err := <-errors:
			require.NoError(t, err)
		default:
		}
	})
	for range 200 {
		content, err := ReadFile(path)
		if err == nil {
			require.Equal(t, "cached", string(content))
		} else {
			require.Empty(t, content)
		}
		// Successful writes must replace the link, never modify its target.
		_, _ = cache.Set("owner", "repo", "shared.md", "sha", []byte("cached"))
	}
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "outside secret", string(content))
}

func TestImportCacheCreatesMissingRepositoryRoot(t *testing.T) {
	base := filepath.Join(t.TempDir(), "nested", "repo")
	cache := NewImportCache(base)
	path, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("cached"))
	require.NoError(t, err)
	content, err := cache.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "cached", string(content))
}

func TestImportCacheRejectsNonRegularAndInvalidPaths(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	path := filepath.Join(cache.GetCacheDir(), "owner", "repo", "sha", "shared.md")
	require.NoError(t, os.MkdirAll(path, 0o700))
	_, found := cache.Get("owner", "repo", "shared.md", "sha")
	require.False(t, found)
	_, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("replacement"))
	require.Error(t, err)
	_, err = cache.ReadFile(filepath.Join(t.TempDir(), "outside.md"))
	require.ErrorContains(t, err, "outside the import cache")
	for _, owner := range []string{"../outside", ".", "nested/owner", `nested\owner`} {
		_, found := cache.Get(owner, "repo", "shared.md", "sha")
		require.False(t, found)
		_, err := cache.Set(owner, "repo", "shared.md", "sha", []byte("replacement"))
		require.Error(t, err)
	}
}
