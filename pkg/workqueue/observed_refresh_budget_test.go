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

func TestObservationRefreshBudgetIndependentAnswers(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/observation-refresh-budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version          int `json:"version"`
		DefaultRequested int `json:"default_requested"`
		Cases            []struct {
			Name         string `json:"name"`
			Operations   int    `json:"operations"`
			LedgerBytes  int64  `json:"ledger_bytes"`
			LedgerLimit  int64  `json:"ledger_limit"`
			GrantsPaused bool   `json:"grants_paused"`
			Requested    int    `json:"requested"`
			Expected     *int   `json:"expected"`
			Error        string `json:"error"`
		} `json:"cases"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 3 || fixture.DefaultRequested != 128 || len(fixture.Cases) != 15 {
		t.Fatal("independent refresh-budget fixture shape changed")
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			state := newProjection()
			policy := DefaultPolicy(testPrincipal, testRepository)
			policy.Limits.Operations, policy.Limits.LedgerBytes = test.Operations, test.LedgerLimit
			state.Policy, state.LedgerBytes, state.GrantsPaused = &policy, test.LedgerBytes, test.GrantsPaused
			before, err := canonicalValue(state)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := ObservationRefreshBudget(state, test.Requested)
			if test.Error != "" {
				if test.Expected != nil || err == nil || !strings.HasPrefix(err.Error(), test.Error+":") {
					t.Fatalf("budget error=%v, want %s", err, test.Error)
				}
			} else if test.Expected == nil || err != nil || actual != *test.Expected {
				t.Fatalf("budget=%d, error=%v, want %v", actual, err, test.Expected)
			}
			after, err := canonicalValue(state)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("read-only refresh budget mutated its projection")
			}
		})
	}
	if _, err := ObservationRefreshBudget(newProjection(), 128); err == nil ||
		!strings.HasPrefix(err.Error(), "policy_missing:") {
		t.Fatalf("missing policy produced an optional-read budget: %v", err)
	}
	if strconv.IntSize == 64 {
		unsafe := MaxTimestamp + 1
		state := newProjection()
		policy := DefaultPolicy(testPrincipal, testRepository)
		state.Policy = &policy
		if _, err := ObservationRefreshBudget(state, int(unsafe)); err == nil ||
			!strings.HasPrefix(err.Error(), "ledger_invalid:") {
			t.Fatalf("unsafe integer produced an optional-read budget: %v", err)
		}
	}
}

func TestObservationRefreshBudgetCrossEngineParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("strict refresh-budget parity requires Node test tooling")
	}
	data, err := os.ReadFile("../../specs/work-queue/fixtures/observation-refresh-budget.json")
	if err != nil {
		t.Fatal(err)
	}
	const script = `
const fs = require("node:fs");
const { observationRefreshBudget } = require("./actions/setup/js/work_queue_limits.cjs");
const fixture = JSON.parse(fs.readFileSync(0, "utf8"));
const failures = [];
for (const test of fixture.cases) {
  const state = {
    policy: { limits: { operations: test.operations, ledger_bytes: test.ledger_limit } },
    ledgerBytes: test.ledger_bytes,
    grants_paused: test.grants_paused === true,
  };
  const before = JSON.stringify(state);
  try {
    const actual = observationRefreshBudget(state, test.requested);
    if (test.error || actual !== test.expected) failures.push({ name: test.name, actual, expected: test.error || test.expected });
  } catch (error) {
    if (!test.error || (error.message !== test.error && !error.message.startsWith(test.error + ":"))) {
      failures.push({ name: test.name, error: error.message, expected: test.error || test.expected });
    }
  }
  if (JSON.stringify(state) !== before) failures.push({ name: test.name, error: "projection_mutated" });
}
if (failures.length) {
  console.error(JSON.stringify(failures));
  process.exitCode = 1;
}
console.log(JSON.stringify({ cases: fixture.cases.length, failures: failures.length }));
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Dir = "../.."
	command.Stdin = bytes.NewReader(data)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("actual JS refresh-budget helper differs from independent native answers: %v\n%s", err, diagnostics.String())
	}
	var result struct {
		Cases    int `json:"cases"`
		Failures int `json:"failures"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Cases != 15 || result.Failures != 0 {
		t.Fatalf("actual JS omitted independent refresh-budget cases: %s, error=%v", output, err)
	}
}

func TestNativeObservationRefreshStopsBeforeDependencyProbes(t *testing.T) {
	for _, test := range []struct {
		name         string
		grantsPaused bool
		atWatermark  bool
		expected     int
	}{
		{name: "below-watermark", expected: 1},
		{name: "grants-paused", grantsPaused: true},
		{name: "exhausted-optional-watermark", atWatermark: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits := testGenesis(t, func(policy *Policy) { policy.Limits.LedgerBytes = 6000 })
			node := testNode(t, commits, "watermark-gated")
			resource := Resource{
				Kind: "issue", Host: "github.com", Repository: testRepository,
				RepositoryID: "1", ResourceID: "2", Number: "7",
			}
			node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
			commits = testSubmit(t, commits, "watermark-submit", node)
			if test.grantsPaused {
				commits = testOperations(t, commits, testActor("administrator"), "pause-refresh", "control", mustOp(t, map[string]any{
					"kind": "Control", "control": "grants_paused", "value": true, "reason": "incident",
				}))
			}
			state, err := Replay(commits)
			if err != nil {
				t.Fatal(err)
			}
			for index := 0; test.atWatermark && state.LedgerBytes < state.Policy.Limits.LedgerBytes; index++ {
				if index >= 8 {
					t.Fatal("fixture did not exhaust its optional observation watermark")
				}
				commits = testOperations(t, commits, testActor("administrator"), fmt.Sprintf("pad-watermark-%d", index),
					"control", mustOp(t, map[string]any{
						"kind": "Control", "control": "admission_paused", "value": false,
						"reason": strings.Repeat("x", 128),
					}))
				state, err = Replay(commits)
				if err != nil {
					t.Fatal(err)
				}
			}
			installMockLog(t, mock, commits)
			mock.issueStatus = 403
			before, err := canonicalValue(state)
			if err != nil {
				t.Fatal(err)
			}
			head, logs, writes := mock.head, len(mock.logs), mock.refWrites
			observations, err := branch.refreshForDispatch(context.Background(), state, "default", "watermark-refresh")
			if err != nil || observations == nil || len(observations) != test.expected ||
				mock.resourceReads != test.expected {
				t.Fatalf("refresh=%+v, error=%v, native reads=%d, want %d", observations, err, mock.resourceReads, test.expected)
			}
			after, err := canonicalValue(state)
			if err != nil || !bytes.Equal(before, after) || mock.head != head ||
				len(mock.logs) != logs || mock.refWrites != writes {
				t.Fatal("optional refresh mutated authoritative state or published a commit")
			}
		})
	}
}

func observationFrontierFixture(t *testing.T, operations, frontier int, foreign bool) ([]QueueCommit, WorkDefinition) {
	t.Helper()
	commits := testGenesis(t, func(policy *Policy) {
		policy.Limits.Operations, policy.Limits.GraphNodes = operations, 512
		pool := policy.Pools["default"]
		pool.AllowedRepositories = append(pool.AllowedRepositories, "foreign/repo")
		policy.Pools["default"] = pool
	})
	initial, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]WorkDefinition, 0, frontier+1)
	for index := range frontier {
		node, err := NewWork([]byte(`{"task":"gated"}`), "graph", fmt.Sprintf("gated-%d", index),
			"default", *initial.Policy, 1000)
		if err != nil {
			t.Fatal(err)
		}
		resource := Resource{
			Kind: "issue", Host: "github.com", Repository: testRepository,
			RepositoryID: "1", ResourceID: strconv.Itoa(index + 2), Number: "7",
		}
		if foreign {
			resource.Repository, resource.RepositoryID = "foreign/repo", "9"
		}
		node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
		nodes = append(nodes, node)
	}
	ready, err := NewWork([]byte(`{"task":"independent"}`), "graph", "independent",
		"default", *initial.Policy, 1000)
	if err != nil {
		t.Fatal(err)
	}
	nodes = append(nodes, ready)
	for start := 0; start < len(nodes); start += operations {
		end := min(len(nodes), start+operations)
		commits = testSubmit(t, commits, fmt.Sprintf("submit-%d", start), nodes[start:end]...)
	}
	return commits, ready
}

func TestNativeObservedDispatchValidatesBeforeDependencyProbes(t *testing.T) {
	for _, test := range []struct {
		name           string
		role           string
		invalidProfile bool
		change         func(*DispatchParameters)
		code           string
	}{
		{name: "zero-claims", change: func(p *DispatchParameters) { p.MaxClaims = 0 }, code: "request_invalid"},
		{name: "oversized-claims", change: func(p *DispatchParameters) { p.MaxClaims = 257 }, code: "request_invalid"},
		{name: "zero-dispatches", change: func(p *DispatchParameters) { p.MaxDispatches = 0 }, code: "request_invalid"},
		{name: "oversized-dispatches", change: func(p *DispatchParameters) { p.MaxDispatches = 257 }, code: "request_invalid"},
		{name: "zero-bytes", change: func(p *DispatchParameters) { p.MaxBytes = 0 }, code: "request_invalid"},
		{name: "oversized-bytes", change: func(p *DispatchParameters) { p.MaxBytes = 48<<10 + 1 }, code: "request_invalid"},
		{name: "uninstalled-pool", change: func(p *DispatchParameters) { p.Pool = "foreign" }, code: "pool_invalid"},
		{name: "producer-role", role: "producer", code: "actor_unauthorized"},
		{name: "uninstalled-work-profile", invalidProfile: true, code: "work_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits := testGenesis(t, nil)
			node := testNode(t, commits, "gated")
			resource := Resource{
				Kind: "issue", Host: "github.com", Repository: testRepository,
				RepositoryID: "1", ResourceID: "2", Number: "7",
			}
			node.DependsOn = []Dependency{{Kind: "issue", Resource: &resource, Condition: "completed"}}
			commits = testSubmit(t, commits, "submit", node)
			if test.invalidProfile {
				node.WorkerProfile = "uninstalled"
				request, err := NewRequest("submit", "submit", testActor("producer"), SubmitParameters{Nodes: []WorkDefinition{node}})
				if err != nil {
					t.Fatal(err)
				}
				commits[1].Request, commits[1].Operations = request, []Operation{mustOp(t, node)}
			}
			installMockLog(t, mock, commits)
			initialHead, initialLogs, initialCommits := mock.head, len(mock.logs), len(mock.commits)
			params := DispatchParameters{Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10}
			if test.change != nil {
				test.change(&params)
			}
			role := test.role
			if role == "" {
				role = "administrator"
			}
			actor := testActor(role)
			request, err := NewRequest("invalid-before-read", "dispatch_next", actor, params)
			if err != nil {
				t.Fatal(err)
			}
			publication, err := branch.Publish(context.Background(), actor, request)
			if err == nil || !strings.HasPrefix(err.Error(), test.code+":") ||
				publication.Commit != nil || publication.Changed {
				t.Fatalf("invalid observed request did not fail before evaluation: %+v %v", publication, err)
			}
			if mock.resourceReads != 0 || mock.refWrites != 0 || mock.head != initialHead ||
				len(mock.logs) != initialLogs || len(mock.commits) != initialCommits {
				t.Fatal("invalid scope/profile/budget probed dependencies or changed physical authority")
			}
		})
	}
}

func TestNativeObservationRefreshLeavesIndependentClaimBudget(t *testing.T) {
	for _, test := range []struct {
		name           string
		operations     int
		frontier       int
		foreign        bool
		expectedProofs int
	}{
		{name: "large-unknown-frontier", operations: 256, frontier: 130, expectedProofs: 128},
		{name: "large-missing-read-scope", operations: 256, frontier: 130, foreign: true, expectedProofs: 128},
		{name: "small-operation-budget", operations: 3, frontier: 4, foreign: true, expectedProofs: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			branch, mock := newQueueAPI(t)
			commits, ready := observationFrontierFixture(t, test.operations, test.frontier, test.foreign)
			installMockLog(t, mock, commits)
			mock.issueStatus = 403
			request, err := NewRequest("independent-grant", "dispatch_next", testActor("administrator"), DispatchParameters{
				Pool: "default", MaxClaims: 1, MaxDispatches: 1, MaxBytes: 48 << 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			publication, err := branch.Publish(context.Background(), testActor("administrator"), request)
			if err != nil {
				t.Fatal(err)
			}
			if !publication.Changed || publication.Commit == nil ||
				len(publication.Commit.Operations) != test.expectedProofs+1 ||
				len(publication.Decision.Assignments) != 1 ||
				publication.Decision.Assignments[0].Claims[0].WorkID != ready.WorkID {
				t.Fatalf("unknown dependency frontier denied independent fair Work: %+v", publication)
			}
			latest, err := branch.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			state, err := Replay(latest)
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Observations) != test.expectedProofs || state.Stats.Claimed != 1 {
				t.Fatal("observation preface exceeded its bound or granted a gated node")
			}
			for _, observation := range state.Observations {
				if observation.State != "unknown" || observation.ObservedAt > publication.Commit.At {
					t.Fatal("missing proof fabricated readiness or used a pre-read decision clock")
				}
				if test.foreign && observation.ReadStatus != "credentials_missing" {
					t.Fatal("foreign resource read proceeded without separately bound credentials")
				}
			}
			expectedReads := test.expectedProofs
			if test.foreign {
				expectedReads = 0
			}
			if mock.resourceReads != expectedReads {
				t.Fatalf("typed resource reads exceeded the allowed scope/budget: %d", mock.resourceReads)
			}
		})
	}
}
