//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestCompileEnclaveOnlyGitHubToolsGuardPolicy compiles a workflow that disables
// primary-agent GitHub access and enables GitHub tools only for a static enclave agent.
// The GitHub MCP server must not reference the determine-automatic-lockdown step outputs,
// because that step is not generated when tools.github is false: the resulting empty
// server-level guard policy makes mcpg exit during startup.
func TestCompileEnclaveOnlyGitHubToolsGuardPolicy(t *testing.T) {
	tmp := t.TempDir()
	workflowPath := filepath.Join(tmp, "enclave-only-github.md")
	content := `---
on: workflow_dispatch
strict: false
network: defaults
engine: copilot
tools:
  github: false
enclaves:
  - agent:
      model: gpt-5
      tools:
        github:
          allowed: [list_issues, issue_read]
          allowed-repos: [octo-org/private-service]
          min-integrity: none
    repos:
      - repo: octo-org/private-service
        sensitivity: confidential
safe-outputs:
  add-comment:
    max: 1
---

Read the private repository's issues through the enclave.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockBytes, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	require.NoError(t, err)
	lock := string(lockBytes)

	var doc any
	require.NoError(t, yaml.Unmarshal(lockBytes, &doc), "generated lock file must be valid YAML")

	// The determine-automatic-lockdown step IS generated in this configuration, solely to
	// supply the target repository's visibility for the static enclave's write-sink policy
	// (GH_AW_SINK_VISIBILITY). Its min_integrity/repos outputs must still not be referenced,
	// because the server-level guard policy for this enclave-only backend is derived
	// statically from the enclave declaration, not from the step outputs.
	assert.Contains(t, lock, "Determine automatic lockdown mode")
	assert.Contains(t, lock, "id: determine-automatic-lockdown")
	assert.Contains(t, lock, "GH_AW_SINK_VISIBILITY: ${{ steps.determine-automatic-lockdown.outputs.visibility }}")
	assert.NotContains(t, lock, "$GITHUB_MCP_GUARD_MIN_INTEGRITY")
	assert.NotContains(t, lock, "$GITHUB_MCP_GUARD_REPOS")
	assert.NotContains(t, lock, "GITHUB_MCP_GUARD_MIN_INTEGRITY: ${{ steps.determine-automatic-lockdown.outputs.min_integrity }}")
	assert.NotContains(t, lock, "GITHUB_MCP_GUARD_REPOS: ${{ steps.determine-automatic-lockdown.outputs.repos }}")

	// The server-level guard policy mirrors the enclave identity policy, so it never
	// broadens access beyond what the enclave identity already allows.
	assert.Contains(t, lock, `"min-integrity": "none"`)
	assert.Contains(t, lock, `"octo-org/private-service"`)
	assert.Contains(t, lock, `"write-sink"`)
	assert.Contains(t, lock, `"private:octo-org/private-service"`)
	assert.Contains(t, lock, `"sink-visibility": "${GH_AW_SINK_VISIBILITY}"`)
	// The sink-visibility env var must reference the step actually generated above — no
	// dangling steps.<id>.outputs.* reference.
	assert.Contains(t, lock, "steps.determine-automatic-lockdown.outputs.visibility")
	assert.Contains(t, lock, `"${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}":{"servers":["github"],"tools":{"github":["list_issues","issue_read"]},"allow-only":{"min-integrity":"none","repos":["octo-org/private-service"]}}`)
	assert.Contains(t, lock, `export GH_AW_MCP_GITHUB_CHECK_AGENT_ID="${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}"`)
	assert.NotContains(t, lock, `"${MCP_GATEWAY_AGENT_ID}":{"servers":["awf-enclave","github"`)

	// The gateway's runtime forcePublicRepos override would rewrite the enclave's
	// allow-only scope to repos="public" in a public repository, discarding
	// allowed-repos and leaving the enclave with nothing to read.
	assert.Contains(t, lock, `"forcePublicRepos": false`)
}

// stepOutputRefPattern matches steps.<id>.outputs.<name> references, capturing the step id.
var stepOutputRefPattern = regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs\.[A-Za-z0-9_-]+`)

// assertNoDanglingStepOutputReferences verifies that every `steps.<id>.outputs.*` reference
// in the generated lock file corresponds to a step id that is actually emitted in the same
// job. For references made from a step field, the producer step must also appear earlier in
// that job's step list. This guards against the class of bug described in gh-aw#60336, where a
// consumer (e.g. an environment variable or guard policy) references a step's outputs even
// though the producer step itself was never generated, expanding to an empty string at runtime.
func assertNoDanglingStepOutputReferences(t *testing.T, lock string) {
	t.Helper()

	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(lock), &workflow), "generated lock file must be valid YAML")

	jobs, ok := workflow["jobs"].(map[string]any)
	require.True(t, ok, "generated lock file must contain jobs")

	for jobID, jobValue := range jobs {
		job, ok := jobValue.(map[string]any)
		require.True(t, ok, "job %q must be an object", jobID)

		steps, _ := job["steps"].([]any)
		allStepIDs := make(map[string]bool)
		for _, stepValue := range steps {
			step, ok := stepValue.(map[string]any)
			if !ok {
				continue
			}
			if stepID, ok := step["id"].(string); ok {
				allStepIDs[stepID] = true
			}
		}

		jobBytes, err := yaml.Marshal(jobValue)
		require.NoError(t, err, "job %q must marshal for step reference checks", jobID)
		for _, match := range stepOutputRefPattern.FindAllStringSubmatch(string(jobBytes), -1) {
			stepID := match[1]
			assert.True(t, allStepIDs[stepID], "job %q reference %q has no corresponding emitted step id %q", jobID, match[0], stepID)
		}

		previousStepIDs := make(map[string]bool)
		for stepIndex, stepValue := range steps {
			step, ok := stepValue.(map[string]any)
			if !ok {
				continue
			}

			stepBytes, err := yaml.Marshal(stepValue)
			require.NoError(t, err, "job %q step %d must marshal for step reference checks", jobID, stepIndex)
			for _, match := range stepOutputRefPattern.FindAllStringSubmatch(string(stepBytes), -1) {
				stepID := match[1]
				assert.True(t, previousStepIDs[stepID], "job %q step %d reference %q must refer to a previously emitted step id %q", jobID, stepIndex, match[0], stepID)
			}

			if stepID, ok := step["id"].(string); ok {
				previousStepIDs[stepID] = true
			}
		}
	}
}

// TestCompileStaticEnclaveOnlyGitHubDisabledSinkVisibility is a regression test for
// gh-aw#60336: a static GitHub enclave combined with `tools.github: false` and safe-outputs
// must not emit GH_AW_SINK_VISIBILITY (or any other value) referencing the
// determine-automatic-lockdown step unless that step is actually generated. Before the fix,
// the compiler emitted `GH_AW_SINK_VISIBILITY: ${{ steps.determine-automatic-lockdown.outputs.visibility }}`
// and `"sink-visibility": "${GH_AW_SINK_VISIBILITY}"` even though no `determine-automatic-lockdown`
// step existed, so the env var resolved to an empty string and MCP Gateway rejected the
// resulting `sink-visibility: ""` as invalid.
func TestCompileStaticEnclaveOnlyGitHubDisabledSinkVisibility(t *testing.T) {
	tmp := t.TempDir()
	workflowPath := filepath.Join(tmp, "roadmap-triage-enclave.md")
	content := `---
on: workflow_dispatch
strict: false
network: defaults
engine: copilot
tools:
  github: false
enclaves:
  - agent:
      model: gpt-5
      tools:
        github:
          allowed: [list_issues, issue_read]
          allowed-repos: [githubnext/gh-aw-enclave-demo-private]
          min-integrity: none
    repos:
      - repo: githubnext/gh-aw-enclave-demo-private
        sensitivity: confidential
safe-outputs:
  add-comment:
    max: 1
---

Read the private repository's issues through the enclave and post a triage comment.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lockBytes, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	require.NoError(t, err)
	lock := string(lockBytes)

	var doc any
	require.NoError(t, yaml.Unmarshal(lockBytes, &doc), "generated lock file must be valid YAML")

	// General invariant: every steps.<id>.outputs.* reference must correspond to an
	// emitted step id somewhere in the generated workflow.
	assertNoDanglingStepOutputReferences(t, lock)

	// The determine-automatic-lockdown step and its producer for GH_AW_SINK_VISIBILITY must
	// either both be present, or both be absent — never a dangling reference to one without
	// the other. Here, the step IS generated (solely to supply visibility for the static
	// enclave's write-sink policy).
	assert.Contains(t, lock, "id: determine-automatic-lockdown")
	assert.Contains(t, lock, "GH_AW_SINK_VISIBILITY: ${{ steps.determine-automatic-lockdown.outputs.visibility }}")
	assert.Contains(t, lock, `"sink-visibility": "${GH_AW_SINK_VISIBILITY}"`)

	// The primary agent must have no GitHub access: its own guard policy must not be
	// automatically derived from the lockdown step's min_integrity/repos outputs.
	assert.NotContains(t, lock, "GITHUB_MCP_GUARD_MIN_INTEGRITY: ${{ steps.determine-automatic-lockdown.outputs.min_integrity }}")
	assert.NotContains(t, lock, "GITHUB_MCP_GUARD_REPOS: ${{ steps.determine-automatic-lockdown.outputs.repos }}")

	// The enclave identity retains its scoped repository access, and the static write-sink
	// retains its narrow accepted secrecy labels.
	assert.Contains(t, lock, `"private:githubnext/gh-aw-enclave-demo-private"`)
	assert.Contains(t, lock, `"allow-only":{"min-integrity":"none","repos":["githubnext/gh-aw-enclave-demo-private"]}`)
}

// TestBuildMCPGatewayConfigForcePublicReposForStaticEnclave verifies that the gateway's
// runtime public-repos override is disabled when the GitHub MCP server exists solely to
// serve a static enclave agent identity, and left at its default when the primary agent
// also has GitHub MCP access.
func TestBuildMCPGatewayConfigForcePublicReposForStaticEnclave(t *testing.T) {
	t.Run("enclave-only GitHub backend disables the override", func(t *testing.T) {
		data := enclaveGitHubToolsWorkflowData()
		delete(data.Tools, "github")
		data.ExplicitlyDisabledTools = map[string]struct{}{"github": {}}

		cfg := buildMCPGatewayConfig(data)
		require.NotNil(t, cfg)
		require.NotNil(t, cfg.ForcePublicRepos, "ForcePublicRepos must be set for an enclave-only GitHub backend")
		assert.False(t, *cfg.ForcePublicRepos)
	})

	t.Run("primary GitHub access keeps the override enabled", func(t *testing.T) {
		data := enclaveGitHubToolsWorkflowData()
		data.Tools["github"] = map[string]any{}

		cfg := buildMCPGatewayConfig(data)
		require.NotNil(t, cfg)
		assert.Nil(t, cfg.ForcePublicRepos, "ForcePublicRepos must stay at the gateway default when the primary agent reads GitHub")
	})

	t.Run("workflows without enclaves keep the override enabled", func(t *testing.T) {
		data := enclaveWorkflowData(true, false, 120, 0)
		data.Tools["github"] = map[string]any{}

		cfg := buildMCPGatewayConfig(data)
		require.NotNil(t, cfg)
		assert.Nil(t, cfg.ForcePublicRepos)
	})
}

func TestGitHubGuardPoliciesFromStepSkipsEnclaveOnlyBackend(t *testing.T) {
	data := enclaveGitHubToolsWorkflowData()
	// applyDefaultTools removes the "github" key when tools.github is false; the explicit
	// refusal survives in ExplicitlyDisabledTools.
	delete(data.Tools, "github")
	data.ExplicitlyDisabledTools = map[string]struct{}{"github": {}}

	// The determine-automatic-lockdown step is still generated for an enclave-only static
	// backend, solely to supply GH_AW_SINK_VISIBILITY for the write-sink policy — but its
	// min_integrity/repos outputs must never drive the primary GitHub MCP server's guard
	// policy, which stays derived statically from the enclave declaration.
	assert.True(t, githubLockdownDetectionStepEnabled(data))
	assert.False(t, githubGuardPoliciesFromStep(data, nil))
	assert.True(t, githubBackendIsStaticEnclaveDelegationOnly(data))

	data.Tools["github"] = map[string]any{}
	assert.True(t, githubLockdownDetectionStepEnabled(data))
	assert.True(t, githubGuardPoliciesFromStep(data, nil))
	assert.False(t, githubBackendIsStaticEnclaveDelegationOnly(data))
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

// TestStaticEnclaveGitHubScopeOverrideWarning verifies that combining a static GitHub agent
// enclave with primary-agent GitHub access is reported at compile time, because the gateway's
// forcePublicRepos override cannot be disabled without also relaxing the primary read path.
func TestStaticEnclaveGitHubScopeOverrideWarning(t *testing.T) {
	data := enclaveGitHubToolsWorkflowData()
	delete(data.Tools, "github")
	data.ExplicitlyDisabledTools = map[string]struct{}{"github": {}}
	assert.Empty(t, staticEnclaveGitHubScopeOverrideWarning(data))

	data.Tools["github"] = map[string]any{}
	assert.Contains(t, staticEnclaveGitHubScopeOverrideWarning(data), "static GitHub agent enclave is combined with primary 'tools.github'")

	assert.Empty(t, staticEnclaveGitHubScopeOverrideWarning(enclaveWorkflowData(true, false, 120, 0)))
}

func TestStaticEnclaveGitHubScopeOverrideWarningIncrementsWarningCount(t *testing.T) {
	compiler := NewCompiler()
	data := enclaveGitHubToolsWorkflowData()
	data.Tools["github"] = map[string]any{}

	require.NoError(t, compiler.validateCoreToolConfiguration(data, ""))

	assert.Equal(t, 1, compiler.GetWarningCount())
}
