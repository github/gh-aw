//go:build !integration

package workflow

import (
	"testing"
)

func TestExtractYAMLValue(t *testing.T) {
	compiler := &Compiler{}

	tests := []struct {
		name        string
		frontmatter map[string]any
		key         string
		expected    string
	}{
		{
			name:        "string value",
			frontmatter: map[string]any{"name": "test-workflow"},
			key:         "name",
			expected:    "test-workflow",
		},
		{
			name:        "int value",
			frontmatter: map[string]any{"timeout": 42},
			key:         "timeout",
			expected:    "42",
		},
		{
			name:        "int64 value",
			frontmatter: map[string]any{"count": int64(12345)},
			key:         "count",
			expected:    "12345",
		},
		{
			name:        "uint64 value",
			frontmatter: map[string]any{"id": uint64(99999)},
			key:         "id",
			expected:    "99999",
		},
		{
			name:        "float64 value",
			frontmatter: map[string]any{"version": 3.14},
			key:         "version",
			expected:    "3",
		},
		{
			name:        "float64 whole number",
			frontmatter: map[string]any{"port": 8080.0},
			key:         "port",
			expected:    "8080",
		},
		{
			name:        "key not found",
			frontmatter: map[string]any{"name": "test"},
			key:         "missing",
			expected:    "",
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
			key:         "name",
			expected:    "",
		},
		{
			name:        "unsupported type (array)",
			frontmatter: map[string]any{"items": []string{"a", "b"}},
			key:         "items",
			expected:    "",
		},
		{
			name:        "unsupported type (map)",
			frontmatter: map[string]any{"config": map[string]string{"key": "value"}},
			key:         "config",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.extractYAMLValue(tt.frontmatter, tt.key)
			if result != tt.expected {
				t.Errorf("extractYAMLValue() = %q, want %q", result, tt.expected)
			}
		})
	}
}
