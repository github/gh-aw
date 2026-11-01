//go:build !integration && !js && !wasm

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadFileWithinBaseSymlinks(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "shared"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "shared", "safe.md"), []byte("safe"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0644))
	links := map[string]string{
		"relative.md": "shared/safe.md",
		"absolute.md": filepath.Join(base, "shared", "safe.md"),
		"parent":      "shared",
		"escape.md":   filepath.Join(outside, "secret.md"),
		"escape-dir":  outside,
		"dangling.md": filepath.Join(outside, "missing.md"),
		"dangling":    filepath.Join(outside, "missing"),
		"loop.md":     "loop.md",
	}
	for name, target := range links {
		require.NoError(t, os.Symlink(target, filepath.Join(base, name)))
	}
	for _, test := range []struct {
		path    string
		wantErr bool
	}{
		{path: "shared/safe.md"},
		{path: "relative.md"},
		{path: "absolute.md"},
		{path: "parent/safe.md"},
		{path: "escape.md", wantErr: true},
		{path: "escape-dir/secret.md", wantErr: true},
		{path: "escape-dir/missing.md", wantErr: true},
		{path: "dangling.md", wantErr: true},
		{path: "dangling/child.md", wantErr: true},
		{path: "loop.md", wantErr: true},
		{path: "../secret.md", wantErr: true},
	} {
		t.Run(test.path, func(t *testing.T) {
			content, err := ReadFileWithinBase(base, filepath.Join(base, test.path))
			if test.wantErr {
				require.Error(t, err)
				require.Empty(t, content)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "safe", string(content))
		})
	}
}

func TestReadFileWithinBaseRelativePaths(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "safe.md"), []byte("safe"), 0644))
	t.Chdir(base)
	content, err := ReadFileWithinBase(".", "safe.md")
	require.NoError(t, err)
	require.Equal(t, "safe", string(content))
}

func TestReadFileWithinBaseConcurrentParentReplacement(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	dir := filepath.Join(base, "shared")
	parked := filepath.Join(base, "parked")
	require.NoError(t, os.Mkdir(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "safe.md"), []byte("safe"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "safe.md"), []byte("secret"), 0644))
	done := make(chan error, 1)
	go func() {
		done <- replaceReadParentRepeatedly(dir, parked, outside)
	}()
	var readErr error
	successfulReads := 0
	for range 200 {
		content, err := ReadFileWithinBase(base, filepath.Join(dir, "safe.md"))
		if err == nil {
			successfulReads++
			if string(content) != "safe" {
				readErr = fmt.Errorf("read content outside base: %q", content)
				break
			}
		}
	}
	require.NoError(t, <-done)
	require.NoError(t, readErr)
	// The scheduler may allow only rejected reads during replacement.
	if successfulReads == 0 {
		content, err := ReadFileWithinBase(base, filepath.Join(dir, "safe.md"))
		require.NoError(t, err)
		require.Equal(t, "safe", string(content))
		successfulReads++
	}
	require.Positive(t, successfulReads)
}

func replaceReadParentRepeatedly(dir, parked, outside string) error {
	for range 200 {
		if err := os.Rename(dir, parked); err != nil {
			return err
		}
		if err := os.Symlink(outside, dir); err != nil {
			return err
		}
		if err := os.Remove(dir); err != nil {
			return err
		}
		if err := os.Rename(parked, dir); err != nil {
			return err
		}
	}
	return nil
}

func TestReadFileWithinBaseRejectsSymlinkedBase(t *testing.T) {
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0644))
	base := filepath.Join(t.TempDir(), "base")
	require.NoError(t, os.Symlink(outside, base))
	content, err := ReadFileWithinBase(base, filepath.Join(base, "secret.md"))
	require.ErrorContains(t, err, "is a symlink or changed while opening")
	require.Empty(t, content)
}

func TestReadFileWithinBaseConcurrentBaseReplacement(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "base")
	outside := t.TempDir()
	require.NoError(t, os.Mkdir(base, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "safe.md"), []byte("safe"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "safe.md"), []byte("secret"), 0644))
	done := make(chan error, 1)
	go func() {
		done <- replaceReadParentRepeatedly(base, filepath.Join(parent, "parked"), outside)
	}()
	var readErr error
	successfulReads := 0
	for range 200 {
		content, err := ReadFileWithinBase(base, filepath.Join(base, "safe.md"))
		if err == nil {
			successfulReads++
			if string(content) != "safe" {
				readErr = fmt.Errorf("read content from replaced base: %q", content)
				break
			}
		}
	}
	require.NoError(t, <-done)
	require.NoError(t, readErr)
	// The scheduler may allow only rejected reads during replacement.
	if successfulReads == 0 {
		content, err := ReadFileWithinBase(base, filepath.Join(base, "safe.md"))
		require.NoError(t, err)
		require.Equal(t, "safe", string(content))
		successfulReads++
	}
	require.Positive(t, successfulReads)
}
