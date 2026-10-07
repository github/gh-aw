package workqueue

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNativeDecimalIdentityBoundsBeforeWireValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		digits int
		valid  bool
	}{
		{name: "positive", digits: 1, valid: true},
		{name: "lossless-large", digits: 129, valid: true},
		{name: "exact-ceiling", digits: 256, valid: true},
		{name: "above-ceiling", digits: 257},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := strings.Repeat("9", test.digits)
			if decimalIdentity(id) != test.valid {
				t.Errorf("native decimal guard validity=%t, want %t", decimalIdentity(id), test.valid)
			}
			actor := testActor("administrator")
			actor.Principal = id
			if err := validateActorOrigin(actor); (err == nil) != test.valid {
				t.Errorf("native actor origin validity disagrees: %v", err)
			} else if !test.valid && !strings.HasPrefix(err.Error(), "actor_unauthorized:") {
				t.Errorf("native actor origin used the wrong rejection code: %v", err)
			}
			for _, scope := range []string{"profile", "producer"} {
				policy := DefaultPolicy(testPrincipal, testRepository)
				if scope == "profile" {
					pool := policy.Pools["default"]
					profile := pool.Profiles["default"]
					profile.Principal = id
					pool.Profiles["default"] = profile
					policy.Pools["default"] = pool
				} else {
					policy.Producers[id] = policy.Producers[testPrincipal]
					delete(policy.Producers, testPrincipal)
				}
				if err := validatePolicy(policy); (err == nil) != test.valid {
					t.Errorf("native %s policy validity disagrees before schema validation: %v", scope, err)
				} else if !test.valid && !strings.HasPrefix(err.Error(), "policy_invalid:") {
					t.Errorf("native %s policy used the wrong rejection code: %v", scope, err)
				}
			}
			branch, mock := newQueueAPI(t)
			mock.userID = json.Number(id)
			authenticated, err := branch.Authenticate(context.Background(), "administrator")
			if (err == nil) != test.valid || test.valid && authenticated.Principal != id {
				t.Errorf("native authenticated principal bound/lossless value disagrees: %+v %v", authenticated, err)
			} else if !test.valid && !strings.HasPrefix(err.Error(), "actor_unauthorized:") {
				t.Errorf("native authenticated principal used the wrong rejection code: %v", err)
			}
			mock.nativeRun = &NativeRun{ID: json.Number(id), RunAttempt: 1, Event: "workflow_dispatch"}
			mock.nativeRun.Repository.FullName = testRepository
			run, err := branch.exactRun(context.Background(), id)
			if (err == nil) != test.valid || test.valid && run.ID.String() != id {
				t.Errorf("native exact run bound/lossless value disagrees: %+v %v", run, err)
			} else if !test.valid && (!strings.HasPrefix(err.Error(), "run_invalid:") ||
				!strings.Contains(err.Error(), "1..256") || !strings.Contains(err.Error(), "example: 202")) {
				t.Errorf("native exact run did not explain its bounded decimal format: %v", err)
			}
			expectedReads := 0
			if test.valid {
				expectedReads = 1
			}
			if mock.nativeReads != expectedReads {
				t.Errorf("native run requests=%d, want %d", mock.nativeReads, expectedReads)
			}
		})
	}
}

func TestNativeDecimalOriginRejectedBeforeNoOpAndAcknowledgment(t *testing.T) {
	for _, kind := range []string{"no-grant", "accepted-request"} {
		t.Run(kind, func(t *testing.T) {
			commits := testGenesis(t, nil)
			if kind == "accepted-request" {
				commits = testOperations(t, commits, testActor("administrator"), "accepted", "control", Op(map[string]any{
					"kind": "Control", "control": "admission_paused", "value": true, "reason": "incident",
				}))
			}
			before, err := Serialize(commits)
			if err != nil {
				t.Fatal(err)
			}
			actor := testActor("administrator")
			actor.Principal = strings.Repeat("9", 257)
			var request Request
			if kind == "no-grant" {
				request, err = NewRequest("no-grant", "dispatch_next", actor, DispatchParameters{
					Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
				})
			} else {
				request, err = NewRequest("accepted", "control", actor, OperationsParameters{
					Operations: commits[len(commits)-1].Operations,
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := BuildCandidate(commits, actor, request, 4000); err == nil ||
				!strings.HasPrefix(err.Error(), "request_invalid:") {
				t.Fatalf("invalid origin reached no-op/acknowledgment before closed validation: %v", err)
			}
			after, err := Serialize(commits)
			if err != nil || string(before) != string(after) {
				t.Fatal("invalid no-op/acknowledgment origin modified authoritative history")
			}
		})
	}
}

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
