//go:build !integration

package workflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRepoMemoryLedgerDoesNotEmitExperimentalWarning(t *testing.T) {
	tests := []struct {
		name   string
		config *RepoMemoryConfig
	}{
		{
			name: "enabled ledger",
			config: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
				{ID: "events", Ledger: &RepoMemoryLedgerConfig{}},
			}},
		},
		{
			name: "ledger disabled",
			config: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
				{ID: "plain"},
			}},
		},
		{name: "repo memory disabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{RepoMemoryConfig: tt.config}, &output)

			assert.Empty(t, output.String())
			assert.Zero(t, compiler.GetWarningCount())
		})
	}
}

func TestRepoMemoryLedgerDoesNotTrackExperimentalUsageInBatchMode(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetBatchMode(true)
	var output bytes.Buffer
	compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
			{ID: "events", Ledger: &RepoMemoryLedgerConfig{}},
		}},
	}, &output)

	assert.Empty(t, output.String())
	assert.Zero(t, compiler.GetWarningCount())
	assert.Empty(t, compiler.GetExperimentalFeatureUsage())
}

func TestRepoMemoryLedgerPreservesOtherExperimentalWarnings(t *testing.T) {
	for _, mode := range []struct {
		name  string
		batch bool
	}{
		{name: "normal"},
		{name: "batch", batch: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetBatchMode(mode.batch)
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{
				RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
					{ID: "events", Ledger: &RepoMemoryLedgerConfig{}},
				}},
				LSP: map[string]LSPServerConfig{"typescript": {}},
			}, &output)

			assert.Equal(t, 1, compiler.GetWarningCount())
			assert.Equal(t, 1, compiler.GetExperimentalWarningCount())
			const warning = "Using experimental feature: lsp"
			if mode.batch {
				assert.Empty(t, output.String())
				assert.Equal(t, map[string]int{warning: 1}, compiler.GetExperimentalFeatureUsage())
			} else {
				assert.Contains(t, output.String(), warning)
				assert.NotContains(t, output.String(), "ledger")
				assert.Empty(t, compiler.GetExperimentalFeatureUsage())
			}
		})
	}
}
