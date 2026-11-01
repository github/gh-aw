package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ReadFileWithinBase reads an existing file without allowing symlinks to escape base.
// The base directory itself must not be a symlink.
// Absolute symlinks to targets inside base are supported by resolving them before
// the rooted read. The open root confines access even if path components change.
func ReadFileWithinBase(base, candidate string) (content []byte, err error) {
	root, err := openReadRoot(base)
	if err != nil {
		return nil, fmt.Errorf("failed to open base directory %q: %w", base, err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil && err == nil {
			content = nil
			err = fmt.Errorf("failed to close base directory %q: %w", base, closeErr)
		}
	}()

	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base directory %q: %w", base, err)
	}
	realBase, err = filepath.Abs(realBase)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute base directory %q: %w", base, err)
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve file %q: %w", candidate, err)
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute file %q: %w", candidate, err)
	}
	relativePath, err := filepath.Rel(realBase, realPath)
	if err != nil || !filepath.IsLocal(relativePath) {
		fileutilLog.Printf("ReadFileWithinBase: path escape detected: candidate=%q base=%q", candidate, base)
		return nil, fmt.Errorf("security: path %q escapes base directory %q", candidate, base)
	}
	content, err = root.ReadFile(relativePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q within base directory %q: %w", candidate, base, err)
	}
	return content, nil
}

func openReadRoot(base string) (*os.Root, error) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	rootInfo, err := root.Stat(".")
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	baseInfo, err := os.Lstat(base)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	if baseInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(rootInfo, baseInfo) {
		err := fmt.Errorf("security: base directory %q is a symlink or changed while opening", base)
		return nil, errors.Join(err, root.Close())
	}
	return root, nil
}
