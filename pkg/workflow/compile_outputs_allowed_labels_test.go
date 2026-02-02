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

func TestAllowedLabelsConfigParsing(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectIssue   []string
		expectDiscuss []string
		expectPR      []string
	}{
		{
			name: "create-issue with allowed-labels",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    allowed-labels: [bug, enhancement, documentation]
---

# Test Allowed Labels for Issues

This workflow tests the allowed-labels configuration for create-issue.
`,
			expectIssue: []string{"bug", "enhancement", "documentation"},
		},
		{
			name: "create-discussion with allowed-labels",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  discussions: write
engine: claude
strict: false
safe-outputs:
  create-discussion:
    allowed-labels: [question, idea]
---

# Test Allowed Labels for Discussions

This workflow tests the allowed-labels configuration for create-discussion.
`,
			expectDiscuss: []string{"question", "idea"},
		},
		{
			name: "create-pull-request with allowed-labels",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-pull-request:
    allowed-labels: [automated, needs-review]
---

# Test Allowed Labels for Pull Requests

This workflow tests the allowed-labels configuration for create-pull-request.
`,
			expectPR: []string{"automated", "needs-review"},
		},
		{
			name: "all safe outputs with allowed-labels",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: write
  issues: write
  discussions: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    allowed-labels: [bug, feature]
  create-discussion:
    allowed-labels: [general, help]
  create-pull-request:
    allowed-labels: [pr-label]
---

# Test All Safe Outputs with Allowed Labels

This workflow tests allowed-labels for all safe outputs.
`,
			expectIssue:   []string{"bug", "feature"},
			expectDiscuss: []string{"general", "help"},
			expectPR:      []string{"pr-label"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir := testutil.TempDir(t, "allowed-labels-test")

			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Parse the workflow data
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			if err != nil {
				t.Fatalf("Unexpected error parsing workflow: %v", err)
			}

			// Verify safe-outputs configuration is parsed
			if workflowData.SafeOutputs == nil {
				t.Fatal("Expected safe-outputs configuration to be parsed")
			}

			// Check create-issue allowed-labels
			if tt.expectIssue != nil {
				if workflowData.SafeOutputs.CreateIssues == nil {
					t.Fatal("Expected create-issue configuration to be parsed")
				}
				if len(workflowData.SafeOutputs.CreateIssues.AllowedLabels) != len(tt.expectIssue) {
					t.Errorf("Expected %d allowed labels for issues, got %d",
						len(tt.expectIssue), len(workflowData.SafeOutputs.CreateIssues.AllowedLabels))
				}
				for i, expected := range tt.expectIssue {
					if i >= len(workflowData.SafeOutputs.CreateIssues.AllowedLabels) ||
						workflowData.SafeOutputs.CreateIssues.AllowedLabels[i] != expected {
						t.Errorf("Expected issue allowed label[%d] to be '%s', got '%s'",
							i, expected, workflowData.SafeOutputs.CreateIssues.AllowedLabels[i])
					}
				}
			}

			// Check create-discussion allowed-labels
			if tt.expectDiscuss != nil {
				if workflowData.SafeOutputs.CreateDiscussions == nil {
					t.Fatal("Expected create-discussion configuration to be parsed")
				}
				if len(workflowData.SafeOutputs.CreateDiscussions.AllowedLabels) != len(tt.expectDiscuss) {
					t.Errorf("Expected %d allowed labels for discussions, got %d",
						len(tt.expectDiscuss), len(workflowData.SafeOutputs.CreateDiscussions.AllowedLabels))
				}
				for i, expected := range tt.expectDiscuss {
					if i >= len(workflowData.SafeOutputs.CreateDiscussions.AllowedLabels) ||
						workflowData.SafeOutputs.CreateDiscussions.AllowedLabels[i] != expected {
						t.Errorf("Expected discussion allowed label[%d] to be '%s', got '%s'",
							i, expected, workflowData.SafeOutputs.CreateDiscussions.AllowedLabels[i])
					}
				}
			}

			// Check create-pull-request allowed-labels
			if tt.expectPR != nil {
				if workflowData.SafeOutputs.CreatePullRequests == nil {
					t.Fatal("Expected create-pull-request configuration to be parsed")
				}
				if len(workflowData.SafeOutputs.CreatePullRequests.AllowedLabels) != len(tt.expectPR) {
					t.Errorf("Expected %d allowed labels for PRs, got %d",
						len(tt.expectPR), len(workflowData.SafeOutputs.CreatePullRequests.AllowedLabels))
				}
				for i, expected := range tt.expectPR {
					if i >= len(workflowData.SafeOutputs.CreatePullRequests.AllowedLabels) ||
						workflowData.SafeOutputs.CreatePullRequests.AllowedLabels[i] != expected {
						t.Errorf("Expected PR allowed label[%d] to be '%s', got '%s'",
							i, expected, workflowData.SafeOutputs.CreatePullRequests.AllowedLabels[i])
					}
				}
			}
		})
	}
}

func TestAllowedLabelsJobGeneration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "allowed-labels-job-test")

	// Test case with allowed-labels configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: write
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    allowed-labels: [bug, enhancement]
  create-pull-request:
    allowed-labels: [automated]
---

# Test Allowed Labels Job Generation

This workflow tests that allowed-labels are passed to safe output jobs.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}
	lockfileContent := string(lockBytes)

	// Verify that handler config contains allowed-labels for create-issue
	if !strings.Contains(lockfileContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable in compiled workflow")
	}

	// Verify the allowed labels are in the handler config for issues
	if !strings.Contains(lockfileContent, `\"create_issue\"`) {
		t.Error("Expected create_issue in handler config")
	}
	if !strings.Contains(lockfileContent, `\"allowed_labels\":[\"bug\",\"enhancement\"]`) {
		t.Error("Expected allowed labels for create_issue in handler config")
	}

	// Verify the allowed labels are also in handler config for PRs (now handled by handler manager)
	if !strings.Contains(lockfileContent, `\"create_pull_request\"`) {
		t.Error("Expected create_pull_request in handler config")
	}
}

func TestAllowedLabelsInSafeOutputsConfig(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "allowed-labels-config-test")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  create-issue:
    allowed-labels: [triage, bug]
---

# Test Allowed Labels in Safe Outputs Config

This workflow tests that allowed-labels are included in safe outputs config JSON.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow: %v", err)
	}

	// Generate safe outputs config
	configJSON := generateSafeOutputsConfig(workflowData)

	// Verify that allowed_labels is in the config
	if !strings.Contains(configJSON, `"allowed_labels"`) {
		t.Error("Expected allowed_labels to be in safe outputs config JSON")
	}

	// Verify the allowed labels array is present
	if !strings.Contains(configJSON, `"triage"`) || !strings.Contains(configJSON, `"bug"`) {
		t.Error("Expected allowed labels 'triage' and 'bug' to be in safe outputs config JSON")
	}
}
