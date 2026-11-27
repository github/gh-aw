//go:build !integration

package cli

import (
	"testing"
	"time"
)

// TestIsNonDispatchedConclusion verifies that conclusions for runs which never
// dispatched a job are recognised, so they are excluded from executed-run metrics.
func TestIsNonDispatchedConclusion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		conclusion string
		want       bool
	}{
		{conclusion: "skipped", want: true},
		{conclusion: "action_required", want: true},
		{conclusion: "success", want: false},
		{conclusion: "failure", want: false},
		{conclusion: "cancelled", want: false},
		{conclusion: "timed_out", want: false},
		{conclusion: "neutral", want: false},
		{conclusion: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.conclusion, func(t *testing.T) {
			t.Parallel()
			if got := isNonDispatchedConclusion(tt.conclusion); got != tt.want {
				t.Errorf("isNonDispatchedConclusion(%q) = %v, want %v", tt.conclusion, got, tt.want)
			}
		})
	}
}

// TestIsCompletedDispatchedRunExcludesNonDispatched verifies that runs held for
// manual approval (action_required) are not treated as executed runs. Command
// workflows such as `q` accumulate large numbers of these runs when a bot actor
// comments on an issue, which previously dragged their reported success rate
// down to near zero.
func TestIsCompletedDispatchedRunExcludesNonDispatched(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     string
		conclusion string
		want       bool
	}{
		{name: "completed success", status: "completed", conclusion: "success", want: true},
		{name: "completed failure", status: "completed", conclusion: "failure", want: true},
		{name: "completed skipped", status: "completed", conclusion: "skipped", want: false},
		{name: "completed action_required", status: "completed", conclusion: "action_required", want: false},
		{name: "in progress", status: "in_progress", conclusion: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := WorkflowRun{Status: tt.status, Conclusion: tt.conclusion}
			if got := isCompletedDispatchedRun(run); got != tt.want {
				t.Errorf("isCompletedDispatchedRun(%+v) = %v, want %v", run, got, tt.want)
			}
		})
	}
}

// TestCalculateWorkflowHealthOnDispatchedRuns documents the reporting impact of
// the fix: once non-dispatched runs are filtered out of the run set, the success
// rate reflects only runs that actually executed the agent.
func TestCalculateWorkflowHealthOnDispatchedRuns(t *testing.T) {
	all := []WorkflowRun{
		{Status: "completed", Conclusion: "success"},
		{Status: "completed", Conclusion: "failure"},
	}
	for range 20 {
		all = append(all, WorkflowRun{Status: "completed", Conclusion: "action_required"})
	}

	dispatched := make([]WorkflowRun, 0, len(all))
	for _, run := range all {
		if isNonDispatchedConclusion(run.Conclusion) {
			continue
		}
		dispatched = append(dispatched, run)
	}

	health := CalculateWorkflowHealth("q", dispatched, 50)
	if health.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", health.TotalRuns)
	}
	if health.SuccessRate != 50 {
		t.Errorf("SuccessRate = %v, want 50", health.SuccessRate)
	}
}

// TestFetchWorkflowRunsPaginatesPastFilteredBatches verifies that a batch made
// entirely of non-dispatched runs does not terminate pagination: numbered pages
// advance within the same search window so older dispatched runs are collected.
func TestFetchWorkflowRunsPaginatesPastFilteredBatches(t *testing.T) {
	original := healthListWorkflowRuns
	t.Cleanup(func() { healthListWorkflowRuns = original })

	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	var calls []int
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, collection *HealthCollection) ([]WorkflowRun, int, error) {
		calls = append(calls, page)
		if opts.BeforeDate != "" {
			t.Fatalf("BeforeDate = %q, want unchanged search window before the search-result limit", opts.BeforeDate)
		}
		if opts.OldestFetchedCreatedAt == nil {
			t.Fatal("fetchWorkflowRuns must request the oldest raw run timestamp for pagination")
		}
		switch len(calls) {
		case 1:
			// A full raw batch that filters down to nothing (all approval-pending).
			*opts.OldestFetchedCreatedAt = now.Add(-time.Hour)
			return nil, opts.Limit, nil
		default:
			*opts.OldestFetchedCreatedAt = now.Add(-2 * time.Hour)
			return []WorkflowRun{{Status: "completed", Conclusion: "success", CreatedAt: now.Add(-2 * time.Hour)}}, 1, nil
		}
	}

	runs, err := fetchWorkflowRuns("q.lock.yml", "2026-08-01", "", false)
	if err != nil {
		t.Fatalf("fetchWorkflowRuns returned error: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1 (older dispatched run must survive a fully filtered batch)", len(runs))
	}
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 batches", len(calls))
	}
	if calls[0] != 1 || calls[1] != 2 {
		t.Errorf("pages = %v, want [1 2]", calls)
	}
	if runs[0].Conclusion != "success" || !runs[0].CreatedAt.Equal(now.Add(-2*time.Hour)) {
		t.Errorf("run = %+v, want the older successful run", runs[0])
	}
}

// TestFetchWorkflowRunsStopsWhenSourceExhausted verifies that a raw batch smaller
// than the requested limit ends pagination instead of looping to MaxIterations.
func TestFetchWorkflowRunsStopsWhenSourceExhausted(t *testing.T) {
	original := healthListWorkflowRuns
	t.Cleanup(func() { healthListWorkflowRuns = original })

	callCount := 0
	healthListWorkflowRuns = func(opts ListWorkflowRunsOptions, page int, collection *HealthCollection) ([]WorkflowRun, int, error) {
		callCount++
		return nil, 0, nil
	}

	runs, err := fetchWorkflowRuns("q.lock.yml", "2026-08-01", "", false)
	if err != nil {
		t.Fatalf("fetchWorkflowRuns returned error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("len(runs) = %d, want 0", len(runs))
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1", callCount)
	}
}
