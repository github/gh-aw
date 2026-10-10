//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExpandBotNames verifies that any entry from constants.CopilotBotNames is
// expanded to the full set of Copilot identifiers and that other bot names pass
// through unchanged.
func TestExpandBotNames(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty list",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "nil list",
			input:    nil,
			expected: nil,
		},
		{
			name:     "copilot alias expands to all copilot bot names",
			input:    []string{"copilot"},
			expected: []string{"copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent"},
		},
		{
			name:     "@app/copilot-swe-agent alias expands to all copilot bot names",
			input:    []string{"@app/copilot-swe-agent"},
			expected: []string{"copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent"},
		},
		{
			name:     "copilot-swe-agent expands to all copilot bot names",
			input:    []string{"copilot-swe-agent"},
			expected: []string{"copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent"},
		},
		{
			name:     "non-copilot bots pass through unchanged",
			input:    []string{"dependabot[bot]", "renovate[bot]"},
			expected: []string{"dependabot[bot]", "renovate[bot]"},
		},
		{
			name:     "copilot mixed with other bots deduplicates",
			input:    []string{"dependabot[bot]", "copilot", "renovate[bot]"},
			expected: []string{"dependabot[bot]", "copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent", "renovate[bot]"},
		},
		{
			name:     "@app/copilot-swe-agent mixed with other bots deduplicates",
			input:    []string{"dependabot[bot]", "@app/copilot-swe-agent", "renovate[bot]"},
			expected: []string{"dependabot[bot]", "copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent", "renovate[bot]"},
		},
		{
			name:     "copilot and @app/copilot-swe-agent both expand and deduplicate",
			input:    []string{"copilot", "@app/copilot-swe-agent"},
			expected: []string{"copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent"},
		},
		{
			name:     "copilot-swe-agent explicit does not double-expand",
			input:    []string{"copilot", "copilot-swe-agent"},
			expected: []string{"copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent"},
		},
		{
			name:     "Copilot explicit does not double-expand",
			input:    []string{"copilot", "Copilot"},
			expected: []string{"copilot-swe-agent", "Copilot", "copilot", "@app/copilot-swe-agent"},
		},
		{
			name:     "no copilot alias — list unchanged",
			input:    []string{"github-actions[bot]"},
			expected: []string{"github-actions[bot]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandBotNames(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasCopilotBotName(t *testing.T) {
	assert.True(t, hasCopilotBotName([]string{"dependabot[bot]", "copilot-swe-agent", "renovate[bot]"}))
	assert.False(t, hasCopilotBotName([]string{"dependabot[bot]", "renovate[bot]"}))
}
