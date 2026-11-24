package workqueue

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
)

// Roles are closed; replay checks only repository identity and the per-operation role table.
var Roles = []string{"administrator", "producer", "dispatcher", "worker", "reconciler", "projector", "observer"}

var (
	repositoryPattern      = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)
	actorWorkflowPattern   = regexp.MustCompile(`^\.github/workflows/[^/]+\.ya?ml$`)
	profileWorkflowPattern = regexp.MustCompile(`^\.github/workflows/[^/]+\.lock\.yml$`)
)

// Closed cancellation and release reasons.
var (
	ClaimCancellationReasons = []string{"worker_failed", "worker_terminated", "launch_failed", "run_terminal", "run_missing", "operator"}
	WorkCancellationReasons  = []string{"attempts_exhausted", "operator", "related_issue_closed", "worker_contract_obsolete"}
	ReleaseReasons           = []string{"launch_failed", "worker_terminated", "run_terminal", "run_missing", "operator"}
)

type requestKind struct {
	operations []string
	parameters []string
}

var requestKinds = map[string]requestKind{
	"submit":        {[]string{"Work"}, []string{"work_ids"}},
	"dispatch_next": {[]string{"Observation", "Claim"}, []string{"pool", "max_claims", "max_dispatches", "concurrency", "per_account_limit", "profiles"}},
	"launch":        {[]string{"Dispatch", "Release", "WorkCancellation"}, []string{"dispatch_id"}},
	"bind":          {[]string{"Bound"}, []string{"dispatch_id"}},
	// A finish may admit one continuation Work right after each paired Completion.
	"finish":      {[]string{"Completion", "ClaimCancellation", "WorkCancellation", "Work"}, []string{"dispatch_id"}},
	"release":     {[]string{"Release", "WorkCancellation"}, []string{"dispatch_id", "reason"}},
	"cancel_work": {[]string{"WorkCancellation"}, []string{"work_ids", "reason"}},
	"checkpoint":  {[]string{"Checkpoint"}, []string{"prior_tip"}},
	"issue_link":  {[]string{"IssueLink", "IssueComment"}, []string{"operations"}},
	"migrate":     {[]string{"Work"}, []string{"work_ids"}},
}

var operationRoles = map[string][]string{
	"Work":              {"producer", "dispatcher", "administrator", "worker"},
	"Observation":       {"dispatcher", "administrator"},
	"Claim":             {"dispatcher", "administrator"},
	"Dispatch":          {"dispatcher", "administrator"},
	"Bound":             {"worker"},
	"Completion":        {"worker"},
	"ClaimCancellation": {"worker"},
	"WorkCancellation":  {"producer", "administrator", "reconciler", "worker", "dispatcher"},
	"Release":           {"dispatcher", "reconciler", "administrator"},
	"Checkpoint":        {"administrator", "reconciler"},
	"IssueLink":         {"projector"},
	"IssueComment":      {"projector"},
}

// validateActorValue validates a generic actor and returns it with its role.
func validateActorValue(value any) (object, string, error) {
	actor, err := closed(value, []string{"role", "repository"}, []string{"workflow", "run_id", "run_attempt"}, "actor", "request_invalid")
	if err != nil {
		return nil, "", err
	}
	role, ok := actor["role"].(string)
	if !ok || !slices.Contains(Roles, role) {
		return nil, "", queueError("request_invalid", "unsupported actor role")
	}
	if err := validateActorCoordinates(actor); err != nil {
		return nil, "", err
	}
	if role == "worker" && (!has(actor, "workflow") || !has(actor, "run_id")) {
		return nil, "", queueError("request_invalid", "worker actor requires its workflow and run")
	}
	return actor, role, nil
}

// validateActorCoordinates checks the actor repository, workflow path and run.
func validateActorCoordinates(actor object) error {
	repository, err := identity(actor["repository"], "actor repository")
	if err != nil {
		return err
	}
	if !repositoryPattern.MatchString(repository) {
		return queueError("request_invalid", "actor repository must be owner/name")
	}
	if has(actor, "workflow") {
		workflow, err := identity(actor["workflow"], "actor workflow")
		if err != nil {
			return err
		}
		if !actorWorkflowPattern.MatchString(workflow) {
			return queueError("request_invalid", "actor workflow must be a repository workflow path")
		}
	}
	if has(actor, "run_id") != has(actor, "run_attempt") {
		return queueError("request_invalid", "actor run requires run_id and run_attempt")
	}
	if has(actor, "run_id") {
		if _, err := decimal(actor["run_id"], "actor run ID", "request_invalid"); err != nil {
			return err
		}
		if _, err := integer(actor["run_attempt"], 1, 4096, "actor run attempt"); err != nil {
			return err
		}
	}
	return nil
}

// ValidateActor checks a typed actor against the closed v4 actor rules.
func ValidateActor(actor Actor) error {
	value, err := genericValue(actor)
	if err != nil {
		return err
	}
	_, _, err = validateActorValue(value)
	return err
}

func validateWorkIDs(value any) error {
	ids, ok := value.([]any)
	if !ok || len(ids) < 1 || len(ids) > OperationsPerCommit {
		return queueError("request_invalid", "work_ids requires 1..256 unique Work identities")
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, composite := id.(map[string]any); composite {
			continue
		}
		if _, composite := id.([]any); composite {
			continue
		}
		key, err := canonicalGeneric(id)
		if err != nil {
			return err
		}
		if _, duplicate := seen[string(key)]; duplicate {
			return queueError("request_invalid", "work_ids requires 1..256 unique Work identities")
		}
		seen[string(key)] = struct{}{}
	}
	for _, id := range ids {
		if _, err := identity(id, "Work ID"); err != nil {
			return err
		}
	}
	return nil
}

func validateParameters(kind string, value any) (object, error) {
	parameters, err := closed(value, requestKinds[kind].parameters, nil, kind+" parameters", "request_invalid")
	if err != nil {
		return nil, err
	}
	return parameters, validateParameterValues(kind, parameters)
}

func validateParameterValues(kind string, parameters object) error {
	var err error
	switch kind {
	case "submit", "migrate":
		return validateWorkIDs(parameters["work_ids"])
	case "dispatch_next":
		return validateDispatchNextParameters(parameters, true)
	case "launch", "bind", "finish":
		_, err = identity(parameters["dispatch_id"], "dispatch ID")
	case "release":
		if _, err = identity(parameters["dispatch_id"], "dispatch ID"); err == nil {
			_, err = oneOf(parameters["reason"], ReleaseReasons, "release")
		}
	case "cancel_work":
		if err = validateWorkIDs(parameters["work_ids"]); err == nil {
			_, err = oneOf(parameters["reason"], WorkCancellationReasons, "cancel_work")
		}
	case "checkpoint":
		_, err = identity(parameters["prior_tip"], "prior tip")
	case "issue_link":
		operations, ok := parameters["operations"].([]any)
		if !ok || len(operations) < 1 || len(operations) > OperationsPerCommit {
			return queueError("request_invalid", "issue_link requires 1..256 operations")
		}
	}
	return err
}

// validateDispatchNextParameters checks the scalar limits; strict also checks
// every routed profile (request validation, not the planner).
func validateDispatchNextParameters(parameters object, strict bool) error {
	if _, err := identity(parameters["pool"], "pool"); err != nil {
		return err
	}
	for _, bound := range []struct {
		field    string
		min, max int64
	}{{"max_claims", 1, OperationsPerCommit}, {"max_dispatches", 1, OperationsPerCommit}, {"concurrency", 1, 4096}, {"per_account_limit", 0, 4096}} {
		if _, err := integer(parameters[bound.field], bound.min, bound.max, bound.field); err != nil {
			return err
		}
	}
	profiles, ok := parameters["profiles"].(map[string]any)
	if !strict {
		if !ok {
			return queueError("request_invalid", "dispatch_next requires worker profiles")
		}
		return nil
	}
	if !ok || len(profiles) == 0 || len(profiles) > 256 {
		return queueError("request_invalid", "dispatch_next requires 1..256 worker profiles")
	}
	names := slices.Sorted(maps.Keys(profiles))
	for _, name := range names {
		if _, err := identity(name, "worker profile"); err != nil {
			return err
		}
		profile, err := closed(profiles[name], []string{"workflow"}, nil, "profile "+name, "request_invalid")
		if err != nil {
			return err
		}
		workflow, ok := profile["workflow"].(string)
		if !ok || !profileWorkflowPattern.MatchString(workflow) {
			return queueError("request_invalid", "profile workflow must be .github/workflows/<name>.lock.yml")
		}
	}
	return nil
}

func requestIDValue(actor, kind, parameters any) (string, error) {
	data, err := canonicalGeneric(map[string]any{"actor": actor, "kind": kind, "parameters": parameters})
	if err != nil {
		return "", err
	}
	return "request:" + hashBytes(data), nil
}

// requestRecord is a validated generic request.
type requestRecord struct {
	id, kind   string
	parameters object
}

func validateRequestValue(value any, actor object) (requestRecord, error) {
	request, err := closed(value, []string{"id", "kind", "parameters"}, nil, "request", "request_invalid")
	if err != nil {
		return requestRecord{}, err
	}
	kind, ok := request["kind"].(string)
	if _, known := requestKinds[kind]; !ok || !known {
		return requestRecord{}, queueError("request_invalid", "unsupported request kind")
	}
	parameters, err := validateParameters(kind, request["parameters"])
	if err != nil {
		return requestRecord{}, err
	}
	id, err := requestIDValue(actor, kind, parameters)
	if err != nil {
		return requestRecord{}, err
	}
	if request["id"] != id {
		return requestRecord{}, queueError("request_invalid", "request ID does not bind actor, kind and parameters")
	}
	return requestRecord{id: id, kind: kind, parameters: parameters}, nil
}

// RequestID derives `request:` + sha256(canonical({actor, kind, parameters})).
func RequestID(actor Actor, kind string, parameters any) (string, error) {
	actorValue, err := genericValue(actor)
	if err != nil {
		return "", err
	}
	parameterValue, err := genericValue(parameters)
	if err != nil {
		return "", err
	}
	return requestIDValue(actorValue, kind, parameterValue)
}

// NewRequest canonicalizes parameters and returns a validated request bound to actor.
func NewRequest(kind string, actor Actor, parameters any) (Request, error) {
	actorValue, err := genericValue(actor)
	if err != nil {
		return Request{}, err
	}
	actorRecord, _, err := validateActorValue(actorValue)
	if err != nil {
		return Request{}, err
	}
	parameterValue, err := genericValue(parameters)
	if err != nil {
		return Request{}, err
	}
	id, err := requestIDValue(actorValue, kind, parameterValue)
	if err != nil {
		return Request{}, err
	}
	data, err := canonicalGeneric(parameterValue)
	if err != nil {
		return Request{}, err
	}
	request := Request{ID: id, Kind: kind, Parameters: data}
	requestValue, err := genericValue(request)
	if err != nil {
		return Request{}, err
	}
	if _, err := validateRequestValue(requestValue, actorRecord); err != nil {
		return Request{}, err
	}
	return request, nil
}

// CommitID is `q_` + sha256(previous ? previous + "\n" + request : request).
func CommitID(previous *string, requestID string) string {
	if previous != nil && *previous != "" {
		return "q_" + sha256Text(*previous+"\n"+requestID)
	}
	return "q_" + sha256Text(requestID)
}

// ProposedCommitID is the commit ID a request would receive appended to state.
func ProposedCommitID(state Projection, request Request) string {
	return CommitID(tipPointer(state.Tip), request.ID)
}

func tipPointer(tip string) *string {
	if tip == "" {
		return nil
	}
	return &tip
}

// BuildCommit builds the next commit for a request; at is informational only.
func BuildCommit(state Projection, actor Actor, request Request, operations []Operation, at int64) QueueCommit {
	previous := tipPointer(state.Tip)
	return QueueCommit{Version: Version, ID: CommitID(previous, request.ID), Previous: previous, Request: request, Actor: actor, At: at, Operations: operations}
}

// Op canonicalizes one typed operation.
func Op(value any) (Operation, error) {
	data, err := CanonicalValue(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func decodeParameters[T any](request Request) (T, error) {
	var parameters T
	err := json.Unmarshal(request.Parameters, &parameters)
	return parameters, err
}
