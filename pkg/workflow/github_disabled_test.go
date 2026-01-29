//go:build !integration

package workflow

import (
	"testing"
)

// TestGitHubToolDisabled tests the github: false functionality to disable the GitHub MCP server
func TestGitHubToolDisabled(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		tools        map[string]any
		expectGitHub bool
		expectOthers bool
	}{
		{
			name: "github: false removes github tool",
			tools: map[string]any{
				"github": false,
				"edit":   nil,
			},
			expectGitHub: false,
			expectOthers: true,
		},
		{
			name: "github: nil keeps github tool",
			tools: map[string]any{
				"github": nil,
			},
			expectGitHub: true,
			expectOthers: false,
		},
		{
			name: "github: true keeps github tool",
			tools: map[string]any{
				"github": true,
			},
			expectGitHub: true,
			expectOthers: false,
		},
		{
			name: "github: map keeps github tool",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			expectGitHub: true,
			expectOthers: false,
		},
		{
			name: "no github key adds default github tool",
			tools: map[string]any{
				"edit": nil,
			},
			expectGitHub: true,
			expectOthers: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.applyDefaultTools(tt.tools, nil, nil, nil)

			_, hasGitHub := result["github"]
			if hasGitHub != tt.expectGitHub {
				t.Errorf("Expected github presence to be %v, got %v", tt.expectGitHub, hasGitHub)
			}

			if tt.expectOthers {
				_, hasEdit := result["edit"]
				if !hasEdit {
					t.Error("Expected edit tool to be preserved")
				}
			}
		})
	}
}

// TestHasMCPServersWithGitHubDisabled tests that HasMCPServers correctly handles github: false
func TestHasMCPServersWithGitHubDisabled(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name: "github: false does not count as MCP server",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": false,
				},
			},
			expected: false,
		},
		{
			name: "github: nil counts as MCP server",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": nil,
				},
			},
			expected: true,
		},
		{
			name: "github: map counts as MCP server",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{},
				},
			},
			expected: true,
		},
		{
			name: "github: false with other MCP tool still returns true",
			data: &WorkflowData{
				Tools: map[string]any{
					"github":     false,
					"playwright": nil,
				},
			},
			expected: true,
		},
		{
			name: "github: false with non-MCP tool returns false",
			data: &WorkflowData{
				Tools: map[string]any{
					"github": false,
					"edit":   nil,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasMCPServers(tt.data)
			if result != tt.expected {
				t.Errorf("Expected HasMCPServers to return %v, got %v", tt.expected, result)
			}
		})
	}
}
