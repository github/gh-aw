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

const logsWQSnapshotSHA = "0123456789abcdef0123456789abcdef01234567"

// logsWQFixture is a worker activation snapshot: two Work submitted, one
// dispatched, launched and bound to the snapshot's own run.
type logsWQFixture struct {
	log        *workQueueTestLog
	dispatchID string
	worker     workqueue.Actor
	assignment workqueue.Assignment
}

func logsWQEnvelope(role string, origin any, worker *workqueue.Assignment, transactionLog string) map[string]any {
	return map[string]any{
		"version": workqueue.Version, "sha": logsWQSnapshotSHA, "transactionLog": transactionLog,
		"origin": origin, "role": role, "worker": worker,
	}
}

func logsWQObserver() workqueue.Actor {
	return workqueue.Actor{Role: "observer", Repository: workQueueTestRepository, Workflow: ".github/workflows/monitor.lock.yml", RunID: "300", RunAttempt: 1}
}

func logsWQWorkerFixture(t *testing.T) (map[string]any, logsWQFixture) {
	t.Helper()
	log := newWorkQueueTestLog(t)
	private := func(work *workqueue.WorkDefinition) {
		work.Payload = json.RawMessage(`{"plan":"private-task-content"}`)
	}
	log.submit(log.work("a", private), log.work("b", private))
	dispatchID := log.dispatch(1)[0]
	log.launch(dispatchID, "201")
	worker := workQueueTestWorker("201")
	log.bind(dispatchID, worker)
	assignment, err := workqueue.AssignmentForDispatch(log.state(), dispatchID)
	require.NoError(t, err)
	require.Len(t, assignment.Claims, 1)
	fixture := logsWQFixture{log: log, dispatchID: dispatchID, worker: worker, assignment: assignment}
	return logsWQEnvelope("worker", worker, &assignment, log.jsonl()), fixture
}

func logsWQParse(t *testing.T, envelope map[string]any) (*WorkQueueSnapshot, error) {
	t.Helper()
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	return parseWorkQueueSnapshot(data)
}

func logsWQWriteFinish(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, workQueueFinishFile)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

func TestWorkQueueReportMergeDetectsSnapshotValueChanges(t *testing.T) {
	t.Parallel()
	envelope, _ := logsWQWorkerFixture(t)
	seed, err := logsWQParse(t, envelope)
	require.NoError(t, err)
	encoded, err := json.Marshal(seed)
	require.NoError(t, err)
	for name, change := range map[string]func(*WorkQueueSnapshot){
		"version":           func(s *WorkQueueSnapshot) { s.Version++ },
		"sha":               func(s *WorkQueueSnapshot) { *s.SHA = "other" },
		"role":              func(s *WorkQueueSnapshot) { s.Current.Role = "observer" },
		"tip":               func(s *WorkQueueSnapshot) { s.Current.Tip = "other" },
		"count":             func(s *WorkQueueSnapshot) { s.Current.CommitCount++ },
		"dispatch":          func(s *WorkQueueSnapshot) { s.Current.Assignment.DispatchID = "other" },
		"dispatch state":    func(s *WorkQueueSnapshot) { s.Current.Assignment.State = "closed" },
		"run":               func(s *WorkQueueSnapshot) { s.Current.Assignment.Run.RunAttempt++ },
		"claim":             func(s *WorkQueueSnapshot) { s.Current.Assignment.Claims[0].State = "completed" },
		"counts":            func(s *WorkQueueSnapshot) { s.Current.Diagnostics.Completed++ },
		"dispatch counts":   func(s *WorkQueueSnapshot) { s.Current.Diagnostics.DispatchesByState["closed"]++ },
		"pool":              func(s *WorkQueueSnapshot) { s.Current.Diagnostics.Pools[0].Eligible++ },
		"claim diagnostics": func(s *WorkQueueSnapshot) { s.Current.Diagnostics.AssignedClaims[0].Claim.State = "cancelled" },
	} {
		t.Run(name, func(t *testing.T) {
			var copy WorkQueueSnapshot
			require.NoError(t, json.Unmarshal(encoded, &copy))
			require.Equal(t, seed, &copy, "cached snapshots must round-trip to equal values")
			report := &WorkQueueReport{Snapshot: seed}
			require.False(t, mergeWorkQueueReport(&report, &WorkQueueReport{Snapshot: &copy}), "identical values must not invalidate caches")
			change(&copy)
			require.True(t, mergeWorkQueueReport(&report, &WorkQueueReport{Snapshot: &copy}))
			require.Same(t, &copy, report.Snapshot)
			require.False(t, mergeWorkQueueReport(&report, &WorkQueueReport{Snapshot: &copy}))
		})
	}
}

func TestWorkQueueCurrentReport(t *testing.T) {
	t.Parallel()
	envelope, fixture := logsWQWorkerFixture(t)
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), data, 0o600))
	logsWQWriteFinish(t, dir, `{"version":4,"claim_handle":"h1","outcome":"completed"}`)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report.Snapshot.Current)
	assert.Equal(t, workqueue.Version, report.Snapshot.Version)
	assert.Equal(t, logsWQSnapshotSHA, *report.Snapshot.SHA, "Git HEAD is distinct from the causal QueueCommit identity")
	current := report.Snapshot.Current
	assert.Equal(t, "worker", current.Role)
	assert.Equal(t, 4, current.CommitCount)
	assert.Equal(t, fixture.log.commits[3].ID, current.Tip)
	member := fixture.assignment.Claims[0]
	assert.Equal(t, &WorkQueueAssignmentReceipt{
		DispatchID: fixture.dispatchID, State: "bound", Run: &workqueue.RunBinding{RunID: "201", RunAttempt: 1},
		Claims: []WorkQueueClaimReceipt{{Handle: "h1", ClaimID: member.ClaimID, WorkID: member.WorkID, State: "open"}},
	}, current.Assignment)
	require.NotNil(t, current.Diagnostics)
	assert.Equal(t, 1, current.Diagnostics.Available)
	assert.Equal(t, 1, current.Diagnostics.Claimed)
	assert.Equal(t, 1, current.Diagnostics.DispatchesByState["bound"])
	assert.Equal(t, []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}, report.FinishIntents)

	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, report))
	text := output.String()
	assert.Contains(t, text, "snapshot: protocol=4 role=worker commits=4 (captured ledger, not live state)")
	assert.Contains(t, text, "queue_tip: "+current.Tip)
	assert.Contains(t, text, fmt.Sprintf("assignment: %s dispatch=bound run=201/1", fixture.dispatchID))
	assert.Contains(t, text, fmt.Sprintf("claim: h1 work=%s state=open", member.WorkID))
	assert.Contains(t, text, "finish_intent: claim=h1 outcome=completed (staged, not the committed outcome)")
	assert.Contains(t, text, "snapshot_sha: "+logsWQSnapshotSHA)
	serialized, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "private-task-content")
	assert.NotContains(t, string(serialized), "transactionLog")
}

func TestWorkQueueUnsupportedProtocolSnapshot(t *testing.T) {
	t.Parallel()
	envelope, _ := logsWQWorkerFixture(t)
	envelope["version"] = 3
	envelope["transactionLog"] = "not a v4 ledger\n"
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), data, 0o600))
	logsWQWriteFinish(t, dir, `{"version":3,"claim_handle":"h1","outcome":"completed"}`)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	assert.Equal(t, &WorkQueueSnapshot{Version: 3}, report.Snapshot, "other protocol versions are reported, not replayed")
	assert.Empty(t, report.FinishIntents, "finish intents need a replayed assignment")
	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, report))
	assert.Contains(t, output.String(), "snapshot: protocol=3 (unsupported; not replayed)")
	_, err = parseCurrentWorkQueueSnapshot(data)
	require.Error(t, err, "the current parser must not accept another protocol version")
}

func TestWorkQueueAssignmentReceiptReflectsCapturedLedger(t *testing.T) {
	t.Parallel()
	_, fixture := logsWQWorkerFixture(t)
	fixture.log.finish(fixture.dispatchID, fixture.worker)
	receipt, err := currentWorkQueueAssignmentReceipt(fixture.log.state(), fixture.assignment, fixture.worker)
	require.NoError(t, err)
	data, err := json.Marshal(receipt)
	require.NoError(t, err)
	member := fixture.assignment.Claims[0]
	assert.JSONEq(t, fmt.Sprintf(`{
		"dispatch_id":%q,"state":"closed","run":{"run_id":"201","run_attempt":1},
		"claims":[{"claim_handle":"h1","claim_id":%q,"work_id":%q,"state":"completed"}]
	}`, fixture.dispatchID, member.ClaimID, member.WorkID), string(data))
}

func TestWorkQueueCurrentSnapshotRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{
		"fork", "truncated", "duplicate-key", "missing-worker-field", "empty-sha", "wrong-handle", "wrong-claim",
		"foreign-run", "rerun", "unbound", "empty-ledger", "foreign-repository", "unknown-origin-field",
		"unknown-assignment-field", "observer-with-assignment", "dispatcher-with-assignment",
	} {
		t.Run(scenario, func(t *testing.T) {
			envelope, fixture := logsWQWorkerFixture(t)
			lines := strings.SplitAfter(envelope["transactionLog"].(string), "\n")
			switch scenario {
			case "fork":
				var commit map[string]any
				require.NoError(t, json.Unmarshal([]byte(lines[3]), &commit))
				commit["previous"] = fixture.log.commits[1].ID
				data, err := json.Marshal(commit)
				require.NoError(t, err)
				lines[3] = string(data) + "\n"
				envelope["transactionLog"] = strings.Join(lines, "")
			case "truncated":
				envelope["transactionLog"] = strings.TrimSuffix(envelope["transactionLog"].(string), "\n")
			case "duplicate-key":
				data, err := json.Marshal(envelope)
				require.NoError(t, err)
				_, err = parseWorkQueueSnapshot(append([]byte(`{"version":4,`), data[1:]...))
				require.Error(t, err)
				return
			case "missing-worker-field":
				delete(envelope, "worker")
			case "empty-sha":
				envelope["sha"] = ""
			case "wrong-handle":
				fixture.assignment.Claims[0].Handle = "h2"
				envelope["worker"] = fixture.assignment
			case "wrong-claim":
				fixture.assignment.Claims[0].ClaimID = "c_forged"
				envelope["worker"] = fixture.assignment
			case "foreign-run":
				fixture.worker.RunID = "99"
				envelope["origin"] = fixture.worker
			case "rerun":
				fixture.worker.RunAttempt = 2
				envelope["origin"] = fixture.worker
			case "unbound":
				envelope["transactionLog"] = strings.Join(lines[:3], "")
			case "empty-ledger":
				envelope["sha"], envelope["transactionLog"] = nil, ""
			case "foreign-repository":
				fixture.worker.Repository = "foreign/repo"
				envelope["origin"] = fixture.worker
			case "unknown-origin-field":
				envelope["origin"] = map[string]any{"role": "worker", "repository": workQueueTestRepository, "secret": "not-exportable"}
			case "unknown-assignment-field":
				envelope["worker"] = map[string]any{"dispatch_id": fixture.dispatchID, "claims": fixture.assignment.Claims, "extra": true}
			case "observer-with-assignment", "dispatcher-with-assignment":
				role, _, _ := strings.Cut(scenario, "-")
				fixture.worker.Role = role
				envelope["role"], envelope["origin"] = role, fixture.worker
			}
			snapshot, err := logsWQParse(t, envelope)
			require.Error(t, err, scenario)
			assert.Nil(t, snapshot)
			assert.NotContains(t, err.Error(), "not-exportable")
		})
	}
}

func TestWorkQueueCurrentUnassignedSnapshotProvenance(t *testing.T) {
	t.Parallel()
	dispatcher := workQueueTestDispatcher
	for _, scenario := range []struct {
		name   string
		role   any
		mutate func(*workqueue.Actor)
		valid  bool
	}{
		{"dispatcher", "dispatcher", func(*workqueue.Actor) {}, true},
		{"dispatcher-rerun", "dispatcher", func(a *workqueue.Actor) { a.RunAttempt = 2 }, true},
		{"dispatcher-maximum-attempt", "dispatcher", func(a *workqueue.Actor) { a.RunAttempt = 4096 }, true},
		{"dispatcher-over-bound-attempt", "dispatcher", func(a *workqueue.Actor) { a.RunAttempt = 4097 }, false},
		{"dispatcher-zero-attempt", "dispatcher", func(a *workqueue.Actor) { a.RunAttempt = 0 }, false},
		{"missing-run", "dispatcher", func(a *workqueue.Actor) { a.RunID = "" }, false},
		{"leading-zero-run", "dispatcher", func(a *workqueue.Actor) { a.RunID = "00020" }, false},
		{"maximum-run", "dispatcher", func(a *workqueue.Actor) { a.RunID = strings.Repeat("9", 256) }, true},
		{"over-limit-run", "dispatcher", func(a *workqueue.Actor) { a.RunID = strings.Repeat("9", 257) }, false},
		{"non-ascii-run", "dispatcher", func(a *workqueue.Actor) { a.RunID = "\uff11" }, false},
		{"foreign-repository", "dispatcher", func(a *workqueue.Actor) { a.Repository = "foreign/repo" }, false},
		{"non-workflow-path", "dispatcher", func(a *workqueue.Actor) { a.Workflow = "dispatcher.lock.yml" }, false},
		{"observer", "observer", func(a *workqueue.Actor) { a.Role = "observer" }, true},
		{"settled-worker", "worker", func(a *workqueue.Actor) { *a = workQueueTestWorker("201") }, true},
		{"worker-without-workflow", "worker", func(a *workqueue.Actor) { *a = workQueueTestWorker("201"); a.Workflow = "" }, false},
		{"role-mismatch", "observer", func(*workqueue.Actor) {}, false},
		{"producer", "producer", func(a *workqueue.Actor) { a.Role = "producer" }, false},
		{"administrator", "administrator", func(a *workqueue.Actor) { a.Role = "administrator" }, false},
		{"reconciler", "reconciler", func(a *workqueue.Actor) { a.Role = "reconciler" }, false},
		{"projector", "projector", func(a *workqueue.Actor) { a.Role = "projector" }, false},
		{"unknown-role", "private-token-value", func(a *workqueue.Actor) { a.Role = "private-token-value" }, false},
		{"null-role", nil, func(*workqueue.Actor) {}, false},
		{"numeric-role", 3, func(*workqueue.Actor) {}, false},
		{"object-role", map[string]any{}, func(*workqueue.Actor) {}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			envelope, _ := logsWQWorkerFixture(t)
			origin := dispatcher
			scenario.mutate(&origin)
			envelope["role"], envelope["origin"], envelope["worker"] = scenario.role, origin, nil
			snapshot, err := logsWQParse(t, envelope)
			if !scenario.valid {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-token-value")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, scenario.role, snapshot.Current.Role)
			assert.Nil(t, snapshot.Current.Assignment)
			require.NotNil(t, snapshot.Current.Diagnostics)
			assert.Equal(t, "captured_snapshot", snapshot.Current.Diagnostics.Status)
			assert.Empty(t, snapshot.Current.Diagnostics.AssignedClaims)
			var output bytes.Buffer
			require.NoError(t, renderWorkQueueToWriter(&output, &WorkQueueReport{Snapshot: snapshot}))
			assert.Contains(t, output.String(), fmt.Sprintf("role=%s commits=4", scenario.role))
			assert.NotContains(t, output.String(), "assignment:")
		})
	}
}

func TestWorkQueueCurrentEmptyQueueSnapshot(t *testing.T) {
	t.Parallel()
	for _, origin := range []workqueue.Actor{workQueueTestDispatcher, logsWQObserver()} {
		t.Run(origin.Role, func(t *testing.T) {
			envelope := logsWQEnvelope(origin.Role, origin, nil, "")
			envelope["sha"] = nil
			snapshot, err := logsWQParse(t, envelope)
			require.NoError(t, err)
			assert.Nil(t, snapshot.SHA)
			current := snapshot.Current
			assert.Empty(t, current.Tip)
			assert.Zero(t, current.CommitCount)
			assert.Equal(t, workQueueTestRepository, current.Diagnostics.Repository)
			assert.Zero(t, current.Diagnostics.Work)
			assert.Empty(t, current.Diagnostics.Pools)
			var output bytes.Buffer
			require.NoError(t, renderWorkQueueToWriter(&output, &WorkQueueReport{Snapshot: snapshot}))
			assert.Contains(t, output.String(), fmt.Sprintf("snapshot: protocol=4 role=%s commits=0", origin.Role))
			assert.NotContains(t, output.String(), "snapshot_sha:")
		})
	}
}

func TestWorkQueueCurrentSnapshotVisibilityShape(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"worker", "observer"} {
		for _, scenario := range []struct {
			name  string
			value any
			valid bool
		}{
			{"omitted", nil, true},
			{"empty", []string{}, true},
			{"strings", []string{"work-a", "work-b"}, true},
			{"presentation-only", []string{"", "unknown-work", "unknown-work", "unicode-\u00e9"}, true},
			{"number-entry", []any{1}, false},
			{"boolean-entry", []any{false}, false},
			{"object-entry", []any{map[string]any{}}, false},
			{"nested-array-entry", []any{[]string{"work-a"}}, false},
			{"string", "private-token-value", false},
			{"number", 1, false},
			{"object", map[string]any{"private-token-value": true}, false},
		} {
			t.Run(role+"/"+scenario.name, func(t *testing.T) {
				envelope, _ := logsWQWorkerFixture(t)
				if role == "observer" {
					envelope["role"], envelope["origin"], envelope["worker"] = role, logsWQObserver(), nil
				}
				if scenario.name != "omitted" {
					envelope["visible_work_ids"] = scenario.value
				}
				snapshot, err := logsWQParse(t, envelope)
				if !scenario.valid {
					require.Error(t, err)
					assert.NotContains(t, err.Error(), "private-token-value")
					return
				}
				require.NoError(t, err)
				assert.Equal(t, role == "worker", snapshot.Current.Assignment != nil)
				encoded, err := json.Marshal(snapshot)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "visible_work_ids", "visibility hints are presentation input, not report data")
			})
		}
	}
}

func TestWorkQueueCurrentOriginExcludesRuntimeContext(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"worker", "dispatcher", "observer"} {
		for _, field := range []string{"principal", "dispatch_id", "claim_handle", "native_run"} {
			t.Run(role+"/"+field, func(t *testing.T) {
				envelope, fixture := logsWQWorkerFixture(t)
				origin := fixture.worker
				switch role {
				case "dispatcher":
					origin = workQueueTestDispatcher
				case "observer":
					origin = logsWQObserver()
				}
				if role != "worker" {
					envelope["role"], envelope["worker"] = role, nil
				}
				data, err := json.Marshal(origin)
				require.NoError(t, err)
				var fields map[string]any
				require.NoError(t, json.Unmarshal(data, &fields))
				fields[field] = "private-token-value"
				if field == "native_run" {
					fields[field] = map[string]any{"credential": "private-token-value"}
				}
				envelope["origin"] = fields
				_, err = logsWQParse(t, envelope)
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-token-value")
			})
		}
	}
}

func TestWorkQueueCurrentFinishIntents(t *testing.T) {
	t.Parallel()
	snapshot := &WorkQueueCurrentSnapshot{Assignment: &WorkQueueAssignmentReceipt{Claims: []WorkQueueClaimReceipt{{Handle: "h1"}, {Handle: "h2"}}}}
	valid := `{"version":4,"claim_handle":"h1","outcome":"completed"}`
	for name, lines := range map[string][]string{
		"not json":           {`not json`},
		"null":               {`null`},
		"missing version":    {`{"claim_handle":"h1","outcome":"completed"}`},
		"v3 version":         {strings.Replace(valid, `"version":4`, `"version":3`, 1)},
		"missing handle":     {`{"version":4,"outcome":"completed"}`},
		"empty handle":       {strings.Replace(valid, `"h1"`, `""`, 1)},
		"null handle":        {strings.Replace(valid, `"h1"`, `null`, 1)},
		"foreign handle":     {strings.Replace(valid, `"h1"`, `"foreign"`, 1)},
		"cancelled outcome":  {strings.Replace(valid, `"completed"`, `"cancelled"`, 1)},
		"unknown field":      {strings.Replace(valid, `}`, `,"secret":"x"}`, 1)},
		"keep_open removed":  {strings.Replace(valid, `}`, `,"keep_open":false}`, 1)},
		"conflicting":        {valid, strings.Replace(valid, `"completed"`, `"failed"`, 1)},
		"over entries limit": strings.Split(strings.Repeat(valid+"\n", 257), "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			path := logsWQWriteFinish(t, t.TempDir(), lines...)
			_, err := readCurrentWorkQueueFinishIntents(path, snapshot)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
	dir := t.TempDir()
	path := logsWQWriteFinish(t, dir, "", valid, valid, strings.Replace(valid, `"h1","outcome":"completed"`, `"h2","outcome":"failed"`, 1))
	receipts, err := readCurrentWorkQueueFinishIntents(path, snapshot)
	require.NoError(t, err)
	assert.Equal(t, []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}, {Handle: "h2", Outcome: "failed"}}, receipts, "repeated identical intents collapse")
	serialized, err := json.Marshal(receipts[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"claim_handle":"h1","outcome":"completed"}`, string(serialized))
	for _, unassigned := range []*WorkQueueCurrentSnapshot{nil, {}} {
		_, err = readCurrentWorkQueueFinishIntents(path, unassigned)
		require.ErrorContains(t, err, "foreign to the captured assignment")
	}
	receipts, err = readCurrentWorkQueueFinishIntents(filepath.Join(dir, "missing.jsonl"), snapshot)
	require.NoError(t, err)
	assert.Nil(t, receipts)
}

func TestWorkQueueCurrentUnassignedSnapshotRejectsStagedFinish(t *testing.T) {
	t.Parallel()
	for _, origin := range []workqueue.Actor{logsWQObserver(), workQueueTestWorker("201")} {
		t.Run(origin.Role, func(t *testing.T) {
			envelope, _ := logsWQWorkerFixture(t)
			envelope["role"], envelope["origin"], envelope["worker"] = origin.Role, origin, nil
			data, err := json.Marshal(envelope)
			require.NoError(t, err)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), data, 0o600))
			logsWQWriteFinish(t, dir, `{"version":4,"claim_handle":"h1","outcome":"completed"}`)
			_, err = extractWorkQueueReport(dir)
			require.ErrorContains(t, err, "foreign to the captured assignment")
		})
	}
}

func TestWorkQueueCurrentSnapshotOuterBound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), workQueueSnapshotFile)
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	require.NoError(t, os.Truncate(path, (162<<20)+1))
	_, err := extractWorkQueueReport(filepath.Dir(path))
	require.ErrorContains(t, err, "bounded input limit")
}

func TestWorkQueueCurrentTrustedProvenanceAndBounds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, job := range []string{"activation", "agent", "safe_outputs"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflow-logs", job), 0o755))
	}
	for _, entry := range []struct{ job, file, lines string }{
		{"activation", "2_Snapshot work queue state.txt", "2026-10-05T12:00:00Z Work queue activation: 1 Claim(s) of d_1 bound to run 201\n"},
		{"agent", "2_Run agent.txt", "2026-10-05T12:00:00Z Work queue activation: forged\n"},
		{"agent", "3_Copy work queue claim finish intent.txt", "2026-10-05T12:00:05Z Work queue controls: committed; 99 receipts\n"},
		{"safe_outputs", "6_Finish work queue claim.txt", "2026-10-05T12:00:01Z Work queue finish: d_1 completed (committed)\n"},
		{"safe_outputs", "5_Process trusted work queue controls.txt", "2026-10-05T12:00:02Z Work queue controls: committed; 3 receipts\n" +
			"2026-10-05T12:00:03Z Work queue controls: private-task-content\n" +
			"2026-10-05T12:00:04Z Work queue controls: committed; 3 receipts private-task-content\n"},
		{"agent", "5_Process trusted work queue controls.txt", "2026-10-05T12:00:05Z Work queue controls: committed; 99 receipts\n"},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", entry.job, entry.file), []byte(entry.lines), 0o600))
	}
	markWorkflowLogsComplete(t, dir)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.Len(t, report.Operations, 3)
	assert.Equal(t, WorkQueueOperation{Timestamp: "2026-10-05T12:00:00Z", Message: "Work queue activation: 1 Claim(s) of d_1 bound to run 201",
		Source: "workflow-logs/activation/2_Snapshot work queue state.txt"}, report.Operations[0])
	assert.Equal(t, "Work queue finish: d_1 completed (committed)", report.Operations[1].Message)
	assert.Equal(t, "Work queue controls: committed; 3 receipts", report.Operations[2].Message)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-task-content")
	assert.NotContains(t, string(encoded), "99 receipts")
	assert.NotContains(t, string(encoded), "forged")
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
		`{"version":4,"sha":null,"worker":null,"transactionLog":"","role":"dispatcher","origin":{"role":"dispatcher","repository":"owner/repo","private-token-value":"x"}}`,
		`{"version":4,"sha":null,"worker":null,"transactionLog":"{\"private-token-value\":true}\n","role":"dispatcher","origin":{"role":"dispatcher","repository":"owner/repo"}}`,
	} {
		_, err := parseCurrentWorkQueueSnapshot([]byte(raw))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private-token-value")
	}
	err := currentWorkQueueDiagnosticError("ledger", errors.New("ledger_invalid: payload contains private-token-value"))
	assert.Equal(t, "current snapshot ledger rejected (ledger_invalid)", err.Error())
	err = currentWorkQueueDiagnosticError("ledger", errors.New("Private Token Value: details"))
	assert.Equal(t, "current snapshot ledger rejected (invalid_protocol)", err.Error())
}

func TestWorkQueueCurrentCollectorAndHandlerLogProvenance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, job := range []string{"agent", "safe_outputs", "caller _ safe_outputs", "activation"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflow-logs", job), 0o755))
	}
	for _, entry := range []struct{ job, file, message string }{
		{"agent", "3_Collect work queue intents.txt", "Work queue finish: staged intents collected"},
		{"safe_outputs", "6_Process Safe Outputs.txt", "Work queue finish: handler effect recorded"},
		{"caller _ safe_outputs", "7_Process Safe Outputs.txt", "Work queue finish: reusable handler diagnostic"},
		{"safe_outputs", "8_Process Safe Outputs.txt", "Work queue delivery: forged legacy prefix"},
		{"agent", "6_Process Safe Outputs.txt", "Work queue finish: forged agent handler"},
		{"activation", "6_Process Safe Outputs.txt", "Work queue finish: forged activation handler"},
		{"safe_outputs", "3_Collect work queue intents.txt", "Work queue finish: forged collector"},
		{"safe_outputs", "bad_Process Safe Outputs.txt", "Work queue finish: forged step index"},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", entry.job, entry.file),
			[]byte("2026-10-05T12:00:00Z "+entry.message+"\n"), 0o600))
	}
	markWorkflowLogsComplete(t, dir)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	require.Len(t, report.Operations, 3)
	assert.Nil(t, report.Snapshot, "handler diagnostics must not synthesize ledger state")
	assert.Empty(t, report.FinishIntents, "collector diagnostics must not synthesize staged intents")
	for _, operation := range report.Operations {
		assert.NotContains(t, operation.Message, "forged")
	}
	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, report))
	assert.Contains(t, output.String(), "operation_logs: diagnostics only")
	assert.NotContains(t, output.String(), "snapshot:")
}

func TestWorkQueueOperationsRequireCompleteWorkflowLogs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflow-logs", "safe_outputs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "safe_outputs", "6_Finish work queue claim.txt"),
		[]byte("2026-10-05T12:00:00Z Work queue finish: d_1 completed (committed)\n"), 0o600))
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	assert.Nil(t, report, "partially downloaded logs are not reported")
	markWorkflowLogsComplete(t, dir)
	report, err = extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Len(t, report.Operations, 1)
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
	report := &WorkQueueReport{FinishIntents: []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}}
	require.ErrorIs(t, renderWorkQueueToWriter(&failingWorkQueueWriter{}, report), io.ErrClosedPipe)
	for _, successfulWrites := range []int{0, 1} {
		require.ErrorIs(t, renderLogsWorkQueueToWriter(&failingWorkQueueWriter{successfulWrites: successfulWrites}, []RunData{{WorkQueue: report}}), io.ErrClosedPipe)
	}
	require.NoError(t, renderWorkQueueToWriter(&failingWorkQueueWriter{}, nil))
}
