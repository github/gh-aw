package workqueue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"gopkg.in/yaml.v3"
)

const repositorySettingsPath = workerWorkflowDirectory + "aw.json"

type configWorkerSourceEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

func (b Branch) configFile(ctx context.Context, filePath, revision string) ([]byte, error) {
	var file struct {
		Type     string `json:"type"`
		Path     string `json:"path"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := b.request(ctx, http.MethodGet, "contents/"+filePath+"?ref="+url.QueryEscape(revision), nil, &file); err != nil {
		return nil, err
	}
	if file.Type != "file" || file.Path != filePath || file.Encoding != "base64" {
		return nil, queueError("policy_missing", "%s must be an exact regular file at the verified default revision", filePath)
	}
	content, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || len(content) > 1<<20 {
		return nil, queueError("policy_missing", "%s requires bounded valid file content", filePath)
	}
	return content, nil
}

func (b Branch) approvedConfigPolicy(ctx context.Context, revision string) (Policy, error) {
	data, err := b.configFile(ctx, repositorySettingsPath, revision)
	if err != nil && !hasStatus(err, http.StatusNotFound) {
		return Policy{}, fmt.Errorf("work_queue: read %s at default revision: %w", repositorySettingsPath, err)
	}
	settings, err := ParseRepositorySettings(data)
	if err != nil {
		return Policy{}, err
	}
	policy := DefaultPolicy("", b.Remote)
	policy.Authorization = "aw"
	policy.Producers = map[string]ProducerRule{}
	pool := policy.Pools["default"]
	template := pool.Profiles["default"]
	template.Principal, template.Ref = "", revision
	pool.Profiles, err = b.approvedWorkerProfiles(ctx, revision, template)
	if err != nil {
		return Policy{}, err
	}
	pool.DefaultProfile = slices.Min(sortedSettingsKeys(pool.Profiles))
	policy.Pools["default"] = pool
	policy, err = settings.Apply(policy)
	if err != nil {
		return Policy{}, err
	}
	if err := ValidatePolicy(policy); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (b Branch) configuredWorkerSources(ctx context.Context, revision string) ([]configWorkerSourceEntry, error) {
	directory := strings.TrimSuffix(workerWorkflowDirectory, "/")
	var entries []configWorkerSourceEntry
	if err := b.request(ctx, http.MethodGet, "contents/"+directory+"?ref="+url.QueryEscape(revision), nil, &entries); err != nil {
		return nil, queueError("policy_missing", "cannot discover approved AW worker sources at the default revision: %v", err)
	}
	if len(entries) >= 1000 {
		return nil, queueError("policy_missing", "AW workflow directory exceeds the complete GitHub contents listing limit; cannot verify all approved workers")
	}
	slices.SortFunc(entries, func(left, right configWorkerSourceEntry) int {
		return strings.Compare(left.Path, right.Path)
	})
	return entries, nil
}

func (b Branch) approvedWorkerProfiles(ctx context.Context, revision string, template WorkerProfile) (map[string]WorkerProfile, error) {
	entries, err := b.configuredWorkerSources(ctx, revision)
	if err != nil {
		return nil, err
	}
	profiles := map[string]WorkerProfile{}
	for _, entry := range entries {
		if entry.Type != "file" || path.Dir(entry.Path) != strings.TrimSuffix(workerWorkflowDirectory, "/") || !strings.HasSuffix(entry.Path, ".md") {
			continue
		}
		content, err := b.configFile(ctx, entry.Path, revision)
		if err != nil {
			return nil, queueError("policy_missing", "cannot read AW worker source %s: %v", entry.Path, err)
		}
		approved, err := configSourceIsWorker(content)
		if err != nil {
			return nil, queueError("policy_missing", "cannot parse AW workflow source %s: %v", entry.Path, err)
		}
		if !approved {
			continue
		}
		name := strings.TrimSuffix(path.Base(entry.Path), ".md")
		profile := template
		profile.Workflow = workerWorkflowDirectory + name + ".lock.yml"
		compiled, compiledErr := b.configFile(ctx, profile.Workflow, revision)
		if compiledErr != nil && !deploymentRouteUnavailable(compiledErr) {
			return nil, compiledErr
		}
		if compiledErr == nil {
			if contract, contractErr := compiledWorkerContract(compiled); contractErr == nil {
				profile.LogicalContract = contract
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		profiles[name] = profile
	}
	if len(profiles) == 0 {
		return nil, queueError("policy_missing", "no approved AW worker at the default revision; declare tools.work-queue.worker: true in a .github/workflows/*.md workflow and compile its .lock.yml")
	}
	if len(profiles) > 256 {
		return nil, queueError("policy_invalid", "default revision approves more than 256 AW worker profiles")
	}
	return profiles, nil
}

func configSourceIsWorker(content []byte) (bool, error) {
	frontmatter, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || frontmatter == nil {
		return false, err
	}
	tools, ok := frontmatter.Frontmatter["tools"].(map[string]any)
	if !ok {
		return false, nil
	}
	queue, ok := tools["work-queue"].(map[string]any)
	if !ok {
		return false, nil
	}
	worker, ok := queue["worker"].(bool)
	return ok && worker, nil
}

func (b Branch) verifyWorkerRoutes(ctx context.Context, policy Policy) error {
	if err := ValidatePolicy(policy); err != nil {
		return err
	}
	if policy.Authorization == "aw" {
		return nil
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

func (b Branch) verifySubmissionWorkerRoutes(ctx context.Context, policy Policy, request Request) error {
	var parameters SubmitParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return err
	}
	assignments := make([]Assignment, 0, len(parameters.Nodes))
	for _, node := range parameters.Nodes {
		assignments = append(assignments, Assignment{Pool: node.Pool, WorkerProfile: node.WorkerProfile})
	}
	return b.verifyAssignmentWorkerRoutes(ctx, policy, assignments)
}

func (b Branch) verifyAssignmentWorkerRoutes(ctx context.Context, policy Policy, assignments []Assignment) error {
	seen := identitySet{}
	for _, assignment := range assignments {
		pool, ok := policy.Pools[assignment.Pool]
		if !ok {
			return queueError("pool_invalid", "selected worker pool is not approved")
		}
		profile, ok := pool.Profiles[assignment.WorkerProfile]
		if !ok {
			return queueError("profile_invalid", "selected worker profile is not approved")
		}
		key := profile.Workflow + "\n" + profile.Ref
		if seen.contains(key) {
			continue
		}
		if err := b.verifyWorkerRoute(ctx, profile); err != nil {
			return err
		}
		seen.add(key)
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
		if !hasStatus(err, http.StatusNotFound) {
			return err
		}
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
	if profile.LogicalContract != "" {
		contract, err := compiledWorkerContract(content)
		if err != nil || contract != profile.LogicalContract {
			return queueError("policy_missing", "approved immutable worker route must carry its compiler-derived logical contract")
		}
	}
	return b.verifyRegisteredWorker(ctx, profile)
}

func (b Branch) verifyRegisteredWorker(ctx context.Context, profile WorkerProfile) error {
	var registered struct {
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if err := b.request(ctx, http.MethodGet, "actions/workflows/"+path.Base(profile.Workflow), nil, &registered); err != nil {
		if !hasStatus(err, http.StatusNotFound) {
			return err
		}
		return queueError("policy_missing", "approved worker route registration cannot be verified: %v", err)
	}
	if registered.Path != profile.Workflow || registered.State != "active" {
		return queueError("policy_missing", "approved worker route must be active at its exact registered path")
	}
	return nil
}

func (b Branch) initialSubmissionCommit(ctx context.Context, actor Actor, request Request) (QueueCommit, error) {
	if request.Kind != "submit" || (actor.Role != "producer" && actor.Role != "dispatcher") {
		return QueueCommit{}, queueError("queue_missing", "submit Work to bootstrap an absent queue; standalone Policy seeding is unsupported")
	}
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
	if policy.Authorization == "aw" {
		if err := b.verifySubmissionWorkerRoutes(ctx, *policy, request); err != nil {
			return QueueCommit{}, err
		}
	} else if err := b.verifyWorkerRoutes(ctx, *policy); err != nil {
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
