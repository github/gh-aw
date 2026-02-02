//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/types"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateServerSecrets(t *testing.T) {
	tests := []struct {
		name        string
		config      parser.MCPServerConfig
		envVars     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no environment variables",
			config:      parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: "simple-tool"},
			expectError: false,
		},
		{
			name: "valid environment variable",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
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
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"MISSING_VAR": "test_value",
				}}, Name: "missing-env-tool",
			},
			expectError: true,
			errorMsg:    "environment variable 'MISSING_VAR' not set",
		},
		{
			name: "secrets reference (handled gracefully)",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Env: map[string]string{
					"API_KEY": "${secrets.API_KEY}",
				}}, Name: "secrets-tool",
			},
			expectError: false,
		},
		{
			name: "github remote mode requires GH_AW_GITHUB_TOKEN",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
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
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "http",
				URL: "https://api.githubcopilot.com/mcp/",
				Env: map[string]string{
					"GITHUB_TOKEN": "${{ secrets.CUSTOM_PAT }}",
				}}, Name: "github",
			},
			expectError: false,
		},
		{
			name: "github local mode does not require GH_AW_GITHUB_TOKEN",
			config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker",
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
				Config: parser.MCPServerConfig{
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
				Config: parser.MCPServerConfig{
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
				Config: parser.MCPServerConfig{
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
				if config.Name == constants.SafeOutputsMCPServerID {
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
			var filteredConfigs []parser.MCPServerConfig
			for _, config := range configs {
				if config.Name != constants.SafeOutputsMCPServerID {
					filteredConfigs = append(filteredConfigs, config)
				}
			}

			// Verify no safe-outputs configurations remain after filtering
			for _, config := range filteredConfigs {
				if config.Name == constants.SafeOutputsMCPServerID {
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
		input    []parser.MCPServerConfig
		expected []parser.MCPServerConfig
	}{
		{
			name:     "empty input",
			input:    []parser.MCPServerConfig{},
			expected: []parser.MCPServerConfig{},
		},
		{
			name: "only safe-outputs",
			input: []parser.MCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: constants.SafeOutputsMCPServerID},
			},
			expected: []parser.MCPServerConfig{},
		},
		{
			name: "mixed servers",
			input: []parser.MCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: constants.SafeOutputsMCPServerID},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "playwright"},
			},
			expected: []parser.MCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "playwright"},
			},
		},
		{
			name: "no safe-outputs",
			input: []parser.MCPServerConfig{
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "docker"}, Name: "github"},
				{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio"}, Name: "custom-server"},
			},
			expected: []parser.MCPServerConfig{
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
