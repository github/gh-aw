package workqueue

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"
)

func TestAWPoolInstallRegistersNewWorkerWithOutstandingClaims(t *testing.T) {
	for _, target := range []string{"smoke", "default"} {
		t.Run(target, func(t *testing.T) {
			commits := testGenesis(t, deploymentPolicy)
			commits = testSubmit(t, commits, "existing", testNode(t, commits, "pending"))
			commits, _ = testGrant(t, commits, "active-claim", 1, 1)
			commits = deployTestWorker(t, commits, "deployed", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
			before, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			config := before.Policy.Pools["default"]
			config.Profiles = maps.Clone(config.Profiles)
			profile := config.Profiles["default"]
			profile.Workflow, profile.Ref, profile.LogicalContract = ".github/workflows/smoke-worker.lock.yml", strings.Repeat("f", 40), strings.Repeat("c", 64)
			profile.MaxClaims, profile.ShareKeys = 1, false
			config.Profiles["smoke-worker"] = profile
			install := PoolInstallOperation{Kind: "PoolInstall", Pool: target, Config: config, ConfigRef: profile.Ref}
			commits = testOperations(t, commits, testActor("dispatcher"), "register-smoke", "pool_install", mustOp(t, install))
			after, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			if after.PolicyEpoch != before.PolicyEpoch || !sameJSON(after.Works, before.Works) ||
				!sameJSON(after.Claims, before.Claims) || !sameJSON(after.Dispatches, before.Dispatches) ||
				!sameJSON(after.Clocks, before.Clocks) || !sameJSON(after.Deployments["default"]["default"], before.Deployments["default"]["default"]) {
				t.Fatal("additive worker registration changed existing state or deployments")
			}
			if after.Deployments[target]["smoke-worker"].CurrentRef != profile.Ref {
				t.Fatal("new worker deployment was not registered")
			}
			checkpoint, err := CompactCheckpoint(commits, strings.Repeat("d", 40), testActor("administrator"), 5000)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Replay(checkpoint)
			if err != nil || !sameJSON(restored.Policy, after.Policy) || !sameJSON(restored.Deployments, after.Deployments) {
				t.Fatalf("checkpoint lost additive worker registration: %v", err)
			}
			for name, mutate := range map[string]func(*PoolInstallOperation){
				"missing-provenance": func(op *PoolInstallOperation) { op.ConfigRef = "" },
				"wrong-provenance":   func(op *PoolInstallOperation) { op.ConfigRef = strings.Repeat("a", 40) },
				"batch-authority": func(op *PoolInstallOperation) {
					worker := op.Config.Profiles["smoke-worker"]
					worker.MaxClaims = 2
					op.Config.Profiles["smoke-worker"] = worker
				},
				"existing-authority": func(op *PoolInstallOperation) {
					worker := op.Config.Profiles["default"]
					worker.MaxClaims++
					op.Config.Profiles["default"] = worker
				},
			} {
				t.Run(name, func(t *testing.T) {
					invalid := install
					invalid.Config.Profiles = maps.Clone(install.Config.Profiles)
					mutate(&invalid)
					request, err := NewRequest("invalid-"+name, "pool_install", testActor("dispatcher"), OperationsParameters{Operations: []Operation{mustOp(t, invalid)}})
					if err == nil {
						_, _, _, err = BuildCandidate(commits[:len(commits)-1], testActor("dispatcher"), request, 5000)
					}
					if err == nil {
						t.Fatal("invalid additive worker authority accepted")
					}
				})
			}
		})
	}
}

func TestNativePoolInstallVerifiesNewDefaultWorkerBeforePublication(t *testing.T) {
	for _, failure := range []string{"", "source", "stamp", "registration", "ref", "denied"} {
		t.Run(failure, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits := testGenesis(t, deploymentPolicy)
			commits = testSubmit(t, commits, "pending", testNode(t, commits, "pending"))
			installMockLog(t, mock, commits)
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			profile := state.Policy.Pools["default"].Profiles["default"]
			profile.Workflow, profile.Ref, profile.LogicalContract = ".github/workflows/smoke-worker.lock.yml", strings.Repeat("f", 40), strings.Repeat("c", 64)
			profile.MaxClaims, profile.ShareKeys = 1, false
			config := state.Policy.Pools["default"]
			config.Profiles = map[string]WorkerProfile{"smoke-worker": profile}
			config.DefaultProfile = "smoke-worker"
			mock.sources = map[string]string{"smoke-worker": approvedWorkerSource}
			mock.workerRoutes = map[string]string{profile.Workflow: stampedWorker(profile.LogicalContract)}
			switch failure {
			case "source":
				mock.sources["smoke-worker"] = "---\ntools: {}\n---\nNot a worker."
			case "stamp":
				mock.workerRoutes[profile.Workflow] = stampedWorker(strings.Repeat("d", 64))
			case "registration":
				mock.workerStates = map[string]string{profile.Workflow: "disabled_manually"}
			case "ref":
				profile.Ref = strings.Repeat("e", 40)
				config.Profiles["smoke-worker"] = profile
			case "denied":
				mock.workerStatuses = map[string]int{profile.Workflow: http.StatusForbidden}
			}
			operation := mustOp(t, PoolInstallOperation{Kind: "PoolInstall", Pool: "smoke", Config: config, ConfigRef: profile.Ref})
			request, err := NewRequest("register-smoke", "pool_install", testActor("producer"), OperationsParameters{Operations: []Operation{operation}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = branch.Publish(context.Background(), testActor("producer"), request)
			if failure != "" {
				if err == nil || mock.refWrites != 0 {
					t.Fatalf("unverified worker registered: %v", err)
				}
			} else if err != nil || mock.refWrites != 1 {
				t.Fatalf("verified worker required a drained queue: %v", err)
			}
		})
	}
}
