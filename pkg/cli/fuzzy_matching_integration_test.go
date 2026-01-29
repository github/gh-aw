//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandsWithFuzzyMatching tests that all commands that take workflow names
// properly suggest similar names when a workflow is not found
func TestCommandsWithFuzzyMatching(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "test-fuzzy-commands")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory with test workflows
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	testWorkflows := []string{
		"audit-workflows.md",
		"brave.md",
		"archie.md",
	}

	for _, workflow := range testWorkflows {
		path := filepath.Join(workflowsDir, workflow)
		content := `---
on:
  workflow_dispatch:
---
# Test workflow
Test content
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", workflow, err)
		}
	}

	tests := []struct {
		name                string
		testFunc            func(string) error
		input               string
		expectedSuggestions []string
	}{
		{
			name: "ResolveWorkflowPath with typo",
			testFunc: func(input string) error {
				_, err := ResolveWorkflowPath(input)
				return err
			},
			input:               "audti-workflows",
			expectedSuggestions: []string{"audit-workflows"},
		},
		{
			name: "ResolveWorkflowPath with another typo",
			testFunc: func(input string) error {
				_, err := ResolveWorkflowPath(input)
				return err
			},
			input:               "archei",
			expectedSuggestions: []string{"archie"},
		},
		{
			name: "resolveWorkflowFile with typo",
			testFunc: func(input string) error {
				_, err := resolveWorkflowFile(input, false)
				return err
			},
			input:               "brav",
			expectedSuggestions: []string{"brave"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFunc(tt.input)
			if err == nil {
				t.Errorf("Expected error for non-existent workflow, got nil")
				return
			}

			errorMsg := err.Error()

			// Check that error message contains suggestions
			if !strings.Contains(errorMsg, "Did you mean:") && len(tt.expectedSuggestions) > 0 {
				t.Errorf("Error message should contain 'Did you mean:' but got: %s", errorMsg)
			}

			// Check that expected suggestions are in the error message
			for _, suggestion := range tt.expectedSuggestions {
				if !strings.Contains(errorMsg, suggestion) {
					t.Errorf("Expected suggestion %q not found in error message: %s", suggestion, errorMsg)
				}
			}

			// Verify it still contains helpful suggestions
			if !strings.Contains(errorMsg, "gh aw status") {
				t.Errorf("Error message should mention 'gh aw status' but got: %s", errorMsg)
			}
		})
	}
}

// TestEnableCommandFuzzyMatching tests the enable command's fuzzy matching
func TestEnableCommandFuzzyMatching(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "test-enable-fuzzy")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory with test workflows
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	testWorkflows := []string{
		"audit-workflows.md",
		"brave.md",
	}

	for _, workflow := range testWorkflows {
		path := filepath.Join(workflowsDir, workflow)
		content := `---
on:
  workflow_dispatch:
---
# Test workflow
Test content
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", workflow, err)
		}
	}

	// Test enable command with typo
	err = EnableWorkflowsByNames([]string{"audti-workflows"}, "")
	if err == nil {
		t.Fatal("Expected error for non-existent workflow")
	}

	errorMsg := err.Error()

	// Check that error contains fuzzy match suggestion
	if !strings.Contains(errorMsg, "Did you mean:") {
		t.Errorf("Error message should contain 'Did you mean:' but got: %s", errorMsg)
	}

	if !strings.Contains(errorMsg, "audit-workflows") {
		t.Errorf("Error message should suggest 'audit-workflows' but got: %s", errorMsg)
	}
}

// TestFuzzyMatchingDoesNotShowForExactMatches verifies that exact matches
// are not shown in suggestions (as per parser.FindClosestMatches behavior)
func TestFuzzyMatchingDoesNotShowForExactMatches(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "test-exact-match")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory with test workflow
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a workflow file that actually exists
	path := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:
---
# Test workflow
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create workflow file: %v", err)
	}

	// Attempt to resolve the workflow by its exact name
	resolved, err := ResolveWorkflowPath("test-workflow")
	if err != nil {
		t.Errorf("Expected to resolve exact match, got error: %v", err)
	}

	if !strings.Contains(resolved, "test-workflow.md") {
		t.Errorf("Expected resolved path to contain 'test-workflow.md', got: %s", resolved)
	}
}

// TestFuzzyMatchingWithNoCloseMatches verifies behavior when there are
// no close matches (distance > 3)
func TestFuzzyMatchingWithNoCloseMatches(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "test-no-matches")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory with test workflows
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create workflows with very different names
	testWorkflows := []string{
		"xyz.md",
		"abc.md",
	}

	for _, workflow := range testWorkflows {
		path := filepath.Join(workflowsDir, workflow)
		content := `---
on:
  workflow_dispatch:
---
# Test workflow
`
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", workflow, err)
		}
	}

	// Try to resolve a workflow with a completely different name
	_, err = ResolveWorkflowPath("completely-different-name")
	if err == nil {
		t.Fatal("Expected error for non-existent workflow")
	}

	errorMsg := err.Error()

	// Should NOT contain "Did you mean:" since there are no close matches
	if strings.Contains(errorMsg, "Did you mean:") {
		t.Errorf("Error message should NOT contain suggestions for distant names, but got: %s", errorMsg)
	}

	// Should still contain general help
	if !strings.Contains(errorMsg, "gh aw status") {
		t.Errorf("Error message should contain general help even without suggestions: %s", errorMsg)
	}
}
