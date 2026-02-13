//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestOutputPullRequestConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-pr-config-test")

	// Test case with create-pull-request configuration
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
  issues: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[agent] "
    labels: [automation, bot]
---

# Test Output Pull Request Configuration

This workflow tests the output pull request configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-pr-config.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with output pull-request config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreatePullRequests == nil {
		t.Fatal("Expected pull-request configuration to be parsed")
	}

	// Verify title prefix
	expectedPrefix := "[agent] "
	if workflowData.SafeOutputs.CreatePullRequests.TitlePrefix != expectedPrefix {
		t.Errorf("Expected title prefix '%s', got '%s'", expectedPrefix, workflowData.SafeOutputs.CreatePullRequests.TitlePrefix)
	}

	// Verify labels
	expectedLabels := []string{"automation", "bot"}
	if len(workflowData.SafeOutputs.CreatePullRequests.Labels) != len(expectedLabels) {
		t.Errorf("Expected %d labels, got %d", len(expectedLabels), len(workflowData.SafeOutputs.CreatePullRequests.Labels))
	}

	for i, expectedLabel := range expectedLabels {
		if i >= len(workflowData.SafeOutputs.CreatePullRequests.Labels) || workflowData.SafeOutputs.CreatePullRequests.Labels[i] != expectedLabel {
			t.Errorf("Expected label[%d] to be '%s', got '%s'", i, expectedLabel, workflowData.SafeOutputs.CreatePullRequests.Labels[i])
		}
	}
}

func TestOutputPullRequestJobGeneration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-pr-job-test")

	// Test case with create-pull-request configuration
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
  issues: read
tools:
  github:
    allowed: [list_issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[agent] "
    labels: [automation]
---

# Test Output Pull Request Job Generation

This workflow tests the create_pull_request job generation.
`

	testFile := filepath.Join(tmpDir, "test-output-pr.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output create-pull-request: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	// Convert to string for easier testing
	lockContentStr := string(lockContent)

	// Verify create_pull_request job is present
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Error("Expected 'create_pull_request' job to be in generated workflow")
	}

	// Verify permissions
	if !strings.Contains(lockContentStr, "contents: write") {
		t.Error("Expected contents: write permission in create_pull_request job")
	}

	if !strings.Contains(lockContentStr, "pull-requests: write") {
		t.Error("Expected pull-requests: write permission in create_pull_request job")
	}

	if !strings.Contains(lockContentStr, "issues: write") {
		t.Error("Expected issues: write permission in create_pull_request job (required for fallback issue creation)")
	}

	// Verify steps
	if !strings.Contains(lockContentStr, "Download patch artifact") {
		t.Error("Expected 'Download patch artifact' step in create_pull_request job")
	}

	if !strings.Contains(lockContentStr, "actions/download-artifact@018cc2cf5baa6db3ef3c5f8a56943fffe632ef53") {
		t.Error("Expected download-artifact action to be used in create_pull_request job")
	}

	if !strings.Contains(lockContentStr, "Checkout repository") {
		t.Error("Expected 'Checkout repository' step in create_pull_request job")
	}

	// Verify handler manager step (Process Safe Outputs) exists
	if !strings.Contains(lockContentStr, "Process Safe Outputs") {
		t.Error("Expected 'Process Safe Outputs' (handler manager) step in safe_outputs job")
	}

	if !strings.Contains(lockContentStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Expected github-script action to be used in safe_outputs job")
	}

	// Verify handler manager config includes create_pull_request configuration
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler manager config environment variable")
	}

	if !strings.Contains(lockContentStr, "create_pull_request") {
		t.Error("Expected create_pull_request to be configured in handler manager")
	}

	// Verify job dependencies
	if !strings.Contains(lockContentStr, "needs:") {
		t.Error("Expected safe_outputs job to have dependencies")
	}

	// t.Logf("Generated workflow content:\n%s", lockContentStr)
}

func TestOutputPullRequestDraftFalse(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-pr-draft-false-test")

	// Test case with create-pull-request configuration with draft: false
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
  issues: read
tools:
  github:
    allowed: [list_issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[agent] "
    labels: [automation]
    draft: false
---

# Test Output Pull Request with Draft False

This workflow tests the create_pull_request job generation with draft: false.
`

	testFile := filepath.Join(tmpDir, "test-output-pr-draft-false.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output pull-request draft: false: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	// Convert to string for easier testing
	lockContentStr := string(lockContent)

	// Verify safe_outputs job is present
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Error("Expected 'safe_outputs' job to be in generated workflow")
	}

	// Verify handler manager is configured with create_pull_request
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler manager config environment variable")
	}

	// Verify create_pull_request config in handler manager
	// The config should contain draft: false and other settings as JSON
	if !strings.Contains(lockContentStr, "create_pull_request") {
		t.Error("Expected create_pull_request to be configured in handler manager")
	}

	// Verify the handler manager step (Process Safe Outputs) is present
	if !strings.Contains(lockContentStr, "Process Safe Outputs") || !strings.Contains(lockContentStr, "process_safe_outputs") {
		t.Error("Expected 'Process Safe Outputs' handler manager step in safe_outputs job")
	}

	// t.Logf("Generated workflow content:\n%s", lockContentStr)
}

func TestOutputPullRequestDraftTrue(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-pr-draft-true-test")

	// Test case with create-pull-request configuration with draft: true
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
  issues: read
tools:
  github:
    allowed: [list_issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[agent] "
    labels: [automation]
    draft: true
---

# Test Output Pull Request with Draft True

This workflow tests the create_pull_request job generation with draft: true.
`

	testFile := filepath.Join(tmpDir, "test-output-pr-draft-true.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output pull-request draft: true: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	// Convert to string for easier testing
	lockContentStr := string(lockContent)

	// Verify safe_outputs job is present
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Error("Expected 'safe_outputs' job to be in generated workflow")
	}

	// Verify handler manager is configured with create_pull_request
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler manager config environment variable")
	}

	// Verify create_pull_request config in handler manager
	if !strings.Contains(lockContentStr, "create_pull_request") {
		t.Error("Expected create_pull_request to be configured in handler manager")
	}

	// Verify the handler manager step (Process Safe Outputs) is present
	if !strings.Contains(lockContentStr, "Process Safe Outputs") || !strings.Contains(lockContentStr, "process_safe_outputs") {
		t.Error("Expected 'Process Safe Outputs' handler manager step in safe_outputs job")
	}

	// t.Logf("Generated workflow content:\n%s", lockContentStr)
}

func TestCreatePullRequestIfNoChangesConfig(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "create-pr-if-no-changes-test")

	// Test case with create-pull-request if-no-changes configuration
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
  issues: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[agent] "
    labels: [automation]
    if-no-changes: "error"
---

# Test Create Pull Request If-No-Changes Configuration

This workflow tests the create-pull-request if-no-changes configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-create-pr-if-no-changes.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with create-pull-request if-no-changes config: %v", err)
	}

	// Verify create-pull-request configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected safe-outputs configuration to be present")
	}

	if workflowData.SafeOutputs.CreatePullRequests == nil {
		t.Fatal("Expected create-pull-request configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreatePullRequests.IfNoChanges != "error" {
		t.Errorf("Expected if-no-changes to be 'error', got '%s'", workflowData.SafeOutputs.CreatePullRequests.IfNoChanges)
	}

	// Test with default value
	testContentDefault := `---
on: push
permissions:
  contents: read
  pull-requests: write
  issues: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[agent] "
---

# Test Create Pull Request Default If-No-Changes

This workflow tests the default if-no-changes behavior.
`

	testFileDefault := filepath.Join(tmpDir, "test-create-pr-if-no-changes-default.md")
	if err := os.WriteFile(testFileDefault, []byte(testContentDefault), 0644); err != nil {
		t.Fatal(err)
	}

	// Parse the workflow data for default case
	workflowDataDefault, err := compiler.ParseWorkflowFile(testFileDefault)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with default if-no-changes config: %v", err)
	}

	// Verify default if-no-changes is empty (will default to "warn" at runtime)
	if workflowDataDefault.SafeOutputs.CreatePullRequests.IfNoChanges != "" {
		t.Errorf("Expected default if-no-changes to be empty, got '%s'", workflowDataDefault.SafeOutputs.CreatePullRequests.IfNoChanges)
	}

	// Test compilation with the if-no-changes configuration
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with if-no-changes config: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	// Verify the if-no-changes configuration is passed to the handler manager
	lockContentStr := string(lockContent)
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler manager config environment variable in generated workflow")
	}

	// Verify create_pull_request is configured in the handler manager
	if !strings.Contains(lockContentStr, "create_pull_request") {
		t.Error("Expected create_pull_request to be configured in handler manager")
	}
}

// TestCreatePullRequestPatchArtifactDownload verifies that when create-pull-request
// is enabled, the safe_outputs job includes a step to download the aw.patch artifact
func TestCreatePullRequestPatchArtifactDownload(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test markdown file with create-pull-request configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened]
safe-outputs:
  create-pull-request:
    title-prefix: "[bot] "
---

# Test Create Pull Request Patch Download

This test verifies that the aw.patch artifact is downloaded in the safe_outputs job.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-create-pr-patch-download.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and compile the workflow
	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that safe_outputs job exists
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Fatalf("Generated workflow should contain safe_outputs job")
	}

	// Verify that patch download step exists in safe_outputs job
	if !strings.Contains(lockContentStr, "- name: Download patch artifact") {
		t.Errorf("Expected 'Download patch artifact' step in safe_outputs job when create-pull-request is enabled")
	}

	// Verify that patch is downloaded from unified agent-artifacts
	if !strings.Contains(lockContentStr, "name: agent-artifacts") {
		t.Errorf("Expected patch artifact to be downloaded from 'agent-artifacts'")
	}

	if !strings.Contains(lockContentStr, "path: /tmp/gh-aw/") {
		t.Errorf("Expected patch artifact to be downloaded to '/tmp/gh-aw/'")
	}

	// Verify that the handler manager step exists (processes create_pull_request)
	if !strings.Contains(lockContentStr, "- name: Process Safe Outputs") {
		t.Errorf("Expected 'Process Safe Outputs' (handler manager) step in safe_outputs job")
	}

	// Verify that the condition checks for create_pull_request output type
	if !strings.Contains(lockContentStr, "contains(needs.agent.outputs.output_types, 'create_pull_request')") {
		t.Errorf("Expected condition to check for 'create_pull_request' in output_types")
	}
}

// TestCreatePullRequestAutoMergeConfig verifies that auto-merge configuration
// is properly parsed and passed to the JavaScript handler
func TestCreatePullRequestAutoMergeConfig(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-auto-merge-*")

	// Test case with auto-merge configuration
	testMarkdown := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: write
  pull-requests: write
  issues: read
strict: false
features:
  dangerous-permissions-write: true
safe-outputs:
  create-pull-request:
    title-prefix: "[bot] "
    auto-merge: true
---

# Test Create Pull Request Auto-Merge

This test verifies that auto-merge configuration is properly handled.
`

	// Write the test file
	mdFile := filepath.Join(tmpDir, "test-auto-merge.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	// Create compiler and parse the workflow
	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(mdFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify auto-merge configuration is parsed
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected safe-outputs configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreatePullRequests == nil {
		t.Fatal("Expected create-pull-request configuration to be parsed")
	}

	// Verify auto-merge is set to true
	if !workflowData.SafeOutputs.CreatePullRequests.AutoMerge {
		t.Error("Expected auto-merge to be true")
	}

	// Compile the workflow to verify environment variable is passed
	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG includes auto_merge
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable in safe_outputs job")
	}

	// Verify that the auto-merge configuration is in the handler config JSON
	if !strings.Contains(lockContentStr, `"auto_merge":true`) {
		t.Error("Expected auto_merge:true in handler config JSON")
	}
}

func TestOutputPullRequestFallbackAsIssueFalse(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-pr-fallback-false-test")

	// Test case with create-pull-request configuration with fallback-as-issue: false
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
    fallback-as-issue: false
---

# Test Output Pull Request Fallback False

This workflow tests the create-pull-request with fallback-as-issue disabled.
`

	testFile := filepath.Join(tmpDir, "test-output-pr-fallback-false.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with fallback-as-issue: false: %v", err)
	}

	// Verify that fallback-as-issue is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreatePullRequests == nil {
		t.Fatal("Expected pull-request configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreatePullRequests.FallbackAsIssue == nil {
		t.Fatal("Expected fallback-as-issue to be set")
	}

	if *workflowData.SafeOutputs.CreatePullRequests.FallbackAsIssue != false {
		t.Error("Expected fallback-as-issue to be false")
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with fallback-as-issue: false: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	// Convert to string for easier testing
	lockContentStr := string(lockContent)

	// Find the safe_outputs job section in the lock file
	safeOutputsJobStart := strings.Index(lockContentStr, "safe_outputs:")
	if safeOutputsJobStart == -1 {
		t.Fatal("Could not find safe_outputs job in lock file")
	}

	// Find the next job after safe_outputs (to limit our search scope)
	// Extract a large section after safe_outputs job (next 2000 chars should include all job details)
	endIdx := safeOutputsJobStart + 2000
	if endIdx > len(lockContentStr) {
		endIdx = len(lockContentStr)
	}
	safeOutputsJobSection := lockContentStr[safeOutputsJobStart:endIdx]

	// Verify permissions in safe_outputs job
	if !strings.Contains(safeOutputsJobSection, "contents: write") {
		t.Error("Expected contents: write permission in safe_outputs job")
	}

	if !strings.Contains(safeOutputsJobSection, "pull-requests: write") {
		t.Error("Expected pull-requests: write permission in safe_outputs job")
	}

	if strings.Contains(safeOutputsJobSection, "issues: write") {
		t.Error("Did not expect issues: write permission in safe_outputs job when fallback-as-issue: false")
	}

	// Verify handler config includes fallback_as_issue: false
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable")
	}

	if !strings.Contains(lockContentStr, `fallback_as_issue\":false`) {
		t.Error("Expected fallback_as_issue:false in handler config JSON")
	}
}

func TestOutputPullRequestFallbackAsIssueDefault(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-pr-fallback-default-test")

	// Test case with create-pull-request configuration without fallback-as-issue (should default to true)
	testContent := `---
on: push
permissions:
  contents: read
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
---

# Test Output Pull Request Fallback Default

This workflow tests the create-pull-request with default fallback-as-issue behavior.
`

	testFile := filepath.Join(tmpDir, "test-output-pr-fallback-default.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow: %v", err)
	}

	// Verify that fallback-as-issue defaults to true (nil means default)
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.CreatePullRequests == nil {
		t.Fatal("Expected pull-request configuration to be parsed")
	}

	// When not specified, FallbackAsIssue should be nil (which means default to true)
	if workflowData.SafeOutputs.CreatePullRequests.FallbackAsIssue != nil {
		t.Logf("FallbackAsIssue is set to %v, expected nil for default", *workflowData.SafeOutputs.CreatePullRequests.FallbackAsIssue)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	// Convert to string for easier testing
	lockContentStr := string(lockContent)

	// Find the safe_outputs job section in the lock file
	safeOutputsJobStart := strings.Index(lockContentStr, "safe_outputs:")
	if safeOutputsJobStart == -1 {
		t.Fatal("Could not find safe_outputs job in lock file")
	}

	// Extract a large section after safe_outputs job (next 2000 chars should include all job details)
	endIdx := safeOutputsJobStart + 2000
	if endIdx > len(lockContentStr) {
		endIdx = len(lockContentStr)
	}

	safeOutputsJobSection := lockContentStr[safeOutputsJobStart:endIdx]

	// Verify permissions in safe_outputs job include issues: write (default behavior)
	if !strings.Contains(safeOutputsJobSection, "contents: write") {
		t.Error("Expected contents: write permission in safe_outputs job")
	}

	if !strings.Contains(safeOutputsJobSection, "pull-requests: write") {
		t.Error("Expected pull-requests: write permission in safe_outputs job")
	}
	if !strings.Contains(safeOutputsJobSection, "issues: write") {
		t.Error("Expected issues: write permission in safe_outputs job when fallback-as-issue defaults to true")
	}

	// Verify handler config defaults fallback_as_issue to true (or omitted means default true)
	if !strings.Contains(lockContentStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable")
	}

	// When not explicitly set, fallback_as_issue is omitted from JSON (defaults to true in handler)
	// So we just verify it is NOT explicitly set to false
	if strings.Contains(lockContentStr, `fallback_as_issue\":false`) {
		t.Error("Did not expect fallback_as_issue:false in handler config JSON when using default")
	}
}
