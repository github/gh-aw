//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestOutputConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-config-test")

	// Test case with create-issue configuration
	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    title-prefix: "[genai] "
    labels: [copilot, automation]
---

# Test Output Configuration

This workflow tests the output configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-config.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with output config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreateIssues == nil {
		t.Fatal("Expected issue configuration to be parsed")
	}

	// Verify title prefix
	expectedPrefix := "[genai] "
	if workflowData.SafeOutputs.CreateIssues.TitlePrefix != expectedPrefix {
		t.Errorf("Expected title prefix '%s', got '%s'", expectedPrefix, workflowData.SafeOutputs.CreateIssues.TitlePrefix)
	}

	// Verify labels
	expectedLabels := []string{"copilot", "automation"}
	if len(workflowData.SafeOutputs.CreateIssues.Labels) != len(expectedLabels) {
		t.Errorf("Expected %d labels, got %d", len(expectedLabels), len(workflowData.SafeOutputs.CreateIssues.Labels))
	}

	for i, expectedLabel := range expectedLabels {
		if i >= len(workflowData.SafeOutputs.CreateIssues.Labels) || workflowData.SafeOutputs.CreateIssues.Labels[i] != expectedLabel {
			t.Errorf("Expected label '%s' at index %d, got '%s'", expectedLabel, i, workflowData.SafeOutputs.CreateIssues.Labels[i])
		}
	}
}

func TestOutputConfigEmpty(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-config-empty-test")

	// Test case without output configuration
	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test No Output Configuration

This workflow has no output configuration.
`

	testFile := filepath.Join(tmpDir, "test-no-output.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow without output config: %v", err)
	}

	// Verify output configuration is nil when not specified
	if workflowData.SafeOutputs != nil {
		t.Error("Expected SafeOutputs to be nil when not configured")
	}
}

func TestOutputConfigNull(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-config-null-test")

	// Test case with null values for create-issue and create-pull-request
	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
  create-pull-request:
  add-comment:
  add-labels:
---

# Test Null Output Configuration

This workflow tests the null output configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-null-output-config.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with null output config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	// Verify create-issue configuration is parsed with empty values
	if workflowData.SafeOutputs.CreateIssues == nil {
		t.Fatal("Expected create-issue configuration to be parsed with null value")
	}
	if workflowData.SafeOutputs.CreateIssues.TitlePrefix != "" {
		t.Errorf("Expected empty title prefix for null create-issue, got '%s'", workflowData.SafeOutputs.CreateIssues.TitlePrefix)
	}
	if len(workflowData.SafeOutputs.CreateIssues.Labels) != 0 {
		t.Errorf("Expected empty labels for null create-issue, got %v", workflowData.SafeOutputs.CreateIssues.Labels)
	}

	// Verify create-pull-request configuration is parsed with empty values
	if workflowData.SafeOutputs.CreatePullRequests == nil {
		t.Fatal("Expected create-pull-request configuration to be parsed with null value")
	}
	if workflowData.SafeOutputs.CreatePullRequests.TitlePrefix != "" {
		t.Errorf("Expected empty title prefix for null create-pull-request, got '%s'", workflowData.SafeOutputs.CreatePullRequests.TitlePrefix)
	}
	if len(workflowData.SafeOutputs.CreatePullRequests.Labels) != 0 {
		t.Errorf("Expected empty labels for null create-pull-request, got %v", workflowData.SafeOutputs.CreatePullRequests.Labels)
	}

	// Verify add-comment configuration is parsed with empty values
	if workflowData.SafeOutputs.AddComments == nil {
		t.Fatal("Expected add-comment configuration to be parsed with null value")
	}

	// Verify add-labels configuration is parsed with empty values
	if workflowData.SafeOutputs.AddLabels == nil {
		t.Fatal("Expected add-labels configuration to be parsed with null value")
	}
	if len(workflowData.SafeOutputs.AddLabels.Allowed) != 0 {
		t.Errorf("Expected empty allowed labels for null add-labels, got %v", workflowData.SafeOutputs.AddLabels.Allowed)
	}
	if workflowData.SafeOutputs.AddLabels.Max != 0 {
		t.Errorf("Expected Max to be 0 for null add-labels, got %v", workflowData.SafeOutputs.AddLabels.Max)
	}
}

func TestOutputIssueJobGeneration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-issue-job-test")

	// Test case with create-issue configuration
	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    title-prefix: "[genai] "
    labels: [copilot]
---

# Test Output Issue Job Generation

This workflow tests the create-issue job generation.
`

	testFile := filepath.Join(tmpDir, "test-output-issue.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output issue: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-output-issue.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify safe_outputs consolidated job exists with handler
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'safe_outputs' job to be in generated workflow")
	}
	// Verify handler config is present (handles all safe outputs)
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler config in safe_outputs job")
	}
	// Verify create_issue configuration in handler config
	if !strings.Contains(lockContent, `\"create_issue\"`) {
		t.Error("Expected create_issue in handler config")
	}

	// Verify job properties
	if !strings.Contains(lockContent, "timeout-minutes: 15") {
		t.Error("Expected 15-minute timeout in consolidated safe_outputs job")
	}

	if !strings.Contains(lockContent, "issues: write") {
		t.Error("Expected issues: write permission in safe_outputs job")
	}

	// Verify the job uses github-script
	if !strings.Contains(lockContent, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Expected github-script action to be used in safe_outputs job")
	}

	// Verify JavaScript content includes configuration in handler config
	if !strings.Contains(lockContent, `\"title_prefix\":\"[genai] \"`) {
		t.Error("Expected title prefix in handler config")
	}

	if !strings.Contains(lockContent, `\"labels\":[\"copilot\"]`) {
		t.Error("Expected copilot label in handler config")
	}

	// Verify job dependencies
	if !strings.Contains(lockContent, "needs:") {
		t.Error("Expected safe_outputs job to depend on main job")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputIssueJobGenerationWithCopilotAssigneeAddsAssignmentStep(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-issue-copilot-assignee")

	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    max: 1
    assignees: copilot
---

# Test Output Issue Copilot Assignee

This workflow tests that copilot assignment is wired in consolidated safe outputs mode.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-copilot-assignee.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-output-issue-copilot-assignee.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify consolidated handler manager step exists
	if !strings.Contains(lockContent, "id: process_safe_outputs") {
		t.Error("Expected process_safe_outputs step in generated workflow")
	}

	// Verify copilot assignment step is present and wired to handler manager output
	if !strings.Contains(lockContent, "name: Assign Copilot to created issues") {
		t.Error("Expected copilot assignment step in consolidated safe_outputs job")
	}
	if !strings.Contains(lockContent, "GH_AW_ISSUES_TO_ASSIGN_COPILOT") || !strings.Contains(lockContent, "steps.process_safe_outputs.outputs.issues_to_assign_copilot") {
		t.Error("Expected assignment step to consume issues_to_assign_copilot from process_safe_outputs")
	}
	if !strings.Contains(lockContent, "assign_copilot_to_created_issues.cjs") {
		t.Error("Expected assignment step to require assign_copilot_to_created_issues.cjs")
	}
}
