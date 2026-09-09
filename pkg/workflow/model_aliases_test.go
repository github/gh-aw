//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuiltinModelAliases verifies that the builtin model alias map covers the main
// model families and returns a fresh map on each call.
func TestBuiltinModelAliases(t *testing.T) {
	aliases := BuiltinModelAliases()

	expectedFamilies := []string{
		"sonnet", "sonnet-6x", "haiku", "opus", "opusplan",
		"gpt-5", "gpt-5.5", "gpt-5.4", "gpt-5.3", "gpt-5.2", "gpt-5.1", "gpt-5-mini", "gpt-5-nano", "gpt-5-codex", "gpt-5-pro", "gpt-6", "mai-code", "mai-code-1-flash-picker", "reasoning",
		"gemini-flash", "gemini-flash-lite", "gemini-pro", "gemini-3-pro", "gemini-3-flash", "gemini-3.1-pro", "gemini-3.1-flash", "gemini-3.5-flash", "gemini-3.6-flash", "gemini-3.7-flash", "antigravity", "lyria", "computer-use", "robotics", "deep-research",
		"nano-banana",
		"vision", "image-generation",
		"grok",
		"mini", "large", "auto", "any", "agent", "small-agent", "copilot", "claude", "codex", "gemini", "summarization", "detection", "evals",
	}
	for _, family := range expectedFamilies {
		patterns, ok := aliases[family]
		assert.True(t, ok, "expected builtin alias for family %q", family)
		assert.NotEmpty(t, patterns, "builtin alias %q should have at least one pattern", family)
	}
	assert.NotContains(t, aliases, "gpt-4.1", "gpt-4.1 alias should remain removed")

	// Vendor aliases should include at least one copilot/* pattern.
	// Meta-aliases (mini, large, auto) reference other alias names and are excluded here.
	vendorFamilies := []string{"sonnet", "sonnet-6x", "haiku", "opus", "gpt-5", "gpt-5.5", "gpt-5.4", "gpt-5.3", "gpt-5.2", "gpt-5-mini", "gpt-5-nano", "gpt-5-codex", "gpt-5-pro", "gpt-6", "mai-code", "mai-code-1-flash-picker", "reasoning", "gemini-flash", "gemini-flash-lite", "gemini-pro", "gemini-3-pro", "gemini-3-flash", "gemini-3.1-pro", "gemini-3.1-flash", "gemini-3.5-flash", "gemini-3.6-flash", "gemini-3.7-flash", "antigravity", "lyria", "nano-banana", "computer-use", "robotics", "deep-research", "raptor-mini", "grok"}
	for _, family := range vendorFamilies {
		patterns := aliases[family]
		hasCopilot := false
		for _, p := range patterns {
			if len(p) > 7 && p[:7] == "copilot" {
				hasCopilot = true
				break
			}
		}
		assert.True(t, hasCopilot, "builtin alias %q should include a copilot/* pattern", family)
	}

	assert.Contains(t, aliases["gemini-flash"], "gemini/gemini-*flash*", "gemini-flash should support direct gemini/ provider models")
	assert.Contains(t, aliases["gemini-flash-lite"], "gemini/gemini-*flash*lite*", "gemini-flash-lite should support direct gemini/ provider models")
	assert.Contains(t, aliases["gemini-pro"], "gemini/gemini-*pro*", "gemini-pro should support direct gemini/ provider models")
	assert.Equal(t, []string{"copilot/gpt-5.4*", "openai/gpt-5.4*"}, aliases["gpt-5.4"], "gpt-5.4 should map to copilot/openai gpt-5.4 family")
	assert.Contains(t, aliases["gemini-3-pro"], "gemini/gemini-3*pro*", "gemini-3-pro should support direct gemini/ provider models")
	assert.Contains(t, aliases["gemini-3-pro"], "google/nano-banana*", "gemini-3-pro should include Gemini 3 Pro image variant pattern")
	assert.Contains(t, aliases["gemini-3-flash"], "gemini/gemini-3*flash*", "gemini-3-flash should support direct gemini/ provider models")
	assert.Contains(t, aliases["gemini-3.1-pro"], "gemini/gemini-3.1*pro*", "gemini-3.1-pro should support direct gemini/ provider models")
	assert.Contains(t, aliases["gemini-3.1-flash"], "gemini/gemini-3.1*flash*", "gemini-3.1-flash should support direct gemini/ provider models")
	assert.Equal(t, []string{"copilot/gpt-5.5*", "openai/gpt-5.5*"}, aliases["gpt-5.5"], "gpt-5.5 should map to copilot/openai gpt-5.5 family")
	assert.Equal(t, []string{"copilot/gpt-5.2*", "openai/gpt-5.2*"}, aliases["gpt-5.2"], "gpt-5.2 should map to copilot/openai gpt-5.2 family")
	assert.Equal(t, []string{"copilot/gpt-5.1*", "openai/gpt-5.1*"}, aliases["gpt-5.1"], "gpt-5.1 should map to copilot/openai gpt-5.1 family")
	assert.Equal(t, []string{"copilot/gemini-3.5*flash*", "google/gemini-3.5*flash*", "gemini/gemini-3.5*flash*"}, aliases["gemini-3.5-flash"], "gemini-3.5-flash should map to provider-specific Gemini 3.5 Flash patterns")
	assert.Equal(t, []string{"copilot/gemini-3.6*flash*", "google/gemini-3.6*flash*", "gemini/gemini-3.6*flash*"}, aliases["gemini-3.6-flash"], "gemini-3.6-flash should map to provider-specific Gemini 3.6 Flash patterns")
	assert.Equal(t, []string{"copilot/gemini-3.7*flash*", "google/gemini-3.7*flash*", "gemini/gemini-3.7*flash*"}, aliases["gemini-3.7-flash"], "gemini-3.7-flash should map to provider-specific Gemini 3.7 Flash patterns")
	assert.Contains(t, aliases["antigravity"], "copilot/antigravity*", "antigravity should include copilot/ provider pattern")
	assert.Equal(t, []string{"google/lyria*", "gemini/lyria*", "copilot/lyria*"}, aliases["lyria"], "lyria should map to provider-specific Lyria patterns")
	assert.Equal(t, []string{"copilot/nano-banana*", "google/nano-banana*", "gemini/nano-banana*"}, aliases["nano-banana"], "nano-banana should map to provider-specific patterns")
	assert.Equal(t, []string{"copilot/MAI-Code*", "copilot/mai-code*", "openai/MAI-Code*"}, aliases["mai-code"], "mai-code should map to provider-specific MAI-Code patterns")
	assert.Equal(t, []string{"copilot/MAI-Code-1-Flash-picker*", "copilot/mai-code-1-flash-picker*", "openai/MAI-Code-1-Flash-picker*"}, aliases["mai-code-1-flash-picker"], "mai-code-1-flash-picker should map to provider-specific MAI-Code-1-Flash-picker patterns")
	assert.Equal(t, []string{"copilot/*sonnet-4.5*", "copilot/*sonnet-4.6*", "copilot/*sonnet-5*", "copilot/*sonnet-4-5-*", "anthropic/*sonnet-4-5-*", "copilot/*sonnet-4-6*", "anthropic/*sonnet-4-6*", "anthropic/*sonnet-5*"}, aliases["sonnet-6x"], "sonnet-6x should target Sonnet 4.5/4.6/5 model families across dot and dated variants")
	assert.Equal(t, []string{"opus?effort=high"}, aliases["opusplan"], "opusplan should map to opus with high reasoning effort")
	assert.Contains(t, aliases["deep-research"], "gemini/deep-research*", "deep-research should support direct gemini/ provider models")
	assert.Contains(t, aliases["vision"], "google/gemini-*image*", "vision should include google/ provider image patterns")
	assert.Contains(t, aliases["vision"], "google/gemini-*flash*", "vision should include google/ provider flash patterns")
	assert.Contains(t, aliases["image-generation"], "copilot/gpt-image*", "image-generation should include copilot gpt-image patterns")
	assert.Contains(t, aliases["image-generation"], "openai/gpt-image*", "image-generation should include openai gpt-image patterns")
	assert.Contains(t, aliases["image-generation"], "google/imagen*", "image-generation should include google imagen patterns")
	assert.Equal(t, []string{"copilot/*grok*", "openai/*grok*"}, aliases["grok"], "grok should map to copilot and openai grok patterns")

	// Meta-aliases reference other alias names (resolved recursively by AWF).
	assert.Equal(t, []string{"haiku", "gpt-5-mini", "gpt-5-nano", "gemini-flash-lite"}, aliases["mini"], "mini should reference haiku, gpt-5-mini, gpt-5-nano, and gemini-flash-lite")
	assert.Equal(t, []string{"haiku", "gpt-5-mini", "gemini-flash-lite", "mini"}, aliases["summarization"], "summarization should reference fast/lightweight models")
	assert.Equal(t, []string{"copilot/gpt-6*", "openai/gpt-6*"}, aliases["gpt-6"], "gpt-6 should map to copilot/openai gpt-6 family")
	assert.Equal(t, []string{"sonnet", "gpt-6", "gpt-5-pro", "gpt-5", "gemini-pro"}, aliases["large"], "large should list sonnet-range pricing models including gpt-6")
	assert.Equal(t, []string{"copilot/auto", "large"}, aliases["auto"], "auto should pass copilot/auto as-is to the Copilot API and fall back to large for other providers")
	assert.Equal(t, []string{"copilot/*fable*", "anthropic/*fable*"}, aliases["fable"], "fable should map to provider-specific fable patterns")
	assert.Equal(t, []string{"copilot/gpt-5.6*", "openai/gpt-5.6*"}, aliases["gpt-5.6"], "gpt-5.6 should map to provider-specific gpt-5.6 patterns")
	assert.Equal(t, []string{"copilot/gemini-omni*", "google/gemini-omni*", "gemini/gemini-omni*"}, aliases["gemini-omni"], "gemini-omni should map to provider-specific gemini-omni patterns")
	assert.Equal(t, []string{"copilot/*", "anthropic/*", "openai/*", "google/*", "gemini/*"}, aliases["any"], "any should provide a provider-wide catch-all fallback chain")
	assert.Equal(t, []string{"sonnet-6x", "gpt-6", "gpt-5.4", "gpt-5.5", "gpt-5.6", "gpt-5.3", "gemini-pro", "any"}, aliases["agent"], "agent should default to the configured high-capability fallback chain before any-model fallback")
	assert.Equal(t, []string{"haiku", "gpt-5-mini", "gemini-flash"}, aliases["small-agent"], "small-agent should default to the small/fast model fallback chain")
	assert.Equal(t, []string{"small"}, aliases["detection"], "detection should resolve through small-sized models")
	assert.Equal(t, []string{"small"}, aliases["evals"], "evals should resolve through small-sized models")
	assert.Equal(t, []string{"agent"}, aliases["copilot"], "copilot should define per-engine default fallback chain")
	assert.Equal(t, []string{"agent"}, aliases["claude"], "claude should define per-engine default fallback chain")
	assert.Equal(t, []string{"agent"}, aliases["codex"], "codex should define per-engine default fallback chain")
	assert.Equal(t, []string{"agent"}, aliases["gemini"], "gemini should define per-engine default fallback chain")
	assert.NotContains(t, aliases["agent"], "opus", "agent default chain must not include opus")

	// Returns a fresh copy — mutating one call's map must not affect another call.
	aliases["sonnet"] = []string{"custom/model"}
	aliases2 := BuiltinModelAliases()
	assert.NotEqual(t, aliases["sonnet"], aliases2["sonnet"], "BuiltinModelAliases should return a fresh copy each time")

	// Mutating an alias slice from one call must not affect the next call either.
	aliases2["haiku"][0] = "custom/haiku"
	aliases3 := BuiltinModelAliases()
	assert.NotEqual(t, aliases2["haiku"][0], aliases3["haiku"][0], "BuiltinModelAliases should return fresh slices each time")
}

// awfConfigModelsResult is a helper type for parsing the apiProxy.models section
// from generated AWF config JSON in tests.
type awfConfigModelsResult struct {
	APIProxy struct {
		Models map[string][]string `json:"models"`
	} `json:"apiProxy"`
}

// TestBuildAWFConfigJSON_ModelsSection verifies model alias behaviour in BuildAWFConfigJSON.
//
// Models are serialised under apiProxy.models per the AWF config schema (apiProxy.models
// is supported in AWF v0.25.38+). The builtin aliases are included when ModelMappings is set.
func TestBuildAWFConfigJSON_ModelsSection(t *testing.T) {
	t.Run("builtin model aliases are included when WorkflowData has ModelMappings", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				ModelMappings: MergeImportedModelAliases(nil, nil),
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		var parsed awfConfigModelsResult
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed), "result must be valid JSON")

		// models must appear nested under apiProxy
		assert.NotEmpty(t, parsed.APIProxy.Models, "models section must be present and non-empty under apiProxy in AWF config JSON")
		assert.Contains(t, jsonStr, `"models"`, "models key must appear in AWF config JSON")

		// the alias map is populated in WorkflowData
		assert.NotEmpty(t, config.WorkflowData.ModelMappings, "ModelMappings should be populated on WorkflowData")
		assert.Contains(t, config.WorkflowData.ModelMappings, "sonnet", "ModelMappings should include sonnet alias")
		assert.Contains(t, config.WorkflowData.ModelMappings, "haiku", "ModelMappings should include haiku alias")
	})

	t.Run("frontmatter override is reflected in WorkflowData and in AWF config JSON", func(t *testing.T) {
		custom := map[string][]string{
			"sonnet": {"myvendor/sonnet-v3"},
			"":       {"sonnet"},
		}
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				ModelMappings: MergeImportedModelAliases(nil, custom),
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		var parsed awfConfigModelsResult
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed), "result must be valid JSON")

		// models must appear nested under apiProxy
		assert.Contains(t, jsonStr, `"models"`, "models section must be present in AWF config JSON")

		// frontmatter overrides are visible in both WorkflowData and the JSON
		assert.Equal(t, []string{"myvendor/sonnet-v3"}, config.WorkflowData.ModelMappings["sonnet"],
			"frontmatter override for sonnet should be stored in ModelMappings")
		assert.Equal(t, []string{"myvendor/sonnet-v3"}, parsed.APIProxy.Models["sonnet"],
			"frontmatter override for sonnet should be emitted under apiProxy.models in AWF config JSON")
		assert.Equal(t, []string{"sonnet"}, config.WorkflowData.ModelMappings[""],
			"default policy should be stored in ModelMappings")
		assert.Equal(t, []string{"sonnet"}, parsed.APIProxy.Models[""],
			"default policy should be emitted under apiProxy.models in AWF config JSON")
	})

	t.Run("no models section when ModelMappings is nil", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				ModelMappings: nil,
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should not return an error")

		assert.NotContains(t, jsonStr, `"models"`, "models section should be absent when ModelMappings is nil")
	})
}

// TestMergeImportedModelAliases verifies the three-layer merge: builtins → imports → main.
func TestMergeImportedModelAliases(t *testing.T) {
	t.Run("no imports and no frontmatter returns builtins", func(t *testing.T) {
		merged := MergeImportedModelAliases(nil, nil)
		builtins := BuiltinModelAliases()
		assert.Len(t, merged, len(builtins), "should return exactly the builtins")
		for k, v := range builtins {
			assert.Equal(t, v, merged[k], "builtin alias %q should be present unchanged", k)
		}
	})

	t.Run("imported alias is added when not in builtins", func(t *testing.T) {
		imported := []map[string][]string{
			{"my-imported": {"vendor/imported-model"}},
		}
		merged := MergeImportedModelAliases(imported, nil)
		assert.Equal(t, []string{"vendor/imported-model"}, merged["my-imported"],
			"imported alias should be present in merged map")
		assert.NotEmpty(t, merged["sonnet"], "builtin sonnet should still be present")
	})

	t.Run("import cannot override a builtin alias", func(t *testing.T) {
		imported := []map[string][]string{
			{"sonnet": {"imported/sonnet-override"}},
		}
		merged := MergeImportedModelAliases(imported, nil)
		builtins := BuiltinModelAliases()
		assert.Equal(t, builtins["sonnet"], merged["sonnet"],
			"import should NOT override a builtin alias; builtin takes precedence over import")
	})

	t.Run("first import wins among multiple imports for the same key", func(t *testing.T) {
		imported := []map[string][]string{
			{"shared-alias": {"first-import/model"}},
			{"shared-alias": {"second-import/model"}},
		}
		merged := MergeImportedModelAliases(imported, nil)
		assert.Equal(t, []string{"first-import/model"}, merged["shared-alias"],
			"first import should win among competing imports for the same alias key")
	})

	t.Run("main workflow frontmatter overrides imported alias", func(t *testing.T) {
		imported := []map[string][]string{
			{"my-alias": {"import/model"}},
		}
		frontmatter := map[string][]string{
			"my-alias": {"main/model"},
		}
		merged := MergeImportedModelAliases(imported, frontmatter)
		assert.Equal(t, []string{"main/model"}, merged["my-alias"],
			"main workflow frontmatter should win over imported alias")
	})

	t.Run("main workflow frontmatter overrides builtin alias", func(t *testing.T) {
		frontmatter := map[string][]string{
			"sonnet": {"mygateway/sonnet-v3"},
		}
		merged := MergeImportedModelAliases(nil, frontmatter)
		assert.Equal(t, []string{"mygateway/sonnet-v3"}, merged["sonnet"],
			"main workflow frontmatter should override builtin sonnet alias")
		assert.NotEmpty(t, merged["haiku"], "other builtins should still be present")
	})

	t.Run("all three layers are combined correctly", func(t *testing.T) {
		imported := []map[string][]string{
			{
				"import-only": {"import/model"},
				"both":        {"import/both"},
				"sonnet":      {"import/sonnet"}, // shadowed by builtin
			},
		}
		frontmatter := map[string][]string{
			"main-only": {"main/model"},
			"both":      {"main/both"},
		}
		merged := MergeImportedModelAliases(imported, frontmatter)

		// import-only key comes from import (no conflict)
		assert.Equal(t, []string{"import/model"}, merged["import-only"],
			"import-only alias should come from the import layer")

		// main-only key comes from main workflow
		assert.Equal(t, []string{"main/model"}, merged["main-only"],
			"main-only alias should come from the main workflow layer")

		// 'both' key: main workflow wins over import
		assert.Equal(t, []string{"main/both"}, merged["both"],
			"main workflow should win over import for the 'both' key")

		// 'sonnet' key: builtin wins over import
		builtins := BuiltinModelAliases()
		assert.Equal(t, builtins["sonnet"], merged["sonnet"],
			"builtin should win over import for the 'sonnet' key")
	})
}

// correctly by ParseFrontmatterConfig.
func TestFrontmatterModelsField(t *testing.T) {
	t.Run("models field is optional", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err, "ParseFrontmatterConfig should succeed without models field")
		require.NotNil(t, config, "parsed config should not be nil")
		assert.Nil(t, config.ModelCosts, "ModelCosts should be nil when not specified in frontmatter")
	})

	t.Run("models field with providers structure populates ModelCosts", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"models": map[string]any{
				"providers": map[string]any{
					"anthropic": map[string]any{
						"models": map[string]any{
							"my-custom-claude": map[string]any{
								"cost": map[string]any{
									"input":  "3e-06",
									"output": "1.5e-05",
								},
							},
						},
					},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err, "ParseFrontmatterConfig should succeed with models providers structure")
		require.NotNil(t, config, "parsed config should not be nil")

		require.NotNil(t, config.ModelCosts, "ModelCosts should be populated from models providers structure")

		providers, ok := config.ModelCosts["providers"].(map[string]any)
		require.True(t, ok, "ModelCosts should contain a providers key")
		assert.Contains(t, providers, "anthropic", "providers should contain anthropic")
	})

	t.Run("models policy fields populate parsed model policy lists", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"models": map[string]any{
				"allowed": []any{"gpt-5", "claude-sonnet"},
				"blocked": []any{"gpt-5-pro"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err, "ParseFrontmatterConfig should succeed with model policy fields")
		require.NotNil(t, config, "parsed config should not be nil")
		assert.Equal(t, []string{"gpt-5", "claude-sonnet"}, config.ModelPolicyAllowed)
		assert.Equal(t, []string{"gpt-5-pro"}, config.ModelPolicyBlocked)
	})

	t.Run("models policy fields ignore invalid entries but keep valid strings", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"models": map[string]any{
				"allowed": []any{"gpt-5", 123, ""},
				"blocked": []any{"claude-opus", false},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err, "ParseFrontmatterConfig should succeed with mixed policy entries")
		require.NotNil(t, config, "parsed config should not be nil")
		assert.Equal(t, []string{"gpt-5"}, config.ModelPolicyAllowed)
		assert.Equal(t, []string{"claude-opus"}, config.ModelPolicyBlocked)
	})
}
