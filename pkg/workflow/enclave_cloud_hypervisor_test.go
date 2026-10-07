//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnclaveCloudHypervisorAWFConfig(t *testing.T) {
	data := enclaveWorkflowData(true, true, 45, 180)
	for _, enclave := range data.Enclaves {
		enclave.Runtime = "cloud-hypervisor"
	}
	config := AWFCommandConfig{EngineName: "copilot", WorkflowData: data, UsesTTY: true}
	configJSON, err := BuildAWFConfigJSON(config)
	require.NoError(t, err)
	require.NoError(t, validateAWFConfigJSON(configJSON))

	var awf AWFConfigFile
	require.NoError(t, json.Unmarshal([]byte(configJSON), &awf))
	require.NotNil(t, awf.CloudHypervisor)
	assert.True(t, awf.CloudHypervisor.PreviewEnabled)
	assert.Equal(t, "workspace-only", awf.CloudHypervisor.MountPolicy)
	assert.Equal(t, "docker", awf.Container.ContainerRuntime)
	for _, enclave := range awf.Enclaves {
		assert.Equal(t, "cloud-hypervisor", enclave["runtime"])
	}
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_BINARY}", awf.CloudHypervisor.CloudHypervisorBinary)
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_KERNEL}", awf.CloudHypervisor.KernelPath)
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_ROOTFS}", awf.CloudHypervisor.RootfsPath)
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_SUPERVISOR}", awf.CloudHypervisor.SupervisorPath)
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST}", awf.CloudHypervisor.ArtifactManifestPath)
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST_BUNDLE}", awf.CloudHypervisor.ArtifactManifestBundlePath)
	assert.Equal(t, "${GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG}", awf.CloudHypervisor.ArtifactReleaseTag)

	args := strings.Join(BuildAWFArgs(config), " ")
	assert.NotContains(t, args, "--container-runtime cloud-hypervisor")
	assert.Contains(t, args, "--tty")
	assert.Contains(t, args, "--mount")
	assert.Equal(t, "sudo --preserve-env awf", GetAWFCommandPrefix(data))
	assert.NotContains(t, strings.Join(generateAWFInstallationStepForWorkflow("v0.28.47", data), "\n"), "--rootless")
	assert.NotContains(t, strings.Join(generateFirewallLogParsingStep("enclave", data), "\n"), "--rootless")
	assert.NotContains(t, args, "--legacy-security")
	assert.NotContains(t, args, "--enable-host-access")
	excluded := ComputeAWFExcludeEnvVarNames(data, nil)
	for _, name := range []string{
		"AWF_CLOUD_HYPERVISOR_ENCLAVE_SCRIPT_ROOTFS",
		"AWF_CLOUD_HYPERVISOR_ENCLAVE_AGENT_ROOTFS",
		"AWF_CLOUD_HYPERVISOR_ENCLAVE_MANIFEST",
		"AWF_CLOUD_HYPERVISOR_ENCLAVE_MANIFEST_BUNDLE",
	} {
		assert.Contains(t, excluded, name)
	}
}

func TestEnclaveCloudHypervisorConfigExpandsBundlePaths(t *testing.T) {
	data := enclaveWorkflowData(true, false, 45, 0)
	data.Enclaves[0].Runtime = "cloud-hypervisor"
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
	require.NoError(t, err)
	cmd := exec.Command("bash", "-c", "printf '%s' "+buildAWFConfigPrintfArg(configJSON, false))
	cmd.Env = append(os.Environ(),
		"GH_AW_CLOUD_HYPERVISOR_BINARY=/bundle/cloud-hypervisor",
		"GH_AW_CLOUD_HYPERVISOR_KERNEL=/bundle/vmlinux.bin",
		"GH_AW_CLOUD_HYPERVISOR_ROOTFS=/bundle/rootfs.ext4",
		"GH_AW_CLOUD_HYPERVISOR_SUPERVISOR=/bundle/awf-supervisor",
		"GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST=/bundle/manifest.json",
		"GH_AW_CLOUD_HYPERVISOR_ARTIFACT_MANIFEST_BUNDLE=/bundle/manifest.sigstore.jsonl",
		"GH_AW_CLOUD_HYPERVISOR_ARTIFACT_RELEASE_TAG=v0.28.47",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	var awf AWFConfigFile
	require.NoError(t, json.Unmarshal(output, &awf))
	assert.Equal(t, "/bundle/cloud-hypervisor", awf.CloudHypervisor.CloudHypervisorBinary)
	assert.Equal(t, "/bundle/vmlinux.bin", awf.CloudHypervisor.KernelPath)
	assert.Equal(t, "/bundle/rootfs.ext4", awf.CloudHypervisor.RootfsPath)
	assert.Equal(t, "/bundle/awf-supervisor", awf.CloudHypervisor.SupervisorPath)
	assert.Equal(t, "/bundle/manifest.json", awf.CloudHypervisor.ArtifactManifestPath)
	assert.Equal(t, "/bundle/manifest.sigstore.jsonl", awf.CloudHypervisor.ArtifactManifestBundlePath)
	assert.Equal(t, "v0.28.47", awf.CloudHypervisor.ArtifactReleaseTag)
}

func TestCompileEnclaveCloudHypervisor(t *testing.T) {
	for _, engine := range []string{"copilot", "codex", "claude"} {
		t.Run(engine, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "enclave.md")
			content := `---
on: workflow_dispatch
runs-on: ubuntu-24.04
strict: false
network: defaults
engine: ` + engine + `
sandbox:
  agent:
    id: awf
    version: v0.28.47
enclaves:
  - script:
    runtime: cloud-hypervisor
    repos:
      - repo: github/gh-aw
        sensitivity: internal
    max-invocations: 1
---
Run exactly one enclave script to build and test the repository.
`
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			require.NoError(t, compiler.CompileWorkflow(path))
			lockBytes, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
			require.NoError(t, err)
			lock := string(lockBytes)
			kvm := strings.Index(lock, "- name: Grant runner access to KVM")
			preflight := strings.Index(lock, "- name: Check host eligibility for cloud-hypervisor")
			bundle := strings.Index(lock, "- name: Download and verify cloud-hypervisor bundle")
			enclave := strings.Index(lock, "- name: Download and verify cloud-hypervisor enclave artifacts")
			gateway := strings.Index(lock, "- name: Start MCP Gateway")
			awf := strings.Index(lock, "awf --config")
			require.GreaterOrEqual(t, kvm, 0)
			assert.Less(t, kvm, preflight)
			assert.Less(t, preflight, bundle)
			assert.Less(t, bundle, enclave)
			assert.Less(t, enclave, awf)
			require.GreaterOrEqual(t, gateway, 0)
			assert.Less(t, gateway, awf)
			assert.Equal(t, 1, strings.Count(lock, "- name: Download and verify cloud-hypervisor bundle"))
			assert.Contains(t, lock, "GH_AW_AWF_VERSION: v0.28.47")
			assert.Contains(t, lock, "GH_AW_AWF_VERSION: ${{ steps.cloud-hypervisor-bundle.outputs.release_tag }}")
			assert.Contains(t, lock, `\"containerRuntime\":\"docker\"`)
			assert.Contains(t, lock, `\"mountPolicy\":\"workspace-only\"`)
			assert.Contains(t, lock, `\"runtime\":\"cloud-hypervisor\"`)
			assert.Contains(t, lock, `"awf-enclave"`)
			assert.Contains(t, lock, "sudo --preserve-env awf --config")
			assert.NotContains(t, lock, `install_awf_binary.sh" v0.28.47 --rootless`)
			assert.Contains(t, lock, "--exclude-env AWF_CLOUD_HYPERVISOR_ENCLAVE_SCRIPT_ROOTFS")
			assert.NotContains(t, lock, "--container-runtime cloud-hypervisor")
		})
	}
}

func TestCloudHypervisorEnclaveArtifactInstaller(t *testing.T) {
	script, err := filepath.Abs("../../actions/setup/sh/cloud_hypervisor_setup_enclave_artifacts.sh")
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		version string
		fail    bool
	}{
		{"success", "v0.28.47", false},
		{"invalid release", "../latest", true},
		{"verification failure", "v0.28.47", true},
		{"substituted installer", "v0.28.47", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			envFile := filepath.Join(dir, "github-env")
			gh := `#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == attestation ]]; then
  [[ "${FAIL_VERIFY:-}" != true ]]
  exit
fi
[[ "$*" == *"release download v0.28.47 --repo github/gh-aw-firewall"* ]]
while [[ "$1" != --dir ]]; do shift; done
mkdir -p "$2"
printf '%s\n' '{"release":{"repository":"github/gh-aw-firewall","tag":"v0.28.47","sourceCommit":"0123456789012345678901234567890123456789"}}' > "$2/cloud-hypervisor-enclave-rootfs-x86_64.manifest.json"
touch "$2/cloud-hypervisor-enclave-rootfs-x86_64.manifest.sigstore.jsonl"
cat > "$2/setup-cloud-hypervisor-enclave-artifacts.sh" <<'INSTALLER'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == v0.28.47 ]]
[[ "$2" == "${RUNNER_TEMP}/gh-aw/cloud-hypervisor-enclaves" ]]
[[ "${FAIL_VERIFY:-}" != true ]] || exit 1
for name in SCRIPT_ROOTFS AGENT_ROOTFS MANIFEST MANIFEST_BUNDLE; do
  printf 'AWF_CLOUD_HYPERVISOR_ENCLAVE_%s=%s/%s\n' "$name" "$2" "$name" >> "$GITHUB_ENV"
done
INSTALLER
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o700))
			curl := `#!/usr/bin/env bash
set -euo pipefail
[[ "$2" == "https://raw.githubusercontent.com/github/gh-aw-firewall/0123456789012345678901234567890123456789/guest/cloud-hypervisor/setup-enclave-artifacts.sh" ]]
cp "${RUNNER_TEMP}/gh-aw/cloud-hypervisor-enclave-setup/setup-cloud-hypervisor-enclave-artifacts.sh" "$4"
if [[ "${SUBSTITUTE_INSTALLER:-}" == true ]]; then
  echo '# substituted bytes' >> "$4"
fi
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "curl"), []byte(curl), 0o700))
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"),
				"RUNNER_TEMP="+dir, "GITHUB_ENV="+envFile, "GH_AW_AWF_VERSION="+test.version)
			if test.name == "verification failure" {
				cmd.Env = append(cmd.Env, "FAIL_VERIFY=true")
			}
			if test.name == "substituted installer" {
				cmd.Env = append(cmd.Env, "SUBSTITUTE_INSTALLER=true")
			}
			output, err := cmd.CombinedOutput()
			if test.fail {
				require.Error(t, err, string(output))
				_, err = os.Stat(envFile)
				assert.True(t, os.IsNotExist(err))
				return
			}
			require.NoError(t, err, string(output))
			env, err := os.ReadFile(envFile)
			require.NoError(t, err)
			for _, name := range []string{"SCRIPT_ROOTFS", "AGENT_ROOTFS", "MANIFEST", "MANIFEST_BUNDLE"} {
				assert.Contains(t, string(env), "AWF_CLOUD_HYPERVISOR_ENCLAVE_"+name+"="+dir)
			}
		})
	}
}
