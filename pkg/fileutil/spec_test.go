//go:build !integration

package fileutil_test

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/fileutil"
)

// TestSpec_PublicAPI_EnsureParentDir validates recursive parent creation and
// the documented empty-path error.
func TestSpec_PublicAPI_EnsureParentDir(t *testing.T) {
	t.Parallel()

	t.Run("creates missing parent directories", func(t *testing.T) {
		t.Parallel()
		parent := filepath.Join(t.TempDir(), "nested", "parent")
		err := fileutil.EnsureParentDir(filepath.Join(parent, "output.txt"), 0o750)
		require.NoError(t, err, "EnsureParentDir should create nested parents")
		assert.True(t, fileutil.DirExists(parent), "the requested parent directory should exist")
	})

	t.Run("rejects an empty path", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, fileutil.EnsureParentDir("", 0o750),
			"EnsureParentDir should reject an empty path")
	})
}

// TestSpec_PublicAPI_ValidateExecutablePath validates the documented absolute
// path, symlink resolution, regular-file, and executable-bit contracts.
func TestSpec_PublicAPI_ValidateExecutablePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	executable := filepath.Join(dir, "tool")
	require.NoError(t, os.WriteFile(executable, []byte("tool"), 0o700),
		"test executable should be created")

	resolved, err := fileutil.ValidateExecutablePath(executable)
	require.NoError(t, err, "an absolute executable file should be accepted")
	assert.Equal(t, executable, resolved, "the resolved executable path should be returned")

	require.Error(t, func() error {
		_, err := fileutil.ValidateExecutablePath("relative-tool")
		return err
	}(), "a relative executable path should be rejected")

	require.Error(t, func() error {
		_, err := fileutil.ValidateExecutablePath(dir)
		return err
	}(), "a directory should be rejected as a non-regular executable")

	if runtime.GOOS != "windows" {
		nonExecutable := filepath.Join(dir, "non-executable")
		require.NoError(t, os.WriteFile(nonExecutable, []byte("tool"), 0o600),
			"test non-executable should be created")
		_, err := fileutil.ValidateExecutablePath(nonExecutable)
		assert.Error(t, err, "a file without executable bits should be rejected on non-Windows systems")
	}
}

// TestSpec_PublicAPI_ResolveExecutablePath validates PATH lookup, validation,
// and the documented empty-name and missing-command errors.
func TestSpec_PublicAPI_ResolveExecutablePath(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "spec-tool")
	require.NoError(t, os.WriteFile(executable, []byte("tool"), 0o700),
		"test executable should be created")
	t.Setenv("PATH", dir)

	resolved, err := fileutil.ResolveExecutablePath("spec-tool")
	require.NoError(t, err, "a command present on PATH should resolve")
	assert.Equal(t, executable, resolved, "ResolveExecutablePath should return the validated absolute path")

	_, err = fileutil.ResolveExecutablePath("")
	require.Error(t, err, "an empty executable name should be rejected")

	_, err = fileutil.ResolveExecutablePath("missing-spec-tool")
	assert.Error(t, err, "a command absent from PATH should return an error")
}

// TestSpec_PublicAPI_ValidateAbsolutePath validates the documented behavior:
// rejects empty paths, cleans with filepath.Clean, verifies cleaned path is absolute.
func TestSpec_PublicAPI_ValidateAbsolutePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		expectedOut string
	}{
		{
			name:    "rejects empty path",
			input:   "",
			wantErr: true,
		},
		{
			name:    "rejects relative path",
			input:   "relative/path",
			wantErr: true,
		},
		{
			name:        "accepts and returns clean absolute path",
			input:       "/usr/local/bin",
			wantErr:     false,
			expectedOut: "/usr/local/bin",
		},
		{
			name:        "cleans dot components from absolute path",
			input:       "/usr/./local/bin",
			wantErr:     false,
			expectedOut: "/usr/local/bin",
		},
		{
			name:        "cleans double-dot components from absolute path",
			input:       "/usr/local/../bin",
			wantErr:     false,
			expectedOut: "/usr/bin",
		},
		{
			name:    "rejects control characters in absolute path",
			input:   "/usr/local/\n/bin",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := fileutil.ValidateAbsolutePath(tt.input)
			if tt.wantErr {
				assert.Error(t, err, "expected error for input %q", tt.input)
				return
			}
			require.NoError(t, err, "unexpected error for input %q", tt.input)
			assert.Equal(t, tt.expectedOut, result, "cleaned path mismatch for input %q", tt.input)
		})
	}
}

// TestSpec_PublicAPI_ValidatePathWithinBase validates that candidate must be within the base directory.
// Spec: "prevents both .. traversal and symlink escapes"
func TestSpec_PublicAPI_ValidatePathWithinBase(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	within := filepath.Join(base, "subdir", "file.txt")
	outside := filepath.Join(base, "..", "outside")

	t.Run("accepts path within base", func(t *testing.T) {
		t.Parallel()
		err := fileutil.ValidatePathWithinBase(base, within)
		assert.NoError(t, err, "path within base should be accepted")
	})

	t.Run("rejects path outside base", func(t *testing.T) {
		t.Parallel()
		err := fileutil.ValidatePathWithinBase(base, outside)
		assert.Error(t, err, "path outside base should be rejected")
	})

	t.Run("accepts base path itself", func(t *testing.T) {
		t.Parallel()
		err := fileutil.ValidatePathWithinBase(base, base)
		assert.NoError(t, err, "base path itself should be accepted")
	})
}

// TestSpec_PublicAPI_FileExists validates the documented behavior:
// returns true for regular files, false for directories and non-existent paths.
func TestSpec_PublicAPI_FileExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "regular.txt")
	require.NoError(t, os.WriteFile(regularFile, []byte("content"), 0600))

	t.Run("returns true for regular file", func(t *testing.T) {
		t.Parallel()
		assert.True(t, fileutil.FileExists(regularFile), "FileExists should return true for regular file")
	})

	t.Run("returns false for directory", func(t *testing.T) {
		t.Parallel()
		assert.False(t, fileutil.FileExists(dir), "FileExists should return false for directory")
	})

	t.Run("returns false for non-existent path", func(t *testing.T) {
		t.Parallel()
		assert.False(t, fileutil.FileExists(filepath.Join(dir, "nonexistent.txt")), "FileExists should return false for non-existent path")
	})
}

// TestSpec_PublicAPI_DirExists validates the documented behavior:
// returns true for directories, false for regular files and non-existent paths.
func TestSpec_PublicAPI_DirExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(regularFile, []byte("content"), 0600))

	t.Run("returns true for existing directory", func(t *testing.T) {
		t.Parallel()
		assert.True(t, fileutil.DirExists(dir), "DirExists should return true for directory")
	})

	t.Run("returns false for regular file", func(t *testing.T) {
		t.Parallel()
		assert.False(t, fileutil.DirExists(regularFile), "DirExists should return false for regular file")
	})

	t.Run("returns false for non-existent path", func(t *testing.T) {
		t.Parallel()
		assert.False(t, fileutil.DirExists(filepath.Join(dir, "nonexistent")), "DirExists should return false for non-existent path")
	})
}

// TestSpec_PublicAPI_IsDirEmpty validates the documented behavior:
// returns true when directory has no entries, true when directory cannot be read.
func TestSpec_PublicAPI_IsDirEmpty(t *testing.T) {
	t.Parallel()
	t.Run("returns true for empty directory", func(t *testing.T) {
		t.Parallel()
		emptyDir := t.TempDir()
		assert.True(t, fileutil.IsDirEmpty(emptyDir), "IsDirEmpty should return true for empty directory")
	})

	t.Run("returns false for non-empty directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0600))
		assert.False(t, fileutil.IsDirEmpty(dir), "IsDirEmpty should return false for non-empty directory")
	})

	t.Run("returns true for unreadable or non-existent path", func(t *testing.T) {
		t.Parallel()
		assert.True(t, fileutil.IsDirEmpty("/nonexistent/path/xyzzy"), "IsDirEmpty should return true when directory cannot be read")
	})
}

// TestSpec_PublicAPI_CopyFile validates the documented behavior:
// copies src to dst using buffered I/O, creates dst if not exist, truncates if exists.
func TestSpec_PublicAPI_CopyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "source.txt")
	content := []byte("hello specification test")

	require.NoError(t, os.WriteFile(src, content, 0600))

	t.Run("copies file content to new destination", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(dir, "destination-copy.txt")
		err := fileutil.CopyFile(src, dst)
		require.NoError(t, err, "CopyFile should not error for valid src/dst")
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, content, got, "destination should have same content as source")
	})

	t.Run("truncates existing destination", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(dir, "destination-truncate.txt")
		require.NoError(t, os.WriteFile(dst, []byte("old content that is longer"), 0600))
		err := fileutil.CopyFile(src, dst)
		require.NoError(t, err, "CopyFile should not error when destination exists")
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, content, got, "destination should be truncated and overwritten with source content")
	})
}

// makeTar builds an in-memory tar archive with named entries.
func makeTar(entries map[string][]byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, data := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(data)),
		}
		_ = tw.WriteHeader(hdr)
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	return buf.Bytes()
}

// TestSpec_PublicAPI_ExtractFileFromTar validates extraction and security guarantees.
// Spec: extracts single file by path, skips entries with unsafe names.
func TestSpec_PublicAPI_ExtractFileFromTar(t *testing.T) {
	t.Parallel()
	t.Run("extracts file at specified path from tar", func(t *testing.T) {
		t.Parallel()
		want := []byte("binary content")
		data := makeTar(map[string][]byte{
			"bin/gh": want,
			"other":  []byte("other content"),
		})
		got, err := fileutil.ExtractFileFromTar(data, "bin/gh")
		require.NoError(t, err, "ExtractFileFromTar should not error for present file")
		assert.Equal(t, want, got, "extracted content should match file in archive")
	})

	t.Run("returns error when path is not present in tar", func(t *testing.T) {
		t.Parallel()
		data := makeTar(map[string][]byte{"other": []byte("data")})
		_, err := fileutil.ExtractFileFromTar(data, "bin/gh")
		assert.Error(t, err, "should error when requested path is not in archive")
	})

	t.Run("rejects caller-supplied absolute path", func(t *testing.T) {
		t.Parallel()
		data := makeTar(map[string][]byte{"bin/gh": []byte("x")})
		_, err := fileutil.ExtractFileFromTar(data, "/bin/gh")
		assert.Error(t, err, "absolute caller path should be rejected")
	})

	t.Run("rejects caller-supplied path with .. traversal", func(t *testing.T) {
		t.Parallel()
		data := makeTar(map[string][]byte{"bin/gh": []byte("x")})
		_, err := fileutil.ExtractFileFromTar(data, "../bin/gh")
		assert.Error(t, err, "path traversal in caller path should be rejected")
	})

	t.Run("skips tar entries with unsafe names", func(t *testing.T) {
		t.Parallel()
		// SPEC: "Individual tar entries with unsafe names are skipped, not extracted"
		// The unsafe entry should not be returned even when requested.
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{Name: "../etc/passwd", Mode: 0600, Size: 4})
		_, _ = tw.Write([]byte("root"))
		_ = tw.Close()

		_, err := fileutil.ExtractFileFromTar(buf.Bytes(), "../etc/passwd")
		assert.Error(t, err, "tar entry with unsafe name should be skipped/not found")
	})
}
