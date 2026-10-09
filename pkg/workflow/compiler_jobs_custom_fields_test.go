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
// Custom Job Field Configuration Tests (runs-on, strategy, container)
// ========================================

func TestFormatIndentedYAMLFieldPreservesDefaultsAndSequenceIndent(t *testing.T) {
	value := map[string]any{
		"matrix": map[string]any{
			"config": []any{"a", "b"},
		},
	}

	got, err := formatIndentedYAMLField("strategy", value, false)
	if err != nil {
		t.Fatalf("formatIndentedYAMLField() returned error: %v", err)
	}

	want := "strategy:\n      matrix:\n        config:\n          - a\n          - b\n"
	if got != want {
		t.Errorf("formatIndentedYAMLField() = %q, want %q", got, want)
	}

	multiline, err := formatIndentedYAMLField("env", map[string]any{"SCRIPT": "line one\nline two"}, false)
	if err != nil {
		t.Fatalf("formatIndentedYAMLField() returned error for multiline value: %v", err)
	}
	if !strings.Contains(multiline, "      SCRIPT: |-\n        line one\n        line two") {
		t.Errorf("formatIndentedYAMLField() did not preserve literal multiline style: %q", multiline)
	}
}

// TestBuildCustomJobsRunsOnForms tests that runs-on string, array, and object forms
// are all correctly handled in buildCustomJobs.
func TestBuildCustomJobsRunsOnForms(t *testing.T) {
	tests := []struct {
		name             string
		runsOn           any
		expectedRunsOn   string
		expectedContains []string
		shouldErr        bool
	}{
		{
			name:           "string form",
			runsOn:         "ubuntu-latest",
			expectedRunsOn: "runs-on: ubuntu-latest",
		},
		{
			name:             "array form",
			runsOn:           []any{"self-hosted", "linux", "large"},
			expectedContains: []string{"runs-on:", "- self-hosted", "- linux", "- large"},
		},
		{
			name:             "object form",
			runsOn:           map[string]any{"group": "my-runners"},
			expectedContains: []string{"runs-on:", "group: my-runners"},
		},
		{
			name:      "unmarshalable value returns error",
			runsOn:    make(chan int), // channels cannot be marshaled to YAML
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			data := &WorkflowData{
				Name:   "Test Workflow",
				AI:     "copilot",
				RunsOn: "runs-on: ubuntu-latest",
				Jobs: map[string]any{
					"my_job": map[string]any{
						"runs-on": tt.runsOn,
						"steps":   []any{map[string]any{"run": "echo hi"}},
					},
				},
			}

			err := compiler.buildCustomJobs(data, false)
			if tt.shouldErr {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), "my_job") {
					t.Errorf("Expected error to mention job name 'my_job', got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildCustomJobs() returned unexpected error: %v", err)
			}

			job, exists := compiler.jobManager.GetJob("my_job")
			if !exists {
				t.Fatal("Expected my_job to be added")
			}

			if tt.expectedRunsOn != "" {
				if job.RunsOn != tt.expectedRunsOn {
					t.Errorf("RunsOn = %q, want %q", job.RunsOn, tt.expectedRunsOn)
				}
			}
			for _, want := range tt.expectedContains {
				if !strings.Contains(job.RunsOn, want) {
					t.Errorf("RunsOn %q does not contain %q", job.RunsOn, want)
				}
			}
		})
	}
}

// TestBuildCustomJobsNewSimpleFields tests extraction of simple job fields via CompileWorkflow
func TestBuildCustomJobsNewSimpleFields(t *testing.T) {
	tmpDir := testutil.TempDir(t, "new-simple-fields-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  featured_job:
    runs-on: ubuntu-latest
    name: My Display Name
    timeout-minutes: 30
    continue-on-error: true
    concurrency: my-group
    env:
      MY_VAR: hello
      OTHER_VAR: world
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
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Verify display name
	if !strings.Contains(yamlStr, "name: My Display Name") {
		t.Errorf("Expected 'name: My Display Name' in output, got:\n%s", yamlStr)
	}

	// Verify timeout-minutes
	if !strings.Contains(yamlStr, "timeout-minutes: 30") {
		t.Errorf("Expected 'timeout-minutes: 30' in output, got:\n%s", yamlStr)
	}

	// Verify continue-on-error
	if !strings.Contains(yamlStr, "continue-on-error: true") {
		t.Errorf("Expected 'continue-on-error: true' in output, got:\n%s", yamlStr)
	}

	// Verify concurrency (string form)
	if !strings.Contains(yamlStr, "concurrency: my-group") {
		t.Errorf("Expected 'concurrency: my-group' in output, got:\n%s", yamlStr)
	}

	// Verify env variables
	if !strings.Contains(yamlStr, "MY_VAR: hello") {
		t.Errorf("Expected 'MY_VAR: hello' in output, got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "OTHER_VAR: world") {
		t.Errorf("Expected 'OTHER_VAR: world' in output, got:\n%s", yamlStr)
	}
}

// TestBuildCustomJobsWithContainerAndServices tests extraction of container and services fields
func TestBuildCustomJobsWithContainerAndServices(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		Jobs: map[string]any{
			"services_job": map[string]any{
				"runs-on": "ubuntu-latest",
				"container": map[string]any{
					"image": "node:18",
				},
				"services": map[string]any{
					"redis": map[string]any{
						"image": "redis",
						"ports": []any{"6379:6379"},
					},
				},
				"steps": []any{
					map[string]any{"run": "echo 'test'"},
				},
			},
		},
	}

	err := compiler.buildCustomJobs(data, false)
	if err != nil {
		t.Fatalf("buildCustomJobs() returned error: %v", err)
	}

	job, exists := compiler.jobManager.GetJob("services_job")
	if !exists {
		t.Fatal("Expected services_job to be added")
	}

	// Verify container map form
	if !strings.Contains(job.Container, "container:") {
		t.Errorf("Expected 'container:' header, got: %q", job.Container)
	}
	if !strings.Contains(job.Container, "image: node:18") {
		t.Errorf("Expected 'image: node:18' in container, got: %q", job.Container)
	}

	// Verify services
	if !strings.Contains(job.Services, "services:") {
		t.Errorf("Expected 'services:' header, got: %q", job.Services)
	}
	if !strings.Contains(job.Services, "redis:") {
		t.Errorf("Expected 'redis:' in services, got: %q", job.Services)
	}
}

// TestBuildCustomJobsMapConcurrencyAndContainer tests map-form concurrency and string container
func TestBuildCustomJobsMapConcurrencyAndContainer(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		Jobs: map[string]any{
			"map_fields_job": map[string]any{
				"runs-on": "ubuntu-latest",
				"concurrency": map[string]any{
					"group":              "my-group",
					"cancel-in-progress": true,
				},
				"container": "node:18",
				"steps": []any{
					map[string]any{"run": "echo 'test'"},
				},
			},
		},
	}

	err := compiler.buildCustomJobs(data, false)
	if err != nil {
		t.Fatalf("buildCustomJobs() returned error: %v", err)
	}

	job, exists := compiler.jobManager.GetJob("map_fields_job")
	if !exists {
		t.Fatal("Expected map_fields_job to be added")
	}

	// Verify concurrency map form
	if !strings.Contains(job.Concurrency, "concurrency:") {
		t.Errorf("Expected 'concurrency:' header, got: %q", job.Concurrency)
	}
	if !strings.Contains(job.Concurrency, "group: my-group") {
		t.Errorf("Expected 'group: my-group' in concurrency, got: %q", job.Concurrency)
	}

	// Verify container string form
	if job.Container != "container: node:18" {
		t.Errorf("Expected 'container: node:18', got: %q", job.Container)
	}
}

// TestBuildCustomJobsMapConcurrencyDefaultsCancelInProgress tests that map-form concurrency
// without cancel-in-progress defaults to cancel-in-progress: false for non-agent jobs.
func TestBuildCustomJobsMapConcurrencyDefaultsCancelInProgress(t *testing.T) {
	tests := []struct {
		name            string
		concurrencyMap  map[string]any
		wantCancelFalse bool
		wantCancelTrue  bool
	}{
		{
			name: "no cancel-in-progress defaults to false",
			concurrencyMap: map[string]any{
				"group": "my-group",
			},
			wantCancelFalse: true,
			wantCancelTrue:  false,
		},
		{
			name: "explicit cancel-in-progress true is preserved",
			concurrencyMap: map[string]any{
				"group":              "my-group",
				"cancel-in-progress": true,
			},
			wantCancelFalse: false,
			wantCancelTrue:  true,
		},
		{
			name: "explicit cancel-in-progress false is preserved",
			concurrencyMap: map[string]any{
				"group":              "my-group",
				"cancel-in-progress": false,
			},
			wantCancelFalse: true,
			wantCancelTrue:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			data := &WorkflowData{
				Name:   "Test Workflow",
				AI:     "copilot",
				RunsOn: "runs-on: ubuntu-latest",
				Jobs: map[string]any{
					"custom_job": map[string]any{
						"runs-on":     "ubuntu-latest",
						"concurrency": tt.concurrencyMap,
						"steps":       []any{map[string]any{"run": "echo 'test'"}},
					},
				},
			}

			err := compiler.buildCustomJobs(data, false)
			if err != nil {
				t.Fatalf("buildCustomJobs() returned error: %v", err)
			}

			job, exists := compiler.jobManager.GetJob("custom_job")
			if !exists {
				t.Fatal("Expected custom_job to be added")
			}

			if !strings.Contains(job.Concurrency, "concurrency:") {
				t.Errorf("Expected 'concurrency:' header, got: %q", job.Concurrency)
			}
			if !strings.Contains(job.Concurrency, "group: my-group") {
				t.Errorf("Expected 'group: my-group' in concurrency, got: %q", job.Concurrency)
			}
			if tt.wantCancelFalse && !strings.Contains(job.Concurrency, "cancel-in-progress: false") {
				t.Errorf("Expected 'cancel-in-progress: false' in concurrency, got: %q", job.Concurrency)
			}
			if tt.wantCancelTrue && !strings.Contains(job.Concurrency, "cancel-in-progress: true") {
				t.Errorf("Expected 'cancel-in-progress: true' in concurrency, got: %q", job.Concurrency)
			}
			if !tt.wantCancelFalse && strings.Contains(job.Concurrency, "cancel-in-progress: false") {
				t.Errorf("Did not expect 'cancel-in-progress: false' in concurrency, got: %q", job.Concurrency)
			}
			if !tt.wantCancelTrue && strings.Contains(job.Concurrency, "cancel-in-progress: true") {
				t.Errorf("Did not expect 'cancel-in-progress: true' in concurrency, got: %q", job.Concurrency)
			}
		})
	}
}

// TestBuildCustomJobsAllNewFieldsViaWorkflowData tests all 7 new fields via direct WorkflowData
func TestBuildCustomJobsAllNewFieldsViaWorkflowData(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		Jobs: map[string]any{
			"full_job": map[string]any{
				"runs-on":           "ubuntu-latest",
				"name":              "My Display Name",
				"timeout-minutes":   30,
				"continue-on-error": true,
				"concurrency":       "ci-group",
				"env": map[string]any{
					"KEY1": "val1",
					"KEY2": "val2",
				},
				"container": map[string]any{
					"image": "ubuntu:22.04",
				},
				"services": map[string]any{
					"postgres": map[string]any{
						"image": "postgres:15",
					},
				},
				"steps": []any{
					map[string]any{"run": "echo 'test'"},
				},
			},
		},
	}

	err := compiler.buildCustomJobs(data, false)
	if err != nil {
		t.Fatalf("buildCustomJobs() returned error: %v", err)
	}

	job, exists := compiler.jobManager.GetJob("full_job")
	if !exists {
		t.Fatal("Expected full_job to be added")
	}

	// Verify all 7 fields
	if job.DisplayName != "My Display Name" {
		t.Errorf("DisplayName = %q, want 'My Display Name'", job.DisplayName)
	}
	if job.TimeoutMinutes != 30 {
		t.Errorf("TimeoutMinutes = %d, want 30", job.TimeoutMinutes)
	}
	if job.ContinueOnError == nil || !*job.ContinueOnError {
		t.Error("Expected ContinueOnError to be true")
	}
	if job.Concurrency != "concurrency: ci-group" {
		t.Errorf("Concurrency = %q, want 'concurrency: ci-group'", job.Concurrency)
	}
	if job.Env["KEY1"] != "val1" {
		t.Errorf("Env[KEY1] = %q, want 'val1'", job.Env["KEY1"])
	}
	if job.Env["KEY2"] != "val2" {
		t.Errorf("Env[KEY2] = %q, want 'val2'", job.Env["KEY2"])
	}
	if !strings.Contains(job.Container, "container:") {
		t.Errorf("Expected 'container:' in Container, got: %q", job.Container)
	}
	if !strings.Contains(job.Container, "image: ubuntu:22.04") {
		t.Errorf("Expected 'image: ubuntu:22.04' in Container, got: %q", job.Container)
	}
	if !strings.Contains(job.Services, "services:") {
		t.Errorf("Expected 'services:' in Services, got: %q", job.Services)
	}
	if !strings.Contains(job.Services, "postgres:") {
		t.Errorf("Expected 'postgres:' in Services, got: %q", job.Services)
	}

	// Verify the rendered YAML contains all fields
	jm := NewJobManager()
	if err := jm.AddJob(job); err != nil {
		t.Fatalf("AddJob() error: %v", err)
	}
	var renderedBuf strings.Builder
	jm.WriteJobsYAML(&renderedBuf)
	rendered := renderedBuf.String()

	renderedChecks := []string{
		"name: My Display Name",
		"timeout-minutes: 30",
		"continue-on-error: true",
		"concurrency: ci-group",
		"KEY1: val1",
		"KEY2: val2",
		"container:",
		"image: ubuntu:22.04",
		"services:",
		"postgres:",
	}
	for _, check := range renderedChecks {
		if !strings.Contains(rendered, check) {
			t.Errorf("Expected %q in rendered YAML, got:\n%s", check, rendered)
		}
	}
}

func TestBuildCustomJobsTimeoutMinutesExpressionViaWorkflowData(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		Jobs: map[string]any{
			"templated_timeout_job": map[string]any{
				"runs-on":         "ubuntu-latest",
				"timeout-minutes": "${{ inputs.timeout }}",
				"steps": []any{
					map[string]any{"run": "echo 'test'"},
				},
			},
		},
	}

	err := compiler.buildCustomJobs(data, false)
	if err != nil {
		t.Fatalf("buildCustomJobs() returned error: %v", err)
	}

	job, exists := compiler.jobManager.GetJob("templated_timeout_job")
	if !exists {
		t.Fatal("Expected templated_timeout_job to be added")
	}

	if job.TimeoutMinutesExpression != "${{ inputs.timeout }}" {
		t.Errorf("TimeoutMinutesExpression = %q, want %q", job.TimeoutMinutesExpression, "${{ inputs.timeout }}")
	}
	if job.TimeoutMinutes != 0 {
		t.Errorf("TimeoutMinutes = %d, want 0 when expression is used", job.TimeoutMinutes)
	}

	var renderedBuf strings.Builder
	compiler.jobManager.WriteJobsYAML(&renderedBuf)
	rendered := renderedBuf.String()
	if !strings.Contains(rendered, "timeout-minutes: ${{ inputs.timeout }}") {
		t.Errorf("Expected templated timeout-minutes in rendered YAML, got:\n%s", rendered)
	}
}

func TestBuildCustomJobsTimeoutMinutesInvalidStringViaWorkflowData(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		Jobs: map[string]any{
			"invalid_timeout_job": map[string]any{
				"runs-on":         "ubuntu-latest",
				"timeout-minutes": "not-an-expression",
				"steps": []any{
					map[string]any{"run": "echo 'test'"},
				},
			},
		},
	}

	err := compiler.buildCustomJobs(data, false)
	if err == nil {
		t.Fatal("expected error for non-expression timeout-minutes string")
	}
	if !strings.Contains(err.Error(), "timeout-minutes must be an integer or a GitHub Actions expression") {
		t.Fatalf("expected timeout-minutes validation error, got: %v", err)
	}
}
