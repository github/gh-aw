//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
