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
	"slices"
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
	if err != nil {
		return nil, snapshot, err
	}
	if err := b.verifyCheckpointHistory(ctx, snapshot.head, transactions, 0); err != nil {
		return nil, snapshot, err
	}
	return transactions, snapshot, nil
}

func (b Branch) verifyCheckpointHistory(ctx context.Context, head string, commits []QueueCommit, depth int) error {
	if len(commits) == 0 || !isCheckpoint(commits[0]) {
		return nil
	}
	invalid := func() error {
		return queueError("checkpoint_invalid", "checkpoint does not match its Git parent history")
	}
	if depth >= 64 {
		return invalid()
	}
	var checkpoint CheckpointOperation
	if err := json.Unmarshal(commits[0].Operations[0], &checkpoint); err != nil {
		return invalid()
	}
	current := head
	for {
		var gitCommit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		if err := b.request(ctx, http.MethodGet, "git/commits/"+current, nil, &gitCommit); err != nil {
			return err
		}
		if len(gitCommit.Parents) != 1 || gitCommit.Parents[0].SHA == "" || gitCommit.Parents[0].SHA == current {
			return invalid()
		}
		parent := gitCommit.Parents[0].SHA
		var parentCommit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		}
		if err := b.request(ctx, http.MethodGet, "git/commits/"+parent, nil, &parentCommit); err != nil {
			return err
		}
		if parentCommit.Tree.SHA == "" {
			return invalid()
		}
		if parentCommit.Tree.SHA == gitCommit.Tree.SHA {
			current = parent
			continue
		}
		previous, _, err := b.readLog(ctx, parentCommit.Tree.SHA)
		if err != nil {
			return err
		}
		if len(previous) > 0 && isCheckpoint(previous[0]) && previous[0].ID == commits[0].ID {
			current = parent
			continue
		}
		data, encodeErr := Serialize(previous)
		if parent != checkpoint.PriorGitSHA || len(previous) == 0 || encodeErr != nil || hashBytes(data) != checkpoint.HistorySHA256 {
			return invalid()
		}
		ordered, err := causalOrder(previous)
		if err != nil || ordered[len(ordered)-1].ID != checkpoint.PriorTip {
			return invalid()
		}
		reconstructed, err := CompactCheckpoint(previous, parent, commits[0].Actor, commits[0].At)
		if err != nil {
			return invalid()
		}
		var expected CheckpointOperation
		if json.Unmarshal(reconstructed[0].Operations[0], &expected) != nil ||
			expected.StateSHA256 != checkpoint.StateSHA256 {
			return invalid()
		}
		return b.verifyCheckpointHistory(ctx, parent, previous, depth+1)
	}
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
	if err := b.verifyWorkerRoutes(ctx, policy); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func extending(previous, current []QueueCommit) bool {
	oldOrder, err := causalOrder(previous)
	if err != nil {
		return false
	}
	newOrder, err := causalOrder(current)
	if err != nil {
		return false
	}
	if len(newOrder) > 0 && isCheckpoint(newOrder[0]) {
		var op CheckpointOperation
		data, encodeErr := Serialize(oldOrder)
		if err := json.Unmarshal(newOrder[0].Operations[0], &op); err == nil &&
			encodeErr == nil && op.HistorySHA256 == hashBytes(data) &&
			op.PriorTip == oldOrder[len(oldOrder)-1].ID {
			reconstructed, err := CompactCheckpoint(oldOrder, op.PriorGitSHA, newOrder[0].Actor, newOrder[0].At)
			if err != nil {
				return false
			}
			var expected CheckpointOperation
			return json.Unmarshal(reconstructed[0].Operations[0], &expected) == nil &&
				expected.StateSHA256 == op.StateSHA256
		}
	}
	if len(newOrder) < len(oldOrder) {
		return false
	}
	return slices.EqualFunc(oldOrder, newOrder[:len(oldOrder)], func(old, current QueueCommit) bool {
		return sameJSON(old, current)
	})
}

// Publish retains only authenticated logical intent across CAS retries. It
// discards selections, handles, charges and clocks and recovers uncertain ref
// acknowledgments by stable request identity before constructing another grant.
func (b Branch) Publish(ctx context.Context, actor Actor, request Request) (Publication, error) {
	b, err := b.authenticatePublication(ctx, actor, request)
	if err != nil {
		return Publication{}, err
	}
	var last []QueueCommit
	var pendingErr error
	for attempt := range maxRetries {
		prefix, err := b.readPublicationPrefix(ctx, actor, request, last)
		if err != nil {
			return Publication{}, err
		}
		candidate, err := b.preparePublicationCandidate(ctx, actor, request, prefix, pendingErr)
		if err != nil {
			return Publication{}, err
		}
		if candidate.settled {
			return candidate.publication, nil
		}
		_, err = b.publish(ctx, prefix.snapshot, candidate.commits)
		if err == nil {
			candidate.publication.Changed = true
			return candidate.publication, nil
		}
		if ctx.Err() != nil {
			return Publication{}, ctx.Err()
		}
		// A lost ref response may be an accepted request, not a failed grant.
		pendingErr = err
		if prefix.snapshot.head != "" {
			last = prefix.commits
		}
		if attempt == maxRetries-1 {
			return b.recoverPublicationAcknowledgment(ctx, actor, request, err)
		}
		if err := waitForPublicationRetry(ctx, attempt); err != nil {
			return Publication{}, err
		}
	}
	return Publication{}, errors.New("queue publication exhausted retries")
}

func (b Branch) authenticatePublication(ctx context.Context, actor Actor, request Request) (Branch, error) {
	b, err := b.withClient()
	if err != nil {
		return b, err
	}
	authenticated, err := b.Authenticate(ctx, actor.Role)
	if err != nil || !sameJSON(authenticated, actor) {
		if err != nil {
			return b, err
		}
		return b, queueError("actor_unauthorized", "request actor differs from authenticated operator")
	}
	b.Remote = authenticated.Repository
	if err := validateRequestOrigin(actor, request); err != nil {
		return b, err
	}
	if err := validateRequestRole(actor, request.Kind); err != nil {
		return b, err
	}
	return b, nil
}

type publicationPrefix struct {
	commits  []QueueCommit
	snapshot branchSnapshot
	initial  *QueueCommit
	state    Projection
}

func (b Branch) readPublicationPrefix(ctx context.Context, actor Actor, request Request, previous []QueueCommit) (publicationPrefix, error) {
	commits, snapshot, err := b.read(ctx)
	if err != nil {
		return publicationPrefix{}, err
	}
	if len(previous) > 0 && (snapshot.head == "" || !extending(previous, commits)) {
		return publicationPrefix{}, queueError("ledger_nonextending", "queue history was deleted or rewritten during publication")
	}
	var initial *QueueCommit
	if snapshot.head == "" {
		genesis, genuine, err := b.initialPolicyCommit(ctx, actor, request)
		if err != nil {
			return publicationPrefix{}, err
		}
		commits = []QueueCommit{genesis}
		if genuine {
			initial = &genesis
		}
	}
	state, err := Replay(commits)
	if err != nil {
		return publicationPrefix{}, err
	}
	return publicationPrefix{commits: commits, snapshot: snapshot, initial: initial, state: state}, nil
}

type publicationCandidate struct {
	commits     []QueueCommit
	publication Publication
	settled     bool
}

func (b Branch) preparePublicationCandidate(ctx context.Context, actor Actor, request Request, prefix publicationPrefix, pendingErr error) (publicationCandidate, error) {
	if existing, ok := prefix.state.Requests[request.ID]; ok && prefix.initial == nil {
		if existing.Request.Fingerprint != request.Fingerprint || !sameJSON(existing.Actor, actor) {
			return publicationCandidate{}, queueError("request_reuse", "request identity has different accepted meaning")
		}
		_, commit, decision, err := BuildCandidate(prefix.commits, actor, request, time.Now().UnixMilli())
		return publicationCandidate{publication: Publication{Commit: commit, Decision: decision}, settled: true}, err
	}
	if err := validateRequestRole(actor, request.Kind); err != nil {
		return publicationCandidate{}, err
	}
	if pendingErr != nil && !hasStatus(pendingErr, http.StatusConflict) &&
		!hasStatus(pendingErr, http.StatusUnprocessableEntity) &&
		(hasStatus(pendingErr, http.StatusUnauthorized) || hasStatus(pendingErr, http.StatusForbidden)) {
		return publicationCandidate{}, pendingErr
	}
	if prefix.initial == nil {
		if err := b.verifyRequestEvidence(ctx, prefix.state, actor, request); err != nil {
			return publicationCandidate{}, err
		}
	}
	var observations []Observation
	if request.Kind == "dispatch_next" {
		var params DispatchParameters
		if err := json.Unmarshal(request.Parameters, &params); err != nil {
			return publicationCandidate{}, err
		}
		var err error
		observations, err = b.refreshForDispatch(ctx, prefix.state, params.Pool, request.ID)
		if err != nil {
			return publicationCandidate{}, err
		}
	}
	var candidate publicationCandidate
	var err error
	if prefix.initial != nil {
		candidate.commits, candidate.publication.Commit = prefix.commits, prefix.initial
		candidate.publication.Decision = Decision{Tip: prefix.state.Tip, Operations: prefix.initial.Operations, Assignments: []Assignment{}}
	} else {
		candidate.commits, candidate.publication.Commit, candidate.publication.Decision, err =
			buildCandidateWithObservations(prefix.commits, actor, request, time.Now().UnixMilli(), observations)
	}
	if err != nil {
		return publicationCandidate{}, err
	}
	candidate.settled = candidate.publication.Commit == nil
	return candidate, nil
}

func (b Branch) recoverPublicationAcknowledgment(ctx context.Context, actor Actor, request Request, pendingErr error) (Publication, error) {
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
	return Publication{}, fmt.Errorf("queue publication unresolved after %d attempts: %w", maxRetries, pendingErr)
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
