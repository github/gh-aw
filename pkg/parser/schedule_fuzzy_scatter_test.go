//go:build !integration

package parser

import (
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
}
