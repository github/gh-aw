package workqueue

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Go mirrors of work_queue_test_helpers.cjs.

const testRepository = "owner/repo"
const testWorkerWorkflow = ".github/workflows/worker.lock.yml"

var (
	testAdministrator = Actor{Role: "administrator", Repository: testRepository}
	testProducer      = Actor{Role: "producer", Repository: testRepository, Workflow: ".github/workflows/producer.lock.yml", RunID: "10", RunAttempt: 1}
	testDispatcher    = Actor{Role: "dispatcher", Repository: testRepository, Workflow: ".github/workflows/dispatcher.lock.yml", RunID: "100", RunAttempt: 1}
	testReconciler    = Actor{Role: "reconciler", Repository: testRepository, Workflow: ".github/workflows/agentics-maintenance.yml", RunID: "300", RunAttempt: 1}
	testProfiles      = map[string]DispatchProfile{"worker": {Workflow: testWorkerWorkflow}}
)

func testWorker(runID string, attempt int) Actor {
	return Actor{Role: "worker", Repository: testRepository, Workflow: testWorkerWorkflow, RunID: runID, RunAttempt: attempt}
}

func testWork(t *testing.T, key string, mutate func(*WorkDefinition)) WorkDefinition {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"task": key})
	require.NoError(t, err)
	work := WorkDefinition{Kind: "Work", WorkID: NodeID("graph", key), GraphID: "graph", NodeKey: key, Pool: "default", Priority: 3, FairnessKey: "", WorkerProfile: "worker", Payload: payload, DependsOn: []Dependency{}}
	if mutate != nil {
		mutate(&work)
		work.WorkID = NodeID(work.GraphID, work.NodeKey)
	}
	return work
}

func mustOps(t *testing.T, values ...any) []Operation {
	t.Helper()
	operations := make([]Operation, 0, len(values))
	for _, value := range values {
		operation, err := Op(value)
		require.NoError(t, err)
		operations = append(operations, operation)
	}
	return operations
}

func buildTestCommit(t *testing.T, log []QueueCommit, actor Actor, kind string, parameters any, operations []Operation) QueueCommit {
	t.Helper()
	state, err := Replay(log)
	require.NoError(t, err)
	request, err := NewRequest(kind, actor, parameters)
	require.NoError(t, err)
	return BuildCommit(state, actor, request, operations, 1)
}

func appendTestCommit(t *testing.T, log []QueueCommit, actor Actor, kind string, parameters any, operations []Operation) []QueueCommit {
	t.Helper()
	commit := buildTestCommit(t, log, actor, kind, parameters, operations)
	state, err := Replay(log)
	require.NoError(t, err)
	_, err = AppendCommit(state, commit)
	require.NoError(t, err)
	return append(append([]QueueCommit{}, log...), commit)
}

func testSubmit(t *testing.T, log []QueueCommit, works ...WorkDefinition) []QueueCommit {
	t.Helper()
	ids := []string{}
	values := []any{}
	for _, work := range works {
		ids = append(ids, work.WorkID)
		values = append(values, work)
	}
	return appendTestCommit(t, log, testProducer, "submit", WorkIDsParameters{WorkIDs: ids}, mustOps(t, values...))
}

func testDispatchParameters(mutate func(*DispatchParameters)) DispatchParameters {
	parameters := DispatchParameters{Pool: "default", MaxClaims: 16, MaxDispatches: 16, Concurrency: 16, PerAccountLimit: 0, Profiles: testProfiles}
	if mutate != nil {
		mutate(&parameters)
	}
	return parameters
}

func testDispatchNext(t *testing.T, log []QueueCommit, parameters DispatchParameters) ([]QueueCommit, Decision) {
	t.Helper()
	state, err := Replay(log)
	require.NoError(t, err)
	request, err := NewRequest("dispatch_next", testDispatcher, parameters)
	require.NoError(t, err)
	decision, err := PlanDispatch(state, parameters, request.ID)
	require.NoError(t, err)
	if len(decision.Operations) == 0 {
		return log, decision
	}
	values := []any{}
	for _, operation := range decision.Operations {
		values = append(values, operation)
	}
	commit := BuildCommit(state, testDispatcher, request, mustOps(t, values...), 1)
	_, err = AppendCommit(state, commit)
	require.NoError(t, err)
	return append(append([]QueueCommit{}, log...), commit), decision
}

// testRevision is the head revision of every launched test worker run.
const testRevision = "cccccccccccccccccccccccccccccccccccccccc"

// testLaunch records the launch of worker run runID for a Dispatch by the dispatcher
// attempt that reserved it, the only actor replay lets record it.
func testLaunch(t *testing.T, log []QueueCommit, dispatchID, runID string) []QueueCommit {
	t.Helper()
	state, err := Replay(log)
	require.NoError(t, err)
	require.Contains(t, state.Dispatches, dispatchID)
	actor := testDispatcher
	actor.RunID, actor.RunAttempt = state.Dispatches[dispatchID].ReservedBy.RunID, state.Dispatches[dispatchID].ReservedBy.RunAttempt
	return appendTestCommit(t, log, actor, "launch", DispatchIDParameters{DispatchID: dispatchID}, mustOps(t, DispatchOperation{Kind: "Dispatch", DispatchID: dispatchID, State: "started", RunID: runID, Revision: testRevision}))
}

func testBind(t *testing.T, log []QueueCommit, dispatchID string, worker Actor) []QueueCommit {
	t.Helper()
	return appendTestCommit(t, log, worker, "bind", DispatchIDParameters{DispatchID: dispatchID}, mustOps(t, BoundOperation{Kind: "Bound", DispatchID: dispatchID, RunID: worker.RunID, RunAttempt: worker.RunAttempt}))
}

func testFinishOperations(t *testing.T, log []QueueCommit, dispatchID string) []Operation {
	t.Helper()
	state, err := Replay(log)
	require.NoError(t, err)
	values := []any{}
	if claim := state.OpenClaim(state.Dispatches[dispatchID]); claim != nil {
		values = append(values, CompletionOperation{Kind: "Completion", WorkID: claim.WorkID, ClaimID: claim.ClaimID, DispatchID: dispatchID, ClaimHandle: claim.Handle, Effects: json.RawMessage("[]")})
	}
	return mustOps(t, values...)
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var protocol *ProtocolError
	require.ErrorAs(t, err, &protocol, "expected ProtocolError, got %v", err)
	require.Equal(t, code, protocol.Code, err.Error())
}
