package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOperatorClaimFenceSingleAndMultiple(t *testing.T) {
	base, assignment := boundAssignment(t)
	for _, count := range []int{1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			before, err := Replay(base)
			if err != nil {
				t.Fatal(err)
			}
			operations := []Operation{}
			for _, member := range assignment.Claims[:count] {
				operations = append(operations, mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID, "reason": "operator_cancelled"}))
			}
			actor := testActor("administrator")
			request, err := NewRequest("operator-cancel", "cancel_work", actor, OperationsParameters{Operations: operations})
			if err != nil {
				t.Fatal(err)
			}
			commits, _, _, err := BuildCandidate(base, actor, request, 5000)
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			for index, member := range assignment.Claims {
				want := "claimed"
				if index < count {
					want = "cancelled"
				}
				if state.Works[member.WorkID].State != want {
					t.Fatalf("sibling scope changed: %d %+v", index, state.Works[member.WorkID])
				}
			}
			if state.Dispatches[assignment.DispatchID].Released || !sameJSON(state.Clocks, before.Clocks) ||
				!sameJSON(state.Dispatches[assignment.DispatchID].Assignment, before.Dispatches[assignment.DispatchID].Assignment) {
				t.Fatal("operator cancellation released a native worker, refunded debt or mutated membership")
			}
			recovered, _, _, err := BuildCandidate(commits, actor, request, 6000)
			if err != nil || len(recovered) != len(commits) {
				t.Fatalf("stable acknowledgment recovery failed: %v", err)
			}
			assertOperatorJSProjection(t, commits)
		})
	}
}

func TestOperatorClaimFenceRejectsStaleTerminalAndForeignAtomically(t *testing.T) {
	base, assignment := boundAssignment(t)
	one, two := assignment.Claims[0], assignment.Claims[1]
	actor := testActor("administrator")
	for _, invalid := range []map[string]any{
		{"kind": "WorkCancellation", "work_id": one.WorkID, "claim_id": "missing", "reason": "operator_cancelled"},
		{"kind": "WorkCancellation", "work_id": one.WorkID, "claim_id": two.ClaimID, "reason": "operator_cancelled"},
	} {
		request, err := NewRequest("invalid-bulk", "cancel_work", actor, OperationsParameters{Operations: []Operation{
			mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": two.WorkID, "claim_id": two.ClaimID, "reason": "operator_cancelled"}),
			mustOp(t, invalid),
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := BuildCandidate(base, actor, request, 5000); err == nil || !strings.HasPrefix(err.Error(), "claim_scope_invalid:") {
			t.Fatalf("invalid bulk accepted: %v", err)
		}
	}
	cancel := mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": one.WorkID, "claim_id": one.ClaimID, "reason": "operator_cancelled"})
	request, _ := NewRequest("stale-owner", "cancel_work", actor, OperationsParameters{Operations: []Operation{cancel}})
	for _, outcome := range []string{"completed", "cancelled"} {
		changed := finishMember(t, base, assignment, 0, outcome)
		if _, _, _, err := BuildCandidate(changed, actor, request, 7000); err == nil {
			t.Fatalf("CAS refresh cancelled a %s original owner", outcome)
		}
		assertOperatorJSProjection(t, changed)
	}
	// A retry's new owner must never be cancelled through an old Claim selector.
	retry := finishMember(t, base, assignment, 0, "cancelled")
	grant, err := NewRequest("retry-owner", "dispatch_next", actor, DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
	if err != nil {
		t.Fatal(err)
	}
	retry, _, decision, err := BuildCandidate(retry, actor, grant, 100000)
	if err != nil || len(decision.Assignments) != 1 {
		t.Fatalf("retry did not establish new ownership: %v %+v", err, decision)
	}
	if _, _, _, err := BuildCandidate(retry, actor, request, 101000); err == nil {
		t.Fatal("historical selector cancelled successor ownership")
	}
}

func TestOperatorPriorityIsProspectiveAndFair(t *testing.T) {
	base := testGenesis(t, nil)
	node := testNode(t, base, "priority-change")
	base = testSubmit(t, base, "submit-priority", node)
	before, _ := Replay(base)
	op := mustOp(t, map[string]any{"kind": "WorkPriority", "work_id": node.WorkID, "priority": 1, "expected_priority": 3, "reason": "operator_reprioritized"})
	actor := testActor("administrator")
	request, err := NewRequest("set-priority", "control", actor, OperationsParameters{Operations: []Operation{op}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err := BuildCandidate(base, actor, request, 3000)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := Replay(commits)
	if state.Works[node.WorkID].SchedulingPriority() != 1 || !sameJSON(state.Works[node.WorkID].WorkDefinition, node) ||
		state.Works[node.WorkID].Position != before.Works[node.WorkID].Position || !sameJSON(state.Clocks, before.Clocks) {
		t.Fatal("priority override rewrote admission, queue age or accounting")
	}
	assertOperatorJSProjection(t, commits)
	granted, decision := testGrant(t, commits, "grant-new-priority", 1, 1)
	history, _ := Replay(granted)
	explanation, err := ExplainBeforeClaim(granted, decision.Assignments[0].Claims[0].ClaimID)
	if err != nil || explanation.Priority != 1 || history.Clocks["default"].Classes.Pass["1"] == "" {
		t.Fatalf("grant did not charge new fair class: %+v %v", explanation, err)
	}
	assertOperatorJSProjection(t, granted)
	stale, _ := NewRequest("stale-priority", "control", actor, OperationsParameters{Operations: []Operation{op}})
	if _, _, _, err := BuildCandidate(commits, actor, stale, 4000); err == nil || !strings.HasPrefix(err.Error(), "priority_conflict:") {
		t.Fatalf("stale compare-and-set accepted: %v", err)
	}
	next := mustOp(t, map[string]any{"kind": "WorkPriority", "work_id": node.WorkID, "priority": 2, "expected_priority": 1, "reason": "operator_reprioritized"})
	frozen, _ := NewRequest("frozen-priority", "control", actor, OperationsParameters{Operations: []Operation{next}})
	if _, _, _, err := BuildCandidate(granted, actor, frozen, 5000); err == nil {
		t.Fatal("changed immutable active assignment")
	}
	for _, role := range []string{"producer", "worker", "dispatcher", "reconciler"} {
		origin := testActor(role)
		unauthorized, err := NewRequest("role-priority-"+role, "control", origin, OperationsParameters{Operations: []Operation{op}})
		if err == nil {
			_, _, _, err = BuildCandidate(base, origin, unauthorized, 3000)
		}
		if err == nil {
			t.Fatalf("%s changed administrator priority", role)
		}
	}
	// Stable replay recovers the original operation instead of applying it twice.
	recovered, _, _, err := BuildCandidate(granted, actor, request, 7000)
	if err != nil || !slices.EqualFunc(recovered, granted, func(a, b QueueCommit) bool { return sameJSON(a, b) }) {
		t.Fatalf("priority acknowledgment recovery: %v", err)
	}
}

func assertOperatorJSProjection(t *testing.T, commits []QueueCommit) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("operator protocol parity requires Node.js")
	}
	data, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", `
const q = require("../../actions/setup/js/work_queue_replay.cjs");
const fs = require("node:fs");
console.log(JSON.stringify(q.serializeProjection(q.replayTransactions(q.parseTransactionLog(fs.readFileSync(0, "utf8"))))));
`)
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript replay: %v\n%s", err, output)
	}

	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonicalValue(state)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatal(err)
	}
	actual, err := canonicalValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatalf("operator projection mismatch\nGo: %s\nJS: %s", expected, actual)
	}
}

func TestOperatorNativeCASRefreshPreservesFences(t *testing.T) {
	for _, scenario := range []string{"claim_completed", "claim_retried", "priority_changed", "priority_granted"} {
		t.Run(scenario, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			actor := testActor("administrator")
			var commits []QueueCommit
			var operations []Operation
			var kind, unaffected string
			if strings.HasPrefix(scenario, "claim_") {
				var assignment Assignment
				commits, assignment = boundAssignment(t)
				member, sibling := assignment.Claims[0], assignment.Claims[1]
				unaffected = sibling.WorkID
				kind = "cancel_work"
				operations = []Operation{
					mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": sibling.WorkID, "claim_id": sibling.ClaimID, "reason": "operator_cancelled"}),
					mustOp(t, map[string]any{"kind": "WorkCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID, "reason": "operator_cancelled"}),
				}
				mock.concurrent = func() {
					outcome := "completed"
					if scenario == "claim_retried" {
						outcome = "cancelled"
					}
					other := finishMember(t, commits, assignment, 0, outcome)
					installMockLog(t, mock, other)
				}
			} else {
				commits = testGenesis(t, nil)
				node := testNode(t, commits, "native-priority")
				commits = testSubmit(t, commits, "submit", node)
				kind = "control"
				operations = []Operation{mustOp(t, map[string]any{"kind": "WorkPriority", "work_id": node.WorkID, "priority": 1, "expected_priority": 3, "reason": "operator_reprioritized"})}
				mock.concurrent = func() {
					var other []QueueCommit
					if scenario == "priority_granted" {
						other, _ = testGrant(t, commits, "concurrent-grant", 1, 1)
					} else {
						other = testOperations(t, commits, actor, "concurrent-priority", "control", mustOp(t, map[string]any{
							"kind": "WorkPriority", "work_id": node.WorkID, "priority": 2, "expected_priority": 3, "reason": "operator_reprioritized",
						}))
					}
					installMockLog(t, mock, other)
				}
			}
			installMockLog(t, mock, commits)
			request, err := NewRequest("stale-operator-action", kind, actor, OperationsParameters{Operations: operations})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := branch.Publish(context.Background(), actor, request); err == nil {
				t.Fatal("CAS refresh ignored exact ownership/priority fence")
			}
			latest, err := branch.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(latest)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := state.Requests[request.ID]; ok {
				t.Fatal("rejected atomic action entered authority")
			}
			if unaffected != "" && state.Works[unaffected].State != "claimed" {
				t.Fatal("failed bulk publication cancelled an unaffected sibling")
			}
			assertOperatorJSProjection(t, latest)
		})
	}
}
