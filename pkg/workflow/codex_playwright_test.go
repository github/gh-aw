//go:build !integration

package workflow

import "testing"

func TestCodexEnginePlaywrightUsesCLI(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name  string
		input map[string]any
	}{
		{
			name:  "playwright null does not create MCP config",
			input: map[string]any{"playwright": nil},
		},
		{
			name: "playwright CLI config does not create MCP config",
			input: map[string]any{
				"playwright": map[string]any{
					"mode":    "cli",
					"version": "0.1.18",
				},
			},
		},
		{
			name: "legacy MCP config does not create MCP config",
			input: map[string]any{
				"playwright": map[string]any{"mode": "mcp"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.expandNeutralToolsToCodexToolsFromMap(tt.input)
			if playwrightRaw, hasPlaywright := result["playwright"]; hasPlaywright {
				t.Errorf("expected playwright to be absent from MCP config, got: %v", playwrightRaw)
			}
		})
	}
}

func TestCodexEnginePlaywrightPreservesCustomMCPServer(t *testing.T) {
	engine := NewCodexEngine()

	input := map[string]any{
		"playwright": map[string]any{
			"command": "npx",
			"args":    []any{"--yes", "@playwright/mcp@0.0.79", "--no-sandbox"},
			"allowed": []any{"browser_navigate", "browser_snapshot"},
		},
	}

	result := engine.expandNeutralToolsToCodexToolsFromMap(input)
	playwrightRaw, hasPlaywright := result["playwright"]
	if !hasPlaywright {
		t.Fatal("expected custom mcp-servers.playwright entry to be preserved for MCP rendering")
	}
	playwrightMap, ok := playwrightRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected playwright entry to remain a map, got %T", playwrightRaw)
	}
	if command, _ := playwrightMap["command"].(string); command != "npx" {
		t.Errorf("expected custom command to be preserved, got: %v", playwrightMap["command"])
	}
}
