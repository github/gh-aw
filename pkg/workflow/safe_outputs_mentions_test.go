//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"
)

func TestParseMentionsConfig_Boolean(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *MentionsConfig
	}{
		{
			name:  "mentions: false",
			input: false,
			expected: &MentionsConfig{
				Enabled: boolPtr(false),
			},
		},
		{
			name:  "mentions: true",
			input: true,
			expected: &MentionsConfig{
				Enabled: boolPtr(true),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMentionsConfig(tt.input)

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			if tt.expected.Enabled != nil {
				if result.Enabled == nil {
					t.Errorf("Expected Enabled to be %v, got nil", *tt.expected.Enabled)
				} else if *result.Enabled != *tt.expected.Enabled {
					t.Errorf("Expected Enabled to be %v, got %v", *tt.expected.Enabled, *result.Enabled)
				}
			}
		})
	}
}

func TestParseMentionsConfig_Object(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected *MentionsConfig
	}{
		{
			name: "full config",
			input: map[string]any{
				"allow-team-members": true,
				"allow-context":      false,
				"allowed":            []any{"bot1", "bot2"},
				"max":                10,
			},
			expected: &MentionsConfig{
				AllowTeamMembers: boolPtr(true),
				AllowContext:     boolPtr(false),
				Allowed:          []string{"bot1", "bot2"},
				Max:              intPtr(10),
			},
		},
		{
			name: "partial config",
			input: map[string]any{
				"allowed": []any{"bot1"},
				"max":     5,
			},
			expected: &MentionsConfig{
				Allowed: []string{"bot1"},
				Max:     intPtr(5),
			},
		},
		{
			name: "max as float",
			input: map[string]any{
				"max": 10.5,
			},
			expected: &MentionsConfig{
				Max: intPtr(10), // should be truncated
			},
		},
		{
			name:  "empty object",
			input: map[string]any{},
			expected: &MentionsConfig{
				Allowed: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMentionsConfig(tt.input)

			if result == nil {
				t.Fatal("Expected non-nil result")
			}

			// Check AllowTeamMembers
			if tt.expected.AllowTeamMembers != nil {
				if result.AllowTeamMembers == nil {
					t.Errorf("Expected AllowTeamMembers to be %v, got nil", *tt.expected.AllowTeamMembers)
				} else if *result.AllowTeamMembers != *tt.expected.AllowTeamMembers {
					t.Errorf("Expected AllowTeamMembers to be %v, got %v", *tt.expected.AllowTeamMembers, *result.AllowTeamMembers)
				}
			}

			// Check AllowContext
			if tt.expected.AllowContext != nil {
				if result.AllowContext == nil {
					t.Errorf("Expected AllowContext to be %v, got nil", *tt.expected.AllowContext)
				} else if *result.AllowContext != *tt.expected.AllowContext {
					t.Errorf("Expected AllowContext to be %v, got %v", *tt.expected.AllowContext, *result.AllowContext)
				}
			}

			// Check Allowed
			if len(tt.expected.Allowed) > 0 {
				if len(result.Allowed) != len(tt.expected.Allowed) {
					t.Errorf("Expected Allowed length %d, got %d", len(tt.expected.Allowed), len(result.Allowed))
				} else {
					for i, expected := range tt.expected.Allowed {
						if result.Allowed[i] != expected {
							t.Errorf("Expected Allowed[%d] to be %q, got %q", i, expected, result.Allowed[i])
						}
					}
				}
			}

			// Check Max
			if tt.expected.Max != nil {
				if result.Max == nil {
					t.Errorf("Expected Max to be %v, got nil", *tt.expected.Max)
				} else if *result.Max != *tt.expected.Max {
					t.Errorf("Expected Max to be %v, got %v", *tt.expected.Max, *result.Max)
				}
			}
		})
	}
}

func TestGenerateSafeOutputsConfig_WithMentions(t *testing.T) {
	tests := []struct {
		name     string
		config   *MentionsConfig
		expected map[string]any
	}{
		{
			name: "mentions enabled false",
			config: &MentionsConfig{
				Enabled: boolPtr(false),
			},
			expected: map[string]any{
				"enabled": false,
			},
		},
		{
			name: "mentions enabled true",
			config: &MentionsConfig{
				Enabled: boolPtr(true),
			},
			expected: map[string]any{
				"enabled": true,
			},
		},
		{
			name: "full mentions config",
			config: &MentionsConfig{
				AllowTeamMembers: boolPtr(false),
				AllowContext:     boolPtr(true),
				Allowed:          []string{"bot1", "bot2"},
				Max:              intPtr(20),
			},
			expected: map[string]any{
				"allowTeamMembers": false,
				"allowContext":     true,
				"allowed":          []string{"bot1", "bot2"},
				"max":              20,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Mentions: tt.config,
				},
			}

			configJSON := generateSafeOutputsConfig(data)
			var parsed map[string]any
			err := json.Unmarshal([]byte(configJSON), &parsed)
			if err != nil {
				t.Fatalf("Failed to parse config JSON: %v", err)
			}

			mentionsMap, ok := parsed["mentions"].(map[string]any)
			if !ok {
				t.Fatal("Expected mentions key in config")
			}

			for key, expectedValue := range tt.expected {
				actualValue, ok := mentionsMap[key]
				if !ok {
					t.Errorf("Expected key %q not found in mentions config", key)
					continue
				}

				// Compare values based on type
				switch expected := expectedValue.(type) {
				case bool:
					if actual, ok := actualValue.(bool); !ok || actual != expected {
						t.Errorf("Expected %q to be %v, got %v", key, expected, actualValue)
					}
				case int:
					// JSON unmarshaling converts numbers to float64
					if actual, ok := actualValue.(float64); !ok || int(actual) != expected {
						t.Errorf("Expected %q to be %v, got %v", key, expected, actualValue)
					}
				case []string:
					actualArray, ok := actualValue.([]any)
					if !ok {
						t.Errorf("Expected %q to be array, got %T", key, actualValue)
						continue
					}
					if len(actualArray) != len(expected) {
						t.Errorf("Expected %q length %d, got %d", key, len(expected), len(actualArray))
						continue
					}
					for i, expectedStr := range expected {
						if actualStr, ok := actualArray[i].(string); !ok || actualStr != expectedStr {
							t.Errorf("Expected %q[%d] to be %q, got %v", key, i, expectedStr, actualArray[i])
						}
					}
				}
			}
		})
	}
}

func TestExtractSafeOutputsConfig_WithMentions(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    *MentionsConfig
	}{
		{
			name: "mentions: false",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": false,
				},
			},
			expected: &MentionsConfig{
				Enabled: boolPtr(false),
			},
		},
		{
			name: "mentions: true",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": true,
				},
			},
			expected: &MentionsConfig{
				Enabled: boolPtr(true),
			},
		},
		{
			name: "mentions object config",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": map[string]any{
						"allow-team-members": false,
						"allow-context":      true,
						"allowed":            []any{"bot1"},
						"max":                15,
					},
				},
			},
			expected: &MentionsConfig{
				AllowTeamMembers: boolPtr(false),
				AllowContext:     boolPtr(true),
				Allowed:          []string{"bot1"},
				Max:              intPtr(15),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{}
			config := c.extractSafeOutputsConfig(tt.frontmatter)

			if config == nil {
				t.Fatal("Expected non-nil config")
			}

			if config.Mentions == nil {
				t.Fatal("Expected non-nil Mentions config")
			}

			// Check Enabled
			if tt.expected.Enabled != nil {
				if config.Mentions.Enabled == nil {
					t.Errorf("Expected Enabled to be %v, got nil", *tt.expected.Enabled)
				} else if *config.Mentions.Enabled != *tt.expected.Enabled {
					t.Errorf("Expected Enabled to be %v, got %v", *tt.expected.Enabled, *config.Mentions.Enabled)
				}
			}

			// Check AllowTeamMembers
			if tt.expected.AllowTeamMembers != nil {
				if config.Mentions.AllowTeamMembers == nil {
					t.Errorf("Expected AllowTeamMembers to be %v, got nil", *tt.expected.AllowTeamMembers)
				} else if *config.Mentions.AllowTeamMembers != *tt.expected.AllowTeamMembers {
					t.Errorf("Expected AllowTeamMembers to be %v, got %v", *tt.expected.AllowTeamMembers, *config.Mentions.AllowTeamMembers)
				}
			}

			// Check AllowContext
			if tt.expected.AllowContext != nil {
				if config.Mentions.AllowContext == nil {
					t.Errorf("Expected AllowContext to be %v, got nil", *tt.expected.AllowContext)
				} else if *config.Mentions.AllowContext != *tt.expected.AllowContext {
					t.Errorf("Expected AllowContext to be %v, got %v", *tt.expected.AllowContext, *config.Mentions.AllowContext)
				}
			}

			// Check Allowed
			if len(tt.expected.Allowed) > 0 {
				if len(config.Mentions.Allowed) != len(tt.expected.Allowed) {
					t.Errorf("Expected Allowed length %d, got %d", len(tt.expected.Allowed), len(config.Mentions.Allowed))
				} else {
					for i, expected := range tt.expected.Allowed {
						if config.Mentions.Allowed[i] != expected {
							t.Errorf("Expected Allowed[%d] to be %q, got %q", i, expected, config.Mentions.Allowed[i])
						}
					}
				}
			}

			// Check Max
			if tt.expected.Max != nil {
				if config.Mentions.Max == nil {
					t.Errorf("Expected Max to be %v, got nil", *tt.expected.Max)
				} else if *config.Mentions.Max != *tt.expected.Max {
					t.Errorf("Expected Max to be %v, got %v", *tt.expected.Max, *config.Mentions.Max)
				}
			}
		})
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
