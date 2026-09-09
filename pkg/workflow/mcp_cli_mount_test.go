//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasBashRestrictedAllowlist(t *testing.T) {
	tests := []struct {
		name     string
		tools    map[string]any
		expected bool
	}{
		{
			name:     "nil tools",
			tools:    nil,
			expected: false,
		},
		{
			name:     "no bash key",
			tools:    map[string]any{"edit": nil},
			expected: false,
		},
		{
			name:     "bash nil (unrestricted)",
			tools:    map[string]any{"bash": nil},
			expected: false,
		},
		{
			name:     "bash empty array",
			tools:    map[string]any{"bash": []any{}},
			expected: false,
		},
		{
			name:     "bash wildcard star",
			tools:    map[string]any{"bash": []any{"*"}},
			expected: false,
		},
		{
			name:     "bash wildcard colon-star",
			tools:    map[string]any{"bash": []any{":*"}},
			expected: false,
		},
		{
			name:     "bash with specific commands",
			tools:    map[string]any{"bash": []any{"echo", "ls"}},
			expected: true,
		},
		{
			name:     "bash with single command",
			tools:    map[string]any{"bash": []any{"git:*"}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasBashRestrictedAllowlist(tt.tools)
			assert.Equal(t, tt.expected, result, "unexpected result for hasBashRestrictedAllowlist")
		})
	}
}

func TestWithMountedCLIShellCommandsInRestrictedBash_PlaywrightCLIMode(t *testing.T) {
	t.Run("playwright cli mode adds playwright-cli:* to restricted bash", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"echo"},
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		bash, ok := result["bash"].([]any)
		require.True(t, ok, "bash should be a []any")
		assert.Contains(t, bash, "playwright-cli:*", "playwright-cli:* should be auto-added")
		assert.Contains(t, bash, "echo", "original command should be preserved")
	})

	t.Run("playwright cli mode with unrestricted bash (nil) does not add playwright-cli:*", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": nil,
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		// bash is nil (unrestricted), so tools map should be unchanged
		bash, hasBash := result["bash"]
		assert.True(t, hasBash, "bash key should still exist")
		assert.Nil(t, bash, "bash should remain nil (unrestricted)")
	})

	t.Run("playwright cli mode with wildcard bash does not add playwright-cli:*", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"*"},
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		bash, ok := result["bash"].([]any)
		require.True(t, ok, "bash should be a []any")
		// Wildcard must be preserved and playwright-cli:* must not be injected
		assert.Equal(t, []any{"*"}, bash, "bash should remain exactly [\"*\"] — wildcard preserved and nothing injected")
	})

	t.Run("playwright default mode adds playwright-cli:*", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash":       []any{"echo"},
				"playwright": true,
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		bash, ok := result["bash"].([]any)
		require.True(t, ok, "bash should be a []any")
		assert.Contains(t, bash, "playwright-cli:*")
	})

	t.Run("playwright-cli:* not duplicated when already present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"echo", "playwright-cli:*"},
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		bash, ok := result["bash"].([]any)
		require.True(t, ok, "bash should be a []any")
		count := 0
		for _, cmd := range bash {
			if cmdStr, ok := cmd.(string); ok && cmdStr == "playwright-cli:*" {
				count++
			}
		}
		assert.Equal(t, 1, count, "playwright-cli:* should appear exactly once")
	})

	t.Run("github gh-proxy mode adds gh:* to restricted bash", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"echo"},
				"github": map[string]any{
					"mode": "gh-proxy",
				},
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		bash, ok := result["bash"].([]any)
		require.True(t, ok, "bash should be a []any")
		assert.Contains(t, bash, "gh:*", "gh:* should be auto-added in gh-proxy mode")
		assert.Contains(t, bash, "echo", "original command should be preserved")
	})

	t.Run("github local mode does not add gh:* to restricted bash", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"echo"},
				"github": map[string]any{
					"mode": "local",
				},
			},
		}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		require.NotNil(t, result, "result should not be nil")
		bash, ok := result["bash"].([]any)
		require.True(t, ok, "bash should be a []any")
		assert.NotContains(t, bash, "gh:*", "gh:* should not be auto-added outside gh-proxy mode")
	})

	t.Run("nil workflowData returns nil", func(t *testing.T) {
		result := withMountedCLIShellCommandsInRestrictedBash(nil)
		assert.Nil(t, result, "nil input should return nil")
	})

	t.Run("workflowData with nil tools returns nil tools", func(t *testing.T) {
		workflowData := &WorkflowData{Tools: nil}
		result := withMountedCLIShellCommandsInRestrictedBash(workflowData)
		assert.Nil(t, result, "nil tools should be returned unchanged")
	})
}

func TestBuildMCPCLIPromptSection_PromptFileUsesNonHeadingLabels(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	section := buildMCPCLIPromptSection(data)
	require.NotNil(t, section)
	assert.Equal(t, mcpCLIToolsWithSafeOutputsPromptFile, section.Content)
	// GH_AW_MCP_CLI_SERVERS_LIST must be a compile-time static value, NOT a step output
	// reference. Referencing steps.mount-mcp-clis (agent job) inside the activation job's
	// env block is out of scope and triggers actionlint errors.
	serversList := section.EnvVars["GH_AW_MCP_CLI_SERVERS_LIST"]
	assert.NotEmpty(t, serversList, "server list must not be empty")
	assert.NotContains(t, serversList, "${{", "server list must not use a GitHub Actions expression (step output reference is out of scope in activation job)")
	assert.Contains(t, serversList, "safeoutputs", "server list must mention the safeoutputs server")
	assert.Contains(t, serversList, "--help", "server list must guide agents to use --help for tool signatures")

	wd, err := os.Getwd()
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Clean(filepath.Join(wd, "../../actions/setup/md", section.Content)))
	require.NoError(t, err)

	prompt := string(content)
	assert.NotRegexp(t, `(?m)^\s*(>\s*)?##\s+`, prompt, "prompt must not contain H2 Markdown headings")
	assert.NotRegexp(t, `(?m)^\s*(>\s*)?###\s+`, prompt, "prompt must not contain H3 Markdown headings")
	assert.Contains(t, prompt, "Use `<server> --help` and `<server> <tool> --help` for the same schema-derived signatures and examples before calling any command.")
}

func TestBuildMCPCLIPromptSection_UsesBaseTemplateWithoutSafeOutputs(t *testing.T) {
	data := &WorkflowData{
		MCPScripts: &MCPScriptsConfig{
			Tools: map[string]*MCPScriptToolConfig{
				"hello": {Name: "hello", Script: "return 'ok';"},
			},
		},
	}

	section := buildMCPCLIPromptSection(data)
	require.NotNil(t, section)
	assert.Equal(t, mcpCLIToolsPromptFile, section.Content)
}

func TestGetMCPCLIServerNames_CopilotIncludesManifestServersInPromptList(t *testing.T) {
	t.Run("copilot adds github and custom MCP servers when CLI mounts are active", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: string(constants.CopilotEngine)},
			Tools: map[string]any{
				"github": true,
				"azure-devops": map[string]any{
					"command": "azure-devops-mcp",
				},
			},
			ParsedTools: NewTools(map[string]any{"github": true}),
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
		}

		servers := getMCPCLIServerNames(data)
		assert.Equal(t,
			[]string{"azure-devops", constants.GitHubMCPServerID.String(), constants.SafeOutputsMCPServerID.String()},
			servers,
			"server list should contain all mounted servers in sorted order",
		)
	})

	t.Run("copilot with cli-proxy only and github MCP (no safeoutputs) still advertises github", func(t *testing.T) {
		// Regression: len(servers)==0 before the Copilot block because GitHub is
		// excluded from the initial collection. The activation condition must include
		// ParsedTools.CLIProxy so this case is not silently skipped.
		tools := map[string]any{
			"github":       true,
			"cli-proxy":    true,
			"azure-devops": map[string]any{"command": "azure-devops-mcp"},
		}
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: string(constants.CopilotEngine)},
			Tools:        tools,
			ParsedTools:  NewTools(tools),
			// No SafeOutputs, no MCPScripts → servers is empty before the Copilot block
		}

		servers := getMCPCLIServerNames(data)
		assert.Equal(t,
			[]string{"azure-devops", constants.GitHubMCPServerID.String()},
			servers,
			"server list should contain all mounted servers in sorted order",
		)
	})

	t.Run("copilot without any CLI mount trigger returns nil (github not added)", func(t *testing.T) {
		// Boundary condition: no safeoutputs, no mcpscripts, no cli-proxy.
		// The Copilot augmentation block must not activate and github must not appear.
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: string(constants.CopilotEngine)},
			Tools:        map[string]any{"github": true},
			ParsedTools:  NewTools(map[string]any{"github": true}),
			// SafeOutputs intentionally nil, CLIProxy false
		}

		servers := getMCPCLIServerNames(data)
		assert.Nil(t, servers, "no CLI mount trigger active → Copilot block skipped, github not advertised")
	})

	t.Run("non-copilot keeps existing behavior", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: string(constants.ClaudeEngine)},
			Tools: map[string]any{
				"github": true,
				"azure-devops": map[string]any{
					"command": "azure-devops-mcp",
				},
			},
			ParsedTools: NewTools(map[string]any{"github": true}),
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
		}

		servers := getMCPCLIServerNames(data)
		assert.Equal(t, []string{constants.SafeOutputsMCPServerID.String()}, servers)
	})
}

func TestBuildMCPCLIPromptSection_OmittedWhenBashDisabled(t *testing.T) {
	data := &WorkflowData{
		BashDisabled: true,
		SafeOutputs: &SafeOutputsConfig{
			AddLabels: &AddLabelsConfig{},
		},
	}

	require.NotEmpty(t, getMCPCLIServerNames(data), "safeoutputs is still CLI-mounted")
	assert.Nil(t, buildMCPCLIPromptSection(data), "CLI-only instructions must be omitted when the agent has no shell")
}
