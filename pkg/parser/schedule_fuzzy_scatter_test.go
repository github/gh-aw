//go:build !integration

package parser

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestScatterSchedule(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		expectError        bool
	}{
		{
			name:               "valid fuzzy daily",
			fuzzyCron:          "FUZZY:DAILY * * *",
			workflowIdentifier: "workflow1",
			expectError:        false,
		},
		{
			name:               "not a fuzzy cron",
			fuzzyCron:          "0 0 * * *",
			workflowIdentifier: "workflow1",
			expectError:        true,
		},
		{
			name:               "invalid fuzzy pattern",
			fuzzyCron:          "FUZZY:INVALID",
			workflowIdentifier: "workflow1",
			expectError:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
			// Check that result is daily pattern
			if !IsDailyCron(result) {
				t.Errorf("ScatterSchedule returned non-daily cron: %s", result)
			}
		})
	}
}

func TestScatterScheduleDeterministic(t *testing.T) {
	// Test that scattering is deterministic - same input produces same output
	workflows := []string{"workflow-a", "workflow-b", "workflow-c", "workflow-a"}

	results := make([]string, len(workflows))
	for i, wf := range workflows {
		result, err := ScatterSchedule("FUZZY:DAILY * * *", wf)
		if err != nil {
			t.Fatalf("unexpected error for workflow %s: %v", wf, err)
		}
		results[i] = result
	}

	// workflow-a should produce the same result both times
	if results[0] != results[3] {
		t.Errorf("ScatterSchedule not deterministic: workflow-a produced %s and %s", results[0], results[3])
	}

	// Different workflows should produce different results (with high probability)
	if results[0] == results[1] && results[1] == results[2] {
		t.Errorf("ScatterSchedule produced identical results for all workflows: %s", results[0])
	}
}

func TestFinalizeScatteredMinutes(t *testing.T) {
	tests := []struct {
		name            string
		scatteredMinute int
		wantHour        int
		wantMinute      int
	}{
		{name: "unchanged minute", scatteredMinute: 12*60 + 22, wantHour: 12, wantMinute: 22},
		{name: "hour boundary", scatteredMinute: 7*60 + 2, wantHour: 7, wantMinute: 7},
		{name: "EU peak minute", scatteredMinute: 7*60 + 30, wantHour: 7, wantMinute: 34},
		{name: "US peak minute", scatteredMinute: 15*60 + 15, wantHour: 15, wantMinute: 19},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHour, gotMinute := finalizeScatteredMinutes(tt.scatteredMinute)
			if gotHour != tt.wantHour || gotMinute != tt.wantMinute {
				t.Errorf("finalizeScatteredMinutes(%d) = (%d, %d), want (%d, %d)", tt.scatteredMinute, gotHour, gotMinute, tt.wantHour, tt.wantMinute)
			}
		})
	}
}

func TestScatterScheduleHourly(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		expectError        bool
	}{
		{
			name:               "valid fuzzy hourly 1h",
			fuzzyCron:          "FUZZY:HOURLY/1 * * *",
			workflowIdentifier: "workflow1",
			expectError:        false,
		},
		{
			name:               "valid fuzzy hourly 6h",
			fuzzyCron:          "FUZZY:HOURLY/6 * * *",
			workflowIdentifier: "workflow2",
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
			// Check that result has an hourly interval pattern
			fields := strings.Fields(result)
			if len(fields) != 5 {
				t.Errorf("expected 5 fields in cron, got %d: %s", len(fields), result)
			}
			if !strings.HasPrefix(fields[1], "*/") {
				t.Errorf("expected hourly interval pattern in hour field, got: %s", result)
			}
		})
	}
}

func TestScatterScheduleDailyAround(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		targetHour         int
		targetMinute       int
		expectError        bool
	}{
		{
			name:               "valid fuzzy daily around 9am",
			fuzzyCron:          "FUZZY:DAILY_AROUND:9:0 * * *",
			workflowIdentifier: "workflow1",
			targetHour:         9,
			targetMinute:       0,
			expectError:        false,
		},
		{
			name:               "valid fuzzy daily around 14:30",
			fuzzyCron:          "FUZZY:DAILY_AROUND:14:30 * * *",
			workflowIdentifier: "workflow2",
			targetHour:         14,
			targetMinute:       30,
			expectError:        false,
		},
		{
			name:               "valid fuzzy daily around midnight",
			fuzzyCron:          "FUZZY:DAILY_AROUND:0:0 * * *",
			workflowIdentifier: "workflow3",
			targetHour:         0,
			targetMinute:       0,
			expectError:        false,
		},
		{
			name:               "valid fuzzy daily around 23:30",
			fuzzyCron:          "FUZZY:DAILY_AROUND:23:30 * * *",
			workflowIdentifier: "workflow4",
			targetHour:         23,
			targetMinute:       30,
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
			// Check that result is daily pattern
			if !IsDailyCron(result) {
				t.Errorf("ScatterSchedule returned non-daily cron: %s", result)
			}
		})
	}
}

func TestScatterScheduleDailyAroundDeterministic(t *testing.T) {
	// Test that scattering is deterministic - same input produces same output
	workflows := []string{"workflow-a", "workflow-b", "workflow-c", "workflow-a"}

	results := make([]string, len(workflows))
	for i, wf := range workflows {
		result, err := ScatterSchedule("FUZZY:DAILY_AROUND:14:0 * * *", wf)
		if err != nil {
			t.Fatalf("unexpected error for workflow %s: %v", wf, err)
		}
		results[i] = result
	}

	// workflow-a should produce the same result both times
	if results[0] != results[3] {
		t.Errorf("ScatterSchedule not deterministic: workflow-a produced %s and %s", results[0], results[3])
	}

	// Different workflows should produce different results (with high probability)
	if results[0] == results[1] && results[1] == results[2] {
		t.Errorf("ScatterSchedule produced identical results for all workflows: %s", results[0])
	}
}

func TestScatterScheduleWeekly(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		expectError        bool
	}{
		{
			name:               "valid fuzzy weekly",
			fuzzyCron:          "FUZZY:WEEKLY * * *",
			workflowIdentifier: "workflow1",
			expectError:        false,
		},
		{
			name:               "valid fuzzy weekly on monday",
			fuzzyCron:          "FUZZY:WEEKLY:1 * * *",
			workflowIdentifier: "workflow2",
			expectError:        false,
		},
		{
			name:               "valid fuzzy weekly on friday",
			fuzzyCron:          "FUZZY:WEEKLY:5 * * *",
			workflowIdentifier: "workflow3",
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
			// Check that result has a weekly pattern
			fields := strings.Fields(result)
			if len(fields) != 5 {
				t.Errorf("expected 5 fields in cron, got %d: %s", len(fields), result)
			}
			// Check that day-of-month and month are wildcards
			if fields[2] != "*" || fields[3] != "*" {
				t.Errorf("expected wildcards in day-of-month and month, got: %s", result)
			}
		})
	}
}

func TestScatterScheduleWeeklyDeterministic(t *testing.T) {
	// Test that scattering is deterministic - same input produces same output
	workflows := []string{"workflow-a", "workflow-b", "workflow-c", "workflow-a"}

	results := make([]string, len(workflows))
	for i, wf := range workflows {
		result, err := ScatterSchedule("FUZZY:WEEKLY * * *", wf)
		if err != nil {
			t.Fatalf("unexpected error for workflow %s: %v", wf, err)
		}
		results[i] = result
	}

	// workflow-a should produce the same result both times
	if results[0] != results[3] {
		t.Errorf("ScatterSchedule not deterministic: workflow-a produced %s and %s", results[0], results[3])
	}

	// Different workflows should produce different results (with high probability)
	if results[0] == results[1] && results[1] == results[2] {
		t.Errorf("ScatterSchedule produced identical results for all workflows: %s", results[0])
	}
}

func TestScatterScheduleWeeklyOnDayDeterministic(t *testing.T) {
	// Test that scattering for specific day is deterministic
	workflows := []string{"workflow-a", "workflow-b", "workflow-c", "workflow-a"}

	results := make([]string, len(workflows))
	for i, wf := range workflows {
		result, err := ScatterSchedule("FUZZY:WEEKLY:1 * * *", wf)
		if err != nil {
			t.Fatalf("unexpected error for workflow %s: %v", wf, err)
		}
		results[i] = result
	}

	// workflow-a should produce the same result both times
	if results[0] != results[3] {
		t.Errorf("ScatterSchedule not deterministic: workflow-a produced %s and %s", results[0], results[3])
	}

	// All results should have day-of-week = 1 (Monday)
	for i, result := range results {
		fields := strings.Fields(result)
		if len(fields) != 5 || fields[4] != "1" {
			t.Errorf("workflow %d: expected day-of-week=1 (Monday), got: %s", i, result)
		}
	}

	// Different workflows should produce different times (with high probability)
	time0 := strings.Fields(results[0])[:2]
	time1 := strings.Fields(results[1])[:2]
	time2 := strings.Fields(results[2])[:2]

	time0Str := strings.Join(time0, ":")
	time1Str := strings.Join(time1, ":")
	time2Str := strings.Join(time2, ":")

	if time0Str == time1Str && time1Str == time2Str {
		t.Errorf("ScatterSchedule produced identical times for all workflows: %s", time0Str)
	}
}

func TestScatterScheduleWeeklyAround(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		targetWeekday      string
		targetHour         int
		targetMinute       int
		expectError        bool
	}{
		{
			name:               "valid fuzzy weekly around monday 9am",
			fuzzyCron:          "FUZZY:WEEKLY_AROUND:1:9:0 * * *",
			workflowIdentifier: "workflow1",
			targetWeekday:      "1",
			targetHour:         9,
			targetMinute:       0,
			expectError:        false,
		},
		{
			name:               "valid fuzzy weekly around friday 17:00",
			fuzzyCron:          "FUZZY:WEEKLY_AROUND:5:17:0 * * *",
			workflowIdentifier: "workflow2",
			targetWeekday:      "5",
			targetHour:         17,
			targetMinute:       0,
			expectError:        false,
		},
		{
			name:               "valid fuzzy weekly around sunday midnight",
			fuzzyCron:          "FUZZY:WEEKLY_AROUND:0:0:0 * * *",
			workflowIdentifier: "workflow3",
			targetWeekday:      "0",
			targetHour:         0,
			targetMinute:       0,
			expectError:        false,
		},
		{
			name:               "valid fuzzy weekly around wednesday 14:30",
			fuzzyCron:          "FUZZY:WEEKLY_AROUND:3:14:30 * * *",
			workflowIdentifier: "workflow4",
			targetWeekday:      "3",
			targetHour:         14,
			targetMinute:       30,
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
			// Verify weekday is preserved
			fields := strings.Fields(result)
			if len(fields) != 5 || fields[4] != tt.targetWeekday {
				t.Errorf("expected day-of-week=%s, got: %s", tt.targetWeekday, result)
			}
		})
	}
}

func TestScatterScheduleWeeklyAroundDeterministic(t *testing.T) {
	// Test that scattering is deterministic - same input produces same output
	workflows := []string{"workflow-a", "workflow-b", "workflow-c", "workflow-a"}

	results := make([]string, len(workflows))
	for i, wf := range workflows {
		result, err := ScatterSchedule("FUZZY:WEEKLY_AROUND:1:14:0 * * *", wf)
		if err != nil {
			t.Fatalf("unexpected error for workflow %s: %v", wf, err)
		}
		results[i] = result
	}

	// workflow-a should produce the same result both times
	if results[0] != results[3] {
		t.Errorf("ScatterSchedule not deterministic: workflow-a produced %s and %s", results[0], results[3])
	}

	// All results should have day-of-week = 1 (Monday)
	for i, result := range results {
		fields := strings.Fields(result)
		if len(fields) != 5 || fields[4] != "1" {
			t.Errorf("workflow %d: expected day-of-week=1 (Monday), got: %s", i, result)
		}
	}

	// Different workflows should produce different results (with high probability)
	if results[0] == results[1] && results[1] == results[2] {
		t.Errorf("ScatterSchedule produced identical results for all workflows: %s", results[0])
	}
}

func TestScatterScheduleBiWeekly(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		expectError        bool
	}{
		{
			name:               "bi-weekly fuzzy",
			fuzzyCron:          "FUZZY:BI_WEEKLY * * *",
			workflowIdentifier: "test-workflow",
			expectError:        false,
		},
		{
			name:               "bi-weekly with different workflow",
			fuzzyCron:          "FUZZY:BI_WEEKLY * * *",
			workflowIdentifier: "another-workflow",
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
		})
	}
}

func TestScatterScheduleTriWeekly(t *testing.T) {
	tests := []struct {
		name               string
		fuzzyCron          string
		workflowIdentifier string
		expectError        bool
	}{
		{
			name:               "tri-weekly fuzzy",
			fuzzyCron:          "FUZZY:TRI_WEEKLY * * *",
			workflowIdentifier: "test-workflow",
			expectError:        false,
		},
		{
			name:               "tri-weekly with different workflow",
			fuzzyCron:          "FUZZY:TRI_WEEKLY * * *",
			workflowIdentifier: "another-workflow",
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, tt.workflowIdentifier)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// Check that result is a valid cron expression
			if !IsCronExpression(result) {
				t.Errorf("ScatterSchedule returned invalid cron: %s", result)
			}
		})
	}
}

func TestStableHash(t *testing.T) {
	// Test that hash is deterministic
	s := "test-workflow"
	hash1 := stableHash(s, 100)
	hash2 := stableHash(s, 100)

	if hash1 != hash2 {
		t.Errorf("stableHash not deterministic: got %d and %d", hash1, hash2)
	}

	// Test that hash is within range
	if hash1 < 0 || hash1 >= 100 {
		t.Errorf("stableHash out of range: got %d, want [0, 100)", hash1)
	}

	// Test different strings produce different hashes (with high probability)
	hash3 := stableHash("different-workflow", 100)
	if hash1 == hash3 {
		t.Logf("Warning: different strings produced same hash (rare but possible)")
	}

	// Test zero modulo returns 0 (guard against divide-by-zero)
	if got := stableHash("any", 0); got != 0 {
		t.Errorf("stableHash(_, 0) = %d, want 0", got)
	}

	// Test negative modulo returns 0 safely
	if got := stableHash("any", -1); got != 0 {
		t.Errorf("stableHash(_, -1) = %d, want 0", got)
	}

	// Test modulo larger than math.MaxUint32 to guard against truncation regression
	const largeModulo = math.MaxUint32 + 1 // 4_294_967_296
	hashLarge := stableHash("test-workflow", largeModulo)
	if hashLarge < 0 || hashLarge >= largeModulo {
		t.Errorf("stableHash out of range for large modulo: got %d, want [0, %d)", hashLarge, largeModulo)
	}
}

func TestScatterScheduleWeekdays(t *testing.T) {
	workflowID := "test/repo/workflow.md"

	tests := []struct {
		name           string
		fuzzyCron      string
		expectedSuffix string // Expected day-of-week suffix
	}{
		{
			name:           "FUZZY:DAILY_WEEKDAYS",
			fuzzyCron:      "FUZZY:DAILY_WEEKDAYS * * *",
			expectedSuffix: " 1-5",
		},
		{
			name:           "FUZZY:HOURLY_WEEKDAYS/1",
			fuzzyCron:      "FUZZY:HOURLY_WEEKDAYS/1 * * *",
			expectedSuffix: " 1-5",
		},
		{
			name:           "FUZZY:HOURLY_WEEKDAYS/2",
			fuzzyCron:      "FUZZY:HOURLY_WEEKDAYS/2 * * *",
			expectedSuffix: " 1-5",
		},
		{
			name:           "FUZZY:DAILY_AROUND_WEEKDAYS",
			fuzzyCron:      "FUZZY:DAILY_AROUND_WEEKDAYS:9:0 * * *",
			expectedSuffix: " 1-5",
		},
		{
			name:           "FUZZY:DAILY_BETWEEN_WEEKDAYS",
			fuzzyCron:      "FUZZY:DAILY_BETWEEN_WEEKDAYS:9:0:17:0 * * *",
			expectedSuffix: " 1-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ScatterSchedule(tt.fuzzyCron, workflowID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check that the result ends with the expected suffix (day-of-week)
			if !strings.HasSuffix(result, tt.expectedSuffix) {
				t.Errorf("expected result to end with '%s', got '%s'", tt.expectedSuffix, result)
			}

			// Validate it's a valid cron expression with 5 fields
			fields := strings.Fields(result)
			if len(fields) != 5 {
				t.Errorf("expected 5 cron fields, got %d: %s", len(fields), result)
			}

			// Verify the last field is the weekday range 1-5
			if fields[4] != "1-5" {
				t.Errorf("expected day-of-week field to be '1-5', got '%s'", fields[4])
			}
		})
	}
}

// TestScatterScheduleAvoidsHourBoundary verifies that all fuzzy schedule patterns
// produce a minute value in [5, 54], never within 5 minutes of an hour boundary.
// This avoids peak usage times on GitHub Actions (especially 00:00 UTC and
// the 5-minute windows before/after each hour).
func TestScatterScheduleAvoidsHourBoundary(t *testing.T) {
	// Use a diverse set of workflow identifiers to exercise different hash outcomes.
	workflowIDs := []string{
		"workflow-a.md",
		"workflow-b.md",
		"repo/workflow-c.md",
		"test-workflow",
		"daily-security-scan",
		"weekly-report",
		"hourly-checker",
		"my-org/my-repo/my-workflow.md",
	}

	patterns := []string{
		"FUZZY:DAILY * * *",
		"FUZZY:DAILY_WEEKDAYS * * *",
		"FUZZY:HOURLY/1 * * *",
		"FUZZY:HOURLY/2 * * *",
		"FUZZY:HOURLY/6 * * *",
		"FUZZY:HOURLY_WEEKDAYS/1 * * *",
		"FUZZY:HOURLY_WEEKDAYS/4 * * *",
		"FUZZY:DAILY_AROUND:9:0 * * *",
		"FUZZY:DAILY_AROUND:0:0 * * *",
		"FUZZY:DAILY_AROUND:23:30 * * *",
		"FUZZY:DAILY_AROUND_WEEKDAYS:14:0 * * *",
		"FUZZY:DAILY_BETWEEN:9:0:17:0 * * *",
		"FUZZY:DAILY_BETWEEN:22:0:2:0 * * *",
		"FUZZY:DAILY_BETWEEN_WEEKDAYS:8:30:18:0 * * *",
		"FUZZY:WEEKLY * * *",
		"FUZZY:WEEKLY:1 * * *",
		"FUZZY:WEEKLY:5 * * *",
		"FUZZY:WEEKLY_AROUND:1:9:0 * * *",
		"FUZZY:WEEKLY_AROUND:0:0:0 * * *",
		"FUZZY:BI_WEEKLY * * *",
		"FUZZY:TRI_WEEKLY * * *",
	}

	for _, pattern := range patterns {
		for _, wfID := range workflowIDs {
			t.Run(pattern+"/"+wfID, func(t *testing.T) {
				result, err := ScatterSchedule(pattern, wfID)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				fields := strings.Fields(result)
				if len(fields) != 5 {
					t.Fatalf("expected 5 cron fields, got %d: %s", len(fields), result)
				}

				// The minute field is the first field in a cron expression.
				// Parse the raw minute number.
				minuteStr := fields[0]
				var minute int
				if _, scanErr := fmt.Sscanf(minuteStr, "%d", &minute); scanErr != nil {
					t.Fatalf("could not parse minute from cron %q: %v", result, scanErr)
				}

				if minute < 5 || minute > 54 {
					t.Errorf("pattern=%q wfID=%q: minute %d is within 5 minutes of an hour boundary (must be in [5, 54]); cron=%q",
						pattern, wfID, minute, result)
				}
			})
		}
	}
}

// TestScatterScheduleAvoidsEUMorningPeak verifies that targeted-scatter patterns
// never produce a minute within 3 of :30 (i.e. minutes 27–33) during EU morning
// peak hours (06:00–09:59 UTC).
func TestScatterScheduleAvoidsEUMorningPeak(t *testing.T) {
	workflowIDs := []string{
		"workflow-a.md", "workflow-b.md", "workflow-c.md",
		"test-workflow", "daily-security-scan", "weekly-report",
		"my-org/my-repo/my-workflow.md", "scanner-job",
	}

	// Targeted patterns that scatter around EU morning peak hours.
	patterns := []string{
		"FUZZY:DAILY_AROUND:7:0 * * *",
		"FUZZY:DAILY_AROUND:7:30 * * *",
		"FUZZY:DAILY_AROUND:8:0 * * *",
		"FUZZY:DAILY_AROUND:9:0 * * *",
		"FUZZY:DAILY_AROUND_WEEKDAYS:7:0 * * *",
		"FUZZY:DAILY_AROUND_WEEKDAYS:8:30 * * *",
		"FUZZY:DAILY_BETWEEN:6:0:10:0 * * *",
		"FUZZY:DAILY_BETWEEN_WEEKDAYS:6:0:10:0 * * *",
		"FUZZY:WEEKLY_AROUND:1:7:0 * * *",
		"FUZZY:WEEKLY_AROUND:3:8:30 * * *",
	}

	for _, pattern := range patterns {
		for _, wfID := range workflowIDs {
			t.Run(pattern+"/"+wfID, func(t *testing.T) {
				result, err := ScatterSchedule(pattern, wfID)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				fields := strings.Fields(result)
				if len(fields) != 5 {
					t.Fatalf("expected 5 cron fields, got %d: %s", len(fields), result)
				}

				var minute, hour int
				if _, scanErr := fmt.Sscanf(fields[0], "%d", &minute); scanErr != nil {
					t.Fatalf("could not parse minute from cron %q: %v", result, scanErr)
				}
				if _, scanErr := fmt.Sscanf(fields[1], "%d", &hour); scanErr != nil {
					// Hour field may be a range or wildcard for some patterns – skip check.
					return
				}

				// Must stay 3 minutes away from :30 in hours 06-09
				if hour >= 6 && hour <= 9 && minute >= 27 && minute <= 33 {
					t.Errorf("pattern=%q wfID=%q: cron %q schedules at :%02d during EU morning peak (must stay 3 min from :30 in hours 06-09 UTC)",
						pattern, wfID, result, minute)
				}
			})
		}
	}
}

// TestScatterScheduleAvoidsUSBusinessHours verifies that targeted-scatter patterns
// never produce a minute within 3 of :15 or :45 (i.e. [12,18] or [42,48]) during
// US business hours (14:00–18:59 UTC).
func TestScatterScheduleAvoidsUSBusinessHours(t *testing.T) {
	workflowIDs := []string{
		"workflow-a.md", "workflow-b.md", "workflow-c.md",
		"test-workflow", "daily-security-scan", "weekly-report",
		"my-org/my-repo/my-workflow.md", "scanner-job",
	}

	// Targeted patterns that scatter around US business hours.
	patterns := []string{
		"FUZZY:DAILY_AROUND:14:0 * * *",
		"FUZZY:DAILY_AROUND:15:15 * * *",
		"FUZZY:DAILY_AROUND:16:45 * * *",
		"FUZZY:DAILY_AROUND:17:0 * * *",
		"FUZZY:DAILY_AROUND:18:0 * * *",
		"FUZZY:DAILY_AROUND_WEEKDAYS:15:0 * * *",
		"FUZZY:DAILY_AROUND_WEEKDAYS:16:45 * * *",
		"FUZZY:DAILY_BETWEEN:14:0:19:0 * * *",
		"FUZZY:DAILY_BETWEEN_WEEKDAYS:14:0:19:0 * * *",
		"FUZZY:WEEKLY_AROUND:2:15:15 * * *",
		"FUZZY:WEEKLY_AROUND:4:17:0 * * *",
	}

	for _, pattern := range patterns {
		for _, wfID := range workflowIDs {
			t.Run(pattern+"/"+wfID, func(t *testing.T) {
				result, err := ScatterSchedule(pattern, wfID)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				fields := strings.Fields(result)
				if len(fields) != 5 {
					t.Fatalf("expected 5 cron fields, got %d: %s", len(fields), result)
				}

				var minute, hour int
				if _, scanErr := fmt.Sscanf(fields[0], "%d", &minute); scanErr != nil {
					t.Fatalf("could not parse minute from cron %q: %v", result, scanErr)
				}
				if _, scanErr := fmt.Sscanf(fields[1], "%d", &hour); scanErr != nil {
					// Hour field may be a range or wildcard for some patterns – skip check.
					return
				}

				// Must stay 3 minutes away from :15 and :45 in hours 14-18
				if hour >= 14 && hour <= 18 {
					if minute >= 12 && minute <= 18 {
						t.Errorf("pattern=%q wfID=%q: cron %q schedules at :%02d during US business hours (must stay 3 min from :15 in hours 14-18 UTC)",
							pattern, wfID, result, minute)
					}
					if minute >= 42 && minute <= 48 {
						t.Errorf("pattern=%q wfID=%q: cron %q schedules at :%02d during US business hours (must stay 3 min from :45 in hours 14-18 UTC)",
							pattern, wfID, result, minute)
					}
				}
			})
		}
	}
}

// TestScatterScheduleUsesPreferredWindows verifies that full-day scatter patterns
// (FUZZY:DAILY, FUZZY:DAILY_WEEKDAYS, FUZZY:WEEKLY, etc.) land exclusively in the
// preferred time windows: BEST (02–05 UTC) or BROAD (06–23 UTC).
func TestScatterScheduleUsesPreferredWindows(t *testing.T) {
	workflowIDs := []string{
		"workflow-a.md", "workflow-b.md", "workflow-c.md",
		"test-workflow", "daily-security-scan", "weekly-report",
		"hourly-checker", "my-org/my-repo/my-workflow.md",
		"alpha", "beta", "gamma", "delta", "epsilon",
	}

	patterns := []string{
		"FUZZY:DAILY * * *",
		"FUZZY:DAILY_WEEKDAYS * * *",
		"FUZZY:WEEKLY * * *",
		"FUZZY:WEEKLY:1 * * *",
		"FUZZY:WEEKLY:5 * * *",
		"FUZZY:BI_WEEKLY * * *",
		"FUZZY:TRI_WEEKLY * * *",
	}

	isInPreferredWindow := func(hour int) bool {
		// BEST (02–05 UTC) or BROAD (06–23 UTC) — hours 00–01 are excluded
		return hour >= 2 && hour <= 23
	}

	for _, pattern := range patterns {
		for _, wfID := range workflowIDs {
			t.Run(pattern+"/"+wfID, func(t *testing.T) {
				result, err := ScatterSchedule(pattern, wfID)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				fields := strings.Fields(result)
				if len(fields) != 5 {
					t.Fatalf("expected 5 cron fields, got %d: %s", len(fields), result)
				}

				var hour int
				if _, scanErr := fmt.Sscanf(fields[1], "%d", &hour); scanErr != nil {
					t.Fatalf("could not parse hour from cron %q: %v", result, scanErr)
				}

				if !isInPreferredWindow(hour) {
					t.Errorf("pattern=%q wfID=%q: cron %q schedules at hour %d, which is not in a preferred window (02-05 or 06-23 UTC)",
						pattern, wfID, result, hour)
				}
			})
		}
	}
}
