package cli

import (
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var scheduleWizardLog = logger.New("cli:add_interactive_schedule")

// scheduleFrequencyOption represents a frequency option in the schedule wizard
type scheduleFrequencyOption struct {
	Label      string // Human-readable display label
	Value      string // Internal identifier
	Expression string // Schedule expression to write to frontmatter
}

// standardScheduleFrequencies defines the standard frequency options ordered from most to least frequent
var standardScheduleFrequencies = []scheduleFrequencyOption{
	{Label: "Hourly - runs every hour", Value: "hourly", Expression: "every 1h"},
	{Label: "Every 3 hours", Value: "3-hourly", Expression: "every 3h"},
	{Label: "Daily - runs once per day", Value: "daily", Expression: "daily"},
	{Label: "Weekly - runs once per week", Value: "weekly", Expression: "weekly"},
	{Label: "Monthly - runs on the 1st of each month", Value: "monthly", Expression: "0 0 1 * *"},
}

// scheduleDetection holds the result of detecting schedule info from workflow content.
type scheduleDetection struct {
	RawExpr        string // The original schedule expression (e.g., "daily", "0 9 * * *")
	Frequency      string // Classified frequency ("hourly", "daily", "weekly", etc.)
	IsUpdatable    bool   // Whether the schedule can be updated by the wizard
	IsMultiTrigger bool   // True when on: is a map with triggers besides schedule/workflow_dispatch
	IsOnMap        bool   // True when on: is a map (not a simple scalar string)
}

// detectWorkflowScheduleInfo extracts the schedule expression and classifies its frequency
// from workflow content. Returns a scheduleDetection struct.
//
// Workflows whose "on:" field is a simple string schedule, or a map containing a "schedule"
// key, are considered updatable. For multi-trigger workflows (with triggers beyond
// schedule/workflow_dispatch), IsMultiTrigger is set so the caller knows to update the
// "schedule" sub-field rather than the entire "on:" field.
func detectWorkflowScheduleInfo(content string) scheduleDetection {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil || result.Frontmatter == nil {
		return scheduleDetection{}
	}

	onValue, exists := result.Frontmatter["on"]
	if !exists {
		return scheduleDetection{}
	}

	// Case 1: on is a simple string (e.g., "on: daily" or "on: 0 * * * *")
	if onStr, ok := onValue.(string); ok {
		_, _, parseErr := parser.ParseSchedule(onStr)
		if parseErr == nil {
			return scheduleDetection{
				RawExpr:     onStr,
				Frequency:   classifyScheduleFrequency(onStr),
				IsUpdatable: true,
				IsOnMap:     false,
			}
		}
		return scheduleDetection{}
	}

	// Case 2: on is a map — extract schedule value if present
	if onMap, ok := onValue.(map[string]any); ok {
		schedValue, hasSchedule := onMap["schedule"]
		if !hasSchedule {
			return scheduleDetection{}
		}

		// Determine if on: has triggers beyond schedule / workflow_dispatch
		isMultiTrigger := false
		for key := range onMap {
			if key != "schedule" && key != "workflow_dispatch" {
				isMultiTrigger = true
				scheduleWizardLog.Printf("Multi-trigger on: map detected (trigger '%s')", key)
				break
			}
		}

		// Schedule as string shorthand (e.g., "schedule: daily")
		if schedStr, ok := schedValue.(string); ok {
			return scheduleDetection{
				RawExpr:        schedStr,
				Frequency:      classifyScheduleFrequency(schedStr),
				IsUpdatable:    true,
				IsMultiTrigger: isMultiTrigger,
				IsOnMap:        true,
			}
		}

		// Schedule as array (e.g., "schedule:\n  - cron: daily")
		if schedArray, ok := schedValue.([]any); ok && len(schedArray) > 0 {
			// Workflows with multiple cron entries cannot be safely rewritten to a single
			// frequency, so mark them as not updatable.
			if len(schedArray) > 1 {
				scheduleWizardLog.Printf("Multiple cron entries (%d) detected — not updatable", len(schedArray))
				return scheduleDetection{}
			}
			if item, ok := schedArray[0].(map[string]any); ok {
				if cronVal, ok := item["cron"].(string); ok {
					return scheduleDetection{
						RawExpr:        cronVal,
						Frequency:      classifyScheduleFrequency(cronVal),
						IsUpdatable:    true,
						IsMultiTrigger: isMultiTrigger,
						IsOnMap:        true,
					}
				}
			}
		}
	}

	return scheduleDetection{}
}

// classifyScheduleFrequency determines which standard frequency a schedule expression represents.
// Returns one of: "hourly", "3-hourly", "daily", "weekly", "monthly", or "custom".
func classifyScheduleFrequency(scheduleStr string) string {
	normalized := strings.ToLower(strings.TrimSpace(scheduleStr))

	// Direct friendly-format matches
	switch normalized {
	case "hourly", "every 1h", "every 1 hour", "every 1 hours":
		return "hourly"
	case "every 3h", "every 3 hours":
		return "3-hourly"
	case "daily":
		return "daily"
	case "weekly":
		return "weekly"
	}

	// Fuzzy cron placeholder matches (produced by the compiler during preprocessing)
	if strings.HasPrefix(normalized, "fuzzy:hourly/1 ") || normalized == "fuzzy:hourly/1" { //nolint:tolowerequalfold
		return "hourly"
	}
	if strings.HasPrefix(normalized, "fuzzy:hourly/3 ") || normalized == "fuzzy:hourly/3" { //nolint:tolowerequalfold
		return "3-hourly"
	}
	if strings.HasPrefix(normalized, "fuzzy:daily") {
		return "daily"
	}
	if strings.HasPrefix(normalized, "fuzzy:weekly") {
		return "weekly"
	}

	// Cron expression checks
	if parser.IsHourlyCron(scheduleStr) {
		fields := strings.Fields(scheduleStr)
		if len(fields) == 5 {
			interval := strings.TrimPrefix(fields[1], "*/")
			switch interval {
			case "1":
				return "hourly"
			case "3":
				return "3-hourly"
			}
		}
		return "custom"
	}

	if parser.IsDailyCron(scheduleStr) {
		return "daily"
	}

	if parser.IsWeeklyCron(scheduleStr) {
		return "weekly"
	}

	// Monthly cron: M H <day> * * where <day> is a specific numeric date (e.g. "1", "15").
	// fields: [0]=minute [1]=hour [2]=day-of-month [3]=month [4]=day-of-week
	// Excludes interval expressions like "*/2" so that "0 0 */2 * *" (every-2-days) is
	// correctly classified as "custom" rather than "monthly".
	fields := strings.Fields(scheduleStr)
	if len(fields) == 5 && fields[3] == "*" && fields[4] == "*" {
		day := fields[2] // day-of-month field
		if day != "*" && !strings.ContainsAny(day, "*/-,") {
			return "monthly"
		}
	}

	return "custom"
}

// selectScheduleFrequency presents a schedule-frequency selection form to the user when the
// workflow being added has a schedule trigger. If the user picks a different frequency the
// resolved workflow content is updated in memory so the change is reflected in the PR.
func (c *AddInteractiveConfig) selectScheduleFrequency() error {
	if c.resolvedWorkflows == nil || len(c.resolvedWorkflows.Workflows) == 0 {
		return nil
	}

	for _, wf := range c.resolvedWorkflows.Workflows {
		content := string(wf.Content)
		detection := detectWorkflowScheduleInfo(content)
		if !detection.IsUpdatable {
			continue
		}

		rawExpr := detection.RawExpr
		currentFreq := detection.Frequency
		scheduleWizardLog.Printf("Detected schedule: expr=%q, freq=%s, multiTrigger=%v", rawExpr, currentFreq, detection.IsMultiTrigger)

		// Build the ordered option list
		options := buildScheduleOptions(rawExpr, currentFreq)

		var selected string
		form := console.NewSelectForm(
			huh.NewSelect[string]().
				Title("This workflow runs on a schedule. How often should it run?").
				Description("Current schedule: " + rawExpr).
				Options(options...).
				Value(&selected),
		)

		if err := form.RunWithContext(c.Ctx); err != nil {
			return fmt.Errorf("failed to select schedule frequency: %w", err)
		}

		scheduleWizardLog.Printf("User selected frequency: %s", selected)

		// "custom" or same frequency means keep as-is
		if selected == "custom" || selected == currentFreq {
			scheduleWizardLog.Printf("Schedule unchanged: keeping %q", rawExpr)
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected schedule: "+rawExpr))
			continue
		}

		// Look up the schedule expression for the chosen frequency
		var newExpr string
		for _, opt := range standardScheduleFrequencies {
			if opt.Value == selected {
				newExpr = opt.Expression
				break
			}
		}
		if newExpr == "" {
			continue
		}

		// Update the workflow content in memory.
		// When on: is a mapping, update only the schedule sub-key so other triggers
		// (e.g., workflow_dispatch, push) are preserved.
		// When on: is a scalar string, replace the on: field value directly.
		var updatedContent string
		var updateErr error
		if detection.IsOnMap {
			updatedContent, updateErr = UpdateScheduleInOnBlock(content, newExpr)
		} else {
			updatedContent, updateErr = UpdateFieldInFrontmatter(content, "on", newExpr)
		}
		if updateErr != nil {
			scheduleWizardLog.Printf("Failed to update schedule (isOnMap=%v): %v", detection.IsOnMap, updateErr)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not update schedule: %v", updateErr)))
			continue
		}

		wf.Content = []byte(updatedContent)
		if wf.SourceInfo != nil {
			wf.SourceInfo.Content = []byte(updatedContent)
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected schedule: "+selected))
	}

	return nil
}

// buildScheduleOptions constructs the huh option list for the schedule frequency form.
// The default option (matching the current frequency) is placed first.
func buildScheduleOptions(rawExpr, currentFreq string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(standardScheduleFrequencies)+1)

	// If the current schedule doesn't match any standard frequency, add a "Custom" entry
	if currentFreq == "custom" {
		label := fmt.Sprintf("Custom: %s (keep existing)", rawExpr)
		options = append(options, huh.NewOption(label, "custom"))
	}

	// Standard frequency options — mark the one matching the current schedule
	for _, f := range standardScheduleFrequencies {
		label := f.Label
		if f.Value == currentFreq {
			label += " (current)"
		}
		options = append(options, huh.NewOption(label, f.Value))
	}

	// Move the default option to the front so huh selects it initially.
	// classifyScheduleFrequency always returns a non-empty string ("custom" as its last resort),
	// so currentFreq is never empty when this function is called from selectScheduleFrequency.
	reordered := sliceutil.Filter(options, func(opt huh.Option[string]) bool { return opt.Value == currentFreq })
	rest := sliceutil.Filter(options, func(opt huh.Option[string]) bool { return opt.Value != currentFreq })
	return append(reordered, rest...)
}
