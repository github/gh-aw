package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
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

// parseCleanupCutoff resolves a date string (absolute or relative delta) to a
// time.Time that can be used as a cutoff for the cache cleanup. Accepts the
// same formats as --start-date / --end-date (e.g. "-1w", "-30d", "2024-01-01").
func parseCleanupCutoff(after string) (time.Time, error) {
	cutoffStr, err := workflow.ResolveRelativeDate(after, time.Now())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --after value '%s': %w", after, err)
	}

	// ResolveRelativeDate returns an RFC3339 timestamp for relative inputs and
	// the original string for absolute dates.
	if t, parseErr := time.Parse(time.RFC3339, cutoffStr); parseErr == nil {
		return t, nil
	}
	if t, parseErr := time.Parse("2006-01-02", cutoffStr); parseErr == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --after value '%s': could not parse resolved date '%s'", after, cutoffStr)
}

// cleanupOldRunFolders removes cached run folders from outputDir whose run creation
// date is before the given cutoff time. Only directories matching the "run-{ID}"
// naming pattern are considered. The creation date is read from run_summary.json
// inside each folder; if that file is absent the directory modification time is
// used as a fallback. Returns the number of folders removed.
func cleanupOldRunFolders(outputDir string, cutoff time.Time, verbose bool) (int, error) {
	logsCacheLog.Printf("Cleaning up run folders older than %s in %s", cutoff.Format(time.RFC3339), outputDir)

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read output directory: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		// Only consider directories whose name is exactly "run-{integer}" to avoid
		// accidentally deleting unrelated directories like "run-backup" or "run-temp".
		if _, parseErr := strconv.ParseInt(strings.TrimPrefix(entry.Name(), "run-"), 10, 64); parseErr != nil {
			logsCacheLog.Printf("Skipping non-run directory: %s", entry.Name())
			continue
		}

		runDir := filepath.Join(outputDir, entry.Name())

		// Determine the run date: prefer the GitHub run creation timestamp from
		// run_summary.json so the cutoff is relative to when the workflow actually
		// ran, not when we downloaded or processed it.
		var runDate time.Time
		summaryPath := filepath.Join(runDir, runSummaryFileName)
		if data, readErr := os.ReadFile(summaryPath); readErr == nil {
			var summary RunSummary
			if jsonErr := json.Unmarshal(data, &summary); jsonErr == nil && !summary.Run.CreatedAt.IsZero() {
				runDate = summary.Run.CreatedAt
			}
		}

		// Fall back to directory modification time when the summary is unavailable.
		if runDate.IsZero() {
			info, statErr := entry.Info()
			if statErr != nil {
				logsCacheLog.Printf("Failed to stat run directory %s: %v", entry.Name(), statErr)
				continue
			}
			runDate = info.ModTime()
		}

		if runDate.Before(cutoff) {
			logsCacheLog.Printf("Removing old run folder: %s (run date: %s, cutoff: %s)", entry.Name(), runDate.Format(time.RFC3339), cutoff.Format(time.RFC3339))
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Removing old run folder: %s (run date: %s)", entry.Name(), runDate.Format("2006-01-02"))))
			}
			if removeErr := os.RemoveAll(runDir); removeErr != nil {
				logsCacheLog.Printf("Failed to remove run folder %s: %v", entry.Name(), removeErr)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove old run folder %s: %v", entry.Name(), removeErr)))
				continue
			}
			removed++
		}
	}

	logsCacheLog.Printf("Removed %d old run folders (cutoff: %s)", removed, cutoff.Format(time.RFC3339))
	return removed, nil
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
	if err := os.WriteFile(summaryPath, data, constants.FilePermPublic); err != nil {
		logsCacheLog.Printf("Failed to write run summary to disk: %v", err)
		return fmt.Errorf("failed to write run summary: %w", err)
	}

	logsCacheLog.Printf("Successfully saved run summary cache: path=%s", summaryPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Saved run summary to "+summaryPath))
	}

	return nil
}
