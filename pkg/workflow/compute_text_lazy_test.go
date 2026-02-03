//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestComputeTextLazyInsertion(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "compute-text-lazy-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a .git directory to simulate a git repository
	gitDir := filepath.Join(tempDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Test case 1: Workflow that uses task.outputs.text
	workflowWithText := `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    toolsets: [issues]
---

# Test Workflow With Text Output

This workflow uses the text output: "${{ needs.activation.outputs.text }}"

Please analyze this issue and provide a helpful response.`

	workflowWithTextPath := filepath.Join(tempDir, "with-text.md")
	if err := os.WriteFile(workflowWithTextPath, []byte(workflowWithText), 0644); err != nil {
		t.Fatalf("Failed to write workflow with text: %v", err)
	}

	// Test case 2: Workflow that does NOT use task.outputs.text
	workflowWithoutText := `---
on:
  schedule:
    - cron: "0 9 * * 1"
permissions:
  issues: write
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    toolsets: [issues]
---

# Test Workflow Without Text Output

This workflow does NOT use the text output.

Create a report based on repository analysis.`

	workflowWithoutTextPath := filepath.Join(tempDir, "without-text.md")
	if err := os.WriteFile(workflowWithoutTextPath, []byte(workflowWithoutText), 0644); err != nil {
		t.Fatalf("Failed to write workflow without text: %v", err)
	}

	compiler := NewCompiler()

	// Test workflow WITH text usage
	t.Run("workflow_with_text_usage", func(t *testing.T) {
		err := compiler.CompileWorkflow(workflowWithTextPath)
		if err != nil {
			t.Fatalf("Failed to compile workflow with text: %v", err)
		}

		// Check that compute-text action was NOT created (JavaScript is now inlined)
		actionPath := filepath.Join(tempDir, ".github", "actions", "compute-text", "action.yml")
		if _, err := os.Stat(actionPath); !os.IsNotExist(err) {
			t.Error("Expected compute-text action NOT to be created (JavaScript should be inlined)")
		}

		// Check that the compiled YAML contains inlined compute-text step
		lockPath := stringutil.MarkdownToLockFile(workflowWithTextPath)
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockStr := string(lockContent)
		if !strings.Contains(lockStr, "compute-text") {
			t.Error("Expected compiled workflow to contain compute-text step")
		}
		if !strings.Contains(lockStr, "text: ${{ steps.compute-text.outputs.text }}") {
			t.Error("Expected compiled workflow to contain text output")
		}
		// Check that JavaScript is inlined instead of using shared action
		if !strings.Contains(lockStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
			t.Error("Expected compute-text step to use inlined JavaScript")
		}
		if strings.Contains(lockStr, "uses: ./.github/actions/compute-text") {
			t.Error("Expected compute-text step NOT to use shared action")
		}
	})

	// Remove compute-text action for next test
	os.RemoveAll(filepath.Join(tempDir, ".github"))

	// Test workflow WITHOUT text usage
	t.Run("workflow_without_text_usage", func(t *testing.T) {
		err := compiler.CompileWorkflow(workflowWithoutTextPath)
		if err != nil {
			t.Fatalf("Failed to compile workflow without text: %v", err)
		}

		// Check that compute-text action was NOT created
		actionPath := filepath.Join(tempDir, ".github", "actions", "compute-text", "action.yml")
		if _, err := os.Stat(actionPath); !os.IsNotExist(err) {
			t.Error("Expected compute-text action NOT to be created for workflow that doesn't use text output")
		}

		// Check that the compiled YAML does NOT contain compute-text step
		lockPath := stringutil.MarkdownToLockFile(workflowWithoutTextPath)
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockStr := string(lockContent)
		if strings.Contains(lockStr, "compute-text") {
			t.Error("Expected compiled workflow NOT to contain compute-text step")
		}
		if strings.Contains(lockStr, "text: ${{ steps.compute-text.outputs.text }}") {
			t.Error("Expected compiled workflow NOT to contain text output")
		}
	})
}

func TestDetectTextOutputUsage(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name          string
		content       string
		expectedUsage bool
	}{
		{
			name:          "with_text_usage",
			content:       "Analyze this: \"${{ needs.activation.outputs.text }}\"",
			expectedUsage: true,
		},
		{
			name:          "without_text_usage",
			content:       "Create a report based on repository analysis.",
			expectedUsage: false,
		},
		{
			name:          "with_other_github_expressions",
			content:       "Repository: ${{ github.repository }} but no text output",
			expectedUsage: false,
		},
		{
			name:          "with_partial_match",
			content:       "Something about task.outputs but not the full expression",
			expectedUsage: false,
		},
		{
			name:          "with_multiple_usages",
			content:       "First: \"${{ needs.activation.outputs.text }}\" and second: \"${{ needs.activation.outputs.text }}\"",
			expectedUsage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.detectTextOutputUsage(tt.content)
			if result != tt.expectedUsage {
				t.Errorf("detectTextOutputUsage() = %v, expected %v", result, tt.expectedUsage)
			}
		})
	}
}
