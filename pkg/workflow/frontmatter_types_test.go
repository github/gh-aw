//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestUnmarshalFromMap(t *testing.T) {
	t.Run("extracts simple string field", func(t *testing.T) {
		data := map[string]any{
			"name": "test-workflow",
		}

		var result string

		err := unmarshalFromMap(data, "name", &result)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if result != "test-workflow" {
			t.Errorf("got %q, want %q", result, "test-workflow")
		}
	})

	t.Run("extracts nested map", func(t *testing.T) {
		data := map[string]any{
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
					"timeout": 300,
				},
			},
		}

		var result map[string]any
		err := unmarshalFromMap(data, "tools", &result)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if result == nil {
			t.Fatal("result is nil")
		}

		bash, ok := result["bash"].(map[string]any)
		if !ok {
			t.Fatal("bash tool not found or wrong type")
		}

		if bash["enabled"] != true {
			t.Errorf("bash.enabled = %v, want true", bash["enabled"])
		}
	})

	t.Run("returns error for missing key", func(t *testing.T) {
		data := map[string]any{
			"name": "test",
		}

		var result string

		err := unmarshalFromMap(data, "missing", &result)
		if err == nil {
			t.Error("expected error for missing key, got nil")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("handles numeric types", func(t *testing.T) {
		data := map[string]any{
			"timeout": 42,
			"count":   int64(100),
			"ratio":   3.14,
		}

		var timeout int
		if err := unmarshalFromMap(data, "timeout", &timeout); err != nil {
			t.Errorf("timeout unmarshal error: %v", err)
		}
		if timeout != 42 {
			t.Errorf("timeout = %d, want 42", timeout)
		}

		var count int64
		if err := unmarshalFromMap(data, "count", &count); err != nil {
			t.Errorf("count unmarshal error: %v", err)
		}
		if count != 100 {
			t.Errorf("count = %d, want 100", count)
		}

		var ratio float64
		if err := unmarshalFromMap(data, "ratio", &ratio); err != nil {
			t.Errorf("ratio unmarshal error: %v", err)
		}
		if ratio != 3.14 {
			t.Errorf("ratio = %f, want 3.14", ratio)
		}
	})

	t.Run("handles arrays", func(t *testing.T) {
		data := map[string]any{
			"items": []any{"a", "b", "c"},
		}

		var items []string
		err := unmarshalFromMap(data, "items", &items)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(items) != 3 {
			t.Errorf("got %d items, want 3", len(items))
		}

		if items[0] != "a" || items[1] != "b" || items[2] != "c" {
			t.Errorf("got %v, want [a b c]", items)
		}
	})
}

func TestParseFrontmatterConfig(t *testing.T) {
	t.Run("parses minimal workflow config", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "claude",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Name != "test-workflow" {
			t.Errorf("Name = %q, want %q", config.Name, "test-workflow")
		}

		if config.Engine != "claude" {
			t.Errorf("Engine = %q, want %q", config.Engine, "claude")
		}
	})

	t.Run("parses labels array", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "copilot",
			"labels": []any{"automation", "ci", "diagnostics"},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.Labels) != 3 {
			t.Fatalf("expected 3 labels, got %d", len(config.Labels))
		}

		expectedLabels := []string{"automation", "ci", "diagnostics"}
		for i, expected := range expectedLabels {
			if config.Labels[i] != expected {
				t.Errorf("Labels[%d] = %q, want %q", i, config.Labels[i], expected)
			}
		}
	})

	t.Run("parses complete workflow config", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":        "full-workflow",
			"description": "A complete workflow",
			"engine":      "copilot",
			"source":      "owner/repo/path@main",
			"tracker-id":  "test-tracker-123",
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
				},
			},
			"mcp-servers": map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"enabled": true,
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check core fields
		if config.Name != "full-workflow" {
			t.Errorf("Name = %q, want %q", config.Name, "full-workflow")
		}

		if config.Description != "A complete workflow" {
			t.Errorf("Description = %q, want %q", config.Description, "A complete workflow")
		}

		if config.Engine != "copilot" {
			t.Errorf("Engine = %q, want %q", config.Engine, "copilot")
		}

		if config.Source != "owner/repo/path@main" {
			t.Errorf("Source = %q, want %q", config.Source, "owner/repo/path@main")
		}

		if config.TrackerID != "test-tracker-123" {
			t.Errorf("TrackerID = %q, want %q", config.TrackerID, "test-tracker-123")
		}

		// Check nested configuration sections
		if config.Tools == nil {
			t.Error("Tools should not be nil")
		}

		if config.MCPServers == nil {
			t.Error("MCPServers should not be nil")
		}

		if config.SafeOutputs == nil {
			t.Error("SafeOutputs should not be nil")
		}
	})

	t.Run("handles timeout-minutes as int", func(t *testing.T) {
		frontmatter := map[string]any{
			"timeout-minutes": 60,
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.TimeoutMinutes != 60 {
			t.Errorf("TimeoutMinutes = %d, want 60", config.TimeoutMinutes)
		}
	})

	t.Run("handles empty frontmatter", func(t *testing.T) {
		frontmatter := map[string]any{}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Name != "" {
			t.Errorf("Name should be empty, got %q", config.Name)
		}

		if config.Tools != nil {
			t.Errorf("Tools should be nil for empty frontmatter, got %v", config.Tools)
		}
	})

	t.Run("handles network configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"allowed": []any{"github.com", "api.github.com"},
				"firewall": map[string]any{
					"enabled": true,
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Network == nil {
			t.Fatal("Network should not be nil")
		}

		// Check that allowed domains are present
		if len(config.Network.Allowed) != 2 {
			t.Errorf("expected 2 allowed domains, got %d", len(config.Network.Allowed))
		}
	})

	t.Run("handles sandbox configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"sandbox": map[string]any{
				"agent": map[string]any{
					"type": "awf",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Sandbox == nil {
			t.Fatal("Sandbox should not be nil")
		}
	})

	t.Run("handles jobs configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"jobs": map[string]any{
				"test-job": map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{
							"run": "echo hello",
						},
					},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Jobs == nil {
			t.Fatal("Jobs should not be nil")
		}

		if _, ok := config.Jobs["test-job"]; !ok {
			t.Error("test-job should exist in Jobs")
		}
	})

	t.Run("preserves complex nested structures", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"jobs": map[string]any{
					"custom-job": map[string]any{
						"conditions": []any{
							map[string]any{
								"field": "status",
								"value": "success",
							},
						},
					},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.SafeOutputs == nil {
			t.Fatal("SafeOutputs should not be nil")
		}

		if config.SafeOutputs.Jobs == nil {
			t.Fatal("SafeOutputs.Jobs should not be nil")
		}

		customJob, ok := config.SafeOutputs.Jobs["custom-job"]
		if !ok {
			t.Fatal("custom-job should exist in SafeOutputs.Jobs")
		}

		if customJob == nil {
			t.Fatal("custom-job should not be nil")
		}
	})
}

func TestFrontmatterConfigFieldExtraction(t *testing.T) {
	t.Run("extracts tools using struct", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
				},
				"playwright": map[string]any{
					"version": "v1.41.0",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify tools can be accessed via ToMap()
		if config.Tools == nil {
			t.Fatal("Tools should not be nil")
		}

		toolsMap := config.Tools.ToMap()
		if len(toolsMap) < 2 {
			t.Errorf("expected at least 2 tools, got %d", len(toolsMap))
		}

		if _, ok := toolsMap["bash"]; !ok {
			t.Error("bash tool should exist")
		}

		if _, ok := toolsMap["playwright"]; !ok {
			t.Error("playwright tool should exist")
		}
	})

	t.Run("extracts mcp-servers using struct", func(t *testing.T) {
		frontmatter := map[string]any{
			"mcp-servers": map[string]any{
				"github": map[string]any{
					"mode":     "remote",
					"toolsets": []any{"repos", "issues"},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.MCPServers) != 1 {
			t.Errorf("expected 1 mcp server, got %d", len(config.MCPServers))
		}

		if _, ok := config.MCPServers["github"]; !ok {
			t.Error("github mcp server should exist")
		}
	})

	t.Run("extracts runtimes using struct", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.Runtimes) != 1 {
			t.Errorf("expected 1 runtime, got %d", len(config.Runtimes))
		}

		if _, ok := config.Runtimes["node"]; !ok {
			t.Error("node runtime should exist")
		}
	})
}

func TestFrontmatterConfigBackwardCompatibility(t *testing.T) {
	// This test ensures that the new structured types work with existing
	// frontmatter extraction patterns used throughout the codebase

	t.Run("compatible with extractMapFromFrontmatter pattern", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
				},
			},
		}

		// Old pattern: extractMapFromFrontmatter
		oldResult := extractMapFromFrontmatter(frontmatter, "tools")

		// New pattern: Parse then access field
		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Convert tools back to map for comparison
		newResult := config.Tools.ToMap()

		// Both should have the same tool
		if _, oldOk := oldResult["bash"]; !oldOk {
			t.Error("old pattern missing bash tool")
		}
		if _, newOk := newResult["bash"]; !newOk {
			t.Error("new pattern missing bash tool")
		}
	})

	t.Run("supports round-trip conversion", func(t *testing.T) {
		originalFrontmatter := map[string]any{
			"name":            "test-workflow",
			"engine":          "copilot",
			"description":     "A test workflow",
			"tracker-id":      "test-tracker-12345678",
			"timeout-minutes": 30,
			"on": map[string]any{
				"issues": map[string]any{
					"types": []any{"opened", "labeled"},
				},
			},
			"env": map[string]string{
				"MY_VAR": "value",
			},
		}

		// Parse to struct
		config, err := ParseFrontmatterConfig(originalFrontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Verify key fields are preserved
		if reconstructed["name"] != "test-workflow" {
			t.Errorf("name mismatch: got %v", reconstructed["name"])
		}
		if reconstructed["engine"] != "copilot" {
			t.Errorf("engine mismatch: got %v", reconstructed["engine"])
		}
		if reconstructed["description"] != "A test workflow" {
			t.Errorf("description mismatch: got %v", reconstructed["description"])
		}
		if reconstructed["tracker-id"] != "test-tracker-12345678" {
			t.Errorf("tracker-id mismatch: got %v", reconstructed["tracker-id"])
		}
		if reconstructed["timeout-minutes"] != 30 {
			t.Errorf("timeout-minutes mismatch: got %v", reconstructed["timeout-minutes"])
		}

		// Verify nested structures
		if reconstructed["on"] == nil {
			t.Error("on should not be nil")
		}
		if reconstructed["env"] == nil {
			t.Error("env should not be nil")
		}
	})

	t.Run("round-trip preserves labels", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "copilot",
			"labels": []any{"automation", "ci", "security"},
		}

		// Parse to struct
		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Verify labels are preserved
		labels, ok := reconstructed["labels"].([]string)
		if !ok {
			t.Fatalf("labels should be []string, got %T", reconstructed["labels"])
		}

		if len(labels) != 3 {
			t.Fatalf("expected 3 labels, got %d", len(labels))
		}

		expectedLabels := []string{"automation", "ci", "security"}
		for i, expected := range expectedLabels {
			if labels[i] != expected {
				t.Errorf("labels[%d] = %q, want %q", i, labels[i], expected)
			}
		}
	})

	t.Run("preserves strongly-typed fields", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"allowed": []any{"github.com", "api.github.com"},
			},
			"sandbox": map[string]any{
				"agent": map[string]any{
					"type": "awf",
				},
			},
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"max": 5,
				},
			},
			"safe-inputs": map[string]any{
				"mode": "http",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Verify strongly-typed fields are populated
		if config.Network == nil {
			t.Error("Network should be strongly typed")
		}
		if config.Sandbox == nil {
			t.Error("Sandbox should be strongly typed")
		}
		if config.SafeOutputs == nil {
			t.Error("SafeOutputs should be strongly typed")
		}
		if config.SafeInputs == nil {
			t.Error("SafeInputs should be strongly typed")
		}

		// Convert back and verify they're preserved
		reconstructed := config.ToMap()
		if reconstructed["network"] == nil {
			t.Error("network should be preserved in ToMap")
		}
		if reconstructed["sandbox"] == nil {
			t.Error("sandbox should be preserved in ToMap")
		}
		if reconstructed["safe-outputs"] == nil {
			t.Error("safe-outputs should be preserved in ToMap")
		}
		if reconstructed["safe-inputs"] == nil {
			t.Error("safe-inputs should be preserved in ToMap")
		}
	})
}

// TestFrontmatterConfigTypeSafety demonstrates compile-time type safety benefits
func TestFrontmatterConfigTypeSafety(t *testing.T) {
	t.Run("strongly-typed Tools field provides compile-time safety", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"mode":     "remote",
					"toolsets": []any{"repos", "issues"},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Strong typing allows direct access without type assertions
		if config.Tools != nil {
			// Tools is *ToolsConfig, not map[string]any
			// This provides compile-time validation
			toolsMap := config.Tools.ToMap()
			if _, ok := toolsMap["github"]; !ok {
				t.Error("github tool should exist")
			}
		} else {
			t.Error("Tools should not be nil")
		}
	})

	t.Run("strongly-typed Network field eliminates type assertions", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"allowed": []any{"github.com", "api.github.com"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Old pattern required: networkMap, ok := config["network"].(map[string]any)
		// New pattern: direct access to strongly-typed field
		if config.Network == nil {
			t.Fatal("Network should not be nil")
		}

		// Access allowed domains directly without type assertion
		if len(config.Network.Allowed) != 2 {
			t.Errorf("expected 2 allowed domains, got %d", len(config.Network.Allowed))
		}

		if config.Network.Allowed[0] != "github.com" {
			t.Errorf("first domain should be github.com, got %s", config.Network.Allowed[0])
		}
	})

	t.Run("strongly-typed SafeOutputs eliminates nested type assertions", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"staged": true,
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Strong typing provides direct access to nested config
		if config.SafeOutputs == nil {
			t.Fatal("SafeOutputs should not be nil")
		}

		// Access staged flag directly
		if !config.SafeOutputs.Staged {
			t.Error("Staged should be true")
		}
	})

	t.Run("Env field is type-safe map[string]string", func(t *testing.T) {
		frontmatter := map[string]any{
			"env": map[string]any{
				"VAR1": "value1",
				"VAR2": "value2",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Env is map[string]string, not map[string]any
		if len(config.Env) != 2 {
			t.Errorf("expected 2 env vars, got %d", len(config.Env))
		}

		// Direct string access without type assertion
		if config.Env["VAR1"] != "value1" {
			t.Errorf("VAR1 should be 'value1', got %s", config.Env["VAR1"])
		}
	})
}

// TestFrontmatterConfigEdgeCases tests edge cases and error handling
func TestFrontmatterConfigEdgeCases(t *testing.T) {
	t.Run("handles nil values gracefully", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":  "test",
			"tools": nil,
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.Name != "test" {
			t.Errorf("name should be 'test', got %s", config.Name)
		}

		// Nil tools should result in nil Tools field
		if config.Tools != nil {
			t.Error("Tools should be nil when frontmatter has nil tools")
		}
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "minimal-workflow",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// All optional fields should be nil/empty
		if config.Tools != nil {
			t.Error("Tools should be nil for minimal config")
		}
		if config.Network != nil {
			t.Error("Network should be nil for minimal config")
		}
		if config.SafeOutputs != nil {
			t.Error("SafeOutputs should be nil for minimal config")
		}
	})

	t.Run("handles Strict pointer correctly", func(t *testing.T) {
		// Test with explicit false
		frontmatter1 := map[string]any{
			"strict": false,
		}
		config1, err := ParseFrontmatterConfig(frontmatter1)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if config1.Strict == nil {
			t.Error("Strict should not be nil when explicitly set to false")
		}
		if *config1.Strict != false {
			t.Error("Strict should be false")
		}

		// Test with explicit true
		frontmatter2 := map[string]any{
			"strict": true,
		}
		config2, err := ParseFrontmatterConfig(frontmatter2)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if config2.Strict == nil {
			t.Error("Strict should not be nil when explicitly set to true")
		}
		if *config2.Strict != true {
			t.Error("Strict should be true")
		}

		// Test with missing field (should be nil)
		frontmatter3 := map[string]any{
			"name": "test",
		}
		config3, err := ParseFrontmatterConfig(frontmatter3)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if config3.Strict != nil {
			t.Error("Strict should be nil when not specified")
		}
	})
}

// TestFrontmatterConfigIntegration tests integration with existing extraction functions
func TestFrontmatterConfigIntegration(t *testing.T) {
	t.Run("integrates with ExtractMapField helper", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
			},
		}

		// Old pattern still works for backward compatibility
		tools := ExtractMapField(frontmatter, "tools")
		if len(tools) == 0 {
			t.Error("ExtractMapField should return tools")
		}

		// New pattern with strong typing
		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.Tools == nil {
			t.Error("Tools should be parsed")
		}
		if config.Runtimes == nil {
			t.Error("Runtimes should be parsed")
		}
	})

	t.Run("ToMap produces compatible output for legacy code", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":        "test-workflow",
			"description": "A test",
			"engine":      "copilot",
			"network": map[string]any{
				"allowed": []any{"github.com"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Legacy code can use this map
		if name, ok := reconstructed["name"].(string); !ok || name != "test-workflow" {
			t.Errorf("name should be reconstructed correctly")
		}

		if network := reconstructed["network"]; network == nil {
			t.Error("network should be reconstructed")
		}
	})
}

// TestRuntimesConfigTyping tests the new typed RuntimesConfig field
func TestRuntimesConfigTyping(t *testing.T) {
	t.Run("parses node runtime with string version", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Check that RuntimesTyped is populated
		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		if config.RuntimesTyped.Node == nil {
			t.Fatal("RuntimesTyped.Node should not be nil")
		}

		if config.RuntimesTyped.Node.Version != "20" {
			t.Errorf("Node version = %s, want 20", config.RuntimesTyped.Node.Version)
		}

		// Legacy field should still be populated
		if config.Runtimes == nil {
			t.Error("Legacy Runtimes field should still be populated")
		}
	})

	t.Run("parses python runtime with numeric version", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"python": map[string]any{
					"version": 3.11,
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		if config.RuntimesTyped.Python == nil {
			t.Fatal("RuntimesTyped.Python should not be nil")
		}

		if config.RuntimesTyped.Python.Version != "3.11" {
			t.Errorf("Python version = %s, want 3.11", config.RuntimesTyped.Python.Version)
		}
	})

	t.Run("parses multiple runtimes", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
				"python": map[string]any{
					"version": "3.11",
				},
				"go": map[string]any{
					"version": "1.21",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		if config.RuntimesTyped.Node == nil {
			t.Error("Node runtime should be set")
		}
		if config.RuntimesTyped.Python == nil {
			t.Error("Python runtime should be set")
		}
		if config.RuntimesTyped.Go == nil {
			t.Error("Go runtime should be set")
		}
	})

	t.Run("converts RuntimesTyped back to map", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Check that runtimes is preserved
		runtimes, ok := reconstructed["runtimes"].(map[string]any)
		if !ok {
			t.Fatal("runtimes should be in reconstructed map")
		}

		node, ok := runtimes["node"].(map[string]any)
		if !ok {
			t.Fatal("node runtime should be in runtimes map")
		}

		if node["version"] != "20" {
			t.Errorf("node version = %v, want 20", node["version"])
		}
	})

	t.Run("handles all runtime types", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node":   map[string]any{"version": "20"},
				"python": map[string]any{"version": "3.11"},
				"go":     map[string]any{"version": "1.21"},
				"uv":     map[string]any{"version": "0.1.0"},
				"bun":    map[string]any{"version": "1.0.0"},
				"deno":   map[string]any{"version": "1.40"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		// Check all runtimes are parsed
		if config.RuntimesTyped.Node == nil {
			t.Error("Node should be set")
		}
		if config.RuntimesTyped.Python == nil {
			t.Error("Python should be set")
		}
		if config.RuntimesTyped.Go == nil {
			t.Error("Go should be set")
		}
		if config.RuntimesTyped.UV == nil {
			t.Error("UV should be set")
		}
		if config.RuntimesTyped.Bun == nil {
			t.Error("Bun should be set")
		}
		if config.RuntimesTyped.Deno == nil {
			t.Error("Deno should be set")
		}
	})
}

// TestPermissionsConfigTyping tests the new typed PermissionsConfig field
func TestPermissionsConfigTyping(t *testing.T) {
	t.Run("parses shorthand read-all permission", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"read-all": "read-all",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Check that PermissionsTyped is populated
		if config.PermissionsTyped == nil {
			t.Fatal("PermissionsTyped should not be nil")
		}

		if config.PermissionsTyped.Shorthand != "read-all" {
			t.Errorf("Shorthand = %s, want read-all", config.PermissionsTyped.Shorthand)
		}

		// Legacy field should still be populated
		if config.Permissions == nil {
			t.Error("Legacy Permissions field should still be populated")
		}
	})

	t.Run("parses detailed permissions", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"contents":      "read",
				"issues":        "write",
				"pull-requests": "write",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.PermissionsTyped == nil {
			t.Fatal("PermissionsTyped should not be nil")
		}

		if config.PermissionsTyped.Contents != "read" {
			t.Errorf("Contents = %s, want read", config.PermissionsTyped.Contents)
		}
		if config.PermissionsTyped.Issues != "write" {
			t.Errorf("Issues = %s, want write", config.PermissionsTyped.Issues)
		}
		if config.PermissionsTyped.PullRequests != "write" {
			t.Errorf("PullRequests = %s, want write", config.PermissionsTyped.PullRequests)
		}
	})

	t.Run("parses all permission scopes", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"actions":               "read",
				"checks":                "write",
				"contents":              "write",
				"deployments":           "read",
				"id-token":              "write",
				"issues":                "write",
				"discussions":           "write",
				"packages":              "read",
				"pages":                 "write",
				"pull-requests":         "write",
				"repository-projects":   "write",
				"security-events":       "read",
				"statuses":              "write",
				"organization-projects": "read",
				"organization-packages": "read",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.PermissionsTyped == nil {
			t.Fatal("PermissionsTyped should not be nil")
		}

		// Check all scopes are parsed
		if config.PermissionsTyped.Actions != "read" {
			t.Error("Actions should be read")
		}
		if config.PermissionsTyped.Checks != "write" {
			t.Error("Checks should be write")
		}
		if config.PermissionsTyped.Contents != "write" {
			t.Error("Contents should be write")
		}
		if config.PermissionsTyped.Deployments != "read" {
			t.Error("Deployments should be read")
		}
		if config.PermissionsTyped.IDToken != "write" {
			t.Error("IDToken should be write")
		}
	})

	t.Run("converts PermissionsTyped back to map", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"permissions": map[string]any{
				"contents": "read",
				"issues":   "write",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Check that permissions is preserved
		permissions, ok := reconstructed["permissions"].(map[string]any)
		if !ok {
			t.Fatal("permissions should be in reconstructed map")
		}

		if permissions["contents"] != "read" {
			t.Errorf("contents = %v, want read", permissions["contents"])
		}
		if permissions["issues"] != "write" {
			t.Errorf("issues = %v, want write", permissions["issues"])
		}
	})

	t.Run("converts shorthand permissions back to map", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"write-all": "write-all",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Check that shorthand is preserved
		permissions, ok := reconstructed["permissions"].(map[string]any)
		if !ok {
			t.Fatal("permissions should be in reconstructed map")
		}

		if permissions["write-all"] != "write-all" {
			t.Errorf("shorthand permission not preserved correctly: %v", permissions)
		}
	})
}

// TestTypedConfigsBackwardCompatibility tests backward compatibility of typed configs
func TestTypedConfigsBackwardCompatibility(t *testing.T) {
	t.Run("legacy Runtimes field still accessible", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Both typed and legacy fields should be accessible
		if config.RuntimesTyped == nil {
			t.Error("RuntimesTyped should be set")
		}
		if config.Runtimes == nil {
			t.Error("Legacy Runtimes should still be set")
		}

		// Legacy field should have the same data
		if node, ok := config.Runtimes["node"].(map[string]any); ok {
			if node["version"] != "20" {
				t.Error("Legacy Runtimes field should have version 20")
			}
		} else {
			t.Error("Legacy Runtimes should have node entry")
		}
	})

	t.Run("legacy Permissions field still accessible", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"contents": "read",
				"issues":   "write",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Both typed and legacy fields should be accessible
		if config.PermissionsTyped == nil {
			t.Error("PermissionsTyped should be set")
		}
		if config.Permissions == nil {
			t.Error("Legacy Permissions should still be set")
		}

		// Legacy field should have the same data
		if contents, ok := config.Permissions["contents"].(string); ok {
			if contents != "read" {
				t.Error("Legacy Permissions should have contents: read")
			}
		} else {
			t.Error("Legacy Permissions should have contents entry")
		}
	})

	t.Run("ToMap prefers typed fields over legacy", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "20",
				},
			},
			"permissions": map[string]any{
				"contents": "read",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert to map
		reconstructed := config.ToMap()

		// Check that runtimes and permissions are present
		if reconstructed["runtimes"] == nil {
			t.Error("runtimes should be in reconstructed map")
		}
		if reconstructed["permissions"] == nil {
			t.Error("permissions should be in reconstructed map")
		}
	})
}
