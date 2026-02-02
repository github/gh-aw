package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var logsCacheLog = logger.New("cli:logs_cache")

// loadRunSummary attempts to load a run summary from disk
// Returns the summary and a boolean indicating if it was successfully loaded and is valid
func loadRunSummary(outputDir string, verbose bool) (*RunSummary, bool) {
	logsCacheLog.Printf("Loading run summary from cache: dir=%s", outputDir)
	summaryPath := filepath.Join(outputDir, runSummaryFileName)

	// Check if summary file exists
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		logsCacheLog.Print("Run summary cache file does not exist")
		return nil, false
	}

	// Read the summary file
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		logsCacheLog.Printf("Failed to read run summary cache: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read run summary: %v", err)))
		}
		return nil, false
	}

	// Parse the JSON
	var summary RunSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		logsCacheLog.Printf("Failed to parse run summary JSON: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse run summary: %v", err)))
		}
		return nil, false
	}

	// Validate CLI version matches
	currentVersion := GetVersion()
	if summary.CLIVersion != currentVersion {
		logsCacheLog.Printf("CLI version mismatch: cached=%s, current=%s", summary.CLIVersion, currentVersion)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run summary version mismatch (cached: %s, current: %s), will reprocess", summary.CLIVersion, currentVersion)))
		}
		return nil, false
	}

	logsCacheLog.Printf("Successfully loaded cached run summary: run_id=%d", summary.RunID)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Loaded cached run summary for run %d (processed at %s)", summary.RunID, summary.ProcessedAt.Format(time.RFC3339))))
	}

	return &summary, true
}

// saveRunSummary saves a run summary to disk
func saveRunSummary(outputDir string, summary *RunSummary, verbose bool) error {
	logsCacheLog.Printf("Saving run summary to cache: dir=%s, run_id=%d", outputDir, summary.RunID)
	summaryPath := filepath.Join(outputDir, runSummaryFileName)

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		logsCacheLog.Printf("Failed to marshal run summary: %v", err)
		return fmt.Errorf("failed to marshal run summary: %w", err)
	}

	// Write to file
	if err := os.WriteFile(summaryPath, data, 0644); err != nil {
		logsCacheLog.Printf("Failed to write run summary to disk: %v", err)
		return fmt.Errorf("failed to write run summary: %w", err)
	}

	logsCacheLog.Printf("Successfully saved run summary cache: path=%s", summaryPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Saved run summary to %s", summaryPath)))
	}

	return nil
}
