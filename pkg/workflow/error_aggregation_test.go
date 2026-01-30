//go:build !integration

package workflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewErrorCollector(t *testing.T) {
	tests := []struct {
		name     string
		failFast bool
	}{
		{
			name:     "fail-fast enabled",
			failFast: true,
		},
		{
			name:     "fail-fast disabled",
			failFast: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewErrorCollector(tt.failFast)
			require.NotNil(t, collector, "Collector should be created")
			assert.Equal(t, tt.failFast, collector.failFast, "Fail-fast setting should match")
			assert.False(t, collector.HasErrors(), "New collector should have no errors")
			assert.Equal(t, 0, collector.Count(), "New collector should have zero count")
		})
	}
}

func TestErrorCollectorAdd_FailFast(t *testing.T) {
	collector := NewErrorCollector(true)
	err1 := fmt.Errorf("first error")
	err2 := fmt.Errorf("second error")

	// First error should be returned immediately
	result := collector.Add(err1)
	require.Error(t, result, "Should return error immediately in fail-fast mode")
	assert.Equal(t, err1, result, "Should return the exact error")
	assert.False(t, collector.HasErrors(), "Should not collect errors in fail-fast mode")

	// Second error should also be returned immediately
	result = collector.Add(err2)
	require.Error(t, result, "Should return error immediately in fail-fast mode")
	assert.Equal(t, err2, result, "Should return the exact error")
}

func TestErrorCollectorAdd_Aggregate(t *testing.T) {
	collector := NewErrorCollector(false)
	err1 := fmt.Errorf("first error")
	err2 := fmt.Errorf("second error")
	err3 := fmt.Errorf("third error")

	// Add errors should not return them
	result := collector.Add(err1)
	require.NoError(t, result, "Should not return error in aggregate mode")
	assert.True(t, collector.HasErrors(), "Should have errors")
	assert.Equal(t, 1, collector.Count(), "Should have 1 error")

	result = collector.Add(err2)
	require.NoError(t, result, "Should not return error in aggregate mode")
	assert.Equal(t, 2, collector.Count(), "Should have 2 errors")

	result = collector.Add(err3)
	require.NoError(t, result, "Should not return error in aggregate mode")
	assert.Equal(t, 3, collector.Count(), "Should have 3 errors")
}

func TestErrorCollectorAdd_NilError(t *testing.T) {
	collector := NewErrorCollector(false)

	result := collector.Add(nil)
	require.NoError(t, result, "Should handle nil error")
	assert.False(t, collector.HasErrors(), "Should not have errors")
	assert.Equal(t, 0, collector.Count(), "Should have zero count")
}

func TestErrorCollectorError_NoErrors(t *testing.T) {
	collector := NewErrorCollector(false)

	err := collector.Error()
	assert.NoError(t, err, "Should return nil when no errors collected")
}

func TestErrorCollectorError_SingleError(t *testing.T) {
	collector := NewErrorCollector(false)
	err1 := fmt.Errorf("single error")

	_ = collector.Add(err1)
	result := collector.Error()

	require.Error(t, result, "Should return error")
	assert.Equal(t, err1, result, "Should return the single error as-is")
}

func TestErrorCollectorError_MultipleErrors(t *testing.T) {
	collector := NewErrorCollector(false)
	err1 := fmt.Errorf("first error")
	err2 := fmt.Errorf("second error")
	err3 := fmt.Errorf("third error")

	_ = collector.Add(err1)
	_ = collector.Add(err2)
	_ = collector.Add(err3)

	result := collector.Error()
	require.Error(t, result, "Should return aggregated error")

	// Check that all errors are included
	errStr := result.Error()
	assert.Contains(t, errStr, "first error", "Should contain first error")
	assert.Contains(t, errStr, "second error", "Should contain second error")
	assert.Contains(t, errStr, "third error", "Should contain third error")
}

func TestFormatAggregatedError_NoError(t *testing.T) {
	result := FormatAggregatedError(nil, "validation")
	require.NoError(t, result, "Should handle nil error")
}

func TestFormatAggregatedError_SingleError(t *testing.T) {
	err := fmt.Errorf("single error")
	result := FormatAggregatedError(err, "validation")

	require.Error(t, result, "Should return error")
	assert.Equal(t, err, result, "Should return single error unchanged")
}

func TestFormatAggregatedError_MultipleErrors(t *testing.T) {
	err1 := fmt.Errorf("first error")
	err2 := fmt.Errorf("second error")
	err3 := fmt.Errorf("third error")

	joined := errors.Join(err1, err2, err3)
	result := FormatAggregatedError(joined, "validation")

	require.Error(t, result, "Should return formatted error")
	errStr := result.Error()

	// Should contain header with count
	assert.True(t, strings.Contains(errStr, "Found") && strings.Contains(errStr, "validation errors:"),
		"Should contain header with count and category")

	// Should contain all individual errors
	assert.Contains(t, errStr, "first error", "Should contain first error")
	assert.Contains(t, errStr, "second error", "Should contain second error")
	assert.Contains(t, errStr, "third error", "Should contain third error")
}

func TestSplitJoinedErrors_NoError(t *testing.T) {
	result := SplitJoinedErrors(nil)
	assert.Nil(t, result, "Should return nil for nil error")
}

func TestSplitJoinedErrors_SingleError(t *testing.T) {
	err := fmt.Errorf("single error")
	result := SplitJoinedErrors(err)

	require.Len(t, result, 1, "Should have 1 error")
	assert.Equal(t, err, result[0], "Should contain the single error")
}

func TestSplitJoinedErrors_MultipleErrors(t *testing.T) {
	err1 := fmt.Errorf("first error")
	err2 := fmt.Errorf("second error")
	err3 := fmt.Errorf("third error")

	joined := errors.Join(err1, err2, err3)
	result := SplitJoinedErrors(joined)

	require.NotNil(t, result, "Should return error slice")
	assert.Greater(t, len(result), 1, "Should have multiple errors")

	// Check that all errors are present in the result
	errStr := joined.Error()
	assert.Contains(t, errStr, "first error", "Should contain first error")
	assert.Contains(t, errStr, "second error", "Should contain second error")
	assert.Contains(t, errStr, "third error", "Should contain third error")
}

// TestErrorCollectorIntegration tests the full flow of error collection
func TestErrorCollectorIntegration(t *testing.T) {
	tests := []struct {
		name          string
		failFast      bool
		errors        []error
		expectError   bool
		expectCount   int
		shouldContain []string
	}{
		{
			name:        "no errors collected",
			failFast:    false,
			errors:      []error{},
			expectError: false,
			expectCount: 0,
		},
		{
			name:          "single error aggregated",
			failFast:      false,
			errors:        []error{fmt.Errorf("error 1")},
			expectError:   true,
			expectCount:   1,
			shouldContain: []string{"error 1"},
		},
		{
			name:          "multiple errors aggregated",
			failFast:      false,
			errors:        []error{fmt.Errorf("error 1"), fmt.Errorf("error 2"), fmt.Errorf("error 3")},
			expectError:   true,
			expectCount:   3,
			shouldContain: []string{"error 1", "error 2", "error 3"},
		},
		{
			name:          "fail-fast stops at first error",
			failFast:      true,
			errors:        []error{fmt.Errorf("error 1"), fmt.Errorf("error 2")},
			expectError:   true,
			expectCount:   0, // No errors collected in fail-fast mode
			shouldContain: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewErrorCollector(tt.failFast)

			// Add all errors
			for _, err := range tt.errors {
				result := collector.Add(err)
				if tt.failFast && err != nil {
					// In fail-fast mode, Add should return error immediately
					assert.Error(t, result, "Should return error in fail-fast mode")
					return // Stop test here for fail-fast mode
				}
			}

			// Get the aggregated error
			err := collector.Error()

			if tt.expectError {
				require.Error(t, err, "Should have aggregated error")
				errStr := err.Error()

				for _, expected := range tt.shouldContain {
					assert.Contains(t, errStr, expected, "Should contain expected error message")
				}
			} else {
				require.NoError(t, err, "Should not have error")
			}

			assert.Equal(t, tt.expectCount, collector.Count(), "Error count should match")
		})
	}
}

// TestErrorCollectorFormattedError tests the FormattedError method
func TestErrorCollectorFormattedError(t *testing.T) {
	tests := []struct {
		name          string
		errors        []error
		category      string
		expectError   bool
		shouldContain []string
	}{
		{
			name:        "no errors",
			errors:      []error{},
			category:    "validation",
			expectError: false,
		},
		{
			name:          "single error (no formatting)",
			errors:        []error{fmt.Errorf("single error")},
			category:      "validation",
			expectError:   true,
			shouldContain: []string{"single error"},
		},
		{
			name:        "multiple errors with formatted header",
			errors:      []error{fmt.Errorf("error 1"), fmt.Errorf("error 2"), fmt.Errorf("error 3")},
			category:    "validation",
			expectError: true,
			shouldContain: []string{
				"Found 3 validation errors:",
				"error 1",
				"error 2",
				"error 3",
			},
		},
		{
			name:        "errors with newlines preserved",
			errors:      []error{fmt.Errorf("error with\nmultiple\nlines"), fmt.Errorf("simple error")},
			category:    "test",
			expectError: true,
			shouldContain: []string{
				"Found 2 test errors:",
				"error with",
				"multiple",
				"lines",
				"simple error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewErrorCollector(false)

			// Add all errors
			for _, err := range tt.errors {
				_ = collector.Add(err)
			}

			// Get the formatted error
			err := collector.FormattedError(tt.category)

			if tt.expectError {
				require.Error(t, err, "Should have formatted error")
				errStr := err.Error()

				for _, expected := range tt.shouldContain {
					assert.Contains(t, errStr, expected, "Should contain expected text")
				}
			} else {
				require.NoError(t, err, "Should not have error")
			}
		})
	}
}
