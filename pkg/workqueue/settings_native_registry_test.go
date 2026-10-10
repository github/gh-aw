package workqueue

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNativeSettingsAWRegistryVerifiesOnlySubmittedWorkers(t *testing.T) {
	for _, unavailable := range []string{"missing", "inactive", "invalid compiled artifact"} {
		t.Run(unavailable, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.sources = map[string]string{"worker": approvedWorkerSource}
			mock.workerStatuses = map[string]int{}
			mock.workerStates = map[string]string{}
			mock.workerRoutes = map[string]string{}
			for index := range 30 {
				name := fmt.Sprintf("report-%02d", index)
				mock.sources[name] = approvedWorkerSource
				route := ".github/workflows/" + name + ".lock.yml"
				switch unavailable {
				case "missing":
					mock.workerStatuses[route] = http.StatusNotFound
				case "inactive":
					mock.workerStates[route] = "disabled_manually"
				default:
					mock.workerRoutes[route] = "on: push\n"
				}
			}
			mock.settings = `{"work_queue":{"pools":{"reports":{"concurrency":3}}}}`
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}
			policy, err := branch.PolicyFromConfig(context.Background())
			if err != nil || len(policy.Pools["default"].Profiles) != 31 || len(mock.registrationReads) != 0 {
				t.Fatalf("registry discovery required unrelated live workflows: %v", err)
			}
			discoveryReads := mock.workerReads
			branch.PolicyProposal = &policy
			node, err := NewWork([]byte(`{"task":"selected worker"}`), "selected", "root", "default", policy, 1000)
			if err != nil {
				t.Fatal(err)
			}
			node.WorkerProfile = "worker"
			request, _ := NewRequest("selected", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
			if _, err := branch.Publish(context.Background(), actor, request); err != nil || mock.refWrites != 1 {
				t.Fatalf("unavailable unused registry routes blocked first submission: %v", err)
			}
			if mock.routeReads[".github/workflows/worker.lock.yml"] == 0 || mock.workerRef != strings.Repeat("f", 40) {
				t.Fatal("selected worker bypassed exact immutable route verification")
			}
			if mock.workerReads != discoveryReads+1 {
				t.Fatal("submission reread unselected immutable worker contracts")
			}
			for route := range mock.registrationReads {
				if route != ".github/workflows/worker.lock.yml" {
					t.Fatalf("submission verified an unrelated route: %s", route)
				}
			}
			commits, err := branch.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(commits)
			if err != nil || !sameJSON(*state.Policy, policy) {
				t.Fatal("bootstrap pruned the immutable shared registry")
			}
			report, err := NewWork([]byte(`{"task":"unavailable selected report"}`), "report", "root", "reports", policy, 1000)
			if err != nil {
				t.Fatal(err)
			}
			report.WorkerProfile = "report-00"
			unavailableRequest, _ := NewRequest("unavailable-report", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{report}})
			routeReads := mock.workerReads
			if _, err := branch.Publish(context.Background(), actor, unavailableRequest); err != nil || mock.refWrites != 2 || mock.workerReads != routeReads {
				t.Fatalf("installed AW admission depended on current native route availability: %v", err)
			}
			administrator, err := branch.Authenticate(context.Background(), "administrator")
			if err != nil {
				t.Fatal(err)
			}
			dispatchRequest, _ := NewRequest("dispatch-selected", "dispatch_next", administrator, DispatchParameters{
				Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
			})
			mock.workerStates[".github/workflows/worker.lock.yml"] = "disabled_manually"
			dispatched, err := branch.Publish(context.Background(), administrator, dispatchRequest)
			if err != nil || len(dispatched.Decision.Assignments) != 1 || dispatched.Decision.Assignments[0].WorkerProfile != "worker" || mock.refWrites != 3 || mock.workerReads != routeReads {
				t.Fatalf("native reservation incorrectly acted as a workflow launch: %v", err)
			}
		})
	}
}

func TestNativeSettingsAWSelectedRouteFailureFencesEntireBootstrap(t *testing.T) {
	for _, unavailable := range []string{"missing", "inactive", "invalid compiled artifact"} {
		t.Run(unavailable, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.sources = map[string]string{"worker": approvedWorkerSource, "report": approvedWorkerSource}
			policy, err := branch.PolicyFromConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			route := ".github/workflows/report.lock.yml"
			switch unavailable {
			case "missing":
				mock.workerStatuses = map[string]int{route: http.StatusNotFound}
			case "inactive":
				mock.workerStates = map[string]string{route: "disabled_manually"}
			default:
				mock.workerRoutes = map[string]string{route: "on: push\n"}
			}
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}
			nodes := []WorkDefinition{}
			for _, worker := range []string{"worker", "report"} {
				node, err := NewWork([]byte(`{"task":"selected route"}`), "prefix", worker, "default", policy, 1000)
				if err != nil {
					t.Fatal(err)
				}
				node.WorkerProfile = worker
				nodes = append(nodes, node)
			}
			branch.PolicyProposal = &policy
			request, _ := NewRequest("prefix", "submit", actor, SubmitParameters{Nodes: nodes})
			if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.HasPrefix(err.Error(), "policy_missing:") {
				t.Fatalf("selected unavailable worker became authoritative: %v", err)
			}
			if mock.refWrites != 0 || len(mock.logs) != 0 || mock.head != "" {
				t.Fatal("failed selected route verification partially initialized Policy or Work")
			}
		})
	}
}
