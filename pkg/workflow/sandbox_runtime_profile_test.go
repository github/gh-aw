//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxRuntimeProfiles verifies that every supported runtime resolves to exactly
// one profile and that the omitted runtime keeps the secure Docker default.
func TestSandboxRuntimeProfiles(t *testing.T) {
	t.Run("omitted runtime resolves to the docker profile", func(t *testing.T) {
		profile := resolveSandboxRuntimeProfile(&AgentSandboxConfig{ID: "awf"})

		assert.Equal(t, AgentRuntimeDocker, profile.Runtime)
		assert.True(t, profile.NetworkIsolation, "default profile must keep network isolation")
		assert.True(t, profile.Rootless, "default profile must run AWF rootless")
		assert.False(t, profile.LegacySecurity, "default profile must not enable legacy security")
		assert.False(t, profile.SupportsHostAccess, "default profile must not allow host access")
	})

	t.Run("every supported runtime has a profile", func(t *testing.T) {
		for _, runtime := range supportedAgentRuntimes {
			assert.True(t, isSupportedAgentRuntime(runtime), "runtime %q must be supported", runtime)

			profile := resolveSandboxRuntimeProfile(&AgentSandboxConfig{Runtime: runtime})
			assert.Equal(t, runtime, profile.Runtime, "runtime %q must resolve to its own profile", runtime)
			assert.True(t, profile.NetworkIsolation, "runtime %q must keep network isolation", runtime)
			assert.NotEmpty(t, profile.AWFCommand, "runtime %q must define an AWF command", runtime)
		}
	})

	t.Run("only docker-sudo-iptables is privileged with host access", func(t *testing.T) {
		profile := resolveSandboxRuntimeProfile(&AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables})

		assert.True(t, profile.LegacySecurity)
		assert.True(t, profile.SupportsHostAccess)
		assert.False(t, profile.Rootless)

		for _, runtime := range supportedAgentRuntimes {
			if runtime == AgentRuntimeDockerSudoIptables {
				continue
			}
			other := resolveSandboxRuntimeProfile(&AgentSandboxConfig{Runtime: runtime})
			assert.False(t, other.LegacySecurity, "runtime %q must not use legacy security", runtime)
			assert.False(t, other.SupportsHostAccess, "runtime %q must not allow host access", runtime)
		}
	})

	t.Run("unsupported runtime is rejected", func(t *testing.T) {
		assert.False(t, isSupportedAgentRuntime(AgentRuntime("podman")))
	})
}

func TestValidateSandboxRuntimeProfile(t *testing.T) {
	newWorkflowData := func(agent *AgentSandboxConfig) *WorkflowData {
		return &WorkflowData{
			Tools:         map[string]any{"github": map[string]any{"mode": "remote"}},
			SandboxConfig: &SandboxConfig{Agent: agent},
		}
	}

	t.Run("unsupported runtime fails compilation", func(t *testing.T) {
		err := validateSandboxConfig(newWorkflowData(&AgentSandboxConfig{ID: "awf", Runtime: AgentRuntime("podman")}))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported sandbox runtime")
		assert.Contains(t, err.Error(), string(AgentRuntimeDockerSudoIptables))
	})

	t.Run("services with published ports require docker-sudo-iptables", func(t *testing.T) {
		workflowData := newWorkflowData(&AgentSandboxConfig{ID: "awf"})
		workflowData.ServicePortExpressions = `POSTGRES_PORT: ${{ job.services.postgres.ports['5432'] }}`

		err := validateSandboxConfig(workflowData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "services")
		assert.Contains(t, err.Error(), string(AgentRuntimeDockerSudoIptables))
	})

	t.Run("services with published ports are allowed with docker-sudo-iptables", func(t *testing.T) {
		workflowData := newWorkflowData(&AgentSandboxConfig{ID: "awf", Runtime: AgentRuntimeDockerSudoIptables})
		workflowData.ServicePortExpressions = `POSTGRES_PORT: ${{ job.services.postgres.ports['5432'] }}`

		assert.NoError(t, validateSandboxConfig(workflowData))
	})
}
