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

func TestRequestContinuationRouteSchema(t *testing.T) {
	validate := func(value any) error {
		return ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "push", "safe-outputs": map[string]any{"request-continuation": value},
		}, "continuation.md")
	}
	for _, value := range []map[string]any{
		{"route": "workqueue"},
		{"route": "workqueue", "max-depth": 2, "max": 1},
		{"route": "issue"},
		{"route": "dispatch", "workflow": "summarize"},
		{"route": "dispatch", "workflow": "summarize", "max-depth": 16},
	} {
		require.NoError(t, validate(value), "%v", value)
	}
	for _, value := range []map[string]any{
		{"route": "queue"},
		{"route": "${{ inputs.route }}"},
		{"route": "workqueue", "max": 2},
		{"route": "workqueue", "max": "${{ inputs.max }}"},
		{"route": "workqueue", "max-depth": 0},
		{"route": "workqueue", "max-depth": 17},
		{"route": "workqueue", "max-depth": "${{ inputs.depth }}"},
		{"route": "workqueue", "workflow": "summarize"},
		{"route": "issue", "max-depth": 2},
		{"route": "dispatch"},
		{"route": "dispatch", "workflow": "../summarize"},
		{"route": "dispatch", "workflow": "${{ inputs.workflow }}"},
		{"route": "dispatch", "workflow": "summarize", "target": "main"},
		{"workflow": "summarize"},
		{"max-depth": 2},
	} {
		require.Error(t, validate(value), "%v", value)
	}
}
