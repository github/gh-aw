//go:build integration

package cli

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGradersCommandIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	tests := []struct {
		name       string
		args       []string
		wantErr    bool
		wantOutput []string
	}{
		{
			name:       "graders help",
			args:       []string{"graders", "--help"},
			wantOutput: []string{"Inspect and replay workflow graders", "operational-value", "run"},
		},
		{
			name:       "run grader help",
			args:       []string{"graders", "run", "--help"},
			wantOutput: []string{"Run one grader", "<workflow-id> <grader-id> [run-id]", "--repo"},
		},
		{
			name:       "run grader rejects invalid run ID",
			args:       []string{"graders", "run", "workflow", "loops", "0"},
			wantErr:    true,
			wantOutput: []string{"run ID must be a positive integer"},
		},
		{
			name:       "operational value help",
			args:       []string{"graders", "operational-value", "--help"},
			wantOutput: []string{"Regrade the operational-value observation", "report", "--evidence-at", "--repo", "--json"},
		},
		{
			name:       "operational value report help",
			args:       []string{"graders", "operational-value", "report", "--help"},
			wantOutput: []string{"complete operational-value history", "--until", "--cache-dir", "--refresh", "--output"},
		},
		{
			name:       "operational value requires run ID",
			args:       []string{"graders", "operational-value", "--evidence-at", "2026-08-30T12:00:00.000Z"},
			wantErr:    true,
			wantOutput: []string{"accepts 1 arg(s)"},
		},
		{
			name:       "operational value requires evidence cutoff",
			args:       []string{"graders", "operational-value", "123456789"},
			wantErr:    true,
			wantOutput: []string{"required flag(s) \"evidence-at\" not set"},
		},
		{
			name:       "operational value rejects invalid run ID",
			args:       []string{"graders", "operational-value", "0", "--evidence-at", "2026-08-30T12:00:00.000Z"},
			wantErr:    true,
			wantOutput: []string{"run ID must be a positive integer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(setup.binaryPath, tt.args...)
			output, err := cmd.CombinedOutput()

			if tt.wantErr {
				require.Error(t, err, "command should fail: %s", output)
			} else {
				require.NoError(t, err, "command should succeed: %s", output)
			}

			for _, want := range tt.wantOutput {
				assert.Contains(t, string(output), want)
			}
		})
	}
}
