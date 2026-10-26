//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// ========================================
// Safe Outputs, Threat Detection, and Reusable Workflow Job Tests
// ========================================

// TestBuildSafeOutputsJobsCreatesExpectedJobs tests that safe output steps are created correctly
// in the consolidated safe_outputs job
func TestBuildSafeOutputsJobsCreatesExpectedJobs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "safe-outputs-jobs-test")

	frontmatter := `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  add-comment:
    max: 3
  add-labels:
    allowed: [bug, enhancement]
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	// Read compiled output
	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check that the consolidated safe_outputs job is created
	if !containsInNonCommentLines(yamlStr, "safe_outputs:") {
		t.Error("Expected safe_outputs job not found in output")
	}

	// Check that the handler manager step is created (since create-issue, add-comment, and add-labels are now handled by the handler manager)
	expectedSteps := []string{
		"name: Process Safe Outputs",
		"id: process_safe_outputs",
	}
	for _, step := range expectedSteps {
		if !strings.Contains(yamlStr, step) {
			t.Errorf("Expected step %q not found in output", step)
		}
	}
	if strings.Contains(yamlStr, "process-safe-outputs.stdout.log") {
		t.Error("Process stdout log should NOT be included in safe outputs artifact upload")
	}
	if strings.Contains(yamlStr, "process-safe-outputs.stderr.log") {
		t.Error("Process stderr log should NOT be included in safe outputs artifact upload")
	}

	// Verify handler config contains all three enabled safe outputs
	if !strings.Contains(yamlStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in output")
	}
	if !strings.Contains(yamlStr, "create_issue") {
		t.Error("Expected create_issue in handler config")
	}
	if !strings.Contains(yamlStr, "add_comment") {
		t.Error("Expected add_comment in handler config")
	}
	if !strings.Contains(yamlStr, "add_labels") {
		t.Error("Expected add_labels in handler config")
	}

	// Check that the API-only consolidated job respects ubuntu-slim's timeout limit.
	if !strings.Contains(yamlStr, "timeout-minutes: 15") {
		t.Error("Expected timeout-minutes: 15 for consolidated safe_outputs job")
	}
}

// TestBuildJobsWithThreatDetection tests job building with threat detection enabled
func TestBuildJobsWithThreatDetection(t *testing.T) {
	tmpDir := testutil.TempDir(t, "threat-detection-test")

	frontmatter := `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
  threat-detection:
    enabled: true
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	// Read compiled output
	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check that detection is a separate job (not inline in agent job)
	if !containsInNonCommentLines(yamlStr, "  detection:") {
		t.Error("Expected detection to be a separate job, not inline in agent job")
	}

	// Check that detection job contains detection steps
	detectionSection := extractJobSection(yamlStr, "detection")
	if detectionSection == "" {
		t.Fatal("Detection job not found in compiled YAML")
	}
	if !strings.Contains(detectionSection, "detection_guard") {
		t.Error("Expected detection job to contain detection_guard step")
	}
	if !strings.Contains(detectionSection, "detection_conclusion") {
		t.Error("Expected detection job to contain detection_conclusion step")
	}

	// Check that safe_outputs job references detection job result
	if !strings.Contains(yamlStr, "needs.detection.result == 'success'") {
		t.Error("Expected safe output jobs to check needs.detection.result == 'success'")
	}
}

// TestBuildJobsWithReusableWorkflow tests custom jobs using reusable workflows
func TestBuildJobsWithReusableWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "reusable-workflow-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  call-other:
    uses: owner/repo/.github/workflows/reusable.yml@main
    with:
      param1: value1
    secrets:
      token: ${{ secrets.MY_TOKEN }}
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	// Read compiled output
	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check that reusable workflow job is created
	if !containsInNonCommentLines(yamlStr, "call-other:") {
		t.Error("Expected call-other job")
	}

	// Check for uses directive
	if !strings.Contains(yamlStr, "uses: owner/repo/.github/workflows/reusable.yml@main") {
		t.Error("Expected uses directive for reusable workflow")
	}
}

// TestBuildJobsWithReusableWorkflowSecretsInherit tests that secrets: inherit is correctly emitted
// for reusable workflow call jobs.
func TestBuildJobsWithReusableWorkflowSecretsInherit(t *testing.T) {
	tmpDir := testutil.TempDir(t, "reusable-workflow-secrets-inherit-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  call-other:
    uses: owner/repo/.github/workflows/reusable.yml@main
    secrets: inherit
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "test.lock.yml"))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	yamlStr := string(content)

	if !strings.Contains(yamlStr, "uses: owner/repo/.github/workflows/reusable.yml@main") {
		t.Error("Expected uses directive for reusable workflow")
	}
	if !strings.Contains(yamlStr, "secrets: inherit") {
		t.Error("Expected 'secrets: inherit' directive")
	}
	if strings.Contains(yamlStr, "secrets:\n      ") {
		t.Error("Should not emit secrets map when using inherit")
	}
}

func TestBuildJobsWithReusableWorkflowTimeoutMinutesFails(t *testing.T) {
	tmpDir := testutil.TempDir(t, "reusable-workflow-timeout-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  call-other:
    uses: owner/repo/.github/workflows/reusable.yml@main
    timeout-minutes: 10
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("CompileWorkflow() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "call-other") {
		t.Fatalf("CompileWorkflow() error = %v, want error mentioning job name", err)
	}
	if !strings.Contains(err.Error(), "cannot set timeout-minutes") {
		t.Fatalf("CompileWorkflow() error = %v, want error mentioning timeout-minutes restriction", err)
	}
}

func TestBuildJobsJobConditionExtraction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "job-condition-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  conditional_job:
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    steps:
      - run: echo "conditional"
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	// Read compiled output
	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check that job has if condition
	if !strings.Contains(yamlStr, "github.event_name == 'push'") {
		t.Error("Expected if condition to be preserved")
	}
}

// TestBuildJobsWithOutputs tests custom jobs with outputs
func TestBuildJobsWithOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "job-outputs-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  generate_output:
    runs-on: ubuntu-latest
    outputs:
      result: ${{ steps.compute.outputs.value }}
    steps:
      - id: compute
        run: echo "value=test" >> $GITHUB_OUTPUT
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	// Read compiled output
	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check that job has outputs section
	if !strings.Contains(yamlStr, "outputs:") {
		t.Error("Expected outputs section")
	}

	// Check that result output is defined
	if !strings.Contains(yamlStr, "result:") {
		t.Error("Expected 'result' output")
	}
}
