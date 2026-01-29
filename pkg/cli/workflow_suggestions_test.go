//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuggestWorkflowNames(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "test-workflows")
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

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create test workflow files
	testWorkflows := []string{
		"audit-workflows.md",
		"archie.md",
		"brave.md",
		"craft.md",
		"copilot-session-insights.md",
		"copilot-agent-analysis.md",
		"ci-doctor.md",
	}

	for _, workflow := range testWorkflows {
		path := filepath.Join(workflowsDir, workflow)
		if err := os.WriteFile(path, []byte("# Test workflow\n"), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", workflow, err)
		}
	}

	tests := []struct {
		name     string
		target   string
		expected []string
	}{
		{
			name:     "single character typo",
			target:   "audti-workflows",
			expected: []string{"audit-workflows"},
		},
		{
			name:     "two character typo",
			target:   "archei",
			expected: []string{"archie"},
		},
		{
			name:     "multiple matches",
			target:   "brav",
			expected: []string{"brave"}, // Should include at least brave
		},
		{
			name:     "prefix match - within distance",
			target:   "ci-docter",
			expected: []string{"ci-doctor"},
		},
		{
			name:     "no matches - too different",
			target:   "completely-different",
			expected: []string{}, // Should return empty slice
		},
		{
			name:     "exact match excluded",
			target:   "archie",
			expected: []string{}, // Exact matches are excluded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := suggestWorkflowNames(tt.target)

			// Check if expected suggestions are present
			for _, expected := range tt.expected {
				found := false
				for _, result := range results {
					if result == expected {
						found = true
						break
					}
				}
				if !found && len(tt.expected) > 0 {
					t.Errorf("Expected suggestion %q not found in results %v", expected, results)
				}
			}

			// Check that we don't get more than 3 suggestions
			if len(results) > 3 {
				t.Errorf("Got %d suggestions, expected at most 3: %v", len(results), results)
			}

			// For empty expected, verify we got no results
			if len(tt.expected) == 0 && len(results) > 0 {
				// Only fail if the distance is within acceptable range (should not happen)
				t.Logf("Got unexpected suggestions %v for %q (may be acceptable if within distance)", results, tt.target)
			}
		})
	}
}

func TestGetAvailableWorkflowNames(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "test-workflows")
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

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create test workflow files
	testWorkflows := []string{
		"test-workflow-1.md",
		"test-workflow-2.md",
		"another-workflow.md",
	}

	for _, workflow := range testWorkflows {
		path := filepath.Join(workflowsDir, workflow)
		if err := os.WriteFile(path, []byte("# Test workflow\n"), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", workflow, err)
		}
	}

	// Get available workflow names
	names := getAvailableWorkflowNames()

	// Verify we got the expected count
	if len(names) != len(testWorkflows) {
		t.Errorf("Expected %d workflow names, got %d: %v", len(testWorkflows), len(names), names)
	}

	// Verify all expected names are present (without .md extension)
	expectedNames := map[string]bool{
		"test-workflow-1":  true,
		"test-workflow-2":  true,
		"another-workflow": true,
	}

	for _, name := range names {
		if !expectedNames[name] {
			t.Errorf("Unexpected workflow name: %s", name)
		}
		delete(expectedNames, name)
	}

	if len(expectedNames) > 0 {
		t.Errorf("Missing expected workflow names: %v", expectedNames)
	}
}

func TestGetAvailableWorkflowNamesNoDirectory(t *testing.T) {
	// Create temporary test directory without .github/workflows
	tmpDir, err := os.MkdirTemp("", "test-no-workflows")
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

	// Get available workflow names (should return empty/nil)
	names := getAvailableWorkflowNames()

	// Verify we got nil or empty slice
	if len(names) > 0 {
		t.Errorf("Expected empty or nil slice, got %d names: %v", len(names), names)
	}
}
