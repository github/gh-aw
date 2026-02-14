//go:build integration

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontmatterLocationIntegration(t *testing.T) {
	// Create a temporary file with frontmatter that has additional properties
	tempFile := "/tmp/gh-aw/gh-aw/test_frontmatter_location.md"

	// Ensure the directory exists
	err := os.MkdirAll(filepath.Dir(tempFile), 0o755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	content := `---
name: Test Workflow
on: push
permissions:
  contents: read
invalid_property: value
another_bad_prop: bad_value
engine: claude
---

This is a test workflow with invalid additional properties in frontmatter.
`

	err = os.WriteFile(tempFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile)

	// Create a schema that doesn't allow additional properties
	schemaJSON := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"on": {"type": ["string", "object"]},
			"permissions": {"type": "object"},
			"engine": {"type": "string"}
		},
		"additionalProperties": false
	}`

	// Parse frontmatter
	frontmatterResult, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		t.Fatalf("Failed to extract frontmatter: %v", err)
	}

	// Validate with location information
	err = validateWithSchemaAndLocation(frontmatterResult.Frontmatter, schemaJSON, "test workflow", tempFile)

	if err == nil {
		t.Fatal("Expected validation error for additional properties, got nil")
	}

	errorMessage := err.Error()
	t.Logf("Error message: %s", errorMessage)

	// Verify the error points to the correct location
	expectedPatterns := []string{
		tempFile + ":",     // File path
		"6:1:",             // Line 6 column 1 (where invalid_property is)
		"invalid_property", // The property name in the message
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(errorMessage, pattern) {
			t.Errorf("Error message should contain '%s' but got: %s", pattern, errorMessage)
		}
	}
}

func TestFrontmatterOffsetCalculation(t *testing.T) {
	// Test frontmatter at the beginning of the file
	tempFile := "/tmp/gh-aw/gh-aw/test_frontmatter_offset.md"

	// Ensure the directory exists
	err := os.MkdirAll(filepath.Dir(tempFile), 0o755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	content := `---
name: Test Workflow
invalid_prop: bad
---

# This is content after frontmatter
<!-- HTML comment -->

Content here.
`

	err = os.WriteFile(tempFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile)

	schemaJSON := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"additionalProperties": false
	}`

	frontmatterResult, err := ExtractFrontmatterFromContent(content)
	if err != nil {
		t.Fatalf("Failed to extract frontmatter: %v", err)
	}

	err = validateWithSchemaAndLocation(frontmatterResult.Frontmatter, schemaJSON, "test workflow", tempFile)

	if err == nil {
		t.Fatal("Expected validation error for additional properties, got nil")
	}

	errorMessage := err.Error()
	t.Logf("Error message with offset: %s", errorMessage)

	// The invalid_prop should be on line 3 (1-based: ---, name, invalid_prop)
	expectedPatterns := []string{
		tempFile + ":",
		"3:", // Line 3 where invalid_prop appears
		"invalid_prop",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(errorMessage, pattern) {
			t.Errorf("Error message should contain '%s' but got: %s", pattern, errorMessage)
		}
	}
}

func TestImprovementComparison(t *testing.T) {
	yamlContent := `name: Test
engine: claude
invalid_prop: bad_value
another_invalid: also_bad`

	// Simulate the error message we get from jsonschema
	errorMessage := "at '': additional properties 'invalid_prop', 'another_invalid' not allowed"

	// Test old behavior
	oldLocation := LocateJSONPathInYAML(yamlContent, "")

	// Test new behavior
	newLocation := LocateJSONPathInYAMLWithAdditionalProperties(yamlContent, "", errorMessage)

	// The old behavior should point to line 1, column 1
	if oldLocation.Line != 1 || oldLocation.Column != 1 {
		t.Errorf("Old behavior expected Line=1, Column=1, got Line=%d, Column=%d", oldLocation.Line, oldLocation.Column)
	}

	// The new behavior should point to line 3, column 1 (where invalid_prop is)
	if newLocation.Line != 3 || newLocation.Column != 1 {
		t.Errorf("New behavior expected Line=3, Column=1, got Line=%d, Column=%d", newLocation.Line, newLocation.Column)
	}

	t.Logf("Improvement demonstrated: Old=(Line:%d, Column:%d) -> New=(Line:%d, Column:%d)",
		oldLocation.Line, oldLocation.Column, newLocation.Line, newLocation.Column)
}
