//go:build !integration

package parser

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestLocateJSONPathInYAML(t *testing.T) {
	yamlContent := `name: John Doe
age: 30
tools:
  - name: tool1
    version: "1.0"
  - name: tool2
    description: "second tool"
permissions:
  read: true
  write: false`

	tests := []struct {
		name       string
		jsonPath   string
		expectLine int
		expectCol  int
		shouldFind bool
	}{
		{
			name:       "root level",
			jsonPath:   "",
			expectLine: 1,
			expectCol:  1,
			shouldFind: true,
		},
		{
			name:       "simple key",
			jsonPath:   "/name",
			expectLine: 1,
			expectCol:  6, // After "name:"
			shouldFind: true,
		},
		{
			name:       "simple key - age",
			jsonPath:   "/age",
			expectLine: 2,
			expectCol:  5, // After "age:"
			shouldFind: true,
		},
		{
			name:       "array element",
			jsonPath:   "/tools/0",
			expectLine: 4,
			expectCol:  4, // Start of first array element
			shouldFind: true,
		},
		{
			name:       "nested in array element",
			jsonPath:   "/tools/1",
			expectLine: 6,
			expectCol:  4, // Start of second array element
			shouldFind: true,
		},
		{
			name:       "nested object key",
			jsonPath:   "/permissions/read",
			expectLine: 9,
			expectCol:  8, // After "read: "
			shouldFind: true,
		},
		{
			name:       "invalid path",
			jsonPath:   "/nonexistent",
			expectLine: 1,
			expectCol:  1,
			shouldFind: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location := LocateJSONPathInYAML(yamlContent, tt.jsonPath)

			if location.Found != tt.shouldFind {
				t.Errorf("Expected Found=%v, got Found=%v", tt.shouldFind, location.Found)
			}

			if location.Line != tt.expectLine {
				t.Errorf("Expected Line=%d, got Line=%d", tt.expectLine, location.Line)
			}

			if location.Column != tt.expectCol {
				t.Errorf("Expected Column=%d, got Column=%d", tt.expectCol, location.Column)
			}
		})
	}
}

func TestExtractJSONPathFromValidationError(t *testing.T) {
	// Create a schema with validation errors
	schemaJSON := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "number"},
			"tools": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					},
					"required": ["name"]
				}
			}
		},
		"additionalProperties": false
	}`

	// Create invalid data
	invalidData := map[string]any{
		"name":        "John",
		"age":         "not-a-number", // Should be number
		"invalid_key": "value",        // Additional property not allowed
		"tools": []any{
			map[string]any{
				"name": "tool1",
			},
			map[string]any{
				// Missing required "name" field
				"description": "tool without name",
			},
		},
	}

	// Compile schema and validate
	compiler := jsonschema.NewCompiler()
	var schemaDoc any
	json.Unmarshal([]byte(schemaJSON), &schemaDoc)

	schemaURL := "http://example.com/schema.json"
	compiler.AddResource(schemaURL, schemaDoc)
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatalf("Schema compilation error: %v", err)
	}

	err = schema.Validate(invalidData)
	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	// Extract JSON path information
	paths := ExtractJSONPathFromValidationError(err)

	if len(paths) != 3 {
		t.Errorf("Expected 3 validation errors, got %d", len(paths))
	}

	// Check that we have the expected paths
	expectedPaths := map[string]bool{
		"/tools/1": false,
		"/age":     false,
		"":         false, // Root level for additional properties
	}

	for _, pathInfo := range paths {
		if _, exists := expectedPaths[pathInfo.Path]; exists {
			expectedPaths[pathInfo.Path] = true
			t.Logf("Found expected path: %s with message: %s", pathInfo.Path, pathInfo.Message)
		} else {
			t.Errorf("Unexpected path: %s", pathInfo.Path)
		}
	}

	// Verify all expected paths were found
	for path, found := range expectedPaths {
		if !found {
			t.Errorf("Expected path not found: %s", path)
		}
	}
}

func TestParseJSONPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []PathSegment
	}{
		{
			name:     "empty path",
			path:     "",
			expected: []PathSegment{},
		},
		{
			name:     "root path",
			path:     "/",
			expected: []PathSegment{},
		},
		{
			name: "simple key",
			path: "/name",
			expected: []PathSegment{
				{Type: "key", Value: "name"},
			},
		},
		{
			name: "array index",
			path: "/tools/0",
			expected: []PathSegment{
				{Type: "key", Value: "tools"},
				{Type: "index", Value: "0", Index: 0},
			},
		},
		{
			name: "complex path",
			path: "/tools/1/permissions/read",
			expected: []PathSegment{
				{Type: "key", Value: "tools"},
				{Type: "index", Value: "1", Index: 1},
				{Type: "key", Value: "permissions"},
				{Type: "key", Value: "read"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseJSONPath(tt.path)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d segments, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i].Type != expected.Type {
					t.Errorf("Segment %d: expected Type=%s, got Type=%s", i, expected.Type, result[i].Type)
				}
				if result[i].Value != expected.Value {
					t.Errorf("Segment %d: expected Value=%s, got Value=%s", i, expected.Value, result[i].Value)
				}
				if expected.Type == "index" && result[i].Index != expected.Index {
					t.Errorf("Segment %d: expected Index=%d, got Index=%d", i, expected.Index, result[i].Index)
				}
			}
		})
	}
}
