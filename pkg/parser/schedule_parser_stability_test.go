//go:build !integration

package parser

import (
	"testing"
)

// TestStableHashCrossPlatform verifies that the hash function produces
// consistent results across different platforms, operating systems, and architectures.
// This test uses known input/output pairs to ensure stability.
func TestStableHashCrossPlatform(t *testing.T) {
	tests := []struct {
		input    string
		modulo   int
		expected int
	}{
		// Test cases with known outputs from FNV-1a hash
		{"workflow-a.md", 1440, 1221},
		{"workflow-b.md", 1440, 1282},
		{"repo/workflow-a.md", 1440, 56},
		{"test/workflow.md", 1440, 554},
		{"workflow-a.md", 60, 21},
		{"workflow-b.md", 60, 22},
		{"workflow-a.md", 120, 21},
		{"workflow-b.md", 120, 82},
		{"workflow-a.md", 10080, 4101},
		{"workflow-b.md", 10080, 2722},
		// Additional test cases to ensure stability
		{"daily-workflow", 1440, 380},
		{"weekly-workflow", 1440, 492},
		{"hourly-workflow", 60, 42},
		{"my-workflow.md", 1440, 70},
		{"test-repo/my-workflow.md", 1440, 1082},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := stableHash(tt.input, tt.modulo)
			if result != tt.expected {
				t.Errorf("stableHash(%q, %d) = %d, want %d", tt.input, tt.modulo, result, tt.expected)
			}
		})
	}
}

// TestScatterScheduleCrossPlatformConsistency verifies that schedule scattering
// produces consistent results for known workflow identifiers.
func TestScatterScheduleCrossPlatformConsistency(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		expectedCron       string
	}{
		{
			name:               "daily - workflow-a.md",
			fuzzyCron:          "FUZZY:DAILY * * *",
			workflowIdentifier: "workflow-a.md",
			expectedCron:       "21 20 * * *",
		},
		{
			name:               "daily - workflow-b.md",
			fuzzyCron:          "FUZZY:DAILY * * *",
			workflowIdentifier: "workflow-b.md",
			expectedCron:       "22 21 * * *",
		},
		{
			name:               "hourly/1 - workflow-a.md",
			fuzzyCron:          "FUZZY:HOURLY/1 * * *",
			workflowIdentifier: "workflow-a.md",
			expectedCron:       "21 */1 * * *",
		},
		{
			name:               "hourly/2 - workflow-b.md",
			fuzzyCron:          "FUZZY:HOURLY/2 * * *",
			workflowIdentifier: "workflow-b.md",
			expectedCron:       "22 */2 * * *",
		},
		{
			name:               "weekly - workflow-a.md",
			fuzzyCron:          "FUZZY:WEEKLY * * *",
			workflowIdentifier: "workflow-a.md",
			expectedCron:       "21 20 * * 2",
		},
		{
			name:               "weekly:1 - workflow-a.md",
			fuzzyCron:          "FUZZY:WEEKLY:1 * * *",
			workflowIdentifier: "workflow-a.md",
			expectedCron:       "21 20 * * 1",
		},
		{
			name:               "daily around 14:00 - workflow-a.md",
			fuzzyCron:          "FUZZY:DAILY_AROUND:14:0 * * *",
			workflowIdentifier: "workflow-a.md",
			expectedCron:       "21 13 * * *",
		},
		{
			name:               "weekly around monday 9:00 - workflow-a.md",
			fuzzyCron:          "FUZZY:WEEKLY_AROUND:1:9:0 * * *",
			workflowIdentifier: "workflow-a.md",
			expectedCron:       "21 8 * * 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expectedCron {
				t.Errorf("ScatterSchedule(%q, %q) = %q, want %q",
					tt.fuzzyCron, tt.workflowIdentifier, result, tt.expectedCron)
			}
		})
	}
}

// TestStableHashProperties verifies important properties of the stable hash function
func TestStableHashProperties(t *testing.T) {
	t.Run("same input produces same output", func(t *testing.T) {
		input := "test-workflow.md"
		modulo := 1440
		result1 := stableHash(input, modulo)
		result2 := stableHash(input, modulo)
		if result1 != result2 {
			t.Errorf("same input produced different outputs: %d vs %d", result1, result2)
		}
	})

	t.Run("output is within modulo range", func(t *testing.T) {
		inputs := []string{"workflow-a", "workflow-b", "test", "my-workflow.md"}
		modulos := []int{60, 120, 1440, 10080}

		for _, input := range inputs {
			for _, modulo := range modulos {
				result := stableHash(input, modulo)
				if result < 0 || result >= modulo {
					t.Errorf("stableHash(%q, %d) = %d, which is out of range [0, %d)",
						input, modulo, result, modulo)
				}
			}
		}
	})

	t.Run("different inputs produce different outputs with high probability", func(t *testing.T) {
		inputs := []string{
			"workflow-a.md",
			"workflow-b.md",
			"workflow-c.md",
			"workflow-d.md",
			"workflow-e.md",
		}
		modulo := 1440
		results := make(map[int]bool)

		for _, input := range inputs {
			result := stableHash(input, modulo)
			if results[result] {
				// Collision is possible but should be rare
				t.Logf("Note: collision detected for input %q with result %d", input, result)
			}
			results[result] = true
		}

		// With 5 inputs and modulo 1440, we expect most to be unique
		if len(results) < 4 {
			t.Errorf("too many collisions: only %d unique values out of %d inputs",
				len(results), len(inputs))
		}
	})

	t.Run("empty string is handled", func(t *testing.T) {
		result := stableHash("", 1440)
		if result < 0 || result >= 1440 {
			t.Errorf("stableHash(\"\", 1440) = %d, which is out of range", result)
		}
	})

	t.Run("special characters are handled", func(t *testing.T) {
		inputs := []string{
			"workflow-with-dashes.md",
			"workflow_with_underscores.md",
			"workflow/with/slashes.md",
			"workflow.with.dots.md",
			"workflow with spaces.md",
		}

		for _, input := range inputs {
			result := stableHash(input, 1440)
			if result < 0 || result >= 1440 {
				t.Errorf("stableHash(%q, 1440) = %d, which is out of range", input, result)
			}
		}
	})
}
