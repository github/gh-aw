package workqueue

import (
	"encoding/json"
	"slices"
	"strconv"
)

type Capacity struct {
	Logical int `json:"logical"`
	Native  int `json:"native"`
	Account int `json:"account"`
}

type ClaimExplanation struct {
	Tip            string    `json:"tip"`
	At             int64     `json:"at"`
	Status         string    `json:"status"`
	BeforeTip      string    `json:"before_tip"`
	Position       Position  `json:"position"`
	RequestID      string    `json:"request_id"`
	CommitID       string    `json:"commit_id"`
	PolicyEpoch    string    `json:"policy_epoch"`
	ClaimID        string    `json:"claim_id"`
	DispatchID     string    `json:"dispatch_id"`
	Handle         string    `json:"claim_handle"`
	WorkID         string    `json:"work_id"`
	Pool           string    `json:"pool"`
	Priority       int       `json:"priority"`
	FairnessKey    string    `json:"fairness_key"`
	WorkPosition   Position  `json:"work_position"`
	Selection      Selection `json:"selection"`
	ClassPassPrior string    `json:"class_pass_prior"`
	KeyPassPrior   string    `json:"key_pass_prior"`
	CapacityBefore Capacity  `json:"capacity_before"`
	CapacityAfter  Capacity  `json:"capacity_after"`
}

type RequestExplanation struct {
	Tip         string             `json:"tip"`
	At          int64              `json:"at"`
	Status      string             `json:"status"`
	RequestID   string             `json:"request_id"`
	CommitID    string             `json:"commit_id"`
	Kind        string             `json:"kind"`
	PolicyEpoch string             `json:"policy_epoch"`
	Claims      []ClaimExplanation `json:"claims"`
}

func capacity(state Projection, work *WorkState) Capacity {
	logical, native, accounts := state.outstanding(work.Pool)
	return Capacity{Logical: logical, Native: native, Account: accounts[work.FairnessKey]}
}

func explainClaimPrefix(ordered []QueueCommit, ordinal int, tip, wanted string) ([]ClaimExplanation, error) {
	if ordinal < 0 || ordinal >= len(ordered) {
		return nil, queueError("claim_missing", "Claim has no original commit")
	}
	commit := ordered[ordinal]
	state, err := Replay(ordered[:ordinal])
	if err != nil {
		return nil, err
	}
	result := []ClaimExplanation{}
	for index, operation := range commit.Operations {
		kind, err := operationKind(operation)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "Observation":
			var observation Observation
			if err := json.Unmarshal(operation, &observation); err != nil {
				return nil, err
			}
			if err := state.recordObservation(observation, commit); err != nil {
				return nil, err
			}
		case "Claim":
			explanation, err := explainAndRecordClaim(&state, operation, commit, Position{Commit: ordinal, Operation: index}, tip)
			if err != nil {
				return nil, err
			}
			if wanted == "" || wanted == explanation.ClaimID {
				result = append(result, explanation)
			}
			if wanted == explanation.ClaimID {
				return result, nil
			}
		}
	}
	return result, nil
}

func explainAndRecordClaim(state *Projection, operation Operation, commit QueueCommit, position Position, tip string) (ClaimExplanation, error) {
	var claim ClaimOperation
	if err := json.Unmarshal(operation, &claim); err != nil {
		return ClaimExplanation{}, err
	}
	work := state.Works[claim.WorkID]
	selection, clocks, err := planNext(*state, work.Pool, commit.At)
	if err != nil {
		return ClaimExplanation{}, err
	}
	if selection.WorkID != claim.WorkID {
		return ClaimExplanation{}, queueError("selection_invalid", "historical Claim differs from its native prefix selector")
	}
	current := state.Clocks[work.Pool]
	explanation := ClaimExplanation{
		Tip: tip, At: commit.At, Status: "committed_claim_prefix",
		BeforeTip: state.Tip, Position: position,
		RequestID: commit.Request.ID, CommitID: commit.ID, PolicyEpoch: commit.PolicyEpoch,
		ClaimID: claim.ClaimID, DispatchID: claim.DispatchID, Handle: claim.Handle,
		WorkID: work.WorkID, Pool: work.Pool, Priority: work.SchedulingPriority(), FairnessKey: work.FairnessKey,
		WorkPosition: work.Position, Selection: selection,
		ClassPassPrior: current.Classes.Pass[strconv.Itoa(work.SchedulingPriority())],
		KeyPassPrior:   current.Keys[work.SchedulingPriority()].Pass[work.FairnessKey],
		CapacityBefore: capacity(*state, work),
	}
	if explanation.ClassPassPrior == "" {
		explanation.ClassPassPrior = "0"
	}
	if explanation.KeyPassPrior == "" {
		explanation.KeyPassPrior = "0"
	}
	recordClaim(state, claim, commit, clocks)
	explanation.CapacityAfter = capacity(*state, work)
	return explanation, nil
}

// ExplainBeforeClaim validates the whole authority, then reduces the exact
// predecessor and earlier in-commit observations/Claims with native reducers.
func ExplainBeforeClaim(commits []QueueCommit, claimID string) (ClaimExplanation, error) {
	state, err := Replay(commits)
	if err != nil {
		return ClaimExplanation{}, err
	}
	claim := state.Claims[claimID]
	if claim == nil {
		return ClaimExplanation{}, queueError("claim_missing", "unknown Claim %q", claimID)
	}
	if explanation, ok := state.ClaimExplanations[claimID]; ok {
		explanation.Tip = state.Tip
		return explanation, nil
	}
	ordered, err := causalOrder(commits)
	if err != nil {
		return ClaimExplanation{}, err
	}
	for ordinal, commit := range ordered {
		if commit.ID == claim.CommitID {
			explanations, err := explainClaimPrefix(ordered, ordinal, state.Tip, claimID)
			if err != nil {
				return ClaimExplanation{}, err
			}
			if len(explanations) != 1 {
				return ClaimExplanation{}, queueError("claim_missing", "Claim has no original causal operation")
			}
			for _, explanation := range explanations {
				return explanation, nil
			}
			return ClaimExplanation{}, queueError("claim_missing", "Claim has no original causal operation")
		}
	}
	return ClaimExplanation{}, queueError("claim_missing", "Claim has no original commit")
}

func ExplainRequest(commits []QueueCommit, requestID string) (RequestExplanation, error) {
	state, err := Replay(commits)
	if err != nil {
		return RequestExplanation{}, err
	}
	commit, ok := state.Requests[requestID]
	if !ok {
		return RequestExplanation{}, queueError("request_missing", "unknown committed request %q", requestID)
	}
	result := RequestExplanation{
		Tip: state.Tip, At: commit.At, Status: "committed_request",
		RequestID: requestID, CommitID: commit.ID, Kind: commit.Request.Kind,
		PolicyEpoch: commit.PolicyEpoch, Claims: []ClaimExplanation{},
	}
	if commit.Request.Kind != "dispatch_next" {
		return result, nil
	}
	for _, operation := range commit.Operations {
		var claim ClaimOperation
		if err := json.Unmarshal(operation, &claim); err != nil || claim.ClaimID == "" {
			continue
		}
		if explanation, ok := state.ClaimExplanations[claim.ClaimID]; ok {
			explanation.Tip = state.Tip
			result.Claims = append(result.Claims, explanation)
		}
	}
	if len(result.Claims) > 0 {
		return result, nil
	}
	ordered, err := causalOrder(commits)
	if err != nil {
		return RequestExplanation{}, err
	}
	for ordinal, item := range ordered {
		if item.ID == commit.ID {
			result.Claims, err = explainClaimPrefix(ordered, ordinal, state.Tip, "")
			return result, err
		}
	}
	return RequestExplanation{}, queueError("request_missing", "request has no original commit")
}

type TraceOptions struct {
	RequestID string
	ClaimID   string
	Offset    int
	Limit     int
}

type TraceEvent struct {
	Position      Position        `json:"position"`
	CommitID      string          `json:"commit_id"`
	Previous      *string         `json:"previous"`
	RequestID     string          `json:"request_id"`
	RequestKind   string          `json:"request_kind"`
	PolicyEpoch   string          `json:"policy_epoch"`
	At            int64           `json:"at"`
	Actor         Actor           `json:"actor"`
	Kind          string          `json:"kind"`
	WorkID        string          `json:"work_id,omitempty"`
	ClaimID       string          `json:"claim_id,omitempty"`
	DispatchID    string          `json:"dispatch_id,omitempty"`
	ClaimHandle   string          `json:"claim_handle,omitempty"`
	CompletionID  string          `json:"completion_id,omitempty"`
	State         string          `json:"state,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	Disposition   string          `json:"disposition,omitempty"`
	Control       string          `json:"control,omitempty"`
	Value         json.RawMessage `json:"value,omitempty"`
	ObservationID string          `json:"observation_id,omitempty"`
	Observations  []string        `json:"observations,omitempty"`
	Run           *RunBinding     `json:"run,omitempty"`
	RunID         string          `json:"run_id,omitempty"`
	RunAttempt    int             `json:"run_attempt,omitempty"`
	Evidence      *TraceEvidence  `json:"evidence,omitempty"`
	Trace         *Trace          `json:"trace,omitempty"`
}

type TraceEvidence struct {
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	CheckedAt  int64  `json:"checked_at"`
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Ref        string `json:"ref"`
	Principal  string `json:"principal"`
	RunID      string `json:"run_id,omitempty"`
	RunAttempt int    `json:"run_attempt,omitempty"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	Effects    string `json:"effects,omitempty"`
	Attempts   int    `json:"attempts,omitempty"`
}

type TracePage struct {
	Tip               string       `json:"tip"`
	At                int64        `json:"at"`
	Status            string       `json:"status"`
	TraceAvailability string       `json:"trace_availability"`
	RequestID         string       `json:"request_id,omitempty"`
	ClaimID           string       `json:"claim_id,omitempty"`
	Offset            int          `json:"offset"`
	Limit             int          `json:"limit"`
	TotalEvents       int          `json:"total_events"`
	NextOffset        *int         `json:"next_offset,omitempty"`
	Events            []TraceEvent `json:"events"`
}

func traceEventForOperation(operation Operation) (TraceEvent, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(operation, &fields); err != nil {
		return TraceEvent{}, err
	}
	if state := fields["state"]; len(state) > 0 && (state[0] == '{' || state[0] == '[') {
		delete(fields, "state")
	}
	sanitized, err := json.Marshal(fields)
	if err != nil {
		return TraceEvent{}, err
	}
	var event TraceEvent
	if err := json.Unmarshal(sanitized, &event); err != nil {
		return TraceEvent{}, err
	}
	return event, nil
}

// TraceQueue exposes bounded ledger provenance, never payloads, descriptors,
// receipt contents or request parameters. Missing telemetry does not lose facts.
func TraceQueue(commits []QueueCommit, options TraceOptions, at int64) (TracePage, error) {
	if (options.RequestID == "") == (options.ClaimID == "") || options.Offset < 0 ||
		options.Limit < 1 || options.Limit > 256 || at < 0 || at > MaxTimestamp {
		return TracePage{}, queueError("inspection_invalid", "trace requires exactly one request/Claim and bounded nonnegative pagination")
	}
	state, err := Replay(commits)
	if err != nil {
		return TracePage{}, err
	}
	scope, err := traceScopeFor(state, options)
	if err != nil {
		return TracePage{}, err
	}
	ordered, err := causalOrder(commits)
	if err != nil {
		return TracePage{}, err
	}
	result := TracePage{
		Tip: state.Tip, At: at, Status: "operator_live_read", TraceAvailability: "ledger_only",
		RequestID: options.RequestID, ClaimID: options.ClaimID,
		Offset: options.Offset, Limit: options.Limit, Events: []TraceEvent{},
	}
	appendEvent := func(event TraceEvent) {
		if event.Trace != nil && event.Trace.TraceID != "" {
			result.TraceAvailability = "correlated"
		}
		if result.TotalEvents >= options.Offset && len(result.Events) < options.Limit {
			result.Events = append(result.Events, event)
		}
		result.TotalEvents++
	}
	if len(ordered) > 0 && isCheckpoint(ordered[0]) {
		for receiptIndex, receipt := range state.CheckpointReceipts {
			commit := QueueCommit{
				ID: receipt.ID, Previous: receipt.Previous,
				Request: Request{ID: receipt.RequestID, Kind: receipt.Kind},
				Actor:   receipt.Actor, PolicyEpoch: receipt.PolicyEpoch, At: receipt.At,
			}
			for index, operation := range receipt.Events {
				event, err := traceEventForOperation(operation)
				if err != nil {
					return TracePage{}, err
				}
				annotateTraceEvent(&event, state, commit, Position{Commit: receiptIndex, Operation: index})
				if scope.matches(event, commit, options) {
					appendEvent(event)
				}
			}
		}
		for ordinal, commit := range ordered[1:] {
			for index, operation := range commit.Operations {
				event, err := traceEventForOperation(operation)
				if err != nil {
					return TracePage{}, err
				}
				if !scope.matches(event, commit, options) {
					continue
				}
				annotateTraceEvent(&event, state, commit, Position{Commit: len(state.CheckpointReceipts) + 1 + ordinal, Operation: index})
				appendEvent(event)
			}
		}
	} else {
		for ordinal, commit := range ordered {
			for index, operation := range commit.Operations {
				event, err := traceEventForOperation(operation)
				if err != nil {
					return TracePage{}, err
				}
				if !scope.matches(event, commit, options) {
					continue
				}
				annotateTraceEvent(&event, state, commit, Position{Commit: ordinal, Operation: index})
				appendEvent(event)
			}
		}
	}
	if options.Offset > result.TotalEvents {
		return TracePage{}, queueError("inspection_invalid", "trace offset exceeds matching events")
	}
	if options.Offset+len(result.Events) < result.TotalEvents {
		next := options.Offset + len(result.Events)
		result.NextOffset = &next
	}
	result.Events = slices.Clip(result.Events)
	return result, nil
}

type traceScope struct {
	works, claims, dispatches, observations, epochs identitySet
}

func traceScopeFor(state Projection, options TraceOptions) (traceScope, error) {
	scope := traceScope{identitySet{}, identitySet{}, identitySet{}, identitySet{}, identitySet{}}
	if options.ClaimID != "" {
		claim := state.Claims[options.ClaimID]
		if claim == nil {
			return scope, queueError("claim_missing", "unknown Claim %q", options.ClaimID)
		}
		scope.claims.add(claim.ClaimID)
		scope.works.add(claim.WorkID)
	} else {
		commit, ok := state.Requests[options.RequestID]
		if !ok {
			return scope, queueError("request_missing", "unknown committed request %q", options.RequestID)
		}
		scope.epochs.add(commit.PolicyEpoch)
		if err := scope.addRequestRoots(state, commit); err != nil {
			return scope, err
		}
		for _, receipt := range state.CheckpointReceipts {
			if receipt.RequestID == options.RequestID {
				if err := scope.addOperationRoots(state, receipt.Events); err != nil {
					return scope, err
				}
				break
			}
		}
	}
	for id, claim := range state.Claims {
		if options.ClaimID == "" && scope.works.contains(claim.WorkID) {
			scope.claims.add(id)
		}
		if scope.claims.contains(id) {
			scope.dispatches.add(claim.DispatchID)
			scope.epochs.add(state.Dispatches[claim.DispatchID].PolicyEpoch)
			scope.observations.add(claim.Observations...)
		}
	}
	return scope, nil
}

func (scope *traceScope) addOperationRoots(state Projection, operations []Operation) error {
	for _, operation := range operations {
		event, err := traceEventForOperation(operation)
		if err != nil {
			return err
		}
		if event.WorkID != "" {
			scope.works.add(event.WorkID)
		}
		if event.ClaimID != "" {
			scope.claims.add(event.ClaimID)
		}
		if event.DispatchID != "" {
			scope.dispatches.add(event.DispatchID)
			if dispatch := state.Dispatches[event.DispatchID]; dispatch != nil {
				for _, member := range dispatch.Claims {
					scope.claims.add(member.ClaimID)
					scope.works.add(member.WorkID)
				}
			}
		}
	}
	return nil
}

func (scope traceScope) addRequestRoots(state Projection, commit QueueCommit) error {
	for _, operation := range commit.Operations {
		var root struct {
			WorkID     string `json:"work_id"`
			ClaimID    string `json:"claim_id"`
			DispatchID string `json:"dispatch_id"`
		}
		if err := json.Unmarshal(operation, &root); err != nil {
			return err
		}
		if root.WorkID != "" {
			scope.works.add(root.WorkID)
		}
		if root.ClaimID != "" {
			scope.claims.add(root.ClaimID)
		}
		if root.DispatchID != "" {
			scope.dispatches.add(root.DispatchID)
			dispatch := state.Dispatches[root.DispatchID]
			for _, member := range dispatch.Claims {
				scope.claims.add(member.ClaimID)
				scope.works.add(member.WorkID)
			}
		}
	}
	return nil
}

func (scope traceScope) matches(event TraceEvent, commit QueueCommit, options TraceOptions) bool {
	return commit.Request.ID == options.RequestID || scope.claims.contains(event.ClaimID) ||
		event.Kind == "Work" && scope.works.contains(event.WorkID) ||
		event.Kind == "WorkCancellation" && scope.works.contains(event.WorkID) ||
		(event.Kind == "Dispatch" || event.Kind == "Release") && scope.dispatches.contains(event.DispatchID) ||
		event.Kind == "Observation" && scope.observations.contains(event.ObservationID) ||
		event.Kind == "Policy" && scope.epochs.contains(commit.PolicyEpoch) ||
		event.Kind == "Control" && len(scope.works) != 0
}

func annotateTraceEvent(event *TraceEvent, state Projection, commit QueueCommit, position Position) {
	if event.ClaimID != "" {
		event.DispatchID = state.Claims[event.ClaimID].DispatchID
		event.ClaimHandle = state.Claims[event.ClaimID].Handle
	}
	event.Position, event.CommitID, event.Previous = position, commit.ID, commit.Previous
	event.RequestID, event.RequestKind, event.PolicyEpoch = commit.Request.ID, commit.Request.Kind, commit.PolicyEpoch
	event.At, event.Actor, event.Trace = commit.At, commit.Actor, commit.Trace
}
