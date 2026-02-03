//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
)

// TestPreActivationCustomSteps tests that custom steps from jobs.pre-activation are imported
func TestPreActivationCustomSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-activation-custom-steps-test")
	compiler := NewCompiler()

	t.Run("custom_steps_imported", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
engine: claude
roles: [admin, maintainer]
jobs:
  pre-activation:
    steps:
      - name: Custom check
        id: custom_check
        run: echo "custom_ok=true" >> $GITHUB_OUTPUT
---

Test workflow with custom pre-activation steps
`

		workflowFile := filepath.Join(tmpDir, "test-custom-steps.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("CompileWorkflow() returned error: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify pre_activation job exists
		if !strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected pre_activation job to be created")
		}

		// Verify custom step is present
		if !strings.Contains(lockContentStr, "Custom check") {
			t.Error("Expected custom step 'Custom check' to be present in pre_activation job")
		}

		// Verify the custom step has the correct id
		if !strings.Contains(lockContentStr, "id: custom_check") {
			t.Error("Expected custom step with id 'custom_check'")
		}
	})

	t.Run("custom_outputs_imported", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
engine: claude
roles: [admin, maintainer]
jobs:
  pre-activation:
    outputs:
      custom_output: "${{ steps.custom_check.outputs.custom_ok }}"
---

Test workflow with custom pre-activation outputs
`

		workflowFile := filepath.Join(tmpDir, "test-custom-outputs.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("CompileWorkflow() returned error: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify custom output is present
		if !strings.Contains(lockContentStr, "custom_output:") {
			t.Error("Expected custom output 'custom_output' to be present in pre_activation job")
		}

		// Verify the activated output is still present
		if !strings.Contains(lockContentStr, "activated:") {
			t.Error("Expected standard 'activated' output to still be present")
		}
	})

	t.Run("steps_and_outputs_together", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
engine: claude
roles: [admin, maintainer]
jobs:
  pre-activation:
    steps:
      - name: Custom check
        id: custom_check
        run: echo "custom_ok=true" >> $GITHUB_OUTPUT
    outputs:
      custom_output: "${{ steps.custom_check.outputs.custom_ok }}"
---

Test workflow with both custom steps and outputs
`

		workflowFile := filepath.Join(tmpDir, "test-steps-and-outputs.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("CompileWorkflow() returned error: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify both custom step and output are present
		if !strings.Contains(lockContentStr, "Custom check") {
			t.Error("Expected custom step to be present")
		}
		if !strings.Contains(lockContentStr, "custom_output:") {
			t.Error("Expected custom output to be present")
		}
	})

	t.Run("unsupported_field_errors", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
engine: claude
roles: [admin, maintainer]
jobs:
  pre-activation:
    runs-on: ubuntu-latest
    steps:
      - name: Custom check
        run: echo "ok"
---

Test workflow with unsupported field in pre-activation
`

		workflowFile := filepath.Join(tmpDir, "test-unsupported-field.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err == nil {
			t.Fatal("Expected error for unsupported field 'runs-on', but got none")
		}

		if !strings.Contains(err.Error(), "unsupported field 'runs-on'") {
			t.Errorf("Expected error about unsupported field 'runs-on', got: %v", err)
		}
	})

	t.Run("pre_activation_not_added_as_custom_job", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
engine: claude
roles: [admin, maintainer]
jobs:
  pre-activation:
    steps:
      - name: Custom check
        run: echo "ok"
  custom_job:
    needs: activation
    runs-on: ubuntu-latest
    steps:
      - run: echo "Custom job"
---

Test that pre-activation is not added as a custom job
`

		workflowFile := filepath.Join(tmpDir, "test-not-custom-job.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("CompileWorkflow() returned error: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Count occurrences of "pre_activation:"
		count := strings.Count(lockContentStr, "pre_activation:")

		// Should appear exactly once (as a job definition, not twice)
		if count != 1 {
			t.Errorf("Expected 'pre_activation:' to appear exactly once, found %d occurrences", count)
		}

		// Verify custom_job is still present
		if !strings.Contains(lockContentStr, "custom_job:") {
			t.Error("Expected custom_job to be present")
		}
	})

	t.Run("import_from_both_variants", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
engine: claude
roles: [admin, maintainer]
jobs:
  pre-activation:
    steps:
      - name: Hyphenated check
        id: hyphen_check
        run: echo "hyphen_ok=true" >> $GITHUB_OUTPUT
    outputs:
      hyphen_status: "${{ steps.hyphen_check.outputs.hyphen_ok }}"
  pre_activation:
    steps:
      - name: Underscore check
        id: underscore_check
        run: echo "underscore_ok=true" >> $GITHUB_OUTPUT
    outputs:
      underscore_status: "${{ steps.underscore_check.outputs.underscore_ok }}"
---

Test that both pre-activation and pre_activation are imported
`

		workflowFile := filepath.Join(tmpDir, "test-both-variants.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("CompileWorkflow() returned error: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify both custom steps are present
		if !strings.Contains(lockContentStr, "Hyphenated check") {
			t.Error("Expected 'Hyphenated check' step to be present")
		}
		if !strings.Contains(lockContentStr, "Underscore check") {
			t.Error("Expected 'Underscore check' step to be present")
		}

		// Verify both custom outputs are present
		if !strings.Contains(lockContentStr, "hyphen_status:") {
			t.Error("Expected 'hyphen_status' output to be present")
		}
		if !strings.Contains(lockContentStr, "underscore_status:") {
			t.Error("Expected 'underscore_status' output to be present")
		}

		// Verify the activated output is still present
		if !strings.Contains(lockContentStr, "activated:") {
			t.Error("Expected standard 'activated' output to still be present")
		}
	})
}

// TestExtractPreActivationCustomFields tests the extractPreActivationCustomFields method directly
func TestExtractPreActivationCustomFields(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		jobs          map[string]any
		expectSteps   int
		expectOutputs int
		expectError   bool
		errorContains string
	}{
		{
			name: "no_pre_activation_job",
			jobs: map[string]any{
				"other_job": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
			expectSteps:   0,
			expectOutputs: 0,
			expectError:   false,
		},
		{
			name: "valid_steps_and_outputs",
			jobs: map[string]any{
				string(constants.PreActivationJobName): map[string]any{
					"steps": []any{
						map[string]any{
							"name": "Test step",
							"run":  "echo test",
						},
					},
					"outputs": map[string]any{
						"test_output": "${{ steps.test.outputs.value }}",
					},
				},
			},
			expectSteps:   1,
			expectOutputs: 1,
			expectError:   false,
		},
		{
			name: "unsupported_field",
			jobs: map[string]any{
				string(constants.PreActivationJobName): map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{"run": "echo test"},
					},
				},
			},
			expectError:   true,
			errorContains: "unsupported field 'runs-on'",
		},
		{
			name: "invalid_steps_type",
			jobs: map[string]any{
				string(constants.PreActivationJobName): map[string]any{
					"steps": "not an array",
				},
			},
			expectError:   true,
			errorContains: "steps must be an array",
		},
		{
			name: "invalid_outputs_type",
			jobs: map[string]any{
				string(constants.PreActivationJobName): map[string]any{
					"outputs": "not an object",
				},
			},
			expectError:   true,
			errorContains: "outputs must be an object",
		},
		{
			name: "invalid_output_value_type",
			jobs: map[string]any{
				string(constants.PreActivationJobName): map[string]any{
					"outputs": map[string]any{
						"test": 123,
					},
				},
			},
			expectError:   true,
			errorContains: "must be a string",
		},
		{
			name: "import_from_both_variants",
			jobs: map[string]any{
				"pre-activation": map[string]any{
					"steps": []any{
						map[string]any{
							"name": "Hyphenated step",
							"run":  "echo hyphen",
						},
					},
					"outputs": map[string]any{
						"hyphen_output": "${{ steps.hyphen.outputs.value }}",
					},
				},
				string(constants.PreActivationJobName): map[string]any{
					"steps": []any{
						map[string]any{
							"name": "Underscore step",
							"run":  "echo underscore",
						},
					},
					"outputs": map[string]any{
						"underscore_output": "${{ steps.underscore.outputs.value }}",
					},
				},
			},
			expectSteps:   2, // Both steps should be imported
			expectOutputs: 2, // Both outputs should be imported
			expectError:   false,
		},
		{
			name: "duplicate_output_key_in_both_variants",
			jobs: map[string]any{
				"pre-activation": map[string]any{
					"outputs": map[string]any{
						"same_key": "from hyphen variant",
					},
				},
				string(constants.PreActivationJobName): map[string]any{
					"outputs": map[string]any{
						"same_key": "from underscore variant",
					},
				},
			},
			expectSteps:   0,
			expectOutputs: 1, // Only one output (last one wins)
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, outputs, err := compiler.extractPreActivationCustomFields(tt.jobs)

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error containing '%s', but got none", tt.errorContains)
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(steps) != tt.expectSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectSteps, len(steps))
			}

			if len(outputs) != tt.expectOutputs {
				t.Errorf("Expected %d outputs, got %d", tt.expectOutputs, len(outputs))
			}
		})
	}
}
