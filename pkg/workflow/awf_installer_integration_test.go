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

func TestAWFInstallerLockUsesBashOnLinuxAndWindows(t *testing.T) {
	t.Parallel()
	for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
		t.Run(runner, func(t *testing.T) {
			t.Parallel()
			checksum := strings.Repeat("b", 64)
			path := filepath.Join(t.TempDir(), "awf-install.md")
			content := fmt.Sprintf(`---
on: workflow_dispatch
strict: false
runs-on: %s
sandbox:
  agent:
    version: v0.28.62
    checksums-sha256: "%s"
---
Inspect the repository.
`, runner, checksum)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := NewCompiler(WithSkipValidation(true)).CompileWorkflow(path); err != nil {
				t.Fatal(err)
			}
			lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			if err != nil {
				t.Fatal(err)
			}
			lockText := string(lock)
			const installStepMarker = "      - name: Install AWF binary\n"
			installStepStart := strings.Index(lockText, installStepMarker)
			if installStepStart < 0 {
				t.Fatalf("generated %s lock file lacks the AWF install step:\n%s", runner, lock)
			}
			installStep := lockText[installStepStart:]
			stepLines := strings.Split(installStep, "\n")
			boundary := -1
			for i, line := range stepLines[1:] {
				if strings.HasPrefix(line, "      - ") {
					boundary = i + 1
					break
				}
				if strings.TrimSpace(line) == "" {
					continue
				}
				if len(line)-len(strings.TrimLeft(line, " ")) <= 4 {
					boundary = i + 1
					break
				}
			}
			if boundary < 0 {
				if !strings.HasSuffix(installStep, "\n") {
					t.Fatalf("generated %s lock file lacks a boundary after the AWF install step:\n%s", runner, installStep)
				}
				// The AWF step is the final step in the lock file.
				boundary = len(stepLines) - 1
			}
			installStep = strings.Join(stepLines[:boundary], "\n")
			expectedCommand := `        run: bash "${RUNNER_TEMP}/gh-aw/actions/install_awf_binary.sh" v0.28.62 --checksums-sha256 ` + checksum + ` --rootless`
			if runner == "windows-latest" {
				if !strings.Contains(installStep, "shell: pwsh") ||
					!strings.Contains(installStep, "wsl.exe --distribution Ubuntu --user root --exec bash --noprofile --norc -e -o pipefail") {
					t.Errorf("generated Windows install step does not launch Bash in WSL:\n%s", installStep)
				}
				windowsCommand := strings.ReplaceAll(expectedCommand, "/gh-aw/actions/", "/gh-aw-setup/actions/")
				if script := decodeWindowsWSLRunScript(t, installStep); !strings.Contains(script, strings.TrimPrefix(windowsCommand, "        run: ")) {
					t.Errorf("generated Windows WSL payload lacks the pinned installer command:\n%s", script)
				}
			} else {
				commandFound := false
				for line := range strings.SplitSeq(installStep, "\n") {
					if strings.HasPrefix(line, "        run:") && line == expectedCommand {
						commandFound = true
						break
					}
				}
				if !commandFound {
					t.Errorf("generated %s lock file lacks the pinned installer command:\n%s", runner, installStep)
				}
				if strings.Contains(installStep, "shell: bash") {
					t.Errorf("generated Linux install step unexpectedly overrides its default shell:\n%s", installStep)
				}
			}
		})
	}
}

func TestDockerEnclaveHelperInstallerContract(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"", "v0.29.5", "0.29.5", "v0.29.6", "0.29.6", "v0.29.7", "0.29.7"} {
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
				`/usr/bin/sudo -n /usr/bin/install -o root -g root -m 0500 -- "${RUNNER_TEMP}/gh-aw/actions/install_awf_enclave_helper.py"`,
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
			expectedVersion := "v" + strings.TrimPrefix(version, "v")
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
			installerPath := filepath.Join("..", "..", "actions", "setup", "sh", "install_awf_enclave_helper.py")
			installer, err := os.ReadFile(installerPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(installer)); got != dockerEnclaveHelperInstallerSHA {
				t.Fatalf("installer pin = %s, want %s", got, dockerEnclaveHelperInstallerSHA)
			}
			pythonCheck := exec.Command("/usr/bin/python3", "-I", "-c", "import ast, sys; ast.parse(sys.stdin.read())")
			pythonCheck.Stdin = strings.NewReader(string(installer))
			if output, err := pythonCheck.CombinedOutput(); err != nil {
				t.Fatalf("helper installer Python syntax: %v: %s", err, output)
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
	for _, version := range []string{"", "v0.29.4", "v0.29.5", "v0.29.6", "v0.29.7"} {
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
