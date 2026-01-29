//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestAddMCPFetchServerIfNeeded(t *testing.T) {
	tests := []struct {
		name             string
		tools            map[string]any
		engineSupports   bool
		expectMCPServer  bool // expect web-fetch to be an MCP server (with container key)
		expectNativeTool bool // expect web-fetch to be a native tool (nil or simple config)
	}{
		{
			name: "web-fetch requested, engine supports it",
			tools: map[string]any{
				"web-fetch": nil,
			},
			engineSupports:   true,
			expectMCPServer:  false,
			expectNativeTool: true,
		},
		{
			name: "web-fetch requested, engine does not support it",
			tools: map[string]any{
				"web-fetch": nil,
			},
			engineSupports:   false,
			expectMCPServer:  true,
			expectNativeTool: false,
		},
		{
			name: "web-fetch not requested",
			tools: map[string]any{
				"bash": nil,
			},
			engineSupports:   false,
			expectMCPServer:  false,
			expectNativeTool: false,
		},
		{
			name:             "empty tools",
			tools:            map[string]any{},
			engineSupports:   false,
			expectMCPServer:  false,
			expectNativeTool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the registry to get an actual engine
			registry := GetGlobalEngineRegistry()
			var engine CodingAgentEngine
			if tt.engineSupports {
				engine, _ = registry.GetEngine("claude") // Claude supports web-fetch
			} else {
				engine, _ = registry.GetEngine("codex") // Codex doesn't support web-fetch
			}

			updatedTools, addedServers := AddMCPFetchServerIfNeeded(tt.tools, engine)

			// Check if web-fetch entry exists and determine if it's MCP server or native tool
			webFetchEntry, hasWebFetch := updatedTools["web-fetch"]

			if tt.expectMCPServer {
				if !hasWebFetch {
					t.Errorf("Expected web-fetch MCP server to be present, but it wasn't")
				} else {
					// Check if it's an MCP server config (has "container" key)
					if configMap, ok := webFetchEntry.(map[string]any); ok {
						if _, hasContainer := configMap["container"]; !hasContainer {
							t.Errorf("Expected web-fetch to be an MCP server (with container key), but it wasn't")
						}
					} else {
						t.Errorf("Expected web-fetch to be a map config, got %T", webFetchEntry)
					}
				}
			}

			if tt.expectNativeTool {
				if !hasWebFetch {
					t.Errorf("Expected web-fetch native tool to be present, but it wasn't")
				} else {
					// Native tool should be nil or not have MCP config
					if configMap, ok := webFetchEntry.(map[string]any); ok {
						if _, hasContainer := configMap["container"]; hasContainer {
							t.Errorf("Expected web-fetch to be a native tool, but it has MCP server config")
						}
					}
				}
			}

			if !tt.expectMCPServer && !tt.expectNativeTool {
				if hasWebFetch {
					t.Errorf("Expected no web-fetch entry, but found one")
				}
			}

			// Check the returned list of added servers
			if tt.expectMCPServer {
				if len(addedServers) != 1 || addedServers[0] != "web-fetch" {
					t.Errorf("Expected addedServers to contain 'web-fetch', got %v", addedServers)
				}
			} else {
				if len(addedServers) != 0 {
					t.Errorf("Expected no added servers, got %v", addedServers)
				}
			}
		})
	}
}

func TestRenderMCPFetchServerConfig(t *testing.T) {
	tests := []struct {
		name         string
		format       string
		indent       string
		isLast       bool
		includeTools bool
		expectSubstr []string
	}{
		{
			name:         "JSON format, not last, without tools",
			format:       "json",
			indent:       "    ",
			isLast:       false,
			includeTools: false,
			expectSubstr: []string{
				`"web-fetch": {`,
				`"command": "docker"`,
				`"mcp/fetch"`,
				`},`,
			},
		},
		{
			name:         "JSON format, last, without tools",
			format:       "json",
			indent:       "    ",
			isLast:       true,
			includeTools: false,
			expectSubstr: []string{
				`"web-fetch": {`,
				`"command": "docker"`,
				`"mcp/fetch"`,
				`}`, // No comma
			},
		},
		{
			name:         "JSON format, not last, with tools",
			format:       "json",
			indent:       "    ",
			isLast:       false,
			includeTools: true,
			expectSubstr: []string{
				`"web-fetch": {`,
				`"command": "docker"`,
				`"mcp/fetch"`,
				`},`,
			},
		},
		{
			name:         "JSON format, last, with tools",
			format:       "json",
			indent:       "    ",
			isLast:       true,
			includeTools: true,
			expectSubstr: []string{
				`"web-fetch": {`,
				`"command": "docker"`,
				`"mcp/fetch"`,
				`}`, // No comma
			},
		},
		{
			name:         "TOML format",
			format:       "toml",
			indent:       "  ",
			isLast:       false,
			includeTools: false,
			expectSubstr: []string{
				`[mcp_servers."web-fetch"]`,
				`command = "docker"`,
				`"mcp/fetch"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			renderMCPFetchServerConfig(&yaml, tt.format, tt.indent, tt.isLast, tt.includeTools)
			output := yaml.String()

			for _, substr := range tt.expectSubstr {
				if !strings.Contains(output, substr) {
					t.Errorf("Expected output to contain %q, but it didn't.\nFull output:\n%s", substr, output)
				}
			}
		})
	}
}

func TestEngineSupportsWebFetch(t *testing.T) {
	registry := GetGlobalEngineRegistry()

	tests := []struct {
		engineID       string
		expectsSupport bool
	}{
		{"claude", true},
		{"codex", false},
		{"copilot", true}, // Copilot now supports web-fetch
		{"custom", false},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get engine %s: %v", tt.engineID, err)
			}

			actualSupport := engine.SupportsWebFetch()
			if actualSupport != tt.expectsSupport {
				t.Errorf("Expected engine %s to have SupportsWebFetch()=%v, got %v",
					tt.engineID, tt.expectsSupport, actualSupport)
			}
		})
	}
}

func TestEngineSupportsWebSearch(t *testing.T) {
	registry := GetGlobalEngineRegistry()

	tests := []struct {
		engineID       string
		expectsSupport bool
	}{
		{"claude", true},
		{"codex", true},
		{"copilot", false},
		{"custom", false},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get engine %s: %v", tt.engineID, err)
			}

			actualSupport := engine.SupportsWebSearch()
			if actualSupport != tt.expectsSupport {
				t.Errorf("Expected engine %s to have SupportsWebSearch()=%v, got %v",
					tt.engineID, tt.expectsSupport, actualSupport)
			}
		})
	}
}
