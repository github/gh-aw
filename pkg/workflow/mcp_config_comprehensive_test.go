//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

// TestMapToolConfigGetString tests the GetString method of MapToolConfig
func TestMapToolConfigGetString(t *testing.T) {
	tests := []struct {
		name       string
		config     MapToolConfig
		key        string
		wantValue  string
		wantExists bool
	}{
		{
			name: "existing string key",
			config: MapToolConfig{
				"command": "docker",
			},
			key:        "command",
			wantValue:  "docker",
			wantExists: true,
		},
		{
			name: "non-existent key",
			config: MapToolConfig{
				"command": "docker",
			},
			key:        "missing",
			wantValue:  "",
			wantExists: false,
		},
		{
			name: "key exists but wrong type (int)",
			config: MapToolConfig{
				"port": 8080,
			},
			key:        "port",
			wantValue:  "",
			wantExists: false,
		},
		{
			name: "key exists but wrong type (bool)",
			config: MapToolConfig{
				"enabled": true,
			},
			key:        "enabled",
			wantValue:  "",
			wantExists: false,
		},
		{
			name: "key exists but wrong type (array)",
			config: MapToolConfig{
				"items": []string{"a", "b"},
			},
			key:        "items",
			wantValue:  "",
			wantExists: false,
		},
		{
			name: "key exists but wrong type (map)",
			config: MapToolConfig{
				"config": map[string]any{"key": "value"},
			},
			key:        "config",
			wantValue:  "",
			wantExists: false,
		},
		{
			name:       "empty config",
			config:     MapToolConfig{},
			key:        "anything",
			wantValue:  "",
			wantExists: false,
		},
		{
			name: "empty string value",
			config: MapToolConfig{
				"name": "",
			},
			key:        "name",
			wantValue:  "",
			wantExists: true,
		},
		{
			name: "string with special characters",
			config: MapToolConfig{
				"url": "https://example.com/path?query=value&other=test",
			},
			key:        "url",
			wantValue:  "https://example.com/path?query=value&other=test",
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotExists := tt.config.GetString(tt.key)
			if gotExists != tt.wantExists {
				t.Errorf("GetString() exists = %v, want %v", gotExists, tt.wantExists)
			}
			if gotValue != tt.wantValue {
				t.Errorf("GetString() value = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

// TestMapToolConfigGetStringArray tests the GetStringArray method of MapToolConfig
func TestMapToolConfigGetStringArray(t *testing.T) {
	tests := []struct {
		name       string
		config     MapToolConfig
		key        string
		wantValue  []string
		wantExists bool
	}{
		{
			name: "existing []any array with strings",
			config: MapToolConfig{
				"args": []any{"run", "--rm", "-i"},
			},
			key:        "args",
			wantValue:  []string{"run", "--rm", "-i"},
			wantExists: true,
		},
		{
			name: "existing []string array",
			config: MapToolConfig{
				"args": []string{"run", "--rm"},
			},
			key:        "args",
			wantValue:  []string{"run", "--rm"},
			wantExists: true,
		},
		{
			name: "non-existent key",
			config: MapToolConfig{
				"args": []string{"test"},
			},
			key:        "missing",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "key exists but wrong type (string)",
			config: MapToolConfig{
				"args": "not-an-array",
			},
			key:        "args",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "key exists but wrong type (int)",
			config: MapToolConfig{
				"count": 42,
			},
			key:        "count",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "key exists but wrong type (map)",
			config: MapToolConfig{
				"config": map[string]any{"key": "value"},
			},
			key:        "config",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "empty config",
			config:     MapToolConfig{},
			key:        "args",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "empty array",
			config: MapToolConfig{
				"args": []any{},
			},
			key:        "args",
			wantValue:  []string{},
			wantExists: true,
		},
		{
			name: "[]any with mixed types (filters non-strings)",
			config: MapToolConfig{
				"mixed": []any{"string1", 123, "string2", true, "string3"},
			},
			key:        "mixed",
			wantValue:  []string{"string1", "string2", "string3"},
			wantExists: true,
		},
		{
			name: "[]any with only non-strings",
			config: MapToolConfig{
				"numbers": []any{1, 2, 3},
			},
			key:        "numbers",
			wantValue:  []string{},
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotExists := tt.config.GetStringArray(tt.key)
			if gotExists != tt.wantExists {
				t.Errorf("GetStringArray() exists = %v, want %v", gotExists, tt.wantExists)
			}
			if tt.wantValue == nil {
				if gotValue != nil {
					t.Errorf("GetStringArray() value = %v, want nil", gotValue)
				}
			} else {
				if len(gotValue) != len(tt.wantValue) {
					t.Errorf("GetStringArray() len = %d, want %d", len(gotValue), len(tt.wantValue))
				}
				for i, v := range tt.wantValue {
					if i < len(gotValue) && gotValue[i] != v {
						t.Errorf("GetStringArray()[%d] = %v, want %v", i, gotValue[i], v)
					}
				}
			}
		})
	}
}

// TestMapToolConfigGetStringMap tests the GetStringMap method of MapToolConfig
func TestMapToolConfigGetStringMap(t *testing.T) {
	tests := []struct {
		name       string
		config     MapToolConfig
		key        string
		wantValue  map[string]string
		wantExists bool
	}{
		{
			name: "existing map[string]any with string values",
			config: MapToolConfig{
				"env": map[string]any{
					"KEY1": "value1",
					"KEY2": "value2",
				},
			},
			key: "env",
			wantValue: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			wantExists: true,
		},
		{
			name: "existing map[string]string",
			config: MapToolConfig{
				"headers": map[string]string{
					"Authorization": "Bearer token",
					"Content-Type":  "application/json",
				},
			},
			key: "headers",
			wantValue: map[string]string{
				"Authorization": "Bearer token",
				"Content-Type":  "application/json",
			},
			wantExists: true,
		},
		{
			name: "non-existent key",
			config: MapToolConfig{
				"env": map[string]string{"KEY": "value"},
			},
			key:        "missing",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "key exists but wrong type (string)",
			config: MapToolConfig{
				"env": "not-a-map",
			},
			key:        "env",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "key exists but wrong type (array)",
			config: MapToolConfig{
				"env": []string{"not", "a", "map"},
			},
			key:        "env",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "empty config",
			config:     MapToolConfig{},
			key:        "env",
			wantValue:  nil,
			wantExists: false,
		},
		{
			name: "empty map",
			config: MapToolConfig{
				"env": map[string]any{},
			},
			key:        "env",
			wantValue:  map[string]string{},
			wantExists: true,
		},
		{
			name: "map[string]any with mixed value types (filters non-strings)",
			config: MapToolConfig{
				"mixed": map[string]any{
					"string_val": "text",
					"int_val":    42,
					"bool_val":   true,
					"other_str":  "other",
				},
			},
			key: "mixed",
			wantValue: map[string]string{
				"string_val": "text",
				"other_str":  "other",
			},
			wantExists: true,
		},
		{
			name: "map[string]any with only non-string values",
			config: MapToolConfig{
				"numbers": map[string]any{
					"one":   1,
					"two":   2,
					"three": 3,
				},
			},
			key:        "numbers",
			wantValue:  map[string]string{},
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotExists := tt.config.GetStringMap(tt.key)
			if gotExists != tt.wantExists {
				t.Errorf("GetStringMap() exists = %v, want %v", gotExists, tt.wantExists)
			}
			if tt.wantValue == nil {
				if gotValue != nil {
					t.Errorf("GetStringMap() value = %v, want nil", gotValue)
				}
			} else {
				if len(gotValue) != len(tt.wantValue) {
					t.Errorf("GetStringMap() len = %d, want %d", len(gotValue), len(tt.wantValue))
				}
				for k, v := range tt.wantValue {
					if gotValue[k] != v {
						t.Errorf("GetStringMap()[%q] = %v, want %v", k, gotValue[k], v)
					}
				}
			}
		})
	}
}

// TestIsMCPType tests the unified parser.IsMCPType function
func TestIsMCPType(t *testing.T) {
	tests := []struct {
		name     string
		typeStr  string
		expected bool
	}{
		{
			name:     "stdio type",
			typeStr:  "stdio",
			expected: true,
		},
		{
			name:     "http type",
			typeStr:  "http",
			expected: true,
		},
		{
			name:     "local type",
			typeStr:  "local",
			expected: true,
		},
		{
			name:     "empty string",
			typeStr:  "",
			expected: false,
		},
		{
			name:     "unknown type",
			typeStr:  "unknown",
			expected: false,
		},
		{
			name:     "docker type (not valid)",
			typeStr:  "docker",
			expected: false,
		},
		{
			name:     "websocket type (not valid)",
			typeStr:  "websocket",
			expected: false,
		},
		{
			name:     "grpc type (not valid)",
			typeStr:  "grpc",
			expected: false,
		},
		{
			name:     "mixed case STDIO",
			typeStr:  "STDIO",
			expected: false,
		},
		{
			name:     "mixed case Http",
			typeStr:  "Http",
			expected: false,
		},
		{
			name:     "whitespace padded",
			typeStr:  " stdio ",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.IsMCPType(tt.typeStr)
			if got != tt.expected {
				t.Errorf("parser.IsMCPType(%q) = %v, want %v", tt.typeStr, got, tt.expected)
			}
		})
	}
}

// TestCollectHTTPMCPHeaderSecretsComprehensive provides comprehensive testing for collectHTTPMCPHeaderSecrets
func TestCollectHTTPMCPHeaderSecretsComprehensive(t *testing.T) {
	tests := []struct {
		name         string
		tools        map[string]any
		wantSecrets  map[string]string
		wantMinCount int
	}{
		{
			name:         "empty tools map",
			tools:        map[string]any{},
			wantSecrets:  map[string]string{},
			wantMinCount: 0,
		},
		{
			name: "no HTTP MCP tools",
			tools: map[string]any{
				"tool1": map[string]any{
					"command": "npx",
					"args":    []any{"-y", "test"},
				},
			},
			wantSecrets:  map[string]string{},
			wantMinCount: 0,
		},
		{
			name: "HTTP MCP tool without headers",
			tools: map[string]any{
				"api": map[string]any{
					"type": "http",
					"url":  "https://api.example.com/mcp",
				},
			},
			wantSecrets:  map[string]string{},
			wantMinCount: 0,
		},
		{
			name: "HTTP MCP tool with plain headers (no secrets)",
			tools: map[string]any{
				"api": map[string]any{
					"type": "http",
					"url":  "https://api.example.com/mcp",
					"headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
			},
			wantSecrets:  map[string]string{},
			wantMinCount: 0,
		},
		{
			name: "stdio type tool (should be ignored)",
			tools: map[string]any{
				"cli": map[string]any{
					"type":    "stdio",
					"command": "docker",
					"args":    []any{"run", "test"},
				},
			},
			wantSecrets:  map[string]string{},
			wantMinCount: 0,
		},
		{
			name: "non-map tool value (should be ignored)",
			tools: map[string]any{
				"invalid": "not-a-map",
			},
			wantSecrets:  map[string]string{},
			wantMinCount: 0,
		},
		{
			name: "mixed tools - only HTTP with secrets collected",
			tools: map[string]any{
				"stdio-tool": map[string]any{
					"command": "npx",
				},
				"http-tool": map[string]any{
					"type": "http",
					"url":  "https://api.example.com",
					"headers": map[string]any{
						"Authorization": "Bearer ${{ secrets.API_KEY }}",
					},
				},
			},
			wantMinCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectHTTPMCPHeaderSecrets(tt.tools)

			if tt.wantSecrets != nil {
				if len(result) != len(tt.wantSecrets) {
					t.Errorf("collectHTTPMCPHeaderSecrets() returned %d secrets, want %d", len(result), len(tt.wantSecrets))
				}
				for key, value := range tt.wantSecrets {
					if result[key] != value {
						t.Errorf("collectHTTPMCPHeaderSecrets()[%q] = %q, want %q", key, result[key], value)
					}
				}
			}

			if len(result) < tt.wantMinCount {
				t.Errorf("collectHTTPMCPHeaderSecrets() returned %d secrets, want at least %d", len(result), tt.wantMinCount)
			}
		})
	}
}

// TestRenderSerenaMCPConfigWithOptions tests the renderSerenaMCPConfigWithOptions function
func TestRenderSerenaMCPConfigWithOptions(t *testing.T) {
	tests := []struct {
		name                 string
		serenaTool           any
		isLast               bool
		includeCopilotFields bool
		inlineArgs           bool
		expectedContent      []string
		unexpectedContent    []string
	}{
		{
			name:                 "basic Serena config not last - Claude format",
			serenaTool:           map[string]any{},
			isLast:               false,
			includeCopilotFields: false,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
				`"entrypoint": "serena"`,
				`"entrypointArgs"`,
				`"start-mcp-server"`,
				`              },`,
			},
			unexpectedContent: []string{
				`"type"`,
				`"tools"`,
			},
		},
		{
			name:                 "Serena config is last - Claude format",
			serenaTool:           map[string]any{},
			isLast:               true,
			includeCopilotFields: false,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
				`              }`,
			},
			unexpectedContent: []string{
				`"type"`,
				`"tools"`,
				`              },`,
			},
		},
		{
			name:                 "Serena config with Copilot fields",
			serenaTool:           map[string]any{},
			isLast:               true,
			includeCopilotFields: true,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
			},
			unexpectedContent: []string{},
		},
		{
			name:                 "Serena config with inline args format",
			serenaTool:           map[string]any{},
			isLast:               false,
			includeCopilotFields: true,
			inlineArgs:           true,
			expectedContent: []string{
				`"serena": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
				`"entrypointArgs": ["start-mcp-server", "--context", "codex", "--project", "${{ github.workspace }}"]`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "Serena config with custom args",
			serenaTool: map[string]any{
				"args": []any{"--verbose", "--debug"},
			},
			isLast:               false,
			includeCopilotFields: false,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
				`"--verbose"`,
				`"--debug"`,
			},
			unexpectedContent: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderSerenaMCPConfigWithOptions(&output, tt.serenaTool, tt.isLast, tt.includeCopilotFields, tt.inlineArgs)

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

// TestGetMCPConfigStdioTypeInference tests type inference for stdio MCP configurations
func TestGetMCPConfigStdioTypeInference(t *testing.T) {
	tests := []struct {
		name         string
		toolConfig   map[string]any
		expectedType string
		wantError    bool
	}{
		{
			name: "infer stdio from command field",
			toolConfig: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@test/tool"},
			},
			expectedType: "stdio",
			wantError:    false,
		},
		{
			name: "infer stdio from container field",
			toolConfig: map[string]any{
				"container": "test/image:latest",
			},
			expectedType: "stdio",
			wantError:    false,
		},
		{
			name: "infer http from url field",
			toolConfig: map[string]any{
				"url": "https://api.example.com/mcp",
			},
			expectedType: "http",
			wantError:    false,
		},
		{
			name: "explicit stdio type",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "node",
			},
			expectedType: "stdio",
			wantError:    false,
		},
		{
			name: "explicit http type",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com",
			},
			expectedType: "http",
			wantError:    false,
		},
		{
			name: "local type normalized to stdio",
			toolConfig: map[string]any{
				"type":    "local",
				"command": "node",
			},
			expectedType: "stdio",
			wantError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getMCPConfig(tt.toolConfig, "test-tool")

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Type != tt.expectedType {
				t.Errorf("getMCPConfig() type = %q, want %q", result.Type, tt.expectedType)
			}
		})
	}
}

// TestGetMCPConfigHTTPType tests HTTP type MCP configuration parsing
func TestGetMCPConfigHTTPType(t *testing.T) {
	tests := []struct {
		name            string
		toolConfig      map[string]any
		expectedURL     string
		expectedHeaders map[string]string
		wantError       bool
	}{
		{
			name: "basic HTTP config",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
			},
			expectedURL:     "https://api.example.com/mcp",
			expectedHeaders: map[string]string{},
			wantError:       false,
		},
		{
			name: "HTTP config with headers",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"Authorization": "Bearer token",
					"X-Custom":      "value",
				},
			},
			expectedURL: "https://api.example.com/mcp",
			expectedHeaders: map[string]string{
				"Authorization": "Bearer token",
				"X-Custom":      "value",
			},
			wantError: false,
		},
		{
			name: "HTTP config missing url",
			toolConfig: map[string]any{
				"type": "http",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getMCPConfig(tt.toolConfig, "test-tool")

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Type != "http" {
				t.Errorf("getMCPConfig() type = %q, want %q", result.Type, "http")
			}

			if result.URL != tt.expectedURL {
				t.Errorf("getMCPConfig() URL = %q, want %q", result.URL, tt.expectedURL)
			}

			if len(result.Headers) != len(tt.expectedHeaders) {
				t.Errorf("getMCPConfig() headers count = %d, want %d", len(result.Headers), len(tt.expectedHeaders))
			}

			for key, value := range tt.expectedHeaders {
				if result.Headers[key] != value {
					t.Errorf("getMCPConfig() headers[%q] = %q, want %q", key, result.Headers[key], value)
				}
			}
		})
	}
}

// TestGetMCPConfigWithRegistry tests registry field handling
func TestGetMCPConfigWithRegistry(t *testing.T) {
	tests := []struct {
		name             string
		toolConfig       map[string]any
		expectedRegistry string
	}{
		{
			name: "stdio config with registry",
			toolConfig: map[string]any{
				"command":  "npx",
				"args":     []any{"-y", "@test/tool"},
				"registry": "https://api.mcp.github.com/v0/servers/test/tool",
			},
			expectedRegistry: "https://api.mcp.github.com/v0/servers/test/tool",
		},
		{
			name: "http config with registry",
			toolConfig: map[string]any{
				"type":     "http",
				"url":      "https://api.example.com/mcp",
				"registry": "https://registry.example.com",
			},
			expectedRegistry: "https://registry.example.com",
		},
		{
			name: "config without registry",
			toolConfig: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@test/tool"},
			},
			expectedRegistry: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getMCPConfig(tt.toolConfig, "test-tool")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Registry != tt.expectedRegistry {
				t.Errorf("getMCPConfig() Registry = %q, want %q", result.Registry, tt.expectedRegistry)
			}
		})
	}
}

// TestGetMCPConfigWithProxyArgs tests proxy-args field handling
func TestGetMCPConfigWithProxyArgs(t *testing.T) {
	tests := []struct {
		name              string
		toolConfig        map[string]any
		expectedProxyArgs []string
	}{
		{
			name: "stdio config with proxy-args",
			toolConfig: map[string]any{
				"command":    "docker",
				"args":       []any{"run", "test"},
				"proxy-args": []any{"--proxy", "http://proxy.example.com:8080"},
			},
			expectedProxyArgs: []string{"--proxy", "http://proxy.example.com:8080"},
		},
		{
			name: "config without proxy-args",
			toolConfig: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@test/tool"},
			},
			expectedProxyArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getMCPConfig(tt.toolConfig, "test-tool")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(result.ProxyArgs) != len(tt.expectedProxyArgs) {
				t.Errorf("getMCPConfig() ProxyArgs count = %d, want %d", len(result.ProxyArgs), len(tt.expectedProxyArgs))
			}

			for i, expected := range tt.expectedProxyArgs {
				if i < len(result.ProxyArgs) && result.ProxyArgs[i] != expected {
					t.Errorf("getMCPConfig() ProxyArgs[%d] = %q, want %q", i, result.ProxyArgs[i], expected)
				}
			}
		})
	}
}

// TestGetMCPConfigWithAllowedTools tests allowed tools field handling
func TestGetMCPConfigWithAllowedTools(t *testing.T) {
	tests := []struct {
		name            string
		toolConfig      map[string]any
		expectedAllowed []string
	}{
		{
			name: "config with allowed tools",
			toolConfig: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@test/tool"},
				"allowed": []any{"tool1", "tool2", "tool3"},
			},
			expectedAllowed: []string{"tool1", "tool2", "tool3"},
		},
		{
			name: "config without allowed tools",
			toolConfig: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@test/tool"},
			},
			expectedAllowed: nil,
		},
		{
			name: "config with empty allowed array",
			toolConfig: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@test/tool"},
				"allowed": []any{},
			},
			expectedAllowed: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getMCPConfig(tt.toolConfig, "test-tool")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(result.Allowed) != len(tt.expectedAllowed) {
				t.Errorf("getMCPConfig() Allowed count = %d, want %d", len(result.Allowed), len(tt.expectedAllowed))
			}

			for i, expected := range tt.expectedAllowed {
				if i < len(result.Allowed) && result.Allowed[i] != expected {
					t.Errorf("getMCPConfig() Allowed[%d] = %q, want %q", i, result.Allowed[i], expected)
				}
			}
		})
	}
}

// TestHasMCPConfigComprehensive provides additional tests for hasMCPConfig function
func TestHasMCPConfigComprehensive(t *testing.T) {
	tests := []struct {
		name       string
		toolConfig map[string]any
		wantHasMCP bool
		wantType   string
	}{
		{
			name: "explicit stdio type",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "test",
			},
			wantHasMCP: true,
			wantType:   "stdio",
		},
		{
			name: "explicit http type",
			toolConfig: map[string]any{
				"type": "http",
				"url":  "https://example.com",
			},
			wantHasMCP: true,
			wantType:   "http",
		},
		{
			name: "explicit local type (normalized to stdio)",
			toolConfig: map[string]any{
				"type":    "local",
				"command": "test",
			},
			wantHasMCP: true,
			wantType:   "stdio",
		},
		{
			name: "inferred http from url field",
			toolConfig: map[string]any{
				"url": "https://api.example.com",
			},
			wantHasMCP: true,
			wantType:   "http",
		},
		{
			name: "inferred stdio from command field",
			toolConfig: map[string]any{
				"command": "docker",
			},
			wantHasMCP: true,
			wantType:   "stdio",
		},
		{
			name: "inferred stdio from container field",
			toolConfig: map[string]any{
				"container": "test/image:latest",
			},
			wantHasMCP: true,
			wantType:   "stdio",
		},
		{
			name: "not MCP - only allowed field",
			toolConfig: map[string]any{
				"allowed": []string{"tool1", "tool2"},
			},
			wantHasMCP: false,
			wantType:   "",
		},
		{
			name: "not MCP - unknown type",
			toolConfig: map[string]any{
				"type": "websocket",
			},
			wantHasMCP: false,
			wantType:   "",
		},
		{
			name: "not MCP - type is not string",
			toolConfig: map[string]any{
				"type": 123,
			},
			wantHasMCP: false,
			wantType:   "",
		},
		{
			name:       "empty config",
			toolConfig: map[string]any{},
			wantHasMCP: false,
			wantType:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasMCP, mcpType := hasMCPConfig(tt.toolConfig)

			if hasMCP != tt.wantHasMCP {
				t.Errorf("hasMCPConfig() hasMCP = %v, want %v", hasMCP, tt.wantHasMCP)
			}

			if mcpType != tt.wantType {
				t.Errorf("hasMCPConfig() mcpType = %q, want %q", mcpType, tt.wantType)
			}
		})
	}
}

// TestRenderSerenaMCPConfigLocalMode tests Serena in local mode with HTTP transport
func TestRenderSerenaMCPConfigLocalMode(t *testing.T) {
	tests := []struct {
		name                 string
		serenaTool           any
		isLast               bool
		includeCopilotFields bool
		inlineArgs           bool
		expectedContent      []string
		unexpectedContent    []string
	}{
		{
			name: "Serena local mode - Copilot format",
			serenaTool: map[string]any{
				"mode": "local",
				"languages": map[string]any{
					"go": map[string]any{},
				},
			},
			isLast:               false,
			includeCopilotFields: true,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"type": "http"`,
				`"url": "http://localhost:$GH_AW_SERENA_PORT"`,
			},
			unexpectedContent: []string{
				`"container"`,
				`"entrypoint"`,
				`"Authorization"`,
			},
		},
		{
			name: "Serena local mode - Claude format",
			serenaTool: map[string]any{
				"mode": "local",
				"languages": map[string]any{
					"typescript": map[string]any{},
				},
			},
			isLast:               true,
			includeCopilotFields: false,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"url": "http://localhost:$GH_AW_SERENA_PORT"`,
			},
			unexpectedContent: []string{
				`"container"`,
				`"type"`,
				`"Authorization"`,
			},
		},
		{
			name: "Serena docker mode - should use container",
			serenaTool: map[string]any{
				"mode": "docker",
				"languages": map[string]any{
					"go": map[string]any{},
				},
			},
			isLast:               false,
			includeCopilotFields: true,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
			},
			unexpectedContent: []string{
				`"url"`,
				`"http"`,
			},
		},
		{
			name: "Serena no mode specified - defaults to docker",
			serenaTool: map[string]any{
				"languages": map[string]any{
					"python": map[string]any{},
				},
			},
			isLast:               false,
			includeCopilotFields: true,
			inlineArgs:           false,
			expectedContent: []string{
				`"serena": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/serena-mcp-server:latest"`,
			},
			unexpectedContent: []string{
				`"url"`,
				`"http"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderSerenaMCPConfigWithOptions(&output, tt.serenaTool, tt.isLast, tt.includeCopilotFields, tt.inlineArgs)

			result := output.String()

			// Check expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected output to contain %q\nGot:\n%s", expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Expected output NOT to contain %q\nGot:\n%s", unexpected, result)
				}
			}
		})
	}
}
