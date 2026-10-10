package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

var compileStatsLog = logger.New("cli:compile_stats")

// WorkflowFailure represents a failed workflow with its error count
type WorkflowFailure struct {
	Path          string   // File path of the workflow
	ErrorCount    int      // Number of errors in this workflow
	ErrorMessages []string // Actual error messages to display to the user
}

// CompilationStats tracks the results of workflow compilation
type CompilationStats struct {
	Total           int
	Succeeded       int // Explicitly-tracked count of workflows that compiled successfully
	Errors          int
	Warnings        int
	FailedWorkflows []string          // Names of workflows that failed compilation (deprecated, use FailedWorkflowDetails)
	FailureDetails  []WorkflowFailure // Detailed information about failed workflows (workflow-level failures only)
}

// WorkflowStats holds statistics about a compiled workflow
type WorkflowStats struct {
	Workflow    string
	FileSize    int64
	Jobs        int
	Steps       int
	ScriptCount int
	ScriptSize  int
	ShellCount  int
	ShellSize   int
	Schedules   []string // Cron expressions from on.schedule[*].cron
}

// collectWorkflowStats parses a lock file and collects statistics
func collectWorkflowStats(lockFilePath string) (*WorkflowStats, error) {
	compileStatsLog.Printf("Collecting workflow stats: file=%s", lockFilePath)
	// Get file size
	fileInfo, err := os.Stat(lockFilePath)
	if err != nil {
		compileStatsLog.Printf("Failed to stat file: %v", err)
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Read and parse YAML
	content, err := os.ReadFile(lockFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var workflowYAML map[string]any
	if err := yaml.Unmarshal(content, &workflowYAML); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	stats := &WorkflowStats{
		Workflow: filepath.Base(lockFilePath),
		FileSize: fileInfo.Size(),
	}

	// Count jobs and steps
	if jobs, ok := workflowYAML["jobs"].(map[string]any); ok {
		stats.Jobs = len(jobs)
		compileStatsLog.Printf("Workflow has %d jobs", stats.Jobs)

		// Iterate through jobs to count steps and scripts
		for _, jobData := range jobs {
			if job, ok := jobData.(map[string]any); ok {
				if steps, ok := job["steps"].([]any); ok {
					stats.Steps += len(steps)

					// Check each step for scripts
					for _, stepData := range steps {
						if step, ok := stepData.(map[string]any); ok {
							// Check for "run" field (script)
							if runScript, ok := step["run"].(string); ok {
								stats.ScriptCount++
								stats.ScriptSize += len(runScript)
							}

							// Check for "shell" field
							if shell, ok := step["shell"].(string); ok {
								stats.ShellCount++
								stats.ShellSize += len(shell)
							}
						}
					}
				}
			}
		}
	}

	// Extract cron expressions from on.schedule[*].cron
	if onSection, ok := workflowYAML["on"].(map[string]any); ok {
		if schedules, ok := onSection["schedule"].([]any); ok {
			for _, entry := range schedules {
				if schedMap, ok := entry.(map[string]any); ok {
					if cron, ok := schedMap["cron"].(string); ok && cron != "" {
						stats.Schedules = append(stats.Schedules, cron)
					}
				}
			}
		}
	}

	compileStatsLog.Printf("Stats collected: jobs=%d, steps=%d, scripts=%d, size=%d bytes, schedules=%d",
		stats.Jobs, stats.Steps, stats.ScriptCount, stats.FileSize, len(stats.Schedules))
	return stats, nil
}

// trackWorkflowFailure adds a workflow failure to the compilation statistics
func trackWorkflowFailure(stats *CompilationStats, workflowPath string, errorCount int, errorMessages []string) {
	// Add to FailedWorkflows for backward compatibility
	stats.FailedWorkflows = append(stats.FailedWorkflows, filepath.Base(workflowPath))

	// Add detailed failure information
	stats.FailureDetails = append(stats.FailureDetails, WorkflowFailure{
		Path:          workflowPath,
		ErrorCount:    errorCount,
		ErrorMessages: errorMessages,
	})
}

// printCompilationSummary prints a summary of the compilation results
func printCompilationSummary(stats *CompilationStats, showAllErrors bool) {
	if stats.Total == 0 {
		return
	}

	failedWorkflowCount := len(stats.FailureDetails)
	if failedWorkflowCount == 0 {
		failedWorkflowCount = len(stats.FailedWorkflows)
	}
	successCount := stats.Succeeded

	summary := fmt.Sprintf("Compiled %s: %s, %s",
		formatWorkflowCount(stats.Total),
		formatSucceededCount(successCount),
		formatWarningCount(stats.Warnings))
	if failedWorkflowCount > 0 {
		summary = fmt.Sprintf("Compiled %s: %s, %s, %s, %s",
			formatWorkflowCount(stats.Total),
			formatSucceededCount(successCount),
			formatFailedCount(failedWorkflowCount),
			formatErrorCount(stats.Errors),
			formatWarningCount(stats.Warnings))
	}

	// Use different formatting based on whether there were errors
	if stats.Errors > 0 {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(summary))

		// Show agent-friendly list of failed workflow IDs first
		if len(stats.FailureDetails) > 0 {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Failed workflows:"))
			for _, failure := range stats.FailureDetails {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(filepath.Base(failure.Path)))
			}
			fmt.Fprintln(os.Stderr)

			// Display the actual error messages for each failed workflow
			for _, failure := range stats.FailureDetails {
				report := workflow.BuildPrioritizedErrorReportFromMessages(failure.ErrorMessages, showAllErrors)
				if report.TotalCount == 0 {
					continue
				}

				header := fmt.Sprintf("%s (%d error(s)", filepath.Base(failure.Path), report.TotalCount)
				if !showAllErrors && report.HiddenCount > 0 {
					header += fmt.Sprintf(", showing top %d", len(report.DisplayedErrors))
				}
				header += "):"
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(header))

				lastHeading := ""
				for i, prioritized := range report.DisplayedErrors {
					heading := prioritized.Severity.Heading()
					if heading != lastHeading {
						fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("%s %s:", prioritized.Severity.Icon(), heading)))
						lastHeading = heading
					}
					fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("%d. %s", i+1, prioritized.Message)))
					if prioritized.Suggestion != "" {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("→ "+prioritized.Suggestion))
					}
				}

				if report.RecoveryPlan != nil && len(report.RecoveryPlan.Steps) > 0 {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("💡 Recovery plan:"))
					for i, step := range report.RecoveryPlan.Steps {
						fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("%d. %s", i+1, step)))
					}
				}

				if report.SuppressedCount > 0 {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Suppressed %d cascading error(s) until the root cause is fixed.", report.SuppressedCount)))
				}

				if !showAllErrors && report.HiddenCount > 0 {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Run 'gh aw compile --show-all' to see all %d prioritized error(s).", report.TotalCount)))
				}
			}
		} else if len(stats.FailedWorkflows) > 0 {
			// Fallback for backward compatibility if FailureDetails is not populated
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Failed workflows:"))
			for _, wf := range stats.FailedWorkflows {
				fmt.Fprintln(os.Stderr, console.FormatErrorMessage(wf))
			}
			fmt.Fprintln(os.Stderr)
		}
	} else if stats.Warnings > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(summary))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(summary))
	}
}

func formatWorkflowCount(count int) string {
	return fmt.Sprintf("%d %s", count, pluralize("workflow", count))
}

func formatSucceededCount(count int) string {
	return fmt.Sprintf("%d succeeded", count)
}

func formatFailedCount(count int) string {
	return fmt.Sprintf("%d failed", count)
}

func formatErrorCount(count int) string {
	return fmt.Sprintf("%d %s", count, pluralize("error", count))
}

func formatWarningCount(count int) string {
	return fmt.Sprintf("%d %s", count, pluralize("warning", count))
}

// collectWorkflowStatisticsWrapper collects and returns workflow statistics
func collectWorkflowStatisticsWrapper(markdownFiles []string) []*WorkflowStats {
	compileStatsLog.Printf("Collecting workflow statistics for %d files", len(markdownFiles))

	var statsList []*WorkflowStats
	for _, file := range markdownFiles {
		resolvedFile, err := resolveWorkflowFile(file, false)
		if err != nil {
			continue // Skip files that couldn't be resolved
		}
		lockFile := stringutil.MarkdownToLockFile(resolvedFile)
		if workflowStats, err := collectWorkflowStats(lockFile); err == nil {
			statsList = append(statsList, workflowStats)
		}
	}

	compileStatsLog.Printf("Collected statistics for %d workflows", len(statsList))
	return statsList
}

// displayStatsTable displays workflow statistics in a sorted table
func displayStatsTable(statsList []*WorkflowStats) {
	if len(statsList) == 0 {
		return
	}

	compileStatsLog.Printf("Displaying stats table: workflow_count=%d", len(statsList))

	// Sort by file size (descending)
	slices.SortFunc(statsList, func(a, b *WorkflowStats) int {
		if a.FileSize > b.FileSize {
			return -1
		}
		if a.FileSize < b.FileSize {
			return 1
		}
		return 0
	})

	// Calculate totals
	totalSize := int64(0)
	totalJobs := 0
	totalSteps := 0
	totalScripts := 0
	totalScriptSize := 0

	for _, stats := range statsList {
		totalSize += stats.FileSize
		totalJobs += stats.Jobs
		totalSteps += stats.Steps
		totalScripts += stats.ScriptCount
		totalScriptSize += stats.ScriptSize
	}

	// Limit display to top 10 workflows by size
	displayCount := len(statsList)
	const maxDisplay = 10
	if displayCount > maxDisplay {
		displayCount = maxDisplay
	}

	// Build table rows
	rows := make([][]string, 0, displayCount)
	for i, stats := range statsList {
		if i >= maxDisplay {
			break
		}
		// Check if workflow is above 500KB (512000 bytes)
		const maxSize = 500 * 1024
		workflowName := stats.Workflow
		fileSize := console.FormatFileSize(stats.FileSize)

		if stats.FileSize > maxSize {
			workflowName = console.FormatErrorTextStderr("✗ " + stats.Workflow)
			fileSize = console.FormatErrorTextStderr(console.FormatFileSize(stats.FileSize))
		}

		rows = append(rows, []string{
			workflowName,
			fileSize,
			strconv.Itoa(stats.Jobs),
			strconv.Itoa(stats.Steps),
			strconv.Itoa(stats.ScriptCount),
		})
	}

	// Create table config
	tableConfig := console.TableConfig{
		Title:   "",
		Headers: []string{"WORKFLOW", "FILE SIZE", "JOBS", "STEPS", "SCRIPTS"},
		Rows:    rows,
	}

	// Render and print table
	fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))

	// Print summary
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Summary:"))
	if len(statsList) > maxDisplay {
		fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Showing top %d of %d workflows (sorted by size)", maxDisplay, len(statsList))))
	}
	fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Total workflows: %d", len(statsList))))
	fmt.Fprintln(os.Stderr, console.FormatListItemStderr("Total size:      "+console.FormatFileSize(totalSize)))
	fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Total jobs:      %d", totalJobs)))
	fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Total steps:     %d", totalSteps)))
	fmt.Fprintln(os.Stderr, console.FormatListItemStderr(fmt.Sprintf("Total scripts:   %d (%s)", totalScripts, console.FormatFileSize(int64(totalScriptSize)))))
}
