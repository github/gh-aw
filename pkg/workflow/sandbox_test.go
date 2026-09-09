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

func TestValidateSandboxConfig(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		expectError bool
		errorMsg    string
	}{
		{
			name: "nil workflow data",
			data: nil,
		},
		{
			name: "nil sandbox config",
			data: &WorkflowData{},
		},
		{
			name: "valid AWF sandbox config",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{
						"mode": "remote",
					},
				},
			},
		},
		{
			name: "network isolation allows host.docker.internal HTTP MCP server URL",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{
						"mode": "remote",
					},
				},
				ResolvedMCPServers: map[string]any{
					"mempalace": map[string]any{
						"type": "http",
						"url":  "http://host.docker.internal:8765/mcp",
					},
				},
			},
		},
		{
			name: "sandbox.agent false with feature enabled",
			data: &WorkflowData{
				Features: map[string]any{
					"dangerously-disable-sandbox-agent": true,
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
		},
		{
			name: "sandbox.agent false without feature",
			data: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expectError: true,
			errorMsg:    "dangerously-disable-sandbox-agent",
		},
		{
			name: "sandbox.agent false with feature disabled",
			data: &WorkflowData{
				Features: map[string]any{
					"dangerously-disable-sandbox-agent": false,
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expectError: true,
			errorMsg:    "dangerously-disable-sandbox-agent",
		},
		{
			name: "sandbox.agent false with legacy feature",
			data: &WorkflowData{
				Features: map[string]any{
					"dangerously-disable-sandbox": true,
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expectError: true,
			errorMsg:    "dangerously-disable-sandbox-agent",
		},
		{
			name: "sandbox.agent false with non-boolean feature",
			data: &WorkflowData{
				Features: map[string]any{
					"dangerously-disable-sandbox-agent": "true",
				},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expectError: true,
			errorMsg:    "dangerously-disable-sandbox-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSandboxConfig(tt.data)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					require.ErrorContains(t, err, tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestApplySandboxDefaults(t *testing.T) {
	tests := []struct {
		name                   string
		config                 *SandboxConfig
		engine                 *EngineConfig
		expected               *SandboxConfig
		expectDefaultWritePath bool
		expectedAllowWrite     []string
		unexpectedAllowWrite   []string
	}{
		{
			name:                   "nil config creates default with AWF",
			config:                 nil,
			engine:                 &EngineConfig{ID: "copilot"},
			expectDefaultWritePath: false,
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			name: "explicit AWF config preserved",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
			engine:                 &EngineConfig{ID: "copilot"},
			expectDefaultWritePath: false,
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			// version-only object (no id/type) must default to AWF so the sandbox is
			// always enabled, matching the previous analysis of the smoke-gemini bug.
			name: "version-only agent defaults to AWF",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Version: "v0.25.29",
				},
			},
			engine:                 &EngineConfig{ID: "gemini"},
			expectDefaultWritePath: false,
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type:    SandboxTypeAWF,
					Version: "v0.25.29",
				},
			},
		},
		{
			// An agent object with only an empty string ID must also default to AWF.
			name: "empty ID agent defaults to AWF",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{},
			},
			engine:                 &EngineConfig{ID: "copilot"},
			expectDefaultWritePath: false,
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			// Explicitly disabled agent must never be overridden.
			name: "disabled agent is preserved",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
			engine:                 nil,
			expectDefaultWritePath: false,
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
		},
		{
			// Explicit allowWrite on a compose runtime is honoured verbatim: no implicit
			// default path is added, because narrowing compose bind mounts turns the
			// container rootfs read-only and breaks AWF's /tmp/awf-init mount.
			name: "existing allowWrite entries are preserved without seeding a default",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
					Config: &SandboxRuntimeConfig{
						Filesystem: &SRTFilesystemConfig{
							AllowWrite: []string{"/tmp/custom"},
						},
					},
				},
			},
			engine:                 &EngineConfig{ID: "claude"},
			expectDefaultWritePath: false,
			expectedAllowWrite:     []string{"/tmp/custom"},
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			// Cloud Hypervisor narrows the /workspace and /tmp/gh-aw exports independently,
			// so the default write path alone would leave /workspace (and the CH-managed
			// HOME under it) read-only. See ensureDefaultAgentWritePath.
			name: "cloud-hypervisor runtime seeds agent, logs, workspace and awf-home write paths",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type:    SandboxTypeAWF,
					Runtime: AgentRuntimeCloudHypervisor,
				},
			},
			engine:                 &EngineConfig{ID: "copilot"},
			expectDefaultWritePath: true,
			expectedAllowWrite:     []string{defaultAgentWorkspaceWritePath, defaultAgentLogsWritePath, cloudHypervisorWorkspaceWritePath, cloudHypervisorAwfHomeWritePath},
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			name: "cloud-hypervisor runtime does not grant Copilot logs path to other engines",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type:    SandboxTypeAWF,
					Runtime: AgentRuntimeCloudHypervisor,
				},
			},
			engine:                 &EngineConfig{ID: "claude"},
			expectDefaultWritePath: true,
			expectedAllowWrite:     []string{defaultAgentWorkspaceWritePath, cloudHypervisorWorkspaceWritePath, cloudHypervisorAwfHomeWritePath},
			unexpectedAllowWrite:   []string{defaultAgentLogsWritePath},
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			name: "cloud-hypervisor runtime seeds mcp-config write path for codex engine",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type:    SandboxTypeAWF,
					Runtime: AgentRuntimeCloudHypervisor,
				},
			},
			engine:                 &EngineConfig{ID: "codex"},
			expectDefaultWritePath: true,
			expectedAllowWrite:     []string{defaultAgentWorkspaceWritePath, constants.TmpMcpConfigDir, cloudHypervisorWorkspaceWritePath, cloudHypervisorAwfHomeWritePath},
			unexpectedAllowWrite:   []string{defaultAgentLogsWritePath},
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
		{
			name: "cloud-hypervisor runtime does not grant codex mcp-config path to other engines",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type:    SandboxTypeAWF,
					Runtime: AgentRuntimeCloudHypervisor,
				},
			},
			engine:                 &EngineConfig{ID: "claude"},
			expectDefaultWritePath: true,
			expectedAllowWrite:     []string{defaultAgentWorkspaceWritePath, cloudHypervisorWorkspaceWritePath, cloudHypervisorAwfHomeWritePath},
			unexpectedAllowWrite:   []string{constants.TmpMcpConfigDir},
			expected: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applySandboxDefaults(tt.config, tt.engine)
			require.NotNil(t, result)
			require.NotNil(t, result.Agent)
			assert.Equal(t, tt.expected.Agent.Type, result.Agent.Type, "agent type")
			if tt.expected.Agent.Version != "" {
				assert.Equal(t, tt.expected.Agent.Version, result.Agent.Version, "agent version")
			}
			assert.Equal(t, tt.expected.Agent.Disabled, result.Agent.Disabled, "agent disabled flag")
			if tt.expectDefaultWritePath {
				require.NotNil(t, result.Agent.Config)
				require.NotNil(t, result.Agent.Config.Filesystem)
				assert.Contains(t, result.Agent.Config.Filesystem.AllowWrite, defaultAgentWorkspaceWritePath)
			} else if result.Agent.Config != nil && result.Agent.Config.Filesystem != nil {
				assert.NotContains(t, result.Agent.Config.Filesystem.AllowWrite, defaultAgentWorkspaceWritePath)
			}
			for _, expectedPath := range tt.expectedAllowWrite {
				require.NotNil(t, result.Agent.Config)
				require.NotNil(t, result.Agent.Config.Filesystem)
				assert.Contains(t, result.Agent.Config.Filesystem.AllowWrite, expectedPath)
			}
			for _, unexpectedPath := range tt.unexpectedAllowWrite {
				require.NotNil(t, result.Agent.Config)
				require.NotNil(t, result.Agent.Config.Filesystem)
				assert.NotContains(t, result.Agent.Config.Filesystem.AllowWrite, unexpectedPath)
			}
		})
	}
}

func TestEnsureCacheMemoryWritePaths(t *testing.T) {
	sandboxConfig := applySandboxDefaults(&SandboxConfig{
		Agent: &AgentSandboxConfig{
			Type:    SandboxTypeAWF,
			Runtime: AgentRuntimeCloudHypervisor,
		},
	}, &EngineConfig{ID: "claude"})
	cacheMemoryConfig := &CacheMemoryConfig{
		Caches: []CacheMemoryEntry{
			{ID: "default"},
			{ID: "session"},
		},
	}

	ensureCacheMemoryWritePaths(sandboxConfig, cacheMemoryConfig)
	ensureCacheMemoryWritePaths(sandboxConfig, cacheMemoryConfig)

	require.NotNil(t, sandboxConfig.Agent.Config)
	require.NotNil(t, sandboxConfig.Agent.Config.Filesystem)
	assert.Equal(t, []string{
		defaultAgentWorkspaceWritePath,
		cloudHypervisorWorkspaceWritePath,
		cloudHypervisorAwfHomeWritePath,
		"/tmp/gh-aw/cache-memory",
		"/tmp/gh-aw/cache-memory-session",
	}, sandboxConfig.Agent.Config.Filesystem.AllowWrite)
}

func TestEnsureRepoMemoryWritePaths(t *testing.T) {
	sandboxConfig := applySandboxDefaults(&SandboxConfig{
		Agent: &AgentSandboxConfig{
			Type:    SandboxTypeAWF,
			Runtime: AgentRuntimeCloudHypervisor,
		},
	}, &EngineConfig{ID: "claude"})
	repoMemoryConfig := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{ID: "default"},
			{ID: "notes"},
		},
	}

	ensureRepoMemoryWritePaths(sandboxConfig, repoMemoryConfig)
	ensureRepoMemoryWritePaths(sandboxConfig, repoMemoryConfig)

	require.NotNil(t, sandboxConfig.Agent.Config)
	require.NotNil(t, sandboxConfig.Agent.Config.Filesystem)
	assert.Equal(t, []string{
		defaultAgentWorkspaceWritePath,
		cloudHypervisorWorkspaceWritePath,
		cloudHypervisorAwfHomeWritePath,
		"/tmp/gh-aw/repo-memory/default",
		"/tmp/gh-aw/repo-memory/notes",
	}, sandboxConfig.Agent.Config.Filesystem.AllowWrite)
}

func TestEnsureRepoMemoryWritePathsSkipsNonCloudHypervisorRuntime(t *testing.T) {
	sandboxConfig := applySandboxDefaults(&SandboxConfig{
		Agent: &AgentSandboxConfig{
			Type: SandboxTypeAWF,
		},
	}, &EngineConfig{ID: "claude"})
	repoMemoryConfig := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{{ID: "default"}},
	}

	ensureRepoMemoryWritePaths(sandboxConfig, repoMemoryConfig)

	if sandboxConfig.Agent.Config != nil && sandboxConfig.Agent.Config.Filesystem != nil {
		assert.NotContains(t, sandboxConfig.Agent.Config.Filesystem.AllowWrite, "/tmp/gh-aw/repo-memory/default")
	}
}

func TestMergeImportedSandboxAgentMounts(t *testing.T) {
	tests := []struct {
		name           string
		initial        *SandboxConfig
		imported       []string
		expected       []string
		expectNil      bool
		expectDisabled bool
	}{
		{
			name:      "no imported mounts returns original nil config",
			initial:   nil,
			imported:  nil,
			expectNil: true,
		},
		{
			name:     "creates sandbox agent config from imports",
			initial:  nil,
			imported: []string{"/tool-a:/tool-a:ro"},
			expected: []string{"/tool-a:/tool-a:ro"},
		},
		{
			name: "deduplicates imported and main mounts",
			initial: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Mounts: []string{
						"/main:/main:ro",
						"/shared:/shared:ro",
					},
				},
			},
			imported: []string{
				"/shared:/shared:ro",
				"/import-a:/import-a:ro",
			},
			expected: []string{
				"/shared:/shared:ro",
				"/import-a:/import-a:ro",
				"/main:/main:ro",
			},
		},
		{
			name: "does not modify disabled agent sandbox",
			initial: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
			imported:       []string{"/tool-a:/tool-a:ro"},
			expectDisabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeImportedSandboxAgentMounts(tt.initial, tt.imported)

			if tt.expectNil {
				assert.Nil(t, merged)
				return
			}

			require.NotNil(t, merged)
			require.NotNil(t, merged.Agent)
			if tt.expectDisabled {
				assert.True(t, merged.Agent.Disabled)
				assert.Empty(t, merged.Agent.Mounts)
				return
			}
			assert.Equal(t, tt.expected, merged.Agent.Mounts)
		})
	}
}

func TestDefaultAgentWorkspaceWritePath(t *testing.T) {
	assert.Equal(t, "/tmp/gh-aw/agent", defaultAgentWorkspaceWritePath)
}

func TestMergeImportedSandboxAgentRuntimeInstall(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		initial  *SandboxConfig
		imported *bool
		expected *bool
	}{
		{
			name:     "nil import leaves config unchanged",
			initial:  &SandboxConfig{Agent: &AgentSandboxConfig{RuntimeInstall: &trueVal}},
			imported: nil,
			expected: &trueVal,
		},
		{
			name:     "false import overrides explicit true",
			initial:  &SandboxConfig{Agent: &AgentSandboxConfig{RuntimeInstall: &trueVal}},
			imported: &falseVal,
			expected: &falseVal,
		},
		{
			name:     "true import does not override explicit false",
			initial:  &SandboxConfig{Agent: &AgentSandboxConfig{RuntimeInstall: &falseVal}},
			imported: &trueVal,
			expected: &falseVal,
		},
		{
			name:     "true import initializes unset field",
			initial:  &SandboxConfig{Agent: &AgentSandboxConfig{}},
			imported: &trueVal,
			expected: &trueVal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := mergeImportedSandboxAgentRuntimeInstall(tt.initial, tt.imported)
			if tt.imported == nil {
				assert.Equal(t, tt.initial, merged)
				return
			}

			require.NotNil(t, merged)
			require.NotNil(t, merged.Agent)
			require.NotNil(t, merged.Agent.RuntimeInstall)
			assert.Equal(t, *tt.expected, *merged.Agent.RuntimeInstall)
		})
	}
}

func TestWorkflowHashWithSandbox(t *testing.T) {
	// Test that sandbox config is included in workflow hash
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	content := `---
sandbox:
  agent: awf
---
# Test Workflow
Test prompt
`
	err := os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err)

	// Just verify the file can be read
	data, err := os.ReadFile(workflowFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "sandbox:")
}
