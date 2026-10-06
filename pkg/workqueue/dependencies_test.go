package workqueue

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialGenerationCannotReviveStaleObservations(t *testing.T) {
	commits := testGenesis(t, nil)
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "2", Number: "7"}
	node := testNode(t, commits, "credential-gated")
	node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
	commits = testSubmit(t, commits, "submit", node)
	observation := Observation{
		Kind: "Observation", ObservationID: "original-ready", Resource: resource, Condition: "completed",
		State: "ready", ObservedAt: 3500, CredentialGeneration: "initial",
		ReadStatus: "ok", StateReason: "completed", ResourceState: "closed",
	}
	commits = testOperations(t, commits, testActor("reconciler"), "observe", "observe", Op(observation))
	state, _ := Replay(commits)
	next, err := PlanNext(state, "default", 4000)
	if err != nil || next.WorkID != node.WorkID {
		t.Fatalf("current authorized observation should satisfy the gate: %+v %v", next, err)
	}
	control := func(generation string) Operation {
		return Op(map[string]any{"kind": "Control", "control": "credential_generation", "value": generation, "reason": "cutover"})
	}
	commits = testOperations(t, commits, testActor("administrator"), "rotate", "control", control("rotated"))
	commits = testOperations(t, commits, testActor("administrator"), "same-current", "control", control("rotated"))
	state, _ = Replay(commits)
	next, err = PlanNext(state, "default", 4000)
	if err != nil || next.WorkID != "" {
		t.Fatalf("cutover revived an earlier generation's observation: %+v %v", next, err)
	}
	request, err := NewRequest("revive", "control", testActor("administrator"),
		OperationsParameters{Operations: []Operation{control("initial")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, testActor("administrator"), request, 4000); err == nil ||
		!strings.Contains(err.Error(), "credential_generation_reused") {
		t.Fatalf("stale credential generation was permitted to revive: %v", err)
	}
}

func TestNativeTrustedIssueObservationAndGrantAreAtomic(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits := testGenesis(t, nil)
	node := testNode(t, commits, "gated")
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "9007199254740993", Number: "7"}
	node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
	commits = testSubmit(t, commits, "submit", node)
	installMockLog(t, mock, commits)
	mock.issue = map[string]any{
		"id": json.Number(resource.ResourceID), "number": 7, "state": "closed", "state_reason": "completed",
	}
	request, _ := NewRequest("grant", "dispatch_next", testActor("administrator"), DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	published, err := branch.Publish(context.Background(), testActor("administrator"), request)
	if err != nil || len(published.Commit.Operations) != 2 {
		t.Fatalf("trusted dependency refresh/grant not atomic: %+v %v", published, err)
	}
	var observation Observation
	_ = json.Unmarshal(published.Commit.Operations[0], &observation)
	var claim ClaimOperation
	_ = json.Unmarshal(published.Commit.Operations[1], &claim)
	if observation.State != "ready" || observation.Resource.ResourceID != "9007199254740993" ||
		len(claim.Observations) != 1 || claim.Observations[0] != observation.ObservationID || mock.resourceReads != 1 {
		t.Fatal("grant did not bind lossless authenticated resource observation")
	}
}

func TestNativeFailedDependencyReadPersistsUnknownWithoutConsumingGrant(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits := testGenesis(t, nil)
	node := testNode(t, commits, "gated")
	resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "2", Number: "7"}
	node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
	commits = testSubmit(t, commits, "submit", node)
	installMockLog(t, mock, commits)
	mock.issueStatus = 403
	request, _ := NewRequest("grant", "dispatch_next", testActor("administrator"), DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	published, err := branch.Publish(context.Background(), testActor("administrator"), request)
	if err != nil || len(published.Decision.Operations) != 0 {
		t.Fatalf("read failure fabricated readiness: %+v %v", published, err)
	}
	latest, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(latest)
	if err != nil || len(state.Claims) != 0 || state.Requests["grant"].ID != "" {
		t.Fatal("unknown observation consumed or charged grant request")
	}
	observation := state.Observations[resourceKey(resource, "completed")]
	if observation == nil || observation.State != "unknown" || observation.ReadStatus != "http_403" {
		t.Fatal("failed read did not persist normalized unknown evidence")
	}
}

func TestNativeResourceTypeAndMissingPredicateFailClosed(t *testing.T) {
	for _, reply := range []map[string]any{
		{"id": 2, "number": 7, "state": "closed", "pull_request": map[string]any{}},
		{"id": 2, "number": 7, "state": "closed"},
		{"id": 99, "number": 7, "state": "closed", "state_reason": "completed"},
		{"id": 2, "number": 7, "state": "closed", "state_reason": "not_planned"},
	} {
		branch, mock := newQueueAPI(t)
		mock.issue = reply
		resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "2", Number: "7"}
		observation := branch.readObservation(context.Background(), Dependency{Kind: "issue", Resource: &resource, Condition: "completed"}, "initial", "o1")
		if observation.State == "ready" {
			t.Fatal("PR-via-Issues, missing predicate, foreign identity, or not-planned Issue satisfied completed")
		}
	}
}
