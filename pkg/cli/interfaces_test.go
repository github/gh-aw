//go:build !integration

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandProviderInterface verifies that *cobra.Command satisfies the CommandProvider interface
//
// NOTE: this test intentionally does NOT call t.Parallel(). Cobra's bash completion
// generator (prepareCustomAnnotationsForFlags in bash_completions.go) mutates a
// shared map that is not safe for concurrent use across different *cobra.Command
// trees. Running this alongside other completion-generating tests (e.g.
// TestInstallShellCompletion_TypeAssertion) in parallel can trigger a
// "fatal error: concurrent map writes" crash in the Go runtime.
func TestCommandProviderInterface(t *testing.T) {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test command",
	}

	// Verify the type assertion works
	var provider CommandProvider = cmd
	assert.NotNil(t, provider, "cobra.Command should satisfy CommandProvider interface")

	// Test that we can call the interface methods
	t.Run("GenBashCompletion", func(t *testing.T) {
		var buf bytes.Buffer
		err := provider.GenBashCompletion(&buf)
		require.NoError(t, err, "GenBashCompletion should not error")
		assert.NotEmpty(t, buf.String(), "GenBashCompletion should generate content")
	})

	t.Run("GenZshCompletion", func(t *testing.T) {
		var buf bytes.Buffer
		err := provider.GenZshCompletion(&buf)
		require.NoError(t, err, "GenZshCompletion should not error")
		assert.NotEmpty(t, buf.String(), "GenZshCompletion should generate content")
	})

	t.Run("GenFishCompletion", func(t *testing.T) {
		var buf bytes.Buffer
		err := provider.GenFishCompletion(&buf, true)
		require.NoError(t, err, "GenFishCompletion should not error")
		assert.NotEmpty(t, buf.String(), "GenFishCompletion should generate content")
	})
}

// TestInitRepository_WithNilRootCmd verifies that InitRepository handles nil rootCmd gracefully
func TestInitRepository_WithNilRootCmd(t *testing.T) {
	// Create a temporary git repository
	tempDir := testutil.TempDir(t, "test-*")

	// Change to the temp directory
	oldWd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	require.NoError(t, err, "Failed to change directory")

	// Initialize git repo
	err = exec.Command("git", "init").Run()
	require.NoError(t, err, "Failed to init git repo")

	// InitRepository with nil rootCmd and completions disabled should succeed
	err = InitRepository(InitOptions{Verbose: false, Skill: true, Agent: true, MCP: false, CodespaceRepos: []string{}, CodespaceEnabled: false, Completions: false, CreatePR: false, RootCmd: nil})
	require.NoError(t, err, "InitRepository with nil rootCmd should succeed when completions are disabled")
}

// TestInitRepository_WithRootCmd verifies that InitRepository works with a real rootCmd
func TestInitRepository_WithRootCmd(t *testing.T) {
	// Create a temporary git repository
	tempDir := testutil.TempDir(t, "test-*")

	// Change to the temp directory
	oldWd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	require.NoError(t, err, "Failed to change directory")

	// Initialize git repo
	err = exec.Command("git", "init").Run()
	require.NoError(t, err, "Failed to init git repo")

	// Create a cobra command to use as rootCmd
	rootCmd := &cobra.Command{
		Use:   "gh-aw",
		Short: "GitHub Agentic Workflows",
	}

	// InitRepository with real rootCmd should succeed
	err = InitRepository(InitOptions{Verbose: false, Skill: true, Agent: true, MCP: false, CodespaceRepos: []string{}, CodespaceEnabled: false, Completions: false, CreatePR: false, RootCmd: rootCmd})
	require.NoError(t, err, "InitRepository with rootCmd should succeed")
}

// TestInstallShellCompletion_TypeAssertion verifies type assertion behavior
//
// NOTE: this test intentionally does NOT call t.Parallel() (see the comment on
// TestCommandProviderInterface) since it also generates shell completions via
// cobra, which is unsafe to do concurrently with other completion-generating tests.
func TestInstallShellCompletion_TypeAssertion(t *testing.T) {
	t.Run("with cobra.Command", func(t *testing.T) {
		rootCmd := &cobra.Command{
			Use:   "gh-aw",
			Short: "GitHub Agentic Workflows",
		}

		// Should type assert successfully
		// Note: This test will fail in the DetectShell() call if SHELL env is not set,
		// but that's expected and tests the type assertion path
		err := InstallShellCompletion(false, rootCmd)
		// We expect either success or a shell detection error, not a type assertion error
		if err != nil {
			assert.NotContains(t, err.Error(), "must be a *cobra.Command",
				"Should not fail on type assertion with cobra.Command")
		}
	})

	t.Run("with nil", func(t *testing.T) {
		// Should fail type assertion
		err := InstallShellCompletion(false, nil)
		require.Error(t, err, "Should fail with nil rootCmd")
		require.ErrorContains(t, err, "should be a *cobra.Command",
			"Should fail with type assertion error message")
	})
}
