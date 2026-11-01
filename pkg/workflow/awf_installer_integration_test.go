//go:build !integration

package workflow

import (
	"os/exec"
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
				"3929c9a15c0f239b772affcaae6e55fec92f41c2/install.sh",
				"installer_sha256=a6f844a1f1891bc4ddbb4b95c7d5cfd994e4f61aa1210ad655b36e92128b1202",
				`verify_release_digest "$install_dir/install.sh" "$installer_sha256"`,
				`verify_release_identity "$install_dir/checksums.txt" "$install_dir/release-identity.txt"`,
				"sudo -n --preserve-env=GH_TOKEN,GH_HOST /usr/bin/env AWF_FORCE_BINARY=1",
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
			for _, forbidden := range []string{"sudo -E", "sudo mount", "sudo chown", "NOPASSWD: ALL", "curl |", "| bash", "--rootless"} {
				if strings.Contains(text, forbidden) {
					t.Errorf("helper setup must not grant broad privileges: %s", forbidden)
				}
			}
			if strings.Index(text, "verify_release_identity") > strings.Index(text, "sudo -n --preserve-env") {
				t.Fatal("release authentication must precede privilege escalation")
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
