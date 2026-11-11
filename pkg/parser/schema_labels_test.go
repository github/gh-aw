//go:build !integration

package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLabelNameLengthSchema(t *testing.T) {
	fields := []struct {
		handler string
		field   string
	}{
		{"", "labels"},
		{"create-issue", "labels"},
		{"create-discussion", "labels"},
		{"create-pull-request", "labels"},
		{"create-pull-request", "fallback-labels"},
		{"missing-tool", "labels"},
		{"missing-data", "labels"},
		{"report-incomplete", "labels"},
	}
	for _, field := range fields {
		for _, label := range []struct {
			name  string
			value string
			valid bool
		}{
			{"50 ASCII characters", strings.Repeat("a", 50), true},
			{"51 ASCII characters", strings.Repeat("a", 51), false},
			{"54 ASCII characters", strings.Repeat("a", 54), false},
			{"50 Unicode characters", strings.Repeat("\u754c", 50), true},
			{"51 Unicode characters", strings.Repeat("\u754c", 51), false},
		} {
			t.Run(fmt.Sprintf("%s/%s/%s", field.handler, field.field, label.name), func(t *testing.T) {
				frontmatter := map[string]any{"on": "workflow_dispatch"}
				labels := []any{"bug", label.value}
				if field.handler == "" {
					frontmatter[field.field] = labels
				} else {
					frontmatter["safe-outputs"] = map[string]any{
						field.handler: map[string]any{field.field: labels},
					}
				}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "workflow.md")
				if label.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.ErrorContains(t, err, "maxLength:")
					require.ErrorContains(t, err, "want 50")
				}
			})
		}
	}
}

func TestLabelNameLengthSchemaPreservesDynamicAndNonLabelValues(t *testing.T) {
	for _, tt := range []struct {
		name        string
		frontmatter map[string]any
	}{
		{
			"label expression",
			map[string]any{
				"safe-outputs": map[string]any{
					"create-pull-request": map[string]any{
						"labels": "${{ inputs.a_long_input_name_that_resolves_to_short_label_names }}",
					},
				},
			},
		},
		{
			"allowed label glob",
			map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{"allowed-labels": []any{strings.Repeat("[ab]", 20)}},
				},
			},
		},
		{
			"runner label",
			map[string]any{
				"runs-on": []any{"self-hosted", strings.Repeat("a", 51)},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.frontmatter["on"] = "workflow_dispatch"
			require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tt.frontmatter, "workflow.md"))
		})
	}
}
