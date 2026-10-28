//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLintCommand(t *testing.T) {
	t.Parallel()
	cmd := NewLintCommand()

	require.NotNil(t, cmd, "NewLintCommand should return a non-nil command")
	assert.Equal(t, "lint", cmd.Name(), "Command name should be 'lint'")
	require.NotNil(t, cmd.Flags().Lookup("dir"), "lint command should have a --dir flag")
	assert.Equal(t, "d", cmd.Flags().Lookup("dir").Shorthand, "--dir should have -d shorthand")
	assert.Equal(t,
		"Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)",
		cmd.Flags().Lookup("dir").Usage,
		"--dir usage should describe env override and default",
	)
	require.NotNil(t, cmd.Flags().Lookup("shellcheck"), "lint command should have a --shellcheck flag")
	require.NotNil(t, cmd.Flags().Lookup("pyflakes"), "lint command should have a --pyflakes flag")
	require.NotNil(t, cmd.Flags().Lookup("debug"), "lint command should expose coverage warnings with --debug")
	debug, err := cmd.Flags().GetBool("debug")
	require.NoError(t, err)
	assert.False(t, debug)
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `unknown permission scope "copilot-requests"`,
		"lint command should include built-in ignore for gh-aw permission extension")
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `unknown permission scope "vulnerability-alerts"`,
		"lint command should include built-in ignore for vulnerability-alerts permission scope")
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `unknown permission scope "drives"`,
		"lint command should include built-in ignore for drives permission scope")
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `property "workflow_(repository|sha|ref|file_path)" is not defined in object type`,
		"lint command should include built-in ignore for gh-aw workflow context extensions")
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `unexpected key "queue" for "concurrency" section`,
		"lint command should include built-in ignore for queue concurrency key not yet in actionlint")
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `property "(activation|activated)" is not defined in object type`,
		"lint command should include built-in ignore for gh-aw activation context properties not modeled by actionlint")
	assert.Contains(t, defaultGhAwActionlintIgnorePatterns, `property "artifact_prefix" is not defined in object type`,
		"lint command should include built-in ignore for gh-aw artifact context properties not modeled by actionlint")
}

func TestResolveLockFilesForLint(t *testing.T) {
	tempDir := t.TempDir()
	lockA := filepath.Join(tempDir, "a.lock.yml")
	lockB := filepath.Join(tempDir, "b.lock.yml")
	nonLock := filepath.Join(tempDir, "workflow.txt")

	require.NoError(t, os.WriteFile(lockA, []byte("name: a"), 0o644), "should create lock file a")
	require.NoError(t, os.WriteFile(lockB, []byte("name: b"), 0o644), "should create lock file b")
	require.NoError(t, os.WriteFile(nonLock, []byte("---"), 0o644), "should create non-lock file")

	t.Run("defaults to scanning dir when no args", func(t *testing.T) {
		t.Parallel()
		files, err := resolveLockFilesForLint(nil, tempDir)
		require.NoError(t, err, "should resolve lock files from default dir")
		assert.Equal(t, []string{lockA, lockB}, files, "should return sorted .lock.yml files only")
	})

	t.Run("defaults to GetWorkflowDir when workflow dir is empty", func(t *testing.T) {
		t.Setenv("GH_AW_WORKFLOWS_DIR", tempDir)
		files, err := resolveLockFilesForLint(nil, "")
		require.NoError(t, err, "should resolve lock files from default workflow directory when --dir is empty")
		assert.Equal(t, []string{lockA, lockB}, files, "should fallback to default workflow directory")
	})

	t.Run("accepts explicit lock file path", func(t *testing.T) {
		t.Parallel()
		files, err := resolveLockFilesForLint([]string{lockB}, tempDir)
		require.NoError(t, err, "should accept explicit lock file")
		assert.Equal(t, []string{lockB}, files, "should include explicit lock file")
	})

	t.Run("accepts explicit directory path", func(t *testing.T) {
		t.Parallel()
		files, err := resolveLockFilesForLint([]string{tempDir}, tempDir)
		require.NoError(t, err, "should accept explicit directory")
		assert.Equal(t, []string{lockA, lockB}, files, "should expand directory to lock files")
	})

	t.Run("rejects non lock file path", func(t *testing.T) {
		t.Parallel()
		_, err := resolveLockFilesForLint([]string{nonLock}, tempDir)
		require.Error(t, err, "should reject non-lock file path")
		require.ErrorContains(t, err, "is not a .md, .yml, or .yaml workflow file or directory", "error should explain allowed path types")
	})
}

func TestActionlintCacheModeCompatibility(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		message string
		ignored bool
	}{
		{
			name:    "workflow cache policy",
			message: `unexpected key "cache-mode" for "workflow" section. expected one of "concurrency", "defaults", "env", "jobs", "name", "on", "permissions", "run-name"`,
			ignored: true,
		},
		{
			name:    "workflow cache policy without expected keys",
			message: `unexpected key "cache-mode" for "workflow" section`,
			ignored: true,
		},
		{
			name:    "job cache policy",
			message: `unexpected key "cache-mode" for "job" section. expected one of "concurrency", "container", "defaults", "env", "environment"`,
			ignored: true,
		},
		{
			name:    "job cache policy without expected keys",
			message: `unexpected key "cache-mode" for "job" section`,
			ignored: true,
		},
		{
			name:    "step cache policy",
			message: `unexpected key "cache-mode" for "step" section`,
		},
		{
			name:    "run step cache policy",
			message: `unexpected key "cache-mode" for step to run shell command. expected one of "continue-on-error", "env", "id", "if", "name", "run", "shell", "timeout-minutes", "working-directory"`,
		},
		{
			name:    "misspelled workflow key",
			message: `unexpected key "cache-modes" for "workflow" section`,
		},
		{
			name:    "misspelled job key",
			message: `unexpected key "cache-modes" for "job" section`,
		},
		{
			name:    "invalid job section suffix",
			message: `unexpected key "cache-mode" for "job" sectional`,
		},
		{
			name:    "other workflow key",
			message: `unexpected key "unknown-key" for "workflow" section`,
		},
		{
			name:    "invalid cache policy value",
			message: `expected scalar node for "cache-mode" but found mapping node`,
		},
		{
			name:    "other diagnostic mentioning the key",
			message: `invalid expression: unexpected key "cache-mode" for "workflow" section`,
		},
		{
			name:    "other diagnostic mentioning the job key",
			message: `invalid expression: unexpected key "cache-mode" for "job" section`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ignored := false
			for _, pattern := range defaultGhAwActionlintIgnorePatterns {
				matched, err := regexp.MatchString(pattern, tt.message)
				require.NoError(t, err)
				ignored = ignored || matched
			}
			assert.Equal(t, tt.ignored, ignored)
		})
	}
}
