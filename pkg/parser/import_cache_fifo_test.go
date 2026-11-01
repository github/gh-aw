//go:build !integration && unix

package parser

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImportCacheOpenFIFOIsNonblocking(t *testing.T) {
	cache := NewImportCache(t.TempDir())
	path, err := cache.Set("owner", "repo", "shared.md", "sha", []byte("cached"))
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	dir, err := cache.openCacheDirectory(filepath.Join(ImportCacheDir, "owner", "repo", "sha"), false)
	require.NoError(t, err)
	defer closeImportCacheHandle(dir)

	result := make(chan error, 1)
	go func() {
		file, openErr := openImportCacheFile(dir, filepath.Base(path))
		if openErr == nil {
			info, statErr := file.Stat()
			closeImportCacheHandle(file)
			if statErr != nil {
				result <- statErr
				return
			}
			if info.Mode().IsRegular() {
				result <- os.ErrInvalid
				return
			}
		}
		result <- openErr
	}()

	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("opening a cache FIFO blocked")
	}
}
