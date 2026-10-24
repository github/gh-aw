//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/github/gh-aw/pkg/fileutil"
)

// extractHandlerConfig parses the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG value from a compiled
// lock file. The compiler emits the handler config as a double-quoted YAML scalar, so the
// embedded JSON quotes are backslash-escaped in the lock file and must be unquoted before
// the JSON can be parsed.
func extractHandlerConfig(t *testing.T, lockContent string) map[string]any {
	t.Helper()

	const key = "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:"
	var quoted string
	for _, line := range strings.Split(lockContent, "\n") {
		if idx := strings.Index(line, key); idx >= 0 {
			quoted = strings.TrimSpace(line[idx+len(key):])
			break
		}
	}
	if quoted == "" {
		t.Fatalf("Lock file should contain %s\nLock file content:\n%s", key, lockContent)
	}

	configJSON, err := strconv.Unquote(quoted)
	if err != nil {
		t.Fatalf("Failed to unquote handler config value %s: %v", quoted, err)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("Failed to parse handler config JSON: %v\nJSON: %s", err, configJSON)
	}
	return config
}

// dispatchRepositoryToolConfig returns the per-tool configuration stored under the
// dispatch_repository entry of the safe outputs handler config.
func dispatchRepositoryToolConfig(t *testing.T, config map[string]any, toolName string) map[string]any {
	t.Helper()

	dispatch, ok := config["dispatch_repository"].(map[string]any)
	if !ok {
		t.Fatalf("Handler config should contain a dispatch_repository object, got: %#v", config["dispatch_repository"])
	}
	tools, ok := dispatch["tools"].(map[string]any)
	if !ok {
		t.Fatalf("dispatch_repository config should contain a tools object, got: %#v", dispatch["tools"])
	}
	tool, ok := tools[toolName].(map[string]any)
	if !ok {
		t.Fatalf("dispatch_repository tools should contain %q, got: %#v", toolName, tools)
	}
	return tool
}

// Global binary path shared across all integration tests
var (
	globalBinaryPath string
	projectRoot      string
	binaryTempDir    string
)

const (
	windowsRemoveRetries    = 10
	windowsRemoveRetryDelay = 100 * time.Millisecond
)

// TestMain builds the gh-aw binary once before running tests
func TestMain(m *testing.M) {
	// Get project root
	wd, err := os.Getwd()
	if err != nil {
		panic("Failed to get current working directory: " + err.Error())
	}
	projectRoot = filepath.Join(wd, "..", "..")

	// Prefer a pre-built binary when provided by CI to avoid rebuilding.
	prebuiltBinaryPath := os.Getenv("GH_AW_INTEGRATION_BINARY")
	if prebuiltBinaryPath != "" {
		if _, err := os.Stat(prebuiltBinaryPath); err != nil {
			panic("GH_AW_INTEGRATION_BINARY does not exist: " + err.Error())
		}
		globalBinaryPath = prebuiltBinaryPath
	} else {
		// Create temp directory for the shared binary
		tempDir, err := os.MkdirTemp("", "gh-aw-integration-binary-*")
		if err != nil {
			panic("Failed to create temp directory for binary: " + err.Error())
		}
		binaryTempDir = tempDir

		binaryName := "gh-aw"
		if runtime.GOOS == "windows" {
			binaryName = "gh-aw.exe"
		}
		globalBinaryPath = filepath.Join(tempDir, binaryName)

		// Build the gh-aw binary
		buildCmd := exec.Command("make", "build")
		buildCmd.Dir = projectRoot
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			panic("Failed to build gh-aw binary: " + err.Error())
		}

		// Copy binary to temp directory
		srcBinary := filepath.Join(projectRoot, binaryName)
		if err := fileutil.CopyFile(srcBinary, globalBinaryPath); err != nil {
			panic("Failed to copy gh-aw binary to temp directory: " + err.Error())
		}
	}

	if globalBinaryPath == "" {
		panic("global binary path is empty")
	}

	// Make the binary executable
	if err := os.Chmod(globalBinaryPath, 0755); err != nil {
		panic("Failed to make binary executable: " + err.Error())
	}

	// Run the tests
	code := m.Run()

	// Clean up the shared binary directory
	if binaryTempDir != "" {
		os.RemoveAll(binaryTempDir)
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

	// Copy the pre-built binary to this test's temp directory, preserving the
	// executable extension (e.g. ".exe" on Windows) so the OS can execute it.
	binaryName := "gh-aw" + filepath.Ext(globalBinaryPath)
	binaryPath := filepath.Join(tempDir, binaryName)
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
		err = removeAllWithRetry(tempDir)
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

	t.Logf("Successfully compiled workflow to %s", lockFilePath)
}

func TestCompileGHESArtifactPinsIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflowPath := filepath.Join(setup.workflowsDir, "ghes-artifacts.md")
	err := os.WriteFile(testWorkflowPath, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
steps:
  - uses: actions/upload-artifact@v7
    with:
      name: test
      path: test.txt
---
# GHES artifact pins
`), 0o600)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", "--ghes", testWorkflowPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockContent, err := os.ReadFile(filepath.Join(setup.workflowsDir, "ghes-artifacts.lock.yml"))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	contents := string(lockContent)
	if !strings.Contains(contents, "actions/upload-artifact@c6a366c94c3e0affe28c06c8df20a878f24da3cf # v3.2.2") {
		t.Error("Lock file should contain the GHES-compatible upload-artifact pin")
	}
	if !strings.Contains(contents, "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0") {
		t.Error("Lock file should contain the GHES-compatible download-artifact pin")
	}
	if strings.Contains(contents, "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1") {
		t.Error("Lock file should not contain the default upload-artifact pin")
	}
}

func removeAllWithRetry(path string) error {
	attempts := 1
	if runtime.GOOS == "windows" {
		attempts = windowsRemoveRetries
	}

	var err error
	for i := 0; i < attempts; i++ {
		err = os.RemoveAll(path)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		if runtime.GOOS != "windows" {
			return err
		}
		time.Sleep(windowsRemoveRetryDelay)
	}

	return err
}

func TestCompileWithIncludeWithEmptyFrontmatterUnderPty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY-based test is not supported on Windows")
	}
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

// TestCompileStagedSafeOutputsCreateIssue verifies that a workflow with staged: true
// and a create-issue handler compiles without error and emits GH_AW_SAFE_OUTPUTS_STAGED.
// Prior to the schema fix, staged was not listed in the create-issue schema
// (additionalProperties: false), so the frontmatter validator would reject the workflow.
func TestCompileStagedSafeOutputsCreateIssue(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Create Issue
on:
  workflow_dispatch:
permissions: read-all
engine: copilot
safe-outputs:
  staged: true
  noop:
    report-as-issue: false
  create-issue:
    title-prefix: "[staged] "
    max: 1
---

Verify staged safe-outputs with create-issue.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-create-issue.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-create-issue.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\nLock file content:\n%s", lockContentStr)
	}
}

func TestCompileSafeOutputsCreatePullRequestAllowedBaseBranches(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Allowed Base Branches
on:
  workflow_dispatch:
permissions: read-all
engine: copilot
safe-outputs:
  create-pull-request:
    allowed-base-branches:
      - main
      - release/*
---

Verify allowed base branches in create-pull-request safe output.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "allowed-base-branches.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "allowed-base-branches.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Fatalf("Expected handler config env var in lock file, got:\n%s", lockContentStr)
	}

	if !strings.Contains(lockContentStr, "allowed_base_branches") {
		t.Fatalf("Expected allowed_base_branches in handler config, got:\n%s", lockContentStr)
	}

	if !strings.Contains(lockContentStr, "release/*") {
		t.Fatalf("Expected release/* pattern in lock file handler config, got:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsAddComment verifies that a workflow with staged: true
// and an add-comment handler compiles and emits GH_AW_SAFE_OUTPUTS_STAGED.
// Prior to the schema fix, staged was not listed in the add-comment handler schema.
func TestCompileStagedSafeOutputsAddComment(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Add Comment
on:
  workflow_dispatch:
permissions: read-all
engine: copilot
safe-outputs:
  staged: true
  add-comment:
    max: 1
---

Verify staged safe-outputs with add-comment.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-add-comment.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-add-comment.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsCreateDiscussion verifies that a workflow with staged: true
// and a create-discussion handler compiles and emits GH_AW_SAFE_OUTPUTS_STAGED.
// Prior to the schema fix, staged was not listed in the create-discussion handler schema.
func TestCompileStagedSafeOutputsCreateDiscussion(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Create Discussion
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  staged: true
  create-discussion:
    max: 1
    category: general
---

Verify staged safe-outputs with create-discussion.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-create-discussion.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-create-discussion.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsWithTargetRepo verifies that staged: true emits
// GH_AW_SAFE_OUTPUTS_STAGED even when a target-repo is specified on the handler.
// Staged mode is independent of target-repo.
func TestCompileStagedSafeOutputsWithTargetRepo(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Cross-Repo
on:
  workflow_dispatch:
permissions: read-all
engine: copilot
safe-outputs:
  staged: true
  create-issue:
    title-prefix: "[cross-repo staged] "
    max: 1
    target-repo: org/other-repo
---

Verify that staged mode is independent of target-repo.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-cross-repo.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-cross-repo.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// staged is independent of target-repo: env var must be present
	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\" even with target-repo set\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsMultipleHandlers verifies that staged: true with
// multiple handler types compiles and emits GH_AW_SAFE_OUTPUTS_STAGED exactly once.
// Previously, adding staged to most handler types caused a schema validation error.
func TestCompileStagedSafeOutputsMultipleHandlers(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Multiple Handlers
on:
  workflow_dispatch:
permissions: read-all
engine: copilot
safe-outputs:
  staged: true
  create-issue:
    title-prefix: "[staged] "
    max: 1
  add-comment:
    max: 2
  add-labels:
    allowed:
      - bug
  update-issue:
---

Verify staged safe-outputs with multiple handler types.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-multi-handler.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-multi-handler.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsPermissionsGlobal verifies that when safe-outputs has
// global staged: true, the compiled safe_outputs job has no job-level permissions block
// (staged mode emits only preview output; no GitHub API writes are performed).
func TestCompileStagedSafeOutputsPermissionsGlobal(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Global Permissions
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  staged: true
  create-issue:
    title-prefix: "[staged] "
    max: 1
  add-labels:
    max: 3
  create-discussion:
    max: 1
---

Verify that global staged mode removes all write permissions from the safe_outputs job.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-global-perms.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-global-perms.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)
	safeOutputsJobSection := extractYAMLJobSection(lockContentStr, "safe_outputs")
	if safeOutputsJobSection == "" {
		t.Fatalf("Could not find safe_outputs job in lock file\nLock file content:\n%s", lockContentStr)
	}

	// Global staged means staged handlers should not introduce handler-specific write
	// permissions for discussions/pull requests/contents in the safe_outputs job.
	if strings.Contains(safeOutputsJobSection, "discussions: write") {
		t.Errorf("Staged lock file should NOT contain 'discussions: write' in safe_outputs job\nLock file content:\n%s", lockContentStr)
	}
	if strings.Contains(safeOutputsJobSection, "pull-requests: write") {
		t.Errorf("Staged lock file should NOT contain 'pull-requests: write' in safe_outputs job\nLock file content:\n%s", lockContentStr)
	}
	if strings.Contains(safeOutputsJobSection, "contents: write") {
		t.Errorf("Staged lock file should NOT contain 'contents: write' in safe_outputs job\nLock file content:\n%s", lockContentStr)
	}

	// Staged env var must still be present
	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\nLock file content:\n%s", lockContentStr)
	}
}

func TestCompileFlagForcesStagedSafeOutputs(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Forced Staged Via Flag
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  staged: false
  create-issue:
    max: 1
---

Verify that --staged forces staged mode regardless of frontmatter.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "forced-staged-flag.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", "--staged", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "forced-staged-flag.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)
	safeOutputsJobSection := extractYAMLJobSection(lockContentStr, "safe_outputs")
	if safeOutputsJobSection == "" {
		t.Fatalf("Could not find safe_outputs job in lock file\nLock file content:\n%s", lockContentStr)
	}

	if !strings.Contains(lockContentStr, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`) {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\nLock file content:\n%s", lockContentStr)
	}
	if strings.Contains(safeOutputsJobSection, "issues: write") {
		t.Errorf("Forced staged lock file should not request write permissions in safe_outputs job\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsPermissionsPerHandler verifies that when only specific
// safe-output handlers have staged: true, only those handlers' write permissions are
// omitted. Non-staged handlers still contribute their required permissions.
func TestCompileStagedSafeOutputsPermissionsPerHandler(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Staged Per-Handler Permissions
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    staged: true
    title-prefix: "[staged] "
    max: 1
  add-labels:
    max: 3
---

Verify that per-handler staged mode removes only that handler's write permissions.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-perhandler-perms.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-perhandler-perms.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// add-labels is not staged and needs issues: write and pull-requests: write
	if !strings.Contains(lockContentStr, "issues: write") {
		t.Errorf("Lock file should contain 'issues: write' for non-staged add-labels\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "pull-requests: write") {
		t.Errorf("Lock file should contain 'pull-requests: write' for non-staged add-labels\nLock file content:\n%s", lockContentStr)
	}

	// create-issue is staged so it must NOT add issues: write on its own.
	// However add-labels already contributes issues: write, so we can only verify
	// that discussions and contents: write are absent (which create-issue does not add
	// anyway). The key behaviour is verified via the unit tests in safe_outputs_permissions_test.go.
	if strings.Contains(lockContentStr, "discussions: write") {
		t.Errorf("Lock file should NOT contain 'discussions: write' when only add-labels and staged create-issue are configured\nLock file content:\n%s", lockContentStr)
	}
	if strings.Contains(lockContentStr, "contents: write") {
		t.Errorf("Lock file should NOT contain 'contents: write'\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileStagedSafeOutputsPermissionsAllHandlersStaged verifies that when all
// handlers are per-handler staged, the safe_outputs job has no write permissions.
func TestCompileStagedSafeOutputsPermissionsAllHandlersStaged(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: All Handlers Staged
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  noop:
    report-as-issue: false
  create-issue:
    staged: true
    max: 1
  create-discussion:
    staged: true
    max: 1
---

Verify that when all handlers are per-handler staged, no write permissions appear.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "staged-all-handlers.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "staged-all-handlers.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)
	safeOutputsJobSection := extractYAMLJobSection(lockContentStr, "safe_outputs")
	if safeOutputsJobSection == "" {
		t.Fatalf("Could not find safe_outputs job in lock file\nLock file content:\n%s", lockContentStr)
	}

	// All handlers are staged — handler-specific write permissions should not appear
	// in safe_outputs job.
	for _, perm := range []string{"discussions: write", "pull-requests: write", "contents: write"} {
		if strings.Contains(safeOutputsJobSection, perm) {
			t.Errorf("Staged lock file should NOT contain %q\nLock file content:\n%s", perm, lockContentStr)
		}
	}
}

func extractYAMLJobSection(yamlContent, jobName string) string {
	const (
		// GitHub Actions lock files in tests are emitted with 2-space job indentation
		// under `jobs:` and 4-space indentation for job contents.
		jobIndent        = "  "
		jobContentIndent = "    "
	)

	lines := strings.Split(yamlContent, "\n")
	var jobLines []string
	inJob := false
	jobPrefix := jobIndent + jobName + ":"

	for _, line := range lines {
		if strings.HasPrefix(line, jobPrefix) {
			inJob = true
			jobLines = append(jobLines, line)
			continue
		}

		if inJob {
			if isTopLevelJobStart(line, jobIndent, jobContentIndent) {
				break
			}
			if strings.HasPrefix(line, "jobs:") {
				break
			}
			jobLines = append(jobLines, line)
		}
	}

	return strings.Join(jobLines, "\n")
}

func isTopLevelJobStart(line, jobIndent, jobContentIndent string) bool {
	return strings.HasPrefix(line, jobIndent) &&
		strings.HasSuffix(line, ":") &&
		!strings.HasPrefix(line, jobContentIndent)
}

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

// TestCompileSafeOutputsActions verifies that a workflow with safe-outputs.actions
// compiles successfully and produces the expected output in the lock file:
// - GH_AW_SAFE_OUTPUT_ACTIONS env var on the process_safe_outputs step
// - An injected action step with the correct id and if-condition
func TestCompileSafeOutputsActions(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Safe Output Actions
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
engine: copilot
safe-outputs:
  actions:
    add-label:
      uses: actions-ecosystem/action-add-labels@v1
      description: Add a label to the current PR
      env:
        GITHUB_TOKEN: ${{ github.token }}
---

# Test Safe Output Actions

When done, call add_label with the appropriate label.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-safe-output-actions.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-safe-output-actions.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Verify GH_AW_SAFE_OUTPUT_ACTIONS is emitted on the process_safe_outputs step
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUT_ACTIONS") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUT_ACTIONS\nLock file content:\n%s", lockContentStr)
	}

	// Verify the injected action step id
	if !strings.Contains(lockContentStr, "id: action_add_label") {
		t.Errorf("Lock file should contain step 'id: action_add_label'\nLock file content:\n%s", lockContentStr)
	}

	// Verify the injected action step if-condition
	if !strings.Contains(lockContentStr, "action_add_label_payload") {
		t.Errorf("Lock file should contain 'action_add_label_payload' in the step if-condition\nLock file content:\n%s", lockContentStr)
	}

	// Verify the env block is included in the action step
	if !strings.Contains(lockContentStr, "GITHUB_TOKEN") {
		t.Errorf("Lock file should contain GITHUB_TOKEN env var in the action step\nLock file content:\n%s", lockContentStr)
	}

	// Verify the process safe outputs step is present (required to process action payloads)
	if !strings.Contains(lockContentStr, "process_safe_outputs.cjs") {
		t.Errorf("Lock file should contain the process_safe_outputs.cjs step\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileDispatchRepository verifies that a workflow with a dispatch_repository
// safe-output compiles successfully and produces the expected lock file content:
//   - dispatch_repository appears in the Tools prompt hint
//   - Each tool's _dispatch_repository_tool metadata is present in the tools JSON
//   - GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG contains the dispatch_repository config
//   - The handler config includes workflow, event_type, and repository fields
func TestCompileDispatchRepository(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Dispatch Repository
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  dispatch_repository:
    trigger_ci:
      description: Trigger CI in another repository
      workflow: ci.yml
      event_type: ci_trigger
      repository: org/target-repo
      max: 1
---

# Test Dispatch Repository

Call trigger_ci to trigger CI in the target repository.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-dispatch-repo.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-dispatch-repo.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Verify dispatch_repository appears in the Tools prompt hint
	if !strings.Contains(lockContentStr, "dispatch_repository") {
		t.Errorf("Lock file should contain 'dispatch_repository'\nLock file content:\n%s", lockContentStr)
	}

	// Verify the tool definition has _dispatch_repository_tool metadata
	if !strings.Contains(lockContentStr, `"_dispatch_repository_tool": "trigger_ci"`) {
		t.Errorf("Lock file should contain _dispatch_repository_tool metadata for trigger_ci\nLock file content:\n%s", lockContentStr)
	}

	// Verify the tool name is normalized correctly
	if !strings.Contains(lockContentStr, `"name": "trigger_ci"`) {
		t.Errorf("Lock file should contain the trigger_ci tool definition\nLock file content:\n%s", lockContentStr)
	}

	// Verify GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG contains dispatch_repository
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG\nLock file content:\n%s", lockContentStr)
	}
	handlerConfig := extractHandlerConfig(t, lockContentStr)
	triggerCI := dispatchRepositoryToolConfig(t, handlerConfig, "trigger_ci")

	// Verify required fields are in the handler config
	if triggerCI["workflow"] != "ci.yml" {
		t.Errorf("Handler config should contain workflow 'ci.yml', got: %#v", triggerCI["workflow"])
	}
	if triggerCI["event_type"] != "ci_trigger" {
		t.Errorf("Handler config should contain event_type 'ci_trigger', got: %#v", triggerCI["event_type"])
	}
	if triggerCI["repository"] != "org/target-repo" {
		t.Errorf("Handler config should contain repository 'org/target-repo', got: %#v", triggerCI["repository"])
	}
}

// TestCompileDispatchRepositoryMultipleTools verifies that dispatch_repository with
// multiple tools compiles correctly and all tools appear in the output with their
// correct configurations (repository and allowed_repositories).
func TestCompileDispatchRepositoryMultipleTools(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Dispatch Repository Multiple Tools
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  dispatch_repository:
    trigger_ci:
      description: Trigger CI pipeline
      workflow: ci.yml
      event_type: ci_trigger
      repository: org/target-repo
      inputs:
        environment:
          type: choice
          options:
            - staging
            - production
          default: staging
      max: 1
    notify_service:
      description: Notify external service
      workflow: notify.yml
      event_type: notify_event
      allowed_repositories:
        - org/service-repo
        - org/backup-repo
      inputs:
        message:
          type: string
          description: Notification message
      max: 2
---

# Test Dispatch Repository Multiple Tools

Dispatch trigger_ci and notify_service as appropriate.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-multi.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-multi.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Both tools must have _dispatch_repository_tool metadata
	if !strings.Contains(lockContentStr, `"_dispatch_repository_tool": "trigger_ci"`) {
		t.Errorf("Lock file should contain _dispatch_repository_tool metadata for trigger_ci\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, `"_dispatch_repository_tool": "notify_service"`) {
		t.Errorf("Lock file should contain _dispatch_repository_tool metadata for notify_service\nLock file content:\n%s", lockContentStr)
	}

	// Both tool names must appear in tool definitions
	if !strings.Contains(lockContentStr, `"name": "trigger_ci"`) {
		t.Errorf("Lock file should contain trigger_ci tool\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, `"name": "notify_service"`) {
		t.Errorf("Lock file should contain notify_service tool\nLock file content:\n%s", lockContentStr)
	}

	// Handler config must include both tools with their settings
	handlerConfig := extractHandlerConfig(t, lockContentStr)
	triggerCI := dispatchRepositoryToolConfig(t, handlerConfig, "trigger_ci")
	notifyService := dispatchRepositoryToolConfig(t, handlerConfig, "notify_service")
	if triggerCI["workflow"] != "ci.yml" {
		t.Errorf("Handler config should contain trigger_ci workflow 'ci.yml', got: %#v", triggerCI["workflow"])
	}
	if notifyService["workflow"] != "notify.yml" {
		t.Errorf("Handler config should contain notify_service workflow 'notify.yml', got: %#v", notifyService["workflow"])
	}

	// allowed_repositories must be serialized in the handler config
	if !strings.Contains(lockContentStr, "org/service-repo") {
		t.Errorf("Handler config should contain allowed_repositories entry org/service-repo\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "org/backup-repo") {
		t.Errorf("Handler config should contain allowed_repositories entry org/backup-repo\nLock file content:\n%s", lockContentStr)
	}

	// Input schemas must be reflected in tool properties
	if !strings.Contains(lockContentStr, `"environment"`) {
		t.Errorf("Lock file should contain the environment input property for trigger_ci\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, `"message"`) {
		t.Errorf("Lock file should contain the message input property for notify_service\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileDispatchRepositoryValidationFailure verifies that a workflow with
// an invalid dispatch_repository configuration (missing required 'workflow' field)
// fails compilation with a descriptive error message.
func TestCompileDispatchRepositoryValidationFailure(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Dispatch Repository Validation Failure
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  dispatch_repository:
    trigger_ci:
      event_type: ci_trigger
      repository: org/target-repo
---

# Test Dispatch Repository Validation Failure

This workflow is intentionally missing the required 'workflow' field.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-fail.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Compilation must fail
	if err == nil {
		t.Fatalf("Compilation should have failed due to missing 'workflow' field, but succeeded\nOutput: %s", outputStr)
	}

	// Error message must mention dispatch_repository and the missing field
	if !strings.Contains(outputStr, "dispatch_repository") {
		t.Errorf("Error output should mention 'dispatch_repository'\nOutput: %s", outputStr)
	}
	if !strings.Contains(outputStr, "workflow") {
		t.Errorf("Error output should mention the missing 'workflow' field\nOutput: %s", outputStr)
	}
}

// TestCompileDispatchRepositoryWorkflowFile compiles the canonical test workflow
// from pkg/cli/workflows/test-copilot-dispatch-repository.md and verifies that it
// produces a valid lock file with the expected dispatch_repository configuration.
func TestCompileDispatchRepositoryWorkflowFile(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Copy the canonical workflow file into the test's .github/workflows dir
	srcPath := filepath.Join(projectRoot, "pkg/cli/workflows/test-copilot-dispatch-repository.md")
	dstPath := filepath.Join(setup.workflowsDir, "test-copilot-dispatch-repository.md")

	srcContent, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("Failed to read source workflow file %s: %v", srcPath, err)
	}
	if err := os.WriteFile(dstPath, srcContent, 0644); err != nil {
		t.Fatalf("Failed to write workflow to test dir: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", dstPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed for canonical workflow: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-copilot-dispatch-repository.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Verify both tools are present in the compiled output
	if !strings.Contains(lockContentStr, `"_dispatch_repository_tool": "trigger_ci"`) {
		t.Errorf("Lock file should contain trigger_ci tool metadata\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, `"_dispatch_repository_tool": "notify_service"`) {
		t.Errorf("Lock file should contain notify_service tool metadata\nLock file content:\n%s", lockContentStr)
	}

	// Verify handler config is correctly serialized
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG\nLock file content:\n%s", lockContentStr)
	}
	handlerConfig := extractHandlerConfig(t, lockContentStr)
	if _, ok := handlerConfig["dispatch_repository"]; !ok {
		t.Errorf("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG should contain dispatch_repository config, got keys: %v", handlerConfig)
	}

	t.Logf("Canonical dispatch_repository workflow compiled successfully to %s", lockFilePath)
}

// TestCompileSafeOutputsActionsMultiple verifies that multiple actions in safe-outputs.actions
// all generate separate action steps and all appear in GH_AW_SAFE_OUTPUT_ACTIONS.
func TestCompileSafeOutputsActionsMultiple(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Multiple Safe Output Actions
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  actions:
    add-bug-label:
      uses: actions-ecosystem/action-add-labels@v1
      description: Add a bug label
      env:
        GITHUB_TOKEN: ${{ github.token }}
    close-issue:
      uses: peter-evans/close-issue@v3
      description: Close the issue
---

# Test Multiple Safe Output Actions

Call add_bug_label or close_issue as appropriate.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-multi-safe-output-actions.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-multi-safe-output-actions.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Both action steps must be present
	if !strings.Contains(lockContentStr, "id: action_add_bug_label") {
		t.Errorf("Lock file should contain step 'id: action_add_bug_label'\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "id: action_close_issue") {
		t.Errorf("Lock file should contain step 'id: action_close_issue'\nLock file content:\n%s", lockContentStr)
	}

	// Both payloads must appear in if-conditions
	if !strings.Contains(lockContentStr, "action_add_bug_label_payload") {
		t.Errorf("Lock file should contain 'action_add_bug_label_payload'\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "action_close_issue_payload") {
		t.Errorf("Lock file should contain 'action_close_issue_payload'\nLock file content:\n%s", lockContentStr)
	}

	// GH_AW_SAFE_OUTPUT_ACTIONS must mention both tools
	if !strings.Contains(lockContentStr, "add_bug_label") {
		t.Errorf("Lock file should contain 'add_bug_label' in GH_AW_SAFE_OUTPUT_ACTIONS\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "close_issue") {
		t.Errorf("Lock file should contain 'close_issue' in GH_AW_SAFE_OUTPUT_ACTIONS\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileSafeOutputsActionsCombinedWithBuiltin verifies that safe-outputs.actions
// can be used alongside built-in safe-output handlers (add-comment, create-issue, etc.)
// without compilation errors.
func TestCompileSafeOutputsActionsCombinedWithBuiltin(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Combined Safe Outputs
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  add-comment:
    max: 1
  add-labels:
    allowed: [bug, enhancement]
  actions:
    apply-fix:
      uses: actions-ecosystem/action-add-labels@v1
      description: Apply fix label
      env:
        GITHUB_TOKEN: ${{ github.token }}
---

# Combined Safe Outputs

Use add_comment, add_labels, or apply_fix as appropriate.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-combined-safe-outputs.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-combined-safe-outputs.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Verify both built-in and action tools are present
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUT_ACTIONS") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUT_ACTIONS\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "id: action_apply_fix") {
		t.Errorf("Lock file should contain step 'id: action_apply_fix'\nLock file content:\n%s", lockContentStr)
	}
	// Verify built-in handler config is still present
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileSafeOutputsActionsOnlyNoBuiltin verifies that a workflow with only
// safe-outputs.actions (no built-in handlers) still compiles correctly and emits
// the safe_outputs job.
func TestCompileSafeOutputsActionsOnlyNoBuiltin(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Actions Only Safe Outputs
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
engine: copilot
safe-outputs:
  actions:
    pin-pr:
      uses: actions-ecosystem/action-add-labels@v1
      description: Pin the PR
---

# Actions Only Safe Outputs

Call pin_pr to pin the pull request.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-actions-only-safe-outputs.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-actions-only-safe-outputs.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Verify the safe_outputs job is created
	if !strings.Contains(lockContentStr, "safe_outputs") {
		t.Errorf("Lock file should contain a 'safe_outputs' job\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUT_ACTIONS") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUT_ACTIONS\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "id: action_pin_pr") {
		t.Errorf("Lock file should contain step 'id: action_pin_pr'\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileDispatchRepositoryGitHubActionsExpression verifies that GitHub Actions
// expressions are accepted without format validation errors in the 'repository' field.
// Expressions like "${{ inputs.target_repo }}" or "${{ vars.CI_REPO }}" must compile
// successfully because their values are only known at workflow runtime.
func TestCompileDispatchRepositoryGitHubActionsExpression(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Dispatch Repository GitHub Expression
on:
  workflow_dispatch:
    inputs:
      target_repo:
        description: Target repository for dispatch
        required: true
        type: string
permissions:
  contents: read
engine: copilot
safe-outputs:
  dispatch_repository:
    trigger_ci:
      description: Trigger CI using a runtime-resolved repository
      workflow: ci.yml
      event_type: ci_trigger
      repository: ${{ inputs.target_repo }}
      max: 1
---

# Test Dispatch Repository GitHub Expression

Call trigger_ci to dispatch a repository_dispatch event.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-expr.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed for workflow with GitHub Actions expression in 'repository': %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-expr.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// The expression must be preserved verbatim in the handler config
	if !strings.Contains(lockContentStr, `inputs.target_repo`) {
		t.Errorf("Lock file should preserve the GitHub Actions expression in handler config\nLock file content:\n%s", lockContentStr)
	}

	// GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG must be present
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Errorf("Lock file should contain GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileDispatchRepositoryGitHubActionsExpressionAllowedRepos verifies that
// GitHub Actions expressions are accepted in 'allowed_repositories' entries.
// Mixed lists (static slugs alongside expressions) must also compile successfully.
func TestCompileDispatchRepositoryGitHubActionsExpressionAllowedRepos(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	testWorkflow := `---
name: Test Dispatch Repository Expression in AllowedRepos
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  dispatch_repository:
    notify_dynamic:
      description: Notify a dynamically-resolved set of repositories
      workflow: notify.yml
      event_type: notify_event
      allowed_repositories:
        - org/static-repo
        - ${{ vars.DYNAMIC_REPO }}
      max: 2
---

# Test Dispatch Repository Expression in AllowedRepos

Call notify_dynamic to send notifications.
`
	testWorkflowPath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-expr-allowed.md")
	if err := os.WriteFile(testWorkflowPath, []byte(testWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile", testWorkflowPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed for workflow with GitHub Actions expression in 'allowed_repositories': %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-dispatch-repo-expr-allowed.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Both the static slug and the expression must appear in the handler config
	if !strings.Contains(lockContentStr, "org/static-repo") {
		t.Errorf("Lock file should contain the static allowed_repositories entry\nLock file content:\n%s", lockContentStr)
	}
	if !strings.Contains(lockContentStr, "vars.DYNAMIC_REPO") {
		t.Errorf("Lock file should preserve the GitHub Actions expression in allowed_repositories\nLock file content:\n%s", lockContentStr)
	}
}

// TestCompileWithActionsRepoFlag verifies that the --actions-repo flag causes the
// custom repository to be used in action mode instead of the default github/gh-aw-actions.
func TestCompileWithActionsRepoFlag(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Use the canonical test workflow fixture
	srcPath := filepath.Join(projectRoot, "pkg/cli/workflows/test-actions-repo.md")
	dstPath := filepath.Join(setup.workflowsDir, "test-actions-repo.md")

	srcContent, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("Failed to read source workflow file %s: %v", srcPath, err)
	}
	if err := os.WriteFile(dstPath, srcContent, 0644); err != nil {
		t.Fatalf("Failed to write workflow to test dir: %v", err)
	}

	cmd := exec.Command(setup.binaryPath, "compile",
		"--action-mode", "action",
		"--actions-repo", "myorg/custom-aw-actions",
		"--action-tag", "v9.9.9",
		"--allow-action-refs",
		dstPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-actions-repo.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// The custom repo should appear in the lock file
	if !strings.Contains(lockContentStr, "myorg/custom-aw-actions") {
		t.Errorf("Lock file should contain the custom actions repo 'myorg/custom-aw-actions'\nLock file content:\n%s", lockContentStr)
	}

	// The default repo should NOT appear
	if strings.Contains(lockContentStr, "github/gh-aw-actions") {
		t.Errorf("Lock file should NOT contain the default 'github/gh-aw-actions' when overridden\nLock file content:\n%s", lockContentStr)
	}

	t.Logf("Actions repo flag test passed - custom repo baked into lock file: %s", lockFilePath)
}

// TestCompileWithActionsRepoDefaultFallback verifies that without --actions-repo, the default
// github/gh-aw-actions repository is used in the lock file when action mode is set.
func TestCompileWithActionsRepoDefaultFallback(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Use the canonical test workflow fixture
	srcPath := filepath.Join(projectRoot, "pkg/cli/workflows/test-actions-repo.md")
	dstPath := filepath.Join(setup.workflowsDir, "test-actions-repo.md")

	srcContent, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("Failed to read source workflow file %s: %v", srcPath, err)
	}
	if err := os.WriteFile(dstPath, srcContent, 0644); err != nil {
		t.Fatalf("Failed to write workflow to test dir: %v", err)
	}

	// No --actions-repo flag; action mode with a tag
	cmd := exec.Command(setup.binaryPath, "compile",
		"--action-mode", "action",
		"--action-tag", "v9.9.9",
		"--allow-action-refs",
		dstPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
	}

	lockFilePath := filepath.Join(setup.workflowsDir, "test-actions-repo.lock.yml")
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	// Default repo should appear in the lock file
	if !strings.Contains(lockContentStr, "github/gh-aw-actions") {
		t.Errorf("Lock file should contain the default 'github/gh-aw-actions' when no override is specified\nLock file content:\n%s", lockContentStr)
	}

	t.Logf("Default actions repo test passed - default repo baked into lock file: %s", lockFilePath)
}

func TestCompileWithActionRefOverrideIncludesCompilerVersionMetadata(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
	}{
		{
			name: "action-tag",
			args: []string{"--action-tag", "v9.9.9", "--allow-action-refs"},
		},
		{
			name:      "action-tag-requires-opt-in",
			args:      []string{"--action-tag", "v9.9.9"},
			wantError: true,
		},
		{
			name: "gh-aw-ref",
			args: []string{"--gh-aw-ref", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupIntegrationTest(t)
			defer setup.cleanup()

			srcPath := filepath.Join(projectRoot, "pkg/cli/workflows/test-actions-repo.md")
			dstPath := filepath.Join(setup.workflowsDir, "test-actions-repo.md")

			srcContent, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatalf("Failed to read source workflow file %s: %v", srcPath, err)
			}
			if err := os.WriteFile(dstPath, srcContent, 0o644); err != nil {
				t.Fatalf("Failed to write workflow to test dir: %v", err)
			}

			cmdArgs := append([]string{"compile"}, tt.args...)
			cmdArgs = append(cmdArgs, dstPath)
			cmd := exec.Command(setup.binaryPath, cmdArgs...)
			output, err := cmd.CombinedOutput()
			lockFilePath := filepath.Join(setup.workflowsDir, "test-actions-repo.lock.yml")
			if tt.wantError {
				if err == nil {
					t.Fatal("Compile should reject a mutable action tag without --allow-action-refs")
				}
				if !strings.Contains(string(output), "[action-pin]") {
					t.Fatalf("Compile should report an action-pin error, got:\n%s", output)
				}
				if _, statErr := os.Stat(lockFilePath); !os.IsNotExist(statErr) {
					t.Fatalf("Rejected compile should not emit a lock file, got: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CLI compile command failed: %v\nOutput: %s", err, string(output))
			}

			lockContent, err := os.ReadFile(lockFilePath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			metadataLine := ""
			for line := range strings.SplitSeq(string(lockContent), "\n") {
				if strings.Contains(line, "gh-aw-metadata:") {
					metadataLine = line
					break
				}
			}
			if metadataLine == "" {
				t.Fatal("Could not find gh-aw-metadata in lock file")
			}
			if !strings.Contains(metadataLine, `"compiler_version":"`) || strings.Contains(metadataLine, `"compiler_version":""`) {
				t.Fatalf("Expected non-empty compiler_version in metadata, got: %s", metadataLine)
			}
		})
	}
}
