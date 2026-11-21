//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueCapturedDiagnostics(t *testing.T) {
	t.Parallel()
	envelopeValue, fixture := logsWQWorkerFixture(t)
	data, err := json.Marshal(envelopeValue)
	require.NoError(t, err)
	envelope, state, err := replayCurrentWorkQueueSnapshot(data)
	require.NoError(t, err)
	before, err := workqueue.SerializeProjection(state)
	require.NoError(t, err)
	snapshot, err := parseWorkQueueSnapshot(data)
	require.NoError(t, err)
	diagnostics := snapshot.Current.Diagnostics
	require.NotNil(t, diagnostics)
	assert.Equal(t, "captured_snapshot", diagnostics.Status)
	assert.Equal(t, workQueueTestRepository, diagnostics.Repository)
	assert.Empty(t, diagnostics.Branch, "artifacts do not establish a queue branch name")
	assert.Equal(t, snapshot.Current.Tip, diagnostics.Tip)
	assert.Equal(t, 2, diagnostics.Work)
	assert.Equal(t, 1, diagnostics.Available)
	assert.Equal(t, 1, diagnostics.Claimed)
	assert.Equal(t, map[string]int{"open": 1, "completed": 0, "cancelled": 0}, diagnostics.ClaimsByState)
	assert.Equal(t, map[string]int{"reserved": 0, "started": 0, "bound": 1, "closed": 0}, diagnostics.DispatchesByState)
	require.Len(t, diagnostics.Pools, 1)
	pool := diagnostics.Pools[0]
	assert.Equal(t, "default", pool.Pool)
	assert.Equal(t, 1, pool.OpenDispatches)
	assert.Equal(t, 1, pool.Eligible)
	member := fixture.assignment.Claims[0]
	require.Len(t, diagnostics.AssignedClaims, 1)
	assigned := diagnostics.AssignedClaims[0]
	assert.Equal(t, member.ClaimID, assigned.Claim.ClaimID)
	assert.Equal(t, envelope.Worker.Claims[0].ClaimID, assigned.Claim.ClaimID)
	assert.Equal(t, "open", assigned.Claim.State)
	assert.True(t, assigned.Claim.Current)
	assert.Equal(t, fixture.dispatchID, assigned.Claim.DispatchID)
	assert.Equal(t, member.WorkID, assigned.Work.WorkID)
	assert.Equal(t, "claimed", assigned.Work.State)
	assert.Zero(t, assigned.Work.Attempts)
	serialized, err := json.Marshal(snapshot)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "private-task-content")
	capturedWorkQueueDiagnostics(state, envelope)
	after, err := workqueue.SerializeProjection(state)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "diagnostics must not modify the captured projection")
}

func TestWorkQueueCapturedDiagnosticsReasonsAndAttempts(t *testing.T) {
	t.Parallel()
	log, ids := workQueueMixedFixture(t)
	log.release(ids["running_dispatch"], "run_missing")
	retried := log.dispatch(1)[0]
	log.launch(retried)
	worker := workQueueTestWorker("203")
	log.bind(retried, worker)
	assignment, err := workqueue.AssignmentForDispatch(log.state(), retried)
	require.NoError(t, err)
	snapshot, err := logsWQParse(t, logsWQEnvelope("worker", worker, &assignment, log.jsonl()))
	require.NoError(t, err)
	diagnostics := snapshot.Current.Diagnostics
	assert.Equal(t, map[string]int{"run_missing": 1}, diagnostics.ClaimCancellationReasons)
	assert.Equal(t, map[string]int{"operator": 1}, diagnostics.WorkCancellationReasons)
	assert.Equal(t, map[string]int{"open": 1, "completed": 1, "cancelled": 1}, diagnostics.ClaimsByState)
	assert.Equal(t, 2, diagnostics.DispatchesByState["closed"])
	require.Len(t, diagnostics.AssignedClaims, 1)
	assigned := diagnostics.AssignedClaims[0]
	assert.Equal(t, 1, assigned.Work.Attempts, "a released Claim counts as an attempt")
	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, &WorkQueueReport{Snapshot: snapshot}))
	text := output.String()
	assert.Contains(t, text, "work: available=1 claimed=1 completed=1 cancelled=1 (activation snapshot; no live reads)")
	assert.Contains(t, text, "claims: open=1 completed=1 cancelled=1 | dispatches: reserved=0 started=0 bound=1 closed=2")
	assert.Contains(t, text, "claim_cancellations: run_missing=1")
	assert.Contains(t, text, "work_cancellations: operator=1")
	assert.Contains(t, text, "claim_diagnostics: "+assigned.Claim.ClaimID+" state=open work_attempts=1")
	assert.Contains(t, text, "Finish intents and later operation logs do not update this snapshot.")
}

func TestWorkQueueCapturedDiagnosticsFromCheckpoint(t *testing.T) {
	t.Parallel()
	_, fixture := logsWQWorkerFixture(t)
	state := fixture.log.state()
	checkpoint, err := workqueue.CheckpointCommit(state, workQueueTestAdmin, 99000)
	require.NoError(t, err)
	compacted, err := workqueue.Serialize([]workqueue.QueueCommit{checkpoint})
	require.NoError(t, err)
	parse := func(log string) *WorkQueueCurrentSnapshot {
		snapshot, err := logsWQParse(t, logsWQEnvelope("worker", fixture.worker, &fixture.assignment, log))
		require.NoError(t, err)
		return snapshot.Current
	}
	original, folded := parse(fixture.log.jsonl()), parse(string(compacted))
	assert.Equal(t, original.Assignment, folded.Assignment, "a checkpoint preserves the bound assignment")
	assert.Equal(t, checkpoint.ID, folded.Tip)
	assert.Equal(t, original.Diagnostics.ClaimsByState, folded.Diagnostics.ClaimsByState)
	assert.Equal(t, original.Diagnostics.DispatchesByState, folded.Diagnostics.DispatchesByState)
	assert.Equal(t, original.Diagnostics.Pools, folded.Diagnostics.Pools)
	assert.Equal(t, original.Diagnostics.AssignedClaims, folded.Diagnostics.AssignedClaims)
	assert.Equal(t, original.Diagnostics.Available, folded.Diagnostics.Available)
	assert.Equal(t, original.Diagnostics.Claimed, folded.Diagnostics.Claimed)
}

func TestWorkQueueCapturedDiagnosticsCacheAndLogs(t *testing.T) {
	t.Parallel()
	envelope, _ := logsWQWorkerFixture(t)
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), data, 0o600))
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	current := *report.Snapshot.Current
	current.Diagnostics = nil
	cached := &WorkQueueReport{Snapshot: &WorkQueueSnapshot{Version: workqueue.Version, SHA: report.Snapshot.SHA, Current: &current}}
	require.True(t, backfillWorkQueueReport(&cached, dir), "summaries without diagnostics need backfill")
	require.NotNil(t, cached.Snapshot.Current.Diagnostics)
	assert.False(t, backfillWorkQueueReport(&cached, dir), "identical diagnostic values must not repeatedly invalidate caches")
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: dir}
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: 42, RunAnalysis: RunAnalysis{Run: run, WorkQueue: cached}}
	require.NoError(t, saveRunSummary(dir, summary, false))
	loaded, ok := loadRunSummary(dir, false)
	require.True(t, ok)
	assert.Equal(t, cached.Snapshot, loaded.WorkQueue.Snapshot)
	assert.False(t, backfillWorkQueueReport(&loaded.WorkQueue, dir), "a reloaded summary must not invalidate its own cache")
	processed := processedRunFromSummary(loaded, dir)
	audit, _ := buildLocalAuditData(processed, LogMetrics{}, nil)
	audit.CacheSource = auditCacheSourceFull
	require.NoError(t, writeAuditData(dir, audit))
	restored, ok := loadCachedAuditData(dir, run, auditCacheSourceFull)
	require.True(t, ok)
	assert.Equal(t, cached.Snapshot, restored.WorkQueue.Snapshot)
	runData := newRunData(processed, runEngineInfo{}, SafeOutputChainMetrics{}, nil, "", 0)
	assert.Equal(t, cached.Snapshot, runData.WorkQueue.Snapshot)
	assert.Nil(t, workQueueReportRuns([]RunData{runData}, false)[0].WorkQueue, "usage-only logs must still omit queue evidence")
	var output bytes.Buffer
	require.NoError(t, renderLogsCompactToWriter(&output, LogsData{Runs: []RunData{runData}}))
	assert.Contains(t, output.String(), "work: available=1 claimed=1 completed=0 cancelled=0 (activation snapshot; no live reads)")
	assert.Contains(t, output.String(), "state=open work_attempts=0")
	assert.Contains(t, output.String(), "Finish intents and later operation logs do not update this snapshot.")
}

func TestWorkQueueCapturedDiagnosticsRenderingBounds(t *testing.T) {
	t.Parallel()
	envelope, _ := logsWQWorkerFixture(t)
	snapshot, err := logsWQParse(t, envelope)
	require.NoError(t, err)
	diagnostics := snapshot.Current.Diagnostics
	pool, claim := diagnostics.Pools[0], diagnostics.AssignedClaims[0]
	pool.Pool = "unsafe\n\x1b[31m"
	pool.Blocked = map[string]int{"z": 2, "a": 1}
	claim.Claim.ClaimID = "claim\x1b[2J"
	diagnostics.ClaimCancellationReasons = map[string]int{"reason\x1b": 1}
	diagnostics.Pools, diagnostics.AssignedClaims = nil, nil
	for range 17 {
		diagnostics.Pools = append(diagnostics.Pools, pool)
		diagnostics.AssignedClaims = append(diagnostics.AssignedClaims, claim)
	}
	text := workQueueDiagnosticsText(diagnostics)
	assert.NotContains(t, text, "unsafe\n")
	assert.NotContains(t, text, "\x1b")
	assert.Contains(t, text, "blocked=a=1,z=2")
	assert.Equal(t, 16, strings.Count(text, "    pool: "))
	assert.Equal(t, 16, strings.Count(text, "work_attempts=0"))
	assert.Contains(t, text, "additional pools available in JSON")
	assert.Contains(t, text, "additional Claims available in JSON")
	assert.Empty(t, workQueueDiagnosticsText(nil))
}
