package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
)

func TestWorkCommandPolicyFromConfig(t *testing.T) {
	for _, scenario := range []string{"absent", "drained", "active", "not administrator", "invalid settings"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("GH_TOKEN", "test-token")
			t.Setenv("GH_HOST", "github.com")
			const repository = "owner/repo"
			actor := workqueue.Actor{Role: "administrator", Principal: "1001", Repository: repository}
			legacy := workqueue.DefaultPolicy(actor.Principal, repository)
			genesis, err := workqueue.Genesis(actor, legacy, "legacy", "legacy", 1000)
			if err != nil {
				t.Fatal(err)
			}
			commits := []workqueue.QueueCommit{genesis}
			if scenario == "active" {
				node, err := workqueue.NewWork([]byte(`{"task":"active"}`), "active", "root", "default", legacy, 1000)
				if err != nil {
					t.Fatal(err)
				}
				request, _ := workqueue.NewRequest("active", "submit", actor, workqueue.SubmitParameters{Nodes: []workqueue.WorkDefinition{node}})
				commits, _, _, err = workqueue.BuildCandidate(commits, actor, request, 2000)
				if err != nil {
					t.Fatal(err)
				}
			}
			data, err := workqueue.Serialize(commits)
			if err != nil {
				t.Fatal(err)
			}
			ledger, pending := string(data), ""
			head := strings.Repeat("b", 40)
			writes, configReads := 0, 0
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "token test-token" {
					t.Error("configuration update was not authenticated")
				}
				status := http.StatusOK
				var result any
				path := strings.TrimPrefix(r.URL.Path, "/repos/"+repository+"/")
				file := func(name, content string) any {
					return map[string]any{"type": "file", "path": name, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))}
				}
				if r.Method != http.MethodGet {
					writes++
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/user":
					result = map[string]any{"id": 1001}
				case r.Method == http.MethodGet && r.URL.Path == "/repos/"+repository:
					result = map[string]any{"full_name": repository, "default_branch": "main", "permissions": map[string]bool{"push": true, "admin": scenario != "not administrator"}}
				case r.Method == http.MethodGet && path == "git/ref/heads/"+workqueue.DefaultBranch:
					if scenario == "absent" {
						status, result = http.StatusNotFound, map[string]string{"message": "Not Found"}
					} else {
						result = map[string]any{"ref": "refs/heads/" + workqueue.DefaultBranch, "object": map[string]string{"sha": head}}
					}
				case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
					result = map[string]any{"tree": map[string]string{"sha": "queue-tree"}}
				case r.Method == http.MethodGet && path == "git/trees/queue-tree":
					result = map[string]any{"tree": []map[string]string{{"path": workqueue.FileName, "mode": "100644", "type": "blob", "sha": "queue-log"}}}
				case r.Method == http.MethodGet && path == "git/blobs/queue-log":
					result = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(ledger))}
				case r.Method == http.MethodGet && path == "git/ref/heads/main":
					result = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"sha": strings.Repeat("f", 40)}}
				case r.Method == http.MethodGet && path == "contents/.github/workflows/aw.json":
					configReads++
					if r.URL.Query().Get("ref") != strings.Repeat("f", 40) {
						t.Error("configuration refresh read a moving/ambient ref")
					}
					content := `{"work_queue":{"concurrency":2,"pending_limit":42}}`
					if scenario == "invalid settings" {
						content = `{"work_queue":{"concurrency":0}}`
					}
					result = file(".github/workflows/aw.json", content)
				case r.Method == http.MethodGet && path == "contents/.github/workflows":
					result = []map[string]string{{"type": "file", "path": ".github/workflows/reviewer.md"}}
				case r.Method == http.MethodGet && path == "contents/.github/workflows/reviewer.md":
					result = file(".github/workflows/reviewer.md", "---\ntools:\n  work-queue:\n    worker: true\n---\nWorker")
				case r.Method == http.MethodGet && path == "contents/.github/workflows/reviewer.lock.yml":
					result = file(".github/workflows/reviewer.lock.yml", "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n")
				case r.Method == http.MethodGet && path == "actions/workflows/reviewer.lock.yml":
					result = map[string]string{"path": ".github/workflows/reviewer.lock.yml", "state": "active"}
				case r.Method == http.MethodPost && path == "git/trees":
					var body struct {
						Tree []struct {
							Content string `json:"content"`
						} `json:"tree"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						return nil, err
					}
					pending = body.Tree[0].Content
					result = map[string]string{"sha": "prospective-tree"}
				case r.Method == http.MethodPost && path == "git/commits":
					result = map[string]string{"sha": strings.Repeat("c", 40)}
				case r.Method == http.MethodPatch && path == "git/refs/heads/"+workqueue.DefaultBranch:
					ledger, head = pending, strings.Repeat("c", 40)
					result = map[string]string{"ref": "refs/heads/" + workqueue.DefaultBranch}
				default:
					t.Errorf("unexpected configuration-update API %s %s", r.Method, r.URL.Path)
					status, result = http.StatusNotFound, map[string]string{"message": "Not Found"}
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(bytes.NewReader(encoded)), Request: r}, nil
			})
			cmd := NewWorkCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"--repo", repository, "--json", "policy", "--from-config", "--epoch", "configured", "--request-id", "configured"})
			err = cmd.Execute()
			expected := map[string]string{"absent": "queue_missing", "active": "policy_not_quiescent", "not administrator": "actor_unauthorized", "invalid settings": "work_queue.concurrency"}
			if scenario != "drained" {
				if err == nil || !strings.Contains(err.Error(), expected[scenario]) || writes != 0 || ledger != string(data) {
					t.Fatalf("unsafe config update was accepted or changed authority: %v writes=%d", err, writes)
				}
				if (scenario == "absent" || scenario == "not administrator") && configReads != 0 {
					t.Fatal("nonseeding unauthorized update discovered configuration")
				}
				return
			}
			if err != nil {
				t.Fatalf("drained configuration update failed: %v (%s)", err, output.String())
			}
			next, err := workqueue.Parse([]byte(ledger))
			if err != nil || len(next) != 2 {
				t.Fatal("configuration update did not append to historical queue")
			}
			state, err := workqueue.Replay(next)
			if err != nil || state.PolicyEpoch != "configured" || state.Policy.Authorization != "aw" ||
				state.Policy.Pools["default"].NativeLimit != 2 || state.Policy.Limits.PendingNodes != 42 ||
				next[1].Actor.Principal != "1001" || next[1].Request.ID != "configured" {
				t.Fatalf("configuration update lost authenticated identity or scheduling: %v", err)
			}
			prior, _ := json.Marshal(next[0])
			originalGenesis, _ := json.Marshal(genesis)
			if !bytes.Equal(prior, originalGenesis) {
				t.Fatal("configuration update rewrote historical legacy Policy")
			}
		})
	}
}

func TestWorkCommandPolicyFromConfigClosedFlags(t *testing.T) {
	for _, arguments := range [][]string{
		{"policy", "--from-config"},
		{"policy", "--from-config", "--epoch", "next", "--file", "-"},
	} {
		cmd := NewWorkCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(arguments)
		if err := cmd.Execute(); err == nil || (!strings.Contains(err.Error(), "--epoch") && !strings.Contains(err.Error(), "mutually exclusive")) {
			t.Fatalf("invalid policy configuration selectors were accepted: %v", err)
		}
	}
}
