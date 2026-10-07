package workqueue

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestProductionInitializationRequiresVerifiedWorkerRoute(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*queueAPI)
	}{
		{"missing immutable file", func(mock *queueAPI) { mock.workerStatus = http.StatusNotFound }},
		{"foreign file path", func(mock *queueAPI) { mock.workerPath = ".github/workflows/foreign.lock.yml" }},
		{"not a worker", func(mock *queueAPI) { mock.workerContent = "on: push\n" }},
		{"legacy single Claim", func(mock *queueAPI) {
			mock.workerContent = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_claim:\n        type: string\n"
		}},
		{"wrong assignment type", func(mock *queueAPI) {
			mock.workerContent = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: boolean\n"
		}},
		{"invalid YAML", func(mock *queueAPI) { mock.workerContent = "on: [\n" }},
		{"disabled workflow", func(mock *queueAPI) { mock.workerState = "disabled_manually" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			test.prepare(mock)
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}
			work, err := NewWork([]byte(`{"task":"a"}`), "graph", "a", "default", DefaultPolicy(actor.Principal, actor.Repository), 1000)
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewRequest("submit", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{work}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.HasPrefix(err.Error(), "policy_missing:") {
				t.Fatalf("unprovisioned worker became an authoritative route: %v", err)
			}
			if mock.refWrites != 0 || len(mock.logs) != 0 || mock.head != "" || mock.workerRef != strings.Repeat("f", 40) {
				t.Fatal("failed provisioning wrote a queue or substituted the producer checkout revision")
			}
		})
	}
}

func TestAuthenticatedInitialPolicyUsesExactProposalAndGenuineGenesisIdentity(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "acknowledgment lost"}[ambiguous], func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.ambiguous = ambiguous
			actor, err := branch.Authenticate(context.Background(), "administrator")
			if err != nil {
				t.Fatal(err)
			}

			policy := DefaultPolicy(actor.Principal, actor.Repository)
			pool := policy.Pools["default"]
			profile := pool.Profiles["default"]
			profile.Workflow, profile.Ref = ".github/workflows/custom.lock.yml", strings.Repeat("a", 64)
			pool.Profiles["default"] = profile
			policy.Pools["default"] = pool
			request, err := NewRequest("submit", "policy", actor, OperationsParameters{Operations: []Operation{
				mustOp(t, map[string]any{"kind": "Policy", "epoch": "installed", "policy": policy}),
			}})
			if err != nil {
				t.Fatal(err)
			}
			published, err := branch.Publish(context.Background(), actor, request)
			if err != nil {
				t.Fatal(err)
			}
			commits, err := branch.Read(context.Background())
			if err != nil || len(commits) != 1 || commits[0].Previous != nil ||
				commits[0].Request.ID != "submit" || commits[0].PolicyEpoch != "installed" ||
				commits[0].ID != "q_75490bd7b93e6fa7d18cfdea90cc6bcb983d5f3ea326249d2709ca6c94bc07ba" {
				t.Fatalf("explicit proposal installed a synthetic default or non-genesis candidate: %+v %v", commits, err)
			}
			state, err := Replay(commits)
			if err != nil || !sameJSON(*state.Policy, policy) || mock.workerRef != strings.Repeat("a", 64) || mock.refWrites != 1 {
				t.Fatal("trusted custom route/proposal was changed or charged twice")
			}
			if published.Commit == nil || published.Commit.ID != commits[0].ID || !sameJSON(published.Commit.Request, request) {
				t.Fatal("publication did not return the checked original request")
			}
			reads := mock.workerReads
			again, err := branch.Publish(context.Background(), actor, request)
			if err != nil || again.Changed || mock.refWrites != 1 || mock.workerReads != reads {
				t.Fatalf("installed Policy was automatically reprovisioned/rewritten: %v", err)
			}
		})
	}
}

func TestInitialPolicyRejectsConstructorPlaceholderBeforeNativeWrites(t *testing.T) {
	branch, mock := newQueueAPI(t)
	actor, err := branch.Authenticate(context.Background(), "administrator")
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest("install-placeholder", "policy", actor, OperationsParameters{Operations: []Operation{
		mustOp(t, map[string]any{"kind": "Policy", "epoch": "installed", "policy": DefaultPolicy(actor.Principal, actor.Repository)}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
		!strings.HasPrefix(err.Error(), "policy_missing:") {
		t.Fatalf("pure constructor placeholder became a production route: %v", err)
	}
	if mock.workerReads != 0 || len(mock.logs) != 0 || mock.refWrites != 0 {
		t.Fatal("placeholder initialization read an unrelated route or wrote authoritative state")
	}
}

func TestProspectivePolicyVerifiesRoutesWithoutRewritingInstalledPolicy(t *testing.T) {
	branch, mock := newQueueAPI(t)
	commits := testGenesis(t, nil)
	installMockLog(t, mock, commits)
	actor, err := branch.Authenticate(context.Background(), "administrator")
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy(actor.Principal, actor.Repository)
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Ref = strings.Repeat("e", 40)
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	request, err := NewRequest("prospective", "policy", actor, OperationsParameters{Operations: []Operation{
		mustOp(t, map[string]any{"kind": "Policy", "epoch": "prospective", "policy": policy}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	originalHead := mock.head
	mock.workerStatus = http.StatusNotFound
	if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
		!strings.HasPrefix(err.Error(), "policy_missing:") {
		t.Fatalf("prospective Policy adopted an unverified route: %v", err)
	}
	if mock.head != originalHead || mock.refWrites != 0 {
		t.Fatal("failed route provisioning rewrote installed Policy")
	}
	mock.workerStatus = 0
	published, err := branch.Publish(context.Background(), actor, request)
	if err != nil || published.Commit == nil || !published.Changed || mock.workerRef != profile.Ref {
		t.Fatalf("verified prospective Policy did not install: %v", err)
	}
	next, err := branch.Read(context.Background())
	if err != nil || len(next) != 2 || !sameJSON(next[0], commits[0]) || next[1].PolicyEpoch != "prospective" {
		t.Fatal("prospective installation changed prior authority/history")
	}
	mock.workerStatus = http.StatusNotFound
	writes, reads := mock.refWrites, mock.workerReads
	if again, err := branch.Publish(context.Background(), actor, request); err != nil || again.Changed ||
		mock.refWrites != writes || mock.workerReads != reads {
		t.Fatal("stable request recovery reprovisioned or rewrote installed authority")
	}
}
