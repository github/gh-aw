package workqueue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (b Branch) verifyWorkerRoutes(ctx context.Context, policy Policy) error {
	if err := ValidatePolicy(policy); err != nil {
		return err
	}
	seen := identitySet{}
	for _, pool := range policy.Pools {
		for _, profile := range pool.Profiles {
			key := profile.Workflow + "\n" + profile.Ref
			if seen.contains(key) {
				continue
			}
			if err := b.verifyWorkerRoute(ctx, profile); err != nil {
				return err
			}
			seen.add(key)
		}
	}
	return nil
}

func (b Branch) verifyWorkerRoute(ctx context.Context, profile WorkerProfile) error {
	if profile.Ref == strings.Repeat("0", 40) || profile.Ref == strings.Repeat("0", 64) {
		return queueError("policy_missing", "constructor placeholder revisions cannot provision a worker route")
	}
	var file struct {
		Type     string `json:"type"`
		Path     string `json:"path"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := b.request(ctx, http.MethodGet, "contents/"+profile.Workflow+"?ref="+profile.Ref, nil, &file); err != nil {
		return queueError("policy_missing", "approved worker route cannot be verified at its immutable revision: %v", err)
	}
	if file.Type != "file" || file.Path != profile.Workflow || file.Encoding != "base64" {
		return queueError("policy_missing", "approved worker route is not an exact immutable workflow file")
	}
	content, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || len(content) > 1<<20 {
		return queueError("policy_missing", "approved worker route requires bounded valid workflow content")
	}
	var workflow struct {
		On struct {
			WorkflowDispatch struct {
				Inputs map[string]struct {
					Type string `yaml:"type"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return queueError("policy_missing", "approved worker route has invalid workflow YAML: %v", err)
	}
	if workflow.On.WorkflowDispatch.Inputs["work_queue_assignment"].Type != "string" {
		return queueError("policy_missing", "approved worker route must accept the work_queue_assignment string input")
	}
	var registered struct {
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if err := b.request(ctx, http.MethodGet, "actions/workflows/"+path.Base(profile.Workflow), nil, &registered); err != nil {
		return queueError("policy_missing", "approved worker route registration cannot be verified: %v", err)
	}
	if registered.Path != profile.Workflow || registered.State != "active" {
		return queueError("policy_missing", "approved worker route must be active at its exact registered path")
	}
	return nil
}

func (b Branch) initialPolicyCommit(ctx context.Context, actor Actor, request Request) (QueueCommit, bool, error) {
	if request.Kind == "submit" && actor.Role == "producer" {
		commit, err := b.initialSubmissionCommit(ctx, actor, request)
		return commit, err == nil, err
	}
	if request.Kind != "policy" {
		return QueueCommit{}, false, queueError("queue_missing", "only a first producer submission can bootstrap an absent queue")
	}
	if actor.Role != "administrator" {
		return QueueCommit{}, false, queueError("actor_unauthorized", "explicit Policy installation requires administrator authority")
	}
	var parameters OperationsParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return QueueCommit{}, false, queueError("request_invalid", "initial policy requires exactly one Policy operation")
	}
	only, unique := singleOperation(parameters.Operations)
	if !unique {
		return QueueCommit{}, false, queueError("request_invalid", "initial policy requires exactly one Policy operation")
	}
	kind, err := operationKind(only)
	if err != nil || kind != "Policy" {
		return QueueCommit{}, false, queueError("request_invalid", "initial policy requires exactly one Policy operation")
	}
	var operation struct {
		Policy Policy `json:"policy"`
		Epoch  string `json:"epoch"`
	}
	if err := json.Unmarshal(only, &operation); err != nil {
		return QueueCommit{}, false, queueError("request_invalid", "initial policy operation is malformed")
	}
	if err := b.verifyWorkerRoutes(ctx, operation.Policy); err != nil {
		return QueueCommit{}, false, err
	}
	commit, err := Genesis(actor, operation.Policy, request.ID, operation.Epoch, time.Now().UnixMilli())
	if err != nil {
		return QueueCommit{}, false, err
	}
	if !sameJSON(commit.Request, request) {
		return QueueCommit{}, false, queueError("actor_unauthorized", "initial policy differs from the authenticated administrator request")
	}
	return commit, true, nil
}

func (b Branch) initialSubmissionCommit(ctx context.Context, actor Actor, request Request) (QueueCommit, error) {
	policy := b.PolicyProposal
	if policy == nil {
		resolved, err := b.defaultPolicy(ctx, actor.Principal)
		if err != nil {
			return QueueCommit{}, err
		}
		policy = &resolved
	}
	commit, err := bootstrapSubmit(actor, *policy, request, time.Now().UnixMilli())
	if err != nil {
		return QueueCommit{}, err
	}
	state, err := Replay([]QueueCommit{commit})
	if err != nil {
		return QueueCommit{}, err
	}
	if err := b.verifyRequestEvidence(ctx, state, actor, request); err != nil {
		return QueueCommit{}, err
	}
	if err := b.verifyWorkerRoutes(ctx, *policy); err != nil {
		return QueueCommit{}, err
	}
	return commit, nil
}

func bootstrapSubmit(actor Actor, policy Policy, request Request, at int64) (QueueCommit, error) {
	if request.Kind != "submit" || (actor.Role != "producer" && actor.Role != "dispatcher") {
		return QueueCommit{}, queueError("actor_unauthorized", "first-submit bootstrap requires a producer or dispatcher")
	}
	seed := hashBytes([]byte(request.ID))
	epoch := "epoch_" + seed
	policyOperation, err := Op(map[string]any{"kind": "Policy", "epoch": epoch, "policy": policy})
	if err != nil {
		return QueueCommit{}, err
	}
	commit := QueueCommit{
		Version: Version, ID: "q_" + seed, Request: request, Actor: actor,
		PolicyEpoch: epoch, At: at,
	}
	// Reuse ordinary Work construction, then replay Policy and Work atomically.
	if _, _, err := populateSubmissionCandidate(Projection{}, &commit, Decision{}); err != nil {
		return QueueCommit{}, err
	}
	if len(commit.Operations) == 0 {
		return QueueCommit{}, queueError("request_invalid", "first submission must admit Work")
	}
	commit.Operations = append([]Operation{policyOperation}, commit.Operations...)
	if _, err := Replay([]QueueCommit{commit}); err != nil {
		return QueueCommit{}, err
	}
	return commit, nil
}
