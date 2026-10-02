package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLedgerSchemaRejectsRemovedScripts(t *testing.T) {
	for name, ledger := range map[string]any{
		"single replay script":     map[string]any{"replay": map[string]any{"script": "return {}"}},
		"single replay config":     map[string]any{"replay": map[string]any{"config": map[string]any{}}},
		"typed replay":             map[string]any{"type": "log", "replay": map[string]any{}},
		"named replay":             map[string]any{"history": map[string]any{"replay": map[string]any{"script": "return {}"}}},
		"named empty replay":       map[string]any{"history": map[string]any{"replay": map[string]any{}}},
		"single compaction script": map[string]any{"compaction": map[string]any{"script": "return {}"}},
		"named compaction script":  map[string]any{"history": map[string]any{"compaction": map[string]any{"script": "return {}"}}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":    "workflow_dispatch",
				"tools": map[string]any{"ledger": ledger},
			}, "ledger.md"))
		})
	}
}

func TestLedgerSchemaPreservesSupportedDeclarations(t *testing.T) {
	for name, ledger := range map[string]any{
		"default":            map[string]any{},
		"single notes":       map[string]any{"type": "notes"},
		"single typed":       map[string]any{"type": "log", "compaction": false},
		"single policy":      map[string]any{"compaction": map[string]any{"schedule": "weekly"}},
		"single schema":      map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"replay": map[string]any{"type": "string"}}}},
		"schema path":        map[string]any{"schema": ".github/schemas/history.json"},
		"named typed":        map[string]any{"history": map[string]any{"type": "log", "compaction": map[string]any{"min-segments": 4}}},
		"reserved names":     map[string]any{"type": map[string]any{}, "key": map[string]any{}, "replay": map[string]any{}, "compaction": map[string]any{}},
		"named notes":        map[string]any{"notes": map[string]any{"type": "notes"}},
		"named replay typed": map[string]any{"replay": map[string]any{"type": "log"}},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":    "workflow_dispatch",
				"tools": map[string]any{"ledger": ledger},
			}, "ledger.md"))
		})
	}
}

func TestLedgerSchemaRejectsClaimsType(t *testing.T) {
	require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"on":    "workflow_dispatch",
		"tools": map[string]any{"ledger": map[string]any{"type": "claims"}},
	}, "ledger.md"))
}
