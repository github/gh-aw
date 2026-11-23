package workqueue

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var testApproval = Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "55", Number: "5"}

// boundParent submits parent, claims, launches and binds it to worker run "21".
func boundParent(t *testing.T, log []QueueCommit, parent WorkDefinition, dispatcherRun string) ([]QueueCommit, string, Actor) {
	t.Helper()
	log = testSubmit(t, log, parent)
	return claimAndBind(t, log, dispatcherRun, "21")
}

func claimAndBind(t *testing.T, log []QueueCommit, dispatcherRun, workerRun string) ([]QueueCommit, string, Actor) {
	t.Helper()
	dispatcher := testDispatcher
	dispatcher.RunID = dispatcherRun
	parameters := testDispatchParameters(func(parameters *DispatchParameters) { parameters.MaxClaims, parameters.MaxDispatches = 1, 1 })
	state, err := Replay(log)
	require.NoError(t, err)
	request, err := NewRequest("dispatch_next", dispatcher, parameters)
	require.NoError(t, err)
	decision, err := PlanDispatch(state, parameters, request.ID)
	require.NoError(t, err)
	require.Len(t, decision.Dispatches, 1)
	commit := BuildCommit(state, dispatcher, request, mustOps(t, decision.Operations[0]), 1)
	_, err = AppendCommit(state, commit)
	require.NoError(t, err)
	log = append(append([]QueueCommit{}, log...), commit)
	dispatchID := decision.Dispatches[0]
	log = appendTestCommit(t, log, dispatcher, "launch", DispatchIDParameters{DispatchID: dispatchID}, mustOps(t, DispatchOperation{Kind: "Dispatch", DispatchID: dispatchID, State: "started", RunID: workerRun, Revision: testRevision}))
	worker := testWorker(workerRun, 1)
	return testBind(t, log, dispatchID, worker), dispatchID, worker
}

func finishWithChild(t *testing.T, log []QueueCommit, dispatchID string, worker Actor, children ...any) (QueueCommit, error) {
	t.Helper()
	values := []any{}
	for _, operation := range testFinishOperations(t, log, dispatchID) {
		values = append(values, operation)
	}
	values = append(values, children...)
	commit := buildTestCommit(t, log, worker, "finish", DispatchIDParameters{DispatchID: dispatchID}, mustOps(t, values...))
	state, err := Replay(log)
	require.NoError(t, err)
	_, err = AppendCommit(state, commit)
	return commit, err
}

func auditParent(t *testing.T) WorkDefinition {
	t.Helper()
	return testWork(t, "p", func(work *WorkDefinition) {
		work.Payload = json.RawMessage(`{"effect_contract":{"kind":"none"},"task":"audit pages"}`)
		work.Priority = 2
		work.FairnessKey = "account"
	})
}

func TestContinuationChildShape(t *testing.T) {
	parent := auditParent(t)
	child, err := ContinuationChild(parent, WorkContinuation{Route: "workqueue", Message: "Audit page 2", Remaining: 1}, nil)
	require.NoError(t, err)
	require.Equal(t, "continuation:"+parent.WorkID, child.NodeKey)
	require.Equal(t, NodeID(parent.GraphID, child.NodeKey), child.WorkID)
	require.Equal(t, []Dependency{{Kind: "work", WorkID: parent.WorkID}}, child.DependsOn)
	require.Equal(t, parent.Priority, child.Priority)
	require.Equal(t, parent.FairnessKey, child.FairnessKey)
	require.JSONEq(t, string(parent.Payload), string(child.Payload))
	require.Nil(t, child.BackingIssue)
	require.Nil(t, child.ReplacementOf)

	gated, err := ContinuationChild(parent, WorkContinuation{Route: "issue", Message: "Audit page 2"}, &testApproval)
	require.NoError(t, err)
	require.Equal(t, Dependency{Kind: "issue", Resource: &testApproval, Condition: "approved"}, gated.DependsOn[1])

	_, err = ContinuationChild(parent, WorkContinuation{Route: "issue", Message: "Audit page 2"}, nil)
	requireCode(t, err, "request_invalid")
	_, err = ContinuationChild(parent, WorkContinuation{Route: "issue", Message: "Audit page 2", Remaining: 1}, &testApproval)
	requireCode(t, err, "request_invalid")
}

func TestContinuationValidation(t *testing.T) {
	parent := auditParent(t)
	for _, tc := range []struct {
		name         string
		continuation WorkContinuation
		code         string
	}{
		{"blank message", WorkContinuation{Route: "workqueue", Message: " \t\u00a0\ufeff"}, "request_invalid"},
		{"unknown route", WorkContinuation{Route: "dispatch", Message: "next"}, "request_invalid"},
		{"remaining above bound", WorkContinuation{Route: "workqueue", Message: "next", Remaining: ContinuationRemainingMax + 1}, "request_invalid"},
		{"negative remaining", WorkContinuation{Route: "workqueue", Message: "next", Remaining: -1}, "request_invalid"},
		{"oversized Unicode message", WorkContinuation{Route: "workqueue", Message: strings.Repeat("é", ContinuationMessageBytes/2+1)}, "resource_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ContinuationChild(parent, tc.continuation, nil)
			requireCode(t, err, tc.code)
		})
	}
	t.Run("payload and continuation share the payload budget", func(t *testing.T) {
		big := testWork(t, "big", func(work *WorkDefinition) {
			work.Payload = json.RawMessage(fmt.Sprintf(`{"task":%q}`, strings.Repeat("x", PayloadBytes-64)))
		})
		require.NoError(t, ValidateWork(big))
		_, err := ContinuationChild(big, WorkContinuation{Route: "workqueue", Message: strings.Repeat("y", 128)}, nil)
		requireCode(t, err, "resource_limit")
	})
	t.Run("a NEL-only message is nonblank as in JavaScript", func(t *testing.T) {
		_, err := ContinuationChild(parent, WorkContinuation{Route: "workqueue", Message: "\u0085"}, nil)
		require.NoError(t, err)
	})
}

func TestContinuationFinishAdmitsChild(t *testing.T) {
	parent := auditParent(t)
	log, dispatchID, worker := boundParent(t, nil, parent, "201")
	child, err := ContinuationChild(parent, WorkContinuation{Route: "workqueue", Message: "Audit page 2", Remaining: 1}, nil)
	require.NoError(t, err)
	commit, err := finishWithChild(t, log, dispatchID, worker, child)
	require.NoError(t, err)
	log = append(log, commit)
	state, err := Replay(log)
	require.NoError(t, err)
	require.Equal(t, "completed", state.Works[parent.WorkID].State)
	require.Equal(t, "available", state.Works[child.WorkID].State)
	require.Equal(t, Position{Commit: len(log) - 1, Operation: 1}, state.Works[child.WorkID].Position)

	// The next normal dispatch assigns the successor with its continuation and the parent's results.
	log, childDispatch, _ := claimAndBind(t, log, "202", "22")
	state, err = Replay(log)
	require.NoError(t, err)
	assignment, err := AssignmentForDispatch(state, childDispatch)
	require.NoError(t, err)
	require.Len(t, assignment.Claims, 1)
	require.Equal(t, &WorkContinuation{Route: "workqueue", Message: "Audit page 2", Remaining: 1}, assignment.Claims[0].Continuation)
	require.JSONEq(t, string(parent.Payload), string(assignment.Claims[0].Work))
	require.Equal(t, parent.WorkID, assignment.Claims[0].ResultRefs[0].WorkID)

	// A successor's own automatic hop must spend budget; zero cannot hand off.
	same, err := ContinuationChild(child, WorkContinuation{Route: "workqueue", Message: "Audit page 3", Remaining: 1}, nil)
	require.NoError(t, err)
	_, err = finishWithChild(t, log, childDispatch, testWorker("22", 1), same)
	requireCode(t, err, "request_invalid")
	smaller, err := ContinuationChild(child, WorkContinuation{Route: "workqueue", Message: "Audit page 3", Remaining: 0}, nil)
	require.NoError(t, err)
	_, err = finishWithChild(t, log, childDispatch, testWorker("22", 1), smaller)
	require.NoError(t, err)

	// Checkpoint restoration preserves the continuation member.
	checkpoint, err := CheckpointCommit(state, testReconciler, 1)
	require.NoError(t, err)
	restored, err := Replay([]QueueCommit{checkpoint})
	require.NoError(t, err)
	require.Equal(t, state.Works[child.WorkID].Continuation, restored.Works[child.WorkID].Continuation)
}

func TestContinuationFinishRejections(t *testing.T) {
	parent := auditParent(t)
	log, dispatchID, worker := boundParent(t, nil, parent, "201")
	child, err := ContinuationChild(parent, WorkContinuation{Route: "workqueue", Message: "Audit page 2"}, nil)
	require.NoError(t, err)
	forged := child
	forged.WorkerProfile = "other"
	orphan := child
	orphan.DependsOn = []Dependency{}

	t.Run("capability widening", func(t *testing.T) {
		_, err := finishWithChild(t, log, dispatchID, worker, forged)
		requireCode(t, err, "request_invalid")
	})
	t.Run("missing predecessor", func(t *testing.T) {
		_, err := finishWithChild(t, log, dispatchID, worker, orphan)
		requireCode(t, err, "request_invalid")
	})
	t.Run("duplicate child", func(t *testing.T) {
		_, err := finishWithChild(t, log, dispatchID, worker, child, child)
		requireCode(t, err, "request_invalid")
	})
	t.Run("unpaired child", func(t *testing.T) {
		values := []any{child}
		for _, operation := range testFinishOperations(t, log, dispatchID) {
			values = append(values, operation)
		}
		commit := buildTestCommit(t, log, worker, "finish", DispatchIDParameters{DispatchID: dispatchID}, mustOps(t, values...))
		state, err := Replay(log)
		require.NoError(t, err)
		_, err = AppendCommit(state, commit)
		requireCode(t, err, "request_invalid")
	})
	t.Run("pre-existing deterministic identity", func(t *testing.T) {
		preexisting := testSubmit(t, log, testWork(t, child.NodeKey, nil))
		_, err := finishWithChild(t, preexisting, dispatchID, worker, child)
		requireCode(t, err, "request_invalid")
	})
	t.Run("submit cannot mint a continuation", func(t *testing.T) {
		minted := child
		minted.DependsOn = []Dependency{}
		commit := buildTestCommit(t, log, testProducer, "submit", WorkIDsParameters{WorkIDs: []string{minted.WorkID}}, mustOps(t, minted))
		state, err := Replay(log)
		require.NoError(t, err)
		_, err = AppendCommit(state, commit)
		requireCode(t, err, "request_invalid")
	})
}

func TestContinuationIssueApproval(t *testing.T) {
	parent := auditParent(t)
	log, dispatchID, worker := boundParent(t, nil, parent, "201")
	child, err := ContinuationChild(parent, WorkContinuation{Route: "issue", Message: "Audit page 2"}, &testApproval)
	require.NoError(t, err)
	commit, err := finishWithChild(t, log, dispatchID, worker, child)
	require.NoError(t, err)
	log = append(log, commit)
	state, err := Replay(log)
	require.NoError(t, err)
	decision, err := PlanDispatch(state, testDispatchParameters(nil), "request:planning")
	require.NoError(t, err)
	require.Empty(t, decision.Operations, "an unapproved successor is not schedulable")

	claim := ClaimOperation{Kind: "Claim", WorkID: child.WorkID, ClaimID: "c_premature", DispatchID: "d_premature", Handle: "h1"}
	completed := ObservationOperation{Kind: "Observation", ObservationID: "o-completed", Resource: testApproval, Condition: "completed", State: "ready", StateReason: "completed"}
	approved := ObservationOperation{Kind: "Observation", ObservationID: "o-approved", Resource: testApproval, Condition: "approved", State: "ready", StateReason: "completed", ApprovedBy: "583231"}
	dispatchNext := func(operations ...any) error {
		_, err := AppendCommit(state, buildTestCommit(t, log, testDispatcher, "dispatch_next", testDispatchParameters(nil), mustOps(t, operations...)))
		return err
	}
	requireCode(t, dispatchNext(claim), "approval_required")
	requireCode(t, dispatchNext(completed, claim), "approval_required")
	require.NoError(t, dispatchNext(approved, claim))

	for _, tc := range []struct {
		name        string
		observation ObservationOperation
	}{
		{"approved_by on a completed gate", ObservationOperation{Kind: "Observation", ObservationID: "o", Resource: testApproval, Condition: "completed", State: "ready", StateReason: "completed", ApprovedBy: "1"}},
		{"approved_by on a waiting approval", ObservationOperation{Kind: "Observation", ObservationID: "o", Resource: testApproval, Condition: "approved", State: "waiting", ApprovedBy: "1"}},
		{"ready approval without approver", ObservationOperation{Kind: "Observation", ObservationID: "o", Resource: testApproval, Condition: "approved", State: "ready", StateReason: "completed"}},
		{"ready approval closed as not planned", ObservationOperation{Kind: "Observation", ObservationID: "o", Resource: testApproval, Condition: "approved", State: "ready", StateReason: "not_planned", ApprovedBy: "1"}},
		{"non-decimal approver", ObservationOperation{Kind: "Observation", ObservationID: "o", Resource: testApproval, Condition: "approved", State: "ready", StateReason: "completed", ApprovedBy: "octocat"}},
	} {
		t.Run(tc.name, func(t *testing.T) { requireCode(t, dispatchNext(tc.observation), "observation_invalid") })
	}

	t.Run("one successor per approval Issue", func(t *testing.T) {
		other := testWork(t, "q", nil)
		log, dispatchID, worker := boundParent(t, log, other, "203")
		second, err := ContinuationChild(other, WorkContinuation{Route: "issue", Message: "again"}, &testApproval)
		require.NoError(t, err)
		_, err = finishWithChild(t, log, dispatchID, worker, second)
		requireCode(t, err, "issue_binding_conflict")
	})
}

// A pool at the hard pending ceiling can still replace a completed parent with its successor.
func TestContinuationReplacesPendingSlot(t *testing.T) {
	parent := auditParent(t)
	fillers := []WorkDefinition{}
	for index := range MaxPendingWork - 1 {
		fillers = append(fillers, testWork(t, fmt.Sprintf("filler-%d", index), func(work *WorkDefinition) { work.GraphID, work.Priority = "fillers", 5 }))
	}
	log := testSubmit(t, nil, parent)
	for start := 0; start < len(fillers); start += OperationsPerCommit {
		log = testSubmit(t, log, fillers[start:min(start+OperationsPerCommit, len(fillers))]...)
	}
	full, err := Replay(log)
	require.NoError(t, err)
	require.Equal(t, MaxPendingWork, full.Stats.Available)
	overflow := testWork(t, "overflow", nil)
	_, err = AppendCommit(full, buildTestCommit(t, log, testProducer, "submit", WorkIDsParameters{WorkIDs: []string{overflow.WorkID}}, mustOps(t, overflow)))
	requireCode(t, err, "resource_limit")

	log, dispatchID, worker := claimAndBind(t, log, "201", "21")
	child, err := ContinuationChild(parent, WorkContinuation{Route: "workqueue", Message: "Audit page 2"}, nil)
	require.NoError(t, err)
	_, err = finishWithChild(t, log, dispatchID, worker, child)
	require.NoError(t, err, "the Completion frees the slot its successor takes")
}
