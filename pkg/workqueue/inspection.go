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
	commit := ordered[ordinal]
	state, err := Replay(ordered[:ordinal])
	if err != nil {
		return nil, err
	}
	result := []ClaimExplanation{}
	for index, operation := range commit.Operations {
		switch operationKind(operation) {
		case "Observation":
			var observation Observation
			if err := json.Unmarshal(operation, &observation); err != nil {
				return nil, err
			}
			if err := state.recordObservation(observation, commit); err != nil {
				return nil, err
			}
		case "Claim":
			var claim ClaimOperation
			if err := json.Unmarshal(operation, &claim); err != nil {
				return nil, err
			}
			work := state.Works[claim.WorkID]
			selection, clocks, err := planNext(state, work.Pool, commit.At)
			if err != nil {
				return nil, err
			}
			if selection.WorkID != claim.WorkID {
				return nil, queueError("selection_invalid", "historical Claim differs from its native prefix selector")
			}
			current := state.Clocks[work.Pool]
			explanation := ClaimExplanation{
				Tip: tip, At: commit.At, Status: "committed_claim_prefix",
				BeforeTip: state.Tip, Position: Position{Commit: ordinal, Operation: index},
				RequestID: commit.Request.ID, CommitID: commit.ID, PolicyEpoch: commit.PolicyEpoch,
				ClaimID: claim.ClaimID, DispatchID: claim.DispatchID, Handle: claim.Handle,
				WorkID: work.WorkID, Pool: work.Pool, Priority: work.Priority, FairnessKey: work.FairnessKey,
				WorkPosition: work.Position, Selection: selection,
				ClassPassPrior: current.Classes.Pass[strconv.Itoa(work.Priority)],
				KeyPassPrior:   current.Keys[work.Priority].Pass[work.FairnessKey],
				CapacityBefore: capacity(state, work),
			}
			if explanation.ClassPassPrior == "" {
				explanation.ClassPassPrior = "0"
			}
			if explanation.KeyPassPrior == "" {
				explanation.KeyPassPrior = "0"
			}
			recordClaim(&state, claim, commit, clocks)
			explanation.CapacityAfter = capacity(state, work)
			if wanted == "" || wanted == claim.ClaimID {
				result = append(result, explanation)
			}
			if wanted == claim.ClaimID {
				return result, nil
			}
		}
	}
	return result, nil
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
			return explanations[0], nil
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
	works, claims, dispatches, observations, epochs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	if options.ClaimID != "" {
		claim := state.Claims[options.ClaimID]
		if claim == nil {
			return TracePage{}, queueError("claim_missing", "unknown Claim %q", options.ClaimID)
		}
		claims[claim.ClaimID], works[claim.WorkID] = true, true
	} else {
		commit, ok := state.Requests[options.RequestID]
		if !ok {
			return TracePage{}, queueError("request_missing", "unknown committed request %q", options.RequestID)
		}
		epochs[commit.PolicyEpoch] = true
		for _, operation := range commit.Operations {
			var scope struct {
				WorkID     string `json:"work_id"`
				ClaimID    string `json:"claim_id"`
				DispatchID string `json:"dispatch_id"`
			}
			_ = json.Unmarshal(operation, &scope)
			if scope.WorkID != "" {
				works[scope.WorkID] = true
			}
			if scope.ClaimID != "" {
				claims[scope.ClaimID] = true
			}
			if scope.DispatchID != "" {
				dispatches[scope.DispatchID] = true
				dispatch := state.Dispatches[scope.DispatchID]
				for _, member := range dispatch.Claims {
					claims[member.ClaimID], works[member.WorkID] = true, true
				}
			}
		}
	}
	for id, claim := range state.Claims {
		if options.ClaimID == "" && works[claim.WorkID] {
			claims[id] = true
		}
		if claims[id] {
			dispatches[claim.DispatchID] = true
			epochs[state.Dispatches[claim.DispatchID].PolicyEpoch] = true
			for _, id := range claim.Observations {
				observations[id] = true
			}
		}
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
	for ordinal, commit := range ordered {
		for index, operation := range commit.Operations {
			var event TraceEvent
			if err := json.Unmarshal(operation, &event); err != nil {
				return TracePage{}, err
			}
			if commit.Request.ID != options.RequestID && !claims[event.ClaimID] &&
				!(event.Kind == "Work" && works[event.WorkID]) &&
				!(event.Kind == "WorkCancellation" && works[event.WorkID]) &&
				!((event.Kind == "Dispatch" || event.Kind == "Release") && dispatches[event.DispatchID]) &&
				!(event.Kind == "Observation" && observations[event.ObservationID]) &&
				!(event.Kind == "Policy" && epochs[commit.PolicyEpoch]) &&
				!(event.Kind == "Control" && len(works) > 0) {
				continue
			}
			if event.ClaimID != "" {
				event.DispatchID = state.Claims[event.ClaimID].DispatchID
				event.ClaimHandle = state.Claims[event.ClaimID].Handle
			}
			event.Position, event.CommitID, event.Previous = Position{Commit: ordinal, Operation: index}, commit.ID, commit.Previous
			event.RequestID, event.RequestKind, event.PolicyEpoch = commit.Request.ID, commit.Request.Kind, commit.PolicyEpoch
			event.At, event.Actor, event.Trace = commit.At, commit.Actor, commit.Trace
			if event.Trace != nil && event.Trace.TraceID != "" {
				result.TraceAvailability = "correlated"
			}
			if result.TotalEvents >= options.Offset && len(result.Events) < options.Limit {
				result.Events = append(result.Events, event)
			}
			result.TotalEvents++
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
