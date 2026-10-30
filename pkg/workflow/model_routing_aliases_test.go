//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// awfConfigWritePattern matches the printf that writes the compiled AWF config file.
var awfConfigWritePattern = regexp.MustCompile(`printf '%s\\n' "(\{(?:[^"\\]|\\.)*)" > "\$\{RUNNER_TEMP\}/gh-aw/awf-config\.json"`)

var shellEscapePattern = regexp.MustCompile(`\\(.)`)

type routedAliasCompilation struct {
	err       error
	awfConfig map[string]any
	infoEnv   map[string]string
	lock      string
}

// compileRoutedAliasWorkflow compiles a routed workflow and returns the agent job's
// compiled AWF config and the generate_aw_info step env.
func compileRoutedAliasWorkflow(t *testing.T, engine, provider, allowedModels, policy string) routedAliasCompilation {
	t.Helper()
	var source strings.Builder
	source.WriteString("---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\nengine:\n  id: " + engine + "\n")
	if provider != "" {
		source.WriteString("  model-provider: " + provider + "\n")
	}
	source.WriteString("  model-routing:\n    goal: cost\n    mode: auto\n    allowed-models: " + allowedModels + "\n")
	source.WriteString(policy)
	source.WriteString("---\nReport a finding.\n")
	return compileRoutedWorkflowSource(t, source.String())
}

// compileRoutedWorkflowSource compiles a routed workflow source and returns the agent
// job's compiled AWF config, the generate_aw_info step env, and the lock file.
func compileRoutedWorkflowSource(t *testing.T, source string) routedAliasCompilation {
	t.Helper()
	workflowPath := filepath.Join(t.TempDir(), "routed.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0o600))
	if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
		return routedAliasCompilation{err: err}
	}
	content, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	return routedAliasCompilation{infoEnv: awInfoStepEnv(t, string(content)), lock: string(content), awfConfig: agentAWFConfigFromLock(t, content)}
}

// agentAWFConfigFromLock decodes the agent job's AWF config from the shell-escaped
// printf that the compiled lock uses to write awf-config.json.
func agentAWFConfigFromLock(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var lock struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string            `yaml:"id"`
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &lock))
	var awfConfig map[string]any
	for _, step := range lock.Jobs["agent"].Steps {
		if match := awfConfigWritePattern.FindStringSubmatch(step.Run); match != nil {
			// The AI-credit budget is substituted by the shell at run time.
			config := strings.ReplaceAll(match[1], "${GH_AW_MAX_AI_CREDITS}", "0")
			require.NoError(t, json.Unmarshal([]byte(shellEscapePattern.ReplaceAllString(config, "$1")), &awfConfig))
			break
		}
	}
	require.NotNil(t, awfConfig, "agent job has no compiled AWF config")
	return awfConfig
}

func (c routedAliasCompilation) routing(t *testing.T) map[string]any {
	t.Helper()
	require.NoError(t, c.err)
	return c.awfConfig["apiProxy"].(map[string]any)["routing"].(map[string]any)
}

func (c routedAliasCompilation) candidates(t *testing.T) []string {
	t.Helper()
	raw, ok := c.routing(t)["candidateModels"].([]any)
	require.True(t, ok, "compiled AWF config has no routing.candidateModels")
	candidates := make([]string, len(raw))
	for i, candidate := range raw {
		candidates[i] = candidate.(string)
	}
	return candidates
}

// sharedTableCopilotPatterns expands aliases with the shared alias table and keeps the
// GitHub Copilot patterns, deduplicated in order.
func sharedTableCopilotPatterns(aliases ...string) []string {
	table := BuiltinModelAliases()
	var patterns []string
	for _, alias := range aliases {
		for _, pattern := range expandModelPatterns(alias, table, "github-copilot") {
			if strings.HasPrefix(pattern, "github-copilot/") {
				patterns = appendUnique(patterns, pattern)
			}
		}
	}
	return patterns
}

var copilotSmallPatterns = []string{
	"github-copilot/*haiku*",
	"github-copilot/gpt-5*mini*",
	"github-copilot/gpt-5*nano*",
	"github-copilot/gemini-*flash*lite*",
}

func TestRoutingAllowedModelsExpandsCopilotAliases(t *testing.T) {
	want := append(append([]string{}, copilotSmallPatterns...), "github-copilot/*sonnet*")
	require.Equal(t, want, sharedTableCopilotPatterns("small", "sonnet"), "expected expansion must come from the shared alias table")

	compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[small, sonnet]", "")
	candidates := compiled.candidates(t)
	require.Equal(t, want, candidates)
	require.NotContains(t, candidates, "github-copilot/small")
	require.NotContains(t, candidates, "github-copilot/sonnet")
}

func TestRoutingAllowedModelsPreservesMixedExactAndAlias(t *testing.T) {
	compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[gpt-5.6-luna, small]", "")
	require.Equal(t, append([]string{"github-copilot/gpt-5.6-luna"}, copilotSmallPatterns...), compiled.candidates(t))
}

func TestRoutingAllowedModelsScopesAliasesAcrossEngines(t *testing.T) {
	aliases := []string{"small", "sonnet", "haiku", "mini", "large", "gpt-5-mini", "opus", "any"}
	familyPrefix := map[string]string{"claude": "github-copilot/claude-", "codex": "github-copilot/gpt-"}
	for _, engine := range []string{"copilot", "claude", "codex", "pi"} {
		for _, alias := range aliases {
			t.Run(engine+"/"+alias, func(t *testing.T) {
				patterns, err := expandModelRoutingAlias(alias, BuiltinModelAliases(), engine)
				if engine == "codex" && (alias == "sonnet" || alias == "haiku" || alias == "opus") {
					require.ErrorContains(t, err, "Responses API", "Codex must not route to Claude models")
					return
				}
				if engine == "claude" && alias == "gpt-5-mini" {
					require.ErrorContains(t, err, "Messages API", "Claude must not route to GPT models")
					return
				}
				require.NoError(t, err)
				require.NotEmpty(t, patterns)
				for _, pattern := range patterns {
					require.True(t, strings.HasPrefix(pattern, "github-copilot/"), "pattern %q must use only the github-copilot provider", pattern)
					require.NotContains(t, strings.TrimPrefix(pattern, "github-copilot/"), "/", "pattern %q must not leak another provider", pattern)
					if prefix, limited := familyPrefix[engine]; limited {
						require.True(t, strings.HasPrefix(pattern, prefix), "%s pattern %q must stay in the engine's model family", engine, pattern)
					}
				}
				if _, limited := familyPrefix[engine]; !limited {
					require.Equal(t, sharedTableCopilotPatterns(alias), patterns)
				}
			})
		}
	}

	for _, tc := range []struct {
		name      string
		engine    string
		provider  string
		models    string
		want      []string
		wantError string
	}{
		{name: "copilot", engine: "copilot", models: "[small, sonnet]", want: append(append([]string{}, copilotSmallPatterns...), "github-copilot/*sonnet*")},
		{name: "pi", engine: "pi", models: "[small, sonnet]", want: append(append([]string{}, copilotSmallPatterns...), "github-copilot/*sonnet*")},
		{name: "claude keeps Claude models", engine: "claude", provider: "github", models: "[small, sonnet]", want: []string{"github-copilot/claude-*haiku*", "github-copilot/claude-*sonnet*"}},
		{name: "codex keeps GPT models", engine: "codex", provider: "github", models: "[small, gpt-5.6-luna]", want: []string{"github-copilot/gpt-5*mini*", "github-copilot/gpt-5*nano*", "github-copilot/gpt-5.6-luna"}},
		{name: "codex rejects Sonnet", engine: "codex", provider: "github", models: "[sonnet]", wantError: `alias "sonnet" has no models compatible with Codex's Responses API`},
		{name: "claude family validation still applies to exact ids", engine: "claude", provider: "github", models: "[small, gpt-5.6-luna]", wantError: "native Messages API"},
	} {
		t.Run("compile "+tc.name, func(t *testing.T) {
			compiled := compileRoutedAliasWorkflow(t, tc.engine, tc.provider, tc.models, "")
			if tc.wantError != "" {
				require.ErrorContains(t, compiled.err, tc.wantError)
				return
			}
			require.Equal(t, tc.want, compiled.candidates(t))
		})
	}
}

func TestRoutingAllowedModelsReportsFullyBlockedAlias(t *testing.T) {
	blockSmall := "models:\n  blocked: [\"*haiku*\", \"gpt-5*\", \"gemini-*\"]\n"
	compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[small]", blockSmall)
	require.ErrorContains(t, compiled.err, "all engine.model-routing.allowed-models are excluded by models.allowed or models.blocked policy")
	require.ErrorContains(t, compiled.err, "excluded: small")

	t.Run("other candidates remain", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "", nil, []string{"*haiku*", "gpt-5*", "gemini-*"}, []string{"small", "claude-sonnet-5"})
		candidates, excluded, err := intersectModelRoutingPolicy(mustRoutingGroups(t, data, "copilot"), nil, data.ModelPolicyBlocked, false)
		require.NoError(t, err)
		require.Equal(t, []string{"github-copilot/claude-sonnet-5"}, candidates)
		require.Equal(t, []string{"small"}, excluded, "the warning names the excluded alias")
	})
}

func mustRoutingGroups(t *testing.T, data *WorkflowData, engine string) []modelRoutingCandidateGroup {
	t.Helper()
	groups, err := resolveModelRoutingCandidateGroups(data, engine)
	require.NoError(t, err)
	return groups
}

func TestRoutingAllowedModelsIntersectsExpandedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name           string
		models         string
		policy         string
		want           []string
		wantAllowed    []any
		wantDisallowed []any
	}{
		{
			name:        "exact allow rules inside an alias stay usable",
			models:      "[small]",
			policy:      "models:\n  allowed: [claude-haiku-4.5, gpt-5.4-mini]\n",
			want:        []string{"github-copilot/claude-haiku-4.5", "github-copilot/gpt-5.4-mini"},
			wantAllowed: []any{"claude-haiku-4.5", "gpt-5.4-mini"},
		},
		{
			name:        "wildcard allow rule narrows an alias",
			models:      "[small]",
			policy:      "models:\n  allowed: [\"claude-haiku-*\"]\n",
			want:        []string{"github-copilot/claude-haiku-*"},
			wantAllowed: []any{"claude-haiku-*"},
		},
		{
			name:        "wildcard allow rule covering alias patterns keeps them",
			models:      "[small]",
			policy:      "models:\n  allowed: [\"gpt-5*\"]\n",
			want:        []string{"github-copilot/*haiku*", "github-copilot/gpt-5*mini*", "github-copilot/gpt-5*nano*"},
			wantAllowed: []any{"gpt-5*"},
		},
		{
			name:           "partial block keeps the alias pattern for AWF to filter",
			models:         "[small, sonnet]",
			policy:         "models:\n  blocked: [claude-haiku-4.5, \"gpt-5*nano*\"]\n",
			want:           []string{"github-copilot/*haiku*", "github-copilot/gpt-5*mini*", "github-copilot/gemini-*flash*lite*", "github-copilot/*sonnet*"},
			wantDisallowed: []any{"claude-haiku-4.5", "gpt-5*nano*"},
		},
		{
			name:           "block wins over an allowed exact id",
			models:         "[small]",
			policy:         "models:\n  allowed: [claude-haiku-4.5, gpt-5.4-mini]\n  blocked: [gpt-5.4-mini]\n",
			want:           []string{"github-copilot/claude-haiku-4.5"},
			wantAllowed:    []any{"claude-haiku-4.5"},
			wantDisallowed: []any{"gpt-5.4-mini"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileRoutedAliasWorkflow(t, "copilot", "", tc.models, tc.policy)
			require.Equal(t, tc.want, compiled.candidates(t))
			proxy := compiled.awfConfig["apiProxy"].(map[string]any)
			require.Equal(t, tc.wantAllowed, toAnySliceOrNil(proxy["allowedModels"]), "request policy is unchanged by the shortlist")
			require.Equal(t, tc.wantDisallowed, toAnySliceOrNil(proxy["disallowedModels"]))
		})
	}

	t.Run("AWF without candidateModels never widens models.allowed", func(t *testing.T) {
		data := routedPolicyWorkflowData("copilot", "v0.28.31", []string{"claude-sonnet-5", "gpt-*"}, nil, []string{"sonnet"})
		require.False(t, apiProxySupportsRoutingCandidateModels(data))
		proxy := buildRoutedAPIProxyConfig(t, "copilot", data)
		require.Equal(t, []any{"github-copilot/claude-sonnet-5"}, proxy["allowedModels"])
	})
}

func toAnySliceOrNil(value any) []any {
	if value == nil {
		return nil
	}
	return value.([]any)
}

func TestRoutingAllowedModelsRejectsUnknownName(t *testing.T) {
	compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[smal]", "")
	require.Error(t, compiled.err)
	require.ErrorContains(t, compiled.err, `"smal", which is neither a gh-aw model alias nor an exact model id`)
	require.ErrorContains(t, compiled.err, "small, small-agent, sonnet")
	require.NotContains(t, compiled.err.Error(), "github-copilot/smal")

	data := routedPolicyWorkflowData("copilot", "", nil, nil, []string{"smal"})
	_, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", AllowedDomains: "github.com", WorkflowData: data})
	require.ErrorContains(t, err, `"smal"`)

	// Exact ids are not checked against a live catalogue.
	require.NoError(t, validateModelRouting(routedPolicyWorkflowData("copilot", "", nil, nil, []string{"gpt-9.9-future"}), "copilot"))
}

func TestRoutingModelConfigRecordsAuthoredAndExpandedCandidates(t *testing.T) {
	compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[gpt-5.6-luna, small, sonnet]", "models:\n  blocked: [\"gpt-5*nano*\", \"gemini-*\"]\n")
	emitted := compiled.candidates(t)
	require.Equal(t, []string{"github-copilot/gpt-5.6-luna", "github-copilot/*haiku*", "github-copilot/gpt-5*mini*", "github-copilot/*sonnet*"}, emitted)

	var compiledConfig map[string]any
	require.NoError(t, json.Unmarshal([]byte(compiled.infoEnv["GH_AW_INFO_MODEL_CONFIG"]), &compiledConfig))
	routing := compiledConfig["routing"].(map[string]any)
	require.Equal(t, []any{"gpt-5.6-luna", "small", "sonnet"}, routing["allowed_models"])
	require.Equal(t, toAnySlice(emitted), routing["candidate_models"], "recorded candidates must agree with the emitted AWF JSON")

	// Run the runner's aw_info.model_config parser and the collector's model.config projection.
	cmd := exec.Command("node", "-e", `
const { parseCompiledModelConfig, projectModelConfig } = require("./actions/setup/js/routing_analytics.cjs");
const modelConfig = parseCompiledModelConfig(process.env.GH_AW_INFO_MODEL_CONFIG, "1000");
process.stdout.write(JSON.stringify({ modelConfig, projected: projectModelConfig(modelConfig) }));`)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GH_AW_INFO_MODEL_CONFIG="+compiled.infoEnv["GH_AW_INFO_MODEL_CONFIG"])
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var result struct {
		ModelConfig map[string]any `json:"modelConfig"`
		Projected   map[string]any `json:"projected"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	awInfoRouting := result.ModelConfig["routing"].(map[string]any)
	require.Equal(t, []any{"gpt-5.6-luna", "small", "sonnet"}, awInfoRouting["allowed_models"])
	require.Equal(t, toAnySlice(emitted), awInfoRouting["candidate_models"])
	projectedRouting := result.Projected["routing"].(map[string]any)
	require.Equal(t, []any{"gpt-5.6-luna", "small", "sonnet"}, projectedRouting["allowedModels"])
	require.Equal(t, toAnySlice(emitted), projectedRouting["candidateModels"])
	require.NotContains(t, projectedRouting, "candidate_models")
}

func TestRoutingAllowedModelsPrefixedIdStaysExact(t *testing.T) {
	for _, models := range []string{"[github-copilot/gemini-3.8-flash]", "[copilot/gemini-3.8-flash]"} {
		compiled := compileRoutedAliasWorkflow(t, "copilot", "", models, "")
		require.Equal(t, []string{"github-copilot/gemini-3.8-flash"}, compiled.candidates(t), models)
	}
	// The bare names are aliases but not bundled model ids, so only the prefix keeps them exact.
	for _, tc := range []struct{ prefix, name string }{{"github-copilot/", "gpt-5"}, {"copilot/", "gpt-5.6"}} {
		require.Contains(t, BuiltinModelAliases(), tc.name)
		require.False(t, isBundledCopilotModelID(tc.name))
		compiled := compileRoutedAliasWorkflow(t, "copilot", "", "["+tc.prefix+tc.name+"]", "")
		require.Equal(t, []string{"github-copilot/" + tc.name}, compiled.candidates(t), tc.prefix+tc.name)
	}
}

func TestRoutingAllowedModelsKnownCopilotIdStaysExact(t *testing.T) {
	// Each name is both a bundled GitHub Copilot model id and a gh-aw alias; like
	// resolveConfiguredModel, the model id wins.
	for _, model := range []string{"gpt-5.4", "gpt-5-mini", "gemini-3.8-flash"} {
		require.Contains(t, BuiltinModelAliases(), model)
		require.True(t, isBundledCopilotModelID(model))
		compiled := compileRoutedAliasWorkflow(t, "copilot", "", "["+model+"]", "")
		require.Equal(t, []string{"github-copilot/" + model}, compiled.candidates(t), model)
	}
	// An alias that is not a bundled model id still expands.
	compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[gpt-5.6]", "")
	require.Equal(t, []string{"github-copilot/gpt-5.6*"}, compiled.candidates(t))
}

func TestRoutingAllowedModelsAliasNamedLikeIdFollowsEngineModelRule(t *testing.T) {
	aliases := BuiltinModelAliases()
	var catalog []string
	for id := range bundledCopilotModelIDs() {
		catalog = append(catalog, "github-copilot/"+id)
	}
	names := []string{"gemini-3.8-flash", "gpt-5-mini", "gpt-5.5", "gpt-5", "gpt-5-codex", "gpt-6"}
	aliasesJSON, err := json.Marshal(aliases)
	require.NoError(t, err)
	catalogJSON, err := json.Marshal(catalog)
	require.NoError(t, err)
	namesJSON, err := json.Marshal(names)
	require.NoError(t, err)

	// Resolve each name with the engine.model resolver against the bundled Copilot catalogue.
	cmd := exec.Command("node", "-e", `
const { resolveConfiguredModel } = require("./actions/setup/js/resolve_model_alias.cjs");
const aliasMap = JSON.parse(process.env.ALIASES), catalog = JSON.parse(process.env.CATALOG);
const status = {};
for (const name of JSON.parse(process.env.NAMES)) {
  try {
    status[name] = resolveConfiguredModel({ configuredModel: name, aliasMap, catalog }).status;
  } catch (err) {
    // An alias that the catalogue cannot resolve is still resolved as an alias.
    if (err.name !== "ModelAliasResolutionError") throw err;
    status[name] = "resolved";
  }
}
process.stdout.write(JSON.stringify(status));`)
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "ALIASES="+string(aliasesJSON), "CATALOG="+string(catalogJSON), "NAMES="+string(namesJSON))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	var engineModelStatus map[string]string
	require.NoError(t, json.Unmarshal(output, &engineModelStatus))

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, aliases, name, "%s must be an alias for this test", name)
			groups := mustRoutingGroups(t, routedPolicyWorkflowData("copilot", "", nil, nil, []string{name}), "copilot")
			require.Len(t, groups, 1)
			if engineModelStatus[name] == "exact" {
				require.False(t, groups[0].Alias)
				require.Equal(t, []string{"github-copilot/" + name}, groups[0].Patterns)
			} else {
				require.Equal(t, "resolved", engineModelStatus[name])
				require.True(t, groups[0].Alias)
				require.Equal(t, sharedTableCopilotPatterns(name), groups[0].Patterns)
			}
		})
	}
	require.Equal(t, "exact", engineModelStatus["gemini-3.8-flash"])
	require.Equal(t, "resolved", engineModelStatus["gpt-5"])
}

func TestRoutingAllowedModelsNoInventedFamilyPatterns(t *testing.T) {
	for _, alias := range []string{"grok", "computer-use"} {
		_, err := expandModelRoutingAlias(alias, BuiltinModelAliases(), "codex")
		require.ErrorContains(t, err, "no models compatible with Codex's Responses API", alias)
	}
	patterns, err := expandModelRoutingAlias("sonnet", BuiltinModelAliases(), "claude")
	require.NoError(t, err)
	require.Equal(t, []string{"github-copilot/claude-*sonnet*"}, patterns)
}

func TestRoutingAllowedModelsIntersectsLeadingWildcardWithFamily(t *testing.T) {
	aliases := map[string][]string{"my-claude": {"copilot/*claude-sonnet*", "anthropic/*sonnet*"}}
	patterns, err := expandModelRoutingAlias("my-claude", aliases, "claude")
	require.NoError(t, err)
	require.Equal(t, []string{"github-copilot/claude-sonnet*"}, patterns)
	matched, _ := path.Match(strings.TrimPrefix(patterns[0], "github-copilot/"), "claude-sonnet-5")
	require.True(t, matched, "claude-sonnet-5 matches the authored pattern and the Claude family")

	require.Equal(t, []string{"claude-sonnet*", "claude-*claude-sonnet*"}, intersectModelPatternWithFamily("*claude-sonnet*", "claude-"))
	require.Equal(t, []string{"claude-*sonnet*"}, intersectModelPatternWithFamily("*sonnet*", "claude-"))
	require.Equal(t, []string{"gpt-5*mini*"}, intersectModelPatternWithFamily("gpt-5*mini*", "gpt-"))
	require.Empty(t, intersectModelPatternWithFamily("gemini-*", "gpt-"))
}

func TestExpandModelPatternsKeepsEmptyKeyDefaultAlias(t *testing.T) {
	aliases := map[string][]string{"": {"copilot/*sonnet*", "anthropic/*sonnet*"}}
	require.Equal(t, []string{"github-copilot/*sonnet*", "anthropic/*sonnet*"}, expandModelPatterns("", aliases, "github-copilot"))
	require.Empty(t, expandModelPatterns("*sonnet*", aliases, "github-copilot"), "a literal glob request is still rejected")
}

func TestRoutingAllowedModelsPolicyProviderAliasesAndCase(t *testing.T) {
	for _, tc := range []struct {
		name   string
		models string
		policy string
		want   []string
	}{
		{name: "github/ allow rule", models: "[small]", policy: "models:\n  allowed: [github/gpt-5.4-mini]\n", want: []string{"github-copilot/gpt-5.4-mini"}},
		{name: "mixed-case provider allow rule", models: "[small]", policy: "models:\n  allowed: [GitHub-Copilot/claude-haiku-4.5]\n", want: []string{"github-copilot/claude-haiku-4.5"}},
		{name: "github/ block rule", models: "[small, sonnet]", policy: "models:\n  blocked: [\"github/*haiku*\"]\n", want: []string{"github-copilot/gpt-5*mini*", "github-copilot/gpt-5*nano*", "github-copilot/gemini-*flash*lite*", "github-copilot/*sonnet*"}},
		{name: "mixed-case allow rule", models: "[small]", policy: "models:\n  allowed: [\"GPT-5*\"]\n", want: []string{"github-copilot/*haiku*", "github-copilot/gpt-5*mini*", "github-copilot/gpt-5*nano*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileRoutedAliasWorkflow(t, "copilot", "", tc.models, tc.policy)
			require.Equal(t, tc.want, compiled.candidates(t))
		})
	}

	t.Run("mixed-case fully blocked alias", func(t *testing.T) {
		compiled := compileRoutedAliasWorkflow(t, "copilot", "", "[mai-code]", "models:\n  blocked: [\"mai-code*\"]\n")
		require.ErrorContains(t, compiled.err, "excluded: mai-code")
	})
}
