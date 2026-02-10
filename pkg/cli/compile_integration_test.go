//go:build integration

package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/github/gh-aw/pkg/cli/fileutil"
)

// Global binary path shared across all integration tests
var (
	globalBinaryPath string
	projectRoot      string
)

// TestMain builds the gh-aw binary once before running tests
func TestMain(m *testing.M) {
	// Get project root
	wd, err := os.Getwd()
	if err != nil {
		panic("Failed to get current working directory: " + err.Error())
	}
	projectRoot = filepath.Join(wd, "..", "..")

	// Create temp directory for the shared binary
	tempDir, err := os.MkdirTemp("", "gh-aw-integration-binary-*")
	if err != nil {
		panic("Failed to create temp directory for binary: " + err.Error())
	}

	globalBinaryPath = filepath.Join(tempDir, "gh-aw")

	// Build the gh-aw binary
	buildCmd := exec.Command("make", "build")
	buildCmd.Dir = projectRoot
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		panic("Failed to build gh-aw binary: " + err.Error())
	}

	// Copy binary to temp directory
	srcBinary := filepath.Join(projectRoot, "gh-aw")
	if err := fileutil.CopyFile(srcBinary, globalBinaryPath); err != nil {
		panic("Failed to copy gh-aw binary to temp directory: " + err.Error())
	}

	// Make the binary executable
	if err := os.Chmod(globalBinaryPath, 0755); err != nil {
		panic("Failed to make binary executable: " + err.Error())
	}

	// Run the tests
	code := m.Run()

	// Clean up the shared binary directory
	if globalBinaryPath != "" {
		os.RemoveAll(filepath.Dir(globalBinaryPath))
	}

	// Clean up any action cache files created during tests
	// Tests may create .github/aw/actions-lock.json in the pkg/cli directory
	actionCacheDir := filepath.Join(wd, ".github")
	if _, err := os.Stat(actionCacheDir); err == nil {
		_ = os.RemoveAll(actionCacheDir)
	}

	os.Exit(code)
}

// integrationTestSetup holds the setup state for integration tests
type integrationTestSetup struct {
	tempDir      string
	originalWd   string
	binaryPath   string
	workflowsDir string
	cleanup      func()
}

// setupIntegrationTest creates a temporary directory and uses the pre-built gh-aw binary
// This is the equivalent of @Before in Java - common setup for all integration tests
func setupIntegrationTest(t *testing.T) *integrationTestSetup {
	t.Helper()

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "gh-aw-compile-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Save current working directory and change to temp directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Copy the pre-built binary to this test's temp directory
	binaryPath := filepath.Join(tempDir, "gh-aw")
	if err := fileutil.CopyFile(globalBinaryPath, binaryPath); err != nil {
		t.Fatalf("Failed to copy gh-aw binary to temp directory: %v", err)
	}

	// Make the binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := ".github/workflows"
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Setup cleanup function
	cleanup := func() {
		err := os.Chdir(originalWd)
		if err != nil {
			t.Fatalf("Failed to change back to original working directory: %v", err)
		}
		err = os.RemoveAll(tempDir)
		if err != nil {
			t.Fatalf("Failed to remove temp directory: %v", err)
		}
	}

	return &integrationTestSetup{
		tempDir:      tempDir,
		originalWd:   originalWd,
		binaryPath:   binaryPath,
		workflowsDir: workflowsDir,
		cleanup:      cleanup,
	}
}

// TestCompileIntegration tests the compile command by executing the gh-aw CLI binary
func TestCompileIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test markdown workflow file
	testWorkflow := `---
name: Integration Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Integration Test Workflow

This is a simple integration test workflow.

Please check the repository for any open issues and create a summary.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Run the compile command
	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	// Check that the compiled .lock.yml file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// Read and verify the generated lock file contains expected content
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)
	if !strings.Contains(lockContentStr, "name: \"Integration Test Workflow\"") {
		t.Errorf("Lock file should contain the workflow name")
	}

	if !strings.Contains(lockContentStr, "workflow_dispatch") {
		t.Errorf("Lock file should contain the trigger event")
	}

	if !strings.Contains(lockContentStr, "jobs:") {
		t.Errorf("Lock file should contain jobs section")
	}

	t.Logf("Integration test passed - successfully compiled workflow to %s", lockFilePath)
}

func TestCompileWithIncludeWithEmptyFrontmatterUnderPty(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create an include file
	includeContent := `---
---
# Included Workflow

This is an included workflow file.
`
	includeFile := filepath.Join(setup.workflowsDir, "include.md")
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		t.Fatalf("Failed to write include file: %v", err)
	}

	// Create a test markdown workflow file
	testWorkflow := `---
name: Integration Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Integration Test Workflow

This is a simple integration test workflow.

Please check the repository for any open issues and create a summary.

@include include.md
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Run the compile command
	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	// Start the command with a TTY attached to stdin/stdout/stderr
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start PTY: %v", err)
	}
	defer func() { _ = ptmx.Close() }() // Best effort

	// Capture all output from the PTY
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, ptmx) // reads both stdout/stderr via the PTY
		close(done)
	}()

	// Wait for the process to finish
	err = cmd.Wait()

	// Ensure reader goroutine drains remaining output
	select {
	case <-done:
	case <-time.After(750 * time.Millisecond):
	}

	output := buf.String()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput:\n%s", err, output)
	}

	// Check that the compiled .lock.yml file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// Read and verify the generated lock file contains expected content
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)
	if !strings.Contains(lockContentStr, "name: \"Integration Test Workflow\"") {
		t.Errorf("Lock file should contain the workflow name")
	}

	if !strings.Contains(lockContentStr, "workflow_dispatch") {
		t.Errorf("Lock file should contain the trigger event")
	}

	if !strings.Contains(lockContentStr, "jobs:") {
		t.Errorf("Lock file should contain jobs section")
	}

	if strings.Contains(lockContentStr, "\x1b[") {
		t.Errorf("Lock file must not contain color escape sequences")
	}

	t.Logf("Integration test passed - successfully compiled workflow to %s", lockFilePath)
}

// TestCompileWithZizmor tests the compile command with --zizmor flag
func TestCompileWithZizmor(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Initialize git repository for zizmor to work (it needs git root)
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
name: Zizmor Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Zizmor Test Workflow

This workflow tests the zizmor security scanner integration.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "zizmor-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// First compile without zizmor to create the lock file
	compileCmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	if output, err := compileCmd.CombinedOutput(); err != nil {
		t.Fatalf("Initial compile failed: %v\nOutput: %s", err, string(output))
	}

	// Check that the lock file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "zizmor-test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// Now compile with --zizmor flag
	zizmorCmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath, "--zizmor", "--verbose")
	output, err := zizmorCmd.CombinedOutput()

	// The command should succeed even if zizmor finds issues
	if err != nil {
		t.Fatalf("Compile with --zizmor failed: %v\nOutput: %s", err, string(output))
	}

	outputStr := string(output)

	// Note: With the new behavior, if there are 0 warnings, no zizmor output is displayed
	// The test just verifies that the command succeeds with --zizmor flag
	// If there are warnings, they will be shown in the format:
	// "🌈 zizmor X warnings in <filepath>"
	//   - [Severity] finding-type

	// The lock file should still exist after zizmor scan
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Lock file was removed after zizmor scan")
	}

	t.Logf("Integration test passed - zizmor flag works correctly\nOutput: %s", outputStr)
}

// TestCompileWithFuzzyDailySchedule tests compilation of workflows with fuzzy "daily" schedule
func TestCompileWithFuzzyDailySchedule(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test markdown workflow file with fuzzy daily schedule
	testWorkflow := `---
name: Fuzzy Daily Schedule Test
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Fuzzy Daily Schedule Test

This workflow tests fuzzy daily schedule compilation.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "daily-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Run the compile command
	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	// Check that the compiled .lock.yml file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "daily-test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// Read and verify the generated lock file contains expected content
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that the schedule was processed (should contain "schedule:" section)
	if !strings.Contains(lockContentStr, "schedule:") {
		t.Errorf("Lock file should contain schedule section")
	}

	// Verify that the cron expression is valid (5 fields)
	// The fuzzy schedule should have been scattered to a concrete cron expression
	lines := strings.Split(lockContentStr, "\n")
	foundCron := false
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		// Skip comment lines
		if strings.HasPrefix(trimmedLine, "#") {
			continue
		}
		if strings.Contains(line, "cron:") {
			foundCron = true
			// Extract the cron value
			cronLine := strings.TrimSpace(line)
			// Should look like: - cron: "0 14 * * *"
			if !strings.Contains(cronLine, "cron:") {
				continue
			}

			// Verify it's not still in fuzzy format
			if strings.Contains(cronLine, "FUZZY:") {
				t.Errorf("Lock file should not contain FUZZY: schedule, but got: %s", cronLine)
			}

			// Extract and validate cron expression format
			cronParts := strings.Split(cronLine, "\"")
			if len(cronParts) >= 2 {
				cronExpr := cronParts[1]
				fields := strings.Fields(cronExpr)
				if len(fields) != 5 {
					t.Errorf("Cron expression should have 5 fields, got %d: %s", len(fields), cronExpr)
				}

				// Verify it's a daily pattern (minute hour * * *)
				if fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
					t.Errorf("Expected daily pattern (minute hour * * *), got: %s", cronExpr)
				}

				t.Logf("Successfully compiled fuzzy daily schedule to: %s", cronExpr)
			}
			break
		}
	}

	if !foundCron {
		t.Errorf("Could not find cron expression in lock file")
	}

	// Verify workflow name is present
	if !strings.Contains(lockContentStr, "name: \"Fuzzy Daily Schedule Test\"") {
		t.Errorf("Lock file should contain the workflow name")
	}

	t.Logf("Integration test passed - successfully compiled fuzzy daily schedule to %s", lockFilePath)
}

// TestCompileWithFuzzyDailyScheduleDeterministic tests that fuzzy daily schedule compilation is deterministic
func TestCompileWithFuzzyDailyScheduleDeterministic(t *testing.T) {
	// Create a single test setup to ensure same directory structure
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Compile the same workflow twice and verify the results are identical
	results := make([]string, 2)

	for i := 0; i < 2; i++ {
		// Create a test markdown workflow file with fuzzy daily schedule
		testWorkflow := `---
name: Deterministic Daily Test
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Deterministic Daily Test

This workflow tests deterministic fuzzy daily schedule compilation.
`

		testWorkflowPath := filepath.Join(setup.workflowsDir, "deterministic-daily.md")
		if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
			t.Fatalf("Failed to write test workflow file: %v", err)
		}

		// Run the compile command
		cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("CLI compile command failed (attempt %d): %v\nOutput: %s", i+1, err, string(output))
		}

		// Read the generated lock file
		lockFilePath := filepath.Join(setup.workflowsDir, "deterministic-daily.lock.yml")
		lockContent, err := os.ReadFile(lockFilePath)
		if err != nil {
			t.Fatalf("Failed to read lock file (attempt %d): %v", i+1, err)
		}

		results[i] = string(lockContent)

		// Delete the lock file before next iteration to force recompilation
		if i == 0 {
			if err := os.Remove(lockFilePath); err != nil {
				t.Fatalf("Failed to remove lock file between compilations: %v", err)
			}
		}
	}

	// Compare the two results
	if results[0] != results[1] {
		// Extract just the cron lines for better comparison
		extractCron := func(content string) string {
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				if strings.Contains(line, "cron:") {
					return strings.TrimSpace(line)
				}
			}
			return ""
		}

		cron1 := extractCron(results[0])
		cron2 := extractCron(results[1])

		if cron1 != cron2 {
			t.Errorf("Fuzzy daily schedule compilation is not deterministic.\nFirst cron: %s\nSecond cron: %s", cron1, cron2)
		} else {
			t.Logf("Fuzzy daily schedule compilation is deterministic (cron: %s)", cron1)
		}
	} else {
		t.Logf("Fuzzy daily schedule compilation is deterministic (results are identical)")
	}
}

// TestCompileWithFuzzyDailyScheduleArrayFormat tests compilation of workflows with fuzzy "daily" schedule in array format
func TestCompileWithFuzzyDailyScheduleArrayFormat(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test markdown workflow file with fuzzy daily schedule in array format
	testWorkflow := `---
name: Fuzzy Daily Schedule Array Format Test
on:
  schedule:
    - cron: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Fuzzy Daily Schedule Array Format Test

This workflow tests fuzzy daily schedule compilation using array format with cron field.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "daily-array-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Run the compile command
	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	// Check that the compiled .lock.yml file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "daily-array-test.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	// Read and verify the generated lock file contains expected content
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that the schedule was processed (should contain "schedule:" section)
	if !strings.Contains(lockContentStr, "schedule:") {
		t.Errorf("Lock file should contain schedule section")
	}

	// Verify that the cron expression is valid (5 fields)
	// The fuzzy schedule should have been scattered to a concrete cron expression
	lines := strings.Split(lockContentStr, "\n")
	foundCron := false
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		// Skip comment lines
		if strings.HasPrefix(trimmedLine, "#") {
			continue
		}
		if strings.Contains(line, "cron:") {
			foundCron = true
			// Extract the cron value
			cronLine := strings.TrimSpace(line)
			// Should look like: - cron: "0 14 * * *"
			if !strings.Contains(cronLine, "cron:") {
				continue
			}

			// Verify it's not still in fuzzy format
			if strings.Contains(cronLine, "FUZZY:") {
				t.Errorf("Lock file should not contain FUZZY: schedule, but got: %s", cronLine)
			}

			// Extract and validate cron expression format
			cronParts := strings.Split(cronLine, "\"")
			if len(cronParts) >= 2 {
				cronExpr := cronParts[1]
				fields := strings.Fields(cronExpr)
				if len(fields) != 5 {
					t.Errorf("Cron expression should have 5 fields, got %d: %s", len(fields), cronExpr)
				}

				// Verify it's a daily pattern (minute hour * * *)
				if fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
					t.Errorf("Expected daily pattern (minute hour * * *), got: %s", cronExpr)
				}

				t.Logf("Successfully compiled fuzzy daily schedule (array format) to: %s", cronExpr)
			}
			break
		}
	}

	if !foundCron {
		t.Errorf("Could not find cron expression in lock file")
	}

	// Verify workflow name is present
	if !strings.Contains(lockContentStr, "name: \"Fuzzy Daily Schedule Array Format Test\"") {
		t.Errorf("Lock file should contain the workflow name")
	}

	t.Logf("Integration test passed - successfully compiled fuzzy daily schedule (array format) to %s", lockFilePath)
}

// TestCompileWithInvalidSchedule tests that compilation fails with an invalid schedule string
func TestCompileWithInvalidSchedule(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test markdown workflow file with an invalid schedule
	testWorkflow := `---
name: Invalid Schedule Test
on:
  schedule: invalid schedule format
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Invalid Schedule Test

This workflow tests that invalid schedule strings fail compilation.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "invalid-schedule-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Run the compile command - expect it to fail
	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()

	// The command should fail with an error
	if err == nil {
		t.Fatalf("Expected compile to fail with invalid schedule, but it succeeded\nOutput: %s", string(output))
	}

	outputStr := string(output)

	// Verify the error message contains information about invalid schedule
	if !strings.Contains(outputStr, "schedule") && !strings.Contains(outputStr, "trigger") {
		t.Errorf("Expected error output to mention 'schedule' or 'trigger', got: %s", outputStr)
	}

	// Verify no lock file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "invalid-schedule-test.lock.yml")
	if _, err := os.Stat(lockFilePath); err == nil {
		t.Errorf("Lock file should not be created for invalid workflow, but %s exists", lockFilePath)
	}

	t.Logf("Integration test passed - invalid schedule correctly failed compilation\nOutput: %s", outputStr)
}

// TestCompileWithInvalidScheduleArrayFormat tests that compilation fails with an invalid schedule in array format
func TestCompileWithInvalidScheduleArrayFormat(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test markdown workflow file with an invalid schedule in array format
	testWorkflow := `---
name: Invalid Schedule Array Format Test
on:
  schedule:
    - cron: totally invalid cron here
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Invalid Schedule Array Format Test

This workflow tests that invalid schedule strings in array format fail compilation.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "invalid-schedule-array-test.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Run the compile command - expect it to fail
	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()

	// The command should fail with an error
	if err == nil {
		t.Fatalf("Expected compile to fail with invalid schedule, but it succeeded\nOutput: %s", string(output))
	}

	outputStr := string(output)

	// Verify the error message contains information about invalid schedule
	if !strings.Contains(outputStr, "schedule") && !strings.Contains(outputStr, "cron") {
		t.Errorf("Expected error output to mention 'schedule' or 'cron', got: %s", outputStr)
	}

	// Verify no lock file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "invalid-schedule-array-test.lock.yml")
	if _, err := os.Stat(lockFilePath); err == nil {
		t.Errorf("Lock file should not be created for invalid workflow, but %s exists", lockFilePath)
	}

	t.Logf("Integration test passed - invalid schedule in array format correctly failed compilation\nOutput: %s", outputStr)
}

// TestCompileFromSubdirectoryCreatesActionsLockAtRoot tests that actions-lock.json
// is created at the repository root when compiling from a subdirectory
func TestCompileFromSubdirectoryCreatesActionsLockAtRoot(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Initialize git repository (required for git root detection)
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

	// Create a simple test workflow
	// Note: actions-lock.json is only created when actions need to be pinned,
	// so it may or may not exist. The test verifies it's NOT created in the wrong location.
	testWorkflow := `---
name: Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Test Workflow

Test workflow to verify actions-lock.json path handling when compiling from subdirectory.
`

	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-action.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Change to the .github/workflows subdirectory
	if err := os.Chdir(setup.workflowsDir); err != nil {
		t.Fatalf("Failed to change to workflows subdirectory: %v", err)
	}

	// Run the compile command from the subdirectory using a relative path
	cmd := exec.Command(setup.binaryPath, "compile", "test-action.md")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	// Change back to the temp directory root
	if err := os.Chdir(setup.tempDir); err != nil {
		t.Fatalf("Failed to change back to temp directory: %v", err)
	}

	// Verify actions-lock.json is created at the repository root (.github/aw/actions-lock.json)
	// NOT at .github/workflows/.github/aw/actions-lock.json
	expectedLockPath := filepath.Join(setup.tempDir, ".github", "aw", "actions-lock.json")
	wrongLockPath := filepath.Join(setup.workflowsDir, ".github", "aw", "actions-lock.json")

	// Check if actions-lock.json exists (it may or may not, depending on whether actions were pinned)
	// The important part is that if it exists, it's in the right place
	if _, err := os.Stat(expectedLockPath); err == nil {
		t.Logf("actions-lock.json correctly created at repo root: %s", expectedLockPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("Failed to check for actions-lock.json at expected path: %v", err)
	}

	// Verify actions-lock.json was NOT created in the wrong location
	if _, err := os.Stat(wrongLockPath); err == nil {
		t.Errorf("actions-lock.json incorrectly created at nested path: %s (should be at repo root)", wrongLockPath)
	}

	// Verify the workflow lock file was created
	lockFilePath := filepath.Join(setup.workflowsDir, "test-action.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file %s was not created", lockFilePath)
	}

	t.Logf("Integration test passed - actions-lock.json created at correct location")
}
