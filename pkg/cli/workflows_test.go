//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsWorkflowFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "regular workflow file",
			filename: "my-workflow.md",
			expected: true,
		},
		{
			name:     "README.md should be excluded",
			filename: "README.md",
			expected: false,
		},
		{
			name:     "readme.md lowercase should be excluded",
			filename: "readme.md",
			expected: false,
		},
		{
			name:     "ReadMe.md mixed case should be excluded",
			filename: "ReadMe.md",
			expected: false,
		},
		{
			name:     "READM.md with different name is included",
			filename: "READM.md",
			expected: true,
		},
		{
			name:     "README-workflow.md with prefix is included",
			filename: "README-workflow.md",
			expected: true,
		},
		{
			name:     "workflow-README.md with suffix is included",
			filename: "workflow-README.md",
			expected: true,
		},
		{
			name:     "path with README.md at end should be excluded",
			filename: ".github/workflows/README.md",
			expected: false,
		},
		{
			name:     "path with regular workflow should be included",
			filename: ".github/workflows/my-workflow.md",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWorkflowFile(tt.filename)
			assert.Equal(t, tt.expected, result, "isWorkflowFile(%q) should return %v", tt.filename, tt.expected)
		})
	}
}

func TestFilterWorkflowFiles(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty list",
			input:    []string{},
			expected: []string{},
		},
		{
			name: "no README.md files",
			input: []string{
				"workflow1.md",
				"workflow2.md",
				"my-test.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
				"my-test.md",
			},
		},
		{
			name: "filter out README.md",
			input: []string{
				"workflow1.md",
				"README.md",
				"workflow2.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
			},
		},
		{
			name: "filter out readme.md (lowercase)",
			input: []string{
				"workflow1.md",
				"readme.md",
				"workflow2.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
			},
		},
		{
			name: "filter out ReadMe.md (mixed case)",
			input: []string{
				"workflow1.md",
				"ReadMe.md",
				"workflow2.md",
			},
			expected: []string{
				"workflow1.md",
				"workflow2.md",
			},
		},
		{
			name: "keep README-prefixed files",
			input: []string{
				"README-workflow.md",
				"README.md",
				"workflow-README.md",
			},
			expected: []string{
				"README-workflow.md",
				"workflow-README.md",
			},
		},
		{
			name: "filter with full paths",
			input: []string{
				".github/workflows/workflow1.md",
				".github/workflows/README.md",
				".github/workflows/workflow2.md",
			},
			expected: []string{
				".github/workflows/workflow1.md",
				".github/workflows/workflow2.md",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterWorkflowFiles(tt.input)
			// Use ElementsMatch to handle nil vs empty slice differences
			if len(tt.expected) == 0 && len(result) == 0 {
				return // Both are empty, test passes
			}
			assert.Equal(t, tt.expected, result, "filterWorkflowFiles should filter correctly")
		})
	}
}

// TestGetMarkdownWorkflowFilesExcludesREADME tests that getMarkdownWorkflowFiles filters out README.md
func TestGetMarkdownWorkflowFilesExcludesREADME(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create several workflow files
	testFiles := map[string]string{
		"workflow1.md":   "---\non: push\n---\n# Workflow 1",
		"workflow2.md":   "---\non: pull_request\n---\n# Workflow 2",
		"README.md":      "# This is a README",
		"readme.md":      "# This is a readme",
		"ReadMe.md":      "# This is a ReadMe",
		"my-workflow.md": "---\non: workflow_dispatch\n---\n# My Workflow",
		"README-test.md": "---\non: push\n---\n# README Test",
		"test-README.md": "---\non: push\n---\n# Test README",
	}

	for filename, content := range testFiles {
		path := filepath.Join(workflowsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", filename, err)
		}
	}

	// Change to the temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Get markdown workflow files
	files, err := getMarkdownWorkflowFiles("")
	if err != nil {
		t.Fatalf("getMarkdownWorkflowFiles failed: %v", err)
	}

	// Extract basenames for easier checking
	var basenames []string
	for _, file := range files {
		basenames = append(basenames, filepath.Base(file))
	}

	// Should include regular workflow files and files with README in the middle
	expectedFiles := []string{"workflow1.md", "workflow2.md", "my-workflow.md", "README-test.md", "test-README.md"}
	for _, expected := range expectedFiles {
		assert.Contains(t, basenames, expected, "Should include %s", expected)
	}

	// Should NOT include any README.md variants (exact name, case-insensitive)
	excludedFiles := []string{"README.md", "readme.md", "ReadMe.md"}
	for _, excluded := range excludedFiles {
		assert.NotContains(t, basenames, excluded, "Should NOT include %s", excluded)
	}

	// Verify total count
	assert.Len(t, files, 5, "Should have exactly 5 workflow files (excluding README variants)")
}
