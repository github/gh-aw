package workqueue

import (
	"strings"
	"testing"
)

func verifiedMemberResult(t *testing.T, commits []QueueCommit, assignment Assignment, index int) []QueueCommit {
	t.Helper()
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	member := assignment.Claims[index]
	evidence := terminalFor(state, assignment)
	evidence.Kind, evidence.Source, evidence.Receipt = "delivery", "verified_receipts", "verified-"+member.Handle
	return testOperations(t, commits, testActor("reconciler"), "result-"+member.Handle, "result", Op(map[string]any{
		"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID,
		"completion_id": state.Works[member.WorkID].CompletionID,
		"descriptor":    map[string]any{"ok": true}, "evidence": evidence,
	}))
}

func TestEffectsStopAtVerifiedResultWithoutClosingSiblingScope(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = finishMember(t, commits, assignment, 1, "completed")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	for _, handle := range []string{"h1", "h2"} {
		if err := AuthorizeEffect(state, workerActor(assignment, handle)); err != nil {
			t.Fatalf("pending completed member %s lost effect authority: %v", handle, err)
		}
	}
	commits = verifiedMemberResult(t, commits, assignment, 0)
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h1")); err == nil ||
		!strings.HasPrefix(err.Error(), "claim_effects_unauthorized:") {
		t.Fatalf("verified Result retained effect authority: %v", err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h2")); err != nil {
		t.Fatalf("completed delivery-pending sibling lost independent effect authority: %v", err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h3")); err == nil {
		t.Fatal("open sibling borrowed completed effect authority")
	}
}

func TestVerifiedResultKeepsScopedWorkerQueueContinuation(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = verifiedMemberResult(t, commits, assignment, 0)
	actor := workerActor(assignment, "h1")
	child := testNode(t, commits, "after-verified-result")
	request, err := NewRequest("verified-worker-child", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{child}})
	if err != nil {
		t.Fatal(err)
	}
	next, commit, _, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil || commit == nil {
		t.Fatalf("verified Result incorrectly closed effective queue continuation: %v", err)
	}
	state, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	if state.Works[child.WorkID] == nil {
		t.Fatal("scoped worker child was not admitted")
	}
	dispatch, err := NewRequest("verified-worker-dispatch", "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, grant, decision, err := BuildCandidate(next, actor, dispatch, 4000); err != nil ||
		grant == nil || len(decision.Assignments) != 1 {
		t.Fatalf("verified Result incorrectly closed scoped fair dispatch: %v", err)
	}
	if err := AuthorizeEffect(state, actor); err == nil {
		t.Fatal("queue continuation reopened terminal-result effect authority")
	}
}

func TestRetiredEpochCannotAuthorizeEffectsOrWorkerQueueControls(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	actor := workerActor(assignment, "h1")
	child := testNode(t, commits, "retired-epoch-child")
	request, err := NewRequest("retired-worker-child", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{child}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	state.PolicyEpoch = "retired-snapshot-epoch"
	if err := AuthorizeEffect(state, actor); err == nil ||
		!strings.HasPrefix(err.Error(), "claim_ineffective:") {
		t.Fatalf("retired-epoch Claim acquired current effect authority: %v", err)
	}
	if err := authorizeWorkerQueueRequest(state, actor, request); err == nil ||
		!strings.HasPrefix(err.Error(), "claim_ineffective:") {
		t.Fatalf("retired-epoch Claim acquired current continuation authority: %v", err)
	}
	if _, err := state.scopedClaim(actor, assignment.DispatchID, "h1"); err == nil {
		t.Fatal("retired-epoch Claim acquired an effective native binding")
	}
}
