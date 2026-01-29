//go:build !integration

package workflow

import "testing"

func TestGetGitHubReadOnly(t *testing.T) {
	tests := []struct {
		name       string
		githubTool any
		expected   bool
	}{
		{
			name: "read-only true",
			githubTool: map[string]any{
				"read-only": true,
			},
			expected: true,
		},
		{
			name: "read-only false",
			githubTool: map[string]any{
				"read-only": false,
			},
			expected: false,
		},
		{
			name:       "no read-only field",
			githubTool: map[string]any{},
			expected:   true, // now defaults to true
		},
		{
			name: "read-only with other fields",
			githubTool: map[string]any{
				"read-only": true,
				"version":   "latest",
				"args":      []string{"--verbose"},
			},
			expected: true,
		},
		{
			name:       "nil tool",
			githubTool: nil,
			expected:   true, // now defaults to true
		},
		{
			name:       "string tool (not map)",
			githubTool: "github",
			expected:   true, // now defaults to true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getGitHubReadOnly(tt.githubTool)
			if result != tt.expected {
				t.Errorf("getGitHubReadOnly() = %v, want %v", result, tt.expected)
			}
		})
	}
}
