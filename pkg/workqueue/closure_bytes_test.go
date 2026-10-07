package workqueue

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func maximumClosureActor() Actor {
	actor := testActor("reconciler")
	actor.Workflow = strings.Repeat("\\", 256)
	actor.RunID, actor.RunAttempt = strings.Repeat("9", 256), 4096
	actor.DispatchID, actor.ClaimHandle = strings.Repeat("\\", 256), strings.Repeat("\\", 256)
	return actor
}

func maximumClosureEvidence(t *testing.T, state Projection, assignment Assignment, delivery bool) Evidence {
	t.Helper()
	evidence := terminalFor(state, assignment)
	if delivery {
		evidence.Kind, evidence.Source = "delivery", "verified_receipts"
	}
	evidence.Receipt, evidence.Conclusion = strings.Repeat("\\", 256), "x"
	data, err := canonicalValue(evidence)
	if err != nil {
		t.Fatal(err)
	}
	extra := state.Policy.Limits.EvidenceBytes - int64(len(data))
	if extra < 0 || extra > 255 {
		t.Fatalf("maximum evidence fixture cannot fill exact budget: %d", extra)
	}
	evidence.Conclusion += strings.Repeat("x", int(extra))
	data, err = canonicalValue(evidence)
	if err != nil || int64(len(data)) != state.Policy.Limits.EvidenceBytes {
		t.Fatal("fixture did not produce maximum canonical evidence bytes")
	}
	return evidence
}

func TestMaximumStartedDispatchFitsLifecycleReservedBytes(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = commits[:3]
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	actor := maximumClosureActor()
	actor.Role = "dispatcher"
	request, err := NewRequest(strings.Repeat("\\", 256), "dispatch", actor, OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{
			"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": actor,
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	next, commit, _, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	started, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	data, err := canonicalValue(commit)
	if err != nil {
		t.Fatal(err)
	}
	discharged := remainingHeadroom(state) - remainingHeadroom(started)
	t.Logf("valid start canonical bytes=%d discharged reserve=%d", len(data)+1, discharged)
	if int64(len(data)+1) > discharged {
		t.Fatalf("maximum valid start marker uses %d bytes but lifecycle reservation discharges only %d", len(data)+1, discharged)
	}
}

func TestMaximumNativeReleaseFitsFinalReservedBytes(t *testing.T) {
	commits, assignment := boundAssignment(t)
	for member := range assignment.Claims {
		commits = finishMember(t, commits, assignment, member, "completed")
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := state.Dispatches[assignment.DispatchID]
	for dispatch.LifecycleWrites < state.Policy.Pools[dispatch.Pool].Reconciliation.MaxAttempts+4 {
		evidence := terminalFor(state, assignment)
		evidence.Kind, evidence.Status, evidence.Conclusion = "reconciliation", "", ""
		commits = testOperations(t, commits, testActor("reconciler"), fmt.Sprintf("bind-again-%d", dispatch.LifecycleWrites),
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
	actor := maximumClosureActor()
	request, err := NewRequest(strings.Repeat("\\", 256), "release", actor, OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{
			"kind": "Release", "dispatch_id": assignment.DispatchID,
			"evidence": maximumClosureEvidence(t, state, assignment, false),
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	next, commit, _, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	data, err := canonicalValue(commit)
	if err != nil {
		t.Fatal(err)
	}
	discharged := remainingHeadroom(state) - remainingHeadroom(closed)
	t.Logf("maximum Release canonical bytes=%d discharged reserve=%d", len(data)+1, discharged)
	if int64(len(data)+1) > discharged {
		t.Fatalf("maximum valid Release uses %d bytes but final native reservation discharges only %d", len(data)+1, discharged)
	}
}

func TestMaximumResultFitsIndependentDeliveryReservedBytes(t *testing.T) {
	testMaximumResultFitsDeliveryReserve(t, false)
}

func TestReleasedPendingCompletionMaximumResultFitsDeliveryReservedBytes(t *testing.T) {
	testMaximumResultFitsDeliveryReserve(t, true)
}

func testMaximumResultFitsDeliveryReserve(t *testing.T, releaseNative bool) {
	t.Helper()
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	if releaseNative {
		for member := 1; member < len(assignment.Claims); member++ {
			commits = finishMember(t, commits, assignment, member, "completed")
		}
		state, err := Replay(commits)
		if err != nil {
			t.Fatal(err)
		}
		commits = testOperations(t, commits, testActor("reconciler"), "native-release", "release", mustOp(t, map[string]any{
			"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminalFor(state, assignment),
		}))
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	actor := maximumClosureActor()
	member := assignment.Claims[0]
	descriptor := json.RawMessage(`{"x":"` + strings.Repeat("x", int(state.Policy.Limits.ResultBytes)-8) + `"}`)
	request, err := NewRequest(strings.Repeat("\\", 256), "result", actor, OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{
			"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"completion_id": state.Works[member.WorkID].CompletionID, "descriptor": descriptor,
			"evidence": maximumClosureEvidence(t, state, assignment, true),
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	next, commit, _, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	data, err := canonicalValue(commit)
	if err != nil {
		t.Fatal(err)
	}
	discharged := remainingHeadroom(state) - remainingHeadroom(closed)
	t.Logf("maximum Result released=%t canonical bytes=%d discharged delivery reserve=%d", releaseNative, len(data)+1, discharged)
	if int64(len(data)+1) > discharged {
		t.Fatalf("maximum valid Result uses %d bytes but delivery reservation discharges only %d", len(data)+1, discharged)
	}
}

func TestStartMarkerCannotCarryPrematureNativeEvidence(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = commits[:3]
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	actor := testActor("dispatcher")
	actor.Workflow, actor.RunID, actor.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	request, err := NewRequest("start-with-evidence", "dispatch", actor, OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{
			"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": actor,
			"evidence": terminalFor(state, assignment),
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err == nil ||
		!strings.HasPrefix(err.Error(), "launch_started:") {
		t.Fatalf("unbound start marker accepted premature native evidence: %v", err)
	}
}
