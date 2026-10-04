//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestGitConfigurationInMainJob verifies that git configuration step is included in the main agentic job
func TestGitConfigurationInMainJob(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "git-config-test")

	// Create a simple test workflow
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test Git Configuration

This is a test workflow to verify git configuration is included.
`

	testFile := filepath.Join(tmpDir, "test-git-config.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	// Generate YAML content
	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Verify git configuration step is present in the compiled workflow
	if !strings.Contains(lockContent, "Configure Git credentials") {
		t.Error("Expected 'Configure Git credentials' step to be present in compiled workflow")
	}

	// Verify the step calls the git credentials shell script
	if !strings.Contains(lockContent, "configure_git_credentials.sh") {
		t.Error("Expected configure_git_credentials.sh to be called in compiled workflow")
	}
}

// TestGitConfigurationStepsHelper tests the generateGitConfigurationSteps helper directly
func TestGitConfigurationStepsHelper(t *testing.T) {
	compiler := NewCompiler()

	steps := compiler.generateGitConfigurationSteps()

	// Verify we get expected number of lines (6 lines: name, env, GITHUB_REPOSITORY, GITHUB_SERVER_URL, GITHUB_TOKEN, run)
	if len(steps) != 6 {
		t.Errorf("Expected 6 lines in git configuration steps, got %d", len(steps))
	}

	// Verify the content of the steps
	expectedContents := []string{
		"Configure Git credentials",
		"env:",
		"GITHUB_REPOSITORY:",
		"GITHUB_TOKEN:",
		"configure_git_credentials.sh",
	}

	fullContent := strings.Join(steps, "")

	for _, expected := range expectedContents {
		if !strings.Contains(fullContent, expected) {
			t.Errorf("Expected git configuration steps to contain '%s'", expected)
		}
	}

	// Verify proper indentation (should start with 6 spaces for job step level)
	if !strings.HasPrefix(steps[0], "      - name:") {
		t.Error("Expected first line to have proper indentation for job step (6 spaces)")
	}
}

// TestGitCredentialsCleanerStep verifies that git credentials cleaner step is included before agent execution
func TestGitCredentialsCleanerStep(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "git-cleaner-test")

	// Create a simple test workflow
	testContent := `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test Git Credentials Cleaner

This is a test workflow to verify git credentials cleaner is included.
`

	testFile := filepath.Join(tmpDir, "test-git-cleaner.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	// Generate YAML content
	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Verify credentials cleaner step is present
	if !strings.Contains(lockContent, "Clean credentials") {
		t.Error("Expected 'Clean credentials' step to be present in compiled workflow")
	}

	// Verify the cleaner script is called
	if !strings.Contains(lockContent, "clean_git_credentials.sh") {
		t.Error("Expected clean_git_credentials.sh script to be called")
	}

	// Verify the cleaner step comes before the agent execution
	// Find the positions of both steps
	cleanerPos := strings.Index(lockContent, "Clean credentials")
	// The agent execution step is named "Execute GitHub Copilot CLI" (for Copilot engine)
	// or similar names for other engines
	agentPos := strings.Index(lockContent, "Execute GitHub Copilot CLI")
	if agentPos == -1 {
		// Try alternative patterns for other engines
		agentPos = strings.Index(lockContent, "agentic_execution")
	}

	if cleanerPos == -1 {
		t.Fatal("Could not find 'Clean credentials' step in compiled workflow")
	}

	if agentPos == -1 {
		t.Fatal("Could not find agent execution step in compiled workflow")
	}

	// Verify cleaner comes before agent execution
	if cleanerPos >= agentPos {
		t.Error("Expected 'Clean credentials' step to come before agent execution step")
	}
}

// TestCredentialsCleanerStepsHelper tests the generateCredentialsCleanerStep helper directly
func TestCredentialsCleanerStepsHelper(t *testing.T) {
	compiler := NewCompiler()

	t.Run("no known actions - only git credentials script", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(nil)

		fullContent := strings.Join(steps, "")

		expectedContents := []string{
			"Clean credentials",
			"run: |",
			"bash \"${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials.sh\"",
			"bash \"${RUNNER_TEMP}/gh-aw/actions/verify_git_credentials.sh\"",
		}
		if strings.Contains(fullContent, "continue-on-error: true") {
			t.Error("Credential cleanup must fail closed")
		}
		for _, expected := range expectedContents {
			if !strings.Contains(fullContent, expected) {
				t.Errorf("Expected credentials cleaner steps to contain '%s'", expected)
			}
		}
		if strings.Contains(fullContent, "clean_known_action_credentials.sh") {
			t.Error("clean_known_action_credentials.sh must not appear when no known actions are detected")
		}

		// Verify proper indentation (should start with 6 spaces for job step level)
		if !strings.HasPrefix(steps[0], "      - name:") {
			t.Error("Expected first line to have proper indentation for job step (6 spaces)")
		}
	})

	t.Run("with known actions - both scripts in run block", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(map[string]struct{}{"GH_AW_CLEAN_AWS": {}})

		fullContent := strings.Join(steps, "")

		if !strings.Contains(fullContent, "Clean credentials") {
			t.Error("Expected step name 'Clean credentials'")
		}
		if !strings.Contains(fullContent, `GH_AW_CLEAN_AWS: "true"`) {
			t.Error("Expected GH_AW_CLEAN_AWS env var")
		}
		if !strings.Contains(fullContent, "clean_git_credentials.sh") {
			t.Error("Expected clean_git_credentials.sh call")
		}
		if !strings.Contains(fullContent, "clean_known_action_credentials.sh") {
			t.Error("Expected clean_known_action_credentials.sh call")
		}
	})
}

// TestGitConfigurationSkippedWhenCheckoutDisabled verifies that git credential steps
// are not emitted when checkout: false is set in the workflow frontmatter.
func TestGitConfigurationSkippedWhenCheckoutDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "git-config-checkout-false-test")

	testContent := `---
on: issues
permissions:
  issues: read
engine: copilot
checkout: false
---

# Test Workflow (no checkout)

This workflow uses API tools only and does not need the repository to be checked out.
`

	testFile := filepath.Join(tmpDir, "test-no-checkout.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// When checkout: false, the agent job must NOT contain "Configure Git credentials"
	// since there is no .git directory and git remote set-url origin would fail.
	if strings.Contains(lockContent, "Configure Git credentials") {
		t.Error("'Configure Git credentials' step must NOT be present when checkout: false (no .git directory)")
	}

	const cleanerStepBlock = "- name: Clean credentials\n        run: |\n          bash \"${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials.sh\"\n          bash \"${RUNNER_TEMP}/gh-aw/actions/verify_git_credentials.sh\""
	if !strings.Contains(lockContent, cleanerStepBlock) {
		t.Error("Expected fail-closed cleanup and verification even when checkout is disabled")
	}
}

// TestGenerateGitConfigurationStepsForData tests the generateGitConfigurationStepsForData helper
// which handles current: true checkouts in subdirectories.
func TestGenerateGitConfigurationStepsForData(t *testing.T) {
	compiler := NewCompiler()

	t.Run("nil data falls back to standard behavior", func(t *testing.T) {
		steps := compiler.generateGitConfigurationStepsForData(nil)
		fullContent := strings.Join(steps, "")

		if !strings.Contains(fullContent, "Configure Git credentials") {
			t.Error("Expected 'Configure Git credentials' step name")
		}
		if strings.Contains(fullContent, "working-directory:") {
			t.Error("working-directory must not be present when data is nil")
		}
		if !strings.Contains(fullContent, "GITHUB_REPOSITORY: ${{ github.repository }}") {
			t.Error("Expected default GITHUB_REPOSITORY expression")
		}
	})

	t.Run("no checkout configs falls back to standard behavior", func(t *testing.T) {
		data := &WorkflowData{}
		steps := compiler.generateGitConfigurationStepsForData(data)
		fullContent := strings.Join(steps, "")

		if strings.Contains(fullContent, "working-directory:") {
			t.Error("working-directory must not be present when there are no checkout configs")
		}
		if !strings.Contains(fullContent, "GITHUB_REPOSITORY: ${{ github.repository }}") {
			t.Error("Expected default GITHUB_REPOSITORY expression")
		}
	})

	t.Run("current: true at workspace root falls back to standard behavior", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "owner/target", Current: true},
			},
		}
		steps := compiler.generateGitConfigurationStepsForData(data)
		fullContent := strings.Join(steps, "")

		// current at root (no path) → no working-directory needed
		if strings.Contains(fullContent, "working-directory:") {
			t.Error("working-directory must not be present when current checkout is at workspace root")
		}
	})

	t.Run("current: true in subdirectory emits working-directory with custom repo", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "owner/public-target", Path: "./target", Current: true},
			},
		}
		steps := compiler.generateGitConfigurationStepsForData(data)
		fullContent := strings.Join(steps, "")

		if !strings.Contains(fullContent, "Configure Git credentials") {
			t.Error("Expected 'Configure Git credentials' step name")
		}
		if !strings.Contains(fullContent, `working-directory: "target"`) {
			t.Errorf("Expected working-directory to be 'target', got:\n%s", fullContent)
		}
		if !strings.Contains(fullContent, `GITHUB_REPOSITORY: "owner/public-target"`) {
			t.Errorf("Expected GITHUB_REPOSITORY to be the current checkout's repository, got:\n%s", fullContent)
		}
		if !strings.Contains(fullContent, "configure_git_credentials.sh") {
			t.Error("Expected configure_git_credentials.sh to be called")
		}
		// Verify proper indentation
		if !strings.HasPrefix(steps[0], "      - name:") {
			t.Error("Expected first line to have proper indentation for job step (6 spaces)")
		}
	})

	t.Run("current: true in subdirectory without explicit repository uses default", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				// No Repository set — checkout the workflow repo into a subdirectory
				{Path: "./subdir", Current: true},
			},
		}
		steps := compiler.generateGitConfigurationStepsForData(data)
		fullContent := strings.Join(steps, "")

		if !strings.Contains(fullContent, `working-directory: "subdir"`) {
			t.Errorf("Expected working-directory to be 'subdir', got:\n%s", fullContent)
		}
		// When no custom repo, GITHUB_REPOSITORY should be the workflow expression
		if !strings.Contains(fullContent, "GITHUB_REPOSITORY: ${{ github.repository }}") {
			t.Errorf("Expected default GITHUB_REPOSITORY expression when no custom repo set, got:\n%s", fullContent)
		}
	})

	t.Run("current: true in nested subdirectory strips leading ./ from path", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "owner/repo", Path: "./nested/target", Current: true},
			},
		}
		steps := compiler.generateGitConfigurationStepsForData(data)
		fullContent := strings.Join(steps, "")

		if !strings.Contains(fullContent, `working-directory: "nested/target"`) {
			t.Errorf("Expected working-directory to be 'nested/target', got:\n%s", fullContent)
		}
	})
}

// TestGitConfigurationWithCurrentCheckoutInSubdir verifies that the compiled workflow
// emits working-directory in both Configure Git credentials steps (pre- and post-agent)
// when checkout: current: true is in a subdirectory.
func TestGitConfigurationWithCurrentCheckoutInSubdir(t *testing.T) {
	tmpDir := testutil.TempDir(t, "git-config-current-checkout-test")

	testContent := `---
on: push
permissions:
  contents: read
engine: copilot
checkout:
  - sparse-checkout: |
      .github/workflows
  - repository: owner/public-target
    path: ./target
    current: true
---

# Test workflow with current checkout in subdirectory
`

	testFile := filepath.Join(tmpDir, "test-current-checkout.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// The agent job should have Configure Git credentials steps with working-directory
	// pointing to the current checkout path ("target")
	if !strings.Contains(lockContent, "Configure Git credentials") {
		t.Error("Expected 'Configure Git credentials' step to be present")
	}
	if !strings.Contains(lockContent, `working-directory: "target"`) {
		t.Errorf("Expected working-directory: \"target\" in Configure Git credentials step.\nLock content:\n%s", lockContent)
	}
	// The configured repository should be the current checkout's repo
	if !strings.Contains(lockContent, `GITHUB_REPOSITORY: "owner/public-target"`) {
		t.Errorf("Expected GITHUB_REPOSITORY to be 'owner/public-target'.\nLock content:\n%s", lockContent)
	}
}
