//go:build !integration

package workflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRepoMemoryLedgerExperimentalWarningCompliance(t *testing.T) {
	const warning = "Using experimental feature: repo-memory ledger"
	tests := []struct {
		name   string
		config *RepoMemoryConfig
		want   bool
	}{
		{
			name: "enabled ledger",
			config: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
				{ID: "events", Ledger: &RepoMemoryLedgerConfig{}},
			}},
			want: true,
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

			if tt.want {
				assert.Contains(t, output.String(), warning)
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, output.String(), warning)
				assert.Zero(t, compiler.GetWarningCount())
			}
		})
	}
}

func TestRepoMemoryLedgerExperimentalWarningComplianceInBatchMode(t *testing.T) {
	const warning = "Using experimental feature: repo-memory ledger"
	compiler := NewCompiler()
	compiler.SetBatchMode(true)
	var output bytes.Buffer
	compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
			{ID: "events", Ledger: &RepoMemoryLedgerConfig{}},
		}},
	}, &output)

	assert.Empty(t, output.String())
	assert.Equal(t, 1, compiler.GetWarningCount())
	assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()[warning])
}
