package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeDiagnosticLanguages(t *testing.T) {
	for _, additional := range []any{"python", []any{"typescript", "go"}} {
		merged, err := MergeTools(map[string]any{"diagnostics": "go"}, map[string]any{"diagnostics": additional})
		require.NoError(t, err)
		require.Contains(t, merged["diagnostics"], "go")
		if additional == "python" {
			require.Contains(t, merged["diagnostics"], "python")
		} else {
			require.Contains(t, merged["diagnostics"], "typescript")
		}
	}
}
