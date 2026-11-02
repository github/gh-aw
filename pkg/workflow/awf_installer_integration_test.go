//go:build !integration

package workflow

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestAWFInstallerAuthenticationIsStepLocal(t *testing.T) {
	t.Parallel()
	data := &WorkflowData{SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF}}}
	steps := map[string]string{
		"agent":    strings.Join(generateAWFInstallationStepForWorkflow("", data), "\n"),
		"detector": strings.Join(NewCompiler().buildInstallAWFForExternalDetectorStep(data), ""),
	}
	for name, step := range steps {
		t.Run(name, func(t *testing.T) {
			for _, required := range []string{
				"        env:\n          GH_TOKEN: ${{ github.token }}\n          GH_HOST: github.com\n",
				"install_awf_binary.sh",
				"--rootless",
			} {
				if !strings.Contains(step, required) {
					t.Errorf("installer lacks %q:\n%s", required, step)
				}
			}
			if strings.Contains(step, "secrets.") || strings.Contains(step, "sudo -E") {
				t.Fatal("installer must use only its own public GitHub Actions token")
			}
		})
	}
}

func TestDockerEnclaveHelperInstallerContract(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"", "v0.29.5", "0.29.5"} {
		t.Run(version, func(t *testing.T) {
			pin := strings.Repeat("a", 64)
			step := generateDockerEnclaveStorageHelperInstallStep(version, &AgentSandboxConfig{ChecksumsSHA256: pin})
			text := strings.Join(step, "\n")
			for _, required := range []string{
				"GH_TOKEN: ${{ github.token }}", "GH_HOST: github.com",
				"AWF_HELPER_CHECKSUMS_SHA256: " + pin,
				`verify_release_identity "$install_dir/checksums.txt" "$install_dir/release-identity.txt"`,
				`"$AWF_HELPER_VERSION" "$checksums_sha256"`,
				`$2 == "enclave-storage-helper.py"`,
				`verify_release_attestation "$install_dir/enclave-storage-helper.py" github/gh-aw-firewall`,
				`verify_release_digest "$install_dir/enclave-storage-helper.py" "$helper_sha256"`,
				`/usr/bin/sudo -n /usr/bin/mktemp -d /var/lib/awf-helper-install.XXXXXX`,
				`/usr/bin/sudo -n /usr/bin/install -o root -g root -m 0500 -- "${RUNNER_TEMP}/gh-aw/actions/install_awf_enclave_helper.cjs"`,
				`"$installer_dir/node" --no-addons --no-global-search-paths "$installer_path"`,
				`[[ "${installer_sha256%% *}" == "` + dockerEnclaveHelperInstallerSHA + `" ]]`,
				`"$install_dir/checksums.txt" "$install_dir/enclave-storage-helper.py" "$checksums_sha256" "$helper_sha256"`,
				`/var/lib/awf/enclave-storage-helper.py probe`,
				"/usr/bin/python3 -I /var/lib/awf/enclave-storage-helper.py probe",
				`"maxBytes": 4 * 1024**3`,
				`"uid": os.getuid()`,
				`"stateRoot": "/var/lib/awf/enclave-storage/" + str(os.getuid()) + "/host-state"`,
			} {
				if !strings.Contains(text, required) {
					t.Errorf("helper installer lacks %q", required)
				}
			}
			expectedVersion := "v0.29.5"
			if version == "" {
				expectedVersion = string(constants.DefaultFirewallVersion)
			}
			if !strings.Contains(text, "AWF_HELPER_VERSION: "+expectedVersion) {
				t.Error("helper must use the orchestrator's pinned release")
			}
			for _, forbidden := range []string{
				"sudo -E", "sudo mount", "sudo chown", "NOPASSWD: ALL",
				"curl |", "| bash", "--rootless", "install.sh", "AWF_FORCE_BINARY",
				"<<'PY'",
			} {
				if strings.Contains(text, forbidden) {
					t.Errorf("helper setup must not grant broad privileges: %s", forbidden)
				}
			}
			manifestCheck := strings.Index(text, `"$install_dir/checksums.txt" "$install_dir/release-identity.txt"`)
			privilegedInstall := strings.Index(text, `"$install_dir/checksums.txt" "$install_dir/enclave-storage-helper.py" "$checksums_sha256" "$helper_sha256"`)
			protectedDigest := strings.Index(text, `[[ "${installer_sha256%% *}" == "`+dockerEnclaveHelperInstallerSHA+`" ]]`)
			if manifestCheck < 0 || privilegedInstall < 0 || manifestCheck > privilegedInstall {
				t.Fatal("the reviewed checksum manifest must be authenticated before privileged installation")
			}
			if protectedDigest < 0 || protectedDigest > privilegedInstall {
				t.Fatal("the root-owned installer copy must be verified before execution")
			}
			nodeDigest := strings.Index(text, `"${node_binary_digest%% *}" == "$node_binary_sha256"`)
			nodeExecution := strings.Index(text, `-- "$installer_dir/node" --no-addons`)
			if nodeDigest < 0 || nodeExecution < 0 || nodeDigest > nodeExecution {
				t.Fatal("the root-owned Node binary must be authenticated before execution")
			}
			var run []string
			for _, line := range step {
				if strings.HasPrefix(line, "          ") && len(run) > 0 {
					run = append(run, strings.TrimPrefix(line, "          "))
				}
				if line == "        run: |" {
					run = append(run, "")
				}
			}
			cmd := exec.Command("bash", "-n")
			cmd.Stdin = strings.NewReader(strings.Join(run, "\n"))
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("helper installer shell syntax: %v: %s", err, output)
			}
			installerPath := filepath.Join("..", "..", "actions", "setup", "js", "install_awf_enclave_helper.cjs")
			installer, err := os.ReadFile(installerPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(installer)); got != dockerEnclaveHelperInstallerSHA {
				t.Fatalf("installer pin = %s, want %s", got, dockerEnclaveHelperInstallerSHA)
			}
			if output, err := exec.Command("node", "--check", installerPath).CombinedOutput(); err != nil {
				t.Fatalf("helper installer JavaScript syntax: %v: %s", err, output)
			}
		})
	}
	if step := generateDockerEnclaveStorageHelperInstallStep("", &AgentSandboxConfig{Command: "custom-awf"}); len(step) != 0 {
		t.Fatal("custom AWF commands must not install an upstream helper")
	}
	for _, version := range []string{"v0.28.62", "v0.29.0", "v0.29.4", "0.29.4"} {
		if step := generateDockerEnclaveStorageHelperInstallStep(version, nil); len(step) != 0 {
			t.Errorf("legacy %s must not install a helper", version)
		}
	}
}

func TestDockerEnclaveInstallerWrapperKeepsOrchestratorRootless(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"", "v0.29.4", "v0.29.5"} {
		t.Run(version, func(t *testing.T) {
			data := enclaveWorkflowData(true, false, 30, 0)
			data.NetworkPermissions.Firewall.Version = version
			step := strings.Join(generateAWFInstallationStepForWorkflow(version, data), "\n")
			expectedVersion := version
			if expectedVersion == "" {
				expectedVersion = string(constants.DefaultFirewallVersion)
			}
			orchestrator := `bash "${RUNNER_TEMP}/gh-aw/actions/install_awf_binary.sh" ` + expectedVersion + " --rootless"
			if !strings.Contains(step, orchestrator) {
				t.Fatalf("Docker enclave orchestrator must remain rootless:\n%s", step)
			}
			helper := strings.Index(step, "Install authenticated Docker enclave storage helper")
			if version == "v0.29.4" {
				if helper != -1 {
					t.Fatal("legacy installs must retain pre-helper behavior")
				}
				return
			}
			if helper < 0 || helper > strings.Index(step, orchestrator) {
				t.Fatal("helper setup must precede rootless orchestrator installation")
			}
			if !strings.Contains(step, `/usr/bin/python3 -I /var/lib/awf/enclave-storage-helper.py probe "${AWF_HELPER_VERSION#v}"`) {
				t.Fatal("wrapper must probe the narrow version-matched storage helper")
			}
			if strings.Contains(step, "sudo -E") || strings.Contains(step, "sudo awf") {
				t.Fatal("helper setup must not escalate the orchestrator")
			}
		})
	}
}
