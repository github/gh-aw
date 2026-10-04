//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awfDynamicModelAiCreditsMinVersion is the AWF release that added first-class AI-credits
// accounting for recognized dynamic model selectors such as Copilot's server-side "auto".
// From this version on, gh-aw does not emit any compiler-generated pricing overlay for
// dynamic selectors: the API proxy accounts the concrete model reported by the response.
const awfDynamicModelAiCreditsMinVersion = "v0.28.4"

// TestZeroConfigCopilotAutoModelWithMaxAiCredits is the contract test for zero-config
// Copilot `auto` running under an AI-credits budget. It proves that gh-aw relies on
// native AWF accounting instead of synthesizing a `github-copilot/auto` pricing entry.
func TestZeroConfigCopilotAutoModelWithMaxAiCredits(t *testing.T) {
	t.Run("default AWF version provides native dynamic-selector accounting", func(t *testing.T) {
		assert.True(t,
			versionAtLeast("", string(constants.DefaultFirewallVersion), awfDynamicModelAiCreditsMinVersion),
			"DefaultFirewallVersion %s must be at least %s so dynamic model selectors are accounted natively",
			constants.DefaultFirewallVersion, awfDynamicModelAiCreditsMinVersion)
	})

	t.Run("unknown static models receive default fallback pricing", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				Model: "gpt-6.1-sol",
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		var parsed struct {
			APIProxy struct {
				DefaultAiCreditsPricing AiCreditsPricingConfig `json:"defaultAiCreditsPricing"`
			} `json:"apiProxy"`
		}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		assert.Equal(t, AiCreditsPricingConfig{Input: 3, Output: 15}, parsed.APIProxy.DefaultAiCreditsPricing)
	})

	t.Run("no synthetic auto pricing overlay is emitted with maxAiCredits", func(t *testing.T) {
		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData: &WorkflowData{
				Model: "copilot/auto",
				EngineConfig: &EngineConfig{
					ID:           "copilot",
					MaxAICredits: 1000,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
		}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)
		assert.Contains(t, jsonStr, `"maxAiCredits":1000`, "the AI-credits budget must reach the API proxy")

		var parsed struct {
			APIProxy struct {
				Providers               map[string]any `json:"providers"`
				DefaultAiCreditsPricing map[string]any `json:"defaultAiCreditsPricing"`
			} `json:"apiProxy"`
		}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
		assert.Empty(t, parsed.APIProxy.Providers,
			"no compiler-generated provider pricing overlay may be emitted for the dynamic 'auto' selector")
		assert.Empty(t, parsed.APIProxy.DefaultAiCreditsPricing,
			"no fallback pricing may be synthesized for the dynamic 'auto' selector")
	})

	t.Run("concrete models keep their own resolved pricing", func(t *testing.T) {
		c := &Compiler{}
		c.modelPricingResolver = func(_ context.Context, _, _ string) (map[string]float64, bool) {
			return map[string]float64{"input": 1.25e-06, "output": 1e-05}, true
		}

		workflowData := &WorkflowData{
			Model:        "gpt-5.1",
			EngineConfig: &EngineConfig{ID: "copilot"},
		}
		workflowData.ModelCosts = c.resolveModelPricingIfMissing(nil, workflowData)

		config := AWFCommandConfig{
			EngineName:     "copilot",
			AllowedDomains: "github.com",
			WorkflowData:   workflowData,
		}
		workflowData.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}

		jsonStr, err := BuildAWFConfigJSON(config)
		require.NoError(t, err)

		var parsed struct {
			APIProxy struct {
				Providers map[string]struct {
					Models map[string]any `json:"models"`
				} `json:"providers"`
			} `json:"apiProxy"`
		}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))

		copilotProvider, ok := parsed.APIProxy.Providers["github-copilot"]
		require.True(t, ok, "concrete model pricing must be emitted for the resolved model")
		assert.Contains(t, copilotProvider.Models, "gpt-5.1")
		assert.NotContains(t, copilotProvider.Models, "auto",
			"the dynamic selector must never receive its own pricing entry")
	})
}
