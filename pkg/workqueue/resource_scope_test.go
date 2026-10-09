package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

type resourceScopeFixtureCase struct {
	Name      string                     `json:"name"`
	Scope     json.RawMessage            `json:"scope,omitempty"`
	Subject   *Resource                  `json:"subject"`
	Target    EffectResource             `json:"target"`
	Valid     bool                       `json:"valid"`
	Ancestors []resourceScopeFixtureCase `json:"ancestors,omitempty"`
}

func resourceScopeFixture(t *testing.T) []resourceScopeFixtureCase {
	t.Helper()
	data, err := os.ReadFile("../../specs/work-queue/fixtures/resource-scope.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version       int                        `json:"version"`
		Cases         []resourceScopeFixtureCase `json:"cases"`
		AncestorCases []resourceScopeFixtureCase `json:"ancestor_cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) != 47 || len(fixture.AncestorCases) != 10 {
		t.Fatal("all independent frozen resource scope cases must be consumed")
	}
	return append(fixture.Cases, fixture.AncestorCases...)
}

func applyFixtureResourceBinding(t *testing.T, work *WorkDefinition, test resourceScopeFixtureCase) {
	t.Helper()
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
}

func resourceScopeFixtureState(t *testing.T, test resourceScopeFixtureCase) ([]QueueCommit, Projection, Actor) {
	t.Helper()
	root := test
	descendants := []resourceScopeFixtureCase{}
	if len(test.Ancestors) != 0 {
		root = test.Ancestors[0]
		descendants = append(descendants, test.Ancestors[1:]...)
		descendants = append(descendants, test)
	}
	commits, assignment := boundAssignmentWithWork(t, func(work *WorkDefinition) {
		applyFixtureResourceBinding(t, work, root)
	})
	commits = finishMember(t, commits, assignment, 0, "completed")
	actor := workerActor(assignment, "h1")
	for index, binding := range descendants {
		child := testNode(t, commits, fmt.Sprintf("resource-child-%d", index))
		applyFixtureResourceBinding(t, &child, binding)
		request, err := NewRequest(fmt.Sprintf("resource-child-submit-%d", index), "submit", actor,
			SubmitParameters{Nodes: []WorkDefinition{child}})
		if err != nil {
			t.Fatal(err)
		}
		commits, _, _, err = BuildCandidate(commits, actor, request, 4000)
		if err != nil {
			t.Fatal(err)
		}
		var decision Decision
		commits, decision = testGrant(t, commits, fmt.Sprintf("resource-child-grant-%d", index), 1, 1)
		if len(decision.Assignments) != 1 || len(decision.Assignments[0].Claims) != 1 ||
			decision.Assignments[0].Claims[0].WorkID != child.WorkID {
			t.Fatalf("ancestor fixture did not dispatch its admitted child: %+v", decision)
		}
		assignment = decision.Assignments[0]
		sender := testActor("dispatcher")
		sender.Workflow, sender.RunID, sender.RunAttempt = ".github/workflows/dispatcher.lock.yml", "101", 1
		commits = testOperations(t, commits, sender, fmt.Sprintf("resource-child-start-%d", index), "dispatch", mustOp(t, map[string]any{
			"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": sender,
		}))
		actor = workerActor(assignment, assignment.Claims[0].Handle)
		actor.RunID = strconv.Itoa(203 + index)
		claimsState, err := Replay(commits)
		if err != nil {
			t.Fatal(err)
		}
		profile := claimsState.Policy.Pools[assignment.Pool].Profiles[assignment.WorkerProfile]
		commits = testOperations(t, commits, testActor("reconciler"), fmt.Sprintf("resource-child-bind-%d", index), "dispatch", mustOp(t, map[string]any{
			"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound",
			"run": RunBinding{RunID: actor.RunID, RunAttempt: 1, Repository: testRepository,
				Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal, Event: "workflow_dispatch"},
			"evidence": Evidence{Kind: "reconciliation", Source: "github_api", Repository: testRepository,
				Workflow: profile.Workflow, Ref: profile.Ref, Principal: profile.Principal,
				CheckedAt: 4000, RunID: actor.RunID, RunAttempt: 1},
		}))
		request, err = NewRequest(fmt.Sprintf("resource-child-finish-%d", index), "finish", actor,
			FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: actor.ClaimHandle, Outcome: "completed"})
		if err != nil {
			t.Fatal(err)
		}
		commits, _, _, err = BuildCandidate(commits, actor, request, 4000)
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	return commits, state, actor
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
				"version": 1, "resources": []EffectResource{{"kind": "issue", "host": "github.com",
					"repository": testRepository, "repository_id": "1", "resource_id": number, "number": number}},
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
	target := EffectResource{"kind": "issue", "host": "github.com", "repository": testRepository,
		"repository_id": "1", "resource_id": "7", "number": "7"}
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

func TestFullSubjectRequiresExplicitGenericSelectors(t *testing.T) {
	subject := &Resource{Kind: "issue", Host: "github.com", Repository: testRepository,
		RepositoryID: "1", ResourceID: "2", Number: "7"}
	for field, value := range map[string]string{"run_id": "202", "ref": "refs/heads/topic", "path": "src/a.go"} {
		for _, scoped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/scoped=%t", field, scoped), func(t *testing.T) {
				test := resourceScopeFixtureCase{Subject: subject}
				if scoped {
					var err error
					test.Scope, err = canonicalValue(map[string]any{"version": 1,
						"resources": []EffectResource{{"repository": testRepository, field: value}}})
					if err != nil {
						t.Fatal(err)
					}
				}
				_, state, actor := resourceScopeFixtureState(t, test)
				target := EffectResource{"kind": "issue", "host": "github.com", "repository": testRepository,
					"repository_id": "1", "resource_id": "2", "number": "7", field: value}
				err := AuthorizeEffect(state, actor, target)
				if (err == nil) != scoped {
					t.Fatalf("Subject cannot substitute for an exact %s selector: %v", field, err)
				}
				if err != nil && !strings.HasPrefix(err.Error(), "claim_scope_invalid:") {
					t.Fatalf("wrong generic effect rejection: %v", err)
				}
			})
		}
	}
}

func TestFrozenWorkResourceScopeBoundsAndProfileIntersection(t *testing.T) {
	for _, count := range []int{128, 129} {
		selectors := []EffectResource{}
		for index := range count {
			selectors = append(selectors, EffectResource{
				"host": "github.com", "repository_id": "1",
				"repository": testRepository, "ref": fmt.Sprintf("topic-%d", index),
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
		scope, err := canonicalValue(map[string]any{"version": 1, "resources": []EffectResource{{
			"host": "github.com", "repository": testRepository, "repository_id": "1", "ref": ref,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		_, state, actor := resourceScopeFixtureState(t, resourceScopeFixtureCase{Scope: scope})
		err = AuthorizeEffect(state, actor, EffectResource{"host": "github.com", "repository": testRepository,
			"repository_id": "1", "ref": ref})
		if (err == nil) != (len(ref) <= 256) {
			t.Fatalf("scope UTF-8 byte bound %d: %v", len(ref), err)
		}
	}
	for _, changeFrozen := range []bool{false, true} {
		_, state, actor := resourceScopeFixtureState(t, resourceScopeFixtureCase{
			Scope: json.RawMessage(`{"version":1,"resources":[{"host":"github.com","repository":"owner/repo","repository_id":"1"},{"host":"github.com","repository":"other/repo","repository_id":"2"}]}`),
		})
		target := EffectResource{"host": "github.com", "repository": "other/repo", "repository_id": "2"}
		profile := state.Policy.Pools["default"].Profiles["default"]
		profile.EffectScope = "other/repo"
		state.Policy.Pools["default"].Profiles["default"] = profile
		if changeFrozen {
			state.Dispatches[actor.DispatchID].Profile.EffectScope = "other/repo"
			target["repository"] = testRepository
			target["repository_id"] = "1"
		}
		if err := AuthorizeEffect(state, actor, target); err == nil {
			t.Fatal("installed or frozen profile widening became a permission union")
		}
	}
}

func TestNativeFrozenWorkResourceScopeParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("strict resource parity requires the actual Node runtime")
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

func TestFrozenWorkAncestorOriginsCannotBeReplacedOrLost(t *testing.T) {
	var test resourceScopeFixtureCase
	for _, candidate := range resourceScopeFixture(t) {
		if candidate.Name == "child-cannot-widen-parent-native-target" {
			test = candidate
			break
		}
	}
	if len(test.Ancestors) == 0 {
		t.Fatal("literal ancestor denial is missing")
	}
	commits, state, actor := resourceScopeFixtureState(t, test)
	dispatch := state.Dispatches[actor.DispatchID]
	child := state.Works[dispatch.Claims[0].WorkID]
	commits = testSubmit(t, commits, "resource-duplicate-child", child.WorkDefinition)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	creators, err := immutableWorkCreators(state)
	if err != nil || creators[child.WorkID].Role != "worker" {
		t.Fatalf("later producer retry replaced original worker authority: %v", err)
	}
	if err := AuthorizeEffect(state, actor, test.Target); err == nil {
		t.Fatal("duplicate producer submission erased ancestor restriction")
	}
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("c", 40), testActor("administrator"), commits[len(commits)-1].At)
	if err != nil {
		t.Fatal(err)
	}
	compacted, err := Replay(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	creators, err = immutableWorkCreators(compacted)
	if err != nil || creators[child.WorkID].Role != "worker" {
		t.Fatalf("checkpoint lost immutable worker creator: %v", err)
	}
	if err := AuthorizeEffect(compacted, actor, test.Target); err == nil {
		t.Fatal("checkpoint erased ancestor resource restrictions")
	}
	for _, candidate := range resourceScopeFixture(t) {
		if candidate.Name == "child-preserves-parent-native-target" {
			test = candidate
			break
		}
	}
	commits, state, actor = resourceScopeFixtureState(t, test)
	if err := AuthorizeEffect(state, actor, test.Target); err != nil {
		t.Fatalf("ancestor proof mutations require an initially authorized target: %v", err)
	}
	child = state.Works[state.Dispatches[actor.DispatchID].Claims[0].WorkID]
	creators, err = immutableWorkCreators(state)
	if err != nil {
		t.Fatal(err)
	}
	parentDispatchID := creators[child.WorkID].DispatchID
	for _, mutation := range []struct {
		name   string
		change func(*Projection)
	}{
		{"missing-parent-dispatch", func(state *Projection) { delete(state.Dispatches, parentDispatchID) }},
		{"cyclic-parent-member", func(state *Projection) {
			for index := range state.Dispatches[parentDispatchID].Claims {
				state.Dispatches[parentDispatchID].Claims[index].WorkID = child.WorkID
			}
		}},
		{"foreign-frozen-parent-profile", func(state *Projection) {
			state.Dispatches[parentDispatchID].Profile.EffectScope = "other/repo"
		}},
		{"missing-causal-origin", func(state *Projection) {
			for key, commit := range state.Requests {
				if commit.ID == commits[0].ID {
					delete(state.Requests, key)
				}
			}
		}},
		{"cyclic-causal-origin", func(state *Projection) {
			for key, commit := range state.Requests {
				if commit.ID == state.Tip {
					tip := state.Tip
					commit.Previous = &tip
					state.Requests[key] = commit
				}
			}
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			fresh, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			mutation.change(&fresh)
			err = AuthorizeEffect(fresh, actor, test.Target)
			if err == nil || !strings.HasPrefix(err.Error(), "claim_scope_invalid:") {
				t.Fatalf("missing/cyclic ancestor proof accepted: %v", err)
			}
		})
	}
}

func TestLifecycleFrozenWorkResourceScopeParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("strict lifecycle resource parity requires the actual Node runtime")
	}
	inputs := []resourceScopeFixtureCase{}
	for _, test := range resourceScopeFixture(t) {
		if len(test.Ancestors) != 0 {
			continue
		}
		if test.Name == "original-native-run" || test.Name == "scope-cannot-borrow-sibling-run" {
			continue
		}
		inputs = append(inputs, test)
	}
	inputs = append(inputs,
		resourceScopeFixtureCase{
			Name:   "lifecycle-original-native-run",
			Scope:  json.RawMessage(`{"version":1,"resources":[{"host":"github.com","repository":"owner/repo","repository_id":"1","run_id":"42"}]}`),
			Target: EffectResource{"host": "github.com", "repository": testRepository, "repository_id": "1", "run_id": "42"}, Valid: true,
		},
		resourceScopeFixtureCase{
			Name:   "lifecycle-foreign-native-run",
			Scope:  json.RawMessage(`{"version":1,"resources":[{"host":"github.com","repository":"owner/repo","repository_id":"1","run_id":"43"}]}`),
			Target: EffectResource{"host": "github.com", "repository": testRepository, "repository_id": "1", "run_id": "43"},
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
