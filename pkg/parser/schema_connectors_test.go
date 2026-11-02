//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectorCentralizedSchema(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"on", "tools", "context", "safe-outputs", ""} {
		t.Run(field, func(t *testing.T) {
			config := map[string]any{"definition": "spec.yaml", "connection": "connection.yaml"}
			frontmatter := map[string]any{
				"on":         "workflow_dispatch",
				"connectors": map[string]any{"slack": config},
			}
			if field == "context" {
				config[field] = map[string]any{"thread": map[string]any{"channel": "C123"}}
			} else if field != "" {
				config[field] = []any{"capability"}
			}
			if field == "on" {
				delete(frontmatter, "on")
			}
			require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "proof.md"))
			assert.Equal(t, field == "on", HasConnectorTriggers(frontmatter))
		})
	}
}

func TestConnectorSchemaRejectsInvalidOrScatteredDeclarations(t *testing.T) {
	t.Parallel()
	for _, config := range []map[string]any{
		{"connection": "connection.yaml"},
		{"definition": "spec.yaml"},
		{"definition": "spec.yaml", "connection": "connection.yaml", "tools": []any{}},
		{"definition": "spec.yaml", "connection": "connection.yaml", "tools": []any{"*"}},
		{"definition": "spec.yaml", "connection": "connection.yaml", "tools": []any{"read", "read"}},
		{"definition": "spec.yaml", "connection": "connection.yaml", "safe-outputs": true},
		{"definition": "spec.yaml", "connection": "connection.yaml", "context": []any{"read"}},
		{"definition": "spec.yaml", "connection": "connection.yaml", "token": "inline-secret"},
	} {
		frontmatter := map[string]any{"on": "workflow_dispatch", "connectors": map[string]any{"slack": config}}
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "proof.md"))
	}

	for _, field := range []string{"tools", "safe-outputs", "context"} {
		frontmatter := map[string]any{"on": "workflow_dispatch", field: map[string]any{"connectors": map[string]any{"slack": []any{"read"}}}}
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "proof.md"))
	}
	require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"connectors": map[string]any{"slack": map[string]any{"definition": "spec.yaml", "connection": "connection.yaml", "tools": []any{"read"}}},
	}, "proof.md"), "a tools-only connector is not a trigger")
}

func TestAWConnectorDefinitionsAreImportableButNotTriggers(t *testing.T) {
	t.Parallel()
	config := map[string]any{
		"endpoints":   map[string]any{"api": "https://api.example.com"},
		"credentials": map[string]any{},
		"on":          map[string]any{"issue-created": map[string]any{}},
		"tools":       map[string]any{"get-issue": map[string]any{}},
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{},
		},
	}
	frontmatter := map[string]any{"connectors": map[string]any{"tracker": config}}
	require.NoError(t, ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter, "shared.md"))
	assert.False(t, HasConnectorTriggers(frontmatter), "declaring events must not activate them")
	require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "proof.md"), "main workflow still needs a selected trigger")
	frontmatter["on"] = "workflow_dispatch"
	require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "proof.md"))
	config["tools"] = []any{"get-issue"}
	delete(frontmatter, "on")
	require.ErrorContains(t, ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter, "shared.md"), "main workflow")
}
