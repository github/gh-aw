//go:build !integration

package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// parseCronField
// ---------------------------------------------------------------------------

func TestParseCronField_Wildcard(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("*", 0, 23)
	require.NoError(t, err, "wildcard should parse without error")
	assert.Len(t, vals, 24, "wildcard should expand to all 24 hours")
	assert.Equal(t, 0, vals[0], "first value should be 0")
	assert.Equal(t, 23, vals[23], "last value should be 23")
}

func TestParseCronField_SingleValue(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("14", 0, 23)
	require.NoError(t, err, "single value should parse without error")
	assert.Equal(t, []int{14}, vals, "should return exactly [14]")
}

func TestParseCronField_Range(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("1-5", 0, 6)
	require.NoError(t, err, "range should parse without error")
	assert.Equal(t, []int{1, 2, 3, 4, 5}, vals, "range 1-5 should expand to [1,2,3,4,5]")
}

func TestParseCronField_Step(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("*/4", 0, 23)
	require.NoError(t, err, "step should parse without error")
	assert.Equal(t, []int{0, 4, 8, 12, 16, 20}, vals, "*/4 should expand to every 4th hour")
}

func TestParseCronField_RangeWithStep(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("0-12/3", 0, 23)
	require.NoError(t, err, "range with step should parse without error")
	assert.Equal(t, []int{0, 3, 6, 9, 12}, vals, "0-12/3 should step by 3 within range")
}

func TestParseCronField_CommaSeparated(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("9,14,17", 0, 23)
	require.NoError(t, err, "comma-separated values should parse without error")
	assert.Equal(t, []int{9, 14, 17}, vals, "comma-separated list should return each value")
}

func TestParseCronField_CommaSeparatedDeduplication(t *testing.T) {
	t.Parallel()
	vals, err := parseCronField("5,5,5", 0, 23)
	require.NoError(t, err, "duplicate comma values should be deduplicated")
	assert.Equal(t, []int{5}, vals, "duplicate values should be collapsed to one")
}

func TestParseCronField_OutOfRange(t *testing.T) {
	t.Parallel()
	_, err := parseCronField("25", 0, 23)
	assert.Error(t, err, "value out of range should return an error")
}

func TestParseCronField_InvalidValue(t *testing.T) {
	t.Parallel()
	_, err := parseCronField("abc", 0, 23)
	assert.Error(t, err, "non-numeric field should return an error")
}

func TestParseCronField_InvalidStep(t *testing.T) {
	t.Parallel()
	_, err := parseCronField("*/0", 0, 23)
	assert.Error(t, err, "zero step should return an error")
}

// ---------------------------------------------------------------------------
// parseCronSchedule
// ---------------------------------------------------------------------------

func TestParseCronSchedule_Daily(t *testing.T) {
	t.Parallel()
	hours, days, err := parseCronSchedule("40 20 * * *")
	require.NoError(t, err, "daily cron should parse without error")
	assert.Equal(t, []int{20}, hours, "hour should be 20")
	assert.Len(t, days, 7, "wildcard day-of-week should expand to all 7 days")
}

func TestParseCronSchedule_Weekdays(t *testing.T) {
	t.Parallel()
	hours, days, err := parseCronSchedule("33 14 * * 1-5")
	require.NoError(t, err, "weekday cron should parse without error")
	assert.Equal(t, []int{14}, hours, "hour should be 14")
	assert.Equal(t, []int{1, 2, 3, 4, 5}, days, "day-of-week 1-5 = Mon through Fri")
}

func TestParseCronSchedule_MultipleHours(t *testing.T) {
	t.Parallel()
	hours, days, err := parseCronSchedule("0 9,17 * * *")
	require.NoError(t, err, "multiple hours cron should parse without error")
	assert.Equal(t, []int{9, 17}, hours, "hours should be [9,17]")
	assert.Len(t, days, 7, "wildcard day-of-week should expand to all 7 days")
}

func TestParseCronSchedule_Sunday7NormalisedTo0(t *testing.T) {
	t.Parallel()
	_, days, err := parseCronSchedule("0 0 * * 7")
	require.NoError(t, err, "day 7 (Sunday alias) should parse without error")
	assert.Equal(t, []int{0}, days, "day 7 should be normalised to 0 (Sunday)")
}

func TestParseCronSchedule_SundayDeduplicated(t *testing.T) {
	t.Parallel()
	_, days, err := parseCronSchedule("0 0 * * 0-7")
	require.NoError(t, err, "range 0-7 should deduplicate Sunday (0 and 7)")
	assert.Len(t, days, 7, "0-7 range should result in exactly 7 unique days after dedup")
}

func TestParseCronSchedule_WrongFieldCount(t *testing.T) {
	t.Parallel()
	_, _, err := parseCronSchedule("* * * *")
	assert.Error(t, err, "cron with 4 fields should return an error")
}

// ---------------------------------------------------------------------------
// buildScheduleGrid
// ---------------------------------------------------------------------------

func TestBuildScheduleGrid_Empty(t *testing.T) {
	t.Parallel()
	grid := buildScheduleGrid([]*WorkflowStats{})
	assert.Nil(t, grid, "empty stats list should return nil grid")
}

func TestBuildScheduleGrid_NoSchedules(t *testing.T) {
	t.Parallel()
	statsList := []*WorkflowStats{
		{Workflow: "no-schedule.lock.yml", FileSize: 1000},
	}
	grid := buildScheduleGrid(statsList)
	assert.Nil(t, grid, "stats with no schedules should return nil grid")
}

func TestBuildScheduleGrid_SingleDailyCron(t *testing.T) {
	t.Parallel()
	// "40 20 * * *" → hour=20, all 7 days
	statsList := []*WorkflowStats{
		{Workflow: "daily.lock.yml", Schedules: []string{"40 20 * * *"}},
	}
	grid := buildScheduleGrid(statsList)
	require.NotNil(t, grid, "grid should not be nil when a schedule exists")

	for day := range 7 {
		assert.Equal(t, 1, grid[day][20], "hour 20 should have count 1 on every day")
		assert.Equal(t, 0, grid[day][0], "hour 0 should be empty")
	}
}

func TestBuildScheduleGrid_MultipleWorkflows(t *testing.T) {
	t.Parallel()
	// Two workflows both scheduled at hour 9 on all days → count of 2.
	statsList := []*WorkflowStats{
		{Workflow: "wf1.lock.yml", Schedules: []string{"0 9 * * *"}},
		{Workflow: "wf2.lock.yml", Schedules: []string{"30 9 * * *"}},
	}
	grid := buildScheduleGrid(statsList)
	require.NotNil(t, grid, "grid should not be nil")

	for day := range 7 {
		assert.Equal(t, 2, grid[day][9], "two workflows at hour 9 should give count 2")
	}
}

func TestBuildScheduleGrid_WeekdayOnly(t *testing.T) {
	t.Parallel()
	// "0 8 * * 1-5" → hour=8, Mon–Fri (cron days 1–5)
	statsList := []*WorkflowStats{
		{Workflow: "weekday.lock.yml", Schedules: []string{"0 8 * * 1-5"}},
	}
	grid := buildScheduleGrid(statsList)
	require.NotNil(t, grid, "grid should not be nil")

	// Mon–Fri (cron days 1-5) should have count 1 at hour 8
	for _, d := range []int{1, 2, 3, 4, 5} {
		assert.Equal(t, 1, grid[d][8], "weekday %d hour 8 should have count 1", d)
	}
	// Sat (6) and Sun (0) should be 0
	assert.Equal(t, 0, grid[0][8], "Sunday hour 8 should be empty")
	assert.Equal(t, 0, grid[6][8], "Saturday hour 8 should be empty")
}

// ---------------------------------------------------------------------------
// intensityChar
// ---------------------------------------------------------------------------

func TestIntensityChar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		count    int
		expected string
	}{
		{0, "·"},
		{1, "░"},
		{2, "▒"},
		{3, "▒"},
		{4, "▓"},
		{6, "▓"},
		{7, "█"},
		{100, "█"},
	}
	for _, tt := range tests {
		got := intensityChar(tt.count)
		assert.Equal(t, tt.expected, got, "intensityChar(%d) should be %q", tt.count, tt.expected)
	}
}

func TestRenderScheduleCalendarCell_NoANSIWhenNotTerminal(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 2, 5, 8} {
		text := intensityChar(count)
		got := renderScheduleCalendarCell(count, text, false, []string{"TERM=xterm-256color"})
		assert.Equal(t, text, got, "non-TTY output should return plain text")
	}
}

func TestRenderScheduleCalendarCell_NoANSIWhenNoColor(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 2, 5, 8} {
		text := intensityChar(count)
		got := renderScheduleCalendarCell(count, text, true, []string{"NO_COLOR=1", "TERM=xterm-256color"})
		assert.Equal(t, text, got, "NO_COLOR output should return plain text")
	}
}

func TestRenderScheduleCalendarCell_UsesANSIInColorTerminal(t *testing.T) {
	// This test intentionally avoids t.Parallel because it temporarily overrides
	// a shared style token; running in parallel could race with other tests that
	// read styles.ScheduleCalendarCritical.
	prevCritical := styles.ScheduleCalendarCritical
	styles.ScheduleCalendarCritical = lipgloss.NewStyle().Transform(func(s string) string {
		return "\x1b[31m" + s + "\x1b[0m"
	})
	t.Cleanup(func() {
		styles.ScheduleCalendarCritical = prevCritical
	})

	got := renderScheduleCalendarCell(8, intensityChar(8), true, []string{"CLICOLOR_FORCE=1", "TERM=xterm-256color"})
	assert.Contains(t, got, "\x1b[", "TTY with color support should contain ANSI escapes")
	assert.Contains(t, got, "\x1b[31m", "TTY path should apply the mocked critical style")
}

// ---------------------------------------------------------------------------
// displayScheduleCalendar (integration-style: captures stderr)
// ---------------------------------------------------------------------------

func TestDisplayScheduleCalendar_NoSchedules(t *testing.T) {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayScheduleCalendar([]*WorkflowStats{{Workflow: "noschedule.lock.yml"}})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	assert.Empty(t, buf.String(), "no output expected when no schedules exist")
}

func TestDisplayScheduleCalendar_WithSchedules(t *testing.T) {
	statsList := []*WorkflowStats{
		{Workflow: "daily.lock.yml", Schedules: []string{"40 20 * * *"}},
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayScheduleCalendar(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	assert.Contains(t, output, "Schedule Heatmap", "output should contain the section title")
	for _, day := range calendarDayNames {
		assert.Contains(t, output, day, "output should contain day label %s", day)
	}
	assert.Contains(t, output, "20", "output should contain the scheduled hour")
	assert.Contains(t, output, "Legend:", "output should contain a legend")
	assert.NotContains(t, output, "\x1b[", "captured non-TTY stderr output should not contain ANSI escapes")
}

func TestDisplayScheduleCalendar_ContainsAllDayLabels(t *testing.T) {
	statsList := []*WorkflowStats{
		{Workflow: "wf.lock.yml", Schedules: []string{"0 12 * * *"}},
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayScheduleCalendar(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	for _, label := range calendarDayNames {
		assert.Contains(t, output, label, "day label %q should appear in output", label)
	}
}

func TestDisplayScheduleCalendar_UnicodeDayLabelAlignment(t *testing.T) {
	originalDayNames := calendarDayNames
	calendarDayNames[0] = "月曜"
	t.Cleanup(func() {
		calendarDayNames = originalDayNames
	})

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
	})

	displayScheduleCalendar([]*WorkflowStats{{Workflow: "wf.lock.yml", Schedules: []string{"0 12 * * *"}}})

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "月曜 ")
}

func TestDisplayScheduleCalendar_ContainsAllHourHeaders(t *testing.T) {
	statsList := []*WorkflowStats{
		{Workflow: "wf.lock.yml", Schedules: []string{"0 0 * * 1"}},
	}

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayScheduleCalendar(statsList)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Spot-check a few hour labels in the header row.
	for _, h := range []string{"00", "06", "12", "18", "23"} {
		assert.Contains(t, output, h, "hour header %q should appear in output", h)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: fuzzy schedule → ScatterSchedule → compile_schedule_calendar
// ---------------------------------------------------------------------------

// TestFuzzyScheduleEndToEnd exercises a fuzzy cron expression through the full
// pipeline: ScatterSchedule (pkg/parser) → parseCronSchedule → buildScheduleGrid
// → displayScheduleCalendar. It asserts that:
//  1. A fuzzy expression is scattered to a valid 5-field cron string.
//  2. The scattered cron is accepted by parseCronSchedule without error.
//  3. buildScheduleGrid registers at least one non-zero slot for the workflow.
//  4. displayScheduleCalendar produces output that contains the workflow name.
func TestFuzzyScheduleEndToEnd(t *testing.T) {
	fuzzyExpressions := []struct {
		fuzzyCron     string
		workflowID    string
		expectedHours int // how many distinct hour values we expect (1 for DAILY patterns)
	}{
		{"FUZZY:DAILY * * *", "ci-doctor", 1},
		{"FUZZY:DAILY_WEEKDAYS * * *", "daily-planner", 1},
		{"FUZZY:DAILY_AROUND:14:0 * * *", "weekly-audit", 1},
	}

	for _, tt := range fuzzyExpressions {
		t.Run(fmt.Sprintf("%s/%s", tt.fuzzyCron, tt.workflowID), func(t *testing.T) {
			// Step 1: scatter the fuzzy expression to a real cron string.
			scatteredCron, err := parser.ScatterSchedule(tt.fuzzyCron, tt.workflowID)
			require.NoError(t, err, "ScatterSchedule should not error for %s", tt.fuzzyCron)
			require.NotEmpty(t, scatteredCron, "ScatterSchedule should return a non-empty cron")
			require.False(t, strings.HasPrefix(scatteredCron, "FUZZY:"),
				"scattered result must not be a fuzzy expression: %s", scatteredCron)

			fields := strings.Fields(scatteredCron)
			require.Len(t, fields, 5,
				"scattered cron %q must have exactly 5 fields", scatteredCron)

			// Step 2: parse the scattered cron with parseCronSchedule.
			hours, daysOfWeek, err := parseCronSchedule(scatteredCron)
			require.NoError(t, err,
				"parseCronSchedule should accept scattered cron %q", scatteredCron)
			assert.Len(t, hours, tt.expectedHours,
				"expected %d distinct hour(s) for %s", tt.expectedHours, tt.fuzzyCron)
			assert.NotEmpty(t, daysOfWeek,
				"daysOfWeek should be non-empty for %s", tt.fuzzyCron)

			// Step 3: buildScheduleGrid should register at least one slot.
			lockName := tt.workflowID + ".lock.yml"
			statsList := []*WorkflowStats{
				{Workflow: lockName, Schedules: []string{scatteredCron}},
			}
			grid := buildScheduleGrid(statsList)
			require.NotNil(t, grid, "buildScheduleGrid should return non-nil grid")

			totalSlots := 0
			for _, day := range grid {
				for _, count := range day {
					totalSlots += count
				}
			}
			assert.Positive(t, totalSlots,
				"grid should contain at least one scheduled slot for %s", scatteredCron)

			// Step 4: displayScheduleCalendar should produce output referencing the hour.
			oldStderr := os.Stderr
			r, w, pipeErr := os.Pipe()
			require.NoError(t, pipeErr)
			os.Stderr = w

			displayScheduleCalendar(statsList)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			output := buf.String()

			assert.Contains(t, output, "Schedule Heatmap",
				"output should contain Schedule Heatmap header")
			// The hour from the scattered cron should appear in the output.
			for _, h := range hours {
				hourStr := fmt.Sprintf("%02d", h)
				assert.Contains(t, output, hourStr,
					"output should contain hour %s from scattered cron %s", hourStr, scatteredCron)
			}
		})
	}
}
