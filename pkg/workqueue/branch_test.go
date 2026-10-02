package workqueue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type queueCommit struct {
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}

type queueAPI struct {
	head       string
	logs       map[string]string
	bases      map[string]string
	commits    map[string]queueCommit
	refWrites  int
	failPath   string
	failStatus int
	conflicts  int
	mode       string
	truncated  bool
}

func newQueueAPI(t *testing.T) (Branch, *queueAPI) {
	t.Helper()
	mock := &queueAPI{
		logs: make(map[string]string), bases: make(map[string]string), commits: make(map[string]queueCommit), mode: "100644",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Error("request was not authenticated")
		}
		mock.serve(t, w, r)
	}))
	t.Cleanup(server.Close)
	client, err := api.NewRESTClient(api.ClientOptions{
		Host: "github.com", AuthToken: "test-token", Transport: queueTransport{server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	return Branch{Remote: "owner/repo", Name: DefaultBranch, client: client}, mock
}

type queueTransport struct{ url string }

func (transport queueTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme = "http"
	request.URL.Host = strings.TrimPrefix(transport.url, "http://")
	return http.DefaultTransport.RoundTrip(request)
}

func (mock *queueAPI) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/")
	w.Header().Set("Content-Type", "application/json")
	respond := func(value any) {
		t.Helper()
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}
	fail := func(status int) {
		w.WriteHeader(status)
		respond(map[string]string{"message": "API request rejected"})
	}
	if path == mock.failPath {
		fail(mock.failStatus)
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "git/ref/heads/"+DefaultBranch:
		if mock.head == "" {
			fail(http.StatusNotFound)
		} else {
			respond(map[string]any{"ref": "refs/heads/" + DefaultBranch, "object": map[string]string{"sha": mock.head}})
		}
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
		respond(map[string]string{"full_name": "owner/repo"})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
		commit, ok := mock.commits[strings.TrimPrefix(path, "git/commits/")]
		if !ok {
			t.Errorf("unknown commit: %s", path)
			fail(http.StatusNotFound)
			return
		}
		respond(map[string]any{"tree": map[string]string{"sha": commit.Tree}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/trees/"):
		tree := strings.TrimPrefix(path, "git/trees/")
		respond(map[string]any{"truncated": mock.truncated, "tree": []map[string]string{
			{"path": "unrelated.txt", "mode": "100644", "type": "blob", "sha": "unrelated"},
			{"path": FileName, "mode": mock.mode, "type": "blob", "sha": tree},
		}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/blobs/"):
		log := mock.logs[strings.TrimPrefix(path, "git/blobs/")]
		respond(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(log)) + "\n"})
	case r.Method == http.MethodPost && path == "git/trees":
		var body struct {
			BaseTree string `json:"base_tree"`
			Tree     []struct {
				Path    string `json:"path"`
				Mode    string `json:"mode"`
				Type    string `json:"type"`
				Content string `json:"content"`
			} `json:"tree"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			fail(http.StatusBadRequest)
			return
		}
		if len(body.Tree) != 1 {
			t.Errorf("expected only queue file to change: %+v", body)
			fail(http.StatusBadRequest)
			return
		}
		for _, entry := range body.Tree {
			if entry.Path != FileName || entry.Mode != "100644" || entry.Type != "blob" {
				t.Errorf("invalid queue tree entry: %+v", entry)
			}
			sha := fmt.Sprintf("tree-%d", len(mock.logs)+1)
			mock.logs[sha] = entry.Content
			mock.bases[sha] = body.BaseTree
			respond(map[string]string{"sha": sha})
		}
	case r.Method == http.MethodPost && path == "git/commits":
		var commit queueCommit
		if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
			t.Error(err)
			fail(http.StatusBadRequest)
			return
		}
		if commit.Parents == nil {
			t.Error("parents must be an array, including for orphan initialization")
		}
		for _, parent := range commit.Parents {
			if mock.bases[commit.Tree] != mock.commits[parent].Tree {
				t.Error("tree must preserve the snapshot parent's other files")
			}
		}
		sha := fmt.Sprintf("commit-%d", len(mock.commits)+1)
		mock.commits[sha] = commit
		respond(map[string]string{"sha": sha})
	case (r.Method == http.MethodPost && path == "git/refs") ||
		(r.Method == http.MethodPatch && path == "git/refs/heads/"+DefaultBranch):
		mock.refWrites++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			fail(http.StatusBadRequest)
			return
		}
		if mock.conflicts > 0 {
			mock.conflicts--
			fail(http.StatusUnprocessableEntity)
			return
		}
		sha, _ := body["sha"].(string)
		commit := mock.commits[sha]
		if r.Method == http.MethodPost {
			if body["ref"] != "refs/heads/"+DefaultBranch || len(commit.Parents) != 0 {
				t.Error("invalid orphan branch creation")
			}
			if mock.head != "" {
				fail(http.StatusUnprocessableEntity)
				return
			}
		} else {
			if body["force"] != false {
				t.Error("reference update must explicitly disable force")
			}
			if len(commit.Parents) != 1 {
				t.Error("reference update must have one parent")
			}
			for _, parent := range commit.Parents {
				if parent != mock.head {
					fail(http.StatusConflict)
					return
				}
				if mock.commits[parent].Tree == "" {
					t.Error("missing parent tree")
				}
			}
		}
		mock.head = sha
		respond(map[string]string{"ref": "refs/heads/" + DefaultBranch})
	default:
		t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
		fail(http.StatusNotFound)
	}
}

func appendTransaction(tx Transaction) func([]Transaction) ([]Transaction, bool, error) {
	return func(current []Transaction) ([]Transaction, bool, error) { return Apply(current, tx) }
}

func TestBranchWithoutCheckoutAndConflictRetry(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	branch, mock := newQueueAPI(t)
	ctx := context.Background()
	work, a, b := fixture(t)
	otherWorkID, otherPayload, err := WorkID([]byte(`{"task":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	otherWork := Transaction{Kind: "Work", WorkID: otherWorkID, Work: otherPayload}
	if current, err := branch.Read(ctx); err != nil || len(current) != 0 {
		t.Fatalf("missing branch read: %v, %v", current, err)
	}
	if _, changed, err := branch.Update(ctx, appendTransaction(work)); err != nil || !changed {
		t.Fatalf("branch initialization: %v", err)
	}
	if _, _, err := branch.Update(ctx, appendTransaction(b)); err != nil {
		t.Fatal(err)
	}
	raced := false
	next, changed, err := branch.Update(ctx, func(current []Transaction) ([]Transaction, bool, error) {
		if !raced {
			raced = true
			if _, _, err := branch.Update(ctx, appendTransaction(otherWork)); err != nil {
				return nil, false, err
			}
		}
		return Apply(current, a)
	})
	if err != nil || !changed || len(next) != 4 {
		t.Fatalf("conflict retry did not replay latest branch: %v, %d", err, len(next))
	}
	projection, err := Replay(next)
	if err != nil || projection.Stats.Work != 2 {
		t.Fatalf("lost concurrent work: %v, %v", projection, err)
	}
	latest, err := branch.Read(ctx)
	if err != nil || len(latest) != 4 {
		t.Fatalf("remote read did not reflect both writers: %v, %v", latest, err)
	}
	writes := mock.refWrites
	if _, changed, err := branch.Update(ctx, appendTransaction(work)); err != nil || changed || mock.refWrites != writes {
		t.Fatalf("idempotent update published a commit: %t, %v", changed, err)
	}
}

func TestBranchCreationConflictRetry(t *testing.T) {
	branch, _ := newQueueAPI(t)
	work, _, _ := fixture(t)
	otherID, payload, err := WorkID([]byte(`{"task":"concurrent"}`))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	next, _, err := branch.Update(context.Background(), func(current []Transaction) ([]Transaction, bool, error) {
		calls++
		if calls == 1 {
			_, _, err := branch.Update(context.Background(), appendTransaction(Transaction{Kind: "Work", WorkID: otherID, Work: payload}))
			if err != nil {
				return nil, false, err
			}
		}
		return Apply(current, work)
	})
	if err != nil || calls != 2 || len(next) != 2 {
		t.Fatalf("creation race lost concurrent work: calls=%d, next=%v, err=%v", calls, next, err)
	}
}

func TestBranchAPIErrorHandling(t *testing.T) {
	work, _, _ := fixture(t)
	for _, test := range []struct {
		name   string
		path   string
		status int
	}{
		{"authentication", "git/ref/heads/" + DefaultBranch, http.StatusUnauthorized},
		{"repository unavailable", "/repos/owner/repo", http.StatusNotFound},
		{"tree permission", "git/trees", http.StatusForbidden},
		{"invalid commit", "git/commits", http.StatusUnprocessableEntity},
		{"reference permission", "git/refs", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.failPath, mock.failStatus = test.path, test.status
			_, changed, err := branch.Update(context.Background(), appendTransaction(work))
			if !hasStatus(err, test.status) || changed || mock.refWrites > 1 {
				t.Fatalf("API failure was hidden or retried: %v, changed=%t, writes=%d", err, changed, mock.refWrites)
			}
		})
	}
}

func TestBranchConflictRechecksTerminalState(t *testing.T) {
	branch, mock := newQueueAPI(t)
	ctx := context.Background()
	work, a, b := fixture(t)
	for _, tx := range []Transaction{work, b} {
		if _, _, err := branch.Update(ctx, appendTransaction(tx)); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	_, changed, err := branch.Update(ctx, func(current []Transaction) ([]Transaction, bool, error) {
		calls++
		if calls == 1 {
			completion := Transaction{Kind: "Completion", WorkID: work.WorkID, ClaimID: b.ClaimID, AttemptID: "attempt"}
			if _, _, err := branch.Update(ctx, appendTransaction(completion)); err != nil {
				return nil, false, err
			}
		}
		return Apply(current, a)
	})
	if err == nil || !strings.Contains(err.Error(), "terminal") || changed || calls != 2 || mock.refWrites != 4 {
		t.Fatalf("stale claim reopened completed work: changed=%t, calls=%d, writes=%d, err=%v", changed, calls, mock.refWrites, err)
	}
	latest, err := branch.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := Replay(latest)
	if err != nil || projection.Stats.Completed != 1 || projection.Stats.Claims != 1 {
		t.Fatalf("terminal state changed: %+v, %v", projection, err)
	}
}

func TestBranchInvalidMutationDoesNotPublish(t *testing.T) {
	branch, mock := newQueueAPI(t)
	_, changed, err := branch.Update(context.Background(), func([]Transaction) ([]Transaction, bool, error) {
		return []Transaction{{Kind: "Claim", WorkID: "missing", ClaimID: "a", RunID: "run"}}, true, nil
	})
	if err == nil || changed || len(mock.logs) != 0 || mock.refWrites != 0 {
		t.Fatalf("invalid mutation was published: changed=%t, err=%v", changed, err)
	}
}

func TestBranchRetryExhaustion(t *testing.T) {
	branch, mock := newQueueAPI(t)
	mock.conflicts = maxRetries
	work, _, _ := fixture(t)
	calls := 0
	_, changed, err := branch.Update(context.Background(), func(current []Transaction) ([]Transaction, bool, error) {
		calls++
		return Apply(current, work)
	})
	if err == nil || !strings.Contains(err.Error(), "after 5 attempts") || changed || calls != maxRetries || mock.head != "" {
		t.Fatalf("incorrect retry exhaustion: calls=%d, changed=%t, head=%s, err=%v", calls, changed, mock.head, err)
	}
}

func TestBranchRejectsInvalidLog(t *testing.T) {
	for _, test := range []struct {
		name, log, mode string
		truncated       bool
	}{
		{name: "malformed JSON", log: "not JSON\n", mode: "100644"},
		{name: "invalid reference", log: `{"kind":"Claim","work_id":"missing","claim_id":"a","run_id":"run"}`, mode: "100644"},
		{name: "symlink", log: "/outside", mode: "120000"},
		{name: "truncated tree", mode: "100644", truncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.head = "existing"
			mock.commits[mock.head] = queueCommit{Tree: "tree"}
			mock.logs["tree"] = test.log
			mock.mode, mock.truncated = test.mode, test.truncated
			if _, err := branch.Read(context.Background()); err == nil {
				t.Fatal("accepted invalid queue")
			}
			called := false
			if _, _, err := branch.Update(context.Background(), func(current []Transaction) ([]Transaction, bool, error) {
				called = true
				return current, false, nil
			}); err == nil || called || mock.refWrites != 0 {
				t.Fatalf("invalid queue reached mutation callback: called=%t, err=%v", called, err)
			}
		})
	}
}

func TestBranchCancellation(t *testing.T) {
	branch, mock := newQueueAPI(t)
	work, _, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, changed, err := branch.Update(ctx, func(current []Transaction) ([]Transaction, bool, error) {
		cancel()
		return Apply(current, work)
	})
	if !errors.Is(err, context.Canceled) || changed || mock.refWrites != 0 {
		t.Fatalf("cancelled update published: changed=%t, err=%v", changed, err)
	}
}

func TestBranchRejectsInvalidRepositoryAndBranch(t *testing.T) {
	for _, branch := range []Branch{
		{Remote: "not-a-repo", Name: DefaultBranch},
		{Remote: "/tmp/local.git", Name: DefaultBranch},
		{Remote: "owner/..", Name: DefaultBranch},
		{Remote: "owner/repo", Name: "../oops"},
		{Remote: "owner/repo", Name: "a.lock"},
		{Remote: "owner/repo", Name: "a/./b"},
		{Remote: "owner/repo", Name: "a.lock/b"},
		{Remote: "owner/repo", Name: "a."},
	} {
		if _, err := branch.Read(context.Background()); err == nil {
			t.Errorf("accepted invalid branch: %+v", branch)
		}
	}
}
