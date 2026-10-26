//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMainWorkflowSchemaSafeJobOutputs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs any
		valid   bool
	}{
		{"empty", map[string]any{}, true},
		{"status", map[string]any{"status": "${{ steps.publish.outputs.status }}"}, true},
		{"url", map[string]any{"url": "${{ steps.publish.outputs.url }}"}, true},
		{"both", map[string]any{"status": "${{ steps.publish.outputs.status }}", "url": "${{ steps.publish.outputs.url }}"}, true},
		{"unknown", map[string]any{"private": "${{ steps.publish.outputs.private }}"}, false},
		{"numeric-status", map[string]any{"status": 1}, false},
		{"boolean-url", map[string]any{"url": true}, false},
		{"scalar", "status", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "push",
				"safe-outputs": map[string]any{
					"jobs": map[string]any{
						"publish": map[string]any{
							"outputs": tc.outputs,
							"steps":   []any{map[string]any{"run": "echo published"}},
						},
					},
				},
			}, "safe-job-outputs.md")
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
