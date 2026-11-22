package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func episodeHopTestContext(hop, parent, caller string) *AwContext {
	return &AwContext{EpisodeID: "canonical", HopID: hop, ParentHopID: parent, RunID: caller, Repo: "org/repo", RootRunID: "42", RootRepo: "org/repo"}
}

func episodeHopTestReport(t *testing.T) LogsData {
	t.Helper()
	root := episodeHopTestContext("root", "", "42")
	child := episodeHopTestContext("reusable", "root", "42")
	run := WorkflowRun{DatabaseID: 42, Repository: "org/repo", WorkflowName: "Root"}
	infos := []*AwInfo{{WorkflowName: "Root", Context: root}, {WorkflowName: "Reusable", Context: child}, {WorkflowName: "Reusable", Context: child}}
	hops := collectRunEpisodeHops(ProcessedRun{Run: run, EpisodeContexts: infos}, root)
	require.Len(t, hops, 2, "mirrored metadata must not create additional hops")
	runs := []RunData{
		{RunID: 42, Repository: "org/repo", WorkflowName: "Root", AwContext: root, EpisodeHops: hops, TokenUsage: 100, AIC: new(float64(0.25)), AICCoverage: logsAICAvailable},
		{RunID: 43, Repository: "org/repo", WorkflowName: "Dispatched", AwContext: episodeHopTestContext("dispatched", "reusable", "42"), TokenUsage: 200, AIC: new(float64(0.75)), AICCoverage: logsAICAvailable},
	}
	episodes, edges := buildEpisodeData(runs, nil)
	return LogsData{Summary: LogsSummary{AICCoverage: logsAICAvailable}, Runs: runs, Episodes: episodes, Edges: edges}
}

func TestEpisodeHopsKeepSharedRunAccountingSeparate(t *testing.T) {
	logs := episodeHopTestReport(t)
	require.Len(t, logs.Episodes, 1)
	episode := logs.Episodes[0]
	require.Equal(t, 2, episode.TotalRuns)
	require.Equal(t, 300, episode.TotalTokens)
	require.InDelta(t, 1.0, episode.TotalAIC, 1e-9)
	view := episode.Explanation
	require.Len(t, view.Hops, 3)
	require.Len(t, view.RunCosts, 2)
	require.Len(t, view.RunEdges, 1)
	require.Len(t, view.Edges, 2)
	for _, edge := range view.Edges {
		require.Equal(t, "recorded", edge.Status)
	}
	require.Len(t, slices.DeleteFunc(slices.Clone(view.Hops), func(hop EpisodeHop) bool { return !hop.SharedRun }), 2)
	var tokens int
	var aic float64
	for _, cost := range view.RunCosts {
		tokens += cost.Tokens
		aic += cost.AIC
	}
	require.Equal(t, 300, tokens)
	require.InDelta(t, 1.0, aic, 1e-9)
	encoded, err := json.Marshal(view.Hops)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"aic"`)
	require.NotContains(t, string(encoded), `"tokens"`)
}

func TestEpisodeHopsExplainMissingAndConflictingEvidence(t *testing.T) {
	logs := episodeHopTestReport(t)
	run := logs.Runs[0]
	run.EpisodeHops = []EpisodeHop{
		{RunID: 42, Repository: "org/repo", HopID: "known"},
		{RunID: 42, Repository: "org/repo", HopID: "child", ParentHopID: "absent", CallerRunID: "41"},
		{RunID: 42, Repository: "org/repo", HopID: "conflict", ParentHopID: "known", CallerRunID: "999"},
	}
	view := explainEpisode(logs.Episodes[0], []RunData{run}, nil)
	require.Len(t, view.Edges, 2)
	require.Contains(t, view.Edges, EpisodeHopEdge{SourceHopID: "absent", TargetHopID: "child", TargetRunID: 42, Status: "missing", Reason: "missing_parent_hop"})
	require.Contains(t, view.Edges, EpisodeHopEdge{SourceHopID: "known", TargetHopID: "conflict", TargetRunID: 42, Status: "conflicting", Reason: "conflicting_parent_identity"})
	run.EpisodeHops, run.AwContext = nil, nil
	view = explainEpisode(logs.Episodes[0], []RunData{run}, nil)
	require.Empty(t, view.Hops)
	require.Contains(t, view.MissingEvidence, "run 42: missing hop context")
	hops := collectRunEpisodeHops(ProcessedRun{Run: WorkflowRun{DatabaseID: 42}}, &AwContext{EpisodeID: "canonical", RunID: "41"})
	require.Len(t, hops, 1)
	require.Empty(t, hops[0].HopID, "missing recorded IDs must not be synthesized")
	require.Contains(t, hops[0].MissingEvidence, "missing_hop_id")
	require.Contains(t, hops[0].MissingEvidence, "missing_parent_hop_id")
}

func TestEpisodeHopsReadCurrentNormalArtifactMetadata(t *testing.T) {
	dir := t.TempDir()
	for name, info := range map[string]*AwInfo{
		"one-info":  {WorkflowName: "Root", RunAttempt: "2", Context: episodeHopTestContext("root", "", "42")},
		"two-usage": {WorkflowName: "Reusable", RunAttempt: "2", Context: episodeHopTestContext("reusable", "root", "42")},
		"old-info":  {WorkflowName: "Old", RunAttempt: "1", Context: episodeHopTestContext("old", "", "42")},
		"agent":     {WorkflowName: "Untrusted", Context: episodeHopTestContext("agent", "", "42")},
	} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o700))
		encoded, err := json.Marshal(info)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name, "aw_info.json"), encoded, 0o600))
	}
	pr := ProcessedRun{Run: WorkflowRun{DatabaseID: 42, Repository: "org/repo", WorkflowName: "Root", Attempt: 2, LogsPath: dir}}
	hops := collectRunEpisodeHops(pr, episodeHopTestContext("root", "", "42"))
	require.Len(t, hops, 2)
	require.True(t, slices.ContainsFunc(hops, func(hop EpisodeHop) bool { return hop.HopID == "reusable" }))
	require.False(t, slices.ContainsFunc(hops, func(hop EpisodeHop) bool { return hop.HopID == "old" || hop.HopID == "agent" }))
}

func TestEpisodeHopsViewUsesStandardJSON(t *testing.T) {
	logs := compactLogsData(episodeHopTestReport(t))
	require.Nil(t, logs.Runs[0].AwContext)
	var console bytes.Buffer
	require.NoError(t, renderEpisodeView(&console, logs, "hops", false))
	require.Contains(t, console.String(), "Hop reusable: run 42")
	require.Contains(t, console.String(), "[shared run]")
	require.Contains(t, console.String(), "Hop edge root -> reusable: recorded")
	require.Contains(t, console.String(), "Run-level accounting")
	require.Contains(t, console.String(), "complete membership is not established")
	require.ErrorIs(t, renderEpisodeView(episodeFailWriter{}, logs, "hops", false), io.ErrClosedPipe)
	require.ErrorIs(t, renderEpisodeView(episodeFailWriter{}, logs, "hops", true), io.ErrClosedPipe)
	require.ErrorContains(t, renderEpisodeView(io.Discard, logs, "other", true), "invalid --view")
	var output bytes.Buffer
	require.NoError(t, renderEpisodeView(&output, logs, "hops", true))
	var data LogsData
	require.NoError(t, json.Unmarshal(output.Bytes(), &data))
	require.Len(t, data.Episodes, 1)
	require.Len(t, data.Episodes[0].Explanation.Hops, 3)
	require.Len(t, data.Episodes[0].Explanation.RunCosts, 2)
	schemaJSON, err := GenerateNamedOutputSchema("logs")
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(output.Bytes(), &value))
	require.NoError(t, resolved.Validate(value))
}

func TestEpisodeHopsBoundedRetrievalPreservesReusableInvocations(t *testing.T) {
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "org/one", 2)
	root := &AwContext{EpisodeID: "canonical", HopID: "root", RunID: "2", Repo: "org/one"}
	child := &AwContext{EpisodeID: "canonical", HopID: "reusable", ParentHopID: "root", RunID: "2", Repo: "org/one"}
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		switch {
		case strings.Contains(endpoint, "/artifacts?"):
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{{ID: 20, Name: "a-usage"}, {ID: 21, Name: "b-usage"}}})
		case strings.HasSuffix(endpoint, "/20/zip"):
			return episodeCacheRegressionArchive(t, AwInfo{WorkflowName: "Root", RunID: 2, RunAttempt: "1", Context: root}, 10), nil
		case strings.HasSuffix(endpoint, "/21/zip"):
			return episodeCacheRegressionArchive(t, AwInfo{WorkflowName: "Reusable", RunID: 2, RunAttempt: "1", Context: child}, 30), nil
		}
		return fixture.request(ctx, host, endpoint)
	}
	data, err := retrieveEpisodeLogs(t.Context(), episodeRetrievalTestOptions(t), request)
	require.NoError(t, err)
	require.Len(t, data.Episodes, 1)
	view := data.Episodes[0].Explanation
	require.Len(t, view.Hops, 2)
	require.Len(t, view.Edges, 1)
	require.Equal(t, "recorded", view.Edges[0].Status)
	require.Equal(t, view.Edges[0].SourceRunID, view.Edges[0].TargetRunID)
	require.Len(t, view.RunCosts, 1)
	require.Equal(t, 44, view.RunCosts[0].Tokens)
	require.InDelta(t, 0.5, view.RunCosts[0].AIC, 1e-9)
	require.InDelta(t, 0.5, data.Episodes[0].TotalAIC, 1e-9)
}
