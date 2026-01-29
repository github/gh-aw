//go:build !integration

package parser

import (
	"reflect"
	"testing"
)

func TestMergeMCPTools(t *testing.T) {
	tests := []struct {
		name        string
		existing    map[string]any
		new         map[string]any
		expected    map[string]any
		expectError bool
		errorMsg    string
	}{
		{
			name:     "merge with empty existing map",
			existing: map[string]any{},
			new: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
				},
			},
			expected: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
				},
			},
			expectError: false,
		},
		{
			name: "merge with empty new map",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
				},
			},
			new: map[string]any{},
			expected: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
				},
			},
			expectError: false,
		},
		{
			name: "merge with different servers",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1"},
				},
			},
			new: map[string]any{
				"server2": map[string]any{
					"command": []string{"node", "server2.js"},
					"allowed": []any{"tool3"},
				},
			},
			expected: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1"},
				},
				"server2": map[string]any{
					"command": []string{"node", "server2.js"},
					"allowed": []any{"tool3"},
				},
			},
			expectError: false,
		},
		{
			name: "merge server config with conflicts",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
				},
			},
			new: map[string]any{
				"server1": map[string]any{
					"allowed": []any{"tool2", "tool3"}, // This will cause a conflict
				},
			},
			expected:    nil,
			expectError: true,
			errorMsg:    "conflict",
		},
		{
			name: "conflict detection for non-allowed properties",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"timeout": 30,
				},
			},
			new: map[string]any{
				"server1": map[string]any{
					"timeout": 60, // Different value - should cause conflict
				},
			},
			expected:    nil,
			expectError: true,
			errorMsg:    "conflict",
		},
		{
			name: "merge with new server only",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
					"env":     map[string]string{"VAR1": "value1"},
				},
				"server2": map[string]any{
					"command": []string{"node", "server2.js"},
				},
			},
			new: map[string]any{
				"server3": map[string]any{
					"command": []string{"go", "run", "server3.go"},
					"allowed": []any{"tool5"},
				},
			},
			expected: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2"},
					"env":     map[string]string{"VAR1": "value1"},
				},
				"server2": map[string]any{
					"command": []string{"node", "server2.js"},
				},
				"server3": map[string]any{
					"command": []string{"go", "run", "server3.go"},
					"allowed": []any{"tool5"},
				},
			},
			expectError: false,
		},
		{
			name: "merge with nil allowed arrays - conflicts",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": nil,
				},
			},
			new: map[string]any{
				"server1": map[string]any{
					"allowed": []any{"tool1"},
				},
			},
			expected:    nil,
			expectError: true,
			errorMsg:    "conflict",
		},
		{
			name: "merge with non-array allowed values",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": "not-an-array",
				},
			},
			new: map[string]any{
				"server1": map[string]any{
					"allowed": []any{"tool1"},
				},
			},
			expected:    nil,
			expectError: true,
			errorMsg:    "conflict",
		},
		{
			name: "merge with duplicate values causes conflict",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
					"allowed": []any{"tool1", "tool2", "tool1"}, // Duplicate tool1
				},
			},
			new: map[string]any{
				"server1": map[string]any{
					"allowed": []any{"tool2", "tool3", "tool2"}, // Duplicate tool2
				},
			},
			expected:    nil,
			expectError: true,
			errorMsg:    "conflict",
		},
		{
			name:        "both maps nil",
			existing:    nil,
			new:         nil,
			expected:    map[string]any{},
			expectError: false,
		},
		{
			name:     "existing nil, new has content",
			existing: nil,
			new: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
				},
			},
			expected: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
				},
			},
			expectError: false,
		},
		{
			name: "new nil, existing has content",
			existing: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
				},
			},
			new: nil,
			expected: map[string]any{
				"server1": map[string]any{
					"command": []string{"python", "-m", "server1"},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeMCPTools(tt.existing, tt.new)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', but got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}

				if !mapsEqual(result, tt.expected) {
					t.Errorf("Expected result %+v, got %+v", tt.expected, result)
				}
			}
		})
	}
}

// Test the helper function mergeAllowedArrays directly
func TestMergeAllowedArrays(t *testing.T) {
	tests := []struct {
		name     string
		existing []any
		new      []any
		expected []string
	}{
		{
			name:     "merge with no overlap",
			existing: []any{"tool1", "tool2"},
			new:      []any{"tool3", "tool4"},
			expected: []string{"tool1", "tool2", "tool3", "tool4"},
		},
		{
			name:     "merge with overlap",
			existing: []any{"tool1", "tool2"},
			new:      []any{"tool2", "tool3"},
			expected: []string{"tool1", "tool2", "tool3"},
		},
		{
			name:     "merge with empty existing",
			existing: []any{},
			new:      []any{"tool1", "tool2"},
			expected: []string{"tool1", "tool2"},
		},
		{
			name:     "merge with empty new",
			existing: []any{"tool1", "tool2"},
			new:      []any{},
			expected: []string{"tool1", "tool2"},
		},
		{
			name:     "merge with both empty",
			existing: []any{},
			new:      []any{},
			expected: []string{},
		},
		{
			name:     "merge with duplicates in input",
			existing: []any{"tool1", "tool1", "tool2"},
			new:      []any{"tool2", "tool3", "tool3"},
			expected: []string{"tool1", "tool2", "tool3"},
		},
		{
			name:     "merge with nil arrays",
			existing: nil,
			new:      []any{"tool1"},
			expected: []string{"tool1"},
		},
		{
			name:     "merge with non-string values (should be converted)",
			existing: []any{"tool1", 123, true},
			new:      []any{"tool2", 456, false},
			expected: []string{"tool1", "tool2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeAllowedArrays(tt.existing, tt.new)

			// Convert []any result to []string for comparison
			var resultStrings []string
			for _, item := range result {
				if str, ok := item.(string); ok {
					resultStrings = append(resultStrings, str)
				}
			}

			if !stringSlicesEqual(resultStrings, tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, resultStrings)
			}
		})
	}
}

// Helper functions for testing

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle ||
			containsSubStr(haystack, needle))
}

func containsSubStr(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}

	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if bv, exists := b[k]; !exists || !valuesEqual(v, bv) {
			return false
		}
	}

	return true
}

func valuesEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i, v := range a {
		if v != b[i] {
			return false
		}
	}

	return true
}
