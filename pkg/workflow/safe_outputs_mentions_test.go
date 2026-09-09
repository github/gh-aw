//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
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
				"allowed-collaborators": true,
				"allow-context":         false,
				"allowed":               []any{"bot1", "bot2"},
				"max":                   10,
			},
			expected: &MentionsConfig{
				AllowedCollaborators: boolPtr(true),
				AllowContext:         boolPtr(false),
				Allowed:              []string{"bot1", "bot2"},
				Max:                  strPtr("10"),
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
				Max:     strPtr("5"),
			},
		},
		{
			name: "deprecated allow-team-members alias",
			input: map[string]any{
				"allow-team-members": false,
				"allow-context":      false,
			},
			expected: &MentionsConfig{
				AllowedCollaborators: boolPtr(false),
				AllowContext:         boolPtr(false),
			},
		},
		{
			name: "allowed list with @ prefix - should normalize",
			input: map[string]any{
				"allowed": []any{"@pelikhan", "@bot1"},
			},
			expected: &MentionsConfig{
				Allowed: []string{"pelikhan", "bot1"},
			},
		},
		{
			name: "allowed list with mixed @ prefix - should normalize all",
			input: map[string]any{
				"allowed": []any{"@pelikhan", "bot1", "@user2"},
			},
			expected: &MentionsConfig{
				Allowed: []string{"pelikhan", "bot1", "user2"},
			},
		},
		{
			name: "allowed-teams with org/team format",
			input: map[string]any{
				"allowed-teams": []any{"myorg/my-team", "anotherorg/eng"},
			},
			expected: &MentionsConfig{
				AllowedTeams: []string{"myorg/my-team", "anotherorg/eng"},
			},
		},
		{
			name: "allowed-teams with team-slug only",
			input: map[string]any{
				"allowed-teams": []any{"my-team"},
			},
			expected: &MentionsConfig{
				AllowedTeams: []string{"my-team"},
			},
		},
		{
			name: "allowed-teams with @ prefix - should normalize",
			input: map[string]any{
				"allowed-teams": []any{"@myorg/my-team"},
			},
			expected: &MentionsConfig{
				AllowedTeams: []string{"myorg/my-team"},
			},
		},
		{
			name: "full config with allowed-teams",
			input: map[string]any{
				"allowed-collaborators": true,
				"allow-context":         false,
				"allowed":               []any{"bot1"},
				"allowed-teams":         []any{"myorg/eng"},
				"max":                   10,
			},
			expected: &MentionsConfig{
				AllowedCollaborators: boolPtr(true),
				AllowContext:         boolPtr(false),
				Allowed:              []string{"bot1"},
				AllowedTeams:         []string{"myorg/eng"},
				Max:                  strPtr("10"),
			},
		},
		{
			name: "max as float",
			input: map[string]any{
				"max": 10.5,
			},
			expected: &MentionsConfig{
				Max: strPtr("10"), // should be truncated
			},
		},
		{
			name: "max as expression",
			input: map[string]any{
				"max": "${{ inputs.max-mentions }}",
			},
			expected: &MentionsConfig{
				Max: strPtr("${{ inputs.max-mentions }}"),
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

			// Check AllowedCollaborators
			if tt.expected.AllowedCollaborators != nil {
				if result.AllowedCollaborators == nil {
					t.Errorf("Expected AllowedCollaborators to be %v, got nil", *tt.expected.AllowedCollaborators)
				} else if *result.AllowedCollaborators != *tt.expected.AllowedCollaborators {
					t.Errorf("Expected AllowedCollaborators to be %v, got %v", *tt.expected.AllowedCollaborators, *result.AllowedCollaborators)
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

			// Check AllowedTeams
			if len(tt.expected.AllowedTeams) > 0 {
				if len(result.AllowedTeams) != len(tt.expected.AllowedTeams) {
					t.Errorf("Expected AllowedTeams length %d, got %d", len(tt.expected.AllowedTeams), len(result.AllowedTeams))
				} else {
					for i, expected := range tt.expected.AllowedTeams {
						if result.AllowedTeams[i] != expected {
							t.Errorf("Expected AllowedTeams[%d] to be %q, got %q", i, expected, result.AllowedTeams[i])
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
				AllowedCollaborators: boolPtr(false),
				AllowContext:         boolPtr(true),
				Allowed:              []string{"bot1", "bot2"},
				Max:                  strPtr("20"),
			},
			expected: map[string]any{
				"allowedCollaborators": false,
				"allowContext":         true,
				"allowed":              []string{"bot1", "bot2"},
				"max":                  20,
			},
		},
		{
			name: "allowed-teams propagates to handler config",
			config: &MentionsConfig{
				AllowedTeams: []string{"myorg/eng", "myorg/reviewers"},
			},
			expected: map[string]any{
				"allowedTeams": []string{"myorg/eng", "myorg/reviewers"},
			},
		},
		{
			name: "full config with allowed-teams",
			config: &MentionsConfig{
				AllowedCollaborators: boolPtr(false),
				AllowedTeams:         []string{"myorg/eng"},
				Allowed:              []string{"bot1"},
				Max:                  strPtr("30"),
			},
			expected: map[string]any{
				"allowedCollaborators": false,
				"allowedTeams":         []string{"myorg/eng"},
				"allowed":              []string{"bot1"},
				"max":                  30,
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

			configJSON, err := generateSafeOutputsConfig(data)
			require.NoError(t, err, "generateSafeOutputsConfig should not return an error")
			var parsed map[string]any
			err = json.Unmarshal([]byte(configJSON), &parsed)
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
						"allowed-collaborators": false,
						"allow-context":         true,
						"allowed":               []any{"bot1"},
						"max":                   15,
					},
				},
			},
			expected: &MentionsConfig{
				AllowedCollaborators: boolPtr(false),
				AllowContext:         boolPtr(true),
				Allowed:              []string{"bot1"},
				Max:                  strPtr("15"),
			},
		},
		{
			name: "mentions with @ prefix - should normalize",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": map[string]any{
						"allowed": []any{"@pelikhan"},
					},
				},
			},
			expected: &MentionsConfig{
				Allowed: []string{"pelikhan"},
			},
		},
		{
			name: "mentions with mixed @ prefix - should normalize all",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": map[string]any{
						"allowed": []any{"@user1", "user2", "@user3"},
					},
				},
			},
			expected: &MentionsConfig{
				Allowed: []string{"user1", "user2", "user3"},
			},
		},
		{
			name: "mentions with allowed-teams",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": map[string]any{
						"allowed-teams": []any{"myorg/eng", "myorg/reviewers"},
					},
				},
			},
			expected: &MentionsConfig{
				AllowedTeams: []string{"myorg/eng", "myorg/reviewers"},
			},
		},
		{
			name: "mentions with allowed-teams @ prefix - should normalize",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"mentions": map[string]any{
						"allowed-teams": []any{"@myorg/eng"},
					},
				},
			},
			expected: &MentionsConfig{
				AllowedTeams: []string{"myorg/eng"},
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

			// Check AllowedCollaborators
			if tt.expected.AllowedCollaborators != nil {
				if config.Mentions.AllowedCollaborators == nil {
					t.Errorf("Expected AllowedCollaborators to be %v, got nil", *tt.expected.AllowedCollaborators)
				} else if *config.Mentions.AllowedCollaborators != *tt.expected.AllowedCollaborators {
					t.Errorf("Expected AllowedCollaborators to be %v, got %v", *tt.expected.AllowedCollaborators, *config.Mentions.AllowedCollaborators)
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

			// Check AllowedTeams
			if len(tt.expected.AllowedTeams) > 0 {
				if len(config.Mentions.AllowedTeams) != len(tt.expected.AllowedTeams) {
					t.Errorf("Expected AllowedTeams length %d, got %d", len(tt.expected.AllowedTeams), len(config.Mentions.AllowedTeams))
				} else {
					for i, expected := range tt.expected.AllowedTeams {
						if config.Mentions.AllowedTeams[i] != expected {
							t.Errorf("Expected AllowedTeams[%d] to be %q, got %q", i, expected, config.Mentions.AllowedTeams[i])
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
