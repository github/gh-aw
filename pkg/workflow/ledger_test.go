package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseStandaloneLedgerForms(t *testing.T) {
	single, err := parseLedgerToolConfig(map[string]any{
		"schema":        map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
		"max-record-kb": 16,
	})
	require.NoError(t, err)
	require.Len(t, single.Ledgers, 1)
	require.Equal(t, "ledgers/default", single.Ledgers[0].BranchName)
	require.NotNil(t, single.Ledgers[0].Schema)

	multiple, err := parseLedgerToolConfig(map[string]any{
		"findings":    map[string]any{"schema": ".github/schemas/findings.json"},
		"experiments": map[string]any{},
	})
	require.NoError(t, err)
	require.Len(t, multiple.Ledgers, 2)
	require.Equal(t, []string{"experiments", "findings"}, []string{multiple.Ledgers[0].Name, multiple.Ledgers[1].Name})
}

func TestStandaloneLedgerRejectsUnsafeSchemas(t *testing.T) {
	_, err := parseLedgerToolConfig(map[string]any{
		"schema": map[string]any{"properties": map[string]any{"${{ inputs.name }}": map[string]any{}}},
	})
	require.ErrorContains(t, err, "expressions")

	_, err = parseLedgerToolConfig(map[string]any{"schema": "../schema.json"})
	require.ErrorContains(t, err, "repository-relative")
}

func TestStandaloneLedgerPrompt(t *testing.T) {
	config, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{}})
	require.NoError(t, err)
	section := buildLedgerPromptSection(config)
	require.NotNil(t, section)
	require.Contains(t, section.Content, "/tmp/gh-aw/ledgers/findings/ledger.db")
	require.Contains(t, section.Content, "push_ledger_changes")
	require.NotContains(t, strings.ToLower(section.Content), "ledger_append")
}
