package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeForecastJSONLRecord(t *testing.T, path string, run RunData) {
	t.Helper()
	record, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRun,
		Run:           &cachedLogsJSONLRunData{RunData: run},
	})
	require.NoError(t, err)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.Write(append(record, '\n'))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestResolveForecastJSONLInputsSupportsFilesDirectoriesAndGlobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	require.NoError(t, os.Mkdir(nested, 0o700))
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(nested, "second.jsonl")
	require.NoError(t, os.WriteFile(first, nil, 0o600))
	require.NoError(t, os.WriteFile(second, nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignored.txt"), nil, 0o600))

	files, err := resolveForecastJSONLInputs([]string{first, nested, filepath.Join(dir, "*.jsonl")})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{first, second}, files)
}

func TestForecastJSONLHistoryDeduplicatesAndKeepsRepositoryScopes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := filepath.Join(dir, "a.jsonl")
	second := filepath.Join(dir, "b.jsonl")
	when := time.Now().Add(-24 * time.Hour).UTC()
	writeForecastJSONLRecord(t, first, RunData{
		RunID: 1, Repository: "octo/one", WorkflowName: "Build", WorkflowPath: ".github/workflows/build.yml",
		Status: "completed", Conclusion: "success", CreatedAt: when, AIC: 1,
	})
	writeForecastJSONLRecord(t, second, RunData{
		RunID: 1, Repository: "octo/one", WorkflowName: "Build", WorkflowPath: ".github/workflows/build.yml",
		Status: "completed", Conclusion: "success", CreatedAt: when, AIC: 2,
	})
	writeForecastJSONLRecord(t, second, RunData{
		RunID: 2, Repository: "octo/two", WorkflowName: "Build", WorkflowPath: ".github/workflows/build.yml",
		Status: "completed", Conclusion: "success", CreatedAt: when, AIC: 3,
	})

	history, err := loadForecastJSONLHistory([]string{filepath.Join(dir, "*.jsonl")})
	require.NoError(t, err)
	assert.Len(t, history.runs, 2)
	assert.Equal(t, 1, history.duplicates)
	assert.InDelta(t, 2.0, history.runs[1].AIC, 1e-9)

	targets, err := history.targets(nil, "")
	require.NoError(t, err)
	require.Len(t, targets, 2)
	assert.NotEqual(t, targets[0].repository, targets[1].repository)
}

func TestForecastWorkflowFromJSONLUsesEmbeddedAIC(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	now := time.Now().UTC()
	for id, aic := range []float64{1, 2, 3} {
		writeForecastJSONLRecord(t, path, RunData{
			RunID: int64(id + 1), Repository: "octo/repo", WorkflowName: "Build",
			Status: "completed", Conclusion: "success", CreatedAt: now.Add(time.Duration(-id-1) * time.Hour), AIC: aic,
		})
	}
	history, err := loadForecastJSONLHistory([]string{path})
	require.NoError(t, err)
	config := ForecastConfig{Days: 7, Period: "week", SampleSize: 100, history: history}
	result, truncated, err := forecastWorkflowFromJSONL(t.Context(), forecastWorkflowTarget{
		repository: "octo/repo", name: "Build",
	}, now.AddDate(0, 0, -7), time.Time{}, config, 7)
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Equal(t, 3, result.SampledRuns)
	assert.InDelta(t, 2.0, result.AvgAIC, 1e-9)
	assert.Equal(t, "octo/repo", result.Repository)
	require.NotNil(t, result.WorkflowRunAPI)
}

func TestForecastCommandLogsJSONLIsRepeatable(t *testing.T) {
	t.Parallel()
	command := NewForecastCommand()
	flag := command.Flags().Lookup("logs-jsonl")
	require.NotNil(t, flag)
	assert.Equal(t, "stringArray", flag.Value.Type())
}
