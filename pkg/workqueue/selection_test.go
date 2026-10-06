package workqueue

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFairPrefixNeverBackfillsProfileX_Y_X(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		pool := policy.Pools["default"]
		x := pool.Profiles["default"]
		x.MaxClaims = 16
		y := x
		y.Workflow = ".github/workflows/other.lock.yml"
		pool.Profiles["x"], pool.Profiles["y"] = x, y
		policy.Pools["default"] = pool
	})
	a, b, c := testNode(t, commits, "a"), testNode(t, commits, "b"), testNode(t, commits, "c")
	a.WorkerProfile, b.WorkerProfile, c.WorkerProfile = "x", "y", "x"
	commits = testSubmit(t, commits, "submit", a, b, c)
	_, decision := testGrant(t, commits, "grant", 3, 1)
	if len(decision.Operations) != 1 || decision.Reason != "dispatch_budget_blocked" || decision.Next.WorkID != b.WorkID {
		t.Fatalf("packer bypassed fair winner: %+v", decision)
	}
}

func TestWeightedAccountAndClassExactIntegerService(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		policy.AccountingWeights["a"], policy.AccountingWeights["b"] = 2, 1
		rule := policy.Producers[testPrincipal]
		rule.FairnessKeys = []string{"", "a", "b"}
		policy.Producers[testPrincipal] = rule
		pool := policy.Pools["default"]
		pool.LogicalLimit, pool.NativeLimit = 4096, 4096
		profile := pool.Profiles["default"]
		profile.MaxClaims, profile.ShareKeys = 16, true
		pool.Profiles["default"] = profile
		policy.Pools["default"] = pool
	})
	nodes := []WorkDefinition{}
	for i := 0; i < 18; i++ {
		node := testNode(t, commits, fmt.Sprintf("a%d", i))
		node.FairnessKey = "a"
		nodes = append(nodes, node)
		node = testNode(t, commits, fmt.Sprintf("b%d", i))
		node.FairnessKey = "b"
		nodes = append(nodes, node)
	}
	commits = testSubmit(t, commits, "submit", nodes...)
	state, _ := Replay(commits)
	decision, err := PlanDispatch(state, DispatchParameters{Pool: "default", MaxClaims: 18, MaxDispatches: 2, MaxBytes: 48 << 10}, "grant", "commit", 3000)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	expected := []string{"a", "a", "b", "a", "a", "b"}
	for i, op := range decision.Operations {
		var claim ClaimOperation
		_ = json.Unmarshal(op, &claim)
		key := state.Works[claim.WorkID].FairnessKey
		counts[key]++
		if i < len(expected) && key != expected[i] {
			t.Fatalf("independent expected 2:1 prefix differs at %d: %s", i, key)
		}
	}
	if counts["a"] != 12 || counts["b"] != 6 {
		t.Fatalf("exact opportunity share: %v", counts)
	}
}

func TestBigIntegerScaleAndReactivationClampsWithoutGoalpostMovement(t *testing.T) {
	weights := map[string]int{"a": 997, "b": 991, "c": 983, "d": 977, "e": 971, "f": 967, "": 1}
	if tickScale(weights).Cmp(big.NewInt(9007199254740991)) <= 0 {
		t.Fatal("fixture must exercise arithmetic above JavaScript exact numbers")
	}
	clock := newClock()
	clock.V = "900719925474099100000000000000000000"
	key, joined := pick(clock, []string{"a", "b"}, weights, false)
	if key != "a" || integer(joined.V).Cmp(integer(clock.V)) <= 0 {
		t.Fatal("exact integer join clamp did not advance correctly")
	}
	oldB := joined.Pass["b"]
	_, next := pick(joined, []string{"a", "b"}, weights, false)
	if next.V != oldB && next.Pass["b"] != oldB {
		t.Fatal("unselected continuously eligible account goalpost moved")
	}
	_, absent := pick(next, []string{"a"}, weights, false)
	_, returned := pick(absent, []string{"a", "b"}, weights, false)
	floor := new(big.Int).Add(integer(absent.V),
		new(big.Int).Div(tickScale(weights), big.NewInt(int64(weights["b"]))))
	if integer(returned.Pass["b"]).Cmp(floor) < 0 {
		t.Fatal("reactivated account accumulated idle credit")
	}
}

func TestSharedCanonicalSelectionFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "work-queue", "fixtures", "selection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name           string         `json:"name"`
		Mode           string         `json:"mode"`
		ClassWeights   []int          `json:"class_weights"`
		AccountWeights map[string]int `json:"accounting_weights"`
		Nodes          []struct {
			Key      string `json:"key"`
			Priority int    `json:"priority"`
			Account  string `json:"account"`
			Profile  string `json:"profile"`
			Enqueued int64  `json:"enqueued"`
		} `json:"nodes"`
		MaxClaims     int      `json:"max_claims"`
		MaxDispatches int      `json:"max_dispatches"`
		ProfileMax    int      `json:"profile_max"`
		ShareKeys     bool     `json:"share_keys"`
		Expected      []string `json:"expected"`
		Reason        string   `json:"reason"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			commits := testGenesis(t, func(policy *Policy) {
				policy.Mode, policy.ClassWeights = fixture.Mode, fixture.ClassWeights
				policy.AccountingWeights = fixture.AccountWeights
				rule := policy.Producers[testPrincipal]
				rule.FairnessKeys = []string{}
				for key := range fixture.AccountWeights {
					rule.FairnessKeys = append(rule.FairnessKeys, key)
				}
				policy.Producers[testPrincipal] = rule
				pool := policy.Pools["default"]
				profile := pool.Profiles["default"]
				profile.MaxClaims, profile.ShareKeys = fixture.ProfileMax, fixture.ShareKeys
				pool.Profiles["default"], pool.Profiles["x"], pool.Profiles["y"] = profile, profile, profile
				policy.Pools["default"] = pool
			})
			nodes := []WorkDefinition{}
			for _, input := range fixture.Nodes {
				node := testNode(t, commits, input.Key)
				node.Priority, node.FairnessKey, node.Enqueued = input.Priority, input.Account, input.Enqueued
				node.WorkerProfile = input.Profile
				nodes = append(nodes, node)
			}
			commits = testSubmit(t, commits, "submit", nodes...)
			state, _ := Replay(commits)
			before, _ := canonicalValue(state)
			decision, err := PlanDispatch(state, DispatchParameters{
				Pool: "default", MaxClaims: fixture.MaxClaims, MaxDispatches: fixture.MaxDispatches, MaxBytes: 48 << 10,
			}, "fixture-grant", "fixture-commit", 3000)
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, operation := range decision.Operations {
				var claim ClaimOperation
				_ = json.Unmarshal(operation, &claim)
				got = append(got, state.Works[claim.WorkID].NodeKey)
			}
			if !slices.Equal(got, fixture.Expected) || decision.Reason != fixture.Reason {
				t.Fatalf("independent answers: got %v/%s expected %v/%s", got, decision.Reason, fixture.Expected, fixture.Reason)
			}
			after, _ := canonicalValue(state)
			if string(before) != string(after) {
				t.Fatal("prediction mutated authoritative state")
			}
		})
	}
}

func TestReplayRejectsUnscheduledClaimAndRegrouping(t *testing.T) {
	commits := testGenesis(t, nil)
	a, b := testNode(t, commits, "a"), testNode(t, commits, "b")
	commits = testSubmit(t, commits, "submit", a, b)
	next, _ := testGrant(t, commits, "grant", 2, 2)
	var claim ClaimOperation
	_ = json.Unmarshal(next[len(next)-1].Operations[0], &claim)
	claim.WorkID = b.WorkID
	next[len(next)-1].Operations[0] = Op(claim)
	if _, err := Replay(next); err == nil || !strings.Contains(err.Error(), "selection_invalid") {
		t.Fatalf("arbitrary Work Claim accepted: %v", err)
	}
}

func TestClassSelectionUpdatesOnlyItsSelectedChildClock(t *testing.T) {
	commits := testGenesis(t, nil)
	a, b := testNode(t, commits, "a"), testNode(t, commits, "b")
	a.Priority, b.Priority = 1, 2
	commits = testSubmit(t, commits, "submit", a, b)
	state, _ := Replay(commits)
	clocks := PoolClocks{Classes: newClock(), Keys: map[int]Clock{1: newClock()}}
	clocks.Keys[2] = Clock{V: "42", Pass: map[string]string{"": "8"}, Active: map[string]bool{}}
	state.Clocks["default"] = clocks
	before, _ := canonicalValue(clocks.Keys[2])
	selected, proposed, err := planNext(state, "default", 3000)
	if err != nil || selected.WorkID != a.WorkID {
		t.Fatalf("unexpected selected class: %+v %v", selected, err)
	}
	after, _ := canonicalValue(proposed.Keys[2])
	if string(before) != string(after) {
		t.Fatal("normalization/activation changed an unselected class's child clock")
	}
	if sameJSON(proposed.Keys[1], clocks.Keys[1]) {
		t.Fatal("selected child path did not advance")
	}
}

func TestReplayRejectsValidMembershipOutsideExactFairPrefix(t *testing.T) {
	commits := testGenesis(t, nil)
	a, b := testNode(t, commits, "a"), testNode(t, commits, "b")
	commits = testSubmit(t, commits, "submit", a, b)
	next, _ := testGrant(t, commits, "grant", 2, 2)
	truncated := slices.Clone(next)
	truncated[len(truncated)-1].Operations = truncated[len(truncated)-1].Operations[:1]
	if _, err := Replay(truncated); err == nil || !strings.Contains(err.Error(), "selection_invalid") {
		t.Fatalf("shorter valid-membership prefix became accepted grant: %v", err)
	}
	regrouped := slices.Clone(next)
	regrouped[len(regrouped)-1].Operations = slices.Clone(next[len(next)-1].Operations)
	var first, second ClaimOperation
	_ = json.Unmarshal(regrouped[len(regrouped)-1].Operations[0], &first)
	_ = json.Unmarshal(regrouped[len(regrouped)-1].Operations[1], &second)
	second.DispatchID, second.Handle = first.DispatchID, "h2"
	regrouped[len(regrouped)-1].Operations[1] = Op(second)
	if _, err := Replay(regrouped); err == nil || !strings.Contains(err.Error(), "selection_invalid") {
		t.Fatalf("valid selected membership was regrouped outside approved packing: %v", err)
	}
}

func TestAssignmentByteFitOpensAnotherCompatibleGroup(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) {
		pool := policy.Pools["default"]
		profile := pool.Profiles["default"]
		profile.MaxClaims = 16
		pool.Profiles["default"] = profile
		policy.Pools["default"] = pool
	})
	nodes := []WorkDefinition{}
	for _, key := range []string{"a", "b", "c"} {
		node := testNode(t, commits, key)
		node.Payload = Op(map[string]string{"task": strings.Repeat("x", 2000)})
		nodes = append(nodes, node)
	}
	commits = testSubmit(t, commits, "submit", nodes...)
	state, _ := Replay(commits)
	decision, err := PlanDispatch(state, DispatchParameters{
		Pool: "default", MaxClaims: 3, MaxDispatches: 3, MaxBytes: 3000,
	}, "byte-fit", "byte-fit-commit", 3000)
	if err != nil || len(decision.Assignments) != 3 || len(decision.Operations) != 3 {
		t.Fatalf("oversized compatible group prevented a free native group: %+v %v", decision, err)
	}
	for _, assignment := range decision.Assignments {
		data, _ := canonicalValue(assignment)
		if len(assignment.Claims) != 1 || len(data) > 3000 {
			t.Fatal("new group did not obey its actual canonical byte budget")
		}
	}
}
