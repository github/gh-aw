//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestSandboxTypeEnumValidation tests that sandbox type enum values are correctly validated
func TestSandboxTypeEnumValidation(t *testing.T) {
	tests := []struct {
		name        string
		sandboxType SandboxType
		expectValid bool
	}{
		// Valid enum values
		{
			name:        "valid type: awf",
			sandboxType: SandboxTypeAWF,
			expectValid: true,
		},
		{
			name:        "valid type: srt",
			sandboxType: SandboxTypeSRT,
			expectValid: true,
		},
		{
			name:        "valid type: default (backward compat)",
			sandboxType: SandboxTypeDefault,
			expectValid: true,
		},
		{
			name:        "valid type: sandbox-runtime (backward compat)",
			sandboxType: SandboxTypeRuntime,
			expectValid: true,
		},
		// Invalid enum values
		{
			name:        "invalid type: AWF (uppercase)",
			sandboxType: "AWF",
			expectValid: false,
		},
		{
			name:        "invalid type: SRT (uppercase)",
			sandboxType: "SRT",
			expectValid: false,
		},
		{
			name:        "invalid type: Default (mixed case)",
			sandboxType: "Default",
			expectValid: false,
		},
		{
			name:        "invalid type: Sandbox-Runtime (mixed case)",
			sandboxType: "Sandbox-Runtime",
			expectValid: false,
		},
		{
			name:        "invalid type: random string",
			sandboxType: "random",
			expectValid: false,
		},
		{
			name:        "invalid type: empty string",
			sandboxType: "",
			expectValid: false,
		},
		{
			name:        "invalid type: docker",
			sandboxType: "docker",
			expectValid: false,
		},
		{
			name:        "invalid type: container",
			sandboxType: "container",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := isSupportedSandboxType(tt.sandboxType)
			if isValid != tt.expectValid {
				t.Errorf("isSupportedSandboxType(%q) = %v, want %v", tt.sandboxType, isValid, tt.expectValid)
			}
		})
	}
}

// TestSandboxTypeBackwardCompatibility tests backward compatibility of sandbox type aliases
func TestSandboxTypeBackwardCompatibility(t *testing.T) {
	tests := []struct {
		name         string
		legacyType   SandboxType
		expectedType SandboxType
		shouldBeSRT  bool
		shouldBeAWF  bool
		description  string
	}{
		{
			name:         "default should be treated as AWF",
			legacyType:   SandboxTypeDefault,
			expectedType: SandboxTypeAWF,
			shouldBeAWF:  true,
			shouldBeSRT:  false,
			description:  "default is an alias for awf",
		},
		{
			name:         "sandbox-runtime should be treated as SRT",
			legacyType:   SandboxTypeRuntime,
			expectedType: SandboxTypeSRT,
			shouldBeSRT:  true,
			shouldBeAWF:  false,
			description:  "sandbox-runtime is an alias for srt",
		},
		{
			name:         "awf is the canonical AWF type",
			legacyType:   SandboxTypeAWF,
			expectedType: SandboxTypeAWF,
			shouldBeAWF:  true,
			shouldBeSRT:  false,
			description:  "awf is the canonical name",
		},
		{
			name:         "srt is the canonical SRT type",
			legacyType:   SandboxTypeSRT,
			expectedType: SandboxTypeSRT,
			shouldBeSRT:  true,
			shouldBeAWF:  false,
			description:  "srt is the canonical name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test using a workflow with the specified sandbox type
			workflowData := &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Type: tt.legacyType,
				},
			}

			isSRT := isSRTEnabled(workflowData)
			if isSRT != tt.shouldBeSRT {
				t.Errorf("%s: isSRTEnabled() = %v, want %v", tt.description, isSRT, tt.shouldBeSRT)
			}

			// Also test with new agent format
			workflowData2 := &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: tt.legacyType,
					},
				},
			}

			isSRT2 := isSRTEnabled(workflowData2)
			if isSRT2 != tt.shouldBeSRT {
				t.Errorf("%s (new format): isSRTEnabled() = %v, want %v", tt.description, isSRT2, tt.shouldBeSRT)
			}
		})
	}
}

// TestSandboxConfigValidationWithInvalidTypes tests validation with invalid sandbox types
func TestSandboxConfigValidationWithInvalidTypes(t *testing.T) {
	tests := []struct {
		name      string
		config    *SandboxConfig
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid AWF config",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
			expectErr: false,
		},
		{
			name: "valid SRT config with copilot engine",
			config: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeSRT,
				},
			},
			expectErr: false, // Will be validated in context with engine
		},
		{
			name: "valid default (backward compat)",
			config: &SandboxConfig{
				Type: SandboxTypeDefault,
			},
			expectErr: false,
		},
		{
			name: "valid sandbox-runtime (backward compat)",
			config: &SandboxConfig{
				Type: SandboxTypeRuntime,
			},
			expectErr: false, // Will be validated in context with engine
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal workflow data for validation
			workflowData := &WorkflowData{
				Name:          "test-workflow",
				SandboxConfig: tt.config,
				EngineConfig: &EngineConfig{
					ID: "copilot", // SRT requires copilot engine
				},
				Tools: map[string]any{
					"github": map[string]any{}, // Add MCP server to satisfy validation
				},
			}

			// Enable the sandbox-runtime feature for SRT tests
			if isSRTEnabled(workflowData) {
				workflowData.Features = map[string]any{
					"sandbox-runtime": true,
				}
			}

			err := validateSandboxConfig(workflowData)
			if tt.expectErr {
				if err == nil {
					t.Errorf("validateSandboxConfig() expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateSandboxConfig() error = %q, should contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateSandboxConfig() unexpected error: %v", err)
				}
			}
		})
	}
}

// TestSandboxTypeCaseSensitivity tests that sandbox types are case-sensitive
func TestSandboxTypeCaseSensitivity(t *testing.T) {
	caseSensitiveTests := []struct {
		name     string
		value    SandboxType
		expected bool
	}{
		{"lowercase awf", "awf", true},
		{"uppercase AWF", "AWF", false},
		{"mixed case Awf", "Awf", false},
		{"lowercase srt", "srt", true},
		{"uppercase SRT", "SRT", false},
		{"mixed case Srt", "Srt", false},
		{"lowercase default", "default", true},
		{"uppercase DEFAULT", "DEFAULT", false},
		{"mixed case Default", "Default", false},
		{"lowercase sandbox-runtime", "sandbox-runtime", true},
		{"uppercase SANDBOX-RUNTIME", "SANDBOX-RUNTIME", false},
		{"mixed case Sandbox-Runtime", "Sandbox-Runtime", false},
	}

	for _, tt := range caseSensitiveTests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := isSupportedSandboxType(tt.value)
			if isValid != tt.expected {
				t.Errorf("Case sensitivity check failed: isSupportedSandboxType(%q) = %v, want %v", tt.value, isValid, tt.expected)
			}
		})
	}
}

// TestSandboxTypeEdgeCases tests edge cases for sandbox type validation
func TestSandboxTypeEdgeCases(t *testing.T) {
	edgeCases := []struct {
		name        string
		value       SandboxType
		expectValid bool
		description string
	}{
		{
			name:        "empty string",
			value:       "",
			expectValid: false,
			description: "Empty string is not a valid sandbox type",
		},
		{
			name:        "whitespace only",
			value:       "   ",
			expectValid: false,
			description: "Whitespace-only is not a valid sandbox type",
		},
		{
			name:        "with leading whitespace",
			value:       " awf",
			expectValid: false,
			description: "Leading whitespace makes the value invalid",
		},
		{
			name:        "with trailing whitespace",
			value:       "awf ",
			expectValid: false,
			description: "Trailing whitespace makes the value invalid",
		},
		{
			name:        "with newline",
			value:       "awf\n",
			expectValid: false,
			description: "Newline in value makes it invalid",
		},
		{
			name:        "with tab",
			value:       "awf\t",
			expectValid: false,
			description: "Tab in value makes it invalid",
		},
	}

	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			isValid := isSupportedSandboxType(tt.value)
			if isValid != tt.expectValid {
				t.Errorf("%s: isSupportedSandboxType(%q) = %v, want %v",
					tt.description, tt.value, isValid, tt.expectValid)
			}
		})
	}
}

// TestValidSandboxTypeConstants tests that all defined SandboxType constants are valid
func TestValidSandboxTypeConstants(t *testing.T) {
	validTypes := []SandboxType{
		SandboxTypeAWF,
		SandboxTypeSRT,
		SandboxTypeDefault,
		SandboxTypeRuntime,
	}

	for _, sandboxType := range validTypes {
		t.Run(string(sandboxType), func(t *testing.T) {
			if !isSupportedSandboxType(sandboxType) {
				t.Errorf("Constant %q should be valid but isSupportedSandboxType() returned false", sandboxType)
			}
		})
	}
}

// TestSandboxMCPGatewayValidation tests that agent sandbox requires MCP gateway to be enabled
func TestSandboxMCPGatewayValidation(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expectErr    bool
		errContains  string
	}{
		{
			name: "sandbox enabled without MCP servers - should error",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Tools: map[string]any{}, // No tools configured
			},
			expectErr:   true,
			errContains: "agent sandbox requires MCP servers to be configured",
		},
		{
			name: "sandbox enabled with MCP servers - should pass",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{}, // GitHub tool uses MCP
				},
			},
			expectErr: false,
		},
		{
			name: "sandbox disabled without MCP servers - should pass",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
				Tools: map[string]any{}, // No tools configured
			},
			expectErr: false,
		},
		{
			name: "sandbox disabled with MCP servers - should pass",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Disabled: true,
					},
				},
				Tools: map[string]any{
					"github": map[string]any{}, // GitHub tool uses MCP
				},
			},
			expectErr: false,
		},
		{
			name: "no sandbox config with MCP servers - should pass (defaults applied)",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				Tools: map[string]any{
					"github": map[string]any{}, // GitHub tool uses MCP
				},
			},
			expectErr: false,
		},
		{
			name: "sandbox with playwright tool - should pass",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Tools: map[string]any{
					"playwright": map[string]any{
						"allowed_domains": []any{"example.com"},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "sandbox with safe-outputs enabled - should pass",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{},
				},
			},
			expectErr: false,
		},
		{
			name: "sandbox with agentic-workflows tool - should pass",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Tools: map[string]any{
					"agentic-workflows": true,
				},
			},
			expectErr: false,
		},
		{
			name: "SRT sandbox without MCP servers - should error",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeSRT,
					},
				},
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				Features: map[string]any{
					"sandbox-runtime": true,
				},
				Tools: map[string]any{}, // No tools configured
			},
			expectErr:   true,
			errContains: "agent sandbox requires MCP servers to be configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSandboxConfig(tt.workflowData)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestSandboxMCPGatewayPortValidation tests that sandbox.mcp.port values are validated
func TestSandboxMCPGatewayPortValidation(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expectErr    bool
		errContains  string
	}{
		{
			name: "valid port - 80",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 80,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid port - 8080",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 8080,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid port - minimum (1)",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 1,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid port - maximum (65535)",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 65535,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid port - zero (default will be applied)",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 0,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid port - negative",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: -1,
					},
				},
			},
			expectErr:   true,
			errContains: "sandbox.mcp.port must be between 1 and 65535",
		},
		{
			name: "invalid port - too high",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 65536,
					},
				},
			},
			expectErr:   true,
			errContains: "sandbox.mcp.port must be between 1 and 65535",
		},
		{
			name: "invalid port - way too high",
			workflowData: &WorkflowData{
				Name: "test-workflow",
				SandboxConfig: &SandboxConfig{
					MCP: &MCPGatewayRuntimeConfig{
						Port: 100000,
					},
				},
			},
			expectErr:   true,
			errContains: "sandbox.mcp.port must be between 1 and 65535",
		},
		{
			name: "no MCP config - should pass",
			workflowData: &WorkflowData{
				Name:          "test-workflow",
				SandboxConfig: &SandboxConfig{},
			},
			expectErr: false,
		},
		{
			name:         "no sandbox config - should pass",
			workflowData: &WorkflowData{Name: "test-workflow"},
			expectErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSandboxConfig(tt.workflowData)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
