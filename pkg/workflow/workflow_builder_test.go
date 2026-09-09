//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeFrontmatterPlugins(t *testing.T) {
	got := mergeFrontmatterPlugins(
		&FrontmatterConfig{PluginReferences: []PluginReference{{Plugin: "main-a"}}},
		map[string]any{},
		[]string{"import-a", "import-b"},
		nil,
	)
	assert.Equal(t, []string{"main-a", "import-a", "import-b"}, got)
}

func TestResolveInlinedImports(t *testing.T) {
	assert.True(t, resolveInlinedImports(map[string]any{"inlined-imports": true}))
	assert.False(t, resolveInlinedImports(map[string]any{"inlined-imports": false}))
	assert.False(t, resolveInlinedImports(map[string]any{}))
}

func TestWorkflowBuilderExtractConcurrencySection(t *testing.T) {
	compiler := NewCompiler()

	t.Run("strips job-discriminator from rendered concurrency", func(t *testing.T) {
		got := compiler.extractConcurrencySection(map[string]any{
			"concurrency": map[string]any{
				"group":              "workflow-${{ github.ref }}",
				"cancel-in-progress": true,
				"job-discriminator":  "${{ github.event.number }}",
			},
		})
		assert.Contains(t, got, "group:")
		assert.Contains(t, got, "cancel-in-progress")
		assert.NotContains(t, got, "job-discriminator")
	})

	t.Run("returns empty when discriminator is the only field", func(t *testing.T) {
		got := compiler.extractConcurrencySection(map[string]any{
			"concurrency": map[string]any{"job-discriminator": "x"},
		})
		assert.Empty(t, got)
	})
}

func TestMergeModelCostOverlayPair(t *testing.T) {
	base := map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"endpoint": "https://api.openai.com",
				"models": map[string]any{
					"gpt-4": map[string]any{"input": 1.0},
				},
			},
		},
	}
	overlay := map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"models": map[string]any{
					"gpt-4": map[string]any{"input": 2.0},
					"gpt-5": map[string]any{"input": 3.0},
				},
			},
			"anthropic": map[string]any{
				"models": map[string]any{"claude": map[string]any{"input": 4.0}},
			},
		},
	}

	got := mergeModelCostOverlayPair(base, overlay)
	providers, ok := got["providers"].(map[string]any)
	require.True(t, ok)

	openai, ok := providers["openai"].(map[string]any)
	require.True(t, ok)
	models, ok := openai["models"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, models, "gpt-4")
	assert.Contains(t, models, "gpt-5")
	assert.Equal(t, "https://api.openai.com", openai["endpoint"])
	assert.Contains(t, providers, "anthropic")
}

func TestExtractDefaultAiCreditsPricingFromObject(t *testing.T) {
	pricing := extractDefaultAiCreditsPricingFromObject(map[string]any{
		"input":       1,
		"output":      2.5,
		"cache_read":  0.5,
		"cache_write": 0.25,
	})
	require.NotNil(t, pricing)
	assert.InDelta(t, 1.0, pricing.Input, 0.000001)
	assert.InDelta(t, 2.5, pricing.Output, 0.000001)
	require.NotNil(t, pricing.CachedInput)
	require.NotNil(t, pricing.CacheWrite)
	assert.InDelta(t, 0.5, *pricing.CachedInput, 0.000001)
	assert.InDelta(t, 0.25, *pricing.CacheWrite, 0.000001)
}
