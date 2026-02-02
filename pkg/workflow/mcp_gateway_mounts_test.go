//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPGatewayDefaultMounts tests that default mounts are added to the gateway configuration
func TestMCPGatewayDefaultMounts(t *testing.T) {
	workflowData := &WorkflowData{}

	// Ensure default MCP gateway config is set
	ensureDefaultMCPGatewayConfig(workflowData)

	// Verify that default mounts are set
	require.NotNil(t, workflowData.SandboxConfig, "SandboxConfig should not be nil")
	require.NotNil(t, workflowData.SandboxConfig.MCP, "MCP gateway config should not be nil")
	require.NotEmpty(t, workflowData.SandboxConfig.MCP.Mounts, "Default mounts should be set")

	// Check that the default mounts include the expected values
	expectedMounts := []string{
		"/opt:/opt:ro",
		"/tmp:/tmp:rw",
		"${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw",
	}
	assert.Equal(t, expectedMounts, workflowData.SandboxConfig.MCP.Mounts, "Default mounts should match expected values")
}

// TestMCPGatewayCustomMounts tests that custom mounts can override defaults
func TestMCPGatewayCustomMounts(t *testing.T) {
	customMounts := []string{
		"/custom/path:/container/path:ro",
		"/data:/data:rw",
	}

	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			MCP: &MCPGatewayRuntimeConfig{
				Mounts: customMounts,
			},
		},
	}

	// Ensure default MCP gateway config is set (should not override custom mounts)
	ensureDefaultMCPGatewayConfig(workflowData)

	// Verify that custom mounts are preserved
	require.NotNil(t, workflowData.SandboxConfig, "SandboxConfig should not be nil")
	require.NotNil(t, workflowData.SandboxConfig.MCP, "MCP gateway config should not be nil")
	require.Equal(t, customMounts, workflowData.SandboxConfig.MCP.Mounts, "Custom mounts should be preserved")
}

// TestMCPGatewayMountsInDockerCommand tests the docker command generation with mounts
func TestMCPGatewayMountsInDockerCommand(t *testing.T) {
	tests := []struct {
		name          string
		mounts        []string
		expectedInCmd []string
	}{
		{
			name: "default mounts",
			mounts: []string{
				"/opt:/opt:ro",
				"/tmp:/tmp:rw",
				"${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw",
			},
			expectedInCmd: []string{
				"-v /opt:/opt:ro",
				"-v /tmp:/tmp:rw",
				"-v ${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw",
			},
		},
		{
			name: "custom mounts with spaces",
			mounts: []string{
				"/path with spaces:/container:ro",
			},
			expectedInCmd: []string{
				"-v /path with spaces:/container:ro",
			},
		},
		{
			name:          "no mounts",
			mounts:        []string{},
			expectedInCmd: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Container: constants.DefaultMCPGatewayContainer,
				Version:   string(constants.DefaultMCPGatewayVersion),
				Mounts:    tt.mounts,
			}

			// Build the container command (similar to what's done in mcp_servers.go)
			containerImage := gatewayConfig.Container + ":" + gatewayConfig.Version
			containerCmd := "docker run -i --rm --network host"

			// Add volume mounts (not individually quoted since entire command will be quoted)
			if len(gatewayConfig.Mounts) > 0 {
				for _, mount := range gatewayConfig.Mounts {
					containerCmd += " -v " + mount
				}
			}

			containerCmd += " " + containerImage

			// Verify that expected mount flags are in the command
			if len(tt.expectedInCmd) == 0 {
				// If no mounts expected, verify no -v flags are present (except in the image name)
				beforeImage := strings.Split(containerCmd, containerImage)[0]
				assert.NotContains(t, beforeImage, " -v ", "Should not contain volume mount flags")
			} else {
				for _, expected := range tt.expectedInCmd {
					assert.Contains(t, containerCmd, expected, "Command should contain expected mount flag")
				}
			}
		})
	}
}
