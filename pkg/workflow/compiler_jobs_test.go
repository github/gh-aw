//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
)

// ========================================
// extractJobsFromFrontmatter Tests
// ========================================

// TestExtractJobsFromFrontmatter tests the extractJobsFromFrontmatter method
func TestExtractJobsFromFrontmatter(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		frontmatter map[string]any
		expectedLen int
	}{
		{
			name:        "no jobs in frontmatter",
			frontmatter: map[string]any{"on": "push"},
			expectedLen: 0,
		},
		{
			name: "jobs present",
			frontmatter: map[string]any{
				"on": "push",
				"jobs": map[string]any{
					"job1": map[string]any{"runs-on": "ubuntu-latest"},
					"job2": map[string]any{"runs-on": "windows-latest"},
				},
			},
			expectedLen: 2,
		},
		{
			name: "jobs is not a map",
			frontmatter: map[string]any{
				"on":   "push",
				"jobs": "invalid",
			},
			expectedLen: 0,
		},
		{
			name:        "nil frontmatter",
			frontmatter: nil,
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.extractJobsFromFrontmatter(tt.frontmatter)
			if len(result) != tt.expectedLen {
				t.Errorf("extractJobsFromFrontmatter() returned %d jobs, want %d", len(result), tt.expectedLen)
			}
		})
	}
}

// ========================================
// Integration Tests
// ========================================

// TestBuildPreActivationJobWithPermissionCheck tests building a pre-activation job with permission checks
func TestBuildPreActivationJobWithPermissionCheck(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:    "Test Workflow",
		Command: []string{"test"},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	job, err := compiler.buildPreActivationJob(workflowData, true)
	if err != nil {
		t.Fatalf("buildPreActivationJob() returned error: %v", err)
	}

	if job.Name != string(constants.PreActivationJobName) {
		t.Errorf("Job name = %q, want %q", job.Name, string(constants.PreActivationJobName))
	}

	// Check that it has outputs
	if job.Outputs == nil {
		t.Error("Expected job to have outputs")
	}

	// Check for activated output
	if _, ok := job.Outputs["activated"]; !ok {
		t.Error("Expected 'activated' output")
	}

	// Check steps exist
	if len(job.Steps) == 0 {
		t.Error("Expected job to have steps")
	}
}

// TestBuildPreActivationJobWithStopTime tests building a pre-activation job with stop-time
func TestBuildPreActivationJobWithStopTime(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		StopTime:    "2024-12-31T23:59:59Z",
		SafeOutputs: &SafeOutputsConfig{},
	}

	job, err := compiler.buildPreActivationJob(workflowData, false)
	if err != nil {
		t.Fatalf("buildPreActivationJob() returned error: %v", err)
	}

	// Check that steps include stop-time check
	stepsContent := strings.Join(job.Steps, "")
	if !strings.Contains(stepsContent, "Check stop-time limit") {
		t.Error("Expected 'Check stop-time limit' step")
	}
}

// TestBuildActivationJob tests building an activation job
func TestBuildActivationJob(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	if err != nil {
		t.Fatalf("buildActivationJob() returned error: %v", err)
	}

	if job.Name != string(constants.ActivationJobName) {
		t.Errorf("Job name = %q, want %q", job.Name, string(constants.ActivationJobName))
	}

	// Check for timestamp check step
	stepsContent := strings.Join(job.Steps, "")
	if !strings.Contains(stepsContent, "Check workflow file timestamps") {
		t.Error("Expected 'Check workflow file timestamps' step")
	}
}

// TestBuildActivationJobWithReaction tests building an activation job with AI reaction
func TestBuildActivationJobWithReaction(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AIReaction:  "rocket",
		SafeOutputs: &SafeOutputsConfig{},
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	if err != nil {
		t.Fatalf("buildActivationJob() returned error: %v", err)
	}

	// Check that outputs include comment-related outputs (but not reaction_id since reaction is in pre-activation)
	if _, ok := job.Outputs["comment_id"]; !ok {
		t.Error("Expected 'comment_id' output")
	}

	// Check for comment step (not reaction, since reaction moved to pre-activation)
	stepsContent := strings.Join(job.Steps, "")
	if !strings.Contains(stepsContent, "Add comment with workflow run link") {
		t.Error("Expected comment step in activation job")
	}
}

// TestBuildActivationJobLockFilename tests that lock filenames are passed through
// unchanged to the activation job environment.
func TestBuildActivationJobLockFilename(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "example.workflow.lock.yml")
	if err != nil {
		t.Fatalf("buildActivationJob() returned error: %v", err)
	}

	// Check that GH_AW_WORKFLOW_FILE uses the lock filename exactly
	stepsContent := strings.Join(job.Steps, "")
	if !strings.Contains(stepsContent, `GH_AW_WORKFLOW_FILE: "example.workflow.lock.yml"`) {
		t.Errorf("Expected GH_AW_WORKFLOW_FILE to be 'example.workflow.lock.yml', got steps content:\n%s", stepsContent)
	}
	// Verify it does NOT contain the incorrect .g. version
	if strings.Contains(stepsContent, "example.workflow.g.lock.yml") {
		t.Error("GH_AW_WORKFLOW_FILE should not contain '.g.' in the filename")
	}
}

// TestBuildMainJobWithActivation tests building the main job with activation dependency
func TestBuildMainJobWithActivation(t *testing.T) {
	compiler := NewCompiler()
	// Initialize stepOrderTracker
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
	}

	job, err := compiler.buildMainJob(workflowData, true)
	if err != nil {
		t.Fatalf("buildMainJob() returned error: %v", err)
	}

	if job.Name != string(constants.AgentJobName) {
		t.Errorf("Job name = %q, want %q", job.Name, string(constants.AgentJobName))
	}

	// Check that it depends on activation job
	found := false
	for _, need := range job.Needs {
		if need == string(constants.ActivationJobName) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected job to depend on %s, got needs: %v", string(constants.ActivationJobName), job.Needs)
	}
}

// TestBuildCustomJobsWithActivation tests building custom jobs with activation dependency
func TestBuildCustomJobsWithActivation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-jobs-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_lint:
    runs-on: ubuntu-latest
    steps:
      - run: echo "lint"
  custom_build:
    runs-on: ubuntu-latest
    needs: custom_lint
    steps:
      - run: echo "build"
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

	// Check that custom jobs exist
	if !strings.Contains(yamlStr, "custom_lint:") {
		t.Error("Expected custom_lint job")
	}
	if !strings.Contains(yamlStr, "custom_build:") {
		t.Error("Expected custom_build job")
	}

	// custom_lint without explicit needs should depend on activation
	// custom_build has explicit needs so should keep that
}

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

	// Check that the consolidated job has correct timeout (15 minutes for consolidated job)
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

	// Check that detection job is created
	if !containsInNonCommentLines(yamlStr, "detection:") {
		t.Error("Expected detection job to be created")
	}

	// Check that safe_outputs job depends on detection
	if !strings.Contains(yamlStr, string(constants.DetectionJobName)) {
		t.Error("Expected safe output jobs to depend on detection job")
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

// TestBuildJobsJobConditionExtraction tests that if conditions are properly extracted
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
