package workqueue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func mixedContractQueue(t *testing.T) ([]QueueCommit, []WorkDefinition) {
	t.Helper()
	commits := testGenesis(t, deploymentPolicy)
	nodes := []WorkDefinition{}
	for index := range 9 {
		nodes = append(nodes, testNode(t, commits, string(rune('a'+index))))
	}
	commits = testSubmit(t, commits, "legacy-nine", nodes...)
	commits = deployTestWorker(t, commits, "new-contract", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
	newer := testNode(t, commits, "newer-one")
	newer.LogicalContract = strings.Repeat("b", 64)
	nodes = append(nodes, newer)
	return testSubmit(t, commits, "newer-one", newer), nodes
}

func TestDeploymentMixedContractRoutesPreserveExactAuthorityAndCheckpointParity(t *testing.T) {
	commits, nodes := mixedContractQueue(t)
	before, _ := Replay(commits)
	next := before.Deployments["default"]["default"].Revisions[before.Deployments["default"]["default"].CurrentRef].Profile
	next.Ref, next.LogicalContract = strings.Repeat("c", 40), strings.Repeat("a", 64)
	routes, err := contractRoutesForDeployment(before, "default", "default", next)
	if err != nil || !sameJSON(routes, map[string]string{strings.Repeat("b", 64): strings.Repeat("b", 40)}) {
		t.Fatalf("mixed admission routes: %v %v", routes, err)
	}
	commits = testOperations(t, commits, testActor("dispatcher"), "restore-default", "deployment", mustOp(t, DeploymentOperation{
		Kind: "Deployment", Pool: "default", WorkerProfile: "default", ExpectedRef: strings.Repeat("b", 40),
		ExpectedContract: strings.Repeat("b", 64), Profile: next, Available: true, Reason: "compiler_deployment", ContractRoutes: &routes,
	}))
	after, _ := Replay(commits)
	if !sameJSON(before.Works, after.Works) || !sameJSON(before.Clocks, after.Clocks) || !sameJSON(before.Policy, after.Policy) {
		t.Fatal("automatic routing rewrote Work, economics or admission policy")
	}
	for index, node := range nodes {
		selected, reason := after.executionProfile(after.Works[node.WorkID])
		ref, contract := next.Ref, next.LogicalContract
		if index == 9 {
			ref, contract = strings.Repeat("b", 40), strings.Repeat("b", 64)
		}
		if reason != "ready" || selected.Ref != ref || selected.LogicalContract != contract {
			t.Fatalf("item %d lost exact admitted authority: %+v %s", index, selected, reason)
		}
	}
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("d", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := CompactCheckpoint(checkpoint, strings.Repeat("e", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Replay(twice)
	if err != nil || !sameJSON(after.Deployments, restored.Deployments) {
		t.Fatalf("retained routes did not survive repeated checkpointing: %v", err)
	}
	retained, err := contractRoutesForDeployment(restored, "default", "default", next)
	if err != nil || !sameJSON(routes, retained) {
		t.Fatalf("compacted activation history lost its exact route: %v", err)
	}
	input, _ := canonicalValue(map[string]any{"history": commits, "checkpoint": checkpoint, "twice": twice})
	command := exec.Command("node", "-e", `
const assert = require("node:assert/strict");
const q = require("../../actions/setup/js/work_queue_replay.cjs");
const d = require("../../actions/setup/js/work_queue_deployment.cjs");
const {canonical} = require("../../actions/setup/js/work_queue_codec.cjs");
const f = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const a = q.replayTransactions(f.history), b = q.replayTransactions(f.twice);
assert.equal(canonical(d.serializeDeployments(a)), canonical(d.serializeDeployments(b)));
const own = q.compactTransactions(f.history, "d".repeat(40), f.checkpoint[0].actor, 4000);
assert.equal(canonical(own[0].request), canonical(f.checkpoint[0].request));
const next = d.futurePolicy(b).pools.default.profiles.default;
assert.equal(canonical(d.contractRoutesForDeployment(b, "default", "default", next)), canonical({["b".repeat(64)]: "b".repeat(40)}));
for (const w of b.works.values()) {
  const selected = d.executionProfile(b, w);
  assert.equal(selected.reason, "ready");
  assert.equal(selected.profile.logical_contract, w.admission_contract);
}
`)
	command.Stdin = bytes.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("mixed-route Go/JS parity: %v\n%s", err, output)
	}
	commits, grant := testGrant(t, commits, "mixed-grant", 10, 10)
	granted, _ := Replay(commits)
	for _, assignment := range grant.Assignments {
		profile := granted.Dispatches[assignment.DispatchID].Profile
		for _, member := range assignment.Claims {
			if granted.Works[member.WorkID].AdmissionContract != profile.LogicalContract {
				t.Fatal("packing mixed incompatible contracts into one frozen assignment")
			}
		}
	}
}

func TestDeploymentContractRoutesNeverFallbackOrAcceptInvalidBindings(t *testing.T) {
	commits, _ := mixedContractQueue(t)
	commits = deployTestWorker(t, commits, "last-activation", "default", strings.Repeat("d", 40), strings.Repeat("b", 64), false)
	state, _ := Replay(commits)
	next := state.Policy.Pools["default"].Profiles["default"]
	routes, err := contractRoutesForDeployment(state, "default", "default", next)
	if err != nil || routes[strings.Repeat("b", 64)] != strings.Repeat("d", 40) {
		t.Fatalf("unavailability fell back to an older activation: %v %v", routes, err)
	}
	for _, invalid := range []map[string]string{nil, {"bad": strings.Repeat("b", 40)}, {strings.Repeat("a", 64): strings.Repeat("b", 40)}, {strings.Repeat("b", 64): strings.Repeat("f", 40)}} {
		if err := validateContractRoutes(invalid, state.Deployments["default"]["default"].Revisions); err == nil {
			t.Fatalf("accepted invalid binding %v", invalid)
		}
	}
}

func TestNativeDeploymentAutomaticallyResolvesNinePlusOneAdmissionContracts(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, nodes := mixedContractQueue(t)
	installMockLog(t, mock, commits)
	state, _ := Replay(commits)
	mock.workerContent = bridgeStampedWorker(strings.Repeat("a", 64))
	mock.workerRevisions = map[string]string{
		".github/workflows/worker.lock.yml\n" + strings.Repeat("c", 40): bridgeStampedWorker(strings.Repeat("b", 64)),
	}
	installBridgeApproval(t, mock, map[string]string{strings.Repeat("b", 64): strings.Repeat("c", 40)})
	operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "default")
	if err != nil || len(operations) != 2 {
		t.Fatalf("automatic native migration proposal: %v (%d operations)", err, len(operations))
	}
	var operation DeploymentOperation
	if err := json.Unmarshal(operations[1], &operation); err != nil || operation.ContractRoutes == nil {
		t.Fatalf("native migration omitted exact routes: %v", err)
	}
	actor := testActor("administrator")
	request, _ := NewRequest("native-mixed", "deployment", actor, OperationsParameters{Operations: operations})
	if _, err := branch.Publish(context.Background(), actor, request); err != nil {
		t.Fatal(err)
	}
	log, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Replay(log)
	if err != nil || !sameJSON(state.Works, updated.Works) {
		t.Fatalf("native migration changed Work: %v", err)
	}
	for _, node := range nodes {
		profile, reason := updated.executionProfile(updated.Works[node.WorkID])
		if reason != "ready" || profile.LogicalContract != updated.Works[node.WorkID].AdmissionContract {
			t.Fatalf("native migration failed exact authority: %+v %s", profile, reason)
		}
	}

}

func TestNativeDeploymentRetainedAvailabilityChangesAtomically(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits, nodes := mixedContractQueue(t)
	installMockLog(t, mock, commits)
	state, _ := Replay(commits)
	mock.workerContent = bridgeStampedWorker(strings.Repeat("a", 64))
	historicalKey := ".github/workflows/worker.lock.yml\n" + strings.Repeat("b", 40)
	installBridgeApproval(t, mock, map[string]string{strings.Repeat("b", 64): strings.Repeat("b", 40)})
	mock.workerRevisions = map[string]string{historicalKey: strings.Replace(bridgeStampedWorker(strings.Repeat("b", 64)), `          GH_AW_WORK_QUEUE_CONTRACT: "`+strings.Repeat("b", 64)+`"`+"\n", "", 1)}
	for index, available := range []bool{false, true} {
		if available {
			mock.workerRevisions[historicalKey] = bridgeStampedWorker(strings.Repeat("b", 64))
		}

		operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "default")
		expectedOperations := 2
		if available {
			expectedOperations = 1
		}
		if err != nil || len(operations) != expectedOperations {
			t.Fatalf("availability=%v proposal: %v (%d operations)", available, err, len(operations))
		}
		actor := testActor("administrator")
		request, _ := NewRequest("availability-"+string(rune('a'+index)), "deployment", actor, OperationsParameters{Operations: operations})
		if _, err := branch.Publish(context.Background(), actor, request); err != nil {
			t.Fatalf("availability=%v failed atomic publication: %v", available, err)
		}
		log, err := branch.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		state, err = Replay(log)
		if err != nil {
			t.Fatal(err)
		}
		if _, reason := state.executionProfile(state.Works[nodes[9].WorkID]); (reason == "ready") != available {
			t.Fatalf("retained availability=%v selected reason=%s", available, reason)
		}
		if _, reason := state.executionProfile(state.Works[nodes[0].WorkID]); reason != "ready" {
			t.Fatalf("retained availability paused restored default: %s", reason)
		}
	}
}

func installBridgeApproval(t *testing.T, mock *queueAPI, routes map[string]string) {
	t.Helper()
	mock.bridgeReaders = map[string]string{
		"actions/setup/js/work_queue_replay.cjs":     "reviewed_replay",
		"actions/setup/js/work_queue_deployment.cjs": "reviewed_deployment",
	}
	digests := map[string]string{}
	for path, content := range mock.bridgeReaders {
		digest := sha256.Sum256([]byte(content))
		digests[path] = hex.EncodeToString(digest[:])
	}
	approval, err := json.Marshal(contractBridgeApproval{Version: 1, Readers: digests, Routes: map[string]map[string]string{".github/workflows/worker.lock.yml": routes}})
	if err != nil {
		t.Fatal(err)
	}
	mock.bridgeApproval = string(approval)
}

func bridgeStampedWorker(contract string) string {
	return stampedWorker(contract) + "      - uses: actions/checkout@" + strings.Repeat("a", 40) + "\n      - uses: ./actions/setup\n"
}

func TestCompiledBridgeRuntimeProvenance(t *testing.T) {
	base := bridgeStampedWorker(strings.Repeat("a", 64))
	for name, content := range map[string]string{
		"local":       base,
		"exact ref":   strings.Replace(base, "      - uses: ./actions/setup", "        with:\n          ref: "+strings.Repeat("b", 40)+"\n      - uses: ./actions/setup", 1),
		"trigger sha": strings.Replace(base, "      - uses: ./actions/setup", "        with:\n          ref: ${{ github.sha }}\n      - uses: ./actions/setup", 1),
	} {
		if !compiledBridgeRuntime([]byte(content), testRepository, strings.Repeat("b", 40)) {
			t.Fatalf("rejected supported %s loader", name)
		}
	}
	for name, content := range map[string]string{
		"foreign repository":   strings.Replace(base, "      - uses: ./actions/setup", "        with:\n          repository: foreign/repository\n      - uses: ./actions/setup", 1),
		"other ref":            strings.Replace(base, "      - uses: ./actions/setup", "        with:\n          ref: "+strings.Repeat("c", 40)+"\n      - uses: ./actions/setup", 1),
		"other path":           strings.Replace(base, "      - uses: ./actions/setup", "        with:\n          path: elsewhere\n      - uses: ./actions/setup", 1),
		"release setup":        strings.Replace(base, "./actions/setup", "github/gh-aw/actions/setup@"+strings.Repeat("d", 40), 1),
		"mixed external setup": base + "  external:\n    steps:\n      - uses: github/gh-aw-actions/setup@" + strings.Repeat("d", 40) + "\n",
		"mutable checkout":     strings.Replace(base, "actions/checkout@"+strings.Repeat("a", 40), "actions/checkout@main", 1),
		"script loader":        strings.Replace(base, "      - uses: ./actions/setup", "      - run: echo replace-actions\n      - uses: ./actions/setup", 1),
		"no loader":            stampedWorker(strings.Repeat("a", 64)),
	} {
		if compiledBridgeRuntime([]byte(content), testRepository, strings.Repeat("b", 40)) {
			t.Fatalf("accepted unsupported %s loader", name)
		}
	}
}

func TestNativeContractBridgeReaderAndApprovalFailuresPublishNothing(t *testing.T) {
	for _, scenario := range []string{"missing mapping", "duplicate key", "null routes", "wrong reader", "wrong runtime", "mixed runtime", "old default", "old pin", "active assignment", "unapproved registration"} {
		t.Run(scenario, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits, _ := mixedContractQueue(t)
			oldRef, bridgeRef := strings.Repeat("b", 40), strings.Repeat("c", 40)
			if scenario == "old pin" {
				pin := testNode(t, commits, "exact-pin")
				pin.LogicalContract, pin.ExecutionRef = strings.Repeat("b", 64), oldRef
				commits = testSubmit(t, commits, "exact-pin", pin)
			}
			if scenario == "active assignment" {
				commits, _ = testGrant(t, commits, "active-newer", 1, 1)
			}
			installMockLog(t, mock, commits)
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			before := state
			mock.workerContent = bridgeStampedWorker(strings.Repeat("a", 64))
			mock.workerRevisions = map[string]string{
				".github/workflows/worker.lock.yml\n" + bridgeRef: bridgeStampedWorker(strings.Repeat("b", 64)),
				".github/workflows/worker.lock.yml\n" + oldRef:    bridgeStampedWorker(strings.Repeat("b", 64)),
			}

			installBridgeApproval(t, mock, map[string]string{strings.Repeat("b", 64): bridgeRef})
			reader := "actions/setup/js/work_queue_replay.cjs"
			switch scenario {
			case "missing mapping":
				installBridgeApproval(t, mock, map[string]string{})
			case "duplicate key":
				mock.bridgeApproval = strings.Replace(mock.bridgeApproval, `"version":1`, `"version":1,"version":1`, 1)
			case "null routes":
				mock.bridgeApproval = `{"version":1,"readers":{},"routes":null}`
			case "wrong reader":
				mock.bridgeReaders[reader+"\n"+bridgeRef] = "unreviewed_reader"
			case "wrong runtime":
				key := ".github/workflows/worker.lock.yml\n" + bridgeRef
				mock.workerRevisions[key] = strings.Replace(mock.workerRevisions[key], "./actions/setup", "github/gh-aw/actions/setup@"+strings.Repeat("d", 40), 1)
			case "mixed runtime":
				key := ".github/workflows/worker.lock.yml\n" + bridgeRef
				mock.workerRevisions[key] += "  external:\n    steps:\n      - uses: github/gh-aw-actions/setup@" + strings.Repeat("d", 40) + "\n"
			case "old default":
				mock.bridgeReaders[reader+"\n"+state.Policy.Pools["default"].Profiles["other"].Ref] = "old_reader"
			case "old pin", "active assignment":
				mock.bridgeReaders[reader+"\n"+oldRef] = "old_reader"
			case "unapproved registration":
				profile := state.Policy.Pools["default"].Profiles["default"]
				profile.Ref, profile.LogicalContract = strings.Repeat("d", 40), strings.Repeat("b", 64)
				operation := mustOp(t, DeploymentOperation{
					Kind: "Deployment", Pool: "default", WorkerProfile: "default", ExpectedRef: oldRef,
					ExpectedContract: strings.Repeat("b", 64), Profile: profile,
					Available: true, Activate: new(bool), Register: true, Reason: "contract_bridge",
				})
				request, _ := NewRequest("unapproved-bridge", "deployment", testActor("administrator"), OperationsParameters{Operations: []Operation{operation}})
				if _, err := branch.Publish(context.Background(), testActor("administrator"), request); err == nil {
					t.Fatal("accepted unapproved bridge")
				}
				if mock.refWrites != 0 {
					t.Fatal("failed registration mutated queue")
				}
				return
			}
			if _, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "default"); err == nil {
				t.Fatal("accepted incompatible or unapproved bridge transition")
			}
			if mock.refWrites != 0 || !sameJSON(before.Deployments, state.Deployments) || !sameJSON(before.Works, state.Works) {
				t.Fatal("failed bridge preflight mutated authority")
			}
		})
	}
}
