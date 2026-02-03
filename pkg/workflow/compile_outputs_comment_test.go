//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestOutputCommentConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-comment-config-test")

	// Test case with output.add-comment configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-comment:
---

# Test Output Issue Comment Configuration

This workflow tests the output.add-comment configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-comment.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with output comment config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddComments == nil {
		t.Fatal("Expected issue_comment configuration to be parsed")
	}
}

func TestOutputCommentConfigParsingNull(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-comment-config-null-test")

	// Test case with output.add-comment: null (no {} brackets)
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-comment:
---

# Test Output Issue Comment Configuration with Null Value

This workflow tests the output.add-comment configuration parsing with null value.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-comment-null.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with null output comment config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddComments == nil {
		t.Fatal("Expected issue_comment configuration to be parsed even with null value")
	}
}

func TestOutputCommentConfigTargetParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-comment-target-test")

	// Test case with target: "*"
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-comment:
    target: "*"
---

# Test Output Issue Comment Target Configuration

This workflow tests the output.add-comment target configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-comment-target.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with target comment config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddComments == nil {
		t.Fatal("Expected issue_comment configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddComments.Target != "*" {
		t.Fatalf("Expected target to be '*', got '%s'", workflowData.SafeOutputs.AddComments.Target)
	}
}

func TestOutputCommentMaxTargetParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-comment-max-target-test")

	// Test case with max and target configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-comment:
    max: 3
    target: "123"
---

# Test Output Issue Comments Max Target Configuration

This workflow tests the add-comment max and target configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-comment-max-target.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with max target comment config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddComments == nil {
		t.Fatal("Expected issue_comment configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddComments.Max != 3 {
		t.Fatalf("Expected max to be 3, got %d", workflowData.SafeOutputs.AddComments.Max)
	}

	if workflowData.SafeOutputs.AddComments.Target != "123" {
		t.Fatalf("Expected target to be '123', got '%s'", workflowData.SafeOutputs.AddComments.Target)
	}
}

func TestOutputCommentJobGeneration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-comment-job-test")

	// Test case with output.add-comment configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
tools:
  github:
    allowed: [issue_read]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-comment:
---

# Test Output Issue Comment Job Generation

This workflow tests the safe_outputs job generation.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-comment.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output comment: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-output-issue-comment.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify safe_outputs job exists
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'add_comment' job to be in generated workflow")
	}

	// Verify job properties
	if !strings.Contains(lockContent, "timeout-minutes: 15") {
		t.Error("Expected 10-minute timeout in safe_outputs job")
	}

	if !strings.Contains(lockContent, "permissions:\n      contents: read\n      issues: write\n      pull-requests: write") {
		t.Error("Expected correct permissions in safe_outputs job")
	}

	// Verify the job uses github-script
	if !strings.Contains(lockContent, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Expected github-script action to be used in safe_outputs job")
	}

	// Verify job has conditional execution using BuildSafeOutputType combined with base condition
	expectedConditionParts := []string{
		"!cancelled()",
		"needs.detection.outputs.success == 'true'",
		"github.event.issue.number",
		"github.event.pull_request.number",
	}
	conditionFound := true
	for _, part := range expectedConditionParts {
		if !strings.Contains(lockContent, part) {
			conditionFound = false
			break
		}
	}
	if !conditionFound {
		t.Error("Expected safe_outputs job to have conditional execution with always()")
	}

	// Verify job dependencies
	if !strings.Contains(lockContent, "needs:") {
		t.Error("Expected safe_outputs job to depend on main job")
	}

	// Verify JavaScript content includes environment variable for agent output
	if !strings.Contains(lockContent, "GH_AW_AGENT_OUTPUT:") {
		t.Error("Expected agent output content to be passed as environment variable")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputCommentJobSkippedForNonIssueEvents(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-comment-skip-test")

	// Test case with add-comment configuration but push trigger (not issue/PR)
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
  add-comment:
---

# Test Output Issue Comment Job Skipping

This workflow tests that issue comment job is skipped for non-issue/PR events.
`

	testFile := filepath.Join(tmpDir, "test-comment-skip.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output comment: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-comment-skip.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify safe_outputs job exists (it should be generated regardless of trigger)
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'add_comment' job to be in generated workflow")
	}

	// Verify job has conditional execution using BuildSafeOutputType combined with base condition
	expectedConditionParts := []string{
		"!cancelled()",
		"needs.detection.outputs.success == 'true'",
		"github.event.issue.number",
		"github.event.pull_request.number",
	}
	conditionFound := true
	for _, part := range expectedConditionParts {
		if !strings.Contains(lockContent, part) {
			conditionFound = false
			break
		}
	}
	if !conditionFound {
		t.Error("Expected safe_outputs job to have conditional execution with always() for skipping")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}
