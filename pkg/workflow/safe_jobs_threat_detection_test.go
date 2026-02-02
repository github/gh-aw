//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestSafeOutputsJobsEnableThreatDetectionByDefault verifies that when safe-outputs.jobs
// is configured, threat detection is automatically enabled even if not mentioned in frontmatter
func TestSafeOutputsJobsEnableThreatDetectionByDefault(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that Jobs are parsed
	if len(safeOutputsConfig.Jobs) != 1 {
		t.Fatalf("Expected 1 job in safe-outputs, got %d", len(safeOutputsConfig.Jobs))
	}

	// Verify that threat detection is enabled by default
	// A non-nil ThreatDetection means it's enabled; nil means disabled
	if safeOutputsConfig.ThreatDetection == nil {
		t.Error("Expected threat detection to be enabled by default when safe-outputs.jobs is configured")
	}
}

// TestSafeOutputsJobsRespectExplicitThreatDetectionFalse verifies that when
// threat-detection is explicitly set to false, it respects that setting
func TestSafeOutputsJobsRespectExplicitThreatDetectionFalse(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"threat-detection": false,
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that threat detection respects explicit false
	// When explicitly disabled, ThreatDetection should be nil
	if safeOutputsConfig.ThreatDetection != nil {
		t.Error("Expected threat detection to be disabled (nil) when explicitly set to false")
	}
}

// TestSafeOutputsJobsRespectExplicitThreatDetectionTrue verifies that when
// threat-detection is explicitly set to true, it respects that setting
func TestSafeOutputsJobsRespectExplicitThreatDetectionTrue(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"threat-detection": true,
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that threat detection respects explicit true
	// When explicitly enabled, ThreatDetection should be non-nil
	if safeOutputsConfig.ThreatDetection == nil {
		t.Error("Expected threat detection to be enabled (non-nil) when explicitly set to true")
	}
}

// TestSafeOutputsJobsDependOnDetectionJob verifies that custom safe-output jobs
// depend on the detection job when threat detection is enabled
func TestSafeOutputsJobsDependOnDetectionJob(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				// Non-nil ThreatDetection means enabled
			},
			Jobs: map[string]*SafeJobConfig{
				"my-custom-job": {
					Steps: []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	// Build safe jobs with threat detection enabled
	_, err := c.buildSafeJobs(workflowData, true)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Verify the job depends on both 'agent' and 'detection'
	if len(job.Needs) < 2 {
		t.Errorf("Expected job to have at least 2 dependencies (agent, detection), got %d", len(job.Needs))
	}

	hasAgentDep := false
	hasDetectionDep := false
	for _, dep := range job.Needs {
		if dep == "agent" {
			hasAgentDep = true
		}
		if dep == "detection" {
			hasDetectionDep = true
		}
	}

	if !hasAgentDep {
		t.Error("Expected job to depend on 'agent' job")
	}

	if !hasDetectionDep {
		t.Error("Expected job to depend on 'detection' job when threat detection is enabled")
	}
}

// TestSafeOutputsJobsDoNotDependOnDetectionWhenDisabled verifies that custom safe-output jobs
// do NOT depend on the detection job when threat detection is disabled
func TestSafeOutputsJobsDoNotDependOnDetectionWhenDisabled(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: nil, // nil means disabled
			Jobs: map[string]*SafeJobConfig{
				"my-custom-job": {
					Steps: []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	// Build safe jobs with threat detection disabled
	_, err := c.buildSafeJobs(workflowData, false)
	if err != nil {
		t.Fatalf("Unexpected error building safe jobs: %v", err)
	}

	jobs := c.jobManager.GetAllJobs()
	if len(jobs) != 1 {
		t.Fatalf("Expected 1 job to be created, got %d", len(jobs))
	}

	var job *Job
	for _, j := range jobs {
		job = j
		break
	}

	// Verify the job depends on 'agent' but NOT 'detection'
	hasAgentDep := false
	hasDetectionDep := false
	for _, dep := range job.Needs {
		if dep == "agent" {
			hasAgentDep = true
		}
		if dep == "detection" {
			hasDetectionDep = true
		}
	}

	if !hasAgentDep {
		t.Error("Expected job to depend on 'agent' job")
	}

	if hasDetectionDep {
		t.Error("Expected job NOT to depend on 'detection' job when threat detection is disabled")
	}
}

// TestHasSafeOutputsEnabledWithJobs verifies that HasSafeOutputsEnabled returns true
// when only safe-outputs.jobs is configured (no other safe-outputs)
func TestHasSafeOutputsEnabledWithJobs(t *testing.T) {
	config := &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{
			"my-job": {},
		},
	}

	if !HasSafeOutputsEnabled(config) {
		t.Error("Expected HasSafeOutputsEnabled to return true when safe-outputs.jobs is configured")
	}
}

// TestHasSafeOutputsEnabledWithoutJobs verifies that HasSafeOutputsEnabled returns false
// when safe-outputs exists but has no enabled features
func TestHasSafeOutputsEnabledWithoutJobs(t *testing.T) {
	config := &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{},
	}

	if HasSafeOutputsEnabled(config) {
		t.Error("Expected HasSafeOutputsEnabled to return false when safe-outputs.jobs is empty")
	}
}

// TestSafeJobsWithThreatDetectionConfigObject verifies that threat detection
// configuration object is properly handled
func TestSafeJobsWithThreatDetectionConfigObject(t *testing.T) {
	c := NewCompiler()

	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"threat-detection": map[string]any{
				"enabled": true,
				"prompt":  "Additional security instructions",
			},
			"jobs": map[string]any{
				"my-custom-job": map[string]any{
					"steps": []any{
						map[string]any{
							"run": "echo 'test'",
						},
					},
				},
			},
		},
	}

	safeOutputsConfig := c.extractSafeOutputsConfig(frontmatter)

	if safeOutputsConfig == nil {
		t.Fatal("Expected safe-outputs config to be extracted, got nil")
	}

	// Verify that threat detection is enabled
	// Non-nil ThreatDetection means enabled
	if safeOutputsConfig.ThreatDetection == nil {
		t.Error("Expected threat detection to be enabled (non-nil)")
	}

	// Verify custom prompt is preserved
	if safeOutputsConfig.ThreatDetection.Prompt != "Additional security instructions" {
		t.Errorf("Expected custom prompt to be preserved, got %q", safeOutputsConfig.ThreatDetection.Prompt)
	}
}

// TestSafeJobsIntegrationWithWorkflowCompilation is an integration test that verifies
// the entire workflow compilation process with safe-output jobs and threat detection
func TestSafeJobsIntegrationWithWorkflowCompilation(t *testing.T) {
	c := NewCompiler()

	markdown := `---
on: issues
safe-outputs:
  jobs:
    my-custom-job:
      steps:
        - run: echo "test"
---

# Test Workflow
Test workflow content
`

	// Create temporary test file
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-safe-jobs.md"
	if err := os.WriteFile(testFile, []byte(markdown), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Compile the workflow
	err := c.CompileWorkflow(testFile)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := tmpDir + "/test-safe-jobs.lock.yml"
	workflow, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowStr := string(workflow)

	// Verify detection job is created
	if !strings.Contains(workflowStr, "detection:") {
		t.Error("Expected compiled workflow to contain 'detection:' job")
	}

	// Verify custom safe job is created
	if !strings.Contains(workflowStr, "my_custom_job:") {
		t.Error("Expected compiled workflow to contain 'my_custom_job:' job")
	}

	// Verify custom job depends on detection
	if !strings.Contains(workflowStr, "- detection") {
		t.Error("Expected custom job to depend on detection job")
	}
}
