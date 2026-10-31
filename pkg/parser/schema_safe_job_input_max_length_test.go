//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMainWorkflowSchemaSafeJobInputMaxLength(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input       map[string]any
		valid       bool
		errContains []string
	}{
		{
			name:  "string",
			input: map[string]any{"type": "string", "maxLength": 40000},
			valid: true,
		},
		{
			name:  "unset type",
			input: map[string]any{"maxLength": 40000},
			valid: true,
		},
		{
			name:  "one character",
			input: map[string]any{"type": "string", "maxLength": 1},
			valid: true,
		},
		{
			name:  "maximum length",
			input: map[string]any{"type": "string", "maxLength": 1000000},
			valid: true,
		},
		{
			name:        "zero",
			input:       map[string]any{"type": "string", "maxLength": 0},
			errContains: []string{"payload"},
		},
		{
			name:        "over maximum",
			input:       map[string]any{"type": "string", "maxLength": 1000001},
			errContains: []string{"payload"},
		},
		{
			name:        "boolean",
			input:       map[string]any{"type": "boolean", "maxLength": 40000},
			errContains: []string{"payload"},
		},
		{
			name:        "number",
			input:       map[string]any{"type": "number", "maxLength": 40000},
			errContains: []string{"payload"},
		},
		{
			name:        "choice",
			input:       map[string]any{"type": "choice", "options": []any{"one"}, "maxLength": 40000},
			errContains: []string{"payload"},
		},
		{
			name:        "environment",
			input:       map[string]any{"type": "environment", "maxLength": 40000},
			errContains: []string{"payload"},
		},
		{
			name:        "legacy kebab-case name",
			input:       map[string]any{"type": "string", "max-length": 40000},
			errContains: []string{"max-length"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "push",
				"safe-outputs": map[string]any{
					"jobs": map[string]any{
						"publish": map[string]any{
							"inputs": map[string]any{"payload": tc.input},
							"steps":  []any{map[string]any{"run": "echo published"}},
						},
					},
				},
			}, "safe-job-max-length.md")
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, substring := range tc.errContains {
				require.Contains(t, err.Error(), substring)
			}
		})
	}
}
