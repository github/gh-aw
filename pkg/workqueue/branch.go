package workqueue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/githubapi"
)

const maxRetries = 5

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Branch operates through GitHub Git APIs without a local checkout or Git executable.
type Branch struct {
	Remote              string
	Name                string
	DependencyClients   map[string]*api.RESTClient
	DeliveryVerifier    DeliveryVerifier
	RemediationVerifier RemediationVerifier
	deliveryFailures    map[string]string
	client              *api.RESTClient
}

func (b Branch) validate() error {
	if !repoPattern.MatchString(b.Remote) {
		return errors.New("repo must be owner/repo")
	}
	for part := range strings.SplitSeq(b.Remote, "/") {
		if part == "." || part == ".." {
			return errors.New("repo must be owner/repo")
		}
	}
	if !branchPattern.MatchString(b.Name) || strings.Contains(b.Name, "..") ||
		strings.Contains(b.Name, "//") || strings.HasSuffix(b.Name, ".") || strings.HasSuffix(b.Name, "/") {
		return errors.New("invalid queue branch")
	}
	for part := range strings.SplitSeq(b.Name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return errors.New("invalid queue branch")
		}
	}
	return nil
}

func (b Branch) withClient() (Branch, error) {
	if err := b.validate(); err != nil {
		return b, err
	}
	if b.client == nil {
		client, err := api.NewRESTClient(githubapi.ClientOptions("", ""))
		if err != nil {
			return b, err
		}
		b.client = client
	}
	return b, nil
}

func (b Branch) request(ctx context.Context, method, endpoint string, body, response any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	return b.client.DoWithContext(ctx, method, path.Join("repos", b.Remote, endpoint), bytes.NewReader(data), response)
}

func hasStatus(err error, status int) bool {
	var apiError *api.HTTPError
	return errors.As(err, &apiError) && apiError.StatusCode == status
}

type branchSnapshot struct {
	head string
	tree string
	data []byte
}

func (b Branch) read(ctx context.Context) ([]QueueCommit, branchSnapshot, error) {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := b.request(ctx, http.MethodGet, "git/ref/heads/"+b.Name, nil, &ref); err != nil {
		if hasStatus(err, http.StatusNotFound) {
			// A missing repository or missing read permission is not an empty queue.
			var repository any
			err = b.client.DoWithContext(ctx, http.MethodGet, "repos/"+b.Remote, nil, &repository)
			return nil, branchSnapshot{}, err
		}
		return nil, branchSnapshot{}, err
	}
	if ref.Ref != "refs/heads/"+b.Name || ref.Object.SHA == "" {
		return nil, branchSnapshot{}, errors.New("invalid queue branch reference")
	}
	snapshot := branchSnapshot{head: ref.Object.SHA}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := b.request(ctx, http.MethodGet, "git/commits/"+snapshot.head, nil, &commit); err != nil {
		return nil, snapshot, err
	}
	snapshot.tree = commit.Tree.SHA
	if snapshot.tree == "" {
		return nil, snapshot, errors.New("queue commit has no tree")
	}
	transactions, data, err := b.readLog(ctx, snapshot.tree)
	snapshot.data = data
	return transactions, snapshot, err
}

func (b Branch) readLog(ctx context.Context, treeSHA string) ([]QueueCommit, []byte, error) {
	var tree struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
	}
	if err := b.request(ctx, http.MethodGet, "git/trees/"+treeSHA, nil, &tree); err != nil {
		return nil, nil, err
	}
	if tree.Truncated {
		return nil, nil, errors.New("queue tree is truncated")
	}
	for _, entry := range tree.Tree {
		if entry.Path != FileName {
			continue
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") || entry.SHA == "" {
			return nil, nil, errors.New("queue log must be a regular file")
		}
		var blob struct {
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if err := b.request(ctx, http.MethodGet, "git/blobs/"+entry.SHA, nil, &blob); err != nil {
			return nil, nil, err
		}
		if blob.Encoding != "base64" {
			return nil, nil, errors.New("unsupported queue blob encoding")
		}
		data, err := base64.StdEncoding.DecodeString(blob.Content)
		if err != nil {
			return nil, nil, err
		}
		transactions, err := Parse(data)
		if err != nil {
			return nil, nil, err
		}
		for _, commit := range transactions {
			if !strings.EqualFold(commit.Actor.Repository, b.Remote) {
				return nil, nil, queueError("repository_scope_invalid", "ledger actor belongs to another repository")
			}
		}
		_, err = Replay(transactions)
		return transactions, data, err
	}
	return nil, nil, errors.New("queue branch is missing " + FileName)
}

func (b Branch) Read(ctx context.Context) ([]QueueCommit, error) {
	b, err := b.withClient()
	if err != nil {
		return nil, err
	}
	transactions, snapshot, err := b.read(ctx)
	if err == nil && snapshot.head == "" {
		return nil, queueError("queue_missing", "queue branch %s does not exist", b.Name)
	}
	return transactions, err
}

func (b Branch) publish(ctx context.Context, snapshot branchSnapshot, next []QueueCommit) (bool, error) {
	if _, err := Replay(next); err != nil {
		return false, err
	}
	data, err := Serialize(next)
	if err != nil {
		return false, err
	}
	treeBody := map[string]any{
		"tree": []map[string]string{{"path": FileName, "mode": "100644", "type": "blob", "content": string(data)}},
	}
	parents := []string{}
	if snapshot.head != "" {
		treeBody["base_tree"] = snapshot.tree
		parents = append(parents, snapshot.head)
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if err := b.request(ctx, http.MethodPost, "git/trees", treeBody, &tree); err != nil {
		return false, err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := b.request(ctx, http.MethodPost, "git/commits", map[string]any{
		"message": "Update work queue", "tree": tree.SHA, "parents": parents,
	}, &commit); err != nil {
		return false, err
	}
	if snapshot.head == "" {
		err = b.request(ctx, http.MethodPost, "git/refs", map[string]any{
			"ref": "refs/heads/" + b.Name, "sha": commit.SHA,
		}, new(any))
	} else {
		err = b.request(ctx, http.MethodPatch, "git/refs/heads/"+b.Name, map[string]any{
			"sha": commit.SHA, "force": false,
		}, new(any))
	}
	return hasStatus(err, http.StatusConflict) || hasStatus(err, http.StatusUnprocessableEntity), err
}

// Authenticate derives operator identity from the credential, not command flags,
// Git authors, submitted actor fields, or an untrusted Actions environment.
func (b Branch) Authenticate(ctx context.Context, role string) (Actor, error) {
	b, err := b.withClient()
	if err != nil {
		return Actor{}, err
	}
	if role != "administrator" && role != "producer" && role != "reconciler" {
		return Actor{}, queueError("actor_unauthorized", "native operator publisher cannot impersonate workflow roles")
	}
	var user struct {
		ID json.Number `json:"id"`
	}
	if err := b.client.DoWithContext(ctx, http.MethodGet, "user", nil, &user); err != nil {
		return Actor{}, err
	}
	var repository struct {
		FullName    string `json:"full_name"`
		Permissions struct {
			Push  bool `json:"push"`
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := b.client.DoWithContext(ctx, http.MethodGet, "repos/"+b.Remote, nil, &repository); err != nil {
		return Actor{}, err
	}
	if !decimalIdentity(user.ID.String()) || !strings.EqualFold(repository.FullName, b.Remote) ||
		!repository.Permissions.Push || role == "administrator" && !repository.Permissions.Admin {
		return Actor{}, queueError("actor_unauthorized", "authenticated repository writer/administrator permission is required")
	}
	return Actor{Role: role, Principal: user.ID.String(), Repository: repository.FullName}, nil
}

type Publication struct {
	Commit   *QueueCommit `json:"commit,omitempty"`
	Decision Decision     `json:"decision"`
	Changed  bool         `json:"changed"`
}

func (b Branch) defaultPolicy(ctx context.Context, principal string) (Policy, error) {
	var repository struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := b.request(ctx, http.MethodGet, "", nil, &repository); err != nil {
		return Policy{}, err
	}
	if repository.DefaultBranch == "" {
		return Policy{}, queueError("policy_invalid", "new queue requires an immutable approved default worker revision")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := b.request(ctx, http.MethodGet, "git/ref/heads/"+repository.DefaultBranch, nil, &ref); err != nil {
		return Policy{}, err
	}
	if !revisionPattern.MatchString(ref.Object.SHA) {
		return Policy{}, queueError("policy_invalid", "default worker revision is not immutable")
	}
	policy := DefaultPolicy(principal, b.Remote)
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Ref = ref.Object.SHA
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	return policy, nil
}

func extending(previous, current []QueueCommit) bool {
	oldOrder, err := causalOrder(previous)
	if err != nil {
		return false
	}
	newOrder, err := causalOrder(current)
	if err != nil || len(newOrder) < len(oldOrder) {
		return false
	}
	for i, old := range oldOrder {
		if !sameJSON(old, newOrder[i]) {
			return false
		}
	}
	return true
}

// Publish retains only authenticated logical intent across CAS retries. It
// discards selections, handles, charges and clocks and recovers uncertain ref
// acknowledgments by stable request identity before constructing another grant.
func (b Branch) Publish(ctx context.Context, actor Actor, request Request) (Publication, error) {
	b, err := b.withClient()
	if err != nil {
		return Publication{}, err
	}
	authenticated, err := b.Authenticate(ctx, actor.Role)
	if err != nil || !sameJSON(authenticated, actor) {
		if err != nil {
			return Publication{}, err
		}
		return Publication{}, queueError("actor_unauthorized", "request actor differs from authenticated operator")
	}
	b.Remote = authenticated.Repository
	if err := validateRequestOrigin(actor, request); err != nil {
		return Publication{}, err
	}
	var last []QueueCommit
	var pendingErr error
	for attempt := range maxRetries {
		transactions, snapshot, err := b.read(ctx)
		if err != nil {
			return Publication{}, err
		}
		if len(last) > 0 && (snapshot.head == "" || !extending(last, transactions)) {
			return Publication{}, queueError("ledger_nonextending", "queue history was deleted or rewritten during publication")
		}
		creating := snapshot.head == ""
		if creating {
			admin, err := b.Authenticate(ctx, "administrator")
			if err != nil {
				return Publication{}, queueError("policy_missing", "new queues require authenticated policy initialization: %v", err)
			}
			policy, err := b.defaultPolicy(ctx, admin.Principal)
			if err != nil {
				return Publication{}, err
			}
			genesis, err := Genesis(admin, policy,
				"init_"+hashBytes([]byte(request.ID)), "epoch_"+hashBytes([]byte(request.ID)), time.Now().UnixMilli())
			if err != nil {
				return Publication{}, err
			}
			transactions = []QueueCommit{genesis}
		}
		state, err := Replay(transactions)
		if err != nil {
			return Publication{}, err
		}
		if existing, ok := state.Requests[request.ID]; ok {
			if existing.Request.Fingerprint != request.Fingerprint || !sameJSON(existing.Actor, actor) {
				return Publication{}, queueError("request_reuse", "request identity has different accepted meaning")
			}
			_, commit, decision, err := BuildCandidate(transactions, actor, request, time.Now().UnixMilli())
			return Publication{Commit: commit, Decision: decision, Changed: false}, err
		}
		if err := validateRequestRole(actor, request.Kind); err != nil {
			return Publication{}, err
		}
		if pendingErr != nil && !hasStatus(pendingErr, http.StatusConflict) &&
			!hasStatus(pendingErr, http.StatusUnprocessableEntity) &&
			(hasStatus(pendingErr, http.StatusUnauthorized) || hasStatus(pendingErr, http.StatusForbidden)) {
			return Publication{}, pendingErr
		}
		if err := b.verifyRequestEvidence(ctx, state, actor, request); err != nil {
			return Publication{}, err
		}
		var observations []Observation
		if request.Kind == "dispatch_next" {
			var params DispatchParameters
			if err := json.Unmarshal(request.Parameters, &params); err != nil {
				return Publication{}, err
			}
			observations, err = b.refreshForDispatch(ctx, state, params.Pool, request.ID)
			if err != nil {
				return Publication{}, err
			}
		}
		next, commit, decision, err := buildCandidateWithObservations(transactions, actor, request, time.Now().UnixMilli(), observations)
		if err != nil {
			return Publication{}, err
		}
		if commit == nil {
			if len(observations) == 0 {
				return Publication{Decision: decision}, nil
			}
			operations := []Operation{}
			for _, observation := range observations {
				operations = append(operations, Op(observation))
			}
			evidence, _ := canonicalValue(operations)
			observationRequest, err := NewRequest("observe_"+hashBytes([]byte(request.ID+"\n"+state.Tip+"\n"+string(evidence))),
				"observe", actor, OperationsParameters{Operations: operations})
			if err != nil {
				return Publication{}, err
			}
			next, commit, _, err = BuildCandidate(transactions, actor, observationRequest, time.Now().UnixMilli())
			if err != nil {
				return Publication{}, err
			}
			// The read evidence is durable, but no-grant still does not consume
			// the original dispatch request or any service charge.
			decision.Tip = commit.ID
		}
		conflict, err := b.publish(ctx, snapshot, next)
		if err == nil {
			return Publication{Commit: commit, Decision: decision, Changed: true}, nil
		}
		if ctx.Err() != nil {
			return Publication{}, ctx.Err()
		}
		// Ref update may have succeeded even when the response was lost. Safe
		// reads find the stable request before any new candidate is published.
		pendingErr = err
		if !creating {
			last = transactions
		}
		if attempt == maxRetries-1 {
			current, _, readErr := b.read(ctx)
			if readErr == nil && len(current) > 0 {
				projection, replayErr := Replay(current)
				if replayErr == nil {
					if existing, ok := projection.Requests[request.ID]; ok &&
						existing.Request.Fingerprint == request.Fingerprint && sameJSON(existing.Actor, actor) {
						_, committed, decision, err := BuildCandidate(current, actor, request, time.Now().UnixMilli())
						return Publication{Commit: committed, Decision: decision}, err
					}
				}
			}
			return Publication{}, fmt.Errorf("queue publication unresolved after %d attempts: %w", maxRetries, err)
		}
		_ = conflict
		if err := waitForPublicationRetry(ctx, attempt); err != nil {
			return Publication{}, err
		}
	}

	return Publication{}, errors.New("queue publication exhausted retries")
}

func waitForPublicationRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(50*(1<<attempt)+rand.Intn(50)) * time.Millisecond
	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
