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
