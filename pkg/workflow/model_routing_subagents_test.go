package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestBuildAWFConfigJSON_SubAgentRoutingPolicy(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "copilot", ModelRouting: &CopilotModelRoutingConfig{
			Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-6-sol"},
		}},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.33"}},
		ModelMappings:      map[string][]string{"small": {"copilot/*haiku*"}},
		ModelPolicyBlocked: []string{"gpt-5.4-mini"},
		SubAgentModels: []parser.SubAgentModel{
			{Name: "concrete", Model: "claude-haiku-4.5"},
			{Name: "alias", Model: "small"},
			{Name: "blocked", Model: "gpt-5.4-mini"},
		},
	}
	build := func() map[string]any {
		t.Helper()
		configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
			EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data,
		})
		require.NoError(t, err)
		require.NoError(t, validateAWFConfigJSON(configJSON))
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
		return config["apiProxy"].(map[string]any)
	}

	proxy := build()
	require.NotContains(t, proxy["routing"].(map[string]any), "candidateModels")
	require.Equal(t, []any{"github-copilot/gpt-6-sol"}, proxy["allowedModels"])

	setRoutingCandidateModelsProxyImage(data, "0.28.33")
	proxy = build()
	require.Equal(t, []any{"github-copilot/gpt-6-sol"}, proxy["routing"].(map[string]any)["candidateModels"])
	require.Equal(t, []any{"github-copilot/gpt-6-sol", "github-copilot/claude-haiku-4.5", "github-copilot/*haiku*"}, proxy["allowedModels"])
	_, warnings := subAgentRequestModels(data, []string{"github-copilot/gpt-6-sol"}, nil, data.ModelPolicyBlocked)
	require.Equal(t, []string{`sub-agent "blocked" model "gpt-5.4-mini" cannot be admitted by models.allowed or models.blocked (or resolves to no models)`}, warnings)

	allowed, warnings := subAgentRequestModels(data, []string{"github-copilot/gpt-6-sol"},
		[]string{"gpt-6-sol", "claude-haiku-4.5"}, data.ModelPolicyBlocked)
	require.Equal(t, []string{"github-copilot/gpt-6-sol", "github-copilot/claude-haiku-4.5"}, allowed)
	require.Len(t, warnings, 1)

	setRoutingCandidateModelsProxyImage(data, "0.28.31")
	proxy = build()
	require.NotContains(t, proxy["routing"].(map[string]any), "candidateModels")
	require.Equal(t, []any{"github-copilot/gpt-6-sol"}, proxy["allowedModels"])
	stderr := testutil.CaptureStderr(t, func() {
		(&Compiler{}).warnRoutedSubAgentModels(data)
	})
	require.Contains(t, stderr, "configured image is older or its version is unknown")
	setRoutingCandidateModelsProxyImage(data, "latest")
	require.False(t, apiProxySupportsRoutingCandidateModels(data))

	setRoutingCandidateModelsProxyImage(data, "0.28.33")
	data.NetworkPermissions.Firewall.Version = "v0.28.31"
	proxy = build()
	require.NotContains(t, proxy["routing"].(map[string]any), "candidateModels")
	require.Equal(t, []any{"github-copilot/gpt-6-sol"}, proxy["allowedModels"])
	stderr = testutil.CaptureStderr(t, func() {
		(&Compiler{}).warnRoutedSubAgentModels(data)
	})
	require.Contains(t, stderr, "sub-agent models are limited to engine.model-routing.allowed-models")
}

func setRoutingCandidateModelsProxyImage(data *WorkflowData, version string) {
	if data.SandboxConfig == nil {
		data.SandboxConfig = &SandboxConfig{}
	}
	if data.SandboxConfig.Agent == nil {
		data.SandboxConfig.Agent = &AgentSandboxConfig{}
	}
	if data.SandboxConfig.Agent.Images == nil {
		data.SandboxConfig.Agent.Images = make(map[string]string)
	}
	data.SandboxConfig.Agent.Images[awfImageRoleAPIProxy] = "ghcr.io/github/gh-aw-firewall/api-proxy:" + version + "@sha256:" + strings.Repeat("a", 64)
}

func TestRoutedInlineSubAgentModelsAreRetainedAndWarned(t *testing.T) {
	source := filepath.Join(t.TempDir(), "routed.md")
	shared := filepath.Join(filepath.Dir(source), "shared.md")
	require.NoError(t, os.WriteFile(shared, []byte("# Shared\n\n## agent: `imported`\n---\nmodel: gpt-5.6-luna\n---\nWork.\n"), 0600))
	agentDir := filepath.Join(filepath.Dir(source), ".github", "agents")
	require.NoError(t, os.MkdirAll(agentDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "external.md"), []byte("---\nmodel: claude-sonnet-4.6\n---\nWork.\n"), 0600))
	content := `---
on: issues
permissions:
  contents: read
strict: false
imports:
  - shared.md
  - .github/agents/external.md
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: balanced
    allowed-models: [gpt-6-sol]
sandbox:
  agent:
    id: awf
    version: v0.28.33
models:
  blocked: [gpt-5.4-mini]
---
# Routed workflow

## agent: ` + "`concrete`" + `
---
model: claude-haiku-4.5
---
Work.

## agent: ` + "`alias`" + `
---
model: small
---
Work.

## agent: ` + "`blocked`" + `
---
model: gpt-5.4-mini
---
Work.
`
	require.NoError(t, os.WriteFile(source, []byte(content), 0600))
	compiler := NewCompiler()
	var data *WorkflowData
	var err error
	stderr := testutil.CaptureStderr(t, func() {
		data, err = compiler.ParseWorkflowFile(source)
	})
	require.NoError(t, err)
	require.Equal(t, []parser.SubAgentModel{
		{Name: "concrete", Model: "claude-haiku-4.5"},
		{Name: "alias", Model: "small"},
		{Name: "blocked", Model: "gpt-5.4-mini"},
		{Name: "imported", Model: "gpt-5.6-luna"},
		{Name: "external.md", Model: "claude-sonnet-4.6"},
	}, data.SubAgentModels)
	require.Contains(t, stderr, `sub-agent "blocked" model "gpt-5.4-mini"`)
	require.Contains(t, stderr, "configured image is older or its version is unknown")
	require.GreaterOrEqual(t, compiler.GetWarningCount(), 1)
	setRoutingCandidateModelsProxyImage(data, "0.28.33")
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data,
	})
	require.NoError(t, err)
	require.Contains(t, configJSON, `"candidateModels":["github-copilot/gpt-6-sol"]`)
	require.Contains(t, configJSON, `"github-copilot/gpt-5.6-luna"`)
	require.Contains(t, configJSON, `"github-copilot/claude-sonnet-4.6"`)
	require.NotContains(t, configJSON, `"github-copilot/gpt-5.4-mini"`)
}

func TestModelRoutingCandidateExpressionsAndImportedCandidates(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "copilot",
			ModelRouting: &CopilotModelRoutingConfig{
				Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-6-sol"},
			},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.33"}},
	}
	compiler := &Compiler{}
	data.EngineConfig = compiler.mergeImportedModelRoutingConfigs(data.EngineConfig, []string{
		`{"id":"copilot","model-routing":{"goal":"cost-speed","mode":"economy","allowed-models":["claude-haiku-4.5","${{ inputs.extra-model }}"]}}`,
	})
	setRoutingCandidateModelsProxyImage(data, "0.28.33")

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data,
	})
	require.NoError(t, err)
	require.NoError(t, validateAWFConfigJSON(configJSON))
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	apiProxy := config["apiProxy"].(map[string]any)
	require.Equal(t, []any{
		"github-copilot/gpt-6-sol",
		"github-copilot/claude-haiku-4.5",
		"github-copilot/${{ inputs.extra-model }}",
	}, apiProxy["routing"].(map[string]any)["candidateModels"])
	require.Equal(t, "cost", apiProxy["routing"].(map[string]any)["objective"].(map[string]any)["goal"])

	_, err = intersectModelRoutingPolicy(
		[]string{"github-copilot/${{ inputs.extra-model }}"},
		[]string{"gpt-6-sol"}, nil,
	)
	require.ErrorContains(t, err, "cannot be checked against models.allowed or models.blocked")
}

func TestImportedWorkflowModelRoutingCandidatesAreMerged(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared.md")
	source := filepath.Join(dir, "routed.md")
	require.NoError(t, os.WriteFile(shared, []byte(`---
engine:
  id: copilot
  model-routing:
    goal: cost-speed
    mode: economy
    allowed-models: [claude-haiku-4.5]
---
# Shared workflow
`), 0600))
	require.NoError(t, os.WriteFile(source, []byte(`---
on:
  workflow_dispatch:
    inputs:
      model:
        type: string
        required: true
strict: false
imports:
  - shared.md
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: balanced
    allowed-models: ["${{ inputs.model }}", gpt-6-sol]
sandbox:
  agent:
    id: awf
    version: v0.28.33
---
# Routed workflow
`), 0600))

	data, err := NewCompiler().ParseWorkflowFile(source)
	require.NoError(t, err)
	require.Equal(t, []string{"${{ inputs.model }}", "gpt-6-sol", "claude-haiku-4.5"}, data.EngineConfig.ModelRouting.AllowedModels)
	setRoutingCandidateModelsProxyImage(data, "0.28.33")

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data,
	})
	require.NoError(t, err)
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	require.Equal(t, []any{
		"github-copilot/${{ inputs.model }}",
		"github-copilot/gpt-6-sol",
		"github-copilot/claude-haiku-4.5",
	}, config["apiProxy"].(map[string]any)["routing"].(map[string]any)["candidateModels"])
}
