package workflow

import (
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func routedPolicyWorkflowData(engine, awfVersion string, allowed, blocked, candidates []string) *WorkflowData {
	return &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           engine,
			LLMProvider:  LLMProviderGitHub,
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: candidates},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: awfVersion}},
		ModelPolicyAllowed: allowed,
		ModelPolicyBlocked: blocked,
	}
}

func buildRoutedAPIProxyConfig(t *testing.T, engine string, data *WorkflowData) map[string]any {
	t.Helper()
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: engine, AllowedDomains: "github.com", WorkflowData: data})
	require.NoError(t, err)
	require.NoError(t, validateAWFConfigJSON(configJSON))
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	return config["apiProxy"].(map[string]any)
}

func anySlice(values []string) any {
	if values == nil {
		return nil
	}
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

// TestBuildAWFConfigJSON_ModelRoutingPolicyMatrix covers the eight combinations of
// models.allowed (A), models.blocked (B) and engine.model-routing.allowed-models (C).
func TestBuildAWFConfigJSON_ModelRoutingPolicyMatrix(t *testing.T) {
	a := []string{"gpt-5.6-sol", "gpt-5.4-mini", "claude-sonnet-5"}
	b := []string{"gpt-5.4-mini"}
	c := []string{"gpt-5.6-sol", "gpt-5.4-mini", "gpt-5.6-luna"}
	aMinusB := []string{"gpt-5.6-sol", "claude-sonnet-5"}

	for _, tc := range []struct {
		name           string
		engine         string
		allowed        []string
		blocked        []string
		candidates     []string
		wantAllowed    []string
		wantDisallowed []string
		wantCandidates []string
	}{
		{name: "row 1 copilot", engine: "copilot"},
		{name: "row 1 pi", engine: "pi"},
		{name: "row 1 claude", engine: "claude", wantCandidates: []string{"github-copilot/claude-*"}},
		{name: "row 1 codex", engine: "codex", wantCandidates: []string{"github-copilot/gpt-*"}},
		{name: "row 2 copilot", engine: "copilot", candidates: c,
			wantCandidates: []string{"github-copilot/gpt-5.6-sol", "github-copilot/gpt-5.4-mini", "github-copilot/gpt-5.6-luna"}},
		{name: "row 3 copilot", engine: "copilot", blocked: b, wantDisallowed: b},
		{name: "row 3 claude", engine: "claude", blocked: b, wantDisallowed: b, wantCandidates: []string{"github-copilot/claude-*"}},
		{name: "row 3 codex", engine: "codex", blocked: b, wantDisallowed: b, wantCandidates: []string{"github-copilot/gpt-*"}},
		{name: "row 4 copilot", engine: "copilot", blocked: b, candidates: c, wantDisallowed: b,
			wantCandidates: []string{"github-copilot/gpt-5.6-sol", "github-copilot/gpt-5.6-luna"}},
		{name: "row 5 copilot", engine: "copilot", allowed: a, wantAllowed: a},
		{name: "row 5 claude", engine: "claude", allowed: a, wantAllowed: a, wantCandidates: []string{"github-copilot/claude-*"}},
		{name: "row 5 codex", engine: "codex", allowed: a, wantAllowed: a, wantCandidates: []string{"github-copilot/gpt-*"}},
		{name: "row 6 copilot", engine: "copilot", allowed: a, candidates: c, wantAllowed: a,
			wantCandidates: []string{"github-copilot/gpt-5.6-sol", "github-copilot/gpt-5.4-mini"}},
		{name: "row 7 copilot", engine: "copilot", allowed: a, blocked: b, wantAllowed: aMinusB, wantDisallowed: b},
		{name: "row 7 claude", engine: "claude", allowed: a, blocked: b, wantAllowed: aMinusB, wantDisallowed: b,
			wantCandidates: []string{"github-copilot/claude-*"}},
		{name: "row 7 codex", engine: "codex", allowed: a, blocked: b, wantAllowed: aMinusB, wantDisallowed: b,
			wantCandidates: []string{"github-copilot/gpt-*"}},
		{name: "row 8 copilot", engine: "copilot", allowed: a, blocked: b, candidates: c, wantAllowed: aMinusB, wantDisallowed: b,
			wantCandidates: []string{"github-copilot/gpt-5.6-sol"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := routedPolicyWorkflowData(tc.engine, "", tc.allowed, tc.blocked, tc.candidates)
			require.True(t, apiProxySupportsRoutingCandidateModels(data))
			proxy := buildRoutedAPIProxyConfig(t, tc.engine, data)
			require.Equal(t, anySlice(tc.wantAllowed), proxy["allowedModels"], "allowedModels")
			require.Equal(t, anySlice(tc.wantDisallowed), proxy["disallowedModels"], "disallowedModels")
			routing := proxy["routing"].(map[string]any)
			if tc.wantCandidates == nil {
				require.NotContains(t, routing, "candidateModels", "an omitted candidate list must leave the key out, never an empty array")
			} else {
				require.Equal(t, anySlice(tc.wantCandidates), routing["candidateModels"])
			}
		})
	}
}

func TestNarrowModelRoutingCandidate_NamespacedPolicy(t *testing.T) {
	const candidate = "openrouter/anthropic/claude-opus-4"

	t.Run("blocked exact namespaced model", func(t *testing.T) {
		require.Empty(t, narrowModelRoutingCandidate(candidate, nil, []string{candidate}, false))
	})
	t.Run("allowed provider wildcard does not cross namespace segments", func(t *testing.T) {
		require.Empty(t, narrowModelRoutingCandidate(candidate, []string{"openrouter/*"}, nil, false))
	})
}

func TestBuildAWFConfigJSON_ModelRoutingPolicyErrors(t *testing.T) {
	t.Run("policy excludes every candidate", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "", []string{"gpt-5.6-sol"}, nil, []string{"gpt-5.4-mini"})
		_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data})
		require.ErrorContains(t, err, "all engine.model-routing.allowed-models are excluded by models.allowed or models.blocked policy")
	})
	t.Run("expression candidates with a policy", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "", nil, []string{"gpt-5.4-mini"}, []string{"${{ inputs.model }}"})
		_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data})
		require.ErrorContains(t, err, "cannot be checked against models.allowed or models.blocked")
	})
	for _, tc := range []struct {
		name    string
		engine  string
		allowed []string
		blocked []string
	}{
		{name: "claude family outside models.allowed", engine: "claude", allowed: []string{"gpt-5.6-sol"}},
		{name: "codex family outside models.allowed", engine: "codex", allowed: []string{"claude-sonnet-5", "gemini-*"}},
		{name: "claude allowed models all blocked", engine: "claude", allowed: []string{"claude-sonnet-5", "gpt-5.6-sol"}, blocked: []string{"claude-sonnet-*"}},
		{name: "claude family blocked", engine: "claude", blocked: []string{"claude-*"}},
		{name: "codex family blocked", engine: "codex", blocked: []string{"github-copilot/gpt*"}},
		{name: "codex family blocked by wildcard", engine: "codex", allowed: []string{"gpt-5.6-sol"}, blocked: []string{"*"}},
		{name: "claude family blocked by copilot/ alias", engine: "claude", blocked: []string{"copilot/claude-*"}},
		{name: "claude family blocked by github/ alias", engine: "claude", blocked: []string{"github/claude-*"}},
		{name: "claude family blocked case-insensitively", engine: "claude", blocked: []string{"Claude-*"}},
	} {
		t.Run("default family "+tc.name, func(t *testing.T) {
			data := routedPolicyWorkflowData(tc.engine, "", tc.allowed, tc.blocked, nil)
			const want = "all engine.model-routing.allowed-models are excluded by models.allowed or models.blocked policy"
			_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: tc.engine, AllowedDomains: "github.com", WorkflowData: data})
			require.ErrorContains(t, err, want)
			require.ErrorContains(t, err, "the "+tc.engine+" engine uses its model family")
			require.ErrorContains(t, validateModelRouting(data, tc.engine), want)
		})
	}
	for _, tc := range []struct {
		name    string
		engine  string
		allowed []string
		blocked []string
	}{
		{name: "claude family glob allowed", engine: "claude", allowed: []string{"*sonnet*"}},
		{name: "codex family glob allowed", engine: "codex", allowed: []string{"*-5.6-*"}},
		{name: "claude family partly blocked", engine: "claude", blocked: []string{"claude-opus-*", "claude-?"}},
		{name: "codex model allowed", engine: "codex", allowed: []string{"claude-sonnet-5", "github-copilot/gpt-5.6-sol"}},
		{name: "copilot is not family limited", engine: "copilot", allowed: []string{"claude-sonnet-5"}, blocked: []string{"gpt-*"}},
	} {
		t.Run("default family "+tc.name, func(t *testing.T) {
			data := routedPolicyWorkflowData(tc.engine, "", tc.allowed, tc.blocked, nil)
			_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: tc.engine, AllowedDomains: "github.com", WorkflowData: data})
			require.NoError(t, err)
			require.NoError(t, validateModelRouting(data, tc.engine))
		})
	}
	t.Run("literal family check still applies", func(t *testing.T) {
		data := routedPolicyWorkflowData("claude", "", nil, nil, []string{"gpt-5.6-sol"})
		_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "claude", AllowedDomains: "github.com", WorkflowData: data})
		require.ErrorContains(t, err, "native Messages API")
	})
}

func TestBuildAWFConfigJSON_ModelRoutingBeforeCandidateModels(t *testing.T) {
	t.Run("allow list is the candidates narrowed by policy", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "v0.28.31", []string{"gpt-5.6-sol", "gpt-5.4-mini"}, []string{"gpt-5.4-mini"},
			[]string{"gpt-5.6-sol", "gpt-5.4-mini", "gpt-5.6-luna"})
		require.False(t, apiProxySupportsRoutingCandidateModels(data))
		proxy := buildRoutedAPIProxyConfig(t, "copilot", data)
		require.Equal(t, []any{"github-copilot/gpt-5.6-sol"}, proxy["allowedModels"])
		require.Equal(t, []any{"gpt-5.4-mini"}, proxy["disallowedModels"])
		require.NotContains(t, proxy["routing"].(map[string]any), "candidateModels")
	})
	t.Run("allowed-models is required", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "v0.28.31", nil, nil, nil)
		_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data})
		require.ErrorContains(t, err, "missing model-routing candidates")
		require.ErrorContains(t, err, "v0.28.33")
	})
}

func TestWarnRoutedModelPolicy(t *testing.T) {
	warn := func(data *WorkflowData) (string, int) {
		compiler := &Compiler{}
		stderr := testutil.CaptureStderr(t, func() { compiler.warnRoutedSubAgentModels(data) })
		return stderr, compiler.GetWarningCount()
	}

	t.Run("policy removes some candidates", func(t *testing.T) {
		stderr, count := warn(routedPolicyWorkflowData("copilot", "", []string{"gpt-5.6-sol", "gpt-5.4-mini"}, []string{"gpt-5.4-mini"},
			[]string{"gpt-5.6-sol", "gpt-5.4-mini", "gpt-5.6-luna"}))
		require.Contains(t, stderr, "engine.model-routing.allowed-models entries excluded by models.allowed or models.blocked will not be selected: gpt-5.4-mini, gpt-5.6-luna")
		require.Equal(t, 1, count)
	})
	t.Run("policy keeps every candidate", func(t *testing.T) {
		stderr, count := warn(routedPolicyWorkflowData("copilot", "", []string{"gpt-5.6-sol"}, nil, []string{"gpt-5.6-sol"}))
		require.NotContains(t, stderr, "will not be selected")
		require.Zero(t, count)
	})
	t.Run("candidates omitted", func(t *testing.T) {
		stderr, count := warn(routedPolicyWorkflowData("copilot", "", []string{"gpt-5.6-sol"}, nil, nil))
		require.NotContains(t, stderr, "will not be selected")
		require.Zero(t, count)
	})

	subAgents := []parser.SubAgentModel{
		{Name: "inside", Model: "gpt-5.6-luna"},
		{Name: "outside", Model: "claude-haiku-4.5"},
	}
	t.Run("sub-agent model outside policy", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "", []string{"gpt-5.6-sol", "gpt-5.6-luna"}, nil, nil)
		data.SubAgentModels = subAgents
		stderr, count := warn(data)
		require.Contains(t, stderr, `sub-agent "outside" model "claude-haiku-4.5" is outside models.allowed or models.blocked`)
		require.Contains(t, stderr, "AWF will reject its requests")
		require.NotContains(t, stderr, `sub-agent "inside"`)
		require.Equal(t, 1, count)
	})
	t.Run("sub-agent model blocked", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "", nil, []string{"claude-*"}, nil)
		data.SubAgentModels = subAgents
		stderr, _ := warn(data)
		require.Contains(t, stderr, `sub-agent "outside" model "claude-haiku-4.5" is outside models.allowed or models.blocked`)
	})
	t.Run("sub-agent models without a policy", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "", nil, nil, []string{"gpt-5.6-sol"})
		data.SubAgentModels = subAgents
		stderr, count := warn(data)
		require.NotContains(t, stderr, "AWF will reject its requests")
		require.Zero(t, count)
	})
}
