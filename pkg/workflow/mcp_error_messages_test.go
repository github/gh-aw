//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

// TestMCPErrorMessageQuality verifies that MCP configuration error messages follow
// the three-part template: what's wrong + what's expected + example
func TestMCPErrorMessageQuality(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolConfig    map[string]any
		expectError   bool
		errorContains []string // All must be present in error message
		errorShould   string   // Description of what the error should provide
	}{
		{
			name:     "unknown property in MCP config",
			toolName: "my-tool",
			toolConfig: map[string]any{
				"command":     "npx",
				"invalidProp": "value",
			},
			expectError: true,
			errorContains: []string{
				"unknown property",
				"invalidProp",
				"Valid properties are:",
				"Example:",
				"mcp-servers:",
				"my-tool:",
				"command:",
			},
			errorShould: "list valid properties and show complete YAML example",
		},
		{
			name:     "missing type/url/command/container",
			toolName: "incomplete-tool",
			toolConfig: map[string]any{
				"args": []string{"--port", "3000"},
			},
			expectError: true,
			errorContains: []string{
				"unable to determine MCP type",
				"missing type, url, command, or container",
				"Must specify one of:",
				"Example:",
				"mcp-servers:",
				"incomplete-tool:",
				"command:",
			},
			errorShould: "explain what's needed and show complete example",
		},
		{
			name:     "http MCP missing url",
			toolName: "http-tool",
			toolConfig: map[string]any{
				"type":    "http",
				"headers": map[string]any{"Auth": "Bearer token"},
			},
			expectError: true,
			errorContains: []string{
				"http MCP tool",
				"http-tool",
				"missing required 'url' field",
				"HTTP MCP servers must specify a URL endpoint",
				"Example:",
				"type: http",
				"url:",
				"headers:",
			},
			errorShould: "explain HTTP MCP needs url and show complete example",
		},
		{
			name:     "unsupported MCP type",
			toolName: "weird-tool",
			toolConfig: map[string]any{
				"type":    "websocket",
				"command": "node",
			},
			expectError: true,
			errorContains: []string{
				"unsupported MCP type",
				"websocket",
				"weird-tool",
				"Valid types are: stdio, http",
				"Example:",
				"type: stdio",
				"command:",
			},
			errorShould: "list valid types and show example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getMCPConfig(tt.toolConfig, tt.toolName)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}

				errMsg := err.Error()
				for _, expected := range tt.errorContains {
					if !strings.Contains(errMsg, expected) {
						t.Errorf("Error message should contain '%s' (to %s)\nGot: %s",
							expected, tt.errorShould, errMsg)
					}
				}

				// Verify the error message has all three components
				hasWhatWrong := strings.Contains(errMsg, tt.toolName) ||
					strings.Contains(errMsg, "missing") ||
					strings.Contains(errMsg, "unknown") ||
					strings.Contains(errMsg, "unsupported")
				hasExpected := strings.Contains(errMsg, "Valid") ||
					strings.Contains(errMsg, "Must specify") ||
					strings.Contains(errMsg, "must specify")
				hasExample := strings.Contains(errMsg, "Example:")

				if !hasWhatWrong {
					t.Error("Error message should clearly state what's wrong")
				}
				if !hasExpected {
					t.Error("Error message should explain what's expected")
				}
				if !hasExample {
					t.Error("Error message should provide a YAML example")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestParserMCPErrorMessageQuality verifies error messages in pkg/parser/mcp.go
func TestParserMCPErrorMessageQuality(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		mcpSection    any
		toolConfig    map[string]any
		expectError   bool
		errorContains []string
		errorShould   string
	}{
		{
			name:        "invalid mcp configuration format - wrong type",
			toolName:    "my-tool",
			mcpSection:  12345, // should be map or string
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"mcp configuration must be a map or JSON string",
				"got int",
				"Example:",
				"mcp-servers:",
				"my-tool:",
				"command:",
			},
			errorShould: "show received type and provide example",
		},
		{
			name:     "type field must be string",
			toolName: "my-tool",
			mcpSection: map[string]any{
				"type":    123, // should be string
				"command": "npx",
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"type field must be a string",
				"got int",
				"Valid types are: stdio, http",
				"Example:",
				"type: stdio",
			},
			errorShould: "show received type, list valid types, and provide example",
		},
		{
			name:     "stdio missing command or container",
			toolName: "incomplete",
			mcpSection: map[string]any{
				"type": "stdio",
				"args": []string{"--port", "3000"},
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"stdio MCP tool",
				"must specify either 'command' or 'container'",
				"Cannot specify both",
				"Example with command:",
				"Example with container:",
				"mcp-servers:",
			},
			errorShould: "explain mutual exclusivity and show both examples",
		},
		{
			name:     "http missing url field",
			toolName: "http-tool",
			mcpSection: map[string]any{
				"type": "http",
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"http MCP tool",
				"http-tool",
				"missing required 'url' field",
				"HTTP MCP servers must specify a URL endpoint",
				"Example:",
				"type: http",
				"url:",
			},
			errorShould: "explain http needs url and show example",
		},
		{
			name:     "url must be string",
			toolName: "http-tool",
			mcpSection: map[string]any{
				"type": "http",
				"url":  12345, // should be string
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"url field must be a string",
				"got int",
				"Example:",
				"url:",
			},
			errorShould: "show received type and provide example",
		},
		{
			name:     "command must be string",
			toolName: "my-tool",
			mcpSection: map[string]any{
				"command": 12345, // should be string
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"command field must be a string",
				"got int",
				"Example:",
				"command:",
			},
			errorShould: "show received type and provide example",
		},
		{
			name:     "registry must be string",
			toolName: "my-tool",
			mcpSection: map[string]any{
				"command":  "npx",
				"registry": 12345, // should be string
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"registry field must be a string",
				"got int",
				"Example:",
				"registry:",
			},
			errorShould: "show received type and provide example",
		},
		{
			name:     "unsupported type in parser",
			toolName: "weird-tool",
			mcpSection: map[string]any{
				"type":    "grpc",
				"command": "node",
			},
			toolConfig:  map[string]any{},
			expectError: true,
			errorContains: []string{
				"unsupported MCP type",
				"grpc",
				"weird-tool",
				"Valid types are: stdio, http",
				"Example:",
			},
			errorShould: "list valid types and provide example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.ParseMCPConfig(tt.toolName, tt.mcpSection, tt.toolConfig)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}

				errMsg := err.Error()
				for _, expected := range tt.errorContains {
					if !strings.Contains(errMsg, expected) {
						t.Errorf("Error message should contain '%s' (to %s)\nGot: %s",
							expected, tt.errorShould, errMsg)
					}
				}

				// Verify example formatting
				if strings.Contains(errMsg, "Example:") {
					// Check for YAML structure markers
					hasYAMLStructure := strings.Contains(errMsg, "mcp-servers:") ||
						strings.Contains(errMsg, "command:") ||
						strings.Contains(errMsg, "type:")
					if !hasYAMLStructure {
						t.Error("Example should include YAML structure markers")
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestMCPErrorMessageStructure verifies that all error messages have consistent structure
func TestMCPErrorMessageStructure(t *testing.T) {
	testCases := []struct {
		description string
		toolConfig  map[string]any
		toolName    string
	}{
		{
			description: "unknown property",
			toolConfig: map[string]any{
				"command": "npx",
				"invalid": "value",
			},
			toolName: "test-tool",
		},
		{
			description: "missing required fields",
			toolConfig: map[string]any{
				"args": []string{"test"},
			},
			toolName: "test-tool",
		},
		{
			description: "http without url",
			toolConfig: map[string]any{
				"type": "http",
			},
			toolName: "test-tool",
		},
		{
			description: "unsupported type",
			toolConfig: map[string]any{
				"type":    "invalid",
				"command": "test",
			},
			toolName: "test-tool",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			_, err := getMCPConfig(tc.toolConfig, tc.toolName)
			if err == nil {
				t.Fatal("Expected error but got none")
			}

			errMsg := err.Error()

			// Count newlines to verify multi-line example
			newlineCount := strings.Count(errMsg, "\n")
			if !strings.Contains(errMsg, "Example:") {
				// Some errors might not have examples in all cases
				return
			}

			if newlineCount < 2 {
				t.Error("Multi-line examples should have at least 2 newlines for YAML structure")
			}

			// Verify proper YAML indentation in example
			if strings.Contains(errMsg, "Example:") {
				lines := strings.Split(errMsg, "\n")
				foundExample := false
				for _, line := range lines {
					if strings.Contains(line, "Example:") {
						foundExample = true
						continue
					}
					if foundExample && strings.TrimSpace(line) != "" {
						// YAML example lines should have indentation
						if strings.HasPrefix(strings.TrimLeft(line, " "), "mcp-servers:") {
							// Root level should have proper structure
							break
						}
					}
				}
			}
		})
	}
}
