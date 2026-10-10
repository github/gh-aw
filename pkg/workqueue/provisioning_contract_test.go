package workqueue

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestNativeSettingsDiscoversCompiledContractsWithoutAuthoredIdentities(t *testing.T) {
	branch, mock := newQueueAPI(t)
	mock.sources = map[string]string{"alpha": approvedWorkerSource, "beta": approvedWorkerSource, "legacy": approvedWorkerSource}
	mock.settings = `{"work_queue":{"pools":{"reports":{"concurrency":2}}}}`
	mock.workerRoutes = map[string]string{
		".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64)),
		".github/workflows/beta.lock.yml":  stampedWorker(strings.Repeat("b", 64)),
	}
	mock.workerStatuses = map[string]int{".github/workflows/legacy.lock.yml": http.StatusNotFound}
	mock.workerStates = map[string]string{".github/workflows/alpha.lock.yml": "disabled_manually"}
	policy, err := branch.PolicyFromConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range []string{"default", "reports"} {
		profiles := policy.Pools[pool].Profiles
		if profiles["alpha"].LogicalContract != strings.Repeat("a", 64) || profiles["beta"].LogicalContract != strings.Repeat("b", 64) ||
			profiles["legacy"].LogicalContract != "" || profiles["alpha"].Principal != "" || profiles["beta"].Principal != "" {
			t.Fatalf("compiler-derived contracts were not preserved in %s: %+v", pool, profiles)
		}
	}
	if len(mock.registrationReads) != 0 || mock.workerReads != 3 {
		t.Fatal("immutable contract discovery depended on unused worker availability")
	}
	actor, err := branch.Authenticate(context.Background(), "producer")
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewWork([]byte(`{"task":"selected beta"}`), "contract-discovery", "root", "default", policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	beta := policy.Pools["default"].Profiles["beta"]
	node.WorkerProfile, node.BatchTrustDomain, node.LogicalContract, node.ExecutionRef = "beta", beta.TrustDomain, beta.LogicalContract, beta.Ref
	request, err := NewRequest("contract-discovery", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
	if err != nil {
		t.Fatal(err)
	}
	branch.PolicyProposal = &policy
	if _, err := branch.Publish(context.Background(), actor, request); err != nil {
		t.Fatalf("a selected stamped worker depended on an unused inactive or missing target: %v", err)
	}
	commits, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(commits)
	if err != nil || state.Works[node.WorkID].AdmissionContract != beta.LogicalContract || state.Works[node.WorkID].ExecutionRef != beta.Ref {
		t.Fatalf("the selected worker inherited a different default contract or lost its pin: %v", err)
	}
	if mock.registrationReads[beta.Workflow] != 1 || mock.registrationReads[".github/workflows/alpha.lock.yml"] != 0 {
		t.Fatal("bootstrap checked unrelated native registrations")
	}
}

func TestNativeSettingsSelectedStampedContractMustMatchImmutableArtifact(t *testing.T) {
	for _, changed := range []string{"missing stamp", "different stamp", "conflicting stamp"} {
		t.Run(changed, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			contract := strings.Repeat("a", 64)
			mock.workerContent = stampedWorker(contract)
			policy, err := branch.PolicyFromConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if policy.Pools["default"].Profiles["worker"].LogicalContract != contract {
				t.Fatal("native discovery did not extract the compiler stamp")
			}
			switch changed {
			case "missing stamp":
				mock.workerContent = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n"
			case "different stamp":
				mock.workerContent = stampedWorker(strings.Repeat("b", 64))
			default:
				mock.workerContent += "env:\n  GH_AW_WORK_QUEUE_CONTRACT: \"" + strings.Repeat("b", 64) + "\"\n"
			}
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}
			node, err := NewWork([]byte(`{"task":"contract proof"}`), "contract-proof", "root", "default", policy, 1000)
			if err != nil {
				t.Fatal(err)
			}
			request, _ := NewRequest("contract-proof", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
			branch.PolicyProposal = &policy
			if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.HasPrefix(err.Error(), "policy_missing:") {
				t.Fatalf("a marked worker accepted an unstamped or different artifact: %v", err)
			}
			if mock.refWrites != 0 || len(mock.logs) != 0 {
				t.Fatal("failed contract verification partially bootstrapped the queue")
			}
		})
	}
}

func TestNativeDeploymentChecksPendingHistoricalPinsWithoutMovingCurrentRevision(t *testing.T) {
	branch, mock := newQueueAPI(t)
	policy := nativeDeploymentPolicy()
	actor := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
	genesis, err := Genesis(actor, policy, "pinned", "genesis", 1000)
	if err != nil {
		t.Fatal(err)
	}

	nodes := []WorkDefinition{}
	for _, key := range []string{"pinned", "unpinned", "unrelated"} {
		node, err := NewWork([]byte(`{"task":"native pin"}`), "native-pin", key, "default", policy, 2000)
		if err != nil {
			t.Fatal(err)
		}
		if key == "pinned" {
			node.ExecutionRef = policy.Pools["default"].Profiles["alpha"].Ref
		}
		if key == "unrelated" {
			node.WorkerProfile, node.LogicalContract = "beta", policy.Pools["default"].Profiles["beta"].LogicalContract
		}
		nodes = append(nodes, node)
	}
	submit, _ := NewRequest("native-pin", "submit", actor, SubmitParameters{Nodes: nodes})
	commits, _, _, err := BuildCandidate([]QueueCommit{genesis}, actor, submit, 2000)
	if err != nil {
		t.Fatal(err)
	}
	installMockLog(t, mock, commits)
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	mock.sources = map[string]string{"alpha": approvedWorkerSource, "beta": approvedWorkerSource}
	mock.workerRoutes = map[string]string{
		".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64)),
		".github/workflows/beta.lock.yml":  stampedWorker(strings.Repeat("b", 64)),
	}
	mock.workerStates = map[string]string{".github/workflows/alpha.lock.yml": "disabled_manually"}
	for _, available := range []bool{false, true} {
		if available {
			mock.workerStates[".github/workflows/alpha.lock.yml"] = "active"
		}
		operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "alpha")
		if err != nil || len(operations) != 2 {
			t.Fatalf("native deployment did not update current and pinned availability together: %d %v", len(operations), err)
		}
		request, _ := NewRequest(map[bool]string{false: "paused-pin", true: "resumed-pin"}[available], "deployment", actor, OperationsParameters{Operations: operations})
		if _, err := branch.Publish(context.Background(), actor, request); err != nil {
			t.Fatalf("sequential current/historical CAS evidence failed: %v", err)
		}
		next, err := branch.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		updated, err := Replay(next)
		if err != nil {
			t.Fatal(err)
		}
		deployment := updated.Deployments["default"]["alpha"]
		if deployment.CurrentRef != strings.Repeat("f", 40) || deployment.Revisions[nodes[0].ExecutionRef].Available != available {
			t.Fatal("historical pin availability rewound or changed the current deployment")
		}
		if !sameJSON(state.Works, updated.Works) || !sameJSON(state.Clocks, updated.Clocks) || !sameJSON(state.Policy, updated.Policy) {
			t.Fatal("availability evolution changed admitted Work, economics or fairness debt")
		}
		reason := "worker_unavailable"
		if available {
			reason = "ready"
		}
		for _, node := range nodes[:2] {
			explanation, err := ExplainWork(updated, node.WorkID, 3000)
			if err != nil || explanation.Reason != reason {
				t.Fatalf("affected pinned/unpinned Work did not pause or resume locally: %+v %v", explanation, err)
			}
		}
		explanation, err := ExplainWork(updated, nodes[2].WorkID, 3000)
		if err != nil || explanation.Reason != "ready" {
			t.Fatalf("historical target pause blocked unrelated Work: %+v %v", explanation, err)
		}
		state = updated
	}
}

func TestNativeDeploymentPropagatesMetadataAPIErrorsWithoutAvailabilityChanges(t *testing.T) {
	for _, endpoint := range []string{"source", "artifact", "registration"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
			t.Run(endpoint+"/"+http.StatusText(status), func(t *testing.T) {
				branch, mock := newQueueAPI(t)
				policy := nativeDeploymentPolicy()
				actor := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
				genesis, err := Genesis(actor, policy, "metadata-errors", "genesis", 1000)
				if err != nil {
					t.Fatal(err)
				}
				installMockLog(t, mock, []QueueCommit{genesis})
				state, err := Replay([]QueueCommit{genesis})
				if err != nil {
					t.Fatal(err)
				}
				mock.sources = map[string]string{"alpha": approvedWorkerSource}
				route := ".github/workflows/alpha.lock.yml"
				mock.workerRoutes = map[string]string{route: stampedWorker(strings.Repeat("a", 64))}
				switch endpoint {
				case "source":
					mock.sourceStatuses = map[string]int{"alpha": status}
				case "artifact":
					mock.workerStatuses = map[string]int{route: status}
				case "registration":
					mock.registrationStatuses = map[string]int{route: status}
				}
				operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "alpha")
				if !hasStatus(err, status) || len(operations) != 0 || mock.refWrites != 0 {
					t.Fatalf("metadata API error became an unavailable deployment: status=%d ops=%d err=%v", status, len(operations), err)
				}
				if endpoint == "artifact" {
					if _, err := branch.PolicyFromConfig(context.Background()); !hasStatus(err, status) {
						t.Fatalf("contract discovery silently downgraded a permission failure to legacy: %v", err)
					}
				}
			})
		}
	}
}
