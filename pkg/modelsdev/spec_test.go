//go:build !integration

package modelsdev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSpec_PublicAPI_NormalizeProvider validates the documented alias and
// case-normalization behavior of NormalizeProvider as described in the
// modelsdev README.md specification.
func TestSpec_PublicAPI_NormalizeProvider(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "github alias", input: "github", expected: "github-copilot"},
		{name: "copilot alias", input: "copilot", expected: "github-copilot"},
		{name: "github_models alias", input: "GITHUB_MODELS", expected: "github-copilot"},
		{name: "other provider lower-cased", input: "OpenAI", expected: "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, NormalizeProvider(tt.input),
				"NormalizeProvider(%q) should follow the documented provider normalization", tt.input)
		})
	}
}

// TestSpec_PublicAPI_NormalizeComparableModelID validates the documented
// comparison normalization of NormalizeComparableModelID as described in the
// modelsdev README.md specification.
func TestSpec_PublicAPI_NormalizeComparableModelID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "trims lower-cases and normalizes separators", input: " GPT_4.1_mini ", expected: "gpt-4-1-mini"},
		{name: "normalizes mixed separators", input: "claude-3_5.sonnet", expected: "claude-3-5-sonnet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, NormalizeComparableModelID(tt.input),
				"NormalizeComparableModelID(%q) should follow the documented model ID normalization", tt.input)
		})
	}
}
