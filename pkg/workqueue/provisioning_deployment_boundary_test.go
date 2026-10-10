package workqueue

import (
	"context"
	"strings"
	"testing"
)

func TestNativeDeploymentRejectsCallerSelectedRevisionAndAvailability(t *testing.T) {
	for _, role := range []string{"administrator", "producer"} {
		for _, scenario := range []struct {
			name      string
			ref       string
			available bool
			activate  bool
			inactive  bool
		}{
			{"foreign revision with matching stamp", strings.Repeat("d", 40), true, true, false},
			{"false default availability", strings.Repeat("f", 40), false, true, false},
			{"false installed availability", strings.Repeat("e", 40), false, true, false},
			{"false historical availability", strings.Repeat("e", 40), false, false, false},
			{"unregistered historical revision", strings.Repeat("d", 40), true, false, false},
			{"true historical availability", strings.Repeat("e", 40), true, false, true},
		} {
			t.Run(role+"/"+scenario.name, func(t *testing.T) {
				branch, mock := newQueueAPI(t)
				policy := nativeDeploymentPolicy()
				genesis, err := Genesis(testActor("administrator"), policy, "boundary", "genesis", 1000)
				if err != nil {
					t.Fatal(err)
				}
				installMockLog(t, mock, []QueueCommit{genesis})
				mock.sources = map[string]string{"alpha": approvedWorkerSource}
				mock.workerRoutes = map[string]string{".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64))}
				if scenario.inactive {
					mock.workerState = "disabled_manually"
				}
				actor := testActor(role)
				profile := policy.Pools["default"].Profiles["alpha"]
				proposed := profile
				proposed.Ref = scenario.ref
				operation := mustOp(t, DeploymentOperation{
					Kind: "Deployment", Pool: "default", WorkerProfile: "alpha",
					ExpectedRef: profile.Ref, ExpectedContract: profile.LogicalContract,
					Profile: proposed, Available: scenario.available, Activate: &scenario.activate,
					Reason: "caller_selected",
				})
				request, err := NewRequest("boundary-update", "deployment", actor, OperationsParameters{Operations: []Operation{operation}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
					!strings.HasPrefix(err.Error(), "deployment_invalid:") {
					t.Fatalf("caller-selected worker code or availability became durable authority: %v", err)
				}
				if mock.refWrites != 0 || len(mock.logs) != 1 {
					t.Fatal("rejected deployment wrote authoritative queue state")
				}
			})
		}
	}
}

func TestNativeDeploymentTrustedParticipantsFollowDefaultReference(t *testing.T) {
	for _, role := range []string{"administrator", "producer"} {
		for _, available := range []bool{false, true} {
			t.Run(role+"/"+map[bool]string{false: "inactive", true: "active"}[available], func(t *testing.T) {
				branch, mock := newQueueAPI(t)
				mock.noAdmin = role != "administrator"
				actor, err := branch.Authenticate(context.Background(), role)
				if err != nil {
					t.Fatal(err)
				}
				policy := nativeDeploymentPolicy()
				genesis, err := Genesis(testActor("administrator"), policy, "approved", "genesis", 1000)
				if err != nil {
					t.Fatal(err)
				}
				installMockLog(t, mock, []QueueCommit{genesis})
				state, err := Replay([]QueueCommit{genesis})
				if err != nil {
					t.Fatal(err)
				}
				mock.sources = map[string]string{"alpha": approvedWorkerSource}
				mock.workerRoutes = map[string]string{".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64))}
				if !available {
					mock.workerState = "disabled_manually"
				}
				operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "alpha")
				if err != nil || len(operations) != 1 {
					t.Fatalf("approved participant could not resolve default deployment: %v", err)
				}
				request, err := NewRequest("approved-update", "deployment", actor, OperationsParameters{Operations: operations})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := branch.Publish(context.Background(), actor, request); err != nil {
					t.Fatalf("host-approved deployment required queue-specific administrator enrollment: %v", err)
				}
				commits, err := branch.Read(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				updated, err := Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
				deployment := updated.Deployments["default"]["alpha"]
				if deployment.CurrentRef != strings.Repeat("f", 40) || deployment.Revisions[deployment.CurrentRef].Available != available {
					t.Fatal("deployment did not retain verified default code and native availability")
				}
			})
		}
	}
}

func TestNativeDeploymentRechecksHostBeforePublishing(t *testing.T) {
	for _, scenario := range []string{"default advanced", "worker disabled", "approval removed"} {
		t.Run(scenario, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			policy := nativeDeploymentPolicy()
			actor := testActor("producer")
			genesis, err := Genesis(testActor("administrator"), policy, "refresh", "genesis", 1000)
			if err != nil {
				t.Fatal(err)
			}
			installMockLog(t, mock, []QueueCommit{genesis})
			state, err := Replay([]QueueCommit{genesis})
			if err != nil {
				t.Fatal(err)
			}
			mock.sources = map[string]string{"alpha": approvedWorkerSource}
			mock.workerRoutes = map[string]string{".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64))}
			operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "alpha")
			if err != nil || len(operations) != 1 {
				t.Fatalf("could not prepare host-approved deployment: %v", err)
			}
			switch scenario {
			case "default advanced":
				mock.defaultRevision = strings.Repeat("c", 40)
			case "worker disabled":
				mock.workerState = "disabled_manually"
			case "approval removed":
				mock.sources["alpha"] = "---\ntools:\n  work-queue:\n    worker: false\n---\n"
			}
			request, err := NewRequest("refresh-update", "deployment", actor, OperationsParameters{Operations: operations})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
				!strings.HasPrefix(err.Error(), "deployment_invalid:") || mock.refWrites != 0 {
				t.Fatalf("stale host approval became durable worker authority: %v", err)
			}
		})
	}
}
