package workqueue

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNativeUnsupportedContractCannotBorrowPositiveVerifier(t *testing.T) {
	for _, payload := range []string{
		`{"task":"missing"}`,
		`{"effect_contract":null}`,
		`{"effect_contract":{"version":2,"outputs":[],"no_writes":true}}`,
		`{"effect_contract":{"kind":"none","verified":true}}`,
		`{"effect_contract":{"version":1,"outputs":[]}}`,
		`{"effect_contract":{"version":1,"outputs":[{"type":"custom","min":1,"max":1},{"type":"custom","min":1,"max":1}]}}`,
	} {
		for _, disposition := range []string{"none", "partial"} {
			t.Run(payload+"/"+disposition, func(t *testing.T) {
				branch, mock := newQueueAPI(t)
				commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
					work.Payload = json.RawMessage(payload)
				}, func(policy *Policy) {
					pool := policy.Pools["default"]
					pool.Reconciliation.MaxAttempts = 1
					policy.Pools["default"] = pool
				})
				commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
				installMockLog(t, mock, commits)
				configureNativeRun(mock, assignment)
				calls := 0
				branch.DeliveryVerifier = func(_ context.Context, state Projection, claim ClaimState) (DeliveryVerification, error) {
					calls++
					if !sameJSON(state.Works[claim.WorkID].Payload, json.RawMessage(payload)) {
						t.Fatal("contract gate rewrote the frozen verifier context")
					}
					return DeliveryVerification{
						Verified: true, Descriptor: json.RawMessage(`{"ok":true}`),
						Receipt: "unsubstantiated-positive", Disposition: disposition, PositiveNoEffects: true,
					}, nil
				}
				recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "unsupported-contract")
				if err != nil || recovery.Reason != "delivery_failed_unknown" || recovery.Attempts != 1 || calls != 1 {
					t.Fatalf("unsupported intent borrowed positive verifier authority: %+v calls=%d err=%v", recovery, calls, err)
				}
				latest, err := branch.Read(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				state, err := Replay(latest)
				work := state.Works[assignment.Claims[0].WorkID]
				if err != nil || work.Barrier != "failed" || work.Disposition != "unknown" || state.Stats.Dispatches != 1 {
					t.Fatalf("unsupported delivery changed ownership, native release or conservative barrier: %+v %v", work, err)
				}
				var failure struct {
					Kind     string   `json:"kind"`
					Evidence Evidence `json:"evidence"`
				}
				if err := json.Unmarshal(latest[len(latest)-1].Operations[0], &failure); err != nil ||
					failure.Kind != "DeliveryFailure" || failure.Evidence.Attempts != 1 ||
					failure.Evidence.Kind != "terminal_run" || failure.Evidence.Effects != "unknown" ||
					failure.Evidence.Receipt != "" {
					t.Fatalf("unsupported intent retained a false positive receipt: %+v %v", failure, err)
				}
			})
		}
	}
}

func TestNativeFreshResultRequiresSupportedFrozenContract(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	claim := state.Claims[assignment.Claims[0].ClaimID]
	actor, err := branch.Authenticate(context.Background(), "reconciler")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
		calls++
		return DeliveryVerification{Verified: true, Descriptor: json.RawMessage(`{}`), Receipt: "unsubstantiated"}, nil
	}
	operation := mustOp(t, map[string]any{
		"kind": "Result", "work_id": claim.WorkID, "claim_id": claim.ClaimID,
		"completion_id": state.Works[claim.WorkID].CompletionID, "descriptor": json.RawMessage(`{}`),
		"evidence": Evidence{
			Kind: "delivery", Source: "verified_receipts", Repository: testRepository,
			Workflow: ".github/workflows/worker.lock.yml", Ref: strings.Repeat("0", 40),
			Principal: testPrincipal, CheckedAt: time.Now().UnixMilli(),
			RunID: "202", RunAttempt: 1, Receipt: "unsubstantiated",
		},
	})
	request, err := NewRequest("unsupported-result", "result", actor, OperationsParameters{Operations: []Operation{operation}})
	if err != nil {
		t.Fatal(err)
	}
	before := mock.head
	if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
		!strings.Contains(err.Error(), "delivery_contract_required:") {
		t.Fatalf("fresh Result did not require the frozen supported declaration: %v", err)
	}
	if calls != 0 || mock.refWrites != 0 || mock.head != before {
		t.Fatal("missing contract reached the positive verifier or mutated queue authority")
	}
}

func TestNativeUnsupportedContractCannotSettleBeforeTerminalRun(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t, func(policy *Policy) {
		pool := policy.Pools["default"]
		pool.Reconciliation.MaxAttempts = 1
		policy.Pools["default"] = pool
	})
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	mock.nativeRun.Status, mock.nativeRun.Conclusion = "in_progress", ""
	calls := 0
	branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
		calls++
		return DeliveryVerification{Verified: true, Descriptor: json.RawMessage(`{}`), Receipt: "unsupported"}, nil
	}
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "unsupported-in-progress")
	if err != nil || calls != 1 || recovery.Attempts != 1 || recovery.Reason != "delivery_unresolved" ||
		recovery.Publication != nil || mock.refWrites != 0 {
		t.Fatalf("unsupported declaration settled without native terminal proof: %+v calls=%d err=%v", recovery, calls, err)
	}
}
