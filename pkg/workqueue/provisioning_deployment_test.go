package workqueue

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func nativeDeploymentPolicy() Policy {
	policy := DefaultPolicy("", testRepository)
	policy.Authorization = "aw"
	policy.Producers = map[string]ProducerRule{}
	pool := policy.Pools["default"]
	template := pool.Profiles["default"]
	template.Ref = strings.Repeat("e", 40)
	pool.Profiles = map[string]WorkerProfile{}
	for _, name := range []string{"alpha", "beta"} {
		profile := template
		profile.Workflow = ".github/workflows/" + name + ".lock.yml"
		profile.LogicalContract = strings.Repeat(map[string]string{"alpha": "a", "beta": "b"}[name], 64)
		pool.Profiles[name] = profile
	}
	pool.DefaultProfile = "alpha"
	policy.Pools["default"] = pool
	policy.Pools["reports"] = pool
	return policy
}

func stampedWorker(contract string) string {
	return "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\njobs:\n  worker:\n    steps:\n      - env:\n          GH_AW_WORK_QUEUE_CONTRACT: \"" + contract + "\"\n"
}

func TestNativeDeploymentFromConfigPausesOnlyAffectedTargets(t *testing.T) {
	for _, scenario := range []string{"compatible", "incompatible", "missing", "inactive", "missing stamp", "removed approval"} {
		t.Run(scenario, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			policy := nativeDeploymentPolicy()
			administrator := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
			genesis, err := Genesis(administrator, policy, "contracted", "genesis", 1000)
			if err != nil {
				t.Fatal(err)
			}
			commits := []QueueCommit{genesis}
			nodes := []WorkDefinition{}
			for _, name := range []string{"alpha", "beta"} {
				node, err := NewWork([]byte(`{"task":"pending"}`), "pending", name, "default", policy, 2000)
				if err != nil {
					t.Fatal(err)
				}
				node.WorkerProfile = name
				node.LogicalContract = policy.Pools["default"].Profiles[name].LogicalContract
				nodes = append(nodes, node)
			}
			submit, _ := NewRequest("pending", "submit", administrator, SubmitParameters{Nodes: nodes})
			commits, _, _, err = BuildCandidate(commits, administrator, submit, 2000)
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
			betaRoute := ".github/workflows/beta.lock.yml"
			switch scenario {
			case "incompatible":
				mock.workerRoutes[betaRoute] = stampedWorker(strings.Repeat("c", 64))
			case "missing":
				mock.workerStatuses = map[string]int{betaRoute: http.StatusNotFound}
			case "inactive":
				mock.workerStates = map[string]string{betaRoute: "disabled_manually"}
			case "missing stamp":
				mock.workerRoutes[betaRoute] = "on:\n  workflow_dispatch:\n    inputs:\n      work_queue_assignment:\n        type: string\n"
			case "removed approval":
				mock.sources["beta"] = "---\ntools:\n  work-queue: {}\n---\nNo longer a worker"
			}
			operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "", "")
			if err != nil || len(operations) != 4 {
				t.Fatalf("per-target availability blocked the whole registry: %v", err)
			}
			for route, reads := range mock.routeReads {
				if reads != 1 {
					t.Fatalf("inherited pool repeated compiled route reads: %s %d", route, reads)
				}
			}
			for _, ref := range mock.configRefs {
				if ref != strings.Repeat("f", 40) {
					t.Fatalf("deployment trusted moving or ambient source revision: %s", ref)
				}
			}
			request, _ := NewRequest("deployment", "deployment", administrator, OperationsParameters{Operations: operations})
			if _, err := branch.Publish(context.Background(), administrator, request); err != nil {
				t.Fatalf("pending Work required queue draining for a deployment: %v", err)
			}
			next, err := branch.Read(context.Background())
			if err != nil || len(next) != len(commits)+1 {
				t.Fatal("deployment failed to append durable route authority")
			}
			updated, err := Replay(next)
			if err != nil || !sameJSON(updated.Policy, state.Policy) || updated.PolicyEpoch != state.PolicyEpoch ||
				!sameJSON(updated.Clocks, state.Clocks) || !sameJSON(updated.Works, state.Works) || len(updated.Claims) != 0 {
				t.Fatalf("deployment changed scheduling economics, existing Work or authority: %v", err)
			}
			for index := range commits {
				if !sameJSON(next[index], commits[index]) {
					t.Fatal("deployment rewrote immutable queue history")
				}
			}
			future := FuturePolicy(updated)
			if future.Pools["default"].Profiles["alpha"].Ref != strings.Repeat("f", 40) {
				t.Fatal("compatible deployment did not expose the new immutable execution route")
			}
			explanation, err := ExplainWork(updated, nodes[1].WorkID, time.Now().UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]string{
				"compatible": "ready", "incompatible": "worker_incompatible", "missing": "worker_unavailable",
				"inactive": "worker_unavailable", "missing stamp": "worker_unavailable", "removed approval": "worker_unavailable",
			}[scenario]
			if explanation.Reason != expected {
				t.Fatalf("affected worker did not pause locally: got %s want %s", explanation.Reason, expected)
			}
			if alpha, err := ExplainWork(updated, nodes[0].WorkID, time.Now().UnixMilli()); err != nil || alpha.Reason != "ready" {
				t.Fatalf("unrelated compatible worker stopped running: %+v %v", alpha, err)
			}
			writes, reads := mock.refWrites, mock.workerReads
			if again, err := branch.Publish(context.Background(), administrator, request); err != nil || again.Changed ||
				mock.refWrites != writes || mock.workerReads != reads {
				t.Fatalf("deployment retry changed routes or charged twice: %v", err)
			}
		})
	}
}

func TestNativeDeploymentFromConfigClosedSelection(t *testing.T) {
	policy := nativeDeploymentPolicy()
	actor := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
	commit, err := Genesis(actor, policy, "contracted", "genesis", 1000)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay([]QueueCommit{commit})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []Assignment{{Pool: "missing"}, {WorkerProfile: "missing"}} {
		branch, mock := newQueueAPI(t)
		if _, err := branch.DeploymentOperationsFromConfig(context.Background(), state, target.Pool, target.WorkerProfile); err == nil {
			t.Fatal("unknown selected deployment target was accepted")
		}
		if mock.workerReads != 0 || len(mock.configRefs) != 0 || mock.refWrites != 0 {
			t.Fatal("invalid deployment selector made native reads or mutations")
		}
	}
}

func TestNativeDeploymentCountsOnlyChangedTargets(t *testing.T) {
	branch, mock := newQueueAPI(t)
	policy := nativeDeploymentPolicy()
	policy.Limits.Operations = 3
	for name, pool := range policy.Pools {
		for worker, profile := range pool.Profiles {
			profile.Ref = strings.Repeat("f", 40)
			pool.Profiles[worker] = profile
		}
		policy.Pools[name] = pool
	}
	actor := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
	genesis, err := Genesis(actor, policy, "contracted", "genesis", 1000)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay([]QueueCommit{genesis})
	if err != nil {
		t.Fatal(err)
	}
	mock.sources = map[string]string{"alpha": approvedWorkerSource, "beta": approvedWorkerSource}
	mock.workerRoutes = map[string]string{
		".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64)),
		".github/workflows/beta.lock.yml":  stampedWorker(strings.Repeat("b", 64)),
	}
	if operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "", ""); err != nil || len(operations) != 0 {
		t.Fatalf("unchanged global registry incorrectly exceeded the operation limit: %v", err)
	}
	mock.workerStates = map[string]string{
		".github/workflows/alpha.lock.yml": "disabled_manually",
		".github/workflows/beta.lock.yml":  "disabled_manually",
	}
	if _, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "", ""); err == nil || !strings.Contains(err.Error(), "resource_limit") {
		t.Fatalf("oversized changed deployment prefix was not bounded: %v", err)
	}
	if operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "default", "alpha"); err != nil || len(operations) != 1 {
		t.Fatalf("narrow deployment selector could not fit the operation budget: %v", err)
	}
	if mock.refWrites != 0 {
		t.Fatal("building deployment proposals changed the queue")
	}
}

func TestCompiledWorkerContractRequiresConsistentLiteralStamp(t *testing.T) {
	valid := strings.Repeat("a", 64)
	for _, content := range []string{
		strings.ReplaceAll(stampedWorker(valid), valid, "${{ inputs.contract }}"),
		stampedWorker(valid) + "env:\n  GH_AW_WORK_QUEUE_CONTRACT: \"" + strings.Repeat("b", 64) + "\"\n",
		strings.ReplaceAll(stampedWorker(valid), "type: string", "type: boolean"),
		"on: [\n",
	} {
		if _, err := compiledWorkerContract([]byte(content)); err == nil {
			t.Fatal("untrusted, conflicting or malformed compiled contract accepted")
		}
	}

	if contract, err := compiledWorkerContract([]byte(stampedWorker(valid))); err != nil || contract != valid {
		t.Fatalf("literal compiled worker contract rejected: %s %v", contract, err)
	}
}

func TestNativeDeploymentRejectsForgedContractOrAuthority(t *testing.T) {
	for _, scenario := range []string{"contract", "principal", "scope", "unavailable contract"} {
		t.Run(scenario, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			policy := nativeDeploymentPolicy()
			actor := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
			commit, err := Genesis(actor, policy, "contracted", "genesis", 1000)
			if err != nil {
				t.Fatal(err)
			}
			installMockLog(t, mock, []QueueCommit{commit})
			mock.sources = map[string]string{"alpha": approvedWorkerSource}
			mock.workerRoutes = map[string]string{".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64))}
			profile := policy.Pools["default"].Profiles["alpha"]
			original := profile
			profile.Ref = strings.Repeat("f", 40)
			available := true
			switch scenario {
			case "contract", "unavailable contract":
				profile.LogicalContract = strings.Repeat("d", 64)
				available = scenario != "unavailable contract"
			case "principal":
				profile.Principal = "2002"
			case "scope":
				profile.EffectScope = "other/repository"
			}
			operation, err := Op(DeploymentOperation{
				Kind: "Deployment", Pool: "default", WorkerProfile: "alpha",
				ExpectedRef: original.Ref, ExpectedContract: original.LogicalContract,
				Profile: profile, Available: available, Reason: "worker_deployed",
			})
			if err != nil {
				t.Fatal(err)
			}
			request, _ := NewRequest("forged-deployment", "deployment", actor, OperationsParameters{Operations: []Operation{operation}})
			if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.Contains(err.Error(), "deployment_invalid") {
				t.Fatalf("forged native deployment became durable authority: %v", err)
			}
			if mock.refWrites != 0 {
				t.Fatal("invalid deployment changed queue authority")
			}
		})
	}
}

func TestNativeDeploymentCASRejectsStaleRouteProposal(t *testing.T) {
	branch, mock := newQueueAPI(t)
	policy := nativeDeploymentPolicy()
	actor := Actor{Role: "administrator", Principal: testPrincipal, Repository: testRepository}
	commit, err := Genesis(actor, policy, "contracted", "genesis", 1000)
	if err != nil {
		t.Fatal(err)
	}
	commits := []QueueCommit{commit}
	installMockLog(t, mock, commits)
	mock.sources = map[string]string{"alpha": approvedWorkerSource, "beta": approvedWorkerSource}
	mock.workerRoutes = map[string]string{
		".github/workflows/alpha.lock.yml": stampedWorker(strings.Repeat("a", 64)),
		".github/workflows/beta.lock.yml":  stampedWorker(strings.Repeat("b", 64)),
	}
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	operations, err := branch.DeploymentOperationsFromConfig(context.Background(), state, "", "")
	if err != nil {
		t.Fatal(err)
	}
	profile := policy.Pools["default"].Profiles["alpha"]
	concurrent := profile
	concurrent.Ref = strings.Repeat("d", 40)
	otherOperation := mustOp(t, DeploymentOperation{
		Kind: "Deployment", Pool: "default", WorkerProfile: "alpha",
		ExpectedRef: profile.Ref, ExpectedContract: profile.LogicalContract,
		Profile: concurrent, Available: true, Reason: "worker_deployed",
	})
	mock.concurrent = func() {
		otherRequest, _ := NewRequest("concurrent-deployment", "deployment", actor, OperationsParameters{Operations: []Operation{otherOperation}})
		next, _, _, err := BuildCandidate(commits, actor, otherRequest, 2000)
		if err != nil {
			t.Fatal(err)
		}
		installMockLog(t, mock, next)
	}
	request, _ := NewRequest("stale-deployment", "deployment", actor, OperationsParameters{Operations: operations})
	if _, err := branch.Publish(context.Background(), actor, request); err == nil || !strings.Contains(err.Error(), "deployment_conflict") {
		t.Fatalf("stale deployment silently overwrote concurrent route authority: %v", err)
	}
	next, err := branch.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Replay(next)
	if err != nil {
		t.Fatal(err)
	}
	future := FuturePolicy(updated)
	if len(next) != 2 || future.Pools["default"].Profiles["alpha"].Ref != concurrent.Ref ||
		future.Pools["default"].Profiles["beta"].Ref != profile.Ref || future.Pools["reports"].Profiles["alpha"].Ref != profile.Ref {
		t.Fatal("stale deployment published a partial prefix or overwrote concurrent route authority")
	}
}
