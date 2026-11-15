//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteGateAuditLogsJSON(t *testing.T) {
	for _, verdict := range []string{"allow", "deny", "partial"} {
		t.Run(verdict, func(t *testing.T) {
			root := t.TempDir()
			event := `{"type":"gate.decision","data":{"verdict":"` + verdict + `","reason":"policy","integrity":"unknown","conclusionCheck":"unknown","counts":{"apply":1,"hold":2,"review":0,"stage":0,"drop":0}},"provenance":{"component":"write_gate","phase":"conclusion","path":"usage/aw_session.jsonl","index":0}}` + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(root, "aw_session.jsonl"), []byte(event), 0o600))
			gate := readWriteGate(root)
			require.NotNil(t, gate)
			expected := `{"verdict":"` + verdict + `","reason":"policy","integrity":"unknown","conclusion_check":"unknown","counts":{"apply":1,"drop":0,"hold":2,"review":0,"stage":0}}`
			content, err := json.Marshal(gate)
			require.NoError(t, err)
			require.JSONEq(t, expected, string(content))
			for _, value := range []any{RunData{WriteGate: gate}, AuditData{WriteGate: gate}} {
				content, err := json.Marshal(value)
				require.NoError(t, err)
				require.Contains(t, string(content), `"write_gate":`+stringMustMarshal(t, gate))
			}
		})
	}
	t.Run("ungated and untrusted stay absent", func(t *testing.T) {
		require.Nil(t, readWriteGate(t.TempDir()))
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "aw_session.jsonl"), []byte(`{"type":"gate.decision","data":{"verdict":"allow"},"provenance":{"component":"agent","phase":"agent"}}`), 0o600))
		require.Nil(t, readWriteGate(root))
		content, err := json.Marshal(RunData{})
		require.NoError(t, err)
		require.NotContains(t, string(content), `"write_gate"`)
	})
	t.Run("invalid trusted evidence is unknown", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "aw_session.jsonl"), []byte(`{"type":"gate.decision","data":{"verdict":"allow"},"provenance":{"component":"write_gate","phase":"conclusion","path":"usage/aw_session.jsonl"}}`), 0o600))
		gate := readWriteGate(root)
		require.Equal(t, "unknown", gate.ConclusionCheck)
		require.NotEmpty(t, gate.Error)
	})
}

func stringMustMarshal(t *testing.T, value any) string {
	t.Helper()
	content, err := json.Marshal(value)
	require.NoError(t, err)
	return string(content)
}

func TestWriteGateOutcomesNoAPIAndSummary(t *testing.T) {
	items, err := decodeLogsOutcomeItems(strings.NewReader(`
{"type":"create_issue","status":"skipped","gate_signal":"gate_denied"}
{"type":"create_pull_request","status":"skipped","gate_decision":"hold"}
{"type":"add_comment","status":"staged","gate_decision":"stage"}
{"type":"create_issue","status":"skipped","gate_decision":"drop"}
`))
	require.NoError(t, err)
	reports := evaluateOutcomes(context.Background(), items, "acme/repo", nil, "github.com")
	require.Len(t, reports, 4)
	for _, report := range reports {
		require.Equal(t, OutcomeStatusSkipped, report.OutcomeStatus)
		require.Equal(t, EvidenceStrong, report.EvidenceStrength)
		require.Empty(t, report.EvalError)
	}
	summary := ComputeOutcomeSummary(reports, nil)
	require.Equal(t, 4, summary.Total)
	require.Equal(t, 0, summary.Pending+summary.Unknown+summary.Errors)
	require.Equal(t, 4, summary.GateDenied+summary.GateHeld+summary.GateStaged+summary.GateDropped)
	var rendered strings.Builder
	require.NoError(t, renderLogsOutcomes(&rendered, LogsData{Outcomes: []OutcomesData{{Summary: summary}}}))
	require.Contains(t, rendered.String(), "Gate skipped: 1 denied, 1 held, 1 staged, 1 dropped")
}
