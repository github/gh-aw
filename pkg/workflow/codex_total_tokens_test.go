//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestCodexParseLogMetricsLegacyTokenSnapshots(t *testing.T) {
	for _, test := range []struct {
		name   string
		log    string
		tokens int
	}{
		{"multiline final", "tokens used\n15,234", 15234},
		{"grouped inline", "tokens used: 12,345", 12345},
		{"snapshots", "total_tokens: 100\ntotal_tokens: 200", 200},
		{"debug then final", "TokenCount(TokenCountEvent { total_tokens: 100 })\nTokenCount(TokenCountEvent { total_tokens: 200 })\ntokens used\n200", 200},
		{"known zero", "total_tokens: 100\ntokens used\n0", 0},
		{"legacy inline contributions", "[2025-08-13T04:38:03] tokens used: 32,169\n[2025-08-13T04:38:06] tokens used: 28,828\ntokens used: 5,000", 65997},
		{"invalid grouping", "tokens used: 12,34", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.tokens, NewCodexEngine().ParseLogMetrics(test.log, false).TokenUsage)
		})
	}
}
