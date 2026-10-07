package workqueue

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLastAttemptCompletionCanSpendReservedClosureBytes(t *testing.T) {
	commits, assignment := boundAssignment(t)
	var genesis struct {
		Kind   string `json:"kind"`
		Epoch  string `json:"epoch"`
		Policy Policy `json:"policy"`
	}
	if err := json.Unmarshal(commits[0].Operations[0], &genesis); err != nil {
		t.Fatal(err)
	}
	genesis.Policy.Limits.LedgerBytes = 8 << 10
	genesis.Policy.Limits.RecoveryBytes = 220 << 10
	pool := genesis.Policy.Pools["default"]
	pool.Retry.MaxAttempts = 1
	genesis.Policy.Pools["default"] = pool
	commits[0].Operations = []Operation{mustOp(t, genesis)}
	request, err := NewRequest(commits[0].Request.ID, "policy", commits[0].Actor,
		OperationsParameters{Operations: commits[0].Operations})
	if err != nil {
		t.Fatal(err)
	}
	commits[0].Request = request
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	fillUnallocated := func(t *testing.T, stage string) {
		t.Helper()
		rejected := false
		for index := range 512 {
			control, err := NewRequest(fmt.Sprintf("fill-%s-%d", stage, index), "control", testActor("administrator"),
				OperationsParameters{Operations: []Operation{mustOp(t, map[string]any{
					"kind": "Control", "control": "grants_paused", "value": true, "reason": "x",
				})}})
			if err != nil {
				t.Fatal(err)
			}
			next, _, _, err := BuildCandidate(commits, testActor("administrator"), control, 4000)
			if err != nil {
				if !strings.Contains(err.Error(), "ledger_limit") {
					t.Fatal(err)
				}
				previous := state.Tip
				rejectedCommit := QueueCommit{
					Version: Version, ID: ProposedCommitID(state, control), Previous: &previous,
					Request: control, Actor: testActor("administrator"), PolicyEpoch: state.PolicyEpoch, At: 4000,
					Operations: []Operation{mustOp(t, map[string]any{
						"kind": "Control", "control": "grants_paused", "value": true, "reason": "x",
					})},
				}
				assertRecoveryReplayParity(t, append(append([]QueueCommit{}, commits...), rejectedCommit), "ledger_limit")
				rejected = true
				break
			}
			commits = next
			state, err = Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !rejected || state.LedgerBytes <= state.Policy.Limits.LedgerBytes {
			t.Fatal("fixture did not fill the unallocated recovery region")
		}
	}
	fillUnallocated(t, "completion")
	actor := workerActor(assignment, "h1")
	finish, err := NewRequest("last-attempt-completion", "finish", actor, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: "h1", Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	next, _, _, err := BuildCandidate(commits, actor, finish, 4000)
	if err != nil {
		t.Fatalf("last-attempt Completion cannot spend its reserved closure bytes: %v", err)
	}
	closed, err := Replay(next)
	if err != nil || closed.Works[assignment.Claims[0].WorkID].Barrier != "pending" {
		t.Fatalf("last-attempt Completion did not retain independent delivery recovery: %v", err)
	}
	commits, state = next, closed
	assertRecoveryReplayParity(t, commits, "")
	fillUnallocated(t, "result")
	member := assignment.Claims[0]
	reconciler := maximumClosureActor()
	result, err := NewRequest(strings.Repeat("\\", 256), "result", reconciler, OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{
			"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"completion_id": state.Works[member.WorkID].CompletionID,
			"descriptor":    json.RawMessage(`{"x":"` + strings.Repeat("x", 4096-8) + `"}`),
			"evidence":      maximumClosureEvidence(t, state, assignment, true),
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	next, _, _, err = BuildCandidate(commits, reconciler, result, 4000)
	if err != nil {
		t.Fatalf("maximum Result cannot spend independent reserved delivery bytes: %v", err)
	}
	commits = next
	assertRecoveryReplayParity(t, commits, "")
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	nativePrefix := append([]QueueCommit{}, commits...)
	fillUnallocated(t, "native-release")
	operations := []Operation{}
	for _, member := range assignment.Claims[1:] {
		operations = append(operations, mustOp(t, map[string]any{
			"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"reason": strings.Repeat("x", 128), "retry_not_before": 34000,
		}), mustOp(t, map[string]any{
			"kind": "WorkCancellation", "work_id": member.WorkID, "reason": strings.Repeat("x", 128),
		}))
	}
	operations = append(operations, mustOp(t, map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID,
		"evidence": maximumClosureEvidence(t, state, assignment, false),
	}))
	release, err := NewRequest(strings.Repeat("\"", 256), "release", reconciler, OperationsParameters{Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	next, _, _, err = BuildCandidate(commits, reconciler, release, 4000)
	if err != nil {
		t.Fatalf("maximum mixed native closure cannot spend reserved recovery bytes: %v", err)
	}
	closed, err = Replay(next)
	if err != nil || closed.Stats.Dispatches != 0 || closed.Stats.Completed != 1 ||
		closed.Stats.Cancelled != 2 || remainingHeadroom(closed) != 0 {
		t.Fatalf("bounded independent success/cancellation/native suffix did not fully close: %+v %v", closed.Stats, err)
	}
	assertRecoveryReplayParity(t, next, "")
	t.Run("fully-exhausted-native-escrow", func(t *testing.T) {
		commits = nativePrefix
		for member := 1; member < len(assignment.Claims); member++ {
			commits = finishMember(t, commits, assignment, member, "cancelled")
		}
		state, err = Replay(commits)
		if err != nil {
			t.Fatal(err)
		}
		dispatch := state.Dispatches[assignment.DispatchID]
		for dispatch.LifecycleWrites < state.Policy.Pools[dispatch.Pool].Reconciliation.MaxAttempts+4 {
			evidence := terminalFor(state, assignment)
			evidence.Kind, evidence.Status, evidence.Conclusion = "reconciliation", "", ""
			commits = testOperations(t, commits, testActor("reconciler"), fmt.Sprintf("exhaust-native-%d", dispatch.LifecycleWrites),
				"dispatch", mustOp(t, map[string]any{
					"kind": "Dispatch", "dispatch_id": dispatch.DispatchID, "state": "bound",
					"run": dispatch.Run, "evidence": evidence,
				}))
			state, err = Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			dispatch = state.Dispatches[assignment.DispatchID]
		}
		finalEscrow := 2*state.Policy.Limits.EvidenceBytes + 8192
		if remainingHeadroom(state) != finalEscrow {
			t.Fatalf("final-only native reservation differs: want %d, got %d", finalEscrow, remainingHeadroom(state))
		}
		fillUnallocated(t, "native-escrow-only")
		request, err := NewRequest(strings.Repeat("\\", 255)+"\"", "release", reconciler, OperationsParameters{
			Operations: []Operation{mustOp(t, map[string]any{
				"kind": "Release", "dispatch_id": assignment.DispatchID,
				"evidence": maximumClosureEvidence(t, state, assignment, false),
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		next, _, _, err := BuildCandidate(commits, reconciler, request, 4000)
		if err != nil {
			t.Fatalf("maximum final Release cannot spend its fully exhausted native escrow: %v", err)
		}
		closed, err := Replay(next)
		if err != nil || remainingHeadroom(closed) != 0 || closed.Stats.Dispatches != 0 ||
			closed.Stats.Completed != 1 || closed.Stats.Cancelled != 2 {
			t.Fatalf("native-only recovery suffix did not fully close: %+v %v", closed.Stats, err)
		}
		assertRecoveryReplayParity(t, next, "")
	})
}
