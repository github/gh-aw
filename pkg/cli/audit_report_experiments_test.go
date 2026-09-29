//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindExperimentStatePath(t *testing.T) {
	t.Parallel()
	t.Run("returns empty when logsPath is empty", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, findExperimentStatePath(""), "should return empty string for empty logsPath")
	})

	t.Run("finds state.json at root", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		statePath := filepath.Join(dir, "state.json")
		require.NoError(t, os.WriteFile(statePath, []byte("{}"), 0o600))

		got := findExperimentStatePath(dir)
		assert.Equal(t, statePath, got, "should find state.json at logsPath root")
	})

	t.Run("prefers state.jsonl at root", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0o600))
		statePath := filepath.Join(dir, "state.jsonl")
		require.NoError(t, os.WriteFile(statePath, []byte("{}"), 0o600))

		got := findExperimentStatePath(dir)
		assert.Equal(t, statePath, got, "should prefer state.jsonl at logsPath root")
	})

	t.Run("finds state.json in experiment subdirectory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		subDir := filepath.Join(dir, "experiment")
		require.NoError(t, os.MkdirAll(subDir, 0o755))
		statePath := filepath.Join(subDir, "state.json")
		require.NoError(t, os.WriteFile(statePath, []byte("{}"), 0o600))

		got := findExperimentStatePath(dir)
		assert.Equal(t, statePath, got, "should find state.json in experiment subdirectory")
	})

	t.Run("finds state.jsonl in usage artifact experiment subdirectory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		subDir := filepath.Join(dir, "usage", "experiment")
		require.NoError(t, os.MkdirAll(subDir, 0o755))
		statePath := filepath.Join(subDir, "state.jsonl")
		require.NoError(t, os.WriteFile(statePath, []byte("{}"), 0o600))

		got := findExperimentStatePath(dir)
		assert.Equal(t, statePath, got, "should find state.jsonl staged in the usage artifact")
	})

	t.Run("finds state.jsonl in prefixed experiment and usage directories", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		subDir := filepath.Join(dir, "abc123-usage", "experiment")
		require.NoError(t, os.MkdirAll(subDir, 0o755))
		statePath := filepath.Join(subDir, "state.jsonl")
		require.NoError(t, os.WriteFile(statePath, []byte("{}"), 0o600))
		assert.Equal(t, statePath, findExperimentStatePath(dir), "should find state.jsonl in workflow_call prefixed usage artifact")

		dir2 := t.TempDir()
		expDir := filepath.Join(dir2, "myworkflow-experiment")
		require.NoError(t, os.MkdirAll(expDir, 0o755))
		statePath2 := filepath.Join(expDir, "state.jsonl")
		require.NoError(t, os.WriteFile(statePath2, []byte("{}"), 0o600))
		assert.Equal(t, statePath2, findExperimentStatePath(dir2), "should find state.jsonl in workflow-ID prefixed experiment artifact")
	})

	t.Run("returns empty when no state.json exists", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		got := findExperimentStatePath(dir)
		assert.Empty(t, got, "should return empty string when no state.json found")
	})
}

func TestExtractExperimentData(t *testing.T) {
	t.Parallel()
	t.Run("returns nil for empty logsPath", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, extractExperimentData(""), "should return nil for empty logsPath")
	})

	t.Run("returns nil when no state.json present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		assert.Nil(t, extractExperimentData(dir), "should return nil when state.json missing")
	})

	t.Run("returns nil for invalid JSON", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte("not-json"), 0o600))
		assert.Nil(t, extractExperimentData(dir), "should return nil for invalid JSON")
	})

	t.Run("returns nil for empty counts", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"counts":{}}`), 0o600))
		assert.Nil(t, extractExperimentData(dir), "should return nil when counts map is empty")
	})

	t.Run("extracts single experiment with two variants", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := map[string]any{
			"counts": map[string]any{
				"caveman": map[string]int{"yes": 3, "no": 2},
			},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		assert.Equal(t, "yes", got.Assignments["caveman"], "variant with highest count should be selected")
		assert.Equal(t, 3, got.CumulativeCounts["caveman"]["yes"], "cumulative count for yes should be 3")
		assert.Equal(t, 2, got.CumulativeCounts["caveman"]["no"], "cumulative count for no should be 2")
	})

	t.Run("reads state.json from experiment subdirectory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		subDir := filepath.Join(dir, "experiment")
		require.NoError(t, os.MkdirAll(subDir, 0o755))
		state := map[string]any{
			"counts": map[string]any{
				"style": map[string]int{"concise": 1, "detailed": 2},
			},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData from subdir")
		assert.Equal(t, "detailed", got.Assignments["style"], "detailed has higher count so should be selected")
	})

	t.Run("reads state.jsonl run ledger", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		raw := []byte(`{"run_id":"0","timestamp":"2026-07-31T23:00:00Z","assignments":{"style":"concise"}}
{"run_id":"1","timestamp":"2026-08-01T00:00:00Z","assignments":{"style":"concise"}}
{"run_id":"2","timestamp":"2026-08-01T01:00:00Z","assignments":{"style":"detailed"}}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.jsonl"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		assert.Equal(t, "detailed", got.Assignments["style"], "latest run assignment should be used")
		assert.Equal(t, 2, got.CumulativeCounts["style"]["concise"], "ledger should count both concise runs")
		assert.Equal(t, 1, got.CumulativeCounts["style"]["detailed"], "jsonl run record should increment counts")
	})

	t.Run("extracts multiple experiments", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := map[string]any{
			"counts": map[string]any{
				"caveman": map[string]int{"yes": 1, "no": 0},
				"style":   map[string]int{"concise": 2, "detailed": 1},
			},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		assert.Len(t, got.Assignments, 2, "should have 2 experiment assignments")
		assert.Equal(t, "yes", got.Assignments["caveman"], "caveman should select yes (higher count)")
		assert.Equal(t, "concise", got.Assignments["style"], "style should select concise (higher count)")
	})
}

func TestFormatExperimentLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		exp      *ExperimentData
		expected string
	}{
		{
			name:     "nil returns empty string",
			exp:      nil,
			expected: "",
		},
		{
			name:     "empty assignments returns empty string",
			exp:      &ExperimentData{Assignments: map[string]string{}},
			expected: "",
		},
		{
			name:     "single experiment",
			exp:      &ExperimentData{Assignments: map[string]string{"style": "concise"}},
			expected: "style=concise",
		},
		{
			name:     "multiple experiments sorted alphabetically",
			exp:      &ExperimentData{Assignments: map[string]string{"style": "concise", "caveman": "yes"}},
			expected: "caveman=yes, style=concise",
		},
		{
			name:     "three experiments sorted",
			exp:      &ExperimentData{Assignments: map[string]string{"z": "1", "a": "2", "m": "3"}},
			expected: "a=2, m=3, z=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatExperimentLabel(tt.exp)
			assert.Equal(t, tt.expected, got, "formatExperimentLabel result mismatch")
		})
	}
}

func TestExperimentMatchesFilter(t *testing.T) {
	t.Parallel()
	exp := &ExperimentData{
		Assignments: map[string]string{
			"style":   "concise",
			"caveman": "yes",
		},
	}

	tests := []struct {
		name           string
		exp            *ExperimentData
		experimentName string
		variant        string
		want           bool
	}{
		{
			name:           "no filter passes nil exp",
			exp:            nil,
			experimentName: "",
			variant:        "",
			want:           true,
		},
		{
			name:           "no filter passes non-nil exp",
			exp:            exp,
			experimentName: "",
			variant:        "",
			want:           true,
		},
		{
			name:           "experiment filter passes when experiment present",
			exp:            exp,
			experimentName: "style",
			variant:        "",
			want:           true,
		},
		{
			name:           "experiment filter fails when experiment absent",
			exp:            exp,
			experimentName: "missing-experiment",
			variant:        "",
			want:           false,
		},
		{
			name:           "experiment filter fails when exp is nil",
			exp:            nil,
			experimentName: "style",
			variant:        "",
			want:           false,
		},
		{
			name:           "variant filter passes when variant matches",
			exp:            exp,
			experimentName: "style",
			variant:        "concise",
			want:           true,
		},
		{
			name:           "variant filter fails when variant does not match",
			exp:            exp,
			experimentName: "style",
			variant:        "verbose",
			want:           false,
		},
		{
			name:           "variant filter fails when experiment absent",
			exp:            exp,
			experimentName: "missing-experiment",
			variant:        "concise",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := experimentMatchesFilter(tt.exp, tt.experimentName, tt.variant)
			assert.Equal(t, tt.want, got, "experimentMatchesFilter result mismatch")
		})
	}
}

func TestFormatExperimentSkipMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		runID      int64
		experiment string
		variant    string
		wantSubstr string
	}{
		{
			name:       "experiment only message",
			runID:      12345,
			experiment: "style",
			variant:    "",
			wantSubstr: `experiment "style" not assigned`,
		},
		{
			name:       "experiment and variant message",
			runID:      12345,
			experiment: "style",
			variant:    "concise",
			wantSubstr: `not assigned variant "concise"`,
		},
		{
			name:       "run id is included",
			runID:      99999,
			experiment: "caveman",
			variant:    "",
			wantSubstr: "99999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatExperimentSkipMessage(tt.runID, tt.experiment, tt.variant)
			assert.Contains(t, got, tt.wantSubstr, "formatExperimentSkipMessage output mismatch")
		})
	}
}

func TestDeriveLastSelectedVariant(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		counts   map[string]int
		expected string
	}{
		{
			name:     "returns empty for nil map",
			counts:   map[string]int{},
			expected: "",
		},
		{
			name:     "single variant",
			counts:   map[string]int{"A": 5},
			expected: "A",
		},
		{
			name:     "highest count wins",
			counts:   map[string]int{"A": 2, "B": 5},
			expected: "B",
		},
		{
			name:     "ties broken by sorted order",
			counts:   map[string]int{"A": 3, "B": 3},
			expected: "A",
		},
		{
			name:     "three variants",
			counts:   map[string]int{"yes": 4, "no": 3, "maybe": 2},
			expected: "yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deriveLastSelectedVariant(tt.counts)
			assert.Equal(t, tt.expected, got, "deriveLastSelectedVariant result mismatch")
		})
	}
}

func TestFirstExperimentAssignment(t *testing.T) {
	t.Parallel()
	t.Run("returns false for nil", func(t *testing.T) {
		t.Parallel()
		name, variant, ok := firstExperimentAssignment(nil)
		assert.False(t, ok)
		assert.Empty(t, name)
		assert.Empty(t, variant)
	})

	t.Run("returns false for empty assignments", func(t *testing.T) {
		t.Parallel()
		name, variant, ok := firstExperimentAssignment(&ExperimentData{Assignments: map[string]string{}})
		assert.False(t, ok)
		assert.Empty(t, name)
		assert.Empty(t, variant)
	})

	t.Run("returns alphabetically first assignment", func(t *testing.T) {
		t.Parallel()
		exp := &ExperimentData{
			Assignments: map[string]string{
				"style":   "concise",
				"caveman": "yes",
			},
		}
		name, variant, ok := firstExperimentAssignment(exp)
		assert.True(t, ok)
		assert.Equal(t, "caveman", name)
		assert.Equal(t, "yes", variant)
	})
}

func TestExtractExperimentDataWithRuns(t *testing.T) {
	t.Parallel()
	t.Run("uses last run record when runs array is present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := map[string]any{
			"counts": map[string]any{
				"style": map[string]int{"concise": 3, "detailed": 2},
			},
			"runs": []map[string]any{
				{
					"run_id":      "100",
					"timestamp":   "2026-01-01T00:00:00Z",
					"assignments": map[string]string{"style": "detailed"},
				},
				{
					"run_id":      "101",
					"timestamp":   "2026-01-02T00:00:00Z",
					"assignments": map[string]string{"style": "concise"},
				},
			},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		// Should use the last run's assignment (concise), not the heuristic
		assert.Equal(t, "concise", got.Assignments["style"], "should use last run record assignment")
		assert.Equal(t, 3, got.CumulativeCounts["style"]["concise"], "cumulative counts should still be populated")
	})

	t.Run("falls back to heuristic when runs array is empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := map[string]any{
			"counts": map[string]any{
				"style": map[string]int{"concise": 3, "detailed": 2},
			},
			"runs": []map[string]any{},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		// Falls back to heuristic: highest count = concise
		assert.Equal(t, "concise", got.Assignments["style"], "should fall back to highest-count heuristic")
	})

	t.Run("falls back to heuristic when runs field is absent (legacy state)", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := map[string]any{
			"counts": map[string]any{
				"caveman": map[string]int{"yes": 5, "no": 3},
			},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData for legacy state")
		assert.Equal(t, "yes", got.Assignments["caveman"], "should use heuristic for legacy state")
	})

	t.Run("skips last run record with empty assignments", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := map[string]any{
			"counts": map[string]any{
				"style": map[string]int{"concise": 3, "detailed": 2},
			},
			"runs": []map[string]any{
				{
					"run_id":      "100",
					"timestamp":   "2026-01-01T00:00:00Z",
					"assignments": map[string]string{},
				},
			},
		}
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		// Falls back to heuristic when last run's assignments map is empty
		assert.Equal(t, "concise", got.Assignments["style"], "should fall back to heuristic for empty assignments")
	})
}

func TestExtractExperimentDataFallsBackToUsageSummary(t *testing.T) {
	t.Parallel()
	t.Run("reads assignments from usage activity summary when no state file present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		usageDir := filepath.Join(dir, "usage", "activity")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))

		summary := map[string]any{
			"schema": "usage-activity-summary/v1",
			"experiments": map[string]any{
				"assignments": map[string]string{"style": "concise", "caveman": "yes"},
			},
		}
		raw, err := json.Marshal(summary)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "summary.json"), raw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData from usage summary")
		assert.Equal(t, "concise", got.Assignments["style"])
		assert.Equal(t, "yes", got.Assignments["caveman"])
		assert.Nil(t, got.CumulativeCounts, "usage summary fallback does not have cumulative counts")
	})

	t.Run("prefers state file over usage summary when both exist", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		// Write experiment state file
		state := map[string]any{
			"counts": map[string]any{
				"style": map[string]int{"detailed": 3, "concise": 1},
			},
		}
		stateRaw, err := json.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "state.json"), stateRaw, 0o600))

		// Write usage activity summary with different assignments
		usageDir := filepath.Join(dir, "usage", "activity")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))
		summary := map[string]any{
			"schema": "usage-activity-summary/v1",
			"experiments": map[string]any{
				"assignments": map[string]string{"style": "concise"},
			},
		}
		summaryRaw, err := json.Marshal(summary)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "summary.json"), summaryRaw, 0o600))

		got := extractExperimentData(dir)
		require.NotNil(t, got, "should return non-nil ExperimentData")
		// Should use state file (highest count = detailed)
		assert.Equal(t, "detailed", got.Assignments["style"], "should prefer state file over usage summary")
	})

	t.Run("returns nil when usage summary has no experiments field", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		usageDir := filepath.Join(dir, "usage", "activity")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))

		summary := map[string]any{
			"schema":   "usage-activity-summary/v1",
			"firewall": map[string]any{"total_requests": 10},
		}
		raw, err := json.Marshal(summary)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "summary.json"), raw, 0o600))

		got := extractExperimentData(dir)
		assert.Nil(t, got, "should return nil when usage summary has no experiments")
	})
}
