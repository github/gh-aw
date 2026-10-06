package workqueue

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func observedDispatchFixture(t *testing.T) ([]QueueCommit, Observation) {
	t.Helper()
	commits := testGenesis(t, func(policy *Policy) { policy.Limits.Operations = 3 })
	resource := Resource{
		Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "2", Number: "7",
	}
	nodes := []WorkDefinition{}
	for _, name := range []string{"a", "b", "c", "d"} {
		node := testNode(t, commits, name)
		node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
		nodes = append(nodes, node)
	}
	commits = testSubmit(t, commits, "submit-first", nodes[:3]...)
	commits = testSubmit(t, commits, "submit-last", nodes[3])
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return commits, Observation{
		Kind: "Observation", ObservationID: "budget-observation", Resource: resource,
		Condition: "completed", State: "ready", ObservedAt: 3000,
		CredentialGeneration: state.CredentialGeneration, ReadStatus: "ok",
		ResourceState: "closed", StateReason: "completed",
	}
}

func TestObservedDispatchReservesAtomicOperationBudget(t *testing.T) {
	commits, observation := observedDispatchFixture(t)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := canonicalValue(state)
	actor := testActor("dispatcher")
	actor.Workflow, actor.RunID, actor.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	request, err := NewRequest("observed-grant", "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: 16, MaxDispatches: 16, MaxBytes: 48 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	next, commit, decision, err := buildCandidateWithObservations(commits, actor, request, 3000, []Observation{observation})
	if err != nil {
		t.Fatal(err)
	}
	if commit == nil || len(commit.Operations) != 3 || operationKind(commit.Operations[0]) != "Observation" ||
		len(decision.Operations) != 2 || decision.Reason != "operation_budget_reached" {
		t.Fatalf("observations did not reserve their atomic operation slot: %+v %+v", commit, decision)
	}
	selected := []string{}
	for _, operation := range decision.Operations {
		var claim ClaimOperation
		if err := json.Unmarshal(operation, &claim); err != nil {
			t.Fatal(err)
		}
		selected = append(selected, state.Works[claim.WorkID].NodeKey)
		if !slices.Equal(claim.Observations, []string{observation.ObservationID}) {
			t.Fatal("Claim did not bind the preceding ready observation")
		}
	}
	if !slices.Equal(selected, []string{"a", "b"}) {
		t.Fatalf("bounded grant departed from independent FIFO expectation: %v", selected)
	}
	if _, err := Replay(next); err != nil {
		t.Fatalf("valid observed maximal prefix cannot replay: %v", err)
	}
	for _, count := range []int{1, 2} {
		truncated := slices.Clone(next)
		truncated[len(truncated)-1].Operations = commit.Operations[:count]
		if _, err := Replay(truncated); err == nil {
			t.Fatalf("nonmaximal observed prefix with %d total operations was accepted", count)
		}
	}
	after, _ := canonicalValue(state)
	if !bytes.Equal(before, after) {
		t.Fatal("observed preview mutated the original projection")
	}
}

func TestObservedDispatchNoClaimConsumesNoLogicalRequest(t *testing.T) {
	commits, observation := observedDispatchFixture(t)
	before, _ := Serialize(commits)
	actor := testActor("administrator")
	request, err := NewRequest("no-operation-room", "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: 16, MaxDispatches: 16, MaxBytes: 48 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	observations := []Observation{}
	for _, id := range []string{"first", "second", "third"} {
		copy := observation
		copy.ObservationID = id
		observations = append(observations, copy)
	}
	next, commit, decision, err := buildCandidateWithObservations(commits, actor, request, 3000, observations)
	if err != nil || commit != nil || len(decision.Operations) != 0 || len(decision.Assignments) != 0 ||
		decision.Reason != "operation_budget_blocked" {
		t.Fatalf("zero remaining Claim slots consumed a logical request: %+v %v", decision, err)
	}
	after, _ := Serialize(next)
	if !bytes.Equal(before, after) {
		t.Fatal("no-Claim observed preview changed durable bytes")
	}
	state, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Requests[request.ID]; exists || len(state.Observations) != 0 {
		t.Fatal("no-Claim request or its preview observations became durable")
	}
	observation.ObservationID = "fourth"
	observations = append(observations, observation)
	if _, _, _, err := buildCandidateWithObservations(commits, actor, request, 3000, observations); err == nil ||
		!strings.Contains(err.Error(), "request_invalid") {
		t.Fatalf("observation preface exceeded installed operation budget: %v", err)
	}
}
