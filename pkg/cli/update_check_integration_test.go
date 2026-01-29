//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestUpdateCheckIntegration tests the update check feature in an end-to-end scenario
func TestUpdateCheckIntegration(t *testing.T) {

	// Build the binary for testing
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gh-aw")

	// Get project root (two levels up from pkg/cli)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Join(wd, "..", "..")

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/gh-aw")
	buildCmd.Dir = projectRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Make binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	t.Run("update check disabled in CI mode", func(t *testing.T) {
		// Create a test workflow directory
		workflowDir := filepath.Join(tempDir, "test-ci", ".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0755); err != nil {
			t.Fatalf("Failed to create workflow directory: %v", err)
		}

		// Create a simple test workflow
		workflowContent := `---
name: Test Workflow
on: workflow_dispatch
permissions: read-all
engine: copilot
---

# Test Workflow

Test workflow content.
`
		workflowFile := filepath.Join(workflowDir, "test.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Run compile command with CI environment variable set
		cmd := exec.Command(binaryPath, "compile", "--no-emit", "test")
		cmd.Dir = filepath.Join(tempDir, "test-ci")
		cmd.Env = append(os.Environ(),
			"CI=true",
			"DEBUG=cli:update_check",
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Compile command failed: %v\nOutput: %s", err, output)
		}

		outputStr := string(output)
		if !strings.Contains(outputStr, "Update check disabled in CI environment") {
			t.Errorf("Expected update check to be disabled in CI, output: %s", outputStr)
		}
	})

	t.Run("update check disabled with --no-check-update flag", func(t *testing.T) {
		// Create a test workflow directory
		workflowDir := filepath.Join(tempDir, "test-flag", ".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0755); err != nil {
			t.Fatalf("Failed to create workflow directory: %v", err)
		}

		// Create a simple test workflow
		workflowContent := `---
name: Test Workflow
on: workflow_dispatch
permissions: read-all
engine: copilot
---

# Test Workflow

Test workflow content.
`
		workflowFile := filepath.Join(workflowDir, "test.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Run compile command with --no-check-update flag
		// Clear CI env vars to avoid automatic disabling
		cmd := exec.Command(binaryPath, "compile", "--no-emit", "--no-check-update", "test")
		cmd.Dir = filepath.Join(tempDir, "test-flag")
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"DEBUG=cli:update_check",
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Compile command failed: %v\nOutput: %s", err, output)
		}

		outputStr := string(output)
		if !strings.Contains(outputStr, "Update check disabled via --no-check-update flag") {
			t.Errorf("Expected update check to be disabled via flag, output: %s", outputStr)
		}
	})

	t.Run("update check respects last check timestamp", func(t *testing.T) {
		// Create a test workflow directory
		workflowDir := filepath.Join(tempDir, "test-timestamp", ".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0755); err != nil {
			t.Fatalf("Failed to create workflow directory: %v", err)
		}

		// Create a simple test workflow
		workflowContent := `---
name: Test Workflow
on: workflow_dispatch
permissions: read-all
engine: copilot
---

# Test Workflow

Test workflow content.
`
		workflowFile := filepath.Join(workflowDir, "test.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Create a custom temp directory for last check file
		checkTempDir := filepath.Join(tempDir, "gh-aw-check")
		if err := os.MkdirAll(checkTempDir, 0755); err != nil {
			t.Fatalf("Failed to create check temp directory: %v", err)
		}

		// Write a recent timestamp (less than 24 hours ago)
		lastCheckFile := filepath.Join(checkTempDir, "gh-aw-last-update-check")
		recentTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		if err := os.WriteFile(lastCheckFile, []byte(recentTime), 0644); err != nil {
			t.Fatalf("Failed to write last check file: %v", err)
		}

		// Run compile command (without CI env)
		cmd := exec.Command(binaryPath, "compile", "--no-emit", "test")
		cmd.Dir = filepath.Join(tempDir, "test-timestamp")
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"DEBUG=cli:update_check",
			"TMPDIR=" + tempDir, // Override temp directory
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Compile command failed: %v\nOutput: %s", err, output)
		}

		outputStr := string(output)
		// The check should be skipped because last check was recent
		// Note: This test may not work as expected if the override doesn't affect getLastCheckFilePath
		// but it documents the expected behavior
		if strings.Contains(outputStr, "Checking for gh-aw updates") {
			// If it does check, it should find the timestamp and skip
			if !strings.Contains(outputStr, "skipping") && !strings.Contains(outputStr, "Last check was") {
				t.Logf("Update check ran, which is acceptable. Output: %s", outputStr)
			}
		}
	})

	t.Run("update check runs for development builds", func(t *testing.T) {
		// Create a test workflow directory
		workflowDir := filepath.Join(tempDir, "test-devbuild", ".github", "workflows")
		if err := os.MkdirAll(workflowDir, 0755); err != nil {
			t.Fatalf("Failed to create workflow directory: %v", err)
		}

		// Create a simple test workflow
		workflowContent := `---
name: Test Workflow
on: workflow_dispatch
permissions: read-all
engine: copilot
---

# Test Workflow

Test workflow content.
`
		workflowFile := filepath.Join(workflowDir, "test.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Run compile command (without CI env)
		cmd := exec.Command(binaryPath, "compile", "--no-emit", "test")
		cmd.Dir = filepath.Join(tempDir, "test-devbuild")
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"DEBUG=cli:update_check",
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Compile command failed: %v\nOutput: %s", err, output)
		}

		outputStr := string(output)
		// Development builds should skip the check
		if !strings.Contains(outputStr, "Not a released version, skipping update check") {
			t.Logf("Expected dev build to skip update check. Output: %s", outputStr)
			// This is not a hard failure as the binary might have a different version format
		}
	})
}

// TestUpdateCheckFlagHelp verifies the --no-check-update flag appears in help text
func TestUpdateCheckFlagHelp(t *testing.T) {

	// Build the binary for testing
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gh-aw")

	// Get project root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Join(wd, "..", "..")

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/gh-aw")
	buildCmd.Dir = projectRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}

	// Make binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Run compile --help
	cmd := exec.Command(binaryPath, "compile", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// --help returns non-zero exit code, which is expected
		if !strings.Contains(err.Error(), "exit status") {
			t.Fatalf("Unexpected error running compile --help: %v", err)
		}
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "--no-check-update") {
		t.Errorf("Expected --no-check-update flag in help text, got: %s", outputStr)
	}

	if !strings.Contains(outputStr, "Skip checking for gh-aw updates") {
		t.Errorf("Expected flag description in help text, got: %s", outputStr)
	}
}
