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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adoptionEvidenceArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	return adoptionEvidenceFilesArchive(t, map[string]string{name: content})
}

func adoptionEvidenceFilesArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range files {
		file, err := writer.Create(name)
		require.NoError(t, err)
		_, err = fmt.Fprint(file, content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return archive.Bytes()
}

func installAssignmentAdoptionFixture(t *testing.T) string {
	t.Helper()
	return installAdoptionAPIFixture(t, `case "$2" in
repos/org/one/issues/100) printf '%s' '{"state":"open","labels":[]}' ;;
repos/org/one/issues/100/timeline) printf '%s' '[[{"event":"cross-referenced","created_at":"2026-10-02T00:00:00Z","source":{"issue":{"number":200,"repository_url":"https://api.github.com/repos/org/one","user":{"login":"copilot-swe-agent"},"pull_request":{}}}}]]' ;;
repos/org/one/pulls/200) printf '%s' '{"state":"closed","merged":true,"labels":[]}' ;;
*) printf 'unexpected endpoint: %s\n' "$2" >&2; exit 1 ;;
esac
`)
}

func TestLogsAdoptionDefaultArtifactsRecoverAssignment(t *testing.T) {
	calls := installAssignmentAdoptionFixture(t)
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "safe-outputs-items")
	fallbackDir := filepath.Join(dir, "agent-output-fallback", "gh-aw")
	require.NoError(t, os.MkdirAll(manifestDir, 0o700))
	require.NoError(t, os.MkdirAll(fallbackDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, safeOutputItemsManifestFilename),
		[]byte("{\"type\":\"assign_to_agent\",\"timestamp\":\"2026-10-01T00:00:00Z\"}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, "safeoutputs.jsonl"),
		[]byte("{\"type\":\"assign_to_agent\",\"issue_number\":100}\n"), 0o600))
	report, err := collectRunAdoptions(withAdoptionAPICache(context.Background()),
		ProcessedRun{Run: WorkflowRun{DatabaseID: 2, Repository: "org/one", LogsPath: dir}}, nil, false)
	require.NoError(t, err)
	require.Len(t, report.Items, 1)
	assert.Equal(t, AdoptionStatusAccepted, report.Items[0].AdoptionStatus)
	assert.Equal(t, 100, report.Items[0].ObjectNumber)
	assert.Contains(t, adoptionAPICalls(t, calls), "repos/org/one/pulls/200")
}

func TestEpisodeAdoptionCompactAssignmentEvidence(t *testing.T) {
	calls := installAssignmentAdoptionFixture(t)
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "", 0)
	manifest := adoptionEvidenceArchive(t, safeOutputItemsManifestFilename,
		"{\"type\":\"assign_to_agent\",\"timestamp\":\"2026-10-01T00:00:00Z\"}\n")
	raw := adoptionEvidenceFilesArchive(t, map[string]string{
		"gh-aw/safeoutputs.jsonl": "{\"type\":\"assign_to_agent\",\"issue_number\":100}\n",
		"agent_usage.jsonl":       "{\"aic\":99}\n",
	})
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		switch endpoint {
		case "repos/org/one/actions/runs/2/artifacts?per_page=100&page=1":
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{
				{ID: 20, Name: "usage"}, {ID: 21, Name: "invocation-safe-outputs-items"},
				{ID: 22, Name: "invocation-agent-output-fallback"},
			}})
		case "repos/org/one/actions/artifacts/21/zip":
			return manifest, nil
		case "repos/org/one/actions/artifacts/22/zip":
			return raw, nil
		default:
			return fixture.request(ctx, host, endpoint)
		}
	}
	options := episodeRetrievalTestOptions(t)
	options.Adoptions = true
	data, err := retrieveCachedEpisodeLogs(context.Background(), options, request)
	require.NoError(t, err)
	require.Len(t, data.Runs, 1)
	require.NoError(t, collectEpisodeAdoptions(context.Background(), &data, "", false))
	require.Len(t, data.Adoptions, 1)
	require.Len(t, data.Adoptions[0].Items, 1)
	assert.Equal(t, AdoptionStatusAccepted, data.Adoptions[0].Items[0].AdoptionStatus)
	assert.Contains(t, adoptionAPICalls(t, calls), "repos/org/one/pulls/200")
	usage, err := analyzeTokenUsageAICOnly(data.Runs[0].LogsPath, false)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.InDelta(t, 0.25, usage.TotalAIC, 1e-9, "enrichment accounting mirrors must not inflate compact usage")
}

func writeAdoptionInvocationManifest(t *testing.T, parent, name, content string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".episode-complete"), []byte("v1\n"), 0o600))
	return filepath.Join("..", name)
}

func TestLogsAdoptionAggregatesInvocationManifests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, first, second string
		count               int
	}{
		{"distinct", `{"type":"unsupported_a"}`, `{"type":"unsupported_b"}`, 2},
		{"empty first", "", `{"type":"unsupported_b"}`, 1},
		{"identical invocations", `{"type":"unsupported_a"}`, `{"type":"unsupported_a"}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "collection-test")
			require.NoError(t, os.Mkdir(dir, 0o700))
			inventory := []string{
				writeAdoptionInvocationManifest(t, parent, "21-adoptions", tc.first),
				writeAdoptionInvocationManifest(t, parent, "22-adoptions", tc.second),
			}
			encoded, err := json.Marshal(inventory)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".episode-artifacts.json"), encoded, 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(tc.first), 0o600))
			report, err := collectRunAdoptions(context.Background(),
				ProcessedRun{Run: WorkflowRun{DatabaseID: 2, Repository: "org/one", LogsPath: dir}}, nil, false)
			require.NoError(t, err)
			require.Len(t, report.Items, tc.count)
			if tc.count == 2 {
				assert.NotEqual(t, report.Items[0].ItemID, report.Items[1].ItemID)
			}
		})
	}
}

func TestLogsAdoptionEnrichmentDoesNotCrossInvocations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, prefix := range []string{"a-", "b-"} {
		writeAdoptionInvocationManifest(t, dir, prefix+"safe-outputs-items", `{"type":"assign_to_agent"}`)
	}
	fallbackDir := filepath.Join(dir, "b-agent-output-fallback")
	require.NoError(t, os.Mkdir(fallbackDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, "safeoutputs.jsonl"),
		[]byte("{\"type\":\"assign_to_agent\",\"issue_number\":100}\n"), 0o600))
	items, found, err := readLogsAdoptionItems(ProcessedRun{Run: WorkflowRun{Repository: "org/one", LogsPath: dir}})
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, items, 2)
	assert.Zero(t, items[0].Number, "missing invocation evidence must not borrow another invocation's reference")
	assert.Equal(t, 100, items[1].Number)
}

func TestLogsAdoptionFlatManifestMirror(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := `{"type":"unsupported_a"}`
	writeAdoptionInvocationManifest(t, dir, "safe-outputs-items", manifest)
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))
	items, found, err := readLogsAdoptionItems(ProcessedRun{Run: WorkflowRun{Repository: "org/one", LogsPath: dir}})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Len(t, items, 1, "flattened and artifact-nested copies are the same invocation")
}

func TestEpisodeAdoptionEnrichmentRequiresVerifiedAttempt(t *testing.T) {
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "", 0)
	collector := &episodeCollector{opts: episodeRetrievalTestOptions(t), request: fixture.request}
	require.NoError(t, collector.initializeScope())
	run := fixture.runs["org/one/2"].workflowRun()
	run.Attempt = 2
	fixture.archives[20] = fixture.archiveWithAttempt(2, 2, "attempt-2", 0.25, "")
	_, dirs, err := collector.loadCompactArtifacts(context.Background(), run, []episodeArtifact{
		{ID: 20, Name: "usage"}, {ID: 22, Name: "agent-output-fallback"},
	}, collector.opts.OutputDir)
	require.NoError(t, err)
	assert.Len(t, dirs, 1)
	require.Len(t, collector.report.Issues, 1)
	assert.Equal(t, "adoption_attempt_unverified", collector.report.Issues[0].Reason)
	for _, call := range fixture.calls {
		assert.NotContains(t, call, "/22/zip")
	}
}

func TestLogsAdoptionEmptyManifestsEstablishEpisodeCoverage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, manifest string
		complete       bool
	}{
		{"empty", "\n", true},
		{"all skipped", "{\"type\":\"noop\"}\n", true},
		{"missing", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.manifest != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(tc.manifest), 0o600))
			}
			report, err := collectRunAdoptions(context.Background(),
				ProcessedRun{Run: WorkflowRun{DatabaseID: 2, Repository: "org/one", LogsPath: dir}}, nil, false)
			require.NoError(t, err)
			require.Empty(t, report.Items)
			accepted := episodeObservation(1, "stable-action", AdoptionStatusAccepted).AdoptionReport
			acceptedDir := t.TempDir()
			for _, runDir := range []string{dir, acceptedDir} {
				usageDir := filepath.Join(runDir, "usage")
				require.NoError(t, os.Mkdir(usageDir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(usageDir, "agent_usage.jsonl"), []byte("{\"aic\":1}\n"), 0o600))
			}
			data := LogsData{
				Runs:     []RunData{{RunID: 1, LogsPath: acceptedDir}, {RunID: 2, LogsPath: dir}},
				Episodes: []EpisodeData{{EpisodeID: "canonical", RunIDs: []int64{1, 2}, TotalRuns: 2}},
			}
			require.NoError(t, attachLogsAdoptions(&data, []AdoptionsData{
				{RunID: 1, Items: []AdoptionReport{accepted}}, report,
			}))
			assertEmptyManifestEpisodeCoverage(t, data, tc.complete)
		})
	}
}

func assertEmptyManifestEpisodeCoverage(t *testing.T, data LogsData, complete bool) {
	t.Helper()
	require.NotNil(t, data.EpisodeAdoptions)
	require.Len(t, data.EpisodeAdoptions.Episodes, 1)
	episode := data.EpisodeAdoptions.Episodes[0]
	if complete {
		assert.Equal(t, "complete", episode.AdoptionCoverage)
		assert.Equal(t, []int64{1, 2}, episode.AdoptionRunIDs)
		assert.Empty(t, episode.MissingAdoptionRunIDs)
		require.NotNil(t, episode.CostPerAcceptedAdoption)
		assert.InDelta(t, 2.0, *episode.CostPerAcceptedAdoption, 1e-9)
	} else {
		assert.Equal(t, "partial", episode.AdoptionCoverage)
		assert.Equal(t, []int64{2}, episode.MissingAdoptionRunIDs)
		assert.Nil(t, episode.CostPerAcceptedAdoption)
	}
}
