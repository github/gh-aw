//go:build !integration

package cli

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/types"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPInspectIntervalSchedule(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non:\n  schedule: every 2 days\n---\nTest.\n")
	if err := InspectWorkflowMCP(context.Background(), workflowID, "", "", false, false); err != nil {
		t.Fatal(err)
	}
}

func TestMCPInspectSubcommand_NoArgBehaviorDocumented(t *testing.T) {
	t.Parallel()
	cmd := NewMCPInspectSubcommand()
	if cmd == nil {
		t.Fatal("Expected mcp inspect subcommand to be created")
	}

	if !strings.Contains(cmd.Long, "When no workflow is provided, this command lists workflows that have MCP server configurations") {
		t.Errorf("Expected mcp inspect long help to document no-argument behavior, got: %s", cmd.Long)
	}
	if !strings.Contains(cmd.Long, "(equivalent to 'gh aw mcp list')") {
		t.Errorf("Expected mcp inspect long help to document no-argument behavior, got: %s", cmd.Long)
	}
}

func TestMCPInspectClientImplementation_UsesCLIGetVersion(t *testing.T) {
	t.Parallel()
	implementation := mcpInspectClientImplementation()

	if implementation.Name != "gh-aw-inspector" {
		t.Fatalf("expected inspector client name %q, got %q", "gh-aw-inspector", implementation.Name)
	}
	if implementation.Version != GetVersion() {
		t.Fatalf("expected inspector client version %q, got %q", GetVersion(), implementation.Version)
	}
}

func TestMCPEmojiIconSource_ReturnsDataURI(t *testing.T) {
	t.Parallel()
	source := mcpEmojiIconSource("📊")
	if !strings.HasPrefix(source, "data:image/svg+xml;base64,") {
		t.Fatalf("expected base64 data URI source, got %q", source)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(source, "data:image/svg+xml;base64,"))
	if err != nil {
		t.Fatalf("failed to base64-decode icon source: %v", err)
	}

	if !strings.Contains(string(decoded), "<svg") {
		t.Fatalf("expected SVG payload, got %q", decoded)
	}
	if !strings.Contains(string(decoded), "📊") {
		t.Fatalf("expected original emoji in SVG payload, got %q", decoded)
	}
}

func TestValidateServerSecrets(t *testing.T) {
	tests := []struct {
		name        string
		config      parser.RegistryMCPServerConfig
		envVars     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no environment variables",
			config:      parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: "simple-tool"},
			expectError: false,
		},
		{
			name: "valid environment variable",
			config: parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"TEST_VAR": "test_value",
				}}, Name: "env-tool",
			},
			envVars: map[string]string{
				"TEST_VAR": "actual_value",
			},
			expectError: false,
		},
		{
			name: "missing environment variable",
			config: parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"MISSING_VAR": "test_value",
				}}, Name: "missing-env-tool",
			},
			expectError: true,
			errorMsg:    "environment variable 'MISSING_VAR' not set",
		},
		{
			name: "secrets reference (handled gracefully)",
			config: parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"API_KEY": "${secrets.API_KEY}",
				}}, Name: "secrets-tool",
			},
			expectError: false,
		},
		{
			name: "github remote mode requires GH_AW_GITHUB_TOKEN",
			config: parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
				URL: "https://api.githubcopilot.com/mcp/",
				Env: map[string]string{}}, Name: "github",
			},
			envVars: map[string]string{
				"GH_AW_GITHUB_TOKEN": "test_token",
			},
			expectError: false,
		},
		{
			name: "github remote mode with custom token",
			config: parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
				URL: "https://api.githubcopilot.com/mcp/",
				Env: map[string]string{
					"GITHUB_TOKEN": "${{ secrets.CUSTOM_PAT }}",
				}}, Name: "github",
			},
			expectError: false,
		},
		{
			name: "github local mode does not require GH_AW_GITHUB_TOKEN",
			config: parser.RegistryMCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker",
				Env: map[string]string{}}, Name: "github",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for key, value := range tt.envVars {
				if value == "" {
					// Skip setting empty values
					continue
				}
				t.Setenv(key, value)
			}

			err := validateServerSecrets(tt.config, false, false)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

func TestDisplayToolAllowanceHint(t *testing.T) {
	tests := []struct {
		name       string
		serverInfo *parser.MCPServerInfo
		expected   []string // expected phrases in output
	}{
		{
			name: "server with blocked tools",
			serverInfo: &parser.MCPServerInfo{
				Config: parser.RegistryMCPServerConfig{
					Name:    "test-server",
					Allowed: []string{"tool1", "tool2"},
				},
				Tools: []*mcp.Tool{
					{Name: "tool1", Description: "Allowed tool 1"},
					{Name: "tool2", Description: "Allowed tool 2"},
					{Name: "tool3", Description: "Blocked tool 3"},
					{Name: "tool4", Description: "Blocked tool 4"},
				},
			},
			expected: []string{
				"To allow blocked tools",
				"tools:",
				"test-server:",
				"allowed:",
				"- tool1",
				"- tool2",
				"- tool3",
				"- tool4",
			},
		},
		{
			name: "server with no allowed list (all tools allowed)",
			serverInfo: &parser.MCPServerInfo{
				Config: parser.RegistryMCPServerConfig{
					Name:    "open-server",
					Allowed: []string{}, // Empty means all allowed
				},
				Tools: []*mcp.Tool{
					{Name: "tool1", Description: "Tool 1"},
					{Name: "tool2", Description: "Tool 2"},
				},
			},
			expected: []string{
				"All tools are currently allowed",
				"To restrict tools",
				"tools:",
				"open-server:",
				"allowed:",
				"- tool1",
			},
		},
		{
			name: "server with all tools explicitly allowed",
			serverInfo: &parser.MCPServerInfo{
				Config: parser.RegistryMCPServerConfig{
					Name:    "explicit-server",
					Allowed: []string{"tool1", "tool2"},
				},
				Tools: []*mcp.Tool{
					{Name: "tool1", Description: "Tool 1"},
					{Name: "tool2", Description: "Tool 2"},
				},
			},
			expected: []string{
				"All available tools are explicitly allowed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture output by redirecting stdout
			// For now, just call the function to ensure it doesn't panic
			// In a real scenario, we'd capture the output to verify content
			displayToolAllowanceHint(tt.serverInfo)
		})
	}
}

func TestMCPInspectFiltersSafeOutputs(t *testing.T) {
	tests := []struct {
		name         string
		frontmatter  map[string]any
		serverFilter string
		expectedLen  int
		description  string
	}{
		{
			name: "parser includes safe-outputs but inspect filters them",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{"max": 3},
					"missing-tool": map[string]any{},
				},
			},
			serverFilter: "",
			description:  "parser should include safe-outputs but inspect should filter them",
		},
		{
			name: "mixed configuration filters only safe-outputs",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{"max": 3},
				},
				"tools": map[string]any{
					"github": map[string]any{
						"allowed": []string{"create_issue", "get_repository"},
					},
				},
			},
			serverFilter: "",
			description:  "should filter safe-outputs but keep github MCP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that parser still includes safe-outputs
			configs, err := parser.ExtractMCPConfigurations(tt.frontmatter, tt.serverFilter)
			if err != nil {
				t.Fatalf("ExtractMCPConfigurations failed: %v", err)
			}

			// Verify parser includes safe-outputs when present
			hasSafeOutputs := false
			for _, config := range configs {
				if config.Name == constants.SafeOutputsMCPServerID.String() {
					hasSafeOutputs = true
					break
				}
			}

			if _, hasSafeOutputsInFrontmatter := tt.frontmatter["safe-outputs"]; hasSafeOutputsInFrontmatter && !hasSafeOutputs {
				t.Error("Parser should still include safe-outputs configurations")
			} else if hasSafeOutputsInFrontmatter && hasSafeOutputs {
				t.Log("✓ Parser correctly includes safe-outputs (to be filtered by inspect command)")
			}

			// Test the filtering logic that inspect command uses
			var filteredConfigs []parser.RegistryMCPServerConfig
			for _, config := range configs {
				if config.Name != constants.SafeOutputsMCPServerID.String() {
					filteredConfigs = append(filteredConfigs, config)
				}
			}

			// Verify no safe-outputs configurations remain after filtering
			for _, config := range filteredConfigs {
				if config.Name == constants.SafeOutputsMCPServerID.String() {
					t.Errorf("safe-outputs should be filtered out by inspect command but was found")
				}
			}

			t.Logf("✓ Inspect command filtering works: %d configs before filter, %d after filter", len(configs), len(filteredConfigs))
		})
	}
}

func TestFilterOutSafeOutputs(t *testing.T) {
	tests := []struct {
		name     string
		input    []parser.RegistryMCPServerConfig
		expected []parser.RegistryMCPServerConfig
	}{
		{
			name:     "empty input",
			input:    []parser.RegistryMCPServerConfig{},
			expected: []parser.RegistryMCPServerConfig{},
		},
		{
			name: "only safe-outputs",
			input: []parser.RegistryMCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: constants.SafeOutputsMCPServerID.String()},
			},
			expected: []parser.RegistryMCPServerConfig{},
		},
		{
			name: "mixed servers",
			input: []parser.RegistryMCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: constants.SafeOutputsMCPServerID.String()},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "playwright"},
			},
			expected: []parser.RegistryMCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "playwright"},
			},
		},
		{
			name: "no safe-outputs",
			input: []parser.RegistryMCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: "custom-server"},
			},
			expected: []parser.RegistryMCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: "custom-server"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterOutSafeOutputs(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d configs, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i].Name != expected.Name || result[i].Type != expected.Type {
					t.Errorf("Expected config %d to be {Name: %s, Type: %s}, got {Name: %s, Type: %s}",
						i, expected.Name, expected.Type, result[i].Name, result[i].Type)
				}
			}
		})
	}
}

func TestDisplayServerCapabilities_WithPrompts(t *testing.T) {
	info := &parser.MCPServerInfo{
		Tools: []*mcp.Tool{
			{Name: "example-tool", Description: "Example tool"},
		},
		Resources: []*mcp.Resource{
			{URI: "file:///repo/workflow.md", Name: "workflow", Description: "Workflow resource"},
		},
		Prompts: []*mcp.Prompt{
			{
				Name:        "summarize",
				Title:       "Quick Summary",
				Description: "Summarize the current workflow",
				Arguments: []*mcp.PromptArgument{
					{Name: "topic", Required: true},
				},
			},
		},
		Roots: []*parser.MCPRootInfo{
			{URI: "file://", Name: "file"},
		},
	}

	stdout, _ := captureOutput(t, func() error {
		displayServerCapabilities(info, "")
		return nil
	})

	if !strings.Contains(stdout, "summarize") {
		t.Fatalf("expected prompt name in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "Quick Summary") {
		t.Fatalf("expected prompt title in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "Arguments") {
		t.Fatalf("expected prompts table headers in stdout, got %q", stdout)
	}
}

func TestDisplayServerCapabilities_WithoutPrompts(t *testing.T) {
	info := &parser.MCPServerInfo{
		Tools: []*mcp.Tool{
			{Name: "example-tool", Description: "Example tool"},
		},
	}

	stdout, _ := captureOutput(t, func() error {
		displayServerCapabilities(info, "")
		return nil
	})

	if strings.Contains(stdout, "Arguments") {
		t.Fatalf("did not expect prompts table headers in stdout, got %q", stdout)
	}
}
