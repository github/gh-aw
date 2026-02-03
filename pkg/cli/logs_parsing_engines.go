// This file provides command-line interface functionality for gh-aw.
// This file (logs_parsing_engines.go) contains engine-specific log parsing
// functionality for various AI engines (Claude, Copilot, Codex, Custom).
//
// Key responsibilities:
//   - Parsing log files using engine-specific parsers
//   - Detecting and parsing GitHub Copilot agent logs
//   - Falling back to generic parser when engine is unknown

package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsParsingEnginesLog = logger.New("cli:logs_parsing_engines")

// parseLogFileWithEngine parses a log file using a specific engine or falls back to auto-detection
func parseLogFileWithEngine(filePath string, detectedEngine workflow.CodingAgentEngine, isGitHubCopilotAgent bool, verbose bool) (LogMetrics, error) {
	logsParsingEnginesLog.Printf("Parsing log file: %s, isGitHubCopilotAgent=%v", filePath, isGitHubCopilotAgent)
	// Read the entire log file at once to avoid JSON parsing issues from chunked reading
	content, err := os.ReadFile(filePath)
	if err != nil {
		logsParsingEnginesLog.Printf("Failed to read log file: %v", err)
		return LogMetrics{}, fmt.Errorf("error reading log file: %w", err)
	}

	logContent := string(content)
	logsParsingEnginesLog.Printf("Read %d bytes from log file", len(logContent))

	// If this is a GitHub Copilot agent run, use the specialized parser
	if isGitHubCopilotAgent {
		logsParsingEnginesLog.Print("Using GitHub Copilot agent parser")
		return ParseCopilotAgentLogMetrics(logContent, verbose), nil
	}

	// If we have a detected engine from aw_info.json, use it directly
	if detectedEngine != nil {
		logsParsingEnginesLog.Printf("Using detected engine: %s", detectedEngine.GetID())
		return detectedEngine.ParseLogMetrics(logContent, verbose), nil
	}

	// No aw_info.json metadata available - use fallback parser with common error patterns
	logsParsingEnginesLog.Print("No engine detected, using fallback parser with common error patterns")
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No aw_info.json found, using fallback parser"))
	}

	// Use empty metrics for fallback case
	var metrics LogMetrics

	return metrics, nil
}
