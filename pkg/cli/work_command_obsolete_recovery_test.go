package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
)

func workObsoleteWrite(t *testing.T, queue *workQueueGitRemote, commits []workqueue.QueueCommit) {
	t.Helper()
	data, err := workqueue.Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	queue.write(t, string(data))
}

func workObsoleteDeploy(t *testing.T, commits []workqueue.QueueCommit, id, revision string, at int64) []workqueue.QueueCommit {
	t.Helper()
	state, err := workqueue.Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	deployment := state.Deployments["default"]["default"]
	profile := deployment.Revisions[deployment.CurrentRef].Profile
	profile.Ref, profile.LogicalContract = strings.Repeat(revision, 40), strings.Repeat(revision, 64)
	return workClearAppend(t, commits, state.Requests["deployment"].Actor, id, "deployment", workqueue.OperationsParameters{
		Operations: []workqueue.Operation{mustWorkQueueOperation(t, workqueue.DeploymentOperation{
			Kind: "Deployment", Pool: "default", WorkerProfile: "default",
			ExpectedRef: deployment.CurrentRef, ExpectedContract: deployment.CurrentContract,
			Profile: profile, Available: true, Reason: "compiler_deployment",
		})},
	}, at)
}

func TestWorkCancelObsoleteRejectsReplacementLedger(t *testing.T) {
	for _, scenario := range []string{"original", "legacy_claimed", "legacy_pinned"} {
		for _, compact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/checkpoint_%t", scenario, compact), func(t *testing.T) {
				workClearRoleEnvironment(t, "admin")
				commits, ids := workObsoleteFixture(t)
				before, err := workqueue.Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
				actor := before.Requests["deployment"].Actor
				if scenario != "original" {
					operations := []workqueue.Operation{}
					for _, id := range ids[:2] {
						operations = append(operations, mustWorkQueueOperation(t, map[string]any{
							"kind": "WorkCancellation", "work_id": id, "reason": workObsoleteReason,
						}))
					}
					commits = workClearAppend(t, commits, actor, "obsolete-batch", "cancel_work",
						workqueue.OperationsParameters{Operations: operations}, 4000)
				}
				data, err := workqueue.Serialize(commits)
				if err != nil {
					t.Fatal(err)
				}
				queue := newWorkQueueGitRemote(t, string(data))
				branch := workqueue.Branch{Remote: "owner/repo", Name: workqueue.DefaultBranch}
				if compact {
					if _, err := branch.Compact(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				transport := http.DefaultTransport
				replaced, resetPushes := false, 0
				var replacement string
				http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/user" && !replaced {
						replaced = true
						selected, err := workqueue.Parse([]byte(queue.read(t)))
						if err != nil {
							t.Fatal(err)
						}
						workObsoleteWrite(t, queue, workClearPause(t, selected))
						if _, err := branch.HardClear(context.Background(), "hard-reset", "operator_reset", false); err != nil {
							t.Fatal(err)
						}
						reset, err := workqueue.Parse([]byte(queue.read(t)))
						if err != nil {
							t.Fatal(err)
						}
						at := reset[len(reset)-1].At
						reset = workClearAppend(t, reset, actor, "resume", "control", workqueue.OperationsParameters{
							Operations: []workqueue.Operation{
								mustWorkQueueOperation(t, map[string]any{"kind": "Control", "control": "admission_paused", "value": false, "reason": "resume"}),
								mustWorkQueueOperation(t, map[string]any{"kind": "Control", "control": "grants_paused", "value": false, "reason": "resume"}),
							},
						}, at)
						state, err := workqueue.Replay(reset)
						if err != nil {
							t.Fatal(err)
						}
						nodes := []workqueue.WorkDefinition{}
						for _, id := range ids[:2] {
							original := before.Works[id]
							node, err := workqueue.NewWork(original.Payload, original.GraphID, original.NodeKey, original.Pool, *state.Policy, at)
							if err != nil || node.WorkID != id {
								t.Fatalf("reset did not reuse original Work identity: %v", err)
							}
							if scenario == "legacy_pinned" {
								node.ExecutionRef = strings.Repeat("b", 40)
							}
							nodes = append(nodes, node)
						}
						reset = workClearAppend(t, reset, actor, "backlog", "submit", workqueue.SubmitParameters{Nodes: nodes}, at)
						if scenario == "legacy_claimed" {
							reset = workClearAppend(t, reset, actor, "replacement-grant", "dispatch_next", workqueue.DispatchParameters{
								Pool: "default", MaxClaims: 256, MaxDispatches: 256, MaxBytes: 48 << 10,
							}, at+1)
						}
						// The replacement route is incompatible too: a checked fresh request would
						// otherwise cancel the same IDs, while legacy recovery also lacks ownership guards.
						deployment := state.Deployments["default"]["default"]
						profile := deployment.Revisions[deployment.CurrentRef].Profile
						profile.Ref, profile.LogicalContract = strings.Repeat("c", 40), strings.Repeat("c", 64)
						reset = workClearAppend(t, reset, actor, "replacement-route", "deployment", workqueue.OperationsParameters{
							Operations: []workqueue.Operation{mustWorkQueueOperation(t, workqueue.DeploymentOperation{
								Kind: "Deployment", Pool: "default", WorkerProfile: "default",
								ExpectedRef: deployment.CurrentRef, ExpectedContract: deployment.CurrentContract,
								Profile: profile, Available: true, Reason: "compiler_deployment",
							})},
						}, at+2)
						workObsoleteWrite(t, queue, reset)
						if compact {
							if _, err := branch.Compact(context.Background()); err != nil {
								t.Fatal(err)
							}
						}
						replacement, resetPushes = queue.read(t), queue.pushes(t)
					}
					return transport.RoundTrip(r)
				})
				_, err = workObsoleteExecute(t)
				if err == nil || !strings.Contains(err.Error(), "ledger_nonextending:") {
					t.Fatalf("obsolete cancellation accepted replacement ledger: %v", err)
				}
				after := workClearRead(t, queue)
				if !replaced || queue.read(t) != replacement || queue.pushes(t) != resetPushes || after.Stats.Cancelled != 0 {
					t.Fatal("obsolete cancellation changed replacement ledger")
				}
				if _, ok := after.Requests["obsolete-batch"]; ok {
					t.Fatal("obsolete request was republished in replacement ledger")
				}
				for _, id := range ids[:2] {
					work := after.Works[id]
					expected := "available"
					if scenario == "legacy_claimed" {
						expected = "claimed"
						claim := after.Claims[work.ClaimID]
						if claim == nil || claim.State != "open" || claim.WorkID != id {
							t.Fatal("replacement did not retain effective ownership")
						}
					}
					if work.State != expected || work.AdmissionContract != strings.Repeat("b", 64) ||
						(scenario == "legacy_pinned" && work.ExecutionRef != strings.Repeat("b", 40)) {
						t.Fatalf("replacement Work was not protected: %+v", work)
					}
				}
			})
		}
	}
}

func TestWorkCancelObsoleteAcceptedRetryAfterPolicyLimitReduction(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			t.Run(fmt.Sprintf("legacy_%t/checkpoint_%t", legacy, compact), func(t *testing.T) {
				workClearRoleEnvironment(t, "admin")
				commits, _ := workObsoleteFixture(t)
				commits = workObsoleteDeploy(t, commits, "rollback-for-admission", "a", 3500)
				state, err := workqueue.Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
				actor := state.Requests["deployment"].Actor
				nodes := []workqueue.WorkDefinition{}
				for _, key := range []string{"third", "fourth"} {
					node, err := workqueue.NewWork([]byte(`{}`), "extra", key, "default", *state.Policy, 4000)
					if err != nil {
						t.Fatal(err)
					}
					nodes = append(nodes, node)
				}
				commits = workClearAppend(t, commits, actor, "extra", "submit", workqueue.SubmitParameters{Nodes: nodes}, 4000)
				commits = workObsoleteDeploy(t, commits, "restore-obsolete-route", "b", 4500)
				state, err = workqueue.Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
				params, targets, err := workObsoleteIntent(state, 10000, 4, "obsolete-batch")
				if err != nil || len(targets) != 4 {
					t.Fatalf("invalid original batch: %v %v", targets, err)
				}
				if legacy {
					for i, id := range targets {
						params.Operations[i] = mustWorkQueueOperation(t, map[string]any{
							"kind": "WorkCancellation", "work_id": id, "reason": workObsoleteReason,
						})
					}
				}
				original, err := workqueue.NewRequest("obsolete-batch", "cancel_work", actor, params)
				if err != nil {
					t.Fatal(err)
				}
				commits = workClearAppend(t, commits, actor, original.ID, original.Kind, params, 5000)
				drain := []workqueue.Operation{}
				for id := range state.Works {
					if !slices.Contains(targets, id) {
						drain = append(drain, mustWorkQueueOperation(t, map[string]any{
							"kind": "WorkCancellation", "work_id": id, "reason": "operator_cancelled",
						}))
					}
				}
				commits = workClearAppend(t, commits, actor, "drain-before-policy", "cancel_work",
					workqueue.OperationsParameters{Operations: drain}, 6000)
				policy := workqueue.FuturePolicy(state)
				policy.Limits.Operations = 3
				if err := workqueue.ValidatePolicy(policy); err != nil {
					t.Fatalf("lowered policy is invalid: %v", err)
				}
				commits = workClearAppend(t, commits, actor, "lower-limit", "policy", workqueue.OperationsParameters{
					Operations: []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{"kind": "Policy", "epoch": "lowered", "policy": policy})},
				}, 7000)
				state, err = workqueue.Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
				later := []workqueue.WorkDefinition{}
				for index := range 4 {
					node, err := workqueue.NewWork([]byte(`{}`), "later", fmt.Sprintf("unswept-%d", index), "default", workqueue.FuturePolicy(state), 8000)
					if err != nil {
						t.Fatal(err)
					}
					later = append(later, node)
				}
				commits = workClearAppend(t, commits, actor, "later-three", "submit", workqueue.SubmitParameters{Nodes: later[:3]}, 8000)
				commits = workClearAppend(t, commits, actor, "later-fourth", "submit", workqueue.SubmitParameters{Nodes: later[3:]}, 8000)
				commits = workObsoleteDeploy(t, commits, "later-obsolete-route", "c", 8500)
				data, err := workqueue.Serialize(commits)
				if err != nil {
					t.Fatal(err)
				}
				queue := newWorkQueueGitRemote(t, string(data))
				if compact {
					if _, err := (workqueue.Branch{Remote: "owner/repo", Name: workqueue.DefaultBranch}).Compact(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				before, head, pushes := queue.read(t), queue.head(t), queue.pushes(t)
				for _, args := range [][]string{{"--limit", "3", "--dry-run"}, {"--limit", "3"}} {
					if _, err := workObsoleteExecute(t, args...); err == nil || !strings.Contains(err.Error(), "--limit of at least 4") {
						t.Fatalf("undersized retry did not report required numeric limit: %v", err)
					}
				}
				for _, args := range [][]string{{"--limit", "4", "--dry-run"}, {"--limit", "4"}} {
					retry, err := workObsoleteExecute(t, args...)
					if err != nil || retry.Status != "already_committed" || !slices.Equal(retry.WorkIDs, targets) {
						t.Fatalf("lowered policy blocked accepted retry: %+v %v", retry, err)
					}
					if retry.Publication != nil && (retry.Publication.Changed || retry.Publication.Commit.Request.Fingerprint != original.Fingerprint) {
						t.Fatal("accepted retry republished or changed fingerprint")
					}
				}
				if queue.read(t) != before || queue.head(t) != head || queue.pushes(t) != pushes {
					t.Fatal("receipt recovery wrote to ledger")
				}
				after := workClearRead(t, queue)
				recovered, recoveredIDs, err := workObsoleteIntent(after, time.Now().UnixMilli(), 4, original.ID)
				if err != nil || !slices.EqualFunc(recovered.Operations, params.Operations, func(a, b workqueue.Operation) bool {
					return bytes.Equal(a, b)
				}) || !slices.Equal(recoveredIDs, targets) ||
					after.Requests[original.ID].Request.Fingerprint != original.Fingerprint {
					t.Fatalf("retry changed original intent or swept new Work: %v", err)
				}
				for _, node := range later {
					if after.Works[node.WorkID].State != "available" {
						t.Fatal("receipt recovery swept newly eligible Work")
					}
				}
				fresh, _, err := workObsoleteIntent(after, time.Now().UnixMilli(), 4, "fresh-batch")
				if err != nil || len(fresh.Operations) != 3 {
					t.Fatalf("fresh selection ignored lowered policy cap: %v", err)
				}
			})
		}
	}
}

func TestWorkCancelObsoleteSnapshotAllowsAppendAndCheckpoint(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkpoint_%t", compact), func(t *testing.T) {
			workClearRoleEnvironment(t, "admin")
			commits, ids := workObsoleteFixture(t)
			data, err := workqueue.Serialize(commits)
			if err != nil {
				t.Fatal(err)
			}
			queue := newWorkQueueGitRemote(t, string(data))
			branch := workqueue.Branch{Remote: "owner/repo", Name: workqueue.DefaultBranch}
			transport := http.DefaultTransport
			progressed := false
			var laterID string
			http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/user" && !progressed {
					progressed = true
					state, err := workqueue.Replay(commits)
					if err != nil {
						t.Fatal(err)
					}
					node, err := workqueue.NewWork([]byte(`{}`), "later", "unswept", "default", workqueue.FuturePolicy(state), 4000)
					if err != nil {
						t.Fatal(err)
					}
					laterID = node.WorkID
					commits = workClearAppend(t, commits, state.Requests["deployment"].Actor, "later", "submit", workqueue.SubmitParameters{Nodes: []workqueue.WorkDefinition{node}}, 4000)
					commits = workObsoleteDeploy(t, commits, "later-obsolete-route", "c", 4500)
					workObsoleteWrite(t, queue, commits)
					if compact {
						if _, err := branch.Compact(context.Background()); err != nil {
							t.Fatal(err)
						}
					}
				}
				return transport.RoundTrip(r)
			})
			result, err := workObsoleteExecute(t)
			expected := slices.Clone(ids[:2])
			slices.Sort(expected)
			if err != nil || result.Status != "cancelled" || !slices.Equal(result.WorkIDs, expected) {
				t.Fatalf("same-ledger progression blocked: %+v %v", result, err)
			}
			after := workClearRead(t, queue)
			if !progressed || after.Works[laterID].State != "available" {
				t.Fatal("snapshot publication swept later append")
			}
			for _, id := range expected {
				if after.Works[id].State != "cancelled" || after.Works[id].CancellationReason != workObsoleteReason {
					t.Fatal("snapshot publication did not cancel original targets")
				}
			}
			if _, err := branch.Compact(context.Background()); err != nil {
				t.Fatal(err)
			}
			head, pushes := queue.head(t), queue.pushes(t)
			retry, err := workObsoleteExecute(t)
			if err != nil || retry.Status != "already_committed" || !slices.Equal(retry.WorkIDs, expected) ||
				queue.head(t) != head || queue.pushes(t) != pushes {
				t.Fatalf("checkpoint lost idempotent receipt: %+v %v", retry, err)
			}
		})
	}
}
