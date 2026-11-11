//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveHealthTimeWindow(t *testing.T) {
	now := time.Date(2026, 10, 15, 13, 20, 0, 0, time.UTC)
	tests := []struct {
		name   string
		config HealthConfig
		start  string
		end    string
		err    string
	}{
		{
			name: "default seven days", config: HealthConfig{Days: 7},
			start: "2026-10-08T13:20:00Z", end: "2026-10-15T13:20:00Z",
		},
		{
			name: "relative hours", config: HealthConfig{StartDate: "-24h"},
			start: "2026-10-14T13:20:00Z", end: "2026-10-15T13:20:00Z",
		},
		{
			name: "release timestamp overrides default horizon", config: HealthConfig{Days: 7, StartDate: "2026-09-01T04:49:11Z"},
			start: "2026-09-01T04:49:11Z", end: "2026-10-15T13:20:00Z",
		},
		{
			name:   "bounded timestamps with offsets",
			config: HealthConfig{StartDate: "2026-10-14T06:49:11+02:00", EndDate: "2026-10-15T06:20:00-07:00"},
			start:  "2026-10-14T04:49:11Z", end: "2026-10-15T13:20:00Z",
		},
		{
			name: "inclusive whole day", config: HealthConfig{StartDate: "2026-10-14", EndDate: "2026-10-14"},
			start: "2026-10-14T00:00:00Z", end: "2026-10-14T23:59:59Z",
		},
		{
			name: "days anchored to explicit end", config: HealthConfig{Days: 7, EndDate: "2026-10-10T12:00:00Z"},
			start: "2026-10-03T12:00:00Z", end: "2026-10-10T12:00:00Z",
		},
		{
			name: "relative bounds share clock", config: HealthConfig{StartDate: "-2d", EndDate: "-1d"},
			start: "2026-10-13T13:20:00Z", end: "2026-10-14T13:20:00Z",
		},
		{
			name: "equal timestamp bounds", config: HealthConfig{StartDate: "2026-10-14T04:49:11Z", EndDate: "2026-10-14T04:49:11Z"},
			start: "2026-10-14T04:49:11Z", end: "2026-10-14T04:49:11Z",
		},
		{name: "invalid days", config: HealthConfig{Days: 15}, err: "invalid days value: 15"},
		{name: "invalid start", config: HealthConfig{StartDate: "yesterday"}, err: "invalid --start-date"},
		{name: "invalid relative start", config: HealthConfig{StartDate: "-bad"}, err: "invalid start-date format"},
		{name: "invalid end", config: HealthConfig{Days: 7, EndDate: "2026-13-01"}, err: "invalid --end-date"},
		{name: "reversed bounds", config: HealthConfig{StartDate: "2026-10-15", EndDate: "2026-10-14"}, err: "--start-date must be at or before --end-date"},
		{name: "future start", config: HealthConfig{StartDate: "+1d"}, err: "--start-date must be at or before --end-date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := resolveHealthTimeWindow(tt.config, now)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				require.ErrorContains(t, runHealth(context.Background(), tt.config), tt.err, "invalid windows must fail before accessing GitHub")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.start, start.Format(time.RFC3339))
			assert.Equal(t, tt.end, end.Format(time.RFC3339))
		})
	}
}

func TestHealthCommandTimeWindowFlags(t *testing.T) {
	cmd := NewHealthCommand()
	for _, name := range []string{"start-date", "end-date"} {
		flag := cmd.Flags().Lookup(name)
		require.NotNil(t, flag)
		assert.Empty(t, flag.DefValue)
	}
	cmd.SetArgs([]string{"--days", "7", "--start-date", "-24h"})
	err := cmd.Execute()
	require.ErrorContains(t, err, "were all set")
	require.ErrorContains(t, err, "days")
	require.ErrorContains(t, err, "start-date")
}

func TestHealthTimeWindowReachesSamplingAndJSON(t *testing.T) {
	stubHealthFailureCandidates(t)
	stubHealthTimeWindowAPI(t)
	originalDiscover, originalList := healthDiscoverWorkflows, healthListWorkflowRuns
	t.Cleanup(func() { healthDiscoverWorkflows, healthListWorkflowRuns = originalDiscover, originalList })
	healthDiscoverWorkflows = func(*HealthCollection) ([]GitHubWorkflow, error) {
		return []GitHubWorkflow{{ID: 1, Name: "Example", Path: ".github/workflows/example.lock.yml", State: "active"}}, nil
	}
	const start = "2026-10-14T04:49:11Z"
	const end = "2026-10-15T13:20:00Z"
	for _, detailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "summary", true: "detailed"}[detailed], func(t *testing.T) {
			calls := 0
			healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, collection *HealthCollection) ([]WorkflowRun, int, error) {
				calls++
				assert.Equal(t, start, opts.StartDate)
				assert.Equal(t, end, opts.EndDate)
				assert.Equal(t, 2, collection.MaximumDays)
				assert.Equal(t, 1, page)
				return nil, 0, nil
			}
			args := []string{"--repo", "org/repo", "--json", "--start-date", start, "--end-date", end}
			if detailed {
				args = append(args, "example")
			}
			output := captureAuditGroupStdout(t, func() {
				cmd := NewHealthCommand()
				cmd.SetArgs(args)
				require.NoError(t, cmd.Execute())
			})
			require.Equal(t, 1, calls)
			var report struct {
				Period     string            `json:"period"`
				Collection *HealthCollection `json:"collection"`
			}
			require.NoError(t, json.Unmarshal([]byte(output), &report))
			require.NotNil(t, report.Collection)
			assert.Equal(t, start, report.Collection.StartDate)
			assert.Equal(t, end, report.Collection.EndDate)
			assert.Equal(t, GetVersion(), report.Collection.ProducerVersion)
			assert.Equal(t, 1, report.Collection.ReportSchemaVersion)
			assert.False(t, report.Collection.ObservedAt.IsZero())
			assert.Contains(t, report.Collection.Capabilities, "typed_job_steps")
			assert.Contains(t, report.Collection.Capabilities, "cross_version_raw_jobs")
			if !detailed {
				assert.Contains(t, report.Period, start)
				assert.Contains(t, report.Period, end)
			}
		})
	}
}

func TestHealthRunListingPreservesTimeWindowAcrossPages(t *testing.T) {
	originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
	t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
	}
	opts := ListWorkflowRunsOptions{WorkflowName: "1", Limit: 10, RepoOverride: "org/repo",
		StartDate: "2026-10-14T04:49:11Z", EndDate: "2026-10-15T13:20:00Z"}
	collection := &HealthCollection{Host: "github.com"}
	for _, page := range []int{1, 2} {
		healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			endpoint, err := url.Parse(args[3])
			require.NoError(t, err)
			assert.Equal(t, opts.StartDate+".."+opts.EndDate, endpoint.Query().Get("created"))
			assert.Equal(t, map[int]string{1: "1", 2: "2"}[page], endpoint.Query().Get("page"))
			return []byte(`{"workflow_runs":[]}`), nil
		}
		_, _, err := listHealthWorkflowRuns(opts, page, collection)
		require.NoError(t, err)
	}
}

func TestHealthDaysOnlyPeriodThroughCommand(t *testing.T) {
	stubHealthFailureCandidates(t)
	stubHealthTimeWindowAPI(t)
	originalDiscover, originalList := healthDiscoverWorkflows, healthListWorkflowRuns
	t.Cleanup(func() { healthDiscoverWorkflows, healthListWorkflowRuns = originalDiscover, originalList })
	healthDiscoverWorkflows = func(*HealthCollection) ([]GitHubWorkflow, error) {
		return []GitHubWorkflow{{ID: 1, Name: "Example", Path: ".github/workflows/example.lock.yml", State: "active"}}, nil
	}
	healthListWorkflowRuns = func(ListWorkflowRunsOptions, int, *HealthCollection) ([]WorkflowRun, int, error) {
		return nil, 0, nil
	}

	output := captureAuditGroupStdout(t, func() {
		cmd := NewHealthCommand()
		cmd.SetArgs([]string{"--repo", "org/repo", "--json", "--days", "7"})
		require.NoError(t, cmd.Execute())
	})
	var report struct {
		Period string `json:"period"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Equal(t, "Current status (up to 7 days)", report.Period)
}

func stubHealthTimeWindowAPI(t *testing.T) {
	t.Helper()
	originalRate, originalGH := fetchRateLimitForHostFunc, healthRunGH
	t.Cleanup(func() { fetchRateLimitForHostFunc, healthRunGH = originalRate, originalGH })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{Limit: 5000, Remaining: 4000}, nil
	}
	healthRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[3] {
		case "repos/org/repo":
			return []byte(`{"full_name":"org/repo","default_branch":"main"}`), nil
		case "repos/org/repo/contents/.github/workflows?ref=main":
			return []byte(`[]`), nil
		default:
			t.Fatalf("unexpected health API request: %v", args)
			return nil, nil
		}
	}
}

func TestHealthTimeWindowTextOutput(t *testing.T) {
	config := HealthConfig{
		StartDate: "2026-10-14T04:49:11Z", EndDate: "2026-10-15T13:20:00Z",
		explicitTimeWindow: true,
	}
	period := config.healthPeriod()
	assert.True(t, strings.HasPrefix(period, "Current status"))
	assert.Contains(t, period, config.StartDate)
	assert.Contains(t, period, config.EndDate)
	assert.Equal(t, "Current status (up to 7 days)", (HealthConfig{Days: 7}).healthPeriod())
}
