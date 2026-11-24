//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRoutingCommit struct {
	branch, expectedHead, path, headline string
	content                              []byte
}

type fakeRoutingAPI struct {
	heads   map[string]string
	files   map[string]string // "<ref>:<path>" -> content
	latest  int64
	commits []fakeRoutingCommit
}

func (f *fakeRoutingAPI) BranchHead(_ context.Context, branch string) (string, error) {
	return f.heads[branch], nil
}

func (f *fakeRoutingAPI) ReadFile(_ context.Context, ref, path string) ([]byte, error) {
	content, ok := f.files[ref+":"+path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(content), nil
}

func (f *fakeRoutingAPI) HistoryBranches(context.Context) ([]string, error) {
	var branches []string
	for branch := range f.heads {
		branches = append(branches, branch)
	}
	return branches, nil
}

func (f *fakeRoutingAPI) LatestRunNumber(context.Context, string) (int64, error) {
	return f.latest, nil
}

func (f *fakeRoutingAPI) CommitFile(_ context.Context, branch, expectedHead, path string, content []byte, headline string) (string, error) {
	f.commits = append(f.commits, fakeRoutingCommit{branch: branch, expectedHead: expectedHead, path: path, headline: headline, content: content})
	return "c0ffee", nil
}

// The contract's example history: proven model B and model A after one failed test.
const routingExampleState = `{"v": 2, "workflow": "pr-sous-chef", "seq": 412,
  "generation": {"hash": "9f2c41d0", "since_run": 731}, "reset_after_run": 0, "last_run": 812, "unexplained": 0,
  "scopes": {"copilot/cost/shared": {"incumbent": "github-copilot/model-b@high", "last_role": "observe", "last_success_seq": 411,
    "pairs": {
      "github-copilot/model-a@high": {"history": [[412, "2026-11-19T17:02:40Z", false, 12.0, 9.0, null]], "credits": 12.0, "model_credits": 9.0,
        "bills": 1, "streak": 1, "tests": 1, "suspended": null, "seen": "2026-11-19T17:02:40Z", "summary": {"runs": 1, "rate": 0.75, "hopeful": 0.914}},
      "github-copilot/model-b@high": {"history": [[411, "2026-11-19T16:40:02Z", true, 28.0, 24.0, null]], "credits": 28.0, "model_credits": 24.0,
        "bills": 40, "streak": 0, "tests": 0, "suspended": null, "seen": "2026-11-19T16:40:02Z", "summary": {"runs": 40, "rate": 0.985, "hopeful": 0.999}}}}},
  "updated": {"run": "44156334376", "at": "2026-11-19T17:02:40Z"}}`

const routingExampleRuns = `{"run": "44156334300", "number": 810, "attempt": 1, "at": "2026-11-19T16:40:02Z", "generation": "9f2c41d0", "scope": "copilot/cost/shared", "mode": "observe", "role": "observe", "pair": "github-copilot/model-b@high", "outcome": "success", "credits": 28.0, "counted": true, "note": null}
{"run": "44156334350", "number": 811, "attempt": 1, "at": "2026-11-19T16:50:00Z", "generation": "9f2c41d0", "scope": "copilot/cost/shared", "mode": "observe", "role": "observe", "pair": "github-copilot/model-b@high", "outcome": "skip", "credits": 3.0, "counted": false, "note": "infrastructure"}
{"run": "44156334376", "number": 812, "attempt": 1, "at": "2026-11-19T17:02:40Z", "generation": null, "scope": null, "mode": null, "role": null, "pair": null, "outcome": null, "credits": null, "counted": false, "note": "no_agent"}
`

func exampleRoutingAPI() *fakeRoutingAPI {
	return &fakeRoutingAPI{
		heads: map[string]string{"memory/adaptive-routing/pr-sous-chef": "abc123"},
		files: map[string]string{"abc123:state.json": routingExampleState, "abc123:runs.jsonl": routingExampleRuns},
	}
}

func TestRoutingStatusReportsEachPairsEvidence(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runRoutingStatus(context.Background(), exampleRoutingAPI(), "pr-sous-chef", &out, true))
	var status RoutingStatus
	require.NoError(t, json.Unmarshal(out.Bytes(), &status))

	assert.Equal(t, "memory/adaptive-routing/pr-sous-chef", status.Branch)
	assert.Nil(t, status.Recommendation, "observe mode makes no recommendation")
	assert.Equal(t, routingNoRecommendation, status.Note)
	require.Len(t, status.Scopes, 1)
	pairs := status.Scopes[0].Pairs
	require.Len(t, pairs, 2)
	assert.Equal(t, "github-copilot/model-b@high", pairs[0].Pair, "the pair with the most runs comes first")
	assert.Equal(t, int64(40), pairs[0].Runs)
	assert.InDelta(t, 0.985, *pairs[0].Rate, 1e-12)
	assert.InDelta(t, 0.914, *pairs[1].Hopeful, 1e-12)
	assert.Equal(t, int64(1), pairs[1].Streak)
	assert.Equal(t, RoutingRecentRuns{Runs: 3, Counted: 1, NotCounted: map[string]int{"infrastructure": 1, "no_agent": 1}, Lines: status.Recent.Lines}, status.Recent)

	var text bytes.Buffer
	require.NoError(t, printRoutingStatus(&text, status))
	assert.Contains(t, text.String(), "copilot/cost/shared (observe: ordinary pick)")
	assert.Contains(t, text.String(), routingNoRecommendation)
	assert.Contains(t, text.String(), "Recent runs (last 3): 1 counted; did not count: infrastructure 1, no_agent 1")
}

func TestRoutingStatusListsHistories(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runRoutingList(context.Background(), exampleRoutingAPI(), &out, true))
	var entries []RoutingHistoryListEntry
	require.NoError(t, json.Unmarshal(out.Bytes(), &entries))
	assert.Equal(t, []RoutingHistoryListEntry{{Workflow: "pr-sous-chef", Branch: "memory/adaptive-routing/pr-sous-chef", Scopes: 1, Pairs: 2,
		LastRun: 812, UpdatedAt: "2026-11-19T17:02:40Z"}}, entries)
}

func TestRoutingCommandsRefuseMissingHistoriesAndBadIDs(t *testing.T) {
	api := &fakeRoutingAPI{}
	require.ErrorContains(t, runRoutingStatus(context.Background(), api, "triage", &bytes.Buffer{}, false), "no routing history for triage")
	require.ErrorContains(t, runRoutingReset(context.Background(), api, "triage", time.Now()), "no routing history for triage")
	require.ErrorContains(t, runRoutingStatus(context.Background(), api, "../main", &bytes.Buffer{}, false), "is not a workflow ID")
	assert.Empty(t, api.commits)
}

func TestRoutingStatusReadsABranchWhoseFirstSaveNeverLanded(t *testing.T) {
	// The writer creates the branch from an empty root commit before its first save.
	api := &fakeRoutingAPI{heads: map[string]string{"memory/adaptive-routing/triage": "root01"}}
	var out bytes.Buffer
	require.NoError(t, runRoutingStatus(context.Background(), api, "triage", &out, true))
	var status RoutingStatus
	require.NoError(t, json.Unmarshal(out.Bytes(), &status))
	assert.Empty(t, status.Scopes)
	assert.Zero(t, status.Recent.Runs)

	out.Reset()
	require.NoError(t, runRoutingList(context.Background(), api, &out, true))
	assert.JSONEq(t, `[{"workflow": "triage", "branch": "memory/adaptive-routing/triage", "scopes": 0, "pairs": 0, "last_run": 0, "unexplained": 0, "updated_at": ""}]`, out.String())
}

func TestRoutingStatusListReportsHistoriesItCannotRead(t *testing.T) {
	api := exampleRoutingAPI()
	api.files["abc123:state.json"] = `{"v": 3, "workflow": "pr-sous-chef"}`
	require.ErrorContains(t, runRoutingList(context.Background(), api, &bytes.Buffer{}, true), "memory/adaptive-routing/pr-sous-chef: state.json is not a version 2 routing history")
}

func TestRoutingResetCommitsWhatTheWriterWould(t *testing.T) {
	// The reducer's reset vector pins the state gh aw routing reset writes.
	data, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "fixtures", "routing-history", "aw2-reducer.json"))
	require.NoError(t, err)
	var vectors struct {
		Cases []struct {
			Name  string          `json:"name"`
			State json.RawMessage `json:"state"`
			Reset struct {
				LatestRun int64  `json:"latest_run"`
				At        string `json:"at"`
			} `json:"reset"`
			Expect struct {
				State json.RawMessage `json:"state"`
			} `json:"expect"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(data, &vectors))
	for _, vector := range vectors.Cases {
		if vector.Name != "reset" {
			continue
		}
		var input routingHistoryState
		require.NoError(t, json.Unmarshal(vector.State, &input))
		api := &fakeRoutingAPI{
			heads:  map[string]string{"memory/adaptive-routing/" + input.Workflow: "abc123"},
			files:  map[string]string{"abc123:state.json": string(vector.State)},
			latest: vector.Reset.LatestRun,
		}
		at, err := time.Parse(time.RFC3339, vector.Reset.At)
		require.NoError(t, err)
		require.NoError(t, runRoutingReset(context.Background(), api, input.Workflow, at))

		require.Len(t, api.commits, 1)
		commit := api.commits[0]
		assert.Equal(t, fakeRoutingCommit{branch: "memory/adaptive-routing/" + input.Workflow, expectedHead: "abc123", path: "state.json",
			headline: "Reset the routing history after run 9", content: commit.content}, commit)
		var written, expected any
		require.NoError(t, json.Unmarshal(commit.content, &written))
		require.NoError(t, json.Unmarshal(vector.Expect.State, &expected))
		assert.Equal(t, expected, written)
		return
	}
	t.Fatal("the reducer vectors have no reset case")
}

func TestRoutingResetPreservesReconciliationCoverage(t *testing.T) {
	decodeRaw := func(t *testing.T, data []byte) map[string]any {
		t.Helper()
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var raw map[string]any
		require.NoError(t, dec.Decode(&raw))
		return raw
	}

	t.Run("raises reconciled_through to reset_after_run and clears unresolved runs at or below it", func(t *testing.T) {
		stateJSON := `{"v":2,"workflow":"triage","seq":5,"generation":{"hash":"9f2c41d0","since_run":1},"reset_after_run":0,"last_run":85,"reconciled_through":80,"unresolved_runs":[75,80],"unexplained":2,"scopes":{},"updated":{"run":"9085","at":"2026-11-24T09:00:00Z"}}`
		api := &fakeRoutingAPI{
			heads:  map[string]string{"memory/adaptive-routing/triage": "head1"},
			files:  map[string]string{"head1:state.json": stateJSON},
			latest: 100,
		}
		at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
		require.NoError(t, runRoutingReset(context.Background(), api, "triage", at))
		require.Len(t, api.commits, 1)

		raw := decodeRaw(t, api.commits[0].content)
		assert.Equal(t, json.Number("100"), raw["reset_after_run"])
		assert.Equal(t, json.Number("85"), raw["last_run"])
		assert.Equal(t, json.Number("100"), raw["reconciled_through"])
		assert.Equal(t, []any{}, raw["unresolved_runs"], "unresolved_runs field must be preserved as an empty array")
		assert.Equal(t, json.Number("0"), raw["unexplained"])
	})

	t.Run("preserves reconciliation obligations above reset_after_run", func(t *testing.T) {
		stateJSON := `{"v":2,"workflow":"triage","seq":5,"generation":{"hash":"9f2c41d0","since_run":1},"reset_after_run":0,"last_run":105,"reconciled_through":105,"unresolved_runs":[104,98,100,102,102],"unresolved_run_ids":{"98":"9098","100":"9100","102":"9102","104":"9104"},"unexplained":1,"scopes":{},"updated":{"run":"9105","at":"2026-11-24T09:00:00Z"}}`
		api := &fakeRoutingAPI{
			heads:  map[string]string{"memory/adaptive-routing/triage": "head2"},
			files:  map[string]string{"head2:state.json": stateJSON},
			latest: 100,
		}
		at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
		require.NoError(t, runRoutingReset(context.Background(), api, "triage", at))
		require.Len(t, api.commits, 1)

		raw := decodeRaw(t, api.commits[0].content)
		assert.Equal(t, json.Number("100"), raw["reset_after_run"])
		assert.Equal(t, json.Number("105"), raw["reconciled_through"])
		assert.Equal(t, []any{json.Number("102"), json.Number("104")}, raw["unresolved_runs"])
		assert.Equal(t, map[string]any{"102": "9102", "104": "9104"}, raw["unresolved_run_ids"])
	})
}

func TestRoutingStatusSurfacesReconciliationCoverage(t *testing.T) {
	stateJSON := `{"v":2,"workflow":"triage","seq":1,"generation":{"hash":"9f2c41d0","since_run":1},"reset_after_run":0,"last_run":105,"reconciled_through":105,"unresolved_runs":[102],"unexplained":0,"scopes":{},"updated":{"run":"9105","at":"2026-11-24T10:00:00Z"}}`
	api := &fakeRoutingAPI{
		heads: map[string]string{"memory/adaptive-routing/triage": "head3"},
		files: map[string]string{"head3:state.json": stateJSON},
	}
	var out bytes.Buffer
	require.NoError(t, runRoutingStatus(context.Background(), api, "triage", &out, true))
	var status RoutingStatus
	require.NoError(t, json.Unmarshal(out.Bytes(), &status))
	require.NotNil(t, status.ReconciledThrough)
	assert.Equal(t, int64(105), *status.ReconciledThrough)
	require.NotNil(t, status.UnresolvedRuns)
	assert.Equal(t, []int64{102}, *status.UnresolvedRuns)

	var text bytes.Buffer
	require.NoError(t, printRoutingStatus(&text, status))
	assert.Contains(t, text.String(), "Reconciled:  through run 105 (1 unresolved)")
}
