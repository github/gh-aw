package workqueue

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const approvedWorkerSource = "---\non: workflow_dispatch\ntools:\n  work-queue:\n    worker: true\n---\nWorker"

func TestNativeSettingsDefaultRefMultiWorkerFirstSubmit(t *testing.T) {
	for _, configuration := range []string{
		"", "{}", `{"work_queue":{}}`,
		`{"work_queue":{"concurrency":2,"pending_limit":12,"retry":{"max_attempts":5,"backoff_seconds":60},"pools":{"reviews":{"concurrency":1,"per_account_limit":1}},"accounting_weights":{"alpha":1,"zeta":1}}}`,
		`{"work_queue":{"issues":{"label":"work"}}}`,
	} {
		t.Run(configuration, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.settings = configuration
			mock.sources = map[string]string{
				"zeta": approvedWorkerSource, "alpha": approvedWorkerSource,
				"ordinary": "---\non: workflow_dispatch\ntools:\n  work-queue: {}\n---\nOrdinary producer",
				"not-aw":   "name: Ordinary Actions workflow",
			}
			mock.noAdmin, mock.ambiguous = true, true
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}
			policy, err := branch.PolicyFromConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			pool := policy.Pools["default"]
			if policy.Authorization != "aw" || len(policy.Producers) != 0 || pool.DefaultProfile != "alpha" || len(pool.Profiles) != 2 {
				t.Fatalf("native defaults require authored identities or ignored AW workers: %+v", policy)
			}
			for _, name := range []string{"alpha", "zeta"} {
				profile := pool.Profiles[name]
				if profile.Workflow != ".github/workflows/"+name+".lock.yml" ||
					profile.Ref != strings.Repeat("f", 40) || profile.Principal != "" || profile.MaxClaims != 1 {
					t.Fatalf("worker route is not AW-managed singleton at the default revision: %+v", profile)
				}
			}
			if strings.Contains(configuration, `"concurrency":2`) {
				if pool.NativeLimit != 2 || policy.Limits.PendingNodes != 12 || pool.Retry.MaxAttempts != 5 ||
					pool.Retry.BackoffMS != 60000 || policy.Pools["reviews"].NativeLimit != 1 ||
					policy.Pools["reviews"].PerAccountLimit != 1 || policy.AccountingWeights["zeta"] != 1 ||
					!sameJSON(policy.Pools["reviews"].Profiles, pool.Profiles) {
					t.Fatal("native scheduling did not consume target-repository settings")
				}
			} else if pool.NativeLimit != 16 || policy.Limits.PendingNodes != 4096 || pool.Retry.MaxAttempts != 3 || pool.Retry.BackoffMS != 30000 {
				t.Fatal("missing target settings omitted ordinary defaults")
			}
			node, err := NewWork([]byte(`{"task":"native multi-worker"}`), "multi-worker", "root", "default", policy, 1000)
			if err != nil {
				t.Fatal(err)
			}
			node.WorkerProfile = "zeta"
			request, err := NewRequest("multi-worker", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := branch.Publish(context.Background(), actor, request)
			if err != nil || result.Commit == nil || mock.refWrites != 1 {
				t.Fatalf("default AW policy did not bootstrap once with caller identity: %+v %v", result, err)
			}
			commits, err := branch.Read(context.Background())
			if err != nil || len(commits) != 1 || len(commits[0].Operations) != 2 || !sameJSON(commits[0].Request, request) ||
				!sameJSON(commits[0].Actor, actor) || commits[0].Actor.Principal != testPrincipal {
				t.Fatal("native bootstrap changed identity, intent, or atomic Policy/Work admission")
			}
			state, err := Replay(commits)
			if err != nil || !sameJSON(*state.Policy, policy) || state.Works[node.WorkID].WorkerProfile != "zeta" {
				t.Fatalf("default first submit ignored chosen approved worker: %v", err)
			}
			if mock.routeReads[".github/workflows/alpha.lock.yml"] != 2 || mock.routeReads[".github/workflows/zeta.lock.yml"] != 3 ||
				mock.routeReads[".github/workflows/ordinary.lock.yml"] != 0 {
				t.Fatal("bootstrap exceeded bounded immutable discovery or skipped the submitted AW route")
			}
			if mock.registrationReads[".github/workflows/alpha.lock.yml"] != 0 || mock.registrationReads[".github/workflows/zeta.lock.yml"] != 1 {
				t.Fatal("bootstrap verified native availability for unrelated routes")
			}
			for _, ref := range mock.configRefs {
				if ref != strings.Repeat("f", 40) {
					t.Fatalf("native settings/discovery used an ambient checkout or moving ref: %s", ref)
				}
			}
			reads := len(mock.configRefs)
			mock.settings = `{"work_queue":null}`
			mock.workerStatus = http.StatusNotFound
			if again, err := branch.Publish(context.Background(), actor, request); err != nil || again.Changed || len(mock.configRefs) != reads || mock.refWrites != 1 {
				t.Fatalf("retry reinitialized Policy, rediscovered settings, or lost original receipt: %v", err)
			}
		})
	}
}

func TestNativeSettingsRejectInvalidConfigBeforeBootstrapWrites(t *testing.T) {
	for _, configuration := range []string{
		`null`, `[]`, `{"work_queue":null}`, `{"work_queue":{"concurrency":0}}`,
		`{"work_queue":{"retry":{"backoff_seconds":3601}}}`, `{"work_queue":{"producers":{}}}`,
		`{"work_queue":{"pools":{"default":{"profiles":{}}}}}`,
		`{"work_queue":{"issues":null}}`, `{"work_queue":{"issues":{"label":""}}}`,
	} {
		t.Run(configuration, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			mock.settings = configuration
			actor, err := branch.Authenticate(context.Background(), "producer")
			if err != nil {
				t.Fatal(err)
			}
			node, err := NewWork([]byte(`{"task":"invalid settings"}`), "invalid", "root", "default", DefaultPolicy(actor.Principal, actor.Repository), 1000)
			if err != nil {
				t.Fatal(err)
			}
			request, _ := NewRequest("invalid", "submit", actor, SubmitParameters{Nodes: []WorkDefinition{node}})
			if _, err := branch.Publish(context.Background(), actor, request); err == nil ||
				!strings.Contains(err.Error(), "work_queue") || !strings.Contains(err.Error(), ".github/workflows/aw.json") {
				t.Fatalf("malformed native settings silently defaulted: %v", err)
			}
			if mock.refWrites != 0 || len(mock.logs) != 0 || mock.workerReads != 0 {
				t.Fatal("malformed native settings caused provisioning or queue writes")
			}
		})
	}
}

func TestNativeSettingsDiscoveryFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*queueAPI)
		message string
	}{
		{"no worker", func(mock *queueAPI) {
			mock.sources = map[string]string{"ordinary": "---\non: workflow_dispatch\ntools:\n  work-queue: {}\n---\nOrdinary"}
		}, "no approved AW worker"},
		{"unverified default ref", func(mock *queueAPI) { mock.defaultRef = "refs/heads/other" }, "not immutable"},
		{"inaccessible settings", func(mock *queueAPI) { mock.settingsStatus = http.StatusForbidden }, "work_queue"},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			test.prepare(mock)
			if _, err := branch.PolicyFromConfig(context.Background()); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("unapproved native config did not fail closed: %v", err)
			}
			if mock.refWrites != 0 || len(mock.logs) != 0 {
				t.Fatal("configuration discovery wrote queue state")
			}
		})
	}
}

func TestNativeSettingsQuiescentAdministratorUpdate(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "drained", true: "active work"}[active], func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits := testGenesis(t, nil)
			if active {
				commits = testSubmit(t, commits, "existing-work", testNode(t, commits, "existing-work"))
			}
			installMockLog(t, mock, commits)
			mock.settings = `{"work_queue":{"concurrency":2}}`
			mock.workerStatus = http.StatusNotFound
			actor, err := branch.Authenticate(context.Background(), "administrator")
			if err != nil {
				t.Fatal(err)
			}
			policy, err := branch.PolicyFromConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			request, _ := NewRequest("config-update", "policy", actor, OperationsParameters{Operations: []Operation{
				mustOp(t, map[string]any{"kind": "Policy", "epoch": "config-update", "policy": policy}),
			}})
			_, err = branch.Publish(context.Background(), actor, request)
			if active {
				if err == nil || !strings.Contains(err.Error(), "policy_not_quiescent") || mock.refWrites != 0 {
					t.Fatalf("config update rewrote an active queue: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			next, err := branch.Read(context.Background())
			if err != nil || len(next) != len(commits)+1 || !sameJSON(commits[0], next[0]) {
				t.Fatal("prospective config update rewrote historical authority")
			}
			state, err := Replay(next)
			if err != nil || state.Policy.Authorization != "aw" || state.Policy.Pools["default"].NativeLimit != 2 {
				t.Fatalf("prospective config update did not install managed scheduling: %v", err)
			}
		})
	}
}
