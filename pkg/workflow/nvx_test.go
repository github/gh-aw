//go:build !integration

package workflow

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validNVXWorkflowData() *WorkflowData {
	return &WorkflowData{
		EngineConfig: &EngineConfig{ID: "copilot"},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Version: "v0.28.49"},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeNVX,
				NVX: &AgentNVXConfig{
					PreviewEnabled:             true,
					NetworkIsolation:           true,
					APIProxy:                   true,
					LayerPath:                  "${{ runner.temp }}/nvx/guest-layer",
					OpenVMMPath:                "${{ runner.temp }}/nvx/openvmm",
					KernelPath:                 "${{ runner.temp }}/nvx/vmlinux",
					InitramfsPath:              "${{ runner.temp }}/nvx/initramfs.cpio.gz",
					ArtifactManifestPath:       "${{ runner.temp }}/nvx/nvx-test-x86_64.manifest.json",
					ArtifactManifestBundlePath: "${{ runner.temp }}/nvx/nvx-test-x86_64.manifest.sigstore.jsonl",
					ContainerWorkDir:           "/workspace/build",
				},
			},
		},
	}
}

func TestValidateNVXRuntimeConfig(t *testing.T) {
	t.Run("accepts expression-valued paths and defaults", func(t *testing.T) {
		data := validNVXWorkflowData()
		require.NoError(t, validateNVXRuntimeConfig(data, data.SandboxConfig.Agent))

		effective := effectiveNVXConfig(data.SandboxConfig.Agent.NVX)
		assert.Equal(t, defaultNVXMountPolicy, effective.MountPolicy)
		assert.Equal(t, defaultNVXSignerWorkflow, effective.SignerWorkflow)
		assert.Equal(t, 512, effective.MemoryMiB)
		assert.Equal(t, int64(536870912), effective.MemoryMaxBytes)
		assert.Equal(t, 128, effective.PidsMax)
		assert.Equal(t, "/workspace/build", effective.ContainerWorkDir)
	})

	t.Run("rejects a relative path containing an expression", func(t *testing.T) {
		data := validNVXWorkflowData()
		data.SandboxConfig.Agent.NVX.LayerPath = "relative/${{ runner.temp }}/layer"

		err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "absolute or expression-valued")
	})

	t.Run("rejects unknown signer workflows", func(t *testing.T) {
		data := validNVXWorkflowData()
		data.SandboxConfig.Agent.NVX.SignerWorkflow = "attacker/repo/.github/workflows/release.yml"

		err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not trusted")
	})

	t.Run("rejects invalid mount policies", func(t *testing.T) {
		data := validNVXWorkflowData()
		data.SandboxConfig.Agent.NVX.MountPolicy = "workspace-and-home"

		err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mount policy")
	})

	t.Run("rejects memory ceilings below guest memory", func(t *testing.T) {
		data := validNVXWorkflowData()
		data.SandboxConfig.Agent.NVX.MemoryMiB = 1024
		data.SandboxConfig.Agent.NVX.MemoryMaxBytes = 536870912

		err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "memory ceiling")
	})

	t.Run("rejects working directories outside workspace", func(t *testing.T) {
		data := validNVXWorkflowData()
		data.SandboxConfig.Agent.NVX.ContainerWorkDir = "/tmp"

		err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inside /workspace")
	})

	t.Run("rejects unsupported AWF versions", func(t *testing.T) {
		data := validNVXWorkflowData()
		data.NetworkPermissions.Firewall.Version = "v0.28.48"

		err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "v0.28.49")
	})
}

func TestNVXRuntimeDefaultsWithoutConfiguration(t *testing.T) {
	data := validNVXWorkflowData()
	data.SandboxConfig.Agent.NVX = nil

	require.NoError(t, validateNVXRuntimeConfig(data, data.SandboxConfig.Agent))

	effective := effectiveNVXConfig(nil)
	assert.True(t, effective.PreviewEnabled)
	assert.True(t, effective.NetworkIsolation)
	assert.True(t, effective.APIProxy)
	assert.Equal(t, "${{ runner.temp }}/nvx/guest-layer", effective.LayerPath)
	assert.Equal(t, "${{ runner.temp }}/nvx/openvmm", effective.OpenVMMPath)
	assert.Equal(t, "${{ runner.temp }}/nvx/vmlinux", effective.KernelPath)
	assert.Equal(t, "${{ runner.temp }}/nvx/initramfs.cpio.gz", effective.InitramfsPath)
	assert.Equal(t, "${{ runner.temp }}/nvx/nvx-test-x86_64.manifest.json", effective.ArtifactManifestPath)
	assert.Equal(t, "${{ runner.temp }}/nvx/nvx-test-x86_64.manifest.sigstore.jsonl", effective.ArtifactManifestBundlePath)

	config := AWFCommandConfig{EngineName: "copilot", WorkflowData: data}
	command := BuildAWFCommand(config)
	assert.Contains(t, command, "--container-runtime nvx")
	assert.Contains(t, command, "--nvx-artifact-manifest-bundle")

	steps := generateNVXRuntimeSetupSteps(data)
	require.Len(t, steps, 1)
	step := strings.Join(steps[0], "\n")
	assert.Contains(t, step, `${{ runner.temp }}/nvx/guest-layer`)
	assert.Contains(t, step, `${{ runner.temp }}/nvx/nvx-test-x86_64.manifest.json`)

	cleanupStep := strings.Join(generateNVXRuntimeCleanupStep(data), "\n")
	assert.Contains(t, cleanupStep, "if: always()")
	assert.Contains(t, cleanupStep, `sudo -n "$sudo_bin" "$setfacl_bin" -m "u:${acl_uid}:${acl_permissions}" /dev/kvm`)
	assert.Contains(t, cleanupStep, `sudo -n "$sudo_bin" "$setfacl_bin" -x "u:${acl_uid}" /dev/kvm`)
	assert.Contains(t, cleanupStep, `sudo -n "$resolved_rm" -rf -- "$stage_dir"`)
	assert.Contains(t, cleanupStep, `^/tmp/gh-aw-nvx\.[[:alnum:]]{10}$`)

	var cleanupScript strings.Builder
	for _, line := range generateNVXRuntimeCleanupStep(data)[3:] {
		cleanupScript.WriteString(strings.TrimPrefix(line, "          "))
		cleanupScript.WriteByte('\n')
	}
	require.NoError(t, exec.Command("bash", "-n", "-c", cleanupScript.String()).Run())
}

func TestValidateNVXRuntimeIncompatibleFeatures(t *testing.T) {
	tests := []struct {
		name   string
		config func(*WorkflowData)
	}{
		{"ARC DinD", func(d *WorkflowData) { d.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind} }},
		{"containerized job", func(d *WorkflowData) { d.Container = "ubuntu:latest" }},
		{"interactive Claude", func(d *WorkflowData) { d.EngineConfig.ID = "claude" }},
		{"custom command", func(d *WorkflowData) { d.SandboxConfig.Agent.Command = "sh" }},
		{"custom arguments", func(d *WorkflowData) { d.SandboxConfig.Agent.Args = []string{"--unsafe"} }},
		{"custom environment", func(d *WorkflowData) { d.SandboxConfig.Agent.Env = map[string]string{"TOKEN": "secret"} }},
		{"extra host mounts", func(d *WorkflowData) { d.SandboxConfig.Agent.Mounts = []string{"/host:/guest:ro"} }},
		{"Docker memory limit", func(d *WorkflowData) { d.SandboxConfig.Agent.Memory = "1g" }},
		{"weaker nested sandbox", func(d *WorkflowData) {
			d.SandboxConfig.Agent.Config = &SandboxRuntimeConfig{EnableWeakerNestedSandbox: true}
		}},
		{"host ports", func(d *WorkflowData) { d.SandboxConfig.Agent.AllowHostPorts = []int{8080} }},
		{"service ports", func(d *WorkflowData) { d.ServicePortExpressions = "SERVICE_PORT: ${{ job.services.api.ports['80'] }}" }},
		{"primary-agent enclaves", func(d *WorkflowData) { d.Enclaves = EnclavesConfig{&EnclaveConfig{}} }},
		{"custom AWF arguments", func(d *WorkflowData) { d.NetworkPermissions.Firewall.Args = []string{"--enable-host-access"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validNVXWorkflowData()
			tt.config(data)

			err := validateNVXRuntimeConfig(data, data.SandboxConfig.Agent)
			require.Error(t, err)
		})
	}
}

func TestNVXAWFConfigAndCommand(t *testing.T) {
	data := validNVXWorkflowData()
	config := AWFCommandConfig{EngineName: "copilot", WorkflowData: data}

	configJSON, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	require.NoError(t, validateAWFConfigJSON(configJSON))

	var awfConfig struct {
		Container AWFContainerConfig `json:"container"`
		NVX       AWFNVXConfig       `json:"nvx"`
	}
	require.NoError(t, json.Unmarshal([]byte(configJSON), &awfConfig))
	assert.Equal(t, "nvx", awfConfig.Container.ContainerRuntime)
	assert.Equal(t, "/workspace/build", awfConfig.Container.ContainerWorkDir)
	assert.True(t, awfConfig.NVX.PreviewEnabled)
	assert.Equal(t, "workspace-only", awfConfig.NVX.MountPolicy)
	assert.Equal(t, defaultNVXSignerWorkflow, awfConfig.NVX.SignerWorkflow)
	assert.Equal(t, 512, awfConfig.NVX.MemoryMiB)
	assert.Equal(t, int64(536870912), awfConfig.NVX.MemoryMaxBytes)
	assert.Equal(t, 128, awfConfig.NVX.PidsMax)

	command := BuildAWFCommand(config)
	for _, expected := range []string{
		"--container-runtime nvx",
		"--nvx-preview",
		"--network-isolation",
		"--enable-api-proxy",
		"--nvx-layer \"${GH_AW_NVX_LAYER}\"",
		"--nvx-openvmm \"${GH_AW_NVX_OPENVMM}\"",
		"--nvx-kernel \"${GH_AW_NVX_KERNEL}\"",
		"--nvx-initramfs \"${GH_AW_NVX_INITRAMFS}\"",
		"--nvx-artifact-manifest \"${GH_AW_NVX_ARTIFACT_MANIFEST}\"",
		"--nvx-artifact-manifest-bundle \"${GH_AW_NVX_ARTIFACT_MANIFEST_BUNDLE}\"",
		"--container-workdir /workspace/build",
	} {
		assert.Contains(t, command, expected)
	}
	assert.NotContains(t, command, "--container-runtime docker")
	assert.NotContains(t, command, "--container-runtime cloud-hypervisor")
}

func TestNVXSetupStepUsesConfiguredExpressionPaths(t *testing.T) {
	data := validNVXWorkflowData()
	steps := generateNVXRuntimeSetupSteps(data)
	require.Len(t, steps, 1)

	step := strings.Join(steps[0], "\n")
	assert.Contains(t, step, "Verify and stage trusted NVX artifacts")
	assert.Contains(t, step, `${{ runner.temp }}/nvx/guest-layer`)
	assert.Contains(t, step, "gh-aw/actions/nvx_host_preflight.sh")
}
