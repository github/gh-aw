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
						"version": "v1.41.0",
					},
				},
			},
			expected: true,
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
			name: "workflow with safe-inputs enabled",
			workflowData: &WorkflowData{
				Tools: map[string]any{},
				SafeInputs: &SafeInputsConfig{
					Tools: map[string]*SafeInputToolConfig{
						"test-tool": {
							Name: "test-tool",
							Run:  "echo test",
						},
					},
				},
				Features: map[string]any{
					"safe-inputs": true,
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

	t.Run("safe-inputs without feature flag", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{},
			SafeInputs: &SafeInputsConfig{
				Tools: map[string]*SafeInputToolConfig{
					"test-tool": {
						Name: "test-tool",
						Run:  "echo test",
					},
				},
			},
			Features: map[string]any{},
		}
		result := HasMCPServers(workflowData)
		// Safe-inputs feature flag is optional now, so this should return true
		assert.True(t, result, "Safe-inputs without feature flag should still return true")
	})
}
