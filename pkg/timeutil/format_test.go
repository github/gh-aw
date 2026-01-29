//go:build !integration

package timeutil

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		// Nanosecond range
		{
			name:     "nanoseconds",
			duration: 500 * time.Nanosecond,
			expected: "500ns",
		},
		{
			name:     "999 nanoseconds",
			duration: 999 * time.Nanosecond,
			expected: "999ns",
		},
		// Microsecond range
		{
			name:     "microseconds",
			duration: 500 * time.Microsecond,
			expected: "500µs",
		},
		{
			name:     "999 microseconds",
			duration: 999 * time.Microsecond,
			expected: "999µs",
		},
		// Millisecond range
		{
			name:     "milliseconds",
			duration: 250 * time.Millisecond,
			expected: "250ms",
		},
		{
			name:     "999 milliseconds",
			duration: 999 * time.Millisecond,
			expected: "999ms",
		},
		// Second range
		{
			name:     "seconds",
			duration: 5 * time.Second,
			expected: "5.0s",
		},
		{
			name:     "seconds with decimal",
			duration: 5500 * time.Millisecond,
			expected: "5.5s",
		},
		{
			name:     "59 seconds",
			duration: 59 * time.Second,
			expected: "59.0s",
		},
		// Minute range
		{
			name:     "1 minute",
			duration: time.Minute,
			expected: "1.0m",
		},
		{
			name:     "minutes with decimal",
			duration: 90 * time.Second,
			expected: "1.5m",
		},
		{
			name:     "59 minutes",
			duration: 59 * time.Minute,
			expected: "59.0m",
		},
		// Hour range
		{
			name:     "1 hour",
			duration: time.Hour,
			expected: "1.0h",
		},
		{
			name:     "hours with decimal",
			duration: 90 * time.Minute,
			expected: "1.5h",
		},
		{
			name:     "multiple hours",
			duration: 5*time.Hour + 30*time.Minute,
			expected: "5.5h",
		},
		// Edge cases
		{
			name:     "zero duration",
			duration: 0,
			expected: "0ns",
		},
		{
			name:     "1 nanosecond",
			duration: 1 * time.Nanosecond,
			expected: "1ns",
		},
		{
			name:     "just under microsecond",
			duration: 999 * time.Nanosecond,
			expected: "999ns",
		},
		{
			name:     "exactly 1 microsecond",
			duration: 1 * time.Microsecond,
			expected: "1µs",
		},
		{
			name:     "just under millisecond",
			duration: 999 * time.Microsecond,
			expected: "999µs",
		},
		{
			name:     "exactly 1 millisecond",
			duration: 1 * time.Millisecond,
			expected: "1ms",
		},
		{
			name:     "just under second",
			duration: 999 * time.Millisecond,
			expected: "999ms",
		},
		{
			name:     "exactly 1 second",
			duration: 1 * time.Second,
			expected: "1.0s",
		},
		{
			name:     "just under minute",
			duration: 59*time.Second + 999*time.Millisecond,
			expected: "60.0s",
		},
		{
			name:     "exactly 1 minute",
			duration: 1 * time.Minute,
			expected: "1.0m",
		},
		{
			name:     "just under hour",
			duration: 59*time.Minute + 59*time.Second,
			expected: "60.0m",
		},
		{
			name:     "exactly 1 hour",
			duration: 1 * time.Hour,
			expected: "1.0h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}
