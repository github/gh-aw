package workqueue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type gitQueueCommit struct {
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}

type queueAPI struct {
	head                 string
	logs                 map[string]string
	bases                map[string]string
	commits              map[string]gitQueueCommit
	refWrites            int
	conflicts            int
	ambiguous            bool
	concurrent           func()
	mode                 string
	truncated            bool
	missingLog           bool
	nativeRun            *NativeRun
	nativeReads          int
	issue                any
	issueStatus          int
	resourceReads        int
	resourceRead         func()
	userID               json.Number
	workerStatus         int
	workerContent        string
	workerPath           string
	workerState          string
	workerReads          int
	workerRef            string
	noAdmin              bool
	settings             string
	settingsStatus       int
	sources              map[string]string
	workerRoutes         map[string]string
	workerStatuses       map[string]int
	workerStates         map[string]string
	defaultRef           string
	defaultRevision      string
	configRefs           []string
	routeReads           map[string]int
	registrationReads    map[string]int
	sourceStatuses       map[string]int
	registrationStatuses map[string]int
}

type queueTransport struct{ url string }

func TestBranchAuthenticatesCanonicalRepositoryAndRejectsForeignLedger(t *testing.T) {
	branch, mock := newQueueAPI(t)
	branch.Remote = strings.ToUpper(testRepository)
	actor, err := branch.Authenticate(context.Background(), "administrator")
	if err != nil || actor.Repository != testRepository {
		t.Fatalf("operator selected repository spelling became authority: %+v %v", actor, err)
	}
	control := mustOp(t, map[string]any{"kind": "Control", "control": "admission_paused", "value": true, "reason": "operator"})
	installMockLog(t, mock, testGenesis(t, nil))
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
		respond(map[string]any{"id": 1, "full_name": testRepository, "default_branch": "main", "permissions": map[string]bool{"push": true, "admin": !mock.noAdmin}})
	case r.Method == http.MethodGet && path == "issues/7":
		mock.resourceReads++
		if mock.resourceRead != nil {
			mock.resourceRead()
		}
		if mock.issueStatus != 0 {
			fail(mock.issueStatus)
		} else {
			respond(mock.issue)
		}
	case r.Method == http.MethodGet && (path == "actions/runs/202/attempts/1" ||
		mock.nativeRun != nil && path == "actions/runs/"+mock.nativeRun.ID.String()+"/attempts/1"):
		mock.nativeReads++
		respond(mock.nativeRun)
	case r.Method == http.MethodGet && path == "git/ref/heads/main":
		ref := mock.defaultRef
		if ref == "" {
			ref = "refs/heads/main"
		}
		revision := mock.defaultRevision
		if revision == "" {
			revision = strings.Repeat("f", 40)
		}
		respond(map[string]any{"ref": ref, "object": map[string]string{"sha": revision}})
	case r.Method == http.MethodGet && path == "contents/.github/workflows/aw.json":
		mock.configRefs = append(mock.configRefs, r.URL.Query().Get("ref"))
		if mock.settingsStatus != 0 {
			fail(mock.settingsStatus)
		} else if mock.settings == "" {
			fail(http.StatusNotFound)
		} else {
			respond(map[string]any{"type": "file", "path": repositorySettingsPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(mock.settings))})
		}
	case r.Method == http.MethodGet && path == "contents/.github/workflows":
		mock.configRefs = append(mock.configRefs, r.URL.Query().Get("ref"))
		sources := mock.sources
		if sources == nil {
			sources = map[string]string{"worker": "---\ntools:\n  work-queue:\n    worker: true\n---\nWorker"}
		}
		entries := []map[string]string{}
		for name := range sources {
			entries = append(entries, map[string]string{"type": "file", "path": ".github/workflows/" + name + ".md"})
		}
		respond(entries)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "contents/.github/workflows/") && strings.HasSuffix(path, ".md"):
		mock.configRefs = append(mock.configRefs, r.URL.Query().Get("ref"))
		name := strings.TrimSuffix(strings.TrimPrefix(path, "contents/.github/workflows/"), ".md")
		if status := mock.sourceStatuses[name]; status != 0 {
			fail(status)
			break
		}
		content, exists := mock.sources[name]
		if mock.sources == nil && name == "worker" {
			content, exists = "---\ntools:\n  work-queue:\n    worker: true\n---\nWorker", true
		}
		if !exists {
			fail(http.StatusNotFound)
		} else {
			respond(map[string]any{"type": "file", "path": strings.TrimPrefix(path, "contents/"), "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
		}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "contents/.github/workflows/"):
		mock.workerReads++
		mock.workerRef = r.URL.Query().Get("ref")
		if mock.workerStatus != 0 {
			fail(mock.workerStatus)
			break
		}
		workerPath := strings.TrimPrefix(path, "contents/")
		if status := mock.workerStatuses[workerPath]; status != 0 {
			fail(status)
			break
		}
		if mock.routeReads == nil {
			mock.routeReads = map[string]int{}
		}
		mock.routeReads[workerPath]++
		if mock.workerPath != "" {
			workerPath = mock.workerPath
		}
		content := mock.workerContent
		if route, exists := mock.workerRoutes[workerPath]; exists {
			content = route
		}
		if content == "" {
			content = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n"
		}
		respond(map[string]any{"type": "file", "path": workerPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "actions/workflows/"):
		workerPath := ".github/workflows/" + strings.TrimPrefix(path, "actions/workflows/")
		if mock.registrationReads == nil {
			mock.registrationReads = map[string]int{}
		}
		mock.registrationReads[workerPath]++
		if status := mock.registrationStatuses[workerPath]; status != 0 {
			fail(status)
			break
		}
		if mock.workerPath != "" {
			workerPath = mock.workerPath
		}
		state := mock.workerState
		if configured, exists := mock.workerStates[workerPath]; exists {
			state = configured
		}
		if state == "" {
			state = "active"
		}
		respond(map[string]any{"path": workerPath, "state": state})
	case r.Method == http.MethodGet && path == "git/ref/heads/"+DefaultBranch:
		if mock.head == "" {
			fail(404)
		} else {
			respond(map[string]any{"ref": "refs/heads/" + DefaultBranch, "object": map[string]string{"sha": mock.head}})
		}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
		commit := mock.commits[strings.TrimPrefix(path, "git/commits/")]
		parents := make([]map[string]string, 0, len(commit.Parents))
		for _, sha := range commit.Parents {
			parents = append(parents, map[string]string{"sha": sha})
		}
		respond(map[string]any{"tree": map[string]string{"sha": commit.Tree}, "parents": parents})
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
		sha := hashBytes([]byte(fmt.Sprintf("commit-%d", len(mock.commits)+1)))[:40]
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
	head := hashBytes([]byte(fmt.Sprintf("installed-commit-%d", len(mock.commits)+1)))[:40]
	mock.logs[tree], mock.commits[head], mock.head = string(data), gitQueueCommit{Tree: tree}, head
}

func TestBranchVerifiesCheckpointChainsBeyondFormerDepthLimit(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		History    []QueueCommit `json:"history"`
		Checkpoint []QueueCommit `json:"checkpoint"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	branch, mock := newQueueAPI(t)
	head := strings.Repeat("a", 40)
	tree := "checkpoint-chain-base"
	contents, err := Serialize(fixture.History)
	if err != nil {
		t.Fatal(err)
	}
	mock.logs[tree] = string(contents)
	mock.commits[head] = gitQueueCommit{Tree: tree}
	commits := fixture.History
	actor := fixture.Checkpoint[0].Actor
	for index := range 66 {
		ordered, err := causalOrder(commits)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, err := CompactCheckpoint(commits, head, actor, ordered[len(ordered)-1].At+1)
		if err != nil {
			t.Fatal(err)
		}
		nextTree := fmt.Sprintf("checkpoint-chain-%d", index)
		nextHead := fmt.Sprintf("%040x", index+1)
		contents, err := Serialize(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		mock.logs[nextTree] = string(contents)
		mock.commits[nextHead] = gitQueueCommit{Tree: nextTree, Parents: []string{head}}
		commits, head = checkpoint, nextHead
	}
	mock.head = head
	if _, err := branch.Read(context.Background()); err != nil {
		t.Fatalf("valid checkpoint chain was rejected after 66 compactions: %v", err)
	}
}

func TestBranchCurrentOnlyMandatoryInitializationAndIdempotency(t *testing.T) {
	branch, mock := newQueueAPI(t)
	mock.noAdmin = true
	actor, err := branch.Authenticate(context.Background(), "producer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := branch.Read(context.Background()); err == nil || !strings.Contains(err.Error(), "queue_missing") {
		t.Fatalf("missing queue is not an existing empty policy-less ledger: %v", err)
	}
	policy, err := branch.PolicyFromConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewWork([]byte(`{"task":"a"}`), "graph", "a", "default", policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := NewRequest("submit", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	published, err := branch.Publish(context.Background(), actor, request)
	if err != nil || !published.Changed {
		t.Fatalf("new queue initialization: %+v %v", published, err)
	}
	commits, err := branch.Read(context.Background())
	if err != nil || len(commits) != 1 || mustOperationKind(t, commits[0].Operations[0]) != "Policy" {
		t.Fatalf("mandatory policy genesis absent: %v", err)
	}
	if commits[0].Request.ID != "submit" ||
		commits[0].PolicyEpoch != "epoch_75490bd7b93e6fa7d18cfdea90cc6bcb983d5f3ea326249d2709ca6c94bc07ba" ||
		commits[0].ID != "q_75490bd7b93e6fa7d18cfdea90cc6bcb983d5f3ea326249d2709ca6c94bc07ba" ||
		commits[0].Actor.Role != "producer" || len(commits[0].Operations) != 2 {
		t.Fatal("default publisher identities differ from the independent canonical bootstrap example")
	}
	writes := mock.refWrites
	again, err := branch.Publish(context.Background(), actor, request)
	if err != nil || again.Changed || mock.refWrites != writes || again.Commit.ID != published.Commit.ID {
		t.Fatalf("stable request did not recover accepted result: %v", err)
	}
	request.Parameters = mustOp(t, SubmitParameters{Nodes: []WorkDefinition{testNode(t, commits, "different")}})
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
