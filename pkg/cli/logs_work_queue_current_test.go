//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func currentWorkQueueTestDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestWorkQueueSnapshotEqualityPreservesValuesAndNilShape(t *testing.T) {
	sha, claim, attempt, enqueued := "snapshot-sha", "historical-claim", "historical-attempt", int64(7)
	seed := WorkQueueSnapshot{
		Version: 3, SHA: &sha, Worker: &WorkQueueWorker{WorkID: "work", ClaimID: "claim"},
		Transactions: []WorkQueueTransaction{{Version: 2, Kind: "Completion", Work: "work", Claim: &claim, Attempt: &attempt, Enqueued: &enqueued}},
		Current: &WorkQueueCurrentSnapshot{Role: "worker", PolicyInstalled: true, Tip: "tip", PolicyEpoch: "epoch",
			Assignment: &WorkQueueAssignmentReceipt{DispatchID: "dispatch", RequestID: "request", CommitID: "commit",
				Claims: []WorkQueueClaimReceipt{{Handle: "handle", ClaimID: "claim", WorkID: "work", State: "completed", Barrier: "pending"}}}},
	}
	encoded, err := json.Marshal(seed)
	require.NoError(t, err)
	for name, change := range map[string]func(*WorkQueueSnapshot){
		"version":       func(s *WorkQueueSnapshot) { s.Version++ },
		"sha":           func(s *WorkQueueSnapshot) { *s.SHA = "other" },
		"worker":        func(s *WorkQueueSnapshot) { s.Worker.ClaimID = "other" },
		"transaction":   func(s *WorkQueueSnapshot) { s.Transactions[0].Kind = "Claim" },
		"claim":         func(s *WorkQueueSnapshot) { *s.Transactions[0].Claim = "other" },
		"attempt":       func(s *WorkQueueSnapshot) { *s.Transactions[0].Attempt = "other" },
		"enqueue":       func(s *WorkQueueSnapshot) { *s.Transactions[0].Enqueued++ },
		"origin":        func(s *WorkQueueSnapshot) { s.Current.Role = "observer" },
		"policy":        func(s *WorkQueueSnapshot) { s.Current.PolicyInstalled = false },
		"tip":           func(s *WorkQueueSnapshot) { s.Current.Tip = "other" },
		"epoch":         func(s *WorkQueueSnapshot) { s.Current.PolicyEpoch = "other" },
		"clock":         func(s *WorkQueueSnapshot) { s.Current.CapturedAt++ },
		"count":         func(s *WorkQueueSnapshot) { s.Current.CommitCount++ },
		"admission":     func(s *WorkQueueSnapshot) { s.Current.Paused = true },
		"grants":        func(s *WorkQueueSnapshot) { s.Current.GrantsPaused = true },
		"dispatch":      func(s *WorkQueueSnapshot) { s.Current.Assignment.DispatchID = "other" },
		"request":       func(s *WorkQueueSnapshot) { s.Current.Assignment.RequestID = "other" },
		"commit":        func(s *WorkQueueSnapshot) { s.Current.Assignment.CommitID = "other" },
		"native":        func(s *WorkQueueSnapshot) { s.Current.Assignment.State = "other" },
		"released":      func(s *WorkQueueSnapshot) { s.Current.Assignment.Released = true },
		"claim receipt": func(s *WorkQueueSnapshot) { s.Current.Assignment.Claims[0].Barrier = "verified" },
	} {
		t.Run(name, func(t *testing.T) {
			var copy WorkQueueSnapshot
			require.NoError(t, json.Unmarshal(encoded, &copy))
			require.True(t, equalWorkQueueSnapshots(&seed, &copy), "pointer identity is not value identity")
			change(&copy)
			require.False(t, equalWorkQueueSnapshots(&seed, &copy))
			report := &WorkQueueReport{Snapshot: &seed}
			require.True(t, mergeWorkQueueReport(&report, &WorkQueueReport{Snapshot: &copy}))
			require.False(t, mergeWorkQueueReport(&report, &WorkQueueReport{Snapshot: &copy}))
		})
	}
	require.False(t, equalWorkQueueSnapshots(nil, &seed))
	require.False(t, equalWorkQueueSnapshots(&WorkQueueSnapshot{}, &WorkQueueSnapshot{Transactions: []WorkQueueTransaction{}}))
	require.False(t, equalCurrentWorkQueueSnapshots(nil, seed.Current))
	require.False(t, equalCurrentWorkQueueSnapshots(&WorkQueueCurrentSnapshot{Assignment: &WorkQueueAssignmentReceipt{}},
		&WorkQueueCurrentSnapshot{Assignment: &WorkQueueAssignmentReceipt{Claims: []WorkQueueClaimReceipt{}}}))
}

func currentWorkQueueFixture(t *testing.T) map[string]any {
	t.Helper()
	policy := workqueue.DefaultPolicy("42", "owner/repo")
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.MaxClaims = 2
	profile.Ref = strings.Repeat("a", 40)
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	admin := workqueue.Actor{Role: "administrator", Principal: "42", Repository: "owner/repo"}
	dispatcher := workqueue.Actor{Role: "dispatcher", Principal: "42", Repository: "owner/repo",
		Workflow: ".github/workflows/dispatcher.lock.yml", RunID: "10", RunAttempt: 1}
	commits := []workqueue.QueueCommit{}
	appendCommit := func(actor workqueue.Actor, kind string, params any, operations []workqueue.Operation) {
		id := fmt.Sprintf("commit-%d", len(commits)+1)
		request, err := workqueue.NewRequest("request-"+id, kind, actor, params)
		require.NoError(t, err)
		commit := workqueue.QueueCommit{Version: 3, ID: id, Actor: actor, Request: request,
			PolicyEpoch: "epoch-1", At: int64(len(commits) + 1), Operations: operations}
		if len(commits) > 0 {
			previous := commits[len(commits)-1].ID
			commit.Previous = &previous
		}
		commits = append(commits, commit)
	}
	ops := []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{"kind": "Policy", "epoch": "epoch-1", "policy": policy})}
	appendCommit(admin, "policy", workqueue.OperationsParameters{Operations: ops}, ops)
	nodes := []workqueue.WorkDefinition{}
	for _, key := range []string{"a", "b"} {
		node, err := workqueue.NewWork([]byte(`{"plan":"private-task-content"}`), "graph", key, "default", policy, 2)
		require.NoError(t, err)
		nodes = append(nodes, node)
	}
	ops = []workqueue.Operation{mustWorkQueueOperation(t, nodes[0]), mustWorkQueueOperation(t, nodes[1])}
	appendCommit(admin, "submit", workqueue.SubmitParameters{Nodes: nodes}, ops)
	state, err := workqueue.Replay(commits)
	require.NoError(t, err)
	parameters := workqueue.DispatchParameters{Pool: "default", MaxClaims: 2, MaxDispatches: 1, MaxBytes: 48 << 10}
	decision, err := workqueue.PlanDispatch(state, parameters, "request-commit-3", "commit-3", 3)
	require.NoError(t, err)
	require.Len(t, decision.Assignments, 1)
	require.Len(t, decision.Assignments[0].Claims, 2)
	appendCommit(dispatcher, "dispatch_next", parameters, decision.Operations)
	assignment := decision.Assignments[0]
	ops = []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "started", "sender": dispatcher})}
	appendCommit(dispatcher, "dispatch", workqueue.OperationsParameters{Operations: ops}, ops)
	worker := workqueue.Actor{Role: "worker", Principal: "42", Repository: "owner/repo",
		Workflow: profile.Workflow, RunID: "20", RunAttempt: 1, DispatchID: assignment.DispatchID}
	run := workqueue.RunBinding{RunID: "20", RunAttempt: 1, Repository: "owner/repo",
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: "42", Event: "workflow_dispatch"}
	evidence := workqueue.Evidence{Kind: "reconciliation", Source: "trusted_activation", Repository: "owner/repo",
		Workflow: profile.Workflow, Ref: profile.Ref, Principal: "42", RunID: "20", RunAttempt: 1, CheckedAt: 5}
	ops = []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{"kind": "Dispatch", "dispatch_id": assignment.DispatchID, "state": "bound", "run": run, "evidence": evidence})}
	appendCommit(worker, "dispatch", workqueue.OperationsParameters{Operations: ops}, ops)
	first, second := assignment.Claims[0], assignment.Claims[1]
	worker.ClaimHandle = first.Handle
	ops = []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{"kind": "Completion", "work_id": first.WorkID,
		"claim_id": first.ClaimID, "dispatch_id": assignment.DispatchID, "claim_handle": first.Handle, "run_id": "20", "run_attempt": 1})}
	appendCommit(worker, "finish", workqueue.FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: first.Handle, Outcome: "completed"}, ops)
	worker.ClaimHandle = second.Handle
	ops = []workqueue.Operation{mustWorkQueueOperation(t, map[string]any{"kind": "ClaimCancellation", "work_id": second.WorkID,
		"claim_id": second.ClaimID, "reason": "worker_cancelled", "retry_not_before": 30007})}
	appendCommit(worker, "finish", workqueue.FinishParameters{DispatchID: assignment.DispatchID, ClaimHandle: second.Handle, Outcome: "cancelled"}, ops)
	worker.ClaimHandle = ""
	ledger, err := workqueue.Serialize(commits)
	require.NoError(t, err)
	return map[string]any{"version": 3, "sha": "snapshot-sha", "worker": assignment,
		"transactionLog": string(ledger), "captured_at": 8, "origin": worker}
}

func TestWorkQueueCurrentReport(t *testing.T) {
	t.Parallel()
	fixture := currentWorkQueueFixture(t)
	data, err := json.Marshal(fixture)
	require.NoError(t, err)
	dir := currentWorkQueueTestDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), data, 0o600))
	intents := `{"version":3,"intent_id":"i1","kind":"finish","claim_handle":"h1","parameters":{"outcome":"completed"}}
{"version":3,"intent_id":"i2","kind":"finish","claim_handle":"h2","parameters":{"outcome":"cancelled"}}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueFinishFile), []byte(intents), 0o600))
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report.Snapshot.Current)
	current := report.Snapshot.Current
	assert.Equal(t, 7, current.CommitCount)
	assert.Equal(t, "commit-7", current.Tip)
	assert.Equal(t, "snapshot-sha", *report.Snapshot.SHA, "Git HEAD is distinct from the causal QueueCommit identity")
	require.Len(t, current.Assignment.Claims, 2)
	assert.Equal(t, "completed", current.Assignment.Claims[0].State)
	assert.Equal(t, "pending", current.Assignment.Claims[0].Barrier)
	assert.Equal(t, "cancelled", current.Assignment.Claims[1].State)
	require.Len(t, report.FinishIntents, 2)
	assert.Empty(t, report.FinishIntent)
	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, report))
	assert.Contains(t, output.String(), "captured ledger, not live authority")
	assert.Contains(t, output.String(), "durable=completed delivery=pending")
	assert.Contains(t, output.String(), "durable=cancelled")
	assert.Contains(t, output.String(), "staged, not durable or verified")
	serialized, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "private-task-content")
	assert.NotContains(t, string(serialized), "transactionLog")
	assert.NotContains(t, string(serialized), "principal")
}

func TestWorkQueueCurrentSnapshotRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"fork", "truncated", "duplicate-key", "wrong-worker", "foreign-origin", "rerun", "unknown-origin-field", "missing-policy"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := currentWorkQueueFixture(t)
			switch scenario {
			case "fork":
				lines := strings.Split(strings.TrimSuffix(fixture["transactionLog"].(string), "\n"), "\n")
				var commit map[string]any
				require.NoError(t, json.Unmarshal([]byte(lines[6]), &commit))
				commit["previous"] = "commit-5"
				data, err := json.Marshal(commit)
				require.NoError(t, err)
				lines[6] = string(data)
				fixture["transactionLog"] = strings.Join(lines, "\n") + "\n"
			case "truncated":
				fixture["transactionLog"] = strings.TrimSuffix(fixture["transactionLog"].(string), "\n")
			case "duplicate-key":
				data, err := json.Marshal(fixture)
				require.NoError(t, err)
				data = append([]byte(`{"version":3,`), data[1:]...)
				_, err = parseWorkQueueSnapshot(data)
				require.Error(t, err)
				return
			case "wrong-worker":
				assignment := fixture["worker"].(workqueue.Assignment)
				assignment.Claims[0].Work = json.RawMessage(`{"payload":"forged"}`)
				fixture["worker"] = assignment
			case "foreign-origin", "rerun":
				origin := fixture["origin"].(workqueue.Actor)
				if scenario == "rerun" {
					origin.RunAttempt = 2
				} else {
					origin.RunID = "99"
				}
				fixture["origin"] = origin
			case "unknown-origin-field":
				fixture["origin"] = map[string]any{"role": "worker", "secret": "not-exportable"}
			case "missing-policy":
				fixture["transactionLog"] = ""
			}
			data, err := json.Marshal(fixture)
			require.NoError(t, err)
			_, err = parseWorkQueueSnapshot(data)
			require.Error(t, err, scenario)
		})
	}
}

func TestWorkQueueCurrentUnassignedSnapshotProvenance(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name    string
		role    string
		attempt int
		runID   string
		valid   bool
	}{
		{"original-dispatcher", "dispatcher", 1, "10", true},
		{"producer", "producer", 1, "10", false},
		{"administrator", "administrator", 1, "10", false},
		{"worker-without-assignment", "worker", 1, "10", false},
		{"dispatcher-rerun", "dispatcher", 2, "10", true},
		{"dispatcher-maximum-attempt", "dispatcher", 4096, "10", true},
		{"dispatcher-zero-attempt", "dispatcher", 0, "10", false},
		{"dispatcher-over-bound-attempt", "dispatcher", 4097, "10", false},
		{"missing-native-run", "dispatcher", 1, "", false},
		{"missing-workflow", "dispatcher", 1, "10", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := currentWorkQueueFixture(t)
			fixture["worker"] = nil
			origin := workqueue.Actor{
				Role: scenario.role, Principal: "42", Repository: "owner/repo",
				Workflow: ".github/workflows/dispatcher.lock.yml",
				RunID:    scenario.runID, RunAttempt: scenario.attempt,
			}
			if scenario.name == "missing-workflow" {
				origin.Workflow = ""
			}
			fixture["origin"] = origin
			data, err := json.Marshal(fixture)
			require.NoError(t, err)
			snapshot, err := parseWorkQueueSnapshot(data)
			if !scenario.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, snapshot.Current)
			assert.Nil(t, snapshot.Current.Assignment)
		})
	}
}

func TestWorkQueueCurrentFinishIntents(t *testing.T) {
	t.Parallel()
	snapshot := &WorkQueueCurrentSnapshot{Assignment: &WorkQueueAssignmentReceipt{Claims: []WorkQueueClaimReceipt{{Handle: "h1"}, {Handle: "h2"}}}}
	valid := `{"version":3,"intent_id":"i","kind":"finish","claim_handle":"h1","parameters":{"outcome":"completed"}}`
	for _, data := range []string{
		`{"outcome":"completed"}`, `null`, strings.Replace(valid, `"h1"`, `"foreign"`, 1),
		strings.Replace(valid, `"completed"`, `"invalid"`, 1),
		strings.Replace(valid, `"parameters":{"outcome":"completed"}`, `"parameters":{"outcome":"completed","secret":"x"}`, 1),
		valid + "\n" + strings.Replace(valid, `"completed"`, `"cancelled"`, 1),
		valid + "\n" + strings.Replace(strings.Replace(valid, `"i"`, `"j"`, 1), `"completed"`, `"cancelled"`, 1),
		strings.Repeat(valid+"\n", 257),
	} {
		dir := currentWorkQueueTestDir(t)
		path := filepath.Join(dir, workQueueFinishFile)
		require.NoError(t, os.WriteFile(path, []byte(data+"\n"), 0o600))
		_, err := readCurrentWorkQueueFinishIntents(path, snapshot)
		require.Error(t, err, data[:min(len(data), 256)])
	}
	dir := currentWorkQueueTestDir(t)
	path := filepath.Join(dir, workQueueFinishFile)
	require.NoError(t, os.WriteFile(path, []byte(valid+"\n"+valid+"\n"), 0o600))
	receipts, err := readCurrentWorkQueueFinishIntents(path, snapshot)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
}

func TestWorkQueueCurrentSnapshotRoles(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name       string
		role       any
		assigned   bool
		absent     bool
		originRole string
		valid      bool
	}{
		{"worker", "worker", true, false, "worker", true},
		{"dispatcher", "dispatcher", false, false, "dispatcher", true},
		{"observer", "observer", false, false, "producer", true},
		{"absent-observer", "observer", false, true, "producer", true},
		{"unassigned-worker", "worker", false, false, "worker", false},
		{"assigned-observer", "observer", true, false, "worker", false},
		{"assigned-dispatcher", "dispatcher", true, false, "worker", false},
		{"dispatcher-origin-observer", "observer", false, false, "dispatcher", false},
		{"worker-origin-observer", "observer", false, false, "worker", false},
		{"absent-dispatcher", "dispatcher", false, true, "dispatcher", false},
		{"absent-worker", "worker", true, true, "worker", false},
		{"null-role", nil, false, false, "producer", false},
		{"empty-role", "", false, false, "producer", false},
		{"unknown-role", "private-token-value", false, false, "producer", false},
		{"administrator-role", "administrator", false, false, "administrator", false},
		{"producer-role", "producer", false, false, "producer", false},
		{"reconciler-role", "reconciler", false, false, "reconciler", false},
		{"numeric-role", 3, false, false, "producer", false},
		{"object-role", map[string]any{}, false, false, "producer", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := currentWorkQueueFixture(t)
			fixture["role"] = scenario.role
			origin := fixture["origin"].(workqueue.Actor)
			origin.Role = scenario.originRole
			if !scenario.assigned {
				fixture["worker"] = nil
				origin.DispatchID, origin.ClaimHandle = "", ""
			}
			fixture["origin"] = origin
			if scenario.absent {
				fixture["sha"], fixture["transactionLog"] = nil, ""
			}
			data, err := json.Marshal(fixture)
			require.NoError(t, err)
			snapshot, err := parseWorkQueueSnapshot(data)
			if !scenario.valid {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-token-value")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, scenario.role, snapshot.Current.Role)
			assert.Equal(t, !scenario.absent, snapshot.Current.PolicyInstalled)
			if scenario.assigned {
				require.NotNil(t, snapshot.Current.Assignment)
			} else {
				assert.Nil(t, snapshot.Current.Assignment)
			}
			if scenario.role == "observer" {
				var output bytes.Buffer
				require.NoError(t, renderWorkQueueToWriter(&output, &WorkQueueReport{Snapshot: snapshot}))
				assert.Contains(t, output.String(), "observer (read-only diagnostics; no worker or publisher authority)")
				assert.NotContains(t, output.String(), "durable=")
				if scenario.absent {
					assert.Contains(t, output.String(), "policy: absent (no queue initialized)")
				}
			}
		})
	}
}

func TestWorkQueueCurrentSnapshotVisibilityShape(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"worker", "dispatcher", "observer", "absent-observer"} {
		for _, scenario := range []struct {
			name  string
			value any
			valid bool
		}{
			{"omitted", nil, true},
			{"empty", []string{}, true},
			{"strings", []string{"work-a", "work-b"}, true},
			{"presentation-only", []string{"", "unknown-work", "unknown-work", "unicode-\u00e9"}, true},
			{"null", nil, false},
			{"null-entry", []any{nil}, false},
			{"mixed-null-entry", []any{"work-a", nil}, false},
			{"number-entry", []any{1}, false},
			{"boolean-entry", []any{false}, false},
			{"object-entry", []any{map[string]any{}}, false},
			{"nested-array-entry", []any{[]string{"work-a"}}, false},
			{"string", "private-token-value", false},
			{"number", 1, false},
			{"object", map[string]any{"private-token-value": true}, false},
		} {
			t.Run(role+"/"+scenario.name, func(t *testing.T) {
				fixture := currentWorkQueueFixture(t)
				fixture["role"] = role
				if role != "worker" {
					fixture["worker"] = nil
					origin := fixture["origin"].(workqueue.Actor)
					origin.Role, origin.DispatchID, origin.ClaimHandle = "dispatcher", "", ""
					if role == "observer" || role == "absent-observer" {
						fixture["role"], origin.Role = "observer", "producer"
					}
					fixture["origin"] = origin
				}
				if role == "absent-observer" {
					fixture["sha"], fixture["transactionLog"] = nil, ""
				}
				if scenario.name != "omitted" {
					fixture["visible_work_ids"] = scenario.value
				}
				data, err := json.Marshal(fixture)
				require.NoError(t, err)
				snapshot, err := parseWorkQueueSnapshot(data)
				if !scenario.valid {
					require.Error(t, err)
					assert.NotContains(t, err.Error(), "private-token-value")
					return
				}
				require.NoError(t, err)
				assert.Equal(t, role == "worker", snapshot.Current.Assignment != nil)
				assert.Equal(t, role != "absent-observer", snapshot.Current.PolicyInstalled)
			})
		}
	}
}

func TestWorkQueueCurrentObserverProvenanceAndFinish(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"foreign-repository", "empty-principal", "login-principal", "zero-principal", "leading-zero-principal", "fraction-principal", "exponent-principal", "malformed-run", "empty-workflow", "zero-attempt", "over-bound-attempt", "claim-scope", "existing-policyless-queue", "staged-finish"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := currentWorkQueueFixture(t)
			fixture["role"], fixture["worker"] = "observer", nil
			origin := fixture["origin"].(workqueue.Actor)
			origin.Role, origin.DispatchID, origin.ClaimHandle = "producer", "", ""
			switch scenario {
			case "foreign-repository":
				origin.Repository = "foreign/repo"
			case "empty-principal":
				origin.Principal = ""
			case "login-principal":
				origin.Principal = "operator"
			case "zero-principal":
				origin.Principal = "0"
			case "leading-zero-principal":
				origin.Principal = "001"
			case "fraction-principal":
				origin.Principal = "1.0"
			case "exponent-principal":
				origin.Principal = "1e3"
			case "malformed-run":
				origin.RunID = "00020"
			case "empty-workflow":
				origin.Workflow = ""
			case "zero-attempt":
				origin.RunAttempt = 0
			case "over-bound-attempt":
				origin.RunAttempt = 4097
			case "claim-scope":
				origin.ClaimHandle = "h1"
			case "existing-policyless-queue":
				fixture["transactionLog"] = ""
			}
			fixture["origin"] = origin
			data, err := json.Marshal(fixture)
			require.NoError(t, err)
			snapshot, err := parseWorkQueueSnapshot(data)
			if scenario != "staged-finish" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			dir := currentWorkQueueTestDir(t)
			path := filepath.Join(dir, workQueueFinishFile)
			require.NoError(t, os.WriteFile(path, []byte(`{"version":3,"intent_id":"i","kind":"finish","claim_handle":"h1","parameters":{"outcome":"completed"}}`), 0o600))
			_, err = readCurrentWorkQueueFinishIntents(path, snapshot.Current)
			require.ErrorContains(t, err, "foreign to original immutable assignment")
		})
	}
}

func TestWorkQueueCurrentObserverNativeAttempts(t *testing.T) {
	t.Parallel()
	for _, absent := range []bool{false, true} {
		for _, scenario := range []struct {
			name     string
			explicit bool
			attempt  int
			valid    bool
		}{
			{"original", true, 1, true},
			{"rerun", true, 2, true},
			{"maximum", true, 4096, true},
			{"zero", true, 0, false},
			{"over-bound", true, 4097, false},
			{"implicit-observer", false, 1, false},
		} {
			t.Run(fmt.Sprintf("absent-%t/%s", absent, scenario.name), func(t *testing.T) {
				fixture := currentWorkQueueFixture(t)
				fixture["worker"] = nil
				if scenario.explicit {
					fixture["role"] = "observer"
				} else {
					delete(fixture, "role")
				}
				origin := fixture["origin"].(workqueue.Actor)
				origin.Role, origin.DispatchID, origin.ClaimHandle = "producer", "", ""
				origin.RunAttempt = scenario.attempt
				fixture["origin"] = origin
				if absent {
					fixture["sha"], fixture["transactionLog"] = nil, ""
				}
				data, err := json.Marshal(fixture)
				require.NoError(t, err)
				snapshot, err := parseWorkQueueSnapshot(data)
				if !scenario.valid {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, "observer", snapshot.Current.Role)
				assert.Equal(t, !absent, snapshot.Current.PolicyInstalled)
				assert.Nil(t, snapshot.Current.Assignment)
			})
		}
	}
}

func TestWorkQueueCurrentOriginExcludesRuntimeContext(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"worker", "dispatcher", "observer"} {
		for _, field := range []string{"authenticated", "native_run"} {
			t.Run(role+"/"+field, func(t *testing.T) {
				fixture := currentWorkQueueFixture(t)
				fixture["role"] = role
				origin := fixture["origin"].(workqueue.Actor)
				if role != "worker" {
					fixture["worker"] = nil
					origin.DispatchID, origin.ClaimHandle = "", ""
					origin.Role = "dispatcher"
					if role == "observer" {
						origin.Role = "producer"
					}
				}
				data, err := json.Marshal(origin)
				require.NoError(t, err)
				var fields map[string]any
				require.NoError(t, json.Unmarshal(data, &fields))
				fields[field] = true
				if field == "native_run" {
					fields[field] = map[string]any{"credential": "private-token-value"}
				}
				fixture["origin"] = fields
				data, err = json.Marshal(fixture)
				require.NoError(t, err)
				_, err = parseWorkQueueSnapshot(data)
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-token-value")
			})
		}
	}
}

func TestWorkQueueCurrentNativeIdentifierBoundaries(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"principal", "run"} {
		for _, scenario := range []struct {
			name  string
			value string
			valid bool
		}{
			{"minimum", "1", true},
			{"maximum", strings.Repeat("9", 256), true},
			{"over-limit", strings.Repeat("9", 257), false},
			{"non-ascii", "\uff11", false},
		} {
			t.Run(field+"/"+scenario.name, func(t *testing.T) {
				fixture := currentWorkQueueFixture(t)
				fixture["role"], fixture["worker"] = "observer", nil
				origin := fixture["origin"].(workqueue.Actor)
				origin.Role, origin.DispatchID, origin.ClaimHandle = "producer", "", ""
				if field == "principal" {
					origin.Principal = scenario.value
				} else {
					origin.RunID = scenario.value
				}
				fixture["origin"] = origin
				data, err := json.Marshal(fixture)
				require.NoError(t, err)
				_, err = parseWorkQueueSnapshot(data)
				if scenario.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestWorkQueueCurrentSnapshotOuterBound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(currentWorkQueueTestDir(t), workQueueSnapshotFile)
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	require.NoError(t, os.Truncate(path, (162<<20)+1))
	_, err := extractWorkQueueReport(filepath.Dir(path))
	require.ErrorContains(t, err, "bounded input limit")
}

func TestWorkQueueCurrentSingletonFinishSelector(t *testing.T) {
	t.Parallel()
	implicit := `{"version":3,"intent_id":"singleton","kind":"finish","parameters":{"outcome":"completed"}}`
	singleton := &WorkQueueCurrentSnapshot{Assignment: &WorkQueueAssignmentReceipt{Claims: []WorkQueueClaimReceipt{{Handle: "h1"}}}}
	mixed := &WorkQueueCurrentSnapshot{Assignment: &WorkQueueAssignmentReceipt{Claims: []WorkQueueClaimReceipt{
		{Handle: "h1", State: "completed"}, {Handle: "h2", State: "open"},
	}}}
	for _, scenario := range []struct {
		name     string
		input    string
		snapshot *WorkQueueCurrentSnapshot
		valid    bool
	}{
		{"original-singleton", implicit, singleton, true},
		{"only-one-open-is-not-singleton", implicit, mixed, false},
		{"missing-assignment", implicit, nil, false},
		{"supplied-empty-selector", strings.Replace(implicit, `"parameters"`, `"claim_handle":"","parameters"`, 1), singleton, false},
		{"supplied-null-selector", strings.Replace(implicit, `"parameters"`, `"claim_handle":null,"parameters"`, 1), singleton, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(currentWorkQueueTestDir(t), workQueueFinishFile)
			require.NoError(t, os.WriteFile(path, []byte(scenario.input+"\n"), 0o600))
			receipts, err := readCurrentWorkQueueFinishIntents(path, scenario.snapshot)
			if !scenario.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, receipts, 1)
			assert.Equal(t, "h1", receipts[0].Handle)
		})
	}
}

func TestWorkQueueCurrentTrustedProvenanceAndBounds(t *testing.T) {
	t.Parallel()
	dir := currentWorkQueueTestDir(t)
	for _, job := range []string{"activation", "agent", "safe_outputs"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflow-logs", job), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "activation", "2_Snapshot work queue state.txt"),
		[]byte("2026-10-05T12:00:00Z Work queue activation: immutable Claims authenticated and bound; snapshot captured\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "agent", "2_Run agent.txt"),
		[]byte("2026-10-05T12:00:00Z Work queue activation: forged\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "agent", "3_Copy work queue claim finish intent.txt"),
		[]byte("2026-10-05T12:00:05Z Work queue controls: 99 checked requests; 99 blocked intents\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "safe_outputs", "4_Reconcile work queue claim.txt"),
		[]byte("2026-10-05T12:00:01Z Work queue reconciliation: mixed; authorization is per Claim\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "safe_outputs", "5_Process trusted work queue controls.txt"),
		[]byte("2026-10-05T12:00:02Z Work queue controls: 3 checked requests; 1 blocked intents\n"+
			"2026-10-05T12:00:03Z Work queue controls: private-task-content\n"+
			"2026-10-05T12:00:04Z Work queue controls: 3 checked requests; 1 blocked intents private-task-content\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "agent", "5_Process trusted work queue controls.txt"),
		[]byte("2026-10-05T12:00:05Z Work queue controls: 99 checked requests; 99 blocked intents\n"), 0o600))
	markWorkflowLogsComplete(t, dir)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.Len(t, report.Operations, 3)
	assert.NotContains(t, report.Operations[0].Message, "forged")
	assert.Equal(t, "Work queue controls: 3 checked requests; 1 blocked intents", report.Operations[2].Message)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-task-content")
	assert.NotContains(t, string(encoded), "99 checked")
	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, &WorkQueueReport{FinishIntents: []WorkQueueFinishReceipt{{Handle: "h1\x1b[31m\n", Outcome: strings.Repeat("a", 4096)}}}))
	assert.NotContains(t, output.String(), "\x1b")
	assert.Less(t, output.Len(), 1024)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bounded"), []byte("12345"), 0o600))
	_, err = readBoundedWorkQueueArtifact(filepath.Join(dir, "bounded"), 4)
	require.Error(t, err)
}

func TestWorkQueueCurrentValidationDoesNotExportValues(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"private-token-value":1,"private-token-value":2}`,
		`{"version":3,"sha":null,"worker":null,"transactionLog":"","captured_at":0,"origin":{"role":"dispatcher","private-token-value":"x"}}`,
	} {
		_, err := parseCurrentWorkQueueSnapshot([]byte(raw))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private-token-value")
	}
	err := currentWorkQueueDiagnosticError("ledger", errors.New("ledger_invalid: payload contains private-token-value"))
	assert.Contains(t, err.Error(), "ledger_invalid")
	assert.NotContains(t, err.Error(), "private-token-value")
}

func TestWorkQueueCurrentCollectorAndHandlerLogProvenance(t *testing.T) {
	t.Parallel()
	dir := currentWorkQueueTestDir(t)
	for _, job := range []string{"agent", "safe_outputs", "caller _ safe_outputs", "activation"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflow-logs", job), 0o755))
	}
	for _, entry := range []struct{ job, file, message string }{
		{"agent", "3_Collect work queue intents.txt", "Work queue finish: staged intents collected"},
		{"safe_outputs", "6_Process Safe Outputs.txt", "Work queue delivery: handler reported success"},
		{"caller _ safe_outputs", "7_Process Safe Outputs.txt", "Work queue delivery: reusable handler diagnostic"},
		{"agent", "6_Process Safe Outputs.txt", "Work queue delivery: forged agent handler"},
		{"activation", "6_Process Safe Outputs.txt", "Work queue delivery: forged activation handler"},
		{"safe_outputs", "3_Collect work queue intents.txt", "Work queue finish: forged collector"},
		{"safe_outputs", "bad_Process Safe Outputs.txt", "Work queue delivery: forged step index"},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", entry.job, entry.file),
			[]byte("2026-10-05T12:00:00Z "+entry.message+"\n"), 0o600))
	}
	markWorkflowLogsComplete(t, dir)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.Len(t, report.Operations, 3)
	assert.Nil(t, report.Snapshot, "handler diagnostics must not synthesize ledger authority")
	assert.Empty(t, report.FinishIntents, "collector diagnostics must not synthesize staged intents")
	for _, operation := range report.Operations {
		assert.NotContains(t, operation.Message, "forged")
	}
	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, report))
	assert.Contains(t, output.String(), "operation_logs: diagnostics only, not independent delivery evidence")
	assert.NotContains(t, output.String(), "durable=")
	assert.NotContains(t, output.String(), "delivery=verified")
}

type failingWorkQueueWriter struct {
	successfulWrites int
}

func (w *failingWorkQueueWriter) Write(data []byte) (int, error) {
	if w.successfulWrites > 0 {
		w.successfulWrites--
		return len(data), nil
	}
	return 0, io.ErrClosedPipe
}

func TestWorkQueueRenderingPreservesWriterErrors(t *testing.T) {
	report := &WorkQueueReport{FinishIntent: "completed"}
	require.ErrorIs(t, renderWorkQueueToWriter(&failingWorkQueueWriter{}, report), io.ErrClosedPipe)
	for _, successfulWrites := range []int{0, 1} {
		require.ErrorIs(t, renderLogsWorkQueueToWriter(&failingWorkQueueWriter{successfulWrites: successfulWrites}, []RunData{{WorkQueue: report}}), io.ErrClosedPipe)
	}
	require.NoError(t, renderWorkQueueToWriter(&failingWorkQueueWriter{}, nil))
}

func TestWorkQueueCurrentVerifiedAndFailedDelivery(t *testing.T) {
	t.Parallel()
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed-%t", failed), func(t *testing.T) {
			fixture := currentWorkQueueFixture(t)
			commits, err := workqueue.Parse([]byte(fixture["transactionLog"].(string)))
			require.NoError(t, err)
			member := fixture["worker"].(workqueue.Assignment).Claims[0]
			evidence := workqueue.Evidence{Kind: "delivery", Source: "verified_receipts",
				Repository: "owner/repo", Workflow: ".github/workflows/worker.lock.yml",
				Ref: strings.Repeat("a", 40), Principal: "42", RunID: "20", RunAttempt: 1,
				CheckedAt: 8, Receipt: "private-verifier-receipt"}
			kind, operation := "result", map[string]any{
				"kind": "Result", "work_id": member.WorkID, "claim_id": member.ClaimID,
				"completion_id": "commit-6", "descriptor": map[string]any{"contract": "no_write"}, "evidence": evidence,
			}

			expected := "verified"
			if failed {
				kind, expected = "delivery_failure", "failed"
				evidence.Kind, evidence.Source = "terminal_run", "github_api"
				evidence.Status, evidence.Conclusion, evidence.Attempts, evidence.Effects = "completed", "failure", 5, "unknown"
				operation = map[string]any{"kind": "DeliveryFailure", "work_id": member.WorkID,
					"claim_id": member.ClaimID, "completion_id": "commit-6", "reason": "verification_exhausted",
					"disposition": "unknown", "evidence": evidence}
			}
			ops := []workqueue.Operation{mustWorkQueueOperation(t, operation)}
			actor := workqueue.Actor{Role: "reconciler", Principal: "42", Repository: "owner/repo"}
			request, err := workqueue.NewRequest("settlement", kind, actor, workqueue.OperationsParameters{Operations: ops})
			require.NoError(t, err)
			previous := commits[len(commits)-1].ID
			commits = append(commits, workqueue.QueueCommit{Version: 3, ID: "commit-8", Previous: &previous,
				Request: request, Actor: actor, PolicyEpoch: "epoch-1", At: 8, Operations: ops})
			ledger, err := workqueue.Serialize(commits)
			require.NoError(t, err)
			fixture["transactionLog"] = string(ledger)
			data, err := json.Marshal(fixture)
			require.NoError(t, err)
			snapshot, err := parseWorkQueueSnapshot(data)
			require.NoError(t, err)
			assert.Equal(t, expected, snapshot.Current.Assignment.Claims[0].Barrier)
			receipt, err := json.Marshal(snapshot)
			require.NoError(t, err)
			assert.NotContains(t, string(receipt), "private-verifier-receipt")
			assert.NotContains(t, string(receipt), "no_write")
		})
	}
}
