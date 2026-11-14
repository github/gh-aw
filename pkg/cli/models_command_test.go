//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelsReportOfflineCatalogIsEmpty(t *testing.T) {
	t.Parallel()

	report := buildModelsReport(context.Background(), modelsReportOptions{logsDir: t.TempDir()})
	require.NotNil(t, report.Catalog)
	require.Empty(t, report.Catalog)
	report.Observed = []observedModelRow{{Provider: "copilot", Model: "future-model", Sources: "awf-reflect", Occurrences: 1}}
	data, err := json.Marshal(report)
	require.NoError(t, err)
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.JSONEq(t, "[]", string(parsed["catalog"]))
	var observed []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(parsed["observed"], &observed))
	require.Len(t, observed, 1)
	assert.JSONEq(t, "false", string(observed[0]["in_catalog"]))
	assert.Contains(t, console.RenderStructStdout(report.Observed), "AWF Inventory")
}

func TestCollectObservedModelRowsFromSummaryAndReflect(t *testing.T) {
	t.Parallel()

	logsDir := t.TempDir()
	summaryPath := filepath.Join(logsDir, "summary.json")
	reflectPath := filepath.Join(logsDir, "run-123", "sandbox", "firewall", "awf-reflect.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(reflectPath), 0o755))

	summaryPayload := map[string]any{
		"runs": []any{
			map[string]any{
				"token_usage_summary": map[string]any{
					"by_model": map[string]any{
						"claude-sonnet-4.6": map[string]any{
							"provider": "github-copilot",
							"requests": 7,
						},
					},
				},
			},
		},
	}
	summaryBytes, err := json.Marshal(summaryPayload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(summaryPath, summaryBytes, 0o644))

	reflectPayload := map[string]any{
		"models_fetch_complete": true,
		"endpoints": []any{
			map[string]any{
				"provider": "copilot",
				"models":   []string{"gpt-5.4", "claude-sonnet-4.6"},
			},
		},
	}
	reflectBytes, err := json.Marshal(reflectPayload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(reflectPath, reflectBytes, 0o644))

	_, aliasMap := buildModelAliasRows()
	rows, catalogRows, catalogComplete, warnings := collectObservedModelRows(logsDir, aliasMap)
	require.Empty(t, warnings)
	require.NotEmpty(t, rows)
	require.Len(t, catalogRows, 2)
	require.NotNil(t, catalogComplete)
	assert.True(t, *catalogComplete)

	lookup := make(map[string]observedModelRow, len(rows))
	for _, row := range rows {
		lookup[row.Provider+"/"+row.Model] = row
	}

	sonnet, ok := lookup["github-copilot/claude-sonnet-4.6"]
	require.True(t, ok)
	assert.Contains(t, sonnet.Sources, "summary")
	assert.Contains(t, sonnet.Sources, "awf-reflect")
	assert.GreaterOrEqual(t, sonnet.Occurrences, 8)
	assert.True(t, sonnet.InCatalog)

	gpt, ok := lookup["github-copilot/gpt-5.4"]
	require.True(t, ok)
	assert.Contains(t, gpt.Sources, "awf-reflect")
	assert.True(t, gpt.InCatalog)
}

func TestInferAliasHints(t *testing.T) {
	t.Parallel()

	_, aliasMap := buildModelAliasRows()
	hints := inferAliasHints("github-copilot", "claude-sonnet-4.6", aliasMap)
	assert.NotEmpty(t, hints)
	assert.Contains(t, hints, "sonnet")
}

func TestCollectObservedModelRowsReportsIncompleteAWFInventory(t *testing.T) {
	t.Parallel()

	logsDir := t.TempDir()
	reflectPath := filepath.Join(logsDir, "run-123", "sandbox", "firewall", "awf-reflect.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(reflectPath), 0o755))
	require.NoError(t, os.WriteFile(reflectPath, []byte(`{"models_fetch_complete":false,"endpoints":[]}`), 0o644))

	_, aliasMap := buildModelAliasRows()
	_, _, catalogComplete, warnings := collectObservedModelRows(logsDir, aliasMap)
	require.Empty(t, warnings)
	require.NotNil(t, catalogComplete)
	assert.False(t, *catalogComplete)
}

func TestCollectObservedModelRowsSkipsRunsAlreadyInSummary(t *testing.T) {
	t.Parallel()

	logsDir := t.TempDir()
	usageDir := filepath.Join(logsDir, "run-123", "usage", "agent")
	require.NoError(t, os.MkdirAll(usageDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(usageDir, "token_usage.jsonl"),
		[]byte(`{"provider":"github-copilot","model":"claude-sonnet-4.6","input_tokens":10,"output_tokens":2}`+"\n"),
		0o644,
	))

	summaryPayload := map[string]any{
		"runs": []any{
			map[string]any{
				"run_id": 123,
				"token_usage_summary": map[string]any{
					"by_model": map[string]any{
						"claude-sonnet-4.6": map[string]any{
							"provider": "github-copilot",
							"requests": 1,
						},
					},
				},
			},
		},
	}
	summaryBytes, err := json.Marshal(summaryPayload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "summary.json"), summaryBytes, 0o644))

	_, aliasMap := buildModelAliasRows()
	rows, catalogRows, catalogComplete, warnings := collectObservedModelRows(logsDir, aliasMap)
	require.Empty(t, warnings)
	require.Len(t, rows, 1)
	require.Empty(t, catalogRows)
	assert.Nil(t, catalogComplete)
	assert.Equal(t, "summary", rows[0].Sources)
	assert.Equal(t, 1, rows[0].Occurrences)
}
