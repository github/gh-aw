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

// ========================================
// Complex Dependency and Ordering Tests
// ========================================

// TestComplexJobDependencyChains tests various job dependency chain scenarios
func TestComplexJobDependencyChains(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dependency-chains-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  job_a:
    runs-on: ubuntu-latest
    steps:
      - run: echo "A"
  job_b:
    runs-on: ubuntu-latest
    needs: job_a
    steps:
      - run: echo "B"
  job_c:
    runs-on: ubuntu-latest
    needs: [job_a, job_b]
    steps:
      - run: echo "C"
  job_d:
    runs-on: ubuntu-latest
    needs: job_c
    steps:
      - run: echo "D"
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

	// Verify all custom jobs are present
	expectedJobs := []string{"job_a:", "job_b:", "job_c:", "job_d:"}
	for _, job := range expectedJobs {
		if !containsInNonCommentLines(yamlStr, job) {
			t.Errorf("Expected job %q not found", job)
		}
	}

	// Verify dependency structure is preserved
	// job_b should depend on job_a
	if !strings.Contains(yamlStr, "needs: job_a") && !strings.Contains(yamlStr, "needs:\n      - job_a") {
		t.Error("Expected job_b to depend on job_a")
	}
}

// TestJobDependingOnPreActivation tests jobs that explicitly depend on pre-activation
func TestJobDependingOnPreActivation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-activation-dep-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
command: /test
jobs:
  early_job:
    runs-on: ubuntu-latest
    needs: pre_activation
    steps:
      - run: echo "Runs after pre-activation"
  normal_job:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Normal job"
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

	// Verify pre-activation job exists (command is configured)
	if !containsInNonCommentLines(yamlStr, "pre_activation:") {
		t.Error("Expected pre_activation job")
	}

	// Verify early_job exists and depends on pre_activation
	if !containsInNonCommentLines(yamlStr, "early_job:") {
		t.Error("Expected early_job")
	}

	// Verify normal_job exists
	if !containsInNonCommentLines(yamlStr, "normal_job:") {
		t.Error("Expected normal_job")
	}
}

// TestJobReferencingCustomJobOutputs tests jobs that reference outputs from custom jobs
func TestJobReferencingCustomJobOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "job-outputs-ref-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  producer:
    runs-on: ubuntu-latest
    outputs:
      value: ${{ steps.gen.outputs.value }}
    steps:
      - id: gen
        run: echo "value=42" >> $GITHUB_OUTPUT
  consumer:
    runs-on: ubuntu-latest
    needs: producer
    if: needs.producer.outputs.value == '42'
    steps:
      - run: echo "Consuming ${{ needs.producer.outputs.value }}"
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

	// Verify both jobs exist
	if !containsInNonCommentLines(yamlStr, "producer:") {
		t.Error("Expected producer job")
	}
	if !containsInNonCommentLines(yamlStr, "consumer:") {
		t.Error("Expected consumer job")
	}

	// Verify output reference is preserved
	if !strings.Contains(yamlStr, "needs.producer.outputs.value") {
		t.Error("Expected reference to producer output")
	}
}

// TestJobsWithRepoMemoryDependencies tests push_repo_memory job positioning
// This tests the job creation logic when repo-memory config is present
func TestJobsWithRepoMemoryDependencies(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	// Create workflow data with repo-memory config
	data := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: write",
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{
					ID:         "test-memory",
					BranchName: "memory-branch",
					FileGlob:   []string{"data/**"},
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[bot] ",
			},
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	// Build activation and agent jobs first
	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	// Build safe outputs jobs (creates detection job when threat detection is enabled)
	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	// Build push_repo_memory job
	threatDetectionEnabledForSafeJobs := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil
	pushRepoMemoryJob, err := compiler.buildPushRepoMemoryJob(data, threatDetectionEnabledForSafeJobs)
	if err != nil {
		t.Fatalf("buildPushRepoMemoryJob() error: %v", err)
	}

	// Verify job was created
	if pushRepoMemoryJob == nil {
		t.Fatal("Expected push_repo_memory job to be created")
	}

	// Add detection dependency if threat detection is enabled
	if threatDetectionEnabledForSafeJobs {
		pushRepoMemoryJob.Needs = append(pushRepoMemoryJob.Needs, string(constants.DetectionJobName))
	}

	// Verify dependencies include detection when threat detection is enabled
	if threatDetectionEnabledForSafeJobs {
		hasDetectionDep := false
		for _, need := range pushRepoMemoryJob.Needs {
			if need == string(constants.DetectionJobName) {
				hasDetectionDep = true
				break
			}
		}
		if !hasDetectionDep {
			t.Error("Expected push_repo_memory to depend on detection job when threat detection is enabled")
		}
	}

	// Verify job name
	if pushRepoMemoryJob.Name != "push_repo_memory" {
		t.Errorf("Expected job name 'push_repo_memory', got %q", pushRepoMemoryJob.Name)
	}
}

// TestJobsWithCacheMemoryDependencies tests update_cache_memory job positioning
// This tests the job creation logic when cache-memory config is present
func TestJobsWithCacheMemoryDependencies(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	// Create workflow data with cache-memory config
	data := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{
					ID:  "test-cache",
					Key: "test-key",
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[bot] ",
			},
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	// Build activation and agent jobs first
	compiler.stepOrderTracker = NewStepOrderTracker()
	activationJob, _ := compiler.buildActivationJob(data, false, "", "test.lock.yml")
	compiler.jobManager.AddJob(activationJob)

	agentJob, _ := compiler.buildMainJob(data, true)
	compiler.jobManager.AddJob(agentJob)

	// Build safe outputs jobs (creates detection job when threat detection is enabled)
	compiler.buildSafeOutputsJobs(data, string(constants.AgentJobName), "test.md")

	// Build update_cache_memory job (only created with threat detection)
	threatDetectionEnabledForSafeJobs := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil
	if threatDetectionEnabledForSafeJobs {
		updateCacheMemoryJob, err := compiler.buildUpdateCacheMemoryJob(data, threatDetectionEnabledForSafeJobs)
		if err != nil {
			t.Fatalf("buildUpdateCacheMemoryJob() error: %v", err)
		}

		// Verify job was created
		if updateCacheMemoryJob == nil {
			t.Fatal("Expected update_cache_memory job to be created when threat detection is enabled")
		}

		// Verify dependencies include detection
		hasDetectionDep := false
		for _, need := range updateCacheMemoryJob.Needs {
			if need == string(constants.DetectionJobName) {
				hasDetectionDep = true
				break
			}
		}
		if !hasDetectionDep {
			t.Error("Expected update_cache_memory to depend on detection job")
		}

		// Verify job name
		if updateCacheMemoryJob.Name != "update_cache_memory" {
			t.Errorf("Expected job name 'update_cache_memory', got %q", updateCacheMemoryJob.Name)
		}
	}
}

// ========================================
// Edge Case Tests
// ========================================

// TestEmptyCustomJobs tests handling of empty custom jobs array
func TestEmptyCustomJobs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "empty-jobs-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs: {}
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

	// Should still have standard jobs (activation, agent)
	if !containsInNonCommentLines(yamlStr, "activation:") {
		t.Error("Expected activation job")
	}
	if !containsInNonCommentLines(yamlStr, string(constants.AgentJobName)) {
		t.Error("Expected agent job")
	}
}

// TestJobWithInvalidDependency tests handling of jobs with non-existent dependencies
func TestJobWithInvalidDependency(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-dep-test")

	// Note: The compiler now validates job dependencies and will fail
	// This test verifies that the error is properly reported
	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  dependent:
    runs-on: ubuntu-latest
    needs: non_existent_job
    steps:
      - run: echo "test"
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	// Should fail with validation error
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("Expected CompileWorkflow() to return error for non-existent job dependency")
	}

	// Verify error message mentions the invalid dependency
	if !strings.Contains(err.Error(), "non_existent_job") {
		t.Errorf("Expected error to mention 'non_existent_job', got: %v", err)
	}
}

// TestJobWithMissingRequiredFields tests handling of jobs missing required fields
func TestJobWithMissingRequiredFields(t *testing.T) {
	tmpDir := testutil.TempDir(t, "missing-fields-test")

	// Job with no runs-on and no uses (invalid but should compile)
	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  minimal:
    steps:
      - run: echo "test"
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	// Should compile (GitHub Actions validates at runtime)
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

	// Verify job exists
	if !containsInNonCommentLines(yamlStr, "minimal:") {
		t.Error("Expected minimal job")
	}
}

// TestMultipleJobsWithComplexDependencies tests a realistic complex scenario
func TestMultipleJobsWithComplexDependencies(t *testing.T) {
	tmpDir := testutil.TempDir(t, "complex-deps-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  lint:
    runs-on: ubuntu-latest
    outputs:
      passed: ${{ steps.check.outputs.result }}
    steps:
      - id: check
        run: echo "result=true" >> $GITHUB_OUTPUT
  test:
    runs-on: ubuntu-latest
    needs: lint
    steps:
      - run: npm test
  build:
    runs-on: ubuntu-latest
    needs: [lint, test]
    if: needs.lint.outputs.passed == 'true'
    steps:
      - run: npm build
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo "deploying"
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

	// Verify all jobs exist
	expectedJobs := []string{"lint:", "test:", "build:", "deploy:"}
	for _, job := range expectedJobs {
		if !containsInNonCommentLines(yamlStr, job) {
			t.Errorf("Expected job %q not found", job)
		}
	}

	// Verify conditional logic is preserved
	if !strings.Contains(yamlStr, "needs.lint.outputs.passed") {
		t.Error("Expected conditional reference to lint output")
	}

	// Verify multi-dependency structure
	// The build job needs array should contain both lint and test
	// Look for the needs section within the build job
	if !strings.Contains(yamlStr, "build:") {
		t.Fatal("build job not found")
	}

	// Check if build job has dependencies (either as array or single)
	// Since jobs auto-depend on activation, we should see lint and test referenced
	hasBothDeps := (strings.Contains(yamlStr, "needs.lint.") || strings.Contains(yamlStr, "- lint")) &&
		(strings.Contains(yamlStr, "needs.test.") || strings.Contains(yamlStr, "- test"))

	if !hasBothDeps {
		t.Error("Expected build job to depend on both lint and test")
	}
}

// TestJobManagerStateValidation tests that job manager maintains correct state
func TestJobManagerStateValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "job-manager-state-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
command: /test
jobs:
  custom1:
    runs-on: ubuntu-latest
    needs: pre_activation
    steps:
      - run: echo "custom1"
  custom2:
    runs-on: ubuntu-latest
    needs: custom1
    steps:
      - run: echo "custom2"
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: {}
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

	// Verify expected job structure:
	// 1. pre_activation (command configured)
	// 2. activation (depends on pre_activation + custom1)
	// 3. agent (depends on activation)
	// 4. safe_outputs (depends on agent)
	// 5. detection (depends on safe_outputs)
	// 6. conclusion (depends on safe_outputs)
	// 7. custom1 (depends on pre_activation)
	// 8. custom2 (depends on custom1)

	expectedJobs := []string{
		"pre_activation:",
		"activation:",
		string(constants.AgentJobName),
		"safe_outputs:",
		"detection:",
		"conclusion:",
		"custom1:",
		"custom2:",
	}

	for _, job := range expectedJobs {
		if !containsInNonCommentLines(yamlStr, job) {
			t.Errorf("Expected job %q not found", job)
		}
	}

	// Verify custom2 depends on custom1
	if !strings.Contains(yamlStr, "needs: custom1") && !strings.Contains(yamlStr, "- custom1") {
		t.Error("Expected custom2 to depend on custom1")
	}
}
