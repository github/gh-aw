//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestRenderSharedMCPConfig_CopilotFields(t *testing.T) {
	tests := []struct {
		name              string
		toolConfig        map[string]any
		renderer          MCPConfigRenderer
		expectedContent   []string
		unexpectedContent []string
	}{
		{
			name: "Copilot engine with stdio MCP server",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
				"args":    []string{"run", "--rm", "-i", "mcp/time"},
				"env":     map[string]any{"TZ": "UTC"},
				"allowed": []string{"get_current_time"},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			},
			expectedContent: []string{
				`"type": "stdio"`, // stdio type for copilot
				`"tools": [`,
				`"get_current_time"`,
				`"command": "docker"`,
				`"args": [`,
				`"env": {`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "Copilot engine with HTTP MCP server",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			},
			expectedContent: []string{
				`"type": "http"`, // http stays http for copilot
				`"tools": [`,
				`"*"`, // default to all tools when no allowed specified
				`"url": "https://api.example.com/mcp"`,
				`"headers": {`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "Claude engine with stdio MCP server (no copilot fields)",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "npx",
				"args":    []string{"-y", "my-server"},
				"env":     map[string]any{"NODE_ENV": "production"},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			expectedContent: []string{
				// After auto-containerization, npx becomes container with entrypoint
				`"type": "stdio"`,
				`"container": "node:lts-alpine"`, // Auto-assigned container for npx
				`"entrypoint": "npx"`,
				`"entrypointArgs": [`,
				`"env": {`,
			},
			unexpectedContent: []string{
				`"tools":`, // should NOT include tools field
			},
		},
		{
			name: "Claude engine with HTTP MCP server (no copilot fields)",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer token",
				},
			},
			renderer: MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: false,
			},
			expectedContent: []string{
				`"type": "http"`,
				`"url": "https://api.example.com/mcp"`,
				`"headers": {`,
			},
			unexpectedContent: []string{
				`"tools":`, // should NOT include tools field
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			err := renderSharedMCPConfig(&output, "test-tool", tt.toolConfig, tt.renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()

			// Check expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}
		})
	}
}

func TestRenderSharedMCPConfig_ToolsFieldGeneration(t *testing.T) {
	tests := []struct {
		name          string
		toolConfig    map[string]any
		expectedTools string
	}{
		{
			name: "Specific allowed tools",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
				"allowed": []string{"get_time", "set_timezone"},
			},
			expectedTools: `"tools": [
    "get_time",
    "set_timezone"
  ]`,
		},
		{
			name: "No allowed tools - defaults to all",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
			},
			expectedTools: `"tools": [
    "*"
  ]`,
		},
		{
			name: "Empty allowed array - defaults to all",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "docker",
				"allowed": []string{},
			},
			expectedTools: `"tools": [
    "*"
  ]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: true,
			}

			var output strings.Builder
			err := renderSharedMCPConfig(&output, "test-tool", tt.toolConfig, renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()
			if !strings.Contains(result, tt.expectedTools) {
				t.Errorf("Expected tools format not found:\n%q\nActual output:\n%s", tt.expectedTools, result)
			}
		})
	}
}

func TestRenderSharedMCPConfig_TypeConversion(t *testing.T) {
	tests := []struct {
		name           string
		inputType      string
		copilotFields  bool
		expectedType   string
		shouldHaveType bool
	}{
		{
			name:           "stdio to local conversion for copilot",
			inputType:      "stdio",
			copilotFields:  true,
			expectedType:   `"type": "stdio"`,
			shouldHaveType: true,
		},
		{
			name:           "http stays http for copilot",
			inputType:      "http",
			copilotFields:  true,
			expectedType:   `"type": "http"`,
			shouldHaveType: true,
		},
		{
			name:           "stdio included for claude",
			inputType:      "stdio",
			copilotFields:  false,
			expectedType:   `"type":`,
			shouldHaveType: true,
		},
		{
			name:           "http included for claude",
			inputType:      "http",
			copilotFields:  false,
			expectedType:   `"type":`,
			shouldHaveType: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolConfig := map[string]any{
				"type": tt.inputType,
			}

			if tt.inputType == "http" {
				toolConfig["url"] = "https://api.example.com/mcp"
			} else {
				toolConfig["command"] = "test-command"
			}

			renderer := MCPConfigRenderer{
				IndentLevel:           "  ",
				Format:                "json",
				RequiresCopilotFields: tt.copilotFields,
			}

			var output strings.Builder
			err := renderSharedMCPConfig(&output, "test-tool", toolConfig, renderer)
			if err != nil {
				t.Fatalf("renderSharedMCPConfig failed: %v", err)
			}

			result := output.String()

			if tt.shouldHaveType {
				if !strings.Contains(result, tt.expectedType) {
					t.Errorf("Expected type field not found: %q\nActual output:\n%s", tt.expectedType, result)
				}
			} else {
				if strings.Contains(result, tt.expectedType) {
					t.Errorf("Type field should not be present, but found: %q\nActual output:\n%s", tt.expectedType, result)
				}
			}
		})
	}
}
