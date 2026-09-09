//go:build !integration

package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDefaultBranchFromLsRemote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard main branch",
			input:    "ref: refs/heads/main\tabc123def456\nabc123def456\tHEAD\n",
			expected: "main",
		},
		{
			name:     "master branch",
			input:    "ref: refs/heads/master\tabc123def456\nabc123def456\tHEAD\n",
			expected: "master",
		},
		{
			name:     "custom default branch name",
			input:    "ref: refs/heads/develop\tabc123def456\nabc123def456\tHEAD\n",
			expected: "develop",
		},
		{
			name:     "branch with slashes",
			input:    "ref: refs/heads/release/v2\tabc123def456\nabc123def456\tHEAD\n",
			expected: "release/v2",
		},
		{
			name:     "branch with hyphens and numbers",
			input:    "ref: refs/heads/my-feature-123\tabc123def456\nabc123def456\tHEAD\n",
			expected: "my-feature-123",
		},
		{
			name:     "full 40-char sha",
			input:    "ref: refs/heads/main\t4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b\n4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b\tHEAD\n",
			expected: "main",
		},
		{
			name:     "empty output",
			input:    "",
			expected: "",
		},
		{
			name:     "no symref line",
			input:    "abc123def456\tHEAD\n",
			expected: "",
		},
		{
			name:     "only newlines",
			input:    "\n\n\n",
			expected: "",
		},
		{
			name:     "no trailing newline",
			input:    "ref: refs/heads/main\tabc123",
			expected: "main",
		},
		{
			name:     "extra whitespace in output",
			input:    "ref: refs/heads/main \tabc123def456\nabc123def456\tHEAD\n",
			expected: "main",
		},
		{
			// This is what the old buggy code would have produced:
			// strings.Fields("ref: refs/heads/main\tabc123") -> ["ref:", "refs/heads/main", "abc123"]
			// Using parts[0] ("ref:") with TrimPrefix("ref: refs/heads/") -> "ref:" (no match, returns "ref:")
			// This test ensures we DON'T return "ref:" or any garbage.
			name:     "regression: must not return ref: prefix",
			input:    "ref: refs/heads/main\tabc123def456\nabc123def456\tHEAD\n",
			expected: "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseDefaultBranchFromLsRemote(tt.input)
			assert.Equal(t, tt.expected, result, "parsed branch should match expected")
			// Ensure we never return something that looks like a git ref prefix
			assert.False(t, strings.HasPrefix(result, "ref:"), "result must not start with 'ref:' (regression check)")
			assert.False(t, strings.HasPrefix(result, "refs/"), "result must not start with 'refs/' (regression check)")
		})
	}
}

// TestParseDefaultBranchFromLsRemoteWithRealGit creates real git repositories
// and runs actual `git ls-remote --symref` to verify parsing against real git output.
func TestParseDefaultBranchFromLsRemoteWithRealGit(t *testing.T) {
	t.Parallel()
	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	tests := []struct {
		name          string
		defaultBranch string
	}{
		{name: "main branch", defaultBranch: "main"},
		{name: "master branch", defaultBranch: "master"},
		{name: "custom branch", defaultBranch: "develop"},
		{name: "branch with hyphen", defaultBranch: "my-default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a bare repo to serve as "origin"
			bareDir := t.TempDir()
			runGit(t, bareDir, "init", "--bare", "--initial-branch="+tt.defaultBranch)

			// Clone it to create a working repo with that origin
			workDir := filepath.Join(t.TempDir(), "work")
			runGitIn(t, "", "clone", bareDir, workDir)

			// Create an initial commit so HEAD exists
			dummyFile := filepath.Join(workDir, "README.md")
			require.NoError(t, os.WriteFile(dummyFile, []byte("# test\n"), 0644), "should create dummy file")
			runGit(t, workDir, "add", ".")
			runGit(t, workDir, "commit", "-m", "initial commit")
			runGit(t, workDir, "push", "origin", tt.defaultBranch)

			// Run the actual git ls-remote command
			cmd := exec.Command("git", "ls-remote", "--symref", "origin", "HEAD")
			cmd.Dir = workDir
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "git ls-remote should succeed, output: %s", string(output))

			// Parse with our function
			result := parseDefaultBranchFromLsRemote(string(output))
			assert.Equal(t, tt.defaultBranch, result, "should parse default branch from real git ls-remote output")

			// Regression checks
			assert.NotEqual(t, "ref:", result, "must not return 'ref:' (regression)")
			assert.False(t, strings.HasPrefix(result, "ref:"), "result must not start with 'ref:'")
			assert.False(t, strings.HasPrefix(result, "refs/"), "result must not start with 'refs/'")
		})
	}
}

// runGit executes a git command in the given directory, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), string(out))
}

// runGitIn executes a git command with no specific dir (or empty string for cwd).
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), string(out))
}

func TestBuildWorkingTreeResolutionOptions(t *testing.T) {
	t.Run("offers overwrite first for overlapping unstaged files", func(t *testing.T) {
		options := buildWorkingTreeResolutionOptions(true)
		require.Len(t, options, 3)
		assert.Equal(t, workingTreeOverwrite, options[0].Value)
		assert.Equal(t, workingTreeCleaned, options[1].Value)
		assert.Equal(t, workingTreeExit, options[2].Value)
	})

	t.Run("does not offer overwrite for staged changes", func(t *testing.T) {
		options := buildWorkingTreeResolutionOptions(false)
		require.Len(t, options, 2)
		assert.Equal(t, workingTreeCleaned, options[0].Value)
		assert.Equal(t, workingTreeExit, options[1].Value)
	})
}

func TestFormatWorkingTreeBlockers(t *testing.T) {
	t.Parallel()
	description := formatWorkingTreeBlockers(addWorkingTreeBlockers{
		staged:      []string{"notes.txt"},
		overlapping: []string{".github/workflows/repo-assist.md", ".github/workflows/repo-assist.lock.yml"},
	})

	assert.Equal(t, "Staged changes:\n  • notes.txt\nChanges overlapping files the wizard will add:\n  • .github/workflows/repo-assist.md\n  • .github/workflows/repo-assist.lock.yml", description)
}

func TestIsAlreadyMergedGHError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "already merged phrase", err: errors.New("GraphQL: Pull request is already merged (mergePullRequest)"), want: true},
		{name: "MERGED state literal", err: errors.New("pull request state is MERGED"), want: true},
		{name: "unrelated error", err: errors.New("network timeout"), want: false},
		{name: "merge failure wording", err: errors.New("GraphQL: Pull request could not be merged (mergePullRequest)"), want: false},
		{name: "not merged wording", err: errors.New("pull request is not merged"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isAlreadyMergedGHError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildMergeOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		mergeFailed   bool
		userReviewing bool
		wantValues    []mergeAction
	}{
		{
			name:          "default options",
			mergeFailed:   false,
			userReviewing: false,
			wantValues:    []mergeAction{mergeActionAttempt, mergeActionReview, mergeActionExit},
		},
		{
			name:          "merge failed adds edit title",
			mergeFailed:   true,
			userReviewing: false,
			wantValues:    []mergeAction{mergeActionAttempt, mergeActionEditTitle, mergeActionReview, mergeActionExit},
		},
		{
			name:          "user reviewing shows confirmation path",
			mergeFailed:   false,
			userReviewing: true,
			wantValues:    []mergeAction{mergeActionAttempt, mergeActionConfirmed, mergeActionExit},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			options := buildMergeOptions(tt.mergeFailed, tt.userReviewing)
			values := make([]mergeAction, 0, len(options))
			for _, opt := range options {
				values = append(values, opt.Value)
			}
			assert.Equal(t, tt.wantValues, values)
		})
	}
}
