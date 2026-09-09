// This file provides command-line interface functionality for gh-aw.
// This file (logs_rate_limit.go) contains helpers for querying the GitHub API
// rate limit and pausing execution when the remaining request budget is low.
//
// Key responsibilities:
//   - Fetching the current GitHub API rate limit via the gh CLI
//   - Sleeping until the rate-limit reset window when remaining requests are scarce
//   - Providing a drop-in replacement for the static APICallCooldown sleep used
//     between batch-fetch iterations in the logs orchestrator

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsRateLimitLog = logger.New("cli:logs_rate_limit")
var fetchRateLimitFunc = fetchRateLimit
var fetchRateLimitForHostFunc = func(ctx context.Context, host string) (rateLimitResource, error) {
	if normalizedGitHubAPIHost(host) == "github.com" {
		return fetchRateLimitFunc(ctx)
	}
	return fetchRateLimitForHost(ctx, host)
}
var logsRateLimitGate = make(chan struct{}, 1)
var errInvalidMaxGitHubAPIRateLimit = errors.New("invalid maximum GitHub API rate limit")

// checkAndWaitForRateLimitShared serializes quota checks and their cooldowns so
// concurrent workflow downloads are staggered rather than consuming the API
// budget in synchronized bursts. reserve is the number of additional core
// requests the caller is about to make before its next check; it is added to
// the currently reported usage so a caller that issues several API calls per
// check (e.g. artifact listing plus downloads) does not overshoot a
// configured ceiling between checks.
func checkAndWaitForRateLimitShared(ctx context.Context, verbose bool, configuredMax, reserve int) error {
	select {
	case logsRateLimitGate <- struct{}{}:
		defer func() { <-logsRateLimitGate }()
	case <-ctx.Done():
		return contextCause(ctx)
	}
	return checkAndWaitForRateLimit(ctx, verbose, configuredMax, reserve)
}

func contextCause(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return context.Cause(ctx)
}

// rateLimitResponse models the JSON returned by `gh api rate_limit`.
// Only the "core" resource bucket is used because log downloads and
// workflow-run listing both draw from the core quota.
type rateLimitResponse struct {
	Resources struct {
		Core rateLimitResource `json:"core"`
	} `json:"resources"`
}

// GitHubAPIRateLimitState holds the core API quota at one point in time.
type GitHubAPIRateLimitState struct {
	// Limit is the maximum number of requests allowed per window.
	Limit int `json:"limit"`
	// Remaining is the number of requests still available in the current window.
	Remaining int `json:"remaining"`
	// Reset is the Unix timestamp (seconds) at which the window resets.
	Reset int64 `json:"reset"`
	// Used is the number of requests consumed so far in the current window.
	Used int `json:"used"`
}

type rateLimitResource = GitHubAPIRateLimitState

// GitHubAPIRateLimitReport records the core API quota around a logs command.
type GitHubAPIRateLimitReport struct {
	Host  string                   `json:"host"`
	Start *GitHubAPIRateLimitState `json:"start,omitempty"`
	End   *GitHubAPIRateLimitState `json:"end,omitempty"`
}

func startGitHubAPIRateLimitReport(ctx context.Context, host string) *GitHubAPIRateLimitReport {
	host = normalizedGitHubAPIHost(host)
	state, err := fetchRateLimitForHostFunc(ctx, host)
	if err != nil {
		logsRateLimitLog.Printf("Could not record starting API rate limit: %v", err)
		return &GitHubAPIRateLimitReport{Host: host}
	}
	return &GitHubAPIRateLimitReport{Host: host, Start: &state}
}

func startGitHubAPIRateLimitReports(ctx context.Context, hosts []string) []*GitHubAPIRateLimitReport {
	reports := make([]*GitHubAPIRateLimitReport, 0, len(hosts))
	for _, host := range hosts {
		reports = append(reports, startGitHubAPIRateLimitReport(ctx, host))
	}
	return reports
}

func finishGitHubAPIRateLimitReport(ctx context.Context, report *GitHubAPIRateLimitReport, jsonOutput bool) {
	finishGitHubAPIRateLimitReportToWriter(ctx, report, jsonOutput, os.Stderr)
}

func finishGitHubAPIRateLimitReportToWriter(ctx context.Context, report *GitHubAPIRateLimitReport, jsonOutput bool, warningWriter io.Writer) {
	if report == nil {
		return
	}
	report.Host = normalizedGitHubAPIHost(report.Host)
	state, err := fetchRateLimitForHostFunc(ctx, report.Host)
	if err != nil {
		logsRateLimitLog.Printf("Could not record ending API rate limit: %v", err)
		return
	}
	report.End = &state
	if !jsonOutput && isGitHubAPIRateLimitLow(state) {
		percentRemaining := float64(state.Remaining) * 100 / float64(state.Limit)
		fmt.Fprintln(warningWriter, console.FormatWarningMessage(fmt.Sprintf(
			"GitHub API rate limit for %s is running low: %d of %d core requests remain (%.2f%%); resets at %s",
			report.Host, state.Remaining, state.Limit, percentRemaining, time.Unix(state.Reset, 0).UTC().Format(time.RFC3339),
		)))
	}
}

func finishGitHubAPIRateLimitReports(ctx context.Context, reports []*GitHubAPIRateLimitReport, jsonOutput bool) {
	for _, report := range reports {
		finishGitHubAPIRateLimitReport(ctx, report, jsonOutput)
	}
}

func normalizedGitHubAPIHost(host string) string {
	if host == "" {
		return "github.com"
	}
	return host
}

func logsRateLimitHost(repoOverride string) string {
	parts := strings.SplitN(repoOverride, "/", 3)
	if len(parts) == 3 && strings.Contains(parts[0], ".") {
		return parts[0]
	}
	return ""
}

func isGitHubAPIRateLimitLow(state rateLimitResource) bool {
	return state.Limit > 0 && state.Remaining >= 0 &&
		int64(state.Remaining)*100 <= int64(state.Limit)*int64(RateLimitWarningThresholdPercent)
}

func populatedGitHubAPIRateLimitReport(report *GitHubAPIRateLimitReport) *GitHubAPIRateLimitReport {
	if report == nil || (report.Start == nil && report.End == nil) {
		return nil
	}
	return report
}

func populatedGitHubAPIRateLimitReports(reports []*GitHubAPIRateLimitReport) []*GitHubAPIRateLimitReport {
	populated := make([]*GitHubAPIRateLimitReport, 0, len(reports))
	for _, report := range reports {
		if report = populatedGitHubAPIRateLimitReport(report); report != nil {
			populated = append(populated, report)
		}
	}
	return populated
}

func partitionGitHubAPIRateLimitReports(reports []*GitHubAPIRateLimitReport) (*GitHubAPIRateLimitReport, []*GitHubAPIRateLimitReport) {
	populated := populatedGitHubAPIRateLimitReports(reports)
	if len(populated) == 1 {
		return populated[0], nil
	}
	return nil, populated
}

// fetchRateLimit queries the GitHub API and returns the current core rate-limit
// state.  It is a thin wrapper around `gh api rate_limit` so that callers do
// not need to know about the CLI invocation details.
func fetchRateLimit(ctx context.Context) (rateLimitResource, error) {
	return fetchRateLimitForHost(ctx, "")
}

func fetchRateLimitForHost(ctx context.Context, host string) (rateLimitResource, error) {
	logsRateLimitLog.Print("Querying GitHub API rate limit")

	args := []string{"api", "rate_limit"}
	if host != "" && host != "github.com" {
		args = append(args, "--hostname", host)
	}
	output, err := workflow.RunGHCombinedContext(ctx, "Verifying API quota...", args...)
	if err != nil {
		return rateLimitResource{}, fmt.Errorf("failed to query rate limit: %w", err)
	}

	var resp rateLimitResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return rateLimitResource{}, fmt.Errorf("failed to parse rate limit response: %w", err)
	}

	logsRateLimitLog.Printf("Rate limit: limit=%d, remaining=%d, used=%d, reset=%d",
		resp.Resources.Core.Limit,
		resp.Resources.Core.Remaining,
		resp.Resources.Core.Used,
		resp.Resources.Core.Reset,
	)

	return resp.Resources.Core, nil
}

// sleepWithContext pauses for duration d and returns nil when the timer fires.
// If ctx is cancelled before the timer expires, it stops the timer and returns
// context.Cause(ctx) so callers can propagate cancellation (and any wrapped
// cause) immediately.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	var done <-chan struct{}
	if ctx != nil {
		done = ctx.Done()
	}
	// When ctx is nil, done remains nil. A nil channel is never selected, which
	// intentionally makes cancellation checks a no-op and preserves prior behavior.
	select {
	case <-done:
		return contextCause(ctx)
	default:
	}

	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-done:
		return contextCause(ctx)
	}
}

// checkAndWaitForRateLimit queries the GitHub API rate limit and sleeps until
// the reset window when the remaining core request budget falls at or below
// RateLimitThreshold. Without an explicit ceiling, it waits at least
// APICallCooldown between successive calls. An explicit ceiling instead permits
// full-throughput downloads until the ceiling is reached.
//
// ctx is checked on every sleep so that a user cancellation (Ctrl-C) or
// deadline expiry wakes the function early and propagates the context error.
//
// If the rate limit cannot be fetched (e.g. network error) the function falls
// back to the static APICallCooldown sleep and returns the error so callers
// can decide whether to surface it.
func resolveMaxGitHubAPIRateLimit(configuredMax, apiLimit int) (int, error) {
	if configuredMax == 0 {
		return apiLimit - RateLimitThreshold, nil
	}

	resolvedMax := configuredMax
	if configuredMax < 0 {
		resolvedMax = apiLimit + configuredMax
	}
	if resolvedMax <= 0 || resolvedMax > apiLimit {
		return 0, fmt.Errorf(
			"%w %d for current core limit %d: expected an absolute value from 1 to %d or a negative reserve smaller than %d",
			errInvalidMaxGitHubAPIRateLimit, configuredMax, apiLimit, apiLimit, apiLimit,
		)
	}
	return resolvedMax, nil
}

func checkAndWaitForRateLimit(ctx context.Context, verbose bool, configuredMax, reserve int) error {
	rl, err := fetchRateLimitFunc(ctx)
	if err != nil {
		// Best-effort: fall back to static cooldown so the caller can continue.
		logsRateLimitLog.Printf("Could not fetch rate limit, using static cooldown: %v", err)
		if sleepErr := sleepWithContext(ctx, APICallCooldown); sleepErr != nil {
			return fmt.Errorf("rate-limit fetch failed and context was canceled or timed out during fallback cooldown: %w", errors.Join(err, sleepErr))
		}
		return err
	}

	maxUsed, err := resolveMaxGitHubAPIRateLimit(configuredMax, rl.Limit)
	if err != nil {
		return err
	}

	if rl.Used+reserve > maxUsed {
		resetAt := time.Unix(rl.Reset, 0)
		waitDur := time.Until(resetAt)
		if waitDur <= 0 {
			// Reset has already passed; carry on, retaining the legacy cooldown
			// only when no explicit usage ceiling was configured.
			logsRateLimitLog.Print("Rate limit reset has already passed")
			if configuredMax != 0 {
				return nil
			}
			return sleepWithContext(ctx, APICallCooldown)
		}

		// Add a small buffer so we don't resume right on the boundary.
		waitDur += rateLimitResetBuffer

		msg := fmt.Sprintf(
			"GitHub API usage ceiling reached (%d of %d requests used; maximum %d). Waiting %.0f seconds until reset at %s",
			rl.Used, rl.Limit, maxUsed, waitDur.Seconds(), resetAt.UTC().Format(time.RFC3339),
		)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
		logsRateLimitLog.Printf("Sleeping for rate limit reset: duration=%s", waitDur)
		return sleepWithContext(ctx, waitDur)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
			fmt.Sprintf("Rate limit OK: %d/%d requests used (maximum %d)", rl.Used, rl.Limit, maxUsed),
		))
	}

	// An explicit ceiling enables full-throughput downloads until that ceiling is
	// reached. Preserve the legacy cooldown when no ceiling was configured.
	if configuredMax != 0 {
		return nil
	}
	return sleepWithContext(ctx, APICallCooldown)
}
