package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "go.yaml.in/yaml/v3"
)

func TestDefaultDockerEnclaveReleasePins(t *testing.T) {
	for _, tc := range []struct {
		name          string
		script, agent bool
		roles         []string
	}{
		{"agent", false, true, []string{"enclave-agent", "enclave-mcp-server"}},
		{"script", true, false, []string{"enclave-script", "enclave-mcp-server"}},
		{"both", true, true, []string{"enclave-script", "enclave-agent", "enclave-mcp-server"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := enclaveWorkflowData(tc.script, tc.agent, 30, 30)
			data.NetworkPermissions.Firewall.Version = "v0.29.5"
			require.NoError(t, validateDefaultDockerEnclaveImagePins(data))
			configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
			require.NoError(t, err)
			var config AWFConfigFile
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
			require.NotNil(t, config.Container)
			assert.Empty(t, config.Container.Images, "public-debug must not get closed enclave image overrides")
			assert.True(t, strings.HasPrefix(config.Container.ImageTag, "0.29.5,"))
			for _, role := range append([]string{"squid", "agent", "api-proxy", "cli-proxy"}, tc.roles...) {
				image := constants.DefaultFirewallRegistry + "/" + role + ":0.29.5"
				assert.Contains(t, config.Container.ImageTag, role+"="+lookupContainerDigest(image, data))
			}
		})
	}
}

func TestDefaultDockerEnclavePinsFailClosed(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 30)
	data.NetworkPermissions.Firewall.Version = "v99.99.99"
	require.ErrorContains(t, validateDefaultDockerEnclaveImagePins(data), "immutable same-release pin")
	require.ErrorContains(t, validateSandboxConfig(data), "immutable same-release pin")
	_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
	require.ErrorContains(t, err, "immutable same-release pin")
	command := BuildAWFCommand(AWFCommandConfig{EngineName: "copilot", EngineCommand: "copilot", WorkflowData: data})
	assert.Contains(t, command, "exit 1")
	assert.NotContains(t, command, "GH_AW_AWF_ENGINE_NAME=")
}

func TestDefaultDockerEnclaveMissingRolePin(t *testing.T) {
	for _, role := range []string{"enclave-agent", "enclave-mcp-server"} {
		t.Run(role, func(t *testing.T) {
			data := enclaveWorkflowData(false, true, 0, 30)
			data.NetworkPermissions.Firewall.Version = "v0.29.5"
			image := constants.DefaultFirewallRegistry + "/" + role + ":0.29.5"
			data.ActionCache = &ActionCache{ContainerPins: map[string]ContainerPin{image: {Image: image}}}
			require.ErrorContains(t, validateDefaultDockerEnclaveImagePins(data), image)
			_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
			require.ErrorContains(t, err, image)
		})
	}
}

func TestExplicitEnclaveImageManifestRemainsAuthoritative(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 30)
	data.NetworkPermissions.Firewall.Version = "v99.99.99"
	data.SandboxConfig.Agent.Images = map[string]string{
		awfImageRoleEnclaveAgent: "registry.example.com/custom/agent:fixed@sha256:" + strings.Repeat("a", 64),
	}
	assert.Empty(t, defaultDockerEnclaveImageRoles(data))
	require.NoError(t, validateDefaultDockerEnclaveImagePins(data))
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
	require.NoError(t, err)
	var config AWFConfigFile
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	assert.Empty(t, config.Container.ImageTag)
	assert.Equal(t, data.SandboxConfig.Agent.Images, config.Container.Images)
}

func TestEnclaveHostCredentialIsolation(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 30)
	data.NetworkPermissions.Firewall.Version = "v0.29.5"
	data.SandboxConfig.Agent.HostGitHubToken = "${{ secrets.HOST_STAGING_TOKEN }}"
	data.EngineConfig = &EngineConfig{ID: "copilot"}
	env := map[string]string{}
	addCliProxyGHTokenToEnv(env, data)
	assert.Equal(t, "${{ secrets.HOST_STAGING_TOKEN }}", env["GH_TOKEN"])
	assert.Contains(t, ComputeAWFExcludeEnvVarNames(data, nil), "GH_TOKEN")
	command := BuildAWFCommand(AWFCommandConfig{
		EngineName: "copilot", EngineCommand: "copilot", WorkflowData: data,
	})
	assert.Contains(t, command, "--env-all")
	assert.Contains(t, command, "--exclude-env GH_TOKEN")
	assert.Equal(t, 1, strings.Count(command, "--exclude-env GH_TOKEN"))
	assert.NotContains(t, command, "HOST_STAGING_TOKEN")
	assert.NotContains(t, command, "GITHUB_ENV")
	// The host guard exits before launching AWF without printing the credential.
	guard, _, _ := strings.Cut(command, "\n")
	cmd := exec.Command("bash", "-c", guard)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GH_TOKEN="}
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "resolved to an empty credential")
	cmd = exec.Command("bash", "-c", guard)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GH_TOKEN=runtime-test-value"}
	output, err = cmd.CombinedOutput()
	require.NoError(t, err)
	assert.Empty(t, output)
	for _, detection := range []bool{false, true} {
		copied := *data
		copied.IsDetectionRun = detection
		copied.IsEvalsRun = !detection
		env = map[string]string{}
		addCliProxyGHTokenToEnv(env, &copied)
		assert.NotContains(t, env, "GH_TOKEN")
	}
}

func TestEnclaveHostCredentialConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name, value, version, want string
		noEnclaves                 bool
	}{
		{"literal", "not-a-credential", "v0.29.5", "must be a secret expression", false},
		{"context", "${{ github.event.issue.title }}", "v0.29.5", "must be a secret expression", false},
		{"github token", "${{ github.token }}", "v0.29.5", "must be a secret expression", false},
		{"no enclaves", "${{ secrets.STAGING_TOKEN }}", "v0.29.5", "requires enclaves", true},
		{"old AWF", "${{ secrets.STAGING_TOKEN }}", "v0.29.4", "requires AWF v0.29.5", false},
		{"secret", "${{ secrets.STAGING_TOKEN }}", "v0.29.5", "", false},
		{"secret fallback", "${{ secrets.STAGING_TOKEN || secrets.BACKUP_TOKEN }}", "v0.29.5", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := enclaveWorkflowData(false, true, 0, 30)
			data.NetworkPermissions.Firewall.Version = tc.version
			data.SandboxConfig.Agent.HostGitHubToken = tc.value
			if tc.noEnclaves {
				data.Enclaves = nil
			}
			err := validateHostGitHubTokenConfig(data)
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
				require.ErrorContains(t, validateSandboxConfig(data), tc.want)
				command := BuildAWFCommand(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
				assert.Contains(t, command, tc.want)
				assert.NotContains(t, command, "GH_AW_AWF_ENGINE_NAME=")
			}
		})
	}
}

func TestEnclaveHostCredentialSourcesCompile(t *testing.T) {
	for _, sensitivity := range []string{"public", "confidential"} {
		t.Run(sensitivity, func(t *testing.T) {
			dir := testutil.TempDir(t, "enclave-host-compile")
			source := filepath.Join(dir, "workflow.md")
			content := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
sandbox:
  agent:
    version: v0.29.5
    host-github-token: ${{ secrets.HOST_STAGING_TOKEN }}
tools:
  github: false
enclaves:
  - agent:
      model: gpt-5
    repos:
      - repo: example-org/review-target
        sensitivity: ` + sensitivity + `
---
Inspect the configured repository using its enclave.
`
			require.NoError(t, os.WriteFile(source, []byte(content), 0600))
			compiler := NewCompiler()
			var compileErr error
			warnings := testutil.CaptureStderr(t, func() {
				compileErr = compiler.CompileWorkflow(source)
			})
			require.NoError(t, compileErr)
			assert.NotContains(t, warnings, "secrets detected in '")
			assert.NotContains(t, warnings, "excluded from the agent sandbox")
			lock, err := os.ReadFile(strings.TrimSuffix(source, ".md") + ".lock.yml")
			require.NoError(t, err)
			output := string(lock)
			assert.Contains(t, output, "GH_TOKEN: ${{ secrets.HOST_STAGING_TOKEN }}")
			assertHostCredentialYAMLIsolation(t, lock)
			assert.Contains(t, output, "--exclude-env GH_TOKEN")
			assert.Contains(t, output, "0.29.5,squid=sha256:")
			assert.Contains(t, output, ",enclave-agent=sha256:")
			assert.Contains(t, output, ",enclave-mcp-server=sha256:")
			assert.NotContains(t, output, `"enclaveAgent"`)
			if sensitivity == "confidential" {
				assert.Contains(t, output, "8-bit per-run budget")
				assert.Contains(t, output, "response schema cardinality must be at most 8")
			}
		})
	}
}

func assertHostCredentialYAMLIsolation(t *testing.T, lock []byte) {
	t.Helper()
	type stepConfig struct {
		Name string            `yaml:"name"`
		Env  map[string]string `yaml:"env"`
		Run  string            `yaml:"run"`
		With map[string]any    `yaml:"with"`
	}
	var document struct {
		Env  map[string]string `yaml:"env"`
		Jobs map[string]struct {
			Env   map[string]string `yaml:"env"`
			Steps []stepConfig      `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yamlv3.Unmarshal(lock, &document))
	const expression = "${{ secrets.HOST_STAGING_TOKEN }}"
	for _, value := range document.Env {
		assert.NotContains(t, value, expression)
	}
	var destinations []string
	for jobName, job := range document.Jobs {
		for _, value := range job.Env {
			assert.NotContains(t, value, expression, "job-level credential must not reach activation or sandbox jobs")
		}
		for _, step := range job.Steps {
			assert.NotContains(t, step.Run, expression, "credential must not enter executable scripts or source snapshots")
			withJSON, err := json.Marshal(step.With)
			require.NoError(t, err)
			assert.NotContains(t, string(withJSON), expression)
			for key, value := range step.Env {
				if strings.Contains(value, expression) {
					destinations = append(destinations, jobName+"/"+step.Name+"/"+key)
				}
			}
		}
	}
	assert.ElementsMatch(t, []string{
		"agent/" + copilotExecutionStepName + "/GH_TOKEN",
		"agent/Redact secrets in logs/SECRET_HOST_STAGING_TOKEN",
	}, destinations)
}

func TestHostCredentialExcludedWithoutEnclaves(t *testing.T) {
	data := enclaveWorkflowData(false, false, 0, 0)
	data.SandboxConfig.Agent.HostGitHubToken = "${{ secrets.STAGING_TOKEN }}"
	assert.Contains(t, ComputeAWFExcludeEnvVarNames(data, nil), "GH_TOKEN")
	args := strings.Join(BuildAWFArgs(AWFCommandConfig{EngineName: "copilot", WorkflowData: data}), " ")
	assert.Contains(t, args, "--exclude-env GH_TOKEN")
}

func TestHostCredentialYAMLIsolationIgnoresComments(t *testing.T) {
	assertHostCredentialYAMLIsolation(t, []byte(`
# ${{ secrets.HOST_STAGING_TOKEN }} is a configuration reference, not an env binding.
jobs:
  activation:
    steps:
      - name: Snapshot source
        env:
          PUBLIC_VALUE: public
  agent:
    steps:
      - name: Execute GitHub Copilot CLI
        env:
          GH_TOKEN: ${{ secrets.HOST_STAGING_TOKEN }}
      - name: Redact secrets in logs
        env:
          SECRET_HOST_STAGING_TOKEN: ${{ secrets.HOST_STAGING_TOKEN }}
`))
}

func TestEnclaveHostCredentialRequiresRootlessRuntime(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 30)
	data.SandboxConfig.Agent.HostGitHubToken = "${{ secrets.STAGING_TOKEN }}"
	data.SandboxConfig.Agent.Runtime = AgentRuntimeCloudHypervisor
	require.ErrorContains(t, validateHostGitHubTokenConfig(data), "rootless")
}

func TestEnclaveFiniteSchemaPromptWithoutGitHubTools(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 30)
	data.Enclaves[0].Repos = []*EnclaveRepository{
		{Repo: "example-org/public-target", Sensitivity: "public"},
		{Repo: "example-org/confidential-target", Sensitivity: "confidential"},
		{Repo: "example-org/internal-target", Sensitivity: "internal"},
		{Repo: "example-org/trusted-target", Sensitivity: "trusted"},
		{Repo: "example-org/sealed-target", Sensitivity: "sealed"},
	}
	prompt := strings.Join(staticEnclaveInformationBudgetPromptLines(data), "\n")
	assert.Contains(t, prompt, "example-org/public-target")
	assert.NotContains(t, prompt, "example-org/confidential-target")
	assert.NotContains(t, prompt, "example-org/internal-target")
	assert.NotContains(t, prompt, "example-org/trusted-target")
	assert.NotContains(t, prompt, "example-org/sealed-target")
	assert.Contains(t, prompt, "A confidential repository has an 8-bit per-run budget")
	assert.Contains(t, prompt, "An internal repository has a finite per-run budget")
	assert.Contains(t, prompt, "A trusted repository is unmetered")
	assert.Contains(t, prompt, "A sealed repository has a 0-bit per-run budget")
	assert.Contains(t, prompt, "8-bit per-run budget")
	assert.Contains(t, prompt, "`minimum`/`maximum`")
	assert.Contains(t, prompt, "`additionalProperties: false`")
	assert.Contains(t, prompt, "configured enclave timeout")
	assert.NotContains(t, prompt, "octo-org")
}

func TestDockerEnclaveInstallationWrapperReleaseBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		helper        bool
	}{
		{"default", "", true},
		{"supported bridge", string(constants.AWFDockerEnclaveSetupMinVersion), true},
		{"explicit legacy", "v0.28.47", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := enclaveWorkflowData(false, true, 0, 30)
			data.SandboxConfig.Agent.Runtime = AgentRuntimeDocker
			data.SandboxConfig.Agent.Version = tc.version
			data.NetworkPermissions.Firewall.Version = tc.version
			setup := strings.Join(generateAWFInstallationStepForWorkflow(getAWFVersionForSetup(data), data), "\n")
			assert.Equal(t, tc.helper, strings.Contains(setup, "Install authenticated Docker enclave storage helper"))
			assert.Contains(t, setup, "Install AWF binary")
			assert.Contains(t, setup, "--rootless")
			assert.NotContains(t, GetAWFCommandPrefix(data), "sudo")
			if tc.helper {
				assert.Contains(t, setup, "enclave-storage-helper.py probe")
				assert.Contains(t, setup, "AWF_HELPER_VERSION: "+string(constants.AWFDockerEnclaveSetupMinVersion))
			} else {
				assert.NotContains(t, setup, "enclave-storage-helper.py")
			}
		})
	}
}
