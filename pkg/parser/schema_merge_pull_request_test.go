//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMainWorkflowSchema_MergePullRequestProtectedFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
		valid bool
	}{
		{"blocked", "blocked", true},
		{"allowed", "allowed", true},
		{"expression", "${{ inputs.policy }}", true},
		{"exclusions", map[string]any{"exclude": []any{"AGENTS.md", ".agents/", "/.changeset/*.md"}}, true},
		{"object policy", map[string]any{"policy": "allowed"}, true},
		{"object expression", map[string]any{"policy": "${{ inputs.policy }}"}, true},
		{"request-review unsupported", "request-review", false},
		{"fallback unsupported", "fallback-to-issue", false},
		{"invalid policy", map[string]any{"policy": "invalid"}, false},
		{"unknown key", map[string]any{"files": []any{"package.json"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "push",
				"safe-outputs": map[string]any{
					"merge-pull-request": map[string]any{"allowed-base-branches": []any{"agent-work/*"}, "protected-files": tc.value},
				},
			}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/merge-policy.md")
			if (err == nil) != tc.valid {
				t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

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
						"merge-pull-request": map[string]any{"allowed-base-branches": []any{"agent-work/*"}, field: tc.value},
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
					"merge-pull-request": map[string]any{"allowed-base-branches": []any{"agent-work/*"}, "required-check-apps": tc.value},
				},
			}

			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/merge-policy.md")
			if (err == nil) != tc.valid {
				t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestMainWorkflowSchema_MergePullRequestAllowedBaseBranches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		config any
		valid  bool
	}{
		{"null", nil, false},
		{"omitted", map[string]any{}, false},
		{"empty", map[string]any{"allowed-base-branches": []any{}}, false},
		{"wildcard", map[string]any{"allowed-base-branches": []any{"*"}}, false},
		{"globstar", map[string]any{"allowed-base-branches": []any{"**"}}, false},
		{"slash wildcard", map[string]any{"allowed-base-branches": []any{"*/*"}}, false},
		{"slash globstar prefix", map[string]any{"allowed-base-branches": []any{"**/*"}}, false},
		{"slash globstar suffix", map[string]any{"allowed-base-branches": []any{"*/**"}}, false},
		{"slash globstar", map[string]any{"allowed-base-branches": []any{"**/**"}}, false},
		{"mixed wildcard", map[string]any{"allowed-base-branches": []any{"agent-work/*", "*"}}, false},
		{"blank", map[string]any{"allowed-base-branches": []any{" "}}, false},
		{"not array", map[string]any{"allowed-base-branches": "agent-work/*"}, false},
		{"not string", map[string]any{"allowed-base-branches": []any{true}}, false},
		{"expression string", map[string]any{"allowed-base-branches": "${{inputs.base}}"}, false},
		{"expression entry", map[string]any{"allowed-base-branches": []any{"${{ inputs.base }}"}}, false},
		{"compact expression", map[string]any{"allowed-base-branches": []any{"${{inputs.base}}"}}, false},
		{"embedded expression", map[string]any{"allowed-base-branches": []any{"agent-work/${{inputs.base}}"}}, false},
		{"negation", map[string]any{"allowed-base-branches": []any{"agent-work/**", "!agent-work/prod"}}, false},
		{"character class", map[string]any{"allowed-base-branches": []any{"agent-work/[ab]"}}, false},
		{"optional character", map[string]any{"allowed-base-branches": []any{"agent-work/next?"}}, false},
		{"repetition", map[string]any{"allowed-base-branches": []any{"agent-work/next+"}}, false},
		{"brace expansion", map[string]any{"allowed-base-branches": []any{"agent-work/{next,prod}"}}, false},
		{"extglob", map[string]any{"allowed-base-branches": []any{"agent-work/@(next|prod)"}}, false},
		{"escape", map[string]any{"allowed-base-branches": []any{`agent-work/\*`}}, false},
		{"scoped", map[string]any{"allowed-base-branches": []any{"integration", "agent-work/*"}}, true},
		{"nested scope", map[string]any{"allowed-base-branches": []any{"agent-work/**"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontmatter := map[string]any{"on": "push", "safe-outputs": map[string]any{"merge-pull-request": tc.config}}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/merge-policy.md")
			if (err == nil) != tc.valid {
				t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestMergePullRequestAutocompleteIncludesRequiredConfiguration(t *testing.T) {
	type editorEntry struct {
		Children map[string]editorEntry `json:"children"`
	}
	data, err := os.ReadFile("../../docs/public/editor/autocomplete-data.json")
	if err != nil {
		t.Fatal(err)
	}
	var autocomplete struct {
		Root map[string]editorEntry `json:"root"`
	}
	if err := json.Unmarshal(data, &autocomplete); err != nil {
		t.Fatal(err)
	}
	mergePullRequest := autocomplete.Root["safe-outputs"].Children["merge-pull-request"]
	if _, ok := mergePullRequest.Children["allowed-base-branches"]; !ok {
		t.Fatal("merge-pull-request autocomplete is missing allowed-base-branches")
	}
}
