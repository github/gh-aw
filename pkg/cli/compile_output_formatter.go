// This file provides output formatting functions for workflow compilation.
//
// This file contains functions that format and display compilation results,
// including summaries, statistics tables, and validation outputs.
//
// # Organization Rationale
//
// These output formatting functions are grouped here because they:
//   - Handle presentation layer concerns (what users see)
//   - Are used at the end of compilation operations
//   - Have a clear domain focus (output formatting and display)
//   - Keep the main orchestrator focused on orchestration logic
//
// # Key Functions
//
// Summary Output:
//   - formatCompilationSummary() - Format compilation statistics
//   - formatValidationOutput() - Format validation results as JSON
//
// These functions abstract output formatting, allowing the main compile
// orchestrator to focus on coordination while these handle presentation.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var compileOutputFormatterLog = logger.New("cli:compile_output_formatter")

// formatCompilationSummary formats compilation statistics for display
// This is a wrapper around printCompilationSummary for consistency
func formatCompilationSummary(stats *CompilationStats) {
	printCompilationSummary(stats)
}

// formatValidationOutput formats validation results as JSON
func formatValidationOutput(results []ValidationResult) (string, error) {
	compileOutputFormatterLog.Printf("Formatting validation output for %d workflow(s)", len(results))

	// Sanitize validation results before JSON marshaling to prevent logging of sensitive information
	// This removes potential secret key names from error messages at the output boundary
	sanitizedResults := sanitizeValidationResults(results)

	jsonBytes, err := json.MarshalIndent(sanitizedResults, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return string(jsonBytes), nil
}

// formatActionlintOutput displays the actionlint summary
// This is a wrapper around displayActionlintSummary for consistency
func formatActionlintOutput() {
	displayActionlintSummary()
}

// formatStatsTable displays the workflow statistics table
// This is a wrapper around displayStatsTable for consistency
func formatStatsTable(statsList []*WorkflowStats) {
	displayStatsTable(statsList)
}
