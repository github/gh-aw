package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func awTestPolicy(policy *Policy) {
	policy.Authorization = "aw"
	policy.Producers = map[string]ProducerRule{}
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Principal = ""
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
}

func TestAWPolicyContractAndLegacyPrincipalRequirement(t *testing.T) {
	policy := DefaultPolicy(testPrincipal, testRepository)
	awTestPolicy(&policy)
	if err := ValidatePolicy(policy); err != nil {
		t.Fatal(err)
	}
	data, err := canonicalValue(policy)
	if err != nil || bytes.Contains(data, []byte(`"principal"`)) ||
		!bytes.Contains(data, []byte(`"authorization":"aw"`)) || !bytes.Contains(data, []byte(`"producers":{}`)) {
		t.Fatalf("AW policy retains author principal configuration: %s %v", data, err)
	}
	for _, update := range []func(*Policy){
		func(p *Policy) { p.Authorization = "" },
		func(p *Policy) { p.Authorization = "other" },
		func(p *Policy) {
			p.Producers[testPrincipal] = DefaultPolicy(testPrincipal, testRepository).Producers[testPrincipal]
		},
		func(p *Policy) {
			pool := p.Pools["default"]
			profile := pool.Profiles["default"]
			profile.Principal = "0"
			pool.Profiles["default"] = profile
			p.Pools["default"] = pool
		},
		func(p *Policy) {
			pool := p.Pools["default"]
			profile := pool.Profiles["default"]
			profile.CredentialScope = "custom"
			pool.Profiles["default"] = profile
			p.Pools["default"] = pool
		},
	} {
		p := DefaultPolicy(testPrincipal, testRepository)
		awTestPolicy(&p)
		update(&p)
		if err := ValidatePolicy(p); err == nil {
			t.Fatal("invalid AW/legacy policy escaped validation")
		}
	}
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.EffectScope = "foreign/repository"
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	if _, err := Genesis(testActor("administrator"), policy, "foreign", "foreign", 1000); err == nil {
		t.Fatal("AW policy allowed a non-derived foreign effect scope")
	}
}

func TestAWTrustedProducersRetainSchedulingAndRoleChecks(t *testing.T) {
	commits := testGenesis(t, awTestPolicy)
	for _, principal := range []string{"2002", "9007199254740993"} {
		node := testNode(t, commits, principal)
		actor := testActor("producer")
		actor.Principal = principal
		request, err := NewRequest("producer-"+principal, "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
		if err != nil {
			t.Fatal(err)
		}
		commits, _, _, err = BuildCandidate(commits, actor, request, 2000)
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := Replay(commits)
	if err != nil || len(state.Works) != 2 || len(state.Policy.Producers) != 0 {
		t.Fatalf("trusted AW producers required registration: %+v %v", state.Stats, err)
	}
	node := testNode(t, commits, "invalid")
	for _, update := range []func(*WorkDefinition){
		func(n *WorkDefinition) { n.Pool = "foreign" },
		func(n *WorkDefinition) { n.Priority = 0 },
		func(n *WorkDefinition) { n.FairnessKey = "unregistered" },
	} {
		bad := node
		update(&bad)
		if err := state.validateSubmissionEntitlement(testActor("producer"), bad); err == nil {
			t.Fatal("AW admission bypassed scheduling bounds")
		}
	}
	if err := state.validateSubmissionEntitlement(testActor("reconciler"), node); err == nil {
		t.Fatal("AW authorization bypassed operation role")
	}
	legacy, _ := Replay(testGenesis(t, nil))
	actor := testActor("producer")
	actor.Principal = "2002"
	if err := legacy.validateSubmissionEntitlement(actor, node); err == nil {
		t.Fatal("historical producer entitlements were weakened")
	}
	commits = testOperations(t, commits, actor, "aw-cancel", "cancel_work", mustOp(t, map[string]any{
		"kind": "WorkCancellation", "work_id": NodeID("graph", "2002"), "reason": "aw_authorized",
	}))
	if _, err := Replay(commits); err != nil {
		t.Fatal(err)
	}
}

func awBoundAssignment(t *testing.T) ([]QueueCommit, Assignment, Actor) {
	t.Helper()
	commits := testGenesis(t, awTestPolicy)
	work := testNode(t, commits, "root")
	work.Payload = json.RawMessage(`{"task":"root","effect_contract":{"kind":"none"}}`)
	commits = testSubmit(t, commits, "submit", work)
	commits, decision := testGrant(t, commits, "grant", 1, 1)
	assignment := decision.Assignments[0]
	sender := testActor("dispatcher")
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, commits, sender, "start", "dispatch", mustOp(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender, "credential_principal": "2002",
	}))
	state, _ := Replay(commits)
	profile := state.Dispatches[assignment.DispatchID].Profile
	worker := workerActor(assignment, assignment.Claims[0].Handle)
	worker.Principal = "2002"
	binding := RunBinding{RunID: worker.RunID, RunAttempt: 1, Repository: worker.Repository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: worker.Principal, Event: "workflow_dispatch"}
	evidence := Evidence{Kind: "reconciliation", Source: "trusted_activation", Repository: worker.Repository,
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: worker.Principal, CheckedAt: 4000,
		RunID: worker.RunID, RunAttempt: 1}
	commits = testOperations(t, commits, worker, "bind", "dispatch", mustOp(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": binding, "evidence": evidence,
	}))
	return commits, assignment, worker
}

func TestAWCredentialPrincipalBindingDeliveryAndCheckpointParity(t *testing.T) {
	commits, assignment, worker := awBoundAssignment(t)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := state.Dispatches[assignment.DispatchID]
	if dispatch.Profile.Principal != "" || dispatch.CredentialPrincipal != worker.Principal || dispatch.Run.Principal != worker.Principal {
		t.Fatal("launch replaced frozen profile or failed to retain actual native principal")
	}
	for _, principal := range []string{testPrincipal, "3003"} {
		impostor := worker
		impostor.Principal = principal
		if _, err := state.scopedClaim(impostor, assignment.DispatchID, worker.ClaimHandle); err == nil {
			t.Fatal("Claim accepted dispatcher or arbitrary actor instead of actual run principal")
		}
	}
	request, err := NewRequest("finish-aw", "finish", worker, FinishParameters{
		DispatchID: assignment.DispatchID, ClaimHandle: worker.ClaimHandle, Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, worker, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	state, _ = Replay(commits)
	child, err := NewChildWork(state, worker, []byte(`{"task":"child"}`), "child", 4000)
	if err != nil {
		t.Fatal(err)
	}
	request, err = NewRequest("aw-child", "submit", worker, SubmitParameters{Nodes: []WorkDefinition{child}})
	if err != nil {
		t.Fatal(err)
	}
	commits, _, _, err = BuildCandidate(commits, worker, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	member := assignment.Claims[0]
	evidence := Evidence{Kind: "delivery", Source: "verified_receipts", Repository: worker.Repository,
		Workflow: dispatch.Profile.Workflow, Ref: dispatch.Profile.Ref, Principal: worker.Principal,
		CheckedAt: 4000, RunID: worker.RunID, RunAttempt: 1, Receipt: "verified-none"}
	commits = testOperations(t, commits, testActor("reconciler"), "result-aw", "result", mustOp(t, map[string]any{
		"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID, "completion_id": state.Works[member.WorkID].CompletionID,
		"descriptor": map[string]any{}, "evidence": evidence,
	}))
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("a", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Replay(checkpoint)
	if err != nil || after.Dispatches[assignment.DispatchID].CredentialPrincipal != worker.Principal || after.Works[member.WorkID].Barrier != "verified" {
		t.Fatalf("checkpoint lost AW binding/delivery: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("AW parity tests require Node")
	}
	input, _ := canonicalValue(map[string]any{"history": commits, "checkpoint": checkpoint})
	command := exec.Command(node, "-e", `
const fs = require("node:fs");
const assert = require("node:assert/strict");
const q = require("../../actions/setup/js/work_queue_replay.cjs");
const {canonical} = require("../../actions/setup/js/work_queue_codec.cjs");
const fixture = JSON.parse(fs.readFileSync(0, "utf8"));
const before = q.replayTransactions(fixture.history);
const after = q.replayTransactions(fixture.checkpoint);
assert.equal(canonical(Object.fromEntries(before.dispatches)), canonical(Object.fromEntries(after.dispatches)));
const own = q.compactTransactions(fixture.history, "a".repeat(40), fixture.checkpoint[0].actor, 4000);
const expand = cp => {
  const {requests_compressed, ...state} = cp[0].operations[0].state;
  return {...state, requests: JSON.parse(require("node:zlib").inflateRawSync(Buffer.from(requests_compressed, "base64")))};
};
const differences = (a, b, path = "") => {
  if (JSON.stringify(a) === JSON.stringify(b)) return;
  if (a && b && typeof a === "object" && typeof b === "object") {
    for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) differences(a[key], b[key], path + "/" + key);
  } else console.error(path, a, b);
};
differences(expand(own), expand(fixture.checkpoint));
assert.equal(canonical(own[0].request), canonical(fixture.checkpoint[0].request));
`)
	command.Stdin = bytes.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("AW checkpoint/runtime parity: %v\n%s", err, output)
	}
}

func TestAWNativeRunChecksFrozenCredentialPrincipal(t *testing.T) {
	commits, assignment, _ := awBoundAssignment(t)
	state, _ := Replay(commits)
	branch, mock := newQueueAPI(t)
	configureNativeRun(mock, assignment)
	mock.nativeRun.Actor.ID = json.Number("2002")
	mock.nativeRun.TriggeringActor.ID = json.Number("2002")
	mock.nativeRun.DisplayTitle = "gh-aw work-queue " + assignment.DispatchID
	if _, _, err := branch.runForDispatch(context.Background(), state, state.Dispatches[assignment.DispatchID], "202"); err != nil {
		t.Fatal(err)
	}
	mock.nativeRun.Actor.ID = json.Number(testPrincipal)
	if _, _, err := branch.runForDispatch(context.Background(), state, state.Dispatches[assignment.DispatchID], "202"); err == nil {
		t.Fatal("native run principal substituted dispatcher identity for selected credential")
	}
}
