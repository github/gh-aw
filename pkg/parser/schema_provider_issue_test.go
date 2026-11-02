package parser

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderIssueSchema(t *testing.T) {
	for _, alias := range []string{"jira-issue", "linear-issue"} {
		for name, test := range map[string]struct {
			config any
			valid  bool
		}{
			"scope only":    {map[string]any{}, true},
			"activities":    {map[string]any{"types": []any{"opened", "closed", "labeled"}}, true},
			"labels":        {map[string]any{"labels": []any{"bug"}}, true},
			"filters":       {map[string]any{"filters": map[string]any{"project": "PROJ"}}, false},
			"null":          {nil, false},
			"scalar":        {true, false},
			"unknown key":   {map[string]any{"typo": true}, false},
			"empty types":   {map[string]any{"types": []any{}}, false},
			"native action": {map[string]any{"types": []any{"created"}}, false},
			"misspelling":   {map[string]any{"types": []any{"labelled"}}, false},
			"GitHub repos":  {map[string]any{"repos": []any{"owner/repo"}}, false},
		} {
			t.Run(alias+"/"+name, func(t *testing.T) {
				config := test.config
				valid := test.valid
				if settings, ok := config.(map[string]any); ok {
					settings = maps.Clone(settings)
					field := map[string]string{"jira-issue": "project", "linear-issue": "organization-id"}[alias]
					settings[field] = "source-scope"
					config = settings
				}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
					"on": map[string]any{"gateway-dispatch": map[string]any{alias: config}},
				}, "provider.md")
				if valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestProviderIssueRequiredScopesSchema(t *testing.T) {
	for alias, field := range map[string]string{
		"jira-issue": "project", "linear-issue": "organization-id",
	} {
		t.Run(alias, func(t *testing.T) {
			on := map[string]any{"gateway-dispatch": map[string]any{alias: map[string]any{}}}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": on}, "missing.md")
			require.ErrorContains(t, err, field)
			for _, value := range []any{nil, "", " ", 1, []any{}, []any{"scope", ""}, []any{false}} {
				on["gateway-dispatch"].(map[string]any)[alias] = map[string]any{field: value}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": on}, "invalid.md")
				require.Error(t, err, "scope: %#v", value)
			}
			on["gateway-dispatch"].(map[string]any)[alias] = map[string]any{field: []any{"scope-a", "scope-b"}}
			require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": on}, "scoped.md"))
		})
	}
}

func TestLinearIssueTeamIDSchema(t *testing.T) {
	for _, value := range []any{"team-uuid", []any{"team-a", "team-b"}} {
		config := map[string]any{"organization-id": "organization-uuid", "team-id": value}
		on := map[string]any{"gateway-dispatch": map[string]any{"linear-issue": config}}
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": on}, "linear.md"))
	}
	for _, config := range []map[string]any{
		{"organization-id": "org", "team-id": ""},
		{"organization-id": "org", "team-id": []any{}},
		{"organization_id": "org"},
		{"organization-id": "org", "team_id": "team"},
	} {
		on := map[string]any{"gateway-dispatch": map[string]any{"linear-issue": config}}
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": on}, "invalid-linear.md"))
	}
}
