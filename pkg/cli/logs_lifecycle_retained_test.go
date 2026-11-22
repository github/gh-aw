//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetainedAdoptionColdRestoreWithoutRunArtifacts(t *testing.T) {
	calls := installOutcomeAPIFixture(t, `case "$2" in
repos/acme/repo/issues/42) printf '%s' '{"html_url":"https://github.com/acme/repo/issues/42","state":"closed","state_reason":"completed","closed_at":"2026-11-02T00:00:00Z","closed_by":{"login":"alice","type":"User"}}' ;;
repos/acme/repo/collaborators/alice/permission) printf '%s' '{"permission":"write"}' ;;
repos/acme/repo/issues/42/labels) printf '%s' '[[{"name":"tag"}]]' ;;
repos/acme/repo/pulls/43) printf '%s' '{"state":"open","merged":false}' ;;
repos/acme/repo/pulls/43/reviews) printf '%s' '[[{"id":7,"state":"DISMISSED","submitted_at":"2026-11-01T00:00:00Z"}]]' ;;
*) exit 1 ;;
esac`)
	run := deliveredFixtureRun(t,
		`{"type":"create_issue","number":42,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`+"\n"+
			`{"type":"submit_pull_request_review","number":43,"status":"success","metadata":{"review_id":7},"timestamp":"2026-11-01T00:00:00Z"}`+"\n"+
			`{"type":"add_labels","number":42,"status":"success","labelsBefore":[],"labelsAdded":["tag"],"timestamp":"2026-11-01T00:00:00Z"}`)
	first, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	content, err := os.ReadFile(lifecycleStorePath(run.Run.LogsPath))
	require.NoError(t, err)
	assert.Contains(t, string(content), `"schema_version":7`)
	cold := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cold, "lifecycle.jsonl"), content, 0o600))
	data, err := refreshRetainedAdoption(t.Context(), cold, "acme/repo", 10)
	require.NoError(t, err)
	assert.Equal(t, 3, data.Refreshed)
	assert.Zero(t, data.Deferred)
	require.Len(t, data.Records, 3)
	byID := make(map[string]lifecycle.Record)
	for _, record := range data.Records {
		byID[record.ID] = record
	}
	for _, original := range first.Records {
		record := byID[original.ID]
		assert.Equal(t, original.Identity, record.Identity)
		assert.Equal(t, original.Delivery, record.Delivery)
		switch record.Type {
		case "create_issue":
			assert.Equal(t, "accepted", record.Adoption.State)
			assert.True(t, record.Adoption.HumanVerified)
		case "submit_pull_request_review":
			assert.Equal(t, "rejected", record.Adoption.State)
			assert.Equal(t, "review_dismissed", record.ReversalSignal)
		default:
			assert.Equal(t, "accepted", record.Adoption.State)
			assert.Equal(t, "state_retained", record.Survival.Signal)
			assert.NotNil(t, record.Execution.LabelsBefore)
		}
	}
	for _, call := range outcomeAPICalls(t, calls) {
		assert.NotContains(t, call, "actions/runs", "cold refresh must not fetch run metadata or expired artifacts")
	}
	replay, err := refreshRetainedAdoption(t.Context(), cold, "acme/repo", 10)
	require.NoError(t, err)
	assert.Zero(t, replay.Refreshed, "recent observations are not polled again")
	assert.Equal(t, data.Records, replay.Records)
}

func TestRetainedAdoptionRequiresAnExistingLedger(t *testing.T) {
	_, err := refreshRetainedAdoption(t.Context(), t.TempDir(), "acme/repo", 1)
	require.ErrorContains(t, err, "restore lifecycle.jsonl")
}

func TestRetainedAdoptionRejectsUnrelatedCommandModes(t *testing.T) {
	for _, args := range [][]string{
		{"--retained", "--repo", "acme/repo"},
		{"--retained", "--adoption"},
		{"--retained", "--adoption", "--repo", "acme/repo", "--stdin"},
		{"--retained", "--adoption", "--repo", "acme/repo", "--start-date", "-1d"},
		{"--retained", "--adoption", "--repo", "acme/repo", "--count", "0"},
	} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			cmd := NewLogsCommand()
			cmd.SetContext(t.Context())
			require.NoError(t, cmd.ParseFlags(args))
			require.Error(t, runLogsCommand(cmd, nil))
		})
	}
}

func TestRetainedLifecycleExecutionOmitsUnneededPayload(t *testing.T) {
	execution := retainedLifecycleExecution(CreatedItemReport{
		Type: "submit_pull_request_review", Number: 42,
		Metadata: map[string]any{"review_id": 7, "body": "do not retain"},
	})
	assert.Equal(t, map[string]any{"review_id": 7}, execution.Metadata)
	assert.Nil(t, execution.BeforeState)
}

func TestRetainedAdoptionBoundedProgressAndRepositoryScope(t *testing.T) {
	installOutcomeAPIFixture(t, `printf '{"html_url":"https://github.com/acme/repo/issues/%s","state":"open"}' "${2##*/}"`)
	run := deliveredFixtureRun(t,
		`{"type":"create_issue","number":41,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`+"\n"+
			`{"type":"create_issue","number":42,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
	_, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	dir := filepath.Dir(run.Run.LogsPath)
	first, err := refreshRetainedAdoption(t.Context(), dir, "acme/repo", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Refreshed)
	assert.Equal(t, 1, first.Deferred)
	assert.Equal(t, "partial", first.Coverage)
	second, err := refreshRetainedAdoption(t.Context(), dir, "acme/repo", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, second.Refreshed)
	assert.Zero(t, second.Deferred)
	assert.Equal(t, "complete", second.Coverage)
	for _, record := range second.Records {
		assert.Equal(t, "available", record.Adoption.Coverage)
	}
	other, err := refreshRetainedAdoption(t.Context(), dir, "acme/other", 10)
	require.NoError(t, err)
	assert.Empty(t, other.Records)
	assert.Zero(t, other.Refreshed)
}

func TestRetainedAdoptionProviderFailurePreservesDecision(t *testing.T) {
	installOutcomeAPIFixture(t, "exit 1\n")
	now := time.Now().UTC()
	record, err := lifecycle.Delivered(lifecycle.Identity{
		Host: "github.com", Repository: "acme/repo", Workflow: "test",
		RunID: 42, RunAttempt: 1, InvocationID: "safe-outputs", EvidenceID: "item",
		Type: "create_issue", TargetURL: "https://github.com/acme/repo/issues/42", TargetRepository: "acme/repo",
	}, now.Add(-7*24*time.Hour), now.Add(-6*24*time.Hour), "safe-output-manifest")
	require.NoError(t, err)
	record, err = lifecycle.Observe(record, lifecycle.Observation{
		State: "accepted", Signal: "completed", AcceptanceKind: "explicit_human", HumanVerified: true,
		At: now.Add(-5 * 24 * time.Hour), ObservedAt: now.Add(-4 * 24 * time.Hour),
	})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, writeLifecycleRecords(filepath.Join(dir, "lifecycle.jsonl"), map[string]lifecycle.Record{record.ID: record}))
	data, err := refreshRetainedAdoption(t.Context(), dir, "acme/repo", 1)
	require.NoError(t, err)
	require.Len(t, data.Records, 1)
	assert.Equal(t, "accepted", data.Records[0].Adoption.State)
	assert.Equal(t, record.Adoption.At, data.Records[0].Adoption.At)
	assert.Equal(t, "unavailable", data.Records[0].Adoption.Coverage)
	assert.NotEmpty(t, data.Records[0].Adoption.Error)
	assert.Equal(t, "partial", data.Coverage)
}

func TestRetainedAdoptionExpiredNetworkBudgetDefersWithoutChangingEvidence(t *testing.T) {
	installOutcomeAPIFixture(t, "exit 1\n")
	run := deliveredFixtureRun(t, `{"type":"create_issue","number":42,"status":"success","timestamp":"2026-11-01T00:00:00Z"}`)
	original, err := collectRunLifecycle(t.Context(), run, nil, false, false)
	require.NoError(t, err)
	network, cancel := context.WithCancel(t.Context())
	cancel()
	ctx := context.WithValue(t.Context(), lifecycleNetworkContextKey{}, network)
	data, err := refreshRetainedAdoption(ctx, filepath.Dir(run.Run.LogsPath), "acme/repo", 10)
	require.NoError(t, err)
	assert.Zero(t, data.Refreshed)
	assert.Equal(t, 1, data.Deferred)
	assert.Equal(t, original.Records, data.Records)
}
