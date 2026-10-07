package workqueue

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
)

// NativeDeliveryScope binds approved process callbacks to the immutable member
// and the authenticated original Actions attempt. It is not a wire credential.
type NativeDeliveryScope struct {
	Member               AssignmentClaim
	Run                  RunBinding
	CompletionID         string
	CredentialGeneration string
}

// NativeDeliveryOutput is an item from the protected worker's closed effect
// channel. Intent and Target are data; neither grants verifier authority.
type NativeDeliveryOutput struct {
	Type      string
	Target    EffectResource
	Intent    json.RawMessage
	RequestID string
}

// NativeDeliveryInventory must come from an approved process callback, not a
// receipt file, subprocess stdout, artifact, or caller-supplied source string.
// Closed means the process has stopped accepting effects for this member.
type NativeDeliveryInventory struct {
	Closed  bool
	Outputs []NativeDeliveryOutput
}

// NativeOutputVerifier performs independent readback only. Hosts register these
// callbacks before admitting untrusted worker input; no verifier is executable
// merely because a Work names its ID.
type NativeOutputVerifier func(context.Context, NativeDeliveryScope, NativeDeliveryOutput, json.RawMessage) (json.RawMessage, bool, error)

type NativeDeliveryHostOptions struct {
	Inventory func(context.Context, NativeDeliveryScope) (NativeDeliveryInventory, error)
	// Credentials proves the host has approved read credentials for this exact
	// target and generation before any output verifier may call an external SDK.
	Credentials func(context.Context, NativeDeliveryScope, EffectResource) error
	Builtin     map[string]NativeOutputVerifier
	Declared    map[string]NativeOutputVerifier
}

// NativeDeliveryHost owns in-process callback capabilities. JSON cannot create
// or configure a host. The copied registrations cannot be changed by Work data.
type NativeDeliveryHost struct {
	options NativeDeliveryHostOptions
}

func NewNativeDeliveryHost(options NativeDeliveryHostOptions) *NativeDeliveryHost {
	options.Builtin = copyNativeVerifiers(options.Builtin)
	options.Declared = copyNativeVerifiers(options.Declared)
	return &NativeDeliveryHost{options: options}
}

func copyNativeVerifiers(values map[string]NativeOutputVerifier) map[string]NativeOutputVerifier {
	copy := make(map[string]NativeOutputVerifier, len(values))
	maps.Copy(copy, values)
	return copy
}

func copyNativeDeliveryScope(scope NativeDeliveryScope) NativeDeliveryScope {
	scope.Member.Work = slices.Clone(scope.Member.Work)
	scope.Member.ResultRefs = slices.Clone(scope.Member.ResultRefs)
	for index := range scope.Member.ResultRefs {
		scope.Member.ResultRefs[index].Descriptor = slices.Clone(scope.Member.ResultRefs[index].Descriptor)
	}
	return scope
}

func copyNativeEffectResource(resource EffectResource) EffectResource {
	copy := make(EffectResource, len(resource))
	maps.Copy(copy, resource)
	return copy
}

type nativeDeclaredOutput struct {
	Type         string `json:"type"`
	Min          int    `json:"min"`
	Max          int    `json:"max"`
	Verification *struct {
		VerifierID string          `json:"verifier_id"`
		Expected   json.RawMessage `json:"expected"`
	} `json:"verification,omitempty"`
}

// Preserve the anonymous wire shape, including its JSON decoding errors.
type nativeDeliveryContract = struct {
	Kind     string                 `json:"kind"`
	NoWrites bool                   `json:"no_writes"`
	Outputs  []nativeDeclaredOutput `json:"outputs"`
}

type nativeDeliveryContext struct {
	work     *WorkState
	dispatch *DispatchState
	scope    NativeDeliveryScope
}

func nativeControlInventory(state Projection, scope NativeDeliveryScope, dispatchID string) []QueueCommit {
	controls := []QueueCommit{}
	for _, commit := range state.Requests {
		actor := commit.Actor
		if actor.Role == "worker" && actor.DispatchID == dispatchID &&
			actor.ClaimHandle == scope.Member.Handle && actor.Repository == scope.Run.Repository &&
			actor.Workflow == scope.Run.Workflow && actor.Principal == scope.Run.Principal &&
			actor.RunID == scope.Run.RunID && actor.RunAttempt == 1 &&
			(commit.Request.Kind == "submit" || commit.Request.Kind == "dispatch_next") {
			controls = append(controls, commit)
		}
	}
	slices.SortFunc(controls, func(a, b QueueCommit) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return controls
}

// Verifier returns the native adapter used by both recovery and every fresh CAS
// prefix. It re-reads API provenance and approved callbacks on each invocation;
// a copied receipt/digest or successful job can never bypass those reads.
func (host *NativeDeliveryHost) Verifier(branch Branch) DeliveryVerifier {
	return func(ctx context.Context, state Projection, supplied ClaimState) (DeliveryVerification, error) {
		return host.verifyNativeDelivery(ctx, branch, state, supplied)
	}
}

func (host *NativeDeliveryHost) verifyNativeDelivery(ctx context.Context, branch Branch, state Projection, supplied ClaimState) (DeliveryVerification, error) {
	unknown := DeliveryVerification{Disposition: "unknown"}
	if host == nil || host.options.Inventory == nil {
		return unknown, nil
	}
	delivery, ready, err := nativeDeliveryClaim(ctx, branch, state, supplied)
	if err != nil || !ready {
		return unknown, err
	}
	inventory, closed, err := host.readNativeDeliveryInventory(ctx, delivery.scope)
	if err != nil || !closed {
		return unknown, err
	}
	contract, err := decodeNativeDeliveryContract(delivery.work.Payload)
	if err != nil {
		return unknown, err
	}
	controls := nativeControlInventory(state, delivery.scope, delivery.dispatch.DispatchID)
	declared, complete := host.nativeDeliveryDeclarations(contract, inventory, controls)
	if !complete {
		return unknown, nil
	}
	descriptors, verified, err := host.verifyNativeDeliveryOutputs(ctx, state, delivery, inventory, declared, controls)
	if err != nil || !verified {
		return unknown, err
	}
	return nativeDeliveryReceipt(ctx, state, delivery, contract, controls, inventory, descriptors)
}

func nativeDeliveryClaim(ctx context.Context, branch Branch, state Projection, supplied ClaimState) (nativeDeliveryContext, bool, error) {
	claim := state.Claims[supplied.ClaimID]
	if claim == nil || !sameJSON(claim, supplied) {
		return nativeDeliveryContext{}, false, queueError("claim_scope_invalid", "native delivery requires the original retained Claim")
	}
	work := state.Works[claim.WorkID]
	dispatch := state.Dispatches[claim.DispatchID]
	if work == nil || dispatch == nil || dispatch.Run == nil || claim.State != "completed" ||
		work.State != "completed" || work.ClaimID != claim.ClaimID || work.Barrier != "pending" {
		return nativeDeliveryContext{}, false, nil
	}
	if err := validateDeliveryContract(work.Payload); err != nil {
		return nativeDeliveryContext{}, false, err
	}
	scope := NativeDeliveryScope{Run: *dispatch.Run, CompletionID: work.CompletionID, CredentialGeneration: state.CredentialGeneration}
	for _, member := range dispatch.Claims {
		if member.ClaimID == claim.ClaimID {
			scope.Member = member
			break
		}
	}
	if scope.Member.ClaimID == "" || !sameJSON(scope.Member.Work, work.Payload) {
		return nativeDeliveryContext{}, false, queueError("assignment_invalid", "native delivery lost immutable member payload")
	}
	bound, err := branch.withClient()
	if err != nil {
		return nativeDeliveryContext{}, false, err
	}
	binding, _, err := bound.runForDispatch(ctx, state, dispatch, scope.Run.RunID)
	if err != nil || !sameJSON(binding, scope.Run) {
		return nativeDeliveryContext{}, false, queueError("run_binding_conflict", "native delivery requires exact authenticated attempt one")
	}
	// Callback arguments must not alias the projection used by the CAS candidate.
	scope = copyNativeDeliveryScope(scope)
	return nativeDeliveryContext{work: work, dispatch: dispatch, scope: scope}, true, nil
}

func (host *NativeDeliveryHost) readNativeDeliveryInventory(ctx context.Context, scope NativeDeliveryScope) (NativeDeliveryInventory, bool, error) {
	inventory, err := host.options.Inventory(ctx, copyNativeDeliveryScope(scope))
	if err != nil || !inventory.Closed || len(inventory.Outputs) > 128 {
		return NativeDeliveryInventory{}, false, err
	}
	encodedInventory, err := canonicalValue(inventory)
	if err != nil || json.Unmarshal(encodedInventory, &inventory) != nil {
		return NativeDeliveryInventory{}, false, queueError("delivery_evidence_required", "native process inventory must be canonical")
	}
	return inventory, true, nil
}

func decodeNativeDeliveryContract(work json.RawMessage) (nativeDeliveryContract, error) {
	var payload struct {
		Contract nativeDeliveryContract `json:"effect_contract"`
	}
	err := json.Unmarshal(work, &payload)
	return payload.Contract, err
}

func (host *NativeDeliveryHost) nativeDeliveryDeclarations(contract nativeDeliveryContract, inventory NativeDeliveryInventory, controls []QueueCommit) (map[string]nativeDeclaredOutput, bool) {
	if (contract.Kind == "none" || contract.NoWrites) && (len(inventory.Outputs) != 0 || len(controls) != 0) {
		return nil, false
	}
	declared := map[string]nativeDeclaredOutput{}
	counts := map[string]int{}
	for _, output := range contract.Outputs {
		if output.Type != "work_queue_submit" && output.Type != "work_queue_dispatch_next" {
			verify := host.options.Builtin[output.Type]
			if output.Verification != nil {
				verify = host.options.Declared[output.Verification.VerifierID]
			}
			if verify == nil || host.options.Credentials == nil {
				return nil, false
			}
		}
		declared[output.Type] = output
	}
	for _, output := range inventory.Outputs {
		if _, ok := declared[output.Type]; !ok {
			return nil, false
		}
		counts[output.Type]++
	}
	for name, expected := range declared {
		if counts[name] < expected.Min || counts[name] > expected.Max {
			return nil, false
		}
	}
	return declared, true
}

func (host *NativeDeliveryHost) verifyNativeDeliveryOutputs(ctx context.Context, state Projection, delivery nativeDeliveryContext, inventory NativeDeliveryInventory, declared map[string]nativeDeclaredOutput, controls []QueueCommit) ([]json.RawMessage, bool, error) {
	verifiedControls := map[string]struct{}{}
	descriptors := []json.RawMessage{}
	for _, output := range inventory.Outputs {
		expected, ok := declared[output.Type]
		if !ok {
			return nil, false, nil
		}
		var descriptor json.RawMessage
		var verified bool
		var err error
		if output.Type == "work_queue_submit" || output.Type == "work_queue_dispatch_next" {
			descriptor, verified = verifyNativeDeliveryControl(output, controls, verifiedControls)
		} else {
			descriptor, verified, err = host.readNativeDeliveryOutput(ctx, state, delivery, output, expected)
		}
		if err != nil || !verified {
			return nil, false, err
		}
		descriptors = append(descriptors, descriptor)
	}
	if len(verifiedControls) != len(controls) {
		return nil, false, nil
	}
	return descriptors, true, nil
}

func verifyNativeDeliveryControl(output NativeDeliveryOutput, controls []QueueCommit, verifiedControls map[string]struct{}) (json.RawMessage, bool) {
	kind := "submit"
	if output.Type == "work_queue_dispatch_next" {
		kind = "dispatch_next"
	}
	for _, control := range controls {
		if control.Request.ID == output.RequestID && control.Request.Kind == kind &&
			sameJSON(control.Request.Parameters, output.Intent) {
			if _, verified := verifiedControls[output.RequestID]; verified {
				continue
			}
			descriptor, err := canonicalValue(map[string]string{"type": output.Type, "commit_id": control.ID, "request_id": control.Request.ID})
			if err != nil {
				return descriptor, false
			}
			verifiedControls[output.RequestID] = struct{}{}
			return descriptor, true
		}
	}
	return nil, false
}

func (host *NativeDeliveryHost) readNativeDeliveryOutput(ctx context.Context, state Projection, delivery nativeDeliveryContext, output NativeDeliveryOutput, expected nativeDeclaredOutput) (json.RawMessage, bool, error) {
	if err := AuthorizeEffect(state, workerDeliveryActor(delivery.scope, delivery.dispatch.DispatchID), output.Target); err != nil {
		return nil, false, err
	}
	verify := host.options.Builtin[output.Type]
	var intent json.RawMessage
	if expected.Verification != nil {
		verify = host.options.Declared[expected.Verification.VerifierID]
		intent = slices.Clone(expected.Verification.Expected)
	}
	if verify == nil || host.options.Credentials == nil {
		return nil, false, nil
	}
	if err := host.options.Credentials(ctx, copyNativeDeliveryScope(delivery.scope), copyNativeEffectResource(output.Target)); err != nil {
		return nil, false, err
	}
	output.Target = copyNativeEffectResource(output.Target)
	output.Intent = slices.Clone(output.Intent)
	descriptor, verified, err := verify(ctx, copyNativeDeliveryScope(delivery.scope), output, intent)
	if err != nil || !verified {
		return nil, false, err
	}
	var object map[string]json.RawMessage
	descriptor, err = Canonical(descriptor)
	if err != nil || json.Unmarshal(descriptor, &object) != nil || object == nil {
		return nil, false, queueError("result_invalid", "native readback descriptor must be a canonical object")
	}
	return descriptor, true, nil
}

func nativeDeliveryReceipt(ctx context.Context, state Projection, delivery nativeDeliveryContext, contract nativeDeliveryContract, controls []QueueCommit, inventory NativeDeliveryInventory, descriptors []json.RawMessage) (DeliveryVerification, error) {
	unknown := DeliveryVerification{Disposition: "unknown"}
	descriptor, err := canonicalValue(map[string]any{"outputs": descriptors})
	if err != nil || int64(len(descriptor)) > state.Policy.Limits.ResultBytes {
		return unknown, queueError("result_invalid", "native readback descriptor exceeds Result byte budget")
	}
	record, err := canonicalValue(map[string]any{
		"scope": delivery.scope, "contract": delivery.work.Payload, "controls": controls,
		"outputs": inventory.Outputs, "descriptor": json.RawMessage(descriptor),
	})
	if err != nil {
		return unknown, err
	}
	if err := ctx.Err(); err != nil {
		return unknown, err
	}
	disposition := "partial"
	if contract.Kind == "none" || contract.NoWrites {
		disposition = "none"
	}
	return DeliveryVerification{Verified: true, Descriptor: descriptor, Receipt: hashBytes(record), Disposition: disposition, PositiveNoEffects: disposition == "none"}, nil
}

func workerDeliveryActor(scope NativeDeliveryScope, dispatchID string) Actor {
	return Actor{
		Role: "worker", Principal: scope.Run.Principal, Repository: scope.Run.Repository,
		Workflow: scope.Run.Workflow, RunID: scope.Run.RunID, RunAttempt: 1,
		DispatchID: dispatchID, ClaimHandle: scope.Member.Handle,
	}
}
