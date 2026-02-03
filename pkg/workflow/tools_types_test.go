//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/types"
)

func TestNewTools(t *testing.T) {
	t.Run("creates empty tools from nil map", func(t *testing.T) {
		tools := NewTools(nil)
		if tools == nil {
			t.Fatal("expected non-nil tools")
		}
		if tools.Custom == nil {
			t.Error("expected non-nil Custom map")
		}
		if len(tools.GetToolNames()) != 0 {
			t.Errorf("expected 0 tools, got %d", len(tools.GetToolNames()))
		}
	})

	t.Run("creates empty tools from empty map", func(t *testing.T) {
		tools := NewTools(map[string]any{})
		if tools == nil {
			t.Fatal("expected non-nil tools")
		}
		if len(tools.GetToolNames()) != 0 {
			t.Errorf("expected 0 tools, got %d", len(tools.GetToolNames()))
		}
	})

	t.Run("parses known tools", func(t *testing.T) {
		toolsMap := map[string]any{
			"github":    map[string]any{"allowed": []any{"issue_read"}},
			"bash":      []any{"echo", "ls"},
			"edit":      nil,
			"web-fetch": nil,
		}

		tools := NewTools(toolsMap)
		if tools == nil {
			t.Fatal("expected non-nil tools")
		}

		if !tools.HasTool("github") {
			t.Error("expected GitHub tool to be set")
		}
		if !tools.HasTool("bash") {
			t.Error("expected Bash tool to be set")
		}
		if !tools.HasTool("edit") {
			t.Error("expected Edit tool to be set")
		}
		if !tools.HasTool("web-fetch") {
			t.Error("expected WebFetch tool to be set")
		}

		names := tools.GetToolNames()
		if len(names) != 4 {
			t.Errorf("expected 4 tools, got %d: %v", len(names), names)
		}
	})

	t.Run("parses custom MCP tools", func(t *testing.T) {
		toolsMap := map[string]any{
			"github":      nil,
			"my-custom":   map[string]any{"command": "node", "args": []any{"server.js"}},
			"another-mcp": map[string]any{"type": "http", "url": "http://localhost:8080"},
		}

		tools := NewTools(toolsMap)
		if tools == nil {
			t.Fatal("expected non-nil tools")
		}

		if len(tools.Custom) != 2 {
			t.Errorf("expected 2 custom tools, got %d", len(tools.Custom))
		}

		myCustom, exists := tools.Custom["my-custom"]
		if !exists {
			t.Error("expected my-custom tool in Custom map")
		} else {
			if myCustom.Command != "node" {
				t.Errorf("expected my-custom command to be 'node', got %q", myCustom.Command)
			}
		}

		anotherMCP, exists := tools.Custom["another-mcp"]
		if !exists {
			t.Error("expected another-mcp tool in Custom map")
		} else {
			if anotherMCP.URL != "http://localhost:8080" {
				t.Errorf("expected another-mcp URL to be 'http://localhost:8080', got %q", anotherMCP.URL)
			}
		}

		names := tools.GetToolNames()
		if len(names) != 3 {
			t.Errorf("expected 3 tools, got %d: %v", len(names), names)
		}
	})
}

func TestHasTool(t *testing.T) {
	toolsMap := map[string]any{
		"github":    nil,
		"bash":      []any{"echo"},
		"my-custom": map[string]any{"command": "node"},
	}

	tools := NewTools(toolsMap)

	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"github exists", "github", true},
		{"bash exists", "bash", true},
		{"custom exists", "my-custom", true},
		{"edit doesn't exist", "edit", false},
		{"web-fetch doesn't exist", "web-fetch", false},
		{"unknown doesn't exist", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tools.HasTool(tt.toolName)
			if result != tt.expected {
				t.Errorf("HasTool(%q) = %v, want %v", tt.toolName, result, tt.expected)
			}
		})
	}

	t.Run("nil tools returns false", func(t *testing.T) {
		var tools *Tools
		if tools.HasTool("github") {
			t.Error("expected false for nil tools")
		}
	})
}

func TestGetToolNames(t *testing.T) {
	t.Run("empty tools returns empty list", func(t *testing.T) {
		tools := NewTools(nil)
		names := tools.GetToolNames()
		if len(names) != 0 {
			t.Errorf("expected 0 names, got %d", len(names))
		}
	})

	t.Run("returns all tool names", func(t *testing.T) {
		toolsMap := map[string]any{
			"github":    nil,
			"bash":      []any{"echo"},
			"edit":      nil,
			"my-custom": map[string]any{},
		}

		tools := NewTools(toolsMap)
		names := tools.GetToolNames()

		if len(names) != 4 {
			t.Errorf("expected 4 names, got %d", len(names))
		}

		// Check that all expected names are present
		expectedNames := map[string]bool{
			"github":    false,
			"bash":      false,
			"edit":      false,
			"my-custom": false,
		}

		for _, name := range names {
			if _, ok := expectedNames[name]; ok {
				expectedNames[name] = true
			}
		}

		for name, found := range expectedNames {
			if !found {
				t.Errorf("expected to find tool %q in names list", name)
			}
		}
	})

	t.Run("nil tools returns empty list", func(t *testing.T) {
		var tools *Tools
		names := tools.GetToolNames()
		if len(names) != 0 {
			t.Errorf("expected 0 names, got %d", len(names))
		}
	})
}

func TestGitHubConfigParsing(t *testing.T) {
	t.Run("returns nil when github not set", func(t *testing.T) {
		tools := NewTools(map[string]any{})
		if tools.GitHub != nil {
			t.Error("expected nil GitHub config when github not set")
		}
	})

	t.Run("parses github config map", func(t *testing.T) {
		toolsMap := map[string]any{
			"github": map[string]any{
				"allowed":      []any{"issue_read", "create_issue"},
				"mode":         "remote",
				"version":      "v1.0.0",
				"args":         []any{"--verbose"},
				"read-only":    true,
				"github-token": "${{ secrets.MY_TOKEN }}",
				"toolset":      []any{"repos", "issues"},
			},
		}

		tools := NewTools(toolsMap)
		config := tools.GitHub

		if config == nil {
			t.Fatal("expected non-nil config")
		}

		if len(config.Allowed) != 2 {
			t.Errorf("expected 2 allowed tools, got %d", len(config.Allowed))
		}
		if config.Allowed[0] != "issue_read" {
			t.Errorf("expected first allowed tool to be 'issue_read', got %q", config.Allowed[0])
		}

		if config.Mode != "remote" {
			t.Errorf("expected mode 'remote', got %q", config.Mode)
		}

		if config.Version != "v1.0.0" {
			t.Errorf("expected version 'v1.0.0', got %q", config.Version)
		}

		if len(config.Args) != 1 {
			t.Errorf("expected 1 arg, got %d", len(config.Args))
		}

		if !config.ReadOnly {
			t.Error("expected ReadOnly to be true")
		}

		if config.GitHubToken != "${{ secrets.MY_TOKEN }}" {
			t.Errorf("expected github-token to be '${{ secrets.MY_TOKEN }}', got %q", config.GitHubToken)
		}

		if len(config.Toolset) != 2 {
			t.Errorf("expected 2 toolsets, got %d", len(config.Toolset))
		}
	})
}

func TestPlaywrightConfigParsing(t *testing.T) {
	t.Run("returns nil when playwright not set", func(t *testing.T) {
		tools := NewTools(map[string]any{})
		if tools.Playwright != nil {
			t.Error("expected nil Playwright config when playwright not set")
		}
	})

	t.Run("parses playwright config map", func(t *testing.T) {
		toolsMap := map[string]any{
			"playwright": map[string]any{
				"version":         "v1.41.0",
				"allowed_domains": []any{"github.com", "example.com"},
				"args":            []any{"--headless"},
			},
		}

		tools := NewTools(toolsMap)
		config := tools.Playwright

		if config == nil {
			t.Fatal("expected non-nil config")
		}

		if config.Version != "v1.41.0" {
			t.Errorf("expected version 'v1.41.0', got %q", config.Version)
		}

		if len(config.AllowedDomains) != 2 {
			t.Errorf("expected 2 allowed domains, got %d", len(config.AllowedDomains))
		}

		if len(config.Args) != 1 {
			t.Errorf("expected 1 arg, got %d", len(config.Args))
		}
	})
}

func TestExtractMapFromFrontmatter(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  map[string]any
		key          string
		expectedLen  int
		expectedKeys []string
	}{
		{
			name: "extracts existing map",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"github": nil,
					"bash":   []any{"echo"},
				},
			},
			key:          "tools",
			expectedLen:  2,
			expectedKeys: []string{"github", "bash"},
		},
		{
			name: "returns empty map when key doesn't exist",
			frontmatter: map[string]any{
				"other": "value",
			},
			key:          "tools",
			expectedLen:  0,
			expectedKeys: []string{},
		},
		{
			name: "returns empty map when value is not a map",
			frontmatter: map[string]any{
				"tools": "not-a-map",
			},
			key:          "tools",
			expectedLen:  0,
			expectedKeys: []string{},
		},
		{
			name: "returns empty map when value is nil",
			frontmatter: map[string]any{
				"tools": nil,
			},
			key:          "tools",
			expectedLen:  0,
			expectedKeys: []string{},
		},
		{
			name: "returns empty map when value is array",
			frontmatter: map[string]any{
				"tools": []string{"github", "bash"},
			},
			key:          "tools",
			expectedLen:  0,
			expectedKeys: []string{},
		},
		{
			name:         "handles nil frontmatter",
			frontmatter:  nil,
			key:          "tools",
			expectedLen:  0,
			expectedKeys: []string{},
		},
		{
			name:         "handles empty frontmatter",
			frontmatter:  map[string]any{},
			key:          "tools",
			expectedLen:  0,
			expectedKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMapFromFrontmatter(tt.frontmatter, tt.key)

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if len(result) != tt.expectedLen {
				t.Errorf("expected map with %d entries, got %d", tt.expectedLen, len(result))
			}

			for _, key := range tt.expectedKeys {
				if _, ok := result[key]; !ok {
					t.Errorf("expected key %q to exist in result", key)
				}
			}
		})
	}
}

func TestExtractToolsFromFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"tools": map[string]any{
			"github": nil,
			"bash":   []any{"echo"},
		},
		"mcp-servers": map[string]any{
			"my-server": map[string]any{"command": "node"},
		},
	}

	result := extractToolsFromFrontmatter(frontmatter)

	if len(result) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result))
	}

	if _, ok := result["github"]; !ok {
		t.Error("expected 'github' key in result")
	}

	if _, ok := result["bash"]; !ok {
		t.Error("expected 'bash' key in result")
	}

	// Should not include mcp-servers
	if _, ok := result["my-server"]; ok {
		t.Error("unexpected 'my-server' key in result")
	}
}

func TestExtractMCPServersFromFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"tools": map[string]any{
			"github": nil,
		},
		"mcp-servers": map[string]any{
			"my-server":      map[string]any{"command": "node"},
			"another-server": map[string]any{"command": "python"},
		},
	}

	result := extractMCPServersFromFrontmatter(frontmatter)

	if len(result) != 2 {
		t.Errorf("expected 2 MCP servers, got %d", len(result))
	}

	if _, ok := result["my-server"]; !ok {
		t.Error("expected 'my-server' key in result")
	}

	if _, ok := result["another-server"]; !ok {
		t.Error("expected 'another-server' key in result")
	}

	// Should not include tools
	if _, ok := result["github"]; ok {
		t.Error("unexpected 'github' key in result")
	}
}

func TestExtractRuntimesFromFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"tools": map[string]any{
			"github": nil,
		},
		"runtimes": map[string]any{
			"node":   map[string]any{"version": "18"},
			"python": map[string]any{"version": "3.11"},
		},
	}

	result := extractRuntimesFromFrontmatter(frontmatter)

	if len(result) != 2 {
		t.Errorf("expected 2 runtimes, got %d", len(result))
	}

	if _, ok := result["node"]; !ok {
		t.Error("expected 'node' key in result")
	}

	if _, ok := result["python"]; !ok {
		t.Error("expected 'python' key in result")
	}

	// Should not include tools
	if _, ok := result["github"]; ok {
		t.Error("unexpected 'github' key in result")
	}
}

func TestParseToolsConfig(t *testing.T) {
	t.Run("parses valid tools map", func(t *testing.T) {
		toolsMap := map[string]any{
			"github":    map[string]any{"allowed": []any{"issue_read"}},
			"bash":      []any{"echo", "ls"},
			"edit":      nil,
			"my-custom": map[string]any{"command": "node"},
		}

		config, err := ParseToolsConfig(toolsMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config == nil {
			t.Fatal("expected non-nil config")
		}

		if !config.HasTool("github") {
			t.Error("expected GitHub tool to be set")
		}
		if !config.HasTool("bash") {
			t.Error("expected Bash tool to be set")
		}
		if !config.HasTool("edit") {
			t.Error("expected Edit tool to be set")
		}
		if !config.HasTool("my-custom") {
			t.Error("expected my-custom tool to be set")
		}
	})

	t.Run("handles nil map", func(t *testing.T) {
		config, err := ParseToolsConfig(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config == nil {
			t.Fatal("expected non-nil config")
		}

		if len(config.GetToolNames()) != 0 {
			t.Errorf("expected 0 tools, got %d", len(config.GetToolNames()))
		}
	})
}

func TestToolsConfigToMap(t *testing.T) {
	t.Run("converts ToolsConfig back to map", func(t *testing.T) {
		toolsMap := map[string]any{
			"github": map[string]any{
				"allowed": []any{"issue_read", "create_issue"},
				"mode":    "remote",
			},
			"bash":      []any{"echo"},
			"edit":      nil,
			"my-custom": map[string]any{"command": "node"},
		}

		config, _ := ParseToolsConfig(toolsMap)
		result := config.ToMap()

		if result == nil {
			t.Fatal("expected non-nil result")
		}

		// GitHub should be present
		if _, ok := result["github"]; !ok {
			t.Error("expected 'github' key in result")
		}

		// Bash should be present
		if _, ok := result["bash"]; !ok {
			t.Error("expected 'bash' key in result")
		}

		// Edit should be present
		if _, ok := result["edit"]; !ok {
			t.Error("expected 'edit' key in result")
		}

		// Custom tool should be present
		if _, ok := result["my-custom"]; !ok {
			t.Error("expected 'my-custom' key in result")
		}
	})

	t.Run("handles nil ToolsConfig", func(t *testing.T) {
		var config *ToolsConfig
		result := config.ToMap()

		if result == nil {
			t.Fatal("expected non-nil result")
		}

		if len(result) != 0 {
			t.Errorf("expected empty map, got %d entries", len(result))
		}
	})

	t.Run("ToMap preserves raw map when available", func(t *testing.T) {
		// Create a ToolsConfig with a raw map
		toolsMap := map[string]any{
			"github": map[string]any{"allowed": []any{"issue_read"}},
		}

		config := NewTools(toolsMap)
		result := config.ToMap()

		// Should return the raw map, which is identical to the input
		if len(result) != len(toolsMap) {
			t.Errorf("expected %d entries, got %d", len(toolsMap), len(result))
		}
	})
}

func TestParseMCPServerConfig(t *testing.T) {
	t.Run("parses stdio MCP server config", func(t *testing.T) {
		configMap := map[string]any{
			"command": "node",
			"args":    []any{"server.js", "--port", "3000"},
			"env": map[string]any{
				"NODE_ENV": "production",
			},
			"mode":    "stdio",
			"version": "1.0.0",
		}

		config := parseMCPServerConfig(configMap)

		if config.Command != "node" {
			t.Errorf("expected command 'node', got %q", config.Command)
		}

		if len(config.Args) != 3 {
			t.Errorf("expected 3 args, got %d", len(config.Args))
		}

		if config.Args[0] != "server.js" {
			t.Errorf("expected first arg 'server.js', got %q", config.Args[0])
		}

		if config.Env["NODE_ENV"] != "production" {
			t.Errorf("expected NODE_ENV 'production', got %q", config.Env["NODE_ENV"])
		}

		if config.Mode != "stdio" {
			t.Errorf("expected mode 'stdio', got %q", config.Mode)
		}

		if config.Version != "1.0.0" {
			t.Errorf("expected version '1.0.0', got %q", config.Version)
		}
	})

	t.Run("parses HTTP MCP server config", func(t *testing.T) {
		configMap := map[string]any{
			"type": "http",
			"url":  "http://localhost:8080",
			"headers": map[string]any{
				"Authorization": "Bearer token123",
			},
			"toolsets": []any{"repos", "issues"},
		}

		config := parseMCPServerConfig(configMap)

		if config.Type != "http" {
			t.Errorf("expected type 'http', got %q", config.Type)
		}

		if config.URL != "http://localhost:8080" {
			t.Errorf("expected URL 'http://localhost:8080', got %q", config.URL)
		}

		if config.Headers["Authorization"] != "Bearer token123" {
			t.Errorf("expected Authorization header, got %q", config.Headers["Authorization"])
		}

		if len(config.Toolsets) != 2 {
			t.Errorf("expected 2 toolsets, got %d", len(config.Toolsets))
		}
	})

	t.Run("parses container MCP server config", func(t *testing.T) {
		configMap := map[string]any{
			"container":      "ghcr.io/example/mcp-server:latest",
			"entrypointArgs": []any{"--config", "/etc/config.json"},
		}

		config := parseMCPServerConfig(configMap)

		if config.Container != "ghcr.io/example/mcp-server:latest" {
			t.Errorf("expected container image, got %q", config.Container)
		}

		if len(config.EntrypointArgs) != 2 {
			t.Errorf("expected 2 entrypoint args, got %d", len(config.EntrypointArgs))
		}
	})

	t.Run("preserves custom fields", func(t *testing.T) {
		configMap := map[string]any{
			"command":      "node",
			"customField1": "value1",
			"customField2": 42,
		}

		config := parseMCPServerConfig(configMap)

		if config.Command != "node" {
			t.Errorf("expected command 'node', got %q", config.Command)
		}

		if config.CustomFields["customField1"] != "value1" {
			t.Errorf("expected customField1 'value1', got %v", config.CustomFields["customField1"])
		}

		if config.CustomFields["customField2"] != 42 {
			t.Errorf("expected customField2 42, got %v", config.CustomFields["customField2"])
		}
	})

	t.Run("handles nil config", func(t *testing.T) {
		config := parseMCPServerConfig(nil)

		if config.Command != "" {
			t.Errorf("expected empty command, got %q", config.Command)
		}

		if len(config.CustomFields) != 0 {
			t.Errorf("expected empty CustomFields, got %d entries", len(config.CustomFields))
		}
	})

	t.Run("handles numeric version", func(t *testing.T) {
		configMap := map[string]any{
			"version": 2.0,
		}

		config := parseMCPServerConfig(configMap)

		if config.Version != "2" {
			t.Errorf("expected version '2', got %q", config.Version)
		}
	})
}

func TestMCPServerConfigToMap(t *testing.T) {
	t.Run("converts MCPServerConfig to map", func(t *testing.T) {
		config := MCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Command: "node",
				Args:    []string{"server.js"},
				Env: map[string]string{
					"NODE_ENV": "production",
				},
				Version: "1.0.0",
			},
			Mode:     "stdio",
			Toolsets: []string{"default"},
		}

		result := mcpServerConfigToMap(config)

		if result["command"] != "node" {
			t.Errorf("expected command 'node', got %v", result["command"])
		}

		args, ok := result["args"].([]string)
		if !ok {
			t.Error("expected args to be []string")
		} else if len(args) != 1 {
			t.Errorf("expected 1 arg, got %d", len(args))
		}

		if result["mode"] != "stdio" {
			t.Errorf("expected mode 'stdio', got %v", result["mode"])
		}
	})

	t.Run("includes HTTP fields when set", func(t *testing.T) {
		config := MCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type: "http",
				URL:  "http://localhost:8080",
				Headers: map[string]string{
					"Authorization": "Bearer token",
				},
			},
		}

		result := mcpServerConfigToMap(config)

		if result["type"] != "http" {
			t.Errorf("expected type 'http', got %v", result["type"])
		}

		if result["url"] != "http://localhost:8080" {
			t.Errorf("expected URL, got %v", result["url"])
		}

		headers, ok := result["headers"].(map[string]string)
		if !ok || len(headers) != 1 {
			t.Error("expected headers map")
		}
	})

	t.Run("includes custom fields", func(t *testing.T) {
		config := MCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Command: "node",
			},
			CustomFields: map[string]any{
				"customField": "customValue",
			},
		}

		result := mcpServerConfigToMap(config)

		if result["customField"] != "customValue" {
			t.Errorf("expected customField, got %v", result["customField"])
		}
	})
}
