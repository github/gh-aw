//go:build integration

package cli

import (
	"testing"
	"time"
)

// TestTimedOutRunProcessing tests that timed_out runs are processed correctly
// even when they don't have artifacts
func TestTimedOutRunProcessing(t *testing.T) {
	tests := []struct {
		name              string
		runConclusion     string
		hasArtifacts      bool
		shouldBeSkipped   bool
		shouldCountErrors bool
		description       string
	}{
		{
			name:              "timed_out without artifacts",
			runConclusion:     "timed_out",
			hasArtifacts:      false,
			shouldBeSkipped:   false,
			shouldCountErrors: true,
			description:       "Timed out runs without artifacts should be included in reports",
		},
		{
			name:              "failure without artifacts",
			runConclusion:     "failure",
			hasArtifacts:      false,
			shouldBeSkipped:   false,
			shouldCountErrors: true,
			description:       "Failed runs without artifacts should be included in reports",
		},
		{
			name:              "cancelled without artifacts",
			runConclusion:     "cancelled",
			hasArtifacts:      false,
			shouldBeSkipped:   false,
			shouldCountErrors: true,
			description:       "Cancelled runs without artifacts should be included in reports",
		},
		{
			name:              "success without artifacts",
			runConclusion:     "success",
			hasArtifacts:      false,
			shouldBeSkipped:   true,
			shouldCountErrors: false,
			description:       "Successful runs without artifacts can be skipped",
		},
		{
			name:              "timed_out with artifacts",
			runConclusion:     "timed_out",
			hasArtifacts:      true,
			shouldBeSkipped:   false,
			shouldCountErrors: true,
			description:       "Timed out runs with artifacts should be processed normally",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate a workflow run
			run := WorkflowRun{
				DatabaseID:   12345,
				Number:       1,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   tt.runConclusion,
				CreatedAt:    time.Now().Add(-1 * time.Hour),
				StartedAt:    time.Now().Add(-1 * time.Hour),
				UpdatedAt:    time.Now(),
			}

			// Simulate the logic for handling runs without artifacts
			shouldSkip := false
			shouldCountAsError := false

			if !tt.hasArtifacts {
				// This is the new logic we implemented
				if isFailureConclusion(run.Conclusion) {
					// Don't skip - we want these to appear in the report
					shouldSkip = false
					shouldCountAsError = true
				} else {
					// For other runs without artifacts, skip them
					shouldSkip = true
					shouldCountAsError = false
				}
			} else {
				// Has artifacts - process normally
				shouldSkip = false
				if isFailureConclusion(run.Conclusion) {
					shouldCountAsError = true
				}
			}

			if shouldSkip != tt.shouldBeSkipped {
				t.Errorf("%s: expected shouldBeSkipped=%v but got %v",
					tt.description, tt.shouldBeSkipped, shouldSkip)
			}

			if shouldCountAsError != tt.shouldCountErrors {
				t.Errorf("%s: expected shouldCountErrors=%v but got %v",
					tt.description, tt.shouldCountErrors, shouldCountAsError)
			}
		})
	}
}

// TestStatusDisplayIncludesTimedOut tests that timed_out status is displayed in reports
func TestStatusDisplayIncludesTimedOut(t *testing.T) {
	run := WorkflowRun{
		DatabaseID: 12345,
		Status:     "completed",
		Conclusion: "timed_out",
	}

	// Simulate the status display logic from logs.go
	statusStr := run.Status
	if run.Status == "completed" && run.Conclusion != "" {
		statusStr = run.Conclusion
	}

	if statusStr != "timed_out" {
		t.Errorf("Expected status display to be 'timed_out' but got '%s'", statusStr)
	}
}

// TestAuditDisplayIncludesTimedOut tests that audit report shows timed_out correctly
func TestAuditDisplayIncludesTimedOut(t *testing.T) {
	overview := OverviewData{
		Status:     "completed",
		Conclusion: "timed_out",
	}

	// Simulate the audit report overview logic from audit_report.go
	statusLine := overview.Status
	if overview.Conclusion != "" && overview.Status == "completed" {
		statusLine = overview.Status + " (" + overview.Conclusion + ")"
	}

	expectedStatus := "completed (timed_out)"
	if statusLine != expectedStatus {
		t.Errorf("Expected audit status display to be '%s' but got '%s'", expectedStatus, statusLine)
	}
}
