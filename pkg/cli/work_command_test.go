package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
)

type workAPIRoundTripper func(*http.Request) (*http.Response, error)

func (f workAPIRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func mustWorkQueueOperation(t testing.TB, value any) workqueue.Operation {
	t.Helper()
	operation, err := workqueue.Op(value)
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func TestWorkCommandClosedOperatorSurfaces(t *testing.T) {
	command := NewWorkCommand()
	for _, required := range []string{"compact", "trace", "explain"} {
		found := false
		for _, subcommand := range command.Commands() {
			if subcommand.Name() == required {
				found = true
				if required == "explain" && subcommand.Flags().Lookup("before-claim") == nil {
					t.Fatal("historical before-Claim explanation is missing from the public command")
				}
			}
		}
		if !found {
			t.Fatalf("required operator surface %s is absent", required)
		}
	}
	for _, forbidden := range []string{"claim", "finish", "cancel-claim", "force-release"} {
		for _, subcommand := range command.Commands() {
			if subcommand.Name() == forbidden {
				t.Fatalf("operator retained unsafe unscheduled/worker impersonation surface %s", forbidden)
			}
		}
	}
	for _, args := range [][]string{
		{"--storage", "issues", "stats"},
		{"dispatch-next", "--work-id", "chosen"},
		{"dispatch-next", "--run-id", "forged"},
		{"reconcile", "--force"},
		{"reconcile", "--work-id", "work", "--dispatch-id", "dispatch"},
		{"reconcile", "--work-id", "work", "--cancel-reserved"},
		{"reconcile", "--work-id", "work", "--receipt-file", "copied-js-proof"},
		{"trace"},
		{"trace", "--claim-id", "chosen", "--request-id", "request"},
		{"trace", "--claim-id", "chosen", "--limit", "257"},
		{"explain", "--before-claim", "chosen", "--work-id", "work"},
		{"explain", "--before-claim", "chosen", "--request-id", "request"},
	} {
		cmd := NewWorkCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("unsupported authority accepted: %v", args)
		}
	}
	if branch := command.PersistentFlags().Lookup("branch").DefValue; branch != "work-queue" {
		t.Fatal("CLI default does not match runtime authority")
	}
}

func TestWorkCommandProtectedNativeDeliveryEntryPoint(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	const remote, principal = "owner/repo", "1001"
	policy := workqueue.DefaultPolicy(principal, remote)
	actor := workqueue.Actor{Role: "administrator", Principal: principal, Repository: remote}
	genesis, err := workqueue.Genesis(actor, policy, "init", "init-request", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	commits := []workqueue.QueueCommit{genesis}
	appendRequest := func(actor workqueue.Actor, id, kind string, parameters any) workqueue.Decision {
		t.Helper()
		request, err := workqueue.NewRequest(id, kind, actor, parameters)
		if err != nil {
			t.Fatal(err)
		}
		var decision workqueue.Decision
		commits, _, decision, err = workqueue.BuildCandidate(commits, actor, request, time.Now().UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		return decision
	}
	work, err := workqueue.NewWork([]byte(`{"effect_contract":{"kind":"none"}}`), "native-host", "root", "default", policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	actor.Role = "producer"
	appendRequest(actor, "submit", "submit", workqueue.SubmitParameters{Nodes: []workqueue.WorkDefinition{work}})
	actor.Role, actor.Workflow, actor.RunID, actor.RunAttempt = "dispatcher", ".github/workflows/dispatcher.lock.yml", "101", 1
	decision := appendRequest(actor, "grant", "dispatch_next", workqueue.DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: policy.Limits.AssignmentBytes})
	assignment := decision.Assignments[0]
	appendRequest(actor, "start", "dispatch", workqueue.OperationsParameters{Operations: []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": actor,
	})}})
	profile := policy.Pools["default"].Profiles["default"]
	binding := workqueue.RunBinding{RunID: "202", RunAttempt: 1, Repository: remote, Workflow: profile.Workflow, Ref: profile.Ref, Principal: principal, Event: "workflow_dispatch"}
	evidence := workqueue.Evidence{Kind: "reconciliation", Source: "github_api", Repository: remote, Workflow: profile.Workflow, Ref: profile.Ref, Principal: principal, CheckedAt: time.Now().UnixMilli(), RunID: "202", RunAttempt: 1}
	actor.Role, actor.Workflow, actor.RunID, actor.RunAttempt = "reconciler", "", "", 0
	appendRequest(actor, "bind", "dispatch", workqueue.OperationsParameters{Operations: []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": binding, "evidence": evidence,
	})}})
	actor.Role, actor.Workflow, actor.RunID, actor.RunAttempt = "worker", profile.Workflow, "202", 1
	actor.DispatchID, actor.ClaimHandle = assignment.DispatchID, "h1"
	appendRequest(actor, "finish", "finish", workqueue.FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: "h1", Outcome: "completed"})
	data, err := workqueue.Serialize(commits)
	if err != nil {
		t.Fatal(err)
	}
	log, pending := string(data), ""
	head, writes := 1, 0
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Fatal("native operator API read lacks protected credentials")
		}
		path := strings.TrimPrefix(r.URL.Path, "/repos/"+remote+"/")
		var response any
		switch {
		case r.URL.Path == "/user":
			response = map[string]any{"id": 1001, "login": "operator"}
		case r.URL.Path == "/repos/"+remote:
			response = map[string]any{"id": 9, "full_name": remote, "permissions": map[string]bool{"admin": true, "push": true}}
		case path == "actions/runs/202/attempts/1":
			response = map[string]any{
				"id": 202, "run_attempt": 1, "event": "workflow_dispatch", "status": "in_progress",
				"repository": map[string]any{"full_name": remote, "id": 9}, "path": profile.Workflow,
				"head_sha": profile.Ref, "actor": map[string]any{"id": 1001},
				"display_title": "native worker " + assignment.DispatchID,
			}
		case r.Method == http.MethodGet && path == "git/ref/heads/"+workqueue.DefaultBranch:
			response = map[string]any{"ref": "refs/heads/" + workqueue.DefaultBranch, "object": map[string]string{"sha": strconv.Itoa(head)}}
		case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
			response = map[string]any{"tree": map[string]string{"sha": "tree"}}
		case r.Method == http.MethodGet && path == "git/trees/tree":
			response = map[string]any{"tree": []map[string]string{{"path": workqueue.FileName, "mode": "100644", "type": "blob", "sha": "blob"}}}
		case r.Method == http.MethodGet && path == "git/blobs/blob":
			response = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(log))}
		case r.Method == http.MethodPost && path == "git/trees":
			var body struct {
				Tree []struct {
					Content string `json:"content"`
				} `json:"tree"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pending = body.Tree[0].Content
			response = map[string]string{"sha": "tree"}
		case r.Method == http.MethodPost && path == "git/commits":
			response = map[string]string{"sha": strconv.Itoa(head + 1)}
		case r.Method == http.MethodPatch && path == "git/refs/heads/"+workqueue.DefaultBranch:
			writes++
			head++
			log = pending
			response = map[string]string{"ref": "refs/heads/" + workqueue.DefaultBranch}
		default:
			t.Fatalf("unexpected native host API %s %s", r.Method, r.URL.Path)
		}
		data, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})
	calls := 0
	host := workqueue.NewNativeDeliveryHost(workqueue.NativeDeliveryHostOptions{
		Inventory: func(_ context.Context, scope workqueue.NativeDeliveryScope) (workqueue.NativeDeliveryInventory, error) {
			calls++
			if scope.Member.ClaimID != assignment.Claims[0].ClaimID || scope.Run.RunAttempt != 1 {
				t.Fatal("native CLI lost protected process/member binding")
			}
			return workqueue.NativeDeliveryInventory{Closed: true}, nil
		},
	})
	execute := func(host *workqueue.NativeDeliveryHost, requestID string) workqueue.DeliveryRecovery {
		t.Helper()
		cmd := NewWorkCommandWithNativeDeliveryHost(host)
		// A caller-provided context must not discard the constructor capability.
		cmd.SetContext(context.Background())
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs([]string{"--repo", remote, "--json", "reconcile", "--work-id", work.WorkID, "--request-id", requestID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("native host command failed: %v (%s)", err, output.String())
		}
		var result workqueue.DeliveryRecovery
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := execute(nil, "missing-host"); result.Reason != "delivery_unresolved" || result.Publication != nil || writes != 0 {
		t.Fatalf("missing process evidence silently settled native delivery: %+v", result)
	}
	if result := execute(host, "protected-host"); result.Reason != "delivery_verified" || calls != 2 || writes != 1 {
		t.Fatalf("approved native process callback did not reach concrete CLI: %+v calls=%d writes=%d", result, calls, writes)
	}
}

func TestWorkCommandSubmitDefaultsRequireOmittedIdentityFlags(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	apiCalls := 0
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		apiCalls++
		return &http.Response{
			StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"message":"unexpected API request"}`)), Request: r,
		}, nil
	})
	for _, args := range [][]string{
		{"--graph-id", ""},
		{"--node-key", ""},
		{"--graph-id", "", "--node-key", "distinct"},
		{"--graph-id", "distinct", "--node-key", ""},
	} {
		cmd := NewWorkCommand()
		cmd.SetIn(strings.NewReader(`{"a":1,"b":2}`))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"--repo", "owner/repo", "submit-work", "--file", "-"}, args...))
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "work_invalid:") ||
			!strings.Contains(err.Error(), "omit the flags") || !strings.Contains(err.Error(), "--graph-id review --node-key root") {
			t.Fatalf("explicit empty identity %v did not explain missing-only defaults: %v", args, err)
		}
		if apiCalls != 0 {
			t.Fatalf("explicit empty identity %v reached authenticated queue APIs", args)
		}
	}
}

func TestWorkCommandCurrentProtocolWithoutCheckout(t *testing.T) {
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	const remote = "owner/repo"
	var log, pending string
	head := 0
	apiWrites := 0
	admin := true
	actorID := 1001
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Error("queue API was not authenticated")
		}
		path := strings.TrimPrefix(r.URL.Path, "/repos/"+remote+"/")
		if r.Method != http.MethodGet {
			apiWrites++
		}
		status := http.StatusOK
		var result any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			result = map[string]any{"id": actorID, "login": "operator"}
		case r.Method == http.MethodGet && r.URL.Path == "/repos/"+remote:
			result = map[string]any{"id": 9, "full_name": remote, "default_branch": "main", "permissions": map[string]bool{"admin": admin, "push": true}}
		case r.Method == http.MethodGet && path == "issues/7":
			result = map[string]any{"id": 10, "number": 7, "state": "closed", "state_reason": "completed"}
		case r.Method == http.MethodGet && path == "git/ref/heads/main":
			result = map[string]any{"object": map[string]string{"sha": strings.Repeat("f", 40)}}
		case r.Method == http.MethodGet && path == "contents/.github/workflows/worker.lock.yml":
			if r.URL.Query().Get("ref") != strings.Repeat("f", 40) {
				t.Error("worker provisioning did not use the independently resolved immutable default revision")
			}
			content := "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n"
			result = map[string]any{"type": "file", "path": ".github/workflows/worker.lock.yml", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}
		case r.Method == http.MethodGet && path == "actions/workflows/worker.lock.yml":
			result = map[string]any{"path": ".github/workflows/worker.lock.yml", "state": "active"}
		case r.Method == http.MethodGet && path == "git/ref/heads/"+workqueue.DefaultBranch:
			if head == 0 {
				status, result = http.StatusNotFound, map[string]string{"message": "Not Found"}
			} else {
				result = map[string]any{"ref": "refs/heads/" + workqueue.DefaultBranch, "object": map[string]string{"sha": strconv.Itoa(head)}}
			}
		case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
			result = map[string]any{"tree": map[string]string{"sha": "tree"}}
		case r.Method == http.MethodGet && path == "git/trees/tree":
			result = map[string]any{"tree": []map[string]string{{"path": workqueue.FileName, "mode": "100644", "type": "blob", "sha": "blob"}}}
		case r.Method == http.MethodGet && path == "git/blobs/blob":
			result = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(log))}
		case r.Method == http.MethodPost && path == "git/trees":
			var body struct {
				Tree []struct {
					Content string `json:"content"`
				} `json:"tree"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pending = body.Tree[0].Content
			result = map[string]string{"sha": "tree"}
		case r.Method == http.MethodPost && path == "git/commits":
			result = map[string]string{"sha": strconv.Itoa(head + 1)}
		case r.Method == http.MethodPost && path == "git/refs" ||
			r.Method == http.MethodPatch && path == "git/refs/heads/"+workqueue.DefaultBranch:
			head++
			log = pending
			result = map[string]string{"ref": "refs/heads/" + workqueue.DefaultBranch}
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
			status, result = http.StatusNotFound, map[string]string{"message": "Not Found"}
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})
	run := func(input string, args ...string) map[string]any {
		t.Helper()
		cmd := NewWorkCommand()
		var output bytes.Buffer
		cmd.SetIn(strings.NewReader(input))
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(append([]string{"--repo", remote, "--json"}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command %v: %v (%s)", args, err, output.String())
		}
		var result map[string]any
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatalf("JSON result %q: %v", output.String(), err)
		}
		return result
	}
	reject := func(input, code string, args ...string) {
		t.Helper()
		before := log
		cmd := NewWorkCommand()
		cmd.SetIn(strings.NewReader(input))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(append([]string{"--repo", remote, "--json"}, args...))
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), code+":") {
			t.Fatalf("command %v: expected %s, got %v", args, code, err)
		}
		if log != before {
			t.Fatalf("rejected command %v changed queue authority", args)
		}
	}
	submitted := run(`{"task":"review"}`, "submit-work", "--file", "-", "--request-id", "stable-submit")
	id := submitted["work_id"].(string)
	if !submitted["created"].(bool) || id != "f096f37384d5cea26021021248638442ed2bb42a3b0b01e10e85224c130f97c5" {
		t.Fatalf("submission did not persist current Work: %v", submitted)
	}
	before := log
	if run(`{"task":"\u0072eview"}`, "submit-work", "--file", "-")["created"] != false || log != before {
		t.Fatal("identity resubmission changed age or immutable metadata")
	}
	acknowledged := run(`{"task":"review"}`, "submit-work", "--file", "-", "--request-id", "stable-submit")
	originalCommit := submitted["publication"].(map[string]any)["commit"].(map[string]any)["id"]
	acceptedCommit := acknowledged["publication"].(map[string]any)["commit"].(map[string]any)["id"]
	if originalCommit != acceptedCommit || log != before {
		t.Fatal("restarted submission did not recover the original accepted request")
	}
	originalNode := submitted["publication"].(map[string]any)["commit"].(map[string]any)["operations"].([]any)[0]
	const rootGraphID = "b6bd7716590080313f39cf62b6867584109cd484ac4c78a48d8a4160ecac4785"
	if originalNode.(map[string]any)["graph_id"] != rootGraphID ||
		originalNode.(map[string]any)["node_key"] != "root" {
		t.Fatal("public root graph differs from the canonical payload hash")
	}
	reject(`{"task":"changed"}`, "work_conflict", "submit-work", "--file", "-", "--graph-id", rootGraphID, "--node-key", "root")
	graph, err := json.Marshal([]any{originalNode})
	if err != nil {
		t.Fatal(err)
	}
	if run(string(graph), "submit-graph", "--file", "-")["changed"] != false || log != before {
		t.Fatal("exact graph resubmission changed immutable authority")
	}
	for _, field := range []string{"legacy_field", "subject"} {
		var nodes []map[string]any
		if err := json.Unmarshal(graph, &nodes); err != nil {
			t.Fatal(err)
		}
		nodes[0][field] = nil
		invalid, err := json.Marshal(nodes)
		if err != nil {
			t.Fatal(err)
		}
		reject(string(invalid), "request_invalid", "submit-graph", "--file", "-")
	}
	actorID = 1002
	reject(`{"task":"review"}`, "admission_unauthorized", "submit-work", "--file", "-")
	reject(`{"task":"review"}`, "request_reuse", "submit-work", "--file", "-", "--request-id", "stable-submit")
	actorID = 1001
	replayed := run("", "replay")
	if replayed["policy_epoch"] == "" {
		t.Fatal("mandatory Policy was omitted")
	}
	explained := run("", "explain")
	if explained["status"] != "snapshot_prediction" || log != before {
		t.Fatal("explain mutated authority")
	}
	grant := run("", "dispatch-next", "--request-id", "stable-grant")
	commit := grant["commit"].(map[string]any)
	operations := commit["operations"].([]any)
	if len(operations) != 1 || operations[0].(map[string]any)["work_id"] != id {
		t.Fatal("policy-selected causal FIFO Work did not receive Claim")
	}
	again := run("", "dispatch-next", "--request-id", "stable-grant")
	if again["changed"] != false || again["commit"].(map[string]any)["id"] != commit["id"] {
		t.Fatal("request acknowledgment recovery changed accepted batch")
	}
	claimID := operations[0].(map[string]any)["claim_id"].(string)
	before, writes := log, apiWrites
	historical := run("", "explain", "--before-claim", claimID)
	if historical["status"] != "committed_claim_prefix" || historical["commit_id"] != commit["id"] ||
		historical["selection"].(map[string]any)["work_id"] != id ||
		historical["capacity_before"].(map[string]any)["logical"] != float64(0) {
		t.Fatalf("before-Claim explanation did not replay its actual causal prefix: %v", historical)
	}
	requestExplanation := run("", "explain", "--request-id", "stable-grant")
	if requestExplanation["status"] != "committed_request" || len(requestExplanation["claims"].([]any)) != 1 {
		t.Fatal("request explanation lost accepted batch membership")
	}
	trace := run("", "trace", "--claim-id", claimID, "--limit", "2")
	if trace["status"] != "operator_live_read" || trace["tip"] != commit["id"] ||
		trace["trace_availability"] != "ledger_only" || len(trace["events"].([]any)) != 2 ||
		trace["next_offset"] != float64(2) {
		t.Fatalf("public Claim trace did not return a bounded exact page: %v", trace)
	}
	run("", "trace", "--request-id", "stable-submit")
	run("", "trace", "--claim-id", claimID, "--offset", "2")
	run("", "explain", "--work-id", id)
	if log != before || apiWrites != writes {
		t.Fatal("read-only trace/explanation made a remote mutation or changed debt/history")
	}
	if result := run("", "compact"); result["changed"] != false || apiWrites != writes {
		t.Fatal("already canonical queue was unnecessarily published")
	}
	parsed, err := workqueue.Parse([]byte(log))
	if err != nil {
		t.Fatal(err)
	}
	var physical bytes.Buffer
	for _, commit := range slices.Backward(parsed) {
		line, err := json.Marshal(commit)
		if err != nil {
			t.Fatal(err)
		}
		physical.Write(line)
		physical.WriteByte('\n')
	}
	line, err := json.Marshal(parsed[0])
	if err != nil {
		t.Fatal(err)
	}
	physical.Write(line)
	physical.WriteByte('\n')
	log = physical.String()
	admin = false
	reject("", "actor_unauthorized", "compact")
	admin = true
	compacted := run("", "compact")
	if compacted["changed"] != true || compacted["duplicates_removed"] != float64(1) || log != before {
		t.Fatalf("public compact changed a unique logical commit or lost canonical history: %v", compacted)
	}
	canonicalLog := log
	log = "{\"version\":2}\n"
	reject("", "unsupported_protocol", "compact")
	log = canonicalLog
	reject(`{"task":"review"}`, "request_reuse", "submit-work", "--file", "-", "--request-id", "stable-grant")
	run("", "control", "--name", "grants_paused", "--paused=true", "--reason", "incident")
	state := run("", "stats")
	if state["claims"] != float64(1) || state["dispatches"] != float64(1) {
		t.Fatal("pause reset charges or reservations")
	}
	run("", "cancel-work", "--work-id", id, "--reason", "abandoned")
	if run("", "stats")["dispatches"] != float64(1) {
		t.Fatal("WorkCancellation force-released possibly executing native worker")
	}
	distinct := run(`{"task":"review"}`, "submit-work", "--file", "-", "--node-key", "distinct")
	distinctNode := distinct["publication"].(map[string]any)["commit"].(map[string]any)["operations"].([]any)[0].(map[string]any)
	if distinct["created"] != true || distinct["work_id"] != workqueue.NodeID(rootGraphID, "distinct") ||
		distinctNode["graph_id"] != rootGraphID || distinctNode["node_key"] != "distinct" {
		t.Fatal("explicit same-payload distinct node was collapsed into the independent root")
	}
	distinctGraph := run(`{"task":"review"}`, "submit-work", "--file", "-", "--graph-id", "distinct")
	distinctGraphNode := distinctGraph["publication"].(map[string]any)["commit"].(map[string]any)["operations"].([]any)[0].(map[string]any)
	if distinctGraph["created"] != true || distinctGraph["work_id"] != workqueue.NodeID("distinct", "root") ||
		distinctGraphNode["graph_id"] != "distinct" || distinctGraphNode["node_key"] != "root" {
		t.Fatal("explicit same-payload distinct graph lost its requested identity")
	}
	t.Run("human atomic observation counts only Claims", func(t *testing.T) {
		run("", "control", "--name", "grants_paused", "--paused=false", "--reason", "resumed")
		node, err := workqueue.NewWork([]byte(`{"task":"observed"}`), "observed", "root", "default",
			workqueue.DefaultPolicy("1001", remote), 1000)
		if err != nil {
			t.Fatal(err)
		}
		node.Priority = 1
		node.DependsOn = []workqueue.Dependency{{
			Kind: "issue", Condition: "completed",
			Resource: &workqueue.Resource{
				Kind: "issue", Host: "github.com", Repository: remote,
				RepositoryID: "9", ResourceID: "10", Number: "7",
			},
		}}
		data, err := json.Marshal([]workqueue.WorkDefinition{node})
		if err != nil {
			t.Fatal(err)
		}
		run(string(data), "submit-graph", "--file", "-")
		for _, stage := range []string{"fresh", "acknowledgment"} {
			before := log
			cmd := NewWorkCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--repo", remote, "dispatch-next", "--request-id", "human-observed-grant"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("%s atomic observed dispatch: %v (%s)", stage, err, output.String())
			}
			if !strings.HasPrefix(output.String(), "Fair prefix: 1 Claims, 1 reserved assignments; ") {
				t.Fatalf("%s observation was counted as a Claim: %q", stage, output.String())
			}
			if stage == "acknowledgment" && log != before {
				t.Fatal("human acknowledgment changed accepted atomic dispatch")
			}
		}
		ack := run("", "dispatch-next", "--request-id", "human-observed-grant")
		operations := ack["commit"].(map[string]any)["operations"].([]any)
		if len(operations) != 2 || operations[0].(map[string]any)["kind"] != "Observation" ||
			operations[1].(map[string]any)["kind"] != "Claim" {
			t.Fatal("human summary did not cover an actual atomic Observation plus Claim")
		}
	})
}
