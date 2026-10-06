//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateSingleEngineSpecification tests the validateSingleEngineSpecification function
func TestValidateSingleEngineSpecification(t *testing.T) {
	tests := []struct {
		name                string
		mainEngineSetting   string
		includedEnginesJSON []string
		expectedEngine      string
		expectError         bool
		errorMsg            string
	}{
		{
			name:                "no engine specified anywhere",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{},
			expectedEngine:      "",
			expectError:         false,
		},
		{
			name:                "engine only in main workflow",
			mainEngineSetting:   "copilot",
			includedEnginesJSON: []string{},
			expectedEngine:      "copilot",
			expectError:         false,
		},
		{
			name:                "engine only in included file (string format)",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`"claude"`},
			expectedEngine:      "claude",
			expectError:         false,
		},
		{
			name:                "engine only in included file (object format)",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{"id": "codex", "model": "gpt-4"}`},
			expectedEngine:      "codex",
			expectError:         false,
		},
		{
			name:                "multiple engines in main and included",
			mainEngineSetting:   "copilot",
			includedEnginesJSON: []string{`"claude"`},
			expectedEngine:      "",
			expectError:         true,
			errorMsg:            "multiple engine fields found",
		},
		{
			name:                "multiple engines in different included files",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`"copilot"`, `"claude"`},
			expectedEngine:      "",
			expectError:         true,
			errorMsg:            "multiple engine fields found",
		},
		{
			name:                "empty string in main engine setting",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{},
			expectedEngine:      "",
			expectError:         false,
		},
		{
			name:                "empty strings in included engines are ignored",
			mainEngineSetting:   "copilot",
			includedEnginesJSON: []string{"", ""},
			expectedEngine:      "copilot",
			expectError:         false,
		},
		{
			name:                "invalid JSON in included engine",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{invalid json}`},
			expectedEngine:      "",
			expectError:         true,
			errorMsg:            "failed to parse",
		},
		{
			// Model preference (no id) is valid — returns "" so the compiler uses the default
			// engine. The model value flows through separately; see TestCompileWorkflowWithModelOnlyEngine.
			name:                "included engine with model-only (no id) is a preference, not a spec",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{"model": "gpt-4"}`},
			expectedEngine:      "",
			expectError:         false,
		},
		{
			name:                "included engine with model size hint 'small' is allowed without id",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{"model": "small"}`},
			expectedEngine:      "",
			expectError:         false,
		},
		{
			name:                "model-only included engine does not conflict with main engine",
			mainEngineSetting:   "copilot",
			includedEnginesJSON: []string{`{"model": "small"}`},
			expectedEngine:      "copilot",
			expectError:         false,
		},
		{
			name:                "model-only included engine does not conflict with real included engine",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{"id": "copilot"}`, `{"model": "small"}`},
			expectedEngine:      "copilot",
			expectError:         false,
		},
		{
			// An empty object {} has no id/runtime but also no known preference keys;
			// it must NOT be silently skipped — it should be treated as an engine spec
			// and trigger a validation error (missing id field).
			name:                "empty object is not a preference-only engine and triggers error",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{}`},
			expectedEngine:      "",
			expectError:         true,
			errorMsg:            "invalid engine configuration",
		},
		{
			// An object with an unknown key has no id/runtime but must NOT be silently
			// skipped — it should pass through to normal validation.
			name:                "object with unknown key is not a preference-only engine and triggers error",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{"foo": 1}`},
			expectedEngine:      "",
			expectError:         true,
			errorMsg:            "invalid engine configuration",
		},
		{
			name:                "included engine with non-string id",
			mainEngineSetting:   "",
			includedEnginesJSON: []string{`{"id": 123}`},
			expectedEngine:      "",
			expectError:         true,
			errorMsg:            "invalid engine configuration",
		},
		{
			name:                "main engine takes precedence when only non-empty",
			mainEngineSetting:   "codex",
			includedEnginesJSON: []string{""},
			expectedEngine:      "codex",
			expectError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			result, err := compiler.validateSingleEngineSpecification(tt.mainEngineSetting, tt.includedEnginesJSON)

			if tt.expectError && err == nil {
				t.Error("Expected validation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}

			if !tt.expectError && result != tt.expectedEngine {
				t.Errorf("Expected engine %q, got %q", tt.expectedEngine, result)
			}
		})
	}
}

func TestValidateSingleEngineSpecificationMergesRoutingForSameEngine(t *testing.T) {
	routing := `{"id":"copilot","model-routing":{"goal":"cost","mode":"balanced","allowed-models":["gpt-6-sol"]}}`
	engine, err := (&Compiler{}).validateSingleEngineSpecification("copilot", []string{routing})
	require.NoError(t, err)
	require.Equal(t, "copilot", engine)

	_, err = (&Compiler{}).validateSingleEngineSpecification("claude", []string{routing})
	require.ErrorContains(t, err, "multiple engine fields found")

	engine, err = (&Compiler{}).validateSingleEngineSpecification("", []string{routing, routing})
	require.NoError(t, err)
	require.Equal(t, "copilot", engine)
}

// TestValidateSingleEngineSpecificationErrorMessageQuality verifies error messages follow the style guide
func TestValidateSingleEngineSpecificationErrorMessageQuality(t *testing.T) {
	compiler := NewCompiler()

	t.Run("multiple engines error includes example", func(t *testing.T) {
		_, err := compiler.validateSingleEngineSpecification("copilot", []string{`"claude"`})

		if err == nil {
			t.Fatal("Expected validation to fail for multiple engines")
		}

		errorMsg := err.Error()

		// Error should explain what's wrong
		if !strings.Contains(errorMsg, "multiple engine fields found") {
			t.Errorf("Error should explain multiple engines found, got: %s", errorMsg)
		}

		// Error should include count of specifications
		if !strings.Contains(errorMsg, "2 engine specifications") {
			t.Errorf("Error should include count of engine specifications, got: %s", errorMsg)
		}

		// Error should include example
		if !strings.Contains(errorMsg, "Example:") && !strings.Contains(errorMsg, "engine: copilot") {
			t.Errorf("Error should include an example, got: %s", errorMsg)
		}
	})

	t.Run("parse error includes format examples", func(t *testing.T) {
		_, err := compiler.validateSingleEngineSpecification("", []string{`{invalid json}`})

		if err == nil {
			t.Fatal("Expected validation to fail for invalid JSON")
		}

		errorMsg := err.Error()

		// Error should mention parse failure
		if !strings.Contains(errorMsg, "failed to parse") {
			t.Errorf("Error should mention parse failure, got: %s", errorMsg)
		}

		// Error should show both string and object format examples
		if !strings.Contains(errorMsg, "engine: copilot") {
			t.Errorf("Error should include string format example, got: %s", errorMsg)
		}

		if !strings.Contains(errorMsg, "id: copilot") {
			t.Errorf("Error should include object format example, got: %s", errorMsg)
		}
	})

	t.Run("invalid configuration error includes format examples", func(t *testing.T) {
		_, err := compiler.validateSingleEngineSpecification("", []string{`{"id": 123}`})

		if err == nil {
			t.Fatal("Expected validation to fail for configuration with non-string id")
		}

		errorMsg := err.Error()

		// Error should explain the problem
		if !strings.Contains(errorMsg, "invalid engine configuration") {
			t.Errorf("Error should explain invalid configuration, got: %s", errorMsg)
		}

		// Error should mention missing 'id' field
		if !strings.Contains(errorMsg, "id") {
			t.Errorf("Error should mention 'id' field, got: %s", errorMsg)
		}

		// Error should show both string and object format examples
		if !strings.Contains(errorMsg, "engine: copilot") {
			t.Errorf("Error should include string format example, got: %s", errorMsg)
		}

		if !strings.Contains(errorMsg, "id: copilot") {
			t.Errorf("Error should include object format example, got: %s", errorMsg)
		}
	})
}

// TestValidateEngineVersion tests the validateEngineVersion function
func TestValidateEngineVersion(t *testing.T) {
	tests := []struct {
		name        string
		engineCfg   *EngineConfig
		strictMode  bool
		expectWarn  bool
		expectError bool
	}{
		{
			name:        "no engine config",
			engineCfg:   nil,
			expectWarn:  false,
			expectError: false,
		},
		{
			name:        "empty version",
			engineCfg:   &EngineConfig{Version: ""},
			expectWarn:  false,
			expectError: false,
		},
		{
			name:        "pinned version",
			engineCfg:   &EngineConfig{Version: "2.1.92"},
			expectWarn:  false,
			expectError: false,
		},
		{
			name:        "latest version non-strict",
			engineCfg:   &EngineConfig{Version: "latest"},
			strictMode:  false,
			expectWarn:  true,
			expectError: false,
		},
		{
			name:        "LATEST uppercase non-strict",
			engineCfg:   &EngineConfig{Version: "LATEST"},
			strictMode:  false,
			expectWarn:  true,
			expectError: false,
		},
		{
			name:        "latest version strict mode",
			engineCfg:   &EngineConfig{Version: "latest"},
			strictMode:  true,
			expectWarn:  true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = tt.strictMode

			workflowData := &WorkflowData{
				EngineConfig: tt.engineCfg,
			}

			err := compiler.validateEngineVersion(workflowData)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if !strings.Contains(err.Error(), "strict mode") {
					t.Errorf("Expected strict mode error, got: %s", err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if tt.expectWarn && compiler.warningCount == 0 {
					t.Error("Expected warning count to increment")
				}
			}
		})
	}
}

// TestValidateEngineMCPSessionTimeout tests the validateEngineMCPSessionTimeout function.
func TestValidateEngineMCPSessionTimeout(t *testing.T) {
	tests := []struct {
		name        string
		workflow    *WorkflowData
		expectError bool
		errorSubstr string
	}{
		{
			name:        "nil workflow data",
			workflow:    nil,
			expectError: false,
		},
		{
			name:        "nil engine config",
			workflow:    &WorkflowData{},
			expectError: false,
		},
		{
			name: "empty session timeout - no error",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expectError: false,
		},
		{
			name: "valid duration 4h",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "4h"},
			},
			expectError: false,
		},
		{
			name: "valid duration 30m",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "30m"},
			},
			expectError: false,
		},
		{
			name: "valid duration 12h",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "12h"},
			},
			expectError: false,
		},
		{
			name: "valid duration 5m (minimum)",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "5m"},
			},
			expectError: false,
		},
		{
			name: "invalid duration string",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "2hours"},
			},
			expectError: true,
			errorSubstr: "invalid duration",
		},
		{
			name: "too short - 4m",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "4m"},
			},
			expectError: true,
			errorSubstr: "too short",
		},
		{
			name: "valid duration 24h (no upper bound)",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "24h"},
			},
			expectError: false,
		},
		{
			name: "plain integer - not valid Go duration",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPSessionTimeout: "3600"},
			},
			expectError: true,
			errorSubstr: "invalid duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateEngineMCPSessionTimeout(tt.workflow)

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				if tt.errorSubstr != "" {
					require.ErrorContains(t, err, tt.errorSubstr, "Expected error substring mismatch")
				}
				return
			}

			assert.NoError(t, err, "Expected session-timeout validation to pass")
		})
	}
}

// TestValidateEngineMCPToolTimeout tests the validateEngineMCPToolTimeout function.
func TestValidateEngineMCPToolTimeout(t *testing.T) {
	tests := []struct {
		name        string
		workflow    *WorkflowData
		expectError bool
		errorSubstr string
	}{
		{
			name:        "nil workflow data",
			workflow:    nil,
			expectError: false,
		},
		{
			name:        "nil engine config",
			workflow:    &WorkflowData{},
			expectError: false,
		},
		{
			name: "empty tool timeout - no error",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expectError: false,
		},
		{
			name: "valid duration 2m",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "2m"},
			},
			expectError: false,
		},
		{
			name: "valid duration 30s",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "30s"},
			},
			expectError: false,
		},
		{
			name: "valid duration 10s (minimum)",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "10s"},
			},
			expectError: false,
		},
		{
			name: "valid duration 600s (maximum)",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "600s"},
			},
			expectError: false,
		},
		{
			name: "valid duration 10m (maximum)",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "10m"},
			},
			expectError: false,
		},
		{
			name: "invalid duration string",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "2hours"},
			},
			expectError: true,
			errorSubstr: "invalid duration",
		},
		{
			name: "too short - 5s",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "5s"},
			},
			expectError: true,
			errorSubstr: "too short",
		},
		{
			name: "too long - 601s",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "601s"},
			},
			expectError: true,
			errorSubstr: "exceeds the maximum",
		},
		{
			name: "too long - 11m",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "11m"},
			},
			expectError: true,
			errorSubstr: "exceeds the maximum",
		},
		{
			name: "plain integer - not valid Go duration",
			workflow: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot", MCPToolTimeout: "120"},
			},
			expectError: true,
			errorSubstr: "invalid duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateEngineMCPToolTimeout(tt.workflow)

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				if tt.errorSubstr != "" {
					require.ErrorContains(t, err, tt.errorSubstr, "Expected error substring mismatch")
				}
				return
			}

			assert.NoError(t, err, "Expected tool-timeout validation to pass")
		})
	}
}
