package workqueue

import (
	"fmt"
	"strings"
	"testing"
)

func TestOperationalControlsCannotConsumeOutstandingRecoveryReserve(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		policy.Limits.LedgerBytes = 6000
		policy.Limits.RecoveryBytes = 60000
	})
	work := testNode(t, commits, "reserved")
	commits = testSubmit(t, commits, "submit-reserved", work)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}

	headroom := remainingHeadroom(state)
	if headroom == 0 {
		t.Fatal("fixture must reserve actual outstanding Work closure")
	}
	rejected := false
	for index := range 100 {
		operation := Op(map[string]any{
			"kind": "Control", "control": "grants_paused", "value": true, "reason": strings.Repeat("x", 128),
		})
		request, err := NewRequest(fmt.Sprintf("retained-control-%d", index), "control", testActor("administrator"),
			OperationsParameters{Operations: []Operation{operation}})
		if err != nil {
			t.Fatal(err)
		}
		next, _, _, err := BuildCandidate(commits, testActor("administrator"), request, 3000)
		if err != nil {
			if !strings.Contains(err.Error(), "ledger_limit") {
				t.Fatal(err)
			}
			if state.LedgerBytes >= state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes {
				t.Fatal("Control only stopped after exhausting the raw ledger limit")
			}
			rejected = true
			break
		}
		commits = next
		state, err = Replay(commits)
		if err != nil {
			t.Fatal(err)
		}
		if remainingHeadroom(state) != headroom ||
			state.LedgerBytes+headroom > state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes {
			t.Fatal("optional Control eroded an outstanding closure reservation")
		}
	}
	if !rejected || state.LedgerBytes <= state.Policy.Limits.LedgerBytes {
		t.Fatal("fixture did not exercise Control writes within the recovery region")
	}
	commits = testOperations(t, commits, testActor("administrator"), "close-reserved", "cancel_work",
		Op(map[string]any{"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "operator_cancelled"}))
	closed, err := Replay(commits)
	if err != nil || closed.Works[work.WorkID].State != "cancelled" || remainingHeadroom(closed) != 0 {
		t.Fatalf("genuine closure could not discharge its reserved budget: %v", err)
	}
}
