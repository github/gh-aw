package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

func workObsoleteFixture(t *testing.T) ([]workqueue.QueueCommit, []string) {
	t.Helper()
	actor := workqueue.Actor{Role: "administrator", Principal: "1001", Repository: "owner/repo"}
	policy := workqueue.DefaultPolicy("", actor.Repository)
	policy.Authorization = "aw"
	policy.Producers = map[string]workqueue.ProducerRule{}
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Ref, profile.LogicalContract = strings.Repeat("a", 40), strings.Repeat("a", 64)
	pool.Profiles["default"] = profile
	other := profile
	other.Workflow = ".github/workflows/other.lock.yml"
	pool.Profiles["other"] = other
	policy.Pools["default"] = pool
	genesis, err := workqueue.Genesis(actor, policy, "installed", "genesis", 1000)
	if err != nil {
		t.Fatal(err)
	}
	nodes := []workqueue.WorkDefinition{}
	ids := []string{}
	for _, key := range []string{"old-one", "old-two", "pinned", "compatible", "expired"} {
		work, err := workqueue.NewWork([]byte(`{"task":"PAYLOAD-MUST-STAY-PRIVATE"}`), "backlog", key, "default", policy, 2000)
		if err != nil {
			t.Fatal(err)
		}
		switch key {
		case "pinned":
			work.ExecutionRef = profile.Ref
		case "compatible":
			work.WorkerProfile = "other"
		case "expired":
			deadline := int64(9000)
			work.ExpiresAt = &deadline
		}
		nodes, ids = append(nodes, work), append(ids, work.WorkID)
	}
	commits := workClearAppend(t, []workqueue.QueueCommit{genesis}, actor, "backlog", "submit", workqueue.SubmitParameters{Nodes: nodes}, 2000)
	profile.Ref, profile.LogicalContract = strings.Repeat("b", 40), strings.Repeat("b", 64)
	operation := workqueue.DeploymentOperation{
		Kind: "Deployment", Pool: "default", WorkerProfile: "default",
		ExpectedRef: strings.Repeat("a", 40), ExpectedContract: strings.Repeat("a", 64),
		Profile: profile, Available: true, Reason: "compiler_deployment",
	}
	commits = workClearAppend(t, commits, actor, "deployment", "deployment", workqueue.OperationsParameters{
		Operations: []workqueue.Operation{mustWorkQueueOperation(t, operation)},
	}, 3000)
	return commits, ids
}

func workObsoleteExecute(t *testing.T, extra ...string) (workObsoleteResult, error) {
	t.Helper()
	cmd := NewWorkCommand()
	parent := &cobra.Command{Use: "gh-aw"}
	parent.AddCommand(cmd)
	var output bytes.Buffer
	parent.SetOut(&output)
	parent.SetErr(io.Discard)
	parent.SetArgs(append([]string{"work", "--repo", "owner/repo", "--json", "cancel-work", "--obsolete", "--request-id", "obsolete-batch"}, extra...))
	err := parent.Execute()
	var result workObsoleteResult
	if err == nil {
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatalf("invalid obsolete output: %v (%s)", err, output.String())
		}
		if strings.Contains(output.String(), "PAYLOAD-MUST") {
			t.Fatal("obsolete command leaked payload")
		}
	}
	return result, err
}

func TestWorkCancelObsoleteDryRunAndPublication(t *testing.T) {
	workClearRoleEnvironment(t, "admin")
	commits, ids := workObsoleteFixture(t)
	data, err := workqueue.Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	queue := newWorkQueueGitRemote(t, string(data))
	initial := queue.head(t)
	transport := http.DefaultTransport
	operatorReads := 0
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		operatorReads++
		return transport.RoundTrip(r)
	})
	preview, err := workObsoleteExecute(t, "--dry-run", "--limit", "1")
	if err != nil || preview.Status != "dry_run" || len(preview.WorkIDs) != 1 || preview.Publication != nil {
		t.Fatalf("obsolete preview failed: %+v %v", preview, err)
	}
	expected := slices.Clone(ids[:2])
	slices.Sort(expected)
	if preview.WorkIDs[0] != expected[0] || queue.head(t) != initial || queue.pushes(t) != 0 || operatorReads != 0 {
		t.Fatal("preview changed the ledger, authenticated operator authority, or selected outside the sorted bound")
	}
	result, err := workObsoleteExecute(t, "--limit", "1")
	if err != nil || result.Status != "cancelled" || result.Publication == nil || !result.Publication.Changed {
		t.Fatalf("obsolete publication failed: %+v %v", result, err)
	}
	next, err := workqueue.Parse([]byte(queue.read(t)))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := workqueue.ReplayAt(commits, time.Now().UnixMilli())
	after, err := workqueue.Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	selected := after.Works[expected[0]]
	if selected.State != "cancelled" || selected.CancellationReason != workObsoleteReason || queue.pushes(t) != 1 {
		t.Fatal("checked cancellation was not durable")
	}
	for _, id := range ids {
		if id != expected[0] && !workObsoleteSameJSON(before.Works[id], after.Works[id]) {
			t.Fatalf("unselected or protected Work changed: %s", id)
		}
	}
	if !workObsoleteSameJSON(before.Policy, after.Policy) || before.PolicyEpoch != after.PolicyEpoch ||
		!workObsoleteSameJSON(before.Clocks, after.Clocks) || !workObsoleteSameJSON(before.Claims, after.Claims) ||
		!workObsoleteSameJSON(before.Dispatches, after.Dispatches) || !workObsoleteSameJSON(before.Deployments, after.Deployments) {
		t.Fatal("retirement changed authority, reservations or economics")
	}
	head := queue.head(t)
	for _, args := range [][]string{{"--limit", "1"}, {"--limit", "1", "--dry-run"}} {
		retry, err := workObsoleteExecute(t, args...)
		if err != nil || retry.Status != "already_committed" || !slices.Equal(retry.WorkIDs, result.WorkIDs) ||
			queue.head(t) != head || queue.pushes(t) != 1 {
			t.Fatalf("retry swept newly eligible Work or repeated publication: %+v %v", retry, err)
		}
	}
}

func workObsoleteSameJSON(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func TestWorkCancelObsoleteAuthorityAndFreshPublication(t *testing.T) {
	for _, scenario := range []string{"writer", "maintainer", "compatible", "unavailable", "claimed"} {
		t.Run(scenario, func(t *testing.T) {
			role := "admin"
			switch scenario {
			case "writer":
				role = "write"
			case "maintainer":
				role = "maintain"
			}
			workClearRoleEnvironment(t, role)
			commits, ids := workObsoleteFixture(t)
			data, _ := workqueue.Serialize(commits)
			queue := newWorkQueueGitRemote(t, string(data))
			transport := http.DefaultTransport
			changed := false
			http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/user" && !changed && role == "admin" {
					changed = true
					state, _ := workqueue.Replay(commits)
					actor := state.Requests["deployment"].Actor
					deployment := state.Deployments["default"]["default"]
					profile := deployment.Revisions[deployment.CurrentRef].Profile
					incompatible := profile
					available := scenario != "unavailable"
					if scenario != "unavailable" {
						profile = deployment.Revisions[strings.Repeat("a", 40)].Profile
					}
					op := workqueue.DeploymentOperation{
						Kind: "Deployment", Pool: "default", WorkerProfile: "default", ExpectedRef: deployment.CurrentRef,
						ExpectedContract: deployment.CurrentContract, Profile: profile, Available: available, Reason: "compiler_deployment",
					}
					commits = workClearAppend(t, commits, actor, "route-race", "deployment", workqueue.OperationsParameters{
						Operations: []workqueue.Operation{mustWorkQueueOperation(t, op)},
					}, 5000)
					if scenario == "claimed" {
						commits = workClearAppend(t, commits, actor, "grant-race", "dispatch_next", workqueue.DispatchParameters{
							Pool: "default", MaxClaims: 256, MaxDispatches: 256, MaxBytes: 48 << 10,
						}, 6000)
						op.ExpectedRef, op.ExpectedContract = profile.Ref, profile.LogicalContract
						op.Profile = incompatible
						commits = workClearAppend(t, commits, actor, "restore-incompatible-route", "deployment", workqueue.OperationsParameters{
							Operations: []workqueue.Operation{mustWorkQueueOperation(t, op)},
						}, 7000)
						latest, err := workqueue.Replay(commits)
						if err != nil {
							t.Fatal(err)
						}
						route := latest.Deployments["default"]["default"]
						revision := route.Revisions[route.CurrentRef]
						for _, id := range ids[:2] {
							work := latest.Works[id]
							claim := latest.Claims[work.ClaimID]
							if work.State != "claimed" || claim == nil || claim.State != "open" || claim.WorkID != id ||
								work.ExecutionRef != "" || work.Barrier != "none" || !revision.Available ||
								route.CurrentRef != strings.Repeat("b", 40) || revision.Profile.LogicalContract == work.AdmissionContract {
								t.Fatalf("claimed race did not isolate ownership from route compatibility: %+v", work)
							}
						}
					}
					data, _ := workqueue.Serialize(commits)
					queue.write(t, string(data))
				}
				return transport.RoundTrip(r)
			})
			_, err := workObsoleteExecute(t)
			if err == nil || queue.pushes(t) != 0 {
				t.Fatalf("unauthorized or stale obsolete batch published: %v", err)
			}
			code := "deployment_conflict:"
			if role != "admin" {
				code = "actor_unauthorized:"
			}
			if !strings.Contains(err.Error(), code) {
				t.Fatalf("expected actionable %s, got %v", code, err)
			}
			if scenario == "claimed" {
				expected, err := workqueue.Serialize(commits)
				if err != nil || queue.read(t) != string(expected) || workClearRead(t, queue).Stats.Cancelled != 0 {
					t.Fatal("ownership rejection changed the raced ledger")
				}
			}
		})
	}
}

func TestWorkCancelObsoleteNoWorkAndClosedFlags(t *testing.T) {
	workClearRoleEnvironment(t, "write")
	state, commits, _ := workOperatorFixture(t, false)
	data, _ := workqueue.Serialize(commits)
	queue := newWorkQueueGitRemote(t, string(data))
	result, err := workObsoleteExecute(t)
	if err != nil || result.Status != "no_work" || len(result.WorkIDs) != 0 || queue.pushes(t) != 0 {
		t.Fatalf("legacy compatible queue did not remain a no-op: %+v %v", result, err)
	}
	for _, extra := range [][]string{
		{"--work-id", "exact"}, {"--reason", workObsoleteReason}, {"--limit", "0"}, {"--limit", "257"},
		{"--all"}, {"--force"}, {"--execution-ref", "pin"},
	} {
		if _, err := workObsoleteExecute(t, extra...); err == nil {
			t.Fatalf("unsafe obsolete flags accepted: %v", extra)
		}
	}
	for _, args := range [][]string{{"cancel-work", "--dry-run"}, {"cancel-work", "--limit", "1"}, {"cancel", "--obsolete"}} {
		cmd := NewWorkCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"--repo", "owner/repo"}, args...))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("obsolete-only flag silently ignored: %v", args)
		}
	}
	cmd := workCancelCommand()
	if len(cmd.Aliases) != 0 {
		t.Fatalf("cancel-work must not introduce aliases: %v", cmd.Aliases)
	}
	if _, _, err := workObsoleteIntent(state, 10000, 256, "submit"); err == nil {
		t.Fatal("obsolete request reused a noncancellation identity")
	}
}
