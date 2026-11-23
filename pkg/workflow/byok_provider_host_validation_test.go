//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBYOKProviderHostAllowlist(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		version    string
		allowed    []string
		noFirewall bool
		wantError  bool
	}{
		{name: "expression with old AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.40", allowed: []string{"defaults"}, wantError: true},
		{name: "secret expression with old AWF", baseURL: "${{ secrets.AI_GATEWAY_URL }}", version: "v0.25.40", wantError: true},
		{name: "expression with unknown AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "custom-build", wantError: true},
		{name: "expression with minimum AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.41"},
		{name: "secret expression with minimum AWF", baseURL: "${{ secrets.AI_GATEWAY_URL }}", version: "v0.25.41"},
		{name: "expression with default AWF", baseURL: "${{ vars.AI_GATEWAY_URL }}"},
		{name: "literal URL with old AWF", baseURL: "https://gateway.example.com/v1", version: "v0.25.40"},
		{name: "custom allowed domain cannot be verified", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.40", allowed: []string{"defaults", "gateway.example.com"}, wantError: true},
		{name: "unrelated custom domain", baseURL: "${{ secrets.AI_GATEWAY_URL }}", version: "v0.25.40", allowed: []string{"defaults", "example.com"}, wantError: true},
		{name: "firewall disabled", baseURL: "${{ vars.AI_GATEWAY_URL }}", version: "v0.25.40", noFirewall: true},
		{name: "no base URL", version: "v0.25.40"},
	}
	for _, tt := range tests {
		for _, strict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/strict=%t", tt.name, strict), func(t *testing.T) {
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
				if tt.wantError {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "COPILOT_PROVIDER_BASE_URL")
					assert.Contains(t, err.Error(), "v0.25.41")
					assert.Contains(t, err.Error(), "sandbox.agent.version")
					assert.Contains(t, err.Error(), "sandbox.agent.checksums-sha256")
					assert.Contains(t, err.Error(), "network.allowed entries alone does not bypass")
					assert.Contains(t, err.Error(), "literal URL")
				} else {
					require.NoError(t, err)
				}
				assert.Zero(t, compiler.GetWarningCount())
			})
		}
	}
}

func TestCompileBYOKProviderHostAllowlist(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, context := range []string{"vars", "secrets"} {
			t.Run(fmt.Sprintf("%s/strict=%t", context, strict), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "byok.md")
				content := fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
strict: %t
network: defaults
sandbox:
  agent:
    version: v0.25.40
engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ %s.AI_GATEWAY_URL }}
    COPILOT_PROVIDER_WIRE_API: responses
---
Report.
`, strict, context)
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
				compiler := NewCompiler()
				compiler.SetStrictMode(strict)
				err := compiler.CompileWorkflow(path)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "COPILOT_PROVIDER_BASE_URL")
				assert.Contains(t, err.Error(), "network.allowed")
				assert.Contains(t, err.Error(), "v0.25.41")

				for _, variant := range []struct{ name, sandbox string }{
					{"minimum pin", "sandbox:\n  agent:\n    version: v0.25.41\n"},
					{"no pin", ""},
				} {
					t.Run(variant.name, func(t *testing.T) {
						supported := strings.Replace(content, "sandbox:\n  agent:\n    version: v0.25.40\n", variant.sandbox, 1)
						require.NoError(t, os.WriteFile(path, []byte(supported), 0600))
						compiler := NewCompiler()
						compiler.SetStrictMode(strict)
						require.NoError(t, compiler.CompileWorkflow(path))
						lock, err := os.ReadFile(filepath.Join(filepath.Dir(path), "byok.lock.yml"))
						require.NoError(t, err)
						assert.Contains(t, string(lock), fmt.Sprintf("COPILOT_PROVIDER_BASE_URL: ${{ %s.AI_GATEWAY_URL }}", context))
					})
				}
			})
		}
	}
}

func TestBYOKProviderHostEffectiveEnvironments(t *testing.T) {
	expression := "${{ vars.AI_GATEWAY_URL }}"
	literal := "https://gateway.example.com/v1"
	tests := []struct {
		name       string
		engine     string
		engineURL  string
		agentURL   *string
		detector   *ThreatDetectionConfig
		wantTarget string
	}{
		{name: "agent literal overrides expression", engine: "copilot", engineURL: expression, agentURL: &literal},
		{name: "agent expression overrides literal", engine: "copilot", engineURL: literal, agentURL: &expression, wantTarget: "main agent"},
		{name: "agent expression alone", engine: "copilot", agentURL: &expression, wantTarget: "main agent"},
		{name: "non-copilot ignores shared env", engine: "claude", engineURL: expression},
		{name: "detector inherits engine not agent override", engine: "copilot", engineURL: expression, agentURL: &literal, detector: &ThreatDetectionConfig{}, wantTarget: "threat detection"},
		{name: "detector literal override", engine: "copilot", engineURL: expression, agentURL: &literal, detector: &ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": literal}}}},
		{name: "detector expression override", engine: "copilot", engineURL: literal, detector: &ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": expression}}}, wantTarget: "threat detection"},
		{name: "copilot detector on claude", engine: "claude", detector: &ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": expression}}}, wantTarget: "threat detection"},
		{name: "copilot detector does not inherit claude env", engine: "claude", engineURL: expression, detector: &ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "copilot"}}},
		{name: "disabled detector", engine: "claude", detector: &ThreatDetectionConfig{EngineDisabled: true, EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": expression}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &AgentSandboxConfig{Version: "v0.25.40"}
			if tt.agentURL != nil {
				agent.Env = map[string]string{"COPILOT_PROVIDER_BASE_URL": *tt.agentURL}
			}
			data := &WorkflowData{
				AI:                 tt.engine,
				EngineConfig:       &EngineConfig{ID: tt.engine, Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": tt.engineURL}},
				SandboxConfig:      &SandboxConfig{Agent: agent},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
			}
			if tt.detector != nil {
				data.SafeOutputs = &SafeOutputsConfig{ThreatDetection: tt.detector}
			}
			err := NewCompiler().validateBYOKProviderHostAllowlist(data, "workflow.md")
			if tt.wantTarget == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantTarget)
			}
		})
	}
}

func TestCompileBYOKProviderHostEffectiveEnvironments(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantTarget string
	}{
		{
			name: "main agent literal override",
			config: `network:
  allowed: [defaults, gateway.example.com]
engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ vars.AI_GATEWAY_URL }}
sandbox:
  agent:
    version: v0.25.40
    env:
      COPILOT_PROVIDER_BASE_URL: https://gateway.example.com/v1
safe-outputs:
  noop:
  threat-detection: false
`,
		},
		{
			name: "non-copilot shared environment",
			config: `engine:
  id: claude
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ vars.AI_GATEWAY_URL }}
sandbox:
  agent:
    version: v0.25.40
safe-outputs:
  noop:
  threat-detection: false
`,
		},
		{
			name: "main agent expression override",
			config: `engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: https://gateway.example.com/v1
sandbox:
  agent:
    version: v0.25.40
    env:
      COPILOT_PROVIDER_BASE_URL: ${{ vars.AI_GATEWAY_URL }}
safe-outputs:
  noop:
  threat-detection: false
`,
			wantTarget: "main agent",
		},
		{
			name: "independent copilot detector",
			config: `engine: claude
sandbox:
  agent:
    version: v0.25.40
safe-outputs:
  noop:
  threat-detection:
    engine:
      id: copilot
      env:
        COPILOT_PROVIDER_BASE_URL: ${{ vars.AI_GATEWAY_URL }}
`,
			wantTarget: "threat detection",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "byok.md")
			content := "---\non: workflow_dispatch\npermissions:\n  contents: read\nstrict: false\n" + tt.config + "---\nReport.\n"
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			err := NewCompiler().CompileWorkflow(path)
			if tt.wantTarget == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantTarget)
			}
		})
	}
}
