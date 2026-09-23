//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCachedLogsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	first, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRun,
		Run:           &cachedLogsJSONLRunData{RunData: RunData{RunID: 42, WorkflowName: "cached-workflow"}},
	})
	require.NoError(t, err)
	second, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLSchemaVersion,
		Kind:          cachedLogsJSONLKindRun,
		Run:           &cachedLogsJSONLRunData{RunData: RunData{RunID: 0, WorkflowName: "invalid"}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(append(first, '\n'), append(second, '\n')...), 0o600))

	runs, err := loadCachedLogsJSONL(path)

	require.NoError(t, err)
	require.Len(t, runs.runs, 1)
	assert.Equal(t, "cached-workflow", runs.runs[42].WorkflowName)
}

func TestLoadCachedLogsJSONReportsFoundFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":42}}\n"), 0o600))

	_, stderr := captureOutput(t, func() error {
		_, err := loadCachedLogsJSONL(path)
		return err
	})

	assert.Contains(t, stderr, "Found cached logs JSONL file: "+path)
	assert.Contains(t, stderr, "lines=1, runs=1, workflow_run_lists=0")
}

func TestLoadCachedLogsJSONIgnoresMissingFile(t *testing.T) {
	runs, err := loadCachedLogsJSONL(filepath.Join(t.TempDir(), "missing.jsonl"))

	require.NoError(t, err)
	assert.Nil(t, runs)
}

func TestLoadCachedLogsJSONReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")

	_, stderr := captureOutput(t, func() error {
		_, err := loadCachedLogsJSONL(path)
		return err
	})

	assert.Contains(t, stderr, "Cached logs JSONL file not found: "+path)
}

func TestLoadCachedLogsJSONRejectsInvalidInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{invalid}\n{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":42}}\n"), 0o600))

	_, err := loadCachedLogsJSONL(path)

	require.ErrorContains(t, err, "record 1")
}

func TestLoadCachedLogsJSONRejectsInvalidRunAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":42,\"run_attempt\":\"bogus\"}}\n"), 0o600))

	_, err := loadCachedLogsJSONL(path)

	require.ErrorContains(t, err, "invalid run_attempt")
}

func TestCachedLogsJSONLWriterAppendsImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)

	require.NoError(t, writer.Append(ProcessedRun{Run: WorkflowRun{DatabaseID: 42, WorkflowName: "updated-workflow"}}))

	runs, err := loadCachedLogsJSONL(path)
	require.NoError(t, err)
	require.Contains(t, runs.runs, int64(42))
	assert.Equal(t, "updated-workflow", runs.runs[42].WorkflowName)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCachedLogsJSONLWriterIncludesSafeDashboardEvidence(t *testing.T) {
	runDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_info.json"), []byte(`{
		"engine_id": "copilot",
		"engine_name": "Copilot",
		"model": "gpt-5",
		"version": "1.2.3",
		"cli_version": "0.99.0",
		"awf_version": "0.20.0",
		"awmg_version": "0.30.0",
		"agent_runtime": "cloud-hypervisor"
	}`), 0o600))
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)
	startedAt := time.Date(2026, time.September, 9, 4, 0, 1, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	const sensitiveError = "******"

	require.NoError(t, writer.Append(ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   303,
			Repository:   "githubnext/gh-aw-cao",
			WorkflowName: "Dashboard",
			WorkflowPath: ".github/workflows/dashboard.md",
			Status:       "completed",
			Conclusion:   "success",
			Attempt:      1,
			CreatedAt:    startedAt,
			UpdatedAt:    completedAt,
			LogsPath:     runDir,
		},
		JobDetails: []JobInfoWithDuration{{
			JobInfo: JobInfo{
				ID:          404,
				RunAttempt:  1,
				Name:        "agent",
				Status:      "completed",
				Conclusion:  "success",
				StartedAt:   startedAt,
				CompletedAt: completedAt,
				RunnerName:  "sensitive-runner-name",
			},
		}},
		MCPToolUsage: &MCPToolUsageData{ToolCalls: []MCPToolCall{{
			ToolCallID: "call-7",
			Timestamp:  "2026-09-09T04:00:15Z",
			ServerName: "github",
			ToolName:   "get_file",
			InputSize:  128,
			OutputSize: 1024,
			Status:     "success",
			Error:      sensitiveError,
		}}},
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), sensitiveError)
	assert.NotContains(t, string(data), "sensitive-runner-name")

	var record cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &record))
	require.NotNil(t, record.Run)
	assert.Equal(t, "1.2.3", record.Run.EngineVersion)
	assert.Equal(t, "gpt-5", record.Run.Model)
	assert.Equal(t, "0.99.0", record.Run.GhAwVersion)
	assert.Equal(t, "cloud-hypervisor", record.Run.AgentRuntime)
	assert.Equal(t, "0.20.0", record.Run.FirewallVersion)
	assert.Equal(t, "0.30.0", record.Run.GatewayVersion)
	require.Len(t, record.Run.JobDetails, 1)
	assert.Equal(t, int64(404), record.Run.JobDetails[0].ID)
	assert.Equal(t, "agent", record.Run.JobDetails[0].Name)
	require.NotNil(t, record.Run.MCPToolUsage)
	require.Len(t, record.Run.MCPToolUsage.ToolCalls, 1)
	assert.Equal(t, "call-7", record.Run.MCPToolUsage.ToolCalls[0].ToolCallID)
	assert.Equal(t, "github", record.Run.MCPToolUsage.ToolCalls[0].ServerName)
	assert.Equal(t, "get_file", record.Run.MCPToolUsage.ToolCalls[0].ToolName)
}

func TestCachedLogsJSONLWriterIncludesAuditArtifacts(t *testing.T) {
	runDir := t.TempDir()
	run := ProcessedRun{Run: WorkflowRun{
		DatabaseID: 42,
		Status:     "completed",
		Conclusion: "success",
		LogsPath:   runDir,
	}}
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_info.json"), []byte(`{
		"engine_id": "copilot",
		"engine_name": "Copilot",
		"model": "gpt-5"
	}`), 0o600))
	audit := AuditData{
		CacheSource: auditCacheSourceLogs,
		Overview: OverviewData{
			RunID:      run.Run.DatabaseID,
			Status:     run.Run.Status,
			Conclusion: run.Run.Conclusion,
		},
		CreatedItems: []CreatedItemReport{{
			Type:      "create_issue",
			URL:       "https://github.com/github/gh-aw/issues/1",
			Timestamp: "2026-09-11T04:00:00Z",
		}},
	}
	require.NoError(t, writeAuditData(runDir, audit))
	path := filepath.Join(t.TempDir(), "logs.jsonl")

	require.NoError(t, newCachedLogsJSONLWriter(path).AppendAudit(run))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	require.Len(t, lines, 2)
	var record cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(lines[0], &record))
	require.NotNil(t, record.Run)
	require.NotNil(t, record.Run.Audit)
	assert.Equal(t, int64(42), record.Run.Audit.Overview.RunID)
	require.NotNil(t, record.Run.AwInfo)
	assert.Equal(t, "copilot", record.Run.AwInfo.EngineID)
	require.Len(t, record.Run.SafeOutputs, 1)
	assert.Equal(t, "create_issue", record.Run.SafeOutputs[0].Type)

	var safeOutputRecord cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(lines[1], &safeOutputRecord))
	wantSafeOutputKind := cachedLogsJSONLKindSafeOutput
	assert.Equal(t, wantSafeOutputKind, safeOutputRecord.Kind)
	require.NotNil(t, safeOutputRecord.SafeOutput)
	assert.Equal(t, int64(42), safeOutputRecord.SafeOutput.RunID)
	assert.Equal(t, "create_issue", safeOutputRecord.SafeOutput.Type)
}

func TestCachedLogsJSONLWriterIncludesSafeOutputsWithoutAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	run := ProcessedRun{
		Run: WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success"},
		SafeOutputs: []CreatedItemReport{{
			Type:       "linear_create_issue",
			Provider:   "linear",
			ID:         "issue-id",
			Identifier: "ENG-7",
			Timestamp:  "2026-09-14T00:00:00Z",
		}},
	}

	require.NoError(t, newCachedLogsJSONLWriter(path).Append(run))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	require.Len(t, lines, 2)
	var record cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(lines[0], &record))
	require.Len(t, record.Run.SafeOutputs, 1)
	assert.Equal(t, "linear", record.Run.SafeOutputs[0].Provider)
	assert.Equal(t, "ENG-7", record.Run.SafeOutputs[0].Identifier)
	assert.Nil(t, record.Run.Audit)

	var safeOutputRecord cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(lines[1], &safeOutputRecord))
	wantSafeOutputKind := cachedLogsJSONLKindSafeOutput
	assert.Equal(t, wantSafeOutputKind, safeOutputRecord.Kind)
	require.NotNil(t, safeOutputRecord.SafeOutput)
	assert.Equal(t, int64(42), safeOutputRecord.SafeOutput.RunID)
	assert.Equal(t, "linear", safeOutputRecord.SafeOutput.Provider)
	assert.Equal(t, "ENG-7", safeOutputRecord.SafeOutput.Identifier)
	assert.Equal(t, "issue-id", safeOutputRecord.SafeOutput.ID)
}

func TestCachedLogsJSONLWriterEmitsOneEventPerSafeOutputItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	run := ProcessedRun{
		Run: WorkflowRun{DatabaseID: 99, Status: "completed", Conclusion: "success"},
		SafeOutputs: []CreatedItemReport{
			{Type: "create_issue", Provider: "github", Number: 1, Timestamp: "2026-09-14T00:00:00Z"},
			{Type: "add_labels", Provider: "github", Number: 1, Timestamp: "2026-09-14T00:00:01Z"},
			{Type: "linear_add_comment", Provider: "linear", Identifier: "ENG-1", Timestamp: "2026-09-14T00:00:02Z"},
		},
	}

	require.NoError(t, newCachedLogsJSONLWriter(path).Append(run))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	// One "run" record plus one "safe_output_item" record per entity.
	require.Len(t, lines, 4)

	var runRecord cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(lines[0], &runRecord))
	wantRunKind := cachedLogsJSONLKindRun
	assert.Equal(t, wantRunKind, runRecord.Kind)
	require.Len(t, runRecord.Run.SafeOutputs, 3)

	wantTypes := []string{"create_issue", "add_labels", "linear_add_comment"}
	wantSafeOutputKind := cachedLogsJSONLKindSafeOutput
	for i, line := range lines[1:] {
		var record cachedLogsJSONLRecord
		require.NoError(t, json.Unmarshal(line, &record))
		assert.Equal(t, wantSafeOutputKind, record.Kind)
		require.NotNil(t, record.SafeOutput)
		assert.Equal(t, int64(99), record.SafeOutput.RunID)
		assert.Equal(t, wantTypes[i], record.SafeOutput.Type)
	}

	// Individual safe-output events are informational only and must not create
	// spurious entries in the in-memory run cache when read back.
	cache, err := loadCachedLogsJSONL(path)
	require.NoError(t, err)
	require.Contains(t, cache.runs, int64(99))
	assert.Len(t, cache.runs, 1)
}

func TestCachedLogsJSONLWriterOmitsSafeOutputEventsWhenNoItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	run := ProcessedRun{Run: WorkflowRun{DatabaseID: 7, Status: "completed", Conclusion: "success"}}

	require.NoError(t, newCachedLogsJSONLWriter(path).Append(run))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	require.Len(t, lines, 1)
	var record cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(lines[0], &record))
	wantRunKind := cachedLogsJSONLKindRun
	assert.Equal(t, wantRunKind, record.Kind)
}

func TestProjectCachedLogsJSONLEvidenceSkipsIncompleteEntries(t *testing.T) {
	jobs := projectCachedLogsJSONLJobs([]JobInfoWithDuration{
		{JobInfo: JobInfo{ID: 0, Name: "missing-id"}},
		{JobInfo: JobInfo{ID: 1}},
		{JobInfo: JobInfo{ID: 2, Name: "agent"}},
	})
	require.Len(t, jobs, 1)
	assert.Equal(t, int64(2), jobs[0].ID)

	usage := projectCachedLogsJSONLMCPToolUsage(&MCPToolUsageData{ToolCalls: []MCPToolCall{
		{ServerName: "github", ToolName: "missing-timestamp"},
		{Timestamp: "2026-09-09T04:00:15Z"},
		{Timestamp: "2026-09-09T04:00:16Z", ServerName: "github", ToolName: "get_file"},
	}})
	require.NotNil(t, usage)
	require.Len(t, usage.ToolCalls, 1)
	assert.Equal(t, "get_file", usage.ToolCalls[0].ToolName)
}

func TestPrepareCachedLogsJSONLLoadsOnceAndOnlyAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	first := []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":42}}\n")
	require.NoError(t, os.WriteFile(path, first, 0o600))
	opts := LogsDownloadOptions{CachedJSONL: path}

	require.NoError(t, prepareCachedLogsJSONL(&opts))
	cache := opts.cachedJSONLCache
	require.Contains(t, cache.runs, int64(42))

	require.NoError(t, opts.cachedJSONLWriter.Append(ProcessedRun{Run: WorkflowRun{DatabaseID: 43}}))
	require.NoError(t, prepareCachedLogsJSONL(&opts))

	assert.Same(t, cache, opts.cachedJSONLCache)
	assert.NotContains(t, opts.cachedJSONLCache.runs, int64(43))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, first))
	assert.Equal(t, 2, bytes.Count(bytes.TrimSpace(data), []byte{'\n'})+1)
}

func TestPrepareCachedLogsJSONLWildcardLoadsMatchingFilesAndWritesUniqueFile(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "logs-1.jsonl")
	secondPath := filepath.Join(dir, "logs-2.jsonl")
	require.NoError(t, os.WriteFile(firstPath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":1}}\n"), 0o600))
	require.NoError(t, os.WriteFile(secondPath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":2}}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logs-ignored.json"), []byte("{}\n"), 0o600))

	prepared, err := prepareCachedLogsJSONLPath(filepath.Join(dir, "logs-*"))

	require.NoError(t, err)
	require.NotNil(t, prepared.cache)
	require.NotNil(t, prepared.writer)
	assert.True(t, prepared.wildcard)
	assert.ElementsMatch(t, []string{firstPath, secondPath}, prepared.sourcePaths)
	assert.Contains(t, prepared.cache.runs, int64(1))
	assert.Contains(t, prepared.cache.runs, int64(2))
	assert.Regexp(t, `^logs-\d+-[a-f0-9]{16}\.jsonl$`, filepath.Base(prepared.writer.path))
	assert.NotContains(t, []string{firstPath, secondPath}, prepared.writer.path)

	require.NoError(t, prepared.writer.Append(ProcessedRun{Run: WorkflowRun{DatabaseID: 3}}))
	_, err = os.Stat(prepared.writer.path)
	require.NoError(t, err)
	assert.FileExists(t, firstPath)
	assert.FileExists(t, secondPath)
}

func TestResolveCachedLogsJSONLPathsRejectsNonTrailingWildcard(t *testing.T) {
	_, _, _, err := resolveCachedLogsJSONLPaths(filepath.Join(t.TempDir(), "logs-*-old"))

	require.ErrorContains(t, err, "trailing prefix match")
}

func TestSortCachedLogsJSONLSourcePathsUsesEmbeddedUnixTimeAsTieBreaker(t *testing.T) {
	dir := t.TempDir()
	olderPath := filepath.Join(dir, "logs-100-b.jsonl")
	newerPath := filepath.Join(dir, "logs-200-a.jsonl")
	manualPath := filepath.Join(dir, "logs-manual.jsonl")
	for _, path := range []string{newerPath, manualPath, olderPath} {
		require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
		require.NoError(t, os.Chtimes(path, time.Unix(300, 0), time.Unix(300, 0)))
	}
	paths := []string{newerPath, manualPath, olderPath}

	sortCachedLogsJSONLSourcePaths(paths, "logs-")

	assert.Equal(t, []string{olderPath, newerPath, manualPath}, paths)
}

func TestPruneCachedLogsJSONLWildcardSourcesDeletesFilesWithoutInRangeRuns(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "logs-old.jsonl")
	mixedPath := filepath.Join(dir, "logs-mixed.jsonl")
	undatedPath := filepath.Join(dir, "logs-undated.jsonl")
	unrelatedPath := filepath.Join(dir, "other.jsonl")
	require.NoError(t, os.WriteFile(oldPath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":1,\"created_at\":\"2026-08-31T23:59:59Z\"}}\n"), 0o600))
	require.NoError(t, os.WriteFile(mixedPath, []byte(
		"{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":2,\"created_at\":\"2026-08-31T23:59:59Z\"}}\n"+
			"{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":3,\"created_at\":\"2026-09-05T00:00:00Z\"}}\n",
	), 0o600))
	require.NoError(t, os.WriteFile(undatedPath, []byte(
		"{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":4}}\n"+
			"{\"schema_version\":2,\"kind\":\"github_api_rate_limit\",\"rate_limit\":{\"host\":\"github.com\"}}\n",
	), 0o600))
	require.NoError(t, os.WriteFile(unrelatedPath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":5,\"created_at\":\"2026-08-31T23:59:59Z\"}}\n"), 0o600))

	require.NoError(t, pruneCachedLogsJSONLWildcardSources([]string{oldPath, mixedPath, undatedPath}, true, "2026-09-01", "2026-09-10"))

	assert.NoFileExists(t, oldPath)
	assert.FileExists(t, mixedPath)
	assert.FileExists(t, undatedPath)
	assert.FileExists(t, unrelatedPath)
}

func TestFinalizeCachedLogsJSONLDeletesExpiredWildcardShards(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "logs-old.jsonl")
	currentPath := filepath.Join(dir, "logs-current.jsonl")
	futurePath := filepath.Join(dir, "logs-future.jsonl")
	require.NoError(t, os.WriteFile(oldPath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":1,\"created_at\":\"2026-08-31T23:59:59Z\"}}\n"), 0o600))
	require.NoError(t, os.WriteFile(currentPath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":2,\"created_at\":\"2026-09-05T00:00:00Z\"}}\n"), 0o600))
	require.NoError(t, os.WriteFile(futurePath, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":3,\"created_at\":\"2026-09-11T00:00:00Z\"}}\n"), 0o600))

	prepared, err := prepareCachedLogsJSONLPath(filepath.Join(dir, "logs-*"))
	require.NoError(t, err)
	require.NoError(t, prepared.writer.Append(ProcessedRun{Run: WorkflowRun{
		DatabaseID: 4,
		CreatedAt:  time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC),
	}}))

	require.NoError(t, finalizeCachedLogsJSONL(prepared.writer, prepared.sourcePaths, prepared.wildcard, "2026-09-01", "2026-09-10"))

	assert.NoFileExists(t, oldPath)
	assert.FileExists(t, currentPath)
	assert.NoFileExists(t, futurePath)
	assert.FileExists(t, prepared.writer.path)
}

func TestCachedLogsJSONLWriterFiltersAppendedContentByDateRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	oldRun := `{"schema_version":2,"kind":"run","run":{"run_id":1,"created_at":"2026-08-31T23:59:59Z","future_field":"preserved"}}`
	firstIncludedRun := `{"schema_version":2,"kind":"run","run":{"run_id":2,"created_at":"2026-09-01T00:00:00Z"}}`
	lastIncludedRun := `{"schema_version":2,"kind":"run","run":{"run_id":3,"created_at":"2026-09-10T23:59:59Z"}}`
	futureRun := `{"schema_version":2,"kind":"run","run":{"run_id":4,"created_at":"2026-09-11T00:00:00Z"}}`
	workflowRuns := `{"schema_version":2,"kind":"workflow_runs","request":{"host":"github.com","repository":"github/gh-aw","args":["run","list"]},"payload":[{"databaseId":1}]}`
	rateLimit := `{"schema_version":2,"kind":"github_api_rate_limit","rate_limit":{"host":"github.com"}}`
	unknown := `{"schema_version":99,"kind":"future","value":"preserved"}`
	withoutCreatedAt := `{"schema_version":2,"kind":"run","run":{"run_id":5}}`
	futureSchemaRun := `{"schema_version":99,"kind":"run","run":{"run_id":7,"created_at":"2026-08-31T00:00:00Z"}}`
	olderSchemaRun := `{"schema_version":1,"kind":"run","run":{"run_id":8,"created_at":"2026-08-31T00:00:00Z"}}`
	previous := strings.Join([]string{oldRun, firstIncludedRun, lastIncludedRun, futureRun, workflowRuns, rateLimit, unknown, withoutCreatedAt, futureSchemaRun, olderSchemaRun}, "\n") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(previous), 0o600))
	writer := newCachedLogsJSONLWriter(path)
	appendedAt := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)

	require.NoError(t, writer.Append(ProcessedRun{Run: WorkflowRun{DatabaseID: 6, CreatedAt: appendedAt}}))
	require.NoError(t, writer.filterDateRange("2026-09-01", "2026-09-10"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.NotContains(t, content, `"run_id":1`)
	assert.Contains(t, content, firstIncludedRun)
	assert.Contains(t, content, lastIncludedRun)
	assert.NotContains(t, content, `"run_id":4`)
	assert.Contains(t, content, workflowRuns)
	assert.Contains(t, content, rateLimit)
	assert.Contains(t, content, unknown)
	assert.Contains(t, content, withoutCreatedAt)
	assert.Contains(t, content, futureSchemaRun)
	assert.Contains(t, content, olderSchemaRun)
	assert.Contains(t, content, `"run_id":6`)
	assert.Contains(t, content, appendedAt.Format(time.RFC3339))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCachedLogsJSONLStoresCompleteWorkflowRunsPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)
	request := cachedWorkflowRunsRequest{
		Host:       "github.com",
		Repository: "github/gh-aw",
		Args:       []string{"run", "list", "--limit", "2"},
	}
	payload := []byte("[\n  {\"databaseId\":42,\"futureField\":{\"nested\":true}},\n  {\"databaseId\":41}\n]")

	require.NoError(t, writer.AppendWorkflowRuns(request, payload))

	cache, err := loadCachedLogsJSONL(path)
	require.NoError(t, err)
	cached, ok := cache.lookupWorkflowRuns(request)
	require.True(t, ok)
	assert.JSONEq(t, string(payload), string(cached))
	assert.Contains(t, string(cached), `"futureField"`)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	require.Len(t, lines, 1)
	assert.True(t, json.Valid(lines[0]))
}

func TestLoadCachedLogsJSONLIgnoresIncompatibleSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	data := "{\"schema_version\":0,\"run\":{\"run_id\":41}}\n" +
		"{\"schema_version\":1,\"run\":{\"run_id\":42}}\n" +
		"{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":43}}\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	runs, err := loadCachedLogsJSONL(path)

	require.NoError(t, err)
	require.Len(t, runs.runs, 1)
	assert.Contains(t, runs.runs, int64(43))
	assert.NotContains(t, runs.runs, int64(42))
}

func TestCachedLogsJSONLStoresRateLimitAsOneLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)
	report := GitHubAPIRateLimitReport{
		Host:  "github.com",
		Start: &GitHubAPIRateLimitState{Limit: 5000, Remaining: 4999, Used: 1, Reset: 123},
		End:   &GitHubAPIRateLimitState{Limit: 5000, Remaining: 4990, Used: 10, Reset: 123},
	}

	require.NoError(t, writer.AppendRateLimit(report))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	require.Len(t, lines, 1)
	assert.True(t, json.Valid(lines[0]))
	assert.Contains(t, string(lines[0]), `"kind":"github_api_rate_limit"`)
	assert.Contains(t, string(lines[0]), `"remaining":4990`)
}

func TestCachedLogsJSONLExistingRecordAvoidsDuplicateWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, writer.Append(ProcessedRun{Run: WorkflowRun{
		DatabaseID: 42, Repository: "github/gh-aw", Status: "completed",
		Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt,
	}}))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	cached, err := loadCachedLogsJSONL(path)
	require.NoError(t, err)
	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{{
		DatabaseID: 42, Repository: "github/gh-aw", Status: "completed",
		Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt,
	}}, runArtifactsConcurrentOptions{
		outputDir:    t.TempDir(),
		maxRuns:      1,
		cachedRuns:   cached.runs,
		storageLimit: newLogsStorageLimit(t.TempDir(), 0, false),
	})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].CachedRun)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestCachedLogsCollectorDoesNotRewriteCachedRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)
	cached := RunData{RunID: 42}
	collector := &orderedLogsRunCollector{
		candidates: []ProcessedRun{
			{Run: WorkflowRun{DatabaseID: 41}},
			{Run: WorkflowRun{DatabaseID: 42}, cachedData: &cached},
		},
		accepted: []bool{true, true},
	}

	processedRuns, batchProcessed := collector.appendAccepted(nil, 0, writer)

	require.Len(t, processedRuns, 2)
	assert.Equal(t, 2, batchProcessed)
	cache, err := loadCachedLogsJSONL(path)
	require.NoError(t, err)
	require.Contains(t, cache.runs, int64(41))
	assert.NotContains(t, cache.runs, int64(42))
}

func TestCachedLogsJSONLWriterSerializesConcurrentAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.jsonl")
	writer := newCachedLogsJSONLWriter(path)
	var group sync.WaitGroup
	errs := make(chan error, 20)
	for id := int64(1); id <= 20; id++ {
		group.Go(func() {
			errs <- writer.Append(ProcessedRun{Run: WorkflowRun{DatabaseID: id}})
		})
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	runs, err := loadCachedLogsJSONL(path)
	require.NoError(t, err)
	assert.Len(t, runs.runs, 20)
}

func TestCachedLogsLookupHonorsRepositoryAndFilters(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	runs := cachedLogsRuns{
		42: {RunData: RunData{RunID: 42, Repository: "github/gh-aw", EngineID: "copilot", Status: "completed", Conclusion: "success", RunAttempt: "1", UpdatedAt: updatedAt}},
	}
	run := WorkflowRun{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt}

	_, ok := runs.lookup(run, runFilterOpts{engine: "copilot"})
	assert.True(t, ok)
	_, ok = runs.lookup(run, runFilterOpts{engine: "claude"})
	assert.False(t, ok)
	_, ok = runs.lookup(run, runFilterOpts{runtime: "cloud-hypervisor"})
	assert.False(t, ok)
	_, ok = runs.lookup(WorkflowRun{DatabaseID: 42, Repository: "other/repo", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt}, runFilterOpts{})
	assert.False(t, ok)
}

func TestCachedLogsLookupRejectsChangedRun(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	runs := cachedLogsRuns{
		42: {
			RunData: RunData{
				RunID:      42,
				Status:     "completed",
				Conclusion: "success",
				RunAttempt: "1",
				UpdatedAt:  updatedAt,
			},
		},
	}

	tests := []WorkflowRun{
		{DatabaseID: 42, Status: "in_progress", Conclusion: "", Attempt: 1, UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "failure", Attempt: 1, UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "success", Attempt: 2, UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt.Add(time.Minute)},
	}
	for _, run := range tests {
		_, ok := runs.lookup(run, runFilterOpts{})
		assert.False(t, ok)
	}
}

func TestCachedLogsLookupRejectsUnknownIdentity(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	runs := cachedLogsRuns{
		42: {RunData: RunData{RunID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", RunAttempt: "1", UpdatedAt: updatedAt}},
		43: {RunData: RunData{RunID: 43, Status: "completed", Conclusion: "success", UpdatedAt: updatedAt}},
	}

	tests := []WorkflowRun{
		{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", UpdatedAt: updatedAt},
		{DatabaseID: 42, Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: updatedAt},
		{DatabaseID: 43, Status: "completed", Conclusion: "success", UpdatedAt: updatedAt},
	}
	for _, run := range tests {
		_, ok := runs.lookup(run, runFilterOpts{})
		assert.False(t, ok)
	}
}

func TestCachedJSONLCanSatisfy(t *testing.T) {
	usageFilter := []string{constants.UsageArtifactName.String()}
	agentFilter := []string{constants.AgentArtifactName.String()}
	assert.True(t, cachedJSONLCanSatisfy(usageFilter, false, false, false))
	assert.False(t, cachedJSONLCanSatisfy(agentFilter, false, false, false))
	assert.False(t, cachedJSONLCanSatisfy(nil, false, false, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, true, false, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, false, true, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, false, false, true))
}

func TestDownloadRunArtifactsConcurrentReusesCachedJSONRecord(t *testing.T) {
	originalProcess := processConcurrentRunDownload
	t.Cleanup(func() { processConcurrentRunDownload = originalProcess })
	processConcurrentRunDownload = func(context.Context, WorkflowRun, concurrentRunDownloadParams, *atomic.Int64, *console.ProgressBar) (DownloadResult, error) {
		t.Fatal("cached run should not be downloaded")
		return DownloadResult{}, nil
	}

	cached := RunData{
		RunID:        42,
		WorkflowName: "cached-workflow",
		Repository:   "github/gh-aw",
		Status:       "completed",
		Conclusion:   "success",
		RunAttempt:   "1",
		UpdatedAt:    time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		LogsPath:     "/previous/run-42",
	}
	cachedAudit := &AuditData{Overview: OverviewData{RunID: 42}}

	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: cached.UpdatedAt}}, runArtifactsConcurrentOptions{
		outputDir:    t.TempDir(),
		maxRuns:      1,
		cachedRuns:   cachedLogsRuns{42: {RunData: cached, Audit: cachedAudit}},
		storageLimit: newLogsStorageLimit(t.TempDir(), 0, false),
	})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].CachedRun)
	assert.True(t, results[0].Cached)
	assert.Equal(t, cached, *results[0].CachedRun)
	assert.Same(t, cachedAudit, results[0].cachedAudit)
}

func TestPrepareLogsDataAuditUsesCachedDataBestEffort(t *testing.T) {
	outputDir := t.TempDir()
	baseline := RunData{
		RunID:        41,
		WorkflowName: "cached-workflow",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
	}
	cached := RunData{
		RunID:        42,
		WorkflowName: "cached-workflow",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    baseline.CreatedAt.Add(time.Minute),
		UpdatedAt:    baseline.UpdatedAt,
	}
	baselineAudit := &AuditData{
		CacheSource: auditCacheSourceLogs,
		Overview: OverviewData{
			RunID:        baseline.RunID,
			WorkflowName: baseline.WorkflowName,
			Status:       baseline.Status,
			Conclusion:   baseline.Conclusion,
			CreatedAt:    baseline.CreatedAt,
			UpdatedAt:    baseline.UpdatedAt,
		},
		Metrics:          MetricsData{Turns: 2},
		FirewallAnalysis: &FirewallAnalysis{AnalysisBase: AnalysisBase{BlockedRequests: 1}},
	}
	cachedAudit := &AuditData{
		CacheSource: auditCacheSourceLogs,
		Overview: OverviewData{
			RunID:        cached.RunID,
			WorkflowName: cached.WorkflowName,
			Status:       cached.Status,
			Conclusion:   cached.Conclusion,
			CreatedAt:    cached.CreatedAt,
			UpdatedAt:    cached.UpdatedAt,
		},
		Metrics: MetricsData{Turns: 5},
		CreatedItems: []CreatedItemReport{{
			Type:      "create_issue",
			Timestamp: "cache-only-created-item",
		}},
		FirewallAnalysis: &FirewallAnalysis{AnalysisBase: AnalysisBase{BlockedRequests: 7}},
		MCPFailures:      []MCPFailureReport{{ServerName: "cache-only-mcp", Status: "failed"}},
	}
	baselineRun := processedRunFromCachedData(baseline, baselineAudit, outputDir)
	processedRun := processedRunFromCachedData(cached, cachedAudit, outputDir)

	logsData, err := prepareLogsData([]ProcessedRun{baselineRun, processedRun}, renderLogsOutputOptions{
		audit:     true,
		outputDir: outputDir,
	})
	require.NoError(t, err)
	require.Len(t, logsData.Runs, 2)
	assert.Equal(t, filepath.Join(outputDir, "run-42", auditFileName), logsData.Runs[1].AuditPath)

	written, ok := loadCachedAuditData(processedRun.Run.LogsPath, processedRun.Run, auditCacheSourceLogs)
	require.True(t, ok)
	assert.Equal(t, cachedAudit.Overview, written.Overview)
	assert.Equal(t, cachedAudit.CreatedItems, written.CreatedItems)
	assert.Equal(t, cachedAudit.FirewallAnalysis, written.FirewallAnalysis)
	assert.Equal(t, cachedAudit.MCPFailures, written.MCPFailures)
	require.NotNil(t, written.Comparison)
	require.NotNil(t, written.Comparison.Delta)
	assert.Equal(t, AuditComparisonIntDelta{Before: 2, After: 5, Changed: true}, written.Comparison.Delta.Turns)
	assert.Equal(t, AuditComparisonStringDelta{Before: "read_only", After: "write_capable", Changed: true}, written.Comparison.Delta.Posture)
	assert.Equal(t, AuditComparisonIntDelta{Before: 1, After: 7, Changed: true}, written.Comparison.Delta.BlockedRequests)
	require.NotNil(t, written.Comparison.Delta.MCPFailure)
	assert.Equal(t, []string{"cache-only-mcp"}, written.Comparison.Delta.MCPFailure.After)
}

func TestBuildLogsDataPreservesCachedRunRecord(t *testing.T) {
	cached := RunData{
		RunID:                      42,
		WorkflowName:               "cached-workflow",
		Status:                     "completed",
		Conclusion:                 "success",
		Duration:                   "2m0s",
		TokenUsageSummary:          &TokenUsageSummary{TotalSteeringEvents: 2},
		TokenUsage:                 1200,
		Turns:                      4,
		ErrorCount:                 1,
		WarningCount:               2,
		GitHubAPICalls:             3,
		TemporaryIDMappings:        4,
		ChainedTargetCount:         1,
		ChainedFollowupActionCount: 2,
		DelegatedTempTargetCount:   1,
		TemporaryIDMapStatus:       temporaryIDMapStatusMissing,
		EngineID:                   "copilot",
		CreatedAt:                  time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		LogsPath:                   "/previous/run-42",
		Classification:             "normal",
		IntentionalFailure:         true,
	}

	outputDir := t.TempDir()
	processedRun := processedRunFromCachedData(cached, nil, outputDir)
	assert.Equal(t, filepath.Join(outputDir, "run-42"), processedRun.Run.LogsPath)

	data := buildLogsData([]ProcessedRun{processedRun}, outputDir, nil)

	require.Equal(t, []RunData{cached}, data.Runs)
	assert.Equal(t, 1, data.Summary.TotalRuns)
	assert.Equal(t, "2.0m", data.Summary.TotalDuration)
	assert.Equal(t, 2, data.Summary.TotalSteeringEvents)
	assert.Equal(t, 1200, data.Summary.TotalTokens)
	assert.Equal(t, 4, data.Summary.TotalTurns)
	assert.Equal(t, 3, data.Summary.TotalGitHubAPICalls)
	assert.Equal(t, 1, data.Summary.RunsWithTemporaryIDChains)
	assert.Equal(t, 1, data.Summary.RunsWithDelegatedTempTargets)
	assert.Equal(t, 1, data.Summary.RunsWithMissingTemporaryIDMap)
	assert.Equal(t, 4, data.Summary.TotalTemporaryIDMappings)
	assert.Equal(t, 1, data.Summary.TotalChainedTargets)
	assert.Equal(t, 2, data.Summary.TotalChainedFollowupActions)
	assert.Equal(t, map[string]int{"copilot": 1}, data.Summary.EngineCounts)
	assert.Equal(t, 1, data.Summary.IntentionalFailureRuns)
}
