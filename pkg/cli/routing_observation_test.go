//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const routingObservationSkip = `{"outcome":"skip","reason":"unknown_evidence"}`

func TestModelRoutingReadsRoutingObservation(t *testing.T) {
	// The unified session collector's header and verdict for routed run
	// 39508154330 when its agent job failed after valid output.
	golden, err := os.ReadFile(filepath.Join("testdata", "routing-observation", "aw_session.jsonl"))
	require.NoError(t, err)
	header, verdict, _ := strings.Cut(strings.TrimSpace(string(golden)), "\n")
	require.Contains(t, verdict, routingObservationSkip)
	workflowInfo := `{"type":"workflow.info","data":{"engineId":"copilot","modelRouting":{"status":"selected","wireModel":"gpt-5.6-luna"}},"provenance":{"component":"workflow","phase":"activation","path":"aw_info.json","index":0}}`

	for name, tc := range map[string]struct {
		verdict  string
		expected *RoutingObservation
	}{
		"collector verdict":     {verdict, &RoutingObservation{Outcome: "skip", Reason: "unknown_evidence"}},
		"counted verdict":       {strings.Replace(verdict, routingObservationSkip, `{"outcome":"success"}`, 1), &RoutingObservation{Outcome: "success"}},
		"agent provenance":      {strings.Replace(verdict, `"component":"collector"`, `"component":"agent"`, 1), nil},
		"unknown reason":        {strings.Replace(verdict, `"unknown_evidence"`, `"experiment"`, 1), nil},
		"skip without a reason": {strings.Replace(verdict, routingObservationSkip, `{"outcome":"skip"}`, 1), nil},
		"success with a reason": {strings.Replace(verdict, `"outcome":"skip"`, `"outcome":"success"`, 1), nil},
	} {
		t.Run(name, func(t *testing.T) {
			runDir := t.TempDir()
			writeSessionTestFile(t, runDir, "usage/aw_session.jsonl", header+"\n"+workflowInfo+"\n"+tc.verdict+"\n")
			summary := analyzeModelRouting(runDir)
			require.NotNil(t, summary)
			require.Equal(t, tc.expected, summary.Observation)
		})
	}

	console := captureStderrForGuardPolicyReportTest(func() {
		renderConsoleModelRouting(&ModelRoutingSummary{Status: "selected", Observation: &RoutingObservation{Outcome: "skip", Reason: "unknown_evidence"}})
	})
	require.Contains(t, console, "    observation: outcome=skip reason=unknown_evidence\n")
}
