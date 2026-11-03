package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func publicDiagnosticsWorkflowData() *WorkflowData {
	data := enclaveWorkflowData(false, true, 0, 180)
	data.SandboxConfig.Agent.Version = enclaveDiagnosticsRelease
	data.NetworkPermissions.Firewall.Version = enclaveDiagnosticsRelease
	data.SandboxConfig.Agent.EnclaveDebugPublic = new(true)
	data.Enclaves[0].Repos = []*EnclaveRepository{{Repo: "github/gh-aw-firewall", Sensitivity: "public"}}
	data.Enclaves[0].MaxInvocations = 1
	return data
}

func TestEnclaveDiagnosticsValidation(t *testing.T) {
	assert.NoError(t, validateEnclaveDiagnostics(nil))
	assert.NoError(t, validateEnclaveDiagnostics(publicDiagnosticsWorkflowData()))
	for _, tc := range []struct {
		name string
		edit func(*WorkflowData)
	}{
		{"confidential", func(d *WorkflowData) { d.Enclaves[0].Repos[0].Sensitivity = "confidential" }},
		{"trusted is not public", func(d *WorkflowData) { d.Enclaves[0].Repos[0].Sensitivity = "trusted" }},
		{"dynamic", func(d *WorkflowData) { d.Enclaves[0].Dynamic = &DynamicEnclavePolicy{} }},
		{"mixed repositories", func(d *WorkflowData) { d.Enclaves[0].Repos = append(d.Enclaves[0].Repos, enclaveTestRepos()...) }},
		{"noncanonical", func(d *WorkflowData) { d.Enclaves[0].Repos[0].Repo = "*" }},
		{"empty", func(d *WorkflowData) { d.Enclaves[0].Repos = nil }},
		{"no enclaves", func(d *WorkflowData) { d.Enclaves = nil }},
		{"multiple entries", func(d *WorkflowData) { d.Enclaves = append(d.Enclaves, d.Enclaves[0]) }},
		{"multiple invocations", func(d *WorkflowData) { d.Enclaves[0].MaxInvocations = 2 }},
		{"CH enclave", func(d *WorkflowData) { d.Enclaves[0].Runtime = "cloud-hypervisor" }},
		{"CH primary", func(d *WorkflowData) { d.SandboxConfig.Agent.Runtime = AgentRuntimeCloudHypervisor }},
		{"privileged primary", func(d *WorkflowData) { d.SandboxConfig.Agent.Runtime = AgentRuntimeDockerSudoIptables }},
		{"disabled", func(d *WorkflowData) { d.SandboxConfig.Agent.Disabled = true }},
		{"image override", func(d *WorkflowData) { d.Enclaves[0].Image = "custom:latest" }},
		{"manifest override", func(d *WorkflowData) {
			d.SandboxConfig.Agent.Images = map[string]string{awfImageRoleEnclaveAgent: "custom.example/enclave:v1@sha256:" + strings.Repeat("1", 64)}
		}},
		{"infrastructure-only manifest", func(d *WorkflowData) {
			d.SandboxConfig.Agent.Images = map[string]string{awfImageRoleSquid: "custom.example/squid:v1@sha256:" + strings.Repeat("1", 64)}
		}},
		{"model-routing manifest", func(d *WorkflowData) {
			d.EngineConfig = &EngineConfig{ID: "copilot", ModelRouting: &CopilotModelRoutingConfig{}}
		}},
		{"command override", func(d *WorkflowData) { d.SandboxConfig.Agent.Command = "custom-awf" }},
		{"argument override", func(d *WorkflowData) { d.SandboxConfig.Agent.Args = []string{"--image-registry", "custom.example"} }},
		{"mount override", func(d *WorkflowData) { d.SandboxConfig.Agent.Mounts = []string{"/var/lib:/var/lib:ro"} }},
		{"unreviewed release", func(d *WorkflowData) { d.SandboxConfig.Agent.Version = "v0.29.4" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := publicDiagnosticsWorkflowData()
			tc.edit(data)
			require.ErrorContains(t, validateEnclaveDiagnostics(data), "sandbox.agent.enclave-debug-public")
			data.SandboxConfig.Agent.EnclaveDebugPublic = new(false)
			require.NoError(t, validateEnclaveDiagnostics(data))
		})
	}
}

func TestEnclaveDiagnosticsWrapperHelperReleaseBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		helper        bool
	}{
		{"default Docker", "", true},
		{"reviewed Docker", "v0.29.5", true},
		{"explicit legacy", "v0.29.4", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := enclaveWorkflowData(false, true, 0, 180)
			data.SandboxConfig.Agent.Version = tc.version
			data.NetworkPermissions.Firewall.Version = tc.version
			step := strings.Join(generateAWFInstallationStepForWorkflow(getAWFVersionForSetup(data), data), "\n")
			assert.Equal(t, tc.helper, strings.Contains(step, "Install authenticated Docker enclave storage helper"))
			assert.Contains(t, step, "--rootless")
			assert.Contains(t, step, "GH_TOKEN: ${{ github.token }}")
			assert.Contains(t, step, "GH_HOST: github.com")
		})
	}
}

func TestEnclaveDiagnosticsConfig(t *testing.T) {
	for _, enabled := range []*bool{nil, new(false), new(true)} {
		data := publicDiagnosticsWorkflowData()
		data.SandboxConfig.Agent.EnclaveDebugPublic = enabled
		config, err := BuildAWFConfigJSON(AWFCommandConfig{WorkflowData: data, EngineName: "copilot"})
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(config), &decoded))
		require.Equal(t, enabled != nil && *enabled, decoded["enclaveDebugPublic"])
	}
	data := publicDiagnosticsWorkflowData()
	data.SandboxConfig.Agent.EnclaveDebugPublic = nil
	data.Enclaves[0].Repos[0].Sensitivity = "confidential"
	config, err := BuildAWFConfigJSON(AWFCommandConfig{WorkflowData: data, EngineName: "copilot"})
	require.NoError(t, err)
	require.Contains(t, config, `"enclaveDebugPublic":false`)
	data.Enclaves = nil
	config, err = BuildAWFConfigJSON(AWFCommandConfig{WorkflowData: data, EngineName: "copilot"})
	require.NoError(t, err)
	require.NotContains(t, config, "enclaveDebugPublic")
}

func TestEnclaveDiagnosticsExtraction(t *testing.T) {
	for _, value := range []any{true, false, "${{ inputs.debug }}", "true"} {
		agent := NewCompiler().extractAgentSandboxConfig(map[string]any{"id": "awf", "enclave-debug-public": value})
		if literal, ok := value.(bool); ok {
			require.NotNil(t, agent.EnclaveDebugPublic)
			assert.Equal(t, literal, *agent.EnclaveDebugPublic)
		} else {
			assert.Nil(t, agent.EnclaveDebugPublic)
		}
	}
}

func TestEnclaveDiagnosticsLifecycleAndRetention(t *testing.T) {
	data := publicDiagnosticsWorkflowData()
	var yaml strings.Builder
	require.NoError(t, generateEnclaveDiagnosticsPrepare(&yaml, data))
	yaml.WriteString("      - name: Execution\n        id: agentic_execution\n        run: awf\n")
	generateEnclaveDiagnosticsCollect(&yaml, data)
	content := yaml.String()
	assert.Contains(t, content, `DIAGNOSTICS_STAGING="$staging_dir" NODE_OPTIONS= NODE_PATH= node`)
	assert.Contains(t, content, `install_enclave_diagnostics.py`)
	assert.Contains(t, content, enclaveDiagnosticsInstallerSHA)
	require.Less(t, strings.Index(content, "prepare docker"), strings.Index(content, "id: agentic_execution"))
	require.Less(t, strings.Index(content, "id: agentic_execution"), strings.Index(content, "collect docker"))
	assert.Contains(t, content, "steps.prepare_enclave_diagnostics.outcome == 'success'")
	assert.Contains(t, content, "always() &&")
	assert.Contains(t, content, "steps.collect_enclave_diagnostics.outcome != 'skipped'")
	assert.NotContains(t, content, "steps.collect_enclave_diagnostics.outcome == 'success'")
	assert.NotContains(t, content, "continue-on-error")
	assert.NotContains(t, content, "|| true")
	assert.Contains(t, content, "/upload/")
	for _, config := range []*RepoConfig{nil, {ArtifactRetentionDays: new(TemplatableInt32("90"))}} {
		rewritten, err := applyArtifactRetention("jobs:\n  agent:\n    steps:\n"+content, config)
		require.NoError(t, err)
		assert.Contains(t, rewritten, "retention-days: 1\n")
		assert.NotContains(t, rewritten, "GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS")
		spoofed := strings.Replace(enclaveDiagnosticsUploadStep(), "/upload/", "/raw/", 1)
		rewritten, err = applyArtifactRetention("jobs:\n  agent:\n    steps:\n"+spoofed, config)
		require.NoError(t, err)
		assert.NotContains(t, rewritten, "retention-days: 1\n")
	}
	yaml.Reset()
	data.SandboxConfig.Agent.EnclaveDebugPublic = nil
	require.NoError(t, generateEnclaveDiagnosticsPrepare(&yaml, data))
	generateEnclaveDiagnosticsCollect(&yaml, data)
	assert.Empty(t, yaml.String())
}

func TestEnclaveDiagnosticsBootstrapDoesNotPassNodeCodeToRoot(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "install_enclave_diagnostics.cjs"))
	require.NoError(t, err)
	assert.NotContains(t, string(source), "sudo")
	assert.NotContains(t, string(source), "TRUSTED_INSTALLER")

	data := publicDiagnosticsWorkflowData()
	var yaml strings.Builder
	require.NoError(t, generateEnclaveDiagnosticsPrepare(&yaml, data))
	step := yaml.String()
	assert.Contains(t, step, `install_enclave_diagnostics.py" "$installer_path"`)
	assert.Contains(t, step, `[[ "${installer_sha256%% *}" == "`+enclaveDiagnosticsInstallerSHA+`" ]]`)
	assert.Contains(t, step, `/usr/bin/env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C /usr/bin/sudo -n -- /usr/bin/python3 -I "$installer_path"`)
	assert.NotContains(t, step, "<<'PY'")
	assert.Less(t, strings.Index(step, enclaveDiagnosticsInstallerSHA), strings.Index(step, `"$installer_path" "$staging_dir/node.tar.xz"`))
	runStart := strings.Index(step, "        run: |\n")
	require.GreaterOrEqual(t, runStart, 0)
	runLines := strings.Split(step[runStart+len("        run: |\n"):], "\n")
	var shell strings.Builder
	for _, line := range runLines {
		if !strings.HasPrefix(line, "          ") {
			break
		}
		shell.WriteString(strings.TrimPrefix(line, "          "))
		shell.WriteByte('\n')
	}
	shellCheck := exec.Command("bash", "-n")
	shellCheck.Stdin = strings.NewReader(shell.String())
	if output, err := shellCheck.CombinedOutput(); err != nil {
		t.Fatalf("diagnostic setup shell syntax: %v: %s", err, output)
	}

	installerPath := filepath.Join("..", "..", "actions", "setup", "sh", "install_enclave_diagnostics.py")
	installer, err := os.ReadFile(installerPath)
	require.NoError(t, err)
	require.Equal(t, enclaveDiagnosticsInstallerSHA, fmt.Sprintf("%x", sha256.Sum256(installer)))
	pythonCheck := exec.Command("/usr/bin/python3", "-I", "-c", "import ast, sys; ast.parse(sys.stdin.read())")
	pythonCheck.Stdin = strings.NewReader(string(installer))
	if output, err := pythonCheck.CombinedOutput(); err != nil {
		t.Fatalf("diagnostic setup Python syntax: %v: %s", err, output)
	}
}

func TestEnclaveDiagnosticsCompile(t *testing.T) {
	for _, tc := range []struct {
		name, setting, sensitivity, want string
	}{
		{"public", "true", "public", ""},
		{"disabled confidential", "false", "confidential", ""},
		{"omitted public", "", "public", ""},
		{"reject confidential", "true", "confidential", "every repository"},
		{"reject expression", "${{ inputs.debug }}", "public", "boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "enclave-diagnostics")
			path := filepath.Join(dir, "enclave.md")
			setting := ""
			if tc.setting != "" {
				setting = "    enclave-debug-public: " + tc.setting + "\n"
			}
			source := "---\non: workflow_dispatch\nnetwork: defaults\nengine: copilot\nsandbox:\n  agent:\n    id: awf\n    version: v0.29.5\n" +
				setting + "enclaves:\n  - script:\n    repos:\n      - repo: github/gh-aw-firewall\n        sensitivity: " + tc.sensitivity +
				"\n    max-invocations: 1\n---\nRun one bounded enclave script.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(path)
			if tc.want != "" {
				require.ErrorContains(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			lock, err := os.ReadFile(filepath.Join(dir, "enclave.lock.yml"))
			require.NoError(t, err)
			assert.Contains(t, string(lock), "name: Install authenticated Docker enclave storage helper")
			assert.Contains(t, string(lock), "--rootless")
			for _, role := range []string{"squid", "agent", "api-proxy", "cli-proxy", "enclave-script", "enclave-mcp-server"} {
				assert.Contains(t, string(lock), role+"=sha256:")
			}
			assert.NotContains(t, string(lock), `\"images\":{`)
			installer := strings.Index(string(lock), "      - name: Install AWF binary\n")
			require.GreaterOrEqual(t, installer, 0)
			installStep := strings.SplitN(string(lock)[installer:], "\n      - ", 2)[0]
			assert.Contains(t, installStep, "GH_TOKEN: ${{ github.token }}")
			assert.Contains(t, installStep, "GH_HOST: github.com")
			if tc.setting == "true" {
				assert.Contains(t, string(lock), `\"enclaveDebugPublic\":true`)
				assert.Contains(t, string(lock), "retention-days: 1\n")
				assert.Contains(t, string(lock), enclaveDiagnosticsUploadAction)
				prepare := strings.Index(string(lock), "id: prepare_enclave_diagnostics")
				execution := strings.Index(string(lock), "id: agentic_execution")
				collect := strings.Index(string(lock), "id: collect_enclave_diagnostics")
				require.Positive(t, prepare)
				require.Less(t, prepare, execution)
				require.Less(t, execution, collect)
				assert.Contains(t, string(lock), enclaveDiagnosticsUploadStep())
			} else {
				assert.Contains(t, string(lock), `\"enclaveDebugPublic\":false`)
				assert.NotContains(t, string(lock), "prepare_enclave_diagnostics")
				assert.NotContains(t, string(lock), "collect_enclave_diagnostics")
			}
			require.NoError(t, compiler.CompileWorkflow(path))
			recompiled, err := os.ReadFile(filepath.Join(dir, "enclave.lock.yml"))
			require.NoError(t, err)
			assert.Equal(t, string(lock), string(recompiled), "clean compilation must preserve source/lock parity")
		})
	}
}

func TestEnclaveDiagnosticsTrustedBootstrap(t *testing.T) {
	script, err := os.ReadFile("../../actions/setup/js/install_enclave_diagnostics.cjs")
	require.NoError(t, err)
	source := string(script)
	require.NoError(t, exec.Command("node", "--check", "../../actions/setup/js/install_enclave_diagnostics.cjs").Run())
	assert.Contains(t, source, "v24.22.1")
	assert.Contains(t, source, "3929c9a15c0f239b772affcaae6e55fec92f41c2")
	assert.NotContains(t, source, "command -v node")
	installer, err := os.ReadFile("../../actions/setup/sh/install_enclave_diagnostics.py")
	require.NoError(t, err)
	body := string(installer)
	for _, required := range []string{
		"v24.22.1", "6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6",
		"dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e",
		"7722affc5c076d79692d4268a543ef267281424ad4b374eb8f80c24cbec08cae",
		"os.O_NOFOLLOW", "os.O_EXCL",
		"protected_directory(directory)", "member.isfile()", "archive.getmember",
	} {
		assert.Contains(t, body, required)
	}
	assert.NotContains(t, body, "extractall")
	python, err := exec.LookPath("python3")
	require.NoError(t, err)
	definitions, _, found := strings.Cut(body, "\nif __name__ == \"__main__\":")
	require.True(t, found)
	dir := testutil.TempDir(t, "trusted-collector-files")
	target := filepath.Join(dir, "regular")
	require.NoError(t, os.WriteFile(target, []byte("verified"), 0600))
	harness := definitions + `
target = sys.argv[1]
assert read_regular(target, 8) == b"verified"
def denied(path, limit, category):
    try:
        read_regular(path, limit)
        raise AssertionError("accepted unsafe file")
    except SystemExit as error:
        assert category in str(error), error
denied(target, 7, "file-size")
os.link(target, target + "-hard")
denied(target, 8, "unsafe-file")
os.unlink(target + "-hard")
os.symlink(target, target + "-link")
try:
    read_regular(target + "-link", 8)
    raise AssertionError("accepted symlink")
except OSError:
    pass
os.chmod(os.path.dirname(target), 0o777)
try:
    protected_directory(os.path.dirname(target))
    raise AssertionError("accepted runner-owned or writable directory")
except SystemExit as error:
    assert "unsafe-directory" in str(error)
`
	output, err := exec.Command(python, "-I", "-c", harness, target).CombinedOutput()
	require.NoError(t, err, string(output))
	// Execute the real bootstrap body: corrupt archives must stop before any
	// protected destination is inspected or created.
	bootstrap := definitions + `
from types import SimpleNamespace
from unittest.mock import patch
with patch("os.getuid", return_value=0), patch("os.geteuid", return_value=0), patch("os.uname", return_value=SimpleNamespace(machine="x86_64")):
    main(sys.argv[1:])
`
	output, err = exec.Command(python, "-I", "-c", bootstrap, target, target).CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "archive-checksum")
}
