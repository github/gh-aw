package workqueue

import (
	"bytes"
	"encoding/json"
	"math/big"
	"regexp"
	"slices"
)

var checkpointDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type checkpointReceipt struct {
	ID                 string          `json:"id"`
	RequestID          string          `json:"request_id"`
	Kind               string          `json:"kind"`
	Fingerprint        string          `json:"fingerprint"`
	ParametersDigest   string          `json:"parameters_digest"`
	DispatchParameters json.RawMessage `json:"dispatch_parameters,omitempty"`
	Actor              Actor           `json:"actor"`
	PolicyEpoch        string          `json:"policy_epoch"`
	At                 int64           `json:"at"`
	Claims             []Operation     `json:"claims"`
}

type checkpointSnapshot struct {
	Projection
	Requests         []checkpointReceipt     `json:"requests"`
	SeenEpochs       map[string]bool         `json:"seen_epochs"`
	SeenGenerations  map[string]bool         `json:"seen_generations"`
	ObservationIDs   map[string]*Observation `json:"observation_ids"`
	TerminalBarriers map[string]Operation    `json:"terminal_barriers"`
	Cancellations    map[string]Operation    `json:"cancellations"`
	CompletionTimes  map[string]int64        `json:"completion_times"`
	LastAt           int64                   `json:"last_at"`
}

func checkpointState(state Projection, lastAt int64) (checkpointSnapshot, error) {
	snapshot := checkpointSnapshot{
		Projection: state, Requests: []checkpointReceipt{},
		SeenEpochs: state.SeenEpochs, SeenGenerations: state.SeenGenerations,
		ObservationIDs: state.ObservationIDs, TerminalBarriers: state.TerminalBarriers,
		Cancellations: state.Cancellations, CompletionTimes: map[string]int64{}, LastAt: lastAt,
	}
	for id, work := range state.Works {
		if work.State == "completed" {
			snapshot.CompletionTimes[id] = work.completionAt
		}
	}
	priorReceipts := make(map[string]checkpointReceipt, len(state.CheckpointReceipts))
	for _, receipt := range state.CheckpointReceipts {
		priorReceipts[receipt.RequestID] = receipt
	}
	for _, id := range state.RequestOrder {
		commit, ok := state.Requests[id]
		if !ok {
			return checkpointSnapshot{}, queueError("checkpoint_invalid", "request order omits a committed identity")
		}
		claims := []Operation{}
		for _, operation := range commit.Operations {
			kind, err := operationKind(operation)
			if err != nil {
				return checkpointSnapshot{}, err
			}
			if kind == "Claim" {
				claims = append(claims, operation)
			}
		}
		parametersDigest := ""
		var dispatchParameters json.RawMessage
		if previous, ok := priorReceipts[id]; ok {
			parametersDigest = previous.ParametersDigest
			dispatchParameters = previous.DispatchParameters
		}
		if parametersDigest == "" {
			parameters, err := Canonical(commit.Request.Parameters)
			if err != nil {
				return checkpointSnapshot{}, err
			}
			parametersDigest = hashBytes(parameters)
			if commit.Request.Kind == "dispatch_next" {
				dispatchParameters = commit.Request.Parameters
			}
		}
		snapshot.Requests = append(snapshot.Requests, checkpointReceipt{
			ID: commit.ID, RequestID: id, Kind: commit.Request.Kind,
			Fingerprint: commit.Request.Fingerprint, ParametersDigest: parametersDigest,
			DispatchParameters: dispatchParameters, Actor: commit.Actor,
			PolicyEpoch: commit.PolicyEpoch, At: commit.At, Claims: claims,
		})
	}
	return snapshot, nil
}

// CompactCheckpoint constructs a minimal state checkpoint from validated
// history; priorGitSHA names the Git commit containing the prior ledger.
func CompactCheckpoint(commits []QueueCommit, priorGitSHA string, actor Actor, at int64) ([]QueueCommit, error) {
	if !revisionPattern.MatchString(priorGitSHA) || actor.Role != "administrator" {
		return nil, queueError("checkpoint_invalid", "checkpoint requires an administrator and a prior Git commit SHA")
	}
	state, err := Replay(commits)
	if err != nil {
		return nil, err
	}
	ordered, err := causalOrder(commits)
	if err != nil {
		return nil, err
	}
	if actor.Repository != state.Repository || at < ordered[len(ordered)-1].At || at > MaxTimestamp {
		return nil, queueError("checkpoint_invalid", "checkpoint repository or time is invalid")
	}
	data, err := Serialize(commits)
	if err != nil {
		return nil, err
	}
	snapshot, err := checkpointState(state, ordered[len(ordered)-1].At)
	if err != nil {
		return nil, err
	}
	snapshotData, err := canonicalValue(snapshot)
	if err != nil {
		return nil, err
	}
	parameters := CheckpointParameters{
		PriorGitSHA: priorGitSHA, PriorTip: state.Tip,
		HistorySHA256: hashBytes(data), StateSHA256: hashBytes(snapshotData),
	}
	op, err := Op(CheckpointOperation{
		Kind: "Checkpoint", PriorGitSHA: priorGitSHA, PriorTip: state.Tip,
		HistorySHA256: parameters.HistorySHA256, StateSHA256: parameters.StateSHA256,
		State: snapshotData,
	})
	if err != nil {
		return nil, err
	}
	id := "checkpoint_" + hashBytes([]byte(state.Tip+"\n"+priorGitSHA))
	request, err := NewRequest(id, "checkpoint", actor, parameters)
	if err != nil {
		return nil, err
	}
	checkpoint := QueueCommit{
		Version: Version, ID: "q_" + hashBytes([]byte(id)), Previous: nil,
		Request: request, Actor: actor, PolicyEpoch: state.PolicyEpoch, At: at,
		Operations: []Operation{op},
	}
	if _, err := Replay([]QueueCommit{checkpoint}); err != nil {
		return nil, err
	}
	return []QueueCommit{checkpoint}, nil
}

func restoreCheckpoint(commit QueueCommit) (Projection, error) {
	invalid := func() (Projection, error) {
		return Projection{}, queueError("checkpoint_invalid", "checkpoint state does not bind a valid prior projection")
	}
	if commit.Previous != nil || commit.Actor.Role != "administrator" ||
		len(commit.Operations) != 1 || commit.Request.Kind != "checkpoint" {
		return invalid()
	}
	var op CheckpointOperation
	if err := json.Unmarshal(commit.Operations[0], &op); err != nil ||
		op.Kind != "Checkpoint" || !revisionPattern.MatchString(op.PriorGitSHA) {
		return invalid()
	}
	if err := validateRequest(commit); err != nil {
		return invalid()
	}
	var snapshot checkpointSnapshot
	decoder := json.NewDecoder(bytes.NewReader(op.State))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return invalid()
	}
	data, err := canonicalValue(snapshot)
	input, canonicalErr := Canonical(op.State)
	if err != nil || canonicalErr != nil || !bytes.Equal(data, input) || hashBytes(data) != op.StateSHA256 ||
		snapshot.Tip != op.PriorTip || snapshot.Repository != commit.Actor.Repository ||
		snapshot.PolicyEpoch != commit.PolicyEpoch || snapshot.Policy == nil ||
		commit.At < snapshot.LastAt {
		return invalid()
	}
	if err := ValidatePolicy(*snapshot.Policy); err != nil {
		return invalid()
	}
	state := snapshot.Projection
	if state.Works == nil || state.Claims == nil || state.Dispatches == nil ||
		state.Observations == nil || state.Clocks == nil || state.ObservationWrites == nil ||
		snapshot.ObservationIDs == nil || snapshot.TerminalBarriers == nil ||
		snapshot.Cancellations == nil || len(snapshot.Requests) != state.Stats.Transactions ||
		state.Stats.Work != len(state.Works) || state.Stats.Claims != len(state.Claims) {
		return invalid()
	}
	state.Requests = map[string]QueueCommit{}
	state.RequestOrder = []string{}
	commitIDs := map[string]bool{}
	state.SeenEpochs, state.SeenGenerations = snapshot.SeenEpochs, snapshot.SeenGenerations
	state.ObservationIDs, state.TerminalBarriers, state.Cancellations = snapshot.ObservationIDs, snapshot.TerminalBarriers, snapshot.Cancellations
	if !state.SeenEpochs[state.PolicyEpoch] || !state.SeenGenerations[state.CredentialGeneration] {
		return invalid()
	}
	for _, receipt := range snapshot.Requests {
		if receipt.ID == "" || receipt.RequestID == "" || !checkpointDigestPattern.MatchString(receipt.Fingerprint) ||
			!checkpointDigestPattern.MatchString(receipt.ParametersDigest) ||
			receipt.Actor.Repository != state.Repository || receipt.Kind == "" ||
			receipt.At < 0 || receipt.At > MaxTimestamp || receipt.PolicyEpoch == "" ||
			state.Requests[receipt.RequestID].ID != "" || commitIDs[receipt.ID] ||
			validateActorOrigin(receipt.Actor) != nil || validateRequestRole(receipt.Actor, receipt.Kind) != nil {
			return invalid()
		}
		commitIDs[receipt.ID] = true
		for _, operation := range receipt.Claims {
			if kind, err := operationKind(operation); err != nil || kind != "Claim" {
				return invalid()
			}
		}
		parameters := json.RawMessage("null")
		if receipt.Kind == "dispatch_next" {
			if len(receipt.DispatchParameters) == 0 {
				return invalid()
			}
			parameters = receipt.DispatchParameters
		} else if len(receipt.DispatchParameters) > 0 {
			return invalid()
		}
		state.Requests[receipt.RequestID] = QueueCommit{
			Version: Version, ID: receipt.ID, Request: Request{
				ID: receipt.RequestID, Kind: receipt.Kind,
				Fingerprint: receipt.Fingerprint, Parameters: parameters,
			}, Actor: receipt.Actor, PolicyEpoch: receipt.PolicyEpoch,
			At: receipt.At, Operations: receipt.Claims,
		}
		state.RequestOrder = append(state.RequestOrder, receipt.RequestID)
	}
	for index, receipt := range snapshot.Requests {
		if receipt.Kind != "submit" {
			continue
		}
		nodes := map[int]WorkDefinition{}
		for _, work := range state.Works {
			if work.Position.Commit == index {
				nodes[work.Position.Operation] = work.WorkDefinition
			}
		}
		if len(nodes) == 0 {
			return invalid()
		}
		ordered := make([]WorkDefinition, len(nodes))
		for offset := range ordered {
			node, ok := nodes[offset]
			if !ok {
				return invalid()
			}
			ordered[offset] = node
		}
		parameters, err := canonicalValue(SubmitParameters{Nodes: ordered})
		if err != nil {
			return invalid()
		}
		commit := state.Requests[receipt.RequestID]
		commit.Request.Parameters = parameters
		state.Requests[receipt.RequestID] = commit
	}
	for _, receipt := range snapshot.Requests {
		if receipt.Kind == "submit" || receipt.Kind == "dispatch_next" {
			parameters := state.Requests[receipt.RequestID].Request.Parameters
			canonical, err := Canonical(parameters)
			if err != nil || hashBytes(canonical) != receipt.ParametersDigest {
				return invalid()
			}
			fingerprint, err := Fingerprint(receipt.Actor, receipt.Kind, parameters)
			if err != nil || fingerprint != receipt.Fingerprint {
				return invalid()
			}
		}
	}
	state.CheckpointReceipts = snapshot.Requests
	for id, work := range state.Works {
		if work == nil || work.WorkID != id || work.Position.Commit < 0 ||
			work.Position.Commit >= len(snapshot.Requests) || work.Attempts < 0 {
			return invalid()
		}
		if snapshot.Requests[work.Position.Commit].Kind != "submit" ||
			(work.State == "claimed" && (work.ClaimID == "" ||
				state.Claims[work.ClaimID] == nil ||
				state.Claims[work.ClaimID].State != "open")) {
			return invalid()
		}
		if work.State != "completed" && work.State != "cancelled" {
			pool, ok := state.Policy.Pools[work.Pool]
			if !ok || pool.Profiles[work.WorkerProfile].Workflow == "" ||
				work.Attempts > pool.Retry.MaxAttempts {
				return invalid()
			}
		}
		if work.State == "completed" {
			at, ok := snapshot.CompletionTimes[id]
			if !ok || at < 0 || at > MaxTimestamp {
				return invalid()
			}
			work.completionAt = at
		}
	}
	for id, claim := range state.Claims {
		if claim == nil || claim.ClaimID != id || state.Works[claim.WorkID] == nil ||
			state.Dispatches[claim.DispatchID] == nil {
			return invalid()
		}
	}
	for id, dispatch := range state.Dispatches {
		if dispatch == nil || dispatch.DispatchID != id ||
			state.Requests[dispatch.RequestID].ID != dispatch.CommitID {
			return invalid()
		}
	}
	for _, clocks := range state.Clocks {
		for _, clock := range append([]Clock{clocks.Classes}, valuesClocks(clocks.Keys)...) {
			value, ok := new(big.Int).SetString(clock.V, 10)
			if !ok || value.Sign() < 0 || value.String() != clock.V {
				return invalid()
			}
			for _, pass := range clock.Pass {
				value, ok := new(big.Int).SetString(pass, 10)
				if !ok || value.Sign() < 0 || value.String() != pass {
					return invalid()
				}
			}
		}
	}
	if _, used := state.Requests[commit.Request.ID]; used {
		return invalid()
	}
	state.Tip = commit.ID
	state.Requests[commit.Request.ID] = commit
	state.RequestOrder = append(state.RequestOrder, commit.Request.ID)
	size, err := canonicalValue(commit)
	if err != nil {
		return invalid()
	}
	state.LedgerBytes = int64(len(size) + 1)
	if state.LedgerBytes+remainingHeadroom(state) > state.Policy.Limits.LedgerBytes+state.Policy.Limits.RecoveryBytes {
		return invalid()
	}
	return state, nil
}

func valuesClocks(clocks map[int]Clock) []Clock {
	keys := make([]int, 0, len(clocks))
	for key := range clocks {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]Clock, 0, len(keys))
	for _, key := range keys {
		result = append(result, clocks[key])
	}
	return result
}
