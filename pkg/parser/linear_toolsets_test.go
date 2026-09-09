package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLinearToolsets(t *testing.T) {
	t.Run("expands and deduplicates toolsets", func(t *testing.T) {
		tools, err := ParseLinearToolsets([]any{"issues", "projects"})
		require.NoError(t, err)
		assert.Equal(t, []string{
			"get_issue",
			"get_issue_status",
			"get_project",
			"list_issue_labels",
			"list_issue_statuses",
			"list_issues",
			"list_project_labels",
			"list_projects",
		}, tools)
	})

	t.Run("all remains future compatible", func(t *testing.T) {
		tools, err := ParseLinearToolsets("all")
		require.NoError(t, err)
		assert.Equal(t, []string{"*"}, tools)
	})

	for _, value := range []any{
		[]any{},
		[]any{"issues", "issues"},
		[]any{"issues", "unknown"},
		[]any{"issues", true},
		" unknown",
		true,
	} {
		_, err := ParseLinearToolsets(value)
		require.Error(t, err)
	}
}

func TestValidateLinearAllowedForToolsets(t *testing.T) {
	issueTools, err := ParseLinearToolsets("issues")
	require.NoError(t, err)
	require.NoError(t, ValidateLinearAllowedForToolsets([]string{"get_issue", "list_issue*"}, issueTools))
	require.Error(t, ValidateLinearAllowedForToolsets([]string{"list_projects"}, issueTools))
	require.NoError(t, ValidateLinearAllowedForToolsets([]string{"new_future_tool"}, []string{"*"}))
}
