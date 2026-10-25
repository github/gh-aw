//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestRepeatOutputFailurePreservesExecutionAndCleanup(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			}
			calls, cleanups := 0, 0
			err := ExecuteWithRepeat(RepeatOptions{
				Ctx: ctx, RepeatCount: 1,
				ExecuteFunc: func() error { calls++; return nil },
				CleanupFunc: func() { cleanups++ },
			})
			if cancelled {
				if !errors.Is(err, context.Canceled) || calls != 1 || cleanups != 1 {
					t.Fatalf("cancelled repeat: error=%v executions=%d cleanups=%d", err, calls, cleanups)
				}
			} else if err != nil || calls != 2 || cleanups != 0 {
				t.Fatalf("repeat: error=%v executions=%d cleanups=%d", err, calls, cleanups)
			}
		})
	}
}

type cliOutputFailingWriter struct {
	calls  int
	failAt int
}

func (w *cliOutputFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func TestCLIOutputRenderersStopOnWriteError(t *testing.T) {
	data := LogsData{
		Summary:       LogsSummary{TotalRuns: 1, EngineCounts: map[string]int{"copilot": 1, "claude": 1}},
		Runs:          []RunData{{RunID: 1}, {RunID: 2}},
		Observability: []ObservabilityInsight{{Severity: "high", Title: "Insight", Summary: "Summary"}},
		FirewallLog:   &FirewallLogSummary{TotalRequests: 2, AllowedRequests: 1, BlockedRequests: 1},
	}
	analysis := &ClusterAnalysis{
		Patterns: []ClusterPattern{{Severity: "high", Kind: "test", Title: "Pattern", Description: "Description"}},
		Clusters: []RunCluster{{Dimension: "conclusion", Value: "success", Count: 1}},
	}
	profile := &resolvedBootstrapProfile{
		PackageID: "owner/package",
		Profile: &repositoryPackageBootstrap{Config: []repositoryPackageBootstrapAction{
			{Type: "require-owner-type", Value: "Organization"},
			{Type: "repo-variable", Name: "VARIABLE", Prompt: "Prompt", Optional: true},
			{Type: "repo-secret", Name: "SECRET", Prompt: "Prompt", Optional: true},
			{Type: "repo-label", Name: "label", Color: "ffffff"},
			{Type: "github-app"},
			{Type: "copilot-auth"},
			{Type: "commit-and-push", Message: "Commit"},
			{Type: "handoff", Message: "Next steps"},
		}},
	}
	tests := []struct {
		name   string
		render func(io.Writer)
	}{
		{"TSV", func(w io.Writer) { renderLogsTSVToWriter(w, data) }},
		{"verbose TSV", func(w io.Writer) { renderLogsTSVVerboseToWriter(w, data) }},
		{"Markdown clusters", func(w io.Writer) { renderMarkdownClusterAnalysisToWriter(w, analysis) }},
		{"bootstrap checklist", func(w io.Writer) { printBootstrapConfigTODO(w, profile) }},
		{"observability", func(w io.Writer) {
			renderObservabilityInsightsTo(w, []ObservabilityInsight{
				{Severity: "high", Title: "Insight", Summary: "Summary", Evidence: "Evidence"},
				{Severity: "low", Title: "Other", Summary: "Other summary"},
			})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			success := &cliOutputFailingWriter{}
			tt.render(success)
			if success.calls == 0 {
				t.Fatal("renderer produced no output")
			}
			for failAt := 1; failAt <= success.calls; failAt++ {
				t.Run(fmt.Sprintf("write %d", failAt), func(t *testing.T) {
					w := &cliOutputFailingWriter{failAt: failAt}
					tt.render(w)
					if w.calls != failAt {
						t.Fatalf("writes = %d, want %d: renderer continued after failure", w.calls, failAt)
					}
				})
			}
		})
	}
}

func TestRateLimitWarningWriteErrorPreservesReport(t *testing.T) {
	oldFetch := fetchRateLimitForHostFunc
	defer func() { fetchRateLimitForHostFunc = oldFetch }()
	state := rateLimitResource{Limit: 5000, Remaining: 1, Used: 4999}
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return state, nil
	}
	report := &GitHubAPIRateLimitReport{Host: "github.com"}
	w := &cliOutputFailingWriter{failAt: 1}
	finishGitHubAPIRateLimitReportToWriter(context.Background(), report, false, w)
	if report.End == nil || *report.End != state {
		t.Fatalf("ending quota was lost after warning write failed: %+v", report)
	}
	if w.calls != 1 {
		t.Fatalf("warning writes = %d, want 1", w.calls)
	}
}
