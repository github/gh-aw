package cli

// compile_schedule_calendar.go provides cron parsing and a schedule heatmap
// renderer for the --stats flag. It displays a 7×24 calendar grid (days × hours UTC)
// showing how many workflows are scheduled at each time slot, making it easy to
// identify hotspots in the schedule.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/github/gh-aw/pkg/colorwriter"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

// scheduleGrid is a 7×24 count of workflow triggers per day/hour (UTC).
// Index: [dayOfWeek][hour], dayOfWeek follows cron convention: 0=Sun, 1=Mon, ..., 6=Sat.
type scheduleGrid [7][24]int

// calendarDayNames is the ordered display list starting from Monday.
var calendarDayNames = [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// calendarDayIndex maps display order (Mon=0 … Sun=6) to cron day-of-week (0=Sun,1=Mon…6=Sat).
var calendarDayIndex = [7]int{1, 2, 3, 4, 5, 6, 0}

// parseCronField parses a single cron field string and returns all matching
// integer values within [min, max]. Supports *, n, n-m, */step, n-m/step,
// and comma-separated combinations of the above.
func parseCronField(field string, min, max int) ([]int, error) {
	var combined []int
	for part := range strings.SplitSeq(field, ",") {
		vals, err := parseCronFieldPart(strings.TrimSpace(part), min, max)
		if err != nil {
			return nil, err
		}
		combined = append(combined, vals...)
	}

	// Deduplicate while preserving first-seen order.
	seen := make(map[int]bool, len(combined))
	result := combined[:0]
	for _, v := range combined {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result, nil
}

// parseCronFieldPart parses one comma-free cron field segment.
func parseCronFieldPart(part string, min, max int) ([]int, error) {
	// Split on "/" to detect a step.
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step in cron field %q", part)
		}
		step = s
		part = part[:idx]
	}

	var start, end int

	switch {
	case part == "*":
		start, end = min, max

	case strings.Contains(part, "-"):
		rangeParts := strings.SplitN(part, "-", 2)
		s, err1 := strconv.Atoi(rangeParts[0])
		e, err2 := strconv.Atoi(rangeParts[1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid range in cron field %q", part)
		}
		start, end = s, e

	default:
		v, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid value in cron field %q", part)
		}
		if step == 1 {
			// Plain single value with no step – return it directly.
			if v < min || v > max {
				return nil, fmt.Errorf("cron value %d out of range [%d,%d]", v, min, max)
			}
			return []int{v}, nil
		}
		// Single value with a step: treat as "v/step" → start at v, step up to max.
		start, end = v, max
	}

	if start < min || end > max || start > end {
		return nil, fmt.Errorf("cron range %d-%d out of bounds [%d,%d]", start, end, min, max)
	}

	result := make([]int, 0, (end-start)/step+1)
	for v := start; v <= end; v += step {
		result = append(result, v)
		if step > end-v {
			break
		}
	}
	return result, nil
}

// parseCronSchedule parses a 5-field GitHub Actions cron expression
// (minute hour day-of-month month day-of-week, all in UTC) and returns the
// matching hours (0–23) and cron days-of-week (0=Sun … 6=Sat, with 7=Sun
// normalised to 0).
func parseCronSchedule(cron string) (hours []int, daysOfWeek []int, err error) {
	fields := strings.Fields(cron)
	if len(fields) != 5 {
		return nil, nil, fmt.Errorf("cron expression must have 5 fields, got %d: %q", len(fields), cron)
	}

	// Field index 1 is the hour field (0-23).
	hours, err = parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hour field %q in cron %q: %w", fields[1], cron, err)
	}

	// Field index 4 is the day-of-week field (0-7, where both 0 and 7 mean Sunday).
	rawDays, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid day-of-week field %q in cron %q: %w", fields[4], cron, err)
	}

	// Normalise 7 → 0 (Sunday) and deduplicate.
	seen := make(map[int]bool, 7)
	for _, d := range rawDays {
		if d == 7 {
			d = 0
		}
		if !seen[d] {
			seen[d] = true
			daysOfWeek = append(daysOfWeek, d)
		}
	}

	return hours, daysOfWeek, nil
}

// buildScheduleGrid aggregates all cron expressions found in statsList into a
// 7×24 trigger-count grid. Returns nil when no schedules exist.
func buildScheduleGrid(statsList []*WorkflowStats) *scheduleGrid {
	grid := &scheduleGrid{}
	total := 0

	for _, ws := range statsList {
		for _, cron := range ws.Schedules {
			hours, days, err := parseCronSchedule(cron)
			if err != nil {
				compileStatsLog.Printf("Skipping unparseable cron %q: %v", cron, err)
				continue
			}
			for _, day := range days {
				for _, hour := range hours {
					grid[day][hour]++
					total++
				}
			}
		}
	}

	if total == 0 {
		return nil
	}
	return grid
}

// intensityChar maps a trigger count to a block-element character representing
// the heat level of that time slot.
//
//	0  → ·  (empty)
//	1  → ░  (light)
//	2-3 → ▒  (medium)
//	4-6 → ▓  (heavy)
//	7+  → █  (full)
func intensityChar(count int) string {
	switch {
	case count == 0:
		return "·"
	case count == 1:
		return "░"
	case count <= 3:
		return "▒"
	case count <= 6:
		return "▓"
	default:
		return "█"
	}
}

// scheduleCalendarRenderer abstracts the shared Render method implemented by
// both lipgloss.Style (normal builds) and styles.WasmStyle (js/wasm builds).
// Using this interface keeps intensityStyle free of a concrete lipgloss.Style
// return type that would fail to compile for wasm style tokens.
type scheduleCalendarRenderer interface {
	Render(...string) string
}

// intensityStyle returns a centralized style appropriate for the given trigger
// count.
func intensityStyle(count int) scheduleCalendarRenderer {
	switch {
	case count == 0:
		return styles.ScheduleCalendarEmpty
	case count == 1:
		return styles.ScheduleCalendarLow
	case count <= 3:
		return styles.ScheduleCalendarMedium
	case count <= 6:
		return styles.ScheduleCalendarHigh
	default:
		return styles.ScheduleCalendarCritical
	}
}

func renderScheduleCalendarCell(count int, text string, isTerminal bool, environ []string) string {
	if !isTerminal || console.IsAccessibleMode() {
		return text
	}
	return colorwriter.Degrade(intensityStyle(count).Render(text), environ)
}

// displayScheduleCalendar renders a text heatmap of scheduled workflow times to
// stderr. The grid shows days of the week (Mon–Sun) against hours of the day
// (00–23, UTC). Each cell intensity indicates how many workflows fire at that
// time. Nothing is rendered when no scheduled workflows are found or the list is
// empty.
//
// Only rendered in regular (non-JSON) output mode.
func displayScheduleCalendar(statsList []*WorkflowStats) {
	grid := buildScheduleGrid(statsList)
	if grid == nil {
		return
	}

	isTerminal := tty.IsStderrTerminal()
	environ := os.Environ()

	// Title
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Schedule Heatmap (UTC)"))
	fmt.Fprintln(os.Stderr)

	// Hour header row: each cell is 3 chars wide ("XX ").
	// The day-label column is 5 chars: "Mon  " (3 + 2 spaces).
	const cellWidth = 3
	const dayLabelWidth = 5

	var headerBuf strings.Builder
	headerBuf.WriteString(strings.Repeat(" ", dayLabelWidth))
	for h := range 24 {
		fmt.Fprintf(&headerBuf, "%02d ", h)
	}
	header := headerBuf.String()
	fmt.Fprintln(os.Stderr, console.FormatTableHeaderStderr(header))

	// One row per day, ordered Mon through Sun.
	for calendarIndex, dayLabel := range calendarDayNames {
		cronDay := calendarDayIndex[calendarIndex]

		var row strings.Builder
		label := lipgloss.NewStyle().Width(dayLabelWidth).Render(dayLabel)
		row.WriteString(console.FormatTableHeaderStderr(label))

		for h := range 24 {
			count := grid[cronDay][h]
			ch := intensityChar(count)
			// Pad each cell to cellWidth with a trailing space.
			cell := ch + strings.Repeat(" ", cellWidth-len([]rune(ch)))
			row.WriteString(renderScheduleCalendarCell(count, cell, isTerminal, environ))
		}

		fmt.Fprintln(os.Stderr, row.String())
	}

	// Legend
	fmt.Fprintln(os.Stderr)
	type legendEntry struct {
		count int
		label string
	}
	entries := []legendEntry{{0, "0"}, {1, "1"}, {2, "2-3"}, {5, "4-6"}, {8, "7+"}}

	var legend strings.Builder
	legend.WriteString("Legend: ")
	for _, e := range entries {
		ch := intensityChar(e.count)
		legend.WriteString(renderScheduleCalendarCell(e.count, ch, isTerminal, environ))
		legend.WriteString(" = " + e.label + "   ")
	}
	fmt.Fprintln(os.Stderr, legend.String())
	fmt.Fprintln(os.Stderr)
}
