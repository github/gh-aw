package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSDKModelIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		model, want string
	}{
		{"copilot-responses/gpt-5.6-luna", "gpt-5.6-luna"},
		{"copilot-responses/gpt-5.4-mini-2026-03-17:copilot-responses", "gpt-5.4-mini"},
		{"copilot-completions/claude-haiku-4.5", "claude-haiku-4.5"},
		{"copilot-completions/claude-haiku-4-5-20251001:copilot-completions", "claude-haiku-4.5"},
		{" COPILOT-COMPLETIONS/CLAUDE-HAIKU-4-5-20251001?effort=low ", "claude-haiku-4.5"},
		{"openai/gpt-5.4-mini", "gpt-5.4-mini"},
		{"copilot-responses/gpt-5.4-mini:other", "gpt-5.4-mini:other"},
		{"copilot-responses-1/gpt-5.4-mini:copilot-responses-1", "gpt-5.4-mini"},
		{"copilot-completions-2/claude-haiku-4-5-20251001:copilot-completions-2", "claude-haiku-4.5"},
	} {
		t.Run(test.model, func(t *testing.T) {
			require.Equal(t, test.want, normalizeModelIdentity(test.model))
			require.Equal(t, test.want, newModelIdentityResolver("").resolve(test.model, "", nil))
		})
	}
}

func TestModelIdentityMatchesSDKProviders(t *testing.T) {
	t.Parallel()
	resolver := newModelIdentityResolver("")
	for _, test := range []struct {
		pattern, observed, provider string
		want                        bool
	}{
		{"copilot/gpt-*", "copilot-responses/gpt-5.6-luna:copilot-responses", "github-copilot", true},
		{"copilot/claude-haiku-4.5", "copilot-completions/claude-haiku-4-5-20251001:copilot-completions", "github-copilot", true},
		{"claude-haiku-4.5", "copilot-completions/claude-haiku-4-5-20251001:copilot-completions", "", true},
		{"copilot/claude-*", "copilot-completions/claude-haiku-4.5", "", true},
		{"anthropic/claude-*", "copilot-completions/claude-haiku-4.5", "", false},
		{"copilot/gpt-*", "copilot-completions/claude-haiku-4.5", "", false},
		{"copilot/claude-*", "claude-haiku-4.5", "anthropic", false},
		{"copilot/gpt-*", "copilot-responses-1/gpt-5.6-luna:copilot-responses-1", "github-copilot", true},
		{"copilot/claude-*", "copilot-completions-2/claude-haiku-4.5:copilot-completions-2", "", true},
		{"copilot/gpt-*", "copilot-arbitrary/gpt-5.6-luna", "", false},
		{"copilot/gpt-*", "copilot-responses-extra/gpt-5.6-luna", "", false},
		{"copilot/claude-*", "copilot-completions-1-extra/claude-haiku-4.5", "", false},
	} {
		t.Run(test.pattern+"_"+test.observed+"_"+test.provider, func(t *testing.T) {
			require.Equal(t, test.want, resolver.matches(test.pattern, test.observed, test.provider))
		})
	}

	require.Equal(t, "claude-haiku-4.5", resolver.resolve("haiku", "github-copilot", []string{
		"copilot-completions/claude-haiku-4-5-20251001:copilot-completions",
	}))
}

func TestNormalizeSDKModelProvider(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"copilot-responses", "copilot-completions", "copilot-responses-1", "copilot-completions-12", " COPILOT-RESPONSES-2 "} {
		require.Equal(t, "github-copilot", normalizeModelProvider(provider))
	}
	for _, provider := range []string{"copilot-arbitrary", "copilot-responses-extra", "copilot-completions-1-extra", "copilot-responses-", "copilot-responses-1.2", "other-copilot-responses-1"} {
		require.Equal(t, provider, normalizeModelProvider(provider))
	}
}

func TestSDKQualifiedModelsMergeAttributionWithoutMixingFamilies(t *testing.T) {
	t.Parallel()
	resolver := newModelIdentityResolver("")
	actuals := resolveSubagentActualModels([]SubagentModelActual{
		{Model: "copilot-responses/gpt-5.4-mini:copilot-responses", Provider: "github-copilot", Requests: 2, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 20}, AIC: 0.2},
		{Model: "gpt-5.4-mini", Provider: "github-copilot", Requests: 1, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10}, AIC: 0.1},
		{Model: "copilot-completions/claude-haiku-4-5-20251001:copilot-completions", Provider: "github-copilot", Requests: 1, TokenCoreMetrics: TokenCoreMetrics{InputTokens: 40}, AIC: 0.4},
	}, resolver, "gpt-5.4-mini", "claude-haiku-4.5")
	require.Len(t, actuals, 2)
	require.Equal(t, "gpt-5.4-mini", actuals[0].ResolvedModel)
	require.Equal(t, 3, actuals[0].Requests)
	require.Equal(t, 30, actuals[0].InputTokens)
	require.InDelta(t, 0.3, actuals[0].AIC, 0.000001)
	require.NotContains(t, actuals[0].ServedModels, "claude-haiku-4.5")
	require.Equal(t, "claude-haiku-4.5", actuals[1].ResolvedModel)
	require.Equal(t, 1, actuals[1].Requests)
	require.Equal(t, 40, actuals[1].InputTokens)
	require.InDelta(t, 0.4, actuals[1].AIC, 0.000001)
	require.NotContains(t, actuals[1].ServedModels, "gpt-5.4-mini")
}
