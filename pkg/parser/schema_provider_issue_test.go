package parser

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderIssueSchema(t *testing.T) {
	for _, alias := range []string{"jira-issue", "azure-devops-issue"} {
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
					field := map[string]string{"jira-issue": "project", "azure-devops-issue": "project_id"}[alias]
					settings[field] = "source-scope"
					if alias == "azure-devops-issue" && name == "activities" {
						settings["closed-states"] = []any{"Done"}
					}
					config = settings
				}
				if alias == "azure-devops-issue" && name == "labels" {
					valid = false
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

func TestAzureIssueClosedStatesSchema(t *testing.T) {
	for name, states := range map[string]any{
		"null": nil, "empty": []any{}, "blank": " ", "number": 123, "mixed": []any{"Done", false},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": map[string]any{"gateway-dispatch": map[string]any{"azure-devops-issue": map[string]any{"project_id": "project-uuid", "types": []any{"closed"}, "closed-states": states}}}}, "azure.md")
			require.Error(t, err)
		})
	}
	for _, alias := range []string{"jira-issue"} {
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": map[string]any{"gateway-dispatch": map[string]any{alias: map[string]any{"closed-states": []any{"Done"}}}}}, "provider.md")
		require.Error(t, err)
	}
	for _, activity := range []string{"closed", "reopened"} {
		err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": map[string]any{"gateway-dispatch": map[string]any{"azure-devops-issue": map[string]any{"project_id": "project-uuid", "types": []any{activity}}}}}, "azure.md")
		require.ErrorContains(t, err, "closed-states")
	}
}

func TestProviderIssueRequiredScopesSchema(t *testing.T) {
	for alias, field := range map[string]string{
		"jira-issue": "project", "azure-devops-issue": "project_id",
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
