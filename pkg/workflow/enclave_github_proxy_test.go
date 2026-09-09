//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEnclaveGitHubMCPAgentPolicy(t *testing.T) {
	tests := []struct {
		name             string
		data             *WorkflowData
		wantTools        []string
		wantRepos        []string
		wantMinIntegrity string
	}{
		{
			name: "legacy profile defaults",
			data: func() *WorkflowData {
				data := enclaveGitHubIssuesWorkflowData()
				data.Enclaves[0].Repos = []*EnclaveRepository{
					{Repo: "octo-org/trusted-service", Sensitivity: "trusted"},
					{Repo: "octo-org/public-docs", Sensitivity: "public"},
				}
				return data
			}(),
			wantTools:        []string{"list_issues", "issue_read"},
			wantRepos:        []string{"octo-org/trusted-service", "octo-org/public-docs"},
			wantMinIntegrity: "approved",
		},
		{
			name: "agent tools config overrides defaults",
			data: func() *WorkflowData {
				data := enclaveGitHubToolsWorkflowData()
				data.Enclaves[0].Repos = []*EnclaveRepository{
					{Repo: "octo-org/private-service", Sensitivity: "confidential"},
					{Repo: "octo-org/public-docs", Sensitivity: "public"},
				}
				data.Enclaves[0].Agent.Tools.GitHub.AllowedRepos = GitHubReposScope{"octo-org/public-docs"}
				return data
			}(),
			wantTools:        []string{"list_issues", "issue_read"},
			wantRepos:        []string{"octo-org/public-docs"},
			wantMinIntegrity: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := enclaveGitHubMCPAgentPolicy(tt.data)
			assert.Equal(t, []string{"github"}, policy.Servers)
			assert.Equal(t, map[string][]string{"github": tt.wantTools}, policy.Tools)
			assert.Equal(t, map[string]any{
				"repos":         tt.wantRepos,
				"min-integrity": tt.wantMinIntegrity,
			}, policy.AllowOnly)
		})
	}
}

func TestEnclaveGitHubMCPGatewayConfiguration(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	data.Tools["github"] = map[string]any{}
	data.SafeOutputs = &SafeOutputsConfig{AddComments: &AddCommentsConfig{}}
	config := buildMCPGatewayConfig(data)

	assert.Empty(t, config.AgentID)
	assert.Equal(t, []string{"${MCP_GATEWAY_AGENT_ID}", "${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}"}, config.AgentIDs)
	assert.Equal(t, []string{enclaveMCPServerName, "github", constants.SafeOutputsMCPServerID.String()}, config.AgentPolicies["${MCP_GATEWAY_AGENT_ID}"].Servers)
	assert.Equal(t, []string{"github"}, config.AgentPolicies["${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}"].Servers)

	generatedServers := make(map[string]struct{})
	for _, server := range collectMCPServersForManifest(data) {
		generatedServers[server.Name] = struct{}{}
	}
	for agentID, policy := range config.AgentPolicies {
		for _, server := range policy.Servers {
			assert.Contains(t, generatedServers, server, "policy for %s references an unknown MCP server", agentID)
		}
	}
}

func TestToolsWithEnclaveGitHubIssuesUnionsTypedToolsets(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	tools := map[string]any{
		"github": map[string]any{"toolsets": []string{"context"}},
	}

	updated := toolsWithEnclaveGitHubIssues(tools, data)

	assert.Equal(t, []string{"context", "issues"}, updated["github"].(map[string]any)["toolsets"])
	assert.Equal(t, []string{"context"}, tools["github"].(map[string]any)["toolsets"], "original tools must remain unchanged")
}

func TestDynamicEnclaveRegistersGitHubBackend(t *testing.T) {
	data := dynamicEnclaveWorkflowData()
	config := buildMCPGatewayConfig(data)

	// The GitHub backend stays registered so mcpg's delegation controller can
	// issue delegated identities for it, but the primary agent identity must
	// not gain GitHub MCP access merely because a dynamic enclave is enabled.
	assert.Contains(t, collectMCPTools(data), "github")
	assert.NotContains(t, config.AgentPolicies["${MCP_GATEWAY_AGENT_ID}"].Servers, "github")
}

func TestDynamicEnclaveWithPrimaryGitHubRetainsPrimaryAccess(t *testing.T) {
	data := dynamicEnclaveWorkflowData()
	data.Tools["github"] = map[string]any{}
	config := buildMCPGatewayConfig(data)

	assert.Contains(t, config.AgentPolicies["${MCP_GATEWAY_AGENT_ID}"].Servers, "github")
	assert.NotEmpty(t, config.AgentPolicies["${MCP_GATEWAY_AGENT_ID}"].Tools["github"])
}

func TestGenerateMCPSetupDynamicEnclaveGitHubBackendWithoutPrimaryGitHub(t *testing.T) {
	workflowData := dynamicEnclaveWorkflowData()
	workflowData.Tools["github"] = false
	workflowData.SafeOutputs = &SafeOutputsConfig{AddComments: &AddCommentsConfig{}}
	workflowData.SandboxConfig.MCP.Version = ""
	workflowData.TimeoutMinutes = "timeout-minutes: 20"
	workflowData.Enclaves[0].Dynamic.Sensitivity = "internal"
	workflowData.Enclaves[0].Dynamic.AllowedOwners = []string{"github"}
	workflowData.Enclaves[0].Dynamic.AllowedRepositories = nil

	ensureDefaultMCPGatewayConfig(workflowData)

	compiler := &Compiler{}
	engine := NewCopilotEngine()
	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, engine, workflowData))

	setup := yaml.String()
	assert.Contains(t, setup, `ghcr.io/github/gh-aw-mcpg:`+string(constants.DefaultMCPGatewayVersion))
	assert.Contains(t, setup, `"min-integrity": "approved"`)
	assert.Contains(t, setup, `"github/*"`)
	assert.Contains(t, setup, `"accept": [`)
	assert.Contains(t, setup, `"private:github"`)
	assert.Contains(t, setup, `"sink-visibility": "${GH_AW_SINK_VISIBILITY}"`)
	assert.Contains(t, setup, `"required": false`)
	assert.Contains(t, setup, `GH_AW_TIMEOUT_MINUTES: 20`)
	assert.NotContains(t, setup, `$GITHUB_MCP_GUARD_MIN_INTEGRITY`)
	assert.NotContains(t, setup, `$GITHUB_MCP_GUARD_REPOS`)
}

func TestDynamicEnclaveWriteSinkPolicyUsesWorkflowDestinationVisibility(t *testing.T) {
	for _, sensitivity := range []string{"internal", "confidential"} {
		t.Run(sensitivity, func(t *testing.T) {
			workflowData := dynamicEnclaveWorkflowData()
			workflowData.Tools["github"] = false
			workflowData.Enclaves[0].Dynamic.Sensitivity = sensitivity
			workflowData.Enclaves[0].Dynamic.AllowedOwners = []string{"github"}
			workflowData.Enclaves[0].Dynamic.AllowedRepositories = nil

			assert.Equal(t, map[string]any{
				"write-sink": map[string]any{
					"accept":          []string{"private:github"},
					"sink-visibility": sinkVisibilityRuntimeExpr,
				},
			}, dynamicEnclaveWriteSinkGuardPolicy(workflowData))
		})
	}
}

// TestCompileDynamicGitHubEnclaveDisabledPrimaryGitHub compiles a workflow matching the
// gh-aw#59523 fixture: tools.github: false with a dynamic GitHub repository enclave. It
// asserts the compiler generates a usable lock file without manual edits (guard policy,
// write-sink policy, delegated-only backend readiness, envelope lifetime, mcpg version)
// and that the generated lock file itself is valid YAML end-to-end, guarding against
// regressions like a bare unindented "," corrupting an enclosing "run: |" block scalar.
func TestCompileDynamicGitHubEnclaveDisabledPrimaryGitHub(t *testing.T) {
	tmp := t.TempDir()
	workflowPath := filepath.Join(tmp, "dynamic-enclave.md")
	content := `---
on: workflow_dispatch
strict: false
engine: copilot
tools:
  github: false
enclaves:
  - agent:
      model: gpt-5
      max-task-bytes: 4096
      max-model-requests: 10
      max-model-tokens: 32768
    dynamic:
      allowed-owners: [github]
      sensitivity: internal
      github-policy: github-repository-read-v1
      max-repositories: 1
      quotas:
        max-invocations: 1
        max-output-bytes: 1024
        max-execution-seconds: 180
      audit-labels: ["dynamic-enclave"]
      expires-at: "2027-01-01T00:00:00Z"
    timeout: 180
    memory-limit: "512m"
    cpu-limit: "1"
    pids-limit: 128
    tmpfs-limit: "64m"
    max-output-bytes: 1024
    max-invocations: 1
safe-outputs:
  threat-detection:
    enabled: false
timeout-minutes: 20
---

# Dynamic Enclave Test

Test dynamic enclave delegation.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockBytes, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	require.NoError(t, err)
	lock := string(lockBytes)

	// The generated lock file must be valid YAML end-to-end (it embeds JSON as literal
	// text inside "run: |" block scalars; any unindented line dedents out of the block).
	var doc any
	require.NoError(t, yaml.Unmarshal(lockBytes, &doc), "generated lock file must be valid YAML")

	// 1. GitHub source guard is generated even though tools.github is false.
	assert.Contains(t, lock, `"min-integrity": "approved"`)
	assert.Contains(t, lock, `"github/*"`)
	assert.NotContains(t, lock, "$GITHUB_MCP_GUARD_MIN_INTEGRITY")
	assert.NotContains(t, lock, "$GITHUB_MCP_GUARD_REPOS")

	// 2. Safe Outputs gets the destination visibility from the runtime detection
	// step, while its accepted source secrecy stays scoped to the dynamic enclave.
	assert.Contains(t, lock, `"sink-visibility": "${GH_AW_SINK_VISIBILITY}"`)
	assert.Contains(t, lock, `"private:github"`)
	assert.Contains(t, lock, "Determine automatic lockdown mode")

	// 3. The delegated-only GitHub backend does not block gateway readiness.
	assert.Contains(t, lock, `"required": false`)

	// 4. The delegation envelope lifetime is bounded by the job timeout, while the
	// per-identity TTL stays bounded by max-execution-seconds (180s).
	assert.Contains(t, lock, `GH_AW_ENCLAVE_DYNAMIC_JOB_EXPIRES_EPOCH=$(( $(date -u +%s) + (${GH_AW_TIMEOUT_MINUTES:-20} * 60) ))`)
	assert.Contains(t, lock, `\"max_identity_ttl\":180`)

	// 5. mcpg uses the default version, which must meet the dynamic delegation minimum,
	// and is consistent across manifest, download, and runtime.
	defaultVersion := string(constants.DefaultMCPGatewayVersion)
	assert.True(t, versionAtLeast(defaultVersion, "v0.0.0", string(constants.MCPGDynamicRepositoryDelegationMinVersion)))
	assert.Equal(t, strings.Count(lock, "ghcr.io/github/gh-aw-mcpg:"+defaultVersion), strings.Count(lock, "ghcr.io/github/gh-aw-mcpg:"))
}

func TestCompileEnclaveGitHubSharedGateway(t *testing.T) {
	tmp := t.TempDir()
	workflowPath := filepath.Join(tmp, "enclave-github.md")
	content := `---
on: workflow_dispatch
strict: false
network: defaults
engine: copilot
tools:
  github:
    toolsets: [context]
safe-outputs:
  add-comment:
sandbox:
  agent:
    id: awf
  mcp:
    version: v0.4.15
enclaves:
  - agent:
      model: gpt-5
      github:
        cli: issues-read-v1
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
---

Read the assigned repository's issues through the enclave.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockBytes, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	require.NoError(t, err)
	lock := string(lockBytes)

	assert.Equal(t, 1, strings.Count(lock, "--name awmg-mcpg"))
	assert.Contains(t, lock, `"agentIds": ["${MCP_GATEWAY_AGENT_ID}","${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}"]`)
	assert.Contains(t, lock, `"safeoutputs": {`)
	assert.Contains(t, lock, `"awf-enclave": {`)
	assert.NotContains(t, lock, `"required": false`)
	assert.Contains(t, lock, `"GITHUB_TOOLSETS": "context,issues"`)
	assert.Contains(t, lock, `"${MCP_GATEWAY_AGENT_ID}":{"servers":["awf-enclave","github","safeoutputs"],"tools":{"github":["get_me"]}}`)
	assert.NotContains(t, lock, `"servers":["awf-enclave","github","safe-outputs"]`)
	assert.Contains(t, lock, `"agentPolicies": {"${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}":{"servers":["github"],"tools":{"github":["list_issues","issue_read"]},"allow-only":{"min-integrity":"approved","repos":["octo-org/private-service"]}}`)
	assert.Contains(t, lock, `AWF_ENCLAVE_GITHUB_MCP_AGENT_ID=$(openssl rand -base64 45 | tr -d '/+=')`)
	assert.Contains(t, lock, `printf '%s=%s\n' AWF_ENCLAVE_GITHUB_MCP_AGENT_ID "$AWF_ENCLAVE_GITHUB_MCP_AGENT_ID"`)
	assert.Contains(t, lock, `MCP_GATEWAY_API_KEY: ${{ steps.start-mcp-gateway.outputs.gateway-api-key }}`)
	assert.Contains(t, lock, `--exclude-env MCP_GATEWAY_API_KEY`)
	assert.Contains(t, lock, "--exclude-env AWF_ENCLAVE_GITHUB_MCP_AGENT_ID")
	assert.NotContains(t, lock, "Enclave GitHub Proxy")
	assert.NotContains(t, lock, "start_enclave_github_proxy")
	assert.NotContains(t, lock, "stop_enclave_github_proxy")
}

func TestEnclaveGitHubMCPVersionGates(t *testing.T) {
	data := enclaveGitHubIssuesWorkflowData()
	data.NetworkPermissions.Firewall.Version = string(constants.AWFEnclaveGitHubIssuesMinVersion)
	require.NoError(t, validateEnclavesConfig(data))

	data.SandboxConfig.MCP.Version = "v0.4.14"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(constants.MCPGEnclaveGitHubIssuesMinVersion))
}

func TestDynamicEnclaveMCPVersionGatesAndDefaults(t *testing.T) {
	data := dynamicEnclaveWorkflowData()
	data.SandboxConfig.MCP.Version = ""
	require.NoError(t, validateEnclavesConfig(data))
	ensureDefaultMCPGatewayConfig(data)
	assert.Equal(t, string(constants.DefaultMCPGatewayVersion), data.SandboxConfig.MCP.Version)

	data = dynamicEnclaveWorkflowData()
	data.SandboxConfig.MCP.Version = "v0.4.18"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(constants.MCPGDynamicRepositoryDelegationMinVersion))
	assert.Contains(t, err.Error(), "set sandbox.mcp.version to "+string(constants.MCPGDynamicRepositoryDelegationMinVersion)+" or newer")
}

func TestEnclaveGitHubToolsVersionGates(t *testing.T) {
	data := enclaveGitHubToolsWorkflowData()
	data.NetworkPermissions.Firewall.Version = string(constants.AWFEnclaveGitHubIssuesMinVersion)
	require.NoError(t, validateEnclavesConfig(data))

	data.NetworkPermissions.Firewall.Version = "v0.28.8"
	err := validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(constants.AWFEnclaveGitHubIssuesMinVersion))

	data = enclaveGitHubToolsWorkflowData()
	data.SandboxConfig.MCP.Version = "v0.4.14"
	err = validateEnclavesConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(constants.MCPGEnclaveAgentToolsMinVersion))
}
