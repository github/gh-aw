package workqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"
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
		return NativeRun{}, queueError("run_invalid", "invalid run ID; expected 1..256 canonical decimal digits without a leading zero (for example: 202)")
	}
	var run NativeRun
	if err := b.request(ctx, http.MethodGet, path.Join("actions/runs", id, "attempts/1"), nil, &run); err != nil {
		return NativeRun{}, err
	}
	if run.ID.String() != id || run.RunAttempt != 1 || run.Event != "workflow_dispatch" ||
		!strings.EqualFold(run.Repository.FullName, b.Remote) {
		return NativeRun{}, queueError("run_binding_conflict", "native run identity/event/attempt/repository mismatch")
	}
	return run, nil
}

func decimalIdentity(id string) bool {
	if id == "" || len(id) > 256 || strings.HasPrefix(id, "0") {
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
	principal := DispatchPrincipal(dispatch)
	workflow, _, _ := strings.Cut(run.Path, "@")
	if workflow != profile.Workflow || run.HeadSHA != profile.Ref ||
		!decimalIdentity(principal) || run.Actor.ID.String() != principal ||
		run.TriggeringActor.ID.String() != "" && run.TriggeringActor.ID.String() != principal ||
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
	if dispatchID == "" {
		return false
	}
	for {
		index := strings.Index(title, dispatchID)
		if index < 0 || index >= len(title) {
			return false
		}
		before, _ := utf8.DecodeLastRuneInString(title[:index])
		after, _ := utf8.DecodeRuneInString(title[index+len(dispatchID):])
		if !nativeTitleWord(before) && !nativeTitleWord(after) {
			return true
		}
		title = title[index+1:]
	}
}

func nativeTitleWord(char rune) bool {
	return char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' ||
		char >= '0' && char <= '9' || char == '_'
}

func (b Branch) verifyRequestEvidence(ctx context.Context, state Projection, actor Actor, request Request) error {
	if request.Kind == "policy" {
		return b.verifyPolicyEvidence(ctx, request)
	}
	if request.Kind == "submit" {
		return b.verifySubmissionEvidence(ctx, state, request)
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
		if err := b.verifyOperationEvidence(ctx, state, request, op); err != nil {
			return err
		}
	}
	_ = actor
	return nil
}

func (b Branch) verifyPolicyEvidence(ctx context.Context, request Request) error {
	var parameters OperationsParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return queueError("request_invalid", "prospective policy requires exactly one Policy operation")
	}
	only, unique := singleOperation(parameters.Operations)
	if !unique {
		return queueError("request_invalid", "prospective policy requires exactly one Policy operation")
	}
	kind, err := operationKind(only)
	if err != nil || kind != "Policy" {
		return queueError("request_invalid", "prospective policy requires exactly one Policy operation")
	}
	var operation struct {
		Policy Policy `json:"policy"`
	}
	if err := json.Unmarshal(only, &operation); err != nil {
		return queueError("request_invalid", "prospective policy operation is malformed")
	}
	return b.verifyWorkerRoutes(ctx, operation.Policy)
}

func (b Branch) verifySubmissionEvidence(ctx context.Context, state Projection, request Request) error {
	var params SubmitParameters
	if err := json.Unmarshal(request.Parameters, &params); err != nil {
		return err
	}
	seen := identitySet{}
	for _, node := range params.Nodes {
		if err := b.verifySubmittedNode(ctx, state, node, seen); err != nil {
			return err
		}
	}
	return nil
}

func (b Branch) verifySubmittedNode(ctx context.Context, state Projection, node WorkDefinition, seen identitySet) error {
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
	if node.BackingIssue != nil {
		resources = append(resources, Dependency{Kind: "issue", Resource: node.BackingIssue, Condition: "completed"})
	}
	for _, edge := range resources {
		if edge.Resource == nil {
			return queueError("resource_invalid", "external resource must have resolved immutable identity")
		}
		if err := validateResource(*edge.Resource, pool); err != nil {
			return err
		}
		key := resourceKey(*edge.Resource, "")
		if seen.contains(key) {
			continue
		}
		seen.add(key)
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
	return nil
}

type requestEvidenceFields struct {
	Kind       string      `json:"kind"`
	DispatchID string      `json:"dispatch_id"`
	ClaimID    string      `json:"claim_id"`
	WorkID     string      `json:"work_id"`
	Evidence   *Evidence   `json:"evidence"`
	Run        *RunBinding `json:"run"`
}

func (b Branch) verifyOperationEvidence(ctx context.Context, state Projection, request Request, op Operation) error {
	var fields requestEvidenceFields
	if err := json.Unmarshal(op, &fields); err != nil {
		return err
	}
	switch fields.Kind {
	case "Result":
		return b.verifyResultEvidence(ctx, state, op)
	case "DeliveryFailure":
		return b.verifyFailureEvidence(ctx, state, request, op, fields)
	case "Observation":
		return queueError("dependency_verifier_required", "resource observations require authenticated typed reads")
	case "ClaimCancellation", "WorkCancellation":
		return b.verifyCancellationEvidence(ctx, state, request, fields)
	case "Dispatch", "Release":
		return b.verifyDispatchEvidence(ctx, state, fields)
	default:
		return queueError("actor_unauthorized", "operator cannot publish worker lifecycle facts")
	}
}

func (b Branch) verifyResultEvidence(ctx context.Context, state Projection, op Operation) error {
	var result struct {
		ClaimID    string          `json:"claim_id"`
		Descriptor json.RawMessage `json:"descriptor"`
		Evidence   Evidence        `json:"evidence"`
	}
	if err := json.Unmarshal(op, &result); err != nil {
		return err
	}
	claim := state.Claims[result.ClaimID]
	if b.DeliveryVerifier == nil || claim == nil {
		return queueError("delivery_verifier_required", "native Result requires a separately bound trusted scoped receipt verifier")
	}
	work := state.Works[claim.WorkID]
	if work == nil {
		return queueError("work_missing", "Result references missing immutable Work")
	}
	if err := validateDeliveryContract(work.Payload); err != nil {
		return err
	}
	verification, err := b.DeliveryVerifier(ctx, state, *claim)
	if err != nil || !verification.Verified || verification.Receipt == "" ||
		verification.Receipt != result.Evidence.Receipt || !sameJSON(verification.Descriptor, result.Descriptor) ||
		result.Evidence.Effects != "" && result.Evidence.Effects != verification.Disposition {
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
	return nil
}

func (b Branch) verifyFailureEvidence(ctx context.Context, state Projection, request Request, op Operation, fields requestEvidenceFields) error {
	encoded, err := canonicalValue(op)
	if err != nil {
		return err
	}
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
	return nil
}

func (b Branch) verifyCancellationEvidence(ctx context.Context, state Projection, request Request, fields requestEvidenceFields) error {
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
		return nil
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
	return nil
}

func (b Branch) verifyDispatchEvidence(ctx context.Context, state Projection, fields requestEvidenceFields) error {
	dispatch := state.Dispatches[fields.DispatchID]
	if dispatch == nil || fields.Evidence == nil {
		return queueError("evidence_required", "exact dispatch and normalized evidence required")
	}
	if fields.Evidence.Kind == "prelaunch" && dispatch.State == "reserved" && fields.Kind == "Release" {
		return nil
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
	at := time.Now().UnixMilli()
	operations, err := state.terminalReconciliationOperations(dispatch, evidence, at)
	if err != nil {
		return result, err
	}
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

func (state Projection) terminalReconciliationOperations(dispatch *DispatchState, evidence Evidence, at int64) ([]Operation, error) {
	operations := []Operation{}
	for _, member := range dispatch.Claims {
		claim := state.Claims[member.ClaimID]
		if claim.State != "open" {
			continue
		}
		work := state.Works[claim.WorkID]
		retry := state.Policy.Pools[work.Pool].Retry
		if err := appendOperation(&operations, map[string]any{
			"kind": "ClaimCancellation", "work_id": work.WorkID, "claim_id": claim.ClaimID,
			"reason": "native_run_terminal", "retry_not_before": at + retry.BackoffMS,
		}); err != nil {
			return nil, err
		}
		if work.Attempts >= retry.MaxAttempts {
			if err := appendOperation(&operations, map[string]any{
				"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "attempts_exhausted",
			}); err != nil {
				return nil, err
			}
		}
	}
	if err := appendOperation(&operations, map[string]any{"kind": "Release", "dispatch_id": dispatch.DispatchID, "evidence": evidence}); err != nil {
		return nil, err
	}
	return operations, nil
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
	if evidence.Principal == "" {
		evidence.Principal = actor.Principal
	}
	operations := []Operation{}
	for _, member := range dispatch.Claims {
		claim := state.Claims[member.ClaimID]
		if claim.State != "open" {
			continue
		}
		work := state.Works[claim.WorkID]
		retry := state.Policy.Pools[work.Pool].Retry
		if err := appendOperation(&operations, map[string]any{
			"kind": "ClaimCancellation", "work_id": work.WorkID, "claim_id": claim.ClaimID,
			"reason": "prelaunch_cancelled", "retry_not_before": at + retry.BackoffMS,
		}); err != nil {
			return Reconciliation{}, err
		}
		if work.Attempts >= retry.MaxAttempts {
			if err := appendOperation(&operations, map[string]any{
				"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "attempts_exhausted",
			}); err != nil {
				return Reconciliation{}, err
			}
		}
	}
	if err := appendOperation(&operations, map[string]any{"kind": "Release", "dispatch_id": dispatchID, "evidence": evidence}); err != nil {
		return Reconciliation{}, err
	}
	request, err := NewRequest(requestID, "release", actor, OperationsParameters{Operations: operations})
	if err != nil {
		return Reconciliation{}, err
	}
	publication, err := b.Publish(ctx, actor, request)
	return Reconciliation{DispatchID: dispatchID, State: dispatch.State,
		Reason: "prelaunch_cancelled", Evidence: &evidence, Publication: &publication}, err
}
