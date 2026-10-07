package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type resourceScopeFixtureCase struct {
	Name    string          `json:"name"`
	Scope   json.RawMessage `json:"scope,omitempty"`
	Subject *Resource       `json:"subject"`
	Target  EffectResource  `json:"target"`
	Valid   bool            `json:"valid"`
}

func resourceScopeFixture(t *testing.T) []resourceScopeFixtureCase {
	t.Helper()
	data, err := os.ReadFile("../../specs/work-queue/fixtures/resource-scope.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int                        `json:"version"`
		Cases   []resourceScopeFixtureCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) != 33 {
		t.Fatal("all independent frozen resource scope cases must be consumed")
	}
	return fixture.Cases
}

func resourceScopeFixtureState(t *testing.T, test resourceScopeFixtureCase) ([]QueueCommit, Projection, Actor) {
	t.Helper()
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		payload := map[string]any{"name": work.NodeKey}
		if len(test.Scope) != 0 {
			payload["resource_scope"] = test.Scope
		}
		var err error
		work.Payload, err = canonicalValue(payload)
		if err != nil {
			t.Fatal(err)
		}
		work.Subject = test.Subject
	})
	commits = finishMember(t, commits, assignment, 0, "completed")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return commits, state, workerActor(assignment, "h1")
}

func TestIndependentFrozenWorkResourceScopes(t *testing.T) {
	for _, test := range resourceScopeFixture(t) {
		t.Run(test.Name, func(t *testing.T) {
			_, state, actor := resourceScopeFixtureState(t, test)
			before, err := canonicalValue(state)
			if err != nil {
				t.Fatal(err)
			}
			err = AuthorizeEffect(state, actor, test.Target)
			if (err == nil) != test.Valid {
				t.Fatalf("independent scope expectation valid=%t: %v", test.Valid, err)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "claim_scope_invalid:") {
				t.Fatalf("wrong scope rejection: %v", err)
			}
			after, err := canonicalValue(state)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("effect authorization mutated ownership, payload, counters or policy")
			}
		})
	}
}

func TestFrozenWorkResourceScopesCannotBorrowOrBeReplaced(t *testing.T) {
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		number := "7"
		if work.NodeKey == "b" {
			number = "8"
		}
		var err error
		work.Payload, err = canonicalValue(map[string]any{
			"resource_scope": map[string]any{
				"version": 1, "resources": []EffectResource{{"repository": testRepository, "number": number}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	commits = finishMember(t, commits, assignment, 0, "completed")
	commits = finishMember(t, commits, assignment, 1, "completed")
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	target := EffectResource{"repository": testRepository, "number": "7"}
	if err := AuthorizeEffect(state, workerActor(assignment, "h1"), target); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeEffect(state, workerActor(assignment, "h2"), target); err == nil {
		t.Fatal("completed sibling borrowed another Work's frozen resource selector")
	}
	state.Works[assignment.Claims[1].WorkID].Payload = state.Works[assignment.Claims[0].WorkID].Payload
	if err := AuthorizeEffect(state, workerActor(assignment, "h2"), target); err == nil {
		t.Fatal("projection payload replacement bypassed original immutable assignment")
	}
}

func TestFrozenWorkResourceScopeBoundsAndProfileIntersection(t *testing.T) {
	for _, count := range []int{128, 129} {
		selectors := []EffectResource{}
		for index := range count {
			selectors = append(selectors, EffectResource{
				"repository": testRepository, "ref": strings.Repeat("x", index+1),
			})
		}
		scope, err := canonicalValue(map[string]any{"version": 1, "resources": selectors})
		if err != nil {
			t.Fatal(err)
		}
		test := resourceScopeFixtureCase{Scope: scope}
		_, state, actor := resourceScopeFixtureState(t, test)
		err = AuthorizeEffect(state, actor, selectors[0])
		if (err == nil) != (count == 128) {
			t.Fatalf("scope selector bound %d: %v", count, err)
		}
	}
	for _, ref := range []string{strings.Repeat("\\", 256), strings.Repeat("\u00e9", 128), strings.Repeat("\u00e9", 129)} {
		_, state, actor := resourceScopeFixtureState(t, resourceScopeFixtureCase{})
		err := AuthorizeEffect(state, actor, EffectResource{"repository": testRepository, "ref": ref})
		if (err == nil) != (len(ref) <= 256) {
			t.Fatalf("scope UTF-8 byte bound %d: %v", len(ref), err)
		}
	}
	for _, changeFrozen := range []bool{false, true} {
		_, state, actor := resourceScopeFixtureState(t, resourceScopeFixtureCase{})
		target := EffectResource{"repository": "other/repo"}
		profile := state.Policy.Pools["default"].Profiles["default"]
		profile.EffectScope = "other/repo"
		state.Policy.Pools["default"].Profiles["default"] = profile
		if changeFrozen {
			state.Dispatches[actor.DispatchID].Profile.EffectScope = "other/repo"
			target["repository"] = testRepository
		}
		if err := AuthorizeEffect(state, actor, target); err == nil {
			t.Fatal("installed or frozen profile widening became a permission union")
		}
	}
}

func TestNativeFrozenWorkResourceScopeParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("cross-engine test tooling requires Node; native production has no Node dependency")
	}
	inputs := []map[string]any{}
	tests := resourceScopeFixture(t)
	for _, test := range tests {
		commits, state, actor := resourceScopeFixtureState(t, test)
		if err := AuthorizeEffect(state, actor, test.Target); (err == nil) != test.Valid {
			t.Fatalf("native expectation for %s differs: %v", test.Name, err)
		}
		inputs = append(inputs, map[string]any{
			"commits": commits, "actor": actor, "target": test.Target, "name": test.Name,
		})
	}
	input, err := canonicalValue(inputs)
	if err != nil {
		t.Fatal(err)
	}
	const script = `
const fs = require("node:fs");
const { replayTransactions, validateClaimAuthority } = require("./actions/setup/js/work_queue_replay.cjs");
const inputs = JSON.parse(fs.readFileSync(0, "utf8"));
const outcomes = inputs.map(input => {
  const state = replayTransactions(input.commits);
  const dispatch = state.dispatches.get(input.actor.dispatch_id);
  const member = dispatch.claims.find(member => member.handle === input.actor.claim_handle);
  const context = { ...input.actor, authenticated: true, roles: ["worker"], ref: dispatch.run.ref, event: dispatch.run.event };
  try {
    validateClaimAuthority(state, member.claim_id, context, { requireCompletion: true, resource: input.target });
    return "allowed";
  } catch (error) {
    if (typeof error.code !== "string") throw error;
    return error.code;
  }
});
console.log(JSON.stringify(outcomes));
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Dir = "../.."
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("actual JS frozen resource authority: %v\n%s", err, stderr.String())
	}
	var outcomes []string
	if err := json.Unmarshal(output, &outcomes); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != len(tests) {
		t.Fatal("JS omitted independent resource scope cases")
	}
	for index, test := range tests {
		want := "claim_scope_invalid"
		if test.Valid {
			want = "allowed"
		}
		if outcomes[index] != want {
			t.Errorf("%s: actual JS outcome=%s, independent expected=%s", test.Name, outcomes[index], want)
		}
	}

}

func TestLifecycleFrozenWorkResourceScopeParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("cross-engine test tooling requires Node; native production has no Node dependency")
	}
	inputs := []resourceScopeFixtureCase{}
	for _, test := range resourceScopeFixture(t) {
		if test.Name == "original-native-run" || test.Name == "scope-cannot-borrow-sibling-run" {
			continue
		}
		inputs = append(inputs, test)
	}
	inputs = append(inputs,
		resourceScopeFixtureCase{
			Name:   "lifecycle-original-native-run",
			Scope:  json.RawMessage(`{"version":1,"resources":[{"repository":"owner/repo","run_id":"42"}]}`),
			Target: EffectResource{"repository": testRepository, "run_id": "42"}, Valid: true,
		},
		resourceScopeFixtureCase{
			Name:   "lifecycle-foreign-native-run",
			Scope:  json.RawMessage(`{"version":1,"resources":[{"repository":"owner/repo","run_id":"43"}]}`),
			Target: EffectResource{"repository": testRepository, "run_id": "43"},
		},
	)
	input, err := canonicalValue(inputs)
	if err != nil {
		t.Fatal(err)
	}
	const script = `
	const fs = require("node:fs");
	const { queueFixture, REF, REPOSITORY, WORKFLOW } = require("./actions/setup/js/work_queue_lifecycle.test_helpers.cjs");
	const { authorizeWorkerClaim } = require("./actions/setup/js/finish_work_queue_claim.cjs");
	const tests = JSON.parse(fs.readFileSync(0, "utf8"));
	(async () => {
	  const outcomes = [];
	  for (const test of tests) {
	    const payload = { plan: test.name };
	    if (Object.hasOwn(test, "scope")) payload.resource_scope = test.scope;
	    const fixture = queueFixture({ count: 1, bound: true,
	      workDefaults: { payload, ...(test.subject ? { subject: test.subject } : {}) } });
	    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" },
	      { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
	    const before = fixture.transactions.length;
	    try {
	      const proof = await authorizeWorkerClaim({
	        githubClient: fixture.githubClient, context: fixture.workerContext,
	        workflowRef: REPOSITORY + "/" + WORKFLOW + "@" + REF,
	        assignment: fixture.assignment, claim_handle: "h1", resource: test.target,
	        readWorkQueueLog: fixture.readWorkQueueLog, publishWorkQueueRequest: fixture.publishWorkQueueRequest,
	      });
	      outcomes.push(proof.authorized === true);
	    } catch (error) {
	      if (!/scope/.test(error.message)) throw error;
	      outcomes.push(false);
	    }
	    if (fixture.transactions.length !== before) throw new Error("read-only authorization published a transaction");
	  }
	  console.log(JSON.stringify(outcomes));
	})().catch(error => { console.error(error.stack); process.exitCode = 1; });
	`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Dir = "../.."
	command.Stdin = bytes.NewReader(input)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("actual lifecycle resource authority: %v\n%s", err, diagnostics.String())
	}
	var outcomes []bool
	if err := json.Unmarshal(output, &outcomes); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != len(inputs) {
		t.Fatal("actual lifecycle omitted independent resource cases")
	}
	for index, test := range inputs {
		if outcomes[index] != test.Valid {
			t.Errorf("%s: actual lifecycle authorized=%t, independent expected=%t", test.Name, outcomes[index], test.Valid)
		}
	}
}
