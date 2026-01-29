//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetDefaultBranch tests the getDefaultBranch function
func TestGetDefaultBranch(t *testing.T) {
	t.Run("no remote configured", func(t *testing.T) {
		// Create a temporary directory for test
		tmpDir := t.TempDir()
		originalDir, _ := os.Getwd()
		defer os.Chdir(originalDir)

		// Initialize git repository without remote
		os.Chdir(tmpDir)
		exec.Command("git", "init").Run()
		exec.Command("git", "config", "user.email", "test@example.com").Run()
		exec.Command("git", "config", "user.name", "Test User").Run()

		// Should fail because no remote is configured
		_, err := getDefaultBranch()
		require.Error(t, err, "Should fail when no remote is configured")
		assert.Contains(t, err.Error(), "no remote repository configured")
	})
}

// TestCheckOnDefaultBranch tests the checkOnDefaultBranch function
func TestCheckOnDefaultBranch(t *testing.T) {
	t.Run("no remote configured - should fail", func(t *testing.T) {
		// Create a temporary directory for test
		tmpDir := t.TempDir()
		originalDir, _ := os.Getwd()
		defer os.Chdir(originalDir)

		// Initialize git repository without remote
		os.Chdir(tmpDir)
		exec.Command("git", "init").Run()
		exec.Command("git", "config", "user.email", "test@example.com").Run()
		exec.Command("git", "config", "user.name", "Test User").Run()

		// Create an initial commit
		testFile := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(testFile, []byte("test"), 0644)
		require.NoError(t, err)
		exec.Command("git", "add", "test.txt").Run()
		exec.Command("git", "commit", "-m", "initial commit").Run()

		// Should fail when no remote is configured
		err = checkOnDefaultBranch(false)
		require.Error(t, err, "Should fail when no remote is configured")
		assert.Contains(t, err.Error(), "--push requires a remote repository to be configured")
	})
}

// TestConfirmPushOperation tests the confirmPushOperation function
func TestConfirmPushOperation(t *testing.T) {
	t.Run("skips confirmation in CI", func(t *testing.T) {
		// Set CI environment variable
		origCI := os.Getenv("CI")
		os.Setenv("CI", "true")
		defer func() {
			if origCI == "" {
				os.Unsetenv("CI")
			} else {
				os.Setenv("CI", origCI)
			}
		}()

		// Should succeed without prompting user
		err := confirmPushOperation(false)
		assert.NoError(t, err, "Should skip confirmation in CI")
	})

	// Note: Testing the interactive prompt outside CI is not feasible in automated tests
	// as it requires user interaction. The function behavior in non-CI environments
	// should be tested manually.
}
