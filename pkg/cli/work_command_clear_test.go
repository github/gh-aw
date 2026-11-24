package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

// workClearEnvironment isolates clear from CI detection, the checkout's aw.json
// (max_attempts defaults to 3) and the REST API: v4 clear only uses the Git ledger.
func workClearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS", "COPILOT_AGENT_SESSION_ID"} {
		t.Setenv(name, "")
	}
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	t.Chdir(t.TempDir())
	original := workClearConfirmAction
	t.Cleanup(func() { workClearConfirmAction = original })
	workClearConfirmAction = func(string, string, string) (bool, error) { return true, nil }
	transport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = transport })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Errorf("clear made an API request: %s %s", r.Method, r.URL.Path)
		return nil, fmt.Errorf("unexpected API request %s", r.URL.Path)
	})
}

func workClearExecute(t *testing.T, extra ...string) (workClearResult, error) {
	t.Helper()
	cmd := NewWorkCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{"--repo", workQueueTestRepository, "--json", "clear"}, extra...))
	err := cmd.Execute()
	var result workClearResult
	if err == nil {
		require.NoError(t, json.Unmarshal(output.Bytes(), &result), "clear output: %s", output.String())
	}
	return result, err
}

func workClearRead(t *testing.T, queue *workQueueGitRemote) workqueue.Projection {
	t.Helper()
	state, err := workqueue.ReplayLog([]byte(queue.read(t)))
	require.NoError(t, err)
	return state
}

// workClearTail returns the commits appended after prefix.
func workClearTail(t *testing.T, queue *workQueueGitRemote, prefix string) []workqueue.QueueCommit {
	t.Helper()
	ledger := queue.read(t)
	require.True(t, strings.HasPrefix(ledger, prefix), "clear rewrote ledger history")
	commits, err := workqueue.Parse([]byte(ledger))
	require.NoError(t, err)
	return commits[strings.Count(prefix, "\n"):]
}

func workClearOperationKinds(t *testing.T, commit workqueue.QueueCommit) []string {
	t.Helper()
	kinds := make([]string, 0, len(commit.Operations))
	for _, operation := range commit.Operations {
		var value struct {
			Kind string `json:"kind"`
		}
		require.NoError(t, json.Unmarshal(operation, &value))
		kinds = append(kinds, value.Kind)
	}
	return kinds
}

// workClearFixture extends the mixed fixture with a reserved dispatch (Work
// "waiting") and one more available Work ("queued").
func workClearFixture(t *testing.T) (*workQueueTestLog, map[string]string) {
	t.Helper()
	log, ids := workQueueMixedFixture(t)
	ids["reserved_dispatch"] = log.dispatch(1)[0]
	ids["queued"] = log.submit(log.work("queued", nil))[0]
	return log, ids
}

func TestWorkClearReleasesDispatchesThenCancelsUnfinishedWork(t *testing.T) {
	workClearEnvironment(t)
	log, ids := workClearFixture(t)
	ledger := log.jsonl()
	before := log.state()
	require.Equal(t, "claimed", before.Works[ids["waiting"]].State)
	queue := newWorkQueueGitRemote(t, ledger)

	preview, err := workClearExecute(t, "--dry-run")
	require.NoError(t, err)
	require.Equal(t, "dry_run", preview.Status)
	require.Equal(t, before.Tip, preview.SnapshotTip)
	require.Equal(t, []string{ids["running_dispatch"], ids["reserved_dispatch"]}, preview.DispatchIDs)
	require.ElementsMatch(t, []string{ids["running"], ids["waiting"], ids["queued"]}, preview.WorkIDs)
	require.Equal(t, 1, preview.CompletedRetained)
	require.Empty(t, preview.Requests)
	require.Zero(t, queue.pushes(t), "dry run wrote the ledger")
	require.Equal(t, ledger, queue.read(t))

	result, err := workClearExecute(t)
	require.NoError(t, err)
	require.Equal(t, "cleared", result.Status)
	require.Equal(t, preview.DispatchIDs, result.DispatchIDs)
	require.Equal(t, preview.WorkIDs, result.WorkIDs)
	require.Len(t, result.Requests, 3)
	require.Equal(t, 3, queue.pushes(t))

	appended := workClearTail(t, queue, ledger)
	require.Len(t, appended, 3)
	for index, dispatchID := range preview.DispatchIDs {
		commit := appended[index]
		require.Equal(t, "release", commit.Request.Kind)
		require.Equal(t, workqueue.Actor{Role: "administrator", Repository: workQueueTestRepository}, commit.Actor)
		require.Equal(t, []string{"Release"}, workClearOperationKinds(t, commit), "attempt 1 of 3 must not exhaust Work")
		var release workqueue.ReleaseOperation
		require.NoError(t, json.Unmarshal(commit.Operations[0], &release))
		require.Equal(t, workqueue.ReleaseOperation{Kind: "Release", DispatchID: dispatchID, Reason: "operator"}, release)
	}
	require.Equal(t, "cancel_work", appended[2].Request.Kind)
	require.Equal(t, []string{"WorkCancellation", "WorkCancellation", "WorkCancellation"}, workClearOperationKinds(t, appended[2]))
	require.Equal(t, []string{appended[0].Request.ID, appended[1].Request.ID, appended[2].Request.ID}, result.Requests)

	after := workClearRead(t, queue)
	require.Equal(t, 0, after.Stats.Available)
	require.Equal(t, 0, after.Stats.Claimed)
	require.Equal(t, 1, after.Stats.Completed)
	require.Equal(t, 4, after.Stats.Cancelled)
	for _, id := range before.DispatchOrder() {
		require.Equal(t, "closed", after.Dispatches[id].State, "dispatch %s", id)
	}
	for _, key := range []string{"running", "waiting", "queued"} {
		work := after.Works[ids[key]]
		require.Equal(t, "cancelled", work.State, key)
		require.Equal(t, "operator", work.CancellationReason, key)
		require.Equal(t, before.Works[ids[key]].WorkDefinition, work.WorkDefinition, key)
	}
	require.Equal(t, 1, after.Works[ids["running"]].Attempts, "released Work records its attempt")
	require.Equal(t, before.Works[ids["done"]], after.Works[ids["done"]], "completed Work and its effects are retained")
	require.Equal(t, before.Works[ids["dropped"]], after.Works[ids["dropped"]])
	require.Equal(t, "cancelled", after.Claims[before.Works[ids["running"]].ClaimID].State)

	again, err := workClearExecute(t)
	require.NoError(t, err)
	require.Equal(t, "no_work", again.Status)
	require.Empty(t, again.DispatchIDs)
	require.Empty(t, again.WorkIDs)
	require.Equal(t, 3, queue.pushes(t), "a cleared queue was written again")
}

func TestWorkClearCancelsReleasedWorkAtMaxAttempts(t *testing.T) {
	workClearEnvironment(t)
	workSettingsCheckout(t, `{"work_queue":{"pools":{"default":{"retry":{"max_attempts":1}}}}}`)
	log, ids := workQueueMixedFixture(t)
	ledger := log.jsonl()
	queue := newWorkQueueGitRemote(t, ledger)

	result, err := workClearExecute(t)
	require.NoError(t, err)
	require.Equal(t, "cleared", result.Status)
	appended := workClearTail(t, queue, ledger)
	require.Len(t, appended, 2)
	require.Equal(t, []string{"Release", "WorkCancellation"}, workClearOperationKinds(t, appended[0]))
	require.Equal(t, []string{"WorkCancellation"}, workClearOperationKinds(t, appended[1]), "exhausted Work is not cancelled twice")

	after := workClearRead(t, queue)
	require.Equal(t, "attempts_exhausted", after.Works[ids["running"]].CancellationReason)
	require.Equal(t, "operator", after.Works[ids["waiting"]].CancellationReason)
	require.Equal(t, "completed", after.Works[ids["done"]].State)
}

func TestWorkClearRejectsCIWithoutIO(t *testing.T) {
	for _, name := range []string{"CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS", "COPILOT_AGENT_SESSION_ID"} {
		for _, extra := range [][]string{nil, {"--dry-run"}} {
			t.Run(name+strings.Join(extra, ""), func(t *testing.T) {
				workClearEnvironment(t)
				// Without credentials any ledger read fails with a different error.
				t.Setenv("GH_TOKEN", "")
				t.Setenv(name, "true")
				workClearConfirmAction = func(string, string, string) (bool, error) {
					t.Fatal("CI refusal prompted")
					return false, nil
				}
				_, err := workClearExecute(t, extra...)
				require.ErrorContains(t, err, "clear_ci_forbidden")
			})
		}
	}
}

func TestWorkClearNoopAndRefusals(t *testing.T) {
	settled := func(t *testing.T) string {
		log, ids := workQueueMixedFixture(t)
		log.release(ids["running_dispatch"], "operator")
		state := log.state()
		available := []string{}
		for id, work := range state.Works {
			if work.State == "available" {
				available = append(available, id)
			}
		}
		slices.Sort(available)
		log.cancel(available...)
		return log.jsonl()
	}
	for _, tc := range []struct {
		name, code string
		ledger     func(*testing.T) string
	}{
		{name: "missing branch", ledger: func(*testing.T) string { return "" }},
		{name: "only terminal Work", ledger: settled},
		{name: "malformed", ledger: func(*testing.T) string { return "not a valid ledger\n" }},
		{name: "v3 ledger", code: "unsupported_protocol", ledger: func(*testing.T) string { return `{"previous":null,"version":3}` + "\n" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workClearEnvironment(t)
			workClearConfirmAction = func(string, string, string) (bool, error) {
				t.Fatal("no-op or refused clear prompted")
				return false, nil
			}
			ledger := tc.ledger(t)
			queue := newWorkQueueGitRemote(t, ledger)
			result, err := workClearExecute(t)
			switch tc.name {
			case "missing branch", "only terminal Work":
				require.NoError(t, err)
				require.Equal(t, "no_work", result.Status)
				require.Empty(t, result.DispatchIDs)
				require.Empty(t, result.WorkIDs)
				require.Empty(t, result.Requests)
			default:
				require.Error(t, err)
				if tc.code != "" {
					require.ErrorContains(t, err, tc.code)
				}
			}
			require.Zero(t, queue.pushes(t))
			require.Equal(t, ledger, queue.read(t))
		})
	}
}

func TestWorkClearChunksCancellationsAtOperationLimit(t *testing.T) {
	workClearEnvironment(t)
	log := newWorkQueueTestLog(t)
	total := workqueue.OperationsPerCommit + 1
	works := make([]workqueue.WorkDefinition, 0, total)
	for index := range total {
		works = append(works, log.work(fmt.Sprintf("node-%03d", index), nil))
	}
	log.submit(works[:workqueue.OperationsPerCommit]...)
	log.submit(works[workqueue.OperationsPerCommit:]...)
	ledger := log.jsonl()
	queue := newWorkQueueGitRemote(t, ledger)

	result, err := workClearExecute(t)
	require.NoError(t, err)
	require.Len(t, result.WorkIDs, total)
	require.Len(t, result.Requests, 2)
	appended := workClearTail(t, queue, ledger)
	require.Len(t, appended, 2)
	require.Len(t, appended[0].Operations, workqueue.OperationsPerCommit)
	require.Len(t, appended[1].Operations, 1)
	after := workClearRead(t, queue)
	require.Equal(t, total, after.Stats.Cancelled)
	require.Zero(t, after.Stats.Available)
}

func TestWorkClearSkipsChangesAfterPreview(t *testing.T) {
	workClearEnvironment(t)
	log, ids := workQueueMixedFixture(t)
	ids["spare"] = log.submit(log.work("spare", nil))[0]
	ledger := log.jsonl()
	queue := newWorkQueueGitRemote(t, ledger)
	var late, lateDispatch string
	workClearConfirmAction = func(string, string, string) (bool, error) {
		// While the operator reads the preview: the running worker completes,
		// a dispatcher claims "waiting", and a producer admits new Work.
		log.finish(ids["running_dispatch"], workQueueTestWorker("202"))
		lateDispatch = log.dispatch(1)[0]
		late = log.submit(log.work("late", nil))[0]
		queue.write(t, log.jsonl())
		return true, nil
	}
	result, err := workClearExecute(t)
	require.NoError(t, err)
	require.Equal(t, []string{ids["running_dispatch"]}, result.DispatchIDs)
	require.ElementsMatch(t, []string{ids["running"], ids["waiting"], ids["spare"]}, result.WorkIDs)
	require.Len(t, result.Requests, 1, "only the still-available preview Work is cancelled")
	require.Equal(t, 1, queue.pushes(t))

	after := workClearRead(t, queue)
	require.Equal(t, "completed", after.Works[ids["running"]].State, "concurrent completion was overridden")
	require.Equal(t, "claimed", after.Works[ids["waiting"]].State, "Work claimed after the preview was cancelled")
	require.Equal(t, "reserved", after.Dispatches[lateDispatch].State, "dispatch created after the preview was released")
	require.Equal(t, "cancelled", after.Works[ids["spare"]].State)
	require.Equal(t, "available", after.Works[late].State, "Work admitted after the preview was cancelled")
}
