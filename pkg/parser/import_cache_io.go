package parser

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
)

func closeImportCacheHandle(handle io.Closer) {
	if err := handle.Close(); err != nil {
		importCacheLog.Printf("Failed to close import cache handle: %v", err)
	}
}

// openCacheDirectory pins each directory before descending. os.Root confines
// operations even if a repository-controlled component is replaced after Lstat.
func (c *ImportCache) openCacheDirectory(relativeDir string, create bool) (*os.Root, error) {
	if runtime.GOARCH == "wasm" || runtime.GOOS == "plan9" {
		return nil, errors.New("confined on-disk import cache is unavailable on this platform")
	}
	if !filepath.IsLocal(relativeDir) {
		return nil, fmt.Errorf("invalid cache directory: %q", relativeDir)
	}
	base, err := filepath.Abs(c.baseDir)
	if err != nil {
		return nil, err
	}
	base, err = fileutil.ValidateAbsolutePath(base)
	if err != nil {
		return nil, err
	}
	if create {
		// The caller supplies the repository root; only its cache descendants
		// are repository-controlled and must be traversed through handles.
		if err := os.MkdirAll(base, constants.DirPermSensitive); err != nil {
			return nil, err
		}
	}
	dir, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	for component := range strings.SplitSeq(relativeDir, string(filepath.Separator)) {
		if create {
			if err := dir.Mkdir(component, constants.DirPermSensitive); err != nil && !errors.Is(err, os.ErrExist) {
				closeImportCacheHandle(dir)
				return nil, err
			}
		}
		info, err := dir.Lstat(component)
		if err != nil {
			closeImportCacheHandle(dir)
			return nil, err
		}
		if !info.IsDir() {
			closeImportCacheHandle(dir)
			return nil, fmt.Errorf("cache directory %q is not a directory (symlinks are not allowed)", component)
		}
		next, err := dir.OpenRoot(component)
		if err != nil {
			closeImportCacheHandle(dir)
			return nil, err
		}
		opened, err := next.Stat(".")
		closeImportCacheHandle(dir)
		if err != nil || !os.SameFile(info, opened) {
			closeImportCacheHandle(next)
			return nil, fmt.Errorf("cache directory %q changed while opening", component)
		}
		dir = next
	}
	return dir, nil
}

func regularCacheFile(dir *os.Root, name string) (os.FileInfo, error) {
	info, err := dir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cache file %q is not a regular file (symlinks are not allowed)", name)
	}
	return info, nil
}

// ReadFile reads a cache path through confined directory handles, rejecting
// symlinks, non-regular files, and paths outside this cache.
func (c *ImportCache) ReadFile(path string) ([]byte, error) {
	base, err := filepath.Abs(c.GetCacheDir())
	if err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(base, absolutePath)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return nil, fmt.Errorf("file %q is outside the import cache", path)
	}
	dir, err := c.openCacheDirectory(filepath.Join(ImportCacheDir, filepath.Dir(relative)), false)
	if err != nil {
		return nil, err
	}
	defer closeImportCacheHandle(dir)
	name := filepath.Base(relative)
	info, err := regularCacheFile(dir, name)
	if err != nil {
		return nil, err
	}
	file, err := dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer closeImportCacheHandle(file)
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("cache file %q changed while opening", name)
	}
	return io.ReadAll(file)
}

func writeCacheFileAtomic(dir *os.Root, name string, content []byte, perm os.FileMode, replace bool) error {
	if _, err := regularCacheFile(dir, name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tempName := ".gh-aw-import-" + rand.Text()
	file, err := dir.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if err := dir.Remove(tempName); err != nil && !errors.Is(err, os.ErrNotExist) {
			importCacheLog.Printf("Failed to remove temporary cache file: %v", err)
		}
	}()
	_, writeErr := file.Write(content)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	if replace {
		// Rename replaces the directory entry, never truncating a symlink or
		// hardlink target introduced concurrently at the destination.
		return dir.Rename(tempName, name)
	}
	// Publish .gitattributes only if absent, preserving concurrent/custom files.
	if err := dir.Link(tempName, name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		_, err = regularCacheFile(dir, name)
		return err
	}
	return nil
}

// readCacheAwareFile keeps every native parser consumer on the confined read
// path, including consumers that receive a cache path without an ImportCache.
func readCacheAwareFile(path string) ([]byte, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	marker := string(filepath.Separator) + filepath.FromSlash(ImportCacheDir) + string(filepath.Separator)
	if index := strings.Index(absolutePath, marker); index >= 0 {
		return NewImportCache(absolutePath[:index+1]).ReadFile(absolutePath)
	}
	return os.ReadFile(path)
}
