//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRateLimitResponseUnmarshal verifies that the rateLimitResponse struct correctly
// unmarshals the JSON returned by `gh api rate_limit`.
func TestRateLimitResponseUnmarshal(t *testing.T) {
	now := time.Now().Add(time.Second * 30).Unix()
	raw := []byte(`{
		"resources": {
			"core": {
				"limit": 5000,
				"remaining": 42,
				"reset": ` + jsonInt(now) + `,
				"used": 4958
			}
		},
		"rate": {
			"limit": 5000,
			"remaining": 42,
			"reset": ` + jsonInt(now) + `,
			"used": 4958
		}
	}`)

	var resp rateLimitResponse
	require.NoError(t, json.Unmarshal(raw, &resp), "unmarshal should succeed")

	assert.Equal(t, 5000, resp.Resources.Core.Limit, "Limit should match")
	assert.Equal(t, 42, resp.Resources.Core.Remaining, "Remaining should match")
	assert.Equal(t, now, resp.Resources.Core.Reset, "Reset should match")
	assert.Equal(t, 4958, resp.Resources.Core.Used, "Used should match")
}

// TestRateLimitThresholdConstants verifies that the rate-limit constants are set to
// sensible values so a future edit that accidentally zeroes them will be caught.
func TestRateLimitThresholdConstants(t *testing.T) {
	assert.Positive(t, RateLimitThreshold, "RateLimitThreshold must be positive")
	assert.Positive(t, RateLimitWarningThresholdPercent, "RateLimitWarningThresholdPercent must be positive")
	assert.Less(t, RateLimitWarningThresholdPercent, 100, "RateLimitWarningThresholdPercent must be below 100")
	assert.Positive(t, int64(APICallCooldown), "APICallCooldown must be positive")
	assert.Positive(t, int64(rateLimitResetBuffer), "rateLimitResetBuffer must be positive")
}

// TestRateLimitResourceIsBelowThreshold checks the boundary condition used by
// checkAndWaitForRateLimit: remaining <= RateLimitThreshold means we should wait.
func TestRateLimitResourceIsBelowThreshold(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		wantWait  bool
	}{
		{name: "zero remaining", remaining: 0, wantWait: true},
		{name: "exactly at threshold", remaining: RateLimitThreshold, wantWait: true},
		{name: "one above threshold", remaining: RateLimitThreshold + 1, wantWait: false},
		{name: "plenty remaining", remaining: 4000, wantWait: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := rateLimitResource{
				Limit:     5000,
				Remaining: tt.remaining,
				Reset:     time.Now().Add(60 * time.Second).Unix(),
				Used:      5000 - tt.remaining,
			}
			shouldWait := rl.Remaining <= RateLimitThreshold
			assert.Equal(t, tt.wantWait, shouldWait,
				"remaining=%d vs threshold=%d: wait mismatch", tt.remaining, RateLimitThreshold)
		})
	}
}

func TestGitHubAPIRateLimitReportJSON(t *testing.T) {
	report := &GitHubAPIRateLimitReport{
		Host:  "github.com",
		Start: &rateLimitResource{Limit: 5000, Remaining: 100, Used: 4900, Reset: 123},
		End:   &rateLimitResource{Limit: 5000, Remaining: 80, Used: 4920, Reset: 123},
	}
	data, err := json.Marshal(LogsData{GitHubAPIRateLimit: report})
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))
	rateLimit, ok := output["github_api_rate_limit"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "github.com", rateLimit["host"])
	assert.InDelta(t, 100, rateLimit["start"].(map[string]any)["remaining"], 0)
	assert.InDelta(t, 80, rateLimit["end"].(map[string]any)["remaining"], 0)
}

func TestGitHubAPIRateLimitReportUsesRequestedHost(t *testing.T) {
	oldFetchRateLimitForHostFunc := fetchRateLimitForHostFunc
	var hosts []string
	fetchRateLimitForHostFunc = func(_ context.Context, host string) (rateLimitResource, error) {
		hosts = append(hosts, host)
		return rateLimitResource{Limit: 5000, Remaining: 4000, Used: 1000}, nil
	}
	t.Cleanup(func() { fetchRateLimitForHostFunc = oldFetchRateLimitForHostFunc })

	report := startGitHubAPIRateLimitReport(context.Background(), "github.example.com")
	finishGitHubAPIRateLimitReport(context.Background(), report, true)

	assert.Equal(t, "github.example.com", report.Host)
	assert.Equal(t, []string{"github.example.com", "github.example.com"}, hosts)
}

func TestLogsTargetRateLimitHostsDeduplicatesHosts(t *testing.T) {
	hosts := logsTargetRateLimitHosts([]logsWorkflowTarget{
		{repoOverride: "owner/repo"},
		{repoOverride: "github.example.com/owner/repo"},
		{repoOverride: "github.example.com/other/repo"},
		{repoOverride: "another.example.com/owner/repo"},
	})

	assert.Equal(t, []string{"github.com", "github.example.com", "another.example.com"}, hosts)
}

func TestIsGitHubAPIRateLimitLow(t *testing.T) {
	tests := []struct {
		name  string
		state rateLimitResource
		want  bool
	}{
		{name: "at threshold", state: rateLimitResource{Limit: 5000, Remaining: 1000}, want: true},
		{name: "below threshold", state: rateLimitResource{Limit: 5000, Remaining: 999}, want: true},
		{name: "above threshold", state: rateLimitResource{Limit: 5000, Remaining: 1001}, want: false},
		{name: "missing limit", state: rateLimitResource{Remaining: 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isGitHubAPIRateLimitLow(tt.state))
		})
	}
}

func TestFinishGitHubAPIRateLimitReportWarnsNearLimit(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{
			Limit:     5000,
			Remaining: 1000,
			Used:      4000,
			Reset:     time.Now().Add(time.Hour).Unix(),
		}, nil
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	var output bytes.Buffer
	finishGitHubAPIRateLimitReportToWriter(context.Background(), &GitHubAPIRateLimitReport{}, false, &output)
	assert.Contains(t, output.String(), "GitHub API rate limit for github.com is running low")
	assert.Contains(t, output.String(), "20.00%")
}

func TestFinishGitHubAPIRateLimitReportSuppressesWarningForJSON(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{Limit: 5000, Remaining: 0, Used: 5000}, nil
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	report := &GitHubAPIRateLimitReport{}
	var output bytes.Buffer
	finishGitHubAPIRateLimitReportToWriter(context.Background(), report, true, &output)
	assert.Empty(t, strings.TrimSpace(output.String()))
	require.NotNil(t, report.End)
	assert.Zero(t, report.End.Remaining)
}

func TestGitHubAPIRateLimitReportOmitsUnavailableSnapshots(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{}, stderrors.New("unavailable")
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	report := startGitHubAPIRateLimitReport(context.Background(), "")
	finishGitHubAPIRateLimitReport(context.Background(), report, true)
	assert.Nil(t, populatedGitHubAPIRateLimitReport(report))
}

func TestResolveMaxGitHubAPIRateLimit(t *testing.T) {
	tests := []struct {
		name          string
		configuredMax int
		apiLimit      int
		want          int
		wantErr       bool
	}{
		{name: "default preserves built-in reserve", configuredMax: 0, apiLimit: 5000, want: 4990},
		{name: "absolute maximum", configuredMax: 12000, apiLimit: 15000, want: 12000},
		{name: "relative reserve", configuredMax: -2000, apiLimit: 15000, want: 13000},
		{name: "absolute maximum exceeds limit", configuredMax: 15001, apiLimit: 15000, wantErr: true},
		{name: "relative reserve consumes whole limit", configuredMax: -15000, apiLimit: 15000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMaxGitHubAPIRateLimit(tt.configuredMax, tt.apiLimit)
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidMaxGitHubAPIRateLimit)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// jsonInt is a helper that converts an int64 to its JSON number representation.
func jsonInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

// TestSleepWithContextCancellation verifies that sleepWithContext returns ctx.Err()
// immediately when the context is cancelled before the timer fires.
func TestSleepWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	err := sleepWithContext(ctx, 10*time.Second)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, time.Second, "sleepWithContext should return quickly when context is already cancelled")
}

func TestSharedRateLimitGateRespectsCancellation(t *testing.T) {
	logsRateLimitGate <- struct{}{}
	t.Cleanup(func() { <-logsRateLimitGate })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := checkAndWaitForRateLimitShared(ctx, false, 0, 1)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), time.Second, "waiting for the shared rate-limit gate should be cancellable")
}

// TestSleepWithContextDeadlineExceeded verifies that sleepWithContext respects a
// deadline that expires before the sleep duration.
func TestSleepWithContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sleepWithContext(ctx, 10*time.Second)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, time.Second, "sleepWithContext should return as soon as the deadline expires")
}

// TestSleepWithContextTimerFires verifies that sleepWithContext returns nil when the
// timer fires before context cancellation.
func TestSleepWithContextTimerFires(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	err := sleepWithContext(ctx, 5*time.Millisecond)
	elapsed := time.Since(start)
	require.NoError(t, err, "sleepWithContext should return nil when timer fires normally")
	assert.Less(t, elapsed, time.Second, "timer should have fired and returned promptly")
}

func TestSleepWithContextNilContext(t *testing.T) {
	var nilCtx context.Context
	start := time.Now()
	err := sleepWithContext(nilCtx, 2*time.Millisecond)
	elapsed := time.Since(start)
	require.NoError(t, err, "nil context should fall back to background context")
	assert.Less(t, elapsed, time.Second, "nil context should not block longer than timer duration")
}

func TestSleepWithContextAlreadyCanceled(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := sleepWithContext(canceledCtx, 2*time.Millisecond)
	elapsed := time.Since(start)
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, time.Second, "already-canceled context should return promptly")
}

func TestCheckAndWaitForRateLimitContextCancelled(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{
			Limit:     5000,
			Remaining: 0,
			Reset:     time.Now().Add(10 * time.Minute).Unix(),
			Used:      5000,
		}, nil
	}

	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := checkAndWaitForRateLimit(ctx, false, 0, 1)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 100*time.Millisecond, "cancelled context should interrupt rate-limit wait promptly")
}

func TestConfiguredRateLimitWaitRespectsDeadline(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{
			Limit:     15000,
			Remaining: 3000,
			Reset:     time.Now().Add(10 * time.Minute).Unix(),
			Used:      12000,
		}, nil
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := checkAndWaitForRateLimit(ctx, false, 12000, 1)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestConfiguredRateLimitRunsWithoutCooldownBelowCeiling(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{
			Limit:     15000,
			Remaining: 10000,
			Reset:     time.Now().Add(10 * time.Minute).Unix(),
			Used:      5000,
		}, nil
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	start := time.Now()
	err := checkAndWaitForRateLimit(context.Background(), false, -2000, 1)

	require.NoError(t, err)
	assert.Less(t, time.Since(start), 100*time.Millisecond)
}

func TestCheckAndWaitForRateLimitFetchErrorAndContextDone(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	expectedFetchErr := stderrors.New("fetch failure")
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{}, expectedFetchErr
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := checkAndWaitForRateLimit(ctx, false, 0, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, expectedFetchErr)
	require.ErrorIs(t, err, context.Canceled)
}
