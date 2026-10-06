package workqueue

import "encoding/json"

const FileName = "work-queue.jsonl"
const DefaultBranch = "work-queue"
const Version = 3
const MaxTimestamp int64 = 9007199254740991
const MaxEnqueued = MaxTimestamp
const MaxClaimsPerDispatch = 16

type Actor struct {
	Role        string `json:"role"`
	Principal   string `json:"principal"`
	Repository  string `json:"repository"`
	Workflow    string `json:"workflow,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	RunAttempt  int    `json:"run_attempt,omitempty"`
	DispatchID  string `json:"dispatch_id,omitempty"`
	ClaimHandle string `json:"claim_handle,omitempty"`
}

type Request struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Parameters  json.RawMessage `json:"parameters"`
	Fingerprint string          `json:"fingerprint"`
}

type QueueCommit struct {
	Version     int         `json:"version"`
	ID          string      `json:"id"`
	Previous    *string     `json:"previous"`
	Request     Request     `json:"request"`
	Actor       Actor       `json:"actor"`
	PolicyEpoch string      `json:"policy_epoch"`
	At          int64       `json:"at"`
	Operations  []Operation `json:"operations"`
	Trace       *Trace      `json:"trace,omitempty"`
}

type Trace struct {
	TraceID          string `json:"trace_id,omitempty"`
	SpanID           string `json:"span_id,omitempty"`
	PublisherAttempt int    `json:"publisher_attempt,omitempty"`
}

type Resource struct {
	Kind         string `json:"kind"`
	Host         string `json:"host"`
	Repository   string `json:"repository"`
	RepositoryID string `json:"repository_id"`
	ResourceID   string `json:"resource_id"`
	Number       string `json:"number"`
}

type Dependency struct {
	Kind      string    `json:"kind"`
	WorkID    string    `json:"work_id,omitempty"`
	Resource  *Resource `json:"resource,omitempty"`
	Condition string    `json:"condition,omitempty"`
}

type Replacement struct {
	WorkID      string `json:"work_id"`
	Disposition string `json:"disposition"`
	Evidence    string `json:"evidence"`
}

type WorkDefinition struct {
	Kind             string          `json:"kind"`
	WorkID           string          `json:"work_id"`
	GraphID          string          `json:"graph_id"`
	NodeKey          string          `json:"node_key"`
	Pool             string          `json:"pool"`
	Priority         int             `json:"priority"`
	FairnessKey      string          `json:"fairness_key"`
	WorkerProfile    string          `json:"worker_profile"`
	BatchTrustDomain string          `json:"batch_trust_domain"`
	Payload          json.RawMessage `json:"payload"`
	DependsOn        []Dependency    `json:"depends_on"`
	Enqueued         int64           `json:"enqueued"`
	Subject          *Resource       `json:"subject,omitempty"`
	ReplacementOf    *Replacement    `json:"replacement_of,omitempty"`
}

type Policy struct {
	Mode              string                  `json:"mode"`
	ClassWeights      []int                   `json:"class_weights"`
	AccountingWeights map[string]int          `json:"accounting_weights"`
	Producers         map[string]ProducerRule `json:"producers"`
	Pools             map[string]PoolPolicy   `json:"pools"`
	Limits            Limits                  `json:"limits"`
}

type ProducerRule struct {
	Pools        []string `json:"pools"`
	Priorities   []int    `json:"priorities"`
	FairnessKeys []string `json:"fairness_keys"`
}

type PoolPolicy struct {
	DefaultProfile      string                   `json:"default_profile"`
	Profiles            map[string]WorkerProfile `json:"profiles"`
	LogicalLimit        int                      `json:"logical_limit"`
	NativeLimit         int                      `json:"native_limit"`
	PerAccountLimit     int                      `json:"per_account_limit,omitempty"`
	AllowedRepositories []string                 `json:"allowed_repositories"`
	MaxObservationAgeMS int64                    `json:"max_observation_age_ms"`
	Retry               RetryPolicy              `json:"retry"`
	Reconciliation      ReconciliationPolicy     `json:"reconciliation"`
}

type WorkerProfile struct {
	Workflow        string `json:"workflow"`
	Ref             string `json:"ref"`
	Principal       string `json:"principal"`
	TrustDomain     string `json:"trust_domain"`
	CredentialScope string `json:"credential_scope"`
	EffectScope     string `json:"effect_scope"`
	MaxClaims       int    `json:"max_claims"`
	ShareKeys       bool   `json:"share_keys"`
}

type RetryPolicy struct {
	MaxAttempts int   `json:"max_attempts"`
	BackoffMS   int64 `json:"backoff_ms"`
}

type ReconciliationPolicy struct {
	MaxAttempts int   `json:"max_attempts"`
	DeadlineMS  int64 `json:"deadline_ms"`
}

type Limits struct {
	LedgerBytes       int64 `json:"ledger_bytes"`
	RecoveryBytes     int64 `json:"recovery_bytes"`
	PayloadBytes      int64 `json:"payload_bytes"`
	GraphNodes        int   `json:"graph_nodes"`
	Predecessors      int   `json:"predecessors"`
	PendingNodes      int   `json:"pending_nodes"`
	Operations        int   `json:"operations"`
	AssignmentBytes   int64 `json:"assignment_bytes"`
	ResultBytes       int64 `json:"result_bytes"`
	EvidenceBytes     int64 `json:"evidence_bytes"`
	ObservationWrites int   `json:"observation_writes"`
}

type Evidence struct {
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Ref        string `json:"ref"`
	Principal  string `json:"principal"`
	CheckedAt  int64  `json:"checked_at"`
	RunID      string `json:"run_id,omitempty"`
	RunAttempt int    `json:"run_attempt,omitempty"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	Receipt    string `json:"receipt,omitempty"`
	Attempts   int    `json:"attempts,omitempty"`
	Effects    string `json:"effects,omitempty"`
}

type RunBinding struct {
	RunID      string `json:"run_id"`
	RunAttempt int    `json:"run_attempt"`
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Ref        string `json:"ref"`
	Principal  string `json:"principal"`
	Event      string `json:"event"`
}

// Operation stores a closed variant as raw JSON. Typed accessors keep absent,
// null, and explicit zero distinct, unlike a struct with omitempty authority.
type Operation = json.RawMessage

type ClaimOperation struct {
	Kind         string   `json:"kind"`
	WorkID       string   `json:"work_id"`
	ClaimID      string   `json:"claim_id"`
	DispatchID   string   `json:"dispatch_id"`
	Handle       string   `json:"handle"`
	Observations []string `json:"observations"`
}

type Assignment struct {
	Version       int               `json:"version"`
	DispatchID    string            `json:"dispatch_id"`
	RequestID     string            `json:"request_id"`
	CommitID      string            `json:"commit_id"`
	PolicyEpoch   string            `json:"policy_epoch"`
	Pool          string            `json:"pool"`
	WorkerProfile string            `json:"worker_profile"`
	Claims        []AssignmentClaim `json:"claims"`
}

type AssignmentClaim struct {
	Handle     string            `json:"handle"`
	ClaimID    string            `json:"claim_id"`
	WorkID     string            `json:"work_id"`
	Work       json.RawMessage   `json:"work"`
	ResultRefs []ResultReference `json:"result_refs"`
}

type ResultReference struct {
	WorkID         string          `json:"work_id"`
	ResultCommitID string          `json:"result_commit_id"`
	Descriptor     json.RawMessage `json:"descriptor"`
}

type DispatchParameters struct {
	Pool          string `json:"pool"`
	MaxClaims     int    `json:"max_claims"`
	MaxDispatches int    `json:"max_dispatches"`
	MaxBytes      int64  `json:"max_bytes"`
}

type FinishParameters struct {
	DispatchID  string `json:"dispatch_id"`
	ClaimHandle string `json:"claim_handle"`
	Outcome     string `json:"outcome"`
}

type OperationsParameters struct {
	Operations []Operation `json:"operations"`
}

type SubmitParameters struct {
	Nodes []WorkDefinition `json:"nodes"`
}

type Position struct {
	Commit    int `json:"commit"`
	Operation int `json:"operation"`
}

type WorkState struct {
	WorkDefinition
	State                string          `json:"state"`
	Position             Position        `json:"position"`
	ClaimID              string          `json:"claim_id,omitempty"`
	Attempts             int             `json:"attempts"`
	RetryNotBefore       int64           `json:"retry_not_before"`
	CompletionID         string          `json:"completion_id,omitempty"`
	Barrier              string          `json:"barrier"`
	Result               json.RawMessage `json:"result,omitempty"`
	ResultCommitID       string          `json:"result_commit_id,omitempty"`
	Disposition          string          `json:"disposition,omitempty"`
	CancellationReason   string          `json:"cancellation_reason,omitempty"`
	CancellationCommitID string          `json:"cancellation_commit_id,omitempty"`
	completionAt         int64
}

type ClaimState struct {
	ClaimOperation
	State              string `json:"state"`
	CommitID           string `json:"commit_id"`
	RequestID          string `json:"request_id"`
	TerminalCommitID   string `json:"terminal_commit_id,omitempty"`
	CancellationReason string `json:"cancellation_reason,omitempty"`
	RetryNotBefore     int64  `json:"retry_not_before,omitempty"`
}

type DispatchState struct {
	Assignment
	Profile         WorkerProfile `json:"profile"`
	State           string        `json:"state"`
	Sender          *Actor        `json:"sender,omitempty"`
	Run             *RunBinding   `json:"run,omitempty"`
	Released        bool          `json:"released"`
	Reason          string        `json:"reason,omitempty"`
	LifecycleWrites int           `json:"lifecycle_writes"`
}

type Observation struct {
	Kind                 string   `json:"kind"`
	ObservationID        string   `json:"observation_id"`
	Resource             Resource `json:"resource"`
	Condition            string   `json:"condition"`
	State                string   `json:"state"`
	ObservedAt           int64    `json:"observed_at"`
	CredentialGeneration string   `json:"credential_generation"`
	SourceUpdatedAt      *int64   `json:"source_updated_at,omitempty"`
	ReadStatus           string   `json:"read_status"`
	StateReason          string   `json:"state_reason,omitempty"`
	ResourceState        string   `json:"resource_state,omitempty"`
	Merged               *bool    `json:"merged,omitempty"`
	MergeCommit          string   `json:"merge_commit,omitempty"`
}

type Clock struct {
	V      string            `json:"v"`
	Pass   map[string]string `json:"pass"`
	Active map[string]bool   `json:"active"`
}

type PoolClocks struct {
	Classes Clock         `json:"classes"`
	Keys    map[int]Clock `json:"keys"`
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

type Projection struct {
	Repository           string                    `json:"repository"`
	Tip                  string                    `json:"tip"`
	PolicyEpoch          string                    `json:"policy_epoch"`
	Policy               *Policy                   `json:"policy"`
	Works                map[string]*WorkState     `json:"works"`
	Claims               map[string]*ClaimState    `json:"claims"`
	Dispatches           map[string]*DispatchState `json:"dispatches"`
	Observations         map[string]*Observation   `json:"observations"`
	Requests             map[string]QueueCommit    `json:"requests"`
	Clocks               map[string]PoolClocks     `json:"clocks"`
	AdmissionPaused      bool                      `json:"admission_paused"`
	GrantsPaused         bool                      `json:"grants_paused"`
	CredentialGeneration string                    `json:"credential_generation"`
	Stats                Stats                     `json:"stats"`
	LedgerBytes          int64                     `json:"ledger_bytes"`
	ObservationWrites    map[string]int            `json:"observation_writes"`
}

type Selection struct {
	WorkID       string   `json:"work_id,omitempty"`
	Reason       string   `json:"reason"`
	Observations []string `json:"observations"`
	ClassPass    string   `json:"class_pass,omitempty"`
	KeyPass      string   `json:"key_pass,omitempty"`
}

type Decision struct {
	Tip         string       `json:"tip"`
	Operations  []Operation  `json:"operations"`
	Assignments []Assignment `json:"assignments"`
	Reason      string       `json:"reason"`
	Next        Selection    `json:"next"`
}
