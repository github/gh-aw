//go:build !integration

package workflow

import (
	"reflect"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

// TestEnsureLocalhostDomainsWorkflow tests the parser.ensureLocalhostDomains function integration
func TestEnsureLocalhostDomainsWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "Empty input should add all localhost domains with ports",
			input:    []string{},
			expected: []string{"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*"},
		},
		{
			name:     "Custom domains without localhost should add localhost domains with ports",
			input:    []string{"github.com", "*.github.com"},
			expected: []string{"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*", "github.com", "*.github.com"},
		},
		{
			name:     "Input with localhost but no 127.0.0.1 should add missing domains",
			input:    []string{"localhost", "example.com"},
			expected: []string{"localhost:*", "127.0.0.1", "127.0.0.1:*", "localhost", "example.com"},
		},
		{
			name:     "Input with 127.0.0.1 but no localhost should add missing domains",
			input:    []string{"127.0.0.1", "example.com"},
			expected: []string{"localhost", "localhost:*", "127.0.0.1:*", "127.0.0.1", "example.com"},
		},
		{
			name:     "Input with both localhost domains should add port variants",
			input:    []string{"localhost", "127.0.0.1", "example.com"},
			expected: []string{"localhost:*", "127.0.0.1:*", "localhost", "127.0.0.1", "example.com"},
		},
		{
			name:     "Input with all localhost variants should remain unchanged",
			input:    []string{"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*", "example.com"},
			expected: []string{"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*", "example.com"},
		},
		{
			name:     "Input with some localhost variants should add missing ones",
			input:    []string{"localhost:*", "127.0.0.1", "example.com"},
			expected: []string{"localhost", "127.0.0.1:*", "localhost:*", "127.0.0.1", "example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.EnsureLocalhostDomains(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parser.EnsureLocalhostDomains(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCustomEngine(t *testing.T) {
	engine := NewCustomEngine()

	// Test basic engine properties
	if engine.GetID() != "custom" {
		t.Errorf("Expected ID 'custom', got '%s'", engine.GetID())
	}

	if engine.GetDisplayName() != "Custom Steps" {
		t.Errorf("Expected display name 'Custom Steps', got '%s'", engine.GetDisplayName())
	}

	if engine.GetDescription() != "Executes user-defined GitHub Actions steps" {
		t.Errorf("Expected description 'Executes user-defined GitHub Actions steps', got '%s'", engine.GetDescription())
	}

	if engine.IsExperimental() {
		t.Error("Expected custom engine to not be experimental")
	}

	if engine.SupportsToolsAllowlist() {
		t.Error("Expected custom engine to not support tools allowlist")
	}

	if engine.SupportsHTTPTransport() {
		t.Error("Expected custom engine to not support HTTP transport")
	}

	if !engine.SupportsMaxTurns() {
		t.Error("Expected custom engine to support max turns for consistency with other engines")
	}
}

func TestCustomEngineGetInstallationSteps(t *testing.T) {
	engine := NewCustomEngine()

	steps := engine.GetInstallationSteps(&WorkflowData{})
	if len(steps) != 0 {
		t.Errorf("Expected 0 installation steps for custom engine, got %d", len(steps))
	}
}

func TestCustomEngineGetExecutionSteps(t *testing.T) {
	engine := NewCustomEngine()

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// Custom engine without steps should return just the log step
	if len(steps) != 1 {
		t.Errorf("Expected 1 step (log step) when no engine config provided, got %d", len(steps))
	}
}

func TestCustomEngineGetExecutionStepsWithIdAndContinueOnError(t *testing.T) {
	engine := NewCustomEngine()

	// Create engine config with steps that include id and continue-on-error fields
	engineConfig := &EngineConfig{
		ID: "custom",
		Steps: []map[string]any{
			{
				"name":              "Setup with ID",
				"id":                "setup-step",
				"continue-on-error": true,
				"uses":              "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
				"with": map[string]any{
					"node-version": "18",
				},
			},
			{
				"name":              "Run command with continue-on-error string",
				"id":                "run-step",
				"continue-on-error": "false",
				"run":               "npm test",
			},
		},
	}

	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: engineConfig,
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// Test with engine config - steps should be populated (2 custom steps + 1 log step)
	if len(steps) != 3 {
		t.Errorf("Expected 3 steps when engine config has 2 steps (2 custom + 1 log), got %d", len(steps))
	}

	// Check the first step content includes id and continue-on-error
	if len(steps) > 0 {
		firstStepContent := strings.Join([]string(steps[0]), "\n")
		if !strings.Contains(firstStepContent, "id: setup-step") {
			t.Errorf("Expected first step to contain 'id: setup-step', got:\n%s", firstStepContent)
		}
		if !strings.Contains(firstStepContent, "continue-on-error: true") {
			t.Errorf("Expected first step to contain 'continue-on-error: true', got:\n%s", firstStepContent)
		}
		if !strings.Contains(firstStepContent, "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt") {
			t.Errorf("Expected first step to contain 'GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt', got:\n%s", firstStepContent)
		}
		if !strings.Contains(firstStepContent, "GH_AW_MCP_CONFIG: /tmp/gh-aw/mcp-config/mcp-servers.json") {
			t.Errorf("Expected first step to contain 'GH_AW_MCP_CONFIG: /tmp/gh-aw/mcp-config/mcp-servers.json', got:\n%s", firstStepContent)
		}
	}

	// Check the second step content
	if len(steps) > 1 {
		secondStepContent := strings.Join([]string(steps[1]), "\n")
		if !strings.Contains(secondStepContent, "id: run-step") {
			t.Errorf("Expected second step to contain 'id: run-step', got:\n%s", secondStepContent)
		}
		if !strings.Contains(secondStepContent, "continue-on-error: \"false\"") {
			t.Errorf("Expected second step to contain 'continue-on-error: \"false\"', got:\n%s", secondStepContent)
		}
		if !strings.Contains(secondStepContent, "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt") {
			t.Errorf("Expected second step to contain 'GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt', got:\n%s", secondStepContent)
		}
		if !strings.Contains(secondStepContent, "GH_AW_MCP_CONFIG: /tmp/gh-aw/mcp-config/mcp-servers.json") {
			t.Errorf("Expected second step to contain 'GH_AW_MCP_CONFIG: /tmp/gh-aw/mcp-config/mcp-servers.json', got:\n%s", secondStepContent)
		}
	}
}

func TestCustomEngineGetExecutionStepsWithSteps(t *testing.T) {
	engine := NewCustomEngine()

	// Create engine config with steps
	engineConfig := &EngineConfig{
		ID: "custom",
		Steps: []map[string]any{
			{
				"name": "Setup Node.js",
				"uses": "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
				"with": map[string]any{
					"node-version": "18",
				},
			},
			{
				"name": "Run tests",
				"run":  "npm test",
			},
		},
	}

	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: engineConfig,
	}

	config := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// Test with engine config - steps should be populated (2 custom steps + 1 log step)
	if len(config) != 3 {
		t.Errorf("Expected 3 steps when engine config has 2 steps (2 custom + 1 log), got %d", len(config))
	}

	// Check the first step content
	if len(config) > 0 {
		firstStepContent := strings.Join([]string(config[0]), "\n")
		if !strings.Contains(firstStepContent, "name: Setup Node.js") {
			t.Errorf("Expected first step to contain 'name: Setup Node.js', got:\n%s", firstStepContent)
		}
		if !strings.Contains(firstStepContent, "uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f") {
			t.Errorf("Expected first step to contain 'uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f', got:\n%s", firstStepContent)
		}
	}

	// Check the second step content includes GH_AW_PROMPT
	if len(config) > 1 {
		secondStepContent := strings.Join([]string(config[1]), "\n")
		if !strings.Contains(secondStepContent, "name: Run tests") {
			t.Errorf("Expected second step to contain 'name: Run tests', got:\n%s", secondStepContent)
		}
		if !strings.Contains(secondStepContent, "run:") && !strings.Contains(secondStepContent, "npm test") {
			t.Errorf("Expected second step to contain run command 'npm test', got:\n%s", secondStepContent)
		}
		if !strings.Contains(secondStepContent, "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt") {
			t.Errorf("Expected second step to contain 'GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt', got:\n%s", secondStepContent)
		}
		if !strings.Contains(secondStepContent, "GH_AW_MCP_CONFIG: /tmp/gh-aw/mcp-config/mcp-servers.json") {
			t.Errorf("Expected second step to contain 'GH_AW_MCP_CONFIG: /tmp/gh-aw/mcp-config/mcp-servers.json', got:\n%s", secondStepContent)
		}
	}
}

func TestCustomEngineRenderMCPConfig(t *testing.T) {
	engine := NewCustomEngine()
	var yaml strings.Builder

	// This should generate MCP configuration structure like Claude
	engine.RenderMCPConfig(&yaml, map[string]any{}, []string{}, nil)

	output := yaml.String()
	expectedPrefix := "          cat << MCPCONFIG_EOF | bash /opt/gh-aw/actions/start_mcp_gateway.sh"
	if !strings.Contains(output, expectedPrefix) {
		t.Errorf("Expected MCP config to contain setup prefix, got '%s'", output)
	}

	if !strings.Contains(output, "\"mcpServers\"") {
		t.Errorf("Expected MCP config to contain mcpServers section, got '%s'", output)
	}
}

func TestCustomEngineRenderPlaywrightMCPConfigWithDomainConfiguration(t *testing.T) {
	engine := NewCustomEngine()
	var yaml strings.Builder

	// Test with Playwright domain configuration
	networkPerms := &NetworkPermissions{
		Allowed: []string{"external.example.com"}, // This should be ignored for Playwright
	}

	workflowData := &WorkflowData{
		NetworkPermissions: networkPerms,
	}

	tools := map[string]any{
		"playwright": map[string]any{
			"version":         "v1.40.0",
			"allowed_domains": []string{"example.com", "*.github.com"},
		},
	}

	mcpTools := []string{"playwright"}

	engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData)
	output := yaml.String()

	// Check that the output contains Playwright configuration
	if !strings.Contains(output, `"playwright": {`) {
		t.Errorf("Expected Playwright configuration in output")
	}

	// Check that it contains the official Playwright MCP Docker image
	if !strings.Contains(output, "mcr.microsoft.com/playwright/mcp") {
		t.Errorf("Expected official Playwright MCP Docker image in output")
	}

	// Check that it contains --allowed-hosts and --allowed-origins flags when domains are configured
	if !strings.Contains(output, "--allowed-hosts") {
		t.Errorf("Expected --allowed-hosts flag in docker args")
	}
	if !strings.Contains(output, "--allowed-origins") {
		t.Errorf("Expected --allowed-origins flag in docker args")
	}

	// Check that it contains the specified domains AND localhost domains with port variations
	// Domains should be sorted alphabetically: *.github.com, example.com
	// Both flags should have the same domain list
	expectedDomains := "localhost;localhost:*;127.0.0.1;127.0.0.1:*;*.github.com;example.com"
	if !strings.Contains(output, expectedDomains) {
		t.Errorf("Expected configured domains with localhost and port variations in --allowed-hosts and --allowed-origins values (sorted)\nActual output:\n%s", output)
	}

	// Check that it does NOT contain the old format environment variables
	if strings.Contains(output, "PLAYWRIGHT_ALLOWED_DOMAINS") {
		t.Errorf("Expected new simplified format without environment variables")
	}
}

func TestCustomEngineRenderPlaywrightMCPConfigDefaultDomains(t *testing.T) {
	engine := NewCustomEngine()
	var yaml strings.Builder

	// Test with no Playwright domain configuration - should default to localhost
	networkPerms := &NetworkPermissions{
		Allowed: []string{"external.example.com"}, // This should be ignored for Playwright
	}

	workflowData := &WorkflowData{
		NetworkPermissions: networkPerms,
	}

	tools := map[string]any{
		"playwright": map[string]any{
			"version": "v1.40.0",
			// No allowed_domains specified - should default to localhost
		},
	}

	mcpTools := []string{"playwright"}

	engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData)
	output := yaml.String()

	// Check that the output contains Playwright configuration
	if !strings.Contains(output, `"playwright": {`) {
		t.Errorf("Expected Playwright configuration in output")
	}

	// Check that it contains the official Playwright MCP Docker image
	if !strings.Contains(output, "mcr.microsoft.com/playwright/mcp") {
		t.Errorf("Expected official Playwright MCP Docker image in output")
	}

	// Check that it contains --allowed-hosts and --allowed-origins flags for default domains
	if !strings.Contains(output, "--allowed-hosts") {
		t.Errorf("Expected --allowed-hosts flag in docker args")
	}
	if !strings.Contains(output, "--allowed-origins") {
		t.Errorf("Expected --allowed-origins flag in docker args")
	}

	// Check that it contains default domains with port variations (localhost, localhost:*, 127.0.0.1, 127.0.0.1:*)
	// Both flags should have the same domain list
	expectedDomains := "localhost;localhost:*;127.0.0.1;127.0.0.1:*"
	if !strings.Contains(output, expectedDomains) {
		t.Errorf("Expected default domains with port variations in --allowed-hosts and --allowed-origins values")
	}

	// Check that it does NOT contain the old format environment variables
	if strings.Contains(output, "PLAYWRIGHT_ALLOWED_DOMAINS") {
		t.Errorf("Expected new simplified format without environment variables")
	}
}

func TestCustomEngineParseLogMetrics(t *testing.T) {
	engine := NewCustomEngine()

	logContent := `This is a test log
Error: Something went wrong
Warning: This is a warning
Another line
ERROR: Another error`

	metrics := engine.ParseLogMetrics(logContent, false)

	// Error patterns have been removed
	if metrics.TokenUsage != 0 {
		t.Errorf("Expected 0 token usage, got %d", metrics.TokenUsage)
	}

	if metrics.EstimatedCost != 0 {
		t.Errorf("Expected 0 estimated cost, got %f", metrics.EstimatedCost)
	}
}

func TestCustomEngineGetLogParserScript(t *testing.T) {
	engine := NewCustomEngine()

	script := engine.GetLogParserScriptId()
	if script != "parse_custom_log" {
		t.Errorf("Expected log parser script 'parse_custom_log', got '%s'", script)
	}
}

func TestCustomEngineConvertStepToYAMLWithSection(t *testing.T) {
	engine := NewCustomEngine()

	// Test step with 'with' section to ensure keys are sorted
	stepMap := map[string]any{
		"name": "Test step with sorted with section",
		"uses": "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd",
		"with": map[string]any{
			"zebra": "value-z",
			"alpha": "value-a",
			"beta":  "value-b",
			"gamma": "value-g",
		},
	}

	yaml, err := engine.convertStepToYAML(stepMap)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify that the with keys are in alphabetical order
	lines := strings.Split(yaml, "\n")
	withSection := false
	withKeyOrder := []string{}

	for _, line := range lines {
		if strings.TrimSpace(line) == "with:" {
			withSection = true
			continue
		}
		if withSection && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			// End of with section if we hit another top-level key
			break
		}
		if withSection && strings.Contains(line, ":") {
			// Extract the key (before the colon)
			parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
			if len(parts) > 0 {
				withKeyOrder = append(withKeyOrder, strings.TrimSpace(parts[0]))
			}
		}
	}

	expectedOrder := []string{"alpha", "beta", "gamma", "zebra"}
	if len(withKeyOrder) != len(expectedOrder) {
		t.Errorf("Expected %d with keys, got %d", len(expectedOrder), len(withKeyOrder))
	}

	for i, key := range expectedOrder {
		if i >= len(withKeyOrder) || withKeyOrder[i] != key {
			t.Errorf("Expected with key at position %d to be '%s', got '%s'. Full order: %v", i, key, withKeyOrder[i], withKeyOrder)
		}
	}
}
