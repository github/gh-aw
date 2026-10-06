package workqueue

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkerDispatchControlRequiresCompletedOriginalScope(t *testing.T) {
	commits, assignment := boundAssignment(t)
	node := testNode(t, commits, "next")
	commits = testSubmit(t, commits, "next-submit", node)
	actor := workerActor(assignment, "h1")
	params := DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10}
	request, _ := NewRequest("worker-dispatch", "dispatch_next", actor, params)
	if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err == nil {
		t.Fatal("open worker Claim dispatched new Work")
	}
	commits = finishMember(t, commits, assignment, 0, "completed")
	for _, mutate := range []func(*Actor){
		func(a *Actor) { a.ClaimHandle = "h2" },
		func(a *Actor) { a.ClaimHandle = "foreign" },
		func(a *Actor) { a.RunAttempt = 2 },
		func(a *Actor) { a.RunID = "303" },
		func(a *Actor) { a.Principal = "foreign" },
	} {
		other := actor
		mutate(&other)
		invalid, _ := NewRequest("invalid", "dispatch_next", other, params)
		if _, _, _, err := BuildCandidate(commits, other, invalid, 4000); err == nil {
			t.Fatal("uncompleted/foreign/rerun scope dispatched new Work")
		}
	}
	params.Pool = "other"
	crossPool, _ := NewRequest("cross-pool", "dispatch_next", actor, params)
	if _, _, _, err := BuildCandidate(commits, actor, crossPool, 4000); err == nil ||
		!strings.Contains(err.Error(), "claim_scope_invalid") {
		t.Fatalf("worker dispatch escaped parent pool: %v", err)
	}
	next, commit, decision, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil || commit == nil || commit.Actor.Role != "worker" || len(decision.Assignments) != 1 ||
		decision.Assignments[0].Claims[0].WorkID != node.WorkID {
		t.Fatalf("completed worker could not request ordinary fair prefix: %+v %v", decision, err)
	}
	state, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	terminal := terminalFor(state, assignment)
	operations := []Operation{}
	for _, member := range assignment.Claims[1:] {
		operations = append(operations, Op(map[string]any{
			"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"reason": "native_terminal", "retry_not_before": 34000,
		}))
	}
	operations = append(operations, Op(map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminal,
	}))
	next = testOperations(t, next, testActor("reconciler"), "release-parent", "release", operations...)
	if _, original, recovered, err := BuildCandidate(next, actor, request, 4000); err != nil ||
		original.ID != commit.ID || recovered.Reason != "already_committed" {
		t.Fatalf("original acknowledgment could not recover after scope closed: %+v %v", recovered, err)
	}
	fresh, _ := NewRequest("fresh-after-release", "dispatch_next", actor, params)
	if _, _, _, err := BuildCandidate(next, actor, fresh, 4000); err == nil {
		t.Fatal("released original worker retained new queue-control authority")
	}
}

func TestWorkerChildAdmissionNeedsCompletionAndInheritedScope(t *testing.T) {
	commits, assignment := boundAssignment(t)
	actor := workerActor(assignment, "h1")
	child := testNode(t, commits, "child")
	request, _ := NewRequest("worker-child", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{child}})
	if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err == nil {
		t.Fatal("open Claim admitted worker child")
	}
	commits = finishMember(t, commits, assignment, 0, "completed")
	child.Priority = 1
	boost, _ := NewRequest("boost", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{child}})
	if _, _, _, err := BuildCandidate(commits, actor, boost, 4000); err == nil {
		t.Fatal("worker child acquired a better priority")
	}
	child.Priority = 3
	request, _ = NewRequest("worker-child", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{child}})
	if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err != nil {
		t.Fatal(err)
	}
	state, _ := Replay(commits)
	failure := terminalFor(state, assignment)
	failure.Attempts, failure.Effects = 5, "unknown"
	commits = testOperations(t, commits, testActor("reconciler"), "failure", "delivery_failure", Op(map[string]any{
		"kind": "DeliveryFailure", "work_id": assignment.Claims[0].WorkID,
		"claim_id": assignment.Claims[0].ClaimID, "completion_id": state.Works[assignment.Claims[0].WorkID].CompletionID,
		"reason": "verification_exhausted", "disposition": "unknown", "evidence": failure,
	}))
	if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err == nil {
		t.Fatal("failed delivery barrier allowed new worker child effects")
	}
}

func TestWorkerDerivedObservationsUseSameEffectiveScope(t *testing.T) {
	commits, assignment := boundAssignment(t)
	gated := testNode(t, commits, "gated")
	resource := Resource{
		Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "2", Number: "7",
	}
	gated.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
	commits = testSubmit(t, commits, "gated-submit", gated)
	commits = finishMember(t, commits, assignment, 0, "completed")
	actor := workerActor(assignment, "h1")
	observation := Observation{
		Kind: "Observation", ObservationID: "worker-observation", Resource: resource,
		Condition: "completed", State: "ready", ObservedAt: 4000,
		CredentialGeneration: "initial", ReadStatus: "ok",
		ResourceState: "closed", StateReason: "completed",
	}
	state, _ := Replay(commits)
	observation.CredentialGeneration = state.CredentialGeneration
	request, _ := NewRequest("worker-gated-dispatch", "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	next, commit, _, err := buildCandidateWithObservations(commits, actor, request, 4000, []Observation{observation})
	if err != nil || commit == nil || operationKind(commit.Operations[0]) != "Observation" {
		t.Fatalf("trusted worker refresh could not precede its fair prefix: %v", err)
	}
	state, err = Replay(next)
	if err != nil || state.Works[gated.WorkID].State != "claimed" {
		t.Fatalf("worker observation did not preserve ordinary fair admission: %v", err)
	}
	operation, _ := json.Marshal(observation)
	observe, _ := NewRequest("derived-observe", "observe", actor, OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, actor, observe, 4000); err != nil {
		t.Fatal(err)
	}
	observeActor := workerActor(assignment, "h2")
	invalid, _ := NewRequest("uncompleted-observe", "observe", observeActor, OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, observeActor, invalid, 4000); err == nil {
		t.Fatal("uncompleted sibling forged a derived observation scope")
	}
}
