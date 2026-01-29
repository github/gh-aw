//go:build !integration

package workflow

import (
	"testing"
)

func TestDefaultGitHubToolsets(t *testing.T) {
	// Verify the default toolsets match the documented defaults
	expected := []string{"context", "repos", "issues", "pull_requests"}

	if len(DefaultGitHubToolsets) != len(expected) {
		t.Errorf("Expected %d default toolsets, got %d", len(expected), len(DefaultGitHubToolsets))
	}

	for i, toolset := range expected {
		if i >= len(DefaultGitHubToolsets) || DefaultGitHubToolsets[i] != toolset {
			t.Errorf("Expected default toolset[%d] to be %s, got %s", i, toolset, DefaultGitHubToolsets[i])
		}
	}
}

func TestActionFriendlyGitHubToolsets(t *testing.T) {
	// Verify the action-friendly toolsets exclude "users"
	expected := []string{"context", "repos", "issues", "pull_requests"}

	if len(ActionFriendlyGitHubToolsets) != len(expected) {
		t.Errorf("Expected %d action-friendly toolsets, got %d", len(expected), len(ActionFriendlyGitHubToolsets))
	}

	for i, toolset := range expected {
		if i >= len(ActionFriendlyGitHubToolsets) || ActionFriendlyGitHubToolsets[i] != toolset {
			t.Errorf("Expected action-friendly toolset[%d] to be %s, got %s", i, toolset, ActionFriendlyGitHubToolsets[i])
		}
	}

	// Verify "users" is not in action-friendly toolsets
	for _, toolset := range ActionFriendlyGitHubToolsets {
		if toolset == "users" {
			t.Error("Action-friendly toolsets should not include 'users' toolset")
		}
	}
}

func TestParseGitHubToolsets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Empty string returns default",
			input:    "",
			expected: []string{"context", "repos", "issues", "pull_requests"},
		},
		{
			name:     "Default expands to default toolsets",
			input:    "default",
			expected: []string{"context", "repos", "issues", "pull_requests"},
		},
		{
			name:     "Specific toolsets",
			input:    "repos,issues",
			expected: []string{"repos", "issues"},
		},
		{
			name:     "Default plus additional",
			input:    "default,discussions",
			expected: []string{"context", "repos", "issues", "pull_requests", "discussions"},
		},
		{
			name:  "All expands to all toolsets",
			input: "all",
			// Should include all 19 toolsets - we'll check the count
			expected: nil,
		},
		{
			name:     "Deduplication",
			input:    "repos,issues,repos",
			expected: []string{"repos", "issues"},
		},
		{
			name:     "Whitespace handling",
			input:    " repos , issues , pull_requests ",
			expected: []string{"repos", "issues", "pull_requests"},
		},
		{
			name:     "Single toolset",
			input:    "actions",
			expected: []string{"actions"},
		},
		{
			name:     "Multiple with default in middle",
			input:    "actions,default,discussions",
			expected: []string{"actions", "context", "repos", "issues", "pull_requests", "discussions"},
		},
		{
			name:     "Action-friendly expands to action-friendly toolsets",
			input:    "action-friendly",
			expected: []string{"context", "repos", "issues", "pull_requests"},
		},
		{
			name:     "Action-friendly plus additional",
			input:    "action-friendly,discussions",
			expected: []string{"context", "repos", "issues", "pull_requests", "discussions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseGitHubToolsets(tt.input)

			if tt.name == "All expands to all toolsets" {
				// Check that all toolsets are present
				if len(result) != len(toolsetPermissionsMap) {
					t.Errorf("Expected %d toolsets for 'all', got %d", len(toolsetPermissionsMap), len(result))
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d toolsets, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			// Check that all expected toolsets are present (order doesn't matter for some tests)
			resultMap := make(map[string]bool)
			for _, ts := range result {
				resultMap[ts] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("Expected toolset %s not found in result: %v", expected, result)
				}
			}
		})
	}
}

func TestParseGitHubToolsetsPreservesOrder(t *testing.T) {
	// Test that specific toolsets maintain their order
	input := "repos,issues,pull_requests"
	result := ParseGitHubToolsets(input)
	expected := []string{"repos", "issues", "pull_requests"}

	if len(result) != len(expected) {
		t.Fatalf("Expected %d toolsets, got %d", len(expected), len(result))
	}

	for i, toolset := range expected {
		if result[i] != toolset {
			t.Errorf("Expected toolset[%d] to be %s, got %s", i, toolset, result[i])
		}
	}
}

func TestParseGitHubToolsetsDeduplication(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "Duplicate in simple list",
			input:    "repos,issues,repos,issues",
			expected: 2,
		},
		{
			name:     "Default includes duplicates",
			input:    "context,default",
			expected: 4, // context already in default, so only 4 unique
		},
		{
			name:     "All with duplicates",
			input:    "all,repos,issues",
			expected: len(toolsetPermissionsMap), // All toolsets, duplicates ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseGitHubToolsets(tt.input)
			if len(result) != tt.expected {
				t.Errorf("Expected %d unique toolsets, got %d: %v", tt.expected, len(result), result)
			}

			// Verify no duplicates
			seen := make(map[string]bool)
			for _, toolset := range result {
				if seen[toolset] {
					t.Errorf("Found duplicate toolset: %s", toolset)
				}
				seen[toolset] = true
			}
		})
	}
}
