//go:build !integration

package main

import (
	"testing"
)

func TestFormatListWithOr(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		expected string
	}{
		{
			name:     "empty list",
			items:    []string{},
			expected: "",
		},
		{
			name:     "single item",
			items:    []string{"apple"},
			expected: "apple",
		},
		{
			name:     "two items",
			items:    []string{"apple", "banana"},
			expected: "apple or banana",
		},
		{
			name:     "three items",
			items:    []string{"apple", "banana", "cherry"},
			expected: "apple, banana, or cherry",
		},
		{
			name:     "four items",
			items:    []string{"apple", "banana", "cherry", "date"},
			expected: "apple, banana, cherry, or date",
		},
		{
			name:     "engine names with quotes",
			items:    []string{"'claude'", "'codex'", "'copilot'", "'custom'"},
			expected: "'claude', 'codex', 'copilot', or 'custom'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatListWithOr(tt.items)
			if result != tt.expected {
				t.Errorf("formatListWithOr(%v) = %q, want %q", tt.items, result, tt.expected)
			}
		})
	}
}
