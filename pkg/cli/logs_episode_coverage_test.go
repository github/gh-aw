//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func episodeCoverageTestRuns() []RunData {
	var runs []RunData
	for i, id := range []int64{100, 200, 300} {
		parent := "100"
		if id == 300 {
			parent = "200"
		}
		runs = append(runs, RunData{
			RunID: id, Repository: "org/repo", WorkflowName: "worker",
			CreatedAt:  time.Date(2026, 10, 1, i, 0, 0, 0, time.UTC),
			TokenUsage: 10, AIC: new(0.1),
			AwContext: &AwContext{
				EpisodeID: "canonical", RunID: parent, Repo: "org/repo",
				RootRunID: "100", RootWorkflowID: "org/repo/.github/workflows/root.yml@main", RootRepo: "org/repo",
			},
		})
	}
	runs[0].WorkflowName = "root"
	return runs
}

func TestEpisodeCoverageSelectedLineage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		selected       []int
		representative int64
		observedRoot   bool
		status         string
		reasons        []string
		missingParents []int64
		edges          int
	}{
		{name: "all recorded ancestors present is not complete", selected: []int{0, 1, 2}, representative: 100, observedRoot: true, status: "unknown", edges: 2},
		{name: "root absent", selected: []int{1, 2}, representative: 200, status: "partial", reasons: []string{"missing_recorded_parent", "missing_recorded_root"}, missingParents: []int64{100}, edges: 1},
		{name: "middle absent", selected: []int{0, 2}, representative: 100, observedRoot: true, status: "partial", reasons: []string{"missing_recorded_parent"}, missingParents: []int64{200}},
		{name: "descendant alone", selected: []int{2}, representative: 300, status: "partial", reasons: []string{"missing_recorded_parent", "missing_recorded_root"}, missingParents: []int64{200}},
		{name: "root alone does not rule out descendants", selected: []int{0}, representative: 100, observedRoot: true, status: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allRuns := episodeCoverageTestRuns()
			var runs []RunData
			for _, index := range tt.selected {
				runs = append(runs, allRuns[index])
			}
			episodes, edges := buildEpisodeData(runs, nil)
			require.Len(t, episodes, 1)
			require.Len(t, edges, tt.edges)
			ep := episodes[0]
			assert.Equal(t, "canonical", ep.EpisodeID)
			assert.Equal(t, "high", ep.Confidence, "lineage confidence must not measure coverage")
			assert.Equal(t, tt.representative, ep.RepresentativeRunID)
			assert.Equal(t, len(runs)*10, ep.TotalTokens)
			assert.InDelta(t, float64(len(runs))*0.1, ep.TotalAIC, 1e-9)
			require.Len(t, ep.RecordedRoots, 1)
			assert.Equal(t, EpisodeRoot{
				RunID: "100", WorkflowID: allRuns[0].AwContext.RootWorkflowID,
				Repository: "org/repo", Observed: tt.observedRoot,
			}, ep.RecordedRoots[0])
			assert.Equal(t, "selected_runs", ep.Coverage.Scope)
			assert.Equal(t, tt.status, ep.Coverage.Status)
			assert.Equal(t, tt.reasons, ep.Coverage.Reasons)
			assert.Equal(t, tt.missingParents, ep.Coverage.MissingParentRunIDs)
			assert.False(t, ep.Coverage.CollectionTruncated)
			encoded, err := json.Marshal(ep)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), `"root_run_id":`)
			assert.NotContains(t, string(encoded), `"primary_workflow":`)

			slices.Reverse(runs)
			reversed, _ := buildEpisodeData(runs, nil)
			assert.Equal(t, episodes, reversed, "new evidence must preserve stable ordering")
		})
	}
}

func TestEpisodeCoverageUnknownRootAndReusableHops(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		context *AwContext
		status  string
		reasons []string
	}{
		{name: "standalone without root fields", context: &AwContext{EpisodeID: "canonical", RunID: "100", WorkflowID: "root"}, status: "unknown"},
		{name: "missing caller", context: &AwContext{EpisodeID: "canonical", RunID: "99", WorkflowID: "caller"}, status: "partial", reasons: []string{"missing_recorded_parent"}},
		{name: "canonical ID does not encode trusted root fields", context: &AwContext{EpisodeID: "99-1:root", RunID: "100"}, status: "unknown"},
		{name: "shared-run reusable hops", context: &AwContext{
			EpisodeID: "canonical", RunID: "100", RootRunID: "100", RootRepo: "org/repo",
			HopID: "100-1:reusable", ParentHopID: "100-1:caller",
		}, status: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := episodeCoverageTestRuns()[0]
			run.AwContext = tt.context
			episodes, edges := buildEpisodeData([]RunData{run}, nil)
			require.Len(t, episodes, 1)
			assert.Empty(t, edges, "same-run hops must not introduce a per-hop graph")
			assert.Equal(t, tt.status, episodes[0].Coverage.Status)
			assert.Equal(t, tt.reasons, episodes[0].Coverage.Reasons)
			if tt.name == "shared-run reusable hops" {
				require.Len(t, episodes[0].RecordedRoots, 1)
				assert.True(t, episodes[0].RecordedRoots[0].Observed)
			} else {
				assert.Empty(t, episodes[0].RecordedRoots, "missing root fields must not fabricate canonical identity")
			}
		})
	}
}

func TestEpisodeCoverageUnlinkedRootRemainsUnobserved(t *testing.T) {
	t.Parallel()
	runs := episodeCoverageTestRuns()
	runs[0].AwContext = nil

	episodes, _ := buildEpisodeData([]RunData{runs[0], runs[2]}, nil)
	require.Len(t, episodes, 1)
	episode := episodes[0]
	require.Len(t, episode.RecordedRoots, 1)
	assert.False(t, episode.RecordedRoots[0].Observed)
	assert.Equal(t, "partial", episode.Coverage.Status)
	assert.Equal(t, []string{"missing_recorded_parent", "unverified_root_lineage"}, episode.Coverage.Reasons)
}

func TestEpisodeCoverageSameRunParentChecksRepository(t *testing.T) {
	t.Parallel()
	run := episodeCoverageTestRuns()[0]
	run.AwContext = &AwContext{EpisodeID: "canonical", RunID: "100", Repo: "org/other"}

	episodes, edges := buildEpisodeData([]RunData{run}, nil)
	require.Len(t, episodes, 1)
	assert.Empty(t, edges, "same-run callers must not create self-edges")
	assert.Equal(t, "partial", episodes[0].Coverage.Status)
	assert.Equal(t, []string{"conflicting_parent_lineage"}, episodes[0].Coverage.Reasons)
}

func TestEpisodeCoverageRepositoryIdentityIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	runs := episodeCoverageTestRuns()[:2]
	runs[1].AwContext.RootRepo = "Org/Repo"
	runs[1].AwContext.Repo = "ORG/REPO"

	episodes, _ := buildEpisodeData(runs, nil)
	require.Len(t, episodes, 1)
	assert.Equal(t, "unknown", episodes[0].Coverage.Status)
	require.Len(t, episodes[0].RecordedRoots, 2, "preserve the original recorded claims")
	assert.ElementsMatch(t, []string{"org/repo", "Org/Repo"}, []string{
		episodes[0].RecordedRoots[0].Repository,
		episodes[0].RecordedRoots[1].Repository,
	})
	assert.True(t, episodes[0].RecordedRoots[0].Observed)
	assert.True(t, episodes[0].RecordedRoots[1].Observed)
}

func TestEpisodeCoveragePreservesPartialRootClaims(t *testing.T) {
	t.Parallel()
	run := episodeCoverageTestRuns()[0]
	run.AwContext.RootWorkflowID = "org/repo/.github/workflows/root.yml@main"
	run.AwContext.RootRepo = "Org/Repo"
	run.AwContext.RootRunID = ""

	episodes, _ := buildEpisodeData([]RunData{run}, nil)
	require.Len(t, episodes, 1)
	require.Len(t, episodes[0].RecordedRoots, 1)
	assert.Equal(t, EpisodeRoot{
		WorkflowID: run.AwContext.RootWorkflowID,
		Repository: "Org/Repo",
	}, episodes[0].RecordedRoots[0])
	assert.Equal(t, "unknown", episodes[0].Coverage.Status)
}

func TestEpisodeCoverageContradictoryAndInvalidContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func([]RunData)
		reason string
	}{
		{name: "different root run", change: func(r []RunData) { r[1].AwContext.RootRunID = "200" }, reason: "conflicting_recorded_roots"},
		{name: "different root workflow", change: func(r []RunData) { r[1].AwContext.RootWorkflowID = "other" }, reason: "conflicting_recorded_roots"},
		{name: "different root repository", change: func(r []RunData) { r[1].AwContext.RootRepo = "org/other" }, reason: "conflicting_recorded_roots"},
		{name: "parent in another canonical episode", change: func(r []RunData) { r[0].AwContext.EpisodeID = "other" }, reason: "conflicting_parent_lineage"},
		{name: "recorded root in another canonical episode", change: func(r []RunData) { r[0].AwContext.EpisodeID = "other" }, reason: "conflicting_root_lineage"},
		{name: "parent without canonical identity", change: func(r []RunData) { r[0].AwContext.EpisodeID = "" }, reason: "unverified_parent_lineage"},
		{name: "root without canonical identity", change: func(r []RunData) { r[0].AwContext.EpisodeID = "" }, reason: "unverified_root_lineage"},
		{name: "parent repository mismatch", change: func(r []RunData) { r[1].AwContext.Repo = "org/other" }, reason: "conflicting_parent_lineage"},
		{name: "invalid root ID", change: func(r []RunData) { r[1].AwContext.RootRunID = "not-an-id" }, reason: "invalid_recorded_root_run_id"},
		{name: "invalid parent ID", change: func(r []RunData) { r[1].AwContext.RunID = "-1" }, reason: "invalid_recorded_parent_run_id"},
		{name: "overflow parent ID", change: func(r []RunData) { r[1].AwContext.RunID = "99999999999999999999999" }, reason: "invalid_recorded_parent_run_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := episodeCoverageTestRuns()[:2]
			tt.change(runs)
			episodes, _ := buildEpisodeData(runs, nil)
			for _, ep := range episodes {
				if ep.EpisodeID != "canonical" {
					continue
				}
				assert.Equal(t, "partial", ep.Coverage.Status)
				assert.Contains(t, ep.Coverage.Reasons, tt.reason)
				if tt.reason == "conflicting_recorded_roots" {
					require.Len(t, ep.RecordedRoots, 2, "preserve conflicting claims instead of choosing one")
				}
				return
			}
			t.Fatal("canonical episode missing")
		})
	}
}

func TestEpisodeCoverageMissingParentsUniqueAndSorted(t *testing.T) {
	t.Parallel()
	runs := episodeCoverageTestRuns()
	runs[0].AwContext.RunID = "99"
	runs[1].AwContext.RunID = "98"
	runs[2].AwContext.RunID = "99"
	episodes, _ := buildEpisodeData(runs, nil)
	require.Len(t, episodes, 1)
	assert.Equal(t, []int64{98, 99}, episodes[0].Coverage.MissingParentRunIDs)
}

func TestEpisodeCoverageCollectionAndRendering(t *testing.T) {
	for _, tt := range []struct {
		name    string
		opts    renderLogsOutputOptions
		bounded bool
	}{
		{name: "unbounded selection"},
		{name: "count with date range", opts: renderLogsOutputOptions{continuation: &ContinuationData{Count: 10, StartDate: "-30d"}}, bounded: true},
		{name: "matching multi-target continuation", opts: renderLogsOutputOptions{continuations: []WorkflowContinuation{{Repository: "org/repo"}}}, bounded: true},
		{name: "unrelated multi-target continuation", opts: renderLogsOutputOptions{continuations: []WorkflowContinuation{{Repository: "org/other"}}}},
		{name: "matching workflow continuation", opts: renderLogsOutputOptions{continuations: []WorkflowContinuation{{Repository: "Org/Repo", ContinuationData: ContinuationData{WorkflowName: "root"}}}}, bounded: true},
		{name: "count limit without cursor", opts: renderLogsOutputOptions{countLimitReached: true}, bounded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run := episodeCoverageTestRuns()[0]
			opts := tt.opts
			opts.outputDir = t.TempDir()
			data, err := prepareLogsData([]ProcessedRun{processedRunFromCachedData(run, nil, opts.outputDir)}, opts)
			require.NoError(t, err)
			require.Len(t, data.Episodes, 1)
			assert.Equal(t, tt.bounded, data.Episodes[0].Coverage.CollectionTruncated)
			status := "unknown"
			if tt.bounded {
				status = "partial"
				assert.Equal(t, []string{"collection_truncated"}, data.Episodes[0].Coverage.Reasons)
			} else {
				assert.Empty(t, data.Episodes[0].Coverage.Reasons)
			}
			assert.Equal(t, status, data.Episodes[0].Coverage.Status)
			for _, verbose := range []bool{false, true} {
				var out bytes.Buffer
				require.NoError(t, renderLogsJSONToWriter(&out, data, verbose))
				var rendered LogsData
				require.NoError(t, json.Unmarshal(out.Bytes(), &rendered))
				require.Len(t, rendered.Episodes, 1, "compact JSON must retain root evidence for a single selected run")
				assert.Equal(t, data.Episodes[0], rendered.Episodes[0])
			}
			var out bytes.Buffer
			require.NoError(t, renderLogsCompactVerboseToWriter(&out, data))
			assert.Contains(t, out.String(), "coverage="+status+" scope=selected_runs")
			assert.Contains(t, out.String(), "recorded_root run=100")
			assert.Contains(t, out.String(), "representative_run=100")
		})
	}
}

func TestEpisodeCoverageContinuationOnlyMarksMatchingRepository(t *testing.T) {
	t.Parallel()
	run := episodeCoverageTestRuns()[0]
	otherRun := run
	otherContext := *run.AwContext
	otherRun.RunID = 400
	otherRun.Repository = "org/other"
	otherRun.AwContext = &otherContext
	otherRun.AwContext.EpisodeID = "other"
	otherRun.AwContext.RunID = "400"
	otherRun.AwContext.Repo = "org/other"
	otherRun.AwContext.RootRunID = "400"
	otherRun.AwContext.RootRepo = "org/other"

	opts := renderLogsOutputOptions{
		outputDir: t.TempDir(),
		continuations: []WorkflowContinuation{{
			Repository: "org/other",
			ContinuationData: ContinuationData{
				WorkflowName: "root",
			},
		}},
	}
	data, err := prepareLogsData([]ProcessedRun{
		processedRunFromCachedData(run, nil, opts.outputDir),
		processedRunFromCachedData(otherRun, nil, opts.outputDir),
	}, opts)
	require.NoError(t, err)
	require.Len(t, data.Episodes, 2)
	for _, episode := range data.Episodes {
		expected := episode.Repository == "org/other"
		assert.Equal(t, expected, episode.Coverage.CollectionTruncated, episode.Repository)
	}
}

func TestCompactLogsRetainsCanonicalEpisodeEvidence(t *testing.T) {
	t.Parallel()
	run := episodeCoverageTestRuns()[1]
	run.AwContext.RootRunID, run.AwContext.RootWorkflowID, run.AwContext.RootRepo = "", "", ""
	episodes, edges := buildEpisodeData([]RunData{run}, nil)
	compacted := compactLogsData(LogsData{Episodes: episodes, Edges: edges})
	assert.Equal(t, episodes, compacted.Episodes)

	run.AwContext.RunID = "200"
	episodes, edges = buildEpisodeData([]RunData{run}, nil)
	compacted = compactLogsData(LogsData{Episodes: episodes, Edges: edges})
	require.Len(t, compacted.Episodes, 1, "compact JSON retains single-run canonical episodes even without gaps or root claims")
	assert.Equal(t, "unknown", compacted.Episodes[0].Coverage.Status)

	run.AwContext = nil
	episodes, edges = buildEpisodeData([]RunData{run}, nil)
	compacted = compactLogsData(LogsData{Episodes: episodes, Edges: edges})
	assert.Empty(t, compacted.Episodes, "runs without canonical identity have no episode rollup")
}
