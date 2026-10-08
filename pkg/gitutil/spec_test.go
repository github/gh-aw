//go:build !integration

package gitutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/gitutil"
)

// TestSpec_PublicAPI_ValidateGitRef validates the unsafe ref forms explicitly
// rejected by the README.md contract.
func TestSpec_PublicAPI_ValidateGitRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{name: "ordinary branch name is accepted", ref: "feature/spec-tests"},
		{name: "empty ref is rejected", ref: "", wantErr: true},
		{name: "leading dash is rejected", ref: "--upload-pack=evil", wantErr: true},
		{name: "NUL byte is rejected", ref: "main\x00evil", wantErr: true},
		{name: "object traversal expression is rejected", ref: "main..other", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitutil.ValidateGitRef(tt.ref)
			if tt.wantErr {
				assert.Error(t, err, "ValidateGitRef(%q) should reject the documented unsafe form", tt.ref)
				return
			}
			assert.NoError(t, err, "ValidateGitRef(%q) should accept an ordinary ref", tt.ref)
		})
	}
}

// TestSpec_PublicAPI_ValidateGitPath validates the unsafe path forms explicitly
// rejected by the README.md contract.
func TestSpec_PublicAPI_ValidateGitPath(t *testing.T) {
	t.Parallel()
	absolutePath := "/repo/file.txt"
	if runtime.GOOS == "windows" {
		absolutePath = `C:\repo\file.txt`
	}
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "repository relative path is accepted", path: "docs/spec.md"},
		{name: "empty path is rejected", path: "", wantErr: true},
		{name: "leading dash is rejected", path: "--help", wantErr: true},
		{name: "absolute path is rejected", path: absolutePath, wantErr: true},
		{name: "parent directory is rejected", path: "..", wantErr: true},
		{name: "leading traversal is rejected after cleaning", path: "safe/../../outside", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitutil.ValidateGitPath(tt.path)
			if tt.wantErr {
				assert.Error(t, err, "ValidateGitPath(%q) should reject the documented unsafe form", tt.path)
				return
			}
			assert.NoError(t, err, "ValidateGitPath(%q) should accept a repository-relative path", tt.path)
		})
	}
}

// TestSpec_PublicAPI_WorkingDirectories validates the successful behavior of
// the os.Getwd wrapper and the documented UserHomeDir public signature.
func TestSpec_PublicAPI_WorkingDirectories(t *testing.T) {
	t.Parallel()

	wantWorkingDirectory, err := os.Getwd()
	require.NoError(t, err, "os.Getwd should establish the documented comparison value")
	workingDirectory, err := gitutil.Getwd()
	require.NoError(t, err, "Getwd should return the current working directory")
	assert.Equal(t, wantWorkingDirectory, workingDirectory, "Getwd should behave like os.Getwd")

	userHomeDir := gitutil.UserHomeDir
	assert.NotNil(t, userHomeDir, "UserHomeDir should expose the documented public signature")
}

// TestSpec_ThreadSafety_Validators validates the README guarantee that exported
// pure validation helpers are safe for concurrent use.
func TestSpec_ThreadSafety_Validators(t *testing.T) {
	t.Parallel()
	const workers = 16
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Go(func() {
			assert.True(t, gitutil.IsHexString("aB01"), "concurrent IsHexString call should retain documented behavior")
			assert.True(t, gitutil.IsValidFullSHA("da39a3ee5e6b4b0d3255bfef95601890afd80709"), "concurrent SHA validation should retain documented behavior")
			assert.NoError(t, gitutil.ValidateGitRef("feature/spec"), "concurrent ref validation should remain safe")
		})
	}
	waitGroup.Wait()
}

// TestSpec_PublicAPI_IsHexString validates the documented behavior of
// IsHexString as described in the package README.md.
//
// Specification: Returns true if s consists entirely of hexadecimal characters
// (0–9, a–f, A–F). Returns false for the empty string.
func TestSpec_PublicAPI_IsHexString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "lowercase hex digits returns true",
			input:    "abcdef0123456789",
			expected: true,
		},
		{
			name:     "uppercase hex digits returns true",
			input:    "ABCDEF0123456789",
			expected: true,
		},
		{
			name:     "mixed case hex digits returns true",
			input:    "AbCdEf01",
			expected: true,
		},
		{
			name:     "numeric only returns true",
			input:    "123456",
			expected: true,
		},
		{
			name:     "non-hex character returns false",
			input:    "abcg",
			expected: false,
		},
		{
			name:     "empty string returns false (documented edge case)",
			input:    "",
			expected: false,
		},
		{
			name:     "string with space returns false",
			input:    "abc def",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := gitutil.IsHexString(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsHexString(%q) should match documented behavior", tt.input)
		})
	}
}

// TestSpec_PublicAPI_ExtractBaseRepo validates the documented behavior of
// ExtractBaseRepo as described in the package README.md.
//
// Specification: Extracts the owner/repo portion from an action path that may
// include a sub-folder.
//
// Documented examples:
//
//	gitutil.ExtractBaseRepo("actions/checkout")                   → "actions/checkout"
//	gitutil.ExtractBaseRepo("github/codeql-action/upload-sarif") → "github/codeql-action"
func TestSpec_PublicAPI_ExtractBaseRepo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "two-segment path returns as-is (documented example)",
			input:    "actions/checkout",
			expected: "actions/checkout",
		},
		{
			name:     "three-segment path strips sub-folder (documented example)",
			input:    "github/codeql-action/upload-sarif",
			expected: "github/codeql-action",
		},
		{
			name:     "four-segment path returns owner/repo only",
			input:    "owner/repo/sub/path",
			expected: "owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := gitutil.ExtractBaseRepo(tt.input)
			assert.Equal(t, tt.expected, result,
				"ExtractBaseRepo(%q) should extract owner/repo portion", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsValidFullSHA validates the documented behavior of
// IsValidFullSHA as described in the package README.md.
//
// Specification: Returns true if s is a valid 40-character lowercase hexadecimal
// SHA (the standard Git commit SHA format). Use this for strict SHA validation
// when the full 40-character form is required.
func TestSpec_PublicAPI_IsValidFullSHA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "40-character lowercase hex returns true",
			input:    "da39a3ee5e6b4b0d3255bfef95601890afd80709",
			expected: true,
		},
		{
			name:     "40-character with uppercase hex returns false (must be lowercase)",
			input:    "DA39A3EE5E6B4B0D3255BFEF95601890AFD80709",
			expected: false,
		},
		{
			name:     "39 characters returns false (too short)",
			input:    "da39a3ee5e6b4b0d3255bfef95601890afd807",
			expected: false,
		},
		{
			name:     "41 characters returns false (too long)",
			input:    "da39a3ee5e6b4b0d3255bfef95601890afd807091",
			expected: false,
		},
		{
			name:     "empty string returns false",
			input:    "",
			expected: false,
		},
		{
			name:     "non-hex character in 40-char string returns false",
			input:    "za39a3ee5e6b4b0d3255bfef95601890afd80709",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := gitutil.IsValidFullSHA(tt.input)
			assert.Equal(t, tt.expected, result,
				"IsValidFullSHA(%q) should match documented behavior", tt.input)
		})
	}
}

// TestSpec_PublicAPI_IsValidFullSHACaseInsensitive validates the documented
// behavior of IsValidFullSHACaseInsensitive as described in the package README.md.
func TestSpec_PublicAPI_IsValidFullSHACaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "40-character lowercase hex returns true", input: "da39a3ee5e6b4b0d3255bfef95601890afd80709", expected: true},
		{name: "40-character uppercase hex returns true", input: "DA39A3EE5E6B4B0D3255BFEF95601890AFD80709", expected: true},
		{name: "39 characters returns false", input: "da39a3ee5e6b4b0d3255bfef95601890afd807", expected: false},
		{name: "non-hex character returns false", input: "za39a3ee5e6b4b0d3255bfef95601890afd80709", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, gitutil.IsValidFullSHACaseInsensitive(tt.input))
		})
	}
}

// TestSpec_PublicAPI_FindGitRoot validates the documented behavior of
// FindGitRoot as described in the package README.md.
//
// Specification: Returns the absolute path of the root directory of the current
// Git repository using pure Go filesystem traversal (no `git` subprocess);
// starts from the current working directory.
func TestSpec_PublicAPI_FindGitRoot(t *testing.T) {
	t.Parallel()
	t.Run("returns non-empty absolute path when in git repository", func(t *testing.T) {
		t.Parallel()
		root, err := gitutil.FindGitRoot()
		require.NoError(t, err, "FindGitRoot should not error when inside a git repository")
		assert.NotEmpty(t, root, "FindGitRoot should return a non-empty path")
		assert.True(t, filepath.IsAbs(root),
			"FindGitRoot should return an absolute path, got %q", root)
	})
}

// TestSpec_PublicAPI_FindGitRootFrom validates the documented behavior of
// FindGitRootFrom as described in the package README.md.
//
// Specification: Like FindGitRoot but starts from startDir; traverses upward
// looking for a .git directory or worktree marker file (a `.git` file starting
// with `gitdir:`).
func TestSpec_PublicAPI_FindGitRootFrom(t *testing.T) {
	t.Parallel()
	t.Run("returns absolute repository root when startDir is inside a repo", func(t *testing.T) {
		t.Parallel()
		// The current working directory of this test is inside the gh-aw
		// repository, so any subdirectory inside it should resolve to the
		// repository root.
		repoRoot, err := gitutil.FindGitRoot()
		require.NoError(t, err, "FindGitRoot should succeed inside the gh-aw repository")

		root, err := gitutil.FindGitRootFrom(repoRoot)
		require.NoError(t, err, "FindGitRootFrom should succeed when startDir is inside a repository")
		assert.NotEmpty(t, root, "FindGitRootFrom should return a non-empty path")
		assert.True(t, filepath.IsAbs(root),
			"FindGitRootFrom should return an absolute path, got %q", root)
	})

	t.Run("traverses upward from a subdirectory to locate the repository root", func(t *testing.T) {
		t.Parallel()
		repoRoot, err := gitutil.FindGitRoot()
		require.NoError(t, err, "FindGitRoot should succeed inside the gh-aw repository")

		// Start from a subdirectory of the repo; FindGitRootFrom should walk
		// upward and land on the same root.
		fromSub, err := gitutil.FindGitRootFrom(filepath.Join(repoRoot, "pkg", "gitutil"))
		require.NoError(t, err, "FindGitRootFrom should succeed from a subdirectory")
		assert.Equal(t, repoRoot, fromSub,
			"FindGitRootFrom from a subdirectory should return the same root as FindGitRoot")
	})

	t.Run("returns error when startDir is not inside a git repository", func(t *testing.T) {
		t.Parallel()
		// A directory created outside any git repository should produce an error.
		isolated := t.TempDir()
		_, err := gitutil.FindGitRootFrom(isolated)
		assert.Error(t, err,
			"FindGitRootFrom should return an error when startDir is not inside a git repository")
	})
}

// TestSpec_Variables_ErrNotGitRepository validates the documented sentinel-error
// contract for ErrNotGitRepository.
//
// Specification (Variables + Behavioral contracts):
//   - ErrNotGitRepository is the sentinel error returned by FindGitRoot and
//     FindGitRootFrom when no .git entry is found while traversing up to the
//     filesystem root.
//   - FindGitRoot and FindGitRootFrom MUST return ErrNotGitRepository (not a
//     wrapped error) when the filesystem root is reached without finding a .git
//     entry.
//
// The README usage example relies on errors.Is(err, gitutil.ErrNotGitRepository),
// so the returned error must satisfy that comparison.
func TestSpec_Variables_ErrNotGitRepository(t *testing.T) {
	t.Parallel()
	require.Error(t, gitutil.ErrNotGitRepository,
		"ErrNotGitRepository should be a non-nil sentinel error value")

	// A directory created outside any git repository should surface the
	// documented sentinel error.
	isolated := t.TempDir()
	_, err := gitutil.FindGitRootFrom(isolated)
	require.Error(t, err,
		"FindGitRootFrom should error when startDir is not inside a git repository")
	assert.ErrorIs(t, err, gitutil.ErrNotGitRepository,
		"FindGitRootFrom should return the documented ErrNotGitRepository sentinel")
}

// TestSpec_PublicAPI_ReadFileFromHEAD validates the documented behavior of
// ReadFileFromHEAD as described in the package README.md.
//
// Specification: Reads a file's content from the HEAD commit without touching
// the working tree; rejects paths that escape the repository.
func TestSpec_PublicAPI_ReadFileFromHEAD(t *testing.T) {
	t.Parallel()
	root, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skip("not inside a git repository, skipping ReadFileFromHEAD tests")
	}

	t.Run("reads known file from HEAD without error", func(t *testing.T) {
		t.Parallel()
		content, err := gitutil.ReadFileFromHEAD(filepath.Join(root, "go.mod"), root)
		require.NoError(t, err, "ReadFileFromHEAD should read go.mod without error")
		assert.NotEmpty(t, content, "content of go.mod should not be empty")
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		t.Parallel()
		_, err := gitutil.ReadFileFromHEAD("this-file-does-not-exist-xyzzy.txt", root)
		assert.Error(t, err, "ReadFileFromHEAD should return error for non-existent file")
	})

	t.Run("rejects path with .. traversal", func(t *testing.T) {
		t.Parallel()
		// Specification: "The function rejects paths that escape the repository
		// (i.e. paths containing .. after resolution)."
		outsidePath := filepath.Join(root, "..", "outside.txt")
		_, err := gitutil.ReadFileFromHEAD(outsidePath, root)
		require.Error(t, err, "ReadFileFromHEAD should reject path-traversal attempts")
		require.ErrorContains(t, err, "outside the git repository root")
	})

	t.Run("returns error when gitRoot is empty", func(t *testing.T) {
		t.Parallel()
		// Specification: gitRoot must be the repository root (from FindGitRoot)
		_, err := gitutil.ReadFileFromHEAD("go.mod", "")
		assert.Error(t, err, "ReadFileFromHEAD should return error when gitRoot is empty")
	})
}
