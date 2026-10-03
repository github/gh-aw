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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDispatchCoordinatorFixture(t *testing.T, dir string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"version": 2, "sha": "snapshot-sha",
		"worker": map[string]string{"work_id": "w", "claim_id": "c"},
		"transactionLog": "{\"version\":1,\"kind\":\"Work\",\"work\":\"w\",\"claim\":null,\"attempt\":null}\n" +
			"{\"version\":1,\"kind\":\"Claim\",\"work\":\"w\",\"claim\":\"c\",\"attempt\":null}\n",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, dispatchCoordinatorSnapshotFile), data, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, dispatchCoordinatorFinishFile), []byte("{\"outcome\":\"completed\"}\n"), 0o600))
}

func TestDispatchCoordinatorReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeDispatchCoordinatorFixture(t, dir)
	logDir := filepath.Join(dir, "workflow-logs")
	require.NoError(t, os.MkdirAll(filepath.Join(logDir, "safe_outputs"), 0o755))
	lines := "2026-10-02T12:00:02Z Dispatch coordinator: worker completion verified\n" +
		"2026-10-02T12:00:01Z Dispatch coordinator: publishing worker completion\n" +
		"2026-10-02T12:00:03Z Dispatch work claim reconciliation: completed\n" +
		"2026-10-02T12:00:04Z Unrelated queue output\n" +
		"2026-10-02T12:00:05Z ##[group]echo 'Dispatch coordinator: forged'\n"
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "0_safe_outputs.txt"), []byte(lines), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "safe_outputs", "4_Reconcile dispatch work claim.txt"), []byte(lines), 0o600))
	report, err := extractDispatchCoordinatorReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Equal(t, "snapshot-sha", *report.Snapshot.SHA)
	assert.Equal(t, &DispatchCoordinatorWorker{WorkID: "w", ClaimID: "c"}, report.Snapshot.Worker)
	require.Len(t, report.Snapshot.Transactions, 2)
	assert.Equal(t, "Claim", report.Snapshot.Transactions[1].Kind)
	assert.Equal(t, "completed", report.FinishIntent)
	require.Len(t, report.Operations, 3, "mirrored whole-job and step logs must not double count operations")
	assert.Equal(t, "Dispatch coordinator: publishing worker completion", report.Operations[0].Message)
	assert.Equal(t, "2026-10-02T12:00:01Z", report.Operations[0].Timestamp)
	assert.Equal(t, "workflow-logs/0_safe_outputs.txt", report.Operations[0].Source)
}

func TestDispatchCoordinatorReportMissingAndLogOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	report, err := extractDispatchCoordinatorReport(dir)
	require.NoError(t, err)
	assert.Nil(t, report)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "workflow-logs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs", "0_safe_outputs.txt"),
		[]byte("2026-10-02T12:00:00Z Dispatch work claim reconciliation failed; ordinary safe outputs are blocked.\n"), 0o600))
	report, err = extractDispatchCoordinatorReport(dir)
	require.NoError(t, err)
	require.NotNil(t, report)
	assert.Nil(t, report.Snapshot)
	require.Len(t, report.Operations, 1)
	assert.Contains(t, report.Operations[0].Message, "failed")
}

func TestDispatchCoordinatorSnapshotValidation(t *testing.T) {
	t.Parallel()
	for _, transaction := range []string{
		`{"kind":"Work","work":"w","claim":null,"attempt":null}`,
		`{"version":0,"kind":"Work","work":"w","claim":null,"attempt":null}`,
		`{"version":1,"kind":"Claim","work":"w","claim":"c","attempt":null}`,
		`{"version":1,"kind":"ClaimCancellation","work":"w","claim":"c","attempt":null}`,
		`{"version":1,"kind":"WorkCancellation","work":"w","claim":null,"attempt":null}`,
		`{"version":1,"kind":"Completion","work":"w","claim":"c","attempt":"42-1"}`,
	} {
		tx, err := parseDispatchCoordinatorTransaction([]byte(transaction))
		require.NoError(t, err)
		assert.Equal(t, 1, tx.Version)
	}
	for _, transaction := range []string{
		`null`, `{}`, `bad`,
		`{"version":2,"kind":"Work","work":"w","claim":null,"attempt":null}`,
		`{"version":null,"kind":"Work","work":"w","claim":null,"attempt":null}`,
		`{"version":1,"kind":"Work","work":"","claim":null,"attempt":null}`,
		`{"version":1,"kind":"Work","work":"w","claim":"c","attempt":null}`,
		`{"version":1,"kind":"Claim","work":"w","claim":null,"attempt":null}`,
		`{"version":1,"kind":"Completion","work":"w","claim":"c","attempt":null}`,
		`{"version":1,"kind":"Other","work":"w","claim":null,"attempt":null}`,
		`{"version":1,"kind":"Work","work":"w","claim":null,"extra":null}`,
	} {
		_, err := parseDispatchCoordinatorTransaction([]byte(transaction))
		require.Error(t, err, transaction)
	}
	for _, data := range []string{
		`null`, `{}`, `{"version":1,"transactionLog":""}`,
		`{"version":2,"transactionLog":null}`,
		`{"version":2,"transactionLog":"","worker":{"work_id":"w"}}`,
		`{"version":2,"transactionLog":"not json\n"}`,
	} {
		_, err := parseDispatchCoordinatorSnapshot([]byte(data))
		require.Error(t, err, data)
	}
	snapshot, err := parseDispatchCoordinatorSnapshot([]byte(`{"version":2,"sha":null,"transactionLog":"","worker":null}`))
	require.NoError(t, err)
	assert.Empty(t, snapshot.Transactions)
}

func TestDispatchCoordinatorFinishIntentValidation(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		`not json`, `null`, `{"outcome":"invalid"}`, `{"outcome":"completed","extra":"x"}`,
		"{\"outcome\":\"completed\"}\n{\"outcome\":\"cancelled\"}\n",
	} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, dispatchCoordinatorFinishFile), []byte(data), 0o600))
		_, err := extractDispatchCoordinatorReport(dir)
		require.Error(t, err, data)
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, dispatchCoordinatorFinishFile),
		[]byte("\n{\"outcome\":\"cancelled\"}\n{\"outcome\":\"cancelled\"}\n"), 0o600))
	report, err := extractDispatchCoordinatorReport(dir)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", report.FinishIntent)
}

func TestDispatchCoordinatorReportPropagation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeDispatchCoordinatorFixture(t, dir)
	result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 42, LogsPath: dir}}}
	require.True(t, backfillDispatchCoordinatorReport(&result.DispatchCoordinator, dir))
	summary := newRunSummary(result, LogMetrics{}, nil, nil)
	require.NoError(t, saveRunSummary(dir, summary, false))
	loaded, ok := loadRunSummary(dir, false)
	require.True(t, ok)
	assert.Equal(t, result.DispatchCoordinator, loaded.DispatchCoordinator)
	processed := processedRunFromSummary(loaded, dir)
	assert.Equal(t, result.DispatchCoordinator, processed.DispatchCoordinator)
	processedFromLogs := buildProcessedRun(context.Background(), *result, false, false)
	assert.Equal(t, result.DispatchCoordinator, processedFromLogs.DispatchCoordinator)
	results := auditAnalysisResults{dispatchCoordinator: result.DispatchCoordinator}
	processedAudit := buildProcessedAuditRun(result.Run, results)
	assert.Equal(t, result.DispatchCoordinator, processedAudit.DispatchCoordinator)
	assert.Equal(t, result.DispatchCoordinator, buildAuditRunSummary(result.Run, processedAudit, results).DispatchCoordinator)
	audit, _ := buildLocalAuditData(processed, LogMetrics{}, nil)
	assert.Equal(t, result.DispatchCoordinator, audit.DispatchCoordinator)
	runData := newRunData(processed, runEngineInfo{}, SafeOutputChainMetrics{}, nil, "", 0)
	assert.Equal(t, result.DispatchCoordinator, runData.DispatchCoordinator)
	assert.Equal(t, result.DispatchCoordinator, processedRunFromCachedData(runData, &audit, dir).DispatchCoordinator)

	var output bytes.Buffer
	renderDispatchCoordinatorToWriter(&output, result.DispatchCoordinator)
	assert.Contains(t, output.String(), "worker: work=w claim=c")
	assert.Contains(t, output.String(), "requested, not a verified outcome")
	output.Reset()
	renderLogsCompactToWriter(&output, LogsData{Runs: []RunData{runData}})
	assert.Contains(t, output.String(), "[work-queue] run=42")
	jsonData, err := json.Marshal(runData)
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"work_queue":`)
	jsonData, err = json.Marshal(RunData{})
	require.NoError(t, err)
	assert.NotContains(t, string(jsonData), `"work_queue":`)
}

func TestDispatchCoordinatorCachedAuditRequestsMissingArtifacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: 42, RunAnalysis: RunAnalysis{
		Run: WorkflowRun{DatabaseID: 42, Status: "completed", LogsPath: dir},
	}}
	require.NoError(t, saveRunSummary(dir, summary, false))
	require.NoError(t, markArtifactDownloaded(dir, "usage"))
	filter := ResolveArtifactFilter([]string{"work-queue"})
	cfg := auditRunConfig{runID: 42, outputDir: dir, artifactFilter: filter}
	done, skipped, err := renderCachedAuditIfAvailable(context.Background(), cfg)
	require.NoError(t, err)
	assert.False(t, done, "a usage-only cached summary cannot satisfy a coordinator request")
	assert.False(t, skipped)
	require.NoError(t, markArtifactDownloaded(dir, "activation"))
	require.NoError(t, markArtifactDownloaded(dir, "agent"))
	done, _, err = renderCachedAuditIfAvailable(context.Background(), cfg)
	require.NoError(t, err)
	assert.False(t, done, "cached artifacts without workflow logs need a diagnostic download")
}

func TestDispatchCoordinatorBackfillsPartialReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeDispatchCoordinatorFixture(t, dir)
	report := &DispatchCoordinatorReport{Operations: []DispatchCoordinatorOperation{{Message: "cached operation"}}}
	assert.True(t, backfillDispatchCoordinatorReport(&report, dir))
	require.NotNil(t, report.Snapshot)
	assert.Equal(t, "completed", report.FinishIntent)
	assert.Equal(t, "cached operation", report.Operations[0].Message)
	assert.False(t, backfillDispatchCoordinatorReport(&report, dir))
}

func TestDispatchCoordinatorRefreshesCachedAuditReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: dir}
	report := &DispatchCoordinatorReport{
		Snapshot:     &DispatchCoordinatorSnapshot{Version: 2},
		FinishIntent: "completed",
		Operations:   []DispatchCoordinatorOperation{{Message: "Dispatch coordinator: worker completion verified"}},
	}
	require.NoError(t, writeAuditData(dir, AuditData{
		CacheSource:         auditCacheSourceFull,
		Overview:            OverviewData{RunID: run.DatabaseID, Status: run.Status, Conclusion: run.Conclusion},
		DispatchCoordinator: &DispatchCoordinatorReport{Snapshot: report.Snapshot},
	}))
	require.NoError(t, renderAuditReport(context.Background(), ProcessedRun{
		Run: run, DispatchCoordinator: report,
	}, LogMetrics{}, nil, AuditOptions{OutputDir: dir, Group: true}))
	cached, ok := loadCachedAuditData(dir, run, auditCacheSourceFull)
	require.True(t, ok)
	assert.Equal(t, report, cached.DispatchCoordinator)
}

func TestDownloadDispatchCoordinatorArtifacts(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "cached artifacts missing workflow logs"}[cached], func(t *testing.T) {
			dir, fixtures, bin := t.TempDir(), t.TempDir(), t.TempDir()
			writeDispatchCoordinatorFixture(t, fixtures)
			archivePath := filepath.Join(fixtures, "logs.zip")
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			file, err := writer.Create("safe_outputs/4_Reconcile dispatch work claim.txt")
			require.NoError(t, err)
			_, err = file.Write([]byte("2026-10-02T12:00:00Z Dispatch coordinator: worker completion verified\n"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			require.NoError(t, os.WriteFile(archivePath, archive.Bytes(), 0o600))
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$DISPATCH_TEST_ARGS"
if [ "$1" = "api" ]; then
  case "$2" in
    */logs) cat "$DISPATCH_TEST_FIXTURES/logs.zip"; exit 0 ;;
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
    cp "$DISPATCH_TEST_FIXTURES/dispatch-work-coordinator.snapshot.json" "$dir/"
    printf '%s' '{"engine_id":"copilot"}' > "$dir/aw_info.json" ;;
  *-agent)
    cp "$DISPATCH_TEST_FIXTURES/dispatch-work-coordinator.finish.jsonl" "$dir/"
    printf '%s' 'agent logs' > "$dir/agent-stdio.log" ;;
  *) exit 1 ;;
esac
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755))
			argsPath := filepath.Join(bin, "args")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("DISPATCH_TEST_ARGS", argsPath)
			t.Setenv("DISPATCH_TEST_FIXTURES", fixtures)
			filter := ResolveArtifactFilter([]string{"work-queue"})
			require.Equal(t, []string{"activation", "agent"}, filter)
			require.True(t, shouldDownloadWorkflowRunLogs(filter))
			if cached {
				writeDispatchCoordinatorFixture(t, dir)
				require.NoError(t, markArtifactDownloaded(dir, "abc123-activation"))
				require.NoError(t, markArtifactDownloaded(dir, "abc123-agent"))
			}
			require.NoError(t, downloadRunArtifacts(context.Background(), downloadArtifactsOptions{
				runID: 42, outputDir: dir, owner: "owner", repo: "repo", hostname: "github.example.com", artifactFilter: filter,
			}))
			report, err := extractDispatchCoordinatorReport(dir)
			require.NoError(t, err)
			require.NotNil(t, report.Snapshot)
			assert.Equal(t, "completed", report.FinishIntent)
			require.Len(t, report.Operations, 1)
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
