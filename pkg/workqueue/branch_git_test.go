package workqueue

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func disposableQueueGit(t *testing.T, remote string) *queueGit {
	t.Helper()
	t.Setenv("TMPDIR", ".")
	q, err := newQueueGit(context.Background(), remote, DefaultBranch, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(q.dir) })
	// Production allows only HTTPS. Disposable tests opt into local Git remotes,
	// without changing process configuration or contacting GitHub.
	q.env = append(q.env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=protocol.file.allow", "GIT_CONFIG_VALUE_0=always")
	return q
}

func TestQueueGitCleanupReportsSanitizedFailure(t *testing.T) {
	if os.Getenv("GH_AW_TEST_CLEANUP_CHILD") == "1" {
		cleanupQueueGit("invalid\x00private-cleanup-sentinel")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestQueueGitCleanupReportsSanitizedFailure$")
	command.Env = append(os.Environ(), "GH_AW_TEST_CLEANUP_CHILD=1")
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "queue_git_cleanup_failed") || strings.Contains(string(data), "private-cleanup-sentinel") {
		t.Fatalf("cleanup failure was absent or exposed the raw path: %s", data)
	}
}

func TestQueueGitExplicitHost(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_ENTERPRISE_TOKEN", "host-specific-test-token")
	t.Setenv("TMPDIR", ".")
	branch := Branch{Remote: "owner/repo", Name: DefaultBranch, Host: "github.enterprise.example"}
	branch, cleanup, err := branch.withLedger(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if branch.ledger.remote != "https://github.enterprise.example/owner/repo.git" {
		t.Fatalf("queue ledger used a different host: %s", branch.ledger.remote)
	}
	branch.Host, branch.ledger = "invalid/host", nil
	if _, _, err := branch.withLedger(context.Background()); err == nil || err.Error() != "invalid queue Git host" {
		t.Fatalf("invalid explicit queue host was accepted: %v", err)
	}
}

func TestQueueGitBoundsObjectOutputAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	output := &queueGitOutput{limit: 4, cancel: cancel}
	if n, err := output.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatalf("exact output limit rejected: %d %v", n, err)
	}
	if _, err := output.Write([]byte("5")); !errors.Is(err, errQueueGitOutputLimit) ||
		!output.exceeded || output.buffer.Len() != 4 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("oversize output was not canceled and bounded: %v", err)
	}
	q := disposableQueueGit(t, "")
	tree := queueGitRun(t, q, nil, "mktree")
	head := queueGitRun(t, q, []byte(strings.Repeat("x", queueGitMetadataBytes+1)), "commit-tree", tree)
	if _, _, err := q.commit(context.Background(), head); !errors.Is(err, errQueueGitOutputLimit) {
		t.Fatalf("oversize untrusted commit metadata accepted: %v", err)
	}
	content := []byte(strings.Repeat("b", queueGitMetadataBytes+1))
	blob := queueGitRun(t, q, content, "hash-object", "-w", "--stdin")
	if raw, err := q.run(context.Background(), nil, "cat-file", "blob", blob); err != nil || len(raw) != len(content) {
		t.Fatalf("blob incorrectly used the smaller metadata limit: %v", err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	start := time.Now()
	if _, err := q.run(canceled, nil, "status"); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("Git command ignored caller cancellation: %v", err)
	}
	if queueGitCommandTimeout > 120*time.Second {
		t.Fatal("Git command deadline exceeds 120 seconds")
	}
}

func TestQueueGitStoreOutsideReadOnlyWorkingDirectory(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	working := disposableQueueGit(t, "")
	t.Setenv("TMPDIR", root)
	t.Chdir(working.dir)
	if err := os.Chmod(working.dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(working.dir, 0700) }()
	store, err := newQueueGit(context.Background(), "", DefaultBranch, "")
	if err != nil {
		t.Fatalf("Git store depended on a writable checkout: %v", err)
	}
	defer func() { _ = os.RemoveAll(store.dir) }()
	if filepath.Dir(store.dir) != root || strings.HasPrefix(store.dir, working.dir+string(os.PathSeparator)) {
		t.Fatal("Git object store was created inside the caller's checkout")
	}
}

func TestQueueGitRejectsIncompleteHistory(t *testing.T) {
	for _, defect := range []string{"shallow", "missing-parent"} {
		t.Run(defect, func(t *testing.T) {
			b, _ := gitQueueBranch(t)
			history := testGenesis(t, nil)
			parent := writeRawQueueGit(t, b, history)
			head := writeRawQueueGit(t, b, history, parent)
			if _, err := b.Read(context.Background()); err != nil {
				t.Fatal(err)
			}
			if defect == "shallow" {
				if err := os.WriteFile(filepath.Join(b.ledger.dir, "shallow"), []byte(head+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, dir := range []string{b.ledger.dir, b.ledger.remote} {
					if err := os.Remove(filepath.Join(dir, "objects", parent[:2], parent[2:])); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := b.Read(context.Background()); err == nil {
				t.Fatal("incomplete Git ancestry was accepted")
			}
		})
	}
}

func TestQueueGitResolveFiltersSuffixMatchingRefs(t *testing.T) {
	b, _ := gitQueueBranch(t)
	remote := &queueGit{dir: b.ledger.remote, env: b.ledger.env}
	tree := queueGitRun(t, remote, nil, "mktree")
	head := queueGitRun(t, remote, []byte("Suffix ref fixture\n"), "commit-tree", tree)
	queueGitRun(t, remote, nil, "update-ref", "refs/tags/refs/heads/work-queue", head)
	queueGitRun(t, remote, nil, "update-ref", "refs/heads/x/refs/heads/work-queue", head)

	if _, err := b.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "queue_missing") {
		t.Fatalf("suffix-matching refs were treated as the queue: %v", err)
	}

	history := testGenesis(t, nil)
	data, err := Serialize(history)
	if err != nil {
		t.Fatal(err)
	}
	blob := queueGitRun(t, remote, data, "hash-object", "-w", "--stdin")
	tree = queueGitRun(t, remote, []byte("100644 blob "+blob+"\t"+FileName+"\x00"), "mktree", "-z")
	head = queueGitRun(t, remote, []byte("Exact queue ref fixture\n"), "commit-tree", tree)
	queueGitRun(t, remote, nil, "update-ref", b.ledger.ref, head)
	if commits, err := b.Read(context.Background()); err != nil || !sameJSON(commits, history) {
		t.Fatalf("exact queue ref was hidden by suffix-matching refs: %v", err)
	}
}

func TestLedgerSessionReusesStoreAndRefreshesQueueHead(t *testing.T) {
	b, _ := gitQueueBranch(t)
	session, err := b.OpenLedgerSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	store := session.branch.ledger.dir

	initial := testGenesis(t, nil)
	head := writeRawQueueGit(t, b, initial)
	if commits, err := session.Read(context.Background()); err != nil || !sameJSON(commits, initial) {
		t.Fatalf("initial session read: %v", err)
	}
	next := testSubmit(t, initial, "submit-work", testNode(t, initial, "session-work"))
	writeRawQueueGit(t, b, next, head)
	if commits, err := session.Read(context.Background()); err != nil || !sameJSON(commits, next) {
		t.Fatalf("session did not refresh the remote head: %v", err)
	}
	if session.branch.ledger.dir != store {
		t.Fatal("session replaced its isolated object store")
	}
	session.Close()
	if _, err := session.Read(context.Background()); err == nil {
		t.Fatal("closed ledger session remained usable")
	}
}

func gitQueueBranch(t *testing.T) (Branch, *queueAPI) {
	t.Helper()
	remote := disposableQueueGit(t, "")
	queueGitRun(t, remote, nil, "symbolic-ref", "HEAD", "refs/heads/main")
	queueGitRun(t, remote, nil, "config", "uploadpack.allowFilter", "true")
	queueGitRun(t, remote, nil, "config", "uploadpack.allowReachableSHA1InWant", "true")
	q := disposableQueueGit(t, remote.dir)
	branch, mock := newQueueAPI(t)
	branch.ledgerRequestFn, branch.ledger = nil, q
	return branch, mock
}

func queueGitRun(t *testing.T, q *queueGit, input []byte, operation string, args ...string) string {
	t.Helper()
	raw, err := q.run(context.Background(), input, operation, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

const queueGitHTTPTestToken = "synthetic-queue-http-token"

type queueGitHTTPFixture struct {
	remote   string
	deny     atomic.Bool
	rejected atomic.Bool
	mu       sync.Mutex
	wants    [][]string
}

func authenticatedQueueGitFixture(t *testing.T, source *queueGit) *queueGitHTTPFixture {
	t.Helper()
	fixture := &queueGitHTTPFixture{}
	prefix := "/" + filepath.Base(source.dir)
	want := regexp.MustCompile(`want ([a-f0-9]{40}|[a-f0-9]{64})\n`)
	authorization := "basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+queueGitHTTPTestToken))
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		advertisement := r.URL.Path == prefix+"/info/refs" && r.Method == http.MethodGet && r.URL.Query().Get("service") == "git-upload-pack"
		upload := r.URL.Path == prefix+"/git-upload-pack" && r.Method == http.MethodPost && r.URL.RawQuery == ""
		if !advertisement && !upload {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != authorization || fixture.deny.Load() {
			fixture.rejected.Store(true)
			w.Header().Set("WWW-Authenticate", `Basic realm="queue-fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, queueGitMetadataBytes+1))
		if err != nil || len(body) > queueGitMetadataBytes {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodPost {
			var wants []string
			for _, match := range want.FindAllSubmatch(body, -1) {
				wants = append(wants, string(match[1]))
			}
			fixture.mu.Lock()
			fixture.wants = append(fixture.wants, wants)
			fixture.mu.Unlock()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "git", "http-backend")
		command.Dir = source.dir
		command.Env = []string{
			"PATH=" + os.Getenv("PATH"), "TMPDIR=" + source.dir, "TEMP=" + source.dir, "TMP=" + source.dir,
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0",
			"GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "LC_ALL=C",
			"GIT_PROJECT_ROOT=" + filepath.Dir(source.dir), "GIT_HTTP_EXPORT_ALL=1",
			"PATH_INFO=" + r.URL.Path, "REQUEST_METHOD=" + r.Method, "QUERY_STRING=" + r.URL.RawQuery,
			"CONTENT_TYPE=" + r.Header.Get("Content-Type"), "CONTENT_LENGTH=" + strconv.Itoa(len(body)),
			"HTTP_GIT_PROTOCOL=" + r.Header.Get("Git-Protocol"), "SERVER_PROTOCOL=HTTP/1.1", "REMOTE_ADDR=127.0.0.1",
		}
		command.Stdin = bytes.NewReader(body)
		output := &queueGitOutput{limit: maxParseBytes, cancel: cancel}
		command.Stdout, command.Stderr = output, io.Discard
		command.WaitDelay = time.Second
		if err := command.Run(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		headers, response, ok := bytes.Cut(output.buffer.Bytes(), []byte("\r\n\r\n"))
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		status := http.StatusOK
		for line := range strings.SplitSeq(string(headers), "\r\n") {
			name, value, ok := strings.Cut(line, ":")
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			value = strings.TrimSpace(value)
			if name == "Status" {
				code, _, _ := strings.Cut(value, " ")
				status, err = strconv.Atoi(code)
				if err != nil || status < 100 || status > 599 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			} else {
				w.Header().Set(name, value)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write(response)
	}))
	server.Config.ReadHeaderTimeout = 10 * time.Second
	server.Config.ReadTimeout = 10 * time.Second
	server.Config.WriteTimeout = 15 * time.Second
	server.Start()
	t.Cleanup(server.Close)
	fixture.remote = server.URL + prefix
	return fixture
}

func coldAuthenticatedQueueGit(t *testing.T, fixture *queueGitHTTPFixture) *queueGit {
	t.Helper()
	q, err := newQueueGit(context.Background(), fixture.remote, DefaultBranch, queueGitHTTPTestToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(q.dir) })
	// Only this local fixture opts into HTTP; production remains HTTPS-only.
	q.env = append(q.env, "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_1=protocol.http.allow", "GIT_CONFIG_VALUE_1=always",
		"http_proxy=", "https_proxy=", "all_proxy=", "HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=", "NO_PROXY=127.0.0.1")
	return q
}

func (fixture *queueGitHTTPFixture) exactFetches(sha string) int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	var count int
	for _, wants := range fixture.wants {
		if len(wants) == 1 && wants[0] == sha {
			count++
		}
	}
	return count
}

func TestQueueGitColdFetchExcludesAssetsAndVerifiesCheckpoints(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(strconv.FormatBool(corrupt), func(t *testing.T) {
			ctx := context.Background()
			b, mock := gitQueueBranch(t)
			history := testGenesis(t, nil)
			prior := writeRawQueueGit(t, b, history)
			first, err := CompactCheckpoint(history, prior, testActor("administrator"), 3001)
			if err != nil {
				t.Fatal(err)
			}
			parents := []string{prior}
			if corrupt {
				parents = nil
			}
			firstHead := writeRawQueueGit(t, b, first, parents...)
			second, err := CompactCheckpoint(first, firstHead, testActor("administrator"), 4001)
			if err != nil {
				t.Fatal(err)
			}
			secondHead := writeRawQueueGit(t, b, second, firstHead)
			source := &queueGit{dir: b.ledger.remote, env: b.ledger.env}
			asset := queueGitRun(t, source, bytes.Repeat([]byte("a"), 1024*1024), "hash-object", "-w", "--stdin")
			claims := queueGitRun(t, source, []byte("100644 blob "+asset+"\t"+asset+".bin\x00"), "mktree", "-z")
			contents, err := Serialize(second)
			if err != nil {
				t.Fatal(err)
			}
			log := queueGitRun(t, source, contents, "hash-object", "-w", "--stdin")
			tree := queueGitRun(t, source, []byte("100644 blob "+log+"\t"+FileName+"\x00"+"040000 tree "+claims+"\tclaims\x00"), "mktree", "-z")
			head := queueGitRun(t, source, []byte("Asset fixture\n"), "commit-tree", tree, "-p", secondHead)
			queueGitRun(t, source, nil, "update-ref", b.ledger.ref, head, secondHead)
			fixture := authenticatedQueueGitFixture(t, source)
			b.ledger = coldAuthenticatedQueueGit(t, fixture)
			b.ledger.reuseValidated = true
			got, err := b.Read(ctx)
			if corrupt {
				if err == nil || !strings.Contains(err.Error(), "checkpoint_invalid") {
					t.Fatalf("corrupt oldest checkpoint accepted: %v", err)
				}
				return
			}
			if err != nil || !sameJSON(got, second) {
				t.Fatalf("cold filtered checkpoint read: %v", err)
			}
			if fixture.rejected.Load() || fixture.exactFetches(log) != 1 {
				t.Fatal("required ledger blob was not fetched exactly once with scoped authentication")
			}
			for _, transactions := range [][]QueueCommit{history, first} {
				data, err := Serialize(transactions)
				if err != nil {
					t.Fatal(err)
				}
				blob := queueGitRun(t, source, data, "hash-object", "--stdin")
				if fixture.exactFetches(blob) != 1 {
					t.Fatal("checkpoint ancestry ledger blob was not fetched by exact identity")
				}
			}
			if _, err := b.ledger.run(ctx, nil, "cat-file", "-e", asset+"^{blob}"); err == nil {
				t.Fatal("cold queue read downloaded an unrelated asset blob")
			}
			if count := queueGitRun(t, b.ledger, nil, "rev-list", "--count", head); count != "4" {
				t.Fatalf("filtered read lost causal commit history: %s", count)
			}
			if _, err := b.Read(ctx); err != nil || fixture.exactFetches(log) != 1 {
				t.Fatalf("warm read refetched immutable ledger blobs: %v", err)
			}
			if _, err := b.ledger.request(ctx, http.MethodGet, "git/blobs/"+asset, nil); err != nil || fixture.exactFetches(asset) != 1 {
				t.Fatalf("exact asset fetch did not remain authenticated: %v", err)
			}
			if mock.ledgerCalls != 0 {
				t.Fatal("filtered ledger used an API fallback")
			}
		})
	}
}

func TestQueueGitRejectsUnsupportedFilteringBeforeObjectFetch(t *testing.T) {
	b, _ := gitQueueBranch(t)
	prior := writeRawQueueGit(t, b, testGenesis(t, nil))
	source := &queueGit{dir: b.ledger.remote, env: b.ledger.env}
	queueGitRun(t, source, nil, "config", "uploadpack.allowFilter", "false")
	fixture := authenticatedQueueGitFixture(t, source)
	b.ledger = coldAuthenticatedQueueGit(t, fixture)
	if _, err := b.Read(context.Background()); !errors.Is(err, errQueueGitFilterUnsupported) {
		t.Fatalf("unsupported filtering was not rejected: %v", err)
	}
	if fixture.exactFetches(prior) != 0 || fixture.rejected.Load() {
		t.Fatal("unsupported filtering downloaded objects or used incorrect authentication")
	}
	if refs := queueGitRun(t, b.ledger, nil, "for-each-ref", "--format=%(refname)"); refs != "" {
		t.Fatal("unsupported filtering published a local queue ref")
	}
	queueGitRun(t, source, nil, "update-ref", "-d", "refs/heads/work-queue", prior)
	if _, err := b.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "queue_missing") {
		t.Fatalf("authenticated queue absence was not distinguished: %v", err)
	}
}

func TestQueueGitDeniedExactBlobFetchIsNotQueueAbsence(t *testing.T) {
	ctx := context.Background()
	b, _ := gitQueueBranch(t)
	head := writeRawQueueGit(t, b, testGenesis(t, nil))
	source := &queueGit{dir: b.ledger.remote, env: b.ledger.env}
	fixture := authenticatedQueueGitFixture(t, source)
	b.ledger = coldAuthenticatedQueueGit(t, fixture)
	if _, err := b.ledger.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	tree, _, err := b.ledger.commit(ctx, head)
	if err != nil {
		t.Fatal(err)
	}
	entries := queueGitRun(t, b.ledger, nil, "ls-tree", tree, "--", FileName)
	blob := strings.Fields(entries)[2]
	if _, err := b.ledger.run(ctx, nil, "cat-file", "-e", blob+"^{blob}"); err == nil {
		t.Fatal("initial filtered fetch already downloaded the ledger blob")
	}
	fixture.deny.Store(true)
	if _, err := b.ledger.request(ctx, http.MethodGet, "git/blobs/"+blob, nil); err == nil || hasStatus(err, http.StatusNotFound) {
		t.Fatalf("denied promised blob became queue absence: %v", err)
	}
	if !fixture.rejected.Load() {
		t.Fatal("denied on-demand fetch did not reauthenticate with the fixture")
	}
}

// Write arbitrary wire history so rejection tests do not accidentally validate
// the proposed history before the reader under test sees it.
func writeRawQueueGit(t *testing.T, b Branch, commits []QueueCommit, parents ...string) string {
	t.Helper()
	data, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	blob := queueGitRun(t, b.ledger, data, "hash-object", "-w", "--stdin")
	tree := queueGitRun(t, b.ledger, []byte("100644 blob "+blob+"\t"+FileName+"\x00"), "mktree", "-z")
	args := []string{tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	head := queueGitRun(t, b.ledger, []byte("Queue regression fixture\n"), "commit-tree", args...)
	queueGitRun(t, b.ledger, nil, "push", "--force", "--no-verify", b.ledger.remote, head+":"+b.ledger.ref)
	return head
}

func TestQueueGitLedgerPublicationCompactionAndCAS(t *testing.T) {
	ctx := context.Background()
	b, mock := gitQueueBranch(t)
	defer func() {
		if mock.ledgerCalls != 0 {
			t.Fatalf("ledger issued %d API calls", mock.ledgerCalls)
		}
	}()
	if _, err := b.Read(ctx); err == nil || !strings.Contains(err.Error(), "queue_missing") {
		t.Fatalf("missing branch not distinguished from an unreadable remote: %v", err)
	}
	source := &queueGit{dir: b.ledger.remote, env: b.ledger.env}
	sourceBlob := queueGitRun(t, source, []byte("unrelated source history"), "hash-object", "-w", "--stdin")
	sourceTree := queueGitRun(t, source, []byte("100644 blob "+sourceBlob+"\tREADME.md\x00"), "mktree", "-z")
	sourceHead := queueGitRun(t, source, []byte("Unrelated default branch\n"), "commit-tree", sourceTree)
	queueGitRun(t, source, nil, "update-ref", "refs/heads/main", sourceHead)
	history := testGenesis(t, nil)
	history = testSubmit(t, history, "submit-work", testNode(t, history, "work"))
	if _, err := b.publish(ctx, branchSnapshot{}, history); err != nil {
		t.Fatal(err)
	}
	current, stale, err := b.read(ctx)
	if err != nil || !sameJSON(current, history) {
		t.Fatalf("Git-only read: %v", err)
	}
	noAPI := b
	noAPI.client = nil
	if _, err := noAPI.Read(ctx); err != nil {
		t.Fatalf("ledger read required an API client: %v", err)
	}
	request, err := NewRequest("pause", "control", testActor("administrator"), OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{"kind": "Control", "control": "admission_paused", "value": true, "reason": "operator"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := b.Publish(ctx, testActor("administrator"), request)
	if err != nil || !first.Changed {
		t.Fatalf("append: %+v %v", first, err)
	}
	head := queueGitRun(t, b.ledger, nil, "ls-remote", b.ledger.remote, b.ledger.ref)
	retry, err := b.Publish(ctx, testActor("administrator"), request)
	if err != nil || retry.Changed || head != queueGitRun(t, b.ledger, nil, "ls-remote", b.ledger.remote, b.ledger.ref) {
		t.Fatalf("stable request republished: %+v %v", retry, err)
	}
	// A competing writer uses the old snapshot in a separate object store.
	competitor := disposableQueueGit(t, b.ledger.remote)
	other := b
	other.ledger = competitor
	if _, _, err := other.read(ctx); err != nil {
		t.Fatal(err)
	}
	competitor.head = stale.head
	if _, err := other.publish(ctx, stale, current); !hasStatus(err, http.StatusConflict) {
		t.Fatalf("stale exact lease was not a CAS conflict: %v", err)
	}
	lostRequest, err := NewRequest("lost-ack", "control", testActor("administrator"), OperationsParameters{
		Operations: []Operation{mustOp(t, map[string]any{"kind": "Control", "control": "admission_paused", "value": false, "reason": "operator"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := b
	lost := true
	b.ledgerRequestFn = func(ctx context.Context, method, endpoint string, body, response any) error {
		err := transport.ledgerRequest(ctx, method, endpoint, body, response)
		if err == nil && method == http.MethodPatch && lost {
			lost = false
			return errors.New("simulated lost Git push acknowledgment")
		}
		return err
	}
	recovered, err := b.Publish(ctx, testActor("administrator"), lostRequest)
	if err != nil || lost || recovered.Changed {
		t.Fatalf("accepted Git push acknowledgment was not recovered: %+v %v", recovered, err)
	}
	b.ledgerRequestFn = nil
	if _, err := b.Compact(ctx); err != nil {
		t.Fatal(err)
	}
	checkpoint, _, err := b.read(ctx)
	if err != nil || !isCheckpoint(checkpoint[0]) {
		t.Fatalf("checkpoint history verification: %v", err)
	}
	tail := testOperations(t, checkpoint, testActor("administrator"), "resume", "control",
		mustOp(t, map[string]any{"kind": "Control", "control": "admission_paused", "value": false, "reason": "operator"}))
	_, snapshot, err := b.read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.publish(ctx, snapshot, tail); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Compact(ctx); err != nil {
		t.Fatal(err)
	}
	nested, err := b.Read(ctx)
	if err != nil || !isCheckpoint(nested[0]) {
		t.Fatalf("nested Git checkpoint ancestry: %v", err)
	}
	// The only fetched ref is the queue, and the object store is never shallow.
	if refs := queueGitRun(t, b.ledger, nil, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/ledger" {
		t.Fatalf("unrelated refs fetched: %s", refs)
	}
	if _, err := os.Stat(filepath.Join(b.ledger.dir, "shallow")); !os.IsNotExist(err) {
		t.Fatalf("shallow history: %v", err)
	}
	if _, err := b.ledger.run(ctx, nil, "cat-file", "-e", sourceHead); err == nil {
		t.Fatal("unrelated default branch history was fetched")
	}
}

func TestQueueGitRootPublicationPreservesHostingDefault(t *testing.T) {
	ctx := context.Background()
	b, _ := gitQueueBranch(t)
	history := testGenesis(t, nil)
	if _, err := b.publish(ctx, branchSnapshot{}, history); err == nil {
		t.Fatal("first queue publication initialized a branchless repository")
	}
	if refs := queueGitRun(t, b.ledger, nil, "ls-remote", "--heads", b.ledger.remote); refs != "" {
		t.Fatalf("failed bootstrap wrote a remote branch: %s", refs)
	}
	remote := &queueGit{dir: b.ledger.remote, env: b.ledger.env}
	tree := queueGitRun(t, remote, nil, "mktree")
	main := queueGitRun(t, remote, []byte("Hosting bootstrap\n"), "commit-tree", tree)
	queueGitRun(t, remote, nil, "update-ref", "refs/heads/main", main)
	queueGitRun(t, remote, nil, "update-ref", "refs/heads/foo/HEAD", main)
	if _, err := b.publish(ctx, branchSnapshot{}, history); err != nil {
		t.Fatalf("suffix-matching ref prevented valid default branch publication: %v", err)
	}
	if head := queueGitRun(t, remote, nil, "symbolic-ref", "HEAD"); head != "refs/heads/main" {
		t.Fatalf("queue initialization changed the hosting default: %s", head)
	}
	queueGitRun(t, remote, nil, "symbolic-ref", "HEAD", b.ledger.ref)
	if err := b.ledger.requireHostedDefault(ctx); err == nil {
		t.Fatal("queue branch accepted as the hosting bootstrap default")
	}
}

func TestQueueGitProductionStoreCredentialsAndCleanup(t *testing.T) {
	t.Setenv("TMPDIR", ".")
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "synthetic-queue-token")
	b, cleanup, err := (Branch{Remote: testRepository, Name: DefaultBranch}).withLedger(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if b.ledger.remote != "https://github.com/"+testRepository+".git" || b.client != nil {
		t.Fatal("ledger setup depended on REST or a checkout")
	}
	config, err := os.ReadFile(filepath.Join(b.ledger.dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "token") || strings.Contains(string(config), "extraheader") ||
		strings.Contains(b.ledger.remote, "token") {
		t.Fatal("credential persisted outside the Git process environment")
	}
	cleanup()
	if _, err := os.Stat(b.ledger.dir); !os.IsNotExist(err) {
		t.Fatalf("disposable ledger object store was not cleaned: %v", err)
	}
}

func TestQueueGitCheckpointRecoveryAndInvalidAncestry(t *testing.T) {
	for _, defect := range []string{"", "merge-parent", "missing-parent", "history", "nested"} {
		t.Run(defect, func(t *testing.T) {
			ctx := context.Background()
			b, mock := gitQueueBranch(t)
			defer func() {
				if mock.ledgerCalls != 0 {
					t.Fatalf("checkpoint verification issued %d ledger API calls", mock.ledgerCalls)
				}
			}()
			history := testGenesis(t, deploymentPolicy)
			history = testSubmit(t, history, "submit-test-work", testNode(t, history, "test-work"))
			history, _ = testGrant(t, history, "reserve-test-work", 1, 1)
			prior := writeRawQueueGit(t, b, history)
			checkpointParent := prior
			if defect == "history" {
				checkpointParent = strings.Repeat("f", 40)
			}
			checkpoint, err := CompactCheckpoint(history, checkpointParent, testActor("administrator"), 3001)
			if err != nil {
				t.Fatal(err)
			}
			parent := writeRawQueueGit(t, b, checkpoint, prior)
			valid := deployTestWorker(t, checkpoint, "upgrade-worker", "default", strings.Repeat("d", 40), strings.Repeat("a", 64), true)
			state, err := Replay(valid)
			if err != nil {
				t.Fatal(err)
			}
			deployments, err := canonicalValue(state.Deployments)
			if err != nil {
				t.Fatal(err)
			}
			damaged := append([]QueueCommit(nil), valid...)
			damaged[0], err = withCheckpointDeployments(damaged[0], deployments)
			if err != nil {
				t.Fatal(err)
			}
			parents := []string{parent}
			switch defect {
			case "merge-parent":
				parents = append(parents, prior)
			case "missing-parent":
				parents = nil
			}
			head := writeRawQueueGit(t, b, damaged, parents...)
			expected := valid
			if defect == "nested" {
				expected, err = CompactCheckpoint(valid, head, testActor("administrator"), 4001)
				if err != nil {
					t.Fatal(err)
				}
				writeRawQueueGit(t, b, expected, head)
			}
			got, err := b.Read(ctx)
			if defect != "" && defect != "nested" {
				if err == nil || !strings.Contains(err.Error(), "checkpoint_invalid") {
					t.Fatalf("invalid checkpoint accepted: %v", err)
				}
				return
			}
			if err != nil || !sameJSON(got, expected) {
				t.Fatalf("Git history recovery: %v", err)
			}
			_, snapshot, err := b.read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.publish(ctx, snapshot, expected); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Read(ctx); err != nil {
				t.Fatalf("healed append did not retain verified ancestry: %v", err)
			}
		})
	}
}

func TestQueueGitFailsClosedAndSanitizesEnvironment(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "alias.ls-remote")
	t.Setenv("GIT_CONFIG_VALUE_0", "!false")
	t.Setenv("GIT_TRACE", filepath.Join(".", "forbidden-queue-trace"))
	t.Setenv("SSH_ASKPASS", "must-not-run")
	b, _ := gitQueueBranch(t)
	// An unreadable repository must never be mistaken for a missing queue.
	b.ledger.remote = filepath.Join(b.ledger.dir, "does-not-exist")
	if _, err := b.Read(context.Background()); err == nil || hasStatus(err, http.StatusNotFound) {
		t.Fatalf("repository failure became absence: %v", err)
	}
	if _, err := os.Stat("forbidden-queue-trace"); !os.IsNotExist(err) {
		t.Fatalf("inherited Git tracing enabled: %v", err)
	}
	for _, value := range b.ledger.env {
		if strings.HasPrefix(value, "GIT_TRACE=") || strings.HasPrefix(value, "SSH_ASKPASS=") {
			t.Fatal("inherited Git trace or askpass environment")
		}
	}
	// Even with a matching exact lease, the low-level publisher rejects a
	// non-fast-forward object before invoking push.
	q := b.ledger
	genesis := testGenesis(t, nil)
	q.remote = disposableQueueGit(t, "").dir
	head := writeRawQueueGit(t, b, genesis)
	q.head = head
	blob := queueGitRun(t, q, []byte("unrelated"), "hash-object", "-w", "--stdin")
	tree := queueGitRun(t, q, []byte("100644 blob "+blob+"\t"+FileName+"\x00"), "mktree", "-z")
	unrelated := queueGitRun(t, q, []byte("unrelated\n"), "commit-tree", tree)
	_, err := q.push(context.Background(), map[string]string{"sha": unrelated})
	if err == nil || !strings.Contains(err.Error(), "linear fast-forward") {
		t.Fatalf("non-fast-forward lease publication: %v", err)
	}
}
