package workqueue

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	commits = testOperations(t, commits, testActor("reconciler"), "observe", "observe", mustOp(t, observation))
	state, _ := Replay(commits)
	next, err := PlanNext(state, "default", 4000)
	if err != nil || next.WorkID != node.WorkID {
		t.Fatalf("current authorized observation should satisfy the gate: %+v %v", next, err)
	}
	control := func(generation string) Operation {
		return mustOp(t, map[string]any{"kind": "Control", "control": "credential_generation", "value": generation, "reason": "cutover"})
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
	readFinished := make(chan int64, 1)
	mock.resourceRead = func() {
		time.Sleep(10 * time.Millisecond)
		readFinished <- time.Now().UnixMilli()
	}
	request, _ := NewRequest("grant", "dispatch_next", testActor("administrator"), DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	published, err := branch.Publish(context.Background(), testActor("administrator"), request)
	if err != nil || published.Commit == nil || len(published.Commit.Operations) != 2 {
		t.Fatalf("trusted dependency refresh/grant not atomic: %+v %v", published, err)
	}
	if completedAt := <-readFinished; published.Commit.At < completedAt {
		t.Fatalf("decision clock %d preceded actual API read %d", published.Commit.At, completedAt)
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

func TestNativeNoGrantDiscardsTentativeObservationsWithoutAnyWrite(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		reply      any
		state      string
		readStatus string
		staleReady bool
	}{
		{name: "unknown", status: 403, state: "unknown", readStatus: "http_403"},
		{name: "waiting", reply: map[string]any{"id": 2, "number": 7, "state": "open", "state_reason": nil},
			state: "waiting", readStatus: "ok"},
		{name: "not-completed", reply: map[string]any{"id": 2, "number": 7, "state": "closed", "state_reason": "not_planned"},
			state: "failed", readStatus: "ok"},
		{name: "missing-resource", status: 404, state: "unknown", readStatus: "http_404"},
		{name: "unknown-after-stale-ready", status: 403, state: "unknown", readStatus: "http_403", staleReady: true},
		{name: "waiting-after-stale-ready", reply: map[string]any{"id": 2, "number": 7, "state": "open", "state_reason": nil},
			state: "waiting", readStatus: "ok", staleReady: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits := testGenesis(t, nil)
			node := testNode(t, commits, "gated")
			resource := Resource{Kind: "issue", Host: "github.com", Repository: testRepository,
				RepositoryID: "1", ResourceID: "2", Number: "7"}
			node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
			commits = testSubmit(t, commits, "submit", node)
			if test.staleReady {
				commits = testOperations(t, commits, testActor("reconciler"), "seed-ready", "observe", mustOp(t, Observation{
					Kind: "Observation", ObservationID: "stale-ready", Resource: resource, Condition: "completed",
					State: "ready", ObservedAt: 3000, CredentialGeneration: "initial",
					ReadStatus: "ok", StateReason: "completed", ResourceState: "closed",
				}))
			}
			installMockLog(t, mock, commits)
			mock.issueStatus, mock.issue = test.status, test.reply
			before, err := Serialize(commits)
			if err != nil {
				t.Fatal(err)
			}
			initialState, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			initialProjection, err := canonicalValue(initialState)
			if err != nil {
				t.Fatal(err)
			}
			initialHead, initialLogs, initialCommits := mock.head, len(mock.logs), len(mock.commits)
			request, err := NewRequest("grant", "dispatch_next", testActor("administrator"), DispatchParameters{
				Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			proofs, err := branch.refreshForDispatch(context.Background(), initialState, "default", request.ID)
			if err != nil || len(proofs) != 1 || proofs[0].State != test.state ||
				proofs[0].ReadStatus != test.readStatus || proofs[0].ObservedAt <= commits[len(commits)-1].At {
				t.Fatalf("fixture did not produce a fresh nonempty tentative proof: %+v %v", proofs, err)
			}
			initialReads := mock.resourceReads
			published, err := branch.Publish(context.Background(), testActor("administrator"), request)
			if err != nil || published.Changed || published.Commit != nil || len(published.Decision.Operations) != 0 ||
				len(published.Decision.Assignments) != 0 || published.Decision.Tip != initialState.Tip {
				t.Fatalf("no-grant changed its decision or fabricated a publication: %+v %v", published, err)
			}
			latest, err := branch.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			after, err := Serialize(latest)
			if err != nil || string(before) != string(after) || mock.refWrites != 0 ||
				mock.head != initialHead || len(mock.logs) != initialLogs || len(mock.commits) != initialCommits {
				t.Fatal("no-grant published tentative evidence or changed physical authority")
			}
			state, err := Replay(latest)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := canonicalValue(state)
			if err != nil || string(projection) != string(initialProjection) || mock.resourceReads != initialReads+1 {
				t.Fatal("no-grant changed observations, request IDs, counters, ownership, debt or skipped native refresh")
			}
			if _, accepted := state.Requests[request.ID]; accepted {
				t.Fatal("no-grant consumed the original dispatch request")
			}
			for id := range state.Requests {
				if strings.HasPrefix(id, "observe_") {
					t.Fatalf("no-grant fabricated a derived observation request: %s", id)
				}
			}
		})
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
