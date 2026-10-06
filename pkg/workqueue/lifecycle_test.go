package workqueue

import (
	"encoding/json"
	"strings"
	"testing"
)

func boundAssignment(t *testing.T, updates ...func(*Policy)) ([]QueueCommit, Assignment) {
	t.Helper()
	commits := testGenesis(t, func(policy *Policy) {
		pool := policy.Pools["default"]
		profile := pool.Profiles["default"]
		profile.MaxClaims = 3
		pool.Profiles["default"] = profile
		policy.Pools["default"] = pool
		for _, update := range updates {
			update(policy)
		}
	})
	a, b, c := testNode(t, commits, "a"), testNode(t, commits, "b"), testNode(t, commits, "c")
	commits = testSubmit(t, commits, "submit", a, b, c)
	commits, decision := testGrant(t, commits, "grant", 3, 1)
	assignment := decision.Assignments[0]
	sender := testActor("dispatcher")
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, commits, sender, "start", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender,
	}))
	state, _ := Replay(commits)
	profile := state.Policy.Pools["default"].Profiles["default"]
	binding := RunBinding{
		RunID: "202", RunAttempt: 1, Repository: testRepository, Workflow: profile.Workflow,
		Ref: profile.Ref, Principal: profile.Principal, Event: "workflow_dispatch",
	}
	evidence := Evidence{
		Kind: "reconciliation", Source: "github_api", Repository: testRepository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal,
		CheckedAt: 4000, RunID: "202", RunAttempt: 1,
	}
	commits = testOperations(t, commits, testActor("reconciler"), "bind", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": binding, "evidence": evidence,
	}))
	return commits, assignment
}

func workerActor(assignment Assignment, handle string) Actor {
	actor := testActor("worker")
	actor.Workflow, actor.RunID, actor.RunAttempt = ".github/workflows/worker.lock.yml", "202", 1
	actor.DispatchID, actor.ClaimHandle = assignment.DispatchID, handle
	return actor
}

func finishMember(t *testing.T, commits []QueueCommit, assignment Assignment, member int, outcome string, times ...int64) []QueueCommit {
	t.Helper()
	actor := workerActor(assignment, assignment.Claims[member].Handle)
	request, err := NewRequest("finish-"+assignment.Claims[member].Handle, "finish", actor, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: actor.ClaimHandle, Outcome: outcome,
	})
	if err != nil {
		t.Fatal(err)
	}
	at := int64(4000)
	if len(times) > 0 {
		at = times[0]
	}
	next, _, _, err := BuildCandidate(commits, actor, request, at)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func terminalFor(state Projection, assignment Assignment) Evidence {
	profile := state.Policy.Pools[assignment.Pool].Profiles[assignment.WorkerProfile]
	return Evidence{
		Kind: "terminal_run", Source: "github_api", Repository: testRepository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal,
		CheckedAt: 4000, RunID: "202", RunAttempt: 1, Status: "completed", Conclusion: "failure",
	}
}

func TestIndependentMixedOutcomesAndNativeReservation(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = finishMember(t, commits, assignment, 1, "cancelled")
	state, _ := Replay(commits)
	if state.Stats.Completed != 1 || state.Stats.Available != 1 || state.Stats.Claimed != 1 ||
		state.Stats.Dispatches != 1 {
		t.Fatalf("one member outcome settled the shared native reservation: %+v", state.Stats)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h1")); err != nil {
		t.Fatal(err)
	}
	for _, handle := range []string{"h2", "h3"} {
		if err := AuthorizeEffect(state, workerActor(assignment, handle)); err == nil {
			t.Fatal("cancelled/open sibling borrowed completed member authority")
		}
	}
	if _, err := NormalizeClaimHandle(assignment, nil); err == nil || !strings.Contains(err.Error(), "claim_scope_required") {
		t.Fatal("closing members collapsed original multi-Claim assignment")
	}
	terminal := terminalFor(state, assignment)
	third := assignment.Claims[2]
	commits = testOperations(t, commits, testActor("reconciler"), "release", "release",
		Op(map[string]any{"kind": "ClaimCancellation", "work_id": third.WorkID, "claim_id": third.ClaimID, "reason": "run_terminal", "retry_not_before": 34000}),
		Op(map[string]any{"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminal}))
	state, err := Replay(commits)
	if err != nil || state.Stats.Completed != 1 || state.Stats.Dispatches != 0 || state.Stats.Available != 2 {
		t.Fatalf("terminal recovery rolled back completed sibling: %+v %v", state.Stats, err)
	}
	if state.Works[assignment.Claims[0].WorkID].Barrier != "pending" {
		t.Fatal("native termination invented delivery success")
	}
}

func TestCompletionIsNotResultAndBarriersAreExclusive(t *testing.T) {
	commits, assignment := boundAssignment(t)
	root := assignment.Claims[0]
	state, _ := Replay(commits)
	child := testNode(t, commits, "child")
	child.DependsOn = []Dependency{{Kind: "work", WorkID: root.WorkID}}
	commits = testSubmit(t, commits, "child-submit", child)
	commits = finishMember(t, commits, assignment, 0, "completed")
	state, _ = Replay(commits)
	explanation, _ := ExplainWork(state, child.WorkID, 4000)
	if explanation.Ready || explanation.Reason != "dependency_result_unavailable" {
		t.Fatal("Completion released predecessor readiness without verified Result")
	}
	terminal := terminalFor(state, assignment)
	delivery := terminal
	delivery.Kind, delivery.Source, delivery.Receipt = "delivery", "verified_receipts", "verified-h1"
	completionID := state.Works[root.WorkID].CompletionID
	result := Op(map[string]any{
		"kind": "Result", "work_id": root.WorkID, "claim_id": root.ClaimID,
		"completion_id": completionID, "descriptor": map[string]any{"ok": true}, "evidence": delivery,
	})
	commits = testOperations(t, commits, testActor("reconciler"), "result", "result", result)
	state, _ = Replay(commits)
	explanation, _ = ExplainWork(state, child.WorkID, 4000)
	if !explanation.Ready {
		t.Fatalf("verified Result did not release child: %+v", explanation)
	}
	terminal.Attempts, terminal.Effects = 5, "unknown"
	failure := Op(map[string]any{
		"kind": "DeliveryFailure", "work_id": root.WorkID, "claim_id": root.ClaimID,
		"completion_id": completionID, "reason": "receipts_unknown", "disposition": "unknown", "evidence": terminal,
	})
	request, _ := NewRequest("failure", "delivery_failure", testActor("reconciler"), OperationsParameters{Operations: []Operation{failure}})
	if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), request, 4000); err == nil ||
		!strings.Contains(err.Error(), "delivery_barrier_conflict") {
		t.Fatalf("Result and DeliveryFailure both became authoritative: %v", err)
	}
}

func TestUncertainRunNeverExpiresOrReleases(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = commits[:len(commits)-1]
	commits = testOperations(t, commits, testActor("reconciler"), "uncertain", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "uncertain", "reason": "response_lost",
	}))
	state, _ := Replay(commits)
	release := Op(map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID,
		"evidence": Evidence{Kind: "nonlaunch", Source: "github_api", Repository: testRepository,
			Workflow: ".github/workflows/worker.lock.yml", Ref: state.Policy.Pools["default"].Profiles["default"].Ref,
			Principal: testPrincipal, CheckedAt: 4000},
	})
	request, _ := NewRequest("unsafe-release", "release", testActor("reconciler"), OperationsParameters{Operations: []Operation{release}})
	if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), request, 900000); err == nil {
		t.Fatal("deadline/missing native binding was interpreted as proof of nonlaunch")
	}
}

func TestRunAttemptAndScopeCannotBeForged(t *testing.T) {
	commits, assignment := boundAssignment(t)
	state, _ := Replay(commits)
	for _, mutate := range []func(*Actor){
		func(a *Actor) { a.RunAttempt = 2 },
		func(a *Actor) { a.RunID = "wrong" },
		func(a *Actor) { a.ClaimHandle = "foreign" },
		func(a *Actor) { a.Principal = "foreign" },
		func(a *Actor) { a.Workflow = "foreign" },
	} {
		actor := workerActor(assignment, "h1")
		mutate(&actor)
		if _, err := state.scopedClaim(actor, assignment.DispatchID, "h1"); err == nil {
			t.Fatal("forged attempt/identity/scope acquired Claim")
		}

	}
	selected := ""
	if _, err := NormalizeClaimHandle(assignment, &selected); err == nil {
		t.Fatal("empty is not omitted")
	}
	scalar := Assignment{}
	if err := json.Unmarshal([]byte(`{"work_id":"w","claim_id":"c","work":{}}`), &scalar); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeClaimHandle(scalar, nil); err == nil {
		t.Fatal("legacy scalar assignment authorized native worker")
	}
}

func TestNativeFinishNormalizerPreservesAbsentAndNull(t *testing.T) {
	_, assignment := boundAssignment(t)
	for _, data := range []string{
		`{"outcome":"completed"}`, `{"outcome":"completed","claim_handle":null}`,
		`{"outcome":"completed","claim_handle":""}`, `{"outcome":"completed","claim_handle":"foreign"}`,
		`{"outcome":"completed","claim_id":"c1"}`, `{"outcome":"finished","claim_handle":"h1"}`,
	} {
		if _, err := NormalizeFinishIntent(assignment, []byte(data)); err == nil {
			t.Fatalf("invalid/ambiguous multi-Claim finish accepted: %s", data)
		}
	}
	if result, err := NormalizeFinishIntent(assignment, []byte(`{"outcome":"completed","claim_handle":"h1"}`)); err != nil || result.ClaimHandle != "h1" {
		t.Fatalf("explicit original member rejected: %+v %v", result, err)
	}
	assignment.Claims = assignment.Claims[:1]
	if _, err := NormalizeFinishIntent(assignment, []byte(`{"outcome":"completed"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeFinishIntent(assignment, []byte(`{"outcome":"completed","claim_handle":null}`)); err == nil {
		t.Fatal("null was silently treated as omitted for a one-Claim assignment")
	}
}

func TestCancellationMetadataIsImmutableAndIdempotent(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 1, "cancelled")
	operation := commits[len(commits)-1].Operations[0]
	var cancellation map[string]any
	_ = json.Unmarshal(operation, &cancellation)
	state, _ := Replay(commits)
	claim := state.Claims[assignment.Claims[1].ClaimID]
	if claim.CancellationReason != cancellation["reason"] || claim.RetryNotBefore != 34000 {
		t.Fatalf("Claim lost immutable cancellation metadata: %+v", claim)
	}
	duplicate, _ := NewRequest("duplicate-cancel", "cancel_claim", testActor("reconciler"), OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), duplicate, 4000); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"reason", "retry_not_before"} {
		changed := make(map[string]any, len(cancellation))
		for key, value := range cancellation {
			changed[key] = value
		}
		if field == "reason" {
			changed[field] = "changed"
		} else {
			changed[field] = 35000
		}
		request, _ := NewRequest("changed-"+field, "cancel_claim", testActor("reconciler"), OperationsParameters{Operations: []Operation{Op(changed)}})
		if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), request, 4000); err == nil {
			t.Fatalf("Claim cancellation %s changed after terminal fact", field)
		}
	}
	workCancellation := Op(map[string]string{
		"kind": "WorkCancellation", "work_id": assignment.Claims[2].WorkID, "reason": "operator_cancelled",
	})
	commits = testOperations(t, commits, testActor("administrator"), "cancel-work", "cancel_work", workCancellation)
	state, _ = Replay(commits)
	work := state.Works[assignment.Claims[2].WorkID]
	original := work.CancellationCommitID
	if work.CancellationReason != "operator_cancelled" || original != state.Tip ||
		state.Claims[assignment.Claims[2].ClaimID].CancellationReason != work.CancellationReason ||
		state.Stats.Dispatches != 1 {
		t.Fatal("Work cancellation lost reason or released an executing native reservation")
	}
	commits = testOperations(t, commits, testActor("administrator"), "same-work-cancel", "cancel_work", workCancellation)
	state, _ = Replay(commits)
	if state.Works[work.WorkID].CancellationCommitID != original {
		t.Fatal("idempotent cancellation rewrote original terminal provenance")
	}
	changed, _ := NewRequest("changed-work-cancel", "cancel_work", testActor("administrator"), OperationsParameters{Operations: []Operation{
		Op(map[string]string{"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "changed"}),
	}})
	if _, _, _, err := BuildCandidate(commits, testActor("administrator"), changed, 4000); err == nil {
		t.Fatal("Work terminal reason was overwritten")
	}
}

func TestDispatcherOriginMayDifferFromApprovedLaunchPrincipal(t *testing.T) {
	commits, assignment := boundAssignment(t)
	reserved := commits[:len(commits)-2]
	sender := testActor("dispatcher")
	sender.Principal = "2002"
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, reserved, sender, "different-origin-start", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender,
	}))
	state, err := Replay(commits)
	if err != nil || state.Dispatches[assignment.DispatchID].Sender.Principal == state.Dispatches[assignment.DispatchID].Profile.Principal {
		t.Fatalf("logical dispatcher origin must not masquerade as its dispatch credential: %v", err)
	}
	profile := state.Dispatches[assignment.DispatchID].Profile
	binding := RunBinding{
		RunID: "202", RunAttempt: 1, Repository: testRepository, Workflow: profile.Workflow,
		Ref: profile.Ref, Principal: profile.Principal, Event: "workflow_dispatch",
	}
	evidence := Evidence{
		Kind: "reconciliation", Source: "github_api", Repository: testRepository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal,
		CheckedAt: 4000, RunID: binding.RunID, RunAttempt: 1,
	}
	impostorRun, impostorEvidence := binding, evidence
	impostorRun.Principal, impostorEvidence.Principal = sender.Principal, sender.Principal
	operation := Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound",
		"run": impostorRun, "evidence": impostorEvidence,
	})
	request, err := NewRequest("sender-is-not-worker", "dispatch", testActor("reconciler"),
		OperationsParameters{Operations: []Operation{operation}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), request, 4000); err == nil ||
		!strings.HasPrefix(err.Error(), "run_binding_conflict:") {
		t.Fatalf("dispatcher origin replaced the approved native worker principal: %v", err)
	}
	commits = testOperations(t, commits, testActor("reconciler"), "different-origin-bind", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound",
		"run": binding, "evidence": evidence,
	}))
	commits = finishMember(t, commits, assignment, 0, "completed")
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	if state.Dispatches[assignment.DispatchID].Sender.Principal != sender.Principal ||
		state.Dispatches[assignment.DispatchID].Run.Principal != profile.Principal {
		t.Fatal("binding or Completion conflated logical sender and native worker principals")
	}
	worker := workerActor(assignment, "h1")
	if err := AuthorizeEffect(state, worker); err != nil {
		t.Fatalf("approved native worker lost authority under a different logical sender: %v", err)
	}
	worker.Principal = sender.Principal
	if err := AuthorizeEffect(state, worker); err == nil ||
		!strings.HasPrefix(err.Error(), "run_binding_conflict:") {
		t.Fatalf("logical dispatcher borrowed native worker effect authority: %v", err)
	}
}

func TestFinalRetryWorkerCancellationAtomicallyClosesWork(t *testing.T) {
	commits, assignment := boundAssignment(t, func(policy *Policy) {
		pool := policy.Pools["default"]
		pool.Retry.MaxAttempts = 1
		policy.Pools["default"] = pool
	})
	actor := workerActor(assignment, "h1")
	request, err := NewRequest("final-retry-cancel", "finish", actor, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: actor.ClaimHandle, Outcome: "cancelled",
	})
	if err != nil {
		t.Fatal(err)
	}
	next, commit, _, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil || commit == nil || len(commit.Operations) != 2 ||
		operationKind(commit.Operations[0]) != "ClaimCancellation" ||
		operationKind(commit.Operations[1]) != "WorkCancellation" || commit.Actor.Role != "worker" {
		t.Fatalf("final retry did not atomically close its original Work under worker authority: %v %+v", err, commit)
	}
	state, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	work := state.Works[assignment.Claims[0].WorkID]
	if work.State != "cancelled" || work.CancellationReason != "attempts_exhausted" ||
		state.Claims[assignment.Claims[0].ClaimID].State != "cancelled" ||
		state.Claims[assignment.Claims[1].ClaimID].State != "open" ||
		state.Dispatches[assignment.DispatchID].Released {
		t.Fatal("cold replay reopened exhausted Work, closed a sibling or released native capacity")
	}
	assertRecoveryReplayParity(t, next, "")
}

func TestNativeRunCannotBindAnotherDispatch(t *testing.T) {
	commits, original := boundAssignment(t)
	node := testNode(t, commits, "other")
	commits = testSubmit(t, commits, "other-submit", node)
	commits, decision := testGrant(t, commits, "other-grant", 1, 1)
	target := decision.Assignments[0]
	sender := testActor("dispatcher")
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, commits, sender, "other-start", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": target.DispatchID, "state": "started", "sender": sender,
	}))
	state, _ := Replay(commits)
	originalRun := *state.Dispatches[original.DispatchID].Run
	profile := state.Dispatches[target.DispatchID].Profile
	evidence := Evidence{
		Kind: "reconciliation", Source: "github_api", Repository: testRepository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal,
		CheckedAt: 4000, RunID: originalRun.RunID, RunAttempt: 1,
	}
	operation := Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": target.DispatchID, "state": "bound", "run": originalRun, "evidence": evidence,
	})
	request, _ := NewRequest("reuse-run", "dispatch", testActor("reconciler"), OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), request, 4000); err == nil ||
		!strings.Contains(err.Error(), "run_binding_conflict") {
		t.Fatalf("same native run acquired another group's claims: %v", err)
	}
	operations := []Operation{}
	for _, member := range original.Claims {
		operations = append(operations, Op(map[string]any{
			"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"reason": "native_terminal", "retry_not_before": 34000,
		}))
	}
	operations = append(operations, Op(map[string]any{
		"kind": "Release", "dispatch_id": original.DispatchID, "evidence": terminalFor(state, original),
	}))
	commits = testOperations(t, commits, testActor("reconciler"), "release-original", "release", operations...)
	if _, _, _, err := BuildCandidate(commits, testActor("reconciler"), request, 4000); err == nil {
		t.Fatal("released historical native run acquired new fair claims")
	}
}
