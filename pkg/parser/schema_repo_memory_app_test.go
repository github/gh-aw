//go:build !integration

package parser

import "testing"

func TestRepoMemoryGitHubAppSchema(t *testing.T) {
	for _, tc := range []struct {
		name  string
		app   map[string]any
		valid bool
	}{
		{"client-id", map[string]any{"client-id": "${{ vars.APP_ID }}", "private-key": "${{ secrets.APP_KEY }}"}, true},
		{"legacy", map[string]any{"app-id": "${{ vars.APP_ID }}", "private-key": "${{ secrets.APP_KEY }}"}, true},
		{"missing-key", map[string]any{"client-id": "${{ vars.APP_ID }}"}, false},
		{"missing-id", map[string]any{"private-key": "${{ secrets.APP_KEY }}"}, false},
		{"ignore-missing", map[string]any{"ignore-if-missing": true}, true},
		{"do-not-ignore", map[string]any{"ignore-if-missing": false}, false},
		{"extra-permissions", map[string]any{"client-id": "app", "private-key": "key", "permissions": map[string]any{"issues": "write"}}, false},
	} {
		for _, field := range []string{"github-app", "github-app-read", "github-app-write"} {
			for _, array := range []bool{false, true} {
				t.Run(tc.name+"/"+field, func(t *testing.T) {
					var memory any = map[string]any{"id": "test", field: tc.app}
					if array {
						memory = []any{memory}
					} else {
						memory = map[string]any{field: tc.app}
					}
					err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
						"on": "push", "engine": "copilot", "tools": map[string]any{"repo-memory": memory},
					}, "/tmp/gh-aw/repo-memory-app.md")
					if (err == nil) != tc.valid {
						t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
					}
				})
			}
		}
	}
}
