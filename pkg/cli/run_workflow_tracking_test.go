//go:build !integration

package cli

import (
	"testing"
	"time"
)

// TestWorkflowRunInfo tests the WorkflowRunInfo struct
func TestWorkflowRunInfo(t *testing.T) {
	// Test struct initialization
	runInfo := WorkflowRunInfo{
		URL:        "https://github.com/owner/repo/actions/runs/123456",
		DatabaseID: 123456,
		Status:     "completed",
		Conclusion: "success",
		CreatedAt:  time.Now(),
	}

	if runInfo.URL == "" {
		t.Error("Expected URL to be set")
	}
	if runInfo.DatabaseID == 0 {
		t.Error("Expected DatabaseID to be set")
	}
	if runInfo.Status == "" {
		t.Error("Expected Status to be set")
	}
	if runInfo.Conclusion == "" {
		t.Error("Expected Conclusion to be set")
	}
	if runInfo.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
}

// TestWorkflowRunInfo_EmptyValues tests handling of empty values
func TestWorkflowRunInfo_EmptyValues(t *testing.T) {
	// Test with empty values - should be valid
	runInfo := WorkflowRunInfo{
		URL:        "",
		DatabaseID: 0,
		Status:     "",
		Conclusion: "",
		CreatedAt:  time.Time{},
	}

	// These should be accessible without panicking
	_ = runInfo.URL
	_ = runInfo.DatabaseID
	_ = runInfo.Status
	_ = runInfo.Conclusion
	_ = runInfo.CreatedAt
}

// TestWorkflowRunInfo_StatusValues tests various status values
func TestWorkflowRunInfo_StatusValues(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
	}{
		{
			name:       "completed successfully",
			status:     "completed",
			conclusion: "success",
		},
		{
			name:       "completed with failure",
			status:     "completed",
			conclusion: "failure",
		},
		{
			name:       "in progress",
			status:     "in_progress",
			conclusion: "",
		},
		{
			name:       "queued",
			status:     "queued",
			conclusion: "",
		},
		{
			name:       "cancelled",
			status:     "completed",
			conclusion: "cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runInfo := WorkflowRunInfo{
				URL:        "https://github.com/owner/repo/actions/runs/123456",
				DatabaseID: 123456,
				Status:     tt.status,
				Conclusion: tt.conclusion,
				CreatedAt:  time.Now(),
			}

			if runInfo.Status != tt.status {
				t.Errorf("Expected status %s, got %s", tt.status, runInfo.Status)
			}
			if runInfo.Conclusion != tt.conclusion {
				t.Errorf("Expected conclusion %s, got %s", tt.conclusion, runInfo.Conclusion)
			}
		})
	}
}

// TestWorkflowRunInfo_TimestampHandling tests timestamp handling
func TestWorkflowRunInfo_TimestampHandling(t *testing.T) {
	tests := []struct {
		name      string
		createdAt time.Time
		isZero    bool
	}{
		{
			name:      "valid timestamp",
			createdAt: time.Now(),
			isZero:    false,
		},
		{
			name:      "zero timestamp",
			createdAt: time.Time{},
			isZero:    true,
		},
		{
			name:      "past timestamp",
			createdAt: time.Now().Add(-1 * time.Hour),
			isZero:    false,
		},
		{
			name:      "future timestamp",
			createdAt: time.Now().Add(1 * time.Hour),
			isZero:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runInfo := WorkflowRunInfo{
				URL:        "https://github.com/owner/repo/actions/runs/123456",
				DatabaseID: 123456,
				Status:     "completed",
				Conclusion: "success",
				CreatedAt:  tt.createdAt,
			}

			if runInfo.CreatedAt.IsZero() != tt.isZero {
				t.Errorf("Expected IsZero=%v, got %v", tt.isZero, runInfo.CreatedAt.IsZero())
			}
		})
	}
}

// TestWorkflowRunInfo_URLFormats tests various URL formats
func TestWorkflowRunInfo_URLFormats(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{
			name: "standard GitHub URL",
			url:  "https://github.com/owner/repo/actions/runs/123456",
		},
		{
			name: "GitHub Enterprise URL",
			url:  "https://github.company.com/owner/repo/actions/runs/123456",
		},
		{
			name: "URL with query parameters",
			url:  "https://github.com/owner/repo/actions/runs/123456?check_suite_focus=true",
		},
		{
			name: "empty URL",
			url:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runInfo := WorkflowRunInfo{
				URL:        tt.url,
				DatabaseID: 123456,
				Status:     "completed",
				Conclusion: "success",
				CreatedAt:  time.Now(),
			}

			if runInfo.URL != tt.url {
				t.Errorf("Expected URL %s, got %s", tt.url, runInfo.URL)
			}
		})
	}
}

// Note: getLatestWorkflowRunWithRetry is not tested here as it requires
// GitHub CLI integration and network access. It should be tested in
// integration tests with appropriate fixtures or mocks.
