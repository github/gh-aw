package parser

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractLinearBuiltinMCPTool(t *testing.T) {
	required := false
	frontmatter := map[string]any{
		"tools": map[string]any{
			"linear": map[string]any{
				"token":    "${{ secrets.LINEAR_API_KEY }}",
				"toolsets": []any{"issues", "projects"},
				"allowed":  []any{"get_issue", "list_issues"},
				"required": required,
			},
		},
	}

	configs, err := ExtractMCPConfigurations(frontmatter, "linear")
	require.NoError(t, err)
	require.Len(t, configs, 1)
	config := configs[0]
	assert.Equal(t, "linear", config.Name)
	assert.Equal(t, "http", config.Type)
	assert.Equal(t, constants.LinearMCPReadOnlyURL, config.URL)
	assert.Equal(t, "Bearer "+"${{ secrets.LINEAR_API_KEY }}", config.Headers["Authorization"])
	assert.Equal(t, []string{"get_issue", "list_issues"}, config.Allowed)
	require.NotNil(t, config.Required)
	assert.False(t, *config.Required)
}

func TestExtractLinearBuiltinMCPToolDefaultsReadOnly(t *testing.T) {
	frontmatter := map[string]any{
		"tools": map[string]any{
			"linear": nil,
		},
	}

	configs, err := ExtractMCPConfigurations(frontmatter, "")
	require.NoError(t, err)
	require.Len(t, configs, 1)
	assert.Equal(t, constants.LinearMCPReadOnlyURL, configs[0].URL)
	assert.Equal(t, "Bearer "+constants.LinearMCPDefaultTokenExpr, configs[0].Headers["Authorization"])
	assert.Nil(t, configs[0].Required)
}

func TestExtractLinearBuiltinMCPToolRejectsInvalidConfiguration(t *testing.T) {
	for _, linear := range []any{
		map[string]any{"token": "literal-token"},
		map[string]any{"allowed": []any{}},
		map[string]any{"allowed": []any{true}},
		map[string]any{"allowed": []any{" "}},
		map[string]any{"required": "true"},
		map[string]any{"toolsets": "unknown"},
		map[string]any{"toolsets": "issues", "allowed": []any{"list_projects"}},
		map[string]any{"unknown": true},
	} {
		frontmatter := map[string]any{"tools": map[string]any{"linear": linear}}
		_, err := ExtractMCPConfigurations(frontmatter, "linear")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "literal-token")
	}
}

func TestLinearToolSchema(t *testing.T) {
	valid := map[string]any{
		"on":     "workflow_dispatch",
		"engine": "copilot",
		"tools": map[string]any{
			"linear": map[string]any{
				"token":    "${{ secrets.LINEAR_API_KEY }}",
				"toolsets": []any{"issues", "projects"},
				"allowed":  []any{"get_issue"},
				"required": true,
			},
		},
	}
	require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "/tmp/linear-valid.md"))

	for _, linear := range []any{nil, map[string]any{}} {
		frontmatter := map[string]any{
			"on":     "workflow_dispatch",
			"engine": "copilot",
			"tools":  map[string]any{"linear": linear},
		}
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/linear-default-secret.md"))
	}
	for _, toolset := range linearToolsetNames {
		frontmatter := map[string]any{
			"on":     "workflow_dispatch",
			"engine": "copilot",
			"tools":  map[string]any{"linear": map[string]any{"toolsets": toolset}},
		}
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/linear-toolset.md"))
	}

	tests := []any{
		true,
		map[string]any{"token": "literal-token"},
		map[string]any{"token": ""},
		map[string]any{"token": "${{ secrets.LINEAR_API_KEY }}", "read-only": false},
		map[string]any{"token": "${{ secrets.LINEAR_API_KEY }}", "allowed": []any{}},
		map[string]any{"toolsets": "unknown"},
		map[string]any{"token": "${{ secrets.LINEAR_API_KEY }}", "unknown": true},
	}
	for _, linear := range tests {
		frontmatter := map[string]any{
			"on":     "workflow_dispatch",
			"engine": "copilot",
			"tools":  map[string]any{"linear": linear},
		}
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/linear-invalid.md"))
	}
}
