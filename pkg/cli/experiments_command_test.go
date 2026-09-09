//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileExperimentDetailsWithConfigs(t *testing.T) {
	t.Parallel()

	t.Run("drops stale variant labels not in the declared variants list", func(t *testing.T) {
		details := &ExperimentDetails{
			Experiments: []ExperimentVariantStats{
				{
					Name:     "model_size",
					Variants: map[string]int{"gpt-5.4": 28, "gpt-5.4-mini": 18, "small-agent": 1, "agent": 1},
					Total:    48,
				},
			},
		}
		configs := map[string]*workflow.ExperimentConfig{
			"model_size": {Variants: []string{"gpt-5.4", "gpt-5.4-mini"}},
		}

		reconcileExperimentDetailsWithConfigs(details, configs)

		require.Len(t, details.Experiments, 1)
		assert.Equal(t, map[string]int{"gpt-5.4": 28, "gpt-5.4-mini": 18}, details.Experiments[0].Variants)
		assert.Equal(t, 46, details.Experiments[0].Total, "total should be recomputed from the reconciled counts")
	})

	t.Run("leaves counts untouched when there is no matching config", func(t *testing.T) {
		details := &ExperimentDetails{
			Experiments: []ExperimentVariantStats{
				{Name: "unrelated", Variants: map[string]int{"a": 1, "b": 2}, Total: 3},
			},
		}

		reconcileExperimentDetailsWithConfigs(details, map[string]*workflow.ExperimentConfig{})
		reconcileExperimentDetailsWithConfigs(details, map[string]*workflow.ExperimentConfig{"unrelated": {}})

		assert.Equal(t, map[string]int{"a": 1, "b": 2}, details.Experiments[0].Variants)
		assert.Equal(t, 3, details.Experiments[0].Total)
	})

	t.Run("handles nil details safely", func(t *testing.T) {
		assert.NotPanics(t, func() {
			reconcileExperimentDetailsWithConfigs(nil, map[string]*workflow.ExperimentConfig{"x": {Variants: []string{"a"}}})
		})
	})
}

func TestFetchRemoteExperimentDetailsClassifiesTitleCaseNotFound(t *testing.T) {
	fakeBinDir := t.TempDir()
	fakeGH := filepath.Join(fakeBinDir, "gh")
	require.NoError(t, os.WriteFile(fakeGH, []byte("#!/bin/sh\necho 'HTTP 404: Not Found' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := fetchRemoteExperimentDetails("octo/repo", "experiments/missing", "missing")

	require.EqualError(t, err, `experiment "missing" not found in octo/repo`)
}

func TestBuildSafeGitShowObjectArg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		ref       string
		fileName  string
		want      string
		shouldErr bool
	}{
		{
			name:     "valid ref and file",
			ref:      "origin/experiments/my-feature",
			fileName: "state.jsonl",
			want:     "origin/experiments/my-feature:state.jsonl",
		},
		{
			name:      "rejects flag-like ref",
			ref:       "--help",
			fileName:  "state.jsonl",
			shouldErr: true,
		},
		{
			name:      "rejects revision expression suffix",
			ref:       "origin/experiments/my-feature~1",
			fileName:  "state.jsonl",
			shouldErr: true,
		},
		{
			name:      "rejects revision expression braces",
			ref:       "origin/experiments/my-feature^{tree}",
			fileName:  "state.jsonl",
			shouldErr: true,
		},
		{
			name:      "rejects colon in ref",
			ref:       "origin/experiments/my-feature:other",
			fileName:  "state.jsonl",
			shouldErr: true,
		},
		{
			name:      "rejects path traversal",
			ref:       "origin/experiments/my-feature",
			fileName:  "../state.json",
			shouldErr: true,
		},
		{
			name:      "rejects colon in file name",
			ref:       "origin/experiments/my-feature",
			fileName:  "state.json:HEAD",
			shouldErr: true,
		},
		{
			name:      "rejects flag-like file name",
			ref:       "origin/experiments/my-feature",
			fileName:  "-n",
			shouldErr: true,
		},
		{
			name:      "rejects control character in file name",
			ref:       "origin/experiments/my-feature",
			fileName:  "state\x01.jsonl",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildSafeGitShowObjectArg(tt.ref, tt.fileName)
			if tt.shouldErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsSafeExperimentStateRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{name: "experiment branch", ref: "origin/experiments/my-feature", want: true},
		{name: "local experiment branch", ref: "experiments/my-feature", want: true},
		{name: "evals branch", ref: "evals/myworkflow", want: true},
		{name: "short sha", ref: "a1b2c3d", want: true},
		{name: "reject too-short sha", ref: "a1b2c3", want: false},
		{name: "full sha", ref: "0123456789abcdef0123456789abcdef01234567", want: true},
		{name: "reject revision operator", ref: "origin/experiments/my-feature~1", want: false},
		{name: "reject brace expression", ref: "origin/experiments/my-feature^{tree}", want: false},
		{name: "reject wrong prefix", ref: "origin/main", want: false},
		{name: "reject invalid sequence", ref: "origin/experiments/my..feature", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSafeExperimentStateRef(tt.ref))
		})
	}
}

func TestExtractExperimentName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ref      string
		expected string
	}{
		{
			name:     "remote ref with origin prefix",
			ref:      "origin/experiments/my-feature",
			expected: "my-feature",
		},
		{
			name:     "local ref without origin prefix",
			ref:      "experiments/my-feature",
			expected: "my-feature",
		},
		{
			name:     "nested experiment name",
			ref:      "experiments/team/feature-x",
			expected: "team/feature-x",
		},
		{
			name:     "remote nested ref",
			ref:      "origin/experiments/team/feature-x",
			expected: "team/feature-x",
		},
		{
			name:     "unrelated branch returns empty",
			ref:      "origin/main",
			expected: "",
		},
		{
			name:     "feature branch without prefix returns empty",
			ref:      "feature/my-feature",
			expected: "",
		},
		{
			name:     "bare experiments prefix returns empty",
			ref:      "experiments/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractExperimentName(tt.ref)
			assert.Equal(t, tt.expected, got, "extractExperimentName(%q)", tt.ref)
		})
	}
}

func TestParseExperimentState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		input           []byte
		wantExperiments int
		wantTotalRuns   int
		wantLastRun     string
	}{
		{
			name: "valid state with runs",
			input: []byte(`{
				"counts": {"feature": {"A": 3, "B": 2}},
				"runs": [
					{"run_id": "1", "timestamp": "2024-06-01T10:00:00Z", "assignments": {"feature": "A"}},
					{"run_id": "2", "timestamp": "2024-06-15T12:00:00Z", "assignments": {"feature": "B"}}
				]
			}`),
			wantExperiments: 1,
			wantTotalRuns:   2,
			wantLastRun:     "2024-06-15",
		},
		{
			name: "valid state without runs array",
			input: []byte(`{
				"counts": {"exp1": {"yes": 5, "no": 5}, "exp2": {"on": 3, "off": 7}}
			}`),
			wantExperiments: 2,
			wantTotalRuns:   20,
			wantLastRun:     "",
		},
		{
			name:            "empty JSON object",
			input:           []byte(`{}`),
			wantExperiments: 0,
			wantTotalRuns:   0,
			wantLastRun:     "",
		},
		{
			// A single-record JSONL is also valid standalone JSON.  parseExperimentState must
			// not return it as an empty snapshot; it must fall through to JSONL parsing so the
			// run record is properly loaded.
			name:            "single-record jsonl is not treated as empty snapshot",
			input:           []byte(`{"run_id":"1","timestamp":"2024-06-01T10:00:00Z","assignments":{"feature":"A"}}`),
			wantExperiments: 1,
			wantTotalRuns:   1,
			wantLastRun:     "2024-06-01",
		},
		{
			name:            "invalid JSON returns empty state",
			input:           []byte(`not json`),
			wantExperiments: 0,
			wantTotalRuns:   0,
			wantLastRun:     "",
		},
		{
			name: "jsonl run ledger",
			input: []byte(`{"run_id":"1","timestamp":"2024-06-01T10:00:00Z","assignments":{"feature":"A"}}
{"run_id":"2","timestamp":"2024-06-15T12:00:00Z","assignments":{"feature":"B"}}`),
			wantExperiments: 1,
			wantTotalRuns:   2,
			wantLastRun:     "2024-06-15",
		},
		{
			name: "jsonl run ledger with baseline counts",
			input: []byte(`{"run_id":"1","timestamp":"2024-06-01T10:00:00Z","assignments":{"feature":"A"},"baseline_counts":{"feature":{"A":2}}}
{"run_id":"2","timestamp":"2024-06-15T12:00:00Z","assignments":{"feature":"B"}}`),
			wantExperiments: 1,
			wantTotalRuns:   2,
			wantLastRun:     "2024-06-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := parseExperimentState(tt.input)
			require.NotNil(t, state, "state should never be nil")
			assert.Len(t, state.Counts, tt.wantExperiments, "experiment count")
			assert.Equal(t, tt.wantTotalRuns, experimentTotalRuns(state), "total runs")
			assert.Equal(t, tt.wantLastRun, experimentLastRun(state), "last run date")
		})
	}
}

func TestExperimentDetailsFromState(t *testing.T) {
	t.Parallel()
	state := &ExperimentState{
		Counts: map[string]map[string]int{
			"style":   {"concise": 4, "detailed": 6},
			"feature": {"on": 5, "off": 5},
		},
		Runs: []ExperimentRunRecord{
			{RunID: "r1", Timestamp: "2024-05-01T00:00:00Z", Assignments: map[string]string{"style": "concise", "feature": "on"}},
			{RunID: "r2", Timestamp: "2024-05-02T00:00:00Z", Assignments: map[string]string{"style": "detailed", "feature": "off"}},
		},
	}

	details := experimentDetailsFromState("my-workflow", "experiments/my-workflow", state)
	require.NotNil(t, details, "details should not be nil")
	assert.Equal(t, "my-workflow", details.WorkflowID, "workflow ID")
	assert.Equal(t, "experiments/my-workflow", details.Branch, "branch")
	assert.Equal(t, 2, details.TotalRuns, "total runs from runs array")
	assert.Len(t, details.Experiments, 2, "should have 2 experiment entries")
	assert.Len(t, details.RecentRuns, 2, "should have 2 recent runs")

	// Experiments are sorted by name.
	assert.Equal(t, "feature", details.Experiments[0].Name, "first experiment sorted by name")
	assert.Equal(t, 10, details.Experiments[0].Total, "feature total")
	assert.Equal(t, "style", details.Experiments[1].Name, "second experiment sorted by name")
	assert.Equal(t, 10, details.Experiments[1].Total, "style total")
}

func TestParseExperimentStateJSONLBaselineCounts(t *testing.T) {
	t.Parallel()
	state := parseExperimentState([]byte(`{"run_id":"1","timestamp":"2024-06-01T10:00:00Z","assignments":{"feature":"A"},"baseline_counts":{"feature":{"A":2,"B":1}}}
{"run_id":"2","timestamp":"2024-06-15T12:00:00Z","assignments":{"feature":"B"}}`))

	require.NotNil(t, state)
	assert.Equal(t, map[string]map[string]int{
		"feature": {"A": 3, "B": 2},
	}, state.Counts)
	assert.Len(t, state.Runs, 2)
}

func TestAppendExperimentRunAddsAssignmentToBaseline(t *testing.T) {
	t.Parallel()
	state := emptyExperimentState()
	appendExperimentRun(state, ExperimentRunRecord{
		RunID:       "1",
		Timestamp:   "2026-08-18T12:00:00Z",
		Assignments: map[string]string{"feature": "A"},
		BaselineCounts: map[string]map[string]int{
			"feature": {"A": 2, "B": 1},
		},
	})

	assert.Equal(t, map[string]map[string]int{
		"feature": {"A": 3, "B": 1},
	}, state.Counts)
}

func TestParseExperimentStateJSONLSnapshotDiscardsEarlierRuns(t *testing.T) {
	t.Parallel()
	state := parseExperimentState([]byte(`{"run_id":"before","timestamp":"2026-08-18T10:00:00Z","assignments":{"feature":"A"}}
{"counts":{"feature":{"B":2}}}
{"run_id":"after","timestamp":"2026-08-18T12:00:00Z","assignments":{"feature":"B"}}`))

	assert.Equal(t, map[string]map[string]int{"feature": {"B": 3}}, state.Counts)
	require.Len(t, state.Runs, 1)
	assert.Equal(t, "after", state.Runs[0].RunID)
}

func TestExperimentTotalRunsFallback(t *testing.T) {
	t.Parallel()
	// When no runs array present, sum variant counts.
	state := &ExperimentState{
		Counts: map[string]map[string]int{
			"exp": {"A": 3, "B": 4},
		},
	}
	assert.Equal(t, 7, experimentTotalRuns(state), "total from counts fallback")
}

func TestFormatAssignments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    map[string]string
		expected string
	}{
		{
			name:     "nil map returns dash",
			input:    nil,
			expected: "-",
		},
		{
			name:     "empty map returns dash",
			input:    map[string]string{},
			expected: "-",
		},
		{
			name:     "single entry",
			input:    map[string]string{"style": "concise"},
			expected: "style=concise",
		},
		{
			name:     "multiple entries sorted by key",
			input:    map[string]string{"z": "last", "a": "first"},
			expected: "a=first, z=last",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatAssignments(tt.input), "formatAssignments(%v)", tt.input)
		})
	}
}

func TestParsePagedJSONArray(t *testing.T) {
	t.Parallel()
	type item struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name          string
		input         string
		expectedCount int
		shouldErr     bool
	}{
		{
			name:          "single page",
			input:         `[{"name":"a"},{"name":"b"}]`,
			expectedCount: 2,
		},
		{
			name:          "two pages",
			input:         `[{"name":"a"}][{"name":"b"},{"name":"c"}]`,
			expectedCount: 3,
		},
		{
			name:          "empty array",
			input:         `[]`,
			expectedCount: 0,
		},
		{
			name:      "invalid JSON",
			input:     `{not valid}`,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePagedJSONArray[item](tt.input)
			if tt.shouldErr {
				assert.Error(t, err, "should return an error for invalid JSON")
				return
			}
			require.NoError(t, err, "should parse successfully")
			assert.Len(t, got, tt.expectedCount, "expected %d items", tt.expectedCount)
		})
	}
}

func TestSummarizeMetricEvalResults(t *testing.T) {
	t.Parallel()
	data := []byte(`{"id":"quality","answer":"YES","runid":"100"}
{"id":"quality","answer":"NO","runid":"101"}
{"id":"quality","answer":"UNKNOWN","runid":"102"}
{"id":"quality","answer":"MAYBE","runid":"103"}
{"id":"coverage","answer":"YES","runid":"200"}
`)
	result := summarizeMetricEvalResults(parseEvalResultRecords(data))
	require.NotNil(t, result)

	quality, ok := result["quality"]
	require.True(t, ok)
	assert.Equal(t, 1, quality.Yes)
	assert.Equal(t, 1, quality.No)
	assert.Equal(t, 2, quality.Unknown)
	assert.Equal(t, 4, quality.Total)
	assert.Equal(t, "MAYBE", quality.LatestAnswer)
	assert.Equal(t, "103", quality.LatestRunID)

	coverage, ok := result["coverage"]
	require.True(t, ok)
	assert.Equal(t, 1, coverage.Yes)
	assert.Equal(t, 0, coverage.No)
	assert.Equal(t, 0, coverage.Unknown)
	assert.Equal(t, 1, coverage.Total)
	assert.Equal(t, "YES", coverage.LatestAnswer)
	assert.Equal(t, "200", coverage.LatestRunID)
}

func TestExperimentInfoJSONOutput(t *testing.T) {
	t.Parallel()
	experiments := []ExperimentInfo{
		{
			WorkflowID:  "my-workflow",
			Branch:      "experiments/my-workflow",
			Experiments: 2,
			TotalRuns:   15,
			LastRun:     "2024-06-15",
		},
	}

	jsonBytes, err := json.MarshalIndent(experiments, "", "  ")
	require.NoError(t, err, "should marshal ExperimentInfo to JSON")

	var result []map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result), "should unmarshal JSON back")

	require.Len(t, result, 1, "should have 1 experiment")
	assert.Equal(t, "my-workflow", result[0]["workflow_id"], "workflow_id field should match")
	assert.Equal(t, "experiments/my-workflow", result[0]["branch"], "branch field should match")
	assert.EqualValues(t, 2, result[0]["experiments"], "experiments count should match")
	assert.EqualValues(t, 15, result[0]["total_runs"], "total_runs should match")
	assert.Equal(t, "2024-06-15", result[0]["last_run"], "last_run should match")
}

func TestExperimentDetailsJSONOutput(t *testing.T) {
	t.Parallel()
	details := ExperimentDetails{
		WorkflowID: "my-workflow",
		Branch:     "experiments/my-workflow",
		TotalRuns:  10,
		Experiments: []ExperimentVariantStats{
			{
				Name:     "style",
				Variants: map[string]int{"concise": 6, "detailed": 4},
				Total:    10,
			},
		},
		RecentRuns: []ExperimentRunRecord{
			{RunID: "123", Timestamp: "2024-06-01T00:00:00Z", Assignments: map[string]string{"style": "concise"}},
		},
	}

	jsonBytes, err := json.MarshalIndent(details, "", "  ")
	require.NoError(t, err, "should marshal ExperimentDetails to JSON")

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result), "should unmarshal JSON back")

	assert.Equal(t, "my-workflow", result["workflow_id"], "workflow_id should match")
	assert.EqualValues(t, 10, result["total_runs"], "total_runs should match")

	experiments, ok := result["experiments"].([]any)
	require.True(t, ok, "experiments should be an array")
	require.Len(t, experiments, 1, "should have 1 experiment")

	recentRuns, ok := result["recent_runs"].([]any)
	require.True(t, ok, "recent_runs should be an array")
	require.Len(t, recentRuns, 1, "should have 1 recent run")
}

func TestNewExperimentsCommand(t *testing.T) {
	t.Parallel()
	cmd := NewExperimentsCommand()
	require.NotNil(t, cmd, "command should be created")
	assert.Equal(t, "experiments", cmd.Name(), "command name should be experiments")
	assert.False(t, cmd.Hidden, "experiments command should be visible")

	subCmds := cmd.Commands()
	subNames := make([]string, 0, len(subCmds))
	for _, sub := range subCmds {
		subNames = append(subNames, sub.Name())
	}

	assert.Contains(t, subNames, "list", "should have list subcommand")
	assert.Contains(t, subNames, "analyze", "should have analyze subcommand")
}

func TestExperimentsListSubcommandFlags(t *testing.T) {
	t.Parallel()
	cmd := NewExperimentsListSubcommand()
	require.NotNil(t, cmd, "list subcommand should be created")

	assert.NotNil(t, cmd.Flag("json"), "should have --json flag")
	assert.NotNil(t, cmd.Flag("repo"), "should have --repo flag")
}

func TestExperimentsAnalyzeSubcommandFlags(t *testing.T) {
	t.Parallel()
	cmd := NewExperimentsAnalyzeSubcommand()
	require.NotNil(t, cmd, "analyze subcommand should be created")

	assert.NotNil(t, cmd.Flag("json"), "should have --json flag")
	assert.NotNil(t, cmd.Flag("repo"), "should have --repo flag")
}

func TestExperimentsAnalyzeRequiresArg(t *testing.T) {
	t.Parallel()
	cmd := NewExperimentsAnalyzeSubcommand()
	require.NotNil(t, cmd, "analyze subcommand should be created")

	err := cmd.Args(cmd, []string{})
	assert.Error(t, err, "analyze should require exactly 1 argument")
}

func TestParseExperimentStateJSONLSkipsInvalidLines(t *testing.T) {
	t.Parallel()
	// Valid records plus an unrecognized line: the valid records should still be parsed.
	data := `{"run_id":"1","timestamp":"2024-06-01T10:00:00Z","assignments":{"style":"concise"}}
this is not valid json
{"run_id":"2","timestamp":"2024-06-02T10:00:00Z","assignments":{"style":"detailed"}}`

	state := parseExperimentState([]byte(data))

	require.NotNil(t, state)
	assert.Equal(t, map[string]map[string]int{
		"style": {"concise": 1, "detailed": 1},
	}, state.Counts, "should accumulate counts from valid lines despite invalid lines")
	assert.Len(t, state.Runs, 2, "should have 2 run records")
}

func TestParseExperimentStateJSONLAllInvalid(t *testing.T) {
	t.Parallel()
	// When all lines are invalid, should return an empty state (not nil).
	data := `not json at all
also not json`

	state := parseExperimentState([]byte(data))

	require.NotNil(t, state)
	assert.Empty(t, state.Counts, "should return empty counts for all-invalid input")
	assert.Empty(t, state.Runs, "should return empty runs for all-invalid input")
}
