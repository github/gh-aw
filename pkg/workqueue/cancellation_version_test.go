package workqueue

import (
	"strconv"
	"strings"
	"testing"
)

const obsoleteCancellationReason = "worker_contract_obsolete"

func cancellationVersionFixture(t *testing.T) ([]QueueCommit, []WorkDefinition) {
	t.Helper()
	commits := testGenesis(t, deploymentPolicy)
	nodes := []WorkDefinition{testNode(t, commits, "old"), testNode(t, commits, "pinned"), testNode(t, commits, "compatible")}
	nodes[1].ExecutionRef = pinnedRef(commits, t)
	nodes[2].WorkerProfile = "other"
	return testSubmit(t, commits, "backlog", nodes...), nodes
}

func TestLegacyObsoleteCancellationHistoryParity(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(strconv.FormatBool(changed), func(t *testing.T) {
			commits, nodes := cancellationVersionFixture(t)
			if changed {
				commits = deployTestWorker(t, commits, "incompatible", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
			}
			operations := []Operation{}
			for _, node := range nodes {
				operations = append(operations, mustOp(t, map[string]any{
					"kind": "WorkCancellation", "work_id": node.WorkID, "reason": obsoleteCancellationReason,
				}))
			}
			commits = testOperations(t, commits, testActor("administrator"), "legacy-retirement", "cancel_work", operations...)
			assertOperatorJSProjection(t, commits)
			for _, sha := range []string{strings.Repeat("c", 40), strings.Repeat("d", 40)} {
				var err error
				commits, err = CompactCheckpoint(commits, sha, testActor("administrator"), 5000)
				if err != nil {
					t.Fatal(err)
				}
				state, err := Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
				for index, node := range nodes {
					if state.Works[node.WorkID].State != "cancelled" || !sameJSON(state.Cancellations[node.WorkID], operations[index]) {
						t.Fatal("checkpoint changed historical reason-only cancellation")
					}
				}
				assertOperatorJSProjection(t, commits)
			}
		})
	}
}

func TestCheckedObsoleteCancellationGuards(t *testing.T) {
	base, nodes := cancellationVersionFixture(t)
	base = deployTestWorker(t, base, "incompatible", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
	operation := mustOp(t, map[string]any{
		"kind": "WorkCancellation", "version": 1, "work_id": nodes[0].WorkID, "reason": obsoleteCancellationReason,
	})
	for name, change := range map[string]func(*Projection, *WorkState){
		"claimed":  func(_ *Projection, w *WorkState) { w.State = "claimed" },
		"owner":    func(_ *Projection, w *WorkState) { w.ClaimID = "owned" },
		"pin":      func(_ *Projection, w *WorkState) { w.ExecutionRef = nodes[1].ExecutionRef },
		"pending":  func(_ *Projection, w *WorkState) { w.Barrier = "pending" },
		"failed":   func(_ *Projection, w *WorkState) { w.Barrier = "failed" },
		"verified": func(_ *Projection, w *WorkState) { w.Barrier = "verified" },
		"missing":  func(s *Projection, _ *WorkState) { delete(s.Deployments["default"], "default") },
		"compatible": func(s *Projection, _ *WorkState) {
			worker := s.Deployments["default"]["default"]
			revision := worker.Revisions[worker.CurrentRef]
			revision.Profile.LogicalContract = strings.Repeat("a", 64)
			worker.Revisions[worker.CurrentRef] = revision
		},
		"unavailable": func(s *Projection, _ *WorkState) {
			worker := s.Deployments["default"]["default"]
			revision := worker.Revisions[worker.CurrentRef]
			revision.Available = false
			worker.Revisions[worker.CurrentRef] = revision
		},
	} {
		t.Run(name, func(t *testing.T) {
			state, err := Replay(base)
			if err != nil {
				t.Fatal(err)
			}
			change(&state, state.Works[nodes[0].WorkID])
			before, _ := canonicalValue(state)
			err = state.replayCancellation(operation, "WorkCancellation", QueueCommit{ID: "checked", Actor: testActor("administrator")})
			if err == nil || !strings.HasPrefix(err.Error(), "deployment_conflict:") {
				t.Fatalf("checked cancellation accepted protected Work: %v", err)
			}
			after, _ := canonicalValue(state)
			if string(before) != string(after) {
				t.Fatal("rejected cancellation mutated protected state")
			}
		})
	}
}

func TestCheckedCancellationVersionAndTerminalMetadataParity(t *testing.T) {
	base, nodes := cancellationVersionFixture(t)
	base = deployTestWorker(t, base, "incompatible", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
	legacy := mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": nodes[0].WorkID, "reason": obsoleteCancellationReason})
	checked := mustOp(t, map[string]any{"kind": "WorkCancellation", "version": 1, "work_id": nodes[0].WorkID, "reason": obsoleteCancellationReason})
	for _, pair := range [][2]Operation{{legacy, checked}, {checked, legacy}} {
		commits := testOperations(t, base, testActor("administrator"), "cancel", "cancel_work", pair[0])
		commits = testOperations(t, commits, testActor("administrator"), "repeat", "cancel_work", pair[0])
		assertOperatorJSProjection(t, commits)
		commits, err := CompactCheckpoint(commits, strings.Repeat("c", 40), testActor("administrator"), 5000)
		if err != nil {
			t.Fatal(err)
		}
		assertOperatorJSProjection(t, commits)
		request, _ := NewRequest("rewrite", "cancel_work", testActor("administrator"), OperationsParameters{Operations: []Operation{pair[1]}})
		if _, _, _, err := BuildCandidate(commits, testActor("administrator"), request, 6000); err == nil || !strings.HasPrefix(err.Error(), "cancellation_conflict:") {
			t.Fatalf("terminal receipt version rewritten: %v", err)
		}
	}

	for _, version := range []any{0, 2, nil, "1", false} {
		operation := mustOp(t, map[string]any{
			"kind": "WorkCancellation", "version": version, "work_id": nodes[0].WorkID, "reason": obsoleteCancellationReason,
		})
		request, _ := NewRequest("invalid-version", "cancel_work", testActor("administrator"), OperationsParameters{Operations: []Operation{operation}})
		if _, _, _, err := BuildCandidate(base, testActor("administrator"), request, 5000); err == nil {
			t.Fatalf("accepted unsupported explicit version %v", version)
		}
	}
}

func TestCheckedCancellationExpirationAndAuthorization(t *testing.T) {
	base := testGenesis(t, deploymentPolicy)
	work := testNode(t, base, "expiring")
	deadline := int64(4500)
	work.ExpiresAt = &deadline
	base = testSubmit(t, base, "expiring-backlog", work)
	base = deployTestWorker(t, base, "incompatible", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
	operation := mustOp(t, map[string]any{
		"kind": "WorkCancellation", "version": 1, "work_id": work.WorkID, "reason": obsoleteCancellationReason,
	})
	actor := testActor("administrator")
	request, err := NewRequest("expired-retirement", "cancel_work", actor, OperationsParameters{Operations: []Operation{operation}})
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{deadline, deadline + 1} {
		if _, _, _, err := BuildCandidate(base, actor, request, at); err == nil || !strings.HasPrefix(err.Error(), "cancellation_conflict:") {
			t.Fatalf("checked cancellation accepted elapsed Work: %v", err)
		}
	}
	for _, role := range []string{"dispatcher", "worker", "projector"} {
		actor := testActor(role)
		request, err := NewRequest("unauthorized-retirement", "cancel_work", actor, OperationsParameters{Operations: []Operation{operation}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := BuildCandidate(base, actor, request, 4001); err == nil {
			t.Fatalf("checked version granted %s cancellation authority", role)
		}
	}
	assertOperatorJSProjection(t, base)
}
