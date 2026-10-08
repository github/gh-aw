package workqueue

import (
	"context"
	"encoding/json"
	"time"
)

// DeliveryVerifier is a trusted, read-only receipt/effect-contract adapter.
// It verifies existing scoped effects; it must never execute or retry effects.
// Configuring it is a host capability, not an actor/source string or CLI flag.
type DeliveryVerifier func(context.Context, Projection, ClaimState) (DeliveryVerification, error)

// RemediationVerifier validates domain-specific idempotence, compensation, or
// inspection-only scope before replacing a node with partial/unknown effects.
type RemediationVerifier func(context.Context, Projection, WorkDefinition) error

type DeliveryVerification struct {
	Verified          bool
	Descriptor        json.RawMessage
	Receipt           string
	Disposition       string
	PositiveNoEffects bool
}

type DeliveryRecovery struct {
	WorkID      string       `json:"work_id"`
	Reason      string       `json:"reason"`
	Attempts    int          `json:"attempts"`
	Publication *Publication `json:"publication,omitempty"`
}

type deliveryRecoveryState struct {
	actor Actor
	state Projection
	work  *WorkState
}

func deliveryVerificationExhausted(work *WorkState, recovery ReconciliationPolicy, attempts int, at int64) bool {
	return attempts >= recovery.MaxAttempts || at-work.completionAt >= recovery.DeadlineMS
}

// RecoverDelivery settles a barrier independently of native capacity. Missing
// proof remains unknown; neither an overall run success nor a missing artifact
// is interpreted as verified delivery or as proof that effects never began.
func (b Branch) RecoverDelivery(ctx context.Context, workID, requestID string) (DeliveryRecovery, error) {
	result := DeliveryRecovery{WorkID: workID}
	b, err := b.withClient()
	if err != nil {
		return result, err
	}
	recovered, err := b.readDeliveryRecovery(ctx, workID)
	if err != nil {
		return result, err
	}
	state, work := recovered.state, recovered.work
	if work.Barrier != "pending" {
		result.Reason = "already_" + work.Barrier
		return result, nil
	}
	recovery := state.Policy.Pools[work.Pool].Reconciliation
	now := time.Now()
	if b.DeliveryVerifier == nil && !deliveryVerificationExhausted(work, recovery, 0, now.UnixMilli()) {
		return result, queueError("delivery_verifier_required", "bind a trusted scoped receipt verifier before delivery reconciliation")
	}
	contractSupported := validateDeliveryContract(work.Payload) == nil
	claim := state.Claims[work.ClaimID]
	dispatch := state.Dispatches[claim.DispatchID]
	if dispatch.Run == nil {
		return result, queueError("launch_unresolved", "no authenticated native binding exists")
	}
	_, beforeVerification, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
	if err != nil {
		return result, err
	}
	remaining := max(0, min(recovery.DeadlineMS, work.completionAt+recovery.DeadlineMS-now.UnixMilli()))
	deadline := now.Add(time.Duration(remaining) * time.Millisecond)
	last := DeliveryVerification{Disposition: "unknown"}
	for attempt := 0; b.DeliveryVerifier != nil && attempt < recovery.MaxAttempts && time.Now().Before(deadline); attempt++ {
		verification, verifyErr := func() (DeliveryVerification, error) {
			callContext, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			return b.DeliveryVerifier(callContext, state, *claim)
		}()
		result.Attempts++
		if verifyErr == nil && contractSupported {
			last = verification
		}
		if verifyErr == nil && contractSupported && verification.Verified && verification.Receipt != "" {
			return b.publishVerifiedDelivery(ctx, recovered, requestID, verification, result)
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if attempt+1 < recovery.MaxAttempts {
			delay := min(time.Duration(50*(1<<min(attempt, 10)))*time.Millisecond, time.Until(deadline))
			timer := time.NewTimer(max(0, delay))
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return b.recoverDeliveryFailure(ctx, recovered, requestID, beforeVerification.Kind == "terminal_run", last, result)
}

func (b Branch) readDeliveryRecovery(ctx context.Context, workID string) (deliveryRecoveryState, error) {
	actor, err := b.Authenticate(ctx, "reconciler")
	if err != nil {
		return deliveryRecoveryState{}, err
	}
	commits, err := b.Read(ctx)
	if err != nil {
		return deliveryRecoveryState{}, err
	}
	state, err := Replay(commits)
	if err != nil {
		return deliveryRecoveryState{}, err
	}
	work := state.Works[workID]
	if work == nil || work.State != "completed" {
		return deliveryRecoveryState{}, queueError("result_invalid", "delivery recovery requires completed frozen ownership")
	}
	return deliveryRecoveryState{actor: actor, state: state, work: work}, nil
}

func (b Branch) publishVerifiedDelivery(ctx context.Context, recovered deliveryRecoveryState, requestID string, verification DeliveryVerification, result DeliveryRecovery) (DeliveryRecovery, error) {
	state, work := recovered.state, recovered.work
	claim := state.Claims[work.ClaimID]
	dispatch := state.Dispatches[claim.DispatchID]
	descriptor, err := Canonical(verification.Descriptor)
	var object map[string]any
	if err != nil || json.Unmarshal(descriptor, &object) != nil || object == nil ||
		int64(len(descriptor)) > state.Policy.Limits.ResultBytes {
		return result, queueError("result_invalid", "trusted verifier returned an invalid or oversized descriptor")
	}
	_, evidence, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
	if err != nil {
		return result, err
	}
	evidence.Kind, evidence.Source, evidence.Receipt = "delivery", "verified_receipts", verification.Receipt
	evidence.Status, evidence.Conclusion = "", ""
	if verification.Disposition == "none" || verification.Disposition == "partial" {
		evidence.Effects = verification.Disposition
	}
	operation, err := Op(map[string]any{
		"kind": "Result", "work_id": work.WorkID, "claim_id": claim.ClaimID,
		"completion_id": work.CompletionID, "descriptor": json.RawMessage(descriptor), "evidence": evidence,
	})
	if err != nil {
		return result, err
	}
	request, err := NewRequest(requestID, "result", recovered.actor, OperationsParameters{Operations: []Operation{operation}})
	if err != nil {
		return result, err
	}
	publication, err := b.Publish(ctx, recovered.actor, request)
	if err != nil {
		result.Reason = "delivery_unresolved"
		return result, err
	}
	result.Publication, result.Reason = &publication, "delivery_verified"
	return result, nil
}

func (b Branch) recoverDeliveryFailure(ctx context.Context, recovered deliveryRecoveryState, requestID string, previouslyTerminal bool, last DeliveryVerification, result DeliveryRecovery) (DeliveryRecovery, error) {
	state, work := recovered.state, recovered.work
	recovery := state.Policy.Pools[work.Pool].Reconciliation
	claim := state.Claims[work.ClaimID]
	dispatch := state.Dispatches[claim.DispatchID]
	_, terminal, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
	if err != nil {
		return result, err
	}
	if terminal.Kind != "terminal_run" ||
		!deliveryVerificationExhausted(work, recovery, result.Attempts, time.Now().UnixMilli()) {
		result.Reason = "delivery_unresolved"
		return result, nil
	}
	disposition := "unknown"
	if last.Disposition == "partial" && last.Receipt != "" {
		disposition = "partial"
		terminal.Receipt = last.Receipt
	} else if previouslyTerminal &&
		last.Disposition == "none" && last.PositiveNoEffects && last.Receipt != "" {
		disposition = "none"
		terminal.Receipt = last.Receipt
	}
	terminal.Attempts, terminal.Effects = result.Attempts, disposition
	operation, err := Op(map[string]any{
		"kind": "DeliveryFailure", "work_id": work.WorkID, "claim_id": claim.ClaimID,
		"completion_id": work.CompletionID, "reason": "delivery_verification_exhausted",
		"disposition": disposition, "evidence": terminal,
	})
	if err != nil {
		return result, err
	}
	b.deliveryFailures = map[string]string{}
	encoded, err := canonicalValue(operation)
	if err != nil {
		return result, err
	}
	b.deliveryFailures[requestID] = hashBytes(encoded)
	request, err := NewRequest(requestID, "delivery_failure", recovered.actor, OperationsParameters{Operations: []Operation{operation}})
	if err != nil {
		return result, err
	}
	publication, err := b.Publish(ctx, recovered.actor, request)
	if err != nil {
		result.Reason = "delivery_unresolved"
		return result, err
	}
	result.Publication, result.Reason = &publication, "delivery_failed_"+disposition
	return result, nil
}
