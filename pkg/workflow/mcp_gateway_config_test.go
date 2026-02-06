//go:build !integration

package workflow

import (
	"testing"

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
				assert.Len(t, wd.SandboxConfig.MCP.Mounts, 3, "Should have 3 default mounts")
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
				assert.Len(t, wd.SandboxConfig.MCP.Mounts, 3, "Should have 3 default mounts")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "/opt:/opt:ro", "Should have /opt mount")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "/tmp:/tmp:rw", "Should have /tmp mount")
				assert.Contains(t, wd.SandboxConfig.MCP.Mounts, "${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw", "Should have GITHUB_WORKSPACE mount")
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
			name: "sandbox disabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
			},
			expected: nil,
		},
		{
			name:         "creates default gateway config",
			workflowData: &WorkflowData{},
			expected: &MCPGatewayRuntimeConfig{
				Port:       int(DefaultMCPGatewayPort),
				Domain:     "${MCP_GATEWAY_DOMAIN}",
				APIKey:     "${MCP_GATEWAY_API_KEY}",
				PayloadDir: "${MCP_GATEWAY_PAYLOAD_DIR}",
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
				Port:       int(DefaultMCPGatewayPort),
				Domain:     "${MCP_GATEWAY_DOMAIN}",
				APIKey:     "${MCP_GATEWAY_API_KEY}",
				PayloadDir: "${MCP_GATEWAY_PAYLOAD_DIR}",
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
				assert.Equal(t, tt.expected.APIKey, result.APIKey, "APIKey should match")
				assert.Equal(t, tt.expected.PayloadDir, result.PayloadDir, "PayloadDir should match")
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
			name: "sandbox disabled",
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
