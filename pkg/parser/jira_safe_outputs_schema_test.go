package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraSafeOutputsSchema(t *testing.T) {
	valid := map[string]any{
		"on": map[string]any{"workflow_dispatch": nil},
		"safe-outputs": map[string]any{
			"jira-create-issue": map[string]any{"max": 1},
			"jira-update-issue": map[string]any{"staged": true},
			"jira-add-comment":  nil,
			"jira-add-label":    map[string]any{"max": "${{ inputs.max }}"},
		},
	}
	require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "/tmp/jira-valid.md"))

	invalid := map[string]any{
		"on": map[string]any{"workflow_dispatch": nil},
		"safe-outputs": map[string]any{
			"jira-create-issue": map[string]any{"target-repo": "owner/repo"},
		},
	}
	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(invalid, "/tmp/jira-invalid.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target-repo")
}
