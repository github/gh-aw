//go:build !integration

package workflow

import (
	"context"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── modelCostsHasPricingFor ──────────────────────────────────────────────────

func TestModelCostsHasPricingFor(t *testing.T) {
	costs := map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"models": map[string]any{
					"claude-new-model": map[string]any{
						"cost": map[string]string{"input": "3e-06"},
					},
				},
			},
		},
	}

	assert.True(t, modelCostsHasPricingFor(costs, "anthropic", "claude-new-model"))
	assert.True(t, modelCostsHasPricingFor(costs, "", "claude-new-model")) // any provider
	assert.False(t, modelCostsHasPricingFor(costs, "anthropic", "does-not-exist"))
	assert.False(t, modelCostsHasPricingFor(costs, "openai", "claude-new-model")) // wrong provider
	assert.False(t, modelCostsHasPricingFor(nil, "anthropic", "claude-new-model"))
	assert.False(t, modelCostsHasPricingFor(map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"models": map[string]any{
					"custom": map[string]any{},
				},
			},
		},
	}, "openai", "custom"))
	assert.False(t, modelCostsHasPricingFor(map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"models": map[string]any{
					"custom": map[string]any{"cost": map[string]any{}},
				},
			},
		},
	}, "openai", "custom"))
}

// ── mergeModelPricingIntoModelCosts ─────────────────────────────────────────

func TestMergeModelPricingIntoModelCosts_EmptyBase(t *testing.T) {
	pricing := map[string]float64{"input": 3e-06, "output": 15e-06}
	result := mergeModelPricingIntoModelCosts(nil, "anthropic", "claude-new-model", pricing)

	providers, ok := result["providers"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, providers, "anthropic")

	prov := providers["anthropic"].(map[string]any)
	models := prov["models"].(map[string]any)
	entry := models["claude-new-model"].(map[string]any)
	cost := entry["cost"].(map[string]string)
	assert.Equal(t, "3e-06", cost["input"])
	assert.Equal(t, "1.5e-05", cost["output"])
}

func TestMergeModelPricingIntoModelCosts_PreservesExisting(t *testing.T) {
	existing := map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"models": map[string]any{
					"existing-model": map[string]any{"cost": map[string]string{"input": "1e-06"}},
				},
			},
		},
	}
	pricing := map[string]float64{"input": 3e-06}
	result := mergeModelPricingIntoModelCosts(existing, "anthropic", "new-model", pricing)

	providers := result["providers"].(map[string]any)
	models := providers["anthropic"].(map[string]any)["models"].(map[string]any)
	assert.Contains(t, models, "existing-model", "existing entry must be preserved")
	assert.Contains(t, models, "new-model")
}

func TestMergeModelPricingIntoModelCosts_DoesNotMutateInput(t *testing.T) {
	base := map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"models": map[string]any{},
			},
		},
	}
	pricing := map[string]float64{"input": 1e-06}
	_ = mergeModelPricingIntoModelCosts(base, "openai", "new-model", pricing)

	// Original map must be unchanged.
	openai := base["providers"].(map[string]any)["openai"].(map[string]any)
	assert.Empty(t, openai["models"].(map[string]any))
}

func TestMergeModelPricingIntoModelCosts_DoesNotPreSizeTopLevelClone(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")

	sourcePath := filepath.Join(filepath.Dir(thisFile), "compiler_model_pricing.go")
	file, err := parser.ParseFile(gotoken.NewFileSet(), sourcePath, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	var foundResultMake bool
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "mergeModelPricingIntoModelCosts" {
			return true
		}

		for _, stmt := range fn.Body.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				continue
			}
			lhs, ok := assign.Lhs[0].(*ast.Ident)
			if !ok || lhs.Name != "result" {
				continue
			}
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			fun, ok := call.Fun.(*ast.Ident)
			if !ok || fun.Name != "make" {
				continue
			}

			foundResultMake = true
			assert.Len(t, call.Args, 1, "result map allocation should not pre-size capacity")
			return false
		}
		return false
	})

	assert.True(t, foundResultMake, "expected to find result map allocation in mergeModelPricingIntoModelCosts")
}

// ── resolveEngineProviderForPricing ─────────────────────────────────────────

func TestResolveEngineProviderForPricing(t *testing.T) {
	cases := []struct {
		desc   string
		config *EngineConfig
		want   string
	}{
		{"LLMProvider wins", &EngineConfig{LLMProvider: LLMProviderOpenAI, InlineProviderID: "other", ID: "claude"}, "openai"},
		{"LLMProvider alias normalized", &EngineConfig{LLMProvider: LLMProvider("github_models"), ID: "claude"}, "github-copilot"},
		{"InlineProviderID second", &EngineConfig{InlineProviderID: "openai", ID: "claude"}, "openai"},
		{"claude engine → anthropic", &EngineConfig{ID: "claude"}, "anthropic"},
		{"codex engine → openai", &EngineConfig{ID: "codex"}, "openai"},
		{"copilot engine → github-copilot", &EngineConfig{ID: "copilot"}, "github-copilot"},
		{"empty engine → github-copilot", &EngineConfig{}, "github-copilot"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := resolveEngineProviderForPricing(tc.config)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ── resolveModelPricingIfMissing ────────────────────────────────────────────

func TestResolveModelPricingIfMissing_NilResolver(t *testing.T) {
	c := &Compiler{}
	// No resolver registered — should be a no-op.
	result := c.resolveModelPricingIfMissing(nil, &WorkflowData{Model: "gpt-99", EngineConfig: &EngineConfig{ID: "codex"}})
	assert.Nil(t, result)
}

func TestResolveModelPricingIfMissing_AlreadyPresent(t *testing.T) {
	existing := map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"models": map[string]any{
					"gpt-99": map[string]any{"cost": map[string]string{"input": "1e-06"}},
				},
			},
		},
	}
	called := false
	c := &Compiler{}
	c.modelPricingResolver = func(_ context.Context, _, _ string) (map[string]float64, bool) {
		called = true
		return nil, false
	}
	result := c.resolveModelPricingIfMissing(existing, &WorkflowData{Model: "gpt-99", EngineConfig: &EngineConfig{ID: "codex"}})
	assert.False(t, called, "resolver should not be called when pricing is already present")
	assert.Equal(t, existing, result)
}

func TestResolveModelPricingIfMissing_InjectsFromResolver(t *testing.T) {
	c := &Compiler{}
	c.modelPricingResolver = func(_ context.Context, provider, model string) (map[string]float64, bool) {
		if provider == "anthropic" && model == "claude-new-model" {
			return map[string]float64{"input": 3e-06, "output": 15e-06}, true
		}
		return nil, false
	}

	result := c.resolveModelPricingIfMissing(nil, &WorkflowData{Model: "claude-new-model?effort=high", EngineConfig: &EngineConfig{ID: "claude"}})
	require.NotNil(t, result)
	providers := result["providers"].(map[string]any)
	require.Contains(t, providers, "anthropic")
	models := providers["anthropic"].(map[string]any)["models"].(map[string]any)
	assert.Contains(t, models, "claude-new-model")
}

func TestResolveModelPricingIfMissing_ResolverReturnsNothing(t *testing.T) {
	c := &Compiler{}
	c.modelPricingResolver = func(_ context.Context, _, _ string) (map[string]float64, bool) {
		return nil, false
	}
	result := c.resolveModelPricingIfMissing(nil, &WorkflowData{Model: "mystery-model", EngineConfig: &EngineConfig{ID: "claude"}})
	// Should return the original (nil) map unchanged.
	assert.Nil(t, result)
}

func TestResolveModelPricingIfMissing_SplitsQualifiedModelAndNormalizesProvider(t *testing.T) {
	c := &Compiler{}
	c.modelPricingResolver = func(_ context.Context, provider, model string) (map[string]float64, bool) {
		assert.Equal(t, "github-copilot", provider)
		assert.Equal(t, "claude-sonnet-4.6", model)
		return map[string]float64{"input": 3e-06}, true
	}

	result := c.resolveModelPricingIfMissing(nil, &WorkflowData{
		Model:        "github_models/claude-sonnet-4.6",
		EngineConfig: &EngineConfig{ID: "unknown-engine"},
	})
	require.NotNil(t, result)
	providers := result["providers"].(map[string]any)
	require.Contains(t, providers, "github-copilot")
	models := providers["github-copilot"].(map[string]any)["models"].(map[string]any)
	assert.Contains(t, models, "claude-sonnet-4.6")
}

func TestResolveModelPricingIfMissing_SkipsWhenProviderCannotBeNormalized(t *testing.T) {
	c := &Compiler{}
	called := false
	c.modelPricingResolver = func(_ context.Context, _, _ string) (map[string]float64, bool) {
		called = true
		return map[string]float64{"input": 1e-06}, true
	}

	result := c.resolveModelPricingIfMissing(nil, &WorkflowData{
		Model:        "claude-sonnet-4.6",
		EngineConfig: &EngineConfig{ID: "unknown-engine"},
	})

	assert.False(t, called)
	assert.Nil(t, result)
}

func TestResolveModelPricingIfMissing_SkipsMalformedQualifiedModel(t *testing.T) {
	c := &Compiler{}
	called := false
	c.modelPricingResolver = func(_ context.Context, _, _ string) (map[string]float64, bool) {
		called = true
		return map[string]float64{"input": 1e-06}, true
	}

	assert.Nil(t, c.resolveModelPricingIfMissing(nil, &WorkflowData{Model: "/gpt-4.1", EngineConfig: &EngineConfig{}}))
	assert.Nil(t, c.resolveModelPricingIfMissing(nil, &WorkflowData{Model: "openai/", EngineConfig: &EngineConfig{}}))
	assert.False(t, called)
}

func TestResolveModelPricingIfMissing_SkipsDynamicAutoModelAlias(t *testing.T) {
	c := &Compiler{}
	called := false
	c.modelPricingResolver = func(_ context.Context, _, _ string) (map[string]float64, bool) {
		called = true
		return map[string]float64{"input": 1e-06}, true
	}

	assert.Nil(t, c.resolveModelPricingIfMissing(nil, &WorkflowData{
		Model:        "auto",
		EngineConfig: &EngineConfig{ID: "copilot"},
	}))
	assert.Nil(t, c.resolveModelPricingIfMissing(nil, &WorkflowData{
		Model:        "github_models/auto",
		EngineConfig: &EngineConfig{ID: "copilot"},
	}))
	assert.False(t, called)
}
