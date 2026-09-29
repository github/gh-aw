package workflow

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeLedgerJobConfig(t *testing.T) {
	encoded := encodeLedgerJobConfig(&LedgerToolConfig{Ledgers: []LedgerConfig{{
		Name:         "findings",
		Schema:       map[string]any{"type": "object"},
		SchemaPath:   ".github/schemas/findings.json",
		MaxRecordKB:  8,
		MaxSegmentKB: 64,
		MaxPatchKB:   4,
		BranchName:   "ledgers/findings",
	}}})
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var configs []map[string]any
	require.NoError(t, json.Unmarshal(decoded, &configs))
	require.Equal(t, "findings", configs[0]["name"])
	require.Equal(t, ".github/schemas/findings.json", configs[0]["schemaPath"])
	require.Equal(t, "ledgers/findings", configs[0]["branchName"])
	require.EqualValues(t, 8, configs[0]["maxRecordKB"])
}

func TestPushLedgerChangesJobDownloadsAndPassesLedgerConfiguration(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		LedgerConfig: &LedgerToolConfig{Ledgers: []LedgerConfig{{
			Name:         "findings",
			MaxRecordKB:  8,
			MaxSegmentKB: 64,
			MaxPatchKB:   4,
			BranchName:   "ledgers/findings",
		}}},
	}
	job := compiler.buildPushLedgerChangesJob(data, false)
	steps := strings.Join(job.Steps, "")
	require.Contains(t, steps, "Download agent output artifact")
	require.Contains(t, steps, "safeoutputs.jsonl")
	require.Contains(t, steps, "GH_AW_LEDGER_TRANSACTION_ID: ${{ github.run_id }}-${{ github.run_attempt }}")
	require.Contains(t, steps, "GH_AW_LEDGER_CONFIG_B64:")
}

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
