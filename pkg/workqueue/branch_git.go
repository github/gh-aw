package workqueue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/github/gh-aw/pkg/console"
)

// queueGit adapts the ledger object operations to Git plumbing. API clients are
// reserved for external identity, policy and worker evidence, never ledger data.
type queueGit struct {
	dir, remote, ref, head string
	env                    []string
	reuseValidated         bool
	filterSupported        bool
}

// LedgerSession reuses one isolated object store for a scoped series of ledger
// operations. Each operation still resolves the remote queue ref before use.
type LedgerSession struct {
	mu      sync.Mutex
	branch  Branch
	cleanup func()
	closed  bool
}

const queueGitCommandTimeout = 120 * time.Second
const queueGitMetadataBytes = 1 << 20

var errQueueGitOutputLimit = errors.New("queue Git output exceeds size limit")
var errQueueGitFilterUnsupported = errors.New("queue Git server must support blob filtering; unfiltered asset downloads are not permitted")
var queueGitFilterCapability = regexp.MustCompile(`(?m)\bpacket:\s+\S+< fetch=(?:[^\r\n]* )?filter(?: |\r?$)|\bpacket:\s+\S+< [a-f0-9]{40,64} [^\r\n]*\\0[^\r\n]* filter(?: |\r?$)`)

type queueGitOutput struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (output *queueGitOutput) Write(data []byte) (int, error) {
	if len(data) > output.limit-output.buffer.Len() {
		output.exceeded = true
		output.cancel()
		return 0, errQueueGitOutputLimit
	}
	return output.buffer.Write(data)
}

func (b Branch) withLedger(ctx context.Context) (Branch, func(), error) {
	if err := b.validate(); err != nil {
		return b, nil, err
	}
	if b.ledgerRequestFn != nil || b.ledger != nil {
		return b, func() {}, nil
	}
	host := b.Host
	if host == "" {
		host, _ = auth.DefaultHost()
	}
	if !resourceHostPattern.MatchString(strings.ToLower(host)) {
		return b, nil, errors.New("invalid queue Git host")
	}
	token, _ := auth.TokenForHost(host)
	if token == "" {
		return b, nil, errors.New("queue Git transport requires credentials for the selected GitHub host")
	}
	remote := (&url.URL{Scheme: "https", Host: host, Path: b.Remote + ".git"}).String()
	ledger, err := newQueueGit(ctx, remote, b.Name, token)
	if err != nil {
		return b, nil, err
	}
	b.ledger = ledger
	return b, func() { cleanupQueueGit(ledger.dir) }, nil
}

func cleanupQueueGit(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		console.PrintWarningMessage("queue_git_cleanup_failed: temporary Git object cleanup failed; check temporary-directory permissions")
	}
}

// OpenLedgerSession creates a scoped session with an isolated Git object store.
func (b Branch) OpenLedgerSession(ctx context.Context) (*LedgerSession, error) {
	b, cleanup, err := b.withLedger(ctx)
	if err != nil {
		return nil, err
	}
	if b.ledgerRequestFn == nil && b.ledger != nil {
		b.ledger.reuseValidated = true
	}
	return &LedgerSession{branch: b, cleanup: cleanup}, nil
}

func (b Branch) withClientLedgerSession(ctx context.Context) (Branch, *LedgerSession, error) {
	b, err := b.withClient()
	if err != nil {
		return b, nil, err
	}
	session, err := b.OpenLedgerSession(ctx)
	return b, session, err
}

func (b Branch) withAuthenticatedLedgerSession(ctx context.Context, role string) (Branch, Actor, *LedgerSession, error) {
	return b.withAuthenticatedLedgerSessionAccess(ctx, role, false)
}

func (b Branch) withAuthenticatedLedgerSessionAccess(ctx context.Context, role string, allowMaintain bool) (Branch, Actor, *LedgerSession, error) {
	b, err := b.withClient()
	if err != nil {
		return b, Actor{}, nil, err
	}
	actor, err := b.authenticate(ctx, role, allowMaintain)
	if err != nil {
		return b, Actor{}, nil, err
	}
	session, err := b.OpenLedgerSession(ctx)
	return b, actor, session, err
}

// Read refreshes the queue head and reads its validated ledger.
func (s *LedgerSession) Read(ctx context.Context) ([]QueueCommit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("work queue ledger session is closed")
	}
	return s.branch.Read(ctx)
}

// Publish publishes a checked request using the session's object store.
func (s *LedgerSession) Publish(ctx context.Context, actor Actor, request Request) (Publication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Publication{}, errors.New("work queue ledger session is closed")
	}
	return s.branch.Publish(ctx, actor, request)
}

// Compact compacts the current queue using the session's object store.
func (s *LedgerSession) Compact(ctx context.Context) (Compaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Compaction{}, errors.New("work queue ledger session is closed")
	}
	return s.branch.Compact(ctx)
}

// Close removes the session's temporary Git object store.
func (s *LedgerSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.cleanup()
	}
}

func newQueueGit(ctx context.Context, remote, branch, token string) (*queueGit, error) {
	// Do not reuse a checkout: it may be shallow, sparse, credential-free, or
	// contain replacements, alternates, hooks and unrelated source history.
	dir, err := os.MkdirTemp("", "gh-aw-workqueue-")
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		cleanupQueueGit(dir)
		return nil, err
	}
	dir = absolute
	q := &queueGit{dir: dir, remote: remote, ref: "refs/heads/" + branch}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if (!strings.HasPrefix(key, "GIT_") || key == "GIT_EXEC_PATH") && key != "SSH_ASKPASS" {
			q.env = append(q.env, value)
		}
	}
	q.env = append(q.env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1",
		"GIT_AUTHOR_NAME=GitHub Work Queue", "GIT_AUTHOR_EMAIL=work-queue@github.com",
		"GIT_COMMITTER_NAME=GitHub Work Queue", "GIT_COMMITTER_EMAIL=work-queue@github.com")
	if token != "" {
		// The credential exists only in this child environment, never argv, a
		// URL, repository config, or an error containing Git's stderr.
		header := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		q.env = append(q.env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http."+remote+".extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+header)
	}
	if _, err := q.run(ctx, nil, "init", "--bare", "--template=", dir); err != nil {
		cleanupQueueGit(dir)
		return nil, err
	}
	return q, nil
}

func (q *queueGit) run(ctx context.Context, input []byte, operation string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, queueGitCommandTimeout)
	defer cancel()
	if filepath.IsAbs(q.remote) && (operation == "fetch" || operation == "ls-remote" || operation == "push") {
		// Git rejects absolute filesystem paths as filtered promisor remote names.
		remotePath := filepath.ToSlash(q.remote)
		if !strings.HasPrefix(remotePath, "/") {
			remotePath = "/" + remotePath
		}
		commandRemote := (&url.URL{Scheme: "file", Path: remotePath}).String()
		args = slices.Clone(args)
		for index, arg := range args {
			if arg == q.remote {
				args[index] = commandRemote
			}
		}
	}
	limit := queueGitMetadataBytes
	if operation == "cat-file" && slices.Contains(args, "blob") {
		limit = maxParseBytes
	}
	output := &queueGitOutput{limit: limit, cancel: cancel}
	diagnostics := &queueGitOutput{limit: queueGitMetadataBytes, cancel: cancel}
	base := []string{"--no-replace-objects", "-c", "core.hooksPath=" + os.DevNull,
		"-c", "credential.helper=", "-c", "core.askPass=", "-c", "http.followRedirects=false",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "fetch.fsckObjects=true",
		"-c", "maintenance.auto=false", "-c", "gc.auto=0", operation}
	command := exec.CommandContext(ctx, "git", append(base, args...)...)
	command.Dir, command.Env = q.dir, append(slices.Clone(q.env), "LC_ALL=C")
	probeFilter := operation == "ls-remote" && slices.Contains(args, "--refs")
	if probeFilter {
		// Capture protocol advertisement packets only, never HTTP credentials.
		command.Env = append(command.Env, "GIT_TRACE_PACKET=1")
	}
	command.Stdin = bytes.NewReader(input)
	command.Stdout, command.Stderr = output, diagnostics
	command.WaitDelay = time.Second
	err := command.Run()
	if output.exceeded || diagnostics.exceeded {
		return nil, errQueueGitOutputLimit
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return output.buffer.Bytes(), fmt.Errorf("queue Git %s failed: %w", operation, err)
	}
	if probeFilter {
		q.filterSupported = queueGitFilterCapability.Match(diagnostics.buffer.Bytes())
	}
	if operation == "fetch" && slices.Contains(args, "--filter=blob:none") &&
		bytes.Contains(diagnostics.buffer.Bytes(), []byte("filtering not recognized by server, ignoring")) {
		return nil, errQueueGitFilterUnsupported
	}
	return output.buffer.Bytes(), nil
}

func (b Branch) ledgerRequest(ctx context.Context, method, endpoint string, body, response any) error {
	if b.ledgerRequestFn != nil {
		return b.ledgerRequestFn(ctx, method, endpoint, body, response)
	}
	if b.ledger == nil {
		return errors.New("queue Git transport is not initialized")
	}
	value, err := b.ledger.request(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, response)
}

func (q *queueGit) resolve(ctx context.Context) (any, error) {
	sha, err := q.advertisedHead(ctx)
	if err != nil {
		return nil, err
	}
	if !q.filterSupported {
		return nil, errQueueGitFilterUnsupported
	}
	if q.reuseValidated && sha == q.head {
		if _, err := os.Stat(filepath.Join(q.dir, "shallow")); !os.IsNotExist(err) {
			return nil, errors.New("queue Git history must not be shallow")
		}
		return gitReference{Ref: q.ref, Object: struct {
			SHA string `json:"sha"`
		}{q.head}}, nil
	}
	previousHead := q.head
	if _, err := q.run(ctx, nil, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head",
		"--filter=blob:none", q.remote, "+"+q.ref+":refs/heads/ledger"); err != nil {
		return nil, err
	}
	output, err := q.run(ctx, nil, "rev-parse", "--verify", "refs/heads/ledger^{commit}")
	if err != nil {
		return nil, err
	}
	newHead := strings.TrimSpace(string(output))
	if err := q.verifyFetchedHistory(ctx, previousHead, newHead); err != nil {
		return nil, err
	}
	q.head = newHead
	return gitReference{Ref: q.ref, Object: struct {
		SHA string `json:"sha"`
	}{q.head}}, nil
}

func (q *queueGit) advertisedHead(ctx context.Context) (string, error) {
	q.filterSupported = false
	output, err := q.run(ctx, nil, "ls-remote", "--exit-code", "--refs", q.remote, q.ref)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 2 {
			q.head = ""
			return "", &api.HTTPError{StatusCode: http.StatusNotFound, Message: "queue branch does not exist"}
		}
		return "", err
	}
	var sha string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		value, ref, ok := strings.Cut(line, "\t")
		if !ok {
			return "", errInvalidHeadReference
		}
		if ref != q.ref {
			continue
		}
		if sha != "" || !revisionPattern.MatchString(value) || strings.Trim(value, "0") == "" {
			return "", errInvalidHeadReference
		}
		sha = value
	}
	if sha == "" {
		q.head = ""
		return "", &api.HTTPError{StatusCode: http.StatusNotFound, Message: "queue branch does not exist"}
	}
	if !revisionPattern.MatchString(sha) {
		return "", errInvalidHeadReference
	}
	return sha, nil
}

func (q *queueGit) verifyFetchedHistory(ctx context.Context, previousHead, newHead string) error {
	if _, err := os.Stat(filepath.Join(q.dir, "shallow")); !os.IsNotExist(err) {
		return errors.New("queue Git history must not be shallow")
	}
	if previousHead != "" {
		if _, err := q.run(ctx, nil, "merge-base", "--is-ancestor", previousHead, newHead); err == nil {
			if _, err := q.run(ctx, nil, "rev-list", "--quiet", "--objects", "--missing=allow-promisor", newHead, "^"+previousHead); err != nil {
				return err
			}
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return err
			}
			if _, err := q.run(ctx, nil, "fsck", "--connectivity-only", "--no-dangling", "--no-reflogs", newHead); err != nil {
				return err
			}
		}
	} else if _, err := q.run(ctx, nil, "fsck", "--connectivity-only", "--no-dangling", "--no-reflogs", newHead); err != nil {
		return err
	}
	// Promisor connectivity permits omitted blobs, not missing causal commits
	// or trees. Traverse the complete metadata graph without hydrating blobs.
	_, err := q.run(ctx, nil, "rev-list", "--quiet", "--objects", "--filter=blob:none", "--missing=error", newHead)
	return err
}

func (q *queueGit) commit(ctx context.Context, sha string) (string, []string, error) {
	if !revisionPattern.MatchString(sha) {
		return "", nil, errors.New("invalid queue Git revision")
	}
	raw, err := q.run(ctx, nil, "cat-file", "commit", sha)
	if err != nil {
		return "", nil, err
	}
	var tree string
	var parents []string
	headers, _, complete := strings.Cut(string(raw), "\n\n")
	if !complete {
		return "", nil, errors.New("queue Git commit headers are incomplete")
	}
	for line := range strings.SplitSeq(headers, "\n") {
		if value, ok := strings.CutPrefix(line, "tree "); ok {
			tree = value
		}
		if value, ok := strings.CutPrefix(line, "parent "); ok {
			parents = append(parents, value)
		}
	}
	if !revisionPattern.MatchString(tree) {
		return "", nil, errors.New("queue Git commit has invalid tree")
	}
	for _, parent := range parents {
		if !revisionPattern.MatchString(parent) {
			return "", nil, errors.New("queue Git commit has invalid parent")
		}
	}
	return tree, parents, nil
}

func (q *queueGit) request(ctx context.Context, method, endpoint string, body any) (any, error) {
	switch {
	case method == http.MethodGet && endpoint == "git/ref/heads/"+strings.TrimPrefix(q.ref, "refs/heads/"):
		return q.resolve(ctx)
	case method == http.MethodGet && strings.HasPrefix(endpoint, "git/commits/"):
		tree, parents, err := q.commit(ctx, strings.TrimPrefix(endpoint, "git/commits/"))
		values := []map[string]string{}
		for _, parent := range parents {
			values = append(values, map[string]string{"sha": parent})
		}
		return map[string]any{"tree": map[string]string{"sha": tree}, "parents": values}, err
	case method == http.MethodGet && strings.HasPrefix(endpoint, "git/trees/"):
		return q.readTree(ctx, strings.TrimPrefix(endpoint, "git/trees/"))
	case method == http.MethodGet && strings.HasPrefix(endpoint, "git/blobs/"):
		sha := strings.TrimPrefix(endpoint, "git/blobs/")
		if !revisionPattern.MatchString(sha) {
			return nil, errors.New("invalid queue Git blob")
		}
		if _, err := q.run(ctx, nil, "cat-file", "-e", sha+"^{blob}"); err != nil {
			if !q.filterSupported {
				return nil, errQueueGitFilterUnsupported
			}
			if _, err := q.run(ctx, nil, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head",
				"--filter=blob:none", q.remote, sha); err != nil {
				return nil, err
			}
		}
		data, err := q.run(ctx, nil, "cat-file", "blob", sha)
		return map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)}, err
	case method == http.MethodPost && endpoint == "git/trees":
		return q.writeTree(ctx, body)
	case method == http.MethodPost && endpoint == "git/commits":
		return q.createCommit(ctx, body)
	case method == http.MethodPost && endpoint == "git/refs" ||
		method == http.MethodPatch && endpoint == "git/refs/heads/"+strings.TrimPrefix(q.ref, "refs/heads/"):
		return q.push(ctx, body)
	}
	return nil, errors.New("unsupported queue Git operation")
}

func (q *queueGit) readTree(ctx context.Context, sha string) (any, error) {
	if !revisionPattern.MatchString(sha) {
		return nil, errors.New("invalid queue Git tree")
	}
	raw, err := q.run(ctx, nil, "ls-tree", "-z", sha)
	if err != nil {
		return nil, err
	}
	entries := []map[string]string{}
	if len(raw) == 0 {
		return map[string]any{"tree": entries}, nil
	}
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\x00"), "\x00") {
		meta, name, hasName := strings.Cut(entry, "\t")
		mode, remaining, hasMode := strings.Cut(meta, " ")
		kind, object, hasType := strings.Cut(remaining, " ")
		if !hasName || !hasMode || !hasType || !revisionPattern.MatchString(object) {
			return nil, errors.New("invalid queue Git tree entry")
		}
		entries = append(entries, map[string]string{"path": name, "mode": mode, "type": kind, "sha": object})
	}
	return map[string]any{"tree": entries}, nil
}

func (q *queueGit) publicationParents() []string {
	if q.head == "" {
		return nil
	}
	return []string{q.head}
}

func (q *queueGit) createCommit(ctx context.Context, body any) (any, error) {
	var value struct {
		Tree    string
		Message string
		Parents []string
	}
	if err := decodeGitBody(body, &value); err != nil {
		return nil, err
	}
	if !revisionPattern.MatchString(value.Tree) || !slices.Equal(value.Parents, q.publicationParents()) {
		return nil, errors.New("queue publication must extend the observed Git head")
	}
	args := []string{value.Tree}
	for _, parent := range value.Parents {
		args = append(args, "-p", parent)
	}
	raw, err := q.run(ctx, []byte(value.Message+"\n"), "commit-tree", args...)
	return map[string]string{"sha": strings.TrimSpace(string(raw))}, err
}

func decodeGitBody(body, value any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func (q *queueGit) writeTree(ctx context.Context, body any) (any, error) {
	var value struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct{ Path, Mode, Type, Content string }
	}
	if err := decodeGitBody(body, &value); err != nil {
		return nil, err
	}
	if len(value.Tree) != 1 {
		return nil, errors.New("invalid queue Git tree update")
	}
	var content string
	for _, entry := range value.Tree {
		if entry.Path != FileName || entry.Mode != "100644" || entry.Type != "blob" {
			return nil, errors.New("invalid queue Git tree update")
		}
		content = entry.Content
	}
	return q.writeLogTree(ctx, value.BaseTree, content)
}

func (q *queueGit) writeLogTree(ctx context.Context, baseTree, content string) (any, error) {
	var entries []byte
	if baseTree != "" {
		if !revisionPattern.MatchString(baseTree) {
			return nil, errors.New("invalid queue Git base tree")
		}
		raw, err := q.run(ctx, nil, "ls-tree", "-z", baseTree)
		if err != nil {
			return nil, err
		}
		for entry := range strings.SplitSeq(string(raw), "\x00") {
			_, name, ok := strings.Cut(entry, "\t")
			if ok && name != FileName {
				entries = append(entries, (entry + "\x00")...)
			}
		}
	}
	blob, err := q.run(ctx, []byte(content), "hash-object", "-w", "--stdin")
	if err != nil {
		return nil, err
	}
	entries = append(entries, ("100644 blob " + strings.TrimSpace(string(blob)) + "\t" + FileName + "\x00")...)
	tree, err := q.run(ctx, entries, "mktree", "-z")
	return map[string]string{"sha": strings.TrimSpace(string(tree))}, err
}

func (q *queueGit) push(ctx context.Context, body any) (any, error) {
	var value struct{ SHA, Ref string }
	if err := decodeGitBody(body, &value); err != nil {
		return nil, err
	}
	if value.Ref != "" && value.Ref != q.ref {
		return nil, errors.New("invalid queue publication ref")
	}
	_, parents, err := q.commit(ctx, value.SHA)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(parents, q.publicationParents()) {
		return nil, errors.New("queue publication must be a linear fast-forward")
	}
	if q.head == "" {
		if err := q.requireHostedDefault(ctx); err != nil {
			return nil, err
		}
	}
	output, err := q.run(ctx, nil, "push", "--porcelain", "--no-verify", "--recurse-submodules=no",
		"--force-with-lease="+q.ref+":"+q.head, q.remote, value.SHA+":"+q.ref)
	if err != nil {
		if strings.Contains(string(output), "[rejected] (stale info)") ||
			strings.Contains(string(output), "[rejected] (fetch first)") ||
			strings.Contains(string(output), "[rejected] (non-fast-forward)") {
			return nil, &api.HTTPError{StatusCode: http.StatusConflict, Message: "queue Git compare-and-swap conflict"}
		}

		// Let the existing stable-request retry path distinguish accepted writes
		// from rejected leases by fetching and replaying authoritative history.
		return nil, err
	}
	if _, err := q.run(ctx, nil, "update-ref", "refs/heads/ledger", value.SHA); err != nil {
		return nil, err
	}
	q.head = value.SHA
	return map[string]any{}, nil
}

func (q *queueGit) requireHostedDefault(ctx context.Context) error {
	output, err := q.run(ctx, nil, "ls-remote", "--symref", q.remote, "HEAD")
	if err != nil {
		return err
	}
	if len(output) == 0 {
		return errors.New("queue publication requires an existing non-queue default branch")
	}
	var defaultRef, revision string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		value, name, ok := strings.Cut(line, "\t")
		if !ok {
			return errors.New("invalid remote Git HEAD advertisement")
		}
		if name != "HEAD" {
			continue
		}
		if ref, symbolic := strings.CutPrefix(value, "ref: "); symbolic {
			if defaultRef != "" {
				return errors.New("invalid remote Git HEAD advertisement")
			}
			defaultRef = ref
		} else {
			if revision != "" {
				return errors.New("invalid remote Git HEAD advertisement")
			}
			revision = value
		}
	}
	if !strings.HasPrefix(defaultRef, "refs/heads/") || defaultRef == q.ref ||
		!revisionPattern.MatchString(revision) || strings.Trim(revision, "0") == "" {
		return errors.New("queue publication requires an existing non-queue default branch")
	}
	return nil
}
