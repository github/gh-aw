//go:build !integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditSkipIfDecision(t *testing.T) {
	const decision = `{"proposedAttempt":1,"outcome":"deny","questions":[{"question":1,"outcome":"unknown","error":"timeout"},{"question":2,"outcome":"deny"}],"latencyMs":20,"timestamp":123,"avoidedInvocation":true,"providerBody":"private"}`
	root := t.TempDir()
	writeSessionTestFile(t, root, "usage/aw_session.jsonl", sessionTestHeader+
		`{"type":"harness.decision","data":`+decision+`,"provenance":{"component":"harness","phase":"agent","path":"agent/harness_retry.json","index":1}}`+"\n")
	report := readAuditRetrySummary(root)
	require.NotNil(t, report)
	require.Equal(t, 1, report.AvoidedInvocations)
	require.Zero(t, report.AgentRetries)
	require.Empty(t, report.Attempts)
	require.Len(t, report.Decisions, 1)
	require.Equal(t, int64(20), report.Decisions[0].LatencyMs)
	require.Equal(t, "timeout", report.Decisions[0].Questions[0].Error)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	console := captureStderrForGuardPolicyReportTest(func() { renderConsoleRetries(report) })
	require.Contains(t, console, "skip-if-decision: evaluations=1 avoided_invocations=1")
}

func TestAuditSkipIfDecisionSensitiveContentBlocked(t *testing.T) {
	const body = `{"proposedAttempt":1,"outcome":"unknown","questions":[{"question":1,"outcome":"unknown","error":"sensitive_content_blocked"}],"latencyMs":1,"timestamp":1,"avoidedInvocation":false}`
	event := sessionModelRoutingEvent{Type: "harness.decision", Data: json.RawMessage(body)}
	event.Provenance.Component, event.Provenance.Phase = "harness", "agent"
	report := buildAuditRetrySummary([]sessionModelRoutingEvent{event})
	require.NotNil(t, report)
	require.Len(t, report.Decisions, 1)
	require.Equal(t, "sensitive_content_blocked", report.Decisions[0].Questions[0].Error)
	require.Zero(t, report.AvoidedInvocations)
	require.Empty(t, report.Attempts)
}

func TestAuditSkipIfDecisionInvalidEvidence(t *testing.T) {
	const valid = `{"proposedAttempt":1,"outcome":"unknown","questions":[{"question":1,"outcome":"unknown"}],"latencyMs":1,"timestamp":1,"avoidedInvocation":false}`
	for _, body := range []string{
		strings.Replace(valid, `"outcome":"unknown"`, `"outcome":"deny"`, 1),
		strings.Replace(valid, `"latencyMs":1`, `"latencyMs":-1`, 1),
		strings.Replace(valid, `"question":1`, `"question":0`, 1),
		strings.Replace(valid, `"avoidedInvocation":false`, `"avoidedInvocation":true`, 1),
		strings.ReplaceAll(valid, `"outcome":"unknown"`, `"outcome":"invalid"`),
	} {
		event := sessionModelRoutingEvent{Type: "harness.decision", Data: json.RawMessage(body)}
		event.Provenance.Component, event.Provenance.Phase = "harness", "agent"
		require.Nil(t, buildAuditRetrySummary([]sessionModelRoutingEvent{event}))
	}
}
