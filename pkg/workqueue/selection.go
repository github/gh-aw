package workqueue

import (
	"math/big"
	"slices"
	"sort"
	"strconv"
)

func newClock() Clock {
	return Clock{V: "0", Pass: map[string]string{}, Active: map[string]bool{}}
}

func copyClock(clock Clock) Clock {
	result := newClock()
	if clock.V != "" {
		result.V = clock.V
	}
	for key, pass := range clock.Pass {
		result.Pass[key] = pass
	}
	for key, active := range clock.Active {
		result.Active[key] = active
	}
	return result
}

func integer(value string) *big.Int {
	result := new(big.Int)
	if value != "" {
		result.SetString(value, 10)
	}
	return result
}

func tickScale(weights map[string]int) *big.Int {
	scale := big.NewInt(1)
	for _, weight := range weights {
		current := big.NewInt(int64(weight))
		gcd := new(big.Int).GCD(nil, nil, scale, current)
		scale.Div(scale, gcd).Mul(scale, current)
	}
	return scale
}

func pick(clock Clock, eligible []string, weights map[string]int, numeric bool) (string, Clock) {
	result := copyClock(clock)
	scale := tickScale(weights)
	v := integer(result.V)
	eligibleSet := map[string]bool{}
	for _, key := range eligible {
		eligibleSet[key] = true
		if !clock.Active[key] {
			stride := new(big.Int).Div(new(big.Int).Set(scale), big.NewInt(int64(weights[key])))
			floor := new(big.Int).Add(v, stride)
			pass := integer(result.Pass[key])
			if pass.Cmp(floor) < 0 {
				pass.Set(floor)
			}
			result.Pass[key] = pass.String()
		}
	}
	result.Active = eligibleSet
	if len(eligible) == 0 {
		return "", result
	}
	keys := slices.Clone(eligible)
	slices.SortFunc(keys, func(a, b string) int {
		if order := integer(result.Pass[a]).Cmp(integer(result.Pass[b])); order != 0 {
			return order
		}
		if numeric {
			left, _ := strconv.Atoi(a)
			right, _ := strconv.Atoi(b)
			return left - right
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})
	key := keys[0]
	result.V = result.Pass[key]
	stride := new(big.Int).Div(new(big.Int).Set(scale), big.NewInt(int64(weights[key])))
	result.Pass[key] = new(big.Int).Add(integer(result.Pass[key]), stride).String()
	return key, result
}

func (state Projection) outstanding(pool string) (int, int, map[string]int) {
	logical, native := 0, 0
	accounts := map[string]int{}
	for _, claim := range state.Claims {
		work := state.Works[claim.WorkID]
		if work.Pool == pool && claim.State == "open" {
			logical++
			accounts[work.FairnessKey]++
		}
	}
	for _, dispatch := range state.Dispatches {
		if dispatch.Pool == pool && !dispatch.Released {
			native++
		}
	}
	return logical, native, accounts
}

func positionLess(a, b Position) bool {
	return a.Commit < b.Commit || a.Commit == b.Commit && a.Operation < b.Operation
}

func (state Projection) eligibility(work *WorkState, at int64, logical int, accounts map[string]int) string {
	if work.State != "available" {
		return "ownership_" + work.State
	}
	if state.GrantsPaused {
		return "grants_paused"
	}
	pool := state.Policy.Pools[work.Pool]
	if logical >= pool.LogicalLimit {
		return "capacity_blocked"
	}
	if pool.PerAccountLimit > 0 && accounts[work.FairnessKey] >= pool.PerAccountLimit {
		return "account_capacity_blocked"
	}
	if work.Attempts >= pool.Retry.MaxAttempts {
		return "retry_exhausted"
	}
	if work.RetryNotBefore > at {
		return "retry_delayed"
	}
	reason, _ := state.readiness(work, at)
	return reason
}

func planNext(state Projection, poolName string, at int64) (Selection, PoolClocks, error) {
	selection := Selection{Reason: "no_eligible_work", Observations: []string{}}
	if state.Policy == nil {
		return selection, PoolClocks{}, queueError("policy_missing", "queue requires an installed policy")
	}
	pool, ok := state.Policy.Pools[poolName]
	if !ok {
		return selection, PoolClocks{}, queueError("pool_invalid", "unknown pool %s", poolName)
	}
	if at < 0 || at > MaxTimestamp {
		return selection, PoolClocks{}, queueError("timestamp_invalid", "invalid decision timestamp")
	}
	if state.GrantsPaused {
		selection.Reason = "grants_paused"
		return selection, PoolClocks{}, nil
	}
	logical, _, accounts := state.outstanding(poolName)
	if logical >= pool.LogicalLimit {
		selection.Reason = "capacity_blocked"
		return selection, PoolClocks{}, nil
	}
	buckets := map[int]map[string]*WorkState{}
	hasAvailable := false
	for _, work := range state.Works {
		if work.Pool != poolName || work.State != "available" {
			continue
		}
		hasAvailable = true
		if state.eligibility(work, at, logical, accounts) != "ready" {
			continue
		}
		if buckets[work.Priority] == nil {
			buckets[work.Priority] = map[string]*WorkState{}
		}
		if current := buckets[work.Priority][work.FairnessKey]; current == nil || positionLess(work.Position, current.Position) {
			buckets[work.Priority][work.FairnessKey] = work
		}
	}
	if len(buckets) == 0 {
		if !hasAvailable {
			selection.Reason = "no_work"
		}
		return selection, PoolClocks{}, nil
	}
	current := state.Clocks[poolName]
	classes := []string{}
	weights := map[string]int{}
	for i, weight := range state.Policy.ClassWeights {
		weights[strconv.Itoa(i+1)] = weight
	}
	for priority := range buckets {
		classes = append(classes, strconv.Itoa(priority))
	}
	clocks := PoolClocks{Classes: copyClock(current.Classes), Keys: map[int]Clock{}}
	for priority, clock := range current.Keys {
		clocks.Keys[priority] = copyClock(clock)
	}
	var selectedClass string
	if state.Policy.Mode == "strict-priority" {
		sort.Strings(classes)
		selectedClass = classes[0]
	} else {
		selectedClass, clocks.Classes = pick(current.Classes, classes, weights, true)
	}
	priority, _ := strconv.Atoi(selectedClass)
	keys := []string{}
	for key := range buckets[priority] {
		keys = append(keys, key)
	}
	key, keyClock := pick(current.Keys[priority], keys, state.Policy.AccountingWeights, false)
	clocks.Keys[priority] = keyClock
	work := buckets[priority][key]
	_, observations := state.readiness(work, at)
	return Selection{
		WorkID: work.WorkID, Reason: "selected", Observations: observations,
		ClassPass: clocks.Classes.V, KeyPass: keyClock.V,
	}, clocks, nil
}

// PlanNext does not change clocks, active sets, reservations, or ownership.
func PlanNext(state Projection, pool string, at int64) (Selection, error) {
	selection, _, err := planNext(state, pool, at)
	return selection, err
}

func cloneProjection(state Projection) Projection {
	copy := state
	copy.Works = make(map[string]*WorkState, len(state.Works))
	for id, work := range state.Works {
		item := *work
		copy.Works[id] = &item
	}
	copy.Claims = make(map[string]*ClaimState, len(state.Claims))
	for id, claim := range state.Claims {
		item := *claim
		copy.Claims[id] = &item
	}
	copy.Dispatches = make(map[string]*DispatchState, len(state.Dispatches))
	for id, dispatch := range state.Dispatches {
		item := *dispatch
		item.Claims = slices.Clone(dispatch.Claims)
		copy.Dispatches[id] = &item
	}
	copy.Clocks = make(map[string]PoolClocks, len(state.Clocks))
	for id, clocks := range state.Clocks {
		copy.Clocks[id] = clocks
	}
	copy.Observations = make(map[string]*Observation, len(state.Observations))
	for id, observation := range state.Observations {
		copy.Observations[id] = observation
	}
	copy.ObservationWrites = make(map[string]int, len(state.ObservationWrites))
	for id, writes := range state.ObservationWrites {
		copy.ObservationWrites[id] = writes
	}
	return copy
}

func compatible(state Projection, assignment Assignment, work *WorkState) bool {
	if assignment.Pool != work.Pool || assignment.WorkerProfile != work.WorkerProfile {
		return false
	}
	profile := state.Policy.Pools[work.Pool].Profiles[work.WorkerProfile]
	if len(assignment.Claims) >= profile.MaxClaims {
		return false
	}
	for _, claim := range assignment.Claims {
		member := state.Works[claim.WorkID]
		if member.BatchTrustDomain != work.BatchTrustDomain ||
			!profile.ShareKeys && member.FairnessKey != work.FairnessKey {
			return false
		}
	}
	return true
}

func assignmentMember(state Projection, work *WorkState, claim ClaimOperation) AssignmentClaim {
	return AssignmentClaim{
		Handle: claim.Handle, ClaimID: claim.ClaimID, WorkID: work.WorkID,
		Work: work.Payload, ResultRefs: state.resultRefs(work),
	}
}

func recordClaim(state *Projection, operation ClaimOperation, commit QueueCommit, clocks PoolClocks) {
	work := state.Works[operation.WorkID]
	dispatch := state.Dispatches[operation.DispatchID]
	if dispatch == nil {
		dispatch = &DispatchState{
			Profile: state.Policy.Pools[work.Pool].Profiles[work.WorkerProfile],
			Assignment: Assignment{
				Version: Version, DispatchID: operation.DispatchID, RequestID: commit.Request.ID,
				CommitID: commit.ID, PolicyEpoch: commit.PolicyEpoch, Pool: work.Pool,
				WorkerProfile: work.WorkerProfile, Claims: []AssignmentClaim{},
			},
			State: "reserved",
		}
		state.Dispatches[operation.DispatchID] = dispatch
	}
	dispatch.Claims = append(dispatch.Claims, assignmentMember(*state, work, operation))
	state.Claims[operation.ClaimID] = &ClaimState{
		ClaimOperation: operation, State: "open", CommitID: commit.ID, RequestID: commit.Request.ID,
	}
	work.State = "claimed"
	work.ClaimID = operation.ClaimID
	work.Attempts++
	state.Clocks[work.Pool] = clocks
}

func PlanDispatch(state Projection, params DispatchParameters, requestID, commitID string, at int64) (Decision, error) {
	if state.Policy == nil {
		return Decision{}, queueError("policy_missing", "queue requires an installed policy")
	}
	return planDispatch(state, params, requestID, commitID, at, state.Policy.Limits.Operations)
}

func planDispatch(state Projection, params DispatchParameters, requestID, commitID string, at int64, operationBudget int) (Decision, error) {
	decision := Decision{
		Tip: state.Tip, Operations: []Operation{}, Assignments: []Assignment{},
		Reason: "no_work", Next: Selection{Observations: []string{}},
	}
	if params.MaxClaims < 1 || params.MaxClaims > 256 || params.MaxDispatches < 1 ||
		params.MaxDispatches > 256 || params.MaxBytes < 1 || params.MaxBytes > 48<<10 {
		return decision, queueError("request_invalid", "dispatch budgets outside bounds")
	}
	if operationBudget < 0 || operationBudget > state.Policy.Limits.Operations {
		return decision, queueError("request_invalid", "preceding operations exceed installed operation limit")
	}
	working := cloneProjection(state)
	if operationBudget == 0 {
		selection, _, err := planNext(working, params.Pool, at)
		decision.Next, decision.Reason = selection, "operation_budget_blocked"
		return decision, err
	}
	prefix := hashBytes([]byte(requestID))
	commit := QueueCommit{ID: commitID, Request: Request{ID: requestID}, PolicyEpoch: state.PolicyEpoch}
	limit := min(params.MaxClaims, operationBudget)
	for index := 0; index < limit; index++ {
		selection, clocks, err := planNext(working, params.Pool, at)
		if err != nil {
			return decision, err
		}
		decision.Next = selection
		if selection.WorkID == "" {
			decision.Reason = selection.Reason
			break
		}
		work := working.Works[selection.WorkID]
		group := -1
		for i, assignment := range decision.Assignments {
			if !compatible(working, assignment, work) {
				continue
			}
			member := ClaimOperation{
				Kind: "Claim", WorkID: work.WorkID, ClaimID: "c_" + prefix + "_" + strconv.Itoa(index+1),
				DispatchID: assignment.DispatchID, Handle: "h" + strconv.Itoa(len(assignment.Claims)+1),
				Observations: selection.Observations,
			}
			candidate := assignment
			candidate.Claims = append(slices.Clone(candidate.Claims), assignmentMember(working, work, member))
			data, _ := canonicalValue(candidate)
			if int64(len(data)) > min(params.MaxBytes, state.Policy.Limits.AssignmentBytes) {
				continue
			}
			group = i
			break
		}
		opening := group < 0
		if opening {
			if len(decision.Assignments) >= params.MaxDispatches {
				decision.Reason = "dispatch_budget_blocked"
				break
			}
			_, native, _ := working.outstanding(params.Pool)
			if native >= state.Policy.Pools[params.Pool].NativeLimit {
				decision.Reason = "native_capacity_blocked"
				break
			}
			group = len(decision.Assignments)
		}
		var assignment Assignment
		if opening {
			assignment = Assignment{
				Version: Version, DispatchID: "d_" + prefix + "_" + strconv.Itoa(group+1),
				RequestID: requestID, CommitID: commitID, PolicyEpoch: state.PolicyEpoch,
				Pool: work.Pool, WorkerProfile: work.WorkerProfile, Claims: []AssignmentClaim{},
			}
		} else {
			assignment = decision.Assignments[group]
			assignment.Claims = slices.Clone(assignment.Claims)
		}
		claim := ClaimOperation{
			Kind: "Claim", WorkID: work.WorkID, ClaimID: "c_" + prefix + "_" + strconv.Itoa(index+1),
			DispatchID: assignment.DispatchID, Handle: "h" + strconv.Itoa(len(assignment.Claims)+1),
			Observations: selection.Observations,
		}
		assignment.Claims = append(assignment.Claims, assignmentMember(working, work, claim))
		data, _ := canonicalValue(assignment)
		if int64(len(data)) > min(params.MaxBytes, state.Policy.Limits.AssignmentBytes) {
			decision.Reason = "assignment_bytes_blocked"
			break
		}
		if opening {
			decision.Assignments = append(decision.Assignments, assignment)
		} else {
			decision.Assignments[group] = assignment
		}
		decision.Operations = append(decision.Operations, Op(claim))
		recordClaim(&working, claim, commit, clocks)
		decision.Reason = "claim_budget_reached"
		if limit < params.MaxClaims {
			decision.Reason = "operation_budget_reached"
		}
	}
	if len(decision.Operations) > 0 && decision.Reason == "no_work" {
		decision.Reason = "prefix_complete"
	}
	return decision, nil
}
