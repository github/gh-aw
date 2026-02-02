// This file provides command-line interface functionality for gh-aw.
// This file (logs_display.go) contains functions for displaying workflow logs information
// to the console, including summary tables and metrics.
//
// Key responsibilities:
//   - Rendering workflow logs overview tables
//   - Formatting metrics for display (duration, tokens, cost)
//   - Aggregating totals across multiple runs

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/timeutil"
)

var logsDisplayLog = logger.New("cli:logs_display")

// displayLogsOverview displays a summary table of workflow runs and metrics
func displayLogsOverview(processedRuns []ProcessedRun, verbose bool) {
	if len(processedRuns) == 0 {
		logsDisplayLog.Print("No processed runs to display")
		return
	}

	logsDisplayLog.Printf("Displaying logs overview: runs=%d, verbose=%v", len(processedRuns), verbose)

	// Prepare table data
	headers := []string{"Run ID", "Workflow", "Status", "Duration", "Tokens", "Cost ($)", "Turns", "Errors", "Warnings", "Missing Tools", "Missing Data", "Noops", "Created", "Logs Path"}
	var rows [][]string

	var totalTokens int
	var totalCost float64
	var totalDuration time.Duration
	var totalTurns int
	var totalErrors int
	var totalWarnings int
	var totalMissingTools int
	var totalMissingData int
	var totalNoops int

	for _, pr := range processedRuns {
		run := pr.Run
		// Format duration
		durationStr := ""
		if run.Duration > 0 {
			durationStr = timeutil.FormatDuration(run.Duration)
			totalDuration += run.Duration
		}

		// Format cost
		costStr := ""
		if run.EstimatedCost > 0 {
			costStr = fmt.Sprintf("%.3f", run.EstimatedCost)
			totalCost += run.EstimatedCost
		}

		// Format tokens
		tokensStr := ""
		if run.TokenUsage > 0 {
			tokensStr = console.FormatNumber(run.TokenUsage)
			totalTokens += run.TokenUsage
		}

		// Format turns
		turnsStr := ""
		if run.Turns > 0 {
			turnsStr = fmt.Sprintf("%d", run.Turns)
			totalTurns += run.Turns
		}

		// Format errors
		errorsStr := fmt.Sprintf("%d", run.ErrorCount)
		totalErrors += run.ErrorCount

		// Format warnings
		warningsStr := fmt.Sprintf("%d", run.WarningCount)
		totalWarnings += run.WarningCount

		// Format missing tools
		var missingToolsStr string
		if verbose && len(pr.MissingTools) > 0 {
			// In verbose mode, show actual tool names
			toolNames := make([]string, len(pr.MissingTools))
			for i, tool := range pr.MissingTools {
				toolNames[i] = tool.Tool
			}
			missingToolsStr = strings.Join(toolNames, ", ")
			// Truncate if too long
			if len(missingToolsStr) > 30 {
				missingToolsStr = missingToolsStr[:27] + "..."
			}
		} else {
			// In normal mode, just show the count
			missingToolsStr = fmt.Sprintf("%d", run.MissingToolCount)
		}
		totalMissingTools += run.MissingToolCount

		// Format missing data
		var missingDataStr string
		if verbose && len(pr.MissingData) > 0 {
			// In verbose mode, show actual data types
			dataTypes := make([]string, len(pr.MissingData))
			for i, data := range pr.MissingData {
				dataTypes[i] = data.DataType
			}
			missingDataStr = strings.Join(dataTypes, ", ")
			// Truncate if too long
			if len(missingDataStr) > 30 {
				missingDataStr = missingDataStr[:27] + "..."
			}
		} else {
			// In normal mode, just show the count
			missingDataStr = fmt.Sprintf("%d", run.MissingDataCount)
		}
		totalMissingData += run.MissingDataCount

		// Format noops
		var noopsStr string
		if verbose && len(pr.Noops) > 0 {
			// In verbose mode, show truncated message preview
			messages := make([]string, len(pr.Noops))
			for i, noop := range pr.Noops {
				msg := noop.Message
				if len(msg) > 30 {
					msg = msg[:27] + "..."
				}
				messages[i] = msg
			}
			noopsStr = strings.Join(messages, ", ")
			// Truncate if too long
			if len(noopsStr) > 30 {
				noopsStr = noopsStr[:27] + "..."
			}
		} else {
			// In normal mode, just show the count
			noopsStr = fmt.Sprintf("%d", run.NoopCount)
		}
		totalNoops += run.NoopCount

		// Truncate workflow name if too long
		workflowName := run.WorkflowName
		if len(workflowName) > 20 {
			workflowName = workflowName[:17] + "..."
		}

		// Format relative path
		relPath, _ := filepath.Rel(".", run.LogsPath)

		// Format status - show conclusion directly for completed runs
		statusStr := run.Status
		if run.Status == "completed" && run.Conclusion != "" {
			statusStr = run.Conclusion
		}

		row := []string{
			fmt.Sprintf("%d", run.DatabaseID),
			workflowName,
			statusStr,
			durationStr,
			tokensStr,
			costStr,
			turnsStr,
			errorsStr,
			warningsStr,
			missingToolsStr,
			missingDataStr,
			noopsStr,
			run.CreatedAt.Format("2006-01-02"),
			relPath,
		}
		rows = append(rows, row)
	}

	// Prepare total row
	totalRow := []string{
		fmt.Sprintf("TOTAL (%d runs)", len(processedRuns)),
		"",
		"",
		timeutil.FormatDuration(totalDuration),
		console.FormatNumber(totalTokens),
		fmt.Sprintf("%.3f", totalCost),
		fmt.Sprintf("%d", totalTurns),
		fmt.Sprintf("%d", totalErrors),
		fmt.Sprintf("%d", totalWarnings),
		fmt.Sprintf("%d", totalMissingTools),
		fmt.Sprintf("%d", totalMissingData),
		fmt.Sprintf("%d", totalNoops),
		"",
		"",
	}

	// Render table using console helper
	tableConfig := console.TableConfig{
		Title:     "Workflow Logs Overview",
		Headers:   headers,
		Rows:      rows,
		ShowTotal: true,
		TotalRow:  totalRow,
	}

	logsDisplayLog.Printf("Rendering table: total_tokens=%d, total_cost=%.3f, total_duration=%s", totalTokens, totalCost, totalDuration)

	fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))
}
