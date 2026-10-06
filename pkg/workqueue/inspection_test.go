package workqueue

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestBeforeClaimUsesExactEarlierChargesAndKeepsSnapshotReadOnly(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = verifiedMemberResult(t, commits, assignment, 0)
	before, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	for index, member := range assignment.Claims {
		explanation, err := ExplainBeforeClaim(commits, member.ClaimID)
		if err != nil {
			t.Fatal(err)
		}
		if explanation.Selection.WorkID != member.WorkID || explanation.Selection.KeyPass != strconv.Itoa(index+1) ||
			explanation.Selection.ClassPass != strconv.Itoa(4*(index+1)) ||
			explanation.Position.Operation != index || explanation.Position.Commit != 2 ||
			explanation.BeforeTip != commits[1].ID || explanation.Tip != commits[len(commits)-1].ID ||
			explanation.At != commits[2].At || explanation.Status != "committed_claim_prefix" {
			t.Fatalf("explanation guessed a current winner or omitted earlier operation charges: %+v", explanation)
		}
		if explanation.CapacityBefore.Logical != index || explanation.CapacityAfter.Logical != index+1 ||
			explanation.CapacityAfter.Native != 1 || index > 0 && explanation.CapacityBefore.Native != 1 {
			t.Fatalf("original packed native reservation was not replayed exactly: %+v", explanation)
		}
	}
	request, err := ExplainRequest(commits, assignment.RequestID)
	if err != nil || len(request.Claims) != 3 || request.CommitID != assignment.CommitID {
		t.Fatalf("original committed request did not explain all fair-prefix members: %+v %v", request, err)
	}
	for index, member := range request.Claims {
		if member.ClaimID != assignment.Claims[index].ClaimID {
			t.Fatal("request explanation changed original fair-prefix order")
		}
	}
	after, err := Serialize(commits)
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only explanation changed the ledger or fairness debt")
	}
	if _, err := ExplainBeforeClaim(commits, "missing"); err == nil {
		t.Fatal("missing Claim was replaced with a current prediction")
	}
	if _, err := ExplainRequest(commits, "missing"); err == nil {
		t.Fatal("uncommitted request was reported as a grant")
	}
}

func TestBeforeClaimIncludesAtomicObservationPrefaceAndEarlierClaim(t *testing.T) {
	commits, observation := observedDispatchFixture(t)
	actor := testActor("dispatcher")
	actor.Workflow, actor.RunID, actor.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	request, err := NewRequest("historical-observed-grant", "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: 16, MaxDispatches: 16, MaxBytes: 48 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	next, _, decision, err := buildCandidateWithObservations(commits, actor, request, 3000, []Observation{observation})
	if err != nil || len(decision.Operations) != 2 {
		t.Fatalf("observed grant fixture failed: %+v %v", decision, err)
	}
	var claim ClaimOperation
	if err := json.Unmarshal(decision.Operations[1], &claim); err != nil {
		t.Fatal(err)
	}
	explanation, err := ExplainBeforeClaim(next, claim.ClaimID)
	if err != nil || explanation.Position.Operation != 2 ||
		explanation.Selection.WorkID != claim.WorkID || explanation.Selection.KeyPass != "2" ||
		!slices.Equal(explanation.Selection.Observations, []string{observation.ObservationID}) ||
		explanation.CapacityBefore.Logical != 1 || explanation.CapacityBefore.Native != 1 {
		t.Fatalf("historical prefix ignored its same-commit Observation or earlier Claim: %+v %v", explanation, err)
	}
	trace, err := TraceQueue(next, TraceOptions{ClaimID: claim.ClaimID, Limit: 256}, 3000)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range trace.Events {
		if event.Kind == "Observation" && event.ObservationID == observation.ObservationID {
			found = true
		}
	}
	if !found {
		t.Fatal("trace dropped the exact grant-bound dependency observation")
	}
}

func TestClaimTraceDistinguishesDeliveryFailureAndIndependentNativeRelease(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	evidence := terminalFor(state, assignment)
	evidence.Attempts, evidence.Effects = 5, "unknown"
	commits = testOperations(t, commits, testActor("reconciler"), "trace-failure", "delivery_failure", Op(map[string]any{
		"kind": "DeliveryFailure", "work_id": assignment.Claims[0].WorkID,
		"claim_id": assignment.Claims[0].ClaimID, "completion_id": state.Works[assignment.Claims[0].WorkID].CompletionID,
		"reason": "verification_exhausted", "disposition": "unknown", "evidence": evidence,
	}))
	operations := []Operation{}
	for _, member := range assignment.Claims[1:] {
		operations = append(operations, Op(map[string]any{
			"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
			"reason": "run_terminal", "retry_not_before": 34000,
		}))
	}
	operations = append(operations, Op(map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminalFor(state, assignment),
	}))
	commits = testOperations(t, commits, testActor("reconciler"), "trace-release", "release", operations...)
	trace, err := TraceQueue(commits, TraceOptions{ClaimID: assignment.Claims[0].ClaimID, Limit: 256}, 9000)
	if err != nil {
		t.Fatal(err)
	}
	failed, released := false, false
	for _, event := range trace.Events {
		switch event.Kind {
		case "DeliveryFailure":
			failed = event.Disposition == "unknown" && event.Evidence != nil &&
				event.Evidence.RunID == "202" && event.Evidence.RunAttempt == 1
		case "Release":
			released = event.DispatchID == assignment.DispatchID && event.Evidence != nil &&
				event.Evidence.Kind == "terminal_run"
		case "Result", "ClaimCancellation":
			t.Fatal("failed delivery trace fabricated Result or borrowed a sibling cancellation")
		}
	}
	if !failed || !released {
		t.Fatal("trace conflated delivery failure with independent native release")
	}
}

func TestClaimTraceIncludesExactLifecycleWithoutPayloadOrSiblingOutcomes(t *testing.T) {
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = verifiedMemberResult(t, commits, assignment, 0)
	commits = finishMember(t, commits, assignment, 1, "cancelled")
	trace, err := TraceQueue(commits, TraceOptions{ClaimID: assignment.Claims[0].ClaimID, Limit: 256}, 9000)
	if err != nil {
		t.Fatal(err)
	}

	kinds := []string{}
	for _, event := range trace.Events {
		kinds = append(kinds, event.Kind)
		if event.ClaimID != "" && event.ClaimID != assignment.Claims[0].ClaimID {
			t.Fatal("sibling outcome was attributed to the original Claim")
		}
		if event.Kind == "Dispatch" && event.State == "bound" &&
			(event.Run == nil || event.Run.RunID != "202" || event.Evidence == nil || event.Evidence.RunAttempt != 1) {
			t.Fatal("trace dropped the exact immutable native binding")
		}
	}
	for _, required := range []string{"Policy", "Work", "Claim", "Dispatch", "Completion", "Result"} {
		if !slices.Contains(kinds, required) {
			t.Fatalf("trace dropped %s provenance: %v", required, kinds)
		}
	}
	if slices.Contains(kinds, "ClaimCancellation") || trace.Tip != commits[len(commits)-1].ID ||
		trace.At != 9000 || trace.TraceAvailability != "ledger_only" {
		t.Fatal("trace confused later sibling outcomes or required telemetry")
	}
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"payload"`, `"descriptor"`, `"receipt"`, `"parameters"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("trace disclosed %s", forbidden)
		}
	}
	first, err := TraceQueue(commits, TraceOptions{ClaimID: assignment.Claims[0].ClaimID, Limit: 2}, 9000)
	if err != nil || len(first.Events) != 2 || first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("trace page was not bounded: %+v %v", first, err)
	}
	second, err := TraceQueue(commits, TraceOptions{ClaimID: assignment.Claims[0].ClaimID, Offset: 2, Limit: 256}, 9000)
	if err != nil || !sameJSON(append(first.Events, second.Events...), trace.Events) {
		t.Fatal("pagination reordered or lost exact causal events")
	}
	request, err := TraceQueue(commits, TraceOptions{RequestID: assignment.RequestID, Limit: 256}, 9000)
	if err != nil {
		t.Fatal(err)
	}
	claims := []string{}
	for _, event := range request.Events {
		if event.Kind == "Claim" {
			claims = append(claims, event.ClaimID)
		}
	}
	if len(claims) != 3 || claims[2] != assignment.Claims[2].ClaimID {
		t.Fatal("request trace lost original batch membership")
	}
	bindingTrace, err := TraceQueue(commits, TraceOptions{RequestID: "bind", Limit: 256}, 9000)
	if err != nil {
		t.Fatal(err)
	}
	bindingClaims := 0
	for _, event := range bindingTrace.Events {
		if event.Kind == "Claim" {
			bindingClaims++
		}
	}
	if bindingClaims != 3 {
		t.Fatal("binding request trace omitted its original group grants and member lifecycle")
	}
	for _, options := range []TraceOptions{
		{Limit: 10}, {ClaimID: "missing", Limit: 10},
		{RequestID: "missing", Limit: 10}, {ClaimID: assignment.Claims[0].ClaimID, Limit: 257},
		{ClaimID: assignment.Claims[0].ClaimID, RequestID: assignment.RequestID, Limit: 10},
		{ClaimID: assignment.Claims[0].ClaimID, Offset: 999, Limit: 10},
	} {
		if _, err := TraceQueue(commits, options, 9000); err == nil {
			t.Fatalf("invalid trace scope/pagination silently accepted: %+v", options)
		}
	}
}
