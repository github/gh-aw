//go:build !integration

package workflow

import (
	"testing"
)

func TestExpandDefaultToolset(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string returns action-friendly",
			input:    "",
			expected: "context,repos,issues,pull_requests",
		},
		{
			name:     "Default expands to action-friendly toolsets",
			input:    "default",
			expected: "context,repos,issues,pull_requests",
		},
		{
			name:     "Specific toolsets unchanged",
			input:    "repos,issues",
			expected: "repos,issues",
		},
		{
			name:     "Default plus additional",
			input:    "default,discussions",
			expected: "context,repos,issues,pull_requests,discussions",
		},
		{
			name:     "Default with users explicitly added",
			input:    "default,users",
			expected: "context,repos,issues,pull_requests,users",
		},
		{
			name:     "Action-friendly expands to action-friendly toolsets",
			input:    "action-friendly",
			expected: "context,repos,issues,pull_requests",
		},
		{
			name:     "All keyword preserved",
			input:    "all",
			expected: "all",
		},
		{
			name:     "Multiple defaults deduplicated",
			input:    "default,repos,default",
			expected: "context,repos,issues,pull_requests",
		},
		{
			name:     "Default in middle",
			input:    "actions,default,discussions",
			expected: "actions,context,repos,issues,pull_requests,discussions",
		},
		{
			name:     "Whitespace handling",
			input:    " default , discussions ",
			expected: "context,repos,issues,pull_requests,discussions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandDefaultToolset(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGetGitHubToolsetsExpandsDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "No toolsets configured defaults to action-friendly",
			input:    map[string]any{},
			expected: "context,repos,issues,pull_requests",
		},
		{
			name: "Default as array expands to action-friendly",
			input: map[string]any{
				"toolsets": []string{"default"},
			},
			expected: "context,repos,issues,pull_requests",
		},
		{
			name: "Default with additional toolsets",
			input: map[string]any{
				"toolsets": []string{"default", "discussions"},
			},
			expected: "context,repos,issues,pull_requests,discussions",
		},
		{
			name: "Specific toolsets unchanged",
			input: map[string]any{
				"toolsets": []string{"repos", "issues", "pull_requests"},
			},
			expected: "repos,issues,pull_requests",
		},
		{
			name: "Action-friendly expands to action-friendly toolsets",
			input: map[string]any{
				"toolsets": []string{"action-friendly"},
			},
			expected: "context,repos,issues,pull_requests",
		},
		{
			name: "All keyword preserved",
			input: map[string]any{
				"toolsets": []string{"all"},
			},
			expected: "all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getGitHubToolsets(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
