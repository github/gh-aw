//go:build !integration

package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractConcurrencyLimitToleratesNonBooleanCancelInProgress(t *testing.T) {
	for _, tt := range []struct {
		name        string
		concurrency map[string]any
		want        int
	}{
		{name: "absent"},
		{name: "enabled", concurrency: map[string]any{"cancel-in-progress": true}, want: 1},
		{name: "disabled", concurrency: map[string]any{"cancel-in-progress": false}},
		{name: "string", concurrency: map[string]any{"cancel-in-progress": "true"}},
		{name: "number", concurrency: map[string]any{"cancel-in-progress": 1}},
		{name: "null", concurrency: map[string]any{"cancel-in-progress": nil}},
		{name: "invalid with group", concurrency: map[string]any{"cancel-in-progress": "true", "group": "workflow"}, want: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractConcurrencyLimit(&workflow.FrontmatterConfig{Concurrency: tt.concurrency}))
		})
	}
}

func TestResolveEffectiveToolsPreservesTolerantFallback(t *testing.T) {
	content := "---\nengine: codex\nimports:\n  - missing.md\n---\n# Agent\n"
	filePath := filepath.Join(t.TempDir(), "workflow.md")
	for _, tt := range []struct {
		name  string
		tools any
		want  map[string]any
	}{
		{name: "absent"},
		{name: "string", tools: "bash"},
		{name: "boolean", tools: false},
		{name: "array", tools: []any{"bash"}},
		{name: "map", tools: map[string]any{"github": true}, want: map[string]any{"github": true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{"engine": "codex", "imports": []any{"missing.md"}, "tools": tt.tools}

			tools, err := resolveEffectiveBashTools(content, frontmatter, "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, tools)
			tools, err = resolveEffectiveTools(content, frontmatter, "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, tools)

			_, err = resolveEffectiveBashTools(content, frontmatter, filePath)
			require.Error(t, err, "missing imports must exercise the top-level fallback")
			got, changed, err := applyBashAllowlistUnsupportedEngineCheck(content, frontmatter, filePath)
			require.NoError(t, err)
			assert.False(t, changed)
			assert.Equal(t, content, got)
		})
	}
}

func TestGetStatusFieldSkipsNonStringIDs(t *testing.T) {
	original := projectCommandRunGHInputContext
	t.Cleanup(func() { projectCommandRunGHInputContext = original })
	for _, invalidID := range []string{"null", "false", "42", `""`, `[]`, `{}`} {
		t.Run(invalidID, func(t *testing.T) {
			for _, hasValidField := range []bool{false, true} {
				t.Run(fmt.Sprintf("valid=%t", hasValidField), func(t *testing.T) {
					fields := fmt.Sprintf(`[{"name":"Status","id":%s}`, invalidID)
					if hasValidField {
						fields += `,{"name":"Status","id":"valid-status"}`
					}
					fields += "]"
					calls := 0
					projectCommandRunGHInputContext = func(_ context.Context, _ string, _ io.Reader, _ ...string) ([]byte, error) {
						calls++
						if calls == 1 {
							return []byte("project-id"), nil
						}
						return []byte(fields), nil
					}

					field, err := getStatusField(t.Context(), projectURLInfo{scope: "users", ownerLogin: "owner", projectNumber: 1}, false)
					if hasValidField {
						require.NoError(t, err)
						assert.Equal(t, "valid-status", field.fieldID)
					} else {
						require.EqualError(t, err, "status field not found in project")
					}
				})
			}
		})
	}
}
