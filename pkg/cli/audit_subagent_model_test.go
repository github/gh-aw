package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditSubagentModelUnavailable(t *testing.T) {
	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0755))
	event := `{"type":"subagent.model_unavailable","data":{"agentName":"research","declaredModel":"claude-haiku-4.5","model":"anthropic/claude-opus-4.8"},"provenance":{"component":"agent","phase":"agent"}}`
	content := subagentSessionHeader + event + "\n" + event + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), []byte(content), 0600))

	attribution, found, err := readSessionModelRouting(runDir)
	require.NoError(t, err)
	assert.False(t, found, "availability warnings do not imply workflow.info was found")
	require.Len(t, attribution.UnavailableSubagentModels, 2)
	assert.Nil(t, attribution.modelRouting())

	run := ProcessedRun{Run: WorkflowRun{Conclusion: "success", LogsPath: runDir}}
	findings := generateFindings(run, MetricsData{}, nil)
	require.Len(t, findings, 2, "one deduplicated warning and the existing success finding")
	warning := findings[0]
	assert.Equal(t, AuditFindingSubagentModelUnavailable, warning.Code)
	assert.Equal(t, "tooling", warning.Category)
	assert.Equal(t, "medium", string(warning.Severity))
	assert.Contains(t, warning.Description, "research")
	assert.Contains(t, warning.Description, "claude-haiku-4.5")
	assert.Contains(t, warning.Description, "anthropic/claude-opus-4.8")
	assert.Equal(t, AuditFindingWorkflowSucceeded, findings[1].Code)
	assert.Empty(t, generateSubagentModelFindings(run.TokenUsage))

	requests, actuals, _, err := parseSessionSubagentModels(strings.NewReader(content), true)
	require.NoError(t, err)
	assert.Empty(t, requests, "availability warnings must not create invocations or failures")
	assert.Empty(t, actuals, "fallback metadata is not observed model traffic")
}

func TestAuditSubagentModelUnavailableSources(t *testing.T) {
	event := `{"type":"subagent.model_unavailable","data":{"agentName":"research","declaredModel":"haiku","model":"anthropic/opus"},"provenance":{"component":"agent","phase":"agent"}}`
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		t.Run(relative, func(t *testing.T) {
			runDir := t.TempDir()
			path := filepath.Join(runDir, filepath.FromSlash(relative))
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
			require.NoError(t, os.WriteFile(path, []byte(event), 0600))
			require.Len(t, generateSubagentModelUnavailableFindings(runDir), 1)
			if relative == "usage/aw_session.jsonl" {
				require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_session.jsonl"), []byte("invalid"), 0600))
				require.Len(t, generateSubagentModelUnavailableFindings(runDir), 1, "usage artifact takes precedence")
			}
		})
	}
	assert.Empty(t, generateSubagentModelUnavailableFindings(""))
	assert.Empty(t, generateSubagentModelUnavailableFindings(t.TempDir()))
}

func TestDecodeSubagentModelUnavailableIgnoresInvalidEvidence(t *testing.T) {
	for _, event := range []string{
		`{"type":"subagent.model_unavailable","data":{"agentName":"research","declaredModel":"haiku","model":"anthropic/opus"},"provenance":{"component":"detection","phase":"agent"}}`,
		`{"type":"subagent.model_unavailable","data":{"agentName":"research","declaredModel":"haiku","model":"anthropic/opus"},"provenance":{"component":"agent","phase":"detection"}}`,
		`{"type":"subagent.model_unavailable","data":{"agentName":"research","declaredModel":"haiku"},"provenance":{"component":"agent","phase":"agent"}}`,
		`{"type":"subagent.model_unavailable","data":{"declaredModel":"haiku","model":"anthropic/opus"},"provenance":{"component":"agent","phase":"agent"}}`,
		`{"type":"subagent.model_unavailable","data":{"agentName":"research","model":"anthropic/opus"},"provenance":{"component":"agent","phase":"agent"}}`,
		`{"type":"subagent.model_unavailable","data":[],"provenance":{"component":"agent","phase":"agent"}}`,
		`{"type":"subagent.model_unavailable","data":{"model":42},"provenance":{"component":"agent","phase":"agent"}}`,
	} {
		attribution := &sessionModelRoutingAttribution{}
		require.NoError(t, decodeSessionModelRoutingEvent([]byte(event), 1, attribution))
		assert.Empty(t, attribution.UnavailableSubagentModels)
	}
}
