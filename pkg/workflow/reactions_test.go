//go:build !integration

package workflow

import (
	"sort"
	"testing"
)

func TestIsValidReaction(t *testing.T) {
	tests := []struct {
		name     string
		reaction string
		expected bool
	}{
		{"+1 is valid", "+1", true},
		{"-1 is valid", "-1", true},
		{"laugh is valid", "laugh", true},
		{"confused is valid", "confused", true},
		{"heart is valid", "heart", true},
		{"hooray is valid", "hooray", true},
		{"rocket is valid", "rocket", true},
		{"eyes is valid", "eyes", true},
		{"none is valid", "none", true},
		{"invalid reaction", "thumbsup", false},
		{"empty string is invalid", "", false},
		{"random string is invalid", "random", false},
		{"case sensitive - uppercase invalid", "HEART", false},
		{"case sensitive - mixed case invalid", "Laugh", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidReaction(tt.reaction)
			if result != tt.expected {
				t.Errorf("isValidReaction(%q) = %v, want %v", tt.reaction, result, tt.expected)
			}
		})
	}
}

func TestGetValidReactions(t *testing.T) {
	reactions := getValidReactions()

	if len(reactions) == 0 {
		t.Error("getValidReactions() returned empty slice")
	}

	expectedReactions := []string{"+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes", "none"}
	if len(reactions) != len(expectedReactions) {
		t.Errorf("getValidReactions() returned %d reactions, want %d", len(reactions), len(expectedReactions))
	}

	// Sort both slices for comparison
	sort.Strings(reactions)
	sort.Strings(expectedReactions)

	for i, expected := range expectedReactions {
		if reactions[i] != expected {
			t.Errorf("getValidReactions()[%d] = %q, want %q", i, reactions[i], expected)
		}
	}

	// Verify all returned reactions are valid
	for _, reaction := range reactions {
		if !isValidReaction(reaction) {
			t.Errorf("getValidReactions() returned invalid reaction: %q", reaction)
		}
	}
}

func TestValidReactionsMap(t *testing.T) {
	// Test that the validReactions map contains expected entries
	expectedCount := 9 // +1, -1, laugh, confused, heart, hooray, rocket, eyes, none
	if len(validReactions) != expectedCount {
		t.Errorf("validReactions map has %d entries, want %d", len(validReactions), expectedCount)
	}

	// Test that all entries in the map have value true
	for reaction, valid := range validReactions {
		if !valid {
			t.Errorf("validReactions[%q] = false, expected true", reaction)
		}
	}
}

func TestParseReactionValue(t *testing.T) {
	tests := []struct {
		name        string
		value       any
		expected    string
		expectError bool
	}{
		// String values
		{"string +1", "+1", "+1", false},
		{"string -1", "-1", "-1", false},
		{"string eyes", "eyes", "eyes", false},
		{"string rocket", "rocket", "rocket", false},

		// Integer values (as parsed from unquoted YAML)
		{"int 1 becomes +1", int(1), "+1", false},
		{"int -1 becomes -1", int(-1), "-1", false},
		{"int64 1 becomes +1", int64(1), "+1", false},
		{"int64 -1 becomes -1", int64(-1), "-1", false},
		{"uint64 1 becomes +1", uint64(1), "+1", false},

		// Invalid integer values
		{"int 2 is invalid", int(2), "", true},
		{"int 0 is invalid", int(0), "", true},
		{"int64 5 is invalid", int64(5), "", true},
		{"uint64 2 is invalid", uint64(2), "", true},

		// Float values (YAML may parse +1/-1 as float)
		{"float 1.0 becomes +1", 1.0, "+1", false},
		{"float -1.0 becomes -1", -1.0, "-1", false},
		{"float 2.0 is invalid", 2.0, "", true},

		// Invalid types
		{"bool is invalid", true, "", true},
		{"nil is invalid", nil, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseReactionValue(tt.value)

			if tt.expectError {
				if err == nil {
					t.Errorf("parseReactionValue(%v) expected error, got result %q", tt.value, result)
				}
			} else {
				if err != nil {
					t.Errorf("parseReactionValue(%v) unexpected error: %v", tt.value, err)
				}
				if result != tt.expected {
					t.Errorf("parseReactionValue(%v) = %q, want %q", tt.value, result, tt.expected)
				}
			}
		})
	}
}

func TestIntToReactionString(t *testing.T) {
	tests := []struct {
		name        string
		value       int64
		expected    string
		expectError bool
	}{
		{"1 becomes +1", 1, "+1", false},
		{"-1 becomes -1", -1, "-1", false},
		{"0 is invalid", 0, "", true},
		{"2 is invalid", 2, "", true},
		{"-2 is invalid", -2, "", true},
		{"100 is invalid", 100, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := intToReactionString(tt.value)

			if tt.expectError {
				if err == nil {
					t.Errorf("intToReactionString(%d) expected error, got result %q", tt.value, result)
				}
			} else {
				if err != nil {
					t.Errorf("intToReactionString(%d) unexpected error: %v", tt.value, err)
				}
				if result != tt.expected {
					t.Errorf("intToReactionString(%d) = %q, want %q", tt.value, result, tt.expected)
				}
			}
		})
	}
}
