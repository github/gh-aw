package workqueue

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func configureNativeRun(mock *queueAPI, assignment Assignment) {
	run := &NativeRun{
		ID: "202", RunAttempt: 1, Event: "workflow_dispatch", Status: "completed",
		Conclusion: "failure", Path: ".github/workflows/worker.lock.yml",
		DisplayTitle: "queue worker " + assignment.DispatchID,
	}
	run.HeadSHA = "0000000000000000000000000000000000000000"
	run.Actor.Login, run.Actor.ID, run.TriggeringActor.ID = "operator", json.Number(testPrincipal), json.Number(testPrincipal)
	run.Repository.FullName = testRepository
	mock.nativeRun = run
}

func TestNativePrincipalIsImmutableNumericIdentityNotLogin(t *testing.T) {
	branch, mock := newQueueAPI(t)
	mock.userID = "9007199254740993"
	actor, err := branch.Authenticate(context.Background(), "producer")
	if err != nil || actor.Principal != mock.userID.String() {
		t.Fatalf("authenticated principal rounded or derived from login: %+v %v", actor, err)
	}
	mock.userID = "0"
	if _, err := branch.Authenticate(context.Background(), "producer"); err == nil {
		t.Fatal("nonpositive actor identity authenticated")
	}
	mock.userID = json.Number(testPrincipal)
	commits, assignment := boundAssignment(t)
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	mock.nativeRun.Actor.Login = "renamed-operator"
	if evidence, err := branch.InspectEvidence(context.Background(), assignment.DispatchID); err != nil ||
		evidence.Evidence.Principal != testPrincipal {
		t.Fatalf("mutable login invalidated original numeric identity: %+v %v", evidence, err)
	}
	mock.nativeRun.TriggeringActor.ID = "9999"
	if _, err := branch.InspectEvidence(context.Background(), assignment.DispatchID); err == nil {
		t.Fatal("foreign triggering actor impersonated original launch principal")
	}
	mock.nativeRun.TriggeringActor.ID = json.Number(testPrincipal)
	mock.nativeRun.TriggeringActor.ID = ""
	if _, err := branch.InspectEvidence(context.Background(), assignment.DispatchID); err != nil {
		t.Fatalf("missing optional triggering actor invalidated positive original actor identity: %v", err)
	}
	mock.nativeRun.TriggeringActor.ID = json.Number(testPrincipal)
	mock.nativeRun.Actor.ID = "9999"
	if _, err := branch.InspectEvidence(context.Background(), assignment.DispatchID); err == nil {
		t.Fatal("foreign actor ID borrowed matching original login")
	}
	mock.nativeRun.Actor.ID = json.Number(testPrincipal)
	for _, title := range []string{
		"queue worker " + assignment.DispatchID + "0",
		"queue worker " + assignment.DispatchID + "_foreign",
		"queue worker prefix_" + assignment.DispatchID,
	} {
		mock.nativeRun.DisplayTitle = title
		if _, err := branch.InspectEvidence(context.Background(), assignment.DispatchID); err == nil {
			t.Fatalf("overlapping/foreign dispatch title bound the wrong native group: %q", title)
		}
	}
	mock.nativeRun.DisplayTitle = "custom worker (" + assignment.DispatchID + ") diagnostic suffix"
	if _, err := branch.InspectEvidence(context.Background(), assignment.DispatchID); err != nil {
		t.Fatalf("exact delimited dispatch token rejected valid custom title: %v", err)
	}
}

func TestNativeTerminalRecoveryPreservesCompletedMembers(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed")
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	recovery, err := branch.Reconcile(context.Background(), assignment.DispatchID, "reconcile")
	if err != nil || recovery.Reason != "native_released_delivery_independent" {
		t.Fatalf("native terminal reconciliation failed: %+v %v", recovery, err)
	}
	latest, _ := branch.Read(context.Background())
	state, err := Replay(latest)
	if err != nil || state.Stats.Dispatches != 0 || state.Stats.Completed != 1 ||
		state.Works[assignment.Claims[0].WorkID].Barrier != "pending" {
		t.Fatal("terminal recovery rolled back completion or fabricated Result")
	}
}

func TestNativeResultNeedsScopedVerificationCapability(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	if _, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "result"); err == nil {
		t.Fatal("actor/evidence strings substituted for a trusted verifier")
	}
	calls := 0
	branch.DeliveryVerifier = func(_ context.Context, state Projection, claim ClaimState) (DeliveryVerification, error) {
		calls++
		if claim.ClaimID != assignment.Claims[0].ClaimID || state.Works[claim.WorkID].State != "completed" {
			t.Fatal("verifier lost canonical Claim scope")
		}
		return DeliveryVerification{Verified: true, Descriptor: json.RawMessage(`{"ok":true}`), Receipt: "verified-h1"}, nil
	}
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "result")
	if err != nil || recovery.Reason != "delivery_verified" || calls != 2 {
		t.Fatalf("verified delivery recovery failed: %+v calls=%d err=%v", recovery, calls, err)
	}
	latest, _ := branch.Read(context.Background())
	state, err := Replay(latest)
	if err != nil || state.Works[assignment.Claims[0].WorkID].Barrier != "verified" || state.Stats.Dispatches != 1 {
		t.Fatal("Result failed to settle independently of native reservation")
	}
}

func TestNativeMissingReceiptsBecomeUnknownNotNone(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	calls := 0
	branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
		calls++
		return DeliveryVerification{Disposition: "none"}, nil
	}
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "failure")
	if err != nil || recovery.Reason != "delivery_failed_unknown" || calls != 5 {
		t.Fatalf("unknown delivery did not exhaust finite verification: %+v calls=%d err=%v", recovery, calls, err)
	}
	latest, _ := branch.Read(context.Background())
	state, err := Replay(latest)
	if err != nil || state.Works[assignment.Claims[0].WorkID].Disposition != "unknown" ||
		state.Works[assignment.Claims[0].WorkID].Barrier != "failed" {
		t.Fatal("missing receipt was mislabeled positive proof of no effects")
	}
}

func TestNativeFailureDispositionPreservesPositiveReceipts(t *testing.T) {
	tests := []struct {
		name         string
		verification DeliveryVerification
		disposition  string
		receipt      string
	}{
		{"partial-without-receipt", DeliveryVerification{Disposition: "partial"}, "unknown", ""},
		{"partial-with-receipt", DeliveryVerification{Disposition: "partial", Receipt: "verified-partial"}, "partial", "verified-partial"},
		{"none-with-positive-proof", DeliveryVerification{Disposition: "none", Receipt: "verified-none", PositiveNoEffects: true}, "none", "verified-none"},
		{"none-without-positive-proof", DeliveryVerification{Disposition: "none", Receipt: "unproven-none"}, "unknown", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits, assignment := boundAssignment(t, func(policy *Policy) {
				pool := policy.Pools["default"]
				pool.Reconciliation.MaxAttempts = 1
				policy.Pools["default"] = pool
			})
			commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
			installMockLog(t, mock, commits)
			configureNativeRun(mock, assignment)
			branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
				return test.verification, nil
			}
			recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "failure")
			if err != nil || recovery.Reason != "delivery_failed_"+test.disposition {
				t.Fatalf("failure classification lost trusted receipt scope: %+v %v", recovery, err)
			}
			latest, err := branch.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var failure struct {
				Disposition string   `json:"disposition"`
				Evidence    Evidence `json:"evidence"`
			}
			if err := json.Unmarshal(latest[len(latest)-1].Operations[0], &failure); err != nil {
				t.Fatal(err)
			}
			if failure.Disposition != test.disposition || failure.Evidence.Effects != test.disposition ||
				failure.Evidence.Receipt != test.receipt || failure.Evidence.Attempts != 1 {
				t.Fatalf("failure evidence did not preserve conservative classification and receipt: %+v", failure)
			}
		})
	}
}

func TestNativeMalformedVerifiedDescriptorsFailWithoutPublishing(t *testing.T) {
	for _, descriptor := range []string{`null`, `[]`, `{"a":1,"a":2}`, `{"bad":1e0}`} {
		t.Run(descriptor, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits, assignment := boundAssignment(t)
			commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
			installMockLog(t, mock, commits)
			configureNativeRun(mock, assignment)
			branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
				return DeliveryVerification{Verified: true, Descriptor: json.RawMessage(descriptor), Receipt: "receipt"}, nil
			}
			if _, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "result"); err == nil {
				t.Fatal("malformed descriptor accepted")
			}
			if mock.refWrites != 0 {
				t.Fatal("invalid verifier data reached authoritative publication")
			}
		})
	}
}

func TestNoEffectsProofCannotPrecedeNativeTermination(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	commits = finishMember(t, commits, assignment, 0, "completed", time.Now().UnixMilli())
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	mock.nativeRun.Status, mock.nativeRun.Conclusion = "in_progress", ""
	branch.DeliveryVerifier = func(context.Context, Projection, ClaimState) (DeliveryVerification, error) {
		mock.nativeRun.Status, mock.nativeRun.Conclusion = "completed", "failure"
		return DeliveryVerification{Disposition: "none", PositiveNoEffects: true, Receipt: "early-receipt"}, nil
	}
	recovery, err := branch.RecoverDelivery(context.Background(), assignment.Claims[0].WorkID, "failure")
	if err != nil || recovery.Reason != "delivery_failed_unknown" {
		t.Fatalf("preterminal proof was treated as definitive none: %+v %v", recovery, err)
	}
}

func TestNativePrelaunchCancellationLosesToStartMarker(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	reserved := commits[:len(commits)-2]
	installMockLog(t, mock, reserved)
	mock.concurrent = func() { installMockLog(t, mock, commits[:len(commits)-1]) }
	if _, err := branch.CancelReserved(context.Background(), assignment.DispatchID, "cancel"); err == nil {
		t.Fatal("stale prelaunch proof released a newly started dispatch")
	}
	latest, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(latest)
	if err != nil || state.Dispatches[assignment.DispatchID].Released || state.Stats.Claimed != 3 {
		t.Fatalf("start-marker race lost logical/native ownership: %+v %v", state.Stats, err)
	}
}

func TestNativeTerminalRecoveryExhaustedBudgetCancelsWork(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	genesis := &commits[0]
	var initial struct {
		Kind   string `json:"kind"`
		Epoch  string `json:"epoch"`
		Policy Policy `json:"policy"`
	}
	if err := json.Unmarshal(genesis.Operations[0], &initial); err != nil {
		t.Fatal(err)
	}
	pool := initial.Policy.Pools["default"]
	pool.Retry.MaxAttempts = 1
	initial.Policy.Pools["default"] = pool
	genesis.Operations[0] = Op(initial)
	genesis.Request, _ = NewRequest(genesis.Request.ID, "policy", genesis.Actor, OperationsParameters{Operations: genesis.Operations})
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	if _, err := branch.Reconcile(context.Background(), assignment.DispatchID, "release"); err != nil {
		t.Fatal(err)
	}
	latest, _ := branch.Read(context.Background())
	state, err := Replay(latest)
	if err != nil || state.Stats.Cancelled != 3 || state.Stats.Dispatches != 0 {
		t.Fatalf("exhausted retry left Work retryable or native capacity occupied: %+v %v", state.Stats, err)
	}
}

func TestHistoricalNativeEvidenceUsesFrozenProfileAfterEpochChange(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, assignment := boundAssignment(t)
	for index := range assignment.Claims {
		commits = finishMember(t, commits, assignment, index, "cancelled")
	}
	cancellations := []Operation{}
	for _, member := range assignment.Claims {
		cancellations = append(cancellations, Op(map[string]string{
			"kind": "WorkCancellation", "work_id": member.WorkID, "reason": "operator_cancelled",
		}))
	}
	commits = testOperations(t, commits, testActor("administrator"), "cancel-work", "cancel_work", cancellations...)
	state, _ := Replay(commits)
	commits = testOperations(t, commits, testActor("reconciler"), "release", "release", Op(map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": terminalFor(state, assignment),
	}))
	state, _ = Replay(commits)
	policy := state.Policy
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Ref = "1111111111111111111111111111111111111111"
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	commits = testOperations(t, commits, testActor("administrator"), "new-epoch", "policy", Op(map[string]any{
		"kind": "Policy", "epoch": "next", "policy": policy,
	}))
	installMockLog(t, mock, commits)
	configureNativeRun(mock, assignment)
	evidence, err := branch.InspectEvidence(context.Background(), assignment.DispatchID)
	if err != nil || evidence.Reason != "terminal_run" {
		t.Fatalf("new policy revision invalidated historical authenticated evidence: %+v %v", evidence, err)
	}
}
