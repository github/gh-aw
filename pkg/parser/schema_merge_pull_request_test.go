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

func TestMainWorkflowSchema_MergePullRequestCheckApps(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
		valid bool
	}{
		{"map", map[string]any{"ci": 123}, true},
		{"empty-map", map[string]any{}, true},
		{"empty-name", map[string]any{"": 123}, false},
		{"zero", map[string]any{"ci": 0}, false},
		{"negative", map[string]any{"ci": -1}, false},
		{"fraction", map[string]any{"ci": 1.5}, false},
		{"string", map[string]any{"ci": "123"}, false},
		{"unsafe-integer", map[string]any{"ci": 9007199254740992}, false},
		{"list", []any{123}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "push",
				"safe-outputs": map[string]any{
					"merge-pull-request": map[string]any{"required-check-apps": tc.value},
				},
			}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/merge-policy.md")
			if (err == nil) != tc.valid {
				t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}
