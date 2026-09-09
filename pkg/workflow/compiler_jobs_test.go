//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
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
// Helper Function Tests
// ========================================

// TestReferencesCustomJobOutputsAdditional tests additional edge cases for referencesCustomJobOutputs method
func TestReferencesCustomJobOutputsAdditional(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name       string
		condition  string
		customJobs map[string]any
		expected   bool
	}{
		{
			name:       "references non-existent job",
			condition:  "needs.job2.outputs.value",
			customJobs: map[string]any{"job1": map[string]any{}},
			expected:   false,
		},
		{
			name:       "multiple custom jobs with reference",
			condition:  "needs.producer.outputs.result",
			customJobs: map[string]any{"producer": map[string]any{}, "consumer": map[string]any{}},
			expected:   true,
		},
		{
			name:       "complex condition with output reference",
			condition:  "needs.test.outputs.status == 'pass' && github.ref == 'refs/heads/main'",
			customJobs: map[string]any{"test": map[string]any{}},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.referencesCustomJobOutputs(tt.condition, tt.customJobs)
			if result != tt.expected {
				t.Errorf("referencesCustomJobOutputs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestJobDependsOnPreActivationEdgeCases tests edge cases for jobDependsOnPreActivation function
func TestJobDependsOnPreActivationEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		jobConfig map[string]any
		expected  bool
	}{
		{
			name: "needs is invalid type",
			jobConfig: map[string]any{
				"needs": 123,
			},
			expected: false,
		},
		{
			name: "array with non-string element",
			jobConfig: map[string]any{
				"needs": []any{123, "pre_activation"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobDependsOnPreActivation(tt.jobConfig)
			if result != tt.expected {
				t.Errorf("jobDependsOnPreActivation() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestJobDependsOnAgentEdgeCases tests edge cases for jobDependsOnAgent function
func TestJobDependsOnAgentEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		jobConfig map[string]any
		expected  bool
	}{
		{
			name: "array with mixed types including agent",
			jobConfig: map[string]any{
				"needs": []any{123, "agent", "job2"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobDependsOnAgent(tt.jobConfig)
			if result != tt.expected {
				t.Errorf("jobDependsOnAgent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGetCustomJobsDependingOnPreActivationEdgeCases tests edge cases for getCustomJobsDependingOnPreActivation method
func TestGetCustomJobsDependingOnPreActivationEdgeCases(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		customJobs     map[string]any
		expectedCount  int
		expectedJobIDs []string
	}{
		{
			name: "job with invalid config type",
			customJobs: map[string]any{
				"job1": "invalid",
				"job2": map[string]any{"needs": "pre_activation"},
			},
			expectedCount:  1,
			expectedJobIDs: []string{"job2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.getCustomJobsDependingOnPreActivation(tt.customJobs)
			if len(result) != tt.expectedCount {
				t.Errorf("getCustomJobsDependingOnPreActivation() returned %d jobs, want %d", len(result), tt.expectedCount)
			}
			// Check that expected job IDs are present
			for _, expectedID := range tt.expectedJobIDs {
				found := slices.Contains(result, expectedID)
				if !found {
					t.Errorf("Expected job %q not found in result", expectedID)
				}
			}
		})
	}
}

// TestJobDependsOnActivation tests the jobDependsOnActivation function
func TestJobDependsOnActivation(t *testing.T) {
	tests := []struct {
		name      string
		jobConfig map[string]any
		expected  bool
	}{
		{
			name:      "no needs field",
			jobConfig: map[string]any{"runs-on": "ubuntu-latest"},
			expected:  false,
		},
		{
			name:      "needs: activation as string",
			jobConfig: map[string]any{"needs": "activation"},
			expected:  true,
		},
		{
			name:      "needs: pre_activation only",
			jobConfig: map[string]any{"needs": "pre_activation"},
			expected:  false,
		},
		{
			name:      "needs: agent only",
			jobConfig: map[string]any{"needs": "agent"},
			expected:  false,
		},
		{
			name: "needs: [activation, pre_activation] array",
			jobConfig: map[string]any{
				"needs": []any{"pre_activation", "activation"},
			},
			expected: true,
		},
		{
			name: "needs: array without activation",
			jobConfig: map[string]any{
				"needs": []any{"pre_activation", "config"},
			},
			expected: false,
		},
		{
			name: "needs: array with mixed types including activation",
			jobConfig: map[string]any{
				"needs": []any{123, "activation"},
			},
			expected: true,
		},
		{
			name:      "needs: invalid type",
			jobConfig: map[string]any{"needs": 123},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobDependsOnActivation(tt.jobConfig)
			if result != tt.expected {
				t.Errorf("jobDependsOnActivation() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGetCustomJobsDependingOnPreActivationExcludesActivationDependents tests that
// getCustomJobsDependingOnPreActivation excludes jobs that also depend on activation.
// This prevents the compiler from adding such jobs to activation's needs (which would
// create a circular dependency: activation → job → activation).
func TestGetCustomJobsDependingOnPreActivationExcludesActivationDependents(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		customJobs   map[string]any
		expectedJobs []string
		excludedJobs []string
	}{
		{
			name: "job with both pre_activation and activation is excluded",
			customJobs: map[string]any{
				"config": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   []any{"pre_activation", "activation"},
				},
			},
			expectedJobs: []string{},
			excludedJobs: []string{"config"},
		},
		{
			name: "job with only pre_activation is included",
			customJobs: map[string]any{
				"precompute": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   []any{"pre_activation"},
				},
			},
			expectedJobs: []string{"precompute"},
			excludedJobs: []string{},
		},
		{
			name: "mixed: pre_activation-only included, pre_activation+activation excluded",
			customJobs: map[string]any{
				"precompute": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   "pre_activation",
				},
				"config": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   []any{"pre_activation", "activation"},
				},
				"release": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   []any{"pre_activation", "activation", "config"},
				},
			},
			expectedJobs: []string{"precompute"},
			excludedJobs: []string{"config", "release"},
		},
		{
			name: "job with only activation dependency is excluded",
			customJobs: map[string]any{
				"post_job": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   "activation",
				},
			},
			expectedJobs: []string{},
			excludedJobs: []string{"post_job"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.getCustomJobsDependingOnPreActivation(tt.customJobs)
			resultSet := make(map[string]struct{}, len(result))
			for _, j := range result {
				resultSet[j] = struct{}{}
			}
			for _, expected := range tt.expectedJobs {
				if _, ok := resultSet[expected]; !ok {
					t.Errorf("Expected job %q in result, got: %v", expected, result)
				}
			}
			for _, excluded := range tt.excludedJobs {
				if _, ok := resultSet[excluded]; ok {
					t.Errorf("Job %q should be excluded from result, got: %v", excluded, result)
				}
			}
		})
	}
}

// TestBuildCustomJobsDoesNotAutoAddActivationToOutputReferencedJobs tests that
// buildCustomJobs does NOT auto-add needs: activation to custom jobs whose outputs
// are referenced in the markdown body (they must run before activation).
func TestBuildCustomJobsDoesNotAutoAddActivationToOutputReferencedJobs(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	// Add activation job to manager
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	if err := compiler.jobManager.AddJob(activationJob); err != nil {
		t.Fatal(err)
	}

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		// precompute has no explicit needs and its output is referenced in the markdown
		MarkdownContent: "Action: ${{ needs.precompute.outputs.action }}",
		Jobs: map[string]any{
			"precompute": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{"run": "echo 'precompute'"},
				},
				// No explicit needs — normally would auto-get needs: activation
				// But since precompute's output is referenced in markdown, it should NOT
			},
		},
	}

	err := compiler.buildCustomJobs(data, true)
	if err != nil {
		t.Fatalf("buildCustomJobs() returned error: %v", err)
	}

	job, exists := compiler.jobManager.GetJob("precompute")
	if !exists {
		t.Fatal("Expected precompute job to be added")
	}

	for _, need := range job.Needs {
		if need == string(constants.ActivationJobName) {
			t.Errorf("precompute job should NOT have needs: activation when its output is referenced in markdown (it must run before activation)")
		}
	}
}

func TestRuntimeImportReferencedJobOutputRunsBeforeActivation(t *testing.T) {
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	promptsDir := filepath.Join(tmpDir, ".github", "prompts")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	workflowPath := filepath.Join(workflowsDir, "runtime-import-output.md")
	if err := os.WriteFile(workflowPath, []byte(`---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
jobs:
  select:
    runs-on: ubuntu-latest
    outputs:
      issue_numbers: ${{ steps.values.outputs.issue_numbers }}
      marker: ${{ steps.values.outputs.marker }}
    steps:
      - id: values
        shell: bash
        run: |
          echo 'issue_numbers=[]' >> "$GITHUB_OUTPUT"
          echo 'marker=ordinary-string' >> "$GITHUB_OUTPUT"
---

{{#runtime-import .github/prompts/runtime-import-output.md}}
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "runtime-import-output.md"), []byte(`# Runtime import interpolation reproducer

Issue numbers: ${{ needs.select.outputs.issue_numbers }}
Marker: ${{ needs.select.outputs.marker }}
`), 0600); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler(WithVersion("test"))
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "runtime-import-output.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lock := string(lockContent)
	if !strings.Contains(lock, "  activation:\n    needs: select") {
		t.Fatalf("activation should depend on runtime-import referenced job; lock excerpt:\n%s", lock)
	}
	if !strings.Contains(lock, "GH_AW_NEEDS_SELECT_OUTPUTS_ISSUE_NUMBERS: ${{ needs.select.outputs.issue_numbers }}") {
		t.Errorf("lock file missing issue_numbers env mapping")
	}
	if !strings.Contains(lock, "GH_AW_NEEDS_SELECT_OUTPUTS_MARKER: ${{ needs.select.outputs.marker }}") {
		t.Errorf("lock file missing marker env mapping")
	}
	if strings.Contains(lock, "  select:\n    needs: activation") {
		t.Errorf("runtime-import referenced select job should not depend on activation")
	}
}

func TestGetReferencedCustomJobs(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		content        string
		customJobs     map[string]any
		expectedCount  int
		expectedJobIDs []string
	}{
		{
			name:           "empty content",
			content:        "",
			customJobs:     map[string]any{"job1": map[string]any{}},
			expectedCount:  0,
			expectedJobIDs: []string{},
		},
		{
			name:           "nil custom jobs",
			content:        "needs.job1.outputs.value",
			customJobs:     nil,
			expectedCount:  0,
			expectedJobIDs: []string{},
		},
		{
			name:           "references one job output",
			content:        "needs.producer.outputs.value",
			customJobs:     map[string]any{"producer": map[string]any{}, "consumer": map[string]any{}},
			expectedCount:  1,
			expectedJobIDs: []string{"producer"},
		},
		{
			name:           "references job result",
			content:        "needs.test_job.result == 'success'",
			customJobs:     map[string]any{"test_job": map[string]any{}},
			expectedCount:  1,
			expectedJobIDs: []string{"test_job"},
		},
		{
			name:           "references multiple jobs",
			content:        "needs.job1.outputs.a && needs.job2.outputs.b",
			customJobs:     map[string]any{"job1": map[string]any{}, "job2": map[string]any{}},
			expectedCount:  2,
			expectedJobIDs: []string{"job1", "job2"},
		},
		{
			name:           "no job references",
			content:        "github.event_name == 'push'",
			customJobs:     map[string]any{"job1": map[string]any{}},
			expectedCount:  0,
			expectedJobIDs: []string{},
		},
		{
			name:           "references non-existent job",
			content:        "needs.unknown.outputs.value",
			customJobs:     map[string]any{"job1": map[string]any{}},
			expectedCount:  0,
			expectedJobIDs: []string{},
		},
		{
			name:           "github expression format",
			content:        "${{ needs.check.outputs.status }}",
			customJobs:     map[string]any{"check": map[string]any{}},
			expectedCount:  1,
			expectedJobIDs: []string{"check"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.getReferencedCustomJobs(tt.content, tt.customJobs)
			if len(result) != tt.expectedCount {
				t.Errorf("getReferencedCustomJobs() returned %d jobs, want %d", len(result), tt.expectedCount)
			}
			// Check that expected job IDs are present
			for _, expectedID := range tt.expectedJobIDs {
				found := slices.Contains(result, expectedID)
				if !found {
					t.Errorf("Expected job %q not found in result", expectedID)
				}
			}
		})
	}
}

// TestShouldAddCheckoutStep tests the shouldAddCheckoutStep method
func TestShouldAddCheckoutStep(t *testing.T) {
	tests := []struct {
		name       string
		data       *WorkflowData
		actionMode ActionMode
		expected   bool
	}{
		{
			name: "custom steps with checkout",
			data: &WorkflowData{
				CustomSteps: "- uses: actions/checkout@v4",
			},
			actionMode: ActionModeDev,
			expected:   false,
		},
		{
			name: "custom steps without checkout",
			data: &WorkflowData{
				CustomSteps: "- run: echo 'test'",
			},
			actionMode: ActionModeDev,
			expected:   true,
		},
		{
			name: "agent file specified",
			data: &WorkflowData{
				AgentFile: ".github/agents/custom.md",
			},
			actionMode: ActionModeRelease,
			expected:   true,
		},
		{
			name: "release mode without agent file",
			data: &WorkflowData{
				CustomSteps: "",
			},
			actionMode: ActionModeRelease,
			expected:   true, // Checkout always needed unless already in steps
		},
		{
			name: "dev mode without agent file",
			data: &WorkflowData{
				CustomSteps: "",
			},
			actionMode: ActionModeDev,
			expected:   true,
		},
		{
			name: "script mode without agent file",
			data: &WorkflowData{
				CustomSteps: "",
			},
			actionMode: ActionModeScript,
			expected:   true,
		},
		{
			name:       "uninitialized mode",
			data:       &WorkflowData{},
			actionMode: ActionMode(""),
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.actionMode = tt.actionMode
			result := compiler.shouldAddCheckoutStep(tt.data)
			if result != tt.expected {
				t.Errorf("shouldAddCheckoutStep() = %v, want %v (actionMode=%v)", result, tt.expected, tt.actionMode)
			}
		})
	}
}
