//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestCodexEnginePlaywrightToolsExpansion(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name     string
		input    map[string]any
		expected int // Expected number of playwright tools
	}{
		{
			name:     "playwright null expands to copilot agent tools",
			input:    map[string]any{"playwright": nil},
			expected: 21, // Should expand to all 21 copilot agent playwright tools
		},
		{
			name: "playwright with config preserves config and adds tools",
			input: map[string]any{
				"playwright": map[string]any{
					"version": "v1.40.0",
				},
			},
			expected: 21, // Should still expand to all 21 tools
		},
		{
			name:     "no playwright tool",
			input:    map[string]any{"github": nil},
			expected: 0, // No playwright tools expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.expandNeutralToolsToCodexToolsFromMap(tt.input)

			if tt.expected == 0 {
				// Should not have playwright in result
				if _, hasPlaywright := result["playwright"]; hasPlaywright {
					t.Error("Expected no playwright tool in result")
				}
			} else {
				// Should have playwright with correct number of allowed tools
				playwrightTool, hasPlaywright := result["playwright"]
				if !hasPlaywright {
					t.Error("Expected playwright tool in result")
					return
				}

				playwrightConfig, ok := playwrightTool.(map[string]any)
				if !ok {
					t.Error("Expected playwright tool to be a map")
					return
				}

				allowed, hasAllowed := playwrightConfig["allowed"]
				if !hasAllowed {
					t.Error("Expected playwright tool to have 'allowed' field")
					return
				}

				allowedSlice, ok := allowed.([]any)
				if !ok {
					t.Error("Expected 'allowed' field to be a slice")
					return
				}

				if len(allowedSlice) != tt.expected {
					t.Errorf("Expected %d playwright tools, got %d", tt.expected, len(allowedSlice))
				}

				// Verify that all expected copilot agent tools are present
				expectedTools := GetCopilotAgentPlaywrightTools()
				if len(allowedSlice) != len(expectedTools) {
					t.Errorf("Expected %d tools to match copilot agent tools, got %d", len(expectedTools), len(allowedSlice))
				}

				// Check that some key tools are present
				toolsStr := strings.Join(func() []string {
					var tools []string
					for _, tool := range allowedSlice {
						if str, ok := tool.(string); ok {
							tools = append(tools, str)
						}
					}
					return tools
				}(), ",")

				expectedKeyTools := []string{"browser_click", "browser_navigate", "browser_type", "browser_snapshot"}
				for _, keyTool := range expectedKeyTools {
					if !strings.Contains(toolsStr, keyTool) {
						t.Errorf("Expected key tool '%s' to be present in allowed tools: %s", keyTool, toolsStr)
					}
				}
			}
		})
	}
}
