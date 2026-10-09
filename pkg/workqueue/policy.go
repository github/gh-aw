package workqueue

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var reasonPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$|^[a-f0-9]{64}$`)

const workerWorkflowDirectory = ".github/workflows/"
const defaultWorkerWorkflow = workerWorkflowDirectory + "worker.lock.yml"

var policySchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	data, err := schemas.ReadFile("schema/QueuePolicy.json")
	if err != nil {
		return nil, err
	}
	var resource any
	if err := json.Unmarshal(data, &resource); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("QueuePolicy.json", resource); err != nil {
		return nil, err
	}
	return compiler.Compile("QueuePolicy.json")
})

func DefaultPolicy(principal, repository string) Policy {
	return Policy{
		Mode: "weighted-priority", ClassWeights: []int{8, 4, 2, 1, 1},
		AccountingWeights: map[string]int{"": 1},
		Producers: map[string]ProducerRule{principal: {
			Pools: []string{"default"}, Priorities: []int{1, 2, 3, 4, 5}, FairnessKeys: []string{""},
		}},
		Pools: map[string]PoolPolicy{"default": {
			DefaultProfile: "default", LogicalLimit: 16, NativeLimit: 16,
			AllowedRepositories: []string{repository}, MaxObservationAgeMS: 60000,
			Retry:          RetryPolicy{MaxAttempts: 3, BackoffMS: 30000},
			Reconciliation: ReconciliationPolicy{MaxAttempts: 5, DeadlineMS: 300000},
			Profiles: map[string]WorkerProfile{"default": {
				Workflow: defaultWorkerWorkflow, Ref: strings.Repeat("0", 40),
				Principal: principal, TrustDomain: "default",
				CredentialScope: "repository", EffectScope: repository,
				MaxClaims: 1, ShareKeys: false,
			}},
		}},
		Limits: Limits{
			LedgerBytes: 64 << 20, RecoveryBytes: 16 << 20, PayloadBytes: 16 << 10,
			GraphNodes: 4096, Predecessors: 64, PendingNodes: 4096, Operations: 256,
			AssignmentBytes: 48 << 10, ResultBytes: 4 << 10, EvidenceBytes: 1 << 10,
			ObservationWrites: 4096,
		},
	}
}

// ValidatePolicy checks the closed contract and semantic limits, not caller
// authentication or approval of the configured worker identities.
func ValidatePolicy(policy Policy) error {
	data, err := canonicalValue(policy)
	if err != nil {
		return queueError("policy_invalid", "policy is not canonical: %v", err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	schema, err := policySchema()
	if err != nil {
		return err
	}
	if err := schema.Validate(value); err != nil {
		return queueError("policy_invalid", "invalid version-3 policy: %v", err)
	}
	if err := validateContractIdentityBytes("QueuePolicy", value); err != nil {
		return queueError("policy_invalid", "%v", err)
	}
	return validatePolicy(policy)
}

func validatePolicy(policy Policy) error {
	if err := validateProjectors(policy); err != nil {
		return err
	}
	if policy.Mode != "weighted-priority" && policy.Mode != "strict-priority" {
		return queueError("policy_invalid", "unsupported scheduling mode")
	}
	if len(policy.ClassWeights) != 5 || len(policy.Pools) == 0 || len(policy.Pools) > 64 ||
		len(policy.AccountingWeights) == 0 || len(policy.AccountingWeights) > 1024 {
		return queueError("policy_invalid", "class, pool, or account bounds")
	}
	for _, weight := range policy.ClassWeights {
		if weight < 1 || weight > 1000 {
			return queueError("policy_invalid", "weights must be integers in 1..1000")
		}
	}
	if policy.AccountingWeights[""] != 1 {
		return queueError("policy_invalid", "default accounting key must have weight 1")
	}
	for key, weight := range policy.AccountingWeights {
		if !validKey(key) || weight < 1 || weight > 1000 {
			return queueError("policy_invalid", "invalid account key or weight")
		}
	}
	for _, pool := range policy.Pools {
		if err := validatePoolPolicy(pool, policy.Limits); err != nil {
			return err
		}
	}
	if err := validateProducerRules(policy); err != nil {
		return err
	}
	return validatePolicyLimits(policy.Limits)
}

func validatePoolPolicy(pool PoolPolicy, limits Limits) error {
	if _, ok := pool.Profiles[pool.DefaultProfile]; !ok || len(pool.Profiles) > 256 {
		return queueError("policy_invalid", "pool default profile must be approved")
	}
	for _, profile := range pool.Profiles {
		if !decimalIdentity(profile.Principal) {
			return queueError("policy_invalid", "worker principal must be an immutable positive GitHub actor ID")
		}
		if profile.MaxClaims < 1 || profile.MaxClaims > MaxClaimsPerDispatch ||
			!revisionPattern.MatchString(profile.Ref) ||
			!strings.HasPrefix(profile.Workflow, workerWorkflowDirectory) ||
			!strings.HasSuffix(profile.Workflow, ".lock.yml") || strings.Contains(profile.Workflow, "..") {
			return queueError("policy_invalid", "profiles require immutable revisions, approved workflow paths, and assignment bounds 1..16")
		}
		if limits.Operations < 2*profile.MaxClaims+1 {
			return queueError("policy_invalid", "operation budget cannot hold bounded worst-case assignment closure")
		}
	}
	if pool.LogicalLimit < 1 || pool.LogicalLimit > 4096 || pool.NativeLimit < 1 ||
		pool.NativeLimit > 4096 || pool.PerAccountLimit < 0 || pool.PerAccountLimit > 4096 ||
		pool.Retry.MaxAttempts < 1 || pool.Retry.MaxAttempts > 16 ||
		pool.Reconciliation.MaxAttempts < 1 || pool.Reconciliation.MaxAttempts > 64 ||
		pool.Reconciliation.DeadlineMS < 1 || pool.Reconciliation.DeadlineMS > 3600000 ||
		pool.Retry.BackoffMS < 1 || pool.Retry.BackoffMS > 3600000 ||
		pool.MaxObservationAgeMS < 1 || pool.MaxObservationAgeMS > 3600000 {
		return queueError("policy_invalid", "pool capacity/recovery limits")
	}
	return nil
}

func validateProducerRules(policy Policy) error {
	for principal, rule := range policy.Producers {
		if !decimalIdentity(principal) {
			return queueError("policy_invalid", "producer principal must be an immutable positive GitHub actor ID")
		}
		if len(rule.Pools) == 0 || len(rule.Priorities) == 0 || len(rule.FairnessKeys) == 0 {
			return queueError("policy_invalid", "producer must have explicit entitlements")
		}
		for _, pool := range rule.Pools {
			if _, ok := policy.Pools[pool]; !ok {
				return queueError("policy_invalid", "producer references unknown pool")
			}
		}
		for _, priority := range rule.Priorities {
			if priority < 1 || priority > 5 {
				return queueError("policy_invalid", "invalid producer priority")
			}
		}
		for _, key := range rule.FairnessKeys {
			if _, ok := policy.AccountingWeights[key]; !ok {
				return queueError("policy_invalid", "producer references unknown accounting key")
			}
		}
	}
	return nil
}

func validatePolicyLimits(limit Limits) error {
	if limit.LedgerBytes < 1 || limit.LedgerBytes > 64<<20 ||
		limit.RecoveryBytes < 1 || limit.RecoveryBytes > 16<<20 ||
		limit.PayloadBytes < 1 || limit.PayloadBytes > 16<<10 ||
		limit.GraphNodes < 1 || limit.GraphNodes > 4096 ||
		limit.Predecessors < 1 || limit.Predecessors > 64 ||
		limit.PendingNodes < 1 || limit.PendingNodes > 4096 ||
		limit.Operations < 1 || limit.Operations > 256 ||
		limit.AssignmentBytes < 1 || limit.AssignmentBytes > 48<<10 ||
		limit.ResultBytes < 1 || limit.ResultBytes > 4<<10 ||
		limit.EvidenceBytes < 1 || limit.EvidenceBytes > 1<<10 ||
		limit.ObservationWrites < 1 || limit.ObservationWrites > 4096 {
		return queueError("policy_invalid", "limits exceed the supported engineering envelope")
	}
	return nil
}

func newProjection() Projection {
	return Projection{
		Works: map[string]*WorkState{}, Claims: map[string]*ClaimState{},
		Dispatches: map[string]*DispatchState{}, Observations: map[string]*Observation{},
		Requests: map[string]QueueCommit{}, Clocks: map[string]PoolClocks{},
		CredentialGeneration: "initial", ObservationWrites: map[string]int{},
	}
}

func (state Projection) quiescent() bool {
	for _, work := range state.Works {
		if (work.State != "completed" && work.State != "cancelled") ||
			(work.State == "completed" && work.Barrier == "pending") {
			return false
		}
	}
	for _, dispatch := range state.Dispatches {
		if !dispatch.Released {
			return false
		}
	}
	return true
}

func permitted(role, kind string) bool {
	switch kind {
	case "Policy", "Control":
		return role == "administrator"
	case "Work":
		return role == "producer" || role == "dispatcher" || role == "worker" || role == "administrator"
	case "Claim":
		return role == "dispatcher" || role == "administrator" || role == "worker"
	case "Completion":
		return role == "worker"
	case "Result", "DeliveryFailure", "Release":
		return role == "reconciler"
	case "Observation":
		return role == "reconciler" || role == "dispatcher" || role == "administrator" || role == "worker"
	case "Dispatch":
		return role == "dispatcher" || role == "worker" || role == "reconciler"
	case "ClaimCancellation":
		return role == "worker" || role == "reconciler"
	case "WorkCancellation":
		return role == "producer" || role == "administrator" || role == "reconciler" || role == "worker"
	case "WorkPriority":
		return role == "administrator"
	case "IssueLink", "IssueComment":
		return role == "projector"
	}
	return false
}

func validateRequest(commit QueueCommit) error {
	if err := validateRequestRole(commit.Actor, commit.Request.Kind); err != nil {
		return err
	}
	var parameters map[string]json.RawMessage
	if err := json.Unmarshal(commit.Request.Parameters, &parameters); err != nil {
		return err
	}
	kinds := map[string][]string{
		"policy": {"Policy"}, "control": {"Control", "WorkPriority"}, "submit": {"Work"},
		"dispatch_next": {"Observation", "Claim"}, "finish": {"Completion", "ClaimCancellation", "WorkCancellation"},
		"observe": {"Observation"}, "dispatch": {"Dispatch"}, "release": {"ClaimCancellation", "WorkCancellation", "Release"},
		"result": {"Result"}, "delivery_failure": {"DeliveryFailure"},
		"cancel_work": {"WorkCancellation"}, "cancel_claim": {"ClaimCancellation", "WorkCancellation"},
		"issue_link": {"IssueLink", "IssueComment"},
	}
	for _, operation := range commit.Operations {
		kind, err := operationKind(operation)
		if err != nil || !slices.Contains(kinds[commit.Request.Kind], kind) || !permitted(commit.Actor.Role, kind) {
			return queueError("unauthorized_operation", "%s cannot publish %s through %s", commit.Actor.Role, kind, commit.Request.Kind)
		}
		var reason struct {
			Reason json.RawMessage `json:"reason"`
		}
		if err := json.Unmarshal(operation, &reason); err != nil {
			return err
		}
		if len(reason.Reason) > 0 {
			var code string
			if err := json.Unmarshal(reason.Reason, &code); err != nil || !reasonPattern.MatchString(code) {
				return queueError("reason_invalid", "reason must be a bounded sanitized code")
			}
		}
	}
	return validateRequestIntent(commit, len(parameters))
}

func validateRequestIntent(commit QueueCommit, parameterCount int) error {
	switch commit.Request.Kind {
	case "submit":
		var params SubmitParameters
		if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil || params.Nodes == nil {
			return queueError("request_invalid", "submit requires nodes")
		}
		if len(params.Nodes) != len(commit.Operations) {
			return queueError("request_invalid", "submission differs from stable intent")
		}
		for i, node := range params.Nodes {
			if i < 0 || i >= len(commit.Operations) {
				return queueError("request_invalid", "submission differs from stable intent")
			}
			if !sameJSON(node, commit.Operations[i]) {
				return queueError("request_invalid", "submission changes immutable node")
			}
		}
	case "dispatch_next":
		var params DispatchParameters
		if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil {
			return queueError("request_invalid", "invalid dispatch budgets")
		}
		if err := validateDispatchParameters(params); err != nil {
			return err
		}
		if parameterCount != 4 {
			return queueError("request_invalid", "dispatch_next requires exactly pool/max_claims/max_dispatches/max_bytes")
		}
	case "finish":
		var params FinishParameters
		if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil ||
			params.DispatchID == "" || params.ClaimHandle == "" ||
			(len(commit.Operations) != 1 && len(commit.Operations) != 2) {
			return queueError("request_invalid", "finish requires one immutable Claim scope plus its exhausted retry closure")
		}
		if len(commit.Operations) == 2 {
			for index, operation := range commit.Operations {
				expected := "ClaimCancellation"
				if index == 1 {
					expected = "WorkCancellation"
				}
				kind, err := operationKind(operation)
				if err != nil || params.Outcome != "cancelled" || kind != expected {
					return queueError("request_invalid", "only exhausted cancellation may have a second operation")
				}
			}
		}
	default:
		var params OperationsParameters
		if err := json.Unmarshal(commit.Request.Parameters, &params); err != nil ||
			!sameJSON(params.Operations, commit.Operations) {
			return queueError("request_invalid", "operations differ from stable intent")
		}
	}
	return nil
}

func (state Projection) allowedProducer(actor Actor, node WorkDefinition) bool {
	rule, ok := state.Policy.Producers[actor.Principal]
	return ok && slices.Contains(rule.Pools, node.Pool) &&
		slices.Contains(rule.Priorities, node.Priority) && slices.Contains(rule.FairnessKeys, node.FairnessKey)
}

func (state Projection) validateSubmissionEntitlement(actor Actor, node WorkDefinition) error {
	if err := validateActorOrigin(actor); err != nil {
		return err
	}
	if err := validateRequestRole(actor, "submit"); err != nil {
		return err
	}
	if actor.Role == "worker" {
		_, parent, err := state.workerContinuationScope(actor)
		if err != nil {
			return err
		}
		if node.Pool != parent.Pool || node.Priority != parent.Priority || node.FairnessKey != parent.FairnessKey {
			return queueError("child_entitlement", "worker children must inherit their parent's pool, priority and account")
		}
		return nil
	}
	if !state.allowedProducer(actor, node) {
		return queueError("admission_unauthorized", "principal has no pool/priority/account entitlement")
	}
	return nil
}

func NewWork(payload []byte, graphID, nodeKey, pool string, policy Policy, at int64) (WorkDefinition, error) {
	canonical, err := Canonical(payload)
	if err != nil {
		return WorkDefinition{}, err
	}

	var object map[string]any
	if err := json.Unmarshal(canonical, &object); err != nil || object == nil {
		return WorkDefinition{}, errors.New("work payload must be a JSON object")
	}
	poolPolicy, ok := policy.Pools[pool]
	if !ok {
		return WorkDefinition{}, queueError("pool_invalid", "pool %s is not approved", pool)
	}
	profile := poolPolicy.Profiles[poolPolicy.DefaultProfile]
	return WorkDefinition{
		Kind: "Work", WorkID: NodeID(graphID, nodeKey), GraphID: graphID, NodeKey: nodeKey,
		Pool: pool, Priority: 3, FairnessKey: "", WorkerProfile: poolPolicy.DefaultProfile,
		BatchTrustDomain: profile.TrustDomain, Payload: canonical,
		DependsOn: []Dependency{}, Enqueued: at,
	}, nil
}

func NewChildWork(state Projection, actor Actor, payload []byte, nodeKey string, at int64) (WorkDefinition, error) {
	if err := validateActorOrigin(actor); err != nil {
		return WorkDefinition{}, err
	}
	if err := validateRequestRole(actor, "submit"); err != nil {
		return WorkDefinition{}, err
	}
	_, parent, err := state.workerContinuationScope(actor)
	if err != nil {
		return WorkDefinition{}, err
	}
	work, err := NewWork(payload, parent.GraphID, nodeKey, parent.Pool, *state.Policy, at)
	if err != nil {
		return WorkDefinition{}, err
	}
	work.Priority, work.FairnessKey = parent.Priority, parent.FairnessKey
	return work, nil
}
