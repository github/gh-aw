//go:build !integration

package console

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgressBar(t *testing.T) {
	t.Run("creates progress bar successfully", func(t *testing.T) {
		bar := NewProgressBar(1024)

		require.NotNil(t, bar, "NewProgressBar should not return nil")
		assert.Equal(t, int64(1024), bar.total, "Total should be set correctly")
		assert.Equal(t, int64(0), bar.current, "Current should start at 0")
		require.NotNil(t, bar.progress, "Progress model should be initialized")
	})

	t.Run("creates progress bar with zero total", func(t *testing.T) {
		bar := NewProgressBar(0)

		require.NotNil(t, bar, "NewProgressBar should not return nil even with zero total")
		assert.Equal(t, int64(0), bar.total, "Total should be 0")
	})

	t.Run("creates progress bar with large total", func(t *testing.T) {
		largeSize := int64(10 * 1024 * 1024 * 1024) // 10GB
		bar := NewProgressBar(largeSize)

		require.NotNil(t, bar, "NewProgressBar should handle large sizes")
		assert.Equal(t, largeSize, bar.total, "Total should handle large values")
	})
}

func TestProgressBarUpdate(t *testing.T) {
	tests := []struct {
		name             string
		total            int64
		current          int64
		expectedInTTY    []string
		expectedInNonTTY []string
	}{
		{
			name:             "0% progress",
			total:            1024,
			current:          0,
			expectedInTTY:    []string{}, // Progress bar visual (can't easily test exact rendering)
			expectedInNonTTY: []string{"0%", "0B", "1.0KB"},
		},
		{
			name:             "50% progress",
			total:            1024,
			current:          512,
			expectedInTTY:    []string{}, // Progress bar visual
			expectedInNonTTY: []string{"50%", "512B", "1.0KB"},
		},
		{
			name:             "100% progress",
			total:            1024,
			current:          1024,
			expectedInTTY:    []string{}, // Progress bar visual
			expectedInNonTTY: []string{"100%", "1.0KB", "1.0KB"},
		},
		{
			name:             "large file progress",
			total:            1024 * 1024 * 1024, // 1GB
			current:          512 * 1024 * 1024,  // 512MB
			expectedInTTY:    []string{},
			expectedInNonTTY: []string{"50%", "512.0MB", "1.00GB"},
		},
		{
			name:             "zero total edge case",
			total:            0,
			current:          0,
			expectedInTTY:    []string{}, // Should show 100%
			expectedInNonTTY: []string{"100%", "0B", "0B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := NewProgressBar(tt.total)
			output := bar.Update(tt.current)

			assert.NotEmpty(t, output, "Update should return non-empty string")

			// In non-TTY mode, we can validate the text output
			// In TTY mode, the output contains ANSI codes which we can't easily validate
			// but we ensure it doesn't panic
			if !isTTY() {
				for _, expected := range tt.expectedInNonTTY {
					assert.Contains(t, output, expected, "Output should contain expected text in non-TTY mode")
				}
			}

			// Verify current is updated
			assert.Equal(t, tt.current, bar.current, "Current should be updated after Update()")
		})
	}
}

func TestProgressBarMultipleUpdates(t *testing.T) {
	bar := NewProgressBar(1000)

	// Simulate progressive updates
	updates := []int64{0, 250, 500, 750, 1000}
	for _, value := range updates {
		output := bar.Update(value)
		assert.NotEmpty(t, output, "Each update should produce output")
		assert.Equal(t, value, bar.current, "Current should track the latest update")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "zero bytes",
			bytes:    0,
			expected: "0B",
		},
		{
			name:     "bytes only",
			bytes:    512,
			expected: "512B",
		},
		{
			name:     "exactly 1KB",
			bytes:    1024,
			expected: "1.0KB",
		},
		{
			name:     "kilobytes",
			bytes:    1536, // 1.5KB
			expected: "1.5KB",
		},
		{
			name:     "exactly 1MB",
			bytes:    1024 * 1024,
			expected: "1.0MB",
		},
		{
			name:     "megabytes",
			bytes:    1024 * 1024 * 5, // 5MB
			expected: "5.0MB",
		},
		{
			name:     "megabytes with decimal",
			bytes:    1024*1024*5 + 512*1024, // 5.5MB
			expected: "5.5MB",
		},
		{
			name:     "exactly 1GB",
			bytes:    1024 * 1024 * 1024,
			expected: "1.00GB",
		},
		{
			name:     "gigabytes",
			bytes:    1024 * 1024 * 1024 * 10, // 10GB
			expected: "10.00GB",
		},
		{
			name:     "gigabytes with decimal",
			bytes:    1024*1024*1024*2 + 512*1024*1024, // 2.5GB
			expected: "2.50GB",
		},
		{
			name:     "large file size",
			bytes:    1024 * 1024 * 1024 * 100, // 100GB
			expected: "100.00GB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result, "formatBytes should format bytes correctly")
		})
	}
}

func TestProgressBarPercentageCalculation(t *testing.T) {
	tests := []struct {
		name            string
		total           int64
		current         int64
		expectedPercent int
	}{
		{
			name:            "0% progress",
			total:           1000,
			current:         0,
			expectedPercent: 0,
		},
		{
			name:            "25% progress",
			total:           1000,
			current:         250,
			expectedPercent: 25,
		},
		{
			name:            "50% progress",
			total:           1000,
			current:         500,
			expectedPercent: 50,
		},
		{
			name:            "75% progress",
			total:           1000,
			current:         750,
			expectedPercent: 75,
		},
		{
			name:            "100% progress",
			total:           1000,
			current:         1000,
			expectedPercent: 100,
		},
		{
			name:            "33% progress (rounded down)",
			total:           1000,
			current:         333,
			expectedPercent: 33,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := NewProgressBar(tt.total)
			output := bar.Update(tt.current)

			// In non-TTY mode, check the percentage
			if !isTTY() {
				expectedStr := fmt.Sprintf("%d%%", tt.expectedPercent)
				assert.Contains(t, output, expectedStr, "Output should contain correct percentage")
			}
		})
	}
}

func TestProgressBarOutputFormat(t *testing.T) {
	t.Run("non-TTY format structure", func(t *testing.T) {
		// This test verifies the output format structure
		// Skip if running in TTY mode
		if isTTY() {
			t.Skip("Test requires non-TTY mode")
		}

		bar := NewProgressBar(1024 * 1024) // 1MB
		output := bar.Update(512 * 1024)   // 512KB

		// Should contain: percentage, current size, total size
		assert.Contains(t, output, "%", "Output should contain percentage symbol")
		assert.True(t, strings.Contains(output, "KB") || strings.Contains(output, "MB"), "Output should contain size units")
		assert.Contains(t, output, "/", "Output should contain separator between current and total")
	})
}

func TestProgressBarEdgeCases(t *testing.T) {
	t.Run("current exceeds total", func(t *testing.T) {
		bar := NewProgressBar(100)
		output := bar.Update(150)

		assert.NotEmpty(t, output, "Should handle current exceeding total gracefully")

		// In non-TTY mode, percentage will be > 100
		if !isTTY() {
			assert.Contains(t, output, "150%", "Should show percentage > 100")
		}
	})

	t.Run("negative current value", func(t *testing.T) {
		bar := NewProgressBar(100)
		output := bar.Update(-10)

		assert.NotEmpty(t, output, "Should handle negative values gracefully")
	})

	t.Run("very small progress increments", func(t *testing.T) {
		bar := NewProgressBar(1000000)

		// Update with very small increments
		for i := int64(0); i <= 10; i++ {
			output := bar.Update(i)
			assert.NotEmpty(t, output, "Should handle very small increments")
		}
	})
}

func TestProgressBarConcurrency(t *testing.T) {
	t.Run("multiple updates are safe", func(t *testing.T) {
		bar := NewProgressBar(1000)

		// Sequential updates should be safe
		for i := int64(0); i <= 1000; i += 100 {
			output := bar.Update(i)
			assert.NotEmpty(t, output, "Each update should produce output")
		}
	})
}

func TestProgressBarNonTTYFallback(t *testing.T) {
	// This test documents the expected behavior in non-TTY environments
	t.Run("non-TTY output is human readable", func(t *testing.T) {
		// Skip if running in TTY mode as we can't test the fallback
		if isTTY() {
			t.Skip("Test requires non-TTY mode to validate fallback behavior")
		}

		bar := NewProgressBar(1024 * 1024) // 1MB total
		output := bar.Update(512 * 1024)   // 512KB downloaded

		// Should produce something like: "50% (512.0KB/1.0MB)"
		assert.Contains(t, output, "50%", "Should show percentage")
		assert.Contains(t, output, "512.0KB", "Should show current size")
		assert.Contains(t, output, "1.0MB", "Should show total size")
		assert.Contains(t, output, "(", "Should have opening parenthesis")
		assert.Contains(t, output, ")", "Should have closing parenthesis")
		assert.Contains(t, output, "/", "Should have separator")
	})
}

func TestNewIndeterminateProgressBar(t *testing.T) {
	t.Run("creates indeterminate progress bar successfully", func(t *testing.T) {
		bar := NewIndeterminateProgressBar()

		require.NotNil(t, bar, "NewIndeterminateProgressBar should not return nil")
		assert.Equal(t, int64(0), bar.total, "Total should be 0 for indeterminate mode")
		assert.Equal(t, int64(0), bar.current, "Current should start at 0")
		assert.True(t, bar.indeterminate, "Indeterminate flag should be true")
		require.NotNil(t, bar.progress, "Progress model should be initialized")
	})
}

func TestIndeterminateProgressBarUpdate(t *testing.T) {
	t.Run("indeterminate mode with no data", func(t *testing.T) {
		bar := NewIndeterminateProgressBar()
		output := bar.Update(0)

		assert.NotEmpty(t, output, "Update should return non-empty string")

		// In non-TTY mode, should show "Processing..."
		if !isTTY() {
			assert.Equal(t, "Processing...", output, "Should show processing indicator")
		}
	})

	t.Run("indeterminate mode with current value", func(t *testing.T) {
		bar := NewIndeterminateProgressBar()
		output := bar.Update(1024 * 1024) // 1MB processed

		assert.NotEmpty(t, output, "Update should return non-empty string")

		// In non-TTY mode, should show current value
		if !isTTY() {
			assert.Contains(t, output, "Processing...", "Should show processing text")
			assert.Contains(t, output, "1.0MB", "Should show current size")
		}
	})

	t.Run("indeterminate mode multiple updates", func(t *testing.T) {
		bar := NewIndeterminateProgressBar()

		// Simulate progressive updates without known total
		updates := []int64{0, 512 * 1024, 1024 * 1024, 2 * 1024 * 1024}
		for _, value := range updates {
			output := bar.Update(value)
			assert.NotEmpty(t, output, "Each update should produce output")
			assert.Equal(t, value, bar.current, "Current should track the latest update")
		}
	})

	t.Run("indeterminate mode produces varying output", func(t *testing.T) {
		// Skip if not in TTY mode as the pulsing effect is only visible in TTY
		if !isTTY() {
			t.Skip("Test requires TTY mode to validate pulsing effect")
		}

		bar := NewIndeterminateProgressBar()

		// Update with different values to create pulse effect
		outputs := make([]string, 8)
		for i := range outputs {
			outputs[i] = bar.Update(int64(i * 100))
		}

		// In TTY mode, outputs should vary (pulsing effect)
		// We just verify they're all non-empty and at least some are different
		allSame := true
		for i := 1; i < len(outputs); i++ {
			if outputs[i] != outputs[0] {
				allSame = false
				break
			}
		}
		assert.False(t, allSame, "Indeterminate progress should produce varying visual output for pulsing effect")
	})
}

func TestProgressBarModeSelection(t *testing.T) {
	t.Run("determinate mode has total and not indeterminate", func(t *testing.T) {
		bar := NewProgressBar(1024)
		assert.Equal(t, int64(1024), bar.total, "Determinate mode should have total")
		assert.False(t, bar.indeterminate, "Determinate mode should not be indeterminate")
	})

	t.Run("indeterminate mode has no total and is indeterminate", func(t *testing.T) {
		bar := NewIndeterminateProgressBar()
		assert.Equal(t, int64(0), bar.total, "Indeterminate mode should have zero total")
		assert.True(t, bar.indeterminate, "Indeterminate mode should be indeterminate")
	})
}
