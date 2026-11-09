//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCommentsMaxLinks(t *testing.T) {
	for _, maxLinks := range []int{1, 50, 75} {
		config := NewCompiler().parseCommentsConfig(map[string]any{
			"add-comment": map[string]any{"max-links": maxLinks},
		})
		require.NotNil(t, config)
		assert.Equal(t, maxLinks, config.MaxLinks)
		handler := commentHandlerRegistry["add_comment"](&SafeOutputsConfig{AddComments: config})
		assert.Equal(t, maxLinks, handler["max_links"])
		assert.Contains(t, addCommentConstraints(config), "Maximum "+strconv.Itoa(maxLinks)+" link(s) in the comment body, excluding generated metadata.")
	}

	config := NewCompiler().parseCommentsConfig(map[string]any{"add-comment": map[string]any{}})
	require.NotNil(t, config)
	handler := commentHandlerRegistry["add_comment"](&SafeOutputsConfig{AddComments: config})
	assert.NotContains(t, handler, "max_links", "omitted limits use the runtime default of 50")
}

func TestAddCommentsCollapseData(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		config := NewCompiler().parseCommentsConfig(map[string]any{
			"add-comment": map[string]any{"collapse-data": enabled},
		})
		require.NotNil(t, config)
		require.NotNil(t, config.CollapseData)
		assert.Equal(t, enabled, *config.CollapseData)
		handler := commentHandlerRegistry["add_comment"](&SafeOutputsConfig{AddComments: config})
		assert.Equal(t, enabled, handler["collapse_data"])
		data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{AddComments: config}}
		generated, err := generateSafeOutputsConfig(data)
		require.NoError(t, err)
		assert.Contains(t, generated, `"collapse_data":`+strconv.FormatBool(enabled))
	}
	config := NewCompiler().parseCommentsConfig(map[string]any{"add-comment": map[string]any{}})
	require.NotNil(t, config)
	assert.Nil(t, config.CollapseData)
	assert.NotContains(t, commentHandlerRegistry["add_comment"](&SafeOutputsConfig{AddComments: config}), "collapse_data")
}

func TestAddCommentsCollapseDataSchema(t *testing.T) {
	for _, value := range []string{"true", "false", `"invalid"`} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "comment.md")
			source := fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
strict: false
safe-outputs:
  data: true
  add-comment:
    collapse-data: %s
---
Publish a comment with structured data.
`, value)
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			data, err := NewCompiler().ParseWorkflowFile(path)
			if value == `"invalid"` {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "collapse-data")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, data.SafeOutputs.AddComments.CollapseData)
			assert.Equal(t, value == "true", *data.SafeOutputs.AddComments.CollapseData)
		})
	}
}
