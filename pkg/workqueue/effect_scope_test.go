package workqueue

import (
	"strings"
	"testing"
)

func boundEffectAssignment(t *testing.T) ([]QueueCommit, Assignment, EffectResource) {
	t.Helper()
	subject := Resource{
		Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "2", Number: "7",
	}
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		work.Subject = &subject
	})
	target := EffectResource{
		"kind": subject.Kind, "host": subject.Host, "repository": subject.Repository,
		"repository_id": subject.RepositoryID, "resource_id": subject.ResourceID, "number": subject.Number,
	}
	return commits, assignment, target
}

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

func retireAssignmentEpoch(t *testing.T, commits []QueueCommit, assignment Assignment) []QueueCommit {
	t.Helper()
	for index := range assignment.Claims {
		commits = finishMember(t, commits, assignment, index, "completed")
		commits = verifiedMemberResult(t, commits, assignment, index)
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	commits = testOperations(t, commits, testActor("reconciler"), "drained-native-release", "release", Op(map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminalFor(state, assignment),
	}))
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if !state.quiescent() {
		t.Fatal("all independent outcomes and native reservation must settle before the Policy transition")
	}
	return testOperations(t, commits, testActor("administrator"), "drained-policy-transition", "policy", Op(map[string]any{
		"kind": "Policy", "epoch": "after-drained-epoch", "policy": *state.Policy,
	}))
}

func TestEffectsStopAtVerifiedResultWithoutClosingSiblingScope(t *testing.T) {
	commits, assignment, target := boundEffectAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = finishMember(t, commits, assignment, 1, "completed")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	for _, handle := range []string{"h1", "h2"} {
		if err := AuthorizeEffect(state, workerActor(assignment, handle), target); err != nil {
			t.Fatalf("pending completed member %s lost effect authority: %v", handle, err)
		}
	}
	commits = verifiedMemberResult(t, commits, assignment, 0)
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h1"), target); err == nil ||
		!strings.HasPrefix(err.Error(), "claim_effects_unauthorized:") {
		t.Fatalf("verified Result retained effect authority: %v", err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h2"), target); err != nil {
		t.Fatalf("completed delivery-pending sibling lost independent effect authority: %v", err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h3"), target); err == nil {
		t.Fatal("open sibling borrowed completed effect authority")
	}
}

func TestVerifiedResultClosesFreshWorkerControlsAndPreservesAcknowledgments(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	actor := workerActor(assignment, "h1")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewChildWork(state, actor, []byte(`{"task":"child"}`), "before-result", 4000)
	if err != nil {
		t.Fatal(err)
	}
	observation := Observation{
		Kind: "Observation", ObservationID: "before-result-observation",
		Resource: Resource{
			Kind: "issue", Host: "github.com", Repository: testRepository,
			RepositoryID: "1", ResourceID: "2", Number: "7",
		},
		Condition: "completed", State: "ready", ObservedAt: 4000,
		CredentialGeneration: state.CredentialGeneration, ReadStatus: "ok",
		ResourceState: "closed", StateReason: "completed",
	}
	requests := []Request{}
	accepted := map[string]string{}
	for _, intent := range []struct {
		kind   string
		params any
	}{
		{"submit", SubmitParameters{Nodes: []WorkDefinition{child}}},
		{"observe", OperationsParameters{Operations: []Operation{Op(observation)}}},
		{"dispatch_next", DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10}},
	} {
		request, err := NewRequest("before-result-"+intent.kind, intent.kind, actor, intent.params)
		if err != nil {
			t.Fatal(err)
		}
		next, commit, _, err := BuildCandidate(commits, actor, request, 4000)
		if err != nil || commit == nil {
			t.Fatalf("delivery-pending %s did not produce its scoped control: %v", intent.kind, err)
		}
		commits = next
		requests = append(requests, request)
		accepted[request.ID] = commit.ID
	}
	commits = verifiedMemberResult(t, commits, assignment, 0)
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("child-normalization", func(t *testing.T) {
		if _, err := NewChildWork(state, actor, []byte(`{}`), "after-result", 5000); err == nil ||
			!strings.HasPrefix(err.Error(), "claim_effects_unauthorized:") {
			t.Fatalf("verified Result retained fresh child normalization authority: %v", err)
		}
	})
	if _, _, err := state.completedWorkerScope(actor); err != nil {
		t.Fatalf("fresh-control fence changed the retained completed-Claim query: %v", err)
	}
	before, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		t.Run(request.Kind, func(t *testing.T) {
			fresh, err := NewRequest("after-result-"+request.Kind, request.Kind, actor, request.Parameters)
			if err != nil {
				t.Fatal(err)
			}
			if _, commit, _, err := BuildCandidate(commits, actor, fresh, 5000); err == nil ||
				commit != nil || !strings.HasPrefix(err.Error(), "claim_effects_unauthorized:") {
				t.Fatalf("verified Result retained fresh %s control authority: %v", request.Kind, err)
			}
			_, recovered, decision, err := BuildCandidate(commits, actor, request, 5000)
			if err != nil || recovered == nil || recovered.ID != accepted[request.ID] || decision.Reason != "already_committed" {
				t.Fatalf("verified Result blocked accepted %s acknowledgment: %v", request.Kind, err)
			}
			if _, err := TraceQueue(commits, TraceOptions{RequestID: request.ID, Limit: 256}, 5000); err != nil {
				t.Fatalf("verified Result blocked read-only %s control inventory: %v", request.Kind, err)
			}
		})
	}
	after, err := Serialize(commits)
	if err != nil || string(after) != string(before) {
		t.Fatal("fresh-control rejection or accepted readback changed authoritative history")
	}
	terminal := terminalFor(state, assignment)
	operations := []Operation{}
	for _, member := range assignment.Claims {
		if state.Claims[member.ClaimID].State == "open" {
			operations = append(operations, Op(map[string]any{
				"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
				"reason": "native_terminal", "retry_not_before": 34000,
			}))
		}
	}
	operations = append(operations, Op(map[string]any{"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminal}))
	commits = testOperations(t, commits, testActor("reconciler"), "release-after-result", "release", operations...)
	for _, request := range requests {
		_, recovered, decision, err := BuildCandidate(commits, actor, request, 6000)
		if err != nil || recovered == nil || recovered.ID != accepted[request.ID] || decision.Reason != "already_committed" {
			t.Fatalf("Release blocked accepted %s acknowledgment: %v", request.Kind, err)
		}
		if _, err := TraceQueue(commits, TraceOptions{RequestID: request.ID, Limit: 256}, 6000); err != nil {
			t.Fatalf("Release blocked read-only %s control inventory: %v", request.Kind, err)
		}
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
	if err := AuthorizeEffect(state, actor, EffectResource{"repository": testRepository}); err == nil ||
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

func TestDrainedPolicyEpochRetiresEffectsAndPreservesAcceptedFinish(t *testing.T) {
	commits, assignment := boundAssignment(t)
	actor := workerActor(assignment, "h1")
	commits = retireAssignmentEpoch(t, commits, assignment)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if state.PolicyEpoch == assignment.PolicyEpoch || !state.quiescent() {
		t.Fatal("test did not install an actual new Policy epoch over a drained queue")
	}
	if err := AuthorizeEffect(state, actor, EffectResource{"repository": testRepository}); err == nil ||
		!strings.HasPrefix(err.Error(), "claim_ineffective:") {
		t.Fatalf("retained completed Claim authorized effects in a later drained epoch: %v", err)
	}
	continuation, err := NewRequest("after-drained-worker-control", "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, continuation, 5000); err == nil ||
		!strings.HasPrefix(err.Error(), "claim_ineffective:") {
		t.Fatalf("retired completed Claim retained fresh continuation authority: %v", err)
	}
	finish, err := NewRequest("finish-h1", "finish", actor, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: "h1", Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	next, recovered, decision, err := BuildCandidate(commits, actor, finish, 5000)
	if err != nil || recovered == nil || recovered.ID != state.Requests[finish.ID].ID ||
		decision.Reason != "already_committed" {
		t.Fatalf("drained epoch transition discarded accepted finish acknowledgment: %v", err)
	}
	if _, err := TraceQueue(commits, TraceOptions{RequestID: finish.ID, Limit: 256}, 5000); err != nil {
		t.Fatalf("drained epoch transition blocked read-only finish history: %v", err)
	}
	after, err := Serialize(next)
	if err != nil || string(before) != string(after) {
		t.Fatal("rejected effect/control or accepted acknowledgment changed retired authority")
	}
}
