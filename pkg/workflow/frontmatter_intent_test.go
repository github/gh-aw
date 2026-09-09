//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractIntent(t *testing.T) {
	compiler := &Compiler{}

	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    string
	}{
		{
			name:        "intent not present",
			frontmatter: map[string]any{"description": "does a thing"},
			expected:    "",
		},
		{
			name:        "intent string is trimmed",
			frontmatter: map[string]any{"intent": "  Reduce manual issue-triage work.  "},
			expected:    "Reduce manual issue-triage work.",
		},
		{
			name:        "intent is not a string",
			frontmatter: map[string]any{"intent": 42},
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, compiler.extractIntent(tt.frontmatter))
		})
	}
}

func TestFrontmatterConfigIntentToMap(t *testing.T) {
	fc := &FrontmatterConfig{
		Description: "Classifies newly opened issues and applies appropriate labels.",
		Intent:      "Reduce manual issue-triage work while keeping classifications evidence-based.",
	}

	result := fc.ToMap()
	assert.Equal(t, fc.Description, result["description"])
	assert.Equal(t, fc.Intent, result["intent"])

	empty := (&FrontmatterConfig{}).ToMap()
	_, hasIntent := empty["intent"]
	assert.False(t, hasIntent, "intent should be omitted when empty")
}
