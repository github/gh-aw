//go:build !integration

package workflow

import (
	"testing"
)

func TestParseSafeInputs(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   map[string]any
		expectedTools int
		expectedNil   bool
	}{
		{
			name:        "nil frontmatter",
			frontmatter: nil,
			expectedNil: true,
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
			expectedNil: true,
		},
		{
			name: "single javascript tool",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"search-issues": map[string]any{
						"description": "Search for issues",
						"script":      "return 'hello';",
						"inputs": map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "Search query",
								"required":    true,
							},
						},
					},
				},
			},
			expectedTools: 1,
		},
		{
			name: "single shell tool",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"echo-message": map[string]any{
						"description": "Echo a message",
						"run":         "echo $INPUT_MESSAGE",
						"inputs": map[string]any{
							"message": map[string]any{
								"type":        "string",
								"description": "Message to echo",
								"default":     "Hello",
							},
						},
					},
				},
			},
			expectedTools: 1,
		},
		{
			name: "single python tool",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"analyze-data": map[string]any{
						"description": "Analyze data with Python",
						"py":          "import json\nprint(json.dumps({'result': 'success'}))",
						"inputs": map[string]any{
							"data": map[string]any{
								"type":        "string",
								"description": "Data to analyze",
								"required":    true,
							},
						},
					},
				},
			},
			expectedTools: 1,
		},
		{
			name: "single go tool",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"process-data": map[string]any{
						"description": "Process data with Go",
						"go":          "result := map[string]any{\"count\": len(inputs)}\njson.NewEncoder(os.Stdout).Encode(result)",
						"inputs": map[string]any{
							"data": map[string]any{
								"type":        "string",
								"description": "Data to process",
								"required":    true,
							},
						},
					},
				},
			},
			expectedTools: 1,
		},
		{
			name: "multiple tools",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"tool1": map[string]any{
						"description": "Tool 1",
						"script":      "return 1;",
					},
					"tool2": map[string]any{
						"description": "Tool 2",
						"run":         "echo 2",
					},
				},
			},
			expectedTools: 2,
		},
		{
			name: "tool with env secrets",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"api-call": map[string]any{
						"description": "Call API",
						"script":      "return fetch(url);",
						"env": map[string]any{
							"API_KEY": "${{ secrets.API_KEY }}",
						},
					},
				},
			},
			expectedTools: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSafeInputs(tt.frontmatter)

			if tt.expectedNil {
				if result != nil {
					t.Errorf("Expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Error("Expected non-nil result")
				return
			}

			if len(result.Tools) != tt.expectedTools {
				t.Errorf("Expected %d tools, got %d", tt.expectedTools, len(result.Tools))
			}
		})
	}
}

func TestHasSafeInputs(t *testing.T) {
	tests := []struct {
		name     string
		config   *SafeInputsConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name:     "empty tools",
			config:   &SafeInputsConfig{Tools: map[string]*SafeInputToolConfig{}},
			expected: false,
		},
		{
			name: "with tools",
			config: &SafeInputsConfig{
				Tools: map[string]*SafeInputToolConfig{
					"test": {Name: "test", Description: "Test tool"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasSafeInputs(tt.config)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsSafeInputsEnabled(t *testing.T) {
	// Test config with tools
	configWithTools := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"test": {Name: "test", Description: "Test tool"},
		},
	}

	tests := []struct {
		name         string
		config       *SafeInputsConfig
		workflowData *WorkflowData
		expected     bool
	}{
		{
			name:         "nil config - not enabled",
			config:       nil,
			workflowData: nil,
			expected:     false,
		},
		{
			name:         "empty tools - not enabled",
			config:       &SafeInputsConfig{Tools: map[string]*SafeInputToolConfig{}},
			workflowData: nil,
			expected:     false,
		},
		{
			name:         "with tools - enabled by default",
			config:       configWithTools,
			workflowData: nil,
			expected:     true,
		},
		{
			name:   "with tools and feature flag enabled - enabled (backward compat)",
			config: configWithTools,
			workflowData: &WorkflowData{
				Features: map[string]any{"safe-inputs": true},
			},
			expected: true,
		},
		{
			name:   "with tools and feature flag disabled - still enabled (feature flag ignored)",
			config: configWithTools,
			workflowData: &WorkflowData{
				Features: map[string]any{"safe-inputs": false},
			},
			expected: true,
		},
		{
			name:   "with tools and other features - enabled",
			config: configWithTools,
			workflowData: &WorkflowData{
				Features: map[string]any{"other-feature": true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSafeInputsEnabled(tt.config, tt.workflowData)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsSafeInputsEnabledWithEnv(t *testing.T) {
	// Test config with tools
	configWithTools := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"test": {Name: "test", Description: "Test tool"},
		},
	}

	// Safe-inputs are enabled by default when configured, environment variable no longer needed
	t.Run("with tools - enabled regardless of GH_AW_FEATURES", func(t *testing.T) {
		t.Setenv("GH_AW_FEATURES", "safe-inputs")
		result := IsSafeInputsEnabled(configWithTools, nil)
		if !result {
			t.Errorf("Expected true, got false")
		}
	})

	t.Run("with tools and GH_AW_FEATURES=other - still enabled", func(t *testing.T) {
		t.Setenv("GH_AW_FEATURES", "other")
		result := IsSafeInputsEnabled(configWithTools, nil)
		if !result {
			t.Errorf("Expected true, got false")
		}
	})
}

// TestParseSafeInputsAndExtractSafeInputsConfigConsistency verifies that ParseSafeInputs
// and extractSafeInputsConfig produce identical results for the same input.
// This ensures both functions use the shared helper correctly.
func TestParseSafeInputsAndExtractSafeInputsConfigConsistency(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
	}{
		{
			name:        "nil frontmatter",
			frontmatter: nil,
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
		},
		{
			name: "single tool with all fields",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"search-issues": map[string]any{
						"description": "Search for issues",
						"script":      "return 'hello';",
						"inputs": map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "Search query",
								"required":    true,
								"default":     "test",
							},
						},
						"env": map[string]any{
							"API_KEY": "${{ secrets.API_KEY }}",
						},
					},
				},
			},
		},
		{
			name: "multiple tools with different types",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"js-tool": map[string]any{
						"description": "JavaScript tool",
						"script":      "return 1;",
					},
					"shell-tool": map[string]any{
						"description": "Shell tool",
						"run":         "echo hello",
					},
					"python-tool": map[string]any{
						"description": "Python tool",
						"py":          "print('hello')",
					},
				},
			},
		},
		{
			name: "tool with complex inputs",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"complex-tool": map[string]any{
						"description": "Complex tool",
						"script":      "return inputs;",
						"inputs": map[string]any{
							"string-param": map[string]any{
								"type":        "string",
								"description": "A string parameter",
							},
							"number-param": map[string]any{
								"type":        "number",
								"description": "A number parameter",
								"default":     42,
							},
							"bool-param": map[string]any{
								"type":        "boolean",
								"description": "A boolean parameter",
								"required":    true,
							},
						},
					},
				},
			},
		},
	}

	compiler := &Compiler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result1 := ParseSafeInputs(tt.frontmatter)
			result2 := compiler.extractSafeInputsConfig(tt.frontmatter)

			// Both should be nil or both non-nil
			if (result1 == nil) != (result2 == nil) {
				t.Errorf("Inconsistent nil results: ParseSafeInputs=%v, extractSafeInputsConfig=%v", result1 == nil, result2 == nil)
				return
			}

			if result1 == nil {
				return
			}

			// Compare number of tools
			if len(result1.Tools) != len(result2.Tools) {
				t.Errorf("Different number of tools: ParseSafeInputs=%d, extractSafeInputsConfig=%d", len(result1.Tools), len(result2.Tools))
				return
			}

			// Compare each tool
			for toolName, tool1 := range result1.Tools {
				tool2, exists := result2.Tools[toolName]
				if !exists {
					t.Errorf("Tool %s not found in extractSafeInputsConfig result", toolName)
					continue
				}

				if tool1.Name != tool2.Name {
					t.Errorf("Tool %s: Name mismatch: %s vs %s", toolName, tool1.Name, tool2.Name)
				}
				if tool1.Description != tool2.Description {
					t.Errorf("Tool %s: Description mismatch: %s vs %s", toolName, tool1.Description, tool2.Description)
				}
				if tool1.Script != tool2.Script {
					t.Errorf("Tool %s: Script mismatch: %s vs %s", toolName, tool1.Script, tool2.Script)
				}
				if tool1.Run != tool2.Run {
					t.Errorf("Tool %s: Run mismatch: %s vs %s", toolName, tool1.Run, tool2.Run)
				}
				if tool1.Py != tool2.Py {
					t.Errorf("Tool %s: Py mismatch: %s vs %s", toolName, tool1.Py, tool2.Py)
				}

				// Compare inputs
				if len(tool1.Inputs) != len(tool2.Inputs) {
					t.Errorf("Tool %s: Different number of inputs: %d vs %d", toolName, len(tool1.Inputs), len(tool2.Inputs))
					continue
				}

				for inputName, input1 := range tool1.Inputs {
					input2, exists := tool2.Inputs[inputName]
					if !exists {
						t.Errorf("Tool %s: Input %s not found in extractSafeInputsConfig result", toolName, inputName)
						continue
					}

					if input1.Type != input2.Type {
						t.Errorf("Tool %s, Input %s: Type mismatch: %s vs %s", toolName, inputName, input1.Type, input2.Type)
					}
					if input1.Description != input2.Description {
						t.Errorf("Tool %s, Input %s: Description mismatch: %s vs %s", toolName, inputName, input1.Description, input2.Description)
					}
					if input1.Required != input2.Required {
						t.Errorf("Tool %s, Input %s: Required mismatch: %v vs %v", toolName, inputName, input1.Required, input2.Required)
					}
					// Compare defaults (handle nil case)
					if (input1.Default == nil) != (input2.Default == nil) {
						t.Errorf("Tool %s, Input %s: Default nil mismatch: %v vs %v", toolName, inputName, input1.Default, input2.Default)
					}
				}

				// Compare env
				if len(tool1.Env) != len(tool2.Env) {
					t.Errorf("Tool %s: Different number of env vars: %d vs %d", toolName, len(tool1.Env), len(tool2.Env))
					continue
				}

				for envName, envValue1 := range tool1.Env {
					envValue2, exists := tool2.Env[envName]
					if !exists {
						t.Errorf("Tool %s: Env %s not found in extractSafeInputsConfig result", toolName, envName)
						continue
					}
					if envValue1 != envValue2 {
						t.Errorf("Tool %s, Env %s: Value mismatch: %s vs %s", toolName, envName, envValue1, envValue2)
					}
				}
			}
		})
	}
}
