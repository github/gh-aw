package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
)

func TestWorkCommandDeployFromConfig(t *testing.T) {
	for _, scenario := range []string{"compatible", "incompatible", "inactive", "missing artifact", "missing stamp", "absent", "legacy", "not administrator"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("GH_TOKEN", "test-token")
			t.Setenv("GH_HOST", "github.com")
			const repository = "owner/repo"
			actor := workqueue.Actor{Role: "administrator", Principal: "1001", Repository: repository}
			policy := workqueue.DefaultPolicy("", repository)
			policy.Authorization = "aw"
			policy.Producers = map[string]workqueue.ProducerRule{}
			pool := policy.Pools["default"]
			profile := pool.Profiles["default"]
			profile.Ref = strings.Repeat("e", 40)
			profile.LogicalContract = strings.Repeat("a", 64)
			pool.Profiles = map[string]workqueue.WorkerProfile{"worker": profile}
			pool.DefaultProfile = "worker"
			policy.Pools["default"] = pool
			if scenario == "legacy" {
				policy = workqueue.DefaultPolicy(actor.Principal, repository)
			}
			genesis, err := workqueue.Genesis(actor, policy, "installed", "genesis", 1000)
			if err != nil {
				t.Fatal(err)
			}
			work, err := workqueue.NewWork([]byte(`{"task":"pending deployment"}`), "pending", "root", "default", policy, 2000)
			if err != nil {
				t.Fatal(err)
			}
			submit, _ := workqueue.NewRequest("pending", "submit", actor, workqueue.SubmitParameters{Nodes: []workqueue.WorkDefinition{work}})
			commits, _, _, err := workqueue.BuildCandidate([]workqueue.QueueCommit{genesis}, actor, submit, 2000)
			if err != nil {
				t.Fatal(err)
			}
			data, err := workqueue.Serialize(commits)
			if err != nil {
				t.Fatal(err)
			}
			ledger, pending := string(data), ""
			head := strings.Repeat("b", 40)
			writes, sourceReads, artifactReads := 0, 0, 0
			defaultRevision := strings.Repeat("f", 40)
			contract := strings.Repeat("a", 64)
			if scenario == "incompatible" {
				contract = strings.Repeat("c", 64)
			}
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "token test-token" {
					t.Error("deployment was not authenticated")
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
					result = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"sha": defaultRevision}}
				case r.Method == http.MethodGet && path == "contents/.github/workflows/worker.md":
					sourceReads++
					if r.URL.Query().Get("ref") != defaultRevision {
						t.Error("deployment source read was not pinned to the default revision")
					}
					result = file(".github/workflows/worker.md", "---\ntools:\n  work-queue:\n    worker: true\n---\nWorker")
				case r.Method == http.MethodGet && path == "contents/.github/workflows/worker.lock.yml":
					artifactReads++
					if r.URL.Query().Get("ref") != defaultRevision {
						t.Error("deployment artifact read was not pinned to the default revision")
					}
					if scenario == "missing artifact" {
						status, result = http.StatusNotFound, map[string]string{"message": "Not Found"}
						break
					}
					content := "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n"
					if scenario != "missing stamp" {
						content += "env:\n  GH_AW_WORK_QUEUE_CONTRACT: \"" + contract + "\"\n"
					}
					result = file(".github/workflows/worker.lock.yml", content)
				case r.Method == http.MethodGet && path == "actions/workflows/worker.lock.yml":
					state := "active"
					if scenario == "inactive" {
						state = "disabled_manually"
					}
					result = map[string]string{"path": ".github/workflows/worker.lock.yml", "state": state}
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
					result = map[string]string{"sha": "deployment-tree"}
				case r.Method == http.MethodPost && path == "git/commits":
					result = map[string]string{"sha": strings.Repeat("c", 40)}
				case r.Method == http.MethodPatch && path == "git/refs/heads/"+workqueue.DefaultBranch:
					ledger, head = pending, strings.Repeat("c", 40)
					result = map[string]string{"ref": "refs/heads/" + workqueue.DefaultBranch}
				default:
					t.Errorf("unexpected deployment API %s %s", r.Method, r.URL.Path)
					status, result = http.StatusNotFound, map[string]string{"message": "Not Found"}
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(bytes.NewReader(encoded)), Request: r}, nil
			})
			run := func() error {
				cmd := NewWorkCommand()
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				cmd.SetArgs([]string{"--repo", repository, "deploy", "--from-config", "--request-id", "deployment"})
				return cmd.Execute()
			}
			err = run()
			expectedError := map[string]string{"absent": "queue_missing", "legacy": "deployment_invalid", "not administrator": "actor_unauthorized"}[scenario]
			if expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), expectedError) || writes != 0 || ledger != string(data) || sourceReads != 0 {
					t.Fatalf("invalid native deployment changed queue authority: %v writes=%d sources=%d", err, writes, sourceReads)
				}
				return
			}
			if err != nil {
				t.Fatalf("pending queue could not deploy without draining: %v", err)
			}
			next, err := workqueue.Parse([]byte(ledger))
			if err != nil || len(next) != len(commits)+1 {
				t.Fatalf("deployment did not append a durable receipt: %v", err)
			}
			before, _ := workqueue.Replay(commits)
			after, err := workqueue.Replay(next)
			if err != nil {
				t.Fatal(err)
			}
			beforePolicy, _ := json.Marshal(before.Policy)
			afterPolicy, _ := json.Marshal(after.Policy)
			if !bytes.Equal(beforePolicy, afterPolicy) || before.PolicyEpoch != after.PolicyEpoch || len(after.Claims) != 0 {
				t.Fatal("deployment rewrote scheduling or dispatched Work")
			}
			last := next[len(next)-1]
			var deployment workqueue.DeploymentOperation
			if err := json.Unmarshal(last.Operations[0], &deployment); err != nil ||
				deployment.ExpectedRef != strings.Repeat("e", 40) || deployment.ExpectedContract != strings.Repeat("a", 64) ||
				last.Actor != actor || last.Request.ID != "deployment" {
				t.Fatal("deployment lost authenticated identity or CAS expectations")
			}
			explanation, err := workqueue.ExplainWork(after, work.WorkID, time.Now().UnixMilli())
			expectedReason := map[string]string{
				"compatible": "ready", "incompatible": "worker_incompatible", "inactive": "worker_unavailable",
				"missing artifact": "worker_unavailable", "missing stamp": "worker_unavailable",
			}[scenario]
			if err != nil || explanation.Reason != expectedReason {
				t.Fatalf("deployment did not preserve or pause affected Work: %+v %v", explanation, err)
			}
			previousWrites, previousSources, previousArtifacts := writes, sourceReads, artifactReads
			defaultRevision = strings.Repeat("d", 40)
			if err := run(); err != nil || writes != previousWrites || sourceReads != previousSources || artifactReads != previousArtifacts {
				t.Fatalf("committed deployment retry rediscovered moving config or changed routes: %v", err)
			}
			future := NewWorkCommand()
			future.SetOut(io.Discard)
			future.SetErr(io.Discard)
			future.SetIn(strings.NewReader(`{"task":"future work"}`))
			future.SetArgs([]string{"--repo", repository, "submit-work", "--file", "-", "--graph-id", "future",
				"--worker-profile", "worker", "--request-id", "future"})
			if err := future.Execute(); err != nil {
				t.Fatalf("native future admission did not use the deployed contract: %v", err)
			}
			futureCommits, err := workqueue.Parse([]byte(ledger))
			if err != nil {
				t.Fatal(err)
			}
			futureState, err := workqueue.Replay(futureCommits)
			if err != nil || futureState.Works[workqueue.NodeID("future", "root")].LogicalContract !=
				workqueue.FuturePolicy(after).Pools["default"].Profiles["worker"].LogicalContract {
				t.Fatalf("native future Work retained the obsolete policy contract: %v", err)
			}
			runPinned := func(pin, requestID string) error {
				cmd := NewWorkCommand()
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				cmd.SetIn(strings.NewReader(`{"task":"pinned work"}`))
				cmd.SetArgs([]string{"--repo", repository, "submit-work", "--file", "-", "--graph-id", "pinned",
					"--worker-profile", "worker", "--execution-ref", pin, "--request-id", requestID})
				return cmd.Execute()
			}
			if err := runPinned(profile.Ref, "pinned"); err != nil {
				t.Fatalf("native admission rejected the immutable historical pin: %v", err)
			}
			pinnedCommits, err := workqueue.Parse([]byte(ledger))
			if err != nil {
				t.Fatal(err)
			}
			pinnedState, err := workqueue.Replay(pinnedCommits)
			if err != nil {
				t.Fatal(err)
			}
			pinnedWork := pinnedState.Works[workqueue.NodeID("pinned", "root")]
			if pinnedWork.ExecutionRef != profile.Ref || pinnedWork.AdmissionContract != profile.LogicalContract {
				t.Fatal("native pinned Work inherited the latest incompatible deployment contract")
			}
			previousWrites = writes
			if err := runPinned(profile.Ref, "pinned"); err != nil || writes != previousWrites {
				t.Fatalf("pinned submission replay duplicated Work or deployment receipts: %v", err)
			}
			if err := runPinned(defaultRevision, "changed-pin"); err == nil || !strings.Contains(err.Error(), "work_conflict:") || writes != previousWrites {
				t.Fatalf("an explicit pin mutation silently replaced or reused the original pin: %v", err)
			}
			if err := runPinned("", "empty-pin"); err == nil || !strings.Contains(err.Error(), "work_invalid:") || writes != previousWrites {
				t.Fatalf("an empty explicit pin silently became unpinned: %v", err)
			}
		})
	}
}

func TestWorkReuseSubmissionPreservesAdmittedContract(t *testing.T) {
	old := workqueue.WorkDefinition{
		WorkID: "work", Payload: json.RawMessage(`{"task":"same"}`), LogicalContract: strings.Repeat("a", 64), Enqueued: 1000,
	}
	state := workqueue.Projection{Works: map[string]*workqueue.WorkState{
		old.WorkID: {WorkDefinition: old},
	}}
	current := old
	current.LogicalContract, current.Enqueued = strings.Repeat("b", 64), 2000
	reused, err := workReuseSubmission(state, current)
	if err != nil || reused.LogicalContract != old.LogicalContract || reused.Enqueued != old.Enqueued {
		t.Fatalf("retry refreshed immutable admitted contract after a deployment: %+v %v", reused, err)
	}
	current.Payload = json.RawMessage(`{"task":"changed"}`)
	if _, err := workReuseSubmission(state, current); err == nil {
		t.Fatal("preserving derived deployment fields accepted changed immutable payload")
	}
}

func TestWorkCommandDeployClosedFlags(t *testing.T) {
	for _, arguments := range [][]string{
		{"deploy"},
		{"deploy", "--from-config", "--file", "-"},
		{"deploy", "--from-config", "--epoch", "next"},
		{"deploy", "--from-config", "--principal", "1001"},
	} {
		cmd := NewWorkCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(arguments)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("unsafe deployment selectors accepted: %v", arguments)
		}
	}
}
