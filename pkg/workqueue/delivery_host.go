package workqueue

import (
	"context"
	"encoding/json"
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
	for key, value := range values {
		copy[key] = value
	}
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
	for field, value := range resource {
		copy[field] = value
	}
	return copy
}

func nativeDeliveryTargetBinding(state Projection, work *WorkState, actor Actor, target EffectResource) error {
	if err := AuthorizeEffect(state, actor, target); err != nil {
		return err
	}
	scope, err := frozenResourceScope(work.Payload)
	if err != nil {
		return err
	}
	nativeSelector := func(selector EffectResource) bool {
		if selector["host"] != "github.com" || !decimalIdentity(selector["repository_id"]) {
			return false
		}
		if selector["kind"] != "" || selector["number"] != "" || selector["resource_id"] != "" || selector["comment_id"] != "" {
			return selector["kind"] != "" && decimalIdentity(selector["number"]) && decimalIdentity(selector["resource_id"])
		}
		return true
	}
	matched := false
	if scope != nil {
		for _, selector := range scope.Resources {
			if nativeSelector(selector) && matchesEffectResource(selector, target) {
				matched = true
				break
			}
		}
		if !matched {
			return queueError("claim_scope_invalid", "native readback requires a positive immutable Work resource selector")
		}
	}
	if subject := work.Subject; subject != nil {
		selector := EffectResource{
			"kind": subject.Kind, "host": subject.Host, "repository": subject.Repository,
			"repository_id": subject.RepositoryID, "resource_id": subject.ResourceID, "number": subject.Number,
		}
		if !nativeSelector(selector) || !matchesEffectResource(selector, target) {
			return queueError("claim_scope_invalid", "native readback requires the exact immutable Work subject")
		}
		matched = true
	}
	if !matched {
		return queueError("claim_scope_invalid", "native readback requires immutable repository and resource identities")
	}
	return nil
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
		unknown := DeliveryVerification{Disposition: "unknown"}
		if host == nil || host.options.Inventory == nil {
			return unknown, nil
		}
		claim := state.Claims[supplied.ClaimID]
		if claim == nil || !sameJSON(claim, supplied) {
			return unknown, queueError("claim_scope_invalid", "native delivery requires the original retained Claim")
		}
		work := state.Works[claim.WorkID]
		dispatch := state.Dispatches[claim.DispatchID]
		if work == nil || dispatch == nil || dispatch.Run == nil || claim.State != "completed" ||
			work.State != "completed" || work.ClaimID != claim.ClaimID || work.Barrier != "pending" {
			return unknown, nil
		}
		if err := validateDeliveryContract(work.Payload); err != nil {
			return unknown, err
		}
		scope := NativeDeliveryScope{Run: *dispatch.Run, CompletionID: work.CompletionID, CredentialGeneration: state.CredentialGeneration}
		for _, member := range dispatch.Claims {
			if member.ClaimID == claim.ClaimID {
				scope.Member = member
				break
			}
		}
		if scope.Member.ClaimID == "" || !sameJSON(scope.Member.Work, work.Payload) {
			return unknown, queueError("assignment_invalid", "native delivery lost immutable member payload")
		}
		bound, err := branch.withClient()
		if err != nil {
			return unknown, err
		}
		binding, _, err := bound.runForDispatch(ctx, state, dispatch, scope.Run.RunID)
		if err != nil || !sameJSON(binding, scope.Run) {
			return unknown, queueError("run_binding_conflict", "native delivery requires exact authenticated attempt one")
		}
		// Copy mutable callback arguments so a callback cannot alter the queue
		// projection that will subsequently be used to build a CAS candidate.
		scope = copyNativeDeliveryScope(scope)
		inventory, err := host.options.Inventory(ctx, copyNativeDeliveryScope(scope))
		if err != nil || !inventory.Closed || len(inventory.Outputs) > 128 {
			return unknown, err
		}
		encodedInventory, err := canonicalValue(inventory)
		if err != nil || json.Unmarshal(encodedInventory, &inventory) != nil {
			return unknown, queueError("delivery_evidence_required", "native process inventory must be canonical")
		}
		var payload struct {
			Contract struct {
				Kind     string                 `json:"kind"`
				NoWrites bool                   `json:"no_writes"`
				Outputs  []nativeDeclaredOutput `json:"outputs"`
			} `json:"effect_contract"`
		}
		if err := json.Unmarshal(work.Payload, &payload); err != nil {
			return unknown, err
		}
		contract := payload.Contract
		controls := nativeControlInventory(state, scope, dispatch.DispatchID)
		if (contract.Kind == "none" || contract.NoWrites) && (len(inventory.Outputs) != 0 || len(controls) != 0) {
			return unknown, nil
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
					return unknown, nil
				}
			}
			declared[output.Type] = output
		}
		for _, output := range inventory.Outputs {
			if _, ok := declared[output.Type]; !ok {
				return unknown, nil
			}
			counts[output.Type]++
		}
		for name, expected := range declared {
			if counts[name] < expected.Min || counts[name] > expected.Max {
				return unknown, nil
			}
		}
		verifiedControls := map[string]bool{}
		descriptors := []json.RawMessage{}
		for _, output := range inventory.Outputs {
			expected, ok := declared[output.Type]
			if !ok {
				return unknown, nil
			}
			var descriptor json.RawMessage
			if output.Type == "work_queue_submit" || output.Type == "work_queue_dispatch_next" {
				kind := "submit"
				if output.Type == "work_queue_dispatch_next" {
					kind = "dispatch_next"
				}
				matched := false
				for _, control := range controls {
					if control.Request.ID == output.RequestID && control.Request.Kind == kind &&
						sameJSON(control.Request.Parameters, output.Intent) && !verifiedControls[output.RequestID] {
						descriptor, err = canonicalValue(map[string]string{"type": output.Type, "commit_id": control.ID, "request_id": control.Request.ID})
						matched = err == nil
						verifiedControls[output.RequestID] = matched
						break
					}
				}
				if !matched {
					return unknown, nil
				}
			} else {
				if err := nativeDeliveryTargetBinding(state, work, workerDeliveryActor(scope, dispatch.DispatchID), output.Target); err != nil {
					return unknown, err
				}
				verify := host.options.Builtin[output.Type]
				var intent json.RawMessage
				if expected.Verification != nil {
					verify = host.options.Declared[expected.Verification.VerifierID]
					intent = slices.Clone(expected.Verification.Expected)
				}
				if verify == nil || host.options.Credentials == nil {
					return unknown, nil
				}
				if err := host.options.Credentials(ctx, copyNativeDeliveryScope(scope), copyNativeEffectResource(output.Target)); err != nil {
					return unknown, err
				}
				var verified bool
				output.Target = copyNativeEffectResource(output.Target)
				output.Intent = slices.Clone(output.Intent)
				descriptor, verified, err = verify(ctx, copyNativeDeliveryScope(scope), output, intent)
				if err != nil || !verified {
					return unknown, err
				}
				var object map[string]json.RawMessage
				descriptor, err = Canonical(descriptor)
				if err != nil || json.Unmarshal(descriptor, &object) != nil || object == nil {
					return unknown, queueError("result_invalid", "native readback descriptor must be a canonical object")
				}
			}
			descriptors = append(descriptors, descriptor)
		}
		if len(verifiedControls) != len(controls) {
			return unknown, nil
		}
		descriptor, err := canonicalValue(map[string]any{"outputs": descriptors})
		if err != nil || int64(len(descriptor)) > state.Policy.Limits.ResultBytes {
			return unknown, queueError("result_invalid", "native readback descriptor exceeds Result byte budget")
		}
		record, err := canonicalValue(map[string]any{
			"scope": scope, "contract": json.RawMessage(work.Payload), "controls": controls,
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
}

func workerDeliveryActor(scope NativeDeliveryScope, dispatchID string) Actor {
	return Actor{
		Role: "worker", Principal: scope.Run.Principal, Repository: scope.Run.Repository,
		Workflow: scope.Run.Workflow, RunID: scope.Run.RunID, RunAttempt: 1,
		DispatchID: dispatchID, ClaimHandle: scope.Member.Handle,
	}
}
