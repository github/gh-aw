//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogFileWithCodexNativeJSONLFixtures(t *testing.T) {
	for _, test := range []struct {
		name   string
		tokens int
		tools  int
	}{
		{"codex_ci_smoke", 36716, 1},
		{"codex_ci_mcp", 118247, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			metrics, err := parseLogFileWithEngine("../../actions/setup/js/test_data/"+test.name+".jsonl", workflow.NewCodexEngine(), false, false)
			require.NoError(t, err)
			assert.Equal(t, test.tokens, metrics.TokenUsage)
			assert.Equal(t, 1, metrics.Turns)
			assert.Len(t, metrics.ToolCalls, test.tools)
			assert.Zero(t, metrics.EstimatedCost)
		})
	}
}
