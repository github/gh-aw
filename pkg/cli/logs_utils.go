// This file provides command-line interface functionality for gh-aw.
// This file (logs_utils.go) contains utility functions used by the logs command.
//
// Key responsibilities:
//   - Discovering agentic workflow names from .lock.yml files
//   - Locating agent log files and output artifacts within downloaded artifact trees
//   - Utility functions for slice operations

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsUtilsLog = logger.New("cli:logs_utils")

// getAgenticWorkflowNames reads all .lock.yml files and extracts their workflow names
func getAgenticWorkflowNames(verbose bool) ([]string, error) {
	logsUtilsLog.Print("Discovering agentic workflow names from .lock.yml files")
	var workflowNames []string

	// Look for .lock.yml files in .github/workflows directory
	workflowsDir := constants.GetWorkflowDir()
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No .github/workflows directory found"))
		}
		return workflowNames, nil
	}

	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob .lock.yml files: %w", err)
	}

	logsUtilsLog.Printf("Found %d .lock.yml file(s) in %s", len(files), workflowsDir)

	for _, file := range files {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Reading workflow file: "+file))
		}

		content, err := os.ReadFile(file)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read %s: %v", file, err)))
			}
			continue
		}

		// Extract the workflow name using simple string parsing
		lines := strings.SplitSeq(string(content), "\n")
		for line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "name:") {
				// Parse the name field
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) == 2 {
					name := strings.TrimSpace(parts[1])
					// Remove quotes if present
					name = strings.Trim(name, `"'`)
					if name != "" {
						workflowNames = append(workflowNames, name)
						logsUtilsLog.Printf("Discovered workflow name: %s (from %s)", name, file)
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Found agentic workflow: "+name))
						}
						break
					}
				}
			}
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d agentic workflows", len(workflowNames))))
	}

	return workflowNames, nil
}

// findAgentOutputFile searches for a file named agent_output.json within the logDir tree.
// Returns the first path found (depth-first) and a boolean indicating success.
func findAgentOutputFile(logDir string) (string, bool) {
	var foundPath string
	if walkErr := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logsUtilsLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil {
			return nil
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), constants.AgentOutputArtifactName.String()) {
			foundPath = path
			return errWalkStop
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", logDir, walkErr)))
	}
	if foundPath == "" {
		return "", false
	}
	return foundPath, true
}

// findAgentLogFile searches for agent logs within the logDir.
// It uses engine.GetLogFileForParsing() to determine which log file to use:
//   - If GetLogFileForParsing() returns a non-empty value that doesn't point to agent-stdio.log,
//     look for files in the "agent_output" artifact directory (before flattening)
//     or in the flattened location (after flattening)
//   - Otherwise, look for the "agent-stdio.log" artifact file
//
// Returns the first path found and a boolean indicating success.
func findAgentLogFile(logDir string, engine workflow.CodingAgentEngine) (string, bool) {
	// Use GetLogFileForParsing to determine which log file to use
	logFileForParsing := engine.GetLogFileForParsing()

	// If the engine specifies a log file that isn't the default agent-stdio.log,
	// look in the agent_output artifact directory or flattened location
	if logFileForParsing != "" && logFileForParsing != defaultAgentStdioLogPath {
		// Check for agent_output directory (artifact, before flattening)
		agentOutputDir := filepath.Join(logDir, "agent_output")
		if fileutil.DirExists(agentOutputDir) {
			// Find the first file in this directory
			var foundFile string
			if walkErr := filepath.Walk(agentOutputDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					logsUtilsLog.Printf("walk error at %s: %v", path, err)
					return nil
				}
				if info == nil {
					return nil
				}
				if !info.IsDir() && foundFile == "" {
					foundFile = path
					return errWalkStop
				}
				return nil
			}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", agentOutputDir, walkErr)))
			}
			if foundFile != "" {
				return foundFile, true
			}
		}

		// Check for flattened location (after flattening)
		// The engine's log path is absolute (e.g., /tmp/gh-aw/sandbox/agent/logs/ for a
		// directory, or /tmp/gh-aw/pi-streaming.jsonl for a direct file).
		// After flattening, it's at logDir/<relative-path>.
		// Strip /tmp/gh-aw/ prefix to get the relative path
		const tmpGhAwPrefix = "/tmp/gh-aw/"
		if after, ok := strings.CutPrefix(logFileForParsing, tmpGhAwPrefix); ok {
			relPath := after
			flattenedPath := filepath.Join(logDir, relPath)
			logsUtilsLog.Printf("Checking flattened location for logs: %s", flattenedPath)
			// Case 1: the engine log path points to a specific file (e.g. pi-streaming.jsonl)
			if fileutil.FileExists(flattenedPath) {
				logsUtilsLog.Printf("Found engine log file at flattened path: %s", flattenedPath)
				return flattenedPath, true
			}
			// Case 2: the engine log path points to a directory — walk it for known log formats
			if fileutil.DirExists(flattenedPath) {
				// Prefer events.jsonl (structured Copilot session format) over debug .log files.
				// Walk the full tree: stop immediately when events.jsonl is found (preferred),
				// but keep walking after a .log match in case events.jsonl appears later.
				var foundEventsJsonl, foundLogFile string
				if walkErr := filepath.Walk(flattenedPath, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						logsUtilsLog.Printf("walk error at %s: %v", path, err)
						return nil
					}
					if info == nil {
						return nil
					}
					if !info.IsDir() {
						if info.Name() == "events.jsonl" && foundEventsJsonl == "" {
							foundEventsJsonl = path
							logsUtilsLog.Printf("Found events.jsonl file: %s", path)
							return errWalkStop
						} else if strings.HasSuffix(info.Name(), ".log") && foundLogFile == "" {
							foundLogFile = path
							logsUtilsLog.Printf("Found session log file: %s", path)
						}
					}
					return nil
				}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", flattenedPath, walkErr)))
				}
				if foundEventsJsonl != "" {
					return foundEventsJsonl, true
				}
				if foundLogFile != "" {
					return foundLogFile, true
				}
			}
			// Case 3: fall back to searching logDir for a file whose base name matches
			// the engine's log file name (handles unusual artifact layouts)
			targetBase := filepath.Base(logFileForParsing)
			logsUtilsLog.Printf("Searching logDir %s for engine log file by base name: %s", logDir, targetBase)
			var foundByBase string
			if walkErr := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					logsUtilsLog.Printf("walk error at %s: %v", path, err)
					return nil
				}
				if info == nil {
					return nil
				}
				if !info.IsDir() && info.Name() == targetBase && foundByBase == "" {
					foundByBase = path
					return errWalkStop
				}
				return nil
			}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", logDir, walkErr)))
			}
			if foundByBase != "" {
				logsUtilsLog.Printf("Found engine log file by base name: %s", foundByBase)
				return foundByBase, true
			}
		}

		// Fallback: search recursively in logDir for events.jsonl, session*.log or process*.log files
		// This handles cases where the artifact structure is different than expected
		// Note: Copilot changed from session-*.log to process-*.log naming convention
		logsUtilsLog.Printf("Searching recursively in %s for events.jsonl, session*.log or process*.log files", logDir)
		var foundEventsJsonl, foundLogFile string
		if walkErr := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				logsUtilsLog.Printf("walk error at %s: %v", path, err)
				return nil
			}
			if info == nil {
				return nil
			}
			fileName := info.Name()
			if !info.IsDir() {
				if fileName == "events.jsonl" && foundEventsJsonl == "" {
					foundEventsJsonl = path
					logsUtilsLog.Printf("Found events.jsonl via recursive search: %s", path)
					return errWalkStop
				} else if (strings.HasPrefix(fileName, "session") || strings.HasPrefix(fileName, "process")) && strings.HasSuffix(fileName, ".log") && foundLogFile == "" {
					foundLogFile = path
					logsUtilsLog.Printf("Found Copilot log file via recursive search: %s", path)
				}
			}
			return nil
		}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", logDir, walkErr)))
		}
		if foundEventsJsonl != "" {
			return foundEventsJsonl, true
		}
		if foundLogFile != "" {
			return foundLogFile, true
		}
	}

	// Default to agent-stdio.log
	agentStdioLog := filepath.Join(logDir, "agent-stdio.log")
	if fileutil.FileExists(agentStdioLog) {
		return agentStdioLog, true
	}

	// Also check for nested agent-stdio.log in case it's in a subdirectory
	var foundPath string
	if walkErr := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logsUtilsLog.Printf("walk error at %s: %v", path, err)
			return nil
		}
		if info == nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "agent-stdio.log" {
			foundPath = path
			return errWalkStop
		}
		return nil
	}); walkErr != nil && !errors.Is(walkErr, errWalkStop) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error walking %s: %v", logDir, walkErr)))
	}
	if foundPath != "" {
		return foundPath, true
	}

	return "", false
}
