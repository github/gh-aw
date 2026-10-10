package workqueue

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func deploymentPolicy(policy *Policy) {
	awTestPolicy(policy)
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.LogicalContract = strings.Repeat("a", 64)
	pool.Profiles["default"] = profile
	other := profile
	other.Workflow = ".github/workflows/other.lock.yml"
	pool.Profiles["other"] = other
	policy.Pools["default"] = pool
}

func TestDeploymentHistoricalProfilesOnlyAcceptTheirExactInstalledPin(t *testing.T) {
	commits := testGenesis(t, awTestPolicy)
	profile := commits[0]
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	installed := state.Policy.Pools["default"].Profiles["default"]
	node := testNode(t, commits, "legacy-pin")
	node.ExecutionRef = installed.Ref
	commits = testSubmit(t, commits, "legacy-pin", node)
	updated, err := Replay(commits)
	if err != nil || len(updated.Deployments) != 0 {
		t.Fatalf("an explicit exact pin inferred legacy compatibility: %v", err)
	}
	selected, reason := updated.executionProfile(updated.Works[node.WorkID])
	if reason != "ready" || selected.Ref != installed.Ref || selected.LogicalContract != "" {
		t.Fatalf("legacy exact-ref reproducibility was not preserved: %+v %s", selected, reason)
	}
	foreign := testNode(t, commits, "foreign-pin")
	foreign.ExecutionRef = strings.Repeat("b", 40)
	request, _ := NewRequest("foreign-pin", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{foreign}})
	if _, _, _, err := BuildCandidate(commits, testActor("producer"), request, 3000); err == nil {
		t.Fatal("an unmarked historical worker accepted an unregistered future pin")
	}
	if !sameJSON(profile, commits[0]) {
		t.Fatal("pin intake rewrote historical policy authority")
	}
}

func deployTestWorker(t *testing.T, commits []QueueCommit, id, name, ref, contract string, available bool) []QueueCommit {
	t.Helper()
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	worker := state.Deployments["default"][name]
	profile := worker.Revisions[worker.CurrentRef].Profile
	profile.Ref, profile.LogicalContract = ref, contract
	return testOperations(t, commits, testActor("dispatcher"), id, "deployment", mustOp(t, DeploymentOperation{
		Kind: "Deployment", Pool: "default", WorkerProfile: name, ExpectedRef: worker.CurrentRef,
		ExpectedContract: worker.CurrentContract, Profile: profile, Available: available, Reason: "compiler_deployment",
	}))
}

func TestDeploymentCompatibleEvolutionPreservesFrozenRecoveryAndPins(t *testing.T) {
	commits := testGenesis(t, deploymentPolicy)
	root := testNode(t, commits, "root")
	root.Payload = json.RawMessage(`{"task":"root","effect_contract":{"kind":"none"}}`)
	pinned := testNode(t, commits, "pinned")
	oldRef := pinnedRef(commits, t)
	pinned.ExecutionRef = oldRef
	pending := testNode(t, commits, "pending")
	join := testNode(t, commits, "join")
	join.DependsOn = []Dependency{{Kind: "work", WorkID: root.WorkID}}
	commits = testSubmit(t, commits, "submit-all", root, pinned, pending, join)
	commits, grant := testGrant(t, commits, "old-grant", 1, 1)
	assignment := grant.Assignments[0]
	sender := testActor("dispatcher")
	sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
	commits = testOperations(t, commits, sender, "start-old", "dispatch", mustOp(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender, "credential_principal": "2002",
	}))
	worker := workerActor(assignment, assignment.Claims[0].Handle)
	worker.Principal = "2002"
	state, _ := Replay(commits)
	profile := state.Dispatches[assignment.DispatchID].Profile
	run := RunBinding{RunID: worker.RunID, RunAttempt: 1, Repository: worker.Repository, Workflow: profile.Workflow, Ref: profile.Ref, Principal: worker.Principal, Event: "workflow_dispatch"}
	evidence := Evidence{Kind: "reconciliation", Source: "trusted_activation", Repository: worker.Repository, Workflow: profile.Workflow, Ref: profile.Ref, Principal: worker.Principal, CheckedAt: 4000, RunID: worker.RunID, RunAttempt: 1}
	commits = testOperations(t, commits, worker, "bind-old", "dispatch", mustOp(t, map[string]any{
		"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": run, "evidence": evidence,
	}))
	before, _ := Replay(commits)
	newRef := strings.Repeat("b", 40)
	commits = deployTestWorker(t, commits, "compatible", "default", newRef, profile.LogicalContract, true)
	after, _ := Replay(commits)
	if !sameJSON(before.Works, after.Works) || !sameJSON(before.Claims, after.Claims) ||
		!sameJSON(before.Dispatches, after.Dispatches) || !sameJSON(before.Clocks, after.Clocks) ||
		!sameJSON(before.Policy, after.Policy) || after.PolicyEpoch != before.PolicyEpoch {
		t.Fatal("compatible deployment mutated Work, dependencies, reservations, frozen authority, economics or debt")
	}
	finish, _ := NewRequest("old-finish", "finish", worker, FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: worker.ClaimHandle, Outcome: "completed"})
	var err error
	commits, _, _, err = BuildCandidate(commits, worker, finish, 4000)
	if err != nil {
		t.Fatal(err)
	}
	state, _ = Replay(commits)
	evidence.Kind, evidence.Source, evidence.Receipt = "delivery", "verified_receipts", "verified-none"
	member := assignment.Claims[0]
	commits = testOperations(t, commits, testActor("reconciler"), "old-result", "result", mustOp(t, map[string]any{
		"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID, "completion_id": state.Works[member.WorkID].CompletionID,
		"descriptor": map[string]any{}, "evidence": evidence,
	}))
	evidence.Kind, evidence.Source, evidence.Status, evidence.Conclusion = "terminal_run", "github_api", "completed", "success"
	commits = testOperations(t, commits, testActor("reconciler"), "old-release", "release", mustOp(t, map[string]any{
		"kind": "Release", "dispatch_id": assignment.DispatchID, "evidence": evidence,
	}))
	commits, grant = testGrant(t, commits, "new-grant", 2, 2)
	state, _ = Replay(commits)
	refs := map[string]string{}
	for _, group := range grant.Assignments {
		dispatch := state.Dispatches[group.DispatchID]
		for _, claim := range group.Claims {
			refs[claim.WorkID] = dispatch.Profile.Ref
		}
	}
	if refs[pinned.WorkID] != oldRef || refs[pending.WorkID] != newRef {
		t.Fatalf("explicit pin or prospective unpinned route drifted: %v", refs)
	}
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("c", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	firstCheckpoint := checkpoint
	checkpoint, err = CompactCheckpoint(checkpoint, strings.Repeat("d", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Replay(checkpoint)
	if err != nil || !sameJSON(state.Works, restored.Works) || !sameJSON(state.Dispatches, restored.Dispatches) ||
		!sameJSON(state.Deployments, restored.Deployments) || !sameJSON(state.Clocks, restored.Clocks) {
		t.Fatalf("repeated checkpoint lost admission, pin, reservation, Result or debt: %v", err)
	}
	request, _ := NewRequest("submit-all", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{root, pinned, pending, join}})
	replayed, _, _, err := BuildCandidate(checkpoint, testActor("producer"), request, 4000)
	if err != nil || !sameJSON(replayed, checkpoint) {
		t.Fatalf("accepted submission did not survive deployment and repeated checkpoints: %v", err)
	}
	input, _ := canonicalValue(map[string]any{"history": commits, "checkpoint": checkpoint, "first_checkpoint": firstCheckpoint})
	command := exec.Command("node", "-e", `
const assert = require("node:assert/strict");
const q = require("../../actions/setup/js/work_queue_replay.cjs");
const {canonical} = require("../../actions/setup/js/work_queue_codec.cjs");
const {serializeDeployments} = require("../../actions/setup/js/work_queue_deployment.cjs");
const f = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const a = q.replayTransactions(f.history), b = q.replayTransactions(f.checkpoint);
for (const key of ["works", "dispatches"]) assert.equal(canonical(Object.fromEntries(a[key])), canonical(Object.fromEntries(b[key])));
assert.equal(canonical(serializeDeployments(a)), canonical(serializeDeployments(b)));
const own = q.compactTransactions(f.history, "c".repeat(40), f.checkpoint[0].actor, 4000);
const twice = q.compactTransactions(own, "d".repeat(40), f.checkpoint[0].actor, 4000);
assert.equal(canonical(own[0].request), canonical(f.first_checkpoint[0].request));
const c = q.replayTransactions(twice);
for (const key of ["works", "dispatches"]) assert.equal(canonical(Object.fromEntries(b[key])), canonical(Object.fromEntries(c[key])));
assert.equal(canonical(serializeDeployments(b)), canonical(serializeDeployments(c)));
`)
	command.Stdin = bytes.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("deployment Go/JS checkpoint parity: %v\n%s", err, output)
	}
}

func pinnedRef(commits []QueueCommit, t *testing.T) string {
	t.Helper()
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return state.Policy.Pools["default"].Profiles["default"].Ref
}

func TestDeploymentFuturePolicyCannotMutateInstalledEconomics(t *testing.T) {
	state, err := Replay(testGenesis(t, deploymentPolicy))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := canonicalValue(state.Policy)
	future := FuturePolicy(state)
	future.ClassWeights[0] = 999
	future.AccountingWeights[""] = 999
	pool := future.Pools["default"]
	pool.AllowedRepositories[0] = "foreign/repo"
	profile := pool.Profiles["default"]
	profile.EffectScope = "foreign/repo"
	pool.Profiles["default"] = profile
	after, _ := canonicalValue(state.Policy)
	if !bytes.Equal(before, after) {
		t.Fatal("future route proposal shared mutable scheduling or authorization state")
	}
}

func TestDeploymentIncompatibleUnavailableAndCallerScopePauseLocally(t *testing.T) {
	commits := testGenesis(t, deploymentPolicy)
	blocked := testNode(t, commits, "blocked")
	pinned := testNode(t, commits, "pinned")
	pinned.ExecutionRef = pinnedRef(commits, t)
	other := testNode(t, commits, "other")
	other.WorkerProfile = "other"
	commits = testSubmit(t, commits, "initial", blocked, pinned, other)
	state, _ := Replay(commits)
	commits = deployTestWorker(t, commits, "incompatible", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
	after, _ := Replay(commits)
	if _, reason := after.executionProfile(after.Works[blocked.WorkID]); reason != "worker_incompatible" {
		t.Fatal("incompatible update changed the pending Work contract instead of pausing it")
	}
	if !sameJSON(state.Works, after.Works) || !sameJSON(state.Clocks, after.Clocks) {
		t.Fatal("local pause mutated Work or fairness debt")
	}
	future, err := NewWork([]byte(`{"task":"future"}`), "graph", "future", "default", FuturePolicy(after), 4000)
	if err != nil || future.LogicalContract != strings.Repeat("b", 64) {
		t.Fatalf("new admissions did not use prospective contract: %v", err)
	}
	commits = testSubmit(t, commits, "future", future)
	commits = deployTestWorker(t, commits, "unavailable", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), false)
	after, _ = Replay(commits)
	if _, reason := after.executionProfile(after.Works[future.WorkID]); reason != "worker_unavailable" {
		t.Fatal("unavailable update did not pause only the affected Work")
	}
	params := DispatchParameters{Pool: "default", MaxClaims: 3, MaxDispatches: 3, MaxBytes: 48 << 10, WorkerProfiles: []string{"other"}}
	request, _ := NewRequest("narrow-caller", "dispatch_next", testActor("dispatcher"), params)
	commits, _, decision, err := BuildCandidate(commits, testActor("dispatcher"), request, 4000)
	if err != nil || len(decision.Assignments) != 1 || decision.Assignments[0].Claims[0].WorkID != other.WorkID {
		t.Fatalf("affected/unapproved workers failed the unrelated eligible prefix: %v %+v", err, decision)
	}
	after, _ = Replay(commits)
	selection, err := PlanNext(after, "default", 4000)
	if err != nil || selection.WorkID != pinned.WorkID {
		t.Fatalf("explicit historical pin silently updated or paused with current route: %v %+v", err, selection)
	}
	assertDeploymentCallerParity(t, commits, after)
	activate := false
	deployment := after.Deployments["default"]["default"]
	historical := deployment.Revisions[pinned.ExecutionRef].Profile
	commits = testOperations(t, commits, testActor("dispatcher"), "historical-unavailable", "deployment", mustOp(t, DeploymentOperation{
		Kind: "Deployment", Pool: "default", WorkerProfile: "default", ExpectedRef: deployment.CurrentRef,
		ExpectedContract: deployment.CurrentContract, Profile: historical, Available: false, Activate: &activate, Reason: "historical_unavailable",
	}))
	after, _ = Replay(commits)
	if after.Deployments["default"]["default"].CurrentRef != strings.Repeat("b", 40) {
		t.Fatal("historical availability change moved the current deployment")
	}
	if _, reason := after.executionProfile(after.Works[pinned.WorkID]); reason != "worker_unavailable" {
		t.Fatal("unavailable explicit historical pin was not locally paused")
	}
	current := after.Deployments["default"]["default"]
	profile := current.Revisions[current.CurrentRef].Profile
	profile.EffectScope = "foreign/repo"
	operation := mustOp(t, DeploymentOperation{Kind: "Deployment", Pool: "default", WorkerProfile: "default", ExpectedRef: current.CurrentRef, ExpectedContract: current.CurrentContract, Profile: profile, Available: true, Reason: "scope_change"})
	request, _ = NewRequest("scope-expansion", "deployment", testActor("dispatcher"), OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, testActor("dispatcher"), request, 4000); err == nil {
		t.Fatal("deployment implicitly expanded effect authority")
	}
	operation = mustOp(t, DeploymentOperation{Kind: "Deployment", Pool: "default", WorkerProfile: "default", ExpectedRef: strings.Repeat("f", 40), ExpectedContract: current.CurrentContract, Profile: current.Revisions[current.CurrentRef].Profile, Available: true, Reason: "stale_update"})
	request, _ = NewRequest("stale-cas", "deployment", testActor("dispatcher"), OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, testActor("dispatcher"), request, 4000); err == nil {
		t.Fatal("stale deployment compare-and-swap was accepted")
	}
	policy := *after.Policy
	policy.ClassWeights = []int{1, 1, 1, 1, 1}
	operation = mustOp(t, map[string]any{"kind": "Policy", "epoch": "new-economics", "policy": policy})
	request, _ = NewRequest("economic-change", "policy", testActor("administrator"), OperationsParameters{Operations: []Operation{operation}})
	if _, _, _, err := BuildCandidate(commits, testActor("administrator"), request, 4000); err == nil {
		t.Fatal("scheduling economics bypassed explicit quiescent transition")
	}
}

func assertDeploymentCallerParity(t *testing.T, commits []QueueCommit, state Projection) {
	t.Helper()
	cases := []map[string]any{}
	for _, profiles := range [][]string{nil, {}, {"default"}, {"other"}} {
		parameters := DispatchParameters{Pool: "default", MaxClaims: 3, MaxDispatches: 3, MaxBytes: 48 << 10, WorkerProfiles: profiles}
		decision, err := PlanDispatch(state, parameters, "parity-request", "parity-commit", 4000)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, map[string]any{"parameters": parameters, "decision": decision})
	}
	next, err := PlanNext(state, "default", 4000)
	if err != nil {
		t.Fatal(err)
	}
	forged := append([]QueueCommit(nil), commits...)
	parameters := DispatchParameters{Pool: "default", MaxClaims: 3, MaxDispatches: 3, MaxBytes: 48 << 10, WorkerProfiles: []string{}}
	last := &forged[len(forged)-1]
	last.Request, err = NewRequest(last.Request.ID, "dispatch_next", last.Actor, parameters)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(forged); err == nil {
		t.Fatal("replay accepted a Claim excluded by its frozen protected worker allowlist")
	}
	input, err := canonicalValue(map[string]any{
		"history": commits, "state": state, "next": next, "cases": cases, "forged": forged,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "-e", `
const assert = require("node:assert/strict");
const {canonical} = require("../../actions/setup/js/work_queue_codec.cjs");
const {replayTransactions, serializeProjection} = require("../../actions/setup/js/work_queue_replay.cjs");
const {planDispatch, planNext, selectionOnly} = require("../../actions/setup/js/work_queue_scheduler.cjs");
const input = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const state = replayTransactions(input.history);
const before = canonical(serializeProjection(state));
assert.equal(before, canonical(input.state));
assert.equal(canonical(selectionOnly(planNext(state, "default", 4000))), canonical(input.next));
for (const fixture of input.cases) {
  const decision = planDispatch(state, fixture.parameters, {requestId:"parity-request", commitId:"parity-commit", at:4000});
  assert.equal(canonical(decision), canonical(fixture.decision));
  assert.equal(canonical(serializeProjection(state)), before);
}
assert.throws(() => replayTransactions(input.forged));
`)
	command.Stdin = bytes.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("protected caller Go/JS planning/replay parity: %v\n%s", err, output)
	}
}
