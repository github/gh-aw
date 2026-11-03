//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDockerEnclaveSetupPreservesLegacyConfig(t *testing.T) {
	for _, version := range []string{"v0.28.14", "v0.28.20"} {
		t.Run(version, func(t *testing.T) {
			data := enclaveWorkflowData(false, true, 0, 180)
			data.SandboxConfig.Agent.Version = version
			data.NetworkPermissions.Firewall.Version = version
			require.NoError(t, validateDefaultDockerEnclaveImagePins(data))
			config, err := BuildAWFConfigJSON(AWFCommandConfig{WorkflowData: data, EngineName: "copilot"})
			require.NoError(t, err)
			require.NotContains(t, config, `"enclaveDebugPublic"`)
			steps := strings.Join(generateAWFInstallationStepForWorkflow(version, data), "\n")
			require.NotContains(t, steps, "Docker enclave storage helper")
			require.Contains(t, steps, "--rootless")
		})
	}
}
