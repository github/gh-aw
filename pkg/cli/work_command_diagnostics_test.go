package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

func assertWorkDiagnosticsPrivate(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	for _, forbidden := range []string{"PAYLOAD-MUST", "SECRET-MUST", `"payload"`, `"parameters"`, `"task"`} {
		require.NotContains(t, string(data), forbidden, "diagnostics exposed %s", forbidden)
	}
}

func TestWorkQueueDiagnosticStatsCountLedgerStates(t *testing.T) {
	log, ids := workQueueMixedFixture(t)
	state := log.state()
	before, err := workqueue.SerializeProjection(state)
	require.NoError(t, err)

	report := workQueueStats(state, "work-queue")
	require.Equal(t, workqueue.Stats{Work: 4, Available: 1, Claimed: 1, Completed: 1, Cancelled: 1, Claims: 2, Dispatches: 1, Transactions: 9, Nodes: 4}, report.Stats)
	require.Equal(t, workQueueSnapshot{Status: "committed_snapshot", Tip: state.Tip, Repository: workQueueTestRepository, Branch: "work-queue"}, report.workQueueSnapshot)
	require.Equal(t, map[string]int{"open": 1, "completed": 1, "cancelled": 0}, report.ClaimsByState)
	require.Equal(t, map[string]int{"reserved": 0, "started": 0, "bound": 1, "closed": 1}, report.DispatchesByState)
	require.Equal(t, map[string]int{"operator": 1}, report.WorkCancellationReasons)
	require.Empty(t, report.ClaimCancellationReasons)
	require.Equal(t, []workQueuePoolStats{{
		Pool: "default", WorkByState: map[string]int{"available": 1, "claimed": 1, "completed": 1, "cancelled": 1},
		ClaimsByState: map[string]int{"open": 1, "completed": 1, "cancelled": 0}, OpenDispatches: 1,
		Eligible: 1, Blocked: map[string]int{}, NextEligible: []string{ids["waiting"]},
	}}, report.Pools)
	assertWorkDiagnosticsPrivate(t, report)
	after, err := workqueue.SerializeProjection(state)
	require.NoError(t, err)
	require.Equal(t, before, after, "stats mutated the projection")

	text := workQueueStatsText(report)
	for _, expected := range []string{
		"Work: 4; available: 1; claimed: 1; completed: 1; cancelled: 1; Claims: 2; open dispatches: 1; graph nodes: 4",
		"Claims: open=1 completed=1 cancelled=0 | dispatches: reserved=0 started=0 bound=1 closed=1",
		"Tip: " + workQueueText(state.Tip) + " | transactions: 9",
		"Work cancellations: operator=1",
		"pool: default available=1 eligible=1 claimed=1 open_dispatches=1 next=" + ids["waiting"],
		"not live Actions status",
	} {
		require.Contains(t, text, expected)
	}
	require.NotContains(t, text, "Claim cancellations")

	log.release(ids["running_dispatch"], "run_missing")
	report = workQueueStats(log.state(), "work-queue")
	require.Equal(t, map[string]int{"run_missing": 1}, report.ClaimCancellationReasons)
	require.Equal(t, map[string]int{"reserved": 0, "started": 0, "bound": 0, "closed": 2}, report.DispatchesByState)
	require.Equal(t, 2, report.Pools[0].Eligible)
	require.Equal(t, []string{ids["running"], ids["waiting"]}, report.Pools[0].NextEligible, "released Work keeps its FIFO position")
	require.Contains(t, workQueueStatsText(report), "Claim cancellations: run_missing=1")
}

func TestWorkQueueDiagnosticStatsEligibilityAndBlockedReasons(t *testing.T) {
	t.Setenv("GITHUB_SERVER_URL", "")
	log := newWorkQueueTestLog(t)
	priority := func(value int) func(*workqueue.WorkDefinition) {
		return func(work *workqueue.WorkDefinition) { work.Priority = value }
	}
	gate := func(kind, id string) *workqueue.Resource {
		return &workqueue.Resource{Kind: kind, Host: "github.com", Repository: workQueueTestRepository, RepositoryID: "1", ResourceID: id, Number: id}
	}
	late, first, second, third := log.work("late", priority(5)), log.work("first", nil), log.work("second", nil), log.work("third", nil)
	urgent := log.work("urgent", priority(1))
	doomed := log.work("doomed", nil)
	elsewhere := log.work("elsewhere", func(work *workqueue.WorkDefinition) { work.GraphID, work.Pool, work.Priority = "elsewhere", "other", 1 })
	child := log.work("child", func(work *workqueue.WorkDefinition) {
		work.DependsOn = []workqueue.Dependency{{Kind: "work", WorkID: first.WorkID}}
	})
	orphan := log.work("orphan", func(work *workqueue.WorkDefinition) {
		work.DependsOn = []workqueue.Dependency{{Kind: "work", WorkID: doomed.WorkID}}
	})
	unobserved := log.work("unobserved", func(work *workqueue.WorkDefinition) {
		work.DependsOn = []workqueue.Dependency{{Kind: "issue", Resource: gate("issue", "11"), Condition: "completed"}}
	})
	waiting := log.work("gated", func(work *workqueue.WorkDefinition) {
		work.DependsOn = []workqueue.Dependency{{Kind: "pull_request", Resource: gate("pull_request", "12"), Condition: "merged"}}
	})
	log.submit(late, first, second, third, urgent, doomed, elsewhere, child, orphan, unobserved, waiting)
	log.cancel(doomed.WorkID)
	log.append(workQueueTestDispatcher, "dispatch_next", workqueue.DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, Concurrency: 1,
		Profiles: map[string]workqueue.DispatchProfile{"worker": {Workflow: workQueueTestWorkerWorkflow}}},
		workqueue.ObservationOperation{Kind: "Observation", ObservationID: "o1", Resource: *gate("pull_request", "12"), Condition: "merged", State: "waiting"})
	state := log.state()

	report := workQueueStats(state, "alternate")
	require.Equal(t, "alternate", report.Branch)
	require.Len(t, report.Pools, 2)
	pool := report.Pools[0]
	require.Equal(t, "default", pool.Pool)
	require.Equal(t, 9, pool.WorkByState["available"])
	require.Equal(t, 1, pool.WorkByState["cancelled"])
	require.Equal(t, 5, pool.Eligible)
	require.Equal(t, []string{urgent.WorkID, first.WorkID, second.WorkID}, pool.NextEligible, "priority first, then FIFO, capped at three")
	require.Equal(t, map[string]int{"dependency_pending": 1, "dependency_failed": 1, "external_unobserved": 1, "external_waiting": 1}, pool.Blocked)
	require.Equal(t, "other", report.Pools[1].Pool)
	require.Equal(t, []string{elsewhere.WorkID}, report.Pools[1].NextEligible)
	text := workQueueStatsText(report)
	require.Contains(t, text, "blocked=dependency_failed=1,dependency_pending=1,external_unobserved=1,external_waiting=1")
	require.Contains(t, text, "pool: other available=1 eligible=1 claimed=0 open_dispatches=0 next="+elsewhere.WorkID)

	for _, test := range []struct {
		work      workqueue.WorkDefinition
		reason    string
		reference string
	}{
		{urgent, "ready", "None"},
		{child, "dependency_pending", "Work " + workQueueText(first.WorkID) + ": available"},
		{orphan, "dependency_failed", "Work " + workQueueText(doomed.WorkID) + ": cancelled"},
		{unobserved, "external_unobserved", "requires completed (observed unobserved)"},
		{waiting, "external_waiting", "requires merged (observed waiting)"},
		{doomed, "state_cancelled", "None"},
	} {
		inspection, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "work", ID: test.work.WorkID, WorkID: test.work.WorkID})
		require.NoError(t, err)
		require.Equal(t, test.reason, inspection.Work.Reason, test.work.NodeKey)
		require.Equal(t, test.reason == "ready", inspection.Work.Ready, test.work.NodeKey)
		require.Contains(t, inspection.Details, "scheduling: "+test.reason)
		require.Contains(t, inspection.Details, test.reference)
	}

	empty := workQueueStats(workqueue.NewProjection(), "work-queue")
	require.NotNil(t, empty.Pools)
	require.Empty(t, empty.Pools)
	require.Equal(t, map[string]int{"open": 0, "completed": 0, "cancelled": 0}, empty.ClaimsByState)
	require.Contains(t, workQueueStatsText(empty), "Work: 0; available: 0")
}

func TestWorkQueueDiagnosticInspectionAndRows(t *testing.T) {
	log, ids := workQueueMixedFixture(t)
	state := log.state()

	running, err := workQueueInspect(state, "alternate", workQueueRow{Kind: "work", ID: ids["running"], WorkID: ids["running"]})
	require.NoError(t, err)
	require.Equal(t, "alternate", running.Branch)
	require.Equal(t, "claimed", running.Work.State)
	require.False(t, running.Work.Ready)
	require.Equal(t, "state_claimed", running.Work.Reason)
	require.NotNil(t, running.Work.DependsOn)
	require.NotNil(t, running.Claim)
	require.True(t, running.Claim.Current)
	require.Equal(t, "open", running.Claim.State)
	require.Equal(t, "h1", running.Claim.Handle)
	require.Equal(t, ids["running_dispatch"], running.Claim.DispatchID)
	require.NotNil(t, running.Dispatch)
	require.Equal(t, "bound", running.Dispatch.State)
	require.Equal(t, &workqueue.RunBinding{RunID: "202", RunAttempt: 1}, running.Dispatch.Run)
	require.Equal(t, workQueueTestWorkerWorkflow, running.Dispatch.Workflow)
	require.Equal(t, "worker", running.Dispatch.WorkerProfile)
	require.Equal(t, []workQueueAssignmentMember{{ClaimID: running.Claim.ClaimID, WorkID: ids["running"], Handle: "h1", State: "open"}}, running.Dispatch.Claims)
	require.Contains(t, running.Details, "Run: "+workQueueText("202")+" attempt 1")
	require.Contains(t, running.Details, "Workflow: "+workQueueText(workQueueTestWorkerWorkflow))
	assertWorkDiagnosticsPrivate(t, running)

	done, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "work", ID: ids["done"], WorkID: ids["done"]})
	require.NoError(t, err)
	require.Equal(t, "completed", done.Work.State)
	require.NotEmpty(t, done.Work.CompletionID)
	require.JSONEq(t, "[]", string(done.Work.Effects))
	require.Equal(t, "completed", done.Claim.State)
	require.Equal(t, done.Work.CompletionID, done.Claim.TerminalCommitID)
	require.Equal(t, "closed", done.Dispatch.State)

	dropped, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "work", ID: ids["dropped"], WorkID: ids["dropped"]})
	require.NoError(t, err)
	require.Equal(t, "operator", dropped.Work.CancellationReason)
	require.NotEmpty(t, dropped.Work.CancellationCommitID)
	require.Nil(t, dropped.Claim)
	require.Nil(t, dropped.Dispatch)

	graph, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "graph", ID: "graph", GraphID: "graph"})
	require.NoError(t, err)
	require.Nil(t, graph.Work)
	require.Contains(t, graph.Details, "Select a Work or Claim")
	_, err = workQueueInspect(state, "work-queue", workQueueRow{Kind: "work", ID: "missing", WorkID: "missing"})
	require.ErrorContains(t, err, "work_missing")
	_, err = workQueueInspect(state, "work-queue", workQueueRow{Kind: "claim", ID: "missing", WorkID: ids["running"]})
	require.ErrorContains(t, err, "claim_missing")

	rows := workQueueRows(state, workQueueFilter{}, nil)
	for _, row := range rows {
		switch row.Kind {
		case "work":
			require.Equal(t, "default", row.Pool)
			require.Equal(t, "worker", row.WorkerProfile)
			require.NotEmpty(t, row.NodeKey)
			require.NotNil(t, row.Attempts)
		case "claim":
			require.Equal(t, "default", row.Pool)
			require.Equal(t, "h1", row.Handle)
			if row.WorkID == ids["running"] {
				require.Equal(t, "bound", row.DispatchState)
				require.Equal(t, &workqueue.RunBinding{RunID: "202", RunAttempt: 1}, row.Run)
				require.Contains(t, row.Text, "run="+workQueueText("202/1"))
			}
		}
	}
	assertWorkDiagnosticsPrivate(t, rows)

	// A released Claim stays inspectable as history; its Work returns to the pool.
	log.release(ids["running_dispatch"], "operator")
	state = log.state()
	historical, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "claim", ID: running.Claim.ClaimID, WorkID: ids["running"]})
	require.NoError(t, err)
	require.False(t, historical.Claim.Current)
	require.Equal(t, "cancelled", historical.Claim.State)
	require.Equal(t, "operator", historical.Claim.CancellationReason)
	require.NotEmpty(t, historical.Claim.TerminalCommitID)
	require.Equal(t, "closed", historical.Dispatch.State)
	require.Equal(t, "available", historical.Work.State)
	require.Equal(t, 1, historical.Work.Attempts)
	require.True(t, historical.Work.Ready)
	require.Empty(t, historical.Work.ClaimID)
	current, err := workQueueInspect(state, "work-queue", workQueueRow{Kind: "work", ID: ids["running"], WorkID: ids["running"]})
	require.NoError(t, err)
	require.Nil(t, current.Claim, "released Work has no current Claim")
}

func TestWorkQueueDiagnosticCheckpointPreservesDiagnostics(t *testing.T) {
	log, ids := workQueueMixedFixture(t)
	state := log.state()
	checkpoint, err := workqueue.CheckpointCommit(state, workQueueTestAdmin, 99000)
	require.NoError(t, err)
	restored, err := workqueue.Replay([]workqueue.QueueCommit{checkpoint})
	require.NoError(t, err)

	before, after := workQueueStats(state, "work-queue"), workQueueStats(restored, "work-queue")
	require.Equal(t, before.ClaimsByState, after.ClaimsByState)
	require.Equal(t, before.DispatchesByState, after.DispatchesByState)
	require.Equal(t, before.WorkCancellationReasons, after.WorkCancellationReasons)
	require.Equal(t, before.ClaimCancellationReasons, after.ClaimCancellationReasons)
	require.Equal(t, before.Pools, after.Pools)
	for _, key := range []string{"done", "running", "waiting", "dropped"} {
		row := workQueueRow{Kind: "work", ID: ids[key], WorkID: ids[key]}
		original, err := workQueueInspect(state, "work-queue", row)
		require.NoError(t, err)
		compacted, err := workQueueInspect(restored, "work-queue", row)
		require.NoError(t, err)
		require.Equal(t, original.Work, compacted.Work, key)
		require.Equal(t, original.Claim, compacted.Claim, key)
		require.Equal(t, original.Dispatch, compacted.Dispatch, key)
	}
}

func TestWorkQueueDiagnosticCommandsAreReadOnly(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	log, ids := workQueueMixedFixture(t)
	state := log.state()
	data := log.jsonl()
	queue := newWorkQueueGitRemote(t, data)
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("diagnostic command unexpectedly contacted the API: %s %s", r.Method, r.URL.Path)
		return nil, nil
	})
	claimID := state.Works[ids["running"]].ClaimID
	for _, args := range [][]string{{"stats"}, {"state", "--limit", "2"}, {"inspect", "--work-id", ids["running"]}, {"inspect", "--claim-id", claimID}} {
		command := NewWorkCommand()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(io.Discard)
		command.SetArgs(append([]string{"--repo", workQueueTestRepository, "--json"}, args...))
		require.NoError(t, command.Execute(), "%v: %s", args, output.String())
		var result map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		for _, required := range []string{"status", "tip", "repository", "branch"} {
			require.Contains(t, result, required, "%v omitted snapshot field", args)
		}
		require.JSONEq(t, `"`+state.Tip+`"`, string(result["tip"]))
		switch args[0] {
		case "stats":
			var counters workqueue.Stats
			require.NoError(t, json.Unmarshal(output.Bytes(), &counters))
			require.Equal(t, state.Stats, counters, "stats JSON keeps the root counters")
		case "inspect":
			for _, required := range []string{"selection", "details", "work", "claim", "dispatch"} {
				require.Contains(t, result, required)
			}
		case "state":
			var page workQueuePage
			require.NoError(t, json.Unmarshal(output.Bytes(), &page))
			require.Len(t, page.Rows, 2)
			require.NotNil(t, page.NextOffset)
			require.Equal(t, 2, *page.NextOffset)
		}
		assertWorkDiagnosticsPrivate(t, result)
	}
	command := NewWorkCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--repo", workQueueTestRepository, "stats"})
	require.NoError(t, command.Execute())
	require.Contains(t, output.String(), "Work: 4; available: 1")
	require.Zero(t, queue.pushes(t))
	require.Equal(t, data, queue.read(t), "read-only diagnostics changed queue state")
}
