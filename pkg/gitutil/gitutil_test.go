//go:build !integration

package gitutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsHexString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid lowercase hex",
			input:    "deadbeef",
			expected: true,
		},
		{
			name:     "valid uppercase hex",
			input:    "DEADBEEF",
			expected: true,
		},
		{
			name:     "valid mixed case hex",
			input:    "DeAdBeEf",
			expected: true,
		},
		{
			name:     "valid full git sha",
			input:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			expected: true,
		},
		{
			name:     "digits only",
			input:    "0123456789",
			expected: true,
		},
		{
			name:     "single valid char",
			input:    "a",
			expected: true,
		},
		{
			name:     "invalid char g",
			input:    "deadbeeg",
			expected: false,
		},
		{
			name:     "contains space",
			input:    "dead beef",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "non-hex word",
			input:    "xyz",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsHexString(tt.input)
			assert.Equal(t, tt.expected, result, "IsHexString(%q) should return %v", tt.input, tt.expected)
		})
	}
}

func TestIsValidFullSHA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid lowercase full SHA",
			input:    "abcdef0123456789abcdef0123456789abcdef01",
			expected: true,
		},
		{
			name:     "invalid uppercase full SHA",
			input:    "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			expected: false,
		},
		{
			name:     "invalid short SHA",
			input:    "abcdef0",
			expected: false,
		},
		{
			name:     "invalid non-hex character",
			input:    "abcdef0123456789abcdef0123456789abcdef0g",
			expected: false,
		},
		{
			name:     "invalid empty SHA",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsValidFullSHA(tt.input)
			assert.Equal(t, tt.expected, result, "IsValidFullSHA(%q) should return %v", tt.input, tt.expected)
		})
	}
}

func TestIsValidFullSHACaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "valid lowercase full SHA", input: "abcdef0123456789abcdef0123456789abcdef01", expected: true},
		{name: "valid uppercase full SHA", input: "ABCDEF0123456789ABCDEF0123456789ABCDEF01", expected: true},
		{name: "invalid short SHA", input: "abcdef0", expected: false},
		{name: "invalid non-hex character", input: "abcdef0123456789abcdef0123456789abcdef0g", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, IsValidFullSHACaseInsensitive(tt.input))
		})
	}
}

func TestIsGitObjectID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid sha1 object id",
			input:    "abcdef0123456789abcdef0123456789abcdef01",
			expected: true,
		},
		{
			name:     "valid sha256 object id",
			input:    "abcdef0123456789abcdef0123456789abcdef01abcdef0123456789abcdef01",
			expected: true,
		},
		{
			name:     "rejects uppercase object id",
			input:    "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			expected: false,
		},
		{
			name:     "rejects ref expression",
			input:    "HEAD~1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isGitObjectID(tt.input))
		})
	}
}

func TestExtractBaseRepo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple owner/repo path",
			input:    "actions/checkout",
			expected: "actions/checkout",
		},
		{
			name:     "path with one subpath segment",
			input:    "github/codeql-action/upload-sarif",
			expected: "github/codeql-action",
		},
		{
			name:     "deep path with multiple segments",
			input:    "owner/repo/sub/dir/file",
			expected: "owner/repo",
		},
		{
			name:     "no slash returns input as-is",
			input:    "onlyone",
			expected: "onlyone",
		},
		{
			name:     "empty string returns empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "preserves empty owner segment",
			input:    "/repo",
			expected: "/repo",
		},
		{
			name:     "preserves traversal segment",
			input:    "owner/..",
			expected: "owner/..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ExtractBaseRepo(tt.input)
			assert.Equal(t, tt.expected, result, "ExtractBaseRepo(%q) should return %q", tt.input, tt.expected)
		})
	}
}

func TestGetwd(t *testing.T) {
	t.Run("returns the current working directory", func(t *testing.T) {
		t.Parallel()
		dir, err := Getwd()
		require.NoError(t, err, "Getwd should succeed in a normal test environment")
		assert.NotEmpty(t, dir, "Getwd should return a non-empty path")

		wantDir, wantErr := os.Getwd()
		require.NoError(t, wantErr)
		assert.Equal(t, wantDir, dir, "Getwd should match os.Getwd")
	})

	t.Run("returns wrapped error with recovery guidance", func(t *testing.T) {
		orig := osGetwd
		t.Cleanup(func() { osGetwd = orig })
		osGetwd = func() (string, error) {
			return "", errors.New("cwd unavailable")
		}

		_, err := Getwd()
		require.Error(t, err)
		require.ErrorContains(t, err, "cwd unavailable")
		require.ErrorContains(t, err, "failed to determine current working directory")
		require.ErrorContains(t, err, "valid working directory and read permissions")
	})
}

func TestUserHomeDir(t *testing.T) {
	t.Parallel()
	t.Run("returns the current user's home directory", func(t *testing.T) {
		t.Parallel()
		home, err := UserHomeDir()
		require.NoError(t, err, "UserHomeDir should succeed in a normal test environment")
		assert.NotEmpty(t, home, "UserHomeDir should return a non-empty path")

		wantHome, wantErr := os.UserHomeDir()
		require.NoError(t, wantErr)
		assert.Equal(t, wantHome, home, "UserHomeDir should match os.UserHomeDir")
	})

	t.Run("returns wrapped error with recovery guidance", func(t *testing.T) {
		orig := osUserHomeDir
		t.Cleanup(func() { osUserHomeDir = orig })
		osUserHomeDir = func() (string, error) {
			return "", errors.New("home unavailable")
		}

		_, err := UserHomeDir()
		require.Error(t, err)
		require.ErrorContains(t, err, "home unavailable")
		require.ErrorContains(t, err, "failed to determine home directory")
		require.ErrorContains(t, err, "HOME (Unix) or USERPROFILE/HOMEDRIVE/HOMEPATH (Windows)")
	})
}

func TestFindGitRoot(t *testing.T) {
	t.Parallel()
	t.Run("returns non-empty path when inside a git repository", func(t *testing.T) {
		t.Parallel()
		gitRoot, err := FindGitRoot()
		require.NoError(t, err, "FindGitRoot should succeed when running inside a git repository")
		assert.NotEmpty(t, gitRoot, "FindGitRoot should return a non-empty path")
	})
}

func TestFindGitRootFrom(t *testing.T) {
	t.Parallel()
	t.Run("returns git root from the repository root itself", func(t *testing.T) {
		t.Parallel()
		gitRoot, err := FindGitRoot()
		require.NoError(t, err, "must be inside a git repository")

		root, err := FindGitRootFrom(gitRoot)
		require.NoError(t, err, "FindGitRootFrom should succeed when starting from the git root")
		assert.Equal(t, gitRoot, root, "FindGitRootFrom from git root should return git root")
	})

	t.Run("returns git root from a subdirectory", func(t *testing.T) {
		t.Parallel()
		gitRoot, err := FindGitRoot()
		require.NoError(t, err, "must be inside a git repository")

		// Create a temporary subdirectory inside the repo to avoid depending on
		// specific repo layout (e.g. pkg/ may not exist in all test environments).
		subDir, mkdirErr := os.MkdirTemp(gitRoot, "test-subdir-*")
		require.NoError(t, mkdirErr, "should create temp subdir inside git repo")
		defer os.RemoveAll(subDir)

		root, err := FindGitRootFrom(subDir)
		require.NoError(t, err, "FindGitRootFrom should succeed from a subdirectory")
		assert.Equal(t, gitRoot, root, "FindGitRootFrom from subdirectory should return the git root")
	})

	t.Run("returns error when starting outside any git repository", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		// Create a nested directory that is definitely not a git repo
		nonRepoDir := filepath.Join(tmpDir, "not-a-git-repo", "subdir")
		require.NoError(t, os.MkdirAll(nonRepoDir, 0755), "should create nested temp dir")

		_, err := FindGitRootFrom(nonRepoDir)
		require.Error(t, err, "FindGitRootFrom should return error outside a git repository")
		require.ErrorContains(t, err, "not in a git repository", "error should mention not in git repository")
		require.ErrorContains(t, err, "run this command from inside a git repository", "error should include recovery guidance")
	})

	t.Run("returns git root when .git is a worktree marker file", func(t *testing.T) {
		t.Parallel()
		// Simulate a git worktree: the repo root has a .git *file* (not dir)
		// whose content begins with "gitdir: /some/path"
		tmpDir := t.TempDir()
		repoRoot := filepath.Join(tmpDir, "worktree-repo")
		require.NoError(t, os.MkdirAll(repoRoot, 0755))

		// Write a valid worktree .git file
		gitFile := filepath.Join(repoRoot, ".git")
		require.NoError(t, os.WriteFile(gitFile, []byte("gitdir: /tmp/real-repo/.git/worktrees/myworktree\n"), 0644))

		// Start from the root itself
		root, err := FindGitRootFrom(repoRoot)
		require.NoError(t, err, "FindGitRootFrom should detect a worktree .git file")
		assert.Equal(t, repoRoot, root)

		// Start from a subdirectory inside the worktree
		subDir := filepath.Join(repoRoot, "pkg", "sub")
		require.NoError(t, os.MkdirAll(subDir, 0755))
		root, err = FindGitRootFrom(subDir)
		require.NoError(t, err, "FindGitRootFrom should detect worktree root from a subdirectory")
		assert.Equal(t, repoRoot, root)
	})

	t.Run("ignores non-worktree .git files without gitdir prefix", func(t *testing.T) {
		t.Parallel()
		// A plain file named .git that does NOT start with "gitdir:" should not
		// be treated as a valid repo root.
		tmpDir := t.TempDir()
		repoRoot := filepath.Join(tmpDir, "fake-git-file")
		require.NoError(t, os.MkdirAll(repoRoot, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".git"), []byte("not a valid git file\n"), 0644))

		_, err := FindGitRootFrom(repoRoot)
		require.Error(t, err, "FindGitRootFrom should not accept a .git file without gitdir: prefix")
		require.ErrorContains(t, err, "not in a git repository")
	})

	t.Run("handles relative path input", func(t *testing.T) {
		t.Parallel()
		// "." should resolve to os.Getwd(). Skip gracefully if the working
		// directory is not inside a git repository (e.g. some CI containers).
		root, err := FindGitRootFrom(".")
		if err != nil {
			t.Skipf("skipping: working directory is not inside a git repository (%v)", err)
		}
		assert.NotEmpty(t, root)
	})
}

func TestReadFileFromHEAD(t *testing.T) {
	t.Parallel()
	t.Run("reads a committed file with pre-computed root", func(t *testing.T) {
		t.Parallel()
		gitRoot, err := FindGitRoot()
		require.NoError(t, err, "must be inside a git repository")

		content, err := ReadFileFromHEAD(filepath.Join(gitRoot, "go.mod"), gitRoot)
		require.NoError(t, err, "go.mod should be readable from HEAD with pre-computed root")
		assert.NotEmpty(t, content, "go.mod content should not be empty")
		assert.Contains(t, content, "module ", "go.mod should contain a module declaration")
	})

	t.Run("returns error for path outside git root", func(t *testing.T) {
		t.Parallel()
		gitRoot, err := FindGitRoot()
		require.NoError(t, err, "must be inside a git repository")

		outsidePath := filepath.Join(t.TempDir(), "file.yml")
		_, err = ReadFileFromHEAD(outsidePath, gitRoot)
		require.Error(t, err, "should fail for a file outside the git root")
		require.ErrorContains(t, err, "outside the git repository root", "error should mention path is outside repo")
	})

	t.Run("returns error for empty gitRoot", func(t *testing.T) {
		t.Parallel()
		_, err := ReadFileFromHEAD("some/file.yml", "")
		require.Error(t, err, "should fail when gitRoot is empty")
		require.ErrorContains(t, err, "gitRoot must not be empty", "error should mention empty gitRoot")
	})
}
