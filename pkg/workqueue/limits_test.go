package workqueue

import (
	"strings"
	"testing"
)

func TestResourceBudgetsFailBeforePublication(t *testing.T) {
	commits := testGenesis(t, nil)
	state, _ := Replay(commits)
	node := testNode(t, commits, "oversized")
	node.Payload = Op(map[string]string{"task": strings.Repeat("x", 16<<10)})
	request, _ := NewRequest("oversized", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{node}})
	if _, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000); err == nil ||
		!strings.Contains(err.Error(), "payload_limit") {
		t.Fatalf("payload budget not enforced: %v", err)
	}
	if _, err := PlanDispatch(state, DispatchParameters{Pool: "default", MaxClaims: 257, MaxDispatches: 1, MaxBytes: 48 << 10}, "r", "q", 2000); err == nil {
		t.Fatal("unbounded assignment budget accepted")
	}
	if _, err := PlanNext(Projection{}, "default", 2000); err == nil {
		t.Fatal("missing policy was treated as an empty queue")
	}
	if _, err := PlanDispatch(Projection{}, DispatchParameters{}, "r", "q", 2000); err == nil {
		t.Fatal("missing policy panicked or returned a grant")
	}
	policy := DefaultPolicy(testPrincipal, testRepository)
	policy.Limits.Operations = 1
	if err := validatePolicy(policy); err == nil {
		t.Fatal("policy cannot reserve less operation headroom than required for closure")
	}
	policy = DefaultPolicy(testPrincipal, testRepository)
	policy.AccountingWeights["\t"] = 1
	if err := validatePolicy(policy); err == nil {
		t.Fatal("unbounded/control-character account identity accepted")
	}
}

func TestLedgerAdmissionWatermarkPreservesRecovery(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		policy.Limits.LedgerBytes = 4000
	})
	node := testNode(t, commits, "a")
	request, _ := NewRequest("submit", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{node}})
	next, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	control := Op(map[string]any{"kind": "Control", "control": "admission_paused", "value": true, "reason": "limit"})
	req, _ := NewRequest("pause", "control", testActor("administrator"), OperationsParameters{Operations: []Operation{control}})
	next, _, _, err = BuildCandidate(next, testActor("administrator"), req, 3000)
	if err != nil {
		t.Fatal("closure/control cannot use its separate recovery capacity")
	}
	state, _ := Replay(next)
	other := testNode(t, next, "b")
	request, _ = NewRequest("other", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{other}})
	if _, _, _, err := BuildCandidate(next, testActor("producer"), request, 3000); err == nil {
		t.Fatal("paused/new admission consumed recovery capacity")
	}
	if !state.AdmissionPaused {
		t.Fatal("control did not persist after admission watermark")
	}
}

func TestAdmissionReservesWorstCaseFutureResultReferences(t *testing.T) {
	commits := testGenesis(t, nil)
	nodes := []WorkDefinition{}
	child := testNode(t, commits, "child")
	for index := range 9 {
		parent := testNode(t, commits, strings.Repeat("p", index+1))
		nodes = append(nodes, parent)
		child.DependsOn = append(child.DependsOn, Dependency{Kind: "work", WorkID: parent.WorkID})
	}
	// The forward-referenced Results are unknown, not empty descriptors.
	nodes = append([]WorkDefinition{child}, nodes...)
	request, _ := NewRequest("fan-in", "submit", testActor("producer"), SubmitParameters{Nodes: nodes})
	before, _ := Serialize(commits)
	if _, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000); err == nil ||
		!strings.Contains(err.Error(), "assignment_limit") {
		t.Fatalf("unknown predecessor descriptors stranded future assignment: %v", err)
	}
	after, _ := Serialize(commits)
	if string(before) != string(after) {
		t.Fatal("unpackable atomic graph modified authoritative history")
	}
}
