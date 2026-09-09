//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/modelsdev"
)

func TestFindModelPricing(t *testing.T) {
	t.Parallel()
	pricing, ok := findModelPricing("anthropic", "claude-sonnet-4.6")
	require.True(t, ok)
	assert.InDelta(t, 0.000003, pricing["input"], 1e-12)
}

func TestFindKimiK3Pricing(t *testing.T) {
	t.Parallel()
	pricing, ok := findModelPricing("github-copilot", "kimi-k3")
	require.True(t, ok)
	assert.InDelta(t, 0.000003, pricing["input"], 1e-12)
	assert.InDelta(t, 0.000015, pricing["output"], 1e-12)
	assert.InDelta(t, 0.0000003, pricing["cache_read"], 1e-12)
}

func TestFindMaiCode11FlashPricing(t *testing.T) {
	t.Parallel()
	pricing, ok := findModelPricing("github-copilot", "mai-code-1.1-flash")
	require.True(t, ok)
	assert.InDelta(t, 0.0000002, pricing["input"], 1e-12)
	assert.InDelta(t, 0.0000012, pricing["output"], 1e-12)
	assert.InDelta(t, 0.00000002, pricing["cache_read"], 1e-12)
	assert.InDelta(t, 0.00000025, pricing["cache_write"], 1e-12)
}

func TestFindGrok45CacheReadPricing(t *testing.T) {
	t.Parallel()
	pricing, ok := findModelPricing("github-copilot", "grok-4.5")
	require.True(t, ok)
	assert.InDelta(t, 0.000002, pricing["input"], 1e-12)
	assert.InDelta(t, 0.000006, pricing["output"], 1e-12)
	assert.InDelta(t, 0.0000005, pricing["cache_read"], 1e-12)
}

func TestFindGrok46Pricing(t *testing.T) {
	t.Parallel()
	pricing, ok := findModelPricing("github-copilot", "grok-4.6")
	require.True(t, ok)
	assert.InDelta(t, 0.000002, pricing["input"], 1e-12)
	assert.InDelta(t, 0.000006, pricing["output"], 1e-12)
	assert.InDelta(t, 0.0000005, pricing["cache_read"], 1e-12)
}

func TestFindGemini37FlashPricing(t *testing.T) {
	t.Parallel()
	pricing, ok := findModelPricing("github-copilot", "gemini-3.7-flash")
	require.True(t, ok)
	assert.InDelta(t, 0.00000075, pricing["input"], 1e-12)
	assert.InDelta(t, 0.00000375, pricing["output"], 1e-12)
	assert.InDelta(t, 0.000000075, pricing["cache_read"], 1e-12)
}

func TestComputeModelInferenceAIC(t *testing.T) {
	t.Parallel()
	aic := computeModelInferenceAIC("anthropic", "claude-sonnet-4.6", 1000, 200, 400, 50, 25)
	assert.InDelta(t, 0.54825, aic, 1e-9)
}

func TestNormalizeProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"github", "github-copilot"},
		{"copilot", "github-copilot"},
		{"github_models", "github-copilot"},
		{"GITHUB_MODELS", "github-copilot"},
		{"anthropic", "anthropic"},
		{"openai", "openai"},
		{"", ""},
	}
	for _, tt := range tests {
		name := tt.input
		if name == "" {
			name = "<empty>"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := modelsdev.NormalizeProvider(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeModelInferenceAICGitHubModels(t *testing.T) {
	t.Parallel()
	// provider="github_models" is written by the AWF proxy for Copilot engine runs;
	// it must normalize to "github-copilot" so pricing is found and AIC is non-zero.
	aicViaGitHubModels := computeModelInferenceAIC("github_models", "claude-sonnet-4.6", 1000, 200, 0, 0, 0)
	aicViaGitHubCopilot := computeModelInferenceAIC("github-copilot", "claude-sonnet-4.6", 1000, 200, 0, 0, 0)
	assert.Greater(t, aicViaGitHubModels, 0.0, "github_models provider should produce non-zero AIC")
	assert.InDelta(t, aicViaGitHubCopilot, aicViaGitHubModels, 1e-9, "github_models and github-copilot should yield identical AIC")
}

func TestComputeModelInferenceAICCopilotAlias(t *testing.T) {
	t.Parallel()
	// provider="copilot" is another accepted alias for "github-copilot".
	aicViaCopilot := computeModelInferenceAIC("copilot", "claude-sonnet-4.6", 1000, 200, 0, 0, 0)
	aicViaGitHubCopilot := computeModelInferenceAIC("github-copilot", "claude-sonnet-4.6", 1000, 200, 0, 0, 0)
	assert.Greater(t, aicViaCopilot, 0.0, "copilot provider alias should produce non-zero AIC")
	assert.InDelta(t, aicViaGitHubCopilot, aicViaCopilot, 1e-9, "copilot and github-copilot should yield identical AIC")
}

func TestComputeModelInferenceAICGitHubCopilotCacheReadDeduction(t *testing.T) {
	t.Parallel()
	// github-copilot (and its aliases) proxies OpenAI and Anthropic models, which bundle
	// cache-read tokens inside the reported input total (§3.5).  Cache reads MUST be
	// subtracted from input_tokens before applying the input price so that they are not
	// double-charged.
	//
	// Pricing for github-copilot/claude-sonnet-4.6:
	//   input:      $0.000003/token
	//   output:     $0.000015/token
	//   cache_read: $0.0000003/token
	//
	// With 1000 input, 200 output, 400 cache_read (no cache_write, no reasoning):
	//   net input = 1000 − 400 = 600
	//   cost = 600×0.000003 + 200×0.000015 + 400×0.0000003 = 0.0018 + 0.003 + 0.00012 = 0.00492
	//   AIC  = 0.00492 / 0.01 = 0.492
	const wantAIC = 0.492

	for _, provider := range []string{"github-copilot", "github_models", "github", "copilot"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			aic := computeModelInferenceAIC(provider, "claude-sonnet-4.6", 1000, 200, 400, 0, 0)
			assert.InDelta(t, wantAIC, aic, 1e-9,
				"provider=%q: cache reads must not be double-charged (§3.5)", provider)
		})
	}
}

func TestComputeModelInferenceAICGitHubCopilotNoCacheRead(t *testing.T) {
	t.Parallel()
	// With no cache reads, github-copilot pricing should match anthropic exactly:
	// net input remains inputTokens, so no subtraction is applied.
	aicViaGitHubCopilot := computeModelInferenceAIC("github-copilot", "claude-sonnet-4.6", 1000, 200, 0, 0, 0)
	aicViaAnthropic := computeModelInferenceAIC("anthropic", "claude-sonnet-4.6", 1000, 200, 0, 0, 0)
	assert.InDelta(t, aicViaAnthropic, aicViaGitHubCopilot, 1e-9,
		"zero cache reads must not alter the charged input token count")
}

func TestComputeModelInferenceAICExplicitCacheSemantics(t *testing.T) {
	t.Parallel()

	inclusive := true
	additive := false
	inclusiveAIC := computeModelInferenceAICWithCacheSemantics("copilot", "claude-sonnet-4.6", 1000, 100, 400, 100, 0, &inclusive)
	additiveAIC := computeModelInferenceAICWithCacheSemantics("copilot", "claude-sonnet-4.6", 1000, 100, 400, 100, 0, &additive)

	assert.InDelta(t, 0.3495, inclusiveAIC, 1e-9)
	assert.InDelta(t, 0.4995, additiveAIC, 1e-9)
}
