package workqueue

import (
	"strings"
	"testing"
)

func TestReleaseBackoffUsesOnlyItsOwnValidatedNativeProof(t *testing.T) {
	commits, original := boundAssignment(t)
	work := testNode(t, commits, "other")
	commits = testSubmit(t, commits, "other-submit", work)
	commits, decision := testGrant(t, commits, "other-grant", 1, 1)
	other := decision.Assignments[0]
	sender := testActor("dispatcher")
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, commits, sender, "other-start", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": other.DispatchID, "state": "started", "sender": sender,
	}))
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	binding := *state.Dispatches[original.DispatchID].Run
	binding.RunID = "303"
	proof := terminalFor(state, other)
	proof.Kind, proof.RunID, proof.CheckedAt = "reconciliation", binding.RunID, 4000
	commits = testOperations(t, commits, testActor("reconciler"), "other-bind", "dispatch", Op(map[string]any{
		"kind": "Dispatch", "dispatch_id": other.DispatchID, "state": "bound", "run": binding, "evidence": proof,
	}))
	state, err = Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	firstProof := terminalFor(state, original)
	secondProof := terminalFor(state, other)
	secondProof.RunID, secondProof.CheckedAt = "303", 9000
	for _, test := range []struct {
		name       string
		retry      int64
		proof      Evidence
		releaseOwn bool
		code       string
	}{
		{"delayed publication retains own proof origin", 39000, secondProof, true, ""},
		{"older foreign proof cannot shorten backoff", 38000, secondProof, true, "retry_invalid"},
		{"no own Release uses decision time", 39000, secondProof, false, "retry_invalid"},
		{"foreign native run proof", 39000, firstProof, true, "terminal_evidence_required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			operations := []Operation{}
			for _, member := range original.Claims {
				operations = append(operations, Op(map[string]any{
					"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
					"reason": "native_terminal", "retry_not_before": 34000,
				}))
			}
			member := other.Claims[0]
			operations = append(operations, Op(map[string]any{
				"kind": "ClaimCancellation", "work_id": member.WorkID, "claim_id": member.ClaimID,
				"reason": "native_terminal", "retry_not_before": test.retry,
			}), Op(map[string]any{"kind": "Release", "dispatch_id": original.DispatchID, "evidence": firstProof}))
			if test.releaseOwn {
				operations = append(operations, Op(map[string]any{
					"kind": "Release", "dispatch_id": other.DispatchID, "evidence": test.proof,
				}))
			}
			actor := testActor("reconciler")
			request, err := NewRequest(test.name, "release", actor, OperationsParameters{Operations: operations})
			if err != nil {
				t.Fatal(err)
			}
			next, _, _, err := BuildCandidate(commits, actor, request, 50000)
			if test.code != "" {
				if err == nil || !strings.HasPrefix(err.Error(), test.code+":") {
					t.Fatalf("expected %s, got %v", test.code, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			projection, err := Replay(next)
			if err != nil || !projection.Dispatches[original.DispatchID].Released ||
				!projection.Dispatches[other.DispatchID].Released ||
				projection.Works[work.WorkID].RetryNotBefore != 39000 {
				t.Fatalf("delayed release changed independent native proof/backoff: %v", err)
			}
		})
	}
}
