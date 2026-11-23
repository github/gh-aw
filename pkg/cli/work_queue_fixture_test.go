package cli

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

// workQueueTestLog builds valid v4 ledgers for CLI tests through the public
// workqueue API, so every fixture replays exactly like a published branch.
type workQueueTestLog struct {
	t       testing.TB
	commits []workqueue.QueueCommit
	at      int64
}

const workQueueTestRepository = "owner/repo"
const workQueueTestWorkerWorkflow = ".github/workflows/worker.lock.yml"

var (
	workQueueTestProducer   = workqueue.Actor{Role: "producer", Repository: workQueueTestRepository, Workflow: ".github/workflows/producer.lock.yml", RunID: "10", RunAttempt: 1}
	workQueueTestDispatcher = workqueue.Actor{Role: "dispatcher", Repository: workQueueTestRepository, Workflow: ".github/workflows/dispatcher.lock.yml", RunID: "100", RunAttempt: 1}
	workQueueTestAdmin      = workqueue.Actor{Role: "administrator", Repository: workQueueTestRepository}
)

func workQueueTestWorker(runID string) workqueue.Actor {
	return workqueue.Actor{Role: "worker", Repository: workQueueTestRepository, Workflow: workQueueTestWorkerWorkflow, RunID: runID, RunAttempt: 1}
}

func newWorkQueueTestLog(t testing.TB) *workQueueTestLog {
	return &workQueueTestLog{t: t, at: 1000}
}

// work returns a default-pool Work definition for key; mutate may adjust it.
func (l *workQueueTestLog) work(key string, mutate func(*workqueue.WorkDefinition)) workqueue.WorkDefinition {
	l.t.Helper()
	payload, err := json.Marshal(map[string]string{"task": key})
	require.NoError(l.t, err)
	work := workqueue.WorkDefinition{Kind: "Work", GraphID: "graph", NodeKey: key, Pool: "default", Priority: 3, WorkerProfile: "worker", Payload: payload, DependsOn: []workqueue.Dependency{}}
	if mutate != nil {
		mutate(&work)
	}
	work.WorkID = workqueue.NodeID(work.GraphID, work.NodeKey)
	return work
}

func (l *workQueueTestLog) state() workqueue.Projection {
	l.t.Helper()
	state, err := workqueue.Replay(l.commits)
	require.NoError(l.t, err)
	return state
}

// append commits one request with the given operations and returns its commit.
func (l *workQueueTestLog) append(actor workqueue.Actor, kind string, parameters any, values ...any) workqueue.QueueCommit {
	l.t.Helper()
	state := l.state()
	request, err := workqueue.NewRequest(kind, actor, parameters)
	require.NoError(l.t, err)
	operations := make([]workqueue.Operation, 0, len(values))
	for _, value := range values {
		operation, err := workqueue.Op(value)
		require.NoError(l.t, err)
		operations = append(operations, operation)
	}
	l.at += 1000
	commit := workqueue.BuildCommit(state, actor, request, operations, l.at)
	_, err = workqueue.AppendCommit(state, commit)
	require.NoError(l.t, err)
	l.commits = append(l.commits, commit)
	return commit
}

func (l *workQueueTestLog) submit(works ...workqueue.WorkDefinition) []string {
	l.t.Helper()
	ids := make([]string, 0, len(works))
	values := make([]any, 0, len(works))
	for _, work := range works {
		ids = append(ids, work.WorkID)
		values = append(values, work)
	}
	l.append(workQueueTestProducer, "submit", workqueue.WorkIDsParameters{WorkIDs: ids}, values...)
	return ids
}

// dispatch plans and commits one dispatch_next for the default pool as a new
// dispatcher run; it returns the created dispatch IDs in creation order.
func (l *workQueueTestLog) dispatch(maxClaims int) []string {
	l.t.Helper()
	parameters := workqueue.DispatchParameters{Pool: "default", MaxClaims: maxClaims, MaxDispatches: maxClaims, Concurrency: 16,
		Profiles: map[string]workqueue.DispatchProfile{"worker": {Workflow: workQueueTestWorkerWorkflow}}}
	dispatcher := workQueueTestDispatcher
	dispatcher.RunID = strconv.Itoa(100 + len(l.commits))
	request, err := workqueue.NewRequest("dispatch_next", dispatcher, parameters)
	require.NoError(l.t, err)
	decision, err := workqueue.PlanDispatch(l.state(), parameters, request.ID)
	require.NoError(l.t, err)
	require.NotEmpty(l.t, decision.Operations, "fixture dispatch must claim Work")
	values := make([]any, 0, len(decision.Operations))
	for _, operation := range decision.Operations {
		values = append(values, operation)
	}
	l.append(dispatcher, "dispatch_next", parameters, values...)
	return decision.Dispatches
}

// launch records the dispatcher's launch of worker run runID for a Dispatch.
func (l *workQueueTestLog) launch(dispatchID, runID string) {
	l.t.Helper()
	l.append(workQueueTestDispatcher, "launch", workqueue.DispatchIDParameters{DispatchID: dispatchID},
		workqueue.DispatchOperation{Kind: "Dispatch", DispatchID: dispatchID, State: "started", RunID: runID, Revision: strings.Repeat("c", 40)})
}

func (l *workQueueTestLog) bind(dispatchID string, worker workqueue.Actor) {
	l.t.Helper()
	l.append(worker, "bind", workqueue.DispatchIDParameters{DispatchID: dispatchID},
		workqueue.BoundOperation{Kind: "Bound", DispatchID: dispatchID, RunID: worker.RunID, RunAttempt: worker.RunAttempt})
}

// finish completes every open Claim of a bound dispatch.
func (l *workQueueTestLog) finish(dispatchID string, worker workqueue.Actor) {
	l.t.Helper()
	state := l.state()
	values := []any{}
	for _, claim := range state.OpenClaims(state.Dispatches[dispatchID]) {
		values = append(values, workqueue.CompletionOperation{Kind: "Completion", WorkID: claim.WorkID, ClaimID: claim.ClaimID, DispatchID: dispatchID, ClaimHandle: claim.Handle, Effects: json.RawMessage("[]")})
	}
	l.append(worker, "finish", workqueue.DispatchIDParameters{DispatchID: dispatchID}, values...)
}

func (l *workQueueTestLog) release(dispatchID, reason string) {
	l.t.Helper()
	l.append(workQueueTestAdmin, "release", workqueue.ReleaseParameters{DispatchID: dispatchID, Reason: reason},
		workqueue.ReleaseOperation{Kind: "Release", DispatchID: dispatchID, Reason: reason})
}

func (l *workQueueTestLog) cancel(workIDs ...string) {
	l.t.Helper()
	values := make([]any, 0, len(workIDs))
	for _, id := range workIDs {
		values = append(values, workqueue.WorkCancellationOperation{Kind: "WorkCancellation", WorkID: id, Reason: "operator"})
	}
	l.append(workQueueTestAdmin, "cancel_work", workqueue.CancelWorkParameters{WorkIDs: workIDs, Reason: "operator"}, values...)
}

func (l *workQueueTestLog) jsonl() string {
	l.t.Helper()
	data, err := workqueue.Serialize(l.commits)
	require.NoError(l.t, err)
	return string(data)
}

// workQueueMixedFixture returns a ledger with Work in every state: one completed,
// one claimed and bound, one available, and one cancelled.
func workQueueMixedFixture(t testing.TB) (*workQueueTestLog, map[string]string) {
	t.Helper()
	log := newWorkQueueTestLog(t)
	works := []workqueue.WorkDefinition{log.work("done", nil), log.work("running", nil), log.work("waiting", nil), log.work("dropped", nil)}
	log.submit(works...)
	done := log.dispatch(1)[0]
	log.launch(done, "201")
	log.bind(done, workQueueTestWorker("201"))
	log.finish(done, workQueueTestWorker("201"))
	running := log.dispatch(1)[0]
	log.launch(running, "202")
	log.bind(running, workQueueTestWorker("202"))
	log.cancel(works[3].WorkID)
	return log, map[string]string{
		"done": works[0].WorkID, "running": works[1].WorkID, "waiting": works[2].WorkID, "dropped": works[3].WorkID,
		"done_dispatch": done, "running_dispatch": running,
	}
}
