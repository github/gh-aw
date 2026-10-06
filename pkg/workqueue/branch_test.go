package workqueue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type gitQueueCommit struct {
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}

type queueAPI struct {
	head          string
	logs          map[string]string
	bases         map[string]string
	commits       map[string]gitQueueCommit
	refWrites     int
	conflicts     int
	ambiguous     bool
	concurrent    func()
	mode          string
	truncated     bool
	missingLog    bool
	nativeRun     *NativeRun
	issue         any
	issueStatus   int
	resourceReads int
	userID        json.Number
}

type queueTransport struct{ url string }

func TestBranchAuthenticatesCanonicalRepositoryAndRejectsForeignLedger(t *testing.T) {
	branch, mock := newQueueAPI(t)
	branch.Remote = strings.ToUpper(testRepository)
	actor, err := branch.Authenticate(context.Background(), "administrator")
	if err != nil || actor.Repository != testRepository {
		t.Fatalf("operator selected repository spelling became authority: %+v %v", actor, err)
	}
	control := Op(map[string]any{"kind": "Control", "control": "admission_paused", "value": true, "reason": "operator"})
	request, _ := NewRequest("pause", "control", actor, OperationsParameters{Operations: []Operation{control}})
	if _, err := branch.Publish(context.Background(), actor, request); err != nil {
		t.Fatal(err)
	}
	commits, err := branch.Read(context.Background())
	if err != nil || commits[0].Actor.Repository != testRepository {
		t.Fatalf("initialization did not retain authenticated canonical repository: %v", err)
	}
	foreign := actor
	foreign.Repository = "foreign/repository"
	genesis, err := Genesis(foreign, DefaultPolicy(foreign.Principal, foreign.Repository), "foreign-init", "foreign", 1000)
	if err != nil {
		t.Fatal(err)
	}
	installMockLog(t, mock, []QueueCommit{genesis})
	if _, err := branch.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "repository_scope_invalid") {
		t.Fatalf("foreign repository ledger was accepted as current authority: %v", err)
	}
}

func (transport queueTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme = "http"
	request.URL.Host = strings.TrimPrefix(transport.url, "http://")
	return http.DefaultTransport.RoundTrip(request)
}

func newQueueAPI(t *testing.T) (Branch, *queueAPI) {
	t.Helper()
	mock := &queueAPI{logs: map[string]string{}, bases: map[string]string{}, commits: map[string]gitQueueCommit{}, mode: "100644"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Error("request was not authenticated")
		}
		mock.serve(t, w, r)
	}))
	t.Cleanup(server.Close)
	client, err := api.NewRESTClient(api.ClientOptions{Host: "github.com", AuthToken: "test-token", Transport: queueTransport{server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	return Branch{Remote: testRepository, Name: DefaultBranch, client: client}, mock
}

func (mock *queueAPI) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	repositoryPath := "/repos/" + testRepository
	if len(r.URL.Path) >= len(repositoryPath) && strings.EqualFold(r.URL.Path[:len(repositoryPath)], repositoryPath) &&
		(len(r.URL.Path) == len(repositoryPath) || r.URL.Path[len(repositoryPath)] == '/') {
		r.URL.Path = repositoryPath + r.URL.Path[len(repositoryPath):]
	}
	path := strings.TrimPrefix(r.URL.Path, "/repos/"+testRepository+"/")
	w.Header().Set("Content-Type", "application/json")
	respond := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	fail := func(code int) {
		w.WriteHeader(code)
		respond(map[string]string{"message": "API rejected"})
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/user":
		id := mock.userID
		if id == "" {
			id = json.Number(testPrincipal)
		}
		respond(map[string]any{"id": id, "login": "operator"})
	case r.Method == http.MethodGet && r.URL.Path == "/repos/"+testRepository:
		respond(map[string]any{"id": 1, "full_name": testRepository, "default_branch": "main", "permissions": map[string]bool{"push": true, "admin": true}})
	case r.Method == http.MethodGet && path == "issues/7":
		mock.resourceReads++
		if mock.issueStatus != 0 {
			fail(mock.issueStatus)
		} else {
			respond(mock.issue)
		}
	case r.Method == http.MethodGet && path == "actions/runs/202/attempts/1":
		respond(mock.nativeRun)
	case r.Method == http.MethodGet && path == "git/ref/heads/main":
		respond(map[string]any{"object": map[string]string{"sha": strings.Repeat("f", 40)}})
	case r.Method == http.MethodGet && path == "git/ref/heads/"+DefaultBranch:
		if mock.head == "" {
			fail(404)
		} else {
			respond(map[string]any{"ref": "refs/heads/" + DefaultBranch, "object": map[string]string{"sha": mock.head}})
		}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
		commit := mock.commits[strings.TrimPrefix(path, "git/commits/")]
		respond(map[string]any{"tree": map[string]string{"sha": commit.Tree}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/trees/"):
		tree := []map[string]string{{"path": "unrelated", "mode": "100644", "type": "blob", "sha": "unrelated"}}
		if !mock.missingLog {
			tree = append(tree, map[string]string{"path": FileName, "mode": mock.mode, "type": "blob", "sha": strings.TrimPrefix(path, "git/trees/")})
		}
		respond(map[string]any{"truncated": mock.truncated, "tree": tree})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/blobs/"):
		respond(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(mock.logs[strings.TrimPrefix(path, "git/blobs/")]))})
	case r.Method == http.MethodPost && path == "git/trees":
		var body struct {
			BaseTree string `json:"base_tree"`
			Tree     []struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				Mode    string `json:"mode"`
			} `json:"tree"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Tree) != 1 || body.Tree[0].Path != FileName || body.Tree[0].Mode != "100644" {
			t.Error("publication changed anything outside queue log")
		}
		sha := fmt.Sprintf("tree-%d", len(mock.logs)+1)
		mock.logs[sha], mock.bases[sha] = body.Tree[0].Content, body.BaseTree
		respond(map[string]string{"sha": sha})
	case r.Method == http.MethodPost && path == "git/commits":
		var commit gitQueueCommit
		_ = json.NewDecoder(r.Body).Decode(&commit)
		if commit.Parents == nil {
			t.Error("orphan parents must be an explicit array")
		}
		sha := fmt.Sprintf("commit-%d", len(mock.commits)+1)
		mock.commits[sha] = commit
		respond(map[string]string{"sha": sha})
	case r.Method == http.MethodPost && path == "git/refs" ||
		r.Method == http.MethodPatch && path == "git/refs/heads/"+DefaultBranch:
		mock.refWrites++
		var body struct {
			SHA   string `json:"sha"`
			Force bool   `json:"force"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Force {
			t.Error("force publication is prohibited")
		}
		if mock.concurrent != nil {
			f := mock.concurrent
			mock.concurrent = nil
			f()
			fail(409)
			return
		}
		if mock.conflicts > 0 {
			mock.conflicts--
			fail(422)
			return
		}
		commit := mock.commits[body.SHA]
		if r.Method == http.MethodPatch {
			if len(commit.Parents) != 1 || commit.Parents[0] != mock.head {
				fail(409)
				return
			}
			if mock.bases[commit.Tree] != mock.commits[mock.head].Tree {
				t.Error("update discarded unrelated branch files")
			}
		} else if mock.head != "" {
			fail(422)
			return
		}
		mock.head = body.SHA
		if mock.ambiguous {
			mock.ambiguous = false
			fail(500)
			return
		}
		respond(map[string]string{"ref": "refs/heads/" + DefaultBranch})
	default:
		t.Errorf("unexpected API call %s %s", r.Method, r.URL.Path)
		fail(404)
	}
}

func installMockLog(t *testing.T, mock *queueAPI, commits []QueueCommit) {
	t.Helper()
	data, err := Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	tree := fmt.Sprintf("installed-tree-%d", len(mock.logs)+1)
	head := fmt.Sprintf("installed-commit-%d", len(mock.commits)+1)
	mock.logs[tree], mock.commits[head], mock.head = string(data), gitQueueCommit{Tree: tree}, head
}

func TestBranchCurrentOnlyMandatoryInitializationAndIdempotency(t *testing.T) {
	branch, mock := newQueueAPI(t)
	actor, err := branch.Authenticate(context.Background(), "producer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := branch.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "queue_missing") {
		t.Fatalf("missing queue is not an existing empty policy-less ledger: %v", err)
	}
	node, err := NewWork([]byte(`{"task":"a"}`), "graph", "a", "default", DefaultPolicy(testPrincipal, testRepository), 1000)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := NewRequest("submit", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	published, err := branch.Publish(context.Background(), actor, request)
	if err != nil || !published.Changed {
		t.Fatalf("new queue initialization: %+v %v", published, err)
	}
	commits, err := branch.Read(context.Background())
	if err != nil || len(commits) != 2 || operationKind(commits[0].Operations[0]) != "Policy" {
		t.Fatalf("mandatory policy genesis absent: %v", err)
	}
	writes := mock.refWrites
	again, err := branch.Publish(context.Background(), actor, request)
	if err != nil || again.Changed || mock.refWrites != writes || again.Commit.ID != published.Commit.ID {
		t.Fatalf("stable request did not recover accepted result: %v", err)
	}
	request.Parameters = Op(SubmitParameters{Nodes: []WorkDefinition{testNode(t, commits, "different")}})
	request.Fingerprint, _ = Fingerprint(actor, request.Kind, request.Parameters)
	if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.Contains(err.Error(), "request_reuse") {
		t.Fatalf("reused request changed meaning: %v", err)
	}
}

func TestBranchCASRecomputesAndAmbiguousAckDoesNotDoubleCharge(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits := testGenesis(t, nil)
	a, b := testNode(t, commits, "a"), testNode(t, commits, "b")
	commits = testSubmit(t, commits, "submit", a, b)
	installMockLog(t, mock, commits)
	mock.concurrent = func() {
		other, _ := testGrant(t, commits, "other-grant", 1, 1)
		installMockLog(t, mock, other)
	}
	actor := testActor("administrator")
	request, _ := NewRequest("grant", "dispatch_next", actor, DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
	mock.ambiguous = true
	publication, err := branch.Publish(context.Background(), actor, request)
	if err != nil || publication.Commit == nil {
		t.Fatalf("publication did not reconcile conflict/ambiguous response: %v", err)
	}
	var actual ClaimOperation
	_ = json.Unmarshal(publication.Commit.Operations[0], &actual)
	if actual.WorkID != b.WorkID {
		t.Fatal("CAS loss retained tentative selection instead of fresh winner")
	}
	latest, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(latest)
	if err != nil || state.Stats.Claims != 2 || len(state.Requests) != 4 {
		t.Fatalf("ambiguous acknowledgment duplicated charge: %+v %v", state.Stats, err)
	}
}

func TestBranchRejectsMalformedExistingAuthority(t *testing.T) {
	for _, name := range []string{"legacy", "empty", "missing-log", "symlink", "truncated"} {
		t.Run(name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.head = "head"
			mock.commits["head"] = gitQueueCommit{Tree: "tree"}
			mock.logs["tree"] = `{"version":2,"kind":"Work"}` + "\n"
			switch name {
			case "empty":
				mock.logs["tree"] = ""
			case "missing-log":
				mock.missingLog = true
			case "symlink":
				mock.mode = "120000"
			case "truncated":
				mock.truncated = true
			}
			if _, err := branch.Read(context.Background()); err == nil || mock.refWrites != 0 {
				t.Fatal("old/invalid existing queue was adopted or rewritten")
			}
		})
	}
}

func TestBranchCannotForgeWorkflowOrigin(t *testing.T) {
	branch, mock := newQueueAPI(t)
	request, _ := NewRequest("forged", "dispatch_next", testActor("worker"), DispatchParameters{
		Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
	})
	if _, err := branch.Publish(context.Background(), testActor("worker"), request); err == nil || mock.refWrites != 0 {
		t.Fatal("operator supplied worker actor acquired authority")
	}
	actor := testActor("administrator")
	actor.Principal = "forged"
	request, _ = NewRequest("forged", "dispatch_next", actor, DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
	if _, err := branch.Publish(context.Background(), actor, request); err == nil || mock.refWrites != 0 {
		t.Fatal("submitted actor string impersonated authenticated principal")
	}
}

func TestBranchRejectsInvalidRepositoryAndBranch(t *testing.T) {
	for _, branch := range []Branch{
		{Remote: "bad", Name: DefaultBranch}, {Remote: "owner/..", Name: DefaultBranch},
		{Remote: testRepository, Name: "../oops"}, {Remote: testRepository, Name: "a.lock"},
		{Remote: testRepository, Name: "a/./b"}, {Remote: testRepository, Name: "a.lock/b"},
	} {
		if _, err := branch.Read(context.Background()); err == nil {
			t.Errorf("accepted invalid authority %+v", branch)
		}
	}
}
