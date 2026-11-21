package workqueue

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var replayLog = logger.New("workqueue:replay")

type operationFields struct {
	required []string
	optional []string
}

var opFields = map[string]operationFields{
	"Work":              {workRequired, workOptional},
	"Observation":       {[]string{"kind", "observation_id", "resource", "condition", "state"}, []string{"state_reason", "merged", "merge_commit"}},
	"Claim":             {[]string{"kind", "work_id", "claim_id", "dispatch_id", "handle"}, nil},
	"Dispatch":          {[]string{"kind", "dispatch_id", "state"}, nil},
	"Bound":             {[]string{"kind", "dispatch_id", "run_id", "run_attempt"}, nil},
	"Completion":        {[]string{"kind", "work_id", "claim_id", "dispatch_id", "claim_handle", "effects"}, nil},
	"ClaimCancellation": {[]string{"kind", "work_id", "claim_id", "reason"}, nil},
	"WorkCancellation":  {[]string{"kind", "work_id", "reason"}, []string{"claim_id"}},
	"Release":           {[]string{"kind", "dispatch_id", "reason"}, nil},
	"Checkpoint":        {[]string{"kind", "prior_tip", "state"}, nil},
	"IssueLink":         {[]string{"kind", "work_id", "resource"}, []string{"claim_id"}},
	"IssueComment":      {[]string{"kind", "work_id", "comment_id"}, []string{"claim_id"}},
}

var operationIdentityFields = []string{"work_id", "claim_id", "dispatch_id", "handle", "claim_handle", "observation_id", "comment_id", "prior_tip"}

func validateObservationValue(operation object) error {
	resource, err := validateResourceValue(operation["resource"])
	if err != nil {
		return err
	}
	kind := resource.Kind
	condition, isCondition := operation["condition"].(string)
	state, isState := operation["state"].(string)
	if !isCondition || !isState || !slices.Contains(conditionsFor(kind), condition) || !slices.Contains(observationState, state) {
		return queueError("observation_invalid", "typed condition/state is invalid")
	}
	if has(operation, "state_reason") {
		if reason, ok := operation["state_reason"].(string); !ok || !slices.Contains([]string{"completed", "not_planned", "reopened"}, reason) {
			return queueError("observation_invalid", "invalid Issue state_reason")
		}
	}
	if has(operation, "merged") {
		if _, ok := operation["merged"].(bool); !ok {
			return queueError("observation_invalid", "merged must be boolean")
		}
	}
	if has(operation, "merge_commit") {
		if _, err := identity(operation["merge_commit"], "merge commit"); err != nil {
			return err
		}
	}
	if kind == "issue" && (has(operation, "merged") || has(operation, "merge_commit")) {
		return queueError("observation_invalid", "an Issue cannot carry pull request merge evidence")
	}
	if state == "ready" {
		if kind == "pull_request" && (operation["merged"] != true || operation["merge_commit"] == nil || operation["merge_commit"] == "") {
			return queueError("observation_invalid", "a ready pull request gate requires its merge commit")
		}
		if condition == "completed" && operation["state_reason"] != "completed" {
			return queueError("observation_invalid", "a ready completed Issue gate requires state_reason completed")
		}
	}
	return nil
}

// operationRecord is one validated generic operation: its kind, its Work identity
// (when present) and, once part of a commit, its canonical bytes.
type operationRecord struct {
	value  object
	kind   string
	workID string
	data   Operation
}

// validateOperationValue is the structural validation of one closed v4 operation.
func validateOperationValue(value any) (operationRecord, error) {
	operation, ok := value.(map[string]any)
	kind, isString := operation["kind"].(string)
	fields, known := opFields[kind]
	if !ok || !isString || !known {
		return operationRecord{}, queueError("request_invalid", "unknown queue operation")
	}
	if _, err := closed(operation, fields.required, fields.optional, kind, "request_invalid"); err != nil {
		return operationRecord{}, err
	}
	record := operationRecord{value: operation, kind: kind}
	for _, field := range operationIdentityFields {
		if !has(operation, field) {
			continue
		}
		text, err := identity(operation[field], field)
		if err != nil {
			return operationRecord{}, err
		}
		if field == "work_id" {
			record.workID = text
		}
	}
	return record, validateOperationFields(kind, operation)
}

// validateOperationFields applies the per-kind field rules of one operation.
func validateOperationFields(kind string, operation object) error {
	var err error
	switch kind {
	case "Work":
		_, err = validateWorkValue(operation)
	case "Observation":
		err = validateObservationValue(operation)
	case "Dispatch":
		if operation["state"] != "started" {
			err = queueError("request_invalid", "a Dispatch operation records only the started launch")
		}
	case "Bound":
		if _, err = decimal(operation["run_id"], "bound run ID", "request_invalid"); err == nil {
			_, err = integer(operation["run_attempt"], 1, 4096, "bound run attempt")
		}
	case "Completion":
		effects, isArray := operation["effects"].([]any)
		if !isArray || slices.ContainsFunc(effects, func(effect any) bool { return !plainObject(effect) }) {
			return queueError("request_invalid", "Completion effects must be an array of objects")
		}
		err = boundedBytes(effects, EffectsBytes, "Completion effects")
	case "ClaimCancellation":
		_, err = oneOf(operation["reason"], ClaimCancellationReasons, "ClaimCancellation")
	case "WorkCancellation":
		_, err = oneOf(operation["reason"], WorkCancellationReasons, "WorkCancellation")
	case "Release":
		_, err = oneOf(operation["reason"], ReleaseReasons, "Release")
	case "Checkpoint":
		if !plainObject(operation["state"]) {
			err = queueError("checkpoint_invalid", "checkpoint requires a projection state object")
		}
	case "IssueLink":
		var resource Resource
		if resource, err = validateResourceValue(operation["resource"]); err == nil && resource.Kind != "issue" {
			err = queueError("resource_invalid", "IssueLink requires an Issue")
		}
	}
	return err
}

func authorizeOperation(role, requestKind string, operation operationRecord) error {
	kind := operation.kind
	if !slices.Contains(requestKinds[requestKind].operations, kind) {
		return queueError("request_invalid", "%s cannot carry %s", requestKind, kind)
	}
	unauthorized := queueError("unauthorized_operation", "%s cannot publish %s through %s", role, kind, requestKind)
	if !slices.Contains(operationRoles[kind], role) {
		return unauthorized
	}
	if kind == "WorkCancellation" && (role == "worker" && requestKind != "finish" || role == "dispatcher" && requestKind != "launch") {
		return unauthorized
	}
	if kind == "Release" && role == "dispatcher" && operation.value["reason"] != "launch_failed" {
		return unauthorized
	}
	if requestKind == "migrate" && role != "administrator" {
		return unauthorized
	}
	return nil
}

// commitRecord is one validated commit with its validated operations in order.
type commitRecord struct {
	commit     QueueCommit
	operations []operationRecord
}

// validateCommitRecord validates one generic commit record and returns its typed form.
func validateCommitRecord(value any) (commitRecord, error) {
	record, ok := value.(map[string]any)
	if !ok {
		return commitRecord{}, queueError("ledger_invalid", "QueueCommit must be an object")
	}
	id, previous, err := validateCommitHeader(record)
	if err != nil {
		return commitRecord{}, err
	}
	actor, role, err := validateActorValue(record["actor"])
	if err != nil {
		return commitRecord{}, err
	}
	request, err := validateRequestValue(record["request"], actor)
	if err != nil {
		return commitRecord{}, err
	}
	operations, err := validateCommitOperations(record["operations"], role, request.kind)
	if err != nil {
		return commitRecord{}, err
	}
	if request.kind == "issue_link" {
		if err := requireSameOperations(request.parameters["operations"], record["operations"]); err != nil {
			return commitRecord{}, err
		}
	}
	if id != CommitID(previous, request.id) {
		return commitRecord{}, queueError("ledger_invalid", "commit ID does not bind its predecessor and request")
	}
	var commit QueueCommit
	if err := decodeInto(record, &commit); err != nil {
		return commitRecord{}, queueError("ledger_invalid", "QueueCommit: %v", err)
	}
	commit.Operations = make([]Operation, 0, len(operations))
	for _, operation := range operations {
		commit.Operations = append(commit.Operations, operation.data)
	}
	return commitRecord{commit: commit, operations: operations}, nil
}

// validateCommitHeader checks the protocol version, the closed commit fields, the
// commit identity, its predecessor and its informational time.
func validateCommitHeader(record object) (string, *string, error) {
	if version, isNumber := record["version"].(json.Number); !isNumber || string(version) != "4" {
		return "", nil, queueError("unsupported_protocol", "work queue protocol v4 required; run the v3 converter")
	}
	if _, err := closed(record, []string{"version", "id", "previous", "request", "actor", "at", "operations"}, nil, "QueueCommit", "ledger_invalid"); err != nil {
		return "", nil, err
	}
	id, err := identity(record["id"], "commit ID")
	if err != nil {
		return "", nil, err
	}
	var previous *string
	if record["previous"] != nil {
		text, err := identity(record["previous"], "predecessor")
		if err != nil {
			return "", nil, err
		}
		previous = &text
	}
	if _, err := integer(record["at"], 0, maxSafeInteger, "commit time"); err != nil {
		return "", nil, err
	}
	return id, previous, nil
}

// validateCommitOperations validates, authorizes and canonicalizes every operation
// of a commit.
func validateCommitOperations(value any, role, requestKind string) ([]operationRecord, error) {
	operations, isArray := value.([]any)
	if !isArray || len(operations) < 1 || len(operations) > OperationsPerCommit {
		return nil, queueError("request_invalid", "QueueCommit requires 1..%d operations", OperationsPerCommit)
	}
	records := make([]operationRecord, 0, len(operations))
	for _, operation := range operations {
		record, err := validateOperationValue(operation)
		if err != nil {
			return nil, err
		}
		if err := authorizeOperation(role, requestKind, record); err != nil {
			return nil, err
		}
		if record.data, err = canonicalGeneric(record.value); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// requireSameOperations requires an issue_link commit to carry exactly the
// operations its request names.
func requireSameOperations(requested, committed any) error {
	left, err := canonicalGeneric(requested)
	if err != nil {
		return err
	}
	right, err := canonicalGeneric(committed)
	if err != nil {
		return err
	}
	if !bytes.Equal(left, right) {
		return queueError("request_invalid", "issue_link operations differ from its request")
	}
	return nil
}

// ValidateCommit validates one typed commit structurally.
func ValidateCommit(commit QueueCommit) error {
	value, err := genericValue(commit)
	if err != nil {
		return err
	}
	_, err = validateCommitRecord(value)
	return err
}

// NewProjection returns the empty queue projection.
func NewProjection() Projection {
	return Projection{
		Works:        map[string]*WorkState{},
		Claims:       map[string]*ClaimState{},
		Dispatches:   map[string]*DispatchState{},
		Observations: map[string]ObservationOperation{},
		Requests:     map[string]struct{}{},
		WorkCreators: map[string]Actor{},
		Transactions: []QueueCommit{},
	}
}

// applyCommitValue validates and applies one generic commit to state in place.
func (state *Projection) applyCommitValue(value any) error {
	record, err := validateCommitRecord(value)
	if err != nil {
		return err
	}
	commit := record.commit
	if slices.ContainsFunc(record.operations, func(operation operationRecord) bool { return operation.kind == "Checkpoint" }) {
		return queueError("checkpoint_invalid", "a Checkpoint must be the sole first line of the log")
	}
	if err := state.checkCommitLineage(commit); err != nil {
		return err
	}
	if err := state.admitFreshWork(record.operations); err != nil {
		return err
	}
	ordinal := state.Commits
	for index, operation := range record.operations {
		if err := state.applyOperation(operation.kind, operation.data, commit, ordinal, index); err != nil {
			return err
		}
	}
	state.Requests[commit.Request.ID] = struct{}{}
	state.Tip = commit.ID
	state.Commits++
	state.Transactions = append(state.Transactions, commit)
	return nil
}

// checkCommitLineage requires a commit to extend the causal tip from the queue
// repository with a fresh request, and migrate commits to form the genesis.
func (state *Projection) checkCommitLineage(commit QueueCommit) error {
	if (commit.Previous == nil) != (state.Tip == "") || commit.Previous != nil && *commit.Previous != state.Tip {
		return queueError("ledger_invalid", "commit does not extend the causal tip")
	}
	if state.Tip == "" {
		if commit.Actor.Role == "worker" {
			return queueError("actor_unauthorized", "a worker cannot create the queue genesis")
		}
		state.Repository = commit.Actor.Repository
	} else if commit.Actor.Repository != state.Repository {
		return queueError("actor_unauthorized", "foreign queue repository")
	}
	if _, reused := state.Requests[commit.Request.ID]; reused {
		return queueError("request_reused", "request is already committed")
	}
	if commit.Request.Kind == "migrate" && slices.ContainsFunc(state.Transactions, func(prior QueueCommit) bool { return prior.Request.Kind != "migrate" }) {
		return queueError("request_invalid", "migrate commits must form the log genesis")
	}
	return nil
}

// admitFreshWork validates graph admission of the Work a commit introduces.
func (state *Projection) admitFreshWork(operations []operationRecord) error {
	fresh := map[string][]any{}
	freshOrder := []string{}
	for _, operation := range operations {
		if operation.kind != "Work" {
			continue
		}
		if _, exists := state.Works[operation.workID]; exists {
			continue
		}
		if _, seen := fresh[operation.workID]; !seen {
			freshOrder = append(freshOrder, operation.workID)
		}
		fresh[operation.workID] = append(fresh[operation.workID], operation.value)
	}
	if len(freshOrder) == 0 {
		return nil
	}
	nodes := []any{}
	for _, id := range freshOrder {
		nodes = append(nodes, fresh[id]...)
	}
	return state.validateGraphAdmission(nodes)
}

func (state *Projection) projectStats() {
	stats := Stats{Work: len(state.Works), Claims: len(state.Claims), Transactions: state.Commits, Nodes: len(state.Works)}
	gates := map[string]struct{}{}
	for _, work := range state.Works {
		switch work.State {
		case "available":
			stats.Available++
		case "claimed":
			stats.Claimed++
		case "completed":
			stats.Completed++
		case "cancelled":
			stats.Cancelled++
		}
		for _, edge := range work.DependsOn {
			if edge.Kind != "work" {
				gates[work.GraphID+"\n"+GateKey(*edge.Resource, edge.Condition)] = struct{}{}
			}
		}
	}
	for _, dispatch := range state.Dispatches {
		if dispatch.State != "closed" {
			stats.Dispatches++
		}
	}
	stats.Nodes += len(gates)
	state.Stats = stats
}

func isCheckpointRecord(value any) bool {
	record, ok := value.(map[string]any)
	if !ok {
		return false
	}
	operations, ok := record["operations"].([]any)
	if ok && len(operations) > 0 {
		first, isObject := operations[0].(map[string]any)
		return isObject && first["kind"] == "Checkpoint"
	}
	return false
}

// replayValues replays generic commit records in file order. A Checkpoint may only be line 1.
func replayValues(records []any) (Projection, error) {
	state := NewProjection()
	for index, record := range records {
		if index == 0 && isCheckpointRecord(record) {
			restored, err := restoreCheckpointValue(record)
			if err != nil {
				return Projection{}, err
			}
			state = restored
			continue
		}
		if err := state.applyCommitValue(record); err != nil {
			return Projection{}, err
		}
	}
	state.projectStats()
	return state, nil
}

// ReplayRaw replays JSON commit records (one per element) in order.
func ReplayRaw(records []json.RawMessage) (Projection, error) {
	values := make([]any, 0, len(records))
	for _, record := range records {
		value, err := decodeStrict(record)
		if err != nil {
			return Projection{}, err
		}
		values = append(values, value)
	}
	return replayValues(values)
}

// Replay replays typed commits in order.
func Replay(commits []QueueCommit) (Projection, error) {
	values := make([]any, 0, len(commits))
	for _, commit := range commits {
		value, err := genericValue(commit)
		if err != nil {
			return Projection{}, err
		}
		values = append(values, value)
	}
	return replayValues(values)
}

func parseRecords(data []byte) ([]any, error) {
	if len(data) > LedgerBytes {
		return nil, queueError("resource_limit", "ledger exceeds its byte bound")
	}
	if len(data) == 0 || !bytes.HasSuffix(data, []byte{'\n'}) {
		return nil, queueError("ledger_invalid", "queue log must be nonempty and newline terminated")
	}
	records := []any{}
	for number, line := range bytes.Split(data[:len(data)-1], []byte{'\n'}) {
		if strings.TrimSpace(string(line)) == "" {
			return nil, queueError("ledger_invalid", "empty or truncated log record at line %d", number+1)
		}
		value, err := decodeStrict(line)
		if err != nil {
			if ErrorCode(err) == "" {
				return nil, queueError("codec_invalid", "line %d: %v", number+1, err)
			}
			return nil, err
		}
		records = append(records, value)
	}
	return records, nil
}

// ReplayLog parses and replays the bytes of work-queue.jsonl.
func ReplayLog(data []byte) (Projection, error) {
	records, err := parseRecords(data)
	if err != nil {
		return Projection{}, err
	}
	state, err := replayValues(records)
	if err != nil {
		return Projection{}, err
	}
	replayLog.Printf("replayed %d commits, %d works", len(state.Transactions), len(state.Works))
	return state, nil
}

// Parse parses and validates a ledger, returning its commits in file order.
func Parse(data []byte) ([]QueueCommit, error) {
	state, err := ReplayLog(data)
	if err != nil {
		return nil, err
	}
	return state.Transactions, nil
}

// Serialize replays commits and returns their canonical newline-terminated log.
func Serialize(commits []QueueCommit) ([]byte, error) {
	state, err := Replay(commits)
	if err != nil {
		return nil, err
	}
	return serializeTransactions(state.Transactions)
}

func serializeTransactions(commits []QueueCommit) ([]byte, error) {
	var buffer bytes.Buffer
	for _, commit := range commits {
		data, err := CanonicalValue(commit)
		if err != nil {
			return nil, err
		}
		buffer.Write(data)
		buffer.WriteByte('\n')
	}
	return buffer.Bytes(), nil
}

// Clone copies a projection for a speculative append.
func (state Projection) Clone() Projection {
	copied := NewProjection()
	copied.Repository, copied.Tip, copied.Commits, copied.Stats = state.Repository, state.Tip, state.Commits, state.Stats
	for id, work := range state.Works {
		value := *work
		copied.Works[id] = &value
	}
	for id, claim := range state.Claims {
		value := *claim
		copied.Claims[id] = &value
	}
	for id, dispatch := range state.Dispatches {
		value := *dispatch
		value.Claims = slices.Clone(dispatch.Claims)
		if dispatch.Run != nil {
			run := *dispatch.Run
			value.Run = &run
		}
		copied.Dispatches[id] = &value
	}
	maps.Copy(copied.Observations, state.Observations)
	for id := range state.Requests {
		copied.Requests[id] = struct{}{}
	}
	maps.Copy(copied.WorkCreators, state.WorkCreators)
	copied.Transactions = slices.Clone(state.Transactions)
	copied.dispatchOrder = slices.Clone(state.dispatchOrder)
	return copied
}

// AppendCommit validates commit against a copy of state and returns the new projection.
func AppendCommit(state Projection, commit QueueCommit) (Projection, error) {
	next := state.Clone()
	value, err := genericValue(commit)
	if err != nil {
		return Projection{}, err
	}
	if err := next.applyCommitValue(value); err != nil {
		return Projection{}, err
	}
	next.projectStats()
	return next, nil
}

// CommittedRequest returns the commit that carried requestID; ok reports
// whether the request is committed (commit is nil when folded into a checkpoint).
func (state Projection) CommittedRequest(requestID string) (*QueueCommit, bool) {
	if _, ok := state.Requests[requestID]; !ok {
		return nil, false
	}
	for index := range state.Transactions {
		if state.Transactions[index].Request.ID == requestID {
			return &state.Transactions[index], true
		}
	}
	return nil, true
}

// OpenClaims returns the open Claims of a Dispatch in assignment order.
func (state Projection) OpenClaims(dispatch *DispatchState) []*ClaimState {
	claims := []*ClaimState{}
	for _, id := range dispatch.Claims {
		if claim := state.Claims[id]; claim != nil && claim.State == "open" {
			claims = append(claims, claim)
		}
	}
	return claims
}

// DispatchOrder returns Dispatch identities in creation order.
func (state Projection) DispatchOrder() []string {
	return slices.Clone(state.dispatchOrder)
}
