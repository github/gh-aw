//go:build !integration

package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsOutcomeSubcommands(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	for _, subcommand := range cmd.Commands() {
		assert.NotEqual(t, "adoption-history", subcommand.Name())
		assert.NotEqual(t, "adoption-episodes", subcommand.Name())
	}
	assert.NotContains(t, cmd.UsageString(), "adoption-history")
	assert.NotContains(t, cmd.UsageString(), "adoption-episodes")

	episode, _, err := cmd.Find([]string{"episode"})
	require.NoError(t, err)
	assert.Equal(t, "episode", episode.Name())
}

func TestLogsOutcomeOptions(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("episodes", "true"))
	require.NoError(t, cmd.Flags().Set("adoption", "true"))
	require.NoError(t, cmd.Flags().Set("adoption-dir", "observations"))
	options, err := loadCommonLogsOptions(cmd)
	require.NoError(t, err)
	assert.True(t, options.Episodes)
	assert.True(t, options.Outcomes)
	assert.Equal(t, "observations", options.OutcomesDir)
	assert.Equal(t, []string{"usage", "delivery"}, options.ArtifactSets)
	assert.Equal(t, []string{"usage", "safe-outputs-items", "agent-output-fallback"}, ResolveArtifactFilter(options.ArtifactSets))
	assert.False(t, shouldDownloadWorkflowRunLogs(ResolveArtifactFilter(options.ArtifactSets)))
	assert.False(t, cachedJSONLCanSatisfy(ResolveArtifactFilter(options.ArtifactSets), false, false, false))
	stdin, err := loadStdinLogsOptions(cmd)
	require.NoError(t, err)
	assert.True(t, stdin.Episodes)
	assert.True(t, stdin.Outcomes)
	assert.Equal(t, options.OutcomesDir, stdin.OutcomesDir)
	assert.Equal(t, options.ArtifactSets, stdin.ArtifactSets)

	cmd = NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("adoption-dir", "observations"))
	_, err = loadCommonLogsOptions(cmd)
	require.ErrorContains(t, err, "--adoption-dir requires --adoption")

	episode := newLogsEpisodeCommand()
	assert.NotNil(t, episode.Flags().Lookup("adoption"))
	assert.NotNil(t, episode.Flags().Lookup("adoption-dir"))
}

func TestLogsOutcomeArtifactSets(t *testing.T) {
	t.Parallel()
	sets := []string{"usage"}
	assert.Equal(t, []string{"usage", "delivery"}, logsOutcomeArtifactSets(sets, true))
	assert.Equal(t, []string{"usage"}, sets, "do not mutate the caller's artifact selection")
	assert.Nil(t, logsOutcomeArtifactSets(nil, true), "unfiltered downloads already include manifests")
	assert.Equal(t, []string{"all"}, logsOutcomeArtifactSets([]string{"all"}, true))
	assert.Equal(t, []string{"usage"}, logsOutcomeArtifactSets(sets, false))
	assert.Equal(t, []string{"usage", "delivery"}, normalizeLogsDownloadOptions(LogsDownloadOptions{Outcomes: true, ArtifactSets: sets}).ArtifactSets)
	assert.Equal(t, []string{"usage", "delivery"}, normalizeStdinLogsOptions(StdinLogsOptions{Outcomes: true, ArtifactSets: sets}).ArtifactSets)
}

func TestLogsOutcomeRepository(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url  string
		repo string
		want string
	}{
		{repo: "org/repo", want: "org/repo"},
		{url: "https://github.com/org/repo/actions/runs/1", repo: "org/repo", want: "org/repo"},
		{url: "https://github.example.com/org/repo/actions/runs/1", repo: "org/repo", want: "github.example.com/org/repo"},
		{url: "https://github.example.com/org/repo/actions/runs/1", repo: "github.example.com/org/repo", want: "github.example.com/org/repo"},
	} {
		assert.Equal(t, tc.want, logsOutcomeRepository(WorkflowRun{URL: tc.url, Repository: tc.repo}))
	}
}

func TestLogsOutcomeManifestEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		manifest string
		wantErr  string
		found    bool
	}{
		{name: "missing"},
		{name: "empty", manifest: "\n", found: true},
		{name: "corrupt", manifest: "{", wantErr: "unexpected EOF"},
		{name: "missing type", manifest: "{}", wantErr: "no type"},
		{name: "valid", manifest: "{\"type\":\"noop\",\"timestamp\":\"2026-10-01T00:00:00Z\"}\n", found: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.manifest != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(tc.manifest), 0o600))
			}
			run := ProcessedRun{Run: WorkflowRun{Attempt: 1, DatabaseID: 42, Repository: "acme/repo", LogsPath: dir}}
			items, found, err := readLogsOutcomeItems(run)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.found, found)
			report, err := collectRunOutcomes(context.Background(), run, nil, false)
			require.NoError(t, err)
			if len(items) == 0 {
				assert.Empty(t, report.Items)
			} else {
				require.Len(t, report.Items, 1)
				assert.Equal(t, OutcomeStatusNotApplicable, report.Items[0].OutcomeStatus)
			}
			if tc.found {
				assert.Empty(t, report.EvalError)
			} else {
				assert.Contains(t, report.EvalError, "unavailable")
			}
			if len(items) > 0 {
				assert.Equal(t, "noop", items[0].Type)
			}
		})
	}
}

func TestLogsOutcomePersistenceErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.ErrorContains(t, writeOutcomeJSONL(path, 42, nil), "create adoption directory")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := collectLogsLifecycles(ctx, []ProcessedRun{{Run: WorkflowRun{Attempt: 1, DatabaseID: 42}}}, "", false, true)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLogsOutcomeRunEvidenceErrorsDoNotAbortBatch(t *testing.T) {
	manifest := `{"type":"noop","timestamp":"2026-11-01T00:00:00Z"}` + "\n"
	identity := func(runID int64) string {
		return unifiedDetectionTestHeader + fmt.Sprintf(`{"type":"workflow.info","provenance":{"component":"workflow"},"data":{"runId":%d,"runAttempt":1,"repository":"acme/repo","serverUrl":"https://github.com"}}`, runID) + "\n"
	}
	wantErrors := []AdoptionErrorData{
		{RunID: 2, Reason: "lifecycle_identity_missing", Error: "lifecycle session is missing its format or execution identity"},
		{RunID: 3, Reason: "lifecycle_evidence_invalid", Error: "lifecycle session run identity does not match selected run"},
	}
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprintf("live=%t", live), func(t *testing.T) {
			root := t.TempDir()
			var runs []ProcessedRun
			for _, fixture := range []struct {
				id      int64
				session string
			}{
				{id: 1},
				{id: 2, session: unifiedDetectionTestHeader},
				{id: 3, session: identity(99)},
				{id: 4, session: identity(4)},
			} {
				dir := filepath.Join(root, fmt.Sprintf("run-%d", fixture.id))
				require.NoError(t, os.Mkdir(dir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))
				if fixture.session != "" {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_session.jsonl"), []byte(fixture.session), 0o600))
				}
				runs = append(runs, ProcessedRun{Run: WorkflowRun{
					DatabaseID: fixture.id, Attempt: 1, WorkflowName: "producer",
					WorkflowPath: ".github/workflows/test.lock.yml", Repository: "acme/repo", LogsPath: dir,
				}})
			}

			_, err := collectRunLifecycle(t.Context(), runs[1], nil, false, live)
			require.ErrorIs(t, err, errLifecycleIdentityMissing, "single-run callers still receive the evidence error")

			var reports []OutcomesData
			adoptionDir := ""
			if live {
				adoptionDir = t.TempDir()
			}
			_, stderr := captureOutput(t, func() error {
				reports, err = collectLogsLifecycles(t.Context(), runs, adoptionDir, false, live)
				return nil
			})
			require.NoError(t, err, "one run's unusable evidence must not abort the batch")
			require.Len(t, reports, len(runs))
			assert.Contains(t, stderr, "Run 2: lifecycle session is missing its format or execution identity")
			assert.Contains(t, stderr, "Run 3: lifecycle session run identity does not match selected run")
			if live {
				for runID, written := range map[int64]bool{1: true, 2: false, 3: false, 4: true} {
					_, statErr := os.Stat(filepath.Join(adoptionDir, fmt.Sprintf("adoption-%d.jsonl", runID)))
					assert.Equal(t, written, statErr == nil, "adoption JSONL for run %d", runID)
				}
			}
			for _, index := range []int{0, 3} {
				assert.Equal(t, "complete", reports[index].Coverage)
				assert.Equal(t, "complete", reports[index].Delivery.Coverage)
				assert.Equal(t, 1, reports[index].Delivery.Delivered)
				assert.Nil(t, reports[index].AdoptionError)
			}
			for i, want := range wantErrors {
				report := reports[i+1]
				require.NotNil(t, report.AdoptionError)
				assert.Equal(t, want, *report.AdoptionError)
				assert.Equal(t, "unavailable", report.Coverage)
				assert.Equal(t, want.Error, report.EvalError)
				assert.Equal(t, "unavailable", report.Delivery.Coverage)
				assert.Equal(t, want.Error, report.Delivery.Error)
				assert.Empty(t, report.Delivery.Items)
				assert.Empty(t, report.Items)
			}

			data, err := prepareLogsData(runs, renderLogsOutputOptions{outputDir: root, outcomeReports: reports})
			require.NoError(t, err)
			assert.Equal(t, wantErrors, data.AdoptionErrors)
			var output bytes.Buffer
			require.NoError(t, renderLogsJSONToWriter(&output, data, false))
			var decoded LogsData
			require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
			assert.Len(t, decoded.Runs, len(runs), "failed runs stay in runs[]")
			assert.Len(t, decoded.Delivery, len(runs))
			assert.Equal(t, wantErrors, decoded.AdoptionErrors)
			if live {
				require.Len(t, decoded.Outcomes, len(runs))
				assert.Equal(t, "unavailable", decoded.Outcomes[1].Coverage)
				assert.Equal(t, wantErrors[0].Error, decoded.Outcomes[1].EvalError)
			} else {
				assert.Empty(t, decoded.Outcomes)
			}

			stdout, _ := captureOutput(t, func() error {
				return renderLogsOutputCrossRun(runs, data, renderLogsOutputOptions{jsonOutput: true})
			})
			var crossRun CrossRunAuditReport
			require.NoError(t, json.Unmarshal([]byte(stdout), &crossRun))
			assert.Equal(t, wantErrors, crossRun.AdoptionErrors)
		})
	}
}

func TestLogsOutcomeReportAndEpisodeJoin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	usageDir := filepath.Join(dir, "run-1", "usage")
	require.NoError(t, os.MkdirAll(usageDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(usageDir, "agent_usage.jsonl"), []byte("{\"aic\":2.5}\n"), 0o600))
	run := ProcessedRun{
		Run:       WorkflowRun{Attempt: 1, DatabaseID: 1, Repository: "org/repo", WorkflowName: "producer", LogsPath: filepath.Dir(usageDir)},
		AwContext: &AwContext{EpisodeID: "ep-1"},
	}
	item := episodeObservation(1, "item-1", OutcomeStatusAccepted).OutcomeReport
	reports := []OutcomesData{{RunID: 1, Workflow: "producer", Items: []OutcomeReport{item}, Summary: ComputeOutcomeSummary([]OutcomeReport{item}, nil)}}
	data, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{outputDir: dir, summaryFile: "summary.json", outcomeReports: reports})
	require.NoError(t, err)
	require.Len(t, data.Outcomes, 1)
	require.NotNil(t, data.EpisodeOutcomes)
	require.Len(t, data.EpisodeOutcomes.Episodes, 1)
	assert.Equal(t, 1, data.EpisodeOutcomes.Episodes[0].Summary.Accepted)
	assert.InDelta(t, 2.5, *data.EpisodeOutcomes.Episodes[0].ObservedAIC, 1e-9)

	for _, verbose := range []bool{false, true} {
		var output bytes.Buffer
		require.NoError(t, renderLogsJSONToWriter(&output, data, verbose))
		var decoded LogsData
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
		assert.Equal(t, data.Outcomes, decoded.Outcomes)
		require.NotNil(t, decoded.EpisodeOutcomes)
		assert.Equal(t, 1, decoded.EpisodeOutcomes.Episodes[0].Summary.Accepted)
	}
	var rendered bytes.Buffer
	require.NoError(t, renderLogsOutcomes(&rendered, data))
	assert.Contains(t, rendered.String(), "1 accepted")
	saved, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	require.NoError(t, err)
	assert.Contains(t, string(saved), "\"adoption\"")
	assert.Contains(t, string(saved), "\"episode_adoption\"")
	without, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{outputDir: dir})
	require.NoError(t, err)
	assert.Nil(t, without.Outcomes)
	assert.Nil(t, without.EpisodeOutcomes)

	reportPath := filepath.Join(dir, "report.md")
	require.NoError(t, renderLogsOutput([]ProcessedRun{run}, renderLogsOutputOptions{
		outputDir: dir, format: "markdown", reportFile: reportPath, outcomeReports: reports,
	}))
	markdown, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	assert.Contains(t, string(markdown), "Adoption for producer")
	assert.Contains(t, string(markdown), "1 accepted")
}

func TestEpisodeOutcomesCollectManifestsOnlyForMembers(t *testing.T) {
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "", 0)
	fixture.add("org/one", 3, "unrelated", "", 0)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	file, err := writer.Create(safeOutputItemsManifestFilename)
	require.NoError(t, err)
	_, err = fmt.Fprintln(file, `{"type":"noop"}`)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	var downloads []string
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		for _, id := range []int64{2, 3} {
			if endpoint == fmt.Sprintf("repos/org/one/actions/runs/%d/artifacts?per_page=100&page=1", id) {
				return json.Marshal(map[string]any{"artifacts": []episodeArtifact{
					{ID: id * 10, Name: "usage"},
					{ID: id*10 + 1, Name: "safe-outputs-items"},
				}})
			}
		}
		if strings.HasSuffix(endpoint, "/21/zip") || strings.HasSuffix(endpoint, "/31/zip") {
			downloads = append(downloads, endpoint)
			return archive.Bytes(), nil
		}
		return fixture.request(ctx, host, endpoint)
	}
	options := episodeRetrievalTestOptions(t)
	options.Outcomes = true
	data, err := retrieveCachedEpisodeLogs(context.Background(), options, request)
	require.NoError(t, err)
	require.Len(t, data.Runs, 1)
	assert.Equal(t, []string{"repos/org/one/actions/artifacts/21/zip"}, downloads)
	items, found, err := readLogsOutcomeItems(ProcessedRun{Run: WorkflowRun{LogsPath: data.Runs[0].LogsPath}})
	require.NoError(t, err)
	assert.True(t, found)
	require.Len(t, items, 1)
	assert.Equal(t, "noop", items[0].Type)
	cached, err := retrieveCachedEpisodeLogs(context.Background(), options, func(context.Context, string, string) ([]byte, error) {
		t.Fatal("cached membership must not make API requests")
		return nil, nil
	})
	require.NoError(t, err)
	assert.True(t, cached.EpisodeRetrieval.Cached)
}

func TestEpisodeOutcomesRejectUnverifiedAttemptManifest(t *testing.T) {
	options := episodeRetrievalTestOptions(t)
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "", 0)
	collector := &episodeCollector{opts: options, request: fixture.request}
	require.NoError(t, collector.initializeScope())
	run := fixture.runs["org/one/2"].workflowRun()
	run.Attempt = 2
	fixture.archives[20] = fixture.archiveWithAttempt(2, 2, "attempt-2", 0.25, "")
	infos, dirs, err := collector.loadCompactArtifacts(context.Background(), run, []episodeArtifact{
		{ID: 20, Name: "usage"},
		{ID: 21, Name: "safe-outputs-items"},
	}, options.OutputDir)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Len(t, dirs, 1)
	require.Len(t, collector.report.Issues, 1)
	assert.Equal(t, "outcome_attempt_unverified", collector.report.Issues[0].Reason)
	for _, call := range fixture.calls {
		assert.NotContains(t, call, "/21/zip")
	}
}
