//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func autoModelFixture(engineYAML, modelLine, extra string) string {
	var b strings.Builder
	b.WriteString("---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\n")
	b.WriteString(engineYAML)
	if !strings.HasSuffix(engineYAML, "\n") {
		b.WriteByte('\n')
	}
	if modelLine != "" {
		b.WriteString(modelLine)
		if !strings.HasSuffix(modelLine, "\n") {
			b.WriteByte('\n')
		}
	}
	if extra != "" {
		b.WriteString(extra)
		if !strings.HasSuffix(extra, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("---\nInspect the repository.\n")
	return b.String()
}

func TestAutoModelRouting_EligibleEnginesRouteAsRouter(t *testing.T) {
	cases := []struct {
		name                string
		engineYAML          string
		modelLine           string
		wantProvider        LLMProvider
		wantAddedProvider   bool
		wantEndpoints       []any
		equivalentRouterSrc string
	}{
		{
			name:                "copilot with model: auto",
			engineYAML:          "engine: copilot",
			modelLine:           "model: auto",
			wantProvider:        "",
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/responses", "/chat/completions"},
			equivalentRouterSrc: autoModelFixture("engine: copilot", "model: router", ""),
		},
		{
			name:                "copilot with model: copilot/auto",
			engineYAML:          "engine: copilot",
			modelLine:           "model: copilot/auto",
			wantProvider:        "",
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/responses", "/chat/completions"},
			equivalentRouterSrc: autoModelFixture("engine: copilot", "model: router", ""),
		},
		{
			name:                "pi with model: auto",
			engineYAML:          "engine: pi",
			modelLine:           "model: auto",
			wantProvider:        "",
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/v1/messages", "/responses", "/chat/completions"},
			equivalentRouterSrc: autoModelFixture("engine: pi", "model: router", ""),
		},
		{
			name:                "pi with model: copilot/auto",
			engineYAML:          "engine: pi",
			modelLine:           "model: copilot/auto",
			wantProvider:        "",
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/v1/messages", "/responses", "/chat/completions"},
			equivalentRouterSrc: autoModelFixture("engine: pi", "model: router", ""),
		},
		{
			name:                "claude with model: copilot/auto infers github provider",
			engineYAML:          "engine: claude",
			modelLine:           "model: copilot/auto",
			wantProvider:        LLMProviderGitHub,
			wantAddedProvider:   true,
			wantEndpoints:       []any{"/v1/messages"},
			equivalentRouterSrc: autoModelFixture("engine:\n  id: claude\n  model-provider: github", "model: router", ""),
		},
		{
			name:                "claude with model-provider: github and model: auto",
			engineYAML:          "engine:\n  id: claude\n  model-provider: github",
			modelLine:           "model: auto",
			wantProvider:        LLMProviderGitHub,
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/v1/messages"},
			equivalentRouterSrc: autoModelFixture("engine:\n  id: claude\n  model-provider: github", "model: router", ""),
		},
		{
			name:                "codex with model: copilot/auto infers github provider",
			engineYAML:          "engine: codex",
			modelLine:           "model: copilot/auto",
			wantProvider:        LLMProviderGitHub,
			wantAddedProvider:   true,
			wantEndpoints:       []any{"/responses"},
			equivalentRouterSrc: autoModelFixture("engine:\n  id: codex\n  model-provider: github", "model: router", ""),
		},
		{
			name:                "codex with model-provider: github and model: auto",
			engineYAML:          "engine:\n  id: codex\n  model-provider: github",
			modelLine:           "model: auto",
			wantProvider:        LLMProviderGitHub,
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/responses"},
			equivalentRouterSrc: autoModelFixture("engine:\n  id: codex\n  model-provider: github", "model: router", ""),
		},
		{
			name:                "engine.model: auto on copilot",
			engineYAML:          "engine:\n  id: copilot\n  model: auto",
			modelLine:           "",
			wantProvider:        "",
			wantAddedProvider:   false,
			wantEndpoints:       []any{"/responses", "/chat/completions"},
			equivalentRouterSrc: autoModelFixture("engine:\n  id: copilot\n  model: router", "", ""),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, autoModelFixture(tc.engineYAML, tc.modelLine, ""))
			require.NoError(t, compiled.err)
			require.True(t, isModelRoutingEnabled(compiled.data), "eligible model: auto must enable routing")
			require.False(t, isModelRoutingImplicit(compiled.data), "authored model: auto routes unconditionally like model: router, not runtime-guarded implicit default")
			require.Equal(t, routerModelGoal, compiled.data.EngineConfig.ModelRouting.Goal)
			require.Equal(t, defaultModelRoutingMode, compiled.data.EngineConfig.ModelRouting.Mode)
			require.Equal(t, "router", compiled.data.EngineConfig.ModelRouting.requested)
			require.Empty(t, compiled.data.Model, "routed model: auto clears data.Model just like model: router")
			require.Equal(t, tc.wantProvider, compiled.data.EngineConfig.LLMProvider)

			require.NotNil(t, compiled.data.AutoModelRouting)
			require.True(t, compiled.data.AutoModelRouting.Routed)
			require.Equal(t, tc.wantAddedProvider, compiled.data.AutoModelRouting.AddedGitHubProvider)
			require.Positive(t, compiled.warnings, "deprecation warning must be counted")
			require.Contains(t, compiled.stderr, "is deprecated and now routes as model: router")
			require.NotContains(t, compiled.stderr, defaultRoutingNoticeMarker, "must not emit omitted-model default notice")
			if tc.wantAddedProvider {
				require.Contains(t, compiled.stderr, "engine.model-provider: github")
			}

			configs := compiled.awfConfigs(t)
			require.Len(t, configs, 1, "unconditional routing writes a single routed AWF config")
			routing := configs[0]["apiProxy"].(map[string]any)["routing"].(map[string]any)
			require.Equal(t, map[string]any{"goal": "cost", "mode": "auto"}, routing["objective"])
			require.Equal(t, tc.wantEndpoints, routing["endpoints"])
			require.NotContains(t, routing, "provider", "GitHub Copilot routing omits native provider")

			// Verify parity with the equivalent `model: router` workflow.
			routerCompiled := compileDefaultRoutingSource(t, tc.equivalentRouterSrc)
			require.NoError(t, routerCompiled.err)
			require.Equal(t, routerCompiled.awfConfigs(t), configs, "auto->router AWF config must match equivalent model: router workflow")
		})
	}
}

func TestAutoModelRouting_IneligibleKeepsAutoWithWarning(t *testing.T) {
	cases := []struct {
		name           string
		engineYAML     string
		modelLine      string
		extra          string
		wantModel      string
		wantProvider   LLMProvider
		wantReasonPart string
	}{
		{
			name:           "top-level model-routing: false",
			engineYAML:     "engine: copilot",
			modelLine:      "model: auto",
			extra:          "model-routing: false",
			wantModel:      "auto",
			wantReasonPart: "model-routing is false",
		},
		{
			name:           "engine.fallback-models configured",
			engineYAML:     "engine:\n  id: copilot\n  fallback-models: [copilot/gpt-5.6-luna]",
			modelLine:      "model: copilot/auto",
			wantModel:      "copilot/auto",
			wantReasonPart: "fallback models",
		},
		{
			name:           "engine.model-catalog: false",
			engineYAML:     "engine:\n  id: copilot\n  model-catalog: false",
			modelLine:      "model: auto",
			wantModel:      "auto",
			wantReasonPart: "model-catalog is false",
		},
		{
			name:           "claude native provider with bare auto",
			engineYAML:     "engine: claude",
			modelLine:      "model: auto",
			wantModel:      "auto",
			wantProvider:   "",
			wantReasonPart: "not GitHub Copilot",
		},
		{
			name:           "codex native provider with bare auto",
			engineYAML:     "engine: codex",
			modelLine:      "model: auto",
			wantModel:      "auto",
			wantProvider:   "",
			wantReasonPart: "not GitHub Copilot",
		},
		{
			name:           "claude explicit anthropic provider with copilot/auto is not overwritten",
			engineYAML:     "engine:\n  id: claude\n  model-provider: anthropic",
			modelLine:      "model: copilot/auto",
			wantModel:      "copilot/auto",
			wantProvider:   LLMProviderAnthropic,
			wantReasonPart: "not GitHub Copilot",
		},
		{
			name:           "unsupported engine gemini",
			engineYAML:     "engine: gemini",
			modelLine:      "model: auto",
			wantModel:      "auto",
			wantReasonPart: "unsupported engine",
		},
		{
			name:           "AWF pinned older than fallback release",
			engineYAML:     "engine: copilot",
			modelLine:      "model: auto",
			extra:          "sandbox:\n  agent:\n    version: v0.29.20",
			wantModel:      "auto",
			wantReasonPart: "v0.29.21",
		},
		{
			name:           "AWF pinned to latest",
			engineYAML:     "engine: copilot",
			modelLine:      "model: auto",
			extra:          "sandbox:\n  agent:\n    version: latest",
			wantModel:      "auto",
			wantReasonPart: "v0.29.21",
		},
		{
			name:       "custom apiProxy image older than fallback release",
			engineYAML: "engine: copilot",
			modelLine:  "model: auto",
			extra: `sandbox:
  agent:
    images:
      squid: ghcr.io/github/gh-aw-firewall/squid:0.29.20@sha256:0000000000000000000000000000000000000000000000000000000000000000
      agent: ghcr.io/github/gh-aw-firewall/agent:0.29.20@sha256:0000000000000000000000000000000000000000000000000000000000000000
      api-proxy: ghcr.io/github/gh-aw-firewall/api-proxy:0.29.20@sha256:0000000000000000000000000000000000000000000000000000000000000000`,
			wantModel:      "auto",
			wantReasonPart: "custom AWF",
		},
		{
			name:           "stage-2 rollback on claude copilot/auto with incompatible models.allowed",
			engineYAML:     "engine: claude",
			modelLine:      "model: copilot/auto",
			extra:          "models:\n  allowed: [gpt-5.6-luna]",
			wantModel:      "copilot/auto",
			wantProvider:   "",
			wantReasonPart: "excluded by models.allowed",
		},
		{
			name:           "stage-2 rollback on codex copilot/auto with incompatible models.blocked",
			engineYAML:     "engine: codex",
			modelLine:      "model: copilot/auto",
			extra:          "models:\n  blocked: [\"github-copilot/gpt-*\"]",
			wantModel:      "copilot/auto",
			wantProvider:   "",
			wantReasonPart: "excluded by models.allowed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, autoModelFixture(tc.engineYAML, tc.modelLine, tc.extra))
			require.NoError(t, compiled.err, "ineligible model: auto must keep compiling cleanly")
			require.False(t, isModelRoutingEnabled(compiled.data), "ineligible model: auto must not enable routing")
			require.Equal(t, tc.wantModel, compiled.data.Model, "rollback must preserve authored model")
			require.Equal(t, tc.wantProvider, compiled.data.EngineConfig.LLMProvider, "rollback must restore original LLMProvider")

			require.NotNil(t, compiled.data.AutoModelRouting)
			require.False(t, compiled.data.AutoModelRouting.Routed)
			require.Contains(t, compiled.data.AutoModelRouting.Reason, tc.wantReasonPart)
			require.Positive(t, compiled.warnings, "deprecation warning must still be emitted")
			require.Contains(t, compiled.stderr, "kept existing auto behavior because")
			require.Contains(t, compiled.stderr, tc.wantReasonPart)
		})
	}
}

func TestAutoModelRouting_UntouchedAndSilentCases(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		wantRouted  bool
		wantModel   string
		wantNoAutoW bool
	}{
		{
			name:        "omitted model uses implicit default with zero warnings",
			files:       map[string]string{"main.md": autoModelFixture("engine: copilot", "", "")},
			wantRouted:  true,
			wantModel:   "",
			wantNoAutoW: true,
		},
		{
			name:        "concrete model",
			files:       map[string]string{"main.md": autoModelFixture("engine: copilot", "model: gpt-5.6-luna", "")},
			wantRouted:  false,
			wantModel:   "gpt-5.6-luna",
			wantNoAutoW: true,
		},
		{
			name:        "parameterized auto?effort=high",
			files:       map[string]string{"main.md": autoModelFixture("engine: copilot", "model: auto?effort=high", "")},
			wantRouted:  false,
			wantModel:   "auto?effort=high",
			wantNoAutoW: true,
		},
		{
			name:        "parameterized copilot/auto?effort=high",
			files:       map[string]string{"main.md": autoModelFixture("engine: claude", "model: copilot/auto?effort=high", "")},
			wantRouted:  false,
			wantModel:   "copilot/auto?effort=high",
			wantNoAutoW: true,
		},
		{
			name:        "workflow-defined models.auto alias",
			files:       map[string]string{"main.md": autoModelFixture("engine: copilot", "model: auto", "models:\n  auto: [copilot/gpt-5.6-luna]")},
			wantRouted:  false,
			wantModel:   "auto",
			wantNoAutoW: true,
		},
		{
			name: "imported models.auto alias",
			files: map[string]string{
				"shared/aliases.md": "---\nmodels:\n  auto: [copilot/gpt-5.6-luna]\n---\n",
				"main.md":           autoModelFixture("engine: copilot", "model: auto", "imports:\n  - shared/aliases.md"),
			},
			wantRouted:  false,
			wantModel:   "auto",
			wantNoAutoW: true,
		},
		{
			name:        "threat-detection auto model does not warn",
			files:       map[string]string{"main.md": autoModelFixture("engine: copilot", "model: gpt-5.6-luna", "safe-outputs:\n  noop:\n  threat-detection:\n    model: auto")},
			wantRouted:  false,
			wantModel:   "gpt-5.6-luna",
			wantNoAutoW: true,
		},
		{
			name:        "evals auto model does not warn",
			files:       map[string]string{"main.md": autoModelFixture("engine: copilot", "model: gpt-5.6-luna", "evals:\n  model: auto\n  questions:\n    - id: check\n      question: Did it succeed?")},
			wantRouted:  false,
			wantModel:   "gpt-5.6-luna",
			wantNoAutoW: true,
		},
		{
			name:        "routing mode: auto does not trigger model: auto warning",
			files:       map[string]string{"main.md": autoModelFixture("engine:\n  id: copilot\n  model-routing:\n    goal: cost\n    mode: auto", "", "")},
			wantRouted:  true,
			wantModel:   "",
			wantNoAutoW: true,
		},
		{
			name: "sub-agent auto model does not warn",
			files: map[string]string{
				"main.md": autoModelFixture("engine: copilot", "model: gpt-5.6-luna", "") + "\n---\nagent: helper\nmodel: auto\n---\nHelp.\n",
			},
			wantRouted:  false,
			wantModel:   "gpt-5.6-luna",
			wantNoAutoW: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileDefaultRoutingFiles(t, tc.files)
			require.NoError(t, compiled.err)
			require.Equal(t, tc.wantRouted, isModelRoutingEnabled(compiled.data))
			require.Equal(t, tc.wantModel, compiled.data.Model)
			require.Nil(t, compiled.data.AutoModelRouting, "must not trigger model: auto handling")
			require.NotContains(t, compiled.stderr, "model: auto is deprecated")
			require.NotContains(t, compiled.stderr, "model: copilot/auto is deprecated")
		})
	}
}

func TestAutoModelRouting_StrictModeKeepsDeprecationAsWarning(t *testing.T) {
	for _, tc := range []struct {
		name       string
		extra      string
		wantRouted bool
	}{
		{name: "strict mode routed model: auto", extra: "strict: true", wantRouted: true},
		{name: "strict mode non-routed model: auto", extra: "strict: true\nmodel-routing: false", wantRouted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, autoModelFixture("engine: copilot", "model: auto", tc.extra))
			require.NoError(t, compiled.err, "strict: true must keep model: auto deprecation as a warning, not a compile error")
			require.Equal(t, tc.wantRouted, isModelRoutingEnabled(compiled.data))
			require.Positive(t, compiled.warnings)
			require.Contains(t, compiled.stderr, "model: auto is deprecated")
		})
	}
}

func TestDefaultAndDynamicRouting_FallbackVersionBoundary(t *testing.T) {
	t.Run("implicit default skips older AWF without fallback", func(t *testing.T) {
		compiled := compileDefaultRoutingSource(t, autoModelFixture("engine: copilot", "", "sandbox:\n  agent:\n    version: v0.29.20"))
		require.NoError(t, compiled.err)
		requireNoDefaultRouting(t, compiled)
		require.Zero(t, compiled.warnings)
	})

	t.Run("implicit default routes on v0.29.21", func(t *testing.T) {
		compiled := compileDefaultRoutingSource(t, autoModelFixture("engine: copilot", "", "sandbox:\n  agent:\n    version: v0.29.21"))
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingImplicit(compiled.data))
	})

	t.Run("dynamic expression routing still works on older AWF v0.29.20", func(t *testing.T) {
		src := `---
on:
  workflow_dispatch:
    inputs:
      level:
        type: string
        default: router/balanced
permissions:
  contents: read
  copilot-requests: write
engine: copilot
model: ${{ inputs.level }}
sandbox:
  agent:
    version: v0.29.20
---
Inspect the repository.
`
		compiled := compileDefaultRoutingSource(t, src)
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingDynamic(compiled.data), "dynamic expression routing must stay enabled on older AWF pins")
		require.NotContains(t, compiled.stderr, "deprecated")
		configs := compiled.awfConfigs(t)
		require.Len(t, configs, 2)
		routing := configs[0]["apiProxy"].(map[string]any)["routing"].(map[string]any)
		require.NotContains(t, routing, "endpoints", "older v0.29.20 omits apiProxy.routing.endpoints")
	})
}

func TestAutoModelRouting_ImportedAutoUsesImportSpecificWarning(t *testing.T) {
	t.Run("imported copilot auto", func(t *testing.T) {
		compiled := compileDefaultRoutingFiles(t, map[string]string{
			"shared/base.md": "---\nengine:\n  id: copilot\n  model: auto\n---\n",
			"main.md":        "---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\nimports:\n  - shared/base.md\n---\nInspect the repository.\n",
		})
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingEnabled(compiled.data))
		require.NotNil(t, compiled.data.AutoModelRouting)
		require.True(t, compiled.data.AutoModelRouting.FromImport)
		require.True(t, compiled.data.AutoModelRouting.Routed)
		require.Contains(t, compiled.stderr, "imported model: auto is deprecated and now routes as model: router")
		require.Contains(t, compiled.stderr, "Set model: router in this workflow (or check all consumers before updating the imported workflow)")
		require.NotContains(t, compiled.stderr, "gh aw fix --write")
	})

	t.Run("local claude engine with imported copilot/auto model", func(t *testing.T) {
		compiled := compileDefaultRoutingFiles(t, map[string]string{
			"shared/model.md": "---\nmodel: copilot/auto\n---\n",
			"main.md":         "---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\nengine: claude\nimports:\n  - shared/model.md\n---\nInspect the repository.\n",
		})
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingEnabled(compiled.data))
		require.Equal(t, LLMProviderGitHub, compiled.data.EngineConfig.LLMProvider)
		require.NotNil(t, compiled.data.AutoModelRouting)
		require.True(t, compiled.data.AutoModelRouting.FromImport)
		require.True(t, compiled.data.AutoModelRouting.AddedGitHubProvider)
		require.Contains(t, compiled.stderr, "engine.model-provider: github on the engine declaration (or check all consumers before updating the imported workflow)")
		require.NotContains(t, compiled.stderr, "gh aw fix --write")
	})

	t.Run("local copilot/auto with imported claude engine omits gh aw fix recommendation", func(t *testing.T) {
		compiled := compileDefaultRoutingFiles(t, map[string]string{
			"shared/claude.md": "---\nengine: claude\n---\n",
			"main.md":          "---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\nimports:\n  - shared/claude.md\nmodel: copilot/auto\n---\nInspect the repository.\n",
		})
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingEnabled(compiled.data))
		require.Equal(t, LLMProviderGitHub, compiled.data.EngineConfig.LLMProvider)
		require.NotNil(t, compiled.data.AutoModelRouting)
		require.False(t, compiled.data.AutoModelRouting.FromImport)
		require.True(t, compiled.data.AutoModelRouting.AddedGitHubProvider)
		require.False(t, compiled.data.AutoModelRouting.CanAutoFix)
		require.Contains(t, compiled.stderr, "inherited from an import")
		require.Contains(t, compiled.stderr, "switch to model: router manually")
		require.NotContains(t, compiled.stderr, "gh aw fix --write")
	})
}

func TestAutoModelRouting_AlreadyConfiguredRoutingPreservedAndReported(t *testing.T) {
	src := autoModelFixture(
		"engine:\n  id: copilot\n  model-routing:\n    goal: cost\n    mode: economy\n    allowed-models: [gpt-5.6-luna]",
		"model: auto",
		"",
	)
	compiled := compileDefaultRoutingSource(t, src)
	require.NoError(t, compiled.err)
	require.True(t, isModelRoutingEnabled(compiled.data))
	require.Equal(t, "economy", compiled.data.EngineConfig.ModelRouting.Mode, "existing configured mode must be preserved")
	require.Equal(t, []string{"gpt-5.6-luna"}, compiled.data.EngineConfig.ModelRouting.AllowedModels, "existing shortlist must be preserved")
	require.NotNil(t, compiled.data.AutoModelRouting)
	require.True(t, compiled.data.AutoModelRouting.AlreadyConfiguredRouting)
	require.False(t, compiled.data.AutoModelRouting.Routed)
	require.Contains(t, compiled.stderr, "is overridden by the existing model-routing configuration")
	require.Contains(t, compiled.stderr, "preserving your configured mode and shortlist")
	require.NotContains(t, compiled.stderr, "kept existing auto behavior")
}

func TestAutoModelRouting_CompileToYAMLReportsStructuredDiagnostic(t *testing.T) {
	src := autoModelFixture("engine: copilot", "model: auto", "")
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))

	data, err := compiler.ParseWorkflowString(src, "workflow.md")
	require.NoError(t, err)
	require.Zero(t, compiler.GetWarningCount(), "parse-only calls must stay silent")
	require.Empty(t, compiler.GetDiagnostics(), "parse-only calls must not append diagnostics")

	_, err = compiler.CompileToYAML(data, "workflow.md")
	require.NoError(t, err)
	require.Positive(t, compiler.GetWarningCount())
	var found *CompilerDiagnostic
	for i := range compiler.GetDiagnostics() {
		diag := compiler.GetDiagnostics()[i]
		if strings.Contains(diag.Message, "model: auto is deprecated") {
			found = &diag
			break
		}
	}
	require.NotNil(t, found, "CompileToYAML must record the model: auto deprecation warning as a structured diagnostic")
	require.Equal(t, "warning", found.Level)
	require.Equal(t, "workflow.md", found.Path)
	require.Contains(t, found.Message, "now routes as model: router")
}

func TestAutoModelRouting_PreservesThreatDetectionCopilotProvider(t *testing.T) {
	t.Run("copilot/auto with explicit claude detector stays non-routed so detector keeps Copilot credentials", func(t *testing.T) {
		src := autoModelFixture("engine: copilot", "model: copilot/auto", "safe-outputs:\n  create-issue:\n  threat-detection:\n    engine: claude")
		compiled := compileDefaultRoutingSource(t, src)
		require.NoError(t, compiled.err)
		require.False(t, isModelRoutingEnabled(compiled.data))
		require.Equal(t, "copilot/auto", compiled.data.Model)
		require.NotNil(t, compiled.data.AutoModelRouting)
		require.False(t, compiled.data.AutoModelRouting.Routed)
		require.Contains(t, compiled.stderr, "safe-outputs.threat-detection.engine.model-provider: github")
		detectionJob := extractJobSection(compiled.lock, "detection")
		require.Contains(t, detectionJob, "GH_AW_LLM_PROVIDER: github")
		require.NotContains(t, detectionJob, "secrets.ANTHROPIC_API_KEY")
	})

	t.Run("copilot/auto with explicit claude detector pinning model-provider: github routes main agent", func(t *testing.T) {
		src := autoModelFixture("engine: copilot", "model: copilot/auto", "safe-outputs:\n  create-issue:\n  threat-detection:\n    engine:\n      id: claude\n      model-provider: github")
		compiled := compileDefaultRoutingSource(t, src)
		require.NoError(t, compiled.err)
		require.True(t, isModelRoutingEnabled(compiled.data))
		detectionJob := extractJobSection(compiled.lock, "detection")
		require.Contains(t, detectionJob, "GH_AW_LLM_PROVIDER: github")
		require.NotContains(t, detectionJob, "secrets.ANTHROPIC_API_KEY")
	})
}
