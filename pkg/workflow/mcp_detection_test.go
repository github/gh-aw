//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasMCPServers(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     bool
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			expected:     false,
		},
		{
			name: "workflow with github tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": true,
				},
			},
			expected: true,
		},
		{
			name: "workflow with playwright tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"playwright": map[string]any{
						"version": "0.1.18",
					},
				},
			},
			expected: false,
		},
		{
			name: "workflow with cache-memory tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"cache-memory": true,
				},
			},
			expected: true,
		},
		{
			name: "workflow with agentic-workflows tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"agentic-workflows": true,
				},
			},
			expected: true,
		},
		{
			name: "workflow with disabled github tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": false,
				},
			},
			expected: false,
		},
		{
			name: "workflow with custom MCP tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"custom-mcp": map[string]any{
						"type": "mcp",
						"url":  "http://example.com",
					},
				},
			},
			expected: true,
		},
		{
			name: "workflow with safe-outputs enabled",
			workflowData: &WorkflowData{
				Tools: map[string]any{},
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{},
				},
			},
			expected: true,
		},
		{
			name: "workflow with mcp-scripts enabled",
			workflowData: &WorkflowData{
				Tools: map[string]any{},
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"test-tool": {
							Name: "test-tool",
							Run:  "echo test",
						},
					},
				},
				Features: map[string]any{
					"mcp-scripts": true,
				},
			},
			expected: true,
		},
		{
			name: "workflow with no MCP servers",
			workflowData: &WorkflowData{
				Tools: map[string]any{},
			},
			expected: false,
		},
		{
			name: "workflow with multiple MCP tools",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github":     true,
					"playwright": true,
				},
			},
			expected: true,
		},
		{
			name: "workflow with mixed enabled and disabled tools",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github":     false,
					"playwright": true,
				},
			},
			expected: false,
		},
		{
			name: "workflow with playwright in CLI mode is not an MCP server",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"playwright": map[string]any{
						"mode": "cli",
					},
				},
			},
			expected: false,
		},
		{
			name: "workflow with removed playwright MCP mode is not an MCP server",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"playwright": map[string]any{
						"mode": "mcp",
					},
				},
			},
			expected: false,
		},
		{
			name: "workflow with playwright CLI mode plus other MCP tool still has MCP servers",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": true,
					"playwright": map[string]any{
						"mode": "cli",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasMCPServers(tt.workflowData)
			assert.Equal(t, tt.expected, result, "HasMCPServers result should match expected")
		})
	}
}

func TestHasMCPServers_EdgeCases(t *testing.T) {
	t.Run("empty tools map", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: make(map[string]any),
		}
		result := HasMCPServers(workflowData)
		assert.False(t, result, "Empty tools map should return false")
	})

	t.Run("tools map with non-MCP tools", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash":   true,
				"python": true,
			},
		}
		result := HasMCPServers(workflowData)
		assert.False(t, result, "Non-MCP tools should return false")
	})

	t.Run("safe-outputs with no fields", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			SafeOutputs: &SafeOutputsConfig{},
		}
		result := HasMCPServers(workflowData)
		assert.False(t, result, "Safe-outputs with no fields should return false")
	})

	t.Run("mcp-scripts without feature flag", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{},
			MCPScripts: &MCPScriptsConfig{
				Tools: map[string]*MCPScriptToolConfig{
					"test-tool": {
						Name: "test-tool",
						Run:  "echo test",
					},
				},
			},
			Features: map[string]any{},
		}
		result := HasMCPServers(workflowData)
		// MCP Scripts feature flag is optional now, so this should return true
		assert.True(t, result, "MCP Scripts without feature flag should still return true")
	})
}
