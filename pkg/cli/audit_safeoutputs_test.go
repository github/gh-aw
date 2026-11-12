//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountBlockedSafeOutputsCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		log         string
		wantBlocked int
	}{
		{
			name: "safeoutputs Bash command denied",
			log: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"safe-output","name":"Bash","input":{"command":"safeoutputs create_discussion '{\"title\":\"test\"}'"}}]}}
{"type":"result","permission_denials":[{"tool_name":"Bash","tool_use_id":"safe-output"}]}`,
			wantBlocked: 1,
		},
		{
			name: "safeoutputs command allowed",
			log: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"safe-output","name":"Bash","input":{"command":"safeoutputs create_discussion {}"}}]}}
{"type":"result","permission_denials":[]}`,
			wantBlocked: 0,
		},
		{
			name: "unrelated Bash command denied",
			log: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"other","name":"Bash","input":{"command":"echo safeoutputs"}}]}}
{"type":"result","permission_denials":[{"tool_name":"Bash","tool_use_id":"other"}]}`,
			wantBlocked: 0,
		},
		{
			name: "similar executable name denied",
			log: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"other","name":"Bash","input":{"command":"safeoutputs-wrapper create_discussion {}"}}]}}
{"type":"result","permission_denials":[{"tool_name":"Bash","tool_use_id":"other"}]}`,
			wantBlocked: 0,
		},
		{
			name: "safeoutputs denied as a different tool",
			log: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"safe-output","name":"Bash","input":{"command":"safeoutputs create_discussion {}"}}]}}
{"type":"result","permission_denials":[{"tool_name":"Read","tool_use_id":"safe-output"}]}`,
			wantBlocked: 0,
		},
		{
			name: "denial does not match safeoutputs call",
			log: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"safe-output","name":"Bash","input":{"command":"safeoutputs create_discussion {}"}}]}}
{"type":"result","permission_denials":[{"tool_name":"Bash","tool_use_id":"different-call"}]}`,
			wantBlocked: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logsPath := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(logsPath, "agent-stdio.log"), []byte(tt.log), 0o600))

			assert.Equal(t, tt.wantBlocked, countBlockedSafeOutputsCommands(logsPath))
		})
	}
}

func TestGenerateSafeOutputsBlockedAuditFindingAndRecommendation(t *testing.T) {
	t.Parallel()

	logsPath := t.TempDir()
	log := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"safe-output","name":"Bash","input":{"command":"safeoutputs create_discussion {}"}}]}}
{"type":"result","permission_denials":[{"tool_name":"Bash","tool_use_id":"safe-output"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(logsPath, "agent-stdio.log"), []byte(log), 0o600))

	processedRun := createTestProcessedRun(func(run *ProcessedRun) {
		run.Run.LogsPath = logsPath
	})
	findings := generateFindings(processedRun, MetricsData{}, nil)
	var blockedFinding *AuditFinding
	for i := range findings {
		if findings[i].Code == AuditFindingSafeOutputsBlocked {
			blockedFinding = &findings[i]
			break
		}
	}
	require.NotNil(t, blockedFinding)
	assert.Contains(t, blockedFinding.Description, "1 safeoutputs Bash invocation")

	recommendations := generateRecommendations(processedRun, MetricsData{}, findings)
	require.Len(t, recommendations, 1)
	assert.Equal(t, "high", recommendations[0].Priority)
	assert.Contains(t, recommendations[0].Example, "`safeoutputs *`")
}
