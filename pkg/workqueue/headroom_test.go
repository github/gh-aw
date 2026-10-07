package workqueue

import (
	"fmt"
	"strings"
	"testing"
)

func TestRecoveryHeadroomReportsActualReservationWithoutMutation(t *testing.T) {
	if RecoveryHeadroom(newProjection()) != 0 {
		t.Fatal("an empty projection must not reserve closure bytes")
	}
	commits := testGenesis(t, nil)
	work := testNode(t, commits, "headroom-diagnostic")
	commits = testSubmit(t, commits, "submit-headroom-diagnostic", work)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	before, err := canonicalValue(state)
	if err != nil {
		t.Fatal(err)
	}
	if reserve := RecoveryHeadroom(state); reserve != 55296 || reserve != remainingHeadroom(state) {
		t.Fatalf("expected 55296 bytes for three future attempts and delivery, got %d", reserve)
	}
	after, err := canonicalValue(state)
	if err != nil || string(before) != string(after) {
		t.Fatal("headroom diagnostics mutated the replayed projection")
	}
	commits = testOperations(t, commits, testActor("administrator"), "cancel-headroom-diagnostic", "cancel_work",
		Op(map[string]any{"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "operator_cancelled"}))
	closed, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if RecoveryHeadroom(closed) != 0 {
		t.Fatal("closed Work retained an outstanding reservation")
	}
}

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
