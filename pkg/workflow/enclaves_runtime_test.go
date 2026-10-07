package workflow

import (
	"bytes"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cloudHypervisorEnclaveWorkflowData(script, agent bool) *WorkflowData {
	data := enclaveWorkflowData(script, agent, 30, 120)
	data.NetworkPermissions.Firewall.Version = string(constants.AWFEnclaveCloudHypervisorMinVersion)
	for _, enclave := range data.Enclaves {
		enclave.Runtime = string(AgentRuntimeCloudHypervisor)
	}
	return data
}

func TestHasCloudHypervisorEnclaves(t *testing.T) {
	assert.False(t, hasCloudHypervisorEnclaves(nil))
	assert.False(t, hasCloudHypervisorEnclaves(&WorkflowData{Enclaves: EnclavesConfig{nil}}))
	assert.False(t, hasCloudHypervisorEnclaves(enclaveWorkflowData(true, true, 30, 120)))
	assert.True(t, hasCloudHypervisorEnclaves(cloudHypervisorEnclaveWorkflowData(true, false)))
	assert.True(t, hasCloudHypervisorEnclaves(cloudHypervisorEnclaveWorkflowData(false, true)))
}

func TestValidateCloudHypervisorEnclaveVersion(t *testing.T) {
	for _, version := range []string{"", "v0.28.11", "v0.28.37", "v0.28.46", "v0.28.47", "v0.28.48", "latest"} {
		t.Run(version, func(t *testing.T) {
			data := cloudHypervisorEnclaveWorkflowData(true, false)
			data.NetworkPermissions.Firewall.Version = version
			err := validateEnclavesConfig(data)
			switch version {
			case "v0.28.47", "v0.28.48", "latest":
				require.NoError(t, err)
			default:
				require.ErrorContains(t, err, "requires AWF v0.28.47 or newer")
			}
		})
	}
}

func TestValidateCloudHypervisorEnclaves(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowData)
		want   string
	}{
		{"static script", func(*WorkflowData) {}, ""},
		{"image override", func(data *WorkflowData) { data.Enclaves[0].Image = "custom:latest" }, ".image is incompatible"},
		{"mixed default", func(data *WorkflowData) {
			data.Enclaves = append(data.Enclaves, &EnclaveConfig{Agent: &AgentEnclaveConfig{Model: "gpt-5"}, Repos: enclaveTestRepos()})
		}, "cannot be mixed"},
		{"mixed Docker", func(data *WorkflowData) {
			data.Enclaves = append(data.Enclaves, &EnclaveConfig{Runtime: "docker", Agent: &AgentEnclaveConfig{Model: "gpt-5"}, Repos: enclaveTestRepos()})
		}, "cannot be mixed"},
		{"ARC DinD", func(data *WorkflowData) { data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind} }, "runner.topology: arc-dind"},
		{"primary sbx", func(data *WorkflowData) { data.SandboxConfig.Agent.Runtime = "sbx" }, "sandbox.agent.runtime: sbx"},
		{"primary VM", func(data *WorkflowData) { data.SandboxConfig.Agent.Runtime = AgentRuntimeCloudHypervisor }, "sandbox.agent.runtime: cloud-hypervisor"},
		{"primary NVX", func(data *WorkflowData) { data.SandboxConfig.Agent.Runtime = "nvx" }, "sandbox.agent.runtime: nvx"},
		{"primary docker-sudo-iptables", func(data *WorkflowData) {
			data.SandboxConfig.Agent.Runtime = AgentRuntimeDockerSudoIptables
		}, "use sandbox.agent.runtime: docker"},
		{"gh-proxy", func(data *WorkflowData) {
			data.Tools["github"] = map[string]any{"mode": "gh-proxy"}
		}, "tools.github.mode: gh-proxy"},
		{"agent path prefix", func(data *WorkflowData) {
			data.SandboxConfig.Agent.Args = []string{"--docker-host-path-prefix", "/shared"}
		}, "--docker-host-path-prefix"},
		{"firewall path prefix", func(data *WorkflowData) {
			data.NetworkPermissions.Firewall.Args = []string{"--docker-host-path-prefix=/shared"}
		}, "--docker-host-path-prefix"},
		{"script DinD", func(data *WorkflowData) { data.SandboxConfig.Agent.Args = []string{"--enable-dind"} }, "--enable-dind"},
		{"primary runtime argument", func(data *WorkflowData) {
			data.SandboxConfig.Agent.Args = []string{"--container-runtime", "sbx"}
		}, "--container-runtime sbx"},
		{"primary firewall runtime argument", func(data *WorkflowData) {
			data.NetworkPermissions.Firewall.Args = []string{"--container-runtime=nvx"}
		}, "--container-runtime nvx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := cloudHypervisorEnclaveWorkflowData(true, false)
			tt.mutate(data)
			err := validateEnclavesConfig(data)
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.want)
			}
		})
	}
	t.Run("dynamic VM", func(t *testing.T) {
		data := dynamicEnclaveWorkflowData()
		data.Enclaves[0].Runtime = string(AgentRuntimeCloudHypervisor)
		require.ErrorContains(t, validateEnclavesConfig(data), ".dynamic is incompatible")
	})
	t.Run("Docker remains supported", func(t *testing.T) {
		data := enclaveWorkflowData(true, true, 30, 120)
		data.SandboxConfig.Agent.Args = []string{"--enable-dind"}
		require.NoError(t, validateEnclavesConfig(data))
	})
}

func TestEnclavesCloudHypervisorExperimentalWarning(t *testing.T) {
	const warning = "Using experimental feature: enclaves cloud-hypervisor runtime"
	for _, tc := range []struct {
		name string
		data *WorkflowData
		want bool
	}{
		{"script", cloudHypervisorEnclaveWorkflowData(true, false), true},
		{"agent", cloudHypervisorEnclaveWorkflowData(false, true), true},
		{"both emit once", cloudHypervisorEnclaveWorkflowData(true, true), true},
		{"Docker", enclaveWorkflowData(true, true, 30, 120), false},
		{"no enclaves", &WorkflowData{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiler := NewCompiler()
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(tc.data, &output)
			if tc.want {
				assert.Contains(t, output.String(), warning)
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, output.String(), warning)
				assert.Zero(t, compiler.GetWarningCount())
			}
		})
	}
}

func TestValidateCloudHypervisorAgentEnclave(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowData)
		want   string
	}{
		{"default Copilot route", func(*WorkflowData) {}, ""},
		{"both VM executors", func(data *WorkflowData) {
			data.Enclaves = append(data.Enclaves, &EnclaveConfig{Runtime: "cloud-hypervisor", Script: &ScriptEnclaveConfig{}, Repos: enclaveTestRepos()})
		}, ""},
		{"missing model", func(data *WorkflowData) { data.Enclaves[0].Agent.Model = "" }, "agent.model is required"},
		{"blank model", func(data *WorkflowData) { data.Enclaves[0].Agent.Model = " " }, "agent.model is required"},
		{"DinD", func(data *WorkflowData) { data.SandboxConfig.Agent.Args = []string{"--enable-dind"} }, "--enable-dind"},
		{"firewall DinD", func(data *WorkflowData) { data.NetworkPermissions.Firewall.Args = []string{"--enable-dind=true"} }, "--enable-dind"},
		{"disabled DinD", func(data *WorkflowData) { data.SandboxConfig.Agent.Args = []string{"--enable-dind=false"} }, ""},
		{"disabled API proxy", func(data *WorkflowData) { data.SandboxConfig.Agent.Args = []string{"--no-enable-api-proxy"} }, "requires the API proxy"},
		{"unimplemented engine", func(data *WorkflowData) { data.Enclaves[0].Agent.Engine = "codex" }, "not implemented"},
		{"static GitHub tools", func(data *WorkflowData) {
			data.Enclaves[0].Agent.Tools = &AgentEnclaveToolsConfig{GitHub: &AgentEnclaveGitHubToolConfig{Allowed: []string{"issue_read"}}}
		}, "scoped executor bearer handoff"},
		{"legacy GitHub profile", func(data *WorkflowData) {
			data.Enclaves[0].Agent.GitHub = &AgentEnclaveGitHubConfig{CLI: enclaveGitHubIssuesProfile}
		}, "scoped executor bearer handoff"},
		{"OpenAI primary has no Copilot route", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "codex"}
		}, "configured Copilot API proxy provider route"},
		{"OpenAI primary filters enclave credential", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "codex", Env: map[string]string{
				"COPILOT_PROVIDER_API_KEY": "${{ secrets.ENCLAVE_KEY }}",
			}}
		}, "configured Copilot API proxy provider route"},
		{"GitHub-backed Codex primary", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "codex", LLMProvider: LLMProviderGitHub}
		}, ""},
		{"GitHub-backed Claude primary", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "claude", LLMProvider: LLMProviderGitHub}
		}, ""},
		{"blank credential override", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_GITHUB_TOKEN": ""}}
		}, "configured Copilot API proxy provider route"},
		{"blank sandbox credential override", func(data *WorkflowData) {
			data.SandboxConfig.Agent.Env = map[string]string{"COPILOT_GITHUB_TOKEN": ""}
		}, "configured Copilot API proxy provider route"},
		{"BYOK route", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "copilot", Env: map[string]string{
				"COPILOT_PROVIDER_BASE_URL": "https://provider.example.com",
				"COPILOT_PROVIDER_API_KEY":  "${{ secrets.PROVIDER_KEY }}",
			}}
		}, ""},
		{"BYOK target alone is not a route", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "copilot", Env: map[string]string{
				"COPILOT_PROVIDER_BASE_URL": "https://provider.example.com",
			}}
		}, "configured Copilot API proxy provider route"},
		{"automatic BYOK provider", func(data *WorkflowData) {
			data.EngineConfig = &EngineConfig{ID: "copilot", LLMProvider: LLMProviderAnthropic}
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := cloudHypervisorEnclaveWorkflowData(false, true)
			tt.mutate(data)
			err := validateEnclavesConfig(data)
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.want)
			}
		})
	}
}
