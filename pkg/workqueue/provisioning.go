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
	seen := map[string]bool{}
	for _, pool := range policy.Pools {
		for _, profile := range pool.Profiles {
			key := profile.Workflow + "\n" + profile.Ref
			if seen[key] {
				continue
			}
			if err := b.verifyWorkerRoute(ctx, profile); err != nil {
				return err
			}
			seen[key] = true
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
	admin, err := b.Authenticate(ctx, "administrator")
	if err != nil {
		return QueueCommit{}, false, queueError("policy_missing", "new queues require authenticated policy initialization: %v", err)
	}
	if request.Kind == "policy" {
		var parameters OperationsParameters
		if err := json.Unmarshal(request.Parameters, &parameters); err != nil || len(parameters.Operations) != 1 ||
			operationKind(parameters.Operations[0]) != "Policy" {
			return QueueCommit{}, false, queueError("request_invalid", "initial policy requires exactly one Policy operation")
		}
		var operation struct {
			Policy Policy `json:"policy"`
			Epoch  string `json:"epoch"`
		}
		if err := json.Unmarshal(parameters.Operations[0], &operation); err != nil {
			return QueueCommit{}, false, queueError("request_invalid", "initial policy operation is malformed")
		}
		if err := b.verifyWorkerRoutes(ctx, operation.Policy); err != nil {
			return QueueCommit{}, false, err
		}
		commit, err := Genesis(actor, operation.Policy, request.ID, operation.Epoch, time.Now().UnixMilli())
		if err != nil {
			return QueueCommit{}, false, err
		}
		if !sameJSON(commit.Request, request) || !sameJSON(actor, admin) {
			return QueueCommit{}, false, queueError("actor_unauthorized", "initial policy differs from the authenticated administrator request")
		}
		return commit, true, nil
	}
	policy, err := b.defaultPolicy(ctx, admin.Principal)
	if err != nil {
		return QueueCommit{}, false, err
	}
	commit, err := Genesis(admin, policy,
		"init_"+hashBytes([]byte(request.ID)), "epoch_"+hashBytes([]byte(request.ID)), time.Now().UnixMilli())
	return commit, false, err
}
