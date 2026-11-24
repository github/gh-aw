package workqueue

import (
	"encoding/json"
	"maps"
	"slices"
)

var (
	checkpointFields    = []string{"repository", "tip", "commits", "works", "claims", "dispatches", "observations", "requests", "work_creators"}
	workStates          = []string{"available", "claimed", "completed", "cancelled"}
	claimStates         = []string{"open", "completed", "cancelled"}
	dispatchStates      = []string{"reserved", "started", "bound", "closed"}
	workRecordRequired  = append(slices.Clone(workRequired), "state", "position", "attempts")
	workRecordOptional  = append(slices.Clone(workOptional), "claim_id", "completion_id", "effects", "cancellation_reason", "cancellation_commit_id", "issue_comment")
	claimRecordRequired = []string{"claim_id", "work_id", "dispatch_id", "handle", "state", "commit_id", "request_id"}
	claimRecordOptional = []string{"terminal_commit_id", "cancellation_reason", "effects", "issue_comment"}
	dispatchFields      = []string{"dispatch_id", "request_id", "commit_id", "pool", "worker_profile", "workflow", "state", "run", "launch", "claims"}
)

type projectionRecord struct {
	Repository   string                          `json:"repository"`
	Tip          string                          `json:"tip"`
	Commits      int                             `json:"commits"`
	Works        map[string]*WorkState           `json:"works"`
	Claims       map[string]*ClaimState          `json:"claims"`
	Dispatches   map[string]*DispatchState       `json:"dispatches"`
	Observations map[string]ObservationOperation `json:"observations"`
	Requests     []string                        `json:"requests"`
	WorkCreators map[string]Actor                `json:"work_creators"`
}

// SerializeProjection returns a projection in the canonical checkpoint `state` format.
func SerializeProjection(state Projection) ([]byte, error) {
	// An empty projection still serializes its requests as [], never null.
	requests := slices.AppendSeq(make([]string, 0, len(state.Requests)), maps.Keys(state.Requests))
	slices.Sort(requests)
	record := projectionRecord{Repository: state.Repository, Tip: state.Tip, Commits: state.Commits, Works: state.Works, Claims: state.Claims, Dispatches: state.Dispatches, Observations: state.Observations, Requests: requests, WorkCreators: state.WorkCreators}
	if record.Works == nil {
		record = projectionRecord{Repository: state.Repository, Tip: state.Tip, Commits: state.Commits, Works: map[string]*WorkState{}, Claims: map[string]*ClaimState{}, Dispatches: map[string]*DispatchState{}, Observations: map[string]ObservationOperation{}, Requests: requests, WorkCreators: map[string]Actor{}}
	}
	return CanonicalValue(record)
}

func terminalWork(work *WorkState) bool {
	return work.State == "completed" || work.State == "cancelled"
}

// checkpointProjection is the part of a projection a Checkpoint carries: what a future
// valid commit can still read. Non-terminal Work keeps its `work` predecessors (their
// effects feed result_refs); a terminal Work stays only as such a predecessor or while its
// backing Issue awaits the projector's terminal IssueComment. Claims, Dispatches, creators
// and gate Observations follow the retained Work. Requests keep the dedupe window: the most
// recent RetainedRequests of the log being folded, never the previous checkpoint's inherited ones.
func checkpointProjection(state Projection) Projection {
	folded := NewProjection()
	folded.Repository, folded.Tip, folded.Commits = state.Repository, state.Tip, state.Commits
	for id, work := range state.Works {
		if !terminalWork(work) {
			folded.Works[id] = work
			for _, edge := range work.DependsOn {
				if edge.Kind == "work" {
					folded.Works[edge.WorkID] = state.Works[edge.WorkID]
				}
			}
		} else if work.BackingIssue != nil && work.IssueComment == "" {
			folded.Works[id] = work
		}
	}
	for id, work := range folded.Works {
		folded.WorkCreators[id] = state.WorkCreators[id]
		for _, edge := range work.DependsOn {
			if edge.Kind == "work" {
				continue
			}
			key := GateKey(*edge.Resource, edge.Condition)
			if observation, ok := state.Observations[key]; ok {
				folded.Observations[key] = observation
			}
		}
	}
	for id, claim := range state.Claims {
		if _, retained := folded.Works[claim.WorkID]; retained {
			folded.Claims[id] = claim
		}
	}
	for id, dispatch := range state.Dispatches {
		if _, retained := folded.Claims[dispatch.Claims[0]]; retained {
			folded.Dispatches[id] = dispatch
		}
	}
	for _, commit := range state.Transactions[max(0, len(state.Transactions)-RetainedRequests):] {
		folded.Requests[commit.Request.ID] = struct{}{}
	}
	return folded
}

func sortedKeys(record object) []string {
	return slices.Sorted(maps.Keys(record))
}

func checkpointInvalid(message string) error {
	return queueError("checkpoint_invalid", "%s", message)
}

// checkpointSnapshot is the validated top level of a checkpoint `state` object.
type checkpointSnapshot struct {
	commits  int64
	requests []string
	sections map[string]object
}

// soleOperation returns the only operation of a commit; ok is false unless there is exactly one.
func soleOperation(operations []operationRecord) (operationRecord, bool) {
	if len(operations) > 0 {
		return operations[0], len(operations) == 1
	}
	return operationRecord{}, false
}

// restoreCheckpointValue validates a root Checkpoint commit and restores its projection.
func restoreCheckpointValue(value any) (Projection, error) {
	record, err := validateCommitRecord(value)
	if err != nil {
		return Projection{}, err
	}
	commit := record.commit
	operation, sole := soleOperation(record.operations)
	if commit.Previous != nil || commit.Request.Kind != "checkpoint" || !sole {
		return Projection{}, checkpointInvalid("a checkpoint must be the sole operation of a root commit")
	}
	snapshot, err := readCheckpointSnapshot(commit, operation.value)
	if err != nil {
		return Projection{}, err
	}
	state := NewProjection()
	state.Repository = commit.Actor.Repository
	if err := restoreWorks(&state, snapshot.sections["works"], snapshot.commits); err != nil {
		return Projection{}, err
	}
	dispatches := snapshot.sections["dispatches"]
	if err := restoreClaims(&state, snapshot.sections["claims"], dispatches); err != nil {
		return Projection{}, err
	}
	if err := restoreDispatches(&state, dispatches); err != nil {
		return Projection{}, err
	}
	if err := restoreObservations(&state, snapshot.sections["observations"]); err != nil {
		return Projection{}, err
	}
	if err := restoreWorkCreators(&state, snapshot.sections["work_creators"]); err != nil {
		return Projection{}, err
	}
	for _, id := range snapshot.requests {
		state.Requests[id] = struct{}{}
	}
	state.Requests[commit.Request.ID] = struct{}{}
	state.Tip = commit.ID
	state.Commits = int(snapshot.commits) + 1
	state.Transactions = []QueueCommit{commit}
	return state, nil
}

// readCheckpointSnapshot validates the top level of a Checkpoint operation's state
// against its commit: repository, prior tip, commit ordinal, sections and requests.
func readCheckpointSnapshot(commit QueueCommit, operation object) (checkpointSnapshot, error) {
	snapshot, err := closed(operation["state"], checkpointFields, nil, "checkpoint state", "checkpoint_invalid")
	if err != nil {
		return checkpointSnapshot{}, err
	}
	parameters, err := decodeParameters[CheckpointParameters](commit.Request)
	if err != nil {
		return checkpointSnapshot{}, checkpointInvalid("checkpoint parameters are malformed")
	}
	if snapshot["repository"] != commit.Actor.Repository || snapshot["tip"] != operation["prior_tip"] || parameters.PriorTip != operation["prior_tip"] {
		return checkpointSnapshot{}, checkpointInvalid("checkpoint does not bind its repository and prior tip")
	}
	commitsValue, isNumber := snapshot["commits"].(json.Number)
	commits, parseErr := commitsValue.Int64()
	if !isNumber || parseErr != nil || commits < 1 {
		return checkpointSnapshot{}, checkpointInvalid("checkpoint requires its commit ordinal")
	}
	sections := make(map[string]object, 5)
	for _, field := range []string{"works", "claims", "dispatches", "observations", "work_creators"} {
		section, ok := snapshot[field].(map[string]any)
		if !ok {
			return checkpointSnapshot{}, checkpointInvalid("checkpoint " + field + " must be an object")
		}
		sections[field] = section
	}
	requests, err := checkpointRequests(snapshot["requests"])
	if err != nil {
		return checkpointSnapshot{}, err
	}
	return checkpointSnapshot{commits: commits, requests: requests, sections: sections}, nil
}

// checkpointRequests validates the unique committed request identities of a checkpoint.
func checkpointRequests(value any) ([]string, error) {
	requests, isArray := value.([]any)
	if !isArray {
		return nil, checkpointInvalid("checkpoint requests must be unique")
	}
	seen := map[string]struct{}{}
	for _, id := range requests {
		key, err := canonicalGeneric(id)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[string(key)]; duplicate {
			return nil, checkpointInvalid("checkpoint requests must be unique")
		}
		seen[string(key)] = struct{}{}
	}
	if len(requests) > RetainedRequests {
		return nil, queueError("checkpoint_invalid", "checkpoint requests exceed the %d retained request window", RetainedRequests)
	}
	ids := make([]string, 0, len(requests))
	for _, id := range requests {
		text, err := identity(id, "checkpoint request")
		if err != nil {
			return nil, err
		}
		ids = append(ids, text)
	}
	return ids, nil
}

func restoreObservations(state *Projection, observations object) error {
	for _, key := range sortedKeys(observations) {
		observation, err := validateOperationValue(observations[key])
		if err != nil {
			return err
		}
		var typed ObservationOperation
		if observation.kind != "Observation" || decodeInto(observation.value, &typed) != nil || GateKey(typed.Resource, typed.Condition) != key {
			return checkpointInvalid("checkpoint Observation key is inconsistent")
		}
		state.Observations[key] = typed
	}
	return nil
}

func restoreWorkCreators(state *Projection, creators object) error {
	for _, id := range sortedKeys(creators) {
		if _, ok := state.Works[id]; !ok {
			return checkpointInvalid("checkpoint Work creator names missing Work")
		}
		actor, _, err := validateActorValue(creators[id])
		if err != nil {
			return err
		}
		var typed Actor
		if err := decodeInto(actor, &typed); err != nil {
			return checkpointInvalid("checkpoint Work creator is invalid")
		}
		state.WorkCreators[id] = typed
	}
	if len(state.WorkCreators) != len(state.Works) {
		return checkpointInvalid("every checkpoint Work requires its creator")
	}
	return nil
}

func restoreWorks(state *Projection, works object, commits int64) error {
	for _, id := range sortedKeys(works) {
		record, err := closed(works[id], workRecordRequired, workRecordOptional, "checkpoint Work", "checkpoint_invalid")
		if err != nil {
			return err
		}
		if err := validateRecordFields(record, []string{"claim_id", "completion_id", "cancellation_commit_id", "issue_comment"}, WorkCancellationReasons, "Work"); err != nil {
			return err
		}
		definition := object{}
		for _, field := range append(slices.Clone(workRequired), workOptional...) {
			if has(record, field) {
				definition[field] = record[field]
			}
		}
		if _, err := validateWorkValue(definition); err != nil {
			return err
		}
		position, err := closed(record["position"], []string{"commit", "operation"}, nil, "checkpoint Work position", "checkpoint_invalid")
		if err != nil {
			return err
		}
		recordState, isString := record["state"].(string)
		attempts, isNumber := record["attempts"].(json.Number)
		attemptCount, parseErr := attempts.Int64()
		if record["work_id"] != id || !isString || !slices.Contains(workStates, recordState) || !isNumber || parseErr != nil || attemptCount < 0 {
			return checkpointInvalid("checkpoint Work record is inconsistent")
		}
		if _, err := integer(position["commit"], 0, commits-1, "Work position commit"); err != nil {
			return err
		}
		if _, err := integer(position["operation"], 0, OperationsPerCommit-1, "Work position operation"); err != nil {
			return err
		}
		if (recordState == "claimed") != has(record, "claim_id") && recordState != "completed" {
			return checkpointInvalid("only claimed or completed Work names a Claim")
		}
		if (recordState == "completed") != has(record, "completion_id") || (recordState == "completed") != has(record, "effects") {
			return checkpointInvalid("completed Work carries its completion")
		}
		if (recordState == "cancelled") != has(record, "cancellation_reason") {
			return checkpointInvalid("cancelled Work carries its reason")
		}
		var work WorkState
		if err := decodeInto(record, &work); err != nil {
			return checkpointInvalid("checkpoint Work record is malformed")
		}
		state.Works[id] = &work
	}
	// A retained non-terminal Work still reads its predecessors; a terminal one never will.
	for _, work := range state.Works {
		if terminalWork(work) {
			continue
		}
		for _, edge := range work.DependsOn {
			if _, present := state.Works[edge.WorkID]; edge.Kind == "work" && !present {
				return checkpointInvalid("a checkpoint keeps every predecessor of non-terminal Work")
			}
		}
	}
	return nil
}

func restoreClaims(state *Projection, claims, dispatches object) error {
	for _, id := range sortedKeys(claims) {
		record, err := closed(claims[id], claimRecordRequired, claimRecordOptional, "checkpoint Claim", "checkpoint_invalid")
		if err != nil {
			return err
		}
		if err := validateRecordFields(record, []string{"handle", "commit_id", "request_id", "terminal_commit_id", "issue_comment"}, ClaimCancellationReasons, "Claim"); err != nil {
			return err
		}
		workID, workNamed := record["work_id"].(string)
		dispatchID, dispatchNamed := record["dispatch_id"].(string)
		recordState, stateNamed := record["state"].(string)
		work := state.Works[workID]
		if record["claim_id"] != id || !workNamed || !dispatchNamed || !stateNamed || !slices.Contains(claimStates, recordState) || work == nil || !plainObject(dispatches[dispatchID]) {
			return checkpointInvalid("checkpoint Claim record is inconsistent")
		}
		if (recordState == "open") != (work.State == "claimed" && work.ClaimID == id) {
			return checkpointInvalid("open Claims and claimed Work must agree")
		}
		var claim ClaimState
		if err := decodeInto(record, &claim); err != nil {
			return checkpointInvalid("checkpoint Claim record is malformed")
		}
		state.Claims[id] = &claim
	}
	return nil
}

// restoreDispatches restores Dispatches and checks, on the raw records, that every
// Dispatch carries exactly one Claim linking back to it, so two Claims can never share one.
func restoreDispatches(state *Projection, dispatches object) error {
	for _, id := range sortedKeys(dispatches) {
		record, err := closed(dispatches[id], dispatchFields, nil, "checkpoint Dispatch", "checkpoint_invalid")
		if err != nil {
			return err
		}
		if err := validateRecordFields(record, []string{"request_id", "commit_id", "pool", "worker_profile"}, nil, "Dispatch"); err != nil {
			return err
		}
		recordState, stateNamed := record["state"].(string)
		members, isArray := record["claims"].([]any)
		workflow, workflowNamed := record["workflow"].(string)
		if record["dispatch_id"] != id || !stateNamed || !slices.Contains(dispatchStates, recordState) || !isArray || !workflowNamed || !profileWorkflowPattern.MatchString(workflow) {
			return checkpointInvalid("checkpoint Dispatch record is inconsistent")
		}
		if len(members) != 1 {
			return checkpointInvalid("a checkpoint Dispatch carries exactly one Claim")
		}
		name, isName := members[0].(string) //nolint:uncheckedsliceindex // len(members) == 1 is checked above
		claim := state.Claims[name]
		if !isName || claim == nil || claim.DispatchID != id {
			return checkpointInvalid("checkpoint Dispatch members are inconsistent")
		}
		if work := state.Works[claim.WorkID]; record["pool"] != work.Pool || record["worker_profile"] != work.WorkerProfile {
			return checkpointInvalid("a checkpoint Dispatch must match its Work's pool and profile")
		}
		if err := validateDispatchExecution(record, recordState); err != nil {
			return err
		}
		if (recordState != "closed") != (claim.State == "open") {
			return checkpointInvalid("a checkpoint Dispatch is closed exactly when its Claim is settled")
		}
		var dispatch DispatchState
		if err := decodeInto(record, &dispatch); err != nil {
			return checkpointInvalid("checkpoint Dispatch record is malformed")
		}
		state.Dispatches[id] = &dispatch
		state.dispatchOrder = append(state.dispatchOrder, id)
	}
	for _, claim := range state.Claims {
		if dispatch := state.Dispatches[claim.DispatchID]; dispatch == nil || dispatch.Claims[0] != claim.ClaimID {
			return checkpointInvalid("checkpoint Claims must each own their Dispatch")
		}
	}
	return nil
}

// validateDispatchExecution checks a checkpoint Dispatch's bound run and recorded launch:
// only bound Dispatches carry a run, only started or bound ones carry their launch, and a
// bound run is the launched run.
func validateDispatchExecution(record object, recordState string) error {
	var runID, launchRunID any
	if record["run"] != nil {
		run, err := closed(record["run"], []string{"run_id", "run_attempt"}, nil, "checkpoint run", "checkpoint_invalid")
		if err != nil {
			return err
		}
		if _, err := decimal(run["run_id"], "checkpoint run ID", "checkpoint_invalid"); err != nil {
			return err
		}
		if _, err := integer(run["run_attempt"], 1, 4096, "checkpoint run attempt"); err != nil {
			return err
		}
		runID = run["run_id"]
	}
	if (recordState == "bound") != (record["run"] != nil) && recordState != "closed" {
		return checkpointInvalid("only bound Dispatches carry a run")
	}
	if record["launch"] != nil {
		launch, err := closed(record["launch"], []string{"run_id", "revision"}, nil, "checkpoint launch", "checkpoint_invalid")
		if err != nil {
			return err
		}
		if _, err := decimal(launch["run_id"], "checkpoint launched run ID", "checkpoint_invalid"); err != nil {
			return err
		}
		if revision, named := launch["revision"].(string); !named || !revisionPattern.MatchString(revision) {
			return checkpointInvalid("checkpoint launch revision is malformed")
		}
		launchRunID = launch["run_id"]
	}
	if (recordState == "reserved") != (record["launch"] == nil) && recordState != "closed" {
		return checkpointInvalid("only started or bound Dispatches carry their launch")
	}
	if runID != nil && launchRunID != runID {
		return checkpointInvalid("a bound run must be the launched run")
	}
	return nil
}

// validateRecordFields checks identity fields, the cancellation reason and recorded
// effects of one checkpoint record.
func validateRecordFields(record object, identities, reasons []string, name string) error {
	for _, field := range identities {
		if has(record, field) {
			if _, err := identity(record[field], field); err != nil {
				return err
			}
		}
	}
	if has(record, "cancellation_reason") {
		if _, err := oneOf(record["cancellation_reason"], reasons, name); err != nil {
			return err
		}
	}
	if has(record, "effects") {
		effects, isArray := record["effects"].([]any)
		if !isArray || slices.ContainsFunc(effects, func(effect any) bool { return !plainObject(effect) }) {
			return checkpointInvalid("checkpoint effects must be an array of objects")
		}
	}
	return nil
}

// CheckpointCommit builds the root commit that replaces the whole log with the projection.
func CheckpointCommit(state Projection, actor Actor, at int64) (QueueCommit, error) {
	if state.Tip == "" {
		return QueueCommit{}, checkpointInvalid("an empty queue has nothing to checkpoint")
	}
	request, err := NewRequest("checkpoint", actor, CheckpointParameters{PriorTip: state.Tip})
	if err != nil {
		return QueueCommit{}, err
	}
	snapshot, err := SerializeProjection(checkpointProjection(state))
	if err != nil {
		return QueueCommit{}, err
	}
	operation, err := Op(CheckpointOperation{Kind: "Checkpoint", PriorTip: state.Tip, State: snapshot})
	if err != nil {
		return QueueCommit{}, err
	}
	commit := QueueCommit{Version: Version, ID: CommitID(nil, request.ID), Request: request, Actor: actor, At: at, Operations: []Operation{operation}}
	value, err := genericValue(commit)
	if err != nil {
		return QueueCommit{}, err
	}
	if _, err := restoreCheckpointValue(value); err != nil {
		return QueueCommit{}, err
	}
	return commit, nil
}
