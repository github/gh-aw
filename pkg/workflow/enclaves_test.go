package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enclaveWorkflowData(script, agent bool, scriptTimeout, agentTimeout int) *WorkflowData {
	data := &WorkflowData{
		Tools: map[string]any{},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID: "awf",
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	if script {
		data.Enclaves = append(data.Enclaves, &EnclaveConfig{
			Script: &ScriptEnclaveConfig{}, Timeout: scriptTimeout, Repos: enclaveTestRepos(),
		})
	}
	if agent {
		data.Enclaves = append(data.Enclaves, &EnclaveConfig{
			Agent: &AgentEnclaveConfig{Model: "gpt-5"}, Timeout: agentTimeout, Repos: enclaveTestRepos(),
		})
	}
	return data
}

func enclaveTestRepos() []*EnclaveRepository {
	return []*EnclaveRepository{{
		Repo: "octo-org/private-service", Sensitivity: "confidential",
	}}
}

func enclaveGitHubIssuesWorkflowData() *WorkflowData {
	data := enclaveWorkflowData(false, true, 0, 120)
	data.Enclaves[0].Agent.GitHub = &AgentEnclaveGitHubConfig{CLI: enclaveGitHubIssuesProfile}
	data.SandboxConfig.MCP = &MCPGatewayRuntimeConfig{
		Container: constants.DefaultMCPGatewayContainer,
		Version:   string(constants.MCPGEnclaveGitHubIssuesMinVersion),
	}
	return data
}

func enclaveGitHubToolsWorkflowData() *WorkflowData {
	data := enclaveWorkflowData(false, true, 0, 120)
	data.Enclaves[0].Agent.Tools = &AgentEnclaveToolsConfig{
		GitHub: &AgentEnclaveGitHubToolConfig{
			Allowed:      []string{"list_issues", "issue_read"},
			AllowedRepos: GitHubReposScope{"octo-org/private-service"},
			MinIntegrity: GitHubIntegrityNone,
		},
	}
	data.NetworkPermissions.Firewall.Version = string(constants.AWFEnclaveGitHubIssuesMinVersion)
	data.SandboxConfig.MCP = &MCPGatewayRuntimeConfig{
		Container: constants.DefaultMCPGatewayContainer,
		Version:   string(constants.MCPGEnclaveAgentToolsMinVersion),
	}
	return data
}

func dynamicEnclaveWorkflowData() *WorkflowData {
	data := enclaveWorkflowData(false, true, 0, 120)
	data.Enclaves[0].Repos = nil
	data.Enclaves[0].Dynamic = &DynamicEnclavePolicy{
		AllowedOwners:       []string{"octo-org"},
		AllowedRepositories: []string{"octo-org/private-service"},
		Sensitivity:         "confidential",
		GitHubPolicy:        enclaveDynamicGitHubPolicy,
		MaxRepositories:     4,
		Quotas: &DynamicEnclaveQuotas{
			MaxInvocations:      8,
			MaxOutputBytes:      32768,
			MaxExecutionSeconds: 900,
		},
		AuditLabels: []string{"dynamic-enclave", "issues"},
		ExpiresAt:   time.Now().UTC().Add(60 * time.Second).Format(time.RFC3339),
	}
	data.Enclaves[0].MemoryLimit = "512m"
	data.Enclaves[0].CPULimit = "1"
	data.Enclaves[0].PIDsLimit = 128
	data.Enclaves[0].TmpfsLimit = "64m"
	data.Enclaves[0].MaxOutputBytes = 8192
	data.Enclaves[0].MaxInvocations = 8
	data.Enclaves[0].Agent.MaxTaskBytes = 4096
	data.Enclaves[0].Agent.MaxModelRequests = 8
	data.Enclaves[0].Agent.MaxModelTokens = 1024
	data.NetworkPermissions.Firewall.Version = string(constants.AWFDynamicRepositoryEnclaveMinVersion)
	data.SandboxConfig.MCP = &MCPGatewayRuntimeConfig{
		Container: constants.DefaultMCPGatewayContainer,
		Version:   string(constants.MCPGDynamicRepositoryDelegationMinVersion),
	}
	return data
}

func TestEnabledEnclaveToolsAndTimeout(t *testing.T) {
	tests := []struct {
		name                  string
		script, agent         bool
		scriptTime, agentTime int
		wantTools             []string
		wantTimeout           int
	}{
		{"script only defaults cover timing bucket", true, false, 0, 0, []string{"enclave_run_script"}, 4860},
		{"agent only defaults cover timing bucket", false, true, 0, 0, []string{"enclave_run_agent"}, 4860},
		{"45 second custom timeout covers timing bucket", true, false, 45, 0, []string{"enclave_run_script"}, 4860},
		{"4740 second maximum timeout covers timing bucket", false, true, 0, 4740, []string{"enclave_run_agent"}, 4860},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := enclaveWorkflowData(tt.script, tt.agent, tt.scriptTime, tt.agentTime)
			assert.Equal(t, tt.wantTools, enabledEnclaveTools(data))
			assert.Equal(t, tt.wantTimeout, enclaveToolTimeout(data))
			assert.Contains(t, collectMCPTools(data), enclaveMCPServerName)
		})
	}

	disabled := enclaveWorkflowData(false, false, 0, 0)
	assert.Empty(t, enabledEnclaveTools(disabled))
	assert.NotContains(t, collectMCPTools(disabled), enclaveMCPServerName)
}

func TestValidateEnclavesRequiresNetworkIsolation(t *testing.T) {
	data := enclaveWorkflowData(true, false, 30, 0)
	data.SandboxConfig.Agent.Disabled = true
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires AWF network isolation")
}

func TestValidateEnclavesRejectsDuplicateTypes(t *testing.T) {
	data := enclaveWorkflowData(true, false, 30, 0)
	data.Enclaves = append(data.Enclaves, &EnclaveConfig{
		Script: &ScriptEnclaveConfig{}, Repos: enclaveTestRepos(),
	})
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate executor type "script"`)
}

func TestValidateEnclavesRequiresConsistentRepositorySensitivity(t *testing.T) {
	data := enclaveWorkflowData(true, true, 30, 120)
	data.Enclaves[1].Repos[0].Sensitivity = "sealed"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must use the same sensitivity across enclave types")
}

func TestValidateEnclavesRequiresAgentModelOnly(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 120)
	data.Enclaves[0].Agent.Model = ""
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent.model is required")

	script := enclaveWorkflowData(true, false, 30, 0)
	assert.NoError(t, validateEnclavesConfig(script))
}

func TestParseTopLevelKeyedEnclaves(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"enclaves": []any{
			map[string]any{
				"script": nil,
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
				"timeout": 45,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, config.Enclaves, 1)
	require.NotNil(t, config.Enclaves[0].Script)
	assert.Equal(t, 45, config.Enclaves[0].Timeout)
	require.Len(t, config.Enclaves[0].Repos, 1)
}

func TestParseTopLevelKeyedEnclavesAgentGitHubTools(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"enclaves": []any{
			map[string]any{
				"agent": map[string]any{
					"model": "gpt-5",
					"tools": map[string]any{
						"github": map[string]any{
							"allowed":       []any{"list_issues", "issue_read"},
							"allowed-repos": []any{"octo-org/private-service"},
							"min-integrity": "none",
						},
					},
				},
				"repos": []any{
					map[string]any{"repo": "octo-org/private-service", "sensitivity": "confidential"},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, config.Enclaves, 1)
	require.NotNil(t, config.Enclaves[0].Agent)
	require.NotNil(t, config.Enclaves[0].Agent.Tools)
	require.NotNil(t, config.Enclaves[0].Agent.Tools.GitHub)
	assert.Equal(t, []string{"list_issues", "issue_read"}, config.Enclaves[0].Agent.Tools.GitHub.Allowed)
	assert.Equal(t, GitHubReposScope{"octo-org/private-service"}, config.Enclaves[0].Agent.Tools.GitHub.AllowedRepos)
	assert.Equal(t, GitHubIntegrityNone, config.Enclaves[0].Agent.Tools.GitHub.MinIntegrity)
}

func TestParseTopLevelKeyedEnclavesDynamicAgentPolicy(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"enclaves": []any{
			map[string]any{
				"agent": map[string]any{
					"model":              "gpt-5",
					"max-task-bytes":     4096,
					"max-model-requests": 8,
					"max-model-tokens":   1024,
				},
				"dynamic": map[string]any{
					"allowed-owners":       []any{"octo-org"},
					"allowed-repositories": []any{"octo-org/private-service"},
					"sensitivity":          "confidential",
					"github-policy":        "github-repository-read-v1",
					"max-repositories":     4,
					"quotas": map[string]any{
						"max-invocations":       8,
						"max-output-bytes":      32768,
						"max-execution-seconds": 900,
					},
					"audit-labels": []any{"dynamic-enclave", "issues"},
					"expires-at":   "2999-01-01T00:00:00Z",
				},
				"timeout":          120,
				"memory-limit":     "512m",
				"cpu-limit":        "1",
				"pids-limit":       128,
				"tmpfs-limit":      "64m",
				"max-output-bytes": 8192,
				"max-invocations":  8,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, config.Enclaves, 1)
	require.NotNil(t, config.Enclaves[0].Dynamic)
	assert.Equal(t, []string{"octo-org"}, config.Enclaves[0].Dynamic.AllowedOwners)
	assert.Equal(t, []string{"octo-org/private-service"}, config.Enclaves[0].Dynamic.AllowedRepositories)
	assert.Equal(t, enclaveDynamicGitHubPolicy, config.Enclaves[0].Dynamic.GitHubPolicy)
}

func TestEnclaveConfigRejectsAmbiguousDiscriminator(t *testing.T) {
	data := enclaveWorkflowData(false, false, 0, 0)
	data.Enclaves = EnclavesConfig{{
		Script: &ScriptEnclaveConfig{},
		Agent:  &AgentEnclaveConfig{Model: "gpt-5"},
		Repos:  enclaveTestRepos(),
	}}
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of script or agent")
}

func TestBuildAWFConfigJSONEnclaves(t *testing.T) {
	data := enclaveWorkflowData(true, true, 45, 180)
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", WorkflowData: data,
	})
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	enclaves := config["enclaves"].([]any)
	script := enclaves[0].(map[string]any)
	agent := enclaves[1].(map[string]any)
	scriptConfig := script["script"].(map[string]any)
	agentConfig := agent["agent"].(map[string]any)
	assert.Empty(t, scriptConfig)
	assert.InDelta(t, 45, script["timeout"], 0)
	assert.Equal(t, "gpt-5", agentConfig["model"])
	assert.Contains(t, script, "repos")
	assert.NotContains(t, script, "enabled")
	assert.NotContains(t, script, "network")
	assert.NotContains(t, script, "interpreter")
	assert.Equal(t, []any{"awmg-mcpg"}, config["network"].(map[string]any)["topologyAttach"])
	assert.NotContains(t, configJSON, "boundedQueries")
	assert.NotContains(t, configJSON, "boundedAgents")
}

func TestBuildAWFConfigJSONEnclaveGitHubIssues(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", WorkflowData: data,
	})
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	enclaves := config["enclaves"].([]any)
	agent := enclaves[0].(map[string]any)["agent"].(map[string]any)
	assert.Equal(t, map[string]any{"cli": enclaveGitHubIssuesProfile}, agent["github"])
}

func TestBuildAWFConfigJSONEnclaveGitHubTools(t *testing.T) {
	data := enclaveGitHubToolsWorkflowData()
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", WorkflowData: data,
	})
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	enclaves := config["enclaves"].([]any)
	agent := enclaves[0].(map[string]any)["agent"].(map[string]any)
	assert.Equal(t, map[string]any{"cli": enclaveGitHubIssuesProfile}, agent["github"])
	assert.NotContains(t, agent, "tools")
}

func TestBuildAWFConfigJSONDynamicEnclavePolicy(t *testing.T) {
	data := dynamicEnclaveWorkflowData()
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", WorkflowData: data,
	})
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	enclaves := config["enclaves"].([]any)
	entry := enclaves[0].(map[string]any)
	assert.NotContains(t, entry, "repos")
	dynamic := entry["dynamic"].(map[string]any)
	assert.Equal(t, []any{"octo-org"}, dynamic["allowedOwners"])
	assert.Equal(t, []any{"octo-org/private-service"}, dynamic["allowedRepositories"])
	assert.Equal(t, "agent", dynamic["executor"])
	assert.Equal(t, "confidential", dynamic["sensitivity"])
	assert.InDelta(t, 4, dynamic["maxRepositories"], 0)
	assert.Equal(t, map[string]any{
		"version": enclaveDynamicGitHubPolicy,
		"tools":   []any{"list_issues", "issue_read"},
	}, dynamic["githubPolicy"])
	assert.Equal(t, map[string]any{
		"maxExecutionSeconds": float64(900),
		"maxInvocations":      float64(8),
		"maxOutputBytes":      float64(32768),
	}, dynamic["quotas"])
	assert.Equal(t, []any{"dynamic-enclave", "issues"}, dynamic["auditLabels"])
	assert.Equal(t, data.Enclaves[0].Dynamic.ExpiresAt, dynamic["expiresAt"])
}

func TestValidateDynamicEnclavePolicyBoundsExpiryAndCPU(t *testing.T) {
	// expires-at is a checked-in upper bound; it must remain valid even after
	// it has grown "stale" relative to compile time, since the runtime/job-
	// relative expiry contract (not compile-time comparison) clamps the
	// effective envelope expiry at workflow setup time.
	data := dynamicEnclaveWorkflowData()
	data.Enclaves[0].Dynamic.ExpiresAt = "2999-01-01T00:00:00Z"
	require.NoError(t, validateEnclavesConfig(data))

	data = dynamicEnclaveWorkflowData()
	data.Enclaves[0].Dynamic.ExpiresAt = "not-a-timestamp"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an absolute RFC3339 timestamp")

	data = dynamicEnclaveWorkflowData()
	data.Enclaves[0].CPULimit = "0"
	err = validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cpu-limit must be a positive finite value")
}

func TestBuildDynamicEnclaveExpiryScriptResolvesMinOfConfiguredAndJobExpiry(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	newEnclave := func(expiresAt string, timeoutSeconds int) *EnclaveConfig {
		return &EnclaveConfig{
			Timeout: timeoutSeconds,
			Dynamic: &DynamicEnclavePolicy{ExpiresAt: expiresAt},
		}
	}

	runScript := func(t *testing.T, workflowData *WorkflowData, enclave *EnclaveConfig) string {
		t.Helper()
		script, err := buildDynamicEnclaveExpiryScript(workflowData, enclave)
		require.NoError(t, err)
		cmd := exec.Command("bash", "-c", "set -eo pipefail\n"+script+"echo \"$MCP_GATEWAY_DELEGATION_EXPIRES_AT\"\n")
		output, err := cmd.Output()
		require.NoError(t, err)
		return strings.TrimSpace(string(output))
	}

	t.Run("configured expires-at earlier than job expiry wins", func(t *testing.T) {
		configured := time.Now().UTC().Add(30 * time.Second).Truncate(time.Second)
		enclave := newEnclave(configured.Format(time.RFC3339), 4800)
		got, err := time.Parse(time.RFC3339, runScript(t, nil, enclave))
		require.NoError(t, err)
		assert.WithinDuration(t, configured, got, time.Second)
	})

	t.Run("job timeout minutes bound the envelope lifetime", func(t *testing.T) {
		enclave := newEnclave("2999-01-01T00:00:00Z", 30)
		workflowData := &WorkflowData{TimeoutMinutes: "timeout-minutes: 1"}
		got, err := time.Parse(time.RFC3339, runScript(t, workflowData, enclave))
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().UTC().Add(1*time.Minute), got, 5*time.Second)
	})

	t.Run("non-UTC offset and fractional-second expires-at is canonicalized before the BSD date fallback", func(t *testing.T) {
		// validateEnclavesConfig accepts any RFC3339 timestamp, including a
		// non-UTC offset and fractional seconds, but the BSD date fallback below
		// only parses the exact whole-second UTC "...Z" form. buildDynamicEnclaveExpiryScript
		// must canonicalize the value before embedding it so this still resolves
		// correctly, matching GNU date's behavior for the same input.
		configured := time.Now().UTC().Add(30 * time.Second).Truncate(time.Second)
		zoned := configured.In(time.FixedZone("", 3600)) // +01:00, with fractional seconds
		enclave := newEnclave(zoned.Format("2006-01-02T15:04:05.000-07:00"), 4800)
		got, err := time.Parse(time.RFC3339, runScript(t, nil, enclave))
		require.NoError(t, err)
		assert.WithinDuration(t, configured, got, time.Second)
	})

	t.Run("non-RFC3339 expires-at (bypassing compile-time validation) surfaces an internal error", func(t *testing.T) {
		enclave := newEnclave("not-a-timestamp", 30)
		_, err := buildDynamicEnclaveExpiryScript(nil, enclave)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "internal error")
	})
}

func TestValidateEnclaveGitHubIssuesRepositoryLimit(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	data.Enclaves[0].Repos = append(data.Enclaves[0].Repos, &EnclaveRepository{
		Repo: "octo-org/another-private-service", Sensitivity: "internal",
	})
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "supports at most one non-public repository")

	data.Enclaves[0].Repos[1].Sensitivity = "public"
	require.NoError(t, validateEnclavesConfig(data))
}

func TestValidateEnclaveGitHubIssuesRepositoryLimitTreatsTrustedAsPublic(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	data.NetworkPermissions.Firewall.Version = "v0.28.14"
	data.Enclaves[0].Repos = []*EnclaveRepository{
		{Repo: "octo-org/trusted-service", Sensitivity: "trusted"},
		{Repo: "octo-org/public-service", Sensitivity: "public"},
	}
	require.NoError(t, validateEnclavesConfig(data))
}

func TestValidateEnclaveTrustedSensitivityRequiresAWFVersion(t *testing.T) {
	data := enclaveWorkflowData(false, true, 0, 120)
	data.Enclaves[0].Repos[0].Sensitivity = "trusted"
	data.NetworkPermissions.Firewall.Version = "v0.28.13"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires AWF v0.28.14 or newer")

	data.NetworkPermissions.Firewall.Version = "v0.28.14"
	require.NoError(t, validateEnclavesConfig(data))
}

func TestValidateEnclaveGitHubIssuesRepositoryLimitScopesToGitHubEntry(t *testing.T) {
	data := enclaveWorkflowData(true, true, 30, 120)
	data.Enclaves[0].Repos = []*EnclaveRepository{{
		Repo: "octo-org/private-a", Sensitivity: "confidential",
	}}
	data.Enclaves[1].Agent.GitHub = &AgentEnclaveGitHubConfig{CLI: enclaveGitHubIssuesProfile}
	data.Enclaves[1].Repos = []*EnclaveRepository{{
		Repo: "octo-org/private-b", Sensitivity: "confidential",
	}}
	data.NetworkPermissions.Firewall.Version = string(constants.AWFEnclaveGitHubIssuesMinVersion)
	data.SandboxConfig.MCP = &MCPGatewayRuntimeConfig{
		Version: string(constants.MCPGEnclaveGitHubIssuesMinVersion),
	}

	require.NoError(t, validateEnclavesConfig(data))
}

func TestValidateEnclaveGitHubIssuesMode(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	data.Enclaves[0].Agent.GitHub.CLI = "read-only"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `must be "issues-read-v1"`)
}

func TestValidateEnclaveGitHubTools(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*WorkflowData)
		errContains string
	}{
		{
			name: "unsupported tool",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Agent.Tools.GitHub.Allowed = []string{"search_issues"}
			},
			errContains: "contains unsupported tool",
		},
		{
			name: "repo outside enclave list",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Agent.Tools.GitHub.AllowedRepos = GitHubReposScope{"octo-org/other-repo"}
			},
			errContains: "must be declared in enclaves[0].repos",
		},
		{
			name: "legacy and new configs are mutually exclusive",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Agent.GitHub = &AgentEnclaveGitHubConfig{CLI: enclaveGitHubIssuesProfile}
			},
			errContains: "cannot both be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := enclaveGitHubToolsWorkflowData()
			tt.mutate(data)
			err := validateEnclavesConfig(data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}

	require.NoError(t, validateEnclavesConfig(enclaveGitHubToolsWorkflowData()))
}

func TestValidateDynamicEnclavePolicy(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*WorkflowData)
		errContains string
	}{
		{
			name: "rejects static and dynamic in same entry",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Repos = enclaveTestRepos()
			},
			errContains: "either static repos or dynamic",
		},
		{
			name: "rejects non canonical owner",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Dynamic.AllowedOwners = []string{"Octo-Org"}
				data.Enclaves[0].Dynamic.AllowedRepositories = nil
			},
			errContains: "canonical lowercase ASCII owner",
		},
		{
			name: "rejects non canonical repository selector",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Dynamic.AllowedOwners = nil
				data.Enclaves[0].Dynamic.AllowedRepositories = []string{"octo-org/../secret"}
			},
			errContains: "canonical dynamic selector",
		},
		{
			name: "rejects unknown policy",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Dynamic.GitHubPolicy = "github-repository-read-v2"
			},
			errContains: "github-policy",
		},
		{
			name: "rejects missing quotas",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Dynamic.Quotas = nil
			},
			errContains: "dynamic.quotas",
		},
		{
			name: "rejects unbounded resource limits",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].MemoryLimit = ""
			},
			errContains: "finite timeout",
		},
		{
			name: "rejects dynamic tool narrowing",
			mutate: func(data *WorkflowData) {
				data.Enclaves[0].Agent.Tools = &AgentEnclaveToolsConfig{GitHub: &AgentEnclaveGitHubToolConfig{Allowed: []string{"list_issues"}}}
			},
			errContains: "agent.tools.github.allowed must match",
		},
		{
			name: "rejects old awf version",
			mutate: func(data *WorkflowData) {
				data.NetworkPermissions.Firewall.Version = "v0.28.13"
			},
			errContains: "requires AWF v0.28.14 or newer",
		},
		{
			name: "rejects old mcpg version",
			mutate: func(data *WorkflowData) {
				data.SandboxConfig.MCP.Version = "v0.4.17"
			},
			errContains: "requires MCPG v0.4.19 or newer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := dynamicEnclaveWorkflowData()
			tt.mutate(data)
			err := validateEnclavesConfig(data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
	require.NoError(t, validateEnclavesConfig(dynamicEnclaveWorkflowData()))
}

func TestDynamicEnclaveGatewayContract(t *testing.T) {
	data := dynamicEnclaveWorkflowData()
	gateway := buildMCPGatewayConfig(data)
	require.NotNil(t, gateway)
	assert.Equal(t, []string{"${MCP_GATEWAY_AGENT_ID}"}, gateway.AgentIDs)
	assert.NotContains(t, gateway.AgentPolicies, "${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}")

	var output strings.Builder
	require.NoError(t, generateMCPGatewaySetup(
		&output, data.Tools, []string{enclaveMCPServerName}, NewCopilotEngine(), data, false, nil,
	))
	generated := output.String()
	// The gateway's strict-stdin config schema does not accept a
	// "delegationControllers" field; the compiler must not emit one.
	assert.NotContains(t, generated, `"delegationControllers"`)
	// The five required settings are bootstrapped as one atomic configuration.
	assert.Contains(t, generated, `export MCP_GATEWAY_DELEGATION_CONTROL_KEY="${AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY}"`)
	assert.Contains(t, generated, `export MCP_GATEWAY_DELEGATION_STATE_PATH="`)
	assert.Contains(t, generated, `export MCP_GATEWAY_DELEGATION_GENERATION="${GITHUB_RUN_ATTEMPT}"`)
	// This workflow runs under bridge-mode network isolation (MCP_GATEWAY_DOMAIN
	// resolves to "awmg-mcpg"), so the in-container listener binds to 0.0.0.0
	// (bridge-reachable) while the host-side -p publish below stays on 127.0.0.1.
	assert.Contains(t, generated, `export MCP_GATEWAY_DELEGATION_CONTROL_LISTEN="0.0.0.0:8090"`)
	assert.Contains(t, generated, `export MCP_GATEWAY_DELEGATION_ENVELOPE=`)
	// Envelope field names and shape match mcpg's delegation.Envelope wire contract
	// exactly (snake_case, DisallowUnknownFields); see buildMCPGatewayDelegationEnvelope.
	assert.Contains(t, generated, `\"run_id\":\"${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}\"`)
	assert.Contains(t, generated, `\"enclave_backend\":\"github\"`)
	assert.Contains(t, generated, `\"tool_policy\":\"github-repository-read-v1\"`)
	assert.Contains(t, generated, `\"max_dynamic_schema_hashes\":4`)
	// max_identity_ttl uses seconds end to end, matching enclaves[].timeout.
	assert.Contains(t, generated, `\"max_identity_ttl\":120`)
	assert.Contains(t, generated, `\"expires_at\":\"${MCP_GATEWAY_DELEGATION_EXPIRES_AT}\"`)
	assert.NotContains(t, generated, `\"version\":\"github-repository-read-v1\"`)
	assert.NotContains(t, generated, `\"tools\":[`)
	assert.NotContains(t, generated, `\"generation\":`)
	assert.NotContains(t, generated, `\"auditLabels\":`)
	// The private control endpoint is distinct from the executor-facing data plane
	// and targets the pinned mcpg build's actual control API base path.
	assert.Contains(t, generated, `export AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT="http://127.0.0.1:8090/internal/awf-enclave-mcp-control/github-repository-delegation-v1"`)
	assert.Contains(t, generated, `AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY=$(openssl rand -hex 32)`)
	assert.Contains(t, generated, `::add-mask::${AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY}`)
	assert.Contains(t, generated, `printf '%s=%s\n' AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY "$AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_CAPABILITY"`)
	assert.Contains(t, generated, `printf '%s=%s\n' AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT "$AWF_ENCLAVE_GITHUB_DELEGATION_CONTROL_ENDPOINT"`)
	assert.NotContains(t, generated, `AWF_ENCLAVE_GITHUB_MCP_AGENT_ID=$(openssl rand`)

	excluded := ComputeAWFExcludeEnvVarNames(data, nil)
	assert.Contains(t, excluded, enclaveGitHubDelegationEnv)
	assert.Contains(t, excluded, enclaveMCPCapabilityEnv)
	assert.Contains(t, excluded, enclaveGitHubDelegationControlEndpointEnv)
}

func TestGenerateEnclaveGatewayContract(t *testing.T) {
	data := enclaveWorkflowData(true, true, 45, 180)
	ensureDefaultMCPGatewayConfig(data)
	var output strings.Builder
	require.NoError(t, generateMCPGatewaySetup(
		&output, data.Tools, []string{enclaveMCPServerName}, NewCopilotEngine(), data, false, nil,
	))
	generated := output.String()

	assert.Contains(t, generated, `"awf-enclave": {`)
	assert.Contains(t, generated, `"url": "http://awf-enclave-mcp:8080/mcp"`)
	assert.Contains(t, generated, `"connectTimeout": 120`)
	assert.Contains(t, generated, `"toolTimeout": 4860`)
	assert.Contains(t, generated, `"tools": ["enclave_run_script", "enclave_run_agent"]`)
	assert.Contains(t, generated, `Bearer \${AWF_ENCLAVE_MCP_CAPABILITY}`)
	assert.Contains(t, generated, `openssl rand -hex 32`)
	assert.Contains(t, generated, `::add-mask::${AWF_ENCLAVE_MCP_CAPABILITY}`)
	assert.Contains(t, generated, `printf '%s=%s\n' MCP_GATEWAY_AGENT_ID "$MCP_GATEWAY_AGENT_ID"`)
	assert.Contains(t, generated, `--network bridge`)
	assert.Contains(t, generated, `--label com.github.gh-aw.mcpg.run=`)
	assert.Contains(t, generated, `${AWF_ENCLAVE_MCP_GATEWAY_IDENTITY}`)
	assert.Contains(t, generated, `-e AWF_ENCLAVE_MCP_CAPABILITY`)
	assert.Contains(t, generated, `AWF_ENCLAVE_MCP_GATEWAY_ENDPOINT="http://localhost:${MCP_GATEWAY_PORT}/mcp/awf-enclave"`)
	assert.Contains(t, generated, `export GH_AW_MCP_DEFERRED_SERVERS="awf-enclave"`)
	assert.NotContains(t, generated, `printf '%s=%s\n' GH_AW_MCP_DEFERRED_SERVERS`)
	assert.NotContains(t, generated, `"required": false`)
	assert.NotRegexp(t, `AWF_ENCLAVE_MCP_CAPABILITY=[0-9a-f]{64}`, generated)
	gatewayCommand := strings.Index(generated, `export MCP_GATEWAY_DOCKER_COMMAND=`)
	require.Greater(t, gatewayCommand, -1)
	for _, name := range optionalPRHeadEnvVars {
		emptyDefault := `export ` + name + `="${` + name + `:-}"`
		assert.Contains(t, generated, emptyDefault)
		assert.Less(t, strings.Index(generated, emptyDefault), gatewayCommand)
	}
	gatewayKeyMask := strings.Index(generated, `::add-mask::${MCP_GATEWAY_AGENT_ID}`)
	gatewayKeyHandoff := strings.Index(generated, `printf '%s=%s\n' MCP_GATEWAY_AGENT_ID "$MCP_GATEWAY_AGENT_ID"`)
	deferred := strings.Index(generated, `export GH_AW_MCP_DEFERRED_SERVERS="awf-enclave"`)
	gatewayRunner := strings.Index(generated, `| "$GH_AW_NODE" "${RUNNER_TEMP}/gh-aw/actions/start_mcp_gateway.cjs"`)
	require.Greater(t, gatewayKeyMask, -1)
	require.Greater(t, gatewayKeyHandoff, gatewayKeyMask)
	require.Greater(t, deferred, -1)
	require.Greater(t, gatewayRunner, deferred)

	excluded := ComputeAWFExcludeEnvVarNames(data, nil)
	assert.Contains(t, excluded, enclaveMCPCapabilityEnv)
	assert.Contains(t, excluded, enclaveMCPGatewayIdentityEnv)
}

func TestCompileEnclaveStartupOrdering(t *testing.T) {
	tmp := t.TempDir()
	workflowPath := filepath.Join(tmp, "enclave.md")
	content := `---
on: workflow_dispatch
strict: false
network: defaults
engine: copilot
sandbox:
  agent:
    id: awf
    version: latest
enclaves:
  - script:
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
    timeout: 45
---

Use the enclave script executor.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockBytes, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	lock := string(lockBytes)

	gateway := strings.Index(lock, "- name: Start MCP Gateway")
	gatewayKeyHandoff := strings.Index(lock, `printf '%s=%s\n' MCP_GATEWAY_AGENT_ID "$MCP_GATEWAY_AGENT_ID"`)
	deferred := strings.Index(lock, `export GH_AW_MCP_DEFERRED_SERVERS="awf-enclave"`)
	awf := strings.Index(lock, "awf --config")
	require.Greater(t, gateway, -1)
	require.Greater(t, gatewayKeyHandoff, gateway)
	require.Greater(t, deferred, gateway)
	require.Greater(t, awf, -1)
	assert.Less(t, gateway, awf)
	assert.Less(t, gatewayKeyHandoff, awf)
	assert.Less(t, deferred, awf)
	assert.Contains(t, lock, `"awf-enclave"`)
	assert.NotContains(t, lock, `"required": false`)
	assert.Contains(t, lock, "--exclude-env MCP_GATEWAY_AGENT_ID")
	if mountStart := strings.Index(lock, "- name: Mount MCP servers as CLIs"); mountStart >= 0 {
		mountEnd := strings.Index(lock[mountStart:], "\n      - name:")
		require.Positive(t, mountEnd)
		assert.NotContains(t, lock[mountStart:mountStart+mountEnd], "steps.start-mcp-gateway.outputs.gateway-agent-id")
	}
	assert.Contains(t, lock, `\"enclaves\":[{\"repos\":[{\"repo\":\"octo-org/private-service\",\"sensitivity\":\"confidential\"}],\"script\":{},\"timeout\":45}]`)
	assert.NotContains(t, lock, "Start Enclave MCP")
	assert.NotContains(t, lock, "start_enclave")
}

// TestBuildMCPGatewayDelegationEnvelopeMaxIdentityTTLSeconds pins the seconds
// contract shared by gh-aw, gh-aw-firewall, and mcpg.
func TestBuildMCPGatewayDelegationEnvelopeMaxIdentityTTLSeconds(t *testing.T) {
	enclave := &EnclaveConfig{
		Timeout: 120,
		Dynamic: &DynamicEnclavePolicy{MaxRepositories: 4},
	}
	envelope := buildMCPGatewayDelegationEnvelope(enclave)

	assert.Equal(t, 120, envelope["max_identity_ttl"])
}

// TestValidateDynamicEnclaveBoundsRejectsOversizedTimeout preserves fail-closed
// validation for enclave timeouts that would otherwise be forwarded to mcpg.
// gh-aw's compile-time bound matches gh-aw-firewall's
// MAX_ENCLAVE_TIMEOUT_SECONDS preflight limit and the awf-config schema so the
// three validation layers cannot disagree about what compiles.
func TestValidateDynamicEnclaveBoundsRejectsOversizedTimeout(t *testing.T) {
	data := dynamicEnclaveWorkflowData()
	data.Enclaves[0].Timeout = maxDynamicEnclaveTimeoutSeconds + 1
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be at most")

	data = dynamicEnclaveWorkflowData()
	data.Enclaves[0].Timeout = maxDynamicEnclaveTimeoutSeconds
	require.NoError(t, validateEnclavesConfig(data),
		"the AWF-compatible maximum timeout must still validate cleanly")
}
