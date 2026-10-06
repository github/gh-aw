package workqueue

import (
	"encoding/json"
	"slices"
)

func (state Projection) scopedClaim(actor Actor, dispatchID, handle string) (*ClaimState, error) {
	if dispatchID == "" {
		dispatchID = actor.DispatchID
	}
	if handle == "" {
		handle = actor.ClaimHandle
	}
	if actor.Role != "worker" || actor.DispatchID != dispatchID || actor.ClaimHandle != handle ||
		actor.RunID == "" || actor.RunAttempt != 1 {
		return nil, queueError("claim_scope_invalid", "worker context does not name an original assignment member")
	}
	dispatch := state.Dispatches[dispatchID]
	if dispatch != nil && dispatch.PolicyEpoch != state.PolicyEpoch {
		return nil, queueError("claim_ineffective", "Claim belongs to a retired Policy epoch")
	}
	if dispatch == nil || dispatch.Released || dispatch.Run == nil || dispatch.State != "bound" ||
		dispatch.Run.RunID != actor.RunID || dispatch.Run.RunAttempt != actor.RunAttempt ||
		dispatch.Run.Principal != actor.Principal || dispatch.Run.Repository != actor.Repository ||
		dispatch.Run.Workflow != actor.Workflow {
		return nil, queueError("run_binding_conflict", "worker context does not match durable native binding")
	}
	for _, member := range dispatch.Claims {
		if member.Handle == handle {
			return state.Claims[member.ClaimID], nil
		}
	}
	return nil, queueError("claim_scope_invalid", "foreign Claim handle")
}

func NormalizeClaimHandle(assignment Assignment, selector *string) (string, error) {
	data, err := canonicalValue(assignment)
	if err != nil {
		return "", err
	}
	if _, err := ParseAssignment(data); err != nil {
		return "", err
	}
	if assignment.Version != Version || len(assignment.Claims) == 0 ||
		len(assignment.Claims) > MaxClaimsPerDispatch || assignment.DispatchID == "" ||
		assignment.RequestID == "" || assignment.CommitID == "" || assignment.PolicyEpoch == "" {
		return "", queueError("assignment_invalid", "immutable assignment provenance is required")
	}
	if selector == nil {
		if len(assignment.Claims) != 1 {
			return "", queueError("claim_scope_required", "multi-Claim assignment requires an explicit handle")
		}
		return assignment.Claims[0].Handle, nil
	}
	if *selector == "" {
		return "", queueError("claim_scope_invalid", "selector must be a nonempty original handle")
	}
	for _, member := range assignment.Claims {
		if member.Handle == *selector {
			return *selector, nil
		}
	}
	return "", queueError("claim_scope_invalid", "selector is not in the immutable assignment")
}

func ValidateAssignment(state Projection, assignment Assignment) error {
	dispatch := state.Dispatches[assignment.DispatchID]
	if dispatch == nil || !sameJSON(dispatch.Assignment, assignment) {
		return queueError("assignment_invalid", "assignment does not match admission commit")
	}
	return nil
}

func (state Projection) completedWorkerScope(actor Actor) (*ClaimState, *WorkState, error) {
	claim, err := state.scopedClaim(actor, actor.DispatchID, actor.ClaimHandle)
	if err != nil {
		return nil, nil, err
	}
	if claim == nil {
		return nil, nil, queueError("claim_scope_invalid", "original assignment member has no retained Claim")
	}
	work := state.Works[claim.WorkID]
	if work == nil || claim.State != "completed" || work.State != "completed" ||
		work.ClaimID != claim.ClaimID || work.Barrier == "failed" {
		return nil, nil, queueError("claim_effects_unauthorized", "this Claim has no verified Completion")
	}
	return claim, work, nil
}

func AuthorizeEffect(state Projection, actor Actor) error {
	_, work, err := state.completedWorkerScope(actor)
	if err != nil {
		return err
	}
	if work.Barrier != "pending" {
		return queueError("claim_effects_unauthorized", "a terminal delivery barrier cannot authorize effects again")
	}
	return nil
}

func authorizeWorkerQueueRequest(state Projection, actor Actor, request Request) error {
	if actor.Role != "worker" ||
		(request.Kind != "submit" && request.Kind != "dispatch_next" && request.Kind != "observe") {
		return nil
	}
	_, parent, err := state.completedWorkerScope(actor)
	if err != nil {
		return err
	}
	switch request.Kind {
	case "submit":
		var params SubmitParameters
		if err := json.Unmarshal(request.Parameters, &params); err != nil {
			return err
		}
		for _, node := range params.Nodes {
			if node.Pool != parent.Pool || node.Priority != parent.Priority || node.FairnessKey != parent.FairnessKey {
				return queueError("child_entitlement", "worker children must inherit their parent's pool, priority and account")
			}
		}
	case "dispatch_next":
		var params DispatchParameters
		if err := json.Unmarshal(request.Parameters, &params); err != nil {
			return err
		}
		if params.Pool != parent.Pool {
			return queueError("claim_scope_invalid", "worker dispatch control is restricted to its parent's pool")
		}
	case "observe":
		var params OperationsParameters
		if err := json.Unmarshal(request.Parameters, &params); err != nil {
			return err
		}
		for _, operation := range params.Operations {
			var observation Observation
			if err := json.Unmarshal(operation, &observation); err != nil {
				return err
			}
			if err := validateResource(observation.Resource, state.Policy.Pools[parent.Pool]); err != nil {
				return err
			}
		}
	}
	return nil
}

func evidenceFits(state Projection, dispatch *DispatchState, evidence Evidence, at int64) error {
	profile := dispatch.Profile
	if evidence.Repository == "" || evidence.Repository != state.Requests[dispatch.RequestID].Actor.Repository ||
		evidence.Workflow != profile.Workflow || evidence.Ref != profile.Ref ||
		evidence.Principal != profile.Principal || evidence.CheckedAt > at ||
		evidence.CheckedAt < 0 {
		return queueError("evidence_invalid", "evidence does not match approved execution scope")
	}
	data, _ := canonicalValue(evidence)
	if int64(len(data)) > state.Policy.Limits.EvidenceBytes {
		return queueError("evidence_limit", "lifecycle evidence exceeds policy limit")
	}
	return nil
}

func terminalEvidence(state Projection, dispatch *DispatchState, evidence Evidence, at int64) error {
	if err := evidenceFits(state, dispatch, evidence, at); err != nil {
		return err
	}
	if dispatch.Run == nil || evidence.Kind != "terminal_run" || evidence.Source != "github_api" ||
		evidence.RunID != dispatch.Run.RunID || evidence.RunAttempt != 1 ||
		evidence.Status != "completed" || evidence.Conclusion == "" {
		return queueError("terminal_evidence_required", "exact native run/attempt terminal evidence is required")
	}
	return nil
}

func (state Projection) applyCompletion(op Operation, commit QueueCommit) error {
	var completion struct {
		Kind        string `json:"kind"`
		WorkID      string `json:"work_id"`
		ClaimID     string `json:"claim_id"`
		DispatchID  string `json:"dispatch_id"`
		ClaimHandle string `json:"claim_handle"`
		RunID       string `json:"run_id"`
		RunAttempt  int    `json:"run_attempt"`
	}
	_ = json.Unmarshal(op, &completion)
	claim, err := state.scopedClaim(commit.Actor, completion.DispatchID, completion.ClaimHandle)
	if err != nil {
		return err
	}
	if claim.ClaimID != completion.ClaimID || claim.WorkID != completion.WorkID ||
		completion.RunID != commit.Actor.RunID || completion.RunAttempt != 1 {
		return queueError("claim_scope_invalid", "Completion changes assignment/run identity")
	}
	var params FinishParameters
	_ = json.Unmarshal(commit.Request.Parameters, &params)
	if params.DispatchID != completion.DispatchID || params.ClaimHandle != completion.ClaimHandle ||
		params.Outcome != "completed" {
		return queueError("request_invalid", "Completion differs from finish intent")
	}
	work := state.Works[claim.WorkID]
	if claim.State == "completed" && work.State == "completed" {
		return nil
	}
	if claim.State != "open" || work.State != "claimed" || work.ClaimID != claim.ClaimID {
		return queueError("ownership_terminal", "only effective open Claim can complete Work")
	}
	claim.State = "completed"
	claim.TerminalCommitID = commit.ID
	work.State, work.Barrier, work.CompletionID = "completed", "pending", commit.ID
	work.completionAt = commit.At
	return nil
}

func (state Projection) cancelClaim(op Operation, commit QueueCommit) error {
	var cancellation struct {
		Kind           string `json:"kind"`
		WorkID         string `json:"work_id"`
		ClaimID        string `json:"claim_id"`
		Reason         string `json:"reason"`
		RetryNotBefore int64  `json:"retry_not_before"`
	}
	_ = json.Unmarshal(op, &cancellation)
	claim := state.Claims[cancellation.ClaimID]
	if claim == nil || claim.WorkID != cancellation.WorkID {
		return queueError("claim_missing", "cancellation references missing Claim")
	}
	if commit.Actor.Role == "worker" {
		scoped, err := state.scopedClaim(commit.Actor, claim.DispatchID, claim.Handle)
		if err != nil || scoped == nil || scoped.ClaimID != claim.ClaimID {
			return queueError("claim_scope_invalid", "worker may cancel only its scoped Claim")
		}
		var params FinishParameters
		_ = json.Unmarshal(commit.Request.Parameters, &params)
		if params.DispatchID != claim.DispatchID || params.ClaimHandle != claim.Handle || params.Outcome != "cancelled" {
			return queueError("request_invalid", "cancellation differs from finish intent")
		}
	}
	if claim.State == "cancelled" {
		if claim.CancellationReason != cancellation.Reason || claim.RetryNotBefore != cancellation.RetryNotBefore {
			return queueError("cancellation_conflict", "Claim cancellation reason and retry boundary are immutable")
		}
		return nil
	}
	work := state.Works[claim.WorkID]
	if claim.State != "open" || work.State != "claimed" || work.ClaimID != claim.ClaimID {
		return queueError("ownership_terminal", "cannot cancel completed/frozen ownership")
	}
	pool := state.Policy.Pools[work.Pool]
	backoffOrigin := commit.At
	if commit.Request.Kind == "release" {
		for _, operation := range commit.Operations {
			if operationKind(operation) == "Release" {
				var release struct {
					Evidence Evidence `json:"evidence"`
				}
				_ = json.Unmarshal(operation, &release)
				if release.Evidence.Kind == "terminal_run" || release.Evidence.Kind == "nonlaunch" ||
					release.Evidence.Kind == "prelaunch" {
					backoffOrigin = min(backoffOrigin, release.Evidence.CheckedAt)
				}
			}
		}
	}
	if cancellation.RetryNotBefore < backoffOrigin ||
		cancellation.RetryNotBefore-backoffOrigin < pool.Retry.BackoffMS {
		return queueError("retry_invalid", "retry must preserve trusted minimum backoff")
	}
	claim.State = "cancelled"
	claim.TerminalCommitID = commit.ID
	claim.CancellationReason, claim.RetryNotBefore = cancellation.Reason, cancellation.RetryNotBefore
	work.State, work.ClaimID = "available", ""
	work.RetryNotBefore = cancellation.RetryNotBefore
	return nil
}

func (state Projection) cancelWork(op Operation, commit QueueCommit) error {
	var cancellation struct {
		WorkID string `json:"work_id"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(op, &cancellation)
	work := state.Works[cancellation.WorkID]
	if work == nil {
		return queueError("work_missing", "cancellation references missing Work")
	}
	if work.State == "completed" {
		return queueError("ownership_terminal", "completed Work cannot be cancelled")
	}
	if commit.Actor.Role == "producer" && !state.allowedProducer(commit.Actor, work.WorkDefinition) {
		return queueError("admission_unauthorized", "producer cannot cancel another accounting scope")
	}
	if commit.Actor.Role == "worker" {
		claim, err := state.scopedClaim(commit.Actor, "", "")
		if err != nil || claim.WorkID != work.WorkID || claim.State != "cancelled" ||
			work.Attempts < state.Policy.Pools[work.Pool].Retry.MaxAttempts ||
			commit.Request.Kind != "finish" {
			return queueError("ownership_unauthorized", "worker may terminally cancel only its exhausted scoped retry")
		}
	}
	if work.State == "cancelled" {
		if work.CancellationReason != cancellation.Reason {
			return queueError("cancellation_conflict", "Work terminal cancellation reason is immutable")
		}
		return nil
	}
	if work.ClaimID != "" {
		claim := state.Claims[work.ClaimID]
		if claim.State == "open" {
			claim.State = "cancelled"
			claim.TerminalCommitID = commit.ID
			claim.CancellationReason = cancellation.Reason
		}
	}
	work.State = "cancelled"
	work.CancellationReason, work.CancellationCommitID = cancellation.Reason, commit.ID
	return nil
}

func (state Projection) applyDispatch(op Operation, commit QueueCommit) error {
	var lifecycle struct {
		DispatchID string      `json:"dispatch_id"`
		State      string      `json:"state"`
		Sender     *Actor      `json:"sender"`
		Run        *RunBinding `json:"run"`
		Evidence   *Evidence   `json:"evidence"`
		Reason     string      `json:"reason"`
	}
	_ = json.Unmarshal(op, &lifecycle)
	dispatch := state.Dispatches[lifecycle.DispatchID]
	if dispatch == nil || dispatch.Released {
		return queueError("dispatch_invalid", "missing or released native reservation")
	}
	if dispatch.LifecycleWrites >= state.Policy.Pools[dispatch.Pool].Reconciliation.MaxAttempts+4 {
		return queueError("reconciliation_limit", "finite lifecycle-write budget exhausted; preserve reservation")
	}
	profile := dispatch.Profile
	switch lifecycle.State {
	case "started":
		if state.GrantsPaused {
			return queueError("grants_paused", "new launch markers are paused")
		}
		if commit.Actor.Role != "dispatcher" || dispatch.State != "reserved" ||
			lifecycle.Sender == nil || !sameJSON(lifecycle.Sender, commit.Actor) ||
			commit.Actor.Workflow == "" || !decimalIdentity(commit.Actor.RunID) ||
			commit.Actor.RunAttempt < 1 || lifecycle.Run != nil {
			return queueError("launch_started", "only one authenticated sender may record a start marker")
		}
		dispatch.State, dispatch.Sender = "started", lifecycle.Sender
	case "bound":
		if lifecycle.Run == nil || lifecycle.Evidence == nil ||
			!slices.Contains([]string{"started", "uncertain", "unresolved", "bound"}, dispatch.State) {
			return queueError("run_binding_conflict", "binding requires a start marker and authenticated evidence")
		}
		run := lifecycle.Run
		if !decimalIdentity(run.RunID) || run.RunAttempt != 1 || run.Event != "workflow_dispatch" ||
			run.Repository != commit.Actor.Repository || run.Workflow != profile.Workflow ||
			run.Ref != profile.Ref || run.Principal != profile.Principal {
			return queueError("run_binding_conflict", "native run does not match immutable approved target")
		}
		if err := evidenceFits(state, dispatch, *lifecycle.Evidence, commit.At); err != nil {
			return err
		}
		if lifecycle.Evidence.RunID != run.RunID || lifecycle.Evidence.RunAttempt != 1 ||
			!slices.Contains([]string{"github_api", "trusted_activation"}, lifecycle.Evidence.Source) {
			return queueError("evidence_invalid", "binding lacks authenticated exact run provenance")
		}
		if commit.Actor.Role == "worker" &&
			(commit.Actor.RunID != run.RunID || commit.Actor.RunAttempt != 1 ||
				commit.Actor.DispatchID != dispatch.DispatchID || commit.Actor.Principal != run.Principal ||
				commit.Actor.Workflow != run.Workflow) {
			return queueError("run_binding_conflict", "activation context does not match the actual worker run")
		}
		if dispatch.Run != nil && !sameJSON(dispatch.Run, run) {
			return queueError("run_binding_conflict", "assignment already bound to another run")
		}
		for id, other := range state.Dispatches {
			if id != dispatch.DispatchID && other.Run != nil && other.Run.RunID == run.RunID {
				return queueError("run_binding_conflict", "native run is already assigned to another dispatch")
			}
		}
		dispatch.State, dispatch.Run = "bound", run
	case "uncertain", "unresolved":
		if dispatch.Run != nil || !slices.Contains([]string{"started", "uncertain", "unresolved"}, dispatch.State) ||
			lifecycle.Reason == "" {
			return queueError("dispatch_invalid", "uncertainty requires an unbound launch marker")
		}
		if lifecycle.State == "unresolved" {
			if lifecycle.Evidence == nil || lifecycle.Evidence.Kind != "reconciliation" ||
				lifecycle.Evidence.Attempts < state.Policy.Pools[dispatch.Pool].Reconciliation.MaxAttempts {
				return queueError("reconciliation_invalid", "unresolved requires bounded reconciliation exhaustion")
			}
		}
		dispatch.State, dispatch.Reason = lifecycle.State, lifecycle.Reason
	case "rejected":
		if dispatch.Run != nil || lifecycle.Evidence == nil ||
			lifecycle.Evidence.Kind != "nonlaunch" || lifecycle.Evidence.Source != "github_api" {
			return queueError("nonlaunch_evidence_required", "rejection needs definitive nonlaunch evidence")
		}
		if err := evidenceFits(state, dispatch, *lifecycle.Evidence, commit.At); err != nil {
			return err
		}
		dispatch.State, dispatch.Reason = "rejected", lifecycle.Reason
	default:
		return queueError("dispatch_invalid", "unknown lifecycle state")
	}
	dispatch.LifecycleWrites++
	return nil
}

func (state Projection) release(op Operation, commit QueueCommit) error {
	var release struct {
		DispatchID string   `json:"dispatch_id"`
		Evidence   Evidence `json:"evidence"`
	}
	_ = json.Unmarshal(op, &release)
	dispatch := state.Dispatches[release.DispatchID]
	if dispatch == nil {
		return queueError("dispatch_missing", "release references missing reservation")
	}
	if dispatch.Released {
		return nil
	}
	if err := evidenceFits(state, dispatch, release.Evidence, commit.At); err != nil {
		return err
	}
	if dispatch.State == "reserved" {
		if release.Evidence.Kind != "prelaunch" || release.Evidence.Source != "trusted_publisher" {
			return queueError("nonlaunch_evidence_required", "prelaunch release must race start through CAS")
		}
	} else if dispatch.State == "rejected" {
		if release.Evidence.Kind != "nonlaunch" || release.Evidence.Source != "github_api" {
			return queueError("nonlaunch_evidence_required", "definitive rejection required")
		}
	} else if err := terminalEvidence(state, dispatch, release.Evidence, commit.At); err != nil {
		return err
	}
	for _, member := range dispatch.Claims {
		if state.Claims[member.ClaimID].State == "open" {
			return queueError("release_open_claims", "close only still-open Claims before native release")
		}
	}
	dispatch.Released = true
	return nil
}

func (state Projection) settleResult(op Operation, commit QueueCommit, failed bool) error {
	var result struct {
		WorkID       string          `json:"work_id"`
		ClaimID      string          `json:"claim_id"`
		CompletionID string          `json:"completion_id"`
		Descriptor   json.RawMessage `json:"descriptor"`
		Reason       string          `json:"reason"`
		Disposition  string          `json:"disposition"`
		Evidence     Evidence        `json:"evidence"`
	}
	_ = json.Unmarshal(op, &result)
	work := state.Works[result.WorkID]
	if work == nil || work.State != "completed" || work.ClaimID != result.ClaimID ||
		work.CompletionID != result.CompletionID {
		return queueError("result_invalid", "barrier must reference exact completed ownership")
	}
	dispatch := state.Dispatches[state.Claims[result.ClaimID].DispatchID]
	if err := evidenceFits(state, dispatch, result.Evidence, commit.At); err != nil {
		return err
	}
	if work.Barrier != "pending" {
		if !failed && work.Barrier == "verified" && sameJSON(work.Result, result.Descriptor) ||
			failed && work.Barrier == "failed" && work.Disposition == result.Disposition {
			return nil
		}
		return queueError("delivery_barrier_conflict", "Result and DeliveryFailure are immutable and exclusive")
	}
	if failed {
		if err := terminalEvidence(state, dispatch, result.Evidence, commit.At); err != nil {
			return err
		}
		recovery := state.Policy.Pools[work.Pool].Reconciliation
		if !deliveryVerificationExhausted(work, recovery, result.Evidence.Attempts, commit.At) {
			return queueError("reconciliation_pending", "delivery failure requires exhausted bounded verification")
		}
		effects := result.Evidence.Effects
		if effects == "" {
			effects = "unknown"
		}
		if result.Disposition != effects || result.Disposition == "none" && result.Evidence.Receipt == "" {
			return queueError("evidence_invalid", "failure disposition must match evidence; none requires positive no-effects proof")
		}
		work.Barrier, work.Disposition = "failed", result.Disposition
		return nil
	}
	data, err := Canonical(result.Descriptor)
	if err != nil || int64(len(data)) > state.Policy.Limits.ResultBytes ||
		result.Evidence.Kind != "delivery" || result.Evidence.Source != "verified_receipts" ||
		result.Evidence.Receipt == "" || dispatch.Run == nil ||
		result.Evidence.RunID != dispatch.Run.RunID || result.Evidence.RunAttempt != 1 {
		return queueError("delivery_evidence_required", "Result requires scoped verified delivery receipts")
	}
	work.Barrier, work.Result, work.ResultCommitID = "verified", data, commit.ID
	return nil
}
