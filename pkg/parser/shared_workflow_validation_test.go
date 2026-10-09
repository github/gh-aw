//go:build !integration

package parser

import "testing"

func TestValidateSharedWorkflowFieldsForbiddenFieldErrors(t *testing.T) {
	const allowedOnFields = "skip-if-match, skip-if-no-match, skip-roles, skip-bots, github-token, github-app"

	tests := []struct {
		name        string
		frontmatter map[string]any
		want        string
	}{
		{
			name:        "single forbidden field",
			frontmatter: map[string]any{"container": "node:lts"},
			want:        "field 'container' cannot be used in shared workflows (only allowed in main workflows with 'on' trigger); shared workflows may use import-safe 'on' fields: " + allowedOnFields,
		},
		{
			name: "multiple forbidden fields are sorted",
			frontmatter: map[string]any{
				"timeout-minutes": 30,
				"name":            "workflow",
				"container":       "node:lts",
			},
			want: "fields [container name timeout-minutes] cannot be used in shared workflows (only allowed in main workflows with 'on' trigger); shared workflows may use import-safe 'on' fields: " + allowedOnFields,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSharedWorkflowFields(tt.frontmatter)
			if err == nil {
				t.Fatal("validateSharedWorkflowFields() returned nil, want an error")
			}
			if got := err.Error(); got != tt.want {
				t.Errorf("validateSharedWorkflowFields() error = %q, want %q", got, tt.want)
			}
		})
	}
}
