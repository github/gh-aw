//go:build !integration

package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMainWorkflowFrontmatter_HostedWebDomainConstraints(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		domains []any
	}{
		{
			name:    "duplicate allowed domains",
			field:   "allowed",
			domains: []any{"docs.github.com", "docs.github.com"},
		},
		{
			name:    "allowed hostname longer than 253 characters",
			field:   "allowed",
			domains: []any{strings.Repeat("a.", 126) + "com"},
		},
		{
			name:    "duplicate blocked domains",
			field:   "blocked",
			domains: []any{"docs.github.com", "docs.github.com"},
		},
		{
			name:    "blocked hostname longer than 253 characters",
			field:   "blocked",
			domains: []any{strings.Repeat("a.", 126) + "com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":     "workflow_dispatch",
				"engine": "claude",
				"network": map[string]any{
					"hosted-web": map[string]any{tt.field: tt.domains},
				},
			}, "workflow.md")
			require.Error(t, err)
		})
	}
}

func TestValidateMainWorkflowFrontmatter_HostedWebRejectsEnabledField(t *testing.T) {
	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"on":     "workflow_dispatch",
		"engine": "claude",
		"network": map[string]any{
			"hosted-web": map[string]any{
				"enabled": true,
				"allowed": []any{"docs.github.com"},
			},
		},
	}, "workflow.md")
	require.Error(t, err)
}
