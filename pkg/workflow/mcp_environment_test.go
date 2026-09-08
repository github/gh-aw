//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasGitHubOIDCAuthInTools(t *testing.T) {
	tests := []struct {
		name     string
		tools    map[string]any
		expected bool
	}{
		{
			name:     "empty tools",
			tools:    map[string]any{},
			expected: false,
		},
		{
			name: "only standard tools (github, playwright)",
			tools: map[string]any{
				"github":     map[string]any{},
				"playwright": map[string]any{},
			},
			expected: false,
		},
		{
			name: "http server with headers but no auth",
			tools: map[string]any{
				"tavily": map[string]any{
					"type": "http",
					"url":  "https://mcp.tavily.com/mcp/",
					"headers": map[string]any{
						"Authorization": "Bearer ${{ secrets.TAVILY_API_KEY }}",
					},
				},
			},
			expected: false,
		},
		{
			name: "http server with github-oidc auth",
			tools: map[string]any{
				"oidc-server": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type":     "github-oidc",
						"audience": "https://my-server.example.com",
					},
				},
			},
			expected: true,
		},
		{
			name: "http server with github-oidc auth no audience",
			tools: map[string]any{
				"oidc-server": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type": "github-oidc",
					},
				},
			},
			expected: true,
		},
		{
			name: "mixed servers with one oidc",
			tools: map[string]any{
				"github": map[string]any{},
				"tavily": map[string]any{
					"type": "http",
					"url":  "https://mcp.tavily.com/mcp/",
					"headers": map[string]any{
						"Authorization": "Bearer ${{ secrets.TAVILY_API_KEY }}",
					},
				},
				"oidc-server": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type": "github-oidc",
					},
				},
			},
			expected: true,
		},
		{
			name: "stdio server is not treated as oidc",
			tools: map[string]any{
				"my-stdio": map[string]any{
					"type":      "stdio",
					"container": "mcp/server:latest",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasGitHubOIDCAuthInTools(tt.tools)
			assert.Equal(t, tt.expected, result, "hasGitHubOIDCAuthInTools should return %v", tt.expected)
		})
	}
}

// TestOIDCEnvVarsInDockerCommand verifies that ACTIONS_ID_TOKEN_REQUEST_URL and
// ACTIONS_ID_TOKEN_REQUEST_TOKEN are included as -e flags in the MCP Gateway docker
// command when an HTTP MCP server uses auth.type: "github-oidc".
func TestOIDCEnvVarsInDockerCommand(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"mode": "local",
			},
			"oidc-server": map[string]any{
				"type": "http",
				"url":  "https://my-server.example.com/mcp",
				"auth": map[string]any{
					"type":     "github-oidc",
					"audience": "https://my-server.example.com",
				},
			},
		},
	}

	compiler := &Compiler{}
	mockEngine := NewClaudeEngine()

	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData),
		"generateMCPSetup should succeed")
	output := yaml.String()

	assert.Contains(t, output, "-e ACTIONS_ID_TOKEN_REQUEST_URL",
		"Docker command should include -e ACTIONS_ID_TOKEN_REQUEST_URL when github-oidc auth is configured")
	assert.Contains(t, output, "-e ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"Docker command should include -e ACTIONS_ID_TOKEN_REQUEST_TOKEN when github-oidc auth is configured")
}

// TestOIDCEnvVarsNotInDockerCommandWithoutOIDCAuth verifies that OIDC env vars are
// NOT included in the docker command when no server uses auth.type: "github-oidc".
func TestOIDCEnvVarsNotInDockerCommandWithoutOIDCAuth(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"mode": "local",
			},
			"tavily": map[string]any{
				"type": "http",
				"url":  "https://mcp.tavily.com/mcp/",
				"headers": map[string]any{
					"Authorization": "Bearer ${{ secrets.TAVILY_API_KEY }}",
				},
			},
		},
	}

	compiler := &Compiler{}
	mockEngine := NewClaudeEngine()

	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData),
		"generateMCPSetup should succeed")
	output := yaml.String()

	assert.NotContains(t, output, "-e ACTIONS_ID_TOKEN_REQUEST_URL",
		"Docker command should NOT include OIDC env vars without github-oidc auth")
	assert.NotContains(t, output, "-e ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"Docker command should NOT include OIDC env vars without github-oidc auth")
}

// TestCollectMCPEnvironmentVariables_CodexEngineIncludesCODEXHOME verifies that
// CODEX_HOME is included in the MCP gateway step environment for Codex engine workflows
func TestCollectMCPEnvironmentVariables_CodexEngineIncludesCODEXHOME(t *testing.T) {
	tools := map[string]any{
		"github": map[string]any{
			"toolsets": []string{"repos"},
		},
	}
	mcpTools := []string{"github"}
	workflowData := &WorkflowData{AI: "codex"}

	envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, false)

	assert.Equal(t, "/tmp/gh-aw/mcp-config", envVars["CODEX_HOME"],
		"CODEX_HOME should be set to /tmp/gh-aw/mcp-config for Codex engine")
}

// TestCollectMCPEnvironmentVariables_NonCodexEngineExcludesCODEXHOME verifies that
// CODEX_HOME is NOT included for non-Codex engine workflows
func TestCollectMCPEnvironmentVariables_NonCodexEngineExcludesCODEXHOME(t *testing.T) {
	tools := map[string]any{
		"github": map[string]any{
			"toolsets": []string{"repos"},
		},
	}
	mcpTools := []string{"github"}

	for _, engine := range []string{"copilot", "claude", ""} {
		t.Run("engine="+engine, func(t *testing.T) {
			workflowData := &WorkflowData{AI: engine}
			envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, false)
			assert.NotContains(t, envVars, "CODEX_HOME",
				"CODEX_HOME should not be set for %s engine", engine)
		})
	}
}

func TestCollectMCPEnvironmentVariables_SafeOutputsIncludesCreatePullRequestPolicy(t *testing.T) {
	tools := map[string]any{
		"safe-outputs": map[string]any{},
	}
	mcpTools := []string{"safe-outputs"}
	workflowData := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}

	envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, false)

	assert.Equal(t,
		"${{ vars."+compilerenv.PolicyAllowCreatePullRequest+" || 'true' }}",
		envVars[compilerenv.PolicyAllowCreatePullRequest],
	)
}

func TestCollectMCPEnvironmentVariables_DynamicDelegationWithoutPrimaryGitHubUsesDestinationVisibility(t *testing.T) {
	workflowData := dynamicEnclaveWorkflowData()
	workflowData.Tools["github"] = false
	workflowData.SafeOutputs = &SafeOutputsConfig{AddComments: &AddCommentsConfig{}}
	envVars := collectMCPEnvironmentVariables(workflowData.Tools, []string{"github", "safe-outputs"}, workflowData, false)

	assert.Equal(t, "${{ steps.determine-automatic-lockdown.outputs.visibility }}", envVars[sinkVisibilityEnvVar])
	assert.Equal(t, "20", envVars["GH_AW_TIMEOUT_MINUTES"])
	assert.NotContains(t, envVars, "GITHUB_MCP_GUARD_MIN_INTEGRITY")
	assert.NotContains(t, envVars, "GITHUB_MCP_GUARD_REPOS")
}
