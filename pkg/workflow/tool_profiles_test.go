//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareToolsForDefaultsExtractsProfiles(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{
			"profile": []any{" go ", "future"},
			"bash":    false,
		},
	}

	require.NoError(t, prepareToolsForDefaults(data))
	assert.Equal(t, []string{"go", "future"}, data.ToolProfiles)
	assert.NotContains(t, data.Tools, "profile")
}

func TestPrepareToolsForDefaultsPreservesProfileMCPServer(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{
			"profile": map[string]any{
				"type": "http",
				"url":  "https://example.invalid/mcp",
			},
		},
	}

	require.NoError(t, prepareToolsForDefaults(data))
	assert.Empty(t, data.ToolProfiles)
	assert.Contains(t, data.Tools, "profile")
	assert.Contains(t, NewTools(data.Tools).Custom, "profile")
	require.NoError(t, ValidateMCPConfigs(data.Tools))
}

func TestExtractToolProfilesRejectsInvalidValues(t *testing.T) {
	for _, value := range []any{
		"",
		[]any{},
		[]any{"go", ""},
		[]any{"go", "go"},
		true,
	} {
		_, err := extractToolProfiles(map[string]any{"profile": value})
		assert.Error(t, err)
	}
}
