//go:build !integration

package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDownloadRunArtifactsParallel(t *testing.T) {
	// Test with empty runs slice
	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{}, "./test-logs", false, 5)
	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty runs, got %d", len(results))
	}

	// Test with mock runs
	runs := []WorkflowRun{
		{
			DatabaseID:   12345,
			Number:       1,
			Status:       "completed",
			Conclusion:   "success",
			WorkflowName: "Test Workflow",
			CreatedAt:    time.Now().Add(-1 * time.Hour),
			StartedAt:    time.Now().Add(-55 * time.Minute),
			UpdatedAt:    time.Now().Add(-50 * time.Minute),
		},
		{
			DatabaseID:   12346,
			Number:       2,
			Status:       "completed",
			Conclusion:   "failure",
			WorkflowName: "Test Workflow",
			CreatedAt:    time.Now().Add(-2 * time.Hour),
			StartedAt:    time.Now().Add(-115 * time.Minute),
			UpdatedAt:    time.Now().Add(-110 * time.Minute),
		},
	}

	// This will fail since we don't have real GitHub CLI access,
	// but we can verify the structure and that no panics occur
	results = downloadRunArtifactsConcurrent(context.Background(), runs, "./test-logs", false, 5)

	// We expect 2 results even if they fail
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Verify we have results for all our runs (order may vary due to parallel execution)
	foundRuns := make(map[int64]bool)
	for _, result := range results {
		foundRuns[result.Run.DatabaseID] = true

		// Verify the LogsPath follows the expected pattern (normalize path separators)
		expectedSuffix := fmt.Sprintf("run-%d", result.Run.DatabaseID)
		if !strings.Contains(result.LogsPath, expectedSuffix) {
			t.Errorf("Expected LogsPath to contain %s, got %s", expectedSuffix, result.LogsPath)
		}
	}

	// Verify we processed all the runs we sent
	for _, run := range runs {
		if !foundRuns[run.DatabaseID] {
			t.Errorf("Missing result for run %d", run.DatabaseID)
		}
	}
}

func TestDownloadRunArtifactsParallelMaxRuns(t *testing.T) {
	// Test that all runs in the batch are processed, regardless of maxRuns
	// This behavior is necessary to correctly handle caching and filtering,
	// where runs may be cached but fail filters, requiring us to check more runs.
	runs := []WorkflowRun{
		{DatabaseID: 1, Status: "completed"},
		{DatabaseID: 2, Status: "completed"},
		{DatabaseID: 3, Status: "completed"},
		{DatabaseID: 4, Status: "completed"},
		{DatabaseID: 5, Status: "completed"},
	}

	// Pass maxRuns=3 as a hint that we need 3 results, but all runs should be processed
	results := downloadRunArtifactsConcurrent(context.Background(), runs, "./test-logs", false, 3)

	// All runs should be processed to account for potential caching/filtering
	if len(results) != 5 {
		t.Errorf("Expected 5 results (all runs processed), got %d", len(results))
	}

	// Verify we got results for all runs (order may vary due to parallel execution)
	expectedIDs := map[int64]bool{1: false, 2: false, 3: false, 4: false, 5: false}
	for _, result := range results {
		if _, expected := expectedIDs[result.Run.DatabaseID]; expected {
			expectedIDs[result.Run.DatabaseID] = true
		} else {
			t.Errorf("Got unexpected DatabaseID %d", result.Run.DatabaseID)
		}
	}

	// Verify all expected IDs were found
	for id, found := range expectedIDs {
		if !found {
			t.Errorf("Missing expected DatabaseID %d", id)
		}
	}
}

func TestDownloadResult(t *testing.T) {
	// Test DownloadResult structure
	run := WorkflowRun{
		DatabaseID: 12345,
		Status:     "completed",
	}

	result := DownloadResult{
		Run:      run,
		LogsPath: "./test-path",
		Skipped:  false,
		Cached:   false,
		Error:    nil,
	}

	if result.Run.DatabaseID != 12345 {
		t.Errorf("Expected DatabaseID 12345, got %d", result.Run.DatabaseID)
	}

	if result.LogsPath != "./test-path" {
		t.Errorf("Expected LogsPath './test-path', got %s", result.LogsPath)
	}

	if result.Skipped {
		t.Error("Expected Skipped to be false")
	}

	if result.Cached {
		t.Error("Expected Cached to be false")
	}

	if result.Error != nil {
		t.Errorf("Expected Error to be nil, got %v", result.Error)
	}

	// Test cached result
	cachedResult := DownloadResult{
		Run:      run,
		LogsPath: "./test-path",
		Cached:   true,
	}

	if !cachedResult.Cached {
		t.Error("Expected Cached to be true")
	}

	// Cached results should be counted as successful (no error, not skipped)
	if cachedResult.Error != nil || cachedResult.Skipped {
		t.Error("Cached results should have no error and not be skipped")
	}
}

func TestMaxConcurrentDownloads(t *testing.T) {
	// Test that MaxConcurrentDownloads constant is properly defined
	if MaxConcurrentDownloads <= 0 {
		t.Errorf("MaxConcurrentDownloads should be positive, got %d", MaxConcurrentDownloads)
	}

	if MaxConcurrentDownloads > 20 {
		t.Errorf("MaxConcurrentDownloads should be reasonable (<=20), got %d", MaxConcurrentDownloads)
	}
}

func TestGetMaxConcurrentDownloads(t *testing.T) {
	// Save original env value
	originalValue := os.Getenv("GH_AW_MAX_CONCURRENT_DOWNLOADS")
	defer func() {
		if originalValue != "" {
			os.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", originalValue)
		} else {
			os.Unsetenv("GH_AW_MAX_CONCURRENT_DOWNLOADS")
		}
	}()

	tests := []struct {
		name     string
		envValue string
		expected int
	}{
		{
			name:     "default when env var not set",
			envValue: "",
			expected: MaxConcurrentDownloads,
		},
		{
			name:     "valid value 5",
			envValue: "5",
			expected: 5,
		},
		{
			name:     "valid value 1 (minimum)",
			envValue: "1",
			expected: 1,
		},
		{
			name:     "valid value 100 (maximum)",
			envValue: "100",
			expected: 100,
		},
		{
			name:     "valid value 50",
			envValue: "50",
			expected: 50,
		},
		{
			name:     "invalid non-numeric value",
			envValue: "invalid",
			expected: MaxConcurrentDownloads,
		},
		{
			name:     "invalid zero value",
			envValue: "0",
			expected: MaxConcurrentDownloads,
		},
		{
			name:     "invalid negative value",
			envValue: "-5",
			expected: MaxConcurrentDownloads,
		},
		{
			name:     "invalid too large value",
			envValue: "101",
			expected: MaxConcurrentDownloads,
		},
		{
			name:     "invalid extremely large value",
			envValue: "1000",
			expected: MaxConcurrentDownloads,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				os.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", tt.envValue)
			} else {
				os.Unsetenv("GH_AW_MAX_CONCURRENT_DOWNLOADS")
			}

			// Test the function
			result := getMaxConcurrentDownloads()
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestDownloadRunArtifactsParallelWithCancellation tests context cancellation during concurrent downloads
func TestDownloadRunArtifactsParallelWithCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test with mock runs
	runs := []WorkflowRun{
		{
			DatabaseID:   12345,
			Number:       1,
			Status:       "completed",
			Conclusion:   "success",
			WorkflowName: "Test Workflow",
		},
		{
			DatabaseID:   12346,
			Number:       2,
			Status:       "completed",
			Conclusion:   "failure",
			WorkflowName: "Test Workflow",
		},
	}

	// Download with cancelled context
	results := downloadRunArtifactsConcurrent(ctx, runs, "./test-logs", false, 5)

	// Should get results for all runs
	if len(results) != 2 {
		t.Errorf("Expected 2 results even with cancelled context, got %d", len(results))
	}

	// All results should be skipped due to context cancellation
	for _, result := range results {
		if !result.Skipped {
			t.Errorf("Expected result for run %d to be skipped due to cancelled context", result.Run.DatabaseID)
		}
		if result.Error != context.Canceled {
			t.Errorf("Expected error to be context.Canceled for run %d, got %v", result.Run.DatabaseID, result.Error)
		}
	}
}
