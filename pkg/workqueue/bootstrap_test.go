package workqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

func TestBootstrapSubmitMatchesJavaScriptGenesis(t *testing.T) {
	policy := DefaultPolicy(testPrincipal, testRepository)
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Ref = strings.Repeat("a", 40)
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	actor := testActor("producer")
	node, err := NewWork([]byte(`{"task":"parity"}`), "bootstrap-parity", "root", "default", policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest("bootstrap-parity", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := bootstrapSubmit(actor, policy, request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{"policy": policy, "actor": actor, "request": request})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "-e", `
const fs = require("node:fs");
const {fakeGitHub} = require("../../actions/setup/js/work_queue_store_checks.cjs");
const {publishWorkQueueRequest} = require("../../actions/setup/js/work_queue_store.cjs");
const input = JSON.parse(fs.readFileSync(0, "utf8"));
const fake = fakeGitHub();
publishWorkQueueRequest({
  githubClient: fake.githubClient, owner: "owner", repo: "repo",
  actor: input.actor, context: {...input.actor, authenticated: true, roles: ["producer"]},
  request: input.request, policyProposal: input.policy, now: () => 2000,
  maxRetries: 0,
}).then(result => process.stdout.write(JSON.stringify(result.commit)))
  .catch(error => { console.error(error); process.exitCode = 1; });
`)
	command.Stdin = strings.NewReader(string(input))
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JavaScript bootstrap failed: %v", err)
	}
	var js QueueCommit
	if err := json.Unmarshal(output, &js); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(commit, js) {
		t.Fatalf("native bootstrap differs from JavaScript genesis:\nGo: %+v\nJS: %+v", commit, js)
	}
	history := []QueueCommit{commit}
	for iteration := range 2 {
		history, err = CompactCheckpoint(history, strings.Repeat("c", 40), testActor("administrator"), int64(3000+iteration))
		if err != nil {
			t.Fatal(err)
		}
		restored, err := Replay(history)
		if err != nil {
			t.Fatal(err)
		}
		if !sameJSON(restored.Requests[request.ID].Request, request) ||
			restored.Works[node.WorkID].Position.Operation != 1 {
			t.Fatal("checkpoint lost combined-genesis Work positions or request identity")
		}
		if _, _, _, err := BuildCandidate(history, actor, request, 4000); err != nil {
			t.Fatalf("checkpointed bootstrap request could not be recovered: %v", err)
		}
	}
}

func TestFirstSubmitBootstrapWithoutAdministratorPermission(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "lost acknowledgment"}[ambiguous], func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.noAdmin, mock.ambiguous = true, ambiguous
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}

			policy := DefaultPolicy(actor.Principal, actor.Repository)
			pool := policy.Pools["default"]
			profile := pool.Profiles["default"]
			profile.Ref = strings.Repeat("e", 40)
			pool.Profiles["default"] = profile
			policy.Pools["default"] = pool
			branch.PolicyProposal = &policy
			node, err := NewWork([]byte(`{"task":"first use"}`), "first-use", "root", "default", policy, 1000)
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewRequest("first-use", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
			if err != nil {
				t.Fatal(err)
			}
			published, err := branch.Publish(context.Background(), actor, request)
			if err != nil {
				t.Fatal(err)
			}
			commits, err := branch.Read(context.Background())
			if err != nil || len(commits) != 1 || len(commits[0].Operations) != 2 ||
				commits[0].Actor.Role != "producer" || !sameJSON(commits[0].Request, request) {
				t.Fatalf("first submission did not publish atomic producer genesis: %+v %v", commits, err)
			}
			state, err := Replay(commits)
			if err != nil || !sameJSON(*state.Policy, policy) || len(state.Works) != 1 ||
				mock.workerRef != profile.Ref || mock.refWrites != 1 || published.Commit == nil {
				t.Fatalf("bootstrap changed proposal or duplicated writes: %v", err)
			}
			mock.workerStatus = http.StatusNotFound
			again, err := branch.Publish(context.Background(), actor, request)
			if err != nil || again.Changed || mock.refWrites != 1 {
				t.Fatalf("replay reinitialized or reprovisioned queue: %v", err)
			}
		})
	}
}

func TestNativeDispatcherFirstSubmissionMatchesJavaScriptBootstrap(t *testing.T) {
	branch, mock := newQueueAPI(t)
	mock.noAdmin = true
	policy, err := branch.PolicyFromConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actor := testActor("dispatcher")
	node, err := NewWork([]byte(`{"task":"dispatcher first use"}`), "dispatcher-first-use", "root", "default", policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest("dispatcher-first-use", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := branch.initialSubmissionCommit(context.Background(), actor, request)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := bootstrapSubmit(actor, policy, request, commit.At)
	if err != nil || !sameJSON(commit, expected) || len(commit.Operations) != 2 ||
		mock.refWrites != 0 || len(mock.logs) != 0 {
		t.Fatalf("verified dispatcher bootstrap differs from the shared atomic genesis: %v", err)
	}
	if _, err := branch.publish(context.Background(), branchSnapshot{}, []QueueCommit{commit}); err != nil {
		t.Fatal(err)
	}
	commits, err := branch.Read(context.Background())
	if err != nil || len(commits) != 1 || !sameJSON(commits[0], commit) || mock.refWrites != 1 {
		t.Fatalf("dispatcher Policy and Work were not published atomically: %v", err)
	}
	if _, _, _, err := BuildCandidate(commits, actor, request, commit.At+1); err != nil {
		t.Fatalf("dispatcher bootstrap could not recover its accepted request: %v", err)
	}
}

func TestBootstrapProposalCannotReplaceInstalledPolicy(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits := testGenesis(t, nil)
	installMockLog(t, mock, commits)
	actor := testActor("producer")
	policy := DefaultPolicy(testPrincipal, testRepository)
	policy.Mode = "strict-priority"
	branch.PolicyProposal = &policy
	node := testNode(t, commits, "different-policy")
	request, err := NewRequest("different-policy", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
		!strings.Contains(err.Error(), "policy_proposal_mismatch") {
		t.Fatalf("installed Policy was overridden by a bootstrap proposal: %v", err)
	}
	if mock.refWrites != 0 {
		t.Fatal("mismatched proposal wrote queue state")
	}
}

func TestBootstrapRejectsUnentitledWorkBeforeNativeWrites(t *testing.T) {
	branch, mock := newQueueAPI(t)
	actor := testActor("producer")
	policy := DefaultPolicy("2002", testRepository)
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Ref = strings.Repeat("e", 40)
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	branch.PolicyProposal = &policy
	node, err := NewWork([]byte(`{"task":"unentitled"}`), "unentitled", "root", "default", policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest("unentitled", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := branch.Publish(context.Background(), actor, request); err == nil {
		t.Fatal("first submission bypassed producer entitlement")
	}
	if mock.refWrites != 0 || len(mock.logs) != 0 || mock.workerReads != 0 {
		t.Fatal("unentitled submission provisioned or published queue state")
	}
}

func TestNonSubmissionCannotBootstrapNativeQueue(t *testing.T) {
	for _, kind := range []string{"dispatch_next", "control", "policy"} {
		t.Run(kind, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			actor, err := branch.Authenticate(context.Background(), "administrator")
			if err != nil {
				t.Fatal(err)
			}
			var parameters any = DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 49152}
			if kind == "control" {
				parameters = OperationsParameters{Operations: []Operation{mustOp(t, map[string]any{
					"kind": "Control", "control": "admission_paused", "value": true, "reason": "operator",
				})}}
			}
			if kind == "policy" {
				parameters = OperationsParameters{Operations: []Operation{mustOp(t, map[string]any{
					"kind": "Policy", "epoch": "standalone", "policy": DefaultPolicy(actor.Principal, actor.Repository),
				})}}
			}
			request, err := NewRequest("no-bootstrap", kind, actor, parameters)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.Contains(err.Error(), "queue_missing") {
				t.Fatalf("non-submission initialized queue: %v", err)
			}
			if mock.refWrites != 0 || len(mock.logs) != 0 || mock.workerReads != 0 {
				t.Fatal("non-submission prepared or wrote queue state")
			}
		})
	}
}
