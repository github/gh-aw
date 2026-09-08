//go:build !integration

package workflow

import (
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureDefaultMCPGatewayConfig(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		validate     func(*testing.T, *WorkflowData)
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			validate: func(t *testing.T, wd *WorkflowData) {
				// Should not panic, just return
			},
		},
		{
			name:         "creates default config when none exists",
			workflowData: &WorkflowData{},
			validate: func(t *testing.T, wd *WorkflowData) {
				require.NotNil(t, wd.SandboxConfig, "SandboxConfig should be created")
				require.NotNil(t, wd.SandboxConfig.MCP, "MCP config should be created")
				assert.Equal(t, constants.DefaultMCPGatewayContainer, wd.SandboxConfig.MCP.Container, "Container should be default")
				assert.Equal(t, string(constants.DefaultMCPGatewayVersion), wd.SandboxConfig.MCP.Version, "Version should be default")
				assert.Equal(t, int(DefaultMCPGatewayPort), wd.SandboxConfig.MCP.Port, "Port should be default")
				assert.Equal(t, constants.DefaultMCPGatewayPayloadDir, wd.SandboxConfig.MCP.PayloadDir, "PayloadDir should be default")
				assert.Len(t, wd.SandboxConfig.MCP.Mounts, 4, "Should have 4 default mounts")
			},
		},
		{
			name: "fills in missing container field",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Version: "v1.0.0",
						Port:    8080,
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, constants.DefaultMCPGatewayContainer, wd.SandboxConfig.MCP.Container, "Container should be filled with default")
				assert.Equal(t, "v1.0.0", wd.SandboxConfig.MCP.Version, "Version should be preserved")
				assert.Equal(t, 8080, wd.SandboxConfig.MCP.Port, "Port should be preserved")
			},
		},
		{
			name: "fills in missing version field",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Port:      8080,
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, "custom-container", wd.SandboxConfig.MCP.Container, "Container should be preserved")
				assert.Equal(t, string(constants.DefaultMCPGatewayVersion), wd.SandboxConfig.MCP.Version, "Version should be filled with default")
				assert.Equal(t, 8080, wd.SandboxConfig.MCP.Port, "Port should be preserved")
			},
		},
		{
			name: "dynamic enclave uses delegation minimum when version is missing",
			workflowData: func() *WorkflowData {
				wd := dynamicEnclaveWorkflowData()
				wd.SandboxConfig.MCP.Version = ""
				return wd
			}(),
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, string(constants.MCPGDynamicRepositoryDelegationMinVersion), wd.SandboxConfig.MCP.Version, "Version should be elevated for dynamic GitHub delegation")
			},
		},
		{
			name: "fills in missing port field",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Version:   "v1.0.0",
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, "custom-container", wd.SandboxConfig.MCP.Container, "Container should be preserved")
				assert.Equal(t, "v1.0.0", wd.SandboxConfig.MCP.Version, "Version should be preserved")
				assert.Equal(t, int(DefaultMCPGatewayPort), wd.SandboxConfig.MCP.Port, "Port should be filled with default")
			},
		},
		{
			name: "preserves user-specified latest version",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Version:   "latest",
						Port:      8080,
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, "latest", wd.SandboxConfig.MCP.Version, "User-specified 'latest' version should be preserved")
			},
		},
		{
			name: "adds default mounts when none exist",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Version:   "v1.0.0",
						Port:      8080,
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Len(t, wd.SandboxConfig.MCP.Mounts, 4, "Should have 4 default mounts")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "/opt:/opt:ro", "Should have /opt mount")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "/tmp:/tmp:rw", "Should have /tmp mount")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw", "Should have GITHUB_WORKSPACE mount")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "Should have safeoutputs mount")
			},
		},
		{
			name: "preserves custom mounts",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Version:   "v1.0.0",
						Port:      8080,
						Mounts:    []string{"/custom:/mount:ro"},
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Len(t, wd.SandboxConfig.MCP.Mounts, 1, "Should preserve custom mounts")
				assert.Equal(t, "/custom:/mount:ro", wd.SandboxConfig.MCP.Mounts[0], "Custom mount should be preserved")
			},
		},
		{
			name: "adds safeoutputs mount to custom mounts when upload assets enabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Version:   "v1.0.0",
						Port:      8080,
						Mounts:    []string{"/custom:/mount:ro"},
					},
				},
				SafeOutputs: &SafeOutputsConfig{
					UploadAssets: &UploadAssetsConfig{},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Len(t, wd.SandboxConfig.MCP.Mounts, 2, "Should preserve custom mount and add safeoutputs mount")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "/custom:/mount:ro", "Custom mount should be preserved")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, safeOutputsMount, "Safeoutputs mount should be present")
			},
		},
		{
			name: "fills in missing payloadDir field",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container: "custom-container",
						Version:   "v1.0.0",
						Port:      8080,
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, constants.DefaultMCPGatewayPayloadDir, wd.SandboxConfig.MCP.PayloadDir, "PayloadDir should be filled with default")
			},
		},
		{
			name: "preserves custom payloadDir",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container:  "custom-container",
						Version:    "v1.0.0",
						Port:       8080,
						PayloadDir: "/custom/payloads",
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, "/custom/payloads", wd.SandboxConfig.MCP.PayloadDir, "Custom payloadDir should be preserved")
			},
		},
		{
			name: "preserves payloadPathPrefix when specified",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container:         "custom-container",
						Version:           "v1.0.0",
						Port:              8080,
						PayloadPathPrefix: "/workspace/payloads",
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, "/workspace/payloads", wd.SandboxConfig.MCP.PayloadPathPrefix, "PayloadPathPrefix should be preserved")
			},
		},
		{
			name: "preserves payloadSizeThreshold when specified",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Container:            "custom-container",
						Version:              "v1.0.0",
						Port:                 8080,
						PayloadSizeThreshold: 1048576, // 1MB
					},
				},
			},
			validate: func(t *testing.T, wd *WorkflowData) {
				assert.Equal(t, 1048576, wd.SandboxConfig.MCP.PayloadSizeThreshold, "PayloadSizeThreshold should be preserved")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ensureDefaultMCPGatewayConfig(tt.workflowData)
			tt.validate(t, tt.workflowData)
		})
	}
}

func TestBuildMCPGatewayConfig(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     *MCPGatewayRuntimeConfig
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			expected:     nil,
		},
		{
			name: "agent sandbox disabled - MCP gateway still enabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name:         "creates default gateway config",
			workflowData: &WorkflowData{},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "with sandbox enabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: false,
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "with custom payloadPathPrefix",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						PayloadPathPrefix: "/workspace/payloads",
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadPathPrefix:    "/workspace/payloads",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "with custom payloadSizeThreshold",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						PayloadSizeThreshold: 1048576, // 1MB
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: 1048576,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "uses default payloadSizeThreshold when not specified",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						// PayloadSizeThreshold not specified
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "propagates trustedBots from frontmatter config",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						TrustedBots: []string{"github-actions[bot]", "copilot-swe-agent[bot]"},
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				TrustedBots:          []string{"github-actions[bot]", "copilot-swe-agent[bot]"},
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "propagates custom keepaliveInterval from frontmatter config",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						KeepaliveInterval: 300,
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				KeepaliveInterval:    300,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "propagates disabled keepaliveInterval (-1) from frontmatter config",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						KeepaliveInterval: -1,
					},
				},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				KeepaliveInterval:    -1,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "propagates session-timeout from engine config",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:                "copilot",
					MCPSessionTimeout: "4h",
				},
				SandboxConfig: &SandboxConfig{},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				SessionTimeout:       "4h",
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "empty session-timeout when engine config is nil",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				SessionTimeout:       "",
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "propagates tool-timeout from engine config",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:             "copilot",
					MCPToolTimeout: "2m",
				},
				SandboxConfig: &SandboxConfig{},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				ToolTimeout:          "2m",
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "propagates both session-timeout and tool-timeout from engine config",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:                "copilot",
					MCPSessionTimeout: "4h",
					MCPToolTimeout:    "5m",
				},
				SandboxConfig: &SandboxConfig{},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				SessionTimeout:       "4h",
				ToolTimeout:          "5m",
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
		{
			name: "uses tools.startup-timeout when specified as integer",
			workflowData: &WorkflowData{
				ToolsStartupTimeout: "180",
				SandboxConfig:       &SandboxConfig{},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       180,
			},
		},
		{
			name: "falls back to default startupTimeout for GitHub Actions expression",
			workflowData: &WorkflowData{
				ToolsStartupTimeout: "${{ inputs.startup-timeout }}",
				SandboxConfig:       &SandboxConfig{},
			},
			expected: &MCPGatewayRuntimeConfig{
				Port:                 int(DefaultMCPGatewayPort),
				Domain:               "${MCP_GATEWAY_DOMAIN}",
				AgentID:              "${MCP_GATEWAY_AGENT_ID}",
				PayloadDir:           "${MCP_GATEWAY_PAYLOAD_DIR}",
				PayloadSizeThreshold: constants.DefaultMCPGatewayPayloadSizeThreshold,
				StartupTimeout:       int(constants.DefaultMCPStartupTimeout / time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMCPGatewayConfig(tt.workflowData)
			if tt.expected == nil {
				assert.Nil(t, result, "buildMCPGatewayConfig should return nil")
			} else {
				require.NotNil(t, result, "buildMCPGatewayConfig should return config")
				assert.Equal(t, tt.expected.Port, result.Port, "Port should match")
				assert.Equal(t, tt.expected.Domain, result.Domain, "Domain should match")
				assert.Equal(t, tt.expected.AgentID, result.AgentID, "AgentID should match")
				assert.Equal(t, tt.expected.PayloadDir, result.PayloadDir, "PayloadDir should match")
				assert.Equal(t, tt.expected.PayloadPathPrefix, result.PayloadPathPrefix, "PayloadPathPrefix should match")
				assert.Equal(t, tt.expected.PayloadSizeThreshold, result.PayloadSizeThreshold, "PayloadSizeThreshold should match")
				assert.Equal(t, tt.expected.TrustedBots, result.TrustedBots, "TrustedBots should match")
				assert.Equal(t, tt.expected.KeepaliveInterval, result.KeepaliveInterval, "KeepaliveInterval should match")
				assert.Equal(t, tt.expected.SessionTimeout, result.SessionTimeout, "SessionTimeout should match")
				assert.Equal(t, tt.expected.ToolTimeout, result.ToolTimeout, "ToolTimeout should match")
				assert.Equal(t, tt.expected.StartupTimeout, result.StartupTimeout, "StartupTimeout should match")
			}
		})
	}
}

func TestIsSandboxDisabled(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     bool
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			expected:     false,
		},
		{
			name:         "nil sandbox config",
			workflowData: &WorkflowData{},
			expected:     false,
		},
		{
			name: "agent sandbox disabled - isSandboxDisabled always returns false (deprecated)",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expected: false, // isSandboxDisabled() always returns false now (deprecated)
		},
		{
			name: "sandbox enabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: false,
					},
				},
			},
			expected: false,
		},
		{
			name: "nil agent config",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSandboxDisabled(tt.workflowData)
			assert.Equal(t, tt.expected, result, "isSandboxDisabled result should match expected")
		})
	}
}

func TestIsAgentSandboxDisabled(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     bool
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			expected:     false,
		},
		{
			name:         "nil sandbox config",
			workflowData: &WorkflowData{},
			expected:     false,
		},
		{
			name: "agent sandbox disabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expected: true,
		},
		{
			name: "agent sandbox enabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: false,
					},
				},
			},
			expected: false,
		},
		{
			name: "nil agent config",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAgentSandboxDisabled(tt.workflowData)
			assert.Equal(t, tt.expected, result, "isAgentSandboxDisabled result should match expected")
		})
	}
}
