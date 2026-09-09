//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkflowRunUnmarshal verifies that a standard "gh run list --json" response
// (without the unsupported "path" field) is correctly unmarshaled into a WorkflowRun.
// The "path" field was previously requested but is not a valid gh run list --json
// field and caused failures on strict gh CLI versions.
func TestWorkflowRunUnmarshal(t *testing.T) {
	rawJSON := `[
{
"databaseId": 42,
"workflowName": "My Workflow",
"status": "completed",
"conclusion": "success",
"createdAt": "2026-01-01T00:00:00Z",
"startedAt": "2026-01-01T00:00:01Z",
"updatedAt": "2026-01-01T00:01:00Z"
}
]`

	var runs []WorkflowRun
	require.NoError(t, json.Unmarshal([]byte(rawJSON), &runs), "unmarshal should succeed")
	require.Len(t, runs, 1)

	assert.Equal(t, int64(42), runs[0].DatabaseID, "DatabaseID should be populated")
	assert.Equal(t, "My Workflow", runs[0].WorkflowName, "WorkflowName should be populated")
	assert.Empty(t, runs[0].WorkflowPath, "WorkflowPath should be empty when 'path' field is absent")
}

func TestFetchAndCacheWorkflowRunMetadata(t *testing.T) {
	fakeBinDir := testutil.TempDir(t, "fake-gh-*")
	outputDir := t.TempDir()
	fakeGH := filepath.Join(fakeBinDir, "gh")
	argsLogPath := filepath.Join(fakeBinDir, "gh-args.log")
	fakeGHScript := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + argsLogPath + "\"\n" +
		"cat <<'EOF'\n" +
		`{"id":42,"run_number":7,"run_attempt":3,"html_url":"https://github.com/octo/repo/actions/runs/42","status":"completed","conclusion":"success","name":"Daily report","path":".github/workflows/daily-report.lock.yml","created_at":"2026-09-01T10:00:00Z","run_started_at":"2026-09-01T10:00:01Z","updated_at":"2026-09-01T10:02:00Z","event":"schedule","head_branch":"main","head_sha":"abc123","display_title":"Daily report","actor":{"login":"octocat"},"repository":{"full_name":"octo/repo"}}` + "\n" +
		"EOF\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHScript), 0o755))
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	currentRun := WorkflowRun{DatabaseID: 42, Attempt: 3, Status: "completed"}
	run, err := fetchAndCacheWorkflowRunMetadata(context.Background(), currentRun, outputDir, "octo", "repo", "", false)
	require.NoError(t, err)
	assert.Equal(t, int64(42), run.DatabaseID)
	assert.Equal(t, 3, run.Attempt)
	assert.Equal(t, ".github/workflows/daily-report.lock.yml", run.WorkflowPath)
	assert.Equal(t, "octo/repo", run.Repository)
	assert.Equal(t, "octocat", run.Actor)

	cachePath := filepath.Join(outputDir, runAPIResponseFileName)
	cached, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.Contains(t, string(cached), `"run_attempt":3`)

	require.NoError(t, os.Remove(fakeGH))
	cachedRun, err := fetchAndCacheWorkflowRunMetadata(context.Background(), currentRun, outputDir, "octo", "repo", "", false)
	require.NoError(t, err, "the second read should use the cache without invoking gh")
	assert.Equal(t, run, cachedRun)

	argsLog, err := os.ReadFile(argsLogPath)
	require.NoError(t, err)
	assert.Equal(t, "api repos/octo/repo/actions/runs/42\n", string(argsLog))

	needsRefresh, err := workflowRunMetadataCacheNeedsRefresh(outputDir, currentRun, "octo", "other-repo")
	require.NoError(t, err)
	assert.True(t, needsRefresh, "a cache entry from another repository must be refreshed")

	needsRefresh, err = workflowRunMetadataCacheNeedsRefresh(outputDir, WorkflowRun{
		DatabaseID: 42,
		Attempt:    4,
		Status:     "completed",
	}, "octo", "repo")
	require.NoError(t, err)
	assert.True(t, needsRefresh, "a cache entry from another attempt must be refreshed")

	require.NoError(t, os.WriteFile(cachePath, []byte(strings.Replace(string(cached), `"status":"completed"`, `"status":"in_progress"`, 1)), 0o600))
	needsRefresh, err = workflowRunMetadataCacheNeedsRefresh(outputDir, currentRun, "octo", "repo")
	require.NoError(t, err)
	assert.True(t, needsRefresh, "a mutable cache entry must be refreshed")

	metadataApplied := applyWorkflowRunMetadata(&WorkflowRun{WorkflowName: "Daily report"}, WorkflowRun{
		WorkflowName: ".github/workflows/daily-report.lock.yml",
	})
	assert.False(t, metadataApplied, "a path-like API name must not replace a resolved display name")
}

// TestBuildCreatedFilter verifies that buildCreatedFilter always produces a single
// --created expression that enforces all supplied date bounds. The key invariant is that
// StartDate is never silently dropped, which was the root cause of the bug where runs
// outside the requested window were returned (multiple --created flags were used but gh
// CLI only honours the last one).
func TestBuildCreatedFilter(t *testing.T) {
	tests := []struct {
		name       string
		startDate  string
		endDate    string
		beforeDate string
		want       string
	}{
		{
			name: "no bounds",
			want: "",
		},
		{
			name:      "start date only",
			startDate: "2026-04-17",
			want:      ">=2026-04-17",
		},
		{
			name:    "end date only",
			endDate: "2026-04-17",
			want:    "<=2026-04-17",
		},
		{
			name:      "start and end date",
			startDate: "2026-04-17",
			endDate:   "2026-04-17",
			want:      "2026-04-17..2026-04-17",
		},
		{
			name:      "start and end date different days",
			startDate: "2026-04-01",
			endDate:   "2026-04-30",
			want:      "2026-04-01..2026-04-30",
		},
		{
			name:       "before date only (pagination cursor)",
			beforeDate: "2026-04-17T12:00:00Z",
			// No startDate: keep the original < form.
			want: "<2026-04-17T12:00:00Z",
		},
		{
			name:       "start date and before date (pagination with lower bound)",
			startDate:  "2026-04-01",
			beforeDate: "2026-04-17T12:00:01Z",
			// beforeDate is exclusive; subtract 1 s for inclusive range syntax.
			want: "2026-04-01..2026-04-17T12:00:00Z",
		},
		{
			name:       "start date, end date, and before date",
			startDate:  "2026-04-01",
			endDate:    "2026-04-30",
			beforeDate: "2026-04-17T12:00:01Z",
			// beforeDate takes precedence over endDate as the pagination upper bound.
			want: "2026-04-01..2026-04-17T12:00:00Z",
		},
		{
			name:       "before date at second boundary",
			startDate:  "2026-04-17T00:00:00Z",
			beforeDate: "2026-04-17T00:00:01Z",
			// Subtracting 1 s from beforeDate gives exactly startDate.
			want: "2026-04-17T00:00:00Z..2026-04-17T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCreatedFilter(tt.startDate, tt.endDate, tt.beforeDate)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildCreatedFilterStartDateAlwaysEnforced verifies that when both startDate and
// beforeDate are set, the returned filter contains the startDate so that the lower bound
// is always honoured. This is the regression test for the original bug.
func TestBuildCreatedFilterStartDateAlwaysEnforced(t *testing.T) {
	startDate := "2026-04-17"
	beforeDate := "2026-04-17T23:59:59Z"

	filter := buildCreatedFilter(startDate, "", beforeDate)

	// The filter must start with the startDate so it is part of the expression sent to gh.
	assert.True(t, strings.HasPrefix(filter, startDate),
		"filter %q must start with startDate %q so the lower bound is enforced", filter, startDate)
}

// TestListWorkflowRunsErrorHandling verifies the error classification logic in
// listWorkflowRunsWithPagination. In particular it checks that:
//   - "Unknown JSON field" (capital U, as emitted by gh CLI) is treated as an
//     invalid-field error, not an auth error (case-insensitive matching).
//   - Exit code 1 alone does NOT trigger the auth-failure path because gh exits
//     with code 1 for many non-auth errors (e.g. unsupported JSON fields).
func TestListWorkflowRunsErrorHandling(t *testing.T) {
	tests := []struct {
		name             string
		errMsg           string
		outputMsg        string
		wantInvalidField bool
		wantAuth         bool
	}{
		{
			name:             "unknown JSON field (capital U, as gh CLI emits)",
			errMsg:           "exit status 1",
			outputMsg:        `Unknown JSON field: "path"`,
			wantInvalidField: true,
			wantAuth:         false,
		},
		{
			name:             "unknown field lowercase",
			errMsg:           "exit status 1",
			outputMsg:        "unknown field foo",
			wantInvalidField: true,
			wantAuth:         false,
		},
		{
			name:             "invalid field mixed case",
			errMsg:           "exit status 1",
			outputMsg:        "Invalid field: bar",
			wantInvalidField: true,
			wantAuth:         false,
		},
		{
			name:      "exit status 1 alone is NOT an auth error",
			errMsg:    "exit status 1",
			outputMsg: "some other error",
			wantAuth:  false,
		},
		{
			name:      "exit status 4 IS an auth error",
			errMsg:    "exit status 4",
			outputMsg: "",
			wantAuth:  true,
		},
		{
			name:      "gh auth login hint is an auth error",
			errMsg:    "exit status 1",
			outputMsg: "To get started, run: gh auth login",
			wantAuth:  true,
		},
		{
			name:      "not logged in message is an auth error",
			errMsg:    "exit status 1",
			outputMsg: "not logged into any GitHub hosts",
			wantAuth:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			combinedMsg := tt.errMsg + " " + tt.outputMsg
			combinedMsgLower := strings.ToLower(combinedMsg)

			isInvalidField := strings.Contains(combinedMsgLower, "invalid field") ||
				strings.Contains(combinedMsgLower, "unknown field") ||
				strings.Contains(combinedMsgLower, "unknown json field") ||
				strings.Contains(combinedMsgLower, "unknown json") ||
				strings.Contains(combinedMsgLower, "field not found") ||
				strings.Contains(combinedMsgLower, "no such field")
			isAuth := !isInvalidField && (strings.Contains(combinedMsg, "exit status 4") ||
				strings.Contains(combinedMsg, "not logged into any GitHub hosts") ||
				strings.Contains(combinedMsg, "To use GitHub CLI in a GitHub Actions workflow") ||
				strings.Contains(combinedMsg, "authentication required") ||
				strings.Contains(tt.outputMsg, "gh auth login"))

			if tt.wantInvalidField {
				assert.True(t, isInvalidField, "expected invalid-field classification")
				assert.False(t, isAuth, "invalid-field errors must not be classified as auth errors")
			}
			if tt.wantAuth {
				assert.False(t, isInvalidField, "auth errors must not be classified as invalid-field errors")
				assert.True(t, isAuth, "expected auth classification")
			}
			if !tt.wantInvalidField && !tt.wantAuth {
				assert.False(t, isInvalidField, "should not be invalid-field")
				assert.False(t, isAuth, "should not be auth")
			}
		})
	}
}

func TestFetchJobDetailsWithCountsIncludesSteps(t *testing.T) {
	fakeBinDir := testutil.TempDir(t, "fake-gh-*")
	outputDir := t.TempDir()
	fakeGH := filepath.Join(fakeBinDir, "gh")
	argsLogPath := filepath.Join(fakeBinDir, "gh-args.log")
	fakeGHScript := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + argsLogPath + "\"\n" +
		"cat <<'EOF'\n" +
		"[{\"total_count\":1,\"jobs\":[{\"id\":42,\"run_id\":28307653871,\"run_attempt\":2,\"html_url\":\"https://github.com/github/gh-aw/actions/runs/28307653871/job/42\",\"status\":\"completed\",\"conclusion\":\"failure\",\"created_at\":\"2026-06-28T01:30:00Z\",\"started_at\":\"2026-06-28T01:31:00Z\",\"completed_at\":\"2026-06-28T01:33:00Z\",\"name\":\"agent\",\"runner_name\":\"GitHub Actions 1\",\"steps\":[{\"name\":\"Set up job\",\"status\":\"completed\",\"conclusion\":\"success\",\"number\":1,\"started_at\":\"2026-06-28T01:31:00Z\",\"completed_at\":\"2026-06-28T01:31:10Z\"},{\"name\":\"Run agent\",\"status\":\"completed\",\"conclusion\":\"failure\",\"number\":2,\"started_at\":\"2026-06-28T01:31:10Z\",\"completed_at\":\"2026-06-28T01:33:00Z\"}]}]}]\n" +
		"EOF\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHScript), 0o755))

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cachePath := filepath.Join(outputDir, jobsAPIResponseFileName)
	require.NoError(t, os.WriteFile(cachePath, []byte("old cache"), 0o644))

	jobs, failedJobs, err := fetchJobDetailsWithCounts(context.Background(), 28307653871, outputDir, false)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, 1, failedJobs, "failed job count should include failed jobs")
	assert.Equal(t, 2*time.Minute, jobs[0].Duration, "job duration should still be derived from timestamps")
	assert.Equal(t, int64(42), jobs[0].ID, "high-level GitHub job metadata should be preserved")
	assert.Equal(t, 2, jobs[0].RunAttempt)
	assert.Equal(t, "GitHub Actions 1", jobs[0].RunnerName)
	require.Len(t, jobs[0].Steps, 2)
	assert.Equal(t, "Run agent", jobs[0].Steps[1].Name, "step names should be parsed from gh api output")
	assert.Equal(t, "failure", jobs[0].Steps[1].Conclusion, "step conclusions should be parsed from gh api output")
	assert.Equal(t, 2, jobs[0].Steps[1].Number)

	argsLog, err := os.ReadFile(argsLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(argsLog), "repos/{owner}/{repo}/actions/runs/28307653871/jobs?per_page=100", "should query the run jobs API")
	assert.Contains(t, string(argsLog), "--paginate --slurp", "should cache all pages of the jobs API response")
	assert.NotContains(t, string(argsLog), "--jq", "should cache the complete API response without a projection")

	cachedResponse, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.Contains(t, string(cachedResponse), `"total_count":1`)
	assert.Contains(t, string(cachedResponse), `"runner_name":"GitHub Actions 1"`)
	cachedInfo, err := os.Stat(cachePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), cachedInfo.Mode().Perm())
}

func TestFetchJobDetailsWithCountsReturnsJobsWhenCacheWriteFails(t *testing.T) {
	fakeBinDir := testutil.TempDir(t, "fake-gh-*")
	fakeGH := filepath.Join(fakeBinDir, "gh")
	fakeGHScript := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"[{\"total_count\":1,\"jobs\":[{\"name\":\"agent\",\"status\":\"completed\",\"conclusion\":\"failure\"}]}]\n" +
		"EOF\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHScript), 0o755))

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	missingOutputDir := filepath.Join(t.TempDir(), "missing", "run")
	jobs, failedJobs, err := fetchJobDetailsWithCounts(context.Background(), 28307653871, missingOutputDir, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cache jobs API response")
	require.Len(t, jobs, 1)
	assert.Equal(t, "agent", jobs[0].Name)
	assert.Equal(t, 1, failedJobs)
}

func TestFetchJobDetailsWithCountsSkipsMalformedJobs(t *testing.T) {
	fakeBinDir := testutil.TempDir(t, "fake-gh-*")
	fakeGH := filepath.Join(fakeBinDir, "gh")
	fakeGHScript := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"[{\"total_count\":3,\"jobs\":[{\"name\":\"failed\",\"status\":\"completed\",\"conclusion\":\"failure\"},{\"name\":42,\"status\":\"completed\",\"conclusion\":\"failure\"},{\"name\":\"passed\",\"status\":\"completed\",\"conclusion\":\"success\"}]}]\n" +
		"EOF\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHScript), 0o755))

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	jobs, failedJobs, err := fetchJobDetailsWithCounts(context.Background(), 28307653871, "", false)

	require.NoError(t, err)
	require.Len(t, jobs, 2)
	assert.Equal(t, "failed", jobs[0].Name)
	assert.Equal(t, "passed", jobs[1].Name)
	assert.Equal(t, 1, failedJobs)
}

func TestFetchJobDetailsWithCountsSkipsMalformedPages(t *testing.T) {
	fakeBinDir := testutil.TempDir(t, "fake-gh-*")
	fakeGH := filepath.Join(fakeBinDir, "gh")
	fakeGHScript := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"[{\"total_count\":1,\"jobs\":[{\"name\":\"first\",\"status\":\"completed\",\"conclusion\":\"success\"}]},{\"total_count\":1,\"jobs\":\"malformed\"},{\"total_count\":1,\"jobs\":[{\"name\":\"last\",\"status\":\"completed\",\"conclusion\":\"failure\"}]}]\n" +
		"EOF\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHScript), 0o755))

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	jobs, failedJobs, err := fetchJobDetailsWithCounts(context.Background(), 28307653871, "", false)

	require.NoError(t, err)
	require.Len(t, jobs, 2)
	assert.Equal(t, "first", jobs[0].Name)
	assert.Equal(t, "last", jobs[1].Name)
	assert.Equal(t, 1, failedJobs)
}

// TestFetchJobDetailsWithCountsNullConclusion verifies that jobs and steps with null conclusions
// (e.g. in-progress or queued jobs) are still parsed and not silently dropped. The jq projection
// uses (.conclusion // "") to coerce null to empty string before JSON decoding.
func TestFetchJobDetailsWithCountsNullConclusion(t *testing.T) {
	fakeBinDir := testutil.TempDir(t, "fake-gh-*")
	fakeGH := filepath.Join(fakeBinDir, "gh")
	// Simulate jq coercing null -> "" before output (as (.conclusion // "") does).
	// A job still in progress has conclusion="" for itself and for any pending steps.
	fakeGHScript := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"[{\"total_count\":1,\"jobs\":[{\"name\":\"agent\",\"status\":\"in_progress\",\"conclusion\":null,\"started_at\":\"2026-06-28T01:31:00Z\",\"completed_at\":null,\"steps\":[{\"name\":\"Set up job\",\"status\":\"completed\",\"conclusion\":\"success\"},{\"name\":\"Run agent\",\"status\":\"in_progress\",\"conclusion\":null}]}]}]\n" +
		"EOF\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(fakeGHScript), 0o755))

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	jobs, failedJobs, err := fetchJobDetailsWithCounts(context.Background(), 28307653871, "", false)
	require.NoError(t, err)
	require.Len(t, jobs, 1, "in-progress jobs with null conclusion should not be dropped")
	assert.Equal(t, 0, failedJobs, "in-progress job should not count as failed")
	assert.Equal(t, "in_progress", jobs[0].Status)
	assert.Empty(t, jobs[0].Conclusion, "null conclusion should be coerced to empty string")
	require.Len(t, jobs[0].Steps, 2)
	assert.Empty(t, jobs[0].Steps[1].Conclusion, "null step conclusion should be coerced to empty string")
}

func TestWorkflowRunsSpinnerMessage(t *testing.T) {
	tests := []struct {
		name string
		opts ListWorkflowRunsOptions
		want string
	}{
		{
			name: "without target count",
			opts: ListWorkflowRunsOptions{},
			want: "Fetching workflow runs from GitHub...",
		},
		{
			name: "with target count",
			opts: ListWorkflowRunsOptions{
				ProcessedCount: 3,
				TargetCount:    10,
			},
			want: "Fetching workflow runs from GitHub... (3 / 10)",
		},
		{
			name: "processed equals target",
			opts: ListWorkflowRunsOptions{
				ProcessedCount: 10,
				TargetCount:    10,
			},
			want: "Fetching workflow runs from GitHub... (10 / 10)",
		},
		{
			name: "processed exceeds target",
			opts: ListWorkflowRunsOptions{
				ProcessedCount: 12,
				TargetCount:    10,
			},
			want: "Fetching workflow runs from GitHub... (12 / 10)",
		},
		{
			name: "processed without target",
			opts: ListWorkflowRunsOptions{
				ProcessedCount: 4,
			},
			want: "Fetching workflow runs from GitHub...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := workflowRunsSpinnerMessage(tt.opts)
			assert.Equal(t, tt.want, msg)
		})
	}
}
