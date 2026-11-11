package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

// extractDownloadedFiles scans the logs directory recursively and returns file information.
// It walks subdirectories (aw-prompts/, base/, etc.) so the JSON output enumerates every
// file available for inspection. Baseline directories are excluded to keep output focused.
func extractDownloadedFiles(logsPath string) []FileInfo {
	auditReportLog.Printf("Extracting downloaded files from: %s", logsPath)
	var files []FileInfo

	absLogsPath, err := filepath.Abs(logsPath)
	if err != nil {
		auditReportLog.Printf("Failed to resolve absolute logs path: %v", err)
		absLogsPath = logsPath
	}

	err = filepath.WalkDir(absLogsPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}

		// Skip baseline directories — they belong to comparison runs, not the audited run
		if d.IsDir() && strings.HasPrefix(d.Name(), "baseline-") {
			return filepath.SkipDir
		}

		// Skip the base/ directory — it's the full cloned repo, not a log artifact
		if d.IsDir() && d.Name() == "base" && path == filepath.Join(absLogsPath, "base") {
			return filepath.SkipDir
		}

		// Skip directories themselves (we only list files)
		if d.IsDir() {
			return nil
		}

		fileInfo := FileInfo{
			Path:        path,
			Description: describeFile(d.Name()),
		}

		if info, statErr := os.Stat(path); statErr == nil {
			fileInfo.Size = info.Size()
		}

		files = append(files, fileInfo)
		return nil
	})
	if err != nil {
		auditReportLog.Printf("Failed to walk logs directory: %v", err)
	}

	auditReportLog.Printf("Extracted %d files from logs directory", len(files))
	return files
}

// safeOutputItemsManifestFilename is the name of the manifest artifact file containing
// all items created in GitHub by safe output handlers.
const safeOutputItemsManifestFilename = "safe-output-items.jsonl"

// extractCreatedItemsFromManifest reads the safe output items manifest from the run
// output directory and returns the list of created items. Returns nil if the file
// does not exist or cannot be parsed.
func extractCreatedItemsFromManifest(logsPath string) []CreatedItemReport {
	if logsPath == "" {
		return nil
	}

	manifestPath := filepath.Join(logsPath, safeOutputItemsManifestFilename)
	f, err := os.Open(manifestPath)
	if err != nil {
		// File not present is expected for runs without safe outputs
		return nil
	}
	defer func() {
		if err := f.Close(); err != nil {
			auditReportLog.Printf("Failed to close safe-output manifest: %v", err)
		}
	}()

	var items []CreatedItemReport
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item CreatedItemReport
		if err := json.Unmarshal(line, &item); err != nil {
			auditReportLog.Printf("Skipping invalid manifest line: %v", err)
			continue
		}
		if item.Type == "" {
			continue
		}
		items = append(items, item)
	}

	if err := scanner.Err(); err != nil {
		auditReportLog.Printf("Error reading manifest file: %v", err)
	}

	auditReportLog.Printf("Extracted %d created item(s) from manifest", len(items))
	return items
}

// resolveCreatedItems returns created items read from the on-disk safe-output manifest
// at logsPath, falling back to entities already carried in-memory (e.g. from a cached
// usage/activity summary) only when the manifest file itself is absent — such as when an
// `--artifacts usage`-only cache is reused and the manifest was never downloaded. A manifest
// file that exists but legitimately contains no created items is not treated as missing, so
// runs with no safe outputs are reported as empty rather than falling back to stale cached data.
func resolveCreatedItems(logsPath string, cachedSafeOutputs []CreatedItemReport) []CreatedItemReport {
	if logsPath != "" {
		manifestPath := filepath.Join(logsPath, safeOutputItemsManifestFilename)
		_, err := os.Stat(manifestPath)
		switch {
		case err == nil:
			return extractCreatedItemsFromManifest(logsPath)
		case errors.Is(err, os.ErrNotExist):
			// Manifest genuinely absent (e.g. usage-only cache reuse); fall back below.
		default:
			auditReportLog.Printf("Error checking safe-output manifest %s: %v", manifestPath, err)
		}
	}
	return cachedSafeOutputs
}

// describeFile provides a short description for known artifact files
func describeFile(filename string) string {
	descriptions := map[string]string{
		"aw_info.json":                            "Engine configuration and workflow metadata",
		"safe_output.jsonl":                       "Safe outputs from workflow execution",
		safeOutputItemsManifestFilename:           "Created items manifest (audit trail)",
		constants.SafeOutputErrorsFilename:        "Safe outputs failure diagnostics (error code, message, failing types)",
		constants.AgentOutputFilename.String():    "Validated safe outputs",
		"aw.patch":                                "Git patch of changes made during execution",
		"agent-stdio.log":                         "Agent standard output/error logs",
		"log.md":                                  "Human-readable agent session summary",
		"firewall.md":                             "Firewall log analysis report",
		"run_summary.json":                        "Cached summary of workflow run analysis",
		forecastAICCacheFileName:                  "Cached AI Credits (AIC) value for forecasting",
		"prompt.txt":                              "Input prompt for AI agent",
		constants.GraderResultsFilename.String():  "Deterministic grader results for the run",
		constants.GraderManifestFilename.String(): "Grader manifest (configured graders and thresholds)",
	}

	if desc, ok := descriptions[filename]; ok {
		return desc
	}

	// Handle directories
	if strings.HasSuffix(filename, "/") {
		return "Directory"
	}

	// Common directory names
	if filename == "agent_output" || filename == "firewall-logs" || filename == "squid-logs" {
		return "Directory containing log files"
	}
	if filename == "aw-prompts" {
		return "Directory containing AI prompts"
	}

	// Handle file patterns by extension
	if strings.HasSuffix(filename, ".log") {
		return "Log file"
	}
	if strings.HasSuffix(filename, ".md") {
		return "Markdown documentation"
	}
	if strings.HasSuffix(filename, ".json") {
		return "JSON data file"
	}
	if strings.HasSuffix(filename, ".jsonl") {
		return "JSON Lines data file"
	}
	if strings.HasSuffix(filename, ".patch") {
		return "Git patch file"
	}
	if strings.HasSuffix(filename, ".txt") {
		return "Text file"
	}

	return ""
}

// parseDurationString parses a duration string back to time.Duration (best effort)
func parseDurationString(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}
