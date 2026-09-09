//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveWriteSinkGuardPolicyFromWorkflow tests the helper that derives guard policies from workflow data
func TestDeriveWriteSinkGuardPolicyFromWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expectNil    bool
		description  string
		expectedKey  string
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			expectNil:    true,
			description:  "nil workflowData should return nil",
		},
		{
			name:         "nil tools",
			workflowData: &WorkflowData{},
			expectNil:    true,
			description:  "no tools should return nil",
		},
		{
			name: "no github tool",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"playwright": map[string]any{},
				},
			},
			expectNil:   true,
			description: "no github tool means no guard policy",
		},
		{
			name: "github tool without guard policy (auto-lockdown)",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"toolsets": []string{"default"},
					},
				},
			},
			expectNil:   false,
			expectedKey: "write-sink",
			description: "github tool without repos/min-integrity triggers auto-lockdown which sets accept=[*]",
		},
		{
			name: "github tool with nil value (auto-lockdown)",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": nil,
				},
			},
			expectNil:   false,
			expectedKey: "write-sink",
			description: "github tool with nil value triggers auto-lockdown which sets accept=[*]",
		},
		{
			name: "github tool with repos=all",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"repos":         "all",
						"min-integrity": "none",
					},
				},
			},
			expectNil:   false,
			expectedKey: "write-sink",
			description: "github guard policy with repos=all should produce write-sink policy",
		},
		{
			name: "github tool with specific repo",
			workflowData: &WorkflowData{
				Tools: map[string]any{
					"github": map[string]any{
						"repos":         "myorg/myrepo",
						"min-integrity": "approved",
					},
				},
			},
			expectNil:   false,
			expectedKey: "write-sink",
			description: "github guard policy with specific repo should produce write-sink policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveWriteSinkGuardPolicyFromWorkflow(tt.workflowData)
			if tt.expectNil {
				assert.Nil(t, result, "Expected nil result for: %s", tt.description)
			} else {
				require.NotNil(t, result, "Expected non-nil result for: %s", tt.description)
				assert.Contains(t, result, tt.expectedKey, "Expected write-sink key in policies for: %s", tt.description)
				writeSink, ok := result[tt.expectedKey].(map[string]any)
				require.True(t, ok, "Expected write-sink policy map for: %s", tt.description)
				assert.Equal(t, sinkVisibilityExpr, writeSink["sink-visibility"], "Expected runtime expression for sink-visibility: %s", tt.description)
			}
		})
	}
}

// TestRenderSharedMCPConfigWithGuardPoliciesJSON tests that guard policies are rendered correctly in JSON format
func TestRenderCustomToolWithGuardPoliciesJSON(t *testing.T) {
	guardPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept": []string{"*"},
		},
	}

	toolConfig := map[string]any{
		"url": "https://example.com/mcp",
	}

	var output strings.Builder
	renderer := MCPConfigRenderer{
		IndentLevel:   "                ",
		Format:        "json",
		GuardPolicies: guardPolicies,
	}

	err := renderSharedMCPConfig(&output, "my-tool", toolConfig, renderer)
	require.NoError(t, err, "renderSharedMCPConfig should succeed")

	result := output.String()
	// The url field should have a trailing comma (guard policies follow)
	assert.Contains(t, result, "\"url\": \"https://example.com/mcp\",", "url field should have trailing comma")
	// Guard policies should be rendered
	assert.Contains(t, result, "\"guard-policies\"", "guard-policies should be rendered")
	assert.Contains(t, result, "\"write-sink\"", "write-sink should be rendered")
	assert.Contains(t, result, "\"accept\"", "accept should be rendered")
}

// TestRenderSharedMCPConfigWithGuardPoliciesTOML tests that guard policies are rendered correctly in TOML format
func TestRenderCustomToolWithGuardPoliciesTOML(t *testing.T) {
	guardPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept": []string{"private:myorg/myrepo"},
		},
	}

	toolConfig := map[string]any{
		"url": "https://example.com/mcp",
	}

	var output strings.Builder
	renderer := MCPConfigRenderer{
		IndentLevel:   "          ",
		Format:        "toml",
		GuardPolicies: guardPolicies,
	}

	err := renderSharedMCPConfig(&output, "my-tool", toolConfig, renderer)
	require.NoError(t, err, "renderSharedMCPConfig should succeed")

	result := output.String()
	// TOML guard policies are in separate sections
	assert.Contains(t, result, "[mcp_servers.my-tool.\"guard-policies\"]", "TOML guard-policies section should be present")
	assert.Contains(t, result, "write-sink", "write-sink should be rendered")
	assert.Contains(t, result, "accept", "accept should be rendered")
	assert.Contains(t, result, "\"private:myorg/myrepo\"", "accept pattern should be rendered")
}

// TestRenderSharedMCPConfigWithoutGuardPoliciesJSON tests that when no guard policies are set, no comma is added
func TestRenderCustomToolWithoutGuardPoliciesJSON(t *testing.T) {
	toolConfig := map[string]any{
		"url": "https://example.com/mcp",
	}

	var output strings.Builder
	renderer := MCPConfigRenderer{
		IndentLevel: "                ",
		Format:      "json",
		// No GuardPolicies set
	}

	err := renderSharedMCPConfig(&output, "my-tool", toolConfig, renderer)
	require.NoError(t, err, "renderSharedMCPConfig should succeed")

	result := output.String()
	// The url field should NOT have a trailing comma (it's the last field)
	assert.NotContains(t, result, "\"url\": \"https://example.com/mcp\",", "url field should not have trailing comma")
	// No guard policies
	assert.NotContains(t, result, "guard-policies", "guard-policies should not be rendered")
}

// TestMCPScriptsMCPWithGuardPoliciesJSON tests that mcp-scripts gets write-sink guard policies in JSON format
func TestMCPScriptsMCPWithGuardPoliciesJSON(t *testing.T) {
	guardPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept": []string{"*"},
		},
	}

	var output strings.Builder
	renderMCPScriptsMCPConfigWithOptions(&output, nil, true, false, nil, guardPolicies)

	result := output.String()
	assert.Contains(t, result, "\"guard-policies\"", "mcp-scripts should have guard-policies in JSON")
	assert.Contains(t, result, "\"write-sink\"", "mcp-scripts should have write-sink in JSON")
	// The headers section should have a trailing comma
	assert.Contains(t, result, "},\n", "headers closing brace should have trailing comma when guard policies follow")
}

// TestAgenticWorkflowsMCPWithGuardPoliciesJSON tests that agentic-workflows gets write-sink guard policies in JSON format
func TestAgenticWorkflowsMCPWithGuardPoliciesJSON(t *testing.T) {
	guardPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept": []string{"*"},
		},
	}

	var output strings.Builder
	renderAgenticWorkflowsMCPConfigWithOptions(&output, true, false, ActionModeRelease, guardPolicies, nil)

	result := output.String()
	assert.Contains(t, result, "\"guard-policies\"", "agentic-workflows should have guard-policies in JSON")
	assert.Contains(t, result, "\"write-sink\"", "agentic-workflows should have write-sink in JSON")
}

// TestAllNonGitHubMCPServersGetGuardPoliciesViaRenderer tests that the MCPConfigRendererUnified
// propagates WriteSinkGuardPolicies to all non-GitHub MCP server render methods
func TestAllNonGitHubMCPServersGetGuardPoliciesViaRenderer(t *testing.T) {
	guardPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept": []string{"*"},
		},
	}

	t.Run("agentic-workflows JSON", func(t *testing.T) {
		renderer := NewMCPConfigRenderer(MCPRendererOptions{
			Format:                 "json",
			IsLast:                 true,
			WriteSinkGuardPolicies: guardPolicies,
		})
		var output strings.Builder
		renderer.RenderAgenticWorkflowsMCP(&output)
		assert.Contains(t, output.String(), "guard-policies", "agentic-workflows JSON should have guard-policies")
	})

	t.Run("agentic-workflows TOML", func(t *testing.T) {
		renderer := NewMCPConfigRenderer(MCPRendererOptions{
			Format:                 "toml",
			WriteSinkGuardPolicies: guardPolicies,
		})
		var output strings.Builder
		renderer.RenderAgenticWorkflowsMCP(&output)
		result := output.String()
		// The TOML section ID for agentic-workflows uses the constant
		assert.Contains(t, result, "guard-policies", "agentic-workflows TOML should have guard-policies")
	})
}

// TestNonGitHubMCPServersGetGuardPoliciesFromAutoLockdown verifies that non-GitHub MCP servers
// get write-sink: {accept: ["*"]} guard policies when the GitHub tool is configured without
// explicit guard policies (auto-lockdown detection will set repos=all at runtime)
func TestNonGitHubMCPServersGetGuardPoliciesFromAutoLockdown(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"toolsets": []string{"default"},
			},
		},
	}

	policies := deriveWriteSinkGuardPolicyFromWorkflow(workflowData)
	require.NotNil(t, policies, "guard policies should be derived when GitHub tool triggers auto-lockdown")

	expectedPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept":          []string{"*"},
			"sink-visibility": sinkVisibilityExpr,
		},
	}
	assert.Equal(t, expectedPolicies, policies, "auto-lockdown should produce write-sink with accept=*")

}

// TestNonGitHubMCPServersGetGuardPoliciesWithGitHubApp verifies that non-GitHub MCP servers
// still get write-sink guard policies when a GitHub App is configured. GitHub App token scope
// is authentication, not a substitute for DIFC sink policy labels.
func TestNonGitHubMCPServersGetGuardPoliciesWithGitHubApp(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"toolsets": []string{"default"},
				"github-app": map[string]any{
					"app-id": "12345",
				},
			},
			"playwright": nil,
		},
	}

	policies := deriveWriteSinkGuardPolicyFromWorkflow(workflowData)
	expectedPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept":          []string{"*"},
			"sink-visibility": sinkVisibilityExpr,
		},
	}
	assert.Equal(t, expectedPolicies, policies, "GitHub App authentication should not suppress write-sink policy generation")
}

// TestAllNonGitHubMCPServersGetWriteSinkWhenGitHubHasAllowOnly verifies that when the GitHub
// MCP server has an explicit allow-only guard-policy configured (repos + min-integrity),
// ALL non-GitHub MCP server types receive a corresponding write-sink guard-policy via
// the MCPConfigRendererUnified.
func TestAllNonGitHubMCPServersGetWriteSinkWhenGitHubHasAllowOnly(t *testing.T) {
	tests := []struct {
		name           string
		githubConfig   map[string]any
		expectedAccept []string
		description    string
	}{
		{
			name: "repos=all min-integrity=none",
			githubConfig: map[string]any{
				"repos":         "all",
				"min-integrity": "none",
			},
			expectedAccept: []string{"*"},
			description:    "repos=all should produce accept=[*]",
		},
		{
			name: "repos=public min-integrity=approved",
			githubConfig: map[string]any{
				"repos":         "public",
				"min-integrity": "approved",
			},
			expectedAccept: []string{"*"},
			description:    "repos=public should produce accept=[*]",
		},
		{
			name: "repos=specific-repo min-integrity=approved",
			githubConfig: map[string]any{
				"repos":         "myorg/myrepo",
				"min-integrity": "approved",
			},
			expectedAccept: []string{"private:myorg/myrepo"},
			description:    "specific repo should produce accept=[private:myorg/myrepo]",
		},
		{
			name: "repos=owner-wildcard min-integrity=merged",
			githubConfig: map[string]any{
				"repos":         "myorg/*",
				"min-integrity": "merged",
			},
			expectedAccept: []string{"private:myorg"},
			description:    "owner/* should produce accept=[private:myorg]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Tools: map[string]any{
					"github":            tt.githubConfig,
					"agentic-workflows": nil,
				},
			}

			// Derive write-sink guard policies from the configured allow-only GitHub guard policy
			policies := deriveWriteSinkGuardPolicyFromWorkflow(workflowData)
			require.NotNil(t, policies, "write-sink guard policies should be derived when GitHub has allow-only policy: %s", tt.description)

			writeSink, ok := policies["write-sink"].(map[string]any)
			require.True(t, ok, "write-sink should be a map: %s", tt.description)
			assert.Equal(t, tt.expectedAccept, writeSink["accept"], "accept list should match: %s", tt.description)
			assert.Equal(t, sinkVisibilityExpr, writeSink["sink-visibility"], "sink-visibility should be the runtime expression: %s", tt.description)

			// Verify every non-GitHub MCP server type gets the guard policies via the renderer
			serverChecks := []struct {
				serverName string
				render     func(*strings.Builder, *MCPConfigRendererUnified)
			}{
				{
					serverName: "agentic-workflows",
					render: func(out *strings.Builder, r *MCPConfigRendererUnified) {
						r.RenderAgenticWorkflowsMCP(out)
					},
				},
				{
					serverName: "mcp-scripts",
					render: func(out *strings.Builder, r *MCPConfigRendererUnified) {
						mcpScripts := &MCPScriptsConfig{}
						r.RenderMCPScriptsMCP(out, mcpScripts, workflowData)
					},
				},
				{
					serverName: "safe-outputs",
					render: func(out *strings.Builder, r *MCPConfigRendererUnified) {
						r.RenderSafeOutputsMCP(out, workflowData)
					},
				},
			}

			for _, check := range serverChecks {
				t.Run(check.serverName+" JSON", func(t *testing.T) {
					renderer := NewMCPConfigRenderer(MCPRendererOptions{
						Format:                 "json",
						IsLast:                 true,
						WriteSinkGuardPolicies: policies,
					})
					var output strings.Builder
					check.render(&output, renderer)
					result := output.String()
					assert.Contains(t, result, "\"guard-policies\"",
						"%s should have guard-policies when GitHub has allow-only policy: %s", check.serverName, tt.description)
					assert.Contains(t, result, "\"write-sink\"",
						"%s should have write-sink policy: %s", check.serverName, tt.description)
					assert.Contains(t, result, "\"accept\"",
						"%s should have accept field: %s", check.serverName, tt.description)
					assert.Contains(t, result, "\"sink-visibility\"",
						"%s should have sink-visibility field: %s", check.serverName, tt.description)
					// The sink-visibility value is a shell env var reference (not a raw GHA expression)
					// so that no ${{ }} expression appears in the run: heredoc.
					assert.Contains(t, result, "${"+sinkVisibilityEnvVar+"}",
						"%s should render sink-visibility as shell env var reference: %s", check.serverName, tt.description)
				})
			}

		})
	}
}

// TestNonGitHubMCPServersGetGuardPoliciesWhenGitHubConfigured verifies the end-to-end flow:
// when GitHub has repos=all, all non-GitHub MCP servers get write-sink: {accept: ["*"]}
func TestNonGitHubMCPServersGetGuardPoliciesWhenGitHubConfigured(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"repos":         "all",
				"min-integrity": "none",
			},
		},
	}

	policies := deriveWriteSinkGuardPolicyFromWorkflow(workflowData)
	require.NotNil(t, policies, "guard policies should be derived when GitHub has guard policy")

	expectedPolicies := map[string]any{
		"write-sink": map[string]any{
			"accept":          []string{"*"},
			"sink-visibility": sinkVisibilityExpr,
		},
	}
	assert.Equal(t, expectedPolicies, policies, "policies should match expected write-sink with accept=*")

}
