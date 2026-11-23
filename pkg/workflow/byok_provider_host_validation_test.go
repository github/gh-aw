//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBYOKProviderHostAllowlist(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		version     string
		allowed     []string
		noFirewall  bool
		wantWarning bool
	}{
		{name: "expression with old AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.40", allowed: []string{"defaults"}, wantWarning: true},
		{name: "secret expression with old AWF", baseURL: "${{ secrets.AI_GATEWAY_URL }}", version: "v0.25.40", wantWarning: true},
		{name: "expression with minimum AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.41"},
		{name: "expression with default AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}"},
		{name: "literal URL with old AWF", baseURL: "https://gateway.example.com/v1", version: "v0.25.40"},
		{name: "custom allowed domain cannot be verified", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.40", allowed: []string{"defaults", "gateway.example.com"}, wantWarning: true},
		{name: "unrelated custom domain", baseURL: "${{ secrets.AI_GATEWAY_URL }}", version: "v0.25.40", allowed: []string{"defaults", "example.com"}, wantWarning: true},
		{name: "firewall disabled", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.40", noFirewall: true},
		{name: "no base URL", version: "v0.25.40"},
	}
	for _, tt := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(tt.name, func(t *testing.T) {
				env := map[string]string{}
				if tt.baseURL != "" {
					env["COPILOT_PROVIDER_BASE_URL"] = tt.baseURL
				}
				data := &WorkflowData{
					AI:           "copilot",
					EngineConfig: &EngineConfig{ID: "copilot", Env: env},
					NetworkPermissions: &NetworkPermissions{
						Allowed:  tt.allowed,
						Firewall: &FirewallConfig{Enabled: !tt.noFirewall, Version: tt.version},
					},
				}
				compiler := NewCompiler()
				compiler.SetStrictMode(strict)
				err := compiler.validateBYOKProviderHostAllowlist(data, "workflow.md")
				switch {
				case tt.wantWarning && strict:
					require.Error(t, err)
					assert.Contains(t, err.Error(), "strict mode")
					assert.Contains(t, err.Error(), "v0.25.41")
					assert.Contains(t, err.Error(), "network.allowed")
				case tt.wantWarning:
					require.NoError(t, err)
					assert.Equal(t, 1, compiler.GetWarningCount())
				default:
					require.NoError(t, err)
					assert.Zero(t, compiler.GetWarningCount())
				}
			})
		}
	}
}
