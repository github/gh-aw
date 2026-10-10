package workqueue

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"regexp"
	"slices"
)

var checkpointDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type checkpointReceipt struct {
	ID                 string             `json:"id"`
	Previous           *string            `json:"previous"`
	RequestID          string             `json:"request_id"`
	Kind               string             `json:"kind"`
	PriorTip           string             `json:"prior_tip,omitempty"`
	Fingerprint        string             `json:"fingerprint"`
	ParametersDigest   string             `json:"parameters_digest"`
	DispatchParameters json.RawMessage    `json:"dispatch_parameters,omitempty"`
	Actor              Actor              `json:"actor"`
	PolicyEpoch        string             `json:"policy_epoch"`
	At                 int64              `json:"at"`
	Events             []Operation        `json:"events"`
	ClaimExplanations  []ClaimExplanation `json:"claim_explanations"`
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

func checkpointTraceOperation(operation Operation) (Operation, error) {
	if kind, err := operationKind(operation); err == nil && kind == "Deployment" {
		return Canonical(operation)
	}
	event, err := traceEventForOperation(operation)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, field := range []string{"position", "commit_id", "previous", "request_id", "request_kind", "policy_epoch", "at", "actor", "pool", "worker_profile", "profile"} {
		delete(fields, field)
	}
	data, err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return Canonical(data)
}

func encodeCheckpointSnapshot(snapshot checkpointSnapshot) ([]byte, error) {
	logical, err := canonicalValue(snapshot)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(logical, &fields); err != nil {
		return nil, err
	}
	requests := fields["requests"]
	delete(fields, "requests")
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(requests); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	fields["requests_compressed"], err = json.Marshal(base64.StdEncoding.EncodeToString(compressed.Bytes()))
	if err != nil {
		return nil, err
	}
	wire, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return Canonical(wire)
}

func decodeCheckpointSnapshot(data []byte) (checkpointSnapshot, []byte, error) {
	invalid := func() (checkpointSnapshot, []byte, error) {
		return checkpointSnapshot{}, nil, queueError("checkpoint_invalid", "checkpoint state does not bind a valid prior projection")
	}
	canonicalInput, err := Canonical(data)
	if err != nil || !bytes.Equal(canonicalInput, data) {
		return invalid()
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return invalid()
	}
	var encoded string
	compressed, ok := fields["requests_compressed"]
	if !ok || json.Unmarshal(compressed, &encoded) != nil {
		return invalid()
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return invalid()
	}
	reader := flate.NewReader(bytes.NewReader(decoded))
	requests, err := io.ReadAll(io.LimitReader(reader, (64<<20)+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(requests) > 64<<20 {
		return invalid()
	}
	canonicalRequests, err := Canonical(requests)
	if err != nil || !bytes.Equal(canonicalRequests, requests) {
		return invalid()
	}
	if _, ok := fields["requests"]; ok {
		return invalid()
	}
	delete(fields, "requests_compressed")
	fields["requests"] = requests
	logicalJSON, err := json.Marshal(fields)
	if err != nil {
		return invalid()
	}
	logical, err := Canonical(logicalJSON)
	if err != nil {
		return invalid()
	}
	var snapshot checkpointSnapshot
	decoder := json.NewDecoder(bytes.NewReader(logical))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil {
		return invalid()
	}
	return snapshot, logical, nil
}

func checkpointState(ordered []QueueCommit, state Projection, lastAt int64) (checkpointSnapshot, error) {
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
	commits := make(map[string]QueueCommit, len(ordered))
	for _, commit := range ordered {
		commits[commit.Request.ID] = commit
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
		priorTip := ""
		claimExplanations := []ClaimExplanation{}
		events := []Operation{}
		if previous, ok := priorReceipts[id]; ok {
			parametersDigest = previous.ParametersDigest
			dispatchParameters = previous.DispatchParameters
			priorTip = previous.PriorTip
			events = previous.Events
			claimExplanations = previous.ClaimExplanations
			for index, explanation := range claimExplanations {
				if current, ok := state.ClaimExplanations[explanation.ClaimID]; ok {
					current.Tip = state.Tip
					claimExplanations[index] = current
				}
			}
		} else if original, ok := commits[id]; ok {
			if original.Request.Kind == "checkpoint" {
				var checkpoint CheckpointOperation
				if len(original.Operations) != 1 || json.Unmarshal(original.Operations[0], &checkpoint) != nil {
					return checkpointSnapshot{}, queueError("checkpoint_invalid", "checkpoint receipt omits its prior tip")
				}
				priorTip = checkpoint.PriorTip
			}
			for _, operation := range original.Operations {
				event, err := checkpointTraceOperation(operation)
				if err != nil {
					return checkpointSnapshot{}, err
				}
				events = append(events, event)
			}
			for _, operation := range claims {
				var claim ClaimOperation
				if err := json.Unmarshal(operation, &claim); err != nil {
					return checkpointSnapshot{}, err
				}
				explanation, ok := state.ClaimExplanations[claim.ClaimID]
				if !ok {
					return checkpointSnapshot{}, queueError("checkpoint_invalid", "claim inspection provenance is missing")
				}
				claimExplanations = append(claimExplanations, explanation)
			}
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
			ID: commit.ID, Previous: commit.Previous, RequestID: id, Kind: commit.Request.Kind,
			PriorTip:    priorTip,
			Fingerprint: commit.Request.Fingerprint, ParametersDigest: parametersDigest,
			DispatchParameters: dispatchParameters, Actor: commit.Actor,
			PolicyEpoch: commit.PolicyEpoch, At: commit.At, Events: events,
			ClaimExplanations: claimExplanations,
		})
	}
	if _, err := immutableWorkCreators(state); err != nil {
		return checkpointSnapshot{}, err
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
	snapshot, err := checkpointState(ordered, state, ordered[len(ordered)-1].At)
	if err != nil {
		return nil, err
	}
	logicalSnapshotData, err := canonicalValue(snapshot)
	if err != nil {
		return nil, err
	}
	snapshotData, err := encodeCheckpointSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	parameters := CheckpointParameters{
		PriorGitSHA: priorGitSHA, PriorTip: state.Tip,
		HistorySHA256: hashBytes(data), StateSHA256: hashBytes(logicalSnapshotData),
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
	snapshot, data, err := decodeCheckpointSnapshot(op.State)
	if err != nil || hashBytes(data) != op.StateSHA256 ||
		snapshot.Tip != op.PriorTip || snapshot.Repository != commit.Actor.Repository ||
		snapshot.PolicyEpoch != commit.PolicyEpoch || snapshot.Policy == nil ||
		commit.At < snapshot.LastAt {
		return invalid()
	}
	if err := ValidatePolicy(*snapshot.Policy); err != nil {
		return invalid()
	}
	state := snapshot.Projection
	if err := validateDeploymentCheckpoint(state, snapshot.Requests); err != nil {
		return invalid()
	}
	if state.Works == nil || state.Claims == nil || state.Dispatches == nil ||
		state.Observations == nil || state.Clocks == nil || state.ObservationWrites == nil ||
		snapshot.ObservationIDs == nil || snapshot.TerminalBarriers == nil ||
		snapshot.Cancellations == nil ||
		len(snapshot.Requests) != state.Stats.Transactions ||
		state.Stats.Work != len(state.Works) || state.Stats.Claims != len(state.Claims) {
		return invalid()
	}
	state.Requests = map[string]QueueCommit{}
	state.RequestOrder = []string{}
	state.WorkCreators = map[string]Actor{}
	state.ClaimExplanations = map[string]ClaimExplanation{}
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
		claims := []Operation{}
		for _, operation := range receipt.Events {
			kind, err := operationKind(operation)
			if err != nil {
				return invalid()
			}
			if kind == "Work" {
				var work WorkDefinition
				if json.Unmarshal(operation, &work) != nil || work.WorkID == "" || state.Works[work.WorkID] == nil {
					return invalid()
				}
				state.WorkCreators[work.WorkID] = receipt.Actor
			}
			if kind == "Claim" {
				claims = append(claims, operation)
			}
		}
		for _, operation := range claims {
			if kind, err := operationKind(operation); err != nil || kind != "Claim" {
				return invalid()
			}
		}
		if len(receipt.ClaimExplanations) != len(claims) || receipt.Events == nil {
			return invalid()
		}
		for index, operation := range claims {
			var claim ClaimOperation
			if json.Unmarshal(operation, &claim) != nil {
				return invalid()
			}
			explanation := receipt.ClaimExplanations[index]
			if explanation.ClaimID != claim.ClaimID || explanation.CommitID != receipt.ID ||
				explanation.RequestID != receipt.RequestID || explanation.WorkID != claim.WorkID {
				return invalid()
			}
			state.ClaimExplanations[claim.ClaimID] = explanation
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
			Version: Version, ID: receipt.ID, Previous: receipt.Previous, Request: Request{
				ID: receipt.RequestID, Kind: receipt.Kind,
				Fingerprint: receipt.Fingerprint, Parameters: parameters,
			}, Actor: receipt.Actor, PolicyEpoch: receipt.PolicyEpoch,
			At: receipt.At, Operations: receipt.Events,
		}
		state.RequestOrder = append(state.RequestOrder, receipt.RequestID)
	}
	if len(state.WorkCreators) != len(state.Works) {
		return invalid()
	}
	for id, work := range state.Works {
		if work.Position.Commit < 0 || work.Position.Commit >= len(snapshot.Requests) {
			return invalid()
		}
		receipt := snapshot.Requests[work.Position.Commit]
		creator, ok := state.WorkCreators[id]
		if !ok || receipt.Kind != "submit" || !sameJSON(creator, receipt.Actor) {
			return invalid()
		}
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
		firstOperation := 0
		if index == 0 && receipt.Previous == nil && isPolicyOperation(receipt.Events) {
			firstOperation = 1
		}
		for offset := range ordered {
			node, ok := nodes[offset+firstOperation]
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
		creator, ok := state.WorkCreators[id]
		if !ok || creator.Repository != state.Repository || validateActorOrigin(creator) != nil {
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
	type checkpointStart struct {
		principal string
		actor     Actor
	}
	starts := map[string]checkpointStart{}
	for _, receipt := range snapshot.Requests {
		for _, event := range receipt.Events {
			var start dispatchLifecycle
			if json.Unmarshal(event, &start) == nil && start.DispatchID != "" && start.State == "started" && start.CredentialPrincipal != "" {
				if _, exists := starts[start.DispatchID]; exists {
					return invalid()
				}
				starts[start.DispatchID] = checkpointStart{principal: start.CredentialPrincipal, actor: receipt.Actor}
			}
		}
	}
	for id, dispatch := range state.Dispatches {
		if dispatch == nil || dispatch.DispatchID != id ||
			state.Requests[dispatch.RequestID].ID != dispatch.CommitID {
			return invalid()
		}
		if dispatch.Profile.Principal != "" && dispatch.CredentialPrincipal == "" {
			continue
		}
		if dispatch.Profile.Principal != "" && !decimalIdentity(dispatch.Profile.Principal) ||
			dispatch.CredentialPrincipal != "" && !decimalIdentity(dispatch.CredentialPrincipal) ||
			dispatch.Profile.Principal != "" && dispatch.CredentialPrincipal != "" &&
				dispatch.Profile.Principal != dispatch.CredentialPrincipal ||
			dispatch.Run != nil && dispatch.Run.Principal != DispatchPrincipal(dispatch) {
			return invalid()
		}
		if dispatch.State == "reserved" {
			if dispatch.Sender != nil || dispatch.CredentialPrincipal != "" || dispatch.Run != nil {
				return invalid()
			}
			continue
		}
		if !decimalIdentity(DispatchPrincipal(dispatch)) || dispatch.Sender == nil {
			return invalid()
		}
		start, found := starts[id]
		if !found || start.principal != dispatch.CredentialPrincipal || !sameJSON(start.actor, dispatch.Sender) {
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
