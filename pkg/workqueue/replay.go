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

// RecoveryHeadroom reports the outstanding closure reservation for a replayed projection.
func RecoveryHeadroom(state Projection) int64 {
	return remainingHeadroom(state)
}

func Replay(commits []QueueCommit) (Projection, error) {
	state := newProjection()
	ordered, err := causalOrder(commits)
	if err != nil {
		return state, err
	}
	epochs := identitySet{}
	generations := identitySet{state.CredentialGeneration: {}}
	offset := 0
	checkpointCount := 0
	if kind, kindErr := operationKind(ordered[0].Operations[0]); kindErr == nil && kind == "Checkpoint" {
		state, err = restoreCheckpoint(ordered[0])
		if err != nil {
			return state, err
		}
		offset = state.Stats.Transactions
		checkpointCount = 1
		for epoch := range state.SeenEpochs {
			epochs.add(epoch)
		}
		for generation := range state.SeenGenerations {
			generations.add(generation)
		}
		ordered = ordered[1:]
	}
	for ordinal, commit := range ordered {
		if err := state.replayCommit(commit, offset+checkpointCount+ordinal, epochs, generations); err != nil {
			return state, err
		}
	}
	state.Stats = Stats{}
	state.updateReplayStats(offset + checkpointCount + len(ordered))
	for epoch := range epochs {
		state.SeenEpochs[epoch] = true
	}
	for generation := range generations {
		state.SeenGenerations[generation] = true
	}
	for id, explanation := range state.ClaimExplanations {
		explanation.Tip = state.Tip
		state.ClaimExplanations[id] = explanation
	}
	return state, nil
}

func (state *Projection) validateReplayCommit(commit QueueCommit, ordinal int) (bool, error) {
	if ordinal == 0 {
		state.Repository = commit.Actor.Repository
	} else if commit.Actor.Repository != state.Repository {
		return false, queueError("ledger_foreign", "commit originates from another queue repository")
	}
	if _, ok := state.Requests[commit.Request.ID]; ok {
		return false, queueError("request_reuse", "request %s appears in multiple commits", commit.Request.ID)
	}
	if err := validateRequest(commit); err != nil {
		return false, err
	}
	if err := authorizeWorkerQueueRequest(*state, commit.Actor, commit.Request); err != nil {
		return false, err
	}
	only, unique := singleOperation(commit.Operations)
	isPolicy := false
	if unique {
		kind, err := operationKind(only)
		if err != nil {
			return false, err
		}
		isPolicy = kind == "Policy"
	}
	if ordinal == 0 && !isPolicy {
		return false, queueError("policy_missing", "genesis must install exactly one policy")
	}
	if !isPolicy && commit.PolicyEpoch != state.PolicyEpoch {
		return false, queueError("policy_epoch_invalid", "commit names a non-current epoch")
	}
	if state.Policy != nil && len(commit.Operations) > state.Policy.Limits.Operations {
		return false, queueError("operation_limit", "commit exceeds installed operation limit")
	}
	return isPolicy, nil
}

func (state *Projection) replayCommit(commit QueueCommit, ordinal int, epochs, generations identitySet) error {
	isPolicy, err := state.validateReplayCommit(commit, ordinal)
	if err != nil {
		return err
	}
	hasAdmission := false
	hasObservations := false
	firstClaim := -1
	for index, operation := range commit.Operations {
		kind, err := operationKind(operation)
		if err != nil {
			return err
		}
		if kind == "Claim" {
			if firstClaim < 0 {
				firstClaim = index
			}
			continue
		}
		if firstClaim >= 0 {
			return queueError("packing_invalid", "observations must precede the atomic Claim prefix")
		}
		hasAdmission = hasAdmission || kind == "Work"
		hasObservations = hasObservations || kind == "Observation" || kind == "IssueLink" || kind == "IssueComment"
		if err := state.replayOperation(operation, kind, commit, Position{Commit: ordinal, Operation: index}, isPolicy, epochs, generations); err != nil {
			return err
		}
	}
	if hasAdmission {
		if err := state.validateGraph(); err != nil {
			return err
		}
	}
	if firstClaim >= 0 {
		if err := state.replayClaimPrefix(commit, firstClaim, ordinal); err != nil {
			return err
		}
		hasAdmission = true
	} else if commit.Request.Kind == "dispatch_next" {
		return queueError("request_invalid", "no-grant evaluation must not consume request identity")
	}
	if err := state.accountReplayCommit(commit, hasAdmission, hasObservations); err != nil {
		return err
	}
	state.Tip = commit.ID
	state.Requests[commit.Request.ID] = commit
	state.RequestOrder = append(state.RequestOrder, commit.Request.ID)
	return nil
}

func (state *Projection) replayOperation(operation Operation, kind string, commit QueueCommit, position Position, isPolicy bool, epochs, generations identitySet) error {
	switch kind {
	case "Policy":
		return state.installReplayPolicy(operation, commit, isPolicy, epochs)
	case "Control":
		return state.applyReplayControl(operation, generations)
	case "Work":
		var node WorkDefinition
		if err := json.Unmarshal(operation, &node); err != nil {
			return err
		}
		if err := state.admitWork(node, commit, position); err != nil {
			return err
		}
		if _, exists := state.WorkCreators[node.WorkID]; !exists {
			state.WorkCreators[node.WorkID] = commit.Actor
		}
		return nil
	case "Observation":
		var observation Observation
		if err := json.Unmarshal(operation, &observation); err != nil {
			return err
		}
		return state.recordObservation(observation, commit)
	case "Completion":
		return state.applyCompletion(operation, commit)
	case "ClaimCancellation":
		if err := state.cancelClaim(operation, commit); err != nil {
			return err
		}
		var value ClaimOperation
		_ = json.Unmarshal(operation, &value)
		state.Cancellations[value.ClaimID] = operation
		return nil
	case "WorkCancellation":
		if err := state.cancelWork(operation, commit); err != nil {
			return err
		}
		var value WorkDefinition
		_ = json.Unmarshal(operation, &value)
		state.Cancellations[value.WorkID] = operation
		return nil
	case "WorkPriority":
		return state.reprioritizeWork(operation)
	case "IssueLink", "IssueComment":
		return state.applyIssueBinding(operation, kind, commit)
	case "Dispatch":
		return state.applyDispatch(operation, commit)
	case "Release":
		return state.release(operation, commit)
	case "Result", "DeliveryFailure":
		if err := state.settleResult(operation, commit, kind == "DeliveryFailure"); err != nil {
			return err
		}
		var value WorkDefinition
		_ = json.Unmarshal(operation, &value)
		state.TerminalBarriers[value.WorkID] = operation
		return nil
	default:
		return queueError("unsupported_protocol", "unknown operation")
	}
}

func (state *Projection) installReplayPolicy(op Operation, commit QueueCommit, isPolicy bool, epochs identitySet) error {
	if !isPolicy || !state.quiescent() {
		return queueError("policy_not_quiescent", "policy changes require queue-wide drain")
	}
	var operation struct {
		Epoch  string `json:"epoch"`
		Policy Policy `json:"policy"`
	}
	if err := json.Unmarshal(op, &operation); err != nil {
		return err
	}
	if commit.PolicyEpoch != operation.Epoch || epochs.contains(operation.Epoch) {
		return queueError("policy_epoch_invalid", "policy epoch is reused or inconsistent")
	}
	if err := validatePolicy(operation.Policy); err != nil {
		return err
	}
	epochs.add(operation.Epoch)
	state.Policy, state.PolicyEpoch = &operation.Policy, operation.Epoch
	state.Clocks = map[string]PoolClocks{}
	state.ObservationWrites = map[string]int{}
	return nil
}

func (state *Projection) applyReplayControl(operation Operation, generations identitySet) error {
	var control struct {
		Control string          `json:"control"`
		Value   json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(operation, &control); err != nil {
		return err
	}
	if control.Control == "credential_generation" {
		var generation string
		if err := json.Unmarshal(control.Value, &generation); err != nil || generation == "" {
			return queueError("control_invalid", "credential generation must be an opaque string")
		}
		if generation != state.CredentialGeneration && generations.contains(generation) {
			return queueError("credential_generation_reused", "credential cutover cannot revive a stale observation generation")
		}
		generations.add(generation)
		state.CredentialGeneration = generation
	} else {
		var paused bool
		if err := json.Unmarshal(control.Value, &paused); err != nil {
			return queueError("control_invalid", "pause controls require a boolean")
		}
		if control.Control == "admission_paused" {
			state.AdmissionPaused = paused
		} else {
			state.GrantsPaused = paused
		}
	}
	return nil
}

func (state *Projection) replayClaimPrefix(commit QueueCommit, firstClaim, ordinal int) error {
	var params DispatchParameters
	if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil {
		return err
	}
	expected, err := planDispatch(*state, params, commit.Request.ID, commit.ID, commit.At, state.Policy.Limits.Operations-firstClaim)
	if err != nil {
		return err
	}
	if firstClaim < 0 || firstClaim >= len(commit.Operations) {
		return queueError("selection_invalid", "Claims do not match the maximal deterministic fair prefix")
	}
	actual := commit.Operations[firstClaim:]
	if len(actual) == 0 || !sameJSON(expected.Operations, actual) {
		return queueError("selection_invalid", "Claims do not match the maximal deterministic fair prefix")
	}
	for index, operation := range actual {
		var claim ClaimOperation
		if err := json.Unmarshal(operation, &claim); err != nil {
			return err
		}
		if _, ok := state.Claims[claim.ClaimID]; ok {
			return queueError("claim_conflict", "Claim identity already exists")
		}
		if existing := state.Dispatches[claim.DispatchID]; existing != nil && existing.CommitID != commit.ID {
			return queueError("assignment_immutable", "cannot extend an existing dispatch")
		}
		explanation, err := explainAndRecordClaim(state, operation, commit, Position{Commit: ordinal, Operation: firstClaim + index}, state.Tip)
		if err != nil || explanation.WorkID != claim.WorkID {
			return queueError("selection_invalid", "Claim is not the next fair winner")
		}
		state.ClaimExplanations[claim.ClaimID] = explanation
	}
	return nil
}

func (state *Projection) accountReplayCommit(commit QueueCommit, hasAdmission, hasObservations bool) error {
	for _, work := range state.Works {
		if work.State == "available" && work.Attempts >= state.Policy.Pools[work.Pool].Retry.MaxAttempts {
			return queueError("retry_exhausted", "cancel exhausted Work in the same recovery commit")
		}
	}
	data, err := canonicalValue(commit)
	if err != nil {
		return err
	}
	state.LedgerBytes += int64(len(data) + 1)
	if state.LedgerBytes > state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes {
		return queueError("ledger_limit", "total ledger capacity exhausted; preserve history")
	}
	headroom := remainingHeadroom(*state)
	if headroom > state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes-state.LedgerBytes {
		return queueError("ledger_limit", "write would consume remaining bounded closure/recovery headroom")
	}
	if hasAdmission && (state.LedgerBytes > state.Policy.Limits.LedgerBytes ||
		headroom > state.Policy.Limits.RecoveryBytes) {
		return queueError("ledger_limit", "new admission would consume bounded closure/recovery headroom")
	}
	if hasObservations && state.LedgerBytes > state.Policy.Limits.LedgerBytes {
		return queueError("ledger_limit", "optional observations cannot consume closure/recovery headroom")
	}
	return nil
}

func (state *Projection) updateReplayStats(transactions int) {
	state.Stats.Transactions = transactions
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
		decision, err := committedDecision(state, existing)
		if err != nil {
			return nil, nil, Decision{}, err
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
	decision, unchanged, err := populateCandidate(state, &commit, observations)
	if unchanged {
		return commits, nil, decision, err
	}
	if err != nil {
		return nil, nil, decision, err
	}
	next := append(slices.Clone(commits), commit)
	if _, err := Replay(next); err != nil {
		return nil, nil, decision, err
	}
	return next, &commit, decision, nil
}

func committedDecision(state Projection, existing QueueCommit) (Decision, error) {
	decision := Decision{Tip: state.Tip, Reason: "already_committed", Operations: existing.Operations, Assignments: []Assignment{}}
	seen := identitySet{}
	for _, op := range existing.Operations {
		kind, err := operationKind(op)
		if err != nil {
			return Decision{}, err
		}
		if kind != "Claim" {
			continue
		}
		var claim ClaimOperation
		if err := json.Unmarshal(op, &claim); err != nil {
			return Decision{}, err
		}
		if !seen.contains(claim.DispatchID) {
			decision.Assignments = append(decision.Assignments, state.Dispatches[claim.DispatchID].Assignment)
			seen.add(claim.DispatchID)
		}
	}
	return decision, nil
}

func populateCandidate(state Projection, commit *QueueCommit, observations []Observation) (Decision, bool, error) {
	decision := Decision{Tip: state.Tip, Assignments: []Assignment{}}
	switch commit.Request.Kind {
	case "dispatch_next":
		return populateDispatchCandidate(state, commit, observations, decision)
	case "submit":
		return populateSubmissionCandidate(state, commit, decision)
	case "finish":
		return populateFinishCandidate(state, commit, decision)
	default:
		return decision, false, populateOperationsCandidate(commit)
	}
}

func populateDispatchCandidate(state Projection, commit *QueueCommit, observations []Observation, decision Decision) (Decision, bool, error) {
	var params DispatchParameters
	if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil {
		return decision, false, err
	}
	working := cloneProjection(state)
	for _, observation := range observations {
		if err := working.recordObservation(observation, *commit); err != nil {
			return decision, false, err
		}
	}
	decision, err := planDispatch(working, params, commit.Request.ID, commit.ID, commit.At, state.Policy.Limits.Operations-len(observations))
	if err != nil || len(decision.Operations) == 0 {
		return decision, true, err
	}
	commit.Operations = []Operation{}
	for _, observation := range observations {
		if err := appendOperation(&commit.Operations, observation); err != nil {
			return decision, false, err
		}
	}
	commit.Operations = append(commit.Operations, decision.Operations...)
	return decision, false, nil
}

func populateSubmissionCandidate(state Projection, commit *QueueCommit, decision Decision) (Decision, bool, error) {
	var params SubmitParameters
	if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil {
		return decision, false, err
	}
	existing := len(params.Nodes) > 0
	seen := identitySet{}
	for _, node := range params.Nodes {
		if seen.contains(node.WorkID) {
			return decision, false, queueError("work_conflict", "submission repeats an immutable node identity")
		}
		seen.add(node.WorkID)
		previous := state.Works[node.WorkID]
		if previous == nil {
			existing = false
		} else if !sameJSON(previous.WorkDefinition, node) {
			return decision, false, queueError("work_conflict", "immutable node differs from its accepted definition")
		}
	}
	if existing {
		for _, node := range params.Nodes {
			if err := state.admitWork(node, *commit, Position{}); err != nil {
				return decision, false, err
			}
		}
		decision.Reason, decision.Operations = "already_submitted", []Operation{}
		return decision, true, nil
	}
	commit.Operations = []Operation{}
	for _, node := range params.Nodes {
		if err := appendOperation(&commit.Operations, node); err != nil {
			return decision, false, err
		}
	}
	return decision, false, nil
}

func populateFinishCandidate(state Projection, commit *QueueCommit, decision Decision) (Decision, bool, error) {
	var params FinishParameters
	if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil {
		return decision, false, err
	}
	claim, err := state.scopedClaim(commit.Actor, params.DispatchID, params.ClaimHandle)
	if err != nil {
		return decision, false, err
	}
	if claim.State == params.Outcome {
		decision.Reason = "already_" + params.Outcome
		return decision, true, nil
	}
	if params.Outcome == "completed" {
		if err := appendOperation(&commit.Operations, map[string]any{
			"kind": "Completion", "work_id": claim.WorkID, "claim_id": claim.ClaimID,
			"dispatch_id": params.DispatchID, "claim_handle": params.ClaimHandle,
			"run_id": commit.Actor.RunID, "run_attempt": 1,
		}); err != nil {
			return decision, false, err
		}
	} else {
		work := state.Works[claim.WorkID]
		if err := appendOperation(&commit.Operations, map[string]any{
			"kind": "ClaimCancellation", "work_id": claim.WorkID, "claim_id": claim.ClaimID,
			"reason": "worker_cancelled", "retry_not_before": commit.At + state.Policy.Pools[work.Pool].Retry.BackoffMS,
		}); err != nil {
			return decision, false, err
		}
		if work.Attempts >= state.Policy.Pools[work.Pool].Retry.MaxAttempts {
			if err := appendOperation(&commit.Operations, map[string]any{
				"kind": "WorkCancellation", "work_id": work.WorkID, "reason": "attempts_exhausted",
			}); err != nil {
				return decision, false, err
			}
		}
	}
	return decision, false, nil
}

func populateOperationsCandidate(commit *QueueCommit) error {
	var params OperationsParameters
	if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil {
		return err
	}
	commit.Operations = params.Operations
	if only, unique := singleOperation(commit.Operations); commit.Request.Kind == "policy" && unique {
		var params struct {
			Epoch string `json:"epoch"`
		}
		if err := json.Unmarshal(only, &params); err != nil {
			return err
		}
		commit.PolicyEpoch = params.Epoch
	}
	return nil
}

func Genesis(actor Actor, policy Policy, requestID, epoch string, at int64) (QueueCommit, error) {
	operation, err := Op(map[string]any{"kind": "Policy", "epoch": epoch, "policy": policy})
	if err != nil {
		return QueueCommit{}, err
	}
	operations := []Operation{operation}
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
