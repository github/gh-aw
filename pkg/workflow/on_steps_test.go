//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOnSteps tests that on.steps are injected into the pre-activation job and their
// results are wired as pre-activation outputs.
func TestOnSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "on-steps-test")
	compiler := NewCompiler()

	t.Run("on_steps_creates_pre_activation_job", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch: null
  roles: all
  steps:
    - name: Gate check
      id: gate
      run: echo "checking..."
engine: copilot
---

Test workflow with on.steps creating pre-activation job
`
		workflowFile := filepath.Join(tmpDir, "test-on-steps-only.md")
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

		// Verify pre_activation job is created
		if !strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected pre_activation job to be created when on.steps is used")
		}

		// Verify the step is included
		if !strings.Contains(lockContentStr, "Gate check") {
			t.Error("Expected Gate check step to be in pre_activation job")
		}
		if !strings.Contains(lockContentStr, "id: gate") {
			t.Error("Expected gate step ID to be in pre_activation job")
		}

		// Verify the output is wired
		if !strings.Contains(lockContentStr, "gate_result: ${{ steps.gate.outcome }}") {
			t.Errorf("Expected gate_result output to be wired. Lock file:\n%s", lockContentStr)
		}

		// Verify activated output is 'true' when on.steps is the only condition
		if !strings.Contains(lockContentStr, `activated: "true"`) {
			t.Errorf("Expected activated output to be 'true'. Lock file:\n%s", lockContentStr)
		}
		var document struct {
			Jobs map[string]struct {
				Outputs map[string]any `yaml:"outputs"`
			} `yaml:"jobs"`
		}
		require.NoError(t, yaml.Unmarshal(lockContent, &document))
		assert.Equal(t, "true", document.Jobs["pre_activation"].Outputs["activated"])
		assert.NotContains(t, lockContentStr, "activated: ${{ 'true' }}")
		compiled, err := actions.Parse(lockContent)
		require.NoError(t, err)
		diagnostics, err := workflowlint.ObfuscationRule().Check(context.Background(), workflowlint.Input{Workflow: compiled})
		require.NoError(t, err)
		assert.Empty(t, diagnostics)
	})

	t.Run("on_steps_with_other_checks", func(t *testing.T) {
		workflowContent := `---
on:
  issues:
    types: [opened]
  roles: [admin, maintainer]
  steps:
    - name: Gate check
      id: gate
      run: echo "checking..."
engine: copilot
---

Test workflow with on.steps combined with role checks
`
		workflowFile := filepath.Join(tmpDir, "test-on-steps-with-roles.md")
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

		// The on.steps step should be present
		if !strings.Contains(lockContentStr, "Gate check") {
			t.Error("Expected Gate check step to be in pre_activation job")
		}

		// The gate_result output should be wired
		if !strings.Contains(lockContentStr, "gate_result: ${{ steps.gate.outcome }}") {
			t.Errorf("Expected gate_result output. Lock file:\n%s", lockContentStr)
		}

		// The activated output should use the membership check (not 'true')
		if strings.Contains(lockContentStr, `activated: "true"`) {
			t.Error("Expected activated output to use membership check, not 'true'")
		}
		if !strings.Contains(lockContentStr, "check_membership") {
			t.Error("Expected membership check step to be present")
		}
	})

	t.Run("on_steps_multiple_steps_with_ids", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch: null
  roles: all
  steps:
    - name: First check
      id: first
      run: echo "first..."
    - name: Second check
      id: second
      run: echo "second..."
    - name: No ID step
      run: echo "no id"
engine: copilot
---

Test workflow with multiple on.steps
`
		workflowFile := filepath.Join(tmpDir, "test-on-steps-multi.md")
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

		// Both step IDs should have outputs
		if !strings.Contains(lockContentStr, "first_result: ${{ steps.first.outcome }}") {
			t.Errorf("Expected first_result output. Lock file:\n%s", lockContentStr)
		}
		if !strings.Contains(lockContentStr, "second_result: ${{ steps.second.outcome }}") {
			t.Errorf("Expected second_result output. Lock file:\n%s", lockContentStr)
		}

		// All three steps should be present
		if !strings.Contains(lockContentStr, "First check") {
			t.Error("Expected First check step to be in pre_activation job")
		}
		if !strings.Contains(lockContentStr, "Second check") {
			t.Error("Expected Second check step to be in pre_activation job")
		}
		if !strings.Contains(lockContentStr, "No ID step") {
			t.Error("Expected No ID step to be in pre_activation job")
		}
	})

	t.Run("on_steps_step_appended_after_builtin_checks", func(t *testing.T) {
		workflowContent := `---
on:
  issues:
    types: [opened]
  roles: [admin, maintainer]
  steps:
    - name: Gate check
      id: gate
      run: echo "checking..."
engine: copilot
---

Test on.steps are appended after built-in checks
`
		workflowFile := filepath.Join(tmpDir, "test-on-steps-order.md")
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

		// The membership check step should appear before the gate step in the pre_activation steps.
		// Search for step-level indented IDs (8 spaces = within steps array under pre_activation job)
		membershipStepIdx := strings.Index(lockContentStr, "        id: check_membership")
		gateStepIdx := strings.Index(lockContentStr, "        id: gate")
		if membershipStepIdx == -1 || gateStepIdx == -1 {
			t.Fatalf("Expected both check_membership step and gate step to be present. Lock file:\n%s", lockContentStr)
		}
		if membershipStepIdx > gateStepIdx {
			t.Error("Expected membership check step to appear before on.steps gate step in pre_activation job")
		}
	})

	t.Run("on_steps_with_memory_stores_restored_in_pre_activation", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch: null
  roles: all
  restore-memory: true
  steps:
    - name: Gate check
      id: gate
      run: echo "checking..."
tools:
  cache-memory: true
  drive-memory: true
  repo-memory: true
  comment-memory: true
engine: copilot
---

Test on.steps with memory stores restored in pre-activation
`
		workflowFile := filepath.Join(tmpDir, "test-on-steps-memory-pre-activation.md")
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
		preActivationStart := strings.Index(lockContentStr, "\n  pre_activation:\n")
		if preActivationStart == -1 {
			preActivationStart = strings.Index(lockContentStr, "  pre_activation:\n")
		}
		if preActivationStart == -1 {
			t.Fatalf("Expected pre_activation section in lock file. Lock file:\n%s", lockContentStr)
		}

		sectionBodyStart := preActivationStart
		if strings.HasPrefix(lockContentStr[preActivationStart:], "\n") {
			sectionBodyStart++
		}
		jobStartPattern := regexp.MustCompile(`\n  [A-Za-z0-9_-]+:\n`)
		searchStart := sectionBodyStart + len("  pre_activation:\n")
		nextJobLoc := jobStartPattern.FindStringIndex(lockContentStr[searchStart:])
		var preActivationSection string
		if nextJobLoc == nil {
			preActivationSection = lockContentStr[sectionBodyStart:]
		} else {
			preActivationSection = lockContentStr[sectionBodyStart : searchStart+nextJobLoc[0]+1]
		}
		if preActivationSection == "" {
			t.Fatalf("Expected pre_activation section in lock file. Lock file:\n%s", lockContentStr)
		}

		// Memory stores should be restored before on.steps run in pre-activation.
		assert.Contains(t, lockContentStr, "# restore-memory: true # Restore-memory enables pre-activation memory restore")
		assert.Contains(t, preActivationSection, "Restore cache-memory file share data")
		assert.Contains(t, preActivationSection, "Checkout drive-memory file share (default)")
		assert.Contains(t, preActivationSection, "runs-on: ubuntu-latest")
		assert.Contains(t, preActivationSection, "Clone repo-memory branch (default)")
		assert.Contains(t, preActivationSection, "Write comment-memory configuration")
		assert.Contains(t, preActivationSection, "Prepare comment memory files")
		assert.Contains(t, preActivationSection, "        id: gate")

		cacheRestoreIdx := strings.Index(preActivationSection, "Restore cache-memory file share data")
		repoRestoreIdx := strings.Index(preActivationSection, "Clone repo-memory branch (default)")
		commentMemoryConfigIdx := strings.Index(preActivationSection, "Write comment-memory configuration")
		commentMemoryIdx := strings.Index(preActivationSection, "Prepare comment memory files")
		gateStepIdx := strings.Index(preActivationSection, "        id: gate")
		if cacheRestoreIdx == -1 || repoRestoreIdx == -1 || commentMemoryConfigIdx == -1 || commentMemoryIdx == -1 || gateStepIdx == -1 {
			t.Fatalf("Expected memory restore steps and gate step in pre_activation section. Section:\n%s", preActivationSection)
		}
		assert.Less(t, cacheRestoreIdx, gateStepIdx, "cache-memory should be restored before on.steps")
		assert.Less(t, repoRestoreIdx, gateStepIdx, "repo-memory should be restored before on.steps")
		assert.Less(t, commentMemoryConfigIdx, commentMemoryIdx, "comment-memory config should be written before restoring comment-memory files")
		assert.Less(t, commentMemoryIdx, gateStepIdx, "comment-memory should be restored before on.steps")

		// Pre-activation must not include write-back/commit steps.
		assert.NotContains(t, preActivationSection, "Commit cache-memory changes")
		assert.NotContains(t, preActivationSection, "actions/gh-drives-preview/commit@")
		assert.NotContains(t, preActivationSection, "Push repo-memory changes")
		assert.Contains(t, preActivationSection, "uses: "+getActionPin("actions/cache/restore"))
		assert.NotContains(t, preActivationSection, "uses: actions/cache@")
		assert.NotContains(t, preActivationSection, "uses: "+getActionPin("actions/cache"))
	})

	t.Run("on_steps_without_restore_memory_does_not_restore_stores_in_pre_activation", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch: null
  roles: all
  steps:
    - name: Gate check
      id: gate
      run: echo "checking..."
tools:
  cache-memory: true
  repo-memory: true
  comment-memory: true
engine: copilot
---

Test on.steps without on.restore-memory
`
		workflowFile := filepath.Join(tmpDir, "test-on-steps-memory-pre-activation-disabled.md")
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
		preActivationStart := strings.Index(lockContentStr, "\n  pre_activation:\n")
		if preActivationStart == -1 {
			preActivationStart = strings.Index(lockContentStr, "  pre_activation:\n")
		}
		if preActivationStart == -1 {
			t.Fatalf("Expected pre_activation section in lock file. Lock file:\n%s", lockContentStr)
		}

		sectionBodyStart := preActivationStart
		if strings.HasPrefix(lockContentStr[preActivationStart:], "\n") {
			sectionBodyStart++
		}
		jobStartPattern := regexp.MustCompile(`\n  [A-Za-z0-9_-]+:\n`)
		searchStart := sectionBodyStart + len("  pre_activation:\n")
		nextJobLoc := jobStartPattern.FindStringIndex(lockContentStr[searchStart:])
		var preActivationSection string
		if nextJobLoc == nil {
			preActivationSection = lockContentStr[sectionBodyStart:]
		} else {
			preActivationSection = lockContentStr[sectionBodyStart : searchStart+nextJobLoc[0]+1]
		}
		if preActivationSection == "" {
			t.Fatalf("Expected pre_activation section in lock file. Lock file:\n%s", lockContentStr)
		}

		assert.Contains(t, preActivationSection, "        id: gate")
		assert.NotContains(t, preActivationSection, "Restore cache-memory file share data")
		assert.NotContains(t, preActivationSection, "Clone repo-memory branch (default)")
		assert.NotContains(t, preActivationSection, "Prepare comment memory files")
	})
}

// TestExtractOnSteps tests the extractOnSteps function directly
func TestExtractOnSteps(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   map[string]any
		expectSteps   int
		expectError   bool
		errorContains string
	}{
		{
			name:        "no_on_section",
			frontmatter: map[string]any{},
			expectSteps: 0,
			expectError: false,
		},
		{
			name: "on_section_string",
			frontmatter: map[string]any{
				"on": "push",
			},
			expectSteps: 0,
			expectError: false,
		},
		{
			name: "on_section_without_steps",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": nil,
				},
			},
			expectSteps: 0,
			expectError: false,
		},
		{
			name: "on_steps_with_steps",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": nil,
					"steps": []any{
						map[string]any{"name": "Step 1", "id": "step1", "run": "echo ok"},
						map[string]any{"name": "Step 2", "run": "echo ok2"},
					},
				},
			},
			expectSteps: 2,
			expectError: false,
		},
		{
			name: "on_steps_invalid_type",
			frontmatter: map[string]any{
				"on": map[string]any{
					"steps": "not an array",
				},
			},
			expectError:   true,
			errorContains: "on.steps must be an array",
		},
		{
			name: "on_steps_invalid_step_type",
			frontmatter: map[string]any{
				"on": map[string]any{
					"steps": []any{"not a map"},
				},
			},
			expectError:   true,
			errorContains: "on.steps[0] must be an object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := extractOnSteps(tt.frontmatter)

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
		})
	}
}

// TestExtractOnPermissions tests the extractOnPermissions function directly
func TestExtractOnPermissions(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  map[string]any
		expectNil    bool
		expectScopes map[string]string // scope -> level
	}{
		{
			name:        "no_on_section",
			frontmatter: map[string]any{},
			expectNil:   true,
		},
		{
			name: "on_section_string",
			frontmatter: map[string]any{
				"on": "push",
			},
			expectNil: true,
		},
		{
			name: "on_section_without_permissions",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": nil,
				},
			},
			expectNil: true,
		},
		{
			name: "on_permissions_issues_read",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": nil,
					"permissions": map[string]any{
						"issues": "read",
					},
				},
			},
			expectNil:    false,
			expectScopes: map[string]string{"issues": "read"},
		},
		{
			name: "on_permissions_multiple_scopes",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": nil,
					"permissions": map[string]any{
						"issues":        "read",
						"pull-requests": "read",
					},
				},
			},
			expectNil:    false,
			expectScopes: map[string]string{"issues": "read", "pull-requests": "read"},
		},
		{
			name: "on_permissions_vulnerability_alerts",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
					"permissions": map[string]any{
						"vulnerability-alerts": "read",
					},
				},
			},
			expectNil:    false,
			expectScopes: map[string]string{"vulnerability-alerts": "read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := extractOnPermissions(tt.frontmatter)
			if tt.expectNil {
				if perms != nil {
					t.Errorf("Expected nil permissions, got non-nil")
				}
				return
			}
			if perms == nil {
				t.Fatalf("Expected non-nil permissions, got nil")
			}
			for scope, wantLevel := range tt.expectScopes {
				gotLevel, ok := perms.Get(convertStringToPermissionScope(scope))
				if !ok {
					t.Errorf("Expected scope %s to be set", scope)
					continue
				}
				if string(gotLevel) != wantLevel {
					t.Errorf("Expected scope %s = %s, got %s", scope, wantLevel, gotLevel)
				}
			}
		})
	}
}

// TestExtractOnNeeds tests the extractOnNeeds function directly.
func TestExtractOnNeeds(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   map[string]any
		expected      []string
		expectError   bool
		errorContains string
	}{
		{
			name:        "no_on_section",
			frontmatter: map[string]any{},
			expected:    nil,
		},
		{
			name: "on_section_string",
			frontmatter: map[string]any{
				"on": "push",
			},
			expected: nil,
		},
		{
			name: "on_section_without_needs",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
				},
			},
			expected: nil,
		},
		{
			name: "on_needs_valid",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
					"needs":             []any{"secrets_fetcher", "config_loader"},
				},
			},
			expected: []string{"secrets_fetcher", "config_loader"},
		},
		{
			name: "on_needs_wrong_type",
			frontmatter: map[string]any{
				"on": map[string]any{
					"needs": "secrets_fetcher",
				},
			},
			expectError:   true,
			errorContains: "on.needs must be an array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needs, err := extractOnNeeds(tt.frontmatter)
			if tt.expectError {
				require.Error(t, err, "expected extraction error")
				require.ErrorContains(t, err, tt.errorContains, "error should contain expected text")
				return
			}

			require.NoError(t, err, "expected extraction to succeed")
			assert.Equal(t, tt.expected, needs, "unexpected on.needs result")
		})
	}
}

func TestExtractOnRestoreMemory(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   map[string]any
		expected      bool
		expectError   bool
		errorContains string
	}{
		{
			name:        "no_on_section",
			frontmatter: map[string]any{},
			expected:    false,
		},
		{
			name: "on_section_string",
			frontmatter: map[string]any{
				"on": "push",
			},
			expected: false,
		},
		{
			name: "on_without_restore_memory",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
				},
			},
			expected: false,
		},
		{
			name: "restore_memory_true",
			frontmatter: map[string]any{
				"on": map[string]any{
					"restore-memory": true,
				},
			},
			expected: true,
		},
		{
			name: "restore_memory_false",
			frontmatter: map[string]any{
				"on": map[string]any{
					"restore-memory": false,
				},
			},
			expected: false,
		},
		{
			name: "restore_memory_wrong_type",
			frontmatter: map[string]any{
				"on": map[string]any{
					"restore-memory": "true",
				},
			},
			expectError:   true,
			errorContains: "on.restore-memory must be a boolean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreMemory, err := extractOnRestoreMemory(tt.frontmatter)
			if tt.expectError {
				require.Error(t, err, "expected extraction error")
				require.ErrorContains(t, err, tt.errorContains, "error should contain expected text")
				return
			}

			require.NoError(t, err, "expected extraction to succeed")
			assert.Equal(t, tt.expected, restoreMemory, "unexpected on.restore-memory result")
		})
	}
}

// TestOnPermissionsAppliedToPreActivation tests that on.permissions are applied to the pre-activation job
func TestOnPermissionsAppliedToPreActivation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "on-permissions-test")
	compiler := NewCompiler()

	workflowContent := `---
on:
  workflow_dispatch: null
  roles: [admin]
  permissions:
    issues: read
    pull-requests: read
  steps:
    - name: Check something
      id: check
      uses: actions/github-script@v8
      with:
        script: |
          const issues = await github.rest.issues.listForRepo({ owner: context.repo.owner, repo: context.repo.repo });
          core.setOutput('count', issues.data.length);
engine: copilot
---

Workflow with on.permissions
`
	workflowFile := filepath.Join(tmpDir, "test-on-permissions.md")
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

	// The pre_activation job should have issues: read and pull-requests: read permissions
	if !strings.Contains(lockContentStr, "issues: read") {
		t.Error("Expected issues: read permission in pre_activation job")
	}
	if !strings.Contains(lockContentStr, "pull-requests: read") {
		t.Error("Expected pull-requests: read permission in pre_activation job")
	}

	// The on.permissions should be commented out in the compiled on: section
	if !strings.Contains(lockContentStr, "# permissions:") {
		t.Error("Expected on.permissions to be commented out in the on: section")
	}
}

// TestReferencesPreActivationOutputs tests the referencesPreActivationOutputs function
func TestReferencesPreActivationOutputs(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		expected  bool
	}{
		{
			name:      "empty_condition",
			condition: "",
			expected:  false,
		},
		{
			name:      "references_pre_activation_outputs",
			condition: "needs.pre_activation.outputs.has_issues == 'true'",
			expected:  true,
		},
		{
			name:      "references_custom_job_outputs",
			condition: "needs.search_issues.outputs.has_issues == 'true'",
			expected:  false,
		},
		{
			name:      "references_pre_activation_activated",
			condition: "needs.pre_activation.outputs.activated == 'true'",
			expected:  true,
		},
		{
			name:      "references_pre_activation_result_not_outputs",
			condition: "needs.pre_activation.result == 'success'",
			expected:  false,
		},
		{
			name:      "references_pre_activation_arbitrary_output",
			condition: "needs.pre_activation.outputs.custom_gate_check == 'passed'",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := referencesPreActivationOutputs(tt.condition)
			if result != tt.expected {
				t.Errorf("referencesPreActivationOutputs(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}
