//go:build !integration

package workflow

import (
	"testing"
)

// TestBashToolsMergeCustomWithDefaults tests that custom bash tools get merged with defaults
func TestBashToolsMergeCustomWithDefaults(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		tools       map[string]any
		safeOutputs *SafeOutputsConfig
		expected    []string
	}{
		{
			name: "bash with make commands should include defaults + make",
			tools: map[string]any{
				"bash": []any{"make:*"},
			},
			safeOutputs: nil,
			expected:    []string{"echo", "ls", "pwd", "cat", "head", "tail", "grep", "wc", "sort", "uniq", "date", "yq", "make:*"},
		},
		{
			name: "bash: true should be converted to wildcard",
			tools: map[string]any{
				"bash": true,
			},
			safeOutputs: nil,
			expected:    []string{"*"},
		},
		{
			name: "bash: false should be removed",
			tools: map[string]any{
				"bash": false,
			},
			safeOutputs: nil,
			expected:    nil, // bash should not exist
		},
		{
			name: "bash: true with safe outputs should use wildcard (not add git commands)",
			tools: map[string]any{
				"bash": true,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: []string{"*"},
		},
		{
			name: "bash with multiple commands should include defaults + custom",
			tools: map[string]any{
				"bash": []any{"make:*", "npm:*"},
			},
			safeOutputs: nil,
			expected:    []string{"echo", "ls", "pwd", "cat", "head", "tail", "grep", "wc", "sort", "uniq", "date", "yq", "make:*", "npm:*"},
		},
		{
			name: "bash with empty array should remain empty",
			tools: map[string]any{
				"bash": []any{},
			},
			safeOutputs: nil,
			expected:    []string{},
		},
		{
			name: "bash with make commands and safe outputs should include defaults + make + git",
			tools: map[string]any{
				"bash": []any{"make:*"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: []string{"echo", "ls", "pwd", "cat", "head", "tail", "grep", "wc", "sort", "uniq", "date", "yq", "make:*", "git checkout:*", "git branch:*", "git switch:*", "git add:*", "git rm:*", "git commit:*", "git merge:*", "git status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply default tools
			result := compiler.applyDefaultTools(tt.tools, tt.safeOutputs, nil, nil)

			// Check the bash tools
			bashTools, exists := result["bash"]

			// Handle case where bash should not exist (e.g., bash: false)
			if tt.expected == nil {
				if exists {
					t.Errorf("Expected bash to be removed, but it exists: %v", bashTools)
				}
				return
			}

			if !exists {
				t.Fatalf("Expected bash tools to exist")
			}

			bashArray, ok := bashTools.([]any)
			if !ok {
				t.Fatalf("Expected bash tools to be an array, got %T", bashTools)
			}

			// Convert to string slice
			actual := make([]string, len(bashArray))
			for i, tool := range bashArray {
				actual[i] = tool.(string)
			}

			// Debug: print actual tools
			t.Logf("Actual tools: %v", actual)
			t.Logf("Expected tools: %v", tt.expected)

			// Check length
			if len(actual) != len(tt.expected) {
				t.Errorf("Expected %d tools, got %d. Expected: %v, Actual: %v", len(tt.expected), len(actual), tt.expected, actual)
				return
			}

			// Check all expected tools are present
			actualMap := make(map[string]bool)
			for _, tool := range actual {
				actualMap[tool] = true
			}

			for _, expected := range tt.expected {
				if !actualMap[expected] {
					t.Errorf("Expected tool '%s' not found in actual tools: %v", expected, actual)
				}
			}
		})
	}
}
