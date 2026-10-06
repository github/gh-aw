package workqueue

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedIdentityByteValidationFixtures(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/identity-validation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version  int `json:"version"`
		Identity []struct {
			Name              string `json:"name"`
			Text              string `json:"text"`
			Repeat            int    `json:"repeat"`
			ExpectedUTF8Bytes int    `json:"expected_utf8_bytes"`
			Valid             bool   `json:"valid"`
		} `json:"identity"`
		Decimal []struct {
			Name   string `json:"name"`
			Digits int    `json:"digits"`
			Valid  bool   `json:"valid"`
		} `json:"decimal"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != Version || len(fixture.Identity) == 0 || len(fixture.Decimal) == 0 {
		t.Fatal("missing shared identity contract")
	}
	for _, test := range fixture.Identity {
		t.Run("identity/"+test.Name, func(t *testing.T) {
			value := strings.Repeat(test.Text, test.Repeat)
			if len(value) != test.ExpectedUTF8Bytes {
				t.Fatal("independent UTF-8 byte expectation is inconsistent")
			}
			commits := testGenesis(t, nil)
			work := testNode(t, commits, "identity")
			work.GraphID, work.WorkID = value, NodeID(value, work.NodeKey)
			request, err := NewRequest("identity-fixture", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{work}})
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, err = BuildCandidate(commits, testActor("producer"), request, 2000)
			if (err == nil) != test.Valid {
				t.Fatalf("declared Work identity byte validity disagrees: valid=%t error=%v", test.Valid, err)
			}
		})
	}
	for _, test := range fixture.Decimal {
		t.Run("decimal/"+test.Name, func(t *testing.T) {
			commits := testGenesis(t, nil)
			commits[0].Actor.RunID, commits[0].Actor.RunAttempt = strings.Repeat("1", test.Digits), 1
			commits[0].Request, err = NewRequest(commits[0].Request.ID, "policy", commits[0].Actor,
				OperationsParameters{Operations: commits[0].Operations})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateCommit(commits[0]); (err == nil) != test.Valid {
				t.Fatalf("canonical decimal length disagrees: valid=%t error=%v", test.Valid, err)
			}
		})
	}
}

func TestIdentityByteBoundsDoNotInspectOpaqueApplicationData(t *testing.T) {
	commits := testGenesis(t, nil)
	work := testNode(t, commits, "opaque")
	work.Payload = Op(map[string]any{
		"graph_id": strings.Repeat("😀", 100),
		"nested":   map[string]string{"principal": strings.Repeat("é", 200)},
	})
	commits = testSubmit(t, commits, "opaque-submit", work)
	granted, decision := testGrant(t, commits, "opaque-grant", 1, 1)
	if _, err := Replay(granted); err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalValue(decision.Assignments[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAssignment(raw); err != nil {
		t.Fatalf("opaque immutable Work text was incorrectly bounded as protocol identities: %v", err)
	}
}

func TestStandalonePolicyAndAssignmentIdentityByteBounds(t *testing.T) {
	for _, name := range []string{"profile", "principal", "pool"} {
		t.Run(name, func(t *testing.T) {
			policy := DefaultPolicy(testPrincipal, testRepository)
			value := strings.Repeat("😀", 65)
			switch name {
			case "profile":
				pool := policy.Pools["default"]
				pool.Profiles[value] = pool.Profiles["default"]
				policy.Pools["default"] = pool
			case "principal":
				policy.Producers[value] = policy.Producers[testPrincipal]
			case "pool":
				policy.Pools[value] = policy.Pools["default"]
			}
			if err := ValidatePolicy(policy); err == nil {
				t.Fatal("standalone policy accepted an oversized UTF-8 identity key")
			}
		})
	}
	t.Run("empty unused profile key", func(t *testing.T) {
		policy := DefaultPolicy(testPrincipal, testRepository)
		pool := policy.Pools["default"]
		pool.Profiles[""] = pool.Profiles["default"]
		policy.Pools["default"] = pool
		if err := ValidatePolicy(policy); err == nil {
			t.Fatal("standalone policy accepted an empty unused profile identity")
		}
	})
	commits := testGenesis(t, nil)
	commits = testSubmit(t, commits, "submit", testNode(t, commits, "assigned"))
	_, decision := testGrant(t, commits, "grant", 1, 1)
	assignment := decision.Assignments[0]
	assignment.DispatchID = strings.Repeat("😀", 65)
	raw, err := canonicalValue(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAssignment(raw); err == nil {
		t.Fatal("standalone assignment accepted an oversized UTF-8 dispatch identity")
	}
	if _, err := NormalizeFinishIntent(decision.Assignments[0],
		Op(map[string]string{"claim_handle": strings.Repeat("😀", 65), "outcome": "completed"})); err == nil {
		t.Fatal("finish normalizer accepted an oversized UTF-8 member identity")
	}
}
