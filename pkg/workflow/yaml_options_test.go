//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	yamlv3 "go.yaml.in/yaml/v3"
)

func TestDefaultMarshalOptionsMultilineRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"leading spaces", "   continuation\nnext: list item\n"},
		{"leading tab", "\tcontinuation\nnext: list item\n"},
		{"single-line leading spaces", "   continuation"},
		{"single-line leading tab", "\tcontinuation"},
		{"single-line trailing space", "continuation "},
		{"single-line trailing tab", "continuation\t"},
		{"uniform indentation", "   first\n   second\n"},
		{"leading newline", "\n   continuation\nnext: list item\n"},
		{"leading blank lines", "\n\nfirst\nsecond"},
		{"whitespace-only first line", "   \nfirst\nsecond\n"},
		{"only newlines", "\n\n"},
		{"single newline", "\n"},
		{"three newlines", "\n\n\n"},
		{"trailing blank lines", "   first\nsecond\n\n"},
		{"CRLF", "first\r\nsecond\r\n"},
		{"quotes and escapes", "   \"quoted\" \\path\nnext: 'value'\n"},
		{"ordinary multiline", "first\n   nested\nlast\n"},
		{"trailing spaces", "first\nsecond  "},
		{"trailing tab", "first\nsecond\t"},
		{"only trailing blank line", "value\n\n"},
		{"unindented trailing blank lines", "first\nsecond\n\n"},
		{"multiple trailing blank lines", "first\nsecond\n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]any{"env": map[string]string{"CONTENT": tt.value}}
			encoded, err := marshalYAMLWithOptions(input, DefaultMarshalOptions...)
			if err != nil {
				t.Fatal(err)
			}
			for name, unmarshal := range map[string]func([]byte, any) error{
				"goccy":  yaml.Unmarshal,
				"yamlv3": yamlv3.Unmarshal,
			} {
				t.Run(name, func(t *testing.T) {
					var decoded map[string]map[string]string
					if err := unmarshal(encoded, &decoded); err != nil {
						t.Fatalf("invalid YAML: %v\n%s", err, encoded)
					}
					if got := decoded["env"]["CONTENT"]; got != tt.value {
						t.Errorf("round trip changed content: got %q, want %q", got, tt.value)
					}
				})
			}
		})
	}
}

func TestMarshalYAMLWithOptionsPreservesOtherValues(t *testing.T) {
	input := yaml.MapSlice{
		{Key: "unsafe", Value: "   first\nsecond\n"},
		{Key: "ordinary", Value: "first\n   second\n"},
		{Key: "trailing", Value: "first\nsecond  "},
		{Key: "blank", Value: "first\nsecond\n\n"},
	}
	encoded, err := marshalYAMLWithOptions(input, DefaultMarshalOptions...)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := yamlv3.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, item := range input {
		if got := decoded[item.Key.(string)]; got != item.Value {
			t.Errorf("%s changed: got %q, want %q", item.Key, got, item.Value)
		}
	}
}

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
			result, err := marshalYAMLWithOptions(tt.input, DefaultMarshalOptions...)
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
		"name":        "workflow",
		"on":          "push",
		"description": "ordinary multiline\nwith another line",
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
	result1, err := marshalYAMLWithOptions(testData, DefaultMarshalOptions...)
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

	result, err := marshalYAMLWithOptions(input, DefaultMarshalOptions...)
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
