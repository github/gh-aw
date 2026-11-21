package workqueue

import (
	"encoding/json"
	"slices"
)

func (state *Projection) applyOperation(kind string, operation Operation, commit QueueCommit, ordinal, index int) error {
	switch kind {
	case "Work":
		var work WorkDefinition
		if err := json.Unmarshal(operation, &work); err != nil {
			return err
		}
		if _, exists := state.Works[work.WorkID]; exists {
			return nil
		}
		record := &WorkState{WorkDefinition: work, State: "available", Position: Position{Commit: ordinal, Operation: index}}
		state.Works[work.WorkID] = record
		state.WorkCreators[work.WorkID] = commit.Actor
		if state.admission != nil {
			state.admission.add(record)
		}
		return nil
	case "Observation":
		var observation ObservationOperation
		if err := json.Unmarshal(operation, &observation); err != nil {
			return err
		}
		state.Observations[GateKey(observation.Resource, observation.Condition)] = observation
		return nil
	case "Claim":
		return decodeAndApply(operation, commit, state.applyClaim)
	case "Dispatch":
		return decodeAndApply(operation, commit, state.applyDispatch)
	case "Bound":
		return decodeAndApply(operation, commit, state.applyBound)
	case "Completion":
		return decodeAndApply(operation, commit, state.applyCompletion)
	case "ClaimCancellation":
		return decodeAndApply(operation, commit, state.applyClaimCancellation)
	case "WorkCancellation":
		return decodeAndApply(operation, commit, state.applyWorkCancellation)
	case "Release":
		return decodeAndApply(operation, commit, state.applyRelease)
	case "IssueLink":
		return decodeAndApply(operation, commit, state.applyIssueLink)
	case "IssueComment":
		return decodeAndApply(operation, commit, state.applyIssueComment)
	}
	return queueError("checkpoint_invalid", "a Checkpoint must be the sole first line of the log")
}

func decodeAndApply[T any](operation Operation, commit QueueCommit, apply func(T, QueueCommit) error) error {
	var typed T
	if err := json.Unmarshal(operation, &typed); err != nil {
		return err
	}
	return apply(typed, commit)
}

func (state *Projection) requireWork(id string) (*WorkState, error) {
	if work := state.Works[id]; work != nil {
		return work, nil
	}
	return nil, queueError("work_missing", "operation references missing Work")
}

func (state *Projection) requireClaim(id string) (*ClaimState, error) {
	if claim := state.Claims[id]; claim != nil {
		return claim, nil
	}
	return nil, queueError("claim_missing", "operation references a missing Claim")
}

func (state *Projection) requireDispatch(id string) (*DispatchState, error) {
	if dispatch := state.Dispatches[id]; dispatch != nil {
		return dispatch, nil
	}
	return nil, queueError("dispatch_missing", "operation references a missing Dispatch")
}

// boundToActor reports whether a worker actor is the run attempt bound to dispatch.
func boundToActor(dispatch *DispatchState, actor Actor) bool {
	return dispatch.State == "bound" && actor.Workflow == dispatch.Workflow && actor.RunID == dispatch.Run.RunID && actor.RunAttempt == dispatch.Run.RunAttempt
}

func (state *Projection) closeIfSettled(dispatch *DispatchState) {
	for _, id := range dispatch.Claims {
		if state.Claims[id].State == "open" {
			return
		}
	}
	dispatch.State = "closed"
}

func (state *Projection) cancelClaim(claim *ClaimState, reason string, commit QueueCommit) {
	work := state.Works[claim.WorkID]
	claim.State = "cancelled"
	claim.TerminalCommitID = commit.ID
	claim.CancellationReason = reason
	work.Attempts++
	work.ClaimID = ""
	state.setWorkState(work, "available")
}

func (state *Projection) applyClaim(operation ClaimOperation, commit QueueCommit) error {
	work, err := state.requireWork(operation.WorkID)
	if err != nil {
		return err
	}
	if work.State != "available" || work.ClaimID != "" {
		return queueError("ownership_conflict", "only available Work can be claimed")
	}
	if _, exists := state.Claims[operation.ClaimID]; exists {
		return queueError("claim_conflict", "Claim identity already exists")
	}
	parameters, err := decodeParameters[DispatchParameters](commit.Request)
	if err != nil {
		return err
	}
	profile, routed := parameters.Profiles[work.WorkerProfile]
	if !routed {
		return queueError("request_invalid", "dispatch_next parameters omit the Work's worker profile")
	}
	dispatch := state.Dispatches[operation.DispatchID]
	if dispatch != nil {
		if dispatch.CommitID != commit.ID || dispatch.Pool != work.Pool || dispatch.WorkerProfile != work.WorkerProfile {
			return queueError("dispatch_state_conflict", "a Dispatch groups compatible Claims of one commit")
		}
		if slices.ContainsFunc(dispatch.Claims, func(id string) bool { return state.Claims[id].Handle == operation.Handle }) {
			return queueError("claim_conflict", "Claim handle is not unique within its Dispatch")
		}
	} else {
		dispatch = &DispatchState{DispatchID: operation.DispatchID, RequestID: commit.Request.ID, CommitID: commit.ID, Pool: work.Pool, WorkerProfile: work.WorkerProfile, Workflow: profile.Workflow, State: "reserved", Claims: []string{}}
		state.Dispatches[dispatch.DispatchID] = dispatch
		state.dispatchOrder = append(state.dispatchOrder, dispatch.DispatchID)
	}
	dispatch.Claims = append(dispatch.Claims, operation.ClaimID)
	state.Claims[operation.ClaimID] = &ClaimState{ClaimID: operation.ClaimID, WorkID: work.WorkID, DispatchID: dispatch.DispatchID, Handle: operation.Handle, State: "open", CommitID: commit.ID, RequestID: commit.Request.ID}
	work.ClaimID = operation.ClaimID
	state.setWorkState(work, "claimed")
	return nil
}

func (state *Projection) applyDispatch(operation DispatchOperation, _ QueueCommit) error {
	dispatch, err := state.requireDispatch(operation.DispatchID)
	if err != nil {
		return err
	}
	if dispatch.State != "reserved" {
		return queueError("dispatch_state_conflict", "only a reserved Dispatch can start")
	}
	dispatch.State = "started"
	return nil
}

func (state *Projection) applyBound(operation BoundOperation, commit QueueCommit) error {
	dispatch, err := state.requireDispatch(operation.DispatchID)
	if err != nil {
		return err
	}
	if dispatch.State == "closed" || len(state.OpenClaims(dispatch)) == 0 {
		return queueError("dispatch_state_conflict", "only a Dispatch with open Claims can bind a run")
	}
	actor := commit.Actor
	if actor.Workflow != dispatch.Workflow || actor.RunID != operation.RunID || actor.RunAttempt != operation.RunAttempt {
		return queueError("run_binding_conflict", "a worker may bind only its own run of the dispatched workflow")
	}
	if dispatch.State == "bound" {
		if dispatch.Run.RunID != operation.RunID || operation.RunAttempt < dispatch.Run.RunAttempt {
			return queueError("run_binding_conflict", "the Dispatch is bound to another run")
		}
		if operation.RunAttempt == dispatch.Run.RunAttempt {
			return nil
		}
	}
	dispatch.State = "bound"
	dispatch.Run = &RunBinding{RunID: operation.RunID, RunAttempt: operation.RunAttempt}
	return nil
}

func (state *Projection) applyCompletion(operation CompletionOperation, commit QueueCommit) error {
	claim, err := state.requireClaim(operation.ClaimID)
	if err != nil {
		return err
	}
	if claim.State != "open" {
		return queueError("claim_ineffective", "only an open Claim can complete")
	}
	dispatch := state.Dispatches[claim.DispatchID]
	if operation.WorkID != claim.WorkID || operation.DispatchID != claim.DispatchID || operation.ClaimHandle != claim.Handle || !boundToActor(dispatch, commit.Actor) {
		return queueError("claim_scope_invalid", "Completion is outside the bound run's Claim scope")
	}
	work := state.Works[claim.WorkID]
	claim.State = "completed"
	claim.TerminalCommitID = commit.ID
	claim.Effects = operation.Effects
	work.Effects = operation.Effects
	work.CompletionID = commit.ID
	state.setWorkState(work, "completed")
	state.closeIfSettled(dispatch)
	return nil
}

func (state *Projection) applyClaimCancellation(operation ClaimCancellationOperation, commit QueueCommit) error {
	claim, err := state.requireClaim(operation.ClaimID)
	if err != nil {
		return err
	}
	if claim.WorkID != operation.WorkID {
		return queueError("claim_scope_invalid", "ClaimCancellation names another Work")
	}
	if claim.State == "cancelled" {
		return nil
	}
	if claim.State != "open" {
		return queueError("claim_ineffective", "only an open Claim can be cancelled")
	}
	if state.Works[claim.WorkID].ClaimID != claim.ClaimID {
		return queueError("ownership_conflict", "Claim is not its Work's current owner")
	}
	dispatch := state.Dispatches[claim.DispatchID]
	if commit.Actor.Role == "worker" && !boundToActor(dispatch, commit.Actor) {
		return queueError("claim_scope_invalid", "ClaimCancellation is outside the bound run's Claim scope")
	}
	state.cancelClaim(claim, operation.Reason, commit)
	state.closeIfSettled(dispatch)
	return nil
}

func (state *Projection) applyWorkCancellation(operation WorkCancellationOperation, commit QueueCommit) error {
	work, err := state.requireWork(operation.WorkID)
	if err != nil {
		return err
	}
	switch work.State {
	case "completed":
		return queueError("work_terminal", "completed Work cannot be cancelled")
	case "cancelled":
		if work.CancellationReason == operation.Reason {
			return nil
		}
		return queueError("work_terminal", "cancelled Work keeps its original reason")
	case "claimed":
		return queueError("ownership_conflict", "release the Dispatch before cancelling claimed Work")
	}
	if operation.ClaimID != "" {
		claim := state.Claims[operation.ClaimID]
		if claim == nil || claim.WorkID != work.WorkID || claim.State != "cancelled" {
			return queueError("claim_scope_invalid", "WorkCancellation must name this Work's cancelled Claim")
		}
	}
	work.CancellationReason = operation.Reason
	work.CancellationCommitID = commit.ID
	state.setWorkState(work, "cancelled")
	return nil
}

func (state *Projection) applyRelease(operation ReleaseOperation, commit QueueCommit) error {
	dispatch, err := state.requireDispatch(operation.DispatchID)
	if err != nil {
		return err
	}
	if dispatch.State == "closed" {
		return queueError("dispatch_state_conflict", "Dispatch is already closed")
	}
	for _, id := range dispatch.Claims {
		if claim := state.Claims[id]; claim.State == "open" {
			state.cancelClaim(claim, operation.Reason, commit)
		}
	}
	dispatch.State = "closed"
	return nil
}

func (state *Projection) applyIssueLink(operation IssueLinkOperation, _ QueueCommit) error {
	work, err := state.requireWork(operation.WorkID)
	if err != nil {
		return err
	}
	if operation.ClaimID != "" {
		if claim := state.Claims[operation.ClaimID]; claim == nil || claim.WorkID != work.WorkID {
			return queueError("claim_scope_invalid", "IssueLink Claim belongs to another Work")
		}
	}
	key := issueKey(operation.Resource)
	if work.BackingIssue != nil {
		if issueKey(*work.BackingIssue) == key {
			return nil
		}
		return queueError("issue_binding_conflict", "Work already has a backing Issue")
	}
	index := state.admissionIndex()
	if owner, ok := index.issues[key]; ok && owner != work.WorkID {
		return queueError("issue_binding_conflict", "one Work per backing Issue")
	}
	resource := operation.Resource
	work.BackingIssue = &resource
	index.issues[key] = work.WorkID
	return nil
}

func (state *Projection) applyIssueComment(operation IssueCommentOperation, _ QueueCommit) error {
	work, err := state.requireWork(operation.WorkID)
	if err != nil {
		return err
	}
	target := &work.IssueComment
	if operation.ClaimID != "" {
		claim, err := state.requireClaim(operation.ClaimID)
		if err != nil {
			return err
		}
		if claim.WorkID != work.WorkID {
			return queueError("claim_scope_invalid", "IssueComment Claim belongs to another Work")
		}
		target = &claim.IssueComment
	}
	if *target != "" {
		if *target == operation.CommentID {
			return nil
		}
		return queueError("issue_binding_conflict", "Issue comment binding is immutable")
	}
	*target = operation.CommentID
	return nil
}
