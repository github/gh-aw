//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// TestDefaultMarshalOptions verifies that the centralized marshal options
// produce the expected YAML formatting.
func TestDefaultMarshalOptions(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected string
	}{
		{
			name: "simple key-value with 2-space indentation",
			input: map[string]any{
				"name": "test",
				"nested": map[string]any{
					"key": "value",
				},
			},
			expected: `name: test
nested:
  key: value
`,
		},
		{
			name: "multiline string uses literal style",
			input: map[string]any{
				"description": "This is a\nmultiline\nstring",
			},
			expected: `description: |-
  This is a
  multiline
  string
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := yaml.MarshalWithOptions(tt.input, DefaultMarshalOptions...)
			if err != nil {
				t.Fatalf("MarshalWithOptions failed: %v", err)
			}

			resultStr := string(result)
			if resultStr != tt.expected {
				t.Errorf("Expected:\n%s\nGot:\n%s", tt.expected, resultStr)
			}
		})
	}
}

// TestDefaultMarshalOptionsConsistency verifies that using DefaultMarshalOptions
// produces the same output as the previous inline options.
func TestDefaultMarshalOptionsConsistency(t *testing.T) {
	testData := map[string]any{
		"name": "workflow",
		"on":   "push",
		"jobs": map[string]any{
			"test": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []map[string]any{
					{"name": "Checkout", "uses": "actions/checkout@v2"},
				},
			},
		},
	}

	// Marshal with DefaultMarshalOptions
	result1, err := yaml.MarshalWithOptions(testData, DefaultMarshalOptions...)
	if err != nil {
		t.Fatalf("MarshalWithOptions with DefaultMarshalOptions failed: %v", err)
	}

	// Marshal with inline options (the old way)
	result2, err := yaml.MarshalWithOptions(testData,
		yaml.Indent(2),
		yaml.UseLiteralStyleIfMultiline(true),
	)
	if err != nil {
		t.Fatalf("MarshalWithOptions with inline options failed: %v", err)
	}

	// Results should be identical
	if string(result1) != string(result2) {
		t.Errorf("DefaultMarshalOptions produces different output than inline options:\nDefaultMarshalOptions:\n%s\nInline options:\n%s",
			result1, result2)
	}
}

// TestDefaultMarshalOptionsIndentation ensures 2-space indentation is used.
func TestDefaultMarshalOptionsIndentation(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": "value",
			},
		},
	}

	result, err := yaml.MarshalWithOptions(input, DefaultMarshalOptions...)
	if err != nil {
		t.Fatalf("MarshalWithOptions failed: %v", err)
	}

	lines := strings.Split(string(result), "\n")

	// Check that level2 has 2 spaces of indentation
	foundLevel2 := false
	for _, line := range lines {
		if strings.Contains(line, "level2:") {
			foundLevel2 = true
			if !strings.HasPrefix(line, "  level2:") {
				t.Errorf("level2 should have 2 spaces of indentation, got: %q", line)
			}
		}
	}

	if !foundLevel2 {
		t.Error("Expected to find 'level2' in output")
	}

	// Check that level3 has 4 spaces of indentation
	foundLevel3 := false
	for _, line := range lines {
		if strings.Contains(line, "level3:") {
			foundLevel3 = true
			if !strings.HasPrefix(line, "    level3:") {
				t.Errorf("level3 should have 4 spaces of indentation, got: %q", line)
			}
		}
	}

	if !foundLevel3 {
		t.Error("Expected to find 'level3' in output")
	}
}
