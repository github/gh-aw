package workqueue

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestSharedRequestRoleContract(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version      int                 `json:"version"`
		RequestRoles map[string][]string `json:"request_roles"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != Version || len(fixture.RequestRoles) != 5 {
		t.Fatal("missing shared request role contract")
	}
	kinds := []string{"policy", "control", "submit", "dispatch_next", "observe", "finish", "dispatch",
		"release", "result", "delivery_failure", "cancel_claim", "cancel_work", "unknown"}
	for role, allowed := range fixture.RequestRoles {
		for _, kind := range kinds {
			t.Run(role+"/"+kind, func(t *testing.T) {
				err := validateRequestRole(testActor(role), kind)
				if (err == nil) != slices.Contains(allowed, kind) {
					t.Fatalf("trusted role request authority differs from shared contract: %v", err)
				}
			})
		}
	}
}

func TestNoGrantCannotBypassRequestOriginOrRoleValidation(t *testing.T) {
	commits := testGenesis(t, nil)
	for _, role := range []string{"producer", "reconciler", "worker"} {
		t.Run(role, func(t *testing.T) {
			actor := testActor(role)
			request, err := NewRequest("no-grant-"+role, "dispatch_next", actor,
				DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err == nil {
				t.Fatal("empty-queue evaluation bypassed actual request-origin authority")
			}
		})
	}
	for _, role := range []string{"administrator", "dispatcher"} {
		t.Run("authorized-"+role, func(t *testing.T) {
			actor := testActor(role)
			request, _ := NewRequest("no-grant-"+role, "dispatch_next", actor,
				DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
			next, commit, decision, err := BuildCandidate(commits, actor, request, 2000)
			if err != nil || commit != nil || len(next) != len(commits) || decision.Reason != "no_work" {
				t.Fatalf("authorized no-grant should remain pure: %+v %v", decision, err)
			}
		})
	}
	for _, principal := range []string{"operator", "0", "01001"} {
		t.Run("invalid-principal-"+principal, func(t *testing.T) {
			actor := testActor("administrator")
			actor.Principal = principal
			request, err := NewRequest("invalid-principal", "dispatch_next", actor,
				DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err == nil ||
				!strings.Contains(err.Error(), "actor_unauthorized") {
				t.Fatalf("no-grant accepted a noncanonical authenticated actor identity: %v", err)
			}
		})
	}
	actor := testActor("administrator")
	request, _ := NewRequest("bad-fingerprint", "dispatch_next", actor,
		DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
	request.Fingerprint = strings.Repeat("0", 64)
	if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err == nil ||
		!strings.Contains(err.Error(), "request_fingerprint") {
		t.Fatalf("no-grant accepted an invalid trusted-origin fingerprint: %v", err)
	}
	request, _ = NewRequest("unknown-parameter", "dispatch_next", actor,
		map[string]any{"pool": "default", "max_claims": 1, "max_dispatches": 1, "max_bytes": 48 << 10, "foreign": true})
	if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err == nil {
		t.Fatal("no-grant accepted an unclosed request parameter")
	}
	actor.Repository = "foreign/repo"
	request, _ = NewRequest("foreign-origin", "dispatch_next", actor,
		DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
	if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err == nil {
		t.Fatal("no-grant accepted a foreign repository's normalized origin")
	}
}

func TestExactExistingWorkSubmissionConsumesNoLedgerOrReserve(t *testing.T) {
	commits := testGenesis(t, func(policy *Policy) { policy.Limits.LedgerBytes = 6000 })
	work := testNode(t, commits, "existing")
	commits = testSubmit(t, commits, "first-submit", work)
	commits = testOperations(t, commits, testActor("administrator"), "pause-admission", "control",
		Op(map[string]any{"kind": "Control", "control": "admission_paused", "value": true, "reason": "pause"}))
	request, _ := NewRequest("same-node-new-logical-request", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{work}})
	next, commit, decision, err := BuildCandidate(commits, testActor("producer"), request, 4000)
	if err != nil || commit != nil || len(next) != len(commits) || decision.Reason != "already_submitted" ||
		len(decision.Operations) != 0 || len(decision.Assignments) != 0 {
		t.Fatalf("exact existing node consumed authoritative log space: %+v %v", decision, err)
	}
	before, _ := Replay(commits)
	after, _ := Replay(next)
	if after.LedgerBytes != before.LedgerBytes || remainingHeadroom(after) != remainingHeadroom(before) ||
		after.Requests[request.ID].ID != "" {
		t.Fatal("idempotent node preview consumed request identity or changed reserve")
	}
	foreign := testActor("producer")
	foreign.Principal = "2002"
	request, _ = NewRequest("unentitled-existing-node", "submit", foreign, SubmitParameters{Nodes: []WorkDefinition{work}})
	if _, _, _, err := BuildCandidate(commits, foreign, request, 4000); err == nil ||
		!strings.Contains(err.Error(), "admission_unauthorized") {
		t.Fatalf("fresh duplicate request bypassed current producer entitlement: %v", err)
	}
	work.Payload = Op(map[string]string{"task": "different immutable meaning"})
	request, _ = NewRequest("changed-existing-node", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{work}})
	if _, _, _, err := BuildCandidate(commits, testActor("producer"), request, 4000); err == nil ||
		!strings.Contains(err.Error(), "work_conflict") {
		t.Fatalf("idempotent node preview hid an immutable conflict: %v", err)
	}
}

func TestLogicalProducerRunRerunCannotMintWorkerAttemptAuthority(t *testing.T) {
	actor := testActor("producer")
	actor.Workflow, actor.RunID, actor.RunAttempt = ".github/workflows/producer.lock.yml", "2", 2
	commits := testGenesis(t, nil)
	request, _ := NewRequest("producer-rerun", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{testNode(t, commits, "rerun-origin")}})
	if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err != nil {
		t.Fatalf("logical producer provenance should preserve its actual source attempt: %v", err)
	}
	actor.Role, actor.DispatchID = "worker", "original-dispatch"
	request, _ = NewRequest("worker-rerun", "dispatch_next", actor,
		DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10})
	if _, _, _, err := BuildCandidate(commits, actor, request, 2000); err == nil {
		t.Fatal("logical rerun provenance minted native worker attempt authority")
	}
}
