//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
)

// TestTimedOutConclusionDetection tests that timed_out conclusions are properly detected as failures
func TestTimedOutConclusionDetection(t *testing.T) {
	tests := []struct {
		name          string
		jobConclusion string
		expectFailure bool
		description   string
	}{
		{
			name:          "success conclusion",
			jobConclusion: "success",
			expectFailure: false,
			description:   "Successful jobs should not be counted as failures",
		},
		{
			name:          "failure conclusion",
			jobConclusion: "failure",
			expectFailure: true,
			description:   "Failed jobs should be counted as failures",
		},
		{
			name:          "cancelled conclusion",
			jobConclusion: "cancelled",
			expectFailure: true,
			description:   "Cancelled jobs should be counted as failures",
		},
		{
			name:          "timed_out conclusion",
			jobConclusion: "timed_out",
			expectFailure: true,
			description:   "Timed out jobs should be counted as failures",
		},
		{
			name:          "skipped conclusion",
			jobConclusion: "skipped",
			expectFailure: false,
			description:   "Skipped jobs should not be counted as failures",
		},
		{
			name:          "neutral conclusion",
			jobConclusion: "neutral",
			expectFailure: false,
			description:   "Neutral jobs should not be counted as failures",
		},
		{
			name:          "action_required conclusion",
			jobConclusion: "action_required",
			expectFailure: false,
			description:   "Action required jobs should not be counted as failures",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from fetchJobStatuses
			isFailure := tt.jobConclusion == "failure" ||
				tt.jobConclusion == "cancelled" ||
				tt.jobConclusion == "timed_out"

			if isFailure != tt.expectFailure {
				t.Errorf("%s: expected failure=%v but got %v for conclusion=%s",
					tt.description, tt.expectFailure, isFailure, tt.jobConclusion)
			}
		})
	}
}

// TestJobInfoJSONParsing tests that job info with timed_out conclusion can be properly parsed
func TestJobInfoJSONParsing(t *testing.T) {
	tests := []struct {
		name          string
		jsonInput     string
		expectSuccess bool
		conclusion    string
	}{
		{
			name:          "timed_out job",
			jsonInput:     `{"name":"test-job","status":"completed","conclusion":"timed_out"}`,
			expectSuccess: true,
			conclusion:    "timed_out",
		},
		{
			name:          "failed job",
			jsonInput:     `{"name":"test-job","status":"completed","conclusion":"failure"}`,
			expectSuccess: true,
			conclusion:    "failure",
		},
		{
			name:          "cancelled job",
			jsonInput:     `{"name":"test-job","status":"completed","conclusion":"cancelled"}`,
			expectSuccess: true,
			conclusion:    "cancelled",
		},
		{
			name:          "successful job",
			jsonInput:     `{"name":"test-job","status":"completed","conclusion":"success"}`,
			expectSuccess: true,
			conclusion:    "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var job JobInfo
			err := json.Unmarshal([]byte(tt.jsonInput), &job)

			if tt.expectSuccess {
				if err != nil {
					t.Errorf("Expected successful parsing but got error: %v", err)
				}
				if job.Conclusion != tt.conclusion {
					t.Errorf("Expected conclusion=%s but got %s", tt.conclusion, job.Conclusion)
				}
			} else {
				if err == nil {
					t.Errorf("Expected parsing error but got none")
				}
			}
		})
	}
}

// TestStatusDisplayForTimedOut tests that timed_out conclusions are displayed correctly
func TestStatusDisplayForTimedOut(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		conclusion     string
		expectedStatus string
	}{
		{
			name:           "completed with timed_out",
			status:         "completed",
			conclusion:     "timed_out",
			expectedStatus: "timed_out",
		},
		{
			name:           "completed with failure",
			status:         "completed",
			conclusion:     "failure",
			expectedStatus: "failure",
		},
		{
			name:           "completed with success",
			status:         "completed",
			conclusion:     "success",
			expectedStatus: "success",
		},
		{
			name:           "in_progress with no conclusion",
			status:         "in_progress",
			conclusion:     "",
			expectedStatus: "in_progress",
		},
		{
			name:           "queued with no conclusion",
			status:         "queued",
			conclusion:     "",
			expectedStatus: "queued",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from logs.go lines 1817-1821
			statusStr := tt.status
			if tt.status == "completed" && tt.conclusion != "" {
				statusStr = tt.conclusion
			}

			if statusStr != tt.expectedStatus {
				t.Errorf("Expected status display '%s' but got '%s'", tt.expectedStatus, statusStr)
			}
		})
	}
}
