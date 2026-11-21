package workqueue

import (
	"cmp"
	"slices"
)

// FIFOCompare orders Work by causal admission position.
func FIFOCompare(a, b Position) int {
	if a.Commit != b.Commit {
		return a.Commit - b.Commit
	}
	return a.Operation - b.Operation
}

// ClaimIdentity derives deterministic Claim/Dispatch identities: one Claim per Dispatch.
func ClaimIdentity(requestID, workID string) (claimID, dispatchID, handle string) {
	seed := sha256Text(requestID + "\n" + workID)
	return "c_" + seed + "_1", "d_" + seed + "_1", "h1"
}

// EligibleWork returns pool Work in dispatch order: priority ascending, then causal FIFO.
// A nil profiles map admits every worker profile.
func (state Projection) EligibleWork(pool string, profiles map[string]DispatchProfile) []*WorkState {
	eligible := []*WorkState{}
	for _, work := range state.Works {
		if work.Pool != pool || work.State != "available" {
			continue
		}
		if profiles != nil {
			if _, routed := profiles[work.WorkerProfile]; !routed {
				continue
			}
		}
		if ready, _ := state.DependencyStatus(work); !ready {
			continue
		}
		eligible = append(eligible, work)
	}
	slices.SortStableFunc(eligible, func(a, b *WorkState) int {
		return cmp.Or(cmp.Compare(a.Priority, b.Priority), FIFOCompare(a.Position, b.Position))
	})
	return eligible
}

// PoolOccupancy counts non-closed Dispatches and open Claims per fairness key of one pool.
func (state Projection) PoolOccupancy(pool string) (int, map[string]int) {
	dispatches := 0
	accounts := map[string]int{}
	for _, dispatch := range state.Dispatches {
		if dispatch.Pool == pool && dispatch.State != "closed" {
			dispatches++
		}
	}
	for _, claim := range state.Claims {
		if claim.State != "open" {
			continue
		}
		if work := state.Works[claim.WorkID]; work.Pool == pool {
			accounts[work.FairnessKey]++
		}
	}
	return dispatches, accounts
}

// PlanDispatch plans the Claims of one dispatch_next request without mutating state.
func PlanDispatch(state Projection, parameters DispatchParameters, requestID string) (Decision, error) {
	value, err := genericValue(parameters)
	if err != nil {
		return Decision{}, err
	}
	record, err := closed(value, requestKinds["dispatch_next"].parameters, nil, "dispatch_next parameters", "request_invalid")
	if err != nil {
		return Decision{}, err
	}
	if err := validateDispatchNextParameters(record, false); err != nil {
		return Decision{}, err
	}
	if _, err := identity(requestID, "request ID"); err != nil {
		return Decision{}, err
	}
	limit := min(parameters.MaxClaims, parameters.MaxDispatches)
	dispatches, accounts := state.PoolOccupancy(parameters.Pool)
	eligible := state.EligibleWork(parameters.Pool, parameters.Profiles)
	decision := Decision{Operations: []ClaimOperation{}, Dispatches: []string{}}
	switch {
	case len(eligible) > 0:
		decision.Reason = "prefix_complete"
	case slices.ContainsFunc(sortedWorks(state), func(work *WorkState) bool { return work.Pool == parameters.Pool && work.State == "available" }):
		decision.Reason = "no_eligible_work"
	default:
		decision.Reason = "no_work"
	}
	for _, work := range eligible {
		if dispatches >= parameters.Concurrency {
			decision.Reason = "capacity_blocked"
			break
		}
		if len(decision.Operations) >= limit {
			decision.Reason = "claim_budget_reached"
			break
		}
		held := accounts[work.FairnessKey]
		if parameters.PerAccountLimit > 0 && held >= parameters.PerAccountLimit {
			decision.Reason = "account_capacity_blocked"
			continue
		}
		claimID, dispatchID, handle := ClaimIdentity(requestID, work.WorkID)
		decision.Operations = append(decision.Operations, ClaimOperation{Kind: "Claim", WorkID: work.WorkID, ClaimID: claimID, DispatchID: dispatchID, Handle: handle})
		decision.Dispatches = append(decision.Dispatches, dispatchID)
		dispatches++
		accounts[work.FairnessKey] = held + 1
		if decision.Reason == "account_capacity_blocked" {
			decision.Reason = "prefix_complete"
		}
	}
	return decision, nil
}

func sortedWorks(state Projection) []*WorkState {
	works := make([]*WorkState, 0, len(state.Works))
	for _, work := range state.Works {
		works = append(works, work)
	}
	slices.SortFunc(works, func(a, b *WorkState) int { return FIFOCompare(a.Position, b.Position) })
	return works
}

// AssignmentForDispatch returns the worker's view of a Dispatch: its open Claims
// with payloads and predecessor effects.
func AssignmentForDispatch(state Projection, dispatchID string) (Assignment, error) {
	dispatch := state.Dispatches[dispatchID]
	if dispatch == nil {
		return Assignment{}, queueError("dispatch_missing", "requested Dispatch does not exist")
	}
	return state.assignmentOnly(dispatch)
}

func (state Projection) assignmentOnly(dispatch *DispatchState) (Assignment, error) {
	assignment := Assignment{DispatchID: dispatch.DispatchID, Claims: []AssignmentClaim{}}
	for _, claim := range state.OpenClaims(dispatch) {
		work := state.Works[claim.WorkID]
		refs, err := state.resultReferences(work)
		if err != nil {
			return Assignment{}, err
		}
		assignment.Claims = append(assignment.Claims, AssignmentClaim{Handle: claim.Handle, ClaimID: claim.ClaimID, WorkID: work.WorkID, Work: work.Payload, ResultRefs: refs})
	}
	return assignment, nil
}

// AssignmentsForRequest returns the assignments of every Dispatch a request created.
func AssignmentsForRequest(state Projection, requestID string) ([]Assignment, error) {
	assignments := []Assignment{}
	for _, id := range state.dispatchOrder {
		dispatch := state.Dispatches[id]
		if dispatch.RequestID != requestID {
			continue
		}
		assignment, err := state.assignmentOnly(dispatch)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, nil
}
