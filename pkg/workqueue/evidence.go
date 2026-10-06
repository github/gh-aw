package workqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// NativeRun is a bounded projection of the authenticated Actions run API.
// Native/resource IDs are decoded as json.Number and never through float64.
type NativeRun struct {
	ID           json.Number `json:"id"`
	RunAttempt   int         `json:"run_attempt"`
	Event        string      `json:"event"`
	Status       string      `json:"status"`
	Conclusion   string      `json:"conclusion"`
	HeadSHA      string      `json:"head_sha"`
	Path         string      `json:"path"`
	DisplayTitle string      `json:"display_title"`
	Actor        struct {
		ID    json.Number `json:"id"`
		Login string      `json:"login"`
	} `json:"actor"`
	TriggeringActor struct {
		ID json.Number `json:"id,omitempty"`
	} `json:"triggering_actor"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (b Branch) exactRun(ctx context.Context, id string) (NativeRun, error) {
	if !decimalIdentity(id) {
		return NativeRun{}, queueError("run_invalid", "run ID must be a positive canonical decimal string")
	}
	var run NativeRun
	if err := b.request(ctx, http.MethodGet, "actions/runs/"+id+"/attempts/1", nil, &run); err != nil {
		return NativeRun{}, err
	}
	if run.ID.String() != id || run.RunAttempt != 1 || run.Event != "workflow_dispatch" ||
		!strings.EqualFold(run.Repository.FullName, b.Remote) {
		return NativeRun{}, queueError("run_binding_conflict", "native run identity/event/attempt/repository mismatch")
	}
	return run, nil
}

func decimalIdentity(id string) bool {
	if id == "" || id[0] == '0' {
		return false
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func (b Branch) runForDispatch(ctx context.Context, state Projection, dispatch *DispatchState, id string) (RunBinding, Evidence, error) {
	run, err := b.exactRun(ctx, id)
	if err != nil {
		return RunBinding{}, Evidence{}, err
	}
	profile := dispatch.Profile
	workflow := strings.Split(run.Path, "@")[0]
	if workflow != profile.Workflow || run.HeadSHA != profile.Ref ||
		!decimalIdentity(profile.Principal) || run.Actor.ID.String() != profile.Principal ||
		run.TriggeringActor.ID.String() != "" && run.TriggeringActor.ID.String() != profile.Principal ||
		!correlatedNativeTitle(run.DisplayTitle, dispatch.DispatchID) {
		return RunBinding{}, Evidence{}, queueError("run_binding_conflict", "run workflow/revision/principal differs from approved assignment")
	}

	binding := RunBinding{
		RunID: id, RunAttempt: 1, Repository: run.Repository.FullName, Workflow: workflow,
		Ref: run.HeadSHA, Principal: run.Actor.ID.String(), Event: run.Event,
	}
	evidence := Evidence{
		Kind: "reconciliation", Source: "github_api", Repository: run.Repository.FullName,
		Workflow: workflow, Ref: run.HeadSHA, Principal: run.Actor.ID.String(),
		CheckedAt: time.Now().UnixMilli(), RunID: id, RunAttempt: 1,
	}
	if run.Status == "completed" && run.Conclusion != "" {
		evidence.Kind, evidence.Status, evidence.Conclusion = "terminal_run", "completed", run.Conclusion
	}
	return binding, evidence, nil
}

func correlatedNativeTitle(title, dispatchID string) bool {
	return dispatchID != "" &&
		regexp.MustCompile(`(^|[^A-Za-z0-9_])`+regexp.QuoteMeta(dispatchID)+`($|[^A-Za-z0-9_])`).MatchString(title)
}

func (b Branch) verifyRequestEvidence(ctx context.Context, state Projection, actor Actor, request Request) error {
	if request.Kind == "submit" {
		var params SubmitParameters
		if err := json.Unmarshal(request.Parameters, &params); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, node := range params.Nodes {
			pool, ok := state.Policy.Pools[node.Pool]
			if !ok {
				return queueError("pool_invalid", "unknown submission pool")
			}
			resources := []Dependency{}
			for _, edge := range node.DependsOn {
				if edge.Kind != "work" {
					resources = append(resources, edge)
				}
			}
			if node.Subject != nil {
				condition := "completed"
				if node.Subject.Kind == "pull_request" {
					condition = "merged"
				}
				resources = append(resources, Dependency{Kind: node.Subject.Kind, Resource: node.Subject, Condition: condition})
			}
			for _, edge := range resources {
				if edge.Resource == nil {
					return queueError("resource_invalid", "external resource must have resolved immutable identity")
				}
				if err := validateResource(*edge.Resource, pool); err != nil {
					return err
				}
				key := resourceKey(*edge.Resource, "")
				if seen[key] {
					continue
				}
				seen[key] = true
				observation := b.readObservation(ctx, edge, state.CredentialGeneration, "admission")
				if observation.ReadStatus != "ok" && observation.ReadStatus != "predicate_unknown" {
					return queueError("resource_unverified", "admission cannot establish resource identity/access: %s", observation.ReadStatus)
				}
			}
			if node.ReplacementOf != nil {
				parent := state.Works[node.ReplacementOf.WorkID]
				if parent == nil || parent.Barrier != "failed" {
					return queueError("replacement_invalid", "replacement must reference an existing failed delivery barrier")
				}
				if parent.Disposition != "none" {
					if b.RemediationVerifier == nil {
						return queueError("remediation_verifier_required", "partial/unknown effects require trusted domain-specific remediation proof")
					}
					if err := b.RemediationVerifier(ctx, state, node); err != nil {
						return queueError("remediation_invalid", "replacement proof failed: %v", err)
					}
				}
			}
		}
		return nil
	}
	if request.Kind == "dispatch_next" || request.Kind == "policy" ||
		request.Kind == "control" || request.Kind == "cancel_work" {
		return nil
	}
	var params OperationsParameters
	if err := json.Unmarshal(request.Parameters, &params); err != nil {
		return err
	}
	for _, op := range params.Operations {
		var fields struct {
			Kind       string      `json:"kind"`
			DispatchID string      `json:"dispatch_id"`
			ClaimID    string      `json:"claim_id"`
			WorkID     string      `json:"work_id"`
			Evidence   *Evidence   `json:"evidence"`
			Run        *RunBinding `json:"run"`
		}
		_ = json.Unmarshal(op, &fields)
		switch fields.Kind {
		case "Result":
			var result struct {
				ClaimID    string          `json:"claim_id"`
				Descriptor json.RawMessage `json:"descriptor"`
				Evidence   Evidence        `json:"evidence"`
			}
			_ = json.Unmarshal(op, &result)
			claim := state.Claims[result.ClaimID]
			if b.DeliveryVerifier == nil || claim == nil {
				return queueError("delivery_verifier_required", "native Result requires a separately bound trusted scoped receipt verifier")
			}
			verification, err := b.DeliveryVerifier(ctx, state, *claim)
			if err != nil || !verification.Verified || verification.Receipt == "" ||
				verification.Receipt != result.Evidence.Receipt || !sameJSON(verification.Descriptor, result.Descriptor) {
				return queueError("delivery_evidence_required", "scoped delivery verification failed")
			}
			dispatch := state.Dispatches[claim.DispatchID]
			if dispatch.Run == nil {
				return queueError("run_binding_conflict", "Result requires an exact native run binding")
			}
			_, evidence, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
			if err != nil {
				return err
			}
			if result.Evidence.RunID != evidence.RunID || result.Evidence.RunAttempt != 1 ||
				result.Evidence.Repository != evidence.Repository || result.Evidence.Workflow != evidence.Workflow ||
				result.Evidence.Ref != evidence.Ref || result.Evidence.Principal != evidence.Principal {
				return queueError("evidence_invalid", "Result evidence differs from authenticated run provenance")
			}
		case "DeliveryFailure":
			encoded, _ := canonicalValue(op)
			if b.deliveryFailures[request.ID] != hashBytes(encoded) {
				return queueError("delivery_verifier_required", "failure publication requires actual bounded native delivery verification")
			}
			if fields.Evidence == nil {
				return queueError("terminal_evidence_required", "failure requires exact native terminal evidence")
			}
			claim := state.Claims[fields.ClaimID]
			if claim == nil {
				return queueError("claim_missing", "failure references missing Claim")
			}
			dispatch := state.Dispatches[claim.DispatchID]
			_, evidence, err := b.runForDispatch(ctx, state, dispatch, fields.Evidence.RunID)
			if err != nil || evidence.Kind != "terminal_run" {
				return queueError("terminal_evidence_required", "native run must actually be terminal")
			}
		case "Observation":
			return queueError("dependency_verifier_required", "resource observations require authenticated typed reads")
		case "ClaimCancellation", "WorkCancellation":
			if request.Kind != "release" && request.Kind != "cancel_claim" {
				return queueError("evidence_required", "operator Claim cancellation requires reconciliation evidence")
			}
			claim := state.Claims[fields.ClaimID]
			if fields.Kind == "WorkCancellation" && state.Works[fields.WorkID] != nil {
				claim = state.Claims[state.Works[fields.WorkID].ClaimID]
			}
			if claim == nil {
				return queueError("claim_missing", "unknown Claim")
			}
			dispatch := state.Dispatches[claim.DispatchID]
			if dispatch.State == "reserved" {
				continue
			}
			if dispatch.Run == nil {
				return queueError("launch_unresolved", "possibly executing native run cannot be force-cancelled or released")
			}
			_, evidence, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
			if err != nil {
				return err
			}
			if evidence.Kind != "terminal_run" {
				return queueError("terminal_evidence_required", "native run remains nonterminal")
			}
		case "Dispatch", "Release":
			dispatch := state.Dispatches[fields.DispatchID]
			if dispatch == nil || fields.Evidence == nil {
				return queueError("evidence_required", "exact dispatch and normalized evidence required")
			}
			if fields.Evidence.Kind == "prelaunch" && dispatch.State == "reserved" && fields.Kind == "Release" {
				continue
			}
			if fields.Evidence.Kind == "nonlaunch" {
				return queueError("nonlaunch_verifier_required", "operator cannot turn a missing run or API error into nonlaunch proof")
			}
			runID := fields.Evidence.RunID
			if fields.Run != nil {
				runID = fields.Run.RunID
			}
			binding, evidence, err := b.runForDispatch(ctx, state, dispatch, runID)
			if err != nil {
				return err
			}
			if fields.Run != nil && !sameJSON(binding, fields.Run) ||
				fields.Evidence.RunID != evidence.RunID ||
				fields.Evidence.RunAttempt != 1 ||
				fields.Evidence.Source != "github_api" ||
				fields.Evidence.Repository != evidence.Repository ||
				fields.Evidence.Workflow != evidence.Workflow ||
				fields.Evidence.Ref != evidence.Ref ||
				fields.Evidence.Principal != evidence.Principal ||
				fields.Evidence.Kind == "terminal_run" && evidence.Kind != "terminal_run" {
				return queueError("evidence_invalid", "submitted evidence differs from authenticated run API")
			}
		default:
			return queueError("actor_unauthorized", "operator cannot publish worker lifecycle facts")
		}
	}
	_ = actor
	return nil
}

type Reconciliation struct {
	DispatchID  string       `json:"dispatch_id"`
	State       string       `json:"state"`
	Reason      string       `json:"reason"`
	Evidence    *Evidence    `json:"evidence,omitempty"`
	Publication *Publication `json:"publication,omitempty"`
}

// Reconcile inspects the exact bound native attempt. Missing/unbound runs retain
// reservations; terminal recovery cancels open members only, preserving every
// Completion and independently pending delivery barrier.
func (b Branch) Reconcile(ctx context.Context, dispatchID, requestID string) (Reconciliation, error) {
	b, err := b.withClient()
	if err != nil {
		return Reconciliation{}, err
	}
	actor, err := b.Authenticate(ctx, "reconciler")
	if err != nil {
		return Reconciliation{}, err
	}
	commits, err := b.Read(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	state, err := Replay(commits)
	if err != nil {
		return Reconciliation{}, err
	}
	dispatch := state.Dispatches[dispatchID]
	if dispatch == nil {
		return Reconciliation{}, queueError("dispatch_missing", "unknown dispatch %s", dispatchID)
	}
	result := Reconciliation{DispatchID: dispatchID, State: dispatch.State}
	if dispatch.Released {
		result.Reason = "already_released"
		return result, nil
	}
	if dispatch.Run == nil {
		if dispatch.State == "reserved" {
			result.Reason = "reserved_not_started"
		} else {
			result.Reason = "launch_unresolved"
		}
		return result, nil
	}
	_, evidence, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
	if err != nil {
		return result, err
	}
	result.Evidence = &evidence
	if evidence.Kind != "terminal_run" {
		result.Reason = "native_run_nonterminal"
		return result, nil
	}
	operations := []Operation{}
	at := time.Now().UnixMilli()
	for _, member := range dispatch.Claims {
		claim := state.Claims[member.ClaimID]
		if claim.State != "open" {
			continue
		}
		work := state.Works[claim.WorkID]
		retry := state.Policy.Pools[work.Pool].Retry
		operations = append(operations, Op(map[string]any{
			"kind": "ClaimCancellation", "work_id": work.WorkID, "claim_id": claim.ClaimID,
			"reason": "native_run_terminal", "retry_not_before": at + retry.BackoffMS,
		}))
		if work.Attempts >= retry.MaxAttempts {
			operations = append(operations, Op(map[string]any{
				"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "attempts_exhausted",
			}))
		}
	}
	operations = append(operations, Op(map[string]any{"kind": "Release", "dispatch_id": dispatchID, "evidence": evidence}))
	request, err := NewRequest(requestID, "release", actor, OperationsParameters{Operations: operations})
	if err != nil {
		return result, err
	}
	publication, err := b.Publish(ctx, actor, request)
	if err != nil {
		return result, err
	}
	result.Publication, result.Reason = &publication, "native_released_delivery_independent"
	return result, nil
}

func (b Branch) InspectEvidence(ctx context.Context, dispatchID string) (Reconciliation, error) {
	b, err := b.withClient()
	if err != nil {
		return Reconciliation{}, err
	}
	commits, err := b.Read(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	state, err := Replay(commits)
	if err != nil {
		return Reconciliation{}, err
	}
	dispatch := state.Dispatches[dispatchID]
	if dispatch == nil {
		return Reconciliation{}, queueError("dispatch_missing", "unknown dispatch")
	}
	result := Reconciliation{DispatchID: dispatchID, State: dispatch.State, Reason: "launch_unresolved"}
	if dispatch.Run == nil {
		return result, nil
	}
	_, evidence, err := b.runForDispatch(ctx, state, dispatch, dispatch.Run.RunID)
	result.Evidence = &evidence
	if err == nil {
		result.Reason = evidence.Kind
	}
	return result, err
}

// CancelReserved is a checked prelaunch cancellation, never a force release.
// A competing start marker wins through CAS and makes this request invalid.
func (b Branch) CancelReserved(ctx context.Context, dispatchID, requestID string) (Reconciliation, error) {
	b, err := b.withClient()
	if err != nil {
		return Reconciliation{}, err
	}
	actor, err := b.Authenticate(ctx, "reconciler")
	if err != nil {
		return Reconciliation{}, err
	}
	commits, err := b.Read(ctx)
	if err != nil {
		return Reconciliation{}, err
	}
	state, err := Replay(commits)
	if err != nil {
		return Reconciliation{}, err
	}
	dispatch := state.Dispatches[dispatchID]
	if dispatch == nil || dispatch.Released || dispatch.State != "reserved" || dispatch.Sender != nil {
		return Reconciliation{}, queueError("launch_unresolved", "prelaunch cancellation requires a reservation with no start marker")
	}
	profile := dispatch.Profile
	at := time.Now().UnixMilli()
	evidence := Evidence{
		Kind: "prelaunch", Source: "trusted_publisher", Repository: actor.Repository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal, CheckedAt: at,
	}
	operations := []Operation{}
	for _, member := range dispatch.Claims {
		claim := state.Claims[member.ClaimID]
		if claim.State != "open" {
			continue
		}
		work := state.Works[claim.WorkID]
		retry := state.Policy.Pools[work.Pool].Retry
		operations = append(operations, Op(map[string]any{
			"kind": "ClaimCancellation", "work_id": work.WorkID, "claim_id": claim.ClaimID,
			"reason": "prelaunch_cancelled", "retry_not_before": at + retry.BackoffMS,
		}))
		if work.Attempts >= retry.MaxAttempts {
			operations = append(operations, Op(map[string]any{
				"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "attempts_exhausted",
			}))
		}
	}
	operations = append(operations, Op(map[string]any{"kind": "Release", "dispatch_id": dispatchID, "evidence": evidence}))
	request, err := NewRequest(requestID, "release", actor, OperationsParameters{Operations: operations})
	if err != nil {
		return Reconciliation{}, err
	}
	publication, err := b.Publish(ctx, actor, request)
	return Reconciliation{DispatchID: dispatchID, State: dispatch.State,
		Reason: "prelaunch_cancelled", Evidence: &evidence, Publication: &publication}, err
}
