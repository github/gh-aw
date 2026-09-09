//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"
)

// TestValidateStrictSandboxCustomization tests that internal sandbox fields are
// rejected in strict mode.
func TestValidateStrictSandboxCustomization(t *testing.T) {
	tests := []struct {
		name        string
		sandbox     *SandboxConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil sandbox config is allowed",
			sandbox:     nil,
			expectError: false,
		},
		{
			name: "basic awf sandbox without customization is allowed",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID: "awf",
				},
			},
			expectError: false,
		},
		{
			name: "sandbox.agent.command is rejected",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:      "awf",
					Command: "/usr/local/bin/custom-awf",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.agent.command' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.agent.args is rejected",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:   "awf",
					Args: []string{"--debug"},
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.agent.args' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.agent.env is rejected",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:  "awf",
					Env: map[string]string{"DEBUG": "true"},
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.agent.env' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.mcp.container is rejected",
			sandbox: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: "ghcr.io/example/gateway",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.mcp.container' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.mcp.version is rejected",
			sandbox: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Version: "v1.0.0",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.mcp.version' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.mcp.entrypoint is rejected",
			sandbox: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Entrypoint: "/custom/start.sh",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.mcp.entrypoint' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.mcp.args is rejected",
			sandbox: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Args: []string{"--verbose"},
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.mcp.args' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.mcp.entrypointArgs is rejected",
			sandbox: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					EntrypointArgs: []string{"--listen", "0.0.0.0:8000"},
				},
			},
			expectError: true,
			errorMsg:    "strict mode: 'sandbox.mcp.entrypointArgs' is not allowed because it is an internal implementation detail",
		},
		{
			name: "sandbox.mcp with only allowed fields is permitted",
			sandbox: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Port:    8080,
					AgentID: "${{ secrets.MCP_KEY }}",
				},
			},
			expectError: false,
		},
		{
			name: "sandbox.agent.mounts is allowed (not an internal field)",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					ID:     "awf",
					Mounts: []string{"/host/data:/data:ro"},
				},
			},
			expectError: false,
		},
		{
			// A bare sandbox.agent object with no id/type is explicitly set to AWF in strict mode.
			name: "sandbox.agent without id is allowed in strict mode (defaults to awf)",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Version: "v0.25.29",
				},
			},
			expectError: false,
		},
		{
			// An empty AgentSandboxConfig (no id, no type, no version) is explicitly set to AWF in strict mode.
			name: "empty sandbox.agent is allowed in strict mode (defaults to awf)",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{},
			},
			expectError: false,
		},
		{
			// sandbox.agent: false (Disabled) is handled by validateStrictFirewall, not here.
			// validateStrictSandboxCustomization must not produce an additional error for it.
			name: "disabled sandbox.agent is not rejected here (handled by validateStrictFirewall)",
			sandbox: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = true

			err := compiler.validateStrictSandboxCustomization(tt.sandbox)

			if tt.expectError && err == nil {
				t.Error("Expected validation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

// TestValidateStrictSandboxCustomizationNonStrictMode verifies that internal fields
// are not rejected when strict mode is disabled.
func TestValidateStrictSandboxCustomizationNonStrictMode(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = false

	sandbox := &SandboxConfig{
		Agent: &AgentSandboxConfig{
			ID:      "awf",
			Command: "/custom/awf",
			Args:    []string{"--debug"},
			Env:     map[string]string{"LOG": "verbose"},
		},
		MCP: &MCPGatewayRuntimeConfig{
			Container:      "ghcr.io/example/gateway",
			Version:        "latest",
			Entrypoint:     "/bin/sh",
			Args:           []string{"--rm"},
			EntrypointArgs: []string{"--listen", "0.0.0.0"},
		},
	}

	err := compiler.validateStrictSandboxCustomization(sandbox)
	if err != nil {
		t.Errorf("Expected non-strict mode to allow all sandbox fields, got error: %v", err)
	}
}

// TestValidateStrictSandboxCustomizationSetsAWFDefault verifies that in strict mode
// a sandbox.agent with no id/type is explicitly set to AWF.
func TestValidateStrictSandboxCustomizationSetsAWFDefault(t *testing.T) {
	tests := []struct {
		name  string
		agent *AgentSandboxConfig
	}{
		{
			name:  "version-only agent gets AWF type",
			agent: &AgentSandboxConfig{Version: "v0.25.29"},
		},
		{
			name:  "empty agent gets AWF type",
			agent: &AgentSandboxConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = true

			sandbox := &SandboxConfig{Agent: tt.agent}
			err := compiler.validateStrictSandboxCustomization(sandbox)
			if err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			}
			if sandbox.Agent.Type != SandboxTypeAWF {
				t.Errorf("Expected sandbox.agent.Type to be %q after strict mode validation, got %q", SandboxTypeAWF, sandbox.Agent.Type)
			}
		})
	}
}

// TestValidateStrictSandboxCustomizationRuntimeProfiles tests that every supported
// runtime profile passes strict-mode sandbox customization validation: the privileged
// setup each runtime needs is derived by the compiler instead of being requested with
// a dedicated sudo field.
func TestValidateStrictSandboxCustomizationRuntimeProfiles(t *testing.T) {
	for _, runtime := range append([]AgentRuntime{""}, supportedAgentRuntimes...) {
		for _, strict := range []bool{true, false} {
			t.Run(fmt.Sprintf("runtime=%q strict=%v", runtime, strict), func(t *testing.T) {
				sandbox := &SandboxConfig{
					Agent: &AgentSandboxConfig{
						ID:      "awf",
						Runtime: runtime,
					},
				}

				compiler := NewCompiler()
				compiler.strictMode = strict
				initialWarnings := compiler.GetWarningCount()

				if err := compiler.validateStrictSandboxCustomization(sandbox); err != nil {
					t.Errorf("Expected no error for runtime %q (strict=%v), got: %v", runtime, strict, err)
				}
				if compiler.GetWarningCount() != initialWarnings {
					t.Errorf("Expected no warnings for runtime %q (strict=%v)", runtime, strict)
				}
			})
		}
	}
}
