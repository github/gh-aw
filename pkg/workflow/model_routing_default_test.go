package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileDefaultModelRouting_CloudHypervisor covers the implicit default on
// the cloud-hypervisor runtime at and below the AWF routing support boundary.
func TestCompileDefaultModelRouting_CloudHypervisor(t *testing.T) {
	t.Run("A1 default AWF routes", func(t *testing.T) {
		compiled := compileDefaultRoutingSource(t, routingRuntimeFixture("none", "sandbox:\n  agent:\n    runtime: cloud-hypervisor\n"))
		require.NoError(t, compiled.err)
		require.Zero(t, compiled.warnings, "the implicit default must not add warnings")
		require.Equal(t, 1, strings.Count(compiled.stderr, defaultRoutingNoticeMarker), "one informational notice per workflow")
		require.True(t, isModelRoutingImplicit(compiled.data))
		require.Equal(t, "cost", compiled.data.EngineConfig.ModelRouting.Goal)
		require.Equal(t, "auto", compiled.data.EngineConfig.ModelRouting.Mode)

		configs := compiled.awfConfigs(t)
		require.Len(t, configs, 2, "runtime-guarded routing writes a routed and a non-routed AWF config")
		routed := configs[0]
		require.Equal(t, true, routed["experimental"].(map[string]any)["modelRouting"])
		routing := routed["apiProxy"].(map[string]any)["routing"].(map[string]any)
		require.Equal(t, map[string]any{"goal": "cost", "mode": "auto"}, routing["objective"])
		routerImage, ok := routed["container"].(map[string]any)["images"].(map[string]any)[awfImageRoleRouter].(string)
		require.True(t, ok, "routed config must pin the router image")
		require.Contains(t, routerImage, "@sha256:")
		require.NotContains(t, configs[1], "experimental")

		require.Contains(t, compiled.lock, "--container-runtime cloud-hypervisor")
		require.Contains(t, compiled.lock, "id: "+modelRoutingDecisionStepID, "model variables still bypass routing at run time")
		require.Contains(t, compiled.lock, "Prepare model-routing conversation")
	})

	t.Run("v0.29.12 boundary routes", func(t *testing.T) {
		compiled := compileDefaultRoutingSource(t, routingRuntimeFixture("none", "sandbox:\n  agent:\n    runtime: cloud-hypervisor\n    version: v0.29.12\n"))
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingImplicit(compiled.data))
	})

	t.Run("A2 v0.29.11 stays non-routed", func(t *testing.T) {
		compiled := compileDefaultRoutingSource(t, routingRuntimeFixture("none", "sandbox:\n  agent:\n    runtime: cloud-hypervisor\n    version: v0.29.11\n"))
		requireNoDefaultRouting(t, compiled)
		require.Zero(t, compiled.warnings)
		require.Contains(t, compiled.lock, "--container-runtime cloud-hypervisor")
	})
}

// TestCompileDefaultModelRouting_RuntimeIncompatibleUnchanged covers C2-C4:
// a runtime, topology, mount, or host that AWF cannot route silently keeps the
// non-routed behavior, with or without the explicit opt-out.
func TestCompileDefaultModelRouting_RuntimeIncompatibleUnchanged(t *testing.T) {
	for name, extra := range map[string]string{
		"nvx":                   routingNVXSandbox,
		"arc-dind":              "runner:\n  topology: arc-dind\n",
		"unmasked socket bind":  "sandbox:\n  agent:\n    mounts: [\"/var/run/docker.sock:/docker.sock:rw\"]\n",
		"parent directory bind": "sandbox:\n  agent:\n    mounts: [\"/var/run:/docker-run:ro\"]\n",
		"native windows host":   "runs-on: [self-hosted, windows, x64]\n",
		"object-form windows":   "runs-on:\n  labels: [self-hosted, windows, x64]\n",
		"old cloud-hypervisor":  "sandbox:\n  agent:\n    runtime: cloud-hypervisor\n    version: v0.29.11\n",
	} {
		for _, routing := range []string{"none", "off"} {
			t.Run(name+"/"+routing, func(t *testing.T) {
				compiled := compileDefaultRoutingSource(t, routingRuntimeFixture(routing, extra))
				require.NoError(t, compiled.err)
				requireNoDefaultRouting(t, compiled)
			})
		}
	}

	for name, extra := range map[string]string{
		"masked socket bind":  "sandbox:\n  agent:\n    mounts: [\"/var/run/docker.sock:/var/run/docker.sock:rw\"]\n",
		"safe unrelated bind": "sandbox:\n  agent:\n    mounts: [\"/opt/tools:/opt/tools:ro\"]\n",
		"object-form group":   "runs-on:\n  group: windows-runners\n",
	} {
		t.Run(name+" keeps the default", func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, routingRuntimeFixture("none", extra))
			require.NoError(t, compiled.err)
			require.True(t, isModelRoutingImplicit(compiled.data))
		})
	}

	t.Run("legacy-security runtime stays excluded", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig:       &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
			SandboxConfig:      &SandboxConfig{Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables}},
		}
		require.Nil(t, modelRoutingRuntimeCompatibility(data), "AWF routes the runc legacy-security runtime explicitly")
		require.NotEmpty(t, defaultModelRoutingIneligibility(data))
	})
}

// TestModelRoutingDockerSocketExposingMount follows AWF's
// findDockerSocketExposingMount masking semantics for emitted custom mounts.
func TestModelRoutingDockerSocketExposingMount(t *testing.T) {
	for mount, exposed := range map[string]bool{
		"/var/run/docker.sock:/docker.sock:rw":         true,
		"/var/run/docker.sock:/var/run/docker.sock:rw": false,
		"/run/docker.sock:/var/run/docker.sock:rw":     false,
		"/var/run/docker.sock:/run/docker.sock:ro":     false,
		"/var/run:/docker-run:ro":                      true,
		"/var/run/:/docker-run:ro":                     true,
		"/var/run:/var/run:ro":                         false,
		"/run:/host-run:ro":                            true,
		"/var:/var:ro":                                 false,
		"/var:/data:ro":                                true,
		"/:/:ro":                                       false,
		"/:/rootfs:ro":                                 true,
		"/opt/tools:/opt/tools:ro":                     false,
		"/var/run-other:/x:ro":                         false,
		"${{ vars.DIR }}:/x:ro":                        false,
	} {
		t.Run(mount, func(t *testing.T) {
			agent := &AgentSandboxConfig{Mounts: []string{mount}}
			data := &WorkflowData{SandboxConfig: &SandboxConfig{Agent: agent}}
			got := modelRoutingDockerSocketExposingMount(data, agent)
			if exposed {
				require.Equal(t, mount, got)
			} else {
				require.Empty(t, got)
			}
		})
	}

	t.Run("mounts are not emitted for cloud-hypervisor", func(t *testing.T) {
		agent := &AgentSandboxConfig{Runtime: AgentRuntimeCloudHypervisor, Mounts: []string{"/var/run:/docker-run:ro"}}
		data := &WorkflowData{SandboxConfig: &SandboxConfig{Agent: agent}}
		require.Empty(t, modelRoutingDockerSocketExposingMount(data, agent))
	})
}

// TestNativeNonLinuxRunsOnLabels checks that only provably native Windows or
// macOS runners are treated as non-Linux AWF hosts.
func TestNativeNonLinuxRunsOnLabels(t *testing.T) {
	for runsOn, native := range map[string]bool{
		"":                           false,
		"runs-on: ubuntu-latest":     false,
		"runs-on: windows-latest":    false,
		"runs-on: windows-2022":      true,
		"runs-on: macos-14":          true,
		"runs-on:\n- windows-latest": true,
		"runs-on:\n- self-hosted\n- windows\n- x64":                      true,
		"runs-on:\n- self-hosted\n- macOS":                               true,
		"runs-on:\n- self-hosted\n- linux\n- windows":                    false,
		"runs-on:\n- self-hosted\n- win-builders":                        false,
		"runs-on:\n- self-hosted\n- windows-2022":                        false,
		"runs-on:\n  group: windows":                                     false,
		"runs-on:\n  labels: [self-hosted, windows, x64]":                true,
		"runs-on:\n  group: builders\n  labels: [self-hosted, linux]":    false,
		"runs-on:\n  group: windows\n  labels: [\"${{ vars.RUNNER }}\"]": false,
		"runs-on: ${{ vars.RUNNER }}":                                    false,
		"runs-on: windows-builders":                                      false,
	} {
		t.Run(runsOn, func(t *testing.T) {
			_, got := nativeNonLinuxRunsOnLabels(runsOn)
			require.Equal(t, native, got)
		})
	}
}
