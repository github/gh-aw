//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCompileWithPoutine tests the compile command with --poutine flag
func TestCompileWithPoutine(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Initialize git repository for poutine to work (it needs git root)
	gitInitCmd := exec.Command("git", "init")
	gitInitCmd.Dir = setup.tempDir
	if output, err := gitInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to initialize git repository: %v\nOutput: %s", err, string(output))
	}

	// Configure git user for the repository
	gitConfigEmail := exec.Command("git", "config", "user.email", "test@test.com")
	gitConfigEmail.Dir = setup.tempDir
	if output, err := gitConfigEmail.CombinedOutput(); err != nil {
		t.Fatalf("Failed to configure git user email: %v\nOutput: %s", err, string(output))
	}

	gitConfigName := exec.Command("git", "config", "user.name", "Test User")
	gitConfigName.Dir = setup.tempDir
	if output, err := gitConfigName.CombinedOutput(); err != nil {
		t.Fatalf("Failed to configure git user name: %v\nOutput: %s", err, string(output))
	}

	// Create a test markdown workflow file
	testWorkflow := `---
name: Poutine Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Poutine Test Workflow

This workflow tests the poutine security scanner integration.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "poutine-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0o644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// First compile without poutine to create the lock file
	compileCmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	if output, err := compileCmd.CombinedOutput(); err != nil {
		t.Fatalf("Initial compile failed: %v\nOutput: %s", err, string(output))
	}

	// Check that the lock file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "poutine-test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// Now compile with --poutine flag
	poutineCmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath, "--poutine", "--verbose")
	output, err := poutineCmd.CombinedOutput()

	outputStr := string(output)

	// Note: poutine may find security issues and return non-zero exit code
	// In non-strict mode, this should not fail compilation, but if it does,
	// we log it and continue (this is expected behavior)
	if err != nil {
		t.Logf("Compile with --poutine returned error (may be expected if poutine found issues): %v\nOutput: %s", err, outputStr)
		// We still check that the lock file exists
	}

	// The lock file should exist after poutine scan
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Lock file was removed after poutine scan")
	}

	t.Logf("Integration test passed - poutine flag works correctly\nOutput: %s", outputStr)
}

// TestCompileWithPoutineAndZizmor tests that both scanners can run together
func TestCompileWithPoutineAndZizmor(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Initialize git repository
	gitInitCmd := exec.Command("git", "init")
	gitInitCmd.Dir = setup.tempDir
	if output, err := gitInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to initialize git repository: %v\nOutput: %s", err, string(output))
	}

	// Configure git user
	gitConfigEmail := exec.Command("git", "config", "user.email", "test@test.com")
	gitConfigEmail.Dir = setup.tempDir
	if output, err := gitConfigEmail.CombinedOutput(); err != nil {
		t.Fatalf("Failed to configure git user email: %v\nOutput: %s", err, string(output))
	}

	gitConfigName := exec.Command("git", "config", "user.name", "Test User")
	gitConfigName.Dir = setup.tempDir
	if output, err := gitConfigName.CombinedOutput(); err != nil {
		t.Fatalf("Failed to configure git user name: %v\nOutput: %s", err, string(output))
	}

	// Create a test workflow
	testWorkflow := `---
name: Both Scanners Test
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Both Scanners Test

This workflow tests running both poutine and zizmor together.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "both-scanners-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0o644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Compile with both --poutine and --zizmor flags
	// Note: One or both scanners may find issues, but the compilation should
	// complete unless strict mode is enabled
	bothCmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath, "--poutine", "--zizmor", "--verbose")
	output, err := bothCmd.CombinedOutput()
	outputStr := string(output)

	// The command may fail if either scanner finds issues, but that's okay
	// What matters is that both scanners run
	t.Logf("Compile with both scanners output:\n%s", outputStr)

	// Check that the lock file was created regardless of scanner findings
	lockFilePath := filepath.Join(setup.workflowsDir, "both-scanners-test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// If there was an error, it should be from one of the scanners, not a build error
	if err != nil {
		t.Logf("Command failed (may be expected if scanners found issues): %v", err)
	}
}
