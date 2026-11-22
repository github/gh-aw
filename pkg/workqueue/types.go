package workqueue

import "encoding/json"

const (
	FileName      = "work-queue.jsonl"
	DefaultBranch = "work-queue"
	// Version is the ledger protocol version; replay rejects every other version.
	Version = 4
)

// Actor is built from the publishing job's own context; write access to the
// queue branch is the only authority v4 relies on.
type Actor struct {
	Role       string `json:"role"`
	Repository string `json:"repository"`
	Workflow   string `json:"workflow,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	RunAttempt int    `json:"run_attempt,omitempty"`
}

// Request binds an actor, a closed request kind and its parameters.
type Request struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Parameters json.RawMessage `json:"parameters"`
}

// Operation is the canonical JSON of one closed ledger operation.
type Operation = json.RawMessage

// QueueCommit is one line of work-queue.jsonl. At is informational only.
type QueueCommit struct {
	Version    int         `json:"version"`
	ID         string      `json:"id"`
	Previous   *string     `json:"previous"`
	Request    Request     `json:"request"`
	Actor      Actor       `json:"actor"`
	At         int64       `json:"at"`
	Operations []Operation `json:"operations"`
}

type Resource struct {
	Kind         string `json:"kind"`
	Host         string `json:"host"`
	Repository   string `json:"repository"`
	RepositoryID string `json:"repository_id"`
	ResourceID   string `json:"resource_id"`
	Number       string `json:"number"`
}

// Dependency is either a Work edge (WorkID) or an external Issue/Pull Request gate.
type Dependency struct {
	Kind      string    `json:"kind"`
	WorkID    string    `json:"work_id,omitempty"`
	Resource  *Resource `json:"resource,omitempty"`
	Condition string    `json:"condition,omitempty"`
}

type Replacement struct {
	WorkID string `json:"work_id"`
}

// WorkContinuation is the remaining-work context a finish hands to its successor,
// kept beside (never inside) the inherited payload.
type WorkContinuation struct {
	Route     string `json:"route"`
	Message   string `json:"message"`
	Remaining int    `json:"remaining"`
}

// WorkDefinition is the immutable Work operation.
type WorkDefinition struct {
	Kind          string            `json:"kind"`
	WorkID        string            `json:"work_id"`
	GraphID       string            `json:"graph_id"`
	NodeKey       string            `json:"node_key"`
	Pool          string            `json:"pool"`
	Priority      int               `json:"priority"`
	FairnessKey   string            `json:"fairness_key"`
	WorkerProfile string            `json:"worker_profile"`
	Payload       json.RawMessage   `json:"payload"`
	DependsOn     []Dependency      `json:"depends_on"`
	Subject       *Resource         `json:"subject,omitempty"`
	BackingIssue  *Resource         `json:"backing_issue,omitempty"`
	ReplacementOf *Replacement      `json:"replacement_of,omitempty"`
	Continuation  *WorkContinuation `json:"continuation,omitempty"`
}

type ObservationOperation struct {
	Kind          string   `json:"kind"`
	ObservationID string   `json:"observation_id"`
	Resource      Resource `json:"resource"`
	Condition     string   `json:"condition"`
	State         string   `json:"state"`
	StateReason   string   `json:"state_reason,omitempty"`
	Merged        *bool    `json:"merged,omitempty"`
	MergeCommit   string   `json:"merge_commit,omitempty"`
	// ApprovedBy is the account ID of the writer whose close as completed approved the gate.
	ApprovedBy string `json:"approved_by,omitempty"`
}

type ClaimOperation struct {
	Kind       string `json:"kind"`
	WorkID     string `json:"work_id"`
	ClaimID    string `json:"claim_id"`
	DispatchID string `json:"dispatch_id"`
	Handle     string `json:"handle"`
}

type DispatchOperation struct {
	Kind       string `json:"kind"`
	DispatchID string `json:"dispatch_id"`
	State      string `json:"state"`
}

type BoundOperation struct {
	Kind       string `json:"kind"`
	DispatchID string `json:"dispatch_id"`
	RunID      string `json:"run_id"`
	RunAttempt int    `json:"run_attempt"`
}

type CompletionOperation struct {
	Kind        string          `json:"kind"`
	WorkID      string          `json:"work_id"`
	ClaimID     string          `json:"claim_id"`
	DispatchID  string          `json:"dispatch_id"`
	ClaimHandle string          `json:"claim_handle"`
	Effects     json.RawMessage `json:"effects"`
}

type ClaimCancellationOperation struct {
	Kind    string `json:"kind"`
	WorkID  string `json:"work_id"`
	ClaimID string `json:"claim_id"`
	Reason  string `json:"reason"`
}

type WorkCancellationOperation struct {
	Kind    string `json:"kind"`
	WorkID  string `json:"work_id"`
	Reason  string `json:"reason"`
	ClaimID string `json:"claim_id,omitempty"`
}

type ReleaseOperation struct {
	Kind       string `json:"kind"`
	DispatchID string `json:"dispatch_id"`
	Reason     string `json:"reason"`
}

type CheckpointOperation struct {
	Kind     string          `json:"kind"`
	PriorTip string          `json:"prior_tip"`
	State    json.RawMessage `json:"state"`
}

type IssueLinkOperation struct {
	Kind     string   `json:"kind"`
	WorkID   string   `json:"work_id"`
	Resource Resource `json:"resource"`
	ClaimID  string   `json:"claim_id,omitempty"`
}

type IssueCommentOperation struct {
	Kind      string `json:"kind"`
	WorkID    string `json:"work_id"`
	CommentID string `json:"comment_id"`
	ClaimID   string `json:"claim_id,omitempty"`
}

// Request parameters (closed per request kind).

type WorkIDsParameters struct {
	WorkIDs []string `json:"work_ids"`
}

type DispatchProfile struct {
	Workflow string `json:"workflow"`
}

type DispatchParameters struct {
	Pool            string                     `json:"pool"`
	MaxClaims       int                        `json:"max_claims"`
	MaxDispatches   int                        `json:"max_dispatches"`
	Concurrency     int                        `json:"concurrency"`
	PerAccountLimit int                        `json:"per_account_limit"`
	Profiles        map[string]DispatchProfile `json:"profiles"`
}

type DispatchIDParameters struct {
	DispatchID string `json:"dispatch_id"`
}

type ReleaseParameters struct {
	DispatchID string `json:"dispatch_id"`
	Reason     string `json:"reason"`
}

type CancelWorkParameters struct {
	WorkIDs []string `json:"work_ids"`
	Reason  string   `json:"reason"`
}

type CheckpointParameters struct {
	PriorTip string `json:"prior_tip"`
}

type IssueLinkParameters struct {
	Operations []Operation `json:"operations"`
}

// Projection records (also the checkpoint state format).

type Position struct {
	Commit    int `json:"commit"`
	Operation int `json:"operation"`
}

type WorkState struct {
	WorkDefinition
	State                string          `json:"state"`
	Position             Position        `json:"position"`
	Attempts             int             `json:"attempts"`
	ClaimID              string          `json:"claim_id,omitempty"`
	CompletionID         string          `json:"completion_id,omitempty"`
	Effects              json.RawMessage `json:"effects,omitempty"`
	CancellationReason   string          `json:"cancellation_reason,omitempty"`
	CancellationCommitID string          `json:"cancellation_commit_id,omitempty"`
	IssueComment         string          `json:"issue_comment,omitempty"`
}

type ClaimState struct {
	ClaimID            string          `json:"claim_id"`
	WorkID             string          `json:"work_id"`
	DispatchID         string          `json:"dispatch_id"`
	Handle             string          `json:"handle"`
	State              string          `json:"state"`
	CommitID           string          `json:"commit_id"`
	RequestID          string          `json:"request_id"`
	TerminalCommitID   string          `json:"terminal_commit_id,omitempty"`
	CancellationReason string          `json:"cancellation_reason,omitempty"`
	Effects            json.RawMessage `json:"effects,omitempty"`
	IssueComment       string          `json:"issue_comment,omitempty"`
}

type RunBinding struct {
	RunID      string `json:"run_id"`
	RunAttempt int    `json:"run_attempt"`
}

type DispatchState struct {
	DispatchID    string      `json:"dispatch_id"`
	RequestID     string      `json:"request_id"`
	CommitID      string      `json:"commit_id"`
	Pool          string      `json:"pool"`
	WorkerProfile string      `json:"worker_profile"`
	Workflow      string      `json:"workflow"`
	State         string      `json:"state"`
	Run           *RunBinding `json:"run"`
	Claims        []string    `json:"claims"`
}

type Stats struct {
	Work         int `json:"work"`
	Available    int `json:"available"`
	Claimed      int `json:"claimed"`
	Completed    int `json:"completed"`
	Cancelled    int `json:"cancelled"`
	Claims       int `json:"claims"`
	Dispatches   int `json:"dispatches"`
	Transactions int `json:"transactions"`
	Nodes        int `json:"nodes"`
}

// Projection is the state derived by replaying the ledger in file order.
type Projection struct {
	Repository string
	Tip        string
	// Commits is the ordinal of the next commit, including history folded into a checkpoint.
	Commits      int
	Works        map[string]*WorkState
	Claims       map[string]*ClaimState
	Dispatches   map[string]*DispatchState
	Observations map[string]ObservationOperation
	Requests     map[string]struct{}
	WorkCreators map[string]Actor
	Transactions []QueueCommit
	Stats        Stats

	dispatchOrder []string
	admission     *admissionIndex
}

// Decision is a planned dispatch_next selection.
type Decision struct {
	Operations []ClaimOperation `json:"operations"`
	Dispatches []string         `json:"dispatches"`
	Reason     string           `json:"reason"`
}

// Assignment is the worker's view of one Dispatch.
type Assignment struct {
	DispatchID string            `json:"dispatch_id"`
	Claims     []AssignmentClaim `json:"claims"`
}

type AssignmentClaim struct {
	Handle       string            `json:"handle"`
	ClaimID      string            `json:"claim_id"`
	WorkID       string            `json:"work_id"`
	Work         json.RawMessage   `json:"work"`
	ResultRefs   []ResultReference `json:"result_refs"`
	Continuation *WorkContinuation `json:"continuation,omitempty"`
}

type ResultReference struct {
	WorkID         string          `json:"work_id"`
	ResultCommitID string          `json:"result_commit_id"`
	Descriptor     json.RawMessage `json:"descriptor"`
}
