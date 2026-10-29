//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// nativeRoutedWorkflowSource returns a routed workflow for engine with an optional
// model-provider, allowed-models list, extra engine fields, and top-level frontmatter.
func nativeRoutedWorkflowSource(engine, provider, allowedModels, engineExtra, frontmatter string) string {
	var source strings.Builder
	source.WriteString("---\non: workflow_dispatch\npermissions:\n  contents: read\nengine:\n  id: " + engine + "\n")
	if provider != "" {
		source.WriteString("  model-provider: " + provider + "\n")
	}
	source.WriteString(engineExtra)
	source.WriteString("  model-routing:\n    goal: cost\n    mode: economy\n")
	if allowedModels != "" {
		source.WriteString("    allowed-models: " + allowedModels + "\n")
	}
	source.WriteString(frontmatter)
	source.WriteString("---\nReport a finding.\n")
	return source.String()
}

// agentExecutionEnv returns the env of the agent job's AWF execution step.
func agentExecutionEnv(t *testing.T, lock string) map[string]string {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(lock), &workflow))
	for _, step := range workflow.Jobs["agent"].Steps {
		if strings.Contains(step.Run, `awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"`) {
			return step.Env
		}
	}
	t.Fatal("agent job has no AWF execution step")
	return nil
}

func TestModelRoutingClaudeNativeProviderCompile(t *testing.T) {
	for _, provider := range []string{"", "anthropic"} {
		t.Run("provider="+provider, func(t *testing.T) {
			compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource("claude", provider, "", "", ""))
			routing := compiled.routing(t)
			require.Equal(t, "anthropic", routing["provider"])
			require.Equal(t, []string{"claude-*"}, compiled.candidates(t))
			for _, candidate := range compiled.candidates(t) {
				require.NotContains(t, candidate, "github-copilot/")
			}
			require.NotContains(t, compiled.awfConfig["apiProxy"], "modelOverrides", "native Anthropic routing must not apply the Copilot model override map")

			env := agentExecutionEnv(t, compiled.lock)
			require.Equal(t, "${{ secrets.ANTHROPIC_API_KEY }}", env["ANTHROPIC_API_KEY"])
			require.Equal(t, "anthropic", env["GH_AW_LLM_PROVIDER"])
			require.Equal(t, "1", env["GH_AW_MODEL_ROUTING"])
			require.NotContains(t, env, "COPILOT_GITHUB_TOKEN", "native routing must not substitute Copilot credentials")
			require.NotContains(t, env, "ANTHROPIC_BASE_URL", "native routing must keep the AWF Anthropic listener")

			var modelConfig map[string]any
			require.NoError(t, json.Unmarshal([]byte(compiled.infoEnv["GH_AW_INFO_MODEL_CONFIG"]), &modelConfig))
			infoRouting := modelConfig["routing"].(map[string]any)
			require.Equal(t, "anthropic", infoRouting["provider"])
			require.Equal(t, []any{"claude-*"}, infoRouting["candidate_models"])
		})
	}
}

func TestModelRoutingCodexNativeProviderCompile(t *testing.T) {
	for _, provider := range []string{"", "openai"} {
		t.Run("provider="+provider, func(t *testing.T) {
			compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource("codex", provider, "", "", ""))
			routing := compiled.routing(t)
			require.Equal(t, "openai", routing["provider"])
			require.Equal(t, []string{"gpt-*"}, compiled.candidates(t))

			env := agentExecutionEnv(t, compiled.lock)
			require.Equal(t, "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}", env["CODEX_API_KEY"])
			require.Equal(t, "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}", env["OPENAI_API_KEY"])
			require.Equal(t, "openai", env["GH_AW_LLM_PROVIDER"])
			require.NotContains(t, env, "COPILOT_GITHUB_TOKEN", "native routing must not substitute Copilot credentials")

			var modelConfig map[string]any
			require.NoError(t, json.Unmarshal([]byte(compiled.infoEnv["GH_AW_INFO_MODEL_CONFIG"]), &modelConfig))
			require.Equal(t, "openai", modelConfig["routing"].(map[string]any)["provider"])
		})
	}
}

func TestModelRoutingNativeAllowedModelsAndAliases(t *testing.T) {
	for _, tc := range []struct {
		name      string
		engine    string
		models    string
		policy    string
		want      []string
		wantError string
	}{
		{name: "exact native Anthropic id", engine: "claude", models: "[claude-sonnet-5]", want: []string{"claude-sonnet-5"}},
		{name: "exact dash-form Anthropic id", engine: "claude", models: "[claude-sonnet-5-5]", want: []string{"claude-sonnet-5-5"}},
		{name: "sonnet alias is native and family-constrained", engine: "claude", models: "[sonnet]", want: []string{"claude-*sonnet*"}},
		{name: "matching provider qualification is normalized", engine: "claude", models: "[anthropic/claude-sonnet-5]", want: []string{"claude-sonnet-5"}},
		{name: "mismatched Copilot qualification is rejected", engine: "claude", models: "[github-copilot/claude-sonnet-5]", wantError: `provider "github-copilot" differs from the native "anthropic" provider`},
		{name: "mismatched OpenAI qualification is rejected", engine: "claude", models: "[openai/gpt-5.4]", wantError: `provider "openai" differs from the native "anthropic" provider`},
		{name: "exact native OpenAI id", engine: "codex", models: "[gpt-5.4]", want: []string{"gpt-5.4"}},
		{name: "matching OpenAI qualification is normalized", engine: "codex", models: "[openai/gpt-5.4]", want: []string{"gpt-5.4"}},
		{name: "Codex rejects Anthropic qualification", engine: "codex", models: "[anthropic/claude-sonnet-5]", wantError: `provider "anthropic" differs from the native "openai" provider`},
		{name: "Codex small alias stays in the GPT family", engine: "codex", models: "[small]", want: []string{"gpt-5*mini*", "gpt-5*nano*"}},
		{name: "OpenAI exact id wins over alias of the same name", engine: "codex", models: "[gpt-5-mini]", want: []string{"gpt-5-mini"}},
		{name: "Codex rejects Sonnet alias", engine: "codex", models: "[sonnet]", wantError: `alias "sonnet" has no models compatible with Codex's Responses API`},
		{name: "Claude family validation applies to exact ids", engine: "claude", models: "[gpt-5.4]", wantError: "native Messages API"},
		{name: "native allow policy narrows alias", engine: "claude", models: "[sonnet]", policy: "models:\n  allowed: [\"anthropic/claude-sonnet-5*\"]\n", want: []string{"claude-sonnet-5*"}},
		{name: "bare allow policy narrows alias", engine: "claude", models: "[sonnet]", policy: "models:\n  allowed: [\"claude-sonnet-5*\"]\n", want: []string{"claude-sonnet-5*"}},
		{name: "Copilot allow policy does not authorize Anthropic", engine: "claude", models: "[claude-sonnet-5]", policy: "models:\n  allowed: [\"github-copilot/claude-*\"]\n", wantError: "excluded by models.allowed or models.blocked"},
		{name: "native block policy excludes exact id", engine: "codex", models: "[gpt-5.4, gpt-5.5]", policy: "models:\n  blocked: [\"openai/gpt-5.4\"]\n", want: []string{"gpt-5.5"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource(tc.engine, "", tc.models, "", tc.policy))
			if tc.wantError != "" {
				require.ErrorContains(t, compiled.err, tc.wantError)
				return
			}
			require.Equal(t, tc.want, compiled.candidates(t))
			for _, candidate := range compiled.candidates(t) {
				require.NotContains(t, candidate, "/", "native candidates are bare ids or patterns")
			}
		})
	}

	t.Run("sonnet alias matches dash-form Anthropic ids", func(t *testing.T) {
		patterns, err := expandModelRoutingAliasInNamespace("sonnet", BuiltinModelAliases(), "claude", "anthropic")
		require.NoError(t, err)
		require.Equal(t, []string{"anthropic/claude-*sonnet*"}, patterns)
		require.True(t, bundledProviderModelMatches("anthropic", "claude-*sonnet*"))
		require.True(t, isBundledProviderModelID("anthropic", "claude-sonnet-5-5"))
		require.False(t, isBundledProviderModelID("anthropic", "claude-sonnet-5.5"), "native Anthropic ids are dash-form")
	})

	t.Run("exact native id wins over alias of the same name", func(t *testing.T) {
		groups, err := resolveModelRoutingCandidateGroups(&WorkflowData{
			EngineConfig: &EngineConfig{
				ID:           "codex",
				ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "economy", AllowedModels: []string{"gpt-5.4"}},
			},
		}, "codex")
		require.NoError(t, err)
		require.Equal(t, []string{"openai/gpt-5.4"}, groups[0].Patterns, "gpt-5.4 is both an alias and an OpenAI model id; the exact id wins")
	})

	t.Run("policy excluding the entire native family", func(t *testing.T) {
		compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource("claude", "", "", "", "models:\n  blocked: [\"anthropic/claude-*\"]\n"))
		require.ErrorContains(t, compiled.err, "the claude engine uses its model family, claude-*")
		compiled = compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource("claude", "", "", "", "models:\n  blocked: [\"github-copilot/claude-*\"]\n"))
		require.NoError(t, compiled.err, "a Copilot block rule does not exclude the native Anthropic family")
		require.Equal(t, []string{"claude-*"}, compiled.candidates(t))
	})
}

func TestModelRoutingRejectsUnsupportedProviderTargets(t *testing.T) {
	for _, tc := range []struct {
		name        string
		engine      string
		provider    string
		engineExtra string
		frontmatter string
		wantError   []string
	}{
		{name: "Claude on OpenAI", engine: "claude", provider: "openai", wantError: []string{`resolves to the "openai" provider`, "does not cross providers", "engine.model-provider: anthropic"}},
		{name: "Codex on Anthropic", engine: "codex", provider: "anthropic", wantError: []string{`resolves to the "anthropic" provider`, "does not cross providers", "engine.model-provider: openai"}},
		{name: "Copilot on OpenAI", engine: "copilot", provider: "openai", wantError: []string{"requires GitHub Copilot inference", "engine.model-provider: github"}},
		{name: "pi on Anthropic", engine: "pi", provider: "anthropic", wantError: []string{"requires GitHub Copilot inference", "engine.model-provider: github"}},
		{name: "Claude custom Anthropic base URL", engine: "claude", engineExtra: "  env:\n    ANTHROPIC_BASE_URL: https://llm-gateway.example.com/anthropic\n", wantError: []string{"engine.env.ANTHROPIC_BASE_URL", "https://api.anthropic.com", "custom targets, BYOK gateways"}},
		{name: "Codex custom OpenAI base URL", engine: "codex", provider: "openai", engineExtra: "  env:\n    OPENAI_BASE_URL: https://llm-gateway.example.com/v1\n", wantError: []string{"engine.env.OPENAI_BASE_URL", "https://api.openai.com"}},
		{name: "Codex custom api-target", engine: "codex", engineExtra: "  api-target: llm-gateway.example.com\n", wantError: []string{"engine.api-target", "https://api.openai.com"}},
		{name: "Claude custom api-target", engine: "claude", engineExtra: "  api-target: llm-gateway.example.com\n", wantError: []string{"engine.api-target", "https://api.anthropic.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource(tc.engine, tc.provider, "", tc.engineExtra, tc.frontmatter))
			require.Error(t, compiled.err)
			for _, want := range tc.wantError {
				require.ErrorContains(t, compiled.err, want)
			}
			require.NotContains(t, compiled.err.Error(), "must switch to")
		})
	}

	t.Run("Azure OpenAI", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:           "codex",
				Auth:         &EngineAuthConfig{Type: "github-oidc", Provider: "azure"},
				ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "economy", AllowedModels: []string{"gpt-5.4"}},
			},
			NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: "v0.29.0"}},
		}
		err := validateModelRouting(data, "codex")
		require.ErrorContains(t, err, "Azure OpenAI")
		require.ErrorContains(t, err, "engine.auth")
	})

	t.Run("native endpoint base URL is allowed", func(t *testing.T) {
		compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource("claude", "", "", "  env:\n    ANTHROPIC_BASE_URL: https://api.anthropic.com\n", ""))
		require.NoError(t, compiled.err)
		require.Equal(t, "anthropic", compiled.routing(t)["provider"])
	})

	t.Run("GitHub Copilot routing for Claude and Codex is unchanged", func(t *testing.T) {
		for _, engine := range []string{"claude", "codex"} {
			compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource(engine, "github", "", "", ""))
			require.NoError(t, compiled.err)
			require.NotContains(t, compiled.routing(t), "provider")
		}
	})
}

// routedLockRoutingPattern matches the apiProxy.routing object written to a compiled lock.
var routedLockRoutingPattern = regexp.MustCompile(`\\"routing\\":\{(\\"provider\\":\\"([a-z]+)\\",)?\\"objective\\"`)

func TestModelRoutingCopilotLocksUnchanged(t *testing.T) {
	for _, tc := range []struct {
		engine   string
		provider string
		want     []string
	}{
		{engine: "copilot", want: nil},
		{engine: "pi", want: nil},
		{engine: "claude", provider: "github", want: []string{"github-copilot/claude-*"}},
		{engine: "codex", provider: "github", want: []string{"github-copilot/gpt-*"}},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			compiled := compileRoutedWorkflowSource(t, nativeRoutedWorkflowSource(tc.engine, tc.provider, "", "", ""))
			require.NoError(t, compiled.err)
			routing := compiled.routing(t)
			require.NotContains(t, routing, "provider", "Copilot routing omits apiProxy.routing.provider so existing locks stay byte-identical")
			if tc.want != nil {
				require.Equal(t, tc.want, compiled.candidates(t))
			}
			var modelConfig map[string]any
			require.NoError(t, json.Unmarshal([]byte(compiled.infoEnv["GH_AW_INFO_MODEL_CONFIG"]), &modelConfig))
			require.NotContains(t, modelConfig["routing"], "provider")
		})
	}

	// Every compiled Copilot-routed lock in the repository omits the routing provider;
	// only the native smokes carry one.
	lockFiles, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.lock.yml"))
	require.NoError(t, err)
	native := map[string]string{
		"smoke-claude-routed-anthropic.lock.yml": "anthropic",
		"smoke-codex-routed-openai.lock.yml":     "openai",
	}
	routed := 0
	for _, lockFile := range lockFiles {
		content, err := os.ReadFile(lockFile)
		require.NoError(t, err)
		for _, match := range routedLockRoutingPattern.FindAllStringSubmatch(string(content), -1) {
			routed++
			require.Equal(t, native[filepath.Base(lockFile)], match[2], "%s routing provider", filepath.Base(lockFile))
		}
	}
	require.NotZero(t, routed, "no compiled lock has apiProxy.routing")
}

// TestNativeRoutingSmokeEvidence checks that the native routed smokes compile to native
// AWF routing with native-key credentials and assert native evidence, without the
// Copilot-only expectations of the Copilot routed smokes.
func TestNativeRoutingSmokeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		provider   string
		candidates []any
		endpoint   string
		credential string
	}{
		{name: "smoke-claude-routed-anthropic", provider: "anthropic", candidates: []any{"claude-sonnet-5"}, endpoint: `mainEndpoint: "/v1/messages"`, credential: "ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}"},
		{name: "smoke-codex-routed-openai", provider: "openai", candidates: []any{"gpt-5.4"}, endpoint: `mainEndpoint: "/responses"`, credential: "CODEX_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join("..", "..", ".github", "workflows")
			source, err := os.ReadFile(filepath.Join(dir, tc.name+".md"))
			require.NoError(t, err)
			lock, err := os.ReadFile(filepath.Join(dir, tc.name+".lock.yml"))
			require.NoError(t, err)

			routing := agentAWFConfigFromLock(t, lock)["apiProxy"].(map[string]any)["routing"].(map[string]any)
			require.Equal(t, tc.provider, routing["provider"])
			require.Equal(t, tc.candidates, routing["candidateModels"])

			env := agentExecutionEnv(t, string(lock))
			require.Equal(t, tc.provider, env["GH_AW_LLM_PROVIDER"])
			require.Contains(t, string(lock), tc.credential)
			require.NotContains(t, string(lock), "github-copilot/", "native smoke has no Copilot routing candidate")
			require.NotContains(t, string(lock), "copilot-requests", "native smoke does not request Copilot inference")

			text := string(source)
			for _, want := range []string{`nativeProvider: "` + tc.provider + `"`, tc.endpoint, `routingMode: "economy"`, "expectRequestedEffortMatchesSelection: true", "expectModelInventory: true"} {
				require.Contains(t, text, want)
			}
			for _, forbidden := range []string{"model-provider:", "copilotInventory", "expectSelectedEffort"} {
				require.NotContains(t, text, forbidden)
			}
		})
	}
}
