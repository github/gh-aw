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
	commits[0].Operations = []Operation{Op(genesis)}
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
	fillUnallocated := func(stage string) {
		t.Helper()
		rejected := false
		for index := range 512 {
			control, err := NewRequest(fmt.Sprintf("fill-%s-%d", stage, index), "control", testActor("administrator"),
				OperationsParameters{Operations: []Operation{Op(map[string]any{
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
					Operations: []Operation{Op(map[string]any{
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
	fillUnallocated("completion")
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
	fillUnallocated("result")
	member := assignment.Claims[0]
	reconciler := maximumClosureActor()
	result, err := NewRequest(strings.Repeat("\\", 256), "result", reconciler, OperationsParameters{
		Operations: []Operation{Op(map[string]any{
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
	fillUnallocated("native-release")
	operations := []Operation{}
	for _, member := range assignment.Claims[1:] {
		operations = append(operations, Op(map[string]any{
			"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"reason": strings.Repeat("x", 128), "retry_not_before": 34000,
		}), Op(map[string]any{
			"kind": "WorkCancellation", "work_id": member.WorkID, "reason": strings.Repeat("x", 128),
		}))
	}
	operations = append(operations, Op(map[string]any{
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
}
