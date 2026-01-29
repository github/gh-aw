//go:build !integration

package workflow

import (
	"testing"
)

func TestCodexExtractTokenUsageTotalTokensPattern(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name           string
		logLine        string
		expectedTokens int
	}{
		{
			name:           "tokens used format",
			logLine:        "tokens used: 13934",
			expectedTokens: 13934,
		},
		{
			name:           "tokens used with newline",
			logLine:        "tokens used\n15234",
			expectedTokens: 15234,
		},
		{
			name:           "total_tokens format in TokenCountEvent",
			logLine:        "TokenCount(TokenCountEvent { prompt_tokens: 123, completion_tokens: 456, total_tokens: 13281 })",
			expectedTokens: 13281,
		},
		{
			name:           "total_tokens with spaces",
			logLine:        "total_tokens:  42000",
			expectedTokens: 42000,
		},
		{
			name:           "no tokens found",
			logLine:        "this is just a regular log line",
			expectedTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.extractCodexTokenUsage(tt.logLine)
			if result != tt.expectedTokens {
				t.Errorf("extractCodexTokenUsage(%q) = %d, expected %d", tt.logLine, result, tt.expectedTokens)
			}
		})
	}
}
