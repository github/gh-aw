//go:build !integration

package parser

import "testing"

func TestMainWorkflowSchema_MergePullRequestPolicies(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"required-checks", "required-approvers"} {
		for _, tc := range []struct {
			name  string
			value any
			valid bool
		}{
			{"list", []any{"maintainer"}, true},
			{"empty-list", []any{}, true},
			{"empty-entry", []any{""}, false},
			{"duplicate", []any{"ci", "ci"}, false},
			{"not-list", "ci", false},
			{"not-string", []any{true}, false},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				frontmatter := map[string]any{
					"on": "push",
					"safe-outputs": map[string]any{
						"merge-pull-request": map[string]any{field: tc.value},
					},
				}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/merge-policy.md")
				if (err == nil) != tc.valid {
					t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
				}
			})
		}
	}
}
