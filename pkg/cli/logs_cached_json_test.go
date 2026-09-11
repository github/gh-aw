//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
		"agent_runtime": "gvisor"
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
	assert.Equal(t, "gvisor", record.Run.AgentRuntime)
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
	var record cachedLogsJSONLRecord
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &record))
	require.NotNil(t, record.Run)
	require.NotNil(t, record.Run.Audit)
	assert.Equal(t, int64(42), record.Run.Audit.Overview.RunID)
	require.NotNil(t, record.Run.AwInfo)
	assert.Equal(t, "copilot", record.Run.AwInfo.EngineID)
	require.Len(t, record.Run.SafeOutputs, 1)
	assert.Equal(t, "create_issue", record.Run.SafeOutputs[0].Type)
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
	_, ok = runs.lookup(run, runFilterOpts{runtime: "gvisor"})
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
	assert.True(t, cachedJSONLCanSatisfy(usageFilter, false, false, false, false))
	assert.False(t, cachedJSONLCanSatisfy(nil, false, false, false, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, true, false, false, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, false, true, false, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, false, false, true, false))
	assert.False(t, cachedJSONLCanSatisfy(usageFilter, false, false, false, true))
}

func TestDownloadRunArtifactsConcurrentReusesCachedJSONRecord(t *testing.T) {
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

	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{{DatabaseID: 42, Repository: "github/gh-aw", Status: "completed", Conclusion: "success", Attempt: 1, UpdatedAt: cached.UpdatedAt}}, runArtifactsConcurrentOptions{
		outputDir:    t.TempDir(),
		maxRuns:      1,
		cachedRuns:   cachedLogsRuns{42: {RunData: cached}},
		storageLimit: newLogsStorageLimit(t.TempDir(), 0, false),
	})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].CachedRun)
	assert.True(t, results[0].Cached)
	assert.Equal(t, cached, *results[0].CachedRun)
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

	processedRun := processedRunFromCachedData(cached)
	assert.Empty(t, processedRun.Run.LogsPath)

	data := buildLogsData([]ProcessedRun{processedRun}, t.TempDir(), nil)

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
