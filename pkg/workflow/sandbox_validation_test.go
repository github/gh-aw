//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxTypeEnumValidation tests that sandbox type enum values are correctly validated
func TestSandboxTypeEnumValidation(t *testing.T) {
	tests := []struct {
		name        string
		sandboxType SandboxType
		expectValid bool
	}{
		// Valid enum values
		{
			name:        "valid type: awf",
			sandboxType: SandboxTypeAWF,
			expectValid: true,
		},
		{
			name:        "valid type: default (backward compat)",
			sandboxType: SandboxTypeDefault,
			expectValid: true,
		},
		// Invalid enum values
		{
			name:        "invalid type: AWF (uppercase)",
			sandboxType: "AWF",
			expectValid: false,
		},
		{
			name:        "invalid type: Default (mixed case)",
			sandboxType: "Default",
			expectValid: false,
		},
		{
			name:        "invalid type: empty string",
			sandboxType: "",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSupportedSandboxType(tt.sandboxType)
			if result != tt.expectValid {
				t.Errorf("isSupportedSandboxType(%q) = %v, want %v", tt.sandboxType, result, tt.expectValid)
			}
		})
	}
}

// TestSandboxTypeCaseSensitivity tests that sandbox types are case-sensitive
func TestSandboxTypeCaseSensitivity(t *testing.T) {
	caseSensitiveTests := []struct {
		name        string
		sandboxType SandboxType
		shouldMatch bool
	}{
		{name: "lowercase awf matches", sandboxType: "awf", shouldMatch: true},
		{name: "uppercase AWF does not match", sandboxType: "AWF", shouldMatch: false},
		{name: "mixed case Awf does not match", sandboxType: "Awf", shouldMatch: false},
		{name: "lowercase default matches", sandboxType: "default", shouldMatch: true},
		{name: "uppercase DEFAULT does not match", sandboxType: "DEFAULT", shouldMatch: false},
	}

	for _, tt := range caseSensitiveTests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSupportedSandboxType(tt.sandboxType)
			if result != tt.shouldMatch {
				t.Errorf("isSupportedSandboxType(%q) = %v, want %v", tt.sandboxType, result, tt.shouldMatch)
			}
		})
	}
}

// TestValidateSandboxConfigTrustBoundaryMessage tests that the compiler diagnostic
// says the sandbox removal is a trust boundary change, not just a validator check.
func TestValidateSandboxConfigTrustBoundaryMessage(t *testing.T) {
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Disabled: true},
		},
		// No Features — validation must fail
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "trust boundary", "diagnostic must say the sandbox removal removes a trust boundary")
	assert.Contains(t, errMsg, "dangerously-disable-sandbox-agent", "diagnostic must name the required feature flag")
}

func TestValidateSandboxConfigMCPEnvironmentVariableNames(t *testing.T) {
	t.Run("valid names pass validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Env: map[string]string{
						"API_TOKEN": "value",
						"_DEBUG":    "true",
					},
				},
			},
		}

		require.NoError(t, validateSandboxConfig(workflowData))
	})

	t.Run("invalid names fail validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Env: map[string]string{
						"BAD-NAME": "value",
					},
				},
			},
		}

		err := validateSandboxConfig(workflowData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox.mcp.env.BAD-NAME")
		assert.Contains(t, err.Error(), "^[A-Z_][A-Z0-9_]*$")
		assert.Contains(t, err.Error(), "API_TOKEN")
	})

	t.Run("reserved transport names fail validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Env: map[string]string{
						"GH_AW_MCP_GATEWAY_ENV_0": "value",
					},
				},
			},
		}

		err := validateSandboxConfig(workflowData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox.mcp.env.GH_AW_MCP_GATEWAY_ENV_0")
		assert.Contains(t, err.Error(), "reserved for internal transport")
		assert.Contains(t, err.Error(), "GH_AW_MCP_GATEWAY_")
	})
}

func TestValidateSandboxConfigRejectsCodexCopilotWithoutAgentSandbox(t *testing.T) {
	workflowData := &WorkflowData{
		Model:        "copilot/auto",
		EngineConfig: &EngineConfig{ID: "codex"},
		Features: map[string]any{
			"dangerously-disable-sandbox-agent": true,
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Disabled: true},
		},
	}

	err := validateSandboxConfig(workflowData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires the agent sandbox for BYOK inference routing")
}

func TestValidateAgentMemoryLimit(t *testing.T) {
	tests := []struct {
		name        string
		memory      string
		expectError bool
	}{
		{name: "valid: 48g", memory: "48g", expectError: false},
		{name: "valid: 512m", memory: "512m", expectError: false},
		{name: "valid: 8G uppercase", memory: "8G", expectError: false},
		{name: "valid: 1024k", memory: "1024k", expectError: false},
		{name: "invalid: no unit", memory: "48", expectError: true},
		{name: "invalid: gb suffix", memory: "48gb", expectError: true},
		{name: "invalid: leading zero", memory: "08g", expectError: true},
		{name: "invalid: zero", memory: "0m", expectError: true},
		{name: "invalid: empty", memory: "", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentMemoryLimit(tt.memory)
			if tt.expectError {
				require.Error(t, err, "expected validation error for memory %q", tt.memory)
			} else {
				require.NoError(t, err, "expected no error for memory %q", tt.memory)
			}
		})
	}
}

func TestValidateSandboxConfigMemory(t *testing.T) {
	t.Run("valid memory passes validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Memory: "4g"},
			},
		}
		err := validateSandboxConfig(workflowData)
		assert.NoError(t, err, "valid memory should pass validation")
	})

	t.Run("invalid memory format fails validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Memory: "48gb"},
			},
		}
		err := validateSandboxConfig(workflowData)
		require.Error(t, err, "invalid memory format should fail validation")
		assert.Contains(t, err.Error(), "48gb")
	})

	t.Run("leading zero memory format explains why it is invalid", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Memory: "08g"},
			},
		}
		err := validateSandboxConfig(workflowData)
		require.Error(t, err, "leading-zero memory format should fail validation")
		assert.Contains(t, err.Error(), "without leading zeros")
	})

	t.Run("absent memory skips validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{},
			},
		}
		err := validateSandboxConfig(workflowData)
		assert.NoError(t, err, "absent memory should pass validation")
	})
}

func TestValidateSandboxConfigAllowHostPorts(t *testing.T) {
	t.Run("valid allow-host-ports passes validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables, AllowHostPorts: []int{8081, 9000}},
			},
		}

		err := validateSandboxConfig(workflowData)
		assert.NoError(t, err, "valid allow-host-ports should pass validation")
	})

	t.Run("out-of-range allow-host-ports fails validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables, AllowHostPorts: []int{0}},
			},
		}

		err := validateSandboxConfig(workflowData)
		require.Error(t, err, "out-of-range allow-host-ports should fail validation")
		assert.Contains(t, err.Error(), "allow-host-ports value 0 is out of range")
		assert.Contains(t, err.Error(), "Example: allow-host-ports: [9000]")
	})

	t.Run("dangerous allow-host-ports fails validation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables, AllowHostPorts: []int{5432}},
			},
		}

		err := validateSandboxConfig(workflowData)
		require.Error(t, err, "a dangerous port should fail validation")
		assert.Contains(t, err.Error(), "allow-host-ports value 5432")
		assert.Contains(t, err.Error(), "PostgreSQL")
		assert.Contains(t, err.Error(), "services:")
		assert.Contains(t, err.Error(), string(AgentRuntimeDockerSudoIptables))
	})

	t.Run("allow-host-ports requires the docker-sudo-iptables runtime", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{AllowHostPorts: []int{9000}},
			},
		}

		err := validateSandboxConfig(workflowData)
		require.Error(t, err, "allow-host-ports on the default docker runtime should fail validation")
		assert.Contains(t, err.Error(), "allow-host-ports")
		assert.Contains(t, err.Error(), string(AgentRuntimeDockerSudoIptables))
	})
}

func TestValidateSandboxConfigDynamicEnclaveDelegationControlPort(t *testing.T) {
	t.Run("port leaving room for the control port passes validation", func(t *testing.T) {
		data := dynamicEnclaveWorkflowData()
		data.Tools = map[string]any{"github": map[string]any{"mode": "remote"}}
		data.SandboxConfig.MCP.Port = int(constants.MaxNetworkPort) - enclaveDelegationControlPortOffset
		require.NoError(t, validateSandboxConfig(data))
	})

	t.Run("port that overflows the derived control port fails validation", func(t *testing.T) {
		data := dynamicEnclaveWorkflowData()
		data.Tools = map[string]any{"github": map[string]any{"mode": "remote"}}
		data.SandboxConfig.MCP.Port = int(constants.MaxNetworkPort) - enclaveDelegationControlPortOffset + 1
		err := validateSandboxConfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox.mcp.port")
		assert.Contains(t, err.Error(), "dynamic enclave delegation control port")
	})

	t.Run("port near the max is fine without dynamic enclave delegation", func(t *testing.T) {
		data := &WorkflowData{
			Tools:         map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf"}, MCP: &MCPGatewayRuntimeConfig{Port: int(constants.MaxNetworkPort) - 1}},
		}
		require.NoError(t, validateSandboxConfig(data))
	})
}
