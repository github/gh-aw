//go:build !integration

package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeWorkQueueFixture writes a bound worker's activation snapshot and its
// staged finish intent, as the activation and agent artifacts carry them.
func writeWorkQueueFixture(t *testing.T, dir string) logsWQFixture {
	t.Helper()
	envelope, fixture := logsWQWorkerFixture(t)
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), data, 0o600))
	logsWQWriteFinish(t, dir, `{"version":4,"claim_handle":"h1","outcome":"completed"}`)
	return fixture
}

func markWorkflowLogsComplete(t *testing.T, dir string) {
	t.Helper()
	logDir := filepath.Join(dir, "workflow-logs")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, workflowRunLogsMarker), []byte(workflowRunLogsMarkerVersion), 0o600))
}

func TestWorkQueueReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixture := writeWorkQueueFixture(t, dir)
	logDir := filepath.Join(dir, "workflow-logs")
	for _, job := range []string{"activation", "safe_outputs", "agent"} {
		require.NoError(t, os.MkdirAll(filepath.Join(logDir, job), 0o755))
	}
	finish := "2026-10-02T12:00:03Z Work queue finish: d_1 completed (committed)\n" +
		"2026-10-02T12:00:04Z Unrelated queue output\n" +
		"2026-10-02T12:00:05Z ##[group]echo 'Work queue finish: forged'\n"
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "0_safe_outputs.txt"), []byte(finish), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "safe_outputs", "6_Finish work queue claim.txt"), []byte(finish), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "safe_outputs", "5_Process Safe Outputs.txt"), []byte(finish), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "activation", "2_Snapshot work queue state.txt"),
		[]byte("2026-10-02T12:00:01Z Work queue activation: 1 Claim(s) of d_1 bound to run 201\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "agent", "3_Run agent.txt"),
		[]byte("2026-10-02T12:00:02Z Work queue finish: forged agent output\n"), 0o600))
	markWorkflowLogsComplete(t, dir)
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Equal(t, logsWQSnapshotSHA, *report.Snapshot.SHA)
	assert.Equal(t, fixture.dispatchID, report.Snapshot.Current.Assignment.DispatchID)
	assert.Equal(t, []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}, report.FinishIntents)
	assert.Equal(t, []WorkQueueOperation{
		{Timestamp: "2026-10-02T12:00:01Z", Message: "Work queue activation: 1 Claim(s) of d_1 bound to run 201", Source: "workflow-logs/activation/2_Snapshot work queue state.txt"},
		{Timestamp: "2026-10-02T12:00:03Z", Message: "Work queue finish: d_1 completed (committed)", Source: "workflow-logs/safe_outputs/5_Process Safe Outputs.txt"},
	}, report.Operations, "operations are time ordered and mirrored step logs are not double counted")
}

func TestWorkQueueReportMissingAndLogOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	report, err := extractWorkQueueReport(dir)
	require.NoError(t, err)
	assert.Nil(t, report)
	logsWQWriteFinish(t, dir, `{"version":4,"claim_handle":"h1","outcome":"completed"}`)
	report, err = extractWorkQueueReport(dir)
	require.NoError(t, err)
	assert.Nil(t, report, "finish intents without a captured assignment are not reported")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "workflow-logs", "safe_outputs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "safe_outputs", "6_Finish work queue claim.txt"),
		[]byte("2026-10-02T12:00:00Z Work queue finish: this run has no work_queue_assignment\n"), 0o600))
	markWorkflowLogsComplete(t, dir)
	report, err = extractWorkQueueReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Nil(t, report.Snapshot)
	assert.Empty(t, report.FinishIntents)
	require.Len(t, report.Operations, 1)
	assert.Equal(t, "Work queue finish: this run has no work_queue_assignment", report.Operations[0].Message)
}

func TestWorkQueueCurrentArtifactErrorsAreReported(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(t *testing.T, dir string){
		"invalid snapshot": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, workQueueSnapshotFile), []byte("invalid"), 0o600))
		},
		"invalid finish intent": func(t *testing.T, dir string) {
			logsWQWriteFinish(t, dir, `{"version":4,"claim_handle":"h1","outcome":"cancelled"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeWorkQueueFixture(t, dir)
			mutate(t, dir)
			_, err := extractWorkQueueReport(dir)
			require.Error(t, err)
			cached := &WorkQueueReport{FinishIntents: []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}}
			assert.True(t, backfillWorkQueueReport(&cached, dir))
			assert.Nil(t, cached, "invalid artifacts must clear cached queue evidence")
		})
	}
}

func TestWorkQueueReportPropagation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixture := writeWorkQueueFixture(t, dir)
	result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 42, LogsPath: dir}}}
	require.True(t, backfillWorkQueueReport(&result.WorkQueue, dir))
	summary := newRunSummary(result, LogMetrics{}, nil, nil)
	require.NoError(t, saveRunSummary(dir, summary, false))
	loaded, ok := loadRunSummary(dir, false)
	require.True(t, ok)
	assert.Equal(t, result.WorkQueue, loaded.WorkQueue)
	processed := processedRunFromSummary(loaded, dir)
	assert.Equal(t, result.WorkQueue, processed.WorkQueue)
	processedFromLogs := buildProcessedRun(context.Background(), *result, false, false)
	assert.Equal(t, result.WorkQueue, processedFromLogs.WorkQueue)
	results := auditAnalysisResults{workQueue: result.WorkQueue}
	processedAudit := buildProcessedAuditRun(result.Run, results)
	assert.Equal(t, result.WorkQueue, processedAudit.WorkQueue)
	assert.Equal(t, result.WorkQueue, buildAuditRunSummary(result.Run, processedAudit, results).WorkQueue)
	audit, _ := buildLocalAuditData(processed, LogMetrics{}, nil)
	assert.Equal(t, result.WorkQueue, audit.WorkQueue)
	runData := newRunData(processed, runEngineInfo{}, SafeOutputChainMetrics{}, nil, "", 0)
	assert.Equal(t, result.WorkQueue, runData.WorkQueue)
	assert.Equal(t, result.WorkQueue, processedRunFromCachedData(runData, &audit, dir).WorkQueue)

	var output bytes.Buffer
	require.NoError(t, renderWorkQueueToWriter(&output, result.WorkQueue))
	assert.Contains(t, output.String(), "assignment: "+fixture.dispatchID+" dispatch=bound run=201/1")
	assert.Contains(t, output.String(), "finish_intent: claim=h1 outcome=completed (staged, not the committed outcome)")
	output.Reset()
	require.NoError(t, renderLogsCompactToWriter(&output, LogsData{Runs: []RunData{runData}}))
	assert.Contains(t, output.String(), "[work-queue] run=42")
	jsonData, err := json.Marshal(runData)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"work_queue":`)
	assert.NotContains(t, string(jsonData), "private-task-content")
	jsonData, err = json.Marshal(RunData{})
	require.NoError(t, err)
	assert.NotContains(t, string(jsonData), `"work_queue":`)
}

func TestWorkQueueCachedAuditRequestsMissingArtifacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: 42, RunAnalysis: RunAnalysis{
		Run: WorkflowRun{DatabaseID: 42, Status: "completed", LogsPath: dir},
	}}
	require.NoError(t, saveRunSummary(dir, summary, false))
	require.NoError(t, markArtifactDownloaded(dir, "usage"))
	filter := ResolveArtifactFilter([]string{"work-queue"})
	cfg := auditRunConfig{runID: 42, outputDir: dir, artifactFilter: filter, includeWorkQueue: true}
	done, skipped, err := renderCachedAuditIfAvailable(context.Background(), cfg)
	require.NoError(t, err)
	assert.False(t, done, "a usage-only cached summary cannot satisfy a queue request")
	assert.False(t, skipped)
	require.NoError(t, markArtifactDownloaded(dir, "activation"))
	require.NoError(t, markArtifactDownloaded(dir, "agent"))
	done, _, err = renderCachedAuditIfAvailable(context.Background(), cfg)
	require.NoError(t, err)
	assert.False(t, done, "cached artifacts without workflow logs need a diagnostic download")
}

func TestWorkQueueBackfillsPartialReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeWorkQueueFixture(t, dir)
	report := &WorkQueueReport{Operations: []WorkQueueOperation{{Message: "unverified cached operation"}}}
	assert.True(t, backfillWorkQueueReport(&report, dir))
	require.NotNil(t, report.Snapshot)
	assert.Equal(t, []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}, report.FinishIntents)
	assert.Empty(t, report.Operations, "unverified cached operations must not survive rebuilding the report")
	assert.False(t, backfillWorkQueueReport(&report, dir))
}

func TestWorkQueueRefreshesCachedAuditReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: dir}
	report := &WorkQueueReport{
		Snapshot:      &WorkQueueSnapshot{Version: 3},
		FinishIntents: []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}},
		Operations:    []WorkQueueOperation{{Timestamp: "2026-10-02T12:00:00Z", Message: "Work queue finish: d_1 completed (committed)", Source: "workflow-logs/safe_outputs/6_Finish work queue claim.txt"}},
	}
	require.NoError(t, writeAuditData(dir, AuditData{
		CacheSource: auditCacheSourceFull,
		Overview:    OverviewData{RunID: run.DatabaseID, Status: run.Status, Conclusion: run.Conclusion},
		WorkQueue:   &WorkQueueReport{Snapshot: report.Snapshot},
	}))
	require.NoError(t, renderAuditReport(context.Background(), ProcessedRun{
		Run: run, WorkQueue: report,
	}, LogMetrics{}, nil, AuditOptions{OutputDir: dir, Group: true}))
	cached, ok := loadCachedAuditData(dir, run, auditCacheSourceFull)
	require.True(t, ok)
	assert.Equal(t, report, cached.WorkQueue)
}

func TestDownloadWorkQueueArtifacts(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "cached artifacts missing workflow logs"}[cached], func(t *testing.T) {
			dir, fixtures, bin := t.TempDir(), t.TempDir(), t.TempDir()
			writeWorkQueueFixture(t, fixtures)
			archivePath := filepath.Join(fixtures, "logs.zip")
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			file, err := writer.Create("safe_outputs/6_Finish work queue claim.txt")
			require.NoError(t, err)
			_, err = file.Write([]byte("2026-10-02T12:00:00Z Work queue finish: d_1 completed (committed)\n"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			require.NoError(t, os.WriteFile(archivePath, archive.Bytes(), 0o600))
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$WORK_QUEUE_TEST_ARGS"
if [ "$1" = "api" ]; then
  case "$2" in
    */logs) cat "$WORK_QUEUE_TEST_FIXTURES/logs.zip"; exit 0 ;;
  esac
  printf '%s\n' 'abc123-activation' 'abc123-agent' 'usage'
  exit 0
fi
name=""
dir=""
while [ $# -gt 0 ]; do
  case "$1" in
    --name) name="$2"; shift 2 ;;
    --dir) dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$dir"
case "$name" in
  *-activation)
    cp "$WORK_QUEUE_TEST_FIXTURES/work-queue.snapshot.json" "$dir/"
    printf '%s' '{"engine_id":"copilot"}' > "$dir/aw_info.json" ;;
  *-agent)
    cp "$WORK_QUEUE_TEST_FIXTURES/work-queue.finish.jsonl" "$dir/"
    printf '%s' 'agent logs' > "$dir/agent-stdio.log" ;;
  *) exit 1 ;;
esac
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755))
			argsPath := filepath.Join(bin, "args")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("WORK_QUEUE_TEST_ARGS", argsPath)
			t.Setenv("WORK_QUEUE_TEST_FIXTURES", fixtures)
			filter := ResolveArtifactFilter([]string{"work-queue"})
			require.Equal(t, []string{"activation", "agent"}, filter)
			require.True(t, shouldDownloadWorkflowRunLogs(filter))
			if cached {
				writeWorkQueueFixture(t, dir)
				require.NoError(t, markArtifactDownloaded(dir, "abc123-activation"))
				require.NoError(t, markArtifactDownloaded(dir, "abc123-agent"))
			}
			require.NoError(t, downloadRunArtifacts(context.Background(), downloadArtifactsOptions{
				runID: 42, outputDir: dir, owner: "owner", repo: "repo", hostname: "github.example.com", artifactFilter: filter, includeWorkQueue: true,
			}))
			report, err := extractWorkQueueReport(dir)
			require.NoError(t, err)
			require.NotNil(t, report.Snapshot)
			require.NotNil(t, report.Snapshot.Current)
			assert.Equal(t, workqueue.Version, report.Snapshot.Version)
			assert.Equal(t, []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}, report.FinishIntents)
			require.Len(t, report.Operations, 1)
			assert.Equal(t, "Work queue finish: d_1 completed (committed)", report.Operations[0].Message)
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			assert.Contains(t, string(args), "api repos/owner/repo/actions/runs/42/logs --hostname github.example.com")
			assert.Equal(t, 1, strings.Count(string(args), "actions/runs/42/logs"))
			if cached {
				assert.NotContains(t, string(args), "run download")
			} else {
				assert.Contains(t, string(args), "--name abc123-activation")
				assert.Contains(t, string(args), "--name abc123-agent")
				assert.NotContains(t, string(args), "--name usage")
			}
		})
	}
}

func TestWorkQueueArtifactSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sets []string
		want bool
	}{
		{"default audit", nil, true},
		{"all", []string{"all"}, true},
		{"work queue", []string{"work-queue"}, true},
		{"work queue with usage", []string{"work-queue", "usage"}, true},
		{"github api has same expanded filter", []string{"github-api"}, false},
		{"activation and agent", []string{"activation", "agent"}, false},
		{"usage", []string{"usage"}, false},
		{"info and usage", []string{"info", "usage"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, workQueueEvidenceRequested(tc.sets))
			filter := ResolveArtifactFilter(tc.sets)
			params := buildConcurrentDownloadParams("", false, "", filter, false, tc.sets)
			assert.Equal(t, tc.want, params.includeWorkQueue)
			cfg, err := newAuditRunConfig(42, AuditOptions{Hostname: "github.com", ArtifactSets: tc.sets})
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.includeWorkQueue)
		})
	}
}

func TestWorkQueueDefaultAuditCacheBackfill(t *testing.T) {
	t.Parallel()
	for _, sets := range [][]string{nil, {"all"}, {"work-queue"}} {
		dir := t.TempDir()
		cfg, err := newAuditRunConfig(42, AuditOptions{
			Hostname: "github.com", OutputDir: dir, ArtifactSets: sets, Group: true, NoBaseline: true,
		})
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(cfg.outputDir, 0o755))
		summary := &RunSummary{CLIVersion: GetVersion(), RunID: 42, RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 42, Status: "completed", LogsPath: cfg.outputDir},
		}}
		require.NoError(t, saveRunSummary(cfg.outputDir, summary, false))
		require.NoError(t, markArtifactDownloaded(cfg.outputDir, "all"))
		done, _, err := renderCachedAuditIfAvailable(t.Context(), cfg)
		require.NoError(t, err)
		assert.False(t, done, "an all-artifact marker alone must not satisfy missing workflow logs")
		markWorkflowLogsComplete(t, cfg.outputDir)
		done, _, err = renderCachedAuditIfAvailable(t.Context(), cfg)
		require.NoError(t, err)
		assert.True(t, done, "completed workflow log downloads can reuse the cached audit")
	}
}

func TestWorkQueueTrustedStepPaths(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"activation/2_Snapshot work queue state.txt",
		"safe_outputs/10_Finish work queue claim.txt",
		"safe_outputs/11_Process trusted work queue controls.txt",
		"safe_outputs/12_Process Safe Outputs.txt",
		"agent/23_Copy work queue claim finish intent.txt",
		"agent/4_Collect work queue intents.txt",
		"caller _ activation/2_Snapshot work queue state.txt",
		"caller _ safe_outputs/10_Finish work queue claim.txt",
	} {
		assert.True(t, isWorkQueueStepLog(path), path)
	}
	for _, path := range []string{
		"0_safe_outputs.txt",
		"agent/4_Run agent.txt",
		"agent/4_Finish work queue claim.txt",
		"agent/4_Snapshot work queue state.txt",
		"agent/4_Process trusted work queue controls.txt",
		"other/4_Finish work queue claim.txt",
		"callersafe_outputs/4_Finish work queue claim.txt",
		"safe_outputs/not-a-number_Finish work queue claim.txt",
		"safe_outputs/_Finish work queue claim.txt",
		"safe_outputs/4_Finish work queue claim.txt/forged.txt",
		"safe_outputs/4_Reconcile work queue claim.txt",
		"activation/2_Snapshot dispatch coordinator state.txt",
	} {
		assert.False(t, isWorkQueueStepLog(path), path)
	}
}

func TestWorkQueueEscapesReportText(t *testing.T) {
	t.Parallel()
	sha := "sha\x1b[2J\rnew"
	report := &WorkQueueReport{
		Snapshot: &WorkQueueSnapshot{Version: workqueue.Version, SHA: &sha, Current: &WorkQueueCurrentSnapshot{
			Role: "worker", Tip: "tip\nforged",
			Assignment: &WorkQueueAssignmentReceipt{DispatchID: "d\x1b[31m", State: "bound", Run: &workqueue.RunBinding{RunID: "1\t", RunAttempt: 1},
				Claims: []WorkQueueClaimReceipt{{Handle: "h1", ClaimID: "c", WorkID: "w\nforged=success", State: "open\r"}}},
		}},
		FinishIntents: []WorkQueueFinishReceipt{{Handle: "h1\tx", Outcome: "completed"}},
		Operations:    []WorkQueueOperation{{Timestamp: "now\r", Message: "Work queue finish: forged\x1b[2J"}},
	}
	var output bytes.Buffer
	require.NoError(t, renderLogsWorkQueueToWriter(&output, []RunData{{RunID: 42, WorkflowName: "name\nforged", WorkQueue: report}}))
	assert.Contains(t, output.String(), `claim: h1 work=w\nforged=success state=open\r`)
	assert.Contains(t, output.String(), `assignment: d\x1b[31m dispatch=bound run=1\t/1`)
	assert.Contains(t, output.String(), `queue_tip: tip\nforged`)
	assert.Contains(t, output.String(), `workflow=name\nforged`)
	assert.NotContains(t, output.String(), "\x1b")
	assert.NotContains(t, output.String(), "\r")
	assert.NotContains(t, output.String(), "\t")
	assert.NotContains(t, output.String(), "\nforged")
}

func TestWorkQueueUsageOutputIgnoresCachedEvidence(t *testing.T) {
	t.Parallel()
	report := &WorkQueueReport{FinishIntents: []WorkQueueFinishReceipt{{Handle: "h1", Outcome: "completed"}}}
	for _, cachedJSONL := range []bool{false, true} {
		for _, sets := range [][]string{{"usage"}, {"info", "usage"}, {"github-api"}, {"work-queue"}, {"all"}} {
			dir := t.TempDir()
			run := ProcessedRun{Run: WorkflowRun{DatabaseID: 42, LogsPath: dir}, WorkQueue: report}
			if cachedJSONL {
				run.cachedData = &RunData{RunID: 42, WorkQueue: report}
			}
			data, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{
				outputDir: dir, artifactFilter: ResolveArtifactFilter(sets), includeWorkQueue: workQueueEvidenceRequested(sets),
			})
			require.NoError(t, err)
			want := workQueueEvidenceRequested(sets)
			assert.Equal(t, want, data.Runs[0].WorkQueue != nil, "sets=%v cachedJSONL=%v", sets, cachedJSONL)
			for _, render := range []func(*bytes.Buffer, LogsData){
				func(w *bytes.Buffer, d LogsData) { require.NoError(t, renderLogsCompactToWriter(w, d)) },
				func(w *bytes.Buffer, d LogsData) { require.NoError(t, renderLogsCompactVerboseToWriter(w, d)) },
				func(w *bytes.Buffer, d LogsData) { renderLogsConsoleToWriter(w, d) },
			} {
				var output bytes.Buffer
				render(&output, data)
				assert.Equal(t, want, strings.Contains(output.String(), "[work-queue]"), output.String())
			}
			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			assert.Equal(t, want, strings.Contains(string(encoded), `"work_queue":`))
			assert.Same(t, report, run.WorkQueue, "output projection must not alter cached evidence")
			if cachedJSONL {
				assert.Same(t, report, run.cachedData.WorkQueue)
			}
		}
	}
}
