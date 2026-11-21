//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestContinuationSchema(t *testing.T) {
	for _, value := range []any{nil, true, false, map[string]any{}, map[string]any{"max": 2}, map[string]any{"max": "${{ inputs.max }}"}} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "push", "safe-outputs": map[string]any{"request-continuation": value},
		}, "continuation.md"))
	}
	for _, value := range []any{"yes", map[string]any{"max": 0}, map[string]any{"max": -2}, map[string]any{"max": "2"}, map[string]any{"dispatch": true}, map[string]any{"github-token": "not-supported"}} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "push", "safe-outputs": map[string]any{"request-continuation": value},
		}, "continuation.md"))
	}
}
