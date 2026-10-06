package workqueue

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const testRepository = "owner/repo"
const testPrincipal = "1001"

func testActor(role string) Actor {
	return Actor{Role: role, Principal: testPrincipal, Repository: testRepository}
}

func testGenesis(t *testing.T, update func(*Policy)) []QueueCommit {
	t.Helper()
	policy := DefaultPolicy(testPrincipal, testRepository)
	if update != nil {
		update(&policy)
	}
	genesis, err := Genesis(testActor("administrator"), policy, "genesis", "epoch1", 1000)
	if err != nil {
		t.Fatal(err)
	}
	return []QueueCommit{genesis}
}

func testSubmit(t *testing.T, commits []QueueCommit, requestID string, nodes ...WorkDefinition) []QueueCommit {
	t.Helper()
	request, err := NewRequest(requestID, "submit", testActor("producer"), SubmitParameters{Nodes: nodes})
	if err != nil {
		t.Fatal(err)
	}
	next, _, _, err := BuildCandidate(commits, testActor("producer"), request, 2000)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func testNode(t *testing.T, commits []QueueCommit, key string) WorkDefinition {
	t.Helper()
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	work, err := NewWork([]byte(`{"task":"`+key+`"}`), "graph", key, "default", *state.Policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func testGrant(t *testing.T, commits []QueueCommit, id string, claims, dispatches int) ([]QueueCommit, Decision) {
	t.Helper()
	actor := testActor("administrator")
	request, err := NewRequest(id, "dispatch_next", actor, DispatchParameters{
		Pool: "default", MaxClaims: claims, MaxDispatches: dispatches, MaxBytes: 48 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	next, _, decision, err := BuildCandidate(commits, actor, request, 3000)
	if err != nil {
		t.Fatal(err)
	}
	return next, decision
}

func testOperations(t *testing.T, commits []QueueCommit, actor Actor, id, kind string, operations ...Operation) []QueueCommit {
	t.Helper()
	request, err := NewRequest(id, kind, actor, OperationsParameters{Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	next, _, _, err := BuildCandidate(commits, actor, request, 4000)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestCausalReplayAndCanonicalDedup(t *testing.T) {
	commits := testGenesis(t, nil)
	a, b := testNode(t, commits, "a"), testNode(t, commits, "b")
	a.Enqueued, b.Enqueued = 90000, 0
	commits = testSubmit(t, commits, "submit", a, b)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := PlanNext(state, "default", 2000)
	if err != nil || selection.WorkID != a.WorkID {
		t.Fatalf("FIFO must use causal position, not client clocks: %v %v", selection, err)
	}
	before, _ := canonicalValue(state)
	for range 3 {
		if _, err := PlanNext(state, "default", 2000); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := canonicalValue(state)
	if !bytes.Equal(before, after) {
		t.Fatal("no-grant preview mutated projection")
	}
	commits, decision := testGrant(t, commits, "grant", 2, 2)
	if len(decision.Assignments) != 2 || len(decision.Operations) != 2 {
		t.Fatalf("default profile must reserve one Claim per worker: %+v", decision)
	}
	commits = append(commits, commits[1])
	slices.Reverse(commits)
	replayed, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	compacted, err := Compact(commits)
	if err != nil || len(compacted) != 3 || replayed.Stats.Claims != 2 {
		t.Fatalf("causal replay/dedup failed: %v %v", replayed.Stats, err)
	}
	serialized, err := Serialize(compacted)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(serialized)
	if err != nil {
		t.Fatal(err)
	}
	state2, err := Replay(parsed)
	if err != nil || !sameJSON(replayed, state2) {
		t.Fatal("canonical cold replay changed authoritative projection")
	}
}

func TestLedgerRejectsBrokenCausalAndAuthority(t *testing.T) {
	base := testGenesis(t, nil)
	node := testNode(t, base, "a")
	valid := testSubmit(t, base, "submit", node)
	tests := map[string]func([]QueueCommit) []QueueCommit{
		"missing genesis": func(c []QueueCommit) []QueueCommit { return c[1:] },
		"missing predecessor": func(c []QueueCommit) []QueueCommit {
			missing := "absent"
			c[1].Previous = &missing
			return c
		},
		"fork": func(c []QueueCommit) []QueueCommit {
			other := c[1]
			other.ID = "fork"
			return append(c, other)
		},
		"duplicate conflicting ID": func(c []QueueCommit) []QueueCommit {
			other := c[1]
			other.At++
			return append(c, other)
		},
		"policy epoch": func(c []QueueCommit) []QueueCommit { c[1].PolicyEpoch = "wrong"; return c },
		"policyless":   func(c []QueueCommit) []QueueCommit { c[0].Operations = []Operation{Op(node)}; return c },
		"unauthorized actor": func(c []QueueCommit) []QueueCommit {
			c[1].Actor.Role = "reconciler"
			c[1].Request.Fingerprint, _ = Fingerprint(c[1].Actor, c[1].Request.Kind, c[1].Request.Parameters)
			return c
		},
		"fingerprint": func(c []QueueCommit) []QueueCommit { c[1].Request.Fingerprint = strings.Repeat("0", 64); return c },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Replay(mutate(slices.Clone(valid))); err == nil {
				t.Fatal("accepted invalid causal ledger or provenance")
			}
		})
	}
}

func TestCanonicalRejectsAmbiguousJSON(t *testing.T) {
	for _, input := range []string{
		`{"x":1,"x":2}`, `{"x":{"a":1,"a":2}}`, `{"x":9007199254740992}`,
		`{"x":1.5}`, `{"x":1e2}`, `{"x":-0}`, `{"x":"\ud800"}`, `{"x":"\udc00"}`,
		`{} {}`, string([]byte{'{', '"', 0xff, '"', ':', '1', '}'}),
	} {
		if _, err := Canonical([]byte(input)); err == nil {
			t.Fatalf("accepted ambiguous/noncanonical input %q", input)
		}
	}
	got, err := Canonical([]byte(`{"z":"<>&","é":2,"a":1,"pair":"\ud83d\ude00"}`))
	if err != nil || string(got) != `{"a":1,"pair":"😀","z":"<>&","é":2}` {
		t.Fatalf("canonical UTF-8 encoding %s: %v", got, err)
	}
	literal := []byte(`{"escaped":"\\u2028","separator":"\u2028","paragraph":"\u2029"}`)
	encoded, err := Canonical(literal)
	if err != nil {
		t.Fatal(err)
	}
	var original, roundTrip map[string]string
	_ = json.Unmarshal(literal, &original)
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || !sameJSON(original, roundTrip) {
		t.Fatalf("literal Unicode escape text was corrupted: %s %v", encoded, err)
	}
}

func TestCurrentOnlyParsing(t *testing.T) {
	for _, input := range []string{
		"", "{}\n", "{\"version\":2,\"kind\":\"Work\",\"work\":\"x\"}\n",
		"{\"kind\":\"Work\",\"work_id\":\"x\",\"work\":{}}\n",
		"{\"version\":3,\"kind\":\"Work\"}\n",
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("accepted legacy/incomplete ledger %q", input)
		}
	}
	commits := testGenesis(t, nil)
	data, _ := Serialize(commits)
	if _, err := Parse(bytes.TrimSuffix(data, []byte{'\n'})); err == nil {
		t.Fatal("accepted truncated final line")
	}
	if _, err := Parse(append(data, '\n')); err == nil {
		t.Fatal("accepted blank trailing record")
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	raw["extra"] = "authority"
	modified, _ := json.Marshal(raw)
	if _, err := Parse(append(modified, '\n')); err == nil {
		t.Fatal("accepted unknown envelope authority")
	}
}

func TestGraphAtomicForwardReferencesCyclesAndSharedGates(t *testing.T) {
	base := testGenesis(t, nil)
	root, child := testNode(t, base, "root"), testNode(t, base, "child")
	child.DependsOn = []Dependency{{Kind: "work", WorkID: root.WorkID}}
	commits := testSubmit(t, base, "forward", child, root)
	state, _ := Replay(commits)
	selected, _ := PlanNext(state, "default", 2000)
	if selected.WorkID != root.WorkID {
		t.Fatal("forward reference was not atomically resolved")
	}
	cycle := root
	cycle.DependsOn = []Dependency{{Kind: "work", WorkID: child.WorkID}}
	request, _ := NewRequest("cycle", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{child, cycle}})
	if _, _, _, err := BuildCandidate(base, testActor("producer"), request, 2000); err == nil ||
		!strings.Contains(err.Error(), "dependency_cycle") || !strings.Contains(err.Error(), "child") {
		t.Fatalf("cycle must have concrete path: %v", err)
	}
	gate := Resource{Kind: "issue", Host: "github.com", Repository: testRepository, RepositoryID: "1", ResourceID: "2", Number: "7"}
	a, b := testNode(t, base, "a"), testNode(t, base, "b")
	a.DependsOn, b.DependsOn = []Dependency{{Kind: "issue", Resource: &gate, Condition: "completed"}},
		[]Dependency{{Kind: "issue", Resource: &gate, Condition: "completed"}}
	commits = testSubmit(t, base, "gates", a, b)
	state, _ = Replay(commits)
	if state.Stats.Nodes != 3 {
		t.Fatalf("shared external gates must count once: %d", state.Stats.Nodes)
	}
	observation := Observation{
		Kind: "Observation", ObservationID: "o1", Resource: gate, Condition: "completed", State: "ready",
		ObservedAt: 4000, CredentialGeneration: "initial", ReadStatus: "ok", StateReason: "completed", ResourceState: "closed",
	}
	commits = testOperations(t, commits, testActor("reconciler"), "observe", "observe", Op(observation))
	state, _ = Replay(commits)
	selected, _ = PlanNext(state, "default", 4000)
	if selected.WorkID != a.WorkID || !slices.Equal(selected.Observations, []string{"o1"}) {
		t.Fatalf("typed ready observation not bound: %+v", selected)
	}
	selected, _ = PlanNext(state, "default", 64001)
	if selected.WorkID != "" {
		t.Fatal("stale external readiness authorized a Claim")
	}
}

func TestControlDoesNotResetDebtAndPolicyRequiresDrain(t *testing.T) {
	commits := testGenesis(t, nil)
	a, b := testNode(t, commits, "a"), testNode(t, commits, "b")
	commits = testSubmit(t, commits, "submit", a, b)
	commits, _ = testGrant(t, commits, "grant", 1, 1)
	before, _ := Replay(commits)
	commits = testOperations(t, commits, testActor("administrator"), "pause", "control",
		Op(map[string]any{"kind": "Control", "control": "grants_paused", "value": true, "reason": "incident"}))
	paused, _ := Replay(commits)
	selected, _ := PlanNext(paused, "default", 4000)
	if selected.Reason != "grants_paused" || !sameJSON(before.Clocks, paused.Clocks) {
		t.Fatal("pause reset debt or granted work")
	}
	commits = testOperations(t, commits, testActor("administrator"), "resume", "control",
		Op(map[string]any{"kind": "Control", "control": "grants_paused", "value": false, "reason": "restored"}))
	resumed, _ := Replay(commits)
	if !sameJSON(before.Clocks, resumed.Clocks) {
		t.Fatal("resume reset fairness debt")
	}
	policy := *resumed.Policy
	policy.Mode = "strict-priority"
	request, _ := NewRequest("reset", "policy", testActor("administrator"), OperationsParameters{Operations: []Operation{
		Op(map[string]any{"kind": "Policy", "epoch": "epoch2", "policy": policy}),
	}})
	if _, _, _, err := BuildCandidate(commits, testActor("administrator"), request, 5000); err == nil ||
		!strings.Contains(err.Error(), "policy_not_quiescent") {
		t.Fatalf("live policy reset accepted: %v", err)
	}
}
