//go:build !integration

package workflow

import (
	"testing"
)

func TestParseInputDefinition(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		expected *InputDefinition
	}{
		{
			name: "full input definition",
			config: map[string]any{
				"description": "Target deployment environment",
				"required":    true,
				"type":        "choice",
				"default":     "staging",
				"options":     []any{"staging", "production"},
			},
			expected: &InputDefinition{
				Description: "Target deployment environment",
				Required:    true,
				Type:        "choice",
				Default:     "staging",
				Options:     []string{"staging", "production"},
			},
		},
		{
			name: "minimal input definition",
			config: map[string]any{
				"description": "Simple input",
			},
			expected: &InputDefinition{
				Description: "Simple input",
			},
		},
		{
			name: "boolean type with default",
			config: map[string]any{
				"description": "Enable debug mode",
				"type":        "boolean",
				"default":     false,
				"required":    false,
			},
			expected: &InputDefinition{
				Description: "Enable debug mode",
				Type:        "boolean",
				Default:     false,
				Required:    false,
			},
		},
		{
			name: "number type with default",
			config: map[string]any{
				"description": "Number of items to fetch",
				"type":        "number",
				"default":     100,
			},
			expected: &InputDefinition{
				Description: "Number of items to fetch",
				Type:        "number",
				Default:     100,
			},
		},
		{
			name:     "empty config",
			config:   map[string]any{},
			expected: &InputDefinition{},
		},
		{
			name: "string options format",
			config: map[string]any{
				"type":    "choice",
				"options": []string{"option1", "option2"},
			},
			expected: &InputDefinition{
				Type:    "choice",
				Options: []string{"option1", "option2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseInputDefinition(tt.config)

			if result.Description != tt.expected.Description {
				t.Errorf("Description: got %q, want %q", result.Description, tt.expected.Description)
			}
			if result.Required != tt.expected.Required {
				t.Errorf("Required: got %v, want %v", result.Required, tt.expected.Required)
			}
			if result.Type != tt.expected.Type {
				t.Errorf("Type: got %q, want %q", result.Type, tt.expected.Type)
			}

			// Check default value
			// Note: The test directly passes values (not JSON), so comparison should be direct.
			// The special int handling is for the case where int might be represented as float64.
			defaultsMatch := result.Default == tt.expected.Default
			if !defaultsMatch {
				// Handle numeric comparison (JSON/YAML may represent int as float64)
				if expectedVal, ok := tt.expected.Default.(int); ok {
					if resultVal, ok := result.Default.(int); ok && resultVal == expectedVal {
						defaultsMatch = true
					} else if resultVal, ok := result.Default.(float64); ok && int(resultVal) == expectedVal {
						defaultsMatch = true
					}
				}
			}
			if !defaultsMatch {
				t.Errorf("Default: got %v (%T), want %v (%T)", result.Default, result.Default, tt.expected.Default, tt.expected.Default)
			}

			// Check options
			if len(result.Options) != len(tt.expected.Options) {
				t.Errorf("Options length: got %d, want %d", len(result.Options), len(tt.expected.Options))
			} else {
				for i, opt := range result.Options {
					if opt != tt.expected.Options[i] {
						t.Errorf("Options[%d]: got %q, want %q", i, opt, tt.expected.Options[i])
					}
				}
			}
		})
	}
}

func TestParseInputDefinitions(t *testing.T) {
	inputsMap := map[string]any{
		"environment": map[string]any{
			"description": "Target deployment environment",
			"required":    true,
			"type":        "choice",
			"options":     []any{"staging", "production"},
		},
		"force": map[string]any{
			"description": "Force deployment",
			"type":        "boolean",
			"default":     false,
		},
		"count": map[string]any{
			"type":    "number",
			"default": 10,
		},
	}

	result := ParseInputDefinitions(inputsMap)

	if result == nil {
		t.Fatal("Expected result to be non-nil")
	}

	if len(result) != 3 {
		t.Fatalf("Expected 3 inputs, got %d", len(result))
	}

	// Check environment input
	env, exists := result["environment"]
	if !exists {
		t.Fatal("Expected 'environment' input to exist")
	}
	if env.Description != "Target deployment environment" {
		t.Errorf("environment.Description: got %q", env.Description)
	}
	if !env.Required {
		t.Error("environment.Required: expected true")
	}
	if env.Type != "choice" {
		t.Errorf("environment.Type: got %q, want 'choice'", env.Type)
	}
	if len(env.Options) != 2 {
		t.Errorf("environment.Options: got %d options, want 2", len(env.Options))
	}

	// Check force input
	force, exists := result["force"]
	if !exists {
		t.Fatal("Expected 'force' input to exist")
	}
	if force.Type != "boolean" {
		t.Errorf("force.Type: got %q, want 'boolean'", force.Type)
	}
	if force.Default != false {
		t.Errorf("force.Default: got %v, want false", force.Default)
	}

	// Check count input
	count, exists := result["count"]
	if !exists {
		t.Fatal("Expected 'count' input to exist")
	}
	if count.Type != "number" {
		t.Errorf("count.Type: got %q, want 'number'", count.Type)
	}
}

func TestParseInputDefinitionsNil(t *testing.T) {
	result := ParseInputDefinitions(nil)
	if result != nil {
		t.Errorf("Expected nil result for nil input, got %v", result)
	}
}

func TestInputDefinitionGetDefaultAsString(t *testing.T) {
	tests := []struct {
		name     string
		input    *InputDefinition
		expected string
	}{
		{
			name:     "nil default",
			input:    &InputDefinition{Default: nil},
			expected: "",
		},
		{
			name:     "string default",
			input:    &InputDefinition{Default: "staging"},
			expected: "staging",
		},
		{
			name:     "bool true default",
			input:    &InputDefinition{Default: true},
			expected: "true",
		},
		{
			name:     "bool false default",
			input:    &InputDefinition{Default: false},
			expected: "false",
		},
		{
			name:     "int default",
			input:    &InputDefinition{Default: 42},
			expected: "42",
		},
		{
			name:     "int64 default",
			input:    &InputDefinition{Default: int64(100)},
			expected: "100",
		},
		{
			name:     "float64 integer default",
			input:    &InputDefinition{Default: float64(50)},
			expected: "50",
		},
		{
			name:     "float64 decimal default",
			input:    &InputDefinition{Default: float64(3.14)},
			expected: "3.14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.GetDefaultAsString()
			if result != tt.expected {
				t.Errorf("GetDefaultAsString: got %q, want %q", result, tt.expected)
			}
		})
	}
}
