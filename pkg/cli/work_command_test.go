package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
)

type workAPIRoundTripper func(*http.Request) (*http.Response, error)

func (f workAPIRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
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
			result = map[string]any{"full_name": remote, "default_branch": "main", "permissions": map[string]bool{"admin": admin, "push": true}}
		case r.Method == http.MethodGet && path == "git/ref/heads/main":
			result = map[string]any{"object": map[string]string{"sha": strings.Repeat("f", 40)}}
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
	if !submitted["created"].(bool) || len(id) != 64 {
		t.Fatalf("submission did not persist current Work: %v", submitted)
	}
	before := log
	if run(`{"task":"review"}`, "submit-work", "--file", "-")["created"] != false || log != before {
		t.Fatal("identity resubmission changed age or immutable metadata")
	}
	acknowledged := run(`{"task":"review"}`, "submit-work", "--file", "-", "--request-id", "stable-submit")
	originalCommit := submitted["publication"].(map[string]any)["commit"].(map[string]any)["id"]
	acceptedCommit := acknowledged["publication"].(map[string]any)["commit"].(map[string]any)["id"]
	if originalCommit != acceptedCommit || log != before {
		t.Fatal("restarted submission did not recover the original accepted request")
	}
	originalNode := submitted["publication"].(map[string]any)["commit"].(map[string]any)["operations"].([]any)[0]
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
	for index := len(parsed) - 1; index >= 0; index-- {
		line, err := json.Marshal(parsed[index])
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
}
