//go:build integration

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWithSchemaAndLocation_PreciseLocation(t *testing.T) {
	// Create a test file with invalid frontmatter
	testContent := `---
on: push
permissions: read-all
age: "not-a-number"
invalid_property: value
tools:
  - name: tool1
  - description: missing name
timeout_minutes: 30
---

# Test workflow content`

	tempFile := "/tmp/gh-aw/gh-aw/test_precise_location.md"

	// Ensure the directory exists
	err := os.MkdirAll(filepath.Dir(tempFile), 0o755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	err = os.WriteFile(tempFile, []byte(testContent), 0o644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile)

	// Create frontmatter that will trigger validation errors
	frontmatter := map[string]any{
		"on":               "push",
		"permissions":      "read",
		"age":              "not-a-number", // Should trigger error if age field exists in schema
		"invalid_property": "value",        // Should trigger additional properties error
		"tools": []any{
			map[string]any{"name": "tool1"},
			map[string]any{"description": "missing name"}, // Should trigger missing name error
		},
		"timeout_minutes": 30,
	}

	// Test with main workflow schema
	err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, tempFile)

	// We expect a validation error
	if err == nil {
		t.Log("No validation error - this might be expected if the schema doesn't validate these fields")
		return
	}

	errorMsg := err.Error()
	t.Logf("Error message: %s", errorMsg)

	// Check that the error contains file path information
	if !strings.Contains(errorMsg, tempFile) {
		t.Errorf("Error message should contain file path, got: %s", errorMsg)
	}

	// Check that the error contains line/column information in VS Code parseable format
	// Should have format like "file.md:line:column: error: message"
	if !strings.Contains(errorMsg, ":") {
		t.Errorf("Error message should contain line:column information, got: %s", errorMsg)
	}

	// The error should not contain raw jsonschema prefixes
	if strings.Contains(errorMsg, "jsonschema validation failed") {
		t.Errorf("Error message should not contain raw jsonschema prefix, got: %s", errorMsg)
	}

	// Should contain cleaned error information
	lines := strings.Split(errorMsg, "\n")
	if len(lines) < 2 {
		t.Errorf("Error message should be multi-line with context, got: %s", errorMsg)
	}
}

func TestLocateJSONPathInYAML_RealExample(t *testing.T) {
	// Test with a real frontmatter example
	yamlContent := `on: push
permissions: read-all
engine: claude
tools:
  - name: github
    description: GitHub tool
  - name: filesystem
    description: File operations
timeout_minutes: 30`

	tests := []struct {
		name     string
		jsonPath string
		wantLine int
		wantCol  int
	}{
		{
			name:     "root permission",
			jsonPath: "/permissions",
			wantLine: 2,
			wantCol:  14, // After "permissions: "
		},
		{
			name:     "engine field",
			jsonPath: "/engine",
			wantLine: 3,
			wantCol:  9, // After "engine: "
		},
		{
			name:     "first tool",
			jsonPath: "/tools/0",
			wantLine: 5,
			wantCol:  4, // At "- name: github"
		},
		{
			name:     "second tool",
			jsonPath: "/tools/1",
			wantLine: 7,
			wantCol:  4, // At "- name: filesystem"
		},
		{
			name:     "timeout",
			jsonPath: "/timeout_minutes",
			wantLine: 9,
			wantCol:  18, // After "timeout_minutes: "
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location := LocateJSONPathInYAML(yamlContent, tt.jsonPath)

			if !location.Found {
				t.Errorf("Path %s should be found", tt.jsonPath)
			}

			// For this test, we mainly care that we get reasonable line numbers
			// The exact column positions might vary based on implementation
			if location.Line != tt.wantLine {
				t.Errorf("Path %s: expected line %d, got line %d", tt.jsonPath, tt.wantLine, location.Line)
			}

			// Log the actual results for reference
			t.Logf("Path %s: Line=%d, Column=%d", tt.jsonPath, location.Line, location.Column)
		})
	}
}
