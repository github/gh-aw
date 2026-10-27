//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// assertTokenInProcessSafeOutputsEnv verifies that a given environment variable name
// appears inside the env block of the process_safe_outputs step. This is more precise
// than a plain strings.Contains on the full lock file, which can produce false positives
// if the name appears in another context (e.g. a downstream step).
func assertTokenInProcessSafeOutputsEnv(t *testing.T, lockContent, tokenName string) {
	t.Helper()

	stepStart := strings.Index(lockContent, "id: process_safe_outputs")
	if stepStart == -1 {
		t.Fatal("Expected process_safe_outputs step in generated lock file")
	}

	// Trim to just the content of this step (stop at the next top-level list item "-").
	stepContent := lockContent[stepStart:]
	if nextStep := strings.Index(stepContent, "\n      - "); nextStep != -1 {
		stepContent = stepContent[:nextStep]
	}

	if !strings.Contains(stepContent, "env:") {
		t.Fatalf("Expected env: block in process_safe_outputs step for %s", tokenName)
	}

	if !strings.Contains(stepContent, tokenName) {
		t.Errorf("Expected %s in env block of process_safe_outputs step", tokenName)
	}
}

func TestOutputIssueJobGenerationWithCopilotAssigneeAddsAgentToken(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-issue-copilot-assignee-token")

	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    max: 1
    assignees: copilot
---

# Test Output Issue Copilot Assignee Agent Token

This workflow tests that GH_AW_ASSIGN_TO_AGENT_TOKEN is set in process_safe_outputs
so create_issue can assign directly without a separate step.
`

	testFile := filepath.Join(tmpDir, "test-output-issue-copilot-assignee-token.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-output-issue-copilot-assignee-token.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify GH_AW_ASSIGN_TO_AGENT_TOKEN is set in the env block of the
	// process_safe_outputs step so create_issue.cjs can assign copilot directly.
	assertTokenInProcessSafeOutputsEnv(t, lockContent, "GH_AW_ASSIGN_TO_AGENT_TOKEN")
}

func TestOutputPRJobGenerationWithCopilotAssigneeAddsAgentToken(t *testing.T) {
	tmpDir := testutil.TempDir(t, "output-pr-copilot-assignee-token")

	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: read
engine: copilot
strict: false
safe-outputs:
  create-pull-request:
    max: 1
    assignees: copilot
---

# Test Output PR Copilot Assignee Agent Token

This workflow tests that GH_AW_ASSIGN_TO_AGENT_TOKEN is set in process_safe_outputs
so create_pull_request can assign copilot to fallback issues directly.
`

	testFile := filepath.Join(tmpDir, "test-output-pr-copilot-assignee-token.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-output-pr-copilot-assignee-token.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify GH_AW_ASSIGN_TO_AGENT_TOKEN is set in the env block of the
	// process_safe_outputs step so create_pull_request.cjs can assign
	// copilot to fallback issues using the agent token.
	assertTokenInProcessSafeOutputsEnv(t, lockContent, "GH_AW_ASSIGN_TO_AGENT_TOKEN")

	// Verify GH_AW_ASSIGN_COPILOT is also set in the same env block.
	assertTokenInProcessSafeOutputsEnv(t, lockContent, "GH_AW_ASSIGN_COPILOT")
}

func TestOutputConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-config-test")

	// Test case with create-issue configuration
	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  create-issue:
    deduplicate-by-title: 1
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

	deduplicateByTitle := workflowData.SafeOutputs.CreateIssues.DeduplicateByTitle
	if deduplicateByTitle == nil || string(*deduplicateByTitle) != "1" {
		t.Errorf("Expected deduplicate-by-title to parse as '1', got %#v", deduplicateByTitle)
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
  issues: read
  pull-requests: read
engine: claude
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

	// Verify create-issue is auto-injected when no safe-outputs are configured (aligns with other builtins)
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be non-nil after auto-injection of create-issue")
	}
	if workflowData.SafeOutputs.CreateIssues == nil {
		t.Error("Expected create-issue to be auto-injected when no safe-outputs configured")
	}
	if !workflowData.SafeOutputs.AutoInjectedCreateIssue {
		t.Error("Expected AutoInjectedCreateIssue to be true when auto-injected")
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
  issues: read
  pull-requests: read
engine: claude
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
	if workflowData.SafeOutputs.AddLabels.Max != nil {
		t.Errorf("Expected Max to be nil for null add-labels, got %v", workflowData.SafeOutputs.AddLabels.Max)
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
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
safe-outputs:
  create-issue:
    deduplicate-by-title: 1
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
	if !strings.Contains(lockContent, "uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3") {
		t.Error("Expected github-script action to be used in safe_outputs job")
	}

	// Verify JavaScript content includes configuration in handler config
	if !strings.Contains(lockContent, `\"title_prefix\":\"[genai] \"`) {
		t.Error("Expected title prefix in handler config")
	}

	if !strings.Contains(lockContent, `\"labels\":[\"copilot\"]`) {
		t.Error("Expected copilot label in handler config")
	}

	if !strings.Contains(lockContent, `\"deduplicate_by_title\":1`) {
		t.Error("Expected deduplicate_by_title in handler config")
	}

	// Verify job dependencies
	if !strings.Contains(lockContent, "needs:") {
		t.Error("Expected safe_outputs job to depend on main job")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputIssueJobGenerationWithCopilotAssigneeUsesHandlerManagerOnly(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-issue-copilot-assignee")

	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
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

	// Copilot assignment for create-issue is handled directly by create_issue.cjs in
	// process_safe_outputs, so the compiler should not emit a legacy follow-up step.
	if strings.Contains(lockContent, "name: Assign Copilot to created issues") {
		t.Error("Did not expect legacy copilot assignment step in consolidated safe_outputs job")
	}
	if strings.Contains(lockContent, "id: assign_copilot_to_created_issues") {
		t.Error("Did not expect legacy assign_copilot_to_created_issues step id")
	}
	if strings.Contains(lockContent, "GH_AW_ISSUES_TO_ASSIGN_COPILOT") || strings.Contains(lockContent, "steps.process_safe_outputs.outputs.issues_to_assign_copilot") {
		t.Error("Did not expect legacy issues_to_assign_copilot wiring from process_safe_outputs")
	}
	if strings.Contains(lockContent, "assign_copilot_to_created_issues.cjs") {
		t.Error("Did not expect legacy assign_copilot_to_created_issues.cjs requirement")
	}
	if strings.Contains(lockContent, "steps.assign_copilot_to_created_issues.outputs.assign_copilot_failure_count") ||
		strings.Contains(lockContent, "steps.assign_copilot_to_created_issues.outputs.assign_copilot_errors") {
		t.Error("Did not expect legacy assign_copilot_to_created_issues output wiring in safe_outputs job")
	}
}

// TestDeduplicateByTitleBoolean verifies that deduplicate-by-title: true is emitted correctly.
func TestDeduplicateByTitleBoolean(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dedup-bool-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    max: 1
    deduplicate-by-title: true
---

Create test issues.
`
	testFile := filepath.Join(tmpDir, "test-dedup-bool.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "test-dedup-bool.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(lockContent), `\"deduplicate_by_title\":true`) {
		t.Errorf("Expected deduplicate_by_title:true in handler config, got:\n%s", lockContent)
	}
}

// TestDeduplicateByTitleFalse verifies that deduplicate-by-title: false is emitted correctly.
func TestDeduplicateByTitleFalse(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dedup-false-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    max: 1
    deduplicate-by-title: false
---

Create test issues.
`
	testFile := filepath.Join(tmpDir, "test-dedup-false.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "test-dedup-false.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(lockContent), `\"deduplicate_by_title\":false`) {
		t.Errorf("Expected deduplicate_by_title:false in handler config, got:\n%s", lockContent)
	}
}

// TestDeduplicateByTitleInteger verifies that deduplicate-by-title: <int> is emitted correctly.
func TestDeduplicateByTitleInteger(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dedup-int-test")

	testContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    max: 1
    deduplicate-by-title: 2
---

Create test issues.
`
	testFile := filepath.Join(tmpDir, "test-dedup-int.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "test-dedup-int.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(lockContent), `\"deduplicate_by_title\":2`) {
		t.Errorf("Expected deduplicate_by_title:2 in handler config, got:\n%s", lockContent)
	}
}

// TestDeduplicateByTitleExpression verifies that deduplicate-by-title accepts
// GitHub Actions expressions and emits them as a JSON string so the runtime
// can evaluate them.
func TestDeduplicateByTitleExpression(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dedup-expr-test")

	testContent := `---
on:
  workflow_dispatch:
    inputs:
      dedup:
        type: boolean
        default: true
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    max: 1
    deduplicate-by-title: ${{ inputs.dedup }}
---

Create test issues.
`
	testFile := filepath.Join(tmpDir, "test-dedup-expr.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "test-dedup-expr.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}

	// GH Actions expressions are emitted as JSON strings so they can be evaluated at runtime.
	if !strings.Contains(string(lockContent), `\"deduplicate_by_title\":\"${{ inputs.dedup }}\"`) {
		t.Errorf("Expected deduplicate_by_title expression in handler config, got:\n%s", lockContent)
	}
}
