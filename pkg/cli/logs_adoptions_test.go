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

func TestLogsAdoptionSubcommands(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	for _, subcommand := range cmd.Commands() {
		assert.NotEqual(t, "adoptions-history", subcommand.Name())
		assert.NotEqual(t, "adoptions-episodes", subcommand.Name())
	}
	assert.NotContains(t, cmd.UsageString(), "adoptions-history")
	assert.NotContains(t, cmd.UsageString(), "adoptions-episodes")

	episode, _, err := cmd.Find([]string{"episode"})
	require.NoError(t, err)
	assert.Equal(t, "episode", episode.Name())
}

func TestLogsAdoptionOptions(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("episodes", "true"))
	require.NoError(t, cmd.Flags().Set("adoptions", "true"))
	require.NoError(t, cmd.Flags().Set("adoptions-dir", "observations"))
	options, err := loadCommonLogsOptions(cmd)
	require.NoError(t, err)
	assert.True(t, options.Episodes)
	assert.True(t, options.Adoptions)
	assert.Equal(t, "observations", options.AdoptionsDir)
	assert.Equal(t, []string{"usage", "adoptions"}, options.ArtifactSets)
	assert.Equal(t, []string{"usage", "safe-outputs-items", "agent-output-fallback"}, ResolveArtifactFilter(options.ArtifactSets))
	assert.False(t, shouldDownloadWorkflowRunLogs(ResolveArtifactFilter(options.ArtifactSets)))
	assert.False(t, cachedJSONLCanSatisfy(ResolveArtifactFilter(options.ArtifactSets), false, false, false))
	stdin, err := loadStdinLogsOptions(cmd)
	require.NoError(t, err)
	assert.True(t, stdin.Episodes)
	assert.True(t, stdin.Adoptions)
	assert.Equal(t, options.AdoptionsDir, stdin.AdoptionsDir)
	assert.Equal(t, options.ArtifactSets, stdin.ArtifactSets)

	cmd = NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("adoptions-dir", "observations"))
	_, err = loadCommonLogsOptions(cmd)
	require.ErrorContains(t, err, "--adoptions-dir requires --adoptions")

	episode := newLogsEpisodeCommand()
	assert.NotNil(t, episode.Flags().Lookup("adoptions"))
	assert.NotNil(t, episode.Flags().Lookup("adoptions-dir"))
}

func TestLogsAdoptionArtifactSets(t *testing.T) {
	t.Parallel()
	sets := []string{"usage"}
	assert.Equal(t, []string{"usage", "adoptions"}, logsAdoptionArtifactSets(sets, true))
	assert.Equal(t, []string{"usage"}, sets, "do not mutate the caller's artifact selection")
	assert.Nil(t, logsAdoptionArtifactSets(nil, true), "unfiltered downloads already include manifests")
	assert.Equal(t, []string{"all"}, logsAdoptionArtifactSets([]string{"all"}, true))
	assert.Equal(t, []string{"usage"}, logsAdoptionArtifactSets(sets, false))
	assert.Equal(t, []string{"usage", "adoptions"}, normalizeLogsDownloadOptions(LogsDownloadOptions{Adoptions: true, ArtifactSets: sets}).ArtifactSets)
	assert.Equal(t, []string{"usage", "adoptions"}, normalizeStdinLogsOptions(StdinLogsOptions{Adoptions: true, ArtifactSets: sets}).ArtifactSets)
}

func TestLogsAdoptionRepository(t *testing.T) {
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
		assert.Equal(t, tc.want, logsAdoptionRepository(WorkflowRun{URL: tc.url, Repository: tc.repo}))
	}
}

func TestLogsAdoptionManifestEvidence(t *testing.T) {
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
		{name: "valid", manifest: "{\"type\":\"noop\"}\n", found: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.manifest != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(tc.manifest), 0o600))
			}
			run := ProcessedRun{Run: WorkflowRun{DatabaseID: 42, Repository: "acme/repo", LogsPath: dir}}
			items, found, err := readLogsAdoptionItems(run)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.found, found)
			report, err := collectRunAdoptions(context.Background(), run, nil, false)
			require.NoError(t, err)
			assert.Empty(t, report.Items)
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

func TestLogsAdoptionPersistenceErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.ErrorContains(t, writeAdoptionJSONL(path, 42, nil), "create adoptions directory")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := collectLogsAdoptions(ctx, []ProcessedRun{{Run: WorkflowRun{DatabaseID: 42}}}, "", false)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLogsAdoptionReportAndEpisodeJoin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	usageDir := filepath.Join(dir, "run-1", "usage")
	require.NoError(t, os.MkdirAll(usageDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(usageDir, "agent_usage.jsonl"), []byte("{\"aic\":2.5}\n"), 0o600))
	run := ProcessedRun{
		Run:       WorkflowRun{DatabaseID: 1, Repository: "org/repo", WorkflowName: "producer", LogsPath: filepath.Dir(usageDir)},
		AwContext: &AwContext{EpisodeID: "ep-1"},
	}
	item := episodeObservation(1, "item-1", AdoptionStatusAccepted).AdoptionReport
	reports := []AdoptionsData{{RunID: 1, Workflow: "producer", Items: []AdoptionReport{item}, Summary: ComputeAdoptionSummary([]AdoptionReport{item}, nil)}}
	data, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{outputDir: dir, summaryFile: "summary.json", adoptionReports: reports})
	require.NoError(t, err)
	require.Len(t, data.Adoptions, 1)
	assert.Equal(t, 1, data.Summary.AdoptionTotal)
	assert.Equal(t, 1, data.Summary.AdoptionAccepted)
	assert.Equal(t, 0, data.Summary.AdoptionLifecycle)
	assert.Equal(t, 0, data.Summary.AdoptionUnknown)
	assert.InDelta(t, 1.0, data.Summary.AdoptionAcceptanceRate, 1e-9)
	require.NotNil(t, data.EpisodeAdoptions)
	require.Len(t, data.EpisodeAdoptions.Episodes, 1)
	assert.Equal(t, 1, data.EpisodeAdoptions.Episodes[0].Summary.Accepted)
	assert.InDelta(t, 2.5, *data.EpisodeAdoptions.Episodes[0].ObservedAIC, 1e-9)

	for _, verbose := range []bool{false, true} {
		var output bytes.Buffer
		require.NoError(t, renderLogsJSONToWriter(&output, data, verbose))
		var decoded LogsData
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
		assert.Equal(t, data.Adoptions, decoded.Adoptions)
		require.NotNil(t, decoded.EpisodeAdoptions)
		assert.Equal(t, 1, decoded.EpisodeAdoptions.Episodes[0].Summary.Accepted)
	}
	var rendered bytes.Buffer
	require.NoError(t, renderLogsAdoptions(&rendered, data))
	assert.Contains(t, rendered.String(), "1 accepted")
	saved, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	require.NoError(t, err)
	assert.Contains(t, string(saved), "\"adoptions\"")
	assert.Contains(t, string(saved), "\"episode_adoptions\"")
	without, err := prepareLogsData([]ProcessedRun{run}, renderLogsOutputOptions{outputDir: dir})
	require.NoError(t, err)
	assert.Nil(t, without.Adoptions)
	assert.Nil(t, without.EpisodeAdoptions)

	reportPath := filepath.Join(dir, "report.md")
	require.NoError(t, renderLogsOutput([]ProcessedRun{run}, renderLogsOutputOptions{
		outputDir: dir, format: "markdown", reportFile: reportPath, adoptionReports: reports,
	}))
	markdown, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	assert.Contains(t, string(markdown), "Adoptions for producer")
	assert.Contains(t, string(markdown), "1 accepted")
}

func TestAttachLogsAdoptionsPopulatesAllSummaryStates(t *testing.T) {
	t.Parallel()
	statuses := []AdoptionStatus{
		AdoptionStatusAccepted,
		AdoptionStatusRejected,
		AdoptionStatusPending,
		AdoptionStatusLifecycle,
		AdoptionStatusNotApplicable,
		AdoptionStatusUnknown,
	}
	items := make([]AdoptionReport, 0, len(statuses))
	for _, status := range statuses {
		items = append(items, AdoptionReport{
			Type:               "test",
			AdoptionEvaluation: normalizeAdoptionStatus(status),
		})
	}
	data := LogsData{}
	require.NoError(t, attachLogsAdoptions(&data, []AdoptionsData{{RunID: 1, Items: items}}))
	assert.Equal(t, len(statuses), data.Summary.AdoptionTotal)
	assert.Equal(t, 1, data.Summary.AdoptionAccepted)
	assert.Equal(t, 1, data.Summary.AdoptionRejected)
	assert.Equal(t, 1, data.Summary.AdoptionPending)
	assert.Equal(t, 1, data.Summary.AdoptionLifecycle)
	assert.Equal(t, 1, data.Summary.AdoptionNotApplicable)
	assert.Equal(t, 1, data.Summary.AdoptionUnknown)
}

func TestEpisodeAdoptionsCollectManifestsOnlyForMembers(t *testing.T) {
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
	options.Adoptions = true
	data, err := retrieveCachedEpisodeLogs(context.Background(), options, request)
	require.NoError(t, err)
	require.Len(t, data.Runs, 1)
	assert.Equal(t, []string{"repos/org/one/actions/artifacts/21/zip"}, downloads)
	items, found, err := readLogsAdoptionItems(ProcessedRun{Run: WorkflowRun{LogsPath: data.Runs[0].LogsPath}})
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

func TestEpisodeAdoptionsRejectUnverifiedAttemptManifest(t *testing.T) {
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
	assert.Equal(t, "adoption_attempt_unverified", collector.report.Issues[0].Reason)
	for _, call := range fixture.calls {
		assert.NotContains(t, call, "/21/zip")
	}
}
