package workqueue

import (
	"encoding/json"
	"slices"
)

func remainingHeadroom(state Projection) int64 {
	if state.Policy == nil {
		return 0
	}
	limits := state.Policy.Limits
	var reserve int64
	for _, work := range state.Works {
		if work.State == "completed" && work.Barrier == "pending" {
			reserve += 2*limits.ResultBytes + 2*limits.EvidenceBytes + 8192
		} else if work.State != "completed" && work.State != "cancelled" {
			remaining := max(0, state.Policy.Pools[work.Pool].Retry.MaxAttempts-work.Attempts)
			reserve += int64(remaining)*(4*limits.EvidenceBytes+8192) +
				2*limits.ResultBytes + 2*limits.EvidenceBytes + 8192
			if work.State == "claimed" {
				// The current outcome must remain spendable even on the final retry.
				reserve += 8192
			}
		}
	}
	for _, dispatch := range state.Dispatches {
		if !dispatch.Released {
			remaining := max(0, state.Policy.Pools[dispatch.Pool].Reconciliation.MaxAttempts+4-dispatch.LifecycleWrites)
			reserve += int64(remaining)*(2*limits.EvidenceBytes+12288) + 2*limits.EvidenceBytes + 8192
		}
	}
	return reserve
}

func Replay(commits []QueueCommit) (Projection, error) {
	state := newProjection()
	ordered, err := causalOrder(commits)
	if err != nil {
		return state, err
	}
	epochs := map[string]bool{}
	generations := map[string]bool{state.CredentialGeneration: true}
	for ordinal, commit := range ordered {
		if ordinal == 0 {
			state.Repository = commit.Actor.Repository
		} else if commit.Actor.Repository != state.Repository {
			return state, queueError("ledger_foreign", "commit originates from another queue repository")
		}
		if _, ok := state.Requests[commit.Request.ID]; ok {
			return state, queueError("request_reuse", "request %s appears in multiple commits", commit.Request.ID)
		}
		if err := validateRequest(commit); err != nil {
			return state, err
		}
		if err := authorizeWorkerQueueRequest(state, commit.Actor, commit.Request); err != nil {
			return state, err
		}
		isPolicy := len(commit.Operations) == 1 && operationKind(commit.Operations[0]) == "Policy"
		if ordinal == 0 && !isPolicy {
			return state, queueError("policy_missing", "genesis must install exactly one policy")
		}
		if !isPolicy && commit.PolicyEpoch != state.PolicyEpoch {
			return state, queueError("policy_epoch_invalid", "commit names a non-current epoch")
		}
		if state.Policy != nil && len(commit.Operations) > state.Policy.Limits.Operations {
			return state, queueError("operation_limit", "commit exceeds installed operation limit")
		}
		hasAdmission := false
		hasObservations := false
		firstClaim := -1
		for index, operation := range commit.Operations {
			kind := operationKind(operation)
			if kind == "Claim" {
				if firstClaim < 0 {
					firstClaim = index
				}
				continue
			}
			if firstClaim >= 0 {
				return state, queueError("packing_invalid", "observations must precede the atomic Claim prefix")
			}
			switch kind {
			case "Policy":
				if !isPolicy || !state.quiescent() {
					return state, queueError("policy_not_quiescent", "policy changes require queue-wide drain")
				}
				var operation struct {
					Epoch  string `json:"epoch"`
					Policy Policy `json:"policy"`
				}
				_ = json.Unmarshal(commit.Operations[index], &operation)
				if commit.PolicyEpoch != operation.Epoch || epochs[operation.Epoch] {
					return state, queueError("policy_epoch_invalid", "policy epoch is reused or inconsistent")
				}
				if err := validatePolicy(operation.Policy); err != nil {
					return state, err
				}
				epochs[operation.Epoch] = true
				state.Policy, state.PolicyEpoch = &operation.Policy, operation.Epoch
				state.Clocks = map[string]PoolClocks{}
				state.ObservationWrites = map[string]int{}
			case "Control":
				var control struct {
					Control string          `json:"control"`
					Value   json.RawMessage `json:"value"`
				}
				_ = json.Unmarshal(operation, &control)
				if control.Control == "credential_generation" {
					var generation string
					if err := json.Unmarshal(control.Value, &generation); err != nil || generation == "" {
						return state, queueError("control_invalid", "credential generation must be an opaque string")
					}
					if generation != state.CredentialGeneration && generations[generation] {
						return state, queueError("credential_generation_reused", "credential cutover cannot revive a stale observation generation")
					}
					generations[generation] = true
					state.CredentialGeneration = generation
				} else {
					var paused bool
					if err := json.Unmarshal(control.Value, &paused); err != nil {
						return state, queueError("control_invalid", "pause controls require a boolean")
					}
					if control.Control == "admission_paused" {
						state.AdmissionPaused = paused
					} else {
						state.GrantsPaused = paused
					}
				}
			case "Work":
				hasAdmission = true
				var node WorkDefinition
				_ = json.Unmarshal(operation, &node)
				if err := state.admitWork(node, commit, Position{Commit: ordinal, Operation: index}); err != nil {
					return state, err
				}
			case "Observation":
				hasObservations = true
				var observation Observation
				_ = json.Unmarshal(operation, &observation)
				if err := state.recordObservation(observation, commit); err != nil {
					return state, err
				}
			case "Completion":
				if err := state.applyCompletion(operation, commit); err != nil {
					return state, err
				}
			case "ClaimCancellation":
				if err := state.cancelClaim(operation, commit); err != nil {
					return state, err
				}
			case "WorkCancellation":
				if err := state.cancelWork(operation, commit); err != nil {
					return state, err
				}
			case "Dispatch":
				if err := state.applyDispatch(operation, commit); err != nil {
					return state, err
				}
			case "Release":
				if err := state.release(operation, commit); err != nil {
					return state, err
				}
			case "Result", "DeliveryFailure":
				if err := state.settleResult(operation, commit, kind == "DeliveryFailure"); err != nil {
					return state, err
				}
			default:
				return state, queueError("unsupported_protocol", "unknown operation")
			}
		}
		if hasAdmission {
			if err := state.validateGraph(); err != nil {
				return state, err
			}
		}
		if firstClaim >= 0 {
			var params DispatchParameters
			_ = json.Unmarshal(commit.Request.Parameters, &params)
			expected, err := planDispatch(state, params, commit.Request.ID, commit.ID, commit.At, state.Policy.Limits.Operations-firstClaim)
			if err != nil {
				return state, err
			}
			actual := commit.Operations[firstClaim:]
			if len(actual) == 0 || !sameJSON(expected.Operations, actual) {
				return state, queueError("selection_invalid", "Claims do not match the maximal deterministic fair prefix")
			}
			for _, operation := range actual {
				hasAdmission = true
				var claim ClaimOperation
				_ = json.Unmarshal(operation, &claim)
				selection, clocks, err := planNext(state, params.Pool, commit.At)
				if err != nil || selection.WorkID != claim.WorkID {
					return state, queueError("selection_invalid", "Claim is not the next fair winner")
				}
				if _, ok := state.Claims[claim.ClaimID]; ok {
					return state, queueError("claim_conflict", "Claim identity already exists")
				}
				if existing := state.Dispatches[claim.DispatchID]; existing != nil && existing.CommitID != commit.ID {
					return state, queueError("assignment_immutable", "cannot extend an existing dispatch")
				}
				recordClaim(&state, claim, commit, clocks)
			}
		} else if commit.Request.Kind == "dispatch_next" {
			return state, queueError("request_invalid", "no-grant evaluation must not consume request identity")
		}
		for _, work := range state.Works {
			if work.State == "available" && work.Attempts >= state.Policy.Pools[work.Pool].Retry.MaxAttempts {
				return state, queueError("retry_exhausted", "cancel exhausted Work in the same recovery commit")
			}
		}
		data, _ := canonicalValue(commit)
		state.LedgerBytes += int64(len(data) + 1)
		if state.LedgerBytes > state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes {
			return state, queueError("ledger_limit", "total ledger capacity exhausted; preserve history")
		}
		headroom := remainingHeadroom(state)
		if headroom > state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes-state.LedgerBytes {
			return state, queueError("ledger_limit", "write would consume remaining bounded closure/recovery headroom")
		}
		if hasAdmission && (state.LedgerBytes > state.Policy.Limits.LedgerBytes ||
			headroom > state.Policy.Limits.RecoveryBytes) {
			return state, queueError("ledger_limit", "new admission would consume bounded closure/recovery headroom")
		}
		if hasObservations && state.LedgerBytes > state.Policy.Limits.LedgerBytes {
			return state, queueError("ledger_limit", "optional observations cannot consume closure/recovery headroom")
		}
		state.Tip = commit.ID
		state.Requests[commit.Request.ID] = commit
	}
	state.Stats.Transactions = len(ordered)
	state.Stats.Work, state.Stats.Claims = len(state.Works), len(state.Claims)
	state.Stats.Nodes = state.nodeCount()
	for _, work := range state.Works {
		switch work.State {
		case "available":
			state.Stats.Available++
		case "claimed":
			state.Stats.Claimed++
		case "completed":
			state.Stats.Completed++
		case "cancelled":
			state.Stats.Cancelled++
		}
	}
	for _, dispatch := range state.Dispatches {
		if !dispatch.Released {
			state.Stats.Dispatches++
		}
	}
	return state, nil
}

func ProposedCommitID(state Projection, request Request) string {
	return "q_" + hashBytes([]byte(state.Tip+"\n"+request.ID))
}

func BuildCandidate(commits []QueueCommit, actor Actor, request Request, at int64) ([]QueueCommit, *QueueCommit, Decision, error) {
	return buildCandidateWithObservations(commits, actor, request, at, nil)
}

func buildCandidateWithObservations(commits []QueueCommit, actor Actor, request Request, at int64, observations []Observation) ([]QueueCommit, *QueueCommit, Decision, error) {
	if err := validateRequestOrigin(actor, request); err != nil {
		return nil, nil, Decision{}, err
	}
	state, err := Replay(commits)
	if err != nil {
		return nil, nil, Decision{}, err
	}
	if existing, ok := state.Requests[request.ID]; ok {
		if existing.Request.Fingerprint != request.Fingerprint || !sameJSON(existing.Actor, actor) {
			return nil, nil, Decision{}, queueError("request_reuse", "stable request was reused with different meaning")
		}
		decision := Decision{Tip: state.Tip, Reason: "already_committed", Operations: existing.Operations, Assignments: []Assignment{}}
		seen := map[string]bool{}
		for _, op := range existing.Operations {
			if operationKind(op) != "Claim" {
				continue
			}
			var claim ClaimOperation
			_ = json.Unmarshal(op, &claim)
			if !seen[claim.DispatchID] {
				decision.Assignments = append(decision.Assignments, state.Dispatches[claim.DispatchID].Assignment)
				seen[claim.DispatchID] = true
			}
		}
		return commits, &existing, decision, nil
	}
	if actor.Repository != state.Repository {
		return nil, nil, Decision{}, queueError("actor_unauthorized", "request originates from another queue repository")
	}
	if err := validateRequestRole(actor, request.Kind); err != nil {
		return nil, nil, Decision{}, err
	}
	if err := authorizeWorkerQueueRequest(state, actor, request); err != nil {
		return nil, nil, Decision{}, err
	}
	previous := state.Tip
	commit := QueueCommit{
		Version: Version, ID: ProposedCommitID(state, request), Previous: &previous,
		Request: request, Actor: actor, PolicyEpoch: state.PolicyEpoch, At: at,
	}
	decision := Decision{Tip: state.Tip, Assignments: []Assignment{}}
	switch request.Kind {
	case "dispatch_next":
		var params DispatchParameters
		if err := json.Unmarshal(request.Parameters, &params); err != nil {
			return nil, nil, decision, err
		}
		working := cloneProjection(state)
		for _, observation := range observations {
			if err := working.recordObservation(observation, commit); err != nil {
				return nil, nil, decision, err
			}
		}
		decision, err = planDispatch(working, params, request.ID, commit.ID, at, state.Policy.Limits.Operations-len(observations))
		if err != nil || len(decision.Operations) == 0 {
			return commits, nil, decision, err
		}
		commit.Operations = []Operation{}
		for _, observation := range observations {
			commit.Operations = append(commit.Operations, Op(observation))
		}
		commit.Operations = append(commit.Operations, decision.Operations...)
	case "submit":
		var params SubmitParameters
		_ = json.Unmarshal(request.Parameters, &params)
		existing := len(params.Nodes) > 0
		seen := map[string]bool{}
		for _, node := range params.Nodes {
			if seen[node.WorkID] {
				return nil, nil, decision, queueError("work_conflict", "submission repeats an immutable node identity")
			}
			seen[node.WorkID] = true
			previous := state.Works[node.WorkID]
			if previous == nil {
				existing = false
			} else if !sameJSON(previous.WorkDefinition, node) {
				return nil, nil, decision, queueError("work_conflict", "immutable node differs from its accepted definition")
			}
		}
		if existing {
			for _, node := range params.Nodes {
				if err := state.admitWork(node, commit, Position{}); err != nil {
					return nil, nil, decision, err
				}
			}
			decision.Reason, decision.Operations = "already_submitted", []Operation{}
			return commits, nil, decision, nil
		}
		commit.Operations = []Operation{}
		for _, node := range params.Nodes {
			commit.Operations = append(commit.Operations, Op(node))
		}
	case "finish":
		var params FinishParameters
		_ = json.Unmarshal(request.Parameters, &params)
		claim, err := state.scopedClaim(actor, params.DispatchID, params.ClaimHandle)
		if err != nil {
			return nil, nil, decision, err
		}
		if claim.State == params.Outcome {
			decision.Reason = "already_" + params.Outcome
			return commits, nil, decision, nil
		}
		if params.Outcome == "completed" {
			commit.Operations = []Operation{Op(map[string]any{
				"kind": "Completion", "work_id": claim.WorkID, "claim_id": claim.ClaimID,
				"dispatch_id": params.DispatchID, "claim_handle": params.ClaimHandle,
				"run_id": actor.RunID, "run_attempt": 1,
			})}
		} else {
			work := state.Works[claim.WorkID]
			commit.Operations = []Operation{Op(map[string]any{
				"kind": "ClaimCancellation", "work_id": claim.WorkID, "claim_id": claim.ClaimID,
				"reason": "worker_cancelled", "retry_not_before": at + state.Policy.Pools[work.Pool].Retry.BackoffMS,
			})}
			if work.Attempts >= state.Policy.Pools[work.Pool].Retry.MaxAttempts {
				commit.Operations = append(commit.Operations, Op(map[string]any{
					"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "attempts_exhausted",
				}))
			}
		}
	default:
		var params OperationsParameters
		_ = json.Unmarshal(request.Parameters, &params)
		commit.Operations = params.Operations
		if request.Kind == "policy" && len(commit.Operations) == 1 {
			var params struct {
				Epoch string `json:"epoch"`
			}
			_ = json.Unmarshal(commit.Operations[0], &params)
			commit.PolicyEpoch = params.Epoch
		}
	}
	next := append(slices.Clone(commits), commit)
	if _, err := Replay(next); err != nil {
		return nil, nil, decision, err
	}
	return next, &commit, decision, nil
}

func Genesis(actor Actor, policy Policy, requestID, epoch string, at int64) (QueueCommit, error) {
	operations := []Operation{Op(map[string]any{"kind": "Policy", "epoch": epoch, "policy": policy})}
	request, err := NewRequest(requestID, "policy", actor, OperationsParameters{Operations: operations})
	if err != nil {
		return QueueCommit{}, err
	}
	commit := QueueCommit{
		Version: Version, ID: "q_" + hashBytes([]byte(requestID)), Previous: nil,
		Request: request, Actor: actor, PolicyEpoch: epoch, At: at, Operations: operations,
	}
	_, err = Replay([]QueueCommit{commit})
	return commit, err
}
