package workqueue

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func releaseCommit(t *testing.T, log []QueueCommit, dispatchID, reason string, extra ...any) []QueueCommit {
	t.Helper()
	values := append([]any{ReleaseOperation{Kind: "Release", DispatchID: dispatchID, Reason: reason}}, extra...)
	return appendTestCommit(t, log, testReconciler, "release", ReleaseParameters{DispatchID: dispatchID, Reason: reason}, mustOps(t, values...))
}

func rejectAppend(t *testing.T, log []QueueCommit, commit QueueCommit, code string) {
	t.Helper()
	state, err := Replay(log)
	require.NoError(t, err)
	_, err = AppendCommit(state, commit)
	requireCode(t, err, code)
}

func TestLifecycleSubmitDispatchBindFinishRelease(t *testing.T) {
	a := testWork(t, "a", nil)
	b := testWork(t, "b", nil)
	log := testSubmit(t, nil, a, b)
	log, decision := testDispatchNext(t, log, testDispatchParameters(func(parameters *DispatchParameters) { parameters.Concurrency = 1 }))
	require.Equal(t, "capacity_blocked", decision.Reason)
	require.Len(t, decision.Operations, 1)
	require.Equal(t, a.WorkID, decision.Operations[0].WorkID)
	first := decision.Dispatches[0]
	launched := testLaunch(t, log, first)

	other := Actor{Role: "worker", Repository: testRepository, Workflow: ".github/workflows/other.lock.yml", RunID: "9", RunAttempt: 1}
	foreignBind := buildTestCommit(t, launched, other, "bind", DispatchIDParameters{DispatchID: first}, mustOps(t, BoundOperation{Kind: "Bound", DispatchID: first, RunID: "9", RunAttempt: 1}))
	rejectAppend(t, launched, foreignBind, "run_binding_conflict")

	worker := testWorker("7", 1)
	bound := testBind(t, launched, first, worker)
	foreignFinish := buildTestCommit(t, bound, testWorker("8", 1), "finish", DispatchIDParameters{DispatchID: first}, testFinishOperations(t, bound, first))
	rejectAppend(t, bound, foreignFinish, "claim_scope_invalid")

	finished := appendTestCommit(t, bound, worker, "finish", DispatchIDParameters{DispatchID: first}, testFinishOperations(t, bound, first))
	state, err := Replay(finished)
	require.NoError(t, err)
	require.Equal(t, "completed", state.Works[a.WorkID].State)
	require.Equal(t, "available", state.Works[b.WorkID].State)
	require.Equal(t, "closed", state.Dispatches[first].State)
	require.JSONEq(t, "[]", string(state.Works[a.WorkID].Effects))

	log, decision = testDispatchNext(t, finished, testDispatchParameters(nil))
	require.Len(t, decision.Operations, 1)
	require.Equal(t, b.WorkID, decision.Operations[0].WorkID)
	second := decision.Dispatches[0]
	claimID := decision.Operations[0].ClaimID
	state, err = Replay(releaseCommit(t, log, second, "run_missing"))
	require.NoError(t, err)
	require.Equal(t, "available", state.Works[b.WorkID].State)
	require.Equal(t, 1, state.Works[b.WorkID].Attempts)
	require.Equal(t, "closed", state.Dispatches[second].State)
	require.Equal(t, "run_missing", state.Claims[claimID].CancellationReason)

	// With max_attempts 1 the publisher appends WorkCancellation attempts_exhausted.
	exhausted := releaseCommit(t, log, second, "run_missing", WorkCancellationOperation{Kind: "WorkCancellation", WorkID: b.WorkID, Reason: "attempts_exhausted", ClaimID: claimID})
	state, err = Replay(exhausted)
	require.NoError(t, err)
	require.Equal(t, "cancelled", state.Works[b.WorkID].State)
	require.Equal(t, "attempts_exhausted", state.Works[b.WorkID].CancellationReason)
	require.Equal(t, Stats{Work: 2, Completed: 1, Cancelled: 1, Claims: 2, Transactions: len(exhausted), Nodes: 2}, state.Stats)
}

func TestReplayRejectsNonV4AndDuplicateRequests(t *testing.T) {
	_, err := ReplayLog([]byte(`{"version":3}` + "\n"))
	requireCode(t, err, "unsupported_protocol")
	log := testSubmit(t, nil, testWork(t, "a", nil))
	duplicate := log[0]
	previous := log[0].ID
	duplicate.Previous = &previous
	duplicate.ID = CommitID(&previous, duplicate.Request.ID)
	_, err = Replay(append(log, duplicate))
	requireCode(t, err, "request_reused")
}

func TestCheckpointRoundTripPreservesProjection(t *testing.T) {
	log := testSubmit(t, nil, testWork(t, "a", nil), testWork(t, "b", nil))
	log, decision := testDispatchNext(t, log, testDispatchParameters(nil))
	log = testLaunch(t, log, decision.Dispatches[0])
	state, err := Replay(log)
	require.NoError(t, err)
	commit, err := CheckpointCommit(state, testReconciler, 5)
	require.NoError(t, err)
	require.Nil(t, commit.Previous)
	restored, err := Replay([]QueueCommit{commit})
	require.NoError(t, err)
	require.Equal(t, state.Commits+1, restored.Commits)
	require.Equal(t, commit.ID, restored.Tip)
	original, err := SerializeProjection(state)
	require.NoError(t, err)
	var before, after map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(original, &before))
	data, err := SerializeProjection(restored)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &after))
	for _, field := range []string{"repository", "works", "claims", "dispatches", "observations", "work_creators"} {
		require.JSONEq(t, string(before[field]), string(after[field]), field)
	}
	// Lines after a checkpoint replay normally; a checkpoint is only valid as line 1.
	next := testBind(t, []QueueCommit{commit}, decision.Dispatches[0], testWorker("7", 1))
	_, err = Replay(append(next, commit))
	require.Error(t, err)
}

func TestPlanDispatchHonorsPerAccountLimit(t *testing.T) {
	works := []WorkDefinition{
		testWork(t, "a", func(work *WorkDefinition) { work.FairnessKey = "x" }),
		testWork(t, "b", func(work *WorkDefinition) { work.FairnessKey = "x" }),
		testWork(t, "c", func(work *WorkDefinition) { work.FairnessKey = "y" }),
	}
	state, err := Replay(testSubmit(t, nil, works...))
	require.NoError(t, err)
	decision, err := PlanDispatch(state, testDispatchParameters(func(parameters *DispatchParameters) { parameters.PerAccountLimit = 1 }), "request:test")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "c"}, nodeKeys(state, decision))
	require.Equal(t, "prefix_complete", decision.Reason)
	claimID, dispatchID, handle := ClaimIdentity("request:test", works[0].WorkID)
	require.Equal(t, ClaimOperation{Kind: "Claim", WorkID: works[0].WorkID, ClaimID: claimID, DispatchID: dispatchID, Handle: handle}, decision.Operations[0])
}
