package parser

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var scheduleLog = logger.New("parser:schedule_parser")

// ErrUnsupportedSyntax is returned by ParseSchedule when the input uses a
// recognised schedule syntax that is explicitly not supported (e.g. "daily at
// 2pm"). Callers that need to distinguish this from a completely unrecognised
// input should test with errors.Is.
var ErrUnsupportedSyntax = errors.New("unsupported schedule syntax")

// durationPattern matches short duration format: number followed by unit (pre-compiled for performance)
var durationPattern = regexp.MustCompile(`^(\d+)([hdwm]|mo)$`)

// ScheduleParser parses human-friendly schedule expressions into cron expressions
type ScheduleParser struct {
	input  string
	tokens []string
	pos    int
}

// ParseSchedule converts a human-friendly schedule expression into a cron expression
// Returns the cron expression and the original friendly format for comments
func ParseSchedule(input string) (cron string, original string, err error) {
	scheduleLog.Printf("Parsing schedule expression: %s", input)
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errors.New("schedule expression cannot be empty")
	}

	// If it's already a cron expression (5 fields separated by spaces), return as-is
	if IsCronExpression(input) {
		scheduleLog.Printf("Input is already a valid cron expression: %s", input)
		return input, "", nil
	}

	parser := &ScheduleParser{
		input: input,
	}

	// Tokenize the input
	if err := parser.tokenize(); err != nil {
		scheduleLog.Printf("Tokenization failed: %s", err)
		return "", "", err
	}

	// Parse the tokens
	cronExpr, err := parser.parse()
	if err != nil {
		scheduleLog.Printf("Parsing failed: %s", err)
		return "", "", err
	}

	scheduleLog.Printf("Successfully parsed schedule to cron: %s", cronExpr)
	return cronExpr, input, nil
}

// tokenize breaks the input into tokens
func (p *ScheduleParser) tokenize() error {
	// Normalize the input
	input := strings.ToLower(strings.TrimSpace(p.input))

	// Split on whitespace
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return errors.New("empty schedule expression")
	}

	p.tokens = tokens
	p.pos = 0
	return nil
}

// parse parses the tokens into a cron expression
func (p *ScheduleParser) parse() (string, error) {
	if len(p.tokens) == 0 {
		return "", errors.New("no tokens to parse")
	}

	// Check for interval-based schedules: "every N minutes|hours"
	if p.tokens[0] == "every" {
		return p.parseInterval()
	}

	// Otherwise, parse as base schedule (daily, weekly, monthly, yearly)
	return p.parseBase()
}

// parseInterval parses interval-based schedules like "every 10 minutes" or "every 2h"
func (p *ScheduleParser) parseInterval() (string, error) {
	if len(p.tokens) < 2 {
		return "", errors.New("invalid interval format, expected 'every N unit' or 'every Nunit'")
	}
	scheduleLog.Printf("Parsing interval schedule: tokens=%v", p.tokens)

	hasWeekdaysSuffix := p.hasWeekdaysSuffix()

	if cronExpr, handled, err := p.parseShortDurationInterval(hasWeekdaysSuffix); handled || err != nil {
		return cronExpr, err
	}

	if cronExpr, handled, err := p.parseEveryDayIntervalAlias(hasWeekdaysSuffix); handled || err != nil {
		return cronExpr, err
	}

	return p.parseLongInterval(hasWeekdaysSuffix)
}

// parseBase parses base schedules like "daily", "weekly on monday", etc.
func (p *ScheduleParser) parseBase() (string, error) {
	if len(p.tokens) == 0 {
		return "", errors.New("empty schedule")
	}

	baseType := p.tokens[0]
	scheduleLog.Printf("Parsing base schedule: type=%s, tokens=%v", baseType, p.tokens)
	hasWeekdaysSuffix := p.hasWeekdaysSuffix()

	switch baseType {
	case "daily":
		return p.parseDailyBase(hasWeekdaysSuffix)
	case "hourly":
		return p.parseHourlyBase(hasWeekdaysSuffix)
	case "weekly":
		return p.parseWeeklyBase()
	case "bi-weekly":
		return p.parseNamedFuzzyBase("bi-weekly", "FUZZY:BI_WEEKLY * * *")
	case "tri-weekly":
		return p.parseNamedFuzzyBase("tri-weekly", "FUZZY:TRI_WEEKLY * * *")
	case "monthly":
		return p.parseMonthlyBase()

	default:
		return "", fmt.Errorf("unsupported schedule type '%s', use 'daily', 'hourly', 'weekly', 'bi-weekly', 'tri-weekly', or 'monthly'", baseType)
	}
}

func (p *ScheduleParser) parseShortDurationInterval(hasWeekdaysSuffix bool) (string, bool, error) {
	if !p.usesShortDurationSyntax(hasWeekdaysSuffix) {
		return "", false, nil
	}

	matches := durationPattern.FindStringSubmatch(p.tokens[1])
	if matches == nil {
		return "", false, nil
	}

	interval, err := strconv.Atoi(matches[1])
	if err != nil {
		return "", true, fmt.Errorf("invalid duration interval %q: %w", matches[1], err)
	}
	unit := matches[2]
	if err := p.validateIntervalTimeClause(2, hasWeekdaysSuffix); err != nil {
		return "", true, err
	}
	if err := validateMinimumInterval(interval, unit); err != nil {
		return "", true, err
	}

	cronExpr, err := formatShortDurationCron(interval, unit, hasWeekdaysSuffix)
	return cronExpr, true, err
}

func (p *ScheduleParser) usesShortDurationSyntax(hasWeekdaysSuffix bool) bool {
	return len(p.tokens) == 2 ||
		(len(p.tokens) == 4 && hasWeekdaysSuffix) ||
		(len(p.tokens) > 2 && !hasWeekdaysSuffix && p.tokens[2] != "minutes" && p.tokens[2] != "hours" && p.tokens[2] != "minute" && p.tokens[2] != "hour")
}

func (p *ScheduleParser) parseEveryDayIntervalAlias(hasWeekdaysSuffix bool) (string, bool, error) {
	if len(p.tokens) < 2 {
		return "", false, nil
	}
	if p.tokens[1] != "day" && p.tokens[1] != "days" {
		return "", false, nil
	}
	if len(p.tokens) == 2 || (len(p.tokens) == 4 && hasWeekdaysSuffix) {
		if hasWeekdaysSuffix {
			return "FUZZY:DAILY_WEEKDAYS * * *", true, nil
		}
		return "FUZZY:DAILY * * *", true, nil
	}
	if len(p.tokens) > 2 && p.tokens[2] == "at" {
		timeStr, err := p.extractTime(2)
		if err != nil {
			return "", true, err
		}
		minute, hour := parseTime(timeStr)
		if hasWeekdaysSuffix {
			return fmt.Sprintf("%s %s * * 1-5", minute, hour), true, nil
		}
		return fmt.Sprintf("%s %s * * *", minute, hour), true, nil
	}
	return "", true, errors.New("invalid 'every day' format, use 'every day' or 'every day at HH:MM'")
}

func (p *ScheduleParser) parseLongInterval(hasWeekdaysSuffix bool) (string, error) {
	if len(p.tokens) < p.minimumIntervalTokenCount(hasWeekdaysSuffix) {
		return "", errors.New("invalid interval format, expected 'every N unit' or 'every Nunit' (e.g., 'every 2h')")
	}

	interval, unit, err := p.parseLongIntervalParts()
	if err != nil {
		return "", err
	}
	if err := p.validateIntervalTimeClause(3, hasWeekdaysSuffix); err != nil {
		return "", err
	}
	if err := validateMinimumInterval(interval, unit); err != nil {
		return "", err
	}
	return formatLongIntervalCron(interval, unit, hasWeekdaysSuffix)
}

func (p *ScheduleParser) minimumIntervalTokenCount(hasWeekdaysSuffix bool) int {
	if hasWeekdaysSuffix {
		return 5
	}
	return 3
}

func (p *ScheduleParser) parseLongIntervalParts() (int, string, error) {
	if len(p.tokens) < 3 {
		return 0, "", errors.New("invalid interval format, expected 'every N unit' or 'every Nunit' (e.g., 'every 2h')")
	}
	intervalStr := p.tokens[1]
	interval, err := strconv.Atoi(intervalStr)
	if err != nil || interval < 1 {
		return 0, "", fmt.Errorf("invalid interval '%s', must be a positive integer", intervalStr)
	}

	unit := p.tokens[2]
	if !strings.HasSuffix(unit, "s") {
		unit += "s"
	}
	if unit != "minutes" && unit != "hours" && unit != "days" {
		return 0, "", fmt.Errorf("unsupported interval unit '%s', use 'minutes', 'hours', or 'days'", unit)
	}
	return interval, unit, nil
}

func (p *ScheduleParser) validateIntervalTimeClause(startPos int, hasWeekdaysSuffix bool) error {
	if startPos < 0 {
		return errors.New("invalid interval time clause start position")
	}
	endPos := len(p.tokens)
	if hasWeekdaysSuffix {
		endPos -= 2
	}
	for i := startPos; i < endPos; i++ {
		if p.tokens[i] == "at" {
			return errors.New("interval schedules cannot have 'at time' clause")
		}
	}
	return nil
}

func validateMinimumInterval(interval int, unit string) error {
	totalMinutes, err := intervalToMinutes(interval, unit)
	if err != nil {
		return err
	}
	if totalMinutes < 5 {
		return fmt.Errorf("minimum schedule interval is 5 minutes, got %d minute(s)", totalMinutes)
	}
	return nil
}

func intervalToMinutes(interval int, unit string) (int, error) {
	switch unit {
	case "m", "minutes":
		return interval, nil
	case "h", "hours":
		return interval * 60, nil
	case "d", "days":
		return interval * 24 * 60, nil
	case "w":
		return interval * 7 * 24 * 60, nil
	case "mo":
		return interval * 30 * 24 * 60, nil
	default:
		return 0, fmt.Errorf("unsupported duration unit '%s'", unit)
	}
}

func formatShortDurationCron(interval int, unit string, hasWeekdaysSuffix bool) (string, error) {
	switch unit {
	case "m":
		if hasWeekdaysSuffix {
			return "", errors.New("minute intervals with 'on weekdays' are not supported")
		}
		return fmt.Sprintf("FUZZY:EVERY_MINUTE/%d * * * *", interval), nil
	case "h":
		return formatHourlyIntervalCron(interval, hasWeekdaysSuffix), nil
	case "d":
		return formatDailyIntervalCron(interval), nil
	case "w":
		return formatWeeklyIntervalCron(interval), nil
	case "mo":
		return formatMonthlyIntervalCron(interval), nil
	default:
		return "", fmt.Errorf("unsupported duration unit '%s'", unit)
	}
}

func formatLongIntervalCron(interval int, unit string, hasWeekdaysSuffix bool) (string, error) {
	switch unit {
	case "minutes":
		if hasWeekdaysSuffix {
			return "", errors.New("minute intervals with 'on weekdays' are not supported")
		}
		return fmt.Sprintf("FUZZY:EVERY_MINUTE/%d * * * *", interval), nil
	case "hours":
		return formatHourlyIntervalCron(interval, hasWeekdaysSuffix), nil
	case "days":
		return formatDailyIntervalCron(interval), nil
	default:
		return "", fmt.Errorf("unsupported interval unit '%s', use 'minutes', 'hours', or 'days'", unit)
	}
}

func formatHourlyIntervalCron(interval int, hasWeekdaysSuffix bool) string {
	if hasWeekdaysSuffix {
		return fmt.Sprintf("FUZZY:HOURLY_WEEKDAYS/%d * * *", interval)
	}
	return fmt.Sprintf("FUZZY:HOURLY/%d * * *", interval)
}

func formatDailyIntervalCron(interval int) string {
	if interval == 1 {
		return "0 0 * * *"
	}
	return fmt.Sprintf("0 0 */%d * *", interval)
}

func formatWeeklyIntervalCron(interval int) string {
	if interval == 1 {
		return "0 0 * * 0"
	}
	return fmt.Sprintf("0 0 */%d * *", interval*7)
}

func formatMonthlyIntervalCron(interval int) string {
	if interval == 1 {
		return "0 0 1 * *"
	}
	return fmt.Sprintf("0 0 1 */%d *", interval)
}

func (p *ScheduleParser) parseDailyBase(hasWeekdaysSuffix bool) (string, error) {
	if len(p.tokens) == 0 {
		return "", errors.New("empty schedule")
	}
	if len(p.tokens) == 1 || (len(p.tokens) == 3 && hasWeekdaysSuffix) {
		if hasWeekdaysSuffix {
			return "FUZZY:DAILY_WEEKDAYS * * *", nil
		}
		return "FUZZY:DAILY * * *", nil
	}
	switch p.tokens[1] {
	case "between":
		return p.parseDailyBetween(hasWeekdaysSuffix)
	case "around":
		return p.parseDailyAround(hasWeekdaysSuffix)
	default:
		return "", fmt.Errorf("'daily at <time>' syntax is not supported. Use fuzzy schedules like 'daily' (scattered), 'daily around <time>', or 'daily between <start> and <end>' for load distribution. For fixed times, use standard cron syntax (e.g., '0 14 * * *'): %w", ErrUnsupportedSyntax)
	}
}

func (p *ScheduleParser) parseDailyBetween(hasWeekdaysSuffix bool) (string, error) {
	andIndex, err := p.findDailyBetweenSeparator(hasWeekdaysSuffix)
	if err != nil {
		return "", err
	}

	startTimeStr, err := p.extractTimeBetween(2, andIndex)
	if err != nil {
		return "", fmt.Errorf("invalid start time in 'between' clause: %w", err)
	}
	endTimeStr, err := p.extractTimeAfter(andIndex+1, hasWeekdaysSuffix)
	if err != nil {
		return "", fmt.Errorf("invalid end time in 'between' clause: %w", err)
	}

	startMinute, startHour := parseTime(startTimeStr)
	endMinute, endHour := parseTime(endTimeStr)
	if parseTimeToMinutes(startHour, startMinute) == parseTimeToMinutes(endHour, endMinute) {
		return "", errors.New("start and end times cannot be the same in 'between' clause")
	}
	if hasWeekdaysSuffix {
		return fmt.Sprintf("FUZZY:DAILY_BETWEEN_WEEKDAYS:%s:%s:%s:%s * * *", startHour, startMinute, endHour, endMinute), nil
	}
	return fmt.Sprintf("FUZZY:DAILY_BETWEEN:%s:%s:%s:%s * * *", startHour, startMinute, endHour, endMinute), nil
}

func (p *ScheduleParser) findDailyBetweenSeparator(hasWeekdaysSuffix bool) (int, error) {
	if len(p.tokens) < 5 {
		return -1, errors.New("invalid 'between' format, expected 'daily between START and END'")
	}
	endPos := len(p.tokens)
	if hasWeekdaysSuffix {
		endPos -= 2
	}
	for i := 2; i < endPos; i++ {
		if p.tokens[i] == "and" {
			return i, nil
		}
	}
	return -1, errors.New("missing 'and' keyword in 'between' clause")
}

func (p *ScheduleParser) parseDailyAround(hasWeekdaysSuffix bool) (string, error) {
	timeStr, err := p.extractTimeWithWeekdays(2, hasWeekdaysSuffix)
	if err != nil {
		return "", err
	}

	minute, hour := parseTime(timeStr)
	if hasWeekdaysSuffix {
		return fmt.Sprintf("FUZZY:DAILY_AROUND_WEEKDAYS:%s:%s * * *", hour, minute), nil
	}
	return fmt.Sprintf("FUZZY:DAILY_AROUND:%s:%s * * *", hour, minute), nil
}

func (p *ScheduleParser) parseHourlyBase(hasWeekdaysSuffix bool) (string, error) {
	scheduleLog.Printf("Parsing hourly schedule: weekdays=%v", hasWeekdaysSuffix)
	if len(p.tokens) == 1 || (len(p.tokens) == 3 && hasWeekdaysSuffix) {
		return formatHourlyIntervalCron(1, hasWeekdaysSuffix), nil
	}
	return "", errors.New("hourly schedule does not support 'at time' clause, use 'hourly' without additional parameters")
}

func (p *ScheduleParser) parseWeeklyBase() (string, error) {
	scheduleLog.Printf("Parsing weekly schedule: token_count=%d", len(p.tokens))
	if len(p.tokens) == 1 {
		return "FUZZY:WEEKLY * * *", nil
	}
	if len(p.tokens) < 3 || p.tokens[1] != "on" {
		return "", errors.New("weekly schedule requires 'on <weekday>' or use 'weekly' alone for fuzzy schedule")
	}

	weekdayStr := p.tokens[2]
	weekday := mapWeekday(weekdayStr)
	if weekday == "" {
		return "", fmt.Errorf("invalid weekday '%s'", weekdayStr)
	}
	if len(p.tokens) == 3 {
		return fmt.Sprintf("FUZZY:WEEKLY:%s * * *", weekday), nil
	}
	if p.tokens[3] != "around" {
		return "", fmt.Errorf("'weekly on <weekday> at <time>' syntax is not supported. Use fuzzy schedules like 'weekly on %s' (scattered), 'weekly on %s around <time>', or standard cron syntax (e.g., '30 6 * * %s'): %w", weekdayStr, weekdayStr, weekday, ErrUnsupportedSyntax)
	}

	timeStr, err := p.extractTime(4)
	if err != nil {
		return "", err
	}
	minute, hour := parseTime(timeStr)
	return fmt.Sprintf("FUZZY:WEEKLY_AROUND:%s:%s:%s * * *", weekday, hour, minute), nil
}

func (p *ScheduleParser) parseNamedFuzzyBase(name, cronExpr string) (string, error) {
	if len(p.tokens) == 1 {
		return cronExpr, nil
	}
	return "", fmt.Errorf("%s schedule does not support additional parameters, use '%s' alone for fuzzy schedule", name, name)
}

func (p *ScheduleParser) parseMonthlyBase() (string, error) {
	scheduleLog.Printf("Parsing monthly schedule: token_count=%d", len(p.tokens))
	if len(p.tokens) < 3 || p.tokens[1] != "on" {
		return "", errors.New("monthly schedule requires 'on <day>'")
	}

	day := p.tokens[2]
	dayNum, err := strconv.Atoi(day)
	if err != nil || dayNum < 1 || dayNum > 31 {
		return "", fmt.Errorf("invalid day of month '%s', must be 1-31", day)
	}
	if len(p.tokens) > 3 {
		return "", fmt.Errorf("'monthly on <day> at <time>' syntax is not supported. Use standard cron syntax for monthly schedules (e.g., '0 9 %s * *' for the %sth at 9am): %w", day, day, ErrUnsupportedSyntax)
	}
	return "", fmt.Errorf("'monthly on <day>' syntax is not supported. Use standard cron syntax for monthly schedules (e.g., '0 0 %s * *' for the %sth at midnight): %w", day, day, ErrUnsupportedSyntax)
}

// extractTime extracts the time specification from tokens starting at startPos
// Returns the time string (HH:MM, midnight, or noon) with optional UTC offset
func (p *ScheduleParser) extractTime(startPos int) (string, error) {
	if startPos < 0 || startPos >= len(p.tokens) {
		return "", errors.New("expected time specification")
	}

	// Check for "at" keyword
	if p.tokens[startPos] == "at" {
		startPos++
		if startPos >= len(p.tokens) {
			return "", errors.New("expected time after 'at'")
		}
	}

	timeTokens := []string{p.tokens[startPos]}
	nextIndex := startPos + 1
	if nextIndex < len(p.tokens) && isAMPMToken(p.tokens[nextIndex]) {
		timeTokens = append(timeTokens, p.tokens[nextIndex])
		nextIndex++
	}
	if nextIndex < len(p.tokens) {
		timezoneToken := strings.ToLower(p.tokens[nextIndex])
		if strings.HasPrefix(timezoneToken, "utc") {
			timeTokens = append(timeTokens, timezoneToken)
		} else if normalized, ok := normalizeTimezoneAbbreviation(timezoneToken); ok {
			timeTokens = append(timeTokens, normalized)
		}
	}

	return normalizeTimeTokens(timeTokens), nil
}

// extractTimeBetween extracts a time specification from tokens between startPos and endPos (exclusive)
// Used for parsing the start time in "between START and END" clauses
func (p *ScheduleParser) extractTimeBetween(startPos, endPos int) (string, error) {
	if startPos < 0 || startPos >= len(p.tokens) || startPos >= endPos {
		return "", errors.New("expected time specification")
	}

	// The time is in the tokens between startPos and endPos
	// It might be a single token (e.g., "9am") or multiple tokens (e.g., "14:00 utc+9")
	timeTokens := []string{}
	for i := startPos; i < endPos && i < len(p.tokens); i++ {
		timeTokens = append(timeTokens, p.tokens[i])
	}

	if len(timeTokens) == 0 {
		return "", errors.New("expected time specification")
	}

	return normalizeTimeTokens(timeTokens), nil
}

// extractTimeAfter extracts a time specification from tokens starting at startPos until the end
// Used for parsing the end time in "between START and END" clauses
// If hasWeekdaysSuffix is true, excludes the last 2 tokens ("on weekdays")
func (p *ScheduleParser) extractTimeAfter(startPos int, hasWeekdaysSuffix bool) (string, error) {
	if startPos < 0 || startPos >= len(p.tokens) {
		return "", errors.New("expected time specification")
	}

	endPos := len(p.tokens)
	if hasWeekdaysSuffix {
		endPos -= 2
	}

	if startPos >= endPos {
		return "", errors.New("expected time specification")
	}

	// Collect tokens until endPos (time and optional UTC offset)
	timeStr := p.tokens[startPos]

	timeTokens := []string{timeStr}
	nextIndex := startPos + 1
	if nextIndex < endPos && isAMPMToken(p.tokens[nextIndex]) {
		timeTokens = append(timeTokens, p.tokens[nextIndex])
		nextIndex++
	}
	if nextIndex < endPos {
		timezoneToken := strings.ToLower(p.tokens[nextIndex])
		if strings.HasPrefix(timezoneToken, "utc") {
			timeTokens = append(timeTokens, timezoneToken)
		} else if normalized, ok := normalizeTimezoneAbbreviation(timezoneToken); ok {
			timeTokens = append(timeTokens, normalized)
		}
	}

	return normalizeTimeTokens(timeTokens), nil
}

// hasWeekdaysSuffix checks if "on weekdays" is present at the end of tokens
func (p *ScheduleParser) hasWeekdaysSuffix() bool {
	if len(p.tokens) < 2 {
		return false
	}
	// Check if the last two tokens are "on" and "weekdays"
	return p.tokens[len(p.tokens)-2] == "on" && p.tokens[len(p.tokens)-1] == "weekdays"
}

// extractTimeWithWeekdays extracts time specification, handling optional "on weekdays" suffix
func (p *ScheduleParser) extractTimeWithWeekdays(startPos int, hasWeekdaysSuffix bool) (string, error) {
	if startPos < 0 || startPos >= len(p.tokens) {
		return "", errors.New("expected time specification")
	}

	// Check for "at" keyword
	if p.tokens[startPos] == "at" {
		startPos++
		if startPos >= len(p.tokens) {
			return "", errors.New("expected time after 'at'")
		}
	}

	endPos := len(p.tokens)
	if hasWeekdaysSuffix {
		endPos -= 2
	}

	timeTokens := []string{p.tokens[startPos]}
	nextIndex := startPos + 1
	if nextIndex < endPos && isAMPMToken(p.tokens[nextIndex]) {
		timeTokens = append(timeTokens, p.tokens[nextIndex])
		nextIndex++
	}
	if nextIndex < endPos {
		timezoneToken := strings.ToLower(p.tokens[nextIndex])
		if strings.HasPrefix(timezoneToken, "utc") {
			timeTokens = append(timeTokens, timezoneToken)
		} else if normalized, ok := normalizeTimezoneAbbreviation(timezoneToken); ok {
			timeTokens = append(timeTokens, normalized)
		}
	}

	return normalizeTimeTokens(timeTokens), nil
}
